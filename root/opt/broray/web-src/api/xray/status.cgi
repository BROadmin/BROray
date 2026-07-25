#!/opt/bin/ash

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
XRAY_LIBRARY="/opt/broray/lib/xray.sh"

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

if [ ! -r "$XRAY_LIBRARY" ]; then
    broray_api_error \
        "500 Internal Server Error" \
        "XRAY_BACKEND_UNAVAILABLE" \
        "Backend-модуль Xray недоступен."
fi

. "$XRAY_LIBRARY"

broray_api_print_json_headers
printf '\r\n'

broray_xray_status_json
