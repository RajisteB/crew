package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/principal"
)

const testPRURL = "https://github.com/acme/app/pull/7"

func (r *missionRig) setSwitch(domain string, mode principal.AutoMode) {
	r.t.Helper()
	store := principal.Store{Dir: filepath.Join(r.crewDir, "principal")}
	rec := principal.SwitchRecord{Repo: "acme/app", Domain: domain, Mode: mode, SetAt: time.Now()}
	if err := store.SetSwitches([]principal.SwitchRecord{rec}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *missionRig) openPR(id string, wait *mission.Wait) {
	r.t.Helper()
	r.edit(id, func(s *mission.State) {
		s.Status = mission.StatusRunning
		s.Pieces[0].Status = mission.PiecePROpen
		s.Pieces[0].PR, s.Pieces[0].PRURL, s.Pieces[0].Wait = 7, testPRURL, wait
	})
}

func TestStatus_OpenPullRequestsSection(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.setSwitch("design", principal.AutoOn)
	r.openPR(id, nil)
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	want := "\nOpen pull requests:\n  P1  PR #7  " + testPRURL + "  auto-merge on\n"
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

func TestStatus_OpenPullRequestsSitsAfterWaiting(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.openPR(id, nil)
	r.edit(id, func(s *mission.State) {
		s.Pieces[1].Status = mission.PieceWaiting
		s.Pieces[1].Wait = &mission.Wait{ID: "w1", Kind: mission.WaitQuestion, Question: "which?", Since: time.Now()}
	})
	out, _ := r.cmd("status", id)
	w, o, run := strings.Index(out, "Waiting on you:"), strings.Index(out, "Open pull requests:"), strings.Index(out, "Running:")
	if w < 0 || o < 0 || !(w < o) || (run >= 0 && run < o) {
		t.Fatalf("section order wrong:\n%s", out)
	}
	if !strings.Contains(out, "auto-merge off") {
		t.Fatalf("a missing switch is off:\n%s", out)
	}
}

func TestStatus_PieceWithAWaitIsNotListedAsOpen(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.openPR(id, &mission.Wait{ID: "w1", Kind: mission.WaitMerge, Question: "PR #7 waits", Since: time.Now()})
	out, _ := r.cmd("status", id)
	if strings.Contains(out, "Open pull requests") || strings.Contains(out, "PR open") {
		t.Fatalf("a pr-open piece with a wait belongs under Waiting on you only:\n%s", out)
	}
	if !strings.Contains(out, "Waiting on you:") {
		t.Fatalf("no waiting section:\n%s", out)
	}
}

func TestStatus_UnreadableSwitch(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.setSwitch("design", principal.AutoOn)
	if err := os.WriteFile(filepath.Join(r.crewDir, "principal", "automerge.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.openPR(id, nil)
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "  P1  PR #7  "+testPRURL+"  auto-merge off (switch unreadable)\n") {
		t.Fatalf("unreadable switch not shown:\n%s", out)
	}
}
