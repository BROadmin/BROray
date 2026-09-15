#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh
. /opt/broray/lib/web-request-body.sh

broray_api_require_method POST
broray_api_require_session
[ "${HTTP_X_BRORAY_REQUEST:-}" = 1 ] || {
    broray_api_error "403 Forbidden" "INVALID_REQUEST" "Запрос отклонён."
}

body_file="/opt/broray/tmp/broray-cleanup-plan-body-$$"
body_rc=0
broray_web_request_body_to_file "$body_file" 4096 || body_rc=$?
case "$body_rc" in
    0) ;;
    2|3) broray_api_error "400 Bad Request" "INVALID_CONTENT_LENGTH" "Тело запроса пусто или имеет некорректный размер." ;;
    4) broray_api_error "413 Payload Too Large" "REQUEST_TOO_LARGE" "Тело запроса слишком большое." ;;
    *) broray_api_error "400 Bad Request" "REQUEST_BODY_INCOMPLETE" "Тело запроса передано не полностью." ;;
esac
body="$(cat "$body_file")"
rm -f "$body_file"
printf '%s' "$body" | jq -e 'type == "object"' >/dev/null 2>&1 || broray_api_error \
    "400 Bad Request" "INVALID_JSON" "Тело запроса содержит некорректный JSON."

temp="$(printf '%s' "$body" | jq -r 'if has("temp") then .temp else true end')"
backups="$(printf '%s' "$body" | jq -r 'if has("backups") then .backups else true end')"
route_backups="$(printf '%s' "$body" | jq -r 'if has("routeBackups") then .routeBackups else true end')"
logs="$(printf '%s' "$body" | jq -r 'if has("logs") then .logs else true end')"

response="/opt/broray/tmp/broray-cleanup-plan-response-$$.json"
wrapped="$response.wrapped"
cleanup_response_files()
{
    rm -f "$body_file" "$response" "$wrapped"
}
trap cleanup_response_files 0

. /opt/broray/lib/broray-cleanup.sh
if ! broray_cleanup_plan_create "$temp" "$backups" "$route_backups" "$logs" "$response" >/dev/null; then
    case "$BRORAY_CLEANUP_ERROR_CODE" in
        CLEANUP_OPERATION_BUSY) status='409 Conflict' ;;
        CLEANUP_OPTIONS_INVALID|CLEANUP_NOTHING_SELECTED) status='400 Bad Request' ;;
        *) status='500 Internal Server Error' ;;
    esac
    [ -n "$BRORAY_CLEANUP_ERROR_CODE" ] || BRORAY_CLEANUP_ERROR_CODE=CLEANUP_PLAN_FAILED
    [ -n "$BRORAY_CLEANUP_ERROR_MESSAGE" ] || BRORAY_CLEANUP_ERROR_MESSAGE='Не удалось сформировать план очистки.'
    broray_api_error "$status" "$BRORAY_CLEANUP_ERROR_CODE" "$BRORAY_CLEANUP_ERROR_MESSAGE"
fi

[ -s "$response" ] && jq -e 'type == "object"' "$response" >/dev/null 2>&1 ||
    broray_api_error "500 Internal Server Error" "CLEANUP_EMPTY_RESPONSE" "Backend очистки сформировал пустой или некорректный ответ."

jq '{success:true,data:.,error:null}' "$response" >"$wrapped" &&
    jq -e '.success == true and ((.data | type) == "object")' "$wrapped" >/dev/null 2>&1 ||
    broray_api_error "500 Internal Server Error" "CLEANUP_RESPONSE_FAILED" "Не удалось подготовить JSON-ответ очистки."

broray_api_print_json_headers
printf '\r\n'
cat "$wrapped"
