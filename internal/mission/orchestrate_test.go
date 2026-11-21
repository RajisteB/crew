package mission

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/run"
)

type orchRunner struct {
	reply string
	err   error
	req   run.StationRequest
}

func (o *orchRunner) Run(_ context.Context, req run.StationRequest) (run.Reply, error) {
	o.req = req
	return run.Reply{Text: o.reply}, o.err
}

const decisionReply = "thinking\n```decision\n{\"action\": \"retry\", \"reason\": \"flaky\"}\n```\n"

func TestHeadlessOrchestrator_ReadOnlyToolsAndDenyRules(t *testing.T) {
	r := &orchRunner{reply: decisionReply}
	o := HeadlessOrchestrator{Registry: fakeRegistry(), Runner: r, Harness: "fake", CrewDir: "/crew"}
	d, err := o.Decide(context.Background(), "/repo", Summary{MissionID: "m-1", Request: "build it"})
	if err != nil || d.Action != "retry" {
		t.Fatalf("decision = %+v err = %v", d, err)
	}
	q := r.req
	if q.Agent != OrchestratorName || q.Harness != "fake" || q.WorkDir != "/repo" || q.Timeout != 10*time.Minute {
		t.Fatalf("request = %+v", q)
	}
	for _, bad := range []string{"Bash", "Edit", "Write"} {
		if slices.Contains(q.Allowed, bad) {
			t.Fatalf("allowed has %s: %v", bad, q.Allowed)
		}
	}
	if !slices.Contains(q.Allowed, "Read") || !slices.Contains(q.Allowed, "Grep") {
		t.Fatalf("allowed = %v", q.Allowed)
	}
	if !slices.Contains(q.Deny, "Read(/crew/secret)") || !slices.Contains(q.Deny, "Bash(git push:*)") {
		t.Fatalf("deny = %v", q.Deny)
	}
	if !strings.Contains(q.Prompt, "build it") {
		t.Fatalf("prompt = %q", q.Prompt)
	}
}

func TestHeadlessOrchestrator_RefusesWithoutDenyRules(t *testing.T) {
	reg := fakeRegistry()
	h := reg.Harnesses["fake"]
	h.Headless.DenyVia = ""
	reg.Harnesses["fake"] = h
	for name, harness := range map[string]string{"no deny": "fake", "unknown": "nope"} {
		r := &orchRunner{reply: decisionReply}
		_, err := HeadlessOrchestrator{Registry: reg, Runner: r, Harness: harness}.Decide(context.Background(), "/repo", Summary{})
		if err == nil || r.req.Prompt != "" {
			t.Fatalf("%s: err = %v, runner called = %v", name, err, r.req.Prompt != "")
		}
	}
}

func TestHeadlessOrchestrator_ErrorsAreReturned(t *testing.T) {
	boom := HeadlessOrchestrator{Registry: fakeRegistry(), Runner: &orchRunner{err: errors.New("boom")}, Harness: "fake"}
	if _, err := boom.Decide(context.Background(), "/repo", Summary{}); err == nil {
		t.Fatal("runner error must be returned")
	}
	bad := HeadlessOrchestrator{Registry: fakeRegistry(), Runner: &orchRunner{reply: "no block"}, Harness: "fake"}
	if _, err := bad.Decide(context.Background(), "/repo", Summary{}); err == nil {
		t.Fatal("parse error must be returned")
	}
}
