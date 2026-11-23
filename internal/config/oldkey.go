package config

import (
	"fmt"
	"sort"
)

// oldForemanSpec is the foreman: block as it was spelled before the cutover.
// Loading folds it into Orchestrator; nothing else reads it.
type oldForemanSpec struct {
	Static             map[string]any `yaml:"static"`
	Launch             string         `yaml:"launch"` // started foremen; read and dropped
	OrchestratorLaunch string         `yaml:"orchestrator_launch"`
	Allow              []string       `yaml:"orchestrator_allow"`
}

// foldOldKeys returns r with every harness's foreman: block turned into
// Orchestrator. It builds a new map and new Harness values; r is not changed.
func foldOldKeys(name string, r Registry) (Registry, error) {
	hs := make(map[string]Harness, len(r.Harnesses))
	for _, hn := range sortedHarnessNames(r.Harnesses) {
		h := r.Harnesses[hn]
		if h.OldForeman != nil {
			if h.Orchestrator != nil {
				return r, fmt.Errorf("%s: harness %s has both foreman: and orchestrator:; keep orchestrator: and remove foreman:", name, hn)
			}
			f := h.OldForeman
			h.Orchestrator = &OrchestratorSpec{Static: f.Static, Launch: f.OrchestratorLaunch, Allow: f.Allow}
			h.OldForeman = nil
			h.UsesOldKey = true
		}
		hs[hn] = h
	}
	r.Harnesses = hs
	return r, nil
}

func sortedHarnessNames(m map[string]Harness) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
