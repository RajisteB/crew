package mission

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/notify"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

const (
	lockAttempts  = 3
	lockRetryWait = 100 * time.Millisecond
	defaultEvery  = 5 * time.Second
	engineBy      = "engine"
)

// Engine drives one mission. Each tick it observes, decides and acts, with
// every side effect behind an interface. An effect that fails changes
// nothing: it becomes a note and the next tick tries again.
type Engine struct {
	Store    Store
	RepoRoot string
	Settings Settings
	Checks   CheckInput
	Domains  map[string]source.Domain
	Git      GitOps
	GH       GHOps
	Runs     RunOps
	Orch     Orchestrator
	Notifier notify.Notifier
	Now      func() time.Time
	Out      io.Writer
	// RepoGate re-checks, before every push, that the repo is still
	// confirmed at its current visibility. Nil means no check.
	RepoGate func(ctx context.Context) error
	// Switches holds the auto-merge switches. Nil means every domain is off.
	Switches SwitchReader
	// MergeMethod is the gh merge method; empty means squash.
	MergeMethod string
	// Tracker is the Jira board. Nil means the repo has no Jira: nothing is
	// planned or sent.
	Tracker Tracker
	// Sprints says the repo's board uses sprints.
	Sprints bool
	// TrackerPermanent says an error can never succeed. Nil means none can.
	TrackerPermanent func(error) bool
	// TrackerDown says an error means the tracker itself can't be reached, so
	// the rest of the pass would fail too. Nil means every error that is not
	// permanent counts as down.
	TrackerDown func(error) bool

	lastNotes   map[string]struct{} // notes of the previous tick, to avoid repeating them
	streaks     map[string]streak   // failures seen in an unbroken run of ticks, by failureKey
	attachTries map[string]int      // failed uploads by piece and file, in memory only: a new engine tries again
}

func (e *Engine) notifier() notify.Notifier {
	if e.Notifier == nil {
		return notify.Nop{}
	}
	return e.Notifier
}

func (e *Engine) out() io.Writer {
	if e.Out == nil {
		return io.Discard
	}
	return e.Out
}

func (e *Engine) now() time.Time {
	if e.Now == nil {
		return time.Now()
	}
	return e.Now()
}

// tick is the working copy of one Tick.
type tick struct {
	e        *Engine
	ctx      context.Context
	m        Mission
	s        State
	lim      Limits
	in       CheckInput
	now      time.Time
	obs      Obs
	notes    map[string]struct{}
	mDirty   bool
	switches *switchSnapshot // the switches read this tick, once
	halted   bool            // the mission was stopped during this tick; no further actions run
}

// Tick runs one round. The caller holds the mission lock. A failure that
// repeats tick after tick is told to the principal once (see trackFailures).
func (e *Engine) Tick(ctx context.Context) (State, error) {
	t, err := e.newTick(ctx)
	if err != nil {
		e.trackFailures(nil, err)
		return State{}, err
	}
	if t.s.Status != StatusRunning {
		return t.s, nil // a mission that isn't running applies no answers and starts nothing
	}
	st, err := t.run()
	e.lastNotes = t.notes
	e.trackFailures(t.notes, err)
	return st, err
}

// run is the body of a tick: inbox, observe, decide, act, save.
func (t *tick) run() (State, error) {
	e := t.e
	acked, err := t.applyInbox()
	if err != nil {
		return t.s, err
	}
	t.observe()
	next, actions := Decide(t.m, t.s, t.obs, t.lim, e.Settings.Remind())
	t.s = next
	if err := t.perform(actions); err != nil {
		return t.s, err
	}
	if err := t.save(); err != nil {
		return t.s, err
	}
	if err := e.Store.Ack(acked); err != nil {
		return t.s, err
	}
	if e.Tracker != nil {
		if _, err := t.syncTracker(); err != nil {
			return t.s, err
		}
	}
	return t.s, nil
}

func (e *Engine) newTick(ctx context.Context) (*tick, error) {
	m, err := e.Store.Mission()
	if err != nil {
		return nil, err
	}
	s, err := e.Store.LoadState()
	if err != nil {
		return nil, err
	}
	lim := e.Settings.For(s.Repo)
	in := e.Checks
	in.Limits = lim
	return &tick{e: e, ctx: ctx, m: m, s: s, lim: lim, in: in, now: e.now(), notes: map[string]struct{}{}}, nil
}

// save writes the mission when it changed, then the state.
func (t *tick) save() error {
	if t.mDirty {
		if err := t.e.Store.SaveMission(t.m); err != nil {
			return err
		}
		t.mDirty = false
	}
	return t.e.Store.SaveState(t.s)
}

// adopt takes a new mission and state produced by a state-machine function.
func (t *tick) adopt(m Mission, s State) {
	if len(m.Changes) != len(t.m.Changes) {
		t.mDirty = true
	}
	t.m, t.s = m, s
}

// emit appends an event. A failure is reported on Out: events are a log, and
// a log that can't be written must not stop the mission.
func (t *tick) emit(ev Event) {
	if ev.Time.IsZero() {
		ev.Time = t.now
	}
	if ev.By == "" {
		ev.By = engineBy
	}
	if _, err := t.e.Store.Emit(ev); err != nil {
		fmt.Fprintf(t.e.out(), "mission %s: can't record %s event: %v\n", t.s.MissionID, ev.Kind, err)
	}
}

// note records a failed effect. The same note for the same piece as on the
// previous tick is not repeated.
func (t *tick) note(piece, format string, args ...any) {
	msg := cutRunes(fmt.Sprintf(format, args...), noteRunes)
	key := piece + "\x00" + msg
	t.notes[key] = struct{}{}
	if _, seen := t.e.lastNotes[key]; seen {
		return
	}
	t.emit(Event{Kind: EvNote, Piece: piece, Msg: msg})
}

// ---------- observation ----------

// observe fills t.obs: runs for running pieces, git for those whose run is
// done, pull requests for pr-open pieces. An observation that fails is a
// note and leaves that piece out.
func (t *tick) observe() {
	o := Obs{Now: t.now, Runs: map[string]RunObs{}, Git: map[string]GitObs{}, Delivered: map[string]Found{}, PRs: map[string]gh.PR{}, Auto: map[string]AutoObs{}}
	t.obs = o
	for _, p := range t.s.Pieces {
		switch {
		case p.Status == PieceRunning && p.RunID != "":
			t.observeRun(p)
		case p.Status == PiecePROpen && p.PR != 0:
			t.observePR(p)
		}
	}
	t.observeAccept()
	t.observeElsewhere()
	t.observeBehind()
}

func (t *tick) observeRun(p PieceState) {
	ro, err := t.e.Runs.Observe(p.RunID)
	if err != nil {
		t.note(p.ID, "can't observe the run: %v", err)
		return
	}
	t.obs.Runs[p.ID] = ro
	if ro.State.Status != run.StatusDone {
		return
	}
	found, err := t.listScratch(p.ID, p.Worktree)
	if err != nil {
		t.note(p.ID, "can't read the deliverables folder: %v", err)
		return
	}
	dirty, err := t.e.Git.Dirty(t.ctx, p.Worktree)
	if err != nil {
		t.note(p.ID, "can't check the worktree: %v", err)
		return
	}
	ahead, err := t.e.Git.Ahead(t.ctx, p.Worktree, "origin/"+t.s.Base)
	if err != nil {
		t.note(p.ID, "can't count commits: %v", err)
		return
	}
	outside, ok := t.observeOutside(p, ahead)
	if !ok {
		return
	}
	t.obs.Git[p.ID] = GitObs{Ahead: ahead, Dirty: dirty, Outside: outside}
	t.obs.Delivered[p.ID] = found
}

func (t *tick) observePR(p PieceState) {
	pr, err := t.e.GH.PRState(t.ctx, p.PR)
	if err != nil {
		t.note(p.ID, "can't read PR #%d: %v", p.PR, err)
		return
	}
	t.obs.PRs[p.ID] = pr
	t.observeAuto(p, pr)
}

// ---------- loop ----------

// Loop drives the mission until it is done or stopped, or ctx ends. It holds
// the mission lock for as long as it runs. A tick's error is written to Out
// and the loop goes on.
func (e *Engine) Loop(ctx context.Context, every time.Duration) (State, error) {
	unlock, err := e.lock(ctx)
	if err != nil {
		return State{}, err
	}
	defer unlock()
	if every <= 0 {
		every = defaultEvery
	}
	if evs, err := e.Store.EventsSince(0); err == nil && len(evs) > 0 {
		e.emit(Event{Kind: EvResumed})
	}
	e.retryCleanups(ctx)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		st, err := e.Tick(ctx)
		switch {
		case err != nil:
			fmt.Fprintf(e.out(), "mission tick: %v\n", err)
		case st.Status != StatusRunning:
			e.drainTracker(ctx, every)
			return st, nil
		}
		select {
		case <-ctx.Done():
			return e.stopAll()
		case <-ticker.C:
		}
	}
}

// lock takes the mission lock, retrying briefly: a status command's
// liveness probe can make one attempt fail.
func (e *Engine) lock(ctx context.Context) (func(), error) {
	var err error
	for i := range lockAttempts {
		var unlock func()
		if unlock, err = e.Store.Lock(); err == nil {
			return unlock, nil
		}
		if i == lockAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockRetryWait):
		}
	}
	return nil, err
}

// retryCleanups retries the removal of finished pieces' worktrees.
func (e *Engine) retryCleanups(ctx context.Context) {
	t, err := e.newTick(ctx)
	if err != nil {
		fmt.Fprintf(e.out(), "mission: can't retry cleanups: %v\n", err)
		return
	}
	t.retryCleanups()
	e.lastNotes = t.notes
	e.trackFailures(t.notes, nil)
}

func (e *Engine) emit(ev Event) {
	ev.Time, ev.By = e.now(), engineBy
	if _, err := e.Store.Emit(ev); err != nil {
		fmt.Fprintf(e.out(), "mission: can't record %s event: %v\n", ev.Kind, err)
	}
}

// stopAll stops every running piece's run and marks the mission stopped.
func (e *Engine) stopAll() (State, error) {
	s, err := e.Store.LoadState()
	if err != nil {
		return State{}, err
	}
	for _, p := range s.Pieces {
		if p.Status != PieceRunning || p.RunID == "" {
			continue
		}
		if err := e.Runs.Stop(p.RunID); err != nil {
			fmt.Fprintf(e.out(), "mission: can't stop run %s of %s: %v\n", p.RunID, p.ID, err)
		}
	}
	next := s.Clone()
	next.Status = StatusStopped
	if err := e.Store.SaveState(next); err != nil {
		return s, err
	}
	e.emit(Event{Kind: EvStopped})
	return next, nil
}
