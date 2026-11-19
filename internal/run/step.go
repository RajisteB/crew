package run

import (
	"context"
	"fmt"
	"strings"
)

// runStep runs the current station once (plus one retry for an unusable
// reply) and applies its verdict.
func (e *Engine) runStep(ctx context.Context, st State) (State, Decision, error) {
	name := st.Current
	station := e.Factory.Stations[name]
	inbox, err := e.Store.DrainInbox()
	if err != nil {
		return st, Decision{}, err
	}
	for _, m := range inbox {
		e.emit(Event{Kind: EvMessage, Station: name, Msg: m})
	}
	req, in, err := e.request(st, inbox)
	if err != nil {
		return st, Decision{}, err
	}
	n := st.Step + 1
	e.emit(Event{Kind: EvStepStart, Station: name, Count: n})

	req.OnStart = func(pid int) {
		if err := e.Store.RecordChild(pid); err != nil {
			e.emit(Event{Kind: EvNote, Station: name, Msg: "couldn't record the station's pid: " + err.Error()})
		}
	}
	output, v, prompt, denials, err := e.attempt(ctx, req, in, station.RouteNames())
	e.Store.ClearChild()
	for _, d := range denials {
		e.emit(Event{Kind: EvNote, Station: name, Msg: "permission denied: " + d + " (not in this agent's capabilities)"})
	}
	if err != nil {
		e.requeue(inbox)
		return e.stopped(ctx, st, err)
	}
	if err := e.Store.WriteStep(StepRecord{N: n, Station: name, Verdict: v}, prompt, output); err != nil {
		return st, Decision{}, err
	}
	e.emit(Event{Kind: EvStepEnd, Station: name, Route: v.Route, Msg: v.Summary, Count: n})
	for _, note := range v.Notes {
		e.emit(Event{Kind: EvNote, Station: name, Msg: note})
	}
	next, d := AfterStation(e.Factory, st, v)
	if v.Route != "fail" {
		r := station.Routes[v.Route]
		e.emit(Event{Kind: EvRoute, Station: name, Route: v.Route, Msg: r.To,
			Count: next.RouteCounts[name+"."+v.Route], Max: r.Max.String()})
	}
	if e.guarded() {
		snap, err := e.Snap(e.Project, e.Factory.WatchRoots())
		if err != nil {
			return next, Decision{}, err
		}
		next = WithSnapshot(next, snap)
	}
	return next, d, e.Store.SaveState(next)
}

// attempt runs the station, retrying once with the rejection appended when
// the reply has no usable verdict. A second bad reply is a station failure.
func (e *Engine) attempt(ctx context.Context, req StationRequest, in PromptInput, routes []string) (string, Verdict, string, []string, error) {
	reply, err := e.Runner.Run(ctx, req)
	denials := reply.Denials
	if err != nil {
		return "", Verdict{}, req.Prompt, denials, err
	}
	output, v, perr := ParseReply(reply.Text, routes)
	if perr == nil {
		return output, v, req.Prompt, denials, nil
	}
	e.emit(Event{Kind: EvRetry, Station: in.Station, Msg: perr.Error()})
	in.RetryError = perr.Error()
	req.Prompt = BuildPrompt(in)
	reply, err = e.Runner.Run(ctx, req)
	denials = append(denials, reply.Denials...)
	if err != nil {
		return "", Verdict{}, req.Prompt, denials, err
	}
	output, v, perr = ParseReply(reply.Text, routes)
	if perr != nil {
		return reply.Text, Verdict{Route: "fail", Summary: fmt.Sprintf("%s gave no usable verdict twice: %v", in.Station, perr)}, req.Prompt, denials, nil
	}
	return output, v, req.Prompt, denials, nil
}

func (e *Engine) request(st State, inbox []string) (StationRequest, PromptInput, error) {
	name := st.Current
	station := e.Factory.Stations[name]
	steps, err := e.Store.Steps()
	if err != nil {
		return StationRequest{}, PromptInput{}, err
	}
	input, err := e.Store.Input()
	if err != nil {
		return StationRequest{}, PromptInput{}, err
	}
	timeout, err := e.Factory.TimeoutFor(name)
	if err != nil {
		return StationRequest{}, PromptInput{}, err
	}
	agent, ok := e.Agents[station.Agent]
	if !ok {
		return StationRequest{}, PromptInput{}, fmt.Errorf("station %s: unknown agent %q", name, station.Agent)
	}
	harness := e.Factory.HarnessFor(name)
	in := PromptInput{Factory: e.Factory, Station: name, Input: input, Steps: steps, Inbox: inbox}
	return StationRequest{
		Harness: harness, Agent: station.Agent, Prompt: BuildPrompt(in), WorkDir: e.Project,
		Allowed: AllowedTools(e.Registry, harness, agent), Timeout: timeout,
	}, in, nil
}

// stopped turns a runner error into state: interrupted (the run was
// stopped) or failed-but-retryable (the station couldn't run).
func (e *Engine) stopped(ctx context.Context, st State, err error) (State, Decision, error) {
	if IsCanceled(err) || ctx.Err() != nil {
		st = Interrupt(st, "interrupted while running "+st.Current)
		e.emit(Event{Kind: EvInterrupted, Station: st.Current, Msg: st.Reason})
		if serr := e.Store.SaveState(st); serr != nil {
			return st, Decision{}, serr
		}
		return st, Decision{}, context.Canceled
	}
	st = FailRetryable(st, err.Error())
	return st, Decision{Kind: Failed, Reason: st.Reason}, e.Store.SaveState(st)
}

// requeue puts messages back when the station that should have read them
// never finished, so the retry still gets them.
func (e *Engine) requeue(msgs []string) {
	for _, m := range msgs {
		if err := e.Store.AppendInbox(m); err != nil {
			e.emit(Event{Kind: EvNote, Msg: "lost a message: " + strings.TrimSpace(m)})
		}
	}
}
