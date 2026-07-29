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
printf '%s' "$body" | jq -e 'type == "object"' >/dev/null 2>&1 || broray_api_error \
    "400 Bad Request" "INVALID_JSON" "Тело запроса содержит некорректный JSON."

temp="$(printf '%s' "$body" | jq -r 'if has("temp") then .temp else true end')"
backups="$(printf '%s' "$body" | jq -r 'if has("backups") then .backups else true end')"
route_backups="$(printf '%s' "$body" | jq -r 'if has("routeBackups") then .routeBackups else true end')"
logs="$(printf '%s' "$body" | jq -r 'if has("logs") then .logs else true end')"

. /opt/broray/lib/broray-cleanup.sh
result="$(broray_cleanup_plan_create "$temp" "$backups" "$route_backups" "$logs")" || {
    case "$BRORAY_CLEANUP_ERROR_CODE" in
        CLEANUP_OPERATION_BUSY) status='409 Conflict' ;;
        CLEANUP_OPTIONS_INVALID|CLEANUP_NOTHING_SELECTED) status='400 Bad Request' ;;
        *) status='500 Internal Server Error' ;;
    esac
    broray_api_error "$status" "$BRORAY_CLEANUP_ERROR_CODE" "$BRORAY_CLEANUP_ERROR_MESSAGE"
}
broray_api_success "$result"
