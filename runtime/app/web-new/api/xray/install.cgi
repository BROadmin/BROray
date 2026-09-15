#!/opt/bin/ash
. /opt/broray/web-new/api/auth-common.sh
broray_api_require_method POST
broray_api_require_session
XRAY_WEB_OPERATION_MODE=install
export XRAY_WEB_OPERATION_MODE
. /opt/broray/lib/xray-web-operation.sh
