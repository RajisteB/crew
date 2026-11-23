package build_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/build"
	"github.com/crewfile-dev/crew/internal/scaffold"
	"github.com/crewfile-dev/crew/internal/verify"
)

func setup(t *testing.T) (dir, out string) {
	t.Helper()
	root := t.TempDir()
	dir, out = filepath.Join(root, "lo"), filepath.Join(root, "out")
	if _, err := scaffold.Init(dir); err != nil {
		t.Fatal(err)
	}
	return dir, out
}

func compile(t *testing.T, dir string, o build.Opts) []build.Action {
	t.Helper()
	p, rep := verify.Run(dir)
	if !rep.OK(false) {
		t.Fatalf("verify failed: %+v", rep.Issues)
	}
	files, _, err := build.Plan(p, o)
	if err != nil {
		t.Fatal(err)
	}
	acts, err := build.Diff(build.StatePath(dir, o), files, o.Force)
	if err != nil {
		t.Fatal(err)
	}
	return acts
}

func count(acts []build.Action, k build.ActionKind) int {
	n := 0
	for _, a := range acts {
		if a.Kind == k {
			n++
		}
	}
	return n
}

func TestScaffoldVerifiesClean(t *testing.T) {
	dir, _ := setup(t)
	_, rep := verify.Run(dir)
	if rep.Count(verify.Error)+rep.Count(verify.Warning) != 0 {
		t.Fatalf("template should verify clean: %+v", rep.Issues)
	}
}

func TestCompileIsIdempotentAndSafe(t *testing.T) {
	dir, out := setup(t)
	o := build.Opts{Scope: "user", Out: out}
	state := build.StatePath(dir, o)

	acts := compile(t, dir, o)
	if count(acts, build.Create) == 0 {
		t.Fatal("expected creates")
	}
	if err := build.Apply(state, acts); err != nil {
		t.Fatal(err)
	}
	if acts = compile(t, dir, o); count(acts, build.Unchanged) != len(acts) {
		t.Fatalf("second compile should be a no-op: %+v", acts)
	}

	// Hand-edit an output: must conflict, and Apply must refuse.
	agent := filepath.Join(out, "home/.claude/agents/code-explorer.md")
	if err := os.WriteFile(agent, []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	acts = compile(t, dir, o)
	if count(acts, build.Conflict) != 1 {
		t.Fatalf("expected 1 conflict, got %+v", acts)
	}
	if err := build.Apply(state, acts); err == nil {
		t.Fatal("apply should refuse on conflict")
	}
	if b, _ := os.ReadFile(agent); string(b) != "hand edit" {
		t.Fatal("conflicting file must not be touched")
	}

	// --force takes it back.
	o.Force = true
	if err := build.Apply(state, compile(t, dir, o)); err != nil {
		t.Fatal(err)
	}
	o.Force = false

	// A foreign file in an output location is never overwritten silently.
	foreign := filepath.Join(out, "home/.claude/agents/epic-planner.md")
	os.Remove(foreign)
	os.WriteFile(foreign, []byte("someone else's"), 0o644)
	os.Remove(state) // forget ownership entirely
	if acts = compile(t, dir, o); count(acts, build.Conflict) == 0 {
		t.Fatal("foreign file must conflict")
	}
	o.Force = true
	must(t, build.Apply(state, compile(t, dir, o)))
	o.Force = false

	// Removing a source prunes its outputs (and empty skill dirs).
	os.Remove(filepath.Join(dir, "agents/proposal-writer.md"))
	os.RemoveAll(filepath.Join(dir, "skills/pr-description"))
	tw := filepath.Join(dir, "agents/ticket-worker.md")
	b, _ := os.ReadFile(tw)
	os.WriteFile(tw, []byte(strings.Replace(string(b), ", pr-description]", "]", 1)), 0o644)
	acts = compile(t, dir, o)
	if count(acts, build.Delete) != 5 { // agent link + built file, for Claude Code and OpenCode, plus the skill link
		t.Fatalf("expected deletes, got %+v", acts)
	}
	if err := build.Apply(state, acts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(out, "home/.claude/agents/proposal-writer.md")); !os.IsNotExist(err) {
		t.Fatal("pruned agent still exists")
	}
	if _, err := os.Stat(filepath.Join(out, "home/.claude/skills/pr-description")); !os.IsNotExist(err) {
		t.Fatal("empty skill dir should be removed")
	}
}

func edit(t *testing.T, path, old, new string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantAll(t *testing.T, s string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("missing %q in:\n%s", w, s)
		}
	}
}

func TestModelAndEffortPerHarness(t *testing.T) {
	dir, out := setup(t)
	o := build.Opts{Scope: "user", Out: out}
	if err := build.Apply(build.StatePath(dir, o), compile(t, dir, o)); err != nil {
		t.Fatal(err)
	}
	cc := read(t, filepath.Join(out, "home/.claude/agents/ticket-worker.md"))
	wantAll(t, cc, "model: claude-opus-5-5\n", "effort: high\n")
	oc := read(t, filepath.Join(out, "home/.config/opencode/agents/ticket-worker.md"))
	wantAll(t, oc, "model: opencode-go/kimi-k3.1-pro#high\n", "mode: subagent\n")
	if strings.Contains(oc, "effort:") {
		t.Errorf("OpenCode carries effort in the model variant, not a key:\n%s", oc)
	}
}

func TestInheritWritesNoModel(t *testing.T) {
	dir, out := setup(t)
	edit(t, filepath.Join(dir, "agents/code-explorer.md"), "model: claude-haiku-5-5\neffort: low\n", "model: inherit\n")
	o := build.Opts{Scope: "user", Out: out}
	if err := build.Apply(build.StatePath(dir, o), compile(t, dir, o)); err != nil {
		t.Fatal(err)
	}
	cc := read(t, filepath.Join(out, "home/.claude/agents/code-explorer.md"))
	if strings.Contains(cc, "model:") || strings.Contains(cc, "effort:") {
		t.Errorf("inherit should write neither model nor effort:\n%s", cc)
	}
	oc := read(t, filepath.Join(out, "home/.config/opencode/agents/code-explorer.md"))
	wantAll(t, oc, "model: opencode-go/deepseek-v4.1-flash#low\n") // per_harness still wins
}

func TestCodexTOMLShape(t *testing.T) {
	dir, out := setup(t)
	edit(t, filepath.Join(dir, "crew.yaml"), "  codex:\n    enabled: false\n    agents: [\"*\"]", "  codex:\n    enabled: true\n    agents: [code-explorer]")
	edit(t, filepath.Join(dir, "agents/code-explorer.md"), "per_harness:\n", "per_harness:\n  codex: {model: gpt-6-luna, effort: low}\n")
	o := build.Opts{Scope: "user", Out: out}
	must(t, build.Apply(build.StatePath(dir, o), compile(t, dir, o)))
	s := read(t, filepath.Join(out, "home/.codex/agents/code-explorer.toml"))
	wantAll(t, s, `name = "code-explorer"`, `model = "gpt-6-luna"`, `sandbox_mode = "read-only"`, `model_reasoning_effort = "low"`, "developer_instructions = '''\n")
}

func TestProjectScope(t *testing.T) {
	dir, _ := setup(t)
	proj := t.TempDir()
	o := build.Opts{Scope: "project", ProjectDir: proj}
	if err := build.Apply(build.StatePath(dir, o), compile(t, dir, o)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".claude/agents/ticket-worker.md", ".claude/skills/ticket-intake/SKILL.md", ".opencode/agents/ticket-worker.md"} {
		if _, err := os.Stat(filepath.Join(proj, p)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(proj, ".opencode/skills")); !os.IsNotExist(err) {
		t.Fatal("OpenCode reads .claude/skills, so crew must not write a second copy")
	}
}

func lstatLink(t *testing.T, path string) string {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s should be a link", path)
	}
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestUserScopeLinksIntoCrew(t *testing.T) {
	dir, out := setup(t)
	o := build.Opts{Scope: "user", Out: out}
	if err := build.Apply(build.StatePath(dir, o), compile(t, dir, o)); err != nil {
		t.Fatal(err)
	}
	if got := lstatLink(t, filepath.Join(out, "home/.claude/skills/pr-description")); got != filepath.Join(dir, "skills/pr-description") {
		t.Fatalf("skill link points at %s", got)
	}
	agent := lstatLink(t, filepath.Join(out, "home/.claude/agents/ticket-worker.md"))
	if !strings.HasPrefix(agent, filepath.Join(out, "crew-build")+string(filepath.Separator)) {
		t.Fatalf("agent link should point into the build dir, got %s", agent)
	}
	// A skill edit is live with no compile.
	edit(t, filepath.Join(dir, "skills/pr-description/SKILL.md"), "Keep it under 200 words.", "Keep it short.")
	wantAll(t, read(t, filepath.Join(out, "home/.claude/skills/pr-description/SKILL.md")), "Keep it short.")
}

func TestSkillSupportingFiles(t *testing.T) {
	dir, out := setup(t)
	sk := filepath.Join(dir, "skills/pr-description")
	os.MkdirAll(filepath.Join(sk, "scripts"), 0o755)
	os.WriteFile(filepath.Join(sk, "scripts/check.sh"), []byte("echo ok\n"), 0o755)
	edit(t, filepath.Join(sk, "SKILL.md"), "description: ", "license: MIT\ndescription: ")

	// user scope: the link carries every file
	o := build.Opts{Scope: "user", Out: out}
	if err := build.Apply(build.StatePath(dir, o), compile(t, dir, o)); err != nil {
		t.Fatal(err)
	}
	wantAll(t, read(t, filepath.Join(out, "home/.claude/skills/pr-description/scripts/check.sh")), "echo ok")

	// project scope: real copies of every file, frontmatter untouched
	proj := t.TempDir()
	po := build.Opts{Scope: "project", ProjectDir: proj}
	if err := build.Apply(build.StatePath(dir, po), compile(t, dir, po)); err != nil {
		t.Fatal(err)
	}
	wantAll(t, read(t, filepath.Join(proj, ".claude/skills/pr-description/scripts/check.sh")), "echo ok")
	wantAll(t, read(t, filepath.Join(proj, ".claude/skills/pr-description/SKILL.md")), "license: MIT")
	if fi, _ := os.Lstat(filepath.Join(proj, ".claude/skills/pr-description")); fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("project scope must write real files, not links")
	}
}

func TestCopiedOutputsMigrateToLinks(t *testing.T) {
	dir, out := setup(t)
	o := build.Opts{Scope: "user", Out: out}
	state := build.StatePath(dir, o)
	if err := build.Apply(state, compile(t, dir, o)); err != nil {
		t.Fatal(err)
	}
	// Simulate the old copy-based compile: real files recorded as owned.
	skillDir := filepath.Join(out, "home/.claude/skills/ticket-intake")
	agent := filepath.Join(out, "home/.claude/agents/code-explorer.md")
	skillBody := read(t, filepath.Join(skillDir, "SKILL.md"))
	agentBody := read(t, agent)
	os.Remove(skillDir)
	os.Remove(agent)
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillBody), 0o644)
	os.WriteFile(agent, []byte(agentBody), 0o644)
	build.SeedStateForTest(t, state, map[string][]byte{
		filepath.Join(skillDir, "SKILL.md"): []byte(skillBody),
		agent:                               []byte(agentBody),
	})

	acts := compile(t, dir, o)
	if n := count(acts, build.Conflict); n != 0 {
		t.Fatalf("owned copies should be replaced, not conflict: %+v", acts)
	}
	if err := build.Apply(state, acts); err != nil {
		t.Fatal(err)
	}
	lstatLink(t, skillDir)
	lstatLink(t, agent)
}

func TestForeignSkillFolderConflicts(t *testing.T) {
	dir, out := setup(t)
	o := build.Opts{Scope: "user", Out: out}
	mine := filepath.Join(out, "home/.claude/skills/pr-description")
	os.MkdirAll(mine, 0o755)
	os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte("someone else's"), 0o644)
	acts := compile(t, dir, o)
	if count(acts, build.Conflict) != 1 {
		t.Fatalf("a skill folder crew didn't write must conflict: %+v", acts)
	}
}

func verifyRun(t *testing.T, dir string) (*verify.Project, *verify.Report) {
	t.Helper()
	p, rep := verify.Run(dir)
	if !rep.OK(false) {
		t.Fatalf("verify failed: %+v", rep.Issues)
	}
	return p, rep
}

func TestBuildPlan_WritesDiagram(t *testing.T) {
	dir, out := setup(t)
	p, rep := verify.Run(dir)
	if !rep.OK(false) {
		t.Fatalf("verify: %+v", rep.Issues)
	}
	files, _, err := build.Plan(p, build.Opts{Out: out})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Kind == "factory-diagram" {
			if !strings.HasSuffix(f.Path, filepath.Join("factories", "ship-change.md")) {
				t.Fatalf("diagram path = %s", f.Path)
			}
			s := string(f.Content)
			if !strings.Contains(s, "```mermaid\nflowchart LR\n") || !strings.HasPrefix(s, "# ship-change\n") {
				t.Fatalf("diagram content:\n%s", s)
			}
			return
		}
	}
	t.Fatal("no factory-diagram file planned")
}

func planFiles(t *testing.T, dir string, o build.Opts) ([]build.File, []build.Note) {
	t.Helper()
	p, rep := verify.Run(dir)
	if !rep.OK(false) {
		t.Fatalf("verify: %+v", rep.Issues)
	}
	files, notes, err := build.Plan(p, o)
	if err != nil {
		t.Fatal(err)
	}
	return files, notes
}

func planFilesAllowWarnings(t *testing.T, dir string, o build.Opts) ([]build.File, []build.Note) {
	t.Helper()
	p, rep := verify.Run(dir)
	if !rep.OK(true) {
		t.Fatalf("verify: %+v", rep.Issues)
	}
	files, notes, err := build.Plan(p, o)
	if err != nil {
		t.Fatal(err)
	}
	return files, notes
}

func TestRender_DoesNotMutateHarnessStatic(t *testing.T) {
	dir, out := setup(t)
	p, _ := verify.Run(dir)
	before := fmt.Sprint(p.Registry.Harnesses["opencode"].Agents.Static)
	if _, _, err := build.Plan(p, build.Opts{Out: out}); err != nil {
		t.Fatal(err)
	}
	if after := fmt.Sprint(p.Registry.Harnesses["opencode"].Agents.Static); after != before {
		t.Fatalf("Agents.Static changed: %s → %s", before, after)
	}
	for _, f := range mustPlan(t, p, out) {
		if f.Name == "code-explorer" && f.Link == "" && f.Harnesses[0] == "opencode" && strings.Contains(string(f.Content), "mode: primary") {
			t.Fatal("regular agents must stay subagents")
		}
	}
}

func mustPlan(t *testing.T, p *verify.Project, out string) []build.File {
	t.Helper()
	files, _, err := build.Plan(p, build.Opts{Out: out})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
