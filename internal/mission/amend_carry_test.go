package mission

import (
	"reflect"
	"strings"
	"testing"
)

// carryInput is delegatingInput with room for a tree and a few additions.
func carryInput() CheckInput {
	in := delegatingInput(2)
	in.Limits = Limits{Pieces: lim(12), Additions: lim(2), OrchestratorCalls: lim(3), Depth: lim(2), Concurrency: lim(3)}
	return in
}

// beforeSplit is a mission whose P3 is delegated and not planned yet.
func beforeSplit() Mission {
	return Mission{Request: "Rebrand acme/app.", Pieces: []Piece{
		{ID: "P3", Domain: "design", Delegate: true, Brief: "the visuals", Paths: []string{"design/**"}},
		{ID: "P4", Domain: "marketing", Factory: "feature", Brief: "landing page", Paths: []string{"site/**"}, WaitsFor: []string{"P3"}},
	}}
}

// afterSplit is beforeSplit once P3 is planned as P3.1, P3.2 and P3.1 runs.
func afterSplit(t *testing.T) (Mission, State) {
	t.Helper()
	s := State{MissionID: "m-1", Status: StatusRunning, NextWait: 1, Pieces: []PieceState{
		{ID: "P3", Status: PieceQueued}, {ID: "P4", Status: PieceQueued}}}
	m, s, _ := ApplySubPlan(beforeSplit(), s, "P3", SubPlan{Reason: "split", Pieces: []Piece{
		{ID: "a", Factory: "visuals", Brief: "logo", Paths: []string{"design/logo/**"}},
		{ID: "b", Factory: "visuals", Brief: "banner", Paths: []string{"design/banner/**"}},
	}}, carryInput(), planNow)
	if _, ok := m.Piece("P3.2"); !ok {
		t.Fatalf("setup: %+v", m.Pieces)
	}
	return m, Started(s, "P3.1", "r1", "crew/m-1/P3.1", "/wt/P3.1")
}

// staleAmendment re-briefs P4 in the plan as it was before P3 was split.
func staleAmendment() Amendment {
	stale := beforeSplit()
	stale.Pieces[1].Brief = "landing page, with the $15 credit"
	return Amendment{Plan: stale.Render(), Words: "make it $15"}
}

func statuses(s State) map[string]PieceStatus {
	out := map[string]PieceStatus{}
	for _, p := range s.Pieces {
		out[p.ID] = p.Status
	}
	return out
}

func TestAmend_KeepsTheSubPiecesAStaleCopyLeavesOut(t *testing.T) {
	m, s := afterSplit(t)
	m2, s2, acts, steps, err := Amend(m, s, staleAmendment(), carryInput(), planNow)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]PieceStatus{"P3": PieceDelegated, "P4": PieceQueued, "P3.1": PieceRunning, "P3.2": PieceQueued}
	if got := statuses(s2); !reflect.DeepEqual(got, want) {
		t.Fatalf("states = %v", got)
	}
	if len(steps) != 0 {
		t.Fatalf("run steps = %+v", steps)
	}
	var ids []string
	for _, p := range m2.Pieces {
		ids = append(ids, p.ID)
	}
	if !reflect.DeepEqual(ids, []string{"P3", "P4", "P3.1", "P3.2"}) || m2.Pieces[1].Brief != "landing page, with the $15 credit" {
		t.Fatalf("pieces = %+v", m2.Pieces)
	}
	if n := of[Notify](acts); len(n) != 1 || n[0].Body != "amendment applied: 1 re-briefed" {
		t.Fatalf("notify = %+v", n)
	}
	kept := "kept P3.1, P3.2 under P3: the amendment named no piece under it"
	found := false
	for _, c := range m2.Changes {
		if c.What == kept {
			found = c.By == crewBy
		}
	}
	if !found {
		t.Fatalf("changes = %+v", m2.Changes)
	}
}

func TestAmend_ACarryOverAloneIsNoChange(t *testing.T) {
	m, s := afterSplit(t)
	_, _, _, _, err := Amend(m, s, Amendment{Plan: beforeSplit().Render(), Words: "w"}, carryInput(), planNow)
	if err == nil || err.Error() != errAmendSame {
		t.Fatalf("err = %v", err)
	}
}

func TestAmend_NamingOneSubPieceCancelsTheOther(t *testing.T) {
	m, s := afterSplit(t)
	next := Mission{Request: m.Request, Pieces: m.Pieces[:3]} // P3, P4, P3.1
	_, s2, _, _, err := Amend(m, s, Amendment{Plan: next.Render(), Words: "no banner"}, carryInput(), planNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(s2); got["P3.2"] != PieceCancelled || got["P3.1"] != PieceRunning || got["P3"] != PieceDelegated {
		t.Fatalf("states = %v", got)
	}
}

func TestAmend_RemovingTheWholeSubtreeCancelsAll(t *testing.T) {
	m, s := afterSplit(t)
	p4 := m.Pieces[1]
	p4.WaitsFor = nil
	next := Mission{Request: m.Request, Pieces: []Piece{p4}}
	_, s2, _, steps, err := Amend(m, s, Amendment{Plan: next.Render(), Words: "drop the visuals"}, carryInput(), planNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"P3", "P3.1", "P3.2"} {
		if statuses(s2)[id] != PieceCancelled {
			t.Errorf("%s is %s", id, statuses(s2)[id])
		}
	}
	if len(steps) != 1 || steps[0].Piece != "P3.1" {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestAmend_CarriesOverANestedSubtree(t *testing.T) {
	m := Mission{Request: "Rebrand acme/app.", Pieces: []Piece{
		{ID: "P3", Domain: "design", Delegate: true, Brief: "the visuals", Paths: []string{"design/**"}},
		{ID: "P4", Domain: "marketing", Factory: "feature", Brief: "landing page", Paths: []string{"site/**"}},
		{ID: "P3.1", Domain: "design", Delegate: true, Brief: "logo", Paths: []string{"design/logo/**"}},
		{ID: "P3.2", Domain: "design", Factory: "visuals", Brief: "banner", Paths: []string{"design/banner/**"}},
		{ID: "P3.1.1", Domain: "design", Factory: "visuals", Brief: "mark", Paths: []string{"design/logo/mark/**"}},
		{ID: "P3.1.2", Domain: "design", Factory: "visuals", Brief: "type", Paths: []string{"design/logo/type/**"}},
	}}
	s := State{MissionID: "m-1", Status: StatusRunning, NextWait: 1, Pieces: []PieceState{
		{ID: "P3", Status: PieceDelegated}, {ID: "P4", Status: PieceQueued}, {ID: "P3.1", Status: PieceDelegated},
		{ID: "P3.2", Status: PieceQueued}, {ID: "P3.1.1", Status: PieceQueued}, {ID: "P3.1.2", Status: PieceQueued}}}
	next := Mission{Request: m.Request, Pieces: append([]Piece(nil), m.Pieces[:4]...)}
	next.Pieces[1].Brief = "landing page, again"
	m2, s2, _, _, err := Amend(m, s, Amendment{Plan: next.Render(), Words: "w"}, carryInput(), planNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.Pieces) != 6 || statuses(s2)["P3.1.2"] != PieceQueued {
		t.Fatalf("pieces = %+v states = %v", m2.Pieces, statuses(s2))
	}
	if !hasChange(m2, "kept P3.1.1, P3.1.2 under P3.1: the amendment named no piece under it") {
		t.Fatalf("changes = %+v", m2.Changes)
	}
}

func hasChange(m Mission, what string) bool {
	for _, c := range m.Changes {
		if c.What == what {
			return true
		}
	}
	return false
}

func TestAmendPreview_SaysWhatTheAmendmentWillDo(t *testing.T) {
	m, s := afterSplit(t)
	got, err := AmendPreview(m, s, staleAmendment(), carryInput(), planNow)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"will re-brief P4", "keeps P3.1, P3.2 under P3: the plan named no piece under it"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q", got)
	}
	next := Mission{Request: m.Request, Pieces: append([]Piece(nil), m.Pieces[:3]...)}
	next.Pieces[1].WaitsFor = []string{"P3.1"}
	next.Pieces = append(next.Pieces, Piece{ID: "P5", Domain: "marketing", Factory: "campaign", Brief: "ads"})
	got, err = AmendPreview(m, s, Amendment{Plan: next.Render(), Words: "w"}, carryInput(), planNow)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"will change P4: waits_for changed", "will add P5", "will cancel P3.2 (queued)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q", got)
	}
}

func TestCheck_ParentMissingHintNamesTheWayOut(t *testing.T) {
	m, _ := afterSplit(t)
	next := Mission{Request: m.Request, Pieces: m.Pieces[1:]}
	next.Pieces[0].WaitsFor = nil
	want := "piece P3.1: its parent P3 is not in the plan (to drop P3, remove every piece under it too; if one is complete, keep P3 and what is finished and remove the rest)"
	if errs := Check(next, carryInput()); !strings.Contains(firstMessages(errs), want) {
		t.Fatalf("errs = %v", errs)
	}
}
