package mission

import (
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
)

func TestDecide_MergedCompletesAndUnblocks(t *testing.T) {
	m := mis(map[string][]string{"P2": {"P1"}}, "P1", "P2")
	s := st(m, PiecePROpen, PieceQueued)
	s.Pieces[0].PR = 12
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitMerge, Since: t0}
	s.NextWait = 2
	o := obs(t0.Add(time.Minute))
	o.PRs["P1"] = gh.PR{Number: 12, State: "MERGED"}
	ns, as := Decide(m, s, o, limits(1, 3), 0)
	p1 := piece(t, ns, "P1")
	if p1.Status != PieceComplete || p1.Wait != nil {
		t.Fatalf("P1 should be complete with no wait: %+v", p1)
	}
	cl := of[Cleanup](as)
	if len(cl) != 1 || !cl[0].Force {
		t.Fatalf("want forced Cleanup, got %+v", cl)
	}
	if !hasKind(as, EvPRMerged) || !hasKind(as, EvPieceComplete) {
		t.Fatalf("want pr-merged and piece-complete, got %v", recordKinds(as))
	}
	if st := of[StartPiece](as); len(st) != 1 || st[0].Piece != "P2" {
		t.Fatalf("P2 should start on the same call, got %+v", as)
	}
}

func TestDecide_OpenPRNotifiesOnceThenReminds(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PiecePROpen)
	s.Pieces[0].PR = 12
	remind := 30 * time.Minute
	open := func(at time.Duration) Obs {
		o := obs(t0.Add(at))
		o.PRs["P1"] = gh.PR{Number: 12, State: "OPEN"}
		return o
	}
	s, as := Decide(m, s, open(0), limits(1, 3), remind)
	n := of[Notify](as)
	if len(n) != 1 || n[0].Body != "PR #12 (P1) is waiting on you" {
		t.Fatalf("want first notification, got %+v", as)
	}
	w := piece(t, s, "P1").Wait
	if w == nil || w.Kind != WaitMerge || piece(t, s, "P1").Status != PiecePROpen {
		t.Fatalf("want merge wait on a pr-open piece: %+v", w)
	}
	s, as = Decide(m, s, open(29*time.Minute), limits(1, 3), remind)
	if len(of[Notify](as)) != 0 {
		t.Fatalf("no reminder at 29m: %+v", as)
	}
	s, as = Decide(m, s, open(30*time.Minute), limits(1, 3), remind)
	if n := of[Notify](as); len(n) != 1 || n[0].Body != "PR #12 (P1) is waiting on you" {
		t.Fatalf("want reminder at 30m: %+v", as)
	}
	s, as = Decide(m, s, open(59*time.Minute), limits(1, 3), remind)
	if len(of[Notify](as)) != 0 {
		t.Fatalf("no reminder at 59m: %+v", as)
	}
	_, as = Decide(m, s, open(60*time.Minute), limits(1, 3), remind)
	if len(of[Notify](as)) != 1 {
		t.Fatalf("want reminder at 60m: %+v", as)
	}
}

func TestDecide_ZeroRemindNeverReminds(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PiecePROpen)
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitMerge, Since: t0}
	o := obs(t0.Add(100 * time.Hour))
	o.PRs["P1"] = gh.PR{Number: 12, State: "OPEN"}
	_, as := Decide(m, s, o, limits(1, 3), 0)
	if len(of[Notify](as)) != 0 {
		t.Fatalf("remind 0 must not notify: %+v", as)
	}
}

func TestDecide_ReminderShiftDoesNotOverflow(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PiecePROpen)
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitMerge, Since: t0, Reminded: 200}
	o := obs(t0.Add(1000 * time.Hour))
	o.PRs["P1"] = gh.PR{Number: 12, State: "OPEN"}
	_, as := Decide(m, s, o, limits(1, 3), time.Minute)
	if len(of[Notify](as)) != 0 {
		t.Fatalf("overflowed shift must not notify: %+v", as)
	}
}

func TestDecide_ReminderForQuestionUsesQuestion(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceWaiting)
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitQuestion, Question: "which db?", Since: t0, Back: PieceRunning}
	_, as := Decide(m, s, obs(t0.Add(time.Hour)), limits(1, 3), 30*time.Minute)
	n := of[Notify](as)
	if len(n) != 1 || !strings.Contains(n[0].Body, "which db?") {
		t.Fatalf("want question reminder, got %+v", as)
	}
}

func TestDecide_ClosedPRWaits(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PiecePROpen)
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitMerge, Since: t0}
	s.NextWait = 2
	o := obs(t0.Add(time.Minute))
	o.PRs["P1"] = gh.PR{Number: 12, State: "CLOSED"}
	ns, as := Decide(m, s, o, limits(1, 3), 0)
	p := piece(t, ns, "P1")
	if p.Status != PiecePROpen || p.Wait == nil || p.Wait.Kind != WaitPRClosed || p.Wait.ID != "w2" {
		t.Fatalf("want pr-closed wait replacing merge wait: %+v", p)
	}
	if !hasKind(as, EvWaitingOnPrincipal) || len(of[Notify](as)) != 1 {
		t.Fatalf("want record and Notify: %+v", as)
	}
	// Seen again: no second wait.
	ns2, as := Decide(m, ns, o, limits(1, 3), 0)
	if piece(t, ns2, "P1").Wait.ID != "w2" || len(as) != 0 {
		t.Fatalf("closed PR must not re-wait: %+v", as)
	}
}

func TestDecide_PROpenWithoutObservationDoesNothing(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PiecePROpen)
	_, as := Decide(m, s, obs(t0), limits(1, 3), 0)
	if len(as) != 0 {
		t.Fatalf("want nothing, got %+v", as)
	}
}

func TestDecide_QueuedBehindCancelledWaitsOnPrincipal(t *testing.T) {
	m := mis(map[string][]string{"P2": {"P1"}}, "P1", "P2")
	s := st(m, PieceCancelled, PieceQueued)
	ns, as := Decide(m, s, obs(t0), limits(1, 3), 0)
	p := piece(t, ns, "P2")
	want := "P2 waits for P1, which was cancelled; cancel it or amend the mission"
	if p.Status != PieceWaiting || p.Wait == nil || p.Wait.Kind != WaitQuestion || p.Wait.Question != want || p.Wait.Back != PieceQueued {
		t.Fatalf("bad wait: %+v", p)
	}
	if !hasKind(as, EvWaitingOnPrincipal) || len(of[Notify](as)) != 1 {
		t.Fatalf("want record and Notify: %+v", as)
	}
	if ns.Status != StatusRunning {
		t.Fatal("a waiting piece must keep the mission from finishing")
	}
}
