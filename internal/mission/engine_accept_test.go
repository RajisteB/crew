package mission

import (
	"context"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/principal"
)

// deliveredHarness has P1 delivered with its wait, an issue in In Review and
// the product switch set to mode.
func deliveredHarness(t *testing.T, mode principal.AutoMode) *harness {
	t.Helper()
	h := newHarness(t, twoPieces()[0])
	h.runPiece("P1")
	h.edit(func(s *State) {
		p := &s.Pieces[0]
		p.Status = PieceDelivered
		p.Deliverable = &Deliverable{Files: briefFiles, Print: "p"}
		p.Wait = &Wait{ID: "w1", Kind: WaitDeliverable, Question: "q", Since: testNow}
		p.Jira = Tracked{Issue: "J-9", Lane: LaneInReview}
		s.NextWait = 2
	})
	h.e.Switches = &fakeSwitches{log: h.log, reads: []switchRead{modes(map[string]principal.AutoMode{"product": mode})}}
	return h
}

func TestEngine_ObservesAcceptance(t *testing.T) {
	t.Run("moved to Done on the board", func(t *testing.T) {
		h := deliveredHarness(t, principal.AutoOff)
		h.withTracker().doneKeys["J-9"] = true
		if p := h.tick().Pieces[0]; p.Status != PieceComplete || p.Deliverable == nil || p.Deliverable.By != "principal" {
			t.Fatalf("piece = %+v", p)
		}
		if h.log.count("done:J-9") != 1 {
			t.Fatalf("calls: %v", h.log.calls)
		}
	})
	t.Run("still in review", func(t *testing.T) {
		h := deliveredHarness(t, principal.AutoOff)
		h.withTracker()
		if p := h.tick().Pieces[0]; p.Status != PieceDelivered || p.Wait == nil {
			t.Fatalf("piece = %+v", p)
		}
	})
	t.Run("switch on never reads the board", func(t *testing.T) {
		h := deliveredHarness(t, principal.AutoOn)
		h.withTracker().doneKeys["J-9"] = true
		p := h.tick().Pieces[0]
		if p.Status != PieceComplete || p.Deliverable.By != "engine" {
			t.Fatalf("piece = %+v", p)
		}
		if h.log.has("done:") {
			t.Fatalf("calls: %v", h.log.calls)
		}
	})
	t.Run("no tracker", func(t *testing.T) {
		h := deliveredHarness(t, principal.AutoOff)
		if p := h.tick().Pieces[0]; p.Status != PieceDelivered || p.Wait == nil {
			t.Fatalf("piece = %+v", p)
		}
	})
}

func TestEngine_StaleDoneDoesNotAcceptANewDeliverable(t *testing.T) {
	h := deliveredHarness(t, principal.AutoOff)
	h.edit(func(s *State) { s.Pieces[0].Jira.Lane = LaneInProgress })
	h.withTracker().doneKeys["J-9"] = true
	p := h.tick().Pieces[0]
	if p.Status != PieceDelivered || p.Deliverable.Accepted {
		t.Fatalf("piece = %+v", p)
	}
	if h.log.has("done:") {
		t.Fatalf("read Done for a piece crew did not put in review: %v", h.log.calls)
	}
}

func TestEngine_DoneReadFailureIsANote(t *testing.T) {
	h := deliveredHarness(t, principal.AutoOff)
	h.withTracker().doneKeys["J-9"] = true
	h.fail.failNext("done", 1)
	p := h.tick().Pieces[0]
	if p.Status != PieceDelivered || !strings.Contains(noteText(h), "Jira: can't read J-9: ") {
		t.Fatalf("piece = %+v, notes = %q", p, noteText(h))
	}
}

func TestEngine_TamperedSwitchNeverAccepts(t *testing.T) {
	h := deliveredHarness(t, principal.AutoOn)
	h.e.Switches = &fakeSwitches{log: h.log, reads: []switchRead{{err: principal.ErrTampered}}}
	p := h.tick().Pieces[0]
	if p.Status != PieceDelivered || p.Wait == nil || p.Deliverable.Accepted {
		t.Fatalf("piece = %+v", p)
	}
	if !strings.Contains(noteText(h), "automerge.json was edited outside crew") {
		t.Fatalf("notes = %q", noteText(h))
	}
}

func TestEngine_SwitchesAreReadOnceATick(t *testing.T) {
	h := deliveredHarness(t, principal.AutoOff)
	h.tick()
	if n := h.log.count("Switches"); n != 1 {
		t.Fatalf("switch reads = %d: %v", n, h.log.calls)
	}
}

func TestEngine_MergeKeepsWorktreeOfDeliveredPiece(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.edit(func(s *State) { s.Pieces[0].Deliverable = &Deliverable{Files: briefFiles, Print: "p"} })
	tk, err := h.e.newTick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tk.merge(Merge{Piece: "P1", Head: "h1", Checked: true}); err != nil {
		t.Fatal(err)
	}
	p := h.piece("P1")
	if p.Status != PieceDelivered || p.Worktree == "" {
		t.Fatalf("piece = %+v", p)
	}
	if h.log.has("RemoveWorktree") {
		t.Fatalf("calls: %v", h.log.calls)
	}
}
