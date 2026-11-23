package mission

import (
	"context"
	"fmt"
	"time"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

// OrchestratorName is the agent the headless orchestrator runs as.
const OrchestratorName = "orchestrator"

const defaultOrchestratorTimeout = 10 * time.Minute

// HeadlessOrchestrator asks a headless harness session what to do with a
// stuck piece. The session can read and search; nothing it replies is
// executed here.
type HeadlessOrchestrator struct {
	Registry *config.Registry
	Runner   run.Runner
	Harness  string
	CrewDir  string
	Timeout  time.Duration // 10 minutes when zero
}

// Decide runs the orchestrator on the summary and parses its decision.
func (o HeadlessOrchestrator) Decide(ctx context.Context, repoRoot string, s Summary) (OrchDecision, error) {
	text, err := o.ask(ctx, repoRoot, s.Prompt())
	if err != nil {
		return OrchDecision{}, err
	}
	return ParseDecision(text)
}

// Plan runs the orchestrator on the request and parses its plan. It runs on
// the same agent, tools, deny rules and timeout as Decide.
func (o HeadlessOrchestrator) Plan(ctx context.Context, repoRoot string, r PlanRequest) (SubPlan, error) {
	text, err := o.ask(ctx, repoRoot, r.Prompt())
	if err != nil {
		return SubPlan{}, err
	}
	return ParseSubPlan(text)
}

// ask runs the prompt as a read-only headless session and returns the reply.
func (o HeadlessOrchestrator) ask(ctx context.Context, repoRoot, prompt string) (string, error) {
	h, ok := o.Registry.Harnesses[o.Harness]
	if !ok {
		return "", fmt.Errorf("orchestrator: unknown harness %q", o.Harness)
	}
	if !run.CanDeny(h) {
		return "", fmt.Errorf("orchestrator: harness %q can't apply deny rules, so the orchestrator won't run on it", o.Harness)
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = defaultOrchestratorTimeout
	}
	reader := source.Agent{Name: OrchestratorName, Capabilities: []string{"read", "search"}}
	reply, err := o.Runner.Run(ctx, run.StationRequest{
		Harness: o.Harness, Agent: OrchestratorName, Prompt: prompt, WorkDir: repoRoot,
		Allowed: run.AllowedTools(o.Registry, o.Harness, reader),
		Deny:    run.DenyRules(o.Registry, o.Harness, o.CrewDir, true),
		Timeout: timeout,
	})
	if err != nil {
		return "", fmt.Errorf("orchestrator: %w", err)
	}
	return reply.Text, nil
}
