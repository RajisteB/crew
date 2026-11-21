# Orchestrator, domains and missions: design

Status: **revised after review** · drafted 2026-11-18 · revised 2026-11-21 · builds on [factories](2026-11-18-factories-design.md)

## Intent

Today crew runs **one factory at a time**. You start a run, and a generated foreman agent per factory keeps you posted. The user wants to hand crew a whole job, like "launch referrals: credits, a landing page and an ad campaign". crew should work out which kinds of work the job needs, split it into pieces, run those pieces through the right factories side by side, merge what passes, and bring everything back. That includes work that acts in the outside world, like ads and publishing, which must not go out without the user's review.

The model is an organization, not a pipeline:

| Name | What it is |
|---|---|
| **principal** | The user. Gives intent, owns every switch and every approval. |
| **orchestrator** | The one agent the principal talks to, in any repo. It plans the job as a mission, handles exceptions, and reports back. |
| **domain** | A kind of work (product, design, marketing, …), defined once in the crew config. It has its own factories, conventions and allowed releases. |
| **mission** | The orchestrator's plan for one job, as a file. crew checks it and runs it. |
| **piece** | One unit of a mission. Each piece is a factory run, or a sub-mission planned by the orchestrator working inside one domain. |
| **factory / station / run** | Unchanged from the factories design, apart from one new optional factory field, `rework`. |
| **proposal** | An outward step that can be reviewed and thrown away: pushing a piece branch, opening a PR. Proposals are automatic. |
| **effect** | An outward step that can't be taken back or costs money: merging into `main`, publishing, launching a campaign, spending. Every effect needs the principal's switch or approval. |
| **release** | An effect other than a merge (Project 2). Releases are approved one at a time. |

**Why:** the user opens one session per repo and runs many kinds of work in it. One repo can hold more than one domain, for example an app, its marketing site and its brand assets. They want one place to talk, real parallelism, and as much automation as is safe: with auto-merge on, a job should run from request to merged PRs without them.

## Goals

- **One conversation per job.** The principal talks only to the orchestrator.
- **Domains as config.** Each domain lives in `~/crew/domains/<name>.md`. Domains are defined once and usable in every repo, so repos carry no domain config.
- **Missions.** The orchestrator picks the domains and factories for a job, splits the job into pieces, and states which pieces wait for which. crew checks the mission and draws it as a diagram.
- **Recursion at the orchestrator level.** A big share of a job in one domain goes to a copy of the orchestrator scoped to that domain, which plans it as a sub-mission. Depth has a limit, under the same rule as loops: a number or `"*"`, and no limit is an error.
- **Parallel pieces in one repo.** Each run gets its own git worktree, with a cap on how many run at once.
- **One PR per piece.** A piece that depends on another waits until that piece is complete, then starts from the updated `main`.
- **Auto-merge that works unattended.** A per-repo switch only the principal can set. With it on, crew merges each PR once its checks pass.
- **The principal can always see what is waiting on them**, and is told when something starts waiting.
- **Nothing is lost when a session or the engine dies.** Mission state is on disk and a mission can be resumed.
- **Problems go to the nearest manager first:** a scoped orchestrator, then the main one, then the principal.
- **Releases** (Project 2): every effect other than a merge waits for the principal's review.

## Non-goals

- Missions that span several repos, and any "workspace" layer above repos.
- **Pilots** (per-factory agents). The orchestrator replaces them, and today's `foreman-<factory>` agents are removed at the end of Project 1.
- A separate receptionist or router. Choosing domains is part of planning.
- **A security boundary against a process running as the user.** See "What the safeguards cover".
- A system daemon. One detached process drives a mission. It restarts its own engine after a crash but doesn't survive a reboot.
- Token budgets. crew can't measure tokens the same way across harnesses, so budgets are counts.
- Parallel stations inside one factory, a live diagram, cloud runs. These stay out, as in the factories design.

## Anti-goals

- crew causing an effect without the principal's switch or approval.
- A mission that stalls silently while it waits on the principal.
- State that exists only in an LLM's context.
- A layer whose only job is relaying messages.
- The orchestrator writing detailed code plans. Those go stale once earlier pieces merge. Each factory's planning station plans the change, against the code as it stands when the piece starts.
- Claiming a guarantee the mechanism doesn't provide.

## What the safeguards cover

Agent sessions run as the principal's own Unix user, with the principal's GitHub login. So no file, fingerprint or terminal prompt can stop a process that deliberately works around it. The design says so plainly and promises this instead:

> crew itself never causes an effect without the principal's switch or approval, and every session crew starts is launched with rules that refuse the commands that would. This protects against mistakes, drift and a misread prompt. It is not a boundary against a process that sets out to get around it.

Three layers, all required:

1. **crew is the only thing that merges or releases.** No agent prompt asks an agent to do either.
2. **Session rules.** crew already launches each station with a tool allowlist built from its agent's capabilities. This design adds a deny list to every session crew starts, the same way the user's story-orchestrator does (`--settings` with deny rules, no permission prompts):
   - merging or closing a PR, pushing to `main`, force-pushing
   - any `crew automerge` or `crew release approve` command
   - for stations and scoped orchestrators, every `crew run` and `crew mission` command, so a station can't pass an approval stop or answer for the principal
   - writing anywhere in `~/crew`
   - the CLIs of release platforms

   Scoped orchestrators get read tools only. The main orchestrator's shell is limited to `crew` commands where the harness can express that. A harness that can't apply deny rules can't run mission pieces; `crew verify` says which can.

   **The session the principal talks to is started by the principal, not by crew.** `crew compile` writes the orchestrator's rules next to the agent, and the launch line crew prints includes them. Started any other way, that session has only its tool list and its prompt.
3. **Principal-only commands.** `crew automerge` and `crew release approve` need an interactive terminal and a typed confirmation, with no flag and no piped input that skips it. A pseudo-terminal can fake this, which is why layer 2 also denies the commands.

A real boundary would need agents to run under a separate identity (another OS user, or a GitHub account that can't merge). That is out of scope here and is revisited in Project 2, where money moves.

**Merging is not deploying, unless the repo makes it so.** If merging to `main` triggers a deploy, then auto-merge on is auto-deploy. The confirmation text of `crew automerge on` says this.

## Approaches considered

1. **Hierarchy**
   - *Pilots per factory under a boss.* Rejected. A pilot relays status and makes exception calls knowing only its own factory, when the orchestrator has the whole mission.
   - *A receptionist routing to separate domain-orchestrator agents.* Rejected once domains became config. Routing is then part of planning.
   - **Picked:** one orchestrator agent, domains as config, and delegation to copies of itself scoped to a domain.
2. **Bringing pieces back together**
   - *The orchestrator merges into one mission branch as it goes.* Rejected.
   - *Leave the branches for the user to merge.* Rejected.
   - **Picked:** one PR per piece, merged by crew when the principal's switch allows it.
3. **Who plans what**
   - **Picked:** the orchestrator plans the mission. A factory's planning station plans the change, inside the worktree.
4. **How strong the safeguards are**
   - *A separate OS user or GitHub identity for agents.* A real boundary, but GitHub auth, harness auth and worktree permissions all assume one user. Deferred to Project 2.
   - *Prompt instructions only.* Rejected: nothing enforces them.
   - **Picked:** crew-only effects, deny rules on every session, principal-only commands, and an honest statement of what that covers.
5. **Where outward steps are gated**
   - *A principal-only switch for pushing branches too.* Rejected. It taxes every piece to guard a step that can be reviewed and deleted.
   - **Picked:** proposals are automatic, effects are gated.

## How it works

### Domain config

```markdown
~/crew/domains/design.md
---
name: design
description: Brand assets, illustrations, ad creatives, UI mockups.
factories: [design-asset, ui-mockup]
paths: ["brand/**", "assets/**"]   # optional: where this domain's work lives in a repo
releases: [figma-publish]          # releases it may request (Project 2)
---
House style, tone, file formats, what "done" means for design…
```

`crew verify` checks that every listed factory and release exists and that names are unique. The briefing is passed to every run of that domain's factories and to the scoped orchestrator.

### The orchestrator

- **One generated agent.** `crew compile` emits `orchestrator` for each enabled harness that can host a main session.
- **It drives everything through `crew` commands.** It reads domains, factories and the repo. It never edits files, runs a station, merges, or approves. Its tool list has no edit tools, and its shell is limited as described above.
- **It runs two ways.** The principal talks to it in a session: planning, amendments, answers and status. The engine also calls it headless to handle exceptions, so a mission keeps moving with no session open.
- **It reads on demand** (`crew domain list`, `crew factory list|diagram`, `crew mission status`) rather than carrying everything in its prompt.
- **Naming a domain limits planning to it.** For example, "design only: new referral badge".
- **Any orchestrator session in the repo can pick up a running mission**, because the state is on disk. At the start of a session it runs `crew mission status` and reports what is waiting.

### The mission file

crew writes `.crew/missions/<id>/mission.md` from the plan the orchestrator passes to `crew mission plan` on standard input. The orchestrator never edits the file.

- **Frontmatter: the pieces.** Each has:
  - `id`
  - `domain`
  - `factory`, or `delegate: true`
  - `brief`
  - `paths`: where this piece may change files. It defaults to the domain's `paths`. A piece may list paths outside its domain, and that shows in the plan the principal sees.
  - `waits_for: [ids]`
- **Body: the principal's request, word for word.** It's passed unchanged to every level so intent doesn't drift through rewrites.
- **`## Changes`: a dated log.** Every change after the start is appended here: who made it, what changed and why.

### Checks at `crew mission start`

crew refuses the mission unless all of these hold:

- every piece's factory belongs to its domain
- every `waits_for` names a piece that exists, and nothing waits in a circle
- delegation stays within the depth limit
- the piece count is within the mission's budget
- every harness the pieces need can apply deny rules
- the repo has a GitHub remote and `gh` is logged in
- no piece's factory watches folders outside the repo

crew then adjusts and reports:

- **Overlapping paths.** If two pieces that could run together have paths that intersect, crew makes the later one wait for the earlier one and says so. A piece with no paths can't be checked, and crew warns about it.
- **Public repo.** If the repo is public, crew says that piece branches and PRs will be visible.

### Mission approval

**On by default.** After the checks, crew shows the plan and the diagram and waits for the principal's "go". The orchestrator passes that on with `crew mission go <id>`. A copy of the approved plan is kept as `approved.md`.

The principal can turn this off in `~/crew` (`mission_approval: off`) for fully unattended starts. It is a check against misplanning, not a locked gate: stations can't run `crew mission go`, but the main orchestrator can, because that is how the principal's "go" arrives.

### The engine

One detached process per mission, `crew mission run <id>`, drives it. Like the run engine, the engine inside it is a pure state machine plus a store. The process is a small supervisor around that engine: if the engine crashes, it restarts it, up to 3 times, and notifies the principal.

**The mission folder**

| File | What it holds |
|---|---|
| `mission.md` | The current plan, the request and the change log. |
| `approved.md` | The plan as the principal approved it. |
| `state.json` | Each piece's status, run id, branch, worktree, PR number and counters. Written atomically. |
| `events.jsonl` | Every event, appended under a lock, as the run engine does today. |
| `lock` | The pid of the process driving the mission. |
| `inbox/` | Answers from the principal, read on the next tick. |

Run folders stay in the main repo's `.crew/runs`, with each run's work directory set to its worktree. `.crew/` already ignores itself, so none of this reaches a PR.

**The loop.** On each tick (default every 60 seconds) the engine:

1. reads run states, PR and check states through `gh`, and the inbox
2. decides the next actions from the state alone
3. acts: starts a piece, opens a PR, merges, calls the orchestrator, notifies the principal

It polls `gh`; there are no webhooks.

**Events.** Each event has `seq`, `time`, `kind`, `piece`, `run`, `by` (engine, orchestrator, scoped orchestrator or principal), `msg`, and `count`/`max` where a limit is involved. Kinds:

- `mission-start`, `mission-done`, `stopped`, `resumed`
- `piece-queued`, `piece-start`, `piece-stopped`, `piece-complete`, `piece-cancelled`
- `pr-opened`, `checks-passed`, `checks-failed`, `pr-merged`, `conflict`
- `exception`, `decision`, `escalated`
- `amendment`, `piece-added`, `rebrief`
- `waiting-on-principal`, `notified`, `answered`
- `budget-hit`

The wrap-up and `crew mission status` are built from these.

**Stopping and resuming**

- `crew mission stop <id>` ends the driving process and the station processes under it. Worktrees, branches and PRs stay.
- `crew mission resume <id>` checks the mission again, then compares `state.json` with what is real: run folders, `git worktree list` and `gh`. A run whose process is alive is adopted. A dead one is marked interrupted and continued with the existing `crew run resume`. A piece that already has a PR is never run again.
- After a reboot, or once the supervisor has given up, nothing restarts the mission. `crew mission status` says so, and the orchestrator reports it at the start of a session.

**Limits**

All limits live in the crew config in `~/crew`, with an optional override per repo, so the orchestrator can't change them. `crew init` writes them with the defaults below. Each is a number or `"*"`, and a missing one is an error.

| Limit | Default |
|---|---|
| Runs at once per repo, across all its missions | 3 |
| Delegation depth | 2 |
| Pieces per mission, sub-pieces included | 12 |
| Station steps per mission, summed over all pieces | 60 |
| Orchestrator calls per piece | 3 |
| Pieces the orchestrator adds on its own, per mission | 2 |

More than one mission may run in a repo. Hitting a mission's limit stops new work in it and goes straight to the principal.

### When a piece is complete

- Its run finished `done`, **and**
- if it changed files, its PR is merged, **and**
- if it ends in a release (Project 2), the release has gone out.

A piece that changed no files is complete at `done`, with no PR. A delegated piece is complete when every one of its sub-pieces is. `waits_for` always means "wait until complete".

### Scoped orchestrator (recursion)

- **Planning.** For a `delegate: true` piece, crew calls the orchestrator agent headless, with that domain's briefing and factories, the original request and the piece's brief. It replies with a structured block listing the sub-pieces.
- **Checks on what it returns.** crew refuses sub-pieces unless each one stays in the parent's domain, uses that domain's factories, and keeps its paths inside the parent piece's paths. This is what makes "authority only narrows going down" a fact.
- **Ids and waits.** Sub-pieces are named under the parent (`P3.1`, `P3.2`). They may wait on each other, and they inherit everything the parent waits for. They can't name a piece outside their own subtree. If one needs an outside piece, the scoped orchestrator escalates and the wait is added to the parent.
- **No tools beyond read.** crew carries out its decisions.

### Exceptions and authority

**What the engine settles alone**

- Everything inside a factory: routes, loop limits, drift. That is unchanged.
- A failure the run engine marks retryable (a missing CLI, a timeout): resumed automatically, up to 2 times.
- A finished piece that has to go back (see "Work sent back to a factory"): up to 2 times for a conflict or failed checks, once for uncommitted work.

Only when those run out does the engine call an orchestrator.

**Who gets called.** The engine calls the nearest orchestrator headless and reads back one structured decision: the scoped orchestrator for a sub-piece, otherwise the main one. This is the same whether or not the principal has a session open. A session sees the decisions in status.

**What the orchestrator is told.** The summary is fixed, because the orchestrator knows nothing else about the run:

- the mission id and the principal's request, word for word
- the piece: id, domain, factory, brief
- why it stopped: `limit-hit`, `failed`, `conflict`, `checks-failed`, `uncommitted` or `out-of-paths`
- the station, which limit was hit, and its count and maximum
- the routes taken so far, with counts
- the last station's verdict summary and notes
- **every earlier decision on this piece:** what was tried, by whom, and what happened
- the limits that remain
- the actions allowed now

The main orchestrator can also read the full step outputs from the run folder.

**Allowed actions**

- **retry:** run the stopped station again
- **`goto` a station, with a note:** the run continues there and its loop counts start over
- **cancel** a piece
- **re-brief** a piece that hasn't started
- **add** a piece, within the limit (see below)
- **escalate**

Each decision carries a one-line reason and is recorded. **The same action can't be chosen twice for the same failure on the same piece.** The second time, the orchestrator must pick something else or escalate. Anything outside the list escalates: up the tree, then to the principal.

**Why `goto` starts the counts over.** After a loop limit is hit, the route that hit it would fail again on its next use, so a `goto` that kept the counts could do nothing. The extra passes are bounded by the calls-per-piece and steps-per-mission limits. The run engine's `goto` keeps counts today, so this is a new option on it. *(My call.)*

**A station marked `approve: true` always waits for the principal.** That stop exists to ask a person, so no orchestrator can resume it. *(My call.)*

### Changes after the start

Two different things, kept apart:

- **An amendment** is the principal changing what they want ("make it $15"). The orchestrator runs `crew mission amend` with the principal's words, quoted. It may re-brief pieces that haven't started, add pieces and cancel pieces. Amendments aren't capped. crew runs the start checks again.
- **An addition** is the orchestrator adding a piece on its own while handling an exception. It must quote the part of the request the piece serves. Additions are capped per mission (default 2), and the principal is notified at once with what was added. Past the cap, it escalates.

Both append to `## Changes` in the mission file.

### Worktrees, PRs and merging

- **Worktrees.** Each run executes in its own worktree, on a piece branch cut from the latest `main` once everything it waits for is complete. The drift guard works per worktree, unchanged.
- **Commits.** Stations commit their own work. crew never runs `git add`, so it never sweeps up stray files. A piece with no commits and a clean worktree changed no files.
- **PRs.** When a run finishes `done` and nothing below sends it back, crew pushes the branch and opens a PR. This is a proposal, so it is automatic.
- **Merging with auto-merge off.** The PR appears under "waiting on you". The principal merges it on GitHub, crew sees the merge on its next tick, and the pieces waiting on it start.
- **Merging with auto-merge on.** crew watches the PR's checks itself and merges when every check has passed. It does not rely on GitHub's own auto-merge, so the repo needs no branch protection for this to be safe.
- **Cleanup.** A worktree is removed only after its PR is merged or its piece is cancelled. Removal can be repeated safely and is retried on resume.

### Work sent back to a factory

A finished piece can be sent back from outside its factory. It goes to the factory's **`rework` station**, a new optional factory field that defaults to `start`, with a note saying why.

| Why | What crew does | The note carries |
|---|---|---|
| Uncommitted work at `done` | Sends the piece back once. | The files left uncommitted. |
| Files changed outside the piece's `paths` | Stops the piece and opens no PR. The orchestrator may send it back or escalate. Only the principal can accept the extra files. | The files outside the paths. |
| `main` moved and the branch conflicts | Merges `main` into the piece branch. A clean merge is pushed and the checks run again. A conflicting one is undone and the piece goes back. | The conflicting files. |
| Checks failed on the PR | Sends the piece back. | The failing checks and the end of their logs. |

crew merges rather than rebases, so it never needs a force-push. A piece with no paths isn't path-checked.

### The auto-merge switch

- **Set only by the principal:** `crew automerge on|off`, per repo, in a terminal, with a typed confirmation.
- **Three states:**

  | State | What crew does with a finished PR |
  |---|---|
  | `off` (default) | Leaves it for the principal. |
  | `on` | Merges once the PR has at least one check and all checks pass. |
  | `on --without-checks` | Merges as soon as the factory finishes. The factory's own review station is the only gate. |

- **A repo with no checks.** `crew automerge on` refuses and explains the two ways forward: add CI, or use `--without-checks`. That flag has its own confirmation text naming the risk. *(My call: this keeps full automation available in a repo with no CI, as an explicit choice.)*
- **Where it lives.** In `~/crew`, keyed by repo, with a fingerprint written each time `crew automerge` sets it. If the stored value changes any other way, crew treats the switch as off, refuses to merge, and tells the principal.
- **Project 2.** The switch becomes the `merge` kind of release setting: `off` maps to `review`, both `on` states map to `auto`.

### Reaching the principal

- **`crew mission status [id]`** leads with **Waiting on you**: PRs ready to merge, stations waiting for approval, escalated questions and budget stops. Below that: running, queued (and what each waits for), complete.
- **Desktop notification** when something starts waiting on the principal, when the orchestrator adds a piece, and when a mission finishes or its engine crashes. On macOS this uses `osascript`, as the story-orchestrator does. Notifications carry no file content and no secrets.
- **A reminder** if something has waited longer than a set time (default 30 minutes), then once more each time that doubles.
- **Answering.** Each waiting item has an id. The principal answers through the orchestrator ("merge them", "use the dark variant") or directly with `crew mission answer <id>`. Answers land in the mission's inbox and are recorded with the principal's words. The "go", answers, amendments, `approve: true` stops and accepting out-of-paths files all travel this way, through `crew mission go`, `answer` and `amend`.

### Releases (Project 2, summary)

- **Release requests.** Factories in outward-facing domains end with a release request instead of acting: what, where, the exact content, previews, cost and audience.
- **Reviewing.** `crew release show <n>` and `crew release approve <n>`, under the same terminal rules as the switch.
- **Running an approved release.** A fixed action defined once per platform, so what was approved is what runs.
- **Settings.** Each kind of release is `review` (the default), `auto` or `off`. Merges are pre-approved by the standing switch; other releases are approved one at a time unless set to `auto`. Spending always has a cap set by the principal.
- **Still to design there:** running releases under a separate identity.

## Walkthroughs

The repo `acme` holds `app/`, `site/`, `ads/` and `brand/`. Three domains are defined: product, design and marketing. The request: *"Launch referrals: $10 credit per invited friend who signs up, plus a landing page and an ad campaign."*

The plan is the same in both:

- P1: product, credits API, `app/api/**`
- P2: product, invite screen, `app/ui/**`, waits for P1
- P3: design, referral visuals, `brand/**`, **delegated**
- P4: marketing, landing page, `site/**`, waits for P3
- P5: marketing, ad campaign, `ads/**`, waits for P1, P2 and P3

### A. Auto-merge off (the default)

1. **Plan.** The orchestrator shows the plan and diagram. The principal says "go".
2. **Start.** crew starts P1 and P3. The design-scoped orchestrator plans P3.1 (a badge) and P3.2 (three ad creatives, waiting for P3.1).
3. **The principal closes the session.** The mission keeps running.
4. **First PR.** P1 finishes and crew opens PR #12. A notification arrives: "acme: PR #12 is waiting on you."
5. **Nothing happens for 30 minutes.** A reminder arrives. P2 and P5 are queued behind P1.
6. **The principal comes back** and opens a new session. The orchestrator reports: "Waiting on you: PR #12 (credits API), PR #13 (badge). Queued behind them: P2, P3.2, P5."
7. **They merge both on GitHub.** On the next tick crew starts P2 and P3.2 from the updated `main` and removes the two worktrees.
8. **And so on** until every PR is merged. The wrap-up lists PRs, merges and decisions.

### B. Auto-merge on

1. **Once, beforehand.** The principal ran `crew automerge on` in `acme` and typed the confirmation.
2. **Plan and start,** as above.
3. **Merging.** P1 opens PR #12. Its checks pass and crew merges it. P2 starts from the updated `main`. Nobody is asked.
4. **A problem.** P3.2's review hits its limit. The design-scoped orchestrator sends it back to the planner once. It fails the same way. It can't choose the same action again, so it escalates, and the main orchestrator has no better option. The principal is notified: "use the dark variant, or drop 9:16?"
5. **An amendment.** The principal answers, and adds "make it $15". P1 is already merged, so the orchestrator amends the mission: P6 (change the credit to $15) is added, and P5, which hasn't started, gets a new brief. Both go in the change log.
6. **An out-of-paths stop.** P2 finishes but also edited `brand/logo.svg`. No PR opens. The orchestrator sends it back to the builder, which drops the edit.
7. **The release (Project 2).** P5 ends with release request #7: Meta Ads, three creatives, US 25–44, $200 a day, $2,000 cap. The principal reviews and approves it in a terminal.
8. **The wrap-up.** PRs, merges, the release, the amendment, and each call the orchestrators made with its reason.

## Build order

**Project 1,** in stages that each work on their own:

1. **Missions, one piece at a time.** Domains, the `orchestrator` agent, the mission file and its checks, mission approval, the engine (state, events, stop, resume), session deny rules, worktrees, one PR per piece, waiting for the principal's merge, status, notifications, exceptions with the fixed summary. Concurrency is 1. Foremen still exist.
2. **Auto-merge.** The switch, check watching, merging, conflict handling.
3. **Parallel pieces.** Concurrency above 1, the overlap check, the path check, budgets.
4. **Delegation.** Scoped orchestrators, sub-pieces and their checks.
5. **Cutover.** The orchestrator takes over the foremen's job of driving plain runs, and the `foreman-*` agents are removed. `crew run` itself stays as it is, for factories that belong to no domain, watch folders outside the repo, or run in a project without GitHub.

**Project 2:** releases, with requests, previews, terminal approval, per-platform actions, `review`/`auto`/`off`, budget caps, and a separate identity for running them.

Each project gets its own plan.

## Testing the planner

Planning quality can't be unit-tested, so the repo keeps **golden missions**: a request and a set of domains, with the domains, pieces and waits a good plan should contain. They run on demand, not in CI, because each run calls a model. They are also the cheapest way to find a badly written domain description.

## Known weak spots

1. **The safeguards are not a boundary.** A session that sets out to merge or to rewrite the switch can. See "What the safeguards cover".
2. **Checks are only as good as the repo's CI.** With `--without-checks`, the factory's review station is the only thing between an agent's work and `main`.
3. **The orchestrator's context grows with mission size.** Reading on demand and exception-only reporting slow this down. Very large missions need delegation.
4. **One exception-handler can make the same kind of mistake on every piece.** The no-repeat rule, the earlier-decisions list and the per-piece call budget limit it but don't remove it.
5. **Context is lost at each delegation hop.** Passing the request down word for word reduces this.
6. **Wrong domain picks** depend on how clearly domain descriptions are written. Golden missions are the check.
7. **Whether an added piece really serves the request is still the orchestrator's judgment.** It is capped, quoted, logged and notified, not prevented.
8. **Pushing publishes in a public repo.** crew warns at mission start and does nothing more.
9. **Answers given through the orchestrator are relayed by an agent.** They are recorded word for word, but nothing proves the principal said them.
10. **crew pushes whatever stations commit.** It doesn't scan for secrets or stray files.
11. **A reboot stops a mission** until someone runs status or opens a session.

## Left to the builder

- The exact field names in the mission and domain files.
- Where worktrees live (proposal: `.crew/worktrees/<mission>-<piece>`).
- The merge method (squash or merge commit).
- The exact deny rules per harness, written from the bullets in "What the safeguards cover".
- Notifications on systems other than macOS.
- How the `foreman-*` links are removed during compile.
