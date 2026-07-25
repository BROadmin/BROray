#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh

broray_api_require_method GET
broray_api_require_session

broray_api_print_json_headers
printf '\r\n'
/opt/broray/bin/broray-system info
