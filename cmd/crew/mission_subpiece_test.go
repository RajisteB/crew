package main

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/verify"
)

func TestMissionPlan_RefusesSubPieceIDs(t *testing.T) {
	r := newMissionRig(t)
	r.stdin = string(planFile("Build.", designPiece("P1"), designPiece("P1.1")))
	out, err := r.cmd("plan")
	if err == nil {
		t.Fatalf("want an error:\n%s", out)
	}
	want := `piece P1.1: an id with a "." names a sub-piece, and crew names those when it plans a delegated piece`
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q:\n%s", want, out)
	}
	if list, _ := mission.List(r.root()); len(list) != 0 {
		t.Fatalf("created %d missions", len(list))
	}
}

func TestNewChecks_SetsTheOrchestratorHarness(t *testing.T) {
	chk := newChecks(&verify.Project{}, mission.Settings{Harness: "claude-code"}, "acme/app")
	if chk.in.OrchestratorHarness != "claude-code" {
		t.Fatalf("OrchestratorHarness = %q", chk.in.OrchestratorHarness)
	}
}
