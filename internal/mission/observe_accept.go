package mission

import "github.com/crewfile-dev/crew/internal/principal"

// snapshot is the switches read once this tick.
func (t *tick) snapshot() *switchSnapshot {
	if t.switches == nil {
		snap := t.readSwitches()
		t.switches = &snap
	}
	return t.switches
}

// observeAccept fills Obs.Accept for every piece with a posted, unaccepted
// deliverable.
func (t *tick) observeAccept() {
	t.obs.Accept = map[string]AcceptObs{}
	for _, p := range t.s.Pieces {
		if p.Status == PieceComplete || p.Status == PieceCancelled || !unaccepted(p) {
			continue
		}
		mode := t.pieceMode(p)
		t.obs.Accept[p.ID] = AcceptObs{Mode: mode, MovedDone: mode == principal.AutoOff && t.movedDone(p)}
	}
}

// pieceMode is the switch of the piece's own domain; off when it can't be read.
func (t *tick) pieceMode(p PieceState) principal.AutoMode {
	piece, ok := t.m.Piece(p.ID)
	if !ok || t.e.Switches == nil {
		return principal.AutoOff
	}
	snap := t.snapshot()
	if snap.err != nil {
		t.noteSwitchError(p.ID, snap.err)
		return principal.AutoOff
	}
	return modeFor(snap.recs, piece.Domain)
}

// movedDone reports whether the principal moved the piece's issue to Done.
// crew reads the board only for an issue crew itself moved to In Review (not
// a move Jira refused) since the deliverable was collected: finding it in
// Done then is the principal's act, not a stale state.
func (t *tick) movedDone(p PieceState) bool {
	if t.e.Tracker == nil || p.Jira.Issue == "" || p.Jira.Lane != LaneInReview || p.Jira.Unconfirmed {
		return false
	}
	done, err := t.e.Tracker.Done(t.ctx, p.Jira.Issue)
	if err != nil {
		t.note(p.ID, "Jira: can't read %s: %v", p.Jira.Issue, err)
		return false
	}
	return done
}
