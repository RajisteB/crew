package mission

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

// writeMissionState saves st as the state of mission id under repo.
func writeMissionState(t *testing.T, repo, id string, st State) {
	t.Helper()
	dir := filepath.Join(missionsDir(repo), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := (Store{Dir: dir}).SaveState(st); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerOfRun_FindsARunAMissionNames(t *testing.T) {
	repo := t.TempDir()
	if got, err := OwnerOfRun(repo, "r-1"); err != nil || got != "" {
		t.Fatalf("no missions folder: %q, %v; want \"\", nil", got, err)
	}
	writeMissionState(t, repo, "m-1", State{MissionID: "m-1", Pieces: []PieceState{
		{ID: "P0", Status: PieceQueued},
		{ID: "P1", Status: PieceRunning, RunID: "r-1"},
	}})
	if got, err := OwnerOfRun(repo, "r-1"); err != nil || got != "mission m-1 piece P1" {
		t.Fatalf("OwnerOfRun(r-1) = %q, %v; want the mission piece", got, err)
	}
	if got, err := OwnerOfRun(repo, "r-other"); err != nil || got != "" {
		t.Fatalf("unknown run: %q, %v; want \"\", nil", got, err)
	}
	if got, err := OwnerOfRun(repo, ""); err != nil || got != "" {
		t.Fatalf("empty run id: %q, %v; want \"\" (a queued piece has no run id)", got, err)
	}
}

func TestOwnerOfRun_FindsASubPiecesRun(t *testing.T) {
	repo := t.TempDir()
	writeMissionState(t, repo, "m-20261123-120000-abcd", State{MissionID: "m-20261123-120000-abcd", Pieces: []PieceState{
		{ID: "P1", Status: PieceDelegated},
		{ID: "P1.2", Status: PieceRunning, RunID: "r-sub"},
	}})
	got, err := OwnerOfRun(repo, "r-sub")
	if err != nil || got != "mission m-20261123-120000-abcd piece P1.2" {
		t.Fatalf("OwnerOfRun(r-sub) = %q, %v", got, err)
	}
}

func TestOwnerOfRun_FindsARunOfAFinishedOrSentBackPiece(t *testing.T) {
	repo := t.TempDir()
	writeMissionState(t, repo, "m-2", State{MissionID: "m-2", Pieces: []PieceState{
		{ID: "P1", Status: PieceComplete, RunID: "r-done"},
		{ID: "P2", Status: PieceRunning, RunID: "r-back", SentBack: map[string]int{"checks": 1}},
		{ID: "P3", Status: PieceCancelled, RunID: "r-cancelled"},
	}})
	for run, want := range map[string]string{"r-done": "mission m-2 piece P1", "r-back": "mission m-2 piece P2", "r-cancelled": "mission m-2 piece P3"} {
		if got, err := OwnerOfRun(repo, run); err != nil || got != want {
			t.Errorf("OwnerOfRun(%s) = %q, %v; want %q", run, got, err, want)
		}
	}
}

func TestOwnerOfRun_UnreadableStateIsAnErrorNamingTheFile(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(missionsDir(repo), "m-3")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := OwnerOfRun(repo, "r-1")
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v, want an error naming %s", err, path)
	}
}

func TestOwnerOfRun_IgnoresWhatIsNoMission(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(missionsDir(repo), "empty-folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(missionsDir(repo), startsLockName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := OwnerOfRun(repo, "r-1"); err != nil || got != "" {
		t.Fatalf("got %q, %v; want \"\", nil", got, err)
	}
}

// The owner must be in the state NewOwnedRun saves, not added by a second
// save: the file on disk after Start is exactly that first state.
func TestLocalRuns_StartWritesTheOwnerInTheFirstSave(t *testing.T) {
	e := newLocal(t)
	id := e.start(t)
	store, err := run.OpenRun(e.repo, id)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if want := RunOwner("m-1", "api"); st.Owner != want {
		t.Fatalf("saved owner = %q, want %q", st.Owner, want)
	}
}
