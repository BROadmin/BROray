#!/opt/bin/ash
# Admission belongs to this executor, including when its CGI parent disappears.
umask 077
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
PATH="$BRORAY_ROOT/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH BRORAY_ROOT
[ "$#" -ge 2 ] || exit 64
worker_action="$1"; worker_function="$2"; shift 2
case "$worker_action:$worker_function" in
  check:broray_server_check|import:broray_server_import|activate:broray_server_activate|\
  deactivate:broray_server_deactivate|delete:broray_server_delete_safe|\
  quality-batch-complete:broray_server_publish_snapshot) ;;
  *) exit 64 ;;
esac
. "$BRORAY_ROOT/lib/server-service.sh"
. "$BRORAY_ROOT/lib/server-import.sh"
. "$BRORAY_ROOT/lib/server-import-job.sh"
worker_source=USER
if [ "$worker_action" = check ]; then
    case "${2:-manual}" in scheduled) worker_source=SERVER_CHECK_AUTO ;; auto-switch) worker_source=AUTO_SWITCH ;; esac
fi
worker_rc=0
broray_job_begin routes "servers:$worker_action" servers "$worker_source" cooperative || worker_rc=$?
case "$worker_rc" in 0) ;; 2) exit 76 ;; *) exit "$worker_rc" ;; esac
worker_exit()
{
    local worker_exit_rc
    worker_exit_rc="$1"
    trap - EXIT
    broray_job_exit "$worker_exit_rc" || worker_exit_rc=75
    exit "$worker_exit_rc"
}
trap 'worker_exit "$?"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
if [ "$worker_action" = quality-batch-complete ]; then
    broray_job_checkpoint committing || exit $?
    # Until domain rollback is confirmed, any interrupted/failed mutation
    # must remain fenced. Persistent Xray is never put under helper tracing.
    BRORAY_JOB_UNRESOLVED=true
fi
if [ "$worker_action" = import ]; then worker_function=broray_server_import_job; fi
"$worker_function" "$@" || exit $?
BRORAY_JOB_UNRESOLVED=false
