package mission

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func delegatedHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, Piece{ID: "P1", Domain: "product", Delegate: true, Brief: "credits platform", Paths: []string{"app/**"}})
	h.e.Settings.Limits.Depth = lim(3)
	h.e.Checks.OrchestratorHarness = "claude-code"
	return h
}

func goodPlan() SubPlan {
	return SubPlan{Reason: "api then ui", Pieces: []Piece{
		{ID: "a", Factory: "feature", Brief: "api", Paths: []string{"app/api/**"}},
		{ID: "b", Factory: "feature", Brief: "ui", Paths: []string{"app/ui/**"}, WaitsFor: []string{"a"}},
	}}
}

func TestEngine_PlansADelegatedPiece(t *testing.T) {
	h := delegatedHarness(t)
	h.orch.plans = []SubPlan{goodPlan()}
	h.tick()
	if p := h.piece("P1"); p.Status != PieceDelegated || p.Calls != 1 {
		t.Fatalf("P1 = %+v", p)
	}
	m, _ := h.store.Mission()
	for _, id := range []string{"P1.1", "P1.2"} {
		if _, ok := m.Piece(id); !ok {
			t.Fatalf("%s not in mission.md", id)
		}
		if _, ok := h.state().Piece(id); !ok {
			t.Fatalf("%s not in state", id)
		}
	}
	if h.log.has("Start") || h.log.count("Orch.Plan") != 1 || len(h.events(EvException)) != 0 {
		t.Fatalf("calls = %v", h.log.calls)
	}
	h.tick()
	if !h.log.has("Start P1.1") || h.log.has("Start P1.2") || h.log.count("Orch.Plan") != 1 {
		t.Fatalf("calls = %v", h.log.calls)
	}
}

func TestEngine_RefusedPlanIsTriedAgainWithItsReason(t *testing.T) {
	h := delegatedHarness(t)
	bad := SubPlan{Reason: "x", Pieces: []Piece{{ID: "a", Factory: "feature", Brief: "site", Paths: []string{"site/**"}}}}
	h.orch.plans = []SubPlan{bad, goodPlan()}
	h.tick()
	if p := h.piece("P1"); p.Status != PieceQueued || p.Calls != 1 || len(p.Decisions) != 1 {
		t.Fatalf("after refusal: %+v", p)
	}
	h.tick()
	if len(h.orch.planReqs) != 2 {
		t.Fatalf("calls = %v", h.log.calls)
	}
	earlier := h.orch.planReqs[1].Earlier
	if len(earlier) != 1 || earlier[0].Reason == "" || !strings.Contains(h.orch.planReqs[1].Prompt(), earlier[0].Reason) {
		t.Fatalf("earlier = %+v", earlier)
	}
	if p := h.piece("P1"); p.Status != PieceDelegated || p.Calls != 2 {
		t.Fatalf("P1 = %+v", p)
	}
}

func TestEngine_FailedPlanningCallsEndInOneWait(t *testing.T) {
	h := delegatedHarness(t)
	h.orch.planErr = errors.New("harness crashed")
	for range 3 {
		h.tick()
	}
	if n := h.log.count("Orch.Plan"); n != 3 {
		t.Fatalf("calls after three ticks = %d", n)
	}
	h.tick() // the call limit is reached: the piece waits instead of a fourth call
	p := h.piece("P1")
	if p.Calls != 3 || p.Status != PieceWaiting || p.Wait == nil || p.Wait.Kind != WaitPlan {
		t.Fatalf("P1 = %+v", p)
	}
	for range 5 {
		h.tick()
	}
	if n := h.log.count("Orch.Plan"); n != 3 {
		t.Fatalf("calls after five more ticks = %d", n)
	}
	if got := len(h.events(EvWaitingOnPrincipal)); got != 1 {
		t.Fatalf("escalations = %d", got)
	}
}

func TestEngine_StopDuringPlanningCountsNothing(t *testing.T) {
	h := delegatedHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.e.Orch = cancelOrch{cancel}
	if _, err := h.e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	p := h.piece("P1")
	if p.Status != PieceQueued || p.Calls != 0 || len(p.Decisions) != 0 {
		t.Fatalf("P1 = %+v", p)
	}
	if len(h.events(EvDecision)) != 0 || len(h.events(EvException)) != 0 || len(h.events(EvEscalated)) != 0 {
		t.Fatalf("kinds = %v", h.kinds())
	}
}

func TestEngine_PlanQuestionWaitsOnThePrincipal(t *testing.T) {
	h := delegatedHarness(t)
	h.orch.plans = []SubPlan{{Question: "which payment provider?", Reason: "needs a decision"}, goodPlan()}
	h.tick()
	p := h.piece("P1")
	if p.Status != PieceWaiting || p.Wait == nil || p.Wait.Kind != WaitPlan || p.Wait.Question != "which payment provider?" || p.Calls != 1 {
		t.Fatalf("P1 = %+v", p)
	}
	h.tick()
	if h.log.count("Orch.Plan") != 1 {
		t.Fatalf("asked again while waiting: %v", h.log.calls)
	}
	if err := h.store.Drop("answer", Answer{Wait: p.Wait.ID, Do: "retry", Words: "stripe"}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if got := h.piece("P1"); got.Status != PieceDelegated {
		t.Fatalf("after retry: %+v calls %v", got, h.log.calls)
	}
	if !slices.Contains(h.log.calls, "Orch.Plan P1") || h.log.count("Orch.Plan") != 2 {
		t.Fatalf("calls = %v", h.log.calls)
	}
}
