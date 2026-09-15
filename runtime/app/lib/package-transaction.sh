#!/opt/bin/ash
# BROray 3.0.0-r14 canonical current-operation lifecycle.
#
# Historical transaction artifacts are not update inputs, but a global lock
# is always classified before a new writer starts.  A fully proven stale
# rollback-failed operation is recovered natively; live or ambiguous state is
# rejected without removing anything.  The only rollback source for a new
# operation is backup.tar.gz created and verified by that exact operation.

BRORAY_TX_TARGET_RELEASE="3.0.0-r14"
BRORAY_TX_TARGET_PACKAGE="3.0.0-r14"
BRORAY_TX_TARGET_APP="3.0.0"
BRORAY_TX_TARGET_WEBUI="WebUI-3.0.0-r15c16"
BRORAY_TX_TARGET_ARCH="aarch64-3.10"
BRORAY_TX_TARGET_REVISION="14"
BRORAY_TX_TARGET_FILENAME="broray_3.0.0-r14_aarch64-3.10.ipk"
BRORAY_TX_TARGET_PACKAGE_BASE_URL="https://api.brovibe.cloud/releases/staging/broray/3.0.0-r15c16/opkg/aarch64-3.10"
BRORAY_TX_RELEASE_URL_DEFAULT="https://api.brovibe.cloud/releases/staging/broray/3.0.0-r15c16/release.json"
BRORAY_TX_CONTRACT="current-operation-full-tmp-snapshot/1"
BRORAY_TX_REQUIREMENTS_CONTRACT="1.7.2"

if [ -z "${BRORAY_TX_FS_ROOT+x}" ]; then
    case "${PKG_ROOT:-/}" in /) BRORAY_TX_FS_ROOT=/ ;; *) BRORAY_TX_FS_ROOT="${PKG_ROOT%/}" ;; esac
fi
BRORAY_TX_FS_ROOT="${BRORAY_TX_FS_ROOT:-/}"
case "$BRORAY_TX_FS_ROOT" in /) BRORAY_TX_PREFIX="" ;; /*) BRORAY_TX_PREFIX="${BRORAY_TX_FS_ROOT%/}" ;; *) exit 2 ;; esac

broray_tx_root_path()
{
    case "$1" in /*) printf '%s%s\n' "$BRORAY_TX_PREFIX" "$1" ;; *) return 1 ;; esac
}

BRORAY_TX_OPT_ROOT="${BRORAY_TX_OPT_ROOT:-$(broray_tx_root_path /opt)}"
BRORAY_TX_TMP_BASE="${BRORAY_TX_TMP_BASE:-$(broray_tx_root_path /tmp)}"
BRORAY_TX_APP_ROOT="${BRORAY_TX_APP_ROOT:-$BRORAY_TX_OPT_ROOT/broray}"
BRORAY_TX_STATE_ROOT="${BRORAY_TX_STATE_ROOT:-$BRORAY_TX_OPT_ROOT/var/lib/broray}"
BRORAY_TX_INFO_ROOT="${BRORAY_TX_INFO_ROOT:-$BRORAY_TX_OPT_ROOT/lib/opkg/info}"
BRORAY_TX_STATUS_FILE="${BRORAY_TX_STATUS_FILE:-$BRORAY_TX_OPT_ROOT/lib/opkg/status}"
BRORAY_TX_OPKG="${BRORAY_TX_OPKG:-opkg}"
BRORAY_TX_OPKG_CONF="${BRORAY_TX_OPKG_CONF:-$BRORAY_TX_OPT_ROOT/etc/opkg.conf}"
BRORAY_TX_ASH="${BRORAY_TX_ASH:-$BRORAY_TX_OPT_ROOT/bin/ash}"
BRORAY_TX_RELEASE_URL="${BRORAY_TX_RELEASE_URL:-$BRORAY_TX_RELEASE_URL_DEFAULT}"
BRORAY_TX_CURRENT="${BRORAY_TX_CURRENT:-$BRORAY_TX_TMP_BASE/.broray-current-operation}"
BRORAY_TX_LOCK_DIR="${BRORAY_TX_LOCK_DIR:-$BRORAY_TX_TMP_BASE/broray-update.lock}"
BRORAY_TX_GLOBAL_LOCK="${BRORAY_TX_GLOBAL_LOCK:-$BRORAY_TX_TMP_BASE/broray-global-operation.lock}"
BRORAY_TX_LEGACY_MARKER="${BRORAY_TX_LEGACY_MARKER:-$BRORAY_TX_STATE_ROOT/current-operation.json}"
BRORAY_TX_OPERATION_ROOT="${BRORAY_TX_OPERATION_ROOT:-$BRORAY_TX_STATE_ROOT/operations}"
BRORAY_TX_PROC_ROOT="${BRORAY_TX_PROC_ROOT:-/proc}"
# Global transaction ownership is deliberately independent from the procfs
# fixture used by the OPKG/service capability probes.  Production always
# binds ownership to the kernel's real process identity.  Isolated tests may
# provide a complete fake proc tree in order to exercise PID-reuse branches.
BRORAY_TX_CONTROL_PROC_ROOT="${BRORAY_TX_CONTROL_PROC_ROOT:-/proc}"
# Resource terms are either exact candidate metadata or explicit bounded
# writers/reserves.  They are not source-version budgets and are not
# environment-overridable in production.
BRORAY_TX_CANDIDATE_FORMAT_MAX_BYTES=67108864
BRORAY_TX_METADATA_MAX_BYTES=1048576
BRORAY_TX_EVIDENCE_GROWTH_CAP_KB=4096
BRORAY_TX_SETUP_GROWTH_CAP_KB=2048
BRORAY_TX_SETUP_OUTPUT_EACH_CAP_KB=2048
BRORAY_TX_SETUP_FILE_LIMIT_BLOCKS512=64
BRORAY_TX_SETUP_WRITABLE_REGULAR_CAP=64
BRORAY_TX_SETUP_WRITE_CONTRACT='candidate-bound-preserve-no-services-max64x32KiB/1'
BRORAY_TX_DURABLE_EVIDENCE_CAP_KB=256
BRORAY_TX_DURABLE_EVIDENCE_FILE_CAP=7
BRORAY_TX_DURABLE_EVIDENCE_INODE_CAP=8
BRORAY_TX_FIELD_DIAGNOSTIC_TOTAL_MAX_BYTES=2097152
BRORAY_TX_FIELD_DIAGNOSTIC_FILE_MAX_BYTES=1048576
BRORAY_TX_FIELD_DIAGNOSTIC_FILE_MAX_COUNT=40
BRORAY_TX_CLEANUP_PLAN_CAP_KB=112
BRORAY_TX_TMP_RESERVE_KB=16384
BRORAY_TX_OPT_RESERVE_KB=8192
BRORAY_TX_TMP_INODE_RESERVE=256
BRORAY_TX_OPT_INODE_RESERVE=128
BRORAY_TX_SETUP_INODE_CAP=64
BRORAY_TX_OPKG_METADATA_INODE_CAP=16
BRORAY_TX_SNAPSHOT_FIXED_FUTURE_INODES=64
BRORAY_TX_CANDIDATE_FIXED_FUTURE_INODES=64
BRORAY_TX_POST_CANDIDATE_FUTURE_INODES=64
BRORAY_TX_WORK=""
BRORAY_TX_OPERATION_ID=""
BRORAY_TX_MODE="update"
BRORAY_TX_ORIGIN="${BRORAY_TX_ORIGIN:-opkg}"
BRORAY_TX_STAGE="initialization"
BRORAY_TX_REASON=""
BRORAY_TX_MUTATED=0
BRORAY_TX_LOCK_HELD=0
BRORAY_TX_NATIVE_OPKG_LOCK_HELD=0
BRORAY_TX_NATIVE_OPKG_LOCK_WORK=""
BRORAY_TX_NATIVE_OPKG_LOCK_FIFO_ARGV=""
BRORAY_TX_NATIVE_OPKG_LOCK_PATH=""
BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID=""
BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_STARTTIME=""
BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_CMDLINE_SHA256=""
BRORAY_TX_NATIVE_OPKG_LOCK_TYPE=""
BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=""
BRORAY_TX_CONTROL_FAILURE_REASON=""
BRORAY_TX_CONTROL_FAILURE_EVIDENCE=""
BRORAY_TX_SOURCE_PACKAGE=""
BRORAY_TX_SOURCE_APP=""
BRORAY_TX_SOURCE_CLASS=""
BRORAY_TX_MIGRATION_ID=""
BRORAY_TX_TRAP_ACTIVE=0
BRORAY_TX_FAILURE_RUNNING=0
BRORAY_TX_HANDOFF_CHILD=0
BRORAY_TX_RECOVERY_FRESH_CONTROL=0
BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE=""
BRORAY_TX_CONTROL_MUTEX_HELD=0
BRORAY_TX_CONTROL_MUTEX_WORK=""
BRORAY_TX_CONTROL_MUTEX_OWNER_PID=""
BRORAY_TX_CONTROL_MUTEX_OWNER_STARTTIME=""
BRORAY_TX_CONTROL_TRANSITION_OWNS_MUTEX=0
BRORAY_TX_CONTROL_TRANSITION_DEPTH=0
BRORAY_TX_CONTROL_TRANSITION_ROOT_OWNS_MUTEX=0
BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_STATE=""
BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_LOCK_IDENTITY=""
BRORAY_TX_RECOVERY_PREDECESSOR_OWNER_IDENTITY=""
BRORAY_TX_RECOVERY_PREDECESSOR_PROOF=""
BRORAY_TX_RECOVERY_CAPTURE_PREDECESSOR=0
BRORAY_TX_TEST_CONTROL_PROCESS_NONCE="$$-${PPID:-0}-$(date '+%s')-${RANDOM:-0}-${RANDOM:-0}"

# Test measurements are accepted only in an isolated non-production root.
# Merely exporting a BRORAY_TX_TEST_* variable can never bypass a physical
# `df` gate.
if [ "${BRORAY_TX_TEST_MODE:-0}" != 1 ] || [ "$BRORAY_TX_FS_ROOT" = / ]; then
    unset BRORAY_TX_TEST_OPT_FREE_KB BRORAY_TX_TEST_TMP_FREE_KB BRORAY_TX_TEST_TMP_TOTAL_KB
    unset BRORAY_TX_TEST_OPT_FREE_INODES BRORAY_TX_TEST_TMP_FREE_INODES
    unset BRORAY_TX_TEST_TMP_FREE_AFTER_SNAPSHOT_KB BRORAY_TX_TEST_TMP_FREE_AFTER_SNAPSHOT_INODES
    unset BRORAY_TX_TEST_TMP_FREE_AFTER_CANDIDATE_KB BRORAY_TX_TEST_TMP_FREE_AFTER_CANDIDATE_INODES
    unset BRORAY_TX_TEST_OPT_FREE_BEFORE_MUTATION_KB BRORAY_TX_TEST_TMP_FREE_BEFORE_MUTATION_KB
    unset BRORAY_TX_TEST_OPT_FREE_BEFORE_MUTATION_INODES BRORAY_TX_TEST_TMP_FREE_BEFORE_MUTATION_INODES
    unset BRORAY_TX_TEST_OPT_FREE_AFTER_SOURCE_DELETE_KB BRORAY_TX_TEST_OPT_FREE_AFTER_SOURCE_DELETE_INODES
    unset BRORAY_TX_TEST_FORCE_MOUNT_DRIFT BRORAY_TX_TEST_FORCE_TMP_CAP_FAILURE
    unset BRORAY_TX_TEST_SERVICE_IDENTITY
    unset BRORAY_TX_TEST_FAIL_STAGE BRORAY_TX_TEST_DROP_SNAPSHOT_SIDECARS_ON_FAILURE
    unset BRORAY_TX_TEST_PAUSE_STAGE
    unset BRORAY_TX_TEST_CONTROL_PROC_PROVIDER
    BRORAY_TX_PROC_ROOT=/proc
    BRORAY_TX_CONTROL_PROC_ROOT=/proc
fi

BRORAY_RUNTIME_CAPABILITY_LIB="${BRORAY_RUNTIME_CAPABILITY_LIB:-}"
if [ -z "$BRORAY_RUNTIME_CAPABILITY_LIB" ]; then
    for broray_tx_capability_candidate in \
        "${0%/*}/runtime-capabilities.sh" \
        "$BRORAY_TX_APP_ROOT/lib/runtime-capabilities.sh"
    do
        if [ -f "$broray_tx_capability_candidate" ] && [ ! -L "$broray_tx_capability_candidate" ]; then
            BRORAY_RUNTIME_CAPABILITY_LIB="$broray_tx_capability_candidate"
            break
        fi
    done
fi
[ -n "$BRORAY_RUNTIME_CAPABILITY_LIB" ] || {
    printf '%s\n' 'BROray: runtime capability contract is missing' >&2
    exit 2
}
. "$BRORAY_RUNTIME_CAPABILITY_LIB" || exit 2
broray_runtime_path_init || exit 2
# A freshly started engine has no resolved paths yet.  An engine adopted by
# the package hand-off inherits the paths proved by the preflight.  Activate
# those exact tools immediately so BusyBox ash applets cannot silently replace
# the proved Entware/GNU behavior (notably find -printf).
broray_runtime_activate_if_resolved || exit 2

broray_tx_sha()
{
    sha256sum "$1" 2>/dev/null | awk 'NR==1{print $1;exit}'
}

# Read Linux /proc/PID/stat field 22 without trusting the parenthesized comm
# field (which may itself contain spaces or parentheses).  The returned birth
# token is immutable for the lifetime of a process and therefore distinguishes
# a live owner from an unrelated process that later reused the same PID.
broray_tx_proc_starttime()
{
    broray_tx_proc_start_root="$1"
    broray_tx_proc_start_pid="$2"
    case "$broray_tx_proc_start_pid" in ''|*[!0-9]*|0) return 1 ;; esac
    [ -r "$broray_tx_proc_start_root/$broray_tx_proc_start_pid/stat" ] || return 1
    broray_tx_proc_start_value="$(awk '
      NR==1 {
        line=$0
        sub(/^.*\) /,"",line)
        n=split(line,field," ")
        if (n>=20 && field[20]~/^[0-9]+$/ && field[20]!="0") print field[20]
      }
    ' "$broray_tx_proc_start_root/$broray_tx_proc_start_pid/stat" 2>/dev/null)" || return 1
    case "$broray_tx_proc_start_value" in ''|*[!0-9]*|0) return 1 ;; esac
    printf '%s\n' "$broray_tx_proc_start_value"
}

# Capture one exact, single-file owner identity.  A separate PID file would
# require a two-file update during package hand-off and create a crash window
# in which recovery could not tell which process owns the lock.  This TSV is
# the sole authoritative owner record and is replaced with one atomic rename.
broray_tx_control_owner_identity_capture()
{
    broray_tx_control_capture_pid="$1"
    broray_tx_control_capture_output="$2"
    case "$broray_tx_control_capture_pid" in ''|*[!0-9]*|0) return 1 ;; esac
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ] &&
       [ -n "${BRORAY_TX_TEST_CONTROL_PROC_PROVIDER:-}" ]; then
        [ -x "$BRORAY_TX_TEST_CONTROL_PROC_PROVIDER" ] &&
            [ ! -L "$BRORAY_TX_TEST_CONTROL_PROC_PROVIDER" ] || return 1
        "$BRORAY_TX_TEST_CONTROL_PROC_PROVIDER" \
            "$BRORAY_TX_CONTROL_PROC_ROOT" "$broray_tx_control_capture_pid" \
            "$BRORAY_TX_TEST_CONTROL_PROCESS_NONCE" "${BRORAY_TX_ASH:-/opt/bin/ash}" || return 1
    fi
    broray_tx_control_capture_start_1="$(broray_tx_proc_starttime "$BRORAY_TX_CONTROL_PROC_ROOT" "$broray_tx_control_capture_pid")" || return 1
    broray_tx_control_capture_exe_1="$(readlink -f "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_capture_pid/exe" 2>/dev/null)" || return 1
    case "$broray_tx_control_capture_exe_1" in /*) ;; *) return 1 ;; esac
    [ -r "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_capture_pid/cmdline" ] || return 1
    broray_tx_control_capture_cmdline_bytes="$(wc -c <"$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_capture_pid/cmdline" 2>/dev/null | tr -d ' ')" || return 1
    broray_tx_number "$broray_tx_control_capture_cmdline_bytes" &&
        [ "$broray_tx_control_capture_cmdline_bytes" -gt 0 ] || return 1
    broray_tx_control_capture_cmdline_1="$(broray_tx_sha "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_capture_pid/cmdline")" || return 1
    case "$broray_tx_control_capture_cmdline_1" in *[!0-9a-f]*) return 1 ;; esac
    [ "${#broray_tx_control_capture_cmdline_1}" -eq 64 ] || return 1
    broray_tx_control_capture_start_2="$(broray_tx_proc_starttime "$BRORAY_TX_CONTROL_PROC_ROOT" "$broray_tx_control_capture_pid")" || return 1
    broray_tx_control_capture_exe_2="$(readlink -f "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_capture_pid/exe" 2>/dev/null)" || return 1
    broray_tx_control_capture_cmdline_2="$(broray_tx_sha "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_capture_pid/cmdline")" || return 1
    [ "$broray_tx_control_capture_start_1" = "$broray_tx_control_capture_start_2" ] &&
    [ "$broray_tx_control_capture_exe_1" = "$broray_tx_control_capture_exe_2" ] &&
    [ "$broray_tx_control_capture_cmdline_1" = "$broray_tx_control_capture_cmdline_2" ] || return 1
    # Tabs/newlines cannot be represented in this exact four-column record.
    [ "$(printf '%s' "$broray_tx_control_capture_exe_1" | awk 'BEGIN{ok=1} /\t/{ok=0} NR>1{ok=0} END{print ok}')" = 1 ] || return 1
    printf '%s\t%s\t%s\t%s\n' \
        "$broray_tx_control_capture_pid" "$broray_tx_control_capture_start_1" \
        "$broray_tx_control_capture_exe_1" "$broray_tx_control_capture_cmdline_1" \
        >"$broray_tx_control_capture_output"
}

broray_tx_control_owner_identity_read()
{
    broray_tx_control_identity_file="$1"
    [ -f "$broray_tx_control_identity_file" ] && [ ! -L "$broray_tx_control_identity_file" ] || return 1
    [ "$(wc -l <"$broray_tx_control_identity_file" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
    awk -F '\t' '
      NF==4 && $1~/^[0-9]+$/ && $1!="0" && $2~/^[0-9]+$/ && $2!="0" &&
      $3~/^\// && $4~/^[0-9a-f]+$/ && length($4)==64 { ok=1 }
      END { exit ok ? 0 : 1 }
    ' "$broray_tx_control_identity_file" 2>/dev/null || return 1
    BRORAY_TX_CONTROL_OWNER_PID="$(awk -F '\t' 'NR==1{print $1}' "$broray_tx_control_identity_file")"
    BRORAY_TX_CONTROL_OWNER_STARTTIME="$(awk -F '\t' 'NR==1{print $2}' "$broray_tx_control_identity_file")"
    BRORAY_TX_CONTROL_OWNER_EXE="$(awk -F '\t' 'NR==1{print $3}' "$broray_tx_control_identity_file")"
    BRORAY_TX_CONTROL_OWNER_CMDLINE_SHA256="$(awk -F '\t' 'NR==1{print $4}' "$broray_tx_control_identity_file")"
}

# Set BRORAY_TX_CONTROL_OWNER_STATE to live, dead, reused or ambiguous.
# Only dead/reused authorize stale recovery; live/ambiguous always preserve
# control state.  No signal is ever sent on the basis of this classification.
broray_tx_control_owner_classify()
{
    BRORAY_TX_CONTROL_OWNER_STATE=ambiguous
    broray_tx_control_owner_identity_read "$1" || return 1
    broray_tx_control_class_pid="$BRORAY_TX_CONTROL_OWNER_PID"
    broray_tx_control_class_start_1="$(broray_tx_proc_starttime "$BRORAY_TX_CONTROL_PROC_ROOT" "$broray_tx_control_class_pid" 2>/dev/null)" ||
        broray_tx_control_class_start_1=""
    if [ -z "$broray_tx_control_class_start_1" ]; then
        if [ ! -e "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_class_pid" ] &&
           [ ! -L "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_class_pid" ] &&
           ! kill -0 "$broray_tx_control_class_pid" 2>/dev/null; then
            BRORAY_TX_CONTROL_OWNER_STATE=dead
            return 0
        fi
        return 0
    fi
    broray_tx_control_class_start_2="$(broray_tx_proc_starttime "$BRORAY_TX_CONTROL_PROC_ROOT" "$broray_tx_control_class_pid" 2>/dev/null)" ||
        broray_tx_control_class_start_2=""
    [ -n "$broray_tx_control_class_start_2" ] &&
        [ "$broray_tx_control_class_start_1" = "$broray_tx_control_class_start_2" ] || return 0
    if [ "$broray_tx_control_class_start_1" != "$BRORAY_TX_CONTROL_OWNER_STARTTIME" ]; then
        BRORAY_TX_CONTROL_OWNER_STATE=reused
        return 0
    fi
    broray_tx_control_class_exe_1="$(readlink -f "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_class_pid/exe" 2>/dev/null)" || return 0
    broray_tx_control_class_cmdline_1="$(broray_tx_sha "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_class_pid/cmdline")" || return 0
    broray_tx_control_class_start_3="$(broray_tx_proc_starttime "$BRORAY_TX_CONTROL_PROC_ROOT" "$broray_tx_control_class_pid" 2>/dev/null)" || return 0
    broray_tx_control_class_exe_2="$(readlink -f "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_class_pid/exe" 2>/dev/null)" || return 0
    broray_tx_control_class_cmdline_2="$(broray_tx_sha "$BRORAY_TX_CONTROL_PROC_ROOT/$broray_tx_control_class_pid/cmdline")" || return 0
    [ "$broray_tx_control_class_start_1" = "$broray_tx_control_class_start_3" ] &&
    [ "$broray_tx_control_class_exe_1" = "$broray_tx_control_class_exe_2" ] &&
    [ "$broray_tx_control_class_cmdline_1" = "$broray_tx_control_class_cmdline_2" ] || return 0
    if [ "$broray_tx_control_class_exe_1" = "$BRORAY_TX_CONTROL_OWNER_EXE" ] &&
       [ "$broray_tx_control_class_cmdline_1" = "$BRORAY_TX_CONTROL_OWNER_CMDLINE_SHA256" ]; then
        BRORAY_TX_CONTROL_OWNER_STATE=live
    fi
    return 0
}

broray_tx_control_owner_require_stale()
{
    broray_tx_control_owner_classify "$1" || return 1
    case "$BRORAY_TX_CONTROL_OWNER_STATE" in dead|reused) return 0 ;; *) return 1 ;; esac
}

broray_tx_control_owner_authorize_recovery()
{
    broray_tx_control_owner_classify "$1" || return 1
    case "$BRORAY_TX_CONTROL_OWNER_STATE" in
        dead|reused) return 0 ;;
        live)
            if [ "$BRORAY_TX_RECOVERY_FRESH_CONTROL" -eq 1 ] &&
               [ "$BRORAY_TX_CONTROL_OWNER_PID" = "$$" ]; then
                BRORAY_TX_CONTROL_OWNER_STATE=self-recovery
                return 0
            fi
            ;;
    esac
    return 1
}

broray_tx_control_owner_write_atomic()
{
    broray_tx_control_write_file="$1"
    broray_tx_control_write_parent="${broray_tx_control_write_file%/*}"
    [ "$broray_tx_control_write_parent" != "$broray_tx_control_write_file" ] || return 1
    [ -d "$broray_tx_control_write_parent" ] && [ ! -L "$broray_tx_control_write_parent" ] || return 1
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ] &&
       [ -n "${BRORAY_TX_TEST_CONTROL_PROC_PROVIDER:-}" ]; then
        [ -x "$BRORAY_TX_TEST_CONTROL_PROC_PROVIDER" ] &&
            [ ! -L "$BRORAY_TX_TEST_CONTROL_PROC_PROVIDER" ] || return 1
        "$BRORAY_TX_TEST_CONTROL_PROC_PROVIDER" \
            "$BRORAY_TX_CONTROL_PROC_ROOT" "$$" \
            "$BRORAY_TX_TEST_CONTROL_PROCESS_NONCE" "${BRORAY_TX_ASH:-/opt/bin/ash}" || return 1
    fi
    broray_tx_control_write_start="$(broray_tx_proc_starttime "$BRORAY_TX_CONTROL_PROC_ROOT" "$$")" || return 1
    # Keep the temporary record outside the authoritative control directory.
    # A power loss before rename therefore cannot create an unrecognised child
    # that wedges parsing of an otherwise valid lock.  PID+starttime makes the
    # staging name unique even after numeric PID reuse; rename stays on the
    # same /tmp filesystem and is the sole owner commit point.
    broray_tx_control_write_part="$broray_tx_control_write_parent/../.broray-owner-identity.$$.${broray_tx_control_write_start}.part"
    [ ! -e "$broray_tx_control_write_part" ] && [ ! -L "$broray_tx_control_write_part" ] || return 1
    broray_tx_control_owner_identity_capture "$$" "$broray_tx_control_write_part" || {
        rm -f "$broray_tx_control_write_part"; return 1;
    }
    chmod 600 "$broray_tx_control_write_part" 2>/dev/null || true
    mv -f "$broray_tx_control_write_part" "$broray_tx_control_write_file"
}

# A rollback-failed retry may crash after owner takeover but before its
# forensic history is written.  Persist the already validated pre-takeover
# lock tuple under the same kernel mutex so every later retry keeps the true
# cross-collection commit owner rather than relabelling an interrupted
# recovery process as the original owner.
broray_tx_recovery_predecessor_proof_publish()
{
    broray_tx_control_mutex_assert || return 1
    broray_tx_predecessor_lock="$1"
    broray_tx_predecessor_identity="$2"
    [ -f "$broray_tx_predecessor_lock/operation-id" ] &&
        [ ! -L "$broray_tx_predecessor_lock/operation-id" ] &&
        [ "$(wc -l <"$broray_tx_predecessor_lock/operation-id" | tr -d ' ')" -eq 1 ] || return 1
    broray_tx_predecessor_id="$(sed -n '1p' "$broray_tx_predecessor_lock/operation-id")"
    broray_tx_valid_id "$broray_tx_predecessor_id" || return 1
    printf '%s\n' "$broray_tx_predecessor_identity" | awk -F '\t' '
      NF==4 && $1~/^[0-9]+$/ && $1!="0" && $2~/^[0-9]+$/ && $2!="0" &&
      $3~/^\// && $4~/^[0-9a-f]+$/ && length($4)==64 { ok=1 }
      END { exit ok ? 0 : 1 }
    ' || return 1
    broray_tx_predecessor_proof="$BRORAY_TX_TMP_BASE/.broray-recovery-predecessor-$broray_tx_predecessor_id.tsv"
    broray_tx_predecessor_part="$broray_tx_predecessor_proof.part"
    if [ -e "$broray_tx_predecessor_proof" ] || [ -L "$broray_tx_predecessor_proof" ]; then
        [ -f "$broray_tx_predecessor_proof" ] &&
            [ ! -L "$broray_tx_predecessor_proof" ] &&
            broray_tx_control_owner_authorize_recovery "$broray_tx_predecessor_proof" || return 1
    else
        if [ -e "$broray_tx_predecessor_part" ] || [ -L "$broray_tx_predecessor_part" ]; then
            [ -f "$broray_tx_predecessor_part" ] &&
                [ ! -L "$broray_tx_predecessor_part" ] || return 1
            if broray_tx_control_owner_identity_read "$broray_tx_predecessor_part"; then
                broray_tx_control_owner_authorize_recovery "$broray_tx_predecessor_part" || return 1
            else
                rm -f "$broray_tx_predecessor_part" || return 1
                [ ! -e "$broray_tx_predecessor_part" ] &&
                    [ ! -L "$broray_tx_predecessor_part" ] || return 1
            fi
        fi
        if [ ! -e "$broray_tx_predecessor_part" ] && [ ! -L "$broray_tx_predecessor_part" ]; then
            printf '%s\n' "$broray_tx_predecessor_identity" >"$broray_tx_predecessor_part" || return 1
            chmod 600 "$broray_tx_predecessor_part" 2>/dev/null || true
        fi
        broray_tx_control_owner_identity_read "$broray_tx_predecessor_part" || return 1
        mv -f "$broray_tx_predecessor_part" "$broray_tx_predecessor_proof" || return 1
    fi
    broray_tx_control_owner_identity_read "$broray_tx_predecessor_proof" || return 1
    BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_LOCK_IDENTITY="$(sed -n '1p' "$broray_tx_predecessor_proof")"
    BRORAY_TX_RECOVERY_PREDECESSOR_PROOF="$broray_tx_predecessor_proof"
}

# Atomically change an already validated stale control owner to this process
# while the native OPKG kernel lock serializes every competing transition.
# No persistent userspace claim is needed: a crash before the final rename
# leaves the old stale tuple; a crash after it leaves this exact tuple dead or
# reused and therefore safely retryable by the next kernel-mutex holder.
broray_tx_control_owner_takeover()
{
    broray_tx_control_takeover_lock="$1"
    broray_tx_control_takeover_allow_self="${2:-0}"
    broray_tx_control_takeover_expected_sha="${3:-}"
    broray_tx_control_takeover_workspace="${4:-}"
    [ -d "$broray_tx_control_takeover_lock" ] &&
        [ ! -L "$broray_tx_control_takeover_lock" ] || return 1
    broray_tx_control_takeover_owner="$broray_tx_control_takeover_lock/owner-identity.tsv"
    BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_LOCK_IDENTITY=""
    [ -f "$broray_tx_control_takeover_owner" ] &&
        [ ! -L "$broray_tx_control_takeover_owner" ] || return 1
    broray_tx_control_mutex_acquire || return 1
    broray_tx_control_takeover_rc=1
    if broray_tx_control_mutex_assert; then
        broray_tx_control_takeover_actual_sha="$(broray_tx_sha "$broray_tx_control_takeover_owner")"
        if { [ -z "$broray_tx_control_takeover_expected_sha" ] ||
             [ "$broray_tx_control_takeover_actual_sha" = "$broray_tx_control_takeover_expected_sha" ]; } &&
           broray_tx_control_owner_classify "$broray_tx_control_takeover_owner"
        then
            broray_tx_control_takeover_original_state="$BRORAY_TX_CONTROL_OWNER_STATE"
            case "$broray_tx_control_takeover_original_state" in
                dead|reused) broray_tx_control_takeover_authorized=1 ;;
                live)
                    if [ "$broray_tx_control_takeover_allow_self" -eq 1 ] &&
                       [ "$BRORAY_TX_CONTROL_OWNER_PID" = "$$" ]; then
                        broray_tx_control_takeover_authorized=1
                    else
                        broray_tx_control_takeover_authorized=0
                    fi
                    ;;
                *) broray_tx_control_takeover_authorized=0 ;;
            esac
            broray_tx_control_takeover_workspace_authorized=1
            if [ -n "$broray_tx_control_takeover_workspace" ]; then
                case "$broray_tx_control_takeover_workspace" in
                    "$BRORAY_TX_TMP_BASE"/broray-update-*) ;;
                    *) broray_tx_control_takeover_workspace_authorized=0 ;;
                esac
                if [ "$broray_tx_control_takeover_workspace_authorized" -eq 1 ]; then
                    [ -d "$broray_tx_control_takeover_workspace" ] &&
                        [ ! -L "$broray_tx_control_takeover_workspace" ] &&
                        [ -f "$broray_tx_control_takeover_workspace/owner-identity.tsv" ] &&
                        [ ! -L "$broray_tx_control_takeover_workspace/owner-identity.tsv" ] ||
                        broray_tx_control_takeover_workspace_authorized=0
                fi
                if [ "$broray_tx_control_takeover_workspace_authorized" -eq 1 ]; then
                    broray_tx_control_owner_classify \
                        "$broray_tx_control_takeover_workspace/owner-identity.tsv" ||
                        broray_tx_control_takeover_workspace_authorized=0
                fi
                if [ "$broray_tx_control_takeover_workspace_authorized" -eq 1 ]; then
                    broray_tx_control_takeover_workspace_state="$BRORAY_TX_CONTROL_OWNER_STATE"
                    broray_tx_control_takeover_workspace_pid="$BRORAY_TX_CONTROL_OWNER_PID"
                    if ! broray_tx_files_equal "$broray_tx_control_takeover_owner" \
                        "$broray_tx_control_takeover_workspace/owner-identity.tsv"; then
                        # A prior workspace-first handoff/takeover may have died
                        # before the transaction-lock owner commit.  Under the
                        # kernel fence, independently stale tuples are an exact
                        # retryable split transition.  A foreign live workspace
                        # remains authoritative and is never stolen.
                        case "$broray_tx_control_takeover_workspace_state" in
                            dead|reused) ;;
                            live)
                                [ "$broray_tx_control_takeover_workspace_pid" = "$$" ] ||
                                    broray_tx_control_takeover_workspace_authorized=0 ;;
                            *) broray_tx_control_takeover_workspace_authorized=0 ;;
                        esac
                    fi
                fi
            fi
            if [ "$broray_tx_control_takeover_authorized" -eq 1 ] &&
               [ "$broray_tx_control_takeover_workspace_authorized" -eq 1 ]; then
                broray_tx_control_takeover_publish_rc=0
                BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_LOCK_IDENTITY="$(
                    sed -n '1p' "$broray_tx_control_takeover_owner"
                )"
                [ -n "$BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_LOCK_IDENTITY" ] ||
                    broray_tx_control_takeover_publish_rc=1
                if [ "$broray_tx_control_takeover_publish_rc" -eq 0 ] &&
                   [ "$BRORAY_TX_RECOVERY_CAPTURE_PREDECESSOR" -eq 1 ]; then
                    broray_tx_recovery_predecessor_proof_publish \
                        "$broray_tx_control_takeover_lock" \
                        "$BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_LOCK_IDENTITY" ||
                        broray_tx_control_takeover_publish_rc=1
                fi
                # Workspace ownership is preparatory; transaction-lock
                # ownership is the cross-collection commit marker and is
                # therefore always published last.
                if [ -n "$broray_tx_control_takeover_workspace" ]; then
                    broray_tx_control_owner_write_atomic \
                        "$broray_tx_control_takeover_workspace/owner-identity.tsv" &&
                    broray_tx_control_owner_assert_self \
                        "$broray_tx_control_takeover_workspace/owner-identity.tsv" &&
                    broray_tx_test_pause owner-transition-workspace-published \
                        "$broray_tx_control_takeover_workspace/evidence" ||
                        broray_tx_control_takeover_publish_rc=1
                fi
                if [ "$broray_tx_control_takeover_publish_rc" -eq 0 ]; then
                    broray_tx_control_owner_write_atomic "$broray_tx_control_takeover_owner" &&
                    broray_tx_control_owner_assert_self "$broray_tx_control_takeover_owner" &&
                    broray_tx_test_pause owner-transition-lock-published \
                        "${broray_tx_control_takeover_workspace:+$broray_tx_control_takeover_workspace/evidence}" ||
                        broray_tx_control_takeover_publish_rc=1
                fi
                if [ "$broray_tx_control_takeover_publish_rc" -eq 0 ]; then
                    broray_tx_control_takeover_rc=0
                    BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_STATE="$broray_tx_control_takeover_original_state"
                fi
            fi
        fi
    fi
    broray_tx_control_mutex_release || return 1
    return "$broray_tx_control_takeover_rc"
}

broray_tx_control_owner_assert_self()
{
    broray_tx_control_owner_classify "$1" || return 1
    [ "$BRORAY_TX_CONTROL_OWNER_STATE" = live ] &&
        [ "$BRORAY_TX_CONTROL_OWNER_PID" = "$$" ]
}

broray_tx_recovery_takeover_current_control()
{
    BRORAY_TX_RECOVERY_PREDECESSOR_OWNER_IDENTITY=""
    broray_tx_control_takeover_allow_self=0
    [ "$BRORAY_TX_RECOVERY_FRESH_CONTROL" -eq 1 ] && broray_tx_control_takeover_allow_self=1
    broray_tx_control_takeover_workspace=""
    broray_tx_control_takeover_workspace_candidate="${broray_tx_recovery_path:-${BRORAY_TX_WORK:-}}"
    if [ -n "$broray_tx_control_takeover_workspace_candidate" ] &&
       [ -f "$broray_tx_control_takeover_workspace_candidate/owner-identity.tsv" ] &&
       [ ! -L "$broray_tx_control_takeover_workspace_candidate/owner-identity.tsv" ]; then
        broray_tx_control_takeover_workspace="$broray_tx_control_takeover_workspace_candidate"
    fi
    broray_tx_control_owner_takeover \
        "$BRORAY_TX_LOCK_DIR" "$broray_tx_control_takeover_allow_self" \
        "${BRORAY_TX_RECOVERY_EXPECTED_OWNER_SHA256:-}" \
        "$broray_tx_control_takeover_workspace" || return 1
    if [ "$BRORAY_TX_RECOVERY_CAPTURE_PREDECESSOR" -eq 1 ]; then
        [ -n "$BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_LOCK_IDENTITY" ] || return 1
        BRORAY_TX_RECOVERY_PREDECESSOR_OWNER_IDENTITY="$BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_LOCK_IDENTITY"
    fi
    [ -n "$BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE" ] ||
        BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE="$BRORAY_TX_CONTROL_TAKEOVER_ORIGINAL_STATE"
    BRORAY_TX_RECOVERY_FRESH_CONTROL=1
    BRORAY_TX_LOCK_HELD=1
    broray_tx_test_pause recovery-control-takeover
}

broray_tx_files_equal()
{
    [ -f "$1" ] && [ ! -L "$1" ] && [ -f "$2" ] && [ ! -L "$2" ] || return 1
    broray_tx_equal_left_size="$(wc -c <"$1" 2>/dev/null | tr -d ' ')"
    broray_tx_equal_right_size="$(wc -c <"$2" 2>/dev/null | tr -d ' ')"
    broray_tx_number "$broray_tx_equal_left_size" && broray_tx_number "$broray_tx_equal_right_size" || return 1
    [ "$broray_tx_equal_left_size" -eq "$broray_tx_equal_right_size" ] || return 1
    [ "$(broray_tx_sha "$1")" = "$(broray_tx_sha "$2")" ]
}

broray_tx_sort_file()
{
    broray_tx_sort_mode="$1"
    broray_tx_sort_input="$2"
    broray_tx_sort_output="$3"
    broray_tx_sort_part="$broray_tx_sort_output.sort.part"
    rm -f "$broray_tx_sort_part"
    case "$broray_tx_sort_mode" in
        unique) LC_ALL=C sort -u "$broray_tx_sort_input" >"$broray_tx_sort_part" ;;
        plain) LC_ALL=C sort "$broray_tx_sort_input" >"$broray_tx_sort_part" ;;
        reverse) LC_ALL=C sort -r "$broray_tx_sort_input" >"$broray_tx_sort_part" ;;
        numeric) LC_ALL=C sort -n "$broray_tx_sort_input" >"$broray_tx_sort_part" ;;
        *) return 1 ;;
    esac || { rm -f "$broray_tx_sort_part"; return 1; }
    mv -f "$broray_tx_sort_part" "$broray_tx_sort_output"
}

broray_tx_number()
{
    case "${1:-}" in ''|*[!0-9]*|0[0-9]*) return 1 ;; esac
    [ "$1" -le 9007199254740991 ] 2>/dev/null
}

broray_tx_uadd()
{
    broray_tx_number "$1" && broray_tx_number "$2" || return 1
    [ "$2" -le $((9007199254740991 - $1)) ] || return 1
    printf '%s\n' "$(($1 + $2))"
}

broray_tx_umul()
{
    broray_tx_number "$1" && broray_tx_number "$2" || return 1
    if [ "$1" -eq 0 ]; then printf '0\n'; return 0; fi
    [ "$2" -le $((9007199254740991 / $1)) ] || return 1
    printf '%s\n' "$(($1 * $2))"
}

broray_tx_ceil_div()
{
    broray_tx_number "$1" && broray_tx_number "$2" && [ "$2" -gt 0 ] || return 1
    broray_tx_ceil_q=$(($1 / $2)); broray_tx_ceil_r=$(($1 % $2))
    [ "$broray_tx_ceil_r" -eq 0 ] || broray_tx_ceil_q="$(broray_tx_uadd "$broray_tx_ceil_q" 1)" || return 1
    printf '%s\n' "$broray_tx_ceil_q"
}

broray_tx_sub0()
{
    broray_tx_number "$1" && broray_tx_number "$2" || return 1
    if [ "$1" -gt "$2" ]; then printf '%s\n' "$(($1 - $2))"; else printf '0\n'; fi
}

broray_tx_usub()
{
    broray_tx_number "$1" && broray_tx_number "$2" && [ "$2" -le "$1" ] || return 1
    printf '%s\n' "$(($1 - $2))"
}

broray_tx_umax()
{
    [ "$#" -gt 0 ] || return 1
    broray_tx_max_value=0
    for broray_tx_max_item in "$@"; do
        broray_tx_number "$broray_tx_max_item" || return 1
        [ "$broray_tx_max_item" -le "$broray_tx_max_value" ] || broray_tx_max_value="$broray_tx_max_item"
    done
    printf '%s\n' "$broray_tx_max_value"
}

# Return a conservative dense allocation in KiB.  The measured one-byte
# allocation unit is part of the capability evidence and is never guessed in
# production.  This deliberately does not use st_blocks: a sparse input is
# restored by the canonical non---sparse GNU tar command as dense bytes.
broray_tx_dense_bytes_upper_kb()
{
    broray_tx_dense_bytes="$1"; broray_tx_dense_unit_kb="$2"
    broray_tx_number "$broray_tx_dense_bytes" && broray_tx_number "$broray_tx_dense_unit_kb" &&
        [ "$broray_tx_dense_unit_kb" -gt 0 ] || return 1
    broray_tx_dense_unit_bytes="$(broray_tx_umul "$broray_tx_dense_unit_kb" 1024)" || return 1
    broray_tx_dense_units="$(broray_tx_ceil_div "$broray_tx_dense_bytes" "$broray_tx_dense_unit_bytes")" || return 1
    broray_tx_umul "$broray_tx_dense_units" "$broray_tx_dense_unit_kb"
}

broray_tx_file_dense_upper_kb()
{
    broray_tx_dense_file="$1"; broray_tx_dense_unit_kb="$2"
    if [ ! -e "$broray_tx_dense_file" ] && [ ! -L "$broray_tx_dense_file" ]; then
        printf '0\n'
        return 0
    fi
    [ -f "$broray_tx_dense_file" ] && [ ! -L "$broray_tx_dense_file" ] || return 1
    broray_tx_dense_file_bytes="$(wc -c <"$broray_tx_dense_file" 2>/dev/null | tr -d ' ')"
    broray_tx_number "$broray_tx_dense_file_bytes" || return 1
    broray_tx_dense_bytes_upper_kb "$broray_tx_dense_file_bytes" "$broray_tx_dense_unit_kb"
}

# Candidate allocation metadata is generated for a 4-KiB baseline.  For a
# larger proved allocation unit every payload object can require at most one
# additional (unit-4) tail.  A smaller unit only makes the baseline safer.
broray_tx_adjust_allocation_upper_kb()
{
    broray_tx_adjust_base="$1"; broray_tx_adjust_objects="$2"; broray_tx_adjust_unit="$3"
    broray_tx_number "$broray_tx_adjust_base" && broray_tx_number "$broray_tx_adjust_objects" &&
        broray_tx_number "$broray_tx_adjust_unit" && [ "$broray_tx_adjust_unit" -gt 0 ] || return 1
    if [ "$broray_tx_adjust_unit" -gt 4 ]; then
        broray_tx_adjust_extra="$(broray_tx_umul "$broray_tx_adjust_objects" "$((broray_tx_adjust_unit - 4))")" || return 1
        broray_tx_uadd "$broray_tx_adjust_base" "$broray_tx_adjust_extra"
    else
        printf '%s\n' "$broray_tx_adjust_base"
    fi
}

# Upper allocation of a source tree after rollback extraction.  Regular data
# uses logical bytes, so sparse files cannot be undercounted.  One full
# allocation unit per manifest object additionally bounds directory entries,
# directory blocks, symlink storage and filesystem metadata variation.
broray_tx_manifest_restore_upper_kb()
{
    broray_tx_restore_manifest="$1"; broray_tx_restore_unit_kb="$2"
    [ -f "$broray_tx_restore_manifest" ] && [ ! -L "$broray_tx_restore_manifest" ] || return 1
    broray_tx_number "$broray_tx_restore_unit_kb" && [ "$broray_tx_restore_unit_kb" -gt 0 ] || return 1
    broray_tx_restore_total=0
    broray_tx_restore_objects=0
    while IFS='|' read -r broray_tx_restore_type broray_tx_restore_path broray_tx_restore_bytes \
        broray_tx_restore_sha broray_tx_restore_mode broray_tx_restore_uid broray_tx_restore_gid broray_tx_restore_target
    do
        case "$broray_tx_restore_type" in
            F)
                broray_tx_number "$broray_tx_restore_bytes" || return 1
                broray_tx_restore_one="$(broray_tx_dense_bytes_upper_kb "$broray_tx_restore_bytes" "$broray_tx_restore_unit_kb")" || return 1
                broray_tx_restore_total="$(broray_tx_uadd "$broray_tx_restore_total" "$broray_tx_restore_one")" || return 1
                ;;
            D|L) ;;
            *) return 1 ;;
        esac
        broray_tx_restore_objects="$(broray_tx_uadd "$broray_tx_restore_objects" 1)" || return 1
    done <"$broray_tx_restore_manifest"
    broray_tx_restore_metadata="$(broray_tx_umul "$broray_tx_restore_objects" "$broray_tx_restore_unit_kb")" || return 1
    broray_tx_uadd "$broray_tx_restore_total" "$broray_tx_restore_metadata"
}

broray_tx_manifest_logical_bytes()
{
    broray_tx_logical_manifest="$1"
    [ -f "$broray_tx_logical_manifest" ] && [ ! -L "$broray_tx_logical_manifest" ] || return 1
    broray_tx_logical_total=0
    while IFS='|' read -r broray_tx_logical_type broray_tx_logical_path broray_tx_logical_bytes \
        broray_tx_logical_sha broray_tx_logical_mode broray_tx_logical_uid broray_tx_logical_gid broray_tx_logical_target
    do
        case "$broray_tx_logical_type" in
            F|L)
                broray_tx_number "$broray_tx_logical_bytes" || return 1
                broray_tx_logical_total="$(broray_tx_uadd "$broray_tx_logical_total" "$broray_tx_logical_bytes")" || return 1
                ;;
            D) ;;
            *) return 1 ;;
        esac
    done <"$broray_tx_logical_manifest"
    printf '%s\n' "$broray_tx_logical_total"
}

# GNU format can emit a GNU.longname/longlink record.  Always reserving those
# records is conservative for short names and exact with respect to arbitrary
# filesystem path and symlink-target lengths.  The manifest grammar excludes
# delimiters and newlines, so byte lengths are unambiguous in LC_ALL=C.
broray_tx_tar_manifest_upper_bytes()
{
    broray_tx_tar_manifest="$1"; broray_tx_tar_prefix="$2"
    [ -f "$broray_tx_tar_manifest" ] && [ ! -L "$broray_tx_tar_manifest" ] || return 1
    broray_tx_tar_total=0
    while IFS='|' read -r broray_tx_tar_type broray_tx_tar_path broray_tx_tar_bytes \
        broray_tx_tar_sha broray_tx_tar_mode broray_tx_tar_uid broray_tx_tar_gid broray_tx_tar_target
    do
        case "$broray_tx_tar_type" in F|D|L) ;; *) return 1 ;; esac
        broray_tx_tar_full_path="$broray_tx_tar_prefix$broray_tx_tar_path"
        broray_tx_tar_path_bytes="$(printf '%s' "$broray_tx_tar_full_path" | wc -c | tr -d ' ')"
        broray_tx_number "$broray_tx_tar_path_bytes" || return 1
        # Base header plus an always-reserved GNU.longname header and payload.
        broray_tx_tar_name_record="$(broray_tx_uadd "$broray_tx_tar_path_bytes" 2)" || return 1
        broray_tx_tar_name_record="$(broray_tx_ceil_div "$broray_tx_tar_name_record" 512)" || return 1
        broray_tx_tar_name_record="$(broray_tx_umul "$broray_tx_tar_name_record" 512)" || return 1
        broray_tx_tar_one="$(broray_tx_uadd 1024 "$broray_tx_tar_name_record")" || return 1
        if [ "$broray_tx_tar_type" = F ]; then
            broray_tx_number "$broray_tx_tar_bytes" || return 1
            broray_tx_tar_data="$(broray_tx_ceil_div "$broray_tx_tar_bytes" 512)" || return 1
            broray_tx_tar_data="$(broray_tx_umul "$broray_tx_tar_data" 512)" || return 1
            broray_tx_tar_one="$(broray_tx_uadd "$broray_tx_tar_one" "$broray_tx_tar_data")" || return 1
        elif [ "$broray_tx_tar_type" = L ]; then
            broray_tx_number "$broray_tx_tar_bytes" || return 1
            broray_tx_tar_link_record="$(broray_tx_uadd "$broray_tx_tar_bytes" 1)" || return 1
            broray_tx_tar_link_record="$(broray_tx_ceil_div "$broray_tx_tar_link_record" 512)" || return 1
            broray_tx_tar_link_record="$(broray_tx_umul "$broray_tx_tar_link_record" 512)" || return 1
            broray_tx_tar_link_record="$(broray_tx_uadd 512 "$broray_tx_tar_link_record")" || return 1
            broray_tx_tar_one="$(broray_tx_uadd "$broray_tx_tar_one" "$broray_tx_tar_link_record")" || return 1
        fi
        broray_tx_tar_total="$(broray_tx_uadd "$broray_tx_tar_total" "$broray_tx_tar_one")" || return 1
    done <"$broray_tx_tar_manifest"
    printf '%s\n' "$broray_tx_tar_total"
}

broray_tx_file_cap_kb()
{
    broray_tx_cap_file="$1"; broray_tx_cap_kb="$2"
    [ ! -e "$broray_tx_cap_file" ] && [ ! -L "$broray_tx_cap_file" ] && return 0
    [ -f "$broray_tx_cap_file" ] && [ ! -L "$broray_tx_cap_file" ] || return 1
    broray_tx_cap_bytes="$(wc -c <"$broray_tx_cap_file" 2>/dev/null | tr -d ' ')"
    broray_tx_number "$broray_tx_cap_bytes" && broray_tx_number "$broray_tx_cap_kb" || return 1
    broray_tx_cap_limit_bytes="$(broray_tx_umul "$broray_tx_cap_kb" 1024)" || return 1
    [ "$broray_tx_cap_bytes" -le "$broray_tx_cap_limit_bytes" ]
}

# Return the non-negative growth only when it is within the declared cap.
# Callers use the returned exact value as evidence, so the comparison and the
# recorded value cannot drift apart.  All arithmetic remains in the checked
# integer domain used by the space planner.
broray_tx_growth_within_cap()
{
    broray_tx_growth_current="$1"; broray_tx_growth_baseline="$2"; broray_tx_growth_cap="$3"
    broray_tx_number "$broray_tx_growth_current" && broray_tx_number "$broray_tx_growth_baseline" &&
        broray_tx_number "$broray_tx_growth_cap" || return 1
    broray_tx_growth_value="$(broray_tx_sub0 "$broray_tx_growth_current" "$broray_tx_growth_baseline")" || return 1
    [ "$broray_tx_growth_value" -le "$broray_tx_growth_cap" ] || return 1
    printf '%s\n' "$broray_tx_growth_value"
}

# Execute one producer behind two exact KiB-bounded FIFO consumers.  The
# optional RLIMIT_FSIZE value is in POSIX 512-byte blocks and is applied only
# to the child.  Thus stdout/stderr can never allocate beyond their planned
# records, and a candidate-bound setup child cannot grow any regular file
# beyond its per-file term before it is rejected.
broray_tx_bounded_command()
{
    broray_tx_bounded_cap_kb="$1"; broray_tx_bounded_stdout="$2"; broray_tx_bounded_stderr="$3"
    broray_tx_bounded_file_blocks="$4"; shift 4
    broray_tx_number "$broray_tx_bounded_cap_kb" && [ "$broray_tx_bounded_cap_kb" -gt 0 ] &&
        broray_tx_number "$broray_tx_bounded_file_blocks" && [ "$#" -gt 0 ] || return 1
    broray_tx_guard_work "$broray_tx_bounded_stdout" && broray_tx_guard_work "$broray_tx_bounded_stderr" || return 1
    broray_tx_bounded_stdout_fifo="$broray_tx_bounded_stdout.fifo"
    broray_tx_bounded_stderr_fifo="$broray_tx_bounded_stderr.fifo"
    broray_tx_guard_work "$broray_tx_bounded_stdout_fifo" && broray_tx_guard_work "$broray_tx_bounded_stderr_fifo" || return 1
    rm -f "$broray_tx_bounded_stdout" "$broray_tx_bounded_stderr" \
        "$broray_tx_bounded_stdout_fifo" "$broray_tx_bounded_stderr_fifo" || return 1
    mkfifo "$broray_tx_bounded_stdout_fifo" "$broray_tx_bounded_stderr_fifo" || return 1
    (
        exec 8<"$broray_tx_bounded_stdout_fifo" || exit 1
        dd of="$broray_tx_bounded_stdout" bs=1024 count="$broray_tx_bounded_cap_kb" iflag=fullblock <&8 2>/dev/null
        broray_tx_bounded_consumer_rc=$?
        broray_tx_bounded_overflow="$(dd bs=1 count=1 <&8 2>/dev/null | wc -c | tr -d ' ')"
        cat <&8 >/dev/null || exit 1
        exec 8<&-
        [ "$broray_tx_bounded_consumer_rc" -eq 0 ] && [ "$broray_tx_bounded_overflow" = 0 ]
    ) &
    broray_tx_bounded_stdout_pid=$!
    (
        exec 8<"$broray_tx_bounded_stderr_fifo" || exit 1
        dd of="$broray_tx_bounded_stderr" bs=1024 count="$broray_tx_bounded_cap_kb" iflag=fullblock <&8 2>/dev/null
        broray_tx_bounded_consumer_rc=$?
        broray_tx_bounded_overflow="$(dd bs=1 count=1 <&8 2>/dev/null | wc -c | tr -d ' ')"
        cat <&8 >/dev/null || exit 1
        exec 8<&-
        [ "$broray_tx_bounded_consumer_rc" -eq 0 ] && [ "$broray_tx_bounded_overflow" = 0 ]
    ) &
    broray_tx_bounded_stderr_pid=$!
    if [ "$broray_tx_bounded_file_blocks" -gt 0 ]; then
        "$BRORAY_TX_ASH" -c 'ulimit -S -f "$1" || exit 125; shift; exec "$@"' \
            ash "$broray_tx_bounded_file_blocks" "$@" \
            >"$broray_tx_bounded_stdout_fifo" 2>"$broray_tx_bounded_stderr_fifo"
    else
        "$@" >"$broray_tx_bounded_stdout_fifo" 2>"$broray_tx_bounded_stderr_fifo"
    fi
    broray_tx_bounded_producer_rc=$?
    wait "$broray_tx_bounded_stdout_pid"; broray_tx_bounded_stdout_rc=$?
    wait "$broray_tx_bounded_stderr_pid"; broray_tx_bounded_stderr_rc=$?
    rm -f "$broray_tx_bounded_stdout_fifo" "$broray_tx_bounded_stderr_fifo" || return 1
    broray_tx_file_cap_kb "$broray_tx_bounded_stdout" "$broray_tx_bounded_cap_kb" &&
        broray_tx_file_cap_kb "$broray_tx_bounded_stderr" "$broray_tx_bounded_cap_kb" || return 1
    [ "$broray_tx_bounded_producer_rc" -eq 0 ] && [ "$broray_tx_bounded_stdout_rc" -eq 0 ] &&
        [ "$broray_tx_bounded_stderr_rc" -eq 0 ]
}

broray_tx_df_inodes()
{
    # The retired full-IPK engine has no supported numeric inode counter on
    # the pinned Entware BusyBox.  Any accidental legacy space check must fail
    # closed instead of invoking an option the target silently lacks.
    return 1
}

broray_tx_valid_id()
{
    case "${1:-}" in ''|.*|-*|*[!0-9A-Za-z._-]*) return 1 ;; esac
    [ "${#1}" -le 96 ]
}

broray_tx_fail()
{
    BRORAY_TX_REASON="$*"
    return 1
}

broray_tx_trap_disable()
{
    trap - EXIT HUP INT TERM 2>/dev/null || true
    BRORAY_TX_TRAP_ACTIVE=0
}

broray_tx_trap_handler()
{
    broray_tx_trap_signal="$1"
    [ "$BRORAY_TX_TRAP_ACTIVE" -eq 1 ] || return 0
    broray_tx_trap_disable
    [ "$BRORAY_TX_FAILURE_RUNNING" -eq 0 ] || return 0
    BRORAY_TX_REASON="signal-$broray_tx_trap_signal"
    if [ -n "$BRORAY_TX_WORK" ] && [ -d "$BRORAY_TX_WORK" ]; then
        broray_tx_failure >/dev/null 2>&1 || true
    fi
}

broray_tx_trap_enable()
{
    BRORAY_TX_TRAP_ACTIVE=1
    trap 'broray_tx_trap_handler EXIT' EXIT
    trap 'broray_tx_trap_handler HUP; exit 129' HUP
    trap 'broray_tx_trap_handler INT; exit 130' INT
    trap 'broray_tx_trap_handler TERM; exit 143' TERM
}

broray_tx_reject_preworkspace()
{
    BRORAY_TX_REASON="$*"
    printf 'BROray: FAIL:%s\n' "$BRORAY_TX_REASON" >&2
    return 1
}

broray_tx_inject()
{
    broray_tx_inject_stage="$1"
    [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ] || return 0
    broray_tx_inject_spec="${BRORAY_TX_TEST_FAIL_STAGE:-}"
    [ -n "$broray_tx_inject_spec" ] || return 0
    case "$broray_tx_inject_stage" in ''|*[!0-9A-Za-z._-]*) return 1 ;; esac
    if [ "$broray_tx_inject_spec" = "$broray_tx_inject_stage" ]; then
        broray_tx_fail "injected-$broray_tx_inject_stage"
        return 1
    fi
    case "$broray_tx_inject_spec" in
        "$broray_tx_inject_stage"@*)
            broray_tx_inject_ordinal="${broray_tx_inject_spec#*@}"
            case "$broray_tx_inject_ordinal" in ''|*[!0-9]*|0|0*) return 1 ;; esac
            [ -n "$BRORAY_TX_WORK" ] && [ -d "$BRORAY_TX_WORK/evidence" ] || return 1
            broray_tx_inject_counter_file="$BRORAY_TX_WORK/evidence/inject-$broray_tx_inject_stage.count"
            broray_tx_inject_count=0
            if [ -f "$broray_tx_inject_counter_file" ] && [ ! -L "$broray_tx_inject_counter_file" ]; then
                broray_tx_inject_count="$(sed -n '1p' "$broray_tx_inject_counter_file")"
                case "$broray_tx_inject_count" in ''|*[!0-9]*) return 1 ;; esac
            elif [ -e "$broray_tx_inject_counter_file" ] || [ -L "$broray_tx_inject_counter_file" ]; then
                return 1
            fi
            broray_tx_inject_count=$((broray_tx_inject_count + 1))
            printf '%s\n' "$broray_tx_inject_count" >"$broray_tx_inject_counter_file.part" || return 1
            mv -f "$broray_tx_inject_counter_file.part" "$broray_tx_inject_counter_file" || return 1
            if [ "$broray_tx_inject_count" -eq "$broray_tx_inject_ordinal" ]; then
                broray_tx_fail "injected-$broray_tx_inject_stage@$broray_tx_inject_ordinal"
                return 1
            fi
            ;;
    esac
    return 0
}

broray_tx_test_pause()
{
    broray_tx_pause_stage="$1"
    [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ] &&
        [ "${BRORAY_TX_TEST_PAUSE_STAGE:-}" = "$broray_tx_pause_stage" ] || return 0
    case "$broray_tx_pause_stage" in ''|*[!0-9A-Za-z._-]*) return 1 ;; esac
    broray_tx_pause_evidence="${2:-${BRORAY_TX_WORK:+$BRORAY_TX_WORK/evidence}}"
    case "$broray_tx_pause_evidence" in "$BRORAY_TX_TMP_BASE"/*) ;; *) return 1 ;; esac
    [ -d "$broray_tx_pause_evidence" ] && [ ! -L "$broray_tx_pause_evidence" ] || return 1
    : >"$broray_tx_pause_evidence/pause-$broray_tx_pause_stage.ready" || return 1
    while [ ! -f "$broray_tx_pause_evidence/pause-$broray_tx_pause_stage.release" ]; do sleep 1; done
}

broray_tx_to_relative()
{
    broray_tx_relative_path="$1"
    if [ -n "$BRORAY_TX_PREFIX" ]; then
        case "$broray_tx_relative_path" in "$BRORAY_TX_PREFIX"/*) printf '%s\n' "${broray_tx_relative_path#"$BRORAY_TX_PREFIX"/}" ;; *) return 1 ;; esac
    else
        case "$broray_tx_relative_path" in /*) printf '%s\n' "${broray_tx_relative_path#/}" ;; *) return 1 ;; esac
    fi
}

broray_tx_guard_work()
{
    case "$1" in "$BRORAY_TX_WORK"|"$BRORAY_TX_WORK"/*) return 0 ;; *) return 1 ;; esac
}

broray_tx_shell_syntax()
{
    if [ -x "$BRORAY_TX_ASH" ]; then "$BRORAY_TX_ASH" -n "$1"; else busybox ash -n "$1"; fi
}

broray_tx_event()
{
    BRORAY_TX_STAGE="$1"
    broray_tx_event_file="$BRORAY_TX_WORK/evidence/events.tsv"
    broray_tx_event_count="$(wc -l <"$broray_tx_event_file" 2>/dev/null | tr -d ' ')"
    broray_tx_number "$broray_tx_event_count" || broray_tx_event_count=0
    broray_tx_event_count=$((broray_tx_event_count + 1))
    printf '%04d\t%s\t%s\n' "$broray_tx_event_count" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$1" >>"$broray_tx_event_file"
}

broray_tx_status()
{
    broray_tx_status_name="$1"
    broray_tx_status_reason="${2:-}"
    jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" --arg mode "$BRORAY_TX_MODE" \
        --arg status "$broray_tx_status_name" --arg stage "$BRORAY_TX_STAGE" \
        --arg reason "$broray_tx_status_reason" --arg contract "$BRORAY_TX_CONTRACT" \
        --arg updatedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
        --argjson mutationStarted "$([ "$BRORAY_TX_MUTATED" -eq 1 ] && printf true || printf false)" \
        '{schemaVersion:1,lifecycleContract:$contract,operationId:$operationId,mode:$mode,
          status:$status,stage:$stage,reason:(if $reason=="" then null else $reason end),
          mutationStarted:$mutationStarted,updatedAt:$updatedAt}' >"$BRORAY_TX_WORK/status.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/status.json.part" "$BRORAY_TX_WORK/status.json"
}

broray_tx_installed_status_field()
{
    [ -f "$BRORAY_TX_STATUS_FILE" ] && [ ! -L "$BRORAY_TX_STATUS_FILE" ] || return 1
    awk -v wanted="$1" '
      BEGIN { RS=""; FS="\n" }
      $0 ~ /(^|\n)Package:[[:space:]]*broray(\n|$)/ {
        for (i=1; i<=NF; i++) {
          if ($i ~ ("^" wanted ":[[:space:]]*")) {
            value=$i; sub("^[^:]*:[[:space:]]*", "", value); print value; exit
          }
        }
      }
    ' "$BRORAY_TX_STATUS_FILE"
}

broray_tx_status_broray_unique()
{
    if [ ! -e "$BRORAY_TX_STATUS_FILE" ] && [ ! -L "$BRORAY_TX_STATUS_FILE" ]; then return 0; fi
    [ -f "$BRORAY_TX_STATUS_FILE" ] && [ ! -L "$BRORAY_TX_STATUS_FILE" ] || return 1
    awk 'BEGIN{RS=""} $0 ~ /(^|\n)Package:[[:space:]]*broray(\n|$)/ {found++} END{exit(found>1)}' \
        "$BRORAY_TX_STATUS_FILE"
}

broray_tx_status_stanza_broray()
{
    [ -f "$BRORAY_TX_STATUS_FILE" ] && [ ! -L "$BRORAY_TX_STATUS_FILE" ] || return 0
    awk 'BEGIN{RS="";ORS="\n\n"} $0 ~ /(^|\n)Package:[[:space:]]*broray(\n|$)/ {print; found++} END{if(found>1)exit 2}' \
        "$BRORAY_TX_STATUS_FILE"
}

# Project the shared OPKG status without rewriting any foreign stanza byte or
# foreign-to-foreign delimiter.  Remove the BROray content plus at most its
# first following blank delimiter; appending one canonical delimiter with the
# replacement stanza therefore makes repeated remove/insert cycles stable.
# The input must be LF-terminated and NUL/CR-free, every paragraph must have
# exactly one Package field, and more than one BROray paragraph is ambiguous.
broray_tx_status_foreign_projection()
{
    broray_tx_status_projection_input="$1"
    broray_tx_status_projection_output="$2"
    [ -f "$broray_tx_status_projection_input" ] &&
        [ ! -L "$broray_tx_status_projection_input" ] || return 1
    if [ ! -s "$broray_tx_status_projection_input" ]; then
        : >"$broray_tx_status_projection_output"
        return 0
    fi
    broray_tx_status_last_byte="$(od -An -tu1 -v "$broray_tx_status_projection_input" 2>/dev/null |
        awk '{for(i=1;i<=NF;i++){if($i==0 || $i==13)bad=1; last=$i}} END{if(bad)exit 1; print last}')" || return 1
    [ "$broray_tx_status_last_byte" = 10 ] || return 1
    awk '
      function flush(    i,is_broray,package_rows) {
        if (lines==0) return
        is_broray=0; package_rows=0
        for (i=1;i<=lines;i++) {
          if (row[i] ~ /^Package:[[:space:]]*/) package_rows++
          if (row[i] ~ /^Package:[[:space:]]*broray[[:space:]]*$/) is_broray=1
        }
        if (package_rows!=1) bad=1
        if (is_broray) { found++; removed_pending=1 }
        else {
          for (i=1;i<=lines;i++) print row[i]
        }
        delete row; lines=0
      }
      $0=="" {
        flush()
        if (removed_pending) removed_pending=0
        else print ""
        next
      }
      { row[++lines]=$0 }
      END { flush(); if (bad || found>1) exit 2 }
    ' "$broray_tx_status_projection_input" >"$broray_tx_status_projection_output.part" || {
        rm -f "$broray_tx_status_projection_output.part"
        return 1
    }
    mv -f "$broray_tx_status_projection_output.part" "$broray_tx_status_projection_output"
}

broray_tx_status_without_broray_to()
{
    if [ ! -f "$BRORAY_TX_STATUS_FILE" ]; then : >"$1"; return 0; fi
    [ ! -L "$BRORAY_TX_STATUS_FILE" ] || return 1
    broray_tx_status_foreign_projection "$BRORAY_TX_STATUS_FILE" "$1"
}

# Universal source admission.  Every structurally intact BROray installation
# is accepted independently of its declared version.  Version strings are
# diagnostics only: there is no source-version matrix and no version-selected
# migration registry.
broray_tx_diagnostic_normalize()
{
    # Diagnostic text never participates in admission.  Bound it for evidence
    # files, replace record separators, and accept packaging characters such
    # as ':' and '~' without a version allowlist.
    printf '%s' "${1:-}" | tr '\r\n\t' '   ' | sed 's/[^0-9A-Za-z._+:-]/_/g' | cut -c 1-128
}

# The lock/control prelude may record bounded identity diagnostics, but it
# must not classify the source or select a migration before CLEANUP.  Keep
# this deliberately narrower than broray_tx_source_admit(): no structural
# admission result, sourceClass, or migrationId is produced here.
broray_tx_source_identity_prelude()
{
    broray_tx_status_broray_unique || return 1
    BRORAY_TX_SOURCE_PACKAGE="$(broray_tx_installed_status_field Version 2>/dev/null || printf '')"
    BRORAY_TX_SOURCE_ARCH="$(broray_tx_installed_status_field Architecture 2>/dev/null || printf '')"
    BRORAY_TX_SOURCE_APP=""
    if [ -e "$BRORAY_TX_APP_ROOT/config/version" ] || [ -L "$BRORAY_TX_APP_ROOT/config/version" ]; then
        [ -f "$BRORAY_TX_APP_ROOT/config/version" ] && [ ! -L "$BRORAY_TX_APP_ROOT/config/version" ] || return 1
        BRORAY_TX_SOURCE_APP="$(sed -n '1p' "$BRORAY_TX_APP_ROOT/config/version")"
    fi
    if [ ! -e "$BRORAY_TX_APP_ROOT" ] && [ ! -L "$BRORAY_TX_APP_ROOT" ] &&
       [ -z "$BRORAY_TX_SOURCE_PACKAGE" ] && [ -z "$BRORAY_TX_SOURCE_APP" ]; then
        BRORAY_TX_SOURCE_PACKAGE=absent
        BRORAY_TX_SOURCE_APP=absent
        BRORAY_TX_SOURCE_ARCH=absent
    else
        [ -n "$BRORAY_TX_SOURCE_PACKAGE" ] || BRORAY_TX_SOURCE_PACKAGE=unregistered
        [ -n "$BRORAY_TX_SOURCE_APP" ] || BRORAY_TX_SOURCE_APP=unknown
        [ -n "$BRORAY_TX_SOURCE_ARCH" ] || BRORAY_TX_SOURCE_ARCH=unregistered
        BRORAY_TX_SOURCE_PACKAGE="$(broray_tx_diagnostic_normalize "$BRORAY_TX_SOURCE_PACKAGE")"
        BRORAY_TX_SOURCE_APP="$(broray_tx_diagnostic_normalize "$BRORAY_TX_SOURCE_APP")"
        BRORAY_TX_SOURCE_ARCH="$(broray_tx_diagnostic_normalize "$BRORAY_TX_SOURCE_ARCH")"
    fi
    BRORAY_TX_SOURCE_CLASS=""
    BRORAY_TX_MIGRATION_ID=""
    return 0
}

broray_tx_source_admit()
{
    broray_tx_status_broray_unique || return 1
    BRORAY_TX_SOURCE_PACKAGE="$(broray_tx_installed_status_field Version 2>/dev/null || printf '')"
    BRORAY_TX_SOURCE_ARCH="$(broray_tx_installed_status_field Architecture 2>/dev/null || printf '')"
    BRORAY_TX_SOURCE_APP=""
    if [ -e "$BRORAY_TX_APP_ROOT/config/version" ] || [ -L "$BRORAY_TX_APP_ROOT/config/version" ]; then
        [ -f "$BRORAY_TX_APP_ROOT/config/version" ] && [ ! -L "$BRORAY_TX_APP_ROOT/config/version" ] || return 1
        BRORAY_TX_SOURCE_APP="$(sed -n '1p' "$BRORAY_TX_APP_ROOT/config/version")"
    fi
    if [ ! -e "$BRORAY_TX_APP_ROOT" ] && [ ! -L "$BRORAY_TX_APP_ROOT" ] &&
       [ -z "$BRORAY_TX_SOURCE_PACKAGE" ] && [ -z "$BRORAY_TX_SOURCE_APP" ]; then
        BRORAY_TX_SOURCE_PACKAGE=absent
        BRORAY_TX_SOURCE_APP=absent
        BRORAY_TX_SOURCE_ARCH=absent
        BRORAY_TX_SOURCE_CLASS=absent
        BRORAY_TX_MIGRATION_ID=install-empty-to-3.0
        return 0
    fi

    [ -d "$BRORAY_TX_APP_ROOT" ] && [ ! -L "$BRORAY_TX_APP_ROOT" ] || return 1
    # Structural admission is an all-of functional core, never one surviving
    # identity marker.  Paths and types are version-independent; the version
    # values themselves remain diagnostics and never select a migration.
    for broray_tx_source_core in \
        "$BRORAY_TX_APP_ROOT/bin/broray" \
        "$BRORAY_TX_APP_ROOT/bin/xray" \
        "$BRORAY_TX_APP_ROOT/config/version" \
        "$BRORAY_TX_APP_ROOT/config/config.json"
    do
        [ -f "$broray_tx_source_core" ] && [ ! -L "$broray_tx_source_core" ] &&
            [ -s "$broray_tx_source_core" ] || return 1
    done
    [ -x "$BRORAY_TX_APP_ROOT/bin/broray" ] &&
        [ -x "$BRORAY_TX_APP_ROOT/bin/xray" ] || return 1

    [ -n "$BRORAY_TX_SOURCE_PACKAGE" ] || BRORAY_TX_SOURCE_PACKAGE=unregistered
    [ -n "$BRORAY_TX_SOURCE_APP" ] || BRORAY_TX_SOURCE_APP=unknown
    [ -n "$BRORAY_TX_SOURCE_ARCH" ] || BRORAY_TX_SOURCE_ARCH=unregistered
    BRORAY_TX_SOURCE_PACKAGE="$(broray_tx_diagnostic_normalize "$BRORAY_TX_SOURCE_PACKAGE")"
    BRORAY_TX_SOURCE_APP="$(broray_tx_diagnostic_normalize "$BRORAY_TX_SOURCE_APP")"
    BRORAY_TX_SOURCE_ARCH="$(broray_tx_diagnostic_normalize "$BRORAY_TX_SOURCE_ARCH")"
    BRORAY_TX_SOURCE_CLASS=bro-any-structural
    BRORAY_TX_MIGRATION_ID=preserve-user-state-byte-exact-1
    return 0
}

# Execute only after the environment capability contract has passed.  Object
# admission above proves the core shape; this probe proves that the factual
# source Xray can parse the factual source configuration before snapshotting.
broray_tx_source_functional_verify()
{
    broray_tx_source_admit || return 1
    [ "$BRORAY_TX_SOURCE_CLASS" = absent ] && return 0
    [ "$BRORAY_TX_SOURCE_CLASS" = bro-any-structural ] || return 1
    XRAY_LOCATION_ASSET="$BRORAY_TX_APP_ROOT/bin" \
        "$BRORAY_TX_APP_ROOT/bin/xray" run -test -c \
        "$BRORAY_TX_APP_ROOT/config/config.json" >/dev/null 2>&1
}

broray_tx_recovery_sidecar_verify()
{
    broray_tx_recovery_object="$1"
    broray_tx_recovery_sidecar="$1.sha256"
    if [ ! -e "$broray_tx_recovery_object" ] && [ ! -L "$broray_tx_recovery_object" ] && \
       [ ! -e "$broray_tx_recovery_sidecar" ] && [ ! -L "$broray_tx_recovery_sidecar" ]; then
        return 0
    fi
    [ -f "$broray_tx_recovery_object" ] && [ ! -L "$broray_tx_recovery_object" ] || return 1
    [ -f "$broray_tx_recovery_sidecar" ] && [ ! -L "$broray_tx_recovery_sidecar" ] || return 1
    broray_tx_recovery_expected="$(awk 'NR==1{print $1;exit}' "$broray_tx_recovery_sidecar")"
    case "$broray_tx_recovery_expected" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#broray_tx_recovery_expected}" -eq 64 ] || return 1
    [ "$(broray_tx_sha "$broray_tx_recovery_object")" = "$broray_tx_recovery_expected" ]
}

broray_tx_recovery_operation_validate()
{
    jq -e --arg id "$2" --arg mode "$3" --arg source "$4" --arg target "$5" \
        --arg path "$6" --arg startedAt "$7" --arg lifecycle "$BRORAY_TX_CONTRACT" '
      (type == "object") and
      (((.schemaVersion == 1) and (.contractVersion == "2.0") and
        ((has("sourceAppVersion") | not) or ((.sourceAppVersion | type) == "string"))) or
       ((.schemaVersion == 2) and (.lifecycleContract == $lifecycle) and
        (.startedAt == $startedAt) and
        (((.sourceAppVersion | type) == "string") and ((.sourceAppVersion | length) > 0) and
          ((.sourceClass | type) == "string") and ((.sourceClass | length) > 0) and
          ((.migrationId | type) == "string") and ((.migrationId | length) > 0) or
         ((has("sourceAppVersion")|not) and (has("sourceClass")|not) and
          (has("migrationId")|not) and (.mutationStarted == false))))) and
      (.operationId == $id) and (.mode == $mode) and
      (.sourceVersion == $source) and (.targetVersion == $target) and
      (.transactionPath == $path)
    ' "$1" >/dev/null 2>&1
}

broray_tx_recovery_failure_validate()
{
    jq -e --arg id "$2" '
      (type == "object") and (.schemaVersion == 1) and (.operationId == $id) and
      ((.step | type) == "string") and ((.step | length) > 0) and
      ((.exitCode | type) == "number") and ((.exitCode | floor) == .exitCode)
    ' "$1" >/dev/null 2>&1
}

broray_tx_recovery_forensic_validate()
{
    jq -e --arg id "$2" '
      (type == "object") and (.schemaVersion == 1) and (.operationId == $id)
    ' "$1" >/dev/null 2>&1
}

# A restore has no package candidate or registration proof.  Its authoritative
# success record is instead bound to the exact protected archive, the current
# rollback snapshot and both completed restore postchecks.  Recovery calls this
# before deciding that a stale mutation is cleanup-only, so every referenced
# volatile object must still be a bounded regular file and agree byte-for-byte
# with the terminal record.
broray_tx_restore_success_terminal_validate()
{
    broray_tx_restore_validate_terminal="$1"
    [ "$BRORAY_TX_MODE" = restore ] || return 1
    [ -f "$broray_tx_restore_validate_terminal" ] && [ ! -L "$broray_tx_restore_validate_terminal" ] || return 1
    broray_tx_file_cap_kb "$broray_tx_restore_validate_terminal" "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" || return 1
    for broray_tx_restore_terminal_file in \
        backup.tar.gz snapshot.verified mutation.started restore-input.tar.gz restore-candidate.manifest \
        restore-postcheck.manifest evidence/events.tsv evidence/restore-input.json
    do
        [ -f "$BRORAY_TX_WORK/$broray_tx_restore_terminal_file" ] &&
            [ ! -L "$BRORAY_TX_WORK/$broray_tx_restore_terminal_file" ] || return 1
    done
    broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/events.tsv" \
        "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" || return 1
    broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/restore-input.json" \
        "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" || return 1
    [ "$(wc -l <"$BRORAY_TX_WORK/snapshot.verified" | tr -d ' ')" -eq 1 ] || return 1
    broray_tx_restore_terminal_snapshot_sha="$(sed -n '1p' "$BRORAY_TX_WORK/snapshot.verified")"
    broray_tx_restore_terminal_archive_sha="$(broray_tx_sha "$BRORAY_TX_WORK/restore-input.tar.gz")" || return 1
    broray_tx_restore_terminal_manifest_sha="$(broray_tx_sha "$BRORAY_TX_WORK/restore-postcheck.manifest")" || return 1
    for broray_tx_restore_terminal_sha in \
        "$broray_tx_restore_terminal_snapshot_sha" \
        "$broray_tx_restore_terminal_archive_sha" \
        "$broray_tx_restore_terminal_manifest_sha"
    do
        case "$broray_tx_restore_terminal_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
        [ "${#broray_tx_restore_terminal_sha}" -eq 64 ] || return 1
    done
    [ "$(broray_tx_sha "$BRORAY_TX_WORK/backup.tar.gz")" = \
        "$broray_tx_restore_terminal_snapshot_sha" ] || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/restore-candidate.manifest" \
        "$BRORAY_TX_WORK/restore-postcheck.manifest" || return 1
    [ "$(awk -F '\t' '$3=="restore-postcheck-pass"{n++}END{print n+0}' \
        "$BRORAY_TX_WORK/evidence/events.tsv")" -eq 2 ] || return 1
    jq -e --arg sha "$broray_tx_restore_terminal_archive_sha" '
      (type=="object") and (.schemaVersion==2) and (.status=="PASS") and
      (.format=="BROray protected backup/2") and (.sha256==$sha) and
      (.memberPathsSafe==true) and (.objectTypesSafe==true) and
      (.rootSetRegistered==true) and (.manifestVerified==true)
    ' "$BRORAY_TX_WORK/evidence/restore-input.json" >/dev/null 2>&1 || return 1
    jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg target "$BRORAY_TX_TARGET_PACKAGE" \
        --arg snapshot "$broray_tx_restore_terminal_snapshot_sha" \
        --arg archive "$broray_tx_restore_terminal_archive_sha" \
        --arg manifest "$broray_tx_restore_terminal_manifest_sha" '
      (type=="object") and
      ((keys|sort)==(["schemaVersion","status","operationId","mode","targetVersion",
                      "snapshotSha256","restoreArchiveSha256","restoreManifestSha256",
                      "restorePostchecks","cleanupComplete","committedAt"]|sort)) and
      (.schemaVersion==1) and (.status=="SUCCESS_COMMITTED") and
      (.operationId==$id) and (.mode=="restore") and (.targetVersion==$target) and
      (.snapshotSha256==$snapshot) and (.restoreArchiveSha256==$archive) and
      (.restoreManifestSha256==$manifest) and (.restorePostchecks==2) and
      (.cleanupComplete==false) and
      ((.committedAt|type)=="string") and ((.committedAt|length)>0)
    ' "$broray_tx_restore_validate_terminal" >/dev/null 2>&1
}

broray_tx_recovery_retired_rollback_failed_admit()
{
    broray_tx_retired_recovery_work="$1"
    case "$broray_tx_retired_recovery_work" in
        "$BRORAY_TX_TMP_BASE"/broray-update-*) ;;
        *) return 1 ;;
    esac
    broray_tx_retired_recovery_id="${broray_tx_retired_recovery_work##*/broray-update-}"
    broray_tx_valid_id "$broray_tx_retired_recovery_id" || return 1
    [ "$broray_tx_retired_recovery_work" = \
        "$BRORAY_TX_TMP_BASE/broray-update-$broray_tx_retired_recovery_id" ] || return 1
    [ -d "$broray_tx_retired_recovery_work" ] &&
        [ ! -L "$broray_tx_retired_recovery_work" ] || return 1
    for broray_tx_retired_recovery_absent in \
        backup.tar.gz owner-identity.tsv owner-pid active.pid
    do
        [ ! -e "$broray_tx_retired_recovery_work/$broray_tx_retired_recovery_absent" ] &&
            [ ! -L "$broray_tx_retired_recovery_work/$broray_tx_retired_recovery_absent" ] || return 1
    done
    broray_tx_retired_recovery_snapshot="$broray_tx_retired_recovery_work/rollback-failed.forensic.tar.gz"
    [ -f "$broray_tx_retired_recovery_snapshot" ] &&
        [ ! -L "$broray_tx_retired_recovery_snapshot" ] &&
        [ -s "$broray_tx_retired_recovery_snapshot" ] || return 1
    broray_tx_retired_recovery_sha="$(broray_tx_sha "$broray_tx_retired_recovery_snapshot")" || return 1
    case "$broray_tx_retired_recovery_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#broray_tx_retired_recovery_sha}" -eq 64 ] || return 1
    broray_tx_retired_recovery_history="$BRORAY_TX_OPERATION_ROOT/$broray_tx_retired_recovery_id"
    broray_tx_retired_recovery_result="$broray_tx_retired_recovery_history/recovery-result.json"
    [ -d "$broray_tx_retired_recovery_history" ] &&
        [ ! -L "$broray_tx_retired_recovery_history" ] &&
        [ -f "$broray_tx_retired_recovery_result" ] &&
        [ ! -L "$broray_tx_retired_recovery_result" ] || return 1
    jq -e --arg id "$broray_tx_retired_recovery_id" \
        --arg work "$broray_tx_retired_recovery_work" \
        --arg snapshot "$broray_tx_retired_recovery_snapshot" \
        --arg sha "$broray_tx_retired_recovery_sha" '
      (type=="object") and (.schemaVersion==2) and
      (.kind=="stale-rollback-failed-recovery") and
      (.operationId==$id) and (.transactionPath==$work) and
      (.lockRemoved==true) and (.currentSnapshotRetired==true) and
      (.historicalInput==false) and (.forensicEvidencePersisted==true) and
      (.workspacePreserved==true) and (.historyPreserved==true) and
      (.forensicSnapshot==$snapshot) and (.forensicSnapshotSha256==$sha)
    ' "$broray_tx_retired_recovery_result" >/dev/null 2>&1
}

broray_tx_recovery_no_second_live()
{
    if [ -e "$BRORAY_TX_CURRENT" ] || [ -L "$BRORAY_TX_CURRENT" ]; then
        [ -f "$BRORAY_TX_CURRENT" ] && [ ! -L "$BRORAY_TX_CURRENT" ] || return 1
        [ "$(wc -l <"$BRORAY_TX_CURRENT" | tr -d ' ')" -eq 1 ] || return 1
        broray_tx_recovery_current_id="${1##*/broray-update-}"
        broray_tx_valid_id "$broray_tx_recovery_current_id" || return 1
        [ "$1" = "$BRORAY_TX_TMP_BASE/broray-update-$broray_tx_recovery_current_id" ] || return 1
        [ -f "$1/backup.tar.gz" ] && [ ! -L "$1/backup.tar.gz" ] || return 1
        broray_tx_recovery_current_snapshot="$(broray_tx_sha "$1/backup.tar.gz")" || return 1
        case "$broray_tx_recovery_current_snapshot" in ''|*[!0-9a-f]*) return 1 ;; esac
        [ "${#broray_tx_recovery_current_snapshot}" -eq 64 ] || return 1
        jq -e --arg id "$broray_tx_recovery_current_id" --arg work "$1" \
            --arg lifecycle "$BRORAY_TX_CONTRACT" \
            --arg snapshot "$broray_tx_recovery_current_snapshot" '
          (type=="object") and
          ((keys|sort)==(["schemaVersion","operationId","workspace","origin",
                          "lifecycleContract","snapshotSha256","applicationPass"]|sort)) and
          (.schemaVersion==1) and (.operationId==$id) and (.workspace==$work) and
          ((.origin|type)=="string") and ((.origin|length)>0) and
          (.lifecycleContract==$lifecycle) and (.snapshotSha256==$snapshot) and
          (.applicationPass==true)
        ' "$BRORAY_TX_CURRENT" >/dev/null 2>&1 || return 1
    fi
    for broray_tx_recovery_other in "$BRORAY_TX_TMP_BASE"/broray-update-*; do
        [ "$broray_tx_recovery_other" != "$1" ] || continue
        [ -e "$broray_tx_recovery_other" ] || [ -L "$broray_tx_recovery_other" ] || continue
        [ -d "$broray_tx_recovery_other" ] && [ ! -L "$broray_tx_recovery_other" ] || return 1
        # Recovery is authorized only with one canonical current snapshot.
        # A second backup archive is a blocker even when its process owner is
        # already dead: choosing either snapshot would make the other a future
        # orphan input and violate the single-current-operation invariant.
        if [ -e "$broray_tx_recovery_other/backup.tar.gz" ] ||
           [ -L "$broray_tx_recovery_other/backup.tar.gz" ]; then
            return 1
        fi
        if [ -e "$broray_tx_recovery_other/owner-identity.tsv" ] ||
           [ -L "$broray_tx_recovery_other/owner-identity.tsv" ]; then
            broray_tx_control_owner_require_stale "$broray_tx_recovery_other/owner-identity.tsv" || return 1
        elif [ -e "$broray_tx_recovery_other/owner-pid" ] ||
             [ -L "$broray_tx_recovery_other/owner-pid" ] ||
             [ -e "$broray_tx_recovery_other/active.pid" ] ||
             [ -L "$broray_tx_recovery_other/active.pid" ]; then
            # A numeric PID without its birth token/exe/argv binding is
            # intrinsically ambiguous and can never authorize a cleanup.
            return 1
        else
            # An ownerless/malformed operation-shaped directory may be a
            # crashed control prelude.  Ignore only the exact committed
            # rollback-failed forensic shape whose durable result is bound to
            # these snapshot bytes; every other ownerless tree remains a
            # fail-closed blocker while a different recovery mutates state.
            broray_tx_recovery_retired_rollback_failed_admit \
                "$broray_tx_recovery_other" || return 1
        fi
    done
    return 0
}

broray_tx_recovery_history_validate()
{
    broray_tx_recovery_history="$1"
    [ -e "$broray_tx_recovery_history" ] || [ -L "$broray_tx_recovery_history" ] || return 0
    [ -d "$broray_tx_recovery_history" ] && [ ! -L "$broray_tx_recovery_history" ] || return 1
    if [ -e "$broray_tx_recovery_history/operation.json" ] || [ -L "$broray_tx_recovery_history/operation.json" ]; then
        [ -f "$broray_tx_recovery_history/operation.json" ] && [ ! -L "$broray_tx_recovery_history/operation.json" ] || return 1
        broray_tx_recovery_operation_validate "$broray_tx_recovery_history/operation.json" \
            "$2" "$3" "$4" "$5" "$6" "$7" || return 1
    fi
    if [ -e "$broray_tx_recovery_history/outcome" ] || [ -L "$broray_tx_recovery_history/outcome" ]; then
        [ -f "$broray_tx_recovery_history/outcome" ] && [ ! -L "$broray_tx_recovery_history/outcome" ] || return 1
        [ "$(sed -n '1p' "$broray_tx_recovery_history/outcome")" = rollback-failed ] || return 1
    fi
    for broray_tx_recovery_pair in \
        "lock-operation-id:$2" "lock-operation-type:$3" \
        "lock-source-version:$4" "lock-target-version:$5" "lock-started-at:$7" \
        "lock-original-owner-identity.tsv:$8"
    do
        broray_tx_recovery_history_file="${broray_tx_recovery_pair%%:*}"
        broray_tx_recovery_history_value="${broray_tx_recovery_pair#*:}"
        if [ -e "$broray_tx_recovery_history/$broray_tx_recovery_history_file" ] || [ -L "$broray_tx_recovery_history/$broray_tx_recovery_history_file" ]; then
            [ -f "$broray_tx_recovery_history/$broray_tx_recovery_history_file" ] && [ ! -L "$broray_tx_recovery_history/$broray_tx_recovery_history_file" ] || return 1
            [ "$(sed -n '1p' "$broray_tx_recovery_history/$broray_tx_recovery_history_file")" = "$broray_tx_recovery_history_value" ] || return 1
        fi
    done
    return 0
}

# Return 0 only for no control state or a fully recovered stale state.  Return
# 2 for every live, foreign, malformed, incomplete or ambiguous classification.
broray_tx_recover_stale_rollback_failed()
{
    broray_tx_recovery_saved_operation_id="$BRORAY_TX_OPERATION_ID"
    broray_tx_recovery_saved_mode="$BRORAY_TX_MODE"
    broray_tx_recovery_saved_work="$BRORAY_TX_WORK"
    broray_tx_recovery_saved_source="$BRORAY_TX_SOURCE_PACKAGE"
    broray_tx_recovery_marker_present=false
    if [ -e "$BRORAY_TX_LEGACY_MARKER" ] || [ -L "$BRORAY_TX_LEGACY_MARKER" ]; then
        broray_tx_recovery_marker_present=true
    fi
    if [ ! -e "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ]; then
        [ "$broray_tx_recovery_marker_present" = false ] || return 2
        return 0
    fi
    [ -d "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] || return 2
    broray_tx_control_owner_assert_self "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" || return 2
    for broray_tx_recovery_lock_file in operation-id owner-identity.tsv operation-type started-at source-version target-version; do
        [ -f "$BRORAY_TX_LOCK_DIR/$broray_tx_recovery_lock_file" ] && [ ! -L "$BRORAY_TX_LOCK_DIR/$broray_tx_recovery_lock_file" ] || return 2
    done
    for broray_tx_recovery_lock_entry in "$BRORAY_TX_LOCK_DIR"/* "$BRORAY_TX_LOCK_DIR"/.[!.]* "$BRORAY_TX_LOCK_DIR"/..?*; do
        [ -e "$broray_tx_recovery_lock_entry" ] || [ -L "$broray_tx_recovery_lock_entry" ] || continue
        case "${broray_tx_recovery_lock_entry##*/}" in
            operation-id|owner-identity.tsv|operation-type|started-at|source-version|target-version)
                [ -f "$broray_tx_recovery_lock_entry" ] && [ ! -L "$broray_tx_recovery_lock_entry" ] || return 2 ;;
            *) return 2 ;;
        esac
    done
    broray_tx_recovery_id="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-id")"
    broray_tx_recovery_type="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-type")"
    broray_tx_recovery_started="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/started-at")"
    broray_tx_recovery_source="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/source-version")"
    broray_tx_recovery_target="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/target-version")"
    for broray_tx_recovery_lock_file in operation-id owner-identity.tsv operation-type started-at source-version target-version; do
        [ "$(wc -l <"$BRORAY_TX_LOCK_DIR/$broray_tx_recovery_lock_file" | tr -d ' ')" = 1 ] || return 2
    done
    broray_tx_valid_id "$broray_tx_recovery_id" || return 2
    broray_tx_control_owner_authorize_recovery "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" || return 2
    broray_tx_recovery_control_owner_state="$BRORAY_TX_CONTROL_OWNER_STATE"
    broray_tx_recovery_owner_state="${BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE:-$BRORAY_TX_CONTROL_OWNER_STATE}"
    broray_tx_recovery_pid="$BRORAY_TX_CONTROL_OWNER_PID"
    case "$broray_tx_recovery_type" in install|update|reinstall|opkg-upgrade|restore) ;; *) return 2 ;; esac
    case "$broray_tx_recovery_started" in
        ????-??-??T??:??:??Z) ;;
        *) return 2 ;;
    esac
    [ -n "$broray_tx_recovery_source" ] && [ -n "$broray_tx_recovery_target" ] || return 2
    # Source/target labels are diagnostics only.  Update versus reinstall is
    # bound later by exact candidate bytes and structural source identity,
    # never by string equality of arbitrary source version labels.
    # Exact process identity, rather than a numeric PID, authorized this stale
    # classification.  A reused PID is safe because no signal is ever sent.
    broray_tx_recovery_path="$BRORAY_TX_TMP_BASE/broray-update-$broray_tx_recovery_id"
    [ -d "$broray_tx_recovery_path" ] && [ ! -L "$broray_tx_recovery_path" ] || return 2
    broray_tx_recovery_original_owner_file="$broray_tx_recovery_path/owner-identity.tsv"
    [ -f "$broray_tx_recovery_original_owner_file" ] &&
        [ ! -L "$broray_tx_recovery_original_owner_file" ] &&
        broray_tx_control_owner_assert_self "$broray_tx_recovery_original_owner_file" || return 2
    broray_tx_control_owner_identity_read "$broray_tx_recovery_original_owner_file" || return 2
    broray_tx_recovery_owner_identity="$BRORAY_TX_RECOVERY_PREDECESSOR_OWNER_IDENTITY"
    broray_tx_recovery_no_second_live "$broray_tx_recovery_path" || return 2
    for broray_tx_recovery_critical in operation.json backup.tar.gz; do
        [ -f "$broray_tx_recovery_path/$broray_tx_recovery_critical" ] && [ ! -L "$broray_tx_recovery_path/$broray_tx_recovery_critical" ] || return 2
    done
    broray_tx_recovery_archive_sha="$(broray_tx_sha "$broray_tx_recovery_path/backup.tar.gz")"
    case "$broray_tx_recovery_archive_sha" in ''|*[!0-9a-f]*) return 2 ;; esac
    [ "${#broray_tx_recovery_archive_sha}" -eq 64 ] || return 2
    broray_tx_recovery_operation_validate "$broray_tx_recovery_path/operation.json" \
        "$broray_tx_recovery_id" "$broray_tx_recovery_type" "$broray_tx_recovery_source" \
        "$broray_tx_recovery_target" "$broray_tx_recovery_path" "$broray_tx_recovery_started" || return 2
    broray_tx_recovery_outcome_present=false
    if [ -e "$broray_tx_recovery_path/outcome" ] || [ -L "$broray_tx_recovery_path/outcome" ]; then
        [ -f "$broray_tx_recovery_path/outcome" ] && [ ! -L "$broray_tx_recovery_path/outcome" ] || return 2
        [ "$(sed -n '1p' "$broray_tx_recovery_path/outcome")" = rollback-failed ] &&
            [ "$(wc -l <"$broray_tx_recovery_path/outcome" | tr -d ' ')" -eq 1 ] || return 2
        broray_tx_recovery_outcome_present=true
    fi
    broray_tx_recovery_rollback_failure_present=false
    if [ -e "$broray_tx_recovery_path/rollback-failure.json" ] ||
       [ -L "$broray_tx_recovery_path/rollback-failure.json" ]; then
        [ -f "$broray_tx_recovery_path/rollback-failure.json" ] &&
            [ ! -L "$broray_tx_recovery_path/rollback-failure.json" ] || return 2
        broray_tx_recovery_failure_validate "$broray_tx_recovery_path/rollback-failure.json" \
            "$broray_tx_recovery_id" || return 2
        broray_tx_recovery_rollback_failure_present=true
    fi
    broray_tx_recovery_failure_present=false
    if [ -e "$broray_tx_recovery_path/failure.json" ] || [ -L "$broray_tx_recovery_path/failure.json" ]; then
        [ -f "$broray_tx_recovery_path/failure.json" ] &&
            [ ! -L "$broray_tx_recovery_path/failure.json" ] || return 2
        broray_tx_recovery_forensic_validate "$broray_tx_recovery_path/failure.json" \
            "$broray_tx_recovery_id" || return 2
        broray_tx_recovery_failure_present=true
    fi
    if [ "$broray_tx_recovery_marker_present" = true ]; then
        [ -f "$BRORAY_TX_LEGACY_MARKER" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER" ] || return 2
        broray_tx_recovery_marker_snapshot="$broray_tx_recovery_archive_sha"
        case "$broray_tx_recovery_marker_snapshot" in ''|*[!0-9a-f]*) return 2 ;; esac
        [ "$(printf %s "$broray_tx_recovery_marker_snapshot" | wc -c | tr -d ' ')" -eq 64 ] || return 2
        broray_tx_recovery_marker_expected="$(printf '{"lifecycleContract":"%s","mode":"%s","operationId":"%s","phase":"rollback-failed","schemaVersion":2,"snapshotSha256":"%s","sourceVersion":"%s","startedAt":"%s","status":"rollback-failed","targetVersion":"%s","transactionPath":"%s"}' \
            "$BRORAY_TX_CONTRACT" "$broray_tx_recovery_type" "$broray_tx_recovery_id" \
            "$broray_tx_recovery_marker_snapshot" "$broray_tx_recovery_source" "$broray_tx_recovery_started" \
            "$broray_tx_recovery_target" "$broray_tx_recovery_path")"
        [ "$(sed -n '1p' "$BRORAY_TX_LEGACY_MARKER")" = "$broray_tx_recovery_marker_expected" ] &&
            [ "$(wc -l <"$BRORAY_TX_LEGACY_MARKER" | tr -d ' ')" -eq 1 ] || return 2
    fi
    [ "$broray_tx_recovery_outcome_present" = true ] ||
        [ "$broray_tx_recovery_marker_present" = true ] || return 2
    broray_tx_recovery_source_app="$(jq -er '
      if ((.sourceAppVersion | type) == "string") then .sourceAppVersion
      elif (.schemaVersion == 1) then .sourceVersion
      else empty end
    ' "$broray_tx_recovery_path/operation.json" 2>/dev/null)" || return 2
    broray_tx_recovery_source_class="$(jq -er '.sourceClass' "$broray_tx_recovery_path/operation.json" 2>/dev/null)" || return 2
    # The authenticated capsule, not volatile sidecars, authorizes this stale
    # classification.  Verify it in scratch without changing caller globals,
    # then independently admit the factual live source.  OPKG registration and
    # all version labels remain optional diagnostics; an unregistered but
    # structurally intact BROray source must not become a permanent blocker.
    (
        BRORAY_TX_WORK="$broray_tx_recovery_path"
        BRORAY_TX_OPERATION_ID="$broray_tx_recovery_id"
        BRORAY_TX_MODE="$broray_tx_recovery_type"
        BRORAY_TX_SOURCE_PACKAGE="$broray_tx_recovery_source"
        BRORAY_TX_SOURCE_APP="$broray_tx_recovery_source_app"
        BRORAY_TX_SOURCE_CLASS="$broray_tx_recovery_source_class"
        BRORAY_TX_TARGET_PACKAGE="$broray_tx_recovery_target"
        broray_tx_recovery_snapshot_admit_readonly &&
        { [ "$BRORAY_TX_RECOVERY_FACTUAL_STATE" = source-exact ] ||
          [ "$BRORAY_TX_RECOVERY_FACTUAL_STATE" = source-changed ]; }
    ) || return 2
    case "$broray_tx_recovery_source_class" in
      absent)
        [ "$broray_tx_recovery_source_app" = absent ] || return 2
        ( broray_tx_source_admit && [ "$BRORAY_TX_SOURCE_CLASS" = absent ] ) || return 2
        ;;
      bro-any-structural)
        ( broray_tx_source_admit && [ "$BRORAY_TX_SOURCE_CLASS" = bro-any-structural ] ) || return 2
        ;;
      *) return 2 ;;
    esac
    if [ -e "$broray_tx_recovery_path/backup.tar.gz.sha256" ] ||
       [ -L "$broray_tx_recovery_path/backup.tar.gz.sha256" ]; then
        broray_tx_recovery_sidecar_verify "$broray_tx_recovery_path/backup.tar.gz" || return 2
    fi
    if [ -e "$broray_tx_recovery_path/user-data.tar.gz" ] ||
       [ -L "$broray_tx_recovery_path/user-data.tar.gz" ] ||
       [ -e "$broray_tx_recovery_path/user-data.tar.gz.sha256" ] ||
       [ -L "$broray_tx_recovery_path/user-data.tar.gz.sha256" ]; then
        broray_tx_recovery_sidecar_verify "$broray_tx_recovery_path/user-data.tar.gz" || return 2
    fi
    if [ -e "$broray_tx_recovery_path/candidate.ipk" ] || [ -L "$broray_tx_recovery_path/candidate.ipk" ] || \
       [ -e "$broray_tx_recovery_path/candidate-validated-identity.json" ] || [ -L "$broray_tx_recovery_path/candidate-validated-identity.json" ]; then
        [ -f "$broray_tx_recovery_path/candidate.ipk" ] && [ ! -L "$broray_tx_recovery_path/candidate.ipk" ] || return 2
        [ -f "$broray_tx_recovery_path/candidate-validated-identity.json" ] && [ ! -L "$broray_tx_recovery_path/candidate-validated-identity.json" ] || return 2
        broray_tx_recovery_candidate_actual_sha="$(broray_tx_sha "$broray_tx_recovery_path/candidate.ipk")" || return 2
        broray_tx_recovery_candidate_actual_bytes="$(wc -c <"$broray_tx_recovery_path/candidate.ipk" | tr -d ' ')"
        broray_tx_number "$broray_tx_recovery_candidate_actual_bytes" || return 2
        jq -e --arg target "$broray_tx_recovery_target" \
            --arg sha "$broray_tx_recovery_candidate_actual_sha" \
            --argjson bytes "$broray_tx_recovery_candidate_actual_bytes" '
          (type == "object") and ((.schemaVersion == 2) or (.schemaVersion == 3)) and (.package == "broray") and
          (((.version | type) == "string") and (.version == $target)) and
          (((.releaseId | type) == "string") and (.releaseId == $target)) and
          (if (.schemaVersion == 3) then
             (((.sha256 | type) == "string") and (.sha256 == $sha)) and
             (((.sizeBytes | type) == "number") and (.sizeBytes == $bytes))
           else ((has("sha256") | not) and (has("sizeBytes") | not)) end)
        ' "$broray_tx_recovery_path/candidate-validated-identity.json" >/dev/null 2>&1 || return 2
    fi
    broray_tx_recovery_history="$BRORAY_TX_OPERATION_ROOT/$broray_tx_recovery_id"
    broray_tx_recovery_history_present=false
    if [ -e "$broray_tx_recovery_history" ] || [ -L "$broray_tx_recovery_history" ]; then
        broray_tx_recovery_history_present=true
    fi
    broray_tx_recovery_original_owner_history="$broray_tx_recovery_history/lock-original-owner-identity.tsv"
    if [ -e "$broray_tx_recovery_original_owner_history" ] ||
       [ -L "$broray_tx_recovery_original_owner_history" ]; then
        [ -f "$broray_tx_recovery_original_owner_history" ] &&
            [ ! -L "$broray_tx_recovery_original_owner_history" ] &&
            broray_tx_control_owner_require_stale "$broray_tx_recovery_original_owner_history" || return 2
        [ "$BRORAY_TX_RECOVERY_PREDECESSOR_PROOF" = \
            "$BRORAY_TX_TMP_BASE/.broray-recovery-predecessor-$broray_tx_recovery_id.tsv" ] &&
            broray_tx_files_equal "$BRORAY_TX_RECOVERY_PREDECESSOR_PROOF" \
                "$broray_tx_recovery_original_owner_history" || return 2
        broray_tx_recovery_owner_identity="$(sed -n '1p' "$broray_tx_recovery_original_owner_history")"
    fi
    [ -n "$broray_tx_recovery_owner_identity" ] || return 2
    broray_tx_recovery_history_validate "$broray_tx_recovery_history" "$broray_tx_recovery_id" \
        "$broray_tx_recovery_type" "$broray_tx_recovery_source" "$broray_tx_recovery_target" \
        "$broray_tx_recovery_path" "$broray_tx_recovery_started" "$broray_tx_recovery_owner_identity" || return 2

    [ -e "$BRORAY_TX_OPERATION_ROOT" ] || [ -L "$BRORAY_TX_OPERATION_ROOT" ] || mkdir -p "$BRORAY_TX_OPERATION_ROOT" || return 2
    [ -d "$BRORAY_TX_OPERATION_ROOT" ] && [ ! -L "$BRORAY_TX_OPERATION_ROOT" ] || return 2
    if [ ! -e "$broray_tx_recovery_history" ] && [ ! -L "$broray_tx_recovery_history" ]; then
        mkdir "$broray_tx_recovery_history" || return 2
        chmod 700 "$broray_tx_recovery_history" 2>/dev/null || true
    fi
    if [ ! -e "$broray_tx_recovery_original_owner_history" ] &&
       [ ! -L "$broray_tx_recovery_original_owner_history" ]; then
        printf '%s\n' "$broray_tx_recovery_owner_identity" | awk -F '\t' '
          NF==4 && $1~/^[0-9]+$/ && $1!="0" && $2~/^[0-9]+$/ && $2!="0" &&
          $3~/^\// && $4~/^[0-9a-f]+$/ && length($4)==64 { ok=1 }
          END { exit ok ? 0 : 1 }
        ' || return 2
        broray_tx_recovery_original_owner_part="$broray_tx_recovery_original_owner_history.part"
        if [ -e "$broray_tx_recovery_original_owner_part" ] ||
           [ -L "$broray_tx_recovery_original_owner_part" ]; then
            [ -f "$broray_tx_recovery_original_owner_part" ] &&
                [ ! -L "$broray_tx_recovery_original_owner_part" ] || return 2
            if [ "$(sed -n '1p' "$broray_tx_recovery_original_owner_part")" != \
                    "$broray_tx_recovery_owner_identity" ] ||
               [ "$(wc -l <"$broray_tx_recovery_original_owner_part" | tr -d ' ')" -ne 1 ]; then
                rm -f "$broray_tx_recovery_original_owner_part" || return 2
                [ ! -e "$broray_tx_recovery_original_owner_part" ] &&
                    [ ! -L "$broray_tx_recovery_original_owner_part" ] || return 2
            fi
        fi
        if [ ! -e "$broray_tx_recovery_original_owner_part" ] &&
           [ ! -L "$broray_tx_recovery_original_owner_part" ]; then
            printf '%s\n' "$broray_tx_recovery_owner_identity" \
                >"$broray_tx_recovery_original_owner_part" || return 2
            chmod 600 "$broray_tx_recovery_original_owner_part" 2>/dev/null || true
        fi
        [ -f "$broray_tx_recovery_original_owner_part" ] &&
            [ ! -L "$broray_tx_recovery_original_owner_part" ] &&
            [ "$(sed -n '1p' "$broray_tx_recovery_original_owner_part")" = \
                "$broray_tx_recovery_owner_identity" ] &&
            [ "$(wc -l <"$broray_tx_recovery_original_owner_part" | tr -d ' ')" -eq 1 ] || return 2
        mv -f "$broray_tx_recovery_original_owner_part" \
            "$broray_tx_recovery_original_owner_history" || return 2
    fi
    [ "$BRORAY_TX_RECOVERY_PREDECESSOR_PROOF" = \
        "$BRORAY_TX_TMP_BASE/.broray-recovery-predecessor-$broray_tx_recovery_id.tsv" ] &&
        broray_tx_files_equal "$BRORAY_TX_RECOVERY_PREDECESSOR_PROOF" \
            "$broray_tx_recovery_original_owner_history" || return 2
    broray_tx_recovery_bundle_base="$broray_tx_recovery_history/interrupted-recovery-$(date '+%s')-$$"
    broray_tx_recovery_bundle_index=0
    while :; do
        broray_tx_recovery_bundle="$broray_tx_recovery_bundle_base-$broray_tx_recovery_bundle_index"
        if [ ! -e "$broray_tx_recovery_bundle" ] && [ ! -L "$broray_tx_recovery_bundle" ]; then
            break
        fi
        broray_tx_recovery_bundle_index=$((broray_tx_recovery_bundle_index + 1))
        [ "$broray_tx_recovery_bundle_index" -lt 100 ] || return 2
    done
    mkdir "$broray_tx_recovery_bundle" || return 2
    chmod 700 "$broray_tx_recovery_bundle" 2>/dev/null || true
    for broray_tx_recovery_copy in operation.json outcome rollback-failure.json failure.json; do
        [ -e "$broray_tx_recovery_path/$broray_tx_recovery_copy" ] ||
            [ -L "$broray_tx_recovery_path/$broray_tx_recovery_copy" ] || continue
        [ -f "$broray_tx_recovery_path/$broray_tx_recovery_copy" ] &&
            [ ! -L "$broray_tx_recovery_path/$broray_tx_recovery_copy" ] || return 2
        cp -p "$broray_tx_recovery_path/$broray_tx_recovery_copy" "$broray_tx_recovery_bundle/workspace-$broray_tx_recovery_copy" || return 2
    done
    for broray_tx_recovery_copy in operation-id operation-type started-at source-version target-version; do
        cp -p "$BRORAY_TX_LOCK_DIR/$broray_tx_recovery_copy" "$broray_tx_recovery_bundle/lock-$broray_tx_recovery_copy" || return 2
    done
    cp -p "$broray_tx_recovery_original_owner_history" \
        "$broray_tx_recovery_bundle/lock-original-owner-identity.tsv" || return 2
    cp -p "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" \
        "$broray_tx_recovery_bundle/lock-recovery-owner-identity.tsv" || return 2
    if [ "$broray_tx_recovery_marker_present" = true ]; then
        cp -p "$BRORAY_TX_LEGACY_MARKER" "$broray_tx_recovery_bundle/current-operation.json" || return 2
    fi
    if [ -f "$broray_tx_recovery_history/recovery-result.json" ] && [ ! -L "$broray_tx_recovery_history/recovery-result.json" ]; then
        cp -p "$broray_tx_recovery_history/recovery-result.json" "$broray_tx_recovery_bundle/prior-recovery-result.json" || return 2
    elif [ -e "$broray_tx_recovery_history/recovery-result.json" ] || [ -L "$broray_tx_recovery_history/recovery-result.json" ]; then
        return 2
    fi
    : >"$broray_tx_recovery_bundle/forensic.sha256" || return 2
    for broray_tx_recovery_hash_file in "$broray_tx_recovery_bundle"/*; do
        [ -f "$broray_tx_recovery_hash_file" ] && [ ! -L "$broray_tx_recovery_hash_file" ] || continue
        [ "${broray_tx_recovery_hash_file##*/}" != forensic.sha256 ] || continue
        printf '%s  %s\n' "$(broray_tx_sha "$broray_tx_recovery_hash_file")" "${broray_tx_recovery_hash_file##*/}" >>"$broray_tx_recovery_bundle/forensic.sha256" || return 2
    done
    broray_tx_recovery_candidate_sha=null
    broray_tx_recovery_candidate_present=false
    if [ -f "$broray_tx_recovery_path/candidate.ipk" ] && [ ! -L "$broray_tx_recovery_path/candidate.ipk" ]; then
        broray_tx_recovery_candidate_sha="$(broray_tx_sha "$broray_tx_recovery_path/candidate.ipk")"
        broray_tx_recovery_candidate_present=true
    fi
    jq -nc --arg operationId "$broray_tx_recovery_id" --arg operationType "$broray_tx_recovery_type" \
        --arg sourceVersion "$broray_tx_recovery_source" --arg sourceAppVersion "$broray_tx_recovery_source_app" \
        --arg targetVersion "$broray_tx_recovery_target" --arg transactionPath "$broray_tx_recovery_path" \
        --arg startedAt "$broray_tx_recovery_started" --arg recoveredAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
        --arg candidateSha256 "$broray_tx_recovery_candidate_sha" \
        --arg ownerIdentityState "$broray_tx_recovery_owner_state" \
        --arg controlOwnerState "$broray_tx_recovery_control_owner_state" \
        --argjson markerPresent "$broray_tx_recovery_marker_present" \
        --argjson rollbackFailurePresent "$broray_tx_recovery_rollback_failure_present" \
        --argjson failurePresent "$broray_tx_recovery_failure_present" \
        --argjson outcomePresent "$broray_tx_recovery_outcome_present" \
        --argjson historyPresent "$broray_tx_recovery_history_present" \
        --argjson candidatePresent "$broray_tx_recovery_candidate_present" '
      {schemaVersion:2,kind:"stale-rollback-failed-recovery",operationId:$operationId,
       operationType:$operationType,sourceVersion:$sourceVersion,sourceAppVersion:$sourceAppVersion,
       targetVersion:$targetVersion,transactionPath:$transactionPath,
       startedAt:$startedAt,markerPresent:$markerPresent,markerEvidenceRecovered:$markerPresent,
       ownerIdentityPresent:true,originalOwnerIdentityState:$ownerIdentityState,
       controlOwnerState:$controlOwnerState,staleOwnerVerified:true,installedStateVerified:true,
       capsuleVerified:true,independentStructuralSourceVerified:true,
       optionalDiagnostics:{outcome:$outcomePresent,rollbackFailure:$rollbackFailurePresent,failure:$failurePresent,
                            priorHistory:$historyPresent,candidate:$candidatePresent},
       identityEvidence:(["lock-control","workspace-operation","self-contained-snapshot",
                          "independent-structural-source","transaction-path","forensic-hashes"] +
                         (if $outcomePresent then ["outcome"] else [] end) +
                         (if $rollbackFailurePresent then ["rollback-failure"] else [] end) +
                         (if $failurePresent then ["failure"] else [] end) +
                         (if $historyPresent then ["persisted-history"] else [] end) +
                         (if $candidatePresent then ["candidate-identity"] else [] end)),
       candidateSha256:(if $candidateSha256 == "null" then null else $candidateSha256 end),
       forensicEvidencePersisted:true,workspacePreserved:true,historyPreserved:true,
       markerRemoved:false,lockRemoved:false,recoveredAt:$recoveredAt}
    ' >"$broray_tx_recovery_history/recovery-result.json.part" || return 2
    mv -f "$broray_tx_recovery_history/recovery-result.json.part" "$broray_tx_recovery_history/recovery-result.json" || return 2
    [ -f "$broray_tx_recovery_history/recovery-result.json" ] && [ ! -L "$broray_tx_recovery_history/recovery-result.json" ] || return 2

    broray_tx_recovery_marker_removed="$broray_tx_recovery_marker_present"
    BRORAY_TX_OPERATION_ID="$broray_tx_recovery_id"
    BRORAY_TX_MODE="$broray_tx_recovery_type"
    BRORAY_TX_WORK="$broray_tx_recovery_path"
    BRORAY_TX_SOURCE_PACKAGE="$broray_tx_recovery_source"
    BRORAY_TX_LOCK_HELD=1
    # Keep the admitted rollback-failed workspace at its canonical operation
    # path.  The verified backup remains named backup.tar.gz until every
    # owner/control record has been retired under the kernel mutex; therefore
    # a crash at any earlier boundary is still recognized by normal orphan
    # recovery.  Renaming the snapshot in place is the final atomic commit and
    # leaves the bounded forensic workspace expected by the next operation.
    broray_tx_recovery_forensic_work="$BRORAY_TX_WORK"
    broray_tx_recovery_forensic_snapshot="$broray_tx_recovery_forensic_work/rollback-failed.forensic.tar.gz"
    broray_tx_recovery_retired_lock="$BRORAY_TX_TMP_BASE/.broray-rollback-failed-lock-$BRORAY_TX_OPERATION_ID"
    broray_tx_recovery_retire_record="$BRORAY_TX_TMP_BASE/.broray-rollback-failed-retire-$BRORAY_TX_OPERATION_ID.tsv"
    broray_tx_recovery_derived_outcome=0
    [ ! -e "$broray_tx_recovery_forensic_snapshot" ] &&
        [ ! -L "$broray_tx_recovery_forensic_snapshot" ] || return 2
    broray_tx_control_transition_begin || return 2
    broray_tx_recovery_forensic_retire_rc=0
    [ ! -e "$broray_tx_recovery_retired_lock" ] &&
        [ ! -L "$broray_tx_recovery_retired_lock" ] ||
        broray_tx_recovery_forensic_retire_rc=1
    broray_tx_control_owner_assert_self "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" &&
        broray_tx_control_owner_assert_self "$BRORAY_TX_WORK/owner-identity.tsv" ||
        broray_tx_recovery_forensic_retire_rc=1
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        broray_tx_rollback_failed_retire_record_publish \
            "$BRORAY_TX_OPERATION_ID" "$broray_tx_recovery_retired_lock" ||
            broray_tx_recovery_forensic_retire_rc=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ] &&
       [ "$broray_tx_recovery_outcome_present" = false ]; then
        [ "$broray_tx_recovery_marker_present" = true ] &&
            [ ! -e "$BRORAY_TX_WORK/outcome" ] && [ ! -L "$BRORAY_TX_WORK/outcome" ] ||
            broray_tx_recovery_forensic_retire_rc=1
        broray_tx_recovery_outcome_part="$BRORAY_TX_TMP_BASE/.broray-rollback-failed-outcome-$BRORAY_TX_OPERATION_ID.part"
        if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ] &&
           { [ -e "$broray_tx_recovery_outcome_part" ] ||
             [ -L "$broray_tx_recovery_outcome_part" ]; }; then
            [ -f "$broray_tx_recovery_outcome_part" ] &&
                [ ! -L "$broray_tx_recovery_outcome_part" ] ||
                broray_tx_recovery_forensic_retire_rc=1
            if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ] &&
               { [ "$(sed -n '1p' "$broray_tx_recovery_outcome_part")" != rollback-failed ] ||
                 [ "$(wc -l <"$broray_tx_recovery_outcome_part" | tr -d ' ')" -ne 1 ]; }; then
                rm -f "$broray_tx_recovery_outcome_part" ||
                    broray_tx_recovery_forensic_retire_rc=1
            fi
        fi
        if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ] &&
           [ ! -e "$broray_tx_recovery_outcome_part" ] &&
           [ ! -L "$broray_tx_recovery_outcome_part" ]; then
            printf '%s\n' rollback-failed >"$broray_tx_recovery_outcome_part" ||
                broray_tx_recovery_forensic_retire_rc=1
            chmod 600 "$broray_tx_recovery_outcome_part" 2>/dev/null || true
        fi
        if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
            [ "$(sed -n '1p' "$broray_tx_recovery_outcome_part")" = rollback-failed ] &&
                [ "$(wc -l <"$broray_tx_recovery_outcome_part" | tr -d ' ')" -eq 1 ] &&
                mv -f "$broray_tx_recovery_outcome_part" "$BRORAY_TX_WORK/outcome" &&
                [ "$(sed -n '1p' "$BRORAY_TX_WORK/outcome")" = rollback-failed ] &&
                [ "$(wc -l <"$BRORAY_TX_WORK/outcome" | tr -d ' ')" -eq 1 ] ||
                broray_tx_recovery_forensic_retire_rc=1
        fi
        [ "$broray_tx_recovery_forensic_retire_rc" -ne 0 ] ||
            broray_tx_recovery_derived_outcome=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        rm -f "$BRORAY_TX_WORK/owner-identity.tsv" ||
            broray_tx_recovery_forensic_retire_rc=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        rm -f "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ||
            broray_tx_recovery_forensic_retire_rc=1
        [ "$broray_tx_recovery_forensic_retire_rc" -ne 0 ] ||
            mv "$BRORAY_TX_LOCK_DIR" "$broray_tx_recovery_retired_lock" ||
            broray_tx_recovery_forensic_retire_rc=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        if [ -e "$BRORAY_TX_CURRENT" ] || [ -L "$BRORAY_TX_CURRENT" ]; then
            [ -f "$BRORAY_TX_CURRENT" ] && [ ! -L "$BRORAY_TX_CURRENT" ] &&
                jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg work "$broray_tx_recovery_path" \
                    '.operationId==$id and .workspace==$work' "$BRORAY_TX_CURRENT" \
                    >/dev/null 2>&1 && rm -f "$BRORAY_TX_CURRENT" ||
                    broray_tx_recovery_forensic_retire_rc=1
        fi
        rm -f "$BRORAY_TX_LEGACY_MARKER" "$BRORAY_TX_LEGACY_MARKER.part" ||
            broray_tx_recovery_forensic_retire_rc=1
    fi
    # Publish the exact terminal result before the snapshot rename.  Until the
    # rename, backup.tar.gz keeps every crash retryable and a later attempt may
    # overwrite this write-ahead record.  After the rename, these authenticated
    # bytes make the forensic workspace safely distinguishable from an
    # ownerless live/control prelude.
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        jq --argjson markerRemoved "$broray_tx_recovery_marker_removed" \
           --arg forensicSnapshot "$broray_tx_recovery_forensic_snapshot" \
           --arg forensicSnapshotSha256 "$broray_tx_recovery_archive_sha" '
          .markerRemoved = $markerRemoved | .lockRemoved = true |
          .currentSnapshotRetired = true | .historicalInput = false |
          .forensicSnapshot = $forensicSnapshot |
          .forensicSnapshotSha256 = $forensicSnapshotSha256
        ' "$broray_tx_recovery_history/recovery-result.json" \
            >"$broray_tx_recovery_history/recovery-result.json.part" ||
            broray_tx_recovery_forensic_retire_rc=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        mv -f "$broray_tx_recovery_history/recovery-result.json.part" \
            "$broray_tx_recovery_history/recovery-result.json" ||
            broray_tx_recovery_forensic_retire_rc=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        jq -e --arg id "$broray_tx_recovery_id" \
            --arg work "$broray_tx_recovery_path" \
            --arg snapshot "$broray_tx_recovery_forensic_snapshot" \
            --arg sha "$broray_tx_recovery_archive_sha" \
            --argjson markerPresent "$broray_tx_recovery_marker_present" '
          (.schemaVersion==2) and (.kind=="stale-rollback-failed-recovery") and
          (.operationId==$id) and (.transactionPath==$work) and
          (.markerPresent==$markerPresent) and (.lockRemoved==true) and
          (.forensicEvidencePersisted==true) and (.workspacePreserved==true) and
          (.historyPreserved==true) and (.currentSnapshotRetired==true) and
          (.historicalInput==false) and (.forensicSnapshot==$snapshot) and
          (.forensicSnapshotSha256==$sha)
        ' "$broray_tx_recovery_history/recovery-result.json" >/dev/null 2>&1 ||
            broray_tx_recovery_forensic_retire_rc=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        broray_tx_test_pause rollback-failed-result-write-ahead \
            "$BRORAY_TX_WORK/evidence" || broray_tx_recovery_forensic_retire_rc=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        mv -f "$broray_tx_recovery_forensic_work/backup.tar.gz" \
            "$broray_tx_recovery_forensic_snapshot" ||
            broray_tx_recovery_forensic_retire_rc=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        broray_tx_test_pause rollback-failed-forensic-snapshot-committed \
            "$BRORAY_TX_WORK/evidence" || broray_tx_recovery_forensic_retire_rc=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ] &&
       { [ -e "$broray_tx_recovery_forensic_work/backup.tar.gz.sha256" ] ||
         [ -L "$broray_tx_recovery_forensic_work/backup.tar.gz.sha256" ]; }; then
        [ -f "$broray_tx_recovery_forensic_work/backup.tar.gz.sha256" ] &&
            [ ! -L "$broray_tx_recovery_forensic_work/backup.tar.gz.sha256" ] ||
            broray_tx_recovery_forensic_retire_rc=1
        [ "$broray_tx_recovery_forensic_retire_rc" -ne 0 ] ||
            mv -f "$broray_tx_recovery_forensic_work/backup.tar.gz.sha256" \
                "$broray_tx_recovery_forensic_snapshot.sha256" 2>/dev/null || true
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ]; then
        broray_tx_rollback_failed_retired_lock_remove \
            "$broray_tx_recovery_retire_record" primary ||
            broray_tx_recovery_forensic_retire_rc=1
    fi
    if [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ] &&
       [ "$broray_tx_recovery_derived_outcome" -eq 1 ]; then
        [ -f "$BRORAY_TX_WORK/outcome" ] && [ ! -L "$BRORAY_TX_WORK/outcome" ] &&
            [ "$(sed -n '1p' "$BRORAY_TX_WORK/outcome")" = rollback-failed ] &&
            [ "$(wc -l <"$BRORAY_TX_WORK/outcome" | tr -d ' ')" -eq 1 ] &&
            rm -f "$BRORAY_TX_WORK/outcome" ||
            broray_tx_recovery_forensic_retire_rc=1
    fi
    broray_tx_control_transition_end || return 2
    [ "$broray_tx_recovery_forensic_retire_rc" -eq 0 ] || return 2
    BRORAY_TX_LOCK_HELD=0
    [ -f "$broray_tx_recovery_forensic_snapshot" ] &&
        [ ! -L "$broray_tx_recovery_forensic_snapshot" ] &&
        [ "$(broray_tx_sha "$broray_tx_recovery_forensic_snapshot")" = "$broray_tx_recovery_archive_sha" ] || return 2
    BRORAY_TX_OPERATION_ID="$broray_tx_recovery_saved_operation_id"
    BRORAY_TX_MODE="$broray_tx_recovery_saved_mode"
    BRORAY_TX_WORK="$broray_tx_recovery_saved_work"
    BRORAY_TX_SOURCE_PACKAGE="$broray_tx_recovery_saved_source"
    jq -e --arg id "$broray_tx_recovery_id" \
        --arg work "$broray_tx_recovery_path" \
        --arg snapshot "$broray_tx_recovery_forensic_snapshot" \
        --arg sha "$broray_tx_recovery_archive_sha" \
        --argjson markerPresent "$broray_tx_recovery_marker_present" '
      (.schemaVersion==2) and (.kind=="stale-rollback-failed-recovery") and
      (.operationId==$id) and (.transactionPath==$work) and
      (.markerPresent==$markerPresent) and (.lockRemoved==true) and
      (.forensicEvidencePersisted==true) and (.workspacePreserved==true) and
      (.historyPreserved==true) and (.currentSnapshotRetired==true) and
      (.historicalInput==false) and (.forensicSnapshot==$snapshot) and
      (.forensicSnapshotSha256==$sha)
    ' "$broray_tx_recovery_history/recovery-result.json" >/dev/null 2>&1 || return 2
    if [ -n "$BRORAY_TX_RECOVERY_PREDECESSOR_PROOF" ]; then
        [ "$BRORAY_TX_RECOVERY_PREDECESSOR_PROOF" = \
            "$BRORAY_TX_TMP_BASE/.broray-recovery-predecessor-$broray_tx_recovery_id.tsv" ] &&
            broray_tx_files_equal "$BRORAY_TX_RECOVERY_PREDECESSOR_PROOF" \
                "$broray_tx_recovery_original_owner_history" || return 2
        rm -f "$BRORAY_TX_RECOVERY_PREDECESSOR_PROOF" || return 2
        [ ! -e "$BRORAY_TX_RECOVERY_PREDECESSOR_PROOF" ] &&
            [ ! -L "$BRORAY_TX_RECOVERY_PREDECESSOR_PROOF" ] || return 2
        BRORAY_TX_RECOVERY_PREDECESSOR_PROOF=""
    fi
    return 0
}

broray_tx_recovery_stale_native_clear()
{
    broray_tx_recovery_native="$BRORAY_TX_WORK/native-opkg-lock"
    [ -e "$broray_tx_recovery_native" ] || [ -L "$broray_tx_recovery_native" ] || return 0
    [ -d "$broray_tx_recovery_native" ] && [ ! -L "$broray_tx_recovery_native" ] || return 1
    for broray_tx_recovery_native_entry in "$broray_tx_recovery_native"/* "$broray_tx_recovery_native"/.[!.]* "$broray_tx_recovery_native"/..?*; do
        [ -e "$broray_tx_recovery_native_entry" ] || [ -L "$broray_tx_recovery_native_entry" ] || continue
        case "${broray_tx_recovery_native_entry##*/}" in
            hold.ipk|owner.stdout|owner.stderr|owner-pid|owner-starttime|owner-cmdline.expected|owner-cmdline-sha256|\
            owner-exe-canonical|owner-profile|owner-opkg-argv0|opkg-canonical|config-arg|config-path|config-sha256|\
            configured-lock-path|lock-fd|lock-mnt-id|lock-inode|lock-devino|lock-type|lock-path|lock-target|\
            acquire-stage|failure-reason|owner-exit-code|proc-locks-before|proc-locks-after|state) ;;
            *) return 1 ;;
        esac
    done
    for broray_tx_recovery_native_identity in owner-pid owner-starttime owner-cmdline-sha256 owner-exe-canonical; do
        [ -f "$broray_tx_recovery_native/$broray_tx_recovery_native_identity" ] &&
            [ ! -L "$broray_tx_recovery_native/$broray_tx_recovery_native_identity" ] &&
            [ "$(wc -l <"$broray_tx_recovery_native/$broray_tx_recovery_native_identity" 2>/dev/null | tr -d ' ')" -eq 1 ] || return 1
    done
    broray_tx_recovery_native_pid="$(sed -n '1p' "$broray_tx_recovery_native/owner-pid")"
    broray_tx_recovery_native_start="$(sed -n '1p' "$broray_tx_recovery_native/owner-starttime")"
    broray_tx_recovery_native_cmdline_sha="$(sed -n '1p' "$broray_tx_recovery_native/owner-cmdline-sha256")"
    broray_tx_recovery_native_exe="$(sed -n '1p' "$broray_tx_recovery_native/owner-exe-canonical")"
    case "$broray_tx_recovery_native_pid:$broray_tx_recovery_native_start:$broray_tx_recovery_native_cmdline_sha" in
        *[!0-9a-f:]*) return 1 ;;
    esac
    [ "$broray_tx_recovery_native_pid" -gt 0 ] && [ "$broray_tx_recovery_native_start" -gt 0 ] &&
        [ "${#broray_tx_recovery_native_cmdline_sha}" -eq 64 ] || return 1
    case "$broray_tx_recovery_native_exe" in /*) ;; *) return 1 ;; esac
    broray_tx_recovery_native_live_start="$(broray_tx_proc_starttime "$BRORAY_TX_PROC_ROOT" "$broray_tx_recovery_native_pid" 2>/dev/null)" ||
        broray_tx_recovery_native_live_start=""
    if [ -z "$broray_tx_recovery_native_live_start" ]; then
        [ ! -e "$BRORAY_TX_PROC_ROOT/$broray_tx_recovery_native_pid" ] &&
        [ ! -L "$BRORAY_TX_PROC_ROOT/$broray_tx_recovery_native_pid" ] &&
        ! kill -0 "$broray_tx_recovery_native_pid" 2>/dev/null || return 1
    elif [ "$broray_tx_recovery_native_live_start" = "$broray_tx_recovery_native_start" ]; then
        # Same birth token means either the exact still-live OPKG holder or an
        # unreadable/mutated identity; both are ambiguous and must be kept.
        broray_tx_recovery_native_live_exe="$(readlink -f "$BRORAY_TX_PROC_ROOT/$broray_tx_recovery_native_pid/exe" 2>/dev/null)" || return 1
        broray_tx_recovery_native_live_cmdline_sha="$(broray_tx_sha "$BRORAY_TX_PROC_ROOT/$broray_tx_recovery_native_pid/cmdline")" || return 1
        [ "$broray_tx_recovery_native_live_exe" != "$broray_tx_recovery_native_exe" ] ||
        [ "$broray_tx_recovery_native_live_cmdline_sha" != "$broray_tx_recovery_native_cmdline_sha" ] || return 1
        return 1
    fi
    # A different immutable starttime is a reused PID and therefore cannot be
    # the recorded native holder.  No signal is sent to that unrelated process.
    broray_tx_guard_work "$broray_tx_recovery_native" || return 1
    rm -rf "$broray_tx_recovery_native"
}

broray_tx_recovery_record()
{
    broray_tx_recovery_kind="$1"
    broray_tx_recovery_action="$2"
    broray_tx_recovery_snapshot_source="${BRORAY_TX_RECOVERY_SNAPSHOT_SOURCE:-current-operation-only}"
    [ -e "$BRORAY_TX_OPERATION_ROOT" ] || mkdir -p "$BRORAY_TX_OPERATION_ROOT" || return 1
    [ -d "$BRORAY_TX_OPERATION_ROOT" ] && [ ! -L "$BRORAY_TX_OPERATION_ROOT" ] || return 1
    broray_tx_recovery_history="$BRORAY_TX_OPERATION_ROOT/$BRORAY_TX_OPERATION_ID"
    if [ ! -e "$broray_tx_recovery_history" ] && [ ! -L "$broray_tx_recovery_history" ]; then
        mkdir "$broray_tx_recovery_history" || return 1
        chmod 700 "$broray_tx_recovery_history" 2>/dev/null || true
    fi
    [ -d "$broray_tx_recovery_history" ] && [ ! -L "$broray_tx_recovery_history" ] || return 1
    broray_tx_recovery_cleanup_json="$broray_tx_recovery_history/.cleanup-interruption.$$"
    if [ -f "$BRORAY_TX_WORK/evidence/cleanup-plan.json" ] &&
       [ ! -L "$BRORAY_TX_WORK/evidence/cleanup-plan.json" ]; then
        broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/cleanup-plan.json" "$BRORAY_TX_CLEANUP_PLAN_CAP_KB" || return 1
        broray_tx_recovery_cleanup_actions="$broray_tx_recovery_history/.cleanup-actions.$$"
        if [ -f "$BRORAY_TX_WORK/evidence/cleanup-actions.txt" ] &&
           [ ! -L "$BRORAY_TX_WORK/evidence/cleanup-actions.txt" ]; then
            broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/cleanup-actions.txt" "$BRORAY_TX_CLEANUP_PLAN_CAP_KB" || return 1
            jq -Rn '[inputs | split("\t") |
              {path:.[0],type:.[1],sizeKB:(.[2]|tonumber),cleanupClass:.[3],reason:.[4],ownershipProof:.[5]}]' \
                <"$BRORAY_TX_WORK/evidence/cleanup-actions.txt" >"$broray_tx_recovery_cleanup_actions" || return 1
        else
            printf '%s\n' '[]' >"$broray_tx_recovery_cleanup_actions" || return 1
        fi
        jq -n --slurpfile plan "$BRORAY_TX_WORK/evidence/cleanup-plan.json" \
            --slurpfile actions "$broray_tx_recovery_cleanup_actions" \
            '{present:true,plan:$plan[0],actions:$actions[0],
              planComplete:(($plan[0]|length)==($actions[0]|length)),
              planEqualsActions:($plan[0]==$actions[0])}' >"$broray_tx_recovery_cleanup_json" || return 1
        rm -f "$broray_tx_recovery_cleanup_actions"
        broray_tx_file_cap_kb "$broray_tx_recovery_cleanup_json" "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" || return 1
    else
        printf '%s\n' '{"present":false}' >"$broray_tx_recovery_cleanup_json" || return 1
    fi
    jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" --arg kind "$broray_tx_recovery_kind" \
        --arg action "$broray_tx_recovery_action" --arg snapshotSource "$broray_tx_recovery_snapshot_source" \
        --slurpfile cleanupInterruption "$broray_tx_recovery_cleanup_json" \
        --arg recoveredAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" '
      {schemaVersion:1,status:"PASS",operationId:$operationId,kind:$kind,action:$action,
       snapshotSource:$snapshotSource,historicalInput:false,
       cleanupInterruption:$cleanupInterruption[0],recoveredAt:$recoveredAt}
    ' >"$broray_tx_recovery_history/recovery-result.json.part" || return 1
    rm -f "$broray_tx_recovery_cleanup_json"
    mv -f "$broray_tx_recovery_history/recovery-result.json.part" "$broray_tx_recovery_history/recovery-result.json" || return 1
    if [ -f "$BRORAY_TX_WORK/failure.json" ] && [ ! -L "$BRORAY_TX_WORK/failure.json" ] &&
       broray_tx_file_cap_kb "$BRORAY_TX_WORK/failure.json" "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB"; then
        cp -p "$BRORAY_TX_WORK/failure.json" "$broray_tx_recovery_history/interrupted-failure.json.part" || return 1
        mv -f "$broray_tx_recovery_history/interrupted-failure.json.part" "$broray_tx_recovery_history/interrupted-failure.json" || return 1
    fi
}

broray_tx_recovery_terminal_cleanup()
{
    if [ -e "$BRORAY_TX_CURRENT" ] || [ -L "$BRORAY_TX_CURRENT" ]; then
        [ -f "$BRORAY_TX_CURRENT" ] && [ ! -L "$BRORAY_TX_CURRENT" ] || return 1
        jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg work "$BRORAY_TX_WORK" \
            '.operationId==$id and .workspace==$work' "$BRORAY_TX_CURRENT" >/dev/null 2>&1 || return 1
    fi
    if [ -e "$BRORAY_TX_LEGACY_MARKER" ] || [ -L "$BRORAY_TX_LEGACY_MARKER" ]; then
        [ -f "$BRORAY_TX_LEGACY_MARKER" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER" ] || return 1
        jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg work "$BRORAY_TX_WORK" \
            '.operationId==$id and .transactionPath==$work' "$BRORAY_TX_LEGACY_MARKER" >/dev/null 2>&1 || return 1
    fi
    broray_tx_terminal_control_retire
}

# Admission for a destructive recovery is read-only with respect to the
# retained operation.  Only the bounded capsule and verification scratch are
# materialized in a separate directory; ambiguous control/snapshot bytes are
# therefore unchanged when this function rejects them.
broray_tx_recovery_capsule_dir_verify()
{
    broray_tx_recovery_capsule_dir="$1"
    broray_tx_recovery_capsule_operation_id="${3:-$BRORAY_TX_OPERATION_ID}"
    broray_tx_valid_id "$broray_tx_recovery_capsule_operation_id" || return 1
    [ -d "$broray_tx_recovery_capsule_dir" ] && [ ! -L "$broray_tx_recovery_capsule_dir" ] || return 1
    broray_tx_recovery_capsule_expected="$2/capsule-files.expected"
    broray_tx_recovery_capsule_actual="$2/capsule-files.actual"
    printf '%s\n' \
        broray-status.stanza candidate-target.json capabilities.json capabilities.tsv capsule.sha256 contracts.env \
        foreign-status.before foreign-status.sha256 keenetic-running-before.txt operation-relation-pre-snapshot.json operation.json \
        opkg-status.before opkg-status.presence opkg-status.sha256 \
        protected-source.manifest protected-source-roots.list services-before.identity.tsv \
        recovery-space.json services-before.tsv services-running-before.list source.allocation.manifest source.manifest \
        source-scope.list | LC_ALL=C sort | sed 's/^/f|/' >"$broray_tx_recovery_capsule_expected" || return 1
    find -P "$broray_tx_recovery_capsule_dir" -mindepth 1 -maxdepth 1 -printf '%y|%f\n' |
        LC_ALL=C sort >"$broray_tx_recovery_capsule_actual" || return 1
    broray_tx_files_equal "$broray_tx_recovery_capsule_expected" "$broray_tx_recovery_capsule_actual" || return 1
    grep -Fqx "requirementsContract=$BRORAY_TX_REQUIREMENTS_CONTRACT" "$broray_tx_recovery_capsule_dir/contracts.env" &&
    grep -Fqx "lifecycleContract=$BRORAY_TX_CONTRACT" "$broray_tx_recovery_capsule_dir/contracts.env" &&
    grep -Fqx 'capabilityContract=keenetic-entware-capabilities/1' "$broray_tx_recovery_capsule_dir/contracts.env" &&
    grep -Fqx 'spaceContract=broray-space/2' "$broray_tx_recovery_capsule_dir/contracts.env" &&
    grep -Fqx 'previousIpkRequired=false' "$broray_tx_recovery_capsule_dir/contracts.env" &&
    grep -Fqx 'historicalTransactionStateRequired=false' "$broray_tx_recovery_capsule_dir/contracts.env" &&
    grep -Fqx 'statelessBootstrap=true' "$broray_tx_recovery_capsule_dir/contracts.env" &&
    grep -Fqx 'sourceAdmission=bro-any-structural' "$broray_tx_recovery_capsule_dir/contracts.env" &&
    [ "$(wc -l <"$broray_tx_recovery_capsule_dir/contracts.env" | tr -d ' ')" -eq 8 ] || return 1
    jq -e --arg id "$broray_tx_recovery_capsule_operation_id" '
      (type=="object") and (.schemaVersion==1) and
      ((keys|sort)==(["schemaVersion","requirementsContract","lifecycleContract","capabilityContract",
                       "spaceContract","previousIpkRequired","historicalTransactionStateRequired",
                       "statelessBootstrap","operationId","opkgMetadataUpperKB",
                       "opkgStatusTransientUpperKB","recoveryUseOnly"]|sort)) and
      (.requirementsContract=="1.7.2") and
      (.lifecycleContract=="current-operation-full-tmp-snapshot/1") and
      (.capabilityContract=="keenetic-entware-capabilities/1") and
      (.spaceContract=="broray-space/2") and
      (.previousIpkRequired==false) and (.historicalTransactionStateRequired==false) and
      (.statelessBootstrap==true) and (.operationId==$id) and
      ((.opkgMetadataUpperKB|type)=="number") and (.opkgMetadataUpperKB|floor)==.opkgMetadataUpperKB and
      (.opkgMetadataUpperKB>0) and
      ((.opkgStatusTransientUpperKB|type)=="number") and
      (.opkgStatusTransientUpperKB|floor)==.opkgStatusTransientUpperKB and (.opkgStatusTransientUpperKB>=0) and
      (.recoveryUseOnly==true)
    ' "$broray_tx_recovery_capsule_dir/recovery-space.json" >/dev/null 2>&1 || return 1
    broray_tx_recovery_status_presence="$(sed -n '1p' "$broray_tx_recovery_capsule_dir/opkg-status.presence" 2>/dev/null)"
    case "$broray_tx_recovery_status_presence" in present|absent) ;; *) return 1 ;; esac
    [ "$(wc -l <"$broray_tx_recovery_capsule_dir/opkg-status.presence" | tr -d ' ')" -eq 1 ] || return 1
    broray_tx_recovery_status_sha="$(sed -n '1p' "$broray_tx_recovery_capsule_dir/opkg-status.sha256" 2>/dev/null)"
    case "$broray_tx_recovery_status_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#broray_tx_recovery_status_sha}" -eq 64 ] &&
        [ "$(broray_tx_sha "$broray_tx_recovery_capsule_dir/opkg-status.before")" = \
          "$broray_tx_recovery_status_sha" ] || return 1
    [ "$broray_tx_recovery_status_presence" = present ] ||
        [ ! -s "$broray_tx_recovery_capsule_dir/opkg-status.before" ] || return 1
    broray_tx_status_foreign_projection "$broray_tx_recovery_capsule_dir/opkg-status.before" \
        "$2/capsule-foreign-status.computed" || return 1
    broray_tx_files_equal "$broray_tx_recovery_capsule_dir/foreign-status.before" \
        "$2/capsule-foreign-status.computed" || return 1
    [ "$(broray_tx_sha "$broray_tx_recovery_capsule_dir/foreign-status.before")" = \
      "$(sed -n '1p' "$broray_tx_recovery_capsule_dir/foreign-status.sha256")" ] || return 1
    awk 'BEGIN{RS="";ORS="\n\n"}
      $0 ~ /(^|\n)Package:[[:space:]]*broray(\n|$)/ {print; found++}
      END{if(found>1)exit 2}' "$broray_tx_recovery_capsule_dir/opkg-status.before" \
        >"$2/capsule-broray-status.computed" || return 1
    broray_tx_files_equal "$broray_tx_recovery_capsule_dir/broray-status.stanza" \
        "$2/capsule-broray-status.computed" || return 1
    (
        cd "$broray_tx_recovery_capsule_dir" || exit 1
        find -P . -mindepth 1 -maxdepth 1 -type f ! -name capsule.sha256 -print |
            LC_ALL=C sort | while IFS= read -r broray_tx_recovery_hash_member; do
                sha256sum "$broray_tx_recovery_hash_member" || exit 1
            done >"$2/capsule.sha256.computed"
    ) || return 1
    broray_tx_files_equal "$broray_tx_recovery_capsule_dir/capsule.sha256" "$2/capsule.sha256.computed" || return 1
    mkdir -p "$2/evidence" || return 1
    broray_tx_operation_relation_capsule_verify "$broray_tx_recovery_capsule_dir" \
        "$2/evidence/operation-relation-recovery.json" || return 1
    cp -p "$broray_tx_recovery_capsule_dir/capabilities.json" "$2/evidence/capabilities.json" || return 1
    cp -p "$broray_tx_recovery_capsule_dir/capabilities.tsv" "$2/evidence/capabilities.tsv" || return 1
    broray_runtime_reactivate_current_operation "$2"
}

broray_tx_recovery_factual_source_classify()
{
    broray_tx_recovery_factual_capsule="$1"
    broray_tx_recovery_factual_scratch="$2"
    BRORAY_TX_RECOVERY_FACTUAL_STATE=ambiguous
    broray_tx_manifest_build "$BRORAY_TX_FS_ROOT" \
        "$broray_tx_recovery_factual_capsule/source-scope.list" \
        "$broray_tx_recovery_factual_scratch/live-source.manifest" || {
            # A capsule-declared source object that is now absent is a
            # conclusive factual mismatch, not an ambiguous read failure.
            # Clean replacement deliberately unregisters source OPKG info at
            # the mutation barrier, so restart must authorize the one current
            # snapshot rollback even before target extraction begins.
            [ "${BRORAY_TX_MANIFEST_FAILURE_REASON:-}" = scope-object-missing ] &&
                BRORAY_TX_RECOVERY_FACTUAL_STATE=source-changed
            return 0
        }
    if ! broray_tx_files_equal "$broray_tx_recovery_factual_capsule/source.manifest" \
        "$broray_tx_recovery_factual_scratch/live-source.manifest"; then
        BRORAY_TX_RECOVERY_FACTUAL_STATE=source-changed
        return 0
    fi
    broray_tx_recovery_factual_status_presence="$(sed -n '1p' \
        "$broray_tx_recovery_factual_capsule/opkg-status.presence" 2>/dev/null)"
    case "$broray_tx_recovery_factual_status_presence" in
        present)
            [ -f "$BRORAY_TX_STATUS_FILE" ] && [ ! -L "$BRORAY_TX_STATUS_FILE" ] || {
                BRORAY_TX_RECOVERY_FACTUAL_STATE=source-changed
                return 0
            }
            if ! broray_tx_files_equal "$broray_tx_recovery_factual_capsule/opkg-status.before" \
                "$BRORAY_TX_STATUS_FILE"; then
                BRORAY_TX_RECOVERY_FACTUAL_STATE=source-changed
                return 0
            fi
            ;;
        absent)
            if [ -e "$BRORAY_TX_STATUS_FILE" ] || [ -L "$BRORAY_TX_STATUS_FILE" ]; then
                BRORAY_TX_RECOVERY_FACTUAL_STATE=source-changed
                return 0
            fi
            ;;
        *) return 0 ;;
    esac
    [ "$(broray_tx_sha "$broray_tx_recovery_factual_capsule/opkg-status.before")" = \
      "$(sed -n '1p' "$broray_tx_recovery_factual_capsule/opkg-status.sha256")" ] || return 0
    broray_tx_status_stanza_broray >"$broray_tx_recovery_factual_scratch/live-broray-status.stanza" || return 0
    broray_tx_status_without_broray_to "$broray_tx_recovery_factual_scratch/live-foreign-status" || return 0
    broray_tx_files_equal "$broray_tx_recovery_factual_capsule/broray-status.stanza" \
        "$broray_tx_recovery_factual_scratch/live-broray-status.stanza" || {
            BRORAY_TX_RECOVERY_FACTUAL_STATE=source-changed
            return 0
        }
    broray_tx_files_equal "$broray_tx_recovery_factual_capsule/foreign-status.before" \
        "$broray_tx_recovery_factual_scratch/live-foreign-status" || return 0
    [ "$(broray_tx_sha "$broray_tx_recovery_factual_capsule/foreign-status.before")" = \
      "$(sed -n '1p' "$broray_tx_recovery_factual_capsule/foreign-status.sha256")" ] || return 0
    BRORAY_TX_RECOVERY_FACTUAL_STATE=source-exact
    return 0
}

broray_tx_recovery_snapshot_admit_readonly()
{
    broray_tx_recovery_original_work="$BRORAY_TX_WORK"
    broray_tx_recovery_archive="$broray_tx_recovery_original_work/backup.tar.gz"
    [ -f "$broray_tx_recovery_archive" ] && [ ! -L "$broray_tx_recovery_archive" ] &&
        [ -s "$broray_tx_recovery_archive" ] || return 1
    broray_tx_recovery_admit="$(mktemp -d "$BRORAY_TX_TMP_BASE/broray-recovery-admit.XXXXXX")" || return 1
    case "$broray_tx_recovery_admit" in "$BRORAY_TX_TMP_BASE"/broray-recovery-admit.*) ;; *) return 1 ;; esac
    broray_tx_recovery_admit_rc=0
    mkdir -p "$broray_tx_recovery_admit/evidence" || broray_tx_recovery_admit_rc=1
    if [ "$broray_tx_recovery_admit_rc" -eq 0 ]; then
        gzip -t "$broray_tx_recovery_archive" >/dev/null 2>&1 || broray_tx_recovery_admit_rc=1
    fi
    if [ "$broray_tx_recovery_admit_rc" -eq 0 ]; then
        broray_tx_tar_safe "$broray_tx_recovery_archive" "$broray_tx_recovery_admit/archive.members" || broray_tx_recovery_admit_rc=1
    fi
    if [ "$broray_tx_recovery_admit_rc" -eq 0 ]; then
        tar -xzf "$broray_tx_recovery_archive" -C "$broray_tx_recovery_admit" .broray-recovery \
            >/dev/null 2>"$broray_tx_recovery_admit/evidence/capsule.stderr" || broray_tx_recovery_admit_rc=1
    fi
    if [ "$broray_tx_recovery_admit_rc" -eq 0 ]; then
        broray_tx_recovery_capsule_dir_verify "$broray_tx_recovery_admit/.broray-recovery" \
            "$broray_tx_recovery_admit" || broray_tx_recovery_admit_rc=1
    fi
    if [ "$broray_tx_recovery_admit_rc" -eq 0 ]; then
        jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg mode "$BRORAY_TX_MODE" \
            --arg source "$BRORAY_TX_SOURCE_PACKAGE" --arg sourceApp "$BRORAY_TX_SOURCE_APP" \
            --arg target "$BRORAY_TX_TARGET_PACKAGE" --arg path "$broray_tx_recovery_original_work" \
            --arg lifecycle "$BRORAY_TX_CONTRACT" '
          .schemaVersion==2 and .lifecycleContract==$lifecycle and .operationId==$id and
          .mode==$mode and .sourceVersion==$source and .sourceAppVersion==$sourceApp and
          .targetVersion==$target and .transactionPath==$path
        ' "$broray_tx_recovery_admit/.broray-recovery/operation.json" >/dev/null 2>&1 ||
            broray_tx_recovery_admit_rc=1
    fi
    if [ "$broray_tx_recovery_admit_rc" -eq 0 ]; then
        cp -p "$broray_tx_recovery_admit/.broray-recovery/source.manifest" "$broray_tx_recovery_admit/source.manifest" &&
        cp -p "$broray_tx_recovery_admit/.broray-recovery/source.allocation.manifest" "$broray_tx_recovery_admit/source.allocation.manifest" ||
            broray_tx_recovery_admit_rc=1
    fi
    if [ "$broray_tx_recovery_admit_rc" -eq 0 ]; then
        awk -F '|' '{print $2}' "$broray_tx_recovery_admit/source.manifest" | LC_ALL=C sort -u \
            >"$broray_tx_recovery_admit/source.members" || broray_tx_recovery_admit_rc=1
        find -P "$broray_tx_recovery_admit/.broray-recovery" -printf '%P\n' |
            sed '/^$/d;s#^#.broray-recovery/#' | sed 's#/$##' >"$broray_tx_recovery_admit/capsule.members" ||
            broray_tx_recovery_admit_rc=1
        { cat "$broray_tx_recovery_admit/source.members"; printf '%s\n' .broray-recovery;
          cat "$broray_tx_recovery_admit/capsule.members"; } | LC_ALL=C sort -u \
            >"$broray_tx_recovery_admit/expected.members" || broray_tx_recovery_admit_rc=1
        broray_tx_files_equal "$broray_tx_recovery_admit/expected.members" "$broray_tx_recovery_admit/archive.members" ||
            broray_tx_recovery_admit_rc=1
    fi
    if [ "$broray_tx_recovery_admit_rc" -eq 0 ]; then
        BRORAY_TX_WORK="$broray_tx_recovery_admit"
        BRORAY_TX_SNAPSHOT_ARCHIVE="$broray_tx_recovery_archive"
        export BRORAY_TX_SNAPSHOT_ARCHIVE
        broray_tx_snapshot_self_contained_verify || broray_tx_recovery_admit_rc=1
        unset BRORAY_TX_SNAPSHOT_ARCHIVE
        BRORAY_TX_WORK="$broray_tx_recovery_original_work"
    fi
    if [ "$broray_tx_recovery_admit_rc" -eq 0 ]; then
        BRORAY_TX_WORK="$broray_tx_recovery_admit"
        broray_tx_recovery_factual_source_classify \
            "$broray_tx_recovery_admit/.broray-recovery" "$broray_tx_recovery_admit" ||
            broray_tx_recovery_admit_rc=1
        BRORAY_TX_WORK="$broray_tx_recovery_original_work"
    fi
    BRORAY_TX_WORK="$broray_tx_recovery_original_work"
    if [ "$broray_tx_recovery_admit_rc" -eq 0 ]; then
        BRORAY_TX_RECOVERY_ARCHIVE_SHA="$(broray_tx_sha "$broray_tx_recovery_archive")"
        case "$BRORAY_TX_RECOVERY_ARCHIVE_SHA" in ''|*[!0-9a-f]*) broray_tx_recovery_admit_rc=1 ;; esac
        [ "${#BRORAY_TX_RECOVERY_ARCHIVE_SHA}" -eq 64 ] || broray_tx_recovery_admit_rc=1
    fi
    rm -rf "$broray_tx_recovery_admit" || return 1
    [ "$broray_tx_recovery_admit_rc" -eq 0 ]
}

# Derive an orphan operation identity from the authenticated capsule before
# consulting any volatile workspace sidecar.  The temporary extraction is
# discarded on every path and no retained control byte is changed here.
broray_tx_recovery_orphan_identity_admit()
{
    broray_tx_orphan_path="$1"
    broray_tx_orphan_archive="$broray_tx_orphan_path/backup.tar.gz"
    [ -f "$broray_tx_orphan_archive" ] && [ ! -L "$broray_tx_orphan_archive" ] &&
        [ -s "$broray_tx_orphan_archive" ] || return 1
    broray_runtime_resolve_release_tools || return 1
    broray_runtime_activate_if_resolved || return 1
    broray_tx_orphan_admit="$(mktemp -d "$BRORAY_TX_TMP_BASE/broray-orphan-admit.XXXXXX")" || return 1
    case "$broray_tx_orphan_admit" in "$BRORAY_TX_TMP_BASE"/broray-orphan-admit.*) ;; *) return 1 ;; esac
    broray_tx_orphan_admit_rc=0
    mkdir -p "$broray_tx_orphan_admit/evidence" || broray_tx_orphan_admit_rc=1
    if [ "$broray_tx_orphan_admit_rc" -eq 0 ]; then
        gzip -t "$broray_tx_orphan_archive" >/dev/null 2>&1 || broray_tx_orphan_admit_rc=1
    fi
    if [ "$broray_tx_orphan_admit_rc" -eq 0 ]; then
        broray_tx_tar_safe "$broray_tx_orphan_archive" "$broray_tx_orphan_admit/archive.members" ||
            broray_tx_orphan_admit_rc=1
    fi
    if [ "$broray_tx_orphan_admit_rc" -eq 0 ]; then
        tar -xzf "$broray_tx_orphan_archive" -C "$broray_tx_orphan_admit" .broray-recovery \
            >/dev/null 2>"$broray_tx_orphan_admit/evidence/capsule.stderr" || broray_tx_orphan_admit_rc=1
    fi
    if [ "$broray_tx_orphan_admit_rc" -eq 0 ]; then
        # Read the claimed id only to parameterize authenticated capsule
        # verification.  It is not trusted for path/control selection until
        # capsule.sha256 and the complete operation schema pass below.
        broray_tx_orphan_capsule_id="$(jq -er '.operationId' \
            "$broray_tx_orphan_admit/.broray-recovery/operation.json" 2>/dev/null)" ||
            broray_tx_orphan_admit_rc=1
        if [ "$broray_tx_orphan_admit_rc" -eq 0 ]; then
            broray_tx_valid_id "$broray_tx_orphan_capsule_id" || broray_tx_orphan_admit_rc=1
        fi
        if [ "$broray_tx_orphan_admit_rc" -eq 0 ]; then
            broray_tx_recovery_capsule_dir_verify "$broray_tx_orphan_admit/.broray-recovery" \
                "$broray_tx_orphan_admit" "$broray_tx_orphan_capsule_id" || broray_tx_orphan_admit_rc=1
        fi
    fi
    broray_tx_orphan_operation="$broray_tx_orphan_admit/.broray-recovery/operation.json"
    if [ "$broray_tx_orphan_admit_rc" -eq 0 ]; then
        broray_tx_orphan_id="$(jq -er '.operationId' "$broray_tx_orphan_operation" 2>/dev/null)" || broray_tx_orphan_admit_rc=1
        broray_tx_orphan_mode="$(jq -er '.mode' "$broray_tx_orphan_operation" 2>/dev/null)" || broray_tx_orphan_admit_rc=1
        broray_tx_orphan_source="$(jq -er '.sourceVersion' "$broray_tx_orphan_operation" 2>/dev/null)" || broray_tx_orphan_admit_rc=1
        broray_tx_orphan_source_app="$(jq -er '.sourceAppVersion' "$broray_tx_orphan_operation" 2>/dev/null)" || broray_tx_orphan_admit_rc=1
        broray_tx_orphan_source_class="$(jq -er '.sourceClass' "$broray_tx_orphan_operation" 2>/dev/null)" || broray_tx_orphan_admit_rc=1
        broray_tx_orphan_migration="$(jq -er '.migrationId' "$broray_tx_orphan_operation" 2>/dev/null)" || broray_tx_orphan_admit_rc=1
        broray_tx_orphan_target="$(jq -er '.targetVersion' "$broray_tx_orphan_operation" 2>/dev/null)" || broray_tx_orphan_admit_rc=1
        broray_tx_orphan_started="$(jq -er '.startedAt' "$broray_tx_orphan_operation" 2>/dev/null)" || broray_tx_orphan_admit_rc=1
    fi
    if [ "$broray_tx_orphan_admit_rc" -eq 0 ]; then
        broray_tx_valid_id "$broray_tx_orphan_id" &&
        [ "$broray_tx_orphan_path" = "$BRORAY_TX_TMP_BASE/broray-update-$broray_tx_orphan_id" ] &&
        [ "$broray_tx_orphan_target" = "$BRORAY_TX_TARGET_PACKAGE" ] || broray_tx_orphan_admit_rc=1
        case "$broray_tx_orphan_mode" in install|update|reinstall|opkg-upgrade|restore) ;; *) broray_tx_orphan_admit_rc=1 ;; esac
    fi
    if [ "$broray_tx_orphan_admit_rc" -eq 0 ]; then
        BRORAY_TX_WORK="$broray_tx_orphan_path"
        BRORAY_TX_OPERATION_ID="$broray_tx_orphan_id"
        BRORAY_TX_MODE="$broray_tx_orphan_mode"
        BRORAY_TX_SOURCE_PACKAGE="$broray_tx_orphan_source"
        BRORAY_TX_SOURCE_APP="$broray_tx_orphan_source_app"
        BRORAY_TX_SOURCE_CLASS="$broray_tx_orphan_source_class"
        BRORAY_TX_MIGRATION_ID="$broray_tx_orphan_migration"
        broray_tx_recovery_operation_validate "$broray_tx_orphan_operation" \
            "$broray_tx_orphan_id" "$broray_tx_orphan_mode" "$broray_tx_orphan_source" \
            "$broray_tx_orphan_target" "$broray_tx_orphan_path" "$broray_tx_orphan_started" ||
            broray_tx_orphan_admit_rc=1
    fi
    if [ "$broray_tx_orphan_admit_rc" -eq 0 ] &&
       { [ -e "$broray_tx_orphan_path/operation.json" ] || [ -L "$broray_tx_orphan_path/operation.json" ]; }; then
        [ -f "$broray_tx_orphan_path/operation.json" ] && [ ! -L "$broray_tx_orphan_path/operation.json" ] &&
            broray_tx_files_equal "$broray_tx_orphan_operation" "$broray_tx_orphan_path/operation.json" ||
            broray_tx_orphan_admit_rc=1
    fi
    if [ "$broray_tx_orphan_admit_rc" -eq 0 ]; then
        broray_tx_orphan_cap_json=0
        broray_tx_orphan_cap_tsv=0
        [ ! -e "$broray_tx_orphan_path/evidence/capabilities.json" ] &&
            [ ! -L "$broray_tx_orphan_path/evidence/capabilities.json" ] || broray_tx_orphan_cap_json=1
        [ ! -e "$broray_tx_orphan_path/evidence/capabilities.tsv" ] &&
            [ ! -L "$broray_tx_orphan_path/evidence/capabilities.tsv" ] || broray_tx_orphan_cap_tsv=1
        [ "$broray_tx_orphan_cap_json" -eq "$broray_tx_orphan_cap_tsv" ] || broray_tx_orphan_admit_rc=1
        if [ "$broray_tx_orphan_admit_rc" -eq 0 ] && [ "$broray_tx_orphan_cap_json" -eq 1 ]; then
            broray_runtime_reactivate_current_operation "$broray_tx_orphan_path" || broray_tx_orphan_admit_rc=1
        fi
    fi
    rm -rf "$broray_tx_orphan_admit" || return 1
    [ "$broray_tx_orphan_admit_rc" -eq 0 ] || return 1
    broray_tx_recovery_snapshot_admit_readonly || return 1
    BRORAY_TX_RECOVERY_SNAPSHOT_SOURCE=archive-capsule-only
    return 0
}

broray_tx_recovery_materialize_capsule()
{
    [ -n "${BRORAY_TX_RECOVERY_ARCHIVE_SHA:-}" ] &&
        [ "$(broray_tx_sha "$BRORAY_TX_WORK/backup.tar.gz")" = "$BRORAY_TX_RECOVERY_ARCHIVE_SHA" ] || return 1
    mkdir -p "$BRORAY_TX_WORK/evidence" "$BRORAY_TX_WORK/snapshot-meta" || return 1
    # An archive-only orphan has no volatile event stream.  Recovery creates
    # a fresh bounded stream for its own rollback/terminal audit; historical
    # events are neither required nor reconstructed from another operation.
    : >"$BRORAY_TX_WORK/evidence/events.tsv" || return 1
    tar -xzf "$BRORAY_TX_WORK/backup.tar.gz" -C "$BRORAY_TX_WORK" .broray-recovery \
        >"$BRORAY_TX_WORK/evidence/recovery-materialize.stdout" \
        2>"$BRORAY_TX_WORK/evidence/recovery-materialize.stderr" || return 1
    broray_tx_recovery_capsule_dir_verify "$BRORAY_TX_WORK/.broray-recovery" "$BRORAY_TX_WORK" || return 1
    for broray_tx_recovery_materialized in operation.json source.manifest source.allocation.manifest source-scope.list \
        services-before.tsv services-before.identity.tsv services-running-before.list \
        protected-source.manifest protected-source-roots.list candidate-target.json \
        opkg-status.before opkg-status.presence opkg-status.sha256 \
        foreign-status.before foreign-status.sha256; do
        cp -p "$BRORAY_TX_WORK/.broray-recovery/$broray_tx_recovery_materialized" \
            "$BRORAY_TX_WORK/$broray_tx_recovery_materialized" || return 1
    done
    cp -p "$BRORAY_TX_WORK/.broray-recovery/operation-relation-pre-snapshot.json" \
        "$BRORAY_TX_WORK/evidence/operation-relation-pre-snapshot.json" || return 1
    cp -p "$BRORAY_TX_WORK/.broray-recovery/capabilities.json" "$BRORAY_TX_WORK/evidence/capabilities.json" || return 1
    cp -p "$BRORAY_TX_WORK/.broray-recovery/capabilities.tsv" "$BRORAY_TX_WORK/evidence/capabilities.tsv" || return 1
    cp -p "$BRORAY_TX_WORK/.broray-recovery/recovery-space.json" "$BRORAY_TX_WORK/evidence/space.json" || return 1
    cp -p "$BRORAY_TX_WORK/.broray-recovery/keenetic-running-before.txt" \
        "$BRORAY_TX_WORK/snapshot-meta/keenetic-running-before.txt" || return 1
    printf '%s  backup.tar.gz\n' "$BRORAY_TX_RECOVERY_ARCHIVE_SHA" >"$BRORAY_TX_WORK/backup.tar.gz.sha256" || return 1
    printf '%s\n' "$BRORAY_TX_RECOVERY_ARCHIVE_SHA" >"$BRORAY_TX_WORK/snapshot.verified" || return 1
    return 0
}

# A handoff record is an exact reserved control object, not a cleanup
# filename allowlist.  When no current snapshot exists, retire it only after
# validating the complete schema, proving no live workspace owner and running
# an independent factual source admission.  Malformed/foreign bytes are never
# deleted or overwritten by a later transaction.
broray_tx_recover_stale_handoff_without_snapshot()
{
    [ -f "$BRORAY_TX_CURRENT" ] && [ ! -L "$BRORAY_TX_CURRENT" ] || return 2
    [ "$(wc -l <"$BRORAY_TX_CURRENT" | tr -d ' ')" -eq 1 ] || return 2
    broray_tx_handoff_id="$(jq -er '.operationId' "$BRORAY_TX_CURRENT" 2>/dev/null)" || return 2
    broray_tx_handoff_work="$(jq -er '.workspace' "$BRORAY_TX_CURRENT" 2>/dev/null)" || return 2
    broray_tx_handoff_snapshot="$(jq -er '.snapshotSha256' "$BRORAY_TX_CURRENT" 2>/dev/null)" || return 2
    broray_tx_valid_id "$broray_tx_handoff_id" || return 2
    [ "$broray_tx_handoff_work" = "$BRORAY_TX_TMP_BASE/broray-update-$broray_tx_handoff_id" ] || return 2
    case "$broray_tx_handoff_snapshot" in ''|*[!0-9a-f]*) return 2 ;; esac
    [ "${#broray_tx_handoff_snapshot}" -eq 64 ] || return 2
    jq -e --arg lifecycle "$BRORAY_TX_CONTRACT" '
      (type=="object") and
      ((keys|sort)==(["schemaVersion","operationId","workspace","origin","lifecycleContract",
                       "snapshotSha256","applicationPass"]|sort)) and
      (.schemaVersion==1) and (.lifecycleContract==$lifecycle) and
      ((.operationId|type)=="string") and ((.workspace|type)=="string") and
      ((.origin|type)=="string") and ((.origin|length)>0) and
      ((.snapshotSha256|type)=="string") and (.applicationPass==true)
    ' "$BRORAY_TX_CURRENT" >/dev/null 2>&1 || return 2
    broray_tx_handoff_workspace_present=false
    if [ -e "$broray_tx_handoff_work" ] || [ -L "$broray_tx_handoff_work" ]; then
        [ -d "$broray_tx_handoff_work" ] && [ ! -L "$broray_tx_handoff_work" ] || return 2
        [ ! -e "$broray_tx_handoff_work/backup.tar.gz" ] &&
            [ ! -L "$broray_tx_handoff_work/backup.tar.gz" ] || return 2
        broray_tx_control_owner_require_stale "$broray_tx_handoff_work/owner-identity.tsv" || return 2
        broray_tx_handoff_workspace_present=true
    fi
    # applicationPass=true can only have been emitted after the target tree
    # passed twice.  An absent live source contradicts the handoff and is not
    # ownership proof, even though absence is a valid fresh-install source.
    ( broray_tx_source_admit && [ "$BRORAY_TX_SOURCE_CLASS" = bro-any-structural ] ) || return 2
    [ -e "$BRORAY_TX_OPERATION_ROOT" ] || mkdir -p "$BRORAY_TX_OPERATION_ROOT" || return 2
    [ -d "$BRORAY_TX_OPERATION_ROOT" ] && [ ! -L "$BRORAY_TX_OPERATION_ROOT" ] || return 2
    broray_tx_handoff_history="$BRORAY_TX_OPERATION_ROOT/$broray_tx_handoff_id"
    if [ ! -e "$broray_tx_handoff_history" ] && [ ! -L "$broray_tx_handoff_history" ]; then
        mkdir "$broray_tx_handoff_history" || return 2
        chmod 700 "$broray_tx_handoff_history" 2>/dev/null || true
    fi
    [ -d "$broray_tx_handoff_history" ] && [ ! -L "$broray_tx_handoff_history" ] || return 2
    broray_tx_handoff_saved="$broray_tx_handoff_history/stale-handoff.json"
    if [ -e "$broray_tx_handoff_saved" ] || [ -L "$broray_tx_handoff_saved" ]; then
        [ -f "$broray_tx_handoff_saved" ] && [ ! -L "$broray_tx_handoff_saved" ] &&
            broray_tx_files_equal "$BRORAY_TX_CURRENT" "$broray_tx_handoff_saved" || return 2
    else
        [ ! -e "$broray_tx_handoff_saved.part" ] && [ ! -L "$broray_tx_handoff_saved.part" ] || return 2
        cp -p "$BRORAY_TX_CURRENT" "$broray_tx_handoff_saved.part" || return 2
        mv -f "$broray_tx_handoff_saved.part" "$broray_tx_handoff_saved" || return 2
    fi
    broray_tx_handoff_result="$broray_tx_handoff_history/stale-handoff-recovery.json"
    if [ -e "$broray_tx_handoff_result" ] || [ -L "$broray_tx_handoff_result" ]; then
        [ -f "$broray_tx_handoff_result" ] && [ ! -L "$broray_tx_handoff_result" ] || return 2
        jq -e --arg operationId "$broray_tx_handoff_id" --arg snapshotSha256 "$broray_tx_handoff_snapshot" '
          .status=="PASS" and .kind=="stale-handoff-no-current-snapshot" and
          .action=="owned-control-cleanup" and .operationId==$operationId and
          .snapshotSha256==$snapshotSha256 and .historicalInput==false
        ' "$broray_tx_handoff_result" >/dev/null 2>&1 || return 2
    else
        [ ! -e "$broray_tx_handoff_result.part" ] && [ ! -L "$broray_tx_handoff_result.part" ] || return 2
        jq -nc --arg operationId "$broray_tx_handoff_id" --arg snapshotSha256 "$broray_tx_handoff_snapshot" \
            --arg recoveredAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
            --argjson workspacePresent "$broray_tx_handoff_workspace_present" '
          {schemaVersion:1,status:"PASS",kind:"stale-handoff-no-current-snapshot",
           action:"owned-control-cleanup",operationId:$operationId,snapshotSha256:$snapshotSha256,
           independentStructuralSourceVerified:true,workspacePresent:$workspacePresent,
           workspacePreserved:$workspacePresent,historicalInput:false,recoveredAt:$recoveredAt}
        ' >"$broray_tx_handoff_result.part" || return 2
        mv -f "$broray_tx_handoff_result.part" "$broray_tx_handoff_result" || return 2
    fi
    rm -f "$BRORAY_TX_CURRENT" || return 2
    [ ! -e "$BRORAY_TX_CURRENT" ] && [ ! -L "$BRORAY_TX_CURRENT" ]
}

broray_tx_recover_marker_only_control()
{
    [ ! -e "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] || return 2
    [ -f "$BRORAY_TX_LEGACY_MARKER" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER" ] || return 2
    [ "$(wc -l <"$BRORAY_TX_LEGACY_MARKER" | tr -d ' ')" -eq 1 ] || return 2
    broray_tx_marker_only_id="$(jq -er '.operationId' "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 2
    broray_tx_marker_only_mode="$(jq -er '.mode' "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 2
    broray_tx_marker_only_source="$(jq -er '.sourceVersion' "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 2
    broray_tx_marker_only_target="$(jq -er '.targetVersion' "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 2
    broray_tx_marker_only_work="$(jq -er '.transactionPath' "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 2
    broray_tx_marker_only_started="$(jq -er '.startedAt' "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 2
    broray_tx_marker_only_phase="$(jq -er '.phase' "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 2
    broray_tx_valid_id "$broray_tx_marker_only_id" || return 2
    [ "$broray_tx_marker_only_target" = "$BRORAY_TX_TARGET_PACKAGE" ] || return 2
    [ "$broray_tx_marker_only_work" = "$BRORAY_TX_TMP_BASE/broray-update-$broray_tx_marker_only_id" ] || return 2
    case "$broray_tx_marker_only_mode" in install|update|reinstall|opkg-upgrade|restore) ;; *) return 2 ;; esac
    case "$broray_tx_marker_only_phase" in control-prelude|pre-snapshot) ;; *) return 2 ;; esac
    jq -e --arg lifecycle "$BRORAY_TX_CONTRACT" '
      (keys|sort)==(["lifecycleContract","mode","operationId","phase","schemaVersion","snapshotSha256",
                     "sourceVersion","startedAt","status","targetVersion","transactionPath"]|sort) and
      .schemaVersion==2 and .lifecycleContract==$lifecycle and .status=="active" and .snapshotSha256==null
    ' "$BRORAY_TX_LEGACY_MARKER" >/dev/null 2>&1 || return 2
    if [ "$broray_tx_marker_only_phase" = control-prelude ]; then
        broray_tx_marker_only_expected="$(printf '{"schemaVersion":2,"lifecycleContract":"%s","operationId":"%s","mode":"%s","sourceVersion":"%s","targetVersion":"%s","transactionPath":"%s","phase":"control-prelude","status":"active","startedAt":"%s","snapshotSha256":null}' \
            "$BRORAY_TX_CONTRACT" "$broray_tx_marker_only_id" "$broray_tx_marker_only_mode" \
            "$broray_tx_marker_only_source" "$broray_tx_marker_only_target" "$broray_tx_marker_only_work" "$broray_tx_marker_only_started")"
    else
        broray_tx_marker_only_expected="$(printf '{"lifecycleContract":"%s","mode":"%s","operationId":"%s","phase":"pre-snapshot","schemaVersion":2,"snapshotSha256":null,"sourceVersion":"%s","startedAt":"%s","status":"active","targetVersion":"%s","transactionPath":"%s"}' \
            "$BRORAY_TX_CONTRACT" "$broray_tx_marker_only_mode" "$broray_tx_marker_only_id" \
            "$broray_tx_marker_only_source" "$broray_tx_marker_only_started" "$broray_tx_marker_only_target" "$broray_tx_marker_only_work")"
    fi
    [ "$(sed -n '1p' "$BRORAY_TX_LEGACY_MARKER")" = "$broray_tx_marker_only_expected" ] || return 2
    if [ -e "$broray_tx_marker_only_work" ] || [ -L "$broray_tx_marker_only_work" ]; then
        [ -d "$broray_tx_marker_only_work" ] && [ ! -L "$broray_tx_marker_only_work" ] || return 2
        [ ! -e "$broray_tx_marker_only_work/backup.tar.gz" ] && [ ! -L "$broray_tx_marker_only_work/backup.tar.gz" ] || return 2
        broray_tx_control_owner_require_stale "$broray_tx_marker_only_work/owner-identity.tsv" || return 2
        if [ -e "$broray_tx_marker_only_work/operation.json" ] || [ -L "$broray_tx_marker_only_work/operation.json" ]; then
            [ -f "$broray_tx_marker_only_work/operation.json" ] && [ ! -L "$broray_tx_marker_only_work/operation.json" ] || return 2
            broray_tx_recovery_operation_validate "$broray_tx_marker_only_work/operation.json" \
                "$broray_tx_marker_only_id" "$broray_tx_marker_only_mode" "$broray_tx_marker_only_source" \
                "$broray_tx_marker_only_target" "$broray_tx_marker_only_work" "$broray_tx_marker_only_started" || return 2
        fi
    fi
    broray_tx_marker_only_caller_id="$BRORAY_TX_OPERATION_ID"
    broray_tx_marker_only_caller_mode="$BRORAY_TX_MODE"
    broray_tx_source_admit || return 2
    case "$broray_tx_marker_only_mode:$BRORAY_TX_SOURCE_CLASS" in
        install:absent|update:bro-any-structural|reinstall:bro-any-structural|opkg-upgrade:bro-any-structural|restore:bro-any-structural) ;;
        *) return 2 ;;
    esac
    BRORAY_TX_OPERATION_ID="$broray_tx_marker_only_id"
    BRORAY_TX_MODE="$broray_tx_marker_only_mode"
    BRORAY_TX_SOURCE_PACKAGE="$broray_tx_marker_only_source"
    BRORAY_TX_WORK="$broray_tx_marker_only_work"
    BRORAY_TX_RECOVERY_SNAPSHOT_SOURCE=marker-only-no-snapshot
    broray_tx_recovery_record pre-mutation volatile-cleanup || return 2
    if [ -d "$BRORAY_TX_WORK" ] && [ ! -L "$BRORAY_TX_WORK" ]; then
        broray_tx_guard_work "$BRORAY_TX_WORK" || return 2
        rm -rf "$BRORAY_TX_WORK" || return 2
    fi
    rm -f "$BRORAY_TX_LEGACY_MARKER" || return 2
    BRORAY_TX_OPERATION_ID="$broray_tx_marker_only_caller_id"
    BRORAY_TX_MODE="$broray_tx_marker_only_caller_mode"
    BRORAY_TX_WORK=""
    return 0
}

broray_tx_recover_orphan_snapshot()
{
    set --
    for broray_tx_orphan_path in "$BRORAY_TX_TMP_BASE"/broray-update-*; do
        [ -e "$broray_tx_orphan_path" ] || [ -L "$broray_tx_orphan_path" ] || continue
        [ -d "$broray_tx_orphan_path" ] && [ ! -L "$broray_tx_orphan_path" ] || return 2
        # Historical operation-only directories are not snapshot candidates.
        # Selection counts only current, regular backup archives.
        if [ -e "$broray_tx_orphan_path/backup.tar.gz" ] || [ -L "$broray_tx_orphan_path/backup.tar.gz" ]; then
            set -- "$@" "$broray_tx_orphan_path"
        fi
    done
    if [ "$#" -eq 0 ]; then
        if [ -e "$BRORAY_TX_CURRENT" ] || [ -L "$BRORAY_TX_CURRENT" ]; then
            broray_tx_recover_stale_handoff_without_snapshot || return 2
        fi
        if [ -e "$BRORAY_TX_LEGACY_MARKER" ] || [ -L "$BRORAY_TX_LEGACY_MARKER" ]; then
            broray_tx_recover_marker_only_control
            return $?
        fi
        return 0
    fi
    [ "$#" -eq 1 ] || return 2
    broray_tx_orphan_path="$1"
    [ -f "$broray_tx_orphan_path/backup.tar.gz" ] && [ ! -L "$broray_tx_orphan_path/backup.tar.gz" ] || return 2
    broray_tx_recovery_orphan_identity_admit "$broray_tx_orphan_path" || return 2
    case "$BRORAY_TX_RECOVERY_FACTUAL_STATE" in source-exact|source-changed) ;; *) return 2 ;; esac
    broray_tx_recovery_no_second_live "$broray_tx_orphan_path" || return 2
    broray_tx_orphan_ownerless=0
    if [ -e "$broray_tx_orphan_path/owner-identity.tsv" ] ||
       [ -L "$broray_tx_orphan_path/owner-identity.tsv" ]; then
        broray_tx_control_owner_require_stale "$broray_tx_orphan_path/owner-identity.tsv" || return 2
        BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE="$BRORAY_TX_CONTROL_OWNER_STATE"
    else
        broray_tx_orphan_ownerless=1
        BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE=dead
    fi
    if [ -e "$BRORAY_TX_LEGACY_MARKER" ] || [ -L "$BRORAY_TX_LEGACY_MARKER" ]; then
        [ -f "$BRORAY_TX_LEGACY_MARKER" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER" ] || return 2
        jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg mode "$BRORAY_TX_MODE" \
            --arg source "$BRORAY_TX_SOURCE_PACKAGE" --arg target "$broray_tx_orphan_target" \
            --arg work "$BRORAY_TX_WORK" --arg started "$broray_tx_orphan_started" \
            --arg lifecycle "$BRORAY_TX_CONTRACT" '
          .schemaVersion==2 and .lifecycleContract==$lifecycle and .operationId==$id and
          .mode==$mode and .sourceVersion==$source and .targetVersion==$target and
          .transactionPath==$work and .startedAt==$started and
          (.phase=="pre-snapshot" or .phase=="snapshot-verified" or .phase=="candidate-verified" or
           .phase=="mutation-started" or .phase=="rollback-verified" or
           .phase=="rollback-failed" or .phase=="success-committed")
        ' "$BRORAY_TX_LEGACY_MARKER" >/dev/null 2>&1 || return 2
    fi

    # A terminal crash may have removed the workspace owner immediately before
    # its atomic retirement rename.  Re-publish that owner while the kernel
    # mutex proves no transaction lock can appear; a crash after this point
    # leaves an ordinary stale owner and is retryable.
    if [ "$broray_tx_orphan_ownerless" -eq 1 ]; then
        broray_tx_control_transition_begin || return 2
        broray_tx_orphan_owner_publish_rc=1
        if [ ! -e "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] &&
           [ -d "$broray_tx_orphan_path" ] && [ ! -L "$broray_tx_orphan_path" ] &&
           [ ! -e "$broray_tx_orphan_path/owner-identity.tsv" ] &&
           [ ! -L "$broray_tx_orphan_path/owner-identity.tsv" ] &&
           [ "$(broray_tx_sha "$broray_tx_orphan_path/backup.tar.gz")" = \
                "$BRORAY_TX_RECOVERY_ARCHIVE_SHA" ] &&
           broray_tx_control_owner_write_atomic \
                "$broray_tx_orphan_path/owner-identity.tsv" &&
           broray_tx_control_owner_assert_self \
                "$broray_tx_orphan_path/owner-identity.tsv"
        then
            broray_tx_orphan_owner_publish_rc=0
        fi
        broray_tx_control_transition_end || return 2
        [ "$broray_tx_orphan_owner_publish_rc" -eq 0 ] || return 2
    fi

    # The unique snapshot is fully admitted.  Publish fresh control through the
    # same owner-last kernel-fenced primitive used by a normal transaction;
    # invalid or multiple orphans above never change retained state.
    broray_tx_lock_publish "$broray_tx_orphan_started" || return 2
    broray_tx_recovery_materialize_capsule || return 2
    if [ ! -e "$BRORAY_TX_LEGACY_MARKER" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER" ]; then
        [ -e "$BRORAY_TX_STATE_ROOT" ] || mkdir -p "$BRORAY_TX_STATE_ROOT" || return 2
        broray_tx_orphan_status=active
        if [ "$(sed -n '1p' "$BRORAY_TX_WORK/outcome" 2>/dev/null)" = rollback-failed ]; then
            broray_tx_orphan_phase=rollback-failed
            broray_tx_orphan_status=rollback-failed
        else
            case "$BRORAY_TX_RECOVERY_FACTUAL_STATE" in
                source-exact) broray_tx_orphan_phase=snapshot-verified ;;
                source-changed)
                    broray_tx_orphan_phase=mutation-started
                    printf '%s\n' yes >"$BRORAY_TX_WORK/mutation.started" || return 2
                    ;;
            esac
        fi
        jq -ncS --arg lifecycle "$BRORAY_TX_CONTRACT" --arg id "$BRORAY_TX_OPERATION_ID" \
            --arg mode "$BRORAY_TX_MODE" --arg source "$BRORAY_TX_SOURCE_PACKAGE" \
            --arg target "$broray_tx_orphan_target" --arg work "$BRORAY_TX_WORK" \
            --arg started "$broray_tx_orphan_started" --arg phase "$broray_tx_orphan_phase" \
            --arg status "$broray_tx_orphan_status" \
            --arg snapshot "$BRORAY_TX_RECOVERY_ARCHIVE_SHA" '
          {schemaVersion:2,lifecycleContract:$lifecycle,operationId:$id,mode:$mode,
           sourceVersion:$source,targetVersion:$target,transactionPath:$work,phase:$phase,
           status:$status,startedAt:$started,snapshotSha256:$snapshot}
        ' >"$BRORAY_TX_LEGACY_MARKER.part" || return 2
        chmod 600 "$BRORAY_TX_LEGACY_MARKER.part" 2>/dev/null || true
        mv -f "$BRORAY_TX_LEGACY_MARKER.part" "$BRORAY_TX_LEGACY_MARKER" || return 2
    fi
    broray_tx_persistent_marker_validate || return 2
    BRORAY_TX_RECOVERY_FRESH_CONTROL=1
    broray_tx_recover_stale_control
    broray_tx_orphan_recover_rc=$?
    BRORAY_TX_RECOVERY_FRESH_CONTROL=0
    BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE=""
    return "$broray_tx_orphan_recover_rc"
}

# A committed transaction control can exist briefly before work_init finishes
# its minimal workspace/operation/marker prelude.  If that owner later dies,
# this exact bounded state is provably pre-mutation.  Normalize only the known
# partial workspace shape, then retire control under the kernel fence so the
# same operation id is immediately retryable.
broray_tx_recover_incomplete_control_prelude()
{
    [ ! -e "$BRORAY_TX_CURRENT" ] && [ ! -L "$BRORAY_TX_CURRENT" ] || return 2
    broray_tx_prelude_started="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/started-at" 2>/dev/null)"
    case "$broray_tx_prelude_started" in ????-??-??T??:??:??Z) ;; *) return 2 ;; esac
    if [ -e "$BRORAY_TX_LEGACY_MARKER.part" ] || [ -L "$BRORAY_TX_LEGACY_MARKER.part" ]; then
        [ -f "$BRORAY_TX_LEGACY_MARKER.part" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER.part" ] || return 2
        broray_tx_prelude_marker_part_bytes="$(wc -c <"$BRORAY_TX_LEGACY_MARKER.part" 2>/dev/null | tr -d ' ')" || return 2
        case "$broray_tx_prelude_marker_part_bytes" in ''|*[!0-9]*) return 2 ;; esac
        [ "$broray_tx_prelude_marker_part_bytes" -le "$BRORAY_TX_METADATA_MAX_BYTES" ] || return 2
    fi
    if [ -e "$BRORAY_TX_LEGACY_MARKER" ] || [ -L "$BRORAY_TX_LEGACY_MARKER" ]; then
        [ -f "$BRORAY_TX_LEGACY_MARKER" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER" ] || return 2
        [ "$(wc -l <"$BRORAY_TX_LEGACY_MARKER" | tr -d ' ')" -eq 1 ] || return 2
        broray_tx_prelude_marker_expected="$(printf '{"schemaVersion":2,"lifecycleContract":"%s","operationId":"%s","mode":"%s","sourceVersion":"%s","targetVersion":"%s","transactionPath":"%s","phase":"control-prelude","status":"active","startedAt":"%s","snapshotSha256":null}' \
            "$BRORAY_TX_CONTRACT" "$broray_tx_recovery_id" "$broray_tx_recovery_mode" \
            "$broray_tx_recovery_source" "$broray_tx_recovery_target" \
            "$broray_tx_recovery_path" "$broray_tx_prelude_started")"
        [ "$(sed -n '1p' "$BRORAY_TX_LEGACY_MARKER")" = \
            "$broray_tx_prelude_marker_expected" ] || return 2
    fi
    if [ -e "$broray_tx_recovery_path" ] || [ -L "$broray_tx_recovery_path" ]; then
        [ -d "$broray_tx_recovery_path" ] && [ ! -L "$broray_tx_recovery_path" ] || return 2
        [ ! -e "$broray_tx_recovery_path/backup.tar.gz" ] &&
            [ ! -L "$broray_tx_recovery_path/backup.tar.gz" ] &&
        [ ! -e "$broray_tx_recovery_path/mutation.started" ] &&
            [ ! -L "$broray_tx_recovery_path/mutation.started" ] || return 2
        for broray_tx_prelude_entry in "$broray_tx_recovery_path"/* "$broray_tx_recovery_path"/.[!.]* "$broray_tx_recovery_path"/..?*; do
            [ -e "$broray_tx_prelude_entry" ] || [ -L "$broray_tx_prelude_entry" ] || continue
            case "${broray_tx_prelude_entry##*/}" in
                evidence)
                    [ -d "$broray_tx_prelude_entry" ] && [ ! -L "$broray_tx_prelude_entry" ] || return 2
                    for broray_tx_prelude_evidence in "$broray_tx_prelude_entry"/* "$broray_tx_prelude_entry"/.[!.]* "$broray_tx_prelude_entry"/..?*; do
                        [ -e "$broray_tx_prelude_evidence" ] || [ -L "$broray_tx_prelude_evidence" ] || continue
                        [ "${broray_tx_prelude_evidence##*/}" = events.tsv ] &&
                            [ -f "$broray_tx_prelude_evidence" ] && [ ! -L "$broray_tx_prelude_evidence" ] || return 2
                    done
                    ;;
                snapshot-meta)
                    [ -d "$broray_tx_prelude_entry" ] && [ ! -L "$broray_tx_prelude_entry" ] || return 2
                    for broray_tx_prelude_snapshot in "$broray_tx_prelude_entry"/* "$broray_tx_prelude_entry"/.[!.]* "$broray_tx_prelude_entry"/..?*; do
                        [ -e "$broray_tx_prelude_snapshot" ] || [ -L "$broray_tx_prelude_snapshot" ] || continue
                        return 2
                    done
                    ;;
                operation-id|mode|origin|lifecycle-contract|owner-identity.tsv|operation.json)
                    [ -f "$broray_tx_prelude_entry" ] && [ ! -L "$broray_tx_prelude_entry" ] || return 2
                    broray_tx_prelude_bytes="$(wc -c <"$broray_tx_prelude_entry" 2>/dev/null | tr -d ' ')" || return 2
                    broray_tx_number "$broray_tx_prelude_bytes" &&
                        [ "$broray_tx_prelude_bytes" -le "$BRORAY_TX_METADATA_MAX_BYTES" ] || return 2
                    ;;
                *) return 2 ;;
            esac
        done
        for broray_tx_prelude_pair in \
            "operation-id:$broray_tx_recovery_id" "mode:$broray_tx_recovery_mode" \
            "lifecycle-contract:$BRORAY_TX_CONTRACT"
        do
            broray_tx_prelude_name="${broray_tx_prelude_pair%%:*}"
            broray_tx_prelude_value="${broray_tx_prelude_pair#*:}"
            if [ -e "$broray_tx_recovery_path/$broray_tx_prelude_name" ] ||
               [ -L "$broray_tx_recovery_path/$broray_tx_prelude_name" ]; then
                [ -f "$broray_tx_recovery_path/$broray_tx_prelude_name" ] &&
                    [ ! -L "$broray_tx_recovery_path/$broray_tx_prelude_name" ] &&
                    [ "$(sed -n '1p' "$broray_tx_recovery_path/$broray_tx_prelude_name")" = \
                        "$broray_tx_prelude_value" ] || return 2
            fi
        done
        if [ -e "$broray_tx_recovery_path/operation.json" ] ||
           [ -L "$broray_tx_recovery_path/operation.json" ]; then
            [ -f "$broray_tx_recovery_path/operation.json" ] &&
                [ ! -L "$broray_tx_recovery_path/operation.json" ] || return 2
            [ "$(wc -l <"$broray_tx_recovery_path/operation.json" | tr -d ' ')" -eq 1 ] || return 2
            broray_tx_prelude_operation_expected="$(printf '{"schemaVersion":2,"lifecycleContract":"%s","operationId":"%s","mode":"%s","sourceVersion":"%s","targetVersion":"%s","transactionPath":"%s","startedAt":"%s","mutationStarted":false}' \
                "$BRORAY_TX_CONTRACT" "$broray_tx_recovery_id" "$broray_tx_recovery_mode" \
                "$broray_tx_recovery_source" "$broray_tx_recovery_target" \
                "$broray_tx_recovery_path" "$broray_tx_prelude_started")"
            [ "$(sed -n '1p' "$broray_tx_recovery_path/operation.json")" = \
                "$broray_tx_prelude_operation_expected" ] || return 2
        fi
        if [ -e "$broray_tx_recovery_path/owner-identity.tsv" ] ||
           [ -L "$broray_tx_recovery_path/owner-identity.tsv" ]; then
            [ -f "$broray_tx_recovery_path/owner-identity.tsv" ] &&
                [ ! -L "$broray_tx_recovery_path/owner-identity.tsv" ] &&
                broray_tx_control_owner_require_stale \
                    "$broray_tx_recovery_path/owner-identity.tsv" &&
                broray_tx_files_equal "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" \
                    "$broray_tx_recovery_path/owner-identity.tsv" || return 2
        fi
    fi

    broray_tx_prelude_caller_id="$BRORAY_TX_OPERATION_ID"
    broray_tx_prelude_caller_mode="$BRORAY_TX_MODE"
    broray_tx_prelude_caller_work="$BRORAY_TX_WORK"
    BRORAY_TX_OPERATION_ID="$broray_tx_recovery_id"
    BRORAY_TX_MODE="$broray_tx_recovery_mode"
    BRORAY_TX_WORK="$broray_tx_recovery_path"
    BRORAY_TX_SOURCE_PACKAGE="$broray_tx_recovery_source"
    broray_tx_prelude_expected_source="$BRORAY_TX_SOURCE_PACKAGE"
    broray_tx_source_admit || return 2
    [ "$BRORAY_TX_SOURCE_PACKAGE" = "$broray_tx_prelude_expected_source" ] || return 2
    case "$BRORAY_TX_MODE:$BRORAY_TX_SOURCE_CLASS" in
        install:absent|update:bro-any-structural|reinstall:bro-any-structural|opkg-upgrade:bro-any-structural|restore:bro-any-structural) ;;
        *) return 2 ;;
    esac
    broray_tx_recovery_takeover_current_control || return 2
    # Retire both control collections in one bounded kernel-fenced transition.
    # Each owner is removed first; no unowned residue is exposed before all
    # exact pre-mutation bytes have disappeared.
    broray_tx_control_transition_begin || return 2
    broray_tx_prelude_retire_rc=0
    broray_tx_control_owner_assert_self "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ||
        broray_tx_prelude_retire_rc=1
    if [ "$broray_tx_prelude_retire_rc" -eq 0 ] &&
       [ -d "$BRORAY_TX_WORK" ] && [ ! -L "$BRORAY_TX_WORK" ]; then
        if [ -e "$BRORAY_TX_WORK/owner-identity.tsv" ] ||
           [ -L "$BRORAY_TX_WORK/owner-identity.tsv" ]; then
            broray_tx_control_owner_assert_self "$BRORAY_TX_WORK/owner-identity.tsv" &&
                rm -f "$BRORAY_TX_WORK/owner-identity.tsv" || broray_tx_prelude_retire_rc=1
        fi
        if [ "$broray_tx_prelude_retire_rc" -eq 0 ]; then
            broray_tx_guard_work "$BRORAY_TX_WORK" && rm -rf "$BRORAY_TX_WORK" ||
                broray_tx_prelude_retire_rc=1
        fi
    fi
    if [ "$broray_tx_prelude_retire_rc" -eq 0 ]; then
        rm -f "$BRORAY_TX_LEGACY_MARKER" "$BRORAY_TX_LEGACY_MARKER.part" ||
            broray_tx_prelude_retire_rc=1
    fi
    if [ "$broray_tx_prelude_retire_rc" -eq 0 ]; then
        rm -f "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" || broray_tx_prelude_retire_rc=1
        for broray_tx_prelude_lock_file in operation-id operation-type started-at source-version target-version; do
            rm -f "$BRORAY_TX_LOCK_DIR/$broray_tx_prelude_lock_file" ||
                broray_tx_prelude_retire_rc=1
        done
        [ "$broray_tx_prelude_retire_rc" -ne 0 ] || rmdir "$BRORAY_TX_LOCK_DIR" ||
            broray_tx_prelude_retire_rc=1
    fi
    broray_tx_control_transition_end || return 2
    [ "$broray_tx_prelude_retire_rc" -eq 0 ] || return 2
    BRORAY_TX_LOCK_HELD=0
    BRORAY_TX_OPERATION_ID="$broray_tx_prelude_caller_id"
    BRORAY_TX_MODE="$broray_tx_prelude_caller_mode"
    BRORAY_TX_WORK="$broray_tx_prelude_caller_work"
    BRORAY_TX_SOURCE_PACKAGE=""
    BRORAY_TX_SOURCE_APP=""
    BRORAY_TX_SOURCE_CLASS=""
    BRORAY_TX_MIGRATION_ID=""
    return 0
}

# Reconstitute an ownerless transaction residue without ever exposing an
# unlocked "no transaction" window.  The kernel mutex is acquired before the
# tuple is rechecked; workspace ownership is committed first and the lock
# owner is committed last.  Snapshot admission remains read-only and happens
# before that bounded transition.
broray_tx_recover_ownerless_control()
{
    [ -d "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] || return 2
    [ ! -e "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ] &&
        [ ! -L "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ] || return 2
    for broray_tx_ownerless_required in operation-id operation-type started-at source-version target-version; do
        [ -f "$BRORAY_TX_LOCK_DIR/$broray_tx_ownerless_required" ] &&
            [ ! -L "$BRORAY_TX_LOCK_DIR/$broray_tx_ownerless_required" ] &&
            [ "$(wc -l <"$BRORAY_TX_LOCK_DIR/$broray_tx_ownerless_required" | tr -d ' ')" -eq 1 ] ||
            return 2
    done
    for broray_tx_ownerless_entry in "$BRORAY_TX_LOCK_DIR"/* "$BRORAY_TX_LOCK_DIR"/.[!.]* "$BRORAY_TX_LOCK_DIR"/..?*; do
        [ -e "$broray_tx_ownerless_entry" ] || [ -L "$broray_tx_ownerless_entry" ] || continue
        case "${broray_tx_ownerless_entry##*/}" in
            operation-id|operation-type|started-at|source-version|target-version)
                [ -f "$broray_tx_ownerless_entry" ] && [ ! -L "$broray_tx_ownerless_entry" ] || return 2 ;;
            *) return 2 ;;
        esac
    done
    broray_tx_ownerless_id="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-id")"
    broray_tx_ownerless_mode="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-type")"
    broray_tx_ownerless_started="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/started-at")"
    broray_tx_ownerless_source="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/source-version")"
    broray_tx_ownerless_target="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/target-version")"
    broray_tx_valid_id "$broray_tx_ownerless_id" || return 2
    case "$broray_tx_ownerless_mode" in install|update|reinstall|opkg-upgrade|restore) ;; *) return 2 ;; esac
    case "$broray_tx_ownerless_started" in ????-??-??T??:??:??Z) ;; *) return 2 ;; esac
    [ "$broray_tx_ownerless_target" = "$BRORAY_TX_TARGET_PACKAGE" ] || return 2
    broray_tx_ownerless_work="$BRORAY_TX_TMP_BASE/broray-update-$broray_tx_ownerless_id"

    broray_tx_ownerless_has_snapshot=0
    if [ -e "$broray_tx_ownerless_work/backup.tar.gz" ] ||
       [ -L "$broray_tx_ownerless_work/backup.tar.gz" ]; then
        [ -f "$broray_tx_ownerless_work/backup.tar.gz" ] &&
            [ ! -L "$broray_tx_ownerless_work/backup.tar.gz" ] || return 2
        broray_tx_ownerless_has_snapshot=1
        broray_tx_recovery_orphan_identity_admit "$broray_tx_ownerless_work" || return 2
        [ "$BRORAY_TX_OPERATION_ID" = "$broray_tx_ownerless_id" ] &&
        [ "$BRORAY_TX_MODE" = "$broray_tx_ownerless_mode" ] &&
        [ "$BRORAY_TX_SOURCE_PACKAGE" = "$broray_tx_ownerless_source" ] &&
        [ "$broray_tx_orphan_target" = "$broray_tx_ownerless_target" ] &&
        [ "$broray_tx_orphan_started" = "$broray_tx_ownerless_started" ] || return 2
    else
        BRORAY_TX_OPERATION_ID="$broray_tx_ownerless_id"
        BRORAY_TX_MODE="$broray_tx_ownerless_mode"
        BRORAY_TX_SOURCE_PACKAGE="$broray_tx_ownerless_source"
        BRORAY_TX_WORK="$broray_tx_ownerless_work"
    fi
    broray_tx_recovery_no_second_live "$broray_tx_ownerless_work" || return 2

    broray_tx_control_transition_begin || return 2
    broray_tx_ownerless_publish_rc=1
    if [ -d "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] &&
       [ ! -e "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ] &&
       [ ! -L "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ] &&
       [ "$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-id")" = "$broray_tx_ownerless_id" ] &&
       [ "$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-type")" = "$broray_tx_ownerless_mode" ] &&
       [ "$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/started-at")" = "$broray_tx_ownerless_started" ] &&
       [ "$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/source-version")" = "$broray_tx_ownerless_source" ] &&
       [ "$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/target-version")" = "$broray_tx_ownerless_target" ]
    then
        broray_tx_ownerless_workspace_ok=1
        if [ -e "$broray_tx_ownerless_work" ] || [ -L "$broray_tx_ownerless_work" ]; then
            [ -d "$broray_tx_ownerless_work" ] && [ ! -L "$broray_tx_ownerless_work" ] ||
                broray_tx_ownerless_workspace_ok=0
            if [ "$broray_tx_ownerless_workspace_ok" -eq 1 ] &&
               { [ -e "$broray_tx_ownerless_work/owner-identity.tsv" ] ||
                 [ -L "$broray_tx_ownerless_work/owner-identity.tsv" ]; }; then
                [ -f "$broray_tx_ownerless_work/owner-identity.tsv" ] &&
                    [ ! -L "$broray_tx_ownerless_work/owner-identity.tsv" ] &&
                    broray_tx_control_owner_require_stale \
                        "$broray_tx_ownerless_work/owner-identity.tsv" ||
                    broray_tx_ownerless_workspace_ok=0
            fi
            if [ "$broray_tx_ownerless_workspace_ok" -eq 1 ]; then
                broray_tx_control_owner_write_atomic \
                    "$broray_tx_ownerless_work/owner-identity.tsv" ||
                    broray_tx_ownerless_workspace_ok=0
            fi
        elif [ "$broray_tx_ownerless_has_snapshot" -eq 1 ]; then
            broray_tx_ownerless_workspace_ok=0
        fi
        if [ "$broray_tx_ownerless_workspace_ok" -eq 1 ] &&
           broray_tx_control_owner_write_atomic "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" &&
           broray_tx_control_owner_assert_self "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" &&
           { [ ! -d "$broray_tx_ownerless_work" ] ||
             broray_tx_control_owner_assert_self \
                "$broray_tx_ownerless_work/owner-identity.tsv"; }
        then
            broray_tx_ownerless_publish_rc=0
        fi
    fi
    broray_tx_control_transition_end || return 2
    [ "$broray_tx_ownerless_publish_rc" -eq 0 ] || return 2
    BRORAY_TX_RECOVERY_EXPECTED_OWNER_SHA256="$(broray_tx_sha "$BRORAY_TX_LOCK_DIR/owner-identity.tsv")" || return 2
    BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE=dead
    BRORAY_TX_RECOVERY_FRESH_CONTROL=1
    BRORAY_TX_LOCK_HELD=1
    broray_tx_recover_stale_control
    broray_tx_ownerless_recover_rc=$?
    BRORAY_TX_RECOVERY_FRESH_CONTROL=0
    BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE=""
    return "$broray_tx_ownerless_recover_rc"
}

# Restart recovery is phase-driven.  PID/marker/history are diagnostics; the
# operation record and the self-contained current snapshot must agree before
# any rollback is allowed.  Ambiguous or invalid state is preserved intact.
broray_tx_recover_stale_control()
{
    if [ "$BRORAY_TX_RECOVERY_FRESH_CONTROL" -ne 1 ]; then
        BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE=""
        BRORAY_TX_RECOVERY_PREDECESSOR_OWNER_IDENTITY=""
    fi
    if [ ! -e "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ]; then
        broray_tx_recover_orphan_snapshot
        return $?
    fi
    [ -d "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] || return 2
    if [ ! -e "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ] &&
       [ ! -L "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ]; then
        broray_tx_recover_ownerless_control
        return $?
    fi
    broray_tx_recovery_lock_complete=true
    for broray_tx_recovery_file in operation-id owner-identity.tsv operation-type started-at source-version target-version; do
        if [ ! -f "$BRORAY_TX_LOCK_DIR/$broray_tx_recovery_file" ] ||
           [ -L "$BRORAY_TX_LOCK_DIR/$broray_tx_recovery_file" ]; then
            broray_tx_recovery_lock_complete=false
        fi
    done
    if [ "$broray_tx_recovery_lock_complete" != true ]; then
        broray_tx_recover_orphan_snapshot
        return $?
    fi
    for broray_tx_recovery_file in operation-id owner-identity.tsv operation-type started-at source-version target-version; do
        [ -f "$BRORAY_TX_LOCK_DIR/$broray_tx_recovery_file" ] && [ ! -L "$BRORAY_TX_LOCK_DIR/$broray_tx_recovery_file" ] || return 2
        [ "$(wc -l <"$BRORAY_TX_LOCK_DIR/$broray_tx_recovery_file" | tr -d ' ')" -eq 1 ] || return 2
    done
    for broray_tx_recovery_entry in "$BRORAY_TX_LOCK_DIR"/* "$BRORAY_TX_LOCK_DIR"/.[!.]* "$BRORAY_TX_LOCK_DIR"/..?*; do
        [ -e "$broray_tx_recovery_entry" ] || [ -L "$broray_tx_recovery_entry" ] || continue
        case "${broray_tx_recovery_entry##*/}" in
            operation-id|owner-identity.tsv|operation-type|started-at|source-version|target-version)
                [ -f "$broray_tx_recovery_entry" ] && [ ! -L "$broray_tx_recovery_entry" ] || return 2 ;;
            *) return 2 ;;
        esac
    done
    broray_tx_recovery_id="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-id")"
    broray_tx_recovery_mode="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-type")"
    broray_tx_recovery_source="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/source-version")"
    broray_tx_recovery_target="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/target-version")"
    broray_tx_valid_id "$broray_tx_recovery_id" || return 2
    broray_tx_control_owner_authorize_recovery "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" || return 2
    BRORAY_TX_RECOVERY_EXPECTED_OWNER_SHA256="$(broray_tx_sha "$BRORAY_TX_LOCK_DIR/owner-identity.tsv")" || return 2
    broray_tx_recovery_owner_state="$BRORAY_TX_CONTROL_OWNER_STATE"
    [ -n "$BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE" ] ||
        BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE="$BRORAY_TX_CONTROL_OWNER_STATE"
    broray_tx_recovery_pid="$BRORAY_TX_CONTROL_OWNER_PID"
    case "$broray_tx_recovery_mode" in install|update|reinstall|opkg-upgrade|restore) ;; *) return 2 ;; esac
    broray_tx_recovery_path="$BRORAY_TX_TMP_BASE/broray-update-$broray_tx_recovery_id"
    if [ ! -e "$broray_tx_recovery_path" ] && [ ! -L "$broray_tx_recovery_path" ]; then
        broray_tx_recover_incomplete_control_prelude
        return $?
    fi
    [ -d "$broray_tx_recovery_path" ] && [ ! -L "$broray_tx_recovery_path" ] || return 2
    broray_tx_recovery_no_second_live "$broray_tx_recovery_path" || return 2
    if [ ! -e "$broray_tx_recovery_path/operation.json" ] &&
       [ ! -L "$broray_tx_recovery_path/operation.json" ]; then
        broray_tx_recover_incomplete_control_prelude
        return $?
    fi
    [ -f "$broray_tx_recovery_path/operation.json" ] && [ ! -L "$broray_tx_recovery_path/operation.json" ] || return 2
    broray_tx_recovery_started="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/started-at")"
    if [ ! -e "$broray_tx_recovery_path/owner-identity.tsv" ] &&
       [ ! -L "$broray_tx_recovery_path/owner-identity.tsv" ]; then
        if [ -f "$broray_tx_recovery_path/backup.tar.gz" ] &&
           [ ! -L "$broray_tx_recovery_path/backup.tar.gz" ]; then
            # Crash during owner-first workspace retirement.  Re-publish only
            # the missing owner under the kernel mutex; all destructive
            # authorization still waits for full capsule admission below.
            broray_tx_control_transition_begin || return 2
            broray_tx_recovery_workspace_owner_rc=1
            if [ -d "$broray_tx_recovery_path" ] &&
               [ ! -L "$broray_tx_recovery_path" ] &&
               [ ! -e "$broray_tx_recovery_path/owner-identity.tsv" ] &&
               [ ! -L "$broray_tx_recovery_path/owner-identity.tsv" ] &&
               [ "$(broray_tx_sha "$BRORAY_TX_LOCK_DIR/owner-identity.tsv")" = \
                    "$BRORAY_TX_RECOVERY_EXPECTED_OWNER_SHA256" ] &&
               broray_tx_control_owner_write_atomic \
                    "$broray_tx_recovery_path/owner-identity.tsv" &&
               broray_tx_control_owner_assert_self \
                    "$broray_tx_recovery_path/owner-identity.tsv"
            then
                broray_tx_recovery_workspace_owner_rc=0
            fi
            broray_tx_control_transition_end || return 2
            [ "$broray_tx_recovery_workspace_owner_rc" -eq 0 ] || return 2
        else
            broray_tx_recover_incomplete_control_prelude
            return $?
        fi
    fi
    broray_tx_recovery_operation_validate "$broray_tx_recovery_path/operation.json" \
        "$broray_tx_recovery_id" "$broray_tx_recovery_mode" "$broray_tx_recovery_source" \
        "$broray_tx_recovery_target" "$broray_tx_recovery_path" "$broray_tx_recovery_started" || return 2
    broray_tx_recovery_outcome="$(sed -n '1p' "$broray_tx_recovery_path/outcome" 2>/dev/null)"
    case "$broray_tx_recovery_outcome" in
        rollback-failed)
            BRORAY_TX_RECOVERY_CAPTURE_PREDECESSOR=1
            broray_tx_recovery_takeover_current_control || {
                BRORAY_TX_RECOVERY_CAPTURE_PREDECESSOR=0
                return 2
            }
            BRORAY_TX_RECOVERY_CAPTURE_PREDECESSOR=0
            broray_tx_recover_stale_rollback_failed
            return $? ;;
        ''|failed-before-mutation|rollback-verified|success-committed) ;;
        *) return 2 ;;
    esac
    if [ -z "$broray_tx_recovery_outcome" ] &&
       [ -f "$BRORAY_TX_LEGACY_MARKER" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER" ] &&
       [ "$(jq -r '.phase // ""' "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" = rollback-failed ]; then
        BRORAY_TX_RECOVERY_CAPTURE_PREDECESSOR=1
        broray_tx_recovery_takeover_current_control || {
            BRORAY_TX_RECOVERY_CAPTURE_PREDECESSOR=0
            return 2
        }
        BRORAY_TX_RECOVERY_CAPTURE_PREDECESSOR=0
        broray_tx_recover_stale_rollback_failed
        return $?
    fi

    broray_tx_caller_id="$BRORAY_TX_OPERATION_ID"
    broray_tx_caller_mode="$BRORAY_TX_MODE"
    BRORAY_TX_WORK="$broray_tx_recovery_path"
    BRORAY_TX_OPERATION_ID="$broray_tx_recovery_id"
    BRORAY_TX_MODE="$broray_tx_recovery_mode"
    BRORAY_TX_SOURCE_PACKAGE="$broray_tx_recovery_source"
    broray_tx_recovery_minimal_operation=false
    if jq -e 'has("sourceAppVersion") and has("sourceClass") and has("migrationId")' \
        "$BRORAY_TX_WORK/operation.json" >/dev/null 2>&1; then
        BRORAY_TX_SOURCE_APP="$(jq -er '.sourceAppVersion' "$BRORAY_TX_WORK/operation.json" 2>/dev/null)" || return 2
        BRORAY_TX_SOURCE_CLASS="$(jq -er '.sourceClass' "$BRORAY_TX_WORK/operation.json" 2>/dev/null)" || return 2
        BRORAY_TX_MIGRATION_ID="$(jq -er '.migrationId' "$BRORAY_TX_WORK/operation.json" 2>/dev/null)" || return 2
    else
        broray_tx_recovery_minimal_operation=true
        BRORAY_TX_SOURCE_APP=""
        BRORAY_TX_SOURCE_CLASS=""
        BRORAY_TX_MIGRATION_ID=""
    fi
    BRORAY_TX_PERSISTENT_PHASE=""
    if [ -e "$BRORAY_TX_LEGACY_MARKER" ] || [ -L "$BRORAY_TX_LEGACY_MARKER" ]; then
        broray_tx_persistent_marker_validate || return 2
    fi
    if [ "$broray_tx_recovery_minimal_operation" = true ]; then
        [ "$BRORAY_TX_PERSISTENT_PHASE" = control-prelude ] || return 2
        [ ! -e "$BRORAY_TX_WORK/backup.tar.gz" ] && [ ! -L "$BRORAY_TX_WORK/backup.tar.gz" ] || return 2
        [ ! -e "$BRORAY_TX_WORK/mutation.started" ] && [ ! -L "$BRORAY_TX_WORK/mutation.started" ] || return 2
        [ ! -e "$BRORAY_TX_WORK/evidence/source-selection.json" ] &&
            [ ! -L "$BRORAY_TX_WORK/evidence/source-selection.json" ] || return 2
        broray_tx_recovery_prelude_source="$BRORAY_TX_SOURCE_PACKAGE"
        broray_tx_source_admit || return 2
        [ "$BRORAY_TX_SOURCE_PACKAGE" = "$broray_tx_recovery_prelude_source" ] || return 2
        case "$BRORAY_TX_MODE:$BRORAY_TX_SOURCE_CLASS" in
            install:absent|update:bro-any-structural|reinstall:bro-any-structural|opkg-upgrade:bro-any-structural|restore:bro-any-structural) ;;
            *) return 2 ;;
        esac
    fi
    if [ -e "$BRORAY_TX_CURRENT" ] || [ -L "$BRORAY_TX_CURRENT" ]; then
        [ -f "$BRORAY_TX_CURRENT" ] && [ ! -L "$BRORAY_TX_CURRENT" ] || return 2
        jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg work "$BRORAY_TX_WORK" \
            '.operationId==$id and .workspace==$work' "$BRORAY_TX_CURRENT" >/dev/null 2>&1 || return 2
    fi

    # All feasible authorization is read-only with respect to retained
    # operation/control bytes.  The capsule is authoritative; outer capability
    # sidecars are optional diagnostics and, when present, must be a complete
    # pair that independently reactivates to the same canonical tool paths.
    broray_tx_recovery_cap_json=0
    broray_tx_recovery_cap_tsv=0
    [ ! -e "$BRORAY_TX_WORK/evidence/capabilities.json" ] &&
        [ ! -L "$BRORAY_TX_WORK/evidence/capabilities.json" ] || broray_tx_recovery_cap_json=1
    [ ! -e "$BRORAY_TX_WORK/evidence/capabilities.tsv" ] &&
        [ ! -L "$BRORAY_TX_WORK/evidence/capabilities.tsv" ] || broray_tx_recovery_cap_tsv=1
    [ "$broray_tx_recovery_cap_json" -eq "$broray_tx_recovery_cap_tsv" ] || return 2
    if [ "$broray_tx_recovery_cap_json" -eq 1 ]; then
        broray_runtime_reactivate_current_operation "$BRORAY_TX_WORK" || return 2
    fi
    BRORAY_TX_RECOVERY_FACTUAL_STATE=unavailable
    if [ -e "$BRORAY_TX_WORK/backup.tar.gz" ] || [ -L "$BRORAY_TX_WORK/backup.tar.gz" ]; then
        broray_tx_recovery_snapshot_admit_readonly || return 2
    fi

    case "$BRORAY_TX_PERSISTENT_PHASE" in
        mutation-started)
            # The volatile barrier file may be lost independently of the
            # durable phase marker (power loss between filesystem writes).
            # Authorize rollback only when the capsule's factual source
            # manifest proves that installed state differs from the captured
            # source; an exact source with a missing barrier is contradictory
            # and therefore remains fail-closed.
            if [ ! -f "$BRORAY_TX_WORK/mutation.started" ] || [ -L "$BRORAY_TX_WORK/mutation.started" ]; then
                [ "$BRORAY_TX_RECOVERY_FACTUAL_STATE" = source-changed ] || return 2
            fi
            ;;
        control-prelude|pre-snapshot|snapshot-verified|candidate-verified|'')
            if [ -e "$BRORAY_TX_WORK/mutation.started" ] || [ -L "$BRORAY_TX_WORK/mutation.started" ]; then
                [ "$BRORAY_TX_RECOVERY_FACTUAL_STATE" = source-changed ] || return 2
            fi
            ;;
        rollback-verified|success-committed) ;;
        *) return 2 ;;
    esac

    broray_tx_recovery_authorized=""
    broray_tx_terminal="$BRORAY_TX_OPERATION_ROOT/$BRORAY_TX_OPERATION_ID/terminal.json"
    if [ "$BRORAY_TX_MODE" = restore ] &&
       { [ -e "$broray_tx_terminal" ] || [ -L "$broray_tx_terminal" ]; }; then
        broray_tx_restore_success_terminal_validate "$broray_tx_terminal" || return 2
        case "$broray_tx_recovery_outcome" in ''|success-committed) ;; *) return 2 ;; esac
        case "$BRORAY_TX_PERSISTENT_PHASE" in mutation-started|success-committed) ;; *) return 2 ;; esac
        case "$BRORAY_TX_RECOVERY_FACTUAL_STATE" in source-exact|source-changed) ;; *) return 2 ;; esac
        broray_tx_recovery_authorized=restore-success-committed
    fi

    if [ "$broray_tx_recovery_authorized" = restore-success-committed ]; then
        :
    elif [ "$broray_tx_recovery_outcome" = success-committed ]; then
        [ "$BRORAY_TX_PERSISTENT_PHASE" = success-committed ] || return 2
        [ -f "$broray_tx_terminal" ] && [ ! -L "$broray_tx_terminal" ] || return 2
        jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg target "$BRORAY_TX_TARGET_PACKAGE" \
            --arg sha "$(broray_tx_sha "$BRORAY_TX_WORK/candidate.ipk")" '
          .status=="SUCCESS_COMMITTED" and .operationId==$id and .targetVersion==$target and
          .candidateSha256==$sha and .applicationPasses==4 and .registeredPasses==2 and .cleanupComplete==false
        ' "$broray_tx_terminal" >/dev/null 2>&1 || return 2
        [ "$BRORAY_TX_RECOVERY_FACTUAL_STATE" = source-changed ] || return 2
        broray_tx_recovery_authorized=success-committed
    elif [ "$broray_tx_recovery_outcome" = rollback-verified ] || [ -f "$BRORAY_TX_WORK/rollback.verified" ]; then
        [ "$broray_tx_recovery_outcome" = rollback-verified ] || [ -z "$broray_tx_recovery_outcome" ] || return 2
        [ -f "$BRORAY_TX_WORK/rollback.verified" ] && [ ! -L "$BRORAY_TX_WORK/rollback.verified" ] || return 2
        [ -z "$BRORAY_TX_PERSISTENT_PHASE" ] || [ "$BRORAY_TX_PERSISTENT_PHASE" = rollback-verified ] || return 2
        [ "$BRORAY_TX_RECOVERY_FACTUAL_STATE" = source-exact ] || return 2
        broray_tx_recovery_authorized=rollback-verified
    elif [ -f "$BRORAY_TX_WORK/mutation.started" ] ||
         [ "$BRORAY_TX_PERSISTENT_PHASE" = mutation-started ] ||
         [ "$BRORAY_TX_RECOVERY_FACTUAL_STATE" = source-changed ]; then
        [ -z "$broray_tx_recovery_outcome" ] || return 2
        [ "$BRORAY_TX_RECOVERY_FACTUAL_STATE" = source-exact ] ||
            [ "$BRORAY_TX_RECOVERY_FACTUAL_STATE" = source-changed ] || return 2
        broray_tx_recovery_authorized=mutation-incomplete
    else
        [ -z "$broray_tx_recovery_outcome" ] || [ "$broray_tx_recovery_outcome" = failed-before-mutation ] || return 2
        case "$BRORAY_TX_RECOVERY_FACTUAL_STATE" in
            unavailable|source-exact) ;;
            *) return 2 ;;
        esac
        broray_tx_recovery_authorized=pre-mutation
    fi

    broray_tx_recovery_takeover_current_control || return 2
    broray_tx_recovery_stale_native_clear || return 2
    broray_tx_trap_disable
    case "$broray_tx_recovery_authorized" in
      pre-mutation)
        if [ -f "$BRORAY_TX_WORK/services.stopped" ] && [ ! -L "$BRORAY_TX_WORK/services.stopped" ]; then
            broray_tx_native_opkg_lock_acquire || return 2
            broray_tx_service_state_restore || { broray_tx_native_opkg_lock_release 2>/dev/null || true; return 2; }
            rm -f "$BRORAY_TX_WORK/services.stopped" || return 2
        fi
        broray_tx_recovery_record pre-mutation volatile-cleanup || return 2
        broray_tx_recovery_terminal_cleanup || return 2
        ;;
      success-committed)
        broray_tx_native_opkg_lock_acquire || return 2
        BRORAY_TX_MUTATED=1
        broray_tx_snapshot_verify_core || { broray_tx_native_opkg_lock_release 2>/dev/null || true; return 2; }
        broray_tx_recovery_record success-committed success-cleanup || return 2
        broray_tx_success_cleanup || return 2
        jq '.cleanupComplete=true' "$broray_tx_terminal" >"$broray_tx_terminal.part" || return 2
        mv -f "$broray_tx_terminal.part" "$broray_tx_terminal" || return 2
        ;;
      restore-success-committed)
        broray_tx_native_opkg_lock_acquire || return 2
        BRORAY_TX_MUTATED=1
        broray_tx_snapshot_verify_core || { broray_tx_native_opkg_lock_release 2>/dev/null || true; return 2; }
        broray_tx_restore_success_terminal_validate "$broray_tx_terminal" || {
            broray_tx_native_opkg_lock_release 2>/dev/null || true; return 2;
        }
        broray_tx_recovery_record restore-success-committed success-cleanup || return 2
        broray_tx_success_cleanup || return 2
        jq '.cleanupComplete=true' "$broray_tx_terminal" >"$broray_tx_terminal.part" || return 2
        mv -f "$broray_tx_terminal.part" "$broray_tx_terminal" || return 2
        ;;
      rollback-verified)
        broray_tx_native_opkg_lock_acquire || return 2
        BRORAY_TX_MUTATED=1
        broray_tx_snapshot_verify_core || { broray_tx_native_opkg_lock_release 2>/dev/null || true; return 2; }
        broray_tx_postcheck_source && broray_tx_postcheck_source || { broray_tx_native_opkg_lock_release 2>/dev/null || true; return 2; }
        broray_tx_recovery_record rollback-verified rollback-cleanup || return 2
        broray_tx_recovery_terminal_cleanup || return 2
        ;;
      mutation-incomplete)
        broray_tx_native_opkg_lock_acquire || return 2
        BRORAY_TX_MUTATED=1
        if ! broray_tx_snapshot_verify_core || ! broray_tx_rollback; then
            broray_tx_native_opkg_lock_release 2>/dev/null || true
            printf '%s\n' rollback-failed >"$BRORAY_TX_WORK/outcome" 2>/dev/null || true
            return 2
        fi
        printf '%s\n' rollback-verified >"$BRORAY_TX_WORK/outcome" || return 2
        broray_tx_recovery_record mutation-incomplete single-snapshot-rollback || return 2
        broray_tx_recovery_terminal_cleanup || return 2
        ;;
      *) return 2 ;;
    esac
    BRORAY_TX_OPERATION_ID="$broray_tx_caller_id"
    BRORAY_TX_MODE="$broray_tx_caller_mode"
    BRORAY_TX_WORK=""
    BRORAY_TX_SOURCE_PACKAGE=""
    BRORAY_TX_SOURCE_APP=""
    BRORAY_TX_SOURCE_CLASS=""
    BRORAY_TX_MIGRATION_ID=""
    BRORAY_TX_MUTATED=0
    BRORAY_TX_LOCK_HELD=0
    BRORAY_TX_NATIVE_OPKG_LOCK_HELD=0
    BRORAY_TX_RECOVERY_ORIGINAL_OWNER_STATE=""
    return 0
}

broray_tx_lock_publish()
{
    broray_tx_lock_publish_started="$1"
    case "$broray_tx_lock_publish_started" in ????-??-??T??:??:??Z) ;; *) return 1 ;; esac
    broray_tx_control_transition_begin || return 1
    broray_tx_lock_publish_rc=1
    if broray_tx_global_control_relation_assert; then
        if [ -e "$BRORAY_TX_LOCK_DIR" ] || [ -L "$BRORAY_TX_LOCK_DIR" ]; then
            # Only recovery may classify an ownerless residue.  A normal
            # publisher must not erase a crashed terminal retirement or race
            # the re-admission of its unique snapshot.
            broray_tx_lock_publish_rc=2
        fi
        if [ "$broray_tx_lock_publish_rc" -ne 2 ] &&
           [ ! -e "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] &&
           mkdir "$BRORAY_TX_LOCK_DIR" 2>/dev/null
        then
            chmod 700 "$BRORAY_TX_LOCK_DIR" 2>/dev/null || true
            if printf '%s\n' "$BRORAY_TX_OPERATION_ID" >"$BRORAY_TX_LOCK_DIR/operation-id" &&
               printf '%s\n' "$BRORAY_TX_MODE" >"$BRORAY_TX_LOCK_DIR/operation-type" &&
               printf '%s\n' "$broray_tx_lock_publish_started" >"$BRORAY_TX_LOCK_DIR/started-at" &&
               printf '%s\n' "$BRORAY_TX_SOURCE_PACKAGE" >"$BRORAY_TX_LOCK_DIR/source-version" &&
               printf '%s\n' "$BRORAY_TX_TARGET_PACKAGE" >"$BRORAY_TX_LOCK_DIR/target-version" &&
               broray_tx_control_transition_assert &&
               broray_tx_control_owner_write_atomic "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" &&
               broray_tx_control_owner_assert_self "$BRORAY_TX_LOCK_DIR/owner-identity.tsv"
            then
                broray_tx_lock_publish_rc=0
            else
                # Before owner publication these are explicitly uncommitted
                # bytes.  Normalize them while the same kernel mutex is held.
                if [ ! -e "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ] &&
                   [ ! -L "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ]; then
                    broray_tx_transaction_ownerless_residue_cleanup 2>/dev/null || true
                fi
            fi
        fi
    fi
    broray_tx_control_transition_end || return 1
    [ "$broray_tx_lock_publish_rc" -eq 0 ] || return 1
    BRORAY_TX_LOCK_HELD=1
}

broray_tx_lock_acquire()
{
    broray_tx_recover_stale_control || return 2
    broray_tx_source_identity_prelude || return 3
    broray_tx_lock_publish "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" || return 2
}

broray_tx_lock_adopt()
{
    [ -d "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] || return 1
    [ -f "$BRORAY_TX_LOCK_DIR/operation-id" ] && [ ! -L "$BRORAY_TX_LOCK_DIR/operation-id" ] || return 1
    [ "$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-id")" = "$BRORAY_TX_OPERATION_ID" ] || return 1
    [ -f "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ] && [ ! -L "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ] || return 1
    [ -n "$BRORAY_TX_WORK" ] && [ -d "$BRORAY_TX_WORK" ] && [ ! -L "$BRORAY_TX_WORK" ] || return 1
    [ -f "$BRORAY_TX_WORK/owner-identity.tsv" ] && [ ! -L "$BRORAY_TX_WORK/owner-identity.tsv" ] || return 1
    broray_tx_control_transition_assert || return 1
    broray_tx_control_owner_classify "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" || return 1
    broray_tx_adopt_lock_state="$BRORAY_TX_CONTROL_OWNER_STATE"
    broray_tx_adopt_lock_pid="$BRORAY_TX_CONTROL_OWNER_PID"
    case "$broray_tx_adopt_lock_state" in live|dead|reused) ;; *) return 1 ;; esac
    broray_tx_control_owner_classify "$BRORAY_TX_WORK/owner-identity.tsv" || return 1
    broray_tx_adopt_work_state="$BRORAY_TX_CONTROL_OWNER_STATE"
    broray_tx_adopt_work_pid="$BRORAY_TX_CONTROL_OWNER_PID"
    case "$broray_tx_adopt_work_state" in live|dead|reused) ;; *) return 1 ;; esac
    if ! broray_tx_files_equal "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" \
        "$BRORAY_TX_WORK/owner-identity.tsv"; then
        # Only a self/stale split can be the residue of the immediately prior
        # workspace-first transition.  Two equal foreign-live records are the
        # normal inherited-FD handoff; a mismatched foreign-live record is not.
        case "$broray_tx_adopt_lock_state:$broray_tx_adopt_lock_pid" in
            dead:*|reused:*|live:"$$") ;;
            *) return 1 ;;
        esac
        case "$broray_tx_adopt_work_state:$broray_tx_adopt_work_pid" in
            dead:*|reused:*|live:"$$") ;;
            *) return 1 ;;
        esac
    fi
    if [ "$broray_tx_adopt_work_state" != live ] || [ "$broray_tx_adopt_work_pid" != "$$" ]; then
        broray_tx_control_owner_write_atomic "$BRORAY_TX_WORK/owner-identity.tsv" || return 1
    fi
    broray_tx_control_owner_assert_self "$BRORAY_TX_WORK/owner-identity.tsv" || return 1
    broray_tx_test_pause owner-transition-workspace-published "$BRORAY_TX_WORK/evidence" || return 1
    if ! broray_tx_control_owner_assert_self "$BRORAY_TX_LOCK_DIR/owner-identity.tsv"; then
        broray_tx_control_owner_write_atomic "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" || return 1
    fi
    broray_tx_control_owner_assert_self "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" || return 1
    broray_tx_test_pause owner-transition-lock-published "$BRORAY_TX_WORK/evidence" || return 1
    BRORAY_TX_LOCK_HELD=1
}

broray_tx_persistent_marker_validate_canonical()
{
    [ -f "$BRORAY_TX_LEGACY_MARKER" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER" ] || return 1
    [ "$(wc -l <"$BRORAY_TX_LEGACY_MARKER" | tr -d ' ')" = 1 ] || return 1
    broray_tx_marker_started="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/started-at" 2>/dev/null)"
    [ -n "$broray_tx_marker_started" ] || return 1
    broray_tx_marker_actual="$(sed -n '1p' "$BRORAY_TX_LEGACY_MARKER")"
    broray_tx_marker_archive_snapshot=null
    if [ -f "$BRORAY_TX_WORK/snapshot.verified" ] && [ ! -L "$BRORAY_TX_WORK/snapshot.verified" ]; then
        broray_tx_marker_archive_snapshot="$(sed -n '1p' "$BRORAY_TX_WORK/snapshot.verified")"
    elif [ -f "$BRORAY_TX_WORK/backup.tar.gz" ] && [ ! -L "$BRORAY_TX_WORK/backup.tar.gz" ]; then
        broray_tx_marker_archive_snapshot="$(broray_tx_sha "$BRORAY_TX_WORK/backup.tar.gz")"
    fi
    case "$broray_tx_marker_archive_snapshot" in
        null) ;;
        ''|*[!0-9a-f]*) return 1 ;;
        *) [ "$(printf %s "$broray_tx_marker_archive_snapshot" | wc -c | tr -d ' ')" -eq 64 ] || return 1 ;;
    esac
    for broray_tx_marker_candidate in control-prelude pre-snapshot snapshot-verified candidate-verified mutation-started rollback-verified rollback-failed success-committed; do
        case "$broray_tx_marker_candidate" in
            control-prelude|pre-snapshot)
                broray_tx_marker_candidate_snapshot=null
                broray_tx_marker_candidate_json=null
                broray_tx_marker_candidate_status=active
                ;;
            snapshot-verified|candidate-verified|mutation-started)
                [ "$broray_tx_marker_archive_snapshot" != null ] || continue
                broray_tx_marker_candidate_snapshot="$broray_tx_marker_archive_snapshot"
                broray_tx_marker_candidate_json="\"$broray_tx_marker_archive_snapshot\""
                broray_tx_marker_candidate_status=active
                ;;
            rollback-verified|rollback-failed|success-committed)
                [ "$broray_tx_marker_archive_snapshot" != null ] || continue
                broray_tx_marker_candidate_snapshot="$broray_tx_marker_archive_snapshot"
                broray_tx_marker_candidate_json="\"$broray_tx_marker_archive_snapshot\""
                broray_tx_marker_candidate_status="$broray_tx_marker_candidate"
                ;;
        esac
        if [ "$broray_tx_marker_candidate" = control-prelude ]; then
            broray_tx_marker_expected="$(printf '{"schemaVersion":2,"lifecycleContract":"%s","operationId":"%s","mode":"%s","sourceVersion":"%s","targetVersion":"%s","transactionPath":"%s","phase":"control-prelude","status":"active","startedAt":"%s","snapshotSha256":null}' \
                "$BRORAY_TX_CONTRACT" "$BRORAY_TX_OPERATION_ID" "$BRORAY_TX_MODE" \
                "$BRORAY_TX_SOURCE_PACKAGE" "$BRORAY_TX_TARGET_PACKAGE" "$BRORAY_TX_WORK" "$broray_tx_marker_started")"
        else
            broray_tx_marker_expected="$(printf '{"lifecycleContract":"%s","mode":"%s","operationId":"%s","phase":"%s","schemaVersion":2,"snapshotSha256":%s,"sourceVersion":"%s","startedAt":"%s","status":"%s","targetVersion":"%s","transactionPath":"%s"}' \
                "$BRORAY_TX_CONTRACT" "$BRORAY_TX_MODE" "$BRORAY_TX_OPERATION_ID" "$broray_tx_marker_candidate" \
                "$broray_tx_marker_candidate_json" "$BRORAY_TX_SOURCE_PACKAGE" "$broray_tx_marker_started" \
                "$broray_tx_marker_candidate_status" "$BRORAY_TX_TARGET_PACKAGE" "$BRORAY_TX_WORK")"
        fi
        if [ "$broray_tx_marker_actual" = "$broray_tx_marker_expected" ]; then
            BRORAY_TX_PERSISTENT_PHASE="$broray_tx_marker_candidate"
            BRORAY_TX_PERSISTENT_SNAPSHOT="$broray_tx_marker_candidate_snapshot"
            return 0
        fi
    done
    return 1
}

broray_tx_persistent_marker_validate()
{
    broray_tx_persistent_marker_validate_canonical
    return $?
    # Kept unreachable as a format migration reference: runtime ownership is
    # intentionally authorized only by the deterministic byte form above.
    [ -f "$BRORAY_TX_LEGACY_MARKER" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER" ] || return 1
    [ "$(wc -l <"$BRORAY_TX_LEGACY_MARKER" | tr -d ' ')" = 1 ] || return 1
    broray_tx_marker_started="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/started-at" 2>/dev/null)"
    [ -n "$broray_tx_marker_started" ] || return 1
    if ! command -v jq >/dev/null 2>&1; then
        broray_tx_marker_initial="$(printf '{"schemaVersion":2,"lifecycleContract":"%s","operationId":"%s","mode":"%s","sourceVersion":"%s","targetVersion":"%s","transactionPath":"%s","phase":"control-prelude","status":"active","startedAt":"%s","snapshotSha256":null}' \
            "$BRORAY_TX_CONTRACT" "$BRORAY_TX_OPERATION_ID" "$BRORAY_TX_MODE" \
            "$BRORAY_TX_SOURCE_PACKAGE" "$BRORAY_TX_TARGET_PACKAGE" "$BRORAY_TX_WORK" "$broray_tx_marker_started")"
        [ "$(sed -n '1p' "$BRORAY_TX_LEGACY_MARKER")" = "$broray_tx_marker_initial" ] || return 1
        BRORAY_TX_PERSISTENT_PHASE=control-prelude
        BRORAY_TX_PERSISTENT_SNAPSHOT=null
        return 0
    fi
    broray_tx_marker_phase="$(jq -er '.phase' "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 1
    case "$broray_tx_marker_phase" in
        control-prelude|pre-snapshot|snapshot-verified|candidate-verified|mutation-started|rollback-verified|rollback-failed|success-committed) ;;
        *) return 1 ;;
    esac
    case "$broray_tx_marker_phase" in
        control-prelude|pre-snapshot)
            broray_tx_marker_status=active
            broray_tx_marker_snapshot=null
            ;;
        snapshot-verified|candidate-verified|mutation-started)
            broray_tx_marker_status=active
            broray_tx_marker_snapshot="$(jq -er '.snapshotSha256 | select(type=="string" and length==64 and all(explode[]; ((.>=48) and (.<=57)) or ((.>=97) and (.<=102))))' \
                "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 1
            ;;
        rollback-verified)
            broray_tx_marker_status=rollback-verified
            broray_tx_marker_snapshot="$(jq -er '.snapshotSha256 | select(type=="string" and length==64 and all(explode[]; ((.>=48) and (.<=57)) or ((.>=97) and (.<=102))))' \
                "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 1
            ;;
        rollback-failed)
            broray_tx_marker_status=rollback-failed
            broray_tx_marker_snapshot="$(jq -er '.snapshotSha256 | select(type=="string" and length==64 and all(explode[]; ((.>=48) and (.<=57)) or ((.>=97) and (.<=102))))' \
                "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 1
            ;;
        success-committed)
            broray_tx_marker_status=success-committed
            broray_tx_marker_snapshot="$(jq -er '.snapshotSha256 | select(type=="string" and length==64 and all(explode[]; ((.>=48) and (.<=57)) or ((.>=97) and (.<=102))))' \
                "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null)" || return 1
            ;;
    esac
    jq -e --arg lifecycle "$BRORAY_TX_CONTRACT" --arg id "$BRORAY_TX_OPERATION_ID" \
        --arg mode "$BRORAY_TX_MODE" --arg source "$BRORAY_TX_SOURCE_PACKAGE" \
        --arg target "$BRORAY_TX_TARGET_PACKAGE" --arg work "$BRORAY_TX_WORK" \
        --arg started "$broray_tx_marker_started" --arg phase "$broray_tx_marker_phase" \
        --arg status "$broray_tx_marker_status" --arg snapshot "$broray_tx_marker_snapshot" '
      (keys|sort)==(["lifecycleContract","mode","operationId","phase","schemaVersion","snapshotSha256","sourceVersion","startedAt","status","targetVersion","transactionPath"]|sort) and
      .schemaVersion==2 and .lifecycleContract==$lifecycle and .operationId==$id and
      .mode==$mode and .sourceVersion==$source and .targetVersion==$target and
      .transactionPath==$work and .startedAt==$started and .phase==$phase and .status==$status and
      (if $snapshot=="null" then .snapshotSha256==null else .snapshotSha256==$snapshot end)
    ' "$BRORAY_TX_LEGACY_MARKER" >/dev/null 2>&1 || return 1
    case "$broray_tx_marker_phase" in
        control-prelude|pre-snapshot) ;;
        *)
            if [ -f "$BRORAY_TX_WORK/snapshot.verified" ] && [ ! -L "$BRORAY_TX_WORK/snapshot.verified" ]; then
                [ "$(sed -n '1p' "$BRORAY_TX_WORK/snapshot.verified")" = "$broray_tx_marker_snapshot" ] || return 1
            elif [ -f "$BRORAY_TX_WORK/backup.tar.gz" ] && [ ! -L "$BRORAY_TX_WORK/backup.tar.gz" ]; then
                [ "$(broray_tx_sha "$BRORAY_TX_WORK/backup.tar.gz")" = "$broray_tx_marker_snapshot" ] || return 1
            else
                return 1
            fi
            ;;
    esac
    BRORAY_TX_PERSISTENT_PHASE="$broray_tx_marker_phase"
    BRORAY_TX_PERSISTENT_SNAPSHOT="$broray_tx_marker_snapshot"
    return 0
}

broray_tx_persistent_marker_phase_update()
{
    broray_tx_marker_next="$1"
    broray_tx_persistent_marker_validate || return 1
    broray_tx_marker_previous="$BRORAY_TX_PERSISTENT_PHASE"
    if [ "$broray_tx_marker_previous" != "$broray_tx_marker_next" ]; then
        case "$broray_tx_marker_previous:$broray_tx_marker_next" in
            control-prelude:pre-snapshot|pre-snapshot:snapshot-verified|\
            snapshot-verified:candidate-verified|candidate-verified:mutation-started|\
            mutation-started:rollback-verified|mutation-started:rollback-failed|mutation-started:success-committed|\
            candidate-verified:rollback-failed) ;;
            *) return 1 ;;
        esac
    fi
    case "$broray_tx_marker_next" in
        control-prelude|pre-snapshot)
            broray_tx_marker_next_status=active
            broray_tx_marker_next_snapshot=null
            ;;
        snapshot-verified|candidate-verified|mutation-started|rollback-verified|rollback-failed|success-committed)
            broray_tx_marker_next_snapshot="$(sed -n '1p' "$BRORAY_TX_WORK/snapshot.verified" 2>/dev/null)"
            if [ -z "$broray_tx_marker_next_snapshot" ] && [ "$broray_tx_marker_next" = rollback-failed ] &&
               [ -f "$BRORAY_TX_WORK/backup.tar.gz" ] && [ ! -L "$BRORAY_TX_WORK/backup.tar.gz" ]; then
                broray_tx_marker_next_snapshot="$(broray_tx_sha "$BRORAY_TX_WORK/backup.tar.gz")"
            fi
            case "$broray_tx_marker_next_snapshot" in *[!0-9a-f]*|'') return 1 ;; esac
            [ "${#broray_tx_marker_next_snapshot}" -eq 64 ] || return 1
            [ -f "$BRORAY_TX_WORK/backup.tar.gz" ] && [ ! -L "$BRORAY_TX_WORK/backup.tar.gz" ] || return 1
            [ "$(broray_tx_sha "$BRORAY_TX_WORK/backup.tar.gz")" = "$broray_tx_marker_next_snapshot" ] || return 1
            case "$broray_tx_marker_next" in
                rollback-verified) broray_tx_marker_next_status=rollback-verified ;;
                rollback-failed) broray_tx_marker_next_status=rollback-failed ;;
                success-committed) broray_tx_marker_next_status=success-committed ;;
                *) broray_tx_marker_next_status=active ;;
            esac
            ;;
        *) return 1 ;;
    esac
    broray_tx_marker_part="$BRORAY_TX_LEGACY_MARKER.part.$$"
    jq -ncS --arg lifecycle "$BRORAY_TX_CONTRACT" --arg id "$BRORAY_TX_OPERATION_ID" \
        --arg mode "$BRORAY_TX_MODE" --arg source "$BRORAY_TX_SOURCE_PACKAGE" \
        --arg target "$BRORAY_TX_TARGET_PACKAGE" --arg work "$BRORAY_TX_WORK" \
        --arg started "$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/started-at")" \
        --arg phase "$broray_tx_marker_next" --arg status "$broray_tx_marker_next_status" \
        --arg snapshot "$broray_tx_marker_next_snapshot" '
      {schemaVersion:2,lifecycleContract:$lifecycle,operationId:$id,mode:$mode,
       sourceVersion:$source,targetVersion:$target,transactionPath:$work,phase:$phase,
       status:$status,startedAt:$started,
       snapshotSha256:(if $snapshot=="null" then null else $snapshot end)}
    ' >"$broray_tx_marker_part" || { rm -f "$broray_tx_marker_part"; return 1; }
    [ -f "$broray_tx_marker_part" ] && [ ! -L "$broray_tx_marker_part" ] || { rm -f "$broray_tx_marker_part"; return 1; }
    chmod 600 "$broray_tx_marker_part" 2>/dev/null || true
    mv -f "$broray_tx_marker_part" "$BRORAY_TX_LEGACY_MARKER" || { rm -f "$broray_tx_marker_part"; return 1; }
    broray_tx_persistent_marker_validate
}

broray_tx_persistent_marker_phase_reach()
{
    if [ -e "$BRORAY_TX_LEGACY_MARKER" ] || [ -L "$BRORAY_TX_LEGACY_MARKER" ]; then
        broray_tx_persistent_marker_phase_update "$1"
        return $?
    fi
    # Isolated arithmetic/format fixtures intentionally exercise individual
    # production functions without owning global transaction control.  Full
    # lifecycle fixtures always have the marker and cannot take this branch.
    [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ]
}

broray_tx_persistent_marker_remove()
{
    if [ -e "$BRORAY_TX_LEGACY_MARKER" ] || [ -L "$BRORAY_TX_LEGACY_MARKER" ]; then
        broray_tx_persistent_marker_validate || return 1
        rm -f "$BRORAY_TX_LEGACY_MARKER" || return 1
    fi
}

broray_tx_lock_release()
{
    [ "$BRORAY_TX_LOCK_HELD" -eq 1 ] || return 0
    broray_tx_control_transition_begin || return 1
    broray_tx_lock_release_owns_mutex="$BRORAY_TX_CONTROL_TRANSITION_OWNS_MUTEX"
    broray_tx_lock_release_rc=1
    broray_tx_lock_release_shape_ok=1
    if [ -d "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] &&
       [ -f "$BRORAY_TX_LOCK_DIR/operation-id" ] && [ ! -L "$BRORAY_TX_LOCK_DIR/operation-id" ] &&
       [ "$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-id")" = "$BRORAY_TX_OPERATION_ID" ] &&
       broray_tx_control_owner_assert_self "$BRORAY_TX_LOCK_DIR/owner-identity.tsv"
    then
        for broray_tx_lock_entry in "$BRORAY_TX_LOCK_DIR"/* "$BRORAY_TX_LOCK_DIR"/.[!.]* "$BRORAY_TX_LOCK_DIR"/..?*; do
            [ -e "$broray_tx_lock_entry" ] || [ -L "$broray_tx_lock_entry" ] || continue
            case "${broray_tx_lock_entry##*/}" in
                operation-id|owner-identity.tsv|operation-type|started-at|source-version|target-version)
                    [ -f "$broray_tx_lock_entry" ] && [ ! -L "$broray_tx_lock_entry" ] ||
                        broray_tx_lock_release_shape_ok=0 ;;
                *) broray_tx_lock_release_shape_ok=0 ;;
            esac
        done
        if [ "$broray_tx_lock_release_shape_ok" -eq 1 ]; then
            if [ -e "$BRORAY_TX_LEGACY_MARKER" ] || [ -L "$BRORAY_TX_LEGACY_MARKER" ]; then
                broray_tx_persistent_marker_validate || broray_tx_lock_release_shape_ok=0
            fi
        fi
        if [ "$broray_tx_lock_release_shape_ok" -eq 1 ] &&
           broray_tx_control_transition_assert &&
           rm -f "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" &&
           broray_tx_persistent_marker_remove
        then
            broray_tx_lock_release_rc=0
            for broray_tx_lock_file in operation-id operation-type started-at source-version target-version; do
                rm -f "$BRORAY_TX_LOCK_DIR/$broray_tx_lock_file" || broray_tx_lock_release_rc=1
            done
            if [ "$broray_tx_lock_release_rc" -eq 0 ]; then
                rmdir "$BRORAY_TX_LOCK_DIR" || broray_tx_lock_release_rc=1
            fi
        fi
    fi
    broray_tx_control_transition_end || return 1
    if [ "$broray_tx_lock_release_owns_mutex" -eq 0 ] &&
       [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ]; then
        broray_tx_native_opkg_lock_release || return 1
    fi
    [ "$broray_tx_lock_release_rc" -eq 0 ] || return 1
    BRORAY_TX_LOCK_HELD=0
}

# Terminal cleanup retires the transaction lock and workspace as one control
# transition.  Canonical owner records disappear first.  The workspace is
# then atomically moved out of the operation namespace while the kernel fence
# is still held; if it contains the long native-lock FIFO, the in-memory lock
# root follows that rename and EOF remains the final release action.
broray_tx_terminal_control_retire()
{
    [ "$BRORAY_TX_LOCK_HELD" -eq 1 ] || return 1
    case "$BRORAY_TX_WORK" in "$BRORAY_TX_TMP_BASE"/broray-update-*) ;; *) return 1 ;; esac
    [ -d "$BRORAY_TX_WORK" ] && [ ! -L "$BRORAY_TX_WORK" ] || return 1
    [ -d "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] || return 1
    [ -f "$BRORAY_TX_LOCK_DIR/operation-id" ] &&
        [ ! -L "$BRORAY_TX_LOCK_DIR/operation-id" ] &&
        [ "$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-id")" = "$BRORAY_TX_OPERATION_ID" ] || return 1
    broray_tx_control_owner_assert_self "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" || return 1
    broray_tx_control_owner_assert_self "$BRORAY_TX_WORK/owner-identity.tsv" || return 1
    if [ -e "$BRORAY_TX_CURRENT" ] || [ -L "$BRORAY_TX_CURRENT" ]; then
        [ -f "$BRORAY_TX_CURRENT" ] && [ ! -L "$BRORAY_TX_CURRENT" ] &&
            jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg work "$BRORAY_TX_WORK" \
                '.operationId==$id and .workspace==$work' "$BRORAY_TX_CURRENT" \
                >/dev/null 2>&1 || return 1
    fi
    if [ -e "$BRORAY_TX_LEGACY_MARKER" ] || [ -L "$BRORAY_TX_LEGACY_MARKER" ]; then
        broray_tx_persistent_marker_validate || return 1
    fi
    broray_tx_terminal_retire_owner_pid="$BRORAY_TX_CONTROL_OWNER_PID"
    broray_tx_terminal_retire_owner_start="$BRORAY_TX_CONTROL_OWNER_STARTTIME"
    case "$broray_tx_terminal_retire_owner_pid:$broray_tx_terminal_retire_owner_start" in ''|*[!0-9:]*) return 1 ;; esac
    broray_tx_terminal_retire_work="$BRORAY_TX_TMP_BASE/.broray-retired-$BRORAY_TX_OPERATION_ID-$broray_tx_terminal_retire_owner_pid-$broray_tx_terminal_retire_owner_start"
    broray_tx_terminal_retire_lock="$BRORAY_TX_TMP_BASE/.broray-retired-lock-$BRORAY_TX_OPERATION_ID-$broray_tx_terminal_retire_owner_pid-$broray_tx_terminal_retire_owner_start"
    broray_tx_terminal_retire_record="$BRORAY_TX_TMP_BASE/.broray-retired-record-$BRORAY_TX_OPERATION_ID-$broray_tx_terminal_retire_owner_pid-$broray_tx_terminal_retire_owner_start.tsv"
    [ ! -e "$broray_tx_terminal_retire_work" ] && [ ! -L "$broray_tx_terminal_retire_work" ] || return 1
    [ ! -e "$broray_tx_terminal_retire_lock" ] && [ ! -L "$broray_tx_terminal_retire_lock" ] || return 1
    broray_tx_terminal_retire_had_long_lock=0
    if [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ]; then
        [ "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK" = "$BRORAY_TX_WORK" ] || return 1
        broray_tx_terminal_retire_had_long_lock=1
    fi

    broray_tx_control_transition_begin || return 1
    broray_tx_terminal_retire_rc=0
    broray_tx_control_owner_assert_self "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" &&
        broray_tx_control_owner_assert_self "$BRORAY_TX_WORK/owner-identity.tsv" ||
        broray_tx_terminal_retire_rc=1
    broray_tx_terminal_retire_record_part="$BRORAY_TX_TMP_BASE/.broray-terminal-retire-record-part-$BRORAY_TX_OPERATION_ID-$broray_tx_terminal_retire_owner_pid-$broray_tx_terminal_retire_owner_start.tsv"
    if [ "$broray_tx_terminal_retire_rc" -eq 0 ]; then
        [ ! -e "$broray_tx_terminal_retire_record_part" ] && [ ! -L "$broray_tx_terminal_retire_record_part" ] ||
            broray_tx_terminal_retire_rc=1
    fi
    if [ "$broray_tx_terminal_retire_rc" -eq 0 ]; then
        printf 'contract\tterminal-retire/1\noperation-id\t%s\ncanonical-work\t%s\nretired-work\t%s\nretired-lock\t%s\nowner-pid\t%s\nowner-starttime\t%s\n' \
            "$BRORAY_TX_OPERATION_ID" "$BRORAY_TX_WORK" "$broray_tx_terminal_retire_work" "$broray_tx_terminal_retire_lock" \
            "$broray_tx_terminal_retire_owner_pid" "$broray_tx_terminal_retire_owner_start" \
            >"$broray_tx_terminal_retire_record_part" || broray_tx_terminal_retire_rc=1
    fi
    if [ "$broray_tx_terminal_retire_rc" -eq 0 ]; then
        chmod 600 "$broray_tx_terminal_retire_record_part" 2>/dev/null || true
        if [ -e "$broray_tx_terminal_retire_record" ] || [ -L "$broray_tx_terminal_retire_record" ]; then
            broray_tx_files_equal "$broray_tx_terminal_retire_record_part" "$broray_tx_terminal_retire_record" &&
                rm -f "$broray_tx_terminal_retire_record_part" || broray_tx_terminal_retire_rc=1
        else
            mv -f "$broray_tx_terminal_retire_record_part" "$broray_tx_terminal_retire_record" ||
                broray_tx_terminal_retire_rc=1
        fi
    fi
    if [ "$broray_tx_terminal_retire_rc" -eq 0 ]; then
        # The transaction collection becomes ownerless first; recovery of a
        # crash at any following boundary re-publishes under this same mutex.
        rm -f "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" || broray_tx_terminal_retire_rc=1
        [ "$broray_tx_terminal_retire_rc" -ne 0 ] ||
            broray_tx_test_pause terminal-retire-transaction-owner-removed \
                "$BRORAY_TX_WORK/evidence" || broray_tx_terminal_retire_rc=1
    fi
    if [ "$broray_tx_terminal_retire_rc" -eq 0 ]; then
        mv "$BRORAY_TX_LOCK_DIR" "$broray_tx_terminal_retire_lock" || broray_tx_terminal_retire_rc=1
    fi
    if [ "$broray_tx_terminal_retire_rc" -eq 0 ]; then
        rm -f "$BRORAY_TX_CURRENT" "$BRORAY_TX_LEGACY_MARKER" "$BRORAY_TX_LEGACY_MARKER.part" ||
            broray_tx_terminal_retire_rc=1
    fi
    if [ "$broray_tx_terminal_retire_rc" -eq 0 ]; then
        # Workspace owner is its first retired atom.  The following rename is
        # on /tmp and atomically removes the canonical operation path.
        rm -f "$BRORAY_TX_WORK/owner-identity.tsv" || broray_tx_terminal_retire_rc=1
    fi
    if [ "$broray_tx_terminal_retire_rc" -eq 0 ]; then
        mv "$BRORAY_TX_WORK" "$broray_tx_terminal_retire_work" || broray_tx_terminal_retire_rc=1
        if [ "$broray_tx_terminal_retire_rc" -eq 0 ] && [ "$broray_tx_terminal_retire_had_long_lock" -eq 1 ]; then
            BRORAY_TX_NATIVE_OPKG_LOCK_WORK="$broray_tx_terminal_retire_work"
        fi
        if [ "$broray_tx_terminal_retire_rc" -eq 0 ]; then
            broray_tx_test_pause terminal-retire-workspace-renamed \
                "$broray_tx_terminal_retire_work/evidence" || broray_tx_terminal_retire_rc=1
        fi
    fi
    broray_tx_control_transition_end || return 1
    [ "$broray_tx_terminal_retire_rc" -eq 0 ] || return 1
    if [ "$broray_tx_terminal_retire_had_long_lock" -eq 1 ]; then
        broray_tx_native_opkg_lock_release || return 1
    fi
    rm -rf "$broray_tx_terminal_retire_work" || return 1
    rm -rf "$broray_tx_terminal_retire_lock" || return 1
    broray_tx_terminal_cleanup_complete "$BRORAY_TX_OPERATION_ID" || return 1
    rm -f "$broray_tx_terminal_retire_record" || return 1
    BRORAY_TX_LOCK_HELD=0
}

broray_tx_opkg_lock_config_read()
{
    BRORAY_TX_NATIVE_OPKG_LOCK_CONFIGURED=""
    BRORAY_TX_NATIVE_OPKG_CONFIG_ARG=0
    if [ ! -e "$BRORAY_TX_OPKG_CONF" ] && [ ! -L "$BRORAY_TX_OPKG_CONF" ]; then
        return 0
    fi
    [ -f "$BRORAY_TX_OPKG_CONF" ] && [ ! -L "$BRORAY_TX_OPKG_CONF" ] || return 1
    BRORAY_TX_NATIVE_OPKG_CONFIG_ARG=1
    broray_tx_opkg_config_lock_rows="$(awk '
      /^[[:space:]]*(#|$)/ { next }
      {
        if ($1=="option" && $2=="lock_file") {
          if (NF!=3) bad=1
          else { rows++; print $3 }
        } else if ($0 ~ /(^|[[:space:]])lock_file([[:space:]]|$)/) bad=1
      }
      END { if (bad || rows>1) exit 1 }
    ' "$BRORAY_TX_OPKG_CONF" 2>/dev/null)" || return 1
    if [ -n "$broray_tx_opkg_config_lock_rows" ]; then
        case "$broray_tx_opkg_config_lock_rows" in
            /*) ;;
            *) return 1 ;;
        esac
        case "$broray_tx_opkg_config_lock_rows" in *[!0-9A-Za-z._/+:-]*) return 1 ;; esac
        BRORAY_TX_NATIVE_OPKG_LOCK_CONFIGURED="$broray_tx_opkg_config_lock_rows"
    fi
}

broray_tx_native_opkg_lock_record_absent()
{
    broray_tx_opkg_absent_pid="$1"
    broray_tx_opkg_absent_devino="$2"
    broray_tx_opkg_absent_type="$3"
    case "$broray_tx_opkg_absent_pid:$broray_tx_opkg_absent_devino" in
        ''|*' '*) return 1 ;;
    esac
    case "$broray_tx_opkg_absent_type" in POSIX|FLOCK) ;; *) return 1 ;; esac
    [ -r "$BRORAY_TX_PROC_ROOT/locks" ] || return 1
    broray_tx_opkg_absent_count="$(awk -v pid="$broray_tx_opkg_absent_pid" \
      -v devino="$broray_tx_opkg_absent_devino" -v lock_type="$broray_tx_opkg_absent_type" '
      $2==lock_type && $3=="ADVISORY" && $4=="WRITE" &&
      $5==pid && $6==devino && $7=="0" && $8=="EOF" { count++ }
      END { print count+0 }
    ' "$BRORAY_TX_PROC_ROOT/locks" 2>/dev/null)" || return 1
    [ "$broray_tx_opkg_absent_count" = 0 ]
}

# A PID alone is never an OPKG owner identity.  Bind the child to the Linux
# process birth token (/proc/PID/stat starttime in production), the canonical executable and
# the exact NUL-delimited argv which this transaction launched.  Production
# accepts only the native OPKG executable.  The isolated LAB shim may be an
# exact #!/bin/sh wrapper; that exception cannot be enabled against root=/.
broray_tx_native_opkg_owner_starttime()
{
    broray_tx_proc_starttime "$BRORAY_TX_PROC_ROOT" "$1"
}

broray_tx_native_opkg_owner_expected_cmdline()
{
    broray_tx_opkg_owner_expected_output="$1"
    broray_tx_opkg_owner_expected_profile="$2"
    : >"$broray_tx_opkg_owner_expected_output" || return 1
    case "$broray_tx_opkg_owner_expected_profile" in
        native-opkg-binary) ;;
        isolated-lab-shell-wrapper)
            printf '%s\000' /bin/sh >>"$broray_tx_opkg_owner_expected_output" || return 1
            ;;
        *) return 1 ;;
    esac
    printf '%s\000' "$BRORAY_TX_OPKG" >>"$broray_tx_opkg_owner_expected_output" || return 1
    if [ "$BRORAY_TX_NATIVE_OPKG_CONFIG_ARG" -eq 1 ]; then
        printf '%s\000%s\000' -f "$BRORAY_TX_OPKG_CONF" >>"$broray_tx_opkg_owner_expected_output" || return 1
    fi
    case "$BRORAY_TX_NATIVE_OPKG_LOCK_FIFO_ARGV" in
        "$BRORAY_TX_TMP_BASE"/*/native-opkg-lock/hold.ipk) ;;
        *) return 1 ;;
    esac
    printf '%s\000%s\000' install "$BRORAY_TX_NATIVE_OPKG_LOCK_FIFO_ARGV" \
        >>"$broray_tx_opkg_owner_expected_output" || return 1
}

broray_tx_native_opkg_owner_opkg_canonical()
{
    case "$BRORAY_TX_OPKG" in
        /*) broray_tx_opkg_owner_resolved="$BRORAY_TX_OPKG" ;;
        *) broray_tx_opkg_owner_resolved="$(broray_runtime_command_path "$BRORAY_TX_OPKG")" || return 1 ;;
    esac
    [ -x "$broray_tx_opkg_owner_resolved" ] && [ ! -d "$broray_tx_opkg_owner_resolved" ] || return 1
    readlink -f "$broray_tx_opkg_owner_resolved" 2>/dev/null
}

broray_tx_native_opkg_owner_identity_capture()
{
    broray_tx_opkg_owner_identity_pid="$1"
    broray_tx_opkg_owner_identity_dir="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock"
    broray_tx_opkg_owner_identity_start_1="$(broray_tx_native_opkg_owner_starttime "$broray_tx_opkg_owner_identity_pid")" || return 1
    broray_tx_opkg_owner_identity_exe="$(readlink -f "$BRORAY_TX_PROC_ROOT/$broray_tx_opkg_owner_identity_pid/exe" 2>/dev/null)" || return 1
    broray_tx_opkg_owner_identity_opkg="$(broray_tx_native_opkg_owner_opkg_canonical)" || return 1
    [ -n "$broray_tx_opkg_owner_identity_exe" ] && [ -n "$broray_tx_opkg_owner_identity_opkg" ] || return 1
    if [ "$broray_tx_opkg_owner_identity_exe" = "$broray_tx_opkg_owner_identity_opkg" ]; then
        broray_tx_opkg_owner_identity_profile=native-opkg-binary
    elif [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ] &&
         [ "$(sed -n '1p' "$BRORAY_TX_OPKG" 2>/dev/null)" = '#!/bin/sh' ] &&
         [ "$broray_tx_opkg_owner_identity_exe" = "$(readlink -f /bin/sh 2>/dev/null)" ]; then
        broray_tx_opkg_owner_identity_profile=isolated-lab-shell-wrapper
    else
        return 1
    fi
    [ -r "$BRORAY_TX_PROC_ROOT/$broray_tx_opkg_owner_identity_pid/cmdline" ] || return 1
    broray_tx_native_opkg_owner_expected_cmdline \
        "$broray_tx_opkg_owner_identity_dir/owner-cmdline.expected" \
        "$broray_tx_opkg_owner_identity_profile" || return 1
    broray_tx_opkg_owner_identity_actual_sha="$(broray_tx_sha "$BRORAY_TX_PROC_ROOT/$broray_tx_opkg_owner_identity_pid/cmdline")" || return 1
    broray_tx_opkg_owner_identity_expected_sha="$(broray_tx_sha "$broray_tx_opkg_owner_identity_dir/owner-cmdline.expected")" || return 1
    [ "$broray_tx_opkg_owner_identity_actual_sha" = "$broray_tx_opkg_owner_identity_expected_sha" ] || return 1
    [ "$(wc -c <"$BRORAY_TX_PROC_ROOT/$broray_tx_opkg_owner_identity_pid/cmdline" | tr -d ' ')" = \
      "$(wc -c <"$broray_tx_opkg_owner_identity_dir/owner-cmdline.expected" | tr -d ' ')" ] || return 1
    broray_tx_opkg_owner_identity_start_2="$(broray_tx_native_opkg_owner_starttime "$broray_tx_opkg_owner_identity_pid")" || return 1
    [ "$broray_tx_opkg_owner_identity_start_1" = "$broray_tx_opkg_owner_identity_start_2" ] || return 1
    [ "$(readlink -f "$BRORAY_TX_PROC_ROOT/$broray_tx_opkg_owner_identity_pid/exe" 2>/dev/null)" = "$broray_tx_opkg_owner_identity_exe" ] || return 1
    printf '%s\n' "$broray_tx_opkg_owner_identity_start_1" >"$broray_tx_opkg_owner_identity_dir/owner-starttime" || return 1
    printf '%s\n' "$broray_tx_opkg_owner_identity_actual_sha" >"$broray_tx_opkg_owner_identity_dir/owner-cmdline-sha256" || return 1
    printf '%s\n' "$broray_tx_opkg_owner_identity_exe" >"$broray_tx_opkg_owner_identity_dir/owner-exe-canonical" || return 1
    printf '%s\n' "$broray_tx_opkg_owner_identity_opkg" >"$broray_tx_opkg_owner_identity_dir/opkg-canonical" || return 1
    printf '%s\n' "$broray_tx_opkg_owner_identity_profile" >"$broray_tx_opkg_owner_identity_dir/owner-profile" || return 1
    printf '%s\n' "$BRORAY_TX_OPKG" >"$broray_tx_opkg_owner_identity_dir/owner-opkg-argv0" || return 1
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_STARTTIME="$broray_tx_opkg_owner_identity_start_1"
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_CMDLINE_SHA256="$broray_tx_opkg_owner_identity_actual_sha"
}

broray_tx_native_opkg_owner_identity_assert()
{
    broray_tx_opkg_owner_assert_dir="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock"
    broray_tx_opkg_owner_assert_pid="$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID"
    for broray_tx_opkg_owner_assert_file in owner-starttime owner-cmdline-sha256 owner-exe-canonical opkg-canonical owner-profile owner-opkg-argv0; do
        [ -f "$broray_tx_opkg_owner_assert_dir/$broray_tx_opkg_owner_assert_file" ] &&
            [ ! -L "$broray_tx_opkg_owner_assert_dir/$broray_tx_opkg_owner_assert_file" ] || return 1
    done
    broray_tx_opkg_owner_assert_start="$(sed -n '1p' "$broray_tx_opkg_owner_assert_dir/owner-starttime")"
    broray_tx_opkg_owner_assert_cmdline_sha="$(sed -n '1p' "$broray_tx_opkg_owner_assert_dir/owner-cmdline-sha256")"
    broray_tx_opkg_owner_assert_exe="$(sed -n '1p' "$broray_tx_opkg_owner_assert_dir/owner-exe-canonical")"
    broray_tx_opkg_owner_assert_opkg="$(sed -n '1p' "$broray_tx_opkg_owner_assert_dir/opkg-canonical")"
    broray_tx_opkg_owner_assert_profile="$(sed -n '1p' "$broray_tx_opkg_owner_assert_dir/owner-profile")"
    [ "$(sed -n '1p' "$broray_tx_opkg_owner_assert_dir/owner-opkg-argv0")" = "$BRORAY_TX_OPKG" ] || return 1
    case "$broray_tx_opkg_owner_assert_start:$broray_tx_opkg_owner_assert_cmdline_sha" in
        *[!0-9a-f:]*) return 1 ;;
    esac
    [ "${#broray_tx_opkg_owner_assert_cmdline_sha}" -eq 64 ] || return 1
    [ "$(broray_tx_native_opkg_owner_starttime "$broray_tx_opkg_owner_assert_pid")" = "$broray_tx_opkg_owner_assert_start" ] || return 1
    [ "$(readlink -f "$BRORAY_TX_PROC_ROOT/$broray_tx_opkg_owner_assert_pid/exe" 2>/dev/null)" = "$broray_tx_opkg_owner_assert_exe" ] || return 1
    [ "$(broray_tx_native_opkg_owner_opkg_canonical)" = "$broray_tx_opkg_owner_assert_opkg" ] || return 1
    broray_tx_native_opkg_owner_expected_cmdline "$broray_tx_opkg_owner_assert_dir/owner-cmdline.recheck.$$" \
        "$broray_tx_opkg_owner_assert_profile" || return 1
    broray_tx_opkg_owner_assert_expected_sha="$(broray_tx_sha "$broray_tx_opkg_owner_assert_dir/owner-cmdline.recheck.$$")" || return 1
    rm -f "$broray_tx_opkg_owner_assert_dir/owner-cmdline.recheck.$$" || return 1
    [ "$broray_tx_opkg_owner_assert_expected_sha" = "$broray_tx_opkg_owner_assert_cmdline_sha" ] || return 1
    [ "$(broray_tx_sha "$BRORAY_TX_PROC_ROOT/$broray_tx_opkg_owner_assert_pid/cmdline")" = "$broray_tx_opkg_owner_assert_cmdline_sha" ] || return 1
    [ "$(broray_tx_native_opkg_owner_starttime "$broray_tx_opkg_owner_assert_pid")" = "$broray_tx_opkg_owner_assert_start" ] || return 1
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_STARTTIME="$broray_tx_opkg_owner_assert_start"
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_CMDLINE_SHA256="$broray_tx_opkg_owner_assert_cmdline_sha"
}

# Prove the native OPKG write lock directly from the holder's kernel state.
# No competing OPKG command is used: maintained OPKG variants intentionally
# allow concurrent read-only commands, so contention on print-architecture is
# not a portable lock proof.  fdinfo binds a lock record to an exact holder FD;
# /proc/locks independently confirms the same PID/device/inode/range tuple.
broray_tx_native_opkg_lock_proc_snapshot()
{
    broray_tx_opkg_proc_tag="$1"
    case "$broray_tx_opkg_proc_tag" in ''|*[!0-9A-Za-z._-]*) return 1 ;; esac
    broray_tx_native_opkg_owner_identity_assert || return 1
    broray_tx_opkg_proc_pid="$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID"
    broray_tx_opkg_proc_pid_root="$BRORAY_TX_PROC_ROOT/$broray_tx_opkg_proc_pid"
    broray_tx_opkg_proc_dir="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock"
    broray_tx_opkg_proc_fifo="$broray_tx_opkg_proc_dir/hold.ipk"
    [ -p "$broray_tx_opkg_proc_fifo" ] && [ ! -L "$broray_tx_opkg_proc_fifo" ] || return 1
    broray_tx_opkg_proc_fifo_canonical="$(readlink -f "$broray_tx_opkg_proc_fifo" 2>/dev/null)" || return 1
    [ -d "$broray_tx_opkg_proc_pid_root/fd" ] && [ ! -L "$broray_tx_opkg_proc_pid_root/fd" ] || return 1
    [ -d "$broray_tx_opkg_proc_pid_root/fdinfo" ] && [ ! -L "$broray_tx_opkg_proc_pid_root/fdinfo" ] || return 1
    [ -r "$BRORAY_TX_PROC_ROOT/locks" ] || return 1

    broray_tx_opkg_proc_matches="$broray_tx_opkg_proc_dir/proc-lock-$broray_tx_opkg_proc_tag.matches.$$"
    : >"$broray_tx_opkg_proc_matches" || return 1
    broray_tx_opkg_proc_fifo_fds=0
    for broray_tx_opkg_proc_fd_link in "$broray_tx_opkg_proc_pid_root"/fd/[0-9]*; do
        [ -L "$broray_tx_opkg_proc_fd_link" ] || continue
        broray_tx_opkg_proc_fd="${broray_tx_opkg_proc_fd_link##*/}"
        case "$broray_tx_opkg_proc_fd" in ''|*[!0-9]*) rm -f "$broray_tx_opkg_proc_matches"; return 1 ;; esac
        broray_tx_opkg_proc_target="$(readlink -f "$broray_tx_opkg_proc_fd_link" 2>/dev/null)" || continue
        if [ "$broray_tx_opkg_proc_target" = "$broray_tx_opkg_proc_fifo_canonical" ]; then
            broray_tx_opkg_proc_fifo_fds=$((broray_tx_opkg_proc_fifo_fds + 1))
            continue
        fi
        broray_tx_opkg_proc_fdinfo="$broray_tx_opkg_proc_pid_root/fdinfo/$broray_tx_opkg_proc_fd"
        [ -r "$broray_tx_opkg_proc_fdinfo" ] || continue
        broray_tx_opkg_proc_mnt_id="$(awk '
          $1=="mnt_id:" && $2~/^[0-9]+$/ { count++; value=$2 }
          END { if (count==1) print value; else exit 1 }
        ' "$broray_tx_opkg_proc_fdinfo" 2>/dev/null)" || continue
        broray_tx_opkg_proc_lock_tuple="$(awk -v pid="$broray_tx_opkg_proc_pid" '
          $1=="lock:" && $2~/^[0-9]+:$/ && ($3=="POSIX" || $3=="FLOCK") &&
          $4=="ADVISORY" && $5=="WRITE" && $6==pid &&
          $7~/^[0-9A-Fa-f]+:[0-9A-Fa-f]+:[0-9]+$/ && $8=="0" && $9=="EOF" {
            count++; value=$3 "\t" $7
          }
          END { if (count==1) print value; else exit 1 }
        ' "$broray_tx_opkg_proc_fdinfo" 2>/dev/null)" || continue
        broray_tx_opkg_proc_lock_type="${broray_tx_opkg_proc_lock_tuple%%	*}"
        broray_tx_opkg_proc_devino="${broray_tx_opkg_proc_lock_tuple#*	}"
        case "$broray_tx_opkg_proc_lock_type" in POSIX|FLOCK) ;; *) continue ;; esac
        # Keenetic's procfs exposes the exact per-FD `lock:` tuple but does not
        # guarantee a separate `ino:` row in fdinfo.  The tuple is authoritative
        # for the locked inode; independently bind it to this FD's canonical
        # regular-file target with the already-required findutils `-printf`.
        broray_tx_opkg_proc_inode="${broray_tx_opkg_proc_devino##*:}"
        case "$broray_tx_opkg_proc_mnt_id:$broray_tx_opkg_proc_inode" in
            *[!0-9:]*) continue ;;
        esac
        broray_tx_opkg_proc_target_meta="$(find -P "$broray_tx_opkg_proc_target" \
            -maxdepth 0 -printf '%y|%i' 2>/dev/null)" || continue
        case "$broray_tx_opkg_proc_target_meta" in
            f\|[0-9]*) ;;
            *) continue ;;
        esac
        broray_tx_opkg_proc_target_inode="${broray_tx_opkg_proc_target_meta#*|}"
        case "$broray_tx_opkg_proc_target_inode" in
            ''|*[!0-9]*) continue ;;
        esac
        [ "$broray_tx_opkg_proc_target_inode" = "$broray_tx_opkg_proc_inode" ] || continue
        [ "${broray_tx_opkg_proc_devino##*:}" = "$broray_tx_opkg_proc_inode" ] || continue
        broray_tx_opkg_proc_kernel_count="$(awk -v pid="$broray_tx_opkg_proc_pid" \
          -v devino="$broray_tx_opkg_proc_devino" -v lock_type="$broray_tx_opkg_proc_lock_type" '
          $2==lock_type && $3=="ADVISORY" && $4=="WRITE" &&
          $5==pid && $6==devino && $7=="0" && $8=="EOF" { count++ }
          END { print count+0 }
        ' "$BRORAY_TX_PROC_ROOT/locks" 2>/dev/null)" || {
            rm -f "$broray_tx_opkg_proc_matches"; return 1;
        }
        [ "$broray_tx_opkg_proc_kernel_count" = 1 ] || continue

        if [ -n "$BRORAY_TX_PREFIX" ]; then
            case "$broray_tx_opkg_proc_target" in
                "$BRORAY_TX_PREFIX"/*) broray_tx_opkg_proc_logical="${broray_tx_opkg_proc_target#$BRORAY_TX_PREFIX}" ;;
                *) continue ;;
            esac
        else
            broray_tx_opkg_proc_logical="$broray_tx_opkg_proc_target"
        fi
        case "$broray_tx_opkg_proc_logical" in
            /*) ;;
            *) continue ;;
        esac
        case "$broray_tx_opkg_proc_logical" in *[!0-9A-Za-z._/+:-]*) continue ;; esac
        if [ -n "$BRORAY_TX_NATIVE_OPKG_LOCK_CONFIGURED" ] &&
           [ "$broray_tx_opkg_proc_logical" != "$BRORAY_TX_NATIVE_OPKG_LOCK_CONFIGURED" ]; then
            continue
        fi
        printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
            "$broray_tx_opkg_proc_fd" "$broray_tx_opkg_proc_mnt_id" \
            "$broray_tx_opkg_proc_inode" "$broray_tx_opkg_proc_devino" \
            "$broray_tx_opkg_proc_lock_type" "$broray_tx_opkg_proc_logical" \
            "$broray_tx_opkg_proc_target" \
            >>"$broray_tx_opkg_proc_matches" || {
                rm -f "$broray_tx_opkg_proc_matches"; return 1;
            }
    done
    [ "$broray_tx_opkg_proc_fifo_fds" -eq 1 ] &&
    [ "$(wc -l <"$broray_tx_opkg_proc_matches" | tr -d ' ')" -eq 1 ] || {
        rm -f "$broray_tx_opkg_proc_matches"; return 1;
    }
    IFS="$(printf '\t')" read -r BRORAY_TX_NATIVE_OPKG_LOCK_FD \
        BRORAY_TX_NATIVE_OPKG_LOCK_MNT_ID BRORAY_TX_NATIVE_OPKG_LOCK_INODE \
        BRORAY_TX_NATIVE_OPKG_LOCK_DEVINO BRORAY_TX_NATIVE_OPKG_LOCK_TYPE \
        BRORAY_TX_NATIVE_OPKG_LOCK_PATH BRORAY_TX_NATIVE_OPKG_LOCK_TARGET \
        <"$broray_tx_opkg_proc_matches" || {
            rm -f "$broray_tx_opkg_proc_matches"; return 1;
        }
    case "$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE" in POSIX|FLOCK) ;; *) return 1 ;; esac
    rm -f "$broray_tx_opkg_proc_matches" || return 1

    if [ -e "$broray_tx_opkg_proc_dir/lock-devino" ] || [ -L "$broray_tx_opkg_proc_dir/lock-devino" ]; then
        for broray_tx_opkg_proc_saved in lock-fd lock-mnt-id lock-inode lock-devino lock-type lock-path lock-target; do
            [ -f "$broray_tx_opkg_proc_dir/$broray_tx_opkg_proc_saved" ] &&
                [ ! -L "$broray_tx_opkg_proc_dir/$broray_tx_opkg_proc_saved" ] || return 1
        done
        [ "$(sed -n '1p' "$broray_tx_opkg_proc_dir/lock-fd")" = "$BRORAY_TX_NATIVE_OPKG_LOCK_FD" ] || return 1
        [ "$(sed -n '1p' "$broray_tx_opkg_proc_dir/lock-mnt-id")" = "$BRORAY_TX_NATIVE_OPKG_LOCK_MNT_ID" ] || return 1
        [ "$(sed -n '1p' "$broray_tx_opkg_proc_dir/lock-inode")" = "$BRORAY_TX_NATIVE_OPKG_LOCK_INODE" ] || return 1
        [ "$(sed -n '1p' "$broray_tx_opkg_proc_dir/lock-devino")" = "$BRORAY_TX_NATIVE_OPKG_LOCK_DEVINO" ] || return 1
        [ "$(sed -n '1p' "$broray_tx_opkg_proc_dir/lock-type")" = "$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE" ] || return 1
        [ "$(sed -n '1p' "$broray_tx_opkg_proc_dir/lock-path")" = "$BRORAY_TX_NATIVE_OPKG_LOCK_PATH" ] || return 1
        [ "$(sed -n '1p' "$broray_tx_opkg_proc_dir/lock-target")" = "$BRORAY_TX_NATIVE_OPKG_LOCK_TARGET" ] || return 1
    else
        for broray_tx_opkg_proc_new in \
            "lock-fd:$BRORAY_TX_NATIVE_OPKG_LOCK_FD" \
            "lock-mnt-id:$BRORAY_TX_NATIVE_OPKG_LOCK_MNT_ID" \
            "lock-inode:$BRORAY_TX_NATIVE_OPKG_LOCK_INODE" \
            "lock-devino:$BRORAY_TX_NATIVE_OPKG_LOCK_DEVINO" \
            "lock-type:$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE" \
            "lock-path:$BRORAY_TX_NATIVE_OPKG_LOCK_PATH" \
            "lock-target:$BRORAY_TX_NATIVE_OPKG_LOCK_TARGET"
        do
            broray_tx_opkg_proc_new_name="${broray_tx_opkg_proc_new%%:*}"
            broray_tx_opkg_proc_new_value="${broray_tx_opkg_proc_new#*:}"
            [ ! -e "$broray_tx_opkg_proc_dir/$broray_tx_opkg_proc_new_name" ] &&
                [ ! -L "$broray_tx_opkg_proc_dir/$broray_tx_opkg_proc_new_name" ] || return 1
            printf '%s\n' "$broray_tx_opkg_proc_new_value" \
                >"$broray_tx_opkg_proc_dir/$broray_tx_opkg_proc_new_name" || return 1
        done
    fi
    printf 'tag\townerPid\townerStarttime\tfd\tmntId\tinode\tdevInode\tlockType\tlockPath\townerFdTarget\tfifoFdCount\tfifoPayloadBytesWritten\n%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t1\t0\n' \
        "$broray_tx_opkg_proc_tag" "$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID" \
        "$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_STARTTIME" "$BRORAY_TX_NATIVE_OPKG_LOCK_FD" \
        "$BRORAY_TX_NATIVE_OPKG_LOCK_MNT_ID" "$BRORAY_TX_NATIVE_OPKG_LOCK_INODE" \
        "$BRORAY_TX_NATIVE_OPKG_LOCK_DEVINO" "$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE" \
        "$BRORAY_TX_NATIVE_OPKG_LOCK_PATH" \
        "$BRORAY_TX_NATIVE_OPKG_LOCK_TARGET" \
        >"$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/evidence/opkg-lock-$broray_tx_opkg_proc_tag.proc.tsv" || return 1
    broray_tx_native_opkg_owner_identity_assert
}

# Abort only an owner launched by the current acquire attempt.  Closing fd 9
# ends the FIFO stream and lets OPKG release its own advisory lock; no PID is
# signalled.  A bounded birth-token poll plus disappearance of the exact
# PID/device/inode record proves that this holder no longer owns the lock.
broray_tx_native_opkg_lock_acquire_abort()
{
    broray_tx_opkg_lock_abort_work="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK"
    exec 9>&- || return 1
    broray_tx_opkg_lock_abort_pid="$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID"
    broray_tx_opkg_lock_abort_start="$(sed -n '1p' "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock/owner-starttime" 2>/dev/null)"
    broray_tx_opkg_lock_abort_devino="$(sed -n '1p' "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock/lock-devino" 2>/dev/null)"
    broray_tx_opkg_lock_abort_type="$(sed -n '1p' "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock/lock-type" 2>/dev/null)"
    broray_tx_opkg_lock_abort_wait=0
    while :; do
        if [ -n "$broray_tx_opkg_lock_abort_start" ]; then
            [ "$(broray_tx_native_opkg_owner_starttime "$broray_tx_opkg_lock_abort_pid" 2>/dev/null)" = \
                "$broray_tx_opkg_lock_abort_start" ] || break
        else
            kill -0 "$broray_tx_opkg_lock_abort_pid" 2>/dev/null || break
        fi
        broray_tx_opkg_lock_abort_wait=$((broray_tx_opkg_lock_abort_wait + 1))
        [ "$broray_tx_opkg_lock_abort_wait" -lt 10 ] || return 1
        sleep 1
    done
    if wait "$broray_tx_opkg_lock_abort_pid" 2>/dev/null; then
        broray_tx_opkg_lock_abort_rc=0
    else
        broray_tx_opkg_lock_abort_rc=$?
    fi
    printf '%s\n' "$broray_tx_opkg_lock_abort_rc" \
        >"$broray_tx_opkg_lock_abort_work/native-opkg-lock/owner-exit-code" 2>/dev/null || true
    if [ -n "$broray_tx_opkg_lock_abort_devino" ] &&
       [ -n "$broray_tx_opkg_lock_abort_type" ]; then
        broray_tx_native_opkg_lock_record_absent \
            "$broray_tx_opkg_lock_abort_pid" "$broray_tx_opkg_lock_abort_devino" \
            "$broray_tx_opkg_lock_abort_type" || return 1
    fi
    printf '%s\n' acquire-aborted >"$broray_tx_opkg_lock_abort_work/native-opkg-lock/state" 2>/dev/null || true
    BRORAY_TX_NATIVE_OPKG_LOCK_HELD=0
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID=""
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_STARTTIME=""
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_CMDLINE_SHA256=""
    BRORAY_TX_NATIVE_OPKG_LOCK_TYPE=""
    BRORAY_TX_NATIVE_OPKG_LOCK_FIFO_ARGV=""
    BRORAY_TX_NATIVE_OPKG_LOCK_WORK=""
}

broray_tx_native_opkg_lock_acquire_fail()
{
    broray_tx_opkg_lock_fail_work="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK"
    case "$BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON" in
        ''|*[!0-9A-Za-z._:-]*) BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=unclassified-native-lock-failure ;;
    esac
    if [ -d "$broray_tx_opkg_lock_fail_work/native-opkg-lock" ] &&
       [ ! -L "$broray_tx_opkg_lock_fail_work/native-opkg-lock" ]; then
        printf '%s\n' "$BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON" \
            >"$broray_tx_opkg_lock_fail_work/native-opkg-lock/failure-reason" 2>/dev/null || true
        cp -p "$BRORAY_TX_PROC_ROOT/locks" \
            "$broray_tx_opkg_lock_fail_work/native-opkg-lock/proc-locks-after" 2>/dev/null || true
    fi
    broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true
    return 1
}

broray_tx_native_opkg_lock_acquire()
{
    BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=precondition-failed
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 0 ] || return 1
    if [ -z "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK" ]; then
        BRORAY_TX_NATIVE_OPKG_LOCK_WORK="$BRORAY_TX_WORK"
    fi
    [ -d "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK" ] &&
        [ ! -L "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK" ] &&
        [ -d "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/evidence" ] &&
        [ ! -L "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/evidence" ] || return 1
    BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=opkg-config-invalid
    broray_tx_opkg_lock_config_read || return 1
    broray_tx_opkg_lock_dir="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock"
    [ ! -e "$broray_tx_opkg_lock_dir" ] && [ ! -L "$broray_tx_opkg_lock_dir" ] || return 1
    mkdir "$broray_tx_opkg_lock_dir" || return 1
    chmod 700 "$broray_tx_opkg_lock_dir" 2>/dev/null || true
    printf '%s\n' lock-directory-created >"$broray_tx_opkg_lock_dir/acquire-stage" || return 1
    broray_tx_opkg_lock_fifo="$broray_tx_opkg_lock_dir/hold.ipk"
    mkfifo "$broray_tx_opkg_lock_fifo" || return 1
    [ -p "$broray_tx_opkg_lock_fifo" ] && [ ! -L "$broray_tx_opkg_lock_fifo" ] || return 1
    exec 9<>"$broray_tx_opkg_lock_fifo" || return 1
    BRORAY_TX_NATIVE_OPKG_LOCK_FIFO_ARGV="$broray_tx_opkg_lock_fifo"
    : >"$broray_tx_opkg_lock_dir/owner.stdout" || return 1
    : >"$broray_tx_opkg_lock_dir/owner.stderr" || return 1
    cp -p "$BRORAY_TX_PROC_ROOT/locks" "$broray_tx_opkg_lock_dir/proc-locks-before" 2>/dev/null || true
    BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=opkg-owner-launch-failed
    if [ "$BRORAY_TX_NATIVE_OPKG_CONFIG_ARG" -eq 1 ]; then
        "$BRORAY_TX_OPKG" -f "$BRORAY_TX_OPKG_CONF" install "$broray_tx_opkg_lock_fifo" \
            >"$broray_tx_opkg_lock_dir/owner.stdout" \
            2>"$broray_tx_opkg_lock_dir/owner.stderr" 9>&- &
    else
        "$BRORAY_TX_OPKG" install "$broray_tx_opkg_lock_fifo" \
            >"$broray_tx_opkg_lock_dir/owner.stdout" \
            2>"$broray_tx_opkg_lock_dir/owner.stderr" 9>&- &
    fi
    broray_tx_opkg_lock_owner=$!
    case "$broray_tx_opkg_lock_owner" in ''|*[!0-9]*|0) exec 9>&-; return 1 ;; esac
    printf '%s\n' "$broray_tx_opkg_lock_owner" >"$broray_tx_opkg_lock_dir/owner-pid" || return 1
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID="$broray_tx_opkg_lock_owner"
    BRORAY_TX_NATIVE_OPKG_LOCK_HELD=1
    printf '%s\n' owner-launched >"$broray_tx_opkg_lock_dir/acquire-stage" || {
        broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
    }
    printf '%s\n' "$BRORAY_TX_NATIVE_OPKG_CONFIG_ARG" >"$broray_tx_opkg_lock_dir/config-arg" || {
        broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
    }
    if [ "$BRORAY_TX_NATIVE_OPKG_CONFIG_ARG" -eq 1 ]; then
        printf '%s\n' "$BRORAY_TX_OPKG_CONF" >"$broray_tx_opkg_lock_dir/config-path" || {
            broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
        }
        broray_tx_opkg_lock_conf_sha="$(broray_tx_sha "$BRORAY_TX_OPKG_CONF")" || {
            broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
        }
    else
        : >"$broray_tx_opkg_lock_dir/config-path" || {
            broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
        }
        broray_tx_opkg_lock_conf_sha=absent
    fi
    printf '%s\n' "$broray_tx_opkg_lock_conf_sha" >"$broray_tx_opkg_lock_dir/config-sha256" || {
        broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
    }
    printf '%s\n' "$BRORAY_TX_NATIVE_OPKG_LOCK_CONFIGURED" >"$broray_tx_opkg_lock_dir/configured-lock-path" || {
        broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
    }

    broray_tx_opkg_lock_attempt=0
    broray_tx_opkg_lock_attempt_limit=10
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ]; then
        case "${BRORAY_TX_TEST_OPKG_LOCK_ATTEMPTS:-}" in
            [1-9]|10) broray_tx_opkg_lock_attempt_limit="$BRORAY_TX_TEST_OPKG_LOCK_ATTEMPTS" ;;
        esac
    fi
    broray_tx_opkg_lock_path=""
    BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=native-lock-proof-not-observed
    printf '%s\n' waiting-for-native-lock-proof >"$broray_tx_opkg_lock_dir/acquire-stage" || {
        broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
    }
    while [ "$broray_tx_opkg_lock_attempt" -lt "$broray_tx_opkg_lock_attempt_limit" ]; do
        if broray_tx_native_opkg_owner_identity_capture "$broray_tx_opkg_lock_owner" 2>/dev/null &&
           broray_tx_native_opkg_lock_proc_snapshot acquire-1 2>/dev/null; then
            broray_tx_opkg_lock_path="$BRORAY_TX_NATIVE_OPKG_LOCK_PATH"
            break
        fi
        if ! kill -0 "$broray_tx_opkg_lock_owner" 2>/dev/null; then
            BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=opkg-owner-exited-before-native-lock-proof
            break
        fi
        broray_tx_opkg_lock_attempt=$((broray_tx_opkg_lock_attempt + 1))
        sleep 1
    done
    [ -n "$broray_tx_opkg_lock_path" ] || {
        broray_tx_native_opkg_lock_acquire_fail
        return 1
    }
    BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=second-native-lock-proof-failed
    broray_tx_native_opkg_lock_proc_snapshot acquire-2 || {
        broray_tx_native_opkg_lock_acquire_fail; return 1;
    }
    BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=native-lock-path-drift
    [ "$broray_tx_opkg_lock_path" = "$BRORAY_TX_NATIVE_OPKG_LOCK_PATH" ] || {
        broray_tx_native_opkg_lock_acquire_fail; return 1;
    }
    BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=opkg-owner-identity-drift
    broray_tx_native_opkg_owner_identity_assert || {
        broray_tx_native_opkg_lock_acquire_fail; return 1;
    }
    printf '%s\n' held >"$broray_tx_opkg_lock_dir/state" || {
        broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
    }
    printf '%s\n' native-lock-proven >"$broray_tx_opkg_lock_dir/acquire-stage" || {
        broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
    }
    BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=evidence-json-failed
    jq -nc --arg contract "$BRORAY_RUNTIME_CAPABILITY_CONTRACT" --arg operationId "$BRORAY_TX_OPERATION_ID" \
        --arg path "$BRORAY_TX_NATIVE_OPKG_LOCK_PATH" --argjson ownerPid "$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID" \
        --arg config "$([ "$BRORAY_TX_NATIVE_OPKG_CONFIG_ARG" -eq 1 ] && printf '%s' "$BRORAY_TX_OPKG_CONF" || printf '')" \
        --arg configured "$BRORAY_TX_NATIVE_OPKG_LOCK_CONFIGURED" \
        --arg ownerStarttime "$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_STARTTIME" \
        --arg ownerCmdlineSha256 "$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_CMDLINE_SHA256" \
        --arg ownerExeCanonical "$(sed -n '1p' "$broray_tx_opkg_lock_dir/owner-exe-canonical")" \
        --arg ownerProfile "$(sed -n '1p' "$broray_tx_opkg_lock_dir/owner-profile")" \
        --arg opkgArgv0 "$BRORAY_TX_OPKG" --arg fifo "$broray_tx_opkg_lock_fifo" \
        --arg lockFd "$BRORAY_TX_NATIVE_OPKG_LOCK_FD" \
        --arg lockMntId "$BRORAY_TX_NATIVE_OPKG_LOCK_MNT_ID" \
        --arg lockInode "$BRORAY_TX_NATIVE_OPKG_LOCK_INODE" \
        --arg lockDevInode "$BRORAY_TX_NATIVE_OPKG_LOCK_DEVINO" \
        --arg lockType "$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE" \
        --arg ownerFdTarget "$BRORAY_TX_NATIVE_OPKG_LOCK_TARGET" \
        --arg proof1Sha256 "$(broray_tx_sha "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/evidence/opkg-lock-acquire-1.proc.tsv")" \
        --arg proof2Sha256 "$(broray_tx_sha "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/evidence/opkg-lock-acquire-2.proc.tsv")" '
      {schemaVersion:1,contract:$contract,contractIds:["REQ-UPD-025",$contract],
       operationId:$operationId,candidateSha256:null,
       status:"HELD",mechanism:"native-opkg-owner-fd-advisory-write-lock-while-local-ipk-fifo-awaits-eof",
       lockPath:$path,ownerPid:$ownerPid,configPath:(if $config=="" then null else $config end),
       configuredLockPath:(if $configured=="" then null else $configured end),
       ownerStarttimeTicks:$ownerStarttime,ownerCmdlineSha256:$ownerCmdlineSha256,
       ownerExeCanonical:$ownerExeCanonical,ownerProfile:$ownerProfile,
       ownerArgv:((if $ownerProfile=="isolated-lab-shell-wrapper" then ["/bin/sh"] else [] end) +
         [$opkgArgv0] + (if $config=="" then [] else ["-f",$config] end) + ["install",$fifo]),
       fifoPackageSuffix:".ipk",fifoOpenedByOwner:true,fifoFdCount:1,
       fifoPayloadBytesWritten:0,
       pathDiscovery:"owner-fd-fdinfo-lock-tuple-proc-locks-and-find-printf",
       fdinfoInodeRowRequired:false,
       lockInodeSource:"fdinfo-lock-tuple-cross-checked-with-find-printf",
       lockFd:($lockFd|tonumber),lockMntId:($lockMntId|tonumber),
       lockInode:($lockInode|tonumber),lockDevInode:$lockDevInode,
       ownerFdTarget:$ownerFdTarget,lockType:$lockType,lockMode:"ADVISORY",
       lockAccess:"WRITE",lockRange:{start:0,end:"EOF"},procLockProofs:2,
       proof1Sha256:$proof1Sha256,proof2Sha256:$proof2Sha256,
       mutationStarted:false}
    ' >"$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/evidence/opkg-native-lock.json" || {
        broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || true; return 1;
    }
    BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON=""
    printf '%s\n' held >"$broray_tx_opkg_lock_dir/acquire-stage" || return 1
}

broray_tx_native_opkg_lock_adopt()
{
    BRORAY_TX_NATIVE_OPKG_LOCK_WORK="$BRORAY_TX_WORK"
    BRORAY_TX_NATIVE_OPKG_LOCK_FIFO_ARGV="$BRORAY_TX_WORK/native-opkg-lock/hold.ipk"
    broray_tx_opkg_lock_dir="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock"
    [ -d "$broray_tx_opkg_lock_dir" ] && [ ! -L "$broray_tx_opkg_lock_dir" ] || return 1
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID="$(sed -n '1p' "$broray_tx_opkg_lock_dir/owner-pid" 2>/dev/null)"
    BRORAY_TX_NATIVE_OPKG_LOCK_PATH="$(sed -n '1p' "$broray_tx_opkg_lock_dir/lock-path" 2>/dev/null)"
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_STARTTIME="$(sed -n '1p' "$broray_tx_opkg_lock_dir/owner-starttime" 2>/dev/null)"
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_CMDLINE_SHA256="$(sed -n '1p' "$broray_tx_opkg_lock_dir/owner-cmdline-sha256" 2>/dev/null)"
    BRORAY_TX_NATIVE_OPKG_LOCK_TYPE="$(sed -n '1p' "$broray_tx_opkg_lock_dir/lock-type" 2>/dev/null)"
    BRORAY_TX_NATIVE_OPKG_CONFIG_ARG="$(sed -n '1p' "$broray_tx_opkg_lock_dir/config-arg" 2>/dev/null)"
    case "$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID" in ''|*[!0-9]*|0) return 1 ;; esac
    case "$BRORAY_TX_NATIVE_OPKG_LOCK_PATH" in /*) ;; *) return 1 ;; esac
    case "$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE" in POSIX|FLOCK) ;; *) return 1 ;; esac
    case "$BRORAY_TX_NATIVE_OPKG_CONFIG_ARG" in 0|1) ;; *) return 1 ;; esac
    case "$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_STARTTIME" in ''|*[!0-9]*|0) return 1 ;; esac
    case "$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_CMDLINE_SHA256" in *[!0-9a-f]*) return 1 ;; esac
    [ "${#BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_CMDLINE_SHA256}" -eq 64 ] || return 1
    [ "$(sed -n '1p' "$broray_tx_opkg_lock_dir/state" 2>/dev/null)" = held ] || return 1
    BRORAY_TX_NATIVE_OPKG_LOCK_HELD=1
    broray_tx_native_opkg_lock_assert adopt
}

# The OPKG child proof alone is insufficient for handoff: the adopting shell
# must still own the inherited FIFO writer whose EOF controls that exact child.
# Bind fd 9 twice to the canonical operation FIFO and to one immutable shell
# birth token.  Production and hermetic tests both use the kernel's real procfs
# for the current shell FD, even when the OPKG child itself is modelled by a
# proc fixture.
broray_tx_native_opkg_caller_fd_assert()
{
    broray_tx_caller_fd_dir="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock"
    broray_tx_caller_fd_fifo="$broray_tx_caller_fd_dir/hold.ipk"
    [ -p "$broray_tx_caller_fd_fifo" ] && [ ! -L "$broray_tx_caller_fd_fifo" ] || return 1
    broray_tx_caller_fd_fifo_canonical="$(readlink -f "$broray_tx_caller_fd_fifo" 2>/dev/null)" || return 1
    # $$ may name the container PID while /proc is host-mounted.  Resolve
    # /proc/self in this shell (redirection + read are builtins, so no helper
    # process can change the meaning of self), then bind that visible PID,
    # starttime and fd target twice around the proof.
    IFS=' ' read -r broray_tx_caller_proc_pid_1 broray_tx_caller_proc_rest_1 </proc/self/stat || return 1
    case "$broray_tx_caller_proc_pid_1" in ''|*[!0-9]*|0) return 1 ;; esac
    [ -L "/proc/$broray_tx_caller_proc_pid_1/fd/9" ] &&
        [ -r "/proc/$broray_tx_caller_proc_pid_1/fdinfo/9" ] || return 1
    broray_tx_caller_fd_start_1="$(broray_tx_proc_starttime /proc "$broray_tx_caller_proc_pid_1")" || return 1
    broray_tx_caller_fd_target_1="$(readlink -f "/proc/$broray_tx_caller_proc_pid_1/fd/9" 2>/dev/null)" || return 1
    IFS=' ' read -r broray_tx_caller_proc_pid_2 broray_tx_caller_proc_rest_2 </proc/self/stat || return 1
    [ "$broray_tx_caller_proc_pid_1" = "$broray_tx_caller_proc_pid_2" ] || return 1
    broray_tx_caller_fd_start_2="$(broray_tx_proc_starttime /proc "$broray_tx_caller_proc_pid_2")" || return 1
    broray_tx_caller_fd_target_2="$(readlink -f "/proc/$broray_tx_caller_proc_pid_2/fd/9" 2>/dev/null)" || return 1
    [ "$broray_tx_caller_fd_start_1" = "$broray_tx_caller_fd_start_2" ] &&
    [ "$broray_tx_caller_fd_target_1" = "$broray_tx_caller_fd_target_2" ] &&
    [ "$broray_tx_caller_fd_target_1" = "$broray_tx_caller_fd_fifo_canonical" ]
}

broray_tx_native_opkg_lock_assert()
{
    broray_tx_opkg_lock_assert_tag="$1"
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ] || return 1
    broray_tx_native_opkg_caller_fd_assert || return 1
    broray_tx_native_opkg_owner_identity_assert || return 1
    broray_tx_opkg_lock_config_read || return 1
    broray_tx_opkg_lock_dir="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock"
    [ "$(sed -n '1p' "$broray_tx_opkg_lock_dir/config-arg" 2>/dev/null)" = "$BRORAY_TX_NATIVE_OPKG_CONFIG_ARG" ] || return 1
    if [ "$BRORAY_TX_NATIVE_OPKG_CONFIG_ARG" -eq 1 ]; then
        [ "$(broray_tx_sha "$BRORAY_TX_OPKG_CONF")" = "$(sed -n '1p' "$broray_tx_opkg_lock_dir/config-sha256" 2>/dev/null)" ] || return 1
        [ "$BRORAY_TX_NATIVE_OPKG_LOCK_CONFIGURED" = "$(sed -n '1p' "$broray_tx_opkg_lock_dir/configured-lock-path" 2>/dev/null)" ] || return 1
    fi
    broray_tx_opkg_lock_assert_expected_path="$BRORAY_TX_NATIVE_OPKG_LOCK_PATH"
    broray_tx_opkg_lock_assert_expected_type="$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE"
    broray_tx_native_opkg_lock_proc_snapshot "$broray_tx_opkg_lock_assert_tag" || return 1
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_PATH" = "$broray_tx_opkg_lock_assert_expected_path" ] || return 1
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE" = "$broray_tx_opkg_lock_assert_expected_type" ] || return 1
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$broray_tx_opkg_lock_assert_tag" \
        "$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID" "$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE" \
        "$BRORAY_TX_NATIVE_OPKG_LOCK_PATH" \
        "$BRORAY_TX_NATIVE_OPKG_LOCK_DEVINO" \
        >>"$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/evidence/opkg-lock-assertions.tsv"
}

broray_tx_native_opkg_lock_release()
{
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ] || return 0
    broray_tx_opkg_lock_owner="$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID"
    case "$broray_tx_opkg_lock_owner" in ''|*[!0-9]*|0) return 1 ;; esac
    broray_tx_opkg_lock_dir="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock"
    if [ -f "$broray_tx_opkg_lock_dir/owner-starttime" ]; then
        # Prove both immutable process identity and its exact kernel lock
        # before ending the FIFO stream.  No signal is sent, so a reused PID can
        # never be killed: EOF makes native OPKG release its own advisory lock.
        broray_tx_native_opkg_owner_identity_assert || return 1
        broray_tx_native_opkg_lock_proc_snapshot release-before-eof || return 1
    fi
    broray_tx_opkg_lock_release_start="$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_STARTTIME"
    broray_tx_opkg_lock_release_devino="$BRORAY_TX_NATIVE_OPKG_LOCK_DEVINO"
    broray_tx_opkg_lock_release_type="$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE"
    exec 9>&- || return 1
    broray_tx_opkg_lock_wait=0
    while [ "$(broray_tx_native_opkg_owner_starttime "$broray_tx_opkg_lock_owner" 2>/dev/null)" = \
            "$broray_tx_opkg_lock_release_start" ]; do
        broray_tx_opkg_lock_wait=$((broray_tx_opkg_lock_wait + 1))
        [ "$broray_tx_opkg_lock_wait" -lt 10 ] || return 1
        sleep 1
    done
    # The birth token disappeared before wait, so only the already-terminated
    # child job is reaped.  A numerically reused PID is neither signalled nor
    # treated as the holder.
    if wait "$broray_tx_opkg_lock_owner" 2>/dev/null; then
        broray_tx_opkg_lock_release_rc=0
    else
        broray_tx_opkg_lock_release_rc=$?
    fi
    printf '%s\n' "$broray_tx_opkg_lock_release_rc" \
        >"$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock/owner-exit-code" 2>/dev/null || true
    broray_tx_native_opkg_lock_record_absent \
        "$broray_tx_opkg_lock_owner" "$broray_tx_opkg_lock_release_devino" \
        "$broray_tx_opkg_lock_release_type" || return 1
    printf '%s\n' released >"$BRORAY_TX_NATIVE_OPKG_LOCK_WORK/native-opkg-lock/state" || return 1
    BRORAY_TX_NATIVE_OPKG_LOCK_HELD=0
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID=""
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_STARTTIME=""
    BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_CMDLINE_SHA256=""
    BRORAY_TX_NATIVE_OPKG_LOCK_TYPE=""
    BRORAY_TX_NATIVE_OPKG_LOCK_FIFO_ARGV=""
    BRORAY_TX_NATIVE_OPKG_LOCK_WORK=""
}

# Persist only bounded regular diagnostic files.  The FIFO is never copied,
# and the diagnostic path is unique to the exact operation/PID tuple.  This
# runs after EOF release on failures as well as on the explicit field preflight.
broray_tx_native_opkg_diagnostic_publish()
{
    broray_tx_diag_work="$1"
    broray_tx_diag_status="$2"
    broray_tx_diag_reason="$3"
    broray_tx_diag_id="$4"
    broray_tx_diag_mutation="${5:-false}"
    case "$broray_tx_diag_status" in PASS|FAIL) ;; *) return 1 ;; esac
    case "$broray_tx_diag_reason" in ''|*[!0-9A-Za-z._:-]*) return 1 ;; esac
    case "$broray_tx_diag_mutation" in false|unknown) ;; *) return 1 ;; esac
    broray_tx_valid_id "$broray_tx_diag_id" || return 1
    [ -d "$broray_tx_diag_work" ] && [ ! -L "$broray_tx_diag_work" ] || return 1
    broray_tx_diag_root="$BRORAY_TX_STATE_ROOT/field-gates"
    if [ ! -e "$broray_tx_diag_root" ] && [ ! -L "$broray_tx_diag_root" ]; then
        mkdir -p "$broray_tx_diag_root" || return 1
    fi
    [ -d "$broray_tx_diag_root" ] && [ ! -L "$broray_tx_diag_root" ] || return 1
    chmod 700 "$broray_tx_diag_root" 2>/dev/null || true
    broray_tx_diag_final="$broray_tx_diag_root/$broray_tx_diag_id"
    broray_tx_diag_part="$broray_tx_diag_root/.$broray_tx_diag_id.part.$$"
    [ ! -e "$broray_tx_diag_final" ] && [ ! -L "$broray_tx_diag_final" ] &&
    [ ! -e "$broray_tx_diag_part" ] && [ ! -L "$broray_tx_diag_part" ] || return 1
    mkdir "$broray_tx_diag_part" || return 1
    chmod 700 "$broray_tx_diag_part" 2>/dev/null || true

    broray_tx_diag_count=0
    broray_tx_diag_total_bytes=0
    for broray_tx_diag_source in \
        "$broray_tx_diag_work/opkg-state-before.tsv" \
        "$broray_tx_diag_work/opkg-state-after.tsv" \
        "$broray_tx_diag_work/native-opkg-lock/acquire-stage" \
        "$broray_tx_diag_work/native-opkg-lock/failure-reason" \
        "$broray_tx_diag_work/native-opkg-lock/owner-exit-code" \
        "$broray_tx_diag_work/native-opkg-lock/owner.stdout" \
        "$broray_tx_diag_work/native-opkg-lock/owner.stderr" \
        "$broray_tx_diag_work/native-opkg-lock/owner-pid" \
        "$broray_tx_diag_work/native-opkg-lock/owner-starttime" \
        "$broray_tx_diag_work/native-opkg-lock/owner-cmdline-sha256" \
        "$broray_tx_diag_work/native-opkg-lock/owner-exe-canonical" \
        "$broray_tx_diag_work/native-opkg-lock/owner-profile" \
        "$broray_tx_diag_work/native-opkg-lock/owner-opkg-argv0" \
        "$broray_tx_diag_work/native-opkg-lock/opkg-canonical" \
        "$broray_tx_diag_work/native-opkg-lock/config-arg" \
        "$broray_tx_diag_work/native-opkg-lock/config-path" \
        "$broray_tx_diag_work/native-opkg-lock/config-sha256" \
        "$broray_tx_diag_work/native-opkg-lock/configured-lock-path" \
        "$broray_tx_diag_work/native-opkg-lock/lock-fd" \
        "$broray_tx_diag_work/native-opkg-lock/lock-mnt-id" \
        "$broray_tx_diag_work/native-opkg-lock/lock-inode" \
        "$broray_tx_diag_work/native-opkg-lock/lock-devino" \
        "$broray_tx_diag_work/native-opkg-lock/lock-type" \
        "$broray_tx_diag_work/native-opkg-lock/lock-path" \
        "$broray_tx_diag_work/native-opkg-lock/lock-target" \
        "$broray_tx_diag_work/native-opkg-lock/proc-locks-before" \
        "$broray_tx_diag_work/native-opkg-lock/proc-locks-after" \
        "$broray_tx_diag_work/native-opkg-lock/state" \
        "$broray_tx_diag_work/evidence/opkg-lock-acquire-1.proc.tsv" \
        "$broray_tx_diag_work/evidence/opkg-lock-acquire-2.proc.tsv" \
        "$broray_tx_diag_work/evidence/opkg-lock-physical-preflight.proc.tsv" \
        "$broray_tx_diag_work/evidence/opkg-lock-release-before-eof.proc.tsv" \
        "$broray_tx_diag_work/evidence/opkg-lock-assertions.tsv" \
        "$broray_tx_diag_work/evidence/opkg-native-lock.json"
    do
        [ -e "$broray_tx_diag_source" ] || [ -L "$broray_tx_diag_source" ] || continue
        [ -f "$broray_tx_diag_source" ] && [ ! -L "$broray_tx_diag_source" ] || {
            rm -rf "$broray_tx_diag_part"; return 1;
        }
        broray_tx_diag_bytes="$(wc -c <"$broray_tx_diag_source" 2>/dev/null | tr -d ' ')" || {
            rm -rf "$broray_tx_diag_part"; return 1;
        }
        broray_tx_number "$broray_tx_diag_bytes" &&
        [ "$broray_tx_diag_bytes" -le "$BRORAY_TX_FIELD_DIAGNOSTIC_FILE_MAX_BYTES" ] || {
            rm -rf "$broray_tx_diag_part"; return 1;
        }
        broray_tx_diag_next_count=$((broray_tx_diag_count + 1))
        broray_tx_diag_next_total=$((broray_tx_diag_total_bytes + broray_tx_diag_bytes))
        [ "$broray_tx_diag_next_count" -le "$BRORAY_TX_FIELD_DIAGNOSTIC_FILE_MAX_COUNT" ] &&
        [ "$broray_tx_diag_next_total" -le "$BRORAY_TX_FIELD_DIAGNOSTIC_TOTAL_MAX_BYTES" ] || {
            rm -rf "$broray_tx_diag_part"; return 1;
        }
        broray_tx_diag_name="${broray_tx_diag_source#$broray_tx_diag_work/}"
        broray_tx_diag_name="$(printf '%s' "$broray_tx_diag_name" | tr '/' '_')"
        cp -p "$broray_tx_diag_source" "$broray_tx_diag_part/$broray_tx_diag_name" || {
            rm -rf "$broray_tx_diag_part"; return 1;
        }
        broray_tx_diag_count="$broray_tx_diag_next_count"
        broray_tx_diag_total_bytes="$broray_tx_diag_next_total"
    done
    jq -nc --arg candidateId 3.0.0-r15c16 \
        --arg operationId "$broray_tx_diag_id" --arg status "$broray_tx_diag_status" \
        --arg reason "$broray_tx_diag_reason" --arg completedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
        --arg mutation "$broray_tx_diag_mutation" \
        --argjson fileCount "$broray_tx_diag_count" \
        --argjson evidenceBytes "$broray_tx_diag_total_bytes" '
      {schemaVersion:1,candidateId:$candidateId,operationId:$operationId,
       status:$status,reason:$reason,
       mutationStarted:(if $mutation=="false" then false else null end),
       mutationState:$mutation,opkgPayloadBytesWritten:0,
       fileCount:$fileCount,evidenceBytes:$evidenceBytes,completedAt:$completedAt}
    ' >"$broray_tx_diag_part/result.json" || {
        rm -rf "$broray_tx_diag_part"; return 1;
    }
    (
        cd "$broray_tx_diag_part" || exit 1
        for broray_tx_diag_file in *; do
            [ "$broray_tx_diag_file" = SHA256SUMS ] && continue
            [ -f "$broray_tx_diag_file" ] && [ ! -L "$broray_tx_diag_file" ] || exit 1
            sha256sum "$broray_tx_diag_file"
        done | LC_ALL=C sort >SHA256SUMS
    ) || { rm -rf "$broray_tx_diag_part"; return 1; }
    mv "$broray_tx_diag_part" "$broray_tx_diag_final" || {
        rm -rf "$broray_tx_diag_part"; return 1;
    }
    BRORAY_TX_CONTROL_FAILURE_EVIDENCE="$broray_tx_diag_final"
}

broray_tx_physical_opkg_preflight_fail()
{
    broray_tx_physical_fail_reason="$1"
    case "$broray_tx_physical_fail_reason" in
        ''|*[!0-9A-Za-z._:-]*) broray_tx_physical_fail_reason=unclassified-physical-preflight-failure ;;
    esac
    if [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ]; then
        BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON="$broray_tx_physical_fail_reason"
        broray_tx_native_opkg_lock_acquire_fail >/dev/null 2>&1 || true
    fi
    if broray_tx_physical_opkg_state_fingerprint "$BRORAY_TX_WORK/opkg-state-after.tsv" &&
       cmp -s "$BRORAY_TX_WORK/opkg-state-before.tsv" "$BRORAY_TX_WORK/opkg-state-after.tsv"; then
        broray_tx_physical_fail_state=UNCHANGED
        broray_tx_physical_fail_mutation=false
    else
        broray_tx_physical_fail_state=CHANGED_OR_UNREADABLE
        broray_tx_physical_fail_mutation=unknown
    fi
    BRORAY_TX_CONTROL_FAILURE_REASON="$broray_tx_physical_fail_reason"
    broray_tx_native_opkg_diagnostic_publish "$BRORAY_TX_WORK" FAIL \
        "$BRORAY_TX_CONTROL_FAILURE_REASON" "$BRORAY_TX_OPERATION_ID" \
        "$broray_tx_physical_fail_mutation" || true
    printf 'PHYSICAL_OPKG_PREFLIGHT=FAIL\nREASON=%s\nOPKG_SHARED_STATE=%s\nEVIDENCE=%s\nMUTATION_STARTED=%s\n' \
        "$BRORAY_TX_CONTROL_FAILURE_REASON" "$broray_tx_physical_fail_state" \
        "${BRORAY_TX_CONTROL_FAILURE_EVIDENCE:-$BRORAY_TX_WORK}" \
        "$broray_tx_physical_fail_mutation"
    return 1
}

# Emit one fail-closed OPKG shared-state row without GNU coreutils `stat`.
# The target contract already requires findutils-find with `-printf` on
# Keenetic, and the same primitive is used by the canonical tree manifests.
broray_tx_physical_opkg_file_fingerprint()
{
    broray_tx_physical_file_path="$1"
    case "$broray_tx_physical_file_path" in
        /*) ;;
        *) return 1 ;;
    esac
    case "$broray_tx_physical_file_path" in
        *[!0-9A-Za-z._/+:-]*) return 1 ;;
    esac
    [ -f "$broray_tx_physical_file_path" ] &&
        [ ! -L "$broray_tx_physical_file_path" ] || return 1
    broray_tx_physical_file_meta="$(find -P "$broray_tx_physical_file_path" \
        -maxdepth 0 -printf '%m|%U|%G' 2>/dev/null)" || return 1
    IFS='|' read -r broray_tx_physical_file_mode broray_tx_physical_file_uid \
        broray_tx_physical_file_gid broray_tx_physical_file_extra <<EOF
$broray_tx_physical_file_meta
EOF
    [ -z "${broray_tx_physical_file_extra:-}" ] || return 1
    for broray_tx_physical_file_number in "$broray_tx_physical_file_mode" \
        "$broray_tx_physical_file_uid" "$broray_tx_physical_file_gid"
    do
        case "$broray_tx_physical_file_number" in
            ''|*[!0-9]*) return 1 ;;
        esac
    done
    broray_tx_physical_file_bytes="$(wc -c <"$broray_tx_physical_file_path" 2>/dev/null | tr -d ' ')" || return 1
    case "$broray_tx_physical_file_bytes" in
        ''|*[!0-9]*) return 1 ;;
    esac
    broray_tx_physical_file_sha="$(broray_tx_sha "$broray_tx_physical_file_path")" || return 1
    case "$broray_tx_physical_file_sha" in
        ''|*[!0-9a-f]*) return 1 ;;
    esac
    [ "${#broray_tx_physical_file_sha}" -eq 64 ] || return 1
    printf 'F\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$broray_tx_physical_file_mode" "$broray_tx_physical_file_uid" \
        "$broray_tx_physical_file_gid" "$broray_tx_physical_file_bytes" \
        "$broray_tx_physical_file_sha" "$broray_tx_physical_file_path"
}

broray_tx_physical_opkg_state_fingerprint()
{
    broray_tx_physical_state_output="$1"
    broray_tx_physical_state_part="$broray_tx_physical_state_output.part.$$"
    : >"$broray_tx_physical_state_part" || return 1
    for broray_tx_physical_state_path in \
        "$BRORAY_TX_STATUS_FILE" "$BRORAY_TX_OPKG_CONF" \
        "$BRORAY_TX_INFO_ROOT" "$BRORAY_TX_OPT_ROOT/var/opkg-lists"
    do
        if [ ! -e "$broray_tx_physical_state_path" ] && [ ! -L "$broray_tx_physical_state_path" ]; then
            printf 'A\t%s\n' "$broray_tx_physical_state_path" >>"$broray_tx_physical_state_part" || return 1
        elif [ -f "$broray_tx_physical_state_path" ] && [ ! -L "$broray_tx_physical_state_path" ]; then
            broray_tx_physical_opkg_file_fingerprint "$broray_tx_physical_state_path" \
                >>"$broray_tx_physical_state_part" || return 1
        elif [ -d "$broray_tx_physical_state_path" ] && [ ! -L "$broray_tx_physical_state_path" ]; then
            find -P "$broray_tx_physical_state_path" -type f -print | LC_ALL=C sort |
            while IFS= read -r broray_tx_physical_state_file; do
                [ -f "$broray_tx_physical_state_file" ] && [ ! -L "$broray_tx_physical_state_file" ] || exit 1
                broray_tx_physical_opkg_file_fingerprint "$broray_tx_physical_state_file" || exit 1
            done >>"$broray_tx_physical_state_part" || return 1
        else
            rm -f "$broray_tx_physical_state_part"
            return 1
        fi
    done
    mv -f "$broray_tx_physical_state_part" "$broray_tx_physical_state_output"
}

broray_tx_physical_opkg_preflight()
{
    broray_tx_physical_id="${1:-r14c01-opkg-preflight-$(date -u '+%Y%m%d%H%M%S')-$$}"
    broray_tx_valid_id "$broray_tx_physical_id" || return 2
    [ "$BRORAY_TX_FS_ROOT" = / ] || {
        [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] || return 2
    }
    [ -d "$BRORAY_TX_TMP_BASE" ] && [ ! -L "$BRORAY_TX_TMP_BASE" ] &&
        [ -w "$BRORAY_TX_TMP_BASE" ] || return 2
    [ ! -e "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] &&
    [ ! -e "$BRORAY_TX_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_TX_GLOBAL_LOCK" ] || {
        printf '%s\n' 'PHYSICAL_OPKG_PREFLIGHT=BLOCKED_BY_BROray_CONTROL'
        return 1
    }
    BRORAY_TX_OPERATION_ID="$broray_tx_physical_id"
    BRORAY_TX_MODE=verify-only
    BRORAY_TX_WORK="$BRORAY_TX_TMP_BASE/broray-physical-gate-$broray_tx_physical_id"
    [ ! -e "$BRORAY_TX_WORK" ] && [ ! -L "$BRORAY_TX_WORK" ] || return 2
    mkdir -p "$BRORAY_TX_WORK/evidence" || return 2
    chmod 700 "$BRORAY_TX_WORK" "$BRORAY_TX_WORK/evidence" 2>/dev/null || true
    printf '%s\n' "$BRORAY_TX_OPERATION_ID" >"$BRORAY_TX_WORK/operation-id" || return 2
    broray_tx_physical_opkg_state_fingerprint "$BRORAY_TX_WORK/opkg-state-before.tsv" || return 2
    BRORAY_TX_NATIVE_OPKG_LOCK_WORK="$BRORAY_TX_WORK"
    if ! broray_tx_native_opkg_lock_acquire; then
        broray_tx_physical_opkg_preflight_fail \
            "${BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON:-unclassified-native-lock-failure}"
        return $?
    fi
    broray_tx_physical_type="$BRORAY_TX_NATIVE_OPKG_LOCK_TYPE"
    broray_tx_physical_path="$BRORAY_TX_NATIVE_OPKG_LOCK_PATH"
    broray_tx_physical_owner="$BRORAY_TX_NATIVE_OPKG_LOCK_OWNER_PID"
    broray_tx_native_opkg_lock_assert physical-preflight || {
        broray_tx_physical_opkg_preflight_fail physical-preflight-assertion-failed
        return $?
    }
    broray_tx_native_opkg_lock_release || {
        broray_tx_physical_opkg_preflight_fail native-lock-release-proof-failed
        return $?
    }
    broray_tx_physical_opkg_state_fingerprint "$BRORAY_TX_WORK/opkg-state-after.tsv" || {
        broray_tx_physical_opkg_preflight_fail opkg-shared-state-fingerprint-failed
        return $?
    }
    cmp -s "$BRORAY_TX_WORK/opkg-state-before.tsv" "$BRORAY_TX_WORK/opkg-state-after.tsv" || {
        broray_tx_physical_opkg_preflight_fail opkg-shared-state-changed-during-preflight
        return $?
    }
    broray_tx_native_opkg_diagnostic_publish "$BRORAY_TX_WORK" PASS \
        native-opkg-lock-proven "$BRORAY_TX_OPERATION_ID" || return 1
    rm -rf "$BRORAY_TX_WORK" || return 1
    printf 'PHYSICAL_OPKG_PREFLIGHT=PASS\nLOCK_TYPE=%s\nLOCK_PATH=%s\nOWNER_PID=%s\nOPKG_SHARED_STATE=UNCHANGED\nEVIDENCE=%s\nMUTATION_STARTED=false\n' \
        "$broray_tx_physical_type" "$broray_tx_physical_path" "$broray_tx_physical_owner" \
        "$BRORAY_TX_CONTROL_FAILURE_EVIDENCE"
}

broray_tx_terminal_cleanup_complete()
{
    broray_tx_terminal_complete_id="$1"
    broray_tx_valid_id "$broray_tx_terminal_complete_id" || return 1
    broray_tx_terminal_complete_file="$BRORAY_TX_OPERATION_ROOT/$broray_tx_terminal_complete_id/terminal.json"
    [ -e "$broray_tx_terminal_complete_file" ] || [ -L "$broray_tx_terminal_complete_file" ] || return 0
    [ -f "$broray_tx_terminal_complete_file" ] && [ ! -L "$broray_tx_terminal_complete_file" ] || return 1
    command -v jq >/dev/null 2>&1 || return 2
    jq -e --arg id "$broray_tx_terminal_complete_id" '
      .operationId==$id and
      (.status=="SUCCESS_COMMITTED" or .status=="RESTORE_SUCCESS_COMMITTED") and
      (.cleanupComplete==false or .cleanupComplete==true)
    ' "$broray_tx_terminal_complete_file" >/dev/null 2>&1 || return 1
    jq -e '.cleanupComplete==true' "$broray_tx_terminal_complete_file" >/dev/null 2>&1 && return 0
    broray_tx_terminal_complete_part="$broray_tx_terminal_complete_file.part.$$"
    jq '.cleanupComplete=true' "$broray_tx_terminal_complete_file" \
        >"$broray_tx_terminal_complete_part" || { rm -f "$broray_tx_terminal_complete_part"; return 1; }
    chmod 600 "$broray_tx_terminal_complete_part" 2>/dev/null || true
    mv -f "$broray_tx_terminal_complete_part" "$broray_tx_terminal_complete_file" || {
        rm -f "$broray_tx_terminal_complete_part"; return 1;
    }
}

# The rollback-failed lock rename is authenticated by a sibling record whose
# five hashes describe the immutable ownerless lock payload.  Deletion is
# deliberately file-by-file: after a crash, any exact subset remains
# authorized by the record and the next kernel-mutex holder can resume it.
broray_tx_rollback_failed_retire_record_validate()
{
    broray_tx_rollback_record="$1"
    [ -f "$broray_tx_rollback_record" ] && [ ! -L "$broray_tx_rollback_record" ] || return 1
    broray_tx_rollback_record_bytes="$(wc -c <"$broray_tx_rollback_record" 2>/dev/null | tr -d ' ')" || return 1
    broray_tx_number "$broray_tx_rollback_record_bytes" &&
        [ "$broray_tx_rollback_record_bytes" -le "$BRORAY_TX_METADATA_MAX_BYTES" ] || return 1
    [ "$(wc -l <"$broray_tx_rollback_record" 2>/dev/null | tr -d ' ')" -eq 9 ] || return 1
    awk -F '\t' '
      NR==1 {ok=($1=="contract" && $2=="rollback-failed-lock-retire/1" && NF==2)}
      NR==2 {ok=ok && $1=="operation-id" && NF==2}
      NR==3 {ok=ok && $1=="canonical-lock" && NF==2}
      NR==4 {ok=ok && $1=="retired-lock" && NF==2}
      NR==5 {ok=ok && $1=="operation-id-sha256" && NF==2}
      NR==6 {ok=ok && $1=="operation-type-sha256" && NF==2}
      NR==7 {ok=ok && $1=="started-at-sha256" && NF==2}
      NR==8 {ok=ok && $1=="source-version-sha256" && NF==2}
      NR==9 {ok=ok && $1=="target-version-sha256" && NF==2}
      END {exit ok ? 0 : 1}
    ' "$broray_tx_rollback_record" || return 1
    BRORAY_TX_ROLLBACK_RETIRE_ID="$(awk -F '\t' 'NR==2{print $2}' "$broray_tx_rollback_record")"
    BRORAY_TX_ROLLBACK_RETIRE_CANONICAL="$(awk -F '\t' 'NR==3{print $2}' "$broray_tx_rollback_record")"
    BRORAY_TX_ROLLBACK_RETIRE_PATH="$(awk -F '\t' 'NR==4{print $2}' "$broray_tx_rollback_record")"
    BRORAY_TX_ROLLBACK_RETIRE_OPERATION_ID_SHA="$(awk -F '\t' 'NR==5{print $2}' "$broray_tx_rollback_record")"
    BRORAY_TX_ROLLBACK_RETIRE_OPERATION_TYPE_SHA="$(awk -F '\t' 'NR==6{print $2}' "$broray_tx_rollback_record")"
    BRORAY_TX_ROLLBACK_RETIRE_STARTED_SHA="$(awk -F '\t' 'NR==7{print $2}' "$broray_tx_rollback_record")"
    BRORAY_TX_ROLLBACK_RETIRE_SOURCE_SHA="$(awk -F '\t' 'NR==8{print $2}' "$broray_tx_rollback_record")"
    BRORAY_TX_ROLLBACK_RETIRE_TARGET_SHA="$(awk -F '\t' 'NR==9{print $2}' "$broray_tx_rollback_record")"
    broray_tx_valid_id "$BRORAY_TX_ROLLBACK_RETIRE_ID" || return 1
    [ "$broray_tx_rollback_record" = \
        "$BRORAY_TX_TMP_BASE/.broray-rollback-failed-retire-$BRORAY_TX_ROLLBACK_RETIRE_ID.tsv" ] &&
    [ "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL" = "$BRORAY_TX_LOCK_DIR" ] &&
    [ "$BRORAY_TX_ROLLBACK_RETIRE_PATH" = \
        "$BRORAY_TX_TMP_BASE/.broray-rollback-failed-lock-$BRORAY_TX_ROLLBACK_RETIRE_ID" ] || return 1
    for broray_tx_rollback_record_sha in \
        "$BRORAY_TX_ROLLBACK_RETIRE_OPERATION_ID_SHA" \
        "$BRORAY_TX_ROLLBACK_RETIRE_OPERATION_TYPE_SHA" \
        "$BRORAY_TX_ROLLBACK_RETIRE_STARTED_SHA" \
        "$BRORAY_TX_ROLLBACK_RETIRE_SOURCE_SHA" \
        "$BRORAY_TX_ROLLBACK_RETIRE_TARGET_SHA"
    do
        case "$broray_tx_rollback_record_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
        [ "${#broray_tx_rollback_record_sha}" -eq 64 ] || return 1
    done
}

broray_tx_rollback_failed_retire_record_publish()
{
    broray_tx_rollback_publish_id="$1"
    broray_tx_rollback_publish_path="$2"
    broray_tx_valid_id "$broray_tx_rollback_publish_id" || return 1
    [ "$broray_tx_rollback_publish_path" = \
        "$BRORAY_TX_TMP_BASE/.broray-rollback-failed-lock-$broray_tx_rollback_publish_id" ] || return 1
    [ -d "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] || return 1
    for broray_tx_rollback_publish_name in \
        operation-id operation-type started-at source-version target-version
    do
        [ -f "$BRORAY_TX_LOCK_DIR/$broray_tx_rollback_publish_name" ] &&
            [ ! -L "$BRORAY_TX_LOCK_DIR/$broray_tx_rollback_publish_name" ] &&
            [ "$(wc -l <"$BRORAY_TX_LOCK_DIR/$broray_tx_rollback_publish_name" | tr -d ' ')" -eq 1 ] || return 1
        broray_tx_rollback_publish_sha="$(broray_tx_sha \
            "$BRORAY_TX_LOCK_DIR/$broray_tx_rollback_publish_name")" || return 1
        case "$broray_tx_rollback_publish_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
        [ "${#broray_tx_rollback_publish_sha}" -eq 64 ] || return 1
        case "$broray_tx_rollback_publish_name" in
            operation-id) broray_tx_rollback_publish_id_sha="$broray_tx_rollback_publish_sha" ;;
            operation-type) broray_tx_rollback_publish_type_sha="$broray_tx_rollback_publish_sha" ;;
            started-at) broray_tx_rollback_publish_started_sha="$broray_tx_rollback_publish_sha" ;;
            source-version) broray_tx_rollback_publish_source_sha="$broray_tx_rollback_publish_sha" ;;
            target-version) broray_tx_rollback_publish_target_sha="$broray_tx_rollback_publish_sha" ;;
        esac
    done
    [ "$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/operation-id")" = \
        "$broray_tx_rollback_publish_id" ] || return 1
    broray_tx_rollback_publish_record="$BRORAY_TX_TMP_BASE/.broray-rollback-failed-retire-$broray_tx_rollback_publish_id.tsv"
    broray_tx_rollback_publish_part="$broray_tx_rollback_publish_record.part"
    if [ -e "$broray_tx_rollback_publish_part" ] || [ -L "$broray_tx_rollback_publish_part" ]; then
        [ -f "$broray_tx_rollback_publish_part" ] &&
            [ ! -L "$broray_tx_rollback_publish_part" ] &&
            rm -f "$broray_tx_rollback_publish_part" || return 1
    fi
    printf 'contract\trollback-failed-lock-retire/1\noperation-id\t%s\ncanonical-lock\t%s\nretired-lock\t%s\noperation-id-sha256\t%s\noperation-type-sha256\t%s\nstarted-at-sha256\t%s\nsource-version-sha256\t%s\ntarget-version-sha256\t%s\n' \
        "$broray_tx_rollback_publish_id" "$BRORAY_TX_LOCK_DIR" "$broray_tx_rollback_publish_path" \
        "$broray_tx_rollback_publish_id_sha" "$broray_tx_rollback_publish_type_sha" \
        "$broray_tx_rollback_publish_started_sha" "$broray_tx_rollback_publish_source_sha" \
        "$broray_tx_rollback_publish_target_sha" \
        >"$broray_tx_rollback_publish_part" || return 1
    chmod 600 "$broray_tx_rollback_publish_part" 2>/dev/null || true
    if [ -e "$broray_tx_rollback_publish_record" ] || [ -L "$broray_tx_rollback_publish_record" ]; then
        [ -f "$broray_tx_rollback_publish_record" ] &&
            [ ! -L "$broray_tx_rollback_publish_record" ] &&
            broray_tx_files_equal "$broray_tx_rollback_publish_part" \
                "$broray_tx_rollback_publish_record" &&
            rm -f "$broray_tx_rollback_publish_part" || return 1
    else
        mv -f "$broray_tx_rollback_publish_part" "$broray_tx_rollback_publish_record" || return 1
    fi
    broray_tx_rollback_failed_retire_record_validate "$broray_tx_rollback_publish_record"
}

broray_tx_rollback_failed_retired_lock_remove()
{
    broray_tx_rollback_remove_record="$1"
    broray_tx_rollback_remove_context="${2:-scavenge}"
    case "$broray_tx_rollback_remove_context" in primary|scavenge) ;; *) return 1 ;; esac
    broray_tx_rollback_failed_retire_record_validate "$broray_tx_rollback_remove_record" || return 1
    [ ! -e "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL" ] &&
        [ ! -L "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL" ] || return 1
    if [ -e "$BRORAY_TX_ROLLBACK_RETIRE_PATH" ] || [ -L "$BRORAY_TX_ROLLBACK_RETIRE_PATH" ]; then
        [ -d "$BRORAY_TX_ROLLBACK_RETIRE_PATH" ] &&
            [ ! -L "$BRORAY_TX_ROLLBACK_RETIRE_PATH" ] &&
            [ ! -e "$BRORAY_TX_ROLLBACK_RETIRE_PATH/owner-identity.tsv" ] &&
            [ ! -L "$BRORAY_TX_ROLLBACK_RETIRE_PATH/owner-identity.tsv" ] || return 1
        for broray_tx_rollback_remove_entry in \
            "$BRORAY_TX_ROLLBACK_RETIRE_PATH"/* \
            "$BRORAY_TX_ROLLBACK_RETIRE_PATH"/.[!.]* \
            "$BRORAY_TX_ROLLBACK_RETIRE_PATH"/..?*
        do
            [ -e "$broray_tx_rollback_remove_entry" ] ||
                [ -L "$broray_tx_rollback_remove_entry" ] || continue
            case "${broray_tx_rollback_remove_entry##*/}" in
                operation-id) broray_tx_rollback_remove_sha="$BRORAY_TX_ROLLBACK_RETIRE_OPERATION_ID_SHA" ;;
                operation-type) broray_tx_rollback_remove_sha="$BRORAY_TX_ROLLBACK_RETIRE_OPERATION_TYPE_SHA" ;;
                started-at) broray_tx_rollback_remove_sha="$BRORAY_TX_ROLLBACK_RETIRE_STARTED_SHA" ;;
                source-version) broray_tx_rollback_remove_sha="$BRORAY_TX_ROLLBACK_RETIRE_SOURCE_SHA" ;;
                target-version) broray_tx_rollback_remove_sha="$BRORAY_TX_ROLLBACK_RETIRE_TARGET_SHA" ;;
                *) return 1 ;;
            esac
            [ -f "$broray_tx_rollback_remove_entry" ] &&
                [ ! -L "$broray_tx_rollback_remove_entry" ] &&
                [ "$(wc -l <"$broray_tx_rollback_remove_entry" | tr -d ' ')" -eq 1 ] &&
                [ "$(broray_tx_sha "$broray_tx_rollback_remove_entry")" = \
                    "$broray_tx_rollback_remove_sha" ] || return 1
            rm -f "$broray_tx_rollback_remove_entry" || return 1
            if [ "$broray_tx_rollback_remove_context" = primary ] &&
               [ "${broray_tx_rollback_remove_entry##*/}" = operation-id ]; then
                broray_tx_test_pause rollback-failed-retired-lock-partial \
                    "$BRORAY_TX_WORK/evidence" || return 1
            fi
        done
        rmdir "$BRORAY_TX_ROLLBACK_RETIRE_PATH" || return 1
    fi
    [ ! -e "$BRORAY_TX_ROLLBACK_RETIRE_PATH" ] &&
        [ ! -L "$BRORAY_TX_ROLLBACK_RETIRE_PATH" ] || return 1
    rm -f "$broray_tx_rollback_remove_record" || return 1
    [ ! -e "$broray_tx_rollback_remove_record" ] &&
        [ ! -L "$broray_tx_rollback_remove_record" ]
}

broray_tx_rollback_failed_canonical_lock_matches_record()
{
    broray_tx_rollback_match_record="$1"
    broray_tx_rollback_failed_retire_record_validate "$broray_tx_rollback_match_record" || return 1
    [ -d "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL" ] &&
        [ ! -L "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL" ] &&
        [ ! -e "$BRORAY_TX_ROLLBACK_RETIRE_PATH" ] &&
        [ ! -L "$BRORAY_TX_ROLLBACK_RETIRE_PATH" ] || return 1
    for broray_tx_rollback_match_name in \
        operation-id operation-type started-at source-version target-version
    do
        case "$broray_tx_rollback_match_name" in
            operation-id) broray_tx_rollback_match_sha="$BRORAY_TX_ROLLBACK_RETIRE_OPERATION_ID_SHA" ;;
            operation-type) broray_tx_rollback_match_sha="$BRORAY_TX_ROLLBACK_RETIRE_OPERATION_TYPE_SHA" ;;
            started-at) broray_tx_rollback_match_sha="$BRORAY_TX_ROLLBACK_RETIRE_STARTED_SHA" ;;
            source-version) broray_tx_rollback_match_sha="$BRORAY_TX_ROLLBACK_RETIRE_SOURCE_SHA" ;;
            target-version) broray_tx_rollback_match_sha="$BRORAY_TX_ROLLBACK_RETIRE_TARGET_SHA" ;;
        esac
        [ -f "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL/$broray_tx_rollback_match_name" ] &&
            [ ! -L "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL/$broray_tx_rollback_match_name" ] &&
            [ "$(broray_tx_sha "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL/$broray_tx_rollback_match_name")" = \
                "$broray_tx_rollback_match_sha" ] || return 1
    done
    for broray_tx_rollback_match_entry in \
        "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL"/* \
        "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL"/.[!.]* \
        "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL"/..?*
    do
        [ -e "$broray_tx_rollback_match_entry" ] ||
            [ -L "$broray_tx_rollback_match_entry" ] || continue
        case "${broray_tx_rollback_match_entry##*/}" in
            operation-id|operation-type|started-at|source-version|target-version|owner-identity.tsv) ;;
            *) return 1 ;;
        esac
        [ -f "$broray_tx_rollback_match_entry" ] &&
            [ ! -L "$broray_tx_rollback_match_entry" ] || return 1
    done
}

broray_tx_rollback_failed_retired_lock_scavenge()
{
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ] || return 1
    for broray_tx_rollback_scavenge_record in \
        "$BRORAY_TX_TMP_BASE"/.broray-rollback-failed-retire-*.tsv
    do
        [ -e "$broray_tx_rollback_scavenge_record" ] ||
            [ -L "$broray_tx_rollback_scavenge_record" ] || continue
        broray_tx_rollback_failed_retire_record_validate \
            "$broray_tx_rollback_scavenge_record" || return 1
        broray_tx_rollback_scavenge_canonical=0
        broray_tx_rollback_scavenge_retired=0
        [ ! -e "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL" ] &&
            [ ! -L "$BRORAY_TX_ROLLBACK_RETIRE_CANONICAL" ] ||
            broray_tx_rollback_scavenge_canonical=1
        [ ! -e "$BRORAY_TX_ROLLBACK_RETIRE_PATH" ] &&
            [ ! -L "$BRORAY_TX_ROLLBACK_RETIRE_PATH" ] ||
            broray_tx_rollback_scavenge_retired=1
        [ $((broray_tx_rollback_scavenge_canonical + broray_tx_rollback_scavenge_retired)) -le 1 ] || return 1
        if [ "$broray_tx_rollback_scavenge_retired" -eq 1 ]; then
            broray_tx_rollback_failed_retired_lock_remove \
                "$broray_tx_rollback_scavenge_record" || return 1
        elif [ "$broray_tx_rollback_scavenge_canonical" -eq 1 ]; then
            broray_tx_rollback_failed_canonical_lock_matches_record \
                "$broray_tx_rollback_scavenge_record" || return 1
        else
            rm -f "$broray_tx_rollback_scavenge_record" || return 1
        fi
    done
    for broray_tx_rollback_scavenge_lock in \
        "$BRORAY_TX_TMP_BASE"/.broray-rollback-failed-lock-*
    do
        [ -e "$broray_tx_rollback_scavenge_lock" ] ||
            [ -L "$broray_tx_rollback_scavenge_lock" ] || continue
        broray_tx_rollback_scavenge_id="${broray_tx_rollback_scavenge_lock##*/.broray-rollback-failed-lock-}"
        broray_tx_valid_id "$broray_tx_rollback_scavenge_id" || return 1
        broray_tx_rollback_scavenge_record="$BRORAY_TX_TMP_BASE/.broray-rollback-failed-retire-$broray_tx_rollback_scavenge_id.tsv"
        [ -f "$broray_tx_rollback_scavenge_record" ] &&
            [ ! -L "$broray_tx_rollback_scavenge_record" ] || return 1
    done
    return 0
}

broray_tx_retired_work_scavenge()
{
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ] || return 1
    for broray_tx_retired_record in "$BRORAY_TX_TMP_BASE"/.broray-retired-record-*.tsv; do
        [ -e "$broray_tx_retired_record" ] || [ -L "$broray_tx_retired_record" ] || continue
        [ -f "$broray_tx_retired_record" ] && [ ! -L "$broray_tx_retired_record" ] || return 1
        broray_tx_retired_record_bytes="$(wc -c <"$broray_tx_retired_record" 2>/dev/null | tr -d ' ')" || return 1
        broray_tx_number "$broray_tx_retired_record_bytes" &&
            [ "$broray_tx_retired_record_bytes" -le "$BRORAY_TX_METADATA_MAX_BYTES" ] || return 1
        [ "$(wc -l <"$broray_tx_retired_record" 2>/dev/null | tr -d ' ')" -eq 7 ] || return 1
        awk -F '\t' '
          NR==1 {ok=($1=="contract" && $2=="terminal-retire/1" && NF==2)}
          NR==2 {ok=ok && $1=="operation-id" && NF==2}
          NR==3 {ok=ok && $1=="canonical-work" && NF==2}
          NR==4 {ok=ok && $1=="retired-work" && NF==2}
          NR==5 {ok=ok && $1=="retired-lock" && NF==2}
          NR==6 {ok=ok && $1=="owner-pid" && NF==2}
          NR==7 {ok=ok && $1=="owner-starttime" && NF==2}
          END {exit ok ? 0 : 1}
        ' "$broray_tx_retired_record" || return 1
        broray_tx_retired_id="$(awk -F '\t' 'NR==2{print $2}' "$broray_tx_retired_record")"
        broray_tx_retired_canonical="$(awk -F '\t' 'NR==3{print $2}' "$broray_tx_retired_record")"
        broray_tx_retired_path="$(awk -F '\t' 'NR==4{print $2}' "$broray_tx_retired_record")"
        broray_tx_retired_lock="$(awk -F '\t' 'NR==5{print $2}' "$broray_tx_retired_record")"
        broray_tx_retired_pid="$(awk -F '\t' 'NR==6{print $2}' "$broray_tx_retired_record")"
        broray_tx_retired_start="$(awk -F '\t' 'NR==7{print $2}' "$broray_tx_retired_record")"
        broray_tx_valid_id "$broray_tx_retired_id" || return 1
        case "$broray_tx_retired_pid:$broray_tx_retired_start" in ''|*[!0-9:]*) return 1 ;; esac
        [ "$broray_tx_retired_pid" -gt 0 ] && [ "$broray_tx_retired_start" -gt 0 ] || return 1
        broray_tx_retired_expected_canonical="$BRORAY_TX_TMP_BASE/broray-update-$broray_tx_retired_id"
        broray_tx_retired_expected_path="$BRORAY_TX_TMP_BASE/.broray-retired-$broray_tx_retired_id-$broray_tx_retired_pid-$broray_tx_retired_start"
        broray_tx_retired_expected_lock="$BRORAY_TX_TMP_BASE/.broray-retired-lock-$broray_tx_retired_id-$broray_tx_retired_pid-$broray_tx_retired_start"
        broray_tx_retired_expected_record="$BRORAY_TX_TMP_BASE/.broray-retired-record-$broray_tx_retired_id-$broray_tx_retired_pid-$broray_tx_retired_start.tsv"
        [ "$broray_tx_retired_canonical" = "$broray_tx_retired_expected_canonical" ] &&
        [ "$broray_tx_retired_path" = "$broray_tx_retired_expected_path" ] &&
        [ "$broray_tx_retired_lock" = "$broray_tx_retired_expected_lock" ] &&
        [ "$broray_tx_retired_record" = "$broray_tx_retired_expected_record" ] || return 1
        if [ -e "$broray_tx_retired_lock" ] || [ -L "$broray_tx_retired_lock" ]; then
            [ -d "$broray_tx_retired_lock" ] && [ ! -L "$broray_tx_retired_lock" ] || return 1
            [ ! -e "$broray_tx_retired_lock/owner-identity.tsv" ] &&
                [ ! -L "$broray_tx_retired_lock/owner-identity.tsv" ] || return 1
            for broray_tx_retired_lock_entry in \
                "$broray_tx_retired_lock"/* "$broray_tx_retired_lock"/.[!.]* "$broray_tx_retired_lock"/..?*
            do
                [ -e "$broray_tx_retired_lock_entry" ] || [ -L "$broray_tx_retired_lock_entry" ] || continue
                case "${broray_tx_retired_lock_entry##*/}" in
                    operation-id|operation-type|started-at|source-version|target-version) ;;
                    *) return 1 ;;
                esac
                [ -f "$broray_tx_retired_lock_entry" ] && [ ! -L "$broray_tx_retired_lock_entry" ] || return 1
            done
            [ -f "$broray_tx_retired_lock/operation-id" ] &&
                [ "$(sed -n '1p' "$broray_tx_retired_lock/operation-id")" = "$broray_tx_retired_id" ] || return 1
            rm -rf "$broray_tx_retired_lock" || return 1
        fi
        if [ -e "$broray_tx_retired_path" ] || [ -L "$broray_tx_retired_path" ]; then
            [ ! -e "$broray_tx_retired_canonical" ] && [ ! -L "$broray_tx_retired_canonical" ] || return 1
            [ -d "$broray_tx_retired_path" ] && [ ! -L "$broray_tx_retired_path" ] || return 1
            [ ! -e "$broray_tx_retired_path/owner-identity.tsv" ] &&
                [ ! -L "$broray_tx_retired_path/owner-identity.tsv" ] || return 1
            [ -f "$broray_tx_retired_path/operation-id" ] &&
                [ ! -L "$broray_tx_retired_path/operation-id" ] &&
            [ "$(sed -n '1p' "$broray_tx_retired_path/operation-id")" = "$broray_tx_retired_id" ] || return 1
            rm -rf "$broray_tx_retired_path" || return 1
        fi
        if [ ! -e "$broray_tx_retired_canonical" ] && [ ! -L "$broray_tx_retired_canonical" ] &&
           [ ! -e "$broray_tx_retired_path" ] && [ ! -L "$broray_tx_retired_path" ] &&
           [ ! -e "$broray_tx_retired_lock" ] && [ ! -L "$broray_tx_retired_lock" ]; then
            broray_tx_terminal_cleanup_complete "$broray_tx_retired_id"
            broray_tx_retired_terminal_rc=$?
            case "$broray_tx_retired_terminal_rc" in
                0) rm -f "$broray_tx_retired_record" || return 1 ;;
                2) : ;; # Keep the tiny proof for a later jq-capable pass.
                *) return 1 ;;
            esac
        fi
    done
    # Every retired directory created by this protocol has a record.  Unknown
    # hidden trees are retained fail-closed rather than guessed/deleted.
    for broray_tx_retired_unknown in "$BRORAY_TX_TMP_BASE"/.broray-retired-*; do
        [ -e "$broray_tx_retired_unknown" ] || [ -L "$broray_tx_retired_unknown" ] || continue
        case "${broray_tx_retired_unknown##*/}" in .broray-retired-record-*.tsv) continue ;; esac
        return 1
    done
    return 0
}

# The kernel OPKG write lock is also the short mutex for publishing, taking
# over, handing off and retiring BROray control directories.  Unlike a
# userspace mkdir claim, the mutex cannot become a permanent crash wedge:
# death of this shell closes fd 9, OPKG reads EOF from the FIFO and the kernel
# releases the exact native POSIX or FLOCK lock proven from that OPKG owner FD.
# Lifecycle work never lives in this bounded scratch
# directory and the mutex is released before any long recovery or mutation.
broray_tx_control_mutex_stale_scratch_cleanup()
{
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ] || return 1
    broray_tx_control_scratch_current="$BRORAY_TX_NATIVE_OPKG_LOCK_WORK"
    broray_tx_control_scratch_saved_work="$BRORAY_TX_WORK"
    for broray_tx_control_scratch in "$BRORAY_TX_TMP_BASE"/.broray-control-mutex-*; do
        [ -e "$broray_tx_control_scratch" ] || [ -L "$broray_tx_control_scratch" ] || continue
        [ "$broray_tx_control_scratch" != "$broray_tx_control_scratch_current" ] || continue
        broray_tx_control_scratch_name="${broray_tx_control_scratch##*/}"
        broray_tx_control_scratch_tuple="${broray_tx_control_scratch_name#.broray-control-mutex-}"
        broray_tx_control_scratch_pid="${broray_tx_control_scratch_tuple%%-*}"
        broray_tx_control_scratch_start="${broray_tx_control_scratch_tuple#*-}"
        case "$broray_tx_control_scratch_pid:$broray_tx_control_scratch_start" in
            ''|*[!0-9:]*) continue ;;
        esac
        [ "$broray_tx_control_scratch_pid" -gt 0 ] &&
            [ "$broray_tx_control_scratch_start" -gt 0 ] || continue
        [ -d "$broray_tx_control_scratch" ] && [ ! -L "$broray_tx_control_scratch" ] || continue
        [ -f "$broray_tx_control_scratch/owner-identity.tsv" ] &&
            [ ! -L "$broray_tx_control_scratch/owner-identity.tsv" ] || continue
        broray_tx_control_owner_require_stale \
            "$broray_tx_control_scratch/owner-identity.tsv" || continue
        [ "$BRORAY_TX_CONTROL_OWNER_PID" = "$broray_tx_control_scratch_pid" ] &&
            [ "$BRORAY_TX_CONTROL_OWNER_STARTTIME" = "$broray_tx_control_scratch_start" ] || continue
        broray_tx_control_scratch_shape_ok=1
        for broray_tx_control_scratch_entry in "$broray_tx_control_scratch"/* "$broray_tx_control_scratch"/.[!.]* "$broray_tx_control_scratch"/..?*; do
            [ -e "$broray_tx_control_scratch_entry" ] || [ -L "$broray_tx_control_scratch_entry" ] || continue
            case "${broray_tx_control_scratch_entry##*/}" in
                owner-identity.tsv)
                    [ -f "$broray_tx_control_scratch_entry" ] &&
                        [ ! -L "$broray_tx_control_scratch_entry" ] ||
                        broray_tx_control_scratch_shape_ok=0 ;;
                evidence|native-opkg-lock)
                    [ -d "$broray_tx_control_scratch_entry" ] &&
                        [ ! -L "$broray_tx_control_scratch_entry" ] ||
                        broray_tx_control_scratch_shape_ok=0 ;;
                *) broray_tx_control_scratch_shape_ok=0 ;;
            esac
        done
        [ "$broray_tx_control_scratch_shape_ok" -eq 1 ] || continue

        # Reuse the exact native-owner classifier.  It removes only a dead or
        # reused holder directory; a live, blocked or ambiguous OPKG child is
        # preserved for a later pass.
        BRORAY_TX_WORK="$broray_tx_control_scratch"
        if ! broray_tx_recovery_stale_native_clear; then
            BRORAY_TX_WORK="$broray_tx_control_scratch_saved_work"
            continue
        fi
        BRORAY_TX_WORK="$broray_tx_control_scratch_saved_work"
        [ -d "$broray_tx_control_scratch/evidence" ] &&
            [ ! -L "$broray_tx_control_scratch/evidence" ] || continue
        broray_tx_control_scratch_evidence_ok=1
        for broray_tx_control_scratch_evidence in "$broray_tx_control_scratch/evidence"/* "$broray_tx_control_scratch/evidence"/.[!.]* "$broray_tx_control_scratch/evidence"/..?*; do
            [ -e "$broray_tx_control_scratch_evidence" ] ||
                [ -L "$broray_tx_control_scratch_evidence" ] || continue
            [ -f "$broray_tx_control_scratch_evidence" ] &&
                [ ! -L "$broray_tx_control_scratch_evidence" ] || {
                    broray_tx_control_scratch_evidence_ok=0; continue;
                }
            case "${broray_tx_control_scratch_evidence##*/}" in
                opkg-lock-*.proc.tsv|opkg-native-lock.json|opkg-lock-assertions.tsv) ;;
                *) broray_tx_control_scratch_evidence_ok=0 ;;
            esac
        done
        [ "$broray_tx_control_scratch_evidence_ok" -eq 1 ] || continue
        rm -rf "$broray_tx_control_scratch" 2>/dev/null || true
    done
    BRORAY_TX_WORK="$broray_tx_control_scratch_saved_work"
    broray_tx_rollback_failed_retired_lock_scavenge || return 1
    broray_tx_retired_work_scavenge || return 1
    return 0
}

broray_tx_control_mutex_acquire()
{
    [ "$BRORAY_TX_CONTROL_MUTEX_HELD" -eq 0 ] || return 1
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 0 ] || return 1
    [ -z "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK" ] || return 1
    [ -d "$BRORAY_TX_TMP_BASE" ] && [ ! -L "$BRORAY_TX_TMP_BASE" ] &&
        [ -w "$BRORAY_TX_TMP_BASE" ] || return 1
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ] &&
       [ -n "${BRORAY_TX_TEST_CONTROL_PROC_PROVIDER:-}" ]; then
        [ -x "$BRORAY_TX_TEST_CONTROL_PROC_PROVIDER" ] &&
            [ ! -L "$BRORAY_TX_TEST_CONTROL_PROC_PROVIDER" ] || return 1
        "$BRORAY_TX_TEST_CONTROL_PROC_PROVIDER" \
            "$BRORAY_TX_CONTROL_PROC_ROOT" "$$" \
            "$BRORAY_TX_TEST_CONTROL_PROCESS_NONCE" "${BRORAY_TX_ASH:-/opt/bin/ash}" || return 1
    fi
    broray_tx_control_mutex_start="$(broray_tx_proc_starttime "$BRORAY_TX_CONTROL_PROC_ROOT" "$$")" || return 1
    broray_tx_control_mutex_work="$BRORAY_TX_TMP_BASE/.broray-control-mutex-$$-$broray_tx_control_mutex_start"
    [ ! -e "$broray_tx_control_mutex_work" ] && [ ! -L "$broray_tx_control_mutex_work" ] || return 1
    mkdir "$broray_tx_control_mutex_work" || return 1
    chmod 700 "$broray_tx_control_mutex_work" 2>/dev/null || true
    mkdir "$broray_tx_control_mutex_work/evidence" || {
        rmdir "$broray_tx_control_mutex_work" 2>/dev/null || true
        return 1
    }
    chmod 700 "$broray_tx_control_mutex_work/evidence" 2>/dev/null || true
    broray_tx_control_owner_write_atomic "$broray_tx_control_mutex_work/owner-identity.tsv" || {
        rmdir "$broray_tx_control_mutex_work/evidence" 2>/dev/null || true
        rmdir "$broray_tx_control_mutex_work" 2>/dev/null || true
        return 1
    }
    BRORAY_TX_CONTROL_MUTEX_SAVED_OPERATION_ID="$BRORAY_TX_OPERATION_ID"
    BRORAY_TX_OPERATION_ID="control-mutex-$$-$broray_tx_control_mutex_start"
    BRORAY_TX_NATIVE_OPKG_LOCK_WORK="$broray_tx_control_mutex_work"
    if ! broray_tx_native_opkg_lock_acquire; then
        if [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ]; then
            broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || return 1
        fi
        BRORAY_TX_CONTROL_FAILURE_REASON="${BRORAY_TX_NATIVE_OPKG_ACQUIRE_REASON:-unclassified-native-lock-failure}"
        broray_tx_control_failure_id="r14c01-control-preflight-$$-$broray_tx_control_mutex_start"
        if broray_tx_native_opkg_diagnostic_publish "$broray_tx_control_mutex_work" FAIL \
            "$BRORAY_TX_CONTROL_FAILURE_REASON" "$broray_tx_control_failure_id" >/dev/null 2>&1
        then
            broray_tx_control_failure_published=1
        else
            broray_tx_control_failure_published=0
            BRORAY_TX_CONTROL_FAILURE_EVIDENCE="$broray_tx_control_mutex_work"
        fi
        BRORAY_TX_OPERATION_ID="$BRORAY_TX_CONTROL_MUTEX_SAVED_OPERATION_ID"
        BRORAY_TX_NATIVE_OPKG_LOCK_WORK=""
        if [ "$broray_tx_control_failure_published" -eq 1 ]; then
            rm -rf "$broray_tx_control_mutex_work" 2>/dev/null || true
        fi
        return 1
    fi
    broray_tx_control_mutex_stale_scratch_cleanup || {
        BRORAY_TX_OPERATION_ID="$BRORAY_TX_CONTROL_MUTEX_SAVED_OPERATION_ID"
        broray_tx_native_opkg_lock_acquire_abort >/dev/null 2>&1 || return 1
        BRORAY_TX_NATIVE_OPKG_LOCK_WORK=""
        rm -rf "$broray_tx_control_mutex_work" 2>/dev/null || return 1
        return 1
    }
    BRORAY_TX_OPERATION_ID="$BRORAY_TX_CONTROL_MUTEX_SAVED_OPERATION_ID"
    BRORAY_TX_CONTROL_MUTEX_WORK="$broray_tx_control_mutex_work"
    BRORAY_TX_CONTROL_MUTEX_OWNER_PID="$$"
    BRORAY_TX_CONTROL_MUTEX_OWNER_STARTTIME="$broray_tx_control_mutex_start"
    BRORAY_TX_CONTROL_MUTEX_HELD=1
}

broray_tx_control_mutex_assert()
{
    [ "$BRORAY_TX_CONTROL_MUTEX_HELD" -eq 1 ] || return 1
    [ "$BRORAY_TX_CONTROL_MUTEX_OWNER_PID" = "$$" ] || return 1
    [ "$(broray_tx_proc_starttime "$BRORAY_TX_CONTROL_PROC_ROOT" "$$" 2>/dev/null)" = \
        "$BRORAY_TX_CONTROL_MUTEX_OWNER_STARTTIME" ] || return 1
    [ -d "$BRORAY_TX_CONTROL_MUTEX_WORK" ] && [ ! -L "$BRORAY_TX_CONTROL_MUTEX_WORK" ] || return 1
    broray_tx_control_owner_classify "$BRORAY_TX_CONTROL_MUTEX_WORK/owner-identity.tsv" || return 1
    [ "$BRORAY_TX_CONTROL_OWNER_STATE" = live ] && [ "$BRORAY_TX_CONTROL_OWNER_PID" = "$$" ] || return 1
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK" = "$BRORAY_TX_CONTROL_MUTEX_WORK" ] || return 1
    broray_tx_native_opkg_lock_assert control-transition
}

broray_tx_control_mutex_release()
{
    [ "$BRORAY_TX_CONTROL_MUTEX_HELD" -eq 1 ] || return 0
    broray_tx_control_mutex_assert || return 1
    broray_tx_control_mutex_release_work="$BRORAY_TX_CONTROL_MUTEX_WORK"
    broray_tx_control_mutex_release_expected="$BRORAY_TX_TMP_BASE/.broray-control-mutex-$BRORAY_TX_CONTROL_MUTEX_OWNER_PID-$BRORAY_TX_CONTROL_MUTEX_OWNER_STARTTIME"
    [ "$broray_tx_control_mutex_release_work" = "$broray_tx_control_mutex_release_expected" ] || return 1
    broray_tx_native_opkg_lock_release || return 1
    [ "$(sed -n '1p' "$broray_tx_control_mutex_release_work/native-opkg-lock/state" 2>/dev/null)" = released ] || return 1
    rm -rf "$broray_tx_control_mutex_release_work" || return 1
    BRORAY_TX_CONTROL_MUTEX_HELD=0
    BRORAY_TX_CONTROL_MUTEX_WORK=""
    BRORAY_TX_CONTROL_MUTEX_OWNER_PID=""
    BRORAY_TX_CONTROL_MUTEX_OWNER_STARTTIME=""
    BRORAY_TX_CONTROL_MUTEX_SAVED_OPERATION_ID=""
}

# Failure while proving a just-acquired short mutex must not leave the shell's
# in-memory state looking nested/held.  This uses the acquire-abort EOF path,
# never a signal, and removes only the exact PID+birth-token scratch directory.
broray_tx_control_mutex_abort()
{
    [ "$BRORAY_TX_CONTROL_MUTEX_HELD" -eq 1 ] || return 0
    broray_tx_control_mutex_abort_work="$BRORAY_TX_CONTROL_MUTEX_WORK"
    broray_tx_control_mutex_abort_expected="$BRORAY_TX_TMP_BASE/.broray-control-mutex-$BRORAY_TX_CONTROL_MUTEX_OWNER_PID-$BRORAY_TX_CONTROL_MUTEX_OWNER_STARTTIME"
    [ "$broray_tx_control_mutex_abort_work" = "$broray_tx_control_mutex_abort_expected" ] || return 1
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_WORK" = "$broray_tx_control_mutex_abort_work" ] || return 1
    broray_tx_native_opkg_lock_acquire_abort || return 1
    rm -rf "$broray_tx_control_mutex_abort_work" || return 1
    BRORAY_TX_CONTROL_MUTEX_HELD=0
    BRORAY_TX_CONTROL_MUTEX_WORK=""
    BRORAY_TX_CONTROL_MUTEX_OWNER_PID=""
    BRORAY_TX_CONTROL_MUTEX_OWNER_STARTTIME=""
    BRORAY_TX_CONTROL_MUTEX_SAVED_OPERATION_ID=""
}

# Handoff during the lifecycle already owns the same proven native OPKG lock;
# control-only transitions own the dedicated short mutex above.  No metadata
# owner is ever changed without one of those two kernel-backed states.
broray_tx_control_transition_assert()
{
    if [ "$BRORAY_TX_CONTROL_MUTEX_HELD" -eq 1 ]; then
        broray_tx_control_mutex_assert
    else
        [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ] || return 1
        broray_tx_native_opkg_lock_assert control-transition
    fi
}

# Enter the single kernel-backed control transition domain.  A lifecycle that
# already owns the native OPKG lock reuses it; control-only work acquires a
# short native OPKG mutex.  The caller must always pair this with
# broray_tx_control_transition_end.
broray_tx_control_transition_begin()
{
    case "$BRORAY_TX_CONTROL_TRANSITION_DEPTH" in ''|*[!0-9]*) return 1 ;; esac
    if [ "$BRORAY_TX_CONTROL_TRANSITION_DEPTH" -gt 0 ]; then
        broray_tx_control_transition_assert || return 1
        BRORAY_TX_CONTROL_TRANSITION_DEPTH=$((BRORAY_TX_CONTROL_TRANSITION_DEPTH + 1))
        BRORAY_TX_CONTROL_TRANSITION_OWNS_MUTEX="$BRORAY_TX_CONTROL_TRANSITION_ROOT_OWNS_MUTEX"
        return 0
    fi
    BRORAY_TX_CONTROL_TRANSITION_ROOT_OWNS_MUTEX=0
    BRORAY_TX_CONTROL_TRANSITION_OWNS_MUTEX=0
    if [ "$BRORAY_TX_CONTROL_MUTEX_HELD" -eq 1 ] ||
       [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ]; then
        broray_tx_control_transition_assert || return 1
    else
        broray_tx_control_mutex_acquire || return 1
        BRORAY_TX_CONTROL_TRANSITION_ROOT_OWNS_MUTEX=1
        BRORAY_TX_CONTROL_TRANSITION_OWNS_MUTEX=1
        if ! broray_tx_control_transition_assert; then
            broray_tx_control_mutex_abort >/dev/null 2>&1 || true
            BRORAY_TX_CONTROL_TRANSITION_ROOT_OWNS_MUTEX=0
            BRORAY_TX_CONTROL_TRANSITION_OWNS_MUTEX=0
            return 1
        fi
    fi
    BRORAY_TX_CONTROL_TRANSITION_DEPTH=1
    if ! broray_tx_test_pause control-transition-mutex-acquired \
        "${BRORAY_TX_CONTROL_MUTEX_WORK:+$BRORAY_TX_CONTROL_MUTEX_WORK/evidence}"; then
        broray_tx_control_transition_end >/dev/null 2>&1 || true
        return 1
    fi
    return 0
}

broray_tx_control_transition_end()
{
    case "$BRORAY_TX_CONTROL_TRANSITION_DEPTH" in ''|*[!0-9]*|0) return 1 ;; esac
    broray_tx_control_transition_assert || return 1
    BRORAY_TX_CONTROL_TRANSITION_DEPTH=$((BRORAY_TX_CONTROL_TRANSITION_DEPTH - 1))
    if [ "$BRORAY_TX_CONTROL_TRANSITION_DEPTH" -gt 0 ]; then
        return 0
    fi
    broray_tx_control_transition_end_owned="$BRORAY_TX_CONTROL_TRANSITION_ROOT_OWNS_MUTEX"
    BRORAY_TX_CONTROL_TRANSITION_ROOT_OWNS_MUTEX=0
    BRORAY_TX_CONTROL_TRANSITION_OWNS_MUTEX=0
    if [ "$broray_tx_control_transition_end_owned" -eq 1 ]; then
        broray_tx_control_mutex_release || return 1
    fi
    return 0
}

# owner-identity.tsv is the only commit marker for transaction control.  Once
# the kernel mutex is held, an ownerless directory cannot belong to a live
# publisher.  Remove only the exact protocol filenames and only regular,
# bounded files; anything foreign remains fail-closed.
broray_tx_transaction_ownerless_residue_cleanup()
{
    broray_tx_control_transition_assert || return 1
    [ -d "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] || return 1
    [ ! -e "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ] &&
        [ ! -L "$BRORAY_TX_LOCK_DIR/owner-identity.tsv" ] || return 1
    for broray_tx_ownerless_entry in "$BRORAY_TX_LOCK_DIR"/* "$BRORAY_TX_LOCK_DIR"/.[!.]* "$BRORAY_TX_LOCK_DIR"/..?*; do
        [ -e "$broray_tx_ownerless_entry" ] || [ -L "$broray_tx_ownerless_entry" ] || continue
        [ -f "$broray_tx_ownerless_entry" ] && [ ! -L "$broray_tx_ownerless_entry" ] || return 1
        case "${broray_tx_ownerless_entry##*/}" in
            operation-id|operation-type|started-at|source-version|target-version) ;;
            *) return 1 ;;
        esac
        broray_tx_ownerless_bytes="$(wc -c <"$broray_tx_ownerless_entry" 2>/dev/null | tr -d ' ')" || return 1
        broray_tx_number "$broray_tx_ownerless_bytes" &&
            [ "$broray_tx_ownerless_bytes" -le "$BRORAY_TX_METADATA_MAX_BYTES" ] || return 1
    done
    for broray_tx_ownerless_name in operation-id operation-type started-at source-version target-version; do
        rm -f "$BRORAY_TX_LOCK_DIR/$broray_tx_ownerless_name" || return 1
    done
    rmdir "$BRORAY_TX_LOCK_DIR"
}

# A WebUI worker may start a package transaction while it owns the matching
# global operation.  Every other global state conflicts.  This check executes
# under the same kernel mutex as transaction publication, closing the former
# global-vs-transaction race.
broray_tx_global_control_relation_assert()
{
    broray_tx_control_transition_assert || return 1
    if [ ! -e "$BRORAY_TX_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_TX_GLOBAL_LOCK" ]; then
        return 0
    fi
    [ -d "$BRORAY_TX_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_TX_GLOBAL_LOCK" ] || return 1
    for broray_tx_global_entry in "$BRORAY_TX_GLOBAL_LOCK"/* "$BRORAY_TX_GLOBAL_LOCK"/.[!.]* "$BRORAY_TX_GLOBAL_LOCK"/..?*; do
        [ -e "$broray_tx_global_entry" ] || [ -L "$broray_tx_global_entry" ] || continue
        [ -f "$broray_tx_global_entry" ] && [ ! -L "$broray_tx_global_entry" ] || return 1
        case "${broray_tx_global_entry##*/}" in
            owner-identity.tsv|scope|action|bundle|startedAt|operation-id) ;;
            *) return 1 ;;
        esac
    done
    for broray_tx_global_required in owner-identity.tsv scope action bundle startedAt operation-id; do
        [ -f "$BRORAY_TX_GLOBAL_LOCK/$broray_tx_global_required" ] &&
            [ ! -L "$BRORAY_TX_GLOBAL_LOCK/$broray_tx_global_required" ] || return 1
    done
    [ "$(sed -n '1p' "$BRORAY_TX_GLOBAL_LOCK/scope")" = system ] || return 1
    [ "$(sed -n '1p' "$BRORAY_TX_GLOBAL_LOCK/operation-id")" = "$BRORAY_TX_OPERATION_ID" ] || return 1
    [ "$(sed -n '1p' "$BRORAY_TX_GLOBAL_LOCK/action")" = "$BRORAY_TX_MODE" ] || return 1
    broray_tx_control_owner_assert_self "$BRORAY_TX_GLOBAL_LOCK/owner-identity.tsv"
}

broray_tx_work_init()
{
    BRORAY_TX_OPERATION_ID="$1"
    BRORAY_TX_MODE="${2:-update}"
    broray_tx_valid_id "$BRORAY_TX_OPERATION_ID" || { broray_tx_reject_preworkspace invalid-operation-id; return 1; }
    case "$BRORAY_TX_MODE" in install|update|reinstall|restore|opkg-upgrade) ;; *) broray_tx_reject_preworkspace invalid-mode; return 1 ;; esac
    [ -d "$BRORAY_TX_TMP_BASE" ] && [ ! -L "$BRORAY_TX_TMP_BASE" ] && [ -w "$BRORAY_TX_TMP_BASE" ] || { broray_tx_reject_preworkspace tmp-base-unavailable; return 1; }
    broray_tx_lock_acquire
    broray_tx_lock_rc=$?
    case "$broray_tx_lock_rc" in
        0) ;;
        3) broray_tx_reject_preworkspace unsupported-source-identity; return 1 ;;
        *) broray_tx_reject_preworkspace conflicting-or-ambiguous-transaction-control; return 1 ;;
    esac
    # Workspace publication is a bounded control transition too.  The
    # transaction lock already names this live process; the native mutex now
    # covers every workspace atom through its owner-last commit.
    broray_tx_control_transition_begin || {
        broray_tx_lock_release 2>/dev/null || true
        broray_tx_reject_preworkspace cannot-fence-workspace-publication
        return 1
    }
    BRORAY_TX_WORK="$BRORAY_TX_TMP_BASE/broray-update-$BRORAY_TX_OPERATION_ID"
    [ ! -e "$BRORAY_TX_WORK" ] && [ ! -L "$BRORAY_TX_WORK" ] || {
        broray_tx_control_transition_end 2>/dev/null || true
        broray_tx_reject_preworkspace current-operation-workspace-exists
        return 1
    }
    mkdir -p "$BRORAY_TX_WORK/evidence" "$BRORAY_TX_WORK/snapshot-meta" || {
        broray_tx_control_transition_end 2>/dev/null || true
        broray_tx_reject_preworkspace cannot-create-current-workspace
        return 1
    }
    chmod 700 "$BRORAY_TX_WORK" "$BRORAY_TX_WORK/evidence" "$BRORAY_TX_WORK/snapshot-meta" 2>/dev/null || true
    if ! : >"$BRORAY_TX_WORK/evidence/events.tsv" ||
       ! printf '%s\n' "$BRORAY_TX_OPERATION_ID" >"$BRORAY_TX_WORK/operation-id" ||
       ! printf '%s\n' "$BRORAY_TX_MODE" >"$BRORAY_TX_WORK/mode" ||
       ! printf '%s\n' "$BRORAY_TX_ORIGIN" >"$BRORAY_TX_WORK/origin" ||
       ! printf '%s\n' "$BRORAY_TX_CONTRACT" >"$BRORAY_TX_WORK/lifecycle-contract"
    then
        broray_tx_control_transition_end 2>/dev/null || true
        return 1
    fi
    # The cleanup capability gate intentionally runs before jq is required.
    # This is a minimal control-prelude record: source admission, class and
    # migration selection are intentionally absent until cleanup-complete.
    broray_tx_started_at="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/started-at" 2>/dev/null)"
    for broray_tx_operation_atom in \
        "$BRORAY_TX_CONTRACT" "$BRORAY_TX_OPERATION_ID" "$BRORAY_TX_MODE" \
        "$BRORAY_TX_SOURCE_PACKAGE" "$BRORAY_TX_TARGET_PACKAGE" "$BRORAY_TX_WORK" "$broray_tx_started_at"
    do
        case "$broray_tx_operation_atom" in
            ''|*[!-0-9A-Za-z._/:+]*)
                broray_tx_control_transition_end 2>/dev/null || true
                broray_tx_fail unsafe-operation-metadata-atom
                return 1 ;;
        esac
    done
    printf '{"schemaVersion":2,"lifecycleContract":"%s","operationId":"%s","mode":"%s","sourceVersion":"%s","targetVersion":"%s","transactionPath":"%s","startedAt":"%s","mutationStarted":false}\n' \
        "$BRORAY_TX_CONTRACT" "$BRORAY_TX_OPERATION_ID" "$BRORAY_TX_MODE" \
        "$BRORAY_TX_SOURCE_PACKAGE" "$BRORAY_TX_TARGET_PACKAGE" "$BRORAY_TX_WORK" "$broray_tx_started_at" \
        >"$BRORAY_TX_WORK/operation.json" || {
            broray_tx_control_transition_end 2>/dev/null || true
            return 1
        }
    [ -e "$BRORAY_TX_STATE_ROOT" ] || mkdir -p "$BRORAY_TX_STATE_ROOT" || {
        broray_tx_control_transition_end 2>/dev/null || true
        return 1
    }
    [ -d "$BRORAY_TX_STATE_ROOT" ] && [ ! -L "$BRORAY_TX_STATE_ROOT" ] || {
        broray_tx_control_transition_end 2>/dev/null || true
        return 1
    }
    [ ! -e "$BRORAY_TX_LEGACY_MARKER" ] && [ ! -L "$BRORAY_TX_LEGACY_MARKER" ] || {
        broray_tx_control_transition_end 2>/dev/null || true
        return 1
    }
    printf '{"schemaVersion":2,"lifecycleContract":"%s","operationId":"%s","mode":"%s","sourceVersion":"%s","targetVersion":"%s","transactionPath":"%s","phase":"control-prelude","status":"active","startedAt":"%s","snapshotSha256":null}\n' \
        "$BRORAY_TX_CONTRACT" "$BRORAY_TX_OPERATION_ID" "$BRORAY_TX_MODE" \
        "$BRORAY_TX_SOURCE_PACKAGE" "$BRORAY_TX_TARGET_PACKAGE" "$BRORAY_TX_WORK" "$broray_tx_started_at" \
        >"$BRORAY_TX_LEGACY_MARKER.part" || {
            broray_tx_control_transition_end 2>/dev/null || true
            return 1
        }
    mv -f "$BRORAY_TX_LEGACY_MARKER.part" "$BRORAY_TX_LEGACY_MARKER" || {
        rm -f "$BRORAY_TX_LEGACY_MARKER.part"
        broray_tx_control_transition_end 2>/dev/null || true
        return 1
    }
    chmod 600 "$BRORAY_TX_LEGACY_MARKER" 2>/dev/null || true
    # The workspace identity is its commit marker.  Every other minimal
    # prelude atom, including operation.json and the durable phase marker, is
    # visible first.  A crash before this rename is an exact ownerless prelude
    # and is retired under the kernel control fence by restart recovery.
    broray_tx_control_owner_write_atomic "$BRORAY_TX_WORK/owner-identity.tsv" || {
        broray_tx_control_transition_end 2>/dev/null || true
        broray_tx_reject_preworkspace cannot-capture-workspace-owner-identity
        return 1
    }
    broray_tx_control_transition_end || {
        return 1
    }
    # Do not require jq or any later-stage capability before CLEANUP.  The
    # cleanup stage performs its minimal behavior probe and writes the first
    # status only after cleanup has completed successfully.
    broray_tx_trap_enable
    return 0
}

broray_tx_cleanup_workspace()
{
    broray_tx_cleanup_path="$1"
    [ "$broray_tx_cleanup_path" != "$BRORAY_TX_WORK" ] || return 0
    [ -d "$broray_tx_cleanup_path" ] && [ ! -L "$broray_tx_cleanup_path" ] || return 0
    [ -f "$broray_tx_cleanup_path/.broray-disposable" ] && [ ! -L "$broray_tx_cleanup_path/.broray-disposable" ] || return 0
    [ "$(sed -n '1p' "$broray_tx_cleanup_path/.broray-disposable")" = yes ] || return 0
    case "$broray_tx_cleanup_path" in
        "$BRORAY_TX_TMP_BASE"/broray-update-*|"$BRORAY_TX_TMP_BASE"/broray-verify-*|\
        "$BRORAY_TX_TMP_BASE"/broray-field-gate-*|"$BRORAY_TX_TMP_BASE"/broray-physical-gate-*) ;;
        *) return 1 ;;
    esac
    printf '%s\n' "$broray_tx_cleanup_path" >>"$BRORAY_TX_WORK/evidence/cleanup-removed.txt"
    rm -rf "$broray_tx_cleanup_path"
}

broray_tx_cleanup_file()
{
    broray_tx_cleanup_path="$1"
    [ -e "$broray_tx_cleanup_path" ] || [ -L "$broray_tx_cleanup_path" ] || return 0
    [ -f "$broray_tx_cleanup_path" ] && [ ! -L "$broray_tx_cleanup_path" ] || return 1
    case "$broray_tx_cleanup_path" in
        "$BRORAY_TX_APP_ROOT"/update/*.part|"$BRORAY_TX_APP_ROOT"/tmp/*.part|\
        "$BRORAY_TX_APP_ROOT"/tmp/transactions/*.part|"$BRORAY_TX_APP_ROOT"/routes/tmp/*.part|\
        "$BRORAY_TX_APP_ROOT"/routes/transactions/*.part|"$BRORAY_TX_TMP_BASE"/broray-candidate-*.part|\
        "$BRORAY_TX_TMP_BASE"/broray-download-*.part) ;;
        *) return 1 ;;
    esac
    printf '%s\n' "$broray_tx_cleanup_path" >>"$BRORAY_TX_WORK/evidence/cleanup-removed.txt"
    rm -f "$broray_tx_cleanup_path"
}

# These names are written only by BROray/Xray/lighttpd runtime components.
# A file is eligible only by exact basename (or a numeric gzip rotation of
# that basename), regular-file type, and containment in the real logs dir.
# Unknown objects in the same directory are deliberately preserved.
broray_tx_cleanup_runtime_logs()
{
    broray_tx_logs_dir="$BRORAY_TX_APP_ROOT/logs"
    [ -e "$broray_tx_logs_dir" ] || [ -L "$broray_tx_logs_dir" ] || return 0
    [ -d "$broray_tx_logs_dir" ] && [ ! -L "$broray_tx_logs_dir" ] || broray_tx_fail runtime-logs-directory-unsafe || return 1
    for broray_tx_log_path in "$broray_tx_logs_dir"/* "$broray_tx_logs_dir"/.[!.]* "$broray_tx_logs_dir"/..?*; do
        [ -e "$broray_tx_log_path" ] || [ -L "$broray_tx_log_path" ] || continue
        broray_tx_log_name="${broray_tx_log_path##*/}"
        broray_tx_log_class=""
        for broray_tx_log_base in \
            access.log error.log lighttpd-error.log connection-monitor.log \
            subscriptions.log server-auto-switch.log subscription-scheduler.log \
            update-last.log package-setup.log
        do
            if [ "$broray_tx_log_name" = "$broray_tx_log_base" ]; then
                broray_tx_log_class=runtime-log
                break
            fi
            case "$broray_tx_log_name" in
                "$broray_tx_log_base".*.gz)
                    broray_tx_log_rotation="${broray_tx_log_name#"$broray_tx_log_base".}"
                    broray_tx_log_rotation="${broray_tx_log_rotation%.gz}"
                    case "$broray_tx_log_rotation" in ''|*[!0-9]*) ;; *) broray_tx_log_class=runtime-log-rotation ;; esac
                    ;;
            esac
            [ -z "$broray_tx_log_class" ] || break
        done
        [ -z "$broray_tx_log_class" ] || {
            [ -f "$broray_tx_log_path" ] && [ ! -L "$broray_tx_log_path" ] || broray_tx_fail classified-runtime-log-not-regular || return 1
            broray_tx_cleanup_kb="$(du -sk "$broray_tx_log_path" 2>/dev/null | awk 'NR==1{print $1;exit}')"
            broray_tx_number "$broray_tx_cleanup_kb" || broray_tx_cleanup_kb=0
            printf '%s\t%s\t%s\n' "$broray_tx_log_class" "$broray_tx_cleanup_kb" "$broray_tx_log_path" >>"$BRORAY_TX_WORK/evidence/cleanup-removed.txt" || return 1
            rm -f "$broray_tx_log_path" || return 1
        }
    done
}

# These are application-declared temporary roots, never user-data roots.
# Containment and object type are validated before recursive removal.  Runtime
# state under run/, route catalog/state, subscriptions, servers and config are
# intentionally not members of this classification.
broray_tx_cleanup_managed_temp_dir()
{
    broray_tx_temp_dir="$1"
    case "$broray_tx_temp_dir" in
        "$BRORAY_TX_APP_ROOT/tmp"|"$BRORAY_TX_APP_ROOT/update"|"$BRORAY_TX_APP_ROOT/routes/tmp") ;;
        *) return 1 ;;
    esac
    [ -e "$broray_tx_temp_dir" ] || [ -L "$broray_tx_temp_dir" ] || return 0
    [ -d "$broray_tx_temp_dir" ] && [ ! -L "$broray_tx_temp_dir" ] || broray_tx_fail managed-temp-directory-unsafe || return 1
    for broray_tx_temp_path in "$broray_tx_temp_dir"/* "$broray_tx_temp_dir"/.[!.]* "$broray_tx_temp_dir"/..?*; do
        [ -e "$broray_tx_temp_path" ] || [ -L "$broray_tx_temp_path" ] || continue
        case "$broray_tx_temp_path" in "$broray_tx_temp_dir"/*) ;; *) return 1 ;; esac
        broray_tx_cleanup_kb="$(du -sk "$broray_tx_temp_path" 2>/dev/null | awk 'NR==1{print $1;exit}')"
        broray_tx_number "$broray_tx_cleanup_kb" || broray_tx_cleanup_kb=0
        printf 'managed-temp\t%s\t%s\n' "$broray_tx_cleanup_kb" "$broray_tx_temp_path" >>"$BRORAY_TX_WORK/evidence/cleanup-removed.txt" || return 1
        rm -rf "$broray_tx_temp_path" || return 1
    done
}

# CLEANUP is the first managed mutation.  Only after it is complete may the
# transaction classify the arbitrary structural source and select the one
# generic preservation migration.  Expand the minimal operation record
# atomically, then publish source-selection evidence.
broray_tx_source_admit_after_cleanup()
{
    [ -f "$BRORAY_TX_LOCK_DIR/source-version" ] && [ ! -L "$BRORAY_TX_LOCK_DIR/source-version" ] || return 1
    broray_tx_prelude_source="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/source-version")"
    broray_tx_source_admit || { broray_tx_fail unsupported-source-identity; return 1; }
    [ "$BRORAY_TX_SOURCE_PACKAGE" = "$broray_tx_prelude_source" ] || {
        broray_tx_fail source-identity-drift-during-cleanup
        return 1
    }
    case "$BRORAY_TX_MODE:$BRORAY_TX_SOURCE_CLASS" in
        install:absent) ;;
        update:bro-any-structural|reinstall:bro-any-structural|restore:bro-any-structural|opkg-upgrade:bro-any-structural) ;;
        *) broray_tx_fail invalid-mode-source-relation; return 1 ;;
    esac
    broray_tx_started_at="$(sed -n '1p' "$BRORAY_TX_LOCK_DIR/started-at" 2>/dev/null)"
    for broray_tx_operation_atom in \
        "$BRORAY_TX_CONTRACT" "$BRORAY_TX_OPERATION_ID" "$BRORAY_TX_MODE" \
        "$BRORAY_TX_SOURCE_PACKAGE" "$BRORAY_TX_SOURCE_APP" "$BRORAY_TX_SOURCE_CLASS" \
        "$BRORAY_TX_MIGRATION_ID" "$BRORAY_TX_TARGET_PACKAGE" "$BRORAY_TX_WORK" "$broray_tx_started_at"
    do
        case "$broray_tx_operation_atom" in
            ''|*[!-0-9A-Za-z._/:+]*) broray_tx_fail unsafe-operation-metadata-atom; return 1 ;;
        esac
    done
    printf '{"schemaVersion":2,"lifecycleContract":"%s","operationId":"%s","mode":"%s","sourceVersion":"%s","sourceAppVersion":"%s","sourceClass":"%s","migrationId":"%s","targetVersion":"%s","transactionPath":"%s","startedAt":"%s","mutationStarted":false}\n' \
        "$BRORAY_TX_CONTRACT" "$BRORAY_TX_OPERATION_ID" "$BRORAY_TX_MODE" \
        "$BRORAY_TX_SOURCE_PACKAGE" "$BRORAY_TX_SOURCE_APP" "$BRORAY_TX_SOURCE_CLASS" \
        "$BRORAY_TX_MIGRATION_ID" "$BRORAY_TX_TARGET_PACKAGE" "$BRORAY_TX_WORK" "$broray_tx_started_at" \
        >"$BRORAY_TX_WORK/operation.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/operation.json.part" "$BRORAY_TX_WORK/operation.json" || return 1
    cp -p "$BRORAY_TX_WORK/operation.json" "$BRORAY_TX_WORK/evidence/source-selection.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/evidence/source-selection.json.part" \
        "$BRORAY_TX_WORK/evidence/source-selection.json" || return 1
    broray_tx_event source-admission || return 1
}

broray_tx_cleanup_object_type()
{
    if [ -L "$1" ]; then printf '%s\n' symlink
    elif [ -f "$1" ]; then printf '%s\n' regular
    elif [ -d "$1" ]; then printf '%s\n' directory
    else return 1
    fi
}

broray_tx_cleanup_plan_add()
{
    broray_tx_cleanup_class="$1"
    broray_tx_cleanup_reason="$2"
    broray_tx_cleanup_proof="$3"
    broray_tx_cleanup_path="$4"
    [ -e "$broray_tx_cleanup_path" ] || [ -L "$broray_tx_cleanup_path" ] || return 0
    # A line-oriented durable plan cannot safely represent control characters;
    # reject such names before any delete instead of escaping ambiguously.
    if printf '%s' "$broray_tx_cleanup_path" | LC_ALL=C grep -q '[[:cntrl:]]'; then return 1; fi
    broray_tx_cleanup_type="$(broray_tx_cleanup_object_type "$broray_tx_cleanup_path")" || return 1
    broray_tx_cleanup_size_kb="$(du -sk "$broray_tx_cleanup_path" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_cleanup_size_kb" || return 1
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$broray_tx_cleanup_path" "$broray_tx_cleanup_type" "$broray_tx_cleanup_size_kb" \
        "$broray_tx_cleanup_class" "$broray_tx_cleanup_reason" "$broray_tx_cleanup_proof" \
        >>"$BRORAY_TX_WORK/evidence/cleanup-plan.raw" || return 1
    broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/cleanup-plan.raw" "$BRORAY_TX_CLEANUP_PLAN_CAP_KB"
}

broray_tx_cleanup_runtime_log_name()
{
    broray_tx_cleanup_log_name="$1"
    for broray_tx_cleanup_log_base in \
        access.log error.log lighttpd-error.log connection-monitor.log \
        subscriptions.log server-auto-switch.log subscription-scheduler.log \
        update-last.log package-setup.log
    do
        [ "$broray_tx_cleanup_log_name" = "$broray_tx_cleanup_log_base" ] && return 0
        case "$broray_tx_cleanup_log_name" in
            "$broray_tx_cleanup_log_base".*.gz)
                broray_tx_cleanup_log_rotation="${broray_tx_cleanup_log_name#"$broray_tx_cleanup_log_base".}"
                broray_tx_cleanup_log_rotation="${broray_tx_cleanup_log_rotation%.gz}"
                case "$broray_tx_cleanup_log_rotation" in ''|*[!0-9]*) ;; *) return 0 ;; esac
                ;;
        esac
    done
    return 1
}

broray_tx_cleanup_plan_validate_row()
{
    broray_tx_cleanup_path="$1"
    broray_tx_cleanup_type="$2"
    broray_tx_cleanup_size_kb="$3"
    broray_tx_cleanup_class="$4"
    broray_tx_cleanup_reason="$5"
    broray_tx_cleanup_proof="$6"
    broray_tx_number "$broray_tx_cleanup_size_kb" || return 1
    [ -e "$broray_tx_cleanup_path" ] || [ -L "$broray_tx_cleanup_path" ] || return 1
    [ "$(broray_tx_cleanup_object_type "$broray_tx_cleanup_path")" = "$broray_tx_cleanup_type" ] || return 1
    [ "$(du -sk "$broray_tx_cleanup_path" 2>/dev/null | awk 'NR==1{print $1;exit}')" = "$broray_tx_cleanup_size_kb" ] || return 1
    case "$broray_tx_cleanup_class:$broray_tx_cleanup_reason:$broray_tx_cleanup_proof" in
        exact-part:incomplete-owned-write:exact-part-allowlist)
            [ "$broray_tx_cleanup_type" = regular ] || return 1
            case "$broray_tx_cleanup_path" in
                "$BRORAY_TX_APP_ROOT"/routes/transactions/*.part|\
                "$BRORAY_TX_TMP_BASE"/broray-candidate-*.part|\
                "$BRORAY_TX_TMP_BASE"/broray-download-*.part) ;;
                *) return 1 ;;
            esac
            ;;
        disposable-workspace:completed-owned-workspace:disposable-marker-and-tmp-prefix)
            [ "$broray_tx_cleanup_type" = directory ] && [ "$broray_tx_cleanup_path" != "$BRORAY_TX_WORK" ] || return 1
            case "$broray_tx_cleanup_path" in
                "$BRORAY_TX_TMP_BASE"/broray-update-*|"$BRORAY_TX_TMP_BASE"/broray-verify-*|\
                "$BRORAY_TX_TMP_BASE"/broray-field-gate-*|"$BRORAY_TX_TMP_BASE"/broray-physical-gate-*) ;;
                *) return 1 ;;
            esac
            [ -f "$broray_tx_cleanup_path/.broray-disposable" ] &&
                [ ! -L "$broray_tx_cleanup_path/.broray-disposable" ] &&
                [ "$(sed -n '1p' "$broray_tx_cleanup_path/.broray-disposable")" = yes ] || return 1
            ;;
        runtime-log:bounded-runtime-log:exact-runtime-log-basename)
            [ "$broray_tx_cleanup_type" = regular ] || return 1
            case "$broray_tx_cleanup_path" in "$BRORAY_TX_APP_ROOT"/logs/*) ;; *) return 1 ;; esac
            broray_tx_cleanup_runtime_log_name "${broray_tx_cleanup_path##*/}" || return 1
            ;;
        managed-temp:declared-temporary-object:declared-root-direct-child)
            broray_tx_cleanup_parent="${broray_tx_cleanup_path%/*}"
            case "$broray_tx_cleanup_parent" in
                "$BRORAY_TX_APP_ROOT/tmp"|"$BRORAY_TX_APP_ROOT/update"|"$BRORAY_TX_APP_ROOT/routes/tmp") ;;
                *) return 1 ;;
            esac
            ;;
        *) return 1 ;;
    esac
}

broray_tx_cleanup_plan_validate()
{
    broray_tx_cleanup_tab="$(printf '\t')"
    broray_tx_cleanup_rows=0
    while IFS="$broray_tx_cleanup_tab" read -r broray_tx_cleanup_path broray_tx_cleanup_type \
        broray_tx_cleanup_size_kb broray_tx_cleanup_class broray_tx_cleanup_reason broray_tx_cleanup_proof
    do
        [ -n "$broray_tx_cleanup_path" ] || continue
        broray_tx_cleanup_plan_validate_row "$broray_tx_cleanup_path" "$broray_tx_cleanup_type" \
            "$broray_tx_cleanup_size_kb" "$broray_tx_cleanup_class" \
            "$broray_tx_cleanup_reason" "$broray_tx_cleanup_proof" || return 1
        broray_tx_cleanup_rows=$((broray_tx_cleanup_rows + 1))
    done <"$BRORAY_TX_WORK/evidence/cleanup-plan.txt"
    [ "$broray_tx_cleanup_rows" -eq "$(wc -l <"$BRORAY_TX_WORK/evidence/cleanup-plan.txt" | tr -d ' ')" ]
}

broray_tx_cleanup_plan_build()
{
    : >"$BRORAY_TX_WORK/evidence/cleanup-plan.raw" || return 1
    # Reserved handoff control is classified by recovery before lock creation;
    # cleanup is never authorized to delete it by pathname alone.
    [ ! -e "$BRORAY_TX_CURRENT" ] && [ ! -L "$BRORAY_TX_CURRENT" ] || return 1
    for broray_tx_cleanup_item in \
        "$BRORAY_TX_APP_ROOT"/routes/transactions/*.part \
        "$BRORAY_TX_TMP_BASE"/broray-candidate-*.part "$BRORAY_TX_TMP_BASE"/broray-download-*.part
    do
        [ -e "$broray_tx_cleanup_item" ] || [ -L "$broray_tx_cleanup_item" ] || continue
        broray_tx_cleanup_plan_add exact-part incomplete-owned-write exact-part-allowlist "$broray_tx_cleanup_item" || return 1
    done
    for broray_tx_cleanup_item in \
        "$BRORAY_TX_TMP_BASE"/broray-update-* "$BRORAY_TX_TMP_BASE"/broray-verify-* \
        "$BRORAY_TX_TMP_BASE"/broray-field-gate-* "$BRORAY_TX_TMP_BASE"/broray-physical-gate-*
    do
        [ "$broray_tx_cleanup_item" != "$BRORAY_TX_WORK" ] || continue
        [ -d "$broray_tx_cleanup_item" ] && [ ! -L "$broray_tx_cleanup_item" ] || continue
        [ -f "$broray_tx_cleanup_item/.broray-disposable" ] &&
            [ ! -L "$broray_tx_cleanup_item/.broray-disposable" ] &&
            [ "$(sed -n '1p' "$broray_tx_cleanup_item/.broray-disposable")" = yes ] || continue
        broray_tx_cleanup_plan_add disposable-workspace completed-owned-workspace \
            disposable-marker-and-tmp-prefix "$broray_tx_cleanup_item" || return 1
    done
    broray_tx_cleanup_logs="$BRORAY_TX_APP_ROOT/logs"
    if [ -e "$broray_tx_cleanup_logs" ] || [ -L "$broray_tx_cleanup_logs" ]; then
        [ -d "$broray_tx_cleanup_logs" ] && [ ! -L "$broray_tx_cleanup_logs" ] || return 1
        for broray_tx_cleanup_item in "$broray_tx_cleanup_logs"/* "$broray_tx_cleanup_logs"/.[!.]* "$broray_tx_cleanup_logs"/..?*; do
            [ -e "$broray_tx_cleanup_item" ] || [ -L "$broray_tx_cleanup_item" ] || continue
            broray_tx_cleanup_runtime_log_name "${broray_tx_cleanup_item##*/}" || continue
            broray_tx_cleanup_plan_add runtime-log bounded-runtime-log exact-runtime-log-basename "$broray_tx_cleanup_item" || return 1
        done
    fi
    for broray_tx_cleanup_root in "$BRORAY_TX_APP_ROOT/tmp" "$BRORAY_TX_APP_ROOT/update" "$BRORAY_TX_APP_ROOT/routes/tmp"; do
        [ -e "$broray_tx_cleanup_root" ] || [ -L "$broray_tx_cleanup_root" ] || continue
        [ -d "$broray_tx_cleanup_root" ] && [ ! -L "$broray_tx_cleanup_root" ] || return 1
        for broray_tx_cleanup_item in "$broray_tx_cleanup_root"/* "$broray_tx_cleanup_root"/.[!.]* "$broray_tx_cleanup_root"/..?*; do
            [ -e "$broray_tx_cleanup_item" ] || [ -L "$broray_tx_cleanup_item" ] || continue
            broray_tx_cleanup_plan_add managed-temp declared-temporary-object declared-root-direct-child "$broray_tx_cleanup_item" || return 1
        done
    done
    LC_ALL=C sort "$BRORAY_TX_WORK/evidence/cleanup-plan.raw" \
        >"$BRORAY_TX_WORK/evidence/cleanup-plan.txt.part" || return 1
    awk -F '\t' 'seen[$1]++{exit 1} NF!=6{exit 1}' "$BRORAY_TX_WORK/evidence/cleanup-plan.txt.part" || return 1
    mv -f "$BRORAY_TX_WORK/evidence/cleanup-plan.txt.part" "$BRORAY_TX_WORK/evidence/cleanup-plan.txt" || return 1
    rm -f "$BRORAY_TX_WORK/evidence/cleanup-plan.raw"
    jq -Rn '[inputs | split("\t") |
      {path:.[0],type:.[1],sizeKB:(.[2]|tonumber),cleanupClass:.[3],reason:.[4],ownershipProof:.[5]}]' \
        <"$BRORAY_TX_WORK/evidence/cleanup-plan.txt" >"$BRORAY_TX_WORK/evidence/cleanup-plan.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/evidence/cleanup-plan.json.part" "$BRORAY_TX_WORK/evidence/cleanup-plan.json" || return 1
    broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/cleanup-plan.txt" "$BRORAY_TX_CLEANUP_PLAN_CAP_KB" &&
        broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/cleanup-plan.json" "$BRORAY_TX_CLEANUP_PLAN_CAP_KB" &&
        broray_tx_cleanup_plan_validate
}

broray_tx_cleanup_plan_execute()
{
    : >"$BRORAY_TX_WORK/evidence/cleanup-actions.txt" || return 1
    broray_tx_cleanup_tab="$(printf '\t')"
    while IFS="$broray_tx_cleanup_tab" read -r broray_tx_cleanup_path broray_tx_cleanup_type \
        broray_tx_cleanup_size_kb broray_tx_cleanup_class broray_tx_cleanup_reason broray_tx_cleanup_proof
    do
        [ -n "$broray_tx_cleanup_path" ] || continue
        broray_tx_cleanup_plan_validate_row "$broray_tx_cleanup_path" "$broray_tx_cleanup_type" \
            "$broray_tx_cleanup_size_kb" "$broray_tx_cleanup_class" \
            "$broray_tx_cleanup_reason" "$broray_tx_cleanup_proof" || return 1
        case "$broray_tx_cleanup_type" in
            directory) rm -rf "$broray_tx_cleanup_path" || return 1 ;;
            regular|symlink) rm -f "$broray_tx_cleanup_path" || return 1 ;;
            *) return 1 ;;
        esac
        [ ! -e "$broray_tx_cleanup_path" ] && [ ! -L "$broray_tx_cleanup_path" ] || return 1
        printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
            "$broray_tx_cleanup_path" "$broray_tx_cleanup_type" "$broray_tx_cleanup_size_kb" \
            "$broray_tx_cleanup_class" "$broray_tx_cleanup_reason" "$broray_tx_cleanup_proof" \
            >>"$BRORAY_TX_WORK/evidence/cleanup-actions.txt" || return 1
        broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/cleanup-actions.txt" "$BRORAY_TX_CLEANUP_PLAN_CAP_KB" || return 1
        broray_tx_inject cleanup-action || return 1
        broray_tx_test_pause cleanup-action || return 1
    done <"$BRORAY_TX_WORK/evidence/cleanup-plan.txt"
    broray_tx_files_equal "$BRORAY_TX_WORK/evidence/cleanup-plan.txt" \
        "$BRORAY_TX_WORK/evidence/cleanup-actions.txt"
}

broray_tx_cleanup()
{
    broray_tx_event cleanup || return 1
    broray_runtime_probe_cleanup "$BRORAY_TX_WORK" || {
        broray_tx_fail "cleanup-capability-failed:${BRORAY_RUNTIME_FAILURE_ID:-unknown}"
        return 1
    }
    broray_tx_cleanup_before_kb=0
    if [ -d "$BRORAY_TX_APP_ROOT" ] && [ ! -L "$BRORAY_TX_APP_ROOT" ]; then
        broray_tx_cleanup_before_kb="$(du -sk "$BRORAY_TX_APP_ROOT" 2>/dev/null | awk 'NR==1{print $1;exit}')"
        broray_tx_number "$broray_tx_cleanup_before_kb" || broray_tx_cleanup_before_kb=0
    fi
    # The complete immutable plan is committed and revalidated before the
    # first delete.  Execution consumes that exact ordered row set only.
    broray_tx_cleanup_plan_build || { broray_tx_fail cleanup-plan-invalid; return 1; }
    broray_tx_cleanup_plan_execute || { [ -n "$BRORAY_TX_REASON" ] || broray_tx_fail cleanup-action-failed; return 1; }
    broray_tx_cleanup_after_kb=0
    if [ -d "$BRORAY_TX_APP_ROOT" ] && [ ! -L "$BRORAY_TX_APP_ROOT" ]; then
        broray_tx_cleanup_after_kb="$(du -sk "$BRORAY_TX_APP_ROOT" 2>/dev/null | awk 'NR==1{print $1;exit}')"
        broray_tx_number "$broray_tx_cleanup_after_kb" || broray_tx_cleanup_after_kb=0
    fi
    broray_tx_cleanup_reclaimed_kb=$((broray_tx_cleanup_before_kb - broray_tx_cleanup_after_kb))
    [ "$broray_tx_cleanup_reclaimed_kb" -ge 0 ] || broray_tx_cleanup_reclaimed_kb=0
    broray_tx_cleanup_count="$(wc -l <"$BRORAY_TX_WORK/evidence/cleanup-actions.txt" | tr -d ' ')"
    broray_tx_number "$broray_tx_cleanup_count" || broray_tx_cleanup_count=0
    jq -nc --argjson appBeforeKB "$broray_tx_cleanup_before_kb" --argjson appAfterKB "$broray_tx_cleanup_after_kb" \
        --argjson reclaimedKB "$broray_tx_cleanup_reclaimed_kb" --argjson removedObjectCount "$broray_tx_cleanup_count" \
        --arg planSha256 "$(broray_tx_sha "$BRORAY_TX_WORK/evidence/cleanup-plan.txt")" \
        --arg actionsSha256 "$(broray_tx_sha "$BRORAY_TX_WORK/evidence/cleanup-actions.txt")" '
        {schemaVersion:2,classification:"exact-known-runtime+managed-temp-roots/1",versionDependent:false,
          appBeforeKB:$appBeforeKB,appAfterKB:$appAfterKB,reclaimedKB:$reclaimedKB,removedObjectCount:$removedObjectCount,
          planPath:"evidence/cleanup-plan.json",actionsPath:"evidence/cleanup-actions.txt",
          planSha256:$planSha256,actionsSha256:$actionsSha256,planEqualsActions:($planSha256==$actionsSha256),
          protectedRoots:["config","subscriptions","servers","routes/user-state","external-keenetic-managed-proxy"],
          unknownObjectPolicy:"preserve-unless-contained-in-declared-managed-temp-root"}' \
        >"$BRORAY_TX_WORK/evidence/cleanup.json" || return 1
    jq -e '.planEqualsActions==true' "$BRORAY_TX_WORK/evidence/cleanup.json" >/dev/null 2>&1 || return 1
    broray_tx_inject cleanup || return 1
    broray_tx_event cleanup-complete || return 1
    broray_tx_source_admit_after_cleanup || return 1
    broray_tx_status running ''
}

broray_tx_external_capture()
{
    broray_tx_external="$BRORAY_TX_WORK/snapshot-meta/keenetic-running-before.txt"
    broray_tx_external_part="$broray_tx_external.part"
    rm -f "$broray_tx_external" "$broray_tx_external_part" || return 1
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ -n "${BRORAY_TX_TEST_KEENETIC_STATE:-}" ]; then
        cp -p "$BRORAY_TX_TEST_KEENETIC_STATE" "$broray_tx_external_part" || return 1
    else
        command -v ndmc >/dev/null 2>&1 || return 1
        ndmc -c 'show running-config' >"$broray_tx_external_part" \
            2>"$BRORAY_TX_WORK/evidence/keenetic-capture.stderr" || {
                rm -f "$broray_tx_external_part"
                return 1
            }
    fi
    [ -s "$broray_tx_external_part" ] && [ ! -L "$broray_tx_external_part" ] || {
        rm -f "$broray_tx_external_part"
        return 1
    }
    mv -f "$broray_tx_external_part" "$broray_tx_external" || return 1
    [ -f "$broray_tx_external" ] && [ ! -L "$broray_tx_external" ]
}

broray_tx_scope_add()
{
    broray_tx_scope_abs="$1"
    [ -e "$broray_tx_scope_abs" ] || [ -L "$broray_tx_scope_abs" ] || return 0
    broray_tx_scope_rel="$(broray_tx_to_relative "$broray_tx_scope_abs")" || return 1
    broray_tx_relative_safe "$broray_tx_scope_rel" || return 1
    grep -Fqx "$broray_tx_scope_rel" "$BRORAY_TX_WORK/source-scope.list" 2>/dev/null && return 0
    printf '%s\n' "$broray_tx_scope_rel" >>"$BRORAY_TX_WORK/source-scope.list"
}

broray_tx_scope_canonicalize()
{
    LC_ALL=C sort -u "$BRORAY_TX_WORK/source-scope.list" >"$BRORAY_TX_WORK/source-scope.sorted" || return 1
    : >"$BRORAY_TX_WORK/source-scope.canonical" || return 1
    while IFS= read -r broray_tx_scope_rel; do
        [ -n "$broray_tx_scope_rel" ] || continue
        broray_tx_scope_covered=0
        while IFS= read -r broray_tx_scope_parent; do
            [ -n "$broray_tx_scope_parent" ] || continue
            case "$broray_tx_scope_rel" in
                "$broray_tx_scope_parent"/*)
                    broray_tx_scope_parent_abs="$(broray_tx_root_path "/$broray_tx_scope_parent")"
                    if [ -d "$broray_tx_scope_parent_abs" ] && [ ! -L "$broray_tx_scope_parent_abs" ]; then
                        broray_tx_scope_covered=1
                        break
                    fi
                    ;;
            esac
        done <"$BRORAY_TX_WORK/source-scope.canonical"
        [ "$broray_tx_scope_covered" -eq 1 ] || printf '%s\n' "$broray_tx_scope_rel" >>"$BRORAY_TX_WORK/source-scope.canonical"
    done <"$BRORAY_TX_WORK/source-scope.sorted"
    mv -f "$BRORAY_TX_WORK/source-scope.canonical" "$BRORAY_TX_WORK/source-scope.list" || return 1
    rm -f "$BRORAY_TX_WORK/source-scope.sorted"
    broray_tx_scope_lines="$(wc -l <"$BRORAY_TX_WORK/source-scope.list" | tr -d ' ')"
    broray_tx_scope_unique="$(LC_ALL=C sort -u "$BRORAY_TX_WORK/source-scope.list" | wc -l | tr -d ' ')"
    broray_tx_number "$broray_tx_scope_lines" && broray_tx_number "$broray_tx_scope_unique" || return 1
    [ "$broray_tx_scope_lines" -eq "$broray_tx_scope_unique" ] || return 1
    printf '%s\n' 0 >"$BRORAY_TX_WORK/source-scope.duplicates"
}

# An unregistered but structurally intact source has no OPKG broray.list.
# Discover external objects only inside version-independent BROray namespaces;
# unrelated neighbours remain outside the delete/snapshot scope.
broray_tx_scope_add_broray_named_children()
{
    broray_tx_scope_named_dir="$1"
    [ -e "$broray_tx_scope_named_dir" ] || [ -L "$broray_tx_scope_named_dir" ] || return 0
    [ -d "$broray_tx_scope_named_dir" ] && [ ! -L "$broray_tx_scope_named_dir" ] || return 1
    for broray_tx_scope_named_item in \
        "$broray_tx_scope_named_dir"/* \
        "$broray_tx_scope_named_dir"/.[!.]* \
        "$broray_tx_scope_named_dir"/..?*
    do
        [ -e "$broray_tx_scope_named_item" ] || [ -L "$broray_tx_scope_named_item" ] || continue
        broray_tx_scope_named_base="${broray_tx_scope_named_item##*/}"
        broray_tx_scope_named_lower="$(printf '%s' "$broray_tx_scope_named_base" | tr 'A-Z' 'a-z')" || return 1
        case "$broray_tx_scope_named_lower" in
            *broray*) broray_tx_scope_add "$broray_tx_scope_named_item" || return 1 ;;
        esac
    done
}

broray_tx_scope_build()
{
    : >"$BRORAY_TX_WORK/source-scope.list" || return 1
    broray_tx_external_capture || broray_tx_fail external-state-capture-failed || return 1
    broray_tx_service_state_capture "$BRORAY_TX_WORK/services-before.tsv" \
        "$BRORAY_TX_WORK/services-running-before.list" || broray_tx_fail service-baseline-capture-failed || return 1
    broray_tx_user_validate "$BRORAY_TX_APP_ROOT" protected-source || return 1
    broray_tx_scope_add "$BRORAY_TX_APP_ROOT" || return 1
    # Only package-owned OPKG info belongs to rollback.  Shared status,
    # operations/history/current marker and transaction workspace are control
    # plane or foreign state and are explicitly excluded.
    for broray_tx_scope_item in "$BRORAY_TX_INFO_ROOT"/broray.*; do
        broray_tx_scope_add "$broray_tx_scope_item" || return 1
    done
    if [ -f "$BRORAY_TX_INFO_ROOT/broray.list" ] && [ ! -L "$BRORAY_TX_INFO_ROOT/broray.list" ]; then
        while IFS= read -r broray_tx_scope_item; do
            case "$broray_tx_scope_item" in /opt/broray|/opt/broray/*) continue ;; /opt/*) broray_tx_scope_add "$(broray_tx_root_path "$broray_tx_scope_item")" || return 1 ;; '') ;; *) return 1 ;; esac
        done <"$BRORAY_TX_INFO_ROOT/broray.list"
    fi
    for broray_tx_scope_namespace in \
        "$BRORAY_TX_OPT_ROOT/etc/init.d" "$BRORAY_TX_OPT_ROOT/etc/opkg" \
        "$BRORAY_TX_OPT_ROOT/bin" "$BRORAY_TX_OPT_ROOT/sbin"
    do
        broray_tx_scope_add_broray_named_children "$broray_tx_scope_namespace" || return 1
    done
    for broray_tx_scope_item in \
        "$BRORAY_TX_OPT_ROOT/etc/init.d/S23broray-monitor" "$BRORAY_TX_OPT_ROOT/etc/init.d/S24broray" \
        "$BRORAY_TX_OPT_ROOT/etc/init.d/S25broray-web" "$BRORAY_TX_OPT_ROOT/etc/init.d/S27broray-auto-switch" \
        "$BRORAY_TX_OPT_ROOT/etc/init.d/S28broray-subscriptions" "$BRORAY_TX_OPT_ROOT/etc/opkg/broray.conf" \
        "$BRORAY_TX_OPT_ROOT/etc/xray"
    do broray_tx_scope_add "$broray_tx_scope_item" || return 1; done
    broray_tx_scope_canonicalize || broray_tx_fail snapshot-scope-not-canonical || return 1
    [ -s "$BRORAY_TX_WORK/source-scope.list" ] || broray_tx_fail snapshot-scope-empty
}

broray_tx_scope_size_kb()
{
    broray_tx_scope_total=0
    while IFS= read -r broray_tx_scope_rel; do
        [ -n "$broray_tx_scope_rel" ] || continue
        broray_tx_scope_abs="$(broray_tx_root_path "/$broray_tx_scope_rel")"
        broray_tx_scope_one="$(du -sk "$broray_tx_scope_abs" 2>/dev/null | awk 'NR==1{print $1;exit}')"
        broray_tx_number "$broray_tx_scope_one" || return 1
        broray_tx_scope_total="$(broray_tx_uadd "$broray_tx_scope_total" "$broray_tx_scope_one")" || return 1
    done <"$BRORAY_TX_WORK/source-scope.list"
    printf '%s\n' "$broray_tx_scope_total"
}

# Build the allocation view separately from the content manifest.  Content
# rows intentionally remain path based; allocation rows carry the inode/link
# identity needed to prove which blocks and inodes the exact delete set can
# reclaim.  An inode with links outside the factual scope is valid source
# content, but none of its data blocks/inode is credited as reclaimable.
broray_tx_allocation_manifest_build()
{
    broray_tx_allocation_root="$1"; broray_tx_allocation_content="$2"; broray_tx_allocation_out="$3"
    [ -f "$broray_tx_allocation_content" ] && [ ! -L "$broray_tx_allocation_content" ] || return 1
    : >"$broray_tx_allocation_out.part" || return 1
    while IFS='|' read -r broray_tx_allocation_type broray_tx_allocation_path broray_tx_allocation_bytes \
        broray_tx_allocation_sha broray_tx_allocation_mode broray_tx_allocation_uid broray_tx_allocation_gid broray_tx_allocation_target
    do
        case "$broray_tx_allocation_type" in F|D|L) ;; *) return 1 ;; esac
        broray_tx_relative_safe "$broray_tx_allocation_path" || return 1
        broray_tx_allocation_abs="${broray_tx_allocation_root%/}/$broray_tx_allocation_path"
        broray_tx_allocation_meta="$(find -P "$broray_tx_allocation_abs" -maxdepth 0 \
            -printf '%D|%i|%n|%b|%s' 2>/dev/null)" || return 1
        broray_tx_allocation_device="${broray_tx_allocation_meta%%|*}"
        broray_tx_allocation_tail="${broray_tx_allocation_meta#*|}"
        broray_tx_allocation_inode="${broray_tx_allocation_tail%%|*}"
        broray_tx_allocation_tail="${broray_tx_allocation_tail#*|}"
        broray_tx_allocation_nlink="${broray_tx_allocation_tail%%|*}"
        broray_tx_allocation_tail="${broray_tx_allocation_tail#*|}"
        broray_tx_allocation_blocks="${broray_tx_allocation_tail%%|*}"
        broray_tx_allocation_logical="${broray_tx_allocation_tail#*|}"
        broray_tx_number "$broray_tx_allocation_device" && broray_tx_number "$broray_tx_allocation_inode" &&
            broray_tx_number "$broray_tx_allocation_nlink" && [ "$broray_tx_allocation_nlink" -gt 0 ] &&
            broray_tx_number "$broray_tx_allocation_blocks" && broray_tx_number "$broray_tx_allocation_logical" || return 1
        case "$broray_tx_allocation_type" in
            F)
                [ -f "$broray_tx_allocation_abs" ] && [ ! -L "$broray_tx_allocation_abs" ] || return 1
                [ "$broray_tx_allocation_logical" = "$broray_tx_allocation_bytes" ] || return 1
                broray_tx_allocation_target='-'
                ;;
            D)
                [ -d "$broray_tx_allocation_abs" ] && [ ! -L "$broray_tx_allocation_abs" ] || return 1
                broray_tx_allocation_target='-'
                ;;
            L)
                [ -L "$broray_tx_allocation_abs" ] || return 1
                [ "$(readlink "$broray_tx_allocation_abs")" = "$broray_tx_allocation_target" ] || return 1
                ;;
        esac
        printf '%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n' \
            "$broray_tx_allocation_type" "$broray_tx_allocation_device" "$broray_tx_allocation_inode" \
            "$broray_tx_allocation_nlink" "$broray_tx_allocation_blocks" "$broray_tx_allocation_logical" \
            "$broray_tx_allocation_mode" "$broray_tx_allocation_uid" "$broray_tx_allocation_gid" \
            "$broray_tx_allocation_path" "$broray_tx_allocation_target" \
            >>"$broray_tx_allocation_out.part" || return 1
    done <"$broray_tx_allocation_content"
    broray_tx_sort_file plain "$broray_tx_allocation_out.part" "$broray_tx_allocation_out" || return 1
    rm -f "$broray_tx_allocation_out.part"
    [ -s "$broray_tx_allocation_out" ]
}

broray_tx_allocation_reclaimable_metrics()
{
    broray_tx_allocation_metrics_manifest="$1"
    [ -f "$broray_tx_allocation_metrics_manifest" ] && [ ! -L "$broray_tx_allocation_metrics_manifest" ] || return 1
    : >"$broray_tx_allocation_metrics_manifest.regular.unsorted" || return 1
    BRORAY_TX_ALLOCATION_RECLAIMABLE_BLOCKS512=0
    BRORAY_TX_ALLOCATION_RECLAIMABLE_INODES=0
    BRORAY_TX_ALLOCATION_INTERNAL_HARDLINK_GROUPS=0
    BRORAY_TX_ALLOCATION_EXTERNAL_HARDLINK_GROUPS=0
    while IFS='|' read -r broray_tx_allocation_type broray_tx_allocation_device broray_tx_allocation_inode \
        broray_tx_allocation_nlink broray_tx_allocation_blocks broray_tx_allocation_logical broray_tx_allocation_mode \
        broray_tx_allocation_uid broray_tx_allocation_gid broray_tx_allocation_path broray_tx_allocation_target
    do
        broray_tx_number "$broray_tx_allocation_device" && broray_tx_number "$broray_tx_allocation_inode" &&
            broray_tx_number "$broray_tx_allocation_nlink" && [ "$broray_tx_allocation_nlink" -gt 0 ] &&
            broray_tx_number "$broray_tx_allocation_blocks" || return 1
        case "$broray_tx_allocation_type" in
            F|L)
                # Regular files and symlinks can both have hard links.  Group
                # either type by device/inode and credit it only when every
                # link is inside the exact delete set.  Directories cannot be
                # hard-linked by this lifecycle and are handled per object.
                printf '%s|%s|%s|%s\n' "$broray_tx_allocation_device" "$broray_tx_allocation_inode" \
                    "$broray_tx_allocation_nlink" "$broray_tx_allocation_blocks" \
                    >>"$broray_tx_allocation_metrics_manifest.regular.unsorted" || return 1
                ;;
            D)
                BRORAY_TX_ALLOCATION_RECLAIMABLE_BLOCKS512="$(broray_tx_uadd \
                    "$BRORAY_TX_ALLOCATION_RECLAIMABLE_BLOCKS512" "$broray_tx_allocation_blocks")" || return 1
                BRORAY_TX_ALLOCATION_RECLAIMABLE_INODES="$(broray_tx_uadd \
                    "$BRORAY_TX_ALLOCATION_RECLAIMABLE_INODES" 1)" || return 1
                ;;
            *) return 1 ;;
        esac
    done <"$broray_tx_allocation_metrics_manifest"
    broray_tx_sort_file plain "$broray_tx_allocation_metrics_manifest.regular.unsorted" \
        "$broray_tx_allocation_metrics_manifest.regular" || return 1
    rm -f "$broray_tx_allocation_metrics_manifest.regular.unsorted"

    broray_tx_allocation_previous_key=''
    broray_tx_allocation_group_nlink=0
    broray_tx_allocation_group_blocks=0
    broray_tx_allocation_group_seen=0
    while IFS='|' read -r broray_tx_allocation_device broray_tx_allocation_inode \
        broray_tx_allocation_nlink broray_tx_allocation_blocks
    do
        broray_tx_allocation_key="$broray_tx_allocation_device:$broray_tx_allocation_inode"
        if [ -n "$broray_tx_allocation_previous_key" ] && [ "$broray_tx_allocation_key" != "$broray_tx_allocation_previous_key" ]; then
            [ "$broray_tx_allocation_group_seen" -le "$broray_tx_allocation_group_nlink" ] || return 1
            if [ "$broray_tx_allocation_group_seen" -eq "$broray_tx_allocation_group_nlink" ]; then
                BRORAY_TX_ALLOCATION_RECLAIMABLE_BLOCKS512="$(broray_tx_uadd \
                    "$BRORAY_TX_ALLOCATION_RECLAIMABLE_BLOCKS512" "$broray_tx_allocation_group_blocks")" || return 1
                BRORAY_TX_ALLOCATION_RECLAIMABLE_INODES="$(broray_tx_uadd \
                    "$BRORAY_TX_ALLOCATION_RECLAIMABLE_INODES" 1)" || return 1
                if [ "$broray_tx_allocation_group_nlink" -gt 1 ]; then
                    BRORAY_TX_ALLOCATION_INTERNAL_HARDLINK_GROUPS="$(broray_tx_uadd \
                        "$BRORAY_TX_ALLOCATION_INTERNAL_HARDLINK_GROUPS" 1)" || return 1
                fi
            else
                BRORAY_TX_ALLOCATION_EXTERNAL_HARDLINK_GROUPS="$(broray_tx_uadd \
                    "$BRORAY_TX_ALLOCATION_EXTERNAL_HARDLINK_GROUPS" 1)" || return 1
            fi
            broray_tx_allocation_group_seen=0
        fi
        if [ "$broray_tx_allocation_group_seen" -eq 0 ]; then
            broray_tx_allocation_previous_key="$broray_tx_allocation_key"
            broray_tx_allocation_group_nlink="$broray_tx_allocation_nlink"
            broray_tx_allocation_group_blocks="$broray_tx_allocation_blocks"
        else
            [ "$broray_tx_allocation_group_nlink" = "$broray_tx_allocation_nlink" ] &&
                [ "$broray_tx_allocation_group_blocks" = "$broray_tx_allocation_blocks" ] || return 1
        fi
        broray_tx_allocation_group_seen="$(broray_tx_uadd "$broray_tx_allocation_group_seen" 1)" || return 1
    done <"$broray_tx_allocation_metrics_manifest.regular"
    if [ -n "$broray_tx_allocation_previous_key" ]; then
        [ "$broray_tx_allocation_group_seen" -le "$broray_tx_allocation_group_nlink" ] || return 1
        if [ "$broray_tx_allocation_group_seen" -eq "$broray_tx_allocation_group_nlink" ]; then
            BRORAY_TX_ALLOCATION_RECLAIMABLE_BLOCKS512="$(broray_tx_uadd \
                "$BRORAY_TX_ALLOCATION_RECLAIMABLE_BLOCKS512" "$broray_tx_allocation_group_blocks")" || return 1
            BRORAY_TX_ALLOCATION_RECLAIMABLE_INODES="$(broray_tx_uadd \
                "$BRORAY_TX_ALLOCATION_RECLAIMABLE_INODES" 1)" || return 1
            if [ "$broray_tx_allocation_group_nlink" -gt 1 ]; then
                BRORAY_TX_ALLOCATION_INTERNAL_HARDLINK_GROUPS="$(broray_tx_uadd \
                    "$BRORAY_TX_ALLOCATION_INTERNAL_HARDLINK_GROUPS" 1)" || return 1
            fi
        else
            BRORAY_TX_ALLOCATION_EXTERNAL_HARDLINK_GROUPS="$(broray_tx_uadd \
                "$BRORAY_TX_ALLOCATION_EXTERNAL_HARDLINK_GROUPS" 1)" || return 1
        fi
    fi
    BRORAY_TX_ALLOCATION_RECLAIMABLE_KB="$(broray_tx_ceil_div \
        "$BRORAY_TX_ALLOCATION_RECLAIMABLE_BLOCKS512" 2)" || return 1
    printf 'reclaimableBlocks512|%s\nreclaimableKiB|%s\nreclaimableInodes|%s\ninternalHardlinkGroups|%s\nexternalHardlinkGroups|%s\n' \
        "$BRORAY_TX_ALLOCATION_RECLAIMABLE_BLOCKS512" "$BRORAY_TX_ALLOCATION_RECLAIMABLE_KB" \
        "$BRORAY_TX_ALLOCATION_RECLAIMABLE_INODES" "$BRORAY_TX_ALLOCATION_INTERNAL_HARDLINK_GROUPS" \
        "$BRORAY_TX_ALLOCATION_EXTERNAL_HARDLINK_GROUPS" \
        >"$broray_tx_allocation_metrics_manifest.metrics" || return 1
}

# Reduce allocation identity to the only inode property that must survive a
# delete-and-extract rollback: the exact hardlink equivalence classes.  Device
# and inode numbers are necessarily new after extraction, and block counts may
# legitimately change when a sparse source is restored densely.  A group is
# admissible only when every link to that F/L inode is inside the factual
# source scope; otherwise restoring it would silently split an external inode
# relationship that the transaction neither owns nor may rewrite.
broray_tx_allocation_topology_manifest()
{
    broray_tx_topology_input="$1"; broray_tx_topology_output="$2"
    [ -f "$broray_tx_topology_input" ] && [ ! -L "$broray_tx_topology_input" ] || return 1
    awk -F '|' '
      NF!=11 {bad=1; next}
      $1=="D" {next}
      $1!="F" && $1!="L" {bad=1; next}
      $2 !~ /^(0|[1-9][0-9]*)$/ || $3 !~ /^(0|[1-9][0-9]*)$/ ||
        $4 !~ /^[1-9][0-9]*$/ {bad=1; next}
      {
        k=$2 SUBSEP $3
        if ((k in type) && type[k]!=$1) bad=1
        if ((k in nlink) && nlink[k]!=$4) bad=1
        type[k]=$1; nlink[k]=$4; count[k]++
        if (!(k in anchor) || $10<anchor[k]) anchor[k]=$10
        if ($10 in pathkey) bad=1
        pathkey[$10]=k; pathtype[$10]=$1
      }
      END {
        for (k in count) if (count[k]!=nlink[k]) bad=1
        if (bad) exit 1
        for (p in pathkey) {
          k=pathkey[p]
          printf "%s|%s|%s|%d\n",pathtype[p],p,anchor[k],count[k]
        }
      }
    ' "$broray_tx_topology_input" >"$broray_tx_topology_output.unsorted" || {
        rm -f "$broray_tx_topology_output.unsorted"
        return 1
    }
    LC_ALL=C sort "$broray_tx_topology_output.unsorted" >"$broray_tx_topology_output.part" || {
        rm -f "$broray_tx_topology_output.unsorted" "$broray_tx_topology_output.part"
        return 1
    }
    rm -f "$broray_tx_topology_output.unsorted"
    mv -f "$broray_tx_topology_output.part" "$broray_tx_topology_output"
}

broray_tx_capability_preflight()
{
    broray_tx_event capability-preflight || return 1
    broray_runtime_probe_full "$BRORAY_TX_WORK" || {
        broray_tx_fail "capability-preflight-failed:${BRORAY_RUNTIME_FAILURE_ID:-unknown}"
        return 1
    }
    broray_tx_capability_source_package="$BRORAY_TX_SOURCE_PACKAGE"
    broray_tx_capability_source_app="$BRORAY_TX_SOURCE_APP"
    broray_tx_capability_source_class="$BRORAY_TX_SOURCE_CLASS"
    broray_tx_capability_migration="$BRORAY_TX_MIGRATION_ID"
    broray_tx_native_opkg_lock_acquire || {
        broray_tx_fail native-opkg-lock-not-proven
        return 1
    }
    # Close the pre-lock race: source admission is version-independent, but
    # its measured identity must remain byte-identical after the native OPKG
    # exclusion is held.
    broray_tx_source_admit || { broray_tx_fail source-reread-under-opkg-lock-failed; return 1; }
    [ "$BRORAY_TX_SOURCE_PACKAGE" = "$broray_tx_capability_source_package" ] &&
        [ "$BRORAY_TX_SOURCE_APP" = "$broray_tx_capability_source_app" ] &&
        [ "$BRORAY_TX_SOURCE_CLASS" = "$broray_tx_capability_source_class" ] &&
        [ "$BRORAY_TX_MIGRATION_ID" = "$broray_tx_capability_migration" ] || {
            broray_tx_fail source-drift-before-native-opkg-lock
            return 1
        }
    broray_tx_native_opkg_lock_assert capability-held || {
        broray_tx_fail native-opkg-lock-lost-after-source-reread
        return 1
    }
    broray_tx_event runtime-capabilities-verified || return 1
    broray_tx_status running ''
}

broray_tx_space_check()
{
    [ -f "$BRORAY_TX_WORK/candidate.json" ] && [ ! -L "$BRORAY_TX_WORK/candidate.json" ] || broray_tx_fail candidate-metadata-required-before-space-plan || return 1
    broray_tx_event factual-source-manifest || return 1
    broray_tx_scope_build || return 1
    broray_tx_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/source-scope.list" "$BRORAY_TX_WORK/source.manifest" || {
        broray_tx_fail "source-manifest-build-failed:${BRORAY_TX_MANIFEST_FAILURE_REASON:-unknown}:${BRORAY_TX_MANIFEST_FAILURE_PATH:-unknown}"
        return 1
    }
    broray_tx_allocation_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/source.manifest" \
        "$BRORAY_TX_WORK/source.allocation.manifest" || broray_tx_fail source-allocation-manifest-build-failed || return 1
    broray_tx_allocation_reclaimable_metrics "$BRORAY_TX_WORK/source.allocation.manifest" ||
        broray_tx_fail source-allocation-topology-ambiguous || return 1
    [ "$BRORAY_TX_ALLOCATION_EXTERNAL_HARDLINK_GROUPS" -eq 0 ] || {
        broray_tx_fail source-external-hardlink-topology-unsupported
        return 1
    }
    broray_tx_allocation_topology_manifest "$BRORAY_TX_WORK/source.allocation.manifest" \
        "$BRORAY_TX_WORK/source.hardlink-topology" ||
        broray_tx_fail source-allocation-topology-ambiguous || return 1
    broray_tx_source_kb="$BRORAY_TX_ALLOCATION_RECLAIMABLE_KB"
    broray_tx_source_reclaimable_inodes="$BRORAY_TX_ALLOCATION_RECLAIMABLE_INODES"
    broray_tx_source_internal_hardlinks="$BRORAY_TX_ALLOCATION_INTERNAL_HARDLINK_GROUPS"
    broray_tx_source_external_hardlinks="$BRORAY_TX_ALLOCATION_EXTERNAL_HARDLINK_GROUPS"
    broray_tx_source_bytes="$(broray_tx_manifest_logical_bytes "$BRORAY_TX_WORK/source.manifest")" || return 1
    broray_tx_source_objects="$(wc -l <"$BRORAY_TX_WORK/source.manifest" | tr -d ' ')"
    broray_tx_scope_duplicates="$(cat "$BRORAY_TX_WORK/source-scope.duplicates" 2>/dev/null)"
    broray_tx_number "$broray_tx_source_bytes" && broray_tx_number "$broray_tx_source_objects" && broray_tx_number "$broray_tx_scope_duplicates" || broray_tx_fail invalid-source-scope-metrics || return 1
    [ "$broray_tx_source_objects" -gt 0 ] && [ "$broray_tx_scope_duplicates" -eq 0 ] || broray_tx_fail source-scope-not-unique || return 1

    # Create the self-contained recovery capsule before computing the stream
    # bound.  Capsule bytes and objects are therefore part of the factual T0
    # input rather than an uncounted post-plan addition.
    broray_tx_recovery_capsule_create || broray_tx_fail recovery-capsule-plan-failed || return 1
    broray_tx_tree_manifest "$BRORAY_TX_WORK/.broray-recovery" "$BRORAY_TX_WORK/capsule.manifest" || return 1
    broray_tx_capsule_bytes="$(broray_tx_manifest_logical_bytes "$BRORAY_TX_WORK/capsule.manifest")" || return 1
    broray_tx_capsule_objects="$(wc -l <"$BRORAY_TX_WORK/capsule.manifest" | tr -d ' ')"
    broray_tx_number "$broray_tx_capsule_bytes" && broray_tx_number "$broray_tx_capsule_objects" || return 1

    broray_tx_target_alloc_base_kb="$(jq -r '.targetAllocatedUpperKB' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_target_objects="$(jq -r '.payloadObjectCount' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_candidate_declared_bytes="$(jq -r '.sizeBytes' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_number "$broray_tx_target_alloc_base_kb" && broray_tx_number "$broray_tx_target_objects" &&
        broray_tx_number "$broray_tx_candidate_declared_bytes" || return 1
    [ "$broray_tx_target_alloc_base_kb" -gt 0 ] && [ "$broray_tx_target_objects" -gt 0 ] || return 1
    broray_tx_opt_allocation_unit_kb="$(jq -r '.mountGraph.opt.allocationUnitKB // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    broray_tx_tmp_allocation_unit_kb="$(jq -r '.mountGraph.tmp.allocationUnitKB // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    broray_tx_number "$broray_tx_opt_allocation_unit_kb" && [ "$broray_tx_opt_allocation_unit_kb" -gt 0 ] &&
        broray_tx_number "$broray_tx_tmp_allocation_unit_kb" && [ "$broray_tx_tmp_allocation_unit_kb" -gt 0 ] ||
        broray_tx_fail missing-allocation-unit-evidence || return 1
    jq -r '.protectedDefaultEntries[] | [.path,.allocatedUpperKB,.objectCount] | @tsv' \
        "$BRORAY_TX_WORK/candidate.json" >"$BRORAY_TX_WORK/candidate-protected-defaults.tsv" || return 1
    broray_tx_target_overlap_alloc_base_kb=0
    broray_tx_target_overlap_objects=0
    while IFS="$(printf '\t')" read -r broray_tx_target_default_root broray_tx_target_default_alloc broray_tx_target_default_objects; do
        broray_tx_relative_safe "$broray_tx_target_default_root" &&
            broray_tx_number "$broray_tx_target_default_alloc" &&
            broray_tx_number "$broray_tx_target_default_objects" || return 1
        grep -Fqx "$broray_tx_target_default_root" "$BRORAY_TX_WORK/protected-source-roots.list" || continue
        broray_tx_target_overlap_alloc_base_kb="$(broray_tx_uadd "$broray_tx_target_overlap_alloc_base_kb" \
            "$broray_tx_target_default_alloc")" || return 1
        broray_tx_target_overlap_objects="$(broray_tx_uadd "$broray_tx_target_overlap_objects" \
            "$broray_tx_target_default_objects")" || return 1
    done <"$BRORAY_TX_WORK/candidate-protected-defaults.tsv"
    broray_tx_target_alloc_kb="$(broray_tx_adjust_allocation_upper_kb "$broray_tx_target_alloc_base_kb" \
        "$broray_tx_target_objects" "$broray_tx_opt_allocation_unit_kb")" || return 1
    broray_tx_target_overlap_alloc_kb="$(broray_tx_adjust_allocation_upper_kb \
        "$broray_tx_target_overlap_alloc_base_kb" "$broray_tx_target_overlap_objects" \
        "$broray_tx_opt_allocation_unit_kb")" || return 1
    broray_tx_target_nonprotected_alloc_kb="$(broray_tx_usub "$broray_tx_target_alloc_kb" \
        "$broray_tx_target_overlap_alloc_kb")" || broray_tx_fail candidate-protected-default-allocation-overlap-invalid || return 1
    broray_tx_target_nonprotected_objects="$(broray_tx_usub "$broray_tx_target_objects" \
        "$broray_tx_target_overlap_objects")" || broray_tx_fail candidate-protected-default-object-overlap-invalid || return 1
    broray_tx_source_restore_kb="$(broray_tx_manifest_restore_upper_kb "$BRORAY_TX_WORK/source.manifest" \
        "$broray_tx_opt_allocation_unit_kb")" || broray_tx_fail source-restore-upper-bound-failed || return 1

    # Candidate defaults under factual protected roots are removed before
    # exact source bytes are restored.  The forward formula therefore uses
    # target-non-protected + exact protected, while full target extraction is
    # retained as its own phase maximum.
    broray_tx_protected_kb=0
    while IFS= read -r broray_tx_protected_rel; do
        [ -n "$broray_tx_protected_rel" ] || continue
        broray_tx_protected_one="$(du -sk "$BRORAY_TX_APP_ROOT/$broray_tx_protected_rel" 2>/dev/null | awk 'NR==1{print $1;exit}')"
        broray_tx_number "$broray_tx_protected_one" || return 1
        broray_tx_protected_kb="$(broray_tx_uadd "$broray_tx_protected_kb" "$broray_tx_protected_one")" || return 1
    done <"$BRORAY_TX_WORK/protected-source-roots.list"
    broray_tx_protected_objects="$(wc -l <"$BRORAY_TX_WORK/protected-source.manifest" | tr -d ' ')"
    broray_tx_number "$broray_tx_protected_objects" || return 1

    # Writer limits are logical byte ceilings.  Convert every finite write set
    # to an allocation upper using the measured filesystem unit, and include
    # one unit for each setup/durable inode that may need directory or symlink
    # storage.  A 4-KiB baseline therefore remains exact while a larger-unit
    # filesystem cannot turn a safe logical limit into an allocation overrun.
    broray_tx_setup_file_bytes="$(broray_tx_umul "$BRORAY_TX_SETUP_FILE_LIMIT_BLOCKS512" 512)" || return 1
    broray_tx_setup_file_alloc_kb="$(broray_tx_dense_bytes_upper_kb "$broray_tx_setup_file_bytes" \
        "$broray_tx_opt_allocation_unit_kb")" || return 1
    broray_tx_setup_data_alloc_kb="$(broray_tx_umul "$broray_tx_setup_file_alloc_kb" \
        "$BRORAY_TX_SETUP_WRITABLE_REGULAR_CAP")" || return 1
    broray_tx_setup_metadata_alloc_kb="$(broray_tx_umul "$BRORAY_TX_SETUP_INODE_CAP" \
        "$broray_tx_opt_allocation_unit_kb")" || return 1
    broray_tx_setup_alloc_upper_kb="$(broray_tx_uadd "$broray_tx_setup_data_alloc_kb" \
        "$broray_tx_setup_metadata_alloc_kb")" || return 1
    broray_tx_durable_file_bytes="$(broray_tx_umul "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" 1024)" || return 1
    broray_tx_durable_opt_file_alloc_kb="$(broray_tx_dense_bytes_upper_kb "$broray_tx_durable_file_bytes" \
        "$broray_tx_opt_allocation_unit_kb")" || return 1
    broray_tx_durable_opt_data_alloc_kb="$(broray_tx_umul "$broray_tx_durable_opt_file_alloc_kb" \
        "$BRORAY_TX_DURABLE_EVIDENCE_FILE_CAP")" || return 1
    broray_tx_durable_opt_metadata_alloc_kb="$(broray_tx_umul "$BRORAY_TX_DURABLE_EVIDENCE_INODE_CAP" \
        "$broray_tx_opt_allocation_unit_kb")" || return 1
    broray_tx_durable_opt_alloc_upper_kb="$(broray_tx_uadd "$broray_tx_durable_opt_data_alloc_kb" \
        "$broray_tx_durable_opt_metadata_alloc_kb")" || return 1
    broray_tx_setup_output_bytes="$(broray_tx_umul "$BRORAY_TX_SETUP_OUTPUT_EACH_CAP_KB" 1024)" || return 1
    broray_tx_setup_output_tmp_alloc_each_kb="$(broray_tx_dense_bytes_upper_kb "$broray_tx_setup_output_bytes" \
        "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_durable_tmp_alloc_each_kb="$(broray_tx_dense_bytes_upper_kb "$broray_tx_durable_file_bytes" \
        "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_post_candidate_writer_upper_kb="$(broray_tx_umul "$broray_tx_setup_output_tmp_alloc_each_kb" 2)" || return 1
    broray_tx_durable_tmp_streams_kb="$(broray_tx_umul "$broray_tx_durable_tmp_alloc_each_kb" 2)" || return 1
    broray_tx_post_candidate_writer_upper_kb="$(broray_tx_uadd "$broray_tx_post_candidate_writer_upper_kb" \
        "$broray_tx_durable_tmp_streams_kb")" || return 1
    broray_tx_post_candidate_writer_upper_kb="$(broray_tx_uadd "$broray_tx_post_candidate_writer_upper_kb" \
        "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB")" || return 1

    # The factual source/content/allocation manifests, service/external
    # baseline and recovery capsule above are read-only inputs to this event.
    # `space-check` therefore means the first df/inode comparison, not the
    # beginning of factual discovery.
    broray_tx_event space-check || return 1
    broray_tx_opt_free="${BRORAY_TX_TEST_OPT_FREE_KB:-$(df -Pk "$BRORAY_TX_OPT_ROOT" 2>/dev/null | awk 'NR==2{print $4;exit}')}"
    broray_tx_tmp_free="${BRORAY_TX_TEST_TMP_FREE_KB:-$(df -Pk "$BRORAY_TX_TMP_BASE" 2>/dev/null | awk 'NR==2{print $4;exit}')}"
    broray_tx_tmp_total="${BRORAY_TX_TEST_TMP_TOTAL_KB:-$(df -Pk "$BRORAY_TX_TMP_BASE" 2>/dev/null | awk 'NR==2{print $2;exit}')}"
    broray_tx_opt_free_inodes="${BRORAY_TX_TEST_OPT_FREE_INODES:-$(broray_tx_df_inodes "$BRORAY_TX_OPT_ROOT")}"
    broray_tx_tmp_free_inodes="${BRORAY_TX_TEST_TMP_FREE_INODES:-$(broray_tx_df_inodes "$BRORAY_TX_TMP_BASE")}"
    broray_tx_number "$broray_tx_opt_free" && broray_tx_number "$broray_tx_tmp_free" && broray_tx_number "$broray_tx_tmp_total" &&
        broray_tx_number "$broray_tx_opt_free_inodes" && broray_tx_number "$broray_tx_tmp_free_inodes" || broray_tx_fail cannot-measure-free-space || return 1
    broray_tx_workspace_overhead="$(du -sk "$BRORAY_TX_WORK" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_workspace_overhead" || broray_tx_fail cannot-measure-workspace-overhead || return 1
    broray_tx_tmp_existing="$(broray_tx_sub0 "$broray_tx_tmp_total" "$broray_tx_tmp_free")" || return 1

    broray_tx_source_tar_bound_bytes="$(broray_tx_tar_manifest_upper_bytes "$BRORAY_TX_WORK/source.manifest" '')" ||
        broray_tx_fail source-tar-upper-bound-failed || return 1
    broray_tx_capsule_tar_bound_bytes="$(broray_tx_tar_manifest_upper_bytes "$BRORAY_TX_WORK/capsule.manifest" '.broray-recovery/')" ||
        broray_tx_fail capsule-tar-upper-bound-failed || return 1
    # Synthetic .broray-recovery directory: base header plus an always-reserved
    # GNU.longname header and one 512-byte name payload.
    broray_tx_tar_bound_bytes="$(broray_tx_uadd "$broray_tx_source_tar_bound_bytes" "$broray_tx_capsule_tar_bound_bytes")" || return 1
    broray_tx_tar_bound_bytes="$(broray_tx_uadd "$broray_tx_tar_bound_bytes" 1536)" || return 1
    broray_tx_tar_bound_bytes="$(broray_tx_uadd "$broray_tx_tar_bound_bytes" 1024)" || return 1
    broray_tx_gzip_overhead="$(broray_tx_ceil_div "$broray_tx_tar_bound_bytes" 8)" || return 1
    broray_tx_gzip_bound_bytes="$(broray_tx_uadd "$broray_tx_tar_bound_bytes" "$broray_tx_gzip_overhead")" || return 1
    broray_tx_gzip_bound_bytes="$(broray_tx_uadd "$broray_tx_gzip_bound_bytes" 65536)" || return 1
    broray_tx_snapshot_limit_kb="$(broray_tx_ceil_div "$broray_tx_gzip_bound_bytes" 1024)" || return 1
    broray_tx_snapshot_alloc_upper_kb="$(broray_tx_dense_bytes_upper_kb "$broray_tx_gzip_bound_bytes" \
        "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_capsule_restore_kb="$(broray_tx_manifest_restore_upper_kb "$BRORAY_TX_WORK/capsule.manifest" \
        "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_snapshot_metadata_basis=0
    for broray_tx_snapshot_metadata_file in "$BRORAY_TX_WORK/source.manifest" "$BRORAY_TX_WORK/source.allocation.manifest" \
        "$BRORAY_TX_WORK/capsule.manifest" \
        "$BRORAY_TX_WORK/source-scope.list" "$BRORAY_TX_WORK/capsule.members.all"; do
        broray_tx_snapshot_metadata_one="$(wc -c <"$broray_tx_snapshot_metadata_file" 2>/dev/null | tr -d ' ')"
        broray_tx_number "$broray_tx_snapshot_metadata_one" || return 1
        broray_tx_snapshot_metadata_basis="$(broray_tx_uadd "$broray_tx_snapshot_metadata_basis" "$broray_tx_snapshot_metadata_one")" || return 1
    done
    # Verification writes at most eight path/manifest-shaped copies plus fixed
    # hashes, status and diagnostics.  Reject the source before snapshot if
    # that proved input-derived bound exceeds the writer cap.
    broray_tx_snapshot_metadata_upper_bytes="$(broray_tx_umul "$broray_tx_snapshot_metadata_basis" 8)" || return 1
    broray_tx_snapshot_metadata_upper_bytes="$(broray_tx_uadd "$broray_tx_snapshot_metadata_upper_bytes" 65536)" || return 1
    broray_tx_snapshot_metadata_upper_kb="$(broray_tx_ceil_div "$broray_tx_snapshot_metadata_upper_bytes" 1024)" || return 1
    [ "$broray_tx_snapshot_metadata_upper_kb" -le "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB" ] ||
        broray_tx_fail snapshot-metadata-writer-cap-unprovable || return 1
    broray_tx_tmp_required="$(broray_tx_uadd "$BRORAY_TX_TMP_RESERVE_KB" "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB")" || return 1
    broray_tx_tmp_required="$(broray_tx_uadd "$broray_tx_tmp_required" "$broray_tx_snapshot_alloc_upper_kb")" || return 1
    broray_tx_tmp_required="$(broray_tx_uadd "$broray_tx_tmp_required" "$broray_tx_capsule_restore_kb")" || return 1
    broray_tx_tmp_future_inodes="$(broray_tx_uadd "$broray_tx_capsule_objects" "$BRORAY_TX_SNAPSHOT_FIXED_FUTURE_INODES")" || return 1
    broray_tx_tmp_inode_required="$(broray_tx_uadd "$BRORAY_TX_TMP_INODE_RESERVE" "$broray_tx_tmp_future_inodes")" || return 1

    broray_tx_opkg_metadata_kb="$(broray_tx_umul "$broray_tx_target_objects" 512)" || return 1
    broray_tx_opkg_metadata_kb="$(broray_tx_uadd "$broray_tx_opkg_metadata_kb" 65536)" || return 1
    broray_tx_opkg_metadata_kb="$(broray_tx_ceil_div "$broray_tx_opkg_metadata_kb" 1024)" || return 1
    broray_tx_opkg_status_transient_kb="$(broray_tx_file_dense_upper_kb "$BRORAY_TX_STATUS_FILE" \
        "$broray_tx_opt_allocation_unit_kb")" || broray_tx_fail cannot-bound-opkg-status-transient || return 1
    broray_tx_opt_forward_extraction_kb="$broray_tx_target_alloc_kb"
    broray_tx_opt_forward_restored_kb="$(broray_tx_uadd "$broray_tx_target_nonprotected_alloc_kb" "$broray_tx_protected_kb")" || return 1
    broray_tx_opt_forward_restored_kb="$(broray_tx_uadd "$broray_tx_opt_forward_restored_kb" "$broray_tx_opkg_metadata_kb")" || return 1
    broray_tx_opt_forward_restored_kb="$(broray_tx_uadd "$broray_tx_opt_forward_restored_kb" "$broray_tx_opkg_status_transient_kb")" || return 1
    broray_tx_opt_forward_restored_kb="$(broray_tx_uadd "$broray_tx_opt_forward_restored_kb" "$broray_tx_setup_alloc_upper_kb")" || return 1
    broray_tx_opt_forward_restored_kb="$(broray_tx_uadd "$broray_tx_opt_forward_restored_kb" "$broray_tx_durable_opt_alloc_upper_kb")" || return 1
    broray_tx_opt_forward_kb="$(broray_tx_umax "$broray_tx_opt_forward_extraction_kb" \
        "$broray_tx_opt_forward_restored_kb")" || return 1
    broray_tx_opt_delta="$(broray_tx_sub0 "$broray_tx_opt_forward_kb" "$broray_tx_source_kb")" || return 1
    broray_tx_opt_rollback_kb="$(broray_tx_uadd "$broray_tx_source_restore_kb" "$broray_tx_opkg_status_transient_kb")" || return 1
    broray_tx_opt_rollback_kb="$(broray_tx_uadd "$broray_tx_opt_rollback_kb" "$broray_tx_opkg_metadata_kb")" || return 1
    broray_tx_opt_rollback_kb="$(broray_tx_uadd "$broray_tx_opt_rollback_kb" "$broray_tx_durable_opt_alloc_upper_kb")" || return 1
    broray_tx_opt_rollback_delta="$(broray_tx_sub0 "$broray_tx_opt_rollback_kb" "$broray_tx_source_kb")" || return 1
    broray_tx_opt_peak_delta="$(broray_tx_umax "$broray_tx_opt_delta" "$broray_tx_opt_rollback_delta")" || return 1
    broray_tx_opt_required="$(broray_tx_uadd "$BRORAY_TX_OPT_RESERVE_KB" "$broray_tx_opt_peak_delta")" || return 1
    broray_tx_opt_required_mib="$(broray_tx_ceil_div "$broray_tx_opt_required" 1024)" || return 1
    broray_tx_opt_required="$(broray_tx_umul "$broray_tx_opt_required_mib" 1024)" || return 1
    broray_tx_opt_forward_extraction_inodes="$broray_tx_target_objects"
    broray_tx_opt_forward_restored_inodes="$(broray_tx_uadd "$broray_tx_target_nonprotected_objects" "$broray_tx_protected_objects")" || return 1
    broray_tx_opt_forward_restored_inodes="$(broray_tx_uadd "$broray_tx_opt_forward_restored_inodes" "$BRORAY_TX_SETUP_INODE_CAP")" || return 1
    broray_tx_opt_forward_restored_inodes="$(broray_tx_uadd "$broray_tx_opt_forward_restored_inodes" "$BRORAY_TX_OPKG_METADATA_INODE_CAP")" || return 1
    broray_tx_opt_forward_restored_inodes="$(broray_tx_uadd "$broray_tx_opt_forward_restored_inodes" "$BRORAY_TX_DURABLE_EVIDENCE_INODE_CAP")" || return 1
    broray_tx_opt_forward_restored_inodes="$(broray_tx_uadd "$broray_tx_opt_forward_restored_inodes" 1)" || return 1
    broray_tx_opt_forward_inodes="$(broray_tx_umax "$broray_tx_opt_forward_extraction_inodes" \
        "$broray_tx_opt_forward_restored_inodes")" || return 1
    broray_tx_opt_inode_delta="$(broray_tx_sub0 "$broray_tx_opt_forward_inodes" "$broray_tx_source_reclaimable_inodes")" || return 1
    broray_tx_opt_rollback_inodes="$(broray_tx_uadd "$broray_tx_source_objects" 1)" || return 1
    broray_tx_opt_rollback_inodes="$(broray_tx_uadd "$broray_tx_opt_rollback_inodes" "$BRORAY_TX_DURABLE_EVIDENCE_INODE_CAP")" || return 1
    broray_tx_opt_rollback_inode_delta="$(broray_tx_sub0 "$broray_tx_opt_rollback_inodes" "$broray_tx_source_reclaimable_inodes")" || return 1
    broray_tx_opt_inode_peak_delta="$(broray_tx_umax "$broray_tx_opt_inode_delta" "$broray_tx_opt_rollback_inode_delta")" || return 1
    broray_tx_opt_inode_required="$(broray_tx_uadd "$BRORAY_TX_OPT_INODE_RESERVE" "$broray_tx_opt_inode_peak_delta")" || return 1

    broray_tx_same_fs="$(jq -r 'if (.mountGraph.sameBackingFs|type)=="boolean" then (.mountGraph.sameBackingFs|tostring) else empty end' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    case "$broray_tx_same_fs" in true|false) ;; *) broray_tx_fail missing-backing-fs-identity; return 1 ;; esac
    broray_tx_shared_required=0
    broray_tx_shared_inode_required=0
    broray_tx_shared_phase_delta=0
    broray_tx_shared_phase_inode_delta=0
    broray_tx_tmp_snapshot_delta="$(broray_tx_usub "$broray_tx_tmp_required" "$BRORAY_TX_TMP_RESERVE_KB")" || return 1
    broray_tx_tmp_snapshot_inode_delta="$(broray_tx_usub "$broray_tx_tmp_inode_required" "$BRORAY_TX_TMP_INODE_RESERVE")" || return 1
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_opt_free" = "$broray_tx_tmp_free" ] &&
            [ "$broray_tx_opt_free_inodes" = "$broray_tx_tmp_free_inodes" ] &&
            [ "$broray_tx_opt_allocation_unit_kb" = "$broray_tx_tmp_allocation_unit_kb" ] ||
            broray_tx_fail same-backing-fs-samples-disagree || return 1
        # T0 is non-mutating and rebases after snapshot.  Its two executable
        # phase obligations are snapshot growth (opt delta 0) and the early
        # opt peak admission (tmp delta rebased at T1), hence max, not sum.
        broray_tx_shared_phase_delta="$(broray_tx_umax "$broray_tx_opt_peak_delta" "$broray_tx_tmp_snapshot_delta")" || return 1
        broray_tx_shared_phase_inode_delta="$(broray_tx_umax "$broray_tx_opt_inode_peak_delta" "$broray_tx_tmp_snapshot_inode_delta")" || return 1
        broray_tx_shared_required="$(broray_tx_uadd "$BRORAY_TX_OPT_RESERVE_KB" "$BRORAY_TX_TMP_RESERVE_KB")" || return 1
        broray_tx_shared_required="$(broray_tx_uadd "$broray_tx_shared_required" "$broray_tx_shared_phase_delta")" || return 1
        broray_tx_shared_inode_required="$(broray_tx_uadd "$BRORAY_TX_OPT_INODE_RESERVE" "$BRORAY_TX_TMP_INODE_RESERVE")" || return 1
        broray_tx_shared_inode_required="$(broray_tx_uadd "$broray_tx_shared_inode_required" "$broray_tx_shared_phase_inode_delta")" || return 1
    fi
    broray_tx_space_result=PASS
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_opt_free" -ge "$broray_tx_shared_required" ] || broray_tx_space_result=FAIL
        [ "$broray_tx_opt_free_inodes" -ge "$broray_tx_shared_inode_required" ] || broray_tx_space_result=FAIL
    else
        [ "$broray_tx_opt_free" -ge "$broray_tx_opt_required" ] || broray_tx_space_result=FAIL
        [ "$broray_tx_tmp_free" -ge "$broray_tx_tmp_required" ] || broray_tx_space_result=FAIL
        [ "$broray_tx_opt_free_inodes" -ge "$broray_tx_opt_inode_required" ] || broray_tx_space_result=FAIL
        [ "$broray_tx_tmp_free_inodes" -ge "$broray_tx_tmp_inode_required" ] || broray_tx_space_result=FAIL
    fi
    jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" \
        --arg requirementsContract "$BRORAY_TX_REQUIREMENTS_CONTRACT" --arg lifecycleContract "$BRORAY_TX_CONTRACT" \
        --arg capabilityContract "$BRORAY_RUNTIME_CAPABILITY_CONTRACT" --arg spaceContract broray-space/2 \
        --argjson sameBackingFs "$broray_tx_same_fs" \
        --argjson optFreeKB "$broray_tx_opt_free" --argjson tmpFreeKB "$broray_tx_tmp_free" \
        --argjson optFreeInodes "$broray_tx_opt_free_inodes" --argjson tmpFreeInodes "$broray_tx_tmp_free_inodes" \
        --argjson optRequiredInodes "$broray_tx_opt_inode_required" --argjson tmpRequiredInodes "$broray_tx_tmp_inode_required" \
        --argjson tmpTotalKB "$broray_tx_tmp_total" --argjson tmpExistingUsageKB "$broray_tx_tmp_existing" \
        --argjson sourceScopeKB "$broray_tx_source_kb" --argjson protectedSourceKB "$broray_tx_protected_kb" \
        --argjson protectedSourceObjects "$broray_tx_protected_objects" \
        --argjson sourceBytes "$broray_tx_source_bytes" --argjson sourceScopeUniqueObjects "$broray_tx_source_objects" \
        --argjson sourceReclaimableInodes "$broray_tx_source_reclaimable_inodes" \
        --argjson sourceInternalHardlinkGroups "$broray_tx_source_internal_hardlinks" \
        --argjson sourceExternalHardlinkGroups "$broray_tx_source_external_hardlinks" \
        --argjson sourceScopeDuplicates "$broray_tx_scope_duplicates" --argjson workspaceOverheadKB "$broray_tx_workspace_overhead" \
        --arg candidateSha256 "$(jq -r '.sha256' "$BRORAY_TX_WORK/candidate.json")" \
        --argjson candidateDeclaredBytes "$broray_tx_candidate_declared_bytes" \
        --argjson targetAllocatedUpperKB "$broray_tx_target_alloc_kb" --argjson targetObjectCount "$broray_tx_target_objects" \
        --argjson targetProtectedOverlapAllocatedUpperKB "$broray_tx_target_overlap_alloc_kb" \
        --argjson targetProtectedOverlapObjects "$broray_tx_target_overlap_objects" \
        --argjson targetNonProtectedAllocatedUpperKB "$broray_tx_target_nonprotected_alloc_kb" \
        --argjson targetNonProtectedObjectCount "$broray_tx_target_nonprotected_objects" \
        --argjson optAllocationUnitKB "$broray_tx_opt_allocation_unit_kb" --argjson tmpAllocationUnitKB "$broray_tx_tmp_allocation_unit_kb" \
        --argjson capsuleBytes "$broray_tx_capsule_bytes" --argjson capsuleObjects "$broray_tx_capsule_objects" \
        --argjson capsuleRestoreUpperKB "$broray_tx_capsule_restore_kb" \
        --argjson sourceRestoreUpperKB "$broray_tx_source_restore_kb" \
        --argjson optForwardExtractionPeakKB "$broray_tx_opt_forward_extraction_kb" \
        --argjson optForwardRestoredPeakKB "$broray_tx_opt_forward_restored_kb" \
        --argjson optForwardPeakKB "$broray_tx_opt_forward_kb" --argjson optRollbackPeakKB "$broray_tx_opt_rollback_kb" \
        --argjson optForwardDeltaKB "$broray_tx_opt_delta" --argjson optRollbackDeltaKB "$broray_tx_opt_rollback_delta" \
        --argjson optForwardExtractionPeakInodes "$broray_tx_opt_forward_extraction_inodes" \
        --argjson optForwardRestoredPeakInodes "$broray_tx_opt_forward_restored_inodes" \
        --argjson optForwardPeakInodes "$broray_tx_opt_forward_inodes" --argjson optRollbackPeakInodes "$broray_tx_opt_rollback_inodes" \
        --argjson optForwardInodeDelta "$broray_tx_opt_inode_delta" --argjson optRollbackInodeDelta "$broray_tx_opt_rollback_inode_delta" \
        --argjson opkgMetadataUpperKB "$broray_tx_opkg_metadata_kb" --argjson opkgStatusTransientUpperKB "$broray_tx_opkg_status_transient_kb" \
        --argjson tmpSafetyReserveKB "$BRORAY_TX_TMP_RESERVE_KB" --argjson optSafetyReserveKB "$BRORAY_TX_OPT_RESERVE_KB" \
        --argjson evidenceGrowthCapKB "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB" \
        --argjson setupGrowthCapKB "$BRORAY_TX_SETUP_GROWTH_CAP_KB" --argjson setupInodeCap "$BRORAY_TX_SETUP_INODE_CAP" \
        --argjson setupAllocatedUpperKB "$broray_tx_setup_alloc_upper_kb" \
        --argjson setupOutputEachCapKB "$BRORAY_TX_SETUP_OUTPUT_EACH_CAP_KB" \
        --argjson setupOutputAllocatedUpperEachKB "$broray_tx_setup_output_tmp_alloc_each_kb" \
        --argjson setupFileLimitBlocks512 "$BRORAY_TX_SETUP_FILE_LIMIT_BLOCKS512" \
        --argjson setupWritableRegularCap "$BRORAY_TX_SETUP_WRITABLE_REGULAR_CAP" \
        --arg setupWriteContract "$BRORAY_TX_SETUP_WRITE_CONTRACT" \
        --argjson durableEvidenceCapKB "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" \
        --argjson durableEvidenceFileCap "$BRORAY_TX_DURABLE_EVIDENCE_FILE_CAP" \
        --argjson durableEvidenceInodeCap "$BRORAY_TX_DURABLE_EVIDENCE_INODE_CAP" \
        --argjson durableOptAllocatedUpperKB "$broray_tx_durable_opt_alloc_upper_kb" \
        --argjson durableTmpAllocatedUpperEachKB "$broray_tx_durable_tmp_alloc_each_kb" \
        --argjson postCandidateWriterUpperKB "$broray_tx_post_candidate_writer_upper_kb" \
        --argjson opkgMetadataInodeCap "$BRORAY_TX_OPKG_METADATA_INODE_CAP" \
        --argjson tarUpperBoundBytes "$broray_tx_tar_bound_bytes" --argjson gzipUpperBoundBytes "$broray_tx_gzip_bound_bytes" \
        --argjson snapshotArchiveLimitKB "$broray_tx_snapshot_limit_kb" --argjson snapshotAllocatedUpperKB "$broray_tx_snapshot_alloc_upper_kb" \
        --argjson snapshotMetadataUpperKB "$broray_tx_snapshot_metadata_upper_kb" --arg spaceCheckResult "$broray_tx_space_result" \
        --argjson optRequiredKB "$broray_tx_opt_required" --argjson tmpRequiredKB "$broray_tx_tmp_required" \
        --argjson tmpSnapshotDeltaKB "$broray_tx_tmp_snapshot_delta" \
        --argjson tmpSnapshotDeltaInodes "$broray_tx_tmp_snapshot_inode_delta" \
        --argjson sharedRequiredKB "$broray_tx_shared_required" \
        --argjson sharedRequiredInodes "$broray_tx_shared_inode_required" \
        --argjson sharedPhaseDeltaKB "$broray_tx_shared_phase_delta" \
        --argjson sharedPhaseDeltaInodes "$broray_tx_shared_phase_inode_delta" \
        --arg optFsIdentity "$(jq -r '.mountGraph.opt.identity // .mountGraph.opt // ""' "$BRORAY_TX_WORK/evidence/capabilities.json")" \
        --arg tmpFsIdentity "$(jq -r '.mountGraph.tmp.identity // .mountGraph.tmp // ""' "$BRORAY_TX_WORK/evidence/capabilities.json")" \
        '{schemaVersion:5,contract:$spaceContract,requirementsContract:$requirementsContract,
          lifecycleContract:$lifecycleContract,capabilityContract:$capabilityContract,spaceContract:$spaceContract,
          operationId:$operationId,previousIpkRequired:false,historicalTransactionStateRequired:false,
          statelessBootstrap:true,unit:"KiB",candidateSha256:$candidateSha256,
          sameBackingFs:$sameBackingFs,optFsIdentity:$optFsIdentity,tmpFsIdentity:$tmpFsIdentity,
          optFreeKB:$optFreeKB,tmpTotalKB:$tmpTotalKB,tmpFreeKB:$tmpFreeKB,tmpExistingUsageKB:$tmpExistingUsageKB,
          optFreeInodes:$optFreeInodes,tmpFreeInodes:$tmpFreeInodes,optRequiredInodes:$optRequiredInodes,tmpRequiredInodes:$tmpRequiredInodes,
          sourceScopeKB:$sourceScopeKB,sourceBytes:$sourceBytes,sourceScopeUniqueObjects:$sourceScopeUniqueObjects,
          sourceReclaimableInodes:$sourceReclaimableInodes,sourceInternalHardlinkGroups:$sourceInternalHardlinkGroups,
          sourceExternalHardlinkGroups:$sourceExternalHardlinkGroups,
          sourceScopeDuplicates:$sourceScopeDuplicates,reclaimableSourceKB:$sourceScopeKB,protectedSourceKB:$protectedSourceKB,
          protectedSourceObjects:$protectedSourceObjects,
          candidateDeclaredBytes:$candidateDeclaredBytes,targetAllocatedUpperKB:$targetAllocatedUpperKB,targetObjectCount:$targetObjectCount,
          targetProtectedOverlapAllocatedUpperKB:$targetProtectedOverlapAllocatedUpperKB,
          targetProtectedOverlapObjects:$targetProtectedOverlapObjects,
          targetNonProtectedAllocatedUpperKB:$targetNonProtectedAllocatedUpperKB,
          targetNonProtectedObjectCount:$targetNonProtectedObjectCount,
          optAllocationUnitKB:$optAllocationUnitKB,tmpAllocationUnitKB:$tmpAllocationUnitKB,
          capsuleBytes:$capsuleBytes,capsuleObjects:$capsuleObjects,capsuleRestoreUpperKB:$capsuleRestoreUpperKB,
          sourceRestoreUpperKB:$sourceRestoreUpperKB,
          optForwardExtractionPeakKB:$optForwardExtractionPeakKB,optForwardRestoredPeakKB:$optForwardRestoredPeakKB,
          optForwardPeakKB:$optForwardPeakKB,optRollbackPeakKB:$optRollbackPeakKB,
          optForwardDeltaKB:$optForwardDeltaKB,optRollbackDeltaKB:$optRollbackDeltaKB,
          optForwardExtractionPeakInodes:$optForwardExtractionPeakInodes,
          optForwardRestoredPeakInodes:$optForwardRestoredPeakInodes,
          optForwardPeakInodes:$optForwardPeakInodes,optRollbackPeakInodes:$optRollbackPeakInodes,
          optForwardInodeDelta:$optForwardInodeDelta,optRollbackInodeDelta:$optRollbackInodeDelta,
          opkgMetadataUpperKB:$opkgMetadataUpperKB,opkgStatusTransientUpperKB:$opkgStatusTransientUpperKB,
          optRequiredKB:$optRequiredKB,tmpRequiredKB:$tmpRequiredKB,
          sharedFreeKB:(if $sameBackingFs then $optFreeKB else null end),
          sharedFreeInodes:(if $sameBackingFs then $optFreeInodes else null end),
          sharedRequiredKB:(if $sameBackingFs then $sharedRequiredKB else null end),
          sharedRequiredInodes:(if $sameBackingFs then $sharedRequiredInodes else null end),
          sharedPhaseDeltaKB:(if $sameBackingFs then $sharedPhaseDeltaKB else null end),
          sharedPhaseDeltaInodes:(if $sameBackingFs then $sharedPhaseDeltaInodes else null end),
          workspaceOverheadKB:$workspaceOverheadKB,tmpSafetyReserveKB:$tmpSafetyReserveKB,optSafetyReserveKB:$optSafetyReserveKB,
          evidenceGrowthCapKB:$evidenceGrowthCapKB,setupGrowthCapKB:$setupGrowthCapKB,
          setupInodeCap:$setupInodeCap,setupAllocatedUpperKB:$setupAllocatedUpperKB,
          setupOutputEachCapKB:$setupOutputEachCapKB,setupOutputAllocatedUpperEachKB:$setupOutputAllocatedUpperEachKB,
          setupFileLimitBlocks512:$setupFileLimitBlocks512,setupWritableRegularCap:$setupWritableRegularCap,
          setupWriteContract:$setupWriteContract,durableEvidenceCapKB:$durableEvidenceCapKB,
          durableEvidenceFileCap:$durableEvidenceFileCap,durableEvidenceInodeCap:$durableEvidenceInodeCap,
          durableOptAllocatedUpperKB:$durableOptAllocatedUpperKB,durableTmpAllocatedUpperEachKB:$durableTmpAllocatedUpperEachKB,
          postCandidateWriterUpperKB:$postCandidateWriterUpperKB,opkgMetadataInodeCap:$opkgMetadataInodeCap,
          tarUpperBoundBytes:$tarUpperBoundBytes,gzipUpperBoundBytes:$gzipUpperBoundBytes,
          snapshotArchiveLimitKB:$snapshotArchiveLimitKB,snapshotAllocatedUpperKB:$snapshotAllocatedUpperKB,
          snapshotMetadataUpperKB:$snapshotMetadataUpperKB,snapshotActualBytes:null,snapshotActualKB:null,
          snapshotCompressionRatio:null,snapshotCompressionRatioUse:"observed-only-not-space-check-input",
          snapshotCreationAlgorithm:"bounded-fifo-tar-gzip-direct",uncompressedSourceCopiesInTmp:0,
          candidateBytes:null,candidateActualKB:null,candidateStagingActualKB:null,
          calculatedPeakTmpKB:null,measuredPeakTmpKB:$workspaceOverheadKB,tmpSpaceMarginKB:null,
          spaceCheckResult:$spaceCheckResult,mutationStartedAtSpaceFailure:false,
          phases:[
            {stage:"snapshot-create",optDeltaKB:0,tmpDeltaKB:$tmpSnapshotDeltaKB,optDeltaInodes:0,tmpDeltaInodes:$tmpSnapshotDeltaInodes},
            {stage:"forward",optPeakKB:$optForwardPeakKB,optRequiredKB:$optRequiredKB,optRequiredInodes:$optRequiredInodes},
            {stage:"rollback",ordering:"delete-candidate-before-source-restore",sourceRestoreUpperKB:$sourceRestoreUpperKB,
             optPeakKB:$optRollbackPeakKB,optDeltaKB:$optRollbackDeltaKB}
          ]}' \
        >"$BRORAY_TX_WORK/evidence/space.json" || return 1
    printf '%s\n' "$broray_tx_tmp_free" >"$BRORAY_TX_WORK/tmp-free-at-space-check.kb"
    broray_tx_tmp_operation_capacity="$(broray_tx_uadd "$broray_tx_tmp_free" "$broray_tx_workspace_overhead")" || return 1
    printf '%s\n' "$broray_tx_tmp_operation_capacity" >"$BRORAY_TX_WORK/tmp-operation-capacity.kb"
    printf '%s\n' "$broray_tx_snapshot_limit_kb" >"$BRORAY_TX_WORK/snapshot-archive-limit.kb"
    printf '%s\n' "$broray_tx_workspace_overhead" >"$BRORAY_TX_WORK/measured-peak-tmp.kb"
    printf 'space-check\t%s\n' "$broray_tx_workspace_overhead" >"$BRORAY_TX_WORK/evidence/tmp-usage.tsv"
    : >"$BRORAY_TX_WORK/tmp-work-baseline.kb" || return 1
    broray_tx_tmp_work_baseline="$(du -sk "$BRORAY_TX_WORK" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_tmp_work_baseline" || return 1
    printf '%s\n' "$broray_tx_tmp_work_baseline" >"$BRORAY_TX_WORK/tmp-work-baseline.kb" || return 1
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_opt_free" -ge "$broray_tx_shared_required" ] || broray_tx_fail insufficient-shared-space || return 1
        [ "$broray_tx_opt_free_inodes" -ge "$broray_tx_shared_inode_required" ] || broray_tx_fail insufficient-shared-inodes || return 1
    else
        [ "$broray_tx_opt_free" -ge "$broray_tx_opt_required" ] || broray_tx_fail insufficient-opt-space || return 1
        [ "$broray_tx_tmp_free" -ge "$broray_tx_tmp_required" ] || broray_tx_fail insufficient-tmp-space || return 1
        [ "$broray_tx_opt_free_inodes" -ge "$broray_tx_opt_inode_required" ] || broray_tx_fail insufficient-opt-inodes || return 1
        [ "$broray_tx_tmp_free_inodes" -ge "$broray_tx_tmp_inode_required" ] || broray_tx_fail insufficient-tmp-inodes || return 1
    fi
    broray_tx_inject space-check || return 1
    broray_tx_status running ''
}

broray_tx_tmp_measure()
{
    broray_tx_measure_stage="$1"
    [ -d "$BRORAY_TX_WORK" ] && [ ! -L "$BRORAY_TX_WORK" ] || return 1
    broray_tx_measure_kb="$(du -sk "$BRORAY_TX_WORK" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_measure_kb" || return 1
    broray_tx_peak_kb="$(cat "$BRORAY_TX_WORK/measured-peak-tmp.kb" 2>/dev/null)"
    broray_tx_number "$broray_tx_peak_kb" || broray_tx_peak_kb=0
    if [ "$broray_tx_measure_kb" -gt "$broray_tx_peak_kb" ]; then
        broray_tx_peak_kb="$broray_tx_measure_kb"
        printf '%s\n' "$broray_tx_peak_kb" >"$BRORAY_TX_WORK/measured-peak-tmp.kb" || return 1
    fi
    printf '%s\t%s\n' "$broray_tx_measure_stage" "$broray_tx_measure_kb" >>"$BRORAY_TX_WORK/evidence/tmp-usage.tsv" || return 1
    if [ -f "$BRORAY_TX_WORK/evidence/space.json" ]; then
        jq --argjson measuredPeakTmpKB "$broray_tx_peak_kb" '.measuredPeakTmpKB=$measuredPeakTmpKB' \
            "$BRORAY_TX_WORK/evidence/space.json" >"$BRORAY_TX_WORK/evidence/space.json.part" || return 1
        mv -f "$BRORAY_TX_WORK/evidence/space.json.part" "$BRORAY_TX_WORK/evidence/space.json" || return 1
    fi
}

broray_tx_space_snapshot_finalize()
{
    broray_tx_snapshot_bytes="$(wc -c <"$BRORAY_TX_WORK/backup.tar.gz" | tr -d ' ')"
    broray_tx_number "$broray_tx_snapshot_bytes" || return 1
    broray_tx_snapshot_kb="$(broray_tx_ceil_div "$broray_tx_snapshot_bytes" 1024)" || return 1
    broray_tx_snapshot_allocated_kb="$(du -sk "$BRORAY_TX_WORK/backup.tar.gz" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_snapshot_allocated_kb" || return 1
    broray_tx_work_kb="$(du -sk "$BRORAY_TX_WORK" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_work_kb" || return 1
    broray_tx_workspace_overhead="$(broray_tx_sub0 "$broray_tx_work_kb" "$broray_tx_snapshot_allocated_kb")" || return 1
    broray_tx_initial_work_kb="$(cat "$BRORAY_TX_WORK/tmp-work-baseline.kb" 2>/dev/null)"
    broray_tx_number "$broray_tx_initial_work_kb" || return 1
    broray_tx_snapshot_phase_growth="$(broray_tx_sub0 "$broray_tx_work_kb" "$broray_tx_initial_work_kb")" || return 1
    broray_tx_snapshot_nonarchive_growth="$(broray_tx_sub0 "$broray_tx_snapshot_phase_growth" "$broray_tx_snapshot_allocated_kb")" || return 1
    broray_tx_capsule_restore_kb="$(jq -r '.capsuleRestoreUpperKB' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_number "$broray_tx_capsule_restore_kb" || return 1
    broray_tx_snapshot_nonarchive_cap="$(broray_tx_uadd "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB" "$broray_tx_capsule_restore_kb")" || return 1
    if [ "${BRORAY_TX_TEST_FORCE_TMP_CAP_FAILURE:-0}" = 1 ] ||
       [ "$broray_tx_snapshot_nonarchive_growth" -gt "$broray_tx_snapshot_nonarchive_cap" ]; then
        broray_tx_fail snapshot-workspace-writer-cap-exceeded
        return 1
    fi
    # T1 is rebased after full snapshot verification: snapshot and existing
    # evidence are already reflected in Available.  Only the exact declared
    # candidate plus simultaneous validation allocations are future deltas.
    broray_tx_same_fs="$(jq -r 'if (.sameBackingFs|type)=="boolean" then (.sameBackingFs|tostring) else empty end' "$BRORAY_TX_WORK/evidence/space.json")"
    case "$broray_tx_same_fs" in true|false) ;; *) return 1 ;; esac
    broray_tx_tmp_free_after_snapshot="${BRORAY_TX_TEST_TMP_FREE_AFTER_SNAPSHOT_KB:-$(df -Pk "$BRORAY_TX_TMP_BASE" 2>/dev/null | awk 'NR==2{print $4;exit}')}"
    broray_tx_tmp_inodes_after_snapshot="${BRORAY_TX_TEST_TMP_FREE_AFTER_SNAPSHOT_INODES:-$(broray_tx_df_inodes "$BRORAY_TX_TMP_BASE")}"
    if [ "$broray_tx_same_fs" = true ]; then
        broray_tx_opt_free_after_snapshot="${BRORAY_TX_TEST_OPT_FREE_AFTER_SNAPSHOT_KB:-$(df -Pk "$BRORAY_TX_OPT_ROOT" 2>/dev/null | awk 'NR==2{print $4;exit}')}"
        broray_tx_opt_inodes_after_snapshot="${BRORAY_TX_TEST_OPT_FREE_AFTER_SNAPSHOT_INODES:-$(broray_tx_df_inodes "$BRORAY_TX_OPT_ROOT")}"
    else
        broray_tx_opt_free_after_snapshot=0
        broray_tx_opt_inodes_after_snapshot=0
    fi
    broray_tx_number "$broray_tx_tmp_free_after_snapshot" && broray_tx_number "$broray_tx_tmp_inodes_after_snapshot" &&
        broray_tx_number "$broray_tx_opt_free_after_snapshot" && broray_tx_number "$broray_tx_opt_inodes_after_snapshot" || return 1
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_opt_free_after_snapshot" = "$broray_tx_tmp_free_after_snapshot" ] &&
            [ "$broray_tx_opt_inodes_after_snapshot" = "$broray_tx_tmp_inodes_after_snapshot" ] ||
            broray_tx_fail same-backing-fs-samples-disagree-after-snapshot || return 1
    fi
    broray_tx_candidate_declared_bytes="$(jq -r '.sizeBytes' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_target_alloc_kb="$(jq -r '.targetAllocatedUpperKB' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_target_objects="$(jq -r '.payloadObjectCount' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_outer_declared_bytes="$(jq -r '.outerMembersBytes' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_outer_objects="$(jq -r '.outerMemberCount' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_control_alloc_kb="$(jq -r '.controlAllocatedUpperKB' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_control_objects="$(jq -r '.controlObjectCount' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_number "$broray_tx_candidate_declared_bytes" && broray_tx_number "$broray_tx_target_alloc_kb" &&
        broray_tx_number "$broray_tx_target_objects" && broray_tx_number "$broray_tx_outer_declared_bytes" &&
        broray_tx_number "$broray_tx_outer_objects" && broray_tx_number "$broray_tx_control_alloc_kb" &&
        broray_tx_number "$broray_tx_control_objects" || return 1
    broray_tx_tmp_allocation_unit_kb="$(jq -r '.mountGraph.tmp.allocationUnitKB // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    broray_tx_number "$broray_tx_tmp_allocation_unit_kb" && [ "$broray_tx_tmp_allocation_unit_kb" -gt 0 ] || return 1
    broray_tx_target_alloc_kb="$(broray_tx_adjust_allocation_upper_kb "$broray_tx_target_alloc_kb" \
        "$broray_tx_target_objects" "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_control_alloc_kb="$(broray_tx_adjust_allocation_upper_kb "$broray_tx_control_alloc_kb" \
        "$broray_tx_control_objects" "$broray_tx_tmp_allocation_unit_kb")" || return 1
    # Extracted staging trees include their own root directory, which is not
    # a payload/control manifest member.  Charge one measured allocation unit
    # for each root; candidate_space_finalize enforces these same bounds.
    broray_tx_target_staging_alloc_kb="$(broray_tx_uadd "$broray_tx_target_alloc_kb" \
        "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_control_staging_alloc_kb="$(broray_tx_uadd "$broray_tx_control_alloc_kb" \
        "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_candidate_declared_kb="$(broray_tx_ceil_div "$broray_tx_candidate_declared_bytes" 1024)" || return 1
    broray_tx_candidate_alloc_kb="$(broray_tx_dense_bytes_upper_kb "$broray_tx_candidate_declared_bytes" \
        "$broray_tx_tmp_allocation_unit_kb")" || return 1
    # The portable FIFO writer uses 1-KiB dd records.  It rejects an actual
    # byte count above Size after the bounded write, while the phase plan must
    # still reserve the allocation of the final partial record (at most 1023
    # extra bytes) before that rejection.
    broray_tx_candidate_writer_upper_bytes="$(broray_tx_umul "$broray_tx_candidate_declared_kb" 1024)" || return 1
    broray_tx_candidate_writer_alloc_kb="$(broray_tx_dense_bytes_upper_kb "$broray_tx_candidate_writer_upper_bytes" \
        "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_outer_alloc_kb="$(broray_tx_dense_bytes_upper_kb "$broray_tx_outer_declared_bytes" \
        "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_outer_metadata_kb="$(broray_tx_umul "$broray_tx_outer_objects" "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_outer_alloc_kb="$(broray_tx_uadd "$broray_tx_outer_alloc_kb" "$broray_tx_outer_metadata_kb")" || return 1
    broray_tx_future_validation_kb="$(broray_tx_uadd "$broray_tx_candidate_writer_alloc_kb" "$broray_tx_outer_alloc_kb")" || return 1
    broray_tx_future_validation_kb="$(broray_tx_uadd "$broray_tx_future_validation_kb" "$broray_tx_control_staging_alloc_kb")" || return 1
    broray_tx_future_validation_kb="$(broray_tx_uadd "$broray_tx_future_validation_kb" "$broray_tx_target_staging_alloc_kb")" || return 1
    broray_tx_future_validation_kb="$(broray_tx_uadd "$broray_tx_future_validation_kb" "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB")" || return 1
    broray_tx_future_validation_kb="$(broray_tx_uadd "$broray_tx_future_validation_kb" "$BRORAY_TX_TMP_RESERVE_KB")" || return 1
    broray_tx_post_candidate_writer_upper_kb="$(jq -r '.postCandidateWriterUpperKB' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_number "$broray_tx_post_candidate_writer_upper_kb" || return 1
    # T1 must cover both maxima: expanded candidate validation, and the later
    # compacted candidate plus setup/durable writers.  Candidate ancillary
    # evidence can already occupy its full cap when the latter phase begins,
    # so it is distinct from the future generic-evidence term.
    broray_tx_post_candidate_peak_kb="$(broray_tx_uadd "$broray_tx_candidate_writer_alloc_kb" "$broray_tx_outer_alloc_kb")" || return 1
    broray_tx_post_candidate_peak_kb="$(broray_tx_uadd "$broray_tx_post_candidate_peak_kb" "$broray_tx_control_staging_alloc_kb")" || return 1
    broray_tx_post_candidate_peak_kb="$(broray_tx_uadd "$broray_tx_post_candidate_peak_kb" "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB")" || return 1
    broray_tx_post_candidate_peak_kb="$(broray_tx_uadd "$broray_tx_post_candidate_peak_kb" "$broray_tx_post_candidate_writer_upper_kb")" || return 1
    broray_tx_post_candidate_peak_kb="$(broray_tx_uadd "$broray_tx_post_candidate_peak_kb" "$BRORAY_TX_TMP_RESERVE_KB")" || return 1
    broray_tx_future_validation_objects="$(broray_tx_uadd "$broray_tx_target_objects" "$broray_tx_control_objects")" || return 1
    broray_tx_future_validation_objects="$(broray_tx_uadd "$broray_tx_future_validation_objects" "$broray_tx_outer_objects")" || return 1
    broray_tx_future_validation_objects="$(broray_tx_uadd "$broray_tx_future_validation_objects" "$BRORAY_TX_CANDIDATE_FIXED_FUTURE_INODES")" || return 1
    broray_tx_tmp_phase_inode_delta="$(broray_tx_umax "$broray_tx_future_validation_objects" "$BRORAY_TX_POST_CANDIDATE_FUTURE_INODES")" || return 1
    broray_tx_future_validation_inodes="$(broray_tx_uadd "$broray_tx_tmp_phase_inode_delta" "$BRORAY_TX_TMP_INODE_RESERVE")" || return 1
    broray_tx_calculated_peak="$(broray_tx_umax "$broray_tx_future_validation_kb" "$broray_tx_post_candidate_peak_kb")" || return 1
    broray_tx_margin=$((broray_tx_tmp_free_after_snapshot - broray_tx_calculated_peak))
    broray_tx_inode_margin=$((broray_tx_tmp_inodes_after_snapshot - broray_tx_future_validation_inodes))
    broray_tx_shared_required_after_snapshot=0
    broray_tx_shared_inode_required_after_snapshot=0
    broray_tx_shared_margin_after_snapshot=0
    broray_tx_shared_inode_margin_after_snapshot=0
    broray_tx_shared_validation_delta=0
    broray_tx_shared_mutation_delta=0
    broray_tx_shared_validation_inode_delta=0
    broray_tx_shared_mutation_inode_delta=0
    if [ "$broray_tx_same_fs" = true ]; then
        broray_tx_validation_tmp_delta="$(broray_tx_usub "$broray_tx_future_validation_kb" "$BRORAY_TX_TMP_RESERVE_KB")" || return 1
        broray_tx_mutation_tmp_delta="$(broray_tx_usub "$broray_tx_post_candidate_peak_kb" "$BRORAY_TX_TMP_RESERVE_KB")" || return 1
        broray_tx_opt_peak_delta="$(jq -r '([.optForwardDeltaKB,.optRollbackDeltaKB]|max)' "$BRORAY_TX_WORK/evidence/space.json")"
        broray_tx_opt_inode_peak_delta="$(jq -r '([.optForwardInodeDelta,.optRollbackInodeDelta]|max)' "$BRORAY_TX_WORK/evidence/space.json")"
        broray_tx_number "$broray_tx_opt_peak_delta" && broray_tx_number "$broray_tx_opt_inode_peak_delta" || return 1
        broray_tx_shared_validation_delta="$broray_tx_validation_tmp_delta"
        broray_tx_shared_mutation_delta="$(broray_tx_uadd "$broray_tx_opt_peak_delta" "$broray_tx_mutation_tmp_delta")" || return 1
        broray_tx_shared_phase_delta="$(broray_tx_umax "$broray_tx_shared_validation_delta" "$broray_tx_shared_mutation_delta")" || return 1
        broray_tx_shared_required_after_snapshot="$(broray_tx_uadd "$BRORAY_TX_OPT_RESERVE_KB" "$BRORAY_TX_TMP_RESERVE_KB")" || return 1
        broray_tx_shared_required_after_snapshot="$(broray_tx_uadd "$broray_tx_shared_required_after_snapshot" "$broray_tx_shared_phase_delta")" || return 1
        broray_tx_shared_validation_inode_delta="$broray_tx_future_validation_objects"
        broray_tx_shared_mutation_inode_delta="$(broray_tx_uadd "$broray_tx_opt_inode_peak_delta" "$BRORAY_TX_POST_CANDIDATE_FUTURE_INODES")" || return 1
        broray_tx_shared_phase_inode_delta="$(broray_tx_umax "$broray_tx_shared_validation_inode_delta" "$broray_tx_shared_mutation_inode_delta")" || return 1
        broray_tx_shared_inode_required_after_snapshot="$(broray_tx_uadd "$BRORAY_TX_OPT_INODE_RESERVE" "$BRORAY_TX_TMP_INODE_RESERVE")" || return 1
        broray_tx_shared_inode_required_after_snapshot="$(broray_tx_uadd "$broray_tx_shared_inode_required_after_snapshot" "$broray_tx_shared_phase_inode_delta")" || return 1
        broray_tx_shared_margin_after_snapshot=$((broray_tx_opt_free_after_snapshot - broray_tx_shared_required_after_snapshot))
        broray_tx_shared_inode_margin_after_snapshot=$((broray_tx_opt_inodes_after_snapshot - broray_tx_shared_inode_required_after_snapshot))
    fi
    broray_tx_source_bytes="$(jq -r '.sourceBytes' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_number "$broray_tx_source_bytes" || return 1
    broray_tx_compression_ratio="$(awk -v snapshot="$broray_tx_snapshot_bytes" -v source="$broray_tx_source_bytes" 'BEGIN{if(source>0)printf "%.6f",snapshot/source;else printf "0"}')"
    broray_tx_space_result=PASS
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_shared_margin_after_snapshot" -ge 0 ] || broray_tx_space_result=FAIL
        [ "$broray_tx_shared_inode_margin_after_snapshot" -ge 0 ] || broray_tx_space_result=FAIL
    else
        [ "$broray_tx_margin" -ge 0 ] || broray_tx_space_result=FAIL
        [ "$broray_tx_inode_margin" -ge 0 ] || broray_tx_space_result=FAIL
    fi
    jq --argjson snapshotActualBytes "$broray_tx_snapshot_bytes" --argjson snapshotActualKB "$broray_tx_snapshot_kb" \
        --argjson snapshotActualAllocatedKB "$broray_tx_snapshot_allocated_kb" \
        --argjson snapshotNonarchiveGrowthKB "$broray_tx_snapshot_nonarchive_growth" \
        --argjson candidateAllocatedUpperKB "$broray_tx_candidate_alloc_kb" \
        --argjson candidateDownloadWriterUpperBytes "$broray_tx_candidate_writer_upper_bytes" \
        --argjson candidateDownloadWriterAllocatedUpperKB "$broray_tx_candidate_writer_alloc_kb" \
        --argjson outerMembersAllocatedUpperKB "$broray_tx_outer_alloc_kb" \
        --argjson controlAllocatedUpperKB "$broray_tx_control_alloc_kb" \
        --argjson controlStagingAllocatedUpperKB "$broray_tx_control_staging_alloc_kb" \
        --argjson targetStagingAllocatedUpperKB "$broray_tx_target_staging_alloc_kb" \
        --argjson candidateValidationPeakUpperKB "$broray_tx_future_validation_kb" \
        --argjson postCandidatePeakUpperKB "$broray_tx_post_candidate_peak_kb" \
        --argjson snapshotCompressionRatio "$broray_tx_compression_ratio" --argjson workspaceOverheadKB "$broray_tx_workspace_overhead" \
        --argjson calculatedPeakTmpKB "$broray_tx_calculated_peak" --argjson tmpSpaceMarginKB "$broray_tx_margin" \
        --argjson tmpFreeAfterSnapshotKB "$broray_tx_tmp_free_after_snapshot" --argjson tmpFreeInodesAfterSnapshot "$broray_tx_tmp_inodes_after_snapshot" \
        --argjson tmpRequiredInodesAfterSnapshot "$broray_tx_future_validation_inodes" --argjson tmpInodeMarginAfterSnapshot "$broray_tx_inode_margin" \
        --argjson sharedFreeAfterSnapshotKB "$broray_tx_opt_free_after_snapshot" \
        --argjson sharedFreeInodesAfterSnapshot "$broray_tx_opt_inodes_after_snapshot" \
        --argjson sharedRequiredAfterSnapshotKB "$broray_tx_shared_required_after_snapshot" \
        --argjson sharedRequiredInodesAfterSnapshot "$broray_tx_shared_inode_required_after_snapshot" \
        --argjson sharedSpaceMarginAfterSnapshotKB "$broray_tx_shared_margin_after_snapshot" \
        --argjson sharedInodeMarginAfterSnapshot "$broray_tx_shared_inode_margin_after_snapshot" \
        --argjson sharedValidationDeltaKB "$broray_tx_shared_validation_delta" \
        --argjson sharedMutationDeltaKB "$broray_tx_shared_mutation_delta" \
        --argjson sharedValidationDeltaInodes "$broray_tx_shared_validation_inode_delta" \
        --argjson sharedMutationDeltaInodes "$broray_tx_shared_mutation_inode_delta" \
        --argjson sameBackingFs "$broray_tx_same_fs" \
        --arg spaceCheckResult "$broray_tx_space_result" \
        '.snapshotActualBytes=$snapshotActualBytes|.snapshotActualKB=$snapshotActualKB|.snapshotActualAllocatedKB=$snapshotActualAllocatedKB|
         .snapshotNonarchiveGrowthKB=$snapshotNonarchiveGrowthKB|.snapshotCompressionRatio=$snapshotCompressionRatio|
         .candidateAllocatedUpperKB=$candidateAllocatedUpperKB|
         .candidateDownloadWriterUpperBytes=$candidateDownloadWriterUpperBytes|
         .candidateDownloadWriterAllocatedUpperKB=$candidateDownloadWriterAllocatedUpperKB|
         .outerMembersAllocatedUpperKB=$outerMembersAllocatedUpperKB|
         .controlAllocatedUpperKB=$controlAllocatedUpperKB|
         .controlStagingAllocatedUpperKB=$controlStagingAllocatedUpperKB|
         .targetStagingAllocatedUpperKB=$targetStagingAllocatedUpperKB|
         .candidateValidationPeakUpperKB=$candidateValidationPeakUpperKB|
         .postCandidatePeakUpperKB=$postCandidatePeakUpperKB|
         .workspaceOverheadKB=$workspaceOverheadKB|.calculatedPeakTmpKB=$calculatedPeakTmpKB|.tmpSpaceMarginKB=$tmpSpaceMarginKB|
         .tmpFreeAfterSnapshotKB=$tmpFreeAfterSnapshotKB|
         .tmpFreeInodesAfterSnapshot=$tmpFreeInodesAfterSnapshot|.tmpRequiredInodesAfterSnapshot=$tmpRequiredInodesAfterSnapshot|
         .tmpInodeMarginAfterSnapshot=$tmpInodeMarginAfterSnapshot|
         .sharedFreeAfterSnapshotKB=(if $sameBackingFs then $sharedFreeAfterSnapshotKB else null end)|
         .sharedFreeInodesAfterSnapshot=(if $sameBackingFs then $sharedFreeInodesAfterSnapshot else null end)|
         .sharedRequiredAfterSnapshotKB=(if $sameBackingFs then $sharedRequiredAfterSnapshotKB else null end)|
         .sharedRequiredInodesAfterSnapshot=(if $sameBackingFs then $sharedRequiredInodesAfterSnapshot else null end)|
         .sharedSpaceMarginAfterSnapshotKB=(if $sameBackingFs then $sharedSpaceMarginAfterSnapshotKB else null end)|
         .sharedInodeMarginAfterSnapshot=(if $sameBackingFs then $sharedInodeMarginAfterSnapshot else null end)|
         .sharedValidationDeltaKB=(if $sameBackingFs then $sharedValidationDeltaKB else null end)|
         .sharedMutationDeltaKB=(if $sameBackingFs then $sharedMutationDeltaKB else null end)|
         .sharedValidationDeltaInodes=(if $sameBackingFs then $sharedValidationDeltaInodes else null end)|
         .sharedMutationDeltaInodes=(if $sameBackingFs then $sharedMutationDeltaInodes else null end)|
         .spaceCheckResult=$spaceCheckResult' "$BRORAY_TX_WORK/evidence/space.json" >"$BRORAY_TX_WORK/evidence/space.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/evidence/space.json.part" "$BRORAY_TX_WORK/evidence/space.json" || return 1
    if { [ "$broray_tx_same_fs" = true ] && { [ "$broray_tx_shared_margin_after_snapshot" -lt 0 ] || [ "$broray_tx_shared_inode_margin_after_snapshot" -lt 0 ]; }; } ||
       { [ "$broray_tx_same_fs" = false ] && { [ "$broray_tx_margin" -lt 0 ] || [ "$broray_tx_inode_margin" -lt 0 ]; }; }; then
        rm -f "$BRORAY_TX_WORK/backup.tar.gz" "$BRORAY_TX_WORK/backup.tar.gz.sha256"
        if [ "$broray_tx_same_fs" = true ]; then
            broray_tx_fail insufficient-shared-space-after-streamed-snapshot
        else
            broray_tx_fail insufficient-tmp-space-after-streamed-snapshot
        fi
        return 1
    fi
    : >"$BRORAY_TX_WORK/tmp-work-after-snapshot.kb" || return 1
    broray_tx_work_after_snapshot="$(du -sk "$BRORAY_TX_WORK" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_work_after_snapshot" || return 1
    printf '%s\n' "$broray_tx_work_after_snapshot" >"$BRORAY_TX_WORK/tmp-work-after-snapshot.kb" || return 1
    broray_tx_tmp_measure snapshot-created
}

broray_tx_relative_safe()
{
    case "${1:-}" in
        ''|/*|../*|*/../*|*/..|*'|'*|*'\t'*|*'\n'*|*'\r'*) return 1 ;;
    esac
}

broray_tx_object_type()
{
    if [ -L "$1" ]; then printf '%s\n' symlink
    elif [ -f "$1" ]; then printf '%s\n' regular
    elif [ -d "$1" ]; then printf '%s\n' directory
    elif [ -p "$1" ]; then printf '%s\n' fifo
    elif [ -b "$1" ]; then printf '%s\n' block-device
    elif [ -c "$1" ]; then printf '%s\n' character-device
    else printf '%s\n' unsupported
    fi
}

broray_tx_manifest_failure()
{
    BRORAY_TX_MANIFEST_FAILURE_REASON="$1"
    BRORAY_TX_MANIFEST_FAILURE_PATH="${2:-unknown}"
    BRORAY_TX_MANIFEST_FAILURE_TYPE="${3:-unknown}"
    BRORAY_TX_MANIFEST_FAILURE_OPERATION="${4:-manifest-build}"
    if [ -n "$BRORAY_TX_WORK" ] && [ -d "$BRORAY_TX_WORK/evidence" ]; then
        jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" \
            --arg path "$BRORAY_TX_MANIFEST_FAILURE_PATH" \
            --arg objectType "$BRORAY_TX_MANIFEST_FAILURE_TYPE" \
            --arg reason "$BRORAY_TX_MANIFEST_FAILURE_REASON" \
            --arg operation "$BRORAY_TX_MANIFEST_FAILURE_OPERATION" \
            '{schemaVersion:1,status:"FAIL",stage:"source-manifest",operationId:$operationId,
              path:$path,objectType:$objectType,reason:$reason,operation:$operation,
              versionDependent:false,mutationStarted:false}' \
            >"$BRORAY_TX_WORK/evidence/manifest-failure.json.part" 2>/dev/null &&
            mv -f "$BRORAY_TX_WORK/evidence/manifest-failure.json.part" \
                "$BRORAY_TX_WORK/evidence/manifest-failure.json" 2>/dev/null || true
    fi
    return 1
}

broray_tx_manifest_build()
{
    broray_tx_manifest_root="$1"; broray_tx_manifest_scope="$2"; broray_tx_manifest_out="$3"
    BRORAY_TX_MANIFEST_FAILURE_REASON=""; BRORAY_TX_MANIFEST_FAILURE_PATH=""
    : >"$broray_tx_manifest_out.paths" || return 1
    while IFS= read -r broray_tx_manifest_rel; do
        [ -n "$broray_tx_manifest_rel" ] || continue
        broray_tx_relative_safe "$broray_tx_manifest_rel" || {
            broray_tx_manifest_failure unsafe-scope-relative-path "$broray_tx_manifest_rel" scope validate-path
            return 1
        }
        broray_tx_manifest_abs="${broray_tx_manifest_root%/}/$broray_tx_manifest_rel"
        [ -e "$broray_tx_manifest_abs" ] || [ -L "$broray_tx_manifest_abs" ] || {
            broray_tx_manifest_failure scope-object-missing "$broray_tx_manifest_rel" missing enumerate
            return 1
        }
        find -P "$broray_tx_manifest_abs" -xdev -print >>"$broray_tx_manifest_out.paths" 2>"$BRORAY_TX_WORK/evidence/manifest-find.stderr" || {
            broray_tx_manifest_failure find-tree-enumeration-failed "$broray_tx_manifest_rel" "$(broray_tx_object_type "$broray_tx_manifest_abs")" 'find -P -xdev -print'
            return 1
        }
    done <"$broray_tx_manifest_scope"
    broray_tx_sort_file unique "$broray_tx_manifest_out.paths" "$broray_tx_manifest_out.paths" || {
        broray_tx_manifest_failure deterministic-sort-failed unknown scope 'sort -u redirected-output'
        return 1
    }
    : >"$broray_tx_manifest_out.part" || return 1
    while IFS= read -r broray_tx_manifest_abs; do
        broray_tx_manifest_rel="${broray_tx_manifest_abs#"${broray_tx_manifest_root%/}"/}"
        broray_tx_relative_safe "$broray_tx_manifest_rel" || {
            broray_tx_manifest_failure unsafe-enumerated-relative-path "$broray_tx_manifest_rel" unknown validate-path
            return 1
        }
        broray_tx_manifest_meta="$(find -P "$broray_tx_manifest_abs" -maxdepth 0 -printf '%m|%U|%G' 2>/dev/null)" || return 1
        case "$broray_tx_manifest_meta" in *'|'*'|'*) ;; *) return 1 ;; esac
        broray_tx_manifest_mode="${broray_tx_manifest_meta%%|*}"
        broray_tx_manifest_meta_tail="${broray_tx_manifest_meta#*|}"
        broray_tx_manifest_uid="${broray_tx_manifest_meta_tail%%|*}"
        broray_tx_manifest_gid="${broray_tx_manifest_meta_tail#*|}"
        if [ -L "$broray_tx_manifest_abs" ]; then
            broray_tx_manifest_target="$(readlink "$broray_tx_manifest_abs")" || {
                broray_tx_manifest_failure symlink-read-failed "$broray_tx_manifest_rel" symlink readlink
                return 1
            }
            case "$broray_tx_manifest_target" in *'|'*|*'\t'*|*'\n'*|*'\r'*)
                broray_tx_manifest_failure unsafe-symlink-target "$broray_tx_manifest_rel" symlink validate-target
                return 1 ;;
            esac
            printf 'L|%s|%s|%s|%s|%s|%s|%s\n' "$broray_tx_manifest_rel" "${#broray_tx_manifest_target}" \
                "$(printf '%s' "$broray_tx_manifest_target" | sha256sum | awk '{print $1}')" \
                "$broray_tx_manifest_mode" "$broray_tx_manifest_uid" "$broray_tx_manifest_gid" "$broray_tx_manifest_target" \
                >>"$broray_tx_manifest_out.part" || return 1
        elif [ -f "$broray_tx_manifest_abs" ]; then
            broray_tx_manifest_nlink="$(find -P "$broray_tx_manifest_abs" -maxdepth 0 -printf '%n' 2>/dev/null)" || return 1
            broray_tx_number "$broray_tx_manifest_nlink" && [ "$broray_tx_manifest_nlink" -gt 0 ] || return 1
            broray_tx_manifest_bytes="$(wc -c <"$broray_tx_manifest_abs" 2>/dev/null | tr -d ' ')"
            broray_tx_number "$broray_tx_manifest_bytes" || {
                broray_tx_manifest_failure regular-size-read-failed "$broray_tx_manifest_rel" regular 'wc -c'
                return 1
            }
            broray_tx_manifest_sha="$(broray_tx_sha "$broray_tx_manifest_abs")"
            [ "${#broray_tx_manifest_sha}" -eq 64 ] || {
                broray_tx_manifest_failure regular-sha-read-failed "$broray_tx_manifest_rel" regular sha256sum
                return 1
            }
            printf 'F|%s|%s|%s|%s|%s|%s\n' "$broray_tx_manifest_rel" "$broray_tx_manifest_bytes" \
                "$broray_tx_manifest_sha" "$broray_tx_manifest_mode" "$broray_tx_manifest_uid" "$broray_tx_manifest_gid" >>"$broray_tx_manifest_out.part" || return 1
        elif [ -d "$broray_tx_manifest_abs" ]; then
            printf 'D|%s|-|-|%s|%s|%s\n' "$broray_tx_manifest_rel" "$broray_tx_manifest_mode" "$broray_tx_manifest_uid" "$broray_tx_manifest_gid" >>"$broray_tx_manifest_out.part" || return 1
        else
            broray_tx_manifest_failure unsupported-object-type "$broray_tx_manifest_rel" \
                "$(broray_tx_object_type "$broray_tx_manifest_abs")" classify-object
            return 1
        fi
    done <"$broray_tx_manifest_out.paths"
    broray_tx_sort_file plain "$broray_tx_manifest_out.part" "$broray_tx_manifest_out" || {
        broray_tx_manifest_failure deterministic-manifest-sort-failed unknown manifest 'sort redirected-output'
        return 1
    }
    rm -f "$broray_tx_manifest_out.part" "$broray_tx_manifest_out.paths"
    [ -s "$broray_tx_manifest_out" ] || broray_tx_manifest_failure empty-manifest unknown manifest verify-nonempty
}

broray_tx_tree_manifest()
{
    broray_tx_tree_root="$1"; broray_tx_tree_out="$2"
    find -P "$broray_tx_tree_root" -mindepth 1 -xdev -print >"$broray_tx_tree_out.paths" 2>/dev/null || return 1
    broray_tx_sort_file plain "$broray_tx_tree_out.paths" "$broray_tx_tree_out.paths" || return 1
    : >"$broray_tx_tree_out.part" || return 1
    while IFS= read -r broray_tx_tree_abs; do
        broray_tx_tree_rel="${broray_tx_tree_abs#"$broray_tx_tree_root"/}"
        broray_tx_relative_safe "$broray_tx_tree_rel" || return 1
        broray_tx_tree_meta="$(find -P "$broray_tx_tree_abs" -maxdepth 0 -printf '%m|%U|%G' 2>/dev/null)" || return 1
        broray_tx_tree_mode="${broray_tx_tree_meta%%|*}"
        broray_tx_tree_meta_tail="${broray_tx_tree_meta#*|}"
        broray_tx_tree_uid="${broray_tx_tree_meta_tail%%|*}"
        broray_tx_tree_gid="${broray_tx_tree_meta_tail#*|}"
        if [ -L "$broray_tx_tree_abs" ]; then
            broray_tx_tree_target="$(readlink "$broray_tx_tree_abs")" || return 1
            case "$broray_tx_tree_target" in *'|'*|*'\t'*|*'\n'*|*'\r'*) return 1 ;; esac
            printf 'L|%s|%s|%s|%s|%s|%s|%s\n' "$broray_tx_tree_rel" "${#broray_tx_tree_target}" \
                "$(printf '%s' "$broray_tx_tree_target" | sha256sum | awk '{print $1}')" \
                "$broray_tx_tree_mode" "$broray_tx_tree_uid" "$broray_tx_tree_gid" "$broray_tx_tree_target" >>"$broray_tx_tree_out.part" || return 1
        elif [ -f "$broray_tx_tree_abs" ]; then
            [ "$(find -P "$broray_tx_tree_abs" -maxdepth 0 -printf '%n' 2>/dev/null)" = 1 ] || return 1
            printf 'F|%s|%s|%s|%s|%s|%s\n' "$broray_tx_tree_rel" "$(wc -c <"$broray_tx_tree_abs" | tr -d ' ')" \
                "$(broray_tx_sha "$broray_tx_tree_abs")" "$broray_tx_tree_mode" "$broray_tx_tree_uid" "$broray_tx_tree_gid" >>"$broray_tx_tree_out.part" || return 1
        elif [ -d "$broray_tx_tree_abs" ]; then
            printf 'D|%s|-|-|%s|%s|%s\n' "$broray_tx_tree_rel" "$broray_tx_tree_mode" "$broray_tx_tree_uid" "$broray_tx_tree_gid" >>"$broray_tx_tree_out.part" || return 1
        else return 1
        fi
    done <"$broray_tx_tree_out.paths"
    # The traversal input is already canonical path order.  Preserve that
    # order so the runtime manifest is byte-identical to the builder and the
    # detached validator (sorting whole records would group by D/F/L type).
    mv -f "$broray_tx_tree_out.part" "$broray_tx_tree_out" || return 1
    rm -f "$broray_tx_tree_out.paths"
}

broray_tx_tar_safe()
{
    broray_tx_tar_archive="$1"; broray_tx_tar_list="$2"
    tar -tzf "$broray_tx_tar_archive" >"$broray_tx_tar_list.raw" 2>/dev/null || return 1
    [ -s "$broray_tx_tar_list.raw" ] || return 1
    broray_tx_tar_raw_count="$(wc -l <"$broray_tx_tar_list.raw" | tr -d ' ')"
    broray_tx_tar_root_count="$(awk '$0=="./"{n++} END{print n+0}' "$broray_tx_tar_list.raw")"
    broray_tx_number "$broray_tx_tar_raw_count" && broray_tx_number "$broray_tx_tar_root_count" || return 1
    awk '
      {
        n=$0
        sub(/^\.\//,"",n)
        while (n ~ /\/$/) sub(/\/$/,"",n)
        if (n=="") next
        if (substr(n,1,1)=="/" || n ~ /(^|\/)\.\.($|\/)/ || n ~ /(^|\/)\.($|\/)/ || index(n,"|") || index(n,"\t")) {bad=1; next}
        print n
      }
      END {exit bad ? 1 : 0}
    ' "$broray_tx_tar_list.raw" >"$broray_tx_tar_list.part" || { rm -f "$broray_tx_tar_list.raw" "$broray_tx_tar_list.part"; return 1; }
    rm -f "$broray_tx_tar_list.raw"
    [ -s "$broray_tx_tar_list.part" ] || { rm -f "$broray_tx_tar_list.part"; return 1; }
    broray_tx_tar_count="$(wc -l <"$broray_tx_tar_list.part" | tr -d ' ')"
    LC_ALL=C sort -u "$broray_tx_tar_list.part" >"$broray_tx_tar_list" || return 1
    rm -f "$broray_tx_tar_list.part"
    broray_tx_tar_unique="$(wc -l <"$broray_tx_tar_list" | tr -d ' ')"
    broray_tx_number "$broray_tx_tar_count" && broray_tx_number "$broray_tx_tar_unique" || return 1
    [ "$broray_tx_tar_root_count" -le 1 ] &&
        [ "$broray_tx_tar_raw_count" -eq $((broray_tx_tar_count + broray_tx_tar_root_count)) ] &&
        [ "$broray_tx_tar_count" -eq "$broray_tx_tar_unique" ] &&
        broray_tx_file_cap_kb "$broray_tx_tar_list" "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB"
}

broray_tx_snapshot_live_tar_sha()
{
    broray_tx_stream_out="$1"
    broray_tx_stream_fifo="$BRORAY_TX_WORK/live-source-tar.fifo"
    broray_tx_guard_work "$broray_tx_stream_fifo" || return 1
    rm -f "$broray_tx_stream_fifo" "$broray_tx_stream_out.part" "$broray_tx_stream_out.raw"
    mkfifo "$broray_tx_stream_fifo" || return 1
    sha256sum <"$broray_tx_stream_fifo" >"$broray_tx_stream_out.raw" 2>"$BRORAY_TX_WORK/evidence/live-source-tar-sha.stderr" &
    broray_tx_stream_pid=$!
    set --
    while IFS= read -r broray_tx_stream_rel; do [ -n "$broray_tx_stream_rel" ] && set -- "$@" "$broray_tx_stream_rel"; done <"$BRORAY_TX_WORK/source-scope.list"
    if [ "$#" -gt 0 ]; then
        tar --format=gnu --blocking-factor=1 -cf "$broray_tx_stream_fifo" -C "$BRORAY_TX_FS_ROOT" "$@" \
            -C "$BRORAY_TX_WORK" .broray-recovery 2>"$BRORAY_TX_WORK/evidence/live-source-tar.stderr"
        broray_tx_stream_writer_rc=$?
    else
        broray_tx_stream_writer_rc=1
    fi
    wait "$broray_tx_stream_pid"; broray_tx_stream_reader_rc=$?
    rm -f "$broray_tx_stream_fifo"
    [ "$broray_tx_stream_writer_rc" -eq 0 ] && [ "$broray_tx_stream_reader_rc" -eq 0 ] || { rm -f "$broray_tx_stream_out.raw"; return 1; }
    awk 'NR==1{print $1;exit}' "$broray_tx_stream_out.raw" >"$broray_tx_stream_out.part" || { rm -f "$broray_tx_stream_out.raw" "$broray_tx_stream_out.part"; return 1; }
    broray_tx_stream_sha="$(sed -n '1p' "$broray_tx_stream_out.part")"
    case "$broray_tx_stream_sha" in *[!0-9a-f]*|'') rm -f "$broray_tx_stream_out.raw" "$broray_tx_stream_out.part"; return 1 ;; esac
    [ "${#broray_tx_stream_sha}" -eq 64 ] || { rm -f "$broray_tx_stream_out.raw" "$broray_tx_stream_out.part"; return 1; }
    rm -f "$broray_tx_stream_out.raw"
    mv -f "$broray_tx_stream_out.part" "$broray_tx_stream_out"
}

broray_tx_snapshot_archive_tar_sha()
{
    broray_tx_stream_out="$1"
    broray_tx_stream_fifo="$BRORAY_TX_WORK/archive-tar.fifo"
    broray_tx_guard_work "$broray_tx_stream_fifo" || return 1
    rm -f "$broray_tx_stream_fifo" "$broray_tx_stream_out.part" "$broray_tx_stream_out.raw"
    mkfifo "$broray_tx_stream_fifo" || return 1
    sha256sum <"$broray_tx_stream_fifo" >"$broray_tx_stream_out.raw" 2>"$BRORAY_TX_WORK/evidence/archive-tar-sha.stderr" &
    broray_tx_stream_pid=$!
    gzip -dc "$BRORAY_TX_WORK/backup.tar.gz" >"$broray_tx_stream_fifo" 2>"$BRORAY_TX_WORK/evidence/archive-gzip-stream.stderr"
    broray_tx_stream_writer_rc=$?
    wait "$broray_tx_stream_pid"; broray_tx_stream_reader_rc=$?
    rm -f "$broray_tx_stream_fifo"
    [ "$broray_tx_stream_writer_rc" -eq 0 ] && [ "$broray_tx_stream_reader_rc" -eq 0 ] || { rm -f "$broray_tx_stream_out.raw"; return 1; }
    awk 'NR==1{print $1;exit}' "$broray_tx_stream_out.raw" >"$broray_tx_stream_out.part" || { rm -f "$broray_tx_stream_out.raw" "$broray_tx_stream_out.part"; return 1; }
    broray_tx_stream_sha="$(sed -n '1p' "$broray_tx_stream_out.part")"
    case "$broray_tx_stream_sha" in *[!0-9a-f]*|'') rm -f "$broray_tx_stream_out.raw" "$broray_tx_stream_out.part"; return 1 ;; esac
    [ "${#broray_tx_stream_sha}" -eq 64 ] || { rm -f "$broray_tx_stream_out.raw" "$broray_tx_stream_out.part"; return 1; }
    rm -f "$broray_tx_stream_out.raw"
    mv -f "$broray_tx_stream_out.part" "$broray_tx_stream_out"
}

broray_tx_recovery_capsule_create()
{
    broray_tx_capsule="$BRORAY_TX_WORK/.broray-recovery"
    broray_tx_guard_work "$broray_tx_capsule" || return 1
    rm -rf "$broray_tx_capsule"
    mkdir -p "$broray_tx_capsule" || return 1
    printf 'requirementsContract=%s\nlifecycleContract=%s\ncapabilityContract=%s\nspaceContract=%s\npreviousIpkRequired=false\nhistoricalTransactionStateRequired=false\nstatelessBootstrap=true\nsourceAdmission=%s\n' \
        "$BRORAY_TX_REQUIREMENTS_CONTRACT" "$BRORAY_TX_CONTRACT" \
        keenetic-entware-capabilities/1 broray-space/2 bro-any-structural \
        >"$broray_tx_capsule/contracts.env" || return 1
    for broray_tx_capsule_file in operation.json source.manifest source.allocation.manifest source-scope.list services-before.tsv \
        services-before.identity.tsv services-running-before.list protected-source.manifest protected-source-roots.list; do
        [ -f "$BRORAY_TX_WORK/$broray_tx_capsule_file" ] && [ ! -L "$BRORAY_TX_WORK/$broray_tx_capsule_file" ] || return 1
        cp -p "$BRORAY_TX_WORK/$broray_tx_capsule_file" "$broray_tx_capsule/$broray_tx_capsule_file" || return 1
    done
    [ -f "$BRORAY_TX_WORK/candidate.json" ] && [ ! -L "$BRORAY_TX_WORK/candidate.json" ] || return 1
    [ -f "$BRORAY_TX_WORK/evidence/operation-relation-pre-snapshot.json" ] &&
        [ ! -L "$BRORAY_TX_WORK/evidence/operation-relation-pre-snapshot.json" ] || return 1
    cp -p "$BRORAY_TX_WORK/candidate.json" "$broray_tx_capsule/candidate-target.json" || return 1
    cp -p "$BRORAY_TX_WORK/evidence/operation-relation-pre-snapshot.json" \
        "$broray_tx_capsule/operation-relation-pre-snapshot.json" || return 1
    broray_tx_operation_relation_capsule_verify "$broray_tx_capsule" || return 1
    for broray_tx_capsule_capability in capabilities.json capabilities.tsv; do
        [ -f "$BRORAY_TX_WORK/evidence/$broray_tx_capsule_capability" ] &&
            [ ! -L "$BRORAY_TX_WORK/evidence/$broray_tx_capsule_capability" ] || return 1
        cp -p "$BRORAY_TX_WORK/evidence/$broray_tx_capsule_capability" \
            "$broray_tx_capsule/$broray_tx_capsule_capability" || return 1
    done
    broray_tx_capsule_opt_unit="$(jq -r '.mountGraph.opt.allocationUnitKB // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    broray_tx_number "$broray_tx_capsule_opt_unit" && [ "$broray_tx_capsule_opt_unit" -gt 0 ] || return 1
    broray_tx_capsule_target_objects="$(jq -r '.payloadObjectCount' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_number "$broray_tx_capsule_target_objects" && [ "$broray_tx_capsule_target_objects" -gt 0 ] || return 1
    broray_tx_capsule_opkg_metadata="$(broray_tx_umul "$broray_tx_capsule_target_objects" 512)" || return 1
    broray_tx_capsule_opkg_metadata="$(broray_tx_uadd "$broray_tx_capsule_opkg_metadata" 65536)" || return 1
    broray_tx_capsule_opkg_metadata="$(broray_tx_ceil_div "$broray_tx_capsule_opkg_metadata" 1024)" || return 1
    broray_tx_capsule_status_upper="$(broray_tx_file_dense_upper_kb "$BRORAY_TX_STATUS_FILE" "$broray_tx_capsule_opt_unit")" || return 1
    jq -ncS --arg operationId "$BRORAY_TX_OPERATION_ID" \
        --argjson opkgMetadataUpperKB "$broray_tx_capsule_opkg_metadata" \
        --argjson opkgStatusTransientUpperKB "$broray_tx_capsule_status_upper" '
      {schemaVersion:1,requirementsContract:"1.7.2",
       lifecycleContract:"current-operation-full-tmp-snapshot/1",
       capabilityContract:"keenetic-entware-capabilities/1",spaceContract:"broray-space/2",
       previousIpkRequired:false,historicalTransactionStateRequired:false,statelessBootstrap:true,
       operationId:$operationId,opkgMetadataUpperKB:$opkgMetadataUpperKB,
       opkgStatusTransientUpperKB:$opkgStatusTransientUpperKB,recoveryUseOnly:true}
    ' >"$broray_tx_capsule/recovery-space.json" || return 1
    cp -p "$BRORAY_TX_WORK/snapshot-meta/keenetic-running-before.txt" "$broray_tx_capsule/keenetic-running-before.txt" || return 1
    broray_tx_status_stanza_broray >"$broray_tx_capsule/broray-status.stanza" || return 1
    if [ -e "$BRORAY_TX_STATUS_FILE" ] || [ -L "$BRORAY_TX_STATUS_FILE" ]; then
        [ -f "$BRORAY_TX_STATUS_FILE" ] && [ ! -L "$BRORAY_TX_STATUS_FILE" ] || return 1
        cp -p "$BRORAY_TX_STATUS_FILE" "$broray_tx_capsule/opkg-status.before" || return 1
        printf '%s\n' present >"$broray_tx_capsule/opkg-status.presence" || return 1
    else
        : >"$broray_tx_capsule/opkg-status.before" || return 1
        printf '%s\n' absent >"$broray_tx_capsule/opkg-status.presence" || return 1
    fi
    broray_tx_sha "$broray_tx_capsule/opkg-status.before" \
        >"$broray_tx_capsule/opkg-status.sha256" || return 1
    broray_tx_status_without_broray_to "$BRORAY_TX_WORK/foreign-status.before" || return 1
    cp -p "$BRORAY_TX_WORK/foreign-status.before" "$broray_tx_capsule/foreign-status.before" || return 1
    broray_tx_sha "$BRORAY_TX_WORK/foreign-status.before" >"$broray_tx_capsule/foreign-status.sha256" || return 1
    (
        cd "$broray_tx_capsule" || exit 1
        find -P . -type f ! -name capsule.sha256 -print | LC_ALL=C sort | while IFS= read -r broray_tx_capsule_member; do
            sha256sum "$broray_tx_capsule_member"
        done >capsule.sha256
    ) || return 1
    find -P "$broray_tx_capsule" -printf '%P\n' | sed '/^$/d;s#^#.broray-recovery/#' | sed 's#/$##' | LC_ALL=C sort -u \
        >"$BRORAY_TX_WORK/capsule.members" || return 1
    printf '%s\n' .broray-recovery | { cat; cat "$BRORAY_TX_WORK/capsule.members"; } | LC_ALL=C sort -u \
        >"$BRORAY_TX_WORK/capsule.members.all" || return 1
}

broray_tx_recovery_capsule_extract_verify()
{
    broray_tx_capsule_read="$BRORAY_TX_WORK/recovery-read"
    broray_tx_guard_work "$broray_tx_capsule_read" || return 1
    rm -rf "$broray_tx_capsule_read"
    mkdir -p "$broray_tx_capsule_read" || return 1
    tar -xzf "$BRORAY_TX_WORK/backup.tar.gz" -C "$broray_tx_capsule_read" .broray-recovery \
        2>"$BRORAY_TX_WORK/evidence/recovery-capsule-extract.stderr" || return 1
    broray_tx_recovery_capsule_dir_verify "$broray_tx_capsule_read/.broray-recovery" \
        "$broray_tx_capsule_read" || return 1
    [ -f "$broray_tx_capsule_read/.broray-recovery/contracts.env" ] || return 1
    grep -Fqx "requirementsContract=$BRORAY_TX_REQUIREMENTS_CONTRACT" "$broray_tx_capsule_read/.broray-recovery/contracts.env" || return 1
    grep -Fqx "lifecycleContract=$BRORAY_TX_CONTRACT" "$broray_tx_capsule_read/.broray-recovery/contracts.env" || return 1
    grep -Fqx 'capabilityContract=keenetic-entware-capabilities/1' "$broray_tx_capsule_read/.broray-recovery/contracts.env" || return 1
    grep -Fqx 'spaceContract=broray-space/2' "$broray_tx_capsule_read/.broray-recovery/contracts.env" || return 1
    grep -Fqx 'previousIpkRequired=false' "$broray_tx_capsule_read/.broray-recovery/contracts.env" || return 1
    grep -Fqx 'historicalTransactionStateRequired=false' "$broray_tx_capsule_read/.broray-recovery/contracts.env" || return 1
    grep -Fqx 'statelessBootstrap=true' "$broray_tx_capsule_read/.broray-recovery/contracts.env" || return 1
    grep -Fqx 'sourceAdmission=bro-any-structural' "$broray_tx_capsule_read/.broray-recovery/contracts.env" || return 1
    [ "$(wc -l <"$broray_tx_capsule_read/.broray-recovery/contracts.env" | tr -d ' ')" -eq 8 ] || return 1
    (
        cd "$broray_tx_capsule_read/.broray-recovery" || exit 1
        sha256sum -c capsule.sha256 >/dev/null 2>&1
    ) || return 1
    for broray_tx_capsule_file in operation.json source.manifest source.allocation.manifest source-scope.list services-before.tsv \
        services-before.identity.tsv services-running-before.list protected-source.manifest protected-source-roots.list \
        keenetic-running-before.txt broray-status.stanza candidate-target.json operation-relation-pre-snapshot.json \
        opkg-status.before opkg-status.presence opkg-status.sha256 foreign-status.before foreign-status.sha256 \
        capabilities.json capabilities.tsv recovery-space.json; do
        [ -f "$broray_tx_capsule_read/.broray-recovery/$broray_tx_capsule_file" ] || return 1
    done
    jq -e --arg id "$BRORAY_TX_OPERATION_ID" '
      (type=="object") and (.schemaVersion==1) and
      ((keys|sort)==(["schemaVersion","requirementsContract","lifecycleContract","capabilityContract",
                       "spaceContract","previousIpkRequired","historicalTransactionStateRequired",
                       "statelessBootstrap","operationId","opkgMetadataUpperKB",
                       "opkgStatusTransientUpperKB","recoveryUseOnly"]|sort)) and
      (.requirementsContract=="1.7.2") and
      (.lifecycleContract=="current-operation-full-tmp-snapshot/1") and
      (.capabilityContract=="keenetic-entware-capabilities/1") and
      (.spaceContract=="broray-space/2") and
      (.previousIpkRequired==false) and (.historicalTransactionStateRequired==false) and
      (.statelessBootstrap==true) and (.operationId==$id) and
      ((.opkgMetadataUpperKB|type)=="number") and (.opkgMetadataUpperKB|floor)==.opkgMetadataUpperKB and
      (.opkgMetadataUpperKB>0) and
      ((.opkgStatusTransientUpperKB|type)=="number") and
      (.opkgStatusTransientUpperKB|floor)==.opkgStatusTransientUpperKB and (.opkgStatusTransientUpperKB>=0) and
      (.recoveryUseOnly==true)
    ' "$broray_tx_capsule_read/.broray-recovery/recovery-space.json" >/dev/null 2>&1 || return 1
    jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg mode "$BRORAY_TX_MODE" \
        --arg source "$BRORAY_TX_SOURCE_PACKAGE" --arg sourceApp "$BRORAY_TX_SOURCE_APP" \
        --arg target "$BRORAY_TX_TARGET_PACKAGE" --arg path "$BRORAY_TX_WORK" \
        --arg lifecycle "$BRORAY_TX_CONTRACT" '
      (type=="object") and (.schemaVersion==2) and (.lifecycleContract==$lifecycle) and
      (.operationId==$id) and (.mode==$mode) and (.sourceVersion==$source) and
      (.sourceAppVersion==$sourceApp) and (.targetVersion==$target) and (.transactionPath==$path)
    ' "$broray_tx_capsule_read/.broray-recovery/operation.json" >/dev/null 2>&1 || return 1
    broray_tx_operation_relation_capsule_verify "$broray_tx_capsule_read/.broray-recovery" \
        "$BRORAY_TX_WORK/evidence/operation-relation-recovery.json" || return 1
}

broray_tx_snapshot_create()
{
    broray_tx_persistent_marker_phase_reach pre-snapshot || return 1
    broray_tx_test_pause pre-snapshot || return 1
    broray_tx_event snapshot-create || return 1
    broray_tx_inject snapshot-create || return 1
    broray_tx_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/source-scope.list" \
        "$BRORAY_TX_WORK/source.before-snapshot.manifest" || broray_tx_fail source-manifest-build-failed || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/source.manifest" "$BRORAY_TX_WORK/source.before-snapshot.manifest" ||
        broray_tx_fail source-manifest-drift-before-snapshot || return 1
    broray_tx_allocation_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/source.before-snapshot.manifest" \
        "$BRORAY_TX_WORK/source.before-snapshot.allocation.manifest" || broray_tx_fail source-allocation-recheck-failed || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/source.allocation.manifest" \
        "$BRORAY_TX_WORK/source.before-snapshot.allocation.manifest" ||
        broray_tx_fail source-allocation-drift-before-snapshot || return 1
    rm -f "$BRORAY_TX_WORK/source.before-snapshot.manifest" \
        "$BRORAY_TX_WORK/source.before-snapshot.allocation.manifest"
    broray_tx_recovery_capsule_create || broray_tx_fail recovery-capsule-create-failed || return 1
    set --
    while IFS= read -r broray_tx_snapshot_rel; do [ -n "$broray_tx_snapshot_rel" ] && set -- "$@" "$broray_tx_snapshot_rel"; done <"$BRORAY_TX_WORK/source-scope.list"
    [ "$#" -gt 0 ] || broray_tx_fail source-scope-has-no-objects || return 1
    broray_tx_snapshot_limit_kb="$(cat "$BRORAY_TX_WORK/snapshot-archive-limit.kb" 2>/dev/null)"
    broray_tx_number "$broray_tx_snapshot_limit_kb" && [ "$broray_tx_snapshot_limit_kb" -gt 0 ] || broray_tx_fail invalid-snapshot-archive-limit || return 1
    broray_tx_snapshot_fifo="$BRORAY_TX_WORK/snapshot-compressed.fifo"
    broray_tx_guard_work "$broray_tx_snapshot_fifo" || return 1
    rm -f "$BRORAY_TX_WORK/backup.tar.gz" "$BRORAY_TX_WORK/backup.tar.gz.part" "$broray_tx_snapshot_fifo"
    mkfifo "$broray_tx_snapshot_fifo" || broray_tx_fail snapshot-stream-create-failed || return 1
    # Keep one read descriptor open for the whole stream.  `iflag=fullblock`
    # makes count an exact KiB ceiling even when tar supplies short pipe
    # writes; the one-byte look-ahead proves that the producer did not cross
    # the declared ceiling, and the drain prevents a producer-side SIGPIPE
    # from being mistaken for the only overflow signal.
    (
        exec 8<"$broray_tx_snapshot_fifo" || exit 1
        dd of="$BRORAY_TX_WORK/backup.tar.gz.part" bs=1024 count="$broray_tx_snapshot_limit_kb" iflag=fullblock <&8 \
            2>"$BRORAY_TX_WORK/evidence/snapshot-bounded-write.stderr"
        broray_tx_snapshot_consumer_rc=$?
        broray_tx_snapshot_overflow="$(dd bs=1 count=1 <&8 2>/dev/null | wc -c | tr -d ' ')"
        cat <&8 >/dev/null || exit 1
        exec 8<&-
        printf '%s\n' "$broray_tx_snapshot_overflow" >"$BRORAY_TX_WORK/evidence/snapshot-bounded-write.overflow-bytes" || exit 1
        [ "$broray_tx_snapshot_consumer_rc" -eq 0 ] && [ "$broray_tx_snapshot_overflow" = 0 ]
    ) &
    broray_tx_snapshot_writer_pid=$!
    tar --format=gnu --blocking-factor=1 -czf "$broray_tx_snapshot_fifo" -C "$BRORAY_TX_FS_ROOT" "$@" \
        -C "$BRORAY_TX_WORK" .broray-recovery 2>"$BRORAY_TX_WORK/evidence/snapshot-create.stderr"
    broray_tx_snapshot_tar_rc=$?
    wait "$broray_tx_snapshot_writer_pid"; broray_tx_snapshot_dd_rc=$?
    rm -f "$broray_tx_snapshot_fifo"
    if [ "$broray_tx_snapshot_tar_rc" -ne 0 ] || [ "$broray_tx_snapshot_dd_rc" -ne 0 ] || ! gzip -t "$BRORAY_TX_WORK/backup.tar.gz.part" >/dev/null 2>&1 || ! tar -tzf "$BRORAY_TX_WORK/backup.tar.gz.part" >/dev/null 2>&1; then
        rm -f "$BRORAY_TX_WORK/backup.tar.gz.part"
        broray_tx_fail snapshot-bounded-stream-failed
        return 1
    fi
    mv -f "$BRORAY_TX_WORK/backup.tar.gz.part" "$BRORAY_TX_WORK/backup.tar.gz" || return 1
    [ -f "$BRORAY_TX_WORK/backup.tar.gz" ] && [ ! -L "$BRORAY_TX_WORK/backup.tar.gz" ] && [ -s "$BRORAY_TX_WORK/backup.tar.gz" ] || broray_tx_fail snapshot-archive-empty || return 1
    printf '%s  backup.tar.gz\n' "$(broray_tx_sha "$BRORAY_TX_WORK/backup.tar.gz")" >"$BRORAY_TX_WORK/backup.tar.gz.sha256"
    broray_tx_event snapshot-created || return 1
    broray_tx_status running ''
}

# Verify that one archive member is a canonical direct GNU hardlink to an
# already content-verified f/l member.  The verbose listing is requested for
# exactly one safe member.  Empty stderr is significant: GNU tar reports every
# absolute, dot-segment, or parent-segment hardlink-target normalization there,
# so accepting a normalized display would otherwise hide an unsafe raw target.
broray_tx_snapshot_hardlink_header_verify()
{
    broray_tx_hardlink_path="$1"; broray_tx_hardlink_target="$2"
    broray_tx_hardlink_mode="$3"; broray_tx_hardlink_uid="$4"
    broray_tx_hardlink_gid="$5"; broray_tx_hardlink_kind="$6"
    broray_tx_relative_safe "$broray_tx_hardlink_path" &&
        broray_tx_relative_safe "$broray_tx_hardlink_target" || return 1
    broray_tx_hardlink_verbose="$BRORAY_TX_WORK/archive-hardlink.verbose"
    broray_tx_hardlink_stderr="$BRORAY_TX_WORK/evidence/archive-hardlink-list.stderr"
    : >"$broray_tx_hardlink_stderr" || return 1
    LC_ALL=C tar --numeric-owner --full-time --quoting-style=literal -tvzf \
        "$broray_tx_self_archive" --no-recursion -- "$broray_tx_hardlink_path" \
        >"$broray_tx_hardlink_verbose" 2>"$broray_tx_hardlink_stderr" || return 1
    [ ! -s "$broray_tx_hardlink_stderr" ] &&
        [ "$(wc -l <"$broray_tx_hardlink_verbose" | tr -d ' ')" -eq 1 ] || return 1
    broray_tx_hardlink_header="$(awk 'NR==1 && NF>=3{print $1 "|" $2 "|" $3}' \
        "$broray_tx_hardlink_verbose")" || return 1
    IFS='|' read -r broray_tx_hardlink_symbolic broray_tx_hardlink_owner \
        broray_tx_hardlink_size <<EOF
$broray_tx_hardlink_header
EOF
    broray_tx_hardlink_mode_probe="$BRORAY_TX_WORK/archive-hardlink-mode.probe"
    [ ! -e "$broray_tx_hardlink_mode_probe" ] && [ ! -L "$broray_tx_hardlink_mode_probe" ] || return 1
    : >"$broray_tx_hardlink_mode_probe" || return 1
    chmod "$broray_tx_hardlink_mode" "$broray_tx_hardlink_mode_probe" || return 1
    broray_tx_hardlink_expected_symbolic="$(find -P "$broray_tx_hardlink_mode_probe" \
        -maxdepth 0 -printf '%M' 2>/dev/null)" || return 1
    rm -f "$broray_tx_hardlink_mode_probe" || return 1
    broray_tx_hardlink_expected_symbolic="h${broray_tx_hardlink_expected_symbolic#?}"
    [ "$broray_tx_hardlink_symbolic" = "$broray_tx_hardlink_expected_symbolic" ] &&
        [ "$broray_tx_hardlink_owner" = "$broray_tx_hardlink_uid/$broray_tx_hardlink_gid" ] &&
        [ "$broray_tx_hardlink_size" = 0 ] || return 1
    broray_tx_hardlink_line="$(sed -n '1p' "$broray_tx_hardlink_verbose")"
    case "$broray_tx_hardlink_line" in
        *" link to $broray_tx_hardlink_target") ;;
        *) return 1 ;;
    esac
    printf '%s|%s|%s\n' "$broray_tx_hardlink_kind" "$broray_tx_hardlink_path" \
        "$broray_tx_hardlink_target" >>"$BRORAY_TX_WORK/archive-hardlinks.verified.tsv" || return 1
    return 0
}

# Verify the archived source directly against the authenticated recovery
# capsule without consulting the current live tree.  Regular-file content is
# streamed through GNU tar's to-command interface; only one directory or
# symlink object is materialized at a time for exact metadata inspection.
# This keeps recovery independent from sidecars without creating a second
# uncompressed source copy in /tmp.
broray_tx_snapshot_self_contained_verify()
{
    broray_tx_self_archive="${BRORAY_TX_SNAPSHOT_ARCHIVE:-$BRORAY_TX_WORK/backup.tar.gz}"
    [ -f "$broray_tx_self_archive" ] && [ ! -L "$broray_tx_self_archive" ] || return 1
    broray_tx_self_manifest="$BRORAY_TX_WORK/source.manifest"
    [ -s "$broray_tx_self_manifest" ] && [ ! -L "$broray_tx_self_manifest" ] || return 1
    awk -F '|' '
      function number(v){return v ~ /^(0|[1-9][0-9]*)$/}
      function octal(v){return v ~ /^[0-7]{3,4}$/}
      function sha(v){return v ~ /^[0-9a-f]{64}$/}
      $1=="F" {if(NF!=7 || !number($3) || !sha($4) || !octal($5) || !number($6) || !number($7)) bad=1; next}
      $1=="D" {if(NF!=7 || $3!="-" || $4!="-" || !octal($5) || !number($6) || !number($7)) bad=1; next}
      $1=="L" {if(NF!=8 || !number($3) || !sha($4) || !octal($5) || !number($6) || !number($7) || length($8)!=$3) bad=1; next}
      {bad=1}
      END{exit bad ? 1 : 0}
    ' "$broray_tx_self_manifest" || return 1
    broray_tx_self_lines="$(wc -l <"$broray_tx_self_manifest" | tr -d ' ')"
    broray_tx_self_unique="$(awk -F '|' '{print $2}' "$broray_tx_self_manifest" | LC_ALL=C sort -u | wc -l | tr -d ' ')"
    broray_tx_number "$broray_tx_self_lines" && broray_tx_number "$broray_tx_self_unique" &&
        [ "$broray_tx_self_lines" -eq "$broray_tx_self_unique" ] || return 1
    [ -s "$BRORAY_TX_WORK/source.allocation.manifest" ] && [ ! -L "$BRORAY_TX_WORK/source.allocation.manifest" ] || return 1
    awk -F '|' '
      NR==FNR {type[$2]=$1;mode[$2]=$5;uid[$2]=$6;gid[$2]=$7;target[$2]=(NF==8?$8:"-");seen[$2]=0;next}
      NF!=11 || !($10 in type) || $1!=type[$10] || $7!=mode[$10] || $8!=uid[$10] || $9!=gid[$10] || $11!=target[$10] || seen[$10]++ {bad=1}
      $2 !~ /^(0|[1-9][0-9]*)$/ || $3 !~ /^(0|[1-9][0-9]*)$/ || $4 !~ /^[1-9][0-9]*$/ || $5 !~ /^(0|[1-9][0-9]*)$/ || $6 !~ /^(0|[1-9][0-9]*)$/ {bad=1}
      END {for(p in type) if(seen[p]!=1)bad=1; exit bad ? 1 : 0}
    ' "$broray_tx_self_manifest" "$BRORAY_TX_WORK/source.allocation.manifest" || return 1
    broray_tx_allocation_topology_manifest "$BRORAY_TX_WORK/source.allocation.manifest" \
        "$BRORAY_TX_WORK/source.hardlink-topology.verified" || return 1
    while IFS='|' read -r broray_tx_self_type broray_tx_self_path broray_tx_self_bytes \
        broray_tx_self_sha broray_tx_self_mode broray_tx_self_uid broray_tx_self_gid broray_tx_self_target
    do
        broray_tx_relative_safe "$broray_tx_self_path" || return 1
        case "$broray_tx_self_path" in .broray-recovery|.broray-recovery/*) return 1 ;; esac
        case "$broray_tx_self_type" in
            L)
                [ "$(printf '%s' "$broray_tx_self_target" | sha256sum | awk '{print $1}')" = "$broray_tx_self_sha" ] || return 1
                ;;
            F|D) ;;
            *) return 1 ;;
        esac
    done <"$broray_tx_self_manifest"

    broray_tx_self_script="$BRORAY_TX_WORK/archive-regular-verifier.sh"
    cat >"$broray_tx_self_script" <<'BRORAY_ARCHIVE_REGULAR_VERIFIER'
#!/bin/sh
case "${TAR_FILENAME:-}" in ./*) TAR_FILENAME="${TAR_FILENAME#./}" ;; esac
case "${TAR_FILENAME:-}" in
    .broray-recovery/*) cat >/dev/null; exit 0 ;;
esac
[ "${TAR_FILETYPE:-}" = f ] || exit 31
expected="$(awk -F '|' -v p="$TAR_FILENAME" '$1=="F" && $2==p{print;found++} END{if(found!=1)exit 1}' "$BRORAY_ARCHIVE_EXPECTED")" || exit 32
IFS='|' read -r type path bytes sha mode uid gid <<EOF
$expected
EOF
tar_mode="${TAR_MODE#0}"
[ "$TAR_SIZE" = "$bytes" ] && [ "$tar_mode" = "$mode" ] &&
    [ "$TAR_UID" = "$uid" ] && [ "$TAR_GID" = "$gid" ] || exit 33
actual_sha="$(sha256sum | awk 'NR==1{print $1;exit}')"
[ "$actual_sha" = "$sha" ] || exit 34
printf 'F|%s|%s|%s|%s|%s|%s\n' "$path" "$bytes" "$sha" "$mode" "$uid" "$gid" >>"$BRORAY_ARCHIVE_ACTUAL"
BRORAY_ARCHIVE_REGULAR_VERIFIER
    chmod 700 "$broray_tx_self_script" || return 1
    broray_tx_self_regular_actual="$BRORAY_TX_WORK/archive-regular.actual"
    : >"$broray_tx_self_regular_actual" || return 1
    BRORAY_ARCHIVE_EXPECTED="$broray_tx_self_manifest"
    BRORAY_ARCHIVE_ACTUAL="$broray_tx_self_regular_actual"
    export BRORAY_ARCHIVE_EXPECTED BRORAY_ARCHIVE_ACTUAL
    tar -xzf "$broray_tx_self_archive" --to-command="$BRORAY_TX_ASH $broray_tx_self_script" \
        >"$BRORAY_TX_WORK/evidence/archive-content-verify.stdout" \
        2>"$BRORAY_TX_WORK/evidence/archive-content-verify.stderr" || return 1
    awk -F '|' '$1=="F"' "$broray_tx_self_manifest" | LC_ALL=C sort >"$BRORAY_TX_WORK/archive-regular.expected" || return 1
    LC_ALL=C sort "$broray_tx_self_regular_actual" >"$broray_tx_self_regular_actual.sorted" || return 1
    # --to-command receives data-bearing f records only.  GNU tar represents
    # every later path in an internal regular hardlink group as h with no data,
    # so resolve each missing F row to one already streamed, exact F target.
    # Direct targets only are accepted: chains and cycles have no data-bearing
    # anchor and therefore fail closed.
    awk 'FILENAME==ARGV[1]{expected[$0]=1;next} !($0 in expected){bad=1} END{exit bad?1:0}' \
        "$BRORAY_TX_WORK/archive-regular.expected" "$broray_tx_self_regular_actual.sorted" || return 1
    awk 'FILENAME==ARGV[1]{actual[$0]=1;next} !($0 in actual){print}' \
        "$broray_tx_self_regular_actual.sorted" "$BRORAY_TX_WORK/archive-regular.expected" \
        >"$BRORAY_TX_WORK/archive-regular.missing" || return 1
    : >"$BRORAY_TX_WORK/archive-hardlinks.verified.tsv" || return 1
    while IFS='|' read -r broray_tx_hard_f_type broray_tx_hard_f_path broray_tx_hard_f_bytes \
        broray_tx_hard_f_sha broray_tx_hard_f_mode broray_tx_hard_f_uid broray_tx_hard_f_gid
    do
        [ -n "$broray_tx_hard_f_path" ] || continue
        broray_tx_hard_f_matches=0
        while IFS='|' read -r broray_tx_hard_target_type broray_tx_hard_target_path \
            broray_tx_hard_target_bytes broray_tx_hard_target_sha broray_tx_hard_target_mode \
            broray_tx_hard_target_uid broray_tx_hard_target_gid
        do
            [ "$broray_tx_hard_target_bytes" = "$broray_tx_hard_f_bytes" ] &&
                [ "$broray_tx_hard_target_sha" = "$broray_tx_hard_f_sha" ] &&
                [ "$broray_tx_hard_target_mode" = "$broray_tx_hard_f_mode" ] &&
                [ "$broray_tx_hard_target_uid" = "$broray_tx_hard_f_uid" ] &&
                [ "$broray_tx_hard_target_gid" = "$broray_tx_hard_f_gid" ] || continue
            if broray_tx_snapshot_hardlink_header_verify "$broray_tx_hard_f_path" \
                "$broray_tx_hard_target_path" "$broray_tx_hard_f_mode" \
                "$broray_tx_hard_f_uid" "$broray_tx_hard_f_gid" F; then
                broray_tx_hard_f_matches=$((broray_tx_hard_f_matches + 1))
            fi
        done <"$broray_tx_self_regular_actual.sorted"
        [ "$broray_tx_hard_f_matches" -eq 1 ] || return 1
    done <"$BRORAY_TX_WORK/archive-regular.missing"
    cat "$broray_tx_self_regular_actual.sorted" "$BRORAY_TX_WORK/archive-regular.missing" |
        LC_ALL=C sort >"$BRORAY_TX_WORK/archive-regular.complete" || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/archive-regular.expected" \
        "$BRORAY_TX_WORK/archive-regular.complete" || return 1

    broray_tx_self_meta_root="$BRORAY_TX_WORK/archive-meta-one"
    broray_tx_guard_work "$broray_tx_self_meta_root" || return 1
    : >"$BRORAY_TX_WORK/archive-symlink.direct" || return 1
    : >"$BRORAY_TX_WORK/archive-symlink.hardlink" || return 1
    while IFS='|' read -r broray_tx_self_type broray_tx_self_path broray_tx_self_bytes \
        broray_tx_self_sha broray_tx_self_mode broray_tx_self_uid broray_tx_self_gid broray_tx_self_target
    do
        case "$broray_tx_self_type" in F) continue ;; D|L) ;; *) return 1 ;; esac
        rm -rf "$broray_tx_self_meta_root"
        broray_tx_self_parent="${broray_tx_self_path%/*}"
        [ "$broray_tx_self_parent" != "$broray_tx_self_path" ] || broray_tx_self_parent=''
        mkdir -p "$broray_tx_self_meta_root${broray_tx_self_parent:+/$broray_tx_self_parent}" || return 1
        if ! tar -xzf "$broray_tx_self_archive" -C "$broray_tx_self_meta_root" --no-recursion -- \
            "$broray_tx_self_path" >/dev/null 2>"$BRORAY_TX_WORK/evidence/archive-meta-extract.stderr"; then
            [ "$broray_tx_self_type" = L ] || return 1
            printf '%s|%s|%s|%s|%s|%s|%s|%s\n' "$broray_tx_self_type" "$broray_tx_self_path" \
                "$broray_tx_self_bytes" "$broray_tx_self_sha" "$broray_tx_self_mode" \
                "$broray_tx_self_uid" "$broray_tx_self_gid" "$broray_tx_self_target" \
                >>"$BRORAY_TX_WORK/archive-symlink.hardlink" || return 1
            continue
        fi
        broray_tx_self_object="$broray_tx_self_meta_root/$broray_tx_self_path"
        broray_tx_self_meta="$(find -P "$broray_tx_self_object" -maxdepth 0 -printf '%m|%U|%G' 2>/dev/null)" || return 1
        [ "$broray_tx_self_meta" = "$broray_tx_self_mode|$broray_tx_self_uid|$broray_tx_self_gid" ] || return 1
        case "$broray_tx_self_type" in
            D) [ -d "$broray_tx_self_object" ] && [ ! -L "$broray_tx_self_object" ] || return 1 ;;
            L)
                [ -L "$broray_tx_self_object" ] && [ "$(readlink "$broray_tx_self_object")" = "$broray_tx_self_target" ] || return 1
                printf '%s|%s|%s|%s|%s|%s|%s|%s\n' "$broray_tx_self_type" "$broray_tx_self_path" \
                    "$broray_tx_self_bytes" "$broray_tx_self_sha" "$broray_tx_self_mode" \
                    "$broray_tx_self_uid" "$broray_tx_self_gid" "$broray_tx_self_target" \
                    >>"$BRORAY_TX_WORK/archive-symlink.direct" || return 1
                ;;
        esac
    done <"$broray_tx_self_manifest"
    while IFS='|' read -r broray_tx_hard_l_type broray_tx_hard_l_path broray_tx_hard_l_bytes \
        broray_tx_hard_l_sha broray_tx_hard_l_mode broray_tx_hard_l_uid broray_tx_hard_l_gid broray_tx_hard_l_target
    do
        [ -n "$broray_tx_hard_l_path" ] || continue
        broray_tx_hard_l_matches=0
        while IFS='|' read -r broray_tx_hard_target_type broray_tx_hard_target_path \
            broray_tx_hard_target_bytes broray_tx_hard_target_sha broray_tx_hard_target_mode \
            broray_tx_hard_target_uid broray_tx_hard_target_gid broray_tx_hard_target_link
        do
            [ "$broray_tx_hard_target_bytes" = "$broray_tx_hard_l_bytes" ] &&
                [ "$broray_tx_hard_target_sha" = "$broray_tx_hard_l_sha" ] &&
                [ "$broray_tx_hard_target_mode" = "$broray_tx_hard_l_mode" ] &&
                [ "$broray_tx_hard_target_uid" = "$broray_tx_hard_l_uid" ] &&
                [ "$broray_tx_hard_target_gid" = "$broray_tx_hard_l_gid" ] &&
                [ "$broray_tx_hard_target_link" = "$broray_tx_hard_l_target" ] || continue
            if broray_tx_snapshot_hardlink_header_verify "$broray_tx_hard_l_path" \
                "$broray_tx_hard_target_path" "$broray_tx_hard_l_mode" \
                "$broray_tx_hard_l_uid" "$broray_tx_hard_l_gid" L; then
                broray_tx_hard_l_matches=$((broray_tx_hard_l_matches + 1))
            fi
        done <"$BRORAY_TX_WORK/archive-symlink.direct"
        [ "$broray_tx_hard_l_matches" -eq 1 ] || return 1
    done <"$BRORAY_TX_WORK/archive-symlink.hardlink"
    rm -rf "$broray_tx_self_meta_root"
    rm -f "$broray_tx_self_script" "$BRORAY_TX_WORK/archive-regular.expected" \
        "$broray_tx_self_regular_actual" "$broray_tx_self_regular_actual.sorted" \
        "$BRORAY_TX_WORK/archive-regular.missing" "$BRORAY_TX_WORK/archive-regular.complete" \
        "$BRORAY_TX_WORK/archive-symlink.direct" "$BRORAY_TX_WORK/archive-symlink.hardlink" \
        "$BRORAY_TX_WORK/archive-hardlink.verbose"
    return 0
}

broray_tx_snapshot_verify_core()
{
    broray_tx_snapshot="$BRORAY_TX_WORK/backup.tar.gz"
    [ -f "$broray_tx_snapshot" ] && [ ! -L "$broray_tx_snapshot" ] && [ -s "$broray_tx_snapshot" ] || broray_tx_fail snapshot-not-nonempty-regular || return 1
    broray_tx_snapshot_actual="$(broray_tx_sha "$broray_tx_snapshot")"
    [ "${#broray_tx_snapshot_actual}" -eq 64 ] || broray_tx_fail snapshot-sha-read-failed || return 1
    if [ -f "$BRORAY_TX_WORK/backup.tar.gz.sha256" ] && [ ! -L "$BRORAY_TX_WORK/backup.tar.gz.sha256" ]; then
        broray_tx_snapshot_expected="$(awk 'NR==1{print $1;exit}' "$BRORAY_TX_WORK/backup.tar.gz.sha256")"
        [ "$broray_tx_snapshot_expected" = "$broray_tx_snapshot_actual" ] || broray_tx_fail snapshot-sha-mismatch || return 1
    else
        broray_tx_snapshot_expected="$broray_tx_snapshot_actual"
    fi
    gzip -t "$broray_tx_snapshot" >/dev/null 2>&1 || broray_tx_fail snapshot-gzip-corrupt || return 1
    broray_tx_tar_safe "$broray_tx_snapshot" "$BRORAY_TX_WORK/archive.members" || broray_tx_fail snapshot-tar-unsafe-or-unreadable || return 1
    broray_tx_recovery_capsule_extract_verify || broray_tx_fail snapshot-recovery-capsule-invalid || return 1
    broray_tx_snapshot_recovery_mode=0
    if [ -f "$BRORAY_TX_WORK/mutation.started" ] || [ -f "$BRORAY_TX_WORK/outcome" ]; then
        broray_tx_snapshot_recovery_mode=1
    fi
    if [ "$broray_tx_snapshot_recovery_mode" -eq 1 ] || [ ! -s "$BRORAY_TX_WORK/source.manifest" ]; then
        cp -p "$BRORAY_TX_WORK/recovery-read/.broray-recovery/source.manifest" "$BRORAY_TX_WORK/source.manifest" || return 1
        cp -p "$BRORAY_TX_WORK/recovery-read/.broray-recovery/source.allocation.manifest" \
            "$BRORAY_TX_WORK/source.allocation.manifest" || return 1
        cp -p "$BRORAY_TX_WORK/recovery-read/.broray-recovery/source-scope.list" "$BRORAY_TX_WORK/source-scope.list" || return 1
        cp -p "$BRORAY_TX_WORK/recovery-read/.broray-recovery/services-before.tsv" "$BRORAY_TX_WORK/services-before.tsv" || return 1
        cp -p "$BRORAY_TX_WORK/recovery-read/.broray-recovery/protected-source.manifest" "$BRORAY_TX_WORK/protected-source.manifest" || return 1
        cp -p "$BRORAY_TX_WORK/recovery-read/.broray-recovery/protected-source-roots.list" "$BRORAY_TX_WORK/protected-source-roots.list" || return 1
    fi
    [ -s "$BRORAY_TX_WORK/source.manifest" ] && [ ! -L "$BRORAY_TX_WORK/source.manifest" ] || broray_tx_fail snapshot-source-manifest-missing || return 1
    awk -F '|' '{print $2}' "$BRORAY_TX_WORK/source.manifest" | LC_ALL=C sort -u >"$BRORAY_TX_WORK/source.members" || return 1
    find -P "$BRORAY_TX_WORK/recovery-read/.broray-recovery" -printf '%P\n' | sed '/^$/d;s#^#.broray-recovery/#' | sed 's#/$##' >"$BRORAY_TX_WORK/capsule.members.verify" || return 1
    { cat "$BRORAY_TX_WORK/source.members"; printf '%s\n' .broray-recovery; cat "$BRORAY_TX_WORK/capsule.members.verify"; } | LC_ALL=C sort -u >"$BRORAY_TX_WORK/expected-snapshot.members" || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/expected-snapshot.members" "$BRORAY_TX_WORK/archive.members" || broray_tx_fail snapshot-member-set-does-not-match-source-and-capsule || return 1
    broray_tx_snapshot_archive_tar_sha "$BRORAY_TX_WORK/archive-tar.sha256.current" || broray_tx_fail snapshot-archive-stream-hash-failed || return 1
    if [ "$broray_tx_snapshot_recovery_mode" -eq 1 ]; then
        broray_tx_snapshot_self_contained_verify || broray_tx_fail snapshot-self-contained-content-invalid || return 1
        cp -p "$BRORAY_TX_WORK/source.manifest" "$BRORAY_TX_WORK/archive.manifest" || return 1
        cp -p "$BRORAY_TX_WORK/archive-tar.sha256.current" "$BRORAY_TX_WORK/archive-tar.sha256" || return 1
        printf '%s\n' "$(broray_tx_sha "$BRORAY_TX_WORK/source.manifest")" >"$BRORAY_TX_WORK/source.manifest.sha256"
        printf '%s\n' "$(broray_tx_sha "$BRORAY_TX_WORK/archive.manifest")" >"$BRORAY_TX_WORK/archive.manifest.sha256"
        printf '%s\n' yes >"$BRORAY_TX_WORK/snapshot.binding.verified"
    elif [ ! -f "$BRORAY_TX_WORK/snapshot.binding.verified" ]; then
        broray_tx_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/source-scope.list" "$BRORAY_TX_WORK/source.verify.manifest" || broray_tx_fail source-reverification-manifest-build-failed || return 1
        broray_tx_files_equal "$BRORAY_TX_WORK/source.manifest" "$BRORAY_TX_WORK/source.verify.manifest" || broray_tx_fail source-changed-during-snapshot || return 1
        broray_tx_allocation_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/source.verify.manifest" \
            "$BRORAY_TX_WORK/source.verify.allocation.manifest" || broray_tx_fail source-allocation-reverification-failed || return 1
        broray_tx_files_equal "$BRORAY_TX_WORK/source.allocation.manifest" \
            "$BRORAY_TX_WORK/source.verify.allocation.manifest" || broray_tx_fail source-allocation-changed-during-snapshot || return 1
        broray_tx_snapshot_live_tar_sha "$BRORAY_TX_WORK/live-source-tar.sha256" || broray_tx_fail live-source-stream-hash-failed || return 1
        broray_tx_files_equal "$BRORAY_TX_WORK/live-source-tar.sha256" "$BRORAY_TX_WORK/archive-tar.sha256.current" || broray_tx_fail snapshot-stream-does-not-match-source || return 1
        cp -p "$BRORAY_TX_WORK/source.manifest" "$BRORAY_TX_WORK/archive.manifest" || return 1
        cp -p "$BRORAY_TX_WORK/archive-tar.sha256.current" "$BRORAY_TX_WORK/archive-tar.sha256" || return 1
        printf '%s\n' "$(broray_tx_sha "$BRORAY_TX_WORK/source.manifest")" >"$BRORAY_TX_WORK/source.manifest.sha256"
        printf '%s\n' "$(broray_tx_sha "$BRORAY_TX_WORK/archive.manifest")" >"$BRORAY_TX_WORK/archive.manifest.sha256"
        printf '%s\n' yes >"$BRORAY_TX_WORK/snapshot.binding.verified"
        broray_tx_tmp_measure snapshot-binding-expanded || return 1
    else
        [ "$(broray_tx_sha "$BRORAY_TX_WORK/source.manifest")" = "$(sed -n '1p' "$BRORAY_TX_WORK/source.manifest.sha256" 2>/dev/null)" ] || broray_tx_fail source-manifest-sha-mismatch || return 1
        [ "$(broray_tx_sha "$BRORAY_TX_WORK/archive.manifest")" = "$(sed -n '1p' "$BRORAY_TX_WORK/archive.manifest.sha256" 2>/dev/null)" ] || broray_tx_fail archive-manifest-sha-mismatch || return 1
        broray_tx_files_equal "$BRORAY_TX_WORK/source.manifest" "$BRORAY_TX_WORK/archive.manifest" || broray_tx_fail snapshot-manifest-binding-mismatch || return 1
        broray_tx_files_equal "$BRORAY_TX_WORK/archive-tar.sha256" "$BRORAY_TX_WORK/archive-tar.sha256.current" || broray_tx_fail snapshot-stream-sha-mismatch || return 1
    fi
    rm -f "$BRORAY_TX_WORK/source.verify.manifest" "$BRORAY_TX_WORK/source.verify.allocation.manifest" \
        "$BRORAY_TX_WORK/archive-tar.sha256.current"
    printf '%s\n' "$broray_tx_snapshot_expected" >"$BRORAY_TX_WORK/snapshot.verified"
}

broray_tx_snapshot_verify()
{
    broray_tx_event snapshot-verify || return 1
    broray_tx_inject snapshot-verify || return 1
    broray_tx_snapshot_verify_core || return 1
    broray_tx_space_snapshot_finalize || return 1
    broray_tx_tmp_measure snapshot-verified || return 1
    broray_tx_persistent_marker_phase_reach snapshot-verified || return 1
    broray_tx_event snapshot-verified || return 1
    broray_tx_test_pause snapshot-verified || return 1
    broray_tx_status running ''
}

broray_tx_fetch()
{
    broray_tx_fetch_url="$1"; broray_tx_fetch_out="$2"; broray_tx_fetch_max_bytes="${3:-1048576}"
    broray_tx_number "$broray_tx_fetch_max_bytes" && [ "$broray_tx_fetch_max_bytes" -gt 0 ] || return 1
    case "$broray_tx_fetch_url" in
        https://*)
            broray_tx_fetch_fifo="$broray_tx_fetch_out.fifo"
            broray_tx_guard_work "$broray_tx_fetch_fifo" || return 1
            rm -f "$broray_tx_fetch_fifo" "$broray_tx_fetch_out"
            mkfifo "$broray_tx_fetch_fifo" || return 1
            broray_tx_fetch_blocks=$((broray_tx_fetch_max_bytes / 1024))
            broray_tx_fetch_tail=$((broray_tx_fetch_max_bytes % 1024))
            (
                exec 8<"$broray_tx_fetch_fifo" || exit 1
                {
                    dd bs=1024 count="$broray_tx_fetch_blocks" iflag=fullblock <&8
                    dd bs=1 count="$broray_tx_fetch_tail" iflag=fullblock <&8
                } >"$broray_tx_fetch_out" \
                    2>"$BRORAY_TX_WORK/evidence/download-bounded-write.stderr"
                broray_tx_fetch_consumer_rc=$?
                broray_tx_fetch_overflow="$(dd bs=1 count=1 <&8 2>/dev/null | wc -c | tr -d ' ')"
                cat <&8 >/dev/null || exit 1
                exec 8<&-
                printf '%s\n' "$broray_tx_fetch_overflow" >"$BRORAY_TX_WORK/evidence/download-bounded-write.overflow-bytes" || exit 1
                [ "$broray_tx_fetch_consumer_rc" -eq 0 ] && [ "$broray_tx_fetch_overflow" = 0 ]
            ) &
            broray_tx_fetch_reader=$!
            curl -fL --retry 3 --retry-delay 1 --connect-timeout 15 --max-time 600 \
                -H 'Accept-Encoding: identity' -H 'Cache-Control: no-cache, no-store, max-age=0' -H 'Pragma: no-cache' \
                -o "$broray_tx_fetch_fifo" "$broray_tx_fetch_url"
            broray_tx_fetch_curl_rc=$?
            wait "$broray_tx_fetch_reader"; broray_tx_fetch_dd_rc=$?
            rm -f "$broray_tx_fetch_fifo"
            broray_tx_fetch_actual_bytes="$(wc -c <"$broray_tx_fetch_out" 2>/dev/null | tr -d ' ')"
            broray_tx_number "$broray_tx_fetch_actual_bytes" || { rm -f "$broray_tx_fetch_out"; return 1; }
            printf '%s\n' "$broray_tx_fetch_actual_bytes" >"$BRORAY_TX_WORK/evidence/download-bounded-write.actual-bytes" || return 1
            if [ "$broray_tx_fetch_actual_bytes" -gt "$broray_tx_fetch_max_bytes" ]; then
                rm -f "$broray_tx_fetch_out"
                return 1
            fi
            if [ "$broray_tx_fetch_curl_rc" -ne 0 ] || [ "$broray_tx_fetch_dd_rc" -ne 0 ]; then
                rm -f "$broray_tx_fetch_out"
                return 1
            fi
            ;;
        file://*)
            [ "${BRORAY_TX_ALLOW_FILE_URL:-0}" = 1 ] || return 1
            broray_tx_fetch_source="${broray_tx_fetch_url#file://}"
            [ -f "$broray_tx_fetch_source" ] && [ ! -L "$broray_tx_fetch_source" ] || return 1
            broray_tx_fetch_source_bytes="$(wc -c <"$broray_tx_fetch_source" | tr -d ' ')"
            broray_tx_number "$broray_tx_fetch_source_bytes" && [ "$broray_tx_fetch_source_bytes" -le "$broray_tx_fetch_max_bytes" ] || return 1
            cp -p "$broray_tx_fetch_source" "$broray_tx_fetch_out"
            ;;
        *) return 1 ;;
    esac
}

broray_tx_candidate_json_validate()
{
    broray_tx_candidate_validator_stdout="$BRORAY_TX_WORK/evidence/candidate-metadata-validator.stdout"
    broray_tx_candidate_validator_stderr="$BRORAY_TX_WORK/evidence/candidate-metadata-validator.stderr"
    jq -e --arg release "$BRORAY_TX_TARGET_RELEASE" --arg package "$BRORAY_TX_TARGET_PACKAGE" \
        --arg app "$BRORAY_TX_TARGET_APP" --arg webui "$BRORAY_TX_TARGET_WEBUI" \
        --arg arch "$BRORAY_TX_TARGET_ARCH" --arg filename "$BRORAY_TX_TARGET_FILENAME" \
        --arg baseUrl "$BRORAY_TX_TARGET_PACKAGE_BASE_URL" \
        --arg allowFile "${BRORAY_TX_ALLOW_FILE_URL:-0}" \
        --argjson revision "$BRORAY_TX_TARGET_REVISION" \
        --argjson maxCandidateBytes "$BRORAY_TX_CANDIDATE_FORMAT_MAX_BYTES" '
      def broray_sha256:
        ((type == "string") and (length == 64) and
         all(explode[]; ((. >= 48) and (. <= 57)) or ((. >= 97) and (. <= 102))));
      def broray_uint: ((type=="number") and (floor==.) and (.>=0));
      def broray_protected_defaults:
        ((.protectedDefaultEntries|type)=="array") and (.protectedDefaultEntries|length)>0 and
        all(.protectedDefaultEntries[];
          ((keys|sort)==["allocatedUpperKB","objectCount","path"]) and
          ((.path|type)=="string") and (.path|length)>0 and
          all(.path|explode[];
              (.>=48 and .<=57) or (.>=65 and .<=90) or
              (.>=97 and .<=122) or .==45 or .==46 or .==47 or .==95) and
          (.path|startswith("/")|not) and (.path|contains("..")|not) and
          (.allocatedUpperKB|broray_uint) and (.allocatedUpperKB>0) and
          (.objectCount|broray_uint) and (.objectCount>0)) and
        ([.protectedDefaultEntries[].path] == ([.protectedDefaultEntries[].path]|sort|unique)) and
        (.protectedDefaultAllocatedUpperKB|broray_uint) and
        (.protectedDefaultObjectCount|broray_uint) and
        (([.protectedDefaultEntries[].allocatedUpperKB]|add//0)==.protectedDefaultAllocatedUpperKB) and
        (([.protectedDefaultEntries[].objectCount]|add//0)==.protectedDefaultObjectCount) and
        (.protectedDefaultAllocatedUpperKB<=.targetAllocatedUpperKB) and
        (.protectedDefaultObjectCount<=.payloadObjectCount);
      (type == "object") and
      (((.releaseId | type) == "string") and (.releaseId == $release)) and
      (((.packageVersion | type) == "string") and (.packageVersion == $package)) and
      (((.appVersion | type) == "string") and (.appVersion == $app)) and
      (((.webUIBuild | type) == "string") and (.webUIBuild == $webui)) and
      (((.packageRevision | type) == "number") and ((.packageRevision | floor) == .packageRevision) and (.packageRevision == $revision)) and
      (((.architecture | type) == "string") and (.architecture == $arch)) and
      (((.filename | type) == "string") and (.filename == $filename)) and
      (.sha256 | broray_sha256) and
      (((.sizeBytes | type) == "number") and ((.sizeBytes | floor) == .sizeBytes) and (.sizeBytes > 0) and (.sizeBytes <= $maxCandidateBytes)) and
      (((.actualInstalledBytes | type) == "number") and ((.actualInstalledBytes | floor) == .actualInstalledBytes) and (.actualInstalledBytes > 0)) and
      (((.targetAllocatedUpperKB | type) == "number") and ((.targetAllocatedUpperKB | floor) == .targetAllocatedUpperKB) and (.targetAllocatedUpperKB > 0)) and
      (((.payloadObjectCount | type) == "number") and ((.payloadObjectCount | floor) == .payloadObjectCount) and (.payloadObjectCount > 0)) and
      broray_protected_defaults and
      (((.outerMembersBytes | type) == "number") and ((.outerMembersBytes | floor) == .outerMembersBytes) and (.outerMembersBytes > 0)) and
      (((.outerMemberCount | type) == "number") and (.outerMemberCount == 3)) and
      (((.controlAllocatedUpperKB | type) == "number") and ((.controlAllocatedUpperKB | floor) == .controlAllocatedUpperKB) and (.controlAllocatedUpperKB > 0)) and
      (((.controlObjectCount | type) == "number") and ((.controlObjectCount | floor) == .controlObjectCount) and (.controlObjectCount > 0)) and
      (((.requiredOptKB | type) == "number") and ((.requiredOptKB | floor) == .requiredOptKB) and (.requiredOptKB > 0) and ((.requiredOptKB % 1024) == 0)) and
      (.requirementsContract == "1.7.2") and
      (.lifecycleContract == "current-operation-full-tmp-snapshot/1") and
      (.capabilityContract == "keenetic-entware-capabilities/1") and
      (.spaceContract == "broray-space/2") and
      (.previousIpkRequired == false) and (.historicalTransactionStateRequired == false) and
      (.statelessBootstrap == true) and
      (((.baseUrl | type) == "string") and ((.baseUrl == $baseUrl) or (($allowFile == "1") and (.baseUrl | startswith("file://"))))) and
      (((.distributionRole | type) == "string") and (.distributionRole == "full-candidate"))
    ' "$1" >"$broray_tx_candidate_validator_stdout" 2>"$broray_tx_candidate_validator_stderr"
    broray_tx_candidate_validator_rc=$?
    printf '%s\n' "$broray_tx_candidate_validator_rc" >"$BRORAY_TX_WORK/evidence/candidate-metadata-validator.rc" 2>/dev/null || true
    if [ "$broray_tx_candidate_validator_rc" -ne 0 ]; then
        if [ -f "$1" ] && [ ! -L "$1" ]; then
            cp -p "$1" "$BRORAY_TX_WORK/evidence/candidate-metadata-rejected.json" 2>/dev/null || true
        fi
        jq -c --arg release "$BRORAY_TX_TARGET_RELEASE" --arg package "$BRORAY_TX_TARGET_PACKAGE" \
            --arg app "$BRORAY_TX_TARGET_APP" --arg webui "$BRORAY_TX_TARGET_WEBUI" \
            --arg arch "$BRORAY_TX_TARGET_ARCH" --arg filename "$BRORAY_TX_TARGET_FILENAME" \
            --arg baseUrl "$BRORAY_TX_TARGET_PACKAGE_BASE_URL" --arg allowFile "${BRORAY_TX_ALLOW_FILE_URL:-0}" \
            --argjson revision "$BRORAY_TX_TARGET_REVISION" \
            --argjson maxCandidateBytes "$BRORAY_TX_CANDIDATE_FORMAT_MAX_BYTES" '
          def broray_sha256:
            ((type == "string") and (length == 64) and
             all(explode[]; ((. >= 48) and (. <= 57)) or ((. >= 97) and (. <= 102))));
          def broray_uint: ((type=="number") and (floor==.) and (.>=0));
          def broray_protected_defaults:
            ((.protectedDefaultEntries|type)=="array") and (.protectedDefaultEntries|length)>0 and
            all(.protectedDefaultEntries[];
              ((keys|sort)==["allocatedUpperKB","objectCount","path"]) and
              ((.path|type)=="string") and (.path|length)>0 and
              all(.path|explode[];
                  (.>=48 and .<=57) or (.>=65 and .<=90) or
                  (.>=97 and .<=122) or .==45 or .==46 or .==47 or .==95) and
              (.path|startswith("/")|not) and (.path|contains("..")|not) and
              (.allocatedUpperKB|broray_uint) and (.allocatedUpperKB>0) and
              (.objectCount|broray_uint) and (.objectCount>0)) and
            ([.protectedDefaultEntries[].path] == ([.protectedDefaultEntries[].path]|sort|unique)) and
            (.protectedDefaultAllocatedUpperKB|broray_uint) and
            (.protectedDefaultObjectCount|broray_uint) and
            (([.protectedDefaultEntries[].allocatedUpperKB]|add//0)==.protectedDefaultAllocatedUpperKB) and
            (([.protectedDefaultEntries[].objectCount]|add//0)==.protectedDefaultObjectCount) and
            (.protectedDefaultAllocatedUpperKB<=.targetAllocatedUpperKB) and
            (.protectedDefaultObjectCount<=.payloadObjectCount);
          def check($field; $pass): {field:$field,pass:$pass};
          if type != "object" then [check("candidate.object"; false)] else
          [check("releaseId"; (((.releaseId | type) == "string") and (.releaseId == $release))),
           check("packageVersion"; (((.packageVersion | type) == "string") and (.packageVersion == $package))),
           check("appVersion"; (((.appVersion | type) == "string") and (.appVersion == $app))),
           check("webUIBuild"; (((.webUIBuild | type) == "string") and (.webUIBuild == $webui))),
           check("packageRevision"; (((.packageRevision | type) == "number") and ((.packageRevision | floor) == .packageRevision) and (.packageRevision == $revision))),
           check("architecture"; (((.architecture | type) == "string") and (.architecture == $arch))),
           check("filename"; (((.filename | type) == "string") and (.filename == $filename))),
           check("sha256"; (.sha256 | broray_sha256)),
           check("sizeBytes"; (((.sizeBytes | type) == "number") and ((.sizeBytes | floor) == .sizeBytes) and (.sizeBytes > 0) and (.sizeBytes <= $maxCandidateBytes))),
           check("actualInstalledBytes"; (((.actualInstalledBytes | type) == "number") and ((.actualInstalledBytes | floor) == .actualInstalledBytes) and (.actualInstalledBytes > 0))),
           check("targetAllocatedUpperKB"; (((.targetAllocatedUpperKB | type) == "number") and ((.targetAllocatedUpperKB | floor) == .targetAllocatedUpperKB) and (.targetAllocatedUpperKB > 0))),
           check("payloadObjectCount"; (((.payloadObjectCount | type) == "number") and ((.payloadObjectCount | floor) == .payloadObjectCount) and (.payloadObjectCount > 0))),
           check("protectedDefaultEntries"; broray_protected_defaults),
           check("outerMembersBytes"; (((.outerMembersBytes | type) == "number") and ((.outerMembersBytes | floor) == .outerMembersBytes) and (.outerMembersBytes > 0))),
           check("outerMemberCount"; (((.outerMemberCount | type) == "number") and (.outerMemberCount == 3))),
           check("controlAllocatedUpperKB"; (((.controlAllocatedUpperKB | type) == "number") and ((.controlAllocatedUpperKB | floor) == .controlAllocatedUpperKB) and (.controlAllocatedUpperKB > 0))),
           check("controlObjectCount"; (((.controlObjectCount | type) == "number") and ((.controlObjectCount | floor) == .controlObjectCount) and (.controlObjectCount > 0))),
           check("requiredOptKB"; (((.requiredOptKB | type) == "number") and ((.requiredOptKB | floor) == .requiredOptKB) and (.requiredOptKB > 0) and ((.requiredOptKB % 1024) == 0))),
           check("requirementsContract"; (.requirementsContract == "1.7.2")),
           check("lifecycleContract"; (.lifecycleContract == "current-operation-full-tmp-snapshot/1")),
           check("capabilityContract"; (.capabilityContract == "keenetic-entware-capabilities/1")),
           check("spaceContract"; (.spaceContract == "broray-space/2")),
           check("previousIpkRequired"; (.previousIpkRequired == false)),
           check("historicalTransactionStateRequired"; (.historicalTransactionStateRequired == false)),
           check("statelessBootstrap"; (.statelessBootstrap == true)),
           check("baseUrl"; (((.baseUrl | type) == "string") and ((.baseUrl == $baseUrl) or (($allowFile == "1") and (.baseUrl | startswith("file://")))))),
           check("distributionRole"; (((.distributionRole | type) == "string") and (.distributionRole == "full-candidate")))] end
        ' "$1" >"$BRORAY_TX_WORK/evidence/candidate-metadata-field-results.json" 2>>"$broray_tx_candidate_validator_stderr" || true
        broray_tx_fail candidate-metadata-identity-invalid
        return 1
    fi
    return 0
}

broray_tx_opkg_entry_json_validate()
{
    jq -e --arg release "$BRORAY_TX_TARGET_RELEASE" --arg package "$BRORAY_TX_TARGET_PACKAGE" \
        --arg app "$BRORAY_TX_TARGET_APP" --arg webui "$BRORAY_TX_TARGET_WEBUI" \
        --arg arch "$BRORAY_TX_TARGET_ARCH" --arg filename "$BRORAY_TX_TARGET_FILENAME" \
        --arg baseUrl "$BRORAY_TX_TARGET_PACKAGE_BASE_URL" \
        --arg allowFile "${BRORAY_TX_ALLOW_FILE_URL:-0}" --argjson revision "$BRORAY_TX_TARGET_REVISION" '
      def broray_sha256:
        ((type == "string") and (length == 64) and
         all(explode[]; ((. >= 48) and (. <= 57)) or ((. >= 97) and (. <= 102))));
      (type == "object") and
      .releaseId==$release and .packageVersion==$package and .appVersion==$app and
      .packageRevision==$revision and .architecture==$arch and .webUIBuild==$webui and
      (.filename == $filename) and (.sha256 | broray_sha256) and
      ((.sizeBytes | type) == "number") and (.sizeBytes > 0) and
      ((.baseUrl == $baseUrl) or (($allowFile == "1") and (.baseUrl | startswith("file://")))) and
      .requirementsContract=="1.7.2" and
      .lifecycleContract=="current-operation-full-tmp-snapshot/1" and
      .capabilityContract=="keenetic-entware-capabilities/1" and .spaceContract=="broray-space/2" and
      .previousIpkRequired==false and .historicalTransactionStateRequired==false and .statelessBootstrap==true and
      (.distributionRole == "canonical-full-candidate") and
      (.metadataOnlyRegistration == true) and (.directOpkgMutation == "fail-closed")
    ' "$1" >/dev/null 2>"$BRORAY_TX_WORK/evidence/opkg-entry-validator.stderr"
}

broray_tx_release_metadata()
{
    broray_tx_metadata_input="${1:-}"
    if [ -n "$broray_tx_metadata_input" ]; then
        [ -f "$broray_tx_metadata_input" ] && [ ! -L "$broray_tx_metadata_input" ] || broray_tx_fail provided-metadata-not-regular || return 1
        jq -ce 'if (type == "object") and has("candidate") then .candidate else . end' "$broray_tx_metadata_input" \
            >"$BRORAY_TX_WORK/candidate.json.part" 2>"$BRORAY_TX_WORK/evidence/provided-metadata-parser.stderr" || broray_tx_fail provided-metadata-malformed || return 1
        jq -ce 'if (type == "object") and has("opkgEntry") then .opkgEntry else empty end' "$broray_tx_metadata_input" \
            >"$BRORAY_TX_WORK/opkg-entry.json.part" 2>"$BRORAY_TX_WORK/evidence/provided-opkg-entry-parser.stderr" || true
    else
        broray_tx_fetch "$BRORAY_TX_RELEASE_URL" "$BRORAY_TX_WORK/release.json.part" 2>"$BRORAY_TX_WORK/evidence/release-download.stderr" || broray_tx_fail release-json-download-failed || return 1
        mv -f "$BRORAY_TX_WORK/release.json.part" "$BRORAY_TX_WORK/release.json" || return 1
        jq -e --arg release "$BRORAY_TX_TARGET_RELEASE" --arg package "$BRORAY_TX_TARGET_PACKAGE" --arg app "$BRORAY_TX_TARGET_APP" \
          --arg webui "$BRORAY_TX_TARGET_WEBUI" --arg architecture "$BRORAY_TX_TARGET_ARCH" \
          --argjson revision "$BRORAY_TX_TARGET_REVISION" '
          (type == "object") and (.schemaVersion == 3) and
          (.lifecycleContract == "current-operation-full-tmp-snapshot/1") and
          (.releaseId == $release) and (.packageVersion == $package) and (.version == $app) and
          (.packageRevision == $revision) and (.architecture == $architecture) and (.webUIBuild == $webui) and
          (.requirementsContract == "1.7.2") and (.capabilityContract == "keenetic-entware-capabilities/1") and
          (.spaceContract == "broray-space/2") and (.previousIpkRequired == false) and
          (.historicalTransactionStateRequired == false) and (.statelessBootstrap == true) and
          (.snapshot.location == "/tmp/broray-update-<operation-id>/backup.tar.gz") and
          ((.candidate | type) == "object") and ((.opkgEntry | type) == "object")
        ' "$BRORAY_TX_WORK/release.json" >"$BRORAY_TX_WORK/evidence/release-validator.stdout" \
            2>"$BRORAY_TX_WORK/evidence/release-validator.stderr" || broray_tx_fail release-json-contract-invalid || return 1
        jq -ce '.candidate' "$BRORAY_TX_WORK/release.json" >"$BRORAY_TX_WORK/candidate.json.part" \
            2>"$BRORAY_TX_WORK/evidence/candidate-extract.stderr" || return 1
        jq -ce '.opkgEntry' "$BRORAY_TX_WORK/release.json" >"$BRORAY_TX_WORK/opkg-entry.json.part" \
            2>"$BRORAY_TX_WORK/evidence/opkg-entry-extract.stderr" || return 1
    fi
    mv -f "$BRORAY_TX_WORK/candidate.json.part" "$BRORAY_TX_WORK/candidate.json" || return 1
    broray_tx_candidate_json_validate "$BRORAY_TX_WORK/candidate.json" || return 1
    if [ -s "$BRORAY_TX_WORK/opkg-entry.json.part" ]; then
        broray_tx_opkg_entry_json_validate "$BRORAY_TX_WORK/opkg-entry.json.part" || broray_tx_fail opkg-entry-metadata-invalid || return 1
        jq -e --slurpfile candidate "$BRORAY_TX_WORK/candidate.json" '
          (del(.distributionRole,.metadataOnlyRegistration,.directOpkgMutation) ==
           ($candidate[0] | del(.distributionRole)))
        ' "$BRORAY_TX_WORK/opkg-entry.json.part" >/dev/null 2>&1 || broray_tx_fail opkg-entry-candidate-binding-invalid || return 1
        mv -f "$BRORAY_TX_WORK/opkg-entry.json.part" "$BRORAY_TX_WORK/opkg-entry.json"
    else
        rm -f "$BRORAY_TX_WORK/opkg-entry.json.part"
    fi
}

broray_tx_control_value()
{
    awk -F ': *' -v key="$2" '$1==key{sub(/^[^:]*:[[:space:]]*/,"");print;exit}' "$1"
}

# Bind caller mode to the validated metadata SHA before snapshot creation and
# repeat the same relation immediately before mutation.  Update accepts every
# structurally intact source except the exact target bytes; reinstall accepts
# only the exact currently registered candidate identity.
broray_tx_operation_relation_verify()
{
    broray_tx_relation_stage="$1"
    case "$broray_tx_relation_stage" in pre-snapshot|pre-mutation) ;; *) return 1 ;; esac
    [ -f "$BRORAY_TX_WORK/candidate.json" ] && [ ! -L "$BRORAY_TX_WORK/candidate.json" ] || return 1
    broray_tx_relation_target_sha="$(jq -r '.sha256 // empty' "$BRORAY_TX_WORK/candidate.json")" || return 1
    case "$broray_tx_relation_target_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#broray_tx_relation_target_sha}" -eq 64 ] || return 1
    broray_tx_source_admit || { broray_tx_fail relation-source-reread-failed; return 1; }
    broray_tx_relation_saved_sha_path="$BRORAY_TX_INFO_ROOT/broray.candidate-sha256"
    broray_tx_relation_saved_sha=absent
    if [ -e "$broray_tx_relation_saved_sha_path" ] || [ -L "$broray_tx_relation_saved_sha_path" ]; then
        [ -f "$broray_tx_relation_saved_sha_path" ] && [ ! -L "$broray_tx_relation_saved_sha_path" ] &&
            [ "$(wc -l <"$broray_tx_relation_saved_sha_path" | tr -d ' ')" -eq 1 ] || {
                broray_tx_fail source-candidate-sha-ambiguous
                return 1
            }
        broray_tx_relation_saved_sha="$(sed -n '1p' "$broray_tx_relation_saved_sha_path")"
        case "$broray_tx_relation_saved_sha" in *[!0-9a-f]*|'') broray_tx_fail source-candidate-sha-invalid; return 1 ;; esac
        [ "${#broray_tx_relation_saved_sha}" -eq 64 ] || { broray_tx_fail source-candidate-sha-invalid; return 1; }
    fi
    case "$BRORAY_TX_MODE:$BRORAY_TX_SOURCE_CLASS" in
        install:absent)
            broray_tx_relation_basis=source-absence
            broray_tx_relation_result=install-to-absence
            ;;
        update:bro-any-structural|opkg-upgrade:bro-any-structural)
            if [ "$broray_tx_relation_saved_sha" != absent ]; then
                [ "$broray_tx_relation_saved_sha" != "$broray_tx_relation_target_sha" ] || {
                    broray_tx_fail update-target-already-installed
                    return 1
                }
                broray_tx_relation_basis=saved-candidate-sha-different
                broray_tx_relation_result=exact-candidate-sha-different-target
            else
                broray_tx_relation_known_difference=0
                case "$BRORAY_TX_SOURCE_PACKAGE" in
                    ''|unknown|unregistered|"$BRORAY_TX_TARGET_PACKAGE") ;;
                    *) broray_tx_relation_known_difference=1 ;;
                esac
                case "$BRORAY_TX_SOURCE_APP" in
                    ''|unknown|unregistered|"$BRORAY_TX_TARGET_APP") ;;
                    *) broray_tx_relation_known_difference=1 ;;
                esac
                [ "$broray_tx_relation_known_difference" -eq 1 ] || {
                    broray_tx_fail update-target-identity-ambiguous
                    return 1
                }
                broray_tx_relation_basis=factual-known-identity-difference
                broray_tx_relation_result=factual-source-identity-different-target
            fi
            ;;
        reinstall:bro-any-structural|restore:bro-any-structural)
            broray_tx_relation_control="$BRORAY_TX_INFO_ROOT/broray.control"
            [ -f "$broray_tx_relation_control" ] && [ ! -L "$broray_tx_relation_control" ] || {
                broray_tx_fail "$BRORAY_TX_MODE-source-control-missing"
                return 1
            }
            [ "$broray_tx_relation_saved_sha" = "$broray_tx_relation_target_sha" ] &&
                [ "$BRORAY_TX_SOURCE_PACKAGE" = "$BRORAY_TX_TARGET_PACKAGE" ] &&
                [ "$BRORAY_TX_SOURCE_APP" = "$BRORAY_TX_TARGET_APP" ] &&
                [ "$(broray_tx_control_value "$broray_tx_relation_control" Version)" = "$BRORAY_TX_TARGET_PACKAGE" ] &&
                [ "$(broray_tx_control_value "$broray_tx_relation_control" X-BROray-Release-ID)" = "$BRORAY_TX_TARGET_RELEASE" ] &&
                [ "$(broray_tx_control_value "$broray_tx_relation_control" X-BROray-Package-Revision)" = "$BRORAY_TX_TARGET_REVISION" ] || {
                    broray_tx_fail "$BRORAY_TX_MODE-source-identity-not-exact"
                    return 1
                }
            broray_tx_relation_basis=saved-candidate-sha-and-control-exact
            if [ "$BRORAY_TX_MODE" = restore ]; then
                broray_tx_relation_result=exact-candidate-restore
            else
                broray_tx_relation_result=exact-candidate-reinstall
            fi
            ;;
        *) broray_tx_fail invalid-mode-source-relation; return 1 ;;
    esac
    jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" --arg stage "$broray_tx_relation_stage" \
        --arg mode "$BRORAY_TX_MODE" --arg sourceClass "$BRORAY_TX_SOURCE_CLASS" \
        --arg sourcePackage "$BRORAY_TX_SOURCE_PACKAGE" --arg sourceApp "$BRORAY_TX_SOURCE_APP" \
        --arg targetPackage "$BRORAY_TX_TARGET_PACKAGE" --arg targetApp "$BRORAY_TX_TARGET_APP" \
        --arg targetSha256 "$broray_tx_relation_target_sha" --arg savedSha256 "$broray_tx_relation_saved_sha" \
        --arg basis "$broray_tx_relation_basis" --arg result "$broray_tx_relation_result" \
        '{schemaVersion:1,status:"PASS",operationId:$operationId,stage:$stage,mode:$mode,
          sourceClass:$sourceClass,sourcePackage:$sourcePackage,sourceApp:$sourceApp,
          targetPackage:$targetPackage,targetApp:$targetApp,
          targetSha256:$targetSha256,savedSha256:$savedSha256,
          relationBasis:$basis,result:$result,versionMatrixUsed:false,mutationStarted:false}' \
        >"$BRORAY_TX_WORK/evidence/operation-relation-$broray_tx_relation_stage.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/evidence/operation-relation-$broray_tx_relation_stage.json.part" \
        "$BRORAY_TX_WORK/evidence/operation-relation-$broray_tx_relation_stage.json"
}

# Recovery never guesses a relation from a partially replaced live tree.  It
# revalidates the candidate target and the pre-snapshot relation retained in
# the authenticated, current-operation capsule, then emits a recovery-stage
# derivation bound to those exact bytes.
broray_tx_operation_relation_capsule_verify()
{
    broray_tx_relation_capsule="$1"
    broray_tx_relation_recovery_output="${2:-}"
    [ -d "$broray_tx_relation_capsule" ] && [ ! -L "$broray_tx_relation_capsule" ] || return 1
    for broray_tx_relation_capsule_file in \
        operation.json candidate-target.json operation-relation-pre-snapshot.json
    do
        [ -f "$broray_tx_relation_capsule/$broray_tx_relation_capsule_file" ] &&
            [ ! -L "$broray_tx_relation_capsule/$broray_tx_relation_capsule_file" ] || return 1
    done
    jq -e --arg release "$BRORAY_TX_TARGET_RELEASE" \
      --arg package "$BRORAY_TX_TARGET_PACKAGE" --arg app "$BRORAY_TX_TARGET_APP" \
      --argjson revision "$BRORAY_TX_TARGET_REVISION" \
      --slurpfile operation "$broray_tx_relation_capsule/operation.json" \
      --slurpfile candidate "$broray_tx_relation_capsule/candidate-target.json" '
      def sha256:
        (type=="string") and (length==64) and
        all(explode[]; ((.>=48) and (.<=57)) or ((.>=97) and (.<=102)));
      def known_difference($value;$target):
        ($value|type)=="string" and $value!="" and $value!="unknown" and
        $value!="unregistered" and $value!=$target;
      ($operation|length)==1 and ($candidate|length)==1 and
      ($operation[0]|type)=="object" and ($candidate[0]|type)=="object" and
      $candidate[0].releaseId==$release and $candidate[0].packageVersion==$package and
      $candidate[0].appVersion==$app and $candidate[0].packageRevision==$revision and
      ($candidate[0].sha256|sha256) and
      $candidate[0].requirementsContract=="1.7.2" and
      $candidate[0].lifecycleContract=="current-operation-full-tmp-snapshot/1" and
      $candidate[0].capabilityContract=="keenetic-entware-capabilities/1" and
      $candidate[0].spaceContract=="broray-space/2" and
      type=="object" and .schemaVersion==1 and .status=="PASS" and
      .stage=="pre-snapshot" and .operationId==$operation[0].operationId and
      .mode==$operation[0].mode and .sourceClass==$operation[0].sourceClass and
      .sourcePackage==$operation[0].sourceVersion and
      .sourceApp==$operation[0].sourceAppVersion and
      .targetPackage==$package and .targetApp==$app and
      .targetSha256==$candidate[0].sha256 and .versionMatrixUsed==false and
      .mutationStarted==false and
      (if .mode=="install" then
         .sourceClass=="absent" and .savedSha256=="absent" and
         .relationBasis=="source-absence" and .result=="install-to-absence"
       elif (.mode=="update" or .mode=="opkg-upgrade") then
         .sourceClass=="bro-any-structural" and
         (if .savedSha256=="absent" then
            .relationBasis=="factual-known-identity-difference" and
            .result=="factual-source-identity-different-target" and
            (known_difference(.sourcePackage;$package) or known_difference(.sourceApp;$app))
          else
            (.savedSha256|sha256) and .savedSha256!=.targetSha256 and
            .relationBasis=="saved-candidate-sha-different" and
            .result=="exact-candidate-sha-different-target"
          end)
       elif (.mode=="reinstall" or .mode=="restore") then
         .sourceClass=="bro-any-structural" and .savedSha256==.targetSha256 and
         .sourcePackage==$package and .sourceApp==$app and
         .relationBasis=="saved-candidate-sha-and-control-exact" and
         .result==(if .mode=="restore" then "exact-candidate-restore" else "exact-candidate-reinstall" end)
       else false end)
    ' "$broray_tx_relation_capsule/operation-relation-pre-snapshot.json" >/dev/null 2>&1 || return 1
    if [ -n "$broray_tx_relation_recovery_output" ]; then
        case "$broray_tx_relation_recovery_output" in /*) ;; *) return 1 ;; esac
        jq '.stage="recovery" |
            .relationSource="authenticated-current-operation-capsule-pre-snapshot" |
            .recoveryRelationRevalidated=true' \
            "$broray_tx_relation_capsule/operation-relation-pre-snapshot.json" \
            >"$broray_tx_relation_recovery_output.part" || return 1
        mv -f "$broray_tx_relation_recovery_output.part" "$broray_tx_relation_recovery_output" || return 1
    fi
}

broray_tx_candidate_structure_verify()
{
    broray_tx_candidate="$1"; broray_tx_check="$BRORAY_TX_WORK/candidate-check"
    broray_tx_guard_work "$broray_tx_check" || return 1
    rm -rf "$broray_tx_check"; mkdir -p "$broray_tx_check/outer" "$broray_tx_check/control" "$broray_tx_check/data" || return 1
    broray_tx_tar_safe "$broray_tx_candidate" "$broray_tx_check/outer.list" || broray_tx_fail candidate-outer-unsafe || return 1
    sed 's#^\./##;s#/$##' "$broray_tx_check/outer.list" | sed '/^$/d' | LC_ALL=C sort -u >"$broray_tx_check/outer.actual"
    printf '%s\n' control.tar.gz data.tar.gz debian-binary | LC_ALL=C sort >"$broray_tx_check/outer.expected"
    broray_tx_files_equal "$broray_tx_check/outer.actual" "$broray_tx_check/outer.expected" || broray_tx_fail candidate-outer-members-invalid || return 1
    tar -xzf "$broray_tx_candidate" -C "$broray_tx_check/outer" || return 1
    [ "$(cat "$broray_tx_check/outer/debian-binary")" = 2.0 ] || broray_tx_fail candidate-debian-binary-invalid || return 1
    broray_tx_tar_safe "$broray_tx_check/outer/control.tar.gz" "$broray_tx_check/control.list" || broray_tx_fail candidate-control-unsafe || return 1
    broray_tx_tar_safe "$broray_tx_check/outer/data.tar.gz" "$broray_tx_check/data.list" || broray_tx_fail candidate-data-unsafe || return 1
    tar -xzf "$broray_tx_check/outer/control.tar.gz" -C "$broray_tx_check/control" || return 1
    tar -xzf "$broray_tx_check/outer/data.tar.gz" -C "$broray_tx_check/data" || return 1
    broray_tx_tree_manifest "$broray_tx_check/control" "$broray_tx_check/control.actual" || broray_tx_fail candidate-control-manifest-build-failed || return 1
    broray_tx_control_objects="$(wc -l <"$broray_tx_check/control.actual" | tr -d ' ')"
    broray_tx_control_allocated_upper_kb="$(awk -F '|' '
      $1=="F"{sum+=int(($3+4095)/4096)*4}
      $1=="D"||$1=="L"{sum+=4}
      END{printf "%.0f",sum+0}' "$broray_tx_check/control.actual")"
    broray_tx_number "$broray_tx_control_objects" && broray_tx_number "$broray_tx_control_allocated_upper_kb" || return 1
    [ "$broray_tx_control_objects" = "$(jq -r '.controlObjectCount' "$BRORAY_TX_WORK/candidate.json")" ] ||
        broray_tx_fail candidate-control-object-count-metadata-mismatch || return 1
    [ "$broray_tx_control_allocated_upper_kb" = "$(jq -r '.controlAllocatedUpperKB' "$BRORAY_TX_WORK/candidate.json")" ] ||
        broray_tx_fail candidate-control-allocation-metadata-mismatch || return 1
    broray_tx_control="$broray_tx_check/control/control"
    [ "$(broray_tx_control_value "$broray_tx_control" Package)" = broray ] || broray_tx_fail candidate-control-package-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" Version)" = "$BRORAY_TX_TARGET_PACKAGE" ] || broray_tx_fail candidate-control-version-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" Architecture)" = "$BRORAY_TX_TARGET_ARCH" ] || broray_tx_fail candidate-control-architecture-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Version)" = "$BRORAY_TX_TARGET_APP" ] || broray_tx_fail candidate-control-app-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-WebUI-Version)" = "$BRORAY_TX_TARGET_WEBUI" ] || broray_tx_fail candidate-control-webui-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Release-ID)" = "$BRORAY_TX_TARGET_RELEASE" ] || broray_tx_fail candidate-control-release-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Distribution-Role)" = full-candidate ] || broray_tx_fail candidate-control-role-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Canonical-Lifecycle)" = "$BRORAY_TX_CONTRACT" ] || broray_tx_fail candidate-control-lifecycle-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Requirements-Contract)" = "$BRORAY_TX_REQUIREMENTS_CONTRACT" ] || broray_tx_fail candidate-requirements-contract-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Capability-Contract)" = keenetic-entware-capabilities/1 ] || broray_tx_fail candidate-capability-contract-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Space-Contract)" = broray-space/2 ] || broray_tx_fail candidate-space-contract-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Previous-IPK-Required)" = 0 ] || broray_tx_fail candidate-previous-ipk-contract-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Historical-State-Required)" = 0 ] || broray_tx_fail candidate-historical-state-contract-invalid || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Stateless-Bootstrap)" = 1 ] || broray_tx_fail candidate-stateless-bootstrap-contract-invalid || return 1
    if printf '%s\n' "$(broray_tx_control_value "$broray_tx_control" Depends)" | grep -Fq broray-snapshot-bootstrap; then
        broray_tx_fail forbidden-bootstrap-dependency
        return 1
    fi
    for broray_tx_hook in preinst postinst prerm postrm; do
        [ -f "$broray_tx_check/control/$broray_tx_hook" ] && [ ! -L "$broray_tx_check/control/$broray_tx_hook" ] || broray_tx_fail "candidate-hook-$broray_tx_hook-missing" || return 1
        broray_tx_shell_syntax "$broray_tx_check/control/$broray_tx_hook" || broray_tx_fail "candidate-hook-$broray_tx_hook-syntax" || return 1
    done
    grep -Fq 'X-BROray-Canonical-Safe-Prerm: 1' "$broray_tx_check/control/prerm" || broray_tx_fail candidate-safe-prerm-marker-missing || return 1
    [ -s "$broray_tx_check/control/payload-manifest.tsv" ] && [ ! -L "$broray_tx_check/control/payload-manifest.tsv" ] || broray_tx_fail candidate-payload-manifest-missing || return 1
    broray_tx_tree_manifest "$broray_tx_check/data" "$broray_tx_check/payload.actual" || broray_tx_fail candidate-payload-manifest-build-failed || return 1
    broray_tx_files_equal "$broray_tx_check/control/payload-manifest.tsv" "$broray_tx_check/payload.actual" || broray_tx_fail candidate-payload-manifest-mismatch || return 1
    # The authenticated recovery archive is created before the candidate is
    # downloaded.  Therefore an archive-only restart cannot depend on the
    # volatile candidate-check directory to learn what a partial extraction
    # may have written.  Constrain this release's payload to an immutable,
    # engine-known rollback scope: the exclusive application root and five
    # exclusive service files, plus their shared container directories.
    # This makes the archive-only delete set complete without treating a
    # live tree or an unauthenticated sidecar as ownership evidence.
    broray_tx_candidate_app_rel="$(broray_tx_to_relative "$BRORAY_TX_APP_ROOT")" || return 1
    while IFS='|' read -r broray_tx_candidate_scope_type broray_tx_candidate_scope_path \
        broray_tx_candidate_scope_rest
    do
        broray_tx_relative_safe "$broray_tx_candidate_scope_path" || return 1
        case "$broray_tx_candidate_scope_path:$broray_tx_candidate_scope_type" in
            opt:D|opt/etc:D|opt/etc/init.d:D) ;;
            "$broray_tx_candidate_app_rel":D|"$broray_tx_candidate_app_rel"/*:F|\
            "$broray_tx_candidate_app_rel"/*:D|"$broray_tx_candidate_app_rel"/*:L) ;;
            opt/etc/init.d/S23broray-monitor:F|opt/etc/init.d/S24broray:F|\
            opt/etc/init.d/S25broray-web:F|opt/etc/init.d/S27broray-auto-switch:F|\
            opt/etc/init.d/S28broray-subscriptions:F) ;;
            *) broray_tx_fail candidate-target-rollback-scope-invalid; return 1 ;;
        esac
    done <"$broray_tx_check/control/payload-manifest.tsv"
    broray_tx_payload_objects="$(wc -l <"$broray_tx_check/control/payload-manifest.tsv" | tr -d ' ')"
    broray_tx_payload_regular_bytes="$(awk -F '|' '$1=="F"{sum+=$3} END{printf "%.0f",sum+0}' "$broray_tx_check/control/payload-manifest.tsv")"
    broray_tx_payload_allocated_upper_kb="$(awk -F '|' '
      $1=="F"{sum+=int(($3+4095)/4096)*4}
      $1=="D"||$1=="L"{sum+=4}
      END{printf "%.0f",sum+0}' "$broray_tx_check/control/payload-manifest.tsv")"
    broray_tx_number "$broray_tx_payload_objects" && broray_tx_number "$broray_tx_payload_regular_bytes" &&
        broray_tx_number "$broray_tx_payload_allocated_upper_kb" || return 1
    [ "$broray_tx_payload_objects" = "$(jq -r '.payloadObjectCount' "$BRORAY_TX_WORK/candidate.json")" ] || broray_tx_fail candidate-object-count-metadata-mismatch || return 1
    [ "$broray_tx_payload_regular_bytes" = "$(jq -r '.actualInstalledBytes' "$BRORAY_TX_WORK/candidate.json")" ] || broray_tx_fail candidate-installed-bytes-metadata-mismatch || return 1
    [ "$broray_tx_payload_allocated_upper_kb" = "$(jq -r '.targetAllocatedUpperKB' "$BRORAY_TX_WORK/candidate.json")" ] || broray_tx_fail candidate-allocation-metadata-mismatch || return 1
    broray_tx_candidate_protected_default_metrics "$broray_tx_check/control/payload-manifest.tsv" \
        "$broray_tx_check/protected-defaults.actual.tsv" || broray_tx_fail candidate-protected-default-metadata-build-failed || return 1
    jq -Rn '[inputs | split("|") |
      {path:.[0],allocatedUpperKB:(.[1]|tonumber),objectCount:(.[2]|tonumber)}]' \
      <"$broray_tx_check/protected-defaults.actual.tsv" >"$broray_tx_check/protected-defaults.actual.json" || return 1
    jq -e --slurpfile actual "$broray_tx_check/protected-defaults.actual.json" '
      .protectedDefaultEntries==$actual[0] and
      .protectedDefaultAllocatedUpperKB==([$actual[0][].allocatedUpperKB]|add//0) and
      .protectedDefaultObjectCount==([$actual[0][].objectCount]|add//0)
    ' "$BRORAY_TX_WORK/candidate.json" >/dev/null 2>&1 ||
        broray_tx_fail candidate-protected-default-metadata-mismatch || return 1
    [ "$(broray_tx_control_value "$broray_tx_control" X-BROray-Required-Opt-KB)" = "$(jq -r '.requiredOptKB' "$BRORAY_TX_WORK/candidate.json")" ] || broray_tx_fail candidate-required-opt-metadata-mismatch || return 1
    printf '%s\n' /opt/broray/config/system/server-auto-switch.json /opt/broray/config/system/settings.json /opt/broray/routes/config.json | LC_ALL=C sort >"$broray_tx_check/conffiles.expected"
    LC_ALL=C sort "$broray_tx_check/control/conffiles" >"$broray_tx_check/conffiles.actual"
    broray_tx_files_equal "$broray_tx_check/conffiles.expected" "$broray_tx_check/conffiles.actual" || broray_tx_fail candidate-conffiles-contract-invalid || return 1
    jq -e --arg release "$BRORAY_TX_TARGET_RELEASE" --arg package "$BRORAY_TX_TARGET_PACKAGE" --arg app "$BRORAY_TX_TARGET_APP" --arg webui "$BRORAY_TX_TARGET_WEBUI" '
      .schemaVersion==3 and .releaseId==$release and .packageVersion==$package and .version==$app and .webUIBuild==$webui and
      .previousIpkRequired==false and .historicalTransactionStateRequired==false and .statelessBootstrap==true and
      .rollbackStorage=="/tmp/broray-update-<operation-id>/backup.tar.gz" and
      .requirementsContract=="1.7.2" and .lifecycleContract=="current-operation-full-tmp-snapshot/1" and
      .capabilityContract=="keenetic-entware-capabilities/1" and
      .spaceContract=="broray-space/2" and .cleanReplacement==true and
      .sourceAdmission=="bro-any-structural" and .migrationSelection=="content-only-byte-exact-default"
    ' "$broray_tx_check/data/opt/broray/share/release/manifest.json" >/dev/null 2>&1 || broray_tx_fail candidate-runtime-manifest-invalid || return 1
    [ -x "$broray_tx_check/data/opt/broray/runtime/xray" ] || broray_tx_fail candidate-xray-missing || return 1
    [ -f "$broray_tx_check/data/opt/broray/lib/package-transaction.sh" ] || broray_tx_fail candidate-engine-missing || return 1
    broray_tx_sha "$broray_tx_check/data/opt/broray/lib/package-transaction.sh" >"$BRORAY_TX_WORK/candidate-engine.sha256"
    broray_tx_candidate_identity_sha="$(broray_tx_sha "$broray_tx_candidate")" || return 1
    broray_tx_candidate_identity_bytes="$(wc -c <"$broray_tx_candidate" | tr -d ' ')"
    broray_tx_number "$broray_tx_candidate_identity_bytes" || return 1
    jq -nc --arg package broray --arg version "$BRORAY_TX_TARGET_PACKAGE" \
        --arg architecture "$BRORAY_TX_TARGET_ARCH" --arg appVersion "$BRORAY_TX_TARGET_APP" \
        --arg webUIBuild "$BRORAY_TX_TARGET_WEBUI" --arg releaseId "$BRORAY_TX_TARGET_RELEASE" \
        --arg sha256 "$broray_tx_candidate_identity_sha" --arg engineSha256 "$(cat "$BRORAY_TX_WORK/candidate-engine.sha256")" \
        --argjson packageRevision "$BRORAY_TX_TARGET_REVISION" --argjson sizeBytes "$broray_tx_candidate_identity_bytes" '
      {schemaVersion:3,package:$package,version:$version,architecture:$architecture,
       appVersion:$appVersion,packageRevision:$packageRevision,webUIBuild:$webUIBuild,
       releaseId:$releaseId,sha256:$sha256,sizeBytes:$sizeBytes,engineSha256:$engineSha256}
    ' >"$BRORAY_TX_WORK/candidate-validated-identity.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/candidate-validated-identity.json.part" \
        "$BRORAY_TX_WORK/candidate-validated-identity.json" || return 1
}

broray_tx_candidate_space_finalize()
{
    broray_tx_candidate_bytes="$(wc -c <"$BRORAY_TX_WORK/candidate.ipk" | tr -d ' ')"
    broray_tx_number "$broray_tx_candidate_bytes" || return 1
    [ "$broray_tx_candidate_bytes" = "$(jq -r '.sizeBytes' "$BRORAY_TX_WORK/candidate.json")" ] || return 1
    broray_tx_candidate_kb="$(broray_tx_ceil_div "$broray_tx_candidate_bytes" 1024)" || return 1
    broray_tx_tmp_allocation_unit_kb="$(jq -r '.mountGraph.tmp.allocationUnitKB // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    broray_tx_number "$broray_tx_tmp_allocation_unit_kb" && [ "$broray_tx_tmp_allocation_unit_kb" -gt 0 ] || return 1
    broray_tx_candidate_staging_kb="$(du -sk "$BRORAY_TX_WORK/candidate-check/data" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_candidate_staging_kb" || broray_tx_fail cannot-measure-candidate-staging || return 1
    broray_tx_candidate_staging_bound="$(broray_tx_adjust_allocation_upper_kb \
        "$(jq -r '.targetAllocatedUpperKB' "$BRORAY_TX_WORK/candidate.json")" \
        "$(jq -r '.payloadObjectCount' "$BRORAY_TX_WORK/candidate.json")" "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_candidate_staging_bound="$(broray_tx_uadd "$broray_tx_candidate_staging_bound" "$broray_tx_tmp_allocation_unit_kb")" || return 1
    [ "$broray_tx_candidate_staging_kb" -le "$broray_tx_candidate_staging_bound" ] || broray_tx_fail candidate-staging-exceeds-declared-allocation || return 1
    broray_tx_control_staging_kb="$(du -sk "$BRORAY_TX_WORK/candidate-check/control" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_control_staging_kb" || return 1
    broray_tx_control_staging_bound="$(broray_tx_adjust_allocation_upper_kb \
        "$(jq -r '.controlAllocatedUpperKB' "$BRORAY_TX_WORK/candidate.json")" \
        "$(jq -r '.controlObjectCount' "$BRORAY_TX_WORK/candidate.json")" "$broray_tx_tmp_allocation_unit_kb")" || return 1
    broray_tx_control_staging_bound="$(broray_tx_uadd "$broray_tx_control_staging_bound" "$broray_tx_tmp_allocation_unit_kb")" || return 1
    [ "$broray_tx_control_staging_kb" -le "$broray_tx_control_staging_bound" ] || broray_tx_fail candidate-control-staging-exceeds-declared-allocation || return 1
    broray_tx_candidate_outer_bytes=0
    for broray_tx_candidate_outer in "$BRORAY_TX_WORK/candidate-check/outer/data.tar.gz" "$BRORAY_TX_WORK/candidate-check/outer/control.tar.gz" "$BRORAY_TX_WORK/candidate-check/outer/debian-binary"; do
        [ -f "$broray_tx_candidate_outer" ] || continue
        broray_tx_candidate_one_bytes="$(wc -c <"$broray_tx_candidate_outer" | tr -d ' ')"
        broray_tx_number "$broray_tx_candidate_one_bytes" || return 1
        broray_tx_candidate_outer_bytes="$(broray_tx_uadd "$broray_tx_candidate_outer_bytes" "$broray_tx_candidate_one_bytes")" || return 1
    done
    broray_tx_candidate_outer_kb="$(broray_tx_ceil_div "$broray_tx_candidate_outer_bytes" 1024)" || return 1
    [ "$broray_tx_candidate_outer_bytes" = "$(jq -r '.outerMembersBytes' "$BRORAY_TX_WORK/candidate.json")" ] ||
        broray_tx_fail candidate-outer-bytes-metadata-mismatch || return 1
    [ "$(find -P "$BRORAY_TX_WORK/candidate-check/outer" -mindepth 1 -maxdepth 1 -type f | wc -l | tr -d ' ')" = \
        "$(jq -r '.outerMemberCount' "$BRORAY_TX_WORK/candidate.json")" ] || broray_tx_fail candidate-outer-count-metadata-mismatch || return 1
    broray_tx_tmp_measure candidate-verify-expanded || return 1
    broray_tx_measured_peak="$(cat "$BRORAY_TX_WORK/measured-peak-tmp.kb")"
    broray_tx_number "$broray_tx_measured_peak" || return 1
    broray_tx_candidate_work_baseline="$(cat "$BRORAY_TX_WORK/tmp-work-after-snapshot.kb" 2>/dev/null)"
    broray_tx_number "$broray_tx_candidate_work_baseline" || return 1
    broray_tx_candidate_work_now="$(du -sk "$BRORAY_TX_WORK" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_candidate_work_now" || return 1
    broray_tx_candidate_phase_growth="$(broray_tx_sub0 "$broray_tx_candidate_work_now" "$broray_tx_candidate_work_baseline")" || return 1
    broray_tx_candidate_known_growth="$(du -sk "$BRORAY_TX_WORK/candidate.ipk" "$BRORAY_TX_WORK/candidate-check/outer" \
        "$BRORAY_TX_WORK/candidate-check/control" "$BRORAY_TX_WORK/candidate-check/data" 2>/dev/null |
        awk '{sum+=$1}END{printf "%.0f",sum+0}')"
    broray_tx_number "$broray_tx_candidate_known_growth" || return 1
    broray_tx_candidate_ancillary_growth="$(broray_tx_sub0 "$broray_tx_candidate_phase_growth" "$broray_tx_candidate_known_growth")" || return 1
    [ "$broray_tx_candidate_ancillary_growth" -le "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB" ] ||
        broray_tx_fail candidate-workspace-writer-cap-exceeded || return 1
    broray_tx_guard_work "$BRORAY_TX_WORK/candidate-check/data" || return 1
    rm -rf "$BRORAY_TX_WORK/candidate-check/data" || return 1
    broray_tx_tmp_measure candidate-verify-compacted || return 1
    broray_tx_tmp_free_after_candidate="${BRORAY_TX_TEST_TMP_FREE_AFTER_CANDIDATE_KB:-$(df -Pk "$BRORAY_TX_TMP_BASE" 2>/dev/null | awk 'NR==2{print $4;exit}')}"
    broray_tx_tmp_inodes_after_candidate="${BRORAY_TX_TEST_TMP_FREE_AFTER_CANDIDATE_INODES:-$(broray_tx_df_inodes "$BRORAY_TX_TMP_BASE")}"
    broray_tx_same_fs="$(jq -r 'if (.sameBackingFs|type)=="boolean" then (.sameBackingFs|tostring) else empty end' "$BRORAY_TX_WORK/evidence/space.json")"
    case "$broray_tx_same_fs" in true|false) ;; *) return 1 ;; esac
    if [ "$broray_tx_same_fs" = true ]; then
        broray_tx_opt_free_after_candidate="${BRORAY_TX_TEST_OPT_FREE_AFTER_CANDIDATE_KB:-$(df -Pk "$BRORAY_TX_OPT_ROOT" 2>/dev/null | awk 'NR==2{print $4;exit}')}"
        broray_tx_opt_inodes_after_candidate="${BRORAY_TX_TEST_OPT_FREE_AFTER_CANDIDATE_INODES:-$(broray_tx_df_inodes "$BRORAY_TX_OPT_ROOT")}"
    else
        broray_tx_opt_free_after_candidate=0
        broray_tx_opt_inodes_after_candidate=0
    fi
    broray_tx_number "$broray_tx_tmp_free_after_candidate" && broray_tx_number "$broray_tx_tmp_inodes_after_candidate" &&
        broray_tx_number "$broray_tx_opt_free_after_candidate" && broray_tx_number "$broray_tx_opt_inodes_after_candidate" || return 1
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_opt_free_after_candidate" = "$broray_tx_tmp_free_after_candidate" ] &&
            [ "$broray_tx_opt_inodes_after_candidate" = "$broray_tx_tmp_inodes_after_candidate" ] ||
            broray_tx_fail same-backing-fs-samples-disagree-after-candidate || return 1
    fi
    broray_tx_post_candidate_writer_upper_kb="$(jq -r '.postCandidateWriterUpperKB' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_number "$broray_tx_post_candidate_writer_upper_kb" || return 1
    broray_tx_future_tmp_required="$(broray_tx_uadd "$BRORAY_TX_TMP_RESERVE_KB" "$broray_tx_post_candidate_writer_upper_kb")" || return 1
    broray_tx_future_tmp_inodes="$(broray_tx_uadd "$BRORAY_TX_TMP_INODE_RESERVE" "$BRORAY_TX_POST_CANDIDATE_FUTURE_INODES")" || return 1
    broray_tx_measured_with_reserve="$broray_tx_future_tmp_required"
    broray_tx_measured_margin=$((broray_tx_tmp_free_after_candidate - broray_tx_future_tmp_required))
    broray_tx_measured_inode_margin=$((broray_tx_tmp_inodes_after_candidate - broray_tx_future_tmp_inodes))
    broray_tx_shared_required_after_candidate=0
    broray_tx_shared_inode_required_after_candidate=0
    broray_tx_shared_margin_after_candidate=0
    broray_tx_shared_inode_margin_after_candidate=0
    if [ "$broray_tx_same_fs" = true ]; then
        broray_tx_opt_peak_delta="$(jq -r '([.optForwardDeltaKB,.optRollbackDeltaKB]|max)' "$BRORAY_TX_WORK/evidence/space.json")"
        broray_tx_opt_inode_peak_delta="$(jq -r '([.optForwardInodeDelta,.optRollbackInodeDelta]|max)' "$BRORAY_TX_WORK/evidence/space.json")"
        broray_tx_number "$broray_tx_opt_peak_delta" && broray_tx_number "$broray_tx_opt_inode_peak_delta" || return 1
        broray_tx_shared_phase_delta_after_candidate="$(broray_tx_uadd "$broray_tx_opt_peak_delta" "$broray_tx_post_candidate_writer_upper_kb")" || return 1
        broray_tx_shared_required_after_candidate="$(broray_tx_uadd "$BRORAY_TX_OPT_RESERVE_KB" "$BRORAY_TX_TMP_RESERVE_KB")" || return 1
        broray_tx_shared_required_after_candidate="$(broray_tx_uadd "$broray_tx_shared_required_after_candidate" "$broray_tx_shared_phase_delta_after_candidate")" || return 1
        broray_tx_shared_phase_inode_delta_after_candidate="$(broray_tx_uadd "$broray_tx_opt_inode_peak_delta" "$BRORAY_TX_POST_CANDIDATE_FUTURE_INODES")" || return 1
        broray_tx_shared_inode_required_after_candidate="$(broray_tx_uadd "$BRORAY_TX_OPT_INODE_RESERVE" "$BRORAY_TX_TMP_INODE_RESERVE")" || return 1
        broray_tx_shared_inode_required_after_candidate="$(broray_tx_uadd "$broray_tx_shared_inode_required_after_candidate" "$broray_tx_shared_phase_inode_delta_after_candidate")" || return 1
        broray_tx_shared_margin_after_candidate=$((broray_tx_opt_free_after_candidate - broray_tx_shared_required_after_candidate))
        broray_tx_shared_inode_margin_after_candidate=$((broray_tx_opt_inodes_after_candidate - broray_tx_shared_inode_required_after_candidate))
    else
        broray_tx_shared_phase_delta_after_candidate=0
        broray_tx_shared_phase_inode_delta_after_candidate=0
    fi
    jq --argjson candidateBytes "$broray_tx_candidate_bytes" --argjson candidateActualKB "$broray_tx_candidate_kb" \
        --argjson candidateOuterActualKB "$broray_tx_candidate_outer_kb" --argjson candidateStagingActualKB "$broray_tx_candidate_staging_kb" \
        --argjson candidateControlStagingActualKB "$broray_tx_control_staging_kb" --argjson candidateAncillaryGrowthKB "$broray_tx_candidate_ancillary_growth" \
        --argjson measuredPeakWithSafetyReserveKB "$broray_tx_measured_with_reserve" --argjson measuredTmpSpaceMarginKB "$broray_tx_measured_margin" \
        --argjson tmpFutureRequiredAfterCandidateKB "$broray_tx_future_tmp_required" \
        --argjson tmpFreeAfterCandidateKB "$broray_tx_tmp_free_after_candidate" --argjson tmpFreeInodesAfterCandidate "$broray_tx_tmp_inodes_after_candidate" \
        --argjson tmpRequiredInodesAfterCandidate "$broray_tx_future_tmp_inodes" --argjson tmpInodeMarginAfterCandidate "$broray_tx_measured_inode_margin" \
        --argjson sameBackingFs "$broray_tx_same_fs" \
        --argjson sharedFreeAfterCandidateKB "$broray_tx_opt_free_after_candidate" \
        --argjson sharedFreeInodesAfterCandidate "$broray_tx_opt_inodes_after_candidate" \
        --argjson sharedRequiredAfterCandidateKB "$broray_tx_shared_required_after_candidate" \
        --argjson sharedRequiredInodesAfterCandidate "$broray_tx_shared_inode_required_after_candidate" \
        --argjson sharedSpaceMarginAfterCandidateKB "$broray_tx_shared_margin_after_candidate" \
        --argjson sharedInodeMarginAfterCandidate "$broray_tx_shared_inode_margin_after_candidate" \
        --argjson sharedPhaseDeltaAfterCandidateKB "$broray_tx_shared_phase_delta_after_candidate" \
        --argjson sharedPhaseDeltaInodesAfterCandidate "$broray_tx_shared_phase_inode_delta_after_candidate" \
        '.candidateBytes=$candidateBytes|.candidateActualKB=$candidateActualKB|.candidateOuterActualKB=$candidateOuterActualKB|
         .candidateStagingActualKB=$candidateStagingActualKB|.candidateControlStagingActualKB=$candidateControlStagingActualKB|
         .candidateAncillaryGrowthKB=$candidateAncillaryGrowthKB|.measuredPeakWithSafetyReserveKB=$measuredPeakWithSafetyReserveKB|
         .tmpFutureRequiredAfterCandidateKB=$tmpFutureRequiredAfterCandidateKB|
         .tmpFreeAfterCandidateKB=$tmpFreeAfterCandidateKB|
         .tmpFreeInodesAfterCandidate=$tmpFreeInodesAfterCandidate|.tmpRequiredInodesAfterCandidate=$tmpRequiredInodesAfterCandidate|
         .tmpInodeMarginAfterCandidate=$tmpInodeMarginAfterCandidate|
         .sharedFreeAfterCandidateKB=(if $sameBackingFs then $sharedFreeAfterCandidateKB else null end)|
         .sharedFreeInodesAfterCandidate=(if $sameBackingFs then $sharedFreeInodesAfterCandidate else null end)|
         .sharedRequiredAfterCandidateKB=(if $sameBackingFs then $sharedRequiredAfterCandidateKB else null end)|
         .sharedRequiredInodesAfterCandidate=(if $sameBackingFs then $sharedRequiredInodesAfterCandidate else null end)|
         .sharedSpaceMarginAfterCandidateKB=(if $sameBackingFs then $sharedSpaceMarginAfterCandidateKB else null end)|
         .sharedInodeMarginAfterCandidate=(if $sameBackingFs then $sharedInodeMarginAfterCandidate else null end)|
         .sharedPhaseDeltaAfterCandidateKB=(if $sameBackingFs then $sharedPhaseDeltaAfterCandidateKB else null end)|
         .sharedPhaseDeltaInodesAfterCandidate=(if $sameBackingFs then $sharedPhaseDeltaInodesAfterCandidate else null end)|
         .measuredTmpSpaceMarginKB=$measuredTmpSpaceMarginKB' \
        "$BRORAY_TX_WORK/evidence/space.json" >"$BRORAY_TX_WORK/evidence/space.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/evidence/space.json.part" "$BRORAY_TX_WORK/evidence/space.json" || return 1
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_shared_margin_after_candidate" -ge 0 ] || broray_tx_fail insufficient-shared-space-at-candidate-verification || return 1
        [ "$broray_tx_shared_inode_margin_after_candidate" -ge 0 ] || broray_tx_fail insufficient-shared-inodes-at-candidate-verification || return 1
    else
        [ "$broray_tx_measured_margin" -ge 0 ] || broray_tx_fail insufficient-tmp-space-at-candidate-verification || return 1
        [ "$broray_tx_measured_inode_margin" -ge 0 ] || broray_tx_fail insufficient-tmp-inodes-at-candidate-verification || return 1
    fi
    return 0
}

broray_tx_candidate_capability_evidence_bind()
{
    broray_tx_candidate_evidence_sha="$1"
    case "$broray_tx_candidate_evidence_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#broray_tx_candidate_evidence_sha}" -eq 64 ] || return 1
    broray_tx_native_opkg_lock_assert candidate-evidence-bind || return 1
    jq --arg contract "$BRORAY_RUNTIME_CAPABILITY_CONTRACT" --arg operationId "$BRORAY_TX_OPERATION_ID" \
        --arg candidateSha256 "$broray_tx_candidate_evidence_sha" '
      select(.contract==$contract and .operationId==$operationId and .status=="HELD" and .mutationStarted==false) |
      .candidateSha256=$candidateSha256
    ' "$BRORAY_TX_WORK/evidence/opkg-native-lock.json" >"$BRORAY_TX_WORK/evidence/opkg-native-lock.json.part" || return 1
    [ -s "$BRORAY_TX_WORK/evidence/opkg-native-lock.json.part" ] || return 1
    mv -f "$BRORAY_TX_WORK/evidence/opkg-native-lock.json.part" "$BRORAY_TX_WORK/evidence/opkg-native-lock.json" || return 1

    [ -f "$BRORAY_TX_WORK/services-before.tsv" ] && [ ! -L "$BRORAY_TX_WORK/services-before.tsv" ] || return 1
    [ -f "$BRORAY_TX_WORK/services-before.identity.tsv" ] && [ ! -L "$BRORAY_TX_WORK/services-before.identity.tsv" ] || return 1
    jq -Rn --arg contract "$BRORAY_RUNTIME_CAPABILITY_CONTRACT" --arg operationId "$BRORAY_TX_OPERATION_ID" \
        --arg candidateSha256 "$broray_tx_candidate_evidence_sha" \
        --argjson lab "$([ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && printf true || printf false)" '
      [inputs | split("\t") | select(length==4) |
        {service:.[0],state:.[1],pid:(if .[2]=="" then null else (.[2]|tonumber) end),
         cmdlineSha256:(if .[3]=="" then null else .[3] end)}] as $identities |
      {schemaVersion:1,contract:$contract,contractIds:["REQ-UPD-025",$contract],
       operationId:$operationId,candidateSha256:$candidateSha256,
       mutationStarted:false,identitySource:"NUL-safe-/proc/PID/cmdline-allowlist",
       execution:(if $lab then "NOT_RUN_IN_GENERIC_PACKAGE_FIXTURE" else "PREFLIGHT_MEASURED" end),
       identities:$identities,
       status:(if $lab and ($identities|length)==5 then "LAB_PROFILE"
         elif ($identities|length)==5 and
         all($identities[];(.state=="absent" or .state=="stopped" or .state=="running") and
           (if .state=="running" then (.pid!=null and (.cmdlineSha256|length)==64) else true end))
         then "PASS" else "FAIL" end)}
    ' <"$BRORAY_TX_WORK/services-before.identity.tsv" >"$BRORAY_TX_WORK/evidence/service-identities.json" || return 1
    jq -e --arg operationId "$BRORAY_TX_OPERATION_ID" --arg sha "$broray_tx_candidate_evidence_sha" '
      (.status=="PASS" or .status=="LAB_PROFILE") and .operationId==$operationId and
      .candidateSha256==$sha and .mutationStarted==false and
      (.contractIds==["REQ-UPD-025",.contract])
    ' "$BRORAY_TX_WORK/evidence/service-identities.json" >/dev/null 2>&1 || return 1
}

broray_tx_candidate_evidence_revalidate()
{
    [ -f "$BRORAY_TX_WORK/candidate.ipk" ] && [ ! -L "$BRORAY_TX_WORK/candidate.ipk" ] || return 1
    broray_tx_candidate_revalidate_sha="$(broray_tx_sha "$BRORAY_TX_WORK/candidate.ipk")" || return 1
    jq -e --arg contract "$BRORAY_RUNTIME_CAPABILITY_CONTRACT" --arg operationId "$BRORAY_TX_OPERATION_ID" --arg sha "$broray_tx_candidate_revalidate_sha" '
      .contract==$contract and .contractIds==["REQ-UPD-025",$contract] and
      .operationId==$operationId and .candidateSha256==$sha and .status=="HELD" and .mutationStarted==false and
      (.ownerStarttimeTicks|type)=="string" and (.ownerCmdlineSha256|length)==64 and (.ownerArgv|type)=="array"
    ' "$BRORAY_TX_WORK/evidence/opkg-native-lock.json" >/dev/null 2>&1 || return 1
    jq -e --arg contract "$BRORAY_RUNTIME_CAPABILITY_CONTRACT" --arg operationId "$BRORAY_TX_OPERATION_ID" --arg sha "$broray_tx_candidate_revalidate_sha" \
      --argjson lab "$([ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ] && printf true || printf false)" '
      .contract==$contract and .contractIds==["REQ-UPD-025",$contract] and
      .operationId==$operationId and .candidateSha256==$sha and
      .status==(if $lab then "LAB_PROFILE" else "PASS" end) and .mutationStarted==false
    ' "$BRORAY_TX_WORK/evidence/service-identities.json" >/dev/null 2>&1 || return 1
    broray_tx_native_opkg_lock_assert candidate-evidence-pre-mutation
}

broray_tx_candidate_download_verify()
{
    broray_tx_event candidate-download || return 1
    [ -f "$BRORAY_TX_WORK/snapshot.verified" ] && [ ! -L "$BRORAY_TX_WORK/snapshot.verified" ] || broray_tx_fail candidate-download-before-snapshot-verification || return 1
    broray_tx_inject candidate-download || return 1
    if [ ! -f "$BRORAY_TX_WORK/candidate.json" ] || [ -L "$BRORAY_TX_WORK/candidate.json" ]; then
        broray_tx_release_metadata "${1:-}" || return 1
    fi
    broray_tx_candidate_url="$(jq -r '.baseUrl + "/" + .filename' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_candidate_declared_bytes="$(jq -r '.sizeBytes' "$BRORAY_TX_WORK/candidate.json")"
    broray_tx_number "$broray_tx_candidate_declared_bytes" || return 1
    broray_tx_fetch "$broray_tx_candidate_url" "$BRORAY_TX_WORK/candidate.ipk.part" "$broray_tx_candidate_declared_bytes" 2>"$BRORAY_TX_WORK/evidence/candidate-download.stderr" || broray_tx_fail candidate-download-failed || return 1
    mv -f "$BRORAY_TX_WORK/candidate.ipk.part" "$BRORAY_TX_WORK/candidate.ipk" || return 1
    [ -f "$BRORAY_TX_WORK/candidate.ipk" ] && [ ! -L "$BRORAY_TX_WORK/candidate.ipk" ] || broray_tx_fail candidate-not-regular || return 1
    broray_tx_event candidate-downloaded || return 1
    [ "$(broray_tx_sha "$BRORAY_TX_WORK/candidate.ipk")" = "$(jq -r '.sha256' "$BRORAY_TX_WORK/candidate.json")" ] || broray_tx_fail candidate-sha-mismatch || return 1
    [ "$(wc -c <"$BRORAY_TX_WORK/candidate.ipk" | tr -d ' ')" = "$(jq -r '.sizeBytes' "$BRORAY_TX_WORK/candidate.json")" ] || broray_tx_fail candidate-size-mismatch || return 1
    broray_tx_candidate_structure_verify "$BRORAY_TX_WORK/candidate.ipk" || return 1
    broray_tx_candidate_bound_sha="$(broray_tx_sha "$BRORAY_TX_WORK/candidate.ipk")"
    case "$BRORAY_TX_MODE" in
        reinstall)
            [ "$(cat "$BRORAY_TX_INFO_ROOT/broray.candidate-sha256" 2>/dev/null)" = "$broray_tx_candidate_bound_sha" ] || broray_tx_fail reinstall-source-bytes-not-canonical || return 1
            ;;
        update)
            if [ "$(cat "$BRORAY_TX_INFO_ROOT/broray.candidate-sha256" 2>/dev/null)" = "$broray_tx_candidate_bound_sha" ]; then
                broray_tx_fail update-target-already-installed
                return 1
            fi
            ;;
    esac
    case "$broray_tx_candidate_url" in file://*) broray_tx_transfer_production=false ;; *) broray_tx_transfer_production=true ;; esac
    jq --arg candidateSha256 "$broray_tx_candidate_bound_sha" --arg url "$broray_tx_candidate_url" --argjson production "$broray_tx_transfer_production" \
       '.candidateSha256=$candidateSha256|.candidateBindingStage="candidate-verified"|
        .candidateTransfer={url:$url,exactProductionArgvExecuted:$production,
          labFileFixture:($production|not),sizeAndSha256Verified:true}' \
       "$BRORAY_TX_WORK/evidence/capabilities.json" >"$BRORAY_TX_WORK/evidence/capabilities.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/evidence/capabilities.json.part" "$BRORAY_TX_WORK/evidence/capabilities.json" || return 1
    broray_tx_candidate_capability_evidence_bind "$broray_tx_candidate_bound_sha" || return 1
    broray_tx_candidate_space_finalize || return 1
    broray_tx_persistent_marker_phase_reach candidate-verified || return 1
    broray_tx_event candidate-verified || return 1
    broray_tx_test_pause candidate-verified || return 1
    broray_tx_status running ''
}

broray_tx_user_roots()
{
    printf '%s\n' backup backups config/active-server config/config.json config/interface.json config/subscriptions \
        config/disabled-subscription-servers config/system/settings.json config/system/server-auto-switch.json \
        config/system/dns.json config/system/dot.json config/dns config/dot data deleted-subscriptions subscriptions servers \
        routes/config.json routes/bundles.json routes/custom.json routes/user-import-version routes/catalog \
        routes/dot/config.json routes/dot/state.json routes/installed routes/state routes/backup
}

broray_tx_candidate_protected_default_metrics()
{
    broray_tx_protected_default_manifest="$1"
    broray_tx_protected_default_output="$2"
    [ -f "$broray_tx_protected_default_manifest" ] && [ ! -L "$broray_tx_protected_default_manifest" ] || return 1
    broray_tx_protected_default_roots="$broray_tx_protected_default_output.roots"
    {
        broray_tx_user_roots
        awk -F '|' '$2 ~ /^opt\/broray\/routes\/manifests\/user-[0-9A-Za-z._-]+\.json$/ {
          sub(/^opt\/broray\//,"",$2); print $2
        }' "$broray_tx_protected_default_manifest"
    } >"$broray_tx_protected_default_roots.unsorted" || return 1
    broray_tx_sort_file unique "$broray_tx_protected_default_roots.unsorted" \
        "$broray_tx_protected_default_roots" || return 1
    : >"$broray_tx_protected_default_output" || return 1
    while IFS= read -r broray_tx_protected_default_root; do
        broray_tx_relative_safe "$broray_tx_protected_default_root" || return 1
        broray_tx_protected_default_prefix="opt/broray/$broray_tx_protected_default_root"
        broray_tx_protected_default_metrics="$(awk -F '|' -v p="$broray_tx_protected_default_prefix" '
          $2==p || index($2,p "/")==1 {
            objects++
            if ($1=="F") allocated+=int(($3+4095)/4096)*4
            else if ($1=="D" || $1=="L") allocated+=4
            else bad=1
          }
          END {if(bad)exit 1; printf "%.0f|%.0f",objects+0,allocated+0}
        ' "$broray_tx_protected_default_manifest")" || return 1
        broray_tx_protected_default_objects="${broray_tx_protected_default_metrics%%|*}"
        broray_tx_protected_default_alloc="${broray_tx_protected_default_metrics#*|}"
        broray_tx_number "$broray_tx_protected_default_objects" &&
            broray_tx_number "$broray_tx_protected_default_alloc" || return 1
        [ "$broray_tx_protected_default_objects" -eq 0 ] ||
            printf '%s|%s|%s\n' "$broray_tx_protected_default_root" \
                "$broray_tx_protected_default_alloc" "$broray_tx_protected_default_objects" \
                >>"$broray_tx_protected_default_output" || return 1
    done <"$broray_tx_protected_default_roots"
    rm -f "$broray_tx_protected_default_roots" "$broray_tx_protected_default_roots.unsorted"
}

broray_tx_user_validate()
{
    broray_tx_user_base="$1"; broray_tx_user_prefix="$2"
    : >"$BRORAY_TX_WORK/$broray_tx_user_prefix-roots.list" || return 1
    broray_tx_user_roots | while IFS= read -r broray_tx_user_rel; do
        [ -e "$broray_tx_user_base/$broray_tx_user_rel" ] || [ -L "$broray_tx_user_base/$broray_tx_user_rel" ] || continue
        printf '%s\n' "$broray_tx_user_rel"
    done >"$BRORAY_TX_WORK/$broray_tx_user_prefix-roots.list" || return 1
    if [ -d "$broray_tx_user_base/routes/manifests" ] && [ ! -L "$broray_tx_user_base/routes/manifests" ]; then
        for broray_tx_user_item in "$broray_tx_user_base"/routes/manifests/user-*.json; do
            [ -f "$broray_tx_user_item" ] && [ ! -L "$broray_tx_user_item" ] || continue
            printf 'routes/manifests/%s\n' "${broray_tx_user_item##*/}" >>"$BRORAY_TX_WORK/$broray_tx_user_prefix-roots.list"
        done
    fi
    broray_tx_sort_file unique "$BRORAY_TX_WORK/$broray_tx_user_prefix-roots.list" "$BRORAY_TX_WORK/$broray_tx_user_prefix-roots.list" || return 1
    while IFS= read -r broray_tx_user_rel; do
        [ -n "$broray_tx_user_rel" ] || continue
        broray_tx_user_path="$broray_tx_user_base/$broray_tx_user_rel"
        [ ! -L "$broray_tx_user_path" ] || broray_tx_fail "protected-symlink:$broray_tx_user_rel" || return 1
        if [ -d "$broray_tx_user_path" ]; then
            find "$broray_tx_user_path" -xdev -print | while IFS= read -r broray_tx_user_child; do
                [ ! -L "$broray_tx_user_child" ] || exit 71
                [ -d "$broray_tx_user_child" ] || [ -f "$broray_tx_user_child" ] || exit 72
            done || broray_tx_fail "protected-object-schema-unsupported:$broray_tx_user_rel" || return 1
        elif [ -f "$broray_tx_user_path" ]; then
            # Protected state is an opaque byte stream.  A JSON suffix is a
            # source diagnostic, not an admission selector: released sources
            # legitimately contain zero-byte placeholders, and any semantic
            # incompatibility is handled by candidate postchecks + rollback.
            :
        else broray_tx_fail "protected-object-type-unsupported:$broray_tx_user_rel"; return 1; fi
    done <"$BRORAY_TX_WORK/$broray_tx_user_prefix-roots.list"
    if [ -s "$BRORAY_TX_WORK/$broray_tx_user_prefix-roots.list" ]; then
        broray_tx_manifest_build "$broray_tx_user_base" "$BRORAY_TX_WORK/$broray_tx_user_prefix-roots.list" "$BRORAY_TX_WORK/$broray_tx_user_prefix.manifest" || broray_tx_fail protected-manifest-build-failed || return 1
    else : >"$BRORAY_TX_WORK/$broray_tx_user_prefix.manifest"; fi
}

broray_tx_restore_root_allowed()
{
    broray_tx_restore_root="${1:-}"
    broray_tx_relative_safe "$broray_tx_restore_root" || return 1
    case "$broray_tx_restore_root" in
        routes/manifests/user-*.json)
            broray_tx_restore_name="${broray_tx_restore_root#routes/manifests/user-}"
            case "$broray_tx_restore_name" in ''|*/*|*[!0-9A-Za-z._-]*) return 1 ;; esac
            return 0
            ;;
    esac
    broray_tx_user_roots | grep -Fqx "$broray_tx_restore_root"
}

broray_tx_protected_backup_persist()
{
    [ -s "$BRORAY_TX_WORK/protected-source-roots.list" ] || return 0
    broray_tx_event protected-backup-create || return 1
    broray_tx_backup_stage="$BRORAY_TX_WORK/protected-backup.stage"
    broray_tx_backup_part="$BRORAY_TX_WORK/protected-backup.tar.gz.part"
    broray_tx_guard_work "$broray_tx_backup_stage" && broray_tx_guard_work "$broray_tx_backup_part" || return 1
    rm -rf "$broray_tx_backup_stage"
    rm -f "$broray_tx_backup_part"
    mkdir -p "$broray_tx_backup_stage" || return 1

    while IFS= read -r broray_tx_backup_rel; do
        [ -n "$broray_tx_backup_rel" ] || continue
        broray_tx_restore_root_allowed "$broray_tx_backup_rel" || broray_tx_fail "protected-backup-root-not-registered:$broray_tx_backup_rel" || return 1
        broray_tx_backup_source="$BRORAY_TX_APP_ROOT/$broray_tx_backup_rel"
        [ -e "$broray_tx_backup_source" ] && [ ! -L "$broray_tx_backup_source" ] || broray_tx_fail "protected-backup-source-missing:$broray_tx_backup_rel" || return 1
        broray_tx_backup_parent="${broray_tx_backup_rel%/*}"
        [ "$broray_tx_backup_parent" = "$broray_tx_backup_rel" ] && broray_tx_backup_parent=""
        [ -z "$broray_tx_backup_parent" ] || mkdir -p "$broray_tx_backup_stage/$broray_tx_backup_parent" || return 1
        cp -pR "$broray_tx_backup_source" "$broray_tx_backup_stage/$broray_tx_backup_rel" || return 1
    done <"$BRORAY_TX_WORK/protected-source-roots.list"

    broray_tx_user_validate "$broray_tx_backup_stage" protected-backup-stage || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/protected-source-roots.list" "$BRORAY_TX_WORK/protected-backup-stage-roots.list" || broray_tx_fail protected-backup-root-set-mismatch || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/protected-source.manifest" "$BRORAY_TX_WORK/protected-backup-stage.manifest" || broray_tx_fail protected-backup-manifest-mismatch || return 1
    cp -p "$BRORAY_TX_WORK/protected-source-roots.list" "$broray_tx_backup_stage/.broray-protected-paths" || return 1
    cp -p "$BRORAY_TX_WORK/protected-source.manifest" "$broray_tx_backup_stage/.broray-protected-manifest" || return 1
    printf '%s\n' 'BROray protected backup/2' >"$broray_tx_backup_stage/.broray-protected-format" || return 1
    tar -czf "$broray_tx_backup_part" -C "$broray_tx_backup_stage" . 2>"$BRORAY_TX_WORK/evidence/protected-backup-create.stderr" || return 1
    broray_tx_tar_safe "$broray_tx_backup_part" "$BRORAY_TX_WORK/protected-backup.members" || broray_tx_fail protected-backup-archive-unsafe || return 1
    for broray_tx_backup_metadata in .broray-protected-format .broray-protected-paths .broray-protected-manifest; do
        grep -Fqx "$broray_tx_backup_metadata" "$BRORAY_TX_WORK/protected-backup.members" || broray_tx_fail "protected-backup-metadata-missing:$broray_tx_backup_metadata" || return 1
    done
    broray_tx_backup_sha="$(broray_tx_sha "$broray_tx_backup_part")"
    [ "${#broray_tx_backup_sha}" -eq 64 ] || return 1
    broray_tx_backup_dir="$BRORAY_TX_APP_ROOT/backup"
    if [ -e "$broray_tx_backup_dir" ] || [ -L "$broray_tx_backup_dir" ]; then
        [ -d "$broray_tx_backup_dir" ] && [ ! -L "$broray_tx_backup_dir" ] || return 1
    else
        mkdir -p "$broray_tx_backup_dir" || return 1
    fi
    broray_tx_backup_target="$broray_tx_backup_dir/user-data-$BRORAY_TX_OPERATION_ID.tar.gz"
    case "$broray_tx_backup_target" in "$BRORAY_TX_APP_ROOT"/backup/user-data-*.tar.gz) ;; *) return 1 ;; esac
    [ ! -e "$broray_tx_backup_target" ] && [ ! -L "$broray_tx_backup_target" ] || return 1
    broray_tx_backup_opt_free="$(df -Pk "$BRORAY_TX_OPT_ROOT" 2>/dev/null | awk 'NR==2{print $4;exit}')"
    broray_tx_backup_kb="$(du -sk "$broray_tx_backup_part" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_backup_opt_free" && broray_tx_number "$broray_tx_backup_kb" || return 1
    [ "$broray_tx_backup_opt_free" -ge $((broray_tx_backup_kb + BRORAY_TX_OPT_RESERVE_KB)) ] || broray_tx_fail insufficient-opt-space-for-protected-backup || return 1
    cp -p "$broray_tx_backup_part" "$broray_tx_backup_target.part" || return 1
    [ "$(broray_tx_sha "$broray_tx_backup_target.part")" = "$broray_tx_backup_sha" ] || { rm -f "$broray_tx_backup_target.part"; return 1; }
    mv -f "$broray_tx_backup_target.part" "$broray_tx_backup_target" || return 1
    chmod 600 "$broray_tx_backup_target" 2>/dev/null || true
    printf '%s  %s\n' "$broray_tx_backup_sha" "${broray_tx_backup_target##*/}" >"$broray_tx_backup_target.sha256.part" || return 1
    mv -f "$broray_tx_backup_target.sha256.part" "$broray_tx_backup_target.sha256" || return 1

    mkdir -p "$BRORAY_TX_STATE_ROOT" "$BRORAY_TX_APP_ROOT/run/broray" || return 1
    [ -d "$BRORAY_TX_STATE_ROOT" ] && [ ! -L "$BRORAY_TX_STATE_ROOT" ] || return 1
    printf '%s\n' "$broray_tx_backup_target" >"$BRORAY_TX_STATE_ROOT/last-backup.part" || return 1
    mv -f "$BRORAY_TX_STATE_ROOT/last-backup.part" "$BRORAY_TX_STATE_ROOT/last-backup" || return 1
    printf '%s\n' "$broray_tx_backup_target" >"$BRORAY_TX_APP_ROOT/run/broray/last-backup.part" || return 1
    mv -f "$BRORAY_TX_APP_ROOT/run/broray/last-backup.part" "$BRORAY_TX_APP_ROOT/run/broray/last-backup" || return 1
    jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" --arg archive "$broray_tx_backup_target" \
        --arg sha256 "$broray_tx_backup_sha" --argjson sizeBytes "$(wc -c <"$broray_tx_backup_target" | tr -d ' ')" \
        '{schemaVersion:2,status:"PASS",format:"BROray protected backup/2",operationId:$operationId,
          archive:$archive,sha256:$sha256,sizeBytes:$sizeBytes,manifestVerified:true,rootSetVerified:true}' \
        >"$BRORAY_TX_WORK/evidence/protected-backup.json" || return 1
    rm -rf "$broray_tx_backup_stage"
    rm -f "$broray_tx_backup_part"
    broray_tx_event protected-backup-verified
}

broray_tx_restore_archive_prepare()
{
    broray_tx_restore_archive="${1:-}"
    case "$broray_tx_restore_archive" in "$BRORAY_TX_APP_ROOT"/backup/user-data-*.tar.gz) ;; *) broray_tx_fail restore-archive-outside-managed-root; return 1 ;; esac
    [ -f "$broray_tx_restore_archive" ] && [ ! -L "$broray_tx_restore_archive" ] && [ -s "$broray_tx_restore_archive" ] || broray_tx_fail restore-archive-not-regular || return 1
    broray_tx_restore_resolved="$(readlink -f "$broray_tx_restore_archive" 2>/dev/null)" || return 1
    [ "$broray_tx_restore_resolved" = "$broray_tx_restore_archive" ] || broray_tx_fail restore-archive-path-substitution || return 1
    broray_tx_restore_source_sha="$(broray_tx_sha "$broray_tx_restore_archive")"
    [ "${#broray_tx_restore_source_sha}" -eq 64 ] || return 1
    cp -p "$broray_tx_restore_archive" "$BRORAY_TX_WORK/restore-input.tar.gz.part" || return 1
    [ "$(broray_tx_sha "$BRORAY_TX_WORK/restore-input.tar.gz.part")" = "$broray_tx_restore_source_sha" ] || return 1
    mv -f "$BRORAY_TX_WORK/restore-input.tar.gz.part" "$BRORAY_TX_WORK/restore-input.tar.gz" || return 1
    [ "$(broray_tx_sha "$broray_tx_restore_archive")" = "$broray_tx_restore_source_sha" ] || broray_tx_fail restore-archive-changed-during-copy || return 1
    broray_tx_tmp_measure restore-archive-copied || return 1
    broray_tx_restore_capacity="$(cat "$BRORAY_TX_WORK/tmp-operation-capacity.kb" 2>/dev/null)"
    broray_tx_restore_work_kb="$(du -sk "$BRORAY_TX_WORK" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_number "$broray_tx_restore_capacity" && broray_tx_number "$broray_tx_restore_work_kb" || return 1
    [ $((broray_tx_restore_work_kb + BRORAY_TX_TMP_RESERVE_KB)) -le "$broray_tx_restore_capacity" ] || broray_tx_fail insufficient-tmp-space-for-restore-archive || return 1
    broray_tx_tar_safe "$BRORAY_TX_WORK/restore-input.tar.gz" "$BRORAY_TX_WORK/restore-input.members" || broray_tx_fail restore-archive-unsafe || return 1
    tar -tvzf "$BRORAY_TX_WORK/restore-input.tar.gz" >"$BRORAY_TX_WORK/restore-input.verbose" 2>"$BRORAY_TX_WORK/evidence/restore-list.stderr" || return 1
    awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" {bad=1} END{exit bad?1:0}' "$BRORAY_TX_WORK/restore-input.verbose" || broray_tx_fail restore-archive-non-regular-object || return 1
    broray_tx_restore_verbose_count="$(wc -l <"$BRORAY_TX_WORK/restore-input.verbose" | tr -d ' ')"
    broray_tx_number "$broray_tx_restore_verbose_count" || return 1
    [ "$broray_tx_restore_verbose_count" -eq "$broray_tx_tar_raw_count" ] || broray_tx_fail restore-archive-member-count-mismatch || return 1
    for broray_tx_restore_metadata in .broray-protected-format .broray-protected-paths .broray-protected-manifest; do
        grep -Fqx "$broray_tx_restore_metadata" "$BRORAY_TX_WORK/restore-input.members" || broray_tx_fail "restore-metadata-missing:$broray_tx_restore_metadata" || return 1
    done
    if ! tar -xzOf "$BRORAY_TX_WORK/restore-input.tar.gz" ./.broray-protected-format >"$BRORAY_TX_WORK/restore-format" 2>/dev/null; then
        tar -xzOf "$BRORAY_TX_WORK/restore-input.tar.gz" .broray-protected-format >"$BRORAY_TX_WORK/restore-format" 2>/dev/null || return 1
    fi
    [ "$(sed -n '1p' "$BRORAY_TX_WORK/restore-format")" = 'BROray protected backup/2' ] && [ "$(wc -l <"$BRORAY_TX_WORK/restore-format" | tr -d ' ')" -eq 1 ] || broray_tx_fail restore-format-unsupported || return 1
    if ! tar -xzOf "$BRORAY_TX_WORK/restore-input.tar.gz" ./.broray-protected-paths >"$BRORAY_TX_WORK/restore-roots.declared" 2>/dev/null; then
        tar -xzOf "$BRORAY_TX_WORK/restore-input.tar.gz" .broray-protected-paths >"$BRORAY_TX_WORK/restore-roots.declared" 2>/dev/null || return 1
    fi
    [ -s "$BRORAY_TX_WORK/restore-roots.declared" ] || broray_tx_fail restore-root-set-empty || return 1
    while IFS= read -r broray_tx_restore_rel; do
        [ -n "$broray_tx_restore_rel" ] || broray_tx_fail restore-root-empty || return 1
        broray_tx_restore_root_allowed "$broray_tx_restore_rel" || broray_tx_fail "restore-root-not-registered:$broray_tx_restore_rel" || return 1
    done <"$BRORAY_TX_WORK/restore-roots.declared"
    broray_tx_sort_file unique "$BRORAY_TX_WORK/restore-roots.declared" "$BRORAY_TX_WORK/restore-roots.list" || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/restore-roots.declared" "$BRORAY_TX_WORK/restore-roots.list" || broray_tx_fail restore-root-set-not-canonical || return 1
    while IFS= read -r broray_tx_restore_member; do
        broray_tx_restore_member_allowed=0
        case "$broray_tx_restore_member" in .broray-protected-format|.broray-protected-paths|.broray-protected-manifest) broray_tx_restore_member_allowed=1 ;; esac
        if [ "$broray_tx_restore_member_allowed" -eq 0 ]; then
            while IFS= read -r broray_tx_restore_rel; do
                case "$broray_tx_restore_member" in "$broray_tx_restore_rel"|"$broray_tx_restore_rel"/*) broray_tx_restore_member_allowed=1; break ;; esac
                case "$broray_tx_restore_rel" in "$broray_tx_restore_member"/*) broray_tx_restore_member_allowed=1; break ;; esac
            done <"$BRORAY_TX_WORK/restore-roots.list"
        fi
        [ "$broray_tx_restore_member_allowed" -eq 1 ] || broray_tx_fail "restore-member-outside-declared-roots:$broray_tx_restore_member" || return 1
    done <"$BRORAY_TX_WORK/restore-input.members"
    broray_tx_restore_stage="$BRORAY_TX_WORK/restore-candidate"
    mkdir -p "$broray_tx_restore_stage" || return 1
    tar -xzf "$BRORAY_TX_WORK/restore-input.tar.gz" -C "$broray_tx_restore_stage" 2>"$BRORAY_TX_WORK/evidence/restore-extract.stderr" || return 1
    [ -f "$broray_tx_restore_stage/.broray-protected-manifest" ] && [ ! -L "$broray_tx_restore_stage/.broray-protected-manifest" ] || return 1
    broray_tx_user_validate "$broray_tx_restore_stage" restore-candidate || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/restore-roots.list" "$BRORAY_TX_WORK/restore-candidate-roots.list" || broray_tx_fail restore-extracted-root-set-mismatch || return 1
    broray_tx_files_equal "$broray_tx_restore_stage/.broray-protected-manifest" "$BRORAY_TX_WORK/restore-candidate.manifest" || broray_tx_fail restore-archive-manifest-mismatch || return 1
    jq -nc --arg archive "$broray_tx_restore_archive" --arg sha256 "$broray_tx_restore_source_sha" \
        --argjson sizeBytes "$(wc -c <"$BRORAY_TX_WORK/restore-input.tar.gz" | tr -d ' ')" \
        '{schemaVersion:2,status:"PASS",format:"BROray protected backup/2",archive:$archive,
          sha256:$sha256,sizeBytes:$sizeBytes,memberPathsSafe:true,objectTypesSafe:true,
          rootSetRegistered:true,manifestVerified:true,mutationStarted:false}' \
        >"$BRORAY_TX_WORK/evidence/restore-input.json" || return 1
    broray_tx_persistent_marker_phase_reach candidate-verified || return 1
    broray_tx_test_pause candidate-verified || return 1
    broray_tx_event restore-archive-verified
}

broray_tx_restore_apply()
{
    broray_tx_user_validate "$BRORAY_TX_APP_ROOT" protected-current || return 1
    broray_tx_stop_services || broray_tx_fail restore-service-stop-failed || return 1
    broray_tx_operation_relation_verify pre-mutation || return 1
    broray_tx_native_opkg_lock_assert restore-mutation-barrier || return 1
    broray_tx_event first-destructive-mutation || return 1
    BRORAY_TX_MUTATED=1
    printf '%s\n' yes >"$BRORAY_TX_WORK/mutation.started" || return 1
    broray_tx_persistent_marker_phase_reach mutation-started || return 1
    broray_tx_test_pause mutation-started || return 1
    broray_tx_status running '' || return 1
    {
        cat "$BRORAY_TX_WORK/protected-current-roots.list"
        cat "$BRORAY_TX_WORK/restore-roots.list"
    } >"$BRORAY_TX_WORK/restore-remove.unsorted" || return 1
    broray_tx_sort_file unique "$BRORAY_TX_WORK/restore-remove.unsorted" "$BRORAY_TX_WORK/restore-remove.list" || return 1
    while IFS= read -r broray_tx_restore_rel; do
        [ -n "$broray_tx_restore_rel" ] || continue
        broray_tx_restore_root_allowed "$broray_tx_restore_rel" || return 1
        broray_tx_restore_target="$BRORAY_TX_APP_ROOT/$broray_tx_restore_rel"
        case "$broray_tx_restore_target" in "$BRORAY_TX_APP_ROOT"/*) ;; *) return 1 ;; esac
        [ -e "$broray_tx_restore_target" ] || [ -L "$broray_tx_restore_target" ] || continue
        rm -rf "$broray_tx_restore_target" || return 1
    done <"$BRORAY_TX_WORK/restore-remove.list"
    while IFS= read -r broray_tx_restore_rel; do
        [ -n "$broray_tx_restore_rel" ] || continue
        broray_tx_restore_source="$BRORAY_TX_WORK/restore-candidate/$broray_tx_restore_rel"
        broray_tx_restore_target="$BRORAY_TX_APP_ROOT/$broray_tx_restore_rel"
        [ -e "$broray_tx_restore_source" ] && [ ! -L "$broray_tx_restore_source" ] || return 1
        broray_tx_restore_parent="${broray_tx_restore_target%/*}"
        mkdir -p "$broray_tx_restore_parent" || return 1
        cp -pR "$broray_tx_restore_source" "$broray_tx_restore_target" || return 1
    done <"$BRORAY_TX_WORK/restore-roots.list"
    broray_tx_user_validate "$BRORAY_TX_APP_ROOT" restore-applied || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/restore-roots.list" "$BRORAY_TX_WORK/restore-applied-roots.list" || broray_tx_fail restore-target-root-set-mismatch || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/restore-candidate.manifest" "$BRORAY_TX_WORK/restore-applied.manifest" || broray_tx_fail restore-target-manifest-mismatch || return 1
    broray_tx_service_state_restore || broray_tx_fail restore-service-state-failed || return 1
    broray_tx_event restore-applied
}

broray_tx_restore_postcheck()
{
    broray_tx_event restore-postcheck || return 1
    broray_tx_inject restore-postcheck || return 1
    [ -f "$BRORAY_TX_APP_ROOT/config/version" ] && [ "$(sed -n '1p' "$BRORAY_TX_APP_ROOT/config/version")" = "$BRORAY_TX_TARGET_APP" ] || broray_tx_fail restore-postcheck-app-version || return 1
    jq -e --arg build "$BRORAY_TX_TARGET_WEBUI" --arg release "$BRORAY_TX_TARGET_RELEASE" '.buildId==$build and .releaseId==$release' "$BRORAY_TX_APP_ROOT/web-new/build.json" >/dev/null 2>&1 || broray_tx_fail restore-postcheck-webui-build || return 1
    [ -x "$BRORAY_TX_APP_ROOT/bin/xray" ] || broray_tx_fail restore-postcheck-xray-executable || return 1
    jq -e --arg release "$BRORAY_TX_TARGET_RELEASE" '.schemaVersion==3 and .releaseId==$release and .historicalTransactionStateRequired==false' "$BRORAY_TX_APP_ROOT/share/release/manifest.json" >/dev/null 2>&1 || broray_tx_fail restore-postcheck-runtime-manifest || return 1
    broray_tx_user_validate "$BRORAY_TX_APP_ROOT" restore-postcheck || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/restore-roots.list" "$BRORAY_TX_WORK/restore-postcheck-roots.list" || broray_tx_fail restore-postcheck-root-set || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/restore-candidate.manifest" "$BRORAY_TX_WORK/restore-postcheck.manifest" || broray_tx_fail restore-postcheck-manifest || return 1
    broray_tx_external_postcheck || broray_tx_fail restore-postcheck-external-keenetic-state || return 1
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ]; then
        for broray_tx_service in S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions; do [ -f "$BRORAY_TX_OPT_ROOT/etc/init.d/$broray_tx_service" ] || return 1; done
    else
        [ -f "$BRORAY_TX_APP_ROOT/config/config.json" ] || broray_tx_fail restore-postcheck-xray-config-missing || return 1
        XRAY_LOCATION_ASSET="$BRORAY_TX_APP_ROOT/bin" "$BRORAY_TX_APP_ROOT/bin/xray" run -test -c "$BRORAY_TX_APP_ROOT/config/config.json" >/dev/null 2>&1 || broray_tx_fail restore-postcheck-xray-config-test || return 1
        broray_tx_service_state_postcheck || broray_tx_fail restore-postcheck-service-state || return 1
        broray_tx_lan="$(jq -r '.listenAddress // empty' "$BRORAY_TX_APP_ROOT/config/system/settings.json")"
        [ -n "$broray_tx_lan" ] && broray_tx_local_http_probe "$broray_tx_lan" 8 >/dev/null 2>&1 || broray_tx_fail restore-postcheck-webui-http || return 1
    fi
    broray_tx_event restore-postcheck-pass
}

broray_tx_service_pid_file()
{
    case "$1" in
        S23broray-monitor) printf '%s\n' "$BRORAY_TX_APP_ROOT/run/connection-monitor.pid" ;;
        S24broray) printf '%s\n' '' ;;
        S25broray-web) printf '%s\n' "$BRORAY_TX_APP_ROOT/run/lighttpd.pid" ;;
        S27broray-auto-switch) printf '%s\n' "$BRORAY_TX_APP_ROOT/run/server-auto-switch.pid" ;;
        S28broray-subscriptions) printf '%s\n' "$BRORAY_TX_APP_ROOT/run/subscription-scheduler.pid" ;;
        *) return 1 ;;
    esac
}

# Parse /proc/PID/cmdline as decimal bytes, preserving NUL argument boundaries.
# Only ASCII argv values from the fixed service allowlist are accepted; no ps
# text, basename-only pidof result or init-script status is treated as identity.
broray_tx_service_cmdline_match()
{
    broray_tx_service_cmdline_pid="$1"
    broray_tx_service_cmdline_name="$2"
    broray_tx_service_cmdline_file="$BRORAY_TX_PROC_ROOT/$broray_tx_service_cmdline_pid/cmdline"
    [ -r "$broray_tx_service_cmdline_file" ] || return 1
    od -An -tu1 -v "$broray_tx_service_cmdline_file" 2>/dev/null | awk -v service="$broray_tx_service_cmdline_name" '
      {
        for (i=1;i<=NF;i++) {
          if ($i !~ /^[0-9]+$/ || $i<0 || $i>255) { bad=1; continue }
          if ($i==0) { argc++; argv[argc]=current; current=""; ended=1 }
          else {
            if ($i<32 || $i>126) bad=1
            current=current sprintf("%c",$i); ended=0
          }
        }
      }
      function base(v) { sub(/^.*\//,"",v); return v }
      END {
        if (bad || !ended || current!="" || argc<1) exit 1
        exe=base(argv[1])
        if (service=="S24broray")
          ok=(argc==4 && exe=="xray" && argv[2]=="run" && argv[3]=="-c" && argv[4]=="/opt/broray/config/config.json")
        else if (service=="S25broray-web")
          ok=(argc==3 && exe=="broray-lighttpd" && argv[1]=="/opt/broray/runtime/broray-lighttpd" && argv[2]=="-f" && argv[3]=="/opt/broray/config/lighttpd.conf")
        else if (service=="S23broray-monitor")
          ok=(argc==2 && (exe=="sh" || exe=="ash") && argv[2]=="/opt/broray/bin/broray-connection-monitor")
        else if (service=="S27broray-auto-switch")
          ok=(argc==2 && exe=="ash" && argv[2]=="/opt/broray/bin/broray-server-auto-switch")
        else if (service=="S28broray-subscriptions")
          ok=(argc==2 && exe=="ash" && argv[2]=="/opt/broray/bin/broray-subscription-scheduler")
        exit ok ? 0 : 1
      }
    '
}

broray_tx_service_identity_measure()
{
    broray_tx_service_identity_name="$1"
    broray_tx_service_identity_matches="$BRORAY_TX_WORK/service-identity-$broray_tx_service_identity_name.matches.$$"
    : >"$broray_tx_service_identity_matches" || return 1
    for broray_tx_service_identity_proc in "$BRORAY_TX_PROC_ROOT"/[0-9]*; do
        [ -d "$broray_tx_service_identity_proc" ] && [ ! -L "$broray_tx_service_identity_proc" ] || continue
        broray_tx_service_identity_pid="${broray_tx_service_identity_proc##*/}"
        case "$broray_tx_service_identity_pid" in ''|*[!0-9]*|0) continue ;; esac
        if broray_tx_service_cmdline_match "$broray_tx_service_identity_pid" "$broray_tx_service_identity_name"; then
            printf '%s\n' "$broray_tx_service_identity_pid" >>"$broray_tx_service_identity_matches" || return 1
        fi
    done
    broray_tx_service_identity_count="$(wc -l <"$broray_tx_service_identity_matches" | tr -d ' ')"
    case "$broray_tx_service_identity_count" in 0|1) ;; *) rm -f "$broray_tx_service_identity_matches"; return 1 ;; esac
    broray_tx_service_identity_pidfile="$(broray_tx_service_pid_file "$broray_tx_service_identity_name")" || return 1
    broray_tx_service_identity_declared=""
    if [ -n "$broray_tx_service_identity_pidfile" ] &&
       { [ -e "$broray_tx_service_identity_pidfile" ] || [ -L "$broray_tx_service_identity_pidfile" ]; }
    then
        [ -f "$broray_tx_service_identity_pidfile" ] && [ ! -L "$broray_tx_service_identity_pidfile" ] || return 1
        broray_tx_service_identity_declared="$(sed -n '1p' "$broray_tx_service_identity_pidfile")"
        case "$broray_tx_service_identity_declared" in ''|*[!0-9]*|0) return 1 ;; esac
        [ "$(wc -l <"$broray_tx_service_identity_pidfile" | tr -d ' ')" -eq 1 ] || return 1
    fi
    if [ "$broray_tx_service_identity_count" -eq 0 ]; then
        [ -z "$broray_tx_service_identity_declared" ] || return 1
        BRORAY_TX_SERVICE_IDENTITY_STATE=stopped
        BRORAY_TX_SERVICE_IDENTITY_PID=""
        BRORAY_TX_SERVICE_IDENTITY_SHA256=""
    else
        BRORAY_TX_SERVICE_IDENTITY_PID="$(sed -n '1p' "$broray_tx_service_identity_matches")"
        [ -z "$broray_tx_service_identity_pidfile" ] ||
            [ "$broray_tx_service_identity_declared" = "$BRORAY_TX_SERVICE_IDENTITY_PID" ] || return 1
        if [ "${BRORAY_TX_TEST_MODE:-0}" != 1 ]; then
            kill -0 "$BRORAY_TX_SERVICE_IDENTITY_PID" 2>/dev/null || return 1
        fi
        broray_tx_service_cmdline_match "$BRORAY_TX_SERVICE_IDENTITY_PID" "$broray_tx_service_identity_name" || return 1
        BRORAY_TX_SERVICE_IDENTITY_SHA256="$(broray_tx_sha "$BRORAY_TX_PROC_ROOT/$BRORAY_TX_SERVICE_IDENTITY_PID/cmdline")" || return 1
        [ "${#BRORAY_TX_SERVICE_IDENTITY_SHA256}" -eq 64 ] || return 1
        BRORAY_TX_SERVICE_IDENTITY_STATE=running
    fi
    rm -f "$broray_tx_service_identity_matches"
}

broray_tx_native_opkg_lock_service_guard()
{
    if [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 0 ] &&
       [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ]; then
        return 0
    fi
    broray_tx_native_opkg_lock_assert "$1"
}

broray_tx_service_state_capture()
{
    broray_tx_service_capture_tsv="$1"
    broray_tx_service_capture_running="$2"
    : >"$broray_tx_service_capture_running" || return 1
    : >"$broray_tx_service_capture_tsv" || return 1
    broray_tx_service_capture_identity="${broray_tx_service_capture_tsv%.tsv}.identity.tsv"
    : >"$broray_tx_service_capture_identity" || return 1
    for broray_tx_service in "$BRORAY_TX_OPT_ROOT"/etc/init.d/S23broray-monitor "$BRORAY_TX_OPT_ROOT"/etc/init.d/S24broray "$BRORAY_TX_OPT_ROOT"/etc/init.d/S25broray-web "$BRORAY_TX_OPT_ROOT"/etc/init.d/S27broray-auto-switch "$BRORAY_TX_OPT_ROOT"/etc/init.d/S28broray-subscriptions; do
        broray_tx_service_name="${broray_tx_service##*/}"
        broray_tx_service_state=absent
        BRORAY_TX_SERVICE_IDENTITY_PID=""
        BRORAY_TX_SERVICE_IDENTITY_SHA256=""
        broray_tx_native_opkg_lock_service_guard "service-status-$broray_tx_service_name" || return 1
        if [ ! -e "$broray_tx_service" ] && [ ! -L "$broray_tx_service" ]; then
            : # Exact source state; target defaults are selected separately.
            if [ "${BRORAY_TX_TEST_MODE:-0}" != 1 ] || [ "${BRORAY_TX_TEST_SERVICE_IDENTITY:-0}" = 1 ]; then
                broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
                [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = stopped ] || return 1
            fi
        elif [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "${BRORAY_TX_TEST_SERVICE_IDENTITY:-0}" != 1 ]; then
            # Runtime service behavior has a dedicated fixture.  General
            # isolated package fixtures preserve a deterministic semantic
            # profile without starting host processes.
            broray_tx_service_state=stopped
            case "$broray_tx_service_name" in S24broray|S25broray-web) broray_tx_service_state=running ;; esac
        else
            [ -f "$broray_tx_service" ] && [ ! -L "$broray_tx_service" ] && [ -x "$broray_tx_service" ] || return 1
            "$broray_tx_service" status >/dev/null 2>&1
            broray_tx_service_status_rc=$?
            broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
            broray_tx_service_state="$BRORAY_TX_SERVICE_IDENTITY_STATE"
            case "$broray_tx_service_status_rc:$broray_tx_service_state" in 0:running|[1-9]*:stopped) ;; *) return 1 ;; esac
        fi
        printf '%s\t%s\n' "$broray_tx_service_name" "$broray_tx_service_state" >>"$broray_tx_service_capture_tsv" || return 1
        printf '%s\t%s\t%s\t%s\n' "$broray_tx_service_name" "$broray_tx_service_state" \
            "${BRORAY_TX_SERVICE_IDENTITY_PID:-}" "${BRORAY_TX_SERVICE_IDENTITY_SHA256:-}" \
            >>"$broray_tx_service_capture_identity" || return 1
        if [ "$broray_tx_service_state" = running ]; then
            printf '%s\n' "$broray_tx_service" >>"$broray_tx_service_capture_running" || return 1
        fi
    done
}

broray_tx_stop_services()
{
    broray_tx_native_opkg_lock_service_guard services-pre-stop-capture || return 1
    broray_tx_service_state_capture "$BRORAY_TX_WORK/services-before-mutation.tsv" \
        "$BRORAY_TX_WORK/services-running-before-mutation.list" || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/services-before.tsv" \
        "$BRORAY_TX_WORK/services-before-mutation.tsv" || broray_tx_fail service-state-drift-before-mutation || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/services-running-before.list" \
        "$BRORAY_TX_WORK/services-running-before-mutation.list" || broray_tx_fail running-service-set-drift-before-mutation || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/services-before.identity.tsv" \
        "$BRORAY_TX_WORK/services-before-mutation.identity.tsv" || broray_tx_fail service-argv-drift-before-mutation || return 1
    while IFS= read -r broray_tx_service; do
        [ -n "$broray_tx_service" ] || continue
        broray_tx_service_name="${broray_tx_service##*/}"
        if [ "${BRORAY_TX_TEST_MODE:-0}" != 1 ] || [ "${BRORAY_TX_TEST_SERVICE_IDENTITY:-0}" = 1 ]; then
            broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = running ] || return 1
            broray_tx_service_expected_identity="$(awk -F '\t' -v service="$broray_tx_service_name" '$1==service{print $3 "|" $4;exit}' "$BRORAY_TX_WORK/services-before.identity.tsv")"
            [ "$broray_tx_service_expected_identity" = "$BRORAY_TX_SERVICE_IDENTITY_PID|$BRORAY_TX_SERVICE_IDENTITY_SHA256" ] || return 1
            broray_tx_native_opkg_lock_service_guard "service-stop-$broray_tx_service_name" || return 1
            "$broray_tx_service" stop >/dev/null 2>&1 || return 1
            broray_tx_native_opkg_lock_service_guard "service-stopped-$broray_tx_service_name" || return 1
            broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = stopped ] || return 1
        fi
    done <"$BRORAY_TX_WORK/services-running-before.list"
    printf '%s\n' yes >"$BRORAY_TX_WORK/services.stopped" || return 1
}

broray_tx_service_state_restore()
{
    [ -f "$BRORAY_TX_WORK/services-before.tsv" ] && [ ! -L "$BRORAY_TX_WORK/services-before.tsv" ] || return 1
    while IFS="$(printf '\t')" read -r broray_tx_service_name broray_tx_service_state; do
        case "$broray_tx_service_name" in S23broray-monitor|S24broray|S25broray-web|S27broray-auto-switch|S28broray-subscriptions) ;; *) return 1 ;; esac
        case "$broray_tx_service_state" in running|stopped|absent) ;; *) return 1 ;; esac
        broray_tx_service="$BRORAY_TX_OPT_ROOT/etc/init.d/$broray_tx_service_name"
        if [ "$broray_tx_service_state" = absent ]; then
            [ ! -e "$broray_tx_service" ] && [ ! -L "$broray_tx_service" ] || return 1
            [ "${BRORAY_TX_TEST_MODE:-0}" != 1 ] || [ "${BRORAY_TX_TEST_SERVICE_IDENTITY:-0}" = 1 ] || continue
            broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = stopped ] || return 1
            continue
        fi
        [ -x "$broray_tx_service" ] || return 1
        [ "${BRORAY_TX_TEST_MODE:-0}" != 1 ] || [ "${BRORAY_TX_TEST_SERVICE_IDENTITY:-0}" = 1 ] || continue
        if [ "$broray_tx_service_state" = running ]; then
            broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = stopped ] || return 1
            broray_tx_native_opkg_lock_service_guard "service-start-$broray_tx_service_name" || return 1
            "$broray_tx_service" start >/dev/null 2>&1 || return 1
            broray_tx_native_opkg_lock_service_guard "service-post-start-$broray_tx_service_name" || return 1
            "$broray_tx_service" status >/dev/null 2>&1 || return 1
            broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = running ] || return 1
        else
            broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = stopped ] || return 1
            broray_tx_native_opkg_lock_service_guard "service-ensure-stopped-$broray_tx_service_name" || return 1
            "$broray_tx_service" stop >/dev/null 2>&1 || true
            "$broray_tx_service" status >/dev/null 2>&1 && return 1
            broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = stopped ] || return 1
        fi
    done <"$BRORAY_TX_WORK/services-before.tsv"
}

broray_tx_service_target_state()
{
    broray_tx_service_target_name="$1"
    broray_tx_service_target_source="$2"
    case "$broray_tx_service_target_source" in
        running|stopped) printf '%s\n' "$broray_tx_service_target_source" ;;
        absent)
            case "$broray_tx_service_target_name" in
                S24broray|S25broray-web) printf '%s\n' running ;;
                *) printf '%s\n' stopped ;;
            esac
            ;;
        *) return 1 ;;
    esac
}

broray_tx_service_state_postcheck()
{
    [ -f "$BRORAY_TX_WORK/services-before.tsv" ] && [ ! -L "$BRORAY_TX_WORK/services-before.tsv" ] || return 1
    [ "$(wc -l <"$BRORAY_TX_WORK/services-before.tsv" | tr -d ' ')" -eq 5 ] || return 1
    while IFS="$(printf '\t')" read -r broray_tx_service_name broray_tx_service_source_state; do
        case "$broray_tx_service_name" in S23broray-monitor|S24broray|S25broray-web|S27broray-auto-switch|S28broray-subscriptions) ;; *) return 1 ;; esac
        broray_tx_service_state="$(broray_tx_service_target_state "$broray_tx_service_name" "$broray_tx_service_source_state")" || return 1
        broray_tx_service="$BRORAY_TX_OPT_ROOT/etc/init.d/$broray_tx_service_name"
        [ -x "$broray_tx_service" ] || return 1
        [ "${BRORAY_TX_TEST_MODE:-0}" != 1 ] || [ "${BRORAY_TX_TEST_SERVICE_IDENTITY:-0}" = 1 ] || continue
        broray_tx_native_opkg_lock_service_guard "service-postcheck-$broray_tx_service_name" || return 1
        if [ "$broray_tx_service_state" = running ]; then
            "$broray_tx_service" status >/dev/null 2>&1 || return 1
            broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = running ] || return 1
        elif [ "$broray_tx_service_state" = stopped ]; then
            "$broray_tx_service" status >/dev/null 2>&1 && return 1
            broray_tx_service_identity_measure "$broray_tx_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = stopped ] || return 1
        else
            return 1
        fi
    done <"$BRORAY_TX_WORK/services-before.tsv"
}

broray_tx_remove_program_links()
{
    for broray_tx_link_name in broray broray-connection-monitor broray-log-maintenance broray-route-prepare broray-routes broray-routes-check-source broray-routes-dot broray-routes-user broray-server broray-server-auto-switch broray-server-probe broray-servers broray-subscription-scheduler broray-subscriptions broray-system broray-test broray-xray-start; do
        broray_tx_link="$BRORAY_TX_OPT_ROOT/bin/$broray_tx_link_name"
        [ -L "$broray_tx_link" ] || continue
        case "$(readlink "$broray_tx_link" 2>/dev/null)" in "$BRORAY_TX_APP_ROOT"/*|/opt/broray/*) rm -f "$broray_tx_link" || return 1 ;; esac
    done
}

broray_tx_program_delete_plan_build()
{
    [ -f "$BRORAY_TX_WORK/source.manifest" ] && [ ! -L "$BRORAY_TX_WORK/source.manifest" ] || return 1
    [ -f "$BRORAY_TX_WORK/protected-source.manifest" ] && [ ! -L "$BRORAY_TX_WORK/protected-source.manifest" ] || return 1
    broray_tx_program_app_rel="$(broray_tx_to_relative "$BRORAY_TX_APP_ROOT")" || return 1
    awk -F '|' -v app="$broray_tx_program_app_rel" '{print app "/" $2}' \
        "$BRORAY_TX_WORK/protected-source.manifest" >"$BRORAY_TX_WORK/protected-source.fs-paths" || return 1
    broray_tx_program_info_rel="${BRORAY_TX_INFO_ROOT#"$BRORAY_TX_PREFIX"/}"
    awk -F '|' -v info="$broray_tx_program_info_rel" '
      NR==FNR {protected[++n]=$0; next}
      {
        type=$1; path=$2
        if (path==info || index(path,info "/")==1) next
        keep=0
        for(i=1;i<=n;i++) if(path==protected[i] || index(protected[i],path "/")==1){keep=1;break}
        if(keep) next
        depth=gsub(/\//,"/",path)
        printf "%010d\t%s\t%s\n", depth, path, type
      }
    ' "$BRORAY_TX_WORK/protected-source.fs-paths" "$BRORAY_TX_WORK/source.manifest" \
        | LC_ALL=C sort -r \
        | cut -f2- | awk -F '\t' '{print $2 "|" $1}' \
        >"$BRORAY_TX_WORK/program-delete.plan.part" || return 1
    mv -f "$BRORAY_TX_WORK/program-delete.plan.part" "$BRORAY_TX_WORK/program-delete.plan" || return 1
    while IFS='|' read -r broray_tx_program_type broray_tx_program_rel; do
        [ -n "$broray_tx_program_rel" ] || continue
        broray_tx_relative_safe "$broray_tx_program_rel" || return 1
        case "$broray_tx_program_type" in F|D|L) ;; *) return 1 ;; esac
        broray_tx_program_abs="$(broray_tx_root_path "/$broray_tx_program_rel")" || return 1
        case "$broray_tx_program_type" in
            F) [ -f "$broray_tx_program_abs" ] && [ ! -L "$broray_tx_program_abs" ] || return 1 ;;
            D) [ -d "$broray_tx_program_abs" ] && [ ! -L "$broray_tx_program_abs" ] || return 1 ;;
            L) [ -L "$broray_tx_program_abs" ] || return 1 ;;
        esac
    done <"$BRORAY_TX_WORK/program-delete.plan"
    broray_tx_file_cap_kb "$BRORAY_TX_WORK/program-delete.plan" "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB"
}

broray_tx_source_registration_plan_build()
{
    : >"$BRORAY_TX_WORK/source-registration.plan" || return 1
    for broray_tx_registration_source in "$BRORAY_TX_INFO_ROOT"/broray.*; do
        [ -e "$broray_tx_registration_source" ] || [ -L "$broray_tx_registration_source" ] || continue
        [ -f "$broray_tx_registration_source" ] && [ ! -L "$broray_tx_registration_source" ] || return 1
        printf '%s\n' "$broray_tx_registration_source" >>"$BRORAY_TX_WORK/source-registration.plan" || return 1
    done
    LC_ALL=C sort -u "$BRORAY_TX_WORK/source-registration.plan" \
        >"$BRORAY_TX_WORK/source-registration.plan.sorted" || return 1
    mv -f "$BRORAY_TX_WORK/source-registration.plan.sorted" "$BRORAY_TX_WORK/source-registration.plan" || return 1
    [ -f "$BRORAY_TX_STATUS_FILE" ] && [ ! -L "$BRORAY_TX_STATUS_FILE" ] || return 1
    broray_tx_file_cap_kb "$BRORAY_TX_WORK/source-registration.plan" "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB"
}

broray_tx_source_registration_remove()
{
    [ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" -eq 1 ] || return 1
    broray_tx_foreign_status_guard || return 1
    broray_tx_native_opkg_lock_assert source-unregister-status-part-before || return 1
    broray_tx_status_without_broray_to "$BRORAY_TX_STATUS_FILE.part" || return 1
    broray_tx_native_opkg_lock_assert source-unregister-status-part-after || return 1
    broray_tx_native_opkg_lock_assert source-unregister-status-rename-before || return 1
    mv -f "$BRORAY_TX_STATUS_FILE.part" "$BRORAY_TX_STATUS_FILE" || return 1
    broray_tx_native_opkg_lock_assert source-unregister-status-rename-after || return 1
    while IFS= read -r broray_tx_registration_source; do
        [ -n "$broray_tx_registration_source" ] || continue
        case "$broray_tx_registration_source" in "$BRORAY_TX_INFO_ROOT"/broray.*) ;; *) return 1 ;; esac
        [ -f "$broray_tx_registration_source" ] && [ ! -L "$broray_tx_registration_source" ] || return 1
        broray_tx_native_opkg_lock_assert "source-unregister-info-before-${broray_tx_registration_source##*/}" || return 1
        rm -f "$broray_tx_registration_source" || return 1
        broray_tx_native_opkg_lock_assert "source-unregister-info-after-${broray_tx_registration_source##*/}" || return 1
    done <"$BRORAY_TX_WORK/source-registration.plan"
    broray_tx_status_stanza_broray >"$BRORAY_TX_WORK/source-status.after-unregister" || return 1
    [ ! -s "$BRORAY_TX_WORK/source-status.after-unregister" ] || return 1
    for broray_tx_registration_source in "$BRORAY_TX_INFO_ROOT"/broray.*; do
        [ ! -e "$broray_tx_registration_source" ] && [ ! -L "$broray_tx_registration_source" ] || return 1
    done
    broray_tx_foreign_status_guard || return 1
    broray_tx_event opkg-source-unregistered
}

broray_tx_clean_program_tree()
{
    [ -f "$BRORAY_TX_WORK/program-delete.plan" ] && [ ! -L "$BRORAY_TX_WORK/program-delete.plan" ] || return 1
    : >"$BRORAY_TX_WORK/program-delete.actions" || return 1
    while IFS='|' read -r broray_tx_program_type broray_tx_program_rel; do
        [ -n "$broray_tx_program_rel" ] || continue
        broray_tx_program_abs="$(broray_tx_root_path "/$broray_tx_program_rel")" || return 1
        case "$broray_tx_program_type" in
            F) [ -f "$broray_tx_program_abs" ] && [ ! -L "$broray_tx_program_abs" ] || return 1; rm -f "$broray_tx_program_abs" || return 1 ;;
            L) [ -L "$broray_tx_program_abs" ] || return 1; rm -f "$broray_tx_program_abs" || return 1 ;;
            D) [ -d "$broray_tx_program_abs" ] && [ ! -L "$broray_tx_program_abs" ] || return 1; rmdir "$broray_tx_program_abs" || return 1 ;;
            *) return 1 ;;
        esac
        printf '%s|%s\n' "$broray_tx_program_type" "$broray_tx_program_rel" >>"$BRORAY_TX_WORK/program-delete.actions" || return 1
    done <"$BRORAY_TX_WORK/program-delete.plan"
    broray_tx_files_equal "$BRORAY_TX_WORK/program-delete.plan" "$BRORAY_TX_WORK/program-delete.actions" || return 1
    while IFS='|' read -r broray_tx_program_type broray_tx_program_rel; do
        [ -n "$broray_tx_program_rel" ] || continue
        broray_tx_program_abs="$(broray_tx_root_path "/$broray_tx_program_rel")" || return 1
        [ ! -e "$broray_tx_program_abs" ] && [ ! -L "$broray_tx_program_abs" ] || return 1
    done <"$BRORAY_TX_WORK/program-delete.plan"
}

broray_tx_delete_candidate_tree()
{
    broray_tx_candidate_delete_manifest="$BRORAY_TX_WORK/candidate-check/control/payload-manifest.tsv"
    [ -f "$broray_tx_candidate_delete_manifest" ] && [ ! -L "$broray_tx_candidate_delete_manifest" ] || return 1
    broray_tx_candidate_app_rel="$(broray_tx_to_relative "$BRORAY_TX_APP_ROOT")" || return 1
    if [ -e "$BRORAY_TX_APP_ROOT" ] || [ -L "$BRORAY_TX_APP_ROOT" ]; then
        [ -d "$BRORAY_TX_APP_ROOT" ] && [ ! -L "$BRORAY_TX_APP_ROOT" ] || return 1
        rm -rf "$BRORAY_TX_APP_ROOT" || return 1
    fi
    awk -F '|' -v app="$broray_tx_candidate_app_rel" '
      $2!=app && index($2,app "/")!=1 {
        path=$2; depth=gsub(/\//,"/",path); printf "%010d\t%s\t%s\n", depth, $2, $1
      }
    ' "$broray_tx_candidate_delete_manifest" |
        LC_ALL=C sort -r |
        cut -f2- | while IFS="$(printf '\t')" read -r broray_tx_candidate_rel broray_tx_candidate_type; do
            [ -n "$broray_tx_candidate_rel" ] || continue
            broray_tx_relative_safe "$broray_tx_candidate_rel" || exit 1
            broray_tx_candidate_abs="$(broray_tx_root_path "/$broray_tx_candidate_rel")" || exit 1
            [ -e "$broray_tx_candidate_abs" ] || [ -L "$broray_tx_candidate_abs" ] || continue
            case "$broray_tx_candidate_type" in
                F) [ -f "$broray_tx_candidate_abs" ] && [ ! -L "$broray_tx_candidate_abs" ] || exit 1; rm -f "$broray_tx_candidate_abs" || exit 1 ;;
                L) [ -L "$broray_tx_candidate_abs" ] || exit 1; rm -f "$broray_tx_candidate_abs" || exit 1 ;;
                D)
                    [ -d "$broray_tx_candidate_abs" ] && [ ! -L "$broray_tx_candidate_abs" ] || exit 1
                    # data.tar contains shared container directories such as
                    # /opt and /opt/etc.  They are not exclusively owned by
                    # BROray: prune them only when empty after every exact
                    # candidate leaf has been removed.
                    rmdir "$broray_tx_candidate_abs" 2>/dev/null || {
                        [ -d "$broray_tx_candidate_abs" ] && [ ! -L "$broray_tx_candidate_abs" ] || exit 1
                    }
                    ;;
                *) exit 1 ;;
            esac
        done
    # A retained shared directory is valid; any exact candidate file or link
    # outside the already removed application root is not.
    awk -F '|' -v app="$broray_tx_candidate_app_rel" '
      ($1=="F" || $1=="L") && $2!=app && index($2,app "/")!=1 {print $2}
    ' "$broray_tx_candidate_delete_manifest" |
        while IFS= read -r broray_tx_candidate_rel; do
            [ -n "$broray_tx_candidate_rel" ] || continue
            broray_tx_relative_safe "$broray_tx_candidate_rel" || exit 1
            broray_tx_candidate_abs="$(broray_tx_root_path "/$broray_tx_candidate_rel")" || exit 1
            [ ! -e "$broray_tx_candidate_abs" ] && [ ! -L "$broray_tx_candidate_abs" ] || exit 1
        done
}

# A capsule-only recovery deliberately has no candidate.ipk, extracted
# control archive or candidate-check sidecar.  Candidate admission above
# proves that this release can write only these exclusive roots.  Delete that
# complete static set before restoring the capsule-bound arbitrary source.
# Shared /opt, /opt/etc and /opt/etc/init.d containers are never removed.
broray_tx_delete_candidate_tree_archive_only()
{
    [ "${BRORAY_TX_RECOVERY_SNAPSHOT_SOURCE:-}" = archive-capsule-only ] || return 1
    : >"$BRORAY_TX_WORK/evidence/archive-only-target-delete.actions" || return 1
    if [ -e "$BRORAY_TX_APP_ROOT" ] || [ -L "$BRORAY_TX_APP_ROOT" ]; then
        [ -d "$BRORAY_TX_APP_ROOT" ] && [ ! -L "$BRORAY_TX_APP_ROOT" ] || return 1
        printf 'D|%s\n' "$(broray_tx_to_relative "$BRORAY_TX_APP_ROOT")" \
            >>"$BRORAY_TX_WORK/evidence/archive-only-target-delete.actions" || return 1
        rm -rf "$BRORAY_TX_APP_ROOT" || return 1
    fi
    for broray_tx_candidate_service in \
        S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions
    do
        broray_tx_candidate_service_path="$BRORAY_TX_OPT_ROOT/etc/init.d/$broray_tx_candidate_service"
        [ -e "$broray_tx_candidate_service_path" ] || [ -L "$broray_tx_candidate_service_path" ] || continue
        if [ -L "$broray_tx_candidate_service_path" ]; then
            broray_tx_candidate_service_type=L
        elif [ -f "$broray_tx_candidate_service_path" ]; then
            broray_tx_candidate_service_type=F
        else
            return 1
        fi
        printf '%s|%s\n' "$broray_tx_candidate_service_type" \
            "$(broray_tx_to_relative "$broray_tx_candidate_service_path")" \
            >>"$BRORAY_TX_WORK/evidence/archive-only-target-delete.actions" || return 1
        rm -f "$broray_tx_candidate_service_path" || return 1
    done
    broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/archive-only-target-delete.actions" \
        "$BRORAY_TX_EVIDENCE_GROWTH_CAP_KB" || return 1
    [ ! -e "$BRORAY_TX_APP_ROOT" ] && [ ! -L "$BRORAY_TX_APP_ROOT" ] || return 1
}

broray_tx_install_safe_old_hooks()
{
    [ -d "$BRORAY_TX_INFO_ROOT" ] && [ ! -L "$BRORAY_TX_INFO_ROOT" ] || return 0
    for broray_tx_hook in prerm postrm; do
        [ -f "$BRORAY_TX_WORK/candidate-check/control/$broray_tx_hook" ] || return 1
        broray_tx_native_opkg_lock_assert "opkg-info-hook-part-before-$broray_tx_hook" || return 1
        cp -p "$BRORAY_TX_WORK/candidate-check/control/$broray_tx_hook" "$BRORAY_TX_INFO_ROOT/broray.$broray_tx_hook.part" || return 1
        broray_tx_native_opkg_lock_assert "opkg-info-hook-part-after-$broray_tx_hook" || return 1
        broray_tx_native_opkg_lock_assert "opkg-info-hook-rename-before-$broray_tx_hook" || return 1
        mv -f "$BRORAY_TX_INFO_ROOT/broray.$broray_tx_hook.part" "$BRORAY_TX_INFO_ROOT/broray.$broray_tx_hook" || return 1
        broray_tx_native_opkg_lock_assert "opkg-info-hook-rename-after-$broray_tx_hook" || return 1
        broray_tx_native_opkg_lock_assert "opkg-info-hook-mode-before-$broray_tx_hook" || return 1
        chmod 755 "$BRORAY_TX_INFO_ROOT/broray.$broray_tx_hook" || return 1
        broray_tx_native_opkg_lock_assert "opkg-info-hook-mode-after-$broray_tx_hook" || return 1
    done
}

broray_tx_mount_graph_recheck()
{
    broray_tx_mount_opt_file="$BRORAY_TX_WORK/mount-opt.before-mutation"
    broray_tx_mount_tmp_file="$BRORAY_TX_WORK/mount-tmp.before-mutation"
    broray_tx_mount_opt_expected="$(jq -r '.mountGraph.opt.identity // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    broray_tx_mount_tmp_expected="$(jq -r '.mountGraph.tmp.identity // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    broray_tx_mount_same_expected="$(jq -r 'if (.mountGraph.sameBackingFs|type)=="boolean" then (.mountGraph.sameBackingFs|tostring) else empty end' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    case "$broray_tx_mount_same_expected" in true|false) ;; *) return 1 ;; esac
    [ -n "$broray_tx_mount_opt_expected" ] && [ -n "$broray_tx_mount_tmp_expected" ] || return 1
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ]; then
        printf '%s\n' "$broray_tx_mount_opt_expected" >"$broray_tx_mount_opt_file" || return 1
        printf '%s\n' "$broray_tx_mount_tmp_expected" >"$broray_tx_mount_tmp_file" || return 1
    else
        broray_runtime_mount_identity "$BRORAY_TX_OPT_ROOT" "$broray_tx_mount_opt_file" || return 1
        broray_runtime_mount_identity "$BRORAY_TX_TMP_BASE" "$broray_tx_mount_tmp_file" || return 1
    fi
    broray_tx_mount_opt_now="$(cat "$broray_tx_mount_opt_file" 2>/dev/null)"
    broray_tx_mount_tmp_now="$(cat "$broray_tx_mount_tmp_file" 2>/dev/null)"
    [ "${BRORAY_TX_TEST_FORCE_MOUNT_DRIFT:-0}" != 1 ] &&
        [ -n "$broray_tx_mount_opt_expected" ] && [ -n "$broray_tx_mount_tmp_expected" ] &&
        [ "$broray_tx_mount_opt_now" = "$broray_tx_mount_opt_expected" ] &&
        [ "$broray_tx_mount_tmp_now" = "$broray_tx_mount_tmp_expected" ] || return 1
    broray_tx_mount_opt_dev="$(printf '%s\n' "$broray_tx_mount_opt_now" | awk -F '|' 'NF==6{print $2}')"
    broray_tx_mount_tmp_dev="$(printf '%s\n' "$broray_tx_mount_tmp_now" | awk -F '|' 'NF==6{print $2}')"
    [ -n "$broray_tx_mount_opt_dev" ] && [ -n "$broray_tx_mount_tmp_dev" ] || return 1
    if [ "$broray_tx_mount_same_expected" = true ]; then
        [ "$broray_tx_mount_opt_dev" = "$broray_tx_mount_tmp_dev" ] || return 1
    else
        [ "$broray_tx_mount_opt_dev" != "$broray_tx_mount_tmp_dev" ] || return 1
    fi

    # Re-probe the physical allocation units after identity comparison.  The
    # files are removed before df so their temporary blocks cannot affect the
    # admission sample.
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ]; then
        broray_tx_mount_opt_unit_now="$(jq -r '.mountGraph.opt.allocationUnitKB // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
        broray_tx_mount_tmp_unit_now="$(jq -r '.mountGraph.tmp.allocationUnitKB // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    else
        broray_tx_mount_opt_probe="$BRORAY_TX_OPT_ROOT/.broray-space-recheck.$$"
        broray_tx_mount_tmp_probe="$BRORAY_TX_TMP_BASE/.broray-space-recheck.$$"
        printf x >"$broray_tx_mount_opt_probe" 2>/dev/null && printf x >"$broray_tx_mount_tmp_probe" 2>/dev/null || return 1
        broray_tx_mount_opt_unit_now="$(du -sk "$broray_tx_mount_opt_probe" 2>/dev/null | awk 'NR==1{print $1;exit}')"
        broray_tx_mount_tmp_unit_now="$(du -sk "$broray_tx_mount_tmp_probe" 2>/dev/null | awk 'NR==1{print $1;exit}')"
        rm -f "$broray_tx_mount_opt_probe" "$broray_tx_mount_tmp_probe" || return 1
    fi
    broray_tx_mount_opt_unit_expected="$(jq -r '.mountGraph.opt.allocationUnitKB // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    broray_tx_mount_tmp_unit_expected="$(jq -r '.mountGraph.tmp.allocationUnitKB // empty' "$BRORAY_TX_WORK/evidence/capabilities.json")"
    broray_tx_number "$broray_tx_mount_opt_unit_now" && broray_tx_number "$broray_tx_mount_tmp_unit_now" &&
        [ "$broray_tx_mount_opt_unit_now" = "$broray_tx_mount_opt_unit_expected" ] &&
        [ "$broray_tx_mount_tmp_unit_now" = "$broray_tx_mount_tmp_unit_expected" ] || return 1
    [ "$broray_tx_mount_same_expected" = false ] ||
        [ "$broray_tx_mount_opt_unit_now" = "$broray_tx_mount_tmp_unit_now" ]
}

broray_tx_pre_mutation_space_gate()
{
    broray_tx_mount_graph_recheck || broray_tx_fail mount-or-allocation-identity-drift-before-mutation || return 1
    broray_tx_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/source-scope.list" \
        "$BRORAY_TX_WORK/source.pre-mutation.manifest" || broray_tx_fail source-allocation-recheck-failed || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/source.manifest" "$BRORAY_TX_WORK/source.pre-mutation.manifest" ||
        broray_tx_fail source-manifest-drift-before-mutation || return 1
    broray_tx_allocation_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/source.pre-mutation.manifest" \
        "$BRORAY_TX_WORK/source.pre-mutation.allocation.manifest" || broray_tx_fail source-allocation-recheck-failed || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/source.allocation.manifest" \
        "$BRORAY_TX_WORK/source.pre-mutation.allocation.manifest" ||
        broray_tx_fail source-allocation-drift-before-mutation || return 1
    broray_tx_allocation_reclaimable_metrics "$BRORAY_TX_WORK/source.pre-mutation.allocation.manifest" ||
        broray_tx_fail source-allocation-topology-ambiguous-before-mutation || return 1
    [ "$BRORAY_TX_ALLOCATION_EXTERNAL_HARDLINK_GROUPS" -eq 0 ] ||
        broray_tx_fail source-external-hardlink-topology-unsupported || return 1
    broray_tx_allocation_topology_manifest "$BRORAY_TX_WORK/source.pre-mutation.allocation.manifest" \
        "$BRORAY_TX_WORK/source.pre-mutation.hardlink-topology" ||
        broray_tx_fail source-allocation-topology-ambiguous-before-mutation || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/source.hardlink-topology" \
        "$BRORAY_TX_WORK/source.pre-mutation.hardlink-topology" ||
        broray_tx_fail source-hardlink-topology-drift-before-mutation || return 1
    broray_tx_foreign_status_guard || broray_tx_fail foreign-opkg-drift-before-space-barrier || return 1
    broray_tx_program_delete_plan_build || broray_tx_fail factual-program-delete-plan-invalid || return 1
    broray_tx_source_registration_plan_build || broray_tx_fail source-registration-delete-plan-invalid || return 1
    broray_tx_opt_free_now="${BRORAY_TX_TEST_OPT_FREE_BEFORE_MUTATION_KB:-$(df -Pk "$BRORAY_TX_OPT_ROOT" 2>/dev/null | awk 'NR==2{print $4;exit}')}"
    broray_tx_tmp_free_now="${BRORAY_TX_TEST_TMP_FREE_BEFORE_MUTATION_KB:-$(df -Pk "$BRORAY_TX_TMP_BASE" 2>/dev/null | awk 'NR==2{print $4;exit}')}"
    broray_tx_opt_inodes_now="${BRORAY_TX_TEST_OPT_FREE_BEFORE_MUTATION_INODES:-$(broray_tx_df_inodes "$BRORAY_TX_OPT_ROOT")}"
    broray_tx_tmp_inodes_now="${BRORAY_TX_TEST_TMP_FREE_BEFORE_MUTATION_INODES:-$(broray_tx_df_inodes "$BRORAY_TX_TMP_BASE")}"
    broray_tx_number "$broray_tx_opt_free_now" && broray_tx_number "$broray_tx_tmp_free_now" &&
        broray_tx_number "$broray_tx_opt_inodes_now" && broray_tx_number "$broray_tx_tmp_inodes_now" || return 1
    broray_tx_same_fs="$(jq -r 'if (.sameBackingFs|type)=="boolean" then (.sameBackingFs|tostring) else empty end' "$BRORAY_TX_WORK/evidence/space.json")"
    case "$broray_tx_same_fs" in true|false) ;; *) return 1 ;; esac
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_opt_free_now" = "$broray_tx_tmp_free_now" ] &&
            [ "$broray_tx_opt_inodes_now" = "$broray_tx_tmp_inodes_now" ] ||
            broray_tx_fail same-backing-fs-samples-disagree-before-mutation || return 1
    fi
    broray_tx_source_now_kb="$BRORAY_TX_ALLOCATION_RECLAIMABLE_KB"
    broray_tx_source_reclaimable_inodes_now="$BRORAY_TX_ALLOCATION_RECLAIMABLE_INODES"
    broray_tx_opt_forward_now="$(jq -r '.optForwardPeakKB' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_opt_rollback_now="$(jq -r '.optRollbackPeakKB' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_number "$broray_tx_source_now_kb" && broray_tx_number "$broray_tx_opt_forward_now" &&
        broray_tx_number "$broray_tx_opt_rollback_now" || return 1
    broray_tx_opt_delta_now="$(broray_tx_sub0 "$broray_tx_opt_forward_now" "$broray_tx_source_now_kb")" || return 1
    broray_tx_opt_rollback_delta_now="$(broray_tx_sub0 "$broray_tx_opt_rollback_now" "$broray_tx_source_now_kb")" || return 1
    broray_tx_opt_peak_delta_now="$(broray_tx_umax "$broray_tx_opt_delta_now" "$broray_tx_opt_rollback_delta_now")" || return 1
    broray_tx_opt_required_now="$(broray_tx_uadd "$BRORAY_TX_OPT_RESERVE_KB" "$broray_tx_opt_peak_delta_now")" || return 1
    broray_tx_opt_required_now_mib="$(broray_tx_ceil_div "$broray_tx_opt_required_now" 1024)" || return 1
    broray_tx_opt_required_now="$(broray_tx_umul "$broray_tx_opt_required_now_mib" 1024)" || return 1
    broray_tx_post_candidate_writer_upper_now="$(jq -r '.postCandidateWriterUpperKB' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_number "$broray_tx_post_candidate_writer_upper_now" || return 1
    broray_tx_tmp_required_now="$(broray_tx_uadd "$BRORAY_TX_TMP_RESERVE_KB" "$broray_tx_post_candidate_writer_upper_now")" || return 1
    broray_tx_protected_objects_now="$(wc -l <"$BRORAY_TX_WORK/protected-source.manifest" | tr -d ' ')"
    broray_tx_opt_forward_inodes_now="$(jq -r '.optForwardPeakInodes' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_opt_rollback_inodes_now="$(jq -r '.optRollbackPeakInodes' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_number "$broray_tx_protected_objects_now" && broray_tx_number "$broray_tx_opt_forward_inodes_now" &&
        broray_tx_number "$broray_tx_opt_rollback_inodes_now" || return 1
    broray_tx_opt_forward_inode_delta_now="$(broray_tx_sub0 "$broray_tx_opt_forward_inodes_now" "$broray_tx_source_reclaimable_inodes_now")" || return 1
    broray_tx_opt_rollback_inode_delta_now="$(broray_tx_sub0 "$broray_tx_opt_rollback_inodes_now" "$broray_tx_source_reclaimable_inodes_now")" || return 1
    broray_tx_opt_inode_peak_delta_now="$(broray_tx_umax "$broray_tx_opt_forward_inode_delta_now" "$broray_tx_opt_rollback_inode_delta_now")" || return 1
    broray_tx_opt_inodes_required_now="$(broray_tx_uadd "$BRORAY_TX_OPT_INODE_RESERVE" "$broray_tx_opt_inode_peak_delta_now")" || return 1
    broray_tx_tmp_inodes_required_now="$(broray_tx_uadd "$BRORAY_TX_TMP_INODE_RESERVE" "$BRORAY_TX_POST_CANDIDATE_FUTURE_INODES")" || return 1
    broray_tx_shared_required_before_mutation=0
    broray_tx_shared_inodes_required_before_mutation=0
    broray_tx_shared_margin_before_mutation=0
    broray_tx_shared_inode_margin_before_mutation=0
    if [ "$broray_tx_same_fs" = true ]; then
        broray_tx_shared_phase_delta_before_mutation="$(broray_tx_uadd "$broray_tx_opt_peak_delta_now" "$broray_tx_post_candidate_writer_upper_now")" || return 1
        broray_tx_shared_required_before_mutation="$(broray_tx_uadd "$BRORAY_TX_OPT_RESERVE_KB" "$BRORAY_TX_TMP_RESERVE_KB")" || return 1
        broray_tx_shared_required_before_mutation="$(broray_tx_uadd "$broray_tx_shared_required_before_mutation" "$broray_tx_shared_phase_delta_before_mutation")" || return 1
        broray_tx_shared_phase_inode_delta_before_mutation="$(broray_tx_uadd "$broray_tx_opt_inode_peak_delta_now" "$BRORAY_TX_POST_CANDIDATE_FUTURE_INODES")" || return 1
        broray_tx_shared_inodes_required_before_mutation="$(broray_tx_uadd "$BRORAY_TX_OPT_INODE_RESERVE" "$BRORAY_TX_TMP_INODE_RESERVE")" || return 1
        broray_tx_shared_inodes_required_before_mutation="$(broray_tx_uadd "$broray_tx_shared_inodes_required_before_mutation" "$broray_tx_shared_phase_inode_delta_before_mutation")" || return 1
        broray_tx_shared_margin_before_mutation=$((broray_tx_opt_free_now - broray_tx_shared_required_before_mutation))
        broray_tx_shared_inode_margin_before_mutation=$((broray_tx_opt_inodes_now - broray_tx_shared_inodes_required_before_mutation))
    else
        broray_tx_shared_phase_delta_before_mutation=0
        broray_tx_shared_phase_inode_delta_before_mutation=0
    fi
    jq --argjson optFreeBeforeMutationKB "$broray_tx_opt_free_now" \
       --argjson tmpFreeBeforeMutationKB "$broray_tx_tmp_free_now" \
       --argjson optRequiredBeforeMutationKB "$broray_tx_opt_required_now" \
       --argjson tmpRequiredBeforeMutationKB "$broray_tx_tmp_required_now" --argjson optFreeInodesBeforeMutation "$broray_tx_opt_inodes_now" \
       --argjson optForwardDeltaBeforeMutationKB "$broray_tx_opt_delta_now" --argjson optRollbackDeltaBeforeMutationKB "$broray_tx_opt_rollback_delta_now" \
       --argjson sourceReclaimableInodesBeforeMutation "$broray_tx_source_reclaimable_inodes_now" \
       --argjson protectedSourceObjectsBeforeMutation "$broray_tx_protected_objects_now" \
       --argjson optForwardInodeDeltaBeforeMutation "$broray_tx_opt_forward_inode_delta_now" \
       --argjson optRollbackInodeDeltaBeforeMutation "$broray_tx_opt_rollback_inode_delta_now" \
       --argjson tmpFreeInodesBeforeMutation "$broray_tx_tmp_inodes_now" --argjson optRequiredInodesBeforeMutation "$broray_tx_opt_inodes_required_now" \
       --argjson tmpRequiredInodesBeforeMutation "$broray_tx_tmp_inodes_required_now" \
       --argjson sameBackingFs "$broray_tx_same_fs" \
       --argjson sharedFreeBeforeMutationKB "$broray_tx_opt_free_now" \
       --argjson sharedFreeInodesBeforeMutation "$broray_tx_opt_inodes_now" \
       --argjson sharedRequiredBeforeMutationKB "$broray_tx_shared_required_before_mutation" \
       --argjson sharedRequiredInodesBeforeMutation "$broray_tx_shared_inodes_required_before_mutation" \
       --argjson sharedSpaceMarginBeforeMutationKB "$broray_tx_shared_margin_before_mutation" \
       --argjson sharedInodeMarginBeforeMutation "$broray_tx_shared_inode_margin_before_mutation" \
       --argjson sharedPhaseDeltaBeforeMutationKB "$broray_tx_shared_phase_delta_before_mutation" \
       --argjson sharedPhaseDeltaInodesBeforeMutation "$broray_tx_shared_phase_inode_delta_before_mutation" \
       '.optFreeBeforeMutationKB=$optFreeBeforeMutationKB|
        .tmpFreeBeforeMutationKB=$tmpFreeBeforeMutationKB|
        .optRequiredBeforeMutationKB=$optRequiredBeforeMutationKB|
        .tmpRequiredBeforeMutationKB=$tmpRequiredBeforeMutationKB|
        .optForwardDeltaBeforeMutationKB=$optForwardDeltaBeforeMutationKB|
        .optRollbackDeltaBeforeMutationKB=$optRollbackDeltaBeforeMutationKB|
        .sourceReclaimableInodesBeforeMutation=$sourceReclaimableInodesBeforeMutation|
        .protectedSourceObjectsBeforeMutation=$protectedSourceObjectsBeforeMutation|
        .optForwardInodeDeltaBeforeMutation=$optForwardInodeDeltaBeforeMutation|
        .optRollbackInodeDeltaBeforeMutation=$optRollbackInodeDeltaBeforeMutation|
        .optFreeInodesBeforeMutation=$optFreeInodesBeforeMutation|.tmpFreeInodesBeforeMutation=$tmpFreeInodesBeforeMutation|
        .optRequiredInodesBeforeMutation=$optRequiredInodesBeforeMutation|.tmpRequiredInodesBeforeMutation=$tmpRequiredInodesBeforeMutation|
        .sharedFreeBeforeMutationKB=(if $sameBackingFs then $sharedFreeBeforeMutationKB else null end)|
        .sharedFreeInodesBeforeMutation=(if $sameBackingFs then $sharedFreeInodesBeforeMutation else null end)|
        .sharedRequiredBeforeMutationKB=(if $sameBackingFs then $sharedRequiredBeforeMutationKB else null end)|
        .sharedRequiredInodesBeforeMutation=(if $sameBackingFs then $sharedRequiredInodesBeforeMutation else null end)|
        .sharedSpaceMarginBeforeMutationKB=(if $sameBackingFs then $sharedSpaceMarginBeforeMutationKB else null end)|
        .sharedInodeMarginBeforeMutation=(if $sameBackingFs then $sharedInodeMarginBeforeMutation else null end)|
        .sharedPhaseDeltaBeforeMutationKB=(if $sameBackingFs then $sharedPhaseDeltaBeforeMutationKB else null end)|
        .sharedPhaseDeltaInodesBeforeMutation=(if $sameBackingFs then $sharedPhaseDeltaInodesBeforeMutation else null end)' \
       "$BRORAY_TX_WORK/evidence/space.json" >"$BRORAY_TX_WORK/evidence/space.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/evidence/space.json.part" "$BRORAY_TX_WORK/evidence/space.json" || return 1
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_opt_free_now" -ge "$broray_tx_shared_required_before_mutation" ] || broray_tx_fail insufficient-shared-space-before-mutation || return 1
        [ "$broray_tx_opt_inodes_now" -ge "$broray_tx_shared_inodes_required_before_mutation" ] || broray_tx_fail insufficient-shared-inodes-before-mutation || return 1
    else
        [ "$broray_tx_opt_free_now" -ge "$broray_tx_opt_required_now" ] || broray_tx_fail insufficient-opt-space-before-mutation || return 1
        [ "$broray_tx_tmp_free_now" -ge "$broray_tx_tmp_required_now" ] || broray_tx_fail insufficient-tmp-space-before-mutation || return 1
        [ "$broray_tx_opt_inodes_now" -ge "$broray_tx_opt_inodes_required_now" ] || broray_tx_fail insufficient-opt-inodes-before-mutation || return 1
        [ "$broray_tx_tmp_inodes_now" -ge "$broray_tx_tmp_inodes_required_now" ] || broray_tx_fail insufficient-tmp-inodes-before-mutation || return 1
    fi
}

broray_tx_post_delete_space_gate()
{
    broray_tx_post_delete_free="${BRORAY_TX_TEST_OPT_FREE_AFTER_SOURCE_DELETE_KB:-$(df -Pk "$BRORAY_TX_OPT_ROOT" 2>/dev/null | awk 'NR==2{print $4;exit}')}"
    broray_tx_post_delete_inodes="${BRORAY_TX_TEST_OPT_FREE_AFTER_SOURCE_DELETE_INODES:-$(broray_tx_df_inodes "$BRORAY_TX_OPT_ROOT")}"
    broray_tx_post_delete_tmp_free="${BRORAY_TX_TEST_TMP_FREE_AFTER_SOURCE_DELETE_KB:-$(df -Pk "$BRORAY_TX_TMP_BASE" 2>/dev/null | awk 'NR==2{print $4;exit}')}"
    broray_tx_post_delete_tmp_inodes="${BRORAY_TX_TEST_TMP_FREE_AFTER_SOURCE_DELETE_INODES:-$(broray_tx_df_inodes "$BRORAY_TX_TMP_BASE")}"
    broray_tx_post_delete_forward="$(jq -r '.optForwardPeakKB' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_post_delete_rollback="$(jq -r '.optRollbackPeakKB' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_post_delete_forward_inodes="$(jq -r '.optForwardPeakInodes' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_post_delete_rollback_inodes="$(jq -r '.optRollbackPeakInodes' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_same_fs="$(jq -r 'if (.sameBackingFs|type)=="boolean" then (.sameBackingFs|tostring) else empty end' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_post_candidate_writer_upper="$(jq -r '.postCandidateWriterUpperKB' "$BRORAY_TX_WORK/evidence/space.json")"
    case "$broray_tx_same_fs" in true|false) ;; *) return 1 ;; esac
    broray_tx_number "$broray_tx_post_delete_free" && broray_tx_number "$broray_tx_post_delete_inodes" &&
        broray_tx_number "$broray_tx_post_delete_tmp_free" && broray_tx_number "$broray_tx_post_delete_tmp_inodes" &&
        broray_tx_number "$broray_tx_post_delete_forward" && broray_tx_number "$broray_tx_post_delete_rollback" &&
        broray_tx_number "$broray_tx_post_delete_forward_inodes" && broray_tx_number "$broray_tx_post_delete_rollback_inodes" &&
        broray_tx_number "$broray_tx_post_candidate_writer_upper" || return 1
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_post_delete_free" = "$broray_tx_post_delete_tmp_free" ] &&
            [ "$broray_tx_post_delete_inodes" = "$broray_tx_post_delete_tmp_inodes" ] ||
            broray_tx_fail same-backing-fs-samples-disagree-after-source-delete || return 1
    fi
    broray_tx_post_delete_peak="$(broray_tx_umax "$broray_tx_post_delete_forward" "$broray_tx_post_delete_rollback")" || return 1
    broray_tx_post_delete_required="$(broray_tx_uadd "$BRORAY_TX_OPT_RESERVE_KB" "$broray_tx_post_delete_peak")" || return 1
    broray_tx_post_delete_required_mib="$(broray_tx_ceil_div "$broray_tx_post_delete_required" 1024)" || return 1
    broray_tx_post_delete_required="$(broray_tx_umul "$broray_tx_post_delete_required_mib" 1024)" || return 1
    broray_tx_post_delete_inode_peak="$(broray_tx_umax "$broray_tx_post_delete_forward_inodes" "$broray_tx_post_delete_rollback_inodes")" || return 1
    broray_tx_post_delete_inode_required="$(broray_tx_uadd "$BRORAY_TX_OPT_INODE_RESERVE" "$broray_tx_post_delete_inode_peak")" || return 1
    broray_tx_shared_required_after_delete=0
    broray_tx_shared_inode_required_after_delete=0
    broray_tx_shared_margin_after_delete=0
    broray_tx_shared_inode_margin_after_delete=0
    if [ "$broray_tx_same_fs" = true ]; then
        broray_tx_shared_phase_delta_after_delete="$(broray_tx_uadd "$broray_tx_post_delete_peak" "$broray_tx_post_candidate_writer_upper")" || return 1
        broray_tx_shared_required_after_delete="$(broray_tx_uadd "$BRORAY_TX_OPT_RESERVE_KB" "$BRORAY_TX_TMP_RESERVE_KB")" || return 1
        broray_tx_shared_required_after_delete="$(broray_tx_uadd "$broray_tx_shared_required_after_delete" "$broray_tx_shared_phase_delta_after_delete")" || return 1
        broray_tx_shared_phase_inode_delta_after_delete="$(broray_tx_uadd "$broray_tx_post_delete_inode_peak" "$BRORAY_TX_POST_CANDIDATE_FUTURE_INODES")" || return 1
        broray_tx_shared_inode_required_after_delete="$(broray_tx_uadd "$BRORAY_TX_OPT_INODE_RESERVE" "$BRORAY_TX_TMP_INODE_RESERVE")" || return 1
        broray_tx_shared_inode_required_after_delete="$(broray_tx_uadd "$broray_tx_shared_inode_required_after_delete" "$broray_tx_shared_phase_inode_delta_after_delete")" || return 1
        broray_tx_shared_margin_after_delete=$((broray_tx_post_delete_free - broray_tx_shared_required_after_delete))
        broray_tx_shared_inode_margin_after_delete=$((broray_tx_post_delete_inodes - broray_tx_shared_inode_required_after_delete))
    else
        broray_tx_shared_phase_delta_after_delete=0
        broray_tx_shared_phase_inode_delta_after_delete=0
    fi
    jq --argjson optFreeAfterSourceDeleteKB "$broray_tx_post_delete_free" \
       --argjson optRequiredAfterSourceDeleteKB "$broray_tx_post_delete_required" \
       --argjson optFreeInodesAfterSourceDelete "$broray_tx_post_delete_inodes" \
       --argjson optRequiredInodesAfterSourceDelete "$broray_tx_post_delete_inode_required" \
       --argjson sameBackingFs "$broray_tx_same_fs" \
       --argjson sharedFreeAfterSourceDeleteKB "$broray_tx_post_delete_free" \
       --argjson sharedFreeInodesAfterSourceDelete "$broray_tx_post_delete_inodes" \
       --argjson sharedRequiredAfterSourceDeleteKB "$broray_tx_shared_required_after_delete" \
       --argjson sharedRequiredInodesAfterSourceDelete "$broray_tx_shared_inode_required_after_delete" \
       --argjson sharedSpaceMarginAfterSourceDeleteKB "$broray_tx_shared_margin_after_delete" \
       --argjson sharedInodeMarginAfterSourceDelete "$broray_tx_shared_inode_margin_after_delete" \
       --argjson sharedPhaseDeltaAfterSourceDeleteKB "$broray_tx_shared_phase_delta_after_delete" \
       --argjson sharedPhaseDeltaInodesAfterSourceDelete "$broray_tx_shared_phase_inode_delta_after_delete" \
       '.optFreeAfterSourceDeleteKB=$optFreeAfterSourceDeleteKB|.optRequiredAfterSourceDeleteKB=$optRequiredAfterSourceDeleteKB|
        .optFreeInodesAfterSourceDelete=$optFreeInodesAfterSourceDelete|.optRequiredInodesAfterSourceDelete=$optRequiredInodesAfterSourceDelete|
        .sharedFreeAfterSourceDeleteKB=(if $sameBackingFs then $sharedFreeAfterSourceDeleteKB else null end)|
        .sharedFreeInodesAfterSourceDelete=(if $sameBackingFs then $sharedFreeInodesAfterSourceDelete else null end)|
        .sharedRequiredAfterSourceDeleteKB=(if $sameBackingFs then $sharedRequiredAfterSourceDeleteKB else null end)|
        .sharedRequiredInodesAfterSourceDelete=(if $sameBackingFs then $sharedRequiredInodesAfterSourceDelete else null end)|
        .sharedSpaceMarginAfterSourceDeleteKB=(if $sameBackingFs then $sharedSpaceMarginAfterSourceDeleteKB else null end)|
        .sharedInodeMarginAfterSourceDelete=(if $sameBackingFs then $sharedInodeMarginAfterSourceDelete else null end)|
        .sharedPhaseDeltaAfterSourceDeleteKB=(if $sameBackingFs then $sharedPhaseDeltaAfterSourceDeleteKB else null end)|
        .sharedPhaseDeltaInodesAfterSourceDelete=(if $sameBackingFs then $sharedPhaseDeltaInodesAfterSourceDelete else null end)' \
       "$BRORAY_TX_WORK/evidence/space.json" >"$BRORAY_TX_WORK/evidence/space.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/evidence/space.json.part" "$BRORAY_TX_WORK/evidence/space.json" || return 1
    if [ "$broray_tx_same_fs" = true ]; then
        [ "$broray_tx_post_delete_free" -ge "$broray_tx_shared_required_after_delete" ] || broray_tx_fail insufficient-shared-space-after-source-delete || return 1
        [ "$broray_tx_post_delete_inodes" -ge "$broray_tx_shared_inode_required_after_delete" ] || broray_tx_fail insufficient-shared-inodes-after-source-delete || return 1
    else
        [ "$broray_tx_post_delete_free" -ge "$broray_tx_post_delete_required" ] || broray_tx_fail insufficient-opt-space-after-source-delete || return 1
        [ "$broray_tx_post_delete_inodes" -ge "$broray_tx_post_delete_inode_required" ] || broray_tx_fail insufficient-opt-inodes-after-source-delete || return 1
    fi
}

broray_tx_clean_replace()
{
    broray_tx_event clean-replacement || return 1
    [ -f "$BRORAY_TX_WORK/snapshot.verified" ] && [ -f "$BRORAY_TX_WORK/candidate-engine.sha256" ] || broray_tx_fail mutation-before-verification || return 1
    broray_tx_user_validate "$BRORAY_TX_APP_ROOT" protected-barrier || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/protected-source-roots.list" "$BRORAY_TX_WORK/protected-barrier-roots.list" || broray_tx_fail protected-root-drift-before-mutation || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/protected-source.manifest" "$BRORAY_TX_WORK/protected-barrier.manifest" || broray_tx_fail protected-data-drift-before-mutation || return 1
    broray_tx_external_postcheck || broray_tx_fail external-state-drift-before-mutation || return 1
    broray_tx_stop_services || return 1
    sync || broray_tx_fail sync-before-mutation-failed || return 1
    broray_tx_user_validate "$BRORAY_TX_APP_ROOT" protected-stopped || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/protected-source-roots.list" "$BRORAY_TX_WORK/protected-stopped-roots.list" || broray_tx_fail protected-root-drift-after-stop || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/protected-source.manifest" "$BRORAY_TX_WORK/protected-stopped.manifest" || broray_tx_fail protected-data-drift-after-stop || return 1
    broray_tx_external_postcheck || broray_tx_fail external-state-drift-after-stop || return 1
    # Last re-df/re-inode gate is immediately adjacent to the mutation marker,
    # after service stop and every drift barrier.
    broray_tx_pre_mutation_space_gate || return 1
    broray_tx_candidate_evidence_revalidate || broray_tx_fail candidate-capability-evidence-drift || return 1
    broray_tx_operation_relation_verify pre-mutation || return 1
    broray_tx_test_pause before-mutation || return 1
    broray_tx_event first-destructive-mutation || return 1
    BRORAY_TX_MUTATED=1; printf '%s\n' yes >"$BRORAY_TX_WORK/mutation.started" || return 1
    broray_tx_persistent_marker_phase_reach mutation-started || return 1
    broray_tx_source_registration_remove || broray_tx_fail source-registration-remove-failed || return 1
    broray_tx_test_pause mutation-started || return 1
    broray_tx_status running '' || return 1
    broray_tx_test_pause after-mutation || return 1
    broray_tx_clean_program_tree || broray_tx_fail clean-program-tree-failed || return 1
    broray_tx_post_delete_space_gate || return 1
    broray_tx_inject install || return 1
    if ! broray_tx_inject partial-extraction; then
        # Isolated-root fault injection must leave a genuinely partial target,
        # so the rollback test exercises cleanup of mixed extraction state.
        # Production can never enter this branch: broray_tx_inject is disabled
        # unless TEST_MODE=1 and FS_ROOT is not '/'.
        broray_tx_partial_member="$(awk -F '|' '$1=="F"{print $2;exit}' "$BRORAY_TX_WORK/candidate-check/control/payload-manifest.tsv")"
        broray_tx_relative_safe "$broray_tx_partial_member" || return 1
        tar -xzf "$BRORAY_TX_WORK/candidate-check/outer/data.tar.gz" -C "$BRORAY_TX_FS_ROOT" \
            "$broray_tx_partial_member" 2>"$BRORAY_TX_WORK/evidence/partial-extraction.stderr" || return 1
        [ -f "$BRORAY_TX_FS_ROOT/$broray_tx_partial_member" ] || return 1
        broray_tx_event partial-extraction-injected || return 1
        return 1
    fi
    tar -xzf "$BRORAY_TX_WORK/candidate-check/outer/data.tar.gz" -C "$BRORAY_TX_FS_ROOT" 2>"$BRORAY_TX_WORK/evidence/candidate-extract.stderr" || broray_tx_fail candidate-data-install-failed || return 1
    [ -f "$BRORAY_TX_APP_ROOT/lib/package-transaction.sh" ] && [ ! -L "$BRORAY_TX_APP_ROOT/lib/package-transaction.sh" ] || broray_tx_fail installed-engine-missing || return 1
    [ "$(broray_tx_sha "$BRORAY_TX_APP_ROOT/lib/package-transaction.sh")" = "$(cat "$BRORAY_TX_WORK/candidate-engine.sha256")" ] || broray_tx_fail installed-engine-not-candidate-byte || return 1
    broray_tx_event clean-replacement-complete || return 1
    broray_tx_status running ''
}

broray_tx_adopt_work()
{
    BRORAY_TX_WORK="$1"
    case "$BRORAY_TX_WORK" in "$BRORAY_TX_TMP_BASE"/broray-update-*) ;; *) return 1 ;; esac
    [ -d "$BRORAY_TX_WORK" ] && [ ! -L "$BRORAY_TX_WORK" ] || return 1
    BRORAY_TX_OPERATION_ID="$(sed -n '1p' "$BRORAY_TX_WORK/operation-id" 2>/dev/null)"
    BRORAY_TX_MODE="$(sed -n '1p' "$BRORAY_TX_WORK/mode" 2>/dev/null)"
    BRORAY_TX_ORIGIN="$(sed -n '1p' "$BRORAY_TX_WORK/origin" 2>/dev/null)"
    broray_tx_valid_id "$BRORAY_TX_OPERATION_ID" || return 1
    [ "$(sed -n '1p' "$BRORAY_TX_WORK/lifecycle-contract" 2>/dev/null)" = "$BRORAY_TX_CONTRACT" ] || return 1
    [ -f "$BRORAY_TX_WORK/operation.json" ] && [ ! -L "$BRORAY_TX_WORK/operation.json" ] || return 1
    jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg mode "$BRORAY_TX_MODE" \
        --arg path "$BRORAY_TX_WORK" --arg target "$BRORAY_TX_TARGET_PACKAGE" \
        --arg lifecycle "$BRORAY_TX_CONTRACT" '
      (type == "object") and (.schemaVersion == 2) and
      (.lifecycleContract == $lifecycle) and (.operationId == $id) and
      (.mode == $mode) and (.transactionPath == $path) and (.targetVersion == $target) and
      ((.sourceVersion | type) == "string") and ((.sourceVersion | length) > 0) and
      ((.sourceAppVersion | type) == "string") and ((.sourceAppVersion | length) > 0) and
      ((.sourceClass | type) == "string") and ((.sourceClass | length) > 0) and
      ((.migrationId | type) == "string") and ((.migrationId | length) > 0)
    ' "$BRORAY_TX_WORK/operation.json" >/dev/null 2>&1 || return 1
    BRORAY_TX_SOURCE_PACKAGE="$(jq -r '.sourceVersion' "$BRORAY_TX_WORK/operation.json")"
    BRORAY_TX_SOURCE_APP="$(jq -r '.sourceAppVersion' "$BRORAY_TX_WORK/operation.json")"
    BRORAY_TX_SOURCE_CLASS="$(jq -r '.sourceClass' "$BRORAY_TX_WORK/operation.json")"
    BRORAY_TX_MIGRATION_ID="$(jq -r '.migrationId' "$BRORAY_TX_WORK/operation.json")"
    [ ! -f "$BRORAY_TX_WORK/mutation.started" ] || BRORAY_TX_MUTATED=1
    broray_tx_adopt_cap_json=0
    broray_tx_adopt_cap_tsv=0
    [ ! -e "$BRORAY_TX_WORK/evidence/capabilities.json" ] &&
        [ ! -L "$BRORAY_TX_WORK/evidence/capabilities.json" ] || broray_tx_adopt_cap_json=1
    [ ! -e "$BRORAY_TX_WORK/evidence/capabilities.tsv" ] &&
        [ ! -L "$BRORAY_TX_WORK/evidence/capabilities.tsv" ] || broray_tx_adopt_cap_tsv=1
    [ "$broray_tx_adopt_cap_json" -eq "$broray_tx_adopt_cap_tsv" ] || return 1
    if [ "$broray_tx_adopt_cap_json" -eq 1 ]; then
        broray_runtime_reactivate_current_operation "$BRORAY_TX_WORK" || return 1
    else
        # A retained pre-mutation snapshot remains independently verifiable
        # when every optional outer sidecar is absent.  Admit its authenticated
        # capsule in read-only scratch before reconstructing anything in the
        # retained workspace.  Never use this path for a live/mutated handoff.
        [ ! -f "$BRORAY_TX_WORK/mutation.started" ] &&
            [ ! -f "$BRORAY_TX_WORK/application.pass" ] || return 1
        [ ! -e "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] || return 1
        broray_runtime_resolve_release_tools || return 1
        broray_runtime_activate_if_resolved || return 1
        broray_tx_recovery_snapshot_admit_readonly || return 1
        BRORAY_TX_RECOVERY_SNAPSHOT_SOURCE=archive-capsule-only
    fi
    if [ -e "$BRORAY_TX_LOCK_DIR" ] || [ -L "$BRORAY_TX_LOCK_DIR" ]; then
        broray_tx_native_opkg_lock_adopt || return 1
        broray_tx_lock_adopt || return 1
    else
        # Read-only verification of a retained, pre-mutation snapshot is the
        # only adoption mode allowed after the owning operation released its
        # lock.  Application/registration hand-offs always require ownership.
        [ ! -f "$BRORAY_TX_WORK/mutation.started" ] && [ ! -f "$BRORAY_TX_WORK/application.pass" ] || return 1
        BRORAY_TX_LOCK_HELD=0
    fi
    broray_tx_trap_enable
}

broray_tx_migrate_user_state()
{
    broray_tx_event restore-migrate || return 1
    broray_tx_inject migration || return 1
    broray_tx_inject restore || return 1
    broray_tx_snapshot_verify_core || return 1
    broray_tx_old_app_rel="$(broray_tx_to_relative "$BRORAY_TX_APP_ROOT")" || return 1
    if [ -s "$BRORAY_TX_WORK/protected-source-roots.list" ]; then
        set --
        while IFS= read -r broray_tx_user_rel; do
            [ -n "$broray_tx_user_rel" ] || continue
            case "$broray_tx_user_rel" in ''|/*|*'..'*|*'|'*|*'\t'*) broray_tx_fail "migration-path-unsafe:$broray_tx_user_rel"; return 1 ;; esac
            broray_tx_archive_user="$broray_tx_old_app_rel/$broray_tx_user_rel"
            grep -Fqx "$broray_tx_archive_user" "$BRORAY_TX_WORK/archive.members" || broray_tx_fail "migration-source-not-in-snapshot:$broray_tx_user_rel" || return 1
            broray_tx_user_target="$BRORAY_TX_APP_ROOT/$broray_tx_user_rel"
            case "$broray_tx_user_target" in "$BRORAY_TX_APP_ROOT"/*) ;; *) return 1 ;; esac
            rm -rf "$broray_tx_user_target" || return 1
            mkdir -p "${broray_tx_user_target%/*}" || return 1
            set -- "$@" "$broray_tx_archive_user"
        done <"$BRORAY_TX_WORK/protected-source-roots.list"
        [ "$#" -gt 0 ] || return 1
        tar -xzf "$BRORAY_TX_WORK/backup.tar.gz" -C "$BRORAY_TX_FS_ROOT" "$@" \
            2>"$BRORAY_TX_WORK/evidence/protected-state-restore.stderr" || broray_tx_fail snapshot-direct-migration-failed || return 1
        broray_tx_manifest_build "$BRORAY_TX_APP_ROOT" "$BRORAY_TX_WORK/protected-source-roots.list" "$BRORAY_TX_WORK/protected-restored.manifest" || return 1
        broray_tx_files_equal "$BRORAY_TX_WORK/protected-source.manifest" "$BRORAY_TX_WORK/protected-restored.manifest" || broray_tx_fail protected-state-not-restored || return 1
    fi
    # Conffile-conflict files are source bytes and are never silently deleted.
    # Generic update preservation is byte-exact and version-independent.
    broray_tx_tmp_measure restore-complete || return 1
    broray_tx_event restore-complete || return 1
    broray_tx_status running ''
}

broray_tx_run_package_setup()
{
    broray_tx_event new-version-setup || return 1
    broray_tx_inject setup || return 1
    [ "${BRORAY_TX_SKIP_SETUP:-0}" != 1 ] || return 0
    broray_tx_setup_script="$BRORAY_TX_APP_ROOT/lib/package-setup.sh"
    [ -x "$broray_tx_setup_script" ] && [ ! -L "$broray_tx_setup_script" ] || broray_tx_fail package-setup-missing || return 1
    broray_tx_setup_manifest="$BRORAY_TX_WORK/candidate-check/control/payload-manifest.tsv"
    [ -f "$broray_tx_setup_manifest" ] && [ ! -L "$broray_tx_setup_manifest" ] ||
        broray_tx_fail setup-write-contract-manifest-missing || return 1
    awk -F '|' '$1=="F" && $2=="opt/broray/lib/package-setup.sh"{print}' \
        "$broray_tx_setup_manifest" >"$BRORAY_TX_WORK/setup-write-contract.row" || return 1
    [ "$(wc -l <"$BRORAY_TX_WORK/setup-write-contract.row" | tr -d ' ')" -eq 1 ] ||
        broray_tx_fail setup-write-contract-row-ambiguous || return 1
    IFS='|' read -r broray_tx_setup_type broray_tx_setup_path broray_tx_setup_bytes broray_tx_setup_sha \
        broray_tx_setup_mode broray_tx_setup_uid broray_tx_setup_gid <"$BRORAY_TX_WORK/setup-write-contract.row" || return 1
    [ "$broray_tx_setup_type" = F ] && [ "$broray_tx_setup_path" = opt/broray/lib/package-setup.sh ] &&
        [ "$broray_tx_setup_bytes" = "$(wc -c <"$broray_tx_setup_script" | tr -d ' ')" ] &&
        [ "$broray_tx_setup_sha" = "$(broray_tx_sha "$broray_tx_setup_script")" ] &&
        [ "$broray_tx_setup_mode" = "$(find -P "$broray_tx_setup_script" -maxdepth 0 -printf '%m')" ] ||
        broray_tx_fail setup-write-contract-payload-drift || return 1
    grep -Fqx "BRORAY_SETUP_WRITE_CONTRACT=\"$BRORAY_TX_SETUP_WRITE_CONTRACT\"" "$broray_tx_setup_script" ||
        broray_tx_fail setup-write-contract-marker-missing || return 1
    broray_tx_setup_per_file_kb="$(broray_tx_umul "$BRORAY_TX_SETUP_FILE_LIMIT_BLOCKS512" 512)" || return 1
    broray_tx_setup_per_file_kb="$(broray_tx_ceil_div "$broray_tx_setup_per_file_kb" 1024)" || return 1
    broray_tx_setup_contract_kb="$(broray_tx_umul "$broray_tx_setup_per_file_kb" \
        "$BRORAY_TX_SETUP_WRITABLE_REGULAR_CAP")" || return 1
    [ "$broray_tx_setup_contract_kb" -eq "$BRORAY_TX_SETUP_GROWTH_CAP_KB" ] ||
        broray_tx_fail setup-write-contract-arithmetic-drift || return 1
    broray_tx_setup_allocation_cap_kb="$(jq -r '.setupAllocatedUpperKB' "$BRORAY_TX_WORK/evidence/space.json")"
    broray_tx_number "$broray_tx_setup_allocation_cap_kb" &&
        [ "$broray_tx_setup_allocation_cap_kb" -ge "$BRORAY_TX_SETUP_GROWTH_CAP_KB" ] ||
        broray_tx_fail setup-allocation-upper-missing || return 1
    broray_tx_setup_free_before="$(df -Pk "$BRORAY_TX_OPT_ROOT" 2>/dev/null | awk 'NR==2{print $4;exit}')"
    broray_tx_setup_inodes_before="$(broray_tx_df_inodes "$BRORAY_TX_OPT_ROOT")"
    broray_tx_setup_tree_before="$(du -sk "$BRORAY_TX_APP_ROOT" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_setup_tree_inodes_before="$(find -P "$BRORAY_TX_APP_ROOT" -xdev -print | wc -l | tr -d ' ')"
    broray_tx_number "$broray_tx_setup_free_before" && broray_tx_number "$broray_tx_setup_inodes_before" &&
        broray_tx_number "$broray_tx_setup_tree_before" && broray_tx_number "$broray_tx_setup_tree_inodes_before" || return 1
    BRORAY_PACKAGE_TARGET="$BRORAY_TX_APP_ROOT" BRORAY_OPT_ROOT="$BRORAY_TX_OPT_ROOT" BRORAY_INIT_ROOT="$BRORAY_TX_OPT_ROOT/etc/init.d" \
        BRORAY_ASH="$BRORAY_TX_ASH" BRORAY_PACKAGE_SKIP_KEENETIC=1 BRORAY_PACKAGE_SKIP_SERVICES=1 \
        BRORAY_PACKAGE_SKIP_DNS_MIGRATION=1 BRORAY_PACKAGE_SKIP_MAINTENANCE=1 BRORAY_PACKAGE_PRESERVE_EXISTING=1 \
        BRORAY_PACKAGE_WRITE_CONTRACT="$BRORAY_TX_SETUP_WRITE_CONTRACT" \
        BRORAY_SETUP_SERVICE_STATE_FILE="$BRORAY_TX_WORK/services-before.tsv" BRORAY_SETUP_SOURCE_CLASS="$BRORAY_TX_SOURCE_CLASS" \
        broray_tx_bounded_command "$BRORAY_TX_SETUP_OUTPUT_EACH_CAP_KB" \
            "$BRORAY_TX_WORK/evidence/package-setup.stdout" "$BRORAY_TX_WORK/evidence/package-setup.stderr" \
            "$BRORAY_TX_SETUP_FILE_LIMIT_BLOCKS512" "$BRORAY_TX_ASH" "$broray_tx_setup_script" || {
            broray_tx_fail new-version-package-setup-failed
            return 1
        }
    broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/package-setup.stdout" "$BRORAY_TX_SETUP_OUTPUT_EACH_CAP_KB" &&
        broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/package-setup.stderr" "$BRORAY_TX_SETUP_OUTPUT_EACH_CAP_KB" ||
        broray_tx_fail setup-output-writer-cap-exceeded || return 1
    broray_tx_setup_free_after="$(df -Pk "$BRORAY_TX_OPT_ROOT" 2>/dev/null | awk 'NR==2{print $4;exit}')"
    broray_tx_setup_inodes_after="$(broray_tx_df_inodes "$BRORAY_TX_OPT_ROOT")"
    broray_tx_setup_tree_after="$(du -sk "$BRORAY_TX_APP_ROOT" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_setup_tree_inodes_after="$(find -P "$BRORAY_TX_APP_ROOT" -xdev -print | wc -l | tr -d ' ')"
    broray_tx_number "$broray_tx_setup_free_after" && broray_tx_number "$broray_tx_setup_inodes_after" &&
        broray_tx_number "$broray_tx_setup_tree_after" && broray_tx_number "$broray_tx_setup_tree_inodes_after" || return 1
    broray_tx_setup_df_growth="$(broray_tx_sub0 "$broray_tx_setup_free_before" "$broray_tx_setup_free_after")" || return 1
    broray_tx_setup_tree_growth="$(broray_tx_sub0 "$broray_tx_setup_tree_after" "$broray_tx_setup_tree_before")" || return 1
    broray_tx_setup_growth="$(broray_tx_umax "$broray_tx_setup_df_growth" "$broray_tx_setup_tree_growth")" || return 1
    broray_tx_setup_df_inode_growth="$(broray_tx_sub0 "$broray_tx_setup_inodes_before" "$broray_tx_setup_inodes_after")" || return 1
    broray_tx_setup_tree_inode_growth="$(broray_tx_sub0 "$broray_tx_setup_tree_inodes_after" "$broray_tx_setup_tree_inodes_before")" || return 1
    broray_tx_setup_inode_growth="$(broray_tx_umax "$broray_tx_setup_df_inode_growth" "$broray_tx_setup_tree_inode_growth")" || return 1
    broray_tx_setup_growth="$(broray_tx_growth_within_cap "$broray_tx_setup_growth" 0 "$broray_tx_setup_allocation_cap_kb")" ||
        broray_tx_fail setup-growth-cap-exceeded || return 1
    broray_tx_setup_inode_growth="$(broray_tx_growth_within_cap "$broray_tx_setup_inode_growth" 0 "$BRORAY_TX_SETUP_INODE_CAP")" ||
        broray_tx_fail setup-inode-cap-exceeded || return 1
    jq --argjson setupGrowthActualKB "$broray_tx_setup_growth" --argjson setupGrowthActualInodes "$broray_tx_setup_inode_growth" \
        '.setupGrowthActualKB=$setupGrowthActualKB|.setupGrowthActualInodes=$setupGrowthActualInodes' \
        "$BRORAY_TX_WORK/evidence/space.json" >"$BRORAY_TX_WORK/evidence/space.json.part" || return 1
    mv -f "$BRORAY_TX_WORK/evidence/space.json.part" "$BRORAY_TX_WORK/evidence/space.json" || return 1
}

# Service processes must not inherit the bounded setup child's RLIMIT_FSIZE.
# Apply the same source-state/default policy as package-setup after that child
# has exited, while the native OPKG exclusion is still continuously proved.
broray_tx_target_service_state_apply()
{
    [ -f "$BRORAY_TX_WORK/services-before.tsv" ] && [ ! -L "$BRORAY_TX_WORK/services-before.tsv" ] || return 1
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "${BRORAY_TX_TEST_SERVICE_IDENTITY:-0}" != 1 ]; then
        return 0
    fi
    while IFS="$(printf '\t')" read -r broray_tx_target_service_name broray_tx_target_source_state; do
        case "$broray_tx_target_service_name" in
            S23broray-monitor|S24broray|S25broray-web|S27broray-auto-switch|S28broray-subscriptions) ;;
            *) return 1 ;;
        esac
        case "$broray_tx_target_source_state" in
            running) broray_tx_target_desired_state=running ;;
            stopped) broray_tx_target_desired_state=stopped ;;
            absent)
                case "$broray_tx_target_service_name" in
                    S24broray|S25broray-web) broray_tx_target_desired_state=running ;;
                    *) broray_tx_target_desired_state=stopped ;;
                esac
                ;;
            *) return 1 ;;
        esac
        broray_tx_target_service="$BRORAY_TX_OPT_ROOT/etc/init.d/$broray_tx_target_service_name"
        [ -f "$broray_tx_target_service" ] && [ ! -L "$broray_tx_target_service" ] &&
            [ -x "$broray_tx_target_service" ] || return 1
        broray_tx_service_identity_measure "$broray_tx_target_service_name" || return 1
        if [ "$broray_tx_target_desired_state" = running ]; then
            if [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" != running ]; then
                [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = stopped ] || return 1
                broray_tx_native_opkg_lock_service_guard "target-service-start-$broray_tx_target_service_name" || return 1
                "$broray_tx_target_service" start >/dev/null 2>&1 || return 1
            fi
            "$broray_tx_target_service" status >/dev/null 2>&1 || return 1
            broray_tx_service_identity_measure "$broray_tx_target_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = running ] || return 1
        else
            if [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = running ]; then
                broray_tx_native_opkg_lock_service_guard "target-service-stop-$broray_tx_target_service_name" || return 1
                "$broray_tx_target_service" stop >/dev/null 2>&1 || return 1
            fi
            "$broray_tx_target_service" status >/dev/null 2>&1 && return 1
            broray_tx_service_identity_measure "$broray_tx_target_service_name" || return 1
            [ "$BRORAY_TX_SERVICE_IDENTITY_STATE" = stopped ] || return 1
        fi
    done <"$BRORAY_TX_WORK/services-before.tsv"
}

broray_tx_external_postcheck()
{
    broray_tx_external_before="$BRORAY_TX_WORK/snapshot-meta/keenetic-running-before.txt"
    [ -f "$broray_tx_external_before" ] || return 1
    [ -s "$broray_tx_external_before" ] || return 1
    [ "$(sed -n '1p' "$broray_tx_external_before")" != UNAVAILABLE ] || return 1
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ -n "${BRORAY_TX_TEST_KEENETIC_STATE:-}" ]; then cp -p "$BRORAY_TX_TEST_KEENETIC_STATE" "$BRORAY_TX_WORK/evidence/keenetic-running-after.txt"; else command -v ndmc >/dev/null 2>&1 && ndmc -c 'show running-config' >"$BRORAY_TX_WORK/evidence/keenetic-running-after.txt" 2>/dev/null || return 1; fi
    grep -E 'Proxy[0-9]+|ip http proxy broray|dns-proxy (tls|https) upstream|^[[:space:]]*ip route .*Proxy[0-9]+' "$broray_tx_external_before" | LC_ALL=C sort >"$BRORAY_TX_WORK/evidence/keenetic-protected-before.txt" || true
    grep -E 'Proxy[0-9]+|ip http proxy broray|dns-proxy (tls|https) upstream|^[[:space:]]*ip route .*Proxy[0-9]+' "$BRORAY_TX_WORK/evidence/keenetic-running-after.txt" | LC_ALL=C sort >"$BRORAY_TX_WORK/evidence/keenetic-protected-after.txt" || true
    broray_tx_files_equal "$BRORAY_TX_WORK/evidence/keenetic-protected-before.txt" "$BRORAY_TX_WORK/evidence/keenetic-protected-after.txt"
}

broray_tx_local_http_probe()
{
    broray_tx_local_http_address="$1"
    broray_tx_local_http_timeout="$2"
    case "$broray_tx_local_http_address" in ''|*[!0-9.]*) return 1 ;; esac
    broray_tx_number "$broray_tx_local_http_timeout" &&
        [ "$broray_tx_local_http_timeout" -gt 0 ] || return 1
    (
        unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY no_proxy
        curl -q -fsS --noproxy '*' --max-time "$broray_tx_local_http_timeout" \
            "http://$broray_tx_local_http_address:8080/"
    )
}

broray_tx_postcheck_application()
{
    broray_tx_event postcheck-application || return 1
    broray_tx_inject application-postcheck || return 1
    broray_tx_inject postcheck || return 1
    [ -f "$BRORAY_TX_APP_ROOT/config/version" ] && [ "$(sed -n '1p' "$BRORAY_TX_APP_ROOT/config/version")" = "$BRORAY_TX_TARGET_APP" ] || broray_tx_fail postcheck-app-version || return 1
    jq -e --arg build "$BRORAY_TX_TARGET_WEBUI" --arg release "$BRORAY_TX_TARGET_RELEASE" '.buildId==$build and .releaseId==$release' "$BRORAY_TX_APP_ROOT/web-new/build.json" >/dev/null 2>&1 || broray_tx_fail postcheck-webui-build || return 1
    [ -x "$BRORAY_TX_APP_ROOT/bin/xray" ] || broray_tx_fail postcheck-xray-executable || return 1
    jq -e --arg release "$BRORAY_TX_TARGET_RELEASE" '.schemaVersion==3 and .releaseId==$release and .historicalTransactionStateRequired==false' "$BRORAY_TX_APP_ROOT/share/release/manifest.json" >/dev/null 2>&1 || broray_tx_fail postcheck-runtime-manifest || return 1
    if [ -s "$BRORAY_TX_WORK/protected-source-roots.list" ]; then
        broray_tx_user_validate "$BRORAY_TX_APP_ROOT" protected-check || return 1
        broray_tx_manifest_build "$BRORAY_TX_APP_ROOT" "$BRORAY_TX_WORK/protected-source-roots.list" "$BRORAY_TX_WORK/protected-check.manifest" || return 1
        broray_tx_files_equal "$BRORAY_TX_WORK/protected-source.manifest" "$BRORAY_TX_WORK/protected-check.manifest" || broray_tx_fail postcheck-protected-state || return 1
    fi
    broray_tx_external_postcheck || broray_tx_fail postcheck-external-keenetic-state || return 1
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ]; then
        for broray_tx_service in S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions; do [ -f "$BRORAY_TX_OPT_ROOT/etc/init.d/$broray_tx_service" ] || broray_tx_fail "postcheck-service-file:$broray_tx_service" || return 1; done
    else
        [ -f "$BRORAY_TX_APP_ROOT/config/config.json" ] || broray_tx_fail postcheck-xray-config-missing || return 1
        XRAY_LOCATION_ASSET="$BRORAY_TX_APP_ROOT/bin" "$BRORAY_TX_APP_ROOT/bin/xray" run -test -c "$BRORAY_TX_APP_ROOT/config/config.json" >/dev/null 2>&1 || broray_tx_fail postcheck-xray-config-test || return 1
        broray_tx_service_state_postcheck || broray_tx_fail postcheck-service-semantic-state || return 1
        broray_tx_lan="$(jq -r '.listenAddress // empty' "$BRORAY_TX_APP_ROOT/config/system/settings.json")"
        [ -n "$broray_tx_lan" ] && broray_tx_local_http_probe "$broray_tx_lan" 8 >/dev/null 2>&1 || broray_tx_fail postcheck-webui-http || return 1
        ndmc -c 'show running-config' 2>/dev/null | grep -Eq 'Proxy[0-9]+' || broray_tx_fail postcheck-managed-proxy || return 1
        ndmc -c 'show running-config' 2>/dev/null | grep -Eq 'ip http proxy broray' || broray_tx_fail postcheck-http-proxy || return 1
    fi
    broray_tx_event postcheck-application-pass || return 1
    broray_tx_status running ''
}

broray_tx_handoff_write()
{
    jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" --arg workspace "$BRORAY_TX_WORK" --arg origin "$BRORAY_TX_ORIGIN" --arg lifecycle "$BRORAY_TX_CONTRACT" --arg snapshotSha256 "$(cat "$BRORAY_TX_WORK/snapshot.verified")" \
        '{schemaVersion:1,operationId:$operationId,workspace:$workspace,origin:$origin,lifecycleContract:$lifecycle,snapshotSha256:$snapshotSha256,applicationPass:true}' >"$BRORAY_TX_CURRENT.part" || return 1
    mv -f "$BRORAY_TX_CURRENT.part" "$BRORAY_TX_CURRENT" || return 1
    chmod 600 "$BRORAY_TX_CURRENT" 2>/dev/null || true
}

broray_tx_handoff_adopt()
{
    [ -f "$BRORAY_TX_CURRENT" ] && [ ! -L "$BRORAY_TX_CURRENT" ] || return 1
    broray_tx_handoff_work="$(jq -r '.workspace // empty' "$BRORAY_TX_CURRENT" 2>/dev/null)"
    broray_tx_adopt_work "$broray_tx_handoff_work" || return 1
    jq -e --arg id "$BRORAY_TX_OPERATION_ID" --arg workspace "$BRORAY_TX_WORK" --arg lifecycle "$BRORAY_TX_CONTRACT" --arg sha "$(cat "$BRORAY_TX_WORK/snapshot.verified" 2>/dev/null)" '
      .schemaVersion==1 and .operationId==$id and .workspace==$workspace and .lifecycleContract==$lifecycle and .snapshotSha256==$sha and .applicationPass==true
    ' "$BRORAY_TX_CURRENT" >/dev/null 2>&1 || return 1
    [ -f "$BRORAY_TX_WORK/application.pass" ] && [ ! -L "$BRORAY_TX_WORK/application.pass" ]
}

broray_tx_prepare_application()
{
    broray_tx_fail retired-legacy-application-transaction
    return 1
}

broray_tx_adopt_application()
{
    broray_tx_adopt_work "$1" || return 1
    BRORAY_TX_HANDOFF_CHILD=1
    BRORAY_TX_MUTATED=1
    if ! broray_tx_migrate_user_state || ! broray_tx_run_package_setup ||
       ! broray_tx_inject service-start || ! broray_tx_target_service_state_apply ||
       ! broray_tx_postcheck_application || ! broray_tx_postcheck_application
    then
        broray_tx_failure
        return 1
    fi
    broray_tx_registration_absent || { broray_tx_fail target-was-registered-before-application-handoff; broray_tx_failure; return 1; }
    printf '%s\n' yes >"$BRORAY_TX_WORK/application.pass" || return 1
    broray_tx_handoff_write || { broray_tx_fail handoff-write-failed; broray_tx_failure; return 1; }
    broray_tx_status running ''
}

broray_tx_status_without_broray()
{
    broray_tx_status_without_broray_to "$1"
}

broray_tx_manual_registration_prepare()
{
    # Registration is committed only after 2x application PASS.
    return 0
}

broray_tx_foreign_status_guard()
{
    broray_tx_status_without_broray_to "$BRORAY_TX_WORK/foreign-status.current" || return 1
    broray_tx_foreign_expected="$(cat "$BRORAY_TX_WORK/recovery-read/.broray-recovery/foreign-status.sha256" 2>/dev/null)"
    [ "${#broray_tx_foreign_expected}" -eq 64 ] || return 1
    [ "$(broray_tx_sha "$BRORAY_TX_WORK/foreign-status.current")" = "$broray_tx_foreign_expected" ]
}

broray_tx_registration_absent()
{
    broray_tx_status_stanza_broray >"$BRORAY_TX_WORK/status-unregistered-check" || return 1
    [ ! -s "$BRORAY_TX_WORK/status-unregistered-check" ] || return 1
    for broray_tx_registration_check in "$BRORAY_TX_INFO_ROOT"/broray.*; do
        [ ! -e "$broray_tx_registration_check" ] && [ ! -L "$broray_tx_registration_check" ] || return 1
    done
    broray_tx_foreign_status_guard
}

broray_tx_registration_commit()
{
    broray_tx_event opkg-registration || return 1
    broray_tx_inject registration || return 1
    broray_tx_foreign_status_guard || broray_tx_fail foreign-opkg-drift-before-registration || return 1
    [ -d "$BRORAY_TX_INFO_ROOT" ] && [ ! -L "$BRORAY_TX_INFO_ROOT" ] || return 1
    broray_tx_registration_stage="$BRORAY_TX_WORK/registration-metadata"
    rm -rf "$broray_tx_registration_stage"
    mkdir -p "$broray_tx_registration_stage" || return 1
    cp -p "$BRORAY_TX_WORK/candidate-check/control/control" "$broray_tx_registration_stage/broray.control" || return 1
    cp -p "$BRORAY_TX_WORK/candidate-check/control/conffiles" "$broray_tx_registration_stage/broray.conffiles" || return 1
    cp -p "$BRORAY_TX_WORK/candidate-check/control/payload-manifest.tsv" "$broray_tx_registration_stage/broray.payload-manifest.tsv" || return 1
    for broray_tx_hook in preinst postinst prerm postrm; do
        cp -p "$BRORAY_TX_WORK/candidate-check/control/$broray_tx_hook" "$broray_tx_registration_stage/broray.$broray_tx_hook" || return 1
    done
    awk -F '|' '$1=="F"||$1=="L"{print "/"$2}' "$BRORAY_TX_WORK/candidate-check/control/payload-manifest.tsv" | LC_ALL=C sort -u \
        >"$broray_tx_registration_stage/broray.list" || return 1
    broray_tx_sha "$BRORAY_TX_WORK/candidate.ipk" >"$broray_tx_registration_stage/broray.candidate-sha256" || return 1
    broray_tx_foreign_status_guard || broray_tx_fail foreign-opkg-drift-at-registration || return 1
    for broray_tx_info in "$BRORAY_TX_INFO_ROOT"/broray.*; do
        [ -e "$broray_tx_info" ] || [ -L "$broray_tx_info" ] || continue
        broray_tx_native_opkg_lock_assert "opkg-info-remove-before-${broray_tx_info##*/}" || return 1
        rm -f "$broray_tx_info" || return 1
        broray_tx_native_opkg_lock_assert "opkg-info-remove-after-${broray_tx_info##*/}" || return 1
    done
    for broray_tx_info in "$broray_tx_registration_stage"/broray.*; do
        broray_tx_info_name="${broray_tx_info##*/}"
        broray_tx_native_opkg_lock_assert "opkg-info-part-before-$broray_tx_info_name" || return 1
        cp -p "$broray_tx_info" "$BRORAY_TX_INFO_ROOT/$broray_tx_info_name.part" || return 1
        broray_tx_native_opkg_lock_assert "opkg-info-part-after-$broray_tx_info_name" || return 1
        broray_tx_native_opkg_lock_assert "opkg-info-rename-before-$broray_tx_info_name" || return 1
        mv -f "$BRORAY_TX_INFO_ROOT/$broray_tx_info_name.part" "$BRORAY_TX_INFO_ROOT/$broray_tx_info_name" || return 1
        broray_tx_native_opkg_lock_assert "opkg-info-rename-after-$broray_tx_info_name" || return 1
    done
    broray_tx_native_opkg_lock_assert opkg-status-part-before-registration || return 1
    broray_tx_status_without_broray_to "$BRORAY_TX_STATUS_FILE.part" || return 1
    cat "$BRORAY_TX_INFO_ROOT/broray.control" >>"$BRORAY_TX_STATUS_FILE.part" || return 1
    printf 'Status: install user installed\n\n' >>"$BRORAY_TX_STATUS_FILE.part" || return 1
    broray_tx_native_opkg_lock_assert opkg-status-part-after-registration || return 1
    broray_tx_status_part_kb="$(du -sk "$BRORAY_TX_STATUS_FILE.part" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_status_part_cap="$(broray_tx_uadd \
        "$(jq -r '.opkgStatusTransientUpperKB' "$BRORAY_TX_WORK/evidence/space.json")" \
        "$(jq -r '.opkgMetadataUpperKB' "$BRORAY_TX_WORK/evidence/space.json")")" || return 1
    broray_tx_number "$broray_tx_status_part_kb" && [ "$broray_tx_status_part_kb" -le "$broray_tx_status_part_cap" ] ||
        broray_tx_fail opkg-status-writer-cap-exceeded || return 1
    broray_tx_foreign_status_guard || broray_tx_fail foreign-opkg-drift-at-status-commit || return 1
    broray_tx_native_opkg_lock_assert opkg-status-rename-before-registration || return 1
    mv -f "$BRORAY_TX_STATUS_FILE.part" "$BRORAY_TX_STATUS_FILE" || return 1
    broray_tx_native_opkg_lock_assert opkg-status-rename-after-registration || return 1
    broray_tx_foreign_status_guard || broray_tx_fail foreign-opkg-drift-after-registration || return 1
    broray_tx_event opkg-registration-pass
}

broray_tx_status_version()
{
    awk 'BEGIN{RS=""} $0 ~ /(^|\n)Package:[[:space:]]*broray(\n|$)/ {n=split($0,a,"\n");for(i=1;i<=n;i++)if(a[i]~/^Version:[[:space:]]*/){sub(/^Version:[[:space:]]*/,"",a[i]);print a[i];exit}}' "$BRORAY_TX_STATUS_FILE" 2>/dev/null
}

broray_tx_status_precommit()
{
    [ -f "$BRORAY_TX_INFO_ROOT/broray.control" ] && [ ! -L "$BRORAY_TX_INFO_ROOT/broray.control" ] || return 1
    broray_tx_native_opkg_lock_assert opkg-status-precommit-part-before || return 1
    broray_tx_status_without_broray "$BRORAY_TX_STATUS_FILE.part" || return 1
    cat "$BRORAY_TX_INFO_ROOT/broray.control" >>"$BRORAY_TX_STATUS_FILE.part" || return 1
    printf 'Status: install user installed\n\n' >>"$BRORAY_TX_STATUS_FILE.part" || return 1
    broray_tx_native_opkg_lock_assert opkg-status-precommit-part-after || return 1
    broray_tx_native_opkg_lock_assert opkg-status-precommit-rename-before || return 1
    mv -f "$BRORAY_TX_STATUS_FILE.part" "$BRORAY_TX_STATUS_FILE" || return 1
    broray_tx_native_opkg_lock_assert opkg-status-precommit-rename-after
}

broray_tx_postcheck_registered()
{
    broray_tx_event postcheck-registered || return 1
    broray_tx_inject registered-postcheck || return 1
    [ "$(broray_tx_status_version)" = "$BRORAY_TX_TARGET_PACKAGE" ] || broray_tx_fail postcheck-opkg-version || return 1
    [ "$(broray_tx_control_value "$BRORAY_TX_INFO_ROOT/broray.control" Version)" = "$BRORAY_TX_TARGET_PACKAGE" ] || broray_tx_fail postcheck-installed-control-version || return 1
    [ "$(broray_tx_control_value "$BRORAY_TX_INFO_ROOT/broray.control" X-BROray-Canonical-Lifecycle)" = "$BRORAY_TX_CONTRACT" ] || broray_tx_fail postcheck-installed-control-contract || return 1
    [ "$(cat "$BRORAY_TX_INFO_ROOT/broray.candidate-sha256" 2>/dev/null)" = "$(broray_tx_sha "$BRORAY_TX_WORK/candidate.ipk")" ] || broray_tx_fail postcheck-candidate-sha-binding || return 1
    broray_tx_foreign_status_guard || broray_tx_fail postcheck-foreign-opkg-drift || return 1
    [ -s "$BRORAY_TX_INFO_ROOT/broray.list" ] || broray_tx_fail postcheck-opkg-filelist-empty || return 1
    awk -F '|' '$1=="F"||$1=="L"{print "/"$2}' "$BRORAY_TX_WORK/candidate-check/control/payload-manifest.tsv" | while IFS= read -r broray_tx_owned; do grep -Fqx "$broray_tx_owned" "$BRORAY_TX_INFO_ROOT/broray.list" || exit 91; done || broray_tx_fail postcheck-opkg-filelist-incomplete || return 1
    broray_tx_postcheck_application || return 1
    broray_tx_event postcheck-registered-pass
}

broray_tx_rollback_delete_candidate()
{
    if [ -f "$BRORAY_TX_WORK/candidate-check/control/payload-manifest.tsv" ] &&
       [ ! -L "$BRORAY_TX_WORK/candidate-check/control/payload-manifest.tsv" ]; then
        broray_tx_delete_candidate_tree || return 1
    else
        broray_tx_delete_candidate_tree_archive_only || return 1
    fi
    for broray_tx_info in "$BRORAY_TX_INFO_ROOT"/broray.*; do
        [ -e "$broray_tx_info" ] || [ -L "$broray_tx_info" ] || continue
        broray_tx_native_opkg_lock_assert "rollback-opkg-info-before-${broray_tx_info##*/}" || return 1
        rm -f "$broray_tx_info" || return 1
        broray_tx_native_opkg_lock_assert "rollback-opkg-info-after-${broray_tx_info##*/}" || return 1
    done
}

broray_tx_source_status_restore()
{
    broray_tx_foreign_status_guard || return 1
    broray_tx_native_opkg_lock_assert rollback-opkg-status-part-before || return 1
    broray_tx_status_restore_capsule="$BRORAY_TX_WORK/recovery-read/.broray-recovery"
    broray_tx_status_restore_presence="$(sed -n '1p' "$broray_tx_status_restore_capsule/opkg-status.presence" 2>/dev/null)"
    case "$broray_tx_status_restore_presence" in
        present)
            # The full shared status hash is guard evidence only.  Never
            # overwrite the shared database wholesale: retain the factual
            # current foreign projection and restore only the captured BROray
            # stanza while the native OPKG lock is held.
            broray_tx_status_without_broray_to "$BRORAY_TX_STATUS_FILE.part" || return 1
            if [ -s "$broray_tx_status_restore_capsule/broray-status.stanza" ]; then
                cat "$broray_tx_status_restore_capsule/broray-status.stanza" \
                    >>"$BRORAY_TX_STATUS_FILE.part" || return 1
            fi
            ;;
        absent)
            [ ! -s "$broray_tx_status_restore_capsule/opkg-status.before" ] || return 1
            broray_tx_status_without_broray_to "$BRORAY_TX_STATUS_FILE.part" || return 1
            [ ! -s "$BRORAY_TX_STATUS_FILE.part" ] || return 1
            ;;
        *) return 1 ;;
    esac
    broray_tx_native_opkg_lock_assert rollback-opkg-status-part-after || return 1
    broray_tx_status_restore_part_kb="$(du -sk "$BRORAY_TX_STATUS_FILE.part" 2>/dev/null | awk 'NR==1{print $1;exit}')"
    broray_tx_status_restore_part_cap="$(broray_tx_uadd \
        "$(jq -r '.opkgStatusTransientUpperKB' "$BRORAY_TX_WORK/evidence/space.json")" \
        "$(jq -r '.opkgMetadataUpperKB' "$BRORAY_TX_WORK/evidence/space.json")")" || return 1
    broray_tx_number "$broray_tx_status_restore_part_kb" && [ "$broray_tx_status_restore_part_kb" -le "$broray_tx_status_restore_part_cap" ] || return 1
    broray_tx_foreign_status_guard || return 1
    broray_tx_native_opkg_lock_assert rollback-opkg-status-rename-before || return 1
    if [ "$broray_tx_status_restore_presence" = present ]; then
        mv -f "$BRORAY_TX_STATUS_FILE.part" "$BRORAY_TX_STATUS_FILE" || return 1
    else
        rm -f "$BRORAY_TX_STATUS_FILE.part" "$BRORAY_TX_STATUS_FILE" || return 1
    fi
    broray_tx_native_opkg_lock_assert rollback-opkg-status-rename-after || return 1
    broray_tx_foreign_status_guard || return 1
    broray_tx_source_status_full_guard
}

broray_tx_source_status_full_guard()
{
    broray_tx_status_full_capsule="$BRORAY_TX_WORK/recovery-read/.broray-recovery"
    broray_tx_status_full_presence="$(sed -n '1p' "$broray_tx_status_full_capsule/opkg-status.presence" 2>/dev/null)"
    broray_tx_status_full_expected_sha="$(sed -n '1p' "$broray_tx_status_full_capsule/opkg-status.sha256" 2>/dev/null)"
    case "$broray_tx_status_full_expected_sha" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#broray_tx_status_full_expected_sha}" -eq 64 ] || return 1
    [ "$(broray_tx_sha "$broray_tx_status_full_capsule/opkg-status.before")" = \
      "$broray_tx_status_full_expected_sha" ] || return 1
    case "$broray_tx_status_full_presence" in present|absent) ;; *) return 1 ;; esac
    broray_tx_status_foreign_projection "$broray_tx_status_full_capsule/opkg-status.before" \
        "$BRORAY_TX_WORK/source-full-foreign.guard" || return 1
    broray_tx_files_equal "$broray_tx_status_full_capsule/foreign-status.before" \
        "$BRORAY_TX_WORK/source-full-foreign.guard" || return 1
    awk 'BEGIN{RS="";ORS="\n\n"}
      $0 ~ /(^|\n)Package:[[:space:]]*broray(\n|$)/ {print; found++}
      END{if(found>1)exit 2}' "$broray_tx_status_full_capsule/opkg-status.before" \
        >"$BRORAY_TX_WORK/source-full-broray.guard" || return 1
    broray_tx_files_equal "$broray_tx_status_full_capsule/broray-status.stanza" \
        "$BRORAY_TX_WORK/source-full-broray.guard"
}

broray_tx_postcheck_source()
{
    broray_tx_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/source-scope.list" "$BRORAY_TX_WORK/source-postcheck.manifest" || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/source.manifest" "$BRORAY_TX_WORK/source-postcheck.manifest" || return 1
    broray_tx_status_stanza_broray >"$BRORAY_TX_WORK/source-status.current" || return 1
    broray_tx_files_equal "$BRORAY_TX_WORK/recovery-read/.broray-recovery/broray-status.stanza" "$BRORAY_TX_WORK/source-status.current" || return 1
    broray_tx_foreign_status_guard || return 1
    broray_tx_source_status_full_guard || return 1
    broray_tx_external_postcheck || return 1
    broray_tx_service_state_postcheck || [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ]
}

broray_tx_rollback()
{
    broray_tx_event rollback || return 1
    broray_tx_inject rollback || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=snapshot-verify
    broray_tx_snapshot_verify_core || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=delete-candidate
    broray_tx_rollback_delete_candidate || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=remove-current-source-scope
    while IFS= read -r broray_tx_rollback_rel; do
        [ -n "$broray_tx_rollback_rel" ] || continue
        broray_tx_rollback_abs="$(broray_tx_root_path "/$broray_tx_rollback_rel")"
        case "$broray_tx_rollback_abs" in "$BRORAY_TX_WORK/snapshot-meta/keenetic-running-before.txt") continue ;; esac
        [ -e "$broray_tx_rollback_abs" ] || [ -L "$broray_tx_rollback_abs" ] || continue
        rm -rf "$broray_tx_rollback_abs" || return 1
    done <"$BRORAY_TX_WORK/source-scope.list"
    BRORAY_TX_ROLLBACK_FAILURE_STEP=extract-source-snapshot
    set --
    while IFS= read -r broray_tx_restore_rel; do [ -n "$broray_tx_restore_rel" ] && set -- "$@" "$broray_tx_restore_rel"; done <"$BRORAY_TX_WORK/source-scope.list"
    [ "$#" -gt 0 ] || return 1
    tar -xzf "$BRORAY_TX_WORK/backup.tar.gz" -C "$BRORAY_TX_FS_ROOT" "$@" || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=build-restored-manifest
    broray_tx_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/source-scope.list" "$BRORAY_TX_WORK/rollback.manifest" || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=compare-restored-manifest
    broray_tx_files_equal "$BRORAY_TX_WORK/source.manifest" "$BRORAY_TX_WORK/rollback.manifest" || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=build-restored-hardlink-topology
    broray_tx_allocation_topology_manifest "$BRORAY_TX_WORK/source.allocation.manifest" \
        "$BRORAY_TX_WORK/source.hardlink-topology.rollback-expected" || return 1
    broray_tx_allocation_manifest_build "$BRORAY_TX_FS_ROOT" "$BRORAY_TX_WORK/rollback.manifest" \
        "$BRORAY_TX_WORK/rollback.allocation.manifest" || return 1
    broray_tx_allocation_reclaimable_metrics "$BRORAY_TX_WORK/rollback.allocation.manifest" || return 1
    [ "$BRORAY_TX_ALLOCATION_EXTERNAL_HARDLINK_GROUPS" -eq 0 ] || return 1
    broray_tx_allocation_topology_manifest "$BRORAY_TX_WORK/rollback.allocation.manifest" \
        "$BRORAY_TX_WORK/rollback.hardlink-topology" || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=compare-restored-hardlink-topology
    broray_tx_files_equal "$BRORAY_TX_WORK/source.hardlink-topology.rollback-expected" \
        "$BRORAY_TX_WORK/rollback.hardlink-topology" || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=restore-source-opkg-status
    broray_tx_source_status_restore || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=restore-service-state
    broray_tx_service_state_restore || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=source-postcheck-1
    broray_tx_postcheck_source || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=source-postcheck-2
    broray_tx_postcheck_source || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=remove-current-handoff
    rm -f "$BRORAY_TX_CURRENT" 2>/dev/null || true
    BRORAY_TX_ROLLBACK_FAILURE_STEP=commit-rollback-verification
    printf '%s\n' yes >"$BRORAY_TX_WORK/rollback.verified"
    broray_tx_persistent_marker_phase_reach rollback-verified || return 1
    broray_tx_event rollback-verified
    broray_tx_test_pause rollback-verified || return 1
    BRORAY_TX_ROLLBACK_FAILURE_STEP=none
}

broray_tx_failure_terminal_cleanup()
{
    broray_tx_failure_terminal_kind="$1"
    broray_tx_failure_history="$BRORAY_TX_OPERATION_ROOT/$BRORAY_TX_OPERATION_ID"
    if [ ! -e "$BRORAY_TX_OPERATION_ROOT" ] && [ ! -L "$BRORAY_TX_OPERATION_ROOT" ]; then
        mkdir -p "$BRORAY_TX_OPERATION_ROOT" || return 1
    fi
    [ -d "$BRORAY_TX_OPERATION_ROOT" ] && [ ! -L "$BRORAY_TX_OPERATION_ROOT" ] || return 1
    if [ ! -e "$broray_tx_failure_history" ] && [ ! -L "$broray_tx_failure_history" ]; then
        mkdir "$broray_tx_failure_history" || return 1
        chmod 700 "$broray_tx_failure_history" 2>/dev/null || true
    fi
    [ -d "$broray_tx_failure_history" ] && [ ! -L "$broray_tx_failure_history" ] || return 1
    for broray_tx_failure_keep in operation.json failure.tsv failure.json rollback.verified snapshot.binding.verified pre-mutation-service-restore; do
        [ -f "$BRORAY_TX_WORK/$broray_tx_failure_keep" ] && [ ! -L "$BRORAY_TX_WORK/$broray_tx_failure_keep" ] || continue
        broray_tx_file_cap_kb "$BRORAY_TX_WORK/$broray_tx_failure_keep" "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" || return 1
        cp -p "$BRORAY_TX_WORK/$broray_tx_failure_keep" "$broray_tx_failure_history/$broray_tx_failure_keep.part" || return 1
        mv -f "$broray_tx_failure_history/$broray_tx_failure_keep.part" "$broray_tx_failure_history/$broray_tx_failure_keep" || return 1
    done
    if [ -f "$BRORAY_TX_WORK/evidence/events.tsv" ] && [ ! -L "$BRORAY_TX_WORK/evidence/events.tsv" ]; then
        broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/events.tsv" "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" || return 1
        cp -p "$BRORAY_TX_WORK/evidence/events.tsv" "$broray_tx_failure_history/events.tsv.part" || return 1
        mv -f "$broray_tx_failure_history/events.tsv.part" "$broray_tx_failure_history/events.tsv" || return 1
    fi
    if [ -f "$BRORAY_TX_WORK/evidence/test-sidecars-removed" ] && [ ! -L "$BRORAY_TX_WORK/evidence/test-sidecars-removed" ]; then
        broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/test-sidecars-removed" "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" || return 1
        cp -p "$BRORAY_TX_WORK/evidence/test-sidecars-removed" "$broray_tx_failure_history/test-sidecars-removed.part" || return 1
        mv -f "$broray_tx_failure_history/test-sidecars-removed.part" "$broray_tx_failure_history/test-sidecars-removed" || return 1
    fi
    broray_tx_failure_snapshot_sha="$(sed -n '1p' "$BRORAY_TX_WORK/snapshot.verified" 2>/dev/null)"
    broray_tx_failure_snapshot_bytes=0
    if [ -f "$BRORAY_TX_WORK/backup.tar.gz" ] && [ ! -L "$BRORAY_TX_WORK/backup.tar.gz" ]; then
        broray_tx_failure_snapshot_bytes="$(wc -c <"$BRORAY_TX_WORK/backup.tar.gz" | tr -d ' ')"
    fi
    printf 'contract\t%s\noperationId\t%s\nterminalKind\t%s\nrollback\t%s\nsnapshotSha256\t%s\nsnapshotSizeBytes\t%s\nworkspaceRemoved\ttrue\n' \
        "$BRORAY_TX_CONTRACT" "$BRORAY_TX_OPERATION_ID" "$broray_tx_failure_terminal_kind" \
        "$broray_tx_rollback_result" "$broray_tx_failure_snapshot_sha" "$broray_tx_failure_snapshot_bytes" \
        >"$broray_tx_failure_history/failure-audit.tsv.part" || return 1
    if [ -f "$BRORAY_TX_WORK/evidence/cleanup-plan.txt" ] &&
       [ ! -L "$BRORAY_TX_WORK/evidence/cleanup-plan.txt" ]; then
        broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/cleanup-plan.txt" "$BRORAY_TX_CLEANUP_PLAN_CAP_KB" || return 1
        awk '{print "cleanupPlan\t" $0}' "$BRORAY_TX_WORK/evidence/cleanup-plan.txt" \
            >>"$broray_tx_failure_history/failure-audit.tsv.part" || return 1
        if [ -f "$BRORAY_TX_WORK/evidence/cleanup-actions.txt" ] &&
           [ ! -L "$BRORAY_TX_WORK/evidence/cleanup-actions.txt" ]; then
            broray_tx_file_cap_kb "$BRORAY_TX_WORK/evidence/cleanup-actions.txt" "$BRORAY_TX_CLEANUP_PLAN_CAP_KB" || return 1
            awk '{print "cleanupAction\t" $0}' "$BRORAY_TX_WORK/evidence/cleanup-actions.txt" \
                >>"$broray_tx_failure_history/failure-audit.tsv.part" || return 1
        fi
    fi
    broray_tx_file_cap_kb "$broray_tx_failure_history/failure-audit.tsv.part" "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" || return 1
    mv -f "$broray_tx_failure_history/failure-audit.tsv.part" "$broray_tx_failure_history/failure-audit.tsv" || return 1
    broray_tx_terminal_control_retire || return 1
    BRORAY_TX_FAILURE_DURABLE="$broray_tx_failure_history"
}

broray_tx_failure()
{
    [ "$BRORAY_TX_FAILURE_RUNNING" -eq 0 ] || return 1
    BRORAY_TX_FAILURE_RUNNING=1
    broray_tx_trap_disable
    broray_tx_failure_reason="${BRORAY_TX_REASON:-unknown-failure}"; broray_tx_failure_stage="$BRORAY_TX_STAGE"
    broray_tx_event failure 2>/dev/null || true
    broray_tx_rollback_result=not-required
    if [ "$BRORAY_TX_MUTATED" -eq 1 ] || [ -f "$BRORAY_TX_WORK/mutation.started" ]; then
        if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "$BRORAY_TX_FS_ROOT" != / ] &&
           [ "${BRORAY_TX_TEST_DROP_SNAPSHOT_SIDECARS_ON_FAILURE:-0}" = 1 ]; then
            rm -f "$BRORAY_TX_WORK/backup.tar.gz.sha256" "$BRORAY_TX_WORK/source.manifest" \
                "$BRORAY_TX_WORK/source.allocation.manifest" "$BRORAY_TX_WORK/source-scope.list" \
                "$BRORAY_TX_WORK/services-before.tsv" "$BRORAY_TX_WORK/protected-source.manifest" \
                "$BRORAY_TX_WORK/protected-source-roots.list" "$BRORAY_TX_WORK/archive.manifest" \
                "$BRORAY_TX_WORK/source.members" "$BRORAY_TX_WORK/archive.members" \
                "$BRORAY_TX_WORK/live-source-tar.sha256" "$BRORAY_TX_WORK/archive-tar.sha256" \
                "$BRORAY_TX_WORK/source.manifest.sha256" "$BRORAY_TX_WORK/archive.manifest.sha256" \
                "$BRORAY_TX_WORK/snapshot.binding.verified" "$BRORAY_TX_WORK/snapshot.verified"
            printf '%s\n' yes >"$BRORAY_TX_WORK/evidence/test-sidecars-removed"
        fi
        if broray_tx_rollback; then broray_tx_rollback_result=verified; else broray_tx_rollback_result=failed; fi
    elif [ -f "$BRORAY_TX_WORK/services.stopped" ] && [ ! -L "$BRORAY_TX_WORK/services.stopped" ]; then
        if broray_tx_service_state_restore; then
            rm -f "$BRORAY_TX_WORK/services.stopped" || return 1
            printf '%s\n' restored >"$BRORAY_TX_WORK/pre-mutation-service-restore"
        else
            broray_tx_rollback_result=pre-mutation-service-restore-failed
        fi
    fi
    # Native OPKG exclusion is required during rollback and remains held
    # through terminal control retirement.  Only rollback-failed keeps the
    # forensic workspace, so that branch releases the native lock without
    # retiring custom control.
    # The synchronous parent intentionally retains its inherited FIFO writer
    # while the adopted candidate runs.  A child can therefore prove rollback
    # and write terminal evidence, but it must not attempt release: the parent
    # validates that evidence, closes its owning FD, and removes control state.
    # This compact TSV survives even when jq itself is the missing capability.
    # Values originate from validated ids and internal stage/reason tokens.
    printf 'operationId\t%s\nstage\t%s\nreason\t%s\nrollback\t%s\nmutationStarted\t%s\n' \
        "$BRORAY_TX_OPERATION_ID" "$broray_tx_failure_stage" "$broray_tx_failure_reason" \
        "$broray_tx_rollback_result" "$([ "$BRORAY_TX_MUTATED" -eq 1 ] && printf true || printf false)" \
        >"$BRORAY_TX_WORK/failure.tsv" 2>/dev/null || true
    # A missing jq is itself a supported pre-mutation capability failure.  Do
    # not let shell redirection create a zero-byte file named failure.json in
    # that path: TSV remains the fail-closed evidence, while JSON is committed
    # only after jq produced a complete document.
    broray_tx_failure_json_part="$BRORAY_TX_WORK/failure.json.part.$$"
    if jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" --arg stage "$broray_tx_failure_stage" --arg reason "$broray_tx_failure_reason" --arg rollback "$broray_tx_rollback_result" --arg snapshot "$BRORAY_TX_WORK/backup.tar.gz" --arg failedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
        '{schemaVersion:1,status:"FAIL",operationId:$operationId,stage:$stage,reason:$reason,rollback:$rollback,snapshot:$snapshot,failedAt:$failedAt}' >"$broray_tx_failure_json_part" 2>/dev/null
    then
        mv -f "$broray_tx_failure_json_part" "$BRORAY_TX_WORK/failure.json" 2>/dev/null || rm -f "$broray_tx_failure_json_part"
    else
        rm -f "$broray_tx_failure_json_part"
    fi
    case "$broray_tx_rollback_result" in
        failed|pre-mutation-service-restore-failed)
            printf '%s\n' rollback-failed >"$BRORAY_TX_WORK/outcome" 2>/dev/null || true
            jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" \
                --arg step "${BRORAY_TX_ROLLBACK_FAILURE_STEP:-unknown-rollback-step}" \
                '{schemaVersion:1,operationId:$operationId,step:$step,exitCode:1}' \
                >"$BRORAY_TX_WORK/rollback-failure.json.part" 2>/dev/null &&
                mv -f "$BRORAY_TX_WORK/rollback-failure.json.part" "$BRORAY_TX_WORK/rollback-failure.json" 2>/dev/null || true
            broray_tx_persistent_marker_phase_reach rollback-failed 2>/dev/null || true
            if [ "$BRORAY_TX_HANDOFF_CHILD" -ne 1 ]; then
                broray_tx_native_opkg_lock_release 2>/dev/null || true
            fi
            ;;
        *)
            printf '%s\n' "$([ "$broray_tx_rollback_result" = verified ] && printf rollback-verified || printf failed-before-mutation)" \
                >"$BRORAY_TX_WORK/outcome" 2>/dev/null || true
            ;;
    esac
    BRORAY_TX_STAGE="$broray_tx_failure_stage"; broray_tx_status failure "$broray_tx_failure_reason" 2>/dev/null || true
    broray_tx_failure_evidence="$BRORAY_TX_WORK"
    case "$broray_tx_rollback_result" in
        not-required|verified)
            if [ "$BRORAY_TX_HANDOFF_CHILD" -ne 1 ]; then
                broray_tx_failure_terminal_cleanup "$([ "$broray_tx_rollback_result" = verified ] && printf rollback-verified || printf failed-before-mutation)" || true
                [ -z "${BRORAY_TX_FAILURE_DURABLE:-}" ] || broray_tx_failure_evidence="$BRORAY_TX_FAILURE_DURABLE"
            fi
            ;;
    esac
    printf 'BROray FAIL: stage=%s reason=%s rollback=%s evidence=%s\n' "$broray_tx_failure_stage" "$broray_tx_failure_reason" "$broray_tx_rollback_result" "$broray_tx_failure_evidence" >&2
    BRORAY_TX_FAILURE_RUNNING=0
    return 1
}

broray_tx_success_commit()
{
    [ -f "$BRORAY_TX_WORK/registration.pass" ] && [ ! -L "$BRORAY_TX_WORK/registration.pass" ] || return 1
    [ "$(awk -F '\t' '$3=="postcheck-application-pass"{n++}END{print n+0}' "$BRORAY_TX_WORK/evidence/events.tsv")" -eq 4 ] || return 1
    [ "$(awk -F '\t' '$3=="postcheck-registered-pass"{n++}END{print n+0}' "$BRORAY_TX_WORK/evidence/events.tsv")" -eq 2 ] || return 1
    [ -f "$BRORAY_TX_WORK/candidate.ipk" ] && [ ! -L "$BRORAY_TX_WORK/candidate.ipk" ] || return 1
    [ -f "$BRORAY_TX_WORK/snapshot.verified" ] && [ ! -L "$BRORAY_TX_WORK/snapshot.verified" ] || return 1
    [ -e "$BRORAY_TX_OPERATION_ROOT" ] || mkdir -p "$BRORAY_TX_OPERATION_ROOT" || return 1
    [ -d "$BRORAY_TX_OPERATION_ROOT" ] && [ ! -L "$BRORAY_TX_OPERATION_ROOT" ] || return 1
    broray_tx_terminal_dir="$BRORAY_TX_OPERATION_ROOT/$BRORAY_TX_OPERATION_ID"
    if [ ! -e "$broray_tx_terminal_dir" ] && [ ! -L "$broray_tx_terminal_dir" ]; then
        mkdir "$broray_tx_terminal_dir" || return 1
        chmod 700 "$broray_tx_terminal_dir" 2>/dev/null || true
    fi
    [ -d "$broray_tx_terminal_dir" ] && [ ! -L "$broray_tx_terminal_dir" ] || return 1
    jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" --arg mode "$BRORAY_TX_MODE" \
        --arg targetVersion "$BRORAY_TX_TARGET_PACKAGE" \
        --arg candidateSha256 "$(broray_tx_sha "$BRORAY_TX_WORK/candidate.ipk")" \
        --arg snapshotSha256 "$(sed -n '1p' "$BRORAY_TX_WORK/snapshot.verified")" \
        --arg committedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" '
      {schemaVersion:1,status:"SUCCESS_COMMITTED",operationId:$operationId,mode:$mode,
       targetVersion:$targetVersion,candidateSha256:$candidateSha256,snapshotSha256:$snapshotSha256,
       applicationPasses:4,registeredPasses:2,cleanupComplete:false,committedAt:$committedAt}
    ' >"$broray_tx_terminal_dir/terminal.json.part" || return 1
    mv -f "$broray_tx_terminal_dir/terminal.json.part" "$broray_tx_terminal_dir/terminal.json" || return 1
    printf '%s\n' success-committed >"$BRORAY_TX_WORK/outcome.part" || return 1
    mv -f "$BRORAY_TX_WORK/outcome.part" "$BRORAY_TX_WORK/outcome" || return 1
    broray_tx_persistent_marker_phase_reach success-committed || return 1
    broray_tx_event success-committed || return 1
    broray_tx_trap_disable
    broray_tx_test_pause success-committed
}

# Return 1 only while rollback is still the correct response.  Once the
# terminal rename succeeds, success is authoritative; any later interruption
# returns 3 so the caller leaves the restored target intact for cleanup-only
# restart recovery.
broray_tx_restore_success_commit()
{
    [ "$BRORAY_TX_MODE" = restore ] || return 1
    broray_tx_native_opkg_lock_assert restore-success-commit || return 1
    [ -e "$BRORAY_TX_OPERATION_ROOT" ] || mkdir -p "$BRORAY_TX_OPERATION_ROOT" || return 1
    [ -d "$BRORAY_TX_OPERATION_ROOT" ] && [ ! -L "$BRORAY_TX_OPERATION_ROOT" ] || return 1
    broray_tx_restore_terminal_dir="$BRORAY_TX_OPERATION_ROOT/$BRORAY_TX_OPERATION_ID"
    if [ ! -e "$broray_tx_restore_terminal_dir" ] && [ ! -L "$broray_tx_restore_terminal_dir" ]; then
        mkdir "$broray_tx_restore_terminal_dir" || return 1
        chmod 700 "$broray_tx_restore_terminal_dir" 2>/dev/null || true
    fi
    [ -d "$broray_tx_restore_terminal_dir" ] && [ ! -L "$broray_tx_restore_terminal_dir" ] || return 1
    broray_tx_restore_terminal="$broray_tx_restore_terminal_dir/terminal.json"
    broray_tx_restore_terminal_part="$broray_tx_restore_terminal.part.$$"
    [ ! -e "$broray_tx_restore_terminal" ] && [ ! -L "$broray_tx_restore_terminal" ] || return 1
    [ ! -e "$broray_tx_restore_terminal_part" ] && [ ! -L "$broray_tx_restore_terminal_part" ] || return 1
    jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" \
        --arg targetVersion "$BRORAY_TX_TARGET_PACKAGE" \
        --arg snapshotSha256 "$(sed -n '1p' "$BRORAY_TX_WORK/snapshot.verified" 2>/dev/null)" \
        --arg restoreArchiveSha256 "$(broray_tx_sha "$BRORAY_TX_WORK/restore-input.tar.gz" 2>/dev/null)" \
        --arg restoreManifestSha256 "$(broray_tx_sha "$BRORAY_TX_WORK/restore-postcheck.manifest" 2>/dev/null)" \
        --arg committedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" '
      {schemaVersion:1,status:"SUCCESS_COMMITTED",operationId:$operationId,mode:"restore",
       targetVersion:$targetVersion,snapshotSha256:$snapshotSha256,
       restoreArchiveSha256:$restoreArchiveSha256,restoreManifestSha256:$restoreManifestSha256,
       restorePostchecks:2,cleanupComplete:false,committedAt:$committedAt}
    ' >"$broray_tx_restore_terminal_part" || {
        rm -f "$broray_tx_restore_terminal_part"; return 1;
    }
    chmod 600 "$broray_tx_restore_terminal_part" 2>/dev/null || true
    broray_tx_restore_success_terminal_validate "$broray_tx_restore_terminal_part" || {
        rm -f "$broray_tx_restore_terminal_part"; return 1;
    }

    # Disarm synchronous rollback immediately before the atomic commit point.
    # A signal before the rename leaves an incomplete mutation for recovery; a
    # signal after it can no longer undo a fully postchecked restore.
    broray_tx_trap_disable
    mv -f "$broray_tx_restore_terminal_part" "$broray_tx_restore_terminal" || {
        rm -f "$broray_tx_restore_terminal_part"; return 1;
    }
    broray_tx_restore_outcome_part="$BRORAY_TX_WORK/outcome.part.$$"
    printf '%s\n' success-committed >"$broray_tx_restore_outcome_part" || return 3
    mv -f "$broray_tx_restore_outcome_part" "$BRORAY_TX_WORK/outcome" || return 3
    broray_tx_persistent_marker_phase_reach success-committed || return 3
    broray_tx_event success-committed || return 3
    broray_tx_test_pause success-committed || return 3
    return 0
}

broray_tx_success_cleanup()
{
    broray_tx_event success-cleanup || return 1
    broray_tx_inject success-cleanup || return 1
    broray_tx_tmp_measure success-cleanup-before-removal || return 1
    broray_tx_audit_dir="$BRORAY_TX_STATE_ROOT/evidence"; mkdir -p "$broray_tx_audit_dir" || return 1
    [ -d "$broray_tx_audit_dir" ] && [ ! -L "$broray_tx_audit_dir" ] || return 1
    awk -F '\t' '{print $3}' "$BRORAY_TX_WORK/evidence/events.tsv" | jq -Rsc 'split("\n")[:-1]' >"$BRORAY_TX_WORK/events.json" || return 1
    jq -Rn '[inputs | split("\t") | {sequence:(.[0]|tonumber),timestamp:.[1],event:.[2]}]' <"$BRORAY_TX_WORK/evidence/events.tsv" >"$BRORAY_TX_WORK/event-timeline.json" || return 1
    broray_tx_success_candidate_sha=""
    if [ -f "$BRORAY_TX_WORK/candidate.ipk" ] && [ ! -L "$BRORAY_TX_WORK/candidate.ipk" ]; then
        broray_tx_success_candidate_sha="$(broray_tx_sha "$BRORAY_TX_WORK/candidate.ipk")"
    fi
    broray_tx_success_restore_sha=""
    if [ -f "$BRORAY_TX_WORK/restore-input.tar.gz" ] && [ ! -L "$BRORAY_TX_WORK/restore-input.tar.gz" ]; then
        broray_tx_success_restore_sha="$(broray_tx_sha "$BRORAY_TX_WORK/restore-input.tar.gz")"
    fi
    broray_tx_durable_audit_candidate="$BRORAY_TX_WORK/durable-audit.json"
    broray_tx_bounded_command "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" \
        "$broray_tx_durable_audit_candidate" "$BRORAY_TX_WORK/evidence/durable-audit.stderr" 0 \
        jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" --arg mode "$BRORAY_TX_MODE" --arg releaseId "$BRORAY_TX_TARGET_RELEASE" \
        --arg snapshotSha256 "$(cat "$BRORAY_TX_WORK/snapshot.verified")" --arg candidateSha256 "$broray_tx_success_candidate_sha" \
        --arg restoreArchiveSha256 "$broray_tx_success_restore_sha" \
        --arg snapshotLocation "$BRORAY_TX_WORK/backup.tar.gz" --arg sourceManifestSha256 "$(cat "$BRORAY_TX_WORK/source.manifest.sha256")" \
        --arg archiveManifestSha256 "$(cat "$BRORAY_TX_WORK/archive.manifest.sha256")" --argjson snapshotSizeBytes "$(wc -c <"$BRORAY_TX_WORK/backup.tar.gz" | tr -d ' ')" \
        --arg completedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" --argjson events "$(cat "$BRORAY_TX_WORK/events.json")" \
        --argjson eventTimeline "$(cat "$BRORAY_TX_WORK/event-timeline.json")" --argjson spacePlanner "$(cat "$BRORAY_TX_WORK/evidence/space.json")" \
        --argjson cleanupAudit "$(cat "$BRORAY_TX_WORK/evidence/cleanup.json")" \
        --argjson runtimeCapabilities "$(cat "$BRORAY_TX_WORK/evidence/capabilities.json")" '
        {schemaVersion:2,status:"PASS",operationId:$operationId,mode:$mode,releaseId:$releaseId,
         snapshotVerified:true,snapshotRemovedAfterSuccess:true,snapshotSha256:$snapshotSha256,
         snapshotLocation:$snapshotLocation,snapshotSizeBytes:$snapshotSizeBytes,snapshotRegularFile:true,
         snapshotSymlink:false,snapshotTarReadable:true,snapshotTraversalFree:true,snapshotManifestMatches:true,
         snapshotIntegrityPassed:true,sourceManifestSha256:$sourceManifestSha256,archiveManifestSha256:$archiveManifestSha256,
         candidateSha256:(if $candidateSha256=="" then null else $candidateSha256 end),
         restoreArchiveSha256:(if $restoreArchiveSha256=="" then null else $restoreArchiveSha256 end),
         doublePostcheck:true,historicalStateRequired:false,
         fullSnapshotStreamedDirectlyToGzip:true,uncompressedSourceCopiesInTmp:0,
         cleanupAudit:$cleanupAudit,runtimeCapabilities:$runtimeCapabilities,
         spacePlanner:$spacePlanner,events:$events,eventTimeline:$eventTimeline,completedAt:$completedAt}
    ' || broray_tx_fail durable-evidence-bounded-writer-failed || return 1
    broray_tx_file_cap_kb "$broray_tx_durable_audit_candidate" "$BRORAY_TX_DURABLE_EVIDENCE_CAP_KB" ||
        broray_tx_fail durable-evidence-cap-exceeded || return 1
    cp -p "$broray_tx_durable_audit_candidate" "$broray_tx_audit_dir/$BRORAY_TX_OPERATION_ID.json.part" || return 1
    mv -f "$broray_tx_audit_dir/$BRORAY_TX_OPERATION_ID.json.part" "$broray_tx_audit_dir/$BRORAY_TX_OPERATION_ID.json" || return 1
    broray_tx_trap_disable
    broray_tx_terminal_control_retire || return 1
    printf 'BROray %s: PASS\n' "$BRORAY_TX_TARGET_PACKAGE"
}

broray_tx_opkg_prepare()
{
    printf '%s\n' 'BROray: direct OPKG mutation is disabled; use WebUI or the universal updater.' >&2
    return 42
}

broray_tx_opkg_adopt()
{
    broray_tx_opkg_prepare
}

broray_tx_opkg_finalize()
{
    broray_tx_opkg_prepare
}

broray_tx_run_install()
{
    broray_tx_run_id="$1"; broray_tx_run_mode="${2:-update}"; broray_tx_run_metadata="${3:-}"
    BRORAY_TX_ORIGIN=manual
    broray_tx_work_init "$broray_tx_run_id" "$broray_tx_run_mode" || return 1
    # Test-only interruption point immediately after the bounded control
    # prelude.  No capability evidence, cleanup, snapshot, or service action
    # exists yet, so restart recovery must classify from operation/marker
    # binding alone.
    broray_tx_test_pause control-prelude || return 1
    if ! broray_tx_prepare_application "$broray_tx_run_metadata"; then
        # The adopted candidate engine may already have completed its one and
        # only rollback attempt.  It cannot release the FIFO while this parent
        # owns another writer FD.  Validate exact terminal evidence, reclaim
        # lock ownership in the parent, and release from here without starting
        # a second rollback cycle.
        if [ -f "$BRORAY_TX_WORK/failure.json" ] && [ ! -L "$BRORAY_TX_WORK/failure.json" ] &&
           [ -f "$BRORAY_TX_WORK/failure.tsv" ] && [ ! -L "$BRORAY_TX_WORK/failure.tsv" ] &&
           jq -e --arg id "$BRORAY_TX_OPERATION_ID" \
              '.status=="FAIL" and .operationId==$id and (.rollback=="verified" or .rollback=="failed")' \
              "$BRORAY_TX_WORK/failure.json" >/dev/null 2>&1
        then
            broray_tx_parent_child_rollback="$(jq -r '.rollback' "$BRORAY_TX_WORK/failure.json")"
            broray_tx_parent_child_outcome="$(sed -n '1p' "$BRORAY_TX_WORK/outcome" 2>/dev/null)"
            case "$broray_tx_parent_child_rollback:$broray_tx_parent_child_outcome" in
                verified:rollback-verified)
                    [ -f "$BRORAY_TX_WORK/rollback.verified" ] && [ ! -L "$BRORAY_TX_WORK/rollback.verified" ] || return 1
                    broray_tx_lock_adopt || return 1
                    broray_tx_rollback_result=verified
                    broray_tx_failure_terminal_cleanup rollback-verified || return 1
                    [ ! -e "$BRORAY_TX_LOCK_DIR" ] && [ ! -L "$BRORAY_TX_LOCK_DIR" ] || return 1
                    [ ! -e "$BRORAY_TX_WORK" ] && [ ! -L "$BRORAY_TX_WORK" ] || return 1
                    ;;
                failed:rollback-failed)
                    broray_tx_lock_adopt || return 1
                    broray_tx_native_opkg_lock_release || return 1
                    BRORAY_TX_LOCK_HELD=0
                    ;;
                *) return 1 ;;
            esac
            broray_tx_trap_disable
            return 1
        fi
        if [ -f "$BRORAY_TX_WORK/rollback.verified" ] && [ ! -L "$BRORAY_TX_WORK/rollback.verified" ]; then
            # SIGKILL may land after both source postchecks but before the
            # adopted child commits failure.json.  This is still a terminal
            # rollback-verified phase and must never cause a second rollback.
            printf '%s\n' rollback-verified >"$BRORAY_TX_WORK/outcome" || return 1
            broray_tx_native_opkg_lock_release || return 1
            BRORAY_TX_LOCK_HELD=0
            broray_tx_trap_disable
            broray_tx_recover_stale_control || return 1
            return 1
        fi
        [ -d "$BRORAY_TX_WORK" ] && broray_tx_failure
        return 1
    fi
    broray_tx_handoff_adopt || return 1
    broray_tx_test_pause before-registration || { broray_tx_failure; return 1; }
    broray_tx_registration_commit || { broray_tx_fail metadata-registration-failed; broray_tx_failure; return 1; }
    if ! broray_tx_postcheck_registered || ! broray_tx_postcheck_registered; then broray_tx_failure; return 1; fi
    printf '%s\n' yes >"$BRORAY_TX_WORK/registration.pass" || return 1
    broray_tx_success_commit || { broray_tx_fail success-terminal-commit-failed; broray_tx_failure; return 1; }
    if ! broray_tx_success_cleanup; then
        [ -n "$BRORAY_TX_REASON" ] || broray_tx_fail success-cleanup-failed
        broray_tx_status interrupted "$BRORAY_TX_REASON" 2>/dev/null || true
        printf 'BROray INTERRUPTED after success commit: reason=%s evidence=%s\n' "$BRORAY_TX_REASON" "$BRORAY_TX_WORK" >&2
        return 1
    fi
}

broray_tx_restore_last_user_backup()
{
    broray_tx_restore_id="$1"
    broray_tx_restore_archive="${2:-}"
    BRORAY_TX_ORIGIN=manual
    broray_tx_work_init "$broray_tx_restore_id" restore || return 1
    if [ "$BRORAY_TX_SOURCE_PACKAGE" != "$BRORAY_TX_TARGET_PACKAGE" ] ||
       [ "$BRORAY_TX_SOURCE_APP" != "$BRORAY_TX_TARGET_APP" ]
    then
        broray_tx_fail restore-requires-current-target
        broray_tx_failure
        return 1
    fi
    if ! broray_tx_cleanup || ! broray_tx_capability_preflight || ! broray_tx_source_functional_verify ||
       ! broray_tx_release_metadata || ! broray_tx_operation_relation_verify pre-snapshot || ! broray_tx_space_check ||
       ! broray_tx_snapshot_create || ! broray_tx_snapshot_verify ||
       ! broray_tx_restore_archive_prepare "$broray_tx_restore_archive" ||
       ! broray_tx_restore_apply || ! broray_tx_restore_postcheck || ! broray_tx_restore_postcheck
    then
        broray_tx_failure
        return 1
    fi
    broray_tx_restore_success_commit
    broray_tx_restore_commit_rc=$?
    case "$broray_tx_restore_commit_rc" in
        0) ;;
        3)
            BRORAY_TX_REASON=restore-success-commit-interrupted
            broray_tx_status interrupted "$BRORAY_TX_REASON" 2>/dev/null || true
            printf 'BROray INTERRUPTED after restore success commit: reason=%s evidence=%s\n' \
                "$BRORAY_TX_REASON" "$BRORAY_TX_WORK" >&2
            return 1
            ;;
        *)
            broray_tx_fail restore-success-terminal-commit-failed
            broray_tx_failure
            return 1
            ;;
    esac
    if ! broray_tx_success_cleanup; then
        [ -n "$BRORAY_TX_REASON" ] || broray_tx_fail restore-success-cleanup-failed
        broray_tx_status interrupted "$BRORAY_TX_REASON" 2>/dev/null || true
        printf 'BROray INTERRUPTED after restore success commit: reason=%s evidence=%s\n' \
            "$BRORAY_TX_REASON" "$BRORAY_TX_WORK" >&2
        return 1
    fi
}

# Execute the exact released metadata and package validators without touching
# installed state or transaction control.  This deliberately does not recover
# or acquire the global lock; the following normal updater owns that action.
broray_tx_verify_only()
{
    broray_tx_verify_id="${1:-verify-$(date '+%s')-$$}"
    broray_tx_valid_id "$broray_tx_verify_id" || return 2
    BRORAY_TX_OPERATION_ID="$broray_tx_verify_id"
    BRORAY_TX_MODE=verify-only
    BRORAY_TX_WORK="$BRORAY_TX_TMP_BASE/broray-verify-$broray_tx_verify_id"
    [ -d "$BRORAY_TX_TMP_BASE" ] && [ ! -L "$BRORAY_TX_TMP_BASE" ] && [ -w "$BRORAY_TX_TMP_BASE" ] || return 2
    [ ! -e "$BRORAY_TX_WORK" ] && [ ! -L "$BRORAY_TX_WORK" ] || return 2
    mkdir -p "$BRORAY_TX_WORK/evidence" || return 2
    chmod 700 "$BRORAY_TX_WORK" "$BRORAY_TX_WORK/evidence" 2>/dev/null || true
    : >"$BRORAY_TX_WORK/evidence/events.tsv" || return 2
    printf '%s\n' "$BRORAY_TX_OPERATION_ID" >"$BRORAY_TX_WORK/operation-id" || return 2
    broray_runtime_probe_cleanup "$BRORAY_TX_WORK" || { broray_tx_fail "verify-capability-failed:${BRORAY_RUNTIME_FAILURE_ID:-unknown}"; return 1; }
    broray_runtime_resolve_release_tools || return 1
    broray_runtime_activate_if_resolved || return 1
    broray_tx_release_metadata "" || return 1
    broray_tx_verify_url="$(jq -r '.baseUrl + "/" + .filename' "$BRORAY_TX_WORK/candidate.json")" || return 1
    broray_tx_fetch "$broray_tx_verify_url" "$BRORAY_TX_WORK/candidate.ipk.part" "$(jq -r '.sizeBytes' "$BRORAY_TX_WORK/candidate.json")" \
        >"$BRORAY_TX_WORK/evidence/candidate-download.stdout" 2>"$BRORAY_TX_WORK/evidence/candidate-download.stderr" || return 1
    [ "$(broray_tx_sha "$BRORAY_TX_WORK/candidate.ipk.part")" = "$(jq -r '.sha256' "$BRORAY_TX_WORK/candidate.json")" ] || return 1
    [ "$(wc -c <"$BRORAY_TX_WORK/candidate.ipk.part" | tr -d ' ')" = "$(jq -r '.sizeBytes' "$BRORAY_TX_WORK/candidate.json")" ] || return 1
    mv -f "$BRORAY_TX_WORK/candidate.ipk.part" "$BRORAY_TX_WORK/candidate.ipk" || return 1
    broray_tx_candidate_structure_verify "$BRORAY_TX_WORK/candidate.ipk" || return 1
    jq -nc --arg operationId "$BRORAY_TX_OPERATION_ID" --arg releaseId "$BRORAY_TX_TARGET_RELEASE" \
        --arg candidateSha256 "$(broray_tx_sha "$BRORAY_TX_WORK/candidate.ipk")" \
        --arg candidateMetadataSha256 "$(broray_tx_sha "$BRORAY_TX_WORK/candidate.json")" \
        --arg completedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" '
      {schemaVersion:1,status:"PASS",mode:"verify-only",operationId:$operationId,
       releaseId:$releaseId,candidateSha256:$candidateSha256,
       candidateMetadataSha256:$candidateMetadataSha256,mutationStarted:false,
       lockTouched:false,recoveryPerformed:false,completedAt:$completedAt}
    ' >"$BRORAY_TX_WORK/verification.json" || return 1
    printf '%s\n' yes >"$BRORAY_TX_WORK/.broray-disposable" || return 1
    printf 'BROray %s VERIFY-ONLY PASS: evidence=%s\n' "$BRORAY_TX_TARGET_PACKAGE" "$BRORAY_TX_WORK"
}

# Compatibility API names refer only to this operation's full snapshot.
broray_tx_capture_user_state() { broray_tx_snapshot_create; }
broray_tx_backup_verify() { broray_tx_snapshot_verify; }
broray_tx_restore_after_install() { broray_tx_migrate_user_state; }

# This file is a source-only compatibility library.  Candidate 14 lifecycle
# mutations belong exclusively to the persistent updater-v5 control plane;
# the former standalone transaction CLI is retired and must never mutate the
# installation when the library is invoked directly.
broray_tx_cli()
{
    printf '%s\n' 'package-transaction.sh: retired source-only library; use broray-updaterctl' >&2
    return 2
}

case "${0##*/}" in
    package-transaction.sh)
        broray_tx_cli "$@"
        exit 2
        ;;
esac
