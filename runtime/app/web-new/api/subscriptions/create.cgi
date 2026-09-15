#!/opt/bin/ash
. /opt/broray/web-new/api/subscriptions/common.sh
broray_api_require_method POST
broray_api_require_session
broray_subscriptions_api_lock create
body_file="/opt/broray/tmp/subscriptions-create-body.$$.json"
trap 'rm -f "$body_file"; broray_job_finish failed' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
broray_subscriptions_api_read_body_to_file "$body_file"
broray_subscriptions_api_run broray_subscription_create "$body_file"
rm -f "$body_file"
