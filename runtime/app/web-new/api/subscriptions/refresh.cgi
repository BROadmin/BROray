#!/opt/bin/ash
if [ "${HTTP_X_BRORAY_QUEUE:-}" = 1 ]; then
    . /opt/broray/web-new/api/operations/common.sh
    broray_operations_api POST subscription-refresh
    exit
fi
. /opt/broray/web-new/api/subscriptions/common.sh
broray_api_require_method POST
broray_api_require_session
broray_subscriptions_api_lock refresh
subscription_id="$(broray_subscriptions_api_query id)"
broray_subscriptions_api_run \
    broray_subscription_launch_update "$subscription_id" manual
