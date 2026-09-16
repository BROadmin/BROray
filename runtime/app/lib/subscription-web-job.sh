#!/opt/bin/ash
# Reuse the coordinator's identity-bound executor handoff. No download runs
# inside the CGI, and neither the URL nor credentials appear in worker argv.
broray_subscription_launch_update()
{
    local id trigger path pending operation nonce worker log
    id="$1"; trigger="${2:-manual}"
    broray_job_require_owner || return $?
    broray_subscription_prepare_dirs || return 1
    broray_subscription_validate_id "$id" || { broray_subscription_emit_error; return 1; }
    path="$(broray_subscription_path "$id")"
    [ -f "$path" ] && [ ! -L "$path" ] || {
        broray_subscription_set_error SUBSCRIPTION_NOT_FOUND 'Подписка не найдена.'
        broray_subscription_emit_error; return 1
    }
    operation="$BRORAY_BACKGROUND_OPERATION_ID"
    pending="$BRORAY_SUB_TMP/subscription-launch.$$.json"
    jq --arg operation "$operation" '.lastUpdateStatus="running" | .backgroundOperationId=$operation | .lastError=null' "$path" >"$pending" || return 1
    broray_subscription_write_json "$path" "$pending" || { rm -f "$pending"; return 1; }
    rm -f "$pending"
    broray_job_checkpoint waiting || return $?
    nonce="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || return 1
    [ "${#nonce}" = 32 ] || return 1
    log="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/operations/$operation/subscription-worker.log"
    [ ! -e "$log" ] && [ ! -L "$log" ] || return 1
    (umask 077; set -C; : >"$log") || return 1
    "${BRORAY_OPS_ASH:-/opt/bin/ash}" "$BRORAY_BASE/lib/subscription-web-worker.sh" "$id" "$trigger" "$nonce" </dev/null >/dev/null 2>"$log" & worker=$!
    if ! broray_ops_handoff_to "$worker" "$nonce"; then
        if broray_ops_cancel_requested && broray_job_require_owner; then
            # Still the original owner: transfer was not committed. The child
            # stays gated, observes cancellation and exits without mutations.
            wait "$worker" || true
            broray_job_finish aborted || return 75
            return 130
        fi
        BRORAY_JOB_UNRESOLVED=true
        broray_subscription_set_error OPERATION_UNRESOLVED 'Подписка сохранена, но передача обновления ещё не подтверждена. Проверьте состояние операций.'
        broray_subscription_emit_error; return 75
    fi
    BRORAY_JOB_ACTIVE=false
    BRORAY_SUB_ASYNC_ACCEPTED=true
    jq -nc --arg id "$id" --arg operation "$operation" '{accepted:true,id:$id,backgroundOperationId:$operation}'
}
