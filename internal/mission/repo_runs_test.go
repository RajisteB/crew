package mission

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/flock"
)

func TestHoldingRuns(t *testing.T) {
	s := State{Pieces: []PieceState{
		{ID: "a", Status: PieceRunning},
		{ID: "b", Status: PieceWaiting, Wait: &Wait{Back: PieceRunning}},
		{ID: "c", Status: PieceWaiting, Wait: &Wait{Back: PieceQueued}},
		{ID: "d", Status: PiecePROpen},
		{ID: "e", Status: PieceDelivered},
		{ID: "f", Status: PieceComplete},
	}}
	if got := HoldingRuns(s); got != 2 {
		t.Fatalf("HoldingRuns = %d, want 2", got)
	}
}

// saveOther adds a mission to root whose state has the given status and
// holds the given number of running pieces.
func saveOther(t *testing.T, root string, status Status, running int) State {
	t.Helper()
	h := newHarnessIn(t, root, Piece{ID: "P1", Factory: "feature"})
	h.edit(func(s *State) {
		s.Status = status
		s.Pieces = nil
		for i := range running {
			s.Pieces = append(s.Pieces, PieceState{ID: "R" + string(rune('A'+i)), Status: PieceRunning})
		}
	})
	return h.state()
}

func TestRunsElsewhere(t *testing.T) {
	root := t.TempDir()
	self := saveOther(t, root, StatusRunning, 1)
	saveOther(t, root, StatusRunning, 2)
	saveOther(t, root, StatusStopped, 1)
	if err := os.MkdirAll(filepath.Join(missionsDir(root), "not-a-mission"), 0o755); err != nil {
		t.Fatal(err)
	}
	release, err := lockStarts(root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	got, err := RunsElsewhere(root, self.MissionID)
	if err != nil || got != 2 {
		t.Fatalf("RunsElsewhere = %d, %v; want 2", got, err)
	}
}

func TestRunsElsewhere_NoMissionsFolder(t *testing.T) {
	if got, err := RunsElsewhere(t.TempDir(), "m-x"); err != nil || got != 0 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestRunsElsewhere_UnreadableStateIsAnError(t *testing.T) {
	root := t.TempDir()
	other := saveOther(t, root, StatusRunning, 1)
	path := filepath.Join(missionsDir(root), other.MissionID, "state.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunsElsewhere(root, "m-self"); err == nil {
		t.Fatal("want an error for a state that can't be read")
	}
}

func TestLockStarts_ExcludesAndReleases(t *testing.T) {
	root := t.TempDir()
	saveOther(t, root, StatusRunning, 0) // creates the missions folder
	release, err := lockStarts(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flock.Acquire(filepath.Join(missionsDir(root), startsLockName), 0o644); !errors.Is(err, flock.ErrHeld) {
		t.Fatalf("lock should be held, got %v", err)
	}
	release()
	again, err := lockStarts(root)
	if err != nil {
		t.Fatalf("lock should be free after release: %v", err)
	}
	again()
}

func TestRunsElsewhere_UnenterableMissionIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can enter any folder")
	}
	root := t.TempDir()
	other := saveOther(t, root, StatusRunning, 1)
	dir := filepath.Join(missionsDir(root), other.MissionID)
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	_, err := RunsElsewhere(root, "m-self")
	if err == nil || !strings.Contains(err.Error(), other.MissionID) {
		t.Fatalf("err = %v, want an error naming %s", err, other.MissionID)
	}
}
