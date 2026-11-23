package mission

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ApplySubPlan adds the sub-pieces to the mission and marks parent delegated,
// or records why the reply can't be used. m and s are never modified.
func ApplySubPlan(m Mission, s State, parent string, sp SubPlan, in CheckInput, now time.Time) (Mission, State, []Action) {
	pp, ok := m.Piece(parent)
	if st, found := s.Piece(parent); !ok || !pp.Delegate || !found || st.Status != PieceQueued {
		return m, s, nil
	}
	sp.Question = cutText(sp.Question, maxReasonRunes)
	sp.Reason = cutText(sp.Reason, maxReasonRunes)
	refuse := func(why string) (Mission, State, []Action) {
		next, acts := PlanRefused(s, parent, why, now)
		return m, next, acts
	}
	switch {
	case sp.Question != "" && len(sp.Pieces) == 0:
		d := &decider{m: m, s: s.Clone(), o: Obs{Now: now}}
		d.askForPlan(parent, sp)
		return m, d.s, d.actions
	case len(sp.Pieces) == 0:
		return refuse("the plan lists no pieces")
	case sp.Question != "":
		return refuse("the plan holds both pieces and a question")
	}
	pieces, err := namePieces(m, s, pp, sp.Pieces, in)
	if err != nil {
		return refuse(cutText(err.Error(), maxReasonRunes))
	}
	ids := make([]string, len(pieces))
	for i, p := range pieces {
		ids[i] = p.ID
	}
	next := m.WithChange(Change{Time: now, By: scopedBy(parent), What: "planned " + parent + " as " + strings.Join(ids, ", "), Why: sp.Reason})
	next.Pieces = append(next.Pieces, pieces...)
	if errs := Check(next, in); len(errs) > 0 {
		return refuse(cutText(firstMessages(errs), maxReasonRunes))
	}
	settled := StartedPieces(s)
	settled[parent] = true
	next, _, _ = SeparatedWithLog(m, next, in.Domains, settled, now)
	d := &decider{m: next, s: s.Clone(), o: Obs{Now: now}}
	d.delegate(parent, ids, sp.Reason)
	return d.m, d.s, d.actions
}

// askForPlan holds the delegated piece on the scoped orchestrator's question.
func (d *decider) askForPlan(parent string, sp SubPlan) {
	by := scopedBy(parent)
	p := d.ps(parent)
	p.Decisions = append(p.Decisions, Decision{Time: d.o.Now, By: by, Failure: failurePlan, Action: "escalate", Note: sp.Question, Reason: sp.Reason})
	d.openWait(parent, WaitPlan, sp.Question, PieceQueued)
	d.add(Record{Event: Event{Time: d.o.Now, Kind: EvEscalated, Piece: parent, By: by, Msg: sp.Question}})
	d.notify(waitText(*p))
}

// delegate marks parent delegated and queues the new pieces after the others.
func (d *decider) delegate(parent string, ids []string, reason string) {
	by := scopedBy(parent)
	p := d.ps(parent)
	p.Status, p.Wait = PieceDelegated, nil
	list := strings.Join(ids, ", ")
	d.add(Record{Event: Event{Time: d.o.Now, Kind: EvDecision, Piece: parent, By: by, Msg: fmt.Sprintf("planned as %s: %s", list, reason)}})
	for _, id := range ids {
		d.s.Pieces = append(d.s.Pieces, PieceState{ID: id, Status: PieceQueued})
		d.add(Record{Event: Event{Time: d.o.Now, Kind: EvPieceAdded, Piece: id, By: by, Msg: "planned under " + parent}})
	}
	d.notify(fmt.Sprintf("%s was planned as %d pieces: %s", parent, len(ids), list))
}

// namePieces gives the reply's pieces their ids under parent, rewrites their
// waits_for, and fills in the domain and paths they inherit.
func namePieces(m Mission, s State, parent Piece, in []Piece, ci CheckInput) ([]Piece, error) {
	first, _ := strconv.Atoi(strings.TrimPrefix(NextChildID(s, parent.ID), parent.ID+"."))
	newID := map[string]string{}
	for i, p := range in {
		switch {
		case p.ID == "":
			return nil, fmt.Errorf("a piece of the plan has no id")
		case newID[p.ID] != "":
			return nil, fmt.Errorf("two pieces of the plan share the id %q", p.ID)
		}
		newID[p.ID] = fmt.Sprintf("%s.%d", parent.ID, first+i)
	}
	dom, _ := findDomain(ci.Domains, parent.Domain)
	inherited := EffectivePaths(parent, dom)
	out := make([]Piece, len(in))
	for i, p := range in {
		p = copyPiece(p)
		p.ID = newID[p.ID]
		for j, w := range p.WaitsFor {
			id, ok := newID[w]
			if !ok {
				return nil, fmt.Errorf("%q is not a piece of this plan; a sub-piece waits only for pieces under %s. Ask a question instead if %s needs another piece first", w, parent.ID, parent.ID)
			}
			p.WaitsFor[j] = id
		}
		if p.Domain == "" {
			p.Domain = parent.Domain
		}
		if len(p.Paths) == 0 {
			p.Paths = slices.Clone(inherited)
		}
		out[i] = p
	}
	return out, nil
}

// PlanRefused records a planning try that produced nothing usable: a decision
// on parent and a `decision` event. parent stays queued.
func PlanRefused(s State, parent, why string, now time.Time) (State, []Action) {
	d := &decider{s: s.Clone(), o: Obs{Now: now}}
	p := d.ps(parent)
	if p == nil {
		return d.s, nil
	}
	why = cutText(why, maxReasonRunes)
	by := scopedBy(parent)
	p.Decisions = append(p.Decisions, Decision{Time: now, By: by, Failure: failurePlan, Action: invalidAction, Reason: why})
	d.add(Record{Event: Event{Time: now, Kind: EvDecision, Piece: parent, By: by, Msg: "plan refused: " + why}})
	return d.s, d.actions
}
