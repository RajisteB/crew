package mission

import (
	"strings"
	"testing"
)

// withTrackerDown sets the engine's TrackerDown to a func that answers down.
func (h *harness) withTrackerDown(down bool) {
	h.e.TrackerDown = func(error) bool { return down }
}

// flagFailsWithMoreOwed: after a first tick, P1's flag is owed and keeps
// failing with a plain error, while P2's move and a comment on P1 are owed
// after it in the plan.
func flagFailsWithMoreOwed(h *harness) {
	h.t.Helper()
	h.tick()
	h.fail.failNext("flag:J-2", 1000)
	h.edit(func(s *State) {
		s.Pieces[0].Wait = &Wait{ID: "w9", Kind: WaitApproval, Since: testNow, Back: PieceRunning}
		s.Pieces[1].Status = PieceCancelled
	})
	h.emitEv(Event{Kind: EvDecision, Piece: "P1", Msg: "keep going"})
}

// jiraCallsAfter lists the Jira calls logged after the first call named first.
func jiraCallsAfter(h *harness, first string) []string {
	idx := h.log.indexes(first)
	if len(idx) == 0 {
		return nil
	}
	var out []string
	for _, c := range h.log.calls[idx[len(idx)-1]+1:] {
		for _, p := range []string{"create:", "move:", "flag:", "attach:", "comment:", "sprint:"} {
			if strings.HasPrefix(c, p) {
				out = append(out, c)
			}
		}
	}
	return out
}

func TestEngine_ANonDownErrorSkipsOnlyThatOp(t *testing.T) {
	h := newHarness(t)
	h.withTracker()
	h.withTrackerDown(false)
	flagFailsWithMoreOwed(h)
	before := jiraNotes(h)
	h.tick()
	if !h.log.has("move:J-3:done") || h.log.count("comment:J-2") != 1 {
		t.Fatalf("the ops after the failed flag were not sent: %v", h.log.calls)
	}
	if jiraNotes(h)-before != 1 {
		t.Fatalf("notes = %v", h.events(EvNote))
	}
	if n := h.events(EvNote); !strings.Contains(n[len(n)-1].Msg, "Jira: can't flag J-2: ") {
		t.Fatalf("note = %q", n[len(n)-1].Msg)
	}
	h.tick()
	if h.log.count("flag:J-2:true") != 2 {
		t.Fatalf("the flag was not tried again: %v", h.log.calls)
	}
}

func TestEngine_ADownErrorStillEndsThePass(t *testing.T) {
	h := newHarness(t)
	h.withTracker()
	h.withTrackerDown(true)
	flagFailsWithMoreOwed(h)
	before := jiraNotes(h)
	h.tick()
	if h.log.count("flag:J-2:true") != 1 || jiraNotes(h)-before != 1 {
		t.Fatalf("calls %v notes %v", h.log.calls, h.events(EvNote))
	}
	if after := jiraCallsAfter(h, "flag:J-2:true"); len(after) != 0 {
		t.Fatalf("calls after a down error: %v", after)
	}
}

func TestEngine_NilTrackerDownKeepsOldBehaviour(t *testing.T) {
	h := newHarness(t)
	h.withTracker()
	flagFailsWithMoreOwed(h)
	h.tick()
	if h.log.count("flag:J-2:true") != 1 || h.log.has("move:J-3") || h.log.has("comment:") {
		t.Fatalf("calls = %v", h.log.calls)
	}
}

func TestEngine_SkippedCommentHoldsTheCursor(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.withTrackerDown(false)
	h.tick()
	h.emitEv(Event{Kind: EvDecision, Piece: "P1", Msg: "first"})
	first := lastSeq(t, h)
	h.emitEv(Event{Kind: EvDecision, Piece: "P1", Msg: "second"})
	second := lastSeq(t, h)
	h.fail.failNext("comment:J-2", 1)
	h.tick()
	if h.log.count("comment:J-2") != 1 || h.state().Jira.Seq >= first {
		t.Fatalf("calls %v seq %d (first %d)", h.log.calls, h.state().Jira.Seq, first)
	}
	h.tick()
	if h.log.count("comment:J-2") != 3 || h.state().Jira.Seq < second {
		t.Fatalf("calls %v seq %d (second %d)", h.log.calls, h.state().Jira.Seq, second)
	}
}

func TestEngine_AttachIsGivenUpAfterThreeTries(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.withTrackerDown(true) // an attach that fails is never "down"
	h.tick()
	deliverable(t, h, "a.txt", 5)
	h.fail.failNext("attach", 1000)
	for range 3 {
		h.tick()
	}
	giveUp := 0
	for _, e := range h.events(EvNote) {
		if e.Msg == "Jira: a.txt could not be attached after 3 tries and stays in the mission folder" {
			giveUp++
		}
	}
	if h.log.count("attach:J-2:a.txt") != 3 || giveUp != 1 {
		t.Fatalf("calls %v notes %v", h.log.calls, h.events(EvNote))
	}
	if a := h.piece("P1").Deliverable.Attached; len(a) != 1 {
		t.Fatalf("attached = %v", a)
	}
	h.tick()
	if h.log.count("attach:") != 3 {
		t.Fatalf("calls = %v", h.log.calls)
	}
}

func TestEngine_AFailedAttachDoesNotEndThePass(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.withTrackerDown(true)
	h.tick()
	deliverable(t, h, "a.txt", 5)
	h.fail.failNext("attach", 1000)
	h.emitEv(Event{Kind: EvDecision, Piece: "P1", Msg: "after the attach"})
	h.tick()
	if h.log.count("attach:J-2:a.txt") != 1 || h.log.count("comment:J-2") != 1 {
		t.Fatalf("calls = %v", h.log.calls)
	}
}
