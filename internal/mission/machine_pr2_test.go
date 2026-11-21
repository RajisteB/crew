package mission

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
)

func TestDecide_GreenMustSettle(t *testing.T) {
	m, s, o := prCase(10*time.Minute, principal.AutoOn, chk("pass"))
	s1, as := decide(m, s, o)
	if len(of[Merge](as)) != 0 || !piece(t, s1, "a").GreenSince.Equal(t0) {
		t.Fatalf("first green tick: actions %#v green %v", as, piece(t, s1, "a").GreenSince)
	}
	o.Now = t0.Add(30 * time.Second)
	s2, as := decide(m, s1, o)
	if len(of[Merge](as)) != 0 || !piece(t, s2, "a").GreenSince.Equal(t0) {
		t.Fatalf("30s later: actions %#v green %v", as, piece(t, s2, "a").GreenSince)
	}
	o.Now = t0.Add(61 * time.Second)
	_, as = decide(m, s2, o)
	if got := of[Merge](as); len(got) != 1 || got[0] != (Merge{Piece: "a", Head: "abc", Checked: true}) {
		t.Fatalf("61s later: %#v", as)
	}
	// A pending check puts the minute back to the start.
	pending := withPR(o, func(p *gh.PR) { p.Checks = []gh.Check{chk("pass"), chk("pending")} })
	pending.Now = t0.Add(40 * time.Second)
	s3, as := decide(m, s1, pending)
	if len(of[Merge](as)) != 0 || !piece(t, s3, "a").GreenSince.IsZero() {
		t.Fatalf("pending tick: actions %#v green %v", as, piece(t, s3, "a").GreenSince)
	}
	o.Now = t0.Add(70 * time.Second)
	s4, as := decide(m, s3, o)
	if len(of[Merge](as)) != 0 || !piece(t, s4, "a").GreenSince.Equal(o.Now) {
		t.Fatalf("green again: actions %#v green %v", as, piece(t, s4, "a").GreenSince)
	}
}

func TestReducers_NewHeadClearsGreenSince(t *testing.T) {
	m := mis(nil, "a")
	s := st(m, PiecePROpen)
	s.Pieces[0].GreenSince = t0
	for name, next := range map[string]State{
		"PROpened":     PROpened(s, "a", gh.PR{Number: 7}, "def", t0),
		"BranchUpd":    BranchUpdated(s, "a", "def", t0),
		"SentBackDone": SentBackDone(s, "a", reasonConflict, "x"),
	} {
		if g := piece(t, next, "a").GreenSince; !g.IsZero() {
			t.Errorf("%s left GreenSince = %v", name, g)
		}
	}
	if !piece(t, s, "a").GreenSince.Equal(t0) {
		t.Fatal("input state changed")
	}
}

func TestDecide_UnknownMergeableWaitsAfterPatience(t *testing.T) {
	for _, mode := range []principal.AutoMode{principal.AutoOn, principal.AutoWithoutChecks} {
		for _, mergeable := range []string{"UNKNOWN", ""} {
			t.Run(fmt.Sprintf("%s/%q", mode, mergeable), func(t *testing.T) {
				m, s, o := prCase(5*time.Minute, mode, chk("pass"))
				o = withPR(o, func(p *gh.PR) { p.Mergeable = mergeable })
				s1, as := decide(m, s, o)
				wantNoActions(t, as)
				if piece(t, s1, "a").Wait != nil {
					t.Fatal("unexpected wait")
				}
				m, s, o = prCase(31*time.Minute, mode, chk("pass"))
				o = withPR(o, func(p *gh.PR) { p.Mergeable = mergeable })
				s1, as = decide(m, s, o)
				want := "PR #7 (a): GitHub hasn't said whether it can be merged after 30m; it is waiting on you"
				if w := piece(t, s1, "a").Wait; w == nil || w.Kind != WaitMerge || w.Question != want {
					t.Fatalf("wait = %+v", w)
				}
				if len(of[Notify](as)) != 1 || len(of[Merge](as)) != 0 {
					t.Fatalf("actions = %#v", as)
				}
			})
		}
	}
}

func TestDecide_MergeWaitTextFollowsReason(t *testing.T) {
	m, s, o := prCase(3*time.Minute, principal.AutoOn)
	s1, _ := decide(m, s, o)
	first := piece(t, s1, "a").Wait
	if first == nil || !strings.Contains(first.Question, "has no checks") {
		t.Fatalf("wait = %+v", first)
	}
	m, s, o = prCase(31*time.Minute, principal.AutoOn, chk("pending"))
	s.Pieces[0].Wait = first
	s2, as := decide(m, s, o)
	w := piece(t, s2, "a").Wait
	want := "PR #7 (a): checks are still running after 30m; crew merges it when they pass, or merge it yourself"
	if w == nil || w.Question != want || w.ID != first.ID || !w.Since.Equal(first.Since) {
		t.Fatalf("wait = %+v", w)
	}
	if len(of[Notify](as)) != 0 || len(of[Record](as)) != 0 {
		t.Fatalf("actions = %#v", as)
	}
}

func TestDecide_PRWithoutRecordedHeadSaysSo(t *testing.T) {
	m, s, o := prCase(3*time.Minute, principal.AutoOn, chk("pass"))
	s.Pieces[0].Head = ""
	s1, as := decide(m, s, o)
	want := "PR #7 (a) was opened before crew recorded what it pushed, so crew won't merge it; it is waiting on you"
	if w := piece(t, s1, "a").Wait; w == nil || w.Question != want {
		t.Fatalf("wait = %+v", w)
	}
	if len(of[Merge](as)) != 0 {
		t.Fatal("merged")
	}
}

func TestDecide_WithoutChecksNeverMergesUnsafeHead(t *testing.T) {
	cases := map[string]func(*State, *Obs){
		"foreign head":          func(s *State, o *Obs) { *o = withPR(*o, func(p *gh.PR) { p.HeadSHA = "zzz" }) },
		"empty head":            func(s *State, o *Obs) { s.Pieces[0].Head = "" },
		"unknown mergeable":     func(s *State, o *Obs) { *o = withPR(*o, func(p *gh.PR) { p.Mergeable = "UNKNOWN" }) },
		"blank mergeable":       func(s *State, o *Obs) { *o = withPR(*o, func(p *gh.PR) { p.Mergeable = "" }) },
		"conflicting mergeable": func(s *State, o *Obs) { *o = withPR(*o, func(p *gh.PR) { p.Mergeable = "CONFLICTING" }) },
	}
	for name, edit := range cases {
		for _, since := range []time.Duration{0, time.Minute, 3 * time.Minute, time.Hour} {
			t.Run(fmt.Sprintf("%s/%s", name, since), func(t *testing.T) {
				m, s, o := prCase(since, principal.AutoWithoutChecks, chk("pass"))
				edit(&s, &o)
				_, as := decide(m, s, o)
				if len(of[Merge](as)) != 0 {
					t.Fatalf("merged: %#v", as)
				}
			})
		}
	}
}

func TestDecide_LimitWaitStopsAutomation(t *testing.T) {
	m, s, o := prCase(time.Minute, principal.AutoOn, chk("pass"))
	s.Pieces[0].Wait = &Wait{ID: "w0", Kind: WaitLimit, Question: "q", Since: t0}
	_, as := decide(m, s, o)
	wantNoActions(t, as)
	o = withPR(o, func(p *gh.PR) { p.State = "MERGED" })
	s1, _ := decide(m, s, o)
	if piece(t, s1, "a").Status != PieceComplete {
		t.Fatalf("status = %s", piece(t, s1, "a").Status)
	}
}

func TestDecide_ConflictBeforeFailedChecks(t *testing.T) {
	m, s, o := prCase(time.Minute, principal.AutoOn, chk("fail"))
	o = withPR(o, func(p *gh.PR) { p.Mergeable = "CONFLICTING" })
	_, as := decide(m, s, o)
	if len(of[ResolveConflict](as)) != 1 || len(of[SendBack](as)) != 0 {
		t.Fatalf("actions = %#v", as)
	}
}

func TestPRWaits_OpensOrRefreshes(t *testing.T) {
	m := mis(nil, "a")
	s := st(m, PiecePROpen)
	s.NextWait = 4
	next, opened := PRWaits(s, "a", "q1", t0)
	w := piece(t, next, "a").Wait
	if !opened || w == nil || w.Kind != WaitMerge || w.Question != "q1" || w.ID != "w4" || !w.Since.Equal(t0) || w.Back != "" || next.NextWait != 5 {
		t.Fatalf("opened=%v wait=%+v next=%d", opened, w, next.NextWait)
	}
	if piece(t, s, "a").Wait != nil || s.NextWait != 4 {
		t.Fatal("input state changed")
	}
	again, opened := PRWaits(next, "a", "q2", t0.Add(time.Hour))
	w2 := piece(t, again, "a").Wait
	if opened || w2.Question != "q2" || w2.ID != "w4" || !w2.Since.Equal(t0) || again.NextWait != 5 {
		t.Fatalf("opened=%v wait=%+v", opened, w2)
	}
	if piece(t, next, "a").Wait.Question != "q1" {
		t.Fatal("input state changed")
	}
	q := s.Clone()
	q.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitQuestion, Question: "keep", Since: t0}
	same, opened := PRWaits(q, "a", "q3", t0)
	if opened || piece(t, same, "a").Wait.Question != "keep" {
		t.Fatalf("question wait touched: %+v", piece(t, same, "a").Wait)
	}
	run := st(m, PieceRunning)
	if got, opened := PRWaits(run, "a", "q", t0); opened || piece(t, got, "a").Wait != nil {
		t.Fatal("non-pr-open piece changed")
	}
}
