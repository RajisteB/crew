#!/bin/sh
# Fake harness CLI for tests.
#   FAKE_MODE=echo   print argv (one per line) then stdin
#   FAKE_MODE=sleep  sleep 5 seconds
#   FAKE_MODE=exit3  write to stderr and exit 3
#   otherwise        reply with the file $FAKE_REPLY_DIR/<n>, where n counts
#                    calls (1, 2, ...). The prompt read from stdin (or given
#                    as the last argument) is saved as $FAKE_REPLY_DIR/prompt-<n>.
case "$FAKE_MODE" in
echo)
  for a in "$@"; do printf 'ARG %s\n' "$a"; done
  printf 'STDIN '; cat
  exit 0 ;;
sleep)
  sleep 5
  exit 0 ;;
exit3)
  echo "boom: not logged in" >&2
  exit 3 ;;
stdoutfail)
  echo "Please run /login first"
  exit 1 ;;
json)
  cat > /dev/null
  printf '{"type":"result","is_error":false,"result":"hello from json","permission_denials":[{"tool_name":"Bash","tool_input":{"command":"rm -rf x"}}]}\n'
  exit 0 ;;
jsonerr)
  cat > /dev/null
  printf '{"type":"result","is_error":true,"result":"API overloaded","permission_denials":[]}\n'
  exit 0 ;;
copysettings)
  cat > /dev/null
  prev=""
  for a in "$@"; do
    if [ "$prev" = "--settings" ]; then
      cp "$a" "$FAKE_COPY_TO"
      printf '%s' "$a" > "$FAKE_COPY_TO.path"
      ls -l "$a" | cut -c2-10 | awk '{m=0; for(i=0;i<3;i++){s=substr($0,i*3+1,3); v=0; if(substr(s,1,1)=="r")v+=4; if(substr(s,2,1)=="w")v+=2; if(substr(s,3,1)=="x")v+=1; m=m v}; print m}' > "$FAKE_COPY_TO.mode"
    fi
    prev="$a"
  done
  [ -n "$FAKE_EXIT" ] && { echo "boom" >&2; exit "$FAKE_EXIT"; }
  printf 'ok'
  exit 0 ;;
esac
dir="$FAKE_REPLY_DIR"
n=$(cat "$dir/counter" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" > "$dir/counter"
if [ "$FAKE_PROMPT_VIA" = "arg" ]; then
  for last in "$@"; do :; done
  printf '%s' "$last" > "$dir/prompt-$n"
else
  cat > "$dir/prompt-$n"
fi
if [ -f "$dir/$n" ]; then
  cat "$dir/$n"
else
  echo "no scripted reply $n" >&2
  exit 1
fi
