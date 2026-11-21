package mission

import (
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
)

// prCase builds a one-piece mission whose piece "a" is pr-open at head "abc",
// pushed `since` ago, with an observed pull request.
func prCase(since time.Duration, mode principal.AutoMode, checks ...gh.Check) (Mission, State, Obs) {
	m := mis(nil, "a")
	s := st(m, PiecePROpen)
	s.Pieces[0].PR = 7
	s.Pieces[0].Head = "abc"
	s.Pieces[0].PRSince = t0.Add(-since)
	o := obs(t0)
	o.PRs["a"] = gh.PR{Number: 7, State: "OPEN", Mergeable: "MERGEABLE", HeadSHA: "abc", Checks: checks}
	o.Auto = map[string]AutoObs{"a": {Mode: mode}}
	return m, s, o
}

func chk(state string) gh.Check {
	return gh.Check{Name: "ci-" + state, State: state, URL: "http://x/" + state}
}

func withPR(o Obs, f func(*gh.PR)) Obs {
	pr := o.PRs["a"]
	f(&pr)
	o.PRs = map[string]gh.PR{"a": pr}
	return o
}

func decide(m Mission, s State, o Obs) (State, []Action) {
	return Decide(m, s, o, limits(1, 5), 0)
}

func wantNoActions(t *testing.T, as []Action) {
	t.Helper()
	if len(as) != 0 {
		t.Fatalf("want no actions, got %#v", as)
	}
}

func TestDecide_OffOpensMergeWaitOnce(t *testing.T) {
	m, s, o := prCase(0, principal.AutoOff)
	s1, as := decide(m, s, o)
	if w := piece(t, s1, "a").Wait; w == nil || w.Kind != WaitMerge || w.Question != "PR #7 (a) is waiting on you" {
		t.Fatalf("wait = %+v", w)
	}
	if len(of[Notify](as)) != 1 {
		t.Fatalf("notifies = %d", len(of[Notify](as)))
	}
	s2, as2 := decide(m, s1, o)
	if len(of[Notify](as2)) != 0 || piece(t, s2, "a").Wait.ID != piece(t, s1, "a").Wait.ID {
		t.Fatalf("second call changed things: %#v", as2)
	}
}

func TestDecide_OnMergesWhenChecksPass(t *testing.T) {
	m, s, o := prCase(time.Minute, principal.AutoOn, chk("pass"), chk("skip"))
	s1, as := decide(m, s, o)
	got := of[Merge](as)
	if len(got) != 1 || got[0] != (Merge{Piece: "a", Head: "abc", Checked: true}) {
		t.Fatalf("merges = %#v", got)
	}
	if piece(t, s1, "a").Wait != nil {
		t.Fatal("unexpected wait")
	}
}

func TestDecide_OnHoldsWhilePending(t *testing.T) {
	m, s, o := prCase(5*time.Minute, principal.AutoOn, chk("pending"), chk("pass"))
	s1, as := decide(m, s, o)
	wantNoActions(t, as)
	if piece(t, s1, "a").Wait != nil {
		t.Fatal("unexpected wait")
	}
	m, s, o = prCase(31*time.Minute, principal.AutoOn, chk("pending"))
	s1, _ = decide(m, s, o)
	want := "PR #7 (a): checks are still running after 30m; crew merges it when they pass, or merge it yourself"
	if w := piece(t, s1, "a").Wait; w == nil || w.Question != want {
		t.Fatalf("wait = %+v", w)
	}
	o = withPR(o, func(p *gh.PR) { p.Checks = []gh.Check{chk("pass")} })
	_, as = decide(m, s1, o)
	if len(of[Merge](as)) != 1 {
		t.Fatalf("want merge with wait held, got %#v", as)
	}
}

func TestDecide_NoChecksWaitsAfterGrace(t *testing.T) {
	m, s, o := prCase(time.Minute, principal.AutoOn)
	s1, as := decide(m, s, o)
	wantNoActions(t, as)
	if piece(t, s1, "a").Wait != nil {
		t.Fatal("unexpected wait")
	}
	m, s, o = prCase(3*time.Minute, principal.AutoOn)
	s1, as = decide(m, s, o)
	want := "PR #7 (a) has no checks, and auto-merge needs at least one to pass; it is waiting on you"
	if w := piece(t, s1, "a").Wait; w == nil || w.Question != want {
		t.Fatalf("wait = %+v", w)
	}
	if len(of[Notify](as)) != 1 || len(of[Merge](as)) != 0 {
		t.Fatalf("actions = %#v", as)
	}
}

func TestDecide_SkipsAloneDontCount(t *testing.T) {
	m, s, o := prCase(3*time.Minute, principal.AutoOn, chk("skip"))
	s1, as := decide(m, s, o)
	if len(of[Merge](as)) != 0 {
		t.Fatal("merged on skips alone")
	}
	if w := piece(t, s1, "a").Wait; w == nil || !strings.Contains(w.Question, "has no checks") {
		t.Fatalf("wait = %+v", w)
	}
}

func TestDecide_WithoutChecksMergesAtOnce(t *testing.T) {
	m, s, o := prCase(0, principal.AutoWithoutChecks)
	_, as := decide(m, s, o)
	got := of[Merge](as)
	if len(got) != 1 || got[0] != (Merge{Piece: "a", Head: "abc"}) {
		t.Fatalf("merges = %#v", got)
	}
	m, s, o = prCase(0, principal.AutoWithoutChecks, chk("fail"))
	_, as = decide(m, s, o)
	if len(of[Merge](as)) != 1 || len(of[SendBack](as)) != 0 {
		t.Fatalf("failing check not ignored: %#v", as)
	}
}

func TestDecide_HeadMismatchNeverMerges(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*State, *Obs)
	}{
		{"other head", func(s *State, o *Obs) { *o = withPR(*o, func(p *gh.PR) { p.HeadSHA = "zzz" }) }},
		{"empty head", func(s *State, o *Obs) {
			s.Pieces[0].Head = ""
			*o = withPR(*o, func(p *gh.PR) { p.HeadSHA = "" })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, s, o := prCase(time.Minute, principal.AutoOn, chk("pass"))
			tc.edit(&s, &o)
			s1, as := decide(m, s, o)
			wantNoActions(t, as)
			m, s, o = prCase(3*time.Minute, principal.AutoOn, chk("pass"))
			tc.edit(&s, &o)
			s1, as = decide(m, s, o)
			want := "PR #7 (a) has commits crew didn't push, so crew won't merge it; it is waiting on you"
			if w := piece(t, s1, "a").Wait; w == nil || w.Question != want {
				t.Fatalf("wait = %+v", w)
			}
			if len(of[Merge](as)) != 0 {
				t.Fatal("merged")
			}
		})
	}
}

func TestDecide_UnknownMergeableHolds(t *testing.T) {
	m, s, o := prCase(time.Minute, principal.AutoOn, chk("pass"))
	o = withPR(o, func(p *gh.PR) { p.Mergeable = "UNKNOWN" })
	s1, as := decide(m, s, o)
	wantNoActions(t, as)
	if piece(t, s1, "a").Wait != nil {
		t.Fatal("unexpected wait")
	}
}

func TestDecide_FailedChecksSendBackTwiceThenAsk(t *testing.T) {
	for n := 0; n < 2; n++ {
		m, s, o := prCase(time.Minute, principal.AutoOn, chk("fail"))
		s.Pieces[0].SentBack = map[string]int{"checks-failed": n}
		_, as := decide(m, s, o)
		sb := of[SendBack](as)
		if len(sb) != 1 || sb[0].Reason != "checks-failed" || sb[0].Station != "" ||
			!strings.Contains(sb[0].Note, "ci-fail (http://x/fail)") {
			t.Fatalf("n=%d sendbacks = %#v", n, sb)
		}
	}
	m, s, o := prCase(time.Minute, principal.AutoOn, chk("fail"))
	s.Pieces[0].SentBack = map[string]int{"checks-failed": 2}
	_, as := decide(m, s, o)
	ask := of[AskOrchestrator](as)
	if len(ask) != 1 || ask[0].Failure != "checks-failed" || len(of[SendBack](as)) != 0 {
		t.Fatalf("actions = %#v", as)
	}
}

func TestDecide_FailedChecksSendBackWhenOff(t *testing.T) {
	m, s, o := prCase(time.Minute, principal.AutoOff, chk("fail"))
	s.Pieces[0].Wait = &Wait{ID: "w0", Kind: WaitMerge, Question: "q", Since: t0}
	_, as := decide(m, s, o)
	if len(of[SendBack](as)) != 1 {
		t.Fatalf("actions = %#v", as)
	}
}

func TestDecide_StaleFailureIgnored(t *testing.T) {
	m, s, o := prCase(time.Minute, principal.AutoOn, chk("fail"))
	o = withPR(o, func(p *gh.PR) { p.HeadSHA = "zzz" })
	_, as := decide(m, s, o)
	if len(of[SendBack](as)) != 0 || len(of[AskOrchestrator](as)) != 0 {
		t.Fatalf("acted on a stale failure: %#v", as)
	}
}

func TestDecide_ConflictResolvesTwiceThenAsks(t *testing.T) {
	for n := 0; n < 2; n++ {
		m, s, o := prCase(time.Minute, principal.AutoOn, chk("pass"))
		o = withPR(o, func(p *gh.PR) { p.Mergeable = "CONFLICTING" })
		s.Pieces[0].SentBack = map[string]int{"conflict": n}
		_, as := decide(m, s, o)
		if got := of[ResolveConflict](as); len(got) != 1 || got[0].Piece != "a" || len(of[Merge](as)) != 0 {
			t.Fatalf("n=%d actions = %#v", n, as)
		}
	}
	m, s, o := prCase(time.Minute, principal.AutoOn, chk("pass"))
	o = withPR(o, func(p *gh.PR) { p.Mergeable = "CONFLICTING" })
	s.Pieces[0].SentBack = map[string]int{"conflict": 2}
	_, as := decide(m, s, o)
	if ask := of[AskOrchestrator](as); len(ask) != 1 || ask[0].Failure != "conflict" {
		t.Fatalf("actions = %#v", as)
	}
}

func TestDecide_BlockerNamedInWait(t *testing.T) {
	m, s, o := prCase(0, principal.AutoOff)
	o.Auto = map[string]AutoObs{"a": {Mode: principal.AutoOff, Blocker: "design"}}
	s1, _ := decide(m, s, o)
	want := "PR #7 (a) also changes files of the design domain, whose auto-merge is off; it is waiting on you"
	if w := piece(t, s1, "a").Wait; w == nil || w.Question != want {
		t.Fatalf("wait = %+v", w)
	}
}

func TestDecide_QuestionWaitStopsAutomation(t *testing.T) {
	m, s, o := prCase(time.Minute, principal.AutoOn, chk("pass"))
	s.Pieces[0].Wait = &Wait{ID: "w0", Kind: WaitQuestion, Question: "q", Since: t0}
	_, as := decide(m, s, o)
	wantNoActions(t, as)
	o = withPR(o, func(p *gh.PR) { p.State = "MERGED" })
	s1, _ := decide(m, s, o)
	if piece(t, s1, "a").Status != PieceComplete {
		t.Fatalf("status = %s", piece(t, s1, "a").Status)
	}
}

func TestDecide_AskAtCallLimitKeepsPROpen(t *testing.T) {
	m, s, o := prCase(time.Minute, principal.AutoOn, chk("pass"))
	o = withPR(o, func(p *gh.PR) { p.Mergeable = "CONFLICTING" })
	s.Pieces[0].SentBack = map[string]int{"conflict": 2}
	s.Pieces[0].Calls = 5
	s1, _ := Decide(m, s, o, limits(1, 5), 0)
	p := piece(t, s1, "a")
	if p.Status != PiecePROpen || p.Wait == nil || p.Wait.Kind != WaitLimit || p.Wait.Back != "" {
		t.Fatalf("piece = %+v wait=%+v", p, p.Wait)
	}
}

func TestDecide_AbsentAutoIsOff(t *testing.T) {
	m, s, o := prCase(0, principal.AutoOn, chk("pass"))
	o.Auto = nil
	s1, as := decide(m, s, o)
	if w := piece(t, s1, "a").Wait; w == nil || w.Kind != WaitMerge {
		t.Fatalf("wait = %+v", w)
	}
	if len(of[Merge](as)) != 0 {
		t.Fatal("merged")
	}
}
