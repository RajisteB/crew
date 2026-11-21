// Package gitx is a thin wrapper over the git CLI for worktree management.
// Every command runs through exec with an argument list, never a shell.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// stderrLines is how many trailing stderr lines an error carries.
const stderrLines = 5

// Repo is a git repository's main checkout.
type Repo struct{ Dir string }

// git runs git in dir and returns stdout. Errors carry the tail of stderr.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, tail(stderr.String()))
	}
	return stdout.String(), nil
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
func (r Repo) Root() (string, error) {
	out, err := git(r.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Fetch runs `git fetch origin <branch>` so origin/<branch> is current.
func (r Repo) Fetch(branch string) error {
	if err := checkRef("branch", branch); err != nil {
		return err
	}
	_, err := git(r.Dir, "fetch", "origin", branch)
	return err
}

// AddWorktree creates a worktree at path on a new branch started at from.
func (r Repo) AddWorktree(path, branch, from string) error {
	if err := checkRef("branch", branch); err != nil {
		return err
	}
	if err := checkRef("start point", from); err != nil {
		return err
	}
	_, err := git(r.Dir, "worktree", "add", "-b", branch, "--", path, from)
	return err
}

// RemoveWorktree removes the worktree at path. It returns nil when the
// worktree is already gone. Without force, git refuses uncommitted changes.
func (r Repo) RemoveWorktree(path string, force bool) error {
	listed, err := r.Worktrees()
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
	if _, err := git(r.Dir, args...); err != nil {
		return r.pruneIfMissing(abs, err)
	}
	return nil
}

// pruneIfMissing treats a worktree whose directory vanished as removed.
func (r Repo) pruneIfMissing(abs string, cause error) error {
	if _, statErr := os.Stat(abs); statErr == nil {
		return cause
	}
	if _, err := git(r.Dir, "worktree", "prune"); err != nil {
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
func (r Repo) Worktrees() ([]string, error) {
	out, err := git(r.Dir, "worktree", "list", "--porcelain")
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
func (r Repo) Dirty(worktree string) ([]string, error) {
	out, err := git(worktree, "status", "--porcelain", "-z", "--untracked-files=all")
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
func (r Repo) Ahead(worktree, base string) (int, error) {
	if err := checkRef("base", base); err != nil {
		return 0, err
	}
	out, err := git(worktree, "rev-list", "--count", base+"..HEAD")
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
func (r Repo) Push(worktree, branch string) error {
	if err := checkRef("branch", branch); err != nil {
		return err
	}
	_, err := git(worktree, "push", "-u", "origin", branch)
	return err
}
