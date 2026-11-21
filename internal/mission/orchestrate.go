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
	h, ok := o.Registry.Harnesses[o.Harness]
	if !ok {
		return OrchDecision{}, fmt.Errorf("orchestrator: unknown harness %q", o.Harness)
	}
	if !run.CanDeny(h) {
		return OrchDecision{}, fmt.Errorf("orchestrator: harness %q can't apply deny rules, so the orchestrator won't run on it", o.Harness)
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = defaultOrchestratorTimeout
	}
	reader := source.Agent{Name: OrchestratorName, Capabilities: []string{"read", "search"}}
	reply, err := o.Runner.Run(ctx, run.StationRequest{
		Harness: o.Harness, Agent: OrchestratorName, Prompt: s.Prompt(), WorkDir: repoRoot,
		Allowed: run.AllowedTools(o.Registry, o.Harness, reader),
		Deny:    run.DenyRules(o.Registry, o.Harness, o.CrewDir, true),
		Timeout: timeout,
	})
	if err != nil {
		return OrchDecision{}, fmt.Errorf("orchestrator: %w", err)
	}
	return ParseDecision(reply.Text)
}
