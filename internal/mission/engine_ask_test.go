package mission

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func limitHit() run.State {
	return run.State{Status: run.StatusFailed, Current: "review", LimitRoute: "review.fix",
		RouteCounts: map[string]int{"review.fix": 4}, Reason: "route limit reached"}
}

func TestEngine_LimitHitAsksOrchestratorAndAppliesGoto(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	runID := h.runPiece("P1")
	h.runs.setRun(runID, limitHit(), false)
	h.runs.summary = limitHit()
	h.orch.decisions = []OrchDecision{{Action: "goto", Station: "plan", Note: "replan", Reason: "bad plan"}}
	h.tick()

	order := []string{"Summary", "Orch.Decide", "Step run-P1 goto plan"}
	last := -1
	for _, prefix := range order {
		idx := h.log.indexes(prefix)
		if len(idx) != 1 || idx[0] < last {
			t.Fatalf("%s out of order: %v", prefix, h.log.calls)
		}
		last = idx[0]
	}
	p := h.piece("P1")
	if p.Calls != 1 || len(p.Decisions) != 1 || p.Decisions[0].Action != "goto" || p.Decisions[0].By != "orchestrator" {
		t.Fatalf("piece = %+v", p)
	}
	got := h.kinds()
	if !slices.Contains(got, EvException) || !slices.Contains(got, EvDecision) || slices.Index(got, EvException) > slices.Index(got, EvDecision) {
		t.Fatalf("events = %v", got)
	}
	if !strings.Contains(h.orch.prompts[0], "review.fix") {
		t.Fatalf("prompt = %q", h.orch.prompts[0])
	}
}

func TestEngine_OrchestratorErrorEscalatesToPrincipal(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	runID := h.runPiece("P1")
	h.runs.setRun(runID, limitHit(), false)
	h.runs.summary = limitHit()
	h.orch.err = errors.New("harness crashed")
	h.tick()
	p := h.piece("P1")
	if p.Status != PieceWaiting || p.Wait == nil || p.Wait.Kind != WaitQuestion || !strings.Contains(p.Wait.Question, "harness crashed") {
		t.Fatalf("piece = %+v", p)
	}
	if p.Calls != 1 || h.log.has("Step") || !slices.Contains(h.kinds(), EvEscalated) || len(h.notif.sent) == 0 {
		t.Fatalf("calls %v kinds %v sent %v", h.log.calls, h.kinds(), h.notif.sent)
	}
}

func TestEngine_AskFailsWithoutSummary(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	runID := h.runPiece("P1")
	h.runs.setRun(runID, limitHit(), false)
	h.fail.failNext("Summary", 1)
	h.tick()
	if h.log.has("Orch.Decide") || h.piece("P1").Calls != 0 || len(h.events(EvNote)) != 1 {
		t.Fatalf("calls %v piece %+v", h.log.calls, h.piece("P1"))
	}
}

func TestEngine_AnswerFromInboxApprovesRun(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	runID := h.runPiece("P1")
	h.edit(func(s *State) {
		s.Pieces[0].Status = PieceWaiting
		s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitApproval, Question: "ok?", Since: testNow, Back: PieceRunning}
		s.NextWait = 2
	})
	if err := h.store.Drop("answer", Answer{Wait: "w1", Do: "approve", Words: "looks fine"}); err != nil {
		t.Fatal(err)
	}
	observed := false
	h.runs.onObserve = func() {
		observed = true
		items, _ := h.store.Inbox()
		if len(items) != 1 {
			t.Errorf("inbox emptied before the end of the tick: %d items", len(items))
		}
		if p := h.piece("P1"); p.Wait != nil || p.Status != PieceRunning {
			t.Errorf("state not saved before the item is acknowledged: %+v", p)
		}
	}
	h.tick()
	if !observed {
		t.Fatal("Observe was never called, so the checks inside it did not run")
	}
	if !h.log.has("Step " + runID + " approve") {
		t.Fatalf("calls: %v", h.log.calls)
	}
	if items, _ := h.store.Inbox(); len(items) != 0 {
		t.Fatalf("inbox = %v", items)
	}
	if !slices.Contains(h.kinds(), EvAnswered) {
		t.Fatalf("events = %v", h.kinds())
	}
}

func TestEngine_BadInboxItemsBecomeNotesAndAreAcknowledged(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	if err := h.store.Drop("answer", map[string]string{"wait": "w9", "bogus": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.Drop("answer", Answer{Wait: "w9", Do: "retry"}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if items, _ := h.store.Inbox(); len(items) != 0 {
		t.Fatalf("inbox = %v", items)
	}
	if notes := h.events(EvNote); len(notes) != 2 {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestEngine_FailedStepKeepsAnswerInInbox(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.runPiece("P1")
	h.edit(func(s *State) {
		s.Pieces[0].Status = PieceWaiting
		s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitApproval, Since: testNow, Back: PieceRunning}
	})
	_ = h.store.Drop("answer", Answer{Wait: "w1", Do: "approve", Words: "go"})
	h.fail.failNext("Step", 1)
	h.tick()
	if p := h.piece("P1"); p.Wait == nil {
		t.Fatal("answer applied although its step failed")
	}
	if items, _ := h.store.Inbox(); len(items) != 1 {
		t.Fatalf("inbox = %v", items)
	}
	h.tick()
	if p := h.piece("P1"); p.Wait != nil {
		t.Fatalf("retry did not apply the answer: %+v", p)
	}
}

func TestEngine_AmendFromInboxAddsPiece(t *testing.T) {
	h := newHarness(t)
	m, _ := h.store.Mission()
	next := m
	next.Pieces = append(slices.Clone(m.Pieces), Piece{ID: "P3", Domain: "product", Factory: "feature", Brief: "push", WaitsFor: []string{"P2"}})
	if err := h.store.Drop("amend", Amendment{Plan: next.Render(), Words: "add push"}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	got, err := h.store.Mission()
	if err != nil || len(got.Pieces) != 3 || len(got.Changes) != 1 {
		t.Fatalf("mission = %+v, %v", got, err)
	}
	if p, ok := h.state().Piece("P3"); !ok || p.Status != PieceQueued {
		t.Fatalf("P3 = %+v", p)
	}
	if items, _ := h.store.Inbox(); len(items) != 0 {
		t.Fatalf("inbox = %v", items)
	}
	if k := h.kinds(); !slices.Contains(k, EvAmendment) || !slices.Contains(k, EvPieceAdded) {
		t.Fatalf("events = %v", k)
	}
}

func TestEngine_SummaryOutageIsOneNoteAndNoException(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	runID := h.runPiece("P1")
	h.runs.setRun(runID, limitHit(), false)
	h.runs.summary = limitHit()
	h.fail.failNext("Summary", 2)
	h.orch.decisions = []OrchDecision{{Action: "goto", Station: "plan", Reason: "r"}}
	h.tick()
	h.tick()
	if len(h.events(EvNote)) != 1 || len(h.events(EvException)) != 0 {
		t.Fatalf("events = %v", h.kinds())
	}
	h.tick()
	if len(h.events(EvException)) != 1 {
		t.Fatalf("events = %v", h.kinds())
	}
}

// cancelOrch cancels the tick's context mid-call, the way a stop signal does.
type cancelOrch struct{ cancel context.CancelFunc }

func (o cancelOrch) Decide(context.Context, string, Summary) (OrchDecision, error) {
	o.cancel()
	return OrchDecision{}, errors.New("run interrupted")
}

func TestEngine_StopDuringOrchestratorCallChangesNothing(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	runID := h.runPiece("P1")
	h.runs.setRun(runID, limitHit(), false)
	h.runs.summary = limitHit()
	ctx, cancel := context.WithCancel(context.Background())
	h.e.Orch = cancelOrch{cancel}
	if _, err := h.e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	p := h.piece("P1")
	if p.Status != PieceRunning || p.Calls != 0 || p.Wait != nil {
		t.Fatalf("piece = %+v", p)
	}
	if len(h.notif.sent) != 0 || len(h.events(EvException)) != 0 || len(h.events(EvEscalated)) != 0 || len(h.events(EvDecision)) != 0 {
		t.Fatalf("sent %v kinds %v", h.notif.sent, h.kinds())
	}
}
