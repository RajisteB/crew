package mission

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/crewfile-dev/crew/internal/source"
)

func pathed(id string, paths ...string) Piece {
	return Piece{ID: id, Domain: "product", Factory: "feature", Brief: "do " + id, Paths: paths}
}

func TestEngine_PieceWaitsForTheOtherMissionsPiece(t *testing.T) {
	a := newHarness(t, pathed("P1", "src/**"))
	a.runPiece("P1")
	b := newHarnessIn(t, a.e.RepoRoot, pathed("P1", "src/api/**"), pathed("P2", "docs/**"))
	b.tick()
	p1 := b.piece("P1")
	if p1.Status != PieceQueued || p1.Behind == nil || p1.Behind.Mission != a.id || p1.Behind.Piece != "P1" {
		t.Fatalf("B P1 = %+v", p1)
	}
	if got := b.piece("P2").Status; got != PieceRunning {
		t.Fatalf("B P2 = %s, want running", got)
	}
	a.edit(func(s *State) { s.Pieces[0] = PieceState{ID: "P1", Status: PieceComplete} })
	b.tick()
	p1 = b.piece("P1")
	if p1.Status != PieceRunning || p1.Behind != nil {
		t.Fatalf("B P1 = %+v, want running with no Behind", p1)
	}
}

func TestEngine_StoppedMissionStillHoldsItsPaths(t *testing.T) {
	a := newHarness(t, pathed("P1", "src/**"))
	a.edit(func(s *State) {
		s.Status = StatusStopped
		s.Pieces[0].Status = PiecePROpen
	})
	b := newHarnessIn(t, a.e.RepoRoot, pathed("P1", "src/api/**"))
	b.tick()
	p1 := b.piece("P1")
	if p1.Status != PieceQueued || p1.Behind == nil || !p1.Behind.Stopped {
		t.Fatalf("B P1 = %+v", p1)
	}
	if len(b.notif.sent) != 1 {
		t.Fatalf("notifications = %v", b.notif.sent)
	}
	b.tick()
	if len(b.notif.sent) != 1 {
		t.Fatalf("second tick notified again: %v", b.notif.sent)
	}
}

func TestEngine_TwoEnginesNeverStartOverlappingPieces(t *testing.T) {
	for round := range 25 {
		root := t.TempDir()
		hs := []*harness{
			newHarnessIn(t, root, pathed("P1", "src/**")),
			newHarnessIn(t, root, pathed("P1", "src/**")),
		}
		var wg sync.WaitGroup
		errs := make([]error, len(hs))
		for i, h := range hs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = h.e.Tick(context.Background())
			}()
		}
		wg.Wait()
		started := 0
		for i, h := range hs {
			if errs[i] != nil {
				t.Fatalf("round %d: tick: %v", round, errs[i])
			}
			if h.state().Pieces[0].Status != PieceQueued {
				started++
			}
		}
		if started != 1 {
			t.Fatalf("round %d: %d overlapping pieces started, want exactly 1", round, started)
		}
	}
}

func TestEngine_ClaimsUnreadableStartsNothing(t *testing.T) {
	a := newHarness(t, pathed("P1", "docs/**"))
	a.runPiece("P1")
	if err := os.Remove(filepath.Join(a.store.Dir, "mission.md")); err != nil {
		t.Fatal(err)
	}
	b := newHarnessIn(t, a.e.RepoRoot, pathed("P1", "src/**"))
	b.e.Domains = map[string]source.Domain{"product": {Name: "product"}}
	b.tick()
	if got := b.piece("P1").Status; got != PieceQueued {
		t.Fatalf("B P1 = %s, want queued", got)
	}
	notes := b.events(EvNote)
	if len(notes) != 1 || !strings.Contains(notes[0].Msg, "can't read the repo's other missions") {
		t.Fatalf("notes = %+v", notes)
	}
	if b.log.has("Fetch") || b.log.has("Start ") {
		t.Fatalf("git or run calls: %v", b.log.calls)
	}
}
