package mission

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func heldTick(t *testing.T, h *harness) *tick {
	t.Helper()
	tk, err := h.e.newTick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestPerform_RecordWaitsForSaveWhenStateUnsaved(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	tk := heldTick(t, h)
	tk.unsaved = true
	if err := tk.perform([]Action{Record{Event: Event{Kind: EvDecision, Msg: "held"}}}); err != nil {
		t.Fatal(err)
	}
	if got := h.kinds(); len(got) != 0 {
		t.Fatalf("events before save = %v", got)
	}
	if err := tk.save(); err != nil {
		t.Fatal(err)
	}
	if got := h.kinds(); !slices.Equal(got, []string{EvDecision}) {
		t.Fatalf("events after save = %v", got)
	}
	if tk.unsaved || len(tk.held) != 0 {
		t.Fatalf("unsaved %v held %v", tk.unsaved, tk.held)
	}
}

func TestPerform_RecordIsWrittenAtOnceWhenStateSaved(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	tk := heldTick(t, h)
	if err := tk.perform([]Action{Record{Event: Event{Kind: EvDecision}}}); err != nil {
		t.Fatal(err)
	}
	if got := h.kinds(); !slices.Equal(got, []string{EvDecision}) {
		t.Fatalf("events = %v", got)
	}
}

func TestPerform_HeldEventComesBeforeAnEffectsOwnEvent(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	tk := heldTick(t, h)
	tk.unsaved = true
	acts := []Action{
		Record{Event: Event{Kind: EvDecision}},
		StartPiece{Piece: "P1"},
	}
	if err := tk.perform(acts); err != nil {
		t.Fatal(err)
	}
	if got := h.kinds(); !slices.Equal(got, []string{EvDecision, EvPieceStart}) {
		t.Fatalf("events = %v", got)
	}
}

// The real symptom: while an effect is at work, the log must not describe a
// change the saved state doesn't show yet.
func TestEngine_DecidedEventsWaitForTheSavedState(t *testing.T) {
	h := newHarness(t)
	h.tick()
	h.runs.setRun("run-P1", doneState(), false)
	h.tick()
	h.tick()
	h.gh.setState(1, "MERGED")
	before := len(h.kinds())
	var checked bool
	h.git.onRemoveWorktree = func() {
		if checked {
			return // only the first call comes before the first save
		}
		checked = true
		if got := h.kinds(); len(got) != before {
			t.Errorf("events written before the save: %v", got[before:])
		}
		if p := h.piece("P1"); p.Status == PieceComplete {
			t.Errorf("state already saved: %+v", p)
		}
	}
	h.tick() // P1 completes and P2 starts: a Record, then a worktree removal
	if !checked {
		t.Fatal("the worktree hook never ran")
	}
	if got := h.kinds(); len(got) <= before {
		t.Fatalf("no events after the tick: %v", got)
	}
}

func TestPerform_NotificationAfterAHeldRecordIsHeldToo(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	tk := heldTick(t, h)
	tk.unsaved = true
	if err := tk.perform([]Action{
		Record{Event: Event{Kind: EvWaitingOnPrincipal}},
		Notify{Body: "p1 waits"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := h.kinds(); len(got) != 0 {
		t.Fatalf("events before save = %v", got)
	}
	if err := tk.save(); err != nil {
		t.Fatal(err)
	}
	if got := h.kinds(); !slices.Equal(got, []string{EvWaitingOnPrincipal, EvNotified}) {
		t.Fatalf("events after save = %v", got)
	}
}

// An exception comes from an effect that does not save first; it must not
// jump ahead of the decided events before it.
func TestPerform_ExceptionKeepsItsPlaceAfterHeldEvents(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	runID := h.runPiece("P1")
	h.runs.setRun(runID, limitHit(), false)
	h.runs.summary = limitHit()
	h.orch.decisions = []OrchDecision{{Action: "goto", Station: "plan", Reason: "bad plan"}}
	tk := heldTick(t, h)
	tk.unsaved = true
	if err := tk.perform([]Action{
		Record{Event: Event{Kind: EvWaitingOnPrincipal, Piece: "P0"}},
		Notify{Body: "p0 waits"},
		AskOrchestrator{Piece: "P1", Failure: "limit"},
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{EvWaitingOnPrincipal, EvNotified, EvException, EvDecision}
	if got := h.kinds(); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestPerform_HeldEventsAreDroppedWhenPerformFailsBeforeASave(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	tk := heldTick(t, h)
	tk.unsaved = true
	// A directory where state.json goes makes the effect's save fail.
	statePath := filepath.Join(h.store.Dir, "state.json")
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0o755); err != nil {
		t.Fatal(err)
	}
	err := tk.perform([]Action{
		Record{Event: Event{Kind: EvDecision}},
		StartPiece{Piece: "P1"},
	})
	if err == nil {
		t.Fatal("want the save to fail")
	}
	if got := h.kinds(); len(got) != 0 {
		t.Fatalf("events = %v", got)
	}
}
