package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/crewfile-dev/crew/internal/config"
)

// StationRequest is one station to run on one harness.
type StationRequest struct {
	Harness, Agent, Prompt, WorkDir string
	Allowed                         []string
	Deny                            []string // native deny rules the harness must enforce (see DenyRules)
	Timeout                         time.Duration
	OnStart                         func(pid int) // called once the station's process exists
}

// Reply is what a station returned.
type Reply struct {
	Text    string
	Denials []string // tool calls the harness refused (outside the agent's capabilities)
}

// Runner runs a station and returns its reply.
type Runner interface {
	Run(ctx context.Context, req StationRequest) (Reply, error)
}

// HeadlessRunner runs stations through each harness's headless: command.
type HeadlessRunner struct{ Registry *config.Registry }

// ErrCanceled marks a station stopped because the run itself was stopped.
var ErrCanceled = errors.New("station stopped: run interrupted")

// IsCanceled reports whether a Run error means the run was interrupted.
func IsCanceled(err error) bool { return errors.Is(err, ErrCanceled) }

func (r HeadlessRunner) Run(ctx context.Context, req StationRequest) (Reply, error) {
	h, ok := r.Registry.Harnesses[req.Harness]
	if !ok || h.Headless == nil {
		return Reply{}, fmt.Errorf("%s: no headless: command in harnesses.yaml", req.Harness)
	}
	settings, cleanup, err := writeSettings(req.Harness, *h.Headless, req.Deny)
	if err != nil {
		return Reply{}, err
	}
	defer cleanup()
	argv := expandCommand(*h.Headless, req, settings)
	if _, err := exec.LookPath(argv[0]); err != nil {
		return Reply{}, fmt.Errorf("%s: command %q not found on PATH (headless.command in harnesses.yaml)", req.Harness, argv[0])
	}
	out, err := r.exec(ctx, req, *h.Headless, argv)
	if err != nil {
		return Reply{}, err
	}
	if h.Headless.Output == "claude-json" {
		return parseClaudeJSON(req.Harness, out)
	}
	return Reply{Text: out}, nil
}

func (r HeadlessRunner) exec(ctx context.Context, req StationRequest, h config.HeadlessSpec, argv []string) (string, error) {
	tctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, argv[0], argv[1:]...)
	cmd.Dir = req.WorkDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	if h.PromptVia == "stdin" {
		cmd.Stdin = strings.NewReader(req.Prompt)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Start()
	if err == nil {
		if req.OnStart != nil {
			req.OnStart(cmd.Process.Pid)
		}
		err = cmd.Wait()
	}
	switch {
	case ctx.Err() != nil:
		return "", ErrCanceled
	case tctx.Err() != nil:
		return "", fmt.Errorf("%s station timed out after %v", req.Harness, req.Timeout)
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			detail := tail(stderr.String(), 20)
			if strings.TrimSpace(detail) == "" {
				detail = tail(stdout.String(), 20)
			}
			return "", fmt.Errorf("%s station failed (exit %d) running %s: %s", req.Harness, ee.ExitCode(), argv[0], detail)
		}
		return "", fmt.Errorf("%s station: %w", req.Harness, err)
	}
	return stdout.String(), nil
}

// expandCommand fills in {agent}, {prompt}, {allowed} and {settings}. An empty
// allowlist (or an empty settings path) drops that argument and the flag
// right before it.
func expandCommand(h config.HeadlessSpec, req StationRequest, settings string) []string {
	join := h.AllowJoin
	if join == "" {
		join = ","
	}
	allowed := strings.Join(req.Allowed, join)
	var out []string
	for _, a := range h.Command {
		if (a == "{allowed}" && allowed == "") || (a == "{settings}" && settings == "") {
			if n := len(out); n > 0 && strings.HasPrefix(out[n-1], "-") {
				out = out[:n-1]
			}
			continue
		}
		a = strings.ReplaceAll(a, "{agent}", req.Agent)
		a = strings.ReplaceAll(a, "{allowed}", allowed)
		a = strings.ReplaceAll(a, "{settings}", settings)
		a = strings.ReplaceAll(a, "{prompt}", req.Prompt)
		out = append(out, a)
	}
	return out
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// parseClaudeJSON reads `claude -p --output-format json`: the reply is
// .result; refused tool calls are in .permission_denials.
func parseClaudeJSON(harness, out string) (Reply, error) {
	var res struct {
		IsError     bool   `json:"is_error"`
		Result      string `json:"result"`
		Denials     []struct {
			Tool  string         `json:"tool_name"`
			Input map[string]any `json:"tool_input"`
		} `json:"permission_denials"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &res); err != nil {
		return Reply{}, fmt.Errorf("%s: output is not the expected JSON (headless.output): %v: %s", harness, err, tail(out, 5))
	}
	if res.IsError {
		return Reply{}, fmt.Errorf("%s station reported an error: %s", harness, res.Result)
	}
	r := Reply{Text: res.Result}
	for _, d := range res.Denials {
		desc := d.Tool
		if c, ok := d.Input["command"].(string); ok {
			desc += ": " + c
		} else if p, ok := d.Input["file_path"].(string); ok {
			desc += ": " + p
		}
		r.Denials = append(r.Denials, desc)
	}
	return r, nil
}
