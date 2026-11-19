package build

import (
	"fmt"
	"path/filepath"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/factory"
	"github.com/crewfile-dev/crew/internal/source"
	"github.com/crewfile-dev/crew/internal/verify"
)

// canHostForeman: the harness runs custom agents as a main session and can
// give one a shell (the foreman drives everything through `crew run`).
func canHostForeman(h config.Harness) bool {
	return h.Foreman != nil && h.Agents.Supported && len(h.Capabilities["shell"]) > 0
}

// foremanHarness is a copy of h whose agents.static is merged with
// foreman.static (foreman wins). h itself is never modified.
func foremanHarness(h config.Harness) config.Harness {
	c := h
	static := make(map[string]any, len(h.Agents.Static)+len(h.Foreman.Static))
	for k, v := range h.Agents.Static {
		static[k] = v
	}
	for k, v := range h.Foreman.Static {
		static[k] = v
	}
	c.Agents.Static = static
	return c
}

// foremanOutputs renders one foreman per factory for harness hn, as a built
// file plus link (user scope) or a copy (project scope).
func foremanOutputs(p *verify.Project, hn, crewDir, dir string, link bool, o Opts) ([]File, error) {
	h := foremanHarness(p.Registry.Harnesses[hn])
	var out []File
	for _, f := range p.Factories {
		a := factory.ForemanAgent(f)
		content, err := renderAgent(p.Registry, h, verify.Selected{Agent: a, Model: source.ModelInherit})
		if err != nil {
			return nil, fmt.Errorf("%s [%s] foreman: %w", f.Path, hn, err)
		}
		dest := filepath.Join(dir, a.Name+h.Agents.Ext)
		if !link {
			out = append(out, File{Path: dest, Root: dir, Content: content, Kind: "agent", Name: a.Name, Harnesses: []string{hn}})
			continue
		}
		built := filepath.Join(BuildDir(crewDir, o), hn, "agents", a.Name+h.Agents.Ext)
		out = append(out,
			File{Path: built, Root: filepath.Dir(built), Content: content, Kind: "agent-build", Name: a.Name, Harnesses: []string{hn}},
			File{Path: dest, Root: dir, Link: built, Kind: "agent", Name: a.Name, Harnesses: []string{hn}})
	}
	return out, nil
}
