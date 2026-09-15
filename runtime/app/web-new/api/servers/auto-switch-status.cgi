#!/opt/bin/ash

ROOT="${BRORAY_ROOT:-/opt/broray}"
AUTH="$ROOT/web-new/api/auth-common.sh"
CONFIG="$ROOT/config/system/server-auto-switch.json"
STATE="$ROOT/run/server-auto-switch-state.json"
PIDFILE="$ROOT/run/server-auto-switch.pid"
MONITOR="$ROOT/run/connection-status.json"
MONITOR_PIDFILE="$ROOT/run/connection-monitor.pid"
LOG="$ROOT/logs/server-auto-switch.log"

. "$AUTH"

broray_api_require_method GET
broray_api_require_session

if jq -e 'type == "object"' "$CONFIG" >/dev/null 2>&1; then
    CONFIG_JSON="$(cat "$CONFIG")"
else
    CONFIG_JSON='{"enabled":false}'
fi

. "$ROOT/lib/auto-switch-status.sh"
STATE_JSON="$(broray_auto_switch_public_state "$STATE")"

if jq -e 'type == "object"' "$MONITOR" >/dev/null 2>&1; then
    MONITOR_JSON="$(cat "$MONITOR")"
else
    MONITOR_JSON='null'
fi

. "$ROOT/lib/service-lifecycle.sh"
broray_service_setup auto-switch
SERVICE_JSON="$(broray_service_status_json)"
MONITOR_PID="$(cat "$MONITOR_PIDFILE" 2>/dev/null || true)"
MONITOR_RUNNING=false

if [ -n "$MONITOR_PID" ] && kill -0 "$MONITOR_PID" 2>/dev/null; then
    MONITOR_RUNNING=true
fi

LOG_TAIL="$(tail -n 20 "$LOG" 2>/dev/null || true)"

PAYLOAD="$(
    jq -n \
        --argjson config "$CONFIG_JSON" \
        --argjson state "$STATE_JSON" \
        --argjson monitor "$MONITOR_JSON" \
        --argjson service "$SERVICE_JSON" \
        --argjson monitorRunning "$MONITOR_RUNNING" \
        --arg monitorPid "$MONITOR_PID" \
        --arg logTail "$LOG_TAIL" \
        --arg checkedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        {
            config: $config,
            state: $state,
            monitor: $monitor,

            service: $service,

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
