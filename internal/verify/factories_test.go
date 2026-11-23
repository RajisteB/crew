package verify_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/verify"
)

const shipChangeFile = "factories/ship-change.md"

func TestFactories_ScaffoldIsClean(t *testing.T) {
	p, rep := verify.Run(fresh(t))
	if !rep.OK(false) {
		t.Fatalf("scaffold should verify clean: %+v", rep.Issues)
	}
	if len(p.Factories) != 1 || p.Factories[0].Name != "ship-change" {
		t.Fatalf("Factories = %+v", p.Factories)
	}
}

func TestFactories_UnknownAgent(t *testing.T) {
	dir := fresh(t)
	edit(t, filepath.Join(dir, shipChangeFile), "agent: epic-planner", "agent: nobody")
	expectError(t, dir, `station plan: unknown agent "nobody"`)
}

func TestFactories_GraphErrorReported(t *testing.T) {
	dir := fresh(t)
	edit(t, filepath.Join(dir, shipChangeFile), "replan: {to: plan, max: 2}", "replan: plan")
	expectError(t, dir, shipChangeFile+": loop with no limit")
}

func TestFactories_NoHarness(t *testing.T) {
	dir := fresh(t)
	edit(t, filepath.Join(dir, shipChangeFile), "harness: claude-code\n", "")
	expectError(t, dir, "station plan has no harness")
}

func TestFactories_HarnessNotEnabled(t *testing.T) {
	dir := fresh(t)
	edit(t, filepath.Join(dir, shipChangeFile), "harness: claude-code", "harness: codex")
	expectError(t, dir, "station plan runs on codex, which isn't enabled")
}

func TestFactories_AgentNotCompiledForHarness(t *testing.T) {
	dir := fresh(t)
	edit(t, filepath.Join(dir, "crew.yaml"), "  claude-code:\n    enabled: true\n    agents: [\"*\"]", "  claude-code:\n    enabled: true\n    agents: [\"*\"]\n    exclude: [code-explorer]")
	expectError(t, dir, "station review: agent code-explorer isn't compiled for claude-code")
}

func TestFactories_ReservedName(t *testing.T) {
	dir := fresh(t)
	src := filepath.Join(dir, shipChangeFile)
	b, _ := os.ReadFile(src)
	os.Remove(src)
	os.WriteFile(filepath.Join(dir, "factories", "status.md"), []byte(strings.Replace(string(b), "name: ship-change", "name: status", 1)), 0o644)
	expectError(t, dir, `factory name "status" is reserved`)
}

func TestFactories_AgentMayBeNamedLikeAnOldForeman(t *testing.T) {
	dir := fresh(t)
	b, _ := os.ReadFile(filepath.Join(dir, "agents", "code-explorer.md"))
	os.WriteFile(filepath.Join(dir, "agents", "foreman-ship-change.md"),
		[]byte(strings.Replace(string(b), "name: code-explorer", "name: foreman-ship-change", 1)), 0o644)
	if _, rep := verify.Run(dir); !rep.OK(false) {
		t.Fatalf("an agent named foreman-ship-change must verify: %+v", rep.Issues)
	}
}

func TestFactories_StarIsInfoNotWarning(t *testing.T) {
	_, rep := verify.Run(fresh(t))
	if !rep.OK(false) {
		t.Fatalf("not OK: %+v", rep.Issues)
	}
	for _, i := range rep.Issues {
		if i.Level == verify.Info && strings.Contains(i.Msg, `("*")`) {
			return
		}
	}
	t.Fatalf("expected an info note for max: \"*\": %+v", rep.Issues)
}
