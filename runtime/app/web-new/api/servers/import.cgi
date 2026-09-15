#!/opt/bin/ash
. /opt/broray/web-new/api/servers/common.sh
broray_api_require_method POST
broray_api_require_session
broray_servers_api_lock import

body_file="/opt/broray/tmp/servers-import-body.$$.json"
trap 'rm -f "$body_file"; command -v broray_routes_api_lock_release >/dev/null 2>&1 && broray_routes_api_lock_release' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
broray_servers_api_read_body_to_file "$body_file"
body_json="$(cat "$body_file")"
rm -f "$body_file"

uri="$(
    broray_servers_api_body_field "$body_json" uri
)"

[ -n "$uri" ] ||
    broray_api_error \
        "400 Bad Request" \
        "URI_REQUIRED" \
        "Не указана конфигурация сервера."

broray_servers_api_run \
    broray_server_import \
    "$uri" \
    manual \
    "" \
    0
