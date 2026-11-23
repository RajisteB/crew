package scaffold

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/config"
)

const fixturePath = "testdata/harnesses-before-missions.yaml"

func writeHarnesses(t *testing.T, b []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harnesses.yaml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestUpgradeHarnesses_OldTemplateBecomesNewTemplate(t *testing.T) {
	fix := readFixture(t)
	dir := writeHarnesses(t, fix)
	res, err := UpgradeHarnesses(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := tmpl.ReadFile("template/harnesses.yaml")
	got, _ := os.ReadFile(filepath.Join(dir, "harnesses.yaml"))
	if !bytes.Equal(got, want) {
		t.Error("upgraded file differs from the embedded template")
	}
	if len(res.Added) != 2 || !strings.HasPrefix(res.Added[0], "claude-code:") || !strings.HasPrefix(res.Added[1], "opencode:") {
		t.Errorf("Added = %q", res.Added)
	}
	if res.Backup != "harnesses.yaml.before-missions" {
		t.Errorf("Backup = %q", res.Backup)
	}
	bak, _ := os.ReadFile(filepath.Join(dir, res.Backup))
	if !bytes.Equal(bak, fix) {
		t.Error("backup does not hold the original bytes")
	}
}

func TestUpgradeHarnesses_SecondRunChangesNothing(t *testing.T) {
	dir := writeHarnesses(t, readFixture(t))
	if _, err := UpgradeHarnesses(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "harnesses.yaml")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	res, err := UpgradeHarnesses(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 0 || len(res.Skipped) != 0 || res.Backup != "" {
		t.Errorf("second run = %+v", res)
	}
	fi, _ := os.Stat(path)
	if fi.ModTime().After(old.Add(time.Second)) {
		t.Error("file was rewritten")
	}
}

func TestUpgradeHarnesses_KeepsOwnEdits(t *testing.T) {
	s := string(readFixture(t))
	s = strings.Replace(s, "verified: 2026-11-18", "verified: 2026-12-01", 1)
	s = "# mine\n" + s
	s = strings.Replace(s, "    rules:\n      name_max: 64\n      name_forbidden: [claude, anthropic]",
		"    deny:\n      all: [\"Bash(rm:*)\"]\n    rules:\n      name_max: 64\n      name_forbidden: [claude, anthropic]", 1)
	_, cursor, _ := strings.Cut(s, "\n  cursor:\n")
	s += "\n  mine:\n" + cursor
	dir := writeHarnesses(t, []byte(s))
	if _, err := UpgradeHarnesses(dir); err != nil {
		t.Fatal(err)
	}
	reg, err := config.LoadRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	cc := reg.Harnesses["claude-code"]
	if got := cc.Deny.All; len(got) != 1 || got[0] != "Bash(rm:*)" {
		t.Errorf("Deny.All = %q", got)
	}
	if cc.Headless.DenyVia != "settings-file" {
		t.Errorf("DenyVia = %q", cc.Headless.DenyVia)
	}
	if !strings.Contains(strings.Join(cc.Headless.Command, " "), "{settings}") {
		t.Error("command lacks {settings}")
	}
	if cc.Orchestrator.Launch == "" {
		t.Error("no orchestrator launch")
	}
	if _, ok := reg.Harnesses["mine"]; !ok {
		t.Error("harness mine lost")
	}
	if reg.Verified != "2026-12-01" {
		t.Errorf("Verified = %q", reg.Verified)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "harnesses.yaml"))
	if !strings.Contains(string(b), "# mine") {
		t.Error("comment lost")
	}
}

func TestUpgradeHarnesses_CustomCommandLeftAlone(t *testing.T) {
	s := strings.Replace(string(readFixture(t)), "--output-format, json]", "--output-format, json, --verbose]", 1)
	dir := writeHarnesses(t, []byte(s))
	res, err := UpgradeHarnesses(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The skipped harness keeps its content, old key included (re-encoding the
	// file can change whitespace and comment alignment). Only opencode, which is
	// not skipped, has its foreman: block renamed.
	reg, err := config.LoadRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	cc, oc := reg.Harnesses["claude-code"], reg.Harnesses["opencode"]
	if !cc.UsesOldKey || cc.Headless.DenyVia != "" || strings.Contains(strings.Join(cc.Headless.Command, " "), "{settings}") || !slices.Contains(cc.Headless.Command, "--verbose") {
		t.Errorf("claude-code changed: %+v", cc)
	}
	if len(cc.Deny.All) != 0 || len(cc.Deny.Stations) != 0 {
		t.Errorf("claude-code got deny rules: %+v", cc.Deny)
	}
	if got := cc.Orchestrator.Allow; len(got) != 0 {
		t.Errorf("claude-code allow list was extended: %q", got)
	}
	if oc.UsesOldKey || oc.Orchestrator == nil {
		t.Errorf("opencode not renamed: %+v", oc)
	}
	if len(res.Added) != 1 || !strings.HasPrefix(res.Added[0], "opencode: foreman: renamed") {
		t.Errorf("res = %+v", res)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "claude-code") || !strings.Contains(res.Skipped[0], "{settings}") {
		t.Errorf("Skipped = %q", res.Skipped)
	}
}

func TestUpgradeHarnesses_NoFile(t *testing.T) {
	_, err := UpgradeHarnesses(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "crew init") {
		t.Errorf("err = %v", err)
	}
}
