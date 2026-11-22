package mission

import (
	"reflect"
	"testing"
	"time"
)

// deliveredInReview gives P1 a posted, unaccepted deliverable whose issue is
// recorded in In Review, and a board that shows the issue in Done.
func deliveredInReview(h *harness, ft *fakeTracker, unconfirmed bool) {
	h.t.Helper()
	h.tick()
	ft.doneKeys["J-2"] = true
	h.edit(func(s *State) {
		s.Pieces[0].Status = PieceDelivered
		s.Pieces[0].Deliverable = &Deliverable{Files: []string{"brief.md"}, Attached: []string{"brief.md"}, Posted: testNow}
		s.Pieces[0].Wait = &Wait{ID: "w9", Kind: WaitDeliverable, Since: testNow, Back: PieceDelivered}
		s.Pieces[0].Jira.Lane, s.Pieces[0].Jira.Unconfirmed = LaneInReview, unconfirmed
		s.Pieces[0].Jira.Flagged = true
	})
}

func TestEngine_AGivenUpMoveIsNotReadAsDone(t *testing.T) {
	h := onePiece(t)
	ft := h.withTracker()
	deliveredInReview(h, ft, true)
	h.tick()
	if h.log.has("done:") {
		t.Fatalf("crew read Done for a lane it never confirmed: %v", h.log.calls)
	}
	if d := h.piece("P1").Deliverable; d == nil || d.Accepted {
		t.Fatalf("deliverable = %+v", d)
	}
}

func TestEngine_AConfirmedMoveIsReadAsDone(t *testing.T) {
	h := onePiece(t)
	ft := h.withTracker()
	deliveredInReview(h, ft, false)
	h.tick()
	if !h.log.has("done:J-2") || !h.piece("P1").Deliverable.Accepted {
		t.Fatalf("calls %v deliverable %+v", h.log.calls, h.piece("P1").Deliverable)
	}
}

func TestEngine_APermanentMoveIsRecordedUnconfirmed(t *testing.T) {
	h := onePiece(t)
	ft := h.withTracker()
	ft.permanent["move:J-2"] = true
	h.tick()
	if j := h.piece("P1").Jira; j.Lane != LaneInProgress || !j.Unconfirmed {
		t.Fatalf("jira = %+v", j)
	}
}

func TestDeliverableCollected_ClearsTheLaneSoTheMoveIsOwedAgain(t *testing.T) {
	s := State{Status: StatusRunning, Pieces: []PieceState{{ID: "P1", Status: PieceDelivered,
		Jira: Tracked{Issue: "ACME-2", Lane: LaneInReview, Unconfirmed: true}}}}
	s.Jira = TrackerState{Epic: "ACME-1", EpicLane: LaneInProgress}
	if ops := TrackerOps(Mission{}, s, nil, false); len(ops) != 0 {
		t.Fatalf("before: %v", ops)
	}
	c := DeliverableCollected(s, "P1", []string{"brief.md"}, "p", time.Time{})
	if j := c.Pieces[0].Jira; j.Lane != "" || j.Unconfirmed || j.Issue != "ACME-2" {
		t.Fatalf("jira = %+v", j)
	}
	ops := TrackerOps(Mission{}, c, nil, false)
	if len(ops) == 0 || ops[0].Kind != "move" || ops[0].Piece != "P1" || ops[0].Lane != LaneInReview {
		t.Fatalf("ops = %+v", ops)
	}
}

func TestLaneReducers_TrackConfirmation(t *testing.T) {
	base := State{Pieces: []PieceState{{ID: "P1", Jira: Tracked{Issue: "K", Lane: LaneToDo}}}}
	before := base.Clone()
	g := LaneGivenUp(base, "P1", LaneInReview)
	if j := g.Pieces[0].Jira; j.Lane != LaneInReview || !j.Unconfirmed {
		t.Fatalf("given up: %+v", j)
	}
	if j := LaneSet(g, "P1", LaneDone).Pieces[0].Jira; j.Lane != LaneDone || j.Unconfirmed {
		t.Fatalf("set: %+v", j)
	}
	if e := LaneGivenUp(base, "", LaneDone); e.Jira.EpicLane != LaneDone {
		t.Fatalf("epic: %+v", e.Jira)
	}
	gc := g.Clone()
	DeliverableCollected(g, "P1", []string{"a"}, "p", time.Time{})
	LaneSet(g, "P1", LaneDone)
	if !reflect.DeepEqual(base, before) || !reflect.DeepEqual(g, gc) {
		t.Fatal("a reducer changed its input")
	}
}
