package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktree_ScratchIsIgnored(t *testing.T) {
	repo, _ := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := repo.AddWorktree(ctx, wt, "b1", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, ".crew", "deliverables"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(wt, ".crew", ".gitignore"), "*\n")
	write(t, filepath.Join(wt, ".crew", "deliverables", "x.md"), "x\n")
	dirty, err := repo.Dirty(ctx, wt)
	if err != nil || len(dirty) != 0 {
		t.Fatalf("dirty = %v, err %v", dirty, err)
	}
	if err := repo.RemoveWorktree(ctx, wt, false); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

// TestGit_ChildHasNoJiraCredentials: a station controls its worktree's git
// config, so a core.fsmonitor it sets runs inside crew's own git status. That
// child must not see the Jira credentials.
func TestGit_ChildHasNoJiraCredentials(t *testing.T) {
	repo, _ := newRepo(t)
	t.Setenv("CREW_JIRA_EMAIL", "someone@example.com")
	t.Setenv("CREW_JIRA_TOKEN", "secret-token")
	dump := filepath.Join(t.TempDir(), "env.txt")
	script := filepath.Join(t.TempDir(), "monitor.sh")
	write(t, script, "#!/bin/sh\nenv > '"+dump+"'\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo.Dir, "config", "core.fsmonitor", script)
	if _, err := repo.Dirty(ctx, repo.Dir); err != nil {
		t.Fatalf("dirty: %v", err)
	}
	b, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("fsmonitor did not run: %v", err)
	}
	if env := string(b); strings.Contains(env, "CREW_JIRA_EMAIL") || strings.Contains(env, "CREW_JIRA_TOKEN") {
		t.Fatalf("git's child saw the Jira credentials:\n%s", env)
	}
}
