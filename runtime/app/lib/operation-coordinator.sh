#!/opt/bin/ash
# All writes run synchronously inside broray-ops-guard. No router/network calls.
set -u
umask 077
OPS_APP="${BRORAY_ROOT:-/opt/broray}"
OPS_STATE="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
OPS_ROOT="$OPS_STATE/operations"
OPS_GLOBAL="${BRORAY_ROUTES_API_LOCK:-${BRORAY_GLOBAL_LOCK:-/opt/var/lock/broray/global-operation.lock}}"
OPS_PROC="${BRORAY_OPS_PROC_ROOT:-/proc}"
OPS_UPDATER="${BRORAY_OPS_UPDATER_ROOT:-/opt/var/lib/broray-updater}"
OPS_LEGACY="${BRORAY_LEGACY_GLOBAL_LOCK:-/tmp/broray-global-operation.lock}"
OPS_AUTOMATION="$OPS_STATE/background-automation.json"
OPS_GUARD="${BRORAY_OPS_GUARD:-$OPS_APP/bin/broray-ops-guard}"
OPS_RAM="${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}"
[ "${BRORAY_OPS_GUARD_HELD:-0}" = 1 ] || exit 73
. "$OPS_APP/lib/operation-owner.sh"
. "$OPS_APP/lib/operation-journal.sh"
. "$OPS_APP/lib/operation-report.sh"
. "$OPS_APP/lib/operation-publication.sh"
. "$OPS_APP/lib/operation-route-recovery.sh"

ops_error()
{
    jq -nc --arg code "$1" '{ok:false,errorCode:$code}'
    exit "${2:-2}"
}
ops_id_valid() { case "${1:-}" in ''|.*|-*|*[!A-Za-z0-9._-]*) return 1 ;; esac; [ "${#1}" -le 96 ]; }
ops_nonce_valid() { case "${1:-}" in ''|*[!0-9a-f]*) return 1 ;; esac; [ "${#1}" = 32 ]; }
ops_dir_safe() { [ -d "$1" ] && [ ! -L "$1" ]; }
ops_file_safe() { [ -f "$1" ] && [ ! -L "$1" ] && [ "$(wc -c <"$1")" -le "${2:-32768}" ]; }
ops_now() { date -u '+%Y-%m-%dT%H:%M:%SZ'; }
ops_monotonic() { awk 'NR==1{print $1;exit}' "$OPS_PROC/uptime" 2>/dev/null; }

ops_launch_test_point()
{
    # Only the isolated test root can inject a crash of this coordinator.
    if [ "${BRORAY_OPS_TEST:-0}" = 1 ] && [ "$OPS_APP" != /opt/broray ] &&
       [ "${BRORAY_OPS_TEST_LAUNCH_CRASH:-}" = "$1" ]; then kill -KILL "$$"; fi
    return 0
}

ops_discard_launch_stage()
{
    local dir entry nested name suffix pid nonce
    dir="$1"; ops_dir_safe "$dir" || return 1
    case "$dir" in "$OPS_ROOT/.launch-"*) ;; *) return 1 ;; esac
    name="${dir##*/}"; name="${name#.launch-}"; pid="${name%%-*}"; nonce="${name#*-}"
    case "$pid" in ''|*[!0-9]*|0|1) return 1 ;; esac
    ops_nonce_valid "$nonce" || return 1
    # Hidden launch directories cannot be operation IDs, and this function
    # runs under the exclusive coordinator guard. No child can have been
    # admitted from this staging namespace. Preserve every unknown object.
    [ ! -L "$OPS_GLOBAL" ] || [ "$(readlink "$OPS_GLOBAL")" != "$dir/fence" ] || return 1
    if [ -e "$dir/owner.json" ] || [ -L "$dir/owner.json" ]; then
        ops_file_safe "$dir/owner.json" 4096 && jq -e --arg pid "$pid" --arg nonce "$nonce" \
          '.schemaVersion==2 and .launchNonce==$nonce and (.owner.pid|tostring)==$pid' "$dir/owner.json" >/dev/null || return 1
        jq -c '.owner' "$dir/owner.json" | broray_ops_owner_valid || return 1
    fi
    if [ -e "$dir/state.json" ] || [ -L "$dir/state.json" ]; then
        ops_file_safe "$dir/owner.json" 4096 && ops_file_safe "$dir/state.json" && jq -e \
          '.schemaVersion==2 and .kind=="background" and .state=="starting" and .running==true and .acknowledged==false' "$dir/state.json" >/dev/null || return 1
    fi
    for entry in "$dir"/* "$dir"/.[!.]* "$dir"/..?*; do
        [ -e "$entry" ] || [ -L "$entry" ] || continue
        name="${entry##*/}"
        if [ "$name" = fence ]; then
            ops_dir_safe "$entry" || return 1
            for nested in "$entry"/* "$entry"/.[!.]* "$entry"/..?*; do
                [ -e "$nested" ] || [ -L "$nested" ] || continue
                case "${nested##*/}" in pid|scope|action|bundle|startedAt|owner.json) ;; owner.json.tmp.*)
                    suffix="${nested##*.}"; case "$suffix" in ''|*[!0-9]*) return 1 ;; esac ;; *) return 1 ;; esac
                ops_file_safe "$nested" 4096 && [ "$(find "$nested" -maxdepth 0 -type f -links 1 -print)" = "$nested" ] || return 1
            done
        else
            case "$name" in owner.json|state.json) ;; owner.json.tmp.*|state.json.tmp.*)
                suffix="${name##*.}"; case "$suffix" in ''|*[!0-9]*) return 1 ;; esac ;; *) return 1 ;; esac
            ops_file_safe "$entry" && [ "$(find "$entry" -maxdepth 0 -type f -links 1 -print)" = "$entry" ] || return 1
        fi
    done
    # The entire directory passed the allowlist before the first unlink.
    if [ -d "$dir/fence" ]; then rm -f "$dir/fence"/*; rmdir "$dir/fence" || return 1; fi
    rm -f "$dir"/*; rmdir "$dir"
}

ops_prune_launch_stages()
{
    local dir name pid nonce count
    count=0
    for dir in "$OPS_ROOT"/.launch-*; do
        [ -e "$dir" ] || [ -L "$dir" ] || continue
        count=$((count+1)); [ "$count" -le 128 ] || break
        name="${dir##*/}"; name="${name#.launch-}"; pid="${name%%-*}"; nonce="${name#*-}"
        case "$pid" in ''|*[!0-9]*|0|1) continue ;; esac
        ops_nonce_valid "$nonce" || continue
        ops_discard_launch_stage "$dir" || continue
    done
}

ops_write()
{
    local path json tmp
    path="$1"; json="$2"; tmp="$path.tmp.$$"
    [ ! -L "$path" ] && { [ ! -e "$path" ] || [ -f "$path" ]; } || return 1
    [ ! -e "$tmp" ] && [ ! -L "$tmp" ] || return 1
    (set -C; printf '%s\n' "$json" >"$tmp") || return 1
    jq -e 'type=="object"' "$tmp" >/dev/null 2>&1 || { rm -f "$tmp"; return 1; }
    chmod 600 "$tmp" 2>/dev/null || true
    case "$path" in
        "$OPS_RAM/"*) mv -f "$tmp" "$path" ;;
        *) "$OPS_GUARD" --replace-file "$tmp" "$path" ;;
    esac
}

ops_load()
{
    local id dir
    id="$1"; ops_id_valid "$id" || return 1
    dir="$OPS_ROOT/$id"; ops_dir_safe "$dir" || return 1
    ops_file_safe "$dir/owner.json" 4096 && ops_file_safe "$dir/state.json" || return 1
    jq -e --arg id "$id" 'type=="object" and .schemaVersion==2 and .kind=="background" and .operationId==$id and
      (.revision|type)=="number" and (.running|type)=="boolean" and (.resourceLocks|type)=="array"' "$dir/state.json" >/dev/null 2>&1 || return 1
    jq -e --arg id "$id" 'type=="object" and .schemaVersion==2 and .operationId==$id and
      (.token|type)=="string" and (.token|length)==32 and
      (.token|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102)))' "$dir/owner.json" >/dev/null 2>&1 || return 1
    jq -c '.owner' "$dir/owner.json" | broray_ops_owner_valid || return 1
    OPS_CURRENT="$dir"; OPS_ID="$id"; OPS_EXECUTOR="$dir/owner.json"
    if [ -e "$dir/executor.json" ] || [ -L "$dir/executor.json" ]; then
        ops_file_safe "$dir/executor.json" 8192 || return 1
        jq -e --arg id "$id" '.schemaVersion==1 and .operationId==$id and (.acknowledged|type)=="boolean" and
          (.token|type)=="string" and (.token|length)==32 and (.token|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))) and
          (.handoffNonce|type)=="string" and (.handoffNonce|length)==32 and (.handoffNonce|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))) and
          (.previousTokenDigest|type)=="string" and (.previousTokenDigest|length)==64 and (.previousTokenDigest|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102)))' "$dir/executor.json" >/dev/null 2>&1 || return 1
        jq -c '.owner' "$dir/executor.json" | broray_ops_owner_valid || return 1
        OPS_EXECUTOR="$dir/executor.json"
    fi
}

ops_executor_pending()
{
    [ "$OPS_EXECUTOR" = "$OPS_CURRENT/executor.json" ] && jq -e '.acknowledged==false' "$OPS_EXECUTOR" >/dev/null
}

ops_authorize()
{
    ops_load "$1" || ops_error STATE_UNAVAILABLE 1
    [ "$(jq -r '.token' "$OPS_EXECUTOR")" = "$2" ] || ops_error OWNER_CHANGED
    jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null 2>&1 || ops_error OPERATION_FINISHED
    ! ops_executor_pending || ops_error NOT_ACKNOWLEDGED
}

ops_owner_authorize()
{
    local owner
    owner="$(broray_ops_capture_owner "$1")" || ops_error OWNER_UNCONFIRMED 1
    jq -e --argjson owner "$owner" '.owner==$owner' "$OPS_EXECUTOR" >/dev/null 2>&1 || ops_error OWNER_CHANGED
}

ops_handoff()
{
    local previous next digest token nonce record
    ops_load "$1" || ops_error STATE_UNAVAILABLE 1
    nonce="$5"; ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    previous="$(broray_ops_capture_owner "$3")" || ops_error OWNER_UNCONFIRMED
    [ "$3" != "$4" ] || ops_error HANDOFF_NOT_ALLOWED
    digest="$(printf '%s' "$2" | sha256sum | cut -d ' ' -f 1)" || ops_error STATE_UNAVAILABLE 1
    if [ "$OPS_EXECUTOR" = "$OPS_CURRENT/executor.json" ]; then
        # Lost handoff responses may be retried. Authority is never rotated a
        # second time, and the former token cannot authorize ordinary writes.
        jq -e --argjson previous "$previous" --arg pid "$4" --arg digest "$digest" --arg nonce "$nonce" \
          '.previousOwner==$previous and (.owner.pid|tostring)==$pid and .previousTokenDigest==$digest and .handoffNonce==$nonce' "$OPS_EXECUTOR" >/dev/null || ops_error OWNER_CHANGED
        printf '%s\n' '{"ok":true,"transferred":true}'
        return 0
    fi
    ops_global_matches || ops_error OWNER_CHANGED
    jq -e '.running==true and .acknowledged==true and .phase=="working" and
      (.operation=="xray:install" or .operation=="xray:update" or .operation=="xray:reinstall")' "$OPS_CURRENT/state.json" >/dev/null || ops_error HANDOFF_NOT_ALLOWED
    next="$(broray_ops_capture_owner "$4")" || ops_error OWNER_UNCONFIRMED
    ops_authorize "$1" "$2"; ops_owner_authorize "$3"
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    [ ! -e "$OPS_CURRENT/cancel.json" ] && [ ! -L "$OPS_CURRENT/cancel.json" ] || ops_error CANCELLED
    token="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || ops_error RANDOM_UNAVAILABLE 1
    ops_nonce_valid "$token" || ops_error RANDOM_UNAVAILABLE 1
    record="$(jq -nc --arg id "$OPS_ID" --arg token "$token" --arg digest "$digest" --arg nonce "$nonce" \
      --argjson owner "$next" --argjson previous "$previous" \
      '{schemaVersion:1,operationId:$id,token:$token,owner:$owner,previousTokenDigest:$digest,previousOwner:$previous,handoffNonce:$nonce,acknowledged:false}')" || ops_error STATE_UNAVAILABLE 1
    ops_write "$OPS_CURRENT/executor.json" "$record" || ops_error STATE_UNAVAILABLE 1
    OPS_EXECUTOR="$OPS_CURRENT/executor.json"
    ops_event owner_transferred >/dev/null 2>&1 || true
    printf '%s\n' '{"ok":true,"transferred":true}'
}

ops_accept_handoff()
{
    local owner digest record
    ops_load "$1" || ops_error STATE_UNAVAILABLE 1
    [ "$OPS_EXECUTOR" = "$OPS_CURRENT/executor.json" ] || ops_error HANDOFF_NOT_READY
    ops_global_matches || ops_error OWNER_CHANGED
    jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null || ops_error OPERATION_FINISHED
    owner="$(broray_ops_capture_owner "$3")" || ops_error OWNER_UNCONFIRMED
    digest="$(printf '%s' "$2" | sha256sum | cut -d ' ' -f 1)" || ops_error STATE_UNAVAILABLE 1
    jq -e --argjson owner "$owner" --arg digest "$digest" --arg nonce "$4" \
      '.owner==$owner and .previousTokenDigest==$digest and .handoffNonce==$nonce' "$OPS_EXECUTOR" >/dev/null || ops_error OWNER_CHANGED
    if ops_executor_pending; then
        [ ! -e "$OPS_CURRENT/cancel.json" ] && [ ! -L "$OPS_CURRENT/cancel.json" ] || ops_error CANCELLED
        record="$(jq -c '.acknowledged=true' "$OPS_EXECUTOR")" || ops_error STATE_UNAVAILABLE 1
        ops_write "$OPS_EXECUTOR" "$record" || ops_error STATE_UNAVAILABLE 1
    fi
    jq -c '{ok:true,operationId,token}' "$OPS_EXECUTOR"
}

ops_state_transition()
{
    local state phase error running json
    state="$1"; phase="$2"; error="${3:-}"; running=true
    case "$state" in completed|failed|aborted|recovered) running=false ;; esac
    json="$(jq -c --arg state "$state" --arg phase "$phase" --arg code "$error" --arg mode "${4:-}" --arg now "$(ops_now)" --argjson running "$running" '
      .state=$state | .phase=$phase | .running=$running | .revision+=1 | .updatedAt=$now |
      (if $state=="running" then .acknowledged=true else . end) |
      (if $mode!="" then .cancelability=$mode else . end) |
      .errorCode=(if $code=="" then null else $code end) |
      if $running then . else .finishedAt=$now end' "$OPS_CURRENT/state.json")" || return 1
    ops_write "$OPS_CURRENT/state.json" "$json" || return 1
    case "$state" in
        completed|failed|aborted|recovered) ops_event "$state" "$error" >/dev/null 2>&1 || true ;;
    esac
}

ops_global_matches()
{
    if [ -L "$OPS_GLOBAL" ]; then
        [ "$(readlink "$OPS_GLOBAL")" = "$OPS_CURRENT/fence" ] && ops_dir_safe "$OPS_CURRENT/fence" || return 1
    else
        ops_dir_safe "$OPS_GLOBAL" || return 1
    fi
    ops_file_safe "$OPS_GLOBAL/owner.json" 4096 || return 1
    cmp -s "$OPS_GLOBAL/owner.json" "$OPS_CURRENT/owner.json"
}

ops_retire_global()
{
    local file name
    ops_global_matches || return 1
    for file in "$OPS_GLOBAL"/* "$OPS_GLOBAL"/.[!.]* "$OPS_GLOBAL"/..?*; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        name="${file##*/}"
        case "$name" in pid|scope|action|bundle|startedAt|owner.json) ;; *) return 1 ;; esac
        ops_file_safe "$file" 4096 || return 1
    done
    [ ! -e "$OPS_CURRENT/retired-lock" ] && [ ! -L "$OPS_CURRENT/retired-lock" ] || return 1
    mv "$OPS_GLOBAL" "$OPS_CURRENT/retired-lock"
}

ops_children_absent()
{
    local child state children
    children="$OPS_CURRENT/children.json"
    ops_supervisors_absent || return 1
    ops_supervisors_collect || return 1
    [ -e "$children" ] || { [ ! -L "$children" ]; return $?; }
    ops_file_safe "$children" || return 1
    jq -e '.children|type=="array"' "$children" >/dev/null 2>&1 || return 1
    jq -c '.children[]' "$children" >"$OPS_CURRENT/children.scan.$$" || return 1
    state=0
    while IFS= read -r child; do
        broray_ops_classify_owner "$child"
        [ "$OPS_OWNER_STATUS" = STALE ] || state=1
    done <"$OPS_CURRENT/children.scan.$$"
    rm -f "$OPS_CURRENT/children.scan.$$"
    [ "$state" = 0 ]
}

ops_child_birth_absent()
{
    local record pid ticks boot current
    record="$1"
    printf '%s\n' "$record" | jq -e '(.pid|type)=="number" and .pid>1 and .pid<=2147483647 and .pid==(.pid|floor) and
      (.startTicks|type)=="string" and (.startTicks|length)>0 and (.startTicks|all(explode[]; .>=48 and .<=57)) and
      (.bootId|type)=="string" and (.bootId|length)>0' >/dev/null 2>&1 || return 1
    pid="$(printf '%s\n' "$record" | jq -r '.pid')"
    ticks="$(printf '%s\n' "$record" | jq -r '.startTicks')"
    boot="$(broray_ops_boot_id)" || return 1
    [ -n "$boot" ] || return 1
    [ "$(printf '%s\n' "$record" | jq -r '.bootId')" = "$boot" ] || return 0
    if [ ! -e "$OPS_PROC/$pid" ] && [ ! -L "$OPS_PROC/$pid" ]; then
        kill -0 "$pid" 2>/dev/null && return 1
        return 0
    fi
    current="$(broray_ops_start_ticks "$OPS_PROC/$pid")"
    [ -n "$current" ] && [ "$current" != "$ticks" ]
}

ops_supervisor_absent()
{
    local record id owner dir ledger boot child rows count n
    record="$1"; id="$(printf '%s\n' "$record" | jq -r '.supervisorId')"
    ops_nonce_valid "$id" || return 1
    owner="$(printf '%s\n' "$record" | jq -c '.owner')"
    broray_ops_classify_owner "$owner"
    [ "$OPS_OWNER_STATUS" = STALE ] || return 1
    [ "$OPS_OWNER_REASON" != previous_boot ] || return 0
    dir="$OPS_RAM/supervisors/$OPS_ID/$id"; ledger="$dir/children.json"
    ops_dir_safe "$OPS_RAM" && ops_dir_safe "$OPS_RAM/supervisors" &&
      ops_dir_safe "$OPS_RAM/supervisors/$OPS_ID" && ops_dir_safe "$dir" && ops_file_safe "$ledger" 65536 || return 1
    jq -e --arg id "$OPS_ID" --arg sid "$id" --argjson owner "$owner" '
      .schemaVersion==1 and .operationId==$id and .supervisorId==$sid and .supervisorPid==$owner.pid and
      .supervisorStartTicks==$owner.startTicks and .bootId==$owner.bootId and (.children|type)=="array" and (.children|length)<=256' "$ledger" >/dev/null 2>&1 || return 1
    count="$(jq '.children|length' "$ledger")"; n=0
    while [ "$n" -lt "$count" ]; do
        child="$(jq -c --argjson n "$n" '.children[$n]' "$ledger")" || return 1
        ops_child_birth_absent "$child" || return 1
        n=$((n+1))
    done
}

ops_supervisors_absent()
{
    local file n count record
    file="$OPS_CURRENT/supervisors.json"
    [ -e "$file" ] || { [ ! -L "$file" ]; return $?; }
    ops_file_safe "$file" 131072 || return 1
    jq -e '.schemaVersion==1 and (.supervisors|type)=="array" and (.supervisors|length)<=128' "$file" >/dev/null 2>&1 || return 1
    count="$(jq '.supervisors|length' "$file")"; n=0
    while [ "$n" -lt "$count" ]; do
        record="$(jq -c --argjson n "$n" '.supervisors[$n]' "$file")" || return 1
        ops_supervisor_absent "$record" || return 1
        n=$((n+1))
    done
}

ops_supervisors_collect()
{
    local file records kept removed record sid ledger count n changed
    file="$OPS_CURRENT/supervisors.json"
    [ -e "$file" ] || { [ ! -L "$file" ]; return $?; }
    ops_file_safe "$file" 131072 || return 1
    jq -e '.schemaVersion==1 and (.supervisors|type)=="array" and (.supervisors|length)<=128' "$file" >/dev/null || return 1
    records="$(cat "$file")"; kept='[]'; removed=''; n=0; changed=false
    count="$(printf '%s\n' "$records" | jq '.supervisors|length')"
    while [ "$n" -lt "$count" ]; do
        record="$(printf '%s\n' "$records" | jq -c --argjson n "$n" '.supervisors[$n]')" || return 1
        if ops_supervisor_absent "$record"; then
            sid="$(printf '%s\n' "$record" | jq -r '.supervisorId')"
            ledger="$OPS_RAM/supervisors/$OPS_ID/$sid/children.json"
            # A previous boot is already proof of absence; its RAM is gone.
            if ops_file_safe "$ledger" 65536; then
                jq -e '.termSent==true' "$ledger" >/dev/null && ops_event term '' "$sid" >/dev/null 2>&1 || true
                jq -e '.killTriggered==true' "$ledger" >/dev/null && ops_event kill '' "$sid" >/dev/null 2>&1 || true
            fi
            removed="$removed $sid"; changed=true
        else
            kept="$(jq -nc --argjson rows "$kept" --argjson item "$record" '$rows+[$item]')" || return 1
        fi
        n=$((n+1))
    done
    [ "$changed" = true ] || return 0
    records="$(jq -nc --argjson rows "$kept" '{schemaVersion:1,supervisors:$rows}')" || return 1
    ops_write "$file" "$records" || return 1
    # Registry retirement is durable before deleting any ledger. An interrupted
    # cleanup can leave a harmless RAM directory, never an unprovable registry.
    for sid in $removed; do
        ledger="$OPS_RAM/supervisors/$OPS_ID/$sid/children.json"
        if ops_dir_safe "${ledger%/*}" && ops_file_safe "$ledger" 65536; then
            rm -f "$ledger" || true
            rmdir "${ledger%/*}" 2>/dev/null || true
        fi
    done
    rmdir "$OPS_RAM/supervisors/$OPS_ID" 2>/dev/null || true
}

ops_supervisor_register()
{
    local owner records record dir directory context file nonce route_mode expected_exe
    ops_authorize "$1" "$2"
    route_mode="${5:-false}"
    if [ "$route_mode" = true ]; then
        jq -e '.scope=="routes" and .acknowledged==true and .cancelability=="protected" and .initialCancelability=="protected"' "$OPS_CURRENT/state.json" >/dev/null || ops_error CANCEL_NOT_SUPPORTED
        [ ! -e "$OPS_CURRENT/route-supervision.json" ] && [ ! -L "$OPS_CURRENT/route-supervision.json" ] || ops_error OPERATION_EXISTS
    else
        jq -e -L "$OPS_APP/lib" 'include "operation-public"; (route_protected|not) and .acknowledged==true and .cancelability=="cooperative"' "$OPS_CURRENT/state.json" >/dev/null || ops_error CANCEL_NOT_SUPPORTED
        [ ! -e "$OPS_CURRENT/cancel.json" ] && [ ! -L "$OPS_CURRENT/cancel.json" ] || ops_error CANCELLED
    fi
    nonce="$4"; ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    owner="$(broray_ops_capture_owner "$3")" || ops_error OWNER_UNCONFIRMED 1
    if [ "$route_mode" = true ]; then
        expected_exe="$(readlink -f "${BRORAY_OPS_SUPERVISOR:-$OPS_APP/bin/broray-ops-supervisor}")" || ops_error OWNER_UNCONFIRMED 1
        [ "$(printf '%s\n' "$owner" | jq -r .executable)" = "$expected_exe" ] &&
          [ "$(tr '\000' '\n' <"$OPS_PROC/$3/cmdline" | sed -n '2p')" = --protected-route ] || ops_error OWNER_UNCONFIRMED
    fi
    ops_global_matches || ops_error OWNER_CHANGED
    ops_supervisors_collect || ops_error CHILDREN_UNCONFIRMED
    records='{"schemaVersion":1,"supervisors":[]}'
    file="$OPS_CURRENT/supervisors.json"
    if [ -e "$file" ] || [ -L "$file" ]; then
        ops_file_safe "$file" 131072 && jq -e '.schemaVersion==1 and (.supervisors|type)=="array" and (.supervisors|length)<128' "$file" >/dev/null || ops_error CHILDREN_UNCONFIRMED
        records="$(cat "$file")"
    fi
    printf '%s\n' "$records" | jq -e --arg nonce "$nonce" 'all(.supervisors[]; .supervisorId!=$nonce)' >/dev/null || ops_error OPERATION_EXISTS
    dir="$OPS_RAM/supervisors/$OPS_ID/$nonce"
    for directory in "$OPS_RAM" "$OPS_RAM/supervisors" "$OPS_RAM/supervisors/$OPS_ID"; do
        [ ! -L "$directory" ] && mkdir -p "$directory" && ops_dir_safe "$directory" || ops_error UNSAFE_STATE 1
        chmod 700 "$directory" || ops_error UNSAFE_STATE 1
    done
    mkdir -m 700 "$dir" || ops_error STATE_UNAVAILABLE 1
    record="$(jq -nc --arg id "$OPS_ID" --arg sid "$nonce" --argjson owner "$owner" \
      '{schemaVersion:1,operationId:$id,supervisorId:$sid,supervisorPid:$owner.pid,supervisorStartTicks:$owner.startTicks,bootId:$owner.bootId,state:"gated",revision:0,children:[]}')" || ops_error STATE_UNAVAILABLE 1
    ops_write "$dir/children.json" "$record" || ops_error STATE_UNAVAILABLE 1
    records="$(printf '%s\n' "$records" | jq -c --arg sid "$nonce" --argjson owner "$owner" '.supervisors += [{supervisorId:$sid,owner:$owner}]')" || ops_error STATE_UNAVAILABLE 1
    ops_write "$file" "$records" || ops_error STATE_UNAVAILABLE 1
    if [ "$route_mode" = true ]; then
        context="$(jq -nc --arg id "$OPS_ID" --arg sid "$nonce" --argjson owner "$owner" \
          '{schemaVersion:1,kind:"protected-route-supervision",operationId:$id,supervisorId:$sid,owner:$owner}')" || ops_error STATE_UNAVAILABLE 1
        ops_write "$OPS_CURRENT/route-supervision.json" "$context" || ops_error STATE_UNAVAILABLE 1
    fi
    jq -r --arg ledger "$dir/children.json" '[.owner.pid,.owner.startTicks,.owner.bootId,$ledger]|@tsv' "$OPS_EXECUTOR"
}

ops_route_worker_check()
{
    local context supervisor owner pid sid ledger tracer digest
    ops_authorize "$1" "$2"; ops_global_matches || ops_error OWNER_CHANGED
    jq -e '.scope=="routes" and .acknowledged==true and .cancelability=="protected"' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    context="$OPS_CURRENT/route-supervision.json"
    ops_file_safe "$context" 4096 || ops_error CHILDREN_UNCONFIRMED
    jq -e --arg id "$OPS_ID" '.schemaVersion==1 and .kind=="protected-route-supervision" and .operationId==$id' "$context" >/dev/null || ops_error CHILDREN_UNCONFIRMED
    supervisor="$(jq -c .owner "$context")"; sid="$(jq -r .supervisorId "$context")"
    ops_nonce_valid "$sid" || ops_error CHILDREN_UNCONFIRMED
    broray_ops_classify_owner "$supervisor"
    [ "$OPS_OWNER_STATUS" = ACTIVE ] || ops_error CHILDREN_UNCONFIRMED
    ops_file_safe "$OPS_CURRENT/supervisors.json" 131072 &&
      jq -e --arg sid "$sid" --argjson owner "$supervisor" 'any(.supervisors[]; .supervisorId==$sid and .owner==$owner)' "$OPS_CURRENT/supervisors.json" >/dev/null || ops_error CHILDREN_UNCONFIRMED
    pid="$3"; owner="$(broray_ops_capture_owner "$pid")" || ops_error OWNER_UNCONFIRMED
    tracer="$(awk '$1=="TracerPid:"{print $2}' "$OPS_PROC/$pid/status")" || ops_error CHILDREN_UNCONFIRMED
    [ "$tracer" = "$(printf '%s\n' "$supervisor" | jq -r .pid)" ] || ops_error CHILDREN_UNCONFIRMED
    ledger="$OPS_RAM/supervisors/$OPS_ID/$sid/children.json"
    ops_file_safe "$ledger" 65536 && jq -e --arg id "$OPS_ID" --arg sid "$sid" --argjson owner "$owner" --argjson supervisor "$supervisor" '
      .schemaVersion==1 and .operationId==$id and .supervisorId==$sid and
      .supervisorPid==$supervisor.pid and .supervisorStartTicks==$supervisor.startTicks and .bootId==$supervisor.bootId and
      any(.children[]; .pid==$owner.pid and .startTicks==$owner.startTicks and .bootId==$owner.bootId)' "$ledger" >/dev/null || ops_error CHILDREN_UNCONFIRMED
    digest="$(printf '%s' "$2" | sha256sum | cut -d ' ' -f 1)" || ops_error STATE_UNAVAILABLE 1
    jq -nc --arg id "$OPS_ID" --arg sid "$sid" --arg digest "$digest" --argjson supervisor "$supervisor" \
      --arg bundle "$(jq -r '.bundleId // ""' "$OPS_CURRENT/state.json")" --arg action "$(jq -r '.operation' "$OPS_CURRENT/state.json")" \
      '{ok:true,operationId:$id,supervisorId:$sid,jobTokenDigest:$digest,supervisorOwner:$supervisor,bundleId:$bundle,action:$action}'
}

ops_pending_domain()
{
    local file pointer id resume_bundle resource
    resume_bundle=''
    # Only the existing continuation and its confirmation may pass a paused
    # record for the same bundle. This does not waive global/process ownership
    # or permit an old route resource generation to be removed.
    case "${1:-}" in
      resume|preflight:resume)
        case "${2:-}" in ''|*[!a-z0-9_-]*) ;; *)
            [ "${#2}" -le 63 ] && resume_bundle="$2" ;;
        esac ;;
    esac
    [ ! -e "$OPS_UPDATER/request.lock" ] && [ ! -L "$OPS_UPDATER/request.lock" ] || return 0
    [ ! -e "$OPS_LEGACY" ] && [ ! -L "$OPS_LEGACY" ] || return 0
    pointer="$OPS_STATE/last-operation"
    if [ -e "$pointer" ] || [ -L "$pointer" ]; then
        ops_file_safe "$pointer" 128 || return 0
        id="$(sed -n '1p' "$pointer")"
        ops_id_valid "$id" || return 0
        file="$OPS_ROOT/$id/state.json"
        ops_file_safe "$file" || return 0
        jq -e 'type=="object" and (.running|type)=="boolean"' "$file" >/dev/null 2>&1 || return 0
        jq -e '.running==true' "$file" >/dev/null 2>&1 && return 0
    fi
    for file in "$OPS_APP/routes/operations"/*.json; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        ops_file_safe "$file" || return 0
        jq -e 'type=="object" and (.running|type)=="boolean" and (.resumable|type)=="boolean"' "$file" >/dev/null 2>&1 || return 0
        if jq -e '.running==true or .resumable==true' "$file" >/dev/null 2>&1; then
            if [ -n "${3:-}" ] && [ "$file" = "$OPS_APP/routes/operations/${2:-}.json" ] &&
               jq -e --arg id "$3" --arg bundle "${2:-}" '.backgroundOperationId==$id and .bundleId==$bundle and .kind=="routes" and .schemaVersion==2' "$file" >/dev/null; then
                continue
            fi
            [ -n "$resume_bundle" ] && [ "$file" = "$OPS_APP/routes/operations/$resume_bundle.json" ] || return 0
            jq -e --arg bundle "$resume_bundle" '
              (.schemaVersion==1 or .schemaVersion==2) and .kind=="routes" and .bundleId==$bundle and
              .running==false and .resumable==true and
              (.operation=="install" or .operation=="update" or .operation=="restore" or .operation=="delete")' "$file" >/dev/null || return 0
            resource="$OPS_APP/routes/locks/operation.lock"
            # Recovery already proved this resource belongs to its dead job.
            # An older committed continuation for this bundle is preserved.
            if [ -z "${3:-}" ]; then
                [ ! -e "$resource" ] && [ ! -L "$resource" ] || return 0
            fi
        fi
    done
    return 1
}

ops_recover_global()
{
    local id owner status reason cancelability target
    OPS_RECOVERY_RESULT=absent
    [ -e "$OPS_GLOBAL" ] || [ -L "$OPS_GLOBAL" ] || return 0
    if [ -L "$OPS_GLOBAL" ]; then
        target="$(readlink "$OPS_GLOBAL")"
        case "$target" in "$OPS_ROOT/"*/fence) ;; *) OPS_RECOVERY_RESULT=unsafe_lock; return 2 ;; esac
        id="${target#"$OPS_ROOT/"}"; id="${id%/fence}"
        ops_load "$id" && ops_global_matches || { OPS_RECOVERY_RESULT=owner_changed; return 2; }
    else
        ops_dir_safe "$OPS_GLOBAL" || { OPS_RECOVERY_RESULT=unsafe_lock; return 2; }
    fi
    if ! ops_file_safe "$OPS_GLOBAL/owner.json" 4096; then
        # Legacy PID-only state has no birth/child evidence. It needs the
        # explicit compatibility preflight; never infer safe cleanup by age.
        OPS_RECOVERY_RESULT=legacy_owner_ambiguous; return 2
    fi
    id="$(jq -er '.operationId' "$OPS_GLOBAL/owner.json" 2>/dev/null)" || { OPS_RECOVERY_RESULT=invalid_owner; return 2; }
    ops_load "$id" && ops_global_matches || { OPS_RECOVERY_RESULT=owner_changed; return 2; }
    if jq -e '.running==false' "$OPS_CURRENT/state.json" >/dev/null 2>&1; then
        ops_publication_ready || { OPS_RECOVERY_RESULT=publication_unconfirmed; return 2; }
        ops_children_absent || { OPS_RECOVERY_RESULT=children_unconfirmed; return 2; }
        ops_route_finish_ready || { OPS_RECOVERY_RESULT=domain_pending; return 2; }
        ops_retire_global || return 1
        OPS_RECOVERY_RESULT=terminal_lock_retired
        return 0
    fi
    owner="$(jq -c '.owner' "$OPS_EXECUTOR")"
    broray_ops_classify_owner "$owner"
    status="$OPS_OWNER_STATUS"; reason="$OPS_OWNER_REASON"
    if [ "$status" != STALE ]; then OPS_RECOVERY_RESULT="$status"; return 2; fi
    ops_children_absent || { OPS_RECOVERY_RESULT=children_unconfirmed; return 2; }
    ops_publication_recover || { OPS_RECOVERY_RESULT=publication_unconfirmed; return 2; }
    # Absence of an executor is not proof that a protected domain commit can
    # be discarded. Route/updater/Xray state stays under its original owner.
    cancelability="$(jq -r -L "$OPS_APP/lib" 'include "operation-public"; if route_protected then "protected" else .cancelability end' "$OPS_CURRENT/state.json")"
    if ! ops_executor_pending && ! jq -e '.state=="starting" and .acknowledged==false' "$OPS_CURRENT/state.json" >/dev/null; then
        if [ "$cancelability" != cooperative ]; then
            ops_route_recover || { OPS_RECOVERY_RESULT=protected_recovery; return 2; }
        else
            ops_pending_domain && { OPS_RECOVERY_RESULT=domain_pending; return 2; }
        fi
    fi
    ops_state_transition aborted recovering OWNER_DISAPPEARED || return 1
    ops_retire_global || return 1
    ops_state_transition recovered finished OWNER_DISAPPEARED || return 1
    OPS_RECOVERY_RESULT=recovered
    return 0
}

ops_begin()
{
    local scope action bundle source pid cancelability owner nonce id dir final_dir record state rc fence launch file count
    scope="$1"; action="$2"; bundle="$3"; source="$4"; pid="$5"; cancelability="$6"
    launch="$7"; ops_nonce_valid "$launch" || ops_error INVALID_LAUNCH_NONCE 1
    case "$scope" in routes|system) ;; *) ops_error INVALID_SCOPE 1 ;; esac
    case "$action" in auto-switch|subscriptions:*|servers:*|xray:*|keenetic:*|dot:*|custom:*|preflight:*|check|download|build-export|verify|plan|export|delete|resume) ;; *) ops_error INVALID_ACTION 1 ;; esac
    case "$action:$bundle" in *[!A-Za-z0-9._:-]*) ops_error INVALID_ACTION 1 ;; esac
    [ "${#action}" -le 64 ] && [ "${#bundle}" -le 64 ] || ops_error INVALID_ACTION 1
    case "$source" in USER|SCHEDULER|SUBSCRIPTION_AUTO|SERVER_CHECK_AUTO|AUTO_SWITCH|UPDATER|SYSTEM_RECOVERY) ;; *) ops_error INVALID_SOURCE 1 ;; esac
    case "$cancelability" in cooperative|protected) ;; *) ops_error INVALID_CANCEL_MODE 1 ;; esac
    # Route work is protected for its entire lifetime, including preparation.
    # Enforce centrally even when an older caller asks for cooperative mode.
    cancelability="$(jq -nr -L "$OPS_APP/lib" --arg scope "$scope" --arg action "$action" --arg mode "$cancelability" \
      'include "operation-public"; {scope:$scope,operation:$action} | if route_protected then "protected" else $mode end')" || ops_error STATE_UNAVAILABLE 1
    ops_prune || ops_error STATE_UNAVAILABLE 1
    ops_prune_launch_stages
    owner="$(broray_ops_capture_owner "$pid")" || ops_error OWNER_UNCONFIRMED 1
    count=0
    for file in "$OPS_ROOT"/*/owner.json; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        count=$((count+1)); [ "$count" -le 128 ] || ops_error HISTORY_LIMIT 1
        ops_file_safe "$file" 4096 || ops_error STATE_UNAVAILABLE 1
        jq -e --arg nonce "$launch" --argjson owner "$owner" '.launchNonce==$nonce and .owner==$owner' "$file" >/dev/null 2>&1 || continue
        dir="${file%/owner.json}"; id="${dir##*/}"
        ops_load "$id" || ops_error STATE_UNAVAILABLE 1
        [ "$OPS_EXECUTOR" = "$OPS_CURRENT/owner.json" ] || ops_error OWNER_CHANGED
        jq -e --arg scope "$scope" --arg action "$action" --arg bundle "$bundle" --arg source "$source" --arg mode "$cancelability" \
          '.scope==$scope and .operation==$action and .bundleId==$bundle and .source==$source and .initialCancelability==$mode' "$OPS_CURRENT/state.json" >/dev/null || ops_error LAUNCH_MISMATCH
        jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null || ops_error OPERATION_FINISHED
        # A launch interrupted before publication has never been admitted.
        if ! ops_global_matches; then
            [ ! -e "$OPS_GLOBAL" ] && [ ! -L "$OPS_GLOBAL" ] || ops_error OPERATION_BUSY
            jq -e '.state=="starting" and .acknowledged==false' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
            ops_pending_domain "$action" "$bundle" && ops_error DOMAIN_OPERATION_BUSY
            "$OPS_GUARD" --publish-fence "$OPS_CURRENT/fence" "$OPS_GLOBAL" || ops_error OWNER_PUBLICATION_FAILED 1
        fi
        jq -c '{ok:true,operationId,token}' "$OPS_CURRENT/owner.json"
        return 0
    done
    if [ "$source" != USER ] && [ -e "$OPS_AUTOMATION" ]; then
        ops_file_safe "$OPS_AUTOMATION" 4096 || ops_error AUTOMATION_STATE_INVALID
        jq -e '.paused==false' "$OPS_AUTOMATION" >/dev/null 2>&1 || ops_error AUTOMATION_PAUSED
    fi
    ops_pending_domain "$action" "$bundle" && ops_error DOMAIN_OPERATION_BUSY
    rc=0; ops_recover_global || rc=$?
    [ "$rc" = 0 ] || ops_error OPERATION_BUSY
    nonce="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom 2>/dev/null)"
    if [ "${BRORAY_OPS_TEST:-0}" = 1 ] && [ "$OPS_APP" != /opt/broray ]; then nonce="${BRORAY_OPS_TEST_NONCE:-$nonce}"; fi
    case "$nonce" in *[!0-9a-f]*|'') ops_error RANDOM_UNAVAILABLE 1 ;; esac
    [ "${#nonce}" = 32 ] || ops_error RANDOM_UNAVAILABLE 1
    id="op-$(date -u '+%Y%m%d%H%M%S')-$pid-$(printf '%s' "$launch" | sha256sum | cut -c 1-12)"
    final_dir="$OPS_ROOT/$id"
    [ ! -e "$final_dir" ] && [ ! -L "$final_dir" ] || ops_error OPERATION_EXISTS
    dir="$OPS_ROOT/.launch-$pid-$launch"
    [ ! -e "$dir" ] && [ ! -L "$dir" ] || ops_error STATE_UNAVAILABLE 1
    mkdir "$dir" || ops_error STATE_UNAVAILABLE 1
    ops_launch_test_point directory
    record="$(jq -nc --arg id "$id" --arg token "$nonce" --arg launch "$launch" --argjson owner "$owner" '{schemaVersion:2,operationId:$id,token:$token,launchNonce:$launch,owner:$owner}')" || ops_error STATE_UNAVAILABLE 1
    state="$(jq -nc --arg id "$id" --arg action "$action" --arg source "$source" --arg scope "$scope" --arg bundle "$bundle" --arg now "$(ops_now)" --arg mono "$(ops_monotonic)" --arg mode "$cancelability" \
      '{schemaVersion:2,kind:"background",operationId:$id,operation:$action,type:$action,source:$source,scope:$scope,bundleId:$bundle,
        state:"starting",phase:"starting",running:true,revision:1,resourceLocks:["global"],cancelRequested:false,cancelability:$mode,initialCancelability:$mode,acknowledged:false,
        startedAt:$now,updatedAt:$now,startedMonotonic:$mono,finishedAt:null,errorCode:null}')" || ops_error STATE_UNAVAILABLE 1
    ops_write "$dir/owner.json" "$record" || ops_error STATE_UNAVAILABLE 1
    ops_launch_test_point owner
    ops_write "$dir/state.json" "$state" || ops_error STATE_UNAVAILABLE 1
    ops_launch_test_point state
    mkdir -p "${OPS_GLOBAL%/*}" || ops_error STATE_UNAVAILABLE 1
    fence="$dir/fence"
    mkdir "$fence" || ops_error OWNER_PUBLICATION_FAILED 1
    ops_write "$fence/owner.json" "$record" || ops_error OWNER_PUBLICATION_FAILED 1
    printf '%s\n' "$pid" >"$fence/pid" && printf '%s\n' "$scope" >"$fence/scope" &&
      printf '%s\n' "$action" >"$fence/action" && printf '%s\n' "$bundle" >"$fence/bundle" &&
      printf '%s\n' "$(ops_now)" >"$fence/startedAt" || ops_error OWNER_PUBLICATION_FAILED 1
    ops_launch_test_point fence
    # Only a complete prepared operation enters the public namespace. The
    # native publisher fsyncs its files/directories before admitting any work.
    mv "$dir" "$final_dir" || ops_error STATE_UNAVAILABLE 1
    dir="$final_dir"; fence="$dir/fence"
    OPS_CURRENT="$dir"; OPS_ID="$id"; OPS_EXECUTOR="$dir/owner.json"
    ops_launch_test_point published_directory
    rc=0; "$OPS_GUARD" --publish-fence "$fence" "$OPS_GLOBAL" || rc=$?
    [ "$rc" = 0 ] || { ops_state_transition aborted finished OWNER_PUBLICATION_FAILED; ops_error OWNER_PUBLICATION_FAILED 1; }
    if ops_pending_domain "$action" "$bundle"; then
        ops_state_transition aborted finished DOMAIN_OPERATION_BUSY && ops_retire_global || ops_error STATE_UNAVAILABLE 1
        ops_error DOMAIN_OPERATION_BUSY
    fi
    ops_event lock_acquired >/dev/null 2>&1 || true
    jq -nc --arg id "$id" --arg token "$nonce" '{ok:true,operationId:$id,token:$token}'
}

ops_prune()
{
    local file dir id count owner
    count=0
    # Keep the latest twenty terminal records. Never remove a live/ambiguous
    # owner, a fence, an unreadable state or a child whose absence is unproven.
    printf '%s\n' "$OPS_ROOT"/*/state.json | sort -r | while IFS= read -r file; do
        [ -e "$file" ] || continue
        ops_file_safe "$file" || continue
        jq -e '.kind=="background" and .running==false and (.state=="completed" or .state=="failed" or .state=="aborted" or .state=="recovered")' "$file" >/dev/null 2>&1 || continue
        count=$((count+1)); [ "$count" -gt 20 ] || continue
        dir="${file%/state.json}"; id="${dir##*/}"
        ops_load "$id" || continue
        ops_global_matches && continue
        ops_children_absent || continue
        owner="$(jq -c '.owner' "$OPS_EXECUTOR")"; broray_ops_classify_owner "$owner"
        [ "$OPS_OWNER_STATUS" = STALE ] || continue
        # ops_load checked a direct nonsymlink child and a constrained ID.
        rm -rf "$OPS_ROOT/$id" || return 1
        [ -L "$OPS_RAM/$id.json" ] || rm -f "$OPS_RAM/$id.json"
    done
}

ops_ack()
{
    ops_authorize "$1" "$2"; ops_owner_authorize "$3"
    ops_global_matches || ops_error OWNER_CHANGED
    if jq -e '.acknowledged==true' "$OPS_CURRENT/state.json" >/dev/null; then
        printf '%s\n' '{"ok":true,"acknowledged":true}'; return 0
    fi
    ops_pending_domain "$(jq -r '.operation' "$OPS_CURRENT/state.json")" "$(jq -r '.bundleId' "$OPS_CURRENT/state.json")" && ops_error DOMAIN_OPERATION_BUSY
    if [ -e "$OPS_CURRENT/cancel.json" ] || [ -L "$OPS_CURRENT/cancel.json" ]; then
        # This exact live owner has not received admission to do any work.
        # Rejecting ack alone would strand its starting fence after it exits.
        # Settle only this generation, without signalling or admitting work.
        ops_children_absent || ops_error CHILDREN_UNCONFIRMED
        ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
        ops_state_transition aborted finished CANCELLED && ops_retire_global || ops_error STATE_UNAVAILABLE 1
        ops_error CANCELLED
    fi
    ops_state_transition running working || ops_error STATE_UNAVAILABLE 1
    ops_event started >/dev/null 2>&1 || true
    printf '%s\n' '{"ok":true,"acknowledged":true}'
}

ops_recover_orphans()
{
    local file dir id owner count
    count=0
    for file in "$OPS_ROOT"/*/state.json; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        count=$((count+1)); [ "$count" -le 128 ] || return 1
        ops_file_safe "$file" || return 1
        jq -e '.kind=="background" and .running==true' "$file" >/dev/null 2>&1 || continue
        dir="${file%/state.json}"; id="${dir##*/}"
        ops_load "$id" || return 1
        ops_global_matches && continue
        # Only never-acknowledged work can be recovered without its fence.
        jq -e '.state=="starting" and .acknowledged==false' "$file" >/dev/null || return 1
        owner="$(jq -c '.owner' "$OPS_EXECUTOR")"
        broray_ops_classify_owner "$owner"
        [ "$OPS_OWNER_STATUS" = STALE ] || return 1
        ops_children_absent || return 1
        ops_state_transition recovered finished OWNER_DISAPPEARED || return 1
    done
}

ops_initialize_previous_boot()
{
    local target id owner
    # Initialization also runs during updates and service restarts. Only an
    # immutable identity from a different kernel boot permits startup cleanup.
    # Keep pause policy and every same-boot/live/ambiguous record unchanged.
    [ ! -e "$OPS_UPDATER/request.lock" ] && [ ! -L "$OPS_UPDATER/request.lock" ] || return 0
    [ ! -e "$OPS_LEGACY" ] && [ ! -L "$OPS_LEGACY" ] || return 0
    [ -L "$OPS_GLOBAL" ] || return 0
    target="$(readlink "$OPS_GLOBAL")"
    case "$target" in "$OPS_ROOT/"*/fence) ;; *) return 0 ;; esac
    id="${target#"$OPS_ROOT/"}"; id="${id%/fence}"
    ops_load "$id" && ops_global_matches || return 0
    owner="$(jq -c .owner "$OPS_EXECUTOR")"; broray_ops_classify_owner "$owner"
    [ "$OPS_OWNER_STATUS:$OPS_OWNER_REASON" = STALE:previous_boot ] || return 0
    # Protected domain commits still require their explicit consistency path.
    # This is the same safe cooperative recovery used by the next begin call.
    jq -e -L "$OPS_APP/lib" 'include "operation-public";
      (route_protected|not) and .cancelability=="cooperative"' "$OPS_CURRENT/state.json" >/dev/null || return 0
    ops_recover_global || return 0
}

ops_cancel()
{
    ops_load "$1" || ops_error STATE_UNAVAILABLE 1
    jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null 2>&1 || { printf '%s\n' '{"ok":true,"alreadyFinished":true}'; return 0; }
    jq -e -L "$OPS_APP/lib" 'include "operation-public"; (route_protected|not) and .cancelability=="cooperative"' "$OPS_CURRENT/state.json" >/dev/null 2>&1 || ops_error CANCEL_NOT_SUPPORTED
    if ops_file_safe "$OPS_CURRENT/cancel.json" 4096 && jq -e '.cancelRequested==true' "$OPS_CURRENT/cancel.json" >/dev/null; then
        printf '%s\n' '{"ok":true,"cancelRequested":true}'; return 0
    fi
    ops_write "$OPS_CURRENT/cancel.json" "$(jq -nc --arg now "$(ops_now)" '{cancelRequested:true,requestedAt:$now}')" || ops_error STATE_UNAVAILABLE 1
    ops_event cancel_requested >/dev/null 2>&1 || true
    printf '%s\n' '{"ok":true,"cancelRequested":true}'
}

ops_stop_background()
{
    local file id dir results result rc count
    ops_write "$OPS_AUTOMATION" "$(jq -nc --arg now "$(ops_now)" '{schemaVersion:1,paused:true,updatedAt:$now}')" || ops_error STATE_UNAVAILABLE 1
    results='[]'; count=0
    for file in "$OPS_ROOT"/*/state.json; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        count=$((count+1)); [ "$count" -le 128 ] || ops_error HISTORY_LIMIT 1
        ops_file_safe "$file" || ops_error STATE_UNAVAILABLE 1
        jq -e '.kind=="background" and .running==true' "$file" >/dev/null 2>&1 || continue
        dir="${file%/state.json}"; id="${dir##*/}"
        rc=0; result="$(ops_cancel "$id")" || rc=$?
        # The same projection as status prevents untrusted identifiers escaping.
        results="$(jq -nc -L "$OPS_APP/lib" --argjson rows "$results" --arg id "$id" --argjson result "$result" --argjson rc "$rc" \
          'include "operation-public"; $rows+[{operationId:($id|operation_id),cancelRequested:($result.cancelRequested==true),protected:($result.errorCode=="CANCEL_NOT_SUPPORTED"),ok:($rc==0)}]')" || ops_error STATE_UNAVAILABLE 1
    done
    jq -nc --argjson results "$results" '{ok:true,automationPaused:true,operations:$results}'
}

ops_emergency_recover()
{
    local stopped rc retryable pointer id file
    # Pause and cancellation publication precede any retirement under the same
    # short guard. The worker needs this guard to finish, so never wait here.
    rc=0; stopped="$(ops_stop_background)" || rc=$?
    if [ "$rc" != 0 ]; then printf '%s\n' "$stopped"; return "$rc"; fi
    rc=0; ops_recover_global || rc=$?
    if [ "$rc" = 0 ]; then ops_recover_orphans || { rc=2; OPS_RECOVERY_RESULT=orphan_unconfirmed; }; fi
    # The updater owns these records. Their presence cannot be hidden by an
    # absent background fence and never authorizes this manager to delete them.
    if [ "$rc" = 0 ]; then
        if [ -e "$OPS_UPDATER/request.lock" ] || [ -L "$OPS_UPDATER/request.lock" ]; then
            rc=2; OPS_RECOVERY_RESULT=updater_pending
        elif [ -e "$OPS_LEGACY" ] || [ -L "$OPS_LEGACY" ]; then
            rc=2; OPS_RECOVERY_RESULT=legacy_domain_pending
        fi
    fi
    if [ "$rc" = 0 ]; then
        pointer="$OPS_STATE/last-operation"
        if [ -e "$pointer" ] || [ -L "$pointer" ]; then
            if ! ops_file_safe "$pointer" 128; then
                rc=2; OPS_RECOVERY_RESULT=updater_pending
            else
                id="$(sed -n '1p' "$pointer")"; file="$OPS_ROOT/$id/state.json"
                if ! ops_id_valid "$id" || ! ops_file_safe "$file" ||
                   ! jq -e 'type=="object" and .running==false' "$file" >/dev/null 2>&1; then
                    rc=2; OPS_RECOVERY_RESULT=updater_pending
                fi
            fi
        fi
    fi
    retryable=false
    case "$OPS_RECOVERY_RESULT" in ACTIVE|children_unconfirmed)
        # A bounded UI recheck is useful only for a cancellable operation.
        if [ -n "${OPS_CURRENT:-}" ] && ops_file_safe "$OPS_CURRENT/state.json" &&
           jq -e -L "$OPS_APP/lib" 'include "operation-public"; (route_protected|not) and .cancelability=="cooperative"' "$OPS_CURRENT/state.json" >/dev/null 2>&1; then
            retryable=true
        fi ;;
    esac
    jq -nc --arg result "$OPS_RECOVERY_RESULT" --argjson rc "$rc" --argjson retry "$retryable" --argjson stopped "$stopped" \
      '{ok:($rc==0),result:$result,automationPaused:true,operations:$stopped.operations,retryable:$retry,
        errorCode:(if $rc==0 then null elif $rc==2 then "RECOVERY_BLOCKED" else "STATE_UNAVAILABLE" end)}'
    return "$rc"
}

ops_status()
{
    local file dir id owner status rows errors count item paused fence cancelled
    rows='[]'; errors='[]'; count=0
    for file in "$OPS_ROOT"/*/state.json; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        count=$((count+1)); [ "$count" -le 128 ] || { errors='["HISTORY_LIMIT"]'; break; }
        dir="${file%/state.json}"; id="${dir##*/}"
        # Terminal history is display data, not authority to retire a fence or
        # run work. Avoid /proc identity probes and repeated owner parsing for
        # every old row while holding the global coordinator guard. The actual
        # fence is still independently validated below, even for a terminal row.
        cancelled=false
        [ ! -f "$dir/cancel.json" ] || [ -L "$dir/cancel.json" ] || cancelled=true
        if ops_id_valid "$id" && ops_dir_safe "$dir" && ops_file_safe "$file" &&
          item="$(jq -ec -L "$OPS_APP/lib" --arg id "$id" --argjson cancelled "$cancelled" '
            include "operation-public";
            select(type=="object" and .schemaVersion==2 and .kind=="background" and .operationId==$id and
              .running==false and .phase=="finished" and (.resourceLocks|type)=="array" and
              (.revision|type)=="number" and (.state=="completed" or .state=="failed" or .state=="aborted" or .state=="recovered")) |
            .ownerStatus="FINISHED" | .ownerReason="operation_finished" | .cancelRequested=$cancelled | operation_public' "$file" 2>/dev/null)"; then
            rows="$(jq -nc --argjson rows "$rows" --argjson item "$item" '$rows+[$item]')" || return 1
            continue
        fi
        # Legacy updater history has its own public API; do not invent owners.
        if ops_file_safe "$file" && jq -e '.kind!="background"' "$file" >/dev/null 2>&1; then continue; fi
        if ! ops_load "$id"; then errors='["STATE_UNAVAILABLE"]'; continue; fi
        owner="$(jq -c '.owner' "$OPS_EXECUTOR")"
        broray_ops_classify_owner "$owner"; status="$OPS_OWNER_STATUS"
        item="$(jq -c -L "$OPS_APP/lib" --arg owner "$status" --arg reason "$OPS_OWNER_REASON" \
          'include "operation-public"; .ownerStatus=$owner | .ownerReason=$reason | operation_public' "$file")" || return 1
        if ops_executor_pending; then item="$(printf '%s\n' "$item" | jq -c '.phase="waiting"')" || return 1; fi
        if [ -f "$OPS_CURRENT/cancel.json" ] && [ ! -L "$OPS_CURRENT/cancel.json" ]; then
            item="$(printf '%s\n' "$item" | jq -c '.cancelRequested=true')"
        fi
        rows="$(jq -nc --argjson rows "$rows" --argjson item "$item" '$rows+[$item]')" || return 1
    done
    paused=false
    if [ -e "$OPS_AUTOMATION" ]; then
        ops_file_safe "$OPS_AUTOMATION" 4096 && jq -e '.paused==false' "$OPS_AUTOMATION" >/dev/null 2>&1 || paused=true
    fi
    fence=absent
    if [ -e "$OPS_GLOBAL" ] || [ -L "$OPS_GLOBAL" ]; then
        fence=ambiguous
        if [ -L "$OPS_GLOBAL" ]; then
            dir="$(readlink "$OPS_GLOBAL")"; id="${dir%/fence}"; id="${id##*/}"
            if ops_load "$id" && ops_global_matches; then
                owner="$(jq -c '.owner' "$OPS_EXECUTOR")"; broray_ops_classify_owner "$owner"
                case "$OPS_OWNER_STATUS" in ACTIVE) fence=managed_active ;; STALE) fence=managed_stale ;; esac
            fi
        fi
        [ "$fence" != ambiguous ] || errors='["OWNER_UNCONFIRMED"]'
    fi
    jq -nc --argjson rows "$rows" --argjson errors "$errors" --argjson paused "$paused" --arg fence "$fence" --arg now "$(ops_now)" \
      '{ok:($errors|length==0),complete:($errors|length==0),capturedAt:$now,operations:($rows|sort_by(.startedAt)|reverse),errors:$errors,automationPaused:$paused,globalFence:$fence}'
}

for directory in "$OPS_STATE" "$OPS_ROOT"; do
    [ ! -L "$directory" ] || ops_error UNSAFE_STATE 1
    case "${1:-}" in status|events|report|classify) ;; *) mkdir -p "$directory" || ops_error STATE_UNAVAILABLE 1 ;; esac
    ops_dir_safe "$directory" || ops_error UNSAFE_STATE 1
done
verb="${1:-}"; [ "$#" -gt 0 ] && shift
case "$verb" in
    initialize)
        # Preserve existing policy and current-boot work. A prior-boot
        # cooperative owner cannot survive and follows the normal recovery.
        [ "$#" = 0 ] || ops_error INVALID_REQUEST 1
        ops_initialize_previous_boot
        printf '%s\n' '{"ok":true}' ;;
    begin) [ "$#" = 7 ] || ops_error INVALID_REQUEST 1; ops_begin "$@" ;;
    ack) [ "$#" = 3 ] || ops_error INVALID_REQUEST 1; ops_ack "$@" ;;
    owner-check)
        [ "$#" = 3 ] || ops_error INVALID_REQUEST 1
        ops_authorize "$1" "$2"; ops_owner_authorize "$3"
        ops_global_matches || ops_error OWNER_CHANGED
        jq -e '.acknowledged==true' "$OPS_CURRENT/state.json" >/dev/null || ops_error NOT_ACKNOWLEDGED
        printf '%s\n' '{"ok":true}' ;;
    publish-json) [ "$#" = 8 ] || ops_error INVALID_REQUEST 1; ops_publish_json "$@" ;;
    supervisor-register) [ "$#" = 4 ] || ops_error INVALID_REQUEST 1; ops_supervisor_register "$@" ;;
    route-supervisor-register) [ "$#" = 4 ] || ops_error INVALID_REQUEST 1; ops_supervisor_register "$@" true ;;
    route-worker-check) [ "$#" = 3 ] || ops_error INVALID_REQUEST 1; ops_route_worker_check "$@" ;;
    handoff) [ "$#" = 5 ] || ops_error INVALID_REQUEST 1; ops_handoff "$@" ;;
    accept-handoff) [ "$#" = 4 ] || ops_error INVALID_REQUEST 1; ops_accept_handoff "$@" ;;
    helpers-drain)
        [ "$#" = 2 ] || ops_error INVALID_REQUEST 1
        ops_authorize "$1" "$2"
        ops_global_matches || ops_error OWNER_CHANGED
        ops_children_absent || ops_error CHILDREN_UNCONFIRMED
        printf '%s\n' '{"ok":true}' ;;
    finish)
        [ "$#" = 4 ] || ops_error INVALID_REQUEST 1
        ops_load "$1" || ops_error STATE_UNAVAILABLE 1
        [ "$(jq -r '.token' "$OPS_EXECUTOR")" = "$2" ] || ops_error OWNER_CHANGED
        ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
        case "$3" in completed|failed|aborted) ;; *) ops_error INVALID_STATE 1 ;; esac
        case "$4" in ''|CANCELLED|OPERATION_FAILED) ;; *) ops_error INVALID_ERROR_CODE 1 ;; esac
        ops_children_absent || ops_error CHILDREN_UNCONFIRMED
        ops_route_finish_ready || ops_error DOMAIN_OPERATION_BUSY
        if jq -e '.running==false' "$OPS_CURRENT/state.json" >/dev/null; then
            if ops_global_matches; then ops_retire_global || ops_error STATE_UNAVAILABLE 1; fi
            printf '%s\n' '{"ok":true,"alreadyFinished":true}'; exit 0
        fi
        ! ops_executor_pending || ops_error NOT_ACKNOWLEDGED
        ops_global_matches || ops_error OWNER_CHANGED
        ops_state_transition "$3" finished "$4" && ops_retire_global || ops_error STATE_UNAVAILABLE 1
        printf '%s\n' '{"ok":true}' ;;
    tick)
        [ "$#" = 3 ] || ops_error INVALID_REQUEST 1
        ops_authorize "$1" "$2"
        ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
        case "$3" in working|checking|fetching|parsing|committing|switching|waiting) ;; *) ops_error INVALID_PHASE 1 ;; esac
        ops_global_matches || ops_error OWNER_CHANGED
        jq -e '.acknowledged==true' "$OPS_CURRENT/state.json" >/dev/null || ops_error NOT_ACKNOWLEDGED
        # The commit boundary and cancellation request serialize on this guard.
        case "$3" in committing|switching)
            [ ! -e "$OPS_CURRENT/cancel.json" ] && [ ! -L "$OPS_CURRENT/cancel.json" ] || ops_error CANCELLED ;;
        esac
        case "$3" in committing|switching) ops_children_absent || ops_error CHILDREN_UNCONFIRMED ;; esac
        if [ "$(jq -r '.phase' "$OPS_CURRENT/state.json")" != "$3" ]; then
            mode="$(jq -r -L "$OPS_APP/lib" 'include "operation-public"; if route_protected then "protected" else .initialCancelability end' "$OPS_CURRENT/state.json")"
            case "$3" in committing|switching) mode=protected ;; esac
            ops_state_transition running "$3" '' "$mode" || ops_error STATE_UNAVAILABLE 1
            ops_event phase_changed >/dev/null 2>&1 || true
        fi
        [ ! -L "$OPS_RAM" ] || ops_error UNSAFE_STATE 1
        mkdir -p "$OPS_RAM" && chmod 700 "$OPS_RAM" || ops_error STATE_UNAVAILABLE 1
        ops_write "$OPS_RAM/$OPS_ID.json" "$(jq -nc --arg id "$OPS_ID" --arg boot "$(broray_ops_boot_id)" --arg now "$(ops_now)" --arg mono "$(ops_monotonic)" '{operationId:$id,bootId:$boot,heartbeatAt:$now,heartbeatMonotonic:$mono}')" || ops_error STATE_UNAVAILABLE 1
        printf '%s\n' '{"ok":true}' ;;
    cancel)
        [ "$#" = 1 ] || ops_error INVALID_REQUEST 1
        ops_cancel "$1" ;;
    stop-background) [ "$#" = 0 ] || ops_error INVALID_REQUEST 1; ops_stop_background ;;
    recover)
        [ "$#" = 0 ] || ops_error INVALID_REQUEST 1
        ops_emergency_recover; exit $? ;;
    pause|resume)
        [ "$#" = 0 ] || ops_error INVALID_REQUEST 1
        paused=true; [ "$verb" != resume ] || paused=false
        ops_write "$OPS_AUTOMATION" "$(jq -nc --argjson paused "$paused" --arg now "$(ops_now)" '{schemaVersion:1,paused:$paused,updatedAt:$now}')" || ops_error STATE_UNAVAILABLE 1
        printf '%s\n' '{"ok":true}' ;;
    status) ops_status ;;
    report) [ "$#" = 0 ] || ops_error INVALID_REQUEST 1; ops_report || ops_error REPORT_UNAVAILABLE 1 ;;
    events)
        [ "$#" = 0 ] || ops_error INVALID_REQUEST 1
        ops_journal_snapshot || ops_error JOURNAL_UNAVAILABLE 1 ;;
    classify)
        [ "$#" = 1 ] || ops_error INVALID_REQUEST 1
        ops_load "$1" || ops_error STATE_UNAVAILABLE 1
        broray_ops_classify_owner "$(jq -c '.owner' "$OPS_EXECUTOR")"
        jq -nc --arg status "$OPS_OWNER_STATUS" --arg reason "$OPS_OWNER_REASON" '{ok:true,ownerStatus:$status,reason:$reason}' ;;
    *) ops_error INVALID_REQUEST 1 ;;
esac
