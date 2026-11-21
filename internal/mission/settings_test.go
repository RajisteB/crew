package mission

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/source"
)

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const validBody = `version: 1
mission_approval: on
orchestrator_harness: claude-code
remind_after: 30m
limits:
  runs_at_once: 1
  delegation_depth: 2
  pieces: 12
  steps: 60
  orchestrator_calls_per_piece: 3
  orchestrator_additions: 2
`

func TestLoadSettings_TemplateDefaults(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "scaffold", "template", SettingsFile))
	if err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(writeSettings(t, string(b)))
	if err != nil {
		t.Fatal(err)
	}
	if s.Limits.Pieces.N != 12 {
		t.Errorf("pieces = %d, want 12", s.Limits.Pieces.N)
	}
	if s.Remind() != 30*time.Minute {
		t.Errorf("remind = %v, want 30m", s.Remind())
	}
	if s.Approval != "on" || s.Harness != "claude-code" {
		t.Errorf("approval=%q harness=%q", s.Approval, s.Harness)
	}
}

func TestLoadSettings_MissingFile(t *testing.T) {
	_, err := LoadSettings(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "missions.yaml not found in") || !strings.Contains(err.Error(), "run crew mission setup") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadSettings_MissingLimitIsError(t *testing.T) {
	body := strings.Replace(validBody, "  pieces: 12\n", "", 1)
	_, err := LoadSettings(writeSettings(t, body))
	want := `limits.pieces: a number or "*" is required`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestLoadSettings_BadApproval(t *testing.T) {
	body := strings.Replace(validBody, "mission_approval: on", "mission_approval: maybe", 1)
	_, err := LoadSettings(writeSettings(t, body))
	if err == nil || !strings.Contains(err.Error(), "mission_approval") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadSettings_UnknownKey(t *testing.T) {
	_, err := LoadSettings(writeSettings(t, validBody+"bogus: 1\n"))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v", err)
	}
}

func TestSettings_ForMergesRepoOverride(t *testing.T) {
	body := validBody + "repos:\n  acme/app:\n    pieces: 20\n"
	s, err := LoadSettings(writeSettings(t, body))
	if err != nil {
		t.Fatal(err)
	}
	got := s.For("acme/app")
	if got.Pieces.N != 20 || got.OrchestratorCalls.N != 3 {
		t.Errorf("got pieces=%d calls=%d", got.Pieces.N, got.OrchestratorCalls.N)
	}
	if other := s.For("acme/other"); other.Pieces.N != 12 {
		t.Errorf("other pieces = %d, want 12", other.Pieces.N)
	}
	if s.Limits.Pieces.N != 12 {
		t.Errorf("For mutated top-level limits")
	}
}

func TestAllows_StarAndNumber(t *testing.T) {
	star := source.Limit{Set: true, Unlimited: true}
	three := source.Limit{Set: true, N: 3}
	if !Allows(star, 1_000_000) {
		t.Error("star should allow anything")
	}
	if !Allows(three, 3) || Allows(three, 4) {
		t.Error("number limit boundary wrong")
	}
}

func loadWith(t *testing.T, body string) error {
	t.Helper()
	_, err := LoadSettings(writeSettings(t, body))
	return err
}

func wantOneAtATime(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "runs_at_once") || !strings.Contains(err.Error(), "one at a time") {
		t.Fatalf("err = %v, want a runs_at_once error saying pieces run one at a time", err)
	}
}

func TestLoadSettings_RunsAtOnceAboveOneIsRefused(t *testing.T) {
	wantOneAtATime(t, loadWith(t, strings.Replace(validBody, "runs_at_once: 1", "runs_at_once: 3", 1)))
}

func TestLoadSettings_RunsAtOnceStarIsRefused(t *testing.T) {
	wantOneAtATime(t, loadWith(t, strings.Replace(validBody, "runs_at_once: 1", `runs_at_once: "*"`, 1)))
}

func TestLoadSettings_RepoOverrideRunsAtOnceAboveOneIsRefused(t *testing.T) {
	wantOneAtATime(t, loadWith(t, validBody+"repos:\n  acme/app:\n    runs_at_once: 2\n"))
}

func TestLoadSettings_RepoOverrideRunsAtOnceOneIsAccepted(t *testing.T) {
	if err := loadWith(t, validBody+"repos:\n  acme/app:\n    runs_at_once: 1\n"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSettings_TemplateRunsOneAtATime(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "scaffold", "template", SettingsFile))
	if err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(writeSettings(t, string(b)))
	if err != nil {
		t.Fatalf("the scaffolded missions.yaml must load: %v", err)
	}
	if s.Limits.Concurrency.N != 1 {
		t.Errorf("runs_at_once = %d, want 1", s.Limits.Concurrency.N)
	}
}

func TestSettings_MergeMethod(t *testing.T) {
	load := func(extra string) (Settings, error) {
		return LoadSettings(writeSettings(t, validBody+extra))
	}
	s, err := load("")
	if err != nil || s.Method() != "squash" {
		t.Fatalf("unset: %v %q", err, s.Method())
	}
	s, err = load("merge_method: rebase\n")
	if err != nil || s.Method() != "rebase" {
		t.Fatalf("rebase: %v %q", err, s.Method())
	}
	if _, err = load("merge_method: fast-forward\n"); err == nil || !strings.Contains(err.Error(), "merge_method") {
		t.Fatalf("fast-forward: %v", err)
	}
}
