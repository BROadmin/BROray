#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
BRORAY="/opt/broray/bin/broray"

. "$AUTH"

broray_api_require_method POST
broray_api_require_session

OUTPUT="/tmp/broray-web-xray-update-check-$$.json"
ERROR="/tmp/broray-web-xray-update-check-$$.err"

cleanup()
{
    rm -f "$OUTPUT" "$ERROR"
}

trap cleanup EXIT HUP INT TERM

if "$BRORAY" xray update-check \
    > "$OUTPUT" 2> "$ERROR"
then
    command_ok=true
else
    command_ok=false
fi

if ! jq -e . "$OUTPUT" >/dev/null 2>&1; then
    details="$(
        {
            cat "$ERROR" 2>/dev/null
            cat "$OUTPUT" 2>/dev/null
        } |
            tail -n 30
    )"

    broray_api_error \
        "502 Bad Gateway" \
        "XRAY_UPDATE_CHECK_FAILED" \
        "Не удалось проверить официальный релиз Xray." \
        "$details"
fi

if [ "$command_ok" != true ] ||
   [ "$(jq -r '.success // false' "$OUTPUT")" != true ]
then
    details="$(
        jq -r '.error // .message // empty' "$OUTPUT"
    )"

    broray_api_error \
        "502 Bad Gateway" \
        "XRAY_UPDATE_CHECK_FAILED" \
        "Проверка обновления Xray завершилась ошибкой." \
        "$details"
fi

broray_api_success "$(cat "$OUTPUT")"
