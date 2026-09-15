#!/opt/bin/ash
. "${BRORAY_ROOT:-/opt/broray}/web-new/api/servers/common.sh"
broray_api_require_method POST
broray_api_require_session
broray_servers_api_lock quality-batch-complete
broray_servers_api_run broray_server_publish_snapshot
