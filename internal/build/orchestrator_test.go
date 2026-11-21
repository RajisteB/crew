package build_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/build"
)

type settingsFile struct {
	Permissions struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
	} `json:"permissions"`
}

func findFile(files []build.File, suffix string) (build.File, bool) {
	for _, f := range files {
		if strings.HasSuffix(f.Path, suffix) {
			return f, true
		}
	}
	return build.File{}, false
}

func TestOrchestratorOutputs_WritesAgentAndSettings(t *testing.T) {
	dir, out := setup(t)
	files, _ := planFiles(t, dir, build.Opts{Out: out})

	agent, ok := findFile(files, filepath.Join("claude-code", "agents", "orchestrator.md"))
	if !ok || agent.Link != "" || !strings.Contains(string(agent.Content), "name: orchestrator") {
		t.Fatalf("no built orchestrator agent: %+v", agent)
	}
	if !strings.Contains(string(agent.Content), "tools: Read, Grep, Glob, Bash") {
		t.Errorf("orchestrator tools are not read, search, shell:\n%s", agent.Content)
	}
	sf, ok := findFile(files, filepath.Join("claude-code", "agents", "orchestrator.settings.json"))
	if !ok {
		t.Fatal("no orchestrator.settings.json planned")
	}
	if !strings.HasPrefix(sf.Path, filepath.Join(out, "crew-build")) {
		t.Errorf("settings must live under the build directory, got %s", sf.Path)
	}
	var s settingsFile
	if err := json.Unmarshal(sf.Content, &s); err != nil {
		t.Fatalf("settings are not JSON: %v\n%s", err, sf.Content)
	}
	if !slices.Contains(s.Permissions.Allow, "Bash(crew:*)") {
		t.Errorf("allow lacks Bash(crew:*): %v", s.Permissions.Allow)
	}
	for _, d := range []string{"Bash(crew automerge:*)", "Bash(crew repo confirm:*)", "Bash(crew release approve:*)"} {
		if !slices.Contains(s.Permissions.Deny, d) {
			t.Errorf("deny lacks %s: %v", d, s.Permissions.Deny)
		}
	}
	if slices.Contains(s.Permissions.Deny, "Bash(crew mission:*)") {
		t.Error("crew mission must not be denied")
	}
}

func TestOrchestratorOutputs_ProjectScopeKeepsSettingsInBuildDir(t *testing.T) {
	dir, out := setup(t)
	proj := filepath.Join(t.TempDir(), "proj")
	files, _ := planFiles(t, dir, build.Opts{Scope: "project", ProjectDir: proj, Out: out})
	sf, ok := findFile(files, "orchestrator.settings.json")
	if !ok || !strings.HasPrefix(sf.Path, filepath.Join(out, "crew-build")) {
		t.Fatalf("settings must stay in the build dir in project scope: %+v", sf)
	}
	agent, ok := findFile(files, filepath.Join("agents", "orchestrator.md"))
	if !ok || agent.Link != "" || strings.Contains(agent.Path, "crew-build") {
		t.Fatalf("project scope must copy the agent into the project: %+v", agent)
	}
}

func TestOrchestratorOutputs_SkippedWithoutDenySupport(t *testing.T) {
	dir, out := setup(t)
	p, _ := verifyRun(t, dir)
	h := p.Registry.Harnesses["claude-code"]
	hl := *h.Headless
	hl.DenyVia = ""
	h.Headless = &hl
	p.Registry.Harnesses["claude-code"] = h
	files, notes, err := build.Plan(p, build.Opts{Out: out})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Name == "orchestrator" || strings.HasSuffix(f.Path, "orchestrator.settings.json") {
			t.Fatalf("orchestrator must not be built without deny support: %s", f.Path)
		}
	}
	for _, n := range notes {
		if n.Harness == "claude-code" && strings.Contains(n.Msg, "can't apply deny rules, so the orchestrator isn't built") {
			return
		}
	}
	t.Fatalf("expected a deny-support note, got %+v", notes)
}

func TestOrchestratorOutputs_NotBuiltForHarnessWithoutLaunch(t *testing.T) {
	dir, out := setup(t)
	files, _ := planFiles(t, dir, build.Opts{Out: out})
	for _, f := range files {
		if f.Name == "orchestrator" && f.Harnesses[0] != "claude-code" {
			t.Fatalf("only claude-code has orchestrator_launch, got %v", f.Harnesses)
		}
	}
}

func TestOrchestratorLaunch_FillsNameAndQuotedSettingsPath(t *testing.T) {
	dir, out := setup(t)
	p, _ := verifyRun(t, dir)
	line, ok := build.OrchestratorLaunch(p, "claude-code", build.Opts{Out: out})
	if !ok || !strings.HasPrefix(line, "claude --agent orchestrator --settings ") {
		t.Fatalf("launch = %q, %v", line, ok)
	}
	if !strings.Contains(line, "orchestrator.settings.json") || strings.Contains(line, "{") {
		t.Errorf("unfilled launch: %q", line)
	}
	if _, ok := build.OrchestratorLaunch(p, "opencode", build.Opts{Out: out}); ok {
		t.Error("opencode has no orchestrator launch")
	}
}

func TestOrchestratorLaunch_QuotesUnsafePaths(t *testing.T) {
	dir, _ := setup(t)
	p, _ := verifyRun(t, dir)
	line, ok := build.OrchestratorLaunch(p, "claude-code", build.Opts{Out: "/tmp/with space/o'k"})
	if !ok || !strings.Contains(line, `'/tmp/with space/o'\''k/crew-build/claude-code/agents/orchestrator.settings.json'`) {
		t.Fatalf("path not quoted: %q", line)
	}
}

func TestCompile_StillEmitsForemen(t *testing.T) {
	dir, out := setup(t)
	files, _ := planFiles(t, dir, build.Opts{Out: out})
	if _, ok := findFile(files, filepath.Join("claude-code", "agents", "foreman-ship-change.md")); !ok {
		t.Fatal("foreman missing next to the orchestrator")
	}
}
