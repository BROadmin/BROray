#!/opt/bin/ash
# Read-only projection of a cached automatic cycle onto its durable job result.
broray_auto_switch_public_state()
{
    local cache operation record terminal message
    cache="$1"
    if [ ! -f "$cache" ] || [ -L "$cache" ] || ! jq -e 'type=="object"' "$cache" >/dev/null 2>&1; then
        printf 'null\n'; return 0
    fi
    operation="$(jq -r '.backgroundOperationId // empty' "$cache")"
    terminal=""
    case "$operation" in
      op-*)
        case "$operation" in *[!A-Za-z0-9._-]*) operation="" ;; esac
        [ "${#operation}" -le 96 ] || operation=""
        ;;
      *) operation="" ;;
    esac
    if [ -n "$operation" ]; then
        record="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/operations/$operation/state.json"
        if [ -f "$record" ] && [ ! -L "$record" ]; then
            terminal="$(jq -er --arg id "$operation" '
              select(.kind=="background" and .operationId==$id and .operation=="auto-switch" and .running==false) |
              select(.state=="aborted" or .state=="failed" or .state=="recovered" or .state=="completed") | .state' "$record" 2>/dev/null)" || terminal=""
        fi
    fi
    if [ -z "$terminal" ]; then cat "$cache"; return $?; fi
    message="Автоматическое задание прервано. Сохранены результаты завершённых проверок."
    [ "$terminal" != aborted ] || message="Автоматическое задание отменено. Сохранены результаты завершённых проверок."
    jq --arg terminal "$terminal" --arg message "$message" '
      .backgroundOperationState=$terminal |
      (if .status=="checking-candidates" or .status=="switching" then
        .status=(if $terminal=="aborted" then "cancelled" else "error" end) |
        .lastReason=$message | .lastError=null else . end) |
      (if .qualityRefresh.status=="running" then
        .qualityRefresh.status="error" | .qualityRefresh.runStartedAt=null |
        .qualityRefresh.lastError=$message else . end)
    ' "$cache"
}
