#!/opt/bin/ash

. /opt/broray/web-new/api/routes/custom-common.sh

broray_api_require_method GET
broray_api_require_session
broray_custom_routes_run "$BRORAY_CUSTOM_ROUTES_CLI" list
