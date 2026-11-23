package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

// runAtItsStop starts a plain run of the approval fixture and leaves it at its stop.
func runAtItsStop(t *testing.T) (*cliEnv, string, run.Store) {
	t.Helper()
	e, id, _ := stoppedPlainRun(t)
	store, err := run.OpenRun(e.project, id)
	if err != nil {
		t.Fatal(err)
	}
	return e, id, store
}

func assertNoInbox(t *testing.T, store run.Store) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(store.Dir, "inbox.jsonl")); !os.IsNotExist(err) {
		t.Errorf("the inbox was written (%v)", err)
	}
}

func assertStillAwaitingApproval(t *testing.T, store run.Store) {
	t.Helper()
	st, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != run.StatusAwaitingApproval || st.Pending == nil {
		t.Errorf("status %s (pending %v), want awaiting-approval", st.Status, st.Pending != nil)
	}
}

func TestCLI_EmptyStdinSayIsRefused(t *testing.T) {
	for _, stdin := range []string{"", " \n\t\n"} {
		e, id, store := runAtItsStop(t)
		out, code := e.crewIn(stdin, "run", "say", id, "-")
		if code != 2 || !strings.Contains(out, "crew: say: nothing on stdin") {
			t.Errorf("stdin %q: exit %d, want 2 and the message:\n%s", stdin, code, out)
		}
		assertNoInbox(t, store)
		assertStillAwaitingApproval(t, store)
	}
}

func TestCLI_EmptyStdinNoteIsRefused(t *testing.T) {
	for _, stdin := range []string{"", " \n\t\n"} {
		e, id, store := runAtItsStop(t)
		out, code := e.crewIn(stdin, "run", "approve", id, "--note", "-")
		if code != 2 || !strings.Contains(out, "crew: --note: nothing on stdin") {
			t.Errorf("stdin %q: exit %d, want 2 and the message:\n%s", stdin, code, out)
		}
		assertNoInbox(t, store)
		assertStillAwaitingApproval(t, store)
	}
}

func TestCLI_StdinOverTheLimitIsRefused(t *testing.T) {
	big := strings.Repeat("x", maxStdinText+1)
	e, id, store := runAtItsStop(t)
	cases := map[string][]string{
		"say":   {"run", "say", id, "-"},
		"note":  {"run", "approve", id, "--note", "-"},
		"input": {"run", "ship-change", "--input", "-"},
	}
	for name, args := range cases {
		out, code := e.crewIn(big, args...)
		if code != 2 || !strings.Contains(out, "crew: stdin: more than 1 MB of text") {
			t.Errorf("%s: exit %d, want 2 and the message:\n%.300s", name, code, out)
		}
	}
	assertNoInbox(t, store)
	assertStillAwaitingApproval(t, store)
	if runs, err := run.ListRuns(e.project); err != nil || len(runs) != 1 {
		t.Errorf("runs = %d (%v), want only the first", len(runs), err)
	}
	if out, code := e.crewIn(strings.Repeat("x", maxStdinText), "run", "say", id, "-"); code != 0 {
		t.Errorf("exactly the limit: exit %d:\n%.300s", code, out)
	}
}
