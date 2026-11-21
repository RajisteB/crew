package mission

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestEngine_LoopStopMarksStoppedAndStopsRuns(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.runPiece("P1")
	ctx, cancel := context.WithCancel(context.Background())
	ticked := make(chan struct{}, 1)
	h.runs.onObserve = func() {
		select {
		case ticked <- struct{}{}:
		default:
		}
	}
	type result struct {
		st  State
		err error
	}
	done := make(chan result, 1)
	go func() {
		st, err := h.e.Loop(ctx, 5*time.Millisecond)
		done <- result{st, err}
	}()
	<-ticked
	cancel()
	r := <-done
	if r.err != nil || r.st.Status != StatusStopped {
		t.Fatalf("result = %+v, %v", r.st, r.err)
	}
	if !h.log.has("Stop run-P1") || h.state().Status != StatusStopped {
		t.Fatalf("calls %v state %+v", h.log.calls, h.state())
	}
	if k := h.kinds(); k[len(k)-1] != EvStopped {
		t.Fatalf("events = %v", k)
	}
	if _, ok := h.store.LockedBy(); ok {
		t.Fatal("lock not released")
	}
}

func TestEngine_LoopReturnsWhenMissionIsDone(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.edit(func(s *State) { s.Pieces[0].Status = PieceComplete })
	st, err := h.e.Loop(context.Background(), time.Millisecond)
	if err != nil || st.Status != StatusDone {
		t.Fatalf("st = %+v, err = %v", st, err)
	}
}

func TestEngine_SecondLoopRefused(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	unlock, err := h.store.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = h.e.Loop(context.Background(), time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "already being driven") {
		t.Fatalf("err = %v", err)
	}
	if h.log.has("Observe") || h.log.has("Start") {
		t.Fatalf("second loop acted: %v", h.log.calls)
	}
}
