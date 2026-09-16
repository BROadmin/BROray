#!/opt/bin/ash
# Replay the authenticated CGI inside a protected route job. Only its parent
# publishes the buffered HTTP response, after every traced process is gone.
. "${BRORAY_ROOT:-/opt/broray}/lib/route-job.sh"

broray_route_api_enter()
{
    local route_action route_bundle route_app route_ash route_cgi route_body route_reply route_context route_rc
    route_action="$1"; route_bundle="$2"
    route_app="${BRORAY_ROOT:-/opt/broray}"
    route_ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    route_cgi="$0"; route_body=/dev/null
    # Keep command selection tied to the real entry point, never query input.
    case "$route_cgi" in
      "$route_app/web-new/api/routes/check.cgi"|"$route_app/web-new/api/routes/download.cgi"|\
      "$route_app/web-new/api/routes/verify.cgi"|"$route_app/web-new/api/routes/plan.cgi"|\
      "$route_app/web-new/api/routes/preflight.cgi"|"$route_app/web-new/api/routes/export.cgi"|\
      "$route_app/web-new/api/routes/delete.cgi"|"$route_app/web-new/api/routes/resume.cgi"|\
      "$route_app/web-new/api/routes/custom-list.cgi"|"$route_app/web-new/api/routes/custom-validate.cgi"|\
      "$route_app/web-new/api/routes/custom-remove.cgi"|"$route_app/web-new/api/routes/custom-preview.cgi"|\
      "$route_app/web-new/api/routes/custom-commit.cgi") ;;
      *) return 73 ;;
    esac
    case "$route_action" in
      custom:preview|custom:commit)
        route_body="$route_bundle"; route_bundle=''
        [ -f "$route_body" ] && [ ! -L "$route_body" ] || return 74 ;;
    esac
    if [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ]; then
        route_context="$(broray_route_worker_check)" || return 73
        printf '%s\n' "$route_context" | jq -e --arg action "$route_action" --arg bundle "$route_bundle" \
          '.action==$action and .bundleId==$bundle' >/dev/null || return 73
        return 0
    fi
    [ -d "$route_app/tmp" ] && [ ! -L "$route_app/tmp" ] || return 74
    route_reply="$(mktemp "$route_app/tmp/route-api-reply.XXXXXXXX")" || return 74
    route_rc=0
    broray_route_job_run "$route_action" "$route_bundle" "$route_ash" \
      "$route_app/lib/route-api-worker.sh" "$route_cgi" "$route_reply" <"$route_body" || route_rc=$?
    if [ "${BRORAY_ROUTE_JOB_FINISHED:-false}" = true ] &&
       { [ "$route_rc" = 0 ] || [ "$route_rc" = 1 ]; } && [ -s "$route_reply" ]; then
        cat "$route_reply"
        rm -f "$route_reply"
        exit 0
    fi
    rm -f "$route_reply"
    [ "$route_rc" != 0 ] || route_rc=74
    return "$route_rc"
}
