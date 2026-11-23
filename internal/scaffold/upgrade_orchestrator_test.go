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

const cutoverFixturePath = "testdata/harnesses-before-cutover.yaml"

const sixRuleAllow = `["Bash(crew mission:*)", "Bash(crew domain list:*)", "Bash(crew factory list:*)", "Read", "Grep", "Glob"]`

func readCutoverFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(cutoverFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func addedFor(added []string, harness string) string {
	for _, a := range added {
		if strings.HasPrefix(a, harness+":") {
			return a
		}
	}
	return ""
}

func TestUpgradeHarnesses_FileFromBeforeTheCutoverBecomesTheTemplate(t *testing.T) {
	fix := readCutoverFixture(t)
	dir := writeHarnesses(t, fix)
	res, err := UpgradeHarnesses(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := tmpl.ReadFile("template/harnesses.yaml")
	got, _ := os.ReadFile(filepath.Join(dir, "harnesses.yaml"))
	if !bytes.Equal(got, want) {
		t.Errorf("upgraded file differs from the embedded template:\n%s", got)
	}
	cc := addedFor(res.Added, "claude-code")
	if !strings.Contains(cc, "foreman: renamed to orchestrator:") || !strings.Contains(cc, "allow rules for plain runs") {
		t.Errorf("claude-code line = %q", cc)
	}
	if addedFor(res.Added, "opencode") == "" || len(res.Added) != 2 {
		t.Errorf("Added = %q", res.Added)
	}
	bak, _ := os.ReadFile(filepath.Join(dir, res.Backup))
	if !bytes.Equal(bak, fix) {
		t.Error("backup does not hold the original bytes")
	}
}

func TestUpgradeHarnesses_KeepsAnAllowRuleTheOwnerAdded(t *testing.T) {
	s := strings.Replace(string(readCutoverFixture(t)), sixRuleAllow, strings.Replace(sixRuleAllow, `"Glob"]`, `"Glob", "Bash(ls:*)"]`, 1), 1)
	dir := writeHarnesses(t, []byte(s))
	if _, err := UpgradeHarnesses(dir); err != nil {
		t.Fatal(err)
	}
	reg, err := config.LoadRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Bash(crew mission:*)", "Bash(crew domain list:*)", "Bash(crew factory list:*)", "Read", "Grep", "Glob", "Bash(ls:*)", "Bash(crew run status:*)", "Bash(crew run say:*)", "Bash(crew run resume:*)", "Bash(crew factory diagram:*)"}
	if got := reg.Harnesses["claude-code"].Orchestrator.Allow; !slices.Equal(got, want) {
		t.Errorf("allow = %q", got)
	}
}

func TestUpgradeHarnesses_RenamesAHarnessTheTemplateDoesNotHave(t *testing.T) {
	_, cursor, _ := strings.Cut(string(readCutoverFixture(t)), "\n  cursor:\n")
	for _, tc := range []struct{ block, static string }{
		{`    foreman: {launch: "mine --agent {name}"}`, ""},
		{`    foreman: {static: {mode: primary}, launch: "x"}`, "primary"},
	} {
		s := string(readCutoverFixture(t)) + "\n  mine:\n" + cursor + tc.block + "\n"
		dir := writeHarnesses(t, []byte(s))
		if _, err := UpgradeHarnesses(dir); err != nil {
			t.Fatal(err)
		}
		reg, err := config.LoadRegistry(dir)
		if err != nil {
			t.Fatal(err)
		}
		mine := reg.Harnesses["mine"]
		if mine.UsesOldKey {
			t.Error("mine still says foreman:")
		}
		if tc.static == "" && mine.Orchestrator != nil {
			t.Errorf("an empty block must be removed, got %+v", mine.Orchestrator)
		}
		if tc.static != "" && (mine.Orchestrator == nil || mine.Orchestrator.Static["mode"] != tc.static || mine.Orchestrator.Launch != "") {
			t.Errorf("Orchestrator = %+v", mine.Orchestrator)
		}
	}
}

func TestUpgradeHarnesses_SecondBackupGetsANumber(t *testing.T) {
	fix := readCutoverFixture(t)
	dir := writeHarnesses(t, fix)
	first := filepath.Join(dir, "harnesses.yaml.before-missions")
	if err := os.WriteFile(first, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := UpgradeHarnesses(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Backup != "harnesses.yaml.before-missions.2" {
		t.Fatalf("Backup = %q", res.Backup)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, res.Backup)); !bytes.Equal(b, fix) {
		t.Error("numbered backup does not hold the original bytes")
	}
	if b, _ := os.ReadFile(first); string(b) != "old" {
		t.Errorf("first backup = %q", b)
	}
}

func TestBackupName_AllNineTakenIsAnError(t *testing.T) {
	dir := t.TempDir()
	names := []string{"harnesses.yaml.before-missions"}
	for i := 2; i <= 9; i++ {
		names = append(names, "harnesses.yaml.before-missions."+string(rune('0'+i)))
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := backupName(dir)
	if err == nil || !strings.Contains(err.Error(), "harnesses.yaml.before-missions.9") {
		t.Errorf("err = %v", err)
	}
}

func TestUpgradeHarnesses_BothKeysIsSkipped(t *testing.T) {
	tb, _ := tmpl.ReadFile("template/harnesses.yaml")
	s := strings.Replace(string(tb), "    orchestrator:  ", "    foreman: {launch: z}\n    orchestrator:  ", 1)
	if s == string(tb) {
		t.Fatal("test setup: marker not found")
	}
	dir := writeHarnesses(t, []byte(s))
	res, err := UpgradeHarnesses(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "claude-code: has both foreman: and orchestrator:; remove foreman:"
	if len(res.Skipped) != 1 || res.Skipped[0] != want {
		t.Errorf("Skipped = %q", res.Skipped)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "harnesses.yaml")); string(got) != s || res.Backup != "" {
		t.Error("the file must not change")
	}
}

func TestUpgradeHarnesses_SecondRunAfterTheCutoverChangesNothing(t *testing.T) {
	dir := writeHarnesses(t, readCutoverFixture(t))
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
	if fi, _ := os.Stat(path); fi.ModTime().After(old.Add(time.Second)) {
		t.Error("file was rewritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "harnesses.yaml.before-missions.2")); err == nil {
		t.Error("a second backup was written")
	}
}
