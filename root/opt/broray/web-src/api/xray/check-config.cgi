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

output_file="/tmp/broray-xray-validate-$$.json"
error_file="/tmp/broray-xray-validate-$$.err"

cleanup() {
    rm -f "$output_file" "$error_file"
}

trap cleanup EXIT HUP INT TERM

if "$BRORAY_CLI" xray validate \
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
        "XRAY_CONFIG_CHECK_FAILED" \
        "Не удалось получить результат проверки конфигурации." \
        "$details"
fi

result_success="$(
    jq -r '.success // false' "$output_file"
)"

if [ "$command_success" != true ] ||
   [ "$result_success" != true ]
then
    details="$(
        jq -r '.output // .message // empty' "$output_file"
    )"

    broray_api_error \
        "422 Unprocessable Entity" \
        "XRAY_CONFIG_INVALID" \
        "Конфигурация Xray не прошла проверку." \
        "$details"
fi

data_json="$(
    jq '
        {
            valid: (.success // false),
            output: (.output // ""),
            configPath: "/opt/broray/config/config.json",
            checkedAt: (
                now |
                strftime("%Y-%m-%dT%H:%M:%SZ")
            )
        }
    ' "$output_file"
)"

broray_api_success "$data_json"
