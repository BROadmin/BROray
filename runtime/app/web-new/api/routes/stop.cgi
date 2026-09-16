#!/opt/bin/ash

set -u

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"

if [ ! -r "$AUTH_COMMON" ]; then
    printf '%s\r\n' 'Status: 500 Internal Server Error'
    printf '%s\r\n' 'Content-Type: application/json; charset=utf-8'
    printf '%s\r\n' 'Cache-Control: no-store'
    printf '\r\n'
    printf '%s\n' '{"success":false,"data":null,"error":{"code":"AUTH_MODULE_UNAVAILABLE","message":"Модуль авторизации недоступен."}}'
    exit 0
fi
. "$AUTH_COMMON"
broray_api_require_method POST
broray_api_require_session

# Authenticated old clients receive the same policy as the current WebUI.
broray_api_error "409 Conflict" "ROUTES_STOP_NOT_SUPPORTED" \
    "Остановка операций с маршрутами недоступна. Дождитесь завершения операции."
