package mission

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jiraNotes is the number of notes about Jira in the events so far.
func jiraNotes(h *harness) int {
	n := 0
	for _, e := range h.events(EvNote) {
		if strings.HasPrefix(e.Msg, "Jira") {
			n++
		}
	}
	return n
}

func onePiece(t *testing.T) *harness {
	t.Helper()
	return newHarness(t, twoPieces()[0])
}

// finish drives a one-piece mission to done.
func finish(h *harness) {
	h.tick()
	h.runs.setRun("run-P1", doneState(), false)
	h.tick()
	h.tick()
	h.gh.setState(1, "MERGED")
	h.tick()
}

func TestEngine_NoTrackerSendsNothing(t *testing.T) {
	h := onePiece(t)
	finish(h)
	st := h.state()
	if st.Status != StatusDone || st.Jira.Epic != "" || st.Pieces[0].Jira.Issue != "" {
		t.Fatalf("state = %+v", st)
	}
	if n, err := h.e.SyncTracker(context.Background()); n != 0 || err != nil {
		t.Fatalf("SyncTracker = %d, %v", n, err)
	}
}

func TestEngine_FirstTickCreatesEpicAndIssues(t *testing.T) {
	h := newHarness(t)
	h.withTracker()
	h.tick()
	c := h.log.calls
	var jira []string
	for _, x := range c {
		if strings.HasPrefix(x, "create:") || strings.HasPrefix(x, "move:") {
			jira = append(jira, x)
		}
	}
	want := []string{"create:epic:crew-" + h.id + "-epic", "create:piece:crew-" + h.id + "-P1", "create:piece:crew-" + h.id + "-P2"}
	for i, w := range want {
		if i >= len(jira) || jira[i] != w {
			t.Fatalf("calls = %v", jira)
		}
	}
	st := h.state()
	if st.Jira.Epic != "J-1" || st.Pieces[0].Jira.Issue != "J-2" || st.Pieces[1].Jira.Issue != "J-3" {
		t.Fatalf("keys: %+v", st)
	}
	if !h.log.has("move:J-2:in-progress") || !h.log.has("move:J-1:in-progress") || h.log.has("move:J-3") {
		t.Fatalf("calls = %v", jira)
	}
}

func TestEngine_LanesFollowThePiece(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	finish(h)
	h.tick()
	if h.log.count("move:J-2:in-review") != 1 || h.log.count("move:J-2:done") != 1 {
		t.Fatalf("calls = %v", h.log.calls)
	}
	idx, done := h.log.indexes("move:J-2:in-review"), h.log.indexes("move:J-2:done")
	if idx[0] > done[0] {
		t.Fatalf("order: %v", h.log.calls)
	}
}

func TestEngine_WaitFlagsAndUnflags(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.tick()
	h.edit(func(s *State) {
		s.Pieces[0].Wait = &Wait{ID: "w9", Kind: WaitApproval, Since: testNow, Back: PieceRunning}
	})
	h.emitEv(Event{Kind: EvWaitingOnPrincipal, Piece: "P1", Msg: "approve it"})
	h.tick()
	if h.log.count("flag:J-2:true") != 1 || h.log.count("comment:J-2") != 1 {
		t.Fatalf("calls = %v", h.log.calls)
	}
	h.edit(func(s *State) { s.Pieces[0].Wait = nil })
	h.tick()
	if h.log.count("flag:J-2:false") != 1 {
		t.Fatalf("calls = %v", h.log.calls)
	}
}

func (h *harness) emitEv(ev Event) {
	h.t.Helper()
	ev.Time, ev.By = testNow, engineBy
	if _, err := h.store.Emit(ev); err != nil {
		h.t.Fatal(err)
	}
}

func TestEngine_CommentsOncePerEvent(t *testing.T) {
	h := onePiece(t)
	ft := h.withTracker()
	h.edit(func(s *State) { s.Pieces[0].PRURL = "https://github.com/acme/app/pull/7" })
	h.emitEv(Event{Kind: EvPROpened, Piece: "P1"})
	for range 3 {
		h.tick()
	}
	if h.log.count("comment:") != 1 || len(ft.comments) != 1 || ft.comments[0] != "Pull request opened: https://github.com/acme/app/pull/7" {
		t.Fatalf("calls %v comments %v", h.log.calls, ft.comments)
	}
}

func TestEngine_JiraDownNeverStopsAPiece(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.fail.failNext("create-epic", 1000)
	h.tick()
	h.runs.setRun("run-P1", doneState(), false)
	prev := jiraNotes(h)
	for _, step := range []func(){func() {}, func() {}, func() { h.gh.setState(1, "MERGED") }, func() {}} {
		step()
		h.tick()
		if n := jiraNotes(h); n-prev > 1 {
			t.Fatalf("a tick wrote %d Jira notes", n-prev)
		} else {
			prev = n
		}
	}
	if h.state().Status != StatusDone {
		t.Fatalf("state = %+v", h.state())
	}
	if jiraNotes(h) != 1 {
		t.Fatalf("notes = %d", jiraNotes(h))
	}
	h.fail.failNext("create-epic", 0)
	owed, err := h.e.SyncTracker(context.Background())
	if err != nil || owed != 0 {
		t.Fatalf("owed %d err %v", owed, err)
	}
	if p := h.piece("P1"); p.Jira.Lane != LaneDone {
		t.Fatalf("piece = %+v", p)
	}
	if st := h.state(); st.Jira.EpicLane != LaneDone {
		t.Fatalf("epic lane = %q", st.Jira.EpicLane)
	}
}

func TestEngine_PermanentMoveIsGivenUp(t *testing.T) {
	h := onePiece(t)
	ft := h.withTracker()
	ft.permanent["move:J-2"] = true
	for range 3 {
		h.tick()
	}
	if h.log.count("move:J-2") != 1 || jiraNotes(h) != 1 || h.piece("P1").Jira.Lane != LaneInProgress {
		t.Fatalf("calls %v notes %d piece %+v", h.log.calls, jiraNotes(h), h.piece("P1"))
	}
}

func TestEngine_PermanentCreateSkipsOnlyThatPiece(t *testing.T) {
	h := newHarness(t)
	ft := h.withTracker()
	ft.permanent["create:crew-"+h.id+"-P1"] = true
	h.tick()
	if h.piece("P1").Jira.Issue != "" || h.piece("P2").Jira.Issue == "" || h.piece("P2").Jira.Lane != LaneToDo {
		t.Fatalf("state = %+v", h.state())
	}
	h.tick()
	if h.log.count("create:piece:crew-"+h.id+"-P1") != 2 {
		t.Fatalf("calls = %v", h.log.calls)
	}
}

func TestEngine_SavesEachKeyAtOnce(t *testing.T) {
	h := newHarness(t)
	h.withTracker()
	h.fail.failNext("create", 1000)
	h.tick()
	if h.state().Jira.Epic != "J-1" {
		t.Fatalf("state = %+v", h.state())
	}
	for range 3 {
		h.tick()
	}
	if h.log.count("create:epic") != 1 {
		t.Fatalf("calls = %v", h.log.calls)
	}
}

func TestEngine_NoActiveSprintIsANote(t *testing.T) {
	h := newHarness(t)
	ft := h.withTracker()
	h.e.Sprints, ft.noSprint = true, true
	h.tick()
	h.tick()
	n := 0
	for _, e := range h.events(EvNote) {
		if strings.Contains(e.Msg, "no active sprint") {
			n++
		}
	}
	if n != 1 || !h.log.has("move:J-2:in-progress") || h.log.count("sprint:") != 2 {
		t.Fatalf("notes %d calls %v", n, h.log.calls)
	}
}

func deliverable(t *testing.T, h *harness, name string, size int64) {
	t.Helper()
	dir := filepath.Join(h.store.Dir, "deliverables", "P1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if size >= 0 {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(path, size); err != nil {
			t.Fatal(err)
		}
	}
	h.edit(func(s *State) { s.Pieces[0].Deliverable = &Deliverable{Files: []string{name}} })
}

func TestEngine_BigFileIsNotAttached(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.tick()
	deliverable(t, h, "big.bin", 11<<20)
	h.tick()
	h.tick()
	if h.log.has("attach:") || jiraNotes(h) != 1 || !strings.Contains(h.events(EvNote)[0].Msg, "over 10 MB") {
		t.Fatalf("calls %v notes %v", h.log.calls, h.events(EvNote))
	}
	if a := h.piece("P1").Deliverable.Attached; len(a) != 1 {
		t.Fatalf("attached = %v", a)
	}
}

func TestEngine_AttachesASmallFileOnce(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.tick()
	deliverable(t, h, "a.txt", 5)
	h.tick()
	h.tick()
	if h.log.count("attach:J-2:a.txt") != 1 {
		t.Fatalf("calls = %v", h.log.calls)
	}
}

func TestEngine_MissingFileIsGivenUp(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.tick()
	deliverable(t, h, "gone.txt", -1)
	h.tick()
	h.tick()
	if h.log.has("attach:") || jiraNotes(h) != 1 || len(h.piece("P1").Deliverable.Attached) != 1 {
		t.Fatalf("calls %v notes %v", h.log.calls, h.events(EvNote))
	}
}

func TestEngine_CancelledContextSendsNothing(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.e.SyncTracker(ctx); err != nil {
		t.Fatal(err)
	}
	if h.log.has("create:") || jiraNotes(h) != 0 {
		t.Fatalf("calls = %v", h.log.calls)
	}
}

func TestLoop_DrainsThenSaysWhatIsLeft(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.edit(func(s *State) { s.Pieces[0].Status = PieceComplete })
	h.fail.failNext("create-epic", 1000)
	h.undrive() // Loop takes the mission lock itself
	st, err := h.e.Loop(context.Background(), time.Millisecond)
	if err != nil || st.Status != StatusDone {
		t.Fatalf("st %+v err %v", st, err)
	}
	if got := h.log.count("create:epic"); got != 1+trackerDrainRuns {
		t.Fatalf("create:epic calls = %d", got)
	}
	var left []Event
	for _, e := range h.events(EvNote) {
		if strings.Contains(e.Msg, "crew jira sync "+h.id) {
			left = append(left, e)
		}
	}
	if len(left) != 1 {
		t.Fatalf("notes = %v", h.events(EvNote))
	}
}

func TestLoop_StopsDrainingWhenJiraRecovers(t *testing.T) {
	h := onePiece(t)
	h.withTracker()
	h.edit(func(s *State) { s.Pieces[0].Status = PieceComplete })
	h.fail.failNext("create-epic", 3)
	h.undrive() // Loop takes the mission lock itself
	if _, err := h.e.Loop(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := h.log.count("create:epic"); got != 4 {
		t.Fatalf("create:epic calls = %d", got)
	}
	for _, e := range h.events(EvNote) {
		if strings.Contains(e.Msg, "crew jira sync") {
			t.Fatalf("unexpected note %q", e.Msg)
		}
	}
	if st := h.state(); st.Jira.EpicLane != LaneDone || st.Pieces[0].Jira.Lane != LaneDone {
		t.Fatalf("state = %+v", st)
	}
}
