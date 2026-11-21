package mission

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
)

var prT0 = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

func oneRunning() State {
	return st(mis(nil, "P1"), PieceRunning)
}

func TestPROpened_RecordsHeadAndTime(t *testing.T) {
	s := oneRunning()
	s.Pieces[0].Problem = "tests failed: TestA"
	orig := s.Clone()
	out := PROpened(s, "P1", gh.PR{Number: 7, URL: "u"}, "abc123", prT0)
	p := piece(t, out, "P1")
	if p.Head != "abc123" || !p.PRSince.Equal(prT0) || p.PR != 7 || p.Status != PiecePROpen {
		t.Fatalf("PROpened: %+v", p)
	}
	if p.Problem != "tests failed: TestA" {
		t.Fatalf("PROpened cleared Problem: %q", p.Problem)
	}
	if !reflect.DeepEqual(s, orig) {
		t.Fatal("PROpened modified its argument")
	}
}

func TestSentBackDone_FromPROpenGoesBackToRunning(t *testing.T) {
	s := st(mis(nil, "P1"), PiecePROpen)
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitMerge, Since: prT0}
	orig := s.Clone()
	out := SentBackDone(s, "P1", reasonChecksFailed, "lint")
	p := piece(t, out, "P1")
	if p.Status != PieceRunning || p.Wait != nil || p.SentBack[reasonChecksFailed] != 1 || p.Problem != "lint" {
		t.Fatalf("SentBackDone: %+v", p)
	}
	if !reflect.DeepEqual(s, orig) {
		t.Fatal("SentBackDone modified its argument")
	}
}

func TestSentBackDone_CutsProblem(t *testing.T) {
	long := strings.Repeat("é", 400)
	p := piece(t, SentBackDone(oneRunning(), "P1", reasonConflict, long), "P1")
	if n := len([]rune(p.Problem)); n != 300 {
		t.Fatalf("problem has %d runes, want 300", n)
	}
}

func TestBranchUpdated_KeepsStatus(t *testing.T) {
	s := st(mis(nil, "P1"), PiecePROpen)
	p := piece(t, BranchUpdated(s, "P1", "def456", prT0), "P1")
	if p.Status != PiecePROpen || p.Head != "def456" || !p.PRSince.Equal(prT0) {
		t.Fatalf("BranchUpdated: %+v", p)
	}
}

func TestMerged_CompletesAndClearsWait(t *testing.T) {
	s := st(mis(nil, "P1"), PiecePROpen)
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitMerge}
	s.Pieces[0].Problem = "old"
	p := piece(t, Merged(s, "P1"), "P1")
	if p.Status != PieceComplete || p.Wait != nil || p.Problem != "old" {
		t.Fatalf("Merged: %+v", p)
	}
	if s.Pieces[0].Wait == nil {
		t.Fatal("Merged modified its argument")
	}
}

func TestState_CloneKeepsNewFields(t *testing.T) {
	s := oneRunning()
	s.Pieces[0].Head, s.Pieces[0].PRSince, s.Pieces[0].Problem = "h", prT0, "p"
	c := s.Clone()
	c.Pieces[0].Head = "changed"
	got := piece(t, s.Clone(), "P1")
	if got.Head != "h" || !got.PRSince.Equal(prT0) || got.Problem != "p" {
		t.Fatalf("Clone: %+v", got)
	}
}
