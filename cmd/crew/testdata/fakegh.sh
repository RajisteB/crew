#!/bin/sh
# Fake gh for the mission end-to-end tests. State lives in $FAKE_GH_DIR:
#   pr-number   the number of the pull request, once one is created
#   pr-state    OPEN, MERGED or CLOSED (a test may write MERGED to merge it)
#   pr-branch   the head branch given to `pr create`
#   pr-body     the body read from stdin by `pr create`
#   pr-args     the arguments of `pr create`, one per line
#   checks      written by a test: one state per line (pass, fail or pending),
#               each shown as one check; absent or empty means no checks
#   calls       every invocation, one per line
# $FAKE_GH_ORIGIN is the bare origin repo. The head of the pull request is its
# branch there; `pr merge` moves the origin's main to that head.
dir="$FAKE_GH_DIR"
url="https://github.com/acme/app/pull/7"
printf '%s\n' "$*" >> "$dir/calls"

head_sha() {
  [ -f "$dir/pr-branch" ] && [ -n "$FAKE_GH_ORIGIN" ] || return 0
  git --git-dir="$FAKE_GH_ORIGIN" rev-parse --verify -q "refs/heads/$(cat "$dir/pr-branch")" || true
}

rollup() {
  [ -s "$dir/checks" ] || { printf '[]'; return; }
  n=0 sep=''
  printf '['
  while read -r state; do
    [ -n "$state" ] || continue
    n=$((n + 1))
    case "$state" in
    pass) st=COMPLETED; concl=SUCCESS ;;
    pending) st=IN_PROGRESS; concl='' ;;
    *) st=COMPLETED; concl=FAILURE ;;
    esac
    printf '%s{"__typename":"CheckRun","name":"check-%s","status":"%s","conclusion":"%s","detailsUrl":"https://ci.example/%s"}' "$sep" "$n" "$st" "$concl" "$n"
    sep=,
  done < "$dir/checks"
  printf ']'
}

merge() {
  match=''
  while [ $# -gt 0 ]; do
    [ "$1" = "--match-head-commit" ] && match="$2"
    shift
  done
  sha="$(head_sha)"
  if [ -z "$sha" ] || [ "$match" != "$sha" ]; then
    echo "fakegh: head is $sha, not $match" >&2
    exit 1
  fi
  git --git-dir="$FAKE_GH_ORIGIN" update-ref refs/heads/main "$sha"
  echo MERGED > "$dir/pr-state"
}

case "$1 $2" in
"repo view")
  printf '{"nameWithOwner":"acme/app","visibility":"PRIVATE","defaultBranchRef":{"name":"main"}}\n' ;;
"pr create")
  cat > "$dir/pr-body"
  for a in "$@"; do printf '%s\n' "$a"; done > "$dir/pr-args"
  for a in "$@"; do case "$a" in --head=*) echo "${a#--head=}" > "$dir/pr-branch" ;; esac; done
  echo 7 > "$dir/pr-number"
  echo OPEN > "$dir/pr-state"
  echo "$url" ;;
"pr view")
  printf '{"number":7,"state":"%s","url":"%s","mergeable":"MERGEABLE","headRefOid":"%s","statusCheckRollup":%s}\n' \
    "$(cat "$dir/pr-state")" "$url" "$(head_sha)" "$(rollup)" ;;
"pr merge")
  shift 2
  merge "$@" ;;
"pr list")
  want=''
  for a in "$@"; do
    case "$a" in --head=*) want="${a#--head=}" ;; esac
  done
  if [ -f "$dir/pr-number" ] && { [ -z "$want" ] || [ "$want" = "$(cat "$dir/pr-branch")" ]; }; then
    printf '[{"number":7,"state":"%s","url":"%s"}]\n' "$(cat "$dir/pr-state")" "$url"
  else
    echo '[]'
  fi ;;
*)
  echo "fakegh: unexpected: $*" >&2
  exit 1 ;;
esac
