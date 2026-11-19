package run_test

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func promptIn() run.PromptInput {
	f := shipChange()
	f.Body = "Briefing: ship one change.\n"
	return run.PromptInput{
		Factory: f, Station: "review", Input: "Add a /health endpoint",
		Steps: []run.StepRecord{
			{N: 1, Station: "plan", Dir: "/r/steps/1-plan", Verdict: run.Verdict{Route: "ready", Summary: "planned 2 files"}},
			{N: 2, Station: "build", Dir: "/r/steps/2-build", Verdict: run.Verdict{Route: "done", Summary: "SECRET-OUTPUT-NOT-INLINED"}},
		},
	}
}

func TestBuildPrompt_Sections(t *testing.T) {
	p := run.BuildPrompt(promptIn())
	order := []string{"# Factory: ship-change", "Briefing: ship one change.", "## Your station: review", "## Run input", "Add a /health endpoint", "## Earlier steps", "## How to finish"}
	last := -1
	for _, h := range order {
		i := strings.Index(p, h)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order in:\n%s", h, p)
		}
		last = i
	}
	for _, r := range []string{"`fix`", "`pass`", "`redesign`", "`fail`"} {
		if !strings.Contains(p, r) {
			t.Fatalf("route %s not listed:\n%s", r, p)
		}
	}
	if !strings.Contains(p, "```verdict") || !strings.Contains(p, "Do not ask the user questions") {
		t.Fatalf("contract missing:\n%s", p)
	}
}

func TestBuildPrompt_OmitsEmptyInbox(t *testing.T) {
	if strings.Contains(run.BuildPrompt(promptIn()), "## Messages from the user") {
		t.Fatal("empty inbox should be omitted")
	}
	in := promptIn()
	in.Inbox = []string{"use port 8081"}
	if p := run.BuildPrompt(in); !strings.Contains(p, "## Messages from the user") || !strings.Contains(p, "use port 8081") {
		t.Fatalf("inbox missing:\n%s", p)
	}
}

func TestBuildPrompt_StepsArePaths(t *testing.T) {
	p := run.BuildPrompt(promptIn())
	if !strings.Contains(p, "1. plan → ready — planned 2 files (output: /r/steps/1-plan/output.md)") {
		t.Fatalf("step line missing:\n%s", p)
	}
	if !strings.Contains(p, "/r/steps/2-build/output.md") {
		t.Fatalf("second step path missing:\n%s", p)
	}
}

func TestBuildPrompt_FirstStepHasNoEarlierSteps(t *testing.T) {
	in := promptIn()
	in.Steps = nil
	if p := run.BuildPrompt(in); !strings.Contains(p, "You are the first station") {
		t.Fatalf("got:\n%s", p)
	}
}

func TestBuildPrompt_Retry(t *testing.T) {
	in := promptIn()
	in.RetryError = "no verdict block"
	p := run.BuildPrompt(in)
	if !strings.Contains(p, "## Your previous reply was rejected") || !strings.Contains(p, "no verdict block") {
		t.Fatalf("retry section missing:\n%s", p)
	}
}
