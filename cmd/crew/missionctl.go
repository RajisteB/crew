package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/crewfile-dev/crew/internal/mission"
)

// stopWait and stopPoll bound how long stop waits for the engine to let go
// of the lock. They are variables so tests can shorten them.
var (
	stopWait = 15 * time.Second
	stopPoll = 100 * time.Millisecond
)

// words reads a TEXT|@FILE flag value.
func words(name, v string) (string, error) {
	t, err := textArg(v)
	if err != nil {
		return "", fmt.Errorf("--%s: %w", name, err)
	}
	return t, nil
}

func queuedMessage(store mission.Store) string {
	if _, live := store.LockedBy(); live {
		return "queued; applied on the next tick"
	}
	return "queued; the mission isn't running, so it is applied on resume"
}

func missionAnswer(env missionEnv, id string, args []string) error {
	if len(args) != 1 {
		return errors.New(missionUsage)
	}
	f := env.flags
	if f.words == "" {
		return usageError{"--words is required: what the principal said, word for word"}
	}
	w, err := words("words", f.words)
	if err != nil {
		return err
	}
	note, err := words("note", f.note)
	if err != nil {
		return err
	}
	store, st, m, err := openForDryRun(env, id)
	if err != nil {
		return err
	}
	chk, err := loadChecks(env.crewDir, st.Repo)
	if err != nil {
		return err
	}
	a := mission.Answer{Wait: args[0], Do: f.do, Station: f.station, Note: note, Words: w}
	if _, _, _, _, err := mission.ApplyAnswer(m, st, a, chk.in, time.Now()); err != nil {
		return err
	}
	if err := store.Drop("answer", a); err != nil {
		return err
	}
	fmt.Fprintln(env.out, queuedMessage(store))
	return nil
}

func missionAmend(env missionEnv, id string, args []string) error {
	if env.flags.words == "" {
		return usageError{"--words is required: what the principal said, word for word"}
	}
	w, err := words("words", env.flags.words)
	if err != nil {
		return err
	}
	plan, err := readPlanSource(env, args)
	if err != nil {
		return err
	}
	store, st, m, err := openForDryRun(env, id)
	if err != nil {
		return err
	}
	chk, err := loadChecks(env.crewDir, st.Repo)
	if err != nil {
		return err
	}
	a := mission.Amendment{Plan: plan, Words: w}
	if _, _, _, _, err := mission.Amend(m, st, a, chk.in, time.Now()); err != nil {
		return err
	}
	if err := store.Drop("amend", a); err != nil {
		return err
	}
	fmt.Fprintln(env.out, queuedMessage(store))
	return nil
}

// openForDryRun reads the mission and its state without writing either.
func openForDryRun(env missionEnv, id string) (mission.Store, mission.State, mission.Mission, error) {
	store, st, err := openMission(env, id)
	if err != nil {
		return store, st, mission.Mission{}, err
	}
	m, err := store.Mission()
	return store, st, m, err
}

// drivenError means a live engine holds the mission lock.
type drivenError struct {
	id  string
	pid int
}

func (e drivenError) Error() string {
	return fmt.Sprintf("mission %s is being driven (pid %d)", e.id, e.pid)
}

// writeState is the one way a command other than the engine changes a
// mission's state. It takes the mission lock, reloads the state under it,
// lets f decide the new state (f re-checks the status it needs), saves it and
// emits f's event. Run slow checks before it; start any process after it.
func writeState(store mission.Store, id string, f func(mission.State) (mission.State, mission.Event, error)) (mission.State, error) {
	unlock, err := store.Lock()
	if err != nil {
		if pid, live := store.LockedBy(); live {
			return mission.State{}, drivenError{id: id, pid: pid}
		}
		return mission.State{}, err
	}
	defer unlock()
	cur, err := store.LoadState()
	if err != nil {
		return cur, err
	}
	next, ev, err := f(cur)
	if err != nil {
		return cur, err
	}
	if err := store.SaveState(next); err != nil {
		return cur, err
	}
	if ev.Kind != "" {
		if _, err := store.Emit(ev); err != nil {
			return next, err
		}
	}
	return next, nil
}

func missionStop(env missionEnv, id string) error {
	store, st, err := openMission(env, id)
	if err != nil {
		return err
	}
	switch st.Status {
	case mission.StatusPlanned, mission.StatusAwaitingGo:
		return fmt.Errorf("mission %s hasn't started; nothing to stop", id)
	case mission.StatusStopped, mission.StatusDone:
		fmt.Fprintf(env.out, "mission %s is already %s; nothing changed\n", id, st.Status)
		return nil
	}
	_, err = writeState(store, id, func(cur mission.State) (mission.State, mission.Event, error) {
		if cur.Status != mission.StatusRunning {
			return cur, mission.Event{}, fmt.Errorf("mission %s is %s, not running; nothing changed", id, cur.Status)
		}
		next := cur.Clone()
		next.Status = mission.StatusStopped
		return next, mission.Event{Kind: mission.EvStopped, By: "crew"}, nil
	})
	var driven drivenError
	switch {
	case errors.As(err, &driven):
		return stopEngine(env.out, store, id, driven.pid)
	case err != nil:
		return err
	}
	fmt.Fprintf(env.out, "mission %s wasn't running; marked stopped\n", id)
	return nil
}

func stopEngine(out io.Writer, store mission.Store, id string, pid int) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("can't signal pid %d: %w", pid, err)
	}
	deadline := time.Now().Add(stopWait)
	for time.Now().Before(deadline) {
		if _, live := store.LockedBy(); !live {
			fmt.Fprintf(out, "mission %s stopped\n", id)
			return nil
		}
		time.Sleep(stopPoll)
	}
	return fmt.Errorf("mission %s: the engine (pid %d) is still running %s after SIGTERM", id, pid, stopWait)
}

func missionResume(env missionEnv, id string) error {
	store, st, err := openMission(env, id)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(filepath.Join(store.Dir, "approved.md")); statErr != nil {
		hint := "go"
		if st.Status == mission.StatusPlanned {
			hint = "start"
		}
		return fmt.Errorf("mission %s was never started; use crew mission %s %s", id, hint, id)
	}
	if st.Status != mission.StatusStopped && st.Status != mission.StatusRunning {
		return fmt.Errorf("mission %s is %s; only a stopped mission (or a running one whose engine died) can be resumed", id, st.Status)
	}
	if err := startChecks(env, store); err != nil {
		return err
	}
	_, err = writeState(store, id, func(cur mission.State) (mission.State, mission.Event, error) {
		if cur.Status != mission.StatusStopped && cur.Status != mission.StatusRunning {
			return cur, mission.Event{}, fmt.Errorf("mission %s is %s; nothing was resumed", id, cur.Status)
		}
		next := cur.Clone()
		next.Status = mission.StatusRunning
		return next, mission.Event{}, nil
	})
	if err != nil {
		return err
	}
	return detachMission(env, store, id)
}
