#!/opt/bin/ash

set -u

PATH="/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
BUNDLES="/opt/broray/routes/bundles.json"
PROGRESS_LIBRARY="/opt/broray/lib/routes-operation-progress.sh"
PROGRESS_FILE="/tmp/broray-routes-progress-$$.json"

cleanup()
{
    rm -f "$PROGRESS_FILE"
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
    ''|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
        broray_api_error \
            "400 Bad Request" \
            "ROUTES_BUNDLE_INVALID" \
            "Некорректный идентификатор набора маршрутов."
        ;;
esac

if [ ! -r "$BUNDLES" ] ||
   ! jq -e --arg bundleId "$bundle_id" '
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (.bundles | index($bundleId) != null)
   ' "$BUNDLES" >/dev/null 2>&1
then
    broray_api_error \
        "404 Not Found" \
        "ROUTES_BUNDLE_NOT_FOUND" \
        "Набор маршрутов не найден."
fi

[ -r "$PROGRESS_LIBRARY" ] ||
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_PROGRESS_UNAVAILABLE" \
        "Модуль прогресса операций недоступен."

. "$PROGRESS_LIBRARY"

broray_routes_progress_read "$bundle_id" >"$PROGRESS_FILE" ||
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_PROGRESS_INVALID" \
        "Состояние прогресса операции повреждено."

jq -e --arg bundleId "$bundle_id" '
    (.schemaVersion == 1) and
    (.kind == "routes") and
    (.bundleId == $bundleId) and
    ((.running | type) == "boolean") and
    ((.current | type) == "number") and
    ((.total | type) == "number") and
    ((.percent | type) == "number")
' "$PROGRESS_FILE" >/dev/null 2>&1 ||
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_PROGRESS_INVALID" \
        "Состояние прогресса операции повреждено."

broray_api_success "$(jq -c . "$PROGRESS_FILE")"
