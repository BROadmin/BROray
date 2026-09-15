#!/opt/bin/ash

# BROray 3.0.0 — backend страницы «BROray».
# Совместим с BusyBox ash. JSON формируется только через jq.

PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin
export PATH
umask 077

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_ROOT="$BRORAY_BASE"
export BRORAY_ROOT
BRORAY_BIN="$BRORAY_BASE/bin/broray-system"
BRORAY_RUN="$BRORAY_BASE/run/broray"
BRORAY_UPDATE="$BRORAY_BASE/update"
BRORAY_BACKUP="$BRORAY_BASE/backup"
BRORAY_STATE_ROOT="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
BRORAY_TMP_ROOT="${BRORAY_TMP_ROOT:-/tmp}"
BRORAY_LOCK_ROOT="${BRORAY_LOCK_ROOT:-$BRORAY_TMP_ROOT/broray-locks}"
BRORAY_WORKER_ROOT="${BRORAY_WORKER_ROOT:-$BRORAY_TMP_ROOT/broray-workers}"
BRORAY_SCRATCH_ROOT="${BRORAY_SCRATCH_ROOT:-$BRORAY_TMP_ROOT/broray-scratch}"
BRORAY_OPT_ROOT="${BRORAY_OPT_ROOT:-/opt}"
BRORAY_OPT_TMP="$BRORAY_TMP_ROOT"
BRORAY_OPKG_CACHE="${BRORAY_OPKG_CACHE:-$BRORAY_OPT_ROOT/var/cache/opkg}"
BRORAY_RELEASE_CLEANUP_LOG="$BRORAY_RUN/release-cleanup.log"
BRORAY_LEGACY_STATUS="$BRORAY_RUN/operation.json"
BRORAY_LEGACY_LOG="$BRORAY_RUN/operation.log"
BRORAY_STATUS="$BRORAY_LEGACY_STATUS"
BRORAY_LOG="$BRORAY_LEGACY_LOG"
BRORAY_UPDATE_CHECK_LOG="$BRORAY_RUN/update-check.log"
BRORAY_UPDATE_CACHE="$BRORAY_RUN/update.json"
BRORAY_LOCK="${BRORAY_LOCK:-$BRORAY_TMP_ROOT/broray-system-operation.lock}"
BRORAY_TRANSACTION_LOCK="${BRORAY_TRANSACTION_LOCK:-$BRORAY_TMP_ROOT/broray-update.lock}"
BRORAY_GLOBAL_LOCK="${BRORAY_GLOBAL_LOCK:-$BRORAY_TMP_ROOT/broray-global-operation.lock}"
BRORAY_GLOBAL_LOCK_HELD=false
BRORAY_COMPACT_GLOBAL_LOCK="${BRORAY_COMPACT_GLOBAL_LOCK:-/opt/var/lock/broray/global-operation.lock}"
BRORAY_COMPACT_REQUEST_LOCK="${BRORAY_COMPACT_REQUEST_LOCK:-/opt/var/lib/broray-updater/request.lock}"
BRORAY_UPDATER_STATE_ROOT="${BRORAY_UPDATER_STATE_ROOT:-/opt/var/lib/broray-updater}"
BRORAY_UPDATER_CACHE="${BRORAY_UPDATER_CACHE:-$BRORAY_UPDATER_STATE_ROOT/release-index.json}"
BRORAY_UPDATER_CTL="${BRORAY_UPDATER_CTL:-/opt/bin/broray-updaterctl}"
BRORAY_UPDATER_CACHE_MAX_BYTES="${BRORAY_UPDATER_CACHE_MAX_BYTES:-262144}"
BRORAY_LAST_BACKUP="${BRORAY_LAST_BACKUP:-$BRORAY_STATE_ROOT/last-backup}"
BRORAY_LEGACY_LAST_BACKUP="$BRORAY_RUN/last-backup"
BRORAY_OPERATION_LIBRARY="${BRORAY_OPERATION_LIBRARY:-$BRORAY_BASE/lib/operation-manager.sh}"
BRORAY_RELEASE_LIBRARY="${BRORAY_RELEASE_LIBRARY:-$BRORAY_BASE/lib/release-manifest.sh}"
BRORAY_STATUS_LIBRARY="${BRORAY_STATUS_LIBRARY:-$BRORAY_BASE/lib/status-contract.sh}"
BRORAY_TRANSACTION_LIBRARY="${BRORAY_TRANSACTION_LIBRARY:-$BRORAY_BASE/lib/package-transaction.sh}"
BRORAY_SYSTEM_DOT_LIB="${BRORAY_SYSTEM_DOT_LIB:-$BRORAY_BASE/lib/routes-dot.sh}"
# The WebUI and transaction engine must serialize the exact same paths.  Set
# these before sourcing package-transaction.sh so test roots and production
# cannot silently acquire a native OPKG fence for one path while mutating
# another.
BRORAY_TX_TMP_BASE="${BRORAY_TX_TMP_BASE:-$BRORAY_TMP_ROOT}"
BRORAY_TX_LOCK_DIR="${BRORAY_TX_LOCK_DIR:-$BRORAY_TRANSACTION_LOCK}"
BRORAY_TX_GLOBAL_LOCK="${BRORAY_TX_GLOBAL_LOCK:-$BRORAY_GLOBAL_LOCK}"
[ -r "$BRORAY_OPERATION_LIBRARY" ] && . "$BRORAY_OPERATION_LIBRARY"
[ -r "$BRORAY_RELEASE_LIBRARY" ] && . "$BRORAY_RELEASE_LIBRARY"
[ -r "$BRORAY_STATUS_LIBRARY" ] && . "$BRORAY_STATUS_LIBRARY"
[ -r "$BRORAY_TRANSACTION_LIBRARY" ] && . "$BRORAY_TRANSACTION_LIBRARY"
BRORAY_PACKAGE="broray"
BRORAY_INIT_ROOT="${BRORAY_INIT_ROOT:-/opt/etc/init.d}"
BRORAY_FEED_FILE="${BRORAY_FEED_FILE:-/opt/etc/opkg/broray.conf}"
BRORAY_UPDATER_CHANNEL_FILE="${BRORAY_UPDATER_CHANNEL_FILE:-/opt/var/lib/broray-updater/release-index-url}"
BRORAY_OPKG_LISTS_DIR="${BRORAY_OPKG_LISTS_DIR:-/opt/var/opkg-lists}"
BRORAY_INSTALLED_CONTROL="${BRORAY_INSTALLED_CONTROL:-/opt/lib/opkg/info/broray.control}"
BRORAY_INSTALLED_CANDIDATE_SHA="${BRORAY_INSTALLED_CANDIDATE_SHA:-/opt/lib/opkg/info/broray.candidate-sha256}"
BRORAY_LIGHTTPD_GUARD="${BRORAY_LIGHTTPD_GUARD:-$BRORAY_BASE/lib/lighttpd-guard.sh}"
BRORAY_OPKG_AUTH_ROOT="${BRORAY_OPKG_AUTH_ROOT:-/opt/var/lib/broray-opkg}"
BRORAY_UNINSTALL_AUTH="${BRORAY_UNINSTALL_AUTH:-$BRORAY_OPKG_AUTH_ROOT/uninstall-authorized.json}"
BRORAY_PROJECT_URL="https://docs.brovibe.cloud/broray/"
BRORAY_GITHUB_URL="https://github.com/BROadmin/BROray"
BRORAY_DONATE_URL="https://pay.cloudtips.ru/p/09b23d0a"
BRORAY_RELEASE_INDEX_URL_DEFAULT="https://api.brovibe.cloud/releases/staging/broray/3.0.0-r15c16/release.json"
BRORAY_RELEASE_INDEX_URL_FILE="${BRORAY_RELEASE_INDEX_URL_FILE:-$BRORAY_BASE/config/system/release-index-url}"
BRORAY_RELEASE_INDEX_URL="${BRORAY_RELEASE_INDEX_URL:-$BRORAY_RELEASE_INDEX_URL_DEFAULT}"
if [ -r "$BRORAY_RELEASE_INDEX_URL_FILE" ]; then
    broray_release_index_override="$(sed -n '1p' "$BRORAY_RELEASE_INDEX_URL_FILE" 2>/dev/null || true)"
    case "$broray_release_index_override" in https://*) BRORAY_RELEASE_INDEX_URL="$broray_release_index_override" ;; esac
fi

broray_system_now() {
    date -u '+%Y-%m-%dT%H:%M:%SZ'
}

broray_system_require_runtime() {
    command -v jq >/dev/null 2>&1 || return 1
    [ -d "$BRORAY_TMP_ROOT" ] && [ ! -L "$BRORAY_TMP_ROOT" ] && [ -w "$BRORAY_TMP_ROOT" ] || return 1
    mkdir -p "$BRORAY_RUN" "$BRORAY_UPDATE" "$BRORAY_BACKUP" "$BRORAY_WORKER_ROOT" "$BRORAY_SCRATCH_ROOT" "$BRORAY_STATE_ROOT" "$BRORAY_LOCK_ROOT" || return 1
    chmod 700 "$BRORAY_WORKER_ROOT" "$BRORAY_SCRATCH_ROOT" "$BRORAY_STATE_ROOT" "$BRORAY_LOCK_ROOT" 2>/dev/null || true
    command -v broray_operation_prepare_root >/dev/null 2>&1 && broray_operation_prepare_root || true
}

broray_system_require_transaction_runtime() {
    command -v jq >/dev/null 2>&1 || return 1
    [ -d "$BRORAY_TMP_ROOT" ] && [ ! -L "$BRORAY_TMP_ROOT" ] && [ -w "$BRORAY_TMP_ROOT" ] || return 1
    [ -d "$BRORAY_BASE" ] && [ -d "$BRORAY_RUN" ] || return 1
    mkdir -p "$BRORAY_WORKER_ROOT" "$BRORAY_SCRATCH_ROOT" "$BRORAY_LOCK_ROOT" "$BRORAY_STATE_ROOT" "$BRORAY_OPERATION_ROOT" || return 1
    chmod 700 "$BRORAY_WORKER_ROOT" "$BRORAY_SCRATCH_ROOT" "$BRORAY_LOCK_ROOT" "$BRORAY_STATE_ROOT" "$BRORAY_OPERATION_ROOT" 2>/dev/null || true
}

broray_system_legacy_opkg_feed_valid() {
    legacy_feed_path="${1:-$BRORAY_FEED_FILE}"
    legacy_feed_prefix='src/gz broray https://api.brovibe.cloud/releases/staging/broray/3.0.0-r14c'
    legacy_feed_suffix='/opkg/aarch64-3.10'

    if [ ! -e "$legacy_feed_path" ] && [ ! -L "$legacy_feed_path" ]; then
        return 0
    fi
    [ -f "$legacy_feed_path" ] && [ ! -L "$legacy_feed_path" ] || return 1
    [ "$(wc -l <"$legacy_feed_path" | tr -d ' ')" = 1 ] || return 1
    legacy_feed_line="$(sed -n '1p' "$legacy_feed_path")"
    case "$legacy_feed_line" in
        "$legacy_feed_prefix"*"$legacy_feed_suffix")
            legacy_feed_candidate="${legacy_feed_line#"$legacy_feed_prefix"}"
            legacy_feed_candidate="${legacy_feed_candidate%"$legacy_feed_suffix"}"
            case "$legacy_feed_candidate" in
                ''|*[!0-9]*) return 1 ;;
            esac
            ;;
        *) return 1 ;;
    esac
    return 0
}

broray_system_atomic_json() {
    target="$1"
    tmp="$target.tmp.$$"
    cat >"$tmp" || {
        rm -f "$tmp"
        return 1
    }
    jq -e . "$tmp" >/dev/null 2>&1 || {
        rm -f "$tmp"
        return 1
    }
    mv -f "$tmp" "$target"
}

broray_system_error_json() {
    code="$1"
    message="$2"
    jq -nc \
        --arg code "$code" \
        --arg message "$message" \
        '{ok:false,error:{code:$code,message:$message}}'
}

broray_system_error_details_json() {
    code="$1"
    message="$2"
    reason="${3:-}"
    evidence="${4:-}"
    jq -nc \
        --arg code "$code" \
        --arg message "$message" \
        --arg reason "$reason" \
        --arg evidence "$evidence" \
        '{ok:false,error:{code:$code,message:$message,
          reason:(if $reason=="" then null else $reason end),
          evidence:(if $evidence=="" then null else $evidence end),
          mutationStarted:false}}'
}

broray_system_last_backup_read() {
    backup_pointer=""
    if [ -r "$BRORAY_LAST_BACKUP" ]; then
        backup_pointer="$(sed -n '1p' "$BRORAY_LAST_BACKUP" 2>/dev/null)"
    elif [ -r "$BRORAY_LEGACY_LAST_BACKUP" ]; then
        backup_pointer="$(sed -n '1p' "$BRORAY_LEGACY_LAST_BACKUP" 2>/dev/null)"
    fi
    printf '%s\n' "$backup_pointer"
}

broray_system_last_backup_write() {
    backup_pointer="$1"
    mkdir -p "$(dirname "$BRORAY_LAST_BACKUP")" || return 1
    pointer_tmp="$BRORAY_LAST_BACKUP.tmp.$$"
    printf '%s\n' "$backup_pointer" >"$pointer_tmp" || return 1
    chmod 600 "$pointer_tmp" 2>/dev/null || true
    mv -f "$pointer_tmp" "$BRORAY_LAST_BACKUP" || return 1

    if [ -d "$BRORAY_BASE" ]; then
        mkdir -p "$BRORAY_RUN" 2>/dev/null || true
        printf '%s\n' "$backup_pointer" >"$BRORAY_LEGACY_LAST_BACKUP" 2>/dev/null || true
    fi
}

broray_system_last_backup_clear() {
    rm -f "$BRORAY_LAST_BACKUP" 2>/dev/null || true
    [ -d "$BRORAY_BASE" ] && rm -f "$BRORAY_LEGACY_LAST_BACKUP" 2>/dev/null || true
}

broray_system_version() {
    version=""

    if command -v broray_release_value >/dev/null 2>&1; then
        version="$(broray_release_value version '')"
    fi

    if [ -z "$version" ] && [ -r "$BRORAY_BASE/config/version" ]; then
        version="$(sed -n '1p' "$BRORAY_BASE/config/version")"
    fi

    if [ -z "$version" ] && [ -r "$BRORAY_BASE/VERSION" ]; then
        version="$(sed -n '1p' "$BRORAY_BASE/VERSION")"
    fi

    if [ -z "$version" ] && command -v opkg >/dev/null 2>&1; then
        version="$(
            opkg list-installed "$BRORAY_PACKAGE" 2>/dev/null |
                awk -F ' - ' 'NR == 1 {print $2}'
        )"
    fi

    if [ -z "$version" ] && [ -x "$BRORAY_BASE/bin/broray" ]; then
        version="$(
            "$BRORAY_BASE/bin/broray" version 2>/dev/null |
                awk 'NR == 1 {print $NF}'
        )"
    fi

    [ -n "$version" ] || version="не определена"
    printf '%s\n' "$version"
}

broray_system_build() {
    if command -v broray_release_value >/dev/null 2>&1; then
        release_id="$(broray_release_value releaseId '')"
        [ -n "$release_id" ] && { printf '%s\n' "$release_id"; return; }
    fi

    if [ -r "$BRORAY_BASE/BUILD" ]; then
        sed -n '1p' "$BRORAY_BASE/BUILD"
        return
    fi

    if [ -r "$BRORAY_BASE/.build" ]; then
        sed -n '1p' "$BRORAY_BASE/.build"
        return
    fi

    printf '%s\n' 'Сборка не определена'
}

broray_system_update_channel() {
    updater_url=""
    updater_bytes=""
    updater_lines=""

    if [ -e "$BRORAY_UPDATER_CHANNEL_FILE" ] || [ -L "$BRORAY_UPDATER_CHANNEL_FILE" ]; then
        if [ -f "$BRORAY_UPDATER_CHANNEL_FILE" ] &&
           [ ! -L "$BRORAY_UPDATER_CHANNEL_FILE" ] &&
           [ -r "$BRORAY_UPDATER_CHANNEL_FILE" ]
        then
            updater_bytes="$(wc -c <"$BRORAY_UPDATER_CHANNEL_FILE" 2>/dev/null | tr -d '[:space:]')"
            updater_lines="$(awk 'END {print NR + 0}' "$BRORAY_UPDATER_CHANNEL_FILE" 2>/dev/null)"
            case "$updater_bytes:$updater_lines" in
                ''|*[!0-9:]*) ;;
                *)
                    if [ "$updater_bytes" -le 4096 ] && [ "$updater_lines" -eq 1 ]; then
                        updater_url="$(sed -n '1p' "$BRORAY_UPDATER_CHANNEL_FILE" 2>/dev/null)"
                    fi
                    ;;
            esac
        fi

        if [ -n "$updater_url" ] &&
           printf '%s\n' "$updater_url" |
               LC_ALL=C awk '
                 /^https:\/\/[A-Za-z0-9][A-Za-z0-9.-]*(:[0-9]+)?(\/[^[:space:][:cntrl:]]*)?$/ {valid=1}
                 END {exit valid == 1 ? 0 : 1}
               '
        then
            case "$updater_url" in
                */stable/*) printf '%s\n' stable ;;
                */staging/*) printf '%s\n' staging ;;
                *) printf '%s\n' custom ;;
            esac
        else
            printf '%s\n' unknown
        fi
        return
    fi

    feed_url=""
    if [ -r "$BRORAY_FEED_FILE" ]; then
        feed_url="$(awk '$1 == "src/gz" && $2 == "broray" {print $3; exit}' "$BRORAY_FEED_FILE" 2>/dev/null)"
    fi
    case "$feed_url" in
        */stable/*) printf '%s\n' stable ;;
        */staging/*) printf '%s\n' staging ;;
        '') printf '%s\n' unknown ;;
        *) printf '%s\n' custom ;;
    esac
}

broray_system_architecture() {
    if [ -x "/opt/broray/runtime/xray" ]; then
        arch="$(
            "/opt/broray/runtime/xray" version 2>/dev/null |
                awk 'NR == 1 {
                    for (i = 1; i <= NF; i++) {
                        if ($i ~ /linux\//) {
                            sub(/.*linux\//, "", $i)
                            gsub(/[^[:alnum:]_.-].*$/, "", $i); print $i
                            exit
                        }
                    }
                }'
        )"
        [ -n "$arch" ] && {
            printf '%s\n' "$arch"
            return
        }
    fi

    uname -m 2>/dev/null || printf '%s\n' 'не определена'
}

broray_system_component_json() {
    id="$1"
    name="$2"
    path="$3"
    required="$4"
    version="$5"

    installed=false
    [ -e "$path" ] && installed=true

    jq -nc \
        --arg id "$id" \
        --arg name "$name" \
        --arg path "$path" \
        --arg version "$version" \
        --argjson installed "$installed" \
        --argjson required "$required" \
        '{
            id:$id,
            name:$name,
            path:$path,
            version:$version,
            installed:$installed,
            required:$required,
            healthy:$installed
        }'
}

broray_system_parser_available() {
    protocol="$1"
    case "$protocol" in
        vless)
            [ -s "$BRORAY_BASE/lib/parser-vless.sh" ]
            ;;
        vmess)
            [ -s "$BRORAY_BASE/lib/parser-vmess.sh" ]
            ;;
        trojan)
            [ -s "$BRORAY_BASE/lib/parser-trojan.sh" ]
            ;;
        hysteria2)
            [ -s "$BRORAY_BASE/lib/parser-hysteria2.sh" ]
            ;;
        shadowsocks)
            [ -s "$BRORAY_BASE/lib/parser-shadowsocks.sh" ]
            ;;
        *)
            return 1
            ;;
    esac
}

broray_system_is_pid() {
    case "${1:-}" in
        ''|*[!0-9]*) return 1 ;;
    esac
    return 0
}

broray_system_global_operation_running() {
    [ -d "$BRORAY_GLOBAL_LOCK" ] || return 1
    command -v broray_tx_control_owner_classify >/dev/null 2>&1 || return 0
    if ! broray_tx_control_owner_classify "$BRORAY_GLOBAL_LOCK/owner-identity.tsv"; then
        return 0
    fi
    case "$BRORAY_TX_CONTROL_OWNER_STATE" in
        live|ambiguous) return 0 ;;
        dead|reused) return 1 ;;
        *) return 0 ;;
    esac
}

broray_system_routes_resumable_pending() {
    progress_dir="$BRORAY_BASE/routes/operations"
    if [ ! -e "$progress_dir" ] && [ ! -L "$progress_dir" ]; then
        return 1
    fi
    [ -d "$progress_dir" ] && [ ! -L "$progress_dir" ] || return 0
    for progress_file in "$progress_dir"/*.json; do
        [ -e "$progress_file" ] || [ -L "$progress_file" ] || continue
        [ -f "$progress_file" ] && [ ! -L "$progress_file" ] || return 0
        jq -e '
          type == "object" and .kind == "routes" and
          (.running | type) == "boolean" and
          (.resumable | type) == "boolean" and
          (.bundleId | type) == "string" and (.bundleId | length) > 0 and
          all(.bundleId | explode[];
              (.>=48 and .<=57) or (.>=65 and .<=90) or
              (.>=97 and .<=122) or .==45 or .==46 or .==95)
        ' "$progress_file" >/dev/null 2>&1 || return 0
        jq -e '(.running == true) or (.resumable == true)' \
            "$progress_file" >/dev/null 2>&1 && return 0
    done
    return 1
}

broray_system_control_transition_require() {
    command -v broray_tx_control_transition_begin >/dev/null 2>&1 || return 1
    command -v broray_tx_control_transition_assert >/dev/null 2>&1 || return 1
    command -v broray_tx_control_transition_end >/dev/null 2>&1 || return 1
    command -v broray_tx_control_owner_classify >/dev/null 2>&1 || return 1
    command -v broray_tx_control_owner_identity_capture >/dev/null 2>&1 || return 1
    command -v broray_tx_control_owner_identity_read >/dev/null 2>&1 || return 1
    command -v broray_tx_control_owner_write_atomic >/dev/null 2>&1 || return 1
    command -v broray_tx_control_owner_assert_self >/dev/null 2>&1 || return 1
    command -v broray_tx_files_equal >/dev/null 2>&1 || return 1
    [ "$BRORAY_TX_TMP_BASE" = "$BRORAY_TMP_ROOT" ] || return 1
    [ "$BRORAY_TX_LOCK_DIR" = "$BRORAY_TRANSACTION_LOCK" ] || return 1
    [ "$BRORAY_TX_GLOBAL_LOCK" = "$BRORAY_GLOBAL_LOCK" ] || return 1
}

broray_system_control_file_bounded() {
    broray_control_file_path="$1"
    [ -f "$broray_control_file_path" ] && [ ! -L "$broray_control_file_path" ] || return 1
    broray_control_file_bytes="$(wc -c <"$broray_control_file_path" 2>/dev/null | tr -d ' ')" || return 1
    broray_system_is_pid "$broray_control_file_bytes" || return 1
    [ "$broray_control_file_bytes" -le "${BRORAY_TX_METADATA_MAX_BYTES:-1048576}" ]
}

broray_system_global_control_validate() {
    broray_global_expected_action="${1:-}"
    broray_global_expected_id="${2:-}"
    [ -d "$BRORAY_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_GLOBAL_LOCK" ] || return 1
    for broray_global_validate_entry in \
        "$BRORAY_GLOBAL_LOCK"/* "$BRORAY_GLOBAL_LOCK"/.[!.]* "$BRORAY_GLOBAL_LOCK"/..?*
    do
        [ -e "$broray_global_validate_entry" ] || [ -L "$broray_global_validate_entry" ] || continue
        case "${broray_global_validate_entry##*/}" in
            owner-identity.tsv|scope|action|bundle|startedAt|operation-id) ;;
            *) return 1 ;;
        esac
        broray_system_control_file_bounded "$broray_global_validate_entry" || return 1
    done
    for broray_global_validate_name in \
        owner-identity.tsv scope action bundle startedAt operation-id
    do
        broray_system_control_file_bounded "$BRORAY_GLOBAL_LOCK/$broray_global_validate_name" || return 1
    done
    broray_tx_control_owner_identity_read "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" || return 1
    [ "$(wc -l <"$BRORAY_GLOBAL_LOCK/scope" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
    [ "$(wc -l <"$BRORAY_GLOBAL_LOCK/action" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
    [ "$(wc -l <"$BRORAY_GLOBAL_LOCK/startedAt" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
    [ "$(wc -l <"$BRORAY_GLOBAL_LOCK/operation-id" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
    [ "$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/scope" 2>/dev/null)" = system ] || return 1
    [ ! -s "$BRORAY_GLOBAL_LOCK/bundle" ] || return 1
    broray_global_validate_action="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/action" 2>/dev/null)"
    case "$broray_global_validate_action" in update|reinstall|restore|uninstall) ;; *) return 1 ;; esac
    broray_global_validate_id="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/operation-id" 2>/dev/null)"
    case "$broray_global_validate_id" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 1 ;; esac
    case "$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/startedAt" 2>/dev/null)" in
        [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]Z) ;;
        *) return 1 ;;
    esac
    [ -z "$broray_global_expected_action" ] || [ "$broray_global_validate_action" = "$broray_global_expected_action" ] || return 1
    [ -z "$broray_global_expected_id" ] || [ "$broray_global_validate_id" = "$broray_global_expected_id" ] || return 1
}

broray_system_global_ownerless_control_validate() {
    [ -d "$BRORAY_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_GLOBAL_LOCK" ] || return 1
    [ ! -e "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ] &&
        [ ! -L "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ] || return 1
    for broray_global_ownerless_required in scope action bundle startedAt operation-id; do
        broray_system_control_file_bounded \
            "$BRORAY_GLOBAL_LOCK/$broray_global_ownerless_required" || return 1
    done
    [ "$(wc -l <"$BRORAY_GLOBAL_LOCK/scope" 2>/dev/null | tr -d ' ')" -eq 1 ] &&
    [ "$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/scope" 2>/dev/null)" = system ] &&
    [ ! -s "$BRORAY_GLOBAL_LOCK/bundle" ] || return 1
    BRORAY_SYSTEM_GLOBAL_RELATION_ACTION="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/action" 2>/dev/null)"
    case "$BRORAY_SYSTEM_GLOBAL_RELATION_ACTION" in update|reinstall|restore|uninstall) ;; *) return 1 ;; esac
    BRORAY_SYSTEM_GLOBAL_RELATION_ID="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/operation-id" 2>/dev/null)"
    case "$BRORAY_SYSTEM_GLOBAL_RELATION_ID" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 1 ;; esac
    case "$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/startedAt" 2>/dev/null)" in
        [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]Z) ;;
        *) return 1 ;;
    esac
}

# Ownerless bytes are uncommitted.  They are retryable only while the native
# OPKG control fence is held, after exact allowlist/content validation, and
# only when no transaction control can be related to them.
broray_system_global_ownerless_retire_locked() {
    broray_global_ownerless_allow_related="${1:-0}"
    broray_tx_control_transition_assert || return 1
    [ -d "$BRORAY_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_GLOBAL_LOCK" ] || return 1
    [ ! -e "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ] &&
        [ ! -L "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ] || return 1
    if [ -e "$BRORAY_TRANSACTION_LOCK" ] || [ -L "$BRORAY_TRANSACTION_LOCK" ]; then
        [ "$broray_global_ownerless_allow_related" -eq 1 ] || return 1
    fi
    for broray_global_ownerless_entry in \
        "$BRORAY_GLOBAL_LOCK"/* "$BRORAY_GLOBAL_LOCK"/.[!.]* "$BRORAY_GLOBAL_LOCK"/..?*
    do
        [ -e "$broray_global_ownerless_entry" ] || [ -L "$broray_global_ownerless_entry" ] || continue
        case "${broray_global_ownerless_entry##*/}" in
            scope|action|bundle|startedAt|operation-id) ;;
            *) return 1 ;;
        esac
        broray_system_control_file_bounded "$broray_global_ownerless_entry" || return 1
    done
    if [ -e "$BRORAY_GLOBAL_LOCK/scope" ]; then
        [ "$(wc -l <"$BRORAY_GLOBAL_LOCK/scope" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
        [ "$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/scope" 2>/dev/null)" = system ] || return 1
    fi
    if [ -e "$BRORAY_GLOBAL_LOCK/action" ]; then
        [ "$(wc -l <"$BRORAY_GLOBAL_LOCK/action" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
        case "$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/action" 2>/dev/null)" in
            update|reinstall|restore|uninstall) ;;
            *) return 1 ;;
        esac
    fi
    if [ -e "$BRORAY_GLOBAL_LOCK/bundle" ]; then
        [ ! -s "$BRORAY_GLOBAL_LOCK/bundle" ] || return 1
    fi
    if [ -e "$BRORAY_GLOBAL_LOCK/operation-id" ]; then
        [ "$(wc -l <"$BRORAY_GLOBAL_LOCK/operation-id" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
        broray_global_ownerless_id="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/operation-id" 2>/dev/null)"
        case "$broray_global_ownerless_id" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 1 ;; esac
    fi
    if [ -e "$BRORAY_GLOBAL_LOCK/startedAt" ]; then
        [ "$(wc -l <"$BRORAY_GLOBAL_LOCK/startedAt" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
        case "$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/startedAt" 2>/dev/null)" in
            [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]Z) ;;
            *) return 1 ;;
        esac
    fi
    broray_global_ownerless_retired="$BRORAY_TMP_ROOT/.broray-global-retired-$$-$(date -u '+%s')-${RANDOM:-0}"
    [ ! -e "$broray_global_ownerless_retired" ] && [ ! -L "$broray_global_ownerless_retired" ] || return 1
    # One rename removes the whole ownerless collection from the canonical
    # namespace.  A killed recursive unlink can only strand the hidden bounded
    # directory, which the next mutex holder scavenges below.
    mv "$BRORAY_GLOBAL_LOCK" "$broray_global_ownerless_retired" || return 1
    rm -rf "$broray_global_ownerless_retired"
}

broray_system_global_retired_scavenge_locked() {
    broray_tx_control_transition_assert || return 1
    for broray_global_retired in "$BRORAY_TMP_ROOT"/.broray-global-retired-*; do
        [ -e "$broray_global_retired" ] || [ -L "$broray_global_retired" ] || continue
        [ -d "$broray_global_retired" ] && [ ! -L "$broray_global_retired" ] || return 1
        [ ! -e "$broray_global_retired/owner-identity.tsv" ] &&
            [ ! -L "$broray_global_retired/owner-identity.tsv" ] || return 1
        for broray_global_retired_entry in \
            "$broray_global_retired"/* "$broray_global_retired"/.[!.]* "$broray_global_retired"/..?*
        do
            [ -e "$broray_global_retired_entry" ] || [ -L "$broray_global_retired_entry" ] || continue
            case "${broray_global_retired_entry##*/}" in
                scope|action|bundle|startedAt|operation-id) ;;
                *) return 1 ;;
            esac
            broray_system_control_file_bounded "$broray_global_retired_entry" || return 1
        done
        rm -rf "$broray_global_retired" || return 1
    done
}

# Validate only the immutable global<->transaction relation here.  Owner
# identity is classified independently because an inherited-FD package child
# deliberately owns transaction control while its WebUI parent still owns the
# global operation.  Equality is therefore not a lifecycle invariant.
broray_system_transaction_relation_locked() {
    broray_global_relation_action="$1"
    broray_global_relation_id="$2"
    broray_tx_control_transition_assert || return 1
    [ -d "$BRORAY_TRANSACTION_LOCK" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK" ] || return 1
    for broray_global_tx_entry in \
        "$BRORAY_TRANSACTION_LOCK"/* "$BRORAY_TRANSACTION_LOCK"/.[!.]* "$BRORAY_TRANSACTION_LOCK"/..?*
    do
        [ -e "$broray_global_tx_entry" ] || [ -L "$broray_global_tx_entry" ] || continue
        case "${broray_global_tx_entry##*/}" in
            operation-id|owner-identity.tsv|operation-type|started-at|source-version|target-version) ;;
            *) return 1 ;;
        esac
        broray_system_control_file_bounded "$broray_global_tx_entry" || return 1
    done
    for broray_global_tx_name in operation-id operation-type started-at source-version target-version; do
        broray_system_control_file_bounded "$BRORAY_TRANSACTION_LOCK/$broray_global_tx_name" || return 1
        [ "$(wc -l <"$BRORAY_TRANSACTION_LOCK/$broray_global_tx_name" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
    done
    [ "$(sed -n '1p' "$BRORAY_TRANSACTION_LOCK/operation-id" 2>/dev/null)" = "$broray_global_relation_id" ] || return 1
    [ "$(sed -n '1p' "$BRORAY_TRANSACTION_LOCK/operation-type" 2>/dev/null)" = "$broray_global_relation_action" ] || return 1
    if [ -e "$BRORAY_TRANSACTION_LOCK/owner-identity.tsv" ] ||
       [ -L "$BRORAY_TRANSACTION_LOCK/owner-identity.tsv" ]; then
        broray_system_control_file_bounded "$BRORAY_TRANSACTION_LOCK/owner-identity.tsv" || return 1
        broray_tx_control_owner_classify "$BRORAY_TRANSACTION_LOCK/owner-identity.tsv" || return 1
        BRORAY_SYSTEM_TX_OWNER_STATE="$BRORAY_TX_CONTROL_OWNER_STATE"
        BRORAY_SYSTEM_TX_OWNER_PID="$BRORAY_TX_CONTROL_OWNER_PID"
    else
        BRORAY_SYSTEM_TX_OWNER_STATE=ownerless
        BRORAY_SYSTEM_TX_OWNER_PID=""
    fi
}

broray_system_transaction_matches_global_locked() {
    broray_tx_control_transition_assert || return 1
    broray_system_global_control_validate "${1:-}" "${2:-}" || return 1
    BRORAY_SYSTEM_GLOBAL_RELATION_ACTION="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/action" 2>/dev/null)"
    BRORAY_SYSTEM_GLOBAL_RELATION_ID="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/operation-id" 2>/dev/null)"
    broray_tx_control_owner_classify "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" || return 1
    BRORAY_SYSTEM_GLOBAL_OWNER_STATE="$BRORAY_TX_CONTROL_OWNER_STATE"
    BRORAY_SYSTEM_GLOBAL_OWNER_PID="$BRORAY_TX_CONTROL_OWNER_PID"
    broray_system_transaction_relation_locked \
        "$BRORAY_SYSTEM_GLOBAL_RELATION_ACTION" "$BRORAY_SYSTEM_GLOBAL_RELATION_ID"
}

broray_system_global_retire_locked() {
    broray_global_retire_action="${1:-}"
    broray_global_retire_id="${2:-}"
    broray_tx_control_transition_assert || return 1
    broray_system_global_control_validate "$broray_global_retire_action" "$broray_global_retire_id" || return 1
    broray_tx_control_owner_assert_self "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" || return 1
    # The authoritative owner is always the first deletion.  A crash after
    # this point leaves a structurally bounded ownerless residue, never a
    # falsely live or permanent userspace claim.
    rm -f "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" || return 1
    broray_system_global_ownerless_retire_locked 1
}

broray_system_global_lock_recover_stale_locked() {
    broray_tx_control_transition_assert || return 2
    broray_system_global_retired_scavenge_locked || return 2
    [ ! -e "$BRORAY_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_GLOBAL_LOCK" ] && return 0
    [ -d "$BRORAY_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_GLOBAL_LOCK" ] || return 2
    if [ ! -e "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ] &&
       [ ! -L "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ]; then
        broray_global_ownerless_related=0
        if [ -e "$BRORAY_TRANSACTION_LOCK" ] || [ -L "$BRORAY_TRANSACTION_LOCK" ]; then
            broray_system_global_ownerless_control_validate || return 2
            broray_system_transaction_relation_locked \
                "$BRORAY_SYSTEM_GLOBAL_RELATION_ACTION" \
                "$BRORAY_SYSTEM_GLOBAL_RELATION_ID" || return 2
            broray_global_ownerless_related=1
            case "$BRORAY_SYSTEM_TX_OWNER_STATE" in
                live) return 1 ;;
                ambiguous) return 2 ;;
                dead|reused|ownerless) ;;
                *) return 2 ;;
            esac
        fi
        broray_system_global_ownerless_retire_locked \
            "$broray_global_ownerless_related" || return 2
        return 0
    fi
    broray_system_global_control_validate || return 2
    broray_tx_control_owner_classify "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" || return 2
    BRORAY_SYSTEM_GLOBAL_OWNER_STATE="$BRORAY_TX_CONTROL_OWNER_STATE"
    BRORAY_SYSTEM_GLOBAL_OWNER_PID="$BRORAY_TX_CONTROL_OWNER_PID"
    BRORAY_SYSTEM_TX_OWNER_STATE=absent
    if [ -e "$BRORAY_TRANSACTION_LOCK" ] || [ -L "$BRORAY_TRANSACTION_LOCK" ]; then
        broray_system_transaction_matches_global_locked || return 2
    fi
    global_owner_before_sha="$(broray_tx_sha "$BRORAY_GLOBAL_LOCK/owner-identity.tsv")" || return 2
    case "$BRORAY_SYSTEM_GLOBAL_OWNER_STATE" in
        live) return 1 ;;
        ambiguous) return 2 ;;
        dead|reused) ;;
        *) return 2 ;;
    esac
    case "$BRORAY_SYSTEM_TX_OWNER_STATE" in
        live) return 1 ;;
        ambiguous) return 2 ;;
        absent|dead|reused|ownerless) ;;
        *) return 2 ;;
    esac
    [ "$(broray_tx_sha "$BRORAY_GLOBAL_LOCK/owner-identity.tsv")" = "$global_owner_before_sha" ] || return 2
    broray_tx_control_transition_assert || return 2
    # Take over stale ownership atomically while the kernel fence excludes all
    # legitimate publishers.  No persistent userspace claim is created.
    broray_tx_control_owner_write_atomic "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" || return 2
    broray_tx_control_owner_assert_self "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" || return 2
    broray_system_global_retire_locked || return 2
    return 0
}

broray_system_global_lock_recover_stale() {
    broray_system_control_transition_require || return 2
    broray_tx_control_transition_begin || return 2
    broray_system_global_lock_recover_stale_locked
    global_recover_rc=$?
    broray_tx_control_transition_end || return 2
    return "$global_recover_rc"
}

broray_system_global_lock_acquire() {
    global_action="${1:-system}"
    global_operation_id="${2:-}"
    case "$global_action" in update|reinstall|restore|uninstall) ;; *) return 2 ;; esac
    case "$global_operation_id" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 2 ;; esac
    broray_system_control_transition_require || return 2
    global_lock_parent="${BRORAY_GLOBAL_LOCK%/*}"
    [ "$global_lock_parent" != "$BRORAY_GLOBAL_LOCK" ] &&
        [ -d "$global_lock_parent" ] && [ ! -L "$global_lock_parent" ] &&
        [ -w "$global_lock_parent" ] || return 2
    # Recover in dependency order before publishing a new global owner.  A
    # stale global controller is retired first; only then may the canonical
    # transaction engine normalize an ownerless lock or terminal workspace.
    # Publishing the new global operation before this pass would make the old
    # transaction tuple fail its exact operation-id/action relation forever.
    broray_system_global_lock_recover_stale
    global_recover_rc=$?
    [ "$global_recover_rc" -eq 0 ] || return 2
    [ ! -e "$BRORAY_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_GLOBAL_LOCK" ] || return 2
    command -v broray_tx_recover_stale_control >/dev/null 2>&1 || return 2
    broray_tx_recover_stale_control || return 2
    [ ! -e "$BRORAY_TRANSACTION_LOCK" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK" ] || return 2
    [ ! -e "$BRORAY_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_GLOBAL_LOCK" ] || return 2
    broray_tx_control_transition_begin || return 2
    global_acquire_rc=2
    if ! broray_system_routes_resumable_pending &&
       [ ! -e "$BRORAY_TRANSACTION_LOCK" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK" ]; then
        if [ ! -e "$BRORAY_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_GLOBAL_LOCK" ] &&
           [ ! -e "$BRORAY_TRANSACTION_LOCK" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK" ] &&
           mkdir "$BRORAY_GLOBAL_LOCK" 2>/dev/null
        then
            chmod 700 "$BRORAY_GLOBAL_LOCK" 2>/dev/null || true
            if printf '%s\n' system >"$BRORAY_GLOBAL_LOCK/scope" &&
               printf '%s\n' "$global_action" >"$BRORAY_GLOBAL_LOCK/action" &&
               : >"$BRORAY_GLOBAL_LOCK/bundle" &&
               printf '%s\n' "$(broray_system_now)" >"$BRORAY_GLOBAL_LOCK/startedAt" &&
               printf '%s\n' "$global_operation_id" >"$BRORAY_GLOBAL_LOCK/operation-id" &&
               broray_tx_control_transition_assert &&
               [ ! -e "$BRORAY_TRANSACTION_LOCK" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK" ] &&
               broray_tx_control_owner_write_atomic "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" &&
               broray_tx_control_owner_assert_self "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" &&
               broray_system_global_control_validate "$global_action" "$global_operation_id"
            then
                global_acquire_rc=0
            elif [ ! -e "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ] &&
                 [ ! -L "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ]; then
                broray_system_global_ownerless_retire_locked 2>/dev/null || true
            fi
        fi
    fi
    broray_tx_control_transition_end || return 2
    [ "$global_acquire_rc" -eq 0 ] || return "$global_acquire_rc"
    BRORAY_GLOBAL_LOCK_HELD=true
    return 0
}

broray_system_global_lock_transfer() {
    global_pid="${1:-}"
    global_operation_id="${2:-}"
    global_identity_source="${3:-}"
    global_handoff="${4:-}"
    broray_system_is_pid "$global_pid" || return 1
    case "$global_operation_id" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 1 ;; esac
    [ -f "$global_identity_source" ] && [ ! -L "$global_identity_source" ] || return 1
    [ "$global_handoff" = "$BRORAY_WORKER_ROOT/handoff-$global_operation_id" ] || return 1
    broray_system_control_transition_require || return 1
    broray_tx_control_transition_begin || return 1
    global_transfer_rc=1
    global_transfer_committed=false
    global_action="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/action" 2>/dev/null)"
    if broray_system_global_control_validate "$global_action" "$global_operation_id" &&
       [ ! -e "$BRORAY_TRANSACTION_LOCK" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK" ] &&
       [ -d "$global_handoff" ] && [ ! -L "$global_handoff" ] &&
       [ -f "$global_handoff/ready" ] && [ ! -L "$global_handoff/ready" ] &&
       [ -f "$global_handoff/control-released" ] && [ ! -L "$global_handoff/control-released" ] &&
       [ "$(sed -n '1p' "$global_handoff/control-released" 2>/dev/null)" = "$global_operation_id" ] &&
       [ ! -e "$global_handoff/go" ] && [ ! -L "$global_handoff/go" ] &&
       [ -f "$global_handoff/worker-identity.tsv" ] && [ ! -L "$global_handoff/worker-identity.tsv" ] &&
       broray_tx_files_equal "$global_identity_source" "$global_handoff/worker-identity.tsv"
    then
        global_transfer_owner_ok=true
        broray_tx_control_owner_classify "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" || global_transfer_owner_ok=false
        if [ "$global_transfer_owner_ok" = true ] &&
           [ "$BRORAY_TX_CONTROL_OWNER_STATE" = live ] && [ "$BRORAY_TX_CONTROL_OWNER_PID" = "$$" ]; then
            global_source_sha_before="$(broray_tx_sha "$global_identity_source")" || global_source_sha_before=""
            broray_tx_control_owner_classify "$global_identity_source" || global_source_sha_before=""
            if [ -n "$global_source_sha_before" ] &&
               [ "$BRORAY_TX_CONTROL_OWNER_STATE" = live ] &&
               [ "$BRORAY_TX_CONTROL_OWNER_PID" = "$global_pid" ] &&
               [ "$(sed -n '1p' "$global_handoff/ready" 2>/dev/null)" = "$global_source_sha_before" ] &&
               [ "$(broray_tx_sha "$global_identity_source")" = "$global_source_sha_before" ]
            then
                global_identity_part="$BRORAY_TX_CONTROL_MUTEX_WORK/global-owner-transfer.tsv"
                global_identity_committed=false
                if [ ! -e "$global_identity_part" ] && [ ! -L "$global_identity_part" ] &&
                   cp -p "$global_identity_source" "$global_identity_part"
                then
                    if broray_tx_files_equal "$global_identity_source" "$global_identity_part" &&
                       broray_tx_control_transition_assert &&
                       [ "$(broray_tx_sha "$global_identity_source")" = "$global_source_sha_before" ] &&
                       mv -f "$global_identity_part" "$BRORAY_GLOBAL_LOCK/owner-identity.tsv"
                    then
                        global_identity_committed=true
                    else
                        rm -f "$global_identity_part" 2>/dev/null || true
                    fi
                fi
                if [ "$global_identity_committed" = true ] &&
                   [ -f "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ] &&
                   broray_tx_files_equal "$global_identity_source" "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" &&
                   broray_tx_control_owner_classify "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" &&
                   [ "$BRORAY_TX_CONTROL_OWNER_STATE" = live ] &&
                   [ "$BRORAY_TX_CONTROL_OWNER_PID" = "$global_pid" ] &&
                   [ ! -e "$global_handoff/go" ] && [ ! -L "$global_handoff/go" ]
                then
                    # Ownership is committed while the native OPKG mutex is
                    # held, but the worker must not be woken until that mutex
                    # has actually been released.  Publishing `go` here made
                    # the worker race the parent's transition teardown.
                    global_transfer_committed=true
                fi
            fi
        fi
    fi
    broray_tx_control_transition_end || return 1
    [ "$global_transfer_committed" = true ] || return 1
    BRORAY_GLOBAL_LOCK_HELD=false
    global_go_part="$BRORAY_WORKER_ROOT/.handoff-go-$global_operation_id-$$"
    if [ ! -e "$global_go_part" ] && [ ! -L "$global_go_part" ] &&
       (set -C; printf '%s\n' "$global_operation_id" >"$global_go_part") &&
       chmod 600 "$global_go_part" &&
       [ ! -e "$global_handoff/go" ] && [ ! -L "$global_handoff/go" ] &&
       mv "$global_go_part" "$global_handoff/go"
    then
        global_transfer_rc=0
    else
        rm -f "$global_go_part" 2>/dev/null || true
    fi
    [ "$global_transfer_rc" -eq 0 ]
}

broray_system_handoff_validate_locked() {
    global_handoff="$1"
    global_operation_id="$2"
    broray_tx_control_transition_assert || return 1
    [ "$global_handoff" = "$BRORAY_WORKER_ROOT/handoff-$global_operation_id" ] || return 1
    [ -d "$global_handoff" ] && [ ! -L "$global_handoff" ] || return 1
    for global_handoff_entry in "$global_handoff"/* "$global_handoff"/.[!.]* "$global_handoff"/..?*; do
        [ -e "$global_handoff_entry" ] || [ -L "$global_handoff_entry" ] || continue
        case "${global_handoff_entry##*/}" in
            worker-identity.tsv|ready|control-released|go) ;;
            *) return 1 ;;
        esac
        broray_system_control_file_bounded "$global_handoff_entry" || return 1
    done
    if [ -e "$global_handoff/worker-identity.tsv" ] || [ -L "$global_handoff/worker-identity.tsv" ]; then
        broray_system_control_file_bounded "$global_handoff/worker-identity.tsv" || return 1
    fi
    if [ -e "$global_handoff/ready" ] || [ -L "$global_handoff/ready" ]; then
        broray_system_control_file_bounded "$global_handoff/ready" || return 1
        [ "$(wc -l <"$global_handoff/ready" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
        global_handoff_ready="$(sed -n '1p' "$global_handoff/ready" 2>/dev/null)"
        case "$global_handoff_ready" in *[!0-9a-f]*|'') return 1 ;; esac
        [ "${#global_handoff_ready}" -eq 64 ] || return 1
        if [ -e "$global_handoff/worker-identity.tsv" ] || [ -L "$global_handoff/worker-identity.tsv" ]; then
            [ -f "$global_handoff/worker-identity.tsv" ] && [ ! -L "$global_handoff/worker-identity.tsv" ] || return 1
            [ "$(broray_tx_sha "$global_handoff/worker-identity.tsv")" = "$global_handoff_ready" ] || return 1
        fi
    fi
    if [ -e "$global_handoff/go" ] || [ -L "$global_handoff/go" ]; then
        broray_system_control_file_bounded "$global_handoff/go" || return 1
        [ "$(wc -l <"$global_handoff/go" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
        [ "$(sed -n '1p' "$global_handoff/go" 2>/dev/null)" = "$global_operation_id" ] || return 1
    fi
    if [ -e "$global_handoff/control-released" ] || [ -L "$global_handoff/control-released" ]; then
        broray_system_control_file_bounded "$global_handoff/control-released" || return 1
        [ "$(wc -l <"$global_handoff/control-released" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
        [ "$(sed -n '1p' "$global_handoff/control-released" 2>/dev/null)" = "$global_operation_id" ] || return 1
    fi
}

broray_system_handoff_prepare() {
    global_handoff="$1"
    global_operation_id="$2"
    global_action="$3"
    [ "$global_handoff" = "$BRORAY_WORKER_ROOT/handoff-$global_operation_id" ] || return 1
    broray_system_control_transition_require || return 1
    broray_tx_control_transition_begin || return 1
    global_handoff_prepare_rc=1
    if broray_system_global_control_validate "$global_action" "$global_operation_id" &&
       broray_tx_control_owner_assert_self "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" &&
       [ ! -e "$BRORAY_TRANSACTION_LOCK" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK" ] &&
       [ ! -e "$global_handoff" ] && [ ! -L "$global_handoff" ] &&
       mkdir "$global_handoff"
    then
        chmod 700 "$global_handoff" 2>/dev/null || true
        global_handoff_prepare_rc=0
    fi
    broray_tx_control_transition_end || return 1
    return "$global_handoff_prepare_rc"
}

broray_system_handoff_retire_locked() {
    global_handoff="$1"
    global_operation_id="$2"
    broray_system_handoff_validate_locked "$global_handoff" "$global_operation_id" || return 1
    # worker-identity is the handoff owner/commit record and is therefore the
    # first retire deletion.  Remaining partial metadata is retryable under
    # the same kernel fence.
    if [ -e "$global_handoff/worker-identity.tsv" ] || [ -L "$global_handoff/worker-identity.tsv" ]; then
        broray_tx_control_owner_classify "$global_handoff/worker-identity.tsv" || return 1
        case "$BRORAY_TX_CONTROL_OWNER_STATE" in
            live) [ "$BRORAY_TX_CONTROL_OWNER_PID" = "$$" ] || return 1 ;;
            dead|reused) ;;
            *) return 1 ;;
        esac
        rm -f "$global_handoff/worker-identity.tsv" || return 1
    fi
    rm -f "$global_handoff/ready" "$global_handoff/control-released" "$global_handoff/go" || return 1
    rmdir "$global_handoff"
}

broray_system_handoff_retire() {
    global_handoff="$1"
    global_operation_id="$2"
    [ ! -e "$global_handoff" ] && [ ! -L "$global_handoff" ] && return 0
    broray_system_control_transition_require || return 1
    broray_tx_control_transition_begin || return 1
    broray_system_handoff_retire_locked "$global_handoff" "$global_operation_id"
    global_handoff_retire_rc=$?
    broray_tx_control_transition_end || return 1
    return "$global_handoff_retire_rc"
}

broray_system_global_lock_worker_adopt() {
    global_operation_id="${1:-}"
    global_handoff="${2:-}"
    case "$global_operation_id" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 1 ;; esac
    [ "$global_handoff" = "$BRORAY_WORKER_ROOT/handoff-$global_operation_id" ] || return 1
    broray_system_control_transition_require || return 1
    broray_tx_control_transition_begin || return 1
    global_worker_publish_rc=1
    global_action="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/action" 2>/dev/null)"
    global_worker_identity_part="$BRORAY_TX_CONTROL_MUTEX_WORK/handoff-worker-identity.tsv"
    if broray_system_global_control_validate "$global_action" "$global_operation_id" &&
       [ ! -e "$BRORAY_TRANSACTION_LOCK" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK" ] &&
       [ -d "$global_handoff" ] && [ ! -L "$global_handoff" ] &&
       [ ! -e "$global_handoff/ready" ] && [ ! -L "$global_handoff/ready" ] &&
       [ ! -e "$global_handoff/control-released" ] && [ ! -L "$global_handoff/control-released" ] &&
       [ ! -e "$global_handoff/go" ] && [ ! -L "$global_handoff/go" ] &&
       [ ! -e "$global_handoff/worker-identity.tsv" ] && [ ! -L "$global_handoff/worker-identity.tsv" ] &&
       [ ! -e "$global_worker_identity_part" ] && [ ! -L "$global_worker_identity_part" ] &&
       broray_tx_control_owner_identity_capture "$$" "$global_worker_identity_part"
    then
        global_worker_identity_sha="$(broray_tx_sha "$global_worker_identity_part")" || global_worker_identity_sha=""
        if [ -n "$global_worker_identity_sha" ] &&
           printf '%s\n' "$global_worker_identity_sha" >"$global_handoff/ready" &&
           broray_tx_control_transition_assert &&
           mv -f "$global_worker_identity_part" "$global_handoff/worker-identity.tsv" &&
           broray_system_handoff_validate_locked "$global_handoff" "$global_operation_id"
        then
            global_worker_publish_rc=0
        else
            rm -f "$global_worker_identity_part" 2>/dev/null || true
            if [ ! -e "$global_handoff/worker-identity.tsv" ] &&
               [ ! -L "$global_handoff/worker-identity.tsv" ]; then
                rm -f "$global_handoff/ready" 2>/dev/null || true
            fi
        fi
    fi
    broray_tx_control_transition_end || return 1
    [ "$global_worker_publish_rc" -eq 0 ] || return 1
    # `ready` proves the worker identity, but it is published while the worker
    # still owns the native OPKG control fence.  Publish a second atomic marker
    # only after that fence has been released, so the parent cannot race its
    # transfer against the worker's transition teardown.
    global_worker_released_part="$BRORAY_WORKER_ROOT/.handoff-control-released-$global_operation_id-$$"
    [ ! -e "$global_worker_released_part" ] && [ ! -L "$global_worker_released_part" ] || return 1
    (set -C; printf '%s\n' "$global_operation_id" >"$global_worker_released_part") || return 1
    chmod 600 "$global_worker_released_part" || {
        rm -f "$global_worker_released_part" 2>/dev/null || true
        return 1
    }
    mv "$global_worker_released_part" "$global_handoff/control-released" || {
        rm -f "$global_worker_released_part" 2>/dev/null || true
        return 1
    }
    global_wait=0
    while [ ! -f "$global_handoff/go" ] || [ -L "$global_handoff/go" ]; do
        global_wait=$((global_wait + 1))
        [ "$global_wait" -lt 15 ] || return 1
        sleep 1
    done
    [ "$(sed -n '1p' "$global_handoff/go" 2>/dev/null)" = "$global_operation_id" ] || return 1
    broray_tx_control_transition_begin || return 1
    global_worker_adopt_rc=1
    if broray_system_global_control_validate "$global_action" "$global_operation_id" &&
       [ ! -e "$BRORAY_TRANSACTION_LOCK" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK" ] &&
       broray_tx_control_owner_assert_self "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" &&
       broray_tx_files_equal "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" \
           "$global_handoff/worker-identity.tsv" &&
       broray_system_handoff_retire_locked "$global_handoff" "$global_operation_id"
    then
        global_worker_adopt_rc=0
    fi
    broray_tx_control_transition_end || return 1
    [ "$global_worker_adopt_rc" -eq 0 ] || return 1
    BRORAY_GLOBAL_LOCK_HELD=true
}

broray_system_worker_terminate_exact() {
    global_worker_pid="${1:-}"
    global_worker_identity="${2:-}"
    broray_system_is_pid "$global_worker_pid" || return 1
    [ -f "$global_worker_identity" ] && [ ! -L "$global_worker_identity" ] || return 1
    broray_tx_control_owner_classify "$global_worker_identity" || return 1
    [ "$BRORAY_TX_CONTROL_OWNER_STATE" = live ] &&
        [ "$BRORAY_TX_CONTROL_OWNER_PID" = "$global_worker_pid" ] || return 1
    kill -TERM "$global_worker_pid" 2>/dev/null
}

broray_system_global_lock_release() {
    [ ! -e "$BRORAY_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_GLOBAL_LOCK" ] && {
        BRORAY_GLOBAL_LOCK_HELD=false
        return 0
    }
    broray_system_control_transition_require || return 1
    broray_tx_control_transition_begin || return 1
    global_release_rc=1
    global_release_action="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/action" 2>/dev/null)"
    global_release_id="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/operation-id" 2>/dev/null)"
    if broray_system_global_control_validate "$global_release_action" "$global_release_id" &&
       broray_tx_control_owner_assert_self "$BRORAY_GLOBAL_LOCK/owner-identity.tsv"
    then
        global_release_relation_ok=false
        if [ -e "$BRORAY_TRANSACTION_LOCK" ] || [ -L "$BRORAY_TRANSACTION_LOCK" ]; then
            if broray_system_transaction_matches_global_locked "$global_release_action" "$global_release_id"; then
                global_release_relation_ok=true
            fi
        else
            global_release_relation_ok=true
        fi
        if [ "$global_release_relation_ok" = true ] &&
           broray_system_global_retire_locked "$global_release_action" "$global_release_id"; then
            global_release_rc=0
        fi
    fi
    broray_tx_control_transition_end || return 1
    [ "$global_release_rc" -eq 0 ] || return 1
    BRORAY_GLOBAL_LOCK_HELD=false
    return 0
}

broray_system_operation_running() {
    if [ -e "$BRORAY_GLOBAL_LOCK" ] || [ -L "$BRORAY_GLOBAL_LOCK" ]; then
        broray_system_global_operation_running && return 0
    fi
    operation_id=""
    operation=""
    if [ -s "$BRORAY_STATUS" ] && [ ! -L "$BRORAY_STATUS" ]; then
        operation_id="$(jq -r '.operationId // empty' "$BRORAY_STATUS" 2>/dev/null)"
        operation="$(jq -r '.operation // empty' "$BRORAY_STATUS" 2>/dev/null)"
    fi
    case "$operation_id" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 1 ;; esac

    if [ -e "$BRORAY_TRANSACTION_LOCK" ] || [ -L "$BRORAY_TRANSACTION_LOCK" ]; then
        [ -d "$BRORAY_TRANSACTION_LOCK" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK" ] || return 1
        for lock_file in operation-id owner-identity.tsv operation-type started-at source-version target-version; do
            [ -f "$BRORAY_TRANSACTION_LOCK/$lock_file" ] && [ ! -L "$BRORAY_TRANSACTION_LOCK/$lock_file" ] || return 1
        done
        lock_operation_id="$(sed -n '1p' "$BRORAY_TRANSACTION_LOCK/operation-id" 2>/dev/null)"
        lock_operation="$(sed -n '1p' "$BRORAY_TRANSACTION_LOCK/operation-type" 2>/dev/null)"
        [ "$lock_operation_id" = "$operation_id" ] || return 1
        [ -z "$operation" ] || [ "$lock_operation" = "$operation" ] || return 1
        command -v broray_tx_control_owner_classify >/dev/null 2>&1 || return 0
        if ! broray_tx_control_owner_classify "$BRORAY_TRANSACTION_LOCK/owner-identity.tsv"; then
            return 0
        fi
        case "$BRORAY_TX_CONTROL_OWNER_STATE" in
            live|ambiguous) return 0 ;;
            dead|reused) return 1 ;;
            *) return 0 ;;
        esac
    fi

    [ -d "$BRORAY_LOCK" ] && [ ! -L "$BRORAY_LOCK" ] || return 1
    [ -f "$BRORAY_LOCK/pid" ] && [ ! -L "$BRORAY_LOCK/pid" ] || return 1
    [ -f "$BRORAY_LOCK/operation-id" ] && [ ! -L "$BRORAY_LOCK/operation-id" ] || return 1
    [ "$(sed -n '1p' "$BRORAY_LOCK/operation-id" 2>/dev/null)" = "$operation_id" ] || return 1
    pid="$(sed -n '1p' "$BRORAY_LOCK/pid" 2>/dev/null)"
    broray_system_is_pid "$pid" || return 1
    kill -0 "$pid" 2>/dev/null
}

broray_system_recover_stale_lock() {
    [ ! -e "$BRORAY_LOCK" ] && [ ! -L "$BRORAY_LOCK" ] && return 0
    broray_system_operation_running && return 1
    # Legacy system locks have no complete current-operation identity.  A
    # dead PID is therefore ambiguous and is retained fail-closed.
    return 2
}

broray_system_select_operation() {
    selected_operation_id="$1"
    if command -v broray_operation_select >/dev/null 2>&1; then
        broray_operation_select "$selected_operation_id" || return 1
        BRORAY_STATUS="$BRORAY_CURRENT_OPERATION_STATUS"
        BRORAY_LOG="$BRORAY_CURRENT_OPERATION_LOG"
    fi
}

broray_system_select_last_operation() {
    if command -v broray_operation_select_last >/dev/null 2>&1 &&
       broray_operation_select_last
    then
        BRORAY_STATUS="$BRORAY_CURRENT_OPERATION_STATUS"
        BRORAY_LOG="$BRORAY_CURRENT_OPERATION_LOG"
        return 0
    fi
    BRORAY_STATUS="$BRORAY_LEGACY_STATUS"
    BRORAY_LOG="$BRORAY_LEGACY_LOG"
    return 1
}

broray_system_publish_legacy_status() {
    [ "$BRORAY_STATUS" = "$BRORAY_LEGACY_STATUS" ] && return 0
    [ -d "$BRORAY_BASE" ] || return 0
    mkdir -p "$BRORAY_RUN" 2>/dev/null || return 0
    legacy_tmp="$BRORAY_LEGACY_STATUS.tmp.$$"
    cp -p "$BRORAY_STATUS" "$legacy_tmp" 2>/dev/null || return 0
    mv -f "$legacy_tmp" "$BRORAY_LEGACY_STATUS" 2>/dev/null || {
        rm -f "$legacy_tmp"
        return 0
    }
}

broray_system_status_write() {
    operation_id="$1"
    operation="$2"
    state="$3"
    stage="$4"
    progress="$5"
    message="$6"
    error_message="${7:-}"
    updated_at="$(broray_system_now)"

    if [ "${BRORAY_CURRENT_OPERATION_ID:-}" != "$operation_id" ]; then
        broray_system_select_operation "$operation_id" || return 1
    fi

    jq -nc \
        --arg operationId "$operation_id" \
        --arg operation "$operation" \
        --arg state "$state" \
        --arg stage "$stage" \
        --arg message "$message" \
        --arg error "$error_message" \
        --arg updatedAt "$updated_at" \
        --argjson progress "$progress" \
        '{
            ok:true,
            operationId:$operationId,
            operation:$operation,
            state:$state,
            stage:$stage,
            progress:$progress,
            message:$message,
            error:(if $error == "" then null else $error end),
            running:($state == "queued" or $state == "running" or $state == "restoring"),
            updatedAt:$updatedAt
        }' | broray_system_atomic_json "$BRORAY_STATUS" || return 1
    broray_system_publish_legacy_status >/dev/null 2>&1 || true
    case "$state" in
        success|error)
            command -v broray_operation_finalize_from_state >/dev/null 2>&1 &&
                broray_operation_finalize_from_state "$operation_id" >/dev/null 2>&1 || true
            ;;
    esac
}

broray_system_log_reset() {
    : >"$BRORAY_LOG"
    if [ "$BRORAY_LOG" != "$BRORAY_LEGACY_LOG" ]; then
        : >"$BRORAY_LEGACY_LOG"
    fi
}

broray_system_log() {
    log_line="$(broray_system_now)  $*"
    printf '%s\n' "$log_line" >>"$BRORAY_LOG"
    if [ "$BRORAY_LOG" != "$BRORAY_LEGACY_LOG" ]; then
        printf '%s\n' "$log_line" >>"$BRORAY_LEGACY_LOG"
    fi
}

broray_system_status_json() {
    broray_system_require_runtime || {
        broray_system_error_json RUNTIME_ERROR 'Не удалось подготовить служебный каталог.'
        return 1
    }

    broray_system_select_last_operation >/dev/null 2>&1 || true

    if [ ! -s "$BRORAY_STATUS" ]; then
        jq -nc \
            --arg updatedAt "$(broray_system_now)" \
            '{
                ok:true,
                operationId:null,
                operation:null,
                state:"idle",
                stage:"idle",
                progress:0,
                message:"Операции ещё не выполнялись.",
                error:null,
                running:false,
                logTail:"",
                updatedAt:$updatedAt
            }'
        return
    fi

    if jq -e '.running == true' "$BRORAY_STATUS" >/dev/null 2>&1; then
        if ! broray_system_operation_running; then
            operation_id="$(jq -r '.operationId // "unknown"' "$BRORAY_STATUS")"
            operation="$(jq -r '.operation // "unknown"' "$BRORAY_STATUS")"
            broray_system_status_write \
                "$operation_id" "$operation" error interrupted 100 \
                'Операция была прервана.' \
                'Фоновый процесс не подтверждён; lock сохранён для безопасной классификации.'
        fi
    fi

    log_tail=""
    [ -r "$BRORAY_LOG" ] && log_tail="$(tail -n 80 "$BRORAY_LOG" 2>/dev/null)"
    jq -c --arg logTail "$log_tail" '. + {logTail:$logTail}' "$BRORAY_STATUS"
}

broray_system_available_version() {
    if [ -s "$BRORAY_UPDATE_CACHE" ]; then
        jq -r '.availableVersion // empty' "$BRORAY_UPDATE_CACHE" 2>/dev/null
    fi
}

broray_system_available_package_version() {
    if [ -s "$BRORAY_UPDATE_CACHE" ]; then
        jq -r '.availablePackageVersion // .availableVersion // empty' "$BRORAY_UPDATE_CACHE" 2>/dev/null
    fi
}

broray_system_package_to_app_version() {
    package_version="${1:-}"
    explicit_version=""

    [ -n "$package_version" ] || return 1
    explicit_version="$(
        broray_system_feed_value "$package_version" X-BROray-Version 2>/dev/null || true
    )"
    if [ -n "$explicit_version" ]; then
        printf '%s\n' "$explicit_version"
        return 0
    fi

    case "$package_version" in
        *-[0-9]*)
            printf '%s\n' "${package_version%-*}"
            ;;
        *)
            printf '%s\n' "$package_version"
            ;;
    esac
}

broray_system_user_backup_valid() {
    local archive format
    archive="$1"
    [ -f "$archive" ] && [ ! -L "$archive" ] && [ -s "$archive" ] || return 1
    broray_system_archive_safe "$archive" || return 1
    tar -tzf "$archive" 2>/dev/null |
        grep -Eq '^\./\.broray-protected-paths$|^\.broray-protected-paths$' || return 1
    tar -tzf "$archive" 2>/dev/null |
        grep -Eq '^\./\.broray-protected-manifest$|^\.broray-protected-manifest$' || return 1
    if ! format="$(tar -xzOf "$archive" ./.broray-protected-format 2>/dev/null)"; then
        format="$(tar -xzOf "$archive" .broray-protected-format 2>/dev/null)" || return 1
    fi
    [ "$format" = 'BROray protected backup/2' ]
}

broray_system_protected_roots() {
    printf '%s\n' \
        backup backups config/active-server config/config.json config/interface.json \
        config/subscriptions config/disabled-subscription-servers \
        config/system/settings.json config/system/server-auto-switch.json \
        config/system/dns.json config/system/dot.json config/dns config/dot \
        data deleted-subscriptions subscriptions servers routes/config.json \
        routes/bundles.json routes/custom.json routes/user-import-version \
        routes/catalog routes/dot/config.json routes/dot/state.json \
        routes/installed routes/state routes/backup
}

broray_system_protected_backup_owned_cleanup() {
    if [ "${preserved_part_owned:-false}" = true ]; then
        [ -n "${preserved_part_path:-}" ] || return 1
        rm -f "$preserved_part_path" 2>/dev/null || return 1
        preserved_part_owned=false
    fi
    if [ "${preserved_stage_owned:-false}" = true ]; then
        case "${preserved_stage_path:-}" in
            "$BRORAY_SCRATCH_ROOT"/protected-backup-*)
                rm -rf "$preserved_stage_path" 2>/dev/null || return 1
                preserved_stage_owned=false
                ;;
            *) return 1 ;;
        esac
    fi
    return 0
}

broray_system_protected_backup_create() {
    local destination part stage roots paths manifest item absolute relative meta mode uid gid bytes sha
    destination="$1"
    part="$destination.part.$$"
    stage="$BRORAY_SCRATCH_ROOT/protected-backup-$$"
    roots="$stage/.broray-protected-paths"
    paths="$stage/.paths"
    manifest="$stage/.broray-protected-manifest"

    preserved_part_path=""
    preserved_part_owned=false
    preserved_stage_path=""
    preserved_stage_owned=false
    [ ! -e "$destination" ] && [ ! -L "$destination" ] || return 1
    [ ! -e "$part" ] && [ ! -L "$part" ] || return 1
    [ ! -e "$stage" ] && [ ! -L "$stage" ] || return 1
    mkdir "$stage" || return 1
    preserved_stage_path="$stage"
    preserved_stage_owned=true
    chmod 700 "$stage" || { broray_system_protected_backup_owned_cleanup; return 1; }
    : >"$roots" || { broray_system_protected_backup_owned_cleanup; return 1; }
    {
        broray_system_protected_roots
        for item in "$BRORAY_BASE"/routes/manifests/user-*.json; do
            [ -f "$item" ] && [ ! -L "$item" ] || continue
            printf 'routes/manifests/%s\n' "${item##*/}"
        done
    } | while IFS= read -r item; do
        [ -e "$BRORAY_BASE/$item" ] || continue
        [ ! -L "$BRORAY_BASE/$item" ] || exit 1
        case "$item" in
            */*) mkdir -p "$stage/${item%/*}" || exit 1 ;;
        esac
        cp -pR "$BRORAY_BASE/$item" "$stage/$item" || exit 1
        printf '%s\n' "$item"
    done | LC_ALL=C sort -u >"$roots" || { broray_system_protected_backup_owned_cleanup; return 1; }
    [ -s "$roots" ] || { broray_system_protected_backup_owned_cleanup; return 1; }

    : >"$paths" || { broray_system_protected_backup_owned_cleanup; return 1; }
    while IFS= read -r item; do
        find -P "$stage/$item" -xdev -print >>"$paths" 2>/dev/null || {
            broray_system_protected_backup_owned_cleanup
            return 1
        }
    done <"$roots"
    LC_ALL=C sort -u "$paths" >"$paths.sorted" || { broray_system_protected_backup_owned_cleanup; return 1; }
    mv -f "$paths.sorted" "$paths" || { broray_system_protected_backup_owned_cleanup; return 1; }
    : >"$manifest" || { broray_system_protected_backup_owned_cleanup; return 1; }
    while IFS= read -r absolute; do
        relative="${absolute#"$stage/"}"
        case "$relative" in ''|/*|*../*|../*|*/..) broray_system_protected_backup_owned_cleanup; return 1 ;; esac
        [ ! -L "$absolute" ] || { broray_system_protected_backup_owned_cleanup; return 1; }
        meta="$(find -P "$absolute" -maxdepth 0 -printf '%m|%U|%G' 2>/dev/null)" || {
            broray_system_protected_backup_owned_cleanup
            return 1
        }
        mode="${meta%%|*}"; meta="${meta#*|}"; uid="${meta%%|*}"; gid="${meta#*|}"
        if [ -f "$absolute" ]; then
            bytes="$(wc -c <"$absolute" | tr -d ' ')"
            sha="$(sha256sum "$absolute" | awk 'NR==1{print $1;exit}')"
            printf 'F|%s|%s|%s|%s|%s|%s\n' "$relative" "$bytes" "$sha" "$mode" "$uid" "$gid" >>"$manifest" || {
                broray_system_protected_backup_owned_cleanup; return 1;
            }
        elif [ -d "$absolute" ]; then
            printf 'D|%s|-|-|%s|%s|%s\n' "$relative" "$mode" "$uid" "$gid" >>"$manifest" || {
                broray_system_protected_backup_owned_cleanup; return 1;
            }
        else
            broray_system_protected_backup_owned_cleanup
            return 1
        fi
    done <"$paths"
    rm -f "$paths"
    printf '%s\n' 'BROray protected backup/2' >"$stage/.broray-protected-format" || {
        broray_system_protected_backup_owned_cleanup; return 1;
    }
    (set -C; : >"$part") || { broray_system_protected_backup_owned_cleanup; return 1; }
    preserved_part_path="$part"
    preserved_part_owned=true
    tar -czf "$part" -C "$stage" . || { broray_system_protected_backup_owned_cleanup; return 1; }
    rm -rf "$stage" || { broray_system_protected_backup_owned_cleanup; return 1; }
    preserved_stage_owned=false
    gzip -t "$part" && broray_system_user_backup_valid "$part" || {
        broray_system_protected_backup_owned_cleanup
        return 1
    }
    ln "$part" "$destination" || { broray_system_protected_backup_owned_cleanup; return 1; }
    preserved_archive_owned=true
    chmod 600 "$destination" || { broray_system_protected_backup_owned_cleanup; return 1; }
    rm -f "$part" || return 1
    preserved_part_owned=false
    return 0
}

broray_system_updater_ready() {
    [ -x "$BRORAY_UPDATER_CTL" ] || return 1
    updater_identity="$("$BRORAY_UPDATER_CTL" version 2>/dev/null)" || return 1
    [ "$updater_identity" = 'broray-updater/5' ]
}

broray_system_updater_cache_read() {
    [ -f "$BRORAY_UPDATER_CACHE" ] && [ ! -L "$BRORAY_UPDATER_CACHE" ] && [ -s "$BRORAY_UPDATER_CACHE" ] || return 1
    updater_cache_bytes="$(wc -c <"$BRORAY_UPDATER_CACHE" 2>/dev/null | tr -d ' ')" || return 1
    case "$updater_cache_bytes" in ''|*[!0-9]*) return 1 ;; esac
    [ "$updater_cache_bytes" -le "$BRORAY_UPDATER_CACHE_MAX_BYTES" ] || return 1

    jq -e '
      type == "object" and
      .schemaVersion == 1 and
      .lifecycleContract == "compact-app-rename/1" and
      ((.stable | type) == "boolean") and
      ((.minimumUpdaterVersion | type) == "number") and
      (.minimumUpdaterVersion <= 5) and
      ((.checkedAt | type) == "string") and ((.checkedAt | length) > 0) and
      ((.checkedEpoch | type) == "number") and ((.checkedEpoch | floor) == .checkedEpoch) and (.checkedEpoch > 0) and
      ((.updateAvailable | type) == "boolean") and
      (.releaseRelation == "newer" or .releaseRelation == "same" or
       .releaseRelation == "older" or .releaseRelation == "uncomparable") and
      (.candidateRelation == "newer" or .candidateRelation == "same" or
       .candidateRelation == "older" or .candidateRelation == "uncomparable") and
      ((.currentCandidateAtCheck == null) or ((.currentCandidateAtCheck|type)=="string")) and
      ((.currentReleaseAtCheck == null) or ((.currentReleaseAtCheck|type)=="string")) and
      ((.candidate | type) == "object") and
      ((.candidate.candidateId | type) == "string") and ((.candidate.candidateId | length) > 0) and
      ((.candidate.releaseId | type) == "string") and ((.candidate.releaseId | length) > 0) and
      ((.candidate.appVersion | type) == "string") and ((.candidate.appVersion | length) > 0) and
      ((.candidate.packageVersion | type) == "string") and ((.candidate.packageVersion | length) > 0) and
      ((.candidate.architecture | type) == "string") and ((.candidate.architecture | length) > 0) and
      ((.candidate.bundle | type) == "object") and
      ((.candidate.bundle.filename | type) == "string") and ((.candidate.bundle.filename | length) > 0) and
      ((.candidate.bundle.sha256 | type) == "string") and
      ((.candidate.bundle.sha256 | length) == 64) and
      (.candidate.bundle.sha256 | explode | all(.[];
        ((. >= 48) and (. <= 57)) or ((. >= 97) and (. <= 102))
      )) and
      ((.candidate.bundle.sizeBytes | type) == "number") and ((.candidate.bundle.sizeBytes | floor) == .candidate.bundle.sizeBytes) and (.candidate.bundle.sizeBytes > 0) and
      ((.candidate.bundle.url | type) == "string") and (.candidate.bundle.url | startswith("https://"))
    ' "$BRORAY_UPDATER_CACHE" >/dev/null 2>&1 || return 1

    updater_cache_candidate="$(jq -r '.candidate.candidateId' "$BRORAY_UPDATER_CACHE")" || return 1
    case "$updater_cache_candidate" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 1 ;; esac
    cat "$BRORAY_UPDATER_CACHE"
}

broray_system_info_json() {
    broray_system_require_runtime || {
        broray_system_error_json RUNTIME_ERROR 'Не удалось подготовить служебный каталог.'
        return 1
    }

    current_version="$(broray_system_version)"
    installed_package_version="$(broray_system_installed_package_version 2>/dev/null || true)"
    installed_package_revision="$(broray_system_installed_package_revision 2>/dev/null || true)"
    installed_release_id="$(broray_system_installed_release_id 2>/dev/null || true)"
    installed_webui_build="$(broray_system_installed_webui_build 2>/dev/null || true)"
    installed_control_app="$(broray_system_installed_app_version 2>/dev/null || true)"
    opkg_registration_healthy=false
    command -v broray_opkg_registration_valid >/dev/null 2>&1 &&
        broray_opkg_registration_valid && opkg_registration_healthy=true
    reinstall_supported=false

    release_valid=false
    release_json='{}'
    if command -v broray_release_manifest_valid >/dev/null 2>&1 &&
       command -v broray_release_json >/dev/null 2>&1 &&
       broray_release_manifest_valid
    then
        if release_json="$(broray_release_json 2>/dev/null)" &&
           printf '%s\n' "$release_json" | jq -e 'type == "object"' >/dev/null 2>&1
        then
            release_valid=true
        else
            release_json='{}'
        fi
    fi

    build="$(broray_system_build)"
    architecture="$(broray_system_architecture)"
    channel="$(broray_system_update_channel)"
    package_track="$(printf '%s\n' "$release_json" | jq -r '.buildTrack // "unknown"')"
    webui_build="$(printf '%s\n' "$release_json" | jq -r '.webUIBuild // "unknown"')"
    release_id="$(printf '%s\n' "$release_json" | jq -r '.releaseId // "unknown"')"
    candidate_id="$(printf '%s\n' "$release_json" | jq -r '.candidateId // "unknown"')"
    release_source="$(printf '%s\n' "$release_json" | jq -r '.source // "unknown"')"
    release_commit="$(printf '%s\n' "$release_json" | jq -r '.commit // empty')"
    release_built_at="$(printf '%s\n' "$release_json" | jq -r '.builtAt // empty')"

    if [ "$release_valid" = true ] &&
       [ "$candidate_id" != unknown ] && [ -n "$candidate_id" ] &&
       [ "$opkg_registration_healthy" = true ] &&
       [ -n "$installed_package_version" ] &&
       broray_system_updater_ready
    then
        reinstall_supported=true
    fi

    available_version=""
    available_package_version=""
    available_candidate_id=""
    candidate_relation="not-checked"
    release_relation="not-checked"
    update_available=false
    update_check_fresh=false
    last_checked_at=""
    last_backup=""
    backup_valid=false
    backup_problem=""

    if valid_update_cache="$(broray_system_updater_cache_read 2>/dev/null)"; then
        last_checked_at="$(printf '%s\n' "$valid_update_cache" | jq -r '.checkedAt // empty')"
        checked_epoch="$(printf '%s\n' "$valid_update_cache" | jq -r '.checkedEpoch // 0')"
        available_version="$(printf '%s\n' "$valid_update_cache" | jq -r '.candidate.releaseId // empty')"
        available_package_version="$(printf '%s\n' "$valid_update_cache" | jq -r '.candidate.packageVersion // empty')"
        available_candidate_id="$(printf '%s\n' "$valid_update_cache" | jq -r '.candidate.candidateId // empty')"
        cached_current_candidate="$(printf '%s\n' "$valid_update_cache" | jq -r '.currentCandidateAtCheck // empty')"
        cached_current_release="$(printf '%s\n' "$valid_update_cache" | jq -r '.currentReleaseAtCheck // empty')"
        release_relation="$(printf '%s\n' "$valid_update_cache" | jq -r '.releaseRelation // "uncomparable"')"
        candidate_relation="$(printf '%s\n' "$valid_update_cache" | jq -r '.candidateRelation // "uncomparable"')"
        cached_update_available="$(printf '%s\n' "$valid_update_cache" | jq -r '.updateAvailable // false')"
        now_epoch="$(date '+%s' 2>/dev/null || printf '0')"
        case "$checked_epoch:$now_epoch" in
            *[!0-9:]*|:*|*:) ;;
            *)
                if [ "$now_epoch" -ge "$checked_epoch" ] &&
                   [ $((now_epoch - checked_epoch)) -le 3600 ]
                then
                    update_check_fresh=true
                fi
                ;;
        esac
        if [ "$release_valid" = true ] &&
           [ "$update_check_fresh" = true ] &&
           [ "$cached_current_candidate" = "$candidate_id" ] &&
           [ "$cached_current_release" = "$release_id" ] &&
           [ "$release_relation" = newer ] &&
           [ "$cached_update_available" = true ]
        then
            update_available=true
        fi
    fi

    last_backup="$(broray_system_last_backup_read)"
    if [ -n "$last_backup" ]; then
        if broray_system_user_backup_valid "$last_backup"; then
            backup_valid=true
        else
            backup_problem='Последняя резервная копия отсутствует, является символической ссылкой или повреждена.'
        fi
    fi

    components="$({
        broray_system_component_json core 'Ядро BROray' "$BRORAY_BASE/bin/broray" true "$current_version"
        broray_system_component_json xray 'Xray' "/opt/broray/runtime/xray" true "$("/opt/broray/runtime/xray" version 2>/dev/null | awk 'NR == 1 {print $2}')"
        broray_system_component_json servers 'Серверы' "$BRORAY_BASE/lib/server-service.sh" true "$current_version"
        if [ -e "$BRORAY_BASE/lib/subscription-service.sh" ]; then
            broray_system_component_json subscriptions 'Подписки' "$BRORAY_BASE/lib/subscription-service.sh" true "$current_version"
        else
            broray_system_component_json subscriptions 'Подписки' "$BRORAY_BASE/lib/subscription.sh" true "$current_version"
        fi
        broray_system_component_json keenetic 'Keenetic' "$BRORAY_BASE/lib/keenetic-page.sh" true "$current_version"
        broray_system_component_json routes 'Маршруты' "$BRORAY_BASE/bin/broray-routes" true "$current_version"
        broray_system_component_json webui 'WebUI' "$BRORAY_BASE/web-new/home.html" true "$webui_build"
        broray_system_component_json updater 'Updater' "/opt/libexec/broray-updater/broray-updater.sh" true "$(/opt/libexec/broray-updater/broray-updater.sh version 2>/dev/null || true)"
        broray_system_component_json opkg 'OPKG' "/opt/lib/opkg/info/broray.control" true "$installed_package_version"
    } | jq -sc '.')"

    protocols="$({
        for protocol in vless vmess trojan hysteria2 shadowsocks; do
            supported=false
            broray_system_parser_available "$protocol" && supported=true
            jq -nc --arg id "$protocol" --argjson supported "$supported" '{id:$id,supported:$supported}'
        done
    } | jq -sc '.')"

    installation_healthy="$(printf '%s' "$components" | jq 'all(.[]; (.required | not) or (.healthy == true))')"
    [ "$opkg_registration_healthy" = true ] || installation_healthy=false
    versions_consistent=false
    case "$installed_package_revision" in ''|*[!0-9]*) installed_package_revision="" ;; esac
    if [ "$release_valid" = true ] &&
       [ -n "$installed_package_version" ] &&
       [ -n "$installed_package_revision" ] &&
       [ -n "$installed_release_id" ] &&
       [ -n "$installed_webui_build" ] &&
       [ "$installed_control_app" = "$current_version" ] &&
       [ "$current_version" = "$(printf '%s\n' "$release_json" | jq -r '.version')" ] &&
       [ "$installed_release_id" = "$(printf '%s\n' "$release_json" | jq -r '.releaseId')" ] &&
       [ "$installed_webui_build" = "$(printf '%s\n' "$release_json" | jq -r '.webUIBuild')" ]
    then
        versions_consistent=true
    fi

    broray_system_select_last_operation >/dev/null 2>&1 || true
    last_operation='null'
    if [ -s "$BRORAY_STATUS" ] && jq -e 'type == "object"' "$BRORAY_STATUS" >/dev/null 2>&1; then
        last_operation="$(jq -c '.' "$BRORAY_STATUS")"
    fi
    operation_running="$(printf '%s\n' "$last_operation" | jq -r 'if . == null then false else (.running // false) end')"
    operation_state="$(printf '%s\n' "$last_operation" | jq -r 'if . == null then "idle" else (.state // "unknown") end')"

    health_reasons="$({
        [ "$release_valid" = true ] || broray_status_reason RELEASE_MANIFEST_INVALID 'Манифест установленного релиза отсутствует или некорректен.'
        [ "$opkg_registration_healthy" = true ] || broray_status_reason OPKG_REGISTRATION_INVALID 'Регистрация пакета BROray в OPKG отсутствует или повреждена.'
        [ "$installation_healthy" = true ] || broray_status_reason INSTALLATION_INCOMPLETE 'Один или несколько обязательных компонентов BROray отсутствуют.'
        [ "$versions_consistent" = true ] || broray_status_reason VERSION_MISMATCH 'Версии приложения и манифеста релиза не совпадают либо регистрация OPKG неполна.'
        [ -z "$backup_problem" ] || broray_status_reason BACKUP_INVALID "$backup_problem"
        [ "$update_available" != true ] || broray_status_reason UPDATE_AVAILABLE 'Доступна более новая версия BROray.'
        [ "$operation_state" != error ] || broray_status_reason LAST_OPERATION_FAILED 'Последняя операция завершилась ошибкой; установка продолжает оцениваться отдельно.'
    } | jq -sc '.')"

    if [ "$installation_healthy" != true ] || [ "$versions_consistent" != true ]; then
        health_severity=error
    elif [ "$operation_running" = true ]; then
        health_severity=busy
    elif [ -n "$backup_problem" ] || [ "$update_available" = true ] || [ "$operation_state" = error ]; then
        health_severity=warning
    else
        health_severity=ok
    fi

    health_action_required=false
    [ "$health_severity" = ok ] || [ "$health_severity" = busy ] || health_action_required=true
    health_facts="$(jq -nc \
        --arg version "$current_version" \
        --arg installedPackageVersion "$installed_package_version" \
        --arg releaseId "$release_id" \
        --arg candidateId "$candidate_id" \
        --arg webUIBuild "$webui_build" \
        --arg channel "$channel" \
        --arg packageTrack "$package_track" \
        --argjson releaseManifestValid "$release_valid" \
        --argjson opkgRegistrationHealthy "$opkg_registration_healthy" \
        --argjson installationHealthy "$installation_healthy" \
        --argjson versionsConsistent "$versions_consistent" \
        --argjson backupValid "$backup_valid" \
        --argjson updateCheckFresh "$update_check_fresh" \
        '{version:$version,installedPackageVersion:(if $installedPackageVersion=="" then null else $installedPackageVersion end),releaseId:$releaseId,candidateId:$candidateId,webUIBuild:$webUIBuild,updateChannel:$channel,packageTrack:$packageTrack,releaseManifestValid:$releaseManifestValid,opkgRegistrationHealthy:$opkgRegistrationHealthy,installationHealthy:$installationHealthy,versionsConsistent:$versionsConsistent,backupValid:$backupValid,updateCheckFresh:$updateCheckFresh}')"
    health_json="$(broray_status_contract broray available "$health_severity" "$installation_healthy" "$versions_consistent" "$health_action_required" fresh "$(broray_system_now)" "$health_reasons" "$health_facts" "$last_operation")"

    jq -nc \
        --arg version "$current_version" \
        --arg installedPackageVersion "$installed_package_version" \
        --arg installedReleaseId "$installed_release_id" \
        --arg installedWebUIBuild "$installed_webui_build" \
        --argjson installedPackageRevision "${installed_package_revision:-null}" \
        --arg build "$build" \
        --arg releaseId "$release_id" \
        --arg candidateId "$candidate_id" \
        --arg releaseSource "$release_source" \
        --arg releaseCommit "$release_commit" \
        --arg releaseBuiltAt "$release_built_at" \
        --arg webUIBuild "$webui_build" \
        --arg architecture "$architecture" \
        --arg channel "$channel" \
        --arg packageTrack "$package_track" \
        --arg availableVersion "$available_version" \
        --arg availablePackageVersion "$available_package_version" \
        --arg availableCandidateId "$available_candidate_id" \
        --arg candidateRelation "$candidate_relation" \
        --arg releaseRelation "$release_relation" \
        --arg lastCheckedAt "$last_checked_at" \
        --arg lastBackup "$last_backup" \
        --arg backupProblem "$backup_problem" \
        --arg projectUrl "$BRORAY_PROJECT_URL" \
        --arg githubUrl "$BRORAY_GITHUB_URL" \
        --arg donateUrl "$BRORAY_DONATE_URL" \
        --arg updatedAt "$(broray_system_now)" \
        --argjson updateAvailable "$update_available" \
        --argjson updateCheckFresh "$update_check_fresh" \
        --argjson reinstallSupported "$reinstall_supported" \
        --argjson opkgRegistrationHealthy "$opkg_registration_healthy" \
        --argjson installationHealthy "$installation_healthy" \
        --argjson versionsConsistent "$versions_consistent" \
        --argjson backupValid "$backup_valid" \
        --argjson components "$components" \
        --argjson protocols "$protocols" \
        --argjson health "$health_json" \
        --argjson lastOperation "$last_operation" '
        {
            ok:true,
            version:$version,
            installedPackageVersion:(if $installedPackageVersion == "" then null else $installedPackageVersion end),
            installedPackageRevision:$installedPackageRevision,
            installedReleaseId:(if $installedReleaseId == "" then null else $installedReleaseId end),
            installedWebUIBuild:(if $installedWebUIBuild == "" then null else $installedWebUIBuild end),
            reinstallSupported:$reinstallSupported,
            opkgRegistrationHealthy:$opkgRegistrationHealthy,
            build:$build,
            releaseId:$releaseId,
            candidateId:$candidateId,
            releaseSource:$releaseSource,
            releaseCommit:(if $releaseCommit == "" then null else $releaseCommit end),
            releaseBuiltAt:(if $releaseBuiltAt == "" then null else $releaseBuiltAt end),
            webUIBuild:$webUIBuild,
            architecture:$architecture,
            updateChannel:$channel,
            packageTrack:$packageTrack,
            updateAvailable:$updateAvailable,
            updateCheckFresh:$updateCheckFresh,
            availableVersion:(if $availableVersion == "" then null else $availableVersion end),
            availablePackageVersion:(if $availablePackageVersion == "" then null else $availablePackageVersion end),
            availableCandidateId:(if $availableCandidateId == "" then null else $availableCandidateId end),
            releaseRelation:$releaseRelation,
            candidateRelation:$candidateRelation,
            lastCheckedAt:(if $lastCheckedAt == "" then null else $lastCheckedAt end),
            lastBackup:(if $lastBackup == "" then null else $lastBackup end),
            backupValid:$backupValid,
            backupProblem:(if $backupProblem == "" then null else $backupProblem end),
            installationHealthy:$installationHealthy,
            versionsConsistent:$versionsConsistent,
            components:$components,
            protocols:$protocols,
            health:$health,
            lastOperation:$lastOperation,
            capabilities:[
                "Управление Xray",
                "Импорт подписок",
                "Выбор активного сервера",
                "Интеграция с управляемым ProxyN",
                "Маршрутизация сервисов",
                "Проверка и восстановление компонентов",
                "Обновление через WebUI",
                "Восстановительная переустановка текущей версии",
                "Безопасная очистка резервных копий и временных файлов"
            ],
            links:{github:$githubUrl,project:$projectUrl,donate:$donateUrl},
            updatedAt:$updatedAt
        }'
}

broray_system_feed_value() {
    wanted_version="$1"
    wanted_key="$2"
    feed_temp="$BRORAY_TMP_ROOT/broray-feed-value-$$"
    feed_seen=""

    rm -f "$feed_temp"
    for feed in \
        "$BRORAY_OPKG_LISTS_DIR/broray" \
        "$BRORAY_OPKG_LISTS_DIR"/*
    do
        [ -f "$feed" ] || continue
        [ "$feed" = "$feed_seen" ] && continue
        feed_seen="$feed"

        rm -f "$feed_temp"
        if command -v gzip >/dev/null 2>&1 &&
           gzip -t "$feed" >/dev/null 2>&1
        then
            gzip -dc "$feed" >"$feed_temp" 2>/dev/null || {
                rm -f "$feed_temp"
                continue
            }
        else
            cat "$feed" >"$feed_temp" 2>/dev/null || {
                rm -f "$feed_temp"
                continue
            }
        fi

        feed_value="$(
            awk \
                -v wanted_package="$BRORAY_PACKAGE" \
                -v wanted_version="$wanted_version" \
                -v wanted_key="$wanted_key" '
                function reset_record() {
                    package_name = ""
                    package_version = ""
                    value = ""
                }
                function emit_record() {
                    if (package_name == wanted_package &&
                        package_version == wanted_version &&
                        value != "") {
                        print value
                        found = 1
                    }
                }
                BEGIN { reset_record(); found = 0 }
                $1 == "Package:" {
                    if (package_name != "") {
                        emit_record()
                        if (found == 1) exit
                    }
                    reset_record(); package_name = $2; next
                }
                $1 == "Version:" { package_version = $2; next }
                index($0, wanted_key ":") == 1 {
                    value = $0
                    sub(wanted_key ":[ \\t]*", "", value)
                    next
                }
                END { if (found == 0) emit_record() }
            ' "$feed_temp" | sed -n '1p'
        )"
        rm -f "$feed_temp"
        if [ -n "$feed_value" ]; then
            printf '%s\n' "$feed_value"
            return 0
        fi
    done

    rm -f "$feed_temp"
    return 1
}

broray_system_installed_package_version() {
    opkg list-installed "$BRORAY_PACKAGE" 2>/dev/null |
        awk -F ' - ' -v package="$BRORAY_PACKAGE" '
            $1 == package {
                print $2
                exit
            }
        '
}

broray_system_installed_package_architecture() {
    opkg status "$BRORAY_PACKAGE" 2>/dev/null |
        awk '
            $1 == "Architecture:" {
                print $2
                exit
            }
        '
}

broray_system_configured_target_architecture() {
    configured_arch_output="$(opkg print-architecture 2>/dev/null)" || return 1
    printf '%s\n' "$configured_arch_output" | awk '
      {
        if (NF != 3 || $1 != "arch" || $2 !~ /^[0-9A-Za-z._+-]+$/ ||
            $3 !~ /^(0|[1-9][0-9]*)$/ || seen[$2]++) bad=1
        if ($2 == "aarch64-3.10") {wanted++; value=$2}
      }
      END {
        if (bad || wanted != 1) exit 1
        print value
      }
    '
}

broray_system_installed_candidate_sha() {
    if [ ! -e "$BRORAY_INSTALLED_CANDIDATE_SHA" ] && [ ! -L "$BRORAY_INSTALLED_CANDIDATE_SHA" ]; then
        return 0
    fi
    [ -f "$BRORAY_INSTALLED_CANDIDATE_SHA" ] && [ ! -L "$BRORAY_INSTALLED_CANDIDATE_SHA" ] || return 1
    identity_candidate_sha="$(sed -n '1p' "$BRORAY_INSTALLED_CANDIDATE_SHA")"
    [ "$(wc -l <"$BRORAY_INSTALLED_CANDIDATE_SHA" | tr -d ' ')" -eq 1 ] || return 1
    case "$identity_candidate_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#identity_candidate_sha}" -eq 64 ] || return 1
    printf '%s\n' "$identity_candidate_sha"
}

broray_system_installed_control_value() {
    installed_control_key="$1"
    [ -f "$BRORAY_INSTALLED_CONTROL" ] && [ ! -L "$BRORAY_INSTALLED_CONTROL" ] || return 1
    awk -F ': *' -v key="$installed_control_key" '$1==key{print $2;exit}' "$BRORAY_INSTALLED_CONTROL"
}

broray_system_installed_package_revision() {
    broray_system_installed_control_value X-BROray-Package-Revision
}

broray_system_installed_release_id() {
    broray_system_installed_control_value X-BROray-Release-ID
}

broray_system_installed_webui_build() {
    broray_system_installed_control_value X-BROray-WebUI-Version
}

broray_system_installed_app_version() {
    broray_system_installed_control_value X-BROray-Version
}

broray_system_current_identity_json() {
    identity_package="$(broray_system_installed_package_version 2>/dev/null || true)"
    identity_app="$(broray_system_version 2>/dev/null || true)"
    identity_arch="$(broray_system_installed_package_architecture 2>/dev/null || true)"
    identity_configured_arch="$(broray_system_configured_target_architecture)" || return 1
    identity_candidate_sha="$(broray_system_installed_candidate_sha)" || return 1
    identity_revision="$(broray_system_installed_package_revision 2>/dev/null || true)"
    identity_release="$(broray_system_installed_release_id 2>/dev/null || true)"
    identity_webui="$(broray_system_installed_webui_build 2>/dev/null || true)"
    [ -n "$identity_arch" ] || identity_arch=unknown
    [ -d "$BRORAY_BASE" ] && [ ! -L "$BRORAY_BASE" ] || return 1
    [ -n "$identity_package" ] && identity_registered=true || identity_registered=false
    [ -n "$identity_package" ] || identity_package=unregistered
    [ -n "$identity_app" ] || identity_app=unnumbered
    case "$identity_revision" in ''|*[!0-9]*) identity_revision=0 ;; esac
    [ -n "$identity_release" ] || identity_release=unknown
    [ -n "$identity_webui" ] || identity_webui=unknown

    jq -nc \
      --arg packageVersion "$identity_package" --arg appVersion "$identity_app" \
      --arg architecture "$identity_arch" --arg configuredArchitecture "$identity_configured_arch" \
      --arg candidateSha256 "$identity_candidate_sha" --arg releaseId "$identity_release" \
      --arg webUIBuild "$identity_webui" --argjson packageRevision "$identity_revision" \
      --argjson opkgRegistered "$identity_registered" '
      {packageVersion:$packageVersion,appVersion:$appVersion,architecture:$architecture,
       architectureDiagnosticOnly:true,configuredArchitecture:$configuredArchitecture,
       candidateSha256:(if $candidateSha256=="" then null else $candidateSha256 end),
       packageRevision:$packageRevision,releaseId:$releaseId,webUIBuild:$webUIBuild,
       opkgRegistered:$opkgRegistered,versionIsDiagnosticOnly:true}
    '
}

broray_system_update_cache_read() {
    [ -s "$BRORAY_UPDATE_CACHE" ] && [ ! -L "$BRORAY_UPDATE_CACHE" ] || return 1
    current_identity="$(broray_system_current_identity_json)" || return 1
    jq -e --argjson current "$current_identity" '
      (type == "object") and (.schemaVersion == 6) and (.requirementsContract == "1.7.2") and
      (.capabilityContract == "keenetic-entware-capabilities/1") and (.spaceContract == "broray-space/2") and
      (.lifecycleContract == "current-operation-full-tmp-snapshot/1") and
      (.distribution == "release-json") and (.ok == true) and
      (.previousIpkRequired == false) and (.historicalTransactionStateRequired == false) and
      (.statelessBootstrap == true) and
      ((.updateAvailable | type) == "boolean") and ((.checkedEpoch | type) == "number") and
      (.source == $current) and ((.candidate | type) == "object") and ((.opkgEntry | type) == "object") and
      ((.candidate.packageVersion | type) == "string") and ((.candidate.architecture | type) == "string") and
      ((.candidate.releaseId | type) == "string") and ((.candidate.packageRevision | type) == "number") and
      (.candidate.architecture == $current.configuredArchitecture) and
      (.candidate.requirementsContract == .requirementsContract) and
      (.candidate.lifecycleContract == .lifecycleContract) and
      (.candidate.capabilityContract == .capabilityContract) and (.candidate.spaceContract == .spaceContract) and
      (.candidate.previousIpkRequired == false) and
      (.candidate.historicalTransactionStateRequired == false) and (.candidate.statelessBootstrap == true) and
      (.opkgEntry.filename == .candidate.filename) and (.opkgEntry.sha256 == .candidate.sha256) and
      (.opkgEntry.sizeBytes == .candidate.sizeBytes) and
      ((.opkgEntry | del(.distributionRole,.metadataOnlyRegistration,.directOpkgMutation)) ==
       (.candidate | del(.distributionRole))) and
      (.opkgEntry.distributionRole == "canonical-full-candidate") and
      (.opkgEntry.metadataOnlyRegistration == true) and (.opkgEntry.directOpkgMutation == "fail-closed")
    ' "$BRORAY_UPDATE_CACHE" >/dev/null 2>&1 || return 1
    cache_update="$(jq -r '.updateAvailable' "$BRORAY_UPDATE_CACHE")"
    same="$(jq -r '(.source.candidateSha256 != null and .source.candidateSha256 == .candidate.sha256)' "$BRORAY_UPDATE_CACHE")"
    case "$cache_update:$same" in true:false|false:true) ;; *) return 1;; esac
    cat "$BRORAY_UPDATE_CACHE"
}

broray_system_feed_base_url() {
    [ -r "$BRORAY_FEED_FILE" ] || return 1

    awk \
        -v package="$BRORAY_PACKAGE" '
            $1 == "src/gz" && $2 == package {
                print $3
                exit
            }
        ' "$BRORAY_FEED_FILE"
}


broray_system_download_feed_file() {
    broray_download_base="$1"
    broray_download_filename="$2"
    broray_download_expected_sha="$3"
    broray_download_target="$4"
    broray_download_error_file="$5"
    broray_download_part="$broray_download_target.part"
    broray_download_nonce="$(date '+%s')-$$"
    broray_download_url="${broray_download_base%/}/$broray_download_filename?broray=$broray_download_nonce"

    : >"$broray_download_error_file" 2>/dev/null || return 1

    case "$broray_download_base" in
        https://*) ;;
        *)
            printf '%s\n' 'Источник пакета должен использовать HTTPS.' >"$broray_download_error_file"
            broray_system_log "Источник пакета отклонён: $broray_download_base"
            return 1
            ;;
    esac
    case "$broray_download_filename" in
        ''|/*|*'..'*|*[!0-9A-Za-z._+/-]*)
            printf '%s\n' 'Индекс содержит небезопасное имя файла.' >"$broray_download_error_file"
            return 1
            ;;
    esac
    case "$broray_download_expected_sha" in
        ''|*[!0-9a-fA-F]*)
            printf '%s\n' 'Индекс не содержит корректную SHA-256.' >"$broray_download_error_file"
            return 1
            ;;
    esac
    [ "${#broray_download_expected_sha}" -eq 64 ] || {
        printf '%s\n' 'Индекс содержит SHA-256 неверной длины.' >"$broray_download_error_file"
        return 1
    }

    rm -f "$broray_download_part" "$broray_download_target"
    broray_system_log "URL загрузки: $broray_download_url"

    curl \
        -fL \
        --retry 3 \
        --retry-delay 1 \
        --connect-timeout 15 \
        --max-time 300 \
        -H 'Accept: application/octet-stream' \
        -H 'Accept-Encoding: identity' \
        -H 'Cache-Control: no-cache, no-store, max-age=0' \
        -H 'Pragma: no-cache' \
        -o "$broray_download_part" \
        "$broray_download_url" \
        >>"$BRORAY_LOG" 2>&1 || {
            printf '%s\n' "Не удалось загрузить $broray_download_filename." >"$broray_download_error_file"
            rm -f "$broray_download_part"
            return 1
        }

    broray_download_actual_sha="$(sha256sum "$broray_download_part" 2>/dev/null | awk '{print $1}')"
    broray_download_size="$(wc -c <"$broray_download_part" 2>/dev/null | tr -d ' ')"
    broray_download_expected_lower="$(printf '%s' "$broray_download_expected_sha" | tr 'A-F' 'a-f')"
    broray_download_actual_lower="$(printf '%s' "$broray_download_actual_sha" | tr 'A-F' 'a-f')"

    if [ "$broray_download_actual_lower" != "$broray_download_expected_lower" ]; then
        {
            printf 'Ожидаемая SHA-256: %s\n' "$broray_download_expected_lower"
            printf 'Полученная SHA-256: %s\n' "$broray_download_actual_lower"
            printf 'Имя файла: %s\n' "$broray_download_filename"
            printf 'Размер файла: %s байт\n' "${broray_download_size:-0}"
            printf 'URL источника: %s\n' "$broray_download_url"
        } >"$broray_download_error_file"
        cat "$broray_download_error_file" >>"$BRORAY_LOG" 2>/dev/null || true
        rm -f "$broray_download_part"
        return 1
    fi

    mv -f "$broray_download_part" "$broray_download_target" || return 1
    return 0
}

broray_system_ipk_control_value() {
    broray_ipk_file="$1"
    broray_ipk_key="$2"
    broray_ipk_work="$BRORAY_SCRATCH_ROOT/broray-ipk-control-$$"
    broray_ipk_control_tar="$broray_ipk_work/control.tar.gz"
    broray_ipk_control="$broray_ipk_work/control"
    broray_ipk_value=""

    rm -rf "$broray_ipk_work"
    mkdir -p "$broray_ipk_work" || return 1

    if ! tar -xzOf "$broray_ipk_file" ./control.tar.gz \
        >"$broray_ipk_control_tar" 2>/dev/null
    then
        tar -xzOf "$broray_ipk_file" control.tar.gz \
            >"$broray_ipk_control_tar" 2>/dev/null || {
                rm -rf "$broray_ipk_work"
                return 1
            }
    fi

    if ! tar -xzOf "$broray_ipk_control_tar" ./control \
        >"$broray_ipk_control" 2>/dev/null
    then
        tar -xzOf "$broray_ipk_control_tar" control \
            >"$broray_ipk_control" 2>/dev/null || {
                rm -rf "$broray_ipk_work"
                return 1
            }
    fi

    broray_ipk_value="$(
        awk \
            -v wanted="$broray_ipk_key" '
                index($0, wanted ":") == 1 {
                    value = $0
                    sub(wanted ":[ \t]*", "", value)
                    print value
                    exit
                }
            ' "$broray_ipk_control"
    )"

    rm -rf "$broray_ipk_work"
    [ -n "$broray_ipk_value" ] || return 1
    printf '%s\n' "$broray_ipk_value"
}









broray_system_update_check() {
    broray_system_require_runtime || {
        broray_system_error_json RUNTIME_ERROR 'Не удалось подготовить служебный каталог.'
        return 1
    }

    previous_log="$BRORAY_LOG"
    BRORAY_LOG="$BRORAY_UPDATE_CHECK_LOG"
    : >"$BRORAY_LOG"
    if ! broray_system_update_check_internal; then
        BRORAY_LOG="$previous_log"
        broray_system_error_json UPDATE_CHECK_FAILED 'Не удалось проверить состав релиза.'
        return 1
    fi
    BRORAY_LOG="$previous_log"
    cat "$BRORAY_UPDATE_CACHE"
}

# Legacy update/reinstall transaction code is deliberately absent.

broray_system_archive_safe() {
    local archive list verbose count unique raw_count root_count verbose_count result
    archive="$1"
    list="$BRORAY_SCRATCH_ROOT/broray-archive-list-$$"
    verbose="$BRORAY_SCRATCH_ROOT/broray-archive-verbose-$$"
    [ -f "$archive" ] && [ ! -L "$archive" ] && [ -s "$archive" ] || return 1
    rm -f "$list" "$list.raw" "$verbose"
    tar -tzf "$archive" >"$list.raw" 2>/dev/null || { rm -f "$list" "$list.raw" "$verbose"; return 1; }
    tar -tvzf "$archive" >"$verbose" 2>/dev/null || { rm -f "$list" "$list.raw" "$verbose"; return 1; }
    raw_count="$(wc -l <"$list.raw" | tr -d ' ')"
    root_count="$(awk '$0=="./"{n++} END{print n+0}' "$list.raw")"
    verbose_count="$(wc -l <"$verbose" | tr -d ' ')"
    awk '{n=$0;sub(/^\.\//,"",n);while(n~/\/$/)sub(/\/$/,"",n);if(n=="")next;
          if(substr(n,1,1)=="/"||n~/(^|\/)\.\.($|\/)/||n~/(^|\/)\.($|\/)/||index(n,"|")||index(n,"\t")){bad=1;next} print n}
         END{exit bad?1:0}' "$list.raw" >"$list" || { rm -f "$list" "$list.raw" "$verbose"; return 1; }
    awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" {bad=1} END{exit bad?1:0}' "$verbose" || { rm -f "$list" "$list.raw" "$verbose"; return 1; }
    count="$(wc -l <"$list" | tr -d ' ')"
    unique="$(LC_ALL=C sort -u "$list" | wc -l | tr -d ' ')"
    [ "$count" -gt 0 ] && [ "$root_count" -le 1 ] &&
        [ "$raw_count" -eq $((count + root_count)) ] &&
        [ "$raw_count" -eq "$verbose_count" ] && [ "$count" -eq "$unique" ]
    result=$?
    rm -f "$list" "$list.raw" "$verbose"
    return "$result"
}
broray_system_worker_restore() {
    operation_id="$1"
    archive="$(broray_system_last_backup_read)"
    [ -n "$archive" ] && broray_system_user_backup_valid "$archive" || {
        broray_system_status_write "$operation_id" restore error backup 100 \
            'Проверенная копия пользовательских данных не найдена.' \
            'Восстановление доступно только для BROray protected backup/2.'
        return 1
    }
    command -v broray_tx_restore_last_user_backup >/dev/null 2>&1 || {
        broray_system_status_write "$operation_id" restore error runtime 100 \
            'Canonical transaction engine восстановления недоступен.' \
            'Рабочая установка не изменялась.'
        return 1
    }
    broray_system_status_write "$operation_id" restore running backup 15 \
        'Создаётся и проверяется полный страховочный snapshot текущей системы.' ''
    if ! broray_tx_restore_last_user_backup "$operation_id" "$archive"; then
        broray_system_status_write "$operation_id" restore error restored 100 \
            'Восстановление не завершено; при начавшейся мутации выполнен canonical rollback.' \
            'Exact evidence сохранён в workspace операции.'
        return 1
    fi
    broray_system_status_write "$operation_id" restore success complete 100 \
        'Пользовательские данные восстановлены и дважды проверены.' ''
}

broray_system_copy_worker() {
    operation_id="$1"
    worker_base="$BRORAY_WORKER_ROOT/broray-system-$operation_id"
    worker_bin="$worker_base.sh"
    worker_lib="$worker_base.lib.sh"
    worker_lib_source="${BRORAY_SYSTEM_LIB:-$BRORAY_BASE/lib/broray-page.sh}"

    mkdir -p "$BRORAY_WORKER_ROOT" || return 1
    chmod 700 "$BRORAY_WORKER_ROOT" 2>/dev/null || true
    [ -f "$worker_lib_source" ] && [ ! -L "$worker_lib_source" ] || return 1

    cp -p "$BRORAY_BIN" "$worker_bin" || return 1
    cp -p "$worker_lib_source" "$worker_lib" || {
        rm -f "$worker_bin"
        return 1
    }
    chmod 700 "$worker_bin" "$worker_lib"
    printf '%s\n%s\n' "$worker_bin" "$worker_lib"
}

# A legacy UI status is presentation state, never an ownership primitive.  If
# it still says running while every canonical control object is absent, retire
# only that presentation state before a new request.  No lock, workspace or
# recovery object is removed here.
broray_system_legacy_status_normalize_before_start() {
    [ -e "$BRORAY_LEGACY_STATUS" ] || [ -L "$BRORAY_LEGACY_STATUS" ] || return 0
    [ -f "$BRORAY_LEGACY_STATUS" ] && [ ! -L "$BRORAY_LEGACY_STATUS" ] || return 1
    jq -e 'type=="object" and ((.running // false)|type=="boolean")' \
        "$BRORAY_LEGACY_STATUS" >/dev/null 2>&1 || return 1
    jq -e '.running==true' "$BRORAY_LEGACY_STATUS" >/dev/null 2>&1 || return 0

    for legacy_control in \
        "$BRORAY_GLOBAL_LOCK" "$BRORAY_TRANSACTION_LOCK" "$BRORAY_LOCK" \
        "$BRORAY_RUN/operation.lock" \
        "${BRORAY_TX_CURRENT:-$BRORAY_TMP_ROOT/.broray-current-operation}" \
        "${BRORAY_TX_LEGACY_MARKER:-$BRORAY_STATE_ROOT/current-operation.json}"
    do
        [ ! -e "$legacy_control" ] && [ ! -L "$legacy_control" ] || return 0
    done
    for legacy_control in \
        "$BRORAY_TMP_ROOT"/broray-update-* \
        "$BRORAY_TMP_ROOT"/.broray-control-mutex-* \
        "$BRORAY_TMP_ROOT"/.broray-rollback-failed-*
    do
        [ ! -e "$legacy_control" ] && [ ! -L "$legacy_control" ] || return 0
    done

    legacy_operation_id="$(jq -r '.operationId // "legacy-unknown"' "$BRORAY_LEGACY_STATUS")" || return 1
    legacy_operation="$(jq -r '.operation // "unknown"' "$BRORAY_LEGACY_STATUS")" || return 1
    legacy_previous_updated="$(jq -r '.updatedAt // ""' "$BRORAY_LEGACY_STATUS")" || return 1
    case "$legacy_operation_id" in ''|.*|-*|*[!0-9A-Za-z._-]*) legacy_operation_id=legacy-unknown ;; esac
    case "$legacy_operation" in ''|*[!0-9A-Za-z._-]*) legacy_operation=unknown ;; esac
    jq -nc \
        --arg operationId "$legacy_operation_id" \
        --arg operation "$legacy_operation" \
        --arg previousUpdatedAt "$legacy_previous_updated" \
        --arg updatedAt "$(broray_system_now)" '
      {ok:true,operationId:$operationId,operation:$operation,state:"error",
       stage:"stale-legacy-status",progress:100,
       message:"Старый статус running закрыт: подтверждённого владельца и canonical control-state нет.",
       error:"STALE_LEGACY_STATUS_WITHOUT_OWNER",running:false,mutationStarted:false,
       previousUpdatedAt:(if $previousUpdatedAt=="" then null else $previousUpdatedAt end),
       updatedAt:$updatedAt}
    ' | broray_system_atomic_json "$BRORAY_LEGACY_STATUS"
}

broray_system_control_rejection_status() {
    rejected_operation_id="$1"
    rejected_operation="$2"
    rejected_reason="$3"
    rejected_evidence="${4:-}"
    case "$rejected_operation_id" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 1 ;; esac
    case "$rejected_operation" in update|reinstall|restore|uninstall) ;; *) return 1 ;; esac
    case "$rejected_reason" in ''|*[!0-9A-Za-z._:-]*) return 1 ;; esac
    jq -nc \
        --arg operationId "$rejected_operation_id" \
        --arg operation "$rejected_operation" \
        --arg reason "$rejected_reason" \
        --arg evidence "$rejected_evidence" \
        --arg updatedAt "$(broray_system_now)" '
      {ok:true,operationId:$operationId,operation:$operation,state:"error",
       stage:"control-preflight",progress:0,
       message:"Запрос отклонён до запуска worker и до изменения пакета.",
       error:$reason,running:false,mutationStarted:false,
       evidence:(if $evidence=="" then null else $evidence end),updatedAt:$updatedAt}
    ' | broray_system_atomic_json "$BRORAY_LEGACY_STATUS" || return 1
    printf '%s  CONTROL_PRELUDE_REJECTED operation=%s id=%s reason=%s evidence=%s\n' \
        "$(broray_system_now)" "$rejected_operation" "$rejected_operation_id" \
        "$rejected_reason" "$rejected_evidence" >>"$BRORAY_LEGACY_LOG" 2>/dev/null || true
}

# A package-track caller must be able to distinguish a rejected request from
# a detached worker that has already adopted the canonical lock.  The worker,
# not the parent CGI, publishes this receipt after lock adoption and before the
# first operation-specific mutation.  Its caller-owned 0700 directory lives
# outside /opt/broray, so the receipt survives the normal uninstall boundary.
broray_system_worker_acceptance_publish() {
    acceptance_operation_id="$1"
    acceptance_operation="$2"
    acceptance_path="${BRORAY_WORKER_ACCEPTANCE_FILE:-}"

    [ -n "$acceptance_path" ] || return 0
    case "$acceptance_operation_id" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 1 ;; esac
    case "$acceptance_operation" in uninstall) ;; *) return 1 ;; esac
    case "$acceptance_path" in
        "$BRORAY_TMP_ROOT"/broray-package-track.*/*) ;;
        *) return 1 ;;
    esac
    acceptance_parent="${acceptance_path%/*}"
    acceptance_parent_token="${acceptance_parent#"$BRORAY_TMP_ROOT"/broray-package-track.}"
    case "$acceptance_parent_token" in ''|*/*|*[!0-9A-Za-z]*) return 1 ;; esac
    [ "$acceptance_path" = "$acceptance_parent/uninstall-acceptance.json" ] || return 1
    [ -d "$acceptance_parent" ] && [ ! -L "$acceptance_parent" ] || return 1
    [ "$(find -P "$acceptance_parent" -maxdepth 0 -type d -printf '%m|%U\n' 2>/dev/null)" = '700|0' ] || return 1
    [ ! -e "$acceptance_path" ] && [ ! -L "$acceptance_path" ] || return 1
    [ -f "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ] &&
        [ ! -L "$BRORAY_GLOBAL_LOCK/owner-identity.tsv" ] || return 1
    acceptance_owner_sha="$(broray_tx_sha "$BRORAY_GLOBAL_LOCK/owner-identity.tsv")" || return 1
    case "$acceptance_owner_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#acceptance_owner_sha}" -eq 64 ] || return 1

    acceptance_part="$acceptance_parent/.worker-acceptance-$acceptance_operation_id-$$"
    [ ! -e "$acceptance_part" ] && [ ! -L "$acceptance_part" ] || return 1
    jq -nc \
        --arg operationId "$acceptance_operation_id" \
        --arg operation "$acceptance_operation" \
        --argjson workerPid "$$" \
        --arg ownerIdentitySha256 "$acceptance_owner_sha" \
        --arg acceptedAt "$(broray_system_now)" '
      {schemaVersion:1,ok:true,accepted:true,operationId:$operationId,
       operation:$operation,workerPid:$workerPid,
       ownerIdentitySha256:$ownerIdentitySha256,acceptedAt:$acceptedAt,
       mutationStarted:false,engine:"canonical-current-operation"}
    ' >"$acceptance_part" || {
        rm -f "$acceptance_part"
        return 1
    }
    chmod 600 "$acceptance_part" || {
        rm -f "$acceptance_part"
        return 1
    }
    acceptance_bytes="$(wc -c <"$acceptance_part" | tr -d ' ')" || {
        rm -f "$acceptance_part"
        return 1
    }
    case "$acceptance_bytes" in ''|*[!0-9]*) rm -f "$acceptance_part"; return 1 ;; esac
    [ "$acceptance_bytes" -gt 0 ] && [ "$acceptance_bytes" -le 4096 ] || {
        rm -f "$acceptance_part"
        return 1
    }
    # link(2) is the no-overwrite publication primitive available in BusyBox.
    # The protected parent and same-filesystem path make this atomic.
    ln "$acceptance_part" "$acceptance_path" || {
        rm -f "$acceptance_part"
        return 1
    }
    rm -f "$acceptance_part" || return 1
    sync
}

broray_system_start_worker() {
    operation="$1"; mode="${2:-}"
    worker_acceptance_path="${BRORAY_WORKER_ACCEPTANCE_FILE:-}"
    case "$operation" in uninstall) ;; *) broray_system_error_json INVALID_OPERATION 'Операция обслуживается только persistent updater v5.'; return 1 ;; esac
    broray_system_require_transaction_runtime || { broray_system_error_json RUNTIME_ERROR 'Транзакционная среда не готова.'; return 1; }
    broray_system_legacy_status_normalize_before_start || {
        broray_system_error_json STATUS_STATE_INVALID 'Не удалось безопасно классифицировать предыдущий статус операции.'
        return 1
    }
    if [ -e "$BRORAY_COMPACT_REQUEST_LOCK" ] || [ -L "$BRORAY_COMPACT_REQUEST_LOCK" ]; then
        broray_system_error_json OPERATION_CONFLICT 'Обновление или переустановка BROray уже выполняется.'
        return 1
    fi
    if [ -e "$BRORAY_COMPACT_GLOBAL_LOCK" ] || [ -L "$BRORAY_COMPACT_GLOBAL_LOCK" ]; then
        broray_system_error_json OPERATION_CONFLICT 'Другая операция WebUI BROray выполняется либо её блокировка неоднозначна.'
        return 1
    fi
    operation_id="${operation}-$(date -u '+%Y%m%d%H%M%S')-$$"
    broray_system_global_lock_acquire "$operation" "$operation_id" || {
        control_reason="${BRORAY_TX_CONTROL_FAILURE_REASON:-}"
        control_evidence="${BRORAY_TX_CONTROL_FAILURE_EVIDENCE:-}"
        if [ -n "$control_reason" ]; then
            broray_system_control_rejection_status "$operation_id" "$operation" \
                "$control_reason" "$control_evidence" >/dev/null 2>&1 || true
            broray_system_error_details_json OPKG_CONTROL_PREFLIGHT_FAILED \
                'Штатная блокировка OPKG не подтверждена. Установка не началась; точная причина сохранена в диагностике.' \
                "$control_reason" "$control_evidence"
        else
            broray_system_control_rejection_status "$operation_id" "$operation" \
                control-state-busy-or-ambiguous '' >/dev/null 2>&1 || true
            broray_system_error_json OPERATION_CONFLICT 'Другая операция BROray уже выполняется или её состояние неоднозначно.'
        fi
        return 1
    }
    # Symmetric race closure: routes/updater check the legacy global lock
    # after claiming their own fence, and the system operation rechecks both
    # compact fences after publishing its inherited-owner lock.
    if [ -e "$BRORAY_COMPACT_REQUEST_LOCK" ] || [ -L "$BRORAY_COMPACT_REQUEST_LOCK" ] ||
       [ -e "$BRORAY_COMPACT_GLOBAL_LOCK" ] || [ -L "$BRORAY_COMPACT_GLOBAL_LOCK" ]
    then
        broray_system_global_lock_release >/dev/null 2>&1 || true
        broray_system_error_json OPERATION_CONFLICT 'Другая операция BROray началась одновременно; запрос не принят.'
        return 1
    fi
    broray_system_select_operation "$operation_id" || {
        broray_system_global_lock_release
        return 1
    }
    workers="$(broray_system_copy_worker "$operation_id")" || {
        broray_system_global_lock_release
        return 1
    }
    worker_bin="$(printf '%s
' "$workers"|sed -n '1p')"; worker_lib="$(printf '%s
' "$workers"|sed -n '2p')"
    handoff="$BRORAY_WORKER_ROOT/handoff-$operation_id"
    broray_system_handoff_prepare "$handoff" "$operation_id" "$operation" || {
        rm -f "$worker_bin" "$worker_lib"
        broray_system_global_lock_release
        return 1
    }
    broray_system_log_reset
    worker_pidfile="$BRORAY_WORKER_ROOT/pid-$operation_id"
    [ ! -e "$worker_pidfile" ] && [ ! -L "$worker_pidfile" ] || {
        broray_system_handoff_retire "$handoff" "$operation_id" >/dev/null 2>&1 || true
        rm -f "$worker_bin" "$worker_lib"
        broray_system_global_lock_release
        return 1
    }
    command -v start-stop-daemon >/dev/null 2>&1 || {
        broray_system_handoff_retire "$handoff" "$operation_id" >/dev/null 2>&1 || true
        rm -f "$worker_bin" "$worker_lib"
        broray_system_global_lock_release
        return 1
    }
    BRORAY_SYSTEM_LIB="$worker_lib" start-stop-daemon \
        -S -b -m -p "$worker_pidfile" -x /opt/bin/ash -O "$BRORAY_LOG" -- \
        "$worker_bin" "worker-$operation" "$operation_id" "$mode" "$handoff" \
        "$worker_acceptance_path" || {
        broray_system_handoff_retire "$handoff" "$operation_id" >/dev/null 2>&1 || true
        rm -f "$worker_pidfile" "$worker_bin" "$worker_lib"
        broray_system_global_lock_release
        return 1
    }
    worker_pid_wait=0
    while [ ! -f "$worker_pidfile" ] || [ -L "$worker_pidfile" ]; do
        worker_pid_wait=$((worker_pid_wait + 1))
        [ "$worker_pid_wait" -lt 15 ] || {
            broray_system_handoff_retire "$handoff" "$operation_id" >/dev/null 2>&1 || true
            rm -f "$worker_pidfile" "$worker_bin" "$worker_lib"
            broray_system_global_lock_release
            return 1
        }
        sleep 1
    done
    broray_system_control_file_bounded "$worker_pidfile" || {
        broray_system_handoff_retire "$handoff" "$operation_id" >/dev/null 2>&1 || true
        rm -f "$worker_bin" "$worker_lib"
        broray_system_global_lock_release
        return 1
    }
    [ "$(wc -l <"$worker_pidfile" 2>/dev/null | tr -d ' ')" -eq 1 ] || {
        rm -f "$worker_bin" "$worker_lib"
        broray_system_global_lock_release
        return 1
    }
    worker_pid="$(sed -n '1p' "$worker_pidfile" 2>/dev/null)"
    broray_system_is_pid "$worker_pid" || {
        rm -f "$worker_bin" "$worker_lib"
        broray_system_global_lock_release
        return 1
    }
    handoff_wait=0
    while [ ! -f "$handoff/worker-identity.tsv" ] || [ -L "$handoff/worker-identity.tsv" ] ||
          [ ! -f "$handoff/ready" ] || [ -L "$handoff/ready" ] ||
          [ ! -f "$handoff/control-released" ] || [ -L "$handoff/control-released" ]; do
        handoff_wait=$((handoff_wait + 1))
        if [ "$handoff_wait" -ge 15 ] || ! kill -0 "$worker_pid" 2>/dev/null; then
            if [ -f "$handoff/worker-identity.tsv" ] && [ ! -L "$handoff/worker-identity.tsv" ]; then
                broray_system_worker_terminate_exact "$worker_pid" "$handoff/worker-identity.tsv" 2>/dev/null || true
            fi
            wait "$worker_pid" 2>/dev/null || true
            broray_system_handoff_retire "$handoff" "$operation_id" >/dev/null 2>&1 || true
            rm -f "$worker_pidfile" 2>/dev/null || true
            broray_system_global_lock_release
            return 1
        fi
        sleep 1
    done
    worker_identity="$handoff/worker-identity.tsv"
    if [ ! -f "$worker_identity" ] || [ -L "$worker_identity" ] ||
       [ "$(sed -n '1p' "$handoff/ready" 2>/dev/null)" != "$(broray_tx_sha "$worker_identity")" ] ||
       [ "$(sed -n '1p' "$handoff/control-released" 2>/dev/null)" != "$operation_id" ] ||
       ! broray_system_global_lock_transfer "$worker_pid" "$operation_id" "$worker_identity" "$handoff"; then
        broray_system_worker_terminate_exact "$worker_pid" "$worker_identity" 2>/dev/null || true
        wait "$worker_pid" 2>/dev/null || true
        broray_system_handoff_retire "$handoff" "$operation_id" >/dev/null 2>&1 || true
        rm -f "$worker_pidfile" 2>/dev/null || true
        broray_system_global_lock_release
        broray_system_global_lock_recover_stale >/dev/null 2>&1 || true
        return 1
    fi
    jq -nc --arg operationId "$operation_id" --arg operation "$operation" '{ok:true,accepted:true,operationId:$operationId,operation:$operation,engine:"canonical-current-operation"}'
}

broray_system_worker_finish() {
    worker_bin="$1"; worker_lib="$2"
    if [ -n "${BRORAY_CURRENT_OPERATION_ID:-}" ] && command -v broray_operation_finalize_from_state >/dev/null 2>&1; then broray_operation_finalize_from_state "$BRORAY_CURRENT_OPERATION_ID" >/dev/null 2>&1 || true; fi
    broray_system_global_lock_release || return 1
    worker_pidfile="$BRORAY_WORKER_ROOT/pid-${BRORAY_CURRENT_OPERATION_ID:-}"
    if [ -n "${BRORAY_CURRENT_OPERATION_ID:-}" ] &&
       [ -f "$worker_pidfile" ] && [ ! -L "$worker_pidfile" ] &&
       [ "$(wc -l <"$worker_pidfile" 2>/dev/null | tr -d ' ')" -eq 1 ] &&
       [ "$(sed -n '1p' "$worker_pidfile" 2>/dev/null)" = "$$" ]
    then
        rm -f "$worker_pidfile"
    fi
    rm -f "$worker_bin" "$worker_lib"
}

broray_system_uninstall_start() {
    mode="$1"
    confirmation="$2"

    case "$mode" in
        normal)
            expected='УДАЛИТЬ BROray'
            ;;
        full)
            expected='УДАЛИТЬ BROray ПОЛНОСТЬЮ'
            ;;
        *)
            broray_system_error_json INVALID_MODE 'Неизвестный режим удаления.'
            return 1
            ;;
    esac

    [ "$confirmation" = "$expected" ] || {
        broray_system_error_json CONFIRMATION_REQUIRED 'Фраза подтверждения не совпала.'
        return 1
    }

    broray_system_start_worker uninstall "$mode"
}

broray_system_worker_uninstall() {
    operation_id="$1"
    mode="$2"
    preserved_dir="/opt/broray-preserved"
    preserved_archive=""
    preserved_archive_owned=false
    preserved_sidecar_owned=false
    preserved_sidecar_part_owned=false
    preserved_part_path=""
    preserved_part_owned=false
    preserved_stage_path=""
    preserved_stage_owned=false
    preserved_dir_preexisting=false
    preserved_dir_original_mode=""
    preserved_latest_expected_state=absent
    preserved_latest_expected_name=""
    preserved_latest_expected_pointer_sha=""
    preserved_latest_expected_archive_sha=""
    preserved_latest_expected_sidecar_sha=""
    uninstall_snapshot="$BRORAY_SCRATCH_ROOT/uninstall-$operation_id.tar.gz"
    uninstall_bundles="$BRORAY_SCRATCH_ROOT/uninstall-$operation_id.bundles"
    uninstall_active=""
    uninstall_xray_running=false
    uninstall_interface_owned=false
    uninstall_web_owned=false
    uninstall_dot_owned=false
    uninstall_snapshot_ready=false
    uninstall_mutation_started=false
    uninstall_opkg_committed=false
    uninstall_auth_root_owned=false
    uninstall_signal_active=false
    uninstall_dot_status="$BRORAY_SCRATCH_ROOT/uninstall-$operation_id.dot-status.json"
    uninstall_dot_request="$BRORAY_SCRATCH_ROOT/uninstall-$operation_id.dot-request.json"
    uninstall_dot_verify="$BRORAY_SCRATCH_ROOT/uninstall-$operation_id.dot-verify.json"
    uninstall_services="$BRORAY_SCRATCH_ROOT/uninstall-$operation_id.services"
    uninstall_registration_hashes="$BRORAY_SCRATCH_ROOT/uninstall-$operation_id.registration.sha256"

    broray_system_uninstall_exact_regular_600() {
        exact_regular_path="$1"
        [ -f "$exact_regular_path" ] && [ ! -L "$exact_regular_path" ] || return 1
        [ "$(find -P "$exact_regular_path" -maxdepth 0 -type f -printf '%m\n' 2>/dev/null)" = 600 ]
    }

    broray_system_uninstall_latest_pointer_valid() {
        latest_pointer_path="$1"
        latest_pointer_expected_name="${2:-}"
        broray_system_uninstall_exact_regular_600 "$latest_pointer_path" || return 1
        [ "$(wc -l <"$latest_pointer_path" | tr -d ' ')" = 1 ] || return 1
        latest_pointer_name="$(sed -n '1p' "$latest_pointer_path" 2>/dev/null)" || return 1
        [ -n "$latest_pointer_name" ] || return 1
        [ "$(wc -c <"$latest_pointer_path" | tr -d ' ')" = "$(( ${#latest_pointer_name} + 1 ))" ] || return 1
        case "$latest_pointer_name" in
            broray-user-data-?*.tar.gz) ;;
            *) return 1 ;;
        esac
        latest_pointer_stem="${latest_pointer_name#broray-user-data-}"
        latest_pointer_stem="${latest_pointer_stem%.tar.gz}"
        latest_pointer_date="${latest_pointer_stem%%-*}"
        latest_pointer_rest="${latest_pointer_stem#*-}"
        [ "$latest_pointer_rest" != "$latest_pointer_stem" ] || return 1
        latest_pointer_time="${latest_pointer_rest%%-*}"
        latest_pointer_operation="${latest_pointer_rest#*-}"
        [ "$latest_pointer_operation" != "$latest_pointer_rest" ] || return 1
        [ "${#latest_pointer_date}" -eq 8 ] && [ "${#latest_pointer_time}" -eq 6 ] || return 1
        case "$latest_pointer_date:$latest_pointer_time" in
            *[!0-9:]*) return 1 ;;
        esac
        case "$latest_pointer_operation" in
            ''|*[!A-Za-z0-9._-]*|*..*) return 1 ;;
        esac
        [ -z "$latest_pointer_expected_name" ] ||
            [ "$latest_pointer_name" = "$latest_pointer_expected_name" ] || return 1
        return 0
    }

    broray_system_uninstall_preserved_archive_pair_valid() {
        preserved_pair_name="$1"
        preserved_pair_archive="$preserved_dir/$preserved_pair_name"
        preserved_pair_sidecar="$preserved_pair_archive.sha256"
        broray_system_uninstall_exact_regular_600 "$preserved_pair_archive" || return 1
        broray_system_uninstall_exact_regular_600 "$preserved_pair_sidecar" || return 1
        broray_system_user_backup_valid "$preserved_pair_archive" || return 1
        [ "$(wc -l <"$preserved_pair_sidecar" | tr -d ' ')" = 1 ] || return 1
        preserved_pair_actual_sha="$(sha256sum "$preserved_pair_archive" 2>/dev/null | awk 'NR==1{print $1;exit}')" || return 1
        case "$preserved_pair_actual_sha" in
            ''|*[!0-9a-f]*) return 1 ;;
        esac
        [ "${#preserved_pair_actual_sha}" -eq 64 ] || return 1
        preserved_pair_expected_line="$preserved_pair_actual_sha  $preserved_pair_name"
        [ "$(sed -n '1p' "$preserved_pair_sidecar" 2>/dev/null)" = "$preserved_pair_expected_line" ] || return 1
        [ "$(wc -c <"$preserved_pair_sidecar" | tr -d ' ')" = "$(( ${#preserved_pair_expected_line} + 1 ))" ] || return 1
        return 0
    }

    # Inspecting LATEST is deliberately read-only.  The optional argument is
    # the one temporary pointer created by this worker during publication;
    # every other LATEST.part.* object is an unsafe collision.
    broray_system_uninstall_latest_inspect() {
        latest_allowed_part="${1:-}"
        latest_allowed_part_seen=false
        latest_inspect_state=absent
        latest_inspect_name=""
        latest_inspect_pointer_sha=""
        latest_inspect_archive_sha=""
        latest_inspect_sidecar_sha=""

        if [ ! -e "$preserved_dir" ] && [ ! -L "$preserved_dir" ]; then
            [ -z "$latest_allowed_part" ] || return 1
            return 0
        fi
        [ -d "$preserved_dir" ] && [ ! -L "$preserved_dir" ] || return 1

        for latest_part_object in "$preserved_dir"/LATEST.part.*
        do
            if [ ! -e "$latest_part_object" ] && [ ! -L "$latest_part_object" ]; then
                continue
            fi
            if [ -n "$latest_allowed_part" ] &&
               [ "$latest_part_object" = "$latest_allowed_part" ] &&
               broray_system_uninstall_exact_regular_600 "$latest_part_object"
            then
                latest_allowed_part_seen=true
                continue
            fi
            return 1
        done
        [ -z "$latest_allowed_part" ] || [ "$latest_allowed_part_seen" = true ] || return 1

        latest_pointer="$preserved_dir/LATEST"
        if [ ! -e "$latest_pointer" ] && [ ! -L "$latest_pointer" ]; then
            return 0
        fi
        broray_system_uninstall_latest_pointer_valid "$latest_pointer" || return 1
        latest_inspect_name="$latest_pointer_name"
        broray_system_uninstall_preserved_archive_pair_valid "$latest_inspect_name" || return 1
        latest_inspect_pointer_sha="$(sha256sum "$latest_pointer" 2>/dev/null | awk 'NR==1{print $1;exit}')" || return 1
        latest_inspect_archive_sha="$preserved_pair_actual_sha"
        latest_inspect_sidecar_sha="$(sha256sum "$preserved_dir/$latest_inspect_name.sha256" 2>/dev/null | awk 'NR==1{print $1;exit}')" || return 1
        case "$latest_inspect_pointer_sha:$latest_inspect_sidecar_sha" in
            *[!0-9a-f:]*) return 1 ;;
        esac
        [ "${#latest_inspect_pointer_sha}" -eq 64 ] &&
        [ "${#latest_inspect_sidecar_sha}" -eq 64 ] || return 1
        latest_inspect_state=present
        return 0
    }

    broray_system_uninstall_latest_record_preflight() {
        broray_system_uninstall_latest_inspect || return 1
        preserved_latest_expected_state="$latest_inspect_state"
        preserved_latest_expected_name="$latest_inspect_name"
        preserved_latest_expected_pointer_sha="$latest_inspect_pointer_sha"
        preserved_latest_expected_archive_sha="$latest_inspect_archive_sha"
        preserved_latest_expected_sidecar_sha="$latest_inspect_sidecar_sha"
    }

    broray_system_uninstall_latest_matches_preflight() {
        latest_match_allowed_part="${1:-}"
        broray_system_uninstall_latest_inspect "$latest_match_allowed_part" || return 1
        [ "$latest_inspect_state" = "$preserved_latest_expected_state" ] || return 1
        [ "$latest_inspect_name" = "$preserved_latest_expected_name" ] || return 1
        [ "$latest_inspect_pointer_sha" = "$preserved_latest_expected_pointer_sha" ] || return 1
        [ "$latest_inspect_archive_sha" = "$preserved_latest_expected_archive_sha" ] || return 1
        [ "$latest_inspect_sidecar_sha" = "$preserved_latest_expected_sidecar_sha" ] || return 1
    }

    broray_system_uninstall_latest_publish() {
        latest_publish_name="$1"
        latest_publish_part="$preserved_dir/LATEST.part.$$"
        [ ! -e "$latest_publish_part" ] && [ ! -L "$latest_publish_part" ] || return 1
        broray_system_uninstall_latest_matches_preflight || return 1
        (cd "$preserved_dir" && set -C &&
            printf '%s\n' "$latest_publish_name" >"LATEST.part.$$") || return 1
        if ! chmod 600 "$latest_publish_part" ||
           ! broray_system_uninstall_latest_pointer_valid "$latest_publish_part" "$latest_publish_name" ||
           ! broray_system_uninstall_latest_matches_preflight "$latest_publish_part"
        then
            rm -f "$latest_publish_part" 2>/dev/null || true
            return 1
        fi
        if ! mv -f "$latest_publish_part" "$preserved_dir/LATEST"; then
            rm -f "$latest_publish_part" 2>/dev/null || true
            return 1
        fi
        [ ! -e "$latest_publish_part" ] && [ ! -L "$latest_publish_part" ] || return 1
        broray_system_uninstall_latest_inspect || return 1
        [ "$latest_inspect_state" = present ] &&
        [ "$latest_inspect_name" = "$latest_publish_name" ] || return 1
        return 0
    }

    broray_system_uninstall_preserved_rollback() {
        broray_system_protected_backup_owned_cleanup >/dev/null 2>&1 || true
        if [ "$preserved_sidecar_part_owned" = true ]; then
            rm -f "$preserved_archive.sha256.part" 2>/dev/null || true
        fi
        if [ "$preserved_sidecar_owned" = true ]; then
            rm -f "$preserved_archive.sha256" 2>/dev/null || true
        fi
        if [ "$preserved_archive_owned" = true ]; then
            rm -f "$preserved_archive" 2>/dev/null || true
        fi
        if [ "$preserved_dir_preexisting" = true ]; then
            case "$preserved_dir_original_mode" in
                ''|*[!0-7]*) return 1 ;;
                *) chmod "$preserved_dir_original_mode" "$preserved_dir" 2>/dev/null || return 1 ;;
            esac
        elif [ -d "$preserved_dir" ] && [ ! -L "$preserved_dir" ]; then
            rmdir "$preserved_dir" 2>/dev/null || return 1
        fi
        return 0
    }

    broray_system_uninstall_aux_services_restore() {
        [ -s "$uninstall_services" ] || return 0
        services_restore_failed=false
        while IFS= read -r restore_service
        do
            case "$restore_service" in
                S22broray-updater|S23broray-monitor|S25broray-web|S27broray-auto-switch|S28broray-subscriptions)
                    restore_init="$BRORAY_INIT_ROOT/$restore_service"
                    if [ -x "$restore_init" ]; then
                        /opt/bin/ash "$restore_init" start >>"$BRORAY_LOG" 2>&1 ||
                            services_restore_failed=true
                    else
                        services_restore_failed=true
                    fi
                    ;;
                *) services_restore_failed=true ;;
            esac
        done <"$uninstall_services"
        [ "$services_restore_failed" = false ]
    }

    broray_system_uninstall_auth_retire() {
        [ "$uninstall_auth_root_owned" = true ] || return 0
        [ -d "$BRORAY_OPKG_AUTH_ROOT" ] && [ ! -L "$BRORAY_OPKG_AUTH_ROOT" ] || return 1
        rm -f "$BRORAY_UNINSTALL_AUTH" "$BRORAY_UNINSTALL_AUTH.new.$$" 2>/dev/null || return 1
        rmdir "$BRORAY_OPKG_AUTH_ROOT" 2>/dev/null || return 1
        uninstall_auth_root_owned=false
        return 0
    }

    broray_system_uninstall_aux_services_stop() {
        : >"$uninstall_services" || return 1
        chmod 600 "$uninstall_services" || return 1

        for capture_service in \
            S22broray-updater S23broray-monitor S25broray-web \
            S27broray-auto-switch S28broray-subscriptions
        do
            capture_init="$BRORAY_INIT_ROOT/$capture_service"
            if [ -x "$capture_init" ] &&
               /opt/bin/ash "$capture_init" status >/dev/null 2>&1
            then
                printf '%s\n' "$capture_service" >>"$uninstall_services" || return 1
            fi
        done

        for stop_service in \
            S28broray-subscriptions S27broray-auto-switch S25broray-web \
            S23broray-monitor S22broray-updater
        do
            stop_init="$BRORAY_INIT_ROOT/$stop_service"
            [ -x "$stop_init" ] || continue
            /opt/bin/ash "$stop_init" stop >>"$BRORAY_LOG" 2>&1 || return 1
            if /opt/bin/ash "$stop_init" status >/dev/null 2>&1; then
                return 1
            fi
        done
        return 0
    }

    broray_system_uninstall_payload_preflight() {
        for owned_root in /opt/broray /opt/var/lib/broray /opt/var/lib/broray-updater
        do
            [ -d "$owned_root" ] && [ ! -L "$owned_root" ] || return 1
        done

        [ -d /opt/libexec/broray-updater ] && [ ! -L /opt/libexec/broray-updater ] || return 1
        [ -f /opt/bin/broray-updaterctl ] && [ ! -L /opt/bin/broray-updaterctl ] || return 1
        [ -L /opt/broray/bin ] &&
        [ "$(readlink /opt/broray/bin 2>/dev/null)" = 'current/app/bin' ] || return 1
        [ -L /opt/broray/lib ] &&
        [ "$(readlink /opt/broray/lib 2>/dev/null)" = 'current/app/lib' ] || return 1
        [ -f /opt/lib/opkg/info/broray.control ] &&
        [ ! -L /opt/lib/opkg/info/broray.control ] || return 1
        awk -F ': ' '
          $1=="Package"{p++;pv=$2}
          $1=="Version"{v++;vv=$2}
          $1=="Architecture"{a++;av=$2}
          $1=="X-BROray-Build-ID"{b++;bv=$2}
          END{exit !(p==1&&pv=="broray"&&v==1&&vv=="3.0.0-r14"&&
                     a==1&&av=="aarch64-3.10"&&b==1&&bv=="R14C01")}
        ' /opt/lib/opkg/info/broray.control || return 1

        [ -f /opt/broray/current/app/share/release/manifest.json ] &&
        [ ! -L /opt/broray/current/app/share/release/manifest.json ] || return 1
        jq -e '
          .schemaVersion==3 and .lifecycleContract=="compact-app-rename/1" and
          .packageVersion=="3.0.0-r14" and .packageRevision==14 and
          .architecture=="aarch64-3.10" and
          (.candidateId|type)=="string" and (.candidateId|length)>0 and
          (.webUIBuild|type)=="string" and (.webUIBuild|length)>0
        ' /opt/broray/current/app/share/release/manifest.json >/dev/null 2>&1 || return 1

        for owned_command in \
            broray broray-routes broray-routes-dot broray-routes-user \
            broray-server broray-servers broray-subscriptions broray-system
        do
            owned_path="/opt/bin/$owned_command"
            [ -L "$owned_path" ] || return 1
            [ "$(readlink "$owned_path" 2>/dev/null)" = "/opt/broray/bin/$owned_command" ] || return 1
            [ -x "$owned_path" ] || return 1
        done

        [ -f /opt/etc/init.d/S22broray-updater ] &&
        [ ! -L /opt/etc/init.d/S22broray-updater ] || return 1
        for owned_service in \
            S23broray-monitor S24broray S25broray-web \
            S27broray-auto-switch S28broray-subscriptions
        do
            owned_init="/opt/etc/init.d/$owned_service"
            [ -L "$owned_init" ] || return 1
            [ "$(readlink "$owned_init" 2>/dev/null)" = "/opt/broray/current/init/$owned_service" ] || return 1
        done

        broray_system_legacy_opkg_feed_valid || return 1
        return 0
    }

    broray_system_uninstall_registration_healthy() {
        registration_status="$(opkg status "$BRORAY_PACKAGE" 2>/dev/null || true)"
        printf '%s\n' "$registration_status" | awk -F ': ' '
          $1=="Package"{p++;pv=$2}
          $1=="Version"{v++;vv=$2}
          $1=="Architecture"{a++;av=$2}
          $1=="Status"{s++;sv=$2}
          END{exit !(p==1&&pv=="broray"&&v==1&&vv=="3.0.0-r14"&&
                     a==1&&av=="aarch64-3.10"&&s==1&&sv=="install user installed")}
        ' || return 1
        for registration_file in control preinst postinst prerm postrm list
        do
            registration_path="/opt/lib/opkg/info/broray.$registration_file"
            [ -f "$registration_path" ] && [ ! -L "$registration_path" ] || return 1
        done
        [ ! -s /opt/lib/opkg/info/broray.list ] || return 1
        [ -f "$uninstall_registration_hashes" ] && [ ! -L "$uninstall_registration_hashes" ] || return 1
        sha256sum -c "$uninstall_registration_hashes" >/dev/null 2>&1 || return 1
        return 0
    }

    broray_system_uninstall_payload_finalize() {
        finalize_failed=false

        for finalize_service in \
            S22broray-updater S23broray-monitor S24broray \
            S25broray-web S27broray-auto-switch S28broray-subscriptions
        do
            rm -f "/opt/etc/init.d/$finalize_service" || finalize_failed=true
        done
        for finalize_command in \
            broray broray-routes broray-routes-dot broray-routes-user \
            broray-server broray-servers broray-subscriptions broray-system
        do
            rm -f "/opt/bin/$finalize_command" || finalize_failed=true
        done

        rm -f /opt/bin/broray-updaterctl /opt/etc/opkg/broray.conf || finalize_failed=true
        rm -rf \
            /opt/libexec/broray-updater \
            /opt/broray \
            /opt/var/lib/broray \
            /opt/var/lib/broray-updater \
            /opt/var/lock/broray || finalize_failed=true
        if [ "$mode" = full ]; then
            rm -rf "$preserved_dir" || finalize_failed=true
        fi
        for finalized_path in \
            /opt/broray /opt/libexec/broray-updater /opt/bin/broray-updaterctl \
            /opt/var/lib/broray /opt/var/lib/broray-updater \
            /opt/var/lock/broray
        do
            [ ! -e "$finalized_path" ] && [ ! -L "$finalized_path" ] || finalize_failed=true
        done
        sync || finalize_failed=true
        [ "$finalize_failed" = false ]
    }

    broray_system_uninstall_restore() {
        restore_failed=false

        if [ -s "$uninstall_snapshot" ] && gzip -t "$uninstall_snapshot" >/dev/null 2>&1; then
            tar -xzf "$uninstall_snapshot" -C "$BRORAY_BASE" >>"$BRORAY_LOG" 2>&1 ||
                restore_failed=true
        else
            restore_failed=true
        fi

        # The local DoT ownership receipt/state has just been restored from the
        # verified snapshot.  Re-apply that exact selection through the same
        # transactional manager used by WebUI, then prove that the complete
        # live secure-DNS pre-state is byte-for-byte equivalent by normalized
        # address/SNI/port keys and counts.
        if [ "$uninstall_dot_owned" = true ]; then
            if [ -x "$BRORAY_BASE/bin/broray-routes-dot" ] &&
               [ -s "$uninstall_dot_request" ] &&
               [ -s "$uninstall_dot_status" ] &&
               BRORAY_DOT_LIB="$BRORAY_SYSTEM_DOT_LIB" \
               BRORAY_DOT_RESTORE_EXACT=true \
                   "$BRORAY_BASE/bin/broray-routes-dot" apply "$uninstall_dot_request" \
                   >>"$BRORAY_LOG" 2>&1 &&
               BRORAY_DOT_LIB="$BRORAY_SYSTEM_DOT_LIB" \
                   "$BRORAY_BASE/bin/broray-routes-dot" status >"$uninstall_dot_verify" 2>>"$BRORAY_LOG" &&
               jq -e --slurpfile before "$uninstall_dot_status" '
                 def keys($items):
                   [$items[]? | [.address, (.sni // ""), ((.port // "") | tostring)]] | sort;
                 (.recoveryRequired == false) and
                 (keys(.actual.dot // []) == keys($before[0].actual.dot // [])) and
                 ((.actual.dohCount // 0) == ($before[0].actual.dohCount // 0)) and
                 ((.actual.totalSecure // 0) == ($before[0].actual.totalSecure // 0)) and
                 (keys(.managed // []) == keys($before[0].managed // []))
               ' "$uninstall_dot_verify" >/dev/null 2>&1
            then
                :
            else
                restore_failed=true
            fi
        fi

        if [ "$uninstall_interface_owned" = true ]; then
            BRORAY_BASE="$BRORAY_BASE" /opt/bin/ash "$BRORAY_BASE/lib/interface.sh" repair \
                >>"$BRORAY_LOG" 2>&1 || restore_failed=true
        fi

        if [ "$uninstall_web_owned" = true ] && [ -r "$BRORAY_BASE/lib/web-publish.sh" ]; then
            . "$BRORAY_BASE/lib/web-publish.sh"
            broray_web_publish_ensure >>"$BRORAY_LOG" 2>&1 || restore_failed=true
        fi

        if [ -s "$uninstall_bundles" ]; then
            while IFS= read -r restore_bundle
            do
                [ -n "$restore_bundle" ] || continue
                "$BRORAY_BASE/bin/broray-routes" export "$restore_bundle" \
                    >>"$BRORAY_LOG" 2>&1 || restore_failed=true
            done <"$uninstall_bundles"
        fi

        if [ -n "$uninstall_active" ]; then
            "$BRORAY_BASE/bin/broray-servers" activate "$uninstall_active" \
                >>"$BRORAY_LOG" 2>&1 || restore_failed=true
            if [ "$uninstall_xray_running" != true ]; then
                /opt/bin/ash "$BRORAY_INIT_ROOT/S24broray" stop \
                    >>"$BRORAY_LOG" 2>&1 || restore_failed=true
            fi
        elif [ "$uninstall_xray_running" = true ]; then
            /opt/bin/ash "$BRORAY_INIT_ROOT/S24broray" start \
                >>"$BRORAY_LOG" 2>&1 || restore_failed=true
        fi

        broray_system_uninstall_aux_services_restore || restore_failed=true

        [ "$restore_failed" = false ]
    }

    broray_system_uninstall_abort() {
        abort_stage="$1"
        abort_message="$2"
        if broray_system_uninstall_restore; then
            rollback_message='Исходное состояние восстановлено.'
        else
            rollback_message='Автоматический откат завершился не полностью; проверьте технический журнал.'
        fi
        broray_system_status_write "$operation_id" uninstall error "$abort_stage" 100 \
            "$abort_message" "$rollback_message" >/dev/null 2>&1 || true
        rm -f "$uninstall_snapshot" "$uninstall_bundles" \
            "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
            "$uninstall_services" "$uninstall_registration_hashes" \
            2>/dev/null || true
        broray_system_uninstall_preserved_rollback >/dev/null 2>&1 || true
        broray_system_uninstall_auth_retire >/dev/null 2>&1 || true
        uninstall_mutation_started=false
        return 1
    }

    broray_system_uninstall_registration_recovery_required() {
        recovery_reason="$1"
        recovery_opkg_rc="$2"
        recovery_stage="$3"
        recovery_rollback_ok=true
        broray_system_uninstall_restore >/dev/null 2>&1 || recovery_rollback_ok=false
        broray_system_uninstall_preserved_rollback >/dev/null 2>&1 || recovery_rollback_ok=false

        if [ "$uninstall_auth_root_owned" != true ] ||
           [ ! -d "$BRORAY_OPKG_AUTH_ROOT" ] || [ -L "$BRORAY_OPKG_AUTH_ROOT" ]
        then
            recovery_rollback_ok=false
        else
            rm -f "$BRORAY_UNINSTALL_AUTH" "$BRORAY_UNINSTALL_AUTH.new.$$" 2>/dev/null ||
                recovery_rollback_ok=false
            recovery_marker="$BRORAY_OPKG_AUTH_ROOT/registration-recovery-required.json"
            jq -n \
                --arg operationId "$operation_id" \
                --arg reason "$recovery_reason" \
                --arg stage "$recovery_stage" \
                --argjson opkgRc "$recovery_opkg_rc" \
                --argjson rollbackCompleted "$recovery_rollback_ok" \
                --arg createdAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
                '{schemaVersion:1,owner:"BROray-WebUI",operationId:$operationId,
                  reason:$reason,stage:$stage,opkgRc:$opkgRc,
                  rollbackCompleted:$rollbackCompleted,createdAt:$createdAt,
                  automaticRetry:false}' \
                >"$recovery_marker.part.$$" 2>/dev/null &&
            chmod 600 "$recovery_marker.part.$$" 2>/dev/null &&
            mv -f "$recovery_marker.part.$$" "$recovery_marker" 2>/dev/null || {
                rm -f "$recovery_marker.part.$$" 2>/dev/null || true
                recovery_rollback_ok=false
            }
        fi

        rm -f "$uninstall_snapshot" "$uninstall_bundles" \
            "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
            "$uninstall_services" "$uninstall_registration_hashes" 2>/dev/null || true
        uninstall_mutation_started=false
        if [ "$recovery_rollback_ok" = true ]; then
            recovery_message='Конфигурация и сервисы фактически восстановлены; повтор заблокирован до проверки OPKG.'
        else
            recovery_message='Откат или публикация recovery-маркера завершились не полностью; требуется ручная проверка.'
        fi
        broray_system_status_write "$operation_id" uninstall error "$recovery_stage" 100 \
            'Состояние регистрации OPKG после удаления неоднозначно.' "$recovery_message" \
            >/dev/null 2>&1 || true
        return 1
    }

    broray_system_uninstall_finalization_marker_write() {
        finalization_reason="$1"
        finalization_stage="$2"
        [ "$uninstall_auth_root_owned" = true ] || return 1
        [ -d "$BRORAY_OPKG_AUTH_ROOT" ] && [ ! -L "$BRORAY_OPKG_AUTH_ROOT" ] || return 1
        finalization_marker="$BRORAY_OPKG_AUTH_ROOT/finalization-recovery-required.json"
        jq -n \
            --arg operationId "$operation_id" \
            --arg reason "$finalization_reason" \
            --arg stage "$finalization_stage" \
            --arg createdAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
            '{schemaVersion:1,owner:"BROray-WebUI",operationId:$operationId,
              reason:$reason,stage:$stage,opkgRegistrationRemoved:true,
              createdAt:$createdAt,automaticRetry:false}' \
            >"$finalization_marker.part.$$" || return 1
        chmod 600 "$finalization_marker.part.$$" || {
            rm -f "$finalization_marker.part.$$" 2>/dev/null || true
            return 1
        }
        mv -f "$finalization_marker.part.$$" "$finalization_marker"
    }

    broray_system_uninstall_signal() {
        uninstall_signal_rc="$1"
        trap - HUP INT TERM
        [ "$uninstall_signal_active" = false ] || exit "$uninstall_signal_rc"
        uninstall_signal_active=true

        if [ "$uninstall_opkg_committed" != true ]; then
            if [ "$uninstall_mutation_started" = true ] &&
               [ "$uninstall_snapshot_ready" = true ]
            then
                broray_system_uninstall_abort signal \
                    'Удаление прервано сигналом; выполнен транзакционный откат.' \
                    >/dev/null 2>&1 || true
            else
                rm -f "$uninstall_snapshot" "$uninstall_bundles" \
                    "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
                    "$uninstall_services" "$uninstall_registration_hashes" \
                    2>/dev/null || true
                broray_system_uninstall_auth_retire >/dev/null 2>&1 || true
                broray_system_uninstall_preserved_rollback >/dev/null 2>&1 || true
                broray_system_status_write "$operation_id" uninstall error signal 100 \
                    'Удаление прервано до начала мутаций.' \
                    'Пакет и конфигурация не изменялись.' >/dev/null 2>&1 || true
            fi
        else
            rm -f "$uninstall_snapshot" "$uninstall_bundles" \
                "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
                "$uninstall_services" "$uninstall_registration_hashes" \
                2>/dev/null || true
        fi

        if ! broray_system_worker_finish "${worker_bin:-$0}" "${worker_lib:-}" >/dev/null 2>&1; then
            broray_system_global_lock_release >/dev/null 2>&1 || true
        fi
        exit "$uninstall_signal_rc"
    }

    trap 'broray_system_uninstall_signal 129' HUP
    trap 'broray_system_uninstall_signal 130' INT
    trap 'broray_system_uninstall_signal 143' TERM

    [ -f /opt/lib/opkg/info/broray.prerm ] && [ ! -L /opt/lib/opkg/info/broray.prerm ] || {
        broray_system_status_write "$operation_id" uninstall error preflight 100 \
            'Удаление отменено: OPKG-регистрация BROray повреждена.' ''
        return 1
    }
    opkg status "$BRORAY_PACKAGE" 2>/dev/null |
        awk '$1=="Status:" && $2=="install" && $3=="user" && $4=="installed" {ok=1} END{exit(ok?0:1)}' || {
        broray_system_status_write "$operation_id" uninstall error preflight 100 \
            'Удаление отменено: пакет BROray не находится в состоянии install user installed.' ''
        return 1
    }
    if [ "$mode" = normal ] && ! broray_system_uninstall_latest_record_preflight; then
        broray_system_status_write "$operation_id" uninstall error preflight 100 \
            'Удаление отменено: LATEST или временный указатель архива небезопасен.' ''
        return 1
    fi

    # One pre-mutation snapshot drives both failpoint rollback and the normal
    # external user archive.  It is verified before any Keenetic/service
    # mutation and remains on /tmp until OPKG has committed removal.
    set --
    while IFS= read -r item; do
        [ -e "$BRORAY_BASE/$item" ] && set -- "$@" "$item"
    done <<EOF_UNINSTALL_ROOTS
$(broray_system_protected_roots)
EOF_UNINSTALL_ROOTS
    [ "$#" -gt 0 ] || {
        broray_system_status_write "$operation_id" uninstall error preflight 100 \
            'Удаление отменено: пользовательское состояние не найдено.' ''
        return 1
    }
    tar -czf "$uninstall_snapshot" -C "$BRORAY_BASE" "$@" \
        >>"$BRORAY_LOG" 2>&1 &&
        gzip -t "$uninstall_snapshot" >>"$BRORAY_LOG" 2>&1 || {
            rm -f "$uninstall_snapshot"
            broray_system_status_write "$operation_id" uninstall error backup 100 \
                'Удаление отменено: проверяемый снимок создать не удалось.' ''
            return 1
        }
    uninstall_snapshot_ready=true

    : >"$uninstall_bundles" || return 1
    routes_registry="$BRORAY_BASE/routes/bundles.json"
    routes_summary_library="$BRORAY_BASE/lib/routes-summary.sh"
    if [ ! -f "$routes_registry" ] || [ -L "$routes_registry" ] ||
       [ ! -f "$routes_summary_library" ] || [ -L "$routes_summary_library" ] ||
       ! jq -e '
          (.bundles|type)=="array" and
          ((.bundles|length)==(.bundles|unique|length)) and
          all(.bundles[]; type=="string" and length>0 and
              all(explode[];
                  (.>=48 and .<=57) or (.>=65 and .<=90) or
                  (.>=97 and .<=122) or .==45 or .==46 or .==95))
       ' "$routes_registry" >/dev/null 2>&1
    then
        broray_system_status_write "$operation_id" uninstall error preflight 100 \
            'Удаление отменено: реестр маршрутов отсутствует или повреждён.' ''
        rm -f "$uninstall_snapshot" "$uninstall_bundles" 2>/dev/null || true
        return 1
    fi
    . "$routes_summary_library"
    routes_registry_ids="$(jq -r '.bundles[]' "$routes_registry" 2>/dev/null)" || return 1
    routes_capture_failed=false
    while IFS= read -r uninstall_bundle
    do
        [ -n "$uninstall_bundle" ] || continue
        uninstall_summary="$(broray_routes_summary "$uninstall_bundle" 2>/dev/null)" || {
            routes_capture_failed=true
            break
        }
        uninstall_installed="$(printf '%s' "$uninstall_summary" | jq -er '
          .installed | if .==true then "true" elif .==false then "false" else error("invalid") end
        ' 2>/dev/null)" || {
            routes_capture_failed=true
            break
        }
        [ "$uninstall_installed" != true ] ||
            printf '%s\n' "$uninstall_bundle" >>"$uninstall_bundles" || {
                routes_capture_failed=true
                break
            }
    done <<EOF_UNINSTALL_BUNDLES
$routes_registry_ids
EOF_UNINSTALL_BUNDLES
    if [ "$routes_capture_failed" = true ]; then
        broray_system_status_write "$operation_id" uninstall error preflight 100 \
            'Удаление отменено: состояние маршрутов неоднозначно.' ''
        rm -f "$uninstall_snapshot" "$uninstall_bundles" 2>/dev/null || true
        return 1
    fi

    uninstall_active="$(sed -n '1p' "$BRORAY_BASE/config/active-server" 2>/dev/null || true)"
    if [ -x "$BRORAY_INIT_ROOT/S24broray" ] &&
       /opt/bin/ash "$BRORAY_INIT_ROOT/S24broray" status >/dev/null 2>&1
    then
        uninstall_xray_running=true
    fi
    if [ -r "$BRORAY_BASE/lib/interface.sh" ] &&
       BRORAY_BASE="$BRORAY_BASE" /opt/bin/ash "$BRORAY_BASE/lib/interface.sh" check >/dev/null 2>&1
    then
        uninstall_interface_owned=true
    fi
    if [ -r "$BRORAY_BASE/lib/web-publish.sh" ]; then
        . "$BRORAY_BASE/lib/web-publish.sh"
        uninstall_web_lan="$(broray_web_publish_lan_ip 2>/dev/null || true)"
        if [ -n "$uninstall_web_lan" ] && broray_web_publish_owner_valid "$uninstall_web_lan"; then
            uninstall_web_owned=true
        fi
    fi
    uninstall_dot_managed_count="$(
        jq -r '(.managed // []) | length' "$BRORAY_BASE/routes/dot/config.json" 2>/dev/null || printf '%s' 0
    )"
    case "$uninstall_dot_managed_count" in ''|*[!0-9]*) uninstall_dot_managed_count=0 ;; esac
    if [ "$uninstall_dot_managed_count" -gt 0 ]; then
        if [ ! -x "$BRORAY_BASE/bin/broray-routes-dot" ] ||
           ! BRORAY_DOT_LIB="$BRORAY_SYSTEM_DOT_LIB" \
               "$BRORAY_BASE/bin/broray-routes-dot" status \
               >"$uninstall_dot_status" 2>>"$BRORAY_LOG"
        then
            broray_system_status_write "$operation_id" uninstall error preflight 100 \
                'Удаление отменено: фактическое состояние DNS-over-TLS BROray неоднозначно.' ''
            rm -f "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
                2>/dev/null || true
            return 1
        fi
        if jq -e --argjson managedCount "$uninstall_dot_managed_count" '
          (.recoveryRequired == false) and (.runningConfigAvailable == true) and
          (.drift == false) and (.matchesSelection == true) and
          (.managedPresentCount == $managedCount) and
          ((.requestedIds | type)=="array") and ((.requestedIds | length)>0)
        ' "$uninstall_dot_status" >/dev/null 2>&1
        then
            jq '{serverIds:.requestedIds,allowUntested:false}' "$uninstall_dot_status" \
                >"$uninstall_dot_request" &&
            jq -e '(.serverIds|type)=="array" and (.serverIds|length)>0 and .allowUntested==false' \
                "$uninstall_dot_request" >/dev/null 2>&1 &&
            chmod 600 "$uninstall_dot_status" "$uninstall_dot_request" || {
                broray_system_status_write "$operation_id" uninstall error preflight 100 \
                    'Удаление отменено: DoT receipt не удалось подготовить к точному откату.' ''
                rm -f "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
                    2>/dev/null || true
                return 1
            }
            uninstall_dot_owned=true
        elif jq -e --argjson managedCount "$uninstall_dot_managed_count" '
          (.recoveryRequired == false) and (.runningConfigAvailable == true) and
          (.actual.determinate == true) and (.actual.runtimeReconciled == true) and
          ((.actual.dot // []) | length) == 0 and
          ((.actual.totalSecure // 0) == 0) and
          (.managedPresentCount == 0) and
          ((.managed // []) | length) == $managedCount and
          ((.requestedIds | type)=="array") and ((.requestedIds | length)>0)
        ' "$uninstall_dot_status" >/dev/null 2>&1
        then
            # The receipts are historical only: there is no live selector to
            # delete or restore.  Preserve their bytes in the protected backup,
            # but never grant destructive ownership from history alone.
            uninstall_dot_owned=false
            broray_system_log 'DoT live set is determinately empty; stale receipts are preserved without delete authority.'
        else
            broray_system_status_write "$operation_id" uninstall error preflight 100 \
                'Удаление отменено: фактическое состояние DNS-over-TLS BROray неоднозначно.' ''
            rm -f "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
                2>/dev/null || true
            return 1
        fi
    fi

    broray_system_uninstall_payload_preflight || {
        broray_system_status_write "$operation_id" uninstall error preflight 100 \
            'Удаление отменено: состав или владение файлов BROray неоднозначны.' ''
        rm -f "$uninstall_snapshot" "$uninstall_bundles" \
            "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
            "$uninstall_services" "$uninstall_registration_hashes" 2>/dev/null || true
        return 1
    }

    if [ ! -f "$BRORAY_LIGHTTPD_GUARD" ] || [ -L "$BRORAY_LIGHTTPD_GUARD" ] ||
       [ ! -x "$BRORAY_LIGHTTPD_GUARD" ] ||
       ! /opt/bin/ash "$BRORAY_LIGHTTPD_GUARD" uninstall-preflight >>"$BRORAY_LOG" 2>&1
    then
        broray_system_status_write "$operation_id" uninstall error preflight 100 \
            'Удаление отменено: исходное состояние Lighttpd нельзя восстановить однозначно.' \
            "$BRORAY_LIGHTTPD_GUARD"
        rm -f "$uninstall_snapshot" "$uninstall_bundles" \
            "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
            "$uninstall_services" "$uninstall_registration_hashes" 2>/dev/null || true
        return 1
    fi

    : >"$uninstall_registration_hashes" || return 1
    for registration_file in control preinst postinst prerm postrm list
    do
        registration_path="/opt/lib/opkg/info/broray.$registration_file"
        [ -f "$registration_path" ] && [ ! -L "$registration_path" ] || {
            rm -f "$uninstall_registration_hashes" 2>/dev/null || true
            return 1
        }
        sha256sum "$registration_path" >>"$uninstall_registration_hashes" || {
            rm -f "$uninstall_registration_hashes" 2>/dev/null || true
            return 1
        }
    done
    chmod 600 "$uninstall_registration_hashes" || {
        rm -f "$uninstall_registration_hashes" 2>/dev/null || true
        return 1
    }

    lifecycle_library="$BRORAY_BASE/lib/component-lifecycle.sh"
    if [ ! -f "$lifecycle_library" ] || [ -L "$lifecycle_library" ] ||
       [ ! -r "$lifecycle_library" ] || ! . "$lifecycle_library" ||
       ! command -v broray_lifecycle_routes_remove_all >/dev/null 2>&1 ||
       ! command -v broray_lifecycle_keenetic_delete >/dev/null 2>&1 ||
       ! command -v broray_lifecycle_web_publish_delete >/dev/null 2>&1 ||
       ! command -v broray_lifecycle_servers_deactivate >/dev/null 2>&1 ||
       ! command -v broray_lifecycle_xray_stop >/dev/null 2>&1
    then
        broray_system_status_write "$operation_id" uninstall error preflight 100 \
            'Удаление отменено: модуль безопасного удаления отсутствует или повреждён.' \
            "$lifecycle_library"
        rm -f "$uninstall_snapshot" "$uninstall_bundles" \
            "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
            "$uninstall_services" "$uninstall_registration_hashes" 2>/dev/null || true
        return 1
    fi

    broray_system_status_write "$operation_id" uninstall running backup 10 \
        'Сохраняются данные перед удалением.' ''

    if [ "$mode" = normal ]; then
        if [ -e "$preserved_dir" ] || [ -L "$preserved_dir" ]; then
            [ -d "$preserved_dir" ] && [ ! -L "$preserved_dir" ] || {
                broray_system_uninstall_abort backup 'Удаление отменено: небезопасный каталог сохранения.'
                return 1
            }
            preserved_dir_preexisting=true
            preserved_dir_original_mode="$(find -P "$preserved_dir" -maxdepth 0 -printf '%m' 2>/dev/null || true)"
            case "$preserved_dir_original_mode" in
                ''|*[!0-7]*)
                    broray_system_uninstall_abort backup 'Удаление отменено: права каталога сохранения неоднозначны.'
                    return 1
                    ;;
            esac
        else
            mkdir "$preserved_dir" || {
                broray_system_uninstall_abort backup 'Удаление отменено: каталог сохранения создать не удалось.'
                return 1
            }
        fi
        chmod 700 "$preserved_dir" || {
            broray_system_uninstall_abort backup 'Удаление отменено: права каталога сохранения не установлены.'
            return 1
        }
        stamp="$(date -u '+%Y%m%d-%H%M%S')"
        preserved_archive="$preserved_dir/broray-user-data-$stamp-$operation_id.tar.gz"
        for preserved_collision in \
            "$preserved_archive" "$preserved_archive.part.$$" \
            "$preserved_archive.sha256" "$preserved_archive.sha256.part" \
            "$preserved_archive.sha256.part.$$"
        do
            [ ! -e "$preserved_collision" ] && [ ! -L "$preserved_collision" ] || {
                broray_system_uninstall_abort backup 'Удаление отменено: имя архива сохранения уже занято.'
                return 1
            }
        done
        broray_system_protected_backup_create "$preserved_archive" >>"$BRORAY_LOG" 2>&1 || {
            broray_system_uninstall_abort backup 'Удаление отменено: проверяемый архив данных создать не удалось.'
            return 1
        }
        preserved_archive_owned=true
        preserved_name="${preserved_archive##*/}"
        (cd "$preserved_dir" && set -C && : >"$preserved_name.sha256.part") || {
            broray_system_uninstall_abort backup 'Удаление отменено: временная контрольная сумма не создана.'
            return 1
        }
        preserved_sidecar_part_owned=true
        [ -f "$preserved_archive.sha256.part" ] && [ ! -L "$preserved_archive.sha256.part" ] &&
        (cd "$preserved_dir" &&
            sha256sum "$preserved_name" >"$preserved_name.sha256.part") || {
            broray_system_uninstall_abort backup 'Удаление отменено: контрольная сумма архива не записана.'
            return 1
        }
        chmod 600 "$preserved_archive.sha256.part" || {
            broray_system_uninstall_abort backup 'Удаление отменено: контрольная сумма архива не защищена.'
            return 1
        }
        ln "$preserved_archive.sha256.part" "$preserved_archive.sha256" || {
            broray_system_uninstall_abort backup 'Удаление отменено: контрольная сумма архива не опубликована.'
            return 1
        }
        preserved_sidecar_owned=true
        rm -f "$preserved_archive.sha256.part" || {
            broray_system_uninstall_abort backup 'Удаление отменено: временная контрольная сумма не удалена.'
            return 1
        }
        preserved_sidecar_part_owned=false
        chmod 600 "$preserved_archive" "$preserved_archive.sha256" || {
            broray_system_uninstall_abort backup 'Удаление отменено: безопасные права архива не установлены.'
            return 1
        }
        sync
        broray_system_log "Пользовательские данные сохранены: $preserved_archive"
    fi

    # Close the preflight/backup race before the first Keenetic or service
    # mutation.  Only worker-owned archive objects may have appeared.
    if [ "$mode" = normal ] && ! broray_system_uninstall_latest_matches_preflight; then
        rm -f "$uninstall_snapshot" "$uninstall_bundles" \
            "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
            "$uninstall_services" "$uninstall_registration_hashes" 2>/dev/null || true
        broray_system_uninstall_preserved_rollback >/dev/null 2>&1 || true
        broray_system_status_write "$operation_id" uninstall error backup 100 \
            'Удаление отменено: LATEST изменился до начала мутаций.' \
            'Пакет, маршруты, Keenetic и сервисы не изменялись.' >/dev/null 2>&1 || true
        return 1
    fi

    # Every signal from this point until OPKG commit must restore the verified
    # snapshot, owned Keenetic objects, routes and prior service state.
    uninstall_mutation_started=true

    broray_system_status_write "$operation_id" uninstall running routes 25 \
        'Удаляются маршруты BROray.' ''
    broray_lifecycle_routes_remove_all >>"$BRORAY_LOG" 2>&1 || {
        broray_system_uninstall_abort routes 'Удаление маршрутов не завершено.'
        return 1
    }

    broray_system_status_write "$operation_id" uninstall running keenetic 38 \
        'Удаляется управляемый ProxyN.' ''
    broray_lifecycle_keenetic_delete >>"$BRORAY_LOG" 2>&1 || {
        broray_system_uninstall_abort keenetic 'Удаление управляемого ProxyN не завершено.'
        return 1
    }

    broray_system_status_write "$operation_id" uninstall running publish 48 \
        'Удаляется KeenDNS HTTP Proxy BROray.' ''
    broray_lifecycle_web_publish_delete >>"$BRORAY_LOG" 2>&1 || {
        broray_system_uninstall_abort publish 'Удаление KeenDNS HTTP Proxy не завершено.'
        return 1
    }

    broray_system_status_write "$operation_id" uninstall running servers 58 \
        'Отключается активный сервер.' ''
    broray_lifecycle_servers_deactivate >>"$BRORAY_LOG" 2>&1 || {
        broray_system_uninstall_abort servers 'Отключение активного сервера не завершено.'
        return 1
    }

    broray_system_status_write "$operation_id" uninstall running xray 68 \
        'Останавливается Xray.' ''
    broray_lifecycle_xray_stop >>"$BRORAY_LOG" 2>&1 || {
        broray_system_uninstall_abort xray 'Остановка Xray не завершена.'
        return 1
    }

    broray_system_status_write "$operation_id" uninstall running services 74 \
        'Останавливаются вспомогательные сервисы BROray.' ''
    broray_system_uninstall_aux_services_stop || {
        broray_system_uninstall_abort services 'Остановка сервисов BROray не завершена.'
        return 1
    }

    broray_system_status_write "$operation_id" uninstall running remove 82 \
        'Удаляется пакет BROray.' ''
    if [ "$mode" = normal ] && ! broray_system_uninstall_latest_matches_preflight; then
        broray_system_uninstall_abort authorization \
            'Удаление отменено: LATEST изменился до фиксации OPKG.'
        return 1
    fi
    [ ! -e "$BRORAY_OPKG_AUTH_ROOT" ] && [ ! -L "$BRORAY_OPKG_AUTH_ROOT" ] || {
        broray_system_uninstall_abort authorization 'Обнаружена небезопасная или оставшаяся авторизация OPKG.'
        return 1
    }
    mkdir -m 700 "$BRORAY_OPKG_AUTH_ROOT" || {
        broray_system_uninstall_abort authorization 'Не удалось подготовить авторизацию OPKG.'
        return 1
    }
    uninstall_auth_root_owned=true
    chmod 700 "$BRORAY_OPKG_AUTH_ROOT" || {
        broray_system_uninstall_abort authorization 'Не удалось защитить авторизацию OPKG.'
        return 1
    }
    authorization="$BRORAY_UNINSTALL_AUTH.new.$$"
    jq -n \
        --arg operationId "$operation_id" \
        --arg mode "$mode" \
        --argjson createdEpoch "$(date '+%s')" \
        '{schemaVersion:1,owner:"BROray-WebUI",operationId:$operationId,mode:$mode,createdEpoch:$createdEpoch}' \
        >"$authorization" || {
            rm -f "$authorization"
            broray_system_uninstall_abort authorization 'Не удалось сформировать авторизацию OPKG.'
            return 1
        }
    chmod 600 "$authorization" || {
        rm -f "$authorization"
        broray_system_uninstall_abort authorization 'Не удалось защитить авторизацию OPKG.'
        return 1
    }
    mv -f "$authorization" "$BRORAY_UNINSTALL_AUTH" || {
        rm -f "$authorization"
        broray_system_uninstall_abort authorization 'Не удалось опубликовать авторизацию OPKG.'
        return 1
    }

    # The OPKG commit window is deliberately non-interruptible by the three
    # cooperative service signals.  Ignored dispositions are inherited by
    # OPKG.  Because the IPK is metadata-only, postrm performs preflight only;
    # this copied worker removes the owned application payload after OPKG
    # registration/info absence has been proved.  Signals remain ignored
    # through that finalization, so cooperative interruption cannot expose a
    # half-removed installation. SIGKILL or power loss remains a physical gate.
    trap '' HUP INT TERM
    opkg_remove_rc=0
    BRORAY_UNINSTALL_OPERATION_ID="$operation_id" \
        opkg remove "$BRORAY_PACKAGE" >>"$BRORAY_LOG" 2>&1 || opkg_remove_rc=$?
    if [ "$opkg_remove_rc" -ne 0 ]; then
        trap 'broray_system_uninstall_signal 129' HUP
        trap 'broray_system_uninstall_signal 130' INT
        trap 'broray_system_uninstall_signal 143' TERM
        if broray_system_uninstall_registration_healthy; then
            broray_system_uninstall_abort remove 'OPKG не смог удалить BROray.'
        else
            broray_system_uninstall_registration_recovery_required \
                OPKG_NONZERO_REGISTRATION_AMBIGUOUS "$opkg_remove_rc" registration
        fi
        return 1
    fi

    # A zero OPKG exit alone is insufficient.  Registration and every info
    # object must be absent before the copied worker finalizes owned payload.
    remaining_status="$(opkg status "$BRORAY_PACKAGE" 2>/dev/null || true)"
    registration_absent=true
    [ -z "$remaining_status" ] || registration_absent=false
    for residue in /opt/lib/opkg/info/broray.*
    do
        [ ! -e "$residue" ] && [ ! -L "$residue" ] || registration_absent=false
    done
    if [ "$registration_absent" != true ]; then
        trap 'broray_system_uninstall_signal 129' HUP
        trap 'broray_system_uninstall_signal 130' INT
        trap 'broray_system_uninstall_signal 143' TERM
        broray_system_uninstall_registration_recovery_required \
            OPKG_ZERO_REGISTRATION_PRESENT 0 registration
        return 1
    fi

    uninstall_opkg_committed=true
    if ! /opt/bin/ash "$BRORAY_LIGHTTPD_GUARD" uninstall-restore >>"$BRORAY_LOG" 2>&1; then
        broray_system_uninstall_finalization_marker_write \
            LIGHTTPD_BASELINE_RESTORE_FAILED lighttpd-restore >/dev/null 2>&1 || true
        return 1
    fi
    rm -f "$BRORAY_UNINSTALL_AUTH" || return 1
    broray_system_uninstall_finalization_marker_write \
        FINALIZATION_IN_PROGRESS payload-finalize || return 1
    if ! broray_system_uninstall_payload_finalize; then
        broray_system_uninstall_finalization_marker_write \
            PAYLOAD_FINALIZATION_FAILED payload-finalize >/dev/null 2>&1 || true
        return 1
    fi
    uninstall_mutation_started=false

    for residue in \
        /opt/broray /opt/libexec/broray-updater /opt/bin/broray-updaterctl \
        /opt/var/lib/broray /opt/var/lib/broray-updater \
        /opt/etc/init.d/S22broray-updater /opt/etc/init.d/S23broray-monitor \
        /opt/etc/init.d/S24broray /opt/etc/init.d/S25broray-web \
        /opt/etc/init.d/S27broray-auto-switch /opt/etc/init.d/S28broray-subscriptions
    do
        [ ! -e "$residue" ] && [ ! -L "$residue" ] || return 1
    done
    for command_name in broray broray-routes broray-routes-dot broray-routes-user broray-server broray-servers broray-subscriptions broray-system
    do
        [ ! -e "/opt/bin/$command_name" ] && [ ! -L "/opt/bin/$command_name" ] || return 1
    done
    if [ "$mode" = normal ]; then
        [ -d "$preserved_dir" ] && [ ! -L "$preserved_dir" ] || return 1
        [ "$(find -P "$preserved_dir" -maxdepth 0 -printf '%m' 2>/dev/null)" = 700 ] || return 1
        broray_system_user_backup_valid "$preserved_archive" || return 1
        [ -f "$preserved_archive.sha256" ] && [ ! -L "$preserved_archive.sha256" ] || return 1
        (cd "$preserved_dir" && sha256sum -c "${preserved_archive##*/}.sha256") >/dev/null 2>&1 || return 1
        broray_system_uninstall_latest_publish "${preserved_archive##*/}" || return 1
        sync || return 1
    else
        [ ! -e "$preserved_dir" ] && [ ! -L "$preserved_dir" ] || return 1
    fi
    [ -d "$BRORAY_OPKG_AUTH_ROOT" ] && [ ! -L "$BRORAY_OPKG_AUTH_ROOT" ] || return 1
    rm -rf "$BRORAY_OPKG_AUTH_ROOT" || return 1
    uninstall_auth_root_owned=false
    [ ! -e "$BRORAY_OPKG_AUTH_ROOT" ] && [ ! -L "$BRORAY_OPKG_AUTH_ROOT" ] || return 1
    rm -f "$uninstall_snapshot" "$uninstall_bundles" \
        "$uninstall_dot_status" "$uninstall_dot_request" "$uninstall_dot_verify" \
        "$uninstall_services" "$uninstall_registration_hashes" 2>/dev/null || true
    trap - HUP INT TERM
    return 0
}


# ---------------------------------------------------------------------------
# BROray 3.0 canonical current-operation lifecycle WebUI adapter.
# The full package is downloaded only by the canonical engine after snapshot verification.
# ---------------------------------------------------------------------------



broray_system_release_index_fetch() {
    broray_release_target="$1"
    broray_release_part="$broray_release_target.part"
    broray_release_nonce="$(date '+%s')-$$"
    case "$BRORAY_RELEASE_INDEX_URL" in https://*) ;; *) return 1 ;; esac
    rm -f "$broray_release_part" "$broray_release_target"
    curl -fL --retry 3 --retry-delay 1 --connect-timeout 15 --max-time 90       -H 'Accept: application/json' -H 'Accept-Encoding: identity'       -H 'Cache-Control: no-cache, no-store, max-age=0' -H 'Pragma: no-cache'       -o "$broray_release_part" "${BRORAY_RELEASE_INDEX_URL}?broray=$broray_release_nonce" >>"$BRORAY_LOG" 2>&1 || return 1
    jq -e '
      def broray_sha256:
        ((type == "string") and (length == 64) and
         all(explode[]; ((. >= 48) and (. <= 57)) or ((. >= 97) and (. <= 102))));
      (type == "object") and (.schemaVersion == 3) and
      (.lifecycleContract == "current-operation-full-tmp-snapshot/1") and
      (.requirementsContract == "1.7.2") and
      (.capabilityContract == "keenetic-entware-capabilities/1") and
      (.spaceContract == "broray-space/2") and
      (.previousIpkRequired == false) and (.historicalTransactionStateRequired == false) and
      (.statelessBootstrap == true) and
      ((.candidate | type) == "object") and
      (((.candidate.packageVersion | type) == "string") and ((.candidate.packageVersion | length) > 0)) and
      (((.candidate.appVersion | type) == "string") and ((.candidate.appVersion | length) > 0)) and
      (((.candidate.architecture | type) == "string") and ((.candidate.architecture | length) > 0)) and
      (((.candidate.filename | type) == "string") and ((.candidate.filename | length) > 0)) and
      (.candidate.sha256 | broray_sha256) and
      (((.candidate.sizeBytes | type) == "number") and ((.candidate.sizeBytes | floor) == .candidate.sizeBytes) and (.candidate.sizeBytes > 0)) and
      (((.candidate.baseUrl | type) == "string") and (.candidate.baseUrl | startswith("https://"))) and
      (((.candidate.releaseId | type) == "string") and ((.candidate.releaseId | length) > 0)) and
      (((.candidate.webUIBuild | type) == "string") and ((.candidate.webUIBuild | length) > 0)) and
      (.candidate.requirementsContract == .requirementsContract) and
      (.candidate.lifecycleContract == .lifecycleContract) and
      (.candidate.capabilityContract == .capabilityContract) and (.candidate.spaceContract == .spaceContract) and
      (.candidate.previousIpkRequired == false) and
      (.candidate.historicalTransactionStateRequired == false) and (.candidate.statelessBootstrap == true) and
      ((.opkgEntry | type) == "object") and
      (.opkgEntry.filename == .candidate.filename) and (.opkgEntry.sha256 == .candidate.sha256) and
      (.opkgEntry.sizeBytes == .candidate.sizeBytes) and
      ((.opkgEntry | del(.distributionRole,.metadataOnlyRegistration,.directOpkgMutation)) ==
       (.candidate | del(.distributionRole))) and
      (.opkgEntry.distributionRole == "canonical-full-candidate") and
      (.opkgEntry.metadataOnlyRegistration == true) and (.opkgEntry.directOpkgMutation == "fail-closed")
    ' "$broray_release_part" >/dev/null 2>&1 || { rm -f "$broray_release_part"; return 1; }
    mv -f "$broray_release_part" "$broray_release_target"
}



broray_system_release_index_try_canonical() {
    broray_release_current_file="$1"
    broray_release_canonical_url="$(jq -r '.canonicalIndexUrl // ""' "$broray_release_current_file" 2>/dev/null || true)"
    case "$broray_release_canonical_url" in https://*) ;; *) return 0 ;; esac
    [ "$broray_release_canonical_url" != "$BRORAY_RELEASE_INDEX_URL" ] || return 0
    broray_release_current_id="$(jq -r '.releaseId // ""' "$broray_release_current_file" 2>/dev/null || true)"
    broray_release_old_url="$BRORAY_RELEASE_INDEX_URL"
    broray_release_canonical_file="$BRORAY_RUN/release-index-canonical.json"
    BRORAY_RELEASE_INDEX_URL="$broray_release_canonical_url"
    if broray_system_release_index_fetch "$broray_release_canonical_file" &&
       [ "$(jq -r '.releaseId // ""' "$broray_release_canonical_file" 2>/dev/null || true)" = "$broray_release_current_id" ]; then
        mv -f "$broray_release_canonical_file" "$broray_release_current_file" || return 1
        rm -f "$BRORAY_RELEASE_INDEX_URL_FILE" 2>/dev/null || true
        broray_system_log "Кандидат опубликован в canonical release index; staging override удалён."
        return 0
    fi
    rm -f "$broray_release_canonical_file" 2>/dev/null || true
    BRORAY_RELEASE_INDEX_URL="$broray_release_old_url"
    return 0
}

broray_system_update_check_internal() {
    broray_system_log 'Загружается read-only release.json; полный candidate здесь не скачивается.'
    broray_release_file="$BRORAY_RUN/release-index.json"
    broray_system_release_index_fetch "$broray_release_file" || return 1
    broray_system_release_index_try_canonical "$broray_release_file" || return 1
    source_json="$(broray_system_current_identity_json)" || return 1
    candidate_json="$(jq -ce '.candidate' "$broray_release_file")" || return 1
    opkg_entry_json="$(jq -ce '.opkgEntry' "$broray_release_file")" || return 1
    installed_arch="$(printf '%s
' "$source_json" | jq -r '.configuredArchitecture')"
    [ "$(printf '%s
' "$candidate_json" | jq -r '.architecture')" = "$installed_arch" ] || return 1
    update_available=true
    if [ "$(printf '%s
' "$source_json" | jq -r '.candidateSha256 // ""')" = "$(printf '%s
' "$candidate_json" | jq -r '.sha256')" ]; then
        update_available=false
    fi
    checked_at="$(broray_system_now)"; checked_epoch="$(date '+%s')"
    jq -nc       --arg currentVersion "$(printf '%s
' "$source_json" | jq -r '.appVersion')"       --arg availableVersion "$(printf '%s
' "$candidate_json" | jq -r '.appVersion')"       --arg availablePackageVersion "$(printf '%s
' "$candidate_json" | jq -r '.packageVersion')"       --arg checkedAt "$checked_at" --argjson checkedEpoch "$checked_epoch"       --argjson updateAvailable "$update_available" --argjson source "$source_json"       --argjson candidate "$candidate_json" --argjson opkgEntry "$opkg_entry_json" '
      {schemaVersion:6,requirementsContract:"1.7.2",capabilityContract:"keenetic-entware-capabilities/1",
       spaceContract:"broray-space/2",lifecycleContract:"current-operation-full-tmp-snapshot/1",
       distribution:"release-json",ok:true,currentVersion:$currentVersion,availableVersion:$availableVersion,
       availablePackageVersion:$availablePackageVersion,updateAvailable:$updateAvailable,
       checkedAt:$checkedAt,checkedEpoch:$checkedEpoch,source:$source,candidate:$candidate,opkgEntry:$opkgEntry,
       previousIpkRequired:false,historicalTransactionStateRequired:false,statelessBootstrap:true}
    ' | broray_system_atomic_json "$BRORAY_UPDATE_CACHE" || return 1
    broray_system_update_cache_read >/dev/null 2>&1
}

broray_system_transaction_metadata_write() {
    operation_id="$1"; mode="$2"; output="$3"
    source_json="$(broray_system_current_identity_json)" || return 1
    case "$mode" in
      update)
        release_json="$(broray_system_update_cache_read)" || return 1
        [ "$(printf '%s
' "$release_json" | jq -r '.updateAvailable')" = true ] || return 1
        now_epoch="$(date '+%s')"; checked_epoch="$(printf '%s
' "$release_json" | jq -r '.checkedEpoch // 0')"
        case "$checked_epoch" in ''|*[!0-9]*) return 1 ;; esac
        [ "$now_epoch" -ge "$checked_epoch" ] && [ $((now_epoch-checked_epoch)) -le 3600 ] || return 1
        ;;
      reinstall)
        broray_release_file="$BRORAY_RUN/release-index.json"
        broray_system_release_index_fetch "$broray_release_file" || return 1
        broray_system_release_index_try_canonical "$broray_release_file" || return 1
        release_json="$(cat "$broray_release_file")" || return 1
        ;;
      *) return 1 ;;
    esac
    candidate_json="$(printf '%s
' "$release_json" | jq -ce '.candidate')" || return 1
    opkg_entry_json="$(printf '%s
' "$release_json" | jq -ce '.opkgEntry')" || return 1
    [ "$(printf '%s
' "$candidate_json" | jq -r '.architecture')" = "$(printf '%s
' "$source_json" | jq -r '.configuredArchitecture')" ] || return 1
    case "$mode" in
      reinstall)
        [ "$(printf '%s
' "$source_json" | jq -r '.candidateSha256 // ""')" = "$(printf '%s
' "$candidate_json" | jq -r '.sha256')" ] || return 1
        ;;
    esac
    jq -nc --arg operationId "$operation_id" --arg mode "$mode"       --argjson source "$source_json" --argjson candidate "$candidate_json" --argjson opkgEntry "$opkg_entry_json" '
      {schemaVersion:4,requirementsContract:"1.7.2",capabilityContract:"keenetic-entware-capabilities/1",
       spaceContract:"broray-space/2",lifecycleContract:"current-operation-full-tmp-snapshot/1",
       operationId:$operationId,mode:$mode,source:$source,candidate:$candidate,opkgEntry:$opkgEntry,
       previousIpkRequired:false,historicalTransactionStateRequired:false,statelessBootstrap:true}
    ' >"$output" || return 1
    jq -e --arg id "$operation_id" --arg mode "$mode" '
      (.operationId == $id) and (.mode == $mode) and (.requirementsContract == "1.7.2") and
      (.capabilityContract == "keenetic-entware-capabilities/1") and (.spaceContract == "broray-space/2") and
      (.lifecycleContract == "current-operation-full-tmp-snapshot/1") and
      (.previousIpkRequired == false) and (.historicalTransactionStateRequired == false) and
      (.statelessBootstrap == true) and
      ((.candidate | type) == "object") and ((.opkgEntry | type) == "object") and
      (.candidate.requirementsContract == .requirementsContract) and
      (.candidate.lifecycleContract == .lifecycleContract) and
      (.candidate.capabilityContract == .capabilityContract) and (.candidate.spaceContract == .spaceContract) and
      (.candidate.previousIpkRequired == false) and
      (.candidate.historicalTransactionStateRequired == false) and (.candidate.statelessBootstrap == true) and
      ((.opkgEntry | del(.distributionRole,.metadataOnlyRegistration,.directOpkgMutation)) ==
       (.candidate | del(.distributionRole))) and
      (.opkgEntry.distributionRole == "canonical-full-candidate") and
      (.opkgEntry.metadataOnlyRegistration == true) and (.opkgEntry.directOpkgMutation == "fail-closed")
    ' "$output" >/dev/null 2>&1
}



broray_system_worker_update() {
    return 1
}

broray_system_worker_reinstall() {
    return 1
}

broray_system_main() {
    command_name="${1:-}"
    shift 2>/dev/null || true

    case "$command_name" in
        info)
            broray_system_info_json
            ;;
        status)
            /opt/bin/broray-updaterctl status
            ;;
        update-check)
            /opt/bin/broray-updaterctl check
            ;;
        update-start)
            /opt/bin/broray-updaterctl request update
            ;;
        reinstall-start)
            /opt/bin/broray-updaterctl request reinstall
            ;;
        restore-start)
            broray_system_error_json RESTORE_UNAVAILABLE 'Восстановление резервной копии недоступно для компактной схемы.'
            return 1
            ;;
        uninstall-start)
            broray_system_uninstall_start "${1:-}" "${2:-}"
            ;;
        worker-uninstall)
            operation_id="${1:-}"
            mode="${2:-}"
            handoff="${3:-}"
            BRORAY_WORKER_ACCEPTANCE_FILE="${4:-}"
            export BRORAY_WORKER_ACCEPTANCE_FILE
            worker_bin="$0"
            worker_lib="${BRORAY_SYSTEM_LIB:-$BRORAY_BASE/lib/broray-page.sh}"
            result=0
            broray_system_global_lock_worker_adopt "$operation_id" "$handoff" || return 1
            if ! broray_system_select_operation "$operation_id"; then
                result=1
            elif ! broray_system_worker_acceptance_publish "$operation_id" uninstall; then
                broray_system_status_write \
                    "$operation_id" uninstall error acceptance 100 \
                    'Операция отклонена до изменения пакета.' \
                    'Не удалось атомарно опубликовать worker acceptance receipt.' >/dev/null 2>&1 || true
                result=1
            else
                broray_system_worker_uninstall "$operation_id" "$mode" || result=$?
            fi
            if [ "$result" -ne 0 ] && [ -s "$BRORAY_STATUS" ]; then
                if jq -e '.running == true' "$BRORAY_STATUS" >/dev/null 2>&1; then
                    operation="$(jq -r '.operation // "operation"' "$BRORAY_STATUS")"
                    broray_system_status_write \
                        "$operation_id" "$operation" error failed 100 \
                        'Операция завершилась ошибкой.' \
                        'Подробности сохранены в техническом журнале.'
                fi
            fi
            if ! broray_system_worker_finish "$worker_bin" "$worker_lib"; then
                [ "$result" -ne 0 ] || result=1
            fi
            return "$result"
            ;;
        *)
            printf '%s\n' 'Использование: broray-system {info|status|update-check|update-start|reinstall-start|restore-start|uninstall-start}' >&2
            return 2
            ;;
    esac
}
