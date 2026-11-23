package mission

import (
	"slices"
	"time"

	"github.com/crewfile-dev/crew/internal/source"
)

// AskerOf is the delegated piece whose orchestrator is asked about ps next:
// Ancestors(ps.ID)[ps.Up], or "" (the main orchestrator) when Up is past them.
func AskerOf(ps PieceState) string {
	above := Ancestors(ps.ID)
	if ps.Up < 0 || ps.Up >= len(above) {
		return ""
	}
	return above[ps.Up]
}

// ApplyScopedDecision is ApplyDecision for a decision made by the orchestrator
// scoped to the delegated piece scope. With scope "" it is ApplyDecision by the
// orchestrator. A scoped orchestrator's escalation, or a decision it makes that
// can't be used, is raised one level instead of waiting on the principal.
func ApplyScopedDecision(m Mission, s State, piece, failure string, d OrchDecision, scope string, in CheckInput, now time.Time) (Mission, State, []Action, *RunStep) {
	by := orchestratorBy
	if scope != "" {
		by = scopedBy(scope)
	}
	return applyDecision(m, s, piece, failure, d, by, scope, in, now)
}

// pass puts a question to whoever is next: the principal for the main
// orchestrator, the next orchestrator up for a scoped one.
func (a *applier) pass(question string) {
	if a.scope == "" {
		a.escalate(question)
		return
	}
	a.raise(question)
}

// raise records the question and puts the piece to the next orchestrator with
// the decider's ask: a call when one is left, else the usual wait on the
// principal, which then carries the question. The engine counted the call
// just made before applying.
func (a *applier) raise(question string) {
	a.ps(a.piece).Up++
	a.add(Record{Event: Event{Time: a.o.Now, Kind: EvEscalated, Piece: a.piece, By: a.by, Msg: question}})
	a.askPassing(a.piece, a.failure, a.by+" asked: "+question)
}

// addShapeProblem is what is wrong with the piece an add names, before the
// plan is checked: none, a scope that is gone, a delegated piece, or a dotted
// id from the main orchestrator. "" when nothing is.
func (a *applier) addShapeProblem(d OrchDecision) string {
	_, scopeKnown := a.m.Piece(a.scope)
	switch {
	case d.Piece == nil:
		return "add needs a piece"
	case a.scope != "" && !scopeKnown:
		return "the scope " + a.scope + " is not a piece of the mission"
	case d.Piece.Delegate:
		return "the new piece can't be delegated; add the pieces themselves"
	case a.scope == "" && ParentID(d.Piece.ID) != "":
		return `the new piece's id can't hold a "."`
	}
	return ""
}

// scopedAdd makes an add by a scoped orchestrator one that crew names: the id
// is the scope's next child, an empty domain is the scope's, and no paths are
// the scope's effective paths. The decision's own id is never used.
func (a *applier) scopedAdd(d OrchDecision) OrchDecision {
	parent, ok := a.m.Piece(a.scope)
	if d.Action != "add" || d.Piece == nil || !ok {
		return d
	}
	p := copyPiece(*d.Piece)
	p.ID = NextChildID(a.s, a.scope)
	if p.Domain == "" {
		p.Domain = parent.Domain
	}
	if len(p.Paths) == 0 {
		i := slices.IndexFunc(a.in.Domains, func(dm source.Domain) bool { return dm.Name == parent.Domain })
		var dom source.Domain
		if i >= 0 {
			dom = a.in.Domains[i]
		}
		p.Paths = slices.Clone(EffectivePaths(parent, dom))
	}
	d.Piece = &p
	return d
}
