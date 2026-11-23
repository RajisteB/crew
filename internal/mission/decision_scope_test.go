package mission

import (
	"slices"
	"strings"
	"testing"
)

func TestAskerOf(t *testing.T) {
	for _, c := range []struct {
		id   string
		up   int
		want string
	}{
		{"P3.1.2", 0, "P3.1"}, {"P3.1.2", 1, "P3"}, {"P3.1.2", 2, ""}, {"P3.1.2", 3, ""},
		{"P1", 0, ""}, {"P3.1", 0, "P3"}, {"P3.1", 1, ""},
	} {
		if got := AskerOf(PieceState{ID: c.id, Up: c.up}); got != c.want {
			t.Errorf("AskerOf(%s, up %d) = %q, want %q", c.id, c.up, got, c.want)
		}
	}
}

// scopeFixture is a mission with a delegated P3 whose sub-piece P3.1 is stuck
// after one orchestrator call (the call just made, as the engine counts it).
func scopeFixture() (Mission, State, CheckInput) {
	m := treePlan()
	s := State{MissionID: "m-1", Status: StatusRunning, NextWait: 1}
	for _, p := range m.Pieces {
		s.Pieces = append(s.Pieces, PieceState{ID: p.ID, Status: PieceQueued})
	}
	set := func(id string, f func(*PieceState)) {
		for i := range s.Pieces {
			if s.Pieces[i].ID == id {
				f(&s.Pieces[i])
			}
		}
	}
	set("P3", func(p *PieceState) { p.Status = PieceDelegated })
	set("P3.1", func(p *PieceState) { p.Status, p.RunID, p.Worktree, p.Calls = PieceRunning, "r1", "/wt/P3.1", 1 })
	in := delegatingInput(2)
	in.Limits = Limits{Pieces: lim(10), Additions: lim(2), OrchestratorCalls: lim(3), Depth: lim(2)}
	return m, s, in
}

// edit changes the piece with this id in s.
func edit(s *State, id string, f func(*PieceState)) {
	for i := range s.Pieces {
		if s.Pieces[i].ID == id {
			f(&s.Pieces[i])
		}
	}
}

func applyScoped(t *testing.T, d OrchDecision) (Mission, State, []Action, *RunStep) {
	t.Helper()
	m, s, in := scopeFixture()
	return ApplyScopedDecision(m, s, "P3.1", "limit-hit", d, "P3", in, t0)
}

func pieceOf(s State, id string) PieceState {
	p, _ := s.Piece(id)
	return p
}

func hasAsk(as []Action, piece string) bool {
	for _, a := range as {
		if q, ok := a.(AskOrchestrator); ok && q.Piece == piece {
			return true
		}
	}
	return false
}

func eventsBy(as []Action, kind string) []Event {
	var out []Event
	for _, a := range as {
		if r, ok := a.(Record); ok && r.Event.Kind == kind {
			out = append(out, r.Event)
		}
	}
	return out
}

func TestApplyScopedDecision_GotoIsCarriedOut(t *testing.T) {
	m, s, in := scopeFixture()
	edit(&s, "P3.1", func(p *PieceState) { p.Up = 1 })
	_, s2, as, step := ApplyScopedDecision(m, s, "P3.1", "limit-hit", OrchDecision{Action: "goto", Station: "build", Reason: "r"}, "P3", in, t0)
	if step == nil || step.Kind != "goto" || step.Station != "build" {
		t.Fatalf("step = %+v", step)
	}
	p := pieceOf(s2, "P3.1")
	if p.Up != 0 || len(p.Decisions) != 1 || p.Decisions[0].By != "scoped orchestrator P3" {
		t.Errorf("piece = %+v", p)
	}
	if ev := eventsBy(as, EvDecision); len(ev) != 1 || ev[0].By != "scoped orchestrator P3" {
		t.Errorf("events = %+v", ev)
	}
}

func TestApplyScopedDecision_EscalateRaisesInsteadOfWaiting(t *testing.T) {
	_, s, as, step := applyScoped(t, OrchDecision{Action: "escalate", Note: "which icon set?", Reason: "unsure"})
	p := pieceOf(s, "P3.1")
	if step != nil || p.Wait != nil || p.Up != 1 || hasNotify(as) {
		t.Fatalf("step %+v, piece %+v, notify %v", step, p, hasNotify(as))
	}
	ev := eventsBy(as, EvEscalated)
	if len(ev) != 1 || ev[0].By != "scoped orchestrator P3" || ev[0].Msg != "which icon set?" {
		t.Errorf("events = %+v", ev)
	}
	if !hasAsk(as, "P3.1") {
		t.Errorf("no AskOrchestrator in %v", as)
	}
	if len(p.Decisions) != 1 || p.Decisions[0].Action != "escalate" {
		t.Errorf("decisions = %+v", p.Decisions)
	}
}

func TestApplyScopedDecision_UnusableDecisionIsRaised(t *testing.T) {
	for name, d := range map[string]OrchDecision{
		"no action":   {},
		"not allowed": {Action: "dance"},
		"bad field":   {Action: "goto", Station: "nowhere"},
		"too long":    {Action: strings.Repeat("a", 200)},
		"no note":     {Action: "escalate"},
		"no quote":    {Action: "add", Piece: &Piece{Factory: "visuals", Brief: "b"}, Quote: "not in the request"},
	} {
		_, s, as, step := applyScoped(t, d)
		p := pieceOf(s, "P3.1")
		ev := eventsBy(as, EvEscalated)
		if step != nil || p.Wait != nil || p.Up != 1 || hasNotify(as) || !hasAsk(as, "P3.1") ||
			len(ev) != 1 || !strings.HasPrefix(ev[0].Msg, invalidPrefix) {
			t.Errorf("%s: step %+v piece %+v events %+v", name, step, p, ev)
		}
	}
	// a repeat is raised too
	m, s, in := scopeFixture()
	g := OrchDecision{Action: "goto", Station: "build"}
	m, s, _, _ = ApplyScopedDecision(m, s, "P3.1", "limit-hit", g, "P3", in, t0)
	_, s, as, _ := ApplyScopedDecision(m, s, "P3.1", "limit-hit", g, "P3", in, t0)
	if p := pieceOf(s, "P3.1"); p.Wait != nil || p.Up != 1 || !hasAsk(as, "P3.1") {
		t.Errorf("repeat: %+v", p)
	}
}

func TestApplyScopedDecision_ClimbStopsAtTheCallLimit(t *testing.T) {
	m, s, in := scopeFixture()
	edit(&s, "P3.1", func(p *PieceState) { p.Calls = 3 })
	_, s2, as, _ := ApplyScopedDecision(m, s, "P3.1", "limit-hit", OrchDecision{Action: "escalate", Note: "q"}, "P3", in, t0)
	p := pieceOf(s2, "P3.1")
	if p.Wait == nil || p.Wait.Kind != WaitLimit || hasAsk(as, "P3.1") || !hasNotify(as) {
		t.Fatalf("piece %+v, actions %v", p, as)
	}
	if want := "P3.1 hit the orchestrator call limit after a limit-hit failure. scoped orchestrator P3 asked: q"; p.Wait.Question != want {
		t.Fatalf("question = %q, want %q", p.Wait.Question, want)
	}
	edit(&s, "P3.1", func(p *PieceState) { p.Calls = 2 }) // one call left
	_, s2, as, _ = ApplyScopedDecision(m, s, "P3.1", "limit-hit", OrchDecision{Action: "escalate", Note: "q"}, "P3", in, t0)
	if p := pieceOf(s2, "P3.1"); p.Wait != nil || !hasAsk(as, "P3.1") {
		t.Fatalf("with a call left: piece %+v, actions %v", p, as)
	}
}

func TestApplyScopedDecision_RebriefStaysInTheSubtree(t *testing.T) {
	m, s, in := scopeFixture()
	_, s2, as, _ := ApplyScopedDecision(m, s, "P3.1", "limit-hit", OrchDecision{Action: "rebrief", Target: "P3.2", Brief: "new", Reason: "r"}, "P3", in, t0)
	if got, _ := s2.Piece("P3.1"); got.Up != 0 || len(eventsBy(as, EvRebrief)) != 1 {
		t.Errorf("rebrief inside refused: %+v", got)
	}
	m2, _, _, _ := ApplyScopedDecision(m, s, "P3.1", "limit-hit", OrchDecision{Action: "rebrief", Target: "P3.2", Brief: "new", Reason: "r"}, "P3", in, t0)
	if p, _ := m2.Piece("P3.2"); p.Brief != "new" || m2.Changes[0].By != "scoped orchestrator P3" {
		t.Errorf("mission = %+v", m2.Changes)
	}
	_, s3, as, _ := ApplyScopedDecision(m, s, "P3.1", "limit-hit", OrchDecision{Action: "rebrief", Target: "P1", Brief: "new"}, "P3", in, t0)
	ev := eventsBy(as, EvEscalated)
	if pieceOf(s3, "P3.1").Up != 1 || len(ev) != 1 || !strings.Contains(ev[0].Msg, `rebrief target "P1" is outside P3`) {
		t.Errorf("outside rebrief: %+v", ev)
	}
}

func TestApplyScopedDecision_AddNamesTheNextNumber(t *testing.T) {
	m, s, in := scopeFixture()
	edit(&s, "P3.2", func(p *PieceState) { p.Status = PieceCancelled })
	d := OrchDecision{Action: "add", Quote: "Launch referrals", Reason: "r",
		Piece: &Piece{ID: "X9", Factory: "visuals", Brief: "more icons"}}
	m2, s2, as, _ := ApplyScopedDecision(m, s, "P3.1", "limit-hit", d, "P3", in, t0)
	p, ok := m2.Piece("P3.3")
	if !ok || p.Domain != "design" || !slices.Equal(p.Paths, []string{"design/**"}) {
		t.Fatalf("pieces = %+v, events %+v", m2.Pieces, eventsBy(as, EvEscalated))
	}
	if _, ok := m2.Piece("X9"); ok {
		t.Error("the decision's id was used")
	}
	if ps, ok := s2.Piece("P3.3"); !ok || !ps.Added || s2.Additions != 1 {
		t.Errorf("state = %+v additions %d", ps, s2.Additions)
	}
	ev := eventsBy(as, EvPieceAdded)
	if len(ev) != 1 || ev[0].Piece != "P3.3" || ev[0].Msg != "added by the scoped orchestrator P3" {
		t.Errorf("events = %+v", ev)
	}
	if ch := m2.Changes[0]; ch.By != "scoped orchestrator P3" || ch.What != "add piece P3.3" {
		t.Errorf("changes = %+v", m2.Changes)
	}
}

func TestApplyScopedDecision_AddOutsideTheParentsPathsIsRaised(t *testing.T) {
	d := OrchDecision{Action: "add", Quote: "Launch referrals",
		Piece: &Piece{Factory: "visuals", Brief: "b", Paths: []string{"site/**"}}}
	m, s, as, _ := applyScoped(t, d)
	ev := eventsBy(as, EvEscalated)
	if _, ok := m.Piece("P3.3"); ok || len(ev) != 1 || !strings.HasPrefix(ev[0].Msg, invalidPrefix) || pieceOf(s, "P3.1").Up != 1 || s.Additions != 0 {
		t.Errorf("events = %+v, up = %d", ev, pieceOf(s, "P3.1").Up)
	}
}

func TestApplyDecision_EscalateTwiceIsNotARepeat(t *testing.T) {
	m, s := acme(), exState()
	e := OrchDecision{Action: "escalate", Note: "which amount?"}
	m, s, _, _ = ApplyDecision(m, s, "P1", "limit-hit", e, "orchestrator", exInput(), t0)
	s.Pieces[0].Wait, s.Pieces[0].Status = nil, PieceRunning
	_, s2, _, _ := ApplyDecision(m, s, "P1", "limit-hit", e, "orchestrator", exInput(), t0)
	w := s2.Pieces[0].Wait
	if w == nil || w.Question != "which amount?" {
		t.Fatalf("wait = %+v", w)
	}
}

func TestApplyAnswer_ResetsUp(t *testing.T) {
	m, s, in := scopeFixture()
	edit(&s, "P3.1", func(p *PieceState) {
		p.Up, p.Status = 2, PieceWaiting
		p.Wait = &Wait{ID: "w1", Kind: WaitQuestion, Question: "q", Since: t0, Back: PieceRunning}
	})
	_, s2, _, _, err := ApplyAnswer(m, s, Answer{Wait: "w1", Do: "retry"}, in, t0)
	if err != nil || pieceOf(s2, "P3.1").Up != 0 {
		t.Fatalf("err %v piece %+v", err, pieceOf(s2, "P3.1"))
	}
}

func TestUp_ResetWhenTheStopEndsAnyOtherWay(t *testing.T) {
	_, s, _ := scopeFixture()
	edit(&s, "P3.1", func(p *PieceState) { p.Up = 1 })
	if got := pieceOf(Merged(s, "P3.1"), "P3.1"); got.Up != 0 {
		t.Errorf("merged: %+v", got)
	}
	if got := pieceOf(Resumed(s, "P3.1"), "P3.1"); got.Up != 0 {
		t.Errorf("resumed: %+v", got)
	}
	d := &decider{m: treePlan(), s: s.Clone(), o: Obs{Now: t0}}
	d.complete("P3.1")
	if d.ps("P3.1").Up != 0 {
		t.Error("complete")
	}
	d = &decider{m: treePlan(), s: s.Clone(), o: Obs{Now: t0}}
	d.ps("P3.1").Deliverable = &Deliverable{}
	d.finished("P3.1")
	if d.ps("P3.1").Up != 0 {
		t.Error("delivered")
	}
}
