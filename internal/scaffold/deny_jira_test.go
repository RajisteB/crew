package scaffold

import (
	"slices"
	"testing"

	"github.com/crewfile-dev/crew/internal/config"
)

func TestTemplate_DeniesJiraSetup(t *testing.T) {
	dir := t.TempDir() + "/crew"
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	reg, err := config.LoadRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	all := reg.Harnesses["claude-code"].Deny.All
	for _, want := range []string{"Bash(crew jira setup:*)", "Bash(crew automerge:*)"} {
		if !slices.Contains(all, want) {
			t.Fatalf("deny.all lacks %q: %v", want, all)
		}
	}
}
