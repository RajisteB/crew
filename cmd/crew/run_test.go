package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var crewBin string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "crew-bin-")
	if err != nil {
		panic(err)
	}
	crewBin = filepath.Join(tmp, "crew")
	if out, err := exec.Command("go", "build", "-o", crewBin, ".").CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build crew: %v\n%s", err, out))
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

type cliEnv struct {
	t                     *testing.T
	crewDir, project, rep string
}

func fakeHarness() string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "internal", "run", "testdata", "fakeharness.sh")
}

// newCLIEnv scaffolds a crew dir whose claude-code headless command is the
// fake harness, plus a git project to run in.
func newCLIEnv(t *testing.T, replies ...string) *cliEnv {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	e := &cliEnv{t: t, crewDir: scaffoldDir(t), project: t.TempDir(), rep: t.TempDir()}
	editFile(t, filepath.Join(e.crewDir, "harnesses.yaml"), "command: [claude, -p,", "command: ["+fakeHarness()+", -p,")
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", e.project}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	for i, r := range replies {
		os.WriteFile(filepath.Join(e.rep, fmt.Sprint(i+1)), []byte(r), 0o644)
	}
	return e
}

func verdict(route string) string {
	return fmt.Sprintf("did %s\n```verdict\n{\"route\": %q, \"summary\": \"did %s\"}\n```\n", route, route, route)
}

func (e *cliEnv) crew(args ...string) (string, int) {
	e.t.Helper()
	cmd := exec.Command(crewBin, args...)
	cmd.Dir = e.project
	cmd.Env = append(os.Environ(), "CREW_HOME="+e.crewDir, "FAKE_REPLY_DIR="+e.rep, "FAKE_MODE=")
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		e.t.Fatal(err)
	}
	return string(out), code
}

func runID(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "run ") {
			return strings.Fields(line)[1]
		}
	}
	t.Fatalf("no run id in output:\n%s", out)
	return ""
}

func editFile(t *testing.T, path, old, new string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), old) {
		t.Fatalf("%s does not contain %q (%v)", path, old, err)
	}
	os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644)
}

func (e *cliEnv) noApproval() {
	editFile(e.t, filepath.Join(e.crewDir, "factories", "ship-change.md"), "    approve: true", "    approve: false")
}

func TestCLI_RunToDone(t *testing.T) {
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"))
	e.noApproval()
	out, code := e.crew("run", "ship-change", "--input", "Add /health")
	if code != 0 || !strings.Contains(out, "done: did pass") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	status, _ := e.crew("run", "status", runID(t, out))
	if !strings.Contains(status, "status done") || !strings.Contains(status, "cursor ") {
		t.Fatalf("status:\n%s", status)
	}
	list, _ := e.crew("run", "status")
	if !strings.Contains(list, runID(t, out)) {
		t.Fatalf("status list:\n%s", list)
	}
}

func TestCLI_InputFromFile(t *testing.T) {
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"))
	e.noApproval()
	f := filepath.Join(t.TempDir(), "in.md")
	os.WriteFile(f, []byte("Add a /health endpoint from a file"), 0o644)
	if out, code := e.crew("run", "ship-change", "--input", "@"+f); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	b, _ := os.ReadFile(filepath.Join(e.rep, "prompt-1"))
	if !strings.Contains(string(b), "from a file") {
		t.Fatalf("prompt didn't get the file input:\n%s", b)
	}
}

func TestCLI_ApproveFlow(t *testing.T) {
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"))
	out, code := e.crew("run", "ship-change", "--input", "x")
	if code != 0 || !strings.Contains(out, "waiting for approval") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	out, code = e.crew("run", "approve", runID(t, out))
	if code != 0 || !strings.Contains(out, "done") {
		t.Fatalf("approve exit %d:\n%s", code, out)
	}
}

func TestCLI_SayReachesNextStation(t *testing.T) {
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("fix"), verdict("done"), verdict("pass"))
	out, _ := e.crew("run", "ship-change", "--input", "x")
	id := runID(t, out)
	if out, code := e.crew("run", "say", id, "use", "port", "8081"); code != 0 {
		t.Fatalf("say exit %d:\n%s", code, out)
	}
	if out, code := e.crew("run", "approve", id); code != 0 || !strings.Contains(out, "waiting for approval") {
		t.Fatalf("approve exit %d:\n%s", code, out)
	}
	b, _ := os.ReadFile(filepath.Join(e.rep, "prompt-4"))
	if !strings.Contains(string(b), "use port 8081") {
		t.Fatalf("message missing from the build prompt after approval:\n%s", b)
	}
}

func TestCLI_GotoStoppedRun(t *testing.T) {
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"), verdict("pass"))
	out, _ := e.crew("run", "ship-change", "--input", "x")
	id := runID(t, out)
	if out, code := e.crew("run", "goto", id, "review"); code != 0 || !strings.Contains(out, "review") {
		t.Fatalf("goto exit %d:\n%s", code, out)
	}
	if out, code := e.crew("run", "resume", id); code != 0 || !strings.Contains(out, "→ review") {
		t.Fatalf("resume exit %d:\n%s", code, out)
	}
}

func TestCLI_DetachThenStatus(t *testing.T) {
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"))
	e.noApproval()
	out, code := e.crew("run", "ship-change", "--input", "x", "--detach")
	if code != 0 {
		t.Fatalf("detach exit %d:\n%s", code, out)
	}
	id := runID(t, out)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := e.crew("run", "status", id, "--since", "0")
		if strings.Contains(status, "status done") {
			if !strings.Contains(status, "done: did pass") {
				t.Fatalf("events missing:\n%s", status)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	log, _ := os.ReadFile(filepath.Join(e.project, ".crew", "runs", id, "run.log"))
	t.Fatalf("detached run didn't finish; run.log:\n%s", log)
}

func TestCLI_VerifyFailureBlocksRun(t *testing.T) {
	e := newCLIEnv(t)
	editFile(t, filepath.Join(e.crewDir, "factories", "ship-change.md"), "replan: {to: plan, max: 2}", "replan: plan")
	out, code := e.crew("run", "ship-change", "--input", "x")
	if code != 1 || !strings.Contains(out, "loop with no limit") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

func TestCLI_RunErrors(t *testing.T) {
	e := newCLIEnv(t)
	if out, code := e.crew("run", "ship-change"); code != 2 || !strings.Contains(out, "--input is required") {
		t.Fatalf("missing input: exit %d\n%s", code, out)
	}
	if out, code := e.crew("run", "nope", "--input", "x"); code != 1 || !strings.Contains(out, `no factory "nope"`) {
		t.Fatalf("unknown factory: exit %d\n%s", code, out)
	}
	if out, code := e.crew("run", "status", "nope-123"); code != 1 || !strings.Contains(out, "no run") {
		t.Fatalf("unknown run: exit %d\n%s", code, out)
	}
}
