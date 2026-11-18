# crew: rename + factories — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename `crewfile` to `crew`, then add factories: agent production lines with forward routes and capped back-routes, a Mermaid diagram, a `crew run` engine, and a per-factory foreman agent on top of it.

**Architecture:** `internal/source` parses `factories/<name>.md`. `internal/factory` validates the graph and draws it. `internal/run` holds a pure state machine plus a run folder, git snapshots, prompt assembly and a headless-CLI runner. `crew run` drives it from the terminal (B). `crew compile` also emits `foreman-<factory>` main-session agents that drive the same engine through `crew run` subcommands (A).

**Tech Stack:** Go 1.22 module (toolchain on machine is 1.24), `gopkg.in/yaml.v3` only, stdlib `testing`, `git` CLI for snapshots.

**Spec:** `docs/superpowers/specs/2026-11-18-factories-design.md`. Read it before any task.

## Global Constraints

- The only dependency is `gopkg.in/yaml.v3`. No new modules.
- All config and frontmatter decode strictly (unknown keys are errors), matching `decodeStrict` / `parse`.
- The `verify` levels are unchanged: warnings are fatal. `max: "*"` must be an **info** note, never a warning.
- No headless command may use a permission-bypass flag. verify rejects any `headless.command` token containing `dangerously`, `bypass`, `skip-permissions` or `yolo`.
- State updates are immutable: state-machine functions take a `State` and return a new `State` (copy maps; never mutate the argument).
- Files stay under 400 lines and functions under 50 lines. Follow the existing package style (no new top-level dirs besides those named here).
- Station agents in `agents/` need no factory-specific changes. The contract is injected in the prompt.
- Reserved route targets: `done`, `fail`. `fail` is an implicit route on every station.
- Run folder: `<project>/.crew/runs/<run-id>/`.
- Every commit message uses the conventional `type: description` form and ends with:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01L1o92TnCABZyZodEGzW9mc
  ```

## Review Focus

1. **The station's harness CLI is missing or not logged in.** The run fails with a message naming the harness and the command it tried. The run stays resumable after the user fixes it. → Task 14 `TestHeadlessRunner_MissingBinary`, Task 15 `TestEngine_RunnerErrorFailsResumably`.
2. **Two engines on the same run** (a detached run plus a manual `resume`). The second refuses with "run <id> is already being driven (pid N)". → Task 10 `TestStore_LockExclusive`.
3. **A reply with several fenced `verdict` blocks, or one inside an example.** The last `verdict` block wins. Only that block is removed from `output.md`. → Task 9 `TestParseReply_LastBlockWins`.
4. **Ctrl-C or SIGTERM mid-station.** The child process group is killed and the status becomes `interrupted`. `resume` re-runs that station without a drift check. → Task 15 `TestEngine_CancelMarksInterrupted`.
5. **The factory file is edited while a run is in progress.** The run keeps using the copy saved in its run folder (`factory.md`), so routes and limits can't change underneath it. → Task 10 `TestStore_FactorySnapshotIsFrozen`.

---

## Phase 0: Rename

### Task 0: Put the source under git

The repo at `~/Downloads/crewfile` has no `.git`.

- [ ] **Step 1:** `cd ~/Downloads/crewfile && git init && git add -A && git commit -m "chore: import crewfile source"` (use the commit trailer from Global Constraints).
- [ ] **Step 2:** `go test ./...` → Expected: all `ok`. This is the baseline.

### Task 1: Mechanical rename crewfile → crew

**Files:**
- Modify: `go.mod` (module `github.com/crewfile-dev/crew`), every import path
- Move: `cmd/crewfile/` → `cmd/crew/`
- Modify: `internal/config/config.go` (`CrewfileFile` → `CrewFile = "crew.yaml"`; type `Crewfile` → `Crew`; `LoadCrewfile` → `LoadCrew`; doc comments)
- Modify: `internal/build/state.go:49-60` (state dir `.crew`), `internal/build/build.go:53-58` (`crew-build` staging name), all user-facing strings in `internal/build`, `internal/verify` and `cmd/crew` (`crewfile` → `crew`, including the `fail()` prefix and usage text)
- Modify: `cmd/crew/main.go` `resolveDir`: env `CREW_HOME`, default `~/crew`, cwd detection on `crew.yaml`
- Move: `internal/scaffold/template/crewfile.yaml` → `crew.yaml`, `template/workflows/` → `template/factories/`
- Modify: `internal/source/source.go` (`WorkflowsDir` → `FactoriesDir = "factories"`; remove `ListWorkflows`; add `const LegacyWorkflowsDir = "workflows"`)
- Modify: `internal/verify/verify.go:132-134`: if a `workflows/` dir exists, add info "workflows/ is now factories/; move your files there"
- Modify: `README.md`
- Test: existing tests, plus `cmd/crew/main_test.go`

**Interfaces:** Produces `config.CrewFile`, `config.Crew`, `config.LoadCrew(dir string) (*config.Crew, error)`, `source.FactoriesDir`, `source.LegacyWorkflowsDir`. Later tasks use only these names.

- [ ] **Step 1: Write the failing test.** `TestResolveDir_CrewHome` in `cmd/crew/main_test.go`: with `t.Setenv("CREW_HOME", tmp)`, `resolveDir("", false)` returns `tmp`. With no env, no flag and a cwd without `crew.yaml`, it returns `$HOME/crew`.
- [ ] **Step 2:** `go test ./cmd/crew/ -run TestResolveDir_CrewHome` → FAIL (package/dir doesn't exist yet).
- [ ] **Step 3:** Do the rename listed above. Run `grep -rn -i crewfile --include='*.go' --include='*.yaml' --include='*.md' .`. The only hits left should be the migration code from Task 2 (none yet) and the `docs/` files.
- [ ] **Step 4:** Add `TestVerify_LegacyWorkflowsDirNote` in `internal/verify/verify_test.go`: create `workflows/x.md` in a fresh scaffold and expect an Info containing `factories/`. Then `go test ./...` → all PASS.
- [ ] **Step 5:** `go install ./cmd/crew && crew --help | head -1` → `crew — compile agents and skills across AI coding harnesses`.
- [ ] **Step 6:** Commit `refactor: rename crewfile to crew`.

### Task 2: `crew migrate`

Moves the user's existing setup to the new names. Compile then re-points the harness links. Why this works (from `internal/build/diff.go`): after the move, each harness link still points at its old target. That matches its state record, so it's classified `Update` to the new target. Build files only need their state **keys** rewritten to the new dir, and they come out `Unchanged`.

**Files:**
- Create: `internal/migrate/migrate.go`, `internal/migrate/migrate_test.go`
- Modify: `cmd/crew/main.go` (add `migrate` command: `crew migrate [--from ~/crewfile] [--to ~/crew] [--dry-run]`)

**Interfaces:**
- Produces: `migrate.Plan(from, to string) ([]migrate.Step, error)`, `migrate.Apply(steps []migrate.Step) error`, `type Step struct { Kind string; From, To string }` (Kinds: `move-dir`, `rename-file`, `rewrite-state`)

Rules:
- Refuse if `to` exists.
- Refuse if `from` has no `crewfile.yaml`.
- The move renames `from` → `to`, then `to/crewfile.yaml` → `to/crew.yaml` and `to/.crewfile/` → `to/.crew/`.
- In every `to/.crew/state.*.json`: any key with prefix `from+"/"` becomes `to+"/"`, and so does any `root` with that prefix. **`sha256` values are never changed**, because those link fingerprints must still match what's on disk.

- [ ] **Step 1: Write failing tests.**
  - `TestMigrate_RewritesStateKeysNotFingerprints`: seed `from/` with `crewfile.yaml` and `.crewfile/state.user.json` holding (a) a link entry `H/agents/x.md` → `sha256: "link:FROM/build/claude-code/agents/x.md"` and (b) a build entry key `FROM/build/claude-code/agents/x.md`. After `Apply`, (a) keeps its exact sha256, (b)'s key starts with `TO/build/`, and `to/crew.yaml` exists.
  - `TestMigrate_ThenCompileHasNoConflicts`: scaffold with `scaffold.Init(from)` (then rename its `crew.yaml` back to `crewfile.yaml` and `.crew` to `.crewfile` to imitate an old layout) and compile with `build.Opts{Out: ...}`. Migrate, then `verify.Run(to)`, `build.Plan`, `build.Diff`. Expect zero `Conflict` actions, and every agent link action targets a path under `to`.
  - `TestMigrate_RefusesExistingTarget`.
- [ ] **Step 2:** `go test ./internal/migrate/` → FAIL.
- [ ] **Step 3:** Implement `Plan`/`Apply` in `internal/migrate/migrate.go`. Write the state with a temp file plus rename (same as `writeAtomic` in build). Wire up the CLI. After applying, it prints `next: crew compile`.
- [ ] **Step 4:** `go test ./...` → PASS.
- [ ] **Step 5:** Commit `feat: add crew migrate for existing crewfile setups`.
- [ ] **Step 6 (needs the user's go-ahead, because it touches `~/.claude` and `~/.config/opencode`):** Run `crew migrate --dry-run`, show the output, then `crew migrate && crew compile`. Check with `ls -l ~/.claude/agents | head` that the links point into `~/crew/build/`.

---

## Phase 1: Factory format, verify, diagram

### Task 3: Parse `factories/<name>.md`

**Files:**
- Create: `internal/source/factory.go`, `internal/source/factory_test.go`
- Modify: `internal/source/source.go` (`friendly()` gets an allowed-keys list for `*Factory`)

**Interfaces (Produces):**
```go
type Factory struct {
    Name        string             `yaml:"name"`
    Description string             `yaml:"description"`
    Start       string             `yaml:"start"`
    Harness     string             `yaml:"harness"`   // default station harness (engine)
    Watch       []string           `yaml:"watch"`     // drift roots, relative to project; default ["."]
    Timeout     string             `yaml:"timeout"`   // Go duration; default "60m"
    Stations    map[string]Station `yaml:"stations"`
    Body, Path  string             `yaml:"-"`
}
type Station struct {
    Agent   string           `yaml:"agent"`
    Harness string           `yaml:"harness"`
    Approve bool             `yaml:"approve"`
    OnDrift string           `yaml:"on_drift"`
    Timeout string           `yaml:"timeout"`
    Routes  map[string]Route `yaml:"routes"`
}
type Route struct{ To string; Max Limit }   // UnmarshalYAML: scalar → To; mapping {to, max} (strict keys)
type Limit struct{ Set, Unlimited bool; N int } // UnmarshalYAML: int ≥ 1, or the string "*"
func (l Limit) String() string               // "", "*", or "3"
const TargetDone, TargetFail, RouteFail = "done", "fail", "fail"
func LoadFactories(root string) ([]Factory, []error)
func ParseFactory(path string) (Factory, error)          // used by run to read the frozen copy
func (f Factory) StationNames() []string                 // start first, then the rest sorted
func (f Factory) HarnessFor(station string) string       // station.Harness, else f.Harness
func (f Factory) TimeoutFor(station string) (time.Duration, error)
```

- [ ] **Step 1: Write failing tests** (fixtures written to `t.TempDir()/factories/`):
  - `TestParseFactory_ShortAndLongRoutes`: `ready: build` → `Route{To:"build"}`. `fix: {to: build, max: 3}` → `Max{Set:true,N:3}`. `{to: plan, max: "*"}` → `Max{Set:true,Unlimited:true}`.
  - `TestParseFactory_BadLimit`: `max: 0`, `max: -1` and `max: "lots"` each error with `max must be a positive number or "*"`.
  - `TestParseFactory_UnknownKey`: `colour: red` in a station → an error containing `unknown key "colour"`.
  - `TestStationNames_StartFirst`.
  - `TestTimeoutFor_Defaults`: an unset timeout → `60*time.Minute`, a station value overrides the factory value, and `"abc"` errors.
- [ ] **Step 2:** `go test ./internal/source/` → FAIL.
- [ ] **Step 3:** Implement. `LoadFactories` mirrors `LoadAgents` (it uses `listMarkdown(filepath.Join(root, FactoriesDir))` and strict `parse`).
- [ ] **Step 4:** `go test ./internal/source/` → PASS.
- [ ] **Step 5:** Commit `feat: parse factory definitions`.

### Task 4: Graph validation

**Files:**
- Create: `internal/factory/validate.go`, `internal/factory/validate_test.go`

**Interfaces:**
- Consumes: `source.Factory`, `source.Route`, `source.Limit`
- Produces: `type Issue struct{ Info bool; Msg string }`, `func Validate(f source.Factory) []Issue`, `func IsBackEdge(f source.Factory, from, route string) bool` (true if the route's target can reach `from` again, used by the diagram)

Rules for `Validate` (each is an error unless marked info):
- `start` is empty or not a station.
- A route target is not a station, `done` or `fail`.
- A station declares a route named `fail` that targets anything other than `fail`.
- A station has no routes.
- A station can't be reached from `start`. Drift edges count as reachability.
- `on_drift` names an unknown station.
- A station name is `done`, `fail`, or not `nameRe`-shaped (`^[a-z0-9]+(-[a-z0-9]+)*$`).
- **Uncapped cycle:** remove every route whose `Max.Set` is true. If the remaining graph (routes only, not drift edges) has a cycle, error `loop with no limit: plan →ready→ build →replan→ plan; add max (a number or "*") to one of these routes`. Use DFS with a recursion stack and report the first cycle found.
- **Info:** each route with `max: "*"`: `plan.redesign loops without limit ("*")`.
- **Info:** a route with `max` that is not on any cycle: `max on build.done has no effect: this route can't loop`.

- [ ] **Step 1: Write failing tests**, table-driven `TestValidate` with one case per rule. Include:
  - `"cap on either edge is enough"`: A→B, B→C, C→B (`max: 2`), A→C. No error. Then the same graph with the cap moved to B→C: still no error.
  - `"two cycles one capped"`: the uncapped one is reported.
  - `"self-loop needs max"`: `retry: {to: build}` on station `build` → error.
- [ ] **Step 2:** `go test ./internal/factory/` → FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: validate factory graphs`.

### Task 5: verify integration

**Files:**
- Modify: `internal/verify/verify.go` (add `Factories []source.Factory` to `Project`; load them in `Run`; new `checkFactories(p, rep)`)
- Create: `internal/verify/factories.go` (keeps `verify.go` from growing), `internal/verify/factories_test.go`
- Create: `internal/scaffold/template/factories/ship-change.md`, the spec's example factory using agents that exist in the scaffold template. Change agent names if the template lacks `epic-planner`/`implementer`/`reviewer`. The routes and limits must stay as in the spec.

**Interfaces:** Consumes `factory.Validate`, `config.Harness.Headless` (Task 13; until then skip the headless check behind `if h.Headless != nil`). Produces `verify.Project.Factories`.

Checks:
- Every `factory.Validate` issue goes through `rep.errf` or `rep.infof`, with the factory's path as `Where`.
- `checkCommon` (name, description, filename) applies to factories too.
- A station agent missing from `agents/` is an error.
- The station's harness (from `HarnessFor`) must be set, known, enabled and have `headless`. The agent must also be in `p.Selection[harness]`. Otherwise: error `station build: agent implementer isn't compiled for opencode (crew.yaml targets.opencode.agents)`.
- A factory name equal to a `crew run` subcommand (`status`, `say`, `goto`, `approve`, `resume`) is an error.
- A name used by both a factory and an agent, or by `foreman-<factory>` and an agent, is an error.

- [ ] **Step 1: Write failing tests** with the existing `fresh`/`edit`/`expectError` helpers: `TestFactories_UnknownAgent`, `TestFactories_HarnessNotEnabled`, `TestFactories_ReservedName`, `TestFactories_ForemanNameClash`, and `TestFactories_StarIsInfoNotWarning` (`rep.OK(false)` is true and an Info mentions `"*"`).
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** `go test ./...` PASS.
- [ ] **Step 5:** Commit `feat: verify factories`.

### Task 6: Mermaid diagram

**Files:**
- Create: `internal/factory/diagram.go`, `internal/factory/diagram_test.go`
- Create: `cmd/crew/factory.go` (`crew factory diagram <name>`, `crew factory list`)
- Modify: `cmd/crew/main.go`. Route `factory` and `run` (Task 16) to their own handlers **before** the shared FlagSet, which rejects positional args.
- Modify: `internal/build/build.go` `Plan`. For each factory, add `File{Path: BuildDir(crewDir,o)/factories/<name>.md, Kind: "factory-diagram", Name, Content}`. The content is the title, the description, and a ```` ```mermaid ```` block.

**Interfaces:** `func Mermaid(f source.Factory) string`

Exact output rules (the test pins them):
- The first line is `flowchart LR`.
- Each station is a node named by its station id. The label is `station<br/><small>agent</small>`. Approval stations use the hexagon shape `{{…}}`, and the rest use rounded `(…)`.
- `done` is `done([done])` and `fail` is `fail([fail])`. They appear only if something targets them. The implicit `fail` routes are not drawn.
- Nodes and edges appear in `StationNames()` order, then route names sorted.
- A route without `max` is `a -->|route| b`. A route with `max` is `a -.->|route ≤3| b` (or `≤*`). A drift edge is `a -.->|drift| b`.
- A start marker: `start(( )) --> <start>`.

- [ ] **Step 1: Write the failing test** `TestMermaid_ShipChange`. It parses the template factory from Task 5 and compares against a golden string in the test. Also `TestBuildPlan_WritesDiagram`: the plan contains a `factory-diagram` file whose content includes `flowchart LR`.
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS. Then `crew factory diagram ship-change` on a scaffold prints the same string.
- [ ] **Step 5:** Commit `feat: factory diagrams`.

---

## Phase 2: The engine (`crew run`)

### Task 7: State machine (pure)

**Files:** Create `internal/run/state.go`, `internal/run/machine.go`, `internal/run/machine_test.go`

**Interfaces (Produces):**
```go
type Status string // "running" | "awaiting-approval" | "interrupted" | "done" | "failed"
type Verdict struct{ Route, Summary string; Notes []string }
type Pending struct{ Station, Route string; Verdict Verdict }
type State struct {
    RunID, Factory string
    Status         Status
    Current        string            // station to run next (or awaiting approval)
    Step           int               // number of steps completed
    RouteCounts    map[string]int    // "station.route" → times taken
    Snapshot       map[string]string // root → fingerprint at end of last step
    Pending        *Pending
    Reason         string            // why failed / interrupted / last redirect
    SkipDriftOnce  bool              // set by resume after interrupt
    Retryable      bool              // failed because the runner errored; resume re-runs Current
}
type DecisionKind string // "run-station" | "await-approval" | "finished" | "failed"
type Decision struct{ Kind DecisionKind; Station, Reason string }
func Begin(f source.Factory, runID string) State
func BeforeStation(f source.Factory, s State, drifted []string) (State, Decision)
func AfterStation(f source.Factory, s State, v Verdict) (State, Decision)
func Approve(f source.Factory, s State, gotoStation string) (State, Decision, error)
func Goto(f source.Factory, s State, station string) (State, error)
func Interrupt(s State, reason string) State
func Resume(f source.Factory, s State) (State, Decision)
```

Behavior:
- `AfterStation`: `v.Route` must be in `Routes` or be `fail` (anything else is a bug, because `ParseReply` already checked it). It increments `RouteCounts`. If `Max` is set and not unlimited and the count exceeds `N`, the result is `failed` with Reason `route review.redesign hit its limit (1)`. If the station has `Approve`, the result is `awaiting-approval` with `Pending`. Otherwise it moves to the target: `done` → `finished`, `fail` → `failed` (with Reason = `v.Summary`), and a station → `run-station`.
- `BeforeStation`: if `drifted` is non-empty, the station has `OnDrift`, and `!SkipDriftOnce`, then Current becomes `OnDrift`, Reason is `drift in <roots> before <station>`, and the decision runs `OnDrift`. Drift doesn't touch `RouteCounts`. It always clears `SkipDriftOnce`.
- `Approve` with an empty goto follows `Pending.Route` exactly like `AfterStation` without the approval stop. **It does not increment `RouteCounts` again**: `AfterStation` already counted it, and checked the limit, before pausing. With a station, it goes there. It errors unless the status is `awaiting-approval`.
- `Resume` from `interrupted` sets `SkipDriftOnce=true` and runs `Current`. From `awaiting-approval` it returns `await-approval`. `failed` with `Retryable` → status `running`, clear `Retryable`, run `Current` (with `SkipDriftOnce=true`). Any other terminal status → `finished`/`failed`.
- Every function returns a new `State`; copy the maps with a helper `cloneState`.

- [ ] **Step 1: Write failing tests:**
  - `TestAfterStation_ForwardAndBack`
  - `TestAfterStation_LimitExceeded` (`max: 2`: the third time `fix` is taken fails, and Reason names the route and limit)
  - `TestAfterStation_UnlimitedNeverFails` (1000 iterations)
  - `TestAfterStation_FailRouteImplicit`
  - `TestAfterStation_ApprovalPauses`
  - `TestApprove_FollowsOrRedirects`
  - `TestBeforeStation_DriftRedirectsAndIsExempt` (RouteCounts unchanged)
  - `TestBeforeStation_SkipDriftOnceAfterResume`
  - `TestResume_RetryableFailureRerunsCurrent` and `TestResume_LimitFailureStaysFailed`
  - `TestApprove_DoesNotDoubleCount`
  - `TestMachine_DoesNotMutateInput` (deep-compare the input state before and after each call)
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat: factory run state machine`.

### Task 8: Prompt assembly

**Files:** Create `internal/run/prompt.go`, `internal/run/prompt_test.go`

**Interfaces:**
```go
type StepRecord struct{ N int; Station, Dir string; Verdict Verdict } // Dir = absolute steps/<n>-<station>
type PromptInput struct {
    Factory source.Factory; Station, Input string
    Steps []StepRecord; Inbox []string; Allowed []string; RetryError string
}
func BuildPrompt(in PromptInput) string
```

The prompt contains, in this order and with these headings: `# Factory: <name>` plus the briefing (Body), `## Your station: <station>`, `## Run input`, `## Earlier steps`, `## Messages from the user` (omitted when empty), `## How to finish`.
- Earlier steps are listed one line each: `n. station → route — summary (output: <Dir>/output.md)`. They are paths, not inlined content, which keeps the prompt size bounded.
- "How to finish" lists the allowed routes plus `fail`. It says to end the reply with exactly one fenced block tagged `verdict` containing `{"route": "...", "summary": "...", "notes": [...]}`. It says not to ask the user questions: if information is missing, pick the route that sends the work back, or `fail`. And it says anything the user should know goes in `notes`.
- A `RetryError` adds `## Your previous reply was rejected` with the error.

- [ ] **Step 1: Write failing tests:** `TestBuildPrompt_Sections` (headings in order, every route listed, `fail` listed), `TestBuildPrompt_OmitsEmptyInbox`, `TestBuildPrompt_StepsArePaths` (no output content is inlined), `TestBuildPrompt_Retry`.
- [ ] **Steps 2-4:** FAIL → implement → PASS. **Step 5:** Commit `feat: station prompt assembly`.

### Task 9: Verdict parsing

**Files:** Create `internal/run/verdict.go`, `internal/run/verdict_test.go`

**Interfaces:** `func ParseReply(reply string, allowed []string) (output string, v Verdict, err error)`. `allowed` excludes `fail`, which is always accepted.

Rules:
- Find the **last** fence opening matching `^```verdict\s*$` (multiline) and its closing ```` ``` ````.
- The output is the reply with only that block removed, trimmed, plus a trailing `\n`.
- Errors (exact prefixes, which the engine passes back to the station on retry): `no verdict block`, `verdict is not valid JSON: …`, `verdict route "x" is not one of: a, b, fail`, `verdict route is empty`.

- [ ] **Step 1: Write failing tests:** `TestParseReply_OK`, `TestParseReply_LastBlockWins` (two blocks: the last is used, and the first stays in the output), `TestParseReply_Errors` (table), `TestParseReply_FailAlwaysAllowed`.
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: parse station verdicts`.

### Task 10: Run folder store

**Files:** Create `internal/run/store.go`, `internal/run/events.go`, `internal/run/store_test.go`

**Interfaces:**
```go
type Store struct{ Dir string } // <project>/.crew/runs/<id>
func NewRun(projectDir string, f source.Factory, factorySrc []byte, input string, now time.Time) (Store, State, error)
func OpenRun(projectDir, runID string) (Store, error)
func ListRuns(projectDir string) ([]State, error)              // newest first
func (s Store) Factory() (source.Factory, error)              // parses <Dir>/factory.md (frozen copy)
func (s Store) Input() (string, error)
func (s Store) LoadState() (State, error)
func (s Store) SaveState(st State) error                      // temp file + rename
func (s Store) WriteStep(rec StepRecord, prompt, output string) error // prompt.md, output.md, verdict.json
func (s Store) Steps() ([]StepRecord, error)
func (s Store) AppendInbox(msg string) error
func (s Store) DrainInbox() ([]string, error)                 // returns and clears
func (s Store) RequestGoto(station string) error              // goto.json, picked up before next station
func (s Store) TakeGoto() (string, error)
func (s Store) Lock() (unlock func(), err error)              // O_EXCL lock file holding pid; stale if pid dead
type Event struct{ Seq int; Time time.Time; Kind, Station, Route, Msg string; Count int; Max string }
func (s Store) Emit(e Event) (Event, error)                   // appends to events.jsonl, assigns Seq
func (s Store) EventsSince(seq int) ([]Event, error)
```
- The run id is `<factory>-<YYYYMMDD-HHMMSS>-<4 hex>`.
- Event kinds: `start`, `step-start`, `step-end`, `retry`, `route`, `note`, `drift`, `approval`, `interrupted`, `done`, `failed`, `message`.
- `NewRun` writes `input.md`, `factory.md` (the source bytes as given), `state.json`, and an empty `inbox.md`.
- When `<project>/.crew/` is created for the first time, also write `<project>/.crew/.gitignore` containing `*`. This settles the spec's open "gitignore" item: crew ignores its own folder without touching the project's `.gitignore`.

- [ ] **Step 1: Write failing tests:**
  - `TestStore_RoundTrip`
  - `TestStore_FactorySnapshotIsFrozen` (edit the original factory file after `NewRun`; `Factory()` still returns the old routes)
  - `TestStore_InboxDrain`
  - `TestStore_LockExclusive` (a second `Lock` errors with `already being driven (pid`; a stale lock with a dead pid is taken over)
  - `TestStore_EventsSince`
  - `TestStore_GitignoreCreated`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: run folder store and events`.

### Task 11: Git snapshots

**Files:** Create `internal/run/snapshot.go`, `internal/run/snapshot_test.go`

**Interfaces:** `func Snapshot(project string, watch []string) (map[string]string, error)`, `func Drifted(prev, cur map[string]string) []string` (sorted roots whose fingerprint changed; an empty `prev` → none)

- The fingerprint per root is sha256 over: `git -C <root> rev-parse HEAD`, `git -C <root> diff HEAD --binary -- . ':(exclude).crew'`, and the sorted `git ls-files --others --exclude-standard -z` list, each untracked file's content hashed too.
- A root that isn't inside a git work tree errors with `<root> is not a git repository (factory watch:)`.
- Roots are resolved relative to `project`.

- [ ] **Step 1: Write failing tests** with a temp `git init` repo (skip if `git` isn't on PATH): `TestSnapshot_StableWhenUnchanged`, `TestSnapshot_ChangesOnEdit`, `TestSnapshot_ChangesOnUntracked`, `TestSnapshot_IgnoresCrewDir`, `TestSnapshot_NotARepo`, `TestDrifted`.
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: git snapshots for drift guards`.

### Task 12: Allowlist from capabilities

**Files:** Create `internal/run/allow.go`, `internal/run/allow_test.go`

**Interfaces:** `func AllowedTools(reg *config.Registry, harness string, a source.Agent) []string`. It returns the sorted, de-duplicated union of `reg.Harnesses[harness].Capabilities[c]` for each of the agent's capabilities. `all` means every capability in `reg.Capabilities`.

- [ ] **Step 1: Write failing tests:** `TestAllowedTools_ClaudeCode` (`[read, shell]` → `[Bash, Read]`), `TestAllowedTools_All`, `TestAllowedTools_UnmappedCapabilityIsEmpty`.
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: derive headless allowlists from capabilities`.

### Task 13: `headless:` in harnesses.yaml

**Files:**
- Modify: `internal/config/config.go`: add `Headless *HeadlessSpec \`yaml:"headless"\`` to `Harness`
- Modify: `internal/verify/verify.go` `checkHarness`: validate headless
- Modify: `internal/scaffold/template/harnesses.yaml` and `~/crew/harnesses.yaml` (the user's live copy, after Task 2)
- Test: `internal/verify/verify_test.go`

**Interfaces:**
```go
type HeadlessSpec struct {
    Command   []string `yaml:"command"`    // argv; tokens {agent} {prompt} {allowed}
    PromptVia string   `yaml:"prompt_via"` // "stdin" | "arg" (arg requires a {prompt} token)
    AllowVia  string   `yaml:"allow_via"`  // "flag" ({allowed} token required) | "agent-file" (compiled agent's tools list already restricts)
    AllowJoin string   `yaml:"allow_join"` // separator for {allowed}, default ","
}
```
verify errors:
- an empty command
- an unknown `{token}`
- `prompt_via: arg` without `{prompt}`
- `allow_via: flag` without `{allowed}`
- any argv element (case-insensitive) containing `dangerously`, `bypass`, `skip-permissions` or `yolo`: `headless commands may not bypass permissions`

- [ ] **Step 1: Look up the current flags** for `claude -p` (agent selection, allowed tools, permission mode, output format) at https://code.claude.com/docs/en/cli-reference and https://code.claude.com/docs/en/headless, and for `opencode run` (agent flag, how permissions apply non-interactively) at https://opencode.ai/docs/cli. Record them in both harnesses.yaml files and bump `verified:`. Expected shape for Claude Code, to be confirmed: `command: [claude, -p, --agent, "{agent}", --allowedTools, "{allowed}", --permission-mode, acceptEdits, --output-format, text]`, `prompt_via: stdin`, `allow_via: flag`. If OpenCode can't pre-allow by flag, use `allow_via: agent-file`, since its compiled agents already carry a `tools` map.
- [ ] **Step 2: Write failing tests:** `TestHeadless_RejectsBypass` (table over the four words), `TestHeadless_TokenRules`.
- [ ] **Steps 3-4:** Implement → `go test ./...` PASS. Also remove the `h.Headless != nil` guard added in Task 5.
- [ ] **Step 5:** Commit `feat: headless command spec per harness`.

### Task 14: Headless runner

**Files:** Create `internal/run/runner.go`, `internal/run/runner_test.go`, `internal/run/testdata/fakeharness.sh`

**Interfaces:**
```go
type StationRequest struct {
    Harness, Agent, Prompt, WorkDir string
    Allowed []string; Timeout time.Duration
}
type Runner interface{ Run(ctx context.Context, req StationRequest) (reply string, err error) }
type HeadlessRunner struct{ Registry *config.Registry }
```
- It expands the tokens: `{allowed}` becomes `strings.Join(Allowed, AllowJoin)`. An empty allowlist with `allow_via: flag` drops the flag and its value (any argv element immediately before `{allowed}` that starts with `-`).
- It feeds the prompt by stdin or as an argument, and runs in `WorkDir` in its own process group (`Setpgid`). On ctx cancel or timeout it kills the group.
- The reply is stdout. A non-zero exit errors with `<harness> station failed (exit N): <last 20 lines of stderr>`.
- A missing binary errors with `<harness>: command "claude" not found on PATH (headless.command in harnesses.yaml)`.
- `fakeharness.sh` reads stdin and replies with the contents of the file named by `$FAKE_REPLY_DIR/<n>` (n incremented in `$FAKE_REPLY_DIR/counter`), so tests can script a whole run.

- [ ] **Step 1: Write failing tests:** `TestHeadlessRunner_StdinAndArgs` (the fake echoes its argv and stdin; assert the `{agent}` and `{allowed}` expansion), `TestHeadlessRunner_MissingBinary`, `TestHeadlessRunner_Timeout` (fake sleeps 5s with a 200ms timeout → error within 2s, child gone), `TestHeadlessRunner_NonZeroExit`.
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: headless station runner`.

### Task 15: Engine loop

**Files:** Create `internal/run/engine.go`, `internal/run/engine_test.go`

**Interfaces:**
```go
type Engine struct {
    Store    Store
    Factory  source.Factory        // from Store.Factory()
    Agents   map[string]source.Agent
    Registry *config.Registry
    Runner   Runner
    Snap     func(project string, watch []string) (map[string]string, error) // Snapshot in prod
    Project  string
    Out      io.Writer             // human-readable event lines; nil = silent
}
func (e *Engine) Drive(ctx context.Context) (State, error)
func FormatEvent(ev Event) string   // shared by Out and `crew run status`
```
`Drive`:
1. Take `Store.Lock()`.
2. Load the state. If it's fresh, use `Begin`, take the first `Snap`, and emit `start`. Otherwise use `Resume`.
3. Loop while the decision is `run-station`:
   - If `TakeGoto` returns a station, apply `Goto`.
   - Snapshot, compute `Drifted` against `State.Snapshot`, then call `BeforeStation`. Emit `drift` if it redirected.
   - Drain the inbox (emit `message` per item), build the prompt, emit `step-start`, and run.
   - On a runner error: save the state with `Status=failed`, `Retryable=true`, Reason = the error, Current unchanged. Emit `failed` and return. `resume` re-runs the same station.
   - `ParseReply`. On error, retry once with `RetryError` (emit `retry`). A second failure fails the run.
   - `WriteStep`. Emit `step-end`, a `note` per verdict note, and `route` with count/max.
   - `AfterStation`, then snapshot again and store it in `State.Snapshot`. Save the state.
4. On ctx cancel: `Interrupt`, save, emit `interrupted`, return `ctx.Err()`.
5. On await-approval, finished or failed: emit the matching event, save, return.

`Resume` of a `failed` run whose last failure was a runner error re-runs `Current`. A run that failed because of a route limit or a `fail` route stays failed: it reports and returns.

- [ ] **Step 1: Write failing tests** with a scripted `fakeRunner` (in the test file) and a fake `Snap` you can flip:
  - `TestEngine_FullLoop` (plan→build→review(fix)→build→review(pass)→done; 5 steps on disk, counts `review.fix=1`)
  - `TestEngine_LimitStops`
  - `TestEngine_StarLoopsUntilPass` (50 `fix` then `pass`)
  - `TestEngine_DriftRedirect`
  - `TestEngine_ApprovalPausesThenApprove`
  - `TestEngine_InvalidVerdictRetriesOnce`
  - `TestEngine_InboxDelivered` (the message appears in the next prompt only)
  - `TestEngine_GotoRequest`
  - `TestEngine_RunnerErrorFailsResumably`
  - `TestEngine_CancelMarksInterrupted` (cancel during the runner; then `Drive` again resumes the same station with no drift redirect even though `Snap` changed)
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: factory engine`.

### Task 16: `crew run` CLI

**Files:** Create `cmd/crew/run.go`, `cmd/crew/run_test.go`. Modify the usage text in `cmd/crew/main.go`.

Commands (each with its own FlagSet, so positional args are allowed; `--project` defaults to cwd, `--dir` resolves the crew dir as today):
- `crew run <factory> [--input TEXT | --input @FILE] [--project P] [--detach]`: runs verify (fatal on failure), creates the run, prints `run <id>`, and drives in the foreground streaming `FormatEvent` lines. `--detach` re-execs `os.Args[0] run resume <id> --project P` with `Setsid` and stdout/stderr → `<run>/run.log`, prints the id, and exits 0.
- `crew run status [<id>] [--since N] [--project P]`: without an id, lists runs (`id  factory  status  current  step`). With an id, prints the state summary plus `FormatEvent` for events after `--since`, and finally a line `cursor N` for the foreman.
- `crew run say <id> <message…>`: `AppendInbox` + emits a `message` event.
- `crew run goto <id> <station>`: if the run is live (locked), `RequestGoto`. Otherwise apply `Goto` to the saved state.
- `crew run approve <id> [--goto S] [--detach]`: `Approve`, save, then drive (foreground, or detached with `--detach`).
- `crew run resume <id> [--detach]`.
- Ctrl-C/SIGTERM in the foreground cancels the ctx via `signal.NotifyContext`.
- Exit codes: 0 done / detached / awaiting-approval; 1 failed or interrupted; 2 usage.

- [ ] **Step 1: Write failing tests.** Build the binary in `TestMain` (`go build -o $tmp/crew ./`). Use a scaffold whose `harnesses.yaml` headless command points at `testdata/fakeharness.sh`, and a temp git repo as the project.
  - `TestCLI_RunToDone`
  - `TestCLI_DetachThenStatus` (poll `status --since` until `done`, 10s cap)
  - `TestCLI_ApproveFlow`
  - `TestCLI_SayReachesNextStation`
  - `TestCLI_VerifyFailureBlocksRun` (factory with an uncapped loop → exit 1, the message names the loop)
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: crew run commands`.

---

## Phase 3: Foreman

### Task 17: Foreman agents in compile

**Files:**
- Modify: `internal/config/config.go`: add `Foreman *ForemanSpec \`yaml:"foreman"\`` to `Harness`, with `type ForemanSpec struct{ Static map[string]any \`yaml:"static"\`; Launch string \`yaml:"launch"\` }` (Launch is a human hint like `claude --agent {name}`)
- Create: `internal/build/foreman.go`, `internal/build/foreman_test.go`
- Modify: `internal/build/build.go` `Plan`: after the agents loop, for each enabled harness with `Foreman != nil`, `Agents.Supported`, and a non-empty `shell` mapping, add one foreman per factory. Other harnesses get a `Note`: `no foreman: <Display> can't host a main-session agent with shell`.
- Modify: both harnesses.yaml files. claude-code gets `foreman: {launch: "claude --agent {name}"}`. opencode gets `foreman: {static: {mode: primary}, launch: "opencode --agent {name}"}` (confirm both against the docs, as in Task 13).

**Interfaces:**
- `func ForemanAgent(f source.Factory) source.Agent` → `Name: "foreman-"+f.Name`, `Description: "Foreman for the <name> factory: starts and watches crew runs, relays notes and approvals, passes your messages to the stations. Launch as the main session."`, `Model: "inherit"`, `Capabilities: [read, shell]`, `Body: ForemanBody(f)`
- `func ForemanBody(f source.Factory) string`
- Rendering reuses `renderAgent(reg, hWithForemanStatic, verify.Selected{Agent: ForemanAgent(f), Model: "inherit"})`, where `hWithForemanStatic` is a **copy** of the Harness whose `Agents.Static` is the merge of `Agents.Static` and `Foreman.Static` (Foreman wins). Link/build handling is the same as regular agents.

`ForemanBody` content (exact sections):
1. The factory briefing.
2. The Mermaid diagram from `factory.Mermaid`.
3. The **loop**:
   - Ask the user for the run input if they haven't given it.
   - Run `crew run <name> --detach --input @<tmpfile> --project <cwd>`.
   - Then repeatedly run `crew run status <id> --since <cursor>` and relay `note`, `drift`, `route` (when a limit is close), `failed` and `done` events in one line each.
   - When the user says anything during a run, pass it with `crew run say <id> "…"`. If they ask to send work to a station, use `crew run goto <id> <station>`.
   - On `approval`, show the step's `output.md` and the proposed route, ask the user, then run `crew run approve <id> [--goto S] --detach`.
   - Between polls, wait with `sleep 20` in the shell unless the user is talking.
4. Rules:
   - Never run a station yourself.
   - Never edit files in the project.
   - Never choose a route.
   - The engine is the source of truth.

- [ ] **Step 1: Write failing tests:**
  - `TestPlan_ForemanPerFactory` (claude-code + opencode enabled; two foreman files; the opencode one contains `mode: primary` and not `mode: subagent`)
  - `TestPlan_ForemanNoteWhenUnsupported`
  - `TestForemanBody_ContainsLoopAndDiagram` (contains `crew run ship-change --detach`, `crew run status`, `flowchart LR`)
  - `TestRender_DoesNotMutateHarnessStatic`
- [ ] **Steps 2-4.** **Step 5:** Commit `feat: compile foreman agents for factories`.

### Task 18: Docs and real smoke test

**Files:** Modify `README.md` (factories section: format, limits and `*`, approvals, drift and `watch`, `crew run` commands, foreman launch, diagram); `crew plan` output lists foremen per harness (`printMatrix` gains a `FOREMEN` line).

- [ ] **Step 1:** Write the README section. Paste the scaffold factory and its generated Mermaid.
- [ ] **Step 2:** Run `go test ./... -cover` → all PASS, total coverage ≥ 80% for `internal/factory`, `internal/run`, `internal/migrate` and `internal/source` (`go test -coverprofile`). Report the numbers.
- [ ] **Step 3 (real harness, with the user present):** In a scratch git repo, run a two-station factory using `code-explorer` → `reviewer` on claude-code: `crew run <f> --input "…"` to `done`. Then launch `claude --agent foreman-<f>` and drive the same factory through the foreman. Report both outcomes, including any harness flag that needed fixing in harnesses.yaml.
- [ ] **Step 4:** Commit `docs: factories and crew run`.
