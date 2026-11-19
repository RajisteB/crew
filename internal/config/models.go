package config

// ---------- models.yaml ----------

// Models is the baseline list: every model each harness can run, generated
// from the catalog (models.dev) by `crew models sync`.
type Models struct {
	Version   int                     `yaml:"version"`
	Source    string                  `yaml:"source"`
	Fetched   string                  `yaml:"fetched"` // YYYY-MM-DD
	Harnesses map[string][]ModelEntry `yaml:"harnesses"`
}

// ModelEntry is one model as one harness names it.
type ModelEntry struct {
	ID               string   `yaml:"id"` // exactly what an agent's model: holds and the harness receives
	Name             string   `yaml:"name"`
	Efforts          []string `yaml:"efforts,flow"` // empty = no effort setting for this model
	Context          int      `yaml:"context,omitempty"`
	InputPerMTok     float64  `yaml:"input_per_mtok"`
	OutputPerMTok    float64  `yaml:"output_per_mtok"`
	CacheReadPerMTok *float64 `yaml:"cache_read_per_mtok,omitempty"`
}

// Find returns the entry for id in harness hn.
func (m *Models) Find(hn, id string) (ModelEntry, bool) {
	if m == nil {
		return ModelEntry{}, false
	}
	for _, e := range m.Harnesses[hn] {
		if e.ID == id {
			return e, true
		}
	}
	return ModelEntry{}, false
}

// SupportsEffort reports whether e accepts the effort value.
func (e ModelEntry) SupportsEffort(v string) bool {
	for _, x := range e.Efforts {
		if x == v {
			return true
		}
	}
	return false
}
