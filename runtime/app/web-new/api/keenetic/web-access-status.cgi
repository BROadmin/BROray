#!/opt/bin/ash

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
WEB_PUBLISH_LIBRARY="/opt/broray/lib/web-publish.sh"
ERROR_FILE="/opt/broray/tmp/broray-web-access-status-$$.err"

cleanup()
{
    rm -f "$ERROR_FILE"
}
trap cleanup EXIT HUP INT TERM

if [ ! -r "$AUTH_COMMON" ]; then
    printf '%s\r\n' 'Status: 500 Internal Server Error'
    printf '%s\r\n' 'Content-Type: application/json; charset=utf-8'
    printf '\r\n'
    printf '%s\n' \
        '{"success":false,"data":null,"error":{"code":"AUTH_MODULE_UNAVAILABLE","message":"Модуль авторизации недоступен."}}'
    exit 0
fi

. "$AUTH_COMMON"
broray_api_require_method GET
broray_api_require_session

[ -r "$WEB_PUBLISH_LIBRARY" ] ||
    broray_api_error \
        "500 Internal Server Error" \
        "WEB_ACCESS_BACKEND_UNAVAILABLE" \
        "Модуль управления адресом WebUI недоступен."

. "$WEB_PUBLISH_LIBRARY"

if data_json="$(broray_web_publish_status_json 2>"$ERROR_FILE")" &&
   printf '%s\n' "$data_json" | jq -e 'type == "object"' >/dev/null 2>&1
then
    broray_api_success "$data_json"
    exit 0
fi

broray_api_error \
    "500 Internal Server Error" \
    "WEB_ACCESS_STATUS_FAILED" \
    "Не удалось получить состояние адреса WebUI через KeenDNS." \
    "$(tail -n 30 "$ERROR_FILE" 2>/dev/null)"
