package mission

import (
	"fmt"
	"slices"
)

// PlanPieces asks the scoped orchestrator to split a delegated piece.
type PlanPieces struct{ Piece string }

func (PlanPieces) isAction() {}

const msgAllUnderCancelled = "every piece under it was cancelled"

// plan asks for a ready delegated piece to be split, or, when the piece has
// used its orchestrator calls, hands the question to the principal.
func (d *decider) plan(id string) {
	p := d.ps(id)
	if Allows(d.lim.OrchestratorCalls, p.Calls+1) {
		d.add(PlanPieces{Piece: id})
		return
	}
	d.callBudgetHit(id, p.Calls)
	q := fmt.Sprintf("%s could not be planned in %d orchestrator calls", id, p.Calls)
	if reason := lastPlanReason(*p); reason != "" {
		q += ": " + reason
	}
	d.wait(id, WaitPlan, q, PieceQueued)
}

// lastPlanReason is the reason of the piece's last planning try, or "".
func lastPlanReason(p PieceState) string {
	for i := len(p.Decisions) - 1; i >= 0; i-- {
		if p.Decisions[i].Failure == failurePlan {
			return p.Decisions[i].Reason
		}
	}
	return ""
}

// delegated finishes each delegated piece whose pieces are all finished,
// deepest level first so a piece waiting on a finished one is freed in the
// same call. A delegated piece never had a run, so nothing is cleaned up. One
// with no piece under it in the state goes back to the queue to be planned.
func (d *decider) delegated() {
	ids := d.idsWith(PieceDelegated)
	slices.SortStableFunc(ids, func(a, b string) int { return Level(b) - Level(a) })
	for _, id := range ids {
		if complete, cancelled, open := d.childCounts(id); open {
			continue
		} else if complete+cancelled == 0 {
			d.ps(id).Status = PieceQueued
			d.record(EvNote, id, id+" has no pieces under it; it will be planned again")
		} else if complete > 0 {
			d.complete(id)
		} else {
			p := d.ps(id)
			p.Status, p.Wait = PieceCancelled, nil
			d.record(EvPieceCancelled, id, msgAllUnderCancelled)
		}
	}
}

// childCounts counts the direct children of id in the state that are complete
// and cancelled; open is true when any is neither.
func (d *decider) childCounts(id string) (complete, cancelled int, open bool) {
	for _, c := range d.s.Pieces {
		if ParentID(c.ID) != id {
			continue
		}
		switch c.Status {
		case PieceComplete:
			complete++
		case PieceCancelled:
			cancelled++
		default:
			open = true
		}
	}
	return complete, cancelled, open
}
