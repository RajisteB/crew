package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

func TestMissionStop_AwaitingGoIsRefused(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	if _, err := r.cmd("start", id); err != nil {
		t.Fatal(err)
	}
	_, err := r.cmd("stop", id)
	if err == nil || !strings.Contains(err.Error(), "hasn't started; nothing to stop") {
		t.Fatalf("err = %v", err)
	}
	if r.status(id) != mission.StatusAwaitingGo {
		t.Fatalf("status = %s", r.status(id))
	}
}

func TestMissionResume_NeverApprovedIsRefused(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.edit(id, func(s *mission.State) { s.Status = mission.StatusStopped })
	_, err := r.cmd("resume", id)
	if err == nil || !strings.Contains(err.Error(), "was never started; use crew mission go "+id) {
		t.Fatalf("err = %v", err)
	}
	if r.status(id) != mission.StatusStopped || len(r.starts) != 0 {
		t.Fatalf("status = %s, starts = %d", r.status(id), len(r.starts))
	}
}

func TestMissionGo_UnconfirmedRepoRefused(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	if _, err := r.cmd("start", id); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(r.crewDir, "principal")); err != nil {
		t.Fatal(err)
	}
	_, err := r.cmd("go", id)
	if err == nil || !strings.Contains(err.Error(), "crew repo confirm") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(r.store(id).Dir, "approved.md")); statErr == nil {
		t.Fatal("approved.md must not exist")
	}
	if r.status(id) != mission.StatusAwaitingGo || len(r.starts) != 0 {
		t.Fatalf("status = %s, starts = %d", r.status(id), len(r.starts))
	}
}

func TestMissionGo_SecondGoIsRefused(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	if _, err := r.cmd("start", id); err != nil {
		t.Fatal(err)
	}
	if _, err := r.cmd("go", id); err != nil {
		t.Fatal(err)
	}
	if _, err := r.cmd("go", id); err == nil {
		t.Fatal("a second go should be refused")
	}
	if len(r.starts) != 1 {
		t.Fatalf("starts = %d, want 1", len(r.starts))
	}
}

// TestMissionGo_StatusChangedDuringChecksIsRefused: another go finishes
// while this one is in its slow checks; the re-check under the lock refuses.
func TestMissionGo_StatusChangedDuringChecksIsRefused(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	if _, err := r.cmd("start", id); err != nil {
		t.Fatal(err)
	}
	r.onGH = func() { r.edit(id, func(s *mission.State) { s.Status = mission.StatusRunning }) }
	if _, err := r.cmd("go", id); err == nil {
		t.Fatal("go should be refused when the status changed during the checks")
	}
	if len(r.starts) != 0 {
		t.Fatalf("starts = %d, want 0", len(r.starts))
	}
	if _, err := os.Stat(filepath.Join(r.store(id).Dir, "approved.md")); err == nil {
		t.Fatal("approved.md must not be written by the refused go")
	}
}

func TestMissionStart_StatusChangedDuringChecksIsRefused(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	before := len(r.eventKinds(id))
	r.onGH = func() { r.edit(id, func(s *mission.State) { s.Status = mission.StatusAwaitingGo }) }
	if _, err := r.cmd("start", id); err == nil {
		t.Fatal("start should be refused when the status changed during the checks")
	}
	if r.status(id) != mission.StatusAwaitingGo || len(r.starts) != 0 {
		t.Fatalf("status = %s, starts = %d", r.status(id), len(r.starts))
	}
	if after := len(r.eventKinds(id)); after != before {
		t.Fatalf("events grew from %d to %d", before, after)
	}
}
