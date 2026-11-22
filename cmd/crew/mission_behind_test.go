package main

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

func TestMissionPlan_PrintsOtherMissionsOverlap(t *testing.T) {
	r := newMissionRig(t)
	r.stdin = string(planFile("Build.", pathPiece("P1", "api/**")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatalf("first plan: %v\n%s", err, out)
	}
	first := missionIDRe.FindString(out)
	r.edit(first, func(s *mission.State) {
		s.Status = mission.StatusRunning
		s.Pieces[0].Status = mission.PieceRunning
	})
	r.stdin = string(planFile("Build more.", pathPiece("P1", "api/v2/**"), pathPiece("P2", "docs/**")))
	out, err = r.cmd("plan")
	if err != nil {
		t.Fatalf("second plan: %v\n%s", err, out)
	}
	want := "P1 starts after P1 of mission " + first + ": their paths overlap (api/**, api/v2/**)"
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q:\n%s", want, out)
	}
	if strings.Contains(out, "P2 starts after") {
		t.Errorf("P2 does not overlap but got a line:\n%s", out)
	}
}

func TestStartChecks_PrintsOtherMissionsOverlap(t *testing.T) {
	r := newMissionRig(t)
	r.stdin = string(planFile("Build.", pathPiece("P1", "api/**")))
	out, _ := r.cmd("plan")
	first := missionIDRe.FindString(out)
	r.edit(first, func(s *mission.State) {
		s.Status = mission.StatusStopped
		s.Pieces[0].Status = mission.PiecePROpen
	})
	r.stdin = string(planFile("Build more.", pathPiece("P1", "api/v2/**")))
	out, _ = r.cmd("plan")
	second := missionIDRe.FindString(out)
	out, err := r.cmd("start", second)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	want := "P1 starts after P1 of mission " + first + ", which is stopped: their paths overlap"
	if !strings.Contains(out, want) {
		t.Errorf("start output missing %q:\n%s", want, out)
	}
}

func TestMissionStatus_QueuedPieceBehindAnotherMission(t *testing.T) {
	m := mission.Mission{Pieces: []mission.Piece{pathPiece("P1")}}
	b := mission.Blocker{Mission: "20261122-ab12", Piece: "P7", PathA: "api/**", PathB: "api/v2/**"}
	st := mission.State{Pieces: []mission.PieceState{{ID: "P1", Status: mission.PieceQueued, Behind: &b}}}
	if got, want := waitsText(m, st, "P1"), "starts after P7 of mission 20261122-ab12 (paths overlap)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	b.Stopped = true
	if got, want := waitsText(m, st, "P1"), "starts after P7 of mission 20261122-ab12, which is stopped (paths overlap)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
