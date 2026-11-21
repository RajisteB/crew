package mission

import (
	"reflect"
	"testing"
)

func TestEngine_TickOnStoppedMissionDoesNothing(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.runPiece("P1")
	h.edit(func(s *State) {
		s.Status = StatusStopped
		s.Pieces[0].Status = PieceWaiting
		s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitQuestion, Question: "?", Since: testNow, Back: PieceRunning}
	})
	if err := h.store.Drop("answer", Answer{Wait: "w1", Do: "retry", Words: "go on"}); err != nil {
		t.Fatal(err)
	}
	before := h.state()
	st := h.tick()
	if h.log.has("Step") || h.log.has("Start") || h.log.has("Drive") {
		t.Fatalf("a stopped mission acted: %v", h.log.calls)
	}
	if items, err := h.store.Inbox(); err != nil || len(items) != 1 {
		t.Fatalf("inbox = %v, %v; the answer must stay queued", items, err)
	}
	if !reflect.DeepEqual(st, before) || !reflect.DeepEqual(h.state(), before) {
		t.Fatalf("state changed:\nbefore %+v\nafter  %+v", before, h.state())
	}
}
