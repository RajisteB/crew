package build_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/build"
)

// askLine is the template's ask rules for the claude-code orchestrator.
const askLine = `      ask: ["Bash(crew run approve:*)", "Bash(crew run goto:*)"]` + "\n"

func orchestratorSettingsFile(t *testing.T, dir, out string) []byte {
	t.Helper()
	files, _ := planFiles(t, dir, build.Opts{Out: out})
	sf, ok := findFile(files, filepath.Join("claude-code", "agents", "orchestrator.settings.json"))
	if !ok {
		t.Fatal("no orchestrator.settings.json planned")
	}
	return sf.Content
}

func TestOrchestratorSettings_AsksBeforeApproveAndGoto(t *testing.T) {
	dir, out := setup(t)
	var s struct {
		Permissions struct {
			Ask []string `json:"ask"`
		} `json:"permissions"`
	}
	b := orchestratorSettingsFile(t, dir, out)
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("settings are not JSON: %v\n%s", err, b)
	}
	want := []string{"Bash(crew run approve:*)", "Bash(crew run goto:*)"}
	if !slices.Equal(s.Permissions.Ask, want) {
		t.Errorf("ask = %v, want exactly %v", s.Permissions.Ask, want)
	}
}

func TestOrchestratorSettings_NoAskKeyWithoutAskRules(t *testing.T) {
	dir, out := oldKeyProject(t)
	b := orchestratorSettingsFile(t, dir, out)
	if strings.Contains(string(b), `"ask"`) {
		t.Errorf("a harness with no ask rules got an ask key:\n%s", b)
	}
}
