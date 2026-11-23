package mission

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

// delegating is a mission whose pieces with the given ids delegate.
func delegating(ids []string, delegate ...string) Mission {
	m := mis(nil, ids...)
	for i := range m.Pieces {
		for _, d := range delegate {
			if m.Pieces[i].ID == d {
				m.Pieces[i].Delegate = true
			}
		}
	}
	return m
}

func planned(as []Action) []string {
	var ids []string
	for _, a := range of[PlanPieces](as) {
		ids = append(ids, a.Piece)
	}
	return ids
}

func TestDecide_ReadyDelegatedPieceIsPlanned(t *testing.T) {
	m := delegating([]string{"P1", "P2"}, "P1")
	_, as := Decide(m, st(m, PieceQueued, PieceQueued), obs(t0), limits(5, 3), 0)
	if got := planned(as); len(got) != 1 || got[0] != "P1" {
		t.Fatalf("planned = %v", got)
	}
	if got := of[StartPiece](as); len(got) != 1 || got[0].Piece != "P2" {
		t.Fatalf("started = %+v", got)
	}

	// no run slot is free, yet it is still planned
	m2 := delegating([]string{"P1", "P2"}, "P1")
	m2.Pieces = append(m2.Pieces, Piece{ID: "P3", Factory: "f"})
	o := obs(t0)
	o.Runs["P3"] = RunObs{State: run.State{Status: run.StatusRunning}, Live: true}
	_, as = Decide(m2, st(m2, PieceQueued, PieceQueued, PieceRunning), o, limits(1, 3), 0)
	if got := planned(as); len(got) != 1 || got[0] != "P1" {
		t.Fatalf("planned with no slot = %v", got)
	}
	if got := of[StartPiece](as); len(got) != 0 {
		t.Fatalf("started = %+v", got)
	}
}

func TestDecide_DelegatedPieceWaitsForItsDependency(t *testing.T) {
	m := delegating([]string{"P1", "P2"}, "P1")
	m.Pieces[0].WaitsFor = []string{"P2"}
	o := obs(t0)
	o.Runs["P2"] = RunObs{State: run.State{Status: run.StatusRunning}, Live: true}
	_, as := Decide(m, st(m, PieceQueued, PieceRunning), o, limits(5, 3), 0)
	if got := planned(as); len(got) != 0 {
		t.Fatalf("planned = %v", got)
	}
}

func TestDecide_DelegatedPieceKeptApartFromADelegatedPieceWaits(t *testing.T) {
	m := delegating([]string{"P1", "P2"}, "P1")
	m.Pieces[1].Apart = []string{"P1"}
	s := st(m, PieceDelegated, PieceQueued)
	s.Pieces = append(s.Pieces, PieceState{ID: "P1.1", Status: PieceQueued})
	m.Pieces = append(m.Pieces, Piece{ID: "P1.1", Factory: "f"})
	_, as := Decide(m, s, obs(t0), limits(5, 3), 0)
	for _, a := range of[StartPiece](as) {
		if a.Piece == "P2" {
			t.Fatalf("P2 started while apart from a delegated piece: %+v", as)
		}
	}
}

func TestDecide_PlanningStopsAtTheCallLimit(t *testing.T) {
	m := delegating([]string{"P1"}, "P1")
	s := st(m, PieceQueued)
	s.Pieces[0].Calls = 2
	s.Pieces[0].Decisions = []Decision{
		{By: "orchestrator", Failure: failurePlan, Action: invalidAction, Reason: "first"},
		{By: "orchestrator", Failure: failurePlan, Action: invalidAction, Reason: "paths: a/** is outside"},
	}
	ns, as := Decide(m, s, obs(t0), limits(5, 2), 0)
	if len(planned(as)) != 0 {
		t.Fatalf("planned = %v", planned(as))
	}
	p := piece(t, ns, "P1")
	want := "P1 could not be planned in 2 orchestrator calls: paths: a/** is outside"
	if p.Wait == nil || p.Wait.Kind != WaitPlan || p.Wait.Question != want || p.Wait.Back != PieceQueued || p.Status != PieceWaiting {
		t.Fatalf("piece = %+v wait = %+v", p, p.Wait)
	}
	if rec := budgetRecords(as); len(rec) != 1 || rec[0].Piece != "P1" || rec[0].Count != 2 {
		t.Fatalf("budget records = %+v", rec)
	}
	if n := len(of[Notify](as)); n != 1 {
		t.Fatalf("%d notifications", n)
	}
	_, as2 := Decide(m, ns, obs(t0), limits(5, 2), 0)
	if len(as2) != 0 {
		t.Fatalf("second decide: %+v", as2)
	}
}

func TestDecide_PlanningWithNoCallAllowedHasNoReason(t *testing.T) {
	m := delegating([]string{"P1"}, "P1")
	ns, _ := Decide(m, st(m, PieceQueued), obs(t0), limits(5, 0), 0)
	p := piece(t, ns, "P1")
	if p.Wait == nil || p.Wait.Question != "P1 could not be planned in 0 orchestrator calls" {
		t.Fatalf("wait = %+v", p.Wait)
	}
}

func subState(m Mission, parent PieceStatus, kids ...PieceStatus) (Mission, State) {
	m.Pieces = append(m.Pieces[:0:0], m.Pieces...)
	s := State{MissionID: "m-1", Status: StatusRunning, NextWait: 1}
	for i, p := range m.Pieces {
		status := PieceQueued
		if i == 0 {
			status = parent
		}
		s.Pieces = append(s.Pieces, PieceState{ID: p.ID, Status: status})
	}
	for i, k := range kids {
		id := "P1." + string(rune('1'+i))
		m.Pieces = append(m.Pieces, Piece{ID: id, Factory: "f"})
		s.Pieces = append(s.Pieces, PieceState{ID: id, Status: k})
	}
	return m, s
}

func TestDecide_DelegatedPieceFinishesFromItsSubPieces(t *testing.T) {
	cases := []struct {
		name string
		kids []PieceStatus
		want PieceStatus
		ev   string
	}{
		{"complete and complete", []PieceStatus{PieceComplete, PieceComplete}, PieceComplete, EvPieceComplete},
		{"complete and cancelled", []PieceStatus{PieceComplete, PieceCancelled}, PieceComplete, EvPieceComplete},
		{"cancelled and cancelled", []PieceStatus{PieceCancelled, PieceCancelled}, PieceCancelled, EvPieceCancelled},
		{"complete and running", []PieceStatus{PieceComplete, PieceRunning}, PieceDelegated, ""},
	}
	for _, c := range cases {
		m, s := subState(delegating([]string{"P1"}, "P1"), PieceDelegated, c.kids...)
		o := obs(t0)
		for i, k := range c.kids {
			if k == PieceRunning {
				o.Runs["P1."+string(rune('1'+i))] = RunObs{State: run.State{Status: run.StatusRunning}, Live: true}
			}
		}
		ns, as := Decide(m, s, o, limits(5, 3), 0)
		if got := piece(t, ns, "P1").Status; got != c.want {
			t.Errorf("%s: status = %s", c.name, got)
		}
		if c.ev != "" && !hasKind(as, c.ev) {
			t.Errorf("%s: events = %v", c.name, recordKinds(as))
		}
		if len(of[Cleanup](as))+len(of[Merge](as))+len(of[CollectDeliverable](as))+len(of[StartPiece](as)) != 0 {
			t.Errorf("%s: run steps for a piece with no run: %+v", c.name, as)
		}
	}
}

func TestDecide_CancelledDelegatedPieceEventText(t *testing.T) {
	m, s := subState(delegating([]string{"P1"}, "P1"), PieceDelegated, PieceCancelled)
	_, as := Decide(m, s, obs(t0), limits(5, 3), 0)
	for _, r := range of[Record](as) {
		if r.Event.Kind == EvPieceCancelled {
			if r.Event.Piece != "P1" || r.Event.By != "engine" || r.Event.Msg != "every piece under it was cancelled" {
				t.Fatalf("event = %+v", r.Event)
			}
			return
		}
	}
	t.Fatalf("no piece-cancelled event: %+v", as)
}

func TestDecide_CancelledDelegatedPieceAsksTheOneWaitingForIt(t *testing.T) {
	m, s := subState(delegating([]string{"P1", "P2"}, "P1"), PieceDelegated, PieceCancelled, PieceCancelled)
	m.Pieces[1].WaitsFor = []string{"P1"}
	s.Pieces[1].Status = PieceQueued
	// P2 sits before the sub-pieces in the pieces list built by st()
	ns, _ := Decide(m, s, obs(t0), limits(5, 3), 0)
	p := piece(t, ns, "P2")
	if p.Wait == nil || !strings.Contains(p.Wait.Question, "P1, which was cancelled") {
		t.Fatalf("P2 = %+v", p)
	}
}

func TestDecide_NestedDelegatedPiecesFinishInOneCall(t *testing.T) {
	m := delegating([]string{"P3"}, "P3")
	m.Pieces[0].Delegate = true
	m.Pieces = append(m.Pieces, Piece{ID: "P3.1", Factory: "f", Delegate: true}, Piece{ID: "P3.1.1", Factory: "f"}, Piece{ID: "P3.2", Factory: "f"})
	s := State{MissionID: "m-1", Status: StatusRunning, NextWait: 1, Pieces: []PieceState{
		{ID: "P3", Status: PieceDelegated},
		{ID: "P3.1", Status: PieceDelegated},
		{ID: "P3.1.1", Status: PieceComplete},
		{ID: "P3.2", Status: PieceComplete},
	}}
	ns, as := Decide(m, s, obs(t0), limits(5, 3), 0)
	if piece(t, ns, "P3.1").Status != PieceComplete || piece(t, ns, "P3").Status != PieceComplete {
		t.Fatalf("state = %+v", ns.Pieces)
	}
	if ns.Status != StatusDone || !hasKind(as, EvMissionDone) {
		t.Fatalf("mission did not finish: %v", ns.Status)
	}
}

func TestDecide_PieceWaitingForADelegatedPieceStartsWhenItFinishes(t *testing.T) {
	m, s := subState(delegating([]string{"P1", "P2"}, "P1"), PieceDelegated, PieceComplete)
	m.Pieces[1].WaitsFor = []string{"P1"}
	s.Pieces[1].Status = PieceQueued
	_, as := Decide(m, s, obs(t0), limits(5, 3), 0)
	if got := of[StartPiece](as); len(got) != 1 || got[0].Piece != "P2" {
		t.Fatalf("started = %+v", got)
	}
}

func TestDecide_DelegatedPieceWithNoSubPiecesIsLeftAlone(t *testing.T) {
	m := delegating([]string{"P1"}, "P1")
	ns, as := Decide(m, st(m, PieceDelegated), obs(t0), limits(5, 3), 0)
	if piece(t, ns, "P1").Status != PieceDelegated || len(as) != 0 || ns.Status != StatusRunning {
		t.Fatalf("status %s actions %+v", piece(t, ns, "P1").Status, as)
	}
}

func TestDecide_DelegatedPieceIgnoresOtherMissions(t *testing.T) {
	m := delegating([]string{"P1"}, "P1")
	s := st(m, PieceQueued)
	s.Pieces[0].Behind = &Blocker{Mission: "m-2", Piece: "P9"}
	o := obs(t0)
	o.Behind = map[string]Blocker{"P1": {Mission: "m-2", Piece: "P9"}}
	o.ElsewhereUnknown = true
	ns, as := Decide(m, s, o, limits(5, 3), 0)
	if got := planned(as); len(got) != 1 {
		t.Fatalf("planned = %v", got)
	}
	if piece(t, ns, "P1").Behind != nil {
		t.Fatal("Behind was kept")
	}
}

func TestDecide_HoldDropsPlanning(t *testing.T) {
	m := delegating([]string{"P1"}, "P1")
	s := st(m, PieceQueued)
	s.Pieces[0].Steps = 6
	_, as := Decide(m, s, obs(t0), stepLimits(lim(6)), 0)
	if got := planned(as); len(got) != 0 {
		t.Fatalf("planned during hold: %v", got)
	}
}

func TestHoldingRunsAndClaims_SkipDelegatedPieces(t *testing.T) {
	s := State{Pieces: []PieceState{{ID: "P1", Status: PieceDelegated}, {ID: "P1.1", Status: PieceRunning}}}
	if got := HoldingRuns(s); got != 1 {
		t.Fatalf("HoldingRuns = %d", got)
	}
	if holdsPaths(s.Pieces[0]) {
		t.Fatal("a delegated piece holds paths")
	}
	if !holdsPaths(s.Pieces[1]) {
		t.Fatal("a running piece should hold paths")
	}
}

func TestLane_Delegated(t *testing.T) {
	if got := Lane(PieceState{Status: PieceDelegated}); got != LaneInProgress {
		t.Fatalf("Lane = %q", got)
	}
}

func TestStartedPieces_DelegatedIsSettled(t *testing.T) {
	got := StartedPieces(State{Pieces: []PieceState{{ID: "P1", Status: PieceDelegated}}})
	if !got["P1"] {
		t.Fatalf("settled = %v", got)
	}
}
