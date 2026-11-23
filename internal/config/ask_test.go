package config

import (
	"slices"
	"testing"
)

func TestDecodeRegistry_ReadsTheOrchestratorsAskRules(t *testing.T) {
	r, err := DecodeRegistry("harnesses.yaml", registryWith("    orchestrator: {launch: y, allow: [A], ask: [B, C]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Harnesses["h"].Orchestrator.Ask; !slices.Equal(got, []string{"B", "C"}) {
		t.Errorf("Ask = %v, want [B C]", got)
	}
}
