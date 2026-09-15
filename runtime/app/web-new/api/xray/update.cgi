#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
COMMON="/opt/broray/lib/xray-web-operation.sh"

. "$AUTH"

broray_api_require_method POST
broray_api_require_session

XRAY_WEB_OPERATION_MODE=update
export XRAY_WEB_OPERATION_MODE
. "$COMMON"
