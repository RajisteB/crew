// Package gitx is a thin wrapper over the git CLI for worktree management.
// Every command runs through exec with an argument list, never a shell.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// stderrLines is how many trailing stderr lines an error carries.
	stderrLines = 5
	// networkTimeout bounds a git command that reaches the network (fetch,
	// push, ls-remote); localTimeout bounds every other one.
	networkTimeout = 2 * time.Minute
	localTimeout   = 30 * time.Second
	// waitDelay is how long a killed command's output pipes may stay open
	// (an ssh child can outlive git) before they are closed.
	waitDelay = 2 * time.Second
)

// Repo is a git repository's main checkout.
type Repo struct{ Dir string }

// git runs git in dir with a deadline of timeout (and ctx's own) and returns
// stdout. Errors carry the tail of stderr. Git never prompts for credentials:
// a prompt would block the engine, so it fails instead. The principal's own
// ssh configuration is left alone.
func git(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = waitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	if err := cmd.Run(); err != nil {
		if stopped := stoppedError(ctx, cctx, args[0], timeout, time.Since(start)); stopped != nil {
			return "", stopped
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, tail(stderr.String()))
	}
	return stdout.String(), nil
}

// stoppedError says a command was cut short: cancelled by the caller or out
// of time. It is nil when the context is still alive.
func stoppedError(parent, cctx context.Context, name string, timeout, took time.Duration) error {
	switch {
	case cctx.Err() == nil:
		return nil
	case errors.Is(parent.Err(), context.Canceled):
		return fmt.Errorf("git %s: cancelled", name)
	case parent.Err() != nil: // the caller's own deadline
		return fmt.Errorf("git %s: timed out after %s", name, took.Round(time.Millisecond))
	}
	return fmt.Errorf("git %s: timed out after %s", name, timeout)
}

func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > stderrLines {
		lines = lines[len(lines)-stderrLines:]
	}
	return strings.Join(lines, "\n")
}

// checkRef rejects empty names and names git would parse as options.
func checkRef(kind, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid %s %q", kind, name)
	}
	return nil
}

// Root returns the absolute path of the repository's top-level directory.
func (r Repo) Root(ctx context.Context) (string, error) {
	out, err := git(ctx, localTimeout, r.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Fetch runs `git fetch origin <branch>` so origin/<branch> is current.
func (r Repo) Fetch(ctx context.Context, branch string) error {
	if err := checkRef("branch", branch); err != nil {
		return err
	}
	_, err := git(ctx, networkTimeout, r.Dir, "fetch", "origin", branch)
	return err
}

// AddWorktree creates a worktree at path on branch, started at from. An
// existing branch is reset to from, so a failed start can be retried.
func (r Repo) AddWorktree(ctx context.Context, path, branch, from string) error {
	if err := checkRef("branch", branch); err != nil {
		return err
	}
	if err := checkRef("start point", from); err != nil {
		return err
	}
	_, err := git(ctx, localTimeout, r.Dir, "worktree", "add", "--no-track", "-B", branch, "--", path, from)
	return err
}

// RemoveWorktree removes the worktree at path. It returns nil when the
// worktree is already gone. Without force, git refuses uncommitted changes.
func (r Repo) RemoveWorktree(ctx context.Context, path string, force bool) error {
	listed, err := r.Worktrees(ctx)
	if err != nil {
		return err
	}
	abs := r.canonical(path)
	found := false
	for _, w := range listed {
		if w == abs {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--", path)
	if _, err := git(ctx, localTimeout, r.Dir, args...); err != nil {
		return r.pruneIfMissing(ctx, abs, err)
	}
	return nil
}

// pruneIfMissing treats a worktree whose directory vanished as removed.
func (r Repo) pruneIfMissing(ctx context.Context, abs string, cause error) error {
	if _, statErr := os.Stat(abs); statErr == nil {
		return cause
	}
	if _, err := git(ctx, localTimeout, r.Dir, "worktree", "prune"); err != nil {
		return errors.Join(cause, err)
	}
	return nil
}

// canonical returns path made absolute (relative to the repo) with symlinks
// resolved as far as the path exists.
func (r Repo) canonical(path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.Dir, path)
	}
	path = filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	if real, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(real, filepath.Base(path))
	}
	return path
}

// Worktrees lists the absolute paths of all worktrees, main checkout first.
func (r Repo) Worktrees(ctx context.Context) ([]string, error) {
	out, err := git(ctx, localTimeout, r.Dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, r.canonical(p))
		}
	}
	return paths, nil
}

// Dirty lists paths with uncommitted changes, including untracked files but
// not ignored ones.
func (r Repo) Dirty(ctx context.Context, worktree string) ([]string, error) {
	out, err := git(ctx, localTimeout, worktree, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var paths []string
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		paths = append(paths, f[3:])
		if f[0] == 'R' || f[0] == 'C' {
			i++ // the next field is the rename source
		}
	}
	return paths, nil
}

// Ahead counts the commits in base..HEAD of the worktree.
func (r Repo) Ahead(ctx context.Context, worktree, base string) (int, error) {
	if err := checkRef("base", base); err != nil {
		return 0, err
	}
	out, err := git(ctx, localTimeout, worktree, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("git rev-list: unexpected output %q", out)
	}
	return n, nil
}

// Push runs `git push -u origin <branch>` from the worktree.
func (r Repo) Push(ctx context.Context, worktree, branch string) error {
	if err := checkRef("branch", branch); err != nil {
		return err
	}
	_, err := git(ctx, networkTimeout, worktree, "push", "-u", "origin", branch)
	return err
}

// RemoteURL returns the URL of the named remote.
func (r Repo) RemoteURL(ctx context.Context, name string) (string, error) {
	if err := checkRef("remote", name); err != nil {
		return "", err
	}
	out, err := git(ctx, localTimeout, r.Dir, "remote", "get-url", "--", name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Head returns the commit the worktree's HEAD points at.
func (r Repo) Head(ctx context.Context, worktree string) (string, error) {
	out, err := git(ctx, localTimeout, worktree, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ChangedFiles lists the files the worktree's branch changed since it left
// base: git diff --name-only base...HEAD.
func (r Repo) ChangedFiles(ctx context.Context, worktree, base string) ([]string, error) {
	if err := checkRef("base", base); err != nil {
		return nil, err
	}
	return nameList(ctx, worktree, "diff", "--name-only", "-z", base+"...HEAD")
}

// nameList runs a git command that prints NUL-separated paths.
func nameList(ctx context.Context, dir string, args ...string) ([]string, error) {
	out, err := git(ctx, localTimeout, dir, args...)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// MergeInto merges from into the worktree's branch with --no-edit. When the
// merge conflicts it collects the conflicted paths, runs git merge --abort,
// and returns the paths with a nil error. A clean merge, or nothing to
// merge, returns no paths. A merge that stops for any other reason is an
// error, and the worktree is not left mid-merge.
func (r Repo) MergeInto(ctx context.Context, worktree, from string) ([]string, error) {
	if err := checkRef("merge source", from); err != nil {
		return nil, err
	}
	_, mergeErr := git(ctx, localTimeout, worktree, "merge", "--no-edit", from)
	if mergeErr == nil {
		return nil, nil
	}
	if _, err := git(ctx, localTimeout, worktree, "rev-parse", "-q", "--verify", "MERGE_HEAD"); err != nil {
		return nil, mergeErr // no merge in progress: nothing to undo
	}
	conflicts, listErr := nameList(ctx, worktree, "diff", "--name-only", "-z", "--diff-filter=U")
	if _, err := git(ctx, localTimeout, worktree, "merge", "--abort"); err != nil {
		return nil, errors.Join(mergeErr, listErr, err)
	}
	if listErr != nil || len(conflicts) == 0 {
		return nil, errors.Join(mergeErr, listErr)
	}
	return conflicts, nil
}
