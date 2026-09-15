#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh

broray_servers_api_load_request_body()
{
    command -v broray_web_request_body_to_file >/dev/null 2>&1 && return 0
    . /opt/broray/lib/web-request-body.sh
}

broray_servers_api_load_services()
{
    command -v broray_server_summary >/dev/null 2>&1 &&
        command -v broray_server_import >/dev/null 2>&1 && return 0
    . /opt/broray/lib/server-service.sh
    . /opt/broray/lib/server-import.sh
}

broray_servers_api_lock()
{
    action="$1"
    bundle="${2:-servers}"
    [ -r /opt/broray/lib/routes-api-operation.sh ] ||
        broray_api_error "500 Internal Server Error" "GLOBAL_LOCK_UNAVAILABLE" "Общий координатор операций недоступен."
    . /opt/broray/lib/routes-api-operation.sh
    lock_rc=0
    broray_routes_api_lock_acquire "servers:$action" "$bundle" || lock_rc=$?
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

broray_servers_api_read_body_to_file()
{
    body_file="$1"
    body_rc=0
    broray_servers_api_load_request_body ||
        broray_api_error "500 Internal Server Error" "REQUEST_BODY_LIBRARY_UNAVAILABLE" "Обработчик тела запроса недоступен."
    broray_web_request_body_to_file "$body_file" 65536 || body_rc=$?
    case "$body_rc" in
        0) ;;
        2) broray_api_error "411 Length Required" "CONTENT_LENGTH_REQUIRED" "Для запроса требуется корректный Content-Length." ;;
        3) broray_api_error "400 Bad Request" "REQUEST_BODY_REQUIRED" "Тело запроса отсутствует." ;;
        4) broray_api_error "413 Payload Too Large" "REQUEST_TOO_LARGE" "Тело запроса превышает 64 КБ." ;;
        5) broray_api_error "400 Bad Request" "REQUEST_BODY_READ_FAILED" "Не удалось прочитать тело запроса." ;;
        *) broray_api_error "400 Bad Request" "REQUEST_BODY_INCOMPLETE" "Тело запроса получено не полностью." ;;
    esac
    jq -e 'type == "object"' "$body_file" >/dev/null 2>&1 ||
        broray_api_error "400 Bad Request" "REQUEST_JSON_INVALID" "Тело запроса должно быть корректным JSON-объектом."
}

broray_servers_api_body_field()
{
    body_json="$1"
    field_name="$2"

    printf '%s\n' "$body_json" |
        jq -r \
            --arg field "$field_name" \
            '.[$field] // empty'
}

broray_servers_api_run()
{
    broray_servers_api_load_services ||
        broray_api_error "500 Internal Server Error" "SERVER_LIBRARY_UNAVAILABLE" "Служба серверов недоступна."
    broray_servers_api_output_file="/opt/broray/tmp/servers-api-output.$$.json"
    broray_servers_api_error_file="/opt/broray/tmp/servers-api-error.$$"

    mkdir -p /opt/broray/tmp

    # Server service functions use broray_die (exit 1). Run the requested
    # operation in a child shell so a validated backend failure cannot exit
    # the CGI before this wrapper emits a stable HTTP/application error.
    if ( "$@" ) >"$broray_servers_api_output_file" 2>"$broray_servers_api_error_file"; then
        if ! jq -e . "$broray_servers_api_output_file" >/dev/null 2>&1; then
            jq -n \
                --rawfile output "$broray_servers_api_output_file" '{
                    message: $output
                }' > "$broray_servers_api_output_file.json"

            mv "$broray_servers_api_output_file.json" "$broray_servers_api_output_file"
        fi

        broray_api_success "$(
            cat "$broray_servers_api_output_file"
        )"

        rm -f "$broray_servers_api_output_file" "$broray_servers_api_error_file"
        exit 0
    fi

    broray_servers_api_error_message="$(
        cat "$broray_servers_api_error_file"
    )"

    [ -n "$broray_servers_api_error_message" ] ||
        broray_servers_api_error_message="$(
            cat "$broray_servers_api_output_file"
        )"

    rm -f "$broray_servers_api_output_file" "$broray_servers_api_error_file"

    broray_api_error \
        "400 Bad Request" \
        "SERVER_OPERATION_FAILED" \
        "Операция с сервером завершилась ошибкой." \
        "$broray_servers_api_error_message"
}
