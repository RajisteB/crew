package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

func TestMissionResume_LiveEngineLeavesMissionFileAlone(t *testing.T) {
	r := newMissionRig(t)
	r.stdin = string(planFile("Build.", pathPiece("P1", "api/**"), pathPiece("P2", "api/**")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	id := missionIDRe.FindString(out)
	store := r.store(id)
	m, err := store.Mission()
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Pieces {
		m.Pieces[i].Apart = nil // as if an amendment the engine wrote had not been separated yet
	}
	if err := store.SaveMission(m); err != nil {
		t.Fatal(err)
	}
	r.edit(id, func(s *mission.State) { s.Status = mission.StatusStopped })
	if err := store.SaveApproved(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Dir, "mission.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := store.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = r.cmd("resume", id)
	if err == nil || !strings.Contains(err.Error(), "is being driven (pid ") {
		t.Fatalf("err = %v", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatalf("resume rewrote mission.md under a live engine:\n%s", after)
	}
}
