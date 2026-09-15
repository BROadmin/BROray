#!/opt/bin/ash

ROOT="${BRORAY_ROOT:-/opt/broray}"
AUTH="$ROOT/web-new/api/auth-common.sh"
CONFIG="$ROOT/config/system/server-auto-switch.json"
STATE="$ROOT/run/server-auto-switch-state.json"
PIDFILE="$ROOT/run/server-auto-switch.pid"

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

. "$ROOT/lib/auto-switch-status.sh"
STATE_JSON="$(broray_auto_switch_public_state "$STATE" | jq '.qualityRefresh // null')"
. "$ROOT/lib/service-lifecycle.sh"
broray_service_setup auto-switch
SERVICE_JSON="$(broray_service_status_json)"

PAYLOAD="$(
    jq -n \
        --argjson config "$CONFIG_JSON" \
        --argjson state "$STATE_JSON" \
        --argjson service "$SERVICE_JSON" \
        --arg checkedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        {
            config: $config,
            state: $state,
            service: $service,
            checkedAt: $checkedAt
        }
    '
)"

broray_api_success "$PAYLOAD"
