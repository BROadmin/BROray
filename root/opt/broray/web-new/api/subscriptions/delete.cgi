#!/opt/bin/ash
. /opt/broray/web-new/api/subscriptions/common.sh
broray_api_require_method POST
broray_api_require_session
subscription_id="$(broray_subscriptions_api_query id)"
broray_subscriptions_api_run broray_subscription_delete "$subscription_id"
