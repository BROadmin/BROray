#!/opt/bin/ash

# Serialises WebUI route actions across all route cards. This lock is separate
# from the low-level operation.lock used by CLI modules, so one Web request may
# safely run several CLI stages while every competing Web request is rejected.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_ROUTES_API_LOCK="${BRORAY_ROUTES_API_LOCK:-$BRORAY_ROOT/run/global-operation.lock}"
BRORAY_ROUTES_API_LOCK_HELD=false

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

broray_routes_api_lock_write()
{
    local action bundle

    action="${1:-unknown}"
    bundle="${2:-}"

    printf '%s\n' "$$" >"$BRORAY_ROUTES_API_LOCK/pid" || return 1
    printf '%s\n' "routes" >"$BRORAY_ROUTES_API_LOCK/scope" || return 1
    printf '%s\n' "$action" >"$BRORAY_ROUTES_API_LOCK/action" || return 1
    printf '%s\n' "$bundle" >"$BRORAY_ROUTES_API_LOCK/bundle" || return 1
    printf '%s\n' "$(broray_routes_api_now)" >"$BRORAY_ROUTES_API_LOCK/startedAt" || return 1
    return 0
}

broray_routes_api_lock_acquire()
{
    local action bundle owner

    action="${1:-unknown}"
    bundle="${2:-}"
    mkdir -p "$(dirname "$BRORAY_ROUTES_API_LOCK")" || return 1

    if mkdir "$BRORAY_ROUTES_API_LOCK" 2>/dev/null; then
        broray_routes_api_lock_write "$action" "$bundle" || {
            rm -rf "$BRORAY_ROUTES_API_LOCK" 2>/dev/null || true
            return 1
        }
        BRORAY_ROUTES_API_LOCK_HELD=true
        return 0
    fi

    owner="$(sed -n '1p' "$BRORAY_ROUTES_API_LOCK/pid" 2>/dev/null || true)"
    if broray_routes_api_is_pid "$owner" && kill -0 "$owner" 2>/dev/null; then
        return 2
    fi

    rm -rf "$BRORAY_ROUTES_API_LOCK" 2>/dev/null || return 1
    if mkdir "$BRORAY_ROUTES_API_LOCK" 2>/dev/null; then
        broray_routes_api_lock_write "$action" "$bundle" || {
            rm -rf "$BRORAY_ROUTES_API_LOCK" 2>/dev/null || true
            return 1
        }
        BRORAY_ROUTES_API_LOCK_HELD=true
        return 0
    fi

    return 1
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

    if [ ! -d "$BRORAY_ROUTES_API_LOCK" ]; then
        jq -n '{active:false,pid:null,scope:null,action:null,bundleId:null,startedAt:null,stale:false}'
        return 0
    fi

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

    case "$owner" in ''|*[!0-9]*) owner=0 ;; esac
    jq -n \
        --argjson active "$active" \
        --argjson stale "$stale" \
        --argjson pid "$owner" \
        --arg scope "$scope" \
        --arg action "$action" \
        --arg bundleId "$bundle" \
        --arg startedAt "$started" '
        {
            active: $active,
            pid: (if $pid == 0 then null else $pid end),
            scope: (if $scope == "" then null else $scope end),
            action: (if $action == "" then null else $action end),
            bundleId: (if $bundleId == "" then null else $bundleId end),
            startedAt: (if $startedAt == "" then null else $startedAt end),
            stale: $stale
        }
    '
}
