package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/gitx"
	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/notify"
)

const (
	e2eFactory = "quick-change"
	e2eWait    = 20 * time.Second
)

const e2eFactoryFile = `---
name: quick-change
description: Plans and builds one change.
start: plan
harness: claude-code
stations:
  plan:
    agent: epic-planner
    routes:
      ready: build
  build:
    agent: ticket-worker
    routes:
      done: done
---
Plan the change, then build it.
`

// e2eRig is a mission set up for real: a temp crew directory, a temp repo with
// a bare origin, the fake gh and the fake station harness. Only the engine's
// ticks are driven by the test.
type e2eRig struct {
	t             *testing.T
	crewDir, repo string
	ghDir         string
	store         mission.Store
	engine        *mission.Engine
	runs          *mission.LocalRuns
	id            string
}

func testdataPath(name string) string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "testdata", name)
}

func runIn(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// isolate keeps the test away from the user's home, crew directory and git config.
func isolate(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"git", "sh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CREW_HOME", filepath.Join(t.TempDir(), "crew-home"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	for _, who := range []string{"AUTHOR", "COMMITTER"} {
		t.Setenv("GIT_"+who+"_NAME", "crew test")
		t.Setenv("GIT_"+who+"_EMAIL", "crew@example.com")
	}
}

func newE2ERig(t *testing.T) *e2eRig {
	t.Helper()
	isolate(t)
	r := &e2eRig{t: t, crewDir: scaffoldDir(t), repo: t.TempDir(), ghDir: t.TempDir()}
	r.setupCrewDir()
	r.setupRepo()
	r.setupSeams()
	return r
}

func (r *e2eRig) setupCrewDir() {
	harnesses := filepath.Join(r.crewDir, "harnesses.yaml")
	editFile(r.t, harnesses, `command: [claude, -p, --agent, "{agent}",`, `command: [`+testdataPath("fakeharness-commit.sh")+`, "{agent}",`)
	editFile(r.t, harnesses, "output: claude-json", "output: text")
	files := map[string]string{
		filepath.Join("factories", e2eFactory+".md"): e2eFactoryFile,
		filepath.Join("domains", "design.md"):        "---\nname: design\ndescription: UI work.\nfactories: [" + e2eFactory + "]\n---\nBody.\n",
	}
	if err := os.MkdirAll(filepath.Join(r.crewDir, "domains"), 0o755); err != nil {
		r.t.Fatal(err)
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(r.crewDir, name), []byte(text), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
}

func (r *e2eRig) setupRepo() {
	origin := filepath.Join(r.t.TempDir(), "origin.git")
	runIn(r.t, r.repo, "git", "init", "-q", "-b", "main", r.repo)
	runIn(r.t, r.repo, "git", "init", "-q", "--bare", "-b", "main", origin)
	runIn(r.t, r.repo, "git", "commit", "-q", "--allow-empty", "-m", "init")
	runIn(r.t, r.repo, "git", "remote", "add", "origin", origin)
	runIn(r.t, r.repo, "git", "push", "-q", "origin", "main")
}

// setupSeams points the gh client at fakegh.sh and keeps desktop
// notifications off: newNotifier is replaced by notify.Nop.
func (r *e2eRig) setupSeams() {
	oldGH, oldNotifier := ghClient, newNotifier
	ghClient = func(dir string) gh.Client {
		return gh.Client{Dir: dir, Exec: func(ctx context.Context, d string, in []byte, _ string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, "sh", append([]string{testdataPath("fakegh.sh")}, args...)...)
			cmd.Dir, cmd.Stdin = d, bytes.NewReader(in)
			cmd.Env = append(os.Environ(), "FAKE_GH_DIR="+r.ghDir)
			return cmd.Output()
		}}
	}
	newNotifier = func() notify.Notifier { return notify.Nop{} }
	r.t.Cleanup(func() { ghClient, newNotifier = oldGH, oldNotifier })
}

func (r *e2eRig) cmd(stdin string, args ...string) string {
	r.t.Helper()
	var out bytes.Buffer
	full := append(append([]string{}, args...), "--dir", r.crewDir, "--project", r.repo)
	if err := missionCmd(full, strings.NewReader(stdin), &out); err != nil {
		r.t.Fatalf("crew mission %v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func (r *e2eRig) confirmRepo() {
	r.t.Helper()
	fakeTTY(r.t, "acme/app\n")
	if err := repoCmd([]string{"confirm", "--dir", r.crewDir, "--project", r.repo}, &bytes.Buffer{}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *e2eRig) launch() {
	r.t.Helper()
	oldStart := startDetached
	startDetached = func(string, string, string, string) (int, error) { return 4242, nil }
	r.t.Cleanup(func() { startDetached = oldStart })
	plan := planFile("Build the thing.", e2ePiece("P1"), e2ePiece("P2", "P1"))
	r.id = missionIDRe.FindString(r.cmd(string(plan), "plan"))
	if r.id == "" {
		r.t.Fatal("plan printed no mission id")
	}
	r.cmd("", "start", r.id)
	r.cmd("", "go", r.id)
	root, err := gitx.Repo{Dir: r.repo}.Root()
	if err != nil {
		r.t.Fatal(err)
	}
	if r.store, err = mission.Open(root, r.id); err != nil {
		r.t.Fatal(err)
	}
	r.engine, r.runs, err = newMissionEngine(r.store, r.crewDir, root, io.Discard)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() {
		st, err := r.store.LoadState()
		if err != nil {
			return
		}
		for _, p := range st.Pieces {
			if p.RunID != "" {
				_ = r.runs.Stop(p.RunID)
			}
		}
		r.runs.Wait()
	})
}

func e2ePiece(id string, waits ...string) mission.Piece {
	return mission.Piece{ID: id, Domain: "design", Factory: e2eFactory, Brief: "Do " + id + ".", WaitsFor: waits}
}

// tick runs one engine round under the mission lock and returns the state.
func (r *e2eRig) tick() mission.State {
	r.t.Helper()
	unlock, err := r.store.Lock()
	if err != nil {
		r.t.Fatal(err)
	}
	defer unlock()
	st, err := r.engine.Tick(context.Background())
	if err != nil {
		r.t.Fatalf("tick: %v", err)
	}
	return st
}

// tickUntil ticks until ok holds, with a deadline.
func (r *e2eRig) tickUntil(what string, ok func(mission.State) bool) mission.State {
	r.t.Helper()
	deadline := time.Now().Add(e2eWait)
	for {
		st := r.tick()
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s; state: %+v\nevents: %v", what, st.Pieces, r.events())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *e2eRig) events() []string {
	evs, _ := r.store.EventsSince(0)
	var out []string
	for _, e := range evs {
		out = append(out, e.Kind+" "+e.Piece+" "+e.Msg)
	}
	return out
}

func (r *e2eRig) ghFile(name string) string {
	b, _ := os.ReadFile(filepath.Join(r.ghDir, name))
	return strings.TrimSpace(string(b))
}

func piece(t *testing.T, st mission.State, id string) mission.PieceState {
	t.Helper()
	p, ok := st.Piece(id)
	if !ok {
		t.Fatalf("no piece %s", id)
	}
	return p
}

func TestMission_EndToEnd_TwoPiecesOnePRWaits(t *testing.T) {
	r := newE2ERig(t)
	r.confirmRepo()
	r.launch()

	st := r.tickUntil("P1's pull request and its merge wait", func(s mission.State) bool {
		p := piece(t, s, "P1")
		return p.PR != 0 && p.Wait != nil
	})
	p1 := piece(t, st, "P1")
	if _, err := os.Stat(p1.Worktree); err != nil {
		t.Fatalf("P1 worktree missing: %v", err)
	}
	if want := "crew/" + r.id + "/P1"; p1.Branch != want {
		t.Fatalf("branch = %q, want %q", p1.Branch, want)
	}
	runIn(t, r.repo, "git", "rev-parse", "--verify", "refs/heads/"+p1.Branch)
	if n := strings.Count(r.ghFile("calls"), "pr create"); n != 1 {
		t.Fatalf("pr create ran %d times, want 1; calls:\n%s", n, r.ghFile("calls"))
	}
	if body := r.ghFile("pr-body"); !strings.Contains(body, "P1") || !strings.Contains(body, r.id) {
		t.Fatalf("PR body (sent on stdin) does not name the piece and mission:\n%s", body)
	}
	if got := piece(t, st, "P2").Status; got != mission.PieceQueued {
		t.Fatalf("P2 is %s, want queued while P1's PR is open", got)
	}

	status := r.cmd("", "status", r.id)
	if i, j := strings.Index(status, "Waiting on you"), strings.Index(status, "P1  PR open"); i < 0 || !strings.Contains(status[i:], "merge") || !strings.Contains(status, "pull/7") || (j >= 0 && j < i) {
		t.Fatalf("status should lead with Waiting on you and the PR:\n%s", status)
	}

	if err := os.WriteFile(filepath.Join(r.ghDir, "pr-state"), []byte("MERGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.tickUntil("P1 complete and P2 running", func(s mission.State) bool {
		return piece(t, s, "P1").Status == mission.PieceComplete && piece(t, s, "P2").Status == mission.PieceRunning
	})
	if _, err := os.Stat(p1.Worktree); err == nil {
		t.Fatalf("worktree %s still exists", p1.Worktree)
	}
}
