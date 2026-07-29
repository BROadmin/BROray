#!/opt/bin/ash

set -u

PATH="/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

ROOT="${BRORAY_ROOT:-/opt/broray}"
AUTH_COMMON="$ROOT/web-new/api/auth-common.sh"
API_LOCK_LIBRARY="$ROOT/lib/routes-api-operation.sh"
PREFLIGHT_LIBRARY="$ROOT/lib/routes-operation-preflight.sh"
CLI="$ROOT/bin/broray-routes"
BUNDLES="$ROOT/routes/bundles.json"
PLAN_FILE="/tmp/broray-routes-operation-preflight-plan-$$.json"
RESULT_FILE="/tmp/broray-routes-operation-preflight-result-$$.json"
ERROR_FILE="/tmp/broray-routes-operation-preflight-$$.err"

cleanup()
{
    rm -f "$PLAN_FILE" "$RESULT_FILE" "$ERROR_FILE" \
        "$PLAN_FILE.actual.$$" "$PLAN_FILE.actual.$$.json" \
        "$PLAN_FILE.actual.$$.json.raw" "$PLAN_FILE.actual.$$.json.tsv" \
        "$PLAN_FILE.actual.$$.json.err"
    command -v broray_routes_api_lock_release >/dev/null 2>&1 &&
        broray_routes_api_lock_release
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

[ -x "$CLI" ] || broray_api_error \
    "500 Internal Server Error" "ROUTES_CLI_UNAVAILABLE" \
    "Команда управления маршрутами недоступна."
[ -r "$API_LOCK_LIBRARY" ] || broray_api_error \
    "500 Internal Server Error" "ROUTES_API_LOCK_UNAVAILABLE" \
    "Модуль блокировки операций недоступен."
[ -r "$PREFLIGHT_LIBRARY" ] || broray_api_error \
    "500 Internal Server Error" "ROUTES_PREFLIGHT_UNAVAILABLE" \
    "Модуль предварительной проверки недоступен."
. "$API_LOCK_LIBRARY"
. "$PREFLIGHT_LIBRARY"

bundle_id=""
action=""
old_ifs="$IFS"
IFS='&'
set -- ${QUERY_STRING:-}
IFS="$old_ifs"
for item in "$@"; do
    case "$item" in
        bundleId=*) bundle_id="${item#bundleId=}" ;;
        action=*) action="${item#action=}" ;;
    esac
done

case "$bundle_id" in
    ''|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
        broray_api_error "400 Bad Request" "ROUTES_BUNDLE_INVALID" \
            "Некорректный идентификатор набора маршрутов."
        ;;
esac
case "$action" in
    export|delete|resume) ;;
    *) broray_api_error "400 Bad Request" "ROUTES_PREFLIGHT_ACTION_INVALID" \
        "Некорректный тип предварительной проверки." ;;
esac
jq -e --arg id "$bundle_id" '.schemaVersion == 1 and (.bundles | index($id) != null)' \
    "$BUNDLES" >/dev/null 2>&1 || broray_api_error \
    "404 Not Found" "ROUTES_BUNDLE_NOT_FOUND" "Набор маршрутов не найден."

lock_rc=0
broray_routes_api_lock_acquire "preflight:$action" "$bundle_id" || lock_rc=$?
case "$lock_rc" in
    0) ;;
    2) broray_api_error "409 Conflict" "ROUTES_OPERATION_BUSY" \
        "Другая конфликтующая операция уже выполняется." ;;
    *) broray_api_error "500 Internal Server Error" "ROUTES_API_LOCK_FAILED" \
        "Не удалось установить блокировку операции." ;;
esac

resolved_values="$(broray_routes_operation_preflight_resolve_action "$bundle_id" "$action")" || {
    rc=$?
    [ "$rc" -eq 2 ] && broray_api_error "409 Conflict" "ROUTES_RESUME_NOT_READY" \
        "Для этого набора нет операции, которую можно продолжить."
    broray_api_error "400 Bad Request" "ROUTES_PREFLIGHT_ACTION_INVALID" \
        "Не удалось определить тип операции."
}
resolved_action="$(printf '%s' "$resolved_values" | cut -f1)"
saved_operation="$(printf '%s' "$resolved_values" | cut -f2)"

if [ "$resolved_action" = export ]; then
    if ! "$CLI" plan "$bundle_id" >"$PLAN_FILE" 2>"$ERROR_FILE"; then
        details="$(tail -n 40 "$ERROR_FILE" 2>/dev/null)"
        if printf '%s\n' "$details" | grep -Fq 'Другая операция с маршрутами уже выполняется'; then
            broray_api_error "409 Conflict" "ROUTES_OPERATION_BUSY" \
                "Другая операция с маршрутами уже выполняется." "$details"
        fi
        broray_api_error "409 Conflict" "ROUTES_PREFLIGHT_PLAN_FAILED" \
            "Предварительная проверка установки в Keenetic не завершена." "$details"
    fi
else
    broray_routes_operation_preflight_delete_plan "$bundle_id" "$PLAN_FILE" ||
        broray_api_error "409 Conflict" "ROUTES_PREFLIGHT_DELETE_FAILED" \
            "Предварительная проверка удаления из Keenetic не завершена."
fi

broray_routes_operation_preflight_finalize \
    "$bundle_id" "$action" "$resolved_action" "$saved_operation" \
    "$PLAN_FILE" "$RESULT_FILE" || broray_api_error \
        "500 Internal Server Error" "ROUTES_PREFLIGHT_BUILD_FAILED" \
        "Не удалось сформировать результат предварительной проверки."

result_json="$(jq -c . "$RESULT_FILE")" || broray_api_error \
    "500 Internal Server Error" "ROUTES_PREFLIGHT_READ_FAILED" \
    "Не удалось прочитать результат предварительной проверки."
broray_api_success "$result_json"
