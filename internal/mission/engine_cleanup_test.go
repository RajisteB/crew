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
	h.undrive() // Loop takes the mission lock itself
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
	h.undrive() // Loop takes the mission lock itself
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
	h.undrive() // Loop takes the mission lock itself
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

func mergedCleanup(h *harness, merged bool) {
	h.runPiece("P1")
	h.edit(func(s *State) { s.Pieces[0].Status = PieceComplete })
	t := &tick{e: h.e, ctx: context.Background(), s: h.state(), notes: map[string]struct{}{}}
	t.cleanup(Cleanup{Piece: "P1", Force: true, Merged: merged})
}

func TestEngine_MergedCleanupDeletesBranchAfterWorktree(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	branch := "crew/" + h.id + "/P1"
	mergedCleanup(h, true)
	rm, del := h.log.indexes("RemoveWorktree"), h.log.indexes("DeleteBranch")
	if len(rm) != 1 || len(del) != 1 || rm[0] > del[0] || !h.log.has("DeleteBranch "+branch) {
		t.Fatalf("calls: %v", h.log.calls)
	}
}

func TestEngine_UnmergedCleanupKeepsBranch(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	mergedCleanup(h, false)
	if h.log.has("DeleteBranch") {
		t.Fatalf("calls: %v", h.log.calls)
	}
}

func TestEngine_BranchKeptWhenWorktreeRemovalFails(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.fail.failNext("RemoveWorktree", 1)
	mergedCleanup(h, true)
	if h.log.has("DeleteBranch") {
		t.Fatalf("calls: %v", h.log.calls)
	}
}

func TestEngine_BranchDeleteFailureIsOnlyANote(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.fail.failNext("DeleteBranch", 1)
	mergedCleanup(h, true)
	notes := h.events(EvNote)
	want := "can't delete the local branch crew/" + h.id + "/P1"
	if len(notes) != 1 || !strings.Contains(notes[0].Msg, want) {
		t.Fatalf("notes = %+v", notes)
	}
	if h.piece("P1").Worktree != "" {
		t.Fatal("piece not cleaned up")
	}
}
