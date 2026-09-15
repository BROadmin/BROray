#!/opt/bin/ash
# Client of the single coordinator; shared by old Operation Manager consumers.

broray_ops_call()
{
    local app state guard ash controller rc
    app="${BRORAY_ROOT:-${BRORAY_BASE:-/opt/broray}}"
    state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
    guard="${BRORAY_OPS_GUARD:-$app/bin/broray-ops-guard}"
    ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    controller="$app/lib/operation-coordinator.sh"
    [ -f "$guard" ] && [ ! -L "$guard" ] && [ -f "$controller" ] || return 1
    [ ! -L "$state" ] || return 1
    case "${1:-}" in status|events|report|classify)
        [ -d "$state" ] && [ -f "$state/operations.guard" ] && [ ! -L "$state/operations.guard" ] || return 1 ;;
    *)
        mkdir -p "$state" || return 1
        chmod 700 "$state" 2>/dev/null || true ;;
    esac
    if [ "${BRORAY_OPS_TEST:-0}" = 1 ] && [ "$app" != /opt/broray ]; then
        "$guard" "$state/operations.guard" "$ash" ash "$controller" "$@"
    else
        "$guard" "$state/operations.guard" "$ash" "$controller" "$@"
    fi
}

broray_ops_begin()
{
    local response rc attempt id token
    # Keep this private nonce until finish. Retrying a lost response must recover
    # the same launch, including across a date boundary, without another claim.
    if [ -z "${BRORAY_BACKGROUND_LAUNCH_NONCE:-}" ]; then
        BRORAY_BACKGROUND_LAUNCH_NONCE="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || return 1
    fi
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call begin "$1" "$2" "${3:-}" "${4:-USER}" "$$" "${5:-protected}" "$BRORAY_BACKGROUND_LAUNCH_NONCE")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | broray_ops_response_valid token; then break; fi
        # A structured rejection is final. Retry only unavailable responses.
        [ "$rc" != 0 ] || rc=1
        if printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1; then rc=2; break; fi
    done
    [ "$rc" = 0 ] || { BRORAY_OPS_LAST_ERROR="$response"; return "$rc"; }
    id="$(printf '%s\n' "$response" | jq -er '.operationId')" || return 1
    token="$(printf '%s\n' "$response" | jq -er '.token')" || return 1
    BRORAY_BACKGROUND_OPERATION_ID="$id"
    BRORAY_BACKGROUND_OPERATION_TOKEN="$token"
    export BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1))
        broray_ops_call ack "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" >/dev/null && return 0
    done
    # The caller must exit on failure; it has no permission to execute its job.
    return 1
}

broray_ops_response_valid()
{
    # A zero transport exit code with an empty/truncated body is still a lost
    # response. Never install half an identity or open the worker gate for it.
    jq -es --arg mode "$1" 'length==1 and (.[0] | type=="object" and .ok==true and
      (if $mode=="transfer" then .transferred==true else
        (.operationId|type)=="string" and (.operationId|startswith("op-")) and
        (.operationId|length)<=96 and (.operationId|all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==45 or .==46 or .==95)) and
        (.token|type)=="string" and (.token|length)==32 and
        (.token|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))) end))' >/dev/null 2>&1
}

broray_ops_finish()
{
    [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 0
    broray_ops_call finish "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "${1:-completed}" "${2:-}" >/dev/null || return $?
    unset BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN BRORAY_BACKGROUND_LAUNCH_NONCE
}

broray_ops_tick()
{
    [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 0
    broray_ops_call tick "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "${1:-working}" >/dev/null
}

broray_ops_cancel_requested()
{
    local state id
    state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
    id="${BRORAY_BACKGROUND_OPERATION_ID:-}"
    case "$id" in ''|*[!A-Za-z0-9._-]*|.*) return 1 ;; esac
    [ -f "$state/operations/$id/cancel.json" ] && [ ! -L "$state/operations/$id/cancel.json" ]
}

broray_ops_run_helper()
{
    # Only bounded cooperative work belongs here. Starting the persistent Xray
    # service is a protected owner action, never a traced helper command.
    local app ash supervisor state timeout rc attempt
    [ "$#" -ge 3 ] && [ "$2" = -- ] || return 64
    timeout="$1"; shift 2
    [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] && [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] || return 73
    [ "${BRORAY_OPS_SUPERVISED:-}" != ptrace/1 ] || return 73
    app="${BRORAY_ROOT:-/opt/broray}"
    ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
    supervisor="${BRORAY_OPS_SUPERVISOR:-$app/bin/broray-ops-supervisor}"
    [ -f "$supervisor" ] && [ ! -L "$supervisor" ] || return 74
    rc=0
    "$supervisor" "$ash" "$app/lib/operation-supervisor-control.sh" \
      "$state/operations/$BRORAY_BACKGROUND_OPERATION_ID/cancel.json" "$timeout" 1 2 -- "$@" || rc=$?
    # EXITKILL is asynchronous. The next commit/finish is permitted only after
    # the coordinator confirms every registered helper has disappeared.
    attempt=0
    while [ "$attempt" -lt 5 ]; do
        attempt=$((attempt+1))
        broray_ops_call helpers-drain "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" >/dev/null && return "$rc"
        [ "$attempt" = 5 ] || sleep 1
    done
    return 75
}

broray_ops_handoff_to()
{
    local attempt response rc
    [ "$#" = 2 ] && [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 64
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call handoff "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" "$1" "$2")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | broray_ops_response_valid transfer; then
            unset BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN BRORAY_BACKGROUND_LAUNCH_NONCE
            return 0
        fi
        [ "$rc" != 0 ] || rc=1
        printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1 && return 2
    done
    return "$rc"
}

broray_ops_accept_handoff()
{
    local attempt response rc token
    [ "$#" = 1 ] && [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 64
    attempt=0
    while [ "$attempt" -lt 10 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call accept-handoff "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" "$1")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | broray_ops_response_valid token; then
            token="$(printf '%s\n' "$response" | jq -er '.token')" || return 1
            BRORAY_BACKGROUND_OPERATION_TOKEN="$token"; export BRORAY_BACKGROUND_OPERATION_TOKEN
            return 0
        fi
        # Only an unpublished handoff or missing transport response is retried.
        printf '%s\n' "$response" | jq -e '.ok==false and .errorCode!="HANDOFF_NOT_READY"' >/dev/null 2>&1 && return 2
        [ "$attempt" = 10 ] || sleep 1
    done
    return 1
}
