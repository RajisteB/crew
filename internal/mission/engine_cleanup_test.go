package mission

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestEngine_CleanupFailureRetriedOnResume(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.tick()
	h.runs.setRun("run-P1", doneState(), false)
	h.tick()
	h.tick()
	h.gh.setState(1, "MERGED")
	h.fail.failNext("RemoveWorktree", 1)
	st := h.tick()
	if st.Pieces[0].Status != PieceComplete || st.Pieces[0].Worktree == "" {
		t.Fatalf("piece = %+v", st.Pieces[0])
	}
	wt := st.Pieces[0].Worktree
	before := h.log.count("RemoveWorktree")
	if _, err := h.e.Loop(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if h.log.count("RemoveWorktree") != before+1 || !h.log.has("RemoveWorktree "+wt+" force=true") {
		t.Fatalf("calls: %v", h.log.calls)
	}
	if got := h.piece("P1").Worktree; got != "" {
		t.Fatalf("worktree still recorded: %q", got)
	}
}

func TestEngine_CancelledPieceCleanupIsNotForced(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.runPiece("P1")
	h.edit(func(s *State) { s.Pieces[0].Status = PieceCancelled })
	wt := h.piece("P1").Worktree
	h.fail.failNext("RemoveWorktree", 1)
	if _, err := h.e.Loop(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !h.log.has("RemoveWorktree "+wt+" force=false") || h.log.has("RemoveWorktree "+wt+" force=true") {
		t.Fatalf("calls: %v", h.log.calls)
	}
	notes := h.events(EvNote)
	want := "P1: worktree left at " + wt + " (uncommitted work); remove it by hand when done"
	if len(notes) != 1 || !strings.Contains(notes[0].Msg, want) {
		t.Fatalf("notes = %+v", notes)
	}
	if h.piece("P1").Worktree != wt {
		t.Fatal("worktree cleared although removal failed")
	}
	if _, err := h.e.Loop(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if h.piece("P1").Worktree != "" {
		t.Fatal("second resume did not remove it")
	}
}

func TestEngine_StartClearsLeftoverWorktreeFirst(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.tick()
	rm, add := h.log.indexes("RemoveWorktree"), h.log.indexes("AddWorktree")
	if len(rm) != 1 || len(add) != 1 || rm[0] > add[0] || !strings.HasSuffix(h.log.calls[rm[0]], "force=true") {
		t.Fatalf("calls: %v", h.log.calls)
	}
}

func TestEngine_StartNotAttemptedWhenLeftoverCannotBeRemoved(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.fail.failNext("RemoveWorktree", 1)
	if p := h.tick().Pieces[0]; p.Status != PieceQueued {
		t.Fatalf("piece = %+v", p)
	}
	if h.log.has("AddWorktree") || len(h.events(EvNote)) != 1 {
		t.Fatalf("calls: %v", h.log.calls)
	}
	if p := h.tick().Pieces[0]; p.Status != PieceRunning {
		t.Fatalf("retry: %+v", p)
	}
}
