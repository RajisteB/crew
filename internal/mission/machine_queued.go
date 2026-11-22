package mission

import "fmt"

func (d *decider) queued(id string) {
	piece, ok := d.m.Piece(id)
	if !ok {
		return
	}
	ready := true
	for _, dep := range piece.WaitsFor {
		switch d.status(dep) {
		case PieceCancelled:
			q := fmt.Sprintf("%s waits for %s, which was cancelled; cancel it or amend the mission", id, dep)
			d.wait(id, WaitQuestion, q, PieceQueued)
			return
		case PieceComplete:
		default:
			ready = false
		}
	}
	for _, other := range piece.Apart {
		if d.keepsApart(other) {
			ready = false
		}
	}
	if ready && Allows(d.lim.Concurrency, d.holdingRuns()+d.starting()+1) {
		d.add(StartPiece{Piece: id})
	}
}

// holdingRuns counts the pieces that count toward the limit on runs at once:
// running pieces, and pieces waiting on the principal in the middle of a run
// (their wait goes back to running). A piece whose pull request waits for a
// merge, or that never started, does not count.
func (d *decider) holdingRuns() int {
	n := 0
	for _, p := range d.s.Pieces {
		if p.Status == PieceRunning || (p.Status == PieceWaiting && p.Wait != nil && p.Wait.Back == PieceRunning) {
			n++
		}
	}
	return n
}

// starting counts the pieces this call has already decided to start.
func (d *decider) starting() int {
	n := 0
	for _, a := range d.actions {
		if _, ok := a.(StartPiece); ok {
			n++
		}
	}
	return n
}

// keepsApart says whether the piece named is still in the way: it exists and
// is neither complete nor cancelled.
func (d *decider) keepsApart(id string) bool {
	p, ok := d.s.Piece(id)
	return ok && p.Status != PieceComplete && p.Status != PieceCancelled
}
