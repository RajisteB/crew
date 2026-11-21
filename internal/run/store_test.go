package run_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

var factorySrc = []byte(`---
name: ship-change
description: d
start: plan
harness: claude-code
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
`)

var t0 = time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC)

func newRun(t *testing.T) (string, run.Store, run.State) {
	t.Helper()
	project := t.TempDir()
	f := shipChange()
	s, st, err := run.NewRun(project, f, factorySrc, "Add /health", t0)
	if err != nil {
		t.Fatal(err)
	}
	return project, s, st
}

func TestStore_RoundTrip(t *testing.T) {
	project, s, st := newRun(t)
	if !regexp.MustCompile(`^ship-change-20261118-093000-[0-9a-f]{4}$`).MatchString(st.RunID) {
		t.Fatalf("run id = %q", st.RunID)
	}
	if s.Dir != filepath.Join(project, ".crew", "runs", st.RunID) {
		t.Fatalf("dir = %s", s.Dir)
	}
	if in, _ := s.Input(); in != "Add /health" {
		t.Fatalf("input = %q", in)
	}
	st2 := run.WithSnapshot(st, map[string]string{".": "abc"})
	if err := s.SaveState(st2); err != nil {
		t.Fatal(err)
	}
	opened, err := run.OpenRun(project, st.RunID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := opened.LoadState()
	if err != nil || got.Snapshot["."] != "abc" || got.Current != "plan" {
		t.Fatalf("LoadState = %+v, %v", got, err)
	}
	rec := run.StepRecord{N: 1, Station: "plan", Verdict: run.Verdict{Route: "ready", Summary: "ok"}}
	if err := s.WriteStep(rec, "PROMPT", "OUTPUT\n"); err != nil {
		t.Fatal(err)
	}
	steps, err := s.Steps()
	if err != nil || len(steps) != 1 || steps[0].Verdict.Route != "ready" || steps[0].Dir != filepath.Join(s.Dir, "steps", "1-plan") {
		t.Fatalf("Steps = %+v, %v", steps, err)
	}
	if b, _ := os.ReadFile(filepath.Join(steps[0].Dir, "output.md")); string(b) != "OUTPUT\n" {
		t.Fatalf("output.md = %q", b)
	}
	runs, err := run.ListRuns(project)
	if err != nil || len(runs) != 1 || runs[0].RunID != st.RunID {
		t.Fatalf("ListRuns = %+v, %v", runs, err)
	}
	if _, err := run.OpenRun(project, "nope"); err == nil || !strings.Contains(err.Error(), "no run") {
		t.Fatalf("OpenRun unknown = %v", err)
	}
}

func TestStore_StepsSortNumerically(t *testing.T) {
	_, s, _ := newRun(t)
	for i := 1; i <= 11; i++ {
		s.WriteStep(run.StepRecord{N: i, Station: "plan", Verdict: run.Verdict{Route: "ready"}}, "", "")
	}
	steps, _ := s.Steps()
	if len(steps) != 11 || steps[9].N != 10 || steps[10].N != 11 {
		t.Fatalf("order: %+v", steps)
	}
}

func TestStore_FactorySnapshotIsFrozen(t *testing.T) {
	_, s, _ := newRun(t)
	f, err := s.Factory()
	if err != nil {
		t.Fatal(err)
	}
	if f.Stations["build"].Routes["done"].To != source.TargetDone || len(f.Stations) != 2 {
		t.Fatalf("frozen factory = %+v", f)
	}
}

func TestStore_InboxDrain(t *testing.T) {
	_, s, _ := newRun(t)
	if msgs, err := s.DrainInbox(); err != nil || len(msgs) != 0 {
		t.Fatalf("empty drain = %v, %v", msgs, err)
	}
	s.AppendInbox("use port 8081")
	s.AppendInbox("line one\nline two")
	msgs, err := s.DrainInbox()
	if err != nil || len(msgs) != 2 || msgs[1] != "line one\nline two" {
		t.Fatalf("drain = %q, %v", msgs, err)
	}
	if msgs, _ := s.DrainInbox(); len(msgs) != 0 {
		t.Fatalf("second drain should be empty: %q", msgs)
	}
}

func TestStore_Goto(t *testing.T) {
	_, s, _ := newRun(t)
	if g, _ := s.TakeGoto(); g != "" {
		t.Fatalf("no goto expected, got %q", g)
	}
	s.RequestGoto("build")
	if g, _ := s.TakeGoto(); g != "build" {
		t.Fatalf("TakeGoto = %q", g)
	}
	if g, _ := s.TakeGoto(); g != "" {
		t.Fatalf("goto must be consumed, got %q", g)
	}
}

func TestStore_LockExclusive(t *testing.T) {
	_, s, _ := newRun(t)
	unlock, err := s.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if pid, ok := s.LockedBy(); !ok || pid != os.Getpid() {
		t.Fatalf("LockedBy = %d %v", pid, ok)
	}
	if _, err := s.Lock(); err == nil || !strings.Contains(err.Error(), "already being driven (pid "+strconv.Itoa(os.Getpid())) {
		t.Fatalf("second lock = %v", err)
	}
	unlock()
	if _, ok := s.LockedBy(); ok {
		t.Fatal("still locked after unlock")
	}
	// A stale lock from a dead process is taken over.
	cmd := exec.Command("true")
	cmd.Run()
	os.WriteFile(filepath.Join(s.Dir, "lock"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	unlock, err = s.Lock()
	if err != nil {
		t.Fatalf("stale lock not taken over: %v", err)
	}
	unlock()
}

// A lock file left behind by a process that is gone, whose pid now belongs to
// an unrelated live process, is not a held lock: only a held flock is.
func TestStore_LockFileNamingALivePidIsNotHeld(t *testing.T) {
	_, s, _ := newRun(t)
	if err := os.WriteFile(filepath.Join(s.Dir, "lock"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	if pid, ok := s.LockedBy(); ok {
		t.Fatalf("LockedBy = %d, true for a lock nobody holds", pid)
	}
	unlock, err := s.Lock()
	if err != nil {
		t.Fatalf("a leftover lock file must not block the run: %v", err)
	}
	defer unlock()
	if pid, ok := s.LockedBy(); !ok || pid != os.Getpid() {
		t.Fatalf("LockedBy after Lock = %d %v", pid, ok)
	}
	if _, err := s.Lock(); err == nil || !strings.Contains(err.Error(), "already being driven (pid "+strconv.Itoa(os.Getpid())+")") {
		t.Fatalf("second lock = %v", err)
	}
}

func TestStore_EventsSince(t *testing.T) {
	_, s, _ := newRun(t)
	e1, _ := s.Emit(run.Event{Kind: "start"})
	e2, _ := s.Emit(run.Event{Kind: "note", Station: "plan", Msg: "assumed UTC"})
	if e1.Seq != 1 || e2.Seq != 2 || e2.Time.IsZero() {
		t.Fatalf("seqs %d %d time %v", e1.Seq, e2.Seq, e2.Time)
	}
	evs, err := s.EventsSince(1)
	if err != nil || len(evs) != 1 || evs[0].Msg != "assumed UTC" {
		t.Fatalf("EventsSince(1) = %+v, %v", evs, err)
	}
	all, _ := s.EventsSince(0)
	if len(all) != 2 {
		t.Fatalf("all = %+v", all)
	}
}

func TestStore_GitignoreCreated(t *testing.T) {
	project, _, _ := newRun(t)
	b, err := os.ReadFile(filepath.Join(project, ".crew", ".gitignore"))
	if err != nil || string(b) != "*\n" {
		t.Fatalf(".crew/.gitignore = %q, %v", b, err)
	}
}

func TestStore_KillLeftoverChild(t *testing.T) {
	_, s, _ := newRun(t)
	if killed, err := s.KillLeftoverChild(); err != nil || killed {
		t.Fatalf("no child recorded: killed=%v err=%v", killed, err)
	}
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if err := s.RecordChild(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	killed, err := s.KillLeftoverChild()
	if err != nil || !killed {
		t.Fatalf("killed=%v err=%v", killed, err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("orphaned station process still running")
	}
	if killed, _ := s.KillLeftoverChild(); killed {
		t.Fatal("record must be cleared")
	}
}
