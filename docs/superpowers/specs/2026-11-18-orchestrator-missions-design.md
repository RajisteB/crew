# Orchestrator, domains and missions: design

Status: **draft for a second opinion** · 2026-11-18 · builds on [factories](2026-11-18-factories-design.md)

## Intent

Today crew runs **one factory at a time**. You start a run, and a generated foreman agent per factory keeps you posted. The user wants to hand crew a whole job, like "launch referrals: credits, a landing page and an ad campaign". crew should work out which kinds of work the job needs, split it into pieces, run those pieces through the right factories side by side, and bring everything back for review. That includes work that acts in the outside world, like ads and publishing, which must never go out without the user's review.

The model is an organization, not a pipeline:

| Name | What it is |
|---|---|
| **principal** | The user. Gives intent, owns every switch and every outside action. |
| **orchestrator** | The one agent the principal talks to, in any repo. It plans the job as a mission, handles exceptions, and reports back. |
| **domain** | A kind of work (product, design, marketing, …), defined once in the crew config. It has its own factories, conventions and allowed outside actions. |
| **mission** | The orchestrator's plan for one job, as a file. crew checks it and runs it. |
| **piece** | One unit of a mission. Each piece is a factory run, or a sub-mission planned by the orchestrator working inside one domain. |
| **factory / station / run** | Unchanged from the factories design. |
| **release** | Any action that leaves the machine: merging a PR, publishing, launching a campaign, spending money. |

**Why:** the user will open one session per repo and run many kinds of work in it. One repo can hold more than one domain, for example an app, its marketing site and its brand assets. They want one place to talk, real parallelism, and a hard guarantee that nothing ships or spends without them.

## Goals

- **One conversation per job.** The principal talks only to the orchestrator.
- **Domains as config.** Each domain lives in `~/crew/domains/<name>.md`: a description, the factories it may use, optional repo paths, the release actions it may request, and a briefing body. Domains are defined once and usable in every repo, so repos carry no domain config.
- **Missions.** The orchestrator picks the domains and factories for a job, splits the job into pieces, and states which pieces wait for which. crew checks the mission and draws it as a diagram.
- **Recursion at the orchestrator level.** When one domain's share of a job is big, the orchestrator hands that share to a copy of itself scoped to the domain: that domain's briefing, factories and paths. The scoped copy plans that part as a sub-mission. Delegation depth has a limit, under the same rule as loops: a number or `"*"`, and no limit is an error.
- **Parallel pieces in one repo.** Each run gets its own **git worktree**, with a cap on how many run at once (default 3).
- **One PR per piece.** A piece that depends on another waits until that PR is **merged**, then starts from the updated `main`.
- **The auto-merge switch belongs to the principal alone.** It's off by default and set per repo. Only `crew automerge on|off`, run in a real terminal with a typed confirmation, can change it. crew does the merging; no agent ever does.
- **Problems go to the nearest manager first.** When a run stops, the closest orchestrator that has the authority decides: a scoped orchestrator first, then the main one, then the principal.
- **Releases** (Project 2): anything that leaves the machine waits for the principal's review and approval.

## Non-goals

- Missions that span several repos, and any "workspace" layer above repos. The user runs one session per repo.
- **Pilots** (per-factory agents). They're replaced by the orchestrator, and today's `foreman-<factory>` agents are removed.
- A separate receptionist or router. Choosing domains is part of the orchestrator's planning.
- Parallel stations inside one factory, a live diagram, cloud runs. These stay out, as in the factories design.

## Anti-goals

- Anything leaving the machine without the principal's switch or approval.
- Any session (orchestrator, scoped orchestrator, station) able to change a switch or approve a release.
- State that exists only in an LLM's context. Closing the session must lose nothing.
- A layer whose only job is relaying messages.
- The orchestrator writing detailed code plans. Those go stale once earlier pieces merge. That job belongs to each factory's planning station, which keeps the existing rule: the planner says exactly what to change, the implementer implements, and drift goes back to the planner.

## Approaches considered

1. **Hierarchy**
   - *Pilots per factory under a boss.* Rejected. A pilot only relays status and makes exception calls knowing just its own factory, when the orchestrator has the whole mission. It's a worse decision-maker, plus another hop where messages get lost or reworded.
   - *A receptionist routing to separate domain-orchestrator agents.* Rejected once domains became config. Routing is then just part of planning.
   - **Picked:** one orchestrator agent, domains as config, and the orchestrator delegating to copies of itself scoped to a domain. This gives recursion and domain specialization with a single agent type.
2. **Bringing pieces back together**
   - *The orchestrator merges into one mission branch as it goes.* Rejected.
   - *Leave the branches for the user to merge.* Rejected.
   - **Picked:** one PR per piece, with merging decided by the principal's switch.
3. **Who plans what**
   - **Picked:** the orchestrator plans the mission (which pieces, which domains and factories, and the order). A factory's planning station plans the change, inside the worktree, against the code as it stands when the piece starts.
4. **Scope**
   - Missions spanning several repos were considered and dropped. The user works one repo per session.

## How it works

### Domain config

```markdown
~/crew/domains/design.md
---
name: design
description: Brand assets, illustrations, ad creatives, UI mockups.
factories: [design-asset, ui-mockup]
paths: ["brand/**", "assets/**"]   # optional: where this domain's work lives in a repo
releases: [figma-publish]          # outside actions it may request (Project 2)
---
House style, tone, file formats, what "done" means for design…
```

`crew verify` checks domains the way it checks factories:
- every listed factory exists
- every release action exists
- names are unique

The briefing is passed to every run of that domain's factories and to the scoped orchestrator.

### The orchestrator

- **One generated agent.** `crew compile` emits `orchestrator` for each enabled harness that can host a main session. It replaces the per-factory foremen.
- **Capabilities: `read` and `shell`.** It reads domains, factories and the repo, and it drives everything through `crew` commands. It never edits files, runs a station, merges, or approves a release.
- **Reads on demand.** It looks up domain and factory details (`crew domain list`, `crew factory list|diagram`) when it needs them, rather than carrying them all in its prompt.
- **Naming a domain constrains planning to it.** For example, "design only: new referral badge".

### Missions

- **The mission file.** The orchestrator writes `.crew/missions/<id>/mission.md`:
  - The frontmatter lists the pieces. Each has an `id`, a `domain`, either a `factory` or `delegate: true`, a `brief`, an optional `paths`, and `waits_for: [ids]`.
  - The body is **the principal's request, word for word**. It's passed down unchanged to every level so intent doesn't drift through rewrites.
- **Checking.** `crew mission start` refuses unless every piece's factory belongs to its domain, every `waits_for` exists, nothing waits in a circle, and delegation stays within the depth limit.
- **Running.** The mission runs detached, like `crew run --detach`. The engine is the deterministic layer that does the job of middle management. It schedules pieces, creates worktrees, starts runs, opens PRs, waits for merges, enforces the concurrency cap, and records every event.
- **Approval stop.** If a "show me the mission first" stop is configured, the mission waits after checking. There's no stop unless one is configured.

### Scoped orchestrator (recursion)

- **Planning a sub-mission.** For a `delegate: true` piece, crew calls the orchestrator agent **headless**, with that domain's briefing and factories, the original request and the piece's brief. It replies with a structured block listing the sub-pieces, which crew checks and attaches under the parent piece.
- **Handling problems.** When a run in its part of the tree stops, crew calls it again with a short summary. It replies with one allowed action, or `escalate`.
- **No tools beyond read.** It doesn't use a shell; crew carries out its decisions.

### Exceptions and authority

When a run stops (it hit a limit, failed, conflicts, or reached an approval stop), crew sends a short summary to the nearest orchestrator. The allowed actions are:
- retry
- `goto` a station (within the factory's limits)
- resume
- cancel a piece
- re-brief a piece that hasn't started
- add a follow-up piece **when it fulfils the principal's request**

Changing scope, or anything beyond these, escalates up the tree and finally to the principal. Authority only narrows going down.

### Worktrees, PRs and merging

- **Worktrees.** Each run executes in its own worktree on a piece branch cut from the latest `main` once the pieces it waits for are merged. The drift guard works per worktree unchanged.
- **PRs.** When a run finishes `done`, crew pushes the branch and opens a PR. *(Builder call: this push happens automatically. The principal chose one PR per piece, and the push is the only way to get there.)*
- **Merging.** With auto-merge on, crew runs `gh pr merge --auto`, so branch protection and required checks still apply. With it off, the PR waits for the principal, and crew tracks the PR's state through `gh` to release the pieces waiting on it.
- **Conflicts.** If a PR conflicts after another piece lands, that piece goes back through its factory to the station named by `on_drift`, under a limit like any loop.

### The auto-merge switch

- **Where it lives.** It's stored in `~/crew` and keyed by repo, so sessions working in the repo can't simply edit it in place.
- **Tamper evidence.** crew records a fingerprint each time the switch is set with `crew automerge`. If the stored value changes any other way, crew treats auto-merge as off, refuses to merge, and tells the principal.
- **Not a hard lock.** Sessions run as the same Unix user, so this can be noticed but not prevented. A hard lock would need a separate OS user or root, which was judged overkill.

### Releases (Project 2, summary)

- **Release requests.** Factories in outward-facing domains end with a release request instead of acting: what, where, the exact content, previews, cost and audience.
- **Reviewing.** The principal reviews with `crew release show <n>` and approves with `crew release approve <n>`. Approval works only in a terminal, with a typed confirmation.
- **Running an approved release.** It runs through a fixed action defined once per platform (a script or a connected tool), so what was approved is what runs. Where no fixed action exists, a single-purpose agent with access to only that one tool runs it, and the release is marked as having gone out that way.
- **Settings.** Each kind of release is set to `review` (the default), `auto` or `off`. Auto-merge becomes one of these. Spending always has a budget cap set by the principal, and `auto` applies only within that cap.

## Walkthrough

The repo `acme` holds `app/`, `site/`, `ads/` and `brand/`. Three domains are defined in `~/crew/domains/`: product, design and marketing.

1. **The request.** In a session in `acme`, the principal tells the orchestrator: *"Launch referrals: $10 credit per invited friend who signs up, plus a landing page and an ad campaign."*
2. **The mission.** The orchestrator plans:
   - P1: product, credits API, `ship-change`
   - P2: product, invite screen, waits for P1
   - P3: design, referral visuals, **delegated**
   - P4: marketing, landing page, waits for P3
   - P5: marketing, ad campaign, waits for P1, P2 and P3

   crew checks the mission, draws it, and starts P1 and P3.
3. **The design sub-mission.** The design-scoped orchestrator plans P3.1 (a badge) and P3.2 (three ad creatives, waiting for P3.1).
4. **Running.** Runs proceed in their own worktrees, using the existing factory behaviour.
5. **Merging.** P1 opens PR #12. Auto-merge is on, so it merges once checks pass, and P2 starts from the updated `main`.
6. **A problem.** P3.2's review hits its limit. The design-scoped orchestrator re-plans it once. It fails again, so the problem escalates, and the orchestrator asks the principal: "use the dark variant, or drop 9:16?"
7. **A change partway through.** The principal says "make it $15". P1 is already merged, so the orchestrator adds P6 (change the credit to $15). P5 hasn't started, so its brief is updated.
8. **The release.** P5 ends with release request #7: Meta Ads, three creatives, US 25–44, $200/day, $2,000 cap. The principal reviews and approves it in a terminal, and crew runs the fixed ad-launch action.
9. **The wrap-up.** The orchestrator reports the PRs, the merges, the releases, and the calls it made along the way.

## Build order

1. **Project 1:** domains, the orchestrator (replacing the foremen), missions, scoped delegation, worktrees, one PR per piece, waiting for merges, and the auto-merge switch.
2. **Project 2:** releases, covering requests, previews, terminal approval, per-platform actions, `review`/`auto`/`off`, and budget caps.

Each project gets its own design doc and plan, and is usable on its own.

## Known weak spots (for the critique)

1. **The orchestrator's context grows with mission size.** A short summary and exception-only reporting slow this down, but very large missions may still need deeper delegation.
2. **A single exception-handler makes the same kind of mistake across every run.** Bounded authority and a full event log limit the damage.
3. **Context lost at handoffs.** Each delegation hop can lose context. Passing the request down word for word reduces this but doesn't remove it.
4. **Wrong domain picks.** How well the orchestrator chooses domains depends on how clearly the domain descriptions are written.
5. **More stations to trust.** With no per-run manager, a factory's stations and routes are the main quality control.
6. **The switch can be noticed if tampered with, but not prevented.**
7. **Automatic pushing.** It's still an outward-facing step. Should it also be a principal-only setting?
8. **Domain paths aren't enforced.** They aren't yet a hard boundary on what a run may edit.

## Left to the builder

- The exact field names in the mission and domain files.
- Where run folders live when runs execute in worktrees (proposal: the main repo's `.crew/runs`, with each run's work directory set to its worktree).
- How the event summary is formatted.
- Worktree cleanup after merge.
- How the `foreman-*` links are removed during compile.
