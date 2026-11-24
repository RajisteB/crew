package mission

import (
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
