#!/opt/bin/ash

set -u

PATH="/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
API_LOCK_LIBRARY="/opt/broray/lib/routes-api-operation.sh"
PREFLIGHT_LIBRARY="/opt/broray/lib/routes-operation-preflight.sh"
CLI="/opt/broray/bin/broray-routes"
BUNDLES="/opt/broray/routes/bundles.json"
CONFIG="/opt/broray/routes/config.json"
MANIFEST_DIR="/opt/broray/routes/manifests"
STATE_DIR="/opt/broray/routes/state"
BUNDLE_REGISTRY_DIR="/opt/broray/routes/installed/bundles"
PROGRESS_DIR="/opt/broray/routes/operations"

ACTION="${ROUTES_ACTION:-}"
OUTPUT_FILE="/tmp/broray-routes-${ACTION:-action}-$$.out"
ERROR_FILE="/tmp/broray-routes-${ACTION:-action}-$$.err"

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

case "$ACTION" in
    export|delete|resume)
        ;;
    *)
        broray_api_error \
            "500 Internal Server Error" \
            "ROUTES_ACTION_INVALID" \
            "Операция с маршрутами настроена неверно."
        ;;
esac

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
bundle_id=""
preflight_token=""
old_ifs="$IFS"
IFS='&'
set -- $query
IFS="$old_ifs"
for query_item in "$@"; do
    case "$query_item" in
        bundleId=*) bundle_id="${query_item#bundleId=}" ;;
        preflightToken=*) preflight_token="${query_item#preflightToken=}" ;;
    esac
done

[ -n "$bundle_id" ] || broray_api_error \
    "400 Bad Request" "ROUTES_BUNDLE_REQUIRED" \
    "Не указан идентификатор набора маршрутов."

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
[ -r "$PREFLIGHT_LIBRARY" ] || broray_api_error \
    "500 Internal Server Error" "ROUTES_PREFLIGHT_UNAVAILABLE" \
    "Модуль предварительной проверки недоступен."
. "$API_LOCK_LIBRARY"
. "$PREFLIGHT_LIBRARY"
lock_rc=0
broray_routes_api_lock_acquire "$ACTION" "$bundle_id" || lock_rc=$?
case "$lock_rc" in
    0) ;;
    2) broray_api_error "409 Conflict" "ROUTES_OPERATION_BUSY" \
        "Другая конфликтующая операция уже выполняется." ;;
    *) broray_api_error "500 Internal Server Error" "ROUTES_API_LOCK_FAILED" \
        "Не удалось установить блокировку операции." ;;
esac

preflight_rc=0
broray_routes_operation_preflight_validate \
    "$bundle_id" "$ACTION" "$preflight_token" || preflight_rc=$?
case "$preflight_rc" in
    0) ;;
    2) broray_api_error "409 Conflict" "ROUTES_PREFLIGHT_REQUIRED" \
        "Перед запуском выполните предварительную проверку и подтвердите актуальный план." ;;
    3) broray_api_error "409 Conflict" "ROUTES_PREFLIGHT_EXPIRED" \
        "Предварительная проверка устарела. Выполните её повторно." ;;
    4) broray_api_error "409 Conflict" "ROUTES_PREFLIGHT_CHANGED" \
        "Состояние набора изменилось после проверки. Выполните предварительную проверку повторно." ;;
    *) broray_api_error "500 Internal Server Error" "ROUTES_PREFLIGHT_VALIDATE_FAILED" \
        "Не удалось подтвердить предварительную проверку." ;;
esac

managed_interface="$(
    jq -r '.managedInterface // empty' \
        "$CONFIG" 2>/dev/null
)"

managed_metric="$(
    jq -r '.managedMetric // empty' \
        "$CONFIG" 2>/dev/null
)"

case "$managed_interface" in
    Proxy[0-9]*) ;;
    *) managed_interface="" ;;
esac
case "${managed_interface#Proxy}" in
    ''|*[!0-9]*) managed_interface="" ;;
esac

if [ -z "$managed_interface" ] ||
   [ "$managed_metric" != "1200" ]
then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_POLICY_INVALID" \
        "Политика управляемых маршрутов настроена неверно."
fi

manifest="$MANIFEST_DIR/$bundle_id.json"
state_file="$STATE_DIR/$bundle_id.json"
bundle_registry="$BUNDLE_REGISTRY_DIR/$bundle_id.json"
progress_file="$PROGRESS_DIR/$bundle_id.json"
result_action="$ACTION"
paused_result=false

if [ ! -r "$manifest" ] ||
   [ ! -r "$state_file" ] ||
   [ ! -r "$bundle_registry" ]
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

case "$ACTION" in
    export)
        # Export is intentionally idempotent. The same downloaded version may
        # be exported again to restore routes deleted manually in Keenetic.
        # The CLI rebuilds the plan and checks the full running configuration,
        # so only actually missing routes are recreated.
        if ! jq -e '
            (.downloadedVersion != null)
        ' "$state_file" >/dev/null 2>&1
        then
            broray_api_error \
                "409 Conflict" \
                "ROUTES_EXPORT_NOT_READY" \
                "Маршруты ещё не загружены и не готовы к установке в Keenetic."
        fi
        ;;
    delete)
        if ! jq -e '
            (.installedVersion != null) and
            (((.managedRouteKeys // []) | length) > 0)
        ' "$bundle_registry" >/dev/null 2>&1
        then
            broray_api_error \
                "409 Conflict" \
                "ROUTES_DELETE_NOT_READY" \
                "Управляемые маршруты этого набора не установлены."
        fi
        ;;
    resume)
        if [ ! -r "$progress_file" ] ||
           ! jq -e '.resumable == true and .running == false' "$progress_file" >/dev/null 2>&1
        then
            broray_api_error \
                "409 Conflict" \
                "ROUTES_RESUME_NOT_READY" \
                "Для этого набора нет операции, которую можно продолжить."
        fi
        result_action="$(jq -r '.operation // empty' "$progress_file")"
        case "$result_action" in
            install|update|restore) result_action="export" ;;
            delete) ;;
            *)
                broray_api_error \
                    "409 Conflict" \
                    "ROUTES_RESUME_TYPE_INVALID" \
                    "Тип сохранённой операции не поддерживается."
                ;;
        esac
        ;;
esac

if "$CLI" "$ACTION" "$bundle_id" \
    >"$OUTPUT_FILE" \
    2>"$ERROR_FILE"
then
    command_ok=true
    command_rc=0
else
    command_ok=false
    command_rc=$?
fi

details="$(
    {
        tail -n 40 "$ERROR_FILE" 2>/dev/null
        tail -n 40 "$OUTPUT_FILE" 2>/dev/null
    } |
        tail -n 60
)"

if [ "$command_ok" != true ] && { [ "$command_rc" = 74 ] || [ "$command_rc" = 76 ]; }; then
    command_ok=true
    paused_result=true
fi

if [ "$command_ok" != true ]; then
    if printf '%s\n' "$details" |
        grep -Fq "Другая операция с маршрутами уже выполняется."
    then
        broray_api_error \
            "409 Conflict" \
            "ROUTES_OPERATION_BUSY" \
            "Другая конфликтующая операция уже выполняется." \
            "$details"
    fi

    case "$command_rc" in
        73|75)
            broray_api_error \
                "409 Conflict" \
                "ROUTES_OPERATION_BUSY" \
                "Другая конфликтующая операция уже выполняется." \
                "$details"
            ;;
    esac

    case "$ACTION" in
        export)
            error_code="ROUTES_EXPORT_FAILED"
            error_message="Не удалось установить маршруты в Keenetic."
            ;;
        delete)
            error_code="ROUTES_DELETE_FAILED"
            error_message="Не удалось удалить маршруты из Keenetic."
            ;;
        resume)
            error_code="ROUTES_RESUME_FAILED"
            error_message="Не удалось продолжить операцию с маршрутами."
            ;;
    esac

    broray_api_error \
        "502 Bad Gateway" \
        "$error_code" \
        "$error_message" \
        "$details"
fi

if ! jq -e \
    --arg bundle_id "$bundle_id" '
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        ((.status | type) == "string")
    ' "$state_file" >/dev/null 2>&1
then
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_INVALID" \
        "Состояние набора маршрутов повреждено после операции." \
        "$details"
fi

if [ "$paused_result" != true ]; then
case "$result_action" in
    export)
        if ! jq -e '
            (.status == "installed") and
            (.installedVersion != null) and
            (.exportResult.result == "installed")
        ' "$state_file" >/dev/null 2>&1
        then
            broray_api_error \
                "500 Internal Server Error" \
                "ROUTES_EXPORT_RESULT_INVALID" \
                "Результат установки не подтверждён локальным состоянием." \
                "$details"
        fi
        ;;
    delete)
        if ! jq -e '
            (.status == "downloaded") and
            (.installedVersion == null) and
            (.deleteResult.result == "removed")
        ' "$state_file" >/dev/null 2>&1
        then
            broray_api_error \
                "500 Internal Server Error" \
                "ROUTES_DELETE_RESULT_INVALID" \
                "Результат удаления не подтверждён локальным состоянием." \
                "$details"
        fi
        ;;
esac
fi

command_output="$(tail -n 60 "$OUTPUT_FILE" 2>/dev/null)"
completed_at="$(date '+%Y-%m-%dT%H:%M:%S%z')"
operation_progress="null"
if [ -r "$progress_file" ]; then
    operation_progress="$(jq -c '.' "$progress_file" 2>/dev/null || printf null)"
fi

data_json="$(
    jq -c \
        --arg action "$ACTION" \
        --arg commandOutput "$command_output" \
        --arg completedAt "$completed_at" \
        --argjson operationProgress "$operation_progress" '
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
            resumeOperation,
            operationProgress: $operationProgress,
            updatedAt,
            operation: {
                type: $action,
                completedAt: $completedAt,
                output: $commandOutput
            }
        }
    ' "$state_file"
)" || {
    broray_api_error \
        "500 Internal Server Error" \
        "ROUTES_STATE_READ_FAILED" \
        "Не удалось прочитать результат операции с маршрутами."
}

broray_api_success "$data_json"
