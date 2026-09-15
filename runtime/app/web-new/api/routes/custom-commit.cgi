#!/opt/bin/ash

. /opt/broray/web-new/api/routes/custom-common.sh

broray_api_require_method POST
broray_api_require_session

request_file="/opt/broray/tmp/custom-routes-commit.$$.json"
trap 'rm -f "$request_file"; command -v broray_routes_api_lock_release >/dev/null 2>&1 && broray_routes_api_lock_release' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

broray_custom_routes_read_body_to_file "$request_file"
jq -e 'type == "object"' "$request_file" >/dev/null 2>&1 ||
    broray_api_error \
        "400 Bad Request" \
        "REQUEST_JSON_INVALID" \
        "Запрос не является корректным JSON."

broray_custom_routes_run "$BRORAY_CUSTOM_ROUTES_CLI" commit "$request_file"
