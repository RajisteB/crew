package mission

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/gitx"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
	"github.com/crewfile-dev/crew/internal/verify"
)

var (
	_ RunOps       = (*LocalRuns)(nil)
	_ Orchestrator = HeadlessOrchestrator{}
	_ GitOps       = gitx.Repo{}
	_ GHOps        = gh.Client{}
)

// stopWait is how long Stop waits for a run's goroutine to return.
const stopWait = 15 * time.Second

// LocalRuns drives factory runs in this process, one goroutine per run. The
// run folder lives under RepoRoot; stations work in the piece's worktree.
type LocalRuns struct {
	MissionID         string // the mission these runs belong to
	RepoRoot, CrewDir string
	Project           *verify.Project
	Runner            run.Runner
	Out               io.Writer

	mu      sync.Mutex
	driving map[string]*drive
	wg      sync.WaitGroup
}

// drive is one run's engine goroutine.
type drive struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// RunOwner is the Owner of a run started for a piece: "mission <missionID> piece <pieceID>".
func RunOwner(missionID, pieceID string) string {
	return "mission " + missionID + " piece " + pieceID
}

// Start creates the run folder in the main repo from the piece's factory. It
// does not drive the run.
func (l *LocalRuns) Start(p Piece, input, worktree string) (string, error) {
	if l.MissionID == "" {
		return "", errors.New("mission runs need a mission id")
	}
	f, ok := l.factory(p.Factory)
	if !ok {
		return "", fmt.Errorf("piece %s: no factory %q", p.ID, p.Factory)
	}
	path := f.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(l.CrewDir, path) // verify gives paths relative to the crew directory
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("piece %s: %w", p.ID, err)
	}
	store, st, err := run.NewRun(l.RepoRoot, f, src, input, time.Now())
	if err != nil {
		return "", err
	}
	st.Owner = RunOwner(l.MissionID, p.ID)
	if err := store.SaveState(st); err != nil {
		return "", err
	}
	return filepath.Base(store.Dir), nil
}

func (l *LocalRuns) factory(name string) (source.Factory, bool) {
	for _, f := range l.Project.Factories {
		if f.Name == name {
			return f, true
		}
	}
	return source.Factory{}, false
}

// Observe reports the run's state and whether a process is driving it.
func (l *LocalRuns) Observe(runID string) (RunObs, error) {
	store, err := run.OpenRun(l.RepoRoot, runID)
	if err != nil {
		return RunObs{}, err
	}
	st, err := store.LoadState()
	if err != nil {
		return RunObs{}, err
	}
	return RunObs{State: st, Live: l.live(store, runID)}, nil
}

// live is true when this LocalRuns drives the run or a live process holds its lock.
func (l *LocalRuns) live(store run.Store, runID string) bool {
	l.mu.Lock()
	_, mine := l.driving[runID]
	l.mu.Unlock()
	if mine {
		return true
	}
	_, held := store.LockedBy()
	return held
}

// Drive starts the run's engine in a goroutine. It returns nil at once when a
// driver is already live. A stopped run needs no preparation here: the
// engine resumes it itself.
func (l *LocalRuns) Drive(ctx context.Context, runID, worktree string) error {
	store, err := run.OpenRun(l.RepoRoot, runID)
	if err != nil {
		return err
	}
	e, err := l.engine(store, worktree)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, mine := l.driving[runID]; mine {
		return nil
	}
	if _, held := store.LockedBy(); held {
		return nil
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	d := &drive{cancel: cancel, done: make(chan struct{})}
	if l.driving == nil {
		l.driving = map[string]*drive{}
	}
	l.driving[runID] = d
	l.wg.Add(1)
	go l.drive(runCtx, runID, e, d)
	return nil
}

func (l *LocalRuns) drive(ctx context.Context, runID string, e *run.Engine, d *drive) {
	defer l.wg.Done()
	_, err := e.Drive(ctx)
	l.mu.Lock()
	delete(l.driving, runID)
	l.mu.Unlock()
	d.cancel()
	close(d.done)
	if err != nil && l.Out != nil && !errors.Is(err, context.Canceled) && !run.IsCanceled(err) {
		fmt.Fprintf(l.Out, "crew: run %s: %v\n", runID, err)
	}
}

func (l *LocalRuns) engine(store run.Store, worktree string) (*run.Engine, error) {
	fac, err := store.Factory()
	if err != nil {
		return nil, err
	}
	if l.Project == nil || l.Project.Registry == nil {
		return nil, errors.New("harnesses.yaml failed to load; run crew verify")
	}
	agents := map[string]source.Agent{}
	for _, a := range l.Project.Agents {
		agents[a.Name] = a
	}
	return &run.Engine{
		Store: store, Factory: fac, Agents: agents, Registry: l.Project.Registry,
		Runner: l.Runner, Snap: run.Snapshot, Project: worktree, CrewDir: l.CrewDir, Out: l.Out,
	}, nil
}

// Stop cancels the run's context, which kills the station's process group,
// and waits for the goroutine. It is nil when this LocalRuns isn't driving the run.
func (l *LocalRuns) Stop(runID string) error {
	l.mu.Lock()
	d, ok := l.driving[runID]
	l.mu.Unlock()
	if !ok {
		return nil
	}
	d.cancel()
	select {
	case <-d.done:
		return nil
	case <-time.After(stopWait):
		return fmt.Errorf("run %s did not stop within %s", runID, stopWait)
	}
}

// stopAll cancels every driven run and waits for them.
func (l *LocalRuns) stopAll() {
	l.mu.Lock()
	for _, d := range l.driving {
		d.cancel()
	}
	l.mu.Unlock()
	l.wg.Wait()
}

// Wait blocks until every driven run has returned.
func (l *LocalRuns) Wait() { l.wg.Wait() }

// Summary returns the run's state, factory, last step and steps folder.
func (l *LocalRuns) Summary(runID string) (run.State, source.Factory, run.StepRecord, string, error) {
	store, err := run.OpenRun(l.RepoRoot, runID)
	if err != nil {
		return run.State{}, source.Factory{}, run.StepRecord{}, "", err
	}
	st, err := store.LoadState()
	if err != nil {
		return run.State{}, source.Factory{}, run.StepRecord{}, "", err
	}
	fac, err := store.Factory()
	if err != nil {
		return run.State{}, source.Factory{}, run.StepRecord{}, "", err
	}
	steps, err := store.Steps()
	if err != nil {
		return run.State{}, source.Factory{}, run.StepRecord{}, "", err
	}
	var last run.StepRecord
	if len(steps) > 0 {
		last = steps[len(steps)-1]
	}
	dir, err := filepath.Abs(filepath.Join(store.Dir, "steps"))
	return st, fac, last, dir, err
}

// Step changes the run's state the way the run commands do, saves it, and
// drives the run on. It refuses a run that is being driven.
func (l *LocalRuns) Step(ctx context.Context, runID, worktree string, s RunStep) error {
	store, err := run.OpenRun(l.RepoRoot, runID)
	if err != nil {
		return err
	}
	if l.live(store, runID) {
		return fmt.Errorf("run %s is being driven; stop it before changing it", runID)
	}
	fac, err := store.Factory()
	if err != nil {
		return err
	}
	st, err := store.LoadState()
	if err != nil {
		return err
	}
	next, err := applyStep(fac, st, store, s)
	if err != nil {
		return err
	}
	if err := store.SaveState(next); err != nil {
		return err
	}
	return l.Drive(ctx, runID, worktree)
}

func applyStep(fac source.Factory, st run.State, store run.Store, s RunStep) (run.State, error) {
	switch s.Kind {
	case "retry":
		return run.Goto(fac, st, st.Current)
	case "goto":
		next, err := run.GotoFresh(fac, st, s.Station, "redirected to "+s.Station+" by the orchestrator")
		if err != nil {
			return st, err
		}
		return next, queueNote(store, s.Note)
	case "approve":
		next, _, err := run.Approve(fac, st, s.Station)
		if err != nil {
			return st, err
		}
		return next, queueNote(store, s.Note)
	}
	return st, fmt.Errorf("unknown step kind %q", s.Kind)
}

func queueNote(store run.Store, note string) error {
	if note == "" {
		return nil
	}
	return store.AppendInbox(note)
}
