package verify_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/scaffold"
	"github.com/crewfile-dev/crew/internal/verify"
)

func fresh(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "lo")
	if _, err := scaffold.Init(dir); err != nil {
		t.Fatal(err)
	}
	return dir
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
	os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644)
}

func expectError(t *testing.T, dir, substr string) {
	t.Helper()
	_, rep := verify.Run(dir)
	for _, i := range rep.Issues {
		if i.Level == verify.Error && strings.Contains(i.Where+": "+i.Msg, substr) {
			return
		}
	}
	t.Fatalf("expected an error containing %q, got:\n%+v", substr, rep.Issues)
}

func TestStrictChecks(t *testing.T) {
	cases := []struct {
		name, file, old, new, want string
	}{
		{"unknown frontmatter key", "agents/code-explorer.md", "effort: low\n", "effort: low\ncolour: red\n", `unknown key "colour"`},
		{"name must match file", "agents/code-explorer.md", "name: code-explorer", "name: explorer", "must match the filename"},
		{"model required", "agents/code-explorer.md", "model: claude-haiku-5-5\n", "", "model is required"},
		{"model not in list", "agents/code-explorer.md", "model: claude-haiku-5-5", "model: claude-haiku-9", `[claude-code] model "claude-haiku-9" isn't available in Claude Code`},
		{"harness needs its own model", "agents/code-explorer.md", "per_harness:\n  opencode: {model: opencode-go/deepseek-v4.1-flash, effort: low}\n", "", `[opencode] model "claude-haiku-5-5" isn't available in OpenCode`},
		{"effort not accepted", "agents/code-explorer.md", "effort: low\n", "effort: ludicrous\n", `doesn't accept effort "ludicrous"`},
		{"effort checked per model", "agents/ticket-worker.md", "kimi-k3.1-pro, effort: high}", "kimi-k3.1-pro, effort: medium}", `[opencode] opencode-go/kimi-k3.1-pro doesn't accept effort "medium" (accepted: low, high, max)`},
		{"unknown per_harness key", "agents/code-explorer.md", "  opencode: {model:", "  vscode: {model:", `per_harness: unknown harness "vscode"`},
		{"empty per_harness entry", "agents/code-explorer.md", "  opencode: {model: opencode-go/deepseek-v4.1-flash, effort: low}", "  opencode: {}", "per_harness.opencode: set model, effort or both"},
		{"no catalog means inherit", "crew.yaml", "  cursor:\n    enabled: false\n    agents: []", "  cursor:\n    enabled: true\n    agents: [code-explorer]", "[cursor] Cursor agents can't pin a model"},
		{"inherit effort still validated", "agents/code-explorer.md", "model: claude-haiku-5-5\neffort: low\n", "model: inherit\neffort: hihg\n", `[claude-code] effort "hihg" isn't accepted by any Claude Code model`},
		{"all must be alone", "agents/code-explorer.md", "[read, search]", "[all, read]", `"all" must be the only`},
		{"unknown capability", "agents/code-explorer.md", "[read, search]", "[read, telepathy]", `unknown capability "telepathy"`},
		{"unknown skill ref", "agents/spike-researcher.md", "skills: [go-backend-conventions]", "skills: [nope]", `unknown skill "nope"`},
		{"multi-line description", "skills/ticket-intake/SKILL.md", "description: How", "description: |\n  How\n  more", "single line"},
		{"unknown key in registry", "harnesses.yaml", "stale_after_days: 60", "stale_after_days: 60\nfoo: 1", "unknown key foo"},
		{"bad id_style", "harnesses.yaml", "id_style: provider/model", "id_style: weird", `id_style must be "model" or "provider/model"`},
		{"catalog source must be https", "harnesses.yaml", "catalog_source: https://", "catalog_source: http://", "catalog_source must be an https:// URL"},
		{"effort key and suffix exclusive", "harnesses.yaml", "effort_suffix: \"#\"", "effort_suffix: \"#\"\n        effort: effort", "set effort or effort_suffix, not both"},
		{"missing capability mapping", "harnesses.yaml", "      delegate: [Agent]\n", "", `missing mapping for "delegate"`},
		{"model list missing a harness", "models.yaml", "  opencode:\n", "  opencode-old:\n", "no models listed for opencode"},
		{"unknown target harness", "crew.yaml", "  cursor:", "  vscode:\n    enabled: false\n  cursor:", "unknown harness"},
		{"enabled must be explicit", "crew.yaml", "  cursor:\n    enabled: false\n    agents: []", "  cursor:\n    agents: []", "enabled must be set explicitly"},
		{"unknown agent in target", "crew.yaml", "enabled: false\n    agents: [\"*\"]\n    exclude: [proposal-writer]", "enabled: true\n    agents: [\"*\"]\n    exclude: [proposal-writr]", `unknown agent "proposal-writr"`},
		{"glob matches nothing", "crew.yaml", "enabled: false\n    agents: [\"*\"]\n    exclude: [proposal-writer]", "enabled: true\n    agents: [\"*\"]\n    exclude: [zzz-*]", `pattern "zzz-*" matches no agents`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := fresh(t)
			edit(t, filepath.Join(dir, c.file), c.old, c.new)
			expectError(t, dir, c.want)
		})
	}
}

func TestForbiddenNamePerHarness(t *testing.T) {
	dir := fresh(t)
	src := filepath.Join(dir, "agents/code-explorer.md")
	b, _ := os.ReadFile(src)
	os.Remove(src)
	os.WriteFile(filepath.Join(dir, "agents/claude-helper.md"),
		[]byte(strings.Replace(string(b), "name: code-explorer", "name: claude-helper", 1)), 0o644)
	expectError(t, dir, `[claude-code] name must not contain "claude"`)
}

func TestStrayFilesRejected(t *testing.T) {
	dir := fresh(t)
	os.MkdirAll(filepath.Join(dir, "agents/nested"), 0o755)
	expectError(t, dir, "only one .md file per definition")
}

func TestSkillFolderLayout(t *testing.T) {
	t.Run("flat skill file rejected", func(t *testing.T) {
		dir := fresh(t)
		os.WriteFile(filepath.Join(dir, "skills/flat.md"), []byte("---\nname: flat\ndescription: x\n---\nbody\n"), 0o644)
		expectError(t, dir, "skills/<name>/SKILL.md")
	})
	t.Run("folder without SKILL.md rejected", func(t *testing.T) {
		dir := fresh(t)
		os.MkdirAll(filepath.Join(dir, "skills/empty"), 0o755)
		expectError(t, dir, "missing SKILL.md")
	})
	t.Run("supporting files and extra frontmatter allowed", func(t *testing.T) {
		dir := fresh(t)
		os.MkdirAll(filepath.Join(dir, "skills/pr-description/scripts"), 0o755)
		os.WriteFile(filepath.Join(dir, "skills/pr-description/scripts/run.sh"), []byte("x"), 0o644)
		os.WriteFile(filepath.Join(dir, "skills/pr-description/reference.md"), []byte("x"), 0o644)
		edit(t, filepath.Join(dir, "skills/pr-description/SKILL.md"), "description: ", "license: MIT\nallowed-tools: Read\ndescription: ")
		_, rep := verify.Run(dir)
		if rep.Count(verify.Error) != 0 {
			t.Fatalf("skill folders may hold any files, got %+v", rep.Issues)
		}
	})
	t.Run("name must match folder", func(t *testing.T) {
		dir := fresh(t)
		edit(t, filepath.Join(dir, "skills/pr-description/SKILL.md"), "name: pr-description", "name: pr-desc")
		expectError(t, dir, `must match the folder name "pr-description"`)
	})
	t.Run("scaffold is clean", func(t *testing.T) {
		dir := fresh(t)
		_, rep := verify.Run(dir)
		if rep.Count(verify.Error) != 0 {
			t.Fatalf("fresh scaffold should verify, got %+v", rep.Issues)
		}
	})
}

func TestMissingModelsFile(t *testing.T) {
	dir := fresh(t)
	os.Remove(filepath.Join(dir, "models.yaml"))
	expectError(t, dir, "run `crew models sync`")
	// Other problems must still be reported while the list is missing.
	edit(t, filepath.Join(dir, "agents/spike-researcher.md"), "skills: [go-backend-conventions]", "skills: [nope]")
	expectError(t, dir, `unknown skill "nope"`)
}

func TestModelsFileOptionalWithoutCatalogs(t *testing.T) {
	dir := fresh(t)
	os.Remove(filepath.Join(dir, "models.yaml"))
	b, _ := os.ReadFile(filepath.Join(dir, "harnesses.yaml"))
	noCat := regexp.MustCompile(`(?m)^    catalog:\n(?:      .*\n)+`).ReplaceAllString(string(b), "")
	os.WriteFile(filepath.Join(dir, "harnesses.yaml"), []byte(noCat), 0o644)
	for _, a := range []string{"code-explorer", "epic-planner", "proposal-writer", "spike-researcher", "ticket-worker"} {
		path := filepath.Join(dir, "agents", a+".md")
		ab, _ := os.ReadFile(path)
		out := regexp.MustCompile(`(?m)^model: .*\neffort: .*\nper_harness:\n  .*\n`).ReplaceAllString(string(ab), "model: inherit\n")
		os.WriteFile(path, []byte(out), 0o644)
	}
	_, rep := verify.Run(dir)
	if rep.Count(verify.Error) != 0 {
		t.Fatalf("no catalog anywhere should need no models.yaml, got %+v", rep.Issues)
	}
}

func TestEffortIgnoredWhereHarnessHasNoEffortSetting(t *testing.T) {
	dir := fresh(t)
	edit(t, filepath.Join(dir, "crew.yaml"), "  gemini-cli:\n    enabled: false\n    agents: []", "  gemini-cli:\n    enabled: true\n    agents: [code-explorer]")
	edit(t, filepath.Join(dir, "agents/code-explorer.md"), "per_harness:\n", "per_harness:\n  gemini-cli: {model: gemini-3.8-flash, effort: high}\n")
	_, rep := verify.Run(dir)
	for _, i := range rep.Issues {
		if i.Level == verify.Info && strings.Contains(i.Msg, `[gemini-cli] Gemini CLI has no per-agent effort setting; effort "high" is ignored`) {
			return
		}
	}
	t.Fatalf("expected an ignored-effort note, got %+v", rep.Issues)
}

func TestSymlinkedSkillFolder(t *testing.T) {
	dir := fresh(t)
	shared := filepath.Join(t.TempDir(), "pr-description")
	if err := os.Rename(filepath.Join(dir, "skills/pr-description"), shared); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(dir, "skills/pr-description")); err != nil {
		t.Fatal(err)
	}
	_, rep := verify.Run(dir)
	if rep.Count(verify.Error) != 0 {
		t.Fatalf("a symlinked skill folder should load, got %+v", rep.Issues)
	}
}

func TestStaleModelListWarns(t *testing.T) {
	dir := fresh(t)
	edit(t, filepath.Join(dir, "harnesses.yaml"), "verified: ", "verified: 2099-01-01\n# was: ")
	edit(t, filepath.Join(dir, "models.yaml"), "fetched: ", "fetched: \"2020-01-01\"\n# was: ")
	_, rep := verify.Run(dir)
	for _, i := range rep.Issues {
		if i.Level == verify.Warning && strings.Contains(i.Msg, "model list is") {
			return
		}
	}
	t.Fatalf("expected a stale model list warning, got %+v", rep.Issues)
}

func TestStaleFactsWarn(t *testing.T) {
	dir := fresh(t)
	defer func(f func() time.Time) { verify.Now = f }(verify.Now)
	verify.Now = func() time.Time { return time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC) }
	_, rep := verify.Run(dir)
	if rep.Count(verify.Warning) == 0 || rep.OK(false) {
		t.Fatal("stale registry should warn and block in strict mode")
	}
	if !rep.OK(true) {
		t.Fatal("--allow-warnings should let it through")
	}
}

func TestCapabilityWithoutToolIsNoted(t *testing.T) {
	dir := fresh(t)
	edit(t, filepath.Join(dir, "agents/code-explorer.md"), "[read, search]", "[read, search, structured-output]")
	_, rep := verify.Run(dir)
	want := "[opencode] structured-output has no OpenCode tool, so it's dropped there"
	for _, i := range rep.Issues {
		if i.Level == verify.Info && strings.Contains(i.Msg, want) {
			if strings.Contains(i.Msg, "claude-code") {
				t.Fatalf("Claude Code maps structured-output, shouldn't be noted: %s", i.Msg)
			}
			return
		}
	}
	t.Fatalf("expected note %q, got %+v", want, rep.Issues)
}

func TestVerify_LegacyWorkflowsDirNote(t *testing.T) {
	dir := fresh(t)
	os.MkdirAll(filepath.Join(dir, "workflows"), 0o755)
	os.WriteFile(filepath.Join(dir, "workflows", "x.md"), []byte("---\nname: x\n---\n"), 0o644)
	_, rep := verify.Run(dir)
	for _, i := range rep.Issues {
		if i.Level == verify.Info && strings.Contains(i.Msg, "factories/") {
			return
		}
	}
	t.Fatalf("expected an info note pointing at factories/, got %+v", rep.Issues)
}

func TestHeadless_RejectsBypass(t *testing.T) {
	for _, bad := range []string{"--dangerously-skip-permissions", "bypassPermissions", "--yolo", "--skip-permissions-check"} {
		t.Run(bad, func(t *testing.T) {
			dir := fresh(t)
			edit(t, filepath.Join(dir, "harnesses.yaml"), "--permission-mode, dontAsk,", "--permission-mode, dontAsk, "+bad+",")
			expectError(t, dir, "headless commands may not bypass permissions")
		})
	}
}

func TestHeadless_TokenRules(t *testing.T) {
	cases := []struct{ name, old, new, want string }{
		{"unknown token", `"{agent}", --allowedTools`, `"{agnt}", --allowedTools`, `unknown token "{agnt}"`},
		{"agent token required", `--agent, "{agent}", --allowedTools`, `--allowedTools`, "must contain {agent}"},
		{"flag needs allowed", `--allowedTools, "{allowed}", `, ``, "allow_via: flag needs an {allowed} token"},
		{"arg needs prompt", "prompt_via: stdin", "prompt_via: arg", "prompt_via: arg needs a {prompt} token"},
		{"bad prompt_via", "prompt_via: stdin", "prompt_via: pipe", `prompt_via must be "stdin" or "arg"`},
		{"bad allow_via", "allow_via: flag", "allow_via: maybe", `allow_via must be "flag" or "agent-file"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := fresh(t)
			edit(t, filepath.Join(dir, "harnesses.yaml"), c.old, c.new)
			expectError(t, dir, c.want)
		})
	}
}
