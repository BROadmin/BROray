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
HANDOFF_NAMESPACE=broray-platform-handoff
[ "${1:-}" != preflight ] || HANDOFF_NAMESPACE=broray-updater-preflight
STATE_ROOT="${BRORAY_HANDOFF_STATE_ROOT:-$(root_path "/opt/var/lib/$HANDOFF_NAMESPACE")}"
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
    # Every allowed target must be authenticated exactly once.
    for relative in $PLATFORM_TARGET_FILES; do
        [ "$(awk -v path="$relative" '$2==path {n++} END {print n+0}' "$PAYLOAD_ROOT/SHA256SUMS")" = 1 ] || return 1
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
    # Protected preflight settles obsolete preparation BEFORE app services run.
    # Never call native readiness here: the independent service host can be
    # waiting for this S25 child and cannot answer its own STATUS request.
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

# Authenticated installer entry: called BEFORE an update/reinstall is queued.
# No application, Xray, route, OPKG, or user configuration is changed here.
preflight_paths_safe()
{
    local pf_relative pf_parent pf_stop
    pf_stop="$(root_path /opt)"
    safe_directory "$pf_stop" || return 1
    for pf_relative in $PLATFORM_TARGET_FILES; do
        pf_parent="$(root_path "/$pf_relative")"
        pf_parent="${pf_parent%/*}"
        while [ "$pf_parent" != "$pf_stop" ]; do
            [ ! -L "$pf_parent" ] || return 1
            if [ -e "$pf_parent" ]; then safe_directory "$pf_parent" || return 1; fi
            [ "$pf_parent" != / ] && [ -n "$pf_parent" ] || return 1
            pf_parent="${pf_parent%/*}"
        done
    done
}

# Return 3 only when no protected generation/transaction exists. Directory and
# JSON reads below locate evidence; only the authenticated native entry grants
# mutation or readiness. Never choose a transaction by PID, age or mtime.
preflight_recovery_error()
{
    jq -nc --arg code "$1" '{ok:false,errorCode:$code,platformReady:false,activationAllowed:false}'
    return 75
}

preflight_native_phase()
{
    local step expected output rc
    step="$1"; expected="$2"; rc=0
    output="$("$pf_native" "$step" "$pf_live" "$pf_id" "$pf_migration" "$pf_nonce")" || rc=$?
    if [ "$rc" != 0 ]; then
        [ -z "$output" ] || printf '%s\n' "$output"
        [ -n "$output" ] || preflight_recovery_error PREFLIGHT_RECOVERY_UNCONFIRMED
        return "$rc"
    fi
    printf '%s\n' "$output" | jq -es --arg phase "$expected" \
      'length==1 and .[0].ok==true and .[0].phase==$phase and .[0].activationAllowed==false' >/dev/null || {
        preflight_recovery_error PREFLIGHT_RECOVERY_RESPONSE_INVALID
        return 75
    }
    pf_reply="$output"
}

# Use the authenticated payload's read-only entry to locate the installed
# generation. Its retained native closure, not init exit status or daemon.pid,
# proves readiness. Ledger fields only locate the second native STATUS proof.
preflight_installed()
{
    local reply ledger digest proof live
    reply="$(BRORAY_UPDATER_ROOT_PREFIX="$ROOT_PREFIX" "$ASH" "$PAYLOAD_ROOT/opt/etc/init.d/S22broray-updater" status)" || {
        [ -z "$reply" ] || printf '%s\n' "$reply"
        return 75
    }
    printf '%s\n' "$reply" | jq -es 'length==1 and .[0].ok==true and
      .[0].platformReady==true and .[0].activationAllowed==false and
      (.[0].generationId|type=="string" and length==24 and startswith("g-") and
        all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==95 or .==45))' >/dev/null || return 75
    pf_old_generation="$(printf '%s\n' "$reply" | jq -er .generationId)" || return 75
    ledger="$UPDATER_STATE_ROOT/generations/$pf_old_generation/state.json"
    regular_file "$ledger" || return 75
    pf_old_manifest="$(jq -er .platformManifestSha256 "$ledger")" || return 75
    digest="$(jq -er .platformLaunch.nativeSha256 "$ledger")" || return 75
    valid_sha256 "$digest" && valid_sha256 "$pf_old_manifest" || return 75
    pf_old_native="$UPDATER_STATE_ROOT/runtimes/$digest/runtime"
    regular_file "$pf_old_native" && [ -x "$pf_old_native" ] || return 75
    proof="$(sha256sum "$pf_old_native")" || return 75
    [ "${proof%% *}" = "$digest" ] || return 75
    pf_old_origin="$(jq -er .platformLaunch.operationId "$ledger")" || return 75
    pf_old_nonce="$(jq -er .platformLaunch.stopNonce "$ledger")" || return 75
    proof="$("$pf_old_native" control "${ledger%/state.json}" STATUS "$pf_old_generation" "$pf_old_manifest" "$pf_old_origin" "$pf_old_nonce")" || return 75
    printf '%s\n' "$proof" | jq -es --arg gen "$pf_old_generation" --arg manifest "$pf_old_manifest" --arg native "$digest" '
      length==1 and .[0].contract=="broray-updater-generation/2" and .[0].supervisedFromBirth==true and
      .[0].state=="RUNNING" and .[0].platformReady==true and .[0].generationId==$gen and
      .[0].platformManifestSha256==$manifest and .[0].platformLaunch.nativeSha256==$native' >/dev/null || return 75
    proof="$(sha256sum "$2/bin/broray-updater-generation")" || return 75
    if [ "$pf_old_manifest" != "$1" ] || [ "$digest" != "${proof%% *}" ]; then return 4; fi
    printf '%s\n' "$reply" | jq -c --arg id "$pf_old_origin" '.phase="PREFLIGHT_COMPLETED"|.operationId=$id|.replayed=true'
}

preflight_replacement_phase()
{
    local reply rc
    rc=0
    reply="$(broray_ops_call "platform-replacement-$1" "$pf_id" "$pf_nonce")" || rc=$?
    if [ "$rc" != 0 ]; then
        [ -z "$reply" ] || printf '%s\n' "$reply"
        [ -n "$reply" ] || preflight_recovery_error PREFLIGHT_REPLACEMENT_UNCONFIRMED
        return "$rc"
    fi
    printf '%s\n' "$reply" | jq -es --arg phase "$2" 'length==1 and .[0].ok==true and
      .[0].phase==$phase and .[0].activationAllowed==false' >/dev/null || {
        preflight_recovery_error PREFLIGHT_RECOVERY_RESPONSE_INVALID; return 75
    }
    pf_reply="$reply"
}

preflight_replacement_resume()
{
    local pf_id pf_nonce pf_op pf_reply
    pf_id="$1"; pf_nonce="$2"; pf_op="$BRORAY_STATE_ROOT/operations/$pf_id"
    # Presence chooses the validator, never supplies proof. A START intent may
    # already have a live B generation; do not re-enter an A-only install step.
    if [ ! -e "$pf_op/platform-replacement-start" ] && [ ! -L "$pf_op/platform-replacement-start" ]; then
        if [ ! -e "$pf_op/platform-replacement-backup.record" ] && [ ! -L "$pf_op/platform-replacement-backup.record" ]; then
            preflight_replacement_phase backup BACKUP_READY || return $?
        fi
        preflight_replacement_phase install INSTALLED || return $?
        preflight_replacement_phase start-intent START_INTENT || return $?
    fi
    preflight_replacement_phase start READY || return $?
    preflight_replacement_phase commit COMMITTED || return $?
    preflight_replacement_phase complete PREFLIGHT_COMPLETED || return $?
    printf '%s\n' "$pf_reply"
}

preflight_generation_stop_resume()
{
    local attempt reply rc
    attempt=0
    while [ "$attempt" -lt 12 ]; do
        attempt=$((attempt+1)); rc=0
        reply="$(broray_ops_call platform-preflight-resume-generation-stop "$1" "$2")" || rc=$?
        if [ "$rc" != 0 ]; then
            [ -z "$reply" ] || printf '%s\n' "$reply"
            [ -n "$reply" ] || preflight_recovery_error PREFLIGHT_REPLACEMENT_STOP_UNCONFIRMED
            return "$rc"
        fi
        if printf '%s\n' "$reply" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="STOPPED" and
          .[0].serviceStopped==true and .[0].platformReady==false' >/dev/null; then return 0; fi
        printf '%s\n' "$reply" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="STOPPING" and
          .[0].serviceStopped==false and .[0].platformReady==false' >/dev/null || {
            preflight_recovery_error PREFLIGHT_RECOVERY_RESPONSE_INVALID; return 75
        }
        [ "$attempt" = 12 ] || sleep 1
    done
    preflight_recovery_error PREFLIGHT_REPLACEMENT_STOP_UNCONFIRMED
}

preflight_resume()
{
    local expected code pf_id pf_op pf_state pf_binding pf_native pf_live pf_migration pf_nonce pf_reply
    local target file selected actual running
    expected="$1"; code="$2"; pf_id=''
    if [ -L "$BRORAY_ROUTES_API_LOCK" ]; then
        target="$(readlink "$BRORAY_ROUTES_API_LOCK")" || return 75
        case "$target" in
          "$BRORAY_STATE_ROOT/operations/"op-*/fence)
            pf_id="${target%/fence}"; pf_id="${pf_id##*/}" ;;
          *) preflight_recovery_error PREFLIGHT_RECOVERY_FENCE_UNCONFIRMED; return 75 ;;
        esac
    elif [ -e "$BRORAY_ROUTES_API_LOCK" ]; then
        return 3 # Existing admission classifies/preserves legacy or foreign locks.
    else
        if [ -e "$UPDATER_STATE_ROOT/generations" ] || [ -L "$UPDATER_STATE_ROOT/generations" ]; then
            preflight_installed "$expected" "$code"
            return $?
        fi
        # Lost completion replies have no global fence. Discover one matching
        # completed operation, then re-prove its live generation below. A bare
        # terminal flag is never readiness; multiple matches are ambiguous.
        selected=''
        for file in "$BRORAY_STATE_ROOT/operations"/op-*/state.json; do
            regular_file "$file" || continue
            jq -e --arg sha "$expected" '.operation=="system:platform-preflight" and
              .state=="completed" and .running==false and
              .platformPreflight.expectedPlatformManifestSha256==$sha' "$file" >/dev/null 2>&1 || continue
            # A service stop preserves its launch origin; it is not another
            # installation of these bytes. Preserve/refuse malformed evidence
            # instead of using it as an origin or silently discarding it.
            if jq -e 'has("serviceStop")' "$file" >/dev/null; then
                jq -e '((.serviceStop.schemaVersion==1 and .serviceStop.contract=="broray-service-stop/1") or
                  (.serviceStop.schemaVersion==2 and .serviceStop.contract=="broray-service-stop/2" and .serviceStop.originKind=="supervised-replacement")) and
                  (.serviceStop.originOperationId|type=="string" and
                    length>3 and length<=96 and startswith("op-") and
                    (.[3:]|all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==95 or .==45)))' "$file" >/dev/null || {
                    preflight_recovery_error PREFLIGHT_RECOVERY_EVIDENCE_INCOMPLETE; return 75
                }
                continue
            fi
            [ -z "$selected" ] || { preflight_recovery_error PREFLIGHT_RECOVERY_AMBIGUOUS; return 75; }
            selected="${file%/state.json}"; selected="${selected##*/}"
        done
        pf_id="$selected"
    fi
    if [ -z "$pf_id" ]; then
        if [ -e "$UPDATER_STATE_ROOT/generations" ] || [ -L "$UPDATER_STATE_ROOT/generations" ]; then
            preflight_recovery_error PREFLIGHT_GENERATION_UNCONFIRMED
            return 75
        fi
        return 3
    fi
    valid_id "$pf_id" || return 75
    pf_op="$BRORAY_STATE_ROOT/operations/$pf_id"; pf_state="$pf_op/state.json"
    regular_file "$pf_state" || return 75
    jq -e '.operation=="system:platform-preflight"' "$pf_state" >/dev/null 2>&1 || return 3
    if jq -e '.platformPreflight.generationStop!=null' "$pf_state" >/dev/null; then
        jq -e --arg sha "$expected" '.platformPreflight.expectedPlatformManifestSha256==$sha and
          (.platformPreflight.phase=="STOP_INTENT" or .platformPreflight.phase=="STOPPED") and
          (has("serviceStop")|not)' "$pf_state" >/dev/null || {
            preflight_recovery_error PREFLIGHT_TARGET_CHANGED; return 75
        }
        pf_nonce="$(jq -er .platformPreflight.stopNonce "$pf_state")" || return 75
        if jq -e '.platformPreflight.phase=="STOP_INTENT"' "$pf_state" >/dev/null; then
            preflight_generation_stop_resume "$pf_id" "$pf_nonce" || return $?
        fi
        preflight_replacement_resume "$pf_id" "$pf_nonce"
        return $?
    fi
    pf_binding="$pf_op/platform-bootguard.json"
    if [ ! -e "$pf_binding" ] && [ ! -L "$pf_binding" ] &&
       jq -e '.platformPreflight.phase=="STOP_INTENT"' "$pf_state" >/dev/null 2>&1; then
        # A failed, fully staged attempt may target an older manifest. First
        # prove it never reached live mutation and abandon only its own fence.
        # The new target still needs fresh admission and all normal gates.
        pf_nonce="$(jq -er .platformPreflight.stopNonce "$pf_state")" || return 75
        if ! pf_reply="$(broray_ops_call platform-preflight-discard-stage "$pf_id" "$pf_nonce")"; then
            [ -z "$pf_reply" ] || printf '%s\n' "$pf_reply"
            return 75
        fi
        printf '%s\n' "$pf_reply" | jq -es --arg id "$pf_id" 'length==1 and .[0].ok==true and
          .[0].phase=="PREFLIGHT_STAGING_ABORTED" and .[0].operationId==$id and
          .[0].platformReady==false and .[0].activationAllowed==false and
          .[0].serviceStopped==false and .[0].signalsAuthorized==false' >/dev/null || {
            preflight_recovery_error PREFLIGHT_RECOVERY_RESPONSE_INVALID; return 75
        }
        return 3
    fi
    jq -e --arg sha "$expected" '.platformPreflight.expectedPlatformManifestSha256==$sha' "$pf_state" >/dev/null || {
        preflight_recovery_error PREFLIGHT_TARGET_CHANGED; return 75
    }
    regular_file "$pf_binding" || { preflight_recovery_error PREFLIGHT_RECOVERY_EVIDENCE_INCOMPLETE; return 75; }
    pf_migration="$(jq -er .migrationIntentSha256 "$pf_binding")" || return 75
    pf_nonce="$(jq -er .stopNonce "$pf_binding")" || return 75
    valid_sha256 "$pf_migration" || return 75
    [ "${#pf_nonce}" = 32 ] || return 75
    case "$pf_nonce" in *[!0-9a-f]*) return 75 ;; esac
    # Execute the authenticated slot's binary, never a path supplied by state.
    pf_native="$code/bin/broray-updater-generation"
    regular_file "$pf_native" && [ -x "$pf_native" ] || return 75
    actual="$(sha256sum "$pf_native")" || return 75; actual="${actual%% *}"
    jq -e --arg sha "$actual" --arg id "$pf_id" --arg expected "$expected" \
      '.nativeSha256==$sha and .operationId==$id and .expectedPlatformManifestSha256==$expected' "$pf_binding" >/dev/null || {
        preflight_recovery_error PREFLIGHT_NATIVE_GENERATION_MISMATCH; return 75
    }
    pf_live="$(root_path /)"; pf_live="${pf_live%/}"; [ -n "$pf_live" ] || pf_live=/
    running="$(jq -er '.running|tostring' "$pf_state")" || return 75
    if [ "$running" = true ]; then
        if [ ! -e "$pf_op/platform-install.record" ] && [ ! -L "$pf_op/platform-install.record" ]; then
            preflight_native_phase recovery-inspect BOOT_CONTEXT_VERIFIED || return $?
            if ! printf '%s\n' "$pf_reply" | jq -e '.oldBootEnded==true' >/dev/null; then
                jq -nc --arg id "$pf_id" '{ok:false,errorCode:"UPDATER_LEGACY_REBOOT_REQUIRED",phase:"REBOOT_REQUIRED",
                  operationId:$id,platformReady:false,serviceStopped:false,activationAllowed:false,signalsAuthorized:false}'
                return 75
            fi
            preflight_native_phase recovery-retire STOPPED || return $?
            preflight_native_phase recovery-backup BACKUP_READY || return $?
        fi
        preflight_native_phase recovery-install INSTALLED || return $?
        preflight_native_phase recovery-start-intent START_INTENT || return $?
        preflight_native_phase recovery-start READY || return $?
        preflight_native_phase recovery-commit COMMITTED || return $?
    fi
    preflight_native_phase recovery-complete PREFLIGHT_COMPLETED || return $?
    printf '%s\n' "$pf_reply"
}

preflight_run()
{
    local pf_expected pf_code pf_rc pf_old_generation pf_old_manifest pf_old_native pf_old_origin pf_old_nonce
    local pf_replace
    pf_expected="${1:-}"
    valid_sha256 "$pf_expected" &&
        [ "$(payload_manifest_sha)" = "$pf_expected" ] && payload_valid || return 1
    for pf_code in $PLATFORM_TARGET_FILES; do
        [ -x "$PAYLOAD_ROOT/$pf_code" ] || return 1
    done
    preflight_paths_safe || return 1
    pf_code="${BRORAY_OPS_CODE_ROOT:-$APP_ROOT}"
    regular_file "$pf_code/lib/operation-client.sh" || return 74
    BRORAY_ROOT="$APP_ROOT"
    BRORAY_STATE_ROOT="$(root_path /opt/var/lib/broray)"
    BRORAY_ROUTES_API_LOCK="$(root_path /opt/var/lock/broray/global-operation.lock)"
    BRORAY_OPS_UPDATER_ROOT="$UPDATER_STATE_ROOT"
    BRORAY_LEGACY_GLOBAL_LOCK="$(root_path /tmp/broray-global-operation.lock)"
    BRORAY_OPS_CODE_ROOT="$pf_code"
    export BRORAY_ROOT BRORAY_STATE_ROOT BRORAY_ROUTES_API_LOCK BRORAY_OPS_UPDATER_ROOT
    export BRORAY_LEGACY_GLOBAL_LOCK BRORAY_OPS_CODE_ROOT
    . "$pf_code/lib/operation-client.sh" || return 74
    pf_rc=0
    preflight_resume "$pf_expected" "$pf_code" || pf_rc=$?
    pf_replace=false
    case "$pf_rc" in 0) return 0 ;; 3) ;; 4) pf_replace=true ;; *) return "$pf_rc" ;; esac
    pf_rc=0
    broray_ops_preflight_admit "$pf_expected" || pf_rc=$?
    if [ "$pf_rc" != 0 ]; then
        [ -z "${BRORAY_OPS_LAST_ERROR:-}" ] || printf '%s\n' "$BRORAY_OPS_LAST_ERROR"
        return "$pf_rc"
    fi
    # Generic finish can retire only PREPARED. Once STOP_INTENT is durable,
    # every failure preserves the protected fence and its exact evidence.
    trap 'broray_ops_finish failed OPERATION_FAILED >/dev/null 2>&1 || true' EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    broray_ops_preflight_stop_intent "$pf_expected" || return $?
    if [ "$pf_replace" = true ]; then
        # Only the old authenticated runtime can address A. All platform
        # writers/install/start calls still come from the new verified slot.
        BRORAY_OPS_GENERATION="$pf_old_native"
        export BRORAY_OPS_GENERATION
        # Keep this in the admitted owner shell (no command substitution).
        broray_ops_preflight_stop_generation "$pf_old_generation" "$pf_old_manifest" >/dev/null || {
            preflight_recovery_error PREFLIGHT_REPLACEMENT_STOP_UNCONFIRMED; return 75
        }
        unset BRORAY_OPS_GENERATION
        trap - EXIT HUP INT TERM
        preflight_replacement_resume "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_PREFLIGHT_STOP_NONCE"
        return $?
    fi
    broray_ops_preflight_bind_service || return $?
    # Call in this owner shell, not a pipeline/command-substitution child.
    # An observation-only legacy binding never authorizes a stop signal.
    broray_ops_preflight_stage_bootguard >/dev/null || return $?
    trap - EXIT HUP INT TERM
    jq -nc --arg id "$BRORAY_BACKGROUND_OPERATION_ID" --arg sha "$pf_expected" '
      {ok:false,errorCode:"UPDATER_LEGACY_REBOOT_REQUIRED",phase:"REBOOT_REQUIRED",
       operationId:$id,expectedPlatformManifestSha256:$sha,platformReady:false,
       serviceStopped:false,activationAllowed:false,signalsAuthorized:false}' || return 74
    return 75
}

# A legacy preparing phase has not changed platform files. Supersede it only
# after the protected transaction has proved the exact new generation ready.
# Keep the old request and backup as evidence. This runs before enqueue/service
# launch, outside the independent app-service host. Other unfinished phases
# require their original recovery; an existing worker fence is never removed.
preflight_settle_legacy()
(
    STATE_ROOT="$(root_path /opt/var/lib/broray-platform-handoff)"
    [ -e "$STATE_ROOT" ] || [ -L "$STATE_ROOT" ] || return 0
    safe_directory "$STATE_ROOT" || return 75
    STATUS_FILE="$STATE_ROOT/status.json"
    PHASE_FILE="$STATE_ROOT/phase"
    PID_FILE="$STATE_ROOT/worker.pid"
    LOCK_DIR="$STATE_ROOT/worker.lock"
    regular_file "$PHASE_FILE" || return 75
    existing_phase="$(sed -n '1p' "$PHASE_FILE")"
    case "$existing_phase" in
        complete|rolled-back|rollback-failed) return 0 ;;
        preparing) ;;
        *) return 75 ;;
    esac
    worker_running && return 75
    # mkdir is exclusion, not permission to clean an unproven old lock.
    mkdir "$LOCK_DIR" 2>/dev/null || return 75
    self_start="$(process_starttime "$$")" || return 75
    [ -n "$self_start" ] || return 75
    printf '%s\n' "$$" >"$LOCK_DIR/pid" || return 75
    printf '%s\n' "$self_start" >"$LOCK_DIR/starttime" || return 75
    trap 'lock_release >/dev/null 2>&1 || true' EXIT
    worker_running && return 75
    [ "$(sed -n '1p' "$PHASE_FILE")" = preparing ] || return 75
    platform_current && preflight_installed "$1" "$2" >/dev/null || return 75
    [ "$pf_old_manifest" = "$1" ] || return 75
    candidate_id="$(current_candidate)" || return 75
    status_write success false LEGACY_PREPARATION_SUPERSEDED 'Подготовка прежнего перехода завершена: подтверждён текущий updater.' false false '' "$candidate_id" || return 75
    phase_write complete && sync || return 75
)

preflight()
{
    preflight_run "$@" || return $?
    preflight_settle_legacy "$1" "${BRORAY_OPS_CODE_ROOT:-$APP_ROOT}"
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


# Read-only platform admission check; not a replacement for full preflight.
# This command owns/settles coordinator bookkeeping but NEVER controls services.
preflight_admission_only()
{
    local expected code admission_id admission_rc
    expected="${1:-}"
    valid_sha256 "$expected" && [ "$(payload_manifest_sha)" = "$expected" ] && payload_valid || return 1
    code="${BRORAY_OPS_CODE_ROOT:-$APP_ROOT}"
    [ -f "$code/lib/operation-client.sh" ] && [ ! -L "$code/lib/operation-client.sh" ] || return 74
    BRORAY_ROOT="$APP_ROOT"
    BRORAY_STATE_ROOT="$(root_path /opt/var/lib/broray)"
    BRORAY_ROUTES_API_LOCK="$(root_path /opt/var/lock/broray/global-operation.lock)"
    BRORAY_OPS_UPDATER_ROOT="$UPDATER_STATE_ROOT"
    BRORAY_LEGACY_GLOBAL_LOCK="$(root_path /tmp/broray-global-operation.lock)"
    BRORAY_OPS_CODE_ROOT="$code"
    export BRORAY_ROOT BRORAY_STATE_ROOT BRORAY_ROUTES_API_LOCK BRORAY_OPS_UPDATER_ROOT
    export BRORAY_LEGACY_GLOBAL_LOCK BRORAY_OPS_CODE_ROOT
    . "$code/lib/operation-client.sh" || return 74
    admission_rc=0
    broray_ops_preflight_admit "$expected" || admission_rc=$?
    [ "$admission_rc" = 0 ] || return "$admission_rc"
    trap 'broray_ops_finish failed OPERATION_FAILED >/dev/null 2>&1 || true' EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    admission_id="$BRORAY_BACKGROUND_OPERATION_ID"
    broray_ops_finish completed || return 75
    trap - EXIT HUP INT TERM
    jq -nc --arg id "$admission_id" \
      '{ok:true,code:"PREFLIGHT_ADMISSION_ONLY",operationId:$id,phase:"PREPARED",platformMutationAllowed:false,platformReady:false}'
}

case "${1:-}" in
    preflight-admission) preflight_admission_only "${2:-}" ;;
    preflight) preflight "${2:-}" ;;
    schedule) schedule ;;
    finalize) finalize ;;
    status) status_json ;;
    verify) payload_valid && platform_current ;;
    *)
        printf '%s\n' 'Использование: universal-platform-handoff.sh {preflight SHA256|schedule|finalize|status|verify}' >&2
        exit 2
        ;;
esac
