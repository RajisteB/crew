package main

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func TestCLI_StatusOfAnOwnedRunPointsToTheMission(t *testing.T) {
	for _, status := range []run.Status{run.StatusInterrupted, run.StatusFailed} {
		e, id, store := ownedRun(t)
		st, err := store.LoadState()
		if err != nil {
			t.Fatal(err)
		}
		st.Status, st.Retryable, st.Pending = status, true, nil
		if err := store.SaveState(st); err != nil {
			t.Fatal(err)
		}
		out, code := e.crew("run", "status", id)
		if code != 0 || strings.Contains(out, "resumable:") || strings.Contains(out, "retryable:") {
			t.Errorf("%s: exit %d, want no resume hint:\n%s", status, code, out)
		}
		if !strings.Contains(out, "\ndriven by its mission: use crew mission status\n") {
			t.Errorf("%s: no pointer to the mission:\n%s", status, out)
		}
	}
}

func TestCLI_StatusOfAPlainRunStillSuggestsResume(t *testing.T) {
	e, id, store := runAtItsStop(t)
	st, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Status, st.Pending = run.StatusInterrupted, nil
	if err := store.SaveState(st); err != nil {
		t.Fatal(err)
	}
	out, code := e.crew("run", "status", id)
	if code != 0 || !strings.Contains(out, "resumable: crew run resume "+id) || strings.Contains(out, "driven by its mission") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}
