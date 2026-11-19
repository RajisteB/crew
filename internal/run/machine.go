package run

import (
	"fmt"
	"strings"

	"github.com/crewfile-dev/crew/internal/source"
)

// Begin is the state of a run that hasn't run any station yet.
func Begin(f source.Factory, runID string) State {
	return State{RunID: runID, Factory: f.Name, Status: StatusRunning, Current: f.Start, RouteCounts: map[string]int{}}
}

// BeforeStation applies the drift guard of the station about to run.
// drifted lists watched roots that changed since the previous step ended.
// Drift redirects never count toward route limits.
func BeforeStation(f source.Factory, s State, drifted []string) (State, Decision) {
	c := cloneState(s)
	skip := c.SkipDriftOnce
	c.SkipDriftOnce = false
	guard := f.Stations[c.Current].OnDrift
	if len(drifted) > 0 && guard != "" && !skip {
		c.Reason = fmt.Sprintf("drift in %s before %s", strings.Join(drifted, ", "), c.Current)
		c.Current = guard
	}
	return c, Decision{Kind: RunStation, Station: c.Current, Reason: c.Reason}
}

// AfterStation records the station's verdict and decides what runs next.
func AfterStation(f source.Factory, s State, v Verdict) (State, Decision) {
	c := cloneState(s)
	c.Step++
	c.Reason = ""
	c.InStation = false
	st := f.Stations[c.Current]
	if v.Route == source.RouteFail {
		return failState(c, v.Summary)
	}
	r, ok := st.Routes[v.Route]
	if !ok {
		return failState(c, fmt.Sprintf("%s returned unknown route %q", c.Current, v.Route))
	}
	key := c.Current + "." + v.Route
	c.RouteCounts[key]++
	if r.Max.Set && !r.Max.Unlimited && c.RouteCounts[key] > r.Max.N {
		return failState(c, fmt.Sprintf("route %s hit its limit (%d)", key, r.Max.N))
	}
	if st.Approve {
		c.Status = StatusAwaitingApproval
		c.Pending = &Pending{Station: c.Current, Route: v.Route, Verdict: v}
		return c, Decision{Kind: AwaitApproval, Station: c.Current}
	}
	return moveTo(c, r.To, v.Summary)
}

// Approve continues a run paused at an approval stop: along the route the
// station chose (already counted), or to gotoStation instead.
func Approve(f source.Factory, s State, gotoStation string) (State, Decision, error) {
	if s.Status != StatusAwaitingApproval || s.Pending == nil {
		return s, Decision{}, fmt.Errorf("run %s is %s, not awaiting approval", s.RunID, s.Status)
	}
	c := cloneState(s)
	p := *c.Pending
	c.Pending = nil
	c.Status = StatusRunning
	if gotoStation != "" {
		if _, ok := f.Stations[gotoStation]; !ok {
			return s, Decision{}, fmt.Errorf("no station %q in factory %s", gotoStation, f.Name)
		}
		c.Current = gotoStation
		c.Reason = "approval redirected to " + gotoStation
		return c, Decision{Kind: RunStation, Station: gotoStation}, nil
	}
	next, d := moveTo(c, f.Stations[p.Station].Routes[p.Route].To, p.Verdict.Summary)
	return next, d, nil
}

// Goto sends the run to a station on the user's request. On a live run it
// takes effect before the next station; a stopped run reopens there.
func Goto(f source.Factory, s State, station string) (State, error) {
	if _, ok := f.Stations[station]; !ok {
		return s, fmt.Errorf("no station %q in factory %s", station, f.Name)
	}
	c := cloneState(s)
	c.Current = station
	c.Pending = nil
	c.Reason = "redirected to " + station + " by the user"
	c.Retryable = false
	if c.Status != StatusRunning {
		// A stopped run (paused, failed, done, interrupted) reopens: the next
		// resume runs the chosen station.
		c.Status = StatusInterrupted
	}
	return c, nil
}

// MarkInStation records that the current station is about to run, so a
// crash mid-station resumes without treating its own edits as drift.
func MarkInStation(s State) State {
	c := cloneState(s)
	c.InStation = true
	return c
}

// Interrupt marks a run stopped mid-station (Ctrl-C, SIGTERM).
func Interrupt(s State, reason string) State {
	c := cloneState(s)
	c.Status = StatusInterrupted
	c.Reason = reason
	return c
}

// FailRetryable marks a run failed because the station couldn't run at all
// (missing CLI, timeout, crash); resume re-runs the same station.
func FailRetryable(s State, reason string) State {
	c := cloneState(s)
	c.Status = StatusFailed
	c.Retryable = true
	c.Reason = reason
	return c
}

// Resume decides how a stopped run continues. An interrupted or retryable
// run re-runs its current station without a drift check: its own partial
// work isn't outside drift.
func Resume(f source.Factory, s State) (State, Decision) {
	c := cloneState(s)
	switch {
	case c.Status == StatusAwaitingApproval:
		return c, Decision{Kind: AwaitApproval, Station: c.Current}
	case c.Status == StatusDone:
		return c, Decision{Kind: Finished}
	case c.Status == StatusFailed && !c.Retryable:
		return c, Decision{Kind: Failed, Reason: c.Reason}
	case c.Status == StatusInterrupted || c.Status == StatusFailed || c.InStation:
		// The station's own partial work isn't outside drift.
		c.SkipDriftOnce = true
	}
	c.InStation = false
	c.Status = StatusRunning
	c.Retryable = false
	return c, Decision{Kind: RunStation, Station: c.Current}
}

func moveTo(c State, target, summary string) (State, Decision) {
	switch target {
	case source.TargetDone:
		c.Status = StatusDone
		return c, Decision{Kind: Finished, Reason: summary}
	case source.TargetFail:
		return failState(c, summary)
	}
	c.Current = target
	c.Status = StatusRunning
	return c, Decision{Kind: RunStation, Station: target}
}

func failState(c State, reason string) (State, Decision) {
	c.Status = StatusFailed
	c.Retryable = false
	c.Reason = reason
	return c, Decision{Kind: Failed, Reason: reason}
}
