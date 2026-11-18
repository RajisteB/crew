// Package catalog builds models.yaml, the baseline list of every model each
// harness can run, from the public models.dev catalog (the same data
// OpenCode uses for its model list and effort variants).
package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/crewfile-dev/crewfile/internal/config"
)

const (
	fetchTimeout = 30 * time.Second
	maxBodyBytes = 64 << 20 // models.dev is ~7 MB today; refuse anything absurd
	idProvider   = "provider/model"
	idModel      = "model"
)

// API is the subset of models.dev's api.json that crewfile reads.
type API map[string]Provider

type Provider struct {
	Models map[string]Model `json:"models"`
}

type Model struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Status           string            `json:"status"`
	ReasoningOptions []ReasoningOption `json:"reasoning_options"`
	Limit            struct {
		Context int `json:"context"`
	} `json:"limit"`
	Cost *struct {
		Input     float64  `json:"input"`
		Output    float64  `json:"output"`
		CacheRead *float64 `json:"cache_read"`
	} `json:"cost"`
}

type ReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

var effortRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Fetch downloads and parses the catalog.
func Fetch(url string) (API, error) {
	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("fetch %s: response is larger than %d MB", url, maxBodyBytes>>20)
	}
	return Parse(bytes.NewReader(body))
}

// Parse decodes a models.dev api.json document.
func Parse(r io.Reader) (API, error) {
	var a API
	if err := json.NewDecoder(r).Decode(&a); err != nil {
		return nil, fmt.Errorf("catalog is not valid models.dev JSON: %w", err)
	}
	if len(a) == 0 {
		return nil, errors.New("catalog is empty")
	}
	return a, nil
}

// Build turns the catalog into one model list per harness that has a catalog
// spec. Deprecated models are left out. It fails if a harness names a
// provider the catalog doesn't have, since that usually means a typo.
func Build(api API, reg *config.Registry, source, today string) (*config.Models, error) {
	out := &config.Models{
		Version:   config.SchemaVersion,
		Source:    source,
		Fetched:   today,
		Harnesses: map[string][]config.ModelEntry{},
	}
	var errs []string
	for _, hn := range sortedKeys(reg.Harnesses) {
		spec := reg.Harnesses[hn].Catalog
		if spec == nil {
			continue
		}
		if spec.IDStyle != idModel && spec.IDStyle != idProvider {
			errs = append(errs, fmt.Sprintf("%s: id_style must be %q or %q (got %q)", hn, idModel, idProvider, spec.IDStyle))
			continue
		}
		entries, missing := harnessEntries(api, spec)
		for _, p := range missing {
			errs = append(errs, fmt.Sprintf("%s: provider %q is not in the catalog", hn, p))
		}
		out.Harnesses[hn] = entries
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	return out, nil
}

func harnessEntries(api API, spec *config.CatalogSpec) ([]config.ModelEntry, []string) {
	var entries []config.ModelEntry
	var missing []string
	seen := map[string]bool{}
	for _, pn := range spec.Providers {
		prov, ok := api[pn]
		if !ok {
			missing = append(missing, pn)
			continue
		}
		for _, key := range sortedKeys(prov.Models) {
			m := prov.Models[key]
			if m.Status == "deprecated" || strings.TrimSpace(key) == "" || strings.ContainsAny(key, " \t\n#") {
				continue
			}
			id := key
			if spec.IDStyle == idProvider {
				id = pn + "/" + key
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			entries = append(entries, entry(id, m))
		}
	}
	return entries, missing
}

func entry(id string, m Model) config.ModelEntry {
	e := config.ModelEntry{ID: id, Name: m.Name, Context: m.Limit.Context, Efforts: efforts(m)}
	if e.Name == "" {
		e.Name = id
	}
	if m.Cost != nil {
		e.InputPerMTok, e.OutputPerMTok = m.Cost.Input, m.Cost.Output
		if m.Cost.CacheRead != nil {
			v := *m.Cost.CacheRead
			e.CacheReadPerMTok = &v
		}
	}
	return e
}

// efforts returns the model's effort values in catalog order, deduplicated,
// dropping anything that isn't a plain lowercase word.
func efforts(m Model) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, o := range m.ReasoningOptions {
		if o.Type != "effort" {
			continue
		}
		for _, v := range o.Values {
			if effortRe.MatchString(v) && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

const header = `# models.yaml — the baseline list: every model each harness can run, the effort
# levels each model accepts, and its price (USD per million tokens).
#
# Generated by ` + "`crewfile models sync`" + ` from the catalog in harnesses.yaml. Don't edit it
# by hand; re-run sync. Pick an agent's model by copying an id from here.
# ` + "`crewfile models`" + ` prints the same list as a table.

`

// Write saves m to path atomically (temp file + rename).
func Write(path string, m *config.Models) error {
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	if err := enc.Close(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".models-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
