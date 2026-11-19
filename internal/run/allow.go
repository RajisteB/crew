package run

import (
	"sort"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/source"
)

// AllowedTools is the native tool list an unattended station may use: the
// agent's capabilities mapped through harnesses.yaml, nothing more.
func AllowedTools(reg *config.Registry, harness string, a source.Agent) []string {
	h := reg.Harnesses[harness]
	caps := a.Capabilities
	if a.InheritsAll() {
		caps = reg.Capabilities
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range caps {
		for _, tool := range h.Capabilities[c] {
			if !seen[tool] {
				seen[tool] = true
				out = append(out, tool)
			}
		}
	}
	sort.Strings(out)
	return out
}
