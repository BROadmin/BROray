#!/opt/bin/ash

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
. "$BRORAY_ROOT/web-new/api/auth-common.sh"
. "$BRORAY_ROOT/lib/web-request-body.sh"

BRORAY_DOT_CLI="${BRORAY_DOT_CLI:-$BRORAY_ROOT/bin/broray-routes-dot}"
BRORAY_DOT_API_LOCK_LIBRARY="${BRORAY_DOT_API_LOCK_LIBRARY:-$BRORAY_ROOT/lib/routes-api-operation.sh}"
BRORAY_DOT_BODY_LIMIT=65536
BRORAY_DOT_API_CHILD_PID=''
BRORAY_DOT_API_REQUEST_FILE=''
BRORAY_DOT_API_OUTPUT_FILE=''
BRORAY_DOT_API_ERROR_FILE=''
BRORAY_DOT_API_LOCK_ACQUIRED=false

broray_dot_api_release()
{
    if [ "$BRORAY_DOT_API_LOCK_ACQUIRED" = true ] &&
       command -v broray_routes_api_lock_release >/dev/null 2>&1
    then
        broray_routes_api_lock_release || true
    fi
    BRORAY_DOT_API_LOCK_ACQUIRED=false
}

broray_dot_api_cleanup()
{
    cleanup_pid="$BRORAY_DOT_API_CHILD_PID"
    BRORAY_DOT_API_CHILD_PID=''
    case "$cleanup_pid" in
        ''|*[!0-9]*) ;;
        *)
            if kill -0 "$cleanup_pid" 2>/dev/null; then
                kill -TERM "$cleanup_pid" 2>/dev/null || true
            fi
            wait "$cleanup_pid" 2>/dev/null || true
            ;;
    esac
    [ -z "$BRORAY_DOT_API_REQUEST_FILE" ] || rm -f "$BRORAY_DOT_API_REQUEST_FILE"
    [ -z "$BRORAY_DOT_API_OUTPUT_FILE" ] || rm -f "$BRORAY_DOT_API_OUTPUT_FILE"
    [ -z "$BRORAY_DOT_API_ERROR_FILE" ] || rm -f "$BRORAY_DOT_API_ERROR_FILE"
    broray_dot_api_release
}

broray_dot_api_signal()
{
    signal_rc="$1"
    trap - EXIT HUP INT TERM
    broray_dot_api_cleanup
    exit "$signal_rc"
}

broray_dot_api_install_traps()
{
    trap broray_dot_api_cleanup EXIT
    trap 'broray_dot_api_signal 129' HUP
    trap 'broray_dot_api_signal 130' INT
    trap 'broray_dot_api_signal 143' TERM
}

broray_dot_api_read_body()
{
    target="$1"
    body_rc=0
    broray_web_request_body_to_file "$target" "$BRORAY_DOT_BODY_LIMIT" || body_rc=$?
    case "$body_rc" in
      0) ;;
      2) broray_api_error "400 Bad Request" CONTENT_LENGTH_INVALID "Некорректный размер запроса." ;;
      3) broray_api_error "400 Bad Request" REQUEST_BODY_REQUIRED "Тело запроса отсутствует." ;;
      4) broray_api_error "413 Payload Too Large" REQUEST_TOO_LARGE "Запрос DNS-over-TLS слишком велик." ;;
      5) broray_api_error "400 Bad Request" REQUEST_BODY_READ_FAILED "Не удалось прочитать тело запроса." ;;
      *) broray_api_error "400 Bad Request" REQUEST_BODY_INCOMPLETE "Тело запроса получено не полностью." ;;
    esac
    jq -e 'type=="object"' "$target" >/dev/null 2>&1 ||
        broray_api_error "400 Bad Request" REQUEST_JSON_INVALID "Запрос должен содержать корректный JSON-объект."
}

broray_dot_api_lock()
{
    action="$1"
    [ -r "$BRORAY_DOT_API_LOCK_LIBRARY" ] || broray_api_error "500 Internal Server Error" ROUTES_API_LOCK_UNAVAILABLE "Модуль блокировки операций недоступен."
    . "$BRORAY_DOT_API_LOCK_LIBRARY"
    rc=0
    broray_routes_api_lock_acquire "dot:$action" "dns-over-tls" || rc=$?
    case "$rc" in
      0) BRORAY_DOT_API_LOCK_ACQUIRED=true ;;
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
    BRORAY_DOT_API_OUTPUT_FILE="$output"
    BRORAY_DOT_API_ERROR_FILE="$error"
    mkdir -p "$BRORAY_ROOT/tmp"
    "$BRORAY_DOT_CLI" "$action" "$@" >"$output" 2>"$error" &
    BRORAY_DOT_API_CHILD_PID=$!
    child_rc=0
    wait "$BRORAY_DOT_API_CHILD_PID" || child_rc=$?
    BRORAY_DOT_API_CHILD_PID=''
    if [ "$child_rc" -eq 0 ]; then
        jq -e 'type=="object"' "$output" >/dev/null 2>&1 || {
            details="$(tail -n 30 "$output" 2>/dev/null)"
            rm -f "$output" "$error"
            broray_dot_api_release
            broray_api_error "500 Internal Server Error" DOT_RESPONSE_INVALID "Модуль DNS-over-TLS вернул некорректный ответ." "$details"
        }
        data="$(jq -c . "$output")"
        rm -f "$output" "$error"
        broray_dot_api_release
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
      DOT_TEST_REQUIRED|DOT_LIMIT_EXCEEDED|DOT_RECOVERY_REQUIRED|DOT_PHYSICAL_WRITE_PROTOCOL_REQUIRED|DOT_OBSERVATION_UNDERDETERMINED|DOT_DELETE_AUTHORITY_REFUSED|DOT_SELECTOR_CONFLICT) status="409 Conflict" ;;
      NDMC_UNAVAILABLE|DEPENDENCY_MISSING|MODULE_UNAVAILABLE|STORAGE_UNAVAILABLE|CONFIG_INVALID|CONFIG_MIGRATION_FAILED|STATE_INVALID|DOT_CATALOG_INVALID|DOT_TEST_FAILED) status="500 Internal Server Error" ;;
      KEENETIC_UNAVAILABLE|KEENETIC_RUNTIME_UNAVAILABLE|DOT_APPLY_FAILED|DOT_DELETE_FAILED) status="502 Bad Gateway" ;;
    esac
    rm -f "$output" "$error"
    broray_dot_api_release
    broray_api_error "$status" "$code" "$message" "$details"
}
