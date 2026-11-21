package mission

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
)

var testNow = time.Date(2026, 11, 21, 10, 30, 0, 0, time.UTC)

func testMission() Mission {
	return Mission{Request: "do it", Pieces: []Piece{
		{ID: "P1", Domain: "product", Factory: "f", Brief: "b"},
		{ID: "P2", Domain: "product", Factory: "f", Brief: "b", WaitsFor: []string{"P1"}},
	}}
}

func newTestStore(t *testing.T) (string, Store, State) {
	t.Helper()
	root := t.TempDir()
	s, st, err := New(root, testMission(), gh.RepoInfo{Slug: "o/n", DefaultBranch: "main"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	return root, s, st
}

func TestStore_NewWritesMissionAndState(t *testing.T) {
	root, s, st := newTestStore(t)
	if st.Status != StatusPlanned || st.Repo != "o/n" || st.Base != "main" || st.NextWait != 1 {
		t.Errorf("state = %+v", st)
	}
	for _, p := range st.Pieces {
		if p.Status != PieceQueued {
			t.Errorf("piece %s = %s", p.ID, p.Status)
		}
	}
	if st.Pieces[0].ID != "P1" || st.Pieces[1].ID != "P2" {
		t.Errorf("order = %+v", st.Pieces)
	}
	if !strings.HasPrefix(st.MissionID, "m-20261121-103000-") || len(st.MissionID) != len("m-20261121-103000-abcd") {
		t.Errorf("id = %q", st.MissionID)
	}
	m, err := s.Mission()
	if err != nil || m.Request != "do it" || len(m.Pieces) != 2 {
		t.Errorf("mission = %+v, %v", m, err)
	}
	loaded, err := s.LoadState()
	if err != nil || !reflect.DeepEqual(loaded, st) {
		t.Errorf("loaded = %+v, %v", loaded, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".crew", ".gitignore")); err != nil {
		t.Error(".crew/.gitignore missing")
	}
	if fi, _ := os.Stat(s.Dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Join(s.Dir, "state.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("state mode = %v", fi.Mode().Perm())
	}
}

func TestStore_StateRoundTrip(t *testing.T) {
	_, s, st := newTestStore(t)
	st.Status = StatusRunning
	st.Pieces[0] = PieceState{
		ID: "P1", Status: PieceWaiting, PR: 7, PRURL: "u", Resumes: 2,
		SentBack:  map[string]int{"tests": 1},
		Decisions: []Decision{{Time: testNow, By: "engine", Action: "retry"}},
		Wait:      &Wait{ID: "w1", Kind: WaitMerge, Since: testNow, Back: PieceRunning},
		Added:     true,
	}
	if err := s.SaveState(st); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadState()
	if err != nil || !reflect.DeepEqual(got, st) {
		t.Errorf("got %+v, %v", got, err)
	}
	if p, ok := got.Piece("P1"); !ok || p.PR != 7 {
		t.Error("Piece(P1)")
	}
	if _, ok := got.Piece("nope"); ok {
		t.Error("Piece(nope) found")
	}
}

func TestState_CloneIsDeep(t *testing.T) {
	_, _, st := newTestStore(t)
	st.Pieces[0].SentBack = map[string]int{"a": 1}
	st.Pieces[0].Decisions = []Decision{{By: "engine"}}
	st.Pieces[0].Wait = &Wait{ID: "w"}
	c := st.Clone()
	c.Pieces[0].Status = PieceComplete
	c.Pieces[0].SentBack["a"] = 9
	c.Pieces[0].Decisions[0].By = "x"
	c.Pieces[0].Wait.ID = "changed"
	if st.Pieces[0].Status != PieceQueued || st.Pieces[0].SentBack["a"] != 1 ||
		st.Pieces[0].Decisions[0].By != "engine" || st.Pieces[0].Wait.ID != "w" {
		t.Errorf("original changed: %+v", st.Pieces[0])
	}
	p, _ := st.Piece("P1")
	p.SentBack["a"] = 5
	p.Wait.ID = "z"
	if st.Pieces[0].SentBack["a"] != 1 || st.Pieces[0].Wait.ID != "w" {
		t.Error("Piece returned an alias")
	}
}

func TestStore_LockExclusiveAndStaleTakenOver(t *testing.T) {
	_, s, _ := newTestStore(t)
	unlock, err := s.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if pid, alive := s.LockedBy(); pid != os.Getpid() || !alive {
		t.Errorf("LockedBy = %d %v", pid, alive)
	}
	_, err = s.Lock()
	if err == nil || !strings.Contains(err.Error(), "is already being driven (pid") {
		t.Errorf("second lock err = %v", err)
	}
	unlock()
	if _, err := os.Stat(filepath.Join(s.Dir, "lock")); err == nil {
		t.Error("lock not removed")
	}
	// a dead pid is taken over
	if err := os.WriteFile(filepath.Join(s.Dir, "lock"), []byte("2147483646"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, alive := s.LockedBy(); alive {
		t.Skip("pid unexpectedly alive")
	}
	unlock2, err := s.Lock()
	if err != nil {
		t.Fatalf("stale lock not taken over: %v", err)
	}
	// unlock only removes our own pid
	os.WriteFile(filepath.Join(s.Dir, "lock"), []byte("1"), 0o600)
	unlock2()
	if _, err := os.Stat(filepath.Join(s.Dir, "lock")); err != nil {
		t.Error("unlock removed a lock it no longer owned")
	}
}

func TestStore_InboxOrderAndAck(t *testing.T) {
	_, s, _ := newTestStore(t)
	if err := s.Drop("answer", map[string]string{"id": "w1", "text": "yes"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Drop("amend", map[string]string{"text": "also x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Drop("bogus", 1); err == nil {
		t.Error("bogus kind accepted")
	}
	os.WriteFile(filepath.Join(s.Dir, "inbox", ".tmp-1"), []byte("x"), 0o600)
	items, err := s.Inbox()
	if err != nil || len(items) != 2 {
		t.Fatalf("items = %+v, %v", items, err)
	}
	if items[0].Kind != "answer" || items[1].Kind != "amend" || items[0].Name >= items[1].Name {
		t.Errorf("order = %+v", items)
	}
	if !strings.Contains(string(items[0].Body), `"yes"`) {
		t.Errorf("body = %s", items[0].Body)
	}
	again, _ := s.Inbox()
	if len(again) != 2 {
		t.Error("items did not survive a read")
	}
	if err := s.Ack(items[:1]); err != nil {
		t.Fatal(err)
	}
	left, _ := s.Inbox()
	if len(left) != 1 || left[0].Kind != "amend" {
		t.Errorf("left = %+v", left)
	}
}

func TestStore_EventsSinceAssignsSeq(t *testing.T) {
	_, s, _ := newTestStore(t)
	e1, err := s.Emit(Event{Kind: EvMissionStart})
	if err != nil || e1.Seq != 1 || e1.Time.IsZero() {
		t.Fatalf("e1 = %+v, %v", e1, err)
	}
	e2, _ := s.Emit(Event{Kind: EvPieceStart, Piece: "P1", Msg: "m"})
	if e2.Seq != 2 {
		t.Errorf("seq = %d", e2.Seq)
	}
	got, err := s.EventsSince(1)
	if err != nil || len(got) != 1 || got[0].Piece != "P1" {
		t.Errorf("since 1 = %+v, %v", got, err)
	}
	all, _ := s.EventsSince(0)
	if len(all) != 2 {
		t.Errorf("all = %d", len(all))
	}
}

func TestStore_SaveApprovedCopiesPlan(t *testing.T) {
	_, s, _ := newTestStore(t)
	if _, err := os.Stat(filepath.Join(s.Dir, "approved.md")); err == nil {
		t.Fatal("approved.md exists before approval")
	}
	if err := s.SaveApproved(); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(s.Dir, "approved.md"))
	m, _ := os.ReadFile(filepath.Join(s.Dir, "mission.md"))
	if string(a) != string(m) || len(a) == 0 {
		t.Error("approved.md differs from mission.md")
	}
	if err := s.SaveMission(testMission().WithChange(Change{Time: testNow, By: "principal", What: "w"})); err != nil {
		t.Fatal(err)
	}
	a2, _ := os.ReadFile(filepath.Join(s.Dir, "approved.md"))
	if string(a2) != string(a) {
		t.Error("SaveMission changed approved.md")
	}
}

func TestOpen_RejectsPathLikeIDs(t *testing.T) {
	root, s, st := newTestStore(t)
	for _, id := range []string{"../x", "", "m-1", "m-20261121-103000-ABCD", "m-20261121-103000-abcd/..", st.MissionID + "x"} {
		if _, err := Open(root, id); err == nil {
			t.Errorf("Open(%q) accepted", id)
		}
	}
	got, err := Open(root, st.MissionID)
	if err != nil || got.Dir != s.Dir {
		t.Errorf("Open = %+v, %v", got, err)
	}
}

func TestList_NewestFirstSkipsBroken(t *testing.T) {
	root := t.TempDir()
	info := gh.RepoInfo{Slug: "o/n", DefaultBranch: "main"}
	a, _, _ := New(root, testMission(), info, testNow)
	b, _, _ := New(root, testMission(), info, testNow.Add(time.Hour))
	os.MkdirAll(filepath.Join(root, ".crew", "missions", "m-20200101-000000-0000"), 0o700)
	got, err := List(root)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %d, %v", len(got), err)
	}
	if got[0].MissionID != filepath.Base(b.Dir) || got[1].MissionID != filepath.Base(a.Dir) {
		t.Errorf("order = %s, %s", got[0].MissionID, got[1].MissionID)
	}
	if none, err := List(t.TempDir()); err != nil || len(none) != 0 {
		t.Errorf("empty = %v, %v", none, err)
	}
}
