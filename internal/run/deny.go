package run

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/crewfile-dev/crew/internal/config"
)

const denyViaSettingsFile = "settings-file"

// CanDeny reports whether the harness can apply deny rules to a headless session.
func CanDeny(h config.Harness) bool {
	return h.Headless != nil && h.Headless.DenyVia == denyViaSettingsFile
}

// DenyRules returns the harness's deny rules with {crew_dir} expanded: the
// "all" rules, plus the "stations" rules when station is true. It returns nil
// for a harness that can't apply deny rules. The registry is not modified.
func DenyRules(reg *config.Registry, harness, crewDir string, station bool) []string {
	h, ok := reg.Harnesses[harness]
	if !ok || !CanDeny(h) {
		return nil
	}
	src := append([]string(nil), h.Deny.All...)
	if station {
		src = append(src, h.Deny.Stations...)
	}
	if len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	for i, r := range src {
		out[i] = strings.ReplaceAll(r, "{crew_dir}", crewDir)
	}
	return out
}

// writeSettings writes the deny rules to a 0600 temp settings file and
// returns its path and a cleanup that removes it. With no rules it returns an
// empty path. Rules the harness cannot apply are an error, never dropped.
func writeSettings(harness string, h config.HeadlessSpec, rules []string) (string, func(), error) {
	noop := func() {}
	if len(rules) == 0 {
		return "", noop, nil
	}
	if h.DenyVia != denyViaSettingsFile {
		return "", noop, fmt.Errorf("%s: deny rules requested but headless.deny_via is not %q, refusing to run without them", harness, denyViaSettingsFile)
	}
	body, err := json.Marshal(map[string]any{"permissions": map[string]any{"deny": rules}})
	if err != nil {
		return "", noop, fmt.Errorf("%s: encode deny settings: %w", harness, err)
	}
	f, err := os.CreateTemp("", "crew-settings-*.json") // created 0600
	if err != nil {
		return "", noop, fmt.Errorf("%s: create deny settings file: %w", harness, err)
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	_, werr := f.Write(body)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		cleanup()
		return "", noop, fmt.Errorf("%s: write deny settings file: %w", harness, werr)
	}
	return f.Name(), cleanup, nil
}
