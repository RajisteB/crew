package mission

import (
	"strings"
	"testing"
)

// subPieceHarness is P1 (delegated) with a running sub-piece P1.1 whose run
// has hit a limit.
func subPieceHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t,
		Piece{ID: "P1", Domain: "product", Delegate: true, Brief: "credits platform", Paths: []string{"app/**"}},
		Piece{ID: "P1.1", Domain: "product", Factory: "feature", Brief: "api", Paths: []string{"app/api/**"}})
	h.e.Settings.Limits.Depth = lim(3)
	h.e.Checks.OrchestratorHarness = "claude-code"
	h.edit(func(s *State) { s.Pieces[0].Status = PieceDelegated })
	runID := h.runPiece("P1.1")
	h.runs.setRun(runID, limitHit(), false)
	h.runs.summary = limitHit()
	return h
}

func TestEngine_SubPieceEscalatesUpTheTree(t *testing.T) {
	h := subPieceHarness(t)
	h.orch.decisions = []OrchDecision{
		{Action: "escalate", Note: "first question", Reason: "r"},
		{Action: "escalate", Note: "second question", Reason: "r"},
	}
	h.tick()
	if len(h.orch.sums) != 2 {
		t.Fatalf("calls = %v", h.log.calls)
	}
	if sc := h.orch.sums[0].Scope; sc == nil || sc.Parent.ID != "P1" || sc.Briefing != "Product rules." {
		t.Errorf("first summary scope = %+v", sc)
	}
	if h.orch.sums[1].Scope != nil {
		t.Errorf("second summary is scoped: %+v", h.orch.sums[1].Scope)
	}
	p := h.piece("P1.1")
	if p.Wait == nil || p.Wait.Kind != WaitQuestion || p.Wait.Question != "second question" || p.Calls != 2 {
		t.Fatalf("P1.1 = %+v", p)
	}
	if n := len(h.notif.sent); n != 1 || !strings.Contains(h.notif.sent[0], "second question") {
		t.Errorf("notifications = %v", h.notif.sent)
	}
}

func TestEngine_RestartMidClimbAsksTheSameLevel(t *testing.T) {
	h := subPieceHarness(t)
	h.edit(func(s *State) { s.Pieces[1].Up, s.Pieces[1].Calls = 1, 1 })
	h.orch.decisions = []OrchDecision{{Action: "cancel", Reason: "r"}}
	h.tick()
	if len(h.orch.sums) != 1 || h.orch.sums[0].Scope != nil {
		t.Fatalf("sums = %+v", h.orch.sums)
	}
}

func TestEngine_NestedSubPieceAsksItsOwnParentFirst(t *testing.T) {
	h := newHarness(t,
		Piece{ID: "P1", Domain: "product", Delegate: true, Brief: "credits platform", Paths: []string{"app/**"}},
		Piece{ID: "P1.1", Domain: "product", Delegate: true, Brief: "api", Paths: []string{"app/api/**"}},
		Piece{ID: "P1.1.1", Domain: "product", Factory: "feature", Brief: "endpoint", Paths: []string{"app/api/x/**"}})
	h.e.Settings.Limits.Depth = lim(3)
	h.e.Checks.OrchestratorHarness = "claude-code"
	h.edit(func(s *State) { s.Pieces[0].Status, s.Pieces[1].Status = PieceDelegated, PieceDelegated })
	runID := h.runPiece("P1.1.1")
	h.runs.setRun(runID, limitHit(), false)
	h.runs.summary = limitHit()
	h.orch.decisions = []OrchDecision{{Action: "cancel", Reason: "r"}}
	h.tick()
	if len(h.orch.sums) != 1 || h.orch.sums[0].Scope == nil || h.orch.sums[0].Scope.Parent.ID != "P1.1" ||
		len(h.orch.sums[0].Scope.Above) != 1 || h.orch.sums[0].Scope.Above[0].ID != "P1" {
		t.Fatalf("sums = %+v", h.orch.sums)
	}
}
