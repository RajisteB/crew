package mission

import (
	"fmt"
	"slices"
	"strings"
)

// Kinds of change an amendment makes to a piece.
const (
	chAdd     = "add"
	chRebrief = "rebrief"
	chEdit    = "edit"
	chCancel  = "cancel"
)

// pieceChange is one difference between the current plan and the new one.
type pieceChange struct {
	Kind  string
	Piece string
	Edit  string // chEdit: what changed, e.g. "P4: waits_for changed"
}

// diffPieces compares the new plan with the current mission and state. It
// reads only; the error names the first piece the amendment may not touch.
func diffPieces(m Mission, s State, next Mission) ([]pieceChange, error) {
	var out []pieceChange
	for _, p := range next.Pieces {
		cs, err := changesFor(m, s, p)
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
	}
	for _, old := range m.Pieces {
		if _, kept := next.Piece(old.ID); kept {
			continue
		}
		c, err := removal(s, old.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, c...)
	}
	return out, nil
}

// changesFor is what the new plan's piece p changes about the current one.
func changesFor(m Mission, s State, p Piece) ([]pieceChange, error) {
	old, inMission := m.Piece(p.ID)
	ps, inState := s.Piece(p.ID)
	switch {
	case !inMission && inState:
		return nil, fmt.Errorf("piece %s was used before in this mission", p.ID)
	case !inMission:
		return []pieceChange{{Kind: chAdd, Piece: p.ID}}, nil
	}
	fields := differingFields(old, p)
	switch {
	case len(fields) == 0:
		return nil, nil
	case !notStarted(ps):
		return nil, fmt.Errorf("piece %s is %s and can't be changed; only a queued piece can (changed: %s)", p.ID, ps.Status, strings.Join(fields, ", "))
	}
	var out []pieceChange
	if i := slices.Index(fields, "brief"); i >= 0 {
		out = append(out, pieceChange{Kind: chRebrief, Piece: p.ID})
		fields = slices.Delete(fields, i, i+1)
	}
	if len(fields) > 0 {
		out = append(out, pieceChange{Kind: chEdit, Piece: p.ID, Edit: p.ID + ": " + strings.Join(fields, ", ") + " changed"})
	}
	return out, nil
}

// notStarted says whether a piece hasn't started: queued, or parked with a
// wait that goes back to queued (it waited for a piece that was cancelled).
func notStarted(ps PieceState) bool {
	return ps.Status == PieceQueued || (ps.Status == PieceWaiting && ps.Wait != nil && ps.Wait.Back == PieceQueued)
}

// removal is the change for a piece missing from the new plan.
func removal(s State, id string) ([]pieceChange, error) {
	ps, ok := s.Piece(id)
	switch {
	case !ok:
		return nil, fmt.Errorf("piece %s has no state", id)
	case ps.Status == PieceCancelled:
		return nil, nil
	case ps.Status == PieceComplete:
		return nil, fmt.Errorf("piece %s is complete and can't be removed", id)
	}
	return []pieceChange{{Kind: chCancel, Piece: id}}, nil
}

// differingFields names the fields of a and b that differ. apart_from is not
// one of them: crew writes it.
func differingFields(a, b Piece) []string {
	var out []string
	for _, f := range []struct {
		name string
		same bool
	}{
		{"domain", a.Domain == b.Domain},
		{"factory", a.Factory == b.Factory},
		{"delegate", a.Delegate == b.Delegate},
		{"brief", a.Brief == b.Brief},
		{"paths", slices.Equal(a.Paths, b.Paths)},
		{"waits_for", slices.Equal(a.WaitsFor, b.WaitsFor)},
	} {
		if !f.same {
			out = append(out, f.name)
		}
	}
	return out
}
