#!/opt/bin/ash
# Protected coordinator -> existing lifetime generation. Never start/adopt a
# daemon here. Legacy observation-only service records cannot enter this path.

ops_platform_generation_files()
{
    local rows row path mode digest
    rows="$(ops_platform_service_inventory)" || return 1
    printf '%s\n' "$rows" | jq -e 'length==7 and all(.[];.value!=null and .value.executable==true)' >/dev/null || return 1
    for row in $(printf '%s\n' "$rows" | jq -r '.[].path'); do
        path="${OPS_APP%/broray}/$row"
        mode="$(stat -c '%a' "$path")" || return 1
        [ "$mode" = 755 ] || return 1
    done
    digest="$(printf '%s\n' "$rows" | jq -r 'sort_by(.path)[]|.value.sha256+"  opt/"+.path' | sha256sum)" || return 1
    digest="${digest%% *}"
    [ "$digest" = "$PG_MANIFEST" ] || return 1
    PG_FILES="$rows"
}

ops_platform_generation_status()
{
    PG_STATUS="$("$PG_NATIVE" control "$PG_DIR" STATUS "$PG_ID" "$PG_MANIFEST" "$OPS_ID" "$PG_NONCE")" || return 1
    printf '%s\n' "$PG_STATUS" | jq -e --arg id "$PG_ID" --arg sha "$PG_MANIFEST" '
      .schemaVersion==2 and .contract=="broray-updater-generation/2" and
      .generationId==$id and .platformManifestSha256==$sha and
      .supervisedFromBirth==true and (.supervisor|type)=="object" and
      (.updater|type)=="object" and (.revision|type)=="number" and
      (.children|type)=="array" and (.awaitingBirth|type)=="array" and
      (.exitedUnreaped|type)=="array"' >/dev/null || return 1
    printf '%s\n' "$PG_STATUS" | jq -c .supervisor | broray_ops_owner_valid || return 1
    printf '%s\n' "$PG_STATUS" | jq -c .updater | broray_ops_owner_valid || return 1
}

ops_platform_generation_stop()
{
    local PG_ID PG_MANIFEST PG_NONCE PG_NATIVE PG_DIR PG_FILES PG_STATUS
    local native_hash target saved service next reply terminal expected file bound_owner
    [ "$#" = 6 ] || ops_error INVALID_REQUEST 1
    ops_authorize "$1" "$2"; ops_owner_authorize "$3"
    ops_global_matches || ops_error OWNER_CHANGED
    { ops_platform_stop_valid || ops_platform_stop_valid STOPPED; } || ops_error PLATFORM_PHASE_INVALID
    PG_NONCE="$4"; PG_ID="$5"; PG_MANIFEST="$6"
    ops_nonce_valid "$PG_NONCE" && ops_platform_sha_valid "$PG_MANIFEST" || ops_error INVALID_REQUEST 1
    case "$PG_ID" in ''|*[!A-Za-z0-9_-]*) ops_error INVALID_REQUEST 1 ;; esac
    [ "${#PG_ID}" -le 64 ] || ops_error INVALID_REQUEST 1
    jq -e --arg nonce "$PG_NONCE" '.platformPreflight.stopNonce==$nonce' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    for file in "$OPS_UPDATER/request.lock" "$OPS_CURRENT/platform-service.json" "$OPS_CURRENT/platform-stop-supervision.json"; do
        [ ! -e "$file" ] && [ ! -L "$file" ] || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    done
    [ "$OPS_PROC" = /proc ] && [ "$OPS_UPDATER" = "${OPS_APP%/broray}/var/lib/broray-updater" ] || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    PG_DIR="$OPS_UPDATER/generations/$PG_ID"
    ops_platform_service_path_safe "$PG_DIR" || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    PG_NATIVE="${BRORAY_OPS_GENERATION:-$OPS_CODE/bin/broray-updater-generation}"
    ops_file_safe "$PG_NATIVE" 16777216 && [ -x "$PG_NATIVE" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    [ "$("$PG_NATIVE" --version)" = 'broray-updater-generation/2 supervised-from-birth syscall-containment' ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    native_hash="$(sha256sum "$PG_NATIVE")" || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    native_hash="${native_hash%% *}"
    ops_platform_generation_files || ops_error PLATFORM_CHANGED 75
    ops_platform_generation_status || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    target="$(printf '%s\n' "$PG_STATUS" | jq -c --arg native "$native_hash" --argjson files "$PG_FILES" '
      {contract:"broray-platform-generation-stop/1",generationId:.generationId,
       platformManifestSha256:.platformManifestSha256,nativeSha256:$native,
       supervisor:.supervisor,updater:.updater,platformFiles:$files}')" || ops_error STATE_UNAVAILABLE 1
    saved="$(jq -c '.platformPreflight.generationStop // null' "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 1
    if [ "$saved" = null ]; then
        ops_platform_stop_valid || ops_error PLATFORM_PHASE_INVALID
        printf '%s\n' "$PG_STATUS" | jq -e '.state=="RUNNING" and .stopOperationId=="" and .stopNonce==""' >/dev/null || ops_error UPDATER_GENERATION_UNCONFIRMED 75
        bound_owner=null
        if printf '%s\n' "$PG_STATUS" | jq -e '.platformLaunch!=null' >/dev/null; then
            # The authenticated generation, not a cmdline/PID projection,
            # binds the pipe-executed daemon to exact installed script bytes.
            printf '%s\n' "$PG_STATUS" | jq -e --arg native "$native_hash" --argjson files "$PG_FILES" '
              .platformReady==true and .platformLaunch.contract=="broray-platform-launch/1" and
              .platformLaunch.nativeSha256==$native and
              .platformLaunch.daemonSha256==($files[]|select(.path=="libexec/broray-updater/broray-updater.sh")|.value.sha256)' >/dev/null || ops_error UPDATER_SERVICE_UNCONFIRMED 75
            bound_owner="$(printf '%s\n' "$PG_STATUS" | jq -c .updater)" || ops_error UPDATER_SERVICE_UNCONFIRMED 75
        fi
        service="$(ops_platform_service_capture "$(printf '%s\n' "$PG_STATUS" | jq -c .supervisor)" "$bound_owner")" || ops_error UPDATER_SERVICE_UNCONFIRMED 75
        printf '%s\n' "$service" | jq -e --argjson status "$PG_STATUS" --argjson files "$PG_FILES" '
          .owner==$status.updater and .platformFiles==$files' >/dev/null || ops_error UPDATER_SERVICE_UNCONFIRMED 75
        # Full native identity is independently authenticated by the control
        # client. The old daemon.pid/readiness projections grant no authority.
        next="$(jq -c --argjson target "$target" --arg now "$(ops_now)" '
          .platformPreflight.generationStop=$target|.revision+=1|.updatedAt=$now' "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 1
        ops_write "$OPS_CURRENT/state.json" "$next" || ops_error STATE_UNAVAILABLE 1
    else
        jq -en --argjson saved "$saved" --argjson target "$target" '$saved==$target' >/dev/null || ops_error UPDATER_GENERATION_CHANGED 75
    fi
    # The exact target intent is durable before native STOP. Native keeps its
    # own immutable operation/nonce intent before any TERM and owns all writers
    # from birth. Repeated requests never infer identity from a PID or timeout.
    ops_platform_generation_files || ops_error PLATFORM_CHANGED 75
    reply="$("$PG_NATIVE" control "$PG_DIR" STOP "$PG_ID" "$PG_MANIFEST" "$OPS_ID" "$PG_NONCE")" || ops_error UPDATER_STOP_UNCONFIRMED 75
    printf '%s\n' "$reply" | jq -e --argjson target "$target" --arg op "$OPS_ID" --arg nonce "$PG_NONCE" '
      .schemaVersion==2 and .contract=="broray-updater-generation/2" and .supervisedFromBirth==true and
      .generationId==$target.generationId and .platformManifestSha256==$target.platformManifestSha256 and
      .supervisor==$target.supervisor and .updater==$target.updater and .stopOperationId==$op and .stopNonce==$nonce and
      (.state=="STOP_INTENT" or .state=="STOPPING" or .state=="DRAINING" or .state=="STOPPED")' >/dev/null || ops_error UPDATER_STOP_UNCONFIRMED 75
    ops_platform_generation_files || ops_error PLATFORM_CHANGED 75
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    terminal="$(printf '%s\n' "$reply" | jq -r '.state=="STOPPED" and (.children|length)==0 and (.awaitingBirth|length)==0 and (.exitedUnreaped|length)==0')" || ops_error UPDATER_STOP_UNCONFIRMED 75
    if [ "$terminal" != true ]; then
        ops_platform_stop_valid || ops_error UPDATER_STOP_UNCONFIRMED 75
        printf '%s\n' '{"ok":true,"phase":"STOPPING","serviceStopped":false,"platformReady":false}'
        return 0
    fi
    if ops_platform_stop_valid STOPPED; then
        "$OPS_GUARD" --sync-state "$OPS_CURRENT/state.json" || ops_error STATE_UNAVAILABLE 1
    else
        next="$(jq -c --arg now "$(ops_now)" '.platformPreflight.phase="STOPPED"|.revision+=1|.updatedAt=$now' "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 1
        ops_write "$OPS_CURRENT/state.json" "$next" || ops_error STATE_UNAVAILABLE 1
        ops_event phase_changed >/dev/null 2>&1 || true
    fi
    printf '%s\n' '{"ok":true,"phase":"STOPPED","serviceStopped":true,"platformReady":false}'
}

# Installed init enters through the retained native closure and the existing
# coordinator guard. The completed launch operation is read-only provenance;
# the requested stop receives a fresh canonical operation and durable purpose.
ops_platform_service_stop()
{
    local origin migration original_nonce binding native native_sha actual live manifest proof generation
    local pid rest launch response id token nonce attempt file replay_id current_generation
    [ "$#" = 3 ] || ops_error INVALID_REQUEST 1
    origin="$1"; migration="$2"; original_nonce="$3"
    ops_id_valid "$origin" && ops_platform_sha_valid "$migration" && ops_nonce_valid "$original_nonce" || ops_error INVALID_REQUEST 1
    [ "$OPS_PROC" = /proc ] || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    ops_load "$origin" || ops_error STATE_UNAVAILABLE 1
    jq -e --arg nonce "$original_nonce" '.state=="completed" and .running==false and
      .platformPreflight.stopNonce==$nonce and (has("serviceStop")|not)' "$OPS_CURRENT/state.json" >/dev/null || ops_error PLATFORM_PHASE_INVALID
    binding="$OPS_CURRENT/platform-bootguard.json"; native="$OPS_CURRENT/platform-bootguard/runtime"
    ops_publication_private "$binding" 4096 && ops_file_safe "$native" 16777216 &&
      ops_platform_service_path_safe "$native" && [ -x "$native" ] &&
      [ "$(stat -c '%u:%a:%h' "$native")" = "$(id -u):700:1" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    jq -e --arg id "$origin" --arg sha "$migration" --arg nonce "$original_nonce" '
      .schemaVersion==1 and .contract=="broray-platform-bootguard/1" and .operationId==$id and
      .migrationIntentSha256==$sha and .stopNonce==$nonce' "$binding" >/dev/null || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
    native_sha="$(jq -er .nativeSha256 "$binding")"; ops_platform_sha_valid "$native_sha" || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    actual="$(sha256sum "$native")" || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    [ "${actual%% *}" = "$native_sha" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    case "$OPS_APP" in */opt/broray) live="${OPS_APP%/opt/broray}" ;; *) ops_error UNSAFE_STATE 1 ;; esac
    [ -n "$live" ] || live=/
    manifest="$(jq -er .platformPreflight.expectedPlatformManifestSha256 "$OPS_CURRENT/state.json")"
    ops_platform_sha_valid "$manifest" || ops_error INVALID_PLATFORM_MANIFEST 1
    # Repeated stop is a read-only replay of one exact completed operation.
    # Do not demand live readiness or create a new owner after it has stopped.
    # Read-only discovery from the sealed ordinary lifecycle selects the one
    # current generation. It grants no signal/readiness authority. Completed
    # earlier stops stay in the history and are validated before filtering.
    current_generation=''
    if [ -e "$OPS_UPDATER/cycles" ] || [ -L "$OPS_UPDATER/cycles" ]; then
        proof="$("$native" service-cycle-current "$live" "$origin" "$migration" "$original_nonce")" || ops_error UPDATER_SERVICE_BINDING_UNCONFIRMED 75
        current_generation="$(printf '%s\n' "$proof" | jq -er 'select(.ok==true and .phase=="SERVICE_CURRENT_DISCOVERED" and .readinessProven==false and .activationAllowed==false) | .generationId | select(type=="string" and (length==24 and startswith("g-") and (.[2:]|all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==95 or .==45))))')" || ops_error UPDATER_SERVICE_BINDING_UNCONFIRMED 75
    fi
    replay_id=''
    for file in "$OPS_ROOT"/op-*/state.json; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        ops_file_safe "$file" && jq -e 'type=="object"' "$file" >/dev/null || ops_error STATE_UNAVAILABLE 1
        jq -e --arg origin "$origin" 'has("serviceStop") and .serviceStop.originOperationId==$origin' "$file" >/dev/null || continue
        jq -e --arg origin "$origin" --arg migration "$migration" --arg originalNonce "$original_nonce" \
          --arg manifest "$manifest" --arg native "$native_sha" '
          .operation=="system:platform-preflight" and
          ((.state=="completed" and .running==false) or (.state=="running" and .running==true)) and
          .platformPreflight.phase=="STOPPED" and
          .serviceStop=={schemaVersion:1,contract:"broray-service-stop/1",originOperationId:$origin,
            originMigrationIntentSha256:$migration,originStopNonce:$originalNonce,
            generationId:.platformPreflight.generationStop.generationId,
            platformManifestSha256:$manifest,nativeSha256:$native}' "$file" >/dev/null || ops_error UPDATER_SERVICE_BINDING_UNCONFIRMED 75
        if [ -n "$current_generation" ] && ! jq -e --arg generation "$current_generation" '.serviceStop.generationId==$generation' "$file" >/dev/null; then
            continue
        fi
        [ -z "$replay_id" ] || ops_error UPDATER_SERVICE_IDENTITY_AMBIGUOUS 75
        replay_id="${file%/state.json}"; replay_id="${replay_id##*/}"
        ops_id_valid "$replay_id" && [ "$replay_id" != "$origin" ] || ops_error UPDATER_SERVICE_BINDING_UNCONFIRMED 75
        nonce="$(jq -er .platformPreflight.stopNonce "$file")"; ops_nonce_valid "$nonce" || ops_error UPDATER_SERVICE_BINDING_UNCONFIRMED 75
    done
    if [ -n "$replay_id" ]; then
        ops_platform_completion_fence STOP_INTENT retired-origin || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
        ops_load "$replay_id" || ops_error STATE_UNAVAILABLE 1
        if jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null; then
            broray_ops_classify_owner "$(jq -c .owner "$OPS_EXECUTOR")"
            [ "$OPS_OWNER_STATUS" = STALE ] || ops_error OWNER_ACTIVE 75
        fi
        BRORAY_OPS_GENERATION="$native"; export BRORAY_OPS_GENERATION
        ops_platform_generation_stop_complete guard "$replay_id" "$nonce"
        return $?
    fi
    ops_platform_completion_fence || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    proof="$("$native" recovery-commit-check "$live" "$origin" "$migration" "$original_nonce")" || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    printf '%s\n' "$proof" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="COMMIT_VERIFIED" and
      .[0].platformReady==true and .[0].activationAllowed==false and
      (.[0].generationId|type=="string" and (length==24 and startswith("g-") and (.[2:]|all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==95 or .==45))))' >/dev/null || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    generation="$(printf '%s\n' "$proof" | jq -er .generationId)"
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    OPS_PREFLIGHT_SERVICE_STOP="$(jq -nc --arg origin "$origin" --arg migration "$migration" --arg originalNonce "$original_nonce" \
      --arg generation "$generation" --arg manifest "$manifest" --arg native "$native_sha" '
      {schemaVersion:1,contract:"broray-service-stop/1",originOperationId:$origin,
       originMigrationIntentSha256:$migration,originStopNonce:$originalNonce,
       generationId:$generation,platformManifestSha256:$manifest,nativeSha256:$native}')" || ops_error STATE_UNAVAILABLE 1
    IFS=' ' read -r pid rest </proc/self/stat || ops_error OWNER_UNCONFIRMED 1
    launch="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || ops_error RANDOM_UNAVAILABLE 1
    nonce="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || ops_error RANDOM_UNAVAILABLE 1
    ops_nonce_valid "$launch" && ops_nonce_valid "$nonce" || ops_error RANDOM_UNAVAILABLE 1
    # The purpose is part of the initially published state, never an afterthought
    # inferred from a dead owner or from the absence of an install record.
    verb=platform-preflight-begin
    response="$(ops_platform_admission_request "$manifest" "$pid" "$launch")" || { printf '%s\n' "$response"; return 75; }
    id="$(printf '%s\n' "$response" | jq -er .operationId)"; token="$(printf '%s\n' "$response" | jq -er .token)"
    ops_id_valid "$id" && ops_nonce_valid "$token" || ops_error STATE_UNAVAILABLE 1
    response="$(ops_ack "$id" "$token" "$pid")" || { printf '%s\n' "$response"; return 75; }
    response="$(ops_platform_stop_intent "$id" "$token" "$pid" "$manifest" "$nonce")" || { printf '%s\n' "$response"; return 75; }
    BRORAY_OPS_GENERATION="$native"; export BRORAY_OPS_GENERATION
    attempt=0
    while [ "$attempt" -lt 12 ]; do
        attempt=$((attempt+1))
        response="$(ops_platform_generation_stop "$id" "$token" "$pid" "$nonce" "$generation" "$manifest")" || { printf '%s\n' "$response"; return 75; }
        if printf '%s\n' "$response" | jq -e '.ok==true and .phase=="STOPPED" and .serviceStopped==true and .platformReady==false' >/dev/null; then
            ops_platform_generation_stop_complete guard "$id" "$nonce"
            return $?
        fi
        printf '%s\n' "$response" | jq -e '.ok==true and .phase=="STOPPING" and .serviceStopped==false and .platformReady==false' >/dev/null || ops_error UPDATER_STOP_UNCONFIRMED 75
        [ "$attempt" = 12 ] || sleep .2
    done
    ops_error UPDATER_STOP_UNCONFIRMED 75
}

# Explicit terminal settlement for a NEW operation stopping an installed
# generation. It never reopens the completed migration that launched it.
ops_platform_generation_stop_complete()
{
    local PG_ID PG_MANIFEST PG_NONCE PG_NATIVE PG_DIR PG_FILES PG_STATUS
    local target native_hash file live host host_sha shell shell_sha origin migration proof replay
    [ "$#" = 3 ] || ops_error INVALID_REQUEST 1
    case "$1" in guard|settle) ;; *) ops_error INVALID_REQUEST 1 ;; esac
    ops_load "$2" || ops_error STATE_UNAVAILABLE 1
    PG_NONCE="$3"; ops_nonce_valid "$PG_NONCE" || ops_error INVALID_REQUEST 1
    ops_platform_completion_fence STOPPED || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    jq -e --arg nonce "$PG_NONCE" '.platformPreflight.stopNonce==$nonce and
      (.platformPreflight.generationStop|type)=="object"' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    # This is service-stop settlement, never completion/abandonment of an
    # install transaction. Different target bytes or any additional domain
    # evidence must stay under the original protected fence.
    jq -e '.resourceLocks==["global"] and
      .platformPreflight.expectedPlatformManifestSha256==.platformPreflight.generationStop.platformManifestSha256' \
      "$OPS_CURRENT/state.json" >/dev/null || ops_error PLATFORM_TRANSACTION_PENDING 75
    for file in "$OPS_CURRENT"/* "$OPS_CURRENT"/.[!.]* "$OPS_CURRENT"/..?*; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        case "${file##*/}" in owner.json|state.json|fence|retired-lock) ;; *) ops_error PLATFORM_TRANSACTION_PENDING 75 ;; esac
    done
    ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    for file in "$OPS_LEGACY" "$OPS_UPDATER/request.lock" "$OPS_CURRENT/platform-service.json" "$OPS_CURRENT/platform-stop-supervision.json"; do
        [ ! -e "$file" ] && [ ! -L "$file" ] || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    done
    [ "$OPS_PROC" = /proc ] && [ "$OPS_UPDATER" = "${OPS_APP%/broray}/var/lib/broray-updater" ] || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    target="$(jq -c .platformPreflight.generationStop "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 1
    PG_ID="$(printf '%s\n' "$target" | jq -er .generationId)"; PG_MANIFEST="$(printf '%s\n' "$target" | jq -er .platformManifestSha256)"
    case "$PG_ID" in ''|*[!A-Za-z0-9_-]*) ops_error UPDATER_GENERATION_UNCONFIRMED 75 ;; esac
    [ "${#PG_ID}" -le 64 ] && ops_platform_sha_valid "$PG_MANIFEST" || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    PG_DIR="$OPS_UPDATER/generations/$PG_ID"; host="$OPS_UPDATER/hosts/$PG_ID"
    ops_platform_service_path_safe "$PG_DIR" && ops_platform_service_path_safe "$host" || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    PG_NATIVE="${BRORAY_OPS_GENERATION:-$OPS_CODE/bin/broray-updater-generation}"
    ops_file_safe "$PG_NATIVE" 16777216 && [ -x "$PG_NATIVE" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    native_hash="$(sha256sum "$PG_NATIVE")" || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    native_hash="${native_hash%% *}"
    printf '%s\n' "$target" | jq -e --arg sha "$native_hash" '.nativeSha256==$sha' >/dev/null || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    ops_platform_generation_files || ops_error PLATFORM_CHANGED 75
    printf '%s\n' "$target" | jq -e --argjson files "$PG_FILES" '.platformFiles==$files' >/dev/null || ops_error PLATFORM_CHANGED 75
    # Native RETIRE is exact-operation/nonce bound and idempotent. For a
    # retired generation it only verifies immutable terminal history.
    PG_STATUS="$("$PG_NATIVE" control "$PG_DIR" RETIRE "$PG_ID" "$PG_MANIFEST" "$OPS_ID" "$PG_NONCE")" || ops_error UPDATER_STOP_UNCONFIRMED 75
    printf '%s\n' "$PG_STATUS" | jq -e --argjson target "$target" --arg op "$OPS_ID" --arg nonce "$PG_NONCE" '
      .schemaVersion==2 and .contract=="broray-updater-generation/2" and .supervisedFromBirth==true and
      .state=="STOPPED" and .children==[] and .awaitingBirth==[] and .exitedUnreaped==[] and
      .generationId==$target.generationId and .platformManifestSha256==$target.platformManifestSha256 and
      .supervisor==$target.supervisor and .updater==$target.updater and .stopOperationId==$op and .stopNonce==$nonce and
      .platformReady==false and .platformLaunch.contract=="broray-platform-launch/1" and
      .platformLaunch.nativeSha256==$target.nativeSha256 and
      (.serviceHostRecordSha256|type=="string" and (type=="string" and length==64 and all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))))' >/dev/null || ops_error UPDATER_STOP_UNCONFIRMED 75
    host_sha="$(printf '%s\n' "$PG_STATUS" | jq -er .serviceHostRecordSha256)"
    origin="$(printf '%s\n' "$PG_STATUS" | jq -er .platformLaunch.operationId)"
    ops_id_valid "$origin" && [ "$origin" != "$OPS_ID" ] || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    ops_publication_private "$OPS_ROOT/$origin/platform-bootguard.json" 4096 || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
    migration="$(jq -er .migrationIntentSha256 "$OPS_ROOT/$origin/platform-bootguard.json")"
    ops_platform_sha_valid "$migration" || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
    ops_publication_private "$host/host.record" 65536 || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    shell="$(sed -n '7p' "$host/host.record")"; shell_sha="$(sed -n '8p' "$host/host.record")"
    ops_platform_sha_valid "$shell_sha" || ops_error UPDATER_GENERATION_UNCONFIRMED 75
    case "$OPS_APP" in */opt/broray) live="${OPS_APP%/opt/broray}" ;; *) ops_error UNSAFE_STATE 1 ;; esac
    [ -n "$live" ] || live=/
    if [ "$1" = guard ]; then
        exec "$PG_NATIVE" service-stop-guard "$host" "$PG_DIR" "$PG_ID" "$PG_MANIFEST" "$live" "$shell" "$shell_sha" "$host_sha" "$origin" "$migration" "$OPS_ID" "$PG_NONCE"
        ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    fi
    proof="$("$PG_NATIVE" service-stop-check "$host" "$PG_DIR" "$PG_ID" "$PG_MANIFEST" "$live" "$shell" "$shell_sha" "$host_sha" "$origin" "$migration" "$OPS_ID" "$PG_NONCE")" || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    printf '%s\n' "$proof" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="SERVICE_STOP_EXCLUSION_VERIFIED" and .[0].serviceStopped==true and .[0].platformReady==false' >/dev/null || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    ops_platform_generation_files || ops_error PLATFORM_CHANGED 75
    ops_platform_completion_fence STOPPED || ops_error OWNER_CHANGED
    replay=false
    if jq -e '.running==false' "$OPS_CURRENT/state.json" >/dev/null; then replay=true
    else ops_state_transition completed finished '' || ops_error STATE_UNAVAILABLE 1
    fi
    if ops_global_matches; then ops_retire_global || ops_error STATE_UNAVAILABLE 1; fi
    "$OPS_GUARD" --sync-state "$OPS_GLOBAL" && "$OPS_GUARD" --sync-state "$OPS_CURRENT/state.json" || ops_error STATE_UNAVAILABLE 1
    ops_platform_completion_fence STOPPED || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    "$PG_NATIVE" service-stop-check "$host" "$PG_DIR" "$PG_ID" "$PG_MANIFEST" "$live" "$shell" "$shell_sha" "$host_sha" "$origin" "$migration" "$OPS_ID" "$PG_NONCE" >/dev/null || ops_error PLATFORM_COMPLETION_UNCONFIRMED 75
    printf '%s\n' "$proof" | jq -c --argjson replay "$replay" '.phase="SERVICE_STOP_COMPLETED"|.replayed=$replay'
}
