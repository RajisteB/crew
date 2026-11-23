package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/run"
)

// fastPoll makes the wait poll quickly for the test.
func fastPoll(t *testing.T) {
	t.Helper()
	old := statusPoll
	statusPoll = 20 * time.Millisecond
	t.Cleanup(func() { statusPoll = old })
}

// status runs `crew run status` in this process and reports the output,
// the exit code and how long it took.
func (e *cliEnv) status(args ...string) (string, int, time.Duration) {
	e.t.Helper()
	var out, errb bytes.Buffer
	start := time.Now()
	code := runCmd(append([]string{"status", "--project", e.project}, args...), strings.NewReader(""), &out, &errb)
	return out.String() + errb.String(), code, time.Since(start)
}

// finishedRun returns the id and last cursor of a run that is done.
func finishedRun(t *testing.T) (*cliEnv, string, string) {
	t.Helper()
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"))
	e.noApproval()
	out, code := e.crew("run", "ship-change", "--input", "x")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	id := runID(t, out)
	st, _ := e.crew("run", "status", id, "--since", "0")
	lines := strings.Split(strings.TrimSpace(st), "\n")
	return e, id, strings.TrimPrefix(lines[len(lines)-1], "cursor ")
}

func TestCLI_StatusWaitReturnsAtOnceWhenThereIsNews(t *testing.T) {
	fastPoll(t)
	e, id, _ := finishedRun(t)
	out, code, took := e.status(id, "--since", "0", "--wait", "30s")
	if code != 0 || took > 5*time.Second || !strings.Contains(out, "done: did pass") {
		t.Fatalf("exit %d after %v:\n%s", code, took, out)
	}
}

func TestCLI_StatusWaitReturnsWhenTheRunHasStopped(t *testing.T) {
	fastPoll(t)
	e, id, last := finishedRun(t)
	out, code, took := e.status(id, "--since", last, "--wait", "30s")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || took > 5*time.Second || lines[len(lines)-1] != "cursor "+last {
		t.Fatalf("exit %d after %v:\n%s", code, took, out)
	}
}

// runningRun saves a finished run's state as running; nothing holds its lock.
func runningRun(t *testing.T) (*cliEnv, run.Store, string) {
	t.Helper()
	e, id, last := finishedRun(t)
	store, err := run.OpenRun(e.project, id)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Status = run.StatusRunning
	if err := store.SaveState(st); err != nil {
		t.Fatal(err)
	}
	return e, store, last
}

func TestCLI_StatusWaitGivesUpAfterTheTime(t *testing.T) {
	fastPoll(t)
	e, store, last := runningRun(t)
	_, code, took := e.status(filepath.Base(store.Dir), "--since", last, "--wait", "150ms")
	if code != 0 || took < 150*time.Millisecond || took > 5*time.Second {
		t.Fatalf("exit %d after %v", code, took)
	}
}

func TestCLI_StatusWaitSeesAnEventThatArrivesLater(t *testing.T) {
	fastPoll(t)
	e, store, last := runningRun(t)
	go func() {
		time.Sleep(100 * time.Millisecond)
		store.Emit(run.Event{Kind: run.EvNote, Msg: "a late note"})
	}()
	out, code, took := e.status(filepath.Base(store.Dir), "--since", last, "--wait", "10s")
	if code != 0 || took > 5*time.Second || !strings.Contains(out, "a late note") {
		t.Fatalf("exit %d after %v:\n%s", code, took, out)
	}
}

func TestCLI_StatusWaitUsageErrors(t *testing.T) {
	e := newCLIEnv(t)
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"--wait", "5s"}, "--wait needs a run id"},
		{[]string{"someid", "--wait", "11m"}, "--wait takes a time up to 10m, like 90s"},
		{[]string{"someid", "--wait", "-1s"}, "--wait takes a time up to 10m, like 90s"},
	} {
		out, code, _ := e.status(c.args...)
		if code != 2 || !strings.Contains(out, c.msg) {
			t.Errorf("%v: exit %d:\n%s", c.args, code, out)
		}
	}
}
