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
