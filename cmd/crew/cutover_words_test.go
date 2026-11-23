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
	"internal/config/config.go":         true,
	"internal/config/oldkey.go":         true,
	"internal/config/oldkey_test.go":    true,
	"internal/build/cutover_test.go":    true,
	"internal/build/oldkey_test.go":     true,
	"internal/build/build.go":           true,
	"internal/verify/factories_test.go": true,
	"cmd/crew/factory_test.go":          true,
	"cmd/crew/cutover_words_test.go":    true,
	"docs/missions.md":                  true,
	"README.md":                         true,
}

var foremanSkipped = map[string]bool{
	".git":             true,
	".worktrees":       true,
	".superpowers":     true,
	"docs/superpowers": true,
}

// foremanAllowedPath: under internal/scaffold/ only the upgrade's two files,
// its tests and its test fixtures, not the template a user's config is made from.
func foremanAllowedPath(rel string) bool {
	if foremanAllowed[rel] {
		return true
	}
	rest, ok := strings.CutPrefix(rel, "internal/scaffold/")
	if !ok {
		return false
	}
	return rest == "upgrade.go" || rest == "upgrade_orchestrator.go" || strings.HasSuffix(rest, "_test.go") || strings.HasPrefix(rest, "testdata/")
}

func TestCutover_ScaffoldAllowsOnlyTheUpgradeItsTestsAndFixtures(t *testing.T) {
	for rel, want := range map[string]bool{
		"internal/scaffold/upgrade.go":                             true,
		"internal/scaffold/upgrade_orchestrator.go":                true,
		"internal/scaffold/upgrade_ask_test.go":                    true,
		"internal/scaffold/testdata/harnesses-before-cutover.yaml": true,
		"internal/scaffold/template/harnesses.yaml":                false,
		"internal/scaffold/scaffold.go":                            false,
		"internal/scaffold/upgrade_new.go":                         false,
	} {
		if got := foremanAllowedPath(rel); got != want {
			t.Errorf("foremanAllowedPath(%s) = %v, want %v", rel, got, want)
		}
	}
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
