package main

import (
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/mission"
)

// trackedMission gives every piece an issue and puts the mission in sync with
// the board, so nothing is owed.
func (r *jiraRig) trackedMission(id string) {
	r.t.Helper()
	evs, err := r.store(id).EventsSince(0)
	if err != nil {
		r.t.Fatal(err)
	}
	r.edit(id, func(s *mission.State) {
		s.Jira = mission.TrackerState{Epic: "ACME-1", EpicLane: mission.LaneToDo}
		if n := len(evs); n > 0 {
			s.Jira.Seq = evs[n-1].Seq
		}
		for i := range s.Pieces {
			s.Pieces[i].Jira = mission.Tracked{Issue: "ACME-" + string(rune('2'+i)), Lane: mission.LaneToDo}
		}
	})
}

func TestMissionStatus_JiraLine(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	id := r.plan()
	out, _ := r.cmd("status", id)
	if strings.Contains(out, "\nJira:") {
		t.Fatalf("a mission without an epic has no Jira line:\n%s", out)
	}
	r.trackedMission(id)
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if want := "Jira: ACME-1  " + testSite + "/browse/ACME-1"; lines[1] != want {
		t.Fatalf("line 2 = %q, want %q", lines[1], want)
	}
	if n := len(r.srv.Requests()); n != 0 {
		t.Fatalf("status made %d requests", n)
	}
	r.edit(id, func(s *mission.State) { s.Pieces[0].Jira.Lane = "" })
	out, _ = r.cmd("status", id)
	if !strings.Contains(out, "/browse/ACME-1 (1 write not sent yet)\n") {
		t.Fatalf("no not-sent count:\n%s", out)
	}
}

func TestMissionStatus_JiraLineWithoutAnEntry(t *testing.T) {
	r := newJiraRig(t)
	id := r.plan()
	r.trackedMission(id)
	out, _ := r.cmd("status", id)
	if lines := strings.Split(out, "\n"); lines[1] != "Jira: ACME-1" {
		t.Fatalf("line 2 = %q", lines[1])
	}
}

func TestMissionStatus_Deliverables(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	id := r.plan()
	r.trackedMission(id)
	r.edit(id, func(s *mission.State) {
		s.Status = mission.StatusRunning
		s.Pieces[0].Status = mission.PieceDelivered
		s.Pieces[0].Deliverable = &mission.Deliverable{Files: []string{"brief.md", "plan.md"}, Posted: time.Now()}
		s.Pieces[0].Wait = &mission.Wait{ID: "w1", Kind: mission.WaitDeliverable, Question: "P1 left a deliverable: brief.md, plan.md", Since: time.Now()}
		s.Pieces[1].Status = mission.PieceComplete
		s.Pieces[1].Deliverable = &mission.Deliverable{Files: []string{"research.md"}, Accepted: true, By: "engine"}
	})
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"\nWaiting on you:\n  w1  P1  deliverable  P1 left a deliverable: brief.md, plan.md  waiting ",
		"\nDeliverables:\n  P1 (ACME-2)  brief.md, plan.md  waiting on you  .crew/missions/" + id + "/deliverables/P1\n",
		"  P2 (ACME-3)  research.md  accepted by engine  .crew/missions/" + id + "/deliverables/P2\n",
		"\nComplete:\n  P2 (ACME-3)\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	for _, section := range []string{"Running", "Queued"} {
		if strings.Contains(out, "\n"+section+":") {
			t.Fatalf("a delivered piece is under %s:\n%s", section, out)
		}
	}
	if strings.Contains(out, "Complete:\n  P1") {
		t.Fatalf("a delivered piece is under Complete:\n%s", out)
	}
}

func TestMissionStatus_PRLineCarriesTheIssueKey(t *testing.T) {
	r := newJiraRig(t)
	id := r.plan()
	r.trackedMission(id)
	r.openPR(id, nil)
	out, _ := r.cmd("status", id)
	if !strings.Contains(out, "  P1 (ACME-2)  PR #7  ") {
		t.Fatalf("no key on the pull request line:\n%s", out)
	}
}

func TestWritesText(t *testing.T) {
	for n, want := range map[int]string{1: "1 write", 2: "2 writes", 10: "10 writes"} {
		if got := writesText(n); got != want {
			t.Errorf("writesText(%d) = %q, want %q", n, got, want)
		}
	}
}
