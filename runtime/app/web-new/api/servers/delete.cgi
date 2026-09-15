#!/opt/bin/ash
. /opt/broray/web-new/api/servers/common.sh
broray_api_require_method POST
broray_api_require_session
broray_servers_api_lock delete

body_file="/opt/broray/tmp/servers-delete-body.$$.json"
trap 'rm -f "$body_file"; command -v broray_routes_api_lock_release >/dev/null 2>&1 && broray_routes_api_lock_release' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
broray_servers_api_read_body_to_file "$body_file"
body_json="$(cat "$body_file")"
rm -f "$body_file"

server_id="$(
    broray_servers_api_body_field "$body_json" id
)"

broray_servers_api_run \
    broray_server_delete_safe \
    "$server_id"
