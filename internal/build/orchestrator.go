package build

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
	"github.com/crewfile-dev/crew/internal/verify"
)

const (
	orchestratorSettingsFile = mission.OrchestratorName + ".settings.json"
	kindOrchestratorSettings = "orchestrator-settings"
)

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// canHostMainSession: the harness runs custom agents as a main session and can
// give one a shell.
func canHostMainSession(h config.Harness) bool {
	return h.Orchestrator != nil && h.Agents.Supported && len(h.Capabilities["shell"]) > 0
}

// mainSessionHarness is a copy of h whose agents.static is merged with the
// main-session static keys. h is never modified.
func mainSessionHarness(h config.Harness) config.Harness {
	c := h
	static := make(map[string]any, len(h.Agents.Static)+len(h.Orchestrator.Static))
	for k, v := range h.Agents.Static {
		static[k] = v
	}
	for k, v := range h.Orchestrator.Static {
		static[k] = v
	}
	c.Agents.Static = static
	return c
}

// hasOrchestratorLaunch: the harness says how to start the orchestrator.
func hasOrchestratorLaunch(h config.Harness) bool {
	return h.Orchestrator != nil && h.Orchestrator.Launch != ""
}

// canHostOrchestrator: the harness can host it, start it, and apply deny
// rules. The orchestrator has a shell, so without a deny list a session could
// run commands only the owner may run.
func canHostOrchestrator(h config.Harness) bool {
	return canHostMainSession(h) && hasOrchestratorLaunch(h) && run.CanDeny(h)
}

// orchestratorSettingsPath is always under the build directory: the file holds
// absolute paths of this machine and isn't an agent file.
func orchestratorSettingsPath(crewDir, hn string, o Opts) string {
	return filepath.Join(BuildDir(crewDir, o), hn, "agents", orchestratorSettingsFile)
}

// orchestratorHost reports the harness, the absolute crew directory and the
// absolute settings path for hn, or false when the harness doesn't get an
// orchestrator.
func orchestratorHost(p *verify.Project, hn string, o Opts) (config.Harness, string, bool) {
	h := p.Registry.Harnesses[hn]
	if !canHostOrchestrator(h) {
		return h, "", false
	}
	crewDir, err := filepath.Abs(p.Dir)
	if err != nil {
		return h, "", false
	}
	return h, orchestratorSettingsPath(crewDir, hn, o), true
}

// OrchestratorSettingsPath is the absolute path of the settings file that
// crew compile writes for the orchestrator on harness hn. It is empty when the
// harness doesn't get an orchestrator.
func OrchestratorSettingsPath(p *verify.Project, hn string, o Opts) string {
	_, path, _ := orchestratorHost(p, hn, o)
	return path
}

// OrchestratorLaunch is the line that starts the orchestrator on harness hn,
// with the agent name and the absolute settings path filled in. It reports
// false when the harness doesn't get an orchestrator.
func OrchestratorLaunch(p *verify.Project, hn string, o Opts) (string, bool) {
	h, settings, ok := orchestratorHost(p, hn, o)
	if !ok {
		return "", false
	}
	line := strings.ReplaceAll(h.Orchestrator.Launch, "{name}", mission.OrchestratorName)
	return strings.ReplaceAll(line, "{settings}", shellQuote(settings)), true
}

// OrchestratorArgv is the command that starts the orchestrator on harness hn,
// as separate arguments (no shell quoting). It reports false when the harness
// doesn't get an orchestrator.
func OrchestratorArgv(p *verify.Project, hn string, o Opts) ([]string, bool) {
	h, settings, ok := orchestratorHost(p, hn, o)
	if !ok {
		return nil, false
	}
	words := strings.Fields(h.Orchestrator.Launch)
	if len(words) == 0 {
		return nil, false
	}
	argv := make([]string, len(words))
	for i, w := range words {
		w = strings.ReplaceAll(w, "{name}", mission.OrchestratorName)
		argv[i] = strings.ReplaceAll(w, "{settings}", settings)
	}
	return argv, true
}

// shellQuote single-quotes s unless it only has characters a shell leaves alone.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// orchestratorOutputs renders the orchestrator agent, and the
// settings file that carries its allow and deny rules.
func orchestratorOutputs(p *verify.Project, hn, crewDir, dir string, link bool, o Opts) ([]File, error) {
	h := mainSessionHarness(p.Registry.Harnesses[hn])
	a := mission.OrchestratorAgent()
	content, err := renderAgent(p.Registry, h, verify.Selected{Agent: a, Model: source.ModelInherit})
	if err != nil {
		return nil, fmt.Errorf("[%s] orchestrator: %w", hn, err)
	}
	settings, err := orchestratorSettings(p, hn, crewDir)
	if err != nil {
		return nil, err
	}
	out := []File{{Path: orchestratorSettingsPath(crewDir, hn, o), Root: filepath.Join(BuildDir(crewDir, o), hn, "agents"),
		Content: settings, Kind: kindOrchestratorSettings, Name: a.Name, Harnesses: []string{hn}}}
	dest := filepath.Join(dir, a.Name+h.Agents.Ext)
	if !link {
		return append(out, File{Path: dest, Root: dir, Content: content, Kind: "agent", Name: a.Name, Harnesses: []string{hn}}), nil
	}
	built := filepath.Join(BuildDir(crewDir, o), hn, "agents", a.Name+h.Agents.Ext)
	return append(out,
		File{Path: built, Root: filepath.Dir(built), Content: content, Kind: "agent-build", Name: a.Name, Harnesses: []string{hn}},
		File{Path: dest, Root: dir, Link: built, Kind: "agent", Name: a.Name, Harnesses: []string{hn}}), nil
}

// orchestratorSettings is {"permissions": {"allow": [...], "ask": [...], "deny": [...]}}.
// "ask" is there only when the harness has ask rules. The deny list is never
// empty: an orchestrator without one isn't written.
func orchestratorSettings(p *verify.Project, hn, crewDir string) ([]byte, error) {
	deny := run.DenyRules(p.Registry, hn, crewDir, false)
	if len(deny) == 0 {
		return nil, fmt.Errorf("[%s] orchestrator: harness can apply deny rules but defines none, so the orchestrator isn't built", hn)
	}
	o := p.Registry.Harnesses[hn].Orchestrator
	perms := map[string]any{"allow": append([]string{}, o.Allow...), "deny": deny}
	if len(o.Ask) > 0 {
		perms["ask"] = slices.Clone(o.Ask)
	}
	b, err := json.MarshalIndent(map[string]any{"permissions": perms}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("[%s] orchestrator settings: %w", hn, err)
	}
	return append(b, '\n'), nil
}
