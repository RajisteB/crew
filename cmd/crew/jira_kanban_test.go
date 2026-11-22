package main

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

const noSprintsLine = "  board: 7 has no sprints: issues are not put in a sprint\n"

func TestJiraSetup_KanbanBoardSavesBoardZero(t *testing.T) {
	r := newJiraRig(t)
	r.srv.AddBoardWithoutSprints(7)
	fakeTTY(t, "acme/app ACME\n")
	out, err := r.setup()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, noSprintsLine) || strings.Contains(out, "problem:") {
		t.Fatalf("out:\n%s", out)
	}
	if got, ok := r.entry(); !ok || got.Board != 0 {
		t.Fatalf("entry = %+v, %v", got, ok)
	}
}

func TestJiraCheck_BoardWithoutSprints(t *testing.T) {
	r := newJiraRig(t)
	r.srv.AddBoardWithoutSprints(7)
	r.saveEntry(7)
	out, err := r.jira("check")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, noSprintsLine) || strings.Contains(out, "problem:") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestJiraSync_RefusesAMissionThatHasNotStarted(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	for _, status := range []mission.Status{mission.StatusPlanned, mission.StatusAwaitingGo} {
		id := r.plan()
		r.edit(id, func(st *mission.State) { st.Status = status })
		_, err := r.jira("sync", id)
		if want := "mission " + id + " hasn't started: there is nothing to send"; err == nil || err.Error() != want {
			t.Fatalf("%s: err = %v", status, err)
		}
	}
	if n := len(r.srv.Requests()); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
}
