# Factories — design

Status: draft for review · 2026-11-18

## Intent

**Rename first:** the tool is renamed from `crewfile` to **`crew`** before any factory work. This covers the binary, `crew.yaml`, the `~/crew` folder, `.crew/` (state and runs), `CREW_HOME`, the module path and the README. No `crew` command exists on the user's machine or in Homebrew. ChromeOS's Chromebrew package manager is also called `crew`, which doesn't matter on macOS. The rename must migrate the existing setup: the harness links in `~/.claude/agents` and `~/.config/opencode/agents` point into `~/crewfile/build/`, and `.crewfile/state.user.json` records them, so `crew` must re-point those links and carry the state over rather than orphan them.

crew already lets you write each agent once and compile it into every harness. **Factories** chain those agents into a production line that turns an input into a finished result. Work moves forward from one station to the next. Any station can also send the work **back to any earlier station**, not just the one before it, as many times as the factory allows.

The user will build many different factories for many different end results. The format has to be **general**: nothing in it may assume a particular domain (specs, code, Jira, docs …). The "plan → build → review" line below is only an example.

Every factory comes with a **diagram generated from the same file**, so the picture always shows exactly how that factory routes.

## Goals

- One harness-agnostic definition per factory in `factories/` (replaces the reserved `workflows/` dir).
- Arbitrary forward and backward routes between stations, chosen by each station's result.
- Every route that can loop has a limit, either a number or `*` (unlimited). A missing limit is a `verify` error.
- Optional approval stops, configured per station. If a factory has none, it runs start to finish.
- Two ways to run the same factory, behaving the same way:
  - **A. Foreman:** `crew compile` emits one foreman agent per factory. It runs as the **main session** of a harness, and the user talks to it while it works.
  - **B. Engine:** `crew run <factory>` drives the stations itself through each harness's headless CLI.
- A generic **drift guard**: if files changed between two stations (from outside the run), the work is sent to a named station before continuing.
- `crew factory diagram <name>` outputs a Mermaid diagram.

## Non-goals (v1)

- Parallel stations / fan-out (format must not block adding it later).
- A factory used as a station inside another factory.
- Live/HTML diagram that highlights the running station.
- Cross-machine or cloud runs.

## Anti-goals

- Domain-specific fields or behaviour in the format (e.g., anything that only makes sense for "spec" or "implementer").
- A loop that can run forever without the author having written `*`.
- A and B diverging, i.e. the same factory and the same station results producing different routes.

## Decisions made with the user

1. Build both A and B. The factory file is the shared contract.
2. The foreman is the main session, not a subagent (subagents can't converse with the user mid-task).
3. Limits: a looping route with no limit makes `verify` fail. `*` = loop until a different route is taken.
4. Runs go start to finish unless the factory config has approval stops.
5. The user can message the foreman during a run. The foreman surfaces anything pertinent without stopping.
6. Stations that do the work (e.g., an implementer) never ask the user questions. A question means the earlier station's output was incomplete, so they route **back**, e.g. to the planner.
7. If files changed between stations, the work goes back to the configured station before continuing.
8. In A, the foreman does **not** run stations as subagents. Each station runs through the engine as its own headless session. That way a station can still delegate (e.g., `implementer` → specialists, `reviewer`), and A can't diverge from B.
9. Unattended stations get exactly their agent's capabilities pre-allowed: crew's capability → native-tool mapping becomes the harness's headless allowlist, and nothing more. No skip-permissions flags, ever. `shell` means unrestricted shell for that agent.

## Approaches considered

| | What it is | Trade-off |
|---|---|---|
| **A. Foreman agent** (chosen) | compile emits a main-session agent per factory; it's the conversation on top of the engine | in-session, so you can talk to it; depends on B's engine; you see each station's result and notes, not its live work |
| **B. `crew run` engine** (chosen) | Go engine runs stations through headless harness CLIs | exact, resumable, can mix harnesses; largest build |
| C. Compile to Claude Code workflow scripts | Claude-only native orchestration | rejected: breaks crew's harness-agnostic premise |

**One engine, two front ends (decided with user):** routing, loop counts, guards, approvals **and station execution** all live in Go. B is the engine with a terminal front end. A is the same engine with a foreman agent as the conversational front end. The foreman never routes, counts or runs a station itself. It only starts or steps the engine, relays notes and approvals, and passes the user's messages in. Rejected alternative: the foreman runs stations as subagents. That's visible in-session, but Claude Code subagents can't start subagents, so delegating stations would behave differently in A and B.

**Build order:** shared core (format, verify, diagram) → B engine → A foreman.

## Factory file

`factories/<name>.md`, the same shape as agents: YAML frontmatter holds the graph, and the Markdown body is the **briefing** (what this factory produces, standing instructions) that the foreman and every station receive. Name must equal filename.

```markdown
---
name: ship-change
description: Plans, builds and reviews one change in the current repo.
start: plan
harness: claude-code            # default harness for stations (B); optional
stations:
  plan:
    agent: epic-planner
    routes:
      ready: build
  build:
    agent: implementer
    on_drift: plan              # guard: outside changes since last station → plan
    routes:
      done: review
      replan: {to: plan, max: 2}
  review:
    agent: reviewer
    approve: true               # optional approval stop after this station
    routes:
      pass: done
      fix: {to: build, max: "*"}
      redesign: {to: plan, max: 1}
---
Briefing: what this factory is for, conventions every station follows …
```

Rules:

- `start` names a station. `done` is the reserved terminal target (also `fail`, which ends the run as failed).
- `routes` maps a **route name** (what the station returns) to a target. The short form is `name: target`. The long form is `{to, max}`.
- `max`: positive integer or `"*"`. It counts how many times **that route** has been taken in this run. Taking it once more after it hits its max stops the run as failed, with a report naming the station, the route and the last outputs. Stopping is preferred to silently rerouting.
- **Every cycle must contain at least one route with `max`.** The rule doesn't depend on order: verify enumerates the cycles (e.g., per strongly connected component) and errors on any cycle where no route has a `max`, naming the routes in that cycle. Routes with `max` are the ones the diagram draws dashed. *(builder call)*
- `fail` is always an allowed route for every station, even if it isn't listed. A station whose input is unusable and has nowhere to send the work back returns `fail` with the reason. If a station has to make a call only the user could make, it makes its best call and records it in `notes` as an assumption. The run continues, and the user can redirect (`goto`) or override through the foreman. Authors who want a hard stop configure `approve: true`. *(decided with user)*
- `agent` must exist in `agents/`. For B, it must be enabled on the station's harness in `crew.yaml`.
- `harness` (factory or station level) is used by B only. The foreman always runs stations in its own harness.
- `approve: true`: after the station finishes, the run pauses with its output and chosen route. The user accepts, or redirects to any station with a note.
- `on_drift: <station>`: before this station runs, compare the watched files with the snapshot taken when the previous station finished. If they differ, go to `<station>` instead (the change is recorded as the reason). The snapshot is per git working tree: HEAD, tracked-file diff, untracked files, excluding `.crew/`. A factory-level `watch:` lists the roots to snapshot (default: the project root), because planning docs and code can live in different repos. Every watched root must be a git repo; otherwise `begin` errors. *(builder call)*
  - Drift redirects are exempt from route limits. They only fire on outside edits, and each one is logged and shown as a note. *(builder call)*
  - After a crash, `resume` re-runs the crashed station on the current tree without a drift check, because its own partial work isn't outside drift. The skipped check is logged. *(builder call)*

verify also checks: unknown keys, unreachable stations, stations with no routes (other than ending), and targets that don't exist. `*` gets an **info** note, not a warning, because warnings fail `verify`. The current "workflows ignored" note goes away; any leftover `workflows/` dir gets an info note suggesting `factories/`.

## Station contract (identical in A and B)

**crew captures the station's final reply. The station doesn't write files itself.** Many stations are read-only on purpose (planners, researchers, explorers). The station ends its reply with a fenced `verdict` JSON block. Everything before that block becomes `output.md`, and crew writes both files. The reply is the headless CLI's output in both A and B. *(builder call; keeps "agents need no factory-specific changes")*

Each run has a folder `<project>/.crew/runs/<run-id>/`:

```
input.md            what the run was started with
state.json          current station, route counts, snapshots, status (written only by crew)
inbox.md            user messages not yet delivered to a station
steps/<n>-<station>/
  prompt.md         exactly what the station was given
  output.md         the station's work product / handoff
  verdict.json      {"route": "<one of its routes>", "summary": "...", "notes": ["..."]}
```

The prompt for each step is assembled by crew, never hand-written by the foreman. It contains the briefing, the input, the previous steps' outputs (paths), undelivered inbox messages, the allowed route names, and the instruction to end with the `verdict` block. Agents in `agents/` need no factory-specific changes. The contract is injected per step, and it tells the station that questions for the user are not allowed: pick the route that sends the work back.

Invalid or missing verdict (no block, bad JSON, unknown route): the step is re-run once with the error appended; a second failure fails the run.

`notes` in a verdict are things the user should know. They don't stop the run. The foreman (A) or the terminal (B) shows them.

## A. Foreman

- `crew compile` renders, per factory, an agent `foreman-<factory>` for each enabled harness that can host a main-session agent and has `shell`. Other harnesses get an info note and no foreman. The foreman needs only `shell` (plus `read` to show outputs); stations run on the harness the factory names, not necessarily the foreman's.
- It's a main-session agent: Claude Code launches it with `claude --agent foreman-<factory>`, and OpenCode gets `mode: primary`. How each harness spells this goes in `harnesses.yaml` (new `factories:` block per harness), verified against the docs like the existing fields.
- Its body is generated: the briefing plus a fixed loop. Start the run with `crew run <factory> --input … --detach` (or step it one station at a time; mechanism is the builder's call, but the user must be able to talk to the foreman while a station runs). Watch `crew run status <run-id>` for step results, notes, drift redirects and stops. Relay anything pertinent. Put whatever the user says into the inbox with `crew run say`. If the user asks to redirect, use `crew run goto <run-id> <station>`. Stop when the run reports `done` or `failed`.
- At an approval stop, the foreman asks the user in the session and calls `crew run approve <run-id> [--goto station] [--note …]`.

## B. Engine

- `crew run <factory> [--input text|@file] [--project PATH] [--detach]`: each station is a headless CLI call to its harness's agent (e.g., `claude -p` with the agent selected). The command template per harness lives in `harnesses.yaml` (new `headless:` block, verified date like everything else). Harnesses without one can't be used as station harnesses (a verify error if a factory points at one).
- **Permissions:** the `headless:` block also says how to pre-allow tools (e.g., Claude Code's allowed-tools flag plus an accept-edits mode). The allowlist is exactly the agent's capabilities mapped through `harnesses.yaml`. verify fails a `headless:` block that uses any skip-permissions/bypass flag. A tool denial at runtime fails that step with the denial in the report.
- It streams step start/end, route taken, loop counts (`fix 3/*`), and notes to the terminal.
- `crew run status|say|goto|approve|resume <run-id>` work from another terminal. The engine reads the inbox before each step. `resume` continues a stopped, paused or crashed run from `state.json`.
- Per-step timeout configurable (`timeout:` at factory/station level), default left to builder.

## Diagram

`crew factory diagram <name>` prints Mermaid `flowchart LR`. Forward routes are solid arrows, routes that close a cycle are dashed, each edge is labelled `route (max)`, approval stations get a distinct shape, drift guards are dashed edges labelled `drift`. `compile` also writes `build/factories/<name>.md` containing the diagram so it renders in editors/GitHub.

## Constraints

- Go 1.22, only dependency today is `yaml.v3`. Keep it that way unless a dependency clearly pays for itself.
- Follow the existing package style: new `internal/factory` (parse, validate, graph, diagram) and `internal/run` (state machine, run folder, snapshots, headless driver). Small files, immutable updates to state.
- TDD, 80%+ coverage. B's engine is tested with a fake harness command (a script that writes a scripted verdict), so loops, limits, `*`, drift, approvals and resume are all covered without real models.

## Left to the builder

- Exact CLI flag names beyond those above, the run-id format, and the timeout default.
- Prompt template wording for the station contract.
- How the headless command passes the agent and the prompt for each harness (verify against docs and record in `harnesses.yaml`).
- `--detach` vs. step-at-a-time as the foreman's way of driving the engine.
- Whether `.crew/` in a project gets added to that project's `.gitignore` automatically or just suggested.
