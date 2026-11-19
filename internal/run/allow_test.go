package run_test

import (
	"reflect"
	"testing"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

func registry() *config.Registry {
	return &config.Registry{
		Capabilities: []string{"read", "search", "shell", "web"},
		Harnesses: map[string]config.Harness{"claude-code": {Capabilities: map[string][]string{
			"read": {"Read"}, "search": {"Grep", "Glob"}, "shell": {"Bash"}, "web": {"WebFetch", "WebSearch"},
		}}, "codex": {Capabilities: map[string][]string{"read": {}, "shell": {}}}},
	}
}

func TestAllowedTools_ClaudeCode(t *testing.T) {
	got := run.AllowedTools(registry(), "claude-code", source.Agent{Capabilities: []string{"read", "shell", "read"}})
	if !reflect.DeepEqual(got, []string{"Bash", "Read"}) {
		t.Fatalf("got %v", got)
	}
}

func TestAllowedTools_All(t *testing.T) {
	got := run.AllowedTools(registry(), "claude-code", source.Agent{Capabilities: []string{"all"}})
	if !reflect.DeepEqual(got, []string{"Bash", "Glob", "Grep", "Read", "WebFetch", "WebSearch"}) {
		t.Fatalf("got %v", got)
	}
}

func TestAllowedTools_UnmappedCapabilityIsEmpty(t *testing.T) {
	if got := run.AllowedTools(registry(), "codex", source.Agent{Capabilities: []string{"read", "shell"}}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
