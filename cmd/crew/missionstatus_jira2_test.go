package main

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

func TestMissionStatus_NothingReachedTheBoard(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	id := r.plan()
	r.edit(id, func(s *mission.State) { s.Status = mission.StatusRunning })
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(out, "\n"); lines[1] != "Jira: nothing has reached the board yet" {
		t.Fatalf("line 2 = %q", lines[1])
	}
}

func TestMissionStatus_CancelledDeliverable(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	id := r.plan()
	r.trackedMission(id)
	r.edit(id, func(s *mission.State) {
		s.Status = mission.StatusRunning
		s.Pieces[0].Status = mission.PieceCancelled
		s.Pieces[0].Deliverable = &mission.Deliverable{Files: []string{"brief.md"}}
	})
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\nDeliverables:\n  P1 (ACME-2)  brief.md  cancelled  .crew/missions/" + id + "/deliverables/P1\n"; !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}
