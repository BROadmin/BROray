#!/opt/bin/ash

set -u

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
PROGRESS_LIBRARY="/opt/broray/lib/routes-operation-progress.sh"
BUNDLES="/opt/broray/routes/bundles.json"

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

[ -r "$PROGRESS_LIBRARY" ] || broray_api_error \
    "500 Internal Server Error" "ROUTES_PROGRESS_UNAVAILABLE" \
    "Модуль прогресса маршрутов недоступен."
. "$PROGRESS_LIBRARY"

query="${QUERY_STRING:-}"
case "$query" in bundleId=*) bundle_id="${query#bundleId=}" ;; *) bundle_id="" ;; esac
case "$bundle_id" in
    ''|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
        broray_api_error "400 Bad Request" "ROUTES_BUNDLE_INVALID" \
            "Некорректный идентификатор набора маршрутов."
        ;;
esac
jq -e --arg id "$bundle_id" '.bundles | index($id) != null' "$BUNDLES" >/dev/null 2>&1 ||
    broray_api_error "404 Not Found" "ROUTES_BUNDLE_NOT_FOUND" "Набор маршрутов не найден."

if ! broray_routes_progress_request_stop "$bundle_id"; then
    rc=$?
    case "$rc" in
        2|3) broray_api_error "409 Conflict" "ROUTES_STOP_NOT_RUNNING" \
            "Операция с этим набором сейчас не выполняется." ;;
        *) broray_api_error "500 Internal Server Error" "ROUTES_STOP_FAILED" \
            "Не удалось запросить безопасную остановку операции." ;;
    esac
fi

data_json="$(broray_routes_progress_read "$bundle_id" | jq -c '.')" ||
    broray_api_error "500 Internal Server Error" "ROUTES_PROGRESS_READ_FAILED" \
        "Не удалось прочитать состояние операции."
broray_api_success "$data_json"
