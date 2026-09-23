#!/opt/bin/ash
# Read-only service evidence and guarded STOPPED confirmation. No signals.
ops_platform_service_path_safe()
{
    local p
    p="$1"
    case "$p" in /*) ;; *) return 1 ;; esac
    while [ "$p" != / ]; do
        [ ! -L "$p" ] || return 1
        p="${p%/*}"; [ -n "$p" ] || p=/
    done
}

ops_platform_service_inventory()
{
    local opt path rel rows value hash executable
    opt="${OPS_APP%/broray}"
    [ "$opt/broray" = "$OPS_APP" ] || return 1
    rows='[]'
    for rel in bin/broray-updaterctl etc/init.d/S22broray-updater \
      libexec/broray-updater/broray-compat.sh libexec/broray-updater/broray-migrate-legacy.sh \
      libexec/broray-updater/broray-updater.sh libexec/broray-updater/minisign \
      libexec/broray-updater/xray-wrapper; do
        path="$opt/$rel"; value=null
        ops_platform_service_path_safe "$path" || return 1
        if [ -e "$path" ] || [ -L "$path" ]; then
            ops_file_safe "$path" 16777216 || return 1
            hash="$(sha256sum "$path")" || return 1; hash="${hash%% *}"
            ops_platform_sha_valid "$hash" || return 1
            executable=false; [ ! -x "$path" ] || executable=true
            value="$(jq -nc --arg sha "$hash" --argjson x "$executable" '{sha256:$sha,executable:$x}')" || return 1
        fi
        rows="$(jq -nc --argjson rows "$rows" --arg p "$rel" --argjson v "$value" '$rows+[{path:$p,value:$v}]')" || return 1
    done
    printf '%s\n' "$rows"
}

ops_platform_service_capture()
{
    local script ash exe pid ready owner again cmd row dir found n before after exclude exclude_pid bound bound_pid tracer
    [ "$#" -le 2 ] || return 1
    # Only the new generation path supplies an independently authenticated
    # supervisor. Its argv includes the daemon script as a launch argument;
    # it is not a second daemon. Never skip a process from PID/name alone.
    exclude="${1:-null}";exclude_pid=''
    if [ "$exclude" != null ]; then
        printf '%s\n' "$exclude" | broray_ops_owner_valid || return 1
        exclude_pid="$(printf '%s\n' "$exclude" | jq -r .pid)" || return 1
        again="$(broray_ops_capture_owner "$exclude_pid")" || return 1
        jq -en --argjson expected "$exclude" --argjson current "$again" '$expected==$current' >/dev/null || return 1
    fi
    # Only generation-stop supplies this identity, after authenticating the
    # native socket peer and its exact platform launch. Pipe-based script exec
    # intentionally has no pathname in argv; never rediscover it by a PID file.
    bound="${2:-null}";bound_pid=''
    if [ "$bound" != null ]; then
        [ "$exclude" != null ] || return 1
        printf '%s\n' "$bound" | broray_ops_owner_valid || return 1
        bound_pid="$(printf '%s\n' "$bound" | jq -r .pid)" || return 1
        [ "$bound_pid" != "$exclude_pid" ] || return 1
        again="$(broray_ops_capture_owner "$bound_pid")" || return 1
        jq -en --argjson expected "$bound" --argjson current "$again" '$expected==$current' >/dev/null || return 1
        tracer="$(awk '$1=="TracerPid:"{print $2}' "/proc/$bound_pid/status")" || return 1
        [ "$tracer" = "$exclude_pid" ] || return 1
    fi
    script="${OPS_APP%/broray}/libexec/broray-updater/broray-updater.sh"
    ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"; exe="$(readlink -f "$ash")" || return 1
    [ "$OPS_PROC" = /proc ] || return 1
    [ "$OPS_UPDATER" = "${OPS_APP%/broray}/var/lib/broray-updater" ] || return 1
    ops_platform_service_path_safe "$OPS_UPDATER" || return 1
    before="$(ops_platform_service_inventory)" || return 1
    found="$bound_pid"; n=0
    for dir in /proc/[0-9]*; do
        [ -d "$dir" ] || continue
        n=$((n+1)); [ "$n" -le 4096 ] || return 1
        [ -d "$dir" ] || continue
        [ "${dir##*/}" != "$exclude_pid" ] || continue
        [ "${dir##*/}" != "$bound_pid" ] || continue
        [ -r "$dir/cmdline" ] || { [ ! -d "$dir" ] && continue; return 1; }
        cmd="$(tr '\000' '\n' <"$dir/cmdline" 2>/dev/null)" || { [ ! -d "$dir" ] && continue; return 1; }
        printf '%s\n' "$cmd" | grep -Fqx -- "$script" || continue
        [ -z "$found" ] || return 1
        found="${dir##*/}"
        printf '%s\n' "$cmd" | jq -Rsc --arg ash "$ash" --arg script "$script" \
          'split("\n") | .[:-1] == [$ash,$script,"daemon"]' | grep -qx true || return 1
    done
    if [ -z "$found" ]; then
        for row in daemon.pid daemon.ready daemon.lock; do
            [ ! -e "$OPS_UPDATER/$row" ] && [ ! -L "$OPS_UPDATER/$row" ] || return 1
        done
        owner=null; ready=false
    else
        ops_file_safe "$OPS_UPDATER/daemon.pid" 32 && ops_dir_safe "$OPS_UPDATER/daemon.lock" || return 1
        pid="$(cat "$OPS_UPDATER/daemon.pid")" || return 1
        case "$pid" in ''|*[!0-9]*|0|1) return 1 ;; esac
        [ "$pid" = "$found" ] || return 1
        for row in "$OPS_UPDATER/daemon.lock"/* "$OPS_UPDATER/daemon.lock"/.[!.]* "$OPS_UPDATER/daemon.lock"/..?*; do
            [ ! -e "$row" ] && [ ! -L "$row" ] || return 1
        done
        owner="$(broray_ops_capture_owner "$pid")" || return 1
        [ "$(printf '%s\n' "$owner" | jq -r .executable)" = "$exe" ] || return 1
        ready=false
        if [ -e "$OPS_UPDATER/daemon.ready" ] || [ -L "$OPS_UPDATER/daemon.ready" ]; then
            ops_file_safe "$OPS_UPDATER/daemon.ready" 32 || return 1
            [ "$(cat "$OPS_UPDATER/daemon.ready")" = "$pid" ] || return 1
            ready=true
        fi
        again="$(broray_ops_capture_owner "$pid")" || return 1
        [ "$again" = "$owner" ] || return 1
        if [ "$bound" != null ]; then
            jq -en --argjson expected "$bound" --argjson current "$owner" '$expected==$current' >/dev/null || return 1
            tracer="$(awk '$1=="TracerPid:"{print $2}' "/proc/$pid/status")" || return 1
            [ "$tracer" = "$exclude_pid" ] || return 1
        else
            # Legacy observation still requires exact pathname argv. It never
            # acquires the native launch proof by accepting a pipe name.
            cmd="$(tr '\000' '\n' <"/proc/$pid/cmdline")" || return 1
            printf '%s\n' "$cmd" | jq -Rsc --arg ash "$ash" --arg script "$script" \
              'split("\n") | .[:-1] == [$ash,$script,"daemon"]' | grep -qx true || return 1
        fi
    fi
    after="$(ops_platform_service_inventory)" || return 1
    [ "$after" = "$before" ] || return 1
    if [ "$exclude" != null ]; then
        again="$(broray_ops_capture_owner "$exclude_pid")" || return 1
        jq -en --argjson expected "$exclude" --argjson current "$again" '$expected==$current' >/dev/null || return 1
    fi
    jq -nc --argjson owner "$owner" --argjson ready "$ready" --argjson files "$before" \
      --arg script "$script" '{service:"broray-updater",scriptPath:$script,owner:$owner,readyMarkerMatchesPid:$ready,readinessProven:false,platformFiles:$files}'
}

ops_platform_service_bind()
{
    local nonce captured record file expected
    [ "$#" = 4 ] || ops_error INVALID_REQUEST 1
    ops_authorize "$1" "$2"; ops_owner_authorize "$3"; ops_global_matches || ops_error OWNER_CHANGED
    ops_platform_stop_valid || ops_error PLATFORM_PHASE_INVALID
    nonce="$4"; ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    jq -e --arg nonce "$nonce" '.platformPreflight.stopNonce==$nonce' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    [ ! -e "$OPS_CURRENT/platform-stop-supervision.json" ] && [ ! -L "$OPS_CURRENT/platform-stop-supervision.json" ] || ops_error OPERATION_EXISTS
    captured="$(ops_platform_service_capture)" || ops_error UPDATER_SERVICE_UNCONFIRMED
    expected="$(jq -r .platformPreflight.expectedPlatformManifestSha256 "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 1
    record="$(jq -nc --arg id "$OPS_ID" --arg nonce "$nonce" --arg sha "$expected" --argjson service "$captured" \
      '{schemaVersion:1,contract:"broray-platform-service/1",operationId:$id,stopNonce:$nonce,expectedPlatformManifestSha256:$sha,service:$service,signalsAuthorized:false}')" || ops_error STATE_UNAVAILABLE 1
    file="$OPS_CURRENT/platform-service.json"
    if [ -e "$file" ] || [ -L "$file" ]; then
        ops_file_safe "$file" 16384 || ops_error UPDATER_SERVICE_UNCONFIRMED
        jq -e --argjson expected "$record" '.==$expected' "$file" >/dev/null || ops_error UPDATER_SERVICE_CHANGED
        "$OPS_GUARD" --sync-state "$file" || ops_error STATE_UNAVAILABLE 1
    else
        ops_write "$file" "$record" || ops_error STATE_UNAVAILABLE 1
    fi
    printf '%s\n' '{"ok":true,"serviceBound":true,"signalsAuthorized":false,"phase":"STOP_INTENT"}'
}


# The binding remains observation-only. This separate, durable authorisation
# is issued solely to the native tracer while both exact processes are stopped.
ops_platform_service_record_valid()
{
    local file
    file="$OPS_CURRENT/platform-service.json"
    ops_file_safe "$file" 16384 || return 1
    jq -e --arg id "$OPS_ID" --arg nonce "$(jq -r .platformPreflight.stopNonce "$OPS_CURRENT/state.json")" \
      --arg sha "$(jq -r .platformPreflight.expectedPlatformManifestSha256 "$OPS_CURRENT/state.json")" '
      .schemaVersion==1 and .contract=="broray-platform-service/1" and
      .operationId==$id and .stopNonce==$nonce and .expectedPlatformManifestSha256==$sha and
      .signalsAuthorized==false and .service.service=="broray-updater" and
      .service.readinessProven==false and (.service.platformFiles|type)=="array" and
      (.service.platformFiles|length)==7' "$file" >/dev/null 2>&1
}

# Contract /1 is an observation made at stop time, never a lifetime writer
# proof. Even owner=null cannot exclude an earlier detached legacy writer.
# Preserve the transaction/fence and require the explicit reboot migration.
ops_platform_service_require_birth_proof()
{
    if jq -e '.contract=="broray-platform-service/1"' "$OPS_CURRENT/platform-service.json" >/dev/null 2>&1; then
        ops_error UPDATER_LEGACY_REBOOT_REQUIRED 75
    fi
}

ops_platform_service_stop_target()
{
    local owner
    [ "$#" = 4 ] || ops_error INVALID_REQUEST 1
    ops_authorize "$1" "$2"; ops_owner_authorize "$3"; ops_global_matches || ops_error OWNER_CHANGED
    { ops_platform_stop_valid || ops_platform_stop_valid STOPPED; } || ops_error PLATFORM_PHASE_INVALID
    ops_nonce_valid "$4" && jq -e --arg nonce "$4" '.platformPreflight.stopNonce==$nonce' \
      "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    ops_platform_service_record_valid || ops_error UPDATER_SERVICE_UNCONFIRMED
    ops_platform_service_require_birth_proof
    owner="$(jq -c .service.owner "$OPS_CURRENT/platform-service.json")" || ops_error STATE_UNAVAILABLE 1
    jq -nc --argjson owner "$owner" --arg phase "$(jq -r .platformPreflight.phase "$OPS_CURRENT/state.json")" \
      '{ok:true,owner:$owner,phase:$phase}'
}

ops_platform_service_register_prepare()
{
    ops_platform_service_record_valid || ops_error UPDATER_SERVICE_UNCONFIRMED
    ops_platform_service_require_birth_proof
    jq -e '.service.owner!=null' "$OPS_CURRENT/platform-service.json" >/dev/null || ops_error UPDATER_SERVICE_UNCONFIRMED
    # A successful stop cannot be repeated. Domain recovery handles a failed
    # or interrupted attempt; no automatic retirement based on a helper PID.
    [ ! -e "$OPS_CURRENT/platform-service-authorized.json" ] && \
      [ ! -L "$OPS_CURRENT/platform-service-authorized.json" ] || ops_error OPERATION_EXISTS
}

ops_platform_service_single_thread()
{
    local task count
    count=0
    for task in "$OPS_PROC/$1/task"/[0-9]*; do
        [ -d "$task" ] || return 1
        count=$((count+1)); [ "$count" -le 1 ] || return 1
    done
    [ "$count" = 1 ]
}

ops_platform_service_authorize_stop()
{
    local sid supervisor target idle target_owner idle_owner current snapshot marker ledger expected_sleep cmd children proof record readfd writefd armed
    [ "$#" = 6 ] || ops_error INVALID_REQUEST 1
    ops_authorize "$1" "$2"; ops_global_matches || ops_error OWNER_CHANGED
    ops_platform_stop_valid && ops_platform_service_record_valid || ops_error PLATFORM_PHASE_INVALID
    ops_platform_service_require_birth_proof
    sid="$4"; target="$5"; idle="$6"; ops_nonce_valid "$sid" || ops_error INVALID_REQUEST 1
    case "$3:$target:$idle" in *[!0-9:]*) ops_error INVALID_REQUEST 1 ;; esac
    marker="$OPS_CURRENT/platform-stop-supervision.json"
    ops_file_safe "$marker" 4096 || ops_error CHILDREN_UNCONFIRMED
    jq -e --arg id "$OPS_ID" --arg sid "$sid" --arg pid "$3" '
      .kind=="protected-platform-stop-supervision" and .mode=="bound-service" and
      .operationId==$id and .supervisorId==$sid and (.owner.pid|tostring)==$pid' "$marker" >/dev/null || ops_error CHILDREN_UNCONFIRMED
    supervisor="$(jq -c .owner "$marker")"; broray_ops_classify_owner "$supervisor"
    [ "$OPS_OWNER_STATUS" = ACTIVE ] || ops_error CHILDREN_UNCONFIRMED
    broray_ops_classify_owner "$(jq -c .owner "$OPS_EXECUTOR")"
    [ "$OPS_OWNER_STATUS" = ACTIVE ] || ops_error OWNER_UNCONFIRMED
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    current="$(ops_platform_service_capture)" || ops_error UPDATER_SERVICE_UNCONFIRMED
    snapshot="$(jq -c .service "$OPS_CURRENT/platform-service.json")" || ops_error STATE_UNAVAILABLE 1
    jq -en --argjson a "$current" --argjson b "$snapshot" '$a==$b' >/dev/null || ops_error UPDATER_SERVICE_CHANGED
    target_owner="$(printf '%s\n' "$current" | jq -c .owner)"
    [ "$(printf '%s\n' "$target_owner" | jq -r .pid)" = "$target" ] || ops_error OWNER_CHANGED
    [ "$(awk '$1=="TracerPid:"{print $2}' "$OPS_PROC/$target/status")" = "$3" ] || ops_error CHILDREN_UNCONFIRMED
    [ "$(awk '$1=="TracerPid:"{print $2}' "$OPS_PROC/$idle/status")" = "$3" ] || ops_error CHILDREN_UNCONFIRMED
    ops_platform_service_single_thread "$target" && ops_platform_service_single_thread "$idle" || ops_error CHILDREN_UNCONFIRMED
    children="$(cat "$OPS_PROC/$target/task/$target/children")" || ops_error CHILDREN_UNCONFIRMED
    [ "$(printf '%s' "$children" | tr -d ' \t\r\n')" = "$idle" ] || ops_error CHILDREN_UNCONFIRMED
    [ -z "$(cat "$OPS_PROC/$idle/task/$idle/children")" ] || ops_error CHILDREN_UNCONFIRMED
    [ "$(awk '$1=="PPid:"{print $2}' "$OPS_PROC/$idle/status")" = "$target" ] || ops_error CHILDREN_UNCONFIRMED
    idle_owner="$(broray_ops_capture_owner "$idle")" || ops_error CHILDREN_UNCONFIRMED
    expected_sleep="$(command -v sleep)" && expected_sleep="$(readlink -f "$expected_sleep")" || ops_error CHILDREN_UNCONFIRMED
    [ "$(printf '%s\n' "$idle_owner" | jq -r .executable)" = "$expected_sleep" ] || ops_error CHILDREN_UNCONFIRMED
    cmd="$(tr '\000' '\n' <"$OPS_PROC/$idle/cmdline")" || ops_error CHILDREN_UNCONFIRMED
    printf '%s\n' "$cmd" | jq -Rse 'split("\n")|.[:-1]|length==2 and (.[0]|split("/")|last)=="sleep" and .[1]=="2"' >/dev/null || ops_error UPDATER_NOT_IDLE
    ledger="$OPS_RAM/supervisors/$OPS_ID/$sid/children.json"
    ops_file_safe "$ledger" 65536 && jq -e --arg id "$OPS_ID" --arg sid "$sid" \
      --argjson sup "$supervisor" --argjson a "$target_owner" --argjson b "$idle_owner" '
      .operationId==$id and .supervisorId==$sid and .supervisorPid==$sup.pid and
      .supervisorStartTicks==$sup.startTicks and .bootId==$sup.bootId and
      (.children|length)==2 and
      any(.children[];.pid==$a.pid and .startTicks==$a.startTicks and .bootId==$a.bootId) and
      any(.children[];.pid==$b.pid and .startTicks==$b.startTicks and .bootId==$b.bootId)' "$ledger" >/dev/null || ops_error CHILDREN_UNCONFIRMED
    proof="$OPS_CURRENT/platform-service-authorized.json"
    [ ! -e "$proof" ] && [ ! -L "$proof" ] || ops_error OPERATION_EXISTS
    record="$(jq -nc --arg id "$OPS_ID" --arg sid "$sid" \
      --arg nonce "$(jq -r .platformPreflight.stopNonce "$OPS_CURRENT/state.json")" \
      --argjson sup "$supervisor" --argjson target "$target_owner" --argjson idle "$idle_owner" \
      '{schemaVersion:1,contract:"broray-bound-service-stop/1",operationId:$id,stopNonce:$nonce,supervisorId:$sid,supervisor:$sup,target:$target,idleChild:$idle,signalsAuthorized:true}')" || ops_error STATE_UNAVAILABLE 1
    # Private pipes connect this guarded callback to the same native tracer.
    # Keep the guard through ARM acknowledgement and durable authorization.
    readfd="${BRORAY_BOUND_STOP_READ_FD:-}"; writefd="${BRORAY_BOUND_STOP_WRITE_FD:-}"
    case "$readfd:$writefd" in *[!0-9:]*|:|*:) ops_error CHILDREN_UNCONFIRMED ;; esac
    [ -p /proc/self/fd/8 ] && [ -p /proc/self/fd/9 ] || ops_error CHILDREN_UNCONFIRMED
    [ "$(readlink /proc/self/fd/8)" = "$(readlink "$OPS_PROC/$3/fd/$readfd")" ] &&
      [ "$(readlink /proc/self/fd/9)" = "$(readlink "$OPS_PROC/$3/fd/$writefd")" ] || ops_error CHILDREN_UNCONFIRMED
    printf 'ARM %s %s %s\n' "$sid" "$target" "$idle" >&8 || ops_error CHILDREN_UNCONFIRMED
    IFS= read -r -t 2 armed <&9 && [ "$armed" = "ARMED $sid" ] || ops_error CHILDREN_UNCONFIRMED
    # Both tracees already have EXITKILL before signalsAuthorized becomes true.
    jq -e '.state=="armed" and .termSent==false' "$ledger" >/dev/null || ops_error CHILDREN_UNCONFIRMED
    ops_write "$proof" "$record" || ops_error STATE_UNAVAILABLE 1
    # Revalidation was performed with the target and its only idle child pinned.
    printf '%s\n' '{"ok":true,"signalsAuthorized":true}'
}

ops_platform_service_birth_finished()
{
    local record pid ticks current status
    record="$1"
    ops_child_birth_absent "$record" && return 0
    pid="$(printf '%s\n' "$record" | jq -r .pid)";ticks="$(printf '%s\n' "$record" | jq -r .startTicks)"
    current="$(broray_ops_start_ticks "$OPS_PROC/$pid")" || return 1
    [ "$current" = "$ticks" ] || return 1
    # An unreaped zombie cannot execute. Its descendants must independently
    # have passed the native ledger drain before this predicate is used.
    status="$(awk '{s=$0;sub(/^.*\) /,"",s);split(s,a," ");print a[1]}' "$OPS_PROC/$pid/stat")"
    case "$status" in Z|X) return 0 ;; *) return 1 ;; esac
}

ops_platform_service_stopped()
{
    local record owner current before proof marker state sid
    [ "$#" = 4 ] || ops_error INVALID_REQUEST 1
    ops_authorize "$1" "$2"; ops_owner_authorize "$3"; ops_global_matches || ops_error OWNER_CHANGED
    { ops_platform_stop_valid || ops_platform_stop_valid STOPPED; } || ops_error PLATFORM_PHASE_INVALID
    ops_nonce_valid "$4" && jq -e --arg nonce "$4" '.platformPreflight.stopNonce==$nonce' \
      "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    ops_platform_service_record_valid || ops_error UPDATER_SERVICE_UNCONFIRMED
    ops_platform_service_require_birth_proof
    ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    owner="$(jq -c .service.owner "$OPS_CURRENT/platform-service.json")"
    if [ "$owner" != null ]; then
        proof="$OPS_CURRENT/platform-service-authorized.json"
        marker="$OPS_CURRENT/platform-stop-supervision.json"
        ops_file_safe "$proof" 16384 && ops_file_safe "$marker" 4096 || ops_error UPDATER_STOP_UNCONFIRMED
        jq -e --arg id "$OPS_ID" --arg nonce "$4" --argjson target "$owner" \
          --slurpfile marker "$marker" '
          .schemaVersion==1 and .contract=="broray-bound-service-stop/1" and .operationId==$id and
          .stopNonce==$nonce and .target==$target and .signalsAuthorized==true and
          .supervisorId==$marker[0].supervisorId and .supervisor==$marker[0].owner and
          $marker[0].mode=="bound-service"' "$proof" >/dev/null || ops_error UPDATER_STOP_UNCONFIRMED
        broray_ops_classify_owner "$(jq -c .supervisor "$proof")"
        [ "$OPS_OWNER_STATUS" = STALE ] || ops_error CHILDREN_UNCONFIRMED
        ops_platform_service_birth_finished "$owner" || ops_error UPDATER_STOP_UNCONFIRMED
        ops_platform_service_birth_finished "$(jq -c .idleChild "$proof")" || ops_error CHILDREN_UNCONFIRMED
    else
        [ ! -e "$OPS_CURRENT/platform-stop-supervision.json" ] && [ ! -L "$OPS_CURRENT/platform-stop-supervision.json" ] || ops_error CHILDREN_UNCONFIRMED
    fi
    current="$(ops_platform_service_capture)" || ops_error UPDATER_STOP_UNCONFIRMED
    printf '%s\n' "$current" | jq -e '.owner==null' >/dev/null || ops_error UPDATER_STOP_UNCONFIRMED
    before="$(jq -c .service.platformFiles "$OPS_CURRENT/platform-service.json")"
    printf '%s\n' "$current" | jq -e --argjson before "$before" '.platformFiles==$before' >/dev/null || ops_error PLATFORM_CHANGED
    if ops_platform_stop_valid STOPPED; then
        "$OPS_GUARD" --sync-state "$OPS_CURRENT/state.json" || ops_error STATE_UNAVAILABLE 1
    else
        state="$(jq -c --arg now "$(ops_now)" '.platformPreflight.phase="STOPPED"|.revision+=1|.updatedAt=$now' "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 1
        ops_write "$OPS_CURRENT/state.json" "$state" || ops_error STATE_UNAVAILABLE 1
        ops_event phase_changed >/dev/null 2>&1 || true
    fi
    printf '%s\n' '{"ok":true,"phase":"STOPPED","serviceStopped":true,"platformReady":false}'
}

# Prepare legacy reboot evidence inside the existing protected transaction.
# This verb does not signal, reboot, install, enqueue or retire the global fence.
# The native child inherits the coordinator guard until publication finishes.
ops_platform_migration_stage()
{
    local nonce expected generation payload live stage saved current desired response
    [ "$#" = 4 ] || ops_error INVALID_REQUEST 1
    ops_authorize "$1" "$2"; ops_owner_authorize "$3"; ops_global_matches || ops_error OWNER_CHANGED
    ops_platform_stop_valid && ops_platform_service_record_valid || ops_error PLATFORM_PHASE_INVALID
    nonce="$4"; ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    jq -e --arg nonce "$nonce" '.platformPreflight.stopNonce==$nonce' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    ops_platform_queue_clear || ops_error DOMAIN_OPERATION_BUSY
    [ ! -e "$OPS_CURRENT/platform-stop-supervision.json" ] && [ ! -L "$OPS_CURRENT/platform-stop-supervision.json" ] || ops_error CHILDREN_UNCONFIRMED
    generation="${BRORAY_OPS_GENERATION:-$OPS_CODE/bin/broray-updater-generation}"
    ops_file_safe "$generation" 16777216 && [ -x "$generation" ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    [ "$("$generation" --version)" = 'broray-updater-generation/2 supervised-from-birth syscall-containment' ] || ops_error GENERATION_RUNTIME_UNAVAILABLE 75
    payload="$OPS_CODE/share/updater-platform"
    expected="$(jq -r .platformPreflight.expectedPlatformManifestSha256 "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 1
    ops_platform_service_path_safe "$payload" && ops_file_safe "$payload/SHA256SUMS" 4096 || ops_error PLATFORM_MANIFEST_CHANGED
    [ "$(sha256sum "$payload/SHA256SUMS" | awk '{print $1}')" = "$expected" ] || ops_error PLATFORM_MANIFEST_CHANGED
    saved="$(jq -c .service "$OPS_CURRENT/platform-service.json")" || ops_error STATE_UNAVAILABLE 1
    current="$(ops_platform_service_capture)" || ops_error UPDATER_SERVICE_UNCONFIRMED
    jq -en --argjson a "$saved" --argjson b "$current" '$a==$b' >/dev/null || ops_error UPDATER_SERVICE_CHANGED
    desired="$(printf '%s\n' "$saved" | jq -r 'if .owner==null then "stopped" else "running" end')" || ops_error STATE_UNAVAILABLE 1
    # Observation of an absent legacy PID is only the previous service state.
    # It never excludes detached writers; native still requires a new boot.
    case "$OPS_APP" in */opt/broray) live="${OPS_APP%/opt/broray}" ;; *) ops_error UNSAFE_STATE 1 ;; esac
    [ -n "$live" ] || live=/
    stage="$OPS_CURRENT/platform-migration"
    if [ -e "$stage" ] || [ -L "$stage" ]; then ops_dir_safe "$stage" || ops_error UNSAFE_STATE 1
    else mkdir -m 0700 "$stage" || ops_error STATE_UNAVAILABLE 1
    fi
    response="$("$generation" migration-stage "$stage" "$live" "$payload" "$expected" "$OPS_ID" "$nonce" "$desired")" || ops_error MIGRATION_PREPARATION_UNCONFIRMED 75
    printf '%s\n' "$response" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="REBOOT_REQUIRED" and .[0].activationAllowed==false and .[0].serviceStopped==false and .[0].oldBootId==.[0].currentBootId and (.[0].intentSha256|(type=="string" and length==64 and all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))))' >/dev/null || ops_error MIGRATION_PREPARATION_UNCONFIRMED 75
    current="$(ops_platform_service_capture)" || ops_error UPDATER_SERVICE_UNCONFIRMED
    jq -en --argjson a "$saved" --argjson b "$current" '$a==$b' >/dev/null || ops_error UPDATER_SERVICE_CHANGED
    printf '%s\n' "$response" | jq -c --arg id "$OPS_ID" '.+{operationId:$id,platformReady:false,signalsAuthorized:false}'
}
