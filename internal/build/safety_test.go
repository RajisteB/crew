package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/build"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// oldCopy turns the linked skill into a copied folder recorded as owned,
// the way the earlier copy-based compile left it.
func oldCopy(t *testing.T, dir, out, state, name string) string {
	t.Helper()
	skillDir := filepath.Join(out, "home/.claude/skills", name)
	body, err := os.ReadFile(filepath.Join(dir, "skills", name, "SKILL.md"))
	must(t, err)
	must(t, os.Remove(skillDir))
	must(t, os.MkdirAll(skillDir, 0o755))
	must(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), body, 0o644))
	must(t, os.Remove(state))
	build.SeedStateForTest(t, state, map[string][]byte{filepath.Join(skillDir, "SKILL.md"): body})
	return skillDir
}

func linkedUser(t *testing.T) (dir, out, state string, o build.Opts) {
	t.Helper()
	dir, out = setup(t)
	o = build.Opts{Scope: "user", Out: out}
	state = build.StatePath(dir, o)
	must(t, build.Apply(state, compile(t, dir, o)))
	return dir, out, state, o
}

func sourceSurvives(t *testing.T, dir, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "skills", name, "SKILL.md")); err != nil {
		t.Fatalf("source skills/%s/SKILL.md was deleted: %v", name, err)
	}
}

// Review finding 1: a run that fails after migrating a skill to a link must
// not make the next run delete the source through that link.
func TestFailedApplyThenRerunKeepsSource(t *testing.T) {
	dir, out, state, o := linkedUser(t)
	skillDir := oldCopy(t, dir, out, state, "pr-description")

	ro := filepath.Join(out, "crew-build")
	must(t, os.RemoveAll(ro))
	must(t, os.MkdirAll(ro, 0o555)) // a later write fails, standing in for any I/O error
	if err := build.Apply(state, compile(t, dir, o)); err == nil {
		t.Fatal("expected the first apply to fail")
	}
	must(t, os.Chmod(ro, 0o755))

	acts := compile(t, dir, o)
	if n := count(acts, build.Conflict); n != 0 {
		t.Fatalf("rerun should be clean, got %+v", acts)
	}
	must(t, build.Apply(state, acts))
	sourceSurvives(t, dir, "pr-description")
	lstatLink(t, skillDir)
	if acts := compile(t, dir, o); count(acts, build.Unchanged) != len(acts) {
		t.Fatalf("third run should be a no-op: %+v", acts)
	}
}

// A stale record below a link is forgotten, never deleted.
func TestStaleRecordUnderLinkIsForgotten(t *testing.T) {
	dir, out, state, o := linkedUser(t)
	link := filepath.Join(out, "home/.claude/skills/pr-description")
	body, _ := os.ReadFile(filepath.Join(dir, "skills/pr-description/SKILL.md"))
	build.SeedStateForTest(t, state, map[string][]byte{filepath.Join(link, "SKILL.md"): body})

	acts := compile(t, dir, o)
	if count(acts, build.Delete) != 0 || count(acts, build.Forget) != 1 {
		t.Fatalf("want one forget and no deletes, got %+v", acts)
	}
	o.Force = true
	must(t, build.Apply(state, compile(t, dir, o))) // not even --force deletes through the link
	sourceSurvives(t, dir, "pr-description")
	lstatLink(t, link)
}

// Review finding 2: --force can't replace a folder holding files crew
// doesn't own, so it must say so up front instead of failing halfway.
func TestForceDoesNotReplaceForeignFolder(t *testing.T) {
	dir, out, state, o := linkedUser(t)
	mine := filepath.Join(out, "home/.claude/skills/ticket-intake")
	must(t, os.Remove(mine))
	must(t, os.MkdirAll(mine, 0o755))
	must(t, os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte("mine"), 0o644))
	o.Force = true
	acts := compile(t, dir, o)
	if count(acts, build.Conflict) != 1 {
		t.Fatalf("want a conflict even with --force, got %+v", acts)
	}
	if err := build.Apply(state, acts); err == nil {
		t.Fatal("apply must refuse")
	}
	if got := read(t, filepath.Join(mine, "SKILL.md")); got != "mine" {
		t.Fatalf("user folder touched: %q", got)
	}
}

// Review finding 7: macOS .DS_Store doesn't block migrating an old copy.
func TestDSStoreDoesNotBlockMigration(t *testing.T) {
	dir, out, state, o := linkedUser(t)
	skillDir := oldCopy(t, dir, out, state, "pr-description")
	must(t, os.WriteFile(filepath.Join(skillDir, ".DS_Store"), []byte("x"), 0o644))
	acts := compile(t, dir, o)
	if n := count(acts, build.Conflict); n != 0 {
		t.Fatalf("unexpected conflicts: %+v", acts)
	}
	must(t, build.Apply(state, acts))
	lstatLink(t, skillDir)
	sourceSurvives(t, dir, "pr-description")
}

// Review finding 5: project scope never writes through a skill folder the
// user linked elsewhere, even with --force.
func TestProjectScopeNeverWritesThroughLinkedFolder(t *testing.T) {
	dir, _ := setup(t)
	proj := t.TempDir()
	elsewhere := t.TempDir()
	must(t, os.WriteFile(filepath.Join(elsewhere, "SKILL.md"), []byte("theirs"), 0o644))
	must(t, os.MkdirAll(filepath.Join(proj, ".claude/skills"), 0o755))
	must(t, os.Symlink(elsewhere, filepath.Join(proj, ".claude/skills/pr-description")))
	po := build.Opts{Scope: "project", ProjectDir: proj, Force: true}
	acts := compile(t, dir, po)
	found := false
	for _, a := range acts {
		if a.Kind == build.Conflict && strings.Contains(a.Reason, "is a link") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a link conflict, got %+v", acts)
	}
	if got := read(t, filepath.Join(elsewhere, "SKILL.md")); got != "theirs" {
		t.Fatalf("wrote outside the project: %q", got)
	}
}

// Review finding 6: in project scope a linked file inside the skill folder
// is copied; one pointing outside it is refused.
func TestProjectScopeLinkedSkillFiles(t *testing.T) {
	dir, _ := setup(t)
	sk := filepath.Join(dir, "skills/pr-description")
	must(t, os.Rename(filepath.Join(sk, "SKILL.md"), filepath.Join(sk, "real.md")))
	must(t, os.Symlink(filepath.Join(sk, "real.md"), filepath.Join(sk, "SKILL.md")))
	proj := t.TempDir()
	po := build.Opts{Scope: "project", ProjectDir: proj}
	must(t, build.Apply(build.StatePath(dir, po), compile(t, dir, po)))
	wantAll(t, read(t, filepath.Join(proj, ".claude/skills/pr-description/SKILL.md")), "name: pr-description")

	secret := filepath.Join(t.TempDir(), "secret")
	must(t, os.WriteFile(secret, []byte("token"), 0o600))
	must(t, os.Symlink(secret, filepath.Join(sk, "notes.md")))
	p, rep := verifyRun(t, dir)
	_ = rep
	if _, _, err := build.Plan(p, po); err == nil || !strings.Contains(err.Error(), "outside the skill folder") {
		t.Fatalf("want an outside-link error, got %v", err)
	}
}
