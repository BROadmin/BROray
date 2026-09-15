#!/opt/bin/ash
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
. "$BRORAY_ROOT/web-new/api/auth-common.sh"
broray_api_require_method GET
broray_api_require_session
. "$BRORAY_ROOT/lib/xray-web-status.sh"
PAYLOAD="$(broray_xray_web_status)" || broray_api_error '503 Service Unavailable' XRAY_STATUS_UNAVAILABLE 'Не удалось прочитать состояние операции Xray.'
if ! printf '%s\n' "$PAYLOAD" | jq -e '.complete==true' >/dev/null; then
    broray_api_error '503 Service Unavailable' XRAY_OWNER_UNCONFIRMED 'Владелец операции Xray не подтверждён. Откройте диагностику операций.'
fi
broray_api_success "$PAYLOAD"
