#!/opt/bin/ash

# BROray 3.0.0 operation storage.
#
# Operation evidence must survive replacement of /opt/broray during an OPKG
# reinstall/update.  Therefore the authoritative state lives outside the
# application tree under /opt/var/lib/broray.  Files under /opt/broray/run are
# compatibility mirrors only and may disappear while a transaction commits.

BRORAY_APPLICATION_ROOT="${BRORAY_APPLICATION_ROOT:-${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}}"
BRORAY_STATE_ROOT="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
BRORAY_OPERATION_ROOT="${BRORAY_OPERATION_ROOT:-$BRORAY_STATE_ROOT/operations}"
BRORAY_OPERATION_POINTER="${BRORAY_OPERATION_POINTER:-$BRORAY_STATE_ROOT/last-operation}"
BRORAY_OPERATION_LEGACY_ROOT="${BRORAY_OPERATION_LEGACY_ROOT:-$BRORAY_APPLICATION_ROOT/run/broray}"
BRORAY_OPERATION_KEEP="${BRORAY_OPERATION_KEEP:-20}"

if [ -f "$BRORAY_APPLICATION_ROOT/lib/operation-client.sh" ] && [ ! -L "$BRORAY_APPLICATION_ROOT/lib/operation-client.sh" ]; then
    . "$BRORAY_APPLICATION_ROOT/lib/operation-client.sh"
fi

broray_operation_valid_id()
{
    case "${1:-}" in
        ''|*[!A-Za-z0-9._-]*) return 1 ;;
    esac
    return 0
}

broray_operation_prepare_root()
{
    # Never create anything below /opt/broray here.  During a transactional
    # reinstall that tree is intentionally absent for a short period; creating
    # the legacy directory would break the atomic replacement.
    mkdir -p \
        "$BRORAY_OPERATION_ROOT" \
        "$(dirname "$BRORAY_OPERATION_POINTER")" || return 1

    chmod 700 \
        "$BRORAY_OPERATION_ROOT" \
        "$(dirname "$BRORAY_OPERATION_POINTER")" 2>/dev/null || true
}

broray_operation_legacy_available()
{
    [ -d "$BRORAY_APPLICATION_ROOT" ] || return 1
    mkdir -p "$BRORAY_OPERATION_LEGACY_ROOT" 2>/dev/null || return 1
    chmod 700 "$BRORAY_OPERATION_LEGACY_ROOT" 2>/dev/null || true
}

broray_operation_dir()
{
    operation_id="$1"
    broray_operation_valid_id "$operation_id" || return 1
    printf '%s/%s\n' "$BRORAY_OPERATION_ROOT" "$operation_id"
}

broray_operation_publish_pointer()
{
    operation_id="$1"
    broray_operation_valid_id "$operation_id" || return 1
    broray_operation_prepare_root || return 1

    pointer_tmp="$BRORAY_OPERATION_POINTER.tmp.$$"
    legacy_pointer="$BRORAY_OPERATION_LEGACY_ROOT/last-operation"
    legacy_tmp="$legacy_pointer.tmp.$$"

    printf '%s\n' "$operation_id" >"$pointer_tmp" || {
        rm -f "$pointer_tmp"
        return 1
    }
    mv -f "$pointer_tmp" "$BRORAY_OPERATION_POINTER" || {
        rm -f "$pointer_tmp"
        return 1
    }

    # The legacy pointer is best-effort only.  It must never recreate the
    # application tree while a transaction has moved it away.
    if broray_operation_legacy_available &&
       printf '%s\n' "$operation_id" >"$legacy_tmp" 2>/dev/null
    then
        mv -f "$legacy_tmp" "$legacy_pointer" 2>/dev/null || rm -f "$legacy_tmp"
    fi
}

broray_operation_select()
{
    operation_id="$1"
    operation_dir="$(broray_operation_dir "$operation_id")" || return 1
    broray_operation_prepare_root || return 1
    mkdir -p "$operation_dir" || return 1
    chmod 700 "$operation_dir" 2>/dev/null || true
    broray_operation_publish_pointer "$operation_id" || return 1

    BRORAY_CURRENT_OPERATION_ID="$operation_id"
    BRORAY_CURRENT_OPERATION_DIR="$operation_dir"
    BRORAY_CURRENT_OPERATION_STATUS="$operation_dir/state.json"
    BRORAY_CURRENT_OPERATION_LOG="$operation_dir/log.txt"
    BRORAY_CURRENT_OPERATION_RESULT="$operation_dir/result.json"
    export BRORAY_CURRENT_OPERATION_ID BRORAY_CURRENT_OPERATION_DIR
    export BRORAY_CURRENT_OPERATION_STATUS BRORAY_CURRENT_OPERATION_LOG BRORAY_CURRENT_OPERATION_RESULT
}

broray_operation_select_last()
{
    [ -r "$BRORAY_OPERATION_POINTER" ] || return 1
    operation_id="$(sed -n '1p' "$BRORAY_OPERATION_POINTER" 2>/dev/null)"
    broray_operation_valid_id "$operation_id" || return 1
    [ -d "$BRORAY_OPERATION_ROOT/$operation_id" ] || return 1
    broray_operation_select "$operation_id"
}

broray_operation_write_result()
{
    operation_id="$1"
    result_json="$2"
    operation_dir="$(broray_operation_dir "$operation_id")" || return 1
    [ -d "$operation_dir" ] || return 1
    printf '%s\n' "$result_json" | jq -e 'type == "object"' >/dev/null 2>&1 || return 1

    result_tmp="$operation_dir/result.json.tmp.$$"
    printf '%s\n' "$result_json" >"$result_tmp" || {
        rm -f "$result_tmp"
        return 1
    }
    chmod 600 "$result_tmp" 2>/dev/null || true
    mv -f "$result_tmp" "$operation_dir/result.json"
}

broray_operation_result_from_state()
{
    operation_id="$1"
    operation_dir="$(broray_operation_dir "$operation_id")" || return 1
    state_file="$operation_dir/state.json"
    [ -s "$state_file" ] || return 1

    jq -c '{
        schemaVersion:1,
        operationId:(.operationId // null),
        operation:(.operation // null),
        state:(.state // "unknown"),
        stage:(.stage // null),
        progress:(.progress // 0),
        message:(.message // null),
        error:(.error // null),
        success:((.state // "") == "success"),
        finishedAt:(.updatedAt // null)
    }' "$state_file" 2>/dev/null
}

broray_operation_finalize_from_state()
{
    operation_id="$1"
    result_json="$(broray_operation_result_from_state "$operation_id")" || return 1
    broray_operation_write_result "$operation_id" "$result_json"
}

broray_operation_prune()
{
    broray_operation_prepare_root || return 1
    keep="$BRORAY_OPERATION_KEEP"
    case "$keep" in ''|*[!0-9]*) keep=20 ;; esac
    [ "$keep" -ge 1 ] || keep=1

    current=""
    [ -r "$BRORAY_OPERATION_POINTER" ] && current="$(sed -n '1p' "$BRORAY_OPERATION_POINTER" 2>/dev/null)"

    # IDs begin with a sortable UTC timestamp.  The fallback sort by path keeps
    # deterministic behaviour for legacy IDs too.
    find "$BRORAY_OPERATION_ROOT" -mindepth 1 -maxdepth 1 -type d -print 2>/dev/null |
        sort -r |
        awk -v keep="$keep" 'NR > keep {print}' |
        while IFS= read -r old_dir; do
            [ -n "$old_dir" ] || continue
            old_id="${old_dir##*/}"
            [ "$old_id" = "$current" ] && continue
            # Background records and ambiguous/corrupt records are retained.
            # Only the serialized background coordinator can prune its owners.
            [ -f "$old_dir/state.json" ] && [ ! -L "$old_dir/state.json" ] || continue
            [ ! -e "$old_dir/owner.json" ] && [ ! -L "$old_dir/owner.json" ] || continue
            jq -e 'type=="object" and .kind!="background" and .running==false' "$old_dir/state.json" >/dev/null 2>&1 || continue
            if [ -s "$old_dir/state.json" ] &&
               jq -e '.running == true' "$old_dir/state.json" >/dev/null 2>&1
            then
                continue
            fi
            rm -rf "$old_dir"
        done
}
