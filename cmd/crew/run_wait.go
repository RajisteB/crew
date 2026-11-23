package main

import (
	"time"

	"github.com/crewfile-dev/crew/internal/run"
)

// statusPoll is how often a waiting status looks again; tests lower it.
var statusPoll = 500 * time.Millisecond

// maxStatusWait is the longest `crew run status --wait` accepts.
const maxStatusWait = 10 * time.Minute

// waitForNews returns when the run has an event after since, or it has stopped
// and no process drives it, or d has passed, whichever is first. A run saved
// as running with no holder is waited on: its process may be starting.
func waitForNews(store run.Store, since int, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	deadline := time.Now().Add(d)
	for {
		evs, err := store.EventsSince(since)
		if err != nil {
			return err
		}
		if len(evs) > 0 {
			return nil
		}
		st, err := store.LoadState()
		if err != nil {
			return err
		}
		if _, held := store.LockedBy(); !held && st.Status != run.StatusRunning {
			return nil
		}
		left := time.Until(deadline)
		if left <= 0 {
			return nil
		}
		time.Sleep(min(statusPoll, left))
	}
}
