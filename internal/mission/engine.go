package mission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	lastNotes map[string]struct{} // notes of the previous tick, to avoid repeating them
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
	e      *Engine
	ctx    context.Context
	m      Mission
	s      State
	lim    Limits
	in     CheckInput
	now    time.Time
	obs    Obs
	notes  map[string]struct{}
	mDirty bool
}

// Tick runs one round. The caller holds the mission lock.
func (e *Engine) Tick(ctx context.Context) (State, error) {
	m, err := e.Store.Mission()
	if err != nil {
		return State{}, err
	}
	s, err := e.Store.LoadState()
	if err != nil {
		return State{}, err
	}
	lim := e.Settings.For(s.Repo)
	in := e.Checks
	in.Limits = lim
	t := &tick{e: e, ctx: ctx, m: m, s: s, lim: lim, in: in, now: e.now(), notes: map[string]struct{}{}}
	defer func() { e.lastNotes = t.notes }()

	acked, err := t.applyInbox()
	if err != nil {
		return t.s, err
	}
	t.observe()
	next, actions := Decide(t.m, t.s, t.obs, lim, e.Settings.Remind())
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
	return t.s, nil
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

// ---------- inbox ----------

// applyInbox applies each waiting item and returns those to acknowledge once
// the state is saved. An item that can't be read or applied is a note and is
// still acknowledged; one whose run step failed stays for the next tick.
func (t *tick) applyInbox() ([]Item, error) {
	items, err := t.e.Store.Inbox()
	if err != nil {
		return nil, err
	}
	var acked []Item
	for _, it := range items {
		done, err := t.applyItem(it)
		if err != nil {
			return acked, err
		}
		if done {
			acked = append(acked, it)
		}
	}
	return acked, nil
}

func (t *tick) applyItem(it Item) (bool, error) {
	var (
		m     Mission
		s     State
		acts  []Action
		steps []RunStep
		err   error
	)
	switch it.Kind {
	case "answer":
		m, s, acts, steps, err = t.answerItem(it)
	case "amend":
		m, s, acts, steps, err = t.amendItem(it)
	default:
		err = fmt.Errorf("unknown kind %q", it.Kind)
	}
	if err != nil {
		t.note("", "inbox item %s: %v", it.Name, err)
		return true, nil
	}
	for i := range steps {
		if err := t.runStep(&steps[i]); err != nil {
			t.note(steps[i].Piece, "inbox item %s: can't carry it out yet: %v", it.Name, err)
			return false, nil
		}
	}
	t.adopt(m, s)
	if err := t.save(); err != nil {
		return false, err
	}
	return true, t.perform(acts)
}

func (t *tick) answerItem(it Item) (Mission, State, []Action, []RunStep, error) {
	var a Answer
	if err := decodeStrict(it.Body, &a); err != nil {
		return t.m, t.s, nil, nil, err
	}
	m, s, acts, step, err := ApplyAnswer(t.m, t.s, a, t.in, t.now)
	if err != nil || step == nil {
		return m, s, acts, nil, err
	}
	return m, s, acts, []RunStep{*step}, nil
}

func (t *tick) amendItem(it Item) (Mission, State, []Action, []RunStep, error) {
	var a Amendment
	if err := decodeStrict(it.Body, &a); err != nil {
		return t.m, t.s, nil, nil, err
	}
	return Amend(t.m, t.s, a, t.in, t.now)
}

// decodeStrict decodes one JSON value and refuses unknown fields and
// trailing content.
func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("can't read it: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("can't read it: content after the JSON value")
	}
	return nil
}

// ---------- observation ----------

// observe fills t.obs: runs for running pieces, git for those whose run is
// done, pull requests for pr-open pieces. An observation that fails is a
// note and leaves that piece out.
func (t *tick) observe() {
	o := Obs{Now: t.now, Runs: map[string]RunObs{}, Git: map[string]GitObs{}, PRs: map[string]gh.PR{}}
	t.obs = o
	for _, p := range t.s.Pieces {
		switch {
		case p.Status == PieceRunning && p.RunID != "":
			t.observeRun(p)
		case p.Status == PiecePROpen && p.PR != 0:
			t.observePR(p)
		}
	}
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
	dirty, err := t.e.Git.Dirty(p.Worktree)
	if err != nil {
		t.note(p.ID, "can't check the worktree: %v", err)
		return
	}
	ahead, err := t.e.Git.Ahead(p.Worktree, "origin/"+t.s.Base)
	if err != nil {
		t.note(p.ID, "can't count commits: %v", err)
		return
	}
	t.obs.Git[p.ID] = GitObs{Ahead: ahead, Dirty: dirty}
}

func (t *tick) observePR(p PieceState) {
	pr, err := t.e.GH.PRState(t.ctx, p.PR)
	if err != nil {
		t.note(p.ID, "can't read PR #%d: %v", p.PR, err)
		return
	}
	t.obs.PRs[p.ID] = pr
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
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		st, err := e.Tick(ctx)
		switch {
		case err != nil:
			fmt.Fprintf(e.out(), "mission tick: %v\n", err)
		case st.Status != StatusRunning:
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
