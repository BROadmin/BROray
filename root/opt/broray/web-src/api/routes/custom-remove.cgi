#!/opt/bin/ash

. /opt/broray/web-new/api/routes/custom-common.sh

broray_api_require_method POST
broray_api_require_session
broray_custom_routes_bundle_from_query
bundle_id="$BRORAY_CUSTOM_BUNDLE_ID"
broray_custom_routes_run "$BRORAY_CUSTOM_ROUTES_CLI" remove "$bundle_id"
