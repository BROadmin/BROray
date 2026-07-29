#!/opt/bin/ash

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
BUNDLES="/opt/broray/routes/bundles.json"
PRESENCE_LIBRARY="/opt/broray/lib/routes-router-presence.sh"
PROGRESS_LIBRARY="/opt/broray/lib/routes-operation-progress.sh"
API_LOCK_LIBRARY="/opt/broray/lib/routes-api-operation.sh"
ERROR_FILE="/tmp/broray-routes-status-$$.err"
PRESENCE_FILE="/tmp/broray-routes-presence-$$.json"
PROGRESS_FILE="/tmp/broray-routes-progress-status-$$.json"
GLOBAL_OPERATION_FILE="/tmp/broray-routes-global-operation-$$.json"

cleanup()
{
    rm -f "$ERROR_FILE" "$PRESENCE_FILE" "$PROGRESS_FILE" "$GLOBAL_OPERATION_FILE"
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
    ((.lastError == null) or ((.lastError | type) == "string") or ((.lastError | type) == "object")) and
    ((.lastVerifiedAt == null) or ((.lastVerifiedAt | type) == "string")) and
    ((.verifyResult == null) or ((.verifyResult | type) == "object"))
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

progress_json="$(
    jq -n --arg bundleId "$bundle_id" '
        {
            schemaVersion: 1,
            kind: "routes",
            bundleId: $bundleId,
            operation: null,
            phase: "idle",
            current: 0,
            total: 0,
            percent: 0,
            currentRoute: null,
            message: "Операция не выполняется.",
            running: false,
            success: null,
            rolledBack: false,
            pid: null,
            startedAt: null,
            updatedAt: null,
            completedAt: null
        }
    '
)"

if [ -r "$PROGRESS_LIBRARY" ]; then
    . "$PROGRESS_LIBRARY"
    if broray_routes_progress_read "$bundle_id" >"$PROGRESS_FILE" &&
       jq -e 'type == "object"' "$PROGRESS_FILE" >/dev/null 2>&1
    then
        progress_json="$(jq -c . "$PROGRESS_FILE")"
    fi
fi

global_operation_json='{"active":false,"pid":null,"scope":null,"action":null,"bundleId":null,"startedAt":null,"stale":false}'
if [ -r "$API_LOCK_LIBRARY" ]; then
    . "$API_LOCK_LIBRARY"
    if broray_routes_api_lock_read_json >"$GLOBAL_OPERATION_FILE" &&
       jq -e 'type == "object"' "$GLOBAL_OPERATION_FILE" >/dev/null 2>&1
    then
        global_operation_json="$(jq -c . "$GLOBAL_OPERATION_FILE")"
    fi
fi

data_json="$(
    jq -c \
        --argjson router_presence "$presence_json" \
        --argjson operation_progress "$progress_json" \
        --argjson global_operation "$global_operation_json" '
        {
            schemaVersion,
            bundleId,
            status,
            availableVersion,
            downloadedVersion,
            installedVersion,
            routeCount,
            lastCheckedAt,
            lastVerifiedAt,
            lastDownloadedAt,
            lastExportedAt,
            lastDeletedAt,
            lastError,
            checkResult,
            verifyResult,
            downloadResult,
            exportBuild,
            preflight,
            exportResult,
            deleteResult,
            updatedAt,
            routerPresence: $router_presence,
            operationProgress: $operation_progress,
            globalOperation: $global_operation
        }
    ' "$STATE_FILE"
)" || {
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_READ_FAILED" \
        "Не удалось прочитать локальное состояние маршрутов."
}

broray_api_success "$data_json"
