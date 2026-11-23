package mission

import (
	"reflect"
	"testing"
)

// mission.md was saved with the sub-pieces and state.json was not (the
// engine died between the two writes): the next tick plans P1 afresh.
func TestEngine_PlansAgainAfterASubPlanSavedOnlyInTheMission(t *testing.T) {
	h := delegatedHarness(t)
	m, _ := h.store.Mission()
	s := h.state()
	in := h.e.Checks
	in.Limits = h.e.Settings.For(s.Repo)
	m2, _, _ := ApplySubPlan(m, s, "P1", goodPlan(), in, planNow)
	if len(m2.Pieces) != 3 {
		t.Fatalf("setup: %+v", m2.Pieces)
	}
	if err := h.store.SaveMission(m2); err != nil { // the first of the two writes
		t.Fatal(err)
	}
	h.orch.plans = []SubPlan{goodPlan()}
	h.tick()
	if p := h.piece("P1"); p.Status != PieceDelegated || p.Calls != 1 || p.Wait != nil {
		t.Fatalf("P1 = %+v", p)
	}
	after, _ := h.store.Mission()
	var ids []string
	for _, p := range after.Pieces {
		ids = append(ids, p.ID)
	}
	if !reflect.DeepEqual(ids, []string{"P1", "P1.1", "P1.2"}) {
		t.Fatalf("pieces = %v", ids)
	}
	for _, id := range []string{"P1.1", "P1.2"} {
		if _, ok := h.state().Piece(id); !ok {
			t.Fatalf("%s has no state", id)
		}
	}
}

// A piece under the parent that has a state is never dropped.
func TestApplySubPlan_KeepsASubPieceThatHasAState(t *testing.T) {
	m, s, in := scopeFixture() // P3 delegated, P3.1 running, P3.2 queued
	m2, s2, acts := ApplySubPlan(m, s, "P3", SubPlan{Reason: "r", Pieces: []Piece{{ID: "a", Factory: "visuals", Brief: "b"}}}, in, t0)
	if !reflect.DeepEqual(m2, m) || !reflect.DeepEqual(s2, s) || len(acts) != 0 {
		t.Fatalf("a delegated parent was planned again: %+v", m2.Pieces)
	}
	edit(&s, "P3", func(p *PieceState) { p.Status = PieceQueued })
	m3, _, _ := ApplySubPlan(m, s, "P3", SubPlan{Reason: "r", Pieces: []Piece{{ID: "a", Factory: "visuals", Brief: "b", Paths: []string{"design/c/**"}}}}, in, t0)
	for _, id := range []string{"P3.1", "P3.2"} {
		if _, ok := m3.Piece(id); !ok {
			t.Errorf("%s was dropped: %+v", id, m3.Pieces)
		}
	}
}

// A delegated piece with no piece under it in the state goes back to the
// queue once, with a note.
func TestDecide_ADelegatedPieceWithNothingUnderItIsPlannedAgain(t *testing.T) {
	m := Mission{Request: "r", Pieces: []Piece{{ID: "P3", Domain: "design", Delegate: true, Brief: "b"}}}
	s := State{MissionID: "m-1", Status: StatusRunning, NextWait: 1, Pieces: []PieceState{{ID: "P3", Status: PieceDelegated}}}
	l := Limits{Concurrency: lim(3), OrchestratorCalls: lim(3)}
	s1, acts := Decide(m, s, Obs{Now: planNow}, l, 0)
	notes := eventsBy(acts, EvNote)
	if p, _ := s1.Piece("P3"); p.Status != PieceQueued || len(notes) != 1 || notes[0].Msg != "P3 has no pieces under it; it will be planned again" {
		t.Fatalf("P3 = %+v notes = %+v", p, notes)
	}
	_, acts = Decide(m, s1, Obs{Now: planNow}, l, 0)
	if got := of[PlanPieces](acts); len(got) != 1 || got[0].Piece != "P3" || len(eventsBy(acts, EvNote)) != 0 {
		t.Fatalf("second Decide: %+v", acts)
	}
}
