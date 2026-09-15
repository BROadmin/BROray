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
    mkdir -p "$state" || return 1
    chmod 700 "$state" 2>/dev/null || true
    if [ "${BRORAY_OPS_TEST:-0}" = 1 ] && [ "$app" != /opt/broray ]; then
        "$guard" "$state/operations.guard" "$ash" ash "$controller" "$@"
    else
        "$guard" "$state/operations.guard" "$ash" "$controller" "$@"
    fi
}

broray_ops_begin()
{
    local response rc
    rc=0
    response="$(broray_ops_call begin "$1" "$2" "${3:-}" "${4:-USER}" "$$" "${5:-protected}")" || rc=$?
    [ "$rc" = 0 ] || { BRORAY_OPS_LAST_ERROR="$response"; return "$rc"; }
    BRORAY_BACKGROUND_OPERATION_ID="$(printf '%s\n' "$response" | jq -er '.operationId')" || return 1
    BRORAY_BACKGROUND_OPERATION_TOKEN="$(printf '%s\n' "$response" | jq -er '.token')" || return 1
    export BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN
}

broray_ops_finish()
{
    [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 0
    broray_ops_call finish "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "${1:-completed}" "${2:-}" >/dev/null
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
