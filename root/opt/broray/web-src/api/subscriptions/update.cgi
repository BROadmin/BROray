#!/opt/bin/ash
. /opt/broray/web-new/api/subscriptions/common.sh
broray_api_require_method POST
broray_api_require_session
subscription_id="$(broray_subscriptions_api_query id)"
body_file="/opt/broray/tmp/subscriptions-update-body.$$.json"
trap 'rm -f "$body_file"' EXIT INT TERM
broray_subscriptions_api_read_body > "$body_file"
broray_subscriptions_api_run \
    broray_subscription_update_settings "$subscription_id" "$body_file"
rm -f "$body_file"
