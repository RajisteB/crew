package mission

import (
	"context"
	"strings"
	"testing"
	"time"
)

func lastSeq(t *testing.T, h *harness) int {
	t.Helper()
	evs, err := h.store.EventsSince(0)
	if err != nil || len(evs) == 0 {
		t.Fatalf("events: %v %v", evs, err)
	}
	return evs[len(evs)-1].Seq
}

func TestEngine_BadDeliverableNameIsGivenUp(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.tick()
	deliverable(t, h, "ok.txt", 5)
	h.edit(func(s *State) { s.Pieces[0].Deliverable.Files = []string{"../x", "ok.txt"} })
	h.tick()
	h.tick()
	if h.log.has("attach:J-2:x") || h.log.count("attach:J-2:ok.txt") != 1 || jiraNotes(h) != 1 {
		t.Fatalf("calls %v notes %v", h.log.calls, h.events(EvNote))
	}
	if a := h.piece("P1").Deliverable.Attached; len(a) != 2 {
		t.Fatalf("attached = %v", a)
	}
}

func TestEngine_SavesTheEpicKeyBeforeTheNextCreate(t *testing.T) {
	h := newHarness(t)
	ft := h.withTracker()
	onDisk := "unset"
	ft.onCreate = func(kind, _ string) {
		if kind == "piece" && onDisk == "unset" {
			st, err := h.store.LoadState()
			if err != nil {
				t.Error(err)
			}
			onDisk = st.Jira.Epic
		}
	}
	h.tick()
	if onDisk != "J-1" {
		t.Fatalf("epic key on disk at P1's create = %q", onDisk)
	}
}

func TestEngine_EmptyIssueKeyIsAnError(t *testing.T) {
	h := newHarness(t)
	ft := h.withTracker()
	ft.emptyKey = true
	h.tick()
	if h.log.count("create:") != 1 || jiraNotes(h) != 1 || h.state().Jira.Epic != "" {
		t.Fatalf("calls %v notes %v", h.log.calls, h.events(EvNote))
	}
	if !strings.Contains(h.events(EvNote)[0].Msg, "the tracker returned no issue key") {
		t.Fatalf("note = %q", h.events(EvNote)[0].Msg)
	}
}

func TestEngine_CommentOfAPieceWithoutAnIssueHoldsTheCursor(t *testing.T) {
	h := newHarness(t)
	ft := h.withTracker()
	ft.permanent["create:crew-"+h.id+"-P1"] = true
	h.emitEv(Event{Kind: EvDecision, Piece: "P1", Msg: "first"})
	first := lastSeq(t, h)
	h.emitEv(Event{Kind: EvDecision, Piece: "P2", Msg: "second"})
	h.tick()
	if h.log.has("comment:") || h.state().Jira.Seq >= first {
		t.Fatalf("calls %v seq %d (first %d)", h.log.calls, h.state().Jira.Seq, first)
	}
	delete(ft.permanent, "create:crew-"+h.id+"-P1")
	h.tick()
	if h.log.count("comment:J-3") != 1 || h.log.count("comment:J-2") != 1 || h.state().Jira.Seq < first+1 {
		t.Fatalf("calls %v seq %d", h.log.calls, h.state().Jira.Seq)
	}
}

func TestEngine_EventsWithoutCommentsMoveTheCursor(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.emitEv(Event{Kind: EvPROpened, Piece: "P1"})
	h.emitEv(Event{Kind: EvNote, Piece: "P1", Msg: "just a note"})
	h.tick()
	want := lastSeq(t, h)
	if got := h.state().Jira.Seq; got != want {
		t.Fatalf("seq = %d, want %d (events %v)", got, want, h.kinds())
	}
	h.tick()
	if evs, _ := h.store.EventsSince(h.state().Jira.Seq); len(evs) != 0 {
		t.Fatalf("events left after the cursor: %v", evs)
	}
}

func TestEngine_PermanentCommentAndFlagAreGivenUp(t *testing.T) {
	h := onePiece(t)
	ft := h.withTracker()
	ft.permanent["comment:J-2"], ft.permanent["flag:J-2"] = true, true
	h.tick()
	h.edit(func(s *State) {
		s.Pieces[0].Wait = &Wait{ID: "w9", Kind: WaitApproval, Since: testNow, Back: PieceRunning}
	})
	h.emitEv(Event{Kind: EvWaitingOnPrincipal, Piece: "P1", Msg: "approve it"})
	seq := lastSeq(t, h)
	for range 3 {
		h.tick()
	}
	if h.log.count("comment:J-2") != 1 || h.log.count("flag:J-2") != 1 || jiraNotes(h) != 2 {
		t.Fatalf("calls %v notes %v", h.log.calls, h.events(EvNote))
	}
	if st := h.state(); !st.Pieces[0].Jira.Flagged || st.Jira.Seq < seq {
		t.Fatalf("flagged %v seq %d (want >= %d)", st.Pieces[0].Jira.Flagged, st.Jira.Seq, seq)
	}
}

func TestEngine_PermanentSprintIsANoteAndTriedAgain(t *testing.T) {
	h := onePiece(t)
	ft := h.withTracker()
	h.e.Sprints = true
	ft.permanent["sprint"] = true
	h.tick()
	h.tick()
	if h.log.count("sprint:") != 2 || !h.log.has("move:J-2:in-progress") || jiraNotes(h) != 1 {
		t.Fatalf("calls %v notes %v", h.log.calls, h.events(EvNote))
	}
	if h.piece("P1").Jira.Sprint {
		t.Fatal("sprint recorded as done")
	}
}

func TestLoop_NoActiveSprintDoesNotDrain(t *testing.T) {
	h := onePiece(t)
	ft := h.withTracker()
	h.e.Sprints, ft.noSprint = true, true
	h.edit(func(s *State) { s.Pieces[0].Status = PieceComplete })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := h.e.Loop(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the loop waited through drain passes")
	}
	for _, e := range h.events(EvNote) {
		if strings.Contains(e.Msg, "Jira is behind") {
			t.Fatalf("unexpected note %q", e.Msg)
		}
	}
}
