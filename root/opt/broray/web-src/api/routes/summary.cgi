#!/opt/bin/ash

set -u

PATH="/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
SUMMARY_LIBRARY="/opt/broray/lib/routes-summary.sh"
OUTPUT_FILE="/opt/broray/tmp/routes-summary-api-output.$$.json"
ERROR_FILE="/opt/broray/tmp/routes-summary-api-error.$$"

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
    printf '%s\n' \
        '{"success":false,"data":null,"error":{"code":"AUTH_MODULE_UNAVAILABLE","message":"Модуль авторизации недоступен."}}'
    exit 0
fi

. "$AUTH_COMMON"

broray_api_require_method GET
broray_api_require_session

if [ ! -r "$SUMMARY_LIBRARY" ]; then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_SUMMARY_UNAVAILABLE" \
        "Summary маршрутов недоступен."
fi

. "$SUMMARY_LIBRARY"

mkdir -p /opt/broray/tmp

query="${QUERY_STRING:-}"

case "$query" in
    "")

CONFIG="/opt/broray/routes/config.json"
managed_interface="$(jq -r '.managedInterface // empty' "$CONFIG" 2>/dev/null)"
case "$managed_interface" in
    Proxy[0-9]*) ;;
    *) managed_interface="" ;;
esac
case "${managed_interface#Proxy}" in
    ''|*[!0-9]*) managed_interface="" ;;
esac

[ -n "$managed_interface" ] ||
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_INTERFACE_POLICY_INVALID" \
        "Разрешённый интерфейс маршрутов настроен неверно."
summary_mode="all"
        ;;
    bundleId=*)
        bundle_id="${query#bundleId=}"
        case "$bundle_id" in
            ""|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
                broray_api_error \
                    "400 Bad Request" \
                    "ROUTES_BUNDLE_INVALID" \
                    "Некорректный идентификатор набора маршрутов."
                ;;
        esac
        summary_mode="one"
        ;;
    *)
        broray_api_error \
            "400 Bad Request" \
            "ROUTES_QUERY_INVALID" \
            "Некорректные параметры запроса."
        ;;
esac

if [ "$summary_mode" = "all" ]; then
    summary_command="broray_routes_summary_all"
else
    summary_command="broray_routes_summary"
fi

if ! "$summary_command" ${bundle_id:-} \
    >"$OUTPUT_FILE" \
    2>"$ERROR_FILE"
then
    error_line="$(
        grep 'BRORAY_ERROR:' "$ERROR_FILE" 2>/dev/null |
            tail -n 1
    )"
    error_code="$(
        printf '%s' "$error_line" |
            cut -d: -f2
    )"
    error_message="$(
        printf '%s' "$error_line" |
            cut -d: -f3-
    )"

    [ -n "$error_code" ] ||
        error_code="ROUTES_SUMMARY_FAILED"
    [ -n "$error_message" ] ||
        error_message="Не удалось получить summary маршрутов."

    broray_api_error \
        "500 Internal Server Error" \
        "$error_code" \
        "$error_message"
fi

if [ "$summary_mode" = "all" ]; then
    summary_valid_filter='
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        ((.availableBundles | type) == "number") and
        ((.installedBundles | type) == "number") and
        ((.installedRouteCount | type) == "number")
    '
else
    summary_valid_filter='
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        ((.state | type) == "string") and
        ((.installed | type) == "boolean") and
        ((.downloaded | type) == "boolean") and
        ((.updateAvailable | type) == "boolean") and
        ((.routeCount | type) == "number") and
        ((.installedRouteCount | type) == "number") and
        (.managedInterface == $managed_interface) and
        (.managedMetric == 1200)
    '
fi

if ! jq -e \
    --arg bundle_id "${bundle_id:-}" \
    --arg managed_interface "$managed_interface" \
    "$summary_valid_filter" \
    "$OUTPUT_FILE" >/dev/null 2>&1
then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_SUMMARY_INVALID" \
        "Модуль маршрутов вернул некорректный summary."
fi

broray_api_success "$(cat "$OUTPUT_FILE")"
