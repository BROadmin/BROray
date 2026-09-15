#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
CONFIG="/opt/broray/config/system/server-auto-switch.json"
STATE="/opt/broray/run/server-auto-switch-state.json"
PIDFILE="/opt/broray/run/server-auto-switch.pid"

. "$AUTH"

broray_api_require_method GET
broray_api_require_session

if jq -e 'type == "object"' "$CONFIG" >/dev/null 2>&1; then
    CONFIG_JSON="$(
        jq '
            {
                enabled: (
                    if (.qualityRefreshEnabled | type) == "boolean"
                    then .qualityRefreshEnabled
                    else false
                    end
                ),
                intervalMinutes: (
                    if .qualityRefreshIntervalMinutes == 30 or
                       .qualityRefreshIntervalMinutes == 60 or
                       .qualityRefreshIntervalMinutes == 180 or
                       .qualityRefreshIntervalMinutes == 360
                    then .qualityRefreshIntervalMinutes
                    else 60
                    end
                ),
                updatedAt: (.updatedAt // null)
            }
        ' "$CONFIG"
    )"
else
    CONFIG_JSON='{"enabled":false,"intervalMinutes":60,"updatedAt":null}'
fi

if jq -e 'type == "object"' "$STATE" >/dev/null 2>&1; then
    STATE_JSON="$(jq '.qualityRefresh // null' "$STATE")"
else
    STATE_JSON='null'
fi

PID="$(sed -n '1p' "$PIDFILE" 2>/dev/null || true)"
PID_JSON=null
SERVICE_RUNNING=false
case "$PID" in
    ''|0|*[!0-9]*) ;;
    *)
        if [ "${#PID}" -le 10 ]; then
            PID_JSON="$PID"
            if kill -0 "$PID" 2>/dev/null; then
                SERVICE_RUNNING=true
            fi
        fi
        ;;
esac

PAYLOAD="$(
    jq -n \
        --argjson config "$CONFIG_JSON" \
        --argjson state "$STATE_JSON" \
        --argjson serviceRunning "$SERVICE_RUNNING" \
        --argjson pid "$PID_JSON" \
        --arg checkedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        {
            config: $config,
            state: $state,
            service: {
                running: $serviceRunning,
                pid: $pid
            },
            checkedAt: $checkedAt
        }
    '
)"

broray_api_success "$PAYLOAD"
