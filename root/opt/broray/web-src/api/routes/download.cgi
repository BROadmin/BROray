#!/opt/bin/ash

set -u

PATH="/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
API_LOCK_LIBRARY="/opt/broray/lib/routes-api-operation.sh"
CLI="/opt/broray/bin/broray-routes"
BUNDLES="/opt/broray/routes/bundles.json"
CONFIG="/opt/broray/routes/config.json"
MANIFEST_DIR="/opt/broray/routes/manifests"
STATE_DIR="/opt/broray/routes/state"

OUTPUT_FILE="/tmp/broray-routes-download-$$.out"
ERROR_FILE="/tmp/broray-routes-download-$$.err"

cleanup()
{
    rm -f "$OUTPUT_FILE" "$ERROR_FILE"
    command -v broray_routes_api_lock_release >/dev/null 2>&1 &&
        broray_routes_api_lock_release
}

trap cleanup EXIT HUP INT TERM

if [ ! -r "$AUTH_COMMON" ]; then
    printf '%s\r\n' 'Status: 500 Internal Server Error'
    printf '%s\r\n' 'Content-Type: application/json; charset=utf-8'
    printf '%s\r\n' 'Cache-Control: no-store'
    printf '\r\n'
    printf '%s\n' \
        '{"success":false,"data":null,"error":{"code":"AUTH_MODULE_UNAVAILABLE","message":"Модуль авторизации недоступен."}}'
    exit 0
fi

. "$AUTH_COMMON"

broray_api_require_method POST
broray_api_require_session

if [ ! -x "$CLI" ]; then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_CLI_UNAVAILABLE" \
        "Команда управления маршрутами недоступна."
fi

if [ ! -r "$BUNDLES" ] ||
   [ ! -r "$CONFIG" ]
then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_CONFIGURATION_UNAVAILABLE" \
        "Конфигурация маршрутов недоступна."
fi

query="${QUERY_STRING:-}"

case "$query" in
    bundleId=*)
        bundle_id="${query#bundleId=}"
        ;;
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

if ! jq -e \
    --arg bundle_id "$bundle_id" \
    '.bundles[] | select(. == $bundle_id)' \
    "$BUNDLES" >/dev/null 2>&1
then
    broray_api_error \
        "404 Not Found" \
        "ROUTES_BUNDLE_NOT_FOUND" \
        "Набор маршрутов не найден."
fi

[ -r "$API_LOCK_LIBRARY" ] || broray_api_error \
    "500 Internal Server Error" "ROUTES_API_LOCK_UNAVAILABLE" \
    "Модуль блокировки операций недоступен."
. "$API_LOCK_LIBRARY"
lock_rc=0
broray_routes_api_lock_acquire "download" "$bundle_id" || lock_rc=$?
case "$lock_rc" in
    0) ;;
    2) broray_api_error "409 Conflict" "ROUTES_OPERATION_BUSY" \
        "Другая конфликтующая операция уже выполняется." ;;
    *) broray_api_error "500 Internal Server Error" "ROUTES_API_LOCK_FAILED" \
        "Не удалось установить блокировку операции." ;;
esac

managed_interface="$(
    jq -r '.managedInterface // empty' \
        "$CONFIG" 2>/dev/null
)"

case "$managed_interface" in
    Proxy[0-9]*) ;;
    *) managed_interface="" ;;
esac
case "${managed_interface#Proxy}" in
    ''|*[!0-9]*) managed_interface="" ;;
esac

if [ -z "$managed_interface" ]; then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_INTERFACE_POLICY_INVALID" \
        "Разрешённый интерфейс маршрутов настроен неверно."
fi

manifest="$MANIFEST_DIR/$bundle_id.json"
state_file="$STATE_DIR/$bundle_id.json"

if [ ! -r "$manifest" ] ||
   [ ! -r "$state_file" ]
then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_BUNDLE_FILES_UNAVAILABLE" \
        "Файлы выбранного набора маршрутов недоступны."
fi

if ! jq -e \
    --arg bundle_id "$bundle_id" \
    --arg target_interface "$managed_interface" '
        (.id == $bundle_id) and
        (.targetInterface == $target_interface) and
        (.exportComment == "BROray")
    ' "$manifest" >/dev/null 2>&1
then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_BUNDLE_POLICY_INVALID" \
        "Набор маршрутов не соответствует политике BROray."
fi

if "$CLI" download "$bundle_id" \
    >"$OUTPUT_FILE" \
    2>"$ERROR_FILE"
then
    command_ok=true
else
    command_ok=false
fi

if ! jq -e \
    --arg bundle_id "$bundle_id" '
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        ((.status | type) == "string")
    ' "$state_file" >/dev/null 2>&1
then
    details="$(
        {
            tail -n 20 "$ERROR_FILE" 2>/dev/null
            tail -n 20 "$OUTPUT_FILE" 2>/dev/null
        } |
            tail -n 30
    )"

    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_INVALID" \
        "Состояние набора маршрутов повреждено." \
        "$details"
fi

if [ "$command_ok" != true ]; then
    details="$(
        {
            tail -n 20 "$ERROR_FILE" 2>/dev/null
            tail -n 20 "$OUTPUT_FILE" 2>/dev/null
        } |
            tail -n 30
    )"

    if printf '%s\n' "$details" |
        grep -Fq "Другая операция с маршрутами уже выполняется."
    then
        broray_api_error \
            "409 Conflict" \
            "ROUTES_OPERATION_BUSY" \
            "Другая конфликтующая операция уже выполняется." \
            "$details"
    fi

    broray_api_error \
        "502 Bad Gateway" \
        "ROUTES_DOWNLOAD_FAILED" \
        "Не удалось загрузить файлы маршрутов." \
        "$details"
fi

command_output="$(tail -n 30 "$OUTPUT_FILE" 2>/dev/null)"
completed_at="$(date '+%Y-%m-%dT%H:%M:%S%z')"

data_json="$(
    jq -c \
        --arg commandOutput "$command_output" \
        --arg completedAt "$completed_at" '
        . + {
            operation: {
                type: "download",
                completedAt: $completedAt,
                output: $commandOutput
            }
        }
    ' "$state_file"
)" || {
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_READ_FAILED" \
        "Не удалось прочитать результат загрузки маршрутов."
}

broray_api_success "$data_json"
