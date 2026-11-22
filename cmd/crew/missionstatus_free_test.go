package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/mission"
)

func (r *missionRig) setRunsAtOnce(v string) {
	r.t.Helper()
	p := filepath.Join(r.crewDir, "missions.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		r.t.Fatal(err)
	}
	b = bytes.Replace(b, []byte("runs_at_once: 3"), []byte("runs_at_once: "+v), 1)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func TestMissionStatus_WaitingForAFreeRun(t *testing.T) {
	r := newMissionRig(t)
	r.setRunsAtOnce("1")
	other := r.plan()
	r.edit(other, func(s *mission.State) {
		s.Status = mission.StatusRunning
		s.Pieces[0].Status = mission.PieceRunning
	})
	unlock, err := r.store(other).Lock() // a live engine drives it
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	id := r.plan()
	r.edit(id, func(s *mission.State) { s.Status = mission.StatusRunning })
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\n  P1  waiting for a free run: 1 of 1 in use across this repo's missions\n"; !strings.Contains(out, want) {
		t.Errorf("status missing %q:\n%s", want, out)
	}
	unlock()
	if out, _ = r.cmd("status", id); !strings.Contains(out, "\n  P1  ready to start\n") {
		t.Errorf("with no live engine elsewhere, P1 is ready:\n%s", out)
	}
	r.edit(id, func(s *mission.State) {
		s.Hold = &mission.Hold{Kind: "steps", Count: 6, Max: "6", Since: time.Now()}
	})
	if out, _ = r.cmd("status", id); !strings.Contains(out, "\n  P1  on hold\n") {
		t.Errorf("a mission on hold says so for a ready piece:\n%s", out)
	}
}
