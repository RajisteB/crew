package mission

import (
	"context"
	"sync"
	"testing"
)

func independent(ids ...string) []Piece {
	var out []Piece
	for _, id := range ids {
		out = append(out, Piece{ID: id, Domain: "product", Factory: "feature", Brief: "do " + id})
	}
	return out
}

func TestDecide_StartsUpToTheLimit(t *testing.T) {
	m := mis(nil, "P1", "P2", "P3", "P4")
	s := st(m, PieceQueued, PieceQueued, PieceQueued, PieceQueued)
	_, as := Decide(m, s, obs(t0), limits(3, 3), 0)
	got := of[StartPiece](as)
	if len(got) != 3 || got[0].Piece != "P1" || got[1].Piece != "P2" || got[2].Piece != "P3" {
		t.Fatalf("want P1 P2 P3, got %+v", got)
	}
	star := limits(3, 3)
	star.Concurrency.Unlimited = true
	_, as = Decide(m, s, obs(t0), star, 0)
	if got := of[StartPiece](as); len(got) != 4 {
		t.Fatalf("with \"*\" want 4 starts, got %+v", got)
	}
}

func TestDecide_CountsTheRepoOtherMissions(t *testing.T) {
	m := mis(nil, "P1", "P2")
	s := st(m, PieceQueued, PieceQueued)
	cases := []struct {
		name string
		o    func(*Obs)
		want int
	}{
		{"two elsewhere", func(o *Obs) { o.Elsewhere = 2 }, 1},
		{"three elsewhere", func(o *Obs) { o.Elsewhere = 3 }, 0},
		{"unknown", func(o *Obs) { o.ElsewhereUnknown = true }, 0},
	}
	for _, c := range cases {
		o := obs(t0)
		c.o(&o)
		_, as := Decide(m, s, o, limits(3, 3), 0)
		if got := of[StartPiece](as); len(got) != c.want {
			t.Errorf("%s: %d starts, want %d: %+v", c.name, len(got), c.want, got)
		}
	}
}

func TestEngine_StartsTwoPiecesInOneTick(t *testing.T) {
	h := newHarness(t, independent("P1", "P2")...)
	h.e.Settings.Limits.Concurrency = lim(2)
	h.tick()
	p1, p2 := h.piece("P1"), h.piece("P2")
	if p1.Status != PieceRunning || p2.Status != PieceRunning {
		t.Fatalf("statuses = %s, %s; want both running", p1.Status, p2.Status)
	}
	if p1.Worktree == p2.Worktree || p1.Branch == p2.Branch || p1.Worktree == "" || p1.Branch == "" {
		t.Fatalf("each piece needs its own worktree and branch: %+v %+v", p1, p2)
	}
}

func TestEngine_StartGateCountsOtherMissions(t *testing.T) {
	a := newHarness(t, independent("A1")...)
	a.e.Settings.Limits.Concurrency = lim(1)
	a.runPiece("A1")
	b := newHarnessIn(t, a.e.RepoRoot, independent("B1")...)
	b.e.Settings.Limits.Concurrency = lim(1)
	b.tick()
	if got := b.piece("B1").Status; got != PieceQueued {
		t.Fatalf("B1 = %s, want queued while A holds the only slot", got)
	}
	if b.log.has("Start ") {
		t.Fatal("no run should have been started")
	}
	a.edit(func(s *State) { s.Pieces[0] = PieceState{ID: "A1", Status: PieceComplete} })
	b.tick()
	if got := b.piece("B1").Status; got != PieceRunning {
		t.Fatalf("B1 = %s, want running once A's piece is done", got)
	}
}

func TestEngine_StartLockHeldLeavesPieceQueuedWithoutNote(t *testing.T) {
	h := newHarness(t, independent("P1")...)
	release, err := lockStarts(h.e.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	h.tick()
	if got := h.piece("P1").Status; got != PieceQueued {
		t.Fatalf("P1 = %s, want queued", got)
	}
	if h.log.has("Fetch") || len(h.events(EvNote)) != 0 {
		t.Fatalf("no git calls and no note expected: %v %v", h.log.calls, h.kinds())
	}
}

func TestEngine_TwoEnginesNeverPassTheLimit(t *testing.T) {
	for round := range 25 {
		root := t.TempDir()
		hs := []*harness{
			newHarnessIn(t, root, independent("P1")...),
			newHarnessIn(t, root, independent("P1")...),
		}
		var wg sync.WaitGroup
		errs := make([]error, len(hs))
		for i, h := range hs {
			h.e.Settings.Limits.Concurrency = lim(1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = h.e.Tick(context.Background())
			}()
		}
		wg.Wait()
		running := 0
		for i, h := range hs {
			if errs[i] != nil {
				t.Fatalf("round %d: tick: %v", round, errs[i])
			}
			running += HoldingRuns(h.state())
		}
		if running > 1 {
			t.Fatalf("round %d: %d pieces running across the repo, limit 1", round, running)
		}
	}
}
