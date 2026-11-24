package build_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/build"
)

func TestOrchestratorArgv_SplitsTheTemplateAndFillsTheSettingsPath(t *testing.T) {
	dir, out := setup(t)
	p, _ := verifyRun(t, dir)
	o := build.Opts{Out: out}
	argv, ok := build.OrchestratorArgv(p, "claude-code", o)
	want := []string{"claude", "--agent", "orchestrator", "--settings", build.OrchestratorSettingsPath(p, "claude-code", o)}
	if !ok || !slices.Equal(argv, want) {
		t.Fatalf("argv = %q, %v; want %q", argv, ok, want)
	}
	if !strings.HasSuffix(argv[4], "/crew-build/claude-code/agents/orchestrator.settings.json") || !strings.HasPrefix(argv[4], out) {
		t.Errorf("settings path = %q", argv[4])
	}
}

func TestOrchestratorArgv_PathWithSpacesStaysOneUnquotedArgument(t *testing.T) {
	dir, _ := setup(t)
	p, _ := verifyRun(t, dir)
	argv, ok := build.OrchestratorArgv(p, "claude-code", build.Opts{Out: "/tmp/with space/o'k"})
	if !ok || len(argv) != 5 || argv[4] != "/tmp/with space/o'k/crew-build/claude-code/agents/orchestrator.settings.json" {
		t.Fatalf("argv = %q, %v", argv, ok)
	}
}

func TestOrchestratorArgv_FalseWithoutAnOrchestrator(t *testing.T) {
	dir, out := setup(t)
	p, _ := verifyRun(t, dir)
	if argv, ok := build.OrchestratorArgv(p, "opencode", build.Opts{Out: out}); ok || argv != nil {
		t.Errorf("opencode: argv = %q, %v", argv, ok)
	}
	if _, ok := build.OrchestratorArgv(p, "no-such-harness", build.Opts{Out: out}); ok {
		t.Error("unknown harness got an orchestrator")
	}
}
