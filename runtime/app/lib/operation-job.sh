#!/opt/bin/ash
# Lifecycle for an actual foreground job, never the scheduling daemon.
. "${BRORAY_ROOT:-/opt/broray}/lib/operation-client.sh"

broray_job_require_owner()
{
    local job_pid job_rest
    [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] &&
      [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] || return 73
    # $$ is unchanged in ash subshells. A builtin open/read identifies the
    # process executing the mutation, without spawning a PID-query child.
    IFS=' ' read -r job_pid job_rest </proc/self/stat || return 73
    broray_ops_call owner-check "$BRORAY_BACKGROUND_OPERATION_ID" \
      "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$job_pid" >/dev/null
}

broray_job_begin()
{
    [ -z "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 73
    broray_ops_begin "$@" || return $?
    BRORAY_JOB_ACTIVE=true
}

broray_job_finish()
{
    [ "${BRORAY_JOB_ACTIVE:-false}" = true ] || return 0
    # An unresolved domain commit must retain the fence for domain recovery.
    [ "${BRORAY_JOB_UNRESOLVED:-false}" != true ] || return 75
    broray_job_require_owner || return $?
    case "${1:-failed}" in
      completed) broray_ops_finish completed || return $? ;;
      aborted) broray_ops_finish aborted CANCELLED || return $? ;;
      *)
        if broray_ops_cancel_requested; then broray_ops_finish aborted CANCELLED || return $?
        else broray_ops_finish failed OPERATION_FAILED || return $?; fi ;;
    esac
    BRORAY_JOB_ACTIVE=false
}

broray_job_exit()
{
    local job_rc
    job_rc="${1:-1}"
    case "$job_rc" in
      0) broray_job_finish completed ;;
      130) broray_job_finish aborted ;;
      *) broray_job_finish failed ;;
    esac
}

broray_job_checkpoint()
{
    local job_phase_rc
    broray_job_require_owner || return $?
    if broray_ops_cancel_requested; then return 130; fi
    job_phase_rc=0
    broray_ops_tick "${1:-working}" || job_phase_rc=$?
    [ "$job_phase_rc" = 0 ] || { broray_ops_cancel_requested && return 130; return "$job_phase_rc"; }
}

broray_job_publish_json()
{
    local pub_pid pub_rest pub_nonce pub_revision pub_response pub_rc pub_attempt
    broray_job_require_owner || return $?
    IFS=' ' read -r pub_pid pub_rest </proc/self/stat || return 73
    pub_nonce="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || return 1
    pub_revision="$(jq -er '.revision' "${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json")" || return 1
    pub_attempt=0
    while [ "$pub_attempt" -lt 3 ]; do
        pub_attempt=$((pub_attempt+1)); pub_rc=0
        pub_response="$(broray_ops_call publish-json "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" \
          "$pub_pid" "$1" "$2" "$3" "$pub_nonce" "$pub_revision")" || pub_rc=$?
        if [ "$pub_rc" = 0 ] && printf '%s\n' "$pub_response" | jq -es 'length==1 and .[0].ok==true and .[0].published==true' >/dev/null 2>&1; then return 0; fi
        if printf '%s\n' "$pub_response" | jq -e '.ok==false and .errorCode=="CANCELLED"' >/dev/null 2>&1; then return 130; fi
        # Retry the same nonce/revision even after a writer or transport error.
        # If durability still cannot be confirmed, finish must retain the fence.
    done
    BRORAY_JOB_UNRESOLVED=true
    return 75
}
