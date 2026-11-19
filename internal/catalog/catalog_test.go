package catalog_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/catalog"
	"github.com/crewfile-dev/crew/internal/config"
)

const api = `{
  "anthropic": {"models": {
    "claude-opus-5-5": {"id": "claude-opus-5-5", "name": "Claude Opus 5.5",
      "reasoning_options": [{"type": "effort", "values": ["low", "high", "max"]}],
      "limit": {"context": 1000000}, "cost": {"input": 4, "output": 20, "cache_read": 0.2}},
    "claude-old": {"id": "claude-old", "name": "Old", "status": "deprecated", "cost": {"input": 1, "output": 2}}
  }},
  "opencode-go": {"models": {
    "kimi-k3": {"id": "kimi-k3", "name": "Kimi K3",
      "reasoning_options": [{"type": "toggle"}, {"type": "effort", "values": ["max", "max", "BAD VALUE"]}],
      "cost": {"input": 3, "output": 15}},
    "mimo": {"id": "mimo", "name": "MiMo"}
  }}
}`

func reg() *config.Registry {
	return &config.Registry{Harnesses: map[string]config.Harness{
		"claude-code": {Catalog: &config.CatalogSpec{Providers: []string{"anthropic"}, IDStyle: "model"}},
		"opencode":    {Catalog: &config.CatalogSpec{Providers: []string{"opencode-go"}, IDStyle: "provider/model"}},
		"cursor":      {},
	}}
}

func build(t *testing.T) *config.Models {
	t.Helper()
	parsed, err := catalog.Parse(strings.NewReader(api))
	if err != nil {
		t.Fatal(err)
	}
	m, err := catalog.Build(parsed, reg(), "https://example.test/api.json", "2026-11-18")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBuildMapsEachHarness(t *testing.T) {
	m := build(t)
	if _, ok := m.Harnesses["cursor"]; ok {
		t.Fatal("harness without a catalog should get no list")
	}
	cc := m.Harnesses["claude-code"]
	if len(cc) != 1 || cc[0].ID != "claude-opus-5-5" {
		t.Fatalf("deprecated models should be dropped, bare ids kept: %+v", cc)
	}
	if got := strings.Join(cc[0].Efforts, ","); got != "low,high,max" {
		t.Fatalf("efforts = %s", got)
	}
	if cc[0].Context != 1000000 || cc[0].InputPerMTok != 4 || cc[0].CacheReadPerMTok == nil || *cc[0].CacheReadPerMTok != 0.2 {
		t.Fatalf("limits/prices not copied: %+v", cc[0])
	}
	oc := m.Harnesses["opencode"]
	if len(oc) != 2 || oc[0].ID != "opencode-go/kimi-k3" || oc[1].ID != "opencode-go/mimo" {
		t.Fatalf("provider-prefixed ids, sorted: %+v", oc)
	}
	if got := strings.Join(oc[0].Efforts, ","); got != "max" {
		t.Fatalf("efforts should be deduped and junk dropped, got %s", got)
	}
	if len(oc[1].Efforts) != 0 {
		t.Fatal("model without effort options should list none")
	}
	if m.Fetched != "2026-11-18" || m.Source != "https://example.test/api.json" {
		t.Fatalf("header fields: %+v", m)
	}
}

func TestBuildFailsOnMissingProvider(t *testing.T) {
	parsed, _ := catalog.Parse(strings.NewReader(`{"anthropic": {"models": {}}}`))
	_, err := catalog.Build(parsed, reg(), "u", "2026-11-18")
	if err == nil || !strings.Contains(err.Error(), `"opencode-go"`) {
		t.Fatalf("want missing-provider error, got %v", err)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := catalog.Parse(strings.NewReader("<html>")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestFetchAndWriteRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(api))
	}))
	defer srv.Close()

	if _, err := catalog.Fetch(srv.URL + "/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want HTTP status error, got %v", err)
	}
	parsed, err := catalog.Fetch(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	m, err := catalog.Build(parsed, reg(), srv.URL, "2026-11-18")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := catalog.Write(filepath.Join(dir, config.ModelsFile), m); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, config.ModelsFile))
	if !strings.HasPrefix(string(b), "# models.yaml") {
		t.Fatalf("missing header comment:\n%s", b)
	}
	back, err := config.LoadModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := back.Find("opencode", "opencode-go/kimi-k3"); !ok || !e.SupportsEffort("max") || e.SupportsEffort("low") {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestBuildRejectsBadIDStyle(t *testing.T) {
	parsed, _ := catalog.Parse(strings.NewReader(api))
	r := &config.Registry{Harnesses: map[string]config.Harness{
		"x": {Catalog: &config.CatalogSpec{Providers: []string{"anthropic"}, IDStyle: "provider-model"}},
	}}
	if _, err := catalog.Build(parsed, r, "u", "2026-11-18"); err == nil || !strings.Contains(err.Error(), "id_style") {
		t.Fatalf("want id_style error, got %v", err)
	}
}
