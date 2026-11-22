# crew missions, stage 4: parallel pieces — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A mission runs several pieces at once, up to a limit that holds across all of a repo's missions. crew keeps apart pieces whose paths overlap, within a mission and across the repo's missions, stops a piece that changed files outside its paths until the principal accepts them or the piece is sent back, and puts a mission on hold when it has used its station steps.

**Architecture:** `Decide` already starts as many ready pieces as the limit allows; what stops it today is one rule in the settings loader. This stage removes that rule and adds what makes parallel runs safe. Three pure helpers work on path patterns: which patterns a piece may touch, whether two patterns could match one file, and which changed files fall outside a piece's patterns. A pure function, `SeparateOverlaps`, gives the later of two overlapping pieces an `apart_from` entry, and `Decide` starts a piece only when everything it is kept apart from is finished. The engine counts the runs of the repo's other missions before each start, under a short repo-wide lock, and under the same lock it checks that no started, unfinished piece of another mission has paths that overlap the piece it is about to start. The path check and the step budget are new cases in `Decide`, fed by two new observations.

**Tech Stack:** Go 1.22 module (toolchain 1.24), `gopkg.in/yaml.v3` only, stdlib `testing`.

**Spec:** `docs/superpowers/specs/2026-11-18-orchestrator-missions-design.md`. Read "The mission file", "Checks at `crew mission start`" (the Overlapping paths bullet), "The engine" (Limits), "Work sent back to a factory" (the row on files outside the piece's paths) and "Exceptions and authority" before any task. This plan covers **stage 4 of its Build order**.

**Four places where this plan departs from the spec's wording, on purpose:**

1. *"crew makes the later one wait for the earlier one."* crew does not edit `waits_for`. It writes a separate field it owns, `apart_from`. `waits_for` means "needs that piece's result": a piece whose dependency is cancelled is stuck, and a piece receives the deliverables of what it waits for. Neither is right for two pieces that merely touch the same files, so a piece kept apart from a cancelled piece simply starts.
2. *The overlap check runs "at `crew mission start`".* It runs wherever the pieces can change: at `plan`, at `start`, `go` and `resume`, on an amendment, and when the orchestrator adds a piece.
3. *"Hitting a mission's limit … goes straight to the principal."* For the step budget the principal lifts the hold by raising `steps` in `missions.yaml`, which only they can edit; the engine reads the file again while it is on hold. There is no new answer for it.
4. *The overlap check covers "two pieces that could run together" in one mission.* On the principal's word (2026-11-22) it also covers pieces of different missions in one repo. Those can't be ordered in a plan, because the missions are planned apart, so the check is made when a piece is about to start: it stays queued while an overlapping piece of another mission has started and is not finished.

**Not in this plan:** using the files the principal accepted outside a piece's paths when checking overlap (only the paths in the plan and the domain count); delegation and its depth limit (stage 5); accepting only some of the files outside a piece's paths; stopping a station in the middle of a step when the budget runs out; raising `runs_at_once` in a crew directory that already exists (the principal edits one line).

## Global Constraints

- The only dependency is `gopkg.in/yaml.v3`. No new modules.
- Before writing Go, load the `use-modern-go` skill and follow it for Go 1.22.
- Config and mission files decode strictly: unknown keys are errors.
- State is immutable: every state-machine function takes a `State` and returns a new one. `Decide`, `SeparateOverlaps` and the path helpers perform no I/O.
- **Limits are the principal's.** Nothing crew or an agent does changes `missions.yaml`. A limit is a number or `"*"`.
- **`runs_at_once` counts across the repo's missions.** A piece counts while it is `running`, or `waiting` with a wait that goes back to `running`. A piece crew sends back from its pull request is not held back by the limit (unchanged from stage 2).
- **Overlap is checked across the repo's missions too.** A piece doesn't start while a piece of another mission has started, is not complete or cancelled, and has paths that overlap its own, whether that mission is running or stopped. A piece that has started is never held back by this, so two missions can't block each other for good.
- **A piece with no paths, and whose domain has none, is not checked:** not for overlap and not for files outside its paths. crew says so in the plan.
- **When crew can't tell whether two patterns overlap, they overlap.** Keeping two pieces apart costs time; running them together can cost a conflict.
- **Only the principal accepts files outside a piece's paths.** The orchestrator may send the piece back, cancel it, or escalate.
- `apart_from` is written only by crew. Whatever a plan or an amendment supplies for it is replaced.
- State holds file names and short messages only: never file contents.
- Tests never call the real `gh`, a git remote, a Jira site, `osascript` or a harness CLI.
- `go test -race` is run on `internal/mission`, `internal/gitx`, `internal/source`; never on `./cmd/crew` (an older test there has a known data race).
- The repo is public: examples use `acme/app`. No real name, site or account appears in code, tests or docs.
- Files stay under 400 lines and functions under 50 lines. To keep a file under the limit, move code into a new file of the same package; put new tests in new test files.
- Every commit message uses the conventional `type: description` form and ends with the `Co-Authored-By` line naming the model that wrote the commit, and the `Claude-Session` line. The controller passes both to each implementer.

## Review Focus

1. **Two missions in one repo start a piece in the same instant.** The repo never has more held runs than `runs_at_once`, even when two engines tick together. → Task 3 `TestEngine_TwoEnginesNeverPassTheLimit`.
2. **The earlier of two pieces kept apart is cancelled, or the pieces already wait on each other through a third.** The later piece starts; no adjustment ever makes a circle. → Task 2 `TestDecide_ApartFromACancelledPieceStarts`, `TestSeparateOverlaps_NeverMakesACircle`.
3. **A piece deletes or moves a file outside its paths.** The deletion, and both ends of the move, count as changes outside the paths. → Task 5 `TestOutside_DeletionsAndMovesCount`.
4. **The principal accepts some outside files, and a later rework touches a different one.** The earlier acceptance stands; the new file is asked about. → Task 5 `TestDecide_AcceptedFilesStayAcceptedNewOnesAreAsked`.
5. **The step budget runs out while pieces are running.** Running stations finish; nothing new starts and nothing is sent back; the principal is told once, not every tick; raising the limit lets work go on without a restart. → Task 6 `TestDecide_HoldDropsNewWorkOnly`, `TestEngine_RaisedLimitLiftsTheHold`.
6. **Two missions are about to start overlapping pieces in the same instant, or a piece is kept behind a mission that is stopped.** Only one of the two starts. A piece behind a stopped mission's piece doesn't sit there unseen: the principal is told once and status names the mission and piece. → Task 4 `TestEngine_TwoEnginesNeverStartOverlappingPieces`, `TestDecide_BehindAStoppedMissionNotifiesOnce`.

---

## Phase A: paths

### Task 1: What a piece may touch, whether two patterns overlap, what fell outside

**Files:** Modify `internal/source/domain_paths.go` (export the check), `internal/mission/paths.go`, `internal/mission/check.go`. Create `internal/mission/paths_overlap.go`, `internal/mission/paths_overlap_test.go`, `internal/mission/paths_piece_test.go`, `cmd/crew/mission_paths.go`, `cmd/crew/mission_paths_test.go`. Modify `cmd/crew/mission.go` (`printPieceTable` and its callers).

**Interfaces (Produces):**
```go
package source
// CheckPaths validates path patterns the way a domain's are validated. It is the
// existing checkPaths, exported; its error text is unchanged: `paths: "<p>": <why>`.
func CheckPaths(paths []string) error

package mission
// EffectivePaths is where a piece may change files: its own paths, or its domain's
// when it lists none. nil means the piece can't be checked.
func EffectivePaths(p Piece, d source.Domain) []string
// PatternsOverlap reports whether one file could match both patterns. When it
// can't tell, it says yes. A malformed or empty pattern overlaps nothing.
func PatternsOverlap(a, b string) bool
// PathsOverlap returns the first pair, one pattern from each list, that overlaps.
func PathsOverlap(a, b []string) (pa, pb string, ok bool)
// Outside returns the files that match none of the patterns, in the order given.
// With no patterns it returns nil.
func Outside(patterns, files []string) []string
// OutsideDomain returns the entries of the piece's own paths that its domain's
// paths don't cover. nil when the piece lists no paths or the domain has none.
func OutsideDomain(p Piece, d source.Domain) []string
```

**`PatternsOverlap`.** Clean both patterns as `MatchPath` does. A pattern with no wildcard character (`*`, `?`, `[`) stands for itself and everything under it, so treat it as `<pattern>/**`. Split into segments and compare from the left:
- both lists used up → overlap;
- one side's next segment is `**` → overlap if skipping the `**` overlaps, or if the other side has a segment left and dropping that segment overlaps;
- otherwise both sides need a segment left, the two segments must be able to match one name, and the rest must overlap.

Two segments can match one name when: both are plain text and equal; or at least one has a wildcard, and their literal prefixes (the text before the first wildcard character; the whole segment when it has none) are such that one is a prefix of the other, and their literal suffixes (the text after the last `*`, `?` or `]`; the whole segment when it has no wildcard) are such that one is a suffix of the other.

**`OutsideDomain`.** For each entry of the piece's own paths, take its literal lead: the segments before the first segment that holds a wildcard, joined with `/`. The entry is covered when some domain pattern `MatchPath`es that lead; an entry whose lead is empty is covered only when the domain lists the same entry or `**`.

**`Check`** gains, per piece: `source.CheckPaths(p.Paths)`; an error is reported as `<id>: <the CheckPaths error>`.

**The plan table and its notes** (`cmd/crew/mission_paths.go`; printed by `crew mission plan`, and by `start` when it waits for go):
- `printPieceTable` gains a last column `PATHS`: the effective paths joined with `, `, or `-` when there are none.
- After the table, one line per piece it applies to, in piece order:
  - `<id> may change files outside the <domain> domain: <entries joined with ", ">`
  - `<id> has no paths, so crew can't check it against other pieces or check what it changes`

- [ ] **Step 1: Write the failing tests**
  - `TestPatternsOverlap`: table. Overlap: `api/**` and `api/v2/**`; `api` and `api/handlers/x.go`; `**/*.md` and `docs/readme.md`; `src/*.go` and `src/main.go`; `**` and `anything/at/all`; `a/**/z` and `a/b/c/z`; `docs` and `docs`. No overlap: `api/**` and `web/**`; `src/*.go` and `src/*.md`; `a/b` and `a/c`; `api/*` and `api/v2/x.go`; `a*b` and `a`; an empty pattern and `**`; `[` (malformed) and `**`.
  - `TestPatternsOverlap_IsSymmetric`: every row of the table gives the same answer with the arguments swapped.
  - `TestPathsOverlap_ReturnsThePair`: `["web/**","api/**"]` and `["docs/**","api/v2/**"]` → `api/**`, `api/v2/**`, true.
  - `TestOutside`: patterns `["api/**","README.md"]`, files `["api/a.go","README.md","web/x.ts","go.mod"]` → `["web/x.ts","go.mod"]`; no patterns → nil.
  - `TestEffectivePaths`: own paths win; none → the domain's; neither → nil.
  - `TestOutsideDomain`: domain `["api/**","**/*.md"]`; piece paths `["api/v2/**","deploy/**","docs/x.md","**"]` → `["deploy/**","**"]`; a piece with no paths → nil; a domain with no paths → nil.
  - `TestCheck_PiecePathsAreValidated`: a piece with `paths: ["/abs"]` and one with `["a/../b"]` → an error naming the piece and the entry.
  - `TestMissionPlan_ShowsPathsAndNotes` (cmd): a plan with three pieces (own paths inside the domain; own paths with one entry outside it; no paths in a domain with none) → the `PATHS` column, the "may change files outside" line for the second and the "has no paths" line for the third, and no note for the first.
- [ ] **Step 2: Run** `go test ./internal/source/ ./internal/mission/ ./cmd/crew/` — Expected: FAIL (undefined names).
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race ./internal/source/ ./internal/mission/ && go test ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: crew knows what a piece may touch and when two pieces' paths overlap`

### Task 2: Pieces whose paths overlap are kept apart

**Files:** Modify `internal/mission/mission.go` (`Piece`, `Render`), `internal/mission/machine.go` (`queued`), `internal/mission/amend.go`, `internal/mission/amend_diff.go`, `internal/mission/decision.go` (`addPiece`), `cmd/crew/mission.go` (`missionPlan`, `startChecks`), `cmd/crew/missionstatus.go` (the queued line). Create `internal/mission/separate.go`, `internal/mission/separate_test.go`, `internal/mission/machine_apart_test.go`, `cmd/crew/mission_apart_test.go`.

**Interfaces:**
- Consumes: `EffectivePaths`, `PathsOverlap`.
- Produces:
```go
// new Piece field, written only by crew
Piece.Apart []string `yaml:"apart_from,omitempty" json:"apart_from,omitempty"`

// Separation says one piece is kept apart from another, and which patterns overlap.
type Separation struct{ Piece, From, PathA, PathB string } // PathA is From's pattern, PathB is Piece's
func (s Separation) String() string // "P3 starts after P1: their paths overlap (P1: api/**, P3: api/v2/**)"

// SeparateOverlaps clears every piece's Apart and sets it again. It takes each piece
// in file order and looks at the pieces before it, from the nearest back to the
// first: when the two could run together and their effective paths overlap, the
// later piece is kept apart from the earlier. settled names pieces that have
// already started: a settled piece is never given an entry, so the other piece of
// the pair is kept apart from it, and a pair of two settled pieces is left alone.
// It returns a new mission and the separations in the order made. It never changes m.
func SeparateOverlaps(m Mission, domains []source.Domain, settled map[string]bool) (Mission, []Separation)

// SeparationChange is the change-log entry for separations: By "crew",
// What the separations' texts joined with "; ", Why "pieces whose paths overlap don't run together".
func SeparationChange(seps []Separation, now time.Time) Change
```

**"Could run together":** neither piece reaches the other by following `waits_for` and the `Apart` entries already set in this call, in any number of hops. Because an entry is only added between two pieces that don't reach each other, the result never has a circle.

**`queued` in `Decide`:** a piece is ready when every `waits_for` piece is `complete` (as today) and every `Apart` piece is `complete` or `cancelled`. An `Apart` piece that doesn't exist counts as finished.

**Where it is applied.** Each caller appends `SeparationChange` to the mission's changes only when the set of `(Piece, From)` pairs differs from what the mission had before the call.
- `crew mission plan`: after `Check` passes, with no settled pieces, before the mission is created. After the table and the path notes it prints one line per separation (`Separation.String()`).
- `startChecks` (so `start`, `go` and `resume`): with the pieces whose state is not `queued` as settled. When the pairs changed it saves the mission and prints the lines.
- `Amend` and the orchestrator's `add`: after the new plan passes `Check`, with the non-queued pieces as settled. The amendment's own diff (`amend_diff.go`) ignores `apart_from`, and an incoming plan's `apart_from` is discarded.

**Display:** `Render` writes `apart_from` after `waits_for`. In `crew mission status`, a queued piece with `Apart` entries that are not finished shows `starts after <ids> (paths overlap)` where it shows what it waits for.

- [ ] **Step 1: Write the failing tests**
  - `TestSeparateOverlaps_LaterIsKeptApart`: P1 `api/**`, P2 `web/**`, P3 `api/v2/**`, no waits → only P3 gets `Apart: [P1]`; one `Separation{P3, P1, "api/**", "api/v2/**"}`; the input mission is unchanged.
  - `TestSeparateOverlaps_PiecesThatAlreadyWaitAreLeft`: P2 waits for P1 and they overlap → nothing; P3 waits for P2 and overlaps P1 → nothing (reached in two hops).
  - `TestSeparateOverlaps_UsesTheDomainsPaths`: two pieces with no paths in a domain with `src/**` → the second is kept apart; in a domain with no paths → nothing.
  - `TestSeparateOverlaps_ChainOfThree`: three pieces with the same paths → `P2: [P1]` and `P3: [P2]` only. P3 is looked at against P2 first, and by then it reaches P1 through P2, so no `P3: [P1]` is added.
  - `TestSeparateOverlaps_SettledPieceIsNeverMoved`: P1 queued, P2 settled, overlapping → P1 gets `Apart: [P2]`; both settled → nothing.
  - `TestSeparateOverlaps_StartsFromScratch`: a mission whose pieces carry stale `Apart` entries → only the computed ones remain.
  - `TestSeparateOverlaps_NeverMakesACircle`: for 200 seeded random missions of up to 8 pieces with random acyclic waits and paths drawn from 4 patterns, the result passes `Check`'s circle test with `apart_from` treated as waits.
  - `TestDecide_ApartPieceWaitsUntilTheOtherIsDone`: P2 `Apart: [P1]`, P1 running → no `StartPiece{P2}`; P1 `pr-open` → none; P1 `complete` → `StartPiece{P2}`.
  - `TestDecide_ApartFromACancelledPieceStarts`: P1 cancelled → `StartPiece{P2}` and no wait is opened.
  - `TestParseRender_ApartFromRoundTrips`.
  - `TestMissionPlan_PrintsSeparations` (cmd): the line for P3 and P1; `mission.md` holds `apart_from: [P1]` and a change by `crew`.
  - `TestStartChecks_SeparatesAgainOnlyWhenItChanged` (cmd): a second `start` adds no second change line.
  - `TestAmend_NewOverlappingPieceIsKeptApart`: an amendment adds P4 overlapping running P1 → P4 `Apart: [P1]`; a supplied `apart_from: [P9]` is gone.
- [ ] **Step 2: Run** `go test ./internal/mission/ ./cmd/crew/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race ./internal/mission/ && go test ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: pieces whose paths overlap don't run together`

---

## Phase B: running together

### Task 3: More than one piece at a time, counted across the repo

**Files:** Modify `internal/mission/settings.go`, `internal/mission/settings_test.go`, `internal/scaffold/template/missions.yaml`, `internal/mission/machine.go` (`Obs`, `queued`, `holdingRuns`), `internal/mission/engine.go` (`observe`), `internal/mission/effects.go` (`startPiece`), `internal/mission/fakes_test.go`, `cmd/crew/mission_jira.go` or a new `cmd/crew/mission_limits.go` (one output line). Create `internal/mission/repo_runs.go`, `internal/mission/repo_runs_test.go`, `internal/mission/engine_parallel_test.go`.

**Interfaces (Produces):**
```go
// HoldingRuns counts the pieces of s that count toward runs_at_once.
func HoldingRuns(s State) int
// RunsElsewhere counts the runs held by the repo's other missions whose status is running.
func RunsElsewhere(repoRoot, missionID string) (int, error)
// lockStarts takes the repo's start lock, <repoRoot>/.crew/missions/starts.lock,
// trying for up to 2 seconds. The caller releases it.
func lockStarts(repoRoot string) (release func(), err error)

Obs.Elsewhere        int  // runs held by the repo's other missions
Obs.ElsewhereUnknown bool // they could not be counted: nothing is started this tick
```

**Settings.** Remove `maxRunsAtOnce` and `checkRunsAtOnce`: `runs_at_once` is a limit like the others. The template's line becomes `runs_at_once: 3` with the comment `# across all of this repo's missions. A piece crew sends back from its pull request runs again at once, whatever this says`.

**`observe`** fills `Elsewhere` with `RunsElsewhere`. A failure is the note `can't count the repo's other missions: <error>` and sets `ElsewhereUnknown`.

**`queued`** starts a ready piece when `!o.ElsewhereUnknown && Allows(lim.Concurrency, o.Elsewhere+holdingRuns()+starting()+1)`.

**`startPiece`** closes the gap between two engines. Before it touches git: take `lockStarts`; count `RunsElsewhere` again and add `HoldingRuns` of the tick's state; if one more would pass the limit, release and return, leaving the piece queued with no note. Keep the lock until `Started` is saved, then release, then drive the run. A lock that can't be taken leaves the piece queued with no note.

**Output.** `crew mission plan`, and `start` when it waits for go, print after the auto-merge line: `Runs at once: <n> across this repo's missions (limits.runs_at_once in missions.yaml)`, with `no limit` for `"*"`.

**Test support.** `fakeGit` keeps answering every worktree alike unless a test sets facts for one worktree: add `fakeGit.at map[string]gitFacts` (`dirty`, `ahead`, `changed`) looked up by worktree path first. `newHarnessIn(t, root string, pieces ...Piece)` builds a second mission in the same repo root.

- [ ] **Step 1: Write the failing tests**
  - Replace the one-at-a-time settings tests: `TestLoadSettings_RunsAtOnceAboveOneIsAccepted` (3 and `"*"`, in `limits` and under `repos`), `TestLoadSettings_TemplateRunsThreeAtOnce`.
  - `TestHoldingRuns`: running, waiting back to running, waiting back to queued, pr-open, delivered, complete → 2.
  - `TestRunsElsewhere`: three missions in one root (this one; a running one holding 2; a stopped one holding 1) → 2; a folder that is not a mission, and the lock file itself, are ignored.
  - `TestDecide_StartsUpToTheLimit`: four independent queued pieces, limit 3 → three `StartPiece`, in file order; limit `"*"` → four.
  - `TestDecide_CountsTheRepoOtherMissions`: limit 3, `Elsewhere: 2` → one start; `Elsewhere: 3` → none; `ElsewhereUnknown` → none.
  - `TestEngine_StartsTwoPiecesInOneTick`: two independent pieces, limit 2 → both `running` after one tick, each with its own worktree and branch.
  - `TestEngine_StartGateCountsOtherMissions`: mission A (limit 1) has a running piece; mission B in the same root ticks → nothing starts; A's piece completes; B ticks → its piece starts.
  - `TestEngine_TwoEnginesNeverPassTheLimit`: two missions in one root, limit 1, each with one ready piece; tick both from two goroutines, 25 rounds with fresh missions → never more than one running piece across the two states.
  - `TestMissionPlan_PrintsRunsAtOnce` (cmd).
- [ ] **Step 2: Run** `go test ./internal/mission/ ./internal/scaffold/ ./cmd/crew/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race -count=3 ./internal/mission/ && go test ./internal/scaffold/ ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: a mission runs several pieces at once, within the repo's limit`

### Task 4: A piece stays behind an overlapping piece of another mission

**Files:** Modify `internal/mission/state.go` (`PieceState`, its clone), `internal/mission/machine.go` (`Obs`, `queued`), `internal/mission/reduce.go` (`Started`), `internal/mission/decision.go` (`cancelPiece`), `internal/mission/engine.go` (`observe`), `internal/mission/effects.go` (`startPiece`), `cmd/crew/mission_paths.go`, `cmd/crew/mission.go` (`missionPlan`, `startChecks`), `cmd/crew/missionstatus.go` (the queued line). Create `internal/mission/repo_claims.go`, `internal/mission/repo_claims_test.go`, `internal/mission/machine_behind.go`, `internal/mission/machine_behind_test.go`, `internal/mission/engine_behind_test.go`, `cmd/crew/mission_behind_test.go`.

**Interfaces:**
- Consumes: `EffectivePaths`, `PathsOverlap` (Task 1); `lockStarts`, `Obs.ElsewhereUnknown`, `newHarnessIn` (Task 3).
- Produces:
```go
// Claim is a piece of another mission in this repo that has started and is not
// finished, with the paths it may change.
type Claim struct {
    Mission string   // the other mission's id
    Piece   string
    Paths   []string // its effective paths; never empty
    Stopped bool     // that mission's status is stopped
}
// ClaimsElsewhere lists what the repo's other missions hold, by mission id, then
// in piece order. A piece holds its effective paths from the moment it starts
// until it is complete or cancelled, whatever its mission's status. A piece
// with no effective paths holds nothing.
func ClaimsElsewhere(repoRoot, missionID string, domains map[string]source.Domain) ([]Claim, error)

// Blocker names the other mission's piece that a queued piece is kept behind.
type Blocker struct {
    Mission string `json:"mission"`
    Piece   string `json:"piece"`
    PathA   string `json:"path_a"` // the other piece's pattern
    PathB   string `json:"path_b"` // this piece's pattern
    Stopped bool   `json:"stopped,omitempty"`
}
// "P1 of mission 20261122-ab12: their paths overlap (api/**, api/v2/**)"
// "P1 of mission 20261122-ab12, which is stopped: their paths overlap (api/**, api/v2/**)"
func (b Blocker) String() string
// BlockedBy returns the first claim whose paths overlap the given paths.
func BlockedBy(paths []string, claims []Claim) (Blocker, bool)

PieceState.Behind *Blocker `json:"behind,omitempty"`
Obs.Behind map[string]Blocker // queued pieces of this mission whose paths overlap a claim
```
`PieceState.clone` copies `Behind`.

**`ClaimsElsewhere`** walks the missions folder as `RunsElsewhere` does and skips this mission. For a mission with at least one piece whose status is not `queued`, `complete` or `cancelled`, it reads `mission.md`; a file that can't be read or parsed is an error, because crew can't tell what that mission holds.

**`observe`** calls `ClaimsElsewhere` once. A failure is the note `can't read the repo's other missions: <error>` and sets `ElsewhereUnknown`, so nothing starts this tick. Otherwise, for every `queued` piece of this mission, `BlockedBy(EffectivePaths(piece, domain), claims)` fills `Obs.Behind`.

**`queued` in `Decide`.** A piece that is ready (its `waits_for` and `apart_from` are satisfied) and has an entry in `Obs.Behind` is not started, whatever the limit says:
- its `Behind` becomes that blocker;
- when the blocker's `Mission`, `Piece` or `Stopped` differs from what the piece held, a `note` record on the piece: `starts after <Blocker.String()>`;
- when it differs and the new blocker is stopped, one notification as well: `<id> can't start: it overlaps <piece> of mission <mission>, which is stopped. Resume that mission, or cancel that piece with crew mission amend.`

A ready piece with no entry has its `Behind` cleared and goes on to the limit check. A piece that isn't ready keeps no `Behind`. `Started` and `cancelPiece` clear it.

**`startPiece`,** under the start lock and after the count of Task 3: call `ClaimsElsewhere` again and `BlockedBy` for this piece. Blocked → release and return, leaving the piece queued with no note; the next tick's `Decide` records why. A read that fails → release, return, and the same note as in `observe`.

**Output.**
- `crew mission plan`, and `startChecks` (so `start`, `go` and `resume`), print after the separation lines one line per piece of this mission that is not complete or cancelled and overlaps a claim: `<id> starts after <Blocker.String()>`. Nothing is saved: the check that counts is the one made when the piece starts.
- `crew mission status`: a queued piece with `Behind` shows `starts after <piece> of mission <mission> (paths overlap)`, with `, which is stopped` after the mission id when so, where it shows what it waits for.

- [ ] **Step 1: Write the failing tests**
  - `TestClaimsElsewhere`: one root, domains `backend` (`api/**`) and `site` (`web/**`). This mission has a running piece. A running mission has P1 running with `paths: [api/v2/**]`, P2 queued, P3 complete, P4 pr-open with no paths in `site`, P5 delivered in a domain with no paths. A stopped mission has P1 waiting with `paths: [docs/**]`. A planned mission has one piece. → exactly three claims: the running mission's P1 `[api/v2/**]` and P4 `[web/**]`, and the stopped mission's P1 `[docs/**]` with `Stopped`.
  - `TestClaimsElsewhere_UnreadableMissionIsAnError`: a mission whose state has a running piece and whose `mission.md` is gone → an error naming the mission. The same folder with every piece queued → no error.
  - `TestBlockedBy`: paths `["web/**","api/**"]` against claims `[{A, P1, [docs/**]}, {B, P2, [api/v2/**], Stopped}]` → `Blocker{B, P2, "api/v2/**", "api/**", true}`; no overlap → false; nil paths → false.
  - `TestBlocker_String`: the two forms above.
  - `TestDecide_PieceBehindAnotherMissionDoesNotStart`: a ready queued P1, limit 3, `Obs.Behind{P1: …}` → no `StartPiece`, the state's `Behind` is set, one `note` record whose text starts `starts after`; a second `Decide` on that state with the same observation → no record; then an observation without the entry → `Behind` nil and `StartPiece{P1}`.
  - `TestDecide_BehindAStoppedMissionNotifiesOnce`: the blocker has `Stopped` → one `Notify` naming the piece, the other mission and `crew mission amend`; a second `Decide` → none. A blocker that was held with `Stopped: false` and is now observed with `Stopped: true` → a record and a notification.
  - `TestDecide_BehindIsOnlyForReadyPieces`: P2 waits for a running P1 and has an entry in `Obs.Behind` → `Behind` stays nil, no record.
  - `TestDecide_OtherReadyPiecesStillStart`: P1 has an entry, P2 doesn't, limit 3 → `StartPiece{P2}` only.
  - `TestStarted_ClearsBehind`; `TestCancelPiece_ClearsBehind`.
  - `TestEngine_PieceWaitsForTheOtherMissionsPiece`: missions A and B in one root, limit 3. A's P1 (`src/**`) is running. B ticks → B's P1 (`src/api/**`) is `queued` with `Behind` naming A's P1, and B's P2 (`docs/**`) is `running`. A's P1 completes; B ticks → B's P1 is `running` and `Behind` is nil.
  - `TestEngine_StoppedMissionStillHoldsItsPaths`: A is stopped with P1 `pr-open` (`src/**`); B ticks → B's P1 stays queued, `Behind.Stopped` is true, and the fake notifier got one message.
  - `TestEngine_TwoEnginesNeverStartOverlappingPieces`: two missions in one root, limit 3, each with one ready piece with `paths: [src/**]`; tick both from two goroutines, 25 rounds with fresh missions → in every round exactly one of the two pieces has left `queued`.
  - `TestEngine_ClaimsUnreadableStartsNothing`: the other mission's `mission.md` is gone while its piece runs → a note, nothing starts.
  - `TestMissionPlan_PrintsOtherMissionsOverlap` (cmd): a running mission in the repo holds `api/**`; a new plan with a piece `api/v2/**` → the line naming that mission and piece; a piece `docs/**` gets none.
  - `TestMissionStatus_QueuedPieceBehindAnotherMission` (cmd): both forms of the status text.
- [ ] **Step 2: Run** `go test ./internal/mission/ ./cmd/crew/` — Expected: FAIL.
- [ ] **Step 3: Implement.** Put `Blocker`, `BlockedBy` and the `queued` case's helper in `machine_behind.go`; `Claim` and `ClaimsElsewhere` in `repo_claims.go`.
- [ ] **Step 4: Run** `go test -race -count=3 ./internal/mission/ && go test ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: a piece waits for an overlapping piece of another mission in the repo`

---

## Phase C: staying inside the lines

### Task 5: A piece that changed files outside its paths stops

**Files:** Modify `internal/mission/state.go`, `internal/mission/machine.go` (`GitObs`, `done`, `ask`), `internal/mission/engine.go` (`observeRun`), `internal/mission/answer.go`, `internal/mission/decision.go` (`escalate`, the goto step's note), `internal/mission/decision_pr.go` (`allowedFor`), `internal/mission/summary.go`, `internal/mission/summary_prompt.go`, `internal/mission/effects.go` (`askOrchestrator`). Create `internal/mission/machine_paths.go`, `internal/mission/machine_paths_test.go`, `internal/mission/answer_paths_test.go`, `internal/mission/engine_paths_test.go`.

**Interfaces:**
- Consumes: `EffectivePaths`, `Outside`, `GitOps.ChangedFiles`.
- Produces:
```go
const (
    reasonOutOfPaths = "out-of-paths"
    WaitPaths        = "paths"
    maxOutsideFiles  = 200
)
GitObs.Outside []string // changed files outside the piece's effective paths; nil when it has none

PieceState.Outside     []string `json:"outside,omitempty"`      // the files the piece is stopped for, at most maxOutsideFiles
PieceState.OutsideMore int      `json:"outside_more,omitempty"` // how many more there are
PieceState.Allowed     []string `json:"allowed,omitempty"`      // files outside its paths that the principal accepted

Summary.Outside []string // for the orchestrator: the files outside the paths
```
`PieceState.clone` copies the two slices.

**`observeRun`,** for a run that is done and has commits ahead: `ChangedFiles(worktree, "origin/"+base)`, then `Outside(EffectivePaths(piece, domain), files)`. A failure is the note `can't list the files the piece changed: <error>` and leaves the piece out of `Obs.Git`.

**`done` in `Decide`** gains one case, after the uncommitted-files cases and the deliverable collection, before a pull request is opened:
- the files in `Git.Outside` that are not in the piece's `Allowed` are not empty → `Outside` becomes the first `maxOutsideFiles` of them, `OutsideMore` the rest's count, and the piece is put to the orchestrator: `ask(id, reasonOutOfPaths)`. No pull request is opened.
- otherwise `Outside` and `OutsideMore` are cleared and the piece goes on as today.

**The orchestrator.** `allowedFor(reasonOutOfPaths)` is `goto`, `cancel`, `escalate`. Its summary lists `Outside` under `Files changed outside the piece's paths:` and, when `OutsideMore > 0`, `and <n> more`. A `goto` it chooses sends the station this note, followed by the orchestrator's own: `These files are outside this piece's paths: <names, at most 20> (and <n> more). Undo those changes or move the work into the piece's paths.`

**Reaching the principal.** For this failure, the wait opened when the orchestrator escalates, when its decision can't be used, or when its call limit is hit, is a `paths` wait that goes back to `running`. Its question: `<id> changed <n> files outside its paths: <first three names> and <m> more`.

**Answers to a `paths` wait** (`answerChoices`):

| When | Choices |
|---|---|
| `OutsideMore == 0` | `accept`, `goto`, `cancel` |
| otherwise | `goto`, `cancel` |

- `accept`: `Allowed` gains `Outside`; `Outside` is cleared; the wait is cleared and the piece is `running` again, with no run step. The next tick finds nothing unaccepted and opens the pull request. Event `decision` by `principal`, `Msg: "accepted <n> files outside the paths"`.
- `goto`: the usual `goto` step; the note is the one above followed by the principal's.
- `cancel`: as for any piece.

`accept` on any other wait kind stays an error, and `accept` on a `deliverable` wait is unchanged.

- [ ] **Step 1: Write the failing tests**
  - `TestOutside_DeletionsAndMovesCount`: the changed list a move and a deletion produce (`["api/a.go","web/old.ts","web/new.ts","docs/gone.md"]`) against `["api/**"]` → the three files outside.
  - `TestDecide_OutsideFilesStopThePiece`: run done, clean, 2 ahead, `Outside: ["go.mod"]` → exactly `AskOrchestrator{Failure: "out-of-paths"}`, no `OpenPR`, and the state's `Outside` is `["go.mod"]`.
  - `TestDecide_NoOutsideFilesOpensThePR`; `TestDecide_PieceWithNoPathsIsNotChecked` (`Outside` nil).
  - `TestDecide_OutsideIsCappedAndCounted`: 250 files → `Outside` holds 200, `OutsideMore == 50`.
  - `TestDecide_CallLimitOpensAPathsWait`: the piece has used its orchestrator calls → a `paths` wait back to `running` whose question names the count and the first three files.
  - `TestDecide_AcceptedFilesStayAcceptedNewOnesAreAsked`: `Allowed: ["go.mod"]`, `Outside: ["go.mod"]` observed → `OpenPR`; then `["go.mod","Makefile"]` observed → asked, with `Outside == ["Makefile"]`.
  - `TestAllowedFor_OutOfPaths`: `goto`, `cancel`, `escalate`; no `retry`.
  - `TestApplyDecision_EscalateOutOfPathsOpensAPathsWait`; `TestApplyDecision_GotoCarriesTheFiles` (the step's note starts with the files sentence and ends with the orchestrator's note).
  - `TestSummaryPrompt_ListsOutsideFiles`.
  - `TestAnswer_AcceptOutsideFiles`: → `Allowed` holds them, `Outside` empty, status `running`, no wait, no run step, a `decision` event by `principal`.
  - `TestAnswer_AcceptRefusedWhenThereAreMore`: `OutsideMore: 5` → the error lists `goto, cancel`.
  - `TestAnswer_GotoOnAPathsWait`: a `goto` step whose note carries the files.
  - `TestEngine_OutsidePathsEndToEnd`: fake git reports `changed: ["api/a.go","go.mod"]` for a piece with `paths: [api/**]`; the fake orchestrator escalates → no `OpenPR` call, a `paths` wait; answer `accept` → next tick opens the pull request.
  - `TestEngine_ChangedFilesFailureDecidesNothing`: `ChangedFiles` fails → a note, no `OpenPR`, no ask.
- [ ] **Step 2: Run** `go test ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement.** Put the new `done` case and its helpers in `machine_paths.go`.
- [ ] **Step 4: Run** `go test -race ./internal/mission/ && go test ./cmd/crew/` — Expected: PASS, the earlier stages' end-to-end tests included.
- [ ] **Step 5: Commit** `feat: a piece that changes files outside its paths stops for the principal`

### Task 6: The step budget puts a mission on hold

**Files:** Modify `internal/mission/state.go`, `internal/mission/events.go`, `internal/mission/machine.go` (`Decide`, `ask`), `internal/mission/engine.go` (the reload), `internal/mission/summary.go`, `internal/mission/tracker_text.go`, `cmd/crew/missionrun.go` (`newMissionEngine`). Create `internal/mission/machine_budget.go`, `internal/mission/machine_budget_test.go`, `internal/mission/engine_budget_test.go`.

**Interfaces (Produces):**
```go
const EvBudgetHit = "budget-hit"

PieceState.Steps int `json:"steps,omitempty"` // station steps its run has taken, as last seen

// Hold says the mission takes on no new work because a budget ran out.
type Hold struct {
    Kind  string    `json:"kind"`  // "steps"
    Count int       `json:"count"`
    Max   string    `json:"max"`
    Since time.Time `json:"since"`
}
State.Hold *Hold `json:"hold,omitempty"`

// StepsUsed is the mission's station steps so far, summed over its pieces.
func StepsUsed(s State) int

Engine.Reload func() (Settings, error) // nil: the limits never change while the engine runs
Summary.StepsLeft string               // for the orchestrator, like CallsLeft; BuildSummary gains a last parameter stepsUsed int
```
`State.Clone` copies `Hold`.

**Counting.** At the start of `Decide`, for every piece with an observed run, `Steps` becomes the larger of what it holds and the run's `State.Step`.

**The hold.** The mission is over budget when `!Allows(lim.Steps, StepsUsed(s)+1)`.
- Over budget and not on hold → `Hold` is set; an event `budget-hit` with `Count` the steps used, `Max` the limit's text and `Msg: "station steps for this mission"`; and one notification: `step budget reached (<count> of <max>): no new piece starts and nothing is sent back until limits.steps is raised in missions.yaml`.
- On hold → after the rest of `Decide` has run, the actions `StartPiece`, `SendBack`, `AskOrchestrator` and `DriveRun` are dropped. Everything else stays: pull requests are opened and merged, deliverables are collected and accepted, waits are opened and reminded. Runs already going are not stopped.
- On hold and no longer over budget → `Hold` is cleared and a `note` event says `step budget raised to <max>: work goes on`.

What the principal asks for is still carried out while on hold: an answer's `retry`, `goto` or `approve` step runs.

**Reload.** At the start of a tick, when the saved state has a `Hold` and `Engine.Reload` is set, the engine calls it and uses the returned settings from then on. A failure is the note `can't read missions.yaml: <error>` and the old settings stay. `newMissionEngine` sets `Reload` to load the crew directory's settings.

**Other budgets.** When a piece hits the orchestrator call limit, `ask` also records `budget-hit` for that piece: `Count` its calls, `Max` the limit's text, `Msg: "orchestrator calls for this piece"`.

**The board.** `TrackerComment` gains: `budget-hit` → on the piece, or on the epic when the event has no piece: `Budget reached: <msg> (<count> of <max>)`.

- [ ] **Step 1: Write the failing tests**
  - `TestStepsUsed`: pieces holding 3, 0 and 4 → 7.
  - `TestDecide_RecordsStepsFromRuns`: an observed run at step 5 on a piece holding 2 → 5; a run at step 1 on a piece holding 4 → 4.
  - `TestDecide_BudgetHitHoldsOnce`: limit 6, pieces at 6 → `Hold` set, one `budget-hit` record with `Count: 6`, `Max: "6"`, one notification; a second `Decide` on that state → no second record and no second notification.
  - `TestDecide_HoldDropsNewWorkOnly`: on hold with a ready queued piece, a piece whose run is done with uncommitted files, a pr-open piece whose checks failed, and a done piece with commits and nothing else wrong → no `StartPiece`, no `SendBack`, no `AskOrchestrator`, no `DriveRun`; the `OpenPR` is still there.
  - `TestDecide_HoldLiftsWhenTheLimitRises`: on hold, limit raised → `Hold` nil, a `note` record, and the queued piece starts in the same `Decide`.
  - `TestDecide_UnlimitedStepsNeverHold`: limit `"*"`.
  - `TestDecide_CallLimitRecordsBudgetHit`.
  - `TestTrackerComment_BudgetHit`: with and without a piece.
  - `TestBuildSummary_StepsLeft`.
  - `TestEngine_RaisedLimitLiftsTheHold`: a harness with limit 2 whose first piece's run reports step 2 → on hold after the tick, the second piece not started; `Reload` then returns settings with limit 10 → the next tick starts the second piece.
  - `TestEngine_ReloadIsOnlyCalledOnHold`: three ticks with no hold → `Reload` never called.
  - `TestEngine_AnswerRunsWhileOnHold`: on hold, a `question` wait answered `retry` → the run step is carried out.
- [ ] **Step 2: Run** `go test ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement.** Put the counting, the hold and the action filter in `machine_budget.go`.
- [ ] **Step 4: Run** `go test -race ./internal/mission/ && go test ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: a mission that has used its station steps goes on hold`

---

## Phase D: status, the guide, end to end

### Task 7: Status, the orchestrator's instructions, the guide, end to end

**Files:** Modify `cmd/crew/missionstatus.go` (or a new `cmd/crew/missionstatus_limits.go`), `internal/mission/orchestrator.go` (`orchestratorBody`), `internal/mission/orchestrator_test.go`, `docs/missions.md`, `README.md` only if it states the one-at-a-time rule. Create `cmd/crew/missionstatus_limits_test.go`, `cmd/crew/mission_e2e_parallel_test.go`, and the fake harness scripts the end-to-end tests need under `cmd/crew/testdata/`.

**`crew mission status <id>`**
- When the mission is on hold, a line after the first: `On hold: <count> of <max> station steps used. Raise limits.steps in missions.yaml; the engine reads it again within a minute.`
- **Waiting on you:** a `paths` wait's line is `<wait>  <piece>  paths  <question>  waiting <time>`.
- A queued piece kept apart shows `starts after <ids> (paths overlap)` (Task 2), and one kept behind another mission's piece shows that piece and mission (Task 4).

**`orchestratorBody`** gains, in its existing plain style:
- pieces run together when their paths don't overlap, so give each piece the narrowest `paths` that covers its work; crew keeps overlapping pieces apart and says so in the plan;
- the same holds across missions in one repo: a piece doesn't start while a piece of another mission that overlaps it is under way, even when that mission is stopped. `crew mission status` names the mission and piece. Tell the principal; the ways out are to let that piece finish, to resume that mission, or to cancel that piece with `crew mission amend`;
- a piece that changes files outside its paths is stopped: send it back with `goto`, cancel it, or escalate. Only the principal can accept the files;
- the answers for a `paths` wait (`accept`, `goto`, `cancel`), beside the other wait kinds;
- a mission on hold for its step budget takes no new work until the principal raises the limit; the orchestrator never edits `missions.yaml`.

**`docs/missions.md`**
- Rewrite "What this stage does and doesn't do": pieces run together up to `runs_at_once`, counted across the repo's missions; what counts toward it; the send-back exception; what is still missing (delegation, releases). Remove the sentence that tells the reader not to run two missions over the same part of a repo.
- "Mission settings": `runs_at_once` is a normal limit, 3 in a new crew directory, and an existing `missions.yaml` keeps the value it has; `steps` is enforced.
- New section **"Running pieces together"**: paths and their default from the domain; the `PATHS` column and the two notes; how crew decides two patterns overlap, and that it says yes when unsure; `apart_from`, what the plan prints, that it is crew's field and is recomputed on every plan, start, amendment and added piece; that a piece kept apart from a cancelled piece starts.
- New section **"Two missions in one repo"**: `runs_at_once` is shared; a piece doesn't start while a piece of another mission with overlapping paths has started and is not complete or cancelled; this holds for a stopped mission too, and the three ways out (let it finish, `resume`, cancel the piece with `amend`); what the plan prints and what status shows; the one notification when the other mission is stopped; that a piece with no paths isn't checked, and that files accepted outside a piece's paths don't count.
- New section **"Files outside a piece's paths"**: what is checked and when (deletions and both ends of a move count); what the orchestrator may do; the `paths` wait and its three answers; the 200-file limit on `accept`; that accepted files stay accepted and new ones are asked about; that a piece with no paths isn't checked.
- New section **"Budgets"**: the six limits and what each does when reached; the step budget's hold in full, what still happens on hold and how to lift it; `budget-hit` in the event list.
- "What 'waiting on you' means" and the commands table: the `paths` wait and `--do accept` for it.
- Correct this sentence from stage 3, in the Jira section: a write Jira refuses for good is noted once and not tried again, *except* creating an issue and placing issues in a sprint, which are tried again on every pass.

**End to end** (the fake harness, the fake `gh`; `runs_at_once` set in the test's `missions.yaml`):
- `TestMission_EndToEnd_TwoPiecesTogether`: limit 2, two independent pieces with paths `a/**` and `b/**` → after the first tick both are `running`; both open a pull request; both complete.
- `TestMission_EndToEnd_OverlappingPiecesTakeTurns`: limit 2, both pieces `src/**` → the plan output names the separation; P2 is still `queued` while P1's pull request is open; P2 starts after P1 is merged.
- `TestMission_EndToEnd_TwoMissionsTakeTurns`: limit 2, two missions in one repo, each with one piece with `paths: [src/**]` → the second mission's piece is `queued` and its status names the first mission's piece while that pull request is open; it starts after the merge.
- `TestMission_EndToEnd_FilesOutsidePaths`: a piece with `paths: [docs/**]` whose station commits a file at the repo root; the orchestrator stand-in escalates → no pull request, `crew mission status` shows the `paths` wait; `crew mission answer … --do accept` → the pull request opens.
- `TestMission_EndToEnd_StepBudget`: `steps` set to what one piece uses, two pieces where P2 waits for P1 → after P1 the mission is on hold and status says so; the test rewrites `missions.yaml` with a higher limit → P2 starts.

- [ ] **Step 1: Write the failing tests:** the five end-to-end tests; `TestMissionStatus_OnHoldAndPathsWait`; `TestOrchestratorBody_MentionsPathsAndBudgets` (contains `paths`, `accept`, `on hold`, `missions.yaml` and `another mission`).
- [ ] **Step 2: Run** `go test ./cmd/crew/ ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement,** then write the guide changes.
- [ ] **Step 4: Run** `go build ./... && go vet ./... && go test -count=1 ./... && go test -race -count=1 ./internal/mission/ ./internal/gitx/ ./internal/source/` — Expected: PASS. `TestDocs_ExamplePlansParse` still passes.
- [ ] **Step 5: Commit** `feat: status and the guide cover parallel pieces, paths and budgets`
