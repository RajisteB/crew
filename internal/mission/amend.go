package mission

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	maxWordsRunes   = 500
	errAmendRequest = "an amendment can't change the request; start a new mission for a different job"
	errAmendSame    = "the new plan is the same as the current one"
)

// Amendment is the orchestrator's new plan together with what the principal
// said that prompted it.
type Amendment struct {
	Plan  []byte `json:"plan"`  // a whole mission file
	Words string `json:"words"` // what the principal said, word for word
}

// Amend compares the new plan with the current mission and state and returns
// the new mission, the new state and what the engine must do. m and s are
// never modified; on an error they are returned as they came, with no
// actions and no run steps.
func Amend(m Mission, s State, a Amendment, in CheckInput, now time.Time) (Mission, State, []Action, []RunStep, error) {
	next, changes, err := planAmendment(m, s, a, in)
	if err != nil {
		return m, s, nil, nil, err
	}
	d := &decider{m: next, s: s.Clone(), o: Obs{Now: now}}
	steps := d.applyChanges(changes, a.Words)
	d.m.Changes = append(append([]Change(nil), m.Changes...), changeLog(changes, a.Words, now)...)
	return d.m, d.s, d.actions, steps, nil
}

// planAmendment parses and validates the new plan and lists its changes.
func planAmendment(m Mission, s State, a Amendment, in CheckInput) (Mission, []pieceChange, error) {
	if s.Status == StatusDone {
		return Mission{}, nil, errors.New("the mission is done and can't be amended")
	}
	next, err := Parse(a.Plan)
	if err != nil {
		return Mission{}, nil, fmt.Errorf("the new plan can't be read: %w", err)
	}
	if strings.TrimRight(next.Request, whitespace) != strings.TrimRight(m.Request, whitespace) {
		return Mission{}, nil, errors.New(errAmendRequest)
	}
	changes, err := diffPieces(m, s, next)
	switch {
	case err != nil:
		return Mission{}, nil, err
	case len(changes) == 0:
		return Mission{}, nil, errors.New(errAmendSame)
	}
	result := Mission{Pieces: next.Pieces, Request: m.Request, Changes: m.Changes}
	if errs := Check(result, in); len(errs) > 0 {
		return Mission{}, nil, fmt.Errorf("the new plan fails its checks: %w", errors.Join(errs...))
	}
	return result, changes, nil
}

const whitespace = " \t\r\n"

// applyChanges writes the changes into the working state and queues the
// records, cancellations and the summary. It returns the run steps.
func (d *decider) applyChanges(changes []pieceChange, words string) []RunStep {
	d.add(Record{Event: Event{Time: d.o.Now, Kind: EvAmendment, By: principalBy, Msg: cutRunes(words, maxWordsRunes)}})
	var steps []RunStep
	for _, c := range changes {
		switch c.Kind {
		case chAdd:
			d.s.Pieces = append(d.s.Pieces, PieceState{ID: c.Piece, Status: PieceQueued})
			d.add(Record{Event: Event{Time: d.o.Now, Kind: EvPieceAdded, Piece: c.Piece, By: principalBy, Msg: "added by an amendment"}})
		case chRebrief:
			d.add(Record{Event: Event{Time: d.o.Now, Kind: EvRebrief, Piece: c.Piece, By: principalBy, Msg: "brief changed"}})
		case chCancel:
			if step := d.cancelPiece(c.Piece, principalBy); step != nil {
				steps = append(steps, *step)
			}
		}
	}
	d.notify("amendment applied: " + summarise(changes))
	return steps
}

// changeLog is one change-log entry per change.
func changeLog(changes []pieceChange, words string, now time.Time) []Change {
	out := make([]Change, 0, len(changes))
	for _, c := range changes {
		out = append(out, Change{Time: now, By: principalBy, What: changeWhat(c), Why: words})
	}
	return out
}

func changeWhat(c pieceChange) string {
	switch c.Kind {
	case chAdd:
		return "added " + c.Piece
	case chRebrief:
		return "re-briefed " + c.Piece
	case chCancel:
		return "cancelled " + c.Piece
	}
	return c.Edit
}

// summarise counts the changes by kind, e.g. "1 added, 1 re-briefed".
func summarise(changes []pieceChange) string {
	var parts []string
	for _, k := range []struct{ kind, label string }{
		{chAdd, "added"}, {chRebrief, "re-briefed"}, {chEdit, "changed"}, {chCancel, "cancelled"},
	} {
		n := 0
		for _, c := range changes {
			if c.Kind == k.kind {
				n++
			}
		}
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, k.label))
		}
	}
	return strings.Join(parts, ", ")
}
