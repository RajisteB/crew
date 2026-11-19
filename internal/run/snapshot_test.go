package run_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"},
	} {
		gitIn(t, dir, args...)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "init")
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func snap(t *testing.T, project string, watch ...string) map[string]string {
	t.Helper()
	if len(watch) == 0 {
		watch = []string{"."}
	}
	s, err := run.Snapshot(project, watch)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSnapshot_StableWhenUnchanged(t *testing.T) {
	repo := gitRepo(t)
	if a, b := snap(t, repo), snap(t, repo); !reflect.DeepEqual(a, b) {
		t.Fatalf("snapshots differ: %v vs %v", a, b)
	}
}

func TestSnapshot_ChangesOnEdit(t *testing.T) {
	repo := gitRepo(t)
	before := snap(t, repo)
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644)
	if got := run.Drifted(before, snap(t, repo)); !reflect.DeepEqual(got, []string{"."}) {
		t.Fatalf("Drifted = %v", got)
	}
}

func TestSnapshot_ChangesOnUntrackedEdit(t *testing.T) {
	repo := gitRepo(t)
	os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644)
	before := snap(t, repo)
	os.WriteFile(filepath.Join(repo, "new.txt"), []byte("y"), 0o644)
	if len(run.Drifted(before, snap(t, repo))) != 1 {
		t.Fatal("editing an untracked file must count as drift")
	}
}

func TestSnapshot_ChangesOnCommit(t *testing.T) {
	repo := gitRepo(t)
	before := snap(t, repo)
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644)
	gitIn(t, repo, "commit", "-qam", "second")
	if len(run.Drifted(before, snap(t, repo))) != 1 {
		t.Fatal("a new commit must count as drift")
	}
}

func TestSnapshot_IgnoresCrewDir(t *testing.T) {
	repo := gitRepo(t)
	before := snap(t, repo)
	os.MkdirAll(filepath.Join(repo, ".crew", "runs", "x"), 0o755)
	os.WriteFile(filepath.Join(repo, ".crew", "runs", "x", "state.json"), []byte("{}"), 0o644)
	if got := run.Drifted(before, snap(t, repo)); len(got) != 0 {
		t.Fatalf("writes under .crew/ must not count as drift: %v", got)
	}
}

func TestSnapshot_MultipleRoots(t *testing.T) {
	code, docs := gitRepo(t), gitRepo(t)
	before := snap(t, code, ".", docs)
	os.WriteFile(filepath.Join(docs, "a.txt"), []byte("changed\n"), 0o644)
	got := run.Drifted(before, snap(t, code, ".", docs))
	if !reflect.DeepEqual(got, []string{docs}) {
		t.Fatalf("Drifted = %v, want [%s]", got, docs)
	}
}

func TestSnapshot_NotARepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	_, err := run.Snapshot(t.TempDir(), []string{"."})
	if err == nil || !strings.Contains(err.Error(), "is not a git repository (factory watch:)") {
		t.Fatalf("got %v", err)
	}
}

func TestDrifted(t *testing.T) {
	if got := run.Drifted(nil, map[string]string{".": "a"}); len(got) != 0 {
		t.Fatalf("empty prev must report nothing, got %v", got)
	}
	got := run.Drifted(map[string]string{"b": "1", "a": "1"}, map[string]string{"a": "2", "b": "2"})
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("got %v", got)
	}
}
