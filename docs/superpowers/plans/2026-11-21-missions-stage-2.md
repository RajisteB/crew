# crew missions, stage 2: auto-merge — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The principal turns auto-merge on for a domain in a repo, and from then on crew watches each of that domain's pull requests, merges it when its checks pass, sends it back to its factory when checks fail or `main` conflicts, and never leaves a pull request stuck without saying so. Also: `crew mission setup` upgrades an existing `harnesses.yaml` itself, so no hand edit is needed before a first mission.

**Architecture:** The switch is a sealed file in `<crewDir>/principal`, written only by `crew automerge` after a typed confirmation on the terminal. Each tick the engine reads the switch, the pull request (state, mergeability, head commit, checks) and the files the piece changed, and hands them to `Decide` as observations. `Decide` stays pure and returns two new actions, `Merge` and `ResolveConflict`; the engine carries them out through `gh` and `git` and reads the switch again immediately before merging.

**Tech Stack:** Go 1.22 module (toolchain 1.24), `gopkg.in/yaml.v3` only, stdlib `testing`, the `git` and `gh` CLIs.

**Spec:** `docs/superpowers/specs/2026-11-18-orchestrator-missions-design.md`. Read "The auto-merge switch", "Worktrees, PRs and merging", "Work sent back to a factory" and "What the safeguards cover" before any task. This plan covers **stage 2 of its Build order**, plus one stage 1 follow-up (Task 1).

**Not in this plan:** deliverables following the switch, and Jira (stage 3); the check of a piece's own `paths` and the out-of-paths stop (stage 4); deleting merged piece branches on the remote; updating a branch that is merely behind `main` with no conflict; Project 2's mapping of the switch to a release setting.

## Global Constraints

- The only dependency is `gopkg.in/yaml.v3`. No new modules.
- Before writing Go, load the `use-modern-go` skill and follow it for Go 1.22.
- Config and frontmatter decode strictly: unknown keys are errors.
- State is immutable: every state-machine function takes a `State` and returns a new one. `Decide` performs no I/O.
- **Only `crew automerge` writes the switch**, and only after `principal.Confirm` on `/dev/tty`. It has no flag, environment variable or stdin path that skips the confirmation. No other command and no engine code writes `automerge.json`.
- **Only the engine's merge effect merges**, through `gh pr merge` with `--match-head-commit`, and it reads the switch from disk again immediately before the call. No agent prompt asks an agent to merge.
- A switch that can't be read, or doesn't match its checksum, is `off` for every domain.
- crew never merges a head commit it didn't push itself.
- `runs_at_once` stays 1.
- Every external command runs through `exec` with an argument list. Never build a shell string.
- Tests never call the real `gh`, a git remote, `osascript` or a harness CLI. Use fakes, and local temp repos for git.
- No secret, token or file content in state, events, logs or notifications. Check names, file names and links are allowed; check logs go only into the note a station receives.
- Files stay under 400 lines and functions under 50 lines.
- Every commit message uses the conventional `type: description` form and ends with the `Co-Authored-By` line naming the model that wrote the commit, and the `Claude-Session` line. The controller passes both to each implementer.

## Review Focus

1. **`automerge.json` is edited by hand, or its `.sum` is missing.** Every domain reads as `off`, nothing merges, the pull request waits for the principal, and a note says which file to delete. → Task 2 `TestSwitches_TamperedIsAnError`, Task 9 `TestEngine_TamperedSwitchNeverMerges`.
2. **Someone else pushes to the piece branch, or GitHub still shows the previous head's checks.** crew merges nothing: the pull request's head must be the commit crew pushed, and the merge names that commit. → Task 7 `TestDecide_HeadMismatchNeverMerges`, Task 3 `TestMergePR_NamesTheHead`.
3. **The switch is `on` and no check ever reports** (CI removed, or it doesn't run on this branch). After 2 minutes the pull request goes under "waiting on you" with the reason; it is not left silently. → Task 7 `TestDecide_NoChecksWaitsAfterGrace`.
4. **The principal turns the switch off while a pull request's checks are running.** The merge effect reads the switch again and doesn't merge. → Task 9 `TestEngine_MergeRereadsSwitch`.
5. **`gh pr merge` is refused** (branch protection, a merge method the repo doesn't allow, or the principal merged it a moment earlier). State doesn't change, the refusal is a note that names `merge_method`, and the next tick sees a merged pull request as merged. → Task 9 `TestEngine_MergeRefusedIsANote`.

---

## Phase A: setup without a hand edit

### Task 1: `crew mission setup` upgrades `harnesses.yaml`

**Files:** Create `internal/scaffold/upgrade.go`, `internal/scaffold/upgrade_test.go`, `internal/scaffold/testdata/harnesses-before-missions.yaml`. Modify `cmd/crew/mission.go` (`missionSetup`, `printCheckErrors`, usage text), `cmd/crew/mission_test.go`, `internal/mission/check.go`, `docs/missions.md` (the setup section).

**Interfaces (Produces):**
```go
// internal/scaffold
type HarnessUpgrade struct {
    Added   []string // one line per harness changed, e.g. "claude-code: deny rules, deny_via and {settings}, orchestrator launch and allow list"
    Skipped []string // one line per harness left alone, with the reason
    Backup  string   // name of the copy made before writing; "" when nothing was written
}
func UpgradeHarnesses(dir string) (HarnessUpgrade, error)

// internal/mission
var ErrNoDeny = errors.New("harness can't apply deny rules") // errors.Is matches the check.go error that ends "which can't apply deny rules"; that error's text is unchanged
```

**Rules for `UpgradeHarnesses`** (work on `yaml.Node` trees of the file and of the embedded template, so comments survive):
- Only harnesses present in both are looked at. A harness is upgraded as a whole or not at all.
- A harness needs upgrading when the template's entry has any of these and the file's entry lacks it: `headless.deny_via`, `deny`, `foreman.orchestrator_launch`, `foreman.orchestrator_allow`.
- When `headless.deny_via` is one of the missing keys, the harness's `headless.command` must be upgradable: it already holds `{settings}`, or it equals the template's command with the `{settings}` element and the element before it removed. Otherwise skip the harness with: `<name>: headless.command was changed by hand; add --settings {settings} to it, then run crew mission setup again`.
- An upgraded harness gets, from the template: the command (when it lacked `{settings}`), `headless.deny_via`, `deny` (only when the file has no `deny` key), `foreman.orchestrator_launch` and `foreman.orchestrator_allow` (each only when absent). No existing key is removed, and no other existing value is changed.
- Nothing to add: no write, no backup, zero-value result.
- Before writing: the new content must pass `config.LoadRegistry`'s strict decode. If it doesn't, return the error and leave the file alone.
- Write the copy `harnesses.yaml.before-missions` first (never overwrite an existing one), then the file through a temp file and a rename, keeping its mode.
- If the upgraded content decodes to the same `config.Registry` as the template (`reflect.DeepEqual`), write the template's bytes, so an untouched file ends up formatted exactly like a fresh `crew init`. Otherwise encode the node tree with a 2-space indent.
- No `harnesses.yaml`: error `no harnesses.yaml in <dir>; run crew init`.

- [ ] **Step 1: Make the fixture.** `git show 17fe00c:internal/scaffold/template/harnesses.yaml > internal/scaffold/testdata/harnesses-before-missions.yaml`
- [ ] **Step 2: Write the failing tests** in `upgrade_test.go`:
  - `TestUpgradeHarnesses_OldTemplateBecomesNewTemplate`: the fixture in a temp dir → file bytes equal the embedded `template/harnesses.yaml`; `Added` has exactly one line and it starts with `claude-code:`; `Backup == "harnesses.yaml.before-missions"` and that file holds the fixture's bytes.
  - `TestUpgradeHarnesses_SecondRunChangesNothing`: run twice → second result is the zero value and the file's mod time is unchanged.
  - `TestUpgradeHarnesses_KeepsOwnEdits`: the fixture with `verified: 2026-12-01`, an added top comment `# mine`, an extra harness `mine` (a copy of `cursor`), and `deny: {all: ["Bash(rm:*)"]}` on `claude-code` → the result loads with `config.LoadRegistry`; `claude-code` has `Deny.All == ["Bash(rm:*)"]`, `Headless.DenyVia == "settings-file"`, a command containing `{settings}`, and a non-empty `Foreman.OrchestratorLaunch`; harness `mine` and the text `# mine` are still there; `Verified == "2026-12-01"`.
  - `TestUpgradeHarnesses_CustomCommandLeftAlone`: the fixture with `--verbose` appended to the `claude-code` command → file bytes unchanged, `Added` empty, `Skipped` has one line containing `claude-code` and `{settings}`, `Backup == ""`.
  - `TestUpgradeHarnesses_NoFile`: empty dir → error containing `crew init`.
- [ ] **Step 3: Run** `go test ./internal/scaffold/` — Expected: FAIL, `UpgradeHarnesses` undefined.
- [ ] **Step 4: Implement `UpgradeHarnesses`** per the rules above.
- [ ] **Step 5: Run** `go test ./internal/scaffold/` — Expected: PASS.
- [ ] **Step 6: Write the failing command tests** in `cmd/crew/mission_test.go`:
  - `TestMissionSetup_UpgradesHarnesses`: a crew dir holding the fixture → output has a line starting `updated ` that names `harnesses.yaml` and `harnesses.yaml.before-missions`, a `skipped` line for each `Skipped` entry (none here), and the last line is `next: crew compile`. A second run prints `already set up`.
  - `TestMissionStart_NoDenyPointsToSetup`: a mission whose factory runs on a harness without `deny_via` → the output's last line is `run crew mission setup to add the missions parts to harnesses.yaml`.
- [ ] **Step 7: Implement.** `missionSetup` calls `scaffold.MissionDefaults` then `scaffold.UpgradeHarnesses`; it prints `already set up` only when both changed nothing. `check.go` wraps `ErrNoDeny`; `printCheckErrors` adds the hint line when any error `errors.Is` it.
- [ ] **Step 8: Update `docs/missions.md`.** Replace the hand-edit instructions in the setup section with what `crew mission setup` now does, the backup file's name, and the one case it leaves alone (a hand-changed `headless.command`). Keep the block as reference under a heading that says it is what setup adds.
- [ ] **Step 9: Run** `go test ./... && go vet ./...` — Expected: PASS.
- [ ] **Step 10: Commit** `feat: mission setup upgrades harnesses.yaml itself`

---

## Phase B: foundations

### Task 2: The switch store

**Files:** Create `internal/principal/switch.go`, `internal/principal/switch_test.go`. Modify `internal/principal/store.go` (share the sealed read and write).

**Interfaces (Produces):**
```go
type AutoMode string
const (
    AutoOff           AutoMode = "off"
    AutoOn            AutoMode = "on"
    AutoWithoutChecks AutoMode = "without-checks"
)
type SwitchRecord struct {
    Repo   string    `json:"repo"`   // "owner/name"
    Domain string    `json:"domain"`
    Mode   AutoMode  `json:"mode"`
    SetAt  time.Time `json:"set_at"`
}
// Switches returns repo's records by domain. A missing file is an empty map.
// ErrTampered (wrapped, naming the file) when automerge.json and its checksum
// disagree, the checksum is missing, or a record holds an unknown mode.
func (s Store) Switches(repo string) (map[string]SwitchRecord, error)
// SetSwitches replaces the record of each (repo, domain) given and keeps the rest.
func (s Store) SetSwitches(recs []SwitchRecord) error
// ModeOf is the domain's mode, AutoOff when it has no record.
func ModeOf(recs map[string]SwitchRecord, domain string) AutoMode
```
Files: `automerge.json` and `automerge.json.sum` in `Store.Dir`, mode 0600, written json first, then the checksum, each through `writeAtomic`. Extract `readSealed(path, sumPath string) ([]byte, error)` and `writeSealed(path, sumPath string, data []byte) error` from the repos code and use them for both files. `ErrTampered`'s text becomes `the file does not match its checksum`; callers wrap it with the path. Existing repos tests keep passing.

- [ ] **Step 1: Write the failing tests:**
  - `TestSwitches_MissingFileIsEmpty`: empty dir → empty map, nil error; `ModeOf(m, "product") == AutoOff`.
  - `TestSetSwitches_RoundTrip`: set `a/b product on` and `a/b design without-checks`, then `c/d product on` → `Switches("a/b")` has exactly the two, with those modes; `Switches("c/d")` has one.
  - `TestSetSwitches_ReplacesOneKeepsOthers`: set `a/b product on`, then `a/b product off` → `ModeOf == AutoOff`, and an earlier `a/b design` record is unchanged.
  - `TestSwitches_TamperedIsAnError`: after a set, (a) rewrite `automerge.json` changing `off` to `on`, (b) delete the `.sum`, (c) write a record with mode `always` plus its correct checksum → each returns an error where `errors.Is(err, ErrTampered)` and whose text contains `automerge.json`.
  - `TestSetSwitches_FileModes`: both files are 0600.
- [ ] **Step 2: Run** `go test ./internal/principal/` — Expected: FAIL, undefined.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/principal/ ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: the auto-merge switch store`

### Task 3: gh — checks, mergeability, merging

**Files:** Create `internal/gh/checks.go`, `internal/gh/checks_test.go`. Modify `internal/gh/gh.go` (`PR`, `PRState`), `internal/gh/gh_test.go`.

**Interfaces (Produces):**
```go
type Check struct{ Name, State, URL string } // State: "pass" | "fail" | "pending" | "skip"
type PR struct {
    Number    int
    State     string  // OPEN | MERGED | CLOSED
    URL       string
    Mergeable string  // MERGEABLE | CONFLICTING | UNKNOWN ("" from CreatePR and FindPR)
    HeadSHA   string
    Checks    []Check
}
// PRState now asks for: number,state,url,mergeable,headRefOid,statusCheckRollup
func (c Client) PRState(ctx context.Context, number int) (PR, error)
// MergePR runs: gh pr merge <number> --<method> --match-head-commit <headSHA>
// method is "squash", "merge" or "rebase"; anything else, or an empty headSHA, is an error before gh runs.
func (c Client) MergePR(ctx context.Context, number int, method, headSHA string) error
// FailedLog is the end of a failed GitHub Actions job's log: the last 40 lines,
// cut to 2000 runes. "" and nil when check.URL has no /actions/runs/<n>/job/<id>.
// It runs: gh run view --job <id> --log-failed
func (c Client) FailedLog(ctx context.Context, check Check) (string, error)
// HasChecks reports whether branch's latest commit has a check run or a status. It runs
// gh api repos/{owner}/{repo}/commits/<branch>/check-runs --jq .total_count
// gh api repos/{owner}/{repo}/commits/<branch>/status --jq ".statuses | length"
func (c Client) HasChecks(ctx context.Context, branch string) (bool, error)
```

**Mapping `statusCheckRollup` entries to `Check`** (each entry has `__typename`):
| Entry | Name | URL | State |
|---|---|---|---|
| `CheckRun` | `name` | `detailsUrl` | `status` not `COMPLETED` → pending; `conclusion` `SUCCESS` → pass; `NEUTRAL` or `SKIPPED` → skip; anything else → fail |
| `StatusContext` | `context` | `targetUrl` | `state` `SUCCESS` → pass; `PENDING` or `EXPECTED` → pending; anything else → fail |
| any other type | its `name` or `context` | "" | pending |

Before writing the parser, run `gh pr view --help` and confirm the three field names exist in its JSON field list; if one differs, stop and report it.

- [ ] **Step 1: Write the failing tests** with a fake `Exec` that records args and returns canned JSON:
  - `TestPRState_ParsesChecks`: a rollup holding one `CheckRun` each of COMPLETED/SUCCESS, COMPLETED/FAILURE, IN_PROGRESS, COMPLETED/SKIPPED, COMPLETED/CANCELLED and one `StatusContext` each of SUCCESS, PENDING, ERROR → states `pass, fail, pending, skip, fail, pass, pending, fail` in order; `Mergeable == "CONFLICTING"`; `HeadSHA == "abc123"`; the args hold `number,state,url,mergeable,headRefOid,statusCheckRollup`.
  - `TestPRState_UnknownEntryIsPending`: `{"__typename":"Future","name":"x"}` → one check, `pending`.
  - `TestPRState_NoRollupIsNoChecks`: `"statusCheckRollup": null` → `len(Checks) == 0`, no error.
  - `TestMergePR_NamesTheHead`: `MergePR(ctx, 12, "squash", "abc123")` → args are exactly `pr merge 12 --squash --match-head-commit abc123`.
  - `TestMergePR_RefusesBadInput`: method `fast` and head `""` → each an error, and `Exec` was not called.
  - `TestFailedLog_TailOfActionsJob`: URL `https://github.com/a/b/actions/runs/7/job/99` and 100 output lines → args `run view --job 99 --log-failed`; result is the last 40 lines.
  - `TestFailedLog_NotActions`: URL `https://ci.example/1` → `""`, nil, `Exec` not called.
  - `TestHasChecks`: counts `0` and `0` → false; `0` and `2` → true; `1` and `0` → true.
- [ ] **Step 2: Run** `go test ./internal/gh/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/gh/ ./internal/mission/ ./cmd/crew/` — Expected: PASS (the wider `PR` struct breaks nothing).
- [ ] **Step 5: Commit** `feat: gh reads checks and mergeability and merges a named head`

### Task 4: gitx — head, changed files, merging the base in

**Files:** Modify `internal/gitx/gitx.go`, `internal/gitx/gitx_test.go`. If `gitx.go` would pass 400 lines, put the new functions in `internal/gitx/merge.go`.

**Interfaces (Produces):**
```go
func (r Repo) Head(ctx context.Context, worktree string) (string, error)                 // git rev-parse HEAD
func (r Repo) ChangedFiles(ctx context.Context, worktree, base string) ([]string, error) // git diff --name-only <base>...HEAD
// MergeInto merges from into the worktree's branch with --no-edit. When the merge
// conflicts it collects the conflicted paths (git diff --name-only --diff-filter=U),
// runs git merge --abort, and returns the paths with a nil error. A clean merge,
// or nothing to merge, returns no paths.
func (r Repo) MergeInto(ctx context.Context, worktree, from string) (conflicts []string, err error)
```
`from` and `base` go through `checkRef`. All three use the existing `git` helper and its timeouts.

- [ ] **Step 1: Write the failing tests** on temp repos (a bare origin plus a clone, as the existing tests do):
  - `TestHead_IsTheCommit`: equals `git rev-parse HEAD` run by the test.
  - `TestChangedFiles_ListsBranchFilesOnly`: branch adds `a/x.txt` and `b/y.txt`, main then adds `c/z.txt` → exactly `a/x.txt`, `b/y.txt`.
  - `TestMergeInto_Clean`: main changes another file → no conflicts, `HEAD` has two parents, the worktree is clean.
  - `TestMergeInto_ConflictIsUndone`: main and the branch change the same line of `f.txt` → `conflicts == ["f.txt"]`, nil error, `HEAD` is unchanged, `Dirty` is empty, and no `MERGE_HEAD` exists.
  - `TestMergeInto_UpToDate`: nothing to merge → no conflicts, `HEAD` unchanged.
- [ ] **Step 2: Run** `go test ./internal/gitx/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/gitx/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: gitx reads the head and changed files and merges the base in`

### Task 5: Path matching and the mode a pull request follows

**Files:** Create `internal/mission/paths.go`, `internal/mission/paths_test.go`, `internal/mission/automode.go`, `internal/mission/automode_test.go`.

**Interfaces (Consumes):** `principal.AutoMode`, `principal.SwitchRecord`, `principal.ModeOf` (Task 2); `source.Domain.Paths`.

**Interfaces (Produces):**
```go
// MatchPath reports whether file (slash-separated, relative to the repo root)
// matches pattern. "**" matches any number of path segments, including none;
// "*" and "?" match inside one segment, as path.Match does. A pattern with no
// wildcard also matches everything under it as a folder. A malformed pattern matches nothing.
func MatchPath(pattern, file string) bool

// AutoObs is the auto-merge mode one pull request follows.
type AutoObs struct {
    Mode    principal.AutoMode
    Blocker string // a domain the pull request touches whose switch is off; "" otherwise
}
// EffectiveMode: the piece's own domain decides first. Every other domain that
// lists paths and has at least one of files inside them must be on too.
func EffectiveMode(p Piece, files []string, domains map[string]source.Domain, switches map[string]principal.SwitchRecord) AutoObs
```

**Rules for `EffectiveMode`:**
- Own domain `off` → `{AutoOff, ""}`.
- A touched other domain that is `off` → `{AutoOff, Blocker: <its name>}` (the first by name when several).
- Otherwise `AutoOn` if the own domain or any touched domain is `on`; `AutoWithoutChecks` only when all of them are `without-checks`.
- A domain with no `paths` is never "touched". A file inside no domain's paths follows the piece's own domain.

- [ ] **Step 1: Write the failing tests:**
  - `TestMatchPath` (table): `brand/**` matches `brand/a.svg` and `brand/x/y.svg`, not `brandy/a.svg`; `**/*.md` matches `README.md` and `docs/a/b.md`; `app/*/main.go` matches `app/api/main.go`, not `app/api/v2/main.go`; `brand` matches `brand/a.svg` and `brand`; `a/**/z` matches `a/z` and `a/b/c/z`; `[` matches nothing.
  - `TestEffectiveMode_OwnDomainOff`: product off → `{off, ""}` whatever the files.
  - `TestEffectiveMode_OtherDomainOffBlocks`: product on, design off with `brand/**`, files `app/x.go`, `brand/logo.svg` → `{off, "design"}`.
  - `TestEffectiveMode_UntouchedDomainIgnored`: same switches, files `app/x.go` only → `{on, ""}`.
  - `TestEffectiveMode_StrictestOnWins`: product `without-checks`, design `on`, files in both → `on`; both `without-checks` → `without-checks`.
  - `TestEffectiveMode_DomainWithoutPathsNeverTouched`: marketing off with no paths → doesn't block.
- [ ] **Step 2: Run** `go test ./internal/mission/ -run 'MatchPath|EffectiveMode'` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same command — Expected: PASS.
- [ ] **Step 5: Commit** `feat: path matching and the auto-merge mode a pull request follows`

---

## Phase C: the engine

### Task 6: State, reducers, events, the merge method

**Files:** Modify `internal/mission/state.go`, `reduce.go`, `events.go`, `settings.go`, their tests, `internal/mission/effects.go` (callers of the changed reducers only), `internal/scaffold/template/missions.yaml`.

**Interfaces (Produces):**
```go
// PieceState gains:
Head    string    `json:"head,omitempty"`     // the commit crew pushed to the pull request
PRSince time.Time `json:"pr_since,omitempty"` // when crew last pushed that head
Problem string    `json:"problem,omitempty"`  // why the piece was last sent back from its pull request: names only, at most 300 runes

const (
    reasonConflict     = "conflict"
    reasonChecksFailed = "checks-failed"
    EvChecksPassed     = "checks-passed"
    EvChecksFailed     = "checks-failed"
    EvConflict         = "conflict"
)

func PROpened(s State, piece string, pr gh.PR, head string, now time.Time) State // also sets Head, PRSince; clears Problem
func SentBackDone(s State, piece, reason, problem string) State                 // also: Status = PieceRunning, Wait = nil, Problem = problem cut to 300 runes
func BranchUpdated(s State, piece, head string, now time.Time) State            // Head, PRSince
func Merged(s State, piece string) State                                        // Status = PieceComplete, Wait = nil

// Settings gains:
MergeMethod string `yaml:"merge_method"` // "squash" | "merge" | "rebase"; empty means squash
func (s Settings) Method() string        // "squash" when unset
```
`validate` refuses any other `merge_method`. The template `missions.yaml` gains `merge_method: squash  # how crew merges a pull request when auto-merge is on: squash, merge or rebase`.

- [ ] **Step 1: Write the failing tests:**
  - `TestPROpened_RecordsHeadAndTime`; `TestSentBackDone_FromPROpenGoesBackToRunning` (a pr-open piece holding a merge wait → running, no wait, `SentBack["checks-failed"] == 1`, `Problem` set, and the input state is unchanged); `TestSentBackDone_CutsProblem` (400 runes in → 300 stored); `TestBranchUpdated_KeepsStatus`; `TestMerged_CompletesAndClearsWait`.
  - `TestSettings_MergeMethod`: unset → `Method() == "squash"`; `rebase` → `rebase`; `fast-forward` → load error naming `merge_method`.
  - `TestState_CloneKeepsNewFields`.
- [ ] **Step 2: Run** `go test ./internal/mission/` — Expected: FAIL to compile.
- [ ] **Step 3: Implement,** and pass `""` and the tick's time at the existing call sites so the package builds (Task 9 fills in the real values).
- [ ] **Step 4: Run** `go test ./... ` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: mission state for watched pull requests`

### Task 7: Decide — the pull request rules

**Files:** Create `internal/mission/machine_pr.go`, `internal/mission/machine_pr_test.go`. Modify `internal/mission/machine.go` (move `prOpen` out, `Obs`, actions, `ask`).

**Interfaces (Consumes):** `gh.PR`, `gh.Check` (Task 3), `AutoObs` (Task 5), state fields and reasons (Task 6).

**Interfaces (Produces):**
```go
// Obs gains:
Auto map[string]AutoObs // per pr-open piece; a piece that is absent follows AutoOff

type Merge struct {            // merge the piece's pull request at this head
    Piece, Head string
    Checked     bool           // true when checks passed; false for without-checks
}
type ResolveConflict struct{ Piece string } // merge the base into the branch, or send the piece back

const (
    checksGrace    = 2 * time.Minute  // how long a new head may show no checks
    checksPatience = 30 * time.Minute // how long checks may run before the principal is told
    maxPRSendBacks = 2
)
```
`AskOrchestrator.Failure` may now also be `"conflict"` or `"checks-failed"`.

**The rules, in order, for a pr-open piece whose pull request was observed.** `since` is `Now - PRSince`; `ours` is `pr.HeadSHA == p.Head && p.Head != ""`.

| # | When | What |
|---|---|---|
| 1 | state `MERGED` | complete, record `pr-merged`, `Cleanup{Force: true}` (as today) |
| 2 | state `CLOSED` | the `pr-closed` wait (as today) |
| 3 | the piece holds a `question` or `limit` wait | nothing: it waits for the answer |
| 4 | `Mergeable == "CONFLICTING"` | `SentBack["conflict"] < 2` → `ResolveConflict`; else `ask(id, "conflict")` |
| 5 | mode is not `without-checks`, `ours`, and a check is `fail` | `SentBack["checks-failed"] < 2` → `SendBack{Reason: "checks-failed", Note: failedChecksNote}`; else `ask(id, "checks-failed")` |
| 6 | mode `off` | the `merge` wait, if the piece holds none |
| 7 | not `ours` | `since >= checksGrace` → the `merge` wait; else nothing |
| 8 | `Mergeable != "MERGEABLE"` | nothing |
| 9 | mode `without-checks` | `Merge{Head: p.Head}` |
| 10 | mode `on`, no check is `pass`, `fail` or `pending` | `since >= checksGrace` → the `merge` wait; else nothing |
| 11 | mode `on`, a check is `pending` | `since >= checksPatience` → the `merge` wait; else nothing |
| 12 | mode `on`, at least one `pass`, none `pending` or `fail` | `Merge{Head: p.Head, Checked: true}` |

A `merge` wait never stops rules 4, 5, 9 and 12: crew still acts on a pull request it has told the principal about. Opening a wait when the piece already holds a `merge` wait does nothing.

**The `merge` wait's question, by rule** (`n` is the PR number, `id` the piece):
- 6, no blocker: `PR #n (id) is waiting on you`
- 6, blocker: `PR #n (id) also changes files of the <blocker> domain, whose auto-merge is off; it is waiting on you`
- 7: `PR #n (id) has commits crew didn't push, so crew won't merge it; it is waiting on you`
- 10: `PR #n (id) has no checks, and auto-merge needs at least one to pass; it is waiting on you`
- 11: `PR #n (id): checks are still running after 30m; crew merges it when they pass, or merge it yourself`

`failedChecksNote(checks)`: `These checks failed on the pull request: <name> (<url>), …. Fix what they report and commit.` At most 10 checks, then `(and N more)`.

`ask` on a pr-open piece that has used up its orchestrator calls opens the `limit` wait with an empty `Back`, so the piece stays pr-open and its pull request is still watched.

`Decide` records no event for rules 4, 5, 9 and 12; the engine records them when the effect succeeds, so a failing effect can't repeat an event every tick.

- [ ] **Step 1: Write the failing tests** (`machine_pr_test.go`, one small state builder; each asserts the returned actions and the wait):
  - `TestDecide_OffOpensMergeWaitOnce`: two calls in a row → one wait, second call adds no `Notify`.
  - `TestDecide_OnMergesWhenChecksPass`: one pass, one skip → `Merge{Head: "abc", Checked: true}`, no wait.
  - `TestDecide_OnHoldsWhilePending`: `since` 5m → no actions, no wait. `since` 31m → the rule 11 wait; then checks pass → `Merge`, even with the wait held.
  - `TestDecide_NoChecksWaitsAfterGrace`: `since` 1m → nothing; 3m → the rule 10 wait and one `Notify`.
  - `TestDecide_SkipsAloneDontCount`: only `skip` checks, 3m → the rule 10 wait, no `Merge`.
  - `TestDecide_WithoutChecksMergesAtOnce`: no checks, `since` 0 → `Merge{Checked: false}`; a failing check is ignored.
  - `TestDecide_HeadMismatchNeverMerges`: all checks pass, `HeadSHA` differs → no `Merge` at 1m; the rule 7 wait at 3m; same for an empty `p.Head`.
  - `TestDecide_UnknownMergeableHolds`: mode on, checks pass, `UNKNOWN` → nothing.
  - `TestDecide_FailedChecksSendBackTwiceThenAsk`: counts 0 and 1 → `SendBack` with the check's name and URL in `Note`; count 2 → `AskOrchestrator{Failure: "checks-failed"}`.
  - `TestDecide_FailedChecksSendBackWhenOff`: mode off, holding a merge wait → `SendBack`.
  - `TestDecide_StaleFailureIgnored`: a failing check but `HeadSHA` differs → no `SendBack`.
  - `TestDecide_ConflictResolvesTwiceThenAsks`: counts 0, 1 → `ResolveConflict`; 2 → `AskOrchestrator{Failure: "conflict"}`.
  - `TestDecide_BlockerNamedInWait`: `Auto{off, "design"}` → the rule 6 blocker text.
  - `TestDecide_QuestionWaitStopsAutomation`: a pr-open piece holding a `question` wait, checks pass, mode on → no actions; state `MERGED` → still completes.
  - `TestDecide_AskAtCallLimitKeepsPROpen`: calls used up, conflict count 2 → a `limit` wait with `Back == ""` and status still `pr-open`.
  - `TestDecide_AbsentAutoIsOff`: `Obs.Auto` has no entry → the rule 6 wait.
- [ ] **Step 2: Run** `go test ./internal/mission/ -run Decide` — Expected: FAIL.
- [ ] **Step 3: Implement** in `machine_pr.go`: `prOpen` as the table, one small function per group of rules (ended, conflict, failed checks, by mode), and a pure `tally(checks) (pass, fail, pending int)`.
- [ ] **Step 4: Run** `go test ./internal/mission/` — Expected: PASS, stage 1 tests included.
- [ ] **Step 5: Commit** `feat: Decide watches checks, conflicts and the auto-merge mode`

### Task 8: Decisions and answers on a piece whose pull request is open

**Files:** Modify `internal/mission/decision.go`, `answer.go`, `summary.go`, `summary_prompt.go`, and their tests.

**Interfaces (Produces):**
```go
// allowedFor is what the orchestrator may choose for this failure: every
// action, minus "retry" for "conflict" and "checks-failed" (the run is done; there is no station to run again).
func allowedFor(failure string) []string
// ForPR adapts a summary to a pull request failure.
func (s Summary) ForPR(failure, problem string) Summary // Failure = failure, Reason = problem, Allowed = allowedFor(failure)
```

**Behaviour:**
- `ApplyDecision` checks the action against `allowedFor(failure)`.
- On a pr-open piece: `goto` sets the piece to `running` and clears its wait; `escalate`, and any decision that can't be used, open the `question` wait with an empty `Back` (the piece stays pr-open).
- `ApplyAnswer` on a pr-open piece's `question` or `limit` wait: `retry` returns no run step, and sets `SentBack["conflict"]` and `SentBack["checks-failed"]` to 0 (the principal said to try again); `goto` sets the piece to `running`; `cancel` is unchanged.
- The prompt's wording is unchanged. `failureText` prints `Reason` as today; for these two failures it now carries the failing checks or the conflicting files.

- [ ] **Step 1: Write the failing tests:**
  - `TestApplyDecision_RetryRefusedForChecksFailed`: → a `question` wait whose text contains `"retry" is not an action it may take`, `Back == ""`, status `pr-open`.
  - `TestApplyDecision_GotoFromPROpenRuns`: goto `build` → status `running`, no wait, step `goto build`.
  - `TestApplyDecision_EscalateKeepsPROpen`.
  - `TestApplyAnswer_RetryOnPROpenResetsSendBacks`: counts 2 and 2 → both 0, no step, status `pr-open`, no wait.
  - `TestApplyAnswer_GotoOnPROpenRuns`.
  - `TestSummary_ForPR`: `Allowed` has no `retry`; the prompt's "What happened" section holds `failure: conflict` and the problem text; stage 1's `TestSummary…` tests still pass.
- [ ] **Step 2: Run** `go test ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/mission/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: decisions and answers for a piece whose pull request is open`

### Task 9: Engine effects — observe, merge, resolve a conflict

**Files:** Create `internal/mission/effects_pr.go`, `internal/mission/effects_pr_test.go`. Modify `internal/mission/ops.go` (`GitOps`, `GHOps`, `SwitchReader`), `engine.go` (`Engine` fields, `observe`), `effects.go` (`perform`, `openPR`, `sendBack`, `askOrchestrator`), the engine test fakes, `cmd/crew/missionrun.go` (`newMissionEngine`).

**Interfaces (Consumes):** everything from Tasks 2 to 8.

**Interfaces (Produces):**
```go
// ops.go
type SwitchReader interface {
    Switches(repo string) (map[string]principal.SwitchRecord, error) // principal.Store satisfies it
}
// GitOps gains Head, ChangedFiles, MergeInto (Task 4 signatures).
// GHOps gains MergePR, FailedLog (Task 3 signatures).

// Engine gains:
Switches    SwitchReader // nil: every domain is off
MergeMethod string       // from Settings.Method()
```

**Behaviour:**
- **observe.** For each pr-open piece, after its pull request: read `Switches(t.s.Repo)` once per tick and `Git.ChangedFiles(worktree, "origin/"+base)`, and set `Obs.Auto[id] = EffectiveMode(...)`. If either read fails, leave the piece out of `Obs.Auto` and note it. For `ErrTampered` the note is: `automerge.json was edited outside crew, so auto-merge is off everywhere; delete it and its .sum, then set the switches again with crew automerge`. A piece with no worktree is left out without a note.
- **openPR.** After the push, `head := Git.Head(worktree)`; call `PROpened(s, piece, pr, head, t.now)`. A failed `Head` is a note and the pull request is not recorded this tick.
- **merge** (`Merge`). Read the switch and the changed files again and recompute `EffectiveMode`. If the mode is now `off`, or it is `on` and `a.Checked` is false, or either read fails, note `auto-merge for PR #n was turned off; not merging` and stop. Then `GH.MergePR(PR, MergeMethod, a.Head)`. On an error: note `can't merge PR #n: <err> (the merge method is <method>; set merge_method in missions.yaml)`, state unchanged. On success: `Merged`, save, record `checks-passed` when `a.Checked`, record `pr-merged` with `PR #n merged by crew (auto-merge <mode> for <domain>)`, then remove the worktree with force.
- **resolveConflict** (`ResolveConflict`). If `Git.Dirty` is not empty, note and stop. `Git.Fetch(base)`, then `Git.MergeInto(worktree, "origin/"+base)`.
  - No conflicts: run the repo gate (`repoAllowsPush`), `Git.Push`, `Git.Head`, `BranchUpdated`, save, record `conflict` with `main was merged into the branch; checks run again`.
  - Conflicts: send the piece back to the rework station with the note `main has moved and these files conflict: <files>. Merge origin/<base> into this branch, resolve the conflicts and commit.` (at most 20 files, then `(and N more)`); then `SentBackDone(s, piece, "conflict", "conflicts with <base> in: <files>")`, save, record `conflict` with the files.
- **sendBack** for `checks-failed`: before the run step, add to the note the end of each failed check's log from `GH.FailedLog` (at most 3 checks, under `--- <name> ---`); a log that can't be read is skipped. On success: `SentBackDone(s, piece, "checks-failed", "failed checks: <names>")`, record `checks-failed` with the names. For `uncommitted` pass `""` as the problem (behaviour unchanged).
- **askOrchestrator** for `conflict` and `checks-failed`: `sum = sum.ForPR(a.Failure, ps.Problem)`.
- **`newMissionEngine`** sets `Switches: principal.Store{Dir: filepath.Join(crewDir, "principal")}` and `MergeMethod: settings.Method()`.

- [ ] **Step 1: Extend the fakes** in the engine tests: the fake git gets `Head`, `ChangedFiles`, `MergeInto`; the fake gh gets `MergePR` (records calls, can fail) and `FailedLog`; add a fake `SwitchReader`.
- [ ] **Step 2: Write the failing tests** (`effects_pr_test.go`, each drives `Engine.Tick`):
  - `TestEngine_AutoMergeOnMergesPassingPR`: switch on, checks pass, head matches → `MergePR(n, "squash", head)` called once; piece `complete`; events `checks-passed` and `pr-merged` (containing `merged by crew`); the worktree removal was called with force; the next tick starts the piece that waited for it.
  - `TestEngine_SwitchOffLeavesPRWaiting`: no `MergePR` call; a `merge` wait.
  - `TestEngine_MergeRereadsSwitch`: the fake switch returns `on` for the first read of the tick and `off` for the second → no `MergePR` call; a note containing `turned off`.
  - `TestEngine_TamperedSwitchNeverMerges`: `Switches` returns `ErrTampered` → no `MergePR`; a `merge` wait; a note containing `automerge.json`.
  - `TestEngine_MergeRefusedIsANote`: `MergePR` fails → piece still `pr-open`, a note containing `merge_method`; when the next tick observes `MERGED` the piece completes.
  - `TestEngine_OtherDomainOffBlocks`: `ChangedFiles` returns a file under the design domain's paths, design off → no merge; the wait names `design`.
  - `TestEngine_ChecksFailedSendsBackWithLog`: → `Runs.Step` got a `goto` to the rework station whose note holds the check name and the fake log text; piece `running`; `SentBack["checks-failed"] == 1`; event `checks-failed`; the state's `Problem` holds the check name and not the log text.
  - `TestEngine_ConflictMergedCleanly`: `MergeInto` returns no conflicts → `Push` called, `Head` updated, piece still `pr-open`, event `conflict`.
  - `TestEngine_ConflictSendsBack`: `MergeInto` returns `["f.go"]` → no push; `goto` note names `f.go`; piece `running`; `SentBack["conflict"] == 1`.
  - `TestEngine_SentBackPieceReturnsToSamePR`: after a send-back, the run finishes again → push, `FindPR` finds the open pull request, the piece is `pr-open` with the new head and a fresh `PRSince`.
  - `TestEngine_OpenPRRecordsHead`.
  - `TestEngine_ConflictAskUsesProblem`: count 2 → the fake orchestrator's summary has `Failure == "conflict"`, a `Reason` naming the files, and no `retry` in `Allowed`.
- [ ] **Step 3: Run** `go test ./internal/mission/` — Expected: FAIL.
- [ ] **Step 4: Implement.**
- [ ] **Step 5: Run** `go test ./... && go test -race ./internal/mission/` — Expected: PASS.
- [ ] **Step 6: Commit** `feat: the engine merges, resolves conflicts and sends failed checks back`

---

## Phase D: the command, status and the guide

### Task 10: `crew automerge`

**Files:** Create `cmd/crew/automerge.go`, `cmd/crew/automerge_test.go`. Modify `cmd/crew/main.go` (dispatch, usage).

**Interfaces (Consumes):** `principal.Store.Switches`, `SetSwitches`, `Confirm`, `CheckRepo`; `gh.Client.Repo`, `HasChecks`; `source.LoadDomains`; the `openTTY` and `ghClient` test seams in `cmd/crew/repo.go`.

**Usage text:**
```
usage:
  crew automerge on  (--domain D | --all) [--without-checks] [--project P] [--dir D]
  crew automerge off (--domain D | --all) [--project P] [--dir D]
  crew automerge status [--project P] [--dir D]
on and off need a terminal: you type a confirmation that names the repo and the domain.
```

**`on` and `off`, in order:** open the terminal (fail with `crew automerge needs a terminal: <err>`); read the repo through `gh`; require `principal.CheckRepo` to pass (`<slug> isn't confirmed; run crew repo confirm first`); resolve the domains (`--domain` must be a defined domain, `--all` is every defined one, exactly one of the two is required, `--without-checks` is refused with `off`); for `on` without `--without-checks`, require checks; print the block; `principal.Confirm`; `SetSwitches`; print `auto-merge <mode> for <domains> in <slug>`.

**"Has checks"** is `HasChecks(defaultBranch)` or at least one `.yml`/`.yaml` file in `<repo>/.github/workflows`. When neither holds, `on` fails with:
```
<slug> has no checks: nothing ran on the latest commit of <base>, and there is no workflow in .github/workflows.
With auto-merge on, crew merges a pull request only after a check passes, so nothing would ever merge here.
Add CI, or run crew automerge on --domain <d> --without-checks to merge as soon as the factory finishes.
```

**The block written to the terminal** (`<domains>` is comma-separated; `<phrase>` is `<slug> <domain>`, or `<slug> all` with `--all`):
```

Repo:     <slug> (<visibility>)
Domains:  <domains>
Mode:     <mode line>

<warning>

Type "<phrase>" to confirm: 
```
- `on` mode line: `on. crew merges a pull request of this domain into <base> once it has at least one check and every check has passed.`
- `on --without-checks` mode line: `on, without checks. crew merges as soon as the factory finishes.`
- `off` mode line: `off. crew leaves this domain's pull requests for you to merge.`
- `on` warning: `If merging to <base> deploys this repo, this turns on automatic deploys.`
- `--without-checks` warning: `Nothing but the factory's own review station stands between an agent's work and <base>. If merging to <base> deploys this repo, this turns on automatic deploys of unchecked work.`
- `off`: no warning paragraph.

**`status`** needs no terminal. One line per defined domain, sorted: `<domain>  <mode>  set <YYYY-MM-DD>`, or `<domain>  off` with no record. A tampered store is an error (non-zero exit): `<path>/automerge.json was edited outside crew, so every switch reads as off; delete it and its .sum, then set the switches again with crew automerge`.

- [ ] **Step 1: Write the failing tests** (a pipe-backed fake terminal through `openTTY`, a fake `gh` through `ghClient`):
  - `TestAutomerge_OnRecordsAfterTypedPhrase`: typing `acme/app product` → store has `product on`; the terminal text holds the slug, `product`, and `automatic deploys`.
  - `TestAutomerge_WrongPhraseRecordsNothing`: typing `yes` → error, store empty.
  - `TestAutomerge_NoTerminalFails`: `openTTY` fails → error containing `needs a terminal`, and nothing was read from stdin (pass a stdin holding the right phrase).
  - `TestAutomerge_OnRefusesRepoWithoutChecks`: `HasChecks` false, no workflows folder → the three-line message; nothing recorded. With a `.github/workflows/ci.yml` file → goes on to the confirmation.
  - `TestAutomerge_WithoutChecksSkipsTheCheckAndWarns`: terminal text holds `unchecked work`; store has `without-checks`.
  - `TestAutomerge_AllSetsEveryDomain`: three domains, phrase `acme/app all` → three records.
  - `TestAutomerge_OffNeedsThePhraseToo`.
  - `TestAutomerge_UnknownDomain`, `TestAutomerge_DomainAndAllTogetherRefused`, `TestAutomerge_UnconfirmedRepoRefused`.
  - `TestAutomerge_Status`: lines for on, without-checks and an unset domain; a tampered store → error containing `automerge.json`.
- [ ] **Step 2: Run** `go test ./cmd/crew/ -run Automerge` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: crew automerge, set in a terminal per domain`

### Task 11: Status, the orchestrator's instructions, the guide, end to end

**Files:** Modify `cmd/crew/missionstatus.go`, `cmd/crew/mission.go` (`missionPlan`, `awaitGo` output), `internal/mission/orchestrator.go` (`OrchestratorBody`), `docs/missions.md`, `README.md` (the missions section's command list), `cmd/crew/testdata/fakegh.sh`, `cmd/crew/mission_e2e_test.go`, and the tests beside each.

**Behaviour:**
- **Status.** After "Waiting on you", a section `Open pull requests` lists each pr-open piece with no wait: `<piece>  PR #<n>  <url>  auto-merge <mode>`, where the mode is the piece's own domain's. When the store can't be read, the mode prints as `off (switch unreadable)`.
- **Plan and start output.** Under the piece table, one line: `Auto-merge in <slug>: <domain> <mode>, …` over the domains the plan uses, so the principal sees what will merge without them before saying go.
- **Orchestrator body.** Add: what the three modes mean; that with auto-merge on crew merges on its own and nothing is asked; that a `merge` wait's text says why a pull request is waiting; that `retry` on a conflict or failed-checks question means "look again" and gives the piece two more tries; and that the orchestrator never runs `crew automerge` and tells the principal to run it in their own terminal when they ask for it.
- **Guide.** A section "Auto-merge": the command, the three modes, what the confirmation looks like, the no-checks refusal, the rule about another domain's files, what happens on failed checks and on a conflict (two tries, then the orchestrator, then you), the five reasons a pull request can wait with auto-merge on, `merge_method`, and that merging is deploying where the repo makes it so. Update the wait table's `merge` row and the stage 1 sentences that say nothing merges.
- **`fakegh.sh`** learns `pr merge` (marks the pull request merged, refuses a head that isn't the recorded one) and returns `mergeable`, `headRefOid` and a `statusCheckRollup` read from a file the test writes.

- [ ] **Step 1: Write the failing tests:**
  - `TestStatus_OpenPullRequestsSection`; `TestStatus_UnreadableSwitch`.
  - `TestMissionPlan_PrintsAutoMergeLine`.
  - `TestOrchestratorBody_AutoMerge`: the body contains `crew automerge` only in sentences that say never to run it, and names `without-checks`.
  - `TestDocs_ExamplePlansParse` still passes.
  - `TestMission_EndToEnd_AutoMergeOn`: real git with a bare origin, the fake `gh`, the fake harness; the switch written through `principal.Store.SetSwitches`; two pieces where the second waits for the first; checks file says one passing check → both pull requests are merged by crew with no answer from the test, the second piece's worktree was cut from a base that holds the first piece's commit, and the mission ends `done`.
  - `TestMission_EndToEnd_ChecksFailThenPass`: the checks file says `fail` for the first head → the piece goes back to the factory, commits again, and the same pull request merges once the file says `pass`.
- [ ] **Step 2: Run** `go test ./cmd/crew/ ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go build ./... && go vet ./... && go test -count=1 ./... && go test -race ./internal/mission/ ./internal/gitx/ ./internal/gh/ ./internal/principal/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: auto-merge in status, the orchestrator's instructions and the guide`
