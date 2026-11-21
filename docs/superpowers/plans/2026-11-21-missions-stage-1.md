# crew missions, stage 1: one piece at a time — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The principal gives the `orchestrator` agent a job; crew turns it into a checked mission, runs its pieces one at a time in git worktrees, opens one PR per piece, waits for the principal to merge, and tells them what is waiting.

**Architecture:** `internal/mission` holds a pure state machine (`Decide` plus reducers), the mission folder store, and an engine that ticks: observe, decide, act. Side effects sit behind small interfaces (git, gh, runs, orchestrator, notifier) so the engine is tested with fakes. Each piece's run is the existing `run.Engine`, driven in-process with its work directory set to the piece's worktree. `cmd/crew` adds `mission`, `domain` and `repo` commands; `crew compile` emits the `orchestrator` agent.

**Tech Stack:** Go 1.22 module (toolchain 1.24), `gopkg.in/yaml.v3` only, stdlib `testing`, the `git`, `gh` and `osascript` CLIs.

**Spec:** `docs/superpowers/specs/2026-11-18-orchestrator-missions-design.md`. Read it before any task. This plan covers **stage 1 of its Build order only**.

**Not in this plan** (later stages, each with its own plan): the auto-merge switch, check watching and conflicts (2); Jira and deliverables (3); concurrency above 1, the overlap check, the path check and the steps limit (4); delegation (5); removing foremen (6). `delegate: true` is refused with a message that says so. The spec's golden planner missions also wait: they need real jobs to be written from.

## Global Constraints

- The only dependency is `gopkg.in/yaml.v3`. No new modules.
- Before writing Go, load the `use-modern-go` skill and follow it for Go 1.22.
- Config and frontmatter decode strictly: unknown keys are errors.
- `verify` levels are unchanged: warnings are fatal.
- State is immutable: every state-machine function takes a `State` and returns a new one. Copy slices and maps; never modify the argument.
- **One writer per mission.** Only the process holding the mission lock writes `state.json` and `mission.md`. Every other command writes to `inbox/`, or writes state only when no live process holds the lock.
- Every external command runs through `exec` with an argument list. Never build a shell string. Text from the principal or an agent travels by stdin or a file.
- Tests never call the real `gh`, a git remote, `osascript` or a harness CLI. Use fakes, and local temp repos for git.
- No headless command may use a permission-bypass flag (existing verify rule). Every session crew starts gets deny rules.
- `crew repo confirm` reads its confirmation from `/dev/tty`, never stdin, and has no flag that skips it.
- No secret, token or file content in state, events, logs or notifications.
- Files stay under 400 lines and functions under 50 lines.
- Paths: mission folder `<repo>/.crew/missions/<id>/`; worktree `<repo>/.crew/worktrees/<mission-id>-<piece-id>`; piece branch `crew/<mission-id>/<piece-id>`; run folders stay in `<repo>/.crew/runs/`.
- Mission id: `m-<YYYYMMDD-HHMMSS>-<4 hex>`. The orchestrator agent is named `orchestrator`.
- Every commit message uses the conventional `type: description` form and ends with the `Co-Authored-By` and `Claude-Session` lines of the session doing the work. The controller passes them to each implementer.

## Review Focus

1. **The engine process dies while a piece is running.** `crew mission resume` continues the run from where it was, and a piece that already has a PR is never run again. → Task 14 `TestEngine_ResumeRestartsDeadRun`, `TestEngine_NeverRerunsPieceWithPR`.
2. **`gh` is missing, logged out, or the network is down mid-mission.** The tick records a note, changes no piece status, and tries again on the next tick. No piece is `pr-open` without a PR number. → Task 14 `TestEngine_GHFailureRetriesNextTick`.
3. **The principal's words contain backticks, `$`, `---`, or a line reading `## Changes`.** They arrive in `mission.md` byte for byte, and parsing the file back gives the same request. → Task 9 `TestMission_RequestRoundTripsHostileText`.
4. **The orchestrator's reply has no decision block, names an action that isn't allowed, or repeats one it already tried.** The piece goes to the principal with the reason. The engine never asks again in a loop. → Task 12 `TestApplyDecision_InvalidEscalates`, `TestApplyDecision_RepeatEscalates`.
5. **`repos.json` is edited by hand, or the confirmation is piped in.** A changed file reads as "not confirmed" with a message naming the file. A confirmation with no terminal fails. → Task 8 `TestStore_TamperedReadsAsUnconfirmed`, `TestConfirm_NoTerminalFails`.

---

## Phase A: Config and foundations

### Task 1: Domains

**Files:** Create `internal/source/domain.go`, `internal/source/domain_test.go`, `internal/verify/domains.go`, `internal/verify/domains_test.go`, `cmd/crew/domain.go`, `cmd/crew/domain_test.go`, `internal/scaffold/template/domains/.gitkeep`. Modify `internal/verify/verify.go` (`Project`, `Run`), `cmd/crew/main.go` (dispatch, usage).

**Interfaces (Produces):**
```go
const DomainsDir = "domains"
type Domain struct {
    Name        string   `yaml:"name"`
    Description string   `yaml:"description"`
    Factories   []string `yaml:"factories"`
    Paths       []string `yaml:"paths"`
    Releases    []string `yaml:"releases"`
    Body, Path  string   // yaml:"-"
}
func LoadDomains(root string) ([]Domain, []error) // domains/*.md; a missing folder is zero domains
// verify.Project gains: Domains []source.Domain
```
- `verify` errors: a listed factory that doesn't exist, two domains with one name, a name that differs from the file stem, an empty description, an empty `factories`.
- A non-empty `releases` is an **info** note ("releases are checked from Project 2"), never a warning.
- `crew domain list [--dir D]` prints `name  N factories  description`, one per line.

- [ ] **Step 1: Write failing tests:**
  - `TestLoadDomains_ParsesFrontmatterAndBody`
  - `TestLoadDomains_MissingFolderIsEmpty`
  - `TestLoadDomains_UnknownKey` (error contains `unknown key "colour"`)
  - `TestVerify_DomainUnknownFactory` (error contains `domain design: no factory "ui-mockup"`)
  - `TestVerify_DomainReleasesIsInfo` (report is `OK(false)`)
  - `TestDomainList_Prints`
- [ ] **Step 2:** Run `go test ./internal/source ./internal/verify ./cmd/crew`. Expected: FAIL. **Step 3:** Implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: domains as crew config`.

### Task 2: Mission settings

**Files:** Create `internal/mission/settings.go`, `internal/mission/settings_test.go`, `internal/scaffold/template/missions.yaml`.

**Interfaces (Produces):**
```go
const SettingsFile = "missions.yaml"
type Limits struct {
    Concurrency       source.Limit `yaml:"runs_at_once"`
    Depth             source.Limit `yaml:"delegation_depth"`
    Pieces            source.Limit `yaml:"pieces"`
    Steps             source.Limit `yaml:"steps"`
    OrchestratorCalls source.Limit `yaml:"orchestrator_calls_per_piece"`
    Additions         source.Limit `yaml:"orchestrator_additions"`
}
type Settings struct {
    Version     int               `yaml:"version"`
    Approval    string            `yaml:"mission_approval"`     // "on" | "off"
    Harness     string            `yaml:"orchestrator_harness"` // harness that runs headless orchestrator calls
    RemindAfter string            `yaml:"remind_after"`         // Go duration
    Limits      Limits            `yaml:"limits"`
    Repos       map[string]Limits `yaml:"repos"` // "owner/name" → overrides; unset fields inherit
}
func LoadSettings(crewDir string) (Settings, error)
func (s Settings) For(repo string) Limits // defaults merged with that repo's overrides
func (s Settings) Remind() time.Duration
func Allows(l source.Limit, n int) bool   // true when n is within the limit ("*" is always true)
```
- The template file sets: `mission_approval: on`, `orchestrator_harness: claude-code`, `remind_after: 30m`, and limits `3`, `2`, `12`, `60`, `3`, `2` in the order above.
- `LoadSettings` errors when the file is missing (`missions.yaml not found in <dir>; run crew mission setup`), when any top-level limit is unset (`limits.pieces: a number or "*" is required`), or when `mission_approval` isn't `on` or `off`.

- [ ] **Step 1: Write failing tests:**
  - `TestLoadSettings_TemplateDefaults` (loads the scaffold template; `Limits.Pieces.N == 12`, `Remind() == 30*time.Minute`)
  - `TestLoadSettings_MissingLimitIsError`
  - `TestLoadSettings_BadApproval`
  - `TestSettings_ForMergesRepoOverride` (override sets `pieces: 20`; `For("acme/app").Pieces.N == 20`, `OrchestratorCalls.N == 3`)
  - `TestAllows_StarAndNumber`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: mission settings and limits`.

### Task 3: Run engine additions

**Files:** Modify `internal/source/factory.go`, `internal/factory/validate.go`, `internal/run/state.go`, `internal/run/machine.go`; tests in the matching `_test.go` files.

**Interfaces (Produces):**
```go
// source.Factory gains:
Rework string `yaml:"rework"`
func (f Factory) ReworkStation() string // Rework, or Start when unset
// run.State gains:
LimitRoute string `json:"limit_route,omitempty"` // "station.route" whose limit ended the run
// run gains:
func GotoFresh(f source.Factory, s State, station, reason string) (State, error)
```
- `factory.Validate` errors when `rework` names no station.
- `AfterStation` sets `LimitRoute` when a route exceeds its `max`. `Goto`, `GotoFresh`, `Resume` and `Approve` clear it.
- `GotoFresh` behaves like `Goto`, except `RouteCounts` becomes empty and `Reason` is the given reason.

- [ ] **Step 1: Write failing tests:**
  - `TestParseFactory_Rework` and `TestReworkStation_DefaultsToStart`
  - `TestValidate_ReworkUnknownStation`
  - `TestAfterStation_LimitSetsLimitRoute` (`LimitRoute == "review.fix"`)
  - `TestGotoFresh_ClearsCountsAndReopens` (a failed run with `RouteCounts["review.fix"] == 3` reopens `interrupted` at `plan` with empty counts)
  - `TestGotoFresh_DoesNotMutateInput`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: rework station and fresh goto for runs`.

### Task 4: Deny rules for every session crew starts

**Files:** Create `internal/run/deny.go`, `internal/run/deny_test.go`. Modify `internal/config/config.go`, `internal/verify/headless.go`, `internal/run/runner.go`, `internal/run/engine.go`, `internal/run/step.go`, `cmd/crew/run.go`, `internal/scaffold/template/harnesses.yaml`; tests alongside.

**Interfaces (Produces):**
```go
// config.Harness gains:
Deny DenySpec `yaml:"deny"`
type DenySpec struct {
    All      []string `yaml:"all"`      // native rule strings for every session; {crew_dir} is expanded
    Stations []string `yaml:"stations"` // extra rules for stations and headless orchestrator calls
}
// config.HeadlessSpec gains:
DenyVia string `yaml:"deny_via"` // "settings-file" | "" (this harness can't apply deny rules)
// run:
func DenyRules(reg *config.Registry, harness, crewDir string, station bool) []string
func CanDeny(h config.Harness) bool
// run.StationRequest gains: Deny []string
// run.Engine gains: CrewDir string
```
- `harnesses.yaml` for `claude-code`: add `--settings`, `"{settings}"` to `headless.command` before `--permission-mode`, set `deny_via: settings-file`, and add:
  ```yaml
  deny:
    all:
      - "Bash(gh pr merge:*)"
      - "Bash(gh pr close:*)"
      - "Bash(git push --force:*)"
      - "Bash(git push -f:*)"
      - "Bash(git push origin main:*)"
      - "Bash(crew automerge:*)"
      - "Bash(crew repo confirm:*)"
      - "Bash(crew release approve:*)"
      - "Edit({crew_dir}/**)"
      - "Write({crew_dir}/**)"
    stations:
      - "Bash(crew run:*)"
      - "Bash(crew mission:*)"
  ```
- `HeadlessRunner` writes `{"permissions":{"deny":[…]}}` to a temp file, puts its path where `{settings}` is, and removes it after the station exits. With no rules, the `{settings}` argument and the flag before it are dropped, as `{allowed}` already is.
- `Engine.request` sets `Deny: DenyRules(e.Registry, harness, e.CrewDir, true)`. `drive` in `cmd/crew/run.go` passes `CrewDir`.
- verify: `{settings}` is a known token; `deny_via: settings-file` needs it and a non-empty `deny.all`; any other `deny_via` value is an error.

- [ ] **Step 1: Write failing tests:**
  - `TestDenyRules_ExpandsCrewDirAndAddsStationRules`
  - `TestCanDeny_FalseWithoutDenyVia`
  - `TestHeadlessRunner_WritesSettingsFileAndRemovesIt` (with a fake harness script that copies the file it was given after `--settings`: the copy contains `Bash(gh pr merge:*)`, and the original no longer exists afterwards)
  - `TestExpandCommand_DropsSettingsFlagWhenNoRules`
  - `TestEngine_StationRequestCarriesDenyRules`
  - `TestVerify_DenyViaNeedsSettingsToken`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: deny rules on every headless session`.

### Task 5: git wrapper

**Files:** Create `internal/gitx/gitx.go`, `internal/gitx/gitx_test.go`.

**Interfaces (Produces):**
```go
type Repo struct{ Dir string } // the main checkout
func (r Repo) Root() (string, error)
func (r Repo) Fetch(branch string) error                    // git fetch origin <branch>
func (r Repo) AddWorktree(path, branch, from string) error  // git worktree add -b <branch> <path> <from>
func (r Repo) RemoveWorktree(path string, force bool) error // nil when the worktree is already gone
func (r Repo) Worktrees() ([]string, error)                 // absolute paths
func (r Repo) Dirty(worktree string) ([]string, error)      // paths from `status --porcelain`; ignored files don't count
func (r Repo) Ahead(worktree, base string) (int, error)     // commits in base..HEAD
func (r Repo) Push(worktree, branch string) error           // git push -u origin <branch>
```
- Errors carry the last lines of git's stderr.

- [ ] **Step 1: Write failing tests** (each builds a temp repo, with a bare repo as `origin` where needed):
  - `TestAddWorktree_CreatesBranchFromBase`
  - `TestRemoveWorktree_TwiceIsNil`
  - `TestRemoveWorktree_RefusesDirtyWithoutForce`
  - `TestDirty_ListsUntrackedAndModified`
  - `TestAhead_CountsCommits`
  - `TestPush_SendsBranchToOrigin`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: git worktree wrapper`.

### Task 6: gh wrapper

**Files:** Create `internal/gh/gh.go`, `internal/gh/gh_test.go`.

**Interfaces (Produces):**
```go
type Exec func(ctx context.Context, dir string, stdin []byte, name string, args ...string) (stdout []byte, err error)
type Client struct{ Dir string; Exec Exec } // Exec nil = real exec
type RepoInfo struct{ Slug, Visibility, DefaultBranch string } // Visibility: "public" | "private" | "internal"
type PR struct{ Number int; State, URL string }                // State: "OPEN" | "MERGED" | "CLOSED"
func (c Client) Repo(ctx context.Context) (RepoInfo, error)    // gh repo view --json nameWithOwner,visibility,defaultBranchRef
func (c Client) CreatePR(ctx context.Context, head, base, title, body string) (PR, error) // body via --body-file -
func (c Client) PRState(ctx context.Context, number int) (PR, error)
```
- `Repo` lowercases the visibility. A missing `gh` gives `gh not found on PATH; install it and run gh auth login`.

- [ ] **Step 1: Write failing tests** with a fake `Exec` that records calls and returns canned JSON:
  - `TestRepo_ParsesSlugVisibilityBranch`
  - `TestCreatePR_SendsBodyOnStdin` (the body never appears in `args`)
  - `TestPRState_Merged`
  - `TestClient_ErrorCarriesStderr`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: gh wrapper for repo info and PRs`.

### Task 7: Notifications

**Files:** Create `internal/notify/notify.go`, `internal/notify/notify_test.go`.

**Interfaces (Produces):**
```go
type Notifier interface{ Notify(title, body string) error }
type Nop struct{}
func New(goos string, exec func(name string, args ...string) error) Notifier // "darwin" → osascript; anything else → Nop
```
- macOS runs `osascript -e 'on run argv' -e 'display notification (item 2 of argv) with title (item 1 of argv)' -e 'end run' <title> <body>`, so the text is an argument and never part of the script.

- [ ] **Step 1: Write failing tests:**
  - `TestNew_DarwinPassesTextAsArgv` (title and body are the last two arguments, unchanged, for a body containing `"` and `\`)
  - `TestNew_OtherOSIsNop`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: desktop notifications`.

### Task 8: Repo confirmation

**Files:** Create `internal/principal/store.go`, `internal/principal/confirm.go`, `internal/principal/principal_test.go`, `cmd/crew/repo.go`, `cmd/crew/repo_test.go`. Modify `cmd/crew/main.go`.

**Interfaces (Produces):**
```go
type RepoRecord struct {
    Repo        string    `json:"repo"`       // "owner/name"
    Visibility  string    `json:"visibility"`
    ConfirmedAt time.Time `json:"confirmed_at"`
}
type Store struct{ Dir string } // <crewDir>/principal
var ErrTampered = errors.New("…")
func (s Store) Repo(slug string) (RepoRecord, bool, error) // ErrTampered when repos.json doesn't match repos.json.sum
func (s Store) ConfirmRepo(rec RepoRecord) error           // writes repos.json, then its sha256 to repos.json.sum
func Confirm(tty io.ReadWriter, question, want string) error // prints the question, reads one line, errors unless it equals want
func OpenTTY() (*os.File, error)                           // opens /dev/tty; error when there is no terminal
func CheckRepo(s Store, info gh.RepoInfo) error            // nil only when confirmed for this exact visibility
// cmd/crew:
var openTTY = principal.OpenTTY
func repoCmd(args []string, out io.Writer) error
```
- `crew repo confirm [--project P] [--dir D]` prints the repo slug, **PUBLIC** or **private**, the remote, and "missions will push piece branches and open PRs here". It then asks the principal to type the repo slug.
- `crew repo status` prints whether this repo is confirmed, and for which visibility.
- `CheckRepo` messages: `this repo isn't confirmed; run crew repo confirm in a terminal`, and `this repo was confirmed as private and is now public; run crew repo confirm again`.

- [ ] **Step 1: Write failing tests:**
  - `TestStore_ConfirmThenRead`
  - `TestStore_TamperedReadsAsUnconfirmed` (edit `repos.json` by hand; `Repo` returns `ErrTampered` and `CheckRepo`'s message names the file)
  - `TestConfirm_WrongTextFails` and `TestConfirm_ExactTextPasses`
  - `TestConfirm_NoTerminalFails` (`repoCmd` with `openTTY` replaced by one that errors returns an error containing `needs a terminal`, even with the right text on stdin)
  - `TestCheckRepo_VisibilityChanged`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: one-time repo confirmation by the principal`.

---

## Phase B: The mission, as data

### Task 9: Mission file, checks and diagram

**Files:** Create `internal/mission/mission.go`, `internal/mission/check.go`, `internal/mission/diagram.go`, and their `_test.go` files.

**Interfaces (Produces):**
```go
type Piece struct {
    ID       string   `yaml:"id"`
    Domain   string   `yaml:"domain"`
    Factory  string   `yaml:"factory"`
    Delegate bool     `yaml:"delegate"`
    Brief    string   `yaml:"brief"`
    Paths    []string `yaml:"paths"`
    WaitsFor []string `yaml:"waits_for"`
}
type Change struct{ Time time.Time; By, What, Why string }
type Mission struct {
    Pieces  []Piece  // frontmatter key: pieces
    Request string   // the body, byte for byte
    Changes []Change
}
func Parse(src []byte) (Mission, error)
func (m Mission) Render() []byte
func (m Mission) Piece(id string) (Piece, bool)
func (m Mission) WithChange(c Change) Mission
type CheckInput struct {
    Domains   []source.Domain
    Factories []source.Factory
    Registry  *config.Registry
    Limits    Limits
}
func Check(m Mission, in CheckInput) []error
func Mermaid(m Mission) string // flowchart TD; one node per piece, one arrow per wait
```
- File layout: YAML frontmatter, then the request, then the line `<!-- crew:changes -->`, then `## Changes` with one `- <RFC3339> · <by> · <what> · <why>` line per change. `Parse` splits on the **last** marker line, so a request may contain `---`, `## Changes`, backticks or `$`.
- `Check` errors, each naming the piece:
  - an empty or duplicate `id`; an id not matching `^[A-Za-z0-9][A-Za-z0-9_-]*$`
  - an unknown `domain`; a `factory` that doesn't exist or isn't in the domain's `factories`
  - `delegate: true` (`piece P3: delegation isn't available yet`); both or neither of `factory` and `delegate`
  - an empty `brief`; an empty request
  - a `waits_for` naming no piece, itself, or forming a circle (`P2 and P4 wait on each other`)
  - more pieces than `Limits.Pieces`
  - a station whose harness can't apply deny rules (`run.CanDeny`)
  - a factory `watch` root that is absolute or starts with `..`

- [ ] **Step 1: Write failing tests:**
  - `TestMission_RequestRoundTripsHostileText` (request `"a `$(rm)` b\n---\n## Changes\n- not a change\n"`; `Parse(m.Render()).Request` is identical and `Changes` is empty)
  - `TestMission_ChangesRoundTrip`
  - `TestParse_UnknownPieceKey`
  - `TestCheck_Valid` (the five-piece acme plan from the spec, without the delegated piece)
  - `TestCheck_FactoryNotInDomain`, `TestCheck_DelegateRefused`, `TestCheck_WaitCycle`, `TestCheck_TooManyPieces`, `TestCheck_HarnessCannotDeny`, `TestCheck_WatchOutsideRepo`
  - `TestMermaid_NodesAndWaits` (output contains `P1 --> P2`)
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: mission file, checks and diagram`.

### Task 10: Mission folder

**Files:** Create `internal/mission/state.go`, `internal/mission/store.go`, `internal/mission/events.go`, `internal/mission/store_test.go`.

**Interfaces (Produces):**
```go
type Status string      // "planned" | "awaiting-go" | "running" | "stopped" | "done"
type PieceStatus string // "queued" | "running" | "pr-open" | "waiting" | "complete" | "cancelled"
type Wait struct {
    ID, Kind, Question string // Kind: "merge" | "approval" | "question" | "limit" | "pr-closed"
    Since              time.Time
    Reminded           int
    Back               PieceStatus // status to return to once answered
}
type Decision struct {
    Time                                   time.Time
    By, Failure, Action, Station, Note, Reason string // By: "engine" | "orchestrator" | "principal"
}
type PieceState struct {
    ID                    string
    Status                PieceStatus
    RunID, Branch, Worktree string
    PR                    int
    PRURL                 string
    Resumes, Calls        int
    SentBack              map[string]int // reason → times
    Decisions             []Decision
    Wait                  *Wait
    Added                 bool
}
type State struct {
    MissionID string
    Status    Status
    Repo      string // "owner/name"
    Base      string // default branch
    Pieces    []PieceState
    NextWait  int
    Additions int
}
func (s State) Piece(id string) (PieceState, bool)
func NewState(id string, m Mission, info gh.RepoInfo) State

type Store struct{ Dir string }
func New(repoRoot string, m Mission, info gh.RepoInfo, now time.Time) (Store, State, error)
func Open(repoRoot, id string) (Store, error)
func List(repoRoot string) ([]State, error) // newest first
func (s Store) Mission() (Mission, error)
func (s Store) SaveMission(m Mission) error
func (s Store) SaveApproved() error         // copies mission.md to approved.md
func (s Store) LoadState() (State, error)
func (s Store) SaveState(st State) error    // temp file + rename
func (s Store) Lock() (unlock func(), err error)
func (s Store) LockedBy() (pid int, alive bool)

type Item struct{ Name, Kind string; Body json.RawMessage } // Kind: "answer" | "amend"
func (s Store) Drop(kind string, v any) error // writes inbox/<unix-nano>-<kind>.json
func (s Store) Inbox() ([]Item, error)        // oldest first; nothing is deleted
func (s Store) Ack(items []Item) error        // deletes them

type Event struct {
    Seq   int       `json:"seq"`
    Time  time.Time `json:"time"`
    Kind  string    `json:"kind"`
    Piece string    `json:"piece,omitempty"`
    Run   string    `json:"run,omitempty"`
    By    string    `json:"by,omitempty"`
    Msg   string    `json:"msg,omitempty"`
    Count int       `json:"count,omitempty"`
    Max   string    `json:"max,omitempty"`
}
func (s Store) Emit(e Event) (Event, error)
func (s Store) EventsSince(seq int) ([]Event, error)
```
- Event kinds used in stage 1: `mission-start`, `mission-done`, `stopped`, `resumed`, `piece-queued`, `piece-start`, `piece-stopped`, `piece-complete`, `piece-cancelled`, `pr-opened`, `pr-merged`, `exception`, `decision`, `escalated`, `amendment`, `piece-added`, `rebrief`, `waiting-on-principal`, `notified`, `answered`, `note`.
- `Emit` follows `run.Store.Emit`: an `flock` on `events.lock`, then append to `events.jsonl`.
- `Lock` follows `run.Store.Lock`: the second caller gets `mission <id> is already being driven (pid N)`; a lock whose pid is dead is taken over.
- `New` calls the existing `.crew` setup, so `.crew/.gitignore` exists.

- [ ] **Step 1: Write failing tests:**
  - `TestStore_NewWritesMissionAndState` (every piece `queued`, status `planned`)
  - `TestStore_StateRoundTrip`
  - `TestStore_LockExclusiveAndStaleTakenOver`
  - `TestStore_InboxOrderAndAck` (items survive until `Ack`)
  - `TestStore_EventsSinceAssignsSeq`
  - `TestStore_SaveApprovedCopiesPlan`
  - `TestOpen_RejectsPathLikeIDs` (`../x` errors)
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: mission folder, state and events`.

---

## Phase C: The state machine

### Task 11: Decide and the reducers

**Files:** Create `internal/mission/machine.go`, `internal/mission/reduce.go`, `internal/mission/machine_test.go`.

**Interfaces (Produces):**
```go
type RunObs struct{ State run.State; Live bool }
type GitObs struct{ Ahead int; Dirty []string }
type Obs struct {
    Now  time.Time
    Runs map[string]RunObs // piece id → its run; absent when the piece has none
    Git  map[string]GitObs // present for pieces whose run is done
    PRs  map[string]gh.PR  // present for pr-open pieces
}
type Action interface{ isAction() }
type StartPiece struct{ Piece string }
type DriveRun struct{ Piece string }                          // the run should be going and nothing drives it
type SendBack struct{ Piece, Station, Reason, Note string }   // GotoFresh, then drive
type OpenPR struct{ Piece string }
type AskOrchestrator struct{ Piece, Failure string }          // Failure: "limit-hit" | "failed" | "uncommitted"
type Notify struct{ Title, Body string }
type Cleanup struct{ Piece string; Force bool }
type Record struct{ Event Event }

func Decide(m Mission, s State, o Obs, lim Limits, remind time.Duration) (State, []Action)

// Reducers: the engine calls one after the matching effect succeeded.
func Started(s State, piece, runID, branch, worktree string) State
func Resumed(s State, piece string) State
func SentBackDone(s State, piece, reason string) State
func PROpened(s State, piece string, pr gh.PR) State
func Asked(s State, piece string) State
```
**Rule: `Decide` never records an effect as done.** It returns the action; the engine runs it and calls the reducer. A failed effect changes nothing and comes back on the next tick.

Behavior of `Decide`, when the mission status is `running` (any other status returns no actions):
- **queued:** when every `waits_for` piece is `complete` and fewer than `lim.Concurrency` pieces are `running`, return `StartPiece`. Count the pieces this same call is starting. Earlier pieces in the file go first. Pieces that are `waiting` or `pr-open` don't count as running.
- **running**, by its run's state:
  - no `RunObs`, or `running`/`interrupted` and not `Live` → `DriveRun`.
  - `awaiting-approval` → status `waiting` with a `Wait` of kind `approval` whose question is the pending verdict's summary, plus `Notify`.
  - `failed` and `Retryable`: `DriveRun` while `Resumes < 2`, otherwise `AskOrchestrator{Failure: "failed"}`.
  - `failed` otherwise: `AskOrchestrator`, with `limit-hit` when `LimitRoute != ""`, else `failed`.
  - `done`, with its `GitObs`: `Dirty` non-empty → `SendBack` to the factory's rework station while `SentBack["uncommitted"] < 1`, otherwise `AskOrchestrator{Failure: "uncommitted"}`. Clean and `Ahead == 0` → `complete`, plus `Cleanup` and a `piece-complete` record. Clean and `Ahead > 0` → `OpenPR`.
  - Any `AskOrchestrator` when `Calls` has reached `lim.OrchestratorCalls` becomes a `Wait` of kind `limit` instead.
- **pr-open**, by its PR: `MERGED` → `complete`, `Cleanup{Force: true}`, `pr-merged` and `piece-complete` records. `CLOSED` → `Wait` of kind `pr-closed`. `OPEN` with no `Wait` → a `Wait` of kind `merge` and a `Notify` reading `PR #12 (P1) is waiting on you`.
- **Reminders:** for any `Wait`, when `Now - Since >= remind << Reminded`, return a `Notify` and increment `Reminded`.
- **End:** when every piece is `complete` or `cancelled`, status becomes `done`, with a `mission-done` record and a `Notify`.
- A new `Wait` gets the id `w<NextWait>`, and `NextWait` increments. It also returns a `waiting-on-principal` record.
- `approval`, `question` and `limit` waits set the piece's status to `waiting` with `Back: "running"`. `merge` and `pr-closed` waits leave it `pr-open`, so its PR is still watched.
- `Started` sets the piece `running`; `PROpened` sets it `pr-open`.

- [ ] **Step 1: Write failing tests:**
  - `TestDecide_StartsFirstReadyPiece` and `TestDecide_RespectsWaitsAndConcurrency` (with `Concurrency` 1, P3 stays queued while P1 runs)
  - `TestDecide_DoneCleanAheadOpensPR`, `TestDecide_DoneNoCommitsCompletes`
  - `TestDecide_UncommittedSentBackOnceThenAsked`
  - `TestDecide_RetryableResumedTwiceThenAsked`
  - `TestDecide_LimitHitAsksWithFailure`
  - `TestDecide_CallLimitGoesToPrincipal`
  - `TestDecide_ApprovalStopWaitsOnPrincipal`
  - `TestDecide_MergedCompletesAndUnblocks` (P2 starts on the same call that sees P1 merged)
  - `TestDecide_OpenPRNotifiesOnceThenReminds` (no second notification at 29m; one at 30m; the next at 60m)
  - `TestDecide_ClosedPRWaits`
  - `TestDecide_AllCompleteFinishes`
  - `TestDecide_NotRunningDoesNothing`
  - `TestDecide_DoesNotMutateInput` (deep-compare `s` before and after)
  - `TestReducers_ReturnNewState`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: mission state machine`.

### Task 12: Exceptions, decisions and answers

**Files:** Create `internal/mission/summary.go`, `internal/mission/decision.go`, and their `_test.go` files.

**Interfaces (Produces):**
```go
type Summary struct {
    MissionID, Request      string
    Piece                   Piece
    Failure, Station        string
    LimitRoute              string
    Count                   int
    Max                     string
    Routes                  map[string]int // "station.route" → times taken
    LastSummary             string
    LastNotes               []string
    Earlier                 []Decision
    CallsLeft, AdditionsLeft string
    Allowed                 []string
    StepsDir                string // where the full outputs are
}
func BuildSummary(id string, m Mission, ps PieceState, rs run.State, f source.Factory, last run.StepRecord, stepsDir string, lim Limits, additions int) Summary
func (s Summary) Prompt() string // the whole headless prompt, ending with how to reply

type OrchDecision struct {
    Action  string `json:"action"`  // "retry" | "goto" | "cancel" | "rebrief" | "add" | "escalate"
    Station string `json:"station"` // goto
    Target  string `json:"target"`  // rebrief: the piece to re-brief
    Brief   string `json:"brief"`   // rebrief
    Piece   *Piece `json:"piece"`   // add
    Quote   string `json:"quote"`   // add: the part of the request it serves
    Note    string `json:"note"`    // goto: for the station; escalate: the question
    Reason  string `json:"reason"`
}
func ParseDecision(reply string) (OrchDecision, error) // the last fenced block tagged `decision`

type RunStep struct{ Piece, Kind, Station, Note string } // Kind: "retry" | "goto" | "approve" | "stop"
func ApplyDecision(m Mission, s State, piece, failure string, d OrchDecision, by string, in CheckInput, now time.Time) (Mission, State, []Action, *RunStep)

type Answer struct {
    Wait    string `json:"wait"`
    Do      string `json:"do"`      // "retry" | "goto" | "cancel" | "approve"
    Station string `json:"station"`
    Note    string `json:"note"`
    Words   string `json:"words"`   // what the principal said, word for word
}
func ApplyAnswer(m Mission, s State, a Answer, in CheckInput, now time.Time) (Mission, State, []Action, *RunStep, error)
```
- `Prompt` lists every field of the summary under plain headings, then the allowed actions, then: "Reply with exactly one fenced block tagged `decision` holding one JSON object."
- `ApplyDecision` appends a `Decision` to the piece and returns a `decision` record. Per action:
  - `retry` → `RunStep{Kind: "retry"}`. `goto` → `RunStep{Kind: "goto"}`; an unknown station is invalid.
  - `cancel` → the piece becomes `cancelled`, with `RunStep{Kind: "stop"}` and `Cleanup{Force: false}`.
  - `rebrief` → `Target` must be `queued`; its brief changes in the returned mission, with a `rebrief` record and a change-log entry.
  - `add` → needs `Quote` to be a substring of the request, `Additions` under the limit, and the new plan to pass `Check`. The piece is appended `queued` with `Added: true`, with a `piece-added` record, a change-log entry and a `Notify`.
  - `escalate` → a `Wait` of kind `question` holding `Note`, an `escalated` record and a `Notify`.
- **Invalid decisions escalate.** An unknown action, a missing field, a failed check, or a repeat all turn into a `Wait` of kind `question` whose text starts `the orchestrator's decision couldn't be used:`. The engine never asks again for the same stop.
- **A repeat** is a decision with the same `Action` and `Station` as an earlier `Decision` on that piece with the same `Failure`.
- `ApplyAnswer` finds the piece holding that `Wait`, clears it, restores `Back`, records `answered` with `Words`, and then:
  - `approve` (only for kind `approval`) → `RunStep{Kind: "approve", Station: a.Station, Note: a.Note}`.
  - `retry`, `goto`, `cancel` → the same effect as the matching orchestrator action, recorded with `By: "principal"`. `retry` on a `pr-closed` wait returns the piece to `pr-open`.
  - An unknown wait id, or a `Do` that doesn't fit the wait's kind, is an error and changes nothing.

- [ ] **Step 1: Write failing tests:**
  - `TestSummary_PromptHasEveryField` (limit-hit on `review.fix`, count 4, max `3`; the prompt contains the request verbatim, `review.fix`, `4`, `3`, and each earlier decision)
  - `TestParseDecision_LastBlockWins`, `TestParseDecision_NoBlockIsError`
  - `TestApplyDecision_GotoReturnsRunStep`
  - `TestApplyDecision_CancelStopsAndCleans`
  - `TestApplyDecision_RebriefOnlyQueued`
  - `TestApplyDecision_AddNeedsQuoteFromRequest` and `TestApplyDecision_AddPastLimitEscalates`
  - `TestApplyDecision_InvalidEscalates` (action `merge`)
  - `TestApplyDecision_RepeatEscalates` (`goto plan` twice for the same failure; `goto build` after `goto plan` is accepted)
  - `TestApplyAnswer_ApproveOnlyForApprovalWaits`
  - `TestApplyAnswer_RetryOnClosedPRReturnsToPROpen`
  - `TestApplyAnswer_UnknownWaitIsError`
  - `TestApply_DoesNotMutateInput`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: exception summaries, decisions and answers`.

### Task 13: Amendments

**Files:** Create `internal/mission/amend.go`, `internal/mission/amend_test.go`.

**Interfaces (Produces):**
```go
type Amendment struct {
    Plan  []byte `json:"plan"`  // a whole mission file
    Words string `json:"words"` // what the principal said, word for word
}
func Amend(m Mission, s State, a Amendment, in CheckInput, now time.Time) (Mission, State, []Action, []RunStep, error)
```
- The new plan's request must equal the old one. Its `Changes` are ignored; the old log is kept and extended.
- A piece missing from the new plan is cancelled: `queued` → `cancelled`; `running` → `cancelled` with a `stop` run step and `Cleanup{Force: false}`; `pr-open` → `cancelled`, with a note that its PR stays open. A `complete` piece missing is an error.
- A new piece is appended `queued`. Amendments don't count toward `Additions`.
- A `queued` piece may change any field. A piece in any other status must be unchanged, or it's an error naming the piece.
- The result must pass `Check`. On any error nothing changes.
- Each change appends a change-log entry with `By: "principal"` and `Why` set to `Words`, plus `amendment`, `piece-added`, `rebrief` or `piece-cancelled` records.

- [ ] **Step 1: Write failing tests:**
  - `TestAmend_AddsPieceAndRebriefsQueued` (the spec's "$15" case: one piece added, one queued piece re-briefed, two change-log entries holding the words)
  - `TestAmend_ChangingStartedPieceIsError`
  - `TestAmend_RemovingRunningPieceStopsIt`
  - `TestAmend_RemovingCompletePieceIsError`
  - `TestAmend_RequestMustNotChange`
  - `TestAmend_FailedCheckChangesNothing`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: mission amendments`.

---

## Phase D: The engine

### Task 14: Engine tick

**Files:** Create `internal/mission/engine.go`, `internal/mission/ops.go`, `internal/mission/engine_test.go`, `internal/mission/fakes_test.go`.

**Interfaces (Produces):**
```go
type GitOps interface {
    Fetch(branch string) error
    AddWorktree(path, branch, from string) error
    RemoveWorktree(path string, force bool) error
    Dirty(worktree string) ([]string, error)
    Ahead(worktree, base string) (int, error)
    Push(worktree, branch string) error
} // gitx.Repo satisfies it
type GHOps interface {
    CreatePR(ctx context.Context, head, base, title, body string) (gh.PR, error)
    PRState(ctx context.Context, number int) (gh.PR, error)
}
type RunOps interface {
    Start(p Piece, input, worktree string) (runID string, err error)
    Observe(runID string) (RunObs, error)
    Drive(ctx context.Context, runID, worktree string) error // starts driving; nil if already live
    Step(ctx context.Context, runID, worktree string, st RunStep) error
    Stop(runID string) error
    Summary(runID string) (run.State, source.Factory, run.StepRecord, string, error)
}
type Orchestrator interface {
    Decide(ctx context.Context, repoRoot string, s Summary) (OrchDecision, error)
}
type Engine struct {
    Store    Store
    RepoRoot string
    Settings Settings
    Checks   CheckInput
    Domains  map[string]source.Domain
    Git      GitOps
    GH       GHOps
    Runs     RunOps
    Orch     Orchestrator
    Notifier notify.Notifier
    Now      func() time.Time
    Out      io.Writer
}
func (e *Engine) Tick(ctx context.Context) (State, error)
func (e *Engine) Loop(ctx context.Context, every time.Duration) (State, error) // until done, stopped or ctx ends
func PieceInput(id string, m Mission, p Piece, d source.Domain) string
func PRText(id string, m Mission, p Piece, last run.StepRecord) (title, body string)
```
One `Tick`, in order:
1. Load the mission and the state.
2. Read the inbox. Apply each `amend` with `Amend` and each `answer` with `ApplyAnswer`. An item that errors becomes a `note` event and is still acknowledged.
3. Build `Obs`: `Runs.Observe` for pieces with a run, `Git.Dirty` and `Git.Ahead` for running pieces whose run is `done`, `GH.PRState` for `pr-open` pieces.
4. `Decide`, then run each action:
   - `StartPiece`: `Git.Fetch(base)`, `Git.AddWorktree(path, branch, "origin/"+base)`, `Runs.Start`, then `Started`, then `Runs.Drive`.
   - `DriveRun`: `Runs.Drive`, then `Resumed`.
   - `SendBack`: `Runs.Step` with kind `goto`, then `SentBackDone`.
   - `OpenPR`: `Git.Push`, `GH.CreatePR`, then `PROpened`.
   - `AskOrchestrator`: `Runs.Summary`, `BuildSummary`, `Orch.Decide`, then `Asked` and `ApplyDecision`. An error from `Orch.Decide` counts as an invalid decision.
   - Run steps from decisions, answers and amendments go to `Runs.Step` or `Runs.Stop`.
   - `Notify`, `Cleanup` and `Record` run directly. A failed `Notify` or `Cleanup` is a `note` event, never an error.
5. Save the mission if it changed, save the state, then acknowledge the inbox items.

- An effect that fails becomes a `note` event naming the piece. Its reducer isn't called, so the next tick tries again.
- `Loop` takes the mission lock, emits `resumed` when events already exist, ticks every `every`, and on `ctx.Done()` calls `Runs.Stop` for each running piece, sets status `stopped`, and emits `stopped`.
- `PieceInput` has four headed sections: the piece's brief; the whole request, word for word, marked "for context; do only your piece"; the domain briefing; and "Commit your work on this branch. Don't push, and don't open or merge a pull request."
- `PRText`: the title is the brief's first line, cut to 70 characters. The body names the mission, piece, domain and factory, quotes the request, and ends with the last step's summary.

- [ ] **Step 1: Write failing tests** with fakes for all five interfaces and a fixed clock:
  - `TestEngine_HappyPathOnePiece` (start → run done → PR opened → merged → complete → worktree removed → mission done; assert the event kinds in order)
  - `TestEngine_SecondPieceCutFromFreshBase` (`Fetch` is called again before P2's worktree is added)
  - `TestEngine_GHFailureRetriesNextTick` (`CreatePR` fails once: status stays `running`, `PR == 0`, one `note`; the next tick opens it)
  - `TestEngine_ResumeRestartsDeadRun` (state says `running`, `Observe` reports not live: `Drive` is called)
  - `TestEngine_NeverRerunsPieceWithPR` (a `pr-open` piece across ten ticks: `Start` and `Drive` are never called)
  - `TestEngine_LimitHitAsksOrchestratorAndAppliesGoto`
  - `TestEngine_OrchestratorErrorEscalatesToPrincipal`
  - `TestEngine_AnswerFromInboxApprovesRun` (and the inbox item is gone only after the state was saved)
  - `TestEngine_AmendFromInboxAddsPiece`
  - `TestEngine_LoopStopMarksStoppedAndStopsRuns`
  - `TestEngine_SecondLoopRefused`
  - `TestPieceInput_CarriesRequestVerbatim`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: mission engine`.

### Task 15: Real run and orchestrator operations, and the supervisor

**Files:** Create `internal/mission/runs.go`, `internal/mission/orchestrate.go`, `internal/mission/supervise.go`, and their `_test.go` files.

**Interfaces (Produces):**
```go
type LocalRuns struct {
    RepoRoot, CrewDir string
    Project           *verify.Project
    Runner            run.Runner
    Out               io.Writer
} // implements RunOps; one goroutine per driven run
func (l *LocalRuns) Wait() // blocks until every driven run has returned

type HeadlessOrchestrator struct {
    Registry *config.Registry
    Runner   run.Runner
    Harness  string
    CrewDir  string
    Timeout  time.Duration // 10 minutes when zero
} // implements Orchestrator

func Supervise(ctx context.Context, start func(ctx context.Context) error, max int, onCrash func(n int, err error)) error
```
- `LocalRuns.Start` calls `run.NewRun(RepoRoot, …)`, so the run folder is in the main repo.
- `Drive` builds a `run.Engine` with `Project: worktree` and `CrewDir`, and calls `Drive` on it in a goroutine. It returns nil at once when the run's lock is held by a live process.
- `Step`: `retry` is `run.Goto` to the current station; `goto` is `run.GotoFresh` with the note queued through `AppendInbox`; `approve` is `run.Approve` with an optional station and note. Each saves the run state, then drives.
- `Stop` cancels that run's context, which kills the station's process group through the existing runner.
- `HeadlessOrchestrator.Decide` runs the agent `orchestrator` on `Harness` with the tools of the `read` and `search` capabilities only, `DenyRules(…, true)`, the work directory set to the repo root and the prompt on stdin, then calls `ParseDecision`.
- `Supervise` calls `start`. A nil return or a cancelled `ctx` ends it. Any other error calls `onCrash(n, err)` and starts again, up to `max` restarts; after that it returns the last error.

- [ ] **Step 1: Write failing tests:**
  - `TestLocalRuns_RunFolderInMainRepoWorkInWorktree` (with the `fakeRunner` pattern from `internal/run/engine_test.go`: the request's `WorkDir` is the worktree and the run folder is under `<repo>/.crew/runs`)
  - `TestLocalRuns_DriveTwiceIsOneEngine`
  - `TestLocalRuns_StepGotoResetsCountsAndDeliversNote`
  - `TestLocalRuns_StopInterruptsRun`
  - `TestHeadlessOrchestrator_ReadOnlyToolsAndDenyRules` (the request's `Allowed` holds no `Bash`, `Edit` or `Write`)
  - `TestSupervise_RestartsUpToMaxThenReturnsError` (`max` 3: four starts, three `onCrash` calls)
  - `TestSupervise_CleanExitDoesNotRestart` and `TestSupervise_CancelDoesNotRestart`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: run and orchestrator operations, engine supervisor`.

---

## Phase E: Commands and the agent

### Task 16: `crew mission`

**Files:** Create `cmd/crew/mission.go`, `cmd/crew/missionctl.go`, `cmd/crew/missionstatus.go`, `cmd/crew/mission_test.go`. Modify `cmd/crew/main.go`.

**Commands:**
```
crew mission setup                                   write missions.yaml and domains/ if they're missing
crew mission plan [-|@FILE]                          check a plan and create the mission
crew mission start <id>                              run the start checks; wait for go, or go
crew mission go <id>                                 the principal's go
crew mission status [<id>] [--since N]
crew mission answer <id> <wait> --do retry|goto|cancel|approve [--station S] [--note TEXT|@FILE] --words TEXT|@FILE
crew mission amend <id> [-|@FILE] --words TEXT|@FILE
crew mission stop <id>
crew mission resume <id>
crew mission run <id> [--engine]                     internal: the supervisor, or the engine under it
common flags: --dir D, --project P
```
- `plan` reads a whole mission file from stdin or a file, runs `Check`, calls `gh` for the repo slug and default branch, creates the mission (`planned`), and prints the id and the Mermaid diagram. Any check error prints them all and creates nothing.
- `start` requires `planned`. It runs `verify`, `Check` again, `principal.CheckRepo`, and requires `gh` to answer. With `mission_approval: on` the status becomes `awaiting-go` and it prints the plan and `waiting for go: crew mission go <id>`. With `off` it goes straight on.
- `go` requires `awaiting-go`. It saves `approved.md`, sets `running`, emits `mission-start`, and starts `crew mission run <id>` detached, with output to `<mission>/mission.log`, the way `driveOrDetach` does for runs.
- `run` without `--engine` is the supervisor: it runs `crew mission run <id> --engine` as a child through `Supervise` with `max` 3, forwards SIGTERM and SIGINT, and notifies on each crash and when it gives up. With `--engine` it builds the `Engine` with the real operations and calls `Loop` every 60 seconds.
- `stop` sends SIGTERM to the pid in the lock and waits up to 15 seconds for the lock to clear. `resume` requires `stopped` or a dead lock, runs `Check` again, sets `running` and starts the supervisor.
- `answer` and `amend` validate against the current state with `ApplyAnswer` or `Amend` as a dry run, print any error, and otherwise `Drop` the item in the inbox and print `queued; applied on the next tick`.
- `status` with no id lists missions. With an id it prints, in this order: the header line (with `live, pid N` or `not running`); **Waiting on you** (wait id, piece, kind, question or PR URL, how long); Running (piece, run id, station, step); Queued (piece, what it waits for); Complete and Cancelled; the events after `--since`; and `cursor N`.

- [ ] **Step 1: Write failing tests** (temp crew dir and temp repo; `gh`, the runner and the TTY are package-level variables the tests replace):
  - `TestMissionPlan_CreatesMissionAndPrintsDiagram`
  - `TestMissionPlan_CheckErrorsCreateNothing`
  - `TestMissionStart_UnconfirmedRepoRefused` (message contains `crew repo confirm`)
  - `TestMissionStart_ApprovalOnWaitsForGo` and `TestMissionStart_ApprovalOffGoes`
  - `TestMissionGo_SavesApprovedAndStartsRunning`
  - `TestMissionStatus_WaitingOnYouComesFirst`
  - `TestMissionStatus_NotRunningIsSaid`
  - `TestMissionAnswer_BadWaitPrintsErrorAndQueuesNothing`
  - `TestMissionAmend_QueuesWithWords`
  - `TestMissionSetup_WritesDefaultsOnce` (a second call leaves an edited file alone)
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: crew mission commands`.

### Task 17: The orchestrator agent

**Files:** Create `internal/mission/orchestrator.go`, `internal/mission/orchestrator_test.go`, `internal/build/orchestrator.go`, `internal/build/orchestrator_test.go`. Modify `internal/config/config.go`, `internal/build/build.go`, `cmd/crew/main.go` (`printForemen`), `internal/scaffold/template/harnesses.yaml`.

**Interfaces (Produces):**
```go
const OrchestratorName = "orchestrator"
func OrchestratorAgent() source.Agent // capabilities: read, search, shell; model inherit
func OrchestratorBody() string
// config.ForemanSpec gains:
OrchestratorLaunch string   `yaml:"orchestrator_launch"` // {name} and {settings}
Allow              []string `yaml:"orchestrator_allow"`  // native allow rules for the orchestrator's session
// build:
func orchestratorOutputs(p *verify.Project, hn, crewDir, dir string, link bool, o Opts) ([]File, error)
```
- `harnesses.yaml` for `claude-code`: `orchestrator_launch: "claude --agent {name} --settings {settings}"` and `orchestrator_allow: ["Bash(crew:*)", "Read", "Grep", "Glob"]`.
- `orchestratorOutputs` renders the agent like a foreman, on every harness where `canHostForeman` is true, and writes `orchestrator.settings.json` beside the built agent: `permissions.allow` from `orchestrator_allow`, `permissions.deny` from `DenyRules(…, false)`.
- `crew compile` and `crew verify` print the launch line with the settings path filled in. Foremen are still emitted.
- `verify` errors when an agent in `agents/` is named `orchestrator`.
- `OrchestratorBody` tells the agent, in this order:
  1. It plans and reports. It never edits files, runs a station's work, merges, or approves.
  2. At the start of a session: run `crew mission status` and report what is waiting.
  3. To plan: read `crew domain list` and `crew factory list`, split the job into pieces, and pass the plan to `crew mission plan -` on stdin with a quoted heredoc. The body is the principal's request, word for word.
  4. Show the plan and the diagram, run `crew mission start`, and run `crew mission go` only after the principal says go.
  5. For a change of mind: `crew mission amend` with the principal's words. For a waiting item: `crew mission answer` with their words.
  6. Never run `crew automerge`, `crew repo confirm` or `crew release approve`. Tell the principal to run them in a terminal.
  7. The mission file format, with one complete example.

- [ ] **Step 1: Write failing tests:**
  - `TestOrchestratorAgent_HasNoEditCapabilities`
  - `TestOrchestratorBody_NamesEveryCommandItMayRun` and `TestOrchestratorBody_ForbidsPrincipalCommands`
  - `TestOrchestratorBody_ExamplePlanParsesAndChecks` (extract the example from the body; `Parse` succeeds)
  - `TestOrchestratorOutputs_WritesAgentAndSettings` (the settings allow `Bash(crew:*)` and deny `Bash(crew automerge:*)`, and don't deny `Bash(crew mission:*)`)
  - `TestVerify_AgentNamedOrchestratorIsError`
  - `TestCompile_StillEmitsForemen`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: orchestrator agent`.

### Task 18: Docs and an end-to-end test

**Files:** Create `cmd/crew/mission_e2e_test.go`, `cmd/crew/testdata/fakegh.sh`, `cmd/crew/testdata/fakeharness-commit.sh`, `docs/missions.md`. Modify `README.md`.

- `fakegh.sh` answers `repo view`, `pr create` and `pr view` from files in a directory named by `FAKE_GH_DIR`, so the test can flip a PR to `MERGED`.
- The test builds a temp crew dir (one domain, a two-station factory, a fake harness script) and a temp repo with a bare `origin`. The fake harness follows `internal/run/testdata/fakeharness.sh` and commits one file in its work directory before replying.
- `docs/missions.md` covers: setting up a domain, `crew repo confirm`, launching the orchestrator, the commands, what "waiting on you" means, stop and resume, and what the safeguards do and don't cover (taken from the spec's section of that name).

- [ ] **Step 1: Write the failing test** `TestMission_EndToEnd_TwoPiecesOnePRWaits`: plan two pieces where P2 waits for P1; confirm the repo; start and go; drive ticks by calling `Engine.Tick` directly with the real git, the fake `gh` and the fake harness. Assert: P1's worktree and branch exist; one PR was created with the body on stdin; P2 is still `queued`; status output leads with `Waiting on you` and the PR; after flipping the PR to `MERGED`, P1 is `complete`, its worktree is gone and P2 is `running`.
- [ ] **Step 2:** Run `go test ./cmd/crew -run TestMission_EndToEnd -v`. Expected: FAIL. **Step 3:** Fix whatever the pieces above left unconnected. **Step 4:** PASS.
- [ ] **Step 5:** Run `go test ./...` and `go vet ./...`. Expected: all pass, no output from vet.
- [ ] **Step 6:** Write `docs/missions.md` and the README section.
- [ ] **Step 7:** Commit `docs: missions guide and end-to-end test`.
