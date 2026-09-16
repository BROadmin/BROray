#!/opt/bin/ash
# Caller owns a cooperative operation. Never start, stop or reconfigure Xray here.
broray_active_proxy_context() (
    local config binary pid start digest active
    config="$(broray_xray_config_path)" || return 1
    binary="$(broray_xray_binary_path)" || return 1
    [ -f "$config" ] && [ ! -L "$config" ] || return 1
    active="$(cat "$BRORAY_ACTIVE_SERVER_FILE" 2>/dev/null)" || return 1
    [ -n "$active" ] && [ "$active" = "$1" ] || return 1
    pid="$(broray_xray_pid)" || return 1
    BRORAY_XRAY_BINARY="$binary"; BRORAY_XRAY_CONFIG="$config"
    start="$(broray_xray_runtime_identity "$pid")" || return 1
    digest="$(sha256sum "$config")" || return 1
    printf '%s\n%s\n%s\n%s\n' "$active" "$pid:$start" "$config" "$digest" | sha256sum | awk '{print $1}'
)

broray_active_proxy_measure()
{
    local active_before active_after active_dir active_rc active_config active_id
    active_id="$1"
    ACTIVE_PROXY_HEALTH=unknown; ACTIVE_PROXY_CONTEXT=''
    ACTIVE_PROXY_RESULT='{"method":"current-socks-https","status":"unknown","errorCode":"ACTIVE_CONTEXT_UNAVAILABLE"}'
    broray_job_checkpoint checking || return $?
    active_before="$(broray_active_proxy_context "$active_id")" || return 0
    active_config="$(broray_xray_config_path)" || return 0
    active_dir="$(mktemp -d "$BRORAY_BASE/tmp/active-health-$BRORAY_BACKGROUND_OPERATION_ID-XXXXXX")" || return 1
    chmod 700 "$active_dir" || return 1
    active_rc=0
    broray_ops_run_helper 25 -- "${BRORAY_OPS_ASH:-/opt/bin/ash}" \
      "$BRORAY_BASE/lib/active-proxy-probe.sh" "$active_config" "$active_id" \
      >"$active_dir/result.json" 2>"$active_dir/error" || active_rc=$?
    if [ "$active_rc" = 75 ]; then BRORAY_JOB_UNRESOLVED=true; return 75; fi
    case "$active_rc" in 0|1|2) ;; *) rm -rf "$active_dir"; return "$active_rc" ;; esac
    active_after="$(broray_active_proxy_context "$active_id")" || active_after=''
    if [ "$active_after" = "$active_before" ] &&
      jq -e --arg id "$active_id" --argjson rc "$active_rc" '
        type=="object" and .method=="current-socks-https" and .serverId==$id and
        .status==(if $rc==0 then "healthy" elif $rc==1 then "unhealthy" else "unknown" end) and
        (.checkedAt|type)=="string" and (.checkedEpoch|type)=="number"
      ' "$active_dir/result.json" >/dev/null 2>&1; then
        ACTIVE_PROXY_RESULT="$(cat "$active_dir/result.json")"
        ACTIVE_PROXY_CONTEXT="$active_before"
        case "$active_rc" in 0) ACTIVE_PROXY_HEALTH=true ;; 1) ACTIVE_PROXY_HEALTH=false ;; esac
    else
        ACTIVE_PROXY_RESULT='{"method":"current-socks-https","status":"unknown","errorCode":"ACTIVE_CONTEXT_CHANGED_OR_INCOMPLETE"}'
    fi
    rm -rf "$active_dir" || return 1
}
