package run

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/source"
)

// Engine drives one run until it finishes, fails, pauses for approval or is
// interrupted. It is the only thing that routes, counts and runs stations;
// `crew run` and the mission engine both go through it.
type Engine struct {
	Store    Store
	Factory  source.Factory // the frozen copy from Store.Factory()
	Agents   map[string]source.Agent
	Registry *config.Registry
	Runner   Runner
	Snap     func(project string, watch []string) (map[string]string, error)
	Project  string
	CrewDir  string    // absolute crew config directory; {crew_dir} in deny rules
	Out      io.Writer // human-readable events; nil = silent
}

// Drive runs stations until the run stops. A returned error means the run
// couldn't be driven at all (lock held, unreadable state, interrupted).
func (e *Engine) Drive(ctx context.Context) (State, error) {
	unlock, err := e.Store.Lock()
	if err != nil {
		return State{}, err
	}
	defer unlock()
	if killed, err := e.Store.KillLeftoverChild(); err != nil {
		return State{}, err
	} else if killed {
		e.emit(Event{Kind: EvNote, Msg: "stopped a station process left running by a driver that died"})
	}
	st, err := e.loop(ctx)
	if err != nil && !errors.Is(err, context.Canceled) && st.RunID != "" {
		// Record it, so `crew run status` shows why the run
		// stopped instead of a run that looks forever "running".
		st = FailRetryable(st, err.Error())
		e.emit(Event{Kind: EvFailed, Station: st.Current, Msg: st.Reason})
		if serr := e.Store.SaveState(st); serr != nil {
			return st, errors.Join(err, serr)
		}
	}
	return st, err
}

func (e *Engine) loop(ctx context.Context) (State, error) {
	st, d, err := e.start()
	if err != nil {
		return st, err
	}
	for {
		for d.Kind == RunStation {
			if st, d, err = e.next(ctx, st); err != nil {
				return st, err
			}
		}
		// A goto that arrived while the last station ran still applies.
		g, err := e.Store.TakeGoto()
		if err != nil {
			return st, err
		}
		if g == "" {
			return st, e.finish(st, d)
		}
		moved, gerr := Goto(e.Factory, st, g)
		if gerr != nil {
			e.emit(Event{Kind: EvNote, Msg: "ignored goto: " + gerr.Error()})
			return st, e.finish(st, d)
		}
		e.emit(Event{Kind: EvGoto, Station: g, Msg: moved.Reason})
		st, d = Resume(e.Factory, moved)
	}
}

func (e *Engine) guarded() bool {
	for _, s := range e.Factory.Stations {
		if s.OnDrift != "" {
			return true
		}
	}
	return false
}

// start loads the state: a fresh run takes its first snapshot, anything else
// resumes.
func (e *Engine) start() (State, Decision, error) {
	st, err := e.Store.LoadState()
	if err != nil {
		return st, Decision{}, err
	}
	evs, err := e.Store.EventsSince(0)
	if err != nil {
		return st, Decision{}, err
	}
	if len(evs) > 0 || st.Step > 0 {
		st, d := Resume(e.Factory, st)
		return st, d, nil
	}
	if e.guarded() {
		snap, err := e.Snap(e.Project, e.Factory.WatchRoots())
		if err != nil {
			return st, Decision{}, err
		}
		st = WithSnapshot(st, snap)
	}
	e.emit(Event{Kind: EvStart, Station: st.Current, Msg: st.RunID})
	return st, Decision{Kind: RunStation, Station: st.Current}, e.Store.SaveState(st)
}

// next applies a pending goto and the drift guard, then runs one station.
func (e *Engine) next(ctx context.Context, st State) (State, Decision, error) {
	if g, err := e.Store.TakeGoto(); err != nil {
		return st, Decision{}, err
	} else if g != "" {
		if moved, err := Goto(e.Factory, st, g); err == nil {
			st = moved
			e.emit(Event{Kind: EvGoto, Station: g, Msg: st.Reason})
		} else {
			e.emit(Event{Kind: EvNote, Msg: "ignored goto: " + err.Error()})
		}
	}
	var drifted []string
	if e.guarded() {
		cur, err := e.Snap(e.Project, e.Factory.WatchRoots())
		if err != nil {
			return st, Decision{}, err
		}
		drifted = Drifted(st.Snapshot, cur)
	}
	before := st.Current
	skipping := st.SkipDriftOnce
	st, _ = BeforeStation(e.Factory, st, drifted)
	switch {
	case st.Current != before:
		e.emit(Event{Kind: EvDrift, Station: st.Current, Msg: st.Reason})
	case skipping && len(drifted) > 0 && e.Factory.Stations[before].OnDrift != "":
		e.emit(Event{Kind: EvNote, Station: before, Msg: "drift check skipped on resume: the changes are this station's own unfinished work"})
	}
	st = MarkInStation(st)
	if err := e.Store.SaveState(st); err != nil {
		return st, Decision{}, err
	}
	return e.runStep(ctx, st)
}

func (e *Engine) finish(st State, d Decision) error {
	switch d.Kind {
	case AwaitApproval:
		p := st.Pending
		e.emit(Event{Kind: EvApproval, Station: p.Station, Route: p.Route,
			Msg: fmt.Sprintf("%s chose %s: %s — crew run approve %s [--goto STATION]", p.Station, p.Route, p.Verdict.Summary, st.RunID)})
	case Finished:
		e.emit(Event{Kind: EvDone, Msg: d.Reason})
	case Failed:
		e.emit(Event{Kind: EvFailed, Station: st.Current, Msg: st.Reason})
	}
	return e.Store.SaveState(st)
}

func (e *Engine) emit(ev Event) {
	ev, err := e.Store.Emit(ev)
	if e.Out == nil {
		return
	}
	if err != nil {
		fmt.Fprintf(e.Out, "crew: couldn't record event: %v\n", err)
	}
	fmt.Fprintln(e.Out, FormatEvent(ev))
}
