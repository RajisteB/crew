package config

import (
	"reflect"
	"strings"
	"testing"
)

func registryWith(block string) []byte {
	return []byte("version: 1\nharnesses:\n  h:\n    display: H\n" + block)
}

func TestDecodeRegistry_ReadsTheOldForemanKey(t *testing.T) {
	r, err := DecodeRegistry("harnesses.yaml", registryWith(
		"    foreman: {static: {mode: primary}, launch: \"x {name}\", orchestrator_launch: \"y {name} {settings}\", orchestrator_allow: [A]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	h := r.Harnesses["h"]
	want := &OrchestratorSpec{Static: map[string]any{"mode": "primary"}, Launch: "y {name} {settings}", Allow: []string{"A"}}
	if !reflect.DeepEqual(h.Orchestrator, want) {
		t.Errorf("Orchestrator = %+v", h.Orchestrator)
	}
	if h.OldForeman != nil {
		t.Error("OldForeman not cleared")
	}
	if !h.UsesOldKey {
		t.Error("UsesOldKey not set")
	}
}

func TestDecodeRegistry_OldKeyWithOnlyAForemanLaunch(t *testing.T) {
	r, err := DecodeRegistry("harnesses.yaml", registryWith("    foreman: {launch: \"x\"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	h := r.Harnesses["h"]
	if h.Orchestrator == nil || h.Orchestrator.Launch != "" {
		t.Errorf("Orchestrator = %+v", h.Orchestrator)
	}
}

func TestDecodeRegistry_BothKeysIsAnError(t *testing.T) {
	_, err := DecodeRegistry("harnesses.yaml", registryWith("    foreman: {launch: x}\n    orchestrator: {launch: y}\n"))
	want := "harnesses.yaml: harness h has both foreman: and orchestrator:; keep orchestrator: and remove foreman:"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
}

func TestDecodeRegistry_OldSpellingsUnderTheNewKeyAreRefused(t *testing.T) {
	_, err := DecodeRegistry("harnesses.yaml", registryWith("    orchestrator: {orchestrator_launch: x}\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("err = %v", err)
	}
}

func TestDecodeRegistry_NewKeyDoesNotSetUsesOldKey(t *testing.T) {
	r, err := DecodeRegistry("harnesses.yaml", registryWith("    orchestrator: {launch: y}\n"))
	if err != nil {
		t.Fatal(err)
	}
	h := r.Harnesses["h"]
	if h.UsesOldKey || h.OldForeman != nil || h.Orchestrator == nil || h.Orchestrator.Launch != "y" {
		t.Errorf("h = %+v", h)
	}
}

func TestDecodeRegistry_AskUnderTheOldKeyIsRefused(t *testing.T) {
	_, err := DecodeRegistry("harnesses.yaml", registryWith("    foreman: {orchestrator_launch: x, ask: [B]}\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("err = %v, want an unknown-key error", err)
	}
}

func TestDecodeRegistry_AFoldedOldBlockHasNoAskRules(t *testing.T) {
	r, err := DecodeRegistry("harnesses.yaml", registryWith("    foreman: {orchestrator_launch: x, orchestrator_allow: [A]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Harnesses["h"].Orchestrator.Ask; got != nil {
		t.Errorf("Ask = %v, want none", got)
	}
}
