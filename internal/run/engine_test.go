package run_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

func reply(route string) string {
	return fmt.Sprintf("work for %s\n```verdict\n{\"route\": %q, \"summary\": \"did %s\", \"notes\": []}\n```\n", route, route, route)
}

// fakeRunner returns scripted replies in order. A reply of "BLOCK" waits for
// the context to end; "ERR" returns a runner error.
type fakeRunner struct {
	mu      sync.Mutex
	replies []string
	reqs    []run.StationRequest
}

func (r *fakeRunner) Run(ctx context.Context, req run.StationRequest) (string, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	if len(r.replies) == 0 {
		r.mu.Unlock()
		return "", errors.New("fake: no scripted reply left")
	}
	next := r.replies[0]
	r.replies = r.replies[1:]
	r.mu.Unlock()
	switch next {
	case "BLOCK":
		<-ctx.Done()
		return "", errStopped
	case "ERR":
		return "", errors.New("fake: claude not logged in")
	}
	return next, nil
}

var errStopped = run.ErrCanceled

func (r *fakeRunner) stations() string {
	var s []string
	for _, q := range r.reqs {
		s = append(s, q.Agent)
	}
	return strings.Join(s, ",")
}

// switchSnap returns "v1" for the first n calls, then "v2".
type switchSnap struct {
	calls, after int
}

func (s *switchSnap) snap(project string, watch []string) (map[string]string, error) {
	s.calls++
	v := "v1"
	if s.after > 0 && s.calls > s.after {
		v = "v2"
	}
	return map[string]string{".": v}, nil
}

func engineFactory() source.Factory {
	f := shipChange()
	f.Harness = "fake"
	return f
}

func newEngine(t *testing.T, f source.Factory, replies ...string) (*run.Engine, *fakeRunner) {
	t.Helper()
	project := t.TempDir()
	store, _, err := run.NewRun(project, f, factorySrc, "Add /health", t0)
	if err != nil {
		t.Fatal(err)
	}
	fr := &fakeRunner{replies: replies}
	caps := []string{"read"}
	return &run.Engine{
		Store: store, Factory: f, Project: project, Runner: fr,
		Agents: map[string]source.Agent{"p": {Name: "p", Capabilities: caps}, "b": {Name: "b", Capabilities: caps}, "r": {Name: "r", Capabilities: caps}},
		Registry: &config.Registry{Harnesses: map[string]config.Harness{"fake": {Capabilities: map[string][]string{"read": {"Read"}}}}},
		Snap:     (&switchSnap{}).snap,
		Out:      &bytes.Buffer{},
	}, fr
}

func drive(t *testing.T, e *run.Engine) run.State {
	t.Helper()
	st, err := e.Drive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestEngine_FullLoop(t *testing.T) {
	e, fr := newEngine(t, engineFactory(), reply("ready"), reply("done"), reply("fix"), reply("done"), reply("pass"))
	st := drive(t, e)
	if st.Status != run.StatusDone || st.Step != 5 || st.RouteCounts["review.fix"] != 1 {
		t.Fatalf("state = %+v", st)
	}
	if fr.stations() != "p,b,r,b,r" {
		t.Fatalf("stations run = %s", fr.stations())
	}
	steps, _ := e.Store.Steps()
	if len(steps) != 5 || steps[4].Verdict.Route != "pass" {
		t.Fatalf("steps = %+v", steps)
	}
	if fr.reqs[0].Allowed[0] != "Read" || fr.reqs[0].WorkDir != e.Project || fr.reqs[0].Harness != "fake" {
		t.Fatalf("request = %+v", fr.reqs[0])
	}
	out := e.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "review --fix--> build (1/*)") || !strings.Contains(out, "done") {
		t.Fatalf("event output:\n%s", out)
	}
}

func TestEngine_LimitStops(t *testing.T) {
	e, _ := newEngine(t, engineFactory(), reply("ready"), reply("replan"), reply("ready"), reply("replan"), reply("ready"), reply("replan"))
	st := drive(t, e)
	if st.Status != run.StatusFailed || !strings.Contains(st.Reason, "build.replan hit its limit (2)") {
		t.Fatalf("state = %+v", st)
	}
}

func TestEngine_StarLoopsUntilPass(t *testing.T) {
	replies := []string{reply("ready"), reply("done")}
	for i := 0; i < 50; i++ {
		replies = append(replies, reply("fix"), reply("done"))
	}
	replies = append(replies, reply("pass"))
	e, _ := newEngine(t, engineFactory(), replies...)
	st := drive(t, e)
	if st.Status != run.StatusDone || st.RouteCounts["review.fix"] != 50 {
		t.Fatalf("state = %+v", st)
	}
}

func TestEngine_DriftRedirect(t *testing.T) {
	e, fr := newEngine(t, engineFactory(), reply("ready"), reply("ready"), reply("done"), reply("pass"))
	// begin(1), before plan(2), after plan(3), before build(4): outside change lands here.
	e.Snap = (&switchSnap{after: 3}).snap
	st := drive(t, e)
	if fr.stations() != "p,p,b,r" || st.Status != run.StatusDone {
		t.Fatalf("stations = %s state = %+v", fr.stations(), st)
	}
	if st.RouteCounts["build.replan"] != 0 {
		t.Fatalf("drift must not count toward limits: %v", st.RouteCounts)
	}
	if out := e.Out.(*bytes.Buffer).String(); !strings.Contains(out, "drift in . before build") {
		t.Fatalf("no drift event:\n%s", out)
	}
}

func TestEngine_NoGuardNeverSnapshots(t *testing.T) {
	f := engineFactory()
	b := f.Stations["build"]
	b.OnDrift = ""
	f.Stations["build"] = b
	e, _ := newEngine(t, f, reply("ready"), reply("done"), reply("pass"))
	e.Snap = func(string, []string) (map[string]string, error) { return nil, errors.New("not a git repository") }
	if st := drive(t, e); st.Status != run.StatusDone {
		t.Fatalf("state = %+v", st)
	}
}

func TestEngine_GuardOutsideGitFailsAtBegin(t *testing.T) {
	e, fr := newEngine(t, engineFactory(), reply("ready"))
	e.Snap = func(string, []string) (map[string]string, error) { return nil, errors.New(". is not a git repository (factory watch:)") }
	_, err := e.Drive(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not a git repository") || len(fr.reqs) != 0 {
		t.Fatalf("err=%v reqs=%d", err, len(fr.reqs))
	}
}

func approvalEngineFactory() source.Factory {
	f := engineFactory()
	r := f.Stations["review"]
	r.Approve = true
	f.Stations["review"] = r
	return f
}

func TestEngine_ApprovalPausesThenApprove(t *testing.T) {
	f := approvalEngineFactory()
	e, _ := newEngine(t, f, reply("ready"), reply("done"), reply("pass"))
	st := drive(t, e)
	if st.Status != run.StatusAwaitingApproval {
		t.Fatalf("state = %+v", st)
	}
	if out := e.Out.(*bytes.Buffer).String(); !strings.Contains(out, "waiting for approval") {
		t.Fatalf("no approval event:\n%s", out)
	}
	approved, _, err := run.Approve(f, st, "")
	if err != nil {
		t.Fatal(err)
	}
	e.Store.SaveState(approved)
	if st = drive(t, e); st.Status != run.StatusDone {
		t.Fatalf("after approve: %+v", st)
	}
}

func TestEngine_InvalidVerdictRetriesOnce(t *testing.T) {
	e, fr := newEngine(t, engineFactory(), "no verdict here", reply("ready"), reply("done"), reply("pass"))
	if st := drive(t, e); st.Status != run.StatusDone {
		t.Fatalf("state = %+v", st)
	}
	if !strings.Contains(fr.reqs[1].Prompt, "## Your previous reply was rejected") {
		t.Fatalf("retry prompt missing the rejection:\n%s", fr.reqs[1].Prompt)
	}
	e2, _ := newEngine(t, engineFactory(), "bad", "still bad")
	if st := drive(t, e2); st.Status != run.StatusFailed || !strings.Contains(st.Reason, "no verdict block") || st.Retryable {
		t.Fatalf("two bad replies must fail the run: %+v", st)
	}
}

func TestEngine_InboxDelivered(t *testing.T) {
	e, fr := newEngine(t, engineFactory(), reply("ready"), reply("done"), reply("pass"))
	e.Store.AppendInbox("use port 8081")
	drive(t, e)
	if !strings.Contains(fr.reqs[0].Prompt, "use port 8081") || strings.Contains(fr.reqs[1].Prompt, "use port 8081") {
		t.Fatal("message must reach exactly the next station")
	}
}

func TestEngine_GotoRequest(t *testing.T) {
	e, fr := newEngine(t, engineFactory(), reply("pass"))
	e.Store.RequestGoto("review")
	if st := drive(t, e); st.Status != run.StatusDone || fr.stations() != "r" {
		t.Fatalf("stations=%s state=%+v", fr.stations(), st)
	}
}

func TestEngine_RunnerErrorFailsResumably(t *testing.T) {
	e, fr := newEngine(t, engineFactory(), "ERR", reply("ready"), reply("done"), reply("pass"))
	e.Store.AppendInbox("hello")
	st := drive(t, e)
	if st.Status != run.StatusFailed || !st.Retryable || !strings.Contains(st.Reason, "not logged in") {
		t.Fatalf("state = %+v", st)
	}
	if st = drive(t, e); st.Status != run.StatusDone || fr.stations() != "p,p,b,r" {
		t.Fatalf("resume: stations=%s state=%+v", fr.stations(), st)
	}
	if !strings.Contains(fr.reqs[1].Prompt, "hello") {
		t.Fatal("a message drained for a failed station must be delivered on retry")
	}
}

func TestEngine_CancelMarksInterrupted(t *testing.T) {
	e, fr := newEngine(t, engineFactory(), reply("ready"), "BLOCK", reply("done"), reply("pass"))
	ss := &switchSnap{}
	e.Snap = ss.snap
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var st run.State
	var err error
	go func() { st, err = e.Drive(ctx); close(done) }()
	for {
		fr.mu.Lock()
		n := len(fr.reqs)
		fr.mu.Unlock()
		if n == 2 {
			break
		}
	}
	cancel()
	<-done
	if !errors.Is(err, context.Canceled) || st.Status != run.StatusInterrupted {
		t.Fatalf("err=%v state=%+v", err, st)
	}
	// The interrupted station's own half-done edits change the tree; resume
	// must re-run build, not treat that as drift and go back to plan.
	ss.after = ss.calls
	if st = drive(t, e); st.Status != run.StatusDone || fr.stations() != "p,b,b,r" {
		t.Fatalf("resume: stations=%s state=%+v", fr.stations(), st)
	}
}

func TestEngine_SecondDriverRefused(t *testing.T) {
	e, _ := newEngine(t, engineFactory(), reply("ready"))
	unlock, err := e.Store.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := e.Drive(context.Background()); err == nil || !strings.Contains(err.Error(), "already being driven") {
		t.Fatalf("got %v", err)
	}
}

// A SIGKILL or power loss mid-station leaves state.json "running" with the
// station marked in progress and the lock's pid dead.
func TestEngine_CrashMidStationResumesWithoutDrift(t *testing.T) {
	e, fr := newEngine(t, engineFactory(), reply("ready"), reply("done"), reply("pass"))
	ss := &switchSnap{}
	e.Snap = ss.snap
	st := run.Begin(e.Factory, "x")
	st, _ = run.AfterStation(e.Factory, st, v("ready")) // plan finished
	st = run.MarkInStation(run.WithSnapshot(st, map[string]string{".": "v1"}))
	orig, _ := e.Store.LoadState()
	st.RunID = orig.RunID
	e.Store.SaveState(st)
	e.Store.Emit(run.Event{Kind: run.EvStart})
	// every snapshot from now on differs: build's half-done edits
	e.Snap = func(string, []string) (map[string]string, error) { return map[string]string{".": "v2"}, nil }
	fr.replies = fr.replies[1:]
	if st = drive(t, e); st.Status != run.StatusDone || fr.stations() != "b,r" {
		t.Fatalf("stations=%s state=%+v", fr.stations(), st)
	}
}
