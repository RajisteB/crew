package main

import (
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func TestCLI_ApproveRecordsEventAndGivesDoneItsReason(t *testing.T) {
	good := "ok\n```verdict\n{\"route\": \"pass\", \"summary\": \"looks good\"}\n```\n"
	e := newCLIEnv(t, verdict("ready"), verdict("done"), good)
	out, _ := e.crew("run", "ship-change", "--input", "x")
	id := runID(t, out)
	if out, code := e.crew("run", "approve", id, "--note", "ship it"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	store, err := run.OpenRun(e.project, id)
	if err != nil {
		t.Fatal(err)
	}
	evs, err := store.EventsSince(0)
	if err != nil {
		t.Fatal(err)
	}
	var approved, done []run.Event
	for _, ev := range evs {
		switch ev.Kind {
		case run.EvApproved:
			approved = append(approved, ev)
		case run.EvDone:
			done = append(done, ev)
		}
	}
	if len(approved) != 1 || approved[0].Msg != "ship it" {
		t.Fatalf("approved events = %+v", approved)
	}
	if len(done) != 1 || done[0].Msg != "looks good" {
		t.Fatalf("done events = %+v", done)
	}
}
