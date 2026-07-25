#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh
. /opt/broray/lib/subscription-service.sh

broray_subscriptions_api_read_body()
{
    content_length="${CONTENT_LENGTH:-0}"
    case "$content_length" in
        ''|*[!0-9]*) content_length=0 ;;
    esac
    if [ "$content_length" -gt 65536 ]; then
        broray_api_error \
            "413 Payload Too Large" \
            "REQUEST_TOO_LARGE" \
            "Тело запроса слишком большое."
    fi
    if [ "$content_length" -gt 0 ]; then
        dd bs=1 count="$content_length" 2>/dev/null
    else
        cat
    fi
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
    error_code="$1"
    case "$error_code" in
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
    output_file="/opt/broray/tmp/subscriptions-api-output.$$.json"
    error_file="/opt/broray/tmp/subscriptions-api-error.$$"
    mkdir -p /opt/broray/tmp
    if "$@" > "$output_file" 2> "$error_file"; then
        if ! jq -e . "$output_file" >/dev/null 2>&1; then
            rm -f "$output_file" "$error_file"
            broray_api_error \
                "500 Internal Server Error" \
                "INVALID_SERVICE_RESPONSE" \
                "Модуль подписок вернул некорректный ответ."
        fi
        response_json="$(cat "$output_file")"
        rm -f "$output_file" "$error_file"
        broray_api_success "$response_json"
        exit 0
    fi

    error_line="$(grep 'BRORAY_ERROR:' "$error_file" | tail -n 1)"
    error_code="$(printf '%s' "$error_line" | cut -d: -f2)"
    error_message="$(printf '%s' "$error_line" | cut -d: -f3-)"
    [ -n "$error_code" ] || error_code="SUBSCRIPTION_OPERATION_FAILED"
    [ -n "$error_message" ] || error_message="Операция с подпиской завершилась ошибкой."
    http_status="$(broray_subscriptions_api_error_status "$error_code")"
    rm -f "$output_file" "$error_file"
    broray_api_error \
        "$http_status" \
        "$error_code" \
        "$error_message"
}
