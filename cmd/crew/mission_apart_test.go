package main

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

func pathPiece(id string, paths ...string) mission.Piece {
	p := designPiece(id)
	p.Paths = paths
	return p
}

func crewChanges(m mission.Mission) int {
	n := 0
	for _, c := range m.Changes {
		if c.By == "crew" {
			n++
		}
	}
	return n
}

const apartLine = "P3 starts after P1: their paths overlap (P1: api/**, P3: api/v2/**)"

func TestMissionPlan_PrintsSeparations(t *testing.T) {
	r := newMissionRig(t)
	r.stdin = string(planFile("Build.", pathPiece("P1", "api/**"), pathPiece("P2", "web/**"), pathPiece("P3", "api/v2/**")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	if !strings.Contains(out, apartLine) {
		t.Errorf("output missing %q:\n%s", apartLine, out)
	}
	m, err := r.store(missionIDRe.FindString(out)).Mission()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Pieces[2].Apart; len(got) != 1 || got[0] != "P1" || !strings.Contains(string(m.Render()), "apart_from") {
		t.Errorf("apart = %v", got)
	}
	if crewChanges(m) != 1 {
		t.Errorf("changes = %+v", m.Changes)
	}
}

func TestStartChecks_SeparatesAgainOnlyWhenItChanged(t *testing.T) {
	r := newMissionRig(t)
	r.stdin = string(planFile("Build.", pathPiece("P1", "api/**"), pathPiece("P3", "api/v2/**")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatal(err)
	}
	id := missionIDRe.FindString(out)
	store := r.store(id)
	m, _ := store.Mission()
	m.Pieces[1].Apart = nil // as a mission planned before crew kept pieces apart
	m.Changes = nil
	if err := store.SaveMission(m); err != nil {
		t.Fatal(err)
	}
	out, err = r.cmd("start", id)
	if err != nil || !strings.Contains(out, "P3 starts after P1") {
		t.Fatalf("start: %v\n%s", err, out)
	}
	m, _ = store.Mission()
	if crewChanges(m) != 1 || len(m.Pieces[1].Apart) != 1 {
		t.Fatalf("after first start: %+v", m.Changes)
	}
	r.edit(id, func(s *mission.State) { s.Status = mission.StatusPlanned })
	out, err = r.cmd("start", id)
	if err != nil || strings.Contains(out, "starts after") {
		t.Fatalf("second start: %v\n%s", err, out)
	}
	if m, _ = store.Mission(); crewChanges(m) != 1 {
		t.Fatalf("after second start: %+v", m.Changes)
	}
}

func TestWaitsText_ShowsPiecesKeptApart(t *testing.T) {
	m := mission.Mission{Pieces: []mission.Piece{pathPiece("P1"), pathPiece("P2")}}
	m.Pieces[1].Apart = []string{"P1"}
	st := mission.State{Pieces: []mission.PieceState{{ID: "P1", Status: mission.PieceRunning}, {ID: "P2", Status: mission.PieceQueued}}}
	if got := waitsText(m, st, "P2"); got != "starts after P1 (paths overlap)" {
		t.Fatalf("got %q", got)
	}
	st.Pieces[0].Status = mission.PieceComplete
	if got := waitsText(m, st, "P2"); got != "ready to start" {
		t.Fatalf("got %q", got)
	}
}
