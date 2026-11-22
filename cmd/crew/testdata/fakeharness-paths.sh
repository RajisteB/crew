#!/bin/sh
# Fake station harness for the parallel-pieces end-to-end tests. It reads the
# prompt, finds "Touch <dir>" in the piece's brief and commits one file in that
# folder of its work directory ("Touch ." means the top folder), so a test
# decides which files a piece changes. The plan station commits nothing.
# $1 is the agent name: epic-planner replies "ready", anything else "done".
agent="$1"
prompt="$(cat)"
if [ "$agent" = "epic-planner" ]; then
  route=ready
else
  dir="$(printf '%s' "$prompt" | sed -n 's/.*Touch \([^ ."]*\).*/\1/p' | head -n 1)"
  [ -n "$dir" ] || dir=.
  mkdir -p "$dir"
  printf 'built by %s\n' "$agent" > "$dir/$(basename "$PWD").txt"
  git add -A
  git commit -q -m "station $agent"
  route=done
fi
printf 'did the work\n\n```verdict\n{"route":"%s"}\n```\n' "$route"
