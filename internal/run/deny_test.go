package run_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/run"
)

func denyRegistry() *config.Registry {
	return &config.Registry{Harnesses: map[string]config.Harness{
		"fake": {
			Headless: &config.HeadlessSpec{DenyVia: "settings-file"},
			Deny: config.DenySpec{
				All:      []string{"Bash(gh pr merge:*)", "Edit({crew_dir}/**)"},
				Stations: []string{"Bash(crew run:*)"},
			},
		},
		"plain": {},
	}}
}

func TestDenyRules_ExpandsCrewDirAndAddsStationRules(t *testing.T) {
	reg := denyRegistry()
	all := run.DenyRules(reg, "fake", "/cfg/crew", false)
	if want := []string{"Bash(gh pr merge:*)", "Edit(/cfg/crew/**)"}; !slices.Equal(all, want) {
		t.Fatalf("all = %v, want %v", all, want)
	}
	st := run.DenyRules(reg, "fake", "/cfg/crew", true)
	if want := []string{"Bash(gh pr merge:*)", "Edit(/cfg/crew/**)", "Bash(crew run:*)"}; !slices.Equal(st, want) {
		t.Fatalf("station = %v, want %v", st, want)
	}
	if reg.Harnesses["fake"].Deny.All[1] != "Edit({crew_dir}/**)" {
		t.Fatal("DenyRules mutated the registry")
	}
}

func TestCanDeny_FalseWithoutDenyVia(t *testing.T) {
	reg := denyRegistry()
	if !run.CanDeny(reg.Harnesses["fake"]) {
		t.Fatal("settings-file harness should be able to deny")
	}
	if run.CanDeny(reg.Harnesses["plain"]) {
		t.Fatal("harness without headless should not be able to deny")
	}
	if run.CanDeny(config.Harness{Headless: &config.HeadlessSpec{}}) {
		t.Fatal("harness without deny_via should not be able to deny")
	}
	if got := run.DenyRules(reg, "plain", "/x", true); got != nil {
		t.Fatalf("rules for harness without deny_via = %v, want nil", got)
	}
}

func TestHeadlessRunner_WritesSettingsFileAndRemovesIt(t *testing.T) {
	t.Setenv("FAKE_MODE", "copysettings")
	copyTo := filepath.Join(t.TempDir(), "copy.json")
	t.Setenv("FAKE_COPY_TO", copyTo)
	reg := fakeRegistry(t, fakeHarnessPath(t), "--agent", "{agent}", "--settings", "{settings}", "--mode", "dontAsk")
	h := reg.Harnesses["fake"]
	h.Headless.DenyVia = "settings-file"
	r := req("p")
	r.Deny = []string{"Bash(gh pr merge:*)", `Edit(/a"b/**)`}
	if _, err := (run.HeadlessRunner{Registry: reg}).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(copyTo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Bash(gh pr merge:*)") || !strings.Contains(string(body), `"deny"`) {
		t.Fatalf("settings file content: %s", body)
	}
	pathBytes, err := os.ReadFile(copyTo + ".path")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(strings.TrimSpace(string(pathBytes))); !os.IsNotExist(err) {
		t.Fatalf("settings file still exists after the station exited (err=%v)", err)
	}
	if mode := strings.TrimSpace(readFile(t, copyTo+".mode")); mode != "0600" {
		t.Fatalf("settings file mode = %s, want 0600", mode)
	}
}

func TestHeadlessRunner_RemovesSettingsFileOnFailure(t *testing.T) {
	t.Setenv("FAKE_MODE", "copysettings")
	t.Setenv("FAKE_EXIT", "3")
	copyTo := filepath.Join(t.TempDir(), "copy.json")
	t.Setenv("FAKE_COPY_TO", copyTo)
	reg := fakeRegistry(t, fakeHarnessPath(t), "--agent", "{agent}", "--settings", "{settings}")
	reg.Harnesses["fake"].Headless.DenyVia = "settings-file"
	r := req("p")
	r.Deny = []string{"Bash(gh pr merge:*)"}
	if _, err := (run.HeadlessRunner{Registry: reg}).Run(context.Background(), r); err == nil {
		t.Fatal("expected the station to fail")
	}
	pathBytes, _ := os.ReadFile(copyTo + ".path")
	if _, err := os.Stat(strings.TrimSpace(string(pathBytes))); !os.IsNotExist(err) {
		t.Fatalf("settings file still exists after a failed station (err=%v)", err)
	}
}

func TestHeadlessRunner_RefusesDenyRulesItCannotApply(t *testing.T) {
	reg := fakeRegistry(t, fakeHarnessPath(t), "--agent", "{agent}")
	r := req("p")
	r.Deny = []string{"Bash(gh pr merge:*)"}
	_, err := run.HeadlessRunner{Registry: reg}.Run(context.Background(), r)
	if err == nil || !strings.Contains(err.Error(), "deny") {
		t.Fatalf("want an error about deny rules, got %v", err)
	}
}

func TestEngine_StationRequestCarriesDenyRules(t *testing.T) {
	e, fr := newEngine(t, engineFactory(), reply("done"), reply("done"), reply("done"))
	e.Registry = denyRegistry()
	e.CrewDir = "/cfg/crew"
	drive(t, e)
	if len(fr.reqs) == 0 {
		t.Fatal("no station ran")
	}
	want := []string{"Bash(gh pr merge:*)", "Edit(/cfg/crew/**)", "Bash(crew run:*)"}
	if got := fr.reqs[0].Deny; !slices.Equal(got, want) {
		t.Fatalf("Deny = %v, want %v", got, want)
	}
}
