package build_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/build"
)

func orchestratorAllow(t *testing.T) []string {
	t.Helper()
	dir, out := setup(t)
	files, _ := planFiles(t, dir, build.Opts{Out: out})
	sf, ok := findFile(files, filepath.Join("claude-code", "agents", "orchestrator.settings.json"))
	if !ok {
		t.Fatal("no orchestrator.settings.json planned")
	}
	var s settingsFile
	if err := json.Unmarshal(sf.Content, &s); err != nil {
		t.Fatalf("settings are not JSON: %v\n%s", err, sf.Content)
	}
	return s.Permissions.Allow
}

func TestOrchestratorSettings_AllowListForPlainRuns(t *testing.T) {
	want := []string{
		"Bash(crew mission:*)", "Bash(crew domain list:*)", "Bash(crew factory list:*)",
		"Read", "Grep", "Glob",
		"Bash(crew run status:*)", "Bash(crew run say:*)", "Bash(crew run resume:*)", "Bash(crew factory diagram:*)",
	}
	if got := orchestratorAllow(t); !slices.Equal(got, want) {
		t.Errorf("allow = %v, want exactly %v", got, want)
	}
}

func TestOrchestratorSettings_NeverPreApprovesStartGotoOrApprove(t *testing.T) {
	for _, rule := range orchestratorAllow(t) {
		if rule == "Bash(crew run:*)" || strings.HasPrefix(rule, "Bash(crew run approve") || strings.HasPrefix(rule, "Bash(crew run goto") {
			t.Errorf("allow list pre-approves %q", rule)
		}
	}
}
