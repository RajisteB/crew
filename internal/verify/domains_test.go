package verify_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/verify"
)

func writeDomainFile(t *testing.T, dir, stem, content string) {
	t.Helper()
	d := filepath.Join(dir, "domains")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, stem+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const designDomain = `---
name: design
description: UI work.
factories: [ship-change]
---
Body.
`

func TestVerify_DomainUnknownFactory(t *testing.T) {
	dir := fresh(t)
	writeDomainFile(t, dir, "design", strings.Replace(designDomain, "[ship-change]", "[ui-mockup]", 1))
	expectError(t, dir, `domain design: no factory "ui-mockup"`)
}

func TestVerify_DomainValidIsClean(t *testing.T) {
	dir := fresh(t)
	writeDomainFile(t, dir, "design", designDomain)
	p, rep := verify.Run(dir)
	if !rep.OK(false) || len(p.Domains) != 1 {
		t.Fatalf("domains=%d issues=%+v", len(p.Domains), rep.Issues)
	}
}

func TestVerify_DomainReleasesIsInfo(t *testing.T) {
	dir := fresh(t)
	writeDomainFile(t, dir, "design", strings.Replace(designDomain, "---\nBody", "releases: [web]\n---\nBody", 1))
	_, rep := verify.Run(dir)
	if !rep.OK(false) {
		t.Fatalf("releases must not warn: %+v", rep.Issues)
	}
	found := false
	for _, i := range rep.Issues {
		if i.Level == verify.Info && strings.Contains(i.Msg, "releases are checked from Project 2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no info note: %+v", rep.Issues)
	}
}

func TestVerify_DomainDuplicateNameStemEmpty(t *testing.T) {
	dir := fresh(t)
	writeDomainFile(t, dir, "design", designDomain)
	writeDomainFile(t, dir, "other", strings.Replace(designDomain, "UI work.", "", 1))
	expectError(t, dir, `domain design: name is also used by`)
	expectError(t, dir, `name "design" must match the filename "other"`)
	expectError(t, dir, `domain design: description is required`)
}

func TestVerify_DomainEmptyFactories(t *testing.T) {
	dir := fresh(t)
	writeDomainFile(t, dir, "design", strings.Replace(designDomain, "[ship-change]", "[]", 1))
	expectError(t, dir, `domain design: factories must list at least one factory`)
}
