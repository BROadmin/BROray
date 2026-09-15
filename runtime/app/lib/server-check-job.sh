#!/opt/bin/ash
# Only the admitted executor publishes a completed measurement.
. "$BRORAY_BASE/lib/operation-job.sh"

broray_server_check()
{
    local job_server job_source job_dir job_rc job_quality job_target job_candidate
    broray_job_require_owner || return $?
    job_server="$1"; job_source="${2:-manual}"
    case "$job_source" in manual|auto-switch|scheduled) ;; *) return 64 ;; esac
    broray_server_validate_id "$job_server"
    broray_server_exists "$job_server" || return 1
    broray_job_checkpoint checking || return $?
    mkdir -p "$BRORAY_BASE/tmp" "$BRORAY_QUALITY_DIR" || return 1
    job_dir="$(mktemp -d "$BRORAY_BASE/tmp/server-check-$BRORAY_BACKGROUND_OPERATION_ID-XXXXXX")" || return 1
    chmod 700 "$job_dir" || return 1
    printf '%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >"$job_dir/operation-id" || return 1
    mkdir -m 700 "$job_dir/quality" || return 1
    job_target="$BRORAY_QUALITY_DIR/$job_server.json"
    job_quality="$job_dir/quality/$job_server.json"
    if [ -e "$job_target" ] || [ -L "$job_target" ]; then
        [ -f "$job_target" ] && [ ! -L "$job_target" ] || return 1
        cp "$job_target" "$job_quality" || return 1
    fi
    job_rc=0
    broray_ops_run_helper 120 -- "${BRORAY_OPS_ASH:-/opt/bin/ash}" \
      "$BRORAY_BASE/lib/server-check-prepare.sh" "$job_dir" "$job_server" "$job_source" || job_rc=$?
    if [ "$job_rc" = 75 ]; then BRORAY_JOB_UNRESOLVED=true; return 75; fi
    case "$job_rc" in
      0|1) ;;
      *) rm -rf "$job_dir"; return "$job_rc" ;;
    esac
    # A complete negative measurement has exit 1. A crash, timeout or
    # cancellation without a complete result never replaces quality.
    if [ ! -f "$job_dir/result.json" ] || [ -L "$job_dir/result.json" ] ||
      [ ! -f "$job_quality" ] || [ -L "$job_quality" ] ||
      ! jq -e --arg id "$job_server" --arg source "$job_source" --argjson rc "$job_rc" \
        --slurpfile quality "$job_quality" 'type=="object" and .serverId==$id and
        .success==($rc==0) and .quality==$quality[0] and
        (.quality|type)=="object" and .quality.measurementSource==$source and
        (.quality.status==(if $rc==0 then "available" else "unavailable" end)) and
        all([.quality.successfulChecks,.quality.failedChecks,.quality.disconnects,.quality.durationMs][];
          type=="number" and .>=0 and floor==.)' "$job_dir/result.json" >/dev/null; then
        rm -rf "$job_dir"; return 1
    fi
    broray_job_checkpoint committing || { job_rc=$?; rm -rf "$job_dir"; return "$job_rc"; }
    job_candidate="$(mktemp "$BRORAY_QUALITY_DIR/.quality-$job_server-XXXXXX")" || { rm -rf "$job_dir"; return 1; }
    if ! cp "$job_quality" "$job_candidate" || ! chmod 600 "$job_candidate" ||
      ! "${BRORAY_OPS_GUARD:-$BRORAY_BASE/bin/broray-ops-guard}" --replace-file "$job_candidate" "$job_target"; then
        # Directory fsync can fail after rename. Preserve evidence and fence.
        BRORAY_JOB_UNRESOLVED=true
        return 75
    fi
    cat "$job_dir/result.json"
    rm -rf "$job_dir" || return 1
    return "$job_rc"
}
