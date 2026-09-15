#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh
. /opt/broray/lib/web-request-body.sh
. /opt/broray/lib/subscription-service.sh

broray_subscriptions_api_lock()
{
    action="$1"
    [ -r /opt/broray/lib/routes-api-operation.sh ] ||
        broray_api_error "500 Internal Server Error" "GLOBAL_LOCK_UNAVAILABLE" "Общий координатор операций недоступен."
    . /opt/broray/lib/routes-api-operation.sh
    lock_rc=0
    broray_routes_api_lock_acquire "subscriptions:$action" subscriptions || lock_rc=$?
    case "$lock_rc" in
        0)
            trap 'broray_routes_api_lock_release' EXIT
            trap 'exit 129' HUP
            trap 'exit 130' INT
            trap 'exit 143' TERM
            ;;
        2) broray_api_error "409 Conflict" "OPERATION_BUSY" "Другая конфликтующая операция BROray уже выполняется." ;;
        *) broray_api_error "500 Internal Server Error" "GLOBAL_LOCK_FAILED" "Не удалось установить общую блокировку BROray." ;;
    esac
}

broray_subscriptions_api_read_body_to_file()
{
    broray_subscriptions_body_source="$1"
    [ -n "$broray_subscriptions_body_source" ] ||
        broray_api_error "500 Internal Server Error" "BODY_TARGET_INVALID" "Внутренний файл тела запроса не указан."
    broray_subscriptions_body_rc=0
    broray_web_request_body_to_file "$broray_subscriptions_body_source" 65536 || broray_subscriptions_body_rc=$?
    case "$broray_subscriptions_body_rc" in
        0) ;;
        2) broray_api_error "411 Length Required" "CONTENT_LENGTH_REQUIRED" "Для запроса требуется корректный Content-Length." ;;
        3) broray_api_error "400 Bad Request" "REQUEST_BODY_REQUIRED" "Тело запроса отсутствует." ;;
        4) broray_api_error "413 Payload Too Large" "REQUEST_TOO_LARGE" "Тело запроса слишком большое." ;;
        5) broray_api_error "400 Bad Request" "REQUEST_BODY_READ_FAILED" "Не удалось прочитать тело запроса." ;;
        *) broray_api_error "400 Bad Request" "REQUEST_BODY_INCOMPLETE" "Тело запроса получено не полностью." ;;
    esac
    jq -e 'type == "object"' "$broray_subscriptions_body_source" >/dev/null 2>&1 ||
        broray_api_error "400 Bad Request" "REQUEST_JSON_INVALID" "Тело запроса должно быть корректным JSON-объектом."
}

broray_subscriptions_api_query()
{
    query_name="$1"
    printf '%s' "${QUERY_STRING:-}" |
        tr '&' '\n' |
        awk -F= -v name="$query_name" '$1 == name {sub(/^[^=]*=/, ""); print; exit}'
}

broray_subscriptions_api_error_status()
{
    broray_subscriptions_api_error_code="$1"
    case "$broray_subscriptions_api_error_code" in
        SUBSCRIPTION_NOT_FOUND)
            printf '%s\n' "404 Not Found"
            ;;
        UPDATE_ALREADY_RUNNING|ACTIVE_SERVER_CONFLICT|SERVER_SYNC_BUSY|SERVER_ID_CONFLICT)
            printf '%s\n' "409 Conflict"
            ;;
        DOWNLOAD_TIMEOUT)
            printf '%s\n' "504 Gateway Timeout"
            ;;
        HTTP_ERROR|DOWNLOAD_SECURITY)
            printf '%s\n' "502 Bad Gateway"
            ;;
        CONTENT_TOO_LARGE)
            printf '%s\n' "413 Payload Too Large"
            ;;
        INTERNAL_ERROR|PERSISTENCE_ERROR|SERVER_SYNC_ERROR|SERVER_SOURCE_REMOVE_FAILED|SERVER_SOURCE_STATE_FAILED)
            printf '%s\n' "500 Internal Server Error"
            ;;
        *)
            printf '%s\n' "400 Bad Request"
            ;;
    esac
}

broray_subscriptions_api_run()
{
    broray_subscriptions_api_output_file="/opt/broray/tmp/subscriptions-api-output.$$.json"
    broray_subscriptions_api_error_file="/opt/broray/tmp/subscriptions-api-error.$$"
    mkdir -p /opt/broray/tmp
    if "$@" > "$broray_subscriptions_api_output_file" 2> "$broray_subscriptions_api_error_file"; then
        if ! jq -e . "$broray_subscriptions_api_output_file" >/dev/null 2>&1; then
            rm -f "$broray_subscriptions_api_output_file" "$broray_subscriptions_api_error_file"
            broray_api_error \
                "500 Internal Server Error" \
                "INVALID_SERVICE_RESPONSE" \
                "Модуль подписок вернул некорректный ответ."
        fi
        broray_subscriptions_api_response_json="$(cat "$broray_subscriptions_api_output_file")"
        rm -f "$broray_subscriptions_api_output_file" "$broray_subscriptions_api_error_file"
        broray_api_success "$broray_subscriptions_api_response_json"
        exit 0
    fi

    broray_subscriptions_api_error_line="$(grep 'BRORAY_ERROR:' "$broray_subscriptions_api_error_file" | tail -n 1)"
    broray_subscriptions_api_error_code="$(printf '%s' "$broray_subscriptions_api_error_line" | cut -d: -f2)"
    broray_subscriptions_api_error_message="$(printf '%s' "$broray_subscriptions_api_error_line" | cut -d: -f3-)"
    [ -n "$broray_subscriptions_api_error_code" ] || broray_subscriptions_api_error_code="SUBSCRIPTION_OPERATION_FAILED"
    [ -n "$broray_subscriptions_api_error_message" ] || broray_subscriptions_api_error_message="Операция с подпиской завершилась ошибкой."
    broray_subscriptions_api_http_status="$(broray_subscriptions_api_error_status "$broray_subscriptions_api_error_code")"
    rm -f "$broray_subscriptions_api_output_file" "$broray_subscriptions_api_error_file"
    broray_api_error \
        "$broray_subscriptions_api_http_status" \
        "$broray_subscriptions_api_error_code" \
        "$broray_subscriptions_api_error_message"
}

