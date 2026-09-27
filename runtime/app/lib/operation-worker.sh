#!/opt/bin/ash
# Finite queue worker. Code/handler are fixed; queue text is never a command.
set -u
umask 077
WORK_CODE="${BRORAY_OPS_CODE_ROOT:-${BRORAY_ROOT:-/opt/broray}}"
. "$WORK_CODE/lib/operation-job.sh"
[ "$#" = 2 ] && [ "$1" = --request ] || exit 64
WORK_REQUEST="$2"
broray_ops_request_id_valid "$WORK_REQUEST" || exit 64
broray_job_claim_step "$WORK_REQUEST" || exit $?
trap 'broray_job_exit "$?"' EXIT
if [ "${BRORAY_QUEUE_DISPATCH_NEXT:-}" = 1 ]; then
    # Claim + ack have occupied this resource. Fill another free resource now,
    # instead of waiting for this potentially slow helper to finish. Selection
    # and the child's claim still enforce priority, pause and writer exclusion.
    # A concurrent dispatcher may lose its claim; it never runs the handler.
    broray_job_dispatch_step continue || exit $?
fi
WORK_DIRECTORY="$(broray_ops_operation_directory)" || exit 74
WORK_ACTION="$(jq -er .operation "$WORK_DIRECTORY/state.json")" || exit 74
WORK_STAGE="$(jq -er .queueStep.stage "$WORK_DIRECTORY/state.json")" || exit 74
case "$WORK_ACTION:$WORK_STAGE" in
  servers:active-health:checking)
    . "$WORK_CODE/lib/active-proxy-health.sh"
    broray_auto_health_step "$WORK_REQUEST" "$WORK_STAGE" ;;
  servers:quality:probe)
    . "$WORK_CODE/lib/server-service.sh"
    broray_quality_step "$WORK_REQUEST" "$WORK_STAGE" ;;
  servers:failover:verify|servers:failover:probe|servers:failover:activate)
    . "$WORK_CODE/lib/active-proxy-health.sh"
    broray_auto_failover_step "$WORK_REQUEST" "$WORK_STAGE" ;;
  subscriptions:refresh:fetch|subscriptions:refresh:parse|subscriptions:refresh:apply)
    . "$WORK_CODE/lib/subscription-job.sh"
    broray_subscription_step "$WORK_REQUEST" "$WORK_STAGE" ;;
  dot:auto-check:probe)
    . "$WORK_CODE/lib/dot-auto.sh"
    broray_dot_auto_step "$WORK_REQUEST" "$WORK_STAGE" ;;
  *) exit 64 ;;
esac
WORK_RC=$?
broray_job_exit "$WORK_RC" || exit $?
trap - EXIT
if [ "${BRORAY_QUEUE_DISPATCH_NEXT:-}" = 1 ]; then
    # No waiting worker/daemon. Only a confirmed finish or yield can ask the
    # existing coordinator for one next finite stage; all priorities and
    # resource ownership are checked again. Unconfirmed completion exits above.
    unset BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN BRORAY_BACKGROUND_LAUNCH_NONCE
    broray_job_dispatch_step continue || exit $?
fi
exit "$WORK_RC"
