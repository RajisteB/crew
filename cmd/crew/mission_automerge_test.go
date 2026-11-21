package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/principal"
)

func TestMissionPlan_PrintsAutoMergeLine(t *testing.T) {
	r := newMissionRig(t)
	r.stdin = string(planFile("Build the thing.", designPiece("P1"), designPiece("P2", "P1")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatal(err)
	}
	table := strings.Index(out, "P2  ")
	line := strings.Index(out, "Auto-merge in acme/app: design off\n")
	if table < 0 || line < table || strings.Count(out, "Auto-merge in") != 1 {
		t.Fatalf("auto-merge line missing or misplaced:\n%s", out)
	}
	if mer := strings.Index(out, "```mermaid"); mer < line {
		t.Fatalf("line must come before the diagram:\n%s", out)
	}
}

func TestMissionPlan_AutoMergeLineShowsTheSwitch(t *testing.T) {
	r := newMissionRig(t)
	r.setSwitch("design", principal.AutoWithoutChecks)
	r.stdin = string(planFile("Build the thing.", designPiece("P1")))
	out, _ := r.cmd("plan")
	if !strings.Contains(out, "Auto-merge in acme/app: design without-checks\n") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestMissionStart_WaitingForGoPrintsAutoMergeLine(t *testing.T) {
	r := newMissionRig(t)
	r.setApproval("on")
	id := r.plan()
	out, err := r.cmd("start", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Auto-merge in acme/app: design off\n") || !strings.Contains(out, "waiting for go") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestAutoMergeLine_DomainsInPlanOrderAndUnreadable(t *testing.T) {
	got := autoMergeLine("acme/app", []string{"b", "a", "b"}, nil, errors.New("boom"))
	if want := "Auto-merge in acme/app: b off (switch unreadable), a off (switch unreadable)"; got != want {
		t.Fatalf("got %q", got)
	}
	recs := map[string]principal.SwitchRecord{"a": {Mode: principal.AutoOn}}
	got = autoMergeLine("acme/app", []string{"b", "a"}, recs, nil)
	if want := "Auto-merge in acme/app: b off, a on"; got != want {
		t.Fatalf("got %q", got)
	}
}
