package mission

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/principal"
)

// gatePiece puts P1 in the state where the next tick pushes and opens a PR.
func gatePiece(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, twoPieces()[0])
	runID := h.runPiece("P1")
	h.runs.setRun(runID, doneState(), false)
	return h
}

func TestEngine_RepoGateVisibilityChangedStopsBeforePush(t *testing.T) {
	h := gatePiece(t)
	h.e.RepoGate = func(context.Context) error { return principal.ErrVisibilityChanged }
	st := h.tick()
	if h.log.has("Push") {
		t.Fatalf("pushed after the gate refused: %v", h.log.calls)
	}
	if st.Status != StatusStopped || h.state().Status != StatusStopped {
		t.Fatalf("status = %s", st.Status)
	}
	if p := h.piece("P1"); p.Status != PieceRunning || p.RunID == "" {
		t.Fatalf("the piece must keep its state: %+v", p)
	}
	if len(h.notif.sent) != 1 {
		t.Fatalf("notifications = %v", h.notif.sent)
	}
	evs := h.events(EvStopped)
	if len(evs) != 1 || !containsAll(evs[0].Msg, "crew repo confirm", "crew mission resume "+h.id) {
		t.Fatalf("stopped events = %+v", evs)
	}
	if !containsAll(h.notif.sent[0], "crew repo confirm", "crew mission resume "+h.id) {
		t.Fatalf("notification = %q", h.notif.sent[0])
	}
}

func TestEngine_RepoGateNotConfirmedStops(t *testing.T) {
	h := gatePiece(t)
	h.e.RepoGate = func(context.Context) error { return errors.Join(errors.New("x"), principal.ErrNotConfirmed) }
	if st := h.tick(); st.Status != StatusStopped || h.log.has("Push") {
		t.Fatalf("status %s calls %v", st.Status, h.log.calls)
	}
}

func TestEngine_RepoGatePlainErrorRetriesNextTick(t *testing.T) {
	h := gatePiece(t)
	gateErr := errors.New("gh is down")
	h.e.RepoGate = func(context.Context) error { return gateErr }
	st := h.tick()
	if st.Status != StatusRunning || h.log.has("Push") {
		t.Fatalf("status %s calls %v", st.Status, h.log.calls)
	}
	if notes := h.events(EvNote); len(notes) != 1 || notes[0].Piece != "P1" {
		t.Fatalf("notes = %+v", notes)
	}
	if len(h.notif.sent) != 0 {
		t.Fatalf("notifications = %v", h.notif.sent)
	}
	gateErr = nil
	h.e.RepoGate = func(context.Context) error { return nil }
	if p := h.tick().Pieces[0]; p.Status != PiecePROpen || !h.log.has("Push") {
		t.Fatalf("after the gate cleared: %+v calls %v", p, h.log.calls)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
