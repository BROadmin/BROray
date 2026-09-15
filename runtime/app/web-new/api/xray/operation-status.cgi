#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
RUN="/opt/broray/run"
RESULT="$RUN/xray-web-operation.json"
LOG="$RUN/xray-web-operation.log"
PIDFILE="$RUN/xray-web-operation.pid"
MODEFILE="$RUN/xray-web-operation.mode"

. "$AUTH"

broray_api_require_method GET
broray_api_require_session

MODE="$(cat "$MODEFILE" 2>/dev/null || true)"
case "$MODE" in
    update|reinstall|install)
        ;;
    *)
        MODE="reinstall"
        ;;
esac

PID="$(cat "$PIDFILE" 2>/dev/null || true)"
running=false

case "$PID" in
    ''|*[!0-9]*)
        PID=""
        ;;
    *)
        if kill -0 "$PID" 2>/dev/null; then
            running=true
        fi
        ;;
esac

if [ -f "$RESULT" ] && jq -e . "$RESULT" >/dev/null 2>&1; then
    result_json="$(cat "$RESULT")"
    result_mode="$(jq -r '.operation // empty' "$RESULT" 2>/dev/null)"
    case "$result_mode" in
        update|reinstall|install)
            MODE="$result_mode"
            ;;
    esac
else
    result_json="null"
fi

log_tail="$(tail -n 40 "$LOG" 2>/dev/null || true)"

broray_api_success "$(
    jq -n \
        --arg operation "$MODE" \
        --argjson operationRunning "$running" \
        --arg pid "$PID" \
        --argjson result "$result_json" \
        --arg logTail "$log_tail" '
        {
            operation: $operation,
            operationRunning: $operationRunning,
            pid: (
                if $pid == ""
                then null
                else ($pid | tonumber)
                end
            ),
            result: $result,
            logTail: (
                if $logTail == ""
                then null
                else $logTail
                end
            )
        }
    '
)"
