package mission

import (
	"slices"
	"strings"
	"time"
)

// carried is a delegated piece whose sub-pieces an amendment left out
// entirely, and the pieces crew kept under it.
type carried struct {
	Parent string
	Pieces []string
}

// carryOver adds to next, in the current file order, every piece under a
// delegated piece that next keeps and names no piece under. A plan that names
// at least one piece under it is taken as written. next is not modified.
func carryOver(m Mission, s State, next Mission) (Mission, []carried) {
	out := Mission{Request: next.Request, Pieces: slices.Clone(next.Pieces), Changes: next.Changes}
	var kept []carried
	for _, d := range m.Pieces {
		ps, ok := s.Piece(d.ID)
		if !ok || ps.Status != PieceDelegated {
			continue
		}
		if _, inNext := out.Piece(d.ID); !inNext || namesUnder(out, d.ID) {
			continue
		}
		c := carried{Parent: d.ID}
		for _, p := range m.Pieces {
			if Under(p.ID, d.ID) {
				out.Pieces = append(out.Pieces, p)
				c.Pieces = append(c.Pieces, p.ID)
			}
		}
		if len(c.Pieces) > 0 {
			kept = append(kept, c)
		}
	}
	return out, kept
}

// namesUnder says whether m holds a piece under id.
func namesUnder(m Mission, id string) bool {
	return slices.ContainsFunc(m.Pieces, func(p Piece) bool { return Under(p.ID, id) })
}

// carryLog is crew's change-log entry for each carry-over.
func carryLog(kept []carried, now time.Time) []Change {
	out := make([]Change, 0, len(kept))
	for _, c := range kept {
		out = append(out, Change{Time: now, By: crewBy, What: "kept " + c.text() + ": the amendment named no piece under it"})
	}
	return out
}

// text is e.g. "P3.1, P3.2 under P3".
func (c carried) text() string {
	return strings.Join(c.Pieces, ", ") + " under " + c.Parent
}
