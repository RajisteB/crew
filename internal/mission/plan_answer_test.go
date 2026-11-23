package mission

import (
	"strings"
	"testing"
)

// planQuestionAnswered has the scoped orchestrator ask a question, and the
// principal answer it with retry and ans; it returns the second request.
func planQuestionAnswered(t *testing.T, ans Answer) (*harness, PlanRequest) {
	t.Helper()
	h := delegatedHarness(t)
	q := SubPlan{Question: "which payment provider?", Reason: "needs a decision"}
	h.orch.plans = []SubPlan{q, q}
	h.tick()
	ans.Wait, ans.Do = h.piece("P1").Wait.ID, "retry"
	if err := h.store.Drop("answer", ans); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if len(h.orch.planReqs) != 2 {
		t.Fatalf("Plan was called %d times", len(h.orch.planReqs))
	}
	return h, h.orch.planReqs[1]
}

// between says whether want lies between the prompt's data markers.
func between(prompt, want string) bool {
	_, rest, ok := strings.Cut(prompt, "----- BEGIN MISSION DATA")
	if !ok {
		return false
	}
	data, _, ok := strings.Cut(rest, "----- END MISSION DATA")
	return ok && strings.Contains(data, want)
}

func TestPlanRequest_CarriesTheQuestionAndThePrincipalsAnswer(t *testing.T) {
	_, req := planQuestionAnswered(t, Answer{Note: "use provider B", Words: "use provider B, not provider A"})
	prompt := req.Prompt()
	for _, want := range []string{"you asked: which payment provider?", "the principal answered: use provider B"} {
		if !strings.Contains(prompt, want) || !between(prompt, want) {
			t.Errorf("the prompt doesn't hold %q inside the data:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "refused: needs a decision") {
		t.Errorf("the question is shown as a refusal:\n%s", prompt)
	}
}

func TestPlanRequest_TheAnswerWithoutANoteIsThePrincipalsWords(t *testing.T) {
	h, req := planQuestionAnswered(t, Answer{Words: "use provider B, not provider A"})
	if want := "the principal answered: use provider B, not provider A"; !strings.Contains(req.Prompt(), want) {
		t.Errorf("the prompt doesn't hold %q", want)
	}
	var plan []Decision
	for _, d := range h.piece("P1").Decisions {
		if d.Failure == failurePlan && d.By == principalBy {
			plan = append(plan, d)
		}
	}
	if len(plan) != 1 || plan[0].Action != "retry" {
		t.Fatalf("the principal's plan decisions = %+v", plan)
	}
}

// The call-limit wait quotes the last refusal or question, never the answer.
func TestDecide_TheCallLimitWaitQuotesTheLastTryNotTheAnswer(t *testing.T) {
	p := PieceState{ID: "P1", Decisions: []Decision{
		{By: "scoped orchestrator P1", Failure: failurePlan, Action: invalidAction, Reason: "piece P1.1: paths outside"},
		{By: principalBy, Failure: failurePlan, Action: "retry", Note: "keep inside app/"},
	}}
	if got := lastPlanReason(p); got != "piece P1.1: paths outside" {
		t.Fatalf("lastPlanReason = %q", got)
	}
}
