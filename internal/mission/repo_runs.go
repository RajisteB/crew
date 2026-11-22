package mission

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/crewfile-dev/crew/internal/flock"
)

const (
	startsLockName  = "starts.lock"
	startsLockWait  = 2 * time.Second
	startsLockRetry = 10 * time.Millisecond
)

// HoldingRuns counts the pieces of s that count toward runs_at_once: running
// pieces, and pieces waiting on the principal in the middle of a run (their
// wait goes back to running). A piece whose pull request waits for a merge,
// or that never started, does not count.
func HoldingRuns(s State) int {
	n := 0
	for _, p := range s.Pieces {
		if p.Status == PieceRunning || (p.Status == PieceWaiting && p.Wait != nil && p.Wait.Back == PieceRunning) {
			n++
		}
	}
	return n
}

// RunsElsewhere counts the runs held by the repo's other missions whose status
// is running. It reads their saved state and takes none of their locks. A
// folder that is not a mission is ignored; a mission whose state can't be read
// is an error, since its runs can't be counted.
func RunsElsewhere(repoRoot, missionID string) (int, error) {
	entries, err := os.ReadDir(missionsDir(repoRoot))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() || e.Name() == missionID {
			continue
		}
		st, found, err := loadMissionState(repoRoot, e.Name())
		if err != nil {
			return 0, err
		}
		if !found {
			continue
		}
		if st.Status == StatusRunning {
			n += HoldingRuns(st)
		}
	}
	return n, nil
}

// lockStarts takes the repo's start lock, <repoRoot>/.crew/missions/starts.lock,
// trying for up to 2 seconds. The caller releases it. Engines take it inside
// their own mission lock, never the other way round.
func lockStarts(repoRoot string) (release func(), err error) {
	path := filepath.Join(missionsDir(repoRoot), startsLockName)
	deadline := time.Now().Add(startsLockWait)
	for {
		release, err = flock.Acquire(path, fileMode)
		if !errors.Is(err, flock.ErrHeld) {
			return release, err
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(startsLockRetry)
	}
}

// startGateOpen counts the repo again, under the start lock, and says whether
// one more run fits within the limit. A count that fails says no.
func (t *tick) startGateOpen() bool {
	elsewhere, err := RunsElsewhere(t.e.RepoRoot, t.s.MissionID)
	if err != nil {
		return false
	}
	return Allows(t.lim.Concurrency, elsewhere+HoldingRuns(t.s)+1)
}

// observeElsewhere fills Elsewhere in the tick's observation.
func (t *tick) observeElsewhere() {
	n, err := RunsElsewhere(t.e.RepoRoot, t.s.MissionID)
	if err != nil {
		t.note("", "can't count the repo's other missions: %v", err)
		t.obs.ElsewhereUnknown = true
		return
	}
	t.obs.Elsewhere = n
}

// takeStartSlot takes the start lock and checks the limit under it. When it
// says no, the piece stays queued with no note and the lock is already let go.
// release is safe to call more than once.
func (t *tick) takeStartSlot() (release func(), ok bool) {
	unlock, err := lockStarts(t.e.RepoRoot)
	if err != nil {
		return nil, false
	}
	if !t.startGateOpen() {
		unlock()
		return nil, false
	}
	return unlock, true
}

// loadMissionState reads the saved state of the folder named. found is false
// for something that is not a mission: a name that is no mission id, or a
// folder with no state.json. Any other failure to look or read is an error
// naming the folder, since that mission's runs can't be counted.
func loadMissionState(repoRoot, name string) (st State, found bool, err error) {
	if validID(name) != nil {
		return State{}, false, nil
	}
	store := Store{Dir: filepath.Join(missionsDir(repoRoot), name)}
	st, err = store.LoadState()
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("mission %s: %w", name, err)
	}
	return st, true, nil
}
