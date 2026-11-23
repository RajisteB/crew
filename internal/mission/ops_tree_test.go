package mission

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPieceInput_NamesWhatItIsPartOf(t *testing.T) {
	m := Mission{Request: "req", Pieces: []Piece{
		{ID: "P3", Delegate: true, Brief: "build the invite flow"},
		{ID: "P3.2", Delegate: true, Brief: "the screens"},
		{ID: "P3.2.1", Brief: "the first screen"},
	}}
	got := PieceInput("m-1", m, m.Pieces[2], sourceDomain("d"), nil)
	near := strings.Index(got, "## The piece yours is part of: P3.2\n\nthe screens")
	far := strings.Index(got, "## The piece yours is part of: P3\n\nbuild the invite flow")
	mine := strings.Index(got, "## Your piece: P3.2.1")
	whole := strings.Index(got, "## The whole request")
	if mine < 0 || near < mine || far < near || whole < far {
		t.Fatalf("order wrong (mine %d near %d far %d whole %d):\n%s", mine, near, far, whole, got)
	}
	top := PieceInput("m-1", m, m.Pieces[0], sourceDomain("d"), nil)
	if strings.Contains(top, "is part of") {
		t.Fatalf("a top-level piece is part of nothing:\n%s", top)
	}
}

func TestPrepareScratch_HandsOverWhatADelegatedPieceProduced(t *testing.T) {
	h := newHarness(t)
	m := Mission{Pieces: []Piece{
		{ID: "P3", Delegate: true},
		{ID: "P3.1"},
		{ID: "P3.2"},
		{ID: "P4", WaitsFor: []string{"P3"}},
	}}
	s := State{MissionID: h.id, Pieces: []PieceState{
		{ID: "P3", Status: PieceDelegated},
		{ID: "P3.1", Status: PieceComplete, Deliverable: &Deliverable{Files: []string{"a.md"}}},
		{ID: "P3.2", Status: PieceComplete},
		{ID: "P4", Status: PieceQueued},
	}}
	writeFile(t, filepath.Join(h.store.DeliverablesDir("P3.1"), "a.md"), "from the sub-piece")
	tk := &tick{e: h.e, m: m, s: s}
	wt := t.TempDir()

	earlier, err := tk.prepareScratch(m.Pieces[3], wt)
	if err != nil {
		t.Fatal(err)
	}
	if len(earlier) != 1 || earlier[0].Piece != "P3.1" || earlier[0].Files[0] != "a.md" {
		t.Fatalf("earlier = %+v", earlier)
	}
	b, err := os.ReadFile(filepath.Join(wt, ".crew", "inputs", "P3.1", "a.md"))
	if err != nil || string(b) != "from the sub-piece" {
		t.Fatalf("copy: %q %v", b, err)
	}
}

func TestIssueText_DelegatedAndSubPiece(t *testing.T) {
	m := Mission{Pieces: []Piece{
		{ID: "P3", Domain: "d", Delegate: true, Brief: "the whole flow"},
		{ID: "P3.1", Domain: "d", Factory: "f", Brief: "one part"},
	}}
	_, body := IssueText("m1", m, m.Pieces[0])
	if !strings.Contains(body, "Factory: none (delegated: its pieces are planned when it is ready)\n") {
		t.Fatalf("delegated body:\n%s", body)
	}
	if strings.Contains(body, "Part of:") {
		t.Fatalf("top-level piece has no parent:\n%s", body)
	}
	_, body = IssueText("m1", m, m.Pieces[1])
	if !strings.Contains(body, "Mission m1, piece P3.1\nPart of: P3\nDomain: d\nFactory: f\n") {
		t.Fatalf("sub-piece body:\n%s", body)
	}
}

func TestTrackerOps_CreatesAnIssueForEachSubPiece(t *testing.T) {
	m := Mission{Request: "r", Pieces: []Piece{
		{ID: "P3", Delegate: true}, {ID: "P3.1"}, {ID: "P3.2"},
	}}
	s := State{MissionID: "m", Status: StatusRunning, Jira: TrackerState{Epic: "ACME-1", EpicLane: LaneInProgress},
		Pieces: []PieceState{
			{ID: "P3", Status: PieceDelegated, Jira: Tracked{Issue: "ACME-3", Lane: LaneToDo, Sprint: true}},
			{ID: "P3.1", Status: PieceQueued},
			{ID: "P3.2", Status: PieceQueued},
		}}
	got := strings.Join(kinds(TrackerOps(m, s, nil, false)), "|")
	want := "create P3.1 |create P3.2 |move P3 in-progress"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
