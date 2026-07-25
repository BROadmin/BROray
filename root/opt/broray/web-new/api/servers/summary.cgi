#!/opt/bin/ash
. /opt/broray/web-new/api/servers/common.sh
broray_api_require_method GET
broray_api_require_session
broray_servers_api_run broray_server_summary
