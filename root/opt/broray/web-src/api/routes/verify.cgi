#!/opt/bin/ash

set -u

PATH="/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

ROOT="${BRORAY_ROOT:-/opt/broray}"
AUTH_COMMON="$ROOT/web-new/api/auth-common.sh"
API_LOCK_LIBRARY="$ROOT/lib/routes-api-operation.sh"
CLI="$ROOT/bin/broray-routes"
BUNDLES="$ROOT/routes/bundles.json"
STATE_DIR="$ROOT/routes/state"

BUILD_OUTPUT="/tmp/broray-routes-verify-build-$$.out"
BUILD_ERROR="/tmp/broray-routes-verify-build-$$.err"
PLAN_OUTPUT="/tmp/broray-routes-verify-plan-$$.json"
PLAN_ERROR="/tmp/broray-routes-verify-plan-$$.err"
STATE_NEW=""

cleanup()
{
    rm -f \
        "$BUILD_OUTPUT" \
        "$BUILD_ERROR" \
        "$PLAN_OUTPUT" \
        "$PLAN_ERROR"
    [ -n "$STATE_NEW" ] && rm -f "$STATE_NEW"
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

[ -r "$API_LOCK_LIBRARY" ] || broray_api_error \
    "500 Internal Server Error" "ROUTES_API_LOCK_UNAVAILABLE" \
    "Модуль блокировки операций недоступен."
. "$API_LOCK_LIBRARY"
lock_rc=0
broray_routes_api_lock_acquire "verify" "$bundle_id" || lock_rc=$?
case "$lock_rc" in
    0) ;;
    2) broray_api_error "409 Conflict" "ROUTES_OPERATION_BUSY" \
        "Другая конфликтующая операция уже выполняется." ;;
    *) broray_api_error "500 Internal Server Error" "ROUTES_API_LOCK_FAILED" \
        "Не удалось установить блокировку операции." ;;
esac

STATE_FILE="$STATE_DIR/$bundle_id.json"
STATE_NEW="$STATE_FILE.new.$$"
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
    ((.downloadedVersion == null) or ((.downloadedVersion | type) == "object"))
' "$STATE_FILE" >/dev/null 2>&1
then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_INVALID" \
        "Локальное состояние маршрутов повреждено."
fi

if ! jq -e '.downloadedVersion != null' "$STATE_FILE" >/dev/null 2>&1; then
    broray_api_error \
        "409 Conflict" \
        "ROUTES_SET_NOT_DOWNLOADED" \
        "Сначала скачайте набор маршрутов."
fi

now="$(date '+%Y-%m-%dT%H:%M:%S%z')"

if ! "$CLI" build-export "$bundle_id" >"$BUILD_OUTPUT" 2>"$BUILD_ERROR"; then
    details="$(
        {
            tail -n 20 "$BUILD_ERROR" 2>/dev/null
            tail -n 20 "$BUILD_OUTPUT" 2>/dev/null
        } | tail -n 30
    )"

    if printf '%s\n' "$details" | grep -Fq 'Другая операция с маршрутами уже выполняется'; then
        broray_api_error \
            "409 Conflict" \
            "ROUTES_OPERATION_BUSY" \
            "Другая конфликтующая операция уже выполняется." \
            "$details"
    fi

    message="Локальный набор маршрутов не прошёл проверку. Скачайте файлы заново."

    if jq \
        --arg now "$now" \
        --arg message "$message" \
        --arg details "$details" '
        .lastVerifiedAt = $now |
        .verifyResult = {
            result: "invalid_local",
            success: false,
            message: $message,
            checkedAt: $now,
            contentSha256: (.downloadedVersion.contentSha256 // null),
            local: {
                valid: false,
                routeCount: (.routeCount // 0),
                sourceFileCount: (.downloadedVersion.sourceFileCount // 0),
                duplicateRouteCount: null,
                invalidRouteCount: null
            },
            keenetic: {
                checked: false,
                available: null,
                status: "not_checked",
                expectedRouteCount: null,
                presentRouteCount: null,
                missingRouteCount: null,
                conflictCount: null,
                externalRouteCount: null,
                complete: null,
                updatePending: null
            }
        } |
        .lastError = {
            code: "ROUTES_SET_INVALID",
            message: $message,
            details: $details
        } |
        .updatedAt = $now
    ' "$STATE_FILE" >"$STATE_NEW" &&
       chmod 644 "$STATE_NEW" 2>/dev/null &&
       mv "$STATE_NEW" "$STATE_FILE"
    then
        :
    else
        broray_api_error \
            "500 Internal Server Error" \
            "ROUTES_VERIFY_STATE_WRITE_FAILED" \
            "Не удалось сохранить результат проверки набора."
    fi

    broray_api_error \
        "422 Unprocessable Entity" \
        "ROUTES_SET_INVALID" \
        "$message" \
        "$details"
fi

if ! "$CLI" plan "$bundle_id" >"$PLAN_OUTPUT" 2>"$PLAN_ERROR"; then
    details="$(
        {
            tail -n 20 "$PLAN_ERROR" 2>/dev/null
            tail -n 20 "$PLAN_OUTPUT" 2>/dev/null
        } | tail -n 30
    )"

    if printf '%s\n' "$details" | grep -Fq 'Другая операция с маршрутами уже выполняется'; then
        broray_api_error \
            "409 Conflict" \
            "ROUTES_OPERATION_BUSY" \
            "Другая конфликтующая операция уже выполняется." \
            "$details"
    fi

    message="Локальный набор исправен, но проверить его состояние в Keenetic не удалось."

    if jq \
        --arg now "$now" \
        --arg message "$message" \
        --arg details "$details" '
        .lastVerifiedAt = $now |
        .verifyResult = {
            result: "router_unavailable",
            success: false,
            message: $message,
            checkedAt: $now,
            contentSha256: (.downloadedVersion.contentSha256 // null),
            local: {
                valid: true,
                routeCount: (.routeCount // 0),
                sourceFileCount: (.downloadedVersion.sourceFileCount // 0),
                duplicateRouteCount: 0,
                invalidRouteCount: 0
            },
            keenetic: {
                checked: false,
                available: false,
                status: "unavailable",
                expectedRouteCount: null,
                presentRouteCount: null,
                missingRouteCount: null,
                conflictCount: null,
                externalRouteCount: null,
                complete: null,
                updatePending: null
            }
        } |
        .lastError = {
            code: "ROUTES_VERIFY_ROUTER_FAILED",
            message: $message,
            details: $details
        } |
        .updatedAt = $now
    ' "$STATE_FILE" >"$STATE_NEW" &&
       chmod 644 "$STATE_NEW" 2>/dev/null &&
       mv "$STATE_NEW" "$STATE_FILE"
    then
        :
    else
        broray_api_error \
            "500 Internal Server Error" \
            "ROUTES_VERIFY_STATE_WRITE_FAILED" \
            "Не удалось сохранить результат проверки набора."
    fi

    broray_api_error \
        "502 Bad Gateway" \
        "ROUTES_VERIFY_ROUTER_FAILED" \
        "$message" \
        "$details"
fi

if ! jq -e \
    --arg bundle_id "$bundle_id" '
    (.schemaVersion == 1) and
    (.bundleId == $bundle_id) and
    ((.mode | type) == "string") and
    ((.canApply | type) == "boolean") and
    ((.summary | type) == "object") and
    ((.summary.total | type) == "number")
' "$PLAN_OUTPUT" >/dev/null 2>&1
then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_VERIFY_PLAN_INVALID" \
        "Результат проверки Keenetic повреждён."
fi

if ! jq \
    --arg now "$now" \
    --slurpfile plan "$PLAN_OUTPUT" '
    $plan[0] as $p |
    (.installedVersion != null) as $installed |
    (
        $installed and
        ((.downloadedVersion.contentSha256 // "") != (.installedVersion.contentSha256 // ""))
    ) as $updatePending |
    ($p.summary.total // 0) as $total |
    ($p.summary.managedExisting // 0) as $present |
    ($p.summary.toCreate // 0) as $missing |
    ($p.summary.conflicts // 0) as $conflicts |
    ($p.summary.externalExisting // 0) as $external |
    (
        if ($p.canApply | not) then "conflict"
        elif ($installed | not) then "not_installed"
        elif $updatePending then "update_pending"
        elif ($missing > 0) or (($p.summary.sharedToRestore // 0) > 0) then "restore_required"
        else "complete"
        end
    ) as $keeneticStatus |
    (
        $installed and
        ($updatePending | not) and
        $p.canApply and
        ($missing == 0) and
        ($conflicts == 0) and
        ($external == 0)
    ) as $complete |
    (
        if $keeneticStatus == "conflict" then
            "Локальный набор исправен, но в Keenetic обнаружены конфликты: " + ($conflicts | tostring) + "."
        elif $keeneticStatus == "not_installed" then
            "Набор исправен: " + ($total | tostring) + " маршрутов, ошибок и дубликатов нет. В Keenetic набор не установлен."
        elif $keeneticStatus == "update_pending" then
            "Набор исправен. Загруженная версия содержит " + ($total | tostring) + " маршрутов и ожидает обновления в Keenetic."
        elif $keeneticStatus == "restore_required" then
            "Набор исправен. В Keenetic найдено " + ($present | tostring) + " из " + ($total | tostring) + " маршрутов; отсутствует " + ($missing | tostring) + "."
        else
            "Набор исправен. В Keenetic найдено " + ($total | tostring) + " из " + ($total | tostring) + " маршрутов."
        end
    ) as $message |
    .lastVerifiedAt = $now |
    .verifyResult = {
        result: $keeneticStatus,
        success: ($keeneticStatus != "conflict"),
        message: $message,
        checkedAt: $now,
        contentSha256: (.downloadedVersion.contentSha256 // null),
        local: {
            valid: true,
            routeCount: $total,
            sourceFileCount: (.downloadedVersion.sourceFileCount // 0),
            duplicateRouteCount: 0,
            invalidRouteCount: 0
        },
        keenetic: {
            checked: true,
            available: true,
            status: $keeneticStatus,
            expectedRouteCount: $total,
            presentRouteCount: $present,
            missingRouteCount: $missing,
            conflictCount: $conflicts,
            externalRouteCount: $external,
            complete: $complete,
            updatePending: $updatePending,
            mode: $p.mode,
            canApply: $p.canApply,
            targetInterface: $p.targetInterface,
            managedMetric: $p.managedMetric
        }
    } |
    .lastError = null |
    .operation = {
        type: "verify",
        completedAt: $now,
        output: $message
    } |
    .updatedAt = $now
' "$STATE_FILE" >"$STATE_NEW"
then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_VERIFY_STATE_BUILD_FAILED" \
        "Не удалось сформировать результат проверки набора."
fi

chmod 644 "$STATE_NEW" 2>/dev/null || true
if ! mv "$STATE_NEW" "$STATE_FILE"; then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_VERIFY_STATE_WRITE_FAILED" \
        "Не удалось сохранить результат проверки набора."
fi

if ! data_json="$(jq -c . "$STATE_FILE")"; then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_READ_FAILED" \
        "Не удалось прочитать результат проверки набора."
fi

broray_api_success "$data_json"
