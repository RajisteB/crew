package main

import (
	"fmt"

	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/run"
)

// ownedVerbs are the run commands a mission's run refuses, with the
// number of positional arguments each needs (the verb and the run id, then
// the rest).
var ownedVerbs = map[string]int{"say": 3, "goto": 3, "approve": 2, "resume": 2}

// ownerOf returns the run's owner: the one in its state, or else the mission piece
// that names the run in the repo's mission states. "" for a plain run.
func ownerOf(repoRoot string, st run.State) (string, error) {
	if st.Owner != "" {
		return st.Owner, nil
	}
	return mission.OwnerOfRun(repoRoot, st.RunID)
}

// refuseOwned returns an error when the run has an owner, or when its owner
// can't be told because a mission state of the repo can't be read (it fails
// closed); verb is "say", "goto", "approve" or "resume". It reads and writes
// nothing else.
func refuseOwned(repoRoot string, store run.Store, verb string) error {
	st, err := store.LoadState()
	if err != nil {
		return err
	}
	owner, err := ownerOf(repoRoot, st)
	if err != nil {
		return fmt.Errorf("crew run %s is refused for run %s: can't tell whether a mission owns it: %w", verb, st.RunID, err)
	}
	if owner == "" {
		return nil
	}
	return fmt.Errorf("run %s belongs to %s: crew run %s is refused for it; use crew mission status, answer, amend, stop or resume", st.RunID, owner, verb)
}

// guardOwned refuses say, goto, approve and resume for a run a mission owns,
// before anything is read from stdin or written. Other verbs pass.
func guardOwned(pos []string, f runFlags) error {
	need, ok := ownedVerbs[pos[0]]
	if !ok {
		return nil
	}
	if len(pos) < need {
		return usageError{runUsage}
	}
	store, err := run.OpenRun(f.project, pos[1])
	if err != nil {
		return err
	}
	return refuseOwned(f.project, store, pos[0])
}
