package main

import (
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/mission"
)

// delegatedMission plans P1 (delegated, paths brand/**) and P2 (waits for P1)
// and returns the mission id.
func (r *missionRig) delegatedMission() string {
	r.t.Helper()
	p1 := mission.Piece{ID: "P1", Domain: "design", Delegate: true, Brief: "Do the brand.", Paths: []string{"brand/**"}}
	r.stdin = string(planFile("Build the thing.", p1, designPiece("P2", "P1")))
	out, err := r.cmd("plan")
	if err != nil {
		r.t.Fatalf("plan: %v\n%s", err, out)
	}
	id := missionIDRe.FindString(out)
	if id == "" {
		r.t.Fatalf("no mission id in:\n%s", out)
	}
	return id
}

func TestMissionPlan_PrintsDelegatedPieces(t *testing.T) {
	r := newMissionRig(t)
	p1 := mission.Piece{ID: "P1", Domain: "design", Delegate: true, Brief: "Do the brand.", Paths: []string{"brand/**"}}
	p3 := mission.Piece{ID: "P3", Domain: "design", Delegate: true, Brief: "Do the rest."}
	r.stdin = string(planFile("Build the thing.", p1, designPiece("P2", "P1"), p3))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	for _, want := range []string{
		"(delegated)",
		"P1 is delegated: the design orchestrator splits it once it is ready. Its pieces stay in design, use design's factories and keep to brand/**.\n",
		"P3 is delegated: the design orchestrator splits it once it is ready. Its pieces stay in design, use design's factories and are not held to any paths.\n",
		`P1["P1 · design · delegated"]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "P2 is delegated") {
		t.Errorf("P2 is not delegated:\n%s", out)
	}
}

func TestMissionStatus_DelegatedSection(t *testing.T) {
	r := newMissionRig(t)
	id := r.delegatedMission()
	store := r.store(id)
	m, err := store.Mission()
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"P1.1", "P1.2", "P1.3"} {
		m.Pieces = append(m.Pieces, mission.Piece{ID: sub, Domain: "design", Factory: "ship-change", Brief: "x", Paths: []string{"brand/**"}})
	}
	if err := store.SaveMission(m); err != nil {
		t.Fatal(err)
	}
	r.edit(id, func(s *mission.State) {
		s.Status = mission.StatusRunning
		s.Pieces[0].Status = mission.PieceDelegated
		s.Pieces = append(s.Pieces,
			mission.PieceState{ID: "P1.1", Status: mission.PieceComplete},
			mission.PieceState{ID: "P1.2", Status: mission.PieceRunning, RunID: "none"},
			mission.PieceState{ID: "P1.3", Status: mission.PieceQueued})
	})
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	running := strings.Index(out, "\nRunning:\n")
	delegated := strings.Index(out, "\nDelegated:\n  P1  3 pieces: 1 complete, 1 running, 1 queued\n")
	queued := strings.Index(out, "\nQueued:\n")
	if running < 0 || delegated < running || queued < delegated {
		t.Errorf("want Running, then Delegated with the counts, then Queued:\n%s", out)
	}
	if strings.Contains(out, "  P1  ready to start") || strings.Contains(out, "  P1  to be planned") {
		t.Errorf("a delegated piece is not queued:\n%s", out)
	}
}

func TestMissionStatus_QueuedDelegatedPieceAndPlanWait(t *testing.T) {
	r := newMissionRig(t)
	id := r.delegatedMission()
	r.edit(id, func(s *mission.State) { s.Status = mission.StatusRunning })
	out, _ := r.cmd("status", id)
	if want := "\n  P1  to be planned\n"; !strings.Contains(out, want) {
		t.Errorf("status missing %q:\n%s", want, out)
	}
	if !strings.Contains(out, "\n  P2  waits for P1\n") {
		t.Errorf("P2 keeps its waits text:\n%s", out)
	}
	r.edit(id, func(s *mission.State) {
		s.Hold = &mission.Hold{Kind: "steps", Count: 6, Max: "6", Since: time.Now()}
	})
	if out, _ = r.cmd("status", id); !strings.Contains(out, "\n  P1  on hold\n") {
		t.Errorf("a held delegated piece says on hold:\n%s", out)
	}
	r.edit(id, func(s *mission.State) {
		s.Hold = nil
		s.Pieces[0].Wait = &mission.Wait{ID: "w1", Kind: mission.WaitPlan, Question: "No design can be made.", Since: time.Now()}
	})
	out, _ = r.cmd("status", id)
	if want := "  w1  P1  plan  No design can be made.  waiting "; !strings.Contains(out, want) {
		t.Errorf("status missing %q:\n%s", want, out)
	}
}
