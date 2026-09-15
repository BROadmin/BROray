#!/opt/bin/ash
. /opt/broray/web-new/api/servers/common.sh
broray_api_require_method GET
broray_api_require_session

server_id="$(
    printf '%s\n' "${QUERY_STRING:-}" |
        sed -n 's/^.*id=\([^&]*\).*$/\1/p'
)"

server_id="$(
    printf '%s' "$server_id" |
        sed 's/%2D/-/g; s/%2E/./g; s/%5F/_/g'
)"

broray_servers_api_run \
    broray_server_details \
    "$server_id"
