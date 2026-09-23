#!/opt/bin/ash
# Separate authorized BOOT_GUARD_STAGING exception for two legacy entry points.
# No signal, STOPPED, reboot, app activation, route capture or queue publication.
ops_platform_runtime_retain()
{
    local generation sha mode store entry response
    generation="$1"; sha="$2"; mode="$3"
    store="$OPS_UPDATER/runtimes"; entry="$store/$sha"
    ops_platform_service_path_safe "$OPS_UPDATER" || ops_error UNSAFE_STATE 1
    if [ ! -e "$OPS_UPDATER" ] && [ ! -L "$OPS_UPDATER" ]; then
        [ "$mode" = fresh ] || ops_error UPDATER_RUNTIME_RETENTION_UNCONFIRMED 75
        mkdir -m 0700 "$OPS_UPDATER" || ops_error STATE_UNAVAILABLE 1
    fi
    ops_dir_safe "$OPS_UPDATER" || ops_error UNSAFE_STATE 1
    if [ ! -e "$store" ] && [ ! -L "$store" ]; then
        [ "$mode" = fresh ] || ops_error UPDATER_RUNTIME_RETENTION_UNCONFIRMED 75
        mkdir -m 0700 "$store" || ops_error STATE_UNAVAILABLE 1
    fi
    ops_dir_safe "$store" || ops_error UNSAFE_STATE 1
    if [ "$mode" = replay ]; then
        # A bound operation already passed retention before changing entries.
        # Missing permanent bytes must never be reconstructed by a replay.
        ops_dir_safe "$entry" && ops_file_safe "$entry/runtime" 16777216 &&
          ops_file_safe "$entry/identity.json" 4096 || ops_error UPDATER_RUNTIME_RETENTION_UNCONFIRMED 75
    fi
    if [ "$mode" = fresh ]; then
        response="$("$generation" runtime-retain "$store" "$sha")" || ops_error UPDATER_RUNTIME_RETENTION_UNCONFIRMED 75
    else
        response="$("$generation" runtime-verify "$store" "$sha")" || ops_error UPDATER_RUNTIME_RETENTION_UNCONFIRMED 75
    fi
    printf '%s\n' "$response" | jq -es --arg sha "$sha" --arg path "$entry/runtime" '
      length==1 and .[0].ok==true and .[0].runtimeSha256==$sha and
      .[0].runtimePath==$path and .[0].processAuthority==false' >/dev/null || ops_error UPDATER_RUNTIME_RETENTION_UNCONFIRMED 75
}

ops_platform_legacy_control_stage()
{
    local generation live stage migration_sha service_sha owner response
    generation="$1"; live="$2"; stage="$3"; migration_sha="$4"
    ops_platform_service_record_valid || ops_error UPDATER_SERVICE_UNCONFIRMED 75
    service_sha="$(sha256sum "$OPS_CURRENT/platform-service.json")" || ops_error UPDATER_SERVICE_UNCONFIRMED 75
    service_sha="${service_sha%% *}"
    owner="$(jq -c .service.owner "$OPS_CURRENT/platform-service.json")" || ops_error UPDATER_SERVICE_UNCONFIRMED 75
    response="$("$generation" legacy-control-stage "$OPS_CURRENT" "$live" "$stage" "$migration_sha" "$service_sha" "$owner")" || ops_error LEGACY_CONTROL_EVIDENCE_UNCONFIRMED 75
    printf '%s\n' "$response" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="LEGACY_CONTROL_STAGED" and
      .[0].signalsAuthorized==false and .[0].serviceStopped==false and .[0].activationAllowed==false and
      (.[0].snapshotSha256|type)=="string" and (.[0].snapshotSha256|(type=="string" and length==64 and all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))))' >/dev/null || ops_error LEGACY_CONTROL_EVIDENCE_UNCONFIRMED 75
}

ops_platform_recovery_code_retain()
{
    local generation live stage migration_sha mode phase response code
    generation="$1"; live="$2"; stage="$3"; migration_sha="$4"; mode="$5"
    code="$OPS_CURRENT/platform-recovery-code/code"
    if [ "$mode" = fresh ]; then
        ops_platform_service_path_safe "$OPS_CODE" || ops_error UNSAFE_STATE 1
        response="$("$generation" recovery-code-stage "$OPS_CURRENT" "$live" "$stage" "$migration_sha" "$OPS_CODE")" || ops_error RECOVERY_CODE_EVIDENCE_UNCONFIRMED 75
        phase=RECOVERY_CODE_STAGED
    else
        response="$("$generation" recovery-code-verify "$OPS_CURRENT" "$live" "$stage" "$migration_sha")" || ops_error RECOVERY_CODE_EVIDENCE_UNCONFIRMED 75
        phase=RECOVERY_CODE_VERIFIED
    fi
    printf '%s\n' "$response" | jq -es --arg phase "$phase" --arg code "$code" '
      length==1 and .[0].ok==true and .[0].phase==$phase and .[0].codeRoot==$code and
      .[0].processAuthority==false and .[0].activationAllowed==false and
      (.[0].codeManifestSha256|type)=="string" and (.[0].codeManifestSha256|(type=="string" and length==64 and all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))))' >/dev/null || ops_error RECOVERY_CODE_EVIDENCE_UNCONFIRMED 75
}

ops_platform_bootguard_stage()
{
    local nonce expected generation native_sha live stage guards binding migration_sha response
    [ "$#" = 4 ] || ops_error INVALID_REQUEST 1
    ops_authorize "$1" "$2"; ops_owner_authorize "$3"; ops_global_matches || ops_error OWNER_CHANGED
    ops_platform_stop_valid && ops_platform_service_record_valid || ops_error PLATFORM_PHASE_INVALID
    nonce="$4"; ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    jq -e --arg nonce "$nonce" '.platformPreflight.stopNonce==$nonce and .platformPreflight.generationStop==null' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    [ ! -e "$OPS_UPDATER/request.lock" ] && [ ! -L "$OPS_UPDATER/request.lock" ] || ops_error DOMAIN_OPERATION_BUSY
    [ ! -e "$OPS_CURRENT/platform-stop-supervision.json" ] && [ ! -L "$OPS_CURRENT/platform-stop-supervision.json" ] || ops_error CHILDREN_UNCONFIRMED
    generation="${BRORAY_OPS_GENERATION:-$OPS_CODE/bin/broray-updater-generation}"
    ops_file_safe "$generation" 16777216 && [ -x "$generation" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    [ "$("$generation" --version)" = 'broray-updater-generation/2 supervised-from-birth syscall-containment' ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    native_sha="$(sha256sum "$generation")" || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    native_sha="${native_sha%% *}"
    expected="$(jq -r .platformPreflight.expectedPlatformManifestSha256 "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 1
    case "$OPS_APP" in */opt/broray) live="${OPS_APP%/opt/broray}" ;; *) ops_error UNSAFE_STATE 1 ;; esac
    [ -n "$live" ] || live=/
    stage="$OPS_CURRENT/platform-migration"; guards="$OPS_CURRENT/platform-bootguard"; binding="$OPS_CURRENT/platform-bootguard.json"
    if [ ! -e "$binding" ] && [ ! -L "$binding" ]; then
        # A missing binding after partial preparation is not a fresh request.
        [ ! -e "$guards" ] && [ ! -L "$guards" ] || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
        response="$(ops_platform_migration_stage "$@")" || ops_error MIGRATION_PREPARATION_UNCONFIRMED 75
        migration_sha="$(printf '%s\n' "$response" | jq -er .intentSha256)" || ops_error MIGRATION_PREPARATION_UNCONFIRMED 75
        ops_platform_sha_valid "$migration_sha" || ops_error MIGRATION_PREPARATION_UNCONFIRMED 75
        ops_platform_runtime_retain "$generation" "$native_sha" fresh
        mkdir -m 0700 "$guards" || ops_error STATE_UNAVAILABLE 1
        "$generation" guard-bind "$OPS_CURRENT" "$live" "$stage" "$migration_sha" >/dev/null || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
        # Only this first, current invocation may create the recovery closure.
        # An existing guard binding without it is incomplete evidence on replay.
        ops_platform_recovery_code_retain "$generation" "$live" "$stage" "$migration_sha" fresh
    fi
    ops_dir_safe "$guards" && ops_file_safe "$binding" 4096 || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
    jq -e --arg id "$OPS_ID" --arg nonce "$nonce" --arg expected "$expected" --arg native "$native_sha" '
      keys==["activationAllowed","contract","expectedPlatformManifestSha256","migrationIntentSha256","nativeSha256","operationId","schemaVersion","signalsAuthorized","stopNonce"] and
      .schemaVersion==1 and .contract=="broray-platform-bootguard/1" and .operationId==$id and .stopNonce==$nonce and
      .expectedPlatformManifestSha256==$expected and .nativeSha256==$native and .signalsAuthorized==false and .activationAllowed==false and
      (.migrationIntentSha256|type)=="string" and (.migrationIntentSha256|(type=="string" and length==64 and all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))))' "$binding" >/dev/null || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
    migration_sha="$(jq -r .migrationIntentSha256 "$binding")" || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
    ops_platform_runtime_retain "$generation" "$native_sha" replay
    # Exact native write-once replay fsyncs the binding again. Unknown bytes
    # cannot be replaced, and live platform mutation starts only after success.
    "$generation" guard-bind "$OPS_CURRENT" "$live" "$stage" "$migration_sha" >/dev/null || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
    ops_platform_recovery_code_retain "$generation" "$live" "$stage" "$migration_sha" replay
    ops_platform_legacy_control_stage "$generation" "$live" "$stage" "$migration_sha"
    response="$("$generation" guard-stage-bound "$guards" "$live" "$stage" "$migration_sha")" || ops_error BOOT_GUARD_PREPARATION_UNCONFIRMED 75
    printf '%s\n' "$response" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="BOOT_GUARDS_STAGED" and .[0].serviceStopped==false and .[0].activationAllowed==false and .[0].oldBootEnded==false and .[0].oldBootId==.[0].currentBootId' >/dev/null || ops_error BOOT_GUARD_PREPARATION_UNCONFIRMED 75
    ops_global_matches || ops_error OWNER_CHANGED
    ops_platform_recovery_code_retain "$generation" "$live" "$stage" "$migration_sha" replay
    ops_platform_legacy_control_stage "$generation" "$live" "$stage" "$migration_sha"
    printf '%s\n' "$response" | jq -c --arg id "$OPS_ID" '.+{operationId:$id,platformReady:false,signalsAuthorized:false}'
}

# Observation only. Admission must repeat all checks while serialized; this
# response is not a capability to mutate or transfer an executor afterwards.
ops_platform_boot_context()
{
    ops_platform_context boot "$@"
}

ops_platform_install_context()
{
    ops_platform_context install "$@"
}

ops_platform_context()
{
    local context guard_verb guard_phase context_phase nonce expected binding native_sha migration_sha live generation response proof control service_sha owner file
    context="$1"; shift
    case "$context" in
      boot) guard_verb=guard-verify-bound; guard_phase=BOOT_GUARDS_VERIFIED; context_phase=BOOT_CONTEXT_VERIFIED ;;
      install) guard_verb=guard-evidence-bound; guard_phase=BOOT_GUARD_EVIDENCE_VERIFIED; context_phase=INSTALL_CONTEXT_VERIFIED ;;
      complete) guard_verb=guard-evidence-bound; guard_phase=BOOT_GUARD_EVIDENCE_VERIFIED; context_phase=COMPLETE_CONTEXT_VERIFIED ;;
      *) ops_error INVALID_REQUEST 1 ;;
    esac
    [ "$#" = 2 ] || ops_error INVALID_REQUEST 1
    ops_load "$1" || ops_error STATE_UNAVAILABLE 1
    nonce="$2"; ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    [ "$OPS_EXECUTOR" = "$OPS_CURRENT/owner.json" ] || ops_error OWNER_CHANGED
    ops_platform_stop_valid && ops_platform_service_record_valid || ops_error PLATFORM_PHASE_INVALID
    jq -e --arg nonce "$nonce" '.platformPreflight.stopNonce==$nonce and .platformPreflight.generationStop==null' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    if [ "$context" = complete ]; then
        ops_platform_completion_fence || ops_error OWNER_CHANGED
    else
        jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null && ops_global_matches || ops_error OWNER_CHANGED
    fi
    ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
    # Never call ops_children_absent here: it collects/changes old registries.
    # This initial legacy staging path admits no stop supervisor or child job.
    for file in children.json platform-stop-supervision.json; do
        [ ! -e "$OPS_CURRENT/$file" ] && [ ! -L "$OPS_CURRENT/$file" ] || ops_error CHILDREN_UNCONFIRMED
    done
    file="$OPS_CURRENT/supervisors.json"
    if [ -e "$file" ] || [ -L "$file" ]; then
        ops_file_safe "$file" 131072 && jq -e 'keys==["schemaVersion","supervisors"] and .schemaVersion==1 and .supervisors==[]' "$file" >/dev/null || ops_error CHILDREN_UNCONFIRMED
    fi
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    expected="$(jq -r .platformPreflight.expectedPlatformManifestSha256 "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 1
    binding="$OPS_CURRENT/platform-bootguard.json"
    ops_file_safe "$binding" 4096 && jq -e --arg id "$OPS_ID" --arg nonce "$nonce" --arg expected "$expected" '
      keys==["activationAllowed","contract","expectedPlatformManifestSha256","migrationIntentSha256","nativeSha256","operationId","schemaVersion","signalsAuthorized","stopNonce"] and
      .schemaVersion==1 and .contract=="broray-platform-bootguard/1" and .operationId==$id and .stopNonce==$nonce and
      .expectedPlatformManifestSha256==$expected and .signalsAuthorized==false and .activationAllowed==false and
      (.migrationIntentSha256|type)=="string" and (.migrationIntentSha256|(type=="string" and length==64 and all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102)))) and
      (.nativeSha256|type)=="string" and (.nativeSha256|(type=="string" and length==64 and all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))))' "$binding" >/dev/null || ops_error BOOT_GUARD_BINDING_UNCONFIRMED 75
    native_sha="$(jq -r .nativeSha256 "$binding")"; migration_sha="$(jq -r .migrationIntentSha256 "$binding")"
    generation="$OPS_UPDATER/runtimes/$native_sha/runtime"
    ops_platform_service_path_safe "$generation" && ops_file_safe "$generation" 16777216 && [ -x "$generation" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    response="$(sha256sum "$generation")" || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    [ "${response%% *}" = "$native_sha" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    ops_platform_runtime_retain "$generation" "$native_sha" replay
    case "$OPS_APP" in */opt/broray) live="${OPS_APP%/opt/broray}" ;; *) ops_error UNSAFE_STATE 1 ;; esac
    [ -n "$live" ] || live=/
    proof="$("$generation" "$guard_verb" "$OPS_CURRENT/platform-bootguard" "$live" "$OPS_CURRENT/platform-migration" "$migration_sha")" || ops_error BOOT_GUARD_PREPARATION_UNCONFIRMED 75
    printf '%s\n' "$proof" | jq -es --arg phase "$guard_phase" 'length==1 and .[0].ok==true and .[0].phase==$phase and .[0].serviceStopped==false and .[0].activationAllowed==false and (.[0].oldBootEnded|type)=="boolean"' >/dev/null || ops_error BOOT_GUARD_PREPARATION_UNCONFIRMED 75
    ops_platform_recovery_code_retain "$generation" "$live" "$OPS_CURRENT/platform-migration" "$migration_sha" replay
    service_sha="$(sha256sum "$OPS_CURRENT/platform-service.json")" || ops_error UPDATER_SERVICE_UNCONFIRMED 75
    service_sha="${service_sha%% *}"; owner="$(jq -c .service.owner "$OPS_CURRENT/platform-service.json")" || ops_error UPDATER_SERVICE_UNCONFIRMED 75
    if [ "$owner" != null ]; then printf '%s\n' "$owner" | broray_ops_owner_valid || ops_error UPDATER_SERVICE_UNCONFIRMED 75; fi
    control="$("$generation" legacy-control-verify "$OPS_CURRENT" "$live" "$OPS_CURRENT/platform-migration" "$migration_sha" "$service_sha" "$owner")" || ops_error LEGACY_CONTROL_EVIDENCE_UNCONFIRMED 75
    printf '%s\n' "$control" | jq -es --argjson proof "$proof" 'length==1 and .[0].ok==true and .[0].phase=="LEGACY_CONTROL_VERIFIED" and
      .[0].signalsAuthorized==false and .[0].serviceStopped==false and .[0].activationAllowed==false and
      .[0].oldBootId==$proof.oldBootId and .[0].currentBootId==$proof.currentBootId and .[0].oldBootEnded==$proof.oldBootEnded' >/dev/null || ops_error LEGACY_CONTROL_EVIDENCE_UNCONFIRMED 75
    jq -e --argjson proof "$proof" '.owner.bootId==$proof.oldBootId' "$OPS_CURRENT/owner.json" >/dev/null || ops_error OWNER_CHANGED
    if [ "$context" = complete ]; then ops_platform_completion_fence || ops_error OWNER_CHANGED
    else ops_global_matches || ops_error OWNER_CHANGED; fi
    printf '%s\n' "$proof" | jq -c --arg id "$OPS_ID" --arg phase "$context_phase" '{ok:true,phase:$phase,operationId:$id,oldBootId:.oldBootId,currentBootId:.currentBootId,oldBootEnded:.oldBootEnded,serviceStopped:false,signalsAuthorized:false,activationAllowed:false,executorAuthorized:false,platformReady:false}'
}
