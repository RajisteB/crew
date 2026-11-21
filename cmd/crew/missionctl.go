package main

import (
	"errors"
	"fmt"
	"io"
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

func missionStop(env missionEnv, id string) error {
	store, st, err := openMission(env, id)
	if err != nil {
		return err
	}
	if pid, live := store.LockedBy(); live {
		return stopEngine(env.out, store, id, pid)
	}
	switch st.Status {
	case mission.StatusRunning, mission.StatusAwaitingGo:
		return markStopped(env.out, store, st)
	}
	fmt.Fprintf(env.out, "mission %s is %s; nothing to stop\n", id, st.Status)
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

// markStopped records a stop when no engine is alive to do it.
func markStopped(out io.Writer, store mission.Store, st mission.State) error {
	st = st.Clone()
	st.Status = mission.StatusStopped
	if err := store.SaveState(st); err != nil {
		return err
	}
	if _, err := store.Emit(mission.Event{Kind: mission.EvStopped, By: "crew"}); err != nil {
		return err
	}
	fmt.Fprintf(out, "mission %s wasn't running; marked stopped\n", st.MissionID)
	return nil
}

func missionResume(env missionEnv, id string) error {
	store, st, err := openMission(env, id)
	if err != nil {
		return err
	}
	if err := requireIdle(store, id); err != nil {
		return err
	}
	if st.Status != mission.StatusStopped && st.Status != mission.StatusRunning {
		return fmt.Errorf("mission %s is %s; only a stopped mission (or a running one whose engine died) can be resumed", id, st.Status)
	}
	if err := startChecks(env, store); err != nil {
		return err
	}
	st = st.Clone()
	st.Status = mission.StatusRunning
	if err := store.SaveState(st); err != nil {
		return err
	}
	return detachMission(env, store, id)
}
