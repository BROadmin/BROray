#!/opt/bin/ash
# One protected owner around the existing route command. Business algorithms
# stay in the original libraries. An inherited token alone is never admission.
. "${BRORAY_ROOT:-/opt/broray}/lib/operation-client.sh"

broray_route_worker_check()
{
    local route_pid route_rest
    [ "${BRORAY_OPS_ROUTE_SUPERVISED:-}" = ptrace/1 ] &&
      [ "${BRORAY_OPS_SUPERVISED:-}" = ptrace/1 ] &&
      [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] &&
      [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] || return 73
    IFS=' ' read -r route_pid route_rest </proc/self/stat || return 73
    broray_ops_call route-worker-check "$BRORAY_BACKGROUND_OPERATION_ID" \
      "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$route_pid"
}

broray_route_cli_worker_check()
{
    local route_context
    route_context="$(broray_route_worker_check)" || return 73
    printf '%s\n' "$route_context" | jq -e --arg action "$1" --arg bundle "$2" '
      .bundleId==$bundle and
      (if .action=="resume" then ["resume","export","delete","build-export"]
       elif .action=="preflight:resume" or .action=="preflight:export" then ["plan","build-export"]
       elif .action=="export" then ["export","build-export"]
       elif .action=="plan" or .action=="verify" then ["plan","build-export"]
       elif .action=="preflight:check" then ["preflight"]
       elif .action=="custom:remove" then ["delete"]
       else [.action] end | index($action)!=null)' >/dev/null || return 73
}

broray_route_user_cli_enter()
{
    local route_action route_bundle route_context route_rc
    [ "$#" = 2 ] || return 64
    route_action="custom:$1"; route_bundle="$2"
    case "$1" in
      preview|commit) route_bundle='' ;;
      validate|remove)
        case "$route_bundle" in user-*) ;; *) return 64 ;; esac
        case "$route_bundle" in *[!a-z0-9_-]*) return 64 ;; esac
        [ "${#route_bundle}" -le 63 ] || return 64 ;;
      *) return 64 ;;
    esac
    if [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ]; then
        route_context="$(broray_route_worker_check)" || return 73
        printf '%s\n' "$route_context" | jq -e --arg action "$route_action" --arg bundle "$route_bundle" '
          .bundleId==$bundle and (.action==$action or (.action=="custom:remove-finalize" and $action=="custom:remove"))' >/dev/null || return 73
        return 0
    fi
    route_rc=0
    broray_route_job_run "$route_action" "$route_bundle" "${BRORAY_OPS_ASH:-/opt/bin/ash}" \
      "${BRORAY_ROOT:-/opt/broray}/bin/broray-routes-user" "$@" || route_rc=$?
    exit "$route_rc"
}

broray_route_job_run()
{
    local route_scope route_bundle route_app route_ash route_supervisor route_state route_rc route_attempt route_drained
    [ "$#" -ge 3 ] || return 64
    BRORAY_ROUTE_JOB_FINISHED=false
    route_scope="$1"; route_bundle="$2"; shift 2
    [ -z "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 73
    broray_ops_begin routes "$route_scope" "$route_bundle" USER protected || return $?
    route_app="${BRORAY_ROOT:-/opt/broray}"
    route_ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    route_supervisor="${BRORAY_OPS_SUPERVISOR:-$route_app/bin/broray-ops-supervisor}"
    route_state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
    route_rc=0
    "$route_supervisor" --protected-route "$route_ash" "$route_app/lib/operation-supervisor-control.sh" \
      "$route_state/operations/$BRORAY_BACKGROUND_OPERATION_ID/cancel.json" 3600 0 2 -- "$@" || route_rc=$?
    route_attempt=0; route_drained=false
    while [ "$route_attempt" -lt 5 ]; do
        route_attempt=$((route_attempt+1))
        if broray_ops_call helpers-drain "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" >/dev/null; then
            route_drained=true; break
        fi
        [ "$route_attempt" = 5 ] || sleep 1
    done
    [ "$route_drained" = true ] || return 75
    if [ "$route_rc" = 0 ]; then broray_ops_finish completed || return 75
    else broray_ops_finish failed OPERATION_FAILED || return 75; fi
    BRORAY_ROUTE_JOB_FINISHED=true
    return "$route_rc"
}

broray_route_cli_enter()
{
    local route_action route_bundle route_rc route_ash route_self
    [ "$#" = 2 ] || return 64
    route_action="$1"; route_bundle="$2"
    case "$route_bundle" in ''|*[!a-z0-9_-]*) return 64 ;; esac
    [ "${#route_bundle}" -le 63 ] || return 64
    if [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ]; then
        broray_route_cli_worker_check "$route_action" "$route_bundle" || return 73
        return 0
    fi
    route_ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    route_self="${BRORAY_ROOT:-/opt/broray}/bin/broray-routes"
    route_rc=0
    broray_route_job_run "$route_action" "$route_bundle" "$route_ash" "$route_self" "$@" || route_rc=$?
    exit "$route_rc"
}
