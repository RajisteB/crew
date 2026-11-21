package mission

import (
	"errors"
	"fmt"

	"github.com/crewfile-dev/crew/internal/principal"
)

// repoAllowsPush runs the engine's repo gate before a push. It returns true
// when the push may go ahead. A repo that is no longer confirmed stops the
// mission: the piece keeps its state, so resume continues it. Any other
// gate error is a note and the push is tried again next tick.
func (t *tick) repoAllowsPush(piece string) (bool, error) {
	if t.e.RepoGate == nil {
		return true, nil
	}
	err := t.e.RepoGate(t.ctx)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, principal.ErrNotConfirmed), errors.Is(err, principal.ErrVisibilityChanged):
		return false, t.stopForRepo(err)
	}
	t.note(piece, "can't check the repo before pushing: %v", err)
	return false, nil
}

// stopForRepo stops the mission because the repo needs confirming again.
func (t *tick) stopForRepo(cause error) error {
	msg := fmt.Sprintf("%v; no push was made. Run crew repo confirm, then crew mission resume %s", cause, t.s.MissionID)
	next := t.s.Clone()
	next.Status = StatusStopped
	t.s = next
	if err := t.save(); err != nil {
		return err
	}
	t.halted = true
	t.emit(Event{Kind: EvStopped, Msg: msg})
	t.notify(Notify{Title: "crew mission " + t.s.MissionID, Body: "mission " + t.s.MissionID + " stopped: " + msg})
	return nil
}
