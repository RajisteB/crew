# Missions Smoke-Run Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix six things the first live smoke run of missions showed: an approval stop that refuses `goto`, titles cut mid-word, handling instructions leaking into the request, a template reviewer that can't run tests, a plain-run approval that leaves no event, and merged piece branches left behind locally.

**Architecture:** Six independent, small changes. Each one is a pure-function or prompt change with its own test, plus the matching sentence in `docs/missions.md`. No new package, no new config key, no new dependency.

**Tech Stack:** Go (the module's current version and dependencies only), the standard `testing` package, the existing fakes in `internal/mission/fakes_test.go`.

**Spec:** `docs/superpowers/specs/2026-11-18-orchestrator-missions-design.md` (the design) and `docs/missions.md` (the user guide, which this plan updates where behavior changes).

## Global Constraints

- Test first: write the failing test, watch it fail for the right reason, then implement. Run `go test ./...` and `go vet ./...` from the repo root before each commit; both must pass.
- No new dependency. No new config key. No change to `missions.yaml`, `harnesses.yaml` or any state file's format, except where a task says so.
- Never mutate an input: functions return new values, as the code around them does.
- Functions stay under 50 lines and files under 800 lines; follow the patterns of the file you are in.
- The repository is public. Committed files, tests and docs use only made-up names: `acme/app`, `ACME`, `https://example.atlassian.net`. No real person, account, site or email.
- Nothing is ever deleted on GitHub by crew. No task may add a remote delete, a pull request close or any new write to GitHub.
- Docs and messages are plain words, short sentences, no em dashes in new prose, straight quotes.
- One commit per task, conventional format (`fix:`, `feat:`, `docs:`, `test:`), ending with two trailer lines: a `Co-Authored-By` line that names the model that wrote the commit (the one your session's attribution reminder gives), then the session line. For a Sonnet implementer:

  ```
  Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01L1o92TnCABZyZodEGzW9mc
  ```

- Do not push, and do not open a pull request.

## Review Focus

- An approval `goto` with no `--station`, or a station that isn't in the piece's factory, must be refused with the existing station error and change nothing (Task 1).
- A title made of one very long word, or of multi-byte characters, must still come out within the limit and never split a rune (Task 2).
- A run approved by an older build has no approval event and an empty reason; `crew run status` must still print it without an error (Task 5).
- Deleting a local branch must never touch a branch that doesn't start with `crew/`, and a branch that is already gone is not an error (Task 6).
- A failed branch delete must not stop the tick, block the piece's completion or be retried forever (Task 6).

---

### Task 1: An approval stop accepts `goto`

Today `crew mission answer <id> <wait> --do goto --station build` on an `approval` wait is refused (`the choices are: approve, cancel`), while `--do approve --station build` does exactly that send-back. Every other wait that can be sent back uses `goto`. Make `goto` a choice on an approval wait, meaning the same as `approve` with a station.

**Files:**
- Modify: `internal/mission/answer.go` (`answerChoices`, `answerStep`)
- Modify: `internal/mission/orchestrator.go` (one sentence of `orchestratorBody`)
- Modify: `docs/missions.md` (the `approval` row of the waits table, near line 557)
- Test: the existing answer tests in `internal/mission` (find the file that tests `ApplyAnswer` on an approval wait), and `internal/mission/orchestrator_test.go`

**Interfaces:**
- Consumes: `ApplyAnswer(m Mission, s State, a Answer, in CheckInput, now time.Time) (Mission, State, []Action, *RunStep, error)`, `RunStep{Piece, Kind, Station, Note}`.
- Produces: nothing new. `answerChoices` returns `[]string{answerApprove, answerGoto, answerCancel}` for `WaitApproval`.

- [ ] **Step 1: Write the failing tests**

  - `TestApplyAnswer_ApprovalGotoSendsBackThroughApprove`: a piece waiting on a `WaitApproval`; answer `Answer{Wait: <id>, Do: "goto", Station: "build", Note: "fix the typo", Words: "send it back"}` with a `CheckInput` whose factory has a `build` station. Assert: no error; the returned step is `&RunStep{Piece: <piece>, Kind: "approve", Station: "build", Note: "fix the typo"}`; the piece's wait is cleared; its last decision has `Action == "goto"` and `Station == "build"`.
  - `TestApplyAnswer_ApprovalGotoNeedsAStation`: the same wait answered with `Do: "goto"` and `Station: ""`. Assert an error containing `is not in the factory of piece`, and that the returned state equals the input state.
  - Update the existing test (if any) that asserts the refusal text for an approval wait: the list is now `approve, goto, cancel`. An approval wait answered with `retry` is still refused with `the choices are: approve, goto, cancel`.
  - In `orchestrator_test.go`: assert `OrchestratorBody()` contains `An approval takes approve, goto or cancel`.

- [ ] **Step 2: Run them and see them fail** (`go test ./internal/mission/ -run 'ApprovalGoto|Orchestrator'`): the first fails with `can't be answered with "goto"`.

- [ ] **Step 3: Implement**

  - `answerChoices`: `WaitApproval` returns `[]string{answerApprove, answerGoto, answerCancel}`.
  - `answerStep`: before the general `switch a.Do`, when `w.Kind == WaitApproval && a.Do == answerGoto`, return `&RunStep{Piece: id, Kind: "approve", Station: a.Station, Note: a.Note}`. (The run is paused at its approval stop; `run.Approve` with a station is the one transition that leaves that state for a named station. The general `goto` step is for a run that is not awaiting approval.)
  - `orchestratorBody`: replace `An approval takes approve or cancel.` with `An approval takes approve, goto or cancel: goto sends the work back to the station named instead of letting it continue.`
  - `docs/missions.md`, the `approval` row: the choices cell becomes `` `approve`, `goto` (with `--station`) or `cancel`. `goto` sends the work back to a station, with your `--note` for that station. `approve --station` does the same thing. ``

- [ ] **Step 4: Run `go test ./...` and `go vet ./...`**; fix any golden or prompt test that pinned the old sentence by updating its expected text.

- [ ] **Step 5: Commit** `fix: let an approval stop in a mission be answered with goto`

---

### Task 2: Titles are cut at a word, with an ellipsis

A pull request title is the first line of the brief cut to 70 runes, and a Jira summary is cut to 120, both mid-word (`...to the greet pac`).

**Files:**
- Modify: `internal/mission/ops.go` (`PRText`), `internal/mission/tracker_text.go` (the two summaries that call `cut(...)` with `maxSummaryRunes`)
- Create or modify: put `cutWords` next to `cutRunes` in `internal/mission/answer.go`, or in `tracker_text.go` next to `cut`; pick the one that keeps both files coherent.
- Test: `internal/mission/ops_test.go` (or the file that tests `PRText`), `internal/mission/tracker_text_test.go` if it exists

**Interfaces:**
- Produces: `func cutWords(s string, n int) string` — returns `s` unchanged when it has at most `n` runes. Otherwise it takes the first `n-1` runes, cuts back to the last space when that space is at rune index `n/2` or later, trims trailing spaces and any of `,;:` from the end, and appends `…`. The result never has more than `n` runes.

- [ ] **Step 1: Write the failing test** `TestCutWords`, table-driven, exact results:

  | input | n | want |
  |---|---|---|
  | `"short"` | 70 | `"short"` |
  | `"alpha beta gamma"` | 12 | `"alpha beta…"` |
  | `"alpha beta, gamma"` | 13 | `"alpha beta…"` |
  | `"abcdefghijklmnop"` | 8 | `"abcdefg…"` |
  | `"ab cdefghijklmnopqrstuvwxyz"` | 12 | `"ab cdefghij…"` |
  | `"ééééé ééééé ééééé"` | 12 | `"ééééé ééééé…"` |
  | `"Add an exported function Farewell(name string) string to the greet package and a test for it."` | 70 | `"Add an exported function Farewell(name string) string to the greet…"` |

  Also assert for every row that `utf8.RuneCountInString(got) <= n` and `utf8.ValidString(got)`.

  Add `TestPRText_TitleEndsAtAWord`: a piece whose brief's first line is the long sentence above gives the title in the last row. Add or extend a tracker text test: a piece summary longer than 120 runes ends with `…` and has at most 120 runes.

- [ ] **Step 2: Run and see them fail** (`cutWords` undefined).

- [ ] **Step 3: Implement `cutWords`** and use it for the title in `PRText` and for both summaries in `tracker_text.go` (the mission summary and the piece summary). Leave `cutRunes` and `cut` for their other callers.

- [ ] **Step 4: Run `go test ./...` and `go vet ./...`**; update any test that pinned a mid-word title.

- [ ] **Step 5: Commit** `fix: cut pull request titles and Jira summaries at a word`

---

### Task 3: Handling instructions stay out of the request

The orchestrator records the principal's message word for word as the request, so a sentence such as "plan it as a mission but don't start it yet" ends up quoted in every pull request and in every station's input. The request stays word for word, but sentences that are only about handling the mission are left out.

**Files:**
- Modify: `internal/mission/orchestrator.go` (`orchestratorBody`, item 2)
- Modify: `docs/missions.md` (the paragraph near line 213 that starts "A plan is a Markdown file.")
- Test: `internal/mission/orchestrator_test.go`

- [ ] **Step 1: Write the failing test**: `OrchestratorBody()` contains `Leave out, whole, any sentence that only tells you how to handle the mission` and still contains `word for word`.

- [ ] **Step 2: Run and see it fail.**

- [ ] **Step 3: Implement.** In item 2 of `orchestratorBody`, directly after the sentence ending `because later checks compare new work against that text.`, add:

  > The request is what the principal wants done. Leave out, whole, any sentence that only tells you how to handle the mission (when to start it, what to show them first, not to start it yet): stations and pull requests quote the request, and those sentences are not part of the job. Change nothing in the sentences you keep.

  In `docs/missions.md`, after "The body is your request, word for word, because crew compares later changes against it.", add: "The orchestrator leaves out sentences that are only about handling the mission, such as \"don't start it yet\", because every pull request quotes the request."

- [ ] **Step 4: Run `go test ./...` and `go vet ./...`**; update any golden file of the compiled orchestrator agent.

- [ ] **Step 5: Commit** `fix: keep handling instructions out of a mission's request`

---

### Task 4: The template factory's review station can run the tests

`ship-change` reviews with `code-explorer`, a read-and-search agent on the smallest model, so its review can't run a test. Give the template a reviewer agent with a shell.

**Files:**
- Create: `internal/scaffold/template/agents/change-reviewer.md`
- Modify: `internal/scaffold/template/factories/ship-change.md` (the `review` station's `agent`)
- Modify: `README.md` (the tree near line 15, the factory example near line 83 and its diagram near line 104)
- Test: the scaffold and verify tests that list template agents or name the review station's agent (`internal/verify/factories_test.go` line 51 expects `station review: agent code-explorer isn't compiled for claude-code`)

- [ ] **Step 1: Write the failing test.** In the scaffold package's tests (next to the test that checks the template loads), add `TestTemplate_ReviewStationCanRunCommands`: load the template project the way the neighboring tests do, take the `ship-change` factory's `review` station, look up its agent, and assert the agent's capabilities contain `shell` and contain neither `edit` nor `write`.

- [ ] **Step 2: Run and see it fail** (the agent is `code-explorer`, no `shell`).

- [ ] **Step 3: Implement.** Create `change-reviewer.md` with exactly:

  ```markdown
  ---
  name: change-reviewer
  description: Reviews one finished change against its plan, runs the project's tests and says whether it passes, needs a fix or needs a new plan. Use as a factory's review station.
  model: claude-sonnet-5-5
  effort: medium
  per_harness:
    opencode: {model: opencode-go/glm-5.3, effort: high}
  capabilities: [read, search, shell]
  ---
  Review the change against the plan it was built from. Read the diff, then run the project's tests and say which command you ran and what it printed.
  Change no files. When something is wrong, say what and where, and leave the fix to the builder.
  ```

  Point `ship-change`'s `review` station at `change-reviewer`. `code-explorer` stays in the template (other tests and users rely on it). Update `README.md`: add the file to the tree, and name `change-reviewer` in the factory example and its diagram.

- [ ] **Step 4: Run `go test ./...` and `go vet ./...`**; update tests that pinned the review station's agent name (for example the expected error in `internal/verify/factories_test.go`) and any count of template agents.

- [ ] **Step 5: Commit** `fix: give the template's review station an agent that can run tests`

---

### Task 5: A run's approval is recorded, and `done` says why

After `crew run approve <id> --note "..."`, `events.jsonl` has no trace of the approval or the note, and the last line prints `■ done: ` with nothing after the colon.

**Files:**
- Modify: `internal/run/events.go` (new kind), `internal/run/format.go` (its line), `internal/run/machine.go` and/or `internal/run/engine.go` (the reason of `done` after an approval)
- Modify: `cmd/crew/runctl.go` (`runApprove`), `internal/mission/runs.go` (`applyStep`, the `approve` case)
- Modify: `docs/missions.md` (the plain runs section, one sentence)
- Test: `internal/run` tests next to the ones for `Approve` and for event formatting; `cmd/crew` test next to the existing `crew run approve` tests

**Interfaces:**
- Produces: `const EvApproved = "approved"` and `func ApprovedEvent(p Pending, gotoStation, note string) Event` in `internal/run` (use the real name of the type of `State.Pending`). It returns `Event{Kind: EvApproved, Station: p.Station, Route: p.Route, Msg: msg}` where `msg` is, joined with `: ` when both are present, `sent to <gotoStation>` (only when `gotoStation != ""`) and the note cut to 500 runes on a rune boundary (only when the note is not empty). With neither, `Msg` is empty.
- The formatted line is `<time> ✔ approved <station>'s <route>` followed by `: <msg>` when `Msg` is not empty.

- [ ] **Step 1: Write the failing tests**

  - `TestApprovedEvent`: table of (goto, note) → Msg: (`""`, `""`) → `""`; (`"build"`, `""`) → `"sent to build"`; (`""`, `"ship it"`) → `"ship it"`; (`"build"`, `"fix the typo"`) → `"sent to build: fix the typo"`; a 600-rune note comes back as 500 runes.
  - A format test: the event for station `review`, route `pass`, Msg `ship it` formats to a line ending `✔ approved review's pass: ship it`; with an empty Msg it ends `✔ approved review's pass`.
  - `cmd/crew`: next to the existing approve test, approve a run that is awaiting approval on a route to `done` whose pending verdict summary is `looks good`, with `--note "ship it"`. Assert the run's events contain one `approved` event with `Msg == "ship it"`, and that the `done` event's `Msg` is `looks good`.
  - `internal/mission`: next to the existing `applyStep` tests, an `approve` step on a run awaiting approval emits one `approved` event in the run's events.

- [ ] **Step 2: Run and see them fail** (undefined names; then the empty `done` message).

- [ ] **Step 3: Implement.** Add the kind, the constructor and the format line. Emit the event in `runApprove` once the state is saved and before the run continues, and in `applyStep`'s `approve` case, in both places from the pending approval read before `run.Approve` cleared it. For the empty `done`: find where the `done` event is emitted after an approval that moves the run to done (the decision `run.Approve` returns is dropped by its callers, so the reason is lost) and make the approved verdict's summary reach that event; a state file with no reason must still print `done: ` without an error. In `docs/missions.md`, in the plain runs section, add: "An approval is recorded in the run's events with the note you gave."

- [ ] **Step 4: Run `go test ./...` and `go vet ./...`.**

- [ ] **Step 5: Commit** `fix: record a run's approval and give done its reason`

---

### Task 6: A merged piece's local branch is deleted

After a piece's pull request is merged crew removes the worktree, and the local branch `crew/<mission>/<piece>` stays forever. Delete the local branch too. The branch on GitHub is not touched: the guide promises crew deletes nothing there.

**Files:**
- Modify: `internal/gitx/gitx.go` (new method), `internal/mission/ops.go` (`GitOps`), `internal/mission/machine.go` (`Cleanup`), `internal/mission/machine_pr.go` (`ended`), `internal/mission/effects.go` (`cleanup`), `internal/mission/fakes_test.go`
- Modify: `docs/missions.md` (the paragraph near line 917)
- Test: `internal/gitx/gitx_test.go`, `internal/mission/effects_pr_test.go` or the file that tests `cleanup`, `internal/mission/machine_pr_test.go`

**Interfaces:**
- Produces: `func (r Repo) DeleteBranch(ctx context.Context, branch string) error` — validates with `checkRef("branch", branch)`, refuses any branch that doesn't start with `crew/` (error: `won't delete branch %q: not a crew branch`), runs `git branch -D <branch>` in the repo, and returns nil when the branch doesn't exist.
- `GitOps` gains `DeleteBranch(ctx context.Context, branch string) error`.
- `Cleanup` gains `Merged bool`: the piece's pull request was merged, so its branch can go.

- [ ] **Step 1: Write the failing tests**

  - `gitx`: in a temp repo with a branch `crew/m1/P1` that isn't checked out, `DeleteBranch` removes it; calling it again returns nil; `DeleteBranch(ctx, "main")` and `DeleteBranch(ctx, "feature/x")` return the `not a crew branch` error and delete nothing.
  - `machine_pr`: a piece with an open pull request that comes back `MERGED` produces `Cleanup{Piece: id, Force: true, Merged: true}`. The other `Cleanup` actions in the package (deliverable accepted, cancelled, finished with nothing to commit) keep `Merged == false`.
  - `effects`: with the fake git, a `Cleanup{Merged: true}` for a piece with a worktree and branch `crew/m1/P1` removes the worktree and then calls `DeleteBranch` with that branch; `Cleanup{Merged: false}` never calls it; when the worktree removal fails, `DeleteBranch` is not called; when `DeleteBranch` fails, the piece is still cleaned up and one note is recorded containing `can't delete the local branch crew/m1/P1`.

- [ ] **Step 2: Run and see them fail.**

- [ ] **Step 3: Implement.** In `ended`, the `MERGED` case adds `Cleanup{Piece: id, Force: true, Merged: true}`. In `tick.cleanup`, after the worktree is gone (git refuses to delete a branch a worktree has checked out) and when `a.Merged` and the piece's branch is not empty, call `DeleteBranch`; a failure is a note (`can't delete the local branch %s: %v`) and nothing else, and it is not retried. Add the method to the fake git with a call record and an error to return. In `docs/missions.md` replace the last sentence of the paragraph near line 917, "The local piece branches stay where they are.", with: "The local branch of a piece whose pull request was merged is deleted with its worktree. A cancelled piece's local branch stays. The branches on GitHub stay too; GitHub's \"Automatically delete head branches\" setting removes those if you want that."

- [ ] **Step 4: Run `go test ./...` and `go vet ./...`.**

- [ ] **Step 5: Commit** `fix: delete a merged piece's local branch with its worktree`
