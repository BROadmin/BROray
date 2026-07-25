#!/opt/bin/ash
. /opt/broray/web-new/api/servers/common.sh
broray_api_require_method POST
broray_api_require_session

body_json="$(
    broray_servers_api_read_body
)"

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
