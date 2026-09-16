#!/opt/bin/ash
umask 077
[ "$#" = 3 ] || exit 64
web_id="$1"; web_trigger="$2"; web_nonce="$3"
case "$web_trigger" in initial|manual) ;; *) exit 64 ;; esac
. "${BRORAY_ROOT:-/opt/broray}/lib/subscription-service.sh"
broray_subscription_validate_id "$web_id" || exit 64
broray_ops_accept_handoff "$web_nonce" || exit $?
BRORAY_JOB_ACTIVE=true
trap 'broray_job_exit "$?"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
broray_ops_cancel_requested && exit 130
broray_subscription_update "$web_id" "$web_trigger"
