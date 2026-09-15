#!/opt/bin/ash
. /opt/broray/web-new/api/subscriptions/common.sh
broray_api_require_method POST
broray_api_require_session
broray_subscriptions_api_lock update
subscription_id="$(broray_subscriptions_api_query id)"
body_file="/opt/broray/tmp/subscriptions-update-body.$$.json"
trap 'rm -f "$body_file"; command -v broray_routes_api_lock_release >/dev/null 2>&1 && broray_routes_api_lock_release' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
broray_subscriptions_api_read_body_to_file "$body_file"
broray_subscriptions_api_run \
    broray_subscription_update_settings "$subscription_id" "$body_file"
rm -f "$body_file"

