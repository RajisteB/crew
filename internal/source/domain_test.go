package source_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/source"
)

func writeDomain(t *testing.T, name, content string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, source.DomainsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadDomains_ParsesFrontmatterAndBody(t *testing.T) {
	root := writeDomain(t, "design.md", `---
name: design
description: UI and brand work.
factories: [ship-change, ui-mockup]
paths: [web/, docs/brand]
releases: [web-app]
---
Design guidance.
`)
	ds, errs := source.LoadDomains(root)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(ds) != 1 {
		t.Fatalf("got %d domains", len(ds))
	}
	d := ds[0]
	if d.Name != "design" || d.Description != "UI and brand work." {
		t.Fatalf("header = %+v", d)
	}
	if !reflect.DeepEqual(d.Factories, []string{"ship-change", "ui-mockup"}) ||
		!reflect.DeepEqual(d.Paths, []string{"web/", "docs/brand"}) ||
		!reflect.DeepEqual(d.Releases, []string{"web-app"}) {
		t.Fatalf("lists = %+v", d)
	}
	if d.Body != "Design guidance.\n" || d.Path != filepath.Join("domains", "design.md") {
		t.Fatalf("body/path = %q %q", d.Body, d.Path)
	}
}

func TestLoadDomains_MissingFolderIsEmpty(t *testing.T) {
	ds, errs := source.LoadDomains(t.TempDir())
	if len(ds) != 0 || len(errs) != 0 {
		t.Fatalf("got %v %v", ds, errs)
	}
}

func TestLoadDomains_UnknownKey(t *testing.T) {
	root := writeDomain(t, "design.md", "---\nname: design\ncolour: red\n---\nx\n")
	_, errs := source.LoadDomains(root)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `unknown key "colour"`) {
		t.Fatalf("errs = %v", errs)
	}
}
