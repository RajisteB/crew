package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// foremanAllowed lists the files that may still name the old foreman key:
// they handle the old key or describe an old compile.
var foremanAllowed = map[string]bool{
	"internal/config/config.go":                 true,
	"internal/config/oldkey.go":                 true,
	"internal/config/oldkey_test.go":            true,
	"internal/scaffold/upgrade.go":              true,
	"internal/scaffold/upgrade_orchestrator.go": true,
	"internal/build/cutover_test.go":            true,
	"internal/build/oldkey_test.go":             true,
	"internal/build/build.go":                   true,
	"internal/verify/factories_test.go":         true,
	"cmd/crew/factory_test.go":                  true,
	"cmd/crew/cutover_words_test.go":            true,
	"docs/missions.md":                          true,
	"README.md":                                 true,
}

var foremanSkipped = map[string]bool{
	".git":             true,
	".worktrees":       true,
	".superpowers":     true,
	"docs/superpowers": true,
}

func foremanAllowedPath(rel string) bool {
	return foremanAllowed[rel] || strings.HasPrefix(rel, "internal/scaffold/")
}

func TestCutover_ForemanIsNamedOnlyWhereTheOldKeyIsHandled(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if foremanSkipped[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(rel) {
		case ".go", ".md", ".yaml", ".yml":
		default:
			return nil
		}
		if foremanAllowedPath(rel) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(b)), "forem") {
			t.Errorf("%s names the foreman; only files that handle the old key may", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readGuide(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "missions.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDocs_GuideHasAPlainRunsSection(t *testing.T) {
	guide := readGuide(t)
	_, after, ok := strings.Cut(guide, "\n## Plain runs\n")
	if !ok {
		t.Fatal("docs/missions.md has no '## Plain runs' heading")
	}
	section, _, _ := strings.Cut(after, "\n## ")
	for _, want := range []string{"--wait", "crew run approve"} {
		if !strings.Contains(section, want) {
			t.Errorf("the Plain runs section does not mention %q", want)
		}
	}
}

func TestDocs_GuideShowsTheOrchestratorBlock(t *testing.T) {
	guide := readGuide(t)
	if !regexp.MustCompile(`(?m)^\s*orchestrator:\s*(#.*)?\n(?:.*\n){0,4}?\s*launch:`).MatchString(guide) {
		t.Error("the guide has no orchestrator: block with a launch: line")
	}
	if regexp.MustCompile(`(?m)^\s*orchestrator_allow:`).MatchString(guide) {
		t.Error("the guide still shows an orchestrator_allow: line")
	}
}
