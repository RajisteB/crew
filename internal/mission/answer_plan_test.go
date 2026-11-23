package mission

import (
	"strings"
	"testing"
)

func planWait() State {
	s := waiting(WaitPlan, PieceQueued)
	s.Pieces[0].Calls, s.Pieces[0].RunID, s.Pieces[0].Worktree = 3, "", ""
	return s
}

func TestApplyAnswer_PlanWait(t *testing.T) {
	_, s, _, step, err := answer(planWait(), Answer{Do: "retry", Words: "try again"})
	if err != nil || step != nil {
		t.Fatalf("step = %+v, err = %v", step, err)
	}
	if p := piece(t, s, "P1"); p.Status != PieceQueued || p.Calls != 0 || p.Wait != nil {
		t.Fatalf("P1 = %+v", p)
	}

	_, s, as, step, err := answer(planWait(), Answer{Do: "cancel", Words: "drop it"})
	if err != nil || step != nil {
		t.Fatalf("step = %+v, err = %v", step, err)
	}
	if p := piece(t, s, "P1"); p.Status != PieceCancelled || !hasKind(as, EvPieceCancelled) {
		t.Fatalf("P1 = %+v", p)
	}

	_, _, _, _, err = answer(planWait(), Answer{Do: "goto", Station: "build"})
	if err == nil || !strings.Contains(err.Error(), "the choices are: retry, cancel") {
		t.Fatalf("err = %v", err)
	}
}

func TestAmend_RebriefOfAParkedDelegatedPieceResetsItsCalls(t *testing.T) {
	s := planWait()
	s.Pieces[1] = PieceState{ID: "P2", Status: PieceWaiting, Calls: 2,
		Wait: &Wait{ID: "w2", Kind: WaitQuestion, Since: t0, Back: PieceQueued}}
	_, s2, _, _, err := amendWith(t, s, func(n *Mission) { n.Pieces[1].Brief = "other brief" })
	if err != nil {
		t.Fatal(err)
	}
	if p := piece(t, s2, "P2"); p.Status != PieceQueued || p.Calls != 2 {
		t.Fatalf("a non-plan wait reset Calls: %+v", p)
	}
	s.Pieces[1].Wait.Kind = WaitPlan
	_, s3, _, _, err := amendWith(t, s, func(n *Mission) { n.Pieces[1].Brief = "other brief" })
	if err != nil {
		t.Fatal(err)
	}
	if p := piece(t, s3, "P2"); p.Status != PieceQueued || p.Calls != 0 {
		t.Fatalf("P2 = %+v", p)
	}
}
