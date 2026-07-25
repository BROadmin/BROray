#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh

broray_api_require_method POST
broray_api_require_session
[ "${HTTP_X_BRORAY_REQUEST:-}" = 1 ] || {
    broray_api_error \
        "403 Forbidden" \
        "INVALID_REQUEST" \
        "Запрос отклонён."
}

length="${CONTENT_LENGTH:-0}"
case "$length" in
    ''|*[!0-9]*)
        length=0
        ;;
esac

if [ "$length" -gt 4096 ]; then
    broray_api_error \
        "413 Payload Too Large" \
        "REQUEST_TOO_LARGE" \
        "Тело запроса слишком большое."
fi

body=""
[ "$length" -gt 0 ] && body="$(dd bs=1 count="$length" 2>/dev/null)"

mode="$(printf '%s' "$body" | jq -r '.mode // empty' 2>/dev/null)"
confirmation="$(printf '%s' "$body" | jq -r '.confirmation // empty' 2>/dev/null)"

broray_api_print_json_headers
printf '\r\n'
/opt/broray/bin/broray-system uninstall-start "$mode" "$confirmation"
