#!/opt/bin/ash
. /opt/broray/web-new/api/subscriptions/common.sh
broray_api_require_method POST
broray_api_require_session
body_file="/opt/broray/tmp/subscriptions-create-body.$$.json"
trap 'rm -f "$body_file"' EXIT INT TERM
broray_subscriptions_api_read_body > "$body_file"
broray_subscriptions_api_run broray_subscription_create "$body_file"
rm -f "$body_file"
