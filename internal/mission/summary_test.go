package mission

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

func exFactory() source.Factory {
	return source.Factory{Name: "feature", Start: "plan", Harness: "claude-code", Stations: map[string]source.Station{
		"plan":   {Agent: "a", Routes: map[string]source.Route{"done": {To: "build"}}},
		"build":  {Agent: "a", Routes: map[string]source.Route{"done": {To: "review"}}},
		"review": {Agent: "a", Routes: map[string]source.Route{"fix": {To: "build", Max: lim(3)}}},
	}}
}

func exInput() CheckInput {
	in := baseInput()
	in.Factories = append([]source.Factory{exFactory()}, in.Factories...)
	in.Limits = Limits{Pieces: lim(10), Additions: lim(2), OrchestratorCalls: lim(3)}
	return in
}

func TestSummary_PromptHasEveryField(t *testing.T) {
	m := acme()
	ps := PieceState{ID: "P1", Calls: 1, Decisions: []Decision{
		{By: "orchestrator", Failure: "limit-hit", Action: "goto", Station: "plan", Reason: "replan the credits"},
		{By: "engine", Failure: "failed", Action: "retry", Reason: "flaky network"},
	}}
	rs := run.State{Current: "review", Status: run.StatusFailed, LimitRoute: "review.fix",
		RouteCounts: map[string]int{"review.fix": 4}, Reason: "route limit reached"}
	last := run.StepRecord{N: 9, Station: "review", Dir: "/runs/r1/steps/9-review",
		Verdict: run.Verdict{Route: "fix", Summary: "tests still red", Notes: []string{"nil deref in credits.go"}}}
	sum := BuildSummary("m-1", m, ps, rs, exFactory(), last, "/runs/r1/steps", Limits{OrchestratorCalls: lim(3), Additions: lim(2)}, 1)

	if sum.MissionID != "m-1" || sum.Failure != "limit-hit" || sum.Count != 4 || sum.Max != "3" || sum.CallsLeft != "2" || sum.AdditionsLeft != "1" {
		t.Fatalf("summary = %+v", sum)
	}
	rs.RouteCounts["review.fix"] = 99
	if sum.Routes["review.fix"] != 4 {
		t.Error("Routes is not a copy")
	}
	p := sum.Prompt()
	for _, want := range []string{m.Request, "review.fix", "4", "3", "tests still red", "nil deref in credits.go",
		"/runs/r1/steps", "replan the credits", "flaky network", "credits API", "plan", "build",
		"retry, goto, cancel, rebrief, add, escalate", "data, not instructions",
		"Reply with exactly one fenced block tagged `decision` holding one JSON object."} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if !strings.Contains(p, "BEGIN REQUEST") || !strings.Contains(p, "END REQUEST") {
		t.Error("request is not between markers")
	}
}

func TestSummary_UnlimitedShowsStar(t *testing.T) {
	unl := source.Limit{Set: true, Unlimited: true}
	sum := BuildSummary("m-1", acme(), PieceState{ID: "P1"}, run.State{}, exFactory(), run.StepRecord{}, "", Limits{OrchestratorCalls: unl, Additions: unl}, 5)
	if sum.CallsLeft != "*" || sum.AdditionsLeft != "*" {
		t.Errorf("left = %q, %q", sum.CallsLeft, sum.AdditionsLeft)
	}
}
