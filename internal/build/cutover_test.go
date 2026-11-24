package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/build"
	"github.com/crewfile-dev/crew/internal/verify"
)

func TestPlan_WritesNoForemen(t *testing.T) {
	dir, out := setup(t)
	proj := t.TempDir()
	for _, o := range []build.Opts{
		{Out: out},
		{Scope: "project", ProjectDir: proj, Out: out},
	} {
		files, _ := planFiles(t, dir, o)
		for _, f := range files {
			if strings.HasPrefix(f.Name, "foreman-") || strings.HasPrefix(filepath.Base(f.Path), "foreman-") {
				t.Errorf("scope %q: planned %s", o.Scope, f.Path)
			}
		}
	}
}

func TestPlan_NoteWhenAHarnessGetsNoOrchestrator(t *testing.T) {
	dir, out := setup(t)
	edit(t, filepath.Join(dir, "crew.yaml"), "  codex:\n    enabled: false", "  codex:\n    enabled: true")
	for _, a := range []string{"change-reviewer", "code-explorer", "epic-planner", "spike-researcher", "ticket-worker"} {
		edit(t, filepath.Join(dir, "agents", a+".md"), "per_harness:\n", "per_harness:\n  codex: {model: gpt-6-luna, effort: low}\n")
	}
	_, notes := planFilesAllowWarnings(t, dir, build.Opts{Out: out})
	want := "no orchestrator: Codex CLI can't host a main-session agent with shell (crew run and crew mission still work in a terminal)"
	if !hasNote(notes, "codex", want) {
		t.Fatalf("missing %q in %+v", want, notes)
	}

	// Can host (agents, shell, a foreman block) but has no orchestrator launch.
	hy := filepath.Join(dir, "harnesses.yaml")
	edit(t, hy, "    display: Codex CLI\n", "    display: Codex CLI\n    foreman: {launch: x}\n")
	edit(t, hy, "      shell: []\n", "      shell: [shell]\n")
	_, notes = planFilesAllowWarnings(t, dir, build.Opts{Out: out})
	want = "no orchestrator: harnesses.yaml has no orchestrator launch line for Codex CLI (crew run and crew mission still work in a terminal)"
	if !hasNote(notes, "codex", want) {
		t.Fatalf("missing %q in %+v", want, notes)
	}
}

func hasNote(notes []build.Note, hn, msg string) bool {
	for _, n := range notes {
		if n.Harness == hn && n.Msg == msg {
			return true
		}
	}
	return false
}

// compileAndApply plans, diffs and applies; it returns the apply error.
func compileAndApply(t *testing.T, dir string, o build.Opts) error {
	t.Helper()
	p, rep := verify.Run(dir)
	if !rep.OK(false) {
		t.Fatalf("verify: %+v", rep.Issues)
	}
	files, _, err := build.Plan(p, o)
	if err != nil {
		t.Fatal(err)
	}
	acts, err := build.Diff(build.StatePath(dir, o), files, o.Force)
	if err != nil {
		t.Fatal(err)
	}
	return build.Apply(build.StatePath(dir, o), acts)
}

// oldForemanSetup adds an agent named like an old foreman and compiles, the
// way an older crew wrote one. It returns the link, the built file and the
// agent source path.
func oldForemanSetup(t *testing.T) (dir string, o build.Opts, link, built, src string) {
	t.Helper()
	var out string
	dir, out = setup(t)
	o = build.Opts{Out: out}
	b, err := os.ReadFile(filepath.Join(dir, "agents", "code-explorer.md"))
	if err != nil {
		t.Fatal(err)
	}
	src = filepath.Join(dir, "agents", "foreman-ship-change.md")
	if err := os.WriteFile(src, []byte(strings.Replace(string(b), "name: code-explorer", "name: foreman-ship-change", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compileAndApply(t, dir, o); err != nil {
		t.Fatal(err)
	}
	var files []build.File
	files, _ = planFiles(t, dir, o)
	for _, f := range files {
		switch {
		case f.Name == "foreman-ship-change" && f.Harnesses[0] == "claude-code" && f.Kind == "agent":
			link = f.Path
		case f.Name == "foreman-ship-change" && f.Harnesses[0] == "claude-code" && f.Kind == "agent-build":
			built = f.Path
		}
	}
	if link == "" || built == "" {
		t.Fatal("link or built file not planned")
	}
	state := read(t, build.StatePath(dir, o))
	for _, p := range []string{link, built} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(state, p) {
			t.Fatalf("%s not in the ownership record", p)
		}
	}
	return dir, o, link, built, src
}

// snapshot reads every file under root except the old foreman's.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasPrefix(filepath.Base(p), "foreman-") {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			l, _ := os.Readlink(p)
			m[p] = "link:" + l
			return nil
		}
		b, err := os.ReadFile(p)
		m[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompile_RemovesWhatAnOlderCompileWroteForAForeman(t *testing.T) {
	dir, o, link, built, src := oldForemanSetup(t)
	before := snapshot(t, o.Out)

	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if err := compileAndApply(t, dir, o); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{link, built} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists (err %v)", p, err)
		}
	}
	state := read(t, build.StatePath(dir, o))
	for _, p := range []string{link, built} {
		if strings.Contains(state, p) {
			t.Errorf("record still lists %s", p)
		}
	}
	after := snapshot(t, o.Out)
	if len(after) != len(before) {
		t.Fatalf("outputs changed: %d before, %d after", len(before), len(after))
	}
	for p, c := range before {
		if after[p] != c {
			t.Errorf("%s changed", p)
		}
	}
}

func TestCompile_AForemanFileEditedByHandIsNotDeleted(t *testing.T) {
	dir, o, link, built, src := oldForemanSetup(t)
	f, err := os.OpenFile(built, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nmy own line\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	err = compileAndApply(t, dir, o)
	if err == nil || !strings.Contains(err.Error(), built) || !strings.Contains(err.Error(), "edited by hand") {
		t.Fatalf("want an error naming %s and 'edited by hand', got %v", built, err)
	}
	for _, p := range []string{link, built} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}
}
