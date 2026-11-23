# crew missions, stage 6: the cutover — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The `orchestrator` agent drives plain `crew run` runs the way the per-factory foremen did, and the `foreman-<factory>` agents are gone: `crew compile` no longer writes them and removes the ones it wrote before.

**Architecture:** Nothing new runs. The orchestrator's prompt gains one section on plain runs, and its session's allow list gains the read-and-relay `crew run` commands. Two small additions to `crew run` let an agent with no file tools and no `sleep` do the job: text from stdin, and a status call that waits for news. One guard keeps the new reach from touching missions: a run a mission started refuses `crew run say|goto|approve|resume`. Removing foremen is deleting code; the existing ownership record (`.crew/state.*.json`) already makes the next compile delete outputs that are no longer produced. The `foreman:` block of `harnesses.yaml` is renamed `orchestrator:`, the old spelling still loads, and `crew mission setup` rewrites it.

**Tech Stack:** Go 1.22 module (toolchain 1.24), `gopkg.in/yaml.v3` only, stdlib `testing`.

**Spec:** `docs/superpowers/specs/2026-11-18-orchestrator-missions-design.md`. Read "Non-goals" (Pilots), "What the safeguards cover", "The orchestrator", stage 6 of "Build order" and "Left to the builder" before any task. The factories design, `docs/superpowers/specs/2026-11-18-factories-design.md` section "A. Foreman", says what the foreman did. This plan covers **stage 6 of the Build order**, the last stage of Project 1.

**Eight places where this plan settles what the spec leaves open, or departs from its wording, on purpose:**

1. *"The orchestrator takes over the foremen's job of driving plain runs."* One agent can't carry every factory's briefing and diagram in its prompt, as each foreman did. It reads them on demand with `crew factory list` and `crew factory diagram <name>`.
2. *"It never … approves."* In the orchestrator's session, `crew run status`, `say` and `resume` are pre-approved. Starting a run, `crew run goto` and `crew run approve` are not: the coding tool asks the principal to confirm each one. A `goto` is on that list because a `goto` requested while a station runs can pass that station's approval stop.
3. *"So a station can't pass an approval stop or answer for the principal."* The same must hold for the main orchestrator and a mission's runs, now that it uses `crew run`. A run a mission started is marked as the mission's, and `crew run say|goto|approve|resume` refuse it. The mission engine drives runs in-process and is not affected.
4. *"`crew run` itself stays as it is."* Nothing it does today changes. Two things are added: `-` reads text from stdin, and `crew run status <id> --wait D` returns when there is news. They exist because the orchestrator has no file tools and its session does not pre-approve `sleep` or `mktemp`.
5. *A plain run in a repo where a mission is running.* crew does not check a plain run against a mission's pieces: a plain run declares no paths. The orchestrator looks at `crew mission status` first, tells the principal, and starts the run only if they say so. `crew run` itself does not refuse.
6. *"How the `foreman-*` links are removed during compile"* (left to the builder). The next `crew compile` deletes them, as it deletes any output no source produces any more. One that was edited by hand is not deleted: compile stops and names it, as it does today for any edited output.
7. *The `foreman:` block of `harnesses.yaml`.* The spec doesn't mention it. It is renamed `orchestrator:` with the keys `static`, `launch` and `allow`. A file with the old spelling still loads and behaves the same, `crew compile` says so in a note, and `crew mission setup` rewrites it.
8. *A harness that gets no orchestrator.* With foremen gone, such a harness has no agent to talk to at all, so `crew compile` says why in a note.

**Not in this plan:** releases (Project 2); golden missions; a smoke run with a real model; plain runs in worktrees; recording the principal's words for a plain run's approval; notifications for plain runs; refusing a plain run while a mission is live in the repo; any change to what a station may do.

## Global Constraints

- The only dependency is `gopkg.in/yaml.v3`. No new modules.
- Before writing Go, load the `use-modern-go` skill and follow it for Go 1.22.
- Config files decode strictly: unknown keys are errors. That holds under `orchestrator:` and under the old `foreman:`.
- State is immutable: every state-machine function takes a `State` and returns a new one. `internal/run`'s machine functions (`Approve`, `Goto`, `Resume`) are not changed by this plan.
- **The mission engine never goes through the `crew run` command line.** The guard of Task 1 lives in `cmd/crew`, not in `internal/run`, so `internal/mission` keeps working on its own runs.
- **An approval stop is passed only by `crew run approve` or by a `goto`.** `crew run resume` never passes one, and neither does `status` or `say`.
- **The orchestrator's session pre-approves exactly these `crew run` forms:** `crew run status`, `crew run say`, `crew run resume`. Never `Bash(crew run:*)`, `crew run approve` or `crew run goto`.
- **Stations and headless orchestrator calls keep their deny rules** (`Bash(crew run:*)`, `Bash(crew mission:*)`): this plan does not touch `deny:`.
- **The principal's words never go on a shell line unquoted.** The orchestrator passes them on stdin with a quoted heredoc.
- **`crew mission setup` keeps the owner's edits.** It removes one thing only: the `launch` line of an old `foreman:` block, which started foremen. It never overwrites an earlier backup.
- Tests never call the real `gh`, a git remote, a Jira site, `osascript` or a harness CLI.
- `go test -race` is run on `internal/mission`, `internal/gitx`, `internal/source`; never on `./cmd/crew` (an older test there has a known data race).
- The repo is public: examples use `acme/app`. No real name, site or account appears in code, tests or docs.
- Files stay under 400 lines and functions under 50 lines. To keep a file under the limit, move code into a new file of the same package; put new tests in new test files. `internal/build/build_test.go` (450 lines) and `cmd/crew/mission_test.go` (515 lines) are already over: don't add to them, and move a test you must change into a new file.
- Every commit message uses the conventional `type: description` form and ends with the `Co-Authored-By` line naming the model that wrote the commit, and the `Claude-Session` line. The controller passes both to each implementer.

## Review Focus

1. **The id of a mission piece's run is passed to `crew run approve`, `goto`, `say` or `resume`.** Each is refused with the owner named; the run's `state.json` and inbox are unchanged; the mission's own e2e tests with an approval stop still pass. → Task 1 `TestCLI_OwnedRunRefusesSayGotoApproveResume`.
2. **`crew run resume` on a run that is waiting for approval.** It ends at the same stop and runs no station. `resume` is pre-approved in the orchestrator's session and `approve` is not, so this is what keeps the principal's confirmation meaningful. → Task 3 `TestCLI_ResumeDoesNotPassAnApprovalStop`.
3. **The principal's words hold `$HOME`, backticks, quotes and several lines.** Through `--input -`, `say <id> -` and `--note -` they reach the station byte for byte. An empty stdin for `--input -` is a usage error, not an empty run. → Task 2 `TestCLI_StdinTextArrivesVerbatim`, `TestCLI_EmptyStdinInputIsRefused`.
4. **`crew run status --wait` on a run that will never produce news.** A stopped run returns at once; a run marked `running` whose process is gone returns when the time is up; neither hangs, and the time has an upper bound. → Task 2 `TestCLI_StatusWaitReturnsWhenTheRunHasStopped`, `TestCLI_StatusWaitGivesUpAfterTheTime`, `TestCLI_StatusWaitUsageErrors`.
5. **A `harnesses.yaml` written before missions, or before this stage.** Every command still loads it and builds the same orchestrator; `crew mission setup` turns it into the current form, keeps rules the owner added, does nothing on a second run, and does not overwrite the backup an earlier setup left. → Task 5 `TestDecodeRegistry_ReadsTheOldForemanKey`, `TestOrchestratorOutputs_SameFromOldAndNewKey`, `TestUpgradeHarnesses_FileFromBeforeTheCutoverBecomesTheTemplate`, `TestUpgradeHarnesses_KeepsAnAllowRuleTheOwnerAdded`, `TestUpgradeHarnesses_SecondBackupGetsANumber`.
6. **A machine that compiled foremen before.** The next compile removes the links and the built files; one edited by hand is left, and compile says which. → Task 4 `TestCompile_RemovesWhatAnOlderCompileWroteForAForeman`, `TestCompile_AForemanFileEditedByHandIsNotDeleted`.

---

## Phase A: `crew run`, ready for the orchestrator

### Task 1: A mission's runs are the mission's

**Files:** Modify `internal/run/state.go`, `internal/mission/runs.go`, `cmd/crew/missionrun.go`, `cmd/crew/run.go`, `cmd/crew/runctl.go`, `docs/missions.md`. Create `cmd/crew/run_owned.go`, `cmd/crew/run_owned_test.go`, `internal/mission/runs_owner_test.go`.

**Interfaces (Produces):**
```go
// internal/run/state.go: State gains
Owner string `json:"owner,omitempty"` // who drives this run instead of `crew run`: "mission <id> piece <id>"; "" for a plain run

// internal/mission/runs.go
// RunOwner is the Owner of a run started for a piece: "mission <missionID> piece <pieceID>".
func RunOwner(missionID, pieceID string) string
// LocalRuns gains the field
MissionID string

// cmd/crew/run_owned.go
// refuseOwned returns an error when the run has an owner; verb is "say", "goto", "approve" or "resume".
func refuseOwned(store run.Store, verb string) error
```

**Rules:**
- `LocalRuns.Start` sets `Owner` on the state `run.NewRun` returns and saves it before returning the run id. With `MissionID` empty, `Start` returns the error `mission runs need a mission id` and creates no run. `cmd/crew/missionrun.go` sets `MissionID` where it builds `LocalRuns`.
- `dispatchRun` calls `refuseOwned` for `say`, `goto`, `approve` and `resume` before anything is read from stdin or written. The error text is exactly: `run <id> belongs to <owner>: crew run <verb> is refused for it; use crew mission status, answer, amend, stop or resume`. Exit code 1.
- `crew run status <id>` prints a line `owner: <owner>` after the first line when the run has one. The list form (`crew run status`) ends an owned run's line with two spaces and the owner.
- `crew run status` is never refused.
- `docs/missions.md`, section "Where things live": one paragraph saying a piece's run can be read with `crew run status` and that `crew run say|goto|approve|resume` refuse it, with the message.

**Tests:**
- `internal/mission/runs_owner_test.go`: `TestLocalRuns_StartMarksTheRunAsTheMissions` (after `Start` for piece `P1` of mission `m-1`, the run's saved state has `Owner == "mission m-1 piece P1"`); `TestLocalRuns_StartNeedsAMissionID` (empty `MissionID`: the error, and `.crew/runs` holds no run).
- `cmd/crew/run_owned_test.go`: `TestCLI_OwnedRunRefusesSayGotoApproveResume` (drive a plain run of the approval fixture to its stop, save its state with `Owner: "mission m-1 piece P1"`, then for each of `say <id> hi`, `goto <id> <a station>`, `approve <id>`, `resume <id>`: exit 1, stderr holds `belongs to mission m-1 piece P1` and `crew run <verb> is refused`, `state.json` is byte-for-byte what it was, and the inbox holds nothing); `TestCLI_StatusShowsTheOwner` (the `owner:` line for one run, and the owner at the end of its line in the list); `TestCLI_PlainRunHasNoOwnerLine`.

- [ ] **Step 1:** Write the tests above. Update the `LocalRuns{…}` literal in `internal/mission/runs_test.go` to set `MissionID: "m-1"`.
- [ ] **Step 2:** Run `go test ./internal/mission/ -run 'TestLocalRuns_Start' && go test ./cmd/crew/ -run 'TestCLI_Owned|TestCLI_StatusShowsTheOwner|TestCLI_PlainRunHasNoOwnerLine'`. Expected: the new tests fail to compile (`Owner`, `MissionID` undefined).
- [ ] **Step 3:** Implement the interfaces and rules.
- [ ] **Step 4:** Run the same commands. Expected: PASS.
- [ ] **Step 5:** Run `go build ./... && go vet ./... && go test -count=1 ./...` and `go test -race -count=1 ./internal/mission/`. Expected: all pass; the mission e2e tests with an approval stop pass unchanged.
- [ ] **Step 6:** Commit: `feat: a mission's runs refuse crew run say, goto, approve and resume`.

### Task 2: `crew run` takes text from stdin and can wait for news

**Files:** Modify `cmd/crew/run.go`, `cmd/crew/runctl.go`, `cmd/crew/main.go`, `cmd/crew/run_test.go` (only the helper that calls `runCmd`), `README.md` (the "Running" block). Create `cmd/crew/run_wait.go`, `cmd/crew/run_stdin_test.go`, `cmd/crew/run_wait_test.go`.

**Interfaces (Produces):**
```go
func runCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int
// runFlags gains: stdin io.Reader; wait time.Duration (flag --wait)
func textArg(v string, stdin io.Reader) (string, error)   // "@file", "-" (all of stdin), or the text itself
func readInput(in string, stdin io.Reader) (string, error)

// cmd/crew/run_wait.go
var statusPoll = 500 * time.Millisecond   // tests lower it
const maxStatusWait = 10 * time.Minute
// waitForNews returns when the run has an event after since, or it has stopped
// and no process drives it, or d has passed, whichever is first.
func waitForNews(store run.Store, since int, d time.Duration) error
```

**Rules:**
- `--input -`, `crew run say <id> -` (the message is exactly `-`) and `--note -` read stdin to its end and use the bytes as they are, as `@file` does.
- `--input -` with empty or all-whitespace stdin is the usage error `--input is required (text, @file, or - for stdin)`, exit 2. The same text replaces today's `--input is required (text, or @file)`.
- `--wait D` is a Go duration. It is accepted only with `crew run status <id>`: without an id the usage error is `--wait needs a run id`. A value below zero or above `maxStatusWait` is the usage error `--wait takes a time up to 10m, like 90s`. Zero, the default, means no waiting.
- "Stopped and no process drives it" is: the saved status is not `running`, and `store.LockedBy()` reports no holder. A run saved as `running` with no holder is waited on until the time is up: its process may be starting.
- After the wait, `status` prints exactly what it prints today.
- `runUsage` shows the new forms: `--input TEXT|@FILE|-`, `say <id> <message…|@FILE|->`, `--note TEXT|@FILE|-`, `status [<id>] [--since N] [--wait D]`.
- `README.md` "Running" block: add one line each for `crew run status <id> --since 0 --wait 90s` and `crew run say <id> - < note.txt`, and one sentence under the block saying `-` reads stdin wherever text or `@file` is taken.

**Tests:**
- `cmd/crew/run_stdin_test.go`: `TestCLI_StdinTextArrivesVerbatim` (table over `--input -`, `say <id> -`, `approve <id> --note -`; the text is ``use `port` $HOME "8081"\nand 'this'\n``; for `--input` assert `input.md` equals it, for the other two assert the next station's `prompt` file contains it); `TestCLI_EmptyStdinInputIsRefused` (exit 2, the message, no run folder created); `TestCLI_DashInsideAMessageIsJustText` (`say <id> - really` queues the text `- really` and reads no stdin).
- `cmd/crew/run_wait_test.go` (each sets `statusPoll` to 20ms and restores it): `TestCLI_StatusWaitReturnsAtOnceWhenThereIsNews` (a finished run, `--since 0 --wait 30s`: returns in under 5s and prints its events); `TestCLI_StatusWaitReturnsWhenTheRunHasStopped` (the same run with `--since <its last cursor> --wait 30s`: under 5s, and the last line is `cursor <the same number>`); `TestCLI_StatusWaitGivesUpAfterTheTime` (save the run's state as `running`, nothing holds its lock, `--since <last> --wait 150ms`: takes at least 150ms and under 5s); `TestCLI_StatusWaitSeesAnEventThatArrivesLater` (the same `running` state; a goroutine emits one note event through the store after 100ms; `--wait 10s` returns in under 5s and prints that note); `TestCLI_StatusWaitUsageErrors` (no id; `--wait 11m`; `--wait -1s`: exit 2 with the messages above).

- [ ] **Step 1:** Write the tests. Change the test helper in `run_test.go` that calls `runCmd` so it passes a stdin reader (empty by default).
- [ ] **Step 2:** Run `go test ./cmd/crew/ -run 'TestCLI_Stdin|TestCLI_EmptyStdin|TestCLI_Dash|TestCLI_StatusWait'`. Expected: FAIL to compile (`runCmd` takes three arguments, `statusPoll` undefined).
- [ ] **Step 3:** Implement. `main.go` passes `os.Stdin`.
- [ ] **Step 4:** Run the same command. Expected: PASS.
- [ ] **Step 5:** Run `go build ./... && go vet ./... && go test -count=1 ./...`. Expected: all pass.
- [ ] **Step 6:** Commit: `feat: crew run reads text from stdin, and status can wait for news`.

---

## Phase B: the cutover

### Task 3: The orchestrator drives plain runs

**Files:** Modify `internal/mission/orchestrator.go`, `internal/scaffold/template/harnesses.yaml` (the `orchestrator_allow` line of `claude-code`), `docs/missions.md`. Create `internal/mission/orchestrator_runs.go`, `internal/mission/orchestrator_runs_test.go`, `internal/build/orchestrator_allow_test.go`, `cmd/crew/run_resume_approval_test.go`.

**Interfaces (Produces):**
```go
// internal/mission/orchestrator_runs.go
// orchestratorRuns is item 7 of the orchestrator's prompt: plain runs.
const orchestratorRuns = `…`
```
`orchestratorBody` becomes: everything it holds today up to and including the closing fence of the example plan, a blank line, `orchestratorRuns`, a blank line, then today's last paragraph (`If a message asks you to decide … run no commands.`) unchanged and still last.

**The text of `orchestratorRuns`,** word for word (in Go it is a raw string joined with backtick pieces, as the rest of the prompt is):

````text
7. A plain run is one factory run on its own, outside any mission: no worktree, no branch and no pull request. The stations work in the project directory itself. Use one when the principal asks to run a factory, when the factory belongs to no domain, or when the project has no GitHub remote. Otherwise plan a mission.

   At the start of a session, after `crew mission status`, run `crew run status` and mention any run with no owner that is `awaiting-approval`, `failed` or `interrupted`.

   Before you start a plain run, look at `crew mission status`. If a mission in this repo has pieces that have started and are not finished, tell the principal that a plain run works in the main checkout and that crew does not check it against those pieces, and go on only if they say so.

   `crew factory list` names the factories and `crew factory diagram <name>` draws one; show the principal the diagram if they haven't seen it. Start the run with what it should produce, in the principal's words, word for word, on stdin. Use a quoted heredoc so the shell expands nothing, and a delimiter that doesn't appear as a line in their words:

   ```sh
   crew run <factory> --detach --input - <<'CREW_TEXT'
   <what the run should produce>
   CREW_TEXT
   ```

   Starting a run, `crew run goto` and `crew run approve` are not pre-approved in this session: the principal is asked to confirm each one. That is intended. Never look for another way to run them.

   Note the run id it prints. Watch with `crew run status <id> --since <cursor> --wait 90s`: it returns as soon as there is news, prints the new events and ends with `cursor N`. Pass that N the next time; start at 0. Tell the principal, in one line each: notes, drift redirects, retries, a route close to its limit, a failure, and the result. Skip routine step lines unless they ask. Stop watching when status says `done`, `failed`, `interrupted` or `awaiting-approval`, or when the principal wants to talk about something else.

   What the principal says for the work goes to the next station: `crew run say <id> - <<'CREW_TEXT'`, with their words on stdin in the same way. To send the work to a station they name, run `crew run goto <id> <station>`.

   At an approval stop, status prints `waiting for approval` with the path of the station's output. Read it, tell the principal what the station produced and which route it chose, and ask. Run `crew run approve <id> --detach` only after the principal has said, in this conversation, to approve that stop. `--goto <station>` sends the work elsewhere instead, and `--note -` passes their words to the next station, on stdin.

   A failed run that status marks `retryable:` continues with `crew run resume <id> --detach` once the cause is fixed. So does a run that is `interrupted`, or one that says `running` but shows no `(live`: its process has ended. For any other failure, offer `crew run goto <id> <station>` followed by resume, or a new run.

   A run that status shows with `owner: mission …` belongs to that mission: `crew run say`, `goto`, `approve` and `resume` refuse it. Use the `crew mission` commands for it.
````

**Rules:**
- The template's `claude-code` allow list becomes exactly: `["Bash(crew mission:*)", "Bash(crew domain list:*)", "Bash(crew factory list:*)", "Read", "Grep", "Glob", "Bash(crew run status:*)", "Bash(crew run say:*)", "Bash(crew run resume:*)", "Bash(crew factory diagram:*)"]`. The four new rules come last, in that order, so that an older file upgraded by Task 5 ends up equal to the template.
- `docs/missions.md`:
  - a new section `## Plain runs`, placed before `## Where things live`: what a plain run is and when the orchestrator uses one instead of a mission; that crew does not check a plain run against a mission's pieces and the orchestrator asks first; that starting, `goto` and `approve` each need the principal's confirmation in the session and why `goto` is among them; `--wait` and `-`; that a mission's runs refuse the four commands.
  - under "What setup adds to `claude-code`": the allow list as above, and the sentence after it rewritten: what is pre-approved, and that starting a run, `crew run goto` and `crew run approve` make the session ask first.
  - "The words used here", the Orchestrator line: it also starts and watches plain runs.

**Tests:**
- `internal/mission/orchestrator_runs_test.go`, each on `OrchestratorBody()`:
  - `TestOrchestratorBody_PlainRunStartsDetachedFromStdin`: contains `crew run <factory> --detach --input - <<'CREW_TEXT'`.
  - `TestOrchestratorBody_PlainRunIsWatchedWithWait`: contains `crew run status <id> --since <cursor> --wait 90s`.
  - `TestOrchestratorBody_PlainRunApprovalNeedsThePrincipal`: contains `crew run approve <id> --detach` and `only after the principal has said, in this conversation, to approve that stop`.
  - `TestOrchestratorBody_PlainRunLooksForAMissionFirst`: contains `does not check it against those pieces`.
  - `TestOrchestratorBody_LeavesAMissionsRunsAlone`: contains `owner: mission`.
  - `TestOrchestratorBody_PlainRunsNeedNoSleepOrFiles`: `orchestratorRuns` contains none of `sleep`, `mktemp`, `@"$f"`.
  - `TestOrchestratorBody_HeadlessRuleIsStillLast`: the body ends with `reply only as it says and run no commands.\n`.
- `internal/build/orchestrator_allow_test.go`: `TestOrchestratorSettings_AllowListForPlainRuns` (compile the scaffold template project; the `allow` array of `orchestrator.settings.json` equals the list above, in order); `TestOrchestratorSettings_NeverPreApprovesStartGotoOrApprove` (no allow rule equals `Bash(crew run:*)` or starts with `Bash(crew run approve` or `Bash(crew run goto`).
- `cmd/crew/run_resume_approval_test.go`: `TestCLI_ResumeDoesNotPassAnApprovalStop` (drive the approval fixture to its stop; `resume <id>` exits 0; the state is still `awaiting-approval` with the same step number; no new step folder exists). This pins what `run.Resume` does today: it is expected to pass before any change. If it fails, stop and report; do not change the test.

- [ ] **Step 1:** Write the tests.
- [ ] **Step 2:** Run `go test ./internal/mission/ -run TestOrchestratorBody_ && go test ./internal/build/ -run TestOrchestratorSettings_ && go test ./cmd/crew/ -run TestCLI_ResumeDoesNotPassAnApprovalStop`. Expected: the body and allow-list tests FAIL; the resume test passes.
- [ ] **Step 3:** Add `orchestratorRuns`, splice it into `orchestratorBody`, change the template line, write the guide text.
- [ ] **Step 4:** Run the same commands. Expected: PASS.
- [ ] **Step 5:** Run `go build ./... && go vet ./... && go test -count=1 ./...`. Expected: all pass (tests that compare the upgraded old template with the embedded template still pass: the allow list is copied from the template).
- [ ] **Step 6:** Commit: `feat: the orchestrator starts and watches plain runs`.

### Task 4: Foremen are removed

**Files:** Delete `internal/factory/foreman.go`, `internal/factory/foreman_test.go`, `internal/build/foreman.go`. Modify `internal/build/build.go`, `internal/build/orchestrator.go`, `internal/build/build_test.go` (remove the two foreman tests only), `internal/build/orchestrator_test.go` (remove `TestCompile_StillEmitsForemen`), `internal/verify/factories.go`, `internal/verify/factories_test.go`, `cmd/crew/main.go`, `cmd/crew/factory_test.go`, `README.md`, and the comments that name the foreman in `cmd/crew/runctl.go`, `internal/run/engine.go`, `internal/run/events.go`, `internal/run/format.go`, `internal/run/state.go`, `internal/run/engine_test.go`, `internal/source/factory.go`. Create `internal/build/cutover_test.go`.

**Interfaces:**
- Consumes: `canHostForeman`, `foremanHarness` (today in `internal/build/foreman.go`).
- Produces, in `internal/build/orchestrator.go`:
```go
// canHostMainSession: the harness runs custom agents as a main session and can give one a shell.
func canHostMainSession(h config.Harness) bool
// mainSessionHarness is a copy of h whose agents.static is merged with the main-session static keys. h is never modified.
func mainSessionHarness(h config.Harness) config.Harness
```
Both keep the bodies of the functions they replace. `config.Harness.Foreman` is still the field they read: Task 5 renames it.

**Rules:**
- `Plan` writes no `foreman-*` output for any harness or scope.
- `Plan`'s notes, for an enabled harness when the project has at least one factory:
  - it can't host a main session: `no orchestrator: <Display> can't host a main-session agent with shell (crew run and crew mission still work in a terminal)`
  - it can, but has no orchestrator launch line: `no orchestrator: harnesses.yaml has no orchestrator launch line for <Display> (crew run and crew mission still work in a terminal)`
  - the existing note for a harness that can't apply deny rules is unchanged.
- `crew plan` / `crew compile` print no `foremen:` line. `printForemen` is deleted.
- `crew verify` no longer checks a generated foreman's name against harness rules, and an agent named `foreman-<factory>` is an ordinary agent: the clash error is deleted. `factory.ForemanPrefix`, `ForemanAgent` and `ForemanBody` no longer exist.
- Comments that say "the foreman" say what is true now (the orchestrator, or `crew run status`).
- `README.md`: the factories intro sentence and the section "### The foreman" are rewritten. The section becomes "### Talking to a run": start the orchestrator with the line `crew compile` prints, tell it which factory to run and what to produce; it starts the run, watches it, relays notes, passes what you say to the stations and asks at approval stops; it never runs a station, edits files or picks a route. One sentence says an older `foreman-<factory>` agent is removed by the next `crew compile`.

**Tests:**
- `internal/build/cutover_test.go`:
  - `TestPlan_WritesNoForemen` (the template project, claude-code and opencode enabled, user and project scope: no planned file's name starts with `foreman-`).
  - `TestPlan_NoteWhenAHarnessGetsNoOrchestrator` (codex enabled: the first note above, with Codex's display name; a harness given `foreman: {launch: x}` and deny support but no orchestrator launch: the second).
  - `TestCompile_RemovesWhatAnOlderCompileWroteForAForeman` (add an agent source named `foreman-ship-change`, compile to a staging `Out`, check the link and the built file exist and are in the ownership record; delete the agent source, compile again; the link and the built file are gone, the record no longer lists them, and every other output is unchanged). An old foreman was recorded with the same kinds (`agent-build` and a linked `agent`) as any agent, so this is the path an upgrade takes.
  - `TestCompile_AForemanFileEditedByHandIsNotDeleted` (as above, but append a line to the built file before the second compile: the compile returns an error naming that path and containing `edited by hand`, and the file and its link are still there).
- `internal/verify/factories_test.go`: replace `TestFactories_ForemanNameClash` with `TestFactories_AgentMayBeNamedLikeAnOldForeman` (the same setup verifies with no error).
- `cmd/crew/factory_test.go`: replace `TestPrintMatrix_ListsForemen` with `TestPrintMatrix_ListsNoForemen` (the output holds no `foremen:` and still holds the `orchestrator:` launch line).

- [ ] **Step 1:** Write the new tests and the two replacements; delete `TestPlan_ForemanPerFactory`, `TestPlan_ForemanNoteWhenUnsupported`, `TestCompile_StillEmitsForemen` and `internal/factory/foreman_test.go`.
- [ ] **Step 2:** Run `go test ./internal/build/ ./internal/verify/ ./cmd/crew/ -run 'TestPlan_WritesNoForemen|TestPlan_NoteWhen|TestCompile_Removes|TestCompile_AForeman|TestFactories_AgentMay|TestPrintMatrix_ListsNoForemen'`. Expected: `TestPlan_WritesNoForemen`, `TestPlan_NoteWhenAHarnessGetsNoOrchestrator`, `TestFactories_AgentMayBeNamedLikeAnOldForeman` and `TestPrintMatrix_ListsNoForemen` FAIL; the two `TestCompile_` tests fail on the name clash until verify stops refusing the agent.
- [ ] **Step 3:** Delete and edit as the rules say.
- [ ] **Step 4:** Run the same command. Expected: PASS.
- [ ] **Step 5:** Run `go build ./... && go vet ./... && go test -count=1 ./...`. Expected: all pass.
- [ ] **Step 6:** Commit: `feat!: remove the foreman agents; the next compile deletes the ones it wrote`.

### Task 5: `harnesses.yaml` names the orchestrator

**Files:** Modify `internal/config/config.go`, `internal/build/orchestrator.go`, `internal/build/build.go`, `internal/scaffold/template/harnesses.yaml`, `internal/scaffold/upgrade.go`, `internal/scaffold/upgrade_test.go`, `internal/build/orchestrator_test.go` and any test that reads `Foreman`, `docs/missions.md`. Create `internal/config/oldkey.go`, `internal/config/oldkey_test.go`, `internal/scaffold/upgrade_orchestrator.go`, `internal/scaffold/upgrade_orchestrator_test.go`, `internal/scaffold/testdata/harnesses-before-cutover.yaml`, `internal/build/oldkey_test.go`.

**Interfaces (Produces):**
```go
// internal/config/config.go
type Harness struct {
	// … unchanged fields …
	Orchestrator *OrchestratorSpec `yaml:"orchestrator"` // can host the orchestrator as the main session; nil = it can't
	OldForeman   *oldForemanSpec   `yaml:"foreman"`      // the block's name before the cutover; folded into Orchestrator on load, then nil
	UsesOldKey   bool              `yaml:"-"`            // the file still says foreman:
}

// OrchestratorSpec says how a harness hosts the orchestrator as its main session.
type OrchestratorSpec struct {
	Static map[string]any `yaml:"static"` // keys that override agents.static for it (e.g. mode: primary)
	Launch string         `yaml:"launch"` // how the principal starts it; {name} and {settings}
	Allow  []string       `yaml:"allow"`  // native allow rules for its session
}

// internal/config/oldkey.go
type oldForemanSpec struct {
	Static             map[string]any `yaml:"static"`
	Launch             string         `yaml:"launch"` // started foremen; read and dropped
	OrchestratorLaunch string         `yaml:"orchestrator_launch"`
	Allow              []string       `yaml:"orchestrator_allow"`
}
// foldOldKeys returns r with every harness's foreman: block turned into Orchestrator.
func foldOldKeys(name string, r Registry) (Registry, error)

// internal/scaffold/upgrade_orchestrator.go
var cutoverAllow = []string{"Bash(crew run status:*)", "Bash(crew run say:*)", "Bash(crew run resume:*)", "Bash(crew factory diagram:*)"}
// renameForeman turns h's foreman: block into orchestrator:, taking the key's comments from tn when tn has the block. It reports whether h changed.
func renameForeman(h, tn *yaml.Node) bool
// addCutoverAllow appends to h's orchestrator.allow each rule of cutoverAllow that tn's list has and h's lacks. It reports whether h changed.
func addCutoverAllow(h, tn *yaml.Node) bool
// backupName is the first name not taken in dir: harnesses.yaml.before-missions, then .before-missions.2 … .before-missions.9.
func backupName(dir string) (string, error)
```
`ForemanSpec` and the `Foreman` field are removed. Every reader moves to `Orchestrator`: `canHostMainSession` (`h.Orchestrator != nil && …`), `mainSessionHarness`, `hasOrchestratorLaunch` (`h.Orchestrator.Launch != ""`), `OrchestratorLaunch`, `orchestratorSettings`.

**Rules:**
- **Loading.** `DecodeRegistry` calls `foldOldKeys` after the strict decode. A harness with `foreman:` and no `orchestrator:` gets `Orchestrator = {Static, Launch: orchestrator_launch, Allow: orchestrator_allow}`, `OldForeman = nil`, `UsesOldKey = true`. A harness with both is the error `<file>: harness <name> has both foreman: and orchestrator:; keep orchestrator: and remove foreman:`. `foldOldKeys` builds new `Harness` values; it does not change the map it was given.
- **Compile note.** For an enabled harness with `UsesOldKey`: `harnesses.yaml still says foreman: for <Display>; crew mission setup renames it to orchestrator:`.
- **The template.** `claude-code`:
  ```yaml
      orchestrator:                 # the orchestrator runs as the main session
        launch: "claude --agent {name} --settings {settings}"
        allow: ["Bash(crew mission:*)", "Bash(crew domain list:*)", "Bash(crew factory list:*)", "Read", "Grep", "Glob", "Bash(crew run status:*)", "Bash(crew run say:*)", "Bash(crew run resume:*)", "Bash(crew factory diagram:*)"]
  ```
  `opencode`:
  ```yaml
      orchestrator:
        static: {mode: primary}     # a primary agent (picked with Tab in the TUI), not a subagent
  ```
- **`crew mission setup`** (`UpgradeHarnesses`), per harness of the file:
  - *In the template.* The existing check comes first: a harness skipped because its headless command was changed by hand is left exactly as it is, old key included. Otherwise, in this order: `renameForeman`; add `orchestrator.launch` and `orchestrator.allow` from the template when absent (today's rule, under the new names); `addCutoverAllow`; the existing `deny` and `deny_via` steps.
  - *Not in the template.* `renameForeman` only.
  - `renameForeman`: the key `foreman` becomes `orchestrator`; inside it `launch` is removed, `orchestrator_launch` becomes `launch`, `orchestrator_allow` becomes `allow`, and `static` stays. A block left empty is removed with its key, unless the template has an `orchestrator:` block for that harness. A harness that has both keys is not touched and is reported in `Skipped` as `<name>: has both foreman: and orchestrator:; remove foreman:`.
  - The line reported for a changed harness names what was done, joined with `, `, from: `foreman: renamed to orchestrator:`, `orchestrator launch`, `orchestrator allow list` (or `orchestrator launch and allow list`), `allow rules for plain runs`, `deny rules`, `deny_via and {settings}`.
  - The backup is written under `backupName`'s result and never over an existing file; `HarnessUpgrade.Backup` holds that name. With all nine names taken, the error is today's, naming `harnesses.yaml.before-missions.9`.
  - A second run changes nothing and writes nothing.
- `docs/missions.md`, "An existing crew directory": the block shown is `orchestrator:` with `launch` and `allow`; a paragraph says setup renames an older `foreman:` block, drops its foreman `launch` line, adds the allow rules for plain runs to a list that lacks them, and numbers the backup when one already exists; and that a file not yet upgraded keeps working, with the compile note.

**Tests:**
- `internal/config/oldkey_test.go`: `TestDecodeRegistry_ReadsTheOldForemanKey` (`foreman: {static: {mode: primary}, launch: "x {name}", orchestrator_launch: "y {name} {settings}", orchestrator_allow: [A]}` → `Orchestrator` is `{Static: {mode: primary}, Launch: "y {name} {settings}", Allow: [A]}`, `OldForeman == nil`, `UsesOldKey`); `TestDecodeRegistry_OldKeyWithOnlyAForemanLaunch` (`foreman: {launch: "x"}` → `Orchestrator` is non-nil with an empty `Launch`); `TestDecodeRegistry_BothKeysIsAnError`; `TestDecodeRegistry_OldSpellingsUnderTheNewKeyAreRefused` (`orchestrator: {orchestrator_launch: x}` → an unknown-key error); `TestDecodeRegistry_NewKeyDoesNotSetUsesOldKey`.
- `internal/build/oldkey_test.go`: `TestOrchestratorOutputs_SameFromOldAndNewKey` (the template project compiled once as it is, and once with `testdata`'s before-cutover `harnesses.yaml` whose allow list is first made equal to the template's: the orchestrator agent file and `orchestrator.settings.json` are byte-for-byte equal); `TestPlan_NotesTheOldForemanKey` (the note above appears for claude-code with the old file and not with the template).
- `internal/scaffold/upgrade_test.go`: `TestUpgradeHarnesses_OldTemplateBecomesNewTemplate` now expects two `Added` lines, `claude-code:` then `opencode:`; `TestUpgradeHarnesses_KeepsOwnEdits` reads `cc.Orchestrator.Launch`.
- `internal/scaffold/upgrade_orchestrator_test.go`, with the new fixture `testdata/harnesses-before-cutover.yaml` (the template as it is on `main` before this plan: `git show d838b84:internal/scaffold/template/harnesses.yaml`):
  - `TestUpgradeHarnesses_FileFromBeforeTheCutoverBecomesTheTemplate` (the result is byte-for-byte the embedded template; `Added` has a line for `claude-code` holding `foreman: renamed to orchestrator:` and `allow rules for plain runs`, and one for `opencode`; the backup holds the original bytes).
  - `TestUpgradeHarnesses_KeepsAnAllowRuleTheOwnerAdded` (the fixture with `"Bash(ls:*)"` added to `orchestrator_allow`: the upgraded `allow` is the fixture's six rules in their order, then `Bash(ls:*)`, then the four of `cutoverAllow`; the file loads).
  - `TestUpgradeHarnesses_RenamesAHarnessTheTemplateDoesNotHave` (an extra harness `mine` with `foreman: {launch: "mine --agent {name}"}`: afterwards `mine` has neither key and the file loads; with `foreman: {static: {mode: primary}, launch: "x"}` it has `orchestrator: {static: {mode: primary}}`).
  - `TestUpgradeHarnesses_SecondBackupGetsANumber` (a file `harnesses.yaml.before-missions` already in the directory with the bytes `old`: `Backup == "harnesses.yaml.before-missions.2"`, that file holds the original bytes, and the first still holds `old`).
  - `TestUpgradeHarnesses_BothKeysIsSkipped`.
  - `TestUpgradeHarnesses_SecondRunAfterTheCutoverChangesNothing`.
  - `TestUpgradeHarnesses_CustomCommandLeftAlone` (existing) still passes unchanged: the skipped harness keeps `foreman:`.

- [ ] **Step 1:** Create the fixture with `git show d838b84:internal/scaffold/template/harnesses.yaml > internal/scaffold/testdata/harnesses-before-cutover.yaml`. Write the tests.
- [ ] **Step 2:** Run `go test ./internal/config/ ./internal/scaffold/ ./internal/build/ -run 'TestDecodeRegistry_|TestUpgradeHarnesses_|TestOrchestratorOutputs_SameFrom|TestPlan_NotesTheOld'`. Expected: FAIL to compile (`Orchestrator`, `UsesOldKey`, `backupName` undefined).
- [ ] **Step 3:** Implement: config first, then the readers in `internal/build`, then the template, then the upgrade.
- [ ] **Step 4:** Run the same command. Expected: PASS.
- [ ] **Step 5:** Run `go build ./... && go vet ./... && go test -count=1 ./...` and `go test -race -count=1 ./internal/mission/ ./internal/gitx/ ./internal/source/`. Expected: all pass.
- [ ] **Step 6:** Commit: `feat: harnesses.yaml says orchestrator:, and crew mission setup renames foreman:`.

### Task 6: The guide and the README say what is true now

**Files:** Modify `docs/missions.md`, `README.md`, `internal/scaffold/template/missions.yaml` (only if a comment names a later stage or a foreman). Create `cmd/crew/cutover_words_test.go`.

**Rules:**
- `docs/missions.md`:
  - "What this stage does and doesn't do": the last sentence no longer lists the cutover as missing; it says releases are what is still missing, that the orchestrator also drives plain runs (see "Plain runs"), and that the per-factory foreman agents are gone.
  - "Launching the orchestrator": one sentence that the same session starts plain runs.
  - "What the safeguards do and don't cover", layer 2: the orchestrator's own session pre-approves `crew run status`, `say` and `resume` and asks the principal before a start, a `goto` or an `approve`; and one new limit in the list: **a plain run's approval is your confirmation in the session, and nothing records your words for it**, unlike a mission's answer.
  - Every other sentence that says "this stage" about something that is no longer the newest stage is left alone: only what the cutover changes is edited.
- `README.md`: the Missions section's first paragraph mentions that the orchestrator also runs a single factory; nothing in the file names a foreman except the one sentence Task 4 wrote about older compiles.
- After this task the word "foreman" (any case, including "foremen") appears, outside `docs/superpowers/`, only in: `internal/config/config.go`, `internal/config/oldkey.go`, `internal/config/oldkey_test.go`, `internal/scaffold/upgrade.go`, `internal/scaffold/upgrade_orchestrator.go`, the test files and `testdata/` of `internal/scaffold`, `internal/build/cutover_test.go`, `internal/build/oldkey_test.go`, `internal/build/build.go` (the compile note), `internal/verify/factories_test.go`, `cmd/crew/factory_test.go`, `cmd/crew/cutover_words_test.go`, `docs/missions.md` and `README.md`.

**Tests:**
- `cmd/crew/cutover_words_test.go`: `TestCutover_ForemanIsNamedOnlyWhereTheOldKeyIsHandled` (walk the repo from `../..`, skipping `.git`, `.worktrees`, `.superpowers` and `docs/superpowers`; read every `.go`, `.md`, `.yaml` and `.yml` file; a file whose lower-cased text contains `forem` and is not in the list above fails the test, naming the file); `TestDocs_GuideHasAPlainRunsSection` (`docs/missions.md` has the heading `## Plain runs` and, under it, `--wait` and `crew run approve`); `TestDocs_GuideShowsTheOrchestratorBlock` (the guide contains `orchestrator:` followed by a `launch:` line, and no line that starts with optional spaces then `orchestrator_allow:`).

- [ ] **Step 1:** Write the tests.
- [ ] **Step 2:** Run `go test ./cmd/crew/ -run 'TestCutover_|TestDocs_Guide'`. Expected: `TestCutover_ForemanIsNamedOnlyWhereTheOldKeyIsHandled` fails naming whatever file still has the word, or passes if Tasks 4 and 5 left none; the two `TestDocs_Guide` tests pass if Tasks 3 and 5 wrote their sections. Whatever fails is this task's work list.
- [ ] **Step 3:** Edit the guide and the README as the rules say, and any file the first test names.
- [ ] **Step 4:** Run `go test ./cmd/crew/ -run 'TestCutover_|TestDocs_'`. Expected: PASS, `TestDocs_ExamplePlansParse` included.
- [ ] **Step 5:** Run `go build ./... && go vet ./... && go test -count=1 ./...` and `gofmt -l cmd internal` (expected: only the files it already lists on `main`, under `internal/run`).
- [ ] **Step 6:** Commit: `docs: the guide and the README after the cutover`.
