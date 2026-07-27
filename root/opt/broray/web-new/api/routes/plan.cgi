#!/opt/bin/ash

set -u

PATH="/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
CLI="/opt/broray/bin/broray-routes"
BUNDLES="/opt/broray/routes/bundles.json"
OUTPUT_FILE="/tmp/broray-routes-plan-$$.json"
ERROR_FILE="/tmp/broray-routes-plan-$$.err"

cleanup()
{
    rm -f "$OUTPUT_FILE" "$ERROR_FILE"
}

trap cleanup EXIT HUP INT TERM

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

[ -x "$CLI" ] ||
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_CLI_UNAVAILABLE" \
        "Команда управления маршрутами недоступна."

[ -r "$BUNDLES" ] ||
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_CONFIGURATION_UNAVAILABLE" \
        "Конфигурация маршрутов недоступна."

query="${QUERY_STRING:-}"
case "$query" in
    bundleId=*) bundle_id="${query#bundleId=}" ;;
    *)
        broray_api_error \
            "400 Bad Request" \
            "ROUTES_BUNDLE_REQUIRED" \
            "Не указан идентификатор набора маршрутов."
        ;;
esac

case "$bundle_id" in
    ""|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
        broray_api_error \
            "400 Bad Request" \
            "ROUTES_BUNDLE_INVALID" \
            "Некорректный идентификатор набора маршрутов."
        ;;
esac

jq -e --arg id "$bundle_id" '
    (.schemaVersion == 1) and
    ((.bundles | type) == "array") and
    (.bundles | index($id) != null)
' "$BUNDLES" >/dev/null 2>&1 ||
    broray_api_error \
        "404 Not Found" \
        "ROUTES_BUNDLE_NOT_FOUND" \
        "Набор маршрутов не найден."

if "$CLI" plan "$bundle_id" >"$OUTPUT_FILE" 2>"$ERROR_FILE"; then
    command_ok=true
    command_rc=0
else
    command_ok=false
    command_rc=$?
fi

if [ "$command_ok" != true ]; then
    details="$(tail -n 40 "$ERROR_FILE" 2>/dev/null)"
    case "$command_rc" in
        73|75)
            broray_api_error \
                "409 Conflict" \
                "ROUTES_OPERATION_BUSY" \
                "Другая операция с маршрутами уже выполняется." \
                "$details"
            ;;
        *)
            broray_api_error \
                "409 Conflict" \
                "ROUTES_PLAN_FAILED" \
                "Не удалось подготовить безопасный план установки в Keenetic." \
                "$details"
            ;;
    esac
fi

jq -e '
    (.schemaVersion == 1) and
    ((.bundleId | type) == "string") and
    ((.mode | type) == "string") and
    ((.canApply | type) == "boolean") and
    ((.summary | type) == "object")
' "$OUTPUT_FILE" >/dev/null 2>&1 ||
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_PLAN_INVALID" \
        "План установки маршрутов повреждён."

plan_json="$(jq -c . "$OUTPUT_FILE")" ||
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_PLAN_READ_FAILED" \
        "Не удалось прочитать план установки маршрутов."

broray_api_success "$plan_json"
