package main

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

// amendRig is the delegated rig with P2 waiting for P1 and P1.1 running.
func amendRig(t *testing.T) *e2eRig {
	t.Helper()
	orch := &delegateOrch{}
	orch.set(twoSubPieces, escalateAll)
	r := delegatedRig(t, orch, true)
	r.tickUntil("P1.1 running", func(s mission.State) bool { p, ok := s.Piece("P1.1"); return ok && p.Status == mission.PieceRunning })
	return r
}

// An amendment written from the plan as it was before P1 was split keeps P1's
// sub-pieces, and the command says so before it queues.
func TestMissionAmend_FromThePlanBeforeTheSplitKeepsTheSubPieces(t *testing.T) {
	r := amendRig(t)
	p1 := mission.Piece{ID: "P1", Domain: "design", Delegate: true, Brief: "Do the brand.", Paths: []string{"brand/**"}}
	p2 := touching("P2", ".", nil, "P1")
	p2.Brief += " Use the $15 credit."
	out := r.cmd(string(planFile("Build the thing.", p1, p2)), "amend", r.id, "--words", "make it $15")
	want := "will re-brief P2\nkeeps P1.1, P1.2 under P1: the plan named no piece under it\nqueued; "
	if !strings.HasPrefix(out, want) {
		t.Fatalf("amend printed:\n%s", out)
	}
	r.tick()
	st := r.tick()
	for id, want := range map[string]mission.PieceStatus{"P1": mission.PieceDelegated, "P2": mission.PieceQueued, "P1.2": mission.PieceQueued} {
		if got := piece(t, st, id).Status; got != want {
			t.Errorf("%s = %s, want %s", id, got, want)
		}
	}
	if got := piece(t, st, "P1.1").Status; got == mission.PieceCancelled {
		t.Errorf("P1.1 was cancelled")
	}
}

// An amendment that names P1.1 and leaves out P1.2 cancels P1.2, and says so.
func TestMissionAmend_LeavingOutOneSubPieceSaysItWillCancelIt(t *testing.T) {
	r := amendRig(t)
	m, err := r.store.Mission()
	if err != nil {
		t.Fatal(err)
	}
	var keep []mission.Piece
	for _, p := range m.Pieces {
		if p.ID != "P1.2" {
			keep = append(keep, p)
		}
	}
	out := r.cmd(string(planFile(m.Request, keep...)), "amend", r.id, "--words", "no second step")
	if !strings.HasPrefix(out, "will cancel P1.2 (queued)\nqueued; ") {
		t.Fatalf("amend printed:\n%s", out)
	}
	st := r.tick()
	if got := piece(t, st, "P1.2").Status; got != mission.PieceCancelled {
		t.Fatalf("P1.2 = %s", got)
	}
}
