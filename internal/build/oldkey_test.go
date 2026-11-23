package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/build"
)

const (
	beforeCutoverFixture = "../scaffold/testdata/harnesses-before-cutover.yaml"
	sixRuleAllow         = `["Bash(crew mission:*)", "Bash(crew domain list:*)", "Bash(crew factory list:*)", "Read", "Grep", "Glob"]`
	tenRuleAllow         = `["Bash(crew mission:*)", "Bash(crew domain list:*)", "Bash(crew factory list:*)", "Read", "Grep", "Glob", "Bash(crew run status:*)", "Bash(crew run say:*)", "Bash(crew run resume:*)", "Bash(crew factory diagram:*)"]`
)

// oldKeyProject is a template project whose harnesses.yaml is the fixture from
// before the cutover.
func oldKeyProject(t *testing.T) (dir, out string) {
	t.Helper()
	dir, out = setup(t)
	b, err := os.ReadFile(beforeCutoverFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harnesses.yaml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, out
}

func orchestratorBytes(t *testing.T, dir, out string) (agent, settings string) {
	t.Helper()
	files, _ := planFiles(t, dir, build.Opts{Out: out})
	for _, f := range files {
		switch {
		case strings.HasSuffix(f.Path, filepath.Join("claude-code", "agents", "orchestrator.md")) && f.Link == "":
			agent = string(f.Content)
		case strings.HasSuffix(f.Path, "orchestrator.settings.json"):
			settings = strings.ReplaceAll(string(f.Content), dir, "<crew dir>") // deny rules name the crew dir
		}
	}
	if agent == "" || settings == "" {
		t.Fatalf("orchestrator outputs missing (agent %d bytes, settings %d bytes)", len(agent), len(settings))
	}
	return agent, settings
}

func TestOrchestratorOutputs_SameFromOldAndNewKey(t *testing.T) {
	newDir, newOut := setup(t)
	wantAgent, wantSettings := orchestratorBytes(t, newDir, newOut)

	oldDir, oldOut := oldKeyProject(t)
	edit(t, filepath.Join(oldDir, "harnesses.yaml"), sixRuleAllow, tenRuleAllow)
	gotAgent, gotSettings := orchestratorBytes(t, oldDir, oldOut)
	if gotAgent != wantAgent {
		t.Errorf("orchestrator agent differs:\n%s\nwant:\n%s", gotAgent, wantAgent)
	}
	if gotSettings != wantSettings {
		t.Errorf("orchestrator.settings.json differs:\n%s\nwant:\n%s", gotSettings, wantSettings)
	}
}

func TestPlan_NotesTheOldForemanKey(t *testing.T) {
	const note = "harnesses.yaml still says foreman: for Claude Code; crew mission setup renames it to orchestrator:"
	oldDir, oldOut := oldKeyProject(t)
	_, notes := planFiles(t, oldDir, build.Opts{Out: oldOut})
	if !hasNote(notes, "claude-code", note) {
		t.Errorf("missing %q in %+v", note, notes)
	}
	newDir, newOut := setup(t)
	_, notes = planFiles(t, newDir, build.Opts{Out: newOut})
	for _, n := range notes {
		if strings.Contains(n.Msg, "foreman:") {
			t.Errorf("template project got %+v", n)
		}
	}
}
