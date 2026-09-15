#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh
. /opt/broray/lib/web-request-body.sh

broray_api_require_method POST
broray_api_require_session
[ "${HTTP_X_BRORAY_REQUEST:-}" = 1 ] || {
    broray_api_error \
        "403 Forbidden" \
        "INVALID_REQUEST" \
        "Запрос отклонён."
}

body_file="/opt/broray/tmp/broray-uninstall-body-$$"
body_rc=0
broray_web_request_body_to_file "$body_file" 4096 || body_rc=$?
case "$body_rc" in
    0) ;;
    2|3) broray_api_error "400 Bad Request" "INVALID_CONTENT_LENGTH" "Тело запроса пусто или имеет некорректный размер." ;;
    4) broray_api_error "413 Payload Too Large" "REQUEST_TOO_LARGE" "Тело запроса слишком большое." ;;
    *) broray_api_error "400 Bad Request" "REQUEST_BODY_INCOMPLETE" "Тело запроса передано не полностью." ;;
esac
jq -e 'type == "object"' "$body_file" >/dev/null 2>&1 ||
    broray_api_error "400 Bad Request" "REQUEST_JSON_INVALID" "Тело запроса должно быть корректным JSON-объектом."
body="$(cat "$body_file")"
rm -f "$body_file"

mode="$(printf '%s' "$body" | jq -r '.mode // empty' 2>/dev/null)"
confirmation="$(printf '%s' "$body" | jq -r '.confirmation // empty' 2>/dev/null)"

broray_api_print_json_headers
printf '\r\n'
/opt/broray/bin/broray-system uninstall-start "$mode" "$confirmation"
