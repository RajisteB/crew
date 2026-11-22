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

// RunsElsewhere counts the runs held by the repo's other missions that are
// being driven (see otherMission.Driven). It reads their saved state and
// takes none of their locks. A folder that is not a mission is ignored; a
// mission whose state can't be read is an error, since its runs can't be
// counted.
func RunsElsewhere(repoRoot, missionID string) (int, error) {
	n := 0
	err := walkOtherMissions(repoRoot, missionID, func(o otherMission) error {
		if o.Driven {
			n += HoldingRuns(o.State)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// otherMission is another mission of the repo as its folder holds it. ID is
// the folder's name, which its state's mission id must match.
type otherMission struct {
	ID     string
	State  State
	Driven bool // its status is running and a live engine holds its mission lock
}

// walkOtherMissions calls fn with each of the repo's other missions, in
// mission id order. It skips what is not a mission (see loadMissionState) and
// stops at the first mission that can't be read or the first error fn returns.
func walkOtherMissions(repoRoot, missionID string, fn func(o otherMission) error) error {
	entries, err := os.ReadDir(missionsDir(repoRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == missionID {
			continue
		}
		st, found, err := loadMissionState(repoRoot, e.Name())
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if err := fn(otherMission{ID: e.Name(), State: st, Driven: beingDriven(repoRoot, e.Name(), st)}); err != nil {
			return err
		}
	}
	return nil
}

// beingDriven says whether the mission is being driven: its status is
// running and a live engine holds its mission lock. A mission that is
// stopped, or marked running after its engine ended (a reboot, a killed
// process), is not.
func beingDriven(repoRoot, id string, st State) bool {
	if st.Status != StatusRunning {
		return false
	}
	_, live := Store{Dir: filepath.Join(missionsDir(repoRoot), id)}.LockedBy()
	return live
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
// says no, the piece stays queued and the lock is already let go. A lock held
// by another engine leaves no note; a lock that can't be taken for any other
// reason does. release is safe to call more than once.
func (t *tick) takeStartSlot() (release func(), ok bool) {
	unlock, err := lockStarts(t.e.RepoRoot)
	if err != nil {
		if !errors.Is(err, flock.ErrHeld) {
			t.note("", "can't take the repo's start lock: %v", err)
		}
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
	if st.MissionID != name {
		return State{}, false, fmt.Errorf("mission %s: its state names mission %q", name, st.MissionID)
	}
	return st, true, nil
}
