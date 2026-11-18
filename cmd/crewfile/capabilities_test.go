package main

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crewfile/internal/config"
)

func testRegistry() *config.Registry {
	return &config.Registry{
		Capabilities: []string{"read", "structured-output"},
		Harnesses: map[string]config.Harness{
			"claude-code": {Display: "Claude Code", Agents: config.AgentsSpec{Supported: true, Fields: config.AgentFields{Tools: &config.ToolsField{Key: "tools", Style: "csv"}}},
				Capabilities: map[string][]string{"read": {"Read"}, "structured-output": {"StructuredOutput"}}},
			"opencode": {Display: "OpenCode", Agents: config.AgentsSpec{Supported: true, Fields: config.AgentFields{Tools: &config.ToolsField{Key: "tools", Style: "map"}}},
				Capabilities: map[string][]string{"read": {"read", "list"}, "structured-output": {}}},
			"codex": {Display: "Codex CLI", Agents: config.AgentsSpec{Supported: true, Derived: []config.Derived{{Key: "sandbox_mode"}}},
				Capabilities: map[string][]string{"read": {}, "structured-output": {}}},
		},
	}
}

func TestCapabilityTable(t *testing.T) {
	head, rows := capabilityTable(testRegistry(), []string{"claude-code", "opencode", "codex"})
	if got := strings.Join(head, "|"); got != "CAPABILITY|Claude Code|OpenCode|Codex CLI" {
		t.Fatalf("header = %s", got)
	}
	want := [][]string{
		{"read", "Read", "read, list", "(sandbox_mode)"},
		{"structured-output", "StructuredOutput", "— not available", "(sandbox_mode)"},
		{"all", "every tool", "every tool", "every tool"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v", rows)
	}
	for i := range want {
		if strings.Join(rows[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d = %v, want %v", i, rows[i], want[i])
		}
	}
}
