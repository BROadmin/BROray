#!/opt/bin/ash
# Owner-side preparation and cleanup. No URLs or credentials in helper argv.
broray_subscription_prepare_update()
{
    local prep_mode prep_rc prep_timeout prep_result
    BRORAY_SUB_PROVIDER_METADATA='{"schemaVersion":1}'
    BRORAY_SUB_PREP_DRAINED=false
    if [ -n "${BRORAY_SUB_QUEUE_REQUEST:-}" ]; then
        broray_subscription_queue_use_prepared "$BRORAY_SUB_QUEUE_REQUEST"
        return $?
    fi
    # Direct/scheduled updates can be the first operation after a clean boot;
    # the CGI launcher is not responsible for creating their progress folder.
    [ ! -L "$BRORAY_SUB_RUN" ] && mkdir -p "$BRORAY_SUB_RUN" || return 1
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
            if [ "$prep_mode" = parse ]; then
                broray_subscription_set_error PARSE_TIMEOUT "Превышено время разбора серверов подписки. Предыдущий список серверов сохранён."
            else
                broray_subscription_set_error DOWNLOAD_TIMEOUT "Превышено время загрузки подписки. Предыдущий список серверов сохранён."
            fi
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
    # Metadata may enter persistent state only after both supervised helpers drain.
    prep_result="$BRORAY_SUB_PREP_DIR/provider-metadata.json"
    if [ ! -f "$prep_result" ] || [ -L "$prep_result" ] || [ "$(wc -c < "$prep_result")" -gt 65536 ] ||
       ! jq -e 'type=="object" and .schemaVersion==1' "$prep_result" >/dev/null 2>&1; then
        broray_subscription_set_error INTERNAL_ERROR 'Не удалось подтвердить сведения провайдера.'; return 1
    fi
    BRORAY_SUB_PROVIDER_METADATA="$(jq -c . "$prep_result")" || return 1
    prep_result="$BRORAY_SUB_PREP_DIR/parse-result.json"
    BRORAY_SUB_RECEIVED="$(jq -r '.received' "$prep_result")"
    BRORAY_SUB_PARSED="$(jq -r '.parsed' "$prep_result")"
    BRORAY_SUB_ACCEPTED="$(jq -r '.accepted' "$prep_result")"
    BRORAY_SUB_REJECTED="$(jq -r '.rejected' "$prep_result")"
}

broray_subscription_cleanup_preparation()
{
    local progress_file
    [ "${BRORAY_SUB_PREP_DRAINED:-false}" = true ] || return 0
    # The terminal queue request owns RAM retention. Legacy cleanup must not
    # remove its continuation/evidence while the global apply is still running.
    if [ -n "${BRORAY_SUB_QUEUE_REQUEST:-}" ] &&
       [ "${BRORAY_SUB_PREP_DIR:-}" = "${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}/requests/$BRORAY_SUB_QUEUE_REQUEST/preparation" ]; then
        return 0
    fi
    case "${BRORAY_SUB_PREP_DIR:-}" in "$BRORAY_BASE/tmp/subscription-op-$BRORAY_BACKGROUND_OPERATION_ID-"*) ;; *) return 1 ;; esac
    [ -d "$BRORAY_SUB_PREP_DIR" ] && [ ! -L "$BRORAY_SUB_PREP_DIR" ] || return 1
    [ "$(cat "$BRORAY_SUB_PREP_DIR/operation-id")" = "$BRORAY_BACKGROUND_OPERATION_ID" ] || return 1
    rm -rf "$BRORAY_SUB_PREP_DIR" || return 1
    progress_file="$BRORAY_SUB_RUN/progress-$BRORAY_BACKGROUND_OPERATION_ID.json"
    if [ -f "$progress_file" ] && [ ! -L "$progress_file" ]; then rm -f "$progress_file"; fi
    unset BRORAY_SUB_PREP_DIR BRORAY_SUB_PREP_DRAINED
}

# Hash the complete private preparation, including names/modes. No symlink,
# special file, hard link or unbounded inventory can become a continuation.
broray_subscription_queue_inventory()
(
    local file metadata size total count uid hash
    set -o pipefail
    [ -d "$1" ] && [ ! -L "$1" ] || exit 76
    cd "$1" || exit 76
    uid="$(id -u)" || exit 76
    [ -z "$(find . ! -type f ! -type d -print)" ] || exit 76
    find . -type d -print | while IFS= read -r file; do
        [ "$(broray_ops_stat -c '%u:%a' "$file")" = "$uid:700" ] || exit 76
    done || exit 76
    find . -type f -print | LC_ALL=C sort | {
        total=0; count=0
        while IFS= read -r file; do
            case "$file" in ''|*[!a-zA-Z0-9._/-]*) exit 76 ;; esac
            metadata="$(broray_ops_stat -c '%u %a %h %s' "$file")" || exit 76
            case "$metadata" in "$uid 600 1 "*) ;; *) exit 76 ;; esac
            size="${metadata##* }"
            case "$size" in ''|*[!0-9]*) exit 76 ;; esac
            count=$((count+1)); total=$((total+size))
            [ "$count" -le 4096 ] && [ "$total" -le 67108864 ] || exit 76
            hash="$(sha256sum "$file")" || exit 76
            printf '%s %s\n' "$metadata" "$hash"
        done
    } | sha256sum | cut -d ' ' -f 1
)

broray_subscription_step()
{
    local sq_request sq_stage sq_owner sq_id sq_context sq_source sq_directory sq_prep sq_path sq_result
    local sq_digest sq_previous sq_outcome sq_inventory sq_rc sq_timeout sq_hwid sq_next sq_tmp sq_expected sq_trigger
    sq_request="$1"; sq_stage="$2"
    case "$sq_stage" in fetch|parse|apply) ;; *) return 64 ;; esac
    . "${BRORAY_OPS_CODE_ROOT:-${BRORAY_ROOT:-/opt/broray}}/lib/subscription-service.sh" || return 74
    broray_job_require_owner || return $?
    sq_owner="$(broray_ops_operation_directory)" || return 74
    sq_id="$(jq -er .bundleId "$sq_owner/state.json")" || return 74
    sq_context="$(jq -er .queueStep.context "$sq_owner/state.json")" || return 74
    sq_source="$(jq -er .source "$sq_owner/state.json")" || return 74
    sq_path="$(broray_subscription_path "$sq_id")" || return 74
    [ -f "$sq_path" ] && [ ! -L "$sq_path" ] &&
      [ "$(sha256sum "$sq_path" | cut -d ' ' -f 1)" = "$sq_context" ] || return 76
    case "$sq_source" in
      SUBSCRIPTION_AUTO) jq -e '.enabled==true and .autoUpdateEnabled==true' "$sq_path" >/dev/null || return 76 ;;
      USER) ;;
      *) return 74 ;;
    esac
    sq_directory="${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}/requests/$sq_request"
    [ -d "$sq_directory" ] && [ ! -L "$sq_directory" ] &&
      [ "$(broray_ops_stat -c '%u:%a' "$sq_directory")" = "$(id -u):700" ] || return 74
    sq_prep="$sq_directory/preparation"; sq_result="$sq_directory/result.json"
    sq_digest="$(jq -r '.queueStep.resultSha256 // empty' "$sq_owner/state.json")" || return 74
    if [ "$sq_stage" = fetch ]; then
        [ -z "$sq_digest" ] && [ ! -e "$sq_result" ] && [ ! -L "$sq_result" ] &&
          [ ! -e "$sq_prep" ] && [ ! -L "$sq_prep" ] || return 76
        mkdir -m 700 "$sq_prep" "$sq_prep/progress" || return 74
        sq_hwid="$(jq -r '.clientHwid // empty' "$sq_path")" || return 74
        if ! broray_subscription_client_hwid_valid "$sq_hwid"; then
            sq_hwid="$(broray_subscription_generate_client_hwid)" || return 74
            broray_subscription_client_hwid_valid "$sq_hwid" || return 74
        fi
        (set -C; jq --arg hwid "$sq_hwid" '.clientHwid=$hwid' "$sq_path" >"$sq_prep/input.json") || return 74
        sq_previous="$(jq -nc --arg request "$sq_request" --arg context "$sq_context" \
          --arg now "$(broray_subscription_now_iso)" --argjson epoch "$(date '+%s')" \
          '{schemaVersion:1,kind:"subscription",requestId:$request,context:$context,startedAt:$now,startedEpoch:$epoch}')"
    else
        [ -n "$sq_digest" ] && [ -f "$sq_result" ] && [ ! -L "$sq_result" ] &&
          [ "$(broray_ops_stat -c '%u:%a:%h' "$sq_result")" = "$(id -u):600:1" ] &&
          [ "$(sha256sum "$sq_result" | cut -d ' ' -f 1)" = "$sq_digest" ] || return 76
        sq_expected=fetch
        [ "$sq_stage" != apply ] || sq_expected=parse
        sq_previous="$(jq -ce --arg request "$sq_request" --arg context "$sq_context" --arg stage "$sq_expected" '
          select(.schemaVersion==1 and .kind=="subscription" and .requestId==$request and
            .context==$context and .stage==$stage and (.outcome.ok|type)=="boolean" and
            (.inventorySha256|type)=="string")' "$sq_result")" || return 76
        sq_inventory="$(broray_subscription_queue_inventory "$sq_prep")" || return 76
        [ "$sq_inventory" = "$(printf '%s\n' "$sq_previous" | jq -r .inventorySha256)" ] || return 76
    fi
    if [ "$sq_stage" = apply ]; then
        sq_hwid="$(jq -er .clientHwid "$sq_prep/input.json")" || return 76
        broray_subscription_client_hwid_valid "$sq_hwid" || return 76
        if [ "$(jq -r '.clientHwid // empty' "$sq_path")" != "$sq_hwid" ]; then
            # Missing legacy HWID was generated privately for the actual
            # download. Persist that same value only under this global owner.
            broray_subscription_acquire_lock "$sq_id" || return $?
            sq_tmp="$sq_directory/hwid-$BRORAY_BACKGROUND_OPERATION_ID.json"
            [ ! -e "$sq_tmp" ] && [ ! -L "$sq_tmp" ] || return 74
            (set -C; jq --arg hwid "$sq_hwid" '.clientHwid=$hwid' "$sq_path" >"$sq_tmp") || return 74
            BRORAY_JOB_UNRESOLVED=true
            broray_subscription_write_json "$sq_path" "$sq_tmp" || return 75
            BRORAY_JOB_UNRESOLVED=false
        fi
        sq_trigger=manual
        [ "$sq_source" != SUBSCRIPTION_AUTO ] || sq_trigger=automatic
        BRORAY_SUB_QUEUE_REQUEST="$sq_request"
        sq_rc=0
        broray_subscription_update "$sq_id" "$sq_trigger" || sq_rc=$?
        unset BRORAY_SUB_QUEUE_REQUEST
        return "$sq_rc"
    fi
    printf '%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >"$sq_prep/operation-id" || return 74
    sq_outcome="$(printf '%s\n' "$sq_previous" | jq -c '.outcome // {ok:true}')"
    if printf '%s\n' "$sq_outcome" | jq -e '.ok==true' >/dev/null; then
        case "$sq_stage" in
          fetch) sq_timeout=180; broray_job_checkpoint fetching ;;
          parse) sq_timeout=300; broray_job_checkpoint parsing ;;
        esac || return $?
        sq_rc=0
        BRORAY_SUB_RUN="$sq_prep/progress" broray_ops_run_helper "$sq_timeout" -- \
          "${BRORAY_OPS_ASH:-/opt/bin/ash}" "$BRORAY_BASE/lib/subscription-prepare.sh" \
          "$sq_stage" "$sq_prep" "$sq_prep/input.json" || sq_rc=$?
        case "$sq_rc" in
          75) BRORAY_JOB_UNRESOLVED=true; return 75 ;;
          0|1) ;;
          124)
            sq_outcome='{"ok":false,"errorCode":"PREPARATION_TIMEOUT","errorMessage":"Превышено время подготовки подписки.","received":0,"parsed":0,"accepted":0,"rejected":0}' ;;
          *) return "$sq_rc" ;;
        esac
        if [ "$sq_rc" != 124 ]; then
            [ -f "$sq_prep/$sq_stage-result.json" ] && [ ! -L "$sq_prep/$sq_stage-result.json" ] || return 74
            sq_outcome="$(jq -ce --argjson rc "$sq_rc" '
              select(type=="object" and .ok==($rc==0) and (.errorCode|type)=="string" and
                (.errorMessage|type)=="string" and
                all([.received,.parsed,.accepted,.rejected][];type=="number" and .>=0 and .<=500 and floor==.))
              ' "$sq_prep/$sq_stage-result.json")" || return 74
        fi
    fi
    # Preparation never edits the live subscription or catalog.
    [ -f "$sq_path" ] && [ ! -L "$sq_path" ] &&
      [ "$(sha256sum "$sq_path" | cut -d ' ' -f 1)" = "$sq_context" ] || return 76
    sq_inventory="$(broray_subscription_queue_inventory "$sq_prep")" || return 76
    sq_tmp="$sq_directory/result-$BRORAY_BACKGROUND_OPERATION_ID.tmp"
    [ ! -e "$sq_tmp" ] && [ ! -L "$sq_tmp" ] && [ ! -L "$sq_result" ] || return 74
    (set -C; printf '%s\n' "$sq_previous" | jq -c --arg stage "$sq_stage" \
      --arg inventory "$sq_inventory" --argjson outcome "$sq_outcome" \
      '.stage=$stage | .inventorySha256=$inventory | .outcome=$outcome' >"$sq_tmp") || return 74
    chmod 600 "$sq_tmp" && mv "$sq_tmp" "$sq_result" || return 74
    sq_digest="$(sha256sum "$sq_result" | cut -d ' ' -f 1)" || return 74
    case "$sq_stage" in fetch) sq_next=parse ;; parse) sq_next=apply ;; esac
    broray_job_yield "$sq_next" "$sq_digest"
}

broray_subscription_queue_use_prepared()
{
    local qr_owner qr_directory qr_result qr_context qr_digest qr_record qr_inventory qr_outcome qr_metadata
    broray_job_require_owner || return $?
    qr_owner="$(broray_ops_operation_directory)" || return 74
    jq -e --arg request "$1" '.operation=="subscriptions:refresh" and
      .queueStep.stage=="apply" and .queueStep.requestId==$request and
      .cancelability=="protected"' "$qr_owner/state.json" >/dev/null || return 73
    qr_context="$(jq -er .queueStep.context "$qr_owner/state.json")" || return 74
    qr_digest="$(jq -er .queueStep.resultSha256 "$qr_owner/state.json")" || return 74
    qr_directory="${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}/requests/$1"
    qr_result="$qr_directory/result.json"
    [ -f "$qr_result" ] && [ ! -L "$qr_result" ] &&
      [ "$(broray_ops_stat -c '%u:%a:%h' "$qr_result")" = "$(id -u):600:1" ] &&
      [ "$(sha256sum "$qr_result" | cut -d ' ' -f 1)" = "$qr_digest" ] || return 76
    qr_record="$(jq -ce --arg request "$1" --arg context "$qr_context" '
      select(.schemaVersion==1 and .kind=="subscription" and .stage=="parse" and
        .requestId==$request and .context==$context and (.outcome.ok|type)=="boolean" and
        all([.outcome.received,.outcome.parsed,.outcome.accepted,.outcome.rejected][];
          type=="number" and .>=0 and .<=500 and floor==.))' "$qr_result")" || return 76
    qr_inventory="$(broray_subscription_queue_inventory "$qr_directory/preparation")" || return 76
    [ "$qr_inventory" = "$(printf '%s\n' "$qr_record" | jq -r .inventorySha256)" ] || return 76
    BRORAY_SUB_PREP_DIR="$qr_directory/preparation"
    BRORAY_SUB_PREP_DRAINED=true
    update_download="$BRORAY_SUB_PREP_DIR/download"
    update_nodes="$BRORAY_SUB_PREP_DIR/nodes"
    update_stage="$BRORAY_SUB_PREP_DIR/stage"
    BRORAY_SUB_WARNINGS_FILE="$BRORAY_SUB_PREP_DIR/warnings.txt"
    qr_outcome="$(printf '%s\n' "$qr_record" | jq -c .outcome)"
    BRORAY_SUB_RECEIVED="$(printf '%s\n' "$qr_outcome" | jq -r .received)"
    BRORAY_SUB_PARSED="$(printf '%s\n' "$qr_outcome" | jq -r .parsed)"
    BRORAY_SUB_ACCEPTED="$(printf '%s\n' "$qr_outcome" | jq -r .accepted)"
    BRORAY_SUB_REJECTED="$(printf '%s\n' "$qr_outcome" | jq -r .rejected)"
    if ! printf '%s\n' "$qr_outcome" | jq -e '.ok==true' >/dev/null; then
        BRORAY_SUB_ERROR_CODE="$(printf '%s\n' "$qr_outcome" | jq -r .errorCode)"
        BRORAY_SUB_ERROR_MESSAGE="$(printf '%s\n' "$qr_outcome" | jq -r .errorMessage)"
        return 1
    fi
    qr_metadata="$BRORAY_SUB_PREP_DIR/provider-metadata.json"
    [ -f "$qr_metadata" ] && [ ! -L "$qr_metadata" ] && [ "$(wc -c <"$qr_metadata")" -le 65536 ] || return 74
    BRORAY_SUB_PROVIDER_METADATA="$(jq -ce 'select(type=="object" and .schemaVersion==1)' "$qr_metadata")" || return 74
    return 0
}

# Existing scheduler admission. Download/parse/apply run only as finite workers.
broray_subscription_enqueue_due()
{
    local se_pause se_paused se_file se_now se_id se_hash se_reply se_requests se_dot
    se_pause="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/background-automation.json"
    se_paused=false
    if [ -e "$se_pause" ] || [ -L "$se_pause" ]; then
        [ -f "$se_pause" ] && [ ! -L "$se_pause" ] || return 74
        se_paused="$(jq -er 'select((.paused|type)=="boolean")|.paused|tostring' "$se_pause")" || return 74
    fi
    se_requests='[]'; se_dot=null
    if [ "$se_paused" != true ]; then
        . "${BRORAY_OPS_CODE_ROOT:-$BRORAY_BASE}/lib/active-proxy-health.sh" || return 74
        . "${BRORAY_OPS_CODE_ROOT:-$BRORAY_BASE}/lib/dot-auto.sh" || return 74
        [ ! -L "$BRORAY_SUB_DIR" ] || return 74
        se_now="$(broray_subscription_now_epoch)"
        for se_file in "$BRORAY_SUB_DIR"/*.json; do
            [ -f "$se_file" ] && [ ! -L "$se_file" ] || continue
            jq -e --argjson now "$se_now" '.enabled==true and .autoUpdateEnabled==true and
              (.nextUpdateEpoch|type)=="number" and .nextUpdateEpoch>0 and .nextUpdateEpoch<=$now' \
              "$se_file" >/dev/null || continue
            se_id="${se_file##*/}"; se_id="${se_id%.json}"
            broray_subscription_validate_id "$se_id" || return 74
            se_hash="$(sha256sum "$se_file" | cut -d ' ' -f 1)" || return 74
            se_reply="$(broray_auto_enqueue_request subscriptions:refresh "$se_id" SUBSCRIPTION_AUTO "$se_hash" 4)" || return $?
            se_requests="$(printf '%s\n' "$se_requests" | jq -c --argjson reply "$se_reply" '.+[$reply]')" || return 74
        done
        if broray_dot_auto_due; then
            se_hash="$(broray_dot_auto_context)" || return 74
            se_dot="$(broray_auto_enqueue_request dot:auto-check selected SCHEDULER "$se_hash" 5)" || return $?
        fi
    fi
    jq -nc --argjson paused "$se_paused" --argjson requests "$se_requests" --argjson dot "$se_dot" \
      '{ok:true,paused:$paused,subscriptionRequests:$requests,dotRequest:$dot}'
}
