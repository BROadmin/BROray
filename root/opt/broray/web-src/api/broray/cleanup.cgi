#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh

broray_api_require_method POST
broray_api_require_session
[ "${HTTP_X_BRORAY_REQUEST:-}" = 1 ] || {
    broray_api_error "403 Forbidden" "INVALID_REQUEST" "Запрос отклонён."
}

length="${CONTENT_LENGTH:-0}"
case "$length" in ''|*[!0-9]*) length=0 ;; esac
[ "$length" -le 4096 ] || broray_api_error \
    "413 Payload Too Large" "REQUEST_TOO_LARGE" "Тело запроса слишком большое."
body='{}'
[ "$length" -gt 0 ] && body="$(dd bs=1 count="$length" 2>/dev/null)"
token="$(printf '%s' "$body" | jq -r '.token // empty' 2>/dev/null)"
[ -n "$token" ] || broray_api_error \
    "400 Bad Request" "CLEANUP_TOKEN_REQUIRED" "Не передано подтверждение очистки."

. /opt/broray/lib/broray-cleanup.sh
result="$(broray_cleanup_execute "$token")" || {
    case "$BRORAY_CLEANUP_ERROR_CODE" in
        CLEANUP_TOKEN_INVALID|CLEANUP_PLAN_NOT_FOUND|CLEANUP_PLAN_EXPIRED|CLEANUP_PLAN_CHANGED)
            status='409 Conflict'
            ;;
        CLEANUP_OPERATION_BUSY) status='409 Conflict' ;;
        *) status='500 Internal Server Error' ;;
    esac
    broray_api_error "$status" "$BRORAY_CLEANUP_ERROR_CODE" "$BRORAY_CLEANUP_ERROR_MESSAGE"
}
broray_api_success "$result"
