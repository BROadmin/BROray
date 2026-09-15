#!/opt/bin/ash
. "$BRORAY_BASE/lib/operation-job.sh"

broray_xray_job_exit()
{
    local xray_exit_rc
    xray_exit_rc="$1"; trap - EXIT
    broray_xray_update_abort_cleanup || xray_exit_rc=75
    if command -v broray_xray_web_finish >/dev/null 2>&1; then
        broray_xray_web_finish "$xray_exit_rc" || xray_exit_rc=75
    fi
    broray_job_exit "$xray_exit_rc" || xray_exit_rc=75
    exit "$xray_exit_rc"
}

broray_xray_update_install()
{
    local install_prepare_rc install_meta install_legacy
    broray_job_require_owner || return $?
    broray_xray_update_mode="${1:-update}"
    case "$broray_xray_update_mode" in install|update|reinstall) ;; *) return 64 ;; esac
    broray_xray_update_lock_acquire || return $?
    [ -f "$BRORAY_XRAY_BINARY" ] && [ ! -L "$BRORAY_XRAY_BINARY" ] || return 1
    [ -f "$BRORAY_XRAY_CONFIG" ] && [ ! -L "$BRORAY_XRAY_CONFIG" ] || return 1
    for install_legacy in "$BRORAY_XRAY_BINARY.new" "$BRORAY_XRAY_BINARY".broray-*-backup; do
        [ ! -e "$install_legacy" ] && [ ! -L "$install_legacy" ] || return 75
    done
    mkdir -p "$BRORAY_BASE/tmp" || return 1
    BRORAY_XRAY_UPDATE_WORK="$(mktemp -d "$BRORAY_BASE/tmp/xray-job-$BRORAY_BACKGROUND_OPERATION_ID-XXXXXX")" || return 1
    chmod 700 "$BRORAY_XRAY_UPDATE_WORK" || return 1
    printf '%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >"$BRORAY_XRAY_UPDATE_WORK/operation-id" || return 1
    BRORAY_XRAY_PREP_DRAINED=true
    if [ "$broray_xray_update_mode" = install ]; then
        [ -f "${2:-}" ] && [ ! -L "$2" ] || return 1
        [ "$(wc -c <"$2")" -le 4096 ] || return 1
        cp "$2" "$BRORAY_XRAY_UPDATE_WORK/request.json" || return 1
    fi
    broray_job_checkpoint fetching || return $?
    install_prepare_rc=0; BRORAY_XRAY_PREP_DRAINED=false
    broray_ops_run_helper 600 -- "${BRORAY_OPS_ASH:-/opt/bin/ash}" \
      "$BRORAY_BASE/lib/xray-install-prepare.sh" "$BRORAY_XRAY_UPDATE_WORK" "$broray_xray_update_mode" \
      >"$BRORAY_XRAY_UPDATE_WORK/preparation-result.json" || install_prepare_rc=$?
    if [ "$install_prepare_rc" = 75 ]; then BRORAY_JOB_UNRESOLVED=true; return 75; fi
    BRORAY_XRAY_PREP_DRAINED=true
    if [ "$install_prepare_rc" != 0 ]; then
        if jq -e '.success==false' "$BRORAY_XRAY_UPDATE_WORK/preparation-result.json" >/dev/null 2>&1; then
            cat "$BRORAY_XRAY_UPDATE_WORK/preparation-result.json"
        fi
        return "$install_prepare_rc"
    fi
    install_meta="$BRORAY_XRAY_UPDATE_WORK/prepared.json"
    [ -f "$install_meta" ] && [ ! -L "$install_meta" ] || return 1
    jq -e 'type=="object" and (.currentVersion|type)=="string" and (.targetVersion|type)=="string" and
      (.oldSha256|length)==64 and (.candidateSha256|length)==64 and
      (.candidateSize|type)=="number" and .candidateSize>10000000' "$install_meta" >/dev/null || return 1
    broray_xray_current_version="$(jq -r .currentVersion "$install_meta")"
    broray_xray_target_version="$(jq -r .targetVersion "$install_meta")"
    broray_xray_old_sha256="$(jq -r .oldSha256 "$install_meta")"
    broray_xray_candidate_size="$(jq -r .candidateSize "$install_meta")"
    broray_xray_candidate_sha256="$(jq -r .candidateSha256 "$install_meta")"
    broray_xray_candidate="$BRORAY_XRAY_UPDATE_WORK/xray.new"
    broray_xray_old_backup="$BRORAY_XRAY_BINARY.broray-$BRORAY_BACKGROUND_OPERATION_ID-backup"
    [ -f "$broray_xray_candidate" ] && [ ! -L "$broray_xray_candidate" ] || return 1
    [ "$(sha256sum "$broray_xray_candidate" | awk '{print $1}')" = "$broray_xray_candidate_sha256" ] || return 1
    [ "$(sha256sum "$BRORAY_XRAY_BINARY" | awk '{print $1}')" = "$broray_xray_old_sha256" ] || return 1
    broray_xray_was_running=false
    broray_xray_is_running && broray_xray_was_running=true
    broray_job_checkpoint switching || return $?
    BRORAY_XRAY_COMMIT_STARTED=true
    BRORAY_JOB_UNRESOLVED=true
    broray_xray_update_commit
}
