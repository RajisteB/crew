package mission

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func TestLocalRuns_StartMarksTheRunAsTheMissions(t *testing.T) {
	e := newLocal(t)
	id, err := e.l.Start(Piece{ID: "P1", Factory: "tiny"}, "do it", e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	store, err := run.OpenRun(e.repo, id)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if want := "mission m-1 piece P1"; st.Owner != want {
		t.Fatalf("Owner = %q, want %q", st.Owner, want)
	}
}

func TestLocalRuns_StartNeedsAMissionID(t *testing.T) {
	e := newLocal(t)
	e.l.MissionID = ""
	_, err := e.l.Start(Piece{ID: "P1", Factory: "tiny"}, "do it", e.worktree)
	if err == nil || err.Error() != "mission runs need a mission id" {
		t.Fatalf("err = %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(e.repo, ".crew", "runs"))
	if len(entries) != 0 {
		t.Fatalf("a run was created: %v", entries)
	}
}
