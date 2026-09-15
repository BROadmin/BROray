#!/opt/bin/ash
# Owner-side preparation and cleanup. No URLs or credentials in helper argv.
broray_subscription_prepare_update()
{
    local prep_mode prep_rc prep_timeout prep_result
    BRORAY_SUB_PREP_DRAINED=false
    BRORAY_SUB_PREP_DIR="$(mktemp -d "$BRORAY_BASE/tmp/subscription-op-$BRORAY_BACKGROUND_OPERATION_ID-XXXXXX")" || return 1
    chmod 700 "$BRORAY_SUB_PREP_DIR" || return 1
    printf '%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >"$BRORAY_SUB_PREP_DIR/operation-id" || return 1
    BRORAY_SUB_PREP_DRAINED=true
    update_download="$BRORAY_SUB_PREP_DIR/download"
    update_nodes="$BRORAY_SUB_PREP_DIR/nodes"
    update_stage="$BRORAY_SUB_PREP_DIR/stage"
    BRORAY_SUB_WARNINGS_FILE="$BRORAY_SUB_PREP_DIR/warnings.txt"
    for prep_mode in fetch parse; do
        case "$prep_mode" in fetch) prep_timeout=180; broray_job_checkpoint fetching ;; parse) prep_timeout=300; broray_job_checkpoint parsing ;; esac || return $?
        prep_rc=0; BRORAY_SUB_PREP_DRAINED=false
        broray_ops_run_helper "$prep_timeout" -- "${BRORAY_OPS_ASH:-/opt/bin/ash}" "$BRORAY_BASE/lib/subscription-prepare.sh" \
          "$prep_mode" "$BRORAY_SUB_PREP_DIR" "$update_path" || prep_rc=$?
        if [ "$prep_rc" = 75 ]; then BRORAY_JOB_UNRESOLVED=true; return 75; fi
        BRORAY_SUB_PREP_DRAINED=true
        [ "$prep_rc" != 130 ] || return 130
        prep_result="$BRORAY_SUB_PREP_DIR/$prep_mode-result.json"
        if [ "$prep_rc" = 124 ]; then
            broray_subscription_set_error DOWNLOAD_TIMEOUT "Превышено время подготовки подписки."
            return 1
        fi
        if [ ! -f "$prep_result" ] || [ -L "$prep_result" ] ||
          ! jq -e 'type=="object" and (.ok|type)=="boolean" and (.errorCode|type)=="string" and (.errorMessage|type)=="string" and
            all([.received,.parsed,.accepted,.rejected][]; type=="number" and .>=0 and .<=500 and floor==.)' "$prep_result" >/dev/null; then
            broray_subscription_set_error INTERNAL_ERROR "Не удалось подтвердить результат подготовки подписки."
            return 1
        fi
        if [ "$prep_rc" != 0 ] || ! jq -e '.ok==true' "$prep_result" >/dev/null; then
            BRORAY_SUB_ERROR_CODE="$(jq -r '.errorCode' "$prep_result")"
            BRORAY_SUB_ERROR_MESSAGE="$(jq -r '.errorMessage' "$prep_result")"
            [ -n "$BRORAY_SUB_ERROR_CODE" ] || BRORAY_SUB_ERROR_CODE=INTERNAL_ERROR
            return 1
        fi
    done
    BRORAY_SUB_RECEIVED="$(jq -r '.received' "$prep_result")"
    BRORAY_SUB_PARSED="$(jq -r '.parsed' "$prep_result")"
    BRORAY_SUB_ACCEPTED="$(jq -r '.accepted' "$prep_result")"
    BRORAY_SUB_REJECTED="$(jq -r '.rejected' "$prep_result")"
}

broray_subscription_cleanup_preparation()
{
    [ "${BRORAY_SUB_PREP_DRAINED:-false}" = true ] || return 0
    case "${BRORAY_SUB_PREP_DIR:-}" in "$BRORAY_BASE/tmp/subscription-op-$BRORAY_BACKGROUND_OPERATION_ID-"*) ;; *) return 1 ;; esac
    [ -d "$BRORAY_SUB_PREP_DIR" ] && [ ! -L "$BRORAY_SUB_PREP_DIR" ] || return 1
    [ "$(cat "$BRORAY_SUB_PREP_DIR/operation-id")" = "$BRORAY_BACKGROUND_OPERATION_ID" ] || return 1
    rm -rf "$BRORAY_SUB_PREP_DIR" || return 1
    unset BRORAY_SUB_PREP_DIR BRORAY_SUB_PREP_DRAINED
}
