#!/opt/bin/ash

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
BRORAY_CLI="/opt/broray/bin/broray"

. "$AUTH_COMMON"

broray_api_require_method POST
broray_api_require_session

if [ ! -x "$BRORAY_CLI" ]; then
    broray_api_error \
        "500 Internal Server Error" \
        "BRORAY_CLI_UNAVAILABLE" \
        "CLI BROray недоступен."
fi

output_file="/tmp/broray-xray-stop-$$.json"
error_file="/tmp/broray-xray-stop-$$.err"

cleanup() {
    rm -f "$output_file" "$error_file"
}

trap cleanup EXIT HUP INT TERM

if "$BRORAY_CLI" xray stop \
    > "$output_file" 2> "$error_file"
then
    command_success=true
else
    command_success=false
fi

if ! jq -e . "$output_file" >/dev/null 2>&1; then
    details="$(
        {
            cat "$error_file" 2>/dev/null
            cat "$output_file" 2>/dev/null
        } |
            tail -n 30
    )"

    broray_api_error \
        "500 Internal Server Error" \
        "XRAY_STOP_FAILED" \
        "Не удалось остановить Xray." \
        "$details"
fi

result_success="$(
    jq -r '.success // false' "$output_file"
)"

if [ "$command_success" != true ] ||
   [ "$result_success" != true ]
then
    details="$(
        {
            jq -r '.message // .output // empty' "$output_file"
            cat "$error_file" 2>/dev/null
        } |
            tail -n 30
    )"

    broray_api_error \
        "500 Internal Server Error" \
        "XRAY_STOP_FAILED" \
        "Xray не был остановлен." \
        "$details"
fi

data_json="$(
    jq '
        . + {
            completedAt: (
                now |
                strftime("%Y-%m-%dT%H:%M:%SZ")
            )
        }
    ' "$output_file"
)"

broray_api_success "$data_json"
