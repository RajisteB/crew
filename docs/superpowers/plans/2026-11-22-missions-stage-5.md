# crew missions, stage 5: delegation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A piece marked `delegate: true` is split into sub-pieces by an orchestrator scoped to that piece's domain, once the piece is ready. crew accepts only sub-pieces that stay inside the delegated piece's domain, factories and paths, runs them like any other piece, and finishes the delegated piece when they are finished. A stuck sub-piece is put to the scoped orchestrator first, then up the tree, then to the principal.

**Architecture:** A sub-piece is an ordinary piece in `mission.md` whose id is its parent's id plus `.<number>`; the tree is read from the ids, and no new field is written. `Check` gains the rules that make authority narrow going down, so every place that already checks a plan (start, resume, an amendment, an added piece) enforces them. `Decide` gains one action, `PlanPieces`, for a delegated piece that is ready, and one pass that finishes a delegated piece from its sub-pieces. The engine carries `PlanPieces` out with a second method on the existing `Orchestrator` interface, and a pure function, `ApplySubPlan`, turns the reply into new pieces or a refusal. The exception path stays as it is, with two additions: the summary carries a scope, and an escalation by a scoped orchestrator raises the question one level instead of opening a wait.

**Tech Stack:** Go 1.22 module (toolchain 1.24), `gopkg.in/yaml.v3` only, stdlib `testing`.

**Spec:** `docs/superpowers/specs/2026-11-18-orchestrator-missions-design.md`. Read "The mission file", "Checks at `crew mission start`", "When a piece is complete", "Scoped orchestrator (recursion)", "Exceptions and authority" and walkthrough B before any task. This plan covers **stage 5 of its Build order**.

**Seven places where this plan settles what the spec leaves open, or departs from its wording, on purpose:**

1. *"Delegation depth: 2."* Depth counts hops. A top-level delegated piece is at depth 1; a delegated sub-piece of it is at depth 2. A limit of 0 switches delegation off.
2. *"Sub-pieces are named under the parent."* crew names them by position, `<parent>.1`, `<parent>.2`, …, whatever ids the reply uses; the reply's own ids only tie its `waits_for` entries to its pieces. A number is never used twice under one parent.
3. *"They inherit everything the parent waits for."* Nothing is written for this. A delegated piece is planned only once everything it waits for, and everything it is kept apart from, is finished, and a planned piece can no longer be given a new wait. So its sub-pieces can never run before what it waited for.
4. *"A delegated piece is complete when every one of its sub-pieces is."* A cancelled sub-piece counts as finished, as a cancelled piece does for the mission. A delegated piece whose sub-pieces were all cancelled is itself cancelled, so a piece waiting for it gets the usual question.
5. *"Anything outside the list escalates: up the tree, then to the principal."* This holds for a stuck sub-piece. A problem while *planning* a delegated piece (a refused plan, a question) goes to the principal directly once the piece's orchestrator calls are used: no orchestrator above has an action that edits a plan it did not make.
6. *"How sub-pieces sit under the epic"* (left to the builder). Each sub-piece is an issue under the epic like any other piece, named by its id, so it is a card on the board. A Jira sub-task would not be.
7. *"Scoped orchestrators get read tools only."* The headless orchestrator session already has read and search only and the stations' deny rules. A scoped orchestrator is that same session with a narrower prompt; what makes it scoped is what crew accepts from it.

**Not in this plan:** planning a delegated piece a second time once it has sub-pieces; an amendment that adds a sub-piece (it adds top-level pieces, and removes or re-briefs sub-pieces that haven't started); a gate where the principal approves a sub-plan (the design has none: authority only narrows); a delegated piece holding its paths against other missions (its sub-pieces do, one by one, as every piece does); golden missions; the cutover (stage 6).

## Global Constraints

- The only dependency is `gopkg.in/yaml.v3`. No new modules.
- Before writing Go, load the `use-modern-go` skill and follow it for Go 1.22.
- Config and mission files decode strictly: unknown keys are errors. A model's JSON reply is decoded the same way.
- State is immutable: every state-machine function takes a `State` and returns a new one. `Decide`, `Check`, `SeparateOverlaps`, `ApplySubPlan`, `ApplyDecision` and the tree and path helpers perform no I/O.
- **Authority only narrows going down.** A sub-piece has its parent's domain, a factory of that domain, and paths inside its parent's paths. It waits only for pieces under its own parent. These are rules of `Check`, so they hold wherever a plan is checked.
- **crew names sub-pieces.** No plan, amendment or orchestrator supplies an id with a `.`.
- **When crew can't tell whether one pattern lies inside another, it doesn't.** A refused sub-plan costs an orchestrator call; an accepted one that reaches outside costs the principal's trust.
- **A delegated piece never has a run.** It holds no run slot, has no worktree, branch or pull request, and holds no paths against other missions.
- **Limits are the principal's.** `pieces` counts sub-pieces. Each planning call and each exception call counts toward `orchestrator_calls_per_piece` of the piece it is about. `orchestrator_additions` is one count for the mission, whoever adds.
- **Nothing a model replies is executed.** A reply becomes a `SubPlan` or an `OrchDecision`, is cut to the existing text caps, and is checked before anything is stored.
- State and events hold ids, counts and short messages only: never file contents.
- Tests never call the real `gh`, a git remote, a Jira site, `osascript` or a harness CLI.
- `go test -race` is run on `internal/mission`, `internal/gitx`, `internal/source`; never on `./cmd/crew` (an older test there has a known data race).
- The repo is public: examples use `acme/app`. No real name, site or account appears in code, tests or docs.
- Files stay under 400 lines and functions under 50 lines. To keep a file under the limit, move code into a new file of the same package; put new tests in new test files.
- Every commit message uses the conventional `type: description` form and ends with the `Co-Authored-By` line naming the model that wrote the commit, and the `Claude-Session` line. The controller passes both to each implementer.

## Review Focus

1. **A piece is kept apart from a delegated piece, and then that piece is planned.** The outside piece still waits for the whole delegated piece, and no sub-piece is made to wait for the outside piece: nothing waits in a circle. → Task 3 `TestSeparateOverlaps_OutsiderIsOrderedThroughTheParent`, `TestSeparateOverlaps_NeverMakesACircleWithSubPieces`.
2. **The scoped orchestrator replies with pieces it has no authority for:** another domain, a factory of another domain, paths outside its parent's, a wait on a piece outside, more pieces than the mission has left, its own delegation past the depth limit. Nothing is added, the reason is kept for the next call, and the principal is asked once the calls are used. → Task 4 `TestApplySubPlan_RefusesWhatReachesOutside`, Task 5 `TestDecide_PlanningStopsAtTheCallLimit`.
3. **The planning call fails, times out, or returns no plan, every time.** Each try counts one call; the piece ends in one `plan` wait, not in a call every tick for ever. A stop of the mission during the call counts nothing. → Task 6 `TestEngine_FailedPlanningCallsEndInOneWait`, `TestEngine_StopDuringPlanningCountsNothing`.
4. **Sub-pieces are cancelled.** One cancelled and the rest complete: the delegated piece is complete. All cancelled: it is cancelled, and a piece waiting for it is asked about. The mission can always finish. → Task 5 `TestDecide_DelegatedPieceFinishesFromItsSubPieces`.
5. **A stuck sub-piece climbs the tree.** The scoped orchestrator is asked first; its escalation goes to the main orchestrator in the same tick; the main orchestrator's escalation reaches the principal with the main orchestrator's own question. The climb stops at the call limit, and an engine that restarts in the middle asks the one whose turn it is. → Task 8 `TestEngine_SubPieceEscalatesUpTheTree`, `TestApplyScopedDecision_ClimbStopsAtTheCallLimit`, `TestEngine_RestartMidClimbAsksTheSameLevel`.
6. **Ids with a dot arrive from outside crew:** in a new plan, an amendment, or an orchestrator's `add`. They are refused. A cancelled sub-piece's number is not given to a later one. → Task 2 `TestCheckNew_RefusesSubPieceIDs`, `TestAmend_CannotAddASubPiece`, Task 8 `TestApplyScopedDecision_AddNamesTheNextNumber`.

---

## Phase A: the tree in the plan

### Task 1: The tree read from ids, and whether one pattern lies inside another

**Files:** Create `internal/mission/tree.go`, `internal/mission/tree_test.go`, `internal/mission/paths_within.go`, `internal/mission/paths_within_test.go`.

**Interfaces (Produces):**
```go
// ParentID is the id of the piece a sub-piece belongs to: "P3" for "P3.2",
// "P3.2" for "P3.2.1", "" for a top-level piece.
func ParentID(id string) string
// Level is 1 for a top-level piece and one more for each "." in the id.
func Level(id string) int
// Under reports whether id is a sub-piece of ancestor, at any depth. A piece is not under itself.
func Under(id, ancestor string) bool
// Ancestors lists the ids above id, nearest first: ["P3.2", "P3"] for "P3.2.1".
func Ancestors(id string) []string
// Children lists the pieces of m whose parent is id, in file order.
func (m Mission) Children(id string) []Piece
// NextChildID is parent + "." + n, where n is one more than the highest number
// any piece of s has directly under parent, cancelled pieces included; "<parent>.1" when none.
func NextChildID(s State, parent string) string

// PatternWithin reports whether every file child can match is also matched by
// parent. When it can't tell, it says no. A malformed or empty pattern is within nothing.
func PatternWithin(child, parent string) bool
// PathsWithin returns the first pattern of child that no single pattern of
// parent holds whole; ok is true when there is none. An empty parent holds everything.
func PathsWithin(child, parent []string) (outside string, ok bool)
```

**`PatternWithin`** works on the segment lists `patternSegments` already builds (a pattern with no wildcard gets a trailing `**`). Compare from the front, memoized as `segmentsOverlap` is:
- both lists ended: yes;
- parent's segment is `**`: yes when the rest of parent holds child as it stands, or child has a segment left and parent as it stands holds the rest of child;
- otherwise, child ended or parent ended: no;
- child's segment is `**` (parent's is not): no;
- otherwise the two segments must agree, and the rests must: parent's segment is `*`; or child's segment has no wildcard and no backslash and `path.Match(parent's, child's)` is true; or the two segments are the same text. Anything else: no.

- [ ] **Step 1: Write the failing tests**
  - `TestParentLevelUnderAncestors`: the examples in the comments above, plus `Under("P3", "P3")` false, `Under("P30.1", "P3")` false, `Ancestors("P1")` empty.
  - `TestMissionChildren`: `P3`, `P3.1`, `P3.1.1`, `P3.2`, `P4` → `Children("P3")` is `P3.1`, `P3.2`.
  - `TestNextChildID`: none → `P3.1`; state holds `P3.1`, `P3.2` (cancelled) and `P3.2.1` → `P3.3`.
  - `TestPatternWithin`, a table: yes for (`brand/logos/**` in `brand/**`), (`brand` in `brand/**`), (`brand/**` in `brand`), (`brand/*.svg` in `brand/**`), (`brand/*.svg` in `brand/*`), (`brand/**/x` in `brand/**`), (`a/b.md` in `a/*.md`), (`x` in `**`); no for (`site/**` in `brand/**`), (`**` in `brand/**`), (`brand/**` in `brand/**/x`), (`a/b` in `a/*`), (`brand/a*.svg` in `brand/*.svg`: can't tell), (`brand/[` in `brand/**`: malformed), (`` in `brand/**`).
  - `TestPathsWithin`: `[brand/a/**, brand/b/**]` in `[brand/**]` ok; `[brand/a/**, site/**]` in `[brand/**]` → `site/**`; anything in an empty parent ok.
- [ ] **Step 2: Run** `go test ./internal/mission/ -run 'Parent|Children|NextChild|Within'` — Expected: FAIL (undefined).
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/mission/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: the piece tree is read from ids, and a pattern can be checked to lie inside another`

### Task 2: The checks that make authority narrow

**Files:** Modify `internal/mission/check.go`, `internal/mission/circles.go`, `internal/mission/amend_diff.go`, `internal/mission/decision.go` (`addProblem`), `internal/mission/check_test.go` (the test that expects "delegation isn't available yet"), `cmd/crew/mission.go` (`missionPlan`, `newChecks`). Create `internal/mission/check_tree.go`, `internal/mission/check_tree_test.go`, `internal/mission/amend_tree_test.go`.

**Interfaces.** Consumes Task 1. Produces:
```go
CheckInput.OrchestratorHarness string // the harness headless orchestrator calls run on
// CheckNew is Check for a plan that starts a mission: it also refuses every id with a ".".
func CheckNew(m Mission, in CheckInput) []error
```

**Ids.** An id is a top part as today, then any number of `.<number>` parts (`[1-9][0-9]*`). The reserved id rule applies to the whole id.

**A delegated piece** (`delegate: true`, no factory) is no longer refused. It needs a brief and a known domain, as every piece does, and:
- `Allows(in.Limits.Depth, Level(p.ID))`, else `piece P3: delegates at depth 1, and limits.delegation_depth is 0`;
- the harness `in.OrchestratorHarness` can apply deny rules, else an `ErrNoDeny` error: `piece P3: the orchestrator's harness "<name>" can't apply deny rules, so nothing can be delegated`.

**A sub-piece** (`ParentID(p.ID) != ""`), checked in this order, each with the text shown:
- its parent is in the plan: `piece P3.1: its parent P3 is not in the plan (to drop P3, remove the pieces under it too)`;
- its parent is delegated: `piece P3.1: its parent P3 is not delegated`;
- same domain: `piece P3.1: domain "product" is not its parent's domain "design"`;
- paths, only when the parent has effective paths: the sub-piece has effective paths, else `piece P3.1: has no paths, and its parent P3 has; a sub-piece keeps inside its parent's paths`; and `PathsWithin(its effective paths, the parent's)`, else `piece P3.1: paths: "site/**" is outside the paths of its parent P3`;
- each `waits_for` entry is `Under` its parent: `piece P3.1: waits_for "P1", which is not under P3; a sub-piece waits only for pieces under its own parent`.

The factory rule (a factory of the piece's domain) already covers sub-pieces.

**Circles.** `waitCircles` follows, besides each piece's own `waits_for`: from a sub-piece, every `waits_for` entry of each of its ancestors; and from a delegated piece, each of its children (it finishes after them). So `P3 waits_for X` with `X waits_for P3.1` is a circle.

**Where a dotted id may not come from.**
- `crew mission plan` calls `CheckNew`: `piece P3.1: an id with a "." names a sub-piece, and crew names those when it plans a delegated piece`.
- An amendment that adds one (`changesFor`, the add case): `piece P3.3 can't be added by an amendment: crew names sub-pieces when it plans a delegated piece; add a top-level piece instead`.
- The orchestrator's `add` (`addProblem`): `the new piece's id can't hold a "."`.

`newChecks` sets `OrchestratorHarness` from `settings.Harness`.

- [ ] **Step 1: Write the failing tests**
  - `TestCheck_DelegatedPieceIsAccepted` (depth 2, a harness that can deny); `TestCheck_DelegationPastTheDepthLimit` (depth 0 with `P3`; depth 1 with a delegated `P3.1`; `"*"` allows both); `TestCheck_DelegationNeedsADenyingOrchestratorHarness` (`errors.Is(err, ErrNoDeny)`).
  - `TestCheck_SubPieceRules`: one case per rule above, asserting the exact text; and one plan that passes them all, including a sub-piece with no paths under a parent that uses its domain's paths.
  - `TestCheck_SubPieceIDForm`: `P3.0`, `P3.x`, `P3.`, `.1` are bad ids; `P3.1.2` is good.
  - `TestCheck_CircleThroughADelegatedPiece`.
  - `TestCheck_PiecesLimitCountsSubPieces`.
  - `TestCheckNew_RefusesSubPieceIDs`; `TestAmend_CannotAddASubPiece`; `TestAmend_RemovingADelegatedPieceNeedsItsSubPiecesGone` (remove `P3` only → the "not in the plan" text; remove `P3`, `P3.1`, `P3.2` → three cancellations); `TestApplyDecision_AddRefusesADottedID`.
  - `TestMissionPlan_RefusesSubPieceIDs` (cmd).
- [ ] **Step 2: Run** `go test ./internal/mission/ ./cmd/crew/` — Expected: FAIL.
- [ ] **Step 3: Implement.** Replace the old "delegation isn't available yet" assertion.
- [ ] **Step 4: Run** `go test ./internal/mission/ ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: a plan may delegate a piece, and sub-pieces are checked to stay inside it`

### Task 3: Keeping pieces apart knows the tree

**Files:** Modify `internal/mission/separate.go`. Create `internal/mission/separate_tree.go`, `internal/mission/separate_tree_test.go`.

**Interfaces.** `SeparateOverlaps`, `SeparatedWithLog` and `StartedPieces` keep their signatures.

**Three rules, added to `SeparateOverlaps`:**
1. A piece is never compared with a piece above or below it.
2. For two pieces in different branches, take the two pieces where their branches part: `P3` and `X` for `P3.1.2` and `X`; `P3.1` and `P3.2` for `P3.1.2` and `P3.2`. When at least one of those two is not the piece itself, and both of those two have effective paths, the pair is not compared: the two pieces where the branches part were compared, and a sub-piece's paths lie inside its parent's.
3. Every other pair is compared as today, and `reaches` knows the tree: a link to a delegated piece is a link to every piece under it, and a piece has the links of its ancestors as well as its own.

Rule 2 is what keeps a piece that is kept apart from `P3` from also being tangled with `P3.1`. Rule 3 covers a delegated piece that has no paths, whose sub-pieces must then be compared with outsiders directly.

- [ ] **Step 1: Write the failing tests**
  - `TestSeparateOverlaps_NeverComparesAPieceWithItsParent`: `P3 [brand/**]` delegated, `P3.1 [brand/a/**]` → no entry either way.
  - `TestSeparateOverlaps_OutsiderIsOrderedThroughTheParent`: `P3 [brand/**]`, `X [brand/logo/**]` (after `P3` in the file), then `P3.1 [brand/logo/**]`, `P3.2 [brand/logo/**]` appended → `X.Apart == [P3]`, `P3.1.Apart` empty, `P3.2.Apart == [P3.1]`. The same with `X` before `P3` in the file and `P3` settled → `X.Apart == [P3]`, nothing on the sub-pieces naming `X`.
  - `TestSeparateOverlaps_SubPiecesOfTwoParents`: `P3 [a/**]`, `P4 [a/**]` both delegated, `P3.1 [a/x/**]`, `P4.1 [a/x/**]` → only `P4.Apart == [P3]`.
  - `TestSeparateOverlaps_ParentWithoutPaths`: `P3` delegated with no effective paths, `P3.1 [a/**]`, `X [a/**]` → one entry between `X` and `P3.1`; with `X waits_for P3` → none.
  - `TestSeparateOverlaps_NeverMakesACircleWithSubPieces`: a seeded random generator of trees (up to three levels, some parents without paths), `waits_for` links that pass `Check`, and settled sets; 500 rounds; the graph of `waits_for`, `apart_from` and delegated-piece-to-child edges, with ancestors' links inherited, never holds a circle. Keep the existing `TestSeparateOverlaps_NeverMakesACircle` passing unchanged.
- [ ] **Step 2: Run** `go test ./internal/mission/ -run SeparateOverlaps` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race -count=3 ./internal/mission/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: pieces are kept apart with the piece tree in mind`

## Phase B: planning a delegated piece

### Task 4: Turning a scoped orchestrator's reply into sub-pieces

**Files:** Modify `internal/mission/state.go` (one status, one wait kind). Create `internal/mission/subplan.go`, `internal/mission/subplan_apply.go`, `internal/mission/subplan_test.go`, `internal/mission/subplan_apply_test.go`.

**Interfaces.** Consumes Tasks 1 to 3. Produces:
```go
// PieceDelegated: the piece has been planned; the pieces under it carry its work.
const PieceDelegated PieceStatus = "delegated"
// WaitPlan: a delegated piece that could not be planned, or whose orchestrator asked a question first.
const WaitPlan = "plan"

const failurePlan = "plan"         // Decision.Failure for a planning try
func scopedBy(parent string) string // "scoped orchestrator P3": the By of events, decisions and changes

// SubPlan is the scoped orchestrator's reply for a delegated piece.
type SubPlan struct {
	Pieces   []Piece `json:"pieces"`
	Question string  `json:"question"` // instead of pieces: what it must know before it can plan
	Reason   string  `json:"reason"`
}
// ParseSubPlan decodes the last fenced block tagged `plan`: one JSON object, no unknown fields, nothing after it.
func ParseSubPlan(reply string) (SubPlan, error)
// ApplySubPlan adds the sub-pieces to the mission and marks parent delegated,
// or records why the reply can't be used. m and s are never modified.
func ApplySubPlan(m Mission, s State, parent string, sp SubPlan, in CheckInput, now time.Time) (Mission, State, []Action)
// PlanRefused records a planning try that produced nothing usable: a decision
// on parent {By: scopedBy(parent), Failure: "plan", Action: "invalid", Reason: why}
// and a `decision` event "plan refused: <why>". parent stays queued.
func PlanRefused(s State, parent, why string, now time.Time) (State, []Action)
```

**`ApplySubPlan`,** in order. `Question`, `Reason` and every refusal text are cut to `maxReasonRunes` first.
1. parent is not a piece of `m` with `delegate: true` whose status in `s` is `queued`: return both unchanged, no actions.
2. `Question` set and no pieces: a wait on parent of kind `WaitPlan` with the question, back to `queued`; a decision `{Failure: "plan", Action: "escalate", Note: question, Reason: reason}`; an `escalated` event by `scopedBy(parent)`; a notification with the wait's text.
3. No pieces, or pieces and a question together: `PlanRefused` with `the plan lists no pieces` / `the plan holds both pieces and a question`.
4. Name the pieces. The reply's ids must be non-empty and distinct (`two pieces of the plan share the id "a"`). The k-th piece gets parent's id plus `.` plus the next free number, counting up from `NextChildID`. Each `waits_for` entry must be an id of the reply and is rewritten to the new id; any other entry refuses the plan: `"P1" is not a piece of this plan; a sub-piece waits only for pieces under P3. Ask a question instead if P3 needs another piece first`.
5. Fill in: an empty domain becomes parent's; a piece with no paths gets a copy of parent's effective paths; `apart_from` is dropped.
6. Append the pieces to a copy of `m` with the change `{By: scopedBy(parent), What: "planned P3 as P3.1, P3.2", Why: reason}` and run `Check`. Any error refuses the plan with `firstMessages(errs)`.
7. `SeparatedWithLog`, with parent added to `StartedPieces(s)`.
8. State: parent becomes `delegated` with no wait; one queued `PieceState` per new piece, after the existing ones.
9. Actions: a `decision` event on parent by `scopedBy(parent)` (`planned as P3.1, P3.2: <reason>`), a `piece-added` event per new piece (`planned under P3`), and the notification `P3 was planned as 2 pieces: P3.1, P3.2`.

- [ ] **Step 1: Write the failing tests**
  - `TestParseSubPlan`: a good block; the last of two `plan` blocks wins; no block, an unknown field, two objects → errors; a `decision` block is not a plan.
  - `TestApplySubPlan_AddsNamedPieces`: reply ids `a`, `b` with `b` waiting for `a`, under `P3 [brand/**]` in `design` → pieces `P3.1`, `P3.2` (`waits_for [P3.1]`), both `design`, both with `brand/**` filled in where none was given; parent `delegated`; the change-log entry; the events and the notification; `m` and `s` untouched.
  - `TestApplySubPlan_KeepsCountingAfterEarlierNumbers`: state already holds a cancelled `P3.1` → new pieces are `P3.2`, `P3.3`.
  - `TestApplySubPlan_RefusesWhatReachesOutside`, a table, each leaving the mission unchanged, parent queued, and one `invalid` decision whose reason holds the text: a piece in another domain; a factory of another domain; `paths: [site/**]` under `brand/**`; `waits_for: [P1]`; thirteen pieces against a limit of 12; a delegated sub-piece at depth 2 against a limit of 1; a duplicate reply id; no pieces; pieces and a question.
  - `TestApplySubPlan_QuestionOpensAPlanWait`.
  - `TestApplySubPlan_OverlappingSubPiecesAreKeptApart`: two sub-pieces on the same paths → the second is `apart_from` the first and a `crew` change-log entry says so.
  - `TestApplySubPlan_NestedDelegate`: depth 2, the reply holds `{delegate: true}` → `P3.1` is queued and delegated in the plan.
  - `TestPlanRefused`.
- [ ] **Step 2: Run** `go test ./internal/mission/ -run 'SubPlan|PlanRefused'` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/mission/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: a scoped orchestrator's plan becomes sub-pieces, or a recorded refusal`

### Task 5: The state machine plans a delegated piece and finishes it

**Files:** Modify `internal/mission/machine.go` (the `Action` list, `Decide`), `internal/mission/machine_queued.go`, `internal/mission/machine_budget.go` (`applyBudget`), `internal/mission/answer.go`, `internal/mission/amend.go` (`unpark`), `internal/mission/repo_claims.go` (`holdsPaths`), `internal/mission/tracker_plan.go` (`Lane`). Create `internal/mission/machine_delegate.go`, `internal/mission/machine_delegate_test.go`, `internal/mission/answer_plan_test.go`.

**Interfaces.** Consumes Task 4 (`PieceDelegated`, `WaitPlan`, `failurePlan`). Produces:
```go
// PlanPieces asks the scoped orchestrator to split a delegated piece.
type PlanPieces struct{ Piece string }
```

**`queued`.** For a delegated piece, after the `waits_for` and `apart_from` checks and instead of everything that follows them: clear `Behind`; when ready, plan it. A delegated piece is never held behind another mission and never waits for a run slot.

**Planning** (`d.plan(id)`): when `Allows(lim.OrchestratorCalls, Calls+1)`, add `PlanPieces{id}`. Otherwise record the call budget as `callBudgetHit` does and open a `WaitPlan` wait back to `queued`: `P3 could not be planned in <Calls> orchestrator calls: <the reason of its last "plan" decision>`.

**Finishing** (`d.delegated()`, run after `deliverables()` and before the queued pieces, deepest level first): for each `delegated` piece whose direct children in the state are all `complete` or `cancelled`: `complete` it when at least one is complete; otherwise mark it `cancelled` with a `piece-cancelled` event by the engine, `every piece under it was cancelled`. A delegated piece with no children in the state is left alone.

**Hold.** `applyBudget` drops `PlanPieces` with the other actions that take on new work.

**Answers.** A `WaitPlan` wait takes `retry` or `cancel`. `retry` puts the piece back to `queued` with `Calls` set to 0 and returns no run step; `cancel` cancels it. An amendment that re-briefs or edits a parked piece (`unpark`) also sets its `Calls` to 0.

**Elsewhere.** `Lane(delegated)` is `in-progress`. `holdsPaths` is false for a `delegated` piece. `HoldingRuns` and `hasStarted` need no change: write the tests that pin it.

- [ ] **Step 1: Write the failing tests**
  - `TestDecide_ReadyDelegatedPieceIsPlanned`: `P1` (delegate) and `P2` → `PlanPieces{P1}` and `StartPiece{P2}`; with `runs_at_once` 1 and a piece running → still `PlanPieces{P1}`; with `P1 waits_for P2` and `P2` running → nothing for `P1`.
  - `TestDecide_PlanningStopsAtTheCallLimit`: `Calls` at the limit, last plan decision `"paths: … is outside"` → no `PlanPieces`, one `WaitPlan` wait holding that reason, a `budget-hit` event, one notification; the next `Decide` adds nothing more.
  - `TestDecide_DelegatedPieceFinishesFromItsSubPieces`: children complete+complete → complete; complete+cancelled → complete; cancelled+cancelled → cancelled, and a piece that waits for it gets the "was cancelled" question; complete+running → unchanged; `P3.1` delegated with all its children complete and `P3` waiting only on it → both complete in one `Decide`; a mission whose last open piece is a delegated one finishes.
  - `TestDecide_DelegatedPieceIgnoresOtherMissions`: `Obs.Behind` names it, `ElsewhereUnknown` set → still planned, `Behind` nil.
  - `TestDecide_HoldDropsPlanning`.
  - `TestHoldingRunsAndClaims_SkipDelegatedPieces`; `TestLane_Delegated`; `TestStartedPieces_DelegatedIsSettled`.
  - `TestApplyAnswer_PlanWait`: `retry` → queued, `Calls` 0, no step; `cancel` → cancelled; `goto` → the choices error naming `retry, cancel`. `TestAmend_RebriefOfAParkedDelegatedPieceResetsItsCalls`.
- [ ] **Step 2: Run** `go test ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement.** The engine's `perform` gets a `PlanPieces` case that does nothing yet (Task 6 fills it).
- [ ] **Step 4: Run** `go test -race ./internal/mission/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: a ready delegated piece is planned, and finishes when the pieces under it do`

### Task 6: The planning call

**Files:** Modify `internal/mission/ops.go` (`Orchestrator`), `internal/mission/orchestrate.go`, `internal/mission/effects.go` (`perform`), `internal/mission/fakes_test.go` (`fakeOrch`), every other `Orchestrator` stand-in in the test files (`cmd/crew/mission_e2e_parallel_test.go` has one). Create `internal/mission/plan_request.go`, `internal/mission/plan_request_test.go`, `internal/mission/effects_plan.go`, `internal/mission/engine_plan_test.go`, `internal/mission/orchestrate_plan_test.go`.

**Interfaces.** Consumes Tasks 4 and 5. Produces:
```go
// PlanRequest is everything the scoped orchestrator is told to plan a delegated piece.
type PlanRequest struct {
	MissionID, Request string
	Changes            []string   // what the principal said in each amendment
	Piece              Piece      // the delegated piece
	Above              []Piece    // the delegated pieces above it, nearest first
	Paths              []string   // its effective paths; nil when it has none
	Briefing           string     // the domain's body
	Factories          []FactoryInfo
	Others             []string   // one line per other piece of the mission: "P1 (product): <first line of its brief>"
	PiecesLeft         string     // how many more pieces the mission may hold, or "*"
	MayDelegate        bool       // a sub-piece may itself be delegated
	Earlier            []Decision // the piece's earlier "plan" decisions
}
type FactoryInfo struct {
	Name, Description string
	Stations          []string
}
func BuildPlanRequest(id string, m Mission, ps PieceState, d source.Domain, factories []source.Factory, lim Limits) PlanRequest
func (r PlanRequest) Prompt() string

type Orchestrator interface {
	Decide(ctx context.Context, repoRoot string, s Summary) (OrchDecision, error)
	Plan(ctx context.Context, repoRoot string, r PlanRequest) (SubPlan, error)
}
func (o HeadlessOrchestrator) Plan(ctx context.Context, repoRoot string, r PlanRequest) (SubPlan, error)
```

**`BuildPlanRequest`**: `Factories` are the domain's factories that exist, in the domain's order; `PiecesLeft` is `left(lim.Pieces, len(m.Pieces))`; `MayDelegate` is `Allows(lim.Depth, Level(piece)+1)`.

**`Prompt`** has the three parts `Summary.Prompt` has: the task, the data between marker lines from `markersFor`, and the reply rule again. The task part, word for word, with `<id>` and `<domain>` filled in:

```
You are the orchestrator for the <domain> domain of a crew mission. A mission is a job split into pieces; each piece is one factory run in its own git worktree and ends as one pull request, or as files handed over as a deliverable. Piece <id> has been handed to you whole. Split it into the pieces that do its work. You may read the repository to decide; you change nothing.

Rules crew checks. A plan that breaks one is refused, and the reasons for earlier refusals are in the data:
- every piece uses one of the factories listed in the data;
- every piece keeps to the paths listed in the data: give each the narrowest paths that cover its work, or leave paths out to use them all;
- a piece waits only for other pieces of your plan: name them in waits_for by the ids you give them;
- the plan holds no more pieces than the number left in the data.
Pieces whose paths don't overlap run at the same time. A piece that must build on another's result waits for it.
If <id> can't be planned until something outside it is done or decided, don't guess: reply with a question for the principal instead of pieces.

Reply with exactly one fenced block tagged `plan` holding one JSON object:
{"pieces": [{"id": "<id>.1", "factory": "...", "brief": "...", "paths": ["..."], "waits_for": []}], "reason": "one line on why this split"}
or {"question": "...", "reason": "..."}.
```
When `MayDelegate`, one more rule line: `- a piece that is itself several pieces may be {"id": "...", "delegate": true, "brief": "...", "paths": ["..."]} with no factory; it is planned later, the same way.`

The data part, with `section` as the summary uses it: `Mission`; `Request (word for word)`; the principal's changes when there are any; `Piece <id>` (brief, paths, waits for); `Above <id>` when `Above` is not empty (each id and brief); `Paths its pieces must keep to` (`(any)` when nil); `Domain briefing`; `Factories` (one line each: name, description, stations); `The mission's other pieces`; `Pieces left`; `Earlier tries at this plan` (each refusal reason).

**`HeadlessOrchestrator.Plan`** runs exactly as `Decide` does (same agent, tools, deny rules and timeout) and returns `ParseSubPlan(reply.Text)`.

**The engine** (`planPieces`):
1. the piece is not a queued delegated piece: nothing;
2. build the request and call `Plan`. No orchestrator configured is an error like any other;
3. the context ended during the call: return, counting nothing;
4. count the call with `Asked`. The `decision` event that step 5 produces is the record of the call;
5. an error from the call: `PlanRefused` with the error's text. Otherwise `ApplySubPlan`;
6. adopt, save, perform the actions.

`fakeOrch` gains `plans []SubPlan`, `planErr error` and `planReqs []PlanRequest`, and logs `Orch.Plan <id>`.

- [ ] **Step 1: Write the failing tests**
  - `TestBuildPlanRequest`: fields as above; a factory the domain lists but that doesn't exist is left out; `Earlier` holds only `plan` decisions.
  - `TestPlanRequestPrompt`: holds the request, the brief, the briefing, each factory name, the paths and the pieces left; with `MayDelegate` false the delegate line is absent; a brief holding the begin marker gets numbered markers; an earlier refusal's reason is in the data.
  - `TestHeadlessOrchestrator_Plan`: a fake runner returns a `plan` block → the `SubPlan`; the request has read and search only and the station deny rules; a harness that can't deny is refused.
  - `TestEngine_PlansADelegatedPiece`: one tick → `P1` delegated, `P1.1` and `P1.2` in `mission.md` and the state, `Calls` 1; the next tick starts `P1.1`.
  - `TestEngine_RefusedPlanIsTriedAgainWithItsReason`: the first reply reaches outside the paths, the second is good → the second request's `Earlier` holds the first refusal; `Calls` 2.
  - `TestEngine_FailedPlanningCallsEndInOneWait`: `planErr` set, limit 3 → three calls over three ticks, then a `plan` wait, and no fourth call over five more ticks.
  - `TestEngine_StopDuringPlanningCountsNothing`: the fake cancels the context during `Plan` → `Calls` 0, no event, piece queued.
  - `TestEngine_PlanQuestionWaitsOnThePrincipal`, then `retry` through the inbox → planned on the next tick.
- [ ] **Step 2: Run** `go test ./internal/mission/ ./cmd/crew/` — Expected: FAIL to compile, then FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race ./internal/mission/ && go test ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: the engine asks a scoped orchestrator to plan a delegated piece`

### Task 7: What a sub-piece is told, and what it is handed

**Files:** Modify `internal/mission/ops.go` (`PieceInput`), `internal/mission/effects_deliverable.go` (`prepareScratch`), `internal/mission/tracker_text.go` (`IssueText`), `internal/mission/tree.go`. Create `internal/mission/tree_inputs_test.go`, `internal/mission/ops_tree_test.go`.

**Interfaces.** Produces:
```go
// InputsFor lists the pieces whose deliverables piece id receives: the pieces
// it waits for and the pieces each of its ancestors waits for, where a
// delegated piece stands for every piece under it that is not delegated. In
// file order, each once.
func InputsFor(m Mission, id string) []string
```

- `PieceInput` adds, after "Your piece" and for each ancestor nearest first: `## The piece yours is part of: <id>` and that piece's brief.
- `prepareScratch` copies the deliverables of `InputsFor(t.m, p.ID)` instead of `p.WaitsFor`.
- `IssueText`: a delegated piece's body says `Factory: none (delegated: its pieces are planned when it is ready)`; a sub-piece's body gains the line `Part of: <parent>` after the first.

- [ ] **Step 1: Write the failing tests**
  - `TestInputsFor`: `P4 waits_for P3` (delegated, with `P3.1`, `P3.2` delegated with `P3.2.1`) → `P3.1`, `P3.2.1`; `P3.2.1` where `P3 waits_for P1` → `P1`, plus its own waits.
  - `TestPieceInput_NamesWhatItIsPartOf`; `TestPrepareScratch_HandsOverWhatADelegatedPieceProduced` (a waiter on `P3` receives `P3.1`'s files in a folder named `P3.1`); `TestIssueText_DelegatedAndSubPiece`; `TestTrackerOps_CreatesAnIssueForEachSubPiece` (a state with `P3` delegated and `P3.1`, `P3.2` without issues → two `create` ops, and `P3` moves to `in-progress`).
- [ ] **Step 2: Run** `go test ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/mission/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: a sub-piece knows what it is part of and receives what its parent waited for`

## Phase C: when a sub-piece is stuck

### Task 8: The nearest orchestrator is asked first, then the one above

**Files:** Modify `internal/mission/state.go` (`PieceState.Up`), `internal/mission/decision.go`, `internal/mission/decision_pr.go` only if `allowedFor` needs it, `internal/mission/summary.go`, `internal/mission/summary_prompt.go`, `internal/mission/answer.go`, `internal/mission/effects.go` (`askOrchestrator`). Create `internal/mission/decision_scope.go`, `internal/mission/decision_scope_test.go`, `internal/mission/summary_scope_test.go`, `internal/mission/engine_scope_test.go`.

**Interfaces.** Produces:
```go
PieceState.Up int `json:"up,omitempty"` // how many orchestrators above the nearest the piece's current stop has been raised

// Scope is the delegated piece a scoped orchestrator answers for.
type Scope struct {
	Parent   Piece
	Above    []Piece  // the delegated pieces above Parent, nearest first
	Paths    []string // Parent's effective paths
	Briefing string   // the domain's body
}
Summary.Scope *Scope // nil for the main orchestrator
// Scoped returns s for the orchestrator scoped to the delegated piece parent.
func (s Summary) Scoped(m Mission, parent string, d source.Domain) Summary

// AskerOf is the delegated piece whose orchestrator is asked about ps next:
// Ancestors(ps.ID)[ps.Up], or "" (the main orchestrator) when Up is past them.
func AskerOf(ps PieceState) string
// ApplyScopedDecision is ApplyDecision for a decision made by the orchestrator
// scoped to the delegated piece scope. With scope "" it is ApplyDecision by the orchestrator.
func ApplyScopedDecision(m Mission, s State, piece, failure string, d OrchDecision, scope string, in CheckInput, now time.Time) (Mission, State, []Action, *RunStep)
```
`ApplyDecision` keeps its signature and behavior for the main orchestrator.

**What changes for a scoped orchestrator (`scope != ""`):**
- Its decisions, events and change-log entries are by `scopedBy(scope)`.
- **`escalate`, and a decision that can't be used:** no wait and no notification. The decision is logged as today, an `escalated` event carries the question (or the `invalidPrefix` text), `Up` goes up by one, and the piece is put to the next orchestrator with the decider's `ask`: an `AskOrchestrator` action when a call is left, the usual wait on the principal when not. The applier's decider gets `lim = in.Limits` so `ask` can count.
- **`rebrief`:** the target must be `Under(target, scope)`, else `rebrief target "P1" is outside P3`.
- **`add`:** crew names the piece `NextChildID(s, scope)`, whatever id the decision gives; an empty domain becomes the scope's, and no paths become the scope's effective paths. The existing quote, additions and `Check` rules apply, so the new piece must pass every sub-piece rule. Its `piece-added` event says `added by the scoped orchestrator P3`.

**For every orchestrator:**
- `escalate` is never a repeat: `repeats` skips it. (Today a second escalation of the same stop is refused and the principal sees the refusal text; with a tree the question must be able to pass through each level.)
- `Up` goes back to 0 when a decision other than `escalate` is carried out, and when the principal answers a wait on the piece.

**The prompt.** With a `Scope`, `taskIntro` is replaced by: `You are the orchestrator for piece <parent> (the <domain> domain) of a crew mission. <parent> was handed to you whole and split into pieces; each is one factory run in its own git worktree. One of them is stuck, and you decide what happens next.` The meaning of `escalate` becomes `pass it up, to the orchestrator above you and then the principal; put the question in the note.`, of `rebrief` `rewrite the brief of a piece under <parent> that hasn't started.`, of `add` `add a piece under <parent>, quoting the words of the request it serves; crew names it.` The data gains, after `Piece`: `Your scope: <parent>` (its brief and paths), `Above <parent>` when there is any, and `Domain briefing`.

**The engine.** `askOrchestrator` works out `AskerOf(ps)`; with a scope it scopes the summary and applies with `ApplyScopedDecision`. The actions it then performs may hold another `AskOrchestrator` for the same piece: that is the climb, and it ends at the main orchestrator or at the call limit.

- [ ] **Step 1: Write the failing tests**
  - `TestAskerOf`: `P3.1.2` with `Up` 0, 1, 2, 3 → `P3.1`, `P3`, ``, ``; a top-level piece → ``.
  - `TestApplyScopedDecision_GotoIsCarriedOut`: by `scoped orchestrator P3`, `Up` 0 afterwards.
  - `TestApplyScopedDecision_EscalateRaisesInsteadOfWaiting`: no wait, no notification, `Up` 1, an `escalated` event by the scoped orchestrator, an `AskOrchestrator` action.
  - `TestApplyScopedDecision_UnusableDecisionIsRaised`.
  - `TestApplyScopedDecision_ClimbStopsAtTheCallLimit`: `Calls` at the limit when the scoped orchestrator escalates → a `limit` wait on the principal, no `AskOrchestrator`.
  - `TestApplyScopedDecision_RebriefStaysInTheSubtree`; `TestApplyScopedDecision_AddNamesTheNextNumber` (decision id `X9`, state holds `P3.1` and a cancelled `P3.2` → `P3.3`, domain and paths filled in, `Additions` 1); `TestApplyScopedDecision_AddOutsideTheParentsPathsIsRaised`.
  - `TestApplyDecision_EscalateTwiceIsNotARepeat`; update any existing test that pins the old refusal.
  - `TestApplyAnswer_ResetsUp`.
  - `TestSummaryPrompt_Scoped`: holds the scope's brief and paths, the briefing and "pass it up"; the unscoped prompt is byte for byte what it was.
  - `TestEngine_SubPieceEscalatesUpTheTree`: the fake orchestrator's decisions are escalate, escalate → two `Orch.Decide` calls in one tick, the first summary scoped to `P1`, the second not; a `question` wait holding the second note; `Calls` 2.
  - `TestEngine_RestartMidClimbAsksTheSameLevel`: state saved with `Up` 1 on a failed sub-piece → the next tick's call is unscoped.
  - `TestEngine_NestedSubPieceAsksItsOwnParentFirst` (`P1.1.1` → scoped to `P1.1`).
- [ ] **Step 2: Run** `go test ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race ./internal/mission/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: a stuck sub-piece goes to its scoped orchestrator, then up the tree`

## Phase D: what the principal sees

### Task 9: The plan table, status, the orchestrator's instructions, the guide, end to end

**Files:** Modify `cmd/crew/mission_paths.go` (`printPieceTable`), `internal/mission/diagram.go`, `cmd/crew/missionstatus.go` (or a new `cmd/crew/missionstatus_tree.go`), `cmd/crew/missionstatus_ready.go`, `internal/mission/orchestrator.go` (`orchestratorBody`), `internal/mission/orchestrator_test.go`, `internal/scaffold/template/missions.yaml` (two comments), `docs/missions.md`, `README.md` only if it says delegation is missing. Create `cmd/crew/missionstatus_tree_test.go`, `cmd/crew/mission_e2e_delegate_test.go`, `internal/mission/diagram_tree_test.go`.

**`crew mission plan`, and `start` when it waits for go**
- The `FACTORY` column shows `(delegated)` for a delegated piece.
- After the table, one line per delegated piece: `P3 is delegated: the design orchestrator splits it once it is ready. Its pieces stay in design, use design's factories and keep to brand/**.` With no effective paths the last sentence ends `and are not held to any paths.`
- The diagram labels a delegated piece `P3 · design · delegated` and draws a dotted arrow from a delegated piece to each piece directly under it (`P3 -.-> node5`).

**`crew mission status <id>`**
- A new section **Delegated**, after Running: one line per `delegated` piece, `P3  3 pieces: 1 complete, 1 running, 1 queued`, counting the pieces directly under it by status and leaving out a count of zero.
- **Queued:** a delegated piece that nothing keeps back shows `to be planned` (or `on hold` during a hold), never the run-slot text.
- **Waiting on you:** a `plan` wait prints like the others, with its kind.

**`orchestratorBody`** gains, in its existing plain style:
- when part of a job is itself several pieces of one domain, and how to split it is better decided once earlier work has landed, plan it as one piece with `delegate: true` and no factory; give it a brief that says what the whole part must achieve, and the paths it may touch;
- crew then asks that domain's orchestrator to split it when it is ready. The sub-pieces are named `P3.1`, `P3.2`, …, stay in that domain, its factories and the piece's paths, and appear in `crew mission status`; a piece that waits for `P3` starts when everything under `P3` is finished;
- never write an id with a `.` in a plan or add one in an amendment; an amendment may remove or re-brief a sub-piece that hasn't started, and to drop a delegated piece it removes that piece and every piece under it;
- a `plan` wait means a delegated piece could not be planned or its orchestrator asked a question first: it takes `retry` or `cancel`, and changing the piece with `crew mission amend` is the other way out: a new brief, or a `waits_for` naming the piece it needs first;
- the example plan gains a third piece with `delegate: true`.

**`missions.yaml` template:** `delegation_depth: 2` gets the comment `# how many levels a piece may be handed down: 0 switches delegation off`; `pieces: 12` gets `# sub-pieces included`.

**`docs/missions.md`**
- "What this stage does and doesn't do": delegation exists; what is still missing is releases and the cutover.
- New section **"Delegation"**, after "Running pieces together": what `delegate: true` is and when to use it; when the piece is planned (once it is ready) and by whom; the four rules a sub-plan must keep and what happens to one that doesn't; how sub-pieces are named; that they run, merge and are accepted like any piece, so the domain's auto-merge switch covers them; when the delegated piece is complete or cancelled; who is asked when a sub-piece is stuck, in order; the `plan` wait and its ways out; what an amendment can and can't do to sub-pieces; that a delegated piece holds no run slot and no paths against other missions; `delegation_depth` counted in hops, with one example of depth 2.
- "Mission settings" and the limits table in "Budgets": `delegation_depth` is enforced; `pieces` counts sub-pieces; planning calls count toward `orchestrator_calls_per_piece`.
- "Status": the Delegated section and `to be planned`. "What 'waiting on you' means" and the commands table: the `plan` wait.
- "When a station fails": the scoped orchestrator, then up the tree.
- "Jira" ("What shows on the board"): a sub-piece is an issue under the epic named by its id; a delegated piece's issue is In Progress while pieces under it are open.

**End to end** (Go stand-ins for the orchestrator, the fake harness, the fake `gh`):
- `TestMission_EndToEnd_DelegatedPiece`: `P1` delegated in `design` with `paths: [brand/**]`, `P2 waits_for P1`. The stand-in plans two pieces, the second waiting for the first. → after the first tick `P1` is `delegated` and `P1.1` is running; `crew mission status` shows the Delegated section; `P1.2` starts after `P1.1` is merged; `P1` is complete once both are; `P2` starts after that; the mission finishes. `mission.md` holds the sub-pieces and the scoped orchestrator's change-log entry.
- `TestMission_EndToEnd_SubPlanOutsideItsPathsIsRefused`: the stand-in always plans a piece on `site/**` → after the call limit, status shows a `plan` wait with the reason; the stand-in is swapped for a good one and `crew mission answer … --do retry` → `P1` is planned.
- `TestMission_EndToEnd_StuckSubPieceReachesThePrincipal`: the sub-piece's run fails; the stand-in escalates every call → status shows one `question` wait on `P1.1`, and the events hold an `escalated` by `scoped orchestrator P1` before one by `orchestrator`.
- `TestMissionResume_WithADelegatedPiece`: stop the mission while `P1` is `delegated` and `P1.1` is running; `crew mission resume` passes its checks and the sub-piece's run is adopted.

- [ ] **Step 1: Write the failing tests:** the four end-to-end tests; `TestMissionPlan_PrintsDelegatedPieces`; `TestMermaid_DelegatedPieceAndItsSubPieces`; `TestMissionStatus_DelegatedSection`; `TestOrchestratorBody_MentionsDelegation` (contains `delegate: true`, `P3.1`, `plan` wait, `retry`).
- [ ] **Step 2: Run** `go test ./cmd/crew/ ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement,** then write the guide changes.
- [ ] **Step 4: Run** `go build ./... && go vet ./... && go test -count=1 ./... && go test -race -count=1 ./internal/mission/ ./internal/gitx/ ./internal/source/` — Expected: PASS. `TestDocs_ExamplePlansParse` still passes.
- [ ] **Step 5: Commit** `feat: the plan, status and the guide cover delegation`
