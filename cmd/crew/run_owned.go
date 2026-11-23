package main

import (
	"fmt"

	"github.com/crewfile-dev/crew/internal/run"
)

// ownedVerbs are the run commands a mission's run refuses, with the
// number of positional arguments each needs (the verb and the run id, then
// the rest).
var ownedVerbs = map[string]int{"say": 3, "goto": 3, "approve": 2, "resume": 2}

// refuseOwned returns an error when the run has an owner; verb is "say",
// "goto", "approve" or "resume". It reads the state and writes nothing.
func refuseOwned(store run.Store, verb string) error {
	st, err := store.LoadState()
	if err != nil {
		return err
	}
	if st.Owner == "" {
		return nil
	}
	return fmt.Errorf("run %s belongs to %s: crew run %s is refused for it; use crew mission status, answer, amend, stop or resume", st.RunID, st.Owner, verb)
}
