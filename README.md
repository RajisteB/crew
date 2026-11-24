# crew

crew is a command-line tool for running AI coding agents as a team. It does three things, and each builds on the one before:

1. **One set of agents and skills for every coding tool.** You write each agent and skill once, as plain Markdown. `crew compile` sets them up in Claude Code, OpenCode, Codex, Gemini CLI and Cursor.
2. **Factories.** A factory chains agents into a production line: plan, build, review. Work moves forward when a station passes it and back when it isn't ready. `crew run` drives one from start to finish.
3. **Missions.** A mission is a job too big for one factory run. It is split into pieces; each piece runs through a factory in its own git worktree and ends as one pull request. You merge the pull requests, or you let crew merge them.

You steer all of it by talking to one agent, the **orchestrator**, or by typing the `crew` commands yourself.

## Contents

- [What you need](#what-you-need)
- [Install](#install)
- [Quick start](#quick-start)
- [The crew directory](#the-crew-directory)
- [Agents and skills](#agents-and-skills)
- [Factories](#factories)
- [The orchestrator](#the-orchestrator)
- [Domains](#domains)
- [Missions](#missions)
- [Command reference](#command-reference)
- [Strict verification](#strict-verification)
- [Safety](#safety)
- [Upgrading an older setup](#upgrading-an-older-setup)
- [Adding a harness](#adding-a-harness)
- [Facts to double-check before trusting them](#facts-to-double-check-before-trusting-them)

## What you need

- **Go 1.22 or later**, to build crew.
- **At least one coding tool** ("harness") that crew supports. Claude Code is the one that can do everything here. The others get your agents and skills; see the table below.
- **For missions:** `git`, and the GitHub CLI `gh`, logged in to an account that can push to the repo.

| Harness | Agents and skills | Runs factory stations | Hosts the orchestrator | Runs mission pieces |
| --- | --- | --- | --- | --- |
| Claude Code | yes | yes | yes | yes |
| OpenCode | yes | yes (not yet tested live) | no | no |
| Codex, Gemini CLI, Cursor | yes | only with a `headless:` command you add | no | no |

Mission pieces need a harness that can apply deny rules to the sessions crew starts. `crew verify -v` says which of your harnesses can't.

## Install

```sh
git clone https://github.com/RajisteB/crew.git
cd crew
go install ./cmd/crew
```

This puts `crew` in `$(go env GOPATH)/bin`, usually `~/go/bin`. Make sure that folder is on your `PATH`. To update later, `git pull` and run `go install ./cmd/crew` again, then `crew compile`.

Install from a clone: `go install <url>@latest` doesn't work, because the module's name (`github.com/crewfile-dev/crew`) is not the repository's address.

## Quick start

```sh
crew init                 # scaffold ~/crew (or --dir somewhere else)
cd ~/crew
crew verify               # strict check of everything you wrote
crew plan                 # per harness: agent → model, effort, price, and the files a compile would change
crew compile              # set up your harnesses
```

`crew init` gives you a working set: a few agents, a few skills and one factory, `ship-change`. Try the factory in any git repository:

```sh
cd ~/src/my-project
crew run ship-change --input "Add a /health endpoint"
```

Or start the orchestrator there and tell it what you want in plain words:

```sh
crew orchestrator
```

For missions, you need two more things: a [domain](#domains), and a one-time `crew repo confirm` in the repo. [Missions](#missions) has the steps.

## The crew directory

Everything you write lives in one folder, `~/crew` by default.

```
~/crew/
├── crew.yaml                 your choices: which harnesses are on, which agents each gets
├── harnesses.yaml            facts about each harness: paths, field names, tool names, deny rules
├── models.yaml               every model and effort each harness can run, with prices
│                             (written by `crew models sync`; don't edit by hand)
├── missions.yaml             settings for missions: approval, limits, merge method
├── agents/                   one file per agent
│   ├── ticket-worker.md
│   ├── change-reviewer.md
│   └── code-explorer.md
├── skills/                   one folder per skill
│   └── pr-description/
│       ├── SKILL.md
│       └── scripts/ …        any supporting files come along
├── factories/                one file per factory
│   └── ship-change.md
├── domains/                  one file per domain (used by missions)
│   └── web.md
└── build/                    generated agent files and factory diagrams (gitignored)
```

crew finds the directory in this order: `--dir`, then `$CREW_HOME`, then the current directory if it has a `crew.yaml`, then `~/crew`.

**Every harness points back at this folder.** Each skill in a harness is a link to `skills/<name>/`, so a skill edit is live everywhere with no compile. Agents need different fields in each harness, so `compile` generates each harness's version under `build/<harness>/` and links the harness's agent file to it; an agent edit needs a compile.

Per repo instead of per user (this writes real copies, since a repo can't commit links into your home directory):

```sh
crew compile --scope project --project ~/src/payroll-service
```

To see exactly what would be written before touching your real dot-dirs:

```sh
crew compile --dry-run
crew compile --out /tmp/crew-preview
```

## Agents and skills

An agent is one Markdown file in `agents/`:

```markdown
---
name: ticket-worker                 # must equal the filename
description: Implements one scoped ticket end to end. Use when ...
model: claude-opus-5-5              # an id from models.yaml, or "inherit" (follow the session)
effort: high                        # optional; must be one this model accepts
per_harness:                        # optional; a different pick where the model above isn't available
  opencode: {model: opencode-go/kimi-k3.1-pro, effort: high}
capabilities: [read, search, edit, write, shell]   # or [all] to inherit everything
skills: [go-backend-conventions]    # optional; preloaded where the harness supports it
---
The system prompt.
```

A skill is a folder in `skills/` with a `SKILL.md`. The folder name is the skill name, and any other files in the folder are part of the skill:

```markdown
---
name: pr-description
description: Writes a PR description reviewers can approve quickly. Use when opening a PR.
---
The instructions.
```

For agents, those are the only keys allowed: anything harness-specific is rejected by `verify` and belongs in `harnesses.yaml`. Skills are linked as they are, so a `SKILL.md` may carry other keys (`license`, `allowed-tools`, `metadata`, …); crew checks only `name` and `description`.

`crew.yaml` says which harnesses are on and which agents each one gets (names or globs; `"*"` is all). Skills are global: every enabled harness gets every skill.

### How the agnostic parts map

**Model and effort, per agent.** Each agent names its model and effort directly, using the ids in `models.yaml`. Ids are spelled the way the harness spells them: `claude-opus-5-5` for Claude Code, `opencode-go/kimi-k3.1-pro` for OpenCode. Claude Code only runs Claude models, so an agent that also goes to OpenCode needs a `per_harness.opencode` pick; `verify` tells you when one is missing. Giving `per_harness` a new model drops the top-level effort (it was chosen for a different model), so set the effort there too.

How it's written: Claude Code gets `model:` and `effort:`; Codex gets `model` and `model_reasoning_effort`; OpenCode gets the effort as a model variant (`model: opencode-go/kimi-k3.1-pro#high`); Gemini CLI has no per-agent effort, so it's dropped; Cursor has no model list, so agents use `inherit`.

**The model list.** `crew models sync` downloads [models.dev](https://models.dev), the catalog OpenCode itself uses, and writes `models.yaml`: for each harness, every non-deprecated model from the providers in its `catalog:` block, with the effort levels that model accepts, its context size and its price. `crew models` prints the list for your enabled harnesses, and `verify` checks each agent against it.

**Capabilities become native tools.** `read, search, edit, write, shell, web, delegate` become `Read, Grep, Glob, …` in Claude Code, a `tools:` true/false map in OpenCode, a tool list in Gemini CLI, `sandbox_mode` in Codex and `readonly` in Cursor. `crew capabilities` shows the whole table. A capability a harness has no tool for is dropped for that harness, and `plan` and `verify -v` note each one.

**Skills are shared, not copied twice.** When one harness already reads another's skill directory (OpenCode loads `~/.claude/skills`), `shares_with` stops a duplicate being written.

## Factories

A factory chains agents into a production line. Each **station** runs one agent. The agent's result picks a **route**, which sends the work forward, or back to any earlier station.

```markdown
---
name: ship-change
description: Plans, builds and reviews one change in the current repo, sending work back when it isn't ready.
start: plan
harness: claude-code
stations:
  plan:
    agent: epic-planner
    routes:
      ready: build
  build:
    agent: ticket-worker
    on_drift: plan              # files changed outside the run since the last station: re-plan first
    routes:
      done: review
      replan: {to: plan, max: 2}
  review:
    agent: change-reviewer
    approve: true               # pause after review so you can accept or redirect
    routes:
      pass: done
      fix: {to: build, max: "*"}
      redesign: {to: plan, max: 1}
---
This factory takes one change request (the run input) to a reviewed result in the current repository.

- plan: say exactly which files change and how. If anything is ambiguous, make the call and record it in notes as an assumption.
- build: implement exactly the plan. Never ask questions; if the plan is incomplete or wrong, return `replan`.
- review: check the change against the plan. `fix` for problems the builder can fix, `redesign` when the plan itself was wrong.
```

`crew factory list` lists your factories. `crew factory diagram ship-change` prints one as a flowchart (it is also written to `build/factories/ship-change.md` on a compile):

```mermaid
flowchart LR
  start(( )) --> s_plan
  s_plan("plan<br/><small>epic-planner</small>")
  s_build("build<br/><small>ticket-worker</small>")
  s_review{{"review<br/><small>change-reviewer</small>"}}
  t_done(["done"])
  s_plan -->|"ready"| s_build
  s_build -->|"done"| s_review
  s_build -.->|"replan ≤2"| s_plan
  s_build -.->|"drift"| s_plan
  s_review -.->|"fix ≤*"| s_build
  s_review -->|"pass"| t_done
  s_review -.->|"redesign ≤1"| s_plan
```

The rules, all checked by `crew verify`:

- **Routes** are `name: station` or `name: {to: station, max: N}`. `done` and `fail` end the run; every station may also return `fail` without listing it.
- **Every loop needs a limit** on at least one of its routes: a number, or `"*"` (in quotes) for "until it passes". A loop with no limit is an error. Taking a route more times than its limit stops the run and reports which route.
- **`approve: true`** pauses after that station until you approve. Without any approval stop, a run goes from start to finish on its own.
- **`on_drift: S`** checks the watched git trees before the station runs. If anything changed since the previous station finished, outside the run, the work goes to `S` first. `watch:` lists the roots (default: the project). Drift redirects don't count toward limits.
- **Stations never ask you questions.** Anything they assumed comes back as a note; if something is missing they route the work back.
- **`harness:`** (on the factory or on a station) is where stations run. That harness needs a `headless:` command in `harnesses.yaml`. An unattended station gets exactly its agent's capabilities and nothing more. Flags that bypass permission checks are rejected.

### Running a factory

A run on its own, outside any mission, is called a plain run. It works in the directory you start it in: no worktree, no branch, no pull request.

```sh
crew run ship-change --input "Add a /health endpoint"     # foreground; Ctrl-C interrupts cleanly
crew run ship-change --input @request.md --detach         # background; logs to .crew/runs/<id>/run.log
crew run status                                           # list runs in this project
crew run status <id> --since 0                            # state and events (ends with "cursor N")
crew run status <id> --since 0 --wait 90s                 # the same, but returns early on news
crew run say <id> "use port 8081"                         # delivered to the next station
crew run goto <id> plan                                   # send the work to a station
crew run approve <id> [--goto build] [--note TEXT]        # continue past an approval stop, or send the work back
crew run resume <id>                                      # continue an interrupted or retryable run
```

A `-` reads standard input wherever text or `@file` is taken (`--input -`, `say <id> -`, `--note -`).

Each run lives in `<project>/.crew/runs/<id>/`: `input.md`, a frozen copy of the factory, `state.json`, `events.jsonl`, and `steps/<n>-<station>/{prompt,output,verdict}`. crew writes `.crew/.gitignore`, so none of it reaches git. An approval is recorded in the run's events with the note you gave.

## The orchestrator

The orchestrator is the agent you talk to. `crew compile` writes it, along with a settings file that holds what it may and may not run. Start it in the repository you want to work on:

```sh
cd ~/src/my-project
crew orchestrator
```

That starts your harness with the orchestrator as the main session, in the current directory. Then say what you want: "run ship-change to add a /health endpoint", or a bigger job it should plan as a mission.

| Command | What it does |
| --- | --- |
| `crew orchestrator` | Starts the orchestrator on the first enabled harness that can host one. |
| `crew orchestrator --harness H` | Picks the harness. |
| `crew orchestrator --print` | Prints the full command instead of running it. |
| `crew orchestrator -- ARGS` | Passes `ARGS` on to the harness. |

`crew verify` and `crew compile` also print the full command, which looks like this:

```
claude-code:
  orchestrator: claude --agent orchestrator --settings /home/you/crew/build/claude-code/agents/orchestrator.settings.json
```

Use `crew orchestrator` or that line, and nothing shorter. The `--settings` file carries the deny rules. A session started as plain `claude --agent orchestrator` has the prompt and no rules.

What the orchestrator does:

- **Plans a mission.** It looks at your domains and factories, splits the job into pieces, shows you the plan and waits for your go.
- **Starts and watches a single factory run** when that is all the job needs.
- **Tells you what matters**: notes, retries, failures, results, and anything that is waiting on you.
- **Passes on what you say** to the stations, and records your answers word for word.

What it never does: run a station, edit files, choose routes, merge anything, or turn auto-merge on. The engine does the work, so a factory behaves the same with or without the orchestrator. Passing an approval stop (`crew run approve`, `crew run goto`) always asks you first.

## Domains

A domain is **one kind of work, described once so missions know how to do it**. It can be a discipline (product, design, marketing) or an area of a codebase ("the web front end", "the API", "the docs").

Domains live in your crew directory, not in a repository, so the same domains apply in every repo you work in.

A factory says *how* work is done (plan, build, review). A domain says *where* and *under what rules*. It answers four questions:

| Question | Where the answer is |
| --- | --- |
| When should a piece of work go here? | `description`. The orchestrator reads it to choose a domain for each piece. |
| Which factories may do the work? | `factories`. A piece in this domain must use one of them. |
| Which files belong to it? | `paths` (optional). |
| What should every agent working here know? | The body. Every station of every piece in the domain gets it as a briefing. |

A domain is a Markdown file in `domains/`, and its name must match the file name:

```markdown
---
name: web
description: The web front end, covering pages, components and styling.
factories: [ship-change]
paths: [web/**]
---
Use the existing component library. Keep changes small.
Run `npm test` in `web/` before you call a change done.
```

What a domain gives you:

- **Routing.** In a plan, every piece names a domain. The orchestrator picks it from the descriptions, so write each description as the sentence that says when to choose this domain.
- **A standing briefing.** Conventions you would otherwise repeat in every request ("use the component library", "never touch the migrations folder") go in the body once.
- **Safe parallel work.** crew uses `paths` to tell which pieces could change the same files. It keeps those apart and runs the rest together. A piece that changes files outside its paths is stopped before a pull request is opened.
- **A merge switch per area.** Auto-merge is set per domain. You might let crew merge `docs` on its own and keep `api` for your own review. A pull request that touches another domain's paths needs that domain's switch too.

`paths` are patterns relative to the repo root: a folder (`web/`), everything under a folder (`api/**`), or a file pattern (`**/*.md`). A domain without `paths` still works, but crew can't check what its pieces touch.

You need at least one domain before you can plan a mission. A small project can have just one:

```markdown
---
name: app
description: Any change to this project's code, tests or docs.
factories: [ship-change]
---
```

`crew domain list` shows what you have. Domains matter only for missions; `crew run` doesn't use them.

## Missions

A mission is one job, written as your request plus a plan of pieces. Each piece has a domain, a factory and a brief, and may wait for other pieces.

How one goes:

1. **You say what you want** to the orchestrator.
2. **It writes a plan** and shows it to you: the pieces, their domains, their paths, what waits for what.
3. **You say go.** Nothing starts before that.
4. **crew runs the pieces**, several at a time when their paths don't overlap. Each gets its own git worktree and branch (`crew/<mission-id>/<piece-id>`).
5. **Each piece ends as one pull request.** You merge it on GitHub, or crew does if auto-merge is on for the piece's domain.
6. **crew sees the merge**, removes the piece's worktree and local branch, and starts the pieces that were waiting for it.
7. **When something needs you**, the piece is parked, you get a desktop notification (macOS for now), and `crew mission status` lists it under "Waiting on you".

### Setting up

Do these once:

```sh
crew mission setup          # only for a crew directory made before missions existed
crew compile                # writes the orchestrator and its settings
```

Add a [domain](#domains). Then, once per repository, in a terminal inside it:

```sh
crew repo confirm
```

It shows the repo and whether it is public or private, and asks you to type the repo's name. Missions push branches and open pull requests, and in a public repo those are public, so crew asks first. No agent session can do this for you.

### Running one

Start the orchestrator in the repo (`crew orchestrator`) and tell it the job. It runs these commands for you, and you can run them yourself:

```sh
crew mission plan @plan.md            # check a plan and create the mission
crew mission start <id>               # run the start checks; stops at "waiting for go"
crew mission go <id>                  # your go: the engine starts
crew mission status                   # list missions
crew mission status <id>              # what runs, what is queued, what waits on you
crew mission answer <id> <wait> --do approve --words "looks good"
crew mission amend <id> @changes.md --words "also cover the admin page"
crew mission stop <id>
crew mission resume <id>
```

A plan is a Markdown file. The frontmatter lists the pieces, and the body is your request, word for word:

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
```

### Things that wait on you

crew does not guess at anything that needs a person. Each such point is a wait with an id (`w1`, `w2`, …), and you answer it with `crew mission answer` or by telling the orchestrator.

| Kind | What it means | What you can answer |
| --- | --- | --- |
| `merge` | A pull request is open and crew did not merge it. | Merge it on GitHub. `cancel` abandons the piece. |
| `approval` | A station with `approve: true` has finished. | `approve`, `goto` (send it back to a station) or `cancel`. |
| `question` | A failure the orchestrator couldn't settle. | `retry`, `goto` or `cancel`. |
| `limit` | A piece used up its orchestrator calls. | `retry`, `goto` or `cancel`. |
| `pr-closed` | The pull request was closed without being merged. | `retry` or `cancel`. |
| `paths` | A piece changed files outside its paths. | `accept`, `goto` or `cancel`. |
| `deliverable` | A piece left files for you to accept. | `accept`, `goto` or `cancel`. |
| `plan` | A delegated piece could not be planned. | `retry` or `cancel`. |

### Auto-merge

By default crew merges nothing. You can switch auto-merge on per domain, in a terminal, by typing a confirmation:

```sh
crew automerge on --domain docs               # merge once the checks have passed
crew automerge on --domain docs --without-checks
crew automerge off --domain docs
crew automerge status
```

The switch is your approval to merge. Sessions are denied the command, and the orchestrator is told never to run it. If merging to your main branch deploys, a merge by crew is a deploy.

### More that missions can do

- **Changes after the start.** `crew mission amend` changes the plan of a running mission, and records your words with the change.
- **When a station fails.** crew resumes what can be resumed, sends uncommitted work back once, then asks the orchestrator for one decision. What can't be settled comes to you as a `question`.
- **Delegation.** A piece with `delegate: true` is split into pieces of its own later, once earlier work has landed.
- **Deliverables.** A piece can end in files (a brief, a research note) instead of, or as well as, a pull request. You accept them.
- **Budgets.** `missions.yaml` caps pieces, steps, pieces running at once, and orchestrator calls per piece.
- **Jira.** `crew jira setup --site URL --key KEY` shows a repo's missions on a Jira board: an epic per mission and an assigned issue per piece, moved across the board as the piece moves. It reads your token only from the environment (`CREW_JIRA_EMAIL`, `CREW_JIRA_TOKEN`).

Everything a mission writes goes under `.crew/` in the repo, which is gitignored. Nothing is ever deleted on GitHub: crew does not delete remote branches or close pull requests.

**[docs/missions.md](docs/missions.md) is the full guide**: every command, every setting, and what the safeguards do and don't cover.

## Command reference

Compiling:

| Command | What it does |
| --- | --- |
| `crew init [--dir D]` | Scaffolds a crew directory. |
| `crew verify [-v]` | Strict check of your config and sources. `-v` also prints info notes. |
| `crew plan` | Shows what each harness gets: models, prices, notes, and file changes. |
| `crew compile [--dry-run] [--force]` | Verifies, then writes files and links. |
| `crew models [--harness H \| --all]` | Lists every model and effort each harness can run. |
| `crew models sync` | Refreshes `models.yaml` from models.dev. |
| `crew capabilities [--harness H \| --all]` | Shows which native tools each capability becomes. |
| `crew migrate [--from D] [--to D] [--dry-run]` | Moves an old `crewfile` setup to crew's names. |

Factories and runs:

| Command | What it does |
| --- | --- |
| `crew factory list` | Lists factories. |
| `crew factory diagram <name>` | Prints a factory as a Mermaid flowchart. |
| `crew run <factory> --input TEXT\|@FILE\|- [--detach]` | Runs a factory in the current directory. |
| `crew run status [<id>] [--since N] [--wait T]` | Lists runs, or shows one. |
| `crew run say <id> TEXT\|-` | Sends a message to the next station. |
| `crew run goto <id> <station>` | Sends the work to a station. |
| `crew run approve <id> [--goto S] [--note TEXT]` | Continues past an approval stop. |
| `crew run resume <id>` | Continues an interrupted run. |

The orchestrator, domains and missions:

| Command | What it does |
| --- | --- |
| `crew orchestrator [--harness H] [--print] [-- ARGS]` | Starts the orchestrator in the current directory. |
| `crew domain list` | Lists domains. |
| `crew repo confirm` / `crew repo status` | Confirms a repo for missions (in a terminal), or shows where it stands. |
| `crew mission setup` | Adds the missions parts to an older crew directory. |
| `crew mission plan`, `start`, `go`, `status`, `answer`, `amend`, `stop`, `resume` | Run a mission. See [Missions](#missions). |
| `crew automerge on\|off\|status` | Sets auto-merge per domain (in a terminal), or shows it. |
| `crew jira setup\|check\|sync` | Shows missions on a Jira board. |

Flags most commands take: `--dir D` (the crew directory). Compile commands also take `--scope user|project`, `--project PATH`, `--out DIR` and `--allow-warnings`. Mission commands take `--project P` (the repo; default: the current directory).

## Strict verification

`verify` (and therefore `compile`) fails on:

- unknown keys anywhere: source frontmatter, `harnesses.yaml`, `crew.yaml`
- a name that differs from its filename, isn't kebab-case, or breaks a harness rule (for example, Claude Code names can't contain "claude")
- unknown capabilities, skills, agents, harnesses or globs that match nothing
- an agent without a model, or with a model that isn't in that harness's list in `models.yaml`
- an effort value that the agent's model doesn't accept
- a missing `models.yaml`, or one with no list for a harness that has a catalog
- a harness missing a mapping for any capability
- two harnesses writing different content to the same path
- stray files or folders in `agents/`, a file directly in `skills/`, or a skill folder without `SKILL.md`
- a factory that breaks the rules in [Factories](#factories), or a domain that names a factory you don't have

It warns when the `verified` date in `harnesses.yaml`, or the `fetched` date in `models.yaml`, is older than `stale_after_days`. Warnings are fatal unless you pass `--allow-warnings`. That is deliberate: these formats change every few months.

## Safety

### What compile will and won't touch

crew keeps a record (`.crew/state.*.json`, gitignored) of every file and link it wrote, with its hash or link target.

- It only overwrites or deletes files and links it wrote. A folder it didn't write (say, your own skill with the same name) is a `CONFLICT`.
- A hand-edited output, or a file it didn't create, shows up as a `CONFLICT`. Nothing is written until the conflict is resolved, or until you pass `--force`.
- When you delete a source, its outputs are removed on the next compile, and so are empty skill folders.
- Writes and links are atomic (temp file or temp link, then rename).

### What missions will and won't do

- **crew merges nothing unless you switched auto-merge on** for that domain, and it merges only a commit it pushed itself.
- **Every session crew starts gets deny rules**: no merging or closing a pull request, no force-push, no push to `main`, no `crew automerge`, no `crew repo confirm`, no writing in your crew directory. Stations also can't run `crew run` or `crew mission`, so a station can't pass an approval stop or answer for you.
- **Some commands only you can run.** `crew repo confirm`, `crew automerge` and `crew jira setup` need a terminal and a typed confirmation, with no flag that skips it.

These protect against mistakes and misread prompts. They are not a security boundary: agent sessions run as your own user with your GitHub login, and a process that sets out to get around the rules can. crew also pushes whatever stations commit, without scanning for secrets. [The guide](docs/missions.md#what-the-safeguards-do-and-dont-cover) lists the limits one by one. Read it before you point crew at a repository that matters.

## Upgrading an older setup

- **A crew directory from before missions:** run `crew mission setup`, then `crew compile`. Setup writes `missions.yaml` and `domains/` if they are missing and adds the missions parts to `harnesses.yaml`, keeping a copy of the old file as `harnesses.yaml.before-missions`.
- **A `workflows/` folder:** it is now called `factories/`. Move your files there.
- **Foreman agents:** the per-factory `foreman-<factory>` agents are gone. The next `crew compile` removes them, and the orchestrator does their job.
- **Coming from `crewfile`:** `crew migrate` moves `~/crewfile` to `~/crew` (and `crewfile.yaml` to `crew.yaml`, `.crewfile/` to `.crew/`), keeping crew's ownership records. Then `crew compile` re-points every harness link. `--dry-run` shows the steps first.
- **New template files:** `crew init` only writes into a new directory. A template change, such as the `change-reviewer` agent that reviews in `ship-change`, doesn't reach a directory you already have; copy the file from `internal/scaffold/template/` if you want it.

## Adding a harness

Add an entry under `harnesses:` in `harnesses.yaml`. Set its skill and agent dirs, `format` (`markdown` or `toml`), the output key names in `fields`, any `static` keys, any `derived` keys (computed from capabilities), the capability-to-tool map, and a `catalog:` block (models.dev providers and `id_style`). Run `crew models sync`, then add it to `targets:` in `crew.yaml`. You only touch Go code for a format that isn't Markdown frontmatter or TOML.

## Facts to double-check before trusting them

Everything in `harnesses.yaml` was researched on 2026-11-18. Confirm these against your installed versions:

- Model ids and effort levels come from models.dev. Spot-check them against `/models` in OpenCode or `/model` in the other harnesses; they can lag a release by a few days.
- OpenCode takes the effort as a model variant (`#high`). Variant names come from the same catalog, but not every model has one for every effort.
- Claude Code reads `effort` in agent frontmatter, but the field is missing from the docs table.
- Codex's agent-level `sandbox_mode: read-only` reportedly doesn't block writes in recent versions. Don't treat it as a security boundary.
- Cursor has no model list in crew, because Cursor bills by plan; agents sent there must use `inherit`.

## Not built yet

- **Releases.** Effects other than a merge, such as publishing or spending, each approved by you. `crew release approve` doesn't exist yet.
- **A separate identity for agents.** Today they run as you.
- **Notifications outside macOS.**
