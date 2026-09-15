#!/opt/bin/ash

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
WEB_PUBLISH_LIBRARY="/opt/broray/lib/web-publish.sh"
ACTION="${1:-}"
ERROR_FILE="/opt/broray/tmp/broray-web-access-action-$$.err"
OUTPUT_FILE="/opt/broray/tmp/broray-web-access-action-$$.out"

cleanup()
{
    rm -f "$ERROR_FILE" "$OUTPUT_FILE"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

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

case "$ACTION" in
    enable|disable) ;;
    *)
        broray_api_error \
            "400 Bad Request" \
            "WEB_ACCESS_ACTION_INVALID" \
            "Неизвестная операция с адресом WebUI."
        ;;
esac

[ -r /opt/broray/lib/routes-api-operation.sh ] ||
    broray_api_error \
        "500 Internal Server Error" \
        "GLOBAL_LOCK_UNAVAILABLE" \
        "Общий координатор операций недоступен."
. /opt/broray/lib/routes-api-operation.sh

lock_rc=0
broray_routes_api_lock_acquire "keenetic:web-access-$ACTION" keenetic || lock_rc=$?
case "$lock_rc" in
    0)
        trap 'broray_routes_api_lock_release; cleanup' EXIT
        trap 'exit 129' HUP
        trap 'exit 130' INT
        trap 'exit 143' TERM
        ;;
    2)
        broray_api_error \
            "409 Conflict" \
            "OPERATION_BUSY" \
            "Другая конфликтующая операция BROray уже выполняется."
        ;;
    *)
        broray_api_error \
            "500 Internal Server Error" \
            "GLOBAL_LOCK_FAILED" \
            "Не удалось установить общую блокировку BROray."
        ;;
esac

[ -r "$WEB_PUBLISH_LIBRARY" ] ||
    broray_api_error \
        "500 Internal Server Error" \
        "WEB_ACCESS_BACKEND_UNAVAILABLE" \
        "Модуль управления адресом WebUI недоступен."
. "$WEB_PUBLISH_LIBRARY"

if [ "$ACTION" = enable ]; then
    broray_web_publish_ensure >"$OUTPUT_FILE" 2>"$ERROR_FILE"
else
    broray_web_publish_delete >"$OUTPUT_FILE" 2>"$ERROR_FILE"
fi
rc=$?

if [ "$rc" -eq 0 ]; then
    if data_json="$(broray_web_publish_status_json 2>>"$ERROR_FILE")" &&
       printf '%s\n' "$data_json" | jq -e 'type == "object"' >/dev/null 2>&1
    then
        broray_api_success "$data_json"
        exit 0
    fi
    broray_api_error \
        "500 Internal Server Error" \
        "WEB_ACCESS_STATUS_FAILED" \
        "Операция выполнена, но итоговое состояние адреса WebUI не подтверждено." \
        "$(tail -n 30 "$ERROR_FILE" 2>/dev/null)"
fi

error_line="$(grep '^BRORAY_WEB_PUBLISH_ERROR:' "$ERROR_FILE" 2>/dev/null | tail -n 1)"
error_code=WEB_ACCESS_ACTION_FAILED
error_message='Не удалось изменить адрес WebUI через KeenDNS.'
http_status='500 Internal Server Error'

if [ -n "$error_line" ]; then
    error_fields="${error_line#BRORAY_WEB_PUBLISH_ERROR:}"
    error_stage="${error_fields%%:*}"
    error_fields="${error_fields#*:}"
    error_code="${error_fields%%:*}"
    error_message="${error_fields#*:}"
    case "$error_code" in
        *OWNERSHIP*|*RECEIPT*|*RECOVERY_REQUIRED|*DEVELOPMENT_WRITE_DISABLED|*WRITE_POLICY_INVALID|*WRITE_PATH_INVALID|*PHYSICAL_SERIALIZATION_REQUIRED)
            http_status='409 Conflict'
            ;;
    esac
fi

broray_api_error \
    "$http_status" \
    "$error_code" \
    "$error_message" \
    "$(tail -n 30 "$ERROR_FILE" 2>/dev/null)"
