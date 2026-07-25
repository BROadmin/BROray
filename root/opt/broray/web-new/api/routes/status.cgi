#!/opt/bin/ash

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
BUNDLES="/opt/broray/routes/bundles.json"
PRESENCE_LIBRARY="/opt/broray/lib/routes-router-presence.sh"
ERROR_FILE="/tmp/broray-routes-status-$$.err"
PRESENCE_FILE="/tmp/broray-routes-presence-$$.json"

cleanup()
{
    rm -f "$ERROR_FILE" "$PRESENCE_FILE"
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

if [ ! -r "$BUNDLES" ] ||
   ! jq -e \
        --arg bundle_id "$bundle_id" '
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (.bundles | index($bundle_id) != null)
    ' "$BUNDLES" >/dev/null 2>&1
then
    broray_api_error \
        "404 Not Found" \
        "ROUTES_BUNDLE_NOT_FOUND" \
        "Набор маршрутов не найден."
fi

STATE_FILE="/opt/broray/routes/state/$bundle_id.json"

if [ ! -r "$STATE_FILE" ]; then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_UNAVAILABLE" \
        "Локальное состояние маршрутов недоступно."
fi

if ! jq -e \
    --arg bundle_id "$bundle_id" '
    (.schemaVersion == 1) and
    (.bundleId == $bundle_id) and
    ((.status | type) == "string") and
    ((.availableVersion == null) or ((.availableVersion | type) == "object")) and
    ((.downloadedVersion == null) or ((.downloadedVersion | type) == "object")) and
    ((.installedVersion == null) or ((.installedVersion | type) == "object")) and
    ((.lastError == null) or ((.lastError | type) == "string") or ((.lastError | type) == "object"))
' "$STATE_FILE" >/dev/null 2>"$ERROR_FILE"
then
    details="$(tail -n 20 "$ERROR_FILE" 2>/dev/null)"

    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_INVALID" \
        "Локальное состояние маршрутов повреждено." \
        "$details"
fi

presence_json='{
    "available": false,
    "registered": false,
    "checkedAt": null,
    "expectedRouteCount": null,
    "presentRouteCount": null,
    "missingRouteCount": null,
    "duplicateRouteCount": null,
    "complete": null,
    "actualInstalled": null,
    "drift": null,
    "status": "unavailable",
    "missingRoutes": []
}'

if [ -r "$PRESENCE_LIBRARY" ]; then
    . "$PRESENCE_LIBRARY"

    if broray_routes_presence_bundle "$bundle_id" "$PRESENCE_FILE" &&
       jq -e 'type == "object"' "$PRESENCE_FILE" >/dev/null 2>&1
    then
        presence_json="$(jq -c . "$PRESENCE_FILE")"
    fi
fi

data_json="$(
    jq -c \
        --argjson router_presence "$presence_json" '
        {
            schemaVersion,
            bundleId,
            status,
            availableVersion,
            downloadedVersion,
            installedVersion,
            routeCount,
            lastCheckedAt,
            lastDownloadedAt,
            lastExportedAt,
            lastDeletedAt,
            lastError,
            checkResult,
            downloadResult,
            exportBuild,
            preflight,
            exportResult,
            deleteResult,
            updatedAt,
            routerPresence: $router_presence
        }
    ' "$STATE_FILE"
)" || {
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_READ_FAILED" \
        "Не удалось прочитать локальное состояние маршрутов."
}

broray_api_success "$data_json"
