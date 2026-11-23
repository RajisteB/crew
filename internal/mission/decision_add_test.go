package mission

import (
	"strings"
	"testing"
)

const noDelegatedAdd = invalidPrefix + "the new piece can't be delegated; add the pieces themselves"

func TestApplyDecision_AnAddCantBeDelegated(t *testing.T) {
	m, s, in := scopeFixture()
	d := OrchDecision{Action: "add", Quote: "Launch referrals", Reason: "r",
		Piece: &Piece{ID: "P7", Domain: "design", Delegate: true, Brief: "b", Paths: []string{"design/z/**"}}}
	m2, s2, _, _ := ApplyScopedDecision(m, s, "P3.1", "limit-hit", d, "", in, t0)
	w := pieceOf(s2, "P3.1").Wait
	if _, ok := m2.Piece("P7"); ok || s2.Additions != 0 || w == nil || w.Question != noDelegatedAdd {
		t.Fatalf("a delegated add was accepted: wait %+v additions %d", w, s2.Additions)
	}
}

func TestApplyScopedDecision_AnAddCantBeDelegated(t *testing.T) {
	d := OrchDecision{Action: "add", Quote: "Launch referrals", Reason: "r",
		Piece: &Piece{Delegate: true, Brief: "b", Paths: []string{"design/x/**"}}}
	m, s, as, _ := applyScoped(t, d)
	ev := eventsBy(as, EvEscalated)
	if _, ok := m.Piece("P3.3"); ok || s.Additions != 0 || len(ev) != 1 || ev[0].Msg != noDelegatedAdd {
		t.Fatalf("a delegated add was accepted: events %+v additions %d", ev, s.Additions)
	}
}

func TestApplyScopedDecision_AnAddWhoseScopeIsGoneIsRaised(t *testing.T) {
	m, s, in := scopeFixture()
	d := OrchDecision{Action: "add", Quote: "Launch referrals", Reason: "r",
		Piece: &Piece{ID: "X9", Domain: "design", Factory: "visuals", Brief: "b", Paths: []string{"design/x/**"}}}
	m2, s2, as, _ := ApplyScopedDecision(m, s, "P3.1", "limit-hit", d, "P9", in, t0)
	ev := eventsBy(as, EvEscalated)
	if _, ok := m2.Piece("X9"); ok || s2.Additions != 0 || len(ev) != 1 || ev[0].Msg != invalidPrefix+"the scope P9 is not a piece of the mission" {
		t.Fatalf("events %+v additions %d", ev, s2.Additions)
	}
}

func TestApplySubPlan_ABlankQuestionIsNoQuestion(t *testing.T) {
	m, s, in := scopeFixture()
	edit(&s, "P3", func(p *PieceState) { p.Status = PieceQueued })
	m = Mission{Request: m.Request, Pieces: m.Pieces[:6]} // P3 not planned yet
	s.Pieces = s.Pieces[:6]
	_, s2, _ := ApplySubPlan(m, s, "P3", SubPlan{Question: " \n\t", Reason: "r"}, in, t0)
	p := pieceOf(s2, "P3")
	if p.Wait != nil || len(p.Decisions) != 1 || p.Decisions[0].Action != invalidAction || !strings.Contains(p.Decisions[0].Reason, "the plan lists no pieces") {
		t.Fatalf("P3 = %+v", p)
	}
}
