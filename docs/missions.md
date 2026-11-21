# Missions

A mission is a job that is too big for one factory run. You describe the job, the orchestrator splits it into pieces, crew runs each piece through a factory in its own git worktree, and every piece ends as one pull request that you merge on GitHub.

This guide is for the owner of a crew setup who has used factories and has not used missions yet.

## The words used here

- **Principal**: you, the person who owns the repo and makes the decisions. Only the principal starts a mission, answers its questions and merges its pull requests.
- **Mission**: one job, written as a request plus a plan of pieces. It lives in the repo under `.crew/missions/<id>/`.
- **Piece**: one part of the mission. A piece has an id (`P1`, `P2`), a domain, a factory and a brief, and it may wait for other pieces.
- **Domain**: an area of the project, such as "web front end" or "API". It is a file in `domains/` that says which factories may do work there and carries a short briefing every station in that area reads.
- **Factory**: a fixed line of stations, the same factories you already run with `crew run`.
- **Station**: one step in a factory, done by one agent.
- **Run**: one pass of a factory. Each piece gets one run, in its own worktree.
- **Orchestrator**: the agent you talk to. It plans the mission, shows you the plan and relays what is waiting on you. It never edits files and never merges anything.
- **Engine**: the crew process that does the work once you say go. It starts pieces, watches their runs, pushes branches and opens pull requests.

## What stage 1 does and doesn't do

Stage 1 runs one piece at a time. A piece that has stopped to wait for your answer in the middle of its run (an approval, a question) still counts as running, so it holds the next piece back; a piece whose pull request is waiting for your merge doesn't, so the next one can start. The limit is per mission: two missions started in the same repo each run their own piece, and nothing checks that they touch different files, so don't run two missions over the same part of a repo at once. Each piece produces one pull request, and you merge it yourself on GitHub. When you do, crew sees the merge, cleans up the piece's worktree and starts the next piece that was waiting.

Stage 1 does not merge for you, does not write to Jira, and does not run pieces in parallel. `crew automerge` and `crew release approve` don't exist yet, and `delegate: true` on a piece is refused.

## Setting up

### A new crew directory

`crew init` already writes `missions.yaml` and an empty `domains/` folder. You need four more things:

1. A domain.
2. A harness that can apply deny rules (the default `claude-code` entry can).
3. A confirmation of the repo, once.
4. A run of `crew compile`, which writes the orchestrator agent and its settings file (see "Launching the orchestrator").

### An existing crew directory

If you created your crew directory before missions existed, run this (add `--dir` if your crew directory isn't the default):

```sh
crew mission setup
```

It writes `missions.yaml` and `domains/` if they are missing and prints what it wrote, or `already set up`. It does not touch your `harnesses.yaml`, so you add the missions parts by hand. Until you do, `crew mission start` refuses, and `crew verify -v` prints one note per harness that can't apply deny rules. The note is an info line, so plain `crew verify` leaves it out; `-v` shows it:

```
info   crew.yaml: harness opencode: can't apply deny rules, so it can't run mission pieces
```

Plain `crew run` keeps working on such a harness. Only missions need the deny rules.

Copy these blocks from the template (`internal/scaffold/template/harnesses.yaml`) into the `claude-code` entry of your own `harnesses.yaml`.

In `headless:`, add `--settings "{settings}"` to the command and `deny_via: settings-file`:

```yaml
    headless:
      command: [claude, -p, --agent, "{agent}", --allowedTools, "{allowed}", --settings, "{settings}", --permission-mode, dontAsk, --output-format, json]
      prompt_via: stdin
      allow_via: flag
      deny_via: settings-file     # deny rules go in a temp settings file at {settings}
      output: claude-json
```

Under `foreman:`, add the two orchestrator lines:

```yaml
    foreman:
      launch: "claude --agent {name}"
      orchestrator_launch: "claude --agent {name} --settings {settings}"
      orchestrator_allow: ["Bash(crew:*)", "Read", "Grep", "Glob"]
```

Next to `foreman:`, add the `deny:` block:

```yaml
    deny:                         # refused in every session crew starts; {crew_dir} = this config directory
      all:
        - "Bash(gh pr merge:*)"
        - "Bash(gh pr close:*)"
        - "Bash(git push --force:*)"
        - "Bash(git push -f:*)"
        - "Bash(git push origin main:*)"
        - "Bash(git push origin HEAD:main:*)"
        - "Bash(crew automerge:*)"
        - "Bash(crew repo confirm:*)"
        - "Bash(crew release approve:*)"
        - "Edit(/{crew_dir}/**)"
        - "Write(/{crew_dir}/**)"
      stations:                   # extra, for stations and headless orchestrator calls
        - "Bash(crew run:*)"
        - "Bash(crew mission:*)"
```

The two path rules start with `/{crew_dir}/**` because Claude Code reads a path with one leading slash as relative to the settings file, and a path with two leading slashes as absolute. `{crew_dir}` is already an absolute path that starts with a slash, so writing one slash in front of it gives the two you need.

Run `crew verify` afterwards. The note about your harness should be gone. Then run `crew compile`, which writes the orchestrator agent and the settings file with its deny rules; `crew verify` only prints the launch line.

### A domain

A domain is a Markdown file in `domains/`. The name must match the file name, and `factories` must list at least one factory you have:

```markdown
---
name: web
description: The web front end, covering pages, components and styling.
factories: [ship-change]
---
Use the existing component library. Keep changes small.
```

The body is the briefing: every piece in this domain gets it as part of its input. The orchestrator picks domains by their `description`, so write it as the sentence that tells it when to choose this domain. The frontmatter is YAML, which breaks on a colon followed by a space and on a space followed by `#`. If your description has either, put it in quotes:

```yaml
description: "The web front end: pages, components and styling."
```

Use single quotes when the text itself contains double quotes. The same applies to a piece's `brief:` in a plan.

List what you have:

```
$ crew domain list
web                      1 factories  The web front end, covering pages, components and styling.
```

### Confirming the repo

Missions push branches and open pull requests, so crew asks you to confirm each repo once. Run this in a terminal, inside the repo:

```sh
crew repo confirm
```

It shows the repo, its visibility (in capitals when the repo is public) and its remote, and asks you to type the repo's name. It reads your answer from the terminal itself, not from standard input, and has no flag that skips the question. After you answer it prints `confirmed acme/app as private`.

`crew repo status` shows where you stand:

```
acme/app: confirmed as private (2026-11-21)
```

If the repo's visibility changes later (private to public, say), `start`, `go` and `resume` refuse to run, and `crew repo status` says what changed. In a public repo every piece branch is public, and so is the text of each pull request, which includes your request.

A mission can run for days, so the engine checks again before every push. If the confirmation has been removed or the visibility has changed, nothing is pushed: the mission is stopped, you get a notification, and the message names the two commands to run, `crew repo confirm` and then `crew mission resume <id>`. The piece keeps its state, so resume carries on where it was. If GitHub can't be reached for the check, the engine notes it and tries again on the next tick.

`start`, `go`, `resume` and that check also make sure the repo gh works on is the repo `origin` points at. In a clone with both `upstream` and `origin`, gh may default to `upstream`; when the two names differ crew refuses and says so. Fix it with `gh repo set-default`. An `origin` that has no owner/repo form (a local path) isn't compared.

### Mission settings

`missions.yaml` holds the settings. The ones you are most likely to change:

- `mission_approval: on` makes `start` stop and wait for your `go`. Set it to `off` and `start` begins the mission at once.
- `limits.runs_at_once` must be `1` for now, and so must any `runs_at_once` under `repos:`. Pieces run one at a time until crew can check that parallel pieces don't touch the same files. Any other value, including `"*"`, makes crew refuse the file with an error like `limits.runs_at_once must be 1 (got "3"): pieces run one at a time until crew can check that parallel pieces don't overlap`.
- `orchestrator_harness` is the harness the engine uses for its own headless calls when a piece fails and needs a decision.
- `remind_after: 30m` is when crew first reminds you of an item that is still waiting, measured from when the item started waiting. Later reminders come at twice, four times and so on that long after it started: with 30 minutes, at 30 minutes, 1 hour and 2 hours.
- `limits` caps what a mission may do: pieces, steps, orchestrator calls per piece, and so on. A number or `"*"` for no limit. A `repos:` section overrides them for one repo. `steps` is read but not enforced in this stage.

## Launching the orchestrator

You talk to the orchestrator in your coding tool, started from the line crew prints. `crew compile` writes the orchestrator agent and its settings file, so run it first; `crew verify` and `crew compile` both print the line:

```
claude-code:
  orchestrator: claude --agent orchestrator --settings /home/you/crew/build/claude-code/agents/orchestrator.settings.json
```

Run that line in a terminal inside the repo you want to work on. The `--settings` file carries the deny rules and the allowed tools. If you start the orchestrator any other way, for example `claude --agent orchestrator`, the session has only its tool list and its prompt, with no deny rules. Use the printed line.

Then tell it the job. It runs `crew mission status`, looks at your domains and factories, writes a plan, and runs `crew mission plan`. You don't have to type any of the commands below yourself, but you can, and it is worth knowing them: they are what the orchestrator runs on your behalf, and what you use when you'd rather not go through it.

## The commands

All of them take `--dir` (the crew directory) and `--project` (the repo; default: the current directory).

| Command | What it does |
| --- | --- |
| `crew mission setup` | Writes `missions.yaml` and `domains/` if they are missing. |
| `crew mission plan [-\|@FILE]` | Checks a plan and creates the mission. Reads the plan from standard input, or from a file. |
| `crew mission start <id>` | Runs the start checks. With approval on, stops at "awaiting go". |
| `crew mission go <id>` | Your go. Re-runs the checks, then starts the engine. |
| `crew mission status [<id>] [--since N]` | Lists missions, or shows one in detail. |
| `crew mission answer <id> <wait> --do CHOICE --words TEXT` | Answers something that is waiting on you. |
| `crew mission amend <id> [-\|@FILE] --words TEXT` | Changes the plan after the mission has started. |
| `crew mission stop <id>` | Stops a running mission. |
| `crew mission resume <id>` | Continues a stopped mission. |

### A plan

A plan is a Markdown file. The frontmatter lists the pieces. The body is your request, word for word, because crew compares later changes against it. The line `<!-- crew:changes -->` ends the request: everything between the frontmatter and that line is the request, word for word, and crew records later changes below it, under `## Changes`. A plan without the marker line is rejected.

```markdown
---
pieces:
  - id: P1
    domain: web
    factory: ship-change
    brief: Add a /health endpoint.
    paths: [api/**]
  - id: P2
    domain: web
    factory: ship-change
    brief: Show the health status on the settings page.
    waits_for: [P1]
---
Add a health endpoint and show its status on the settings page.
<!-- crew:changes -->
## Changes
```

A piece's `paths` records where the piece is meant to work. It is optional, and crew doesn't enforce it yet.

`crew mission plan` checks it before creating anything: every domain and factory must exist, the factory must be one of that domain's, no piece may wait for itself or for a missing piece, pieces can't wait on each other in a loop, every brief must be filled in, the piece count must be no more than your limit, and every station's harness must be able to apply deny rules. If anything fails it lists every problem and creates nothing. If the plan passes, it prints the mission id, a table of pieces, a diagram, and the next command:

```
mission m-20261121-162953-e991 planned (/home/you/src/app/.crew/missions/m-20261121-162953-e991)

PIECE  DOMAIN  FACTORY      WAITS FOR
P1     web     ship-change  -
P2     web     ship-change  P1

(a mermaid diagram follows)

next: crew mission start m-20261121-162953-e991
```

### The go gate

`start` and `go` are two steps on purpose.

- `crew mission start <id>` checks that the plan still passes, that every harness can apply deny rules, and that the repo is confirmed and still has the visibility you confirmed. With `mission_approval: on` it then stops, shows the plan and prints `waiting for go: crew mission go <id>`. With `mission_approval: off` it starts the engine straight away.
- `crew mission go <id>` is your decision. It works only on a mission that is waiting for go, runs the same checks again (a lot can change between the two commands) and then starts the engine. It saves a copy of the plan you approved as `approved.md`. (With `mission_approval: off`, `start` saves it, because starting is the approval.)

Either one ends with:

```
mission m-20261121-162953-e991 running (pid 4242)
follow with: crew mission status m-20261121-162953-e991
```

The orchestrator runs `go` only when you have told it to, in the conversation, after seeing the plan.

### Status

`crew mission status` with no id lists missions, newest first:

```
m-20261121-162953-e991  running  2 pieces, 1 waiting on you
```

With an id it shows the mission in detail. Items waiting on you come first, then what is running, what is queued, what has finished, and the recent events:

```
mission m-20261121-162953-e991  running  live, pid 4242  (acme/app, base main)

Waiting on you:
  w1  P1  merge  https://github.com/acme/app/pull/7  waiting 12m

Queued:
  P2  waits for P1

Events:
  1  11:29:53  mission-start
  2  11:29:54  resumed
  3  11:29:54  piece-start  P1  crew/m-20261121-162953-e991/P1
  4  11:41:12  pr-opened  P1  PR #7
  5  11:42:12  waiting-on-principal  P1  merge: PR #7 (P1) is waiting on you
  6  11:42:12  notified  PR #7 (P1) is waiting on you

cursor 6
```

The engine writes `resumed` every time it starts, the first time included.

A mission marked `running` whose engine has died (after a reboot, say) says so, in the list and in the detail view: `engine not running; crew mission resume <id>` takes the place of `live, pid …`.

`--since N` prints only the events after cursor `N`, which is how a script or the orchestrator follows along.

## What "waiting on you" means

The engine does not guess at anything that needs a person. When it reaches such a point it parks the piece, records a wait with an id (`w1`, `w2`, ...), tells you with a desktop notification (macOS only for now) and shows it under "Waiting on you". If you don't answer, it reminds you at `remind_after`, then at twice that, then four times that, all measured from when the item started waiting (with 30 minutes: 30 minutes, 1 hour, 2 hours).

crew also notifies you, once, when the same failure has kept repeating for the `remind_after` interval (for five ticks in a row if `remind_after` isn't set): GitHub logged out, a push refused by permissions or a hook, a mission file crew can't read. The notification names the mission, the piece if there is one, and the error. A failure that clears and comes back is told again. A changed repo visibility stops the mission until you confirm again (see "Confirming the repo").

There are five kinds:

| Kind | What it means | What you can answer |
| --- | --- | --- |
| `merge` | The piece's pull request is open and waiting for you. | Nothing to type. Merge it on GitHub. crew sees the merge and moves on. `cancel` abandons the piece. |
| `approval` | A station in the piece's factory is set to pause for approval (`approve: true`) and has finished. | `approve` or `cancel`. `approve` takes an optional `--station` to continue from a station other than the next one. |
| `question` | The orchestrator couldn't settle a failure, or you must decide something. | `retry`, `goto` or `cancel`. |
| `limit` | A piece used up its orchestrator calls. | `retry`, `goto` or `cancel`. |
| `pr-closed` | The pull request was closed without being merged. | `retry` (after you reopen it, crew goes back to watching it) or `cancel`. |

A `question` about a piece that hasn't started yet takes only `cancel`. This happens when a queued piece waits for a piece that was cancelled: the piece can never start, so either cancel it too or change the mission with `amend` (see below), for example by dropping the dependency. Amending the piece clears the question and puts it back in the queue.

To answer, pass the wait's id and what you said. `--words` is required: it records your answer word for word.

```sh
crew mission answer m-20261121-162953-e991 w2 --do goto --station build \
  --note "the plan missed the migration" --words "send it back to build, it missed the migration"
```

`--do goto` needs `--station`. `--note` passes a message to the station. `--words` and `--note` each take text or `@FILE`. If the choice doesn't fit the wait, the error lists the ones that do.

A command that changes a mission while the engine is running doesn't edit the mission itself. It leaves the answer in the mission's `inbox/`, and the engine applies it on its next tick (once a minute). If the engine isn't running, the answer is applied when you resume.

## When a station fails

A run can end badly in three ways, and crew handles them in this order.

1. **The run failed in a way that can be retried** (it was interrupted, say). The engine resumes the run where it stopped, at most twice per piece.
2. **The run finished but left uncommitted files.** The engine sends the piece back once, to the factory's rework station (its `rework` station, or its start station when none is set), with a note listing the files. If files are uncommitted again when it finishes, that counts as a failure like the next one.
3. **The run failed in a way that can't be retried, a route hit its `max`, or the two resumes or the one send-back didn't help.** The engine asks the orchestrator for one decision in a headless call. The orchestrator picks `retry`, `goto`, `cancel`, `rebrief`, `add` or `escalate`. A decision that can't be used, or that repeats an action it already took for the same kind of stop, comes to you as a `question`, and so does `escalate`.

Each piece may use `orchestrator_calls_per_piece` such calls (3 by default). When they are used up, the next failure becomes a `limit` wait for you without another call.

A git or GitHub command that doesn't answer is cut off (git after 30 seconds, or 2 minutes when it reaches the network; gh after 60 seconds, and git never asks for a password) and is tried again on the next tick.

A worktree that holds uncommitted work is left in place when its piece is cancelled, so you can look at it. crew says so in a note and keeps trying to remove it each time the engine starts; remove it by hand when you are done. A piece that completes has its worktree removed, because its work is merged or empty.

## Changing a mission after it has started

Use `crew mission amend` when you change your mind mid-mission. Give it the whole revised plan and your words:

```sh
crew mission amend m-20261121-162953-e991 @new-plan.md --words "also add a P3 for the docs"
```

The revised plan is the whole file with the request and the marker lines exactly as they are in the current plan (`.crew/missions/<id>/mission.md`), and only the pieces changed. For the plan above, adding a docs piece looks like this:

```markdown
---
pieces:
  - id: P1
    domain: web
    factory: ship-change
    brief: Add a /health endpoint.
    paths: [api/**]
  - id: P2
    domain: web
    factory: ship-change
    brief: Show the health status on the settings page.
    waits_for: [P1]
  - id: P3
    domain: web
    factory: ship-change
    brief: Document the /health endpoint.
    waits_for: [P1]
---
Add a health endpoint and show its status on the settings page.
<!-- crew:changes -->
## Changes
```

If the current plan already has lines under `## Changes`, you can leave them out or keep them: crew keeps its own change log either way. What you said goes in `--words`, and crew passes it to the stations of every piece that starts afterwards, after the request, so a later change wins where it differs from the request.

crew checks the new plan against the old one first and refuses it, changing nothing, in these cases:

- The request text differs. An amendment can't change the request; start a new mission for a different job.
- The plan is the same as the current one.
- The mission hasn't started. Amend works on a running or stopped mission that has been started; for a planned mission, or one waiting for go, make a new plan with `crew mission plan`.
- The mission is done.
- A piece that has started (running, waiting, with a pull request open, or complete) has a changed field. Only a piece that has not started can be changed: a queued one, or one parked on a question because it waited for a piece that was cancelled.
- A complete piece is removed from the plan.
- A new piece reuses an id that was used before in this mission, even one that was cancelled.
- The new plan fails the same checks `plan` runs.

What an amendment may do: add pieces, change a queued piece (its brief, its domain, its factory, its paths, what it waits for), and remove any piece that isn't complete, which cancels it. Every change goes in the mission's change log with your words.

## Stop and resume

`crew mission stop <id>` applies to a running mission. It tells the engine to shut down, which interrupts the running pieces' runs where they are. Worktrees and branches stay in place. A mission that hasn't started has nothing to stop, and one that is already stopped or done is left alone.

`crew mission resume <id>` continues a stopped mission, and also a mission marked running whose engine has died. It needs a mission that has been started, which is to say one with an `approved.md` (written by `go`, or by `start` when approval is off); a planned mission needs `start` instead. It re-runs the start checks, marks the mission running and starts the engine again, which picks up each piece where it was.

If the engine process crashes, a supervisor restarts it up to three times and records each restart as an event. After that it marks the mission stopped and notifies you. A reboot also ends the engine; run `resume` afterwards.

## Where things live

Everything a mission writes goes in the repo it works on, under `.crew/`. crew writes a `.gitignore` there so none of it reaches git.

```
<repo>/.crew/
├── missions/<mission-id>/
│   ├── mission.md        the plan and your request, with a change log
│   ├── approved.md       the plan as it was when the mission started
│   ├── state.json        where each piece is
│   ├── events.jsonl      what happened, one line per event
│   ├── inbox/            answers and amendments waiting for the engine
│   └── mission.log       the supervisor's and engine's output
├── runs/<run-id>/        each piece's run, as for crew run
└── worktrees/<mission-id>-<piece-id>/
```

A mission id looks like `m-20261121-162953-e991`. A piece's branch is `crew/<mission-id>/<piece-id>`, created from the base branch on `origin`.

The engine is the only writer of `state.json` and `mission.md` while it runs. The other commands leave you a file in `inbox/`, or change the state only when no engine is running.

When a piece's pull request is merged, crew removes the piece's worktree. When a piece finishes with nothing to commit, it is complete at once and no pull request is opened. Nothing is ever deleted on GitHub: crew does not delete remote branches, close pull requests or merge them. The local piece branches stay where they are.

## What the safeguards do and don't cover

Agent sessions run as your own Unix user, with your GitHub login. No file, checksum or terminal prompt can stop a process that sets out to get around it. So the safeguards protect against mistakes, drift and a misread prompt. They are not a boundary against a process that deliberately works around them.

What crew promises is this: crew itself never merges, closes or releases anything without your approval, and every session crew starts is launched with rules that refuse the commands that would. Three layers do the work.

1. **Only crew acts.** No agent prompt asks an agent to merge or release. In stage 1 nothing merges at all; you do it.
2. **Session rules.** Every session crew starts, whether a station or an orchestrator call, gets the deny rules from `harnesses.yaml`: merging or closing a pull request, force-pushing, pushing to `main`, `crew automerge`, `crew repo confirm`, `crew release approve`, and writing anywhere in your crew directory. Stations and headless orchestrator calls also can't run `crew run` or `crew mission`, so a station can't pass an approval stop or answer for you. A harness that can't apply deny rules can't run mission pieces, and `crew verify` says which ones.
3. **Commands only you can run.** `crew repo confirm` needs a terminal and a typed confirmation, with no flag and no piped input that skips it.

The limits, specifically:

- **Deny rules match command prefixes.** A command written another way, such as other flags first or a different spelling of the same push, isn't caught.
- **The rule protecting the crew directory covers the file-editing tools.** A write made through a shell command isn't caught.
- **"Pushing to main" is matched literally** (`git push origin main`, `git push origin HEAD:main`). If the repo's default branch has another name, add rules for it in `harnesses.yaml`.
- **The mission's own folder in the repo (`state.json`, `inbox/`) and `.crew/runs/` are not protected.** "A station can't answer for you" is true of the `crew` commands; a file written into `inbox/` by another route would be applied as your answer.
- **Where the confirmation is read from depends on which crew directory the command resolves.** Confirmations are stored under `<crew dir>/principal/`, so `--dir`, `CREW_HOME`, or a `crew.yaml` in the current directory choose which record is read.
- **The checksum on the confirmation file catches hand edits and accidents only.** It isn't keyed, so someone who understands it can rewrite the file and the checksum together.
- **A pseudo-terminal can fake the terminal.** That is why the session rules also deny `crew repo confirm`.
- **The orchestrator session is yours.** crew can only apply the deny rules to it if you start it from the printed launch line.
- **Answers relayed by the orchestrator are recorded word for word, but nothing proves you said them.**
- **crew pushes whatever stations commit.** It doesn't scan for secrets or stray files.
- **Checks are only as good as the repo's CI.** The factory's review station is the only thing between an agent's work and your review of the pull request.
- **Pushing publishes in a public repo.** After you confirm a repo once, every piece branch in a public repo is public.
- **Merging is not deploying, unless your repo makes it so.** If a merge to `main` triggers a deploy, your merge is a deploy.

A real boundary would need agents to run under a separate identity, such as another OS user or a GitHub account that can't merge. Stage 1 doesn't do that.
