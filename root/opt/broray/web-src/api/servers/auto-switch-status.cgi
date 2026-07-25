#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
CONFIG="/opt/broray/config/system/server-auto-switch.json"
STATE="/opt/broray/run/server-auto-switch-state.json"
PIDFILE="/opt/broray/run/server-auto-switch.pid"
MONITOR="/opt/broray/run/connection-status.json"
MONITOR_PIDFILE="/opt/broray/run/connection-monitor.pid"
LOG="/opt/broray/logs/server-auto-switch.log"

. "$AUTH"

broray_api_require_method GET
broray_api_require_session

if jq -e 'type == "object"' "$CONFIG" >/dev/null 2>&1; then
    CONFIG_JSON="$(cat "$CONFIG")"
else
    CONFIG_JSON='{"enabled":false}'
fi

if jq -e 'type == "object"' "$STATE" >/dev/null 2>&1; then
    STATE_JSON="$(cat "$STATE")"
else
    STATE_JSON='null'
fi

if jq -e 'type == "object"' "$MONITOR" >/dev/null 2>&1; then
    MONITOR_JSON="$(cat "$MONITOR")"
else
    MONITOR_JSON='null'
fi

PID="$(cat "$PIDFILE" 2>/dev/null || true)"
MONITOR_PID="$(cat "$MONITOR_PIDFILE" 2>/dev/null || true)"
SERVICE_RUNNING=false
MONITOR_RUNNING=false

if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    SERVICE_RUNNING=true
fi

if [ -n "$MONITOR_PID" ] && kill -0 "$MONITOR_PID" 2>/dev/null; then
    MONITOR_RUNNING=true
fi

LOG_TAIL="$(tail -n 20 "$LOG" 2>/dev/null || true)"

PAYLOAD="$(
    jq -n \
        --argjson config "$CONFIG_JSON" \
        --argjson state "$STATE_JSON" \
        --argjson monitor "$MONITOR_JSON" \
        --argjson serviceRunning "$SERVICE_RUNNING" \
        --argjson monitorRunning "$MONITOR_RUNNING" \
        --arg pid "$PID" \
        --arg monitorPid "$MONITOR_PID" \
        --arg logTail "$LOG_TAIL" \
        --arg checkedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        {
            config: $config,
            state: $state,
            monitor: $monitor,

            service: {
                running: $serviceRunning,
                pid: (if $pid == "" then null else ($pid | tonumber) end)
            },

            connectionMonitor: {
                running: $monitorRunning,
                pid: (if $monitorPid == "" then null else ($monitorPid | tonumber) end)
            },

            logTail: (if $logTail == "" then null else $logTail end),
            checkedAt: $checkedAt
        }
    '
)"

broray_api_success "$PAYLOAD"
