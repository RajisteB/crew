package mission

import (
	"errors"
	"fmt"
	"slices"
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
	plan, err := planAmendment(m, s, a, in, now)
	if err != nil {
		return m, s, nil, nil, err
	}
	next := plan.m
	d := &decider{m: next, s: s.Clone(), o: Obs{Now: now}}
	steps := d.applyChanges(plan.changes, a.Words)
	d.m.Changes = append(append([]Change(nil), m.Changes...), changeLog(plan.changes, a.Words, now)...)
	d.m.Changes = append(d.m.Changes, next.Changes[len(m.Changes):]...) // crew's own entries
	return d.m, d.s, d.actions, steps, nil
}

// amendPlan is a checked amendment: the new mission, its changes, and the
// sub-pieces crew carried over.
type amendPlan struct {
	m       Mission
	changes []pieceChange
	kept    []carried
}

// planAmendment parses and validates the new plan and lists its changes.
func planAmendment(m Mission, s State, a Amendment, in CheckInput, now time.Time) (amendPlan, error) {
	if s.Status == StatusDone {
		return amendPlan{}, errors.New("the mission is done and can't be amended")
	}
	parsed, err := Parse(a.Plan)
	if err != nil {
		return amendPlan{}, fmt.Errorf("the new plan can't be read: %w", err)
	}
	if strings.TrimRight(parsed.Request, whitespace) != strings.TrimRight(m.Request, whitespace) {
		return amendPlan{}, errors.New(errAmendRequest)
	}
	next, kept := carryOver(m, s, parsed)
	changes, err := diffPieces(m, s, next)
	switch {
	case err != nil:
		return amendPlan{}, err
	case len(changes) == 0:
		return amendPlan{}, errors.New(errAmendSame)
	}
	logged := append(slices.Clone(m.Changes), carryLog(kept, now)...)
	result := Mission{Pieces: next.Pieces, Request: m.Request, Changes: logged}
	if errs := Check(result, in); len(errs) > 0 {
		return amendPlan{}, fmt.Errorf("the new plan fails its checks: %w", errors.Join(errs...))
	}
	result, _, _ = SeparatedWithLog(m, result, in.Domains, StartedPieces(s), now)
	return amendPlan{m: result, changes: changes, kept: kept}, nil
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
			d.unpark(c.Piece)
			d.add(Record{Event: Event{Time: d.o.Now, Kind: EvRebrief, Piece: c.Piece, By: principalBy, Msg: "brief changed"}})
		case chEdit:
			d.unpark(c.Piece)
		case chCancel:
			if step := d.cancelPiece(c.Piece, principalBy); step != nil {
				steps = append(steps, *step)
			}
		}
	}
	d.notify("amendment applied: " + summarise(changes))
	return steps
}

// unpark puts a piece that was parked on a wait it never started from back
// in the queue, so the next Decide looks at its changed plan afresh.
func (d *decider) unpark(id string) {
	if p := d.ps(id); p != nil && p.Status == PieceWaiting && p.Wait != nil && p.Wait.Back == PieceQueued {
		if p.Wait.Kind == WaitPlan {
			p.Calls = 0
		}
		p.Status, p.Wait = PieceQueued, nil
	}
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
