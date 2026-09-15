#!/opt/bin/ash

# Complete the one-time compact updater handoff after an older updater has
# activated this application slot.  The payload is authenticated transitively
# by the signed application bundle.  No OPKG file is read or changed.

set -u
umask 077

PATH="${BRORAY_HANDOFF_PATH:-/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin}"
LC_ALL=C
export PATH LC_ALL

CONTRACT='broray-universal-platform-handoff/1'
ROOT_PREFIX="${BRORAY_HANDOFF_ROOT_PREFIX:-}"
TEST_MODE="${BRORAY_HANDOFF_TEST_MODE:-0}"
NO_ASYNC="${BRORAY_HANDOFF_NO_ASYNC:-0}"
WAIT_LIMIT="${BRORAY_HANDOFF_WAIT_LIMIT:-300}"

root_path()
{
    case "$1" in /*) ;; *) return 2 ;; esac
    if [ -n "$ROOT_PREFIX" ]; then
        printf '%s%s\n' "${ROOT_PREFIX%/}" "$1"
    else
        printf '%s\n' "$1"
    fi
}

APP_ROOT="${BRORAY_HANDOFF_APP_ROOT:-$(root_path /opt/broray)}"
STATE_ROOT="${BRORAY_HANDOFF_STATE_ROOT:-$(root_path /opt/var/lib/broray-platform-handoff)}"
UPDATER_STATE_ROOT="${BRORAY_HANDOFF_UPDATER_STATE_ROOT:-$(root_path /opt/var/lib/broray-updater)}"
OPERATION_ROOT="${BRORAY_HANDOFF_OPERATION_ROOT:-$(root_path /opt/var/lib/broray/operations)}"
OPERATION_POINTER="${BRORAY_HANDOFF_OPERATION_POINTER:-$(root_path /opt/var/lib/broray/last-operation)}"
CURRENT_PATH="$APP_ROOT/current"
RELEASES_ROOT="$APP_ROOT/releases"
PAYLOAD_ROOT="${BRORAY_HANDOFF_PAYLOAD_ROOT:-$CURRENT_PATH/app/share/updater-platform}"
INIT="${BRORAY_HANDOFF_INIT:-$(root_path /opt/etc/init.d/S22broray-updater)}"
UPDATER="${BRORAY_HANDOFF_UPDATER:-$(root_path /opt/libexec/broray-updater/broray-updater.sh)}"
SELF="${BRORAY_HANDOFF_SELF:-$CURRENT_PATH/app/lib/universal-platform-handoff.sh}"
ASH="${BRORAY_HANDOFF_ASH:-$(root_path /opt/bin/ash)}"
STATUS_FILE="$STATE_ROOT/status.json"
REQUEST_FILE="$STATE_ROOT/request.json"
PHASE_FILE="$STATE_ROOT/phase"
PID_FILE="$STATE_ROOT/worker.pid"
LOCK_DIR="$STATE_ROOT/worker.lock"
BACKUP_ROOT="$STATE_ROOT/platform-backup"
DAEMON_STATE_FILE="$STATE_ROOT/daemon-was-running"

PLATFORM_TARGET_FILES='opt/bin/broray-updaterctl
opt/etc/init.d/S22broray-updater
opt/libexec/broray-updater/broray-compat.sh
opt/libexec/broray-updater/broray-migrate-legacy.sh
opt/libexec/broray-updater/minisign
opt/libexec/broray-updater/broray-updater.sh
opt/libexec/broray-updater/xray-wrapper'

now()
{
    date -u '+%Y-%m-%dT%H:%M:%SZ'
}

valid_id()
{
    case "${1:-}" in ''|.*|-*|*[!A-Za-z0-9._-]*) return 1 ;; esac
    [ "${#1}" -le 192 ]
}

valid_sha256()
{
    [ "${#1}" -eq 64 ] || return 1
    case "$1" in *[!0-9a-f]*) return 1 ;; esac
}

regular_file()
{
    [ -f "$1" ] && [ ! -L "$1" ]
}

safe_directory()
{
    [ -d "$1" ] && [ ! -L "$1" ]
}

platform_executable()
{
    [ -x "$1" ] || [ "$TEST_MODE" = 1 ]
}

process_starttime()
{
    case "${1:-}" in ''|*[!0-9]*|0) return 1 ;; esac
    if [ -n "${BRORAY_HANDOFF_PROCESS_STARTTIME_HOOK:-}" ]; then
        "$BRORAY_HANDOFF_PROCESS_STARTTIME_HOOK" "$1"
        return $?
    fi
    awk 'NR==1 {print $22; exit}' "$(root_path /proc)/$1/stat" 2>/dev/null
}

process_matches()
{
    pid="$1"
    start="$2"
    case "$pid:$start" in *[!0-9:]*|:|*:) return 1 ;; esac
    kill -0 "$pid" 2>/dev/null || return 1
    [ "$(process_starttime "$pid" 2>/dev/null || true)" = "$start" ]
}

atomic_text()
{
    target="$1"
    value="$2"
    parent="${target%/*}"
    temporary="$parent/.handoff-${target##*/}.$$"
    safe_directory "$parent" || return 1
    [ ! -e "$temporary" ] && [ ! -L "$temporary" ] || return 1
    printf '%s\n' "$value" >"$temporary" || { rm -f "$temporary"; return 1; }
    chmod 0600 "$temporary" 2>/dev/null || true
    mv -f "$temporary" "$target" || { rm -f "$temporary"; return 1; }
    regular_file "$target"
}

status_write()
{
    state="$1"
    running="$2"
    code="$3"
    message="$4"
    mutation="$5"
    rollback="$6"
    operation_id="${7:-}"
    candidate_id="${8:-}"
    temporary="$STATE_ROOT/.status.$$"
    safe_directory "$STATE_ROOT" || return 1
    [ ! -e "$temporary" ] && [ ! -L "$temporary" ] || return 1
    jq -nc \
        --arg contract "$CONTRACT" \
        --arg state "$state" \
        --arg code "$code" \
        --arg message "$message" \
        --arg operationId "$operation_id" \
        --arg candidateId "$candidate_id" \
        --arg recordedAt "$(now)" \
        --argjson running "$running" \
        --argjson mutationStarted "$mutation" \
        --argjson rollbackPerformed "$rollback" '
        {
          schemaVersion:1,
          contract:$contract,
          state:$state,
          running:$running,
          code:$code,
          message:$message,
          mutationStarted:$mutationStarted,
          rollbackPerformed:$rollbackPerformed,
          operationId:(if $operationId=="" then null else $operationId end),
          candidateId:(if $candidateId=="" then null else $candidateId end),
          recordedAt:$recordedAt
        }' >"$temporary" || { rm -f "$temporary"; return 1; }
    chmod 0600 "$temporary" 2>/dev/null || true
    mv -f "$temporary" "$STATUS_FILE" || { rm -f "$temporary"; return 1; }
    regular_file "$STATUS_FILE"
}

phase_write()
{
    atomic_text "$PHASE_FILE" "$1"
}

ensure_state_root()
{
    if [ -e "$STATE_ROOT" ] || [ -L "$STATE_ROOT" ]; then
        safe_directory "$STATE_ROOT" || return 1
    else
        mkdir -p "$STATE_ROOT" || return 1
        chmod 0700 "$STATE_ROOT" 2>/dev/null || true
    fi
}

payload_manifest_sha()
{
    regular_file "$PAYLOAD_ROOT/SHA256SUMS" || return 1
    sha256sum "$PAYLOAD_ROOT/SHA256SUMS" | awk 'NR==1 {print $1; exit}'
}

payload_valid()
{
    manifest="$PAYLOAD_ROOT/SHA256SUMS"
    safe_directory "$PAYLOAD_ROOT" || return 1
    regular_file "$manifest" || return 1
    [ "$(wc -l <"$manifest" | tr -d ' ')" = 7 ] || return 1
    [ "$(find "$PAYLOAD_ROOT" -mindepth 1 ! -type d ! -type f -print -quit 2>/dev/null)" = '' ] || return 1
    [ "$(find "$PAYLOAD_ROOT" -type f -print | wc -l | tr -d ' ')" = 8 ] || return 1
    for relative in $PLATFORM_TARGET_FILES
    do
        regular_file "$PAYLOAD_ROOT/$relative" || return 1
        platform_executable "$PAYLOAD_ROOT/$relative" || return 1
    done
    (cd "$PAYLOAD_ROOT" && sha256sum -c SHA256SUMS >/dev/null 2>&1) || return 1
    [ "$($ASH "$PAYLOAD_ROOT/opt/libexec/broray-updater/broray-updater.sh" version 2>/dev/null)" = 'broray-updater/5' ] || return 1
    return 0
}

platform_current()
{
    payload_valid || return 1
    for relative in $PLATFORM_TARGET_FILES
    do
        destination="$(root_path "/$relative")"
        regular_file "$destination" || return 1
        platform_executable "$destination" || return 1
        cmp -s "$PAYLOAD_ROOT/$relative" "$destination" || return 1
    done
}

current_slot()
{
    marker="$CURRENT_PATH/.broray-slot"
    regular_file "$marker" || return 1
    slot="$(sed -n '1p' "$marker" 2>/dev/null || true)"
    [ "$(wc -l <"$marker" | tr -d ' ')" = 1 ] || return 1
    valid_id "$slot" || return 1
    printf '%s\n' "$slot"
}

current_candidate()
{
    release="$CURRENT_PATH/release.json"
    regular_file "$release" || return 1
    jq -er '.candidateId | select(type=="string" and length>0)' "$release"
}

operation_binding()
{
    operation_id="$(sed -n '1p' "$OPERATION_POINTER" 2>/dev/null || true)"
    valid_id "$operation_id" || return 1
    operation_dir="$OPERATION_ROOT/$operation_id"
    request="$operation_dir/request.json"
    state="$operation_dir/state.json"
    previous_file="$operation_dir/previous-slot"
    target_file="$operation_dir/target-slot"
    regular_file "$request" && regular_file "$state" &&
        regular_file "$previous_file" && regular_file "$target_file" || return 1
    previous_slot="$(sed -n '1p' "$previous_file" 2>/dev/null || true)"
    target_slot="$(sed -n '1p' "$target_file" 2>/dev/null || true)"
    active_slot="$(current_slot)" || return 1
    candidate_id="$(current_candidate)" || return 1
    valid_id "$previous_slot" && valid_id "$target_slot" || return 1
    [ "$target_slot" = "$active_slot" ] || return 1
    [ "$previous_slot" != "$target_slot" ] || return 1
    jq -e --arg operationId "$operation_id" --arg candidateId "$candidate_id" '
      .schemaVersion==1 and .operationId==$operationId and
      (.operation=="update" or .operation=="reinstall") and
      .target.candidateId==$candidateId
    ' "$request" >/dev/null 2>&1 || return 1
    printf '%s\t%s\t%s\t%s\n' "$operation_id" "$previous_slot" "$target_slot" "$candidate_id"
}

request_write()
{
    binding="$1"
    operation_id="$(printf '%s\n' "$binding" | awk -F '\t' '{print $1}')"
    previous_slot="$(printf '%s\n' "$binding" | awk -F '\t' '{print $2}')"
    target_slot="$(printf '%s\n' "$binding" | awk -F '\t' '{print $3}')"
    candidate_id="$(printf '%s\n' "$binding" | awk -F '\t' '{print $4}')"
    manifest_sha="$(payload_manifest_sha)" || return 1
    valid_sha256 "$manifest_sha" || return 1
    temporary="$STATE_ROOT/.request.$$"
    [ ! -e "$temporary" ] && [ ! -L "$temporary" ] || return 1
    jq -nc \
        --arg contract "$CONTRACT" \
        --arg operationId "$operation_id" \
        --arg previousSlot "$previous_slot" \
        --arg targetSlot "$target_slot" \
        --arg candidateId "$candidate_id" \
        --arg payloadManifestSha256 "$manifest_sha" \
        --arg createdAt "$(now)" '
        {
          schemaVersion:1,
          contract:$contract,
          operationId:$operationId,
          previousSlot:$previousSlot,
          targetSlot:$targetSlot,
          candidateId:$candidateId,
          payloadManifestSha256:$payloadManifestSha256,
          createdAt:$createdAt
        }' >"$temporary" || { rm -f "$temporary"; return 1; }
    chmod 0600 "$temporary" 2>/dev/null || true
    mv -f "$temporary" "$REQUEST_FILE" || { rm -f "$temporary"; return 1; }
    regular_file "$REQUEST_FILE"
}

request_valid()
{
    regular_file "$REQUEST_FILE" || return 1
    manifest_sha="$(payload_manifest_sha)" || return 1
    active_slot="$(current_slot)" || return 1
    candidate_id="$(current_candidate)" || return 1
    jq -e \
        --arg contract "$CONTRACT" \
        --arg manifest "$manifest_sha" \
        --arg slot "$active_slot" \
        --arg candidate "$candidate_id" '
        keys==["candidateId","contract","createdAt","operationId","payloadManifestSha256","previousSlot","schemaVersion","targetSlot"] and
        .schemaVersion==1 and .contract==$contract and
        .payloadManifestSha256==$manifest and .targetSlot==$slot and
        .candidateId==$candidate and
        (.operationId|type)=="string" and (.previousSlot|type)=="string"
    ' "$REQUEST_FILE" >/dev/null 2>&1
}

request_value()
{
    jq -er "$1" "$REQUEST_FILE"
}

operation_terminal_state()
{
    operation_id="$(request_value '.operationId')" || return 2
    state_file="$OPERATION_ROOT/$operation_id/state.json"
    regular_file "$state_file" || return 2
    if jq -e '.state=="success" and .stage=="complete" and .running==false' "$state_file" >/dev/null 2>&1; then
        return 0
    fi
    if jq -e '(.state=="error" or .state=="recovery-required") and .running==false' "$state_file" >/dev/null 2>&1; then
        return 1
    fi
    return 2
}

updater_busy()
{
    [ -e "$UPDATER_STATE_ROOT/request.lock" ] || [ -L "$UPDATER_STATE_ROOT/request.lock" ]
}

init_call()
{
    action="$1"
    if [ -n "${BRORAY_HANDOFF_INIT_HOOK:-}" ]; then
        "$BRORAY_HANDOFF_INIT_HOOK" "$action"
    else
        "$ASH" "$INIT" "$action"
    fi
}

daemon_ready()
{
    if [ -n "${BRORAY_HANDOFF_DAEMON_READY_HOOK:-}" ]; then
        "$BRORAY_HANDOFF_DAEMON_READY_HOOK"
        return $?
    fi
    init_call status >/dev/null 2>&1
}

backup_prepare()
{
    [ ! -e "$BACKUP_ROOT" ] && [ ! -L "$BACKUP_ROOT" ] || return 1
    mkdir "$BACKUP_ROOT" || return 1
    chmod 0700 "$BACKUP_ROOT" 2>/dev/null || true
    : >"$BACKUP_ROOT/inventory.tsv" || return 1
    for relative in $PLATFORM_TARGET_FILES
    do
        destination="$(root_path "/$relative")"
        backup="$BACKUP_ROOT/$relative"
        if [ -e "$destination" ] || [ -L "$destination" ]; then
            regular_file "$destination" || return 1
            mkdir -p "${backup%/*}" || return 1
            cp -p "$destination" "$backup" || return 1
            cmp -s "$destination" "$backup" || return 1
            hash="$(sha256sum "$backup" | awk 'NR==1 {print $1; exit}')"
            printf 'present\t%s\t%s\n' "$relative" "$hash" >>"$BACKUP_ROOT/inventory.tsv" || return 1
        else
            printf 'absent\t%s\t-\n' "$relative" >>"$BACKUP_ROOT/inventory.tsv" || return 1
        fi
    done
    [ "$(wc -l <"$BACKUP_ROOT/inventory.tsv" | tr -d ' ')" = 7 ]
}

backup_valid()
{
    safe_directory "$BACKUP_ROOT" || return 1
    regular_file "$BACKUP_ROOT/inventory.tsv" || return 1
    [ "$(wc -l <"$BACKUP_ROOT/inventory.tsv" | tr -d ' ')" = 7 ] || return 1
    for relative in $PLATFORM_TARGET_FILES
    do
        row="$(awk -F '\t' -v path="$relative" '$2==path {count++; value=$0} END {if(count==1) print value; else exit 1}' "$BACKUP_ROOT/inventory.tsv")" || return 1
        state="$(printf '%s\n' "$row" | awk -F '\t' '{print $1}')"
        hash="$(printf '%s\n' "$row" | awk -F '\t' '{print $3}')"
        case "$state" in
            present)
                valid_sha256 "$hash" || return 1
                regular_file "$BACKUP_ROOT/$relative" || return 1
                [ "$(sha256sum "$BACKUP_ROOT/$relative" | awk 'NR==1 {print $1; exit}')" = "$hash" ] || return 1
                ;;
            absent) [ "$hash" = '-' ] || return 1 ;;
            *) return 1 ;;
        esac
    done
}

atomic_install()
{
    source="$1"
    destination="$2"
    parent="${destination%/*}"
    temporary="$parent/.handoff-${destination##*/}.$$"
    regular_file "$source" || return 1
    safe_directory "$parent" || return 1
    if [ -e "$destination" ] || [ -L "$destination" ]; then
        regular_file "$destination" || return 1
    fi
    [ ! -e "$temporary" ] && [ ! -L "$temporary" ] || return 1
    cp -p "$source" "$temporary" || { rm -f "$temporary"; return 1; }
    cmp -s "$source" "$temporary" || { rm -f "$temporary"; return 1; }
    mv -f "$temporary" "$destination" || { rm -f "$temporary"; return 1; }
    regular_file "$destination" && cmp -s "$source" "$destination"
}

platform_restore()
{
    backup_valid || return 1
    ok=true
    for relative in $PLATFORM_TARGET_FILES
    do
        row="$(awk -F '\t' -v path="$relative" '$2==path {print; exit}' "$BACKUP_ROOT/inventory.tsv")"
        state="$(printf '%s\n' "$row" | awk -F '\t' '{print $1}')"
        destination="$(root_path "/$relative")"
        if [ "$state" = present ]; then
            atomic_install "$BACKUP_ROOT/$relative" "$destination" || ok=false
        else
            if [ -e "$destination" ] || [ -L "$destination" ]; then
                regular_file "$destination" && rm -f "$destination" || ok=false
            fi
        fi
    done
    sync || ok=false
    [ "$ok" = true ]
}

platform_install()
{
    installed_count=0
    payload_valid || return 1
    for relative in $PLATFORM_TARGET_FILES
    do
        destination="$(root_path "/$relative")"
        parent="${destination%/*}"
        if [ ! -e "$parent" ] && [ ! -L "$parent" ]; then
            mkdir -p "$parent" || return 1
        fi
        safe_directory "$parent" || return 1
    done
    for relative in $PLATFORM_TARGET_FILES
    do
        atomic_install "$PAYLOAD_ROOT/$relative" "$(root_path "/$relative")" || return 1
        platform_executable "$(root_path "/$relative")" || return 1
        installed_count=$((installed_count + 1))
        if [ -n "${BRORAY_HANDOFF_FAIL_AFTER:-}" ] &&
           [ "$installed_count" = "$BRORAY_HANDOFF_FAIL_AFTER" ]; then
            return 1
        fi
    done
    sync || return 1
    platform_current
}

service_call()
{
    service="$1"
    action="$2"
    script="$(root_path "/opt/etc/init.d/$service")"
    regular_file "$script" || return 1
    "$ASH" "$script" "$action"
}

application_rollback()
{
    request_valid || return 1
    operation_id="$(request_value '.operationId')" || return 1
    previous_slot="$(request_value '.previousSlot')" || return 1
    target_slot="$(request_value '.targetSlot')" || return 1
    valid_id "$operation_id" && valid_id "$previous_slot" && valid_id "$target_slot" || return 1
    [ "$(current_slot)" = "$target_slot" ] || return 1
    previous_root="$RELEASES_ROOT/$previous_slot"
    target_root="$RELEASES_ROOT/$target_slot"
    safe_directory "$previous_root" || return 1
    [ "$(sed -n '1p' "$previous_root/.broray-slot" 2>/dev/null || true)" = "$previous_slot" ] || return 1
    [ ! -e "$target_root" ] && [ ! -L "$target_root" ] || return 1

    for service in S28broray-subscriptions S27broray-auto-switch S25broray-web S24broray S23broray-monitor
    do
        service_call "$service" stop >/dev/null 2>&1 || true
    done
    mv "$CURRENT_PATH" "$target_root" || return 1
    if ! mv "$previous_root" "$CURRENT_PATH"; then
        mv "$target_root" "$CURRENT_PATH" 2>/dev/null || true
        return 1
    fi
    sync || return 1
    [ "$(current_slot)" = "$previous_slot" ] || return 1
    for service in S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions
    do
        desired="$(awk -F '\t' -v service="$service" '$1==service {print $2; exit}' "$OPERATION_ROOT/$operation_id/services.tsv" 2>/dev/null || true)"
        [ "$desired" = running ] || continue
        service_call "$service" start >/dev/null 2>&1 || return 1
    done
    return 0
}

recover_incomplete()
{
    phase="$(sed -n '1p' "$PHASE_FILE" 2>/dev/null || true)"
    case "$phase" in
        ''|queued|waiting) return 0 ;;
        preparing)
            if [ -e "$BACKUP_ROOT" ] || [ -L "$BACKUP_ROOT" ]; then
                backup_valid || return 1
                rm -rf "$BACKUP_ROOT" || return 1
            fi
            phase_write waiting
            ;;
        installing|restarting)
            backup_valid || return 1
            init_call stop >/dev/null 2>&1 || true
            platform_restore || return 1
            daemon_was_running="$(sed -n '1p' "$DAEMON_STATE_FILE" 2>/dev/null || true)"
            if [ "$daemon_was_running" = true ]; then
                init_call start >/dev/null 2>&1 || return 1
            fi
            application_rollback || return 1
            operation_id="$(request_value '.operationId' 2>/dev/null || true)"
            candidate_id="$(request_value '.candidateId' 2>/dev/null || true)"
            phase_write rolled-back || return 1
            status_write error false POWER_LOSS_ROLLED_BACK 'Прерванный переход обнаружен; предыдущая платформа и приложение восстановлены.' true true "$operation_id" "$candidate_id" || return 1
            return 3
            ;;
        complete)
            platform_current
            ;;
        rolled-back|rollback-failed) return 3 ;;
        *) return 1 ;;
    esac
}

lock_release()
{
    [ -d "$LOCK_DIR" ] && [ ! -L "$LOCK_DIR" ] || return 0
    owner_pid="$(sed -n '1p' "$LOCK_DIR/pid" 2>/dev/null || true)"
    owner_start="$(sed -n '1p' "$LOCK_DIR/starttime" 2>/dev/null || true)"
    self_start="$(process_starttime "$$" 2>/dev/null || true)"
    [ "$owner_pid" = "$$" ] && [ -n "$self_start" ] && [ "$owner_start" = "$self_start" ] || return 1
    rm -rf "$LOCK_DIR"
}

lock_acquire()
{
    if ! mkdir "$LOCK_DIR" 2>/dev/null; then
        safe_directory "$LOCK_DIR" || return 1
        owner_pid="$(sed -n '1p' "$LOCK_DIR/pid" 2>/dev/null || true)"
        owner_start="$(sed -n '1p' "$LOCK_DIR/starttime" 2>/dev/null || true)"
        process_matches "$owner_pid" "$owner_start" && return 2
        rm -rf "$LOCK_DIR" || return 1
        mkdir "$LOCK_DIR" 2>/dev/null || return 1
    fi
    self_start="$(process_starttime "$$")" || { rm -rf "$LOCK_DIR"; return 1; }
    printf '%s\n' "$$" >"$LOCK_DIR/pid" || return 1
    printf '%s\n' "$self_start" >"$LOCK_DIR/starttime" || return 1
}

worker_running()
{
    regular_file "$PID_FILE" || return 1
    pid="$(sed -n '1p' "$PID_FILE" 2>/dev/null || true)"
    start="$(sed -n '2p' "$PID_FILE" 2>/dev/null || true)"
    process_matches "$pid" "$start"
}

worker_identity_write()
{
    start="$(process_starttime "$$")" || return 1
    temporary="$STATE_ROOT/.worker.$$"
    printf '%s\n%s\n' "$$" "$start" >"$temporary" || return 1
    chmod 0600 "$temporary" 2>/dev/null || true
    mv -f "$temporary" "$PID_FILE"
}

worker_start()
{
    rm -f "$PID_FILE" || return 1
    if [ -n "${BRORAY_HANDOFF_WORKER_START_HOOK:-}" ]; then
        "$BRORAY_HANDOFF_WORKER_START_HOOK" "$ASH" "$SELF" finalize || return 1
    else
        # Keenetic Entware does not guarantee a start-stop-daemon binary.
        # Launch exactly as the persistent updater init script does.
        "$ASH" "$SELF" finalize >/dev/null 2>&1 </dev/null &
    fi

    start_attempt=0
    while [ "$start_attempt" -lt 10 ]
    do
        worker_running && return 0
        case "$(sed -n '1p' "$PHASE_FILE" 2>/dev/null || true)" in
            complete|rolled-back|rollback-failed) return 0 ;;
        esac
        sleep 1
        start_attempt=$((start_attempt + 1))
    done
    return 1
}

finalize()
{
    ensure_state_root || return 1
    lock_acquire
    lock_rc=$?
    [ "$lock_rc" -eq 0 ] || { [ "$lock_rc" -eq 2 ] && return 0; return 1; }
    trap 'lock_release >/dev/null 2>&1 || true' EXIT
    trap 'exit 129' HUP INT TERM
    worker_identity_write || return 1
    request_valid || {
        status_write error false HANDOFF_REQUEST_INVALID 'Не подтверждён переход от активной операции обновления.' false false '' '' || true
        return 1
    }
    recover_incomplete
    recovery_rc=$?
    [ "$recovery_rc" -eq 0 ] || { [ "$recovery_rc" -eq 3 ] && return 1; return 1; }
    operation_id="$(request_value '.operationId')"
    candidate_id="$(request_value '.candidateId')"
    status_write running true WAITING_FOR_SOURCE_UPDATER 'Завершается переключение приложения перед установкой универсального updater.' false false "$operation_id" "$candidate_id" || return 1
    phase_write waiting || return 1

    attempt=0
    while [ "$attempt" -lt "$WAIT_LIMIT" ]
    do
        operation_terminal_state
        terminal_rc=$?
        if [ "$terminal_rc" -eq 0 ] && ! updater_busy; then
            break
        fi
        if [ "$terminal_rc" -eq 1 ]; then
            status_write error false SOURCE_UPDATE_FAILED 'Исходное обновление завершилось ошибкой; updater platform не изменялся.' false false "$operation_id" "$candidate_id" || true
            return 1
        fi
        sleep 1
        attempt=$((attempt + 1))
    done
    [ "$attempt" -lt "$WAIT_LIMIT" ] || {
        status_write error false SOURCE_UPDATE_TIMEOUT 'Истекло время ожидания завершения исходного обновления; updater platform не изменялся.' false false "$operation_id" "$candidate_id" || true
        return 1
    }
    request_valid || return 1
    platform_current && {
        status_write success false UNIVERSAL_PLATFORM_READY 'Универсальный updater уже установлен.' false false "$operation_id" "$candidate_id" || return 1
        phase_write complete || return 1
        return 0
    }

    phase_write preparing || return 1
    backup_prepare || {
        status_write error false PLATFORM_BACKUP_FAILED 'Не удалось создать точную резервную копию updater platform.' false false "$operation_id" "$candidate_id" || true
        return 1
    }
    backup_valid || return 1
    daemon_was_running=false
    init_call status >/dev/null 2>&1 && daemon_was_running=true
    atomic_text "$DAEMON_STATE_FILE" "$daemon_was_running" || return 1
    init_call stop >/dev/null 2>&1 || {
        status_write error false PLATFORM_DAEMON_STOP_FAILED 'Не удалось безопасно остановить прежний updater.' false false "$operation_id" "$candidate_id" || true
        return 1
    }

    phase_write installing || return 1
    status_write running true INSTALLING_UNIVERSAL_PLATFORM 'Устанавливается единый updater, не зависящий от OPKG-релиза.' true false "$operation_id" "$candidate_id" || return 1
    transition_ok=true
    platform_install || transition_ok=false
    if [ "$transition_ok" = true ]; then
        phase_write restarting || transition_ok=false
    fi
    if [ "$transition_ok" = true ] && [ "${BRORAY_HANDOFF_FAILPOINT:-}" = crash-after-platform-install ]; then
        status_write running true INJECTED_POWER_LOSS 'Внедрённый сбой после установки platform.' true false "$operation_id" "$candidate_id" || true
        exit 99
    fi
    if [ "$transition_ok" = true ]; then
        init_call start >/dev/null 2>&1 || transition_ok=false
    fi
    if [ "$transition_ok" = true ]; then
        ready_attempt=0
        while [ "$ready_attempt" -lt 20 ]
        do
            daemon_ready && break
            sleep 1
            ready_attempt=$((ready_attempt + 1))
        done
        [ "$ready_attempt" -lt 20 ] || transition_ok=false
    fi
    if [ "$transition_ok" = true ]; then
        platform_current || transition_ok=false
    fi

    if [ "$transition_ok" != true ]; then
        init_call stop >/dev/null 2>&1 || true
        platform_restore_ok=true
        platform_restore || platform_restore_ok=false
        if [ "$daemon_was_running" = true ]; then
            init_call start >/dev/null 2>&1 || platform_restore_ok=false
        fi
        app_rollback_ok=true
        application_rollback || app_rollback_ok=false
        if [ "$platform_restore_ok" = true ] && [ "$app_rollback_ok" = true ]; then
            status_write error false PLATFORM_ACTIVATION_ROLLED_BACK 'Универсальный updater не запущен; предыдущая платформа и приложение восстановлены.' true true "$operation_id" "$candidate_id" || true
            phase_write rolled-back || true
        else
            status_write error false PLATFORM_ROLLBACK_FAILED 'Ошибка updater platform; автоматическое восстановление завершилось не полностью.' true true "$operation_id" "$candidate_id" || true
            phase_write rollback-failed || true
        fi
        return 1
    fi

    rm -rf "$BACKUP_ROOT" || return 1
    sync || return 1
    phase_write complete || return 1
    status_write success false UNIVERSAL_PLATFORM_READY 'Универсальный updater установлен и запущен.' true false "$operation_id" "$candidate_id" || return 1
    return 0
}

schedule()
{
    ensure_state_root || return 1
    payload_valid || {
        status_write error false PLATFORM_PAYLOAD_INVALID 'Встроенный updater platform не прошёл проверку.' false false '' '' || true
        return 1
    }
    worker_running && return 0
    existing_phase="$(sed -n '1p' "$PHASE_FILE" 2>/dev/null || true)"
    case "$existing_phase" in
        preparing|installing|restarting)
            request_valid || return 1
            if [ "$NO_ASYNC" = 1 ]; then
                finalize
                return $?
            fi
            worker_start
            return $?
            ;;
    esac
    if platform_current; then
        candidate_id="$(current_candidate 2>/dev/null || true)"
        status_write success false UNIVERSAL_PLATFORM_READY 'Универсальный updater установлен.' false false '' "$candidate_id" || return 1
        phase_write complete || return 1
        return 0
    fi
    binding="$(operation_binding)" || {
        status_write error false HANDOFF_SOURCE_OPERATION_INVALID 'Нет подтверждённой операции для перехода на универсальный updater.' false false '' '' || true
        return 1
    }
    request_write "$binding" || return 1
    operation_id="$(printf '%s\n' "$binding" | awk -F '\t' '{print $1}')"
    candidate_id="$(printf '%s\n' "$binding" | awk -F '\t' '{print $4}')"
    status_write running true HANDOFF_QUEUED 'Переход на универсальный updater поставлен в очередь.' false false "$operation_id" "$candidate_id" || return 1
    phase_write queued || return 1
    if [ "$NO_ASYNC" = 1 ]; then
        finalize
        return $?
    fi
    worker_start
}

status_json()
{
    if platform_current; then
        candidate_id="$(current_candidate 2>/dev/null || true)"
        jq -nc --arg contract "$CONTRACT" --arg candidateId "$candidate_id" --arg recordedAt "$(now)" '
          {schemaVersion:1,contract:$contract,state:"success",running:false,code:"UNIVERSAL_PLATFORM_READY",message:"Универсальный updater установлен.",mutationStarted:false,rollbackPerformed:false,operationId:null,candidateId:(if $candidateId=="" then null else $candidateId end),recordedAt:$recordedAt}'
        return 0
    fi
    if regular_file "$STATUS_FILE" && jq -e --arg contract "$CONTRACT" '
      keys==["candidateId","code","contract","message","mutationStarted","operationId","recordedAt","rollbackPerformed","running","schemaVersion","state"] and
      .schemaVersion==1 and .contract==$contract and
      (.state=="running" or .state=="success" or .state=="error") and
      (.running|type)=="boolean" and (.code|type)=="string" and
      (.message|type)=="string" and (.mutationStarted|type)=="boolean" and
      (.rollbackPerformed|type)=="boolean"
    ' "$STATUS_FILE" >/dev/null 2>&1; then
        cat "$STATUS_FILE"
        return 0
    fi
    jq -nc --arg contract "$CONTRACT" --arg recordedAt "$(now)" '
      {schemaVersion:1,contract:$contract,state:"error",running:false,code:"UNIVERSAL_PLATFORM_NOT_READY",message:"Переход на универсальный updater не завершён.",mutationStarted:false,rollbackPerformed:false,operationId:null,candidateId:null,recordedAt:$recordedAt}'
    return 1
}

case "${1:-}" in
    schedule) schedule ;;
    finalize) finalize ;;
    status) status_json ;;
    verify) payload_valid && platform_current ;;
    *)
        printf '%s\n' 'Использование: universal-platform-handoff.sh {schedule|finalize|status|verify}' >&2
        exit 2
        ;;
esac
