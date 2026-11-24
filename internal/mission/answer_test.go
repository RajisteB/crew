package mission

import (
	"reflect"
	"strings"
	"testing"
)

// waiting returns a state whose P1 holds a wait of the given kind.
func waiting(kind string, back PieceStatus) State {
	s := exState()
	s.Pieces[0].Status = PieceWaiting
	if back == "" {
		s.Pieces[0].Status = PiecePROpen
		s.Pieces[0].PR = 7
	}
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: kind, Question: "q?", Since: t0, Back: back}
	return s
}

func answer(s State, a Answer) (Mission, State, []Action, *RunStep, error) {
	a.Wait = "w1"
	return ApplyAnswer(acme(), s, a, exInput(), t0)
}

func TestApplyAnswer_ApproveOnlyForApprovalWaits(t *testing.T) {
	_, s, as, step, err := answer(waiting(WaitApproval, PieceRunning), Answer{Do: "approve", Station: "build", Note: "go", Words: "yes, go ahead"})
	if err != nil || step == nil || *step != (RunStep{Piece: "P1", Kind: "approve", Station: "build", Note: "go"}) {
		t.Fatalf("step = %+v, err = %v", step, err)
	}
	if s.Pieces[0].Wait != nil || s.Pieces[0].Status != PieceRunning {
		t.Errorf("piece = %+v", s.Pieces[0])
	}
	var got *Event
	for _, a := range as {
		if r, ok := a.(Record); ok && r.Event.Kind == EvAnswered {
			got = &r.Event
		}
	}
	if got == nil || got.Msg != "yes, go ahead" || got.By != "principal" {
		t.Errorf("answered = %+v", got)
	}
	if d := s.Pieces[0].Decisions; len(d) != 1 || d[0].By != "principal" || d[0].Action != "approve" || d[0].Station != "build" || d[0].Note != "go" {
		t.Errorf("decisions = %+v", d)
	}
	for _, kind := range []string{WaitQuestion, WaitLimit} {
		in := waiting(kind, PieceRunning)
		_, s2, _, _, err := answer(in, Answer{Do: "approve"})
		if err == nil || !strings.Contains(err.Error(), "w1") || !strings.Contains(err.Error(), kind) || !strings.Contains(err.Error(), "retry") {
			t.Errorf("%s: err = %v", kind, err)
		}
		if !reflect.DeepEqual(s2, in) {
			t.Errorf("%s: state changed on error", kind)
		}
	}
}

func TestApplyAnswer_RetryOnClosedPRReturnsToPROpen(t *testing.T) {
	_, s, _, step, err := answer(waiting(WaitPRClosed, ""), Answer{Do: "retry", Words: "reopened it"})
	if err != nil || step != nil || s.Pieces[0].Status != PiecePROpen || s.Pieces[0].Wait != nil {
		t.Errorf("piece = %+v, step = %+v, err = %v", s.Pieces[0], step, err)
	}
}

func TestApplyAnswer_QuestionRetryGotoCancel(t *testing.T) {
	_, s, _, step, err := answer(waiting(WaitLimit, PieceRunning), Answer{Do: "goto", Station: "plan", Note: "n"})
	if err != nil || step == nil || step.Kind != "goto" || step.Station != "plan" || s.Pieces[0].Status != PieceRunning {
		t.Errorf("goto: step = %+v, err = %v, piece = %+v", step, err, s.Pieces[0])
	}
	_, _, _, _, err = answer(waiting(WaitLimit, PieceRunning), Answer{Do: "goto", Station: "nowhere"})
	if err == nil {
		t.Error("goto an unknown station accepted")
	}
	_, _, _, step, err = answer(waiting(WaitQuestion, PieceRunning), Answer{Do: "retry"})
	if err != nil || step == nil || step.Kind != "retry" {
		t.Errorf("retry: step = %+v, err = %v", step, err)
	}
}

func TestApplyAnswer_CancelAndQueuedWait(t *testing.T) {
	_, s, as, step, err := answer(waiting(WaitQuestion, PieceQueued), Answer{Do: "cancel"})
	if err != nil || s.Pieces[0].Status != PieceCancelled || s.Pieces[0].Wait != nil {
		t.Fatalf("piece = %+v, err = %v", s.Pieces[0], err)
	}
	if step == nil || step.Kind != "stop" || !hasKind(as, EvPieceCancelled) {
		t.Errorf("step = %+v, actions = %v", step, as)
	}
	_, _, _, _, err = answer(waiting(WaitQuestion, PieceQueued), Answer{Do: "retry"})
	if err == nil {
		t.Error("retry on a queued wait accepted")
	}
	_, _, _, _, err = answer(waiting(WaitMerge, ""), Answer{Do: "retry"})
	if err == nil {
		t.Error("retry on a merge wait accepted")
	}
}

func TestApplyAnswer_WordsCutOnRuneBoundary(t *testing.T) {
	words := strings.Repeat("é", 600)
	_, _, as, _, err := answer(waiting(WaitQuestion, PieceRunning), Answer{Do: "retry", Words: words})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range as {
		if r, ok := a.(Record); ok && r.Event.Kind == EvAnswered {
			found = true
			if r.Event.Msg != strings.Repeat("é", 500) {
				t.Errorf("msg has %d bytes", len(r.Event.Msg))
			}
		}
	}
	if !found {
		t.Error("no answered record")
	}
}

func TestApplyAnswer_UnknownWaitIsError(t *testing.T) {
	m, s := acme(), waiting(WaitQuestion, PieceRunning)
	before := s.Clone()
	m2, s2, as, step, err := ApplyAnswer(m, s, Answer{Wait: "w99", Do: "retry"}, exInput(), t0)
	if err == nil || !strings.Contains(err.Error(), "w99") || as != nil || step != nil {
		t.Errorf("err = %v, actions = %v, step = %+v", err, as, step)
	}
	if !reflect.DeepEqual(s2, before) || !reflect.DeepEqual(m2, m) {
		t.Error("inputs changed on error")
	}
}

func TestApplyAnswer_DoesNotMutateInput(t *testing.T) {
	s := waiting(WaitLimit, PieceRunning)
	before := s.Clone()
	if _, _, _, _, err := answer(s, Answer{Do: "cancel", Words: "stop"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Error("state mutated")
	}
}

func TestApplyAnswer_ApprovalGotoSendsBackThroughApprove(t *testing.T) {
	_, s, _, step, err := answer(waiting(WaitApproval, PieceRunning), Answer{Do: "goto", Station: "build", Note: "fix the typo", Words: "send it back"})
	if err != nil || step == nil || *step != (RunStep{Piece: "P1", Kind: "approve", Station: "build", Note: "fix the typo"}) {
		t.Fatalf("step = %+v, err = %v", step, err)
	}
	if s.Pieces[0].Wait != nil {
		t.Errorf("wait not cleared: %+v", s.Pieces[0])
	}
	d := s.Pieces[0].Decisions
	if len(d) != 1 || d[0].Action != "goto" || d[0].Station != "build" {
		t.Errorf("decisions = %+v", d)
	}
}

func TestApplyAnswer_ApprovalGotoNeedsAStation(t *testing.T) {
	in := waiting(WaitApproval, PieceRunning)
	_, s, _, _, err := answer(in, Answer{Do: "goto"})
	if err == nil || !strings.Contains(err.Error(), "is not in the factory of piece") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(s, in) {
		t.Error("state changed on error")
	}
}

func TestApplyAnswer_ApprovalRetryListsItsChoices(t *testing.T) {
	_, _, _, _, err := answer(waiting(WaitApproval, PieceRunning), Answer{Do: "retry"})
	if err == nil || !strings.Contains(err.Error(), "the choices are: approve, goto, cancel") {
		t.Errorf("err = %v", err)
	}
}
