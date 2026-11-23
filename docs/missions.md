# Missions

A mission is a job that is too big for one factory run. You describe the job, the orchestrator splits it into pieces, crew runs each piece through a factory in its own git worktree, and every piece ends as one pull request. You merge it on GitHub, or you switch auto-merge on and crew merges it for you.

This guide is for the owner of a crew setup who has used factories and has not used missions yet.

## The words used here

- **Principal**: you, the person who owns the repo and makes the decisions. Only the principal starts a mission, answers its questions and decides whether crew may merge its pull requests.
- **Mission**: one job, written as a request plus a plan of pieces. It lives in the repo under `.crew/missions/<id>/`.
- **Piece**: one part of the mission. A piece has an id (`P1`, `P2`), a domain, a factory and a brief, and it may wait for other pieces.
- **Domain**: an area of the project, such as "web front end" or "API". It is a file in `domains/` that says which factories may do work there and carries a short briefing every station in that area reads.
- **Factory**: a fixed line of stations, the same factories you already run with `crew run`.
- **Station**: one step in a factory, done by one agent.
- **Run**: one pass of a factory. Each piece gets one run, in its own worktree.
- **Orchestrator**: the agent you talk to. It plans the mission, shows you the plan and relays what is waiting on you. It never edits files, never merges anything, and never turns auto-merge on.
- **Engine**: the crew process that does the work once you say go. It starts pieces, watches their runs, pushes branches and opens pull requests.

## What this stage does and doesn't do

Pieces run together, up to `runs_at_once` at a time (3 in a new crew directory). The count is across all of the repo's missions that are running with a live engine, not per mission. A piece counts while it is running, and also while it is stopped in the middle of its run to wait for your answer (an approval, a question), because it will go on running afterwards. A piece whose pull request is waiting for your merge doesn't count, so the next piece can start. One kind of piece is never held back by the limit: a piece that crew sends back from its pull request (see "Failed checks and conflicts") runs again at once.

Running together is safe only when pieces don't change the same files, so crew checks. It keeps pieces whose `paths` overlap apart (see "Running pieces together"), it does the same across two missions in one repo (see "Two missions in one repo"), and it stops a piece that changes files outside its own paths (see "Files outside a piece's paths"). It also stops taking on new work when a mission has used its step budget (see "Budgets").

Each piece produces one pull request. With auto-merge off, which is the default, you merge it yourself on GitHub. With auto-merge on for the piece's domain, crew merges it when the rules in "Auto-merge" are met. Either way, crew sees the merge, cleans up the piece's worktree and starts the next piece that was waiting.

Crew merges a pull request only when you have switched auto-merge on for its domain (see "Auto-merge"). It can show a mission on your Jira board and take files a piece leaves as a deliverable (see "Jira" and "Deliverables"); it does so only when you have set Jira up, and it never reads anything from Jira except the one thing named there. A piece can hand its work on to pieces of its own: delegation (see "Delegation"). Still missing: releases (`crew release approve` doesn't exist yet) and the cutover.

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

It writes `missions.yaml` and `domains/` if they are missing, and it upgrades your `harnesses.yaml` itself. For each harness your file shares with the template, it adds the missions parts that are missing: `--settings "{settings}"` in the headless command, `deny_via`, the `deny` block, and the orchestrator launch line and allow list. Your comments and every value you already have stay as they are. The one exception: a `harnesses.yaml` that differs from the default only by comments is replaced by the new default, and your comments are in the backup copy. Before it writes, it copies the old file to `harnesses.yaml.before-missions` (it won't overwrite an existing copy), and it refuses to write anything that wouldn't load. It prints what it changed, then `next: crew compile`, or `already set up` when there was nothing to do.

The one case it leaves alone is a `headless.command` you changed by hand. It prints a `skipped` line for that harness; add `--settings "{settings}"` to the command yourself and run `crew mission setup` again.

Until a harness has these parts, `crew mission start` refuses and prints `run crew mission setup to add the missions parts to harnesses.yaml`, and `crew verify -v` prints one note per harness that can't apply deny rules. The note is an info line, so plain `crew verify` leaves it out; `-v` shows it:

```
info   crew.yaml: harness opencode: can't apply deny rules, so it can't run mission pieces
```

Plain `crew run` keeps working on such a harness. Only missions need the deny rules.

#### What setup adds to `claude-code`

These are the blocks, for reference, as they appear in the template (`internal/scaffold/template/harnesses.yaml`).

In `headless:`, the command gets `--settings "{settings}"` to the command and the entry gets `deny_via: settings-file`:

```yaml
    headless:
      command: [claude, -p, --agent, "{agent}", --allowedTools, "{allowed}", --settings, "{settings}", --permission-mode, dontAsk, --output-format, json]
      prompt_via: stdin
      allow_via: flag
      deny_via: settings-file     # deny rules go in a temp settings file at {settings}
      output: claude-json
```

Under `foreman:`, the two orchestrator lines:

```yaml
    foreman:
      launch: "claude --agent {name}"
      orchestrator_launch: "claude --agent {name} --settings {settings}"
      orchestrator_allow: ["Bash(crew mission:*)", "Bash(crew domain list:*)", "Bash(crew factory list:*)", "Read", "Grep", "Glob"]
```

`orchestrator_allow` is what the orchestrator session may do without asking you: the `crew mission` commands, `crew domain list`, `crew factory list`, and reading and searching files. Any other command, `crew run approve` included, makes it ask you first, as it would in any session.

Next to `foreman:`, the `deny:` block:

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
paths: [web/**]
---
Use the existing component library. Keep changes small.
```

`paths` is optional. It lists where the domain's files live, as patterns relative to the repo root: a folder name (`web/`), `brand/**` for everything under a folder, or `**/*.md` for every Markdown file. A leading `./` and a trailing `/` are fine. crew refuses a domain file whose `paths` has an empty entry, an entry that starts with `/`, an empty segment (`a//b`), a `..` segment, or a segment that isn't a valid pattern. A domain without `paths` is never counted as touched by a pull request, so its auto-merge switch doesn't hold back another domain's pull request (see "Auto-merge").

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
- `limits.runs_at_once` is how many pieces may run together, across all of the repo's missions. A new crew directory has `3`. A `missions.yaml` you already have keeps the value it has (older files say `1`, which means one piece at a time until you change it). It can be a number or `"*"` for no limit, and a `runs_at_once` under `repos:` overrides it for one repo. A mission's engine reads it when it starts, so a change reaches a running mission only when its engine next starts (`crew mission stop`, then `resume`), or while that mission is on hold, when its engine reads the whole file again each tick.
- `merge_method` is how crew merges a pull request when auto-merge is on: `squash` (the default), `merge` or `rebase`. Pick the one your repo allows; see "Auto-merge".
- `orchestrator_harness` is the harness the engine uses for its own headless calls when a piece fails and needs a decision.
- `remind_after: 30m` is when crew first reminds you of an item that is still waiting, measured from when the item started waiting. Later reminders come at twice, four times and so on that long after it started: with 30 minutes, at 30 minutes, 1 hour and 2 hours.
- `limits` caps what a mission may do: pieces, steps, orchestrator calls per piece, and so on. A number or `"*"` for no limit. A `repos:` section overrides them for one repo. Every limit must be present: a `missions.yaml` without `steps`, for example, is refused when crew reads it. A running engine reads `missions.yaml` once, when it starts, and again only while its mission is on hold for its step budget (see "Budgets"); any other change reaches a running mission when its engine next starts. `steps` and `delegation_depth` are enforced (see "Budgets"). `delegation_depth` is how many levels a piece may be handed down, and `0` switches delegation off; `pieces` counts sub-pieces too.

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
| `crew mission setup` | Writes `missions.yaml` and `domains/` if they are missing, and adds the missions parts to `harnesses.yaml`. |
| `crew mission plan [-\|@FILE]` | Checks a plan and creates the mission. Reads the plan from standard input, or from a file. |
| `crew mission start <id>` | Runs the start checks. With approval on, stops at "awaiting go". |
| `crew mission go <id>` | Your go. Re-runs the checks, then starts the engine. |
| `crew mission status [<id>] [--since N]` | Lists missions, or shows one in detail. |
| `crew mission answer <id> <wait> --do CHOICE --words TEXT` | Answers something that is waiting on you. The choices are `retry`, `goto`, `cancel`, `approve` and `accept`. `accept` is for a `deliverable` wait and for a `paths` wait. A `plan` wait takes `retry` or `cancel`. |
| `crew mission amend <id> [-\|@FILE] --words TEXT` | Changes the plan after the mission has started. |
| `crew mission stop <id>` | Stops a running mission. |
| `crew mission resume <id>` | Continues a stopped mission. |
| `crew jira setup --site URL --key KEY [--board N] [--assignee ACCOUNT_ID]` | Shows this repo's missions on a Jira board. Checks the site, then asks you to confirm in a terminal. |
| `crew jira check` | Tests the repo's Jira settings against the site. |
| `crew jira sync <id>` | Sends the Jira writes a finished or stopped mission still owes. |

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

A piece's `paths` says where the piece may work. It is optional. crew uses it to keep overlapping pieces apart and to stop a piece that strays (see "Running pieces together" and "Files outside a piece's paths").

`crew mission plan` checks it before creating anything: every domain and factory must exist, the factory must be one of that domain's, no piece may wait for itself or for a missing piece, pieces can't wait on each other in a loop, every brief must be filled in, the piece count must be no more than your limit, and every station's harness must be able to apply deny rules. A piece id is letters, digits, `-` and `_`, and the id `epic` is reserved in any case, because crew labels the mission's Jira epic with it (`piece id "epic" is reserved`). If anything fails it lists every problem and creates nothing. If the plan passes, it prints the mission id, a table of pieces, a diagram, and the next command:

```
mission m-20261121-162953-e991 planned (/home/you/src/app/.crew/missions/m-20261121-162953-e991)

PIECE  DOMAIN  FACTORY      WAITS FOR  PATHS
P1     web     ship-change  -          web/**
P2     web     ship-change  P1         web/**

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

With an id it shows the mission in detail. When the mission has an epic on your Jira board, a second line names it. Items waiting on you come first, then the open pull requests, the deliverables, what is running, what is delegated, what is queued, what has finished, and the recent events:

```
mission m-20261121-162953-e991  running  live, pid 4242  (acme/app, base main)

Waiting on you:
  w1  P1  merge  https://github.com/acme/app/pull/7  PR #7 (P1) is waiting on you  waiting 12m

Queued:
  P2  waits for P1

Events:
  1  11:29:53  mission-start  crew
  2  11:29:54  resumed
  3  11:29:54  piece-start  P1  crew/m-20261121-162953-e991/P1
  4  11:41:12  pr-opened  P1  PR #7
  5  11:42:12  waiting-on-principal  P1  merge: PR #7 (P1) is waiting on you
  6  11:42:12  notified  PR #7 (P1) is waiting on you

cursor 6
```

The engine writes `resumed` every time it starts, the first time included. After the piece, an event names who acted when it wasn't the engine: `crew` for something a `crew mission` command did (the go, a stop), `principal` for an answer or an amendment, `orchestrator` or `scoped orchestrator P2` for a decision (see "Delegation").

The Jira line gives the epic and its address. When the board is behind, it also says how many writes haven't been sent yet. `crew mission status` makes no call to Jira and needs no credentials:

```
Jira: ACME-1  https://example.atlassian.net/browse/ACME-1 (2 writes not sent yet)
```

If Jira is set up and the mission has started but nothing has reached the board yet (the epic couldn't be created), the line reads `Jira: nothing has reached the board yet`, with no count: until the epic exists crew can't tell how many writes will follow it. Otherwise the count reads `1 write not sent yet` for one and `<n> writes not sent yet` for more. A piece's Jira key follows its id in the open pull requests, deliverables and finished lines, as `P1 (ACME-2)`.

When the mission is on hold for its step budget, a line follows the first (see "Budgets"):

```
On hold: 60 of 60 station steps used. Raise limits.steps above 60 in missions.yaml; the engine reads it again within a minute.
```

A mission that is done shows no such line.

A `paths` wait reads like the others, and every file the piece changed outside its paths is listed under it, one per line, because `accept` takes all of them and the question names only three:

```
Waiting on you:
  w2  P3  paths  P3 changed 4 files outside its paths: README.md, go.mod, main.go and 1 more  waiting 3m
    README.md
    go.mod
    main.go
    util/clock.go
```

A piece that changed more than 200 files lists the first 200 and ends with `and <n> more (too many to accept: send the piece back or cancel it)`.

A queued piece that crew keeps apart says why in the "Queued" section: `P3  starts after P1 (paths overlap)`, or, for a piece kept behind another mission's piece, `P3  starts after P1 of mission m-20261121-101500-ab12 (paths overlap)` (with `, which is not running` after the mission id when that mission is stopped or its engine has ended).

A delegated piece has its own section, after "Running", with one line for each piece directly under it and the number of them in each status (a status with none is left out):

```
Delegated:
  P3  3 pieces: 1 complete, 1 running, 1 queued
```

The pieces under it are listed in the other sections under their own ids (`P3.1`, `P3.2`). A delegated piece that has not been split yet is queued, and reads `to be planned` when nothing it waits for is open (`on hold` while the mission is on hold). It never reads `waiting for a free run`, because it takes no run slot. A `plan` wait appears under "Waiting on you" like the others, with its kind: `w3  P3  plan  P3 could not be planned in 3 orchestrator calls: …  waiting 4m`.

A queued piece that nothing in the plan keeps back reads `ready to start`, unless something else does: `on hold` while the mission is on hold for its step budget, or `waiting for a free run: 3 of 3 in use across this repo's missions` when one more run would pass `runs_at_once`. Only missions that are running with a live engine count toward that number.

A `deliverable` wait reads like the others: `w1  P1  deliverable  <question>  waiting 4m`. Deliverables a piece has left are listed in their own section, with their files, who has to act and where crew keeps them. A deliverable of a piece that was cancelled before it was accepted shows `cancelled` there:

```
Deliverables:
  P5 (ACME-6)  brief.md, plan.md  waiting on you  .crew/missions/m-20261121-162953-e991/deliverables/P5
  P2 (ACME-3)  research.md  accepted by engine  .crew/missions/m-20261121-162953-e991/deliverables/P2
```

A piece that has delivered is listed there and under "Waiting on you", and nowhere else until it is complete.

A mission marked `running` whose engine has died (after a reboot, say) says so, in the list and in the detail view: `engine not running; crew mission resume <id>` takes the place of `live, pid …`.

A piece whose pull request is open and holds no wait is listed under "Open pull requests", with the auto-merge mode of its own domain:

```
Open pull requests:
  P1  PR #7  https://github.com/acme/app/pull/7  auto-merge on
```

If crew can't read the switch, the mode reads `off (switch unreadable)`. A piece whose pull request holds a wait is listed under "Waiting on you" only.

`crew mission plan` prints one line under the piece table, and `crew mission start` prints it before you say go only when approval is on, so you can see what will merge without you:

```
Auto-merge in acme/app: design on, api off
```

`--since N` prints only the events after cursor `N`, which is how a script or the orchestrator follows along.

## Running pieces together

Pieces that don't change the same files can run at the same time. To know which those are, crew looks at each piece's **paths**: the patterns, relative to the repo root, that say where the piece may change files. A pattern is a folder (`web/`), everything under a folder (`api/**`), or a file pattern (`**/*.md`, `docs/*.txt`). A piece's own `paths` in the plan win. When a piece lists none, crew uses its domain's `paths` (see "A domain"). A piece with neither has no paths, and crew can't check it: it is not kept apart from anything, and nothing it changes is checked. The plan says so.

Give each piece the narrowest `paths` that covers its work. The narrower they are, the more pieces can run together.

`crew mission plan` shows the paths in the `PATHS` column, then, right under the table, a note for each piece that needs one, then a line for each pair of pieces it keeps apart:

```
PIECE  DOMAIN  FACTORY      WAITS FOR  PATHS
P1     web     ship-change  -          api/**
P2     web     ship-change  -          web/**
P3     web     ship-change  -          api/v2/**
P4     web     ship-change  -          -
P4 has no paths, so crew can't check it against other pieces or check what it changes
P3 starts after P1: their paths overlap (P1: api/**, P3: api/v2/**)
```

The notes are of two kinds: a piece that has no paths, and a piece whose own `paths` reach outside its domain's (`P2 may change files outside the web domain: docs/**`). The second is only a heads-up; the piece still runs.

**How crew decides two patterns overlap.** Two patterns overlap when some file could match both. `api/**` and `api/v2/**` overlap. `api/**` and `web/**` don't. A pattern with no wildcard stands for itself and everything under it, so `api` and `api/v2/x.go` overlap. When crew can't tell, it says yes: keeping two pieces apart only costs time, while running them together can cost a merge conflict. A segment with a backslash in it is one crew can't read, so it overlaps anything at that place. A pattern that is malformed overlaps nothing. `crew mission plan` refuses a piece's path that holds a control or invisible character (`P1: paths: "app/?api/**": holds a control or invisible character`), since it could redraw the table you read to approve the plan.

**`apart_from`.** When two pieces overlap and neither waits for the other, crew writes `apart_from: [P1]` on the later one in the mission file, and the later piece doesn't start until P1 is complete or cancelled. The plan prints one line for each pair, as above. `apart_from` is crew's field: whatever a plan or an amendment supplies for it is replaced, and crew works it out again on every plan, start, amendment and added piece. A piece kept apart from a cancelled piece starts, because the piece in its way is gone. A piece that has already started is never given an `apart_from` entry (a piece parked on a question before it ever started still is): when it overlaps a later piece, that later piece is the one kept apart.

When one piece already waits for the other through `waits_for`, directly or through other pieces, there is nothing to separate, and crew adds nothing: a piece whose dependency is cancelled is parked on a question, so that order holds. An order made only of `apart_from` links doesn't count, because a link stops counting when the piece it names is cancelled. So with three pieces on the same paths, P3 gets `apart_from: [P2, P1]`, not just `[P2]`, and cancelling P2 doesn't let P3 start beside a running P1. Each such entry goes in the direction the existing order already gives, so no loop can form.

## Delegation

A piece with `delegate: true` is a part of the job that is itself several pieces of one domain, where how to split it is better decided once earlier work has landed. You give it a brief that says what the whole part must achieve and the `paths` it may touch, and no factory. When it is ready, crew asks that domain's orchestrator to split it into pieces of its own. Use it when you can't yet say what the pieces are, for example "move every settings page to the new layout" before the layout exists. When you can name the pieces now, write them in the plan: it is simpler.

```markdown
---
pieces:
  - id: P1
    domain: web
    factory: ship-change
    brief: Build the new settings layout.
    paths: [web/layout/**]
  - id: P2
    domain: web
    delegate: true
    brief: Move every settings page to the new layout, one piece for each page that needs more than a rename.
    paths: [web/settings/**]
    waits_for: [P1]
---
Move the settings pages to the new layout.
<!-- crew:changes -->
## Changes
```

`crew mission plan` shows `(delegated)` in the `FACTORY` column of the table, and under it says what the piece's pieces will be held to:

```
P2 is delegated: the web orchestrator splits it once it is ready. Its pieces stay in web, use web's factories and keep to web/settings/**.
```

A piece with no paths of its own and none from its domain ends the line with `and are not held to any paths.` instead. The diagram labels the piece as delegated:

```
flowchart TD
    P1["P1 · web"]
    P2["P2 · web · delegated"]
    P1 --> P2
```

**When it is planned, and by whom.** The first time the piece is ready, which is when everything it waits for is complete and nothing keeps it apart from another piece, the engine asks the orchestrator of the piece's domain to split it. This is a headless session like the one crew uses when a piece fails: it reads the repo and the domain's briefing, can read and search and nothing else, and what it replies is checked, never run. A mission that is on hold for its step budget plans nothing until the hold lifts. There is no approval step for the split. The sub-pieces start on the engine's next tick, and you get a notification that the piece was planned as so many pieces, with their ids. `crew mission plan` and `start` refuse a delegated piece when `orchestrator_harness` names a harness that can't apply deny rules (see "What the safeguards do and don't cover"), because the orchestrator would then not be allowed to run.

**The four rules a sub-plan must keep.** Authority only narrows going down. Each piece of the split:

1. is in the delegated piece's domain;
2. uses a factory that domain lists;
3. has paths that lie inside the delegated piece's paths (a piece that gives none gets its parent's); and
4. waits only for other pieces of the same split.

The split as a whole must also pass the checks a plan passes: the `pieces` limit counts the sub-pieces, a piece can't wait for itself through others, and a piece that is itself delegated must be within `delegation_depth`. When crew can't tell whether one path pattern lies inside another, it says no. A split that breaks a rule is refused whole: nothing of it is stored, the piece stays queued, and the refusal is recorded on the piece and as a `decision` event (`plan refused: …`). The reason is given back to the orchestrator on its next try. Each try, usable or not, counts as one of the piece's `orchestrator_calls_per_piece` (3 by default). A try that fails to run, or whose reply can't be read, is refused the same way. A reply that holds a question instead of pieces opens the `plan` wait at once with that question.

**Names.** crew names the sub-pieces `P2.1`, `P2.2` and so on, in the order of the reply, and rewrites the `waits_for` between them to match. A plan names no sub-piece: `crew mission plan` refuses an id with a `.`. Once crew has named them, they are in `mission.md` like any piece, and an amendment keeps them there under those ids; it can't add a new one (see "What an amendment can and can't do"). A sub-piece that is itself delegated has sub-pieces named `P2.1.1`, `P2.1.2`.

**What the sub-pieces are.** Ordinary pieces. They run through their factory in their own worktree, open a pull request, and are merged and accepted like any piece, so the auto-merge switch of the domain covers them: with it on, crew merges their pull requests; with it off, each waits for you in a `merge` wait. They count toward `runs_at_once` and `steps`, show in `crew mission status` under their own ids, and are kept apart from pieces whose paths overlap theirs. A sub-piece starts only when what its parent waited for is complete, and receives the deliverables of those pieces (see "Deliverables"); a piece that waits for a delegated piece receives the deliverables of the completed pieces under it, and nothing from one that was cancelled.

**When the delegated piece is finished.** It is `delegated` from the moment it is planned. It is complete when every piece directly under it is complete or cancelled and at least one is complete; a piece that waits for it starts then, which is when everything under it is finished. When all of them were cancelled, the delegated piece is cancelled too, and a piece that waited for it gets the question described under "What 'waiting on you' means". A delegated piece holds no run slot, has no worktree, branch or pull request, and holds no paths against other missions; its sub-pieces do, as any piece does.

**When a sub-piece is stuck.** A sub-piece that fails is put to the orchestrators in this order, nearest first: the one of the delegated piece directly above it (`scoped orchestrator P2`), the one of the piece above that, and so on up the tree, then the main orchestrator, and last you. A scoped orchestrator can retry the stuck piece, send it back to a station, cancel it, rewrite the brief of a piece under its own delegated piece that hasn't started, add a piece under it (crew names it, the add counts toward `orchestrator_additions`, and it can't be a delegated piece), or escalate. When one escalates, or gives a decision that can't be used, the question goes to the next one up. A call that fails to run (a harness timeout, say) is passed up the same way. Each ask counts toward the stuck piece's `orchestrator_calls_per_piece`, so one stop of a piece n levels down can use up to n+1 calls. When they are used up you get a `limit` wait with no further call, and the wait carries the question that was being passed up: `P2.1 hit the orchestrator call limit after a failed failure. scoped orchestrator P2 asked: Which layout does the billing page use?`. With the default of 3, a full climb from `P2.1` uses 2 calls, so its next stop gets one more ask, of `scoped orchestrator P2`, before it comes to you; a full climb from `P2.3.1` uses all 3, and every later stop of that piece comes straight to you. The events show who decided and who passed the question up:

```
  8  10:04:31  decision  P2.1  scoped orchestrator P2  escalate : the brief doesn't say which layout
  9  10:04:31  escalated  P2.1  scoped orchestrator P2  Which layout does the billing page use?
  10  10:04:58  exception  P2.1  failed
  11  10:04:58  decision  P2.1  orchestrator  escalate : the request doesn't say either
  12  10:04:58  escalated  P2.1  orchestrator  Which layout does the billing page use?
```

**The `plan` wait.** When the calls for planning are used up, status shows a `plan` wait on the delegated piece, with the last reason, or with the orchestrator's question. The piece waits (it is not queued while the wait is open) and the mission goes on with anything else it can. You can answer:

- `retry`: the piece goes back to the queue and crew tries again with a fresh set of calls, on the next tick. When the orchestrator asked a question, put your answer in `--note`: the next planning call is told its question (`you asked: …`) and your answer (`the principal answered: …`). Without a note, your `--words` are passed as the answer.
- `cancel`: the delegated piece is cancelled. A piece that waited for it gets a `question` as for any cancelled piece.

Or change the piece with `crew mission amend`: a new brief, or a `waits_for` naming a piece it needs first. An amendment clears the wait, gives the piece a fresh set of calls and puts it back in the queue.

**What an amendment can and can't do.** Write an amendment from the current `mission.md`, which holds the sub-pieces crew added: keep each one as it is, with its `P2.n` id, and never add an id with a `.`, since crew names every sub-piece. It can re-brief or change a sub-piece that hasn't started. Leaving out a sub-piece that isn't complete while naming others under the same delegated piece cancels it, whether or not it is running (its run is stopped). A plan that keeps a planned delegated piece and names no piece under it at all (one written from the plan as it was before the split, say) keeps all of them as they stand: crew logs `kept P2.1, P2.2 under P2: the amendment named no piece under it`. `crew mission amend` prints what the amendment will do as the mission stands now, one line per change, and then that it is queued. It doesn't ask first: read the lines, and send another amendment if one of them isn't what you meant. If the mission changes before the engine applies the amendment (a delegated piece is planned in between, say), the change log has what was done.

```
will re-brief P4
will change P4: waits_for changed
will add P5
will cancel P3.2 (running)
keeps P2.1, P2.2 under P2: the plan named no piece under it
queued; applied on the next tick
```

A planned delegated piece can't be edited or re-briefed. To drop a delegated piece before it is planned, remove it like any piece. Once it is planned, remove it and every piece under it; this works only while none of them is complete. Once one is complete, keep the delegated piece and what is finished, and remove the rest: the delegated piece then completes with what was done. Removing only the delegated piece is refused.

**`delegation_depth`** counts hops. A top-level delegated piece is depth 1, so `delegation_depth: 1` lets `P2` be delegated and its sub-pieces not. With `delegation_depth: 2`, one of `P2`'s sub-pieces may also be delegated: say `P2.3` has `delegate: true` in the split. Its own orchestrator then splits it when it is ready into `P2.3.1`, `P2.3.2`, kept inside `P2.3`'s paths. `0` switches delegation off: `plan` refuses `delegate: true`. A sub-plan that holds a delegated piece deeper than the limit is refused like any other rule breaker.

## Two missions in one repo

`runs_at_once` is shared: every mission in the repo draws on the same limit, counting only missions that are running with a live engine. Overlap is checked across missions too. A piece doesn't start while a piece of another mission with overlapping paths has started and is not complete or cancelled. A piece that has started is never held back, so two missions can't block each other for good.

**A mission that is not running.** A mission is not running when it is stopped, or when it is marked running but its engine has ended (after a reboot or a killed process, for example). Such a mission holds no run slots, but its pieces that started and haven't finished still hold their paths: their work is unmerged and comes back when the mission does. Only resuming it moves anything. No engine looks at its pull requests, so even one you merge on GitHub stays open in its state until you resume it. An amendment to it is only queued and is applied when it resumes, in its first tick, before crew starts or picks up any piece.

So for a piece kept behind such a mission, there are two ways out:

1. `crew mission resume` that mission, so it can finish the piece.
2. To drop the piece instead, cancel it with `crew mission amend` (a plan without it) and then resume the mission: the amendment is applied before anything runs. Cancelling a piece that has started stops its run if one is going, removes its worktree (one with uncommitted work is left for you, with a note), and leaves its pull request open: close it on GitHub yourself.

There is no command that abandons a stopped mission. To abandon one, cancel all of its unfinished pieces with `crew mission amend` and resume it: with every piece complete or cancelled, the mission is done.

When a piece is kept behind another mission's piece, `crew mission plan`, `start`, `go` and `resume` print a line for each piece that hasn't started, `P1 starts after P2 of mission m-...`, and `crew mission status` shows the same under "Queued" (see "Status"). When the mission in the way is not running, crew notifies you once:

```
P1 can't start: it overlaps P2 of mission m-20261121-101500-ab12, which is not running. Resume it with crew mission resume m-20261121-101500-ab12. To drop that piece instead, cancel it with crew mission amend and then resume: the amendment is applied before anything runs.
```

Two limits of the check:

- A piece with no paths isn't checked against another mission's pieces.
- Files you accepted outside a piece's paths (see below) don't count: only the piece's own paths do.

If crew can't read one of the repo's mission folders, it starts no new piece in any mission in that repo until the folder is fixed, and says so in a note.

## Files outside a piece's paths

When a piece's run is done and before crew opens its pull request, crew compares what the piece changed against the base branch with the piece's paths. Every changed file counts, deleted files too, and a moved file counts at both ends (the old name and the new). If a file is outside the paths, the piece doesn't get a pull request.

The orchestrator is asked what to do. It may send the piece back to a station with a note naming the files, cancel it, or hand the decision to you. It cannot accept the files: only you can. When it hands it to you (or can't settle it), you get a `paths` wait. `crew mission status` lists every file under it.

```sh
crew mission answer m-20261121-162953-e991 w2 --do accept --words "those files are fine"
```

- `accept` takes all of the files listed. The pull request opens, and crew notifies you that the files were accepted (`P3: 4 files outside its paths were accepted: README.md, go.mod, main.go and 1 more`), so an acceptance you didn't make doesn't go unseen. A file you accept stays accepted: if the piece is sent back later and changes more files outside its paths, crew asks only about the new ones.
- `goto --station build` sends the piece back to a station, with a note naming the files.
- `cancel` abandons the piece.

`accept` works for at most 200 files. A piece that changed more shows the first 200 and a line saying how many more there are, and takes only `goto` or `cancel`.

A piece with no paths isn't checked.

## Budgets

`limits` in `missions.yaml` cap what a mission may do. Six of them exist; here is what each does when reached.

| Limit | What it caps | When reached |
| --- | --- | --- |
| `runs_at_once` | Pieces running together across the repo's missions. | Queued pieces wait until one stops counting. |
| `pieces` | Pieces in a plan, sub-pieces included. | `plan` and `amend` refuse a plan with more, and a sub-plan that would pass it is refused. |
| `steps` | Station steps a mission has taken, over all its pieces. | The mission goes on hold (below). |
| `orchestrator_calls_per_piece` | Decisions the orchestrators make for one piece, and each try at planning a delegated piece. | The next failure of that piece becomes a `limit` wait for you (a `paths` wait for a piece stopped for files outside its paths), with no further call. A delegated piece that can't be planned gets a `plan` wait instead. The event `budget-hit` is recorded. |
| `orchestrator_additions` | Pieces the orchestrator adds on its own, whichever orchestrator adds them. An added piece can't be delegated. | An `add` is refused. |
| `delegation_depth` | How many levels a piece may be handed down, counted in hops (see "Delegation"). `0` switches delegation off. | `plan` and `amend` refuse a delegated piece that is deeper, and a sub-plan that holds one is refused. |

**On hold.** A station step is one station of a factory running. When a mission has taken as many steps as `limits.steps` allows, crew puts it on hold: it records a `budget-hit` event, tells you once, and `crew mission status` shows `On hold: 60 of 60 station steps used. Raise limits.steps above 60 in missions.yaml; the engine reads it again within a minute.`

On hold, crew starts no new piece, sends no piece back to a station, doesn't ask the orchestrator for decisions, and doesn't resolve a conflicted pull request (that can send a piece back to a station), because each of those takes more steps. Runs that are already going are not stopped: they carry on to the end of their run. A run that has lost its driver (the engine restarted, or the mission was stopped and resumed) is not picked up again until the hold lifts, and a running piece that fails is not put to the orchestrator until then. Everything that costs no step goes on: pull requests are opened and watched, merges are seen, complete pieces are cleaned up, answers you give are applied.

To lift the hold, raise `limits.steps` (or the repo's override under `repos:`) in `missions.yaml` above the steps used. The number used can keep rising while on hold, because runs that were already going finish their run: status always shows the current count, so raise the limit above that. A raise that is still too small changes nothing but the numbers: crew notes `step budget raised to 70, 75 used: still on hold` once and status shows the new limit. A mission that ends while on hold is done, and the hold goes with it. Nothing else lifts the hold, and nothing crew or an agent does changes the file. While a mission is on hold the engine reads `missions.yaml` again at the start of each tick (once a minute), so it sees the new number without a restart and notes that work goes on. If the file can't be read at that moment, it keeps the old limit and says so in a note.

## What "waiting on you" means

The engine does not guess at anything that needs a person. When it reaches such a point it parks the piece, records a wait with an id (`w1`, `w2`, ...), tells you with a desktop notification (macOS only for now) and shows it under "Waiting on you". If you don't answer, it reminds you at `remind_after`, then at twice that, then four times that, all measured from when the item started waiting (with 30 minutes: 30 minutes, 1 hour, 2 hours).

crew also notifies you, once, when the same failure has kept repeating for the `remind_after` interval (for five ticks in a row if `remind_after` isn't set): GitHub logged out, a push refused by permissions or a hook, a mission file crew can't read. The notification names the mission, the piece if there is one, and the error. A failure that clears and comes back is told again. A changed repo visibility stops the mission until you confirm again (see "Confirming the repo").

There are eight kinds:

| Kind | What it means | What you can answer |
| --- | --- | --- |
| `merge` | The piece's pull request is open and crew did not merge it. The text of the item says why (see "Auto-merge"). | Nothing to type. Merge it on GitHub, or fix what the text names. crew sees the merge and moves on. `cancel` abandons the piece. |
| `approval` | A station in the piece's factory is set to pause for approval (`approve: true`) and has finished. | `approve` or `cancel`. `approve` takes an optional `--station` to continue from a station other than the next one. |
| `question` | The orchestrator couldn't settle a failure, or you must decide something. | `retry`, `goto` or `cancel`. On a conflict or failed-checks question, `retry` gives the piece two more tries. |
| `limit` | A piece used up its orchestrator calls. | `retry`, `goto` or `cancel`. |
| `pr-closed` | The pull request was closed without being merged. | `retry` (after you reopen it, crew goes back to watching it) or `cancel`. |
| `paths` | A piece changed files outside its paths and crew stopped it before opening a pull request (see "Files outside a piece's paths"). | `accept`, `goto` (with `--station`) or `cancel`. |
| `deliverable` | A piece posted a deliverable and nobody has accepted it (see "Deliverables"). | `accept`, `goto` (with `--station`) or `cancel`. A piece that also has a pull request takes only `accept` or `cancel`. Moving its issue to Done on the Jira board also accepts it. |
| `plan` | A delegated piece could not be planned in its orchestrator calls, or its orchestrator asked a question first (see "Delegation"). | `retry` or `cancel`. On `retry`, `--note` carries your answer to the orchestrator's question (your `--words` when there is no note). Changing the piece with `amend` also clears it. |

A `question` about a piece that hasn't started yet takes only `cancel`. This happens when a queued piece waits for a piece that was cancelled: the piece can never start, so either cancel it too or change the mission with `amend` (see below), for example by dropping the dependency. Amending the piece clears the question and puts it back in the queue.

To answer, pass the wait's id and what you said. `--words` is required: it records your answer word for word.

```sh
crew mission answer m-20261121-162953-e991 w2 --do goto --station build \
  --note "the plan missed the migration" --words "send it back to build, it missed the migration"
```

`--do goto` needs `--station`. `--note` passes a message to the station. `--words` and `--note` each take text or `@FILE`. If the choice doesn't fit the wait, the error lists the ones that do.

A command that changes a mission while the engine is running doesn't edit the mission itself. It leaves the answer in the mission's `inbox/`, and the engine applies it on its next tick (once a minute). If the engine isn't running, the answer is applied when you resume.

## Auto-merge

By default crew merges nothing: every pull request waits for you in a `merge` item. Auto-merge lets crew merge a pull request on its own. You switch it on per domain, and the switch is your approval.

```sh
crew automerge on --domain design
crew automerge on --all
crew automerge on --domain design --without-checks
crew automerge off --domain design
crew automerge status
```

`on` and `off` need a terminal, and you must run them yourself: the orchestrator is told never to, and the session rules deny the command. A domain is in one of three modes:

- `off`: crew leaves the domain's pull requests for you. This is the default, and it is what crew uses for any domain with no switch.
- `on`: crew merges a pull request once at least one check has passed and none has failed or is still running, and the checks have all been green for a minute.
- `without-checks`: crew merges as soon as the pull request can be merged, without waiting for any check.

Setting a switch shows what you are about to allow and asks you to type a phrase:

```
Repo:     acme/app (private)
Domains:  design
Mode:     on. crew merges a pull request of this domain into main once it has at least one check and every check has passed.

If merging to main deploys this repo, this turns on automatic deploys.

Type "acme/app design" to confirm:
```

With `--all` you type `acme/app all`. The repo must be confirmed first (see "Confirming the repo"). `on` without `--without-checks` is refused when the default branch has no checks and the repo has no workflow in `.github/workflows`, because nothing would ever pass; the error says so and names `--without-checks`.

A piece follows the switch of its own domain. A pull request that changes files inside another domain's `paths` needs that domain's switch on too; if it is off, the pull request waits for you and the item names that domain. A domain that lists no `paths` is never counted as touched, so its switch can't hold back another domain's pull request; when you run `crew automerge on`, crew prints a paragraph naming the other domains that list no paths, so you know the rule can't protect them. Add `paths:` to their domain files to change that. crew skips the checks only if the piece's domain and every domain the pull request touches are `without-checks`; if any of them is `on`, crew waits for checks.

crew reads the switch again right before it merges, so turning a domain off stops merges at once. crew merges only the head commit it pushed itself, and the merge fails if the pull request has moved on.

### Failed checks and conflicts

With auto-merge `on` or `off`, a pull request whose checks fail on crew's own head goes back to the piece's factory with the names of the failed checks, and the end of the log of the first few. The piece fixes it, commits and crew pushes to the same pull request. Under `without-checks` crew doesn't look at the checks. A pull request that conflicts with the base branch first gets the base merged into the branch; if that conflicts, the piece goes back to the factory to resolve it. A send-back for failed checks costs a try, and so does a send-back for a conflict; a base merge that goes through cleanly costs nothing. A piece has two tries of each kind. After that the orchestrator decides, and if it can't, the item comes to you as a `question`. Your `retry` answer gives the piece two more tries.

This happens with auto-merge off too: crew sends a piece back for failed checks and merges the base into a conflicting branch whether or not it will merge the pull request itself. That is new compared with the first missions version.

### Why a pull request can wait

A pull request waits for you, in a `merge` item, a `question` or a `pr-closed` item, for one of these reasons. The text of the item says which, and `crew mission status` shows it beside the link:

1. Auto-merge is off for the piece's own domain, which is the default. The pull request waits for you to merge it.
2. Another domain it touches has auto-merge off.
3. The branch has commits crew didn't push. crew never merges a head it didn't push. This is reported after 2 minutes.
4. The pull request was opened before crew recorded what it pushed, so crew won't merge it.
5. The pull request has no checks. This is reported after 2 minutes.
6. Checks are still running 30 minutes after crew pushed the head. crew still merges when they pass.
7. GitHub hasn't said whether the pull request can be merged after 30 minutes.
8. GitHub refused the merge. crew keeps trying, and completes the piece when a try goes through or you merge it yourself.
9. GitHub reports a conflict that crew can't find after 30 minutes: the base merges into the branch cleanly, but GitHub still says it conflicts.
10. Failed checks or a conflict that two tries didn't fix, and the orchestrator couldn't settle either.

A pull request that was closed without being merged is a `pr-closed` item.

If crew can't read the switch, or `automerge.json` doesn't match its checksum, every domain is off and the engine says so in a note.

### How crew merges

crew merges with the method in `merge_method` in `missions.yaml`: `squash` (the default), `merge` or `rebase`. If the repo doesn't allow that method, the merge fails, crew notes why, and the pull request waits until you change `merge_method` or merge it by hand.

If GitHub accepts the merge without merging at once (a merge queue, or GitHub's own auto-merge), crew notes that, waits, and completes the piece when the pull request is merged.

Merging is deploying where the repo makes it so. If a merge to the base branch deploys, a pull request crew merges is deployed with nobody watching. Switch auto-merge on for the domains whose checks you trust.

## When a station fails

A run can end badly in three ways, and crew handles them in this order.

1. **The run failed in a way that can be retried** (it was interrupted, say). The engine resumes the run where it stopped, at most twice per piece.
2. **The run finished but left uncommitted files.** The engine sends the piece back once, to the factory's rework station (its `rework` station, or its start station when none is set), with a note listing the files. If files are uncommitted again when it finishes, that counts as a failure like the next one.
3. **The run failed in a way that can't be retried, a route hit its `max`, or the two resumes or the one send-back didn't help.** The engine asks the orchestrator for one decision in a headless call. The orchestrator picks `retry`, `goto`, `cancel`, `rebrief`, `add` or `escalate`. A decision that can't be used, or that repeats an action it already took for the same kind of stop, comes to you as a `question`, and so does `escalate`.

Each piece may use `orchestrator_calls_per_piece` such calls (3 by default). When they are used up, the next failure becomes a `limit` wait for you without another call.

For a sub-piece (see "Delegation") the decision goes first to the orchestrator of the delegated piece above it, then to the one above that, and so on up the tree, then to the main orchestrator, and only then to you. Every one of those asks counts toward the sub-piece's calls. An orchestrator that escalates, or gives a decision that can't be used, passes the question one level up.

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

What an amendment may do: add pieces (not sub-pieces: crew names those, and an amendment keeps the ones in the current plan; see "Delegation"), change a queued piece (its brief, its domain, its factory, its paths, what it waits for), and remove any piece that isn't complete, which cancels it. `crew mission amend` prints what the amendment will cancel, re-brief, change and add as the mission stands now, and queues it. Every change goes in the mission's change log with your words.

## Stop and resume

`crew mission stop <id>` applies to a running mission. It tells the engine to shut down, which interrupts the running pieces' runs where they are. Worktrees and branches stay in place. A mission that hasn't started has nothing to stop, and one that is already stopped or done is left alone.

`crew mission resume <id>` continues a stopped mission, and also a mission marked running whose engine has died. It needs a mission that has been started, which is to say one with an `approved.md` (written by `go`, or by `start` when approval is off); a planned mission needs `start` instead. It re-runs the start checks, marks the mission running and starts the engine again, which picks up each piece where it was. A mission whose engine is still live is refused (`mission <id> is being driven (pid <n>)`) before anything is checked or written. Amendments and answers queued while the mission was stopped are applied in the engine's first tick, before any piece starts or is picked up again.

If the engine process crashes, a supervisor restarts it up to three times and records each restart as an event. After that it marks the mission stopped and notifies you. A reboot also ends the engine; run `resume` afterwards.

## Jira

If your team plans on a Jira board, crew can show a mission there. It is off until you set it up, and a repo with no entry in `jira.yaml` behaves exactly as before.

### What shows on the board

crew writes to Jira. The orchestrator and the stations don't: no agent is asked to touch it, and the orchestrator is told never to run `crew jira setup`.

- **An epic** for the mission. Its summary is the first line of your request, and its description holds the whole request. It sits in To Do until the mission starts, in In Progress while the mission runs or is stopped, and in Done when the mission is done.
- **An issue for each piece**, with the piece's id and the first line of its brief as the summary and the whole brief in the description. The issue is a child of the epic. A sub-piece is an issue under the epic too, not under its parent's issue: its id (`P3.1`) and a `Part of: P3` line in the description say where it belongs, and crew creates it when the delegated piece is planned. It carries a label such as `crew-<mission-id>-P1`, and it is assigned to the account in `assignee`, or to the account your token belongs to.
- **Four lanes.** A piece waiting in the queue is in To Do. A piece that is running, or stopped at a question or an approval, is in In Progress, and so is a delegated piece once it has been planned, until it is complete or cancelled. A piece whose pull request is open, or whose deliverable is posted, is in In Review. A piece that is complete or cancelled is in Done. crew moves the issue by the board's own transitions, so a workflow that doesn't allow a move shows up as a note.
- **A flag** on the issue while the piece is waiting on you. It comes off when the wait ends.
- **Comments** on the issue for the things that matter, in plain words: the pull request was opened (with its link), a decision was made and by whom, the piece was escalated to you (or, for a question a scoped orchestrator passes up to the one above, `Passed up by scoped orchestrator P2: …`), it is waiting on you, its brief was changed, it was cancelled, a deliverable was posted or accepted. A change you make to the plan with `amend` is a comment on the epic.
- **Attachments**: the files of a deliverable (see "Deliverables").

That is all crew sends to Jira: your request and the briefs (in the epic and the issues), the comments above, which carry text written by others by design (a station's summary when it asks for your approval, the orchestrator's reasons for a decision, the words you passed with an answer or an amendment), and the deliverable files. It sends no other file of the repo, no run logs and no credentials. A description longer than 30,000 characters is cut, with a line that says the full text is in the mission folder.

### Setting it up

You need a Jira Cloud site (`https://<name>.atlassian.net`; other hosts are refused), a project on it, and an API token. Create the token at `id.atlassian.com`, under Security, API tokens. crew reads the token and your account email only from the environment:

```sh
export CREW_JIRA_EMAIL=you@example.com
export CREW_JIRA_TOKEN=...
```

Then, inside the repo:

```sh
crew jira setup --site https://example.atlassian.net --key ACME
```

Add `--board N` to use the sprints of board `N`, and `--assignee ACCOUNT_ID` to assign issues to someone other than the token's owner. Without `--board`, crew uses the project's only board when there is exactly one; with none or several it says so and doesn't use sprints. A board that has no sprints at all, such as a Kanban board, is fine: the check prints `board: <n> has no sprints: issues are not put in a sprint`, and setup saves `board: 0`. `crew jira check` prints the same line for an entry whose board has no sprints.

`crew jira setup` needs a terminal. With none, it refuses before it contacts Jira and sends nothing. Otherwise it makes a read-only check against the live site and prints what it found: the account the token belongs to, whether the project exists, whether its issue types have the four statuses, which board and active sprint it will use, and whether the Flagged field is there (without it, a waiting piece gets a comment but no flag). If anything is wrong it stops and writes nothing. If all is well, it then asks you to type `<owner/name> <KEY>` (for example `acme/app ACME`) in a terminal, and only then writes the entry to `jira.yaml` in your crew directory. The phrase can't be answered by a pipe, so a session can't set Jira up for you. New crew directories deny `crew jira setup` to sessions as well. In a crew directory that already existed, the deny list is as you left it, and the typed phrase in a terminal is what stops a session.

All three `crew jira` commands take `--dir` (the crew directory) and `--project` (the repo; default: the current directory). `crew jira check` repeats the check for the repo's current entry. Run it after you change the project's workflow or the entry:

```sh
crew jira check
```

`crew mission start`, `go` and `resume` refuse when the repo has an entry but the two variables aren't set in the shell that runs them: `Jira is set up for <repo>, but CREW_JIRA_TOKEN is not set here. Run this command yourself in a terminal where CREW_JIRA_EMAIL and CREW_JIRA_TOKEN are set.`

The engine is started by whoever runs `crew mission go` or `crew mission resume`, and it reads the two variables from that shell. If the orchestrator runs those commands, the shell the orchestrator session runs in must hold the token, and that session can read it. To keep the token away from every session, don't export it in the shell you start sessions from, and run `go` and `resume` yourself in a terminal where it is set. crew tells the orchestrator to hand you the command when that is the case: when one of these commands is refused for the missing variables, the orchestrator doesn't try to set them and doesn't touch `jira.yaml`; it gives you the command to run.

### jira.yaml

`crew jira setup` writes `jira.yaml` in your crew directory, one entry per repo, with `version: 1`. You can edit it. Unknown keys are errors.

```yaml
version: 1
repos:
  acme/app:
    site: https://example.atlassian.net
    project: ACME
    board: 3            # 0 or left out: issues are not put in a sprint
    assignee: ""        # an account id; empty means the account the token belongs to
    issue_type: Task
    epic_type: Epic
    statuses:           # change these if your workflow names them differently
      to_do: To Do
      in_progress: In Progress
      in_review: In Review
      done: Done
```

Credentials are never in this file, in `state.json`, in events or in notes.

### Sprints

With a `board`, crew puts each new issue in the board's active sprint. crew never starts or ends a sprint. When the board has no active sprint, crew says so in a note and tries again on every tick while the mission runs, so the issues join the sprint you start later. This is not counted as writes not sent. Once the mission has ended, crew no longer tries by itself: issues are placed in a sprint you started afterwards only when you run `crew jira sync <mission>`.

### What crew reads back

One thing. When a piece has posted a deliverable that you haven't accepted, and crew itself has moved its issue to In Review, crew looks at that issue now and then, and if it is in Done, it takes that as your acceptance (see "Deliverables"). crew reads Done only after a move to In Review that crew made itself and Jira accepted, since the deliverable was last collected. When Jira refuses that move, or the piece is reworked and its files are collected again, an issue already in Done is not taken as your acceptance until crew has moved it to In Review again; answer the wait instead. Nothing else you do on the board changes the mission: moving other issues, closing the epic or editing a summary are left alone. crew doesn't move an issue back at once: it moves it only when the piece next changes lane, or after a deliverable is collected again, and then to where the mission says it belongs.

### When Jira is down

Jira never stops a piece. A failed write is a note in the mission's events, and crew plans it again on the next tick, so the board catches up once Jira is back.

When Jira can't be reached at all (the network fails, a call times out, the site rejects the credentials, or it answers 408, 429 or a 5xx), crew notes it once and sends nothing more that tick. When one write fails by itself, for example a 403 on one kind of call, crew notes it, skips that write for the tick and sends the rest; the skipped write is tried again on the next tick. A few writes hold up others: while the epic can't be created nothing else is sent, while a piece's issue can't be created that piece's other writes wait, and a comment that fails holds the comments after it, so they stay in order. A write Jira refuses for good, such as a move the workflow doesn't allow, is noted once and not tried again, except creating an issue and placing issues in a sprint, which are tried again on every pass.

An attachment upload has its own time limit, 30 seconds plus one second per 100 KB. An upload that fails while Jira can't be reached ends the tick like any other write and doesn't count as a try. Any other failed upload, a timeout included, counts as one and doesn't hold up the other writes. The running engine tries each file three times. After the third failure it notes `Jira: <file> could not be attached after 3 tries; crew jira sync <mission> tries again` and stops trying that file. The file is still owed, so it counts in `writes not sent yet`, and `crew jira sync <mission>` tries it again. A file crew can never send, because it is over 10 MB or vanished, is noted and given up at once. Whatever happens on the board, the file is always in the mission folder.

`crew mission status <id>` shows `(<n> writes not sent yet)` on its Jira line while the board is behind, or `Jira: nothing has reached the board yet` while there is no epic. After the mission has ended with writes still unsent, the engine tries 10 more times, a minute apart, and then leaves a note: `Jira is behind: <n> writes were not sent; run crew jira sync <mission>` (`1 write was not sent` for one, and `Jira is behind: nothing reached the board; run crew jira sync <mission>` when the epic was never created). It says to run:

```sh
crew jira sync <mission>
```

`crew jira sync` sends what is owed, once, and prints `Jira is up to date for mission <id>`, or `<n> writes still not sent; see the notes in crew mission status <id>` (`1 write` when it is one). It refuses while the mission's engine is alive, since the engine sends the writes itself. It refuses a mission that hasn't started (planned, or waiting for `go`): `mission <id> hasn't started: there is nothing to send`. It also refuses when the repo is set up for Jira but `CREW_JIRA_EMAIL` or `CREW_JIRA_TOKEN` isn't set in that shell: `Jira is set up for <repo>, but CREW_JIRA_TOKEN is not set here. Run this command yourself in a terminal where CREW_JIRA_EMAIL and CREW_JIRA_TOKEN are set.`

### Your token and the sessions crew starts

crew sends the token only to your `https://<name>.atlassian.net` site, and never follows a redirect with it. crew removes `CREW_JIRA_EMAIL` and `CREW_JIRA_TOKEN` from the environment of every station and orchestrator process it starts, and of the `git`, `gh` and notification commands it runs itself, so a hook or `core.fsmonitor` setting a station leaves in its worktree can't read them either. Only the engine keeps them. But a shell that exports the token lets any session started from that shell read it, for example an orchestrator you launch yourself from the same terminal. If that matters to you, export the variables only in the shell that runs `crew jira` and the engine, or give the token to a Jira account with narrow rights.

## Deliverables

A piece doesn't have to end as a pull request. A piece that does research, writes a brief or drafts a plan ends with files, and those files are its deliverable. A piece may end in a deliverable instead of a pull request, or as well as one. Say so in the piece's brief, so the station knows to write one.

### Where the files go

The station writes them to `.crew/deliverables/` in its worktree. When the run ends, crew collects them:

- crew copies them to `.crew/missions/<id>/deliverables/<piece>/` in the repo. That copy is the one crew keeps and hands on. The worktree's copy is scratch.
- A deliverable is at most 20 files, regular files only. A file past the twentieth is left out, with a note. Symlinks are skipped, and so is a file with more than one hard link (it could be any file the station can read), a file whose name holds control or invisible formatting characters, and a file whose path is over 255 bytes. If `.crew/deliverables` (or `.crew` itself) in the worktree is not a real folder, crew ignores it with a note and the piece goes on as if it left no deliverable.
- When Jira is set up, crew attaches each file to the piece's issue. A file over 10 MB can't be attached: crew notes it and the file stays in the mission folder. A file whose upload fails three times also stays there, and it is still owed to Jira: `crew jira sync <mission>` tries it again. Attachments are the only thing crew puts in Jira from the files, and no file content is in `state.json`.
- Without Jira, nothing else changes: the files stay in the mission folder, and you read them there.

A piece that ends in a deliverable and no pull request has no pull request to merge, so its piece is "delivered" and waits for you to accept the deliverable. A piece that ends in both opens its pull request as usual. After the pull request is merged, the piece is delivered and waits for you to accept the deliverable as well.

crew keeps its own files (missions, runs, worktrees) in `.crew` in your project. If `.crew` in the project is a symlink, or `.crew/.gitignore` is not a regular file, crew refuses to run there and says so, because it would otherwise write through the link. This is about the project's `.crew`; a worktree's `.crew/deliverables` that isn't a real folder is only ignored, as described above.

### How later pieces get them

A piece that lists the deliverable's piece in `waits_for` gets the files in its own worktree, in `.crew/inputs/<piece>/`, and its first station's prompt lists them. Only a complete piece's files are handed on: a piece that waits for a delegated piece gets the files of the completed pieces under it, and nothing from a cancelled one. Name the piece in `waits_for` when a piece needs another one's brief; otherwise it never sees the files.

### Accepting one

A deliverable waits until it is accepted. Three things accept it:

1. **You move its issue to Done on the board.** This is the one thing crew reads back (see "Jira"). It works only when Jira is set up and, since the files were collected, crew has moved that issue to In Review and Jira accepted the move.
2. **You answer the wait with accept:**

   ```sh
   crew mission answer <id> <wait> --do accept --words "the brief is what I wanted"
   ```

3. **Auto-merge is on for the piece's domain.** Then crew accepts the deliverable at once and opens no wait. `crew mission status` shows who accepted it: `accepted by engine` or `accepted by principal`.

Accepting completes the piece, and pieces that wait for it can start.

To send one back, answer with `goto` and a station:

```sh
crew mission answer <id> <wait> --do goto --station build --note "add the cost estimate" --words "send it back to build"
```

The piece runs again from that station, and the old deliverable is cleared. When it finishes, crew collects the files again, and what it collects waits for your acceptance. Files collected after a rework are a new deliverable and must be accepted again: accepting one does not carry over to different files. A deliverable that comes with a pull request can't be sent back with `goto`; answer `accept` or `cancel`, and use the pull request to ask for changes.

## Where things live

Everything a mission writes goes in the repo it works on, under `.crew/`. crew writes a `.gitignore` there so none of it reaches git.

```
<repo>/.crew/
├── missions/<mission-id>/
│   ├── mission.md        the plan and your request, with a change log
│   ├── approved.md       the plan as it was when the mission started
│   ├── state.json        where each piece is
│   ├── events.jsonl      what happened, one line per event
│   ├── deliverables/<piece>/  files a piece left (see "Deliverables")
│   ├── inbox/            answers and amendments waiting for the engine
│   └── mission.log       the supervisor's and engine's output
├── runs/<run-id>/        each piece's run, as for crew run
└── worktrees/<mission-id>-<piece-id>/
```

A mission id looks like `m-20261121-162953-e991`. A piece's branch is `crew/<mission-id>/<piece-id>`, created from the base branch on `origin`.

A piece's run belongs to its mission. You can read it with `crew run status`, which shows an `owner:` line, but `crew run say`, `goto`, `approve` and `resume` refuse it: `run <id> belongs to mission <mission-id> piece <piece-id>: crew run <verb> is refused for it; use crew mission status, answer, amend, stop or resume`.

The engine is the only writer of `state.json` and `mission.md` while it runs. The other commands leave you a file in `inbox/`, or change the state only when no engine is running.

When a piece's pull request is merged, crew removes the piece's worktree. When a piece finishes with nothing to commit, it is complete at once and no pull request is opened. Nothing is ever deleted on GitHub: crew does not delete remote branches or close pull requests. It merges a pull request only when auto-merge is on for the piece's domain. The local piece branches stay where they are.

## What the safeguards do and don't cover

Agent sessions run as your own Unix user, with your GitHub login. No file, checksum or terminal prompt can stop a process that sets out to get around it. So the safeguards protect against mistakes, drift and a misread prompt. They are not a boundary against a process that deliberately works around them.

What crew promises is this: crew itself never merges a pull request unless you switched auto-merge on for its domain, never closes or releases anything without your approval, and every session crew starts is launched with rules that refuse the commands that would. Three layers do the work.

1. **Only crew acts.** No agent prompt asks an agent to merge or release. The only code that merges is the engine's merge step, and it merges only a head commit crew pushed itself, after reading the switch again.
2. **Session rules.** Every session crew starts, whether a station or an orchestrator call, gets the deny rules from `harnesses.yaml`: merging or closing a pull request, force-pushing, pushing to `main`, `crew automerge`, `crew repo confirm`, `crew release approve`, and writing anywhere in your crew directory. Stations and headless orchestrator calls also can't run `crew run` or `crew mission`, so a station can't pass an approval stop or answer for you. A harness that can't apply deny rules can't run mission pieces, and `crew verify` says which ones.
3. **Commands only you can run.** `crew repo confirm` and `crew automerge` need a terminal and a typed confirmation, with no flag and no piped input that skips it. The switch that `crew automerge` writes is your approval to merge.

The limits, specifically:

- **Deny rules match command prefixes.** A command written another way, such as other flags first or a different spelling of the same push, isn't caught.
- **The rule protecting the crew directory covers the file-editing tools.** A write made through a shell command isn't caught.
- **"Pushing to main" is matched literally** (`git push origin main`, `git push origin HEAD:main`). If the repo's default branch has another name, add rules for it in `harnesses.yaml`.
- **The mission's own folder in the repo (`state.json`, `inbox/`) and `.crew/runs/` are not protected.** "A station can't answer for you" is true of the `crew` commands; a file written into `inbox/` by another route would be applied as your answer.
- **Where the confirmation is read from depends on which crew directory the command resolves.** Confirmations are stored under `<crew dir>/principal/`, so `--dir`, `CREW_HOME`, or a `crew.yaml` in the current directory choose which record is read.
- **The checksum on the confirmation file catches hand edits and accidents only.** It isn't keyed, so someone who understands it can rewrite the file and the checksum together.
- **A pseudo-terminal can fake the terminal.** That is why the session rules also deny `crew repo confirm` and `crew automerge`.
- **The auto-merge file has a checksum, nothing more.** `automerge.json` in `<crew dir>/principal/` is sealed like the repo confirmation. A file that has been edited by hand, or has no checksum, reads as off for every domain.
- **The orchestrator session is yours.** crew can only apply the deny rules to it if you start it from the printed launch line.
- **Answers relayed by the orchestrator are recorded word for word, but nothing proves you said them.**
- **crew pushes whatever stations commit.** It doesn't scan for secrets or stray files.
- **Checks are only as good as the repo's CI.** The factory's review station is the only thing between an agent's work and your review of the pull request.
- **Pushing publishes in a public repo.** After you confirm a repo once, every piece branch in a public repo is public.
- **Merging is deploying, where your repo makes it so.** If a merge to `main` triggers a deploy, a merge by crew is a deploy.

A real boundary would need agents to run under a separate identity, such as another OS user or a GitHub account that can't merge. This stage doesn't do that.
