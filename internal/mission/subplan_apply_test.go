package mission

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var planNow = time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)

// delegatedMission is a mission whose only piece, P3, is delegated in design.
func delegatedMission(paths ...string) (Mission, State, CheckInput) {
	m := Mission{Request: "Rebrand acme/app.", Pieces: []Piece{
		{ID: "P3", Domain: "design", Delegate: true, Brief: "the visuals", Paths: paths},
		{ID: "P1", Domain: "product", Factory: "feature", Brief: "api", Paths: []string{"app/**"}},
	}}
	s := State{MissionID: "m-1", Status: StatusRunning, NextWait: 1, Pieces: []PieceState{
		{ID: "P3", Status: PieceQueued}, {ID: "P1", Status: PieceQueued}}}
	in := delegatingInput(2)
	in.Limits.Pieces = lim(12)
	return m, s, in
}

func halvesPlan() SubPlan {
	return SubPlan{Reason: "split", Pieces: []Piece{
		{ID: "a", Factory: "visuals", Brief: "logo"},
		{ID: "b", Factory: "visuals", Brief: "banner", WaitsFor: []string{"a"}},
	}}
}

func decisionsOf(s State, id string) []Decision {
	p, _ := s.Piece(id)
	return p.Decisions
}

func TestApplySubPlan_AddsNamedPieces(t *testing.T) {
	m, s, in := delegatedMission("brand/**")
	m0, s0 := m, s.Clone()
	m0.Pieces = slices.Clone(m.Pieces)
	got, gs, acts := ApplySubPlan(m, s, "P3", halvesPlan(), in, planNow)
	if !reflect.DeepEqual(m, m0) || !reflect.DeepEqual(s, s0) {
		t.Fatal("inputs changed")
	}
	if len(got.Pieces) != 4 || got.Pieces[2].ID != "P3.1" || got.Pieces[3].ID != "P3.2" {
		t.Fatalf("pieces = %+v", got.Pieces)
	}
	for _, p := range got.Pieces[2:] {
		if p.Domain != "design" || !slices.Equal(p.Paths, []string{"brand/**"}) {
			t.Errorf("%s: %+v", p.ID, p)
		}
	}
	if !slices.Equal(got.Pieces[3].WaitsFor, []string{"P3.1"}) {
		t.Errorf("waits_for = %v", got.Pieces[3].WaitsFor)
	}
	last := got.Changes[len(got.Changes)-1]
	if ch := got.Changes[0]; ch.By != "scoped orchestrator P3" || ch.What != "planned P3 as P3.1, P3.2" || ch.Why != "split" {
		t.Errorf("change = %+v (last %+v)", ch, last)
	}
	if st, _ := gs.Piece("P3"); st.Status != PieceDelegated || st.Wait != nil {
		t.Errorf("P3 = %+v", st)
	}
	if len(gs.Pieces) != 4 || gs.Pieces[2].ID != "P3.1" || gs.Pieces[3].Status != PieceQueued {
		t.Errorf("state pieces = %+v", gs.Pieces)
	}
	var kinds []string
	var body string
	for _, a := range acts {
		switch a := a.(type) {
		case Record:
			kinds = append(kinds, a.Event.Kind+":"+a.Event.Piece+":"+a.Event.By)
		case Notify:
			body = a.Body
		}
	}
	want := []string{"decision:P3:scoped orchestrator P3", "piece-added:P3.1:scoped orchestrator P3", "piece-added:P3.2:scoped orchestrator P3"}
	if !slices.Equal(kinds, want) {
		t.Errorf("events = %v", kinds)
	}
	if body != "P3 was planned as 2 pieces: P3.1, P3.2" {
		t.Errorf("notification = %q", body)
	}
}

func TestApplySubPlan_KeepsCountingAfterEarlierNumbers(t *testing.T) {
	m, s, in := delegatedMission("brand/**")
	s.Pieces = append(s.Pieces, PieceState{ID: "P3.1", Status: PieceCancelled})
	got, _, _ := ApplySubPlan(m, s, "P3", halvesPlan(), in, planNow)
	if len(got.Pieces) != 4 || got.Pieces[2].ID != "P3.2" || got.Pieces[3].ID != "P3.3" {
		t.Fatalf("pieces = %+v", got.Pieces)
	}
}

func TestApplySubPlan_RefusesWhatReachesOutside(t *testing.T) {
	big := SubPlan{}
	for i := range 13 {
		big.Pieces = append(big.Pieces, Piece{ID: string(rune('a' + i)), Factory: "visuals", Brief: "x"})
	}
	cases := []struct {
		name string
		plan SubPlan
		want string
		in   func(*CheckInput)
	}{
		{"other domain", SubPlan{Pieces: []Piece{{ID: "a", Domain: "product", Factory: "feature", Brief: "x"}}}, "domain", nil},
		{"other factory", SubPlan{Pieces: []Piece{{ID: "a", Factory: "feature", Brief: "x"}}}, "feature", nil},
		{"paths outside", SubPlan{Pieces: []Piece{{ID: "a", Factory: "visuals", Brief: "x", Paths: []string{"site/**"}}}}, "outside the paths", nil},
		{"waits outside", SubPlan{Pieces: []Piece{{ID: "a", Factory: "visuals", Brief: "x", WaitsFor: []string{"P1"}}}},
			`"P1" is not a piece of this plan; a sub-piece waits only for pieces under P3. Ask a question instead if P3 needs another piece first`, nil},
		{"too many", big, "is more than the limit of 12", nil},
		{"too deep", SubPlan{Pieces: []Piece{{ID: "a", Delegate: true, Brief: "x", Paths: []string{"brand/a/**"}}}}, "delegates at depth 2", func(in *CheckInput) { in.Limits.Depth = lim(1) }},
		{"duplicate id", SubPlan{Pieces: []Piece{{ID: "a", Factory: "visuals"}, {ID: "a", Factory: "visuals"}}}, `two pieces of the plan share the id "a"`, nil},
		{"no pieces", SubPlan{}, "the plan lists no pieces", nil},
		{"both", SubPlan{Question: "q?", Pieces: []Piece{{ID: "a", Factory: "visuals", Brief: "x"}}}, "the plan holds both pieces and a question", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, s, in := delegatedMission("brand/**")
			if c.in != nil {
				c.in(&in)
			}
			got, gs, _ := ApplySubPlan(m, s, "P3", c.plan, in, planNow)
			if !reflect.DeepEqual(got, m) || len(gs.Pieces) != 2 {
				t.Fatalf("mission or state changed: %+v", got)
			}
			st, _ := gs.Piece("P3")
			ds := decisionsOf(gs, "P3")
			if st.Status != PieceQueued || len(ds) != 1 || ds[0].Action != "invalid" || ds[0].Failure != "plan" || !strings.Contains(ds[0].Reason, c.want) {
				t.Errorf("status %s, decisions %+v, want %q", st.Status, ds, c.want)
			}
		})
	}
}

func TestApplySubPlan_QuestionOpensAPlanWait(t *testing.T) {
	m, s, in := delegatedMission("brand/**")
	got, gs, acts := ApplySubPlan(m, s, "P3", SubPlan{Question: "which logo?", Reason: "unclear"}, in, planNow)
	if !reflect.DeepEqual(got, m) {
		t.Error("mission changed")
	}
	st, _ := gs.Piece("P3")
	if st.Status != PieceWaiting || st.Wait == nil || st.Wait.Kind != WaitPlan || st.Wait.Question != "which logo?" || st.Wait.Back != PieceQueued {
		t.Fatalf("P3 = %+v", st)
	}
	if d := st.Decisions; len(d) != 1 || d[0].Action != "escalate" || d[0].Note != "which logo?" || d[0].Reason != "unclear" || d[0].Failure != "plan" {
		t.Errorf("decisions = %+v", d)
	}
	var esc bool
	var body string
	for _, a := range acts {
		if r, ok := a.(Record); ok && r.Event.Kind == EvEscalated && r.Event.By == "scoped orchestrator P3" {
			esc = true
		}
		if n, ok := a.(Notify); ok {
			body = n.Body
		}
	}
	if !esc || body != "P3: which logo?" {
		t.Errorf("esc %v, body %q", esc, body)
	}
}

func TestApplySubPlan_NotQueuedOrNotDelegatedIsUnchanged(t *testing.T) {
	m, s, in := delegatedMission("brand/**")
	s.Pieces[0].Status = PieceWaiting
	got, gs, acts := ApplySubPlan(m, s, "P3", halvesPlan(), in, planNow)
	if !reflect.DeepEqual(got, m) || !reflect.DeepEqual(gs, s) || acts != nil {
		t.Error("waiting parent changed")
	}
	_, _, acts = ApplySubPlan(m, s, "P1", halvesPlan(), in, planNow)
	if acts != nil {
		t.Error("non-delegated parent acted")
	}
}

func TestApplySubPlan_OverlappingSubPiecesAreKeptApart(t *testing.T) {
	m, s, in := delegatedMission("brand/**")
	sp := SubPlan{Pieces: []Piece{{ID: "a", Factory: "visuals", Brief: "x"}, {ID: "b", Factory: "visuals", Brief: "y"}}}
	got, _, _ := ApplySubPlan(m, s, "P3", sp, in, planNow)
	if !slices.Equal(got.Pieces[3].Apart, []string{"P3.1"}) {
		t.Fatalf("apart = %v", got.Pieces[3].Apart)
	}
	if c := got.Changes[len(got.Changes)-1]; c.By != "crew" || !strings.Contains(c.What, "P3.2 starts after P3.1") {
		t.Errorf("change = %+v", c)
	}
}

func TestApplySubPlan_NestedDelegate(t *testing.T) {
	m, s, in := delegatedMission("brand/**")
	sp := SubPlan{Pieces: []Piece{{ID: "a", Delegate: true, Brief: "x", Paths: []string{"brand/a/**"}}}}
	got, gs, _ := ApplySubPlan(m, s, "P3", sp, in, planNow)
	if len(got.Pieces) != 3 || !got.Pieces[2].Delegate || got.Pieces[2].ID != "P3.1" {
		t.Fatalf("pieces = %+v", got.Pieces)
	}
	if st, _ := gs.Piece("P3.1"); st.Status != PieceQueued {
		t.Errorf("P3.1 = %+v", st)
	}
}

func TestPlanRefused(t *testing.T) {
	_, s, _ := delegatedMission("brand/**")
	got, acts := PlanRefused(s, "P3", "no good", planNow)
	if st, _ := s.Piece("P3"); len(st.Decisions) != 0 {
		t.Fatal("input changed")
	}
	st, _ := got.Piece("P3")
	want := Decision{Time: planNow, By: "scoped orchestrator P3", Failure: "plan", Action: "invalid", Reason: "no good"}
	if st.Status != PieceQueued || len(st.Decisions) != 1 || st.Decisions[0] != want {
		t.Errorf("P3 = %+v", st)
	}
	r, ok := acts[0].(Record)
	if len(acts) != 1 || !ok || r.Event.Kind != EvDecision || r.Event.Msg != "plan refused: no good" || r.Event.By != "scoped orchestrator P3" {
		t.Errorf("acts = %+v", acts)
	}
	if _, a := PlanRefused(s, "P9", "x", planNow); a != nil {
		t.Error("unknown piece acted")
	}
}

func TestApplySubPlan_ParentCountsAsStartedWhenSeparating(t *testing.T) {
	m, s, in := delegatedMission("brand/**")
	// P1 comes first in file order and overlaps P3: with P3 settled, P1 is the
	// one kept apart, so the sub-pieces under P3 don't inherit an entry.
	m.Pieces = []Piece{
		{ID: "P1", Domain: "design", Factory: "visuals", Brief: "assets", Paths: []string{"brand/logo/**"}},
		m.Pieces[0],
	}
	s.Pieces = []PieceState{{ID: "P1", Status: PieceQueued}, {ID: "P3", Status: PieceQueued}}
	sp := SubPlan{Pieces: []Piece{{ID: "a", Factory: "visuals", Brief: "x", Paths: []string{"brand/icons/**"}}}}
	got, _, _ := ApplySubPlan(m, s, "P3", sp, in, planNow)
	p1, _ := got.Piece("P1")
	p3, _ := got.Piece("P3")
	if !slices.Equal(p1.Apart, []string{"P3"}) || len(p3.Apart) != 0 {
		t.Errorf("P1.Apart = %v, P3.Apart = %v", p1.Apart, p3.Apart)
	}
}

func TestApplySubPlan_CutsReplyText(t *testing.T) {
	long := strings.Repeat("é", maxReasonRunes+50)
	m, s, in := delegatedMission("brand/**")
	_, gs, acts := ApplySubPlan(m, s, "P3", SubPlan{Question: long, Reason: long}, in, planNow)
	st, _ := gs.Piece("P3")
	d := st.Decisions[0]
	for name, v := range map[string]string{"wait": st.Wait.Question, "note": d.Note, "reason": d.Reason} {
		if n := utf8.RuneCountInString(v); n != maxReasonRunes {
			t.Errorf("%s has %d runes", name, n)
		}
	}
	for _, a := range acts {
		if n, ok := a.(Notify); ok && utf8.RuneCountInString(n.Body) > maxReasonRunes+10 {
			t.Errorf("notification has %d runes", utf8.RuneCountInString(n.Body))
		}
	}
	m, s, in = delegatedMission("brand/**")
	_, gs, _ = ApplySubPlan(m, s, "P3", SubPlan{Reason: long, Pieces: []Piece{{ID: "a", Factory: "visuals", Brief: "x", WaitsFor: []string{long}}}}, in, planNow)
	if r := decisionsOf(gs, "P3")[0].Reason; utf8.RuneCountInString(r) != maxReasonRunes {
		t.Errorf("refusal has %d runes", utf8.RuneCountInString(r))
	}
	got, _, _ := ApplySubPlan(m, s, "P3", SubPlan{Reason: long, Pieces: []Piece{{ID: "a", Factory: "visuals", Brief: "x"}}}, in, planNow)
	if n := utf8.RuneCountInString(got.Changes[0].Why); n != maxReasonRunes {
		t.Errorf("change Why has %d runes", n)
	}
}
