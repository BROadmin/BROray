#!/opt/bin/ash
# Called only below the coordinator and resource guards, after helper drain.
# Archive a complete job-bound generation; never infer ownership from a PID.
set -eu
umask 077
[ "$#" = 3 ] && [ "${BRORAY_OPS_GUARD_HELD:-}" = 1 ] || exit 73
lock="$1"; id="$2"; mode="$3"
OPS_APP="${BRORAY_ROOT:-/opt/broray}"
state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
[ "$lock" = "$OPS_APP/routes/locks/operation.lock" ] || exit 73
case "$id" in ''|.*|-*|*[!A-Za-z0-9._-]*) exit 73 ;; esac
[ "${#id}" -le 96 ] || exit 73
case "$mode" in check|retire) ;; *) exit 64 ;; esac
parent="${lock%/*}"; directory="$state/operations/$id"
for path in "$parent" "$directory"; do
    [ -d "$path" ] && [ "$(readlink -f "$path")" = "$path" ] || exit 73
done
for guard in "$state/operations.guard" "$parent/resource.control.guard"; do
    held=false
    for fd in /proc/$$/fd/*; do
        if [ "$(readlink "$fd" 2>/dev/null)" = "$guard" ]; then held=true; break; fi
    done
    [ "$held" = true ] || exit 73
done
OPS_PROC=/proc
unset BRORAY_OPS_TEST_IDENTITIES
. "$OPS_APP/lib/operation-owner.sh"
safe_file() {
    [ -f "$1" ] && [ ! -L "$1" ] && [ "$(wc -c <"$1")" -le 4096 ] &&
      [ "$(find "$1" -maxdepth 0 -type f -links 1 -print)" = "$1" ]
}
for name in owner.json state.json route-supervision.json supervisors.json; do
    safe_file "$directory/$name" || exit 75
done
jq -e '.schemaVersion==1 and .supervisors==[]' "$directory/supervisors.json" >/dev/null || exit 75
digest="$(jq -jr .token "$directory/owner.json" | sha256sum | cut -d ' ' -f 1)"
job="$(jq -nce --arg id "$id" --arg digest "$digest" --slurpfile marker "$directory/route-supervision.json" --slurpfile state "$directory/state.json" '
  $marker[0] as $m | $state[0] as $s |
  select($m.schemaVersion==1 and $m.kind=="protected-route-supervision" and $m.operationId==$id and
    $s.operationId==$id and $s.scope=="routes" and $s.cancelability=="protected" and $s.acknowledged==true) |
  {operationId:$id,supervisorId:$m.supervisorId,jobTokenDigest:$digest,supervisorOwner:$m.owner,action:$s.operation,bundleId:$s.bundleId}')" || exit 75
for record in "$(jq -c .owner "$directory/owner.json")" "$(jq -c .owner "$directory/route-supervision.json")"; do
    broray_ops_classify_owner "$record"
    [ "$OPS_OWNER_STATUS" = STALE ] || exit 75
done
archive="$parent/recovered-$id"
if [ -e "$lock" ] || [ -L "$lock" ]; then
    [ ! -e "$archive" ] && [ ! -L "$archive" ] || exit 75
    generation="$lock"
elif [ -e "$archive" ] || [ -L "$archive" ]; then
    generation="$archive"
else
    exit 0
fi
[ -d "$generation" ] && [ "$(readlink -f "$generation")" = "$generation" ] || exit 75
safe_file "$generation/owner.json" || exit 75
record="$(jq -ces --argjson job "$job" '
  select(length==1)|.[0]|select(.schemaVersion==1 and .kind=="route-resource-lock" and .job==$job and
    (.token|type)=="string" and (.token|length)==32 and
    (.token|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))))' "$generation/owner.json")" || exit 75
broray_ops_classify_owner "$(printf '%s\n' "$record" | jq -c .owner)"
[ "$OPS_OWNER_STATUS" = STALE ] || exit 75
operation="$(printf '%s\n' "$record" | jq -r .action)"
case "$operation" in check|download|build-export|preflight|sync|export|delete|user-import) ;; *) exit 75 ;; esac
count=0
for file in "$generation"/* "$generation"/.[!.]* "$generation"/..?*; do
    [ -e "$file" ] || [ -L "$file" ] || continue
    case "${file##*/}" in owner.json|pid|action|bundle) ;;
      operation|startedAt) [ "$operation" = delete ] || exit 75 ;;
      *) exit 75 ;;
    esac
    safe_file "$file" || exit 75
    count=$((count+1))
done
if [ "$operation" = delete ]; then
    [ "$count" = 6 ] && [ "$(cat "$generation/operation")" = delete ] &&
      [ "$(cat "$generation/startedAt")" = "$(printf '%s\n' "$record" | jq -r .startedAt)" ] || exit 75
else [ "$count" = 4 ] || exit 75; fi
[ "$(cat "$generation/pid")" = "$(printf '%s\n' "$record" | jq -r .owner.pid)" ] &&
  [ "$(cat "$generation/action")" = "$operation" ] &&
  [ "$(cat "$generation/bundle")" = "$(printf '%s\n' "$record" | jq -r .bundle)" ] || exit 75
[ "$mode" = retire ] || exit 0
guard="${BRORAY_OPS_GUARD:-$OPS_APP/bin/broray-ops-guard}"
"$guard" --sync-state "$generation/owner.json" || exit 74
if [ "$generation" = "$lock" ]; then mv "$lock" "$archive" || exit 74; fi
"$guard" --sync-state "$parent/resource.control.guard"
