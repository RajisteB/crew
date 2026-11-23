package mission

import (
	"context"
	"strings"
	"testing"
)

// A planned delegated piece can't be changed by an amendment: its sub-pieces
// were checked and separated against it as it stands.
func TestAmend_ADelegatedPieceCantBeChanged(t *testing.T) {
	for name, change := range map[string]func(*Piece){
		"waits_for": func(p *Piece) { p.WaitsFor = []string{"P1"} },
		"paths":     func(p *Piece) { p.Paths = []string{"design/**", "brand/**"} },
		"brief":     func(p *Piece) { p.Brief = "the visuals, bolder" },
	} {
		_, _, err := amendTree(t, func(m *Mission) { change(&m.Pieces[5]) })
		want := "piece P3 is delegated and can't be changed; only a queued piece can (changed: " + name + ")"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestApplyDecision_ADelegatedPieceCantBeRebriefed(t *testing.T) {
	m, s, in := scopeFixture()
	d := OrchDecision{Action: "rebrief", Target: "P3", Brief: "the visuals, bolder", Reason: "r"}
	m2, s2, _, _ := ApplyScopedDecision(m, s, "P3.1", "limit-hit", d, "", in, t0)
	w := pieceOf(s2, "P3.1").Wait
	want := invalidPrefix + "piece P3 is delegated; only a queued piece can be re-briefed"
	if p, _ := m2.Piece("P3"); p.Brief != "the visuals" || w == nil || !strings.Contains(w.Question, want) {
		t.Fatalf("brief %q wait %+v", p.Brief, w)
	}
}

func TestApplyScopedDecision_ADelegatedPieceCantBeRebriefed(t *testing.T) {
	m, s, in := scopeFixture()
	edit(&s, "P3.2", func(p *PieceState) { p.Status = PieceDelegated })
	d := OrchDecision{Action: "rebrief", Target: "P3.2", Brief: "a bolder banner", Reason: "r"}
	m2, _, as, _ := ApplyScopedDecision(m, s, "P3.1", "limit-hit", d, "P3", in, t0)
	ev := eventsBy(as, EvEscalated)
	want := invalidPrefix + "piece P3.2 is delegated; only a queued piece can be re-briefed"
	if p, _ := m2.Piece("P3.2"); p.Brief != "banner" || len(ev) != 1 || ev[0].Msg != want {
		t.Fatalf("brief %q events %+v", p.Brief, ev)
	}
}

// amendDuringPlan drops an amendment while the planning call is in flight.
type amendDuringPlan struct {
	*fakeOrch
	drop func()
}

func (o amendDuringPlan) Plan(ctx context.Context, dir string, r PlanRequest) (SubPlan, error) {
	o.drop()
	return o.fakeOrch.Plan(ctx, dir, r)
}

func TestEngine_AnAmendmentQueuedDuringPlanningKeepsTheSubPieces(t *testing.T) {
	h := delegatedHarness(t)
	h.orch.plans = []SubPlan{goodPlan()}
	before, _ := h.store.Mission()
	next := Mission{Request: before.Request, Pieces: append(before.Pieces,
		Piece{ID: "P2", Domain: "product", Factory: "feature", Brief: "docs", Paths: []string{"docs/**"}})}
	a := Amendment{Plan: next.Render(), Words: "add the docs"}
	in := h.e.Checks
	in.Limits = h.e.Settings.For(h.state().Repo)
	if _, _, _, _, err := Amend(before, h.state(), a, in, planNow); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	h.e.Orch = amendDuringPlan{fakeOrch: h.orch, drop: func() {
		if err := h.store.Drop("amend", a); err != nil {
			t.Error(err)
		}
	}}
	h.tick() // plans P1 while the amendment is dropped
	h.tick() // applies the amendment
	for id, want := range map[string]PieceStatus{"P1": PieceDelegated, "P2": PieceRunning, "P1.1": PieceRunning, "P1.2": PieceQueued} {
		if got := h.piece(id).Status; got != want {
			t.Errorf("%s = %s, want %s", id, got, want)
		}
	}
	m, _ := h.store.Mission()
	if !hasChange(m, "kept P1.1, P1.2 under P1: the amendment named no piece under it") {
		t.Fatalf("changes = %+v", m.Changes)
	}
}
