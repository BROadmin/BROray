#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh

BRORAY_CUSTOM_ROUTES_CLI="/opt/broray/bin/broray-routes-user"
BRORAY_CUSTOM_ROUTES_BODY_LIMIT=4194304
BRORAY_CUSTOM_BUNDLE_ID=""

broray_custom_routes_read_body_to_file()
{
    target_file="$1"
    content_length="${CONTENT_LENGTH:-0}"

    case "$content_length" in
        ''|*[!0-9]*)
            broray_api_error \
                "400 Bad Request" \
                "CONTENT_LENGTH_INVALID" \
                "Некорректный размер запроса."
            ;;
    esac

    [ "$content_length" -gt 0 ] ||
        broray_api_error \
            "400 Bad Request" \
            "REQUEST_BODY_REQUIRED" \
            "Тело запроса отсутствует."

    [ "$content_length" -le "$BRORAY_CUSTOM_ROUTES_BODY_LIMIT" ] ||
        broray_api_error \
            "413 Payload Too Large" \
            "REQUEST_TOO_LARGE" \
            "Размер запроса превышает допустимый предел."

    mkdir -p "$(dirname "$target_file")" ||
        broray_api_error \
            "500 Internal Server Error" \
            "REQUEST_STORAGE_UNAVAILABLE" \
            "Временное хранилище запроса недоступно."

    if ! dd bs=1 count="$content_length" >"$target_file" 2>/dev/null; then
        rm -f "$target_file"
        broray_api_error \
            "400 Bad Request" \
            "REQUEST_BODY_READ_FAILED" \
            "Не удалось прочитать тело запроса."
    fi

    actual_length="$(wc -c <"$target_file" | tr -d ' ')"
    [ "$actual_length" = "$content_length" ] || {
        rm -f "$target_file"
        broray_api_error \
            "400 Bad Request" \
            "REQUEST_BODY_INCOMPLETE" \
            "Тело запроса получено не полностью."
    }
}

broray_custom_routes_bundle_from_query()
{
    query="${QUERY_STRING:-}"

    case "$query" in
        bundleId=*) bundle_id="${query#bundleId=}" ;;
        *)
            broray_api_error \
                "400 Bad Request" \
                "ROUTES_BUNDLE_REQUIRED" \
                "Не указан идентификатор пользовательского набора."
            ;;
    esac

    case "$bundle_id" in
        user-*) bundle_suffix="${bundle_id#user-}" ;;
        *) bundle_suffix="" ;;
    esac

    case "$bundle_suffix" in
        ""|*[!a-z0-9_-]*)
            broray_api_error \
                "400 Bad Request" \
                "ROUTES_BUNDLE_INVALID" \
                "Некорректный идентификатор пользовательского набора."
            ;;
    esac

    [ "${#bundle_id}" -le 63 ] ||
        broray_api_error \
            "400 Bad Request" \
            "ROUTES_BUNDLE_INVALID" \
            "Некорректный идентификатор пользовательского набора."

    BRORAY_CUSTOM_BUNDLE_ID="$bundle_id"
    export BRORAY_CUSTOM_BUNDLE_ID
}

broray_custom_routes_run()
{
    output_file="/opt/broray/tmp/custom-routes-api-output.$$.json"
    error_file="/opt/broray/tmp/custom-routes-api-error.$$"

    mkdir -p /opt/broray/tmp

    if "$@" >"$output_file" 2>"$error_file"; then
        if ! jq -e 'type == "object"' "$output_file" >/dev/null 2>&1; then
            details="$(cat "$output_file" 2>/dev/null)"
            rm -f "$output_file" "$error_file"
            broray_api_error \
                "500 Internal Server Error" \
                "CUSTOM_ROUTES_RESPONSE_INVALID" \
                "Модуль пользовательских маршрутов вернул некорректный ответ." \
                "$details"
        fi

        data_json="$(jq -c . "$output_file")"
        rm -f "$output_file" "$error_file"
        broray_api_success "$data_json"
        exit 0
    fi

    first_error="$(sed -n '1p' "$error_file" 2>/dev/null)"
    details="$(sed -n '2,30p' "$error_file" 2>/dev/null)"

    case "$first_error" in
        BRORAY_ERROR:*:*)
            remainder="${first_error#BRORAY_ERROR:}"
            error_code="${remainder%%:*}"
            error_message="${remainder#*:}"
            ;;
        *)
            error_code="CUSTOM_ROUTES_OPERATION_FAILED"
            error_message="Операция с пользовательскими маршрутами завершилась ошибкой."
            if [ -n "$first_error" ]; then
                details="$first_error${details:+
$details}"
            fi
            ;;
    esac

    http_status="400 Bad Request"
    case "$error_code" in
        ROUTES_BUSY) http_status="409 Conflict" ;;
        BUNDLE_NOT_FOUND) http_status="404 Not Found" ;;
        DEPENDENCY_MISSING|MODULE_UNAVAILABLE|INDEX_INVALID|RUNTIME_PREPARE_FAILED)
            http_status="500 Internal Server Error"
            ;;
    esac

    rm -f "$output_file" "$error_file"
    broray_api_error \
        "$http_status" \
        "$error_code" \
        "$error_message" \
        "$details"
}
