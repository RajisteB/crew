// Package gh is a thin wrapper over the GitHub CLI for repo info and pull requests.
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/crewfile-dev/crew/internal/config"
)

const (
	binary         = "gh"
	stderrTailRows = 5
	stateOpen      = "OPEN"
	// callTimeout bounds each gh call; a dropped network or a stuck login
	// must not hang the engine.
	callTimeout = 60 * time.Second
	waitDelay   = 2 * time.Second
)

// errMissing is returned when the gh binary is not installed.
var errMissing = errors.New("gh not found on PATH; install it and run gh auth login")

// Exec runs a command in dir with stdin and returns its stdout.
type Exec func(ctx context.Context, dir string, stdin []byte, name string, args ...string) (stdout []byte, err error)

// Client runs gh in Dir. A nil Exec runs the real binary. A zero Timeout
// means callTimeout.
type Client struct {
	Dir     string
	Exec    Exec
	Timeout time.Duration
}

// RepoInfo describes the repository in Dir. Visibility is lowercase.
type RepoInfo struct{ Slug, Visibility, DefaultBranch string }

// PR is a pull request. State is OPEN, MERGED or CLOSED. Mergeable is
// MERGEABLE, CONFLICTING or UNKNOWN; CreatePR and FindPR leave it, HeadSHA
// and Checks empty.
type PR struct {
	Number    int
	State     string
	URL       string
	Mergeable string
	HeadSHA   string
	Checks    []Check
}

// Repo returns the slug, visibility and default branch of the current repository.
func (c Client) Repo(ctx context.Context) (RepoInfo, error) {
	args := []string{"repo", "view", "--json", "nameWithOwner,visibility,defaultBranchRef"}
	out, err := c.run(ctx, nil, args...)
	if err != nil {
		return RepoInfo{}, err
	}
	var v struct {
		NameWithOwner    string `json:"nameWithOwner"`
		Visibility       string `json:"visibility"`
		DefaultBranchRef struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
	}
	if err := json.Unmarshal(out, &v); err != nil || v.NameWithOwner == "" || v.Visibility == "" {
		return RepoInfo{}, unparseable(args, err)
	}
	return RepoInfo{
		Slug:          v.NameWithOwner,
		Visibility:    strings.ToLower(v.Visibility),
		DefaultBranch: v.DefaultBranchRef.Name,
	}, nil
}

// CreatePR opens a pull request. The body travels on stdin, never in args.
func (c Client) CreatePR(ctx context.Context, head, base, title, body string) (PR, error) {
	args := []string{"pr", "create", "--head=" + head, "--base=" + base, "--title=" + title, "--body-file", "-"}
	out, err := c.run(ctx, []byte(body), args...)
	if err != nil {
		return PR{}, err
	}
	url := lastLine(string(out))
	n, convErr := strconv.Atoi(url[strings.LastIndex(url, "/")+1:])
	if url == "" || convErr != nil || n <= 0 {
		return PR{}, unparseable(args, convErr)
	}
	return PR{Number: n, State: stateOpen, URL: url}, nil
}

// PRState returns the state, mergeability, head commit and checks of pull request number.
func (c Client) PRState(ctx context.Context, number int) (PR, error) {
	args := []string{"pr", "view", strconv.Itoa(number), "--json", "number,state,url,mergeable,headRefOid,statusCheckRollup"}
	out, err := c.run(ctx, nil, args...)
	if err != nil {
		return PR{}, err
	}
	var v struct {
		Number            int           `json:"number"`
		State             string        `json:"state"`
		URL               string        `json:"url"`
		Mergeable         string        `json:"mergeable"`
		HeadRefOid        string        `json:"headRefOid"`
		StatusCheckRollup []rollupEntry `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &v); err != nil || v.Number == 0 || v.State == "" {
		return PR{}, unparseable(args, err)
	}
	return PR{
		Number: v.Number, State: v.State, URL: v.URL,
		Mergeable: v.Mergeable, HeadSHA: v.HeadRefOid,
		Checks: checksFromRollup(v.StatusCheckRollup),
	}, nil
}

// FindPR returns the pull request whose head is branch, in any state. An
// empty list is (PR{}, false, nil).
func (c Client) FindPR(ctx context.Context, head string) (PR, bool, error) {
	args := []string{"pr", "list", "--head=" + head, "--state", "all", "--json", "number,state,url", "--limit", "1"}
	out, err := c.run(ctx, nil, args...)
	if err != nil {
		return PR{}, false, err
	}
	var v []struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return PR{}, false, unparseable(args, err)
	}
	if len(v) == 0 {
		return PR{}, false, nil
	}
	if v[0].Number == 0 || v[0].State == "" {
		return PR{}, false, unparseable(args, nil)
	}
	return PR{Number: v[0].Number, State: v[0].State, URL: v[0].URL}, true, nil
}

func (c Client) run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	exe := c.Exec
	if exe == nil {
		exe = realExec
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = callTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exe(cctx, c.Dir, stdin, binary, args...)
	if err != nil {
		if ctx.Err() == nil && errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%s: timed out after %s", describe(args), timeout)
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errMissing
		}
		return nil, fmt.Errorf("%s: %w", describe(args), err)
	}
	return out, nil
}

func realExec(ctx context.Context, dir string, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Env = append(config.CleanEnv(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	cmd.WaitDelay = waitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := tail(stderr.String(), stderrTailRows); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// describe names the command for error messages: "gh pr view".
func describe(args []string) string {
	words := []string{binary}
	for _, a := range args {
		if strings.HasPrefix(a, "-") || len(words) == 3 {
			break
		}
		words = append(words, a)
	}
	return strings.Join(words, " ")
}

func unparseable(args []string, cause error) error {
	if cause == nil {
		cause = errors.New("missing fields")
	}
	return fmt.Errorf("%s: unparseable output: %w", describe(args), cause)
}

// tail returns the last n non-empty lines of s.
func tail(s string, n int) string {
	var rows []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			rows = append(rows, l)
		}
	}
	if len(rows) > n {
		rows = rows[len(rows)-n:]
	}
	return strings.Join(rows, "\n")
}

func lastLine(s string) string {
	return tail(strings.TrimSpace(s), 1)
}
