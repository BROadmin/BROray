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
[ "${BRORAY_OPS_GUARD_HELD:-0}" = 1 ] || exit 73
. "$OPS_APP/lib/operation-owner.sh"
. "$OPS_APP/lib/operation-journal.sh"

ops_error()
{
    jq -nc --arg code "$1" '{ok:false,errorCode:$code}'
    exit "${2:-2}"
}
ops_id_valid() { case "${1:-}" in ''|.*|-*|*[!A-Za-z0-9._-]*) return 1 ;; esac; [ "${#1}" -le 96 ]; }
ops_dir_safe() { [ -d "$1" ] && [ ! -L "$1" ]; }
ops_file_safe() { [ -f "$1" ] && [ ! -L "$1" ] && [ "$(wc -c <"$1")" -le "${2:-32768}" ]; }
ops_now() { date -u '+%Y-%m-%dT%H:%M:%SZ'; }
ops_monotonic() { awk 'NR==1{print $1;exit}' "$OPS_PROC/uptime" 2>/dev/null; }

ops_write()
{
    local path json tmp
    path="$1"; json="$2"; tmp="$path.tmp.$$"
    [ ! -L "$path" ] && { [ ! -e "$path" ] || [ -f "$path" ]; } || return 1
    [ ! -e "$tmp" ] && [ ! -L "$tmp" ] || return 1
    (set -C; printf '%s\n' "$json" >"$tmp") || return 1
    jq -e 'type=="object"' "$tmp" >/dev/null 2>&1 || { rm -f "$tmp"; return 1; }
    chmod 600 "$tmp" 2>/dev/null || true
    mv -f "$tmp" "$path"
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
      (.token|type)=="string" and (.token|test("^[0-9a-f]{32}$"))' "$dir/owner.json" >/dev/null 2>&1 || return 1
    jq -c '.owner' "$dir/owner.json" | broray_ops_owner_valid || return 1
    OPS_CURRENT="$dir"; OPS_ID="$id"
}

ops_authorize()
{
    ops_load "$1" || ops_error STATE_UNAVAILABLE 1
    [ "$(jq -r '.token' "$OPS_CURRENT/owner.json")" = "$2" ] || ops_error OWNER_CHANGED
    jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null 2>&1 || ops_error OPERATION_FINISHED
}

ops_state_transition()
{
    local state phase error running json
    state="$1"; phase="$2"; error="${3:-}"; running=true
    case "$state" in completed|failed|aborted|recovered) running=false ;; esac
    json="$(jq -c --arg state "$state" --arg phase "$phase" --arg code "$error" --arg now "$(ops_now)" --argjson running "$running" '
      .state=$state | .phase=$phase | .running=$running | .revision+=1 | .updatedAt=$now |
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
    [ -e "$children" ] || return 0
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

ops_pending_domain()
{
    local file pointer id
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
        jq -e '.running==true or .resumable==true' "$file" >/dev/null 2>&1 && return 0
    done
    return 1
}

ops_recover_global()
{
    local id owner status reason cancelability
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
        ops_children_absent || { OPS_RECOVERY_RESULT=children_unconfirmed; return 2; }
        ops_retire_global || return 1
        OPS_RECOVERY_RESULT=terminal_lock_retired
        return 0
    fi
    owner="$(jq -c '.owner' "$OPS_CURRENT/owner.json")"
    broray_ops_classify_owner "$owner"
    status="$OPS_OWNER_STATUS"; reason="$OPS_OWNER_REASON"
    if [ "$status" != STALE ]; then OPS_RECOVERY_RESULT="$status"; return 2; fi
    ops_children_absent || { OPS_RECOVERY_RESULT=children_unconfirmed; return 2; }
    # Absence of an executor is not proof that a protected domain commit can
    # be discarded. Route/updater/Xray state stays under its original owner.
    cancelability="$(jq -r '.cancelability' "$OPS_CURRENT/state.json")"
    if [ "$cancelability" != cooperative ]; then OPS_RECOVERY_RESULT=protected_recovery; return 2; fi
    ops_pending_domain && { OPS_RECOVERY_RESULT=domain_pending; return 2; }
    ops_state_transition aborted recovering OWNER_DISAPPEARED || return 1
    ops_retire_global || return 1
    ops_state_transition recovered finished OWNER_DISAPPEARED || return 1
    OPS_RECOVERY_RESULT=recovered
    return 0
}

ops_begin()
{
    local scope action bundle source pid cancelability owner nonce id dir record state rc fence
    scope="$1"; action="$2"; bundle="$3"; source="$4"; pid="$5"; cancelability="$6"
    case "$scope" in routes|system) ;; *) ops_error INVALID_SCOPE 1 ;; esac
    case "$action" in auto-switch|subscriptions:*|servers:*|xray:*|keenetic:*|dot:*|custom:*|preflight:*|check|download|verify|plan|export|delete|resume) ;; *) ops_error INVALID_ACTION 1 ;; esac
    case "$action:$bundle" in *[!A-Za-z0-9._:-]*) ops_error INVALID_ACTION 1 ;; esac
    [ "${#action}" -le 64 ] && [ "${#bundle}" -le 64 ] || ops_error INVALID_ACTION 1
    case "$source" in USER|SCHEDULER|SUBSCRIPTION_AUTO|SERVER_CHECK_AUTO|AUTO_SWITCH|UPDATER|SYSTEM_RECOVERY) ;; *) ops_error INVALID_SOURCE 1 ;; esac
    case "$cancelability" in cooperative|protected) ;; *) ops_error INVALID_CANCEL_MODE 1 ;; esac
    if [ "$source" != USER ] && [ -e "$OPS_AUTOMATION" ]; then
        ops_file_safe "$OPS_AUTOMATION" 4096 || ops_error AUTOMATION_STATE_INVALID
        jq -e '.paused==false' "$OPS_AUTOMATION" >/dev/null 2>&1 || ops_error AUTOMATION_PAUSED
    fi
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    rc=0; ops_recover_global || rc=$?
    [ "$rc" = 0 ] || ops_error OPERATION_BUSY
    owner="$(broray_ops_capture_owner "$pid")" || ops_error OWNER_UNCONFIRMED 1
    nonce="$(od -An -N16 -tx1 /dev/urandom 2>/dev/null | tr -d ' \n')"
    if [ "${BRORAY_OPS_TEST:-0}" = 1 ] && [ "$OPS_APP" != /opt/broray ]; then nonce="${BRORAY_OPS_TEST_NONCE:-$nonce}"; fi
    case "$nonce" in *[!0-9a-f]*|'') ops_error RANDOM_UNAVAILABLE 1 ;; esac
    [ "${#nonce}" = 32 ] || ops_error RANDOM_UNAVAILABLE 1
    id="op-$(date -u '+%Y%m%d%H%M%S')-$pid-$(printf '%s' "$nonce" | sha256sum | cut -c 1-12)"
    dir="$OPS_ROOT/$id"
    [ ! -e "$dir" ] && [ ! -L "$dir" ] || ops_error OPERATION_EXISTS
    mkdir "$dir" || ops_error STATE_UNAVAILABLE 1
    record="$(jq -nc --arg id "$id" --arg token "$nonce" --argjson owner "$owner" '{schemaVersion:2,operationId:$id,token:$token,owner:$owner}')" || ops_error STATE_UNAVAILABLE 1
    state="$(jq -nc --arg id "$id" --arg action "$action" --arg source "$source" --arg scope "$scope" --arg bundle "$bundle" --arg now "$(ops_now)" --arg mono "$(ops_monotonic)" --arg mode "$cancelability" \
      '{schemaVersion:2,kind:"background",operationId:$id,operation:$action,type:$action,source:$source,scope:$scope,bundleId:$bundle,
        state:"starting",phase:"starting",running:true,revision:1,resourceLocks:["global"],cancelRequested:false,cancelability:$mode,
        startedAt:$now,updatedAt:$now,startedMonotonic:$mono,finishedAt:null,errorCode:null}')" || ops_error STATE_UNAVAILABLE 1
    ops_write "$dir/owner.json" "$record" && ops_write "$dir/state.json" "$state" || ops_error STATE_UNAVAILABLE 1
    OPS_CURRENT="$dir"; OPS_ID="$id"
    mkdir -p "${OPS_GLOBAL%/*}" || ops_error STATE_UNAVAILABLE 1
    fence="$dir/fence"
    mkdir "$fence" || ops_error OWNER_PUBLICATION_FAILED 1
    ops_write "$fence/owner.json" "$record" || ops_error OWNER_PUBLICATION_FAILED 1
    printf '%s\n' "$pid" >"$fence/pid" && printf '%s\n' "$scope" >"$fence/scope" &&
      printf '%s\n' "$action" >"$fence/action" && printf '%s\n' "$bundle" >"$fence/bundle" &&
      printf '%s\n' "$(ops_now)" >"$fence/startedAt" || ops_error OWNER_PUBLICATION_FAILED 1
    rc=0; "$OPS_GUARD" --publish-fence "$fence" "$OPS_GLOBAL" || rc=$?
    [ "$rc" = 0 ] || { ops_state_transition aborted finished OWNER_PUBLICATION_FAILED; ops_error OWNER_PUBLICATION_FAILED 1; }
    if ops_pending_domain; then
        ops_state_transition aborted finished DOMAIN_OPERATION_BUSY && ops_retire_global || ops_error STATE_UNAVAILABLE 1
        ops_error DOMAIN_OPERATION_BUSY
    fi
    ops_state_transition running working || ops_error STATE_UNAVAILABLE 1
    ops_event started >/dev/null 2>&1 || true
    ops_event lock_acquired >/dev/null 2>&1 || true
    jq -nc --arg id "$id" --arg token "$nonce" '{ok:true,operationId:$id,token:$token}'
}

ops_status()
{
    local file dir id owner status rows errors count item paused
    rows='[]'; errors='[]'; count=0
    for file in "$OPS_ROOT"/*/state.json; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        count=$((count+1)); [ "$count" -le 128 ] || { errors='["HISTORY_LIMIT"]'; break; }
        dir="${file%/state.json}"; id="${dir##*/}"
        # Legacy updater history has its own public API; do not invent owners.
        if ops_file_safe "$file" && jq -e '.kind!="background"' "$file" >/dev/null 2>&1; then continue; fi
        if ! ops_load "$id"; then errors='["STATE_UNAVAILABLE"]'; continue; fi
        owner="$(jq -c '.owner' "$OPS_CURRENT/owner.json")"
        broray_ops_classify_owner "$owner"; status="$OPS_OWNER_STATUS"
        item="$(jq -c -L "$OPS_APP/lib" --arg owner "$status" --arg reason "$OPS_OWNER_REASON" \
          'include "operation-public"; .ownerStatus=$owner | .ownerReason=$reason | operation_public' "$file")" || return 1
        if [ -f "$OPS_CURRENT/cancel.json" ] && [ ! -L "$OPS_CURRENT/cancel.json" ]; then
            item="$(printf '%s\n' "$item" | jq -c '.cancelRequested=true')"
        fi
        rows="$(jq -nc --argjson rows "$rows" --argjson item "$item" '$rows+[$item]')" || return 1
    done
    paused=false
    if [ -e "$OPS_AUTOMATION" ]; then
        ops_file_safe "$OPS_AUTOMATION" 4096 && jq -e '.paused==false' "$OPS_AUTOMATION" >/dev/null 2>&1 || paused=true
    fi
    jq -nc --argjson rows "$rows" --argjson errors "$errors" --argjson paused "$paused" --arg now "$(ops_now)" \
      '{ok:($errors|length==0),capturedAt:$now,operations:($rows|sort_by(.startedAt)|reverse),errors:$errors,automationPaused:$paused}'
}

for directory in "$OPS_STATE" "$OPS_ROOT"; do
    [ ! -L "$directory" ] || ops_error UNSAFE_STATE 1
    mkdir -p "$directory" || ops_error STATE_UNAVAILABLE 1
    ops_dir_safe "$directory" || ops_error UNSAFE_STATE 1
done
verb="${1:-}"; [ "$#" -gt 0 ] && shift
case "$verb" in
    begin) [ "$#" = 6 ] || ops_error INVALID_REQUEST 1; ops_begin "$@" ;;
    finish)
        [ "$#" = 4 ] || ops_error INVALID_REQUEST 1
        ops_authorize "$1" "$2"
        case "$3" in completed|failed|aborted) ;; *) ops_error INVALID_STATE 1 ;; esac
        case "$4" in ''|CANCELLED|OPERATION_FAILED) ;; *) ops_error INVALID_ERROR_CODE 1 ;; esac
        ops_children_absent || ops_error CHILDREN_UNCONFIRMED
        ops_global_matches || ops_error OWNER_CHANGED
        ops_state_transition "$3" finished "$4" && ops_retire_global || ops_error STATE_UNAVAILABLE 1
        printf '%s\n' '{"ok":true}' ;;
    tick)
        [ "$#" = 3 ] || ops_error INVALID_REQUEST 1
        ops_authorize "$1" "$2"
        case "$3" in working|checking|fetching|parsing|committing|switching|waiting) ;; *) ops_error INVALID_PHASE 1 ;; esac
        ops_global_matches || ops_error OWNER_CHANGED
        ops_state_transition running "$3" || ops_error STATE_UNAVAILABLE 1
        ops_write "$OPS_CURRENT/heartbeat.json" "$(jq -nc --arg now "$(ops_now)" --arg mono "$(ops_monotonic)" '{heartbeatAt:$now,heartbeatMonotonic:$mono}')" || ops_error STATE_UNAVAILABLE 1
        printf '%s\n' '{"ok":true}' ;;
    cancel)
        [ "$#" = 1 ] || ops_error INVALID_REQUEST 1
        ops_load "$1" || ops_error STATE_UNAVAILABLE 1
        jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null 2>&1 || { printf '%s\n' '{"ok":true,"alreadyFinished":true}'; exit 0; }
        jq -e '.cancelability=="cooperative"' "$OPS_CURRENT/state.json" >/dev/null 2>&1 || ops_error CANCEL_NOT_SUPPORTED
        ops_write "$OPS_CURRENT/cancel.json" "$(jq -nc --arg now "$(ops_now)" '{cancelRequested:true,requestedAt:$now}')" || ops_error STATE_UNAVAILABLE 1
        ops_event cancel_requested >/dev/null 2>&1 || true
        printf '%s\n' '{"ok":true,"cancelRequested":true}' ;;
    recover)
        [ "$#" = 0 ] || ops_error INVALID_REQUEST 1
        rc=0; ops_recover_global || rc=$?
        jq -nc --arg result "$OPS_RECOVERY_RESULT" --argjson rc "$rc" '{ok:($rc==0),result:$result}'
        exit "$rc" ;;
    pause|resume)
        [ "$#" = 0 ] || ops_error INVALID_REQUEST 1
        paused=true; [ "$verb" != resume ] || paused=false
        ops_write "$OPS_AUTOMATION" "$(jq -nc --argjson paused "$paused" --arg now "$(ops_now)" '{schemaVersion:1,paused:$paused,updatedAt:$now}')" || ops_error STATE_UNAVAILABLE 1
        printf '%s\n' '{"ok":true}' ;;
    status) ops_status ;;
    events)
        [ "$#" = 0 ] || ops_error INVALID_REQUEST 1
        events="$(ops_journal_read)" || ops_error JOURNAL_UNAVAILABLE 1
        jq -nc --argjson events "$events" '{ok:true,events:$events}' ;;
    classify)
        [ "$#" = 1 ] || ops_error INVALID_REQUEST 1
        ops_load "$1" || ops_error STATE_UNAVAILABLE 1
        broray_ops_classify_owner "$(jq -c '.owner' "$OPS_CURRENT/owner.json")"
        jq -nc --arg status "$OPS_OWNER_STATUS" --arg reason "$OPS_OWNER_REASON" '{ok:true,ownerStatus:$status,reason:$reason}' ;;
    *) ops_error INVALID_REQUEST 1 ;;
esac
