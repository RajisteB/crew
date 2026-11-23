package main

import (
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

// The Delegated section counts the pieces under a delegated piece from the
// state, as the engine does: a sub-piece an amendment dropped from mission.md
// is still counted.
func TestChildCounts_CountsFromTheState(t *testing.T) {
	st := mission.State{Pieces: []mission.PieceState{
		{ID: "P1", Status: mission.PieceDelegated},
		{ID: "P1.1", Status: mission.PieceCancelled}, // no longer in mission.md
		{ID: "P1.2", Status: mission.PieceRunning},
		{ID: "P3", Status: mission.PieceDelegated},
		{ID: "P3.1", Status: mission.PieceQueued},
	}}
	if got, want := childCounts(st, "P1"), "2 pieces: 1 running, 1 cancelled"; got != want {
		t.Errorf("P1: %q, want %q", got, want)
	}
	if got, want := childCounts(st, "P3"), "1 piece: 1 queued"; got != want {
		t.Errorf("P3: %q, want %q", got, want)
	}
}
