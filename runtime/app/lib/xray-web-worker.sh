#!/opt/bin/ash
umask 077
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_BASE="$BRORAY_ROOT"
XRAY_WEB_WORK="$1"; XRAY_WEB_MODE="$2"; XRAY_WEB_NONCE="$3"
case "$XRAY_WEB_MODE" in install|update|reinstall) ;; *) exit 64 ;; esac
case "$XRAY_WEB_WORK" in "$BRORAY_ROOT/run/xray-operations/$BRORAY_BACKGROUND_OPERATION_ID") ;; *) exit 73 ;; esac
[ -d "$XRAY_WEB_WORK" ] && [ ! -L "$XRAY_WEB_WORK" ] || exit 73
[ "$(cat "$XRAY_WEB_WORK/operation-id")" = "$BRORAY_BACKGROUND_OPERATION_ID" ] || exit 73
. "$BRORAY_ROOT/lib/xray-control.sh"
. "$BRORAY_ROOT/lib/xray-update.sh"
. "$BRORAY_ROOT/lib/xray-releases.sh"
broray_ops_accept_handoff "$XRAY_WEB_NONCE" || exit $?
BRORAY_JOB_ACTIVE=true
trap 'broray_xray_job_exit "$?"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

broray_xray_web_finish()
{
    local web_rc web_output web_result web_temporary
    web_rc="$1"
    broray_job_require_owner || return $?
    web_result="$BRORAY_ROOT/run/xray-web-operation.json"
    jq -e --arg id "$BRORAY_BACKGROUND_OPERATION_ID" '.backgroundOperationId==$id' "$web_result" >/dev/null || return 73
    web_output='{}'
    if jq -e 'type=="object"' "$XRAY_WEB_WORK/output.json" >/dev/null 2>&1; then
        web_output="$(cat "$XRAY_WEB_WORK/output.json")"
    fi
    web_temporary="$web_result.new.$$"
    jq -n --arg id "$BRORAY_BACKGROUND_OPERATION_ID" --arg mode "$XRAY_WEB_MODE" --argjson rc "$web_rc" \
      --argjson output "$web_output" --arg completedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
      '{backgroundOperationId:$id,operation:$mode,success:($rc==0 and $output.success==true),
        running:false,operationRunning:false,pid:null,workerPid:null,result:$output,
        error:(if $rc==0 then null elif $rc==130 then "Операция Xray отменена."
          else ($output.error // "Операция Xray завершилась с ошибкой. Откройте диагностику.") end),completedAt:$completedAt}' >"$web_temporary" || return 1
    chmod 600 "$web_temporary" && "${BRORAY_OPS_GUARD:-$BRORAY_ROOT/bin/broray-ops-guard}" --replace-file "$web_temporary" "$web_result" || {
        BRORAY_JOB_UNRESOLVED=true; return 75
    }
}

web_rc=0
broray_xray_install_dispatch "$XRAY_WEB_MODE" "$XRAY_WEB_WORK/request.json" >"$XRAY_WEB_WORK/output.json" || web_rc=$?
exit "$web_rc"
