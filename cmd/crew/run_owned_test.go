package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

const testOwner = "mission m-1 piece P1"

// ownedRun drives a plain run of the approval fixture to its stop, then marks
// it as owned by a mission.
func ownedRun(t *testing.T) (*cliEnv, string, run.Store) {
	t.Helper()
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
	st, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Owner = testOwner
	if err := store.SaveState(st); err != nil {
		t.Fatal(err)
	}
	return e, id, store
}

func TestCLI_OwnedRunRefusesSayGotoApproveResume(t *testing.T) {
	e, id, store := ownedRun(t)
	statePath := filepath.Join(store.Dir, "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"say":     {"run", "say", id, "hi"},
		"goto":    {"run", "goto", id, "review"},
		"approve": {"run", "approve", id},
		"resume":  {"run", "resume", id},
	}
	for verb, args := range cases {
		out, code := e.crew(args...)
		if code != 1 {
			t.Errorf("%s: exit %d, want 1:\n%s", verb, code, out)
		}
		for _, want := range []string{"run " + id + " belongs to " + testOwner, "crew run " + verb + " is refused", "use crew mission status, answer, amend, stop or resume"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: output lacks %q:\n%s", verb, want, out)
			}
		}
		after, _ := os.ReadFile(statePath)
		if !bytes.Equal(before, after) {
			t.Errorf("%s changed state.json", verb)
		}
		if _, err := os.Stat(filepath.Join(store.Dir, "inbox.jsonl")); err == nil {
			t.Errorf("%s wrote to the inbox", verb)
		}
	}
}

func TestCLI_StatusShowsTheOwner(t *testing.T) {
	e, id, _ := ownedRun(t)
	out, code := e.crew("run", "status", id)
	lines := strings.Split(out, "\n")
	if code != 0 || len(lines) < 2 || lines[1] != "owner: "+testOwner {
		t.Fatalf("exit %d, want an owner line right after the first:\n%s", code, out)
	}
	list, code := e.crew("run", "status")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, list)
	}
	found := false
	for _, l := range strings.Split(list, "\n") {
		if strings.HasPrefix(l, id) {
			found = true
			if !strings.HasSuffix(l, "  "+testOwner) {
				t.Errorf("list line does not end with the owner: %q", l)
			}
		}
	}
	if !found {
		t.Fatalf("run missing from the list:\n%s", list)
	}
}

func TestCLI_PlainRunHasNoOwnerLine(t *testing.T) {
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"))
	out, _ := e.crew("run", "ship-change", "--input", "x")
	id := runID(t, out)
	status, _ := e.crew("run", "status", id)
	list, _ := e.crew("run", "status")
	if strings.Contains(status, "owner:") || strings.Contains(list, "mission") {
		t.Fatalf("a plain run shows an owner:\n%s\n%s", status, list)
	}
}
