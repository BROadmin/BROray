#!/opt/bin/ash
# Short serialized publication/removal. The control guard is never unlinked.
set -u
umask 077
[ "$#" -ge 4 ] && [ "${BRORAY_OPS_GUARD_HELD:-}" = 1 ] || exit 73
lock="$1"; caller="$2"; action="$3"; shift 3
case "$lock" in /*/operation.lock) ;; *) exit 64 ;; esac
parent="${lock%/*}"
[ -d "$parent" ] && [ "$(readlink -f "$parent")" = "$parent" ] || exit 74
held=false
for fd in /proc/$$/fd/*; do
    if [ "$(readlink "$fd" 2>/dev/null)" = "$parent/resource.control.guard" ]; then held=true; break; fi
done
[ "$held" = true ] || exit 73
OPS_PROC=/proc; OPS_APP="${BRORAY_ROOT:-/opt/broray}"
unset BRORAY_OPS_TEST_IDENTITIES
. "$OPS_APP/lib/operation-owner.sh" || exit 74
if [ "$action" = acquire ]; then caller="${BRORAY_ROUTE_RESOURCE_CALLER:-}"; fi
owner="$(broray_ops_capture_owner "$caller")" || exit 75
safe_file()
{
    [ -f "$1" ] && [ ! -L "$1" ] && [ "$(wc -c <"$1")" -le 4096 ] &&
      [ "$(find "$1" -maxdepth 0 -type f -links 1 -print)" = "$1" ]
}
job=null
if [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ]; then
    context="${BRORAY_ROUTE_RESOURCE_JOB_CONTEXT:-null}"
    [ "${#context}" -le 4096 ] || exit 73
    id="$BRORAY_BACKGROUND_OPERATION_ID"
    case "$id" in ''|.*|-*|*[!A-Za-z0-9._-]*) exit 73 ;; esac
    [ "${#id}" -le 96 ] || exit 73
    digest="$(printf '%s' "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" | sha256sum | cut -d ' ' -f 1)" || exit 74
    job="$(printf '%s\n' "$context" | jq -ces --arg id "$id" --arg digest "$digest" '
      select(length==1)|.[0]|select(.ok==true and .operationId==$id and .jobTokenDigest==$digest)|
      {operationId,supervisorId,jobTokenDigest,supervisorOwner,action,bundleId}')" || exit 73
    directory="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/operations/$id"
    safe_file "$directory/route-supervision.json" && safe_file "$directory/owner.json" && safe_file "$directory/state.json" || exit 73
    [ "$(jq -r .token "$directory/owner.json")" = "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] || exit 73
    jq -e --argjson job "$job" '.schemaVersion==1 and .kind=="protected-route-supervision" and
      .operationId==$job.operationId and .supervisorId==$job.supervisorId and .owner==$job.supervisorOwner' \
      "$directory/route-supervision.json" >/dev/null || exit 73
    jq -e --argjson job "$job" '.running==true and .acknowledged==true and .scope=="routes" and
      .cancelability=="protected" and .operation==$job.action and (.bundleId // "")==$job.bundleId' \
      "$directory/state.json" >/dev/null || exit 73
    supervisor="$(printf '%s\n' "$job" | jq -c .supervisorOwner)"
    broray_ops_classify_owner "$supervisor"
    [ "$OPS_OWNER_STATUS" = ACTIVE ] || exit 73
    sid="$(printf '%s\n' "$job" | jq -r .supervisorId)"
    case "$sid" in ''|*[!0-9a-f]*) exit 73 ;; esac
    [ "${#sid}" = 32 ] || exit 73
    ledger="${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}/supervisors/$id/$sid/children.json"
    [ -f "$ledger" ] && [ ! -L "$ledger" ] && [ "$(wc -c <"$ledger")" -le 65536 ] || exit 73
    # Both the lease owner and this publisher must be members of the same
    # traced tree. A copied token/context or caller PID cannot create a lease.
    for pid in "$caller" "$$"; do
        member="$(broray_ops_capture_owner "$pid")" || exit 73
        [ "$(awk '$1=="TracerPid:"{print $2}' "/proc/$pid/status")" = "$(printf '%s\n' "$supervisor" | jq -r .pid)" ] || exit 73
        jq -e --arg id "$id" --arg sid "$sid" --argjson owner "$member" --argjson supervisor "$supervisor" '
          .schemaVersion==1 and .operationId==$id and .supervisorId==$sid and
          .supervisorPid==$supervisor.pid and .supervisorStartTicks==$supervisor.startTicks and .bootId==$supervisor.bootId and
          any(.children[]; .pid==$owner.pid and .startTicks==$owner.startTicks and .bootId==$owner.bootId)' "$ledger" >/dev/null || exit 73
    done
fi
case "$action" in
  acquire)
    [ "$#" = 2 ] || exit 64
    operation="$1"; bundle="$2"
    case "$operation" in check|download|build-export|preflight|sync|export|delete|user-import) ;; *) exit 64 ;; esac
    case "$bundle" in *[!a-z0-9_-]*) exit 64 ;; esac
    [ "${#bundle}" -le 63 ] || exit 64
    if [ "$job" != null ]; then
        printf '%s\n' "$job" | jq -e --arg operation "$operation" --arg bundle "$bundle" '
          if $operation=="user-import" then ($bundle=="" and (.action|startswith("custom:")))
          else .bundleId==$bundle end' >/dev/null || exit 73
    fi
    # Neither PID absence nor a previous boot proves that legacy helpers and
    # domain mutations completed. Every existing generation stays untouched.
    [ ! -e "$lock" ] && [ ! -L "$lock" ] || exit 2
    token="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || exit 74
    [ "${#token}" = 32 ] || exit 74
    mkdir -m 700 "$lock" || exit 74
    printf '%s\n' "$caller" >"$lock/pid" || exit 74
    printf '%s\n' "$operation" >"$lock/action" || exit 74
    printf '%s\n' "$bundle" >"$lock/bundle" || exit 74
    started=''
    if [ "$operation" = delete ]; then
        started="$(date '+%Y-%m-%dT%H:%M:%S%z')" || exit 74
        printf '%s\n' delete >"$lock/operation" || exit 74
        printf '%s\n' "$started" >"$lock/startedAt" || exit 74
    fi
    jq -nc --arg token "$token" --argjson owner "$owner" --arg action "$operation" --arg bundle "$bundle" --arg started "$started" --argjson job "$job" \
      '{schemaVersion:1,kind:"route-resource-lock",token:$token,owner:$owner,action:$action,bundle:$bundle,startedAt:$started,job:$job}' >"$lock/owner.json.pending" || exit 74
    "${BRORAY_OPS_GUARD:-$OPS_APP/bin/broray-ops-guard}" --replace-file "$lock/owner.json.pending" "$lock/owner.json" || exit 74
    printf '%s\n' "$token" ;;
  release)
    [ "$#" = 1 ] || exit 64
    token="$1"
    case "$token" in ''|*[!0-9a-f]*) exit 73 ;; esac
    [ "${#token}" = 32 ] || exit 73
    [ -d "$lock" ] && [ ! -L "$lock" ] && [ "$(readlink -f "$lock")" = "$lock" ] || exit 75
    safe_file "$lock/owner.json" || exit 75
    record="$(jq -ces --arg token "$token" --argjson owner "$owner" --argjson job "$job" '
      select(length==1)|.[0]|select(.schemaVersion==1 and .kind=="route-resource-lock" and .token==$token and .owner==$owner and (.job // null)==$job)' "$lock/owner.json")" || exit 75
    operation="$(printf '%s\n' "$record" | jq -r .action)"
    count=0
    for file in "$lock"/* "$lock"/.[!.]* "$lock"/..?*; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        case "${file##*/}" in
          owner.json|pid|action|bundle) ;;
          operation|startedAt) [ "$operation" = delete ] || exit 75 ;;
          *) exit 75 ;;
        esac
        safe_file "$file" || exit 75
        count=$((count+1))
    done
    if [ "$operation" = delete ]; then
        [ "$count" = 6 ] && [ "$(cat "$lock/operation")" = delete ] &&
          [ "$(cat "$lock/startedAt")" = "$(printf '%s\n' "$record" | jq -r .startedAt)" ] || exit 75
    else [ "$count" = 4 ] || exit 75; fi
    [ "$(cat "$lock/pid")" = "$caller" ] &&
      [ "$(cat "$lock/action")" = "$(printf '%s\n' "$record" | jq -r .action)" ] &&
      [ "$(cat "$lock/bundle")" = "$(printf '%s\n' "$record" | jq -r .bundle)" ] || exit 75
    rm "$lock/pid" "$lock/action" "$lock/bundle" || exit 74
    if [ "$operation" = delete ]; then rm "$lock/operation" "$lock/startedAt" || exit 74; fi
    rm "$lock/owner.json" && rmdir "$lock" || exit 74 ;;
  *) exit 64 ;;
esac
