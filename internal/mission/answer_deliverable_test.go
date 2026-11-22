package mission

import (
	"reflect"
	"strings"
	"testing"
)

// deliveredAnswerState is P1 delivered with a deliverable wait w1.
func deliveredAnswerState(pr int) State {
	s := exState()
	p := &s.Pieces[0]
	p.Status, p.PR = PieceDelivered, pr
	p.Deliverable = &Deliverable{Files: briefFiles, Print: "p"}
	p.Wait = &Wait{ID: "w1", Kind: WaitDeliverable, Question: "q", Since: t0}
	return s
}

func TestAnswer_AcceptCompletes(t *testing.T) {
	_, s, as, step, err := answer(deliveredAnswerState(0), Answer{Do: "accept", Words: "looks good"})
	if err != nil || step != nil {
		t.Fatalf("step = %+v, err = %v", step, err)
	}
	p := s.Pieces[0]
	if p.Status != PieceComplete || p.Wait != nil || !p.Deliverable.Accepted || p.Deliverable.By != "principal" {
		t.Fatalf("piece = %+v", p)
	}
	ev := acceptEvents(as)
	if len(ev) != 1 || ev[0].By != "principal" || ev[0].Msg != "answered" {
		t.Fatalf("events = %+v", ev)
	}
	if cl := of[Cleanup](as); len(cl) != 1 || !cl[0].Force || cl[0].Piece != "P1" {
		t.Fatalf("cleanup = %+v", cl)
	}
	if !hasKind(as, EvAnswered) || !hasKind(as, EvPieceComplete) {
		t.Fatalf("records = %v", recordKinds(as))
	}
}

func TestAnswer_GotoReopensTheWork(t *testing.T) {
	_, s, _, step, err := answer(deliveredAnswerState(0), Answer{Do: "goto", Station: "build", Note: "more detail", Words: "redo"})
	if err != nil {
		t.Fatal(err)
	}
	if want := (RunStep{Piece: "P1", Kind: "goto", Station: "build", Note: "more detail"}); step == nil || *step != want {
		t.Fatalf("step = %+v", step)
	}
	if p := s.Pieces[0]; p.Status != PieceRunning || p.Deliverable != nil || p.Wait != nil {
		t.Fatalf("piece = %+v", p)
	}
}

func TestAnswer_GotoRefusedAfterAMerge(t *testing.T) {
	in := deliveredAnswerState(7)
	_, s, _, _, err := answer(in, Answer{Do: "goto", Station: "build", Words: "redo"})
	if err == nil || !strings.Contains(err.Error(), "accept, cancel") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(s, in) {
		t.Fatal("state changed on error")
	}
}

func TestAnswer_CancelWorksOnADeliverableWait(t *testing.T) {
	_, s, _, _, err := answer(deliveredAnswerState(7), Answer{Do: "cancel", Words: "drop it"})
	if err != nil || s.Pieces[0].Status != PieceCancelled {
		t.Fatalf("status = %s, err = %v", s.Pieces[0].Status, err)
	}
}

func TestAnswer_AcceptRefusedOnOtherWaits(t *testing.T) {
	for _, in := range []State{waiting(WaitMerge, ""), waiting(WaitQuestion, PieceRunning), waiting(WaitApproval, PieceRunning)} {
		if _, _, _, _, err := answer(in, Answer{Do: "accept"}); err == nil {
			t.Errorf("%s: no error", in.Pieces[0].Wait.Kind)
		}
	}
}
