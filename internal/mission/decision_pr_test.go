package mission

import (
	"slices"
	"strings"
	"testing"
)

func prOpenState() State {
	s := exState()
	s.Pieces[0].Status = PiecePROpen
	s.Pieces[0].PR = 7
	return s
}

func applyPR(t *testing.T, failure string, d OrchDecision) (State, []Action, *RunStep) {
	t.Helper()
	_, s, as, step := ApplyDecision(acme(), prOpenState(), "P1", failure, d, "orchestrator", exInput(), t0)
	return s, as, step
}

func TestApplyDecision_RetryRefusedForChecksFailed(t *testing.T) {
	s, _, step := applyPR(t, "checks-failed", OrchDecision{Action: "retry", Reason: "flaky"})
	w := s.Pieces[0].Wait
	if step != nil || w == nil || w.Kind != WaitQuestion || w.Back != "" || s.Pieces[0].Status != PiecePROpen ||
		!strings.Contains(w.Question, `"retry" is not an action it may take`) {
		t.Errorf("step = %+v, piece = %+v", step, s.Pieces[0])
	}
}

func TestApplyDecision_GotoFromPROpenRuns(t *testing.T) {
	s, _, step := applyPR(t, "conflict", OrchDecision{Action: "goto", Station: "build", Note: "n", Reason: "r"})
	if step == nil || step.Kind != "goto" || step.Station != "build" || s.Pieces[0].Status != PieceRunning || s.Pieces[0].Wait != nil {
		t.Errorf("step = %+v, piece = %+v", step, s.Pieces[0])
	}
}

func TestApplyDecision_EscalateKeepsPROpen(t *testing.T) {
	s, _, step := applyPR(t, "conflict", OrchDecision{Action: "escalate", Note: "which side wins?"})
	w := s.Pieces[0].Wait
	if step != nil || w == nil || w.Kind != WaitQuestion || w.Back != "" || s.Pieces[0].Status != PiecePROpen {
		t.Errorf("step = %+v, piece = %+v", step, s.Pieces[0])
	}
}

func TestApplyDecision_RetryStillAllowedForStageOneFailures(t *testing.T) {
	for _, f := range []string{"limit-hit", "failed", "uncommitted", "other"} {
		if !slices.Contains(allowedFor(f), "retry") {
			t.Errorf("%s: retry not allowed", f)
		}
	}
}

func TestApplyAnswer_RetryOnPROpenResetsSendBacks(t *testing.T) {
	for _, kind := range []string{WaitQuestion, WaitLimit} {
		s := waiting(kind, "")
		s.Pieces[0].SentBack = map[string]int{"conflict": 2, "checks-failed": 2}
		_, got, _, step, err := answer(s, Answer{Do: "retry"})
		p := got.Pieces[0]
		if err != nil || step != nil || p.Status != PiecePROpen || p.Wait != nil ||
			p.SentBack["conflict"] != 0 || p.SentBack["checks-failed"] != 0 {
			t.Errorf("%s: piece = %+v, step = %+v, err = %v", kind, p, step, err)
		}
		if s.Pieces[0].SentBack["conflict"] != 2 {
			t.Errorf("%s: input state modified", kind)
		}
	}
}

func TestApplyAnswer_GotoOnPROpenRuns(t *testing.T) {
	_, s, _, step, err := answer(waiting(WaitQuestion, ""), Answer{Do: "goto", Station: "build"})
	if err != nil || step == nil || step.Kind != "goto" || s.Pieces[0].Status != PieceRunning || s.Pieces[0].Wait != nil {
		t.Errorf("piece = %+v, step = %+v, err = %v", s.Pieces[0], step, err)
	}
}

func TestApplyAnswer_CancelOnPROpenUnchanged(t *testing.T) {
	_, s, _, _, err := answer(waiting(WaitLimit, ""), Answer{Do: "cancel"})
	if err != nil || s.Pieces[0].Status != PieceCancelled {
		t.Errorf("piece = %+v, err = %v", s.Pieces[0], err)
	}
}
