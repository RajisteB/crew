package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// isolateGit makes git independent of the developer's configuration.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo builds a repo on branch main with one commit and a bare origin.
func newRepo(t *testing.T) (Repo, string) {
	t.Helper()
	isolateGit(t)
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	dir := filepath.Join(root, "work")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, root, "init", "--bare", "-b", "main", origin)
	run(t, dir, "init", "-b", "main")
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	write(t, filepath.Join(dir, ".gitignore"), "ignored.log\n")
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-m", "init")
	run(t, dir, "remote", "add", "origin", origin)
	run(t, dir, "push", "-u", "origin", "main")
	return Repo{Dir: dir}, origin
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAddWorktree_CreatesBranchFromBase(t *testing.T) {
	repo, _ := newRepo(t)
	if err := repo.Fetch("main"); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if err := repo.AddWorktree(wt, "crew/m1/p1", "origin/main"); err != nil {
		t.Fatal(err)
	}
	if got := run(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); got != "crew/m1/p1" {
		t.Fatalf("branch = %q", got)
	}
	if run(t, wt, "rev-parse", "HEAD") != run(t, repo.Dir, "rev-parse", "origin/main") {
		t.Fatal("worktree not at origin/main")
	}
	list, err := repo.Worktrees()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(list, realPath(t, wt)) {
		t.Fatalf("worktrees %v missing %s", list, wt)
	}
	root, err := repo.Root()
	if err != nil || root != realPath(t, repo.Dir) {
		t.Fatalf("root = %q, %v", root, err)
	}
}

func TestAddWorktree_ExistingBranchSucceeds(t *testing.T) {
	repo, _ := newRepo(t)
	if err := repo.Fetch("main"); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if err := repo.AddWorktree(wt, "crew/m1/p1", "origin/main"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveWorktree(wt, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddWorktree(wt, "crew/m1/p1", "origin/main"); err != nil {
		t.Fatalf("second add: %v", err)
	}
	if got := run(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); got != "crew/m1/p1" {
		t.Fatalf("branch = %q", got)
	}
}

func TestAddWorktree_RejectsDashBranch(t *testing.T) {
	repo, _ := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := repo.AddWorktree(wt, "-evil", "main"); err == nil {
		t.Fatal("expected error for branch starting with -")
	}
	if err := repo.AddWorktree(wt, "ok", "--upload-pack=x"); err == nil {
		t.Fatal("expected error for ref starting with -")
	}
}

func TestRemoveWorktree_TwiceIsNil(t *testing.T) {
	repo, _ := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := repo.AddWorktree(wt, "b1", "main"); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if err := repo.RemoveWorktree(wt, false); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	// Directory deleted by hand.
	wt2 := filepath.Join(t.TempDir(), "wt2")
	if err := repo.AddWorktree(wt2, "b2", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(wt2); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveWorktree(wt2, false); err != nil {
		t.Fatalf("hand-deleted: %v", err)
	}
	list, _ := repo.Worktrees()
	if len(list) != 1 {
		t.Fatalf("worktrees = %v", list)
	}
}

func TestRemoveWorktree_RefusesDirtyWithoutForce(t *testing.T) {
	repo, _ := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := repo.AddWorktree(wt, "b1", "main"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wt, "a.txt"), "changed\n")
	err := repo.RemoveWorktree(wt, false)
	if err == nil || !strings.Contains(err.Error(), "use --force") {
		t.Fatalf("expected git's refusal, got %v", err)
	}
	if _, statErr := os.Stat(wt); statErr != nil {
		t.Fatal("worktree should still exist")
	}
	if err := repo.RemoveWorktree(wt, true); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatal("worktree should be gone")
	}
}

func TestDirty_ListsUntrackedAndModified(t *testing.T) {
	repo, _ := newRepo(t)
	got, err := repo.Dirty(repo.Dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("clean: %v %v", got, err)
	}
	write(t, filepath.Join(repo.Dir, "a.txt"), "changed\n")
	write(t, filepath.Join(repo.Dir, "new.txt"), "n\n")
	write(t, filepath.Join(repo.Dir, "ignored.log"), "x\n")
	got, err = repo.Dirty(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if want := []string{"a.txt", "new.txt"}; !slices.Equal(got, want) {
		t.Fatalf("dirty = %v, want %v", got, want)
	}
}

func TestAhead_CountsCommits(t *testing.T) {
	repo, _ := newRepo(t)
	n, err := repo.Ahead(repo.Dir, "origin/main")
	if err != nil || n != 0 {
		t.Fatalf("ahead = %d, %v", n, err)
	}
	for _, f := range []string{"b.txt", "c.txt"} {
		write(t, filepath.Join(repo.Dir, f), f)
		run(t, repo.Dir, "add", f)
		run(t, repo.Dir, "commit", "-m", f)
	}
	n, err = repo.Ahead(repo.Dir, "origin/main")
	if err != nil || n != 2 {
		t.Fatalf("ahead = %d, %v", n, err)
	}
}

func TestPush_SendsBranchToOrigin(t *testing.T) {
	repo, origin := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := repo.AddWorktree(wt, "crew/m1/p1", "main"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wt, "f.txt"), "f")
	run(t, wt, "add", "f.txt")
	run(t, wt, "commit", "-m", "f")
	if err := repo.Push(wt, "crew/m1/p1"); err != nil {
		t.Fatal(err)
	}
	if run(t, origin, "rev-parse", "crew/m1/p1") != run(t, wt, "rev-parse", "HEAD") {
		t.Fatal("origin branch not at worktree HEAD")
	}
	if err := repo.Push(wt, "-bad"); err == nil {
		t.Fatal("expected error for branch starting with -")
	}
}

func TestRemoteURL_ReturnsOrigin(t *testing.T) {
	repo, origin := newRepo(t)
	got, err := repo.RemoteURL("origin")
	if err != nil {
		t.Fatal(err)
	}
	if got != origin {
		t.Fatalf("got %q want %q", got, origin)
	}
	if _, err := repo.RemoteURL("nope"); err == nil {
		t.Fatal("expected error for a missing remote")
	}
}

func TestAddWorktree_HasNoUpstream(t *testing.T) {
	repo, _ := newRepo(t)
	if err := repo.Fetch("main"); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if err := repo.AddWorktree(wt, "crew/m1/p1", "origin/main"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "@{u}")
	cmd.Dir = wt
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the piece branch tracks %q; it must have no upstream", strings.TrimSpace(string(out)))
	}
}
