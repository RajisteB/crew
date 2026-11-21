#!/bin/sh
# Fake station harness for the mission end-to-end test. Like
# internal/run/testdata/fakeharness.sh it reads the prompt from stdin; it
# then commits one file in its work directory and replies with a verdict.
# $1 is the agent name: epic-planner replies "ready", anything else "done".
agent="$1"
cat > /dev/null
if [ "$agent" = "epic-planner" ]; then
  printf 'plan for %s\n' "$(basename "$PWD")" > plan.txt
  git add plan.txt
  route=ready
else
  printf 'built by %s in %s\n' "$agent" "$(basename "$PWD")" >> "built-$(basename "$PWD").txt"
  git add .
  route=done
fi
git commit -q -m "station $agent"
printf 'did the work\n\n```verdict\n{"route":"%s"}\n```\n' "$route"
