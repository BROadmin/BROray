#!/opt/bin/ash
. /opt/broray/web-new/api/servers/common.sh
broray_api_require_method POST
broray_api_require_session

body_json="$(
    broray_servers_api_read_body
)"

server_id="$(
    broray_servers_api_body_field "$body_json" id
)"

broray_servers_api_run \
    broray_server_delete_safe \
    "$server_id"
