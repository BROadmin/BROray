#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh
. /opt/broray/lib/server-service.sh
. /opt/broray/lib/server-import.sh

broray_servers_api_read_body()
{
    content_length="${CONTENT_LENGTH:-0}"

    case "$content_length" in
        ''|*[!0-9]*)
            content_length=0
            ;;
    esac

    if [ "$content_length" -gt 0 ]; then
        dd bs=1 count="$content_length" 2>/dev/null
    else
        cat
    fi
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
    output_file="/opt/broray/tmp/servers-api-output.$$.json"
    error_file="/opt/broray/tmp/servers-api-error.$$"

    mkdir -p /opt/broray/tmp

    if "$@" >"$output_file" 2>"$error_file"; then
        if ! jq -e . "$output_file" >/dev/null 2>&1; then
            jq -n \
                --rawfile output "$output_file" '{
                    message: $output
                }' > "$output_file.json"

            mv "$output_file.json" "$output_file"
        fi

        broray_api_success "$(
            cat "$output_file"
        )"

        rm -f "$output_file" "$error_file"
        exit 0
    fi

    error_message="$(
        cat "$error_file"
    )"

    [ -n "$error_message" ] ||
        error_message="$(
            cat "$output_file"
        )"

    rm -f "$output_file" "$error_file"

    broray_api_error \
        "400 Bad Request" \
        "SERVER_OPERATION_FAILED" \
        "Операция с сервером завершилась ошибкой." \
        "$error_message"
}
