package mission

import (
	"reflect"
	"strings"
	"testing"
)

const words = "make it $15 and add the push-notification piece"

func extraPiece(id string) Piece {
	return Piece{ID: id, Domain: "marketing", Factory: "campaign", Brief: "push notifications"}
}

// amendWith applies edit to a copy of acme()'s pieces and amends exState().
func amendWith(t *testing.T, s State, edit func(*Mission)) (Mission, State, []Action, []RunStep, error) {
	t.Helper()
	next := acme()
	edit(&next)
	return Amend(acme(), s, Amendment{Plan: next.Render(), Words: words}, exInput(), t0)
}

func TestAmend_AddsPieceAndRebriefsQueued(t *testing.T) {
	m, s, as, steps, err := amendWith(t, exState(), func(n *Mission) {
		n.Pieces[2].Brief = "landing page at $15"
		n.Pieces = append(n.Pieces, extraPiece("P7"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Fatalf("steps = %v", steps)
	}
	if m.Pieces[2].Brief != "landing page at $15" || m.Pieces[5].ID != "P7" {
		t.Fatalf("pieces = %+v", m.Pieces)
	}
	if m.Request != acme().Request {
		t.Fatalf("request changed: %q", m.Request)
	}
	if len(m.Changes) != 2 {
		t.Fatalf("changes = %+v", m.Changes)
	}
	for _, c := range m.Changes {
		if c.By != "principal" || c.Why != words || !c.Time.Equal(t0) {
			t.Fatalf("change = %+v", c)
		}
	}
	if m.Changes[0].What != "re-briefed P4" || m.Changes[1].What != "added P7" {
		t.Fatalf("changes = %+v", m.Changes)
	}
	last := s.Pieces[len(s.Pieces)-1]
	if last.ID != "P7" || last.Status != PieceQueued || last.Added || s.Additions != 0 {
		t.Fatalf("state = %+v additions %d", last, s.Additions)
	}
	if got := recordKinds(as); !reflect.DeepEqual(got, []string{"amendment", "rebrief", "piece-added"}) {
		t.Fatalf("records = %v", got)
	}
	n := of[Notify](as)
	if len(n) != 1 || n[0].Body != "amendment applied: 1 added, 1 re-briefed" {
		t.Fatalf("notify = %+v", n)
	}
}

func TestAmend_OtherFieldChangeIsLogged(t *testing.T) {
	m, _, as, _, err := amendWith(t, exState(), func(n *Mission) { n.Pieces[2].WaitsFor = []string{"P1"} })
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Changes) != 1 || m.Changes[0].What != "P4: waits_for changed" {
		t.Fatalf("changes = %+v", m.Changes)
	}
	if got := recordKinds(as); !reflect.DeepEqual(got, []string{"amendment"}) {
		t.Fatalf("records = %v", got)
	}
}

func TestAmend_ChangingStartedPieceIsError(t *testing.T) {
	_, _, _, _, err := amendWith(t, exState(), func(n *Mission) { n.Pieces[0].Brief = "different" })
	if err == nil || !strings.Contains(err.Error(), "P1") || !strings.Contains(err.Error(), "running") {
		t.Fatalf("err = %v", err)
	}
}

func TestAmend_RemovingRunningPieceStopsIt(t *testing.T) {
	// P2 waits for P1, so dropping P1 alone must fail the plan's check.
	if _, _, _, _, err := amendWith(t, exState(), func(n *Mission) { n.Pieces = n.Pieces[1:] }); err == nil {
		t.Fatal("expected check failure")
	}
	m, s, as, steps, err := amendWith(t, exState(), func(n *Mission) {
		n.Pieces = []Piece{n.Pieces[2], n.Pieces[3]}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := piece(t, s, "P1"); got.Status != PieceCancelled {
		t.Fatalf("P1 = %+v", got)
	}
	if len(steps) != 1 || steps[0] != (RunStep{Piece: "P1", Kind: "stop"}) {
		t.Fatalf("steps = %v", steps)
	}
	if c := of[Cleanup](as); len(c) != 1 || c[0].Piece != "P1" || c[0].Force {
		t.Fatalf("cleanups = %v", c)
	}
	if piece(t, s, "P2").Status != PieceCancelled || len(m.Pieces) != 2 || len(s.Pieces) != 5 {
		t.Fatalf("state = %+v", s.Pieces)
	}
	if !hasKind(as, "amendment") || !hasKind(as, "piece-cancelled") {
		t.Fatalf("records = %v", recordKinds(as))
	}
}

func TestAmend_RemovingPROpenPieceKeepsPR(t *testing.T) {
	s := exState()
	s.Pieces[0] = PieceState{ID: "P1", Status: PiecePROpen, PR: 7}
	_, s, as, steps, err := amendWith(t, s, func(n *Mission) { n.Pieces = []Piece{n.Pieces[2], n.Pieces[3]} })
	if err != nil {
		t.Fatal(err)
	}
	if piece(t, s, "P1").Status != PieceCancelled || len(steps) != 0 {
		t.Fatalf("state %+v steps %v", s.Pieces[0], steps)
	}
	found := false
	for _, r := range of[Record](as) {
		found = found || (r.Event.Kind == "piece-cancelled" && r.Event.Piece == "P1" && strings.Contains(r.Event.Msg, "PR #7 stays open"))
	}
	if !found {
		t.Fatalf("no PR-stays-open record in %v", as)
	}
}

func TestAmend_RemovingCompletePieceIsError(t *testing.T) {
	s := exState()
	s.Pieces[0] = PieceState{ID: "P1", Status: PieceComplete}
	_, _, _, _, err := amendWith(t, s, func(n *Mission) { n.Pieces = []Piece{n.Pieces[2], n.Pieces[3]} })
	if err == nil || !strings.Contains(err.Error(), "P1") || !strings.Contains(err.Error(), "complete") {
		t.Fatalf("err = %v", err)
	}
}

func TestAmend_CancelledPieceMayBeMissingOrPresent(t *testing.T) {
	s := exState()
	s.Pieces[3] = PieceState{ID: "P5", Status: PieceCancelled}
	for _, drop := range []bool{false, true} {
		_, got, _, _, err := amendWith(t, s, func(n *Mission) {
			n.Pieces[2].Brief = "new"
			if drop {
				n.Pieces = append(n.Pieces[:3], n.Pieces[4:]...)
				n.Pieces[3].WaitsFor = []string{"P1", "P2"}
			}
		})
		if err != nil {
			t.Fatalf("drop=%v: %v", drop, err)
		}
		if p := piece(t, got, "P5"); !reflect.DeepEqual(p, s.Pieces[3]) {
			t.Fatalf("P5 = %+v", p)
		}
	}
}

func TestAmend_RemovedIdCannotBeReused(t *testing.T) {
	m := acme()
	m.Pieces = append(m.Pieces[:3], m.Pieces[4:]...) // P5 gone from the mission, kept in state
	m.Pieces[3].WaitsFor = []string{"P1", "P2"}
	s := exState()
	s.Pieces[3] = PieceState{ID: "P5", Status: PieceCancelled}
	next := m
	next.Pieces = append(append([]Piece(nil), m.Pieces...), extraPiece("P5"))
	_, _, _, _, err := Amend(m, s, Amendment{Plan: next.Render(), Words: words}, exInput(), t0)
	if err == nil || !strings.Contains(err.Error(), "piece P5 was used before in this mission") {
		t.Fatalf("err = %v", err)
	}
}

func TestAmend_RequestMustNotChange(t *testing.T) {
	_, _, _, _, err := amendWith(t, exState(), func(n *Mission) { n.Request = "something else" })
	if err == nil || !strings.Contains(err.Error(), "an amendment can't change the request; start a new mission for a different job") {
		t.Fatalf("err = %v", err)
	}
	// trailing whitespace is not a change
	_, _, _, _, err = amendWith(t, exState(), func(n *Mission) {
		n.Request += "\n\n"
		n.Pieces[2].Brief = "x"
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAmend_NothingChangedAndDoneAreErrors(t *testing.T) {
	_, _, _, _, err := amendWith(t, exState(), func(*Mission) {})
	if err == nil || !strings.Contains(err.Error(), "the new plan is the same as the current one") {
		t.Fatalf("err = %v", err)
	}
	s := exState()
	s.Status = StatusDone
	_, _, _, _, err = amendWith(t, s, func(n *Mission) { n.Pieces[2].Brief = "x" })
	if err == nil || !strings.Contains(err.Error(), "done") {
		t.Fatalf("err = %v", err)
	}
}

func TestAmend_BadPlanIsError(t *testing.T) {
	_, _, _, _, err := Amend(acme(), exState(), Amendment{Plan: []byte("nope"), Words: words}, exInput(), t0)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestAmend_FailedCheckChangesNothing(t *testing.T) {
	m, s := acme(), exState()
	bad := extraPiece("P7")
	bad.Domain = "nowhere"
	next := acme()
	next.Pieces = append(next.Pieces, bad)
	gm, gs, as, steps, err := Amend(m, s, Amendment{Plan: next.Render(), Words: words}, exInput(), t0)
	if err == nil || !strings.Contains(err.Error(), "nowhere") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(gm, m) || !reflect.DeepEqual(gs, s) || as != nil || steps != nil {
		t.Fatal("inputs not returned unchanged")
	}
}

func TestAmend_DoesNotModifyInputs(t *testing.T) {
	m, s := acme(), exState()
	m.Changes = []Change{{Time: t0, By: "orchestrator", What: "w", Why: "y"}}
	m0, s0 := acme(), exState()
	m0.Changes = append([]Change(nil), m.Changes...)
	next := acme()
	next.Pieces = []Piece{next.Pieces[2], next.Pieces[3]}
	next.Pieces[0].Brief = "changed"
	next.Pieces = append(next.Pieces, extraPiece("P7"))
	if _, _, _, _, err := Amend(m, s, Amendment{Plan: next.Render(), Words: words}, exInput(), t0); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m, m0) || !reflect.DeepEqual(s, s0) {
		t.Fatal("successful Amend modified its inputs")
	}
	bad := acme()
	bad.Pieces[0].Brief = "x"
	_, _, _, _, err := Amend(m, s, Amendment{Plan: bad.Render(), Words: words}, exInput(), t0)
	if err == nil || !reflect.DeepEqual(m, m0) || !reflect.DeepEqual(s, s0) {
		t.Fatal("failing Amend modified its inputs")
	}
}

func TestAmend_RecordCutsWords(t *testing.T) {
	long := strings.Repeat("é", 600)
	next := acme()
	next.Pieces[2].Brief = "x"
	_, _, as, _, err := Amend(acme(), exState(), Amendment{Plan: next.Render(), Words: long}, exInput(), t0)
	if err != nil {
		t.Fatal(err)
	}
	r := of[Record](as)[0].Event
	if r.Kind != "amendment" || len([]rune(r.Msg)) != 500 {
		t.Fatalf("record = %+v", r)
	}
}

// P1 was cancelled, so P2 (which waits for it) is parked with a question that
// went back to queued. Amending P2 is allowed: it never started.
func TestAmend_WaitingBackToQueuedPieceCanBeChanged(t *testing.T) {
	s := exState()
	s.Pieces[0] = PieceState{ID: "P1", Status: PieceCancelled}
	s.Pieces[1] = PieceState{ID: "P2", Status: PieceWaiting,
		Wait: &Wait{ID: "w1", Kind: WaitQuestion, Question: "P2 waits for P1, which was cancelled", Since: t0, Back: PieceQueued}}
	m, s2, _, _, err := amendWith(t, s, func(n *Mission) { n.Pieces[1].WaitsFor = nil })
	if err != nil {
		t.Fatal(err)
	}
	if p := piece(t, s2, "P2"); p.Status != PieceQueued || p.Wait != nil {
		t.Fatalf("P2 = %+v", p)
	}
	if got := piece(t, s, "P2"); got.Status != PieceWaiting || got.Wait == nil {
		t.Fatal("Amend modified its argument")
	}
	if len(m.Changes) != 1 || m.Changes[0].What != "P2: waits_for changed" {
		t.Fatalf("changes = %+v", m.Changes)
	}
	_, as := Decide(m, s2, obs(t0), limits(3, 3), 0)
	started := false
	for _, a := range of[StartPiece](as) {
		started = started || a.Piece == "P2"
	}
	if !started {
		t.Fatalf("P2 should start on the next Decide: %+v", as)
	}
}

func TestAmend_WaitingMidRunPieceStillCantBeChanged(t *testing.T) {
	s := exState()
	s.Pieces[1] = PieceState{ID: "P2", Status: PieceWaiting, RunID: "r2",
		Wait: &Wait{ID: "w1", Kind: WaitApproval, Since: t0, Back: PieceRunning}}
	if _, _, _, _, err := amendWith(t, s, func(n *Mission) { n.Pieces[1].Brief = "other" }); err == nil {
		t.Fatal("expected an error")
	}
}
