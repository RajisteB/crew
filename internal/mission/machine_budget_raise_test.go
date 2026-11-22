package mission

import "testing"

func notesOf(as []Action) []string {
	var out []string
	for _, r := range of[Record](as) {
		if r.Event.Kind == EvNote {
			out = append(out, r.Event.Msg)
		}
	}
	return out
}

func TestDecide_RaiseThatIsTooSmallSaysSo(t *testing.T) {
	m := mis(nil, "a")
	s := st(m, PieceRunning)
	s.Pieces[0].Steps = 15
	s.Hold = &Hold{Kind: "steps", Count: 6, Max: "6", Since: t0}
	o := obs(t0)
	o.Runs["a"] = runAt(15)
	s1, as := Decide(m, s, o, stepLimits(lim(10)), 0)
	if s1.Hold == nil || s1.Hold.Count != 15 || s1.Hold.Max != "10" || !s1.Hold.Since.Equal(t0) {
		t.Fatalf("hold = %+v, want 15 of 10 since the hold began", s1.Hold)
	}
	if got := notesOf(as); len(got) != 1 || got[0] != "step budget raised to 10, 15 used: still on hold" {
		t.Fatalf("notes = %q", got)
	}
	if len(of[Notify](as)) != 0 || len(budgetRecords(as)) != 0 {
		t.Fatalf("the hold was told again: %+v", as)
	}
	if _, as = Decide(m, s1, o, stepLimits(lim(10)), 0); len(notesOf(as)) != 0 {
		t.Fatalf("second decide noted again: %q", notesOf(as))
	}
}

func TestDecide_HoldCountFollowsTheStepsUsed(t *testing.T) {
	m := mis(nil, "a")
	s := st(m, PieceRunning)
	s.Pieces[0].Steps = 6
	s.Hold = &Hold{Kind: "steps", Count: 6, Max: "6", Since: t0}
	o := obs(t0)
	o.Runs["a"] = runAt(8)
	s1, as := Decide(m, s, o, stepLimits(lim(6)), 0)
	if s1.Hold == nil || s1.Hold.Count != 8 || s1.Hold.Max != "6" || len(notesOf(as)) != 0 {
		t.Fatalf("hold = %+v, notes = %q", s1.Hold, notesOf(as))
	}
}

func TestDecide_DoneMissionHasNoHold(t *testing.T) {
	m := mis(nil, "a")
	s := st(m, PieceComplete)
	s.Pieces[0].Steps = 6
	s.Hold = &Hold{Kind: "steps", Count: 6, Max: "6", Since: t0}
	s1, _ := Decide(m, s, obs(t0), stepLimits(lim(6)), 0)
	if s1.Status != StatusDone || s1.Hold != nil {
		t.Fatalf("status = %s, hold = %+v; want done with no hold", s1.Status, s1.Hold)
	}
}
