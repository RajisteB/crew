package mission

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLane(t *testing.T) {
	cases := []struct {
		name string
		p    PieceState
		want string
	}{
		{"queued", PieceState{Status: PieceQueued}, LaneToDo},
		{"waiting back queued", PieceState{Status: PieceWaiting, Wait: &Wait{Back: PieceQueued}}, LaneToDo},
		{"running", PieceState{Status: PieceRunning}, LaneInProgress},
		{"waiting back running", PieceState{Status: PieceWaiting, Wait: &Wait{Back: PieceRunning}}, LaneInProgress},
		{"waiting no back", PieceState{Status: PieceWaiting, Wait: &Wait{}}, LaneInProgress},
		{"pr-open", PieceState{Status: PiecePROpen}, LaneInReview},
		{"delivered", PieceState{Status: PieceDelivered}, LaneInReview},
		{"complete", PieceState{Status: PieceComplete}, LaneDone},
		{"cancelled", PieceState{Status: PieceCancelled}, LaneDone},
	}
	for _, c := range cases {
		if got := Lane(c.p); got != c.want {
			t.Errorf("%s: Lane = %q, want %q", c.name, got, c.want)
		}
	}
}

func twoPieceMission() Mission {
	return Mission{Request: "Do the thing", Pieces: []Piece{{ID: "P1", Brief: "one"}, {ID: "P2", Brief: "two"}}}
}

func kinds(ops []TrackerOp) []string {
	var out []string
	for _, o := range ops {
		out = append(out, o.Kind+" "+o.Piece+" "+o.Lane+o.File)
	}
	return out
}

func TestTrackerOps_FreshMission(t *testing.T) {
	m := twoPieceMission()
	s := State{MissionID: "m", Status: StatusRunning, Pieces: []PieceState{{ID: "P1", Status: PieceQueued}, {ID: "P2", Status: PieceQueued}}}
	ops := TrackerOps(m, s, nil, false)
	if len(ops) != 1 || ops[0].Kind != "create-epic" {
		t.Fatalf("got %v", ops)
	}
	s.Jira.Epic = "ACME-1"
	want := []string{"create P1 ", "create P2 ", "move-epic  in-progress"}
	if got := kinds(TrackerOps(m, s, nil, false)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func trackerSettled(s State) State {
	s.Jira = TrackerState{Epic: "ACME-1", EpicLane: epicLane(s)}
	for i := range s.Pieces {
		s.Pieces[i].Jira = Tracked{Issue: "ACME-" + s.Pieces[i].ID, Lane: Lane(s.Pieces[i]), Flagged: s.Pieces[i].Wait != nil, Sprint: true}
	}
	return s
}

func TestTrackerOps_NothingOwed(t *testing.T) {
	s := trackerSettled(State{Status: StatusRunning, Pieces: []PieceState{
		{ID: "P1", Status: PieceRunning},
		{ID: "P2", Status: PieceWaiting, Wait: &Wait{Back: PieceQueued}},
	}})
	if ops := TrackerOps(twoPieceMission(), s, nil, true); len(ops) != 0 {
		t.Fatalf("got %v", ops)
	}
}

func TestTrackerOps_Order(t *testing.T) {
	s := State{Status: StatusRunning, Jira: TrackerState{Epic: "ACME-1", EpicLane: LaneInProgress}, Pieces: []PieceState{
		{ID: "P1", Status: PieceComplete, Jira: Tracked{Issue: "ACME-2", Lane: LaneInReview}},
		{ID: "P2", Status: PieceDelivered, Wait: &Wait{Kind: WaitDeliverable}, Jira: Tracked{Issue: "ACME-3", Lane: LaneInReview},
			Deliverable: &Deliverable{Files: []string{"a.md", "b.md"}, Attached: []string{"a.md"}, Posted: time.Now()}},
	}}
	evs := []Event{
		{Seq: 4, Kind: EvDecision, Piece: "P1", By: "engine", Msg: "ok"},
		{Seq: 5, Kind: EvNote, Msg: "skip"},
		{Seq: 6, Kind: EvAmendment, Msg: "more"},
	}
	ops := TrackerOps(twoPieceMission(), s, evs, true)
	if len(ops) != 6 {
		t.Fatalf("got %v", ops)
	}
	if ops[0].Kind != "sprint" || !reflect.DeepEqual(ops[0].Pieces, []string{"P1", "P2"}) || ops[0].Piece != "" {
		t.Errorf("sprint: %+v", ops[0])
	}
	if ops[1].Kind != "move" || ops[1].Piece != "P1" || ops[1].Lane != LaneDone {
		t.Errorf("move: %+v", ops[1])
	}
	if ops[2].Kind != "flag" || ops[2].Piece != "P2" || !ops[2].On {
		t.Errorf("flag: %+v", ops[2])
	}
	if ops[3].Kind != "attach" || ops[3].Piece != "P2" || ops[3].File != "b.md" {
		t.Errorf("attach: %+v", ops[3])
	}
	if ops[4].Kind != "comment" || ops[4].Piece != "P1" || ops[4].Seq != 4 || ops[4].Text != "Decision (engine): ok" {
		t.Errorf("comment 1: %+v", ops[4])
	}
	if ops[5].Kind != "comment" || ops[5].Piece != "" || ops[5].Seq != 6 {
		t.Errorf("comment 2: %+v", ops[5])
	}
}

func TestTrackerOps_NoSprintOpWithoutSprints(t *testing.T) {
	s := trackerSettled(State{Status: StatusRunning, Pieces: []PieceState{{ID: "P1", Status: PieceRunning}}})
	s.Pieces[0].Jira.Sprint = false
	if ops := TrackerOps(twoPieceMission(), s, nil, false); len(ops) != 0 {
		t.Fatalf("got %v", ops)
	}
	if ops := TrackerOps(twoPieceMission(), s, nil, true); len(ops) != 1 || ops[0].Kind != "sprint" {
		t.Fatalf("got %v", ops)
	}
}

func TestTrackerOps_FlagClears(t *testing.T) {
	s := trackerSettled(State{Status: StatusRunning, Pieces: []PieceState{{ID: "P1", Status: PieceRunning}}})
	s.Pieces[0].Jira.Flagged = true
	ops := TrackerOps(twoPieceMission(), s, nil, false)
	if len(ops) != 1 || ops[0].Kind != "flag" || ops[0].On {
		t.Fatalf("got %v", ops)
	}
}

func TestTrackerOps_EpicDone(t *testing.T) {
	s := trackerSettled(State{Status: StatusRunning, Pieces: []PieceState{{ID: "P1", Status: PieceComplete}}})
	s.Status = StatusDone
	ops := TrackerOps(twoPieceMission(), s, nil, false)
	if len(ops) != 1 || ops[0].Kind != "move-epic" || ops[0].Lane != LaneDone {
		t.Fatalf("got %v", ops)
	}
}

func TestTrackerComment(t *testing.T) {
	s := State{Pieces: []PieceState{{ID: "P1", PRURL: "https://example.com/pr/1"}}}
	cases := []struct {
		ev    Event
		piece string
		text  string
	}{
		{Event{Kind: EvPROpened, Piece: "P1"}, "P1", "Pull request opened: https://example.com/pr/1"},
		{Event{Kind: EvDecision, Piece: "P1", By: "engine", Msg: "m"}, "P1", "Decision (engine): m"},
		{Event{Kind: EvEscalated, Piece: "P1", Msg: "m"}, "P1", "Escalated to you: m"},
		{Event{Kind: EvWaitingOnPrincipal, Piece: "P1", Msg: "m"}, "P1", "Waiting on you: m"},
		{Event{Kind: EvRebrief, Piece: "P1", By: "principal"}, "P1", "Brief changed by principal."},
		{Event{Kind: EvPieceCancelled, Piece: "P1", Msg: "m"}, "P1", "Cancelled: m"},
		{Event{Kind: EvDeliverablePosted, Piece: "P1", Msg: "m"}, "P1", "Deliverable posted: m. A copy is in .crew/missions/mid/deliverables/P1."},
		{Event{Kind: EvDeliverableAccepted, Piece: "P1", By: "engine", Msg: "m"}, "P1", "Deliverable accepted by engine: m"},
		{Event{Kind: EvAmendment, Msg: "m"}, "", "Amendment by the principal: m"},
	}
	s.MissionID = "mid"
	for _, c := range cases {
		piece, text, ok := TrackerComment(c.ev, s)
		if !ok || piece != c.piece || text != c.text {
			t.Errorf("%s: got (%q, %q, %v)", c.ev.Kind, piece, text, ok)
		}
	}
	for _, k := range []string{EvPRMerged, EvNote, EvNotified, EvPieceStart} {
		if _, _, ok := TrackerComment(Event{Kind: k, Piece: "P1"}, s); ok {
			t.Errorf("%s should not comment", k)
		}
	}
}

func TestTrackerText(t *testing.T) {
	long := strings.Repeat("é", 200)
	m := Mission{Request: "\n\n" + long + "\nmore", Pieces: []Piece{{ID: "P3", Domain: "d", Factory: "f", Brief: long, WaitsFor: []string{"P1", "P2"}}}}
	sum, body := EpicText("m1", m)
	if n := len([]rune(sum)); n != 120 {
		t.Errorf("epic summary %d runes", n)
	}
	if !strings.HasPrefix(body, "Mission m1\n\n") || !strings.HasSuffix(body, m.Request) {
		t.Errorf("epic body %q", body)
	}
	sum, body = IssueText("m1", m, m.Pieces[0])
	if n := len([]rune(sum)); n != 120 || !strings.HasPrefix(sum, "P3: ") {
		t.Errorf("issue summary %d runes: %q", n, sum)
	}
	if !strings.Contains(body, "Mission m1, piece P3\nDomain: d\nFactory: f\nWaits for: P1, P2\n\n") {
		t.Errorf("issue body %q", body)
	}
	m.Pieces[0].WaitsFor = nil
	if _, body = IssueText("m1", m, m.Pieces[0]); !strings.Contains(body, "Waits for: nothing\n") {
		t.Errorf("body %q", body)
	}
	if TrackerLabel("m1", "P3") != "crew-m1-P3" || TrackerLabel("m1", "") != "crew-m1-epic" {
		t.Error("label")
	}
	if DeliverablesRel("m1", "P3") != ".crew/missions/m1/deliverables/P3" {
		t.Error("rel")
	}
}

func TestTrackerComment_CutsTo1000Runes(t *testing.T) {
	_, text, _ := TrackerComment(Event{Kind: EvEscalated, Piece: "P1", Msg: strings.Repeat("é", 2000)}, State{})
	if n := len([]rune(text)); n != 1000 {
		t.Errorf("got %d runes", n)
	}
}
