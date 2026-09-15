#!/opt/bin/ash

AUTH_COMMON="/opt/broray/web-new/api/auth-common.sh"
BRORAY_CLI="/opt/broray/bin/broray"

. "$AUTH_COMMON"

broray_api_require_method POST
broray_api_require_session

. /opt/broray/lib/routes-api-operation.sh
lock_rc=0
broray_routes_api_lock_acquire 'xray:restart' xray || lock_rc=$?
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

if [ ! -x "$BRORAY_CLI" ]; then
    broray_api_error \
        "500 Internal Server Error" \
        "BRORAY_CLI_UNAVAILABLE" \
        "CLI BROray недоступен."
fi

output_file="/opt/broray/tmp/broray-xray-restart-$$.json"
error_file="/opt/broray/tmp/broray-xray-restart-$$.err"

cleanup() {
    rm -f "$output_file" "$error_file"
    broray_routes_api_lock_release
}

trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if "$BRORAY_CLI" xray restart \
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
        "XRAY_RESTART_FAILED" \
        "Не удалось перезапустить Xray." \
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
        "XRAY_RESTART_FAILED" \
        "Xray не был перезапущен." \
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
