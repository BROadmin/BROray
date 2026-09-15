#!/opt/bin/ash
. /opt/broray/web-new/api/servers/common.sh
broray_api_require_method GET
broray_api_require_session

# Plain list reads are intentionally side-effect-free and fast. Connection and
# Keenetic status are read from their timestamped status files; an explicit
# status action owns active router refresh. Never probe the router while merely
# opening or refreshing the saved-server list.

broray_servers_api_run broray_server_summary
