#!/opt/bin/ash

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
KEENETIC_LIBRARY="/opt/broray/lib/keenetic-page.sh"
ERROR_FILE="/opt/broray/tmp/broray-keenetic-status-$$.err"

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

if [ ! -r "$KEENETIC_LIBRARY" ]; then
    broray_api_error \
        "500 Internal Server Error" \
        "KEENETIC_BACKEND_UNAVAILABLE" \
        "Backend-модуль Keenetic недоступен."
fi

. "$KEENETIC_LIBRARY"

status_function=broray_keenetic_status_json_cached
case "&${QUERY_STRING:-}&" in
    *'&force=1&'*) status_function=broray_keenetic_status_json ;;
esac

if data_json="$($status_function 2>"$ERROR_FILE")"; then
    broray_api_success "$data_json"
    exit 0
fi

details="$(tail -n 30 "$ERROR_FILE" 2>/dev/null)"
broray_api_error \
    "500 Internal Server Error" \
    "KEENETIC_STATUS_FAILED" \
    "Не удалось получить состояние управляемого ProxyN." \
    "$details"
