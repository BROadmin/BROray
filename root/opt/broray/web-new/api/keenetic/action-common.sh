#!/opt/bin/ash

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
KEENETIC_LIBRARY="/opt/broray/lib/keenetic-page.sh"
ACTION="$1"
OUTPUT_FILE="/tmp/broray-keenetic-action-$$.json"
ERROR_FILE="/tmp/broray-keenetic-action-$$.err"

cleanup()
{
    rm -f "$OUTPUT_FILE" "$ERROR_FILE"
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
broray_api_require_method POST
broray_api_require_session

if [ ! -r "$KEENETIC_LIBRARY" ]; then
    broray_api_error \
        "500 Internal Server Error" \
        "KEENETIC_BACKEND_UNAVAILABLE" \
        "Backend-модуль Keenetic недоступен."
fi

. "$KEENETIC_LIBRARY"

broray_keenetic_run_action "$ACTION" \
    >"$OUTPUT_FILE" 2>"$ERROR_FILE"
rc=$?

if [ "$rc" -eq 0 ]; then
    if jq -e . "$OUTPUT_FILE" >/dev/null 2>&1; then
        broray_api_success "$(cat "$OUTPUT_FILE")"
        exit 0
    fi

    broray_api_error \
        "500 Internal Server Error" \
        "KEENETIC_INVALID_RESPONSE" \
        "Backend Keenetic вернул некорректный ответ."
fi

details="$(
    {
        cat "$ERROR_FILE" 2>/dev/null
        cat "$OUTPUT_FILE" 2>/dev/null
    } |
        tail -n 30
)"

case "$rc" in
    2)
        broray_api_error \
            "409 Conflict" \
            "KEENETIC_EXPECTED_STATE_UNAVAILABLE" \
            "Операция недоступна без параметров SOCKS Xray и активного сервера." \
            "$details"
        ;;
    *)
        broray_api_error \
            "500 Internal Server Error" \
            "KEENETIC_ACTION_FAILED" \
            "Не удалось выполнить операцию с управляемым ProxyN." \
            "$details"
        ;;
esac
