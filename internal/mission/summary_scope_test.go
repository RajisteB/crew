package mission

import (
	"os"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

func promptFixture() Summary {
	m := acme()
	ps := PieceState{ID: "P1", Calls: 1}
	rs := run.State{Current: "review", Status: run.StatusFailed, LimitRoute: "review.fix",
		RouteCounts: map[string]int{"review.fix": 4}, Reason: "route limit reached"}
	last := run.StepRecord{N: 9, Station: "review", Verdict: run.Verdict{Route: "fix", Summary: "tests still red", Notes: []string{"nil deref"}}}
	return BuildSummary("m-1", m, ps, rs, exFactory(), last, "/runs/r1/steps", Limits{OrchestratorCalls: lim(3), Additions: lim(2)}, 1, 0)
}

// The unscoped prompt is pinned byte for byte. To refresh it on purpose:
// CREW_GOLDEN=1 go test -run TestSummaryPrompt_Unscoped.
func TestSummaryPrompt_UnscopedIsUnchanged(t *testing.T) {
	got := promptFixture().Prompt()
	const path = "testdata/prompt_unscoped.golden"
	if os.Getenv("CREW_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("the unscoped prompt changed:\n%s", got)
	}
}

func TestSummaryPrompt_Scoped(t *testing.T) {
	m := treePlan()
	m.Pieces[5].Brief = "the visuals brief"
	s := promptFixture()
	s.Piece = m.Pieces[6]
	d := source.Domain{Name: "design", Body: "Use the brand palette.", Paths: []string{"design/**"}}
	sum := s.Scoped(m, "P3", d)
	if sum.Scope == nil || sum.Scope.Parent.ID != "P3" || len(sum.Scope.Above) != 0 || sum.Scope.Briefing != d.Body ||
		strings.Join(sum.Scope.Paths, ",") != "design/**" {
		t.Fatalf("scope = %+v", sum.Scope)
	}
	if s.Scope != nil {
		t.Error("Scoped changed its receiver")
	}
	p := sum.Prompt()
	for _, want := range []string{
		"You are the orchestrator for piece P3 (the design domain) of a crew mission. P3 was handed to you whole and split into pieces; each is one factory run in its own git worktree. One of them is stuck, and you decide what happens next.",
		"pass it up, to the orchestrator above you and then the principal; put the question in the note.",
		"rewrite the brief of a piece under P3 that hasn't started.",
		"add a piece under P3, quoting the words of the request it serves; crew names it.",
		"Your scope: P3", "the visuals brief", "design/**", "Domain briefing", "Use the brand palette.",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "You are the orchestrator of a crew mission") || strings.Contains(p, "Above P3") {
		t.Error("prompt holds the main intro or an Above section")
	}
	// model-written text stays inside the marked data
	task := p[:strings.Index(p, dataBegin)]
	for _, secret := range []string{"the visuals brief", "Use the brand palette."} {
		if strings.Contains(task, secret) {
			t.Errorf("%q is outside the data", secret)
		}
	}
}

func TestSummaryPrompt_ScopedNestedListsTheOnesAbove(t *testing.T) {
	m := treePlan()
	m.Pieces = append(m.Pieces, Piece{ID: "P3.1.1", Domain: "design", Factory: "visuals", Brief: "x"})
	m.Pieces[6] = Piece{ID: "P3.1", Domain: "design", Delegate: true, Brief: "icons brief", Paths: []string{"design/icons/**"}}
	sum := promptFixture().Scoped(m, "P3.1", source.Domain{Name: "design"})
	if len(sum.Scope.Above) != 1 || sum.Scope.Above[0].ID != "P3" {
		t.Fatalf("above = %+v", sum.Scope.Above)
	}
	if p := sum.Prompt(); !strings.Contains(p, "Above P3.1") || !strings.Contains(p, "the visuals") {
		t.Errorf("prompt = %s", p)
	}
}
