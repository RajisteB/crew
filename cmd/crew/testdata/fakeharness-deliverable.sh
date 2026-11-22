#!/bin/sh
# Fake station harness for the Jira and deliverable end-to-end tests. It
# commits nothing. The build station of a piece with no inputs writes
# .crew/deliverables/brief.md; the build station of a piece that has
# .crew/inputs/P1/brief.md copies it to $FAKE_MARK_DIR/saw-input and writes
# its own deliverable. $1 is the agent name: epic-planner replies "ready",
# anything else "done".
agent="$1"
cat > /dev/null
if [ "$agent" = "epic-planner" ]; then
  route=ready
else
  mkdir -p .crew/deliverables
  if [ -r .crew/inputs/P1/brief.md ]; then
    cat .crew/inputs/P1/brief.md > "$FAKE_MARK_DIR/saw-input"
    printf 'second piece result\n' > .crew/deliverables/result.md
  else
    printf 'the brief\n' > .crew/deliverables/brief.md
  fi
  route=done
fi
printf 'did the work\n\n```verdict\n{"route":"%s"}\n```\n' "$route"
