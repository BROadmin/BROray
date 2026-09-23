#!/opt/bin/ash
# Canonical preflight domain. PREPARED recovery is supported; STOP_INTENT is
# retained until explicit domain verification/rollback is implemented.

ops_platform_is_preflight()
{
    jq -e '.operation=="system:platform-preflight"' "$OPS_CURRENT/state.json" >/dev/null 2>&1
}

ops_platform_sha_valid()
{
    case "${1:-}" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#1}" -eq 64 ]
}

ops_platform_prepared_valid()
{
    jq -e --arg id "$OPS_ID" '
      .schemaVersion==2 and .kind=="background" and .operationId==$id and
      .operation=="system:platform-preflight" and .scope=="system" and
      .source=="UPDATER" and .bundleId=="updater-platform" and
      .cancelability=="protected" and .initialCancelability=="protected" and
      (.platformPreflight | type=="object" and
        keys==["contract","expectedPlatformManifestSha256","mutationStarted","phase","schemaVersion"] and
        .schemaVersion==1 and .contract=="broray-platform-preflight/1" and
        .phase=="PREPARED" and .mutationStarted==false and
        (.expectedPlatformManifestSha256|type)=="string" and
        (.expectedPlatformManifestSha256|length)==64 and
        (.expectedPlatformManifestSha256|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))))
    ' "$OPS_CURRENT/state.json" >/dev/null 2>&1 || return 1
    # A later implementation may add these domain records. This stage must
    # never retire them by mistaking a terminal flag for domain consistency.
    local file
    for file in platform-mutation.json platform-recovery.json platform-service.json platform-stop-supervision.json; do
        [ ! -e "$OPS_CURRENT/$file" ] && [ ! -L "$OPS_CURRENT/$file" ] || return 1
    done
}

ops_platform_finish_ready()
{
    ops_platform_is_preflight || return 0
    ops_platform_prepared_valid
}

# The completed platform proof is checked by the retained native runtime under
# this SAME inherited coordinator flock. Generic finish/recover cannot use it.
ops_platform_completion_fence()
{
    local previous rc file name history
    history="${2:-}"; case "$history" in ''|retired-origin) ;; *) return 1 ;; esac
    ops_platform_stop_valid "${1:-STOP_INTENT}" || return 1
    # Validate the complete fence BEFORE terminalizing state. Retirement also
    # checks it, but a foreign file must not leave a false completed operation.
    ops_dir_safe "$OPS_CURRENT/fence" || return 1
    for name in pid scope action bundle startedAt owner.json; do
        ops_publication_private "$OPS_CURRENT/fence/$name" 4096 || return 1
    done
    for file in "$OPS_CURRENT/fence"/* "$OPS_CURRENT/fence"/.[!.]* "$OPS_CURRENT/fence"/..?*; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        case "${file##*/}" in pid|scope|action|bundle|startedAt|owner.json) ;; *) return 1 ;; esac
    done
    if jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null; then
        [ -z "$history" ] || return 1
        [ ! -e "$OPS_CURRENT/retired-lock" ] && [ ! -L "$OPS_CURRENT/retired-lock" ] || return 1
        ops_global_matches
        return $?
    fi
    jq -e '.running==false and .state=="completed" and .phase=="finished" and
        .errorCode==null and (.finishedAt|type)=="string" and (.finishedAt|length)>0' "$OPS_CURRENT/state.json" >/dev/null || return 1
    if [ -z "$history" ] && ops_global_matches; then
        [ ! -e "$OPS_CURRENT/retired-lock" ] && [ ! -L "$OPS_CURRENT/retired-lock" ]
        return $?
    fi
    # Historical origin proof does not authorize the current global owner.
    # Its caller must separately validate that operation and its live fence.
    if [ -z "$history" ]; then [ ! -e "$OPS_GLOBAL" ] && [ ! -L "$OPS_GLOBAL" ] || return 1; fi
    [ -L "$OPS_CURRENT/retired-lock" ] || return 1
    previous="$OPS_GLOBAL"; OPS_GLOBAL="$OPS_CURRENT/retired-lock"
    ops_global_matches; rc=$?
    OPS_GLOBAL="$previous"
    return "$rc"
}

ops_platform_complete()
{
    local nonce binding native_sha migration_sha generation live actual proof replay
    [ "$#" = 2 ] || ops_error INVALID_REQUEST 1
    ops_load "$1" || ops_error STATE_UNAVAILABLE 1
    nonce="$2"; ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    ops_platform_completion_fence || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    jq -e --arg nonce "$nonce" '.platformPreflight.stopNonce==$nonce' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    binding="$OPS_CURRENT/platform-bootguard.json"
    ops_file_safe "$binding" 4096 || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
    native_sha="$(jq -er .nativeSha256 "$binding")"; migration_sha="$(jq -er .migrationIntentSha256 "$binding")"
    ops_platform_sha_valid "$native_sha" && ops_platform_sha_valid "$migration_sha" || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
    generation="$OPS_UPDATER/runtimes/$native_sha/runtime"
    ops_platform_service_path_safe "$generation" && ops_file_safe "$generation" 16777216 && [ -x "$generation" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    actual="$(sha256sum "$generation")" || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    [ "${actual%% *}" = "$native_sha" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    case "$OPS_APP" in */opt/broray) live="${OPS_APP%/opt/broray}" ;; *) ops_error UNSAFE_STATE 1 ;; esac
    [ -n "$live" ] || live=/
    proof="$("$generation" recovery-commit-check "$live" "$OPS_ID" "$migration_sha" "$nonce")" || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    printf '%s\n' "$proof" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="COMMIT_VERIFIED" and
      .[0].platformReady==true and .[0].activationAllowed==false and
      (.[0].generationId|type=="string" and (length==24 and startswith("g-") and (.[2:]|all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==95 or .==45)))) and
      (.[0].commitReceiptSha256|type=="string" and (type=="string" and length==64 and all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))))' >/dev/null || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    ops_platform_completion_fence || ops_error OWNER_CHANGED
    replay=false
    if jq -e '.running==false' "$OPS_CURRENT/state.json" >/dev/null; then
        replay=true
    else
        ops_state_transition completed finished '' || ops_error STATE_UNAVAILABLE 1
    fi
    if ops_global_matches; then ops_retire_global || ops_error STATE_UNAVAILABLE 1; fi
    # Persist both sides of the existing coordinator rename before admission.
    "$OPS_GUARD" --sync-state "$OPS_GLOBAL" && "$OPS_GUARD" --sync-state "$OPS_CURRENT/state.json" || ops_error STATE_UNAVAILABLE 1
    ops_platform_completion_fence || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    printf '%s\n' "$proof" | jq -c --argjson replay "$replay" '.phase="PREFLIGHT_COMPLETED" | .replayed=$replay'
}

ops_platform_recover_prepared()
{
    # Generic recovery/another job cannot settle protected platform state.
    [ "${verb:-}" = platform-preflight-begin ] || return 1
    ops_platform_is_preflight && ops_platform_prepared_valid || return 1
    ops_platform_sha_valid "${OPS_PREFLIGHT_EXPECTED_SHA:-}" || return 1
    jq -e --arg sha "$OPS_PREFLIGHT_EXPECTED_SHA" \
      '.platformPreflight.expectedPlatformManifestSha256==$sha' \
      "$OPS_CURRENT/state.json" >/dev/null 2>&1 || return 1
    # The caller already classified the owner as STALE, drained registered
    # children and checked publication. PREPARED permits NO platform writers.
    # Domain rollback after STOP_INTENT is a separate, not-yet-enabled stage.
    ops_pending_domain && return 1
    ops_platform_queue_clear
}

# Explicitly abandon *only staging* left by a vanished preflight caller. No
# claim is made about legacy descendant writers: the next transaction still
# requires supervised migration and its reboot boundary. Generic recovery and
# finish cannot enter here. All checks and fence retirement share the guard.
ops_platform_discard_stage()
{
    local nonce owner entry name saved current expected desired payload stage live generation proof running previous rc replay
    [ "$#" = 2 ] || ops_error INVALID_REQUEST 1
    ops_load "$1" || ops_error STATE_UNAVAILABLE 1
    nonce="$2"; ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    ops_platform_stop_valid && ops_platform_service_record_valid || ops_error PLATFORM_STAGE_DISCARD_UNCONFIRMED 75
    jq -e --arg nonce "$nonce" '.platformPreflight.stopNonce==$nonce and .platformPreflight.generationStop==null and
      ((.running==true and .state=="running" and .phase=="working") or
       (.running==false and .state=="aborted" and .phase=="finished" and .errorCode=="PREFLIGHT_STAGING_ABORTED"))' \
      "$OPS_CURRENT/state.json" >/dev/null || ops_error PLATFORM_STAGE_DISCARD_UNCONFIRMED 75
    # This exact early phase admits no helper, handoff, publication or guard
    # evidence. Even an empty/unknown record is preserved and refused.
    for entry in "$OPS_CURRENT"/* "$OPS_CURRENT"/.[!.]* "$OPS_CURRENT"/..?*; do
        [ -e "$entry" ] || [ -L "$entry" ] || continue
        case "${entry##*/}" in state.json|owner.json|fence|platform-service.json|platform-migration|retired-lock) ;;
          *) ops_error PLATFORM_STAGE_DISCARD_UNCONFIRMED 75 ;;
        esac
    done
    for name in state.json owner.json platform-service.json; do
        ops_publication_private "$OPS_CURRENT/$name" 32768 || ops_error PLATFORM_STAGE_DISCARD_UNCONFIRMED 75
    done
    for name in pid scope action bundle startedAt owner.json; do
        ops_publication_private "$OPS_CURRENT/fence/$name" 4096 || ops_error PLATFORM_STAGE_DISCARD_UNCONFIRMED 75
    done
    for entry in "$OPS_CURRENT/fence"/* "$OPS_CURRENT/fence"/.[!.]* "$OPS_CURRENT/fence"/..?*; do
        [ -e "$entry" ] || [ -L "$entry" ] || continue
        case "${entry##*/}" in pid|scope|action|bundle|startedAt|owner.json) ;; *) ops_error PLATFORM_STAGE_DISCARD_UNCONFIRMED 75 ;; esac
    done
    [ "$OPS_EXECUTOR" = "$OPS_CURRENT/owner.json" ] || ops_error OWNER_CHANGED
    owner="$(jq -c .owner "$OPS_EXECUTOR")" || ops_error OWNER_CHANGED
    broray_ops_classify_owner "$owner"
    [ "$OPS_OWNER_STATUS:$OPS_OWNER_REASON" = STALE:absent ] || ops_error OWNER_CHANGED
    running="$(jq -r .running "$OPS_CURRENT/state.json")"; replay=false
    if ops_global_matches; then
        [ ! -e "$OPS_CURRENT/retired-lock" ] && [ ! -L "$OPS_CURRENT/retired-lock" ] || ops_error OWNER_CHANGED
    else
        [ "$running" = false ] && [ ! -e "$OPS_GLOBAL" ] && [ ! -L "$OPS_GLOBAL" ] &&
          [ -L "$OPS_CURRENT/retired-lock" ] || ops_error OWNER_CHANGED
        previous="$OPS_GLOBAL"; OPS_GLOBAL="$OPS_CURRENT/retired-lock"
        rc=0; ops_global_matches || rc=$?; OPS_GLOBAL="$previous"
        [ "$rc" = 0 ] || ops_error OWNER_CHANGED
        replay=true
    fi
    [ ! -e "$OPS_LEGACY" ] && [ ! -L "$OPS_LEGACY" ] || ops_error LEGACY_OPERATION_BUSY 75
    ops_publication_ready && ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    saved="$(jq -c .service "$OPS_CURRENT/platform-service.json")" || ops_error STATE_UNAVAILABLE 1
    current="$(ops_platform_service_capture)" || ops_error UPDATER_SERVICE_UNCONFIRMED
    jq -en --argjson a "$saved" --argjson b "$current" '$a==$b' >/dev/null || ops_error UPDATER_SERVICE_CHANGED
    stage="$OPS_CURRENT/platform-migration"
    ops_dir_safe "$stage" && ops_publication_private "$stage/intent.record" 32768 &&
      ops_publication_private "$stage/staged.receipt" 4096 || ops_error PLATFORM_STAGE_DISCARD_UNCONFIRMED 75
    payload="$(sed -n '4p' "$stage/intent.record")"
    expected="$(jq -r .platformPreflight.expectedPlatformManifestSha256 "$OPS_CURRENT/state.json")"
    desired="$(printf '%s\n' "$saved" | jq -r 'if .owner==null then "stopped" else "running" end')"
    case "$OPS_APP" in */opt/broray) live="${OPS_APP%/opt/broray}" ;; *) ops_error UNSAFE_STATE 1 ;; esac
    [ -n "$live" ] || live=/
    generation="${BRORAY_OPS_GENERATION:-$OPS_CODE/bin/broray-updater-generation}"
    ops_file_safe "$generation" 16777216 && [ -x "$generation" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    # Read-only native verification requires every original record, exact
    # payload/before-inventory/modes, operation/nonce and the SAME boot.
    proof="$("$generation" migration-check-staged "$stage" "$live" "$payload" "$expected" "$OPS_ID" "$nonce" "$desired")" || ops_error PLATFORM_STAGE_DISCARD_UNCONFIRMED 75
    printf '%s\n' "$proof" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="STAGED_ONLY_VERIFIED" and
      .[0].oldBootId==.[0].currentBootId and .[0].activationAllowed==false and .[0].serviceStopped==false and
      (.[0].intentSha256|type=="string" and length==64 and all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102)))' >/dev/null || ops_error PLATFORM_STAGE_DISCARD_UNCONFIRMED 75
    current="$(ops_platform_service_capture)" || ops_error UPDATER_SERVICE_UNCONFIRMED
    jq -en --argjson a "$saved" --argjson b "$current" '$a==$b' >/dev/null || ops_error UPDATER_SERVICE_CHANGED
    if [ "$running" = true ]; then
        ops_state_transition aborted finished PREFLIGHT_STAGING_ABORTED || ops_error STATE_UNAVAILABLE 1
        ops_launch_test_point platform-staging-aborted
    fi
    if ops_global_matches; then ops_retire_global || ops_error STATE_UNAVAILABLE 1; fi
    "$OPS_GUARD" --sync-state "$OPS_GLOBAL" && "$OPS_GUARD" --sync-state "$OPS_CURRENT/state.json" || ops_error STATE_UNAVAILABLE 1
    ops_launch_test_point platform-staging-retired
    printf '%s\n' "$proof" | jq -c --arg id "$OPS_ID" --argjson replay "$replay" \
      '.+{phase:"PREFLIGHT_STAGING_ABORTED",operationId:$id,platformReady:false,signalsAuthorized:false,replayed:$replay}'
}

ops_platform_queue_clear()
{
    local queue file
    queue="$OPS_UPDATER/queue"
    [ ! -e "$queue" ] && [ ! -L "$queue" ] && return 0
    ops_dir_safe "$queue" || return 1
    for file in "$queue"/* "$queue"/.[!.]* "$queue"/..?*; do
        [ ! -e "$file" ] && [ ! -L "$file" ] || return 1
    done
}

ops_platform_admission_request()
{
    [ "$#" = 3 ] || ops_error INVALID_REQUEST 1
    OPS_PREFLIGHT_EXPECTED_SHA="$1"
    ops_platform_sha_valid "$OPS_PREFLIGHT_EXPECTED_SHA" || ops_error INVALID_PLATFORM_MANIFEST 1
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    ops_begin system system:platform-preflight updater-platform UPDATER "$2" protected "$3"
}

# Step 04: durable intent and one bounded, protected stop-helper tree.
# Domain completion/rollback is intentionally NOT inferred from helper exit.
ops_platform_stop_valid()
{
    local platform_phase
    platform_phase="${1:-STOP_INTENT}"
    case "$platform_phase" in STOP_INTENT|STOPPED) ;; *) return 1 ;; esac
    ops_file_safe "$OPS_CURRENT/state.json" || return 1
    jq -e --arg id "$OPS_ID" --arg phase "$platform_phase" '
      .schemaVersion==2 and .operationId==$id and .kind=="background" and
      .operation=="system:platform-preflight" and .scope=="system" and
      .source=="UPDATER" and .bundleId=="updater-platform" and
      .cancelability=="protected" and .initialCancelability=="protected" and
      .acknowledged==true and
      (.platformPreflight | type=="object" and
        (keys==["contract","expectedPlatformManifestSha256","mutationStarted","phase","schemaVersion","stopNonce"] or
         (keys==["contract","expectedPlatformManifestSha256","generationStop","mutationStarted","phase","schemaVersion","stopNonce"] and
          (.generationStop|type=="object" and keys==["contract","generationId","nativeSha256","platformFiles","platformManifestSha256","supervisor","updater"] and
           .contract=="broray-platform-generation-stop/1" and (.generationId|type)=="string" and
           (.nativeSha256|type)=="string" and (.platformManifestSha256|type)=="string" and
           (.platformFiles|type)=="array" and (.platformFiles|length)==7 and
           (.supervisor|type)=="object" and (.updater|type)=="object"))) and
        .schemaVersion==1 and .contract=="broray-platform-preflight/1" and
        .phase==$phase and .mutationStarted==true and
        (.stopNonce|type)=="string" and (.stopNonce|length)==32 and
        (.stopNonce|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102)))) and
      (.platformPreflight.expectedPlatformManifestSha256|type)=="string" and
      (.platformPreflight.expectedPlatformManifestSha256|length)==64 and
      (.platformPreflight.expectedPlatformManifestSha256|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102)))
    ' "$OPS_CURRENT/state.json" >/dev/null 2>&1
}

ops_platform_stop_intent()
{
    local record nonce expected
    [ "$#" = 5 ] || ops_error INVALID_REQUEST 1
    ops_authorize "$1" "$2"; ops_owner_authorize "$3"
    ops_global_matches || ops_error OWNER_CHANGED
    expected="$4"; nonce="$5"
    ops_platform_sha_valid "$expected" && ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    jq -e --arg sha "$expected" '.platformPreflight.expectedPlatformManifestSha256==$sha' \
      "$OPS_CURRENT/state.json" >/dev/null || ops_error PLATFORM_MANIFEST_CHANGED
    ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
    # Lost replies are retried with the SAME nonce, without repeating mutation.
    if ops_platform_stop_valid; then
        jq -e --arg nonce "$nonce" '.platformPreflight.stopNonce==$nonce' \
          "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
        "$OPS_GUARD" --sync-state "$OPS_CURRENT/state.json" || ops_error STATE_UNAVAILABLE 1
        printf '%s\n' '{"ok":true,"phase":"STOP_INTENT","mutationIntentDurable":true}'
        return 0
    fi
    ops_platform_prepared_valid || ops_error PLATFORM_PHASE_INVALID
    jq -e '.acknowledged==true' "$OPS_CURRENT/state.json" >/dev/null || ops_error NOT_ACKNOWLEDGED
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    [ ! -e "$OPS_CURRENT/platform-stop-supervision.json" ] && \
      [ ! -L "$OPS_CURRENT/platform-stop-supervision.json" ] || ops_error OPERATION_EXISTS
    record="$(jq -c --arg nonce "$nonce" --arg now "$(ops_now)" '
      .platformPreflight.phase="STOP_INTENT" | .platformPreflight.mutationStarted=true |
      .platformPreflight.stopNonce=$nonce | .revision+=1 | .updatedAt=$now
    ' "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 1
    ops_write "$OPS_CURRENT/state.json" "$record" || ops_error STATE_UNAVAILABLE 1
    ops_event phase_changed >/dev/null 2>&1 || true
    printf '%s\n' '{"ok":true,"phase":"STOP_INTENT","mutationIntentDurable":true}'
}
