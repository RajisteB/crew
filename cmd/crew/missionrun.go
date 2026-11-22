package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/gitx"
	"github.com/crewfile-dev/crew/internal/jira"
	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/notify"
	"github.com/crewfile-dev/crew/internal/principal"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

// maxEngineRestarts is how many times the supervisor restarts a crashed engine.
const maxEngineRestarts = 3

// Seams: tests replace these so no real harness, notification or child
// process is ever started.
var (
	newRunner     = func(reg *config.Registry) run.Runner { return run.HeadlessRunner{Registry: reg} }
	newNotifier   = func() notify.Notifier { return notify.New(runtime.GOOS, nil) }
	startDetached = detachSupervisor
	tickEvery     = 60 * time.Second
	engineCmd     = func(self string, args []string) *exec.Cmd { return exec.Command(self, args...) }
)

// detachSupervisor starts `crew mission run <id>` in its own session, with
// its output appended to logPath, the way driveOrDetach does for runs.
func detachSupervisor(id, dir, project, logPath string) (int, error) {
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer logf.Close()
	self, err := os.Executable()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(self, "mission", "run", id, "--project", project, "--dir", dir)
	cmd.Stdout, cmd.Stderr, cmd.Dir = logf, logf, project
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	return pid, cmd.Process.Release()
}

// newMissionEngine builds the engine with the real operations. It starts
// nothing and handles no signals.
func newMissionEngine(store mission.Store, crewDir, repoRoot string, out io.Writer) (*mission.Engine, *mission.LocalRuns, error) {
	st, err := store.LoadState()
	if err != nil {
		return nil, nil, err
	}
	settings, err := mission.LoadSettings(crewDir)
	if err != nil {
		return nil, nil, err
	}
	p, err := verifiedProject(crewDir, out)
	if err != nil {
		return nil, nil, err
	}
	chk := newChecks(p, settings, st.Repo)
	domains := map[string]source.Domain{}
	for _, d := range p.Domains {
		domains[d.Name] = d
	}
	runner := newRunner(p.Registry)
	runs := &mission.LocalRuns{RepoRoot: repoRoot, CrewDir: crewDir, Project: p, Runner: runner, Out: out}
	e := &mission.Engine{
		Store: store, RepoRoot: repoRoot, Settings: settings, Checks: chk.in, Domains: domains,
		Git: gitx.Repo{Dir: repoRoot}, GH: ghClient(repoRoot), Runs: runs,
		Orch:     mission.HeadlessOrchestrator{Registry: p.Registry, Runner: runner, Harness: settings.Harness, CrewDir: crewDir},
		Notifier: newNotifier(), Out: out, RepoGate: repoGate(crewDir, repoRoot),
		Switches: principal.Store{Dir: filepath.Join(crewDir, "principal")}, MergeMethod: settings.Method(),
	}
	if err := setTracker(e, crewDir, st.Repo); err != nil {
		return nil, nil, err
	}
	return e, runs, nil
}

// setTracker gives the engine the repo's Jira board, if jira.yaml names one.
func setTracker(e *mission.Engine, crewDir, repo string) error {
	t, sprints, err := trackerFor(crewDir, repo, jiraEnv)
	if err != nil {
		return err
	}
	e.Tracker, e.Sprints, e.TrackerPermanent = t, sprints, jira.IsPermanent
	return nil
}

func missionRun(env missionEnv, id string) error {
	store, err := mission.Open(env.repoRoot, id)
	if err != nil {
		return err
	}
	if env.flags.engine {
		return runEngine(env, store)
	}
	return runSupervisor(env, store, id)
}

func runEngine(env missionEnv, store mission.Store) error {
	e, runs, err := newMissionEngine(store, env.crewDir, env.repoRoot, env.out)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_, err = e.Loop(ctx, tickEvery)
	runs.Wait()
	return err
}

// runSupervisor keeps one engine child alive, restarting it when it crashes.
// A signal for the supervisor is forwarded to the child and ends both.
func runSupervisor(env missionEnv, store mission.Store, id string) error {
	logf, err := os.OpenFile(filepath.Join(store.Dir, "mission.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	args := []string{"mission", "run", id, "--engine", "--dir", env.crewDir, "--project", env.project}
	start := func(ctx context.Context) error {
		cmd := engineCmd(self, args)
		cmd.Stdout, cmd.Stderr, cmd.Dir = logf, logf, env.project
		return runChild(ctx, cmd)
	}
	notifier := newNotifier()
	onCrash := func(n int, err error) {
		msg := fmt.Sprintf("engine crashed, restarting (%d/%d)", n, maxEngineRestarts)
		store.Emit(mission.Event{Kind: mission.EvNote, By: "crew", Msg: msg})
		notifier.Notify("crew", fmt.Sprintf("mission %s: %s", id, msg))
	}
	err = mission.Supervise(ctx, start, maxEngineRestarts, onCrash)
	if err == nil {
		return nil
	}
	notifier.Notify("crew", fmt.Sprintf("mission %s stopped: the engine keeps crashing; see mission.log", id))
	if serr := stopAfterCrashes(store); serr != nil {
		fmt.Fprintf(logf, "mission %s: can't record the stop: %v\n", id, serr)
	}
	return fmt.Errorf("mission %s: the engine keeps crashing: %w", id, err)
}

// runChild runs cmd to its end, forwarding SIGTERM to it if ctx ends first.
func runChild(ctx context.Context, cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cmd.Process.Signal(syscall.SIGTERM)
		case <-done:
		}
	}()
	err := cmd.Wait()
	close(done)
	return err
}

// stopAfterCrashes marks a mission stopped once its supervisor has given up.
func stopAfterCrashes(store mission.Store) error {
	id := filepath.Base(store.Dir)
	_, err := writeState(store, id, func(cur mission.State) (mission.State, mission.Event, error) {
		if cur.Status != mission.StatusRunning {
			return cur, mission.Event{}, nil
		}
		next := cur.Clone()
		next.Status = mission.StatusStopped
		return next, mission.Event{Kind: mission.EvStopped, By: "crew", Msg: "the engine keeps crashing"}, nil
	})
	var driven drivenError
	if errors.As(err, &driven) {
		return nil // an engine is alive after all; it owns the state
	}
	return err
}
