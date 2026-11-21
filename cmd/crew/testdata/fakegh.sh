#!/bin/sh
# Fake gh for the mission end-to-end test. State lives in $FAKE_GH_DIR:
#   pr-number   the number of the pull request, once one is created
#   pr-state    OPEN, MERGED or CLOSED (the test writes MERGED to merge it)
#   pr-body     the body read from stdin by `pr create`
#   pr-args     the arguments of `pr create`, one per line
#   calls       every invocation, one per line
dir="$FAKE_GH_DIR"
url="https://github.com/acme/app/pull/7"
printf '%s\n' "$*" >> "$dir/calls"
case "$1 $2" in
"repo view")
  printf '{"nameWithOwner":"acme/app","visibility":"PRIVATE","defaultBranchRef":{"name":"main"}}\n' ;;
"pr create")
  cat > "$dir/pr-body"
  for a in "$@"; do printf '%s\n' "$a"; done > "$dir/pr-args"
  echo 7 > "$dir/pr-number"
  echo OPEN > "$dir/pr-state"
  echo "$url" ;;
"pr view")
  printf '{"number":7,"state":"%s","url":"%s"}\n' "$(cat "$dir/pr-state")" "$url" ;;
"pr list")
  if [ -f "$dir/pr-number" ]; then
    printf '[{"number":7,"state":"%s","url":"%s"}]\n' "$(cat "$dir/pr-state")" "$url"
  else
    echo '[]'
  fi ;;
*)
  echo "fakegh: unexpected: $*" >&2
  exit 1 ;;
esac
