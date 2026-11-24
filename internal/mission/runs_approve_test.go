package mission

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

func TestApplyStep_ApproveRecordsTheApproval(t *testing.T) {
	fac := source.Factory{Name: "tiny", Start: "plan", Stations: map[string]source.Station{
		"plan": {Approve: true, Routes: map[string]source.Route{"ready": {To: source.TargetDone}}},
	}}
	project := t.TempDir()
	store, st, err := run.NewRun(project, fac, []byte("x"), "in", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st.Status = run.StatusAwaitingApproval
	st.Pending = &run.Pending{Station: "plan", Route: "ready", Verdict: run.Verdict{Route: "ready", Summary: "all set"}}
	next, err := applyStep(fac, st, store, RunStep{Kind: "approve", Note: "go ahead"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != run.StatusDone || next.Reason != "all set" {
		t.Fatalf("next = %+v", next)
	}
	evs, err := store.EventsSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != run.EvApproved || evs[0].Station != "plan" || evs[0].Msg != "go ahead" {
		t.Fatalf("events = %+v", evs)
	}
}

func TestApplyStep_ApproveWithFailingEventWriteStillApproves(t *testing.T) {
	fac := source.Factory{Name: "tiny", Start: "plan", Stations: map[string]source.Station{
		"plan": {Approve: true, Routes: map[string]source.Route{"ready": {To: source.TargetDone}}},
	}}
	store, st, err := run.NewRun(t.TempDir(), fac, []byte("x"), "in", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// A folder where the events file belongs makes every event write fail.
	if err := os.Mkdir(filepath.Join(store.Dir, "events.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	st.Status = run.StatusAwaitingApproval
	st.Pending = &run.Pending{Station: "plan", Route: "ready", Verdict: run.Verdict{Route: "ready", Summary: "all set"}}
	next, err := applyStep(fac, st, store, RunStep{Kind: "approve", Note: "go ahead"})
	if err != nil {
		t.Fatalf("the approval must not fail on an event write: %v", err)
	}
	if next.Status != run.StatusDone {
		t.Fatalf("next = %+v", next)
	}
}

func TestApplyStep_ApproveGotoSendsTheRunToTheStation(t *testing.T) {
	fac := source.Factory{Name: "two", Start: "plan", Stations: map[string]source.Station{
		"plan":  {Approve: true, Routes: map[string]source.Route{"ready": {To: "build"}}},
		"build": {Routes: map[string]source.Route{"ok": {To: source.TargetDone}}},
	}}
	store, st, err := run.NewRun(t.TempDir(), fac, []byte("x"), "in", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st.Status = run.StatusAwaitingApproval
	st.Pending = &run.Pending{Station: "plan", Route: "ready", Verdict: run.Verdict{Route: "ready", Summary: "all set"}}
	next, err := applyStep(fac, st, store, RunStep{Kind: "approve", Station: "build", Note: "fix the typo"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != run.StatusRunning || next.Current != "build" {
		t.Fatalf("next = %+v", next)
	}
	inbox, err := store.DrainInbox()
	if err != nil || len(inbox) != 1 || inbox[0] != "fix the typo" {
		t.Fatalf("inbox = %v, err = %v", inbox, err)
	}
}
