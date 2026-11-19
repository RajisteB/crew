package source_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/source"
)

const shipChange = `---
name: ship-change
description: Plans, builds and reviews one change.
start: plan
harness: claude-code
stations:
  plan:
    agent: epic-planner
    routes:
      ready: build
  build:
    agent: implementer
    on_drift: plan
    timeout: 90m
    routes:
      done: review
      replan: {to: plan, max: 2}
  review:
    agent: reviewer
    approve: true
    routes:
      pass: done
      fix: {to: build, max: "*"}
      redesign: {to: plan, max: 1}
---
Briefing text.
`

func writeFactory(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, source.FactoriesDir)
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "ship-change.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func loadOne(t *testing.T, content string) (source.Factory, error) {
	t.Helper()
	fs, errs := source.LoadFactories(writeFactory(t, content))
	if len(errs) > 0 {
		return source.Factory{}, errs[0]
	}
	if len(fs) != 1 {
		t.Fatalf("got %d factories", len(fs))
	}
	return fs[0], nil
}

func TestParseFactory_ShortAndLongRoutes(t *testing.T) {
	f, err := loadOne(t, shipChange)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "ship-change" || f.Start != "plan" || f.Path != filepath.Join("factories", "ship-change.md") {
		t.Fatalf("header = %+v", f)
	}
	if f.Body != "Briefing text.\n" {
		t.Fatalf("body = %q", f.Body)
	}
	if r := f.Stations["plan"].Routes["ready"]; r.To != "build" || r.Max.Set {
		t.Fatalf("short route = %+v", r)
	}
	if r := f.Stations["build"].Routes["replan"]; r.To != "plan" || !r.Max.Set || r.Max.Unlimited || r.Max.N != 2 {
		t.Fatalf("numeric max = %+v", r)
	}
	if r := f.Stations["review"].Routes["fix"]; r.To != "build" || !r.Max.Set || !r.Max.Unlimited {
		t.Fatalf("star max = %+v", r)
	}
	if !f.Stations["review"].Approve || f.Stations["build"].OnDrift != "plan" {
		t.Fatalf("station flags = %+v", f.Stations)
	}
	if got := f.Stations["review"].Routes["fix"].Max.String(); got != "*" {
		t.Fatalf("Limit.String() = %q", got)
	}
}

func TestParseFactory_BadLimit(t *testing.T) {
	for _, bad := range []string{"0", "-1", `"lots"`, "1.5"} {
		content := strings.Replace(shipChange, "max: 2}", "max: "+bad+"}", 1)
		_, err := loadOne(t, content)
		if err == nil || !strings.Contains(err.Error(), `max must be a positive number or "*"`) {
			t.Errorf("max %s: got %v", bad, err)
		}
	}
}

func TestParseFactory_UnquotedStarHint(t *testing.T) {
	content := strings.Replace(shipChange, `max: "*"}`, `max: *}`, 1)
	_, err := loadOne(t, content)
	if err == nil || !strings.Contains(err.Error(), `"*" in quotes`) {
		t.Fatalf("want a hint to quote *, got %v", err)
	}
}

func TestParseFactory_UnknownKey(t *testing.T) {
	content := strings.Replace(shipChange, "    agent: reviewer\n", "    agent: reviewer\n    colour: red\n", 1)
	_, err := loadOne(t, content)
	if err == nil || !strings.Contains(err.Error(), `unknown key "colour"`) {
		t.Fatalf("got %v", err)
	}
	content = strings.Replace(shipChange, "{to: plan, max: 2}", "{to: plan, max: 2, when: never}", 1)
	_, err = loadOne(t, content)
	if err == nil || !strings.Contains(err.Error(), `unknown key "when"`) {
		t.Fatalf("route key: got %v", err)
	}
}

func TestStationNames_StartFirst(t *testing.T) {
	f, err := loadOne(t, shipChange)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(f.StationNames(), ",")
	if got != "plan,build,review" {
		t.Fatalf("StationNames = %s", got)
	}
}

func TestHarnessFor(t *testing.T) {
	f, _ := loadOne(t, strings.Replace(shipChange, "    agent: reviewer\n", "    agent: reviewer\n    harness: opencode\n", 1))
	if f.HarnessFor("review") != "opencode" || f.HarnessFor("plan") != "claude-code" {
		t.Fatalf("HarnessFor wrong: %q %q", f.HarnessFor("review"), f.HarnessFor("plan"))
	}
}

func TestTimeoutFor_Defaults(t *testing.T) {
	f, err := loadOne(t, shipChange)
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := f.TimeoutFor("plan"); d != 60*time.Minute {
		t.Fatalf("default timeout = %v", d)
	}
	if d, _ := f.TimeoutFor("build"); d != 90*time.Minute {
		t.Fatalf("station timeout = %v", d)
	}
	f.Timeout = "5m"
	if d, _ := f.TimeoutFor("plan"); d != 5*time.Minute {
		t.Fatalf("factory timeout = %v", d)
	}
	f.Timeout = "abc"
	if _, err := f.TimeoutFor("plan"); err == nil {
		t.Fatal("want error for bad duration")
	}
}

func TestWatchRoots_Default(t *testing.T) {
	f, _ := loadOne(t, shipChange)
	if got := f.WatchRoots(); len(got) != 1 || got[0] != "." {
		t.Fatalf("WatchRoots = %v", got)
	}
}

func TestParseFactory_FileDirect(t *testing.T) {
	root := writeFactory(t, shipChange)
	f, err := source.ParseFactory(filepath.Join(root, "factories", "ship-change.md"))
	if err != nil || f.Name != "ship-change" || len(f.Stations) != 3 {
		t.Fatalf("ParseFactory = %+v, %v", f, err)
	}
}
