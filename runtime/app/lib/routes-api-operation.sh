#!/opt/bin/ash

# Serialises every conflicting WebUI operation involving routes. A resumable
# operation remains an exclusive logical operation even after its process has
# stopped, so no other card or BROray maintenance task may start until it is
# resumed and completed (or explicitly handled by the same bundle).

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_ROUTES_API_LOCK="${BRORAY_ROUTES_API_LOCK:-/opt/var/lock/broray/global-operation.lock}"
BRORAY_ROUTES_API_PROGRESS_DIR="${BRORAY_ROUTES_API_PROGRESS_DIR:-$BRORAY_ROUTES_ROOT/operations}"
BRORAY_ROUTES_API_LOCK_HELD="${BRORAY_ROUTES_API_LOCK_HELD:-false}"
BRORAY_UPDATER_REQUEST_LOCK="${BRORAY_UPDATER_REQUEST_LOCK:-/opt/var/lib/broray-updater/request.lock}"
BRORAY_UPDATER_OPERATION_POINTER="${BRORAY_UPDATER_OPERATION_POINTER:-/opt/var/lib/broray/last-operation}"
BRORAY_UPDATER_OPERATION_ROOT="${BRORAY_UPDATER_OPERATION_ROOT:-/opt/var/lib/broray/operations}"
BRORAY_LEGACY_GLOBAL_LOCK="${BRORAY_LEGACY_GLOBAL_LOCK:-/tmp/broray-global-operation.lock}"
BRORAY_ROUTES_API_STALE_LOCK_ROOT="${BRORAY_ROUTES_API_STALE_LOCK_ROOT:-/opt/var/lib/broray/stale-locks}"
BRORAY_ROUTES_API_PENDING_BUNDLE=""
BRORAY_ROUTES_API_PENDING_OPERATION=""
BRORAY_ROUTES_API_PENDING_STARTED=""
BRORAY_ROUTES_API_PENDING_UPDATED=""

broray_routes_api_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_api_is_pid()
{
    case "${1:-}" in
        ''|*[!0-9]*) return 1 ;;
    esac
    return 0
}

broray_routes_api_pending_find()
{
    local file values old_ifs tab bundle operation started updated

    BRORAY_ROUTES_API_PENDING_BUNDLE=""
    BRORAY_ROUTES_API_PENDING_OPERATION=""
    BRORAY_ROUTES_API_PENDING_STARTED=""
    BRORAY_ROUTES_API_PENDING_UPDATED=""
    [ -d "$BRORAY_ROUTES_API_PROGRESS_DIR" ] || return 1

    tab="$(printf '\t')"
    for file in "$BRORAY_ROUTES_API_PROGRESS_DIR"/*.json; do
        [ -f "$file" ] && [ ! -L "$file" ] || continue
        values="$(
            jq -r '
                select(
                    type == "object" and
                    .kind == "routes" and
                    .running == false and
                    .resumable == true and
                    (.bundleId | type) == "string" and
                    (.bundleId | length) > 0
                ) |
                [
                    .bundleId,
                    (.operation // "operation"),
                    (.startedAt // ""),
                    (.updatedAt // "")
                ] | @tsv
            ' "$file" 2>/dev/null
        )" || values=""
        [ -n "$values" ] || continue

        old_ifs="$IFS"
        IFS="$tab"
        read -r bundle operation started updated <<EOF_PENDING
$values
EOF_PENDING
        IFS="$old_ifs"

        case "$bundle" in
            ''|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
                continue
                ;;
        esac
        BRORAY_ROUTES_API_PENDING_BUNDLE="$bundle"
        BRORAY_ROUTES_API_PENDING_OPERATION="$operation"
        BRORAY_ROUTES_API_PENDING_STARTED="$started"
        BRORAY_ROUTES_API_PENDING_UPDATED="$updated"
        return 0
    done
    return 1
}

broray_routes_api_pending_action_allowed()
{
    local action bundle
    action="${1:-}"
    bundle="${2:-}"

    case "$action" in
        custom:list)
            return 0
            ;;
        preflight:resume|resume)
            [ -n "$BRORAY_ROUTES_API_PENDING_BUNDLE" ] &&
                [ "$bundle" = "$BRORAY_ROUTES_API_PENDING_BUNDLE" ]
            return $?
            ;;
    esac
    return 1
}

broray_routes_api_lock_write()
{
    local action bundle
    action="${1:-unknown}"
    bundle="${2:-}"

    printf '%s\n' "$$" >"$BRORAY_ROUTES_API_LOCK/pid" || return 1
    printf '%s\n' routes >"$BRORAY_ROUTES_API_LOCK/scope" || return 1
    printf '%s\n' "$action" >"$BRORAY_ROUTES_API_LOCK/action" || return 1
    printf '%s\n' "$bundle" >"$BRORAY_ROUTES_API_LOCK/bundle" || return 1
    printf '%s\n' "$(broray_routes_api_now)" >"$BRORAY_ROUTES_API_LOCK/startedAt" || return 1
    return 0
}

broray_routes_api_stale_action_known()
{
    case "${1:-}" in
        check|download|verify|plan|custom:*|preflight:*|xray:*|subscriptions:*|keenetic:*|servers:*|dot:*)
            return 0
            ;;
    esac
    return 1
}

broray_routes_api_lock_reclaim_stale()
{
    # A five-file record cannot prove boot, process birth or child completion.
    # Preserve it for explicit legacy/domain recovery even if its PID is absent.
    return 1
}

broray_routes_api_lock_acquire()
{
    local action bundle owner updater_operation updater_state
    action="${1:-unknown}"
    bundle="${2:-}"

    { [ ! -e "$BRORAY_UPDATER_REQUEST_LOCK" ] && [ ! -L "$BRORAY_UPDATER_REQUEST_LOCK" ]; } || return 2
    { [ ! -e "$BRORAY_LEGACY_GLOBAL_LOCK" ] && [ ! -L "$BRORAY_LEGACY_GLOBAL_LOCK" ]; } || return 2
    updater_operation="$(sed -n '1p' "$BRORAY_UPDATER_OPERATION_POINTER" 2>/dev/null || true)"
    case "$updater_operation" in
        ''|.*|-*|*[!A-Za-z0-9._-]*) ;;
        *)
            updater_state="$BRORAY_UPDATER_OPERATION_ROOT/$updater_operation/state.json"
            if [ -s "$updater_state" ] && jq -e '.running == true' "$updater_state" >/dev/null 2>&1; then
                return 2
            fi
            ;;
    esac

    if broray_routes_api_pending_find; then
        broray_routes_api_pending_action_allowed "$action" "$bundle" || return 2
    fi

    mkdir -p "$(dirname "$BRORAY_ROUTES_API_LOCK")" || return 1
    if mkdir "$BRORAY_ROUTES_API_LOCK" 2>/dev/null; then
        if [ -e "$BRORAY_UPDATER_REQUEST_LOCK" ] || [ -L "$BRORAY_UPDATER_REQUEST_LOCK" ] ||
           [ -e "$BRORAY_LEGACY_GLOBAL_LOCK" ] || [ -L "$BRORAY_LEGACY_GLOBAL_LOCK" ]
        then
            rmdir "$BRORAY_ROUTES_API_LOCK" 2>/dev/null || true
            return 2
        fi
        broray_routes_api_lock_write "$action" "$bundle" || {
            rm -rf "$BRORAY_ROUTES_API_LOCK" 2>/dev/null || true
            return 1
        }
        BRORAY_ROUTES_API_LOCK_HELD=true
        return 0
    fi

    # A present fence without our already-published identity is foreign or
    # ambiguous.  Never reclaim it here: the owner may be between atomic
    # mkdir and publication of its pid/scope files.
    return 2
}

broray_routes_api_lock_release()
{
    local owner scope
    [ "$BRORAY_ROUTES_API_LOCK_HELD" = true ] || return 0
    owner="$(sed -n '1p' "$BRORAY_ROUTES_API_LOCK/pid" 2>/dev/null || true)"
    scope="$(sed -n '1p' "$BRORAY_ROUTES_API_LOCK/scope" 2>/dev/null || true)"
    if [ "$owner" = "$$" ] && [ "$scope" = routes ]; then
        rm -rf "$BRORAY_ROUTES_API_LOCK" 2>/dev/null || true
    fi
    BRORAY_ROUTES_API_LOCK_HELD=false
}

broray_routes_api_lock_read_json()
{
    local owner scope action bundle started active stale

    if [ -d "$BRORAY_ROUTES_API_LOCK" ]; then
        owner="$(sed -n '1p' "$BRORAY_ROUTES_API_LOCK/pid" 2>/dev/null || true)"
        scope="$(sed -n '1p' "$BRORAY_ROUTES_API_LOCK/scope" 2>/dev/null || true)"
        action="$(sed -n '1p' "$BRORAY_ROUTES_API_LOCK/action" 2>/dev/null || true)"
        bundle="$(sed -n '1p' "$BRORAY_ROUTES_API_LOCK/bundle" 2>/dev/null || true)"
        started="$(sed -n '1p' "$BRORAY_ROUTES_API_LOCK/startedAt" 2>/dev/null || true)"
        active=false
        stale=true
        if broray_routes_api_is_pid "$owner" && kill -0 "$owner" 2>/dev/null; then
            active=true
            stale=false
        fi
        if [ "$active" = true ]; then
            case "$owner" in ''|*[!0-9]*) owner=0 ;; esac
            jq -n \
                --argjson pid "$owner" \
                --arg scope "$scope" \
                --arg action "$action" \
                --arg bundleId "$bundle" \
                --arg startedAt "$started" '
                {
                    active:true,
                    pending:false,
                    resumable:false,
                    pid:(if $pid == 0 then null else $pid end),
                    scope:(if $scope == "" then null else $scope end),
                    action:(if $action == "" then null else $action end),
                    bundleId:(if $bundleId == "" then null else $bundleId end),
                    startedAt:(if $startedAt == "" then null else $startedAt end),
                    updatedAt:null,
                    stale:false
                }
            '
            return 0
        fi
    else
        owner=""
        scope=""
        action=""
        bundle=""
        started=""
        stale=false
    fi

    if broray_routes_api_pending_find; then
        jq -n \
            --arg bundleId "$BRORAY_ROUTES_API_PENDING_BUNDLE" \
            --arg operation "$BRORAY_ROUTES_API_PENDING_OPERATION" \
            --arg startedAt "$BRORAY_ROUTES_API_PENDING_STARTED" \
            --arg updatedAt "$BRORAY_ROUTES_API_PENDING_UPDATED" '
            {
                active:true,
                pending:true,
                resumable:true,
                pid:null,
                scope:"routes",
                action:"resume-required",
                operation:(if $operation == "" then null else $operation end),
                bundleId:$bundleId,
                startedAt:(if $startedAt == "" then null else $startedAt end),
                updatedAt:(if $updatedAt == "" then null else $updatedAt end),
                stale:false
            }
        '
        return 0
    fi

    case "$owner" in ''|*[!0-9]*) owner=0 ;; esac
    jq -n \
        --argjson pid "$owner" \
        --arg scope "$scope" \
        --arg action "$action" \
        --arg bundleId "$bundle" \
        --arg startedAt "$started" \
        --argjson stale "${stale:-false}" '
        {
            active:false,
            pending:false,
            resumable:false,
            pid:(if $pid == 0 then null else $pid end),
            scope:(if $scope == "" then null else $scope end),
            action:(if $action == "" then null else $action end),
            bundleId:(if $bundleId == "" then null else $bundleId end),
            startedAt:(if $startedAt == "" then null else $startedAt end),
            updatedAt:null,
            stale:$stale
        }
    '
}
