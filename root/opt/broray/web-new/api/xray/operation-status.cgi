#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
RESULT="/opt/broray/run/xray-web-reinstall.json"
LOG="/opt/broray/run/xray-web-reinstall.log"
PIDFILE="/opt/broray/run/xray-web-reinstall.pid"

. "$AUTH"

broray_api_require_method GET
broray_api_require_session

PID="$(
    cat "$PIDFILE" 2>/dev/null || true
)"

running=false

if [ -n "$PID" ] &&
   kill -0 "$PID" 2>/dev/null
then
    running=true
fi

if [ -f "$RESULT" ] &&
   jq -e . "$RESULT" >/dev/null 2>&1
then
    result_json="$(
        cat "$RESULT"
    )"
else
    result_json="null"
fi

log_tail="$(
    tail -n 40 "$LOG" 2>/dev/null || true
)"

broray_api_success "$(
    jq -n \
        --argjson operationRunning "$running" \
        --arg pid "$PID" \
        --argjson result "$result_json" \
        --arg logTail "$log_tail" '
        {
            operation: "reinstall",
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
