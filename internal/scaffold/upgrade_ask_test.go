package scaffold

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/config"
)

const askLine = `      ask: ["Bash(crew run approve:*)", "Bash(crew run goto:*)"]` + "\n"

// templateWith is the embedded template with old replaced by new once.
func templateWith(t *testing.T, old, new string) []byte {
	t.Helper()
	tb, err := tmpl.ReadFile("template/harnesses.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(tb, []byte(old)) {
		t.Fatalf("test setup: the template has no %q", old)
	}
	return bytes.Replace(tb, []byte(old), []byte(new), 1)
}

// assertUnchanged checks that UpgradeHarnesses wrote nothing to dir.
func assertUnchanged(t *testing.T, dir string, want []byte, res HarnessUpgrade) {
	t.Helper()
	if len(res.Added) != 0 || res.Backup != "" {
		t.Errorf("res = %+v, want nothing done", res)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "harnesses.yaml")); !bytes.Equal(got, want) {
		t.Errorf("the file changed:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, backupFile)); err == nil {
		t.Error("a backup was written")
	}
}

func TestUpgradeHarnesses_AddsTheAskRules(t *testing.T) {
	dir := writeHarnesses(t, templateWith(t, askLine, ""))
	res, err := UpgradeHarnesses(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cc := addedFor(res.Added, "claude-code"); cc != "claude-code: ask rules for approve and goto" || len(res.Added) != 1 {
		t.Errorf("Added = %q", res.Added)
	}
	reg, err := config.LoadRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reg.Harnesses["claude-code"].Orchestrator.Ask; !slices.Equal(got, []string{"Bash(crew run approve:*)", "Bash(crew run goto:*)"}) {
		t.Errorf("ask = %q", got)
	}
	want, _ := tmpl.ReadFile("template/harnesses.yaml")
	if got, _ := os.ReadFile(filepath.Join(dir, "harnesses.yaml")); !bytes.Equal(got, want) {
		t.Errorf("upgraded file differs from the embedded template:\n%s", got)
	}
}

func TestUpgradeHarnesses_AddsTheAskRulesAfterTheAllowList(t *testing.T) {
	// An owner's rule keeps the result from being written as the template's bytes.
	s := templateWith(t, askLine, "")
	s = bytes.Replace(s, []byte(`"Bash(crew factory diagram:*)"]`), []byte(`"Bash(crew factory diagram:*)", "Bash(ls:*)"]`), 1)
	dir := writeHarnesses(t, s)
	if _, err := UpgradeHarnesses(dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "harnesses.yaml"))
	_, block, _ := strings.Cut(string(b), "    orchestrator:")
	allow, ask := strings.Index(block, "allow:"), strings.Index(block, "ask:")
	if allow < 0 || ask < allow || strings.Index(block, "deny:") < ask {
		t.Errorf("ask is not right after allow:\n%s", block)
	}
}

func TestUpgradeHarnesses_AnEmptyAskListIsLeftAlone(t *testing.T) {
	s := templateWith(t, askLine, "      ask: []\n")
	dir := writeHarnesses(t, s)
	res, err := UpgradeHarnesses(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertUnchanged(t, dir, s, res)
}

func TestUpgradeHarnesses_ATrimmedAllowListOnACurrentFileIsLeftAlone(t *testing.T) {
	s := templateWith(t, `"Bash(crew run say:*)", `, "")
	dir := writeHarnesses(t, s)
	res, err := UpgradeHarnesses(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertUnchanged(t, dir, s, res)
}

func TestUpgradeHarnesses_TheForemanKeysComment(t *testing.T) {
	const fixtureComment = "# factories' foremen run as the main session"
	const templateComment = "# the orchestrator runs as the main session"
	for _, tc := range []struct{ name, comment, want string }{
		{"the owner's own words are kept", "# my notes on this block", "# my notes on this block"},
		{"one about foremen is replaced", "# FOREMAN for the team", templateComment},
		{"none gets the template's", "", templateComment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := strings.Replace(string(readCutoverFixture(t)), fixtureComment, tc.comment, 1)
			// The owner's extra rule keeps the result from being written as the template's bytes.
			s = strings.Replace(s, sixRuleAllow, strings.Replace(sixRuleAllow, `"Glob"]`, `"Glob", "Bash(ls:*)"]`, 1), 1)
			dir := writeHarnesses(t, []byte(s))
			if _, err := UpgradeHarnesses(dir); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(filepath.Join(dir, "harnesses.yaml"))
			_, line, _ := strings.Cut(string(b), "\n    orchestrator:")
			line, _, _ = strings.Cut(line, "\n")
			if strings.TrimSpace(line) != tc.want {
				t.Errorf("orchestrator: line comment = %q, want %q", strings.TrimSpace(line), tc.want)
			}
		})
	}
}
