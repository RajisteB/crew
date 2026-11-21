package mission

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func exState() State {
	m := acme()
	s := State{MissionID: "m-1", Status: StatusRunning, NextWait: 1}
	for _, p := range m.Pieces {
		s.Pieces = append(s.Pieces, PieceState{ID: p.ID, Status: PieceQueued})
	}
	s.Pieces[0] = PieceState{ID: "P1", Status: PieceRunning, RunID: "r1", Worktree: "/wt/P1"}
	return s
}

func fenced(body string) string { return "```decision\n" + body + "\n```" }

func TestParseDecision_LastBlockWins(t *testing.T) {
	reply := "thinking\n" + fenced(`{"action":"retry"}`) + "\nmore\n```json\n{\"action\":\"x\"}\n```\n" +
		fenced(`{"action":"goto","station":"plan","reason":"r"}`) + "\nbye"
	d, err := ParseDecision(reply)
	if err != nil || d.Action != "goto" || d.Station != "plan" || d.Reason != "r" {
		t.Fatalf("d = %+v, err = %v", d, err)
	}
}

func TestParseDecision_NoBlockIsError(t *testing.T) {
	for name, reply := range map[string]string{
		"none":     "just words",
		"wrong":    "```json\n{\"action\":\"retry\"}\n```",
		"unclosed": "```decision\n{\"action\":\"retry\"}",
		"badjson":  fenced(`{"action":`),
		"unknown":  fenced(`{"action":"retry","bogus":1}`),
		"trailing": fenced(`{"action":"retry"} extra`),
		"two":      fenced(`{"action":"retry"}{"action":"cancel"}`),
	} {
		if _, err := ParseDecision(reply); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func apply(t *testing.T, d OrchDecision) (Mission, State, []Action, *RunStep) {
	t.Helper()
	return ApplyDecision(acme(), exState(), "P1", "limit-hit", d, "orchestrator", exInput(), t0)
}

func hasNotify(as []Action) bool {
	for _, a := range as {
		if _, ok := a.(Notify); ok {
			return true
		}
	}
	return false
}

func TestApplyDecision_RetryAndGoto(t *testing.T) {
	_, s, as, step := apply(t, OrchDecision{Action: "retry", Reason: "flaky"})
	if step == nil || step.Kind != "retry" || step.Piece != "P1" || !hasKind(as, EvDecision) {
		t.Errorf("step = %+v, actions = %v", step, as)
	}
	if got := s.Pieces[0].Decisions; len(got) != 1 || got[0].Action != "retry" || got[0].By != "orchestrator" || got[0].Failure != "limit-hit" || got[0].Reason != "flaky" {
		t.Errorf("decisions = %+v", got)
	}
}

func TestApplyDecision_GotoReturnsRunStep(t *testing.T) {
	_, s, as, step := apply(t, OrchDecision{Action: "goto", Station: "plan", Note: "start over", Reason: "bad plan"})
	if step == nil || *step != (RunStep{Piece: "P1", Kind: "goto", Station: "plan", Note: "start over"}) {
		t.Fatalf("step = %+v", step)
	}
	if s.Pieces[0].Decisions[0].Station != "plan" || !hasKind(as, EvDecision) {
		t.Errorf("state = %+v, actions = %v", s.Pieces[0], as)
	}
	_, s, as, step = apply(t, OrchDecision{Action: "goto", Station: "nowhere"})
	if step != nil || s.Pieces[0].Wait == nil || !hasKind(as, EvEscalated) {
		t.Errorf("unknown station accepted: step = %+v, wait = %+v", step, s.Pieces[0].Wait)
	}
}

func TestApplyDecision_CancelStopsAndCleans(t *testing.T) {
	_, s, as, step := apply(t, OrchDecision{Action: "cancel", Reason: "not needed"})
	if s.Pieces[0].Status != PieceCancelled || s.Pieces[0].Wait != nil {
		t.Errorf("piece = %+v", s.Pieces[0])
	}
	if step == nil || step.Kind != "stop" || step.Piece != "P1" {
		t.Errorf("step = %+v", step)
	}
	var cleanup *Cleanup
	for _, a := range as {
		if c, ok := a.(Cleanup); ok {
			cleanup = &c
		}
	}
	if cleanup == nil || cleanup.Piece != "P1" || cleanup.Force || !hasKind(as, EvPieceCancelled) {
		t.Errorf("cleanup = %+v, actions = %v", cleanup, as)
	}
}

func TestApplyDecision_CancelWithoutRunOrWorktree(t *testing.T) {
	s := exState()
	s.Pieces[0].RunID, s.Pieces[0].Worktree, s.Pieces[0].PR = "", "", 7
	_, s2, as, step := ApplyDecision(acme(), s, "P1", "failed", OrchDecision{Action: "cancel"}, "orchestrator", exInput(), t0)
	if step != nil {
		t.Errorf("step = %+v", step)
	}
	for _, a := range as {
		if _, ok := a.(Cleanup); ok {
			t.Error("cleanup without a worktree")
		}
		if r, ok := a.(Record); ok && r.Event.Kind == EvPieceCancelled && !strings.Contains(r.Event.Msg, "PR #7 stays open") {
			t.Errorf("msg = %q", r.Event.Msg)
		}
	}
	if s2.Pieces[0].Status != PieceCancelled {
		t.Error("not cancelled")
	}
}

func TestApplyDecision_RebriefOnlyQueued(t *testing.T) {
	m, s, as, step := apply(t, OrchDecision{Action: "rebrief", Target: "P2", Brief: "new invite screen", Reason: "scope"})
	p2, _ := m.Piece("P2")
	if p2.Brief != "new invite screen" || step != nil || !hasKind(as, EvRebrief) || s.Pieces[0].Wait != nil {
		t.Fatalf("brief = %q, step = %+v, actions = %v", p2.Brief, step, as)
	}
	if n := len(m.Changes); n != 1 || m.Changes[0].By != "orchestrator" || m.Changes[0].Why != "scope" {
		t.Errorf("changes = %+v", m.Changes)
	}
	m, s, as, _ = apply(t, OrchDecision{Action: "rebrief", Target: "P1", Brief: "x"})
	p1, _ := m.Piece("P1")
	if p1.Brief != "credits API" || s.Pieces[0].Wait == nil || !hasKind(as, EvEscalated) {
		t.Errorf("rebrief of a running piece was accepted: %q", p1.Brief)
	}
}

func newPiece() *Piece {
	return &Piece{ID: "P7", Domain: "marketing", Factory: "campaign", Brief: "press kit"}
}

func TestApplyDecision_AddNeedsQuoteFromRequest(t *testing.T) {
	m, s, as, _ := apply(t, OrchDecision{Action: "add", Piece: newPiece(), Quote: "a landing page", Reason: "needed"})
	if len(m.Pieces) != len(acme().Pieces)+1 || len(s.Pieces) != len(exState().Pieces)+1 {
		t.Fatalf("pieces = %d, state pieces = %d", len(m.Pieces), len(s.Pieces))
	}
	added := s.Pieces[len(s.Pieces)-1]
	if added.ID != "P7" || added.Status != PieceQueued || !added.Added || s.Additions != 1 {
		t.Errorf("added = %+v, additions = %d", added, s.Additions)
	}
	if !hasKind(as, EvPieceAdded) || !hasNotify(as) || len(m.Changes) != 1 || m.Changes[0].By != "orchestrator" {
		t.Errorf("actions = %v, changes = %+v", as, m.Changes)
	}
	m, s, as, _ = apply(t, OrchDecision{Action: "add", Piece: newPiece(), Quote: "a podcast"})
	if len(m.Pieces) != len(acme().Pieces) || s.Pieces[0].Wait == nil || !hasKind(as, EvEscalated) {
		t.Error("add with an invented quote was accepted")
	}
	bad := newPiece()
	bad.Factory = "missing"
	_, s, _, _ = apply(t, OrchDecision{Action: "add", Piece: bad, Quote: "a landing page"})
	if s.Pieces[0].Wait == nil {
		t.Error("add that fails Check was accepted")
	}
}

func TestApplyDecision_AddPastLimitEscalates(t *testing.T) {
	s := exState()
	s.Additions = 2
	m, s2, as, _ := ApplyDecision(acme(), s, "P1", "failed", OrchDecision{Action: "add", Piece: newPiece(), Quote: "a landing page"}, "orchestrator", exInput(), t0)
	if len(m.Pieces) != len(acme().Pieces) || s2.Additions != 2 || s2.Pieces[0].Wait == nil || !hasKind(as, EvEscalated) {
		t.Errorf("past-limit add was accepted")
	}
}

func TestApplyDecision_InvalidEscalates(t *testing.T) {
	_, s, as, step := apply(t, OrchDecision{Action: "merge", Reason: "ship it"})
	w := s.Pieces[0].Wait
	if step != nil || w == nil || w.Kind != WaitQuestion || w.Back != PieceRunning || s.Pieces[0].Status != PieceWaiting {
		t.Fatalf("piece = %+v", s.Pieces[0])
	}
	if !strings.HasPrefix(w.Question, "the orchestrator's decision couldn't be used:") || w.ID != "w1" || s.NextWait != 2 {
		t.Errorf("wait = %+v", w)
	}
	if d := s.Pieces[0].Decisions; len(d) != 1 || d[0].Action != "invalid" {
		t.Errorf("decisions = %+v", d)
	}
	if !hasKind(as, EvEscalated) || !hasNotify(as) {
		t.Errorf("actions = %v", as)
	}
	_, s, _, _ = apply(t, OrchDecision{Reason: "the call timed out"})
	if q := s.Pieces[0].Wait.Question; !strings.Contains(q, "the call timed out") {
		t.Errorf("question = %q", q)
	}
}

func TestApplyDecision_EscalateHoldsNote(t *testing.T) {
	_, s, as, _ := apply(t, OrchDecision{Action: "escalate", Note: "Which credit amount?"})
	w := s.Pieces[0].Wait
	if w == nil || w.Kind != WaitQuestion || w.Question != "Which credit amount?" || !hasKind(as, EvEscalated) || !hasNotify(as) {
		t.Errorf("wait = %+v, actions = %v", w, as)
	}
}

func TestApplyDecision_RepeatEscalates(t *testing.T) {
	m, s := acme(), exState()
	goPlan := OrchDecision{Action: "goto", Station: "plan"}
	m, s, _, step := ApplyDecision(m, s, "P1", "limit-hit", goPlan, "orchestrator", exInput(), t0)
	if step == nil {
		t.Fatal("first goto refused")
	}
	_, s2, as, step := ApplyDecision(m, s, "P1", "limit-hit", goPlan, "orchestrator", exInput(), t0)
	w := s2.Pieces[0].Wait
	if step != nil || w == nil || !strings.HasPrefix(w.Question, "the orchestrator's decision couldn't be used:") || !hasKind(as, EvEscalated) {
		t.Fatalf("repeat accepted: step = %+v, wait = %+v", step, w)
	}
	if d := s2.Pieces[0].Decisions; len(d) != 2 || d[1].Action != "goto" {
		t.Errorf("decisions = %+v", d)
	}
	_, _, _, step = ApplyDecision(m, s, "P1", "limit-hit", OrchDecision{Action: "goto", Station: "build"}, "orchestrator", exInput(), t0)
	if step == nil || step.Station != "build" {
		t.Errorf("goto build refused: %+v", step)
	}
	_, _, _, step = ApplyDecision(m, s, "P1", "failed", goPlan, "orchestrator", exInput(), t0)
	if step == nil {
		t.Error("same action for a different failure refused")
	}
}

func TestApply_DoesNotMutateInput(t *testing.T) {
	m, s := acme(), exState()
	m.Changes = []Change{{By: "x"}}
	mBefore, sBefore := m.WithChange(Change{}), s.Clone()
	mBefore.Changes = mBefore.Changes[:1]
	for _, d := range []OrchDecision{
		{Action: "cancel"}, {Action: "retry"}, {Action: "bogus"}, {Action: "escalate", Note: "q"},
		{Action: "rebrief", Target: "P2", Brief: "b"},
		{Action: "add", Piece: newPiece(), Quote: "a landing page"},
	} {
		ApplyDecision(m, s, "P1", "failed", d, "orchestrator", exInput(), t0)
		if !reflect.DeepEqual(s, sBefore) || !reflect.DeepEqual(m, mBefore) {
			t.Fatalf("%s mutated its input", d.Action)
		}
	}
}

// longStrings lists every string the returned state and actions hold.
func longStrings(s State, as []Action) []string {
	var out []string
	for _, p := range s.Pieces {
		for _, d := range p.Decisions {
			out = append(out, d.Failure, d.Action, d.Station, d.Note, d.Reason)
		}
		if p.Wait != nil {
			out = append(out, p.Wait.Question)
		}
	}
	for _, a := range as {
		switch v := a.(type) {
		case Record:
			out = append(out, v.Event.Msg, v.Event.Piece, v.Event.By)
		case Notify:
			out = append(out, v.Title, v.Body)
		}
	}
	return out
}

func TestApplyDecision_ModelTextIsCut(t *testing.T) {
	long := strings.Repeat("é", 5000)
	for name, d := range map[string]OrchDecision{
		"escalate": {Action: "escalate", Note: long, Reason: long},
		"goto":     {Action: "goto", Station: "plan", Note: long, Reason: long},
		"unknown":  {Action: long, Note: long, Reason: long},
		"noaction": {Reason: long},
	} {
		_, s, as, step := apply(t, d)
		for _, str := range longStrings(s, as) {
			if n := len([]rune(str)); n > 600 || !utf8.ValidString(str) {
				t.Errorf("%s: stored string has %d runes (valid %v)", name, n, utf8.ValidString(str))
			}
		}
		if name == "goto" && (step == nil || len([]rune(step.Note)) != 500) {
			t.Errorf("goto step = %+v", step)
		}
	}
}
