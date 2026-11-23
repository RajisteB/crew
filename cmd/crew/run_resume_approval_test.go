package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

// stepFolders lists the step folders of a run.
func stepFolders(t *testing.T, store run.Store) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(store.Dir, "steps"))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// resume never passes an approval stop: only approve or goto does.
func TestCLI_ResumeDoesNotPassAnApprovalStop(t *testing.T) {
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"))
	out, code := e.crew("run", "ship-change", "--input", "x")
	if code != 0 || !strings.Contains(out, "waiting for approval") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	id := runID(t, out)
	store, err := run.OpenRun(e.project, id)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != run.StatusAwaitingApproval {
		t.Fatalf("status = %q before resume, want awaiting-approval", before.Status)
	}
	stepsBefore := stepFolders(t, store)

	if out, code := e.crew("run", "resume", id); code != 0 {
		t.Fatalf("resume exit %d:\n%s", code, out)
	}

	after, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != run.StatusAwaitingApproval || after.Step != before.Step {
		t.Errorf("after resume: status %q step %d, want awaiting-approval step %d", after.Status, after.Step, before.Step)
	}
	if got := stepFolders(t, store); strings.Join(got, ",") != strings.Join(stepsBefore, ",") {
		t.Errorf("step folders changed: %v, was %v", got, stepsBefore)
	}
}
