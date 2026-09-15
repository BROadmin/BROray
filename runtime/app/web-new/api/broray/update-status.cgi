#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh

broray_api_require_method GET
broray_api_require_session

. /opt/broray/web-new/api/broray/updater-api-common.sh
broray_updater_api_call '200 OK' status
