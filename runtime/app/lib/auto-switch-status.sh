#!/opt/bin/ash
# Read-only projection of a cached automatic cycle onto its durable job result.
broray_auto_switch_legacy_state()
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
      (if .qualityRefresh.status=="running" and .qualityRefresh.requestId==null then
        .qualityRefresh.status="error" | .qualityRefresh.runStartedAt=null |
        .qualityRefresh.lastError=$message else . end)
    ' "$cache"
}

# A queue stage may have finished while the batch is waiting for its next
# resource. Only the request receipt describes completion of the whole batch.
broray_auto_switch_public_state()
{
    local view request reply queue_state message code
    view="$(broray_auto_switch_legacy_state "$1")" || return $?
    request="$(printf '%s\n' "$view" | jq -r '.qualityRefresh.requestId // empty')" || return 1
    if [ -z "$request" ]; then printf '%s\n' "$view"; return 0; fi
    code="${BRORAY_OPS_CODE_ROOT:-${BRORAY_ROOT:-${BRORAY_BASE:-/opt/broray}}}"
    . "$code/lib/operation-client.sh"
    queue_state=unknown
    if broray_ops_request_id_valid "$request"; then
        reply="$(broray_ops_queue_lookup "${request#q-}" 2>/dev/null)" &&
          queue_state="$(printf '%s\n' "$reply" | jq -er --arg request "$request" '
            select(.ok==true and .requestId==$request) | .state |
            select(IN("queued","running","completed","cancelled","failed"))')" || queue_state=unknown
    fi
    case "$queue_state" in
      cancelled) message="Проверка отменена. Сохранены результаты завершённых измерений." ;;
      failed) message="Проверка прервана. Сохранены результаты завершённых измерений." ;;
      completed) message="Задание завершено, но итог проверки не подтверждён." ;;
      *) message="Продолжение проверки не подтверждено. Сохранены результаты завершённых измерений." ;;
    esac
    printf '%s\n' "$view" | jq --arg state "$queue_state" --arg message "$message" '
      .qualityRefresh.queueState=$state |
      if .qualityRefresh.status=="running" and ($state!="queued" and $state!="running") then
        .qualityRefresh.status="error" | .qualityRefresh.runStartedAt=null |
        .qualityRefresh.lastError=$message
      else . end'
}
