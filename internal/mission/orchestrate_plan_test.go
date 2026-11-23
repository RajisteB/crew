package mission

import (
	"context"
	"slices"
	"testing"
)

const planReply = "thinking\n```plan\n{\"pieces\": [{\"id\": \"a\", \"factory\": \"feature\", \"brief\": \"api\", \"paths\": [\"app/api/**\"]}], \"reason\": \"one slice\"}\n```\n"

func TestHeadlessOrchestrator_Plan(t *testing.T) {
	r := &orchRunner{reply: planReply}
	o := HeadlessOrchestrator{Registry: fakeRegistry(), Runner: r, Harness: "fake", CrewDir: "/crew"}
	sp, err := o.Plan(context.Background(), "/repo", samplePlanRequest())
	if err != nil || len(sp.Pieces) != 1 || sp.Pieces[0].ID != "a" || sp.Reason != "one slice" {
		t.Fatalf("plan = %+v err = %v", sp, err)
	}
	q := r.req
	if q.Agent != OrchestratorName || q.WorkDir != "/repo" || q.Prompt != samplePlanRequest().Prompt() {
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
}

func TestHeadlessOrchestrator_PlanRefusals(t *testing.T) {
	reg := fakeRegistry()
	h := reg.Harnesses["fake"]
	h.Headless.DenyVia = ""
	reg.Harnesses["fake"] = h
	r := &orchRunner{reply: planReply}
	if _, err := (HeadlessOrchestrator{Registry: reg, Runner: r, Harness: "fake"}).Plan(context.Background(), "/repo", samplePlanRequest()); err == nil || r.req.Prompt != "" {
		t.Fatalf("a harness that can't deny must be refused: err = %v", err)
	}
	bad := HeadlessOrchestrator{Registry: fakeRegistry(), Runner: &orchRunner{reply: "no block"}, Harness: "fake"}
	if _, err := bad.Plan(context.Background(), "/repo", samplePlanRequest()); err == nil {
		t.Fatal("parse error must be returned")
	}
}
