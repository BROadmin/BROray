#!/opt/bin/ash
. /opt/broray/web-new/api/subscriptions/common.sh
broray_api_require_method GET
broray_api_require_session
broray_subscriptions_api_run broray_subscription_list
