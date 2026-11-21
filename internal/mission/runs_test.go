package mission

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
	"github.com/crewfile-dev/crew/internal/verify"
)

const localFactory = `---
name: tiny
description: d
start: plan
harness: fake
stations:
  plan:
    agent: p
    routes:
      ready: build
  build:
    agent: b
    routes:
      done: done
---
Briefing.
`

// scriptRunner is a fake run.Runner: scripted replies in order; "BLOCK" waits
// for the context to end.
type scriptRunner struct {
	mu      sync.Mutex
	replies []string
	reqs    []run.StationRequest
}

func (r *scriptRunner) Run(ctx context.Context, req run.StationRequest) (run.Reply, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	if len(r.replies) == 0 {
		r.mu.Unlock()
		return run.Reply{}, errors.New("fake: no scripted reply left")
	}
	next := r.replies[0]
	r.replies = r.replies[1:]
	r.mu.Unlock()
	if next == "BLOCK" {
		<-ctx.Done()
		return run.Reply{}, run.ErrCanceled
	}
	return run.Reply{Text: next}, nil
}

func (r *scriptRunner) requests() []run.StationRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]run.StationRequest(nil), r.reqs...)
}

func verdictReply(route string) string {
	return "work\n```verdict\n{\"route\": \"" + route + "\", \"summary\": \"s\", \"notes\": []}\n```\n"
}

func fakeRegistry() *config.Registry {
	return &config.Registry{
		Capabilities: []string{"read", "search", "shell", "edit"},
		Harnesses: map[string]config.Harness{"fake": {
			Capabilities: map[string][]string{"read": {"Read"}, "search": {"Grep"}, "shell": {"Bash"}, "edit": {"Edit", "Write"}},
			Headless:     &config.HeadlessSpec{DenyVia: "settings-file"},
			Deny:         config.DenySpec{All: []string{"Read({crew_dir}/secret)"}, Stations: []string{"Bash(git push:*)"}},
		}},
	}
}

type localEnv struct {
	l        *LocalRuns
	repo     string
	worktree string
	runner   *scriptRunner
	out      *bytes.Buffer
}

func newLocal(t *testing.T, replies ...string) localEnv {
	t.Helper()
	repo, worktree, crewDir := t.TempDir(), t.TempDir(), t.TempDir()
	path := filepath.Join(crewDir, "tiny.md")
	if err := os.WriteFile(path, []byte(localFactory), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := source.ParseFactory(path)
	if err != nil {
		t.Fatal(err)
	}
	caps := []string{"read"}
	p := &verify.Project{
		Registry:  fakeRegistry(),
		Factories: []source.Factory{f},
		Agents:    []source.Agent{{Name: "p", Capabilities: caps}, {Name: "b", Capabilities: caps}},
	}
	runner := &scriptRunner{replies: replies}
	out := &bytes.Buffer{}
	l := &LocalRuns{RepoRoot: repo, CrewDir: crewDir, Project: p, Runner: runner, Out: &syncBuf{buf: out}}
	t.Cleanup(func() { l.stopAll() })
	return localEnv{l: l, repo: repo, worktree: worktree, runner: runner, out: out}
}

// syncBuf makes a bytes.Buffer safe for the engine goroutines under -race.
type syncBuf struct {
	mu  sync.Mutex
	buf *bytes.Buffer
}

func (s *syncBuf) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(b)
}

func (e localEnv) start(t *testing.T) string {
	t.Helper()
	id, err := e.l.Start(Piece{ID: "api", Factory: "tiny"}, "do it", e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestLocalRuns_RunFolderInMainRepoWorkInWorktree(t *testing.T) {
	e := newLocal(t, verdictReply("ready"), verdictReply("done"))
	id := e.start(t)
	if _, err := os.Stat(filepath.Join(e.repo, ".crew", "runs", id, "state.json")); err != nil {
		t.Fatalf("run folder not in the main repo: %v", err)
	}
	if err := e.l.Drive(context.Background(), id, e.worktree); err != nil {
		t.Fatal(err)
	}
	e.l.Wait()
	reqs := e.runner.requests()
	if len(reqs) != 2 || reqs[0].WorkDir != e.worktree {
		t.Fatalf("requests = %+v", reqs)
	}
	obs, err := e.l.Observe(id)
	if err != nil || obs.State.Status != run.StatusDone || obs.Live {
		t.Fatalf("obs = %+v err = %v", obs, err)
	}
	st, fac, last, dir, err := e.l.Summary(id)
	if err != nil || st.RunID != id || fac.Name != "tiny" || last.Station != "build" {
		t.Fatalf("summary = %+v %+v %+v %v", st, fac.Name, last, err)
	}
	if !filepath.IsAbs(dir) || !strings.HasPrefix(dir, e.repo) || filepath.Base(dir) != "steps" {
		t.Fatalf("steps dir = %s", dir)
	}
}

func TestLocalRuns_DriveTwiceIsOneEngine(t *testing.T) {
	e := newLocal(t, "BLOCK", "BLOCK")
	id := e.start(t)
	for i := 0; i < 2; i++ {
		if err := e.l.Drive(context.Background(), id, e.worktree); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "the station to start", func() bool { return len(e.runner.requests()) == 1 })
	obs, _ := e.l.Observe(id)
	if !obs.Live {
		t.Fatal("a driven run must be live")
	}
	if err := e.l.Stop(id); err != nil {
		t.Fatal(err)
	}
	if n := len(e.runner.requests()); n != 1 {
		t.Fatalf("stations started = %d, want 1", n)
	}
}

func TestLocalRuns_StepGotoResetsCountsAndDeliversNote(t *testing.T) {
	e := newLocal(t, verdictReply("ready"), verdictReply("done"), verdictReply("ready"), verdictReply("done"))
	id := e.start(t)
	if err := e.l.Drive(context.Background(), id, e.worktree); err != nil {
		t.Fatal(err)
	}
	e.l.Wait()
	err := e.l.Step(context.Background(), id, e.worktree, RunStep{Kind: "goto", Station: "plan", Note: "use the new schema"})
	if err != nil {
		t.Fatal(err)
	}
	e.l.Wait()
	reqs := e.runner.requests()
	if len(reqs) != 4 || reqs[2].Agent != "p" || !strings.Contains(reqs[2].Prompt, "use the new schema") {
		t.Fatalf("requests after goto = %d, %+v", len(reqs), reqs)
	}
	obs, _ := e.l.Observe(id)
	if obs.State.Status != run.StatusDone || obs.State.RouteCounts["plan.ready"] != 1 {
		t.Fatalf("state = %+v (counts must have been reset, not 2)", obs.State)
	}
}

func TestLocalRuns_StepRefusesDrivenRunAndUnknownKind(t *testing.T) {
	e := newLocal(t, "BLOCK")
	id := e.start(t)
	if err := e.l.Drive(context.Background(), id, e.worktree); err != nil {
		t.Fatal(err)
	}
	if err := e.l.Step(context.Background(), id, e.worktree, RunStep{Kind: "retry"}); err == nil {
		t.Fatal("Step on a driven run must fail")
	}
	if err := e.l.Stop(id); err != nil {
		t.Fatal(err)
	}
	if err := e.l.Step(context.Background(), id, e.worktree, RunStep{Kind: "nope"}); err == nil {
		t.Fatal("an unknown kind must fail")
	}
}

func TestLocalRuns_StopInterruptsRun(t *testing.T) {
	e := newLocal(t, "BLOCK")
	id := e.start(t)
	// The tick's context ends right after Drive returns; the run must go on.
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.l.Drive(ctx, id, e.worktree); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitFor(t, "the station to start", func() bool { return len(e.runner.requests()) == 1 })
	if obs, _ := e.l.Observe(id); !obs.Live {
		t.Fatal("the run ended with the tick's context")
	}
	if err := e.l.Stop(id); err != nil {
		t.Fatal(err)
	}
	obs, _ := e.l.Observe(id)
	if obs.Live || obs.State.Status == run.StatusDone {
		t.Fatalf("obs after stop = %+v", obs)
	}
	if err := e.l.Stop("not-driven"); err != nil {
		t.Fatalf("stopping an undriven run = %v, want nil", err)
	}
}

// verify.Run gives factories a path relative to the crew directory; Start
// must read the file from there, not from the process's working directory.
func TestLocalRuns_StartReadsFactoryRelativeToCrewDir(t *testing.T) {
	e := newLocal(t)
	f := e.l.Project.Factories[0]
	f.Path = filepath.Base(f.Path)
	e.l.Project.Factories = []source.Factory{f}
	if _, err := e.l.Start(Piece{ID: "api", Factory: "tiny"}, "do it", e.worktree); err != nil {
		t.Fatalf("Start with a crew-relative factory path: %v", err)
	}
}
