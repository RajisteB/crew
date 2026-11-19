package migrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/build"
	"github.com/crewfile-dev/crew/internal/migrate"
	"github.com/crewfile-dev/crew/internal/scaffold"
	"github.com/crewfile-dev/crew/internal/verify"
)

type entry struct {
	SHA256    string   `json:"sha256"`
	Kind      string   `json:"kind"`
	Harnesses []string `json:"harnesses"`
	Root      string   `json:"root,omitempty"`
}

type state struct {
	Version int              `json:"version"`
	Files   map[string]entry `json:"files"`
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, _ := json.MarshalIndent(v, "", "  ")
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readState(t *testing.T, path string) state {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s state
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func run(t *testing.T, from, to string) {
	t.Helper()
	steps, err := migrate.Plan(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate.Apply(steps); err != nil {
		t.Fatal(err)
	}
}

func TestMigrate_RewritesStateKeysNotFingerprints(t *testing.T) {
	root := t.TempDir()
	from, to, home := filepath.Join(root, "crewfile"), filepath.Join(root, "crew"), filepath.Join(root, "home")
	os.MkdirAll(from, 0o755)
	os.WriteFile(filepath.Join(from, "crewfile.yaml"), []byte("version: 1\n"), 0o644)
	built := filepath.Join(from, "build", "claude-code", "agents", "x.md")
	link := filepath.Join(home, ".claude", "agents", "x.md")
	writeJSON(t, filepath.Join(from, ".crewfile", "state.user.json"), state{Version: 1, Files: map[string]entry{
		link:  {SHA256: "link:" + built, Kind: "agent", Root: filepath.Join(home, ".claude", "agents")},
		built: {SHA256: "abc", Kind: "agent-build", Root: filepath.Dir(built)},
	}})

	run(t, from, to)

	if _, err := os.Stat(filepath.Join(to, "crew.yaml")); err != nil {
		t.Fatalf("crew.yaml missing after migrate: %v", err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("old dir still exists")
	}
	st := readState(t, filepath.Join(to, ".crew", "state.user.json"))
	if got := st.Files[link].SHA256; got != "link:"+built {
		t.Fatalf("link fingerprint changed to %q; it must still match the link on disk", got)
	}
	newBuilt := filepath.Join(to, "build", "claude-code", "agents", "x.md")
	e, ok := st.Files[newBuilt]
	if !ok {
		t.Fatalf("build entry key not rewritten; keys: %v", keys(st.Files))
	}
	if e.Root != filepath.Dir(newBuilt) || e.SHA256 != "abc" {
		t.Fatalf("build entry = %+v, want root under new dir and sha unchanged", e)
	}
	if _, ok := st.Files[built]; ok {
		t.Fatalf("old build key still present")
	}
}

func TestMigrate_ThenCompileHasNoConflicts(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	from, to := filepath.Join(root, "crewfile"), filepath.Join(root, "crew")
	if _, err := scaffold.Init(from); err != nil {
		t.Fatal(err)
	}
	applyCompile(t, from)
	// Turn the fresh layout into the old one: crewfile.yaml + .crewfile/.
	must(t, os.Rename(filepath.Join(from, "crew.yaml"), filepath.Join(from, "crewfile.yaml")))
	must(t, os.Rename(filepath.Join(from, ".crew"), filepath.Join(from, ".crewfile")))

	run(t, from, to)

	acts := plan(t, to)
	links := 0
	for _, a := range acts {
		if a.Kind == build.Conflict {
			t.Errorf("conflict after migrate: %s — %s", a.Path, a.Reason)
		}
		if a.File != nil && a.File.Link != "" {
			links++
			if !strings.HasPrefix(a.File.Link, to+string(filepath.Separator)) {
				t.Errorf("link %s targets %s, want under %s", a.Path, a.File.Link, to)
			}
		}
	}
	if links == 0 {
		t.Fatal("expected link outputs in user scope")
	}
	if err := build.Apply(build.StatePath(to, build.Opts{}), acts); err != nil {
		t.Fatal(err)
	}
	for _, a := range plan(t, to) {
		if a.Kind != build.Unchanged {
			t.Errorf("second compile not clean: %s %s", a.Kind, a.Path)
		}
	}
}

func TestMigrate_RefusesExistingTarget(t *testing.T) {
	root := t.TempDir()
	from, to := filepath.Join(root, "crewfile"), filepath.Join(root, "crew")
	os.MkdirAll(from, 0o755)
	os.WriteFile(filepath.Join(from, "crewfile.yaml"), []byte("version: 1\n"), 0o644)
	os.MkdirAll(to, 0o755)
	if _, err := migrate.Plan(from, to); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want 'already exists' error, got %v", err)
	}
}

func TestMigrate_RefusesNonCrewfileSource(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "random")
	os.MkdirAll(from, 0o755)
	if _, err := migrate.Plan(from, filepath.Join(root, "crew")); err == nil || !strings.Contains(err.Error(), "crewfile.yaml") {
		t.Fatalf("want error naming crewfile.yaml, got %v", err)
	}
}

func applyCompile(t *testing.T, dir string) {
	t.Helper()
	acts := plan(t, dir)
	if err := build.Apply(build.StatePath(dir, build.Opts{}), acts); err != nil {
		t.Fatal(err)
	}
}

func plan(t *testing.T, dir string) []build.Action {
	t.Helper()
	p, rep := verify.Run(dir)
	if !rep.OK(true) {
		t.Fatalf("verify failed: %+v", rep.Issues)
	}
	files, _, err := build.Plan(p, build.Opts{})
	if err != nil {
		t.Fatal(err)
	}
	acts, err := build.Diff(build.StatePath(dir, build.Opts{}), files, false)
	if err != nil {
		t.Fatal(err)
	}
	return acts
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func keys(m map[string]entry) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
