package scaffold

import (
	"slices"
	"testing"

	"github.com/crewfile-dev/crew/internal/source"
)

func TestTemplate_ReviewStationCanRunCommands(t *testing.T) {
	dir := t.TempDir() + "/crew"
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	factories, errs := source.LoadFactories(dir)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	agents, errs := source.LoadAgents(dir)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	var name string
	for _, f := range factories {
		if f.Name == "ship-change" {
			name = f.Stations["review"].Agent
		}
	}
	if name == "" {
		t.Fatal("ship-change has no review station agent")
	}
	for _, a := range agents {
		if a.Name != name {
			continue
		}
		if !slices.Contains(a.Capabilities, "shell") {
			t.Errorf("review agent %s lacks shell: %v", name, a.Capabilities)
		}
		for _, bad := range []string{"edit", "write"} {
			if slices.Contains(a.Capabilities, bad) {
				t.Errorf("review agent %s has %s: %v", name, bad, a.Capabilities)
			}
		}
		return
	}
	t.Fatalf("review agent %s not found", name)
}
