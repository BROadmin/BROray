#!/opt/bin/ash

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
. "$BRORAY_ROOT/web-new/api/auth-common.sh"

BRORAY_DOT_CLI="${BRORAY_DOT_CLI:-$BRORAY_ROOT/bin/broray-routes-dot}"
BRORAY_DOT_API_LOCK_LIBRARY="${BRORAY_DOT_API_LOCK_LIBRARY:-$BRORAY_ROOT/lib/routes-api-operation.sh}"
BRORAY_DOT_BODY_LIMIT=65536

broray_dot_api_read_body()
{
    target="$1"
    length="${CONTENT_LENGTH:-0}"
    case "$length" in ''|*[!0-9]*) broray_api_error "400 Bad Request" CONTENT_LENGTH_INVALID "Некорректный размер запроса." ;; esac
    [ "$length" -gt 0 ] || broray_api_error "400 Bad Request" REQUEST_BODY_REQUIRED "Тело запроса отсутствует."
    [ "$length" -le "$BRORAY_DOT_BODY_LIMIT" ] || broray_api_error "413 Payload Too Large" REQUEST_TOO_LARGE "Запрос DNS-over-TLS слишком велик."
    dd bs=1 count="$length" >"$target" 2>/dev/null || broray_api_error "400 Bad Request" REQUEST_BODY_READ_FAILED "Не удалось прочитать тело запроса."
    [ "$(wc -c <"$target" | tr -d ' ')" = "$length" ] || broray_api_error "400 Bad Request" REQUEST_BODY_INCOMPLETE "Тело запроса получено не полностью."
    jq -e 'type=="object"' "$target" >/dev/null 2>&1 || broray_api_error "400 Bad Request" REQUEST_JSON_INVALID "Запрос должен содержать корректный JSON-объект."
}

broray_dot_api_lock()
{
    action="$1"
    [ -r "$BRORAY_DOT_API_LOCK_LIBRARY" ] || broray_api_error "500 Internal Server Error" ROUTES_API_LOCK_UNAVAILABLE "Модуль блокировки операций недоступен."
    . "$BRORAY_DOT_API_LOCK_LIBRARY"
    rc=0
    broray_routes_api_lock_acquire "dot:$action" "dns-over-tls" || rc=$?
    case "$rc" in
      0) ;;
      2) broray_api_error "409 Conflict" ROUTES_OPERATION_BUSY "Другая конфликтующая операция уже выполняется." ;;
      *) broray_api_error "500 Internal Server Error" ROUTES_API_LOCK_FAILED "Не удалось установить блокировку операции." ;;
    esac
}

broray_dot_api_run()
{
    action="$1"
    shift
    output="$BRORAY_ROOT/tmp/dot-api-output.$$.json"
    error="$BRORAY_ROOT/tmp/dot-api-error.$$"
    mkdir -p "$BRORAY_ROOT/tmp"
    if "$BRORAY_DOT_CLI" "$action" "$@" >"$output" 2>"$error"; then
        jq -e 'type=="object"' "$output" >/dev/null 2>&1 || {
            details="$(tail -n 30 "$output" 2>/dev/null)"
            rm -f "$output" "$error"
            command -v broray_routes_api_lock_release >/dev/null 2>&1 && broray_routes_api_lock_release
            broray_api_error "500 Internal Server Error" DOT_RESPONSE_INVALID "Модуль DNS-over-TLS вернул некорректный ответ." "$details"
        }
        data="$(jq -c . "$output")"
        rm -f "$output" "$error"
        command -v broray_routes_api_lock_release >/dev/null 2>&1 && broray_routes_api_lock_release
        broray_api_success "$data"
        exit 0
    fi
    first="$(sed -n '1p' "$error" 2>/dev/null)"
    details="$(sed -n '2,40p' "$error" 2>/dev/null)"
    code=DOT_OPERATION_FAILED
    message="Операция DNS-over-TLS завершилась ошибкой."
    case "$first" in
      BRORAY_ERROR:*:*) rest="${first#BRORAY_ERROR:}"; code="${rest%%:*}"; message="${rest#*:}" ;;
      '') ;;
      *) details="$first${details:+
$details}" ;;
    esac
    status="400 Bad Request"
    case "$code" in
      ROUTES_OPERATION_BUSY) status="409 Conflict" ;;
      DOT_TEST_CONFIRMATION_REQUIRED|DOT_LIMIT_EXCEEDED) status="409 Conflict" ;;
      NDMC_UNAVAILABLE|DEPENDENCY_MISSING|MODULE_UNAVAILABLE|STORAGE_UNAVAILABLE|CONFIG_INVALID|STATE_INVALID) status="500 Internal Server Error" ;;
      KEENETIC_UNAVAILABLE|DOT_APPLY_FAILED|DOT_DELETE_FAILED) status="502 Bad Gateway" ;;
    esac
    rm -f "$output" "$error"
    command -v broray_routes_api_lock_release >/dev/null 2>&1 && broray_routes_api_lock_release
    broray_api_error "$status" "$code" "$message" "$details"
}
