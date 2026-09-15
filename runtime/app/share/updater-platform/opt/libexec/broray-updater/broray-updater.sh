#!/opt/bin/ash

# BROray compact app-slot updater with one shared Xray runtime.
# One long-lived daemon is the only writer. CGI processes only enqueue durable
# requests and read durable status; they never start workers or touch OPKG.

set -u

PATH="${BRORAY_UPDATER_PATH:-/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin}"
LC_ALL=C
export PATH LC_ALL

UPDATER_VERSION=5
UPDATER_ENGINE="broray-updater/$UPDATER_VERSION"
LIFECYCLE_CONTRACT="compact-app-rename/1"

ROOT_PREFIX="${BRORAY_UPDATER_ROOT_PREFIX:-}"

root_path()
{
    case "$1" in
        /*) ;;
        *) return 2 ;;
    esac

    if [ -n "$ROOT_PREFIX" ]; then
        printf '%s%s\n' "${ROOT_PREFIX%/}" "$1"
    else
        printf '%s\n' "$1"
    fi
}

APP_ROOT="${BRORAY_UPDATER_APP_ROOT:-$(root_path /opt/broray)}"
STATE_ROOT="${BRORAY_UPDATER_STATE_ROOT:-$(root_path /opt/var/lib/broray-updater)}"
OPERATION_ROOT="${BRORAY_UPDATER_OPERATION_ROOT:-$(root_path /opt/var/lib/broray/operations)}"
OPERATION_POINTER="${BRORAY_UPDATER_OPERATION_POINTER:-$(root_path /opt/var/lib/broray/last-operation)}"
RELEASES_ROOT="$APP_ROOT/releases"
CURRENT_PATH="$APP_ROOT/current"
QUEUE_ROOT="$STATE_ROOT/queue"
WORK_ROOT="${BRORAY_UPDATER_WORK_ROOT:-$(root_path /tmp/broray-updater-work)}"
SLOT_META_ROOT="$STATE_ROOT/slots"
XRAY_RUNTIME="$APP_ROOT/runtime/xray"
XRAY_WRAPPER="$(root_path /opt/libexec/broray-updater/xray-wrapper)"
OPKG_CONTROL="${BRORAY_UPDATER_OPKG_CONTROL:-$(root_path /opt/lib/opkg/info/broray.control)}"
CACHE_FILE="$STATE_ROOT/release-index.json"
SIGNED_INDEX_CACHE="$STATE_ROOT/release-index.signed.json"
CACHE_SIGNATURE_FILE="$STATE_ROOT/release-index.json.minisig"
CHANNEL_FILE="$STATE_ROOT/release-index-url"
REQUEST_LOCK="$STATE_ROOT/request.lock"
GLOBAL_OPERATION_LOCK="${BRORAY_UPDATER_GLOBAL_OPERATION_LOCK:-$(root_path /opt/var/lock/broray/global-operation.lock)}"
LEGACY_GLOBAL_OPERATION_LOCK="${BRORAY_UPDATER_LEGACY_GLOBAL_OPERATION_LOCK:-$(root_path /tmp/broray-global-operation.lock)}"
ROUTES_OPERATION_ROOT="${BRORAY_UPDATER_ROUTES_OPERATION_ROOT:-$(root_path /opt/broray/routes/operations)}"
DAEMON_LOCK="$STATE_ROOT/daemon.lock"
DAEMON_PID="$STATE_ROOT/daemon.pid"
DAEMON_READY="$STATE_ROOT/daemon.ready"
DAEMON_LOG="$STATE_ROOT/updater.log"
ASH_BIN="${BRORAY_UPDATER_ASH:-$(root_path /opt/bin/ash)}"
SERVICE_HOOK="${BRORAY_UPDATER_SERVICE_HOOK:-}"
HEALTH_HOOK="${BRORAY_UPDATER_HEALTH_HOOK:-}"
WEBUI_BACKEND_HOOK="${BRORAY_UPDATER_WEBUI_BACKEND_HOOK:-}"
ROUTE_HOOK="${BRORAY_UPDATER_ROUTE_HOOK:-}"
FETCH_HOOK="${BRORAY_UPDATER_FETCH_HOOK:-}"
SPACE_HOOK="${BRORAY_UPDATER_SPACE_HOOK:-}"
ADMISSION_HOOK="${BRORAY_UPDATER_ADMISSION_HOOK:-}"
TEST_MODE="${BRORAY_UPDATER_TEST_MODE:-0}"
ASSUME_DAEMON="${BRORAY_UPDATER_ASSUME_DAEMON:-0}"
RUN_ONCE="${BRORAY_UPDATER_RUN_ONCE:-0}"
NO_SLEEP="${BRORAY_UPDATER_NO_SLEEP:-0}"
FAILPOINT="${BRORAY_UPDATER_FAILPOINT:-}"
ARCHITECTURE="${BRORAY_UPDATER_ARCHITECTURE:-aarch64-3.10}"
SIGNATURE_BIN="${BRORAY_UPDATER_SIGNATURE_BIN:-$(root_path /opt/libexec/broray-updater/minisign)}"
SIGNATURE_BIN_SHA256="${BRORAY_UPDATER_SIGNATURE_BIN_SHA256:-cec9f88be8c975af76854a53b4d49c3d257feae38d916edb0d16fb55aacd3000}"
RELEASE_PUBLIC_KEY="${BRORAY_UPDATER_RELEASE_PUBLIC_KEY:-RWTlNQ0uUR+MmbfELjB7v4VVML2xgK4Ri1ZmR8ZqDqomMQ0GJPMpYO/O}"

CURRENT_OPERATION_ID=""
CURRENT_OPERATION_DIR=""
CURRENT_OPERATION_LOG=""
REQUEST_PUBLISHED=false

now()
{
    date -u '+%Y-%m-%dT%H:%M:%SZ'
}

epoch()
{
    date '+%s'
}

valid_id()
{
    case "${1:-}" in
        ''|.*|-*|*[!A-Za-z0-9._-]*) return 1 ;;
    esac
    return 0
}

valid_sha256()
{
    [ "${#1}" -eq 64 ] || return 1
    case "$1" in
        *[!0-9a-f]*) return 1 ;;
    esac
    return 0
}

valid_positive_integer()
{
    case "${1:-}" in
        ''|*[!0-9]*) return 1 ;;
    esac
    [ "$1" -gt 0 ]
}

release_version_parts()
{
    local release_version version suffix major minor patch release old_ifs
    release_version="$1"
    case "$release_version" in *-r*) ;; *) return 1 ;; esac
    version="${release_version%%-r*}"
    suffix="${release_version#*-r}"
    old_ifs="$IFS"
    IFS=.
    set -- $version
    IFS="$old_ifs"
    [ "$#" -eq 3 ] || return 1
    major="$1"; minor="$2"; patch="$3"
    case "$major.$minor.$patch" in *[!0-9.]*|.*|*..*|*.) return 1 ;; esac
    release="$suffix"
    case "$major$minor$patch$release" in ''|*[!0-9]*) return 1 ;; esac
    [ "$release" -gt 0 ] || return 1
    printf '%s\t%s\t%s\t%s\n' "$major" "$minor" "$patch" "$release"
}

release_relation()
{
    local current available current_parts available_parts old_ifs c1 c2 c3 c4 a1 a2 a3 a4
    current="$1"
    available="$2"
    [ -n "$available" ] || { printf '%s\n' uncomparable; return 0; }
    [ -n "$current" ] || { printf '%s\n' uncomparable; return 0; }
    current_parts="$(release_version_parts "$current" 2>/dev/null)" || { printf '%s\n' uncomparable; return 0; }
    available_parts="$(release_version_parts "$available" 2>/dev/null)" || { printf '%s\n' uncomparable; return 0; }
    old_ifs="$IFS"; IFS="$(printf '\t')"
    read -r c1 c2 c3 c4 <<EOF_CURRENT
$current_parts
EOF_CURRENT
    read -r a1 a2 a3 a4 <<EOF_AVAILABLE
$available_parts
EOF_AVAILABLE
    IFS="$old_ifs"
    for pair in "$a1:$c1" "$a2:$c2" "$a3:$c3" "$a4:$c4"; do
        available_part="${pair%%:*}"
        current_part="${pair#*:}"
        if [ "$available_part" -gt "$current_part" ]; then printf '%s\n' newer; return 0; fi
        if [ "$available_part" -lt "$current_part" ]; then printf '%s\n' older; return 0; fi
    done
    printf '%s\n' same
}

opkg_control_value()
{
    local field
    field="$1"
    [ -f "$OPKG_CONTROL" ] && [ ! -L "$OPKG_CONTROL" ] || return 1
    awk -F ': ' -v key="$field" '
      $1 == key {count++; value=substr($0, length($1) + 3)}
      END {if (count != 1 || value == "") exit 1; print value}
    ' "$OPKG_CONTROL"
}

target_package_track_valid()
{
    local target_json target_package target_architecture installed_package installed_architecture
    target_json="$1"
    target_package="$(printf '%s\n' "$target_json" | jq -er '.packageVersion')" || return 1
    target_architecture="$(printf '%s\n' "$target_json" | jq -er '.architecture')" || return 1
    installed_package="$(opkg_control_value Version)" || return 1
    installed_architecture="$(opkg_control_value Architecture)" || return 1
    [ -n "$target_package" ] || return 1
    [ -n "$installed_package" ] || return 1
    [ "$installed_architecture" = "$ARCHITECTURE" ] || return 1
    [ "$target_architecture" = "$ARCHITECTURE" ]
}

process_start_ticks()
{
    local process_pid
    process_pid="$1"
    case "$process_pid" in ''|*[!0-9]*) return 1 ;; esac
    [ -r "/proc/$process_pid/stat" ] || return 1
    sed 's/.*) //' "/proc/$process_pid/stat" 2>/dev/null |
        awk '{print $20; exit}'
}

ensure_layout()
{
    local command_name
    for command_name in awk chmod cmp cp curl date df find gzip jq kill ln mkdir mv readlink rm sed sha256sum sleep sort sync tail tar tr wc
    do
        command -v "$command_name" >/dev/null 2>&1 || return 1
    done

    mkdir -p \
        "$STATE_ROOT" \
        "$QUEUE_ROOT" \
        "$WORK_ROOT" \
        "$SLOT_META_ROOT" \
        "$OPERATION_ROOT" \
        "$RELEASES_ROOT" || return 1

    chmod 700 \
        "$STATE_ROOT" \
        "$QUEUE_ROOT" \
        "$WORK_ROOT" \
        "$SLOT_META_ROOT" \
        "$OPERATION_ROOT" 2>/dev/null || true
}

atomic_json_file()
{
    local target temporary
    target="$1"
    temporary="$target.tmp.$$"

    cat >"$temporary" || {
        rm -f "$temporary"
        return 1
    }

    jq -e 'type == "object"' "$temporary" >/dev/null 2>&1 || {
        rm -f "$temporary"
        return 1
    }

    chmod 600 "$temporary" 2>/dev/null || true
    mv -f "$temporary" "$target"
}

atomic_text_file()
{
    local target temporary
    target="$1"
    temporary="$target.tmp.$$"

    cat >"$temporary" || {
        rm -f "$temporary"
        return 1
    }

    chmod 600 "$temporary" 2>/dev/null || true
    mv -f "$temporary" "$target"
}

json_error()
{
    local code message details
    code="$1"
    message="$2"
    details="${3:-}"

    jq -nc \
        --arg code "$code" \
        --arg message "$message" \
        --arg details "$details" '
        {
          ok:false,
          error:{
            code:$code,
            message:$message,
            details:(if $details == "" then null else $details end)
          }
        }'
}

operation_select()
{
    local operation_id
    operation_id="$1"
    valid_id "$operation_id" || return 1

    CURRENT_OPERATION_ID="$operation_id"
    CURRENT_OPERATION_DIR="$OPERATION_ROOT/$operation_id"
    CURRENT_OPERATION_LOG="$CURRENT_OPERATION_DIR/log.txt"

    mkdir -p "$CURRENT_OPERATION_DIR" || return 1
    chmod 700 "$CURRENT_OPERATION_DIR" 2>/dev/null || true
}

operation_publish_pointer()
{
    valid_id "$CURRENT_OPERATION_ID" || return 1
    printf '%s\n' "$CURRENT_OPERATION_ID" | atomic_text_file "$OPERATION_POINTER"
}

operation_log()
{
    local message
    message="$*"
    [ -n "$CURRENT_OPERATION_LOG" ] || return 0
    printf '%s  %s\n' "$(now)" "$message" >>"$CURRENT_OPERATION_LOG" 2>/dev/null || true
}

status_write()
{
    local operation state stage progress message error_code mutation_started rollback_performed running
    operation="$1"
    state="$2"
    stage="$3"
    progress="$4"
    message="$5"
    error_code="${6:-}"
    mutation_started="${7:-false}"
    rollback_performed="${8:-false}"
    running=false

    case "$state" in
        queued|running|rolling-back) running=true ;;
    esac

    jq -nc \
        --arg engine "$UPDATER_ENGINE" \
        --arg operationId "$CURRENT_OPERATION_ID" \
        --arg operation "$operation" \
        --arg state "$state" \
        --arg stage "$stage" \
        --argjson progress "$progress" \
        --arg message "$message" \
        --arg error "$error_code" \
        --arg updatedAt "$(now)" \
        --argjson running "$running" \
        --argjson mutationStarted "$mutation_started" \
        --argjson rollbackPerformed "$rollback_performed" '
        {
          schemaVersion:1,
          engine:$engine,
          durable:true,
          ok:true,
          operationId:$operationId,
          operation:$operation,
          state:$state,
          stage:$stage,
          progress:$progress,
          message:$message,
          error:(if $error == "" then null else $error end),
          running:$running,
          mutationStarted:$mutationStarted,
          rollbackPerformed:$rollbackPerformed,
          updatedAt:$updatedAt
        }' | atomic_json_file "$CURRENT_OPERATION_DIR/state.json" || return 1

    operation_publish_pointer
}

status_output()
{
    local operation_id state_file log_file log_tail
    operation_id="$(sed -n '1p' "$OPERATION_POINTER" 2>/dev/null || true)"

    if ! valid_id "$operation_id" || [ ! -s "$OPERATION_ROOT/$operation_id/state.json" ]; then
        jq -nc \
            --arg engine "$UPDATER_ENGINE" \
            --arg updatedAt "$(now)" '
            {
              schemaVersion:1,
              engine:$engine,
              durable:true,
              ok:true,
              operationId:null,
              operation:null,
              state:"idle",
              stage:"idle",
              progress:0,
              message:"Операции ещё не выполнялись.",
              error:null,
              running:false,
              mutationStarted:false,
              rollbackPerformed:false,
              logTail:"",
              updatedAt:$updatedAt
            }'
        return 0
    fi

    state_file="$OPERATION_ROOT/$operation_id/state.json"
    log_file="$OPERATION_ROOT/$operation_id/log.txt"
    log_tail="$(tail -n 80 "$log_file" 2>/dev/null || true)"

    jq -c --arg logTail "$log_tail" '. + {logTail:$logTail}' "$state_file"
}

daemon_identity_valid()
{
    local pid command_line
    pid="$(sed -n '1p' "$DAEMON_PID" 2>/dev/null || true)"
    case "$pid" in
        ''|*[!0-9]*) return 1 ;;
    esac

    kill -0 "$pid" 2>/dev/null || return 1

    if [ -r "/proc/$pid/cmdline" ]; then
        command_line="$(tr '\000' ' ' <"/proc/$pid/cmdline" 2>/dev/null || true)"
        case "$command_line" in
            *broray-updater*daemon*) ;;
            *) return 1 ;;
        esac
    fi

    return 0
}

fetch_file()
{
    local url output temporary
    url="$1"
    output="$2"
    temporary="$output.part.$$"

    case "$url" in
        https://*) ;;
        file://*) [ "$TEST_MODE" = 1 ] || return 1 ;;
        *) return 1 ;;
    esac

    rm -f "$temporary"

    if [ -n "$FETCH_HOOK" ]; then
        "$FETCH_HOOK" "$url" "$temporary" || {
            rm -f "$temporary"
            return 1
        }
    else
        curl -q --proto '=https' --proto-redir '=https' -fL \
            --retry 3 \
            --retry-delay 1 \
            --connect-timeout 15 \
            --max-time 900 \
            -H 'Accept-Encoding: identity' \
            -H 'Cache-Control: no-cache, no-store, max-age=0' \
            -H 'Pragma: no-cache' \
            "$url" \
            -o "$temporary" >>"$DAEMON_LOG" 2>&1 || {
                rm -f "$temporary"
                return 1
            }
    fi

    [ -s "$temporary" ] || {
        rm -f "$temporary"
        return 1
    }

    mv -f "$temporary" "$output"
}

bounded_regular_file()
{
    local file minimum maximum bytes
    file="$1"
    minimum="$2"
    maximum="$3"
    [ -f "$file" ] && [ ! -L "$file" ] || return 1
    bytes="$(wc -c <"$file" 2>/dev/null | tr -d ' ')" || return 1
    case "$bytes:$minimum:$maximum" in *[!0-9:]*) return 1 ;; esac
    [ "$bytes" -ge "$minimum" ] && [ "$bytes" -le "$maximum" ]
}

release_signature_valid()
{
    local index_file signature_file verifier_sha
    index_file="$1"
    signature_file="$2"
    bounded_regular_file "$index_file" 2 262144 || return 1
    bounded_regular_file "$signature_file" 100 4096 || return 1
    [ -f "$SIGNATURE_BIN" ] && [ ! -L "$SIGNATURE_BIN" ] && [ -x "$SIGNATURE_BIN" ] || return 1
    verifier_sha="$(sha256sum "$SIGNATURE_BIN" 2>/dev/null | awk 'NR==1{print $1;exit}')" || return 1
    [ "$verifier_sha" = "$SIGNATURE_BIN_SHA256" ] || return 1
    case "$RELEASE_PUBLIC_KEY" in
        ''|*[!A-Za-z0-9+/=]*) return 1 ;;
    esac
    [ "${#RELEASE_PUBLIC_KEY}" -ge 40 ] && [ "${#RELEASE_PUBLIC_KEY}" -le 128 ] || return 1
    "$SIGNATURE_BIN" -Vm "$index_file" -x "$signature_file" -P "$RELEASE_PUBLIC_KEY" -q \
        >/dev/null 2>>"$DAEMON_LOG"
}

release_index_valid()
{
    local index_file candidate_id release_id bundle_filename bundle_sha256 bundle_url minimum_version candidate_json
    index_file="$1"
    [ -s "$index_file" ] && [ ! -L "$index_file" ] || return 1

    jq -e \
        --arg lifecycle "$LIFECYCLE_CONTRACT" \
        --arg architecture "$ARCHITECTURE" \
        --argjson updaterVersion "$UPDATER_VERSION" '
        type == "object" and
        .schemaVersion == 1 and
        .lifecycleContract == $lifecycle and
        (.stable | type == "boolean") and
        (.candidate | type == "object") and
        (.candidate.candidateId | type == "string") and
        (.candidate.releaseId | type == "string") and
        (.candidate.appVersion | type == "string") and
        (.candidate.packageVersion | type == "string") and
        .candidate.architecture == $architecture and
        (.candidate.appSlot | type == "object") and
        .candidate.appSlot.layout == "broray-compact-app-slot/1" and
        (.candidate.appSlot.logicalBytes | type == "number") and
        (.candidate.appSlot.logicalBytes > 0) and
        (.candidate.appSlot.fileCount | type == "number") and
        (.candidate.appSlot.fileCount > 0) and
        (.candidate.appSlot.directoryCount | type == "number") and
        (.candidate.appSlot.directoryCount > 0) and
        (.candidate.appSlot.maxFileBytes | type == "number") and
        (.candidate.appSlot.maxFileBytes > 0) and
        (.candidate.sharedRuntime.xray | type == "object") and
        .candidate.sharedRuntime.xray.path == "/opt/broray/runtime/xray" and
        .candidate.sharedRuntime.xray.mode == "preserve-installed" and
        .candidate.sharedRuntime.xray.bundled == false and
        (.candidate.bundle | type == "object") and
        (.candidate.bundle.filename | type == "string") and
        (.candidate.bundle.sha256 | type == "string") and
        (.candidate.bundle.sizeBytes | type == "number") and
        (.candidate.bundle.sizeBytes > 0) and
        (.candidate.bundle.url | type == "string") and
        (.minimumUpdaterVersion | type == "number") and
        .minimumUpdaterVersion <= $updaterVersion
        ' "$index_file" >/dev/null 2>&1 || return 1

    candidate_id="$(jq -r '.candidate.candidateId' "$index_file")" || return 1
    release_id="$(jq -r '.candidate.releaseId' "$index_file")" || return 1
    bundle_filename="$(jq -r '.candidate.bundle.filename' "$index_file")"
    bundle_sha256="$(jq -r '.candidate.bundle.sha256' "$index_file")" || return 1
    bundle_url="$(jq -r '.candidate.bundle.url' "$index_file")" || return 1
    minimum_version="$(jq -r '.minimumUpdaterVersion' "$index_file")" || return 1

    valid_id "$candidate_id" || return 1
    valid_id "$release_id" || return 1
    release_version_parts "$release_id" >/dev/null 2>&1 || return 1
    valid_sha256 "$bundle_sha256" || return 1
    valid_positive_integer "$minimum_version" || return 1
    [ "$minimum_version" -le "$UPDATER_VERSION" ] || return 1

    case "$bundle_filename" in
        ''|.*|-*|*/*|*'..'*|*[!A-Za-z0-9._-]*) return 1 ;;
    esac

    case "$bundle_url" in
        https://*) ;;
        file://*) [ "$TEST_MODE" = 1 ] || return 1 ;;
        *) return 1 ;;
    esac

    candidate_json="$(jq -c '.candidate' "$index_file")" || return 1
    target_package_track_valid "$candidate_json" || return 1

    return 0
}

current_slot()
{
    local marker slot line_count
    [ -d "$CURRENT_PATH" ] && [ ! -L "$CURRENT_PATH" ] || return 1
    marker="$CURRENT_PATH/.broray-slot"
    [ -s "$marker" ] && [ ! -L "$marker" ] || return 1
    line_count="$(wc -l <"$marker" | tr -d ' ')"
    [ "$line_count" = 1 ] || return 1
    slot="$(sed -n '1p' "$marker")"
    valid_id "$slot" || return 1
    [ ! -e "$RELEASES_ROOT/$slot" ] && [ ! -L "$RELEASES_ROOT/$slot" ] || return 1
    printf '%s\n' "$slot"
}

slot_metadata()
{
    local slot metadata
    slot="$1"
    valid_id "$slot" || return 1
    metadata="$SLOT_META_ROOT/$slot.json"
    [ -s "$metadata" ] && [ ! -L "$metadata" ] || return 1
    jq -e \
        --arg slot "$slot" \
        --arg lifecycle "$LIFECYCLE_CONTRACT" '
        type == "object" and
        .schemaVersion == 1 and
        .slot == $slot and
        .lifecycleContract == $lifecycle and
        (.candidateId | type == "string") and
        (.releaseId | type == "string") and
        (.appVersion | type == "string") and
        ((.source == null) or
          ((.source | type == "object") and
           (.source.bundle.sha256 | type == "string") and
           (.source.bundle.url | type == "string")))
        ' "$metadata" >/dev/null 2>&1 || return 1
    cat "$metadata"
}

release_check()
{
    local index_url signature_url temporary signature_temporary nonce separator checked_at checked_epoch cache_temporary
    local signed_cache_temporary signature_cache_temporary
    local current_candidate current_release available_release slot metadata candidate_id update_available relation
    ensure_layout || {
        json_error RUNTIME_UNAVAILABLE 'Updater не может подготовить служебные каталоги.'
        return 1
    }

    index_url="${BRORAY_RELEASE_INDEX_URL:-$(sed -n '1p' "$CHANNEL_FILE" 2>/dev/null || true)}"
    case "$index_url" in
        https://*) ;;
        file://*) [ "$TEST_MODE" = 1 ] || {
            json_error CHANNEL_INVALID 'Адрес канала обновлений некорректен.'
            return 1
        } ;;
        *)
            json_error CHANNEL_UNAVAILABLE 'Канал обновлений не настроен.'
            return 1
            ;;
    esac

    temporary="$STATE_ROOT/release-index.download.$$"
    signature_temporary="$STATE_ROOT/release-index.signature.download.$$"
    nonce="$(epoch)-$$"
    separator='?'
    case "$index_url" in *\?*) separator='&' ;; esac

    signature_url="${BRORAY_RELEASE_SIGNATURE_URL:-}"
    if [ -z "$signature_url" ]; then
        case "$index_url" in
            *\?*) signature_url="${index_url%%\?*}.minisig?${index_url#*\?}" ;;
            *) signature_url="${index_url}.minisig" ;;
        esac
    fi
    case "$signature_url" in
        https://*) ;;
        file://*) [ "$TEST_MODE" = 1 ] || {
            json_error CHANNEL_INVALID 'Адрес подписи канала обновлений некорректен.'
            return 1
        } ;;
        *)
            json_error CHANNEL_INVALID 'Адрес подписи канала обновлений некорректен.'
            return 1
            ;;
    esac

    fetch_file "${index_url}${separator}broray=$nonce" "$temporary" || {
        rm -f "$temporary" "$signature_temporary"
        json_error UPDATE_CHECK_FAILED 'Не удалось загрузить индекс релиза.'
        return 1
    }

    separator='?'
    case "$signature_url" in *\?*) separator='&' ;; esac
    fetch_file "${signature_url}${separator}broray=$nonce" "$signature_temporary" || {
        rm -f "$temporary" "$signature_temporary"
        json_error UPDATE_SIGNATURE_MISSING 'Не удалось загрузить подпись индекса релиза.'
        return 1
    }

    release_signature_valid "$temporary" "$signature_temporary" || {
        rm -f "$temporary" "$signature_temporary"
        json_error UPDATE_SIGNATURE_INVALID 'Подпись индекса релиза недействительна.'
        return 1
    }

    release_index_valid "$temporary" || {
        rm -f "$temporary" "$signature_temporary"
        json_error UPDATE_INDEX_INVALID 'Индекс релиза не прошёл проверку.'
        return 1
    }

    checked_at="$(now)"
    checked_epoch="$(epoch)"
    cache_temporary="$CACHE_FILE.tmp.$$"
    signed_cache_temporary="$SIGNED_INDEX_CACHE.tmp.$$"
    signature_cache_temporary="$CACHE_SIGNATURE_FILE.tmp.$$"

    current_candidate=""
    current_release=""
    if slot="$(current_slot 2>/dev/null)" && metadata="$(slot_metadata "$slot" 2>/dev/null)"; then
        current_candidate="$(printf '%s\n' "$metadata" | jq -r '.candidateId // ""')"
        current_release="$(printf '%s\n' "$metadata" | jq -r '.releaseId // ""')"
    fi
    candidate_id="$(jq -r '.candidate.candidateId' "$temporary")"
    available_release="$(jq -r '.candidate.releaseId' "$temporary")"
    relation="$(release_relation "$current_release" "$available_release")"
    update_available=false
    [ "$relation" = newer ] && update_available=true

    jq \
        --arg checkedAt "$checked_at" \
        --argjson checkedEpoch "$checked_epoch" \
        --arg currentCandidateAtCheck "$current_candidate" \
        --arg currentReleaseAtCheck "$current_release" \
        --arg releaseRelation "$relation" \
        --arg candidateRelation "$relation" \
        --argjson updateAvailable "$update_available" \
        '. + {checkedAt:$checkedAt,checkedEpoch:$checkedEpoch,
          currentCandidateAtCheck:(if $currentCandidateAtCheck=="" then null else $currentCandidateAtCheck end),
          currentReleaseAtCheck:(if $currentReleaseAtCheck=="" then null else $currentReleaseAtCheck end),
          releaseRelation:$releaseRelation,
          candidateRelation:$candidateRelation,updateAvailable:$updateAvailable}' \
        "$temporary" >"$cache_temporary" || {
            rm -f "$temporary" "$signature_temporary" "$cache_temporary" "$signed_cache_temporary" "$signature_cache_temporary"
            json_error UPDATE_INDEX_INVALID 'Индекс релиза не удалось нормализовать.'
            return 1
        }

    cp -p "$temporary" "$signed_cache_temporary" || return 1
    cp -p "$signature_temporary" "$signature_cache_temporary" || return 1
    rm -f "$temporary" "$signature_temporary"
    chmod 600 "$cache_temporary" "$signed_cache_temporary" "$signature_cache_temporary" 2>/dev/null || true
    mv -f "$signed_cache_temporary" "$SIGNED_INDEX_CACHE" || return 1
    mv -f "$signature_cache_temporary" "$CACHE_SIGNATURE_FILE" || return 1
    mv -f "$cache_temporary" "$CACHE_FILE" || return 1

    jq -nc \
        --arg engine "$UPDATER_ENGINE" \
        --arg currentCandidate "$current_candidate" \
        --arg currentRelease "$current_release" \
        --arg availableVersion "$(jq -r '.candidate.releaseId' "$CACHE_FILE")" \
        --arg availablePackageVersion "$(jq -r '.candidate.packageVersion' "$CACHE_FILE")" \
        --arg candidateId "$candidate_id" \
        --arg candidateRelation "$relation" \
        --arg checkedAt "$checked_at" \
        --argjson updateAvailable "$update_available" '
        {
          ok:true,
          updateAvailable:$updateAvailable,
          currentCandidate:(if $currentCandidate == "" then null else $currentCandidate end),
          currentRelease:(if $currentRelease == "" then null else $currentRelease end),
          availableVersion:$availableVersion,
          availablePackageVersion:$availablePackageVersion,
          candidateId:$candidateId,
          releaseRelation:$candidateRelation,
          candidateRelation:$candidateRelation,
          checkedAt:$checkedAt,
          engine:$engine
        }'
}

request_target_update()
{
    local checked_epoch now_epoch current_release slot metadata relation cached_release
    release_signature_valid "$SIGNED_INDEX_CACHE" "$CACHE_SIGNATURE_FILE" || return 1
    release_index_valid "$SIGNED_INDEX_CACHE" || return 1
    jq -e --slurpfile signed "$SIGNED_INDEX_CACHE" \
        '($signed | length) == 1 and .candidate == $signed[0].candidate' \
        "$CACHE_FILE" >/dev/null 2>&1 || return 1
    release_index_valid "$CACHE_FILE" || return 1
    checked_epoch="$(jq -r '.checkedEpoch // 0' "$CACHE_FILE")"
    valid_positive_integer "$checked_epoch" || return 1
    now_epoch="$(epoch)"
    [ "$now_epoch" -ge "$checked_epoch" ] || return 1
    [ $((now_epoch - checked_epoch)) -le 3600 ] || return 1

    current_release=""
    if slot="$(current_slot 2>/dev/null)" && metadata="$(slot_metadata "$slot" 2>/dev/null)"; then
        current_release="$(printf '%s\n' "$metadata" | jq -r '.releaseId // ""')"
    fi

    cached_release="$(jq -r '.currentReleaseAtCheck // ""' "$CACHE_FILE")"
    [ "$cached_release" = "$current_release" ] || return 2
    relation="$(release_relation "$current_release" "$(jq -r '.candidate.releaseId' "$CACHE_FILE")")"
    [ "$relation" = newer ] || return 2
    jq -c '.candidate' "$CACHE_FILE"
}

request_target_reinstall()
{
    local slot metadata
    slot="$(current_slot)" || return 1
    metadata="$(slot_metadata "$slot")" || return 1
    printf '%s\n' "$metadata" | jq -ce '
      .source as $source |
      select(
        ($source | type == "object") and
        ($source.bundle | type == "object") and
        ($source.bundle.url | type == "string") and
        ($source.bundle.sha256 | type == "string") and
        ($source.bundle.sizeBytes | type == "number")
      ) |
      $source
    '
}

routes_resumable_pending()
{
    local route_state
    if [ ! -e "$ROUTES_OPERATION_ROOT" ] && [ ! -L "$ROUTES_OPERATION_ROOT" ]; then
        return 1
    fi
    [ -d "$ROUTES_OPERATION_ROOT" ] && [ ! -L "$ROUTES_OPERATION_ROOT" ] || return 0
    for route_state in "$ROUTES_OPERATION_ROOT"/*.json
    do
        [ -e "$route_state" ] || [ -L "$route_state" ] || continue
        [ -f "$route_state" ] && [ ! -L "$route_state" ] || return 0
        jq -e '
          type == "object" and .kind == "routes" and
          (.running | type) == "boolean" and
          (.resumable | type) == "boolean" and
          (.bundleId | type) == "string" and (.bundleId | length) > 0 and
          all(.bundleId | explode[];
              (.>=48 and .<=57) or (.>=65 and .<=90) or
              (.>=97 and .<=122) or .==45 or .==46 or .==95)
        ' "$route_state" >/dev/null 2>&1 || return 0
        jq -e '(.running == true) or (.resumable == true)' \
            "$route_state" >/dev/null 2>&1 && return 0
    done
    return 1
}

# Classify the shared WebUI/routes fence without ever modifying it.  Any
# present object is a conflict: live and publishing owners are busy; stale,
# malformed, oversized, non-directory and symlink objects are ambiguous and
# therefore fail closed for the updater.  Only the owning coordinator may
# recover or delete this namespace.
global_operation_lock_classify()
{
    local owner owner_bytes owner_lines
    GLOBAL_OPERATION_LOCK_STATE=absent
    if [ ! -e "$GLOBAL_OPERATION_LOCK" ] && [ ! -L "$GLOBAL_OPERATION_LOCK" ]; then
        return 0
    fi
    if [ ! -d "$GLOBAL_OPERATION_LOCK" ] || [ -L "$GLOBAL_OPERATION_LOCK" ]; then
        GLOBAL_OPERATION_LOCK_STATE=unsafe-object
        return 1
    fi

    if [ -e "$GLOBAL_OPERATION_LOCK/pid" ] || [ -L "$GLOBAL_OPERATION_LOCK/pid" ]; then
        if [ -e "$GLOBAL_OPERATION_LOCK/owner-identity.tsv" ] ||
           [ -L "$GLOBAL_OPERATION_LOCK/owner-identity.tsv" ]; then
            GLOBAL_OPERATION_LOCK_STATE=ambiguous-multiple-owners
            return 1
        fi
        [ -f "$GLOBAL_OPERATION_LOCK/pid" ] &&
        [ ! -L "$GLOBAL_OPERATION_LOCK/pid" ] || {
            GLOBAL_OPERATION_LOCK_STATE=ambiguous-route-owner
            return 1
        }
        owner_bytes="$(wc -c <"$GLOBAL_OPERATION_LOCK/pid" 2>/dev/null | tr -d ' ')" || {
            GLOBAL_OPERATION_LOCK_STATE=ambiguous-route-owner
            return 1
        }
        case "$owner_bytes" in ''|*[!0-9]*) GLOBAL_OPERATION_LOCK_STATE=ambiguous-route-owner; return 1 ;; esac
        [ "$owner_bytes" -le 128 ] || {
            GLOBAL_OPERATION_LOCK_STATE=oversized-route-owner
            return 1
        }
        owner_lines="$(wc -l <"$GLOBAL_OPERATION_LOCK/pid" 2>/dev/null | tr -d ' ')" || {
            GLOBAL_OPERATION_LOCK_STATE=ambiguous-route-owner
            return 1
        }
        [ "$owner_lines" -eq 1 ] || {
            GLOBAL_OPERATION_LOCK_STATE=ambiguous-route-owner
            return 1
        }
        owner="$(sed -n '1p' "$GLOBAL_OPERATION_LOCK/pid" 2>/dev/null || true)"
        case "$owner" in ''|*[!0-9]*) GLOBAL_OPERATION_LOCK_STATE=ambiguous-route-owner; return 1 ;; esac
        if kill -0 "$owner" 2>/dev/null; then
            GLOBAL_OPERATION_LOCK_STATE=live-route-owner
        else
            GLOBAL_OPERATION_LOCK_STATE=stale-route-owner
        fi
        return 1
    fi

    if [ -e "$GLOBAL_OPERATION_LOCK/owner-identity.tsv" ] ||
       [ -L "$GLOBAL_OPERATION_LOCK/owner-identity.tsv" ]; then
        [ -f "$GLOBAL_OPERATION_LOCK/owner-identity.tsv" ] &&
        [ ! -L "$GLOBAL_OPERATION_LOCK/owner-identity.tsv" ] || {
            GLOBAL_OPERATION_LOCK_STATE=ambiguous-system-owner
            return 1
        }
        owner_bytes="$(wc -c <"$GLOBAL_OPERATION_LOCK/owner-identity.tsv" 2>/dev/null | tr -d ' ')" || {
            GLOBAL_OPERATION_LOCK_STATE=ambiguous-system-owner
            return 1
        }
        case "$owner_bytes" in ''|*[!0-9]*) GLOBAL_OPERATION_LOCK_STATE=ambiguous-system-owner; return 1 ;; esac
        [ "$owner_bytes" -gt 0 ] && [ "$owner_bytes" -le 1048576 ] || {
            GLOBAL_OPERATION_LOCK_STATE=ambiguous-system-owner
            return 1
        }
        GLOBAL_OPERATION_LOCK_STATE=system-owner-present
        return 1
    fi

    GLOBAL_OPERATION_LOCK_STATE=owner-publication-incomplete
    return 1
}

conflicting_operation_admission_clear()
{
    ADMISSION_CONFLICT_STATE=absent
    if ! global_operation_lock_classify; then
        ADMISSION_CONFLICT_STATE="$GLOBAL_OPERATION_LOCK_STATE"
        return 1
    fi
    if [ -e "$LEGACY_GLOBAL_OPERATION_LOCK" ] || [ -L "$LEGACY_GLOBAL_OPERATION_LOCK" ]; then
        ADMISSION_CONFLICT_STATE=legacy-global-present
        return 1
    fi
    if routes_resumable_pending; then
        ADMISSION_CONFLICT_STATE=routes-resumable-pending
        return 1
    fi
    return 0
}

admission_hook_call()
{
    local phase
    phase="$1"
    [ "$TEST_MODE" = 1 ] && [ -n "$ADMISSION_HOOK" ] || return 0
    "$ADMISSION_HOOK" "$phase" "$GLOBAL_OPERATION_LOCK" "$REQUEST_LOCK"
}

request_enqueue()
{
    local operation target_json target_rc operation_id request_temporary pinned_check
    local queue_temporary
    operation="$1"
    operation_id="$operation-$(date -u '+%Y%m%d%H%M%S')-$$"
    valid_id "$operation_id" || return 1
    if ! conflicting_operation_admission_clear; then
        json_error OPERATION_BUSY "Другая операция BROray активна или её блокировка неоднозначна: $ADMISSION_CONFLICT_STATE."
        return 1
    fi
    ensure_layout || {
        json_error RUNTIME_UNAVAILABLE 'Updater не может подготовить служебные каталоги.'
        return 1
    }

    if [ "$ASSUME_DAEMON" != 1 ] &&
       { ! daemon_identity_valid || [ "$(sed -n '1p' "$DAEMON_READY" 2>/dev/null || true)" != "$(sed -n '1p' "$DAEMON_PID" 2>/dev/null || true)" ]; }
    then
        json_error UPDATER_NOT_RUNNING 'Постоянный updater не запущен; операция не принята.'
        return 1
    fi

    if ! mkdir "$REQUEST_LOCK" 2>/dev/null; then
        json_error OPERATION_BUSY 'Другая операция BROray уже принята или выполняется.'
        return 1
    fi

    REQUEST_PUBLISHED=false
    cleanup_request_failure()
    {
        BRORAY_UPDATER_CLEANUP_RC=$?
        trap - EXIT HUP INT TERM
        if [ "$REQUEST_PUBLISHED" != true ]; then
            rm -rf "$REQUEST_LOCK"
        fi
        exit "$BRORAY_UPDATER_CLEANUP_RC"
    }
    trap cleanup_request_failure EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM

    # Publish bounded ownership immediately.  A power loss between mkdir and
    # these two renames leaves only an ownerless, allowlisted directory; the
    # next daemon retires it before publishing daemon.ready, while enqueue is
    # still closed.
    printf '%s\n' "$operation_id" >"$REQUEST_LOCK/operation-id.tmp.$$" || return 1
    mv -f "$REQUEST_LOCK/operation-id.tmp.$$" "$REQUEST_LOCK/operation-id" || return 1
    printf '%s\n' "$$" >"$REQUEST_LOCK/pid.tmp.$$" || return 1
    mv -f "$REQUEST_LOCK/pid.tmp.$$" "$REQUEST_LOCK/pid" || return 1
    request_start="$(process_start_ticks "$$")" || return 1
    printf '%s\n' "$request_start" >"$REQUEST_LOCK/owner-start.tmp.$$" || return 1
    mv -f "$REQUEST_LOCK/owner-start.tmp.$$" "$REQUEST_LOCK/owner-start" || return 1

    # Close the check-then-mkdir race with short WebUI operations.  Those
    # operations recheck REQUEST_LOCK after publishing their common lock; the
    # updater performs the symmetric recheck after claiming REQUEST_LOCK.
    admission_hook_call request-post-claim || return 1
    if ! conflicting_operation_admission_clear; then
        json_error OPERATION_BUSY "Другая операция BROray началась одновременно или оставила неоднозначную блокировку: $ADMISSION_CONFLICT_STATE."
        return 1
    fi

    # A versioned external installer may pin its immutable release index.
    # Refresh it only after the durable request fence is owned so another
    # check cannot replace the cached target between check and enqueue.
    if [ "$operation" = update ] && [ -n "${BRORAY_RELEASE_INDEX_URL:-}" ]; then
        if ! pinned_check="$(release_check)"; then
            printf '%s\n' "$pinned_check"
            return 1
        fi
        printf '%s\n' "$pinned_check" |
            jq -e --arg engine "$UPDATER_ENGINE" \
                '.ok == true and .engine == $engine and (.candidateId | type == "string")' \
                >/dev/null 2>&1 || {
                    json_error UPDATE_INDEX_INVALID 'Закреплённый индекс релиза не прошёл проверку.'
                    return 1
                }
    fi

    target_json=""
    case "$operation" in
        update)
            target_json="$(request_target_update)"
            target_rc=$?
            if [ "$target_rc" -ne 0 ]; then
                if [ "$target_rc" -eq 2 ]; then
                    json_error UPDATE_NOT_AVAILABLE 'Новая версия не обнаружена.'
                else
                    json_error UPDATE_CHECK_REQUIRED 'Сначала выполните свежую проверку обновления.'
                fi
                return 1
            fi
            ;;
        reinstall)
            target_json="$(request_target_reinstall)" || {
                json_error REINSTALL_SOURCE_UNAVAILABLE 'Источник установленной версии не подтверждён.'
                return 1
            }
            ;;
        *)
            json_error INVALID_OPERATION 'Неизвестный тип операции.'
            return 1
            ;;
    esac

    operation_select "$operation_id" || return 1

    request_temporary="$CURRENT_OPERATION_DIR/request.json.tmp.$$"
    jq -nc \
        --arg operationId "$operation_id" \
        --arg operation "$operation" \
        --arg requestedAt "$(now)" \
        --arg engine "$UPDATER_ENGINE" \
        --argjson target "$target_json" '
        {
          schemaVersion:1,
          operationId:$operationId,
          operation:$operation,
          requestedAt:$requestedAt,
          engine:$engine,
          target:$target
        }' >"$request_temporary" || return 1

    jq -e \
        --arg id "$operation_id" \
        --arg operation "$operation" \
        '.schemaVersion == 1 and .operationId == $id and .operation == $operation and
         (.target.bundle.sha256 | type == "string") and
         (.target.bundle.url | type == "string")' \
        "$request_temporary" >/dev/null 2>&1 || return 1

    mv -f "$request_temporary" "$CURRENT_OPERATION_DIR/request.json" || return 1
    : >"$CURRENT_OPERATION_LOG"
    status_write \
        "$operation" queued queued 0 \
        'Запрос сохранён и принят постоянным updater.' \
        '' false false || return 1

    queue_temporary="$QUEUE_ROOT/$operation_id.json.tmp.$$"
    cp -p "$CURRENT_OPERATION_DIR/request.json" "$queue_temporary" || return 1
    mv -f "$queue_temporary" "$QUEUE_ROOT/$operation_id.json" || return 1

    REQUEST_PUBLISHED=true
    trap - EXIT HUP INT TERM

    jq -nc \
        --arg operationId "$operation_id" \
        --arg operation "$operation" \
        --arg engine "$UPDATER_ENGINE" '
        {
          ok:true,
          accepted:true,
          operationId:$operationId,
          operation:$operation,
          state:"queued",
          engine:$engine
        }'
}

archive_safe()
{
    local archive list verbose list_stderr verbose_stderr raw_count unique_count
    archive="$1"
    list="$2/archive.list"
    verbose="$2/archive.verbose"
    list_stderr="$2/archive.list.stderr"
    verbose_stderr="$2/archive.verbose.stderr"

    [ -s "$archive" ] && [ ! -L "$archive" ] || return 1
    gzip -t "$archive" >/dev/null 2>&1 || return 1
    # BusyBox tar normalizes leading ../ and / in its listing.  Any diagnostic
    # produced while reading an otherwise valid release is therefore unsafe:
    # accepting only stdout would validate the normalized name instead of the
    # actual archive header.
    tar -tzf "$archive" >"$list" 2>"$list_stderr" || return 1
    [ ! -s "$list_stderr" ] || return 1
    tar -tvzf "$archive" >"$verbose" 2>"$verbose_stderr" || return 1
    [ ! -s "$verbose_stderr" ] || return 1

    awk '
      {
        path=$0
        sub(/^\.\//, "", path)
        while (path ~ /\/$/) sub(/\/$/, "", path)
        if (path == "") next
        if (substr(path,1,1) == "/" || path ~ /(^|\/)\.\.($|\/)/ ||
            path ~ /(^|\/)\.($|\/)/ || index(path,"|") || index(path,"\t")) bad=1
      }
      END {exit bad ? 1 : 0}
    ' "$list" || return 1

    awk '
      substr($0,1,1) != "-" && substr($0,1,1) != "d" {bad=1}
      END {exit bad ? 1 : 0}
    ' "$verbose" || return 1

    raw_count="$(wc -l <"$list" | tr -d ' ')"
    unique_count="$(sort -u "$list" | wc -l | tr -d ' ')"
    [ "$raw_count" -gt 0 ] && [ "$raw_count" -eq "$unique_count" ]
}

filesystem_free_bytes()
{
    local path free_kib
    path="$1"
    [ -d "$path" ] && [ ! -L "$path" ] || return 1
    if [ "$TEST_MODE" = 1 ] && [ -n "$SPACE_HOOK" ]; then
        "$SPACE_HOOK" "$path"
        return $?
    fi
    free_kib="$(df -Pk "$path" 2>/dev/null | awk 'NR == 2 {print $4; exit}')"
    valid_positive_integer "$free_kib" || return 1
    printf '%s\n' $((free_kib * 1024))
}

app_slot_count_valid()
{
    local count
    count=0
    if [ -d "$CURRENT_PATH" ] && [ ! -L "$CURRENT_PATH" ]; then
        count=1
    fi
    count=$((count + $(find "$RELEASES_ROOT" -mindepth 1 -maxdepth 1 -type d -printf '.\n' 2>/dev/null |
        wc -l | tr -d ' ')))
    [ "$count" -le 2 ]
}

filesystem_has_bytes()
{
    local path required free
    path="$1"
    required="$2"
    valid_positive_integer "$required" || return 1
    free="$(filesystem_free_bytes "$path")" || return 1
    [ "$free" -ge "$required" ]
}

target_required_logical_bytes()
{
    local target_json candidate_id slot static_bytes marker_bytes
    target_json="$1"
    candidate_id="$(printf '%s\n' "$target_json" | jq -r '.candidateId')"
    valid_id "$candidate_id" || return 1
    valid_id "$CURRENT_OPERATION_ID" || return 1
    slot="$candidate_id--$CURRENT_OPERATION_ID"
    valid_id "$slot" || return 1
    static_bytes="$(printf '%s\n' "$target_json" | jq -r '.appSlot.logicalBytes')"
    valid_positive_integer "$static_bytes" || return 1
    marker_bytes="$(printf '%s\n' "$slot" | wc -c | tr -d ' ')"
    valid_positive_integer "$marker_bytes" || return 1
    printf '%s\n' $((static_bytes + marker_bytes))
}

slot_metrics_match()
{
    local slot_root target_json logical_bytes file_count directory_count max_file_bytes
    local expected_logical expected_files expected_directories expected_max
    slot_root="$1"
    target_json="$2"

    logical_bytes="$(find "$slot_root" -xdev -type f ! -path "$slot_root/.broray-slot" -printf '%s\n' |
        awk '{sum += $1} END {printf "%.0f", sum}')" || return 1
    file_count="$(find "$slot_root" -xdev -type f ! -path "$slot_root/.broray-slot" -printf '.\n' |
        wc -l | tr -d ' ')" || return 1
    directory_count="$(find "$slot_root" -xdev -type d -printf '.\n' |
        wc -l | tr -d ' ')" || return 1
    max_file_bytes="$(find "$slot_root" -xdev -type f ! -path "$slot_root/.broray-slot" -printf '%s\n' |
        sort -nr | sed -n '1p')" || return 1

    expected_logical="$(printf '%s\n' "$target_json" | jq -r '.appSlot.logicalBytes')"
    expected_files="$(printf '%s\n' "$target_json" | jq -r '.appSlot.fileCount')"
    expected_directories="$(printf '%s\n' "$target_json" | jq -r '.appSlot.directoryCount')"
    expected_max="$(printf '%s\n' "$target_json" | jq -r '.appSlot.maxFileBytes')"

    [ "$logical_bytes" = "$expected_logical" ] &&
        [ "$file_count" = "$expected_files" ] &&
        [ "$directory_count" = "$expected_directories" ] &&
        [ "$max_file_bytes" = "$expected_max" ]
}

slot_tree_valid()
{
    local slot_root unsafe required marker_slot line_count release_json
    slot_root="$1"
    [ -d "$slot_root" ] && [ ! -L "$slot_root" ] || return 1
    [ -s "$slot_root/SHA256SUMS" ] && [ ! -L "$slot_root/SHA256SUMS" ] || return 1
    [ -s "$slot_root/.broray-slot" ] && [ ! -L "$slot_root/.broray-slot" ] || return 1
    line_count="$(wc -l <"$slot_root/.broray-slot" | tr -d ' ')"
    [ "$line_count" = 1 ] || return 1
    marker_slot="$(sed -n '1p' "$slot_root/.broray-slot")"
    valid_id "$marker_slot" || return 1

    unsafe="$(find "$slot_root" -xdev \( -type l -o -type b -o -type c -o -type p -o -type s \) -print -quit 2>/dev/null)"
    [ -z "$unsafe" ] || return 1

    (
        cd "$slot_root" || exit 1
        sha256sum -c SHA256SUMS >/dev/null 2>&1
    ) || return 1

    find "$slot_root" -xdev -type f \
        ! -path "$slot_root/SHA256SUMS" \
        ! -path "$slot_root/.broray-slot" \
        -printf '%P\n' | sort >"$CURRENT_OPERATION_DIR/tree.files" || return 1
    awk 'NF == 2 {print $2; next} {bad=1} END {exit bad ? 1 : 0}' "$slot_root/SHA256SUMS" | sort >"$CURRENT_OPERATION_DIR/manifest.files" || return 1
    cmp -s "$CURRENT_OPERATION_DIR/tree.files" "$CURRENT_OPERATION_DIR/manifest.files" || return 1

    for required in \
        release.json \
        app/bin/broray \
        app/bin/broray-system \
        app/bin/xray \
        app/lib/broray-page.sh \
        app/share/release/manifest.json \
        app/web-new/index.html \
        app/web-new/build.json \
        init/S24broray \
        init/S25broray-web
    do
        [ -f "$slot_root/$required" ] && [ ! -L "$slot_root/$required" ] || return 1
    done

    jq -e \
        --arg lifecycle "$LIFECYCLE_CONTRACT" \
        --arg architecture "$ARCHITECTURE" '
        type == "object" and
        .schemaVersion == 1 and
        .lifecycleContract == $lifecycle and
        .layout == "broray-compact-app-slot/1" and
        .architecture == $architecture and
        .sharedRuntime.xray.path == "/opt/broray/runtime/xray" and
        .sharedRuntime.xray.mode == "preserve-installed" and
        .sharedRuntime.xray.bundled == false and
        (.candidateId | type == "string") and
        (.releaseId | type == "string") and
        (.appVersion | type == "string")
        ' "$slot_root/release.json" >/dev/null 2>&1 || return 1

    release_json="$(jq -c '.' "$slot_root/release.json")" || return 1
    target_package_track_valid "$release_json" || return 1

    jq -e \
        --argjson release "$release_json" '
        type == "object" and
        .schemaVersion == 3 and
        .lifecycleContract == $release.lifecycleContract and
        .candidateId == $release.candidateId and
        .releaseId == $release.releaseId and
        .version == $release.appVersion and
        .packageVersion == $release.packageVersion and
        (.packageRevision | type) == "number" and
        (.packageRevision | floor) == .packageRevision and
        .packageRevision > 0 and
        .architecture == $release.architecture and
        .webUIBuild == $release.webUIBuild and
        .updaterEngine == $release.updaterEngine
        ' "$slot_root/app/share/release/manifest.json" >/dev/null 2>&1 || return 1

    jq -e \
        --argjson release "$release_json" '
        type == "object" and
        .schemaVersion == 1 and
        .lifecycleContract == $release.lifecycleContract and
        .candidateId == $release.candidateId and
        .releaseId == $release.releaseId and
        .appVersion == $release.appVersion and
        .packageVersion == $release.packageVersion and
        .buildId == $release.webUIBuild and
        .updaterEngine == $release.updaterEngine
        ' "$slot_root/app/web-new/build.json" >/dev/null 2>&1 || return 1

    [ -x "$slot_root/app/bin/broray" ] || return 1
    [ -x "$slot_root/app/bin/broray-system" ] || return 1
    [ -x "$slot_root/app/bin/xray" ] || return 1
    [ -f "$XRAY_WRAPPER" ] && [ ! -L "$XRAY_WRAPPER" ] || return 1
    cmp -s "$slot_root/app/bin/xray" "$XRAY_WRAPPER" || return 1
    [ -f "$XRAY_RUNTIME" ] && [ ! -L "$XRAY_RUNTIME" ] && \
        [ -x "$XRAY_RUNTIME" ] && [ -s "$XRAY_RUNTIME" ] || return 1
    return 0
}

shell_tree_valid()
{
    local slot_root script first_line
    slot_root="$1"
    [ -x "$ASH_BIN" ] || return 1

    find "$slot_root/app" "$slot_root/init" -xdev -type f -print | sort |
        while IFS= read -r script
        do
            first_line="$(sed -n '1p' "$script" 2>/dev/null || true)"
            case "$first_line" in
                '#!'*ash*) "$ASH_BIN" -n "$script" || exit 1 ;;
            esac
        done
}

xray_config_valid()
{
    local slot_root config
    slot_root="$1"
    config="$APP_ROOT/config/config.json"
    [ -f "$config" ] || return 0

    if [ "$TEST_MODE" = 1 ]; then
        return 0
    fi

    XRAY_LOCATION_ASSET="$slot_root/app/bin" \
        "$XRAY_RUNTIME" run -test -c "$config" >/dev/null 2>&1
}

state_seed()
{
    local slot_root seed_root top source relative destination journal journal_temporary
    local expected owner owner_root owner_key kind path extra actual
    slot_root="$1"
    seed_root="$slot_root/state-seed"
    if [ -e "$seed_root" ] || [ -L "$seed_root" ]; then
        [ -d "$seed_root" ] && [ ! -L "$seed_root" ] || return 1
    else
        return 0
    fi
    valid_id "$CURRENT_OPERATION_ID" || return 1
    journal="$CURRENT_OPERATION_DIR/state-seed.created"
    journal_temporary="$journal.tmp.$$"
    owner_root="$CURRENT_OPERATION_DIR/state-seed-owners"
    [ ! -e "$journal" ] && [ ! -L "$journal" ] || return 1
    if [ -e "$owner_root" ] || [ -L "$owner_root" ]; then
        [ -d "$owner_root" ] && [ ! -L "$owner_root" ] || return 1
        rmdir "$owner_root" 2>/dev/null || return 1
    fi
    mkdir "$owner_root" || return 1
    rm -f "$journal_temporary"
    : >"$journal_temporary" || return 1

    for top in config routes
    do
        [ -d "$seed_root/$top" ] || continue
        [ ! -L "$seed_root/$top" ] || {
            rm -f "$journal_temporary"
            return 1
        }
        if [ ! -e "$APP_ROOT/$top" ] && [ ! -L "$APP_ROOT/$top" ]; then
            printf 'D|%s\n' "$APP_ROOT/$top" >>"$journal_temporary" || {
                rm -f "$journal_temporary"
                return 1
            }
        fi
        if [ -e "$APP_ROOT/$top" ] || [ -L "$APP_ROOT/$top" ]; then
            [ -d "$APP_ROOT/$top" ] && [ ! -L "$APP_ROOT/$top" ] || {
                rm -f "$journal_temporary"
                return 1
            }
        fi

        find "$seed_root/$top" -xdev -type d -print | LC_ALL=C sort |
            while IFS= read -r source
            do
                [ "$source" != "$seed_root/$top" ] || continue
                relative="${source#"$seed_root/$top"/}"
                [ -n "$relative" ] || continue
                destination="$APP_ROOT/$top/$relative"
                if [ ! -e "$destination" ] && [ ! -L "$destination" ]; then
                    printf 'D|%s\n' "$destination" >>"$journal_temporary" || exit 1
                else
                    [ -d "$destination" ] && [ ! -L "$destination" ] || exit 1
                fi
            done || {
                rm -f "$journal_temporary"
                return 1
            }

        find "$seed_root/$top" -xdev -type f -print | LC_ALL=C sort |
            while IFS= read -r source
            do
                relative="${source#"$seed_root/$top"/}"
                [ -n "$relative" ] || continue
                destination="$APP_ROOT/$top/$relative"
                if [ ! -e "$destination" ] && [ ! -L "$destination" ]; then
                    expected="$(sha256sum "$source" | awk 'NR==1{print $1;exit}')"
                    valid_sha256 "$expected" || exit 1
                    owner_key="$(printf '%s\n' "$destination" | sha256sum | awk 'NR==1{print $1;exit}')"
                    valid_sha256 "$owner_key" || exit 1
                    owner="$owner_root/$owner_key"
                    [ ! -e "$owner" ] && [ ! -L "$owner" ] || exit 1
                    printf 'F|%s|%s|%s\n' "$destination" "$owner" "$expected" >>"$journal_temporary" || exit 1
                fi
            done || {
                rm -f "$journal_temporary"
                return 1
            }
    done

    # The complete ownership plan is durable before the first user-state
    # mutation.  A crash can therefore roll back every object it may have
    # created, including a partially copied owner file.
    mv -f "$journal_temporary" "$journal" || return 1
    sync || return 1

    while IFS='|' read -r kind path owner expected extra
    do
        [ -z "$extra" ] || return 1
        case "$path" in
            "$APP_ROOT"/config|"$APP_ROOT"/config/*|"$APP_ROOT"/routes|"$APP_ROOT"/routes/*) ;;
            *) return 1 ;;
        esac
        case "$kind" in
            D)
                [ -z "$owner$expected" ] || return 1
                if [ ! -e "$path" ] && [ ! -L "$path" ]; then
                    mkdir "$path" || return 1
                fi
                [ -d "$path" ] && [ ! -L "$path" ] || return 1
                ;;
            F)
                valid_sha256 "$expected" || return 1
                owner_key="$(printf '%s\n' "$path" | sha256sum | awk 'NR==1{print $1;exit}')"
                valid_sha256 "$owner_key" || return 1
                [ "$owner" = "$owner_root/$owner_key" ] || return 1
                relative="${path#"$APP_ROOT"/}"
                source="$seed_root/$relative"
                [ -f "$source" ] && [ ! -L "$source" ] || return 1
                actual="$(sha256sum "$source" | awk 'NR==1{print $1;exit}')"
                [ "$actual" = "$expected" ] || return 1
                if [ ! -e "$owner" ] && [ ! -L "$owner" ]; then
                    cp -p "$source" "$owner" || return 1
                fi
                [ -f "$owner" ] && [ ! -L "$owner" ] || return 1
                actual="$(sha256sum "$owner" | awk 'NR==1{print $1;exit}')"
                [ "$actual" = "$expected" ] || return 1
                if [ ! -e "$path" ] && [ ! -L "$path" ]; then
                    ln "$owner" "$path" 2>/dev/null || {
                        [ -e "$path" ] || [ -L "$path" ] || return 1
                    }
                fi
                ;;
            *) return 1 ;;
        esac
    done <"$journal"
}

state_seed_rollback()
{
    local journal kind path owner expected extra owner_root owner_key actual contents
    journal="$CURRENT_OPERATION_DIR/state-seed.created"
    owner_root="$CURRENT_OPERATION_DIR/state-seed-owners"
    if [ ! -e "$journal" ] && [ ! -L "$journal" ]; then
        if [ -e "$owner_root" ] || [ -L "$owner_root" ]; then
            [ -d "$owner_root" ] && [ ! -L "$owner_root" ] || return 1
            rmdir "$owner_root" 2>/dev/null || return 1
        fi
        return 0
    fi
    [ -f "$journal" ] && [ ! -L "$journal" ] || return 1
    LC_ALL=C sort -r "$journal" | while IFS='|' read -r kind path owner expected extra
    do
        [ -z "$extra" ] || exit 1
        case "$path" in "$APP_ROOT"/config|"$APP_ROOT"/config/*|"$APP_ROOT"/routes|"$APP_ROOT"/routes/*) ;; *) exit 1 ;; esac
        case "$kind" in
            F)
                valid_sha256 "$expected" || exit 1
                owner_key="$(printf '%s\n' "$path" | sha256sum | awk 'NR==1{print $1;exit}')"
                valid_sha256 "$owner_key" || exit 1
                [ "$owner" = "$owner_root/$owner_key" ] || exit 1
                if [ -e "$owner" ] || [ -L "$owner" ]; then
                    [ -f "$owner" ] && [ ! -L "$owner" ] || exit 1
                    if [ -e "$path" ] || [ -L "$path" ]; then
                        if [ -f "$path" ] && [ ! -L "$path" ] && [ "$path" -ef "$owner" ]; then
                            actual="$(sha256sum "$path" | awk 'NR==1{print $1;exit}')"
                            [ "$actual" != "$expected" ] || rm -f "$path" || exit 1
                        fi
                    fi
                    rm -f "$owner" || exit 1
                fi
                ;;
            D)
                [ -z "$owner$expected" ] || exit 1
                if [ -e "$path" ] || [ -L "$path" ]; then
                    [ -d "$path" ] && [ ! -L "$path" ] || exit 1
                    if ! rmdir "$path" 2>/dev/null; then
                        contents="$(find "$path" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)"
                        [ -n "$contents" ] || exit 1
                    fi
                fi
                ;;
            *) exit 1 ;;
        esac
    done || return 1
    if [ -e "$owner_root" ] || [ -L "$owner_root" ]; then
        [ -d "$owner_root" ] && [ ! -L "$owner_root" ] || return 1
        rmdir "$owner_root" 2>/dev/null || return 1
    fi
    rm -f "$journal"
}

state_seed_commit()
{
    local journal kind path owner expected extra owner_root owner_key
    journal="$CURRENT_OPERATION_DIR/state-seed.created"
    owner_root="$CURRENT_OPERATION_DIR/state-seed-owners"
    if [ ! -e "$journal" ] && [ ! -L "$journal" ]; then
        if [ -e "$owner_root" ] || [ -L "$owner_root" ]; then
            [ -d "$owner_root" ] && [ ! -L "$owner_root" ] || return 1
            rmdir "$owner_root" 2>/dev/null || return 1
        fi
        return 0
    fi
    [ -f "$journal" ] && [ ! -L "$journal" ] || return 1
    while IFS='|' read -r kind path owner expected extra
    do
        [ -z "$extra" ] || return 1
        case "$kind" in
            D) [ -z "$owner$expected" ] || return 1 ;;
            F)
                valid_sha256 "$expected" || return 1
                owner_key="$(printf '%s\n' "$path" | sha256sum | awk 'NR==1{print $1;exit}')"
                valid_sha256 "$owner_key" || return 1
                [ "$owner" = "$owner_root/$owner_key" ] || return 1
                if [ -e "$owner" ] || [ -L "$owner" ]; then
                    [ -f "$owner" ] && [ ! -L "$owner" ] || return 1
                    rm -f "$owner" || return 1
                fi
                ;;
            *) return 1 ;;
        esac
    done <"$journal"
    if [ -e "$owner_root" ] || [ -L "$owner_root" ]; then
        [ -d "$owner_root" ] && [ ! -L "$owner_root" ] || return 1
        rmdir "$owner_root" 2>/dev/null || return 1
    fi
    rm -f "$journal"
}

state_seed_abort_before_switch()
{
    local operation stage message error_code
    operation="$1"
    stage="$2"
    message="$3"
    error_code="$4"

    if ! state_seed_rollback; then
        status_write "$operation" error rollback-failed 100 \
            'Не удалось точно отменить добавление значений по умолчанию; новые операции заблокированы.' \
            STATE_SEED_ROLLBACK_FAILED false false || true
        operation_log 'STATE_SEED_ROLLBACK_FAILED before switch'
        return 1
    fi
    if ! services_start_captured >>"$CURRENT_OPERATION_LOG" 2>&1; then
        status_write "$operation" error rollback-failed 100 \
            'Активный релиз не изменялся, но исходное состояние служб не восстановлено.' \
            PRE_SWITCH_SERVICE_RESTORE_FAILED false false || true
        operation_log 'ROLLBACK_FAILED services before switch'
        return 1
    fi
    status_write "$operation" error "$stage" 100 "$message" "$error_code" false false || return 1
    operation_cleanup_terminal
}

service_call()
{
    local action service script
    action="$1"
    service="$2"

    if [ -n "$SERVICE_HOOK" ]; then
        "$SERVICE_HOOK" "$action" "$service"
        return $?
    fi

    script="$(root_path "/opt/etc/init.d/$service")"
    [ -f "$script" ] || [ -L "$script" ] || return 1
    "$ASH_BIN" "$script" "$action"
}

services_capture()
{
    local output service
    output="$CURRENT_OPERATION_DIR/services.tsv"
    : >"$output"

    for service in S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions
    do
        if service_call status "$service" >>"$CURRENT_OPERATION_LOG" 2>&1; then
            printf '%s\trunning\n' "$service" >>"$output" || return 1
        else
            printf '%s\tstopped\n' "$service" >>"$output" || return 1
        fi
    done
}

# A service restart can temporarily remove the owned ProxyN interface.  Keenetic
# then removes every static route bound to that interface.  Capture the exact
# pre-switch route commands and bind them to the durable updater operation so
# update, reinstall, rollback and crash recovery all restore the same bytes.
routes_snapshot_binding_valid()
{
    local snapshot interface_file sha_file expected actual actual_line interface present
    snapshot="$CURRENT_OPERATION_DIR/managed-routes.before"
    interface_file="$CURRENT_OPERATION_DIR/managed-routes.interface"
    sha_file="$CURRENT_OPERATION_DIR/managed-routes.sha256"
    present=0

    [ ! -e "$snapshot" ] && [ ! -L "$snapshot" ] || present=$((present + 1))
    [ ! -e "$interface_file" ] && [ ! -L "$interface_file" ] || present=$((present + 1))
    [ ! -e "$sha_file" ] && [ ! -L "$sha_file" ] || present=$((present + 1))
    [ "$present" -ne 0 ] || return 0
    [ "$present" -eq 3 ] || return 1
    [ -f "$snapshot" ] && [ ! -L "$snapshot" ] &&
    [ -f "$interface_file" ] && [ ! -L "$interface_file" ] &&
    [ -f "$sha_file" ] && [ ! -L "$sha_file" ] || return 1

    interface="$(sed -n '1p' "$interface_file" 2>/dev/null || true)"
    if [ -z "$interface" ]; then
        [ ! -s "$snapshot" ] || return 1
    else
        case "$interface" in Proxy[0-9]*) ;; *) return 1 ;; esac
        case "${interface#Proxy}" in ''|*[!0-9]*) return 1 ;; esac
        awk -v interface="$interface" '
          function ipv4(value, part, count, i) {
            count=split(value, part, ".")
            if (count != 4) return 0
            for (i=1; i<=4; i++) {
              if (part[i] !~ /^[0-9]+$/ || part[i] < 0 || part[i] > 255) return 0
            }
            return 1
          }
          NF == 5 && $1 == "ip" && $2 == "route" && ipv4($3) &&
            $4 == interface && $5 == "1200" {next}
          NF == 6 && $1 == "ip" && $2 == "route" && ipv4($3) &&
            ipv4($4) && $5 == interface && $6 == "1200" {next}
          {exit 1}
        ' "$snapshot" || return 1
    fi

    expected="$(sed -n '1p' "$sha_file" 2>/dev/null || true)"
    valid_sha256 "$expected" || return 1
    actual_line="$(sha256sum "$snapshot")" || return 1
    actual="${actual_line%% *}"
    valid_sha256 "$actual" || return 1
    [ "$actual" = "$expected" ]
}

routes_owner_interface()
{
    local owner config schema interface description write_sha running_sha startup_sha
    owner="$1"
    config="$2"

    [ -f "$owner" ] && [ ! -L "$owner" ] &&
    [ -f "$config" ] && [ ! -L "$config" ] || return 1
    jq -e '
      (type == "object") and
      .owner == "BROray" and
      (.interfaceName | type) == "string" and
      .protocol == "socks5" and
      (.upstream | type) == "object" and
      (.upstream | keys) == ["host", "port"] and
      .upstream.host == "192.168.1.1" and
      .upstream.port == 2080 and
      (
        if .schemaVersion == 1 then
          (keys == ["interfaceName", "owner", "protocol", "schemaVersion",
                    "selectionMode", "updatedAt", "upstream"]) and
          ((.selectionMode | type) == "string" and (.selectionMode | length) > 0) and
          ((.updatedAt | type) == "string" and (.updatedAt | length) > 0)
        elif .schemaVersion == 2 then
          (keys == ["contract", "description", "interfaceName", "owner", "protocol",
                    "runningBlockSha256", "schemaVersion", "selectionMode",
                    "startupBlockSha256", "updatedAt", "upstream",
                    "writeProtocolSha256"]) and
          (.contract == "r14c34-proxy-owned-interface/1" or
           .contract == "r14c35-proxy-owned-interface/1" or
           .contract == "r14c36-proxy-owned-interface/1" or
           .contract == "r14c37-proxy-owned-interface/1" or
           .contract == "r14c38-proxy-owned-interface/1") and
          ((.description | type) == "string") and
          ((.selectionMode | type) == "string" and (.selectionMode | length) > 0) and
          ((.updatedAt | type) == "string" and (.updatedAt | length) > 0) and
          ((.writeProtocolSha256 | type) == "string") and
          ((.runningBlockSha256 | type) == "string") and
          ((.startupBlockSha256 | type) == "string")
        else false end
      )
    ' "$owner" >/dev/null 2>&1 || return 1

    schema="$(jq -r '.schemaVersion' "$owner")" || return 1
    interface="$(jq -r '.interfaceName' "$owner")" || return 1
    case "$interface" in Proxy[0-9]*) ;; *) return 1 ;; esac
    case "${interface#Proxy}" in ''|*[!0-9]*) return 1 ;; esac

    if [ "$schema" = 2 ]; then
        description="$(jq -r '.description' "$owner")" || return 1
        [ -n "$description" ] && [ "${#description}" -le 128 ] || return 1
        case "$description" in BROray|BROray\ -\ *) ;; *) return 1 ;; esac
        case "$description" in
            *'"'*|*"'"*|*'\'*|*';'*|*'`'*|*'$'*|*'|'*|*'&'*|*'<'*|*'>') return 1 ;;
        esac
        [ "$(printf '%s' "$description" | wc -l)" -eq 0 ] || return 1

        write_sha="$(jq -r '.writeProtocolSha256' "$owner")" || return 1
        running_sha="$(jq -r '.runningBlockSha256' "$owner")" || return 1
        startup_sha="$(jq -r '.startupBlockSha256' "$owner")" || return 1
        valid_sha256 "$write_sha" && valid_sha256 "$running_sha" &&
        valid_sha256 "$startup_sha" && [ "$running_sha" = "$startup_sha" ] || return 1
    fi

    jq -e --arg interface "$interface" '
      (type == "object") and
      .managedInterface == $interface and
      .managedMetric == 1200
    ' "$config" >/dev/null 2>&1 || return 1
    printf '%s\n' "$interface"
}

routes_capture()
{
    local snapshot interface_file sha_file snapshot_tmp interface_tmp sha_tmp
    local owner config interface ndmc running all_refs recognized all_refs_unsorted recognized_unsorted
    local refs_count recognized_count snapshot_digest_line snapshot_digest
    snapshot="$CURRENT_OPERATION_DIR/managed-routes.before"
    interface_file="$CURRENT_OPERATION_DIR/managed-routes.interface"
    sha_file="$CURRENT_OPERATION_DIR/managed-routes.sha256"
    snapshot_tmp="$snapshot.tmp.$$"
    interface_tmp="$interface_file.tmp.$$"
    sha_tmp="$sha_file.tmp.$$"
    owner="$APP_ROOT/config/interface.json"
    config="$APP_ROOT/routes/config.json"
    running="$CURRENT_OPERATION_DIR/managed-routes.running-before"
    all_refs="$CURRENT_OPERATION_DIR/managed-routes.references-before"
    recognized="$CURRENT_OPERATION_DIR/managed-routes.recognized-before"
    all_refs_unsorted="$all_refs.unsorted"
    recognized_unsorted="$recognized.unsorted"

    [ ! -e "$snapshot" ] && [ ! -L "$snapshot" ] &&
    [ ! -e "$interface_file" ] && [ ! -L "$interface_file" ] &&
    [ ! -e "$sha_file" ] && [ ! -L "$sha_file" ] || return 1

    if [ -n "$ROUTE_HOOK" ]; then
        "$ROUTE_HOOK" capture "$snapshot_tmp" "$interface_tmp" || return 1
    elif [ ! -e "$owner" ] && [ ! -L "$owner" ]; then
        : >"$snapshot_tmp" || return 1
        : >"$interface_tmp" || return 1
    else
        interface="$(routes_owner_interface "$owner" "$config")" || return 1
        printf '%s\n' "$interface" >"$interface_tmp" || return 1

        ndmc="$(command -v ndmc 2>/dev/null || true)"
        [ -n "$ndmc" ] && [ -x "$ndmc" ] || return 1
        "$ndmc" -c 'show running-config' >"$running" 2>>"$CURRENT_OPERATION_LOG" || return 1
        [ -s "$running" ] || return 1
        awk -v interface="$interface" '
          $1 == "ip" && $2 == "route" {
            for (i=3; i<=NF; i++) if ($i == interface) {print; next}
          }
        ' "$running" >"$all_refs_unsorted" || return 1
        LC_ALL=C sort -u "$all_refs_unsorted" >"$all_refs" || return 1
        awk -v interface="$interface" '
          function ipv4(value, part, count, i) {
            count=split(value, part, ".")
            if (count != 4) return 0
            for (i=1; i<=4; i++) {
              if (part[i] !~ /^[0-9]+$/ || part[i] < 0 || part[i] > 255) return 0
            }
            return 1
          }
          NF == 5 && $1 == "ip" && $2 == "route" && ipv4($3) &&
            $4 == interface && $5 == "1200" {
              print "ip route " $3 " " $4 " " $5; next
          }
          NF == 6 && $1 == "ip" && $2 == "route" && ipv4($3) &&
            ipv4($4) && $5 == interface && $6 == "1200" {
              print "ip route " $3 " " $4 " " $5 " " $6; next
          }
        ' "$running" >"$recognized_unsorted" || return 1
        LC_ALL=C sort -u "$recognized_unsorted" >"$recognized" || return 1
        refs_count="$(awk 'END {print NR + 0}' "$all_refs")" || return 1
        recognized_count="$(awk 'END {print NR + 0}' "$recognized")" || return 1
        [ "$refs_count" = "$recognized_count" ] || return 1
        cmp -s "$all_refs" "$recognized" || return 1
        cp -p "$recognized" "$snapshot_tmp" || return 1
    fi

    mv -f "$interface_tmp" "$interface_file" || return 1
    mv -f "$snapshot_tmp" "$snapshot" || return 1
    snapshot_digest_line="$(sha256sum "$snapshot")" || return 1
    snapshot_digest="${snapshot_digest_line%% *}"
    valid_sha256 "$snapshot_digest" || return 1
    printf '%s\n' "$snapshot_digest" >"$sha_tmp" || return 1
    mv -f "$sha_tmp" "$sha_file" || return 1
    routes_snapshot_binding_valid || return 1
    operation_log "MANAGED_ROUTES_CAPTURE count=$(awk 'END {print NR + 0}' "$snapshot")"
}

routes_reconcile_wait()
{
    local pid_file pid attempt
    [ -z "$ROUTE_HOOK" ] || return 0
    pid_file="$APP_ROOT/run/interface-reconcile.pid"
    attempt=1
    while [ "$attempt" -le 45 ]; do
        pid="$(sed -n '1p' "$pid_file" 2>/dev/null || true)"
        case "$pid" in
            '') return 0 ;;
            *[!0-9]*) return 1 ;;
        esac
        kill -0 "$pid" 2>/dev/null || return 0
        sleep 1
        attempt=$((attempt + 1))
    done
    return 1
}

routes_snapshot_present_in()
{
    local config snapshot line
    config="$1"
    snapshot="$CURRENT_OPERATION_DIR/managed-routes.before"
    [ -f "$config" ] && [ ! -L "$config" ] && [ -s "$config" ] || return 1
    while IFS= read -r line; do
        [ -n "$line" ] || continue
        grep -Fqx "$line" "$config" || return 1
    done <"$snapshot"
}

routes_restore_captured()
{
    local snapshot interface ndmc running startup line changed attempt stable
    snapshot="$CURRENT_OPERATION_DIR/managed-routes.before"
    routes_snapshot_binding_valid || return 1
    [ -e "$snapshot" ] || [ -L "$snapshot" ] || return 0
    [ -s "$snapshot" ] || { operation_log 'MANAGED_ROUTES_RESTORE count=0'; return 0; }
    interface="$(sed -n '1p' "$CURRENT_OPERATION_DIR/managed-routes.interface")"

    if [ -n "$ROUTE_HOOK" ]; then
        "$ROUTE_HOOK" restore "$snapshot" "$interface" || return 1
        "$ROUTE_HOOK" verify "$snapshot" "$interface" || return 1
        operation_log "MANAGED_ROUTES_RESTORE count=$(awk 'END {print NR + 0}' "$snapshot") hook=true"
        return 0
    fi

    ndmc="$(command -v ndmc 2>/dev/null || true)"
    [ -n "$ndmc" ] && [ -x "$ndmc" ] || return 1
    running="$CURRENT_OPERATION_DIR/managed-routes.running-restore"
    startup="$CURRENT_OPERATION_DIR/managed-routes.startup-restore"
    "$ndmc" -c 'show running-config' >"$running" 2>>"$CURRENT_OPERATION_LOG" || return 1
    changed=false
    while IFS= read -r line; do
        [ -n "$line" ] || continue
        if ! grep -Fqx "$line" "$running"; then
            "$ndmc" -c "$line" >>"$CURRENT_OPERATION_LOG" 2>&1 || return 1
            changed=true
        fi
    done <"$snapshot"
    if [ "$changed" = true ]; then
        "$ndmc" -c 'system configuration save' >>"$CURRENT_OPERATION_LOG" 2>&1 || return 1
    fi

    attempt=1
    stable=0
    while [ "$attempt" -le 15 ]; do
        if "$ndmc" -c 'show running-config' >"$running" 2>>"$CURRENT_OPERATION_LOG" &&
           "$ndmc" -c 'more startup-config' >"$startup" 2>>"$CURRENT_OPERATION_LOG" &&
           routes_snapshot_present_in "$running" &&
           routes_snapshot_present_in "$startup"
        then
            stable=$((stable + 1))
            if [ "$stable" -ge 2 ]; then
                operation_log "MANAGED_ROUTES_RESTORE count=$(awk 'END {print NR + 0}' "$snapshot") changed=$changed"
                return 0
            fi
        else
            stable=0
        fi
        sleep 1
        attempt=$((attempt + 1))
    done
    return 1
}

routes_verify_captured()
{
    local snapshot interface ndmc running startup
    snapshot="$CURRENT_OPERATION_DIR/managed-routes.before"
    routes_snapshot_binding_valid || return 1
    [ -e "$snapshot" ] || [ -L "$snapshot" ] || return 0
    [ -s "$snapshot" ] || return 0
    interface="$(sed -n '1p' "$CURRENT_OPERATION_DIR/managed-routes.interface")"
    if [ -n "$ROUTE_HOOK" ]; then
        "$ROUTE_HOOK" verify "$snapshot" "$interface"
        return $?
    fi
    ndmc="$(command -v ndmc 2>/dev/null || true)"
    [ -n "$ndmc" ] && [ -x "$ndmc" ] || return 1
    running="$CURRENT_OPERATION_DIR/managed-routes.running-verify"
    startup="$CURRENT_OPERATION_DIR/managed-routes.startup-verify"
    "$ndmc" -c 'show running-config' >"$running" 2>>"$CURRENT_OPERATION_LOG" &&
    "$ndmc" -c 'more startup-config' >"$startup" 2>>"$CURRENT_OPERATION_LOG" &&
    routes_snapshot_present_in "$running" && routes_snapshot_present_in "$startup"
}

services_stop_captured()
{
    local services_file service state
    services_file="$CURRENT_OPERATION_DIR/services.tsv"
    [ -s "$services_file" ] || return 1

    for service in S28broray-subscriptions S27broray-auto-switch S25broray-web S24broray S23broray-monitor
    do
        state="$(awk -F '\t' -v service="$service" '$1 == service {print $2; exit}' "$services_file")"
        [ "$state" = running ] || continue
        service_call stop "$service" >>"$CURRENT_OPERATION_LOG" 2>&1 || return 1
    done
}

services_start_captured()
{
    local services_file service state
    services_file="$CURRENT_OPERATION_DIR/services.tsv"
    [ -s "$services_file" ] || return 1

    for service in S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions
    do
        state="$(awk -F '\t' -v service="$service" '$1 == service {print $2; exit}' "$services_file")"
        [ "$state" = running ] || continue
        service_call start "$service" >>"$CURRENT_OPERATION_LOG" 2>&1 || return 1
    done
    routes_reconcile_wait || return 1
    routes_restore_captured || return 1
}

services_health_captured()
{
    local services_file service state
    services_file="$CURRENT_OPERATION_DIR/services.tsv"
    [ -s "$services_file" ] || return 1

    while read -r service state
    do
        [ "$state" = running ] || continue
        service_call status "$service" >>"$CURRENT_OPERATION_LOG" 2>&1 || return 1
    done <"$services_file"
}

webui_backend_health()
{
    local expected_slot backend output error_output expected_version
    local expected_release expected_webui
    expected_slot="$1"
    valid_id "$expected_slot" || return 1
    [ "$(current_slot 2>/dev/null || true)" = "$expected_slot" ] || return 1

    expected_version="$(jq -er '.appVersion' "$CURRENT_PATH/release.json" 2>/dev/null)" || return 1
    expected_release="$(jq -er '.releaseId' "$CURRENT_PATH/release.json" 2>/dev/null)" || return 1
    expected_webui="$(jq -er '.webUIBuild' "$CURRENT_PATH/release.json" 2>/dev/null)" || return 1

    output="$CURRENT_OPERATION_DIR/webui-health.json"
    error_output="$CURRENT_OPERATION_DIR/webui-health.stderr"
    rm -f "$output" "$error_output"

    if [ -n "$WEBUI_BACKEND_HOOK" ]; then
        "$WEBUI_BACKEND_HOOK" "$expected_slot" >"$output" 2>"$error_output" || return 1
    else
        backend="$(root_path /opt/broray/bin/broray-system)"
        [ -f "$backend" ] && [ ! -L "$backend" ] && [ -x "$backend" ] || return 1
        "$ASH_BIN" "$backend" info >"$output" 2>"$error_output" || return 1
    fi

    [ -s "$output" ] || return 1
    jq -e \
        --arg version "$expected_version" \
        --arg releaseId "$expected_release" \
        --arg webUIBuild "$expected_webui" '
        type == "object" and
        .ok == true and
        .version == $version and
        (.installedPackageVersion | type) == "string" and
        (.installedPackageVersion | length) > 0 and
        .installedReleaseId == $releaseId and
        .webUIBuild == $webUIBuild and
        .installedWebUIBuild == $webUIBuild and
        (.updateAvailable | type) == "boolean" and
        .installationHealthy == true and
        .versionsConsistent == true
        ' "$output" >/dev/null 2>&1
}

slot_marker_matches()
{
    local root expected actual
    root="$1"
    expected="$2"
    valid_id "$expected" || return 1
    [ -d "$root" ] && [ ! -L "$root" ] || return 1
    [ -s "$root/.broray-slot" ] && [ ! -L "$root/.broray-slot" ] || return 1
    actual="$(sed -n '1p' "$root/.broray-slot" 2>/dev/null || true)"
    [ "$actual" = "$expected" ]
}

switch_phase_write()
{
    local phase
    phase="$1"
    case "$phase" in
        prepared|previous-stored|target-active|rollback-target-stored|rollback-previous-active) ;;
        *) return 1 ;;
    esac
    printf '%s\n' "$phase" | atomic_text_file "$CURRENT_OPERATION_DIR/switch.phase" || return 1
    sync
}

switch_to_slot()
{
    local previous_slot target_slot
    previous_slot="$1"
    target_slot="$2"
    valid_id "$previous_slot" || return 1
    valid_id "$target_slot" || return 1
    [ "$previous_slot" != "$target_slot" ] || return 1
    [ "$(current_slot 2>/dev/null || true)" = "$previous_slot" ] || return 1
    slot_marker_matches "$RELEASES_ROOT/$target_slot" "$target_slot" || return 1
    [ ! -e "$RELEASES_ROOT/$previous_slot" ] && [ ! -L "$RELEASES_ROOT/$previous_slot" ] || return 1

    switch_phase_write prepared || return 1
    mv "$CURRENT_PATH" "$RELEASES_ROOT/$previous_slot" || return 1
    sync
    switch_phase_write previous-stored || return 1

    if [ "$FAILPOINT" = crash-between-renames ]; then
        operation_log 'INJECTED_CRASH between release renames'
        return 96
    fi

    mv "$RELEASES_ROOT/$target_slot" "$CURRENT_PATH" || return 1
    sync
    switch_phase_write target-active || return 1
    [ "$(current_slot 2>/dev/null || true)" = "$target_slot" ]
}

rollback_layout()
{
    local previous_slot target_slot actual
    previous_slot="$1"
    target_slot="$2"
    valid_id "$previous_slot" || return 1
    valid_id "$target_slot" || return 1
    actual="$(current_slot 2>/dev/null || true)"

    if [ "$actual" = "$target_slot" ]; then
        slot_marker_matches "$RELEASES_ROOT/$previous_slot" "$previous_slot" || return 1
        [ ! -e "$RELEASES_ROOT/$target_slot" ] && [ ! -L "$RELEASES_ROOT/$target_slot" ] || return 1
        mv "$CURRENT_PATH" "$RELEASES_ROOT/$target_slot" || return 1
        sync
        switch_phase_write rollback-target-stored || return 1
        if [ "$FAILPOINT" = crash-rollback-between-renames ]; then
            operation_log 'INJECTED_CRASH between rollback renames'
            return 95
        fi
        actual=""
    fi

    if [ -z "$actual" ]; then
        slot_marker_matches "$RELEASES_ROOT/$previous_slot" "$previous_slot" || return 1
        slot_marker_matches "$RELEASES_ROOT/$target_slot" "$target_slot" || return 1
        switch_phase_write rollback-target-stored || return 1
        mv "$RELEASES_ROOT/$previous_slot" "$CURRENT_PATH" || return 1
        sync
        switch_phase_write rollback-previous-active || return 1
        actual="$(current_slot 2>/dev/null || true)"
    fi

    [ "$actual" = "$previous_slot" ] || return 1
    switch_phase_write rollback-previous-active || return 1
    return 0
}

slot_health()
{
    local expected_slot slot_root
    expected_slot="$1"
    [ "$(current_slot 2>/dev/null || true)" = "$expected_slot" ] || return 1
    slot_root="$CURRENT_PATH"
    slot_tree_valid "$slot_root" || return 1
    xray_config_valid "$slot_root" || return 1
    services_health_captured || return 1
    webui_backend_health "$expected_slot" || return 1
    routes_verify_captured || return 1

    if [ -n "$HEALTH_HOOK" ]; then
        "$HEALTH_HOOK" "$expected_slot" || return 1
    fi

    return 0
}

failpoint()
{
    [ "$FAILPOINT" != "$1" ]
}

release_prune_before_stage()
{
    local active_slot candidate slot metadata hidden
    active_slot="$(current_slot)" || return 1

    hidden="$(find "$RELEASES_ROOT" -mindepth 1 -maxdepth 1 -type d -name '.*' -print -quit 2>/dev/null)"
    [ -z "$hidden" ] || return 1

    for candidate in "$RELEASES_ROOT"/*
    do
        [ -e "$candidate" ] || [ -L "$candidate" ] || continue
        [ -d "$candidate" ] && [ ! -L "$candidate" ] || return 1
        slot="${candidate##*/}"
        valid_id "$slot" || return 1
        [ "$slot" != "$active_slot" ] || return 1
        slot_marker_matches "$candidate" "$slot" || return 1

        operation_log "PRUNE_PREVIOUS_ROLLBACK slot=$slot"
        rm -rf "$candidate" || return 1
        metadata="$SLOT_META_ROOT/$slot.json"
        if [ -f "$metadata" ] && [ ! -L "$metadata" ]; then
            rm -f "$metadata" || return 1
        fi
    done
    sync
    return 0
}

release_prune_after_success()
{
    local keep_slot active_slot candidate slot metadata
    keep_slot="$1"
    valid_id "$keep_slot" || return 1
    active_slot="$(current_slot)" || return 1
    [ "$active_slot" != "$keep_slot" ] || return 1
    slot_marker_matches "$RELEASES_ROOT/$keep_slot" "$keep_slot" || return 1

    for candidate in "$RELEASES_ROOT"/*
    do
        [ -d "$candidate" ] && [ ! -L "$candidate" ] || continue
        slot="${candidate##*/}"
        valid_id "$slot" || continue
        [ "$slot" != "$keep_slot" ] || continue
        [ "$slot" != "$active_slot" ] || continue
        slot_marker_matches "$candidate" "$slot" || continue

        operation_log "PRUNE_RELEASE slot=$slot"
        rm -rf "$candidate" || return 1
        metadata="$SLOT_META_ROOT/$slot.json"
        if [ -f "$metadata" ] && [ ! -L "$metadata" ]; then
            rm -f "$metadata" || return 1
        fi
    done
    sync
    return 0
}

operation_target_cleanup()
{
    local target_slot active_slot target_root metadata staging
    target_slot="$(sed -n '1p' "$CURRENT_OPERATION_DIR/target" 2>/dev/null || true)"
    active_slot="$(current_slot 2>/dev/null || true)"

    if valid_id "$target_slot" && [ "$target_slot" != "$active_slot" ]; then
        target_root="$RELEASES_ROOT/$target_slot"
        if slot_marker_matches "$target_root" "$target_slot"; then
            rm -rf "$target_root" || return 1
            metadata="$SLOT_META_ROOT/$target_slot.json"
            if [ -f "$metadata" ] && [ ! -L "$metadata" ]; then
                rm -f "$metadata" || return 1
            fi
        fi
    fi

    staging="$RELEASES_ROOT/.staging-$CURRENT_OPERATION_ID"
    if [ -d "$staging" ] && [ ! -L "$staging" ]; then
        rm -rf "$staging" || return 1
    fi
    return 0
}

operation_cleanup_terminal()
{
    local queue_file work_dir
    queue_file="$QUEUE_ROOT/$CURRENT_OPERATION_ID.json"
    work_dir="$WORK_ROOT/$CURRENT_OPERATION_ID"
    state_seed_rollback || {
        operation_log 'STATE_SEED_ROLLBACK_FAILED'
        return 1
    }
    rm -f "$queue_file"
    case "$work_dir" in
        "$WORK_ROOT"/*) rm -rf "$work_dir" ;;
    esac
    operation_target_cleanup || operation_log 'TARGET_CLEANUP=SKIPPED'
    rm -rf "$REQUEST_LOCK"
}

rollback_after_switch()
{
    local operation reason previous_slot target_slot layout_rc
    operation="$1"
    reason="$2"
    previous_slot="$(sed -n '1p' "$CURRENT_OPERATION_DIR/previous-slot" 2>/dev/null || true)"
    target_slot="$(sed -n '1p' "$CURRENT_OPERATION_DIR/target" 2>/dev/null || true)"
    printf '%s\n' "$reason" | atomic_text_file "$CURRENT_OPERATION_DIR/rollback-reason" || true

    status_write \
        "$operation" rolling-back rollback 90 \
        'Проверка новой версии не пройдена; возвращается предыдущий каталог релиза.' \
        "$reason" true false || true
    operation_log "ROLLBACK_BEGIN reason=$reason target=$target_slot previous=$previous_slot"

    services_stop_captured >>"$CURRENT_OPERATION_LOG" 2>&1 || true

    layout_rc=0
    rollback_layout "$previous_slot" "$target_slot" || layout_rc=$?
    if [ "$layout_rc" -eq 95 ]; then
        return 95
    fi
    if [ "$layout_rc" -ne 0 ]; then
        status_write \
            "$operation" error rollback-failed 100 \
            'Автоматический откат не завершён; новые операции заблокированы.' \
            ROLLBACK_LAYOUT_FAILED true false || true
        operation_log 'ROLLBACK_FAILED layout'
        return 1
    fi

    if ! state_seed_rollback; then
        status_write \
            "$operation" error rollback-failed 100 \
            'Исходный релиз возвращён, но значения по умолчанию не удалось точно отменить.' \
            STATE_SEED_ROLLBACK_FAILED true true || true
        operation_log 'ROLLBACK_FAILED state-seed'
        return 1
    fi

    services_start_captured >>"$CURRENT_OPERATION_LOG" 2>&1 || {
        status_write \
            "$operation" error rollback-failed 100 \
            'Исходный релиз возвращён, но службы не восстановлены.' \
            ROLLBACK_SERVICE_FAILED true true || true
        operation_log 'ROLLBACK_FAILED services'
        return 1
    }

    if ! slot_health "$previous_slot"; then
        status_write \
            "$operation" error rollback-failed 100 \
            'Исходный релиз возвращён, но его здоровье не подтверждено.' \
            ROLLBACK_HEALTH_FAILED true true || true
        operation_log 'ROLLBACK_FAILED health'
        return 1
    fi

    status_write \
        "$operation" error rolled-back 100 \
        'Новая версия отклонена; предыдущая версия восстановлена.' \
        "$reason" true true || true
    operation_log 'ROLLBACK=PASS'
    operation_cleanup_terminal
    return 0
}

slot_metadata_publish()
{
    local slot target_json metadata
    slot="$1"
    target_json="$2"
    metadata="$SLOT_META_ROOT/$slot.json"

    jq -nc \
        --arg slot "$slot" \
        --arg lifecycle "$LIFECYCLE_CONTRACT" \
        --arg activatedBy "$CURRENT_OPERATION_ID" \
        --arg createdAt "$(now)" \
        --argjson source "$target_json" '
        {
          schemaVersion:1,
          slot:$slot,
          lifecycleContract:$lifecycle,
          candidateId:$source.candidateId,
          releaseId:$source.releaseId,
          appVersion:$source.appVersion,
          packageVersion:$source.packageVersion,
          architecture:$source.architecture,
          source:$source,
          activatedBy:$activatedBy,
          createdAt:$createdAt
        }' | atomic_json_file "$metadata"
}

stage_release()
{
    local bundle target_json work_dir candidate_id slot staging final
    local release_candidate release_id app_version
    bundle="$1"
    target_json="$2"
    work_dir="$3"

    candidate_id="$(printf '%s\n' "$target_json" | jq -r '.candidateId')"
    valid_id "$candidate_id" || return 1
    slot="$candidate_id--$CURRENT_OPERATION_ID"
    valid_id "$slot" || return 1
    staging="$RELEASES_ROOT/.staging-$CURRENT_OPERATION_ID"
    final="$RELEASES_ROOT/$slot"

    printf '%s\n' "$slot" >"$CURRENT_OPERATION_DIR/target" || return 1
    printf '%s\n' "$slot" >"$CURRENT_OPERATION_DIR/target-slot" || return 1

    if [ -d "$final" ] && [ ! -L "$final" ]; then
        [ "$(sed -n '1p' "$final/.broray-slot" 2>/dev/null || true)" = "$slot" ] || return 1
        slot_tree_valid "$final" || return 1
        slot_metrics_match "$final" "$target_json" || return 1
        slot_metadata_publish "$slot" "$target_json" || return 1
        printf '%s\n' "$final"
        return 0
    fi

    [ ! -e "$staging" ] && [ ! -L "$staging" ] || return 1
    mkdir "$staging" || return 1
    # BusyBox tar uses -o for "do not restore archive ownership" and rejects
    # the GNU-only long ownership flag on the physical Keenetic runtime.
    tar -xzof "$bundle" -C "$staging" || return 1
    printf '%s\n' "$slot" >"$staging/.broray-slot" || return 1
    chmod 600 "$staging/.broray-slot" 2>/dev/null || true
    slot_tree_valid "$staging" || return 1
    slot_metrics_match "$staging" "$target_json" || return 1
    shell_tree_valid "$staging" || return 1
    xray_config_valid "$staging" || return 1

    release_candidate="$(jq -r '.candidateId' "$staging/release.json")"
    release_id="$(jq -r '.releaseId' "$staging/release.json")"
    app_version="$(jq -r '.appVersion' "$staging/release.json")"
    [ "$release_candidate" = "$candidate_id" ] || return 1
    [ "$release_id" = "$(printf '%s\n' "$target_json" | jq -r '.releaseId')" ] || return 1
    [ "$app_version" = "$(printf '%s\n' "$target_json" | jq -r '.appVersion')" ] || return 1

    mv "$staging" "$final" || return 1
    sync
    slot_tree_valid "$final" || return 1
    slot_metrics_match "$final" "$target_json" || return 1
    slot_metadata_publish "$slot" "$target_json" || return 1
    printf '%s\n' "$final"
}

request_conflict_terminal()
{
    local operation stage
    operation="$1"
    stage="$2"
    status_write \
        "$operation" error "$stage" 100 \
        "Конфликтующая операция BROray активна или её блокировка неоднозначна: $ADMISSION_CONFLICT_STATE. Активный app-slot не изменялся." \
        GLOBAL_OPERATION_CONFLICT false false || true
    operation_log "GLOBAL_OPERATION_CONFLICT state=$ADMISSION_CONFLICT_STATE stage=$stage"
    operation_cleanup_terminal
}

request_process()
{
    local request_file operation_id operation target_json bundle_url bundle_sha bundle_size
    local work_dir bundle actual_size actual_sha slot_root previous_slot target_slot switch_rc required_opt_bytes
    request_file="$1"
    operation_id="$(jq -r '.operationId // ""' "$request_file" 2>/dev/null || true)"
    operation="$(jq -r '.operation // ""' "$request_file" 2>/dev/null || true)"

    valid_id "$operation_id" || return 1
    case "$operation" in update|reinstall) ;; *) return 1 ;; esac
    [ "$request_file" = "$QUEUE_ROOT/$operation_id.json" ] || return 1
    operation_select "$operation_id" || return 1

    # The durable request lock is the long-lived global updater fence.  Bind
    # its owner to the daemon for the whole operation (including rollback),
    # then recheck the other coordinators before the first download/mutation.
    request_lock_operation="$(sed -n '1p' "$REQUEST_LOCK/operation-id" 2>/dev/null || true)"
    [ "$request_lock_operation" = "$operation_id" ] || return 1
    printf '%s\n' "$$" >"$REQUEST_LOCK/pid.tmp.$$" || return 1
    mv -f "$REQUEST_LOCK/pid.tmp.$$" "$REQUEST_LOCK/pid" || return 1
    request_start="$(process_start_ticks "$$")" || return 1
    printf '%s\n' "$request_start" >"$REQUEST_LOCK/owner-start.tmp.$$" || return 1
    mv -f "$REQUEST_LOCK/owner-start.tmp.$$" "$REQUEST_LOCK/owner-start" || return 1
    if ! conflicting_operation_admission_clear; then
        request_conflict_terminal "$operation" admission || true
        return 1
    fi

    jq -e \
        --arg id "$operation_id" \
        --arg operation "$operation" '
        .schemaVersion == 1 and
        .operationId == $id and
        .operation == $operation and
        (.target | type == "object") and
        (.target.bundle | type == "object")
        ' "$request_file" >/dev/null 2>&1 || return 1

    target_json="$(jq -c '.target' "$request_file")" || return 1
    printf '%s\n' "$target_json" | jq -e '
        (.candidateId | type == "string") and
        (.releaseId | type == "string") and
        (.appVersion | type == "string") and
        (.packageVersion | type == "string") and
        (.architecture | type == "string") and
        .appSlot.layout == "broray-compact-app-slot/1" and
        (.appSlot.logicalBytes | type == "number") and (.appSlot.logicalBytes > 0) and
        (.appSlot.fileCount | type == "number") and (.appSlot.fileCount > 0) and
        (.appSlot.directoryCount | type == "number") and (.appSlot.directoryCount > 0) and
        (.appSlot.maxFileBytes | type == "number") and (.appSlot.maxFileBytes > 0) and
        .sharedRuntime.xray.path == "/opt/broray/runtime/xray" and
        .sharedRuntime.xray.mode == "preserve-installed" and
        .sharedRuntime.xray.bundled == false
        ' >/dev/null 2>&1 || return 1
    target_package_track_valid "$target_json" || {
        status_write "$operation" error compatibility 100 'Архитектура app-slot или регистрация OPKG несовместима; активная версия не изменялась.' PACKAGE_TRACK_MISMATCH false false || true
        operation_cleanup_terminal
        return 1
    }
    bundle_url="$(printf '%s\n' "$target_json" | jq -r '.bundle.url')"
    bundle_sha="$(printf '%s\n' "$target_json" | jq -r '.bundle.sha256')"
    bundle_size="$(printf '%s\n' "$target_json" | jq -r '.bundle.sizeBytes')"
    valid_sha256 "$bundle_sha" || return 1
    valid_positive_integer "$bundle_size" || return 1

    work_dir="$WORK_ROOT/$operation_id"
    mkdir -p "$work_dir" || return 1
    bundle="$work_dir/release.tar.gz"

    filesystem_has_bytes "$WORK_ROOT" "$bundle_size" || {
        status_write "$operation" error space 100 'В /tmp недостаточно места для компактного app-архива; активная версия не изменялась.' TEMP_SPACE_INSUFFICIENT false false || true
        operation_cleanup_terminal
        return 1
    }

    status_write \
        "$operation" running downloading 10 \
        'Скачивается компактный архив приложения без Xray.' \
        '' false false || return 1
    operation_log "DOWNLOAD url=$bundle_url expected_sha=$bundle_sha expected_size=$bundle_size"

    fetch_file "$bundle_url" "$bundle" || {
        status_write "$operation" error download 100 'Архив релиза не загружен; активная версия не изменялась.' DOWNLOAD_FAILED false false || true
        operation_cleanup_terminal
        return 1
    }

    actual_size="$(wc -c <"$bundle" | tr -d ' ')"
    actual_sha="$(sha256sum "$bundle" | awk 'NR == 1 {print $1; exit}')"
    if [ "$actual_size" != "$bundle_size" ] || [ "$actual_sha" != "$bundle_sha" ]; then
        status_write "$operation" error verify 100 'Архив релиза не прошёл внешнюю проверку; активная версия не изменялась.' BUNDLE_IDENTITY_MISMATCH false false || true
        operation_cleanup_terminal
        return 1
    fi

    status_write \
        "$operation" running verifying 30 \
        'Проверяются структура и внутренний манифест архива.' \
        '' false false || return 1

    archive_safe "$bundle" "$work_dir" || {
        status_write "$operation" error verify 100 'Структура архива небезопасна; активная версия не изменялась.' BUNDLE_UNSAFE false false || true
        operation_cleanup_terminal
        return 1
    }

    failpoint after-verify || {
        status_write "$operation" error injected 100 'Лабораторная остановка после проверки; активная версия не изменялась.' INJECTED_AFTER_VERIFY false false || true
        operation_cleanup_terminal
        return 1
    }

    # The download may be long.  Reclassify the foreign fence immediately
    # before the first persistent release-tree mutation; never delete or
    # recover another coordinator's namespace here.
    admission_hook_call daemon-pre-mutation || return 1
    if ! conflicting_operation_admission_clear; then
        request_conflict_terminal "$operation" pre-mutation || true
        return 1
    fi

    release_prune_before_stage || {
        status_write "$operation" error retention 100 'Не удалось безопасно освободить прежний компактный rollback-slot; активная версия не изменялась.' ROLLBACK_PRUNE_FAILED false false || true
        operation_cleanup_terminal
        return 1
    }

    required_opt_bytes="$(target_required_logical_bytes "$target_json")" || return 1
    filesystem_has_bytes "$APP_ROOT" "$required_opt_bytes" || {
        status_write "$operation" error space 100 'На /opt недостаточно места для одного нового компактного app-slot; активная версия не изменялась.' OPT_SPACE_INSUFFICIENT false false || true
        operation_cleanup_terminal
        return 1
    }

    status_write \
        "$operation" running staging 45 \
        'Приложение распаковывается в новый компактный slot; общий Xray не копируется.' \
        '' false false || return 1

    if ! slot_root="$(stage_release "$bundle" "$target_json" "$work_dir")"; then
        status_write "$operation" error staging 100 'Новый slot не прошёл офлайн-проверку; активная версия не изменялась.' STAGING_FAILED false false || true
        operation_cleanup_terminal
        return 1
    fi
    app_slot_count_valid || {
        status_write "$operation" error retention 100 'Нарушен предел двух компактных app-slot; переключение запрещено.' APP_SLOT_LIMIT_EXCEEDED false false || true
        operation_cleanup_terminal
        return 1
    }
    operation_log "STAGE_READY slot=$(sed -n '1p' "$CURRENT_OPERATION_DIR/target")"

    previous_slot="$(current_slot)" || {
        status_write "$operation" error pre-switch 100 'Текущий каталог релиза некорректен; переключение запрещено.' CURRENT_RELEASE_INVALID false false || true
        operation_cleanup_terminal
        return 1
    }
    target_slot="$(sed -n '1p' "$CURRENT_OPERATION_DIR/target")"
    printf '%s\n' "$previous_slot" >"$CURRENT_OPERATION_DIR/previous-slot" || return 1

    # Staging is inactive, but a competing coordinator must still block any
    # service stop, state seed or current-slot rename.  A legitimate publisher
    # also rechecks request.lock, which closes the opposite side of this race.
    admission_hook_call daemon-pre-switch || return 1
    if ! conflicting_operation_admission_clear; then
        request_conflict_terminal "$operation" pre-switch || true
        return 1
    fi

    routes_capture || {
        status_write "$operation" error routes 100 'Не удалось зафиксировать установленные маршруты; переключение не выполнялось.' ROUTE_CAPTURE_FAILED false false || true
        operation_cleanup_terminal
        return 1
    }

    services_capture || {
        status_write "$operation" error services 100 'Не удалось зафиксировать состояние служб; переключение не выполнялось.' SERVICE_CAPTURE_FAILED false false || true
        operation_cleanup_terminal
        return 1
    }

    status_write \
        "$operation" running stopping 60 \
        'Останавливаются только ранее запущенные службы BROray.' \
        '' false false || return 1

    if ! services_stop_captured; then
        services_start_captured >>"$CURRENT_OPERATION_LOG" 2>&1 || true
        status_write "$operation" error stopping 100 'Службы не остановлены согласованно; активный релиз не изменялся.' SERVICE_STOP_FAILED false false || true
        operation_cleanup_terminal
        return 1
    fi

    state_seed "$slot_root" || {
        state_seed_abort_before_switch \
            "$operation" state-seed \
            'Не удалось добавить отсутствующие значения по умолчанию; существующие данные не перезаписывались.' \
            STATE_SEED_FAILED || true
        return 1
    }

    if [ "$FAILPOINT" = crash-before-switch ]; then
        operation_log 'INJECTED_CRASH before release switch'
        return 97
    fi

    failpoint before-switch || {
        state_seed_abort_before_switch \
            "$operation" injected \
            'Лабораторная остановка до переключения; активная версия не изменялась.' \
            INJECTED_BEFORE_SWITCH || true
        return 1
    }

    : >"$CURRENT_OPERATION_DIR/mutation.started"
    status_write \
        "$operation" running switching 70 \
        'Компактный app-slot переключается двумя журналируемыми rename.' \
        '' true false || return 1
    operation_log "RELEASE_SWITCH previous=$previous_slot target=$target_slot"

    switch_rc=0
    switch_to_slot "$previous_slot" "$target_slot" || switch_rc=$?
    if [ "$switch_rc" -eq 96 ]; then
        return 96
    fi
    if [ "$switch_rc" -ne 0 ]; then
        rollback_after_switch "$operation" RELEASE_SWITCH_FAILED
        return 1
    fi

    if [ "$FAILPOINT" = crash-after-switch ]; then
        operation_log 'INJECTED_CRASH after release switch'
        return 98
    fi

    case "$FAILPOINT" in
        after-switch|crash-rollback-between-renames)
            rollback_after_switch "$operation" INJECTED_AFTER_SWITCH
            return 1
            ;;
    esac

    status_write \
        "$operation" running starting 82 \
        'Запускаются службы в том же состоянии, что до обновления.' \
        '' true false || return 1

    if ! services_start_captured; then
        rollback_after_switch "$operation" SERVICE_START_FAILED
        return 1
    fi

    status_write \
        "$operation" running health 92 \
        'Проверяются новый slot, конфигурация Xray, службы и JSON backend страницы BROray.' \
        '' true false || return 1

    if ! failpoint before-health || ! slot_health "$target_slot"; then
        rollback_after_switch "$operation" POST_SWITCH_HEALTH_FAILED
        return 1
    fi

    release_prune_after_success "$previous_slot" || operation_log 'RELEASE_PRUNE=SKIPPED'

    state_seed_commit || return 1
    status_write \
        "$operation" success complete 100 \
        'BROray переключён на проверенный компактный app-slot.' \
        '' true false || return 1
    operation_log "SUCCESS target=$target_slot"
    operation_cleanup_terminal
    return 0
}

recover_incomplete()
{
    local operation_id state_file request_file running operation previous_slot target_slot actual stage state work_dir retry_file retry_path
    local queue_temporary rollback_reason request_operation_id queue_file request_lock_operation
    operation_id="$(sed -n '1p' "$OPERATION_POINTER" 2>/dev/null || true)"
    if ! valid_id "$operation_id"; then
        [ -d "$REQUEST_LOCK" ] && [ ! -L "$REQUEST_LOCK" ] || return 0
        request_operation_id="$(sed -n '1p' "$REQUEST_LOCK/operation-id" 2>/dev/null || true)"
        valid_id "$request_operation_id" || return 1
        queue_file="$QUEUE_ROOT/$request_operation_id.json"
        request_file="$OPERATION_ROOT/$request_operation_id/request.json"
        state_file="$OPERATION_ROOT/$request_operation_id/state.json"
        [ -f "$queue_file" ] && [ ! -L "$queue_file" ] || return 1
        [ -f "$request_file" ] && [ ! -L "$request_file" ] || return 1
        [ -f "$state_file" ] && [ ! -L "$state_file" ] || return 1
        cmp -s "$queue_file" "$request_file" || return 1
        jq -e --arg operationId "$request_operation_id" '
          .schemaVersion == 1 and .operationId == $operationId and
          (.operation == "update" or .operation == "reinstall") and
          (.state == "queued" or .state == "running" or .state == "rolling-back") and
          .running == true
        ' "$state_file" >/dev/null 2>&1 || return 1
        printf '%s\n' "$request_operation_id" | atomic_text_file "$OPERATION_POINTER" || return 1
        operation_id="$request_operation_id"
    fi
    operation_select "$operation_id" || return 1
    state_file="$CURRENT_OPERATION_DIR/state.json"
    request_file="$CURRENT_OPERATION_DIR/request.json"
    [ -s "$state_file" ] || return 0

    running="$(jq -r '.running // false' "$state_file" 2>/dev/null || true)"
    if [ "$running" != true ]; then
        stage="$(jq -r '.stage // ""' "$state_file" 2>/dev/null || true)"
        state="$(jq -r '.state // ""' "$state_file" 2>/dev/null || true)"
        if [ "$stage" = rollback-failed ] ||
           [ "$stage" = recovery-ambiguous ] ||
           [ "$state" = recovery-required ]
        then
            operation_log 'RECOVERY_REQUIRED fence retained'
            return 1
        fi
        rm -f "$QUEUE_ROOT/$operation_id.json"
        if [ -d "$REQUEST_LOCK" ] && [ ! -L "$REQUEST_LOCK" ]; then
            request_lock_operation="$(sed -n '1p' "$REQUEST_LOCK/operation-id" 2>/dev/null || true)"
            if [ "$request_lock_operation" = "$operation_id" ]; then
                rm -rf "$REQUEST_LOCK" || return 1
            elif [ -n "$request_lock_operation" ]; then
                valid_id "$request_lock_operation" || return 1
                # This is a newer publisher/accepted request.  Its own queue
                # processing will rebind last-operation; terminal cleanup of
                # the older operation must not remove its fence.
                :
            else
                return 1
            fi
        fi
        return 0
    fi

    operation="$(jq -r '.operation // ""' "$state_file" 2>/dev/null || true)"
    case "$operation" in update|reinstall) ;; *) return 1 ;; esac
    [ -s "$request_file" ] || return 1

    if [ -f "$CURRENT_OPERATION_DIR/mutation.started" ]; then
        previous_slot="$(sed -n '1p' "$CURRENT_OPERATION_DIR/previous-slot" 2>/dev/null || true)"
        target_slot="$(sed -n '1p' "$CURRENT_OPERATION_DIR/target" 2>/dev/null || true)"
        valid_id "$previous_slot" || return 1
        valid_id "$target_slot" || return 1
        actual="$(current_slot 2>/dev/null || true)"
        rollback_reason="$(sed -n '1p' "$CURRENT_OPERATION_DIR/rollback-reason" 2>/dev/null || true)"

        if [ -n "$rollback_reason" ]; then
            operation_log "RECOVERY completing rollback reason=$rollback_reason"
            rollback_after_switch "$operation" "$rollback_reason"
            return $?
        fi

        if [ -z "$actual" ] && \
           [ ! -e "$CURRENT_PATH" ] && [ ! -L "$CURRENT_PATH" ] && \
           slot_marker_matches "$RELEASES_ROOT/$previous_slot" "$previous_slot" && \
           slot_marker_matches "$RELEASES_ROOT/$target_slot" "$target_slot"; then
            operation_log 'RECOVERY previous release stored; completing target rename.'
            mv "$RELEASES_ROOT/$target_slot" "$CURRENT_PATH" || {
                status_write "$operation" error recovery-failed 100 'Не удалось завершить прерванное переключение каталогов.' RECOVERY_TARGET_RENAME_FAILED true false || true
                return 1
            }
            sync
            switch_phase_write target-active || return 1
            actual="$(current_slot 2>/dev/null || true)"
        fi

        if [ "$actual" = "$target_slot" ]; then
            operation_log 'RECOVERY current directory is target; completing health gate.'
            services_start_captured >>"$CURRENT_OPERATION_LOG" 2>&1 || true
            if slot_health "$target_slot"; then
                release_prune_after_success "$previous_slot" || operation_log 'RELEASE_PRUNE=SKIPPED'
                state_seed_commit || return 1
                status_write "$operation" success complete 100 'Updater восстановил операцию после перезапуска; новый slot исправен.' '' true false || return 1
                operation_cleanup_terminal
                return 0
            fi
            rollback_after_switch "$operation" RECOVERY_TARGET_HEALTH_FAILED
            return $?
        fi

        if [ "$actual" != "$previous_slot" ] || \
           ! slot_marker_matches "$RELEASES_ROOT/$target_slot" "$target_slot"; then
            status_write "$operation" error recovery-ambiguous 100 'После перезапуска размещение релизов неоднозначно; новые операции заблокированы.' RECOVERY_LAYOUT_AMBIGUOUS true false || true
            return 1
        fi

        operation_log 'RECOVERY first rename did not occur; restoring services before retry.'
        state_seed_rollback || {
            status_write "$operation" error rollback-failed 100 \
                'После перезапуска не удалось точно отменить значения по умолчанию; новые операции заблокированы.' \
                STATE_SEED_ROLLBACK_FAILED true false || true
            operation_log 'RECOVERY_FAILED state-seed rollback'
            return 1
        }
        services_start_captured >>"$CURRENT_OPERATION_LOG" 2>&1 || true
        rm -f "$CURRENT_OPERATION_DIR/mutation.started"
        rm -f "$CURRENT_OPERATION_DIR/switch.phase"
    elif [ -s "$CURRENT_OPERATION_DIR/services.tsv" ]; then
        operation_log 'RECOVERY restoring pre-switch service state before retry.'
        state_seed_rollback || {
            status_write "$operation" error rollback-failed 100 \
                'После перезапуска не удалось точно отменить значения по умолчанию; новые операции заблокированы.' \
                STATE_SEED_ROLLBACK_FAILED false false || true
            operation_log 'RECOVERY_FAILED state-seed rollback'
            return 1
        }
        services_start_captured >>"$CURRENT_OPERATION_LOG" 2>&1 || return 1
    fi

    # A power loss before a completed switch may leave an inactive staged slot
    # and a partial download owned by this exact operation.  Retire those
    # owned bytes before replaying the same durable request; otherwise the
    # replay collides with its own old slot and cannot recover autonomously.
    operation_target_cleanup || return 1
    work_dir="$WORK_ROOT/$operation_id"
    case "$work_dir" in
        "$WORK_ROOT"/*) rm -rf "$work_dir" || return 1 ;;
        *) return 1 ;;
    esac
    for retry_file in \
        target previous-slot services.tsv \
        managed-routes.before managed-routes.interface managed-routes.sha256 \
        switch.phase mutation.started rollback-reason
    do
        retry_path="$CURRENT_OPERATION_DIR/$retry_file"
        if [ -e "$retry_path" ] || [ -L "$retry_path" ]; then
            [ -f "$retry_path" ] && [ ! -L "$retry_path" ] || return 1
            rm -f "$retry_path" || return 1
        fi
    done

    if [ ! -s "$QUEUE_ROOT/$operation_id.json" ]; then
        queue_temporary="$QUEUE_ROOT/$operation_id.json.tmp.$$"
        cp -p "$request_file" "$queue_temporary" || return 1
        mv -f "$queue_temporary" "$QUEUE_ROOT/$operation_id.json" || return 1
    fi

    return 0
}

# A CGI can be killed in the tiny interval after the atomic mkdir of the
# request fence and before its owner tuple is durable.  The daemon is the only
# process allowed to retire that bounded residue.  Two reads separated by one
# second distinguish a live publisher from an abandoned directory.
request_lock_recover_abandoned()
{
    local owner_before owner_after start_before start_after live_start operation_before operation_after entry pointer pointer_state

    if [ -e "$REQUEST_LOCK" ] || [ -L "$REQUEST_LOCK" ]; then
        [ -d "$REQUEST_LOCK" ] && [ ! -L "$REQUEST_LOCK" ] || return 1
    else
        return 0
    fi
    owner_before="$(sed -n '1p' "$REQUEST_LOCK/pid" 2>/dev/null || true)"
    start_before="$(sed -n '1p' "$REQUEST_LOCK/owner-start" 2>/dev/null || true)"
    operation_before="$(sed -n '1p' "$REQUEST_LOCK/operation-id" 2>/dev/null || true)"
    case "$owner_before" in
        '' ) ;;
        *[!0-9]*) return 1 ;;
        *)
            if kill -0 "$owner_before" 2>/dev/null; then
                live_start="$(process_start_ticks "$owner_before" 2>/dev/null || true)"
                [ -n "$start_before" ] && [ "$live_start" = "$start_before" ] && return 0
            fi
            ;;
    esac

    sleep 1
    if [ -e "$REQUEST_LOCK" ] || [ -L "$REQUEST_LOCK" ]; then
        [ -d "$REQUEST_LOCK" ] && [ ! -L "$REQUEST_LOCK" ] || return 1
    else
        return 0
    fi
    owner_after="$(sed -n '1p' "$REQUEST_LOCK/pid" 2>/dev/null || true)"
    start_after="$(sed -n '1p' "$REQUEST_LOCK/owner-start" 2>/dev/null || true)"
    operation_after="$(sed -n '1p' "$REQUEST_LOCK/operation-id" 2>/dev/null || true)"
    [ "$owner_before" = "$owner_after" ] || return 0
    [ "$start_before" = "$start_after" ] || return 0
    [ "$operation_before" = "$operation_after" ] || return 0
    case "$owner_after" in
        '' ) [ -z "$start_after" ] || return 1 ;;
        *[!0-9]*) return 1 ;;
        *)
            case "$start_after" in *[!0-9]*) return 1 ;; esac
            if kill -0 "$owner_after" 2>/dev/null; then
                live_start="$(process_start_ticks "$owner_after" 2>/dev/null || true)"
                # A publisher that is still between the pid and owner-start
                # renames is live but not yet fully identifiable.  Never
                # retire its fence; a stale PID reuse remains fail-closed.
                [ -n "$start_after" ] || return 1
                [ -n "$start_after" ] && [ "$live_start" = "$start_after" ] && return 0
            fi
            ;;
    esac

    for entry in "$REQUEST_LOCK"/* "$REQUEST_LOCK"/.[!.]* "$REQUEST_LOCK"/..?*
    do
        [ -e "$entry" ] || [ -L "$entry" ] || continue
        [ -f "$entry" ] && [ ! -L "$entry" ] || return 1
        case "${entry##*/}" in
            pid|operation-id|owner-start|pid.tmp.*|operation-id.tmp.*|owner-start.tmp.*) ;;
            *) return 1 ;;
        esac
        [ "$(wc -c <"$entry" 2>/dev/null | tr -d ' ')" -le 128 ] || return 1
    done

    if [ -n "$operation_after" ]; then
        valid_id "$operation_after" || return 1
        if [ -e "$QUEUE_ROOT/$operation_after.json" ] || [ -L "$QUEUE_ROOT/$operation_after.json" ]; then
            [ -f "$QUEUE_ROOT/$operation_after.json" ] &&
            [ ! -L "$QUEUE_ROOT/$operation_after.json" ] || return 1
            return 0
        fi
        pointer="$(sed -n '1p' "$OPERATION_POINTER" 2>/dev/null || true)"
        if [ "$pointer" = "$operation_after" ]; then
            rm -f "$OPERATION_POINTER" || return 1
        elif [ -n "$pointer" ]; then
            valid_id "$pointer" || return 1
            pointer_state="$OPERATION_ROOT/$pointer/state.json"
            [ -f "$pointer_state" ] && [ ! -L "$pointer_state" ] || return 1
            jq -e --arg operationId "$pointer" '
              .operationId == $operationId and .running == false and
              (.state == "success" or .state == "error") and
              (.stage != "rollback-failed") and
              (.stage != "recovery-ambiguous") and
              (.state != "recovery-required")
            ' "$pointer_state" >/dev/null 2>&1 || return 1
        fi
        if [ -d "$OPERATION_ROOT/$operation_after" ] &&
           [ ! -L "$OPERATION_ROOT/$operation_after" ]
        then
            rm -rf "$OPERATION_ROOT/$operation_after" || return 1
        fi
    fi
    rm -rf "$REQUEST_LOCK"
}

daemon_lock_release()
{
    rm -f "$DAEMON_READY" "$DAEMON_PID"
    rm -rf "$DAEMON_LOCK"
}

daemon_lock_claim()
{
    rm -f "$DAEMON_READY"
    if mkdir "$DAEMON_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$DAEMON_PID" || return 1
        return 0
    fi

    if daemon_identity_valid; then
        return 1
    fi

    rm -f "$DAEMON_PID"
    rm -rf "$DAEMON_LOCK"
    mkdir "$DAEMON_LOCK" || return 1
    printf '%s\n' "$$" >"$DAEMON_PID"
}

daemon_run()
{
    local request candidate request_result request_operation
    ensure_layout || return 1
    daemon_lock_claim || return 1
    trap daemon_lock_release EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    printf '%s  DAEMON_START pid=%s version=%s\n' "$(now)" "$$" "$UPDATER_VERSION" >>"$DAEMON_LOG"

    # Retire only a proven abandoned publisher residue before interpreting
    # last-operation.  In particular, a previous terminal pointer must not
    # make a fresh ownerless request fence fatal.
    request_lock_recover_abandoned || {
        printf '%s  REQUEST_LOCK_RECOVERY_STOPPED\n' "$(now)" >>"$DAEMON_LOG"
        return 1
    }

    recover_incomplete || {
        printf '%s  RECOVERY_STOPPED\n' "$(now)" >>"$DAEMON_LOG"
        return 1
    }

    printf '%s\n' "$$" | atomic_text_file "$DAEMON_READY" || return 1

    while :
    do
        request_lock_recover_abandoned || {
            printf '%s  REQUEST_LOCK_RECOVERY_STOPPED\n' "$(now)" >>"$DAEMON_LOG"
            return 1
        }
        request=""
        for candidate in "$QUEUE_ROOT"/*.json
        do
            [ -e "$candidate" ] || [ -L "$candidate" ] || continue
            [ -f "$candidate" ] && [ ! -L "$candidate" ] || {
                printf '%s  QUEUE_ENTRY_UNSAFE path=%s\n' "$(now)" "$candidate" >>"$DAEMON_LOG"
                return 1
            }
            request="$candidate"
            break
        done

        if [ -n "$request" ]; then
            request_result=0
            request_process "$request" || request_result=$?
            request_operation="$(jq -r '.operationId // ""' "$request" 2>/dev/null || true)"

            # A handled terminal error removes both durable acceptance
            # fences.  Any return that leaves either one behind is an
            # incomplete/fatal operation and must stop the daemon instead of
            # immediately processing the same queue entry again.
            if [ -e "$request" ] || [ -L "$request" ] || \
               [ -e "$REQUEST_LOCK" ] || [ -L "$REQUEST_LOCK" ]
            then
                printf '%s  PROCESSING_STOPPED operation=%s rc=%s\n' \
                    "$(now)" "$request_operation" "$request_result" >>"$DAEMON_LOG"
                return 1
            fi
        elif [ "$RUN_ONCE" = 1 ]; then
            break
        fi

        [ "$RUN_ONCE" = 1 ] && break
        if [ "$NO_SLEEP" != 1 ]; then
            sleep 2
        fi
    done
}

usage()
{
    printf '%s\n' 'Использование: broray-updater {daemon|check|request update|request reinstall|status|version}' >&2
}

main()
{
    local command_name
    command_name="${1:-}"
    shift 2>/dev/null || true

    case "$command_name" in
        daemon) daemon_run ;;
        check) release_check ;;
        request) request_enqueue "${1:-}" ;;
        status) ensure_layout && status_output ;;
        version) printf '%s\n' "$UPDATER_ENGINE" ;;
        *) usage; return 2 ;;
    esac
}

main "$@"
