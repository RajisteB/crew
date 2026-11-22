package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissionPlan_PrintsRunsAtOnce(t *testing.T) {
	r := newMissionRig(t)
	r.stdin = string(planFile("Build the thing.", designPiece("P1"), designPiece("P2")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatal(err)
	}
	auto := strings.Index(out, "Auto-merge in acme/app:")
	line := strings.Index(out, "Runs at once: 3 across this repo's missions (limits.runs_at_once in missions.yaml)\n")
	if auto < 0 || line < auto || strings.Index(out, "```mermaid") < line {
		t.Fatalf("runs-at-once line missing or misplaced:\n%s", out)
	}
}

func TestMissionPlan_RunsAtOnceStarIsNoLimit(t *testing.T) {
	r := newMissionRig(t)
	p := filepath.Join(r.crewDir, "missions.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	b = []byte(strings.Replace(string(b), "runs_at_once: 3 ", `runs_at_once: "*"`, 1))
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	r.stdin = string(planFile("Build the thing.", designPiece("P1")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Runs at once: no limit across this repo's missions") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestMissionStart_WaitingForGoPrintsRunsAtOnce(t *testing.T) {
	r := newMissionRig(t)
	r.setApproval("on")
	id := r.plan()
	out, err := r.cmd("start", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Runs at once: 3 across this repo's missions") {
		t.Fatalf("got:\n%s", out)
	}
}
