#!/opt/bin/ash

# BROray transactional route synchronizer.
# Synchronizes the downloaded normalized route set with Keenetic while
# preserving routes owned by other BROray bundles and never touching external
# routes or routes on other interfaces.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_SYNC_ROUTES="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_SYNC_LOCK="$BRORAY_SYNC_ROUTES/locks/operation.lock"
BRORAY_SYNC_CONFIG_LIBRARY="${BRORAY_SYNC_CONFIG_LIBRARY:-$BRORAY_ROOT/lib/routes-router-config.sh}"
BRORAY_SYNC_OWNER_LIBRARY="${BRORAY_SYNC_OWNER_LIBRARY:-$BRORAY_ROOT/lib/interface-owner.sh}"
BRORAY_SYNC_PROGRESS_LIBRARY="${BRORAY_SYNC_PROGRESS_LIBRARY:-$BRORAY_ROOT/lib/routes-operation-progress.sh}"
BRORAY_SYNC_NDMC="${BRORAY_SYNC_NDMC:-ndmc}"
BRORAY_SYNC_ACTIVE_PID=""
BRORAY_SYNC_WORK=""
BRORAY_SYNC_ADDED_FILE=""
BRORAY_SYNC_DELETED_FILE=""
BRORAY_SYNC_LOCAL_BACKUP=""
BRORAY_SYNC_LOCAL_COMMITTED=false
BRORAY_SYNC_ROUTER_CHANGED=false
BRORAY_SYNC_ROLLING_BACK=false
BRORAY_SYNC_LOCK_HELD=false
BRORAY_SYNC_TRANSACTION=""
BRORAY_SYNC_PROGRESS_ACTIVE=false
BRORAY_SYNC_PROGRESS_CURRENT=0
BRORAY_SYNC_PROGRESS_TOTAL=0
BRORAY_SYNC_RESUME_BASE=0
BRORAY_SYNC_RESUMED=false

[ -r "$BRORAY_SYNC_CONFIG_LIBRARY" ] && . "$BRORAY_SYNC_CONFIG_LIBRARY"
[ -r "$BRORAY_SYNC_OWNER_LIBRARY" ] && . "$BRORAY_SYNC_OWNER_LIBRARY"
[ -r "$BRORAY_SYNC_PROGRESS_LIBRARY" ] && . "$BRORAY_SYNC_PROGRESS_LIBRARY"

broray_routes_sync_progress_begin()
{
    command -v broray_routes_progress_begin >/dev/null 2>&1 || return 0
    broray_routes_progress_begin "$@" || return 1
    BRORAY_SYNC_PROGRESS_ACTIVE=true
}

broray_routes_sync_progress_update()
{
    [ "$BRORAY_SYNC_PROGRESS_ACTIVE" = true ] || return 0
    command -v broray_routes_progress_update >/dev/null 2>&1 || return 0
    broray_routes_progress_update "$@"
}

broray_routes_sync_progress_tick()
{
    [ "$BRORAY_SYNC_PROGRESS_ACTIVE" = true ] || return 0
    command -v broray_routes_progress_tick >/dev/null 2>&1 || return 0
    broray_routes_progress_tick "$@"
}

broray_routes_sync_progress_complete()
{
    [ "$BRORAY_SYNC_PROGRESS_ACTIVE" = true ] || return 0
    command -v broray_routes_progress_complete >/dev/null 2>&1 || return 0
    broray_routes_progress_complete "$@"
}

broray_routes_sync_progress_fail()
{
    [ "$BRORAY_SYNC_PROGRESS_ACTIVE" = true ] || return 0
    command -v broray_routes_progress_fail >/dev/null 2>&1 || return 0
    broray_routes_progress_fail "$@"
}

broray_routes_sync_progress_pause()
{
    [ "$BRORAY_SYNC_PROGRESS_ACTIVE" = true ] || return 0
    command -v broray_routes_progress_pause >/dev/null 2>&1 || return 0
    broray_routes_progress_pause "$@"
}

broray_routes_sync_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_sync_stamp()
{
    date '+%Y%m%d-%H%M%S'
}

broray_routes_sync_id_valid()
{
    case "${1:-}" in
        ''|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*) return 1 ;;
    esac
    return 0
}

broray_routes_sync_is_pid()
{
    case "${1:-}" in
        ''|*[!0-9]*) return 1 ;;
    esac
    return 0
}

broray_routes_sync_lock_acquire()
{
    local owner

    mkdir -p "$(dirname "$BRORAY_SYNC_LOCK")" || return 1

    if mkdir "$BRORAY_SYNC_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_SYNC_LOCK/pid" || return 1
        printf '%s\n' "sync" >"$BRORAY_SYNC_LOCK/action" || return 1
        BRORAY_SYNC_LOCK_HELD=true
        return 0
    fi

    owner="$(sed -n '1p' "$BRORAY_SYNC_LOCK/pid" 2>/dev/null)"
    if broray_routes_sync_is_pid "$owner" && kill -0 "$owner" 2>/dev/null; then
        return 2
    fi

    rm -rf "$BRORAY_SYNC_LOCK" 2>/dev/null || return 1
    if mkdir "$BRORAY_SYNC_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_SYNC_LOCK/pid" || return 1
        printf '%s\n' "sync" >"$BRORAY_SYNC_LOCK/action" || return 1
        BRORAY_SYNC_LOCK_HELD=true
        return 0
    fi

    return 1
}

broray_routes_sync_lock_release()
{
    if [ "$BRORAY_SYNC_LOCK_HELD" = true ]; then
        rm -rf "$BRORAY_SYNC_LOCK" 2>/dev/null || true
        BRORAY_SYNC_LOCK_HELD=false
    fi
}

broray_routes_sync_kill_active()
{
    [ -n "$BRORAY_SYNC_ACTIVE_PID" ] || return 0
    if kill -0 "$BRORAY_SYNC_ACTIVE_PID" 2>/dev/null; then
        kill "$BRORAY_SYNC_ACTIVE_PID" 2>/dev/null || true
        sleep 1
        kill -0 "$BRORAY_SYNC_ACTIVE_PID" 2>/dev/null && kill -9 "$BRORAY_SYNC_ACTIVE_PID" 2>/dev/null || true
    fi
    wait "$BRORAY_SYNC_ACTIVE_PID" 2>/dev/null || true
    BRORAY_SYNC_ACTIVE_PID=""
}

broray_routes_sync_ndmc()
{
    local command_text output error limit elapsed rc ndmc_bin

    command_text="$1"
    output="$2"
    error="$3"
    limit="${4:-10}"
    : >"$output"
    : >"$error"

    case "$BRORAY_SYNC_NDMC" in
        */*) ndmc_bin="$BRORAY_SYNC_NDMC" ;;
        *) ndmc_bin="$(command -v "$BRORAY_SYNC_NDMC" 2>/dev/null || true)" ;;
    esac
    [ -n "$ndmc_bin" ] && [ -x "$ndmc_bin" ] || return 127

    "$ndmc_bin" -c "$command_text" >"$output" 2>"$error" &
    BRORAY_SYNC_ACTIVE_PID=$!
    elapsed=0
    while kill -0 "$BRORAY_SYNC_ACTIVE_PID" 2>/dev/null; do
        if [ "$elapsed" -ge "$limit" ]; then
            broray_routes_sync_kill_active
            return 124
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done

    if wait "$BRORAY_SYNC_ACTIVE_PID" 2>/dev/null; then rc=0; else rc=$?; fi
    BRORAY_SYNC_ACTIVE_PID=""
    return "$rc"
}

broray_routes_sync_save_config()
{
    local out err rc
    out="$BRORAY_SYNC_WORK/save.out"
    err="$BRORAY_SYNC_WORK/save.err"
    rc=0
    broray_routes_sync_ndmc "system configuration save" "$out" "$err" 15 || rc=$?
    [ "$rc" -eq 0 ] || return 1
    grep -Eiq 'Io::Netlink error|system failed|invalid command|unknown command|save failed' "$out" "$err" 2>/dev/null && return 1
    return 0
}

broray_routes_sync_transaction_write()
{
    local phase message now file temp

    phase="$1"
    message="$2"
    [ -n "$BRORAY_SYNC_TRANSACTION" ] || return 0

    mkdir -p "$BRORAY_SYNC_TRANSACTION" || return 1
    now="$(broray_routes_sync_now)"
    file="$BRORAY_SYNC_TRANSACTION/transaction.json"
    temp="$file.new.$$"

    jq -n \
        --arg bundleId "${BRORAY_SYNC_BUNDLE:-}" \
        --arg phase "$phase" \
        --arg message "$message" \
        --arg updatedAt "$now" '
        {
            schemaVersion: 1,
            operation: "sync",
            bundleId: $bundleId,
            phase: $phase,
            message: $message,
            updatedAt: $updatedAt
        }
    ' >"$temp" || {
        rm -f "$temp"
        return 1
    }

    chmod 600 "$temp" 2>/dev/null || true
    mv "$temp" "$file"
}

broray_routes_sync_restore_local()
{
    local registry bundle state plan result

    [ -d "$BRORAY_SYNC_LOCAL_BACKUP" ] || return 0
    registry="$BRORAY_SYNC_ROUTES/installed/routes.json"
    bundle="$BRORAY_SYNC_ROUTES/installed/bundles/$BRORAY_SYNC_BUNDLE.json"
    state="$BRORAY_SYNC_ROUTES/state/$BRORAY_SYNC_BUNDLE.json"
    plan="$BRORAY_SYNC_ROUTES/catalog/$BRORAY_SYNC_BUNDLE/export-plan.json"
    result="$BRORAY_SYNC_ROUTES/catalog/$BRORAY_SYNC_BUNDLE/router-export-result.json"

    cp -p "$BRORAY_SYNC_LOCAL_BACKUP/routes.json" "$registry" 2>/dev/null || true
    cp -p "$BRORAY_SYNC_LOCAL_BACKUP/bundle.json" "$bundle" 2>/dev/null || true
    cp -p "$BRORAY_SYNC_LOCAL_BACKUP/state.json" "$state" 2>/dev/null || true
    cp -p "$BRORAY_SYNC_LOCAL_BACKUP/export-plan.json" "$plan" 2>/dev/null || true
    if [ -f "$BRORAY_SYNC_LOCAL_BACKUP/result.missing" ]; then
        rm -f "$result" 2>/dev/null || true
    elif [ -f "$BRORAY_SYNC_LOCAL_BACKUP/router-export-result.json" ]; then
        cp -p "$BRORAY_SYNC_LOCAL_BACKUP/router-export-result.json" "$result" 2>/dev/null || true
    fi
}

broray_routes_sync_rollback_router()
{
    local tab kind key network prefix mask interface metric out err rc

    [ "$BRORAY_SYNC_ROUTER_CHANGED" = true ] || return 0
    tab="$(printf '\t')"

    if [ -f "$BRORAY_SYNC_DELETED_FILE" ]; then
        sed '1!G;h;$!d' "$BRORAY_SYNC_DELETED_FILE" |
        while IFS="$tab" read -r kind key network prefix mask interface metric; do
            [ -n "$network" ] || continue
            out="$BRORAY_SYNC_WORK/rollback-add.out"
            err="$BRORAY_SYNC_WORK/rollback-add.err"
            broray_routes_sync_ndmc "ip route $network $mask $interface $metric" "$out" "$err" 10 >/dev/null 2>&1 || true
        done
    fi

    if [ -f "$BRORAY_SYNC_ADDED_FILE" ]; then
        sed '1!G;h;$!d' "$BRORAY_SYNC_ADDED_FILE" |
        while IFS="$tab" read -r kind key network prefix mask interface metric; do
            [ -n "$network" ] || continue
            out="$BRORAY_SYNC_WORK/rollback-delete.out"
            err="$BRORAY_SYNC_WORK/rollback-delete.err"
            rc=0
            broray_routes_sync_ndmc "no ip route $network $mask $interface" "$out" "$err" 10 || rc=$?
            : "$rc"
        done
    fi

    broray_routes_sync_save_config >/dev/null 2>&1 || true
}

broray_routes_sync_cleanup()
{
    broray_routes_sync_kill_active
    broray_routes_sync_lock_release
    [ -n "$BRORAY_SYNC_WORK" ] && rm -rf "$BRORAY_SYNC_WORK" 2>/dev/null || true
    BRORAY_SYNC_WORK=""
}

broray_routes_sync_partial_commit()
{
    local sync_plan message error_route output now registry bundle_registry state
    local added_json deleted_json added_count deleted_count previous_created previous_deleted

    sync_plan="$1"
    message="$2"
    error_route="$3"
    output="$BRORAY_SYNC_WORK/partial"
    now="$(broray_routes_sync_now)"
    registry="$BRORAY_SYNC_ROUTES/installed/routes.json"
    bundle_registry="$BRORAY_SYNC_ROUTES/installed/bundles/$BRORAY_SYNC_BUNDLE.json"
    state="$BRORAY_SYNC_ROUTES/state/$BRORAY_SYNC_BUNDLE.json"
    added_json="$BRORAY_SYNC_WORK/partial-added.json"
    deleted_json="$BRORAY_SYNC_WORK/partial-deleted.json"

    mkdir -p "$output" || return 1

    jq -Rn '
        [
            inputs | split("\t") | select(length == 7) |
            {
                kind: .[0], key: .[1], network: .[2],
                prefix: (.[3] | tonumber), mask: .[4],
                interface: .[5], metric: (.[6] | tonumber)
            }
        ]
    ' <"$BRORAY_SYNC_ADDED_FILE" >"$added_json" || return 1

    jq -Rn '
        [
            inputs | split("\t") | select(length == 7) |
            {
                kind: .[0], key: .[1], network: .[2],
                prefix: (.[3] | tonumber), mask: .[4],
                interface: .[5], metric: (.[6] | tonumber)
            }
        ]
    ' <"$BRORAY_SYNC_DELETED_FILE" >"$deleted_json" || return 1

    added_count="$(jq -r '[.[] | select(.kind == "create")] | length' "$added_json")"
    deleted_count="$(jq -r 'length' "$deleted_json")"
    previous_created="$(jq -r '.resumeOperation.created // 0' "$state" 2>/dev/null || printf 0)"
    previous_deleted="$(jq -r '.resumeOperation.deleted // 0' "$state" 2>/dev/null || printf 0)"

    jq -n \
        --slurpfile old "$registry" \
        --slurpfile sync "$sync_plan" \
        --slurpfile added "$added_json" \
        --slurpfile deleted "$deleted_json" \
        --arg bundleId "$BRORAY_SYNC_BUNDLE" \
        --arg now "$now" '
        $old[0] as $o |
        $sync[0] as $s |
        def desired_route($key): first($s.desired[]? | select(.route.key == $key) | .route);
        (reduce $added[0][] as $item (($o.routes // []);
            if $item.kind == "create" then
                (desired_route($item.key)) as $r |
                (map(.key) | index($item.key)) as $idx |
                if $idx == null then
                    . + [{
                        key: $r.key,
                        family: ($r.family // "ipv4"),
                        network: $r.network,
                        prefix: $r.prefix,
                        mask: $r.mask,
                        interface: $r.targetInterface,
                        gatewayToken: ($r.gatewayToken // "0.0.0.0"),
                        metric: ($r.metric // 1200),
                        automatic: ($r.automatic // null),
                        exclusive: ($r.exclusive // null),
                        comment: ($r.comment // "BROray"),
                        createdByBROray: true,
                        managed: true,
                        routerCommentPersisted: false,
                        actualStatus: "present",
                        owners: [$bundleId],
                        createdAt: $now,
                        updatedAt: $now
                    }]
                else
                    .[$idx].owners = (((.[$idx].owners // []) + [$bundleId]) | unique) |
                    .[$idx].actualStatus = "present" |
                    .[$idx].updatedAt = $now
                end
            elif $item.kind == "shared_restore" then
                (map(.key) | index($item.key)) as $idx |
                if $idx == null then .
                else
                    .[$idx].owners = (((.[$idx].owners // []) - [$bundleId]) | unique) |
                    .[$idx].actualStatus = "present" |
                    .[$idx].updatedAt = $now
                end
            else . end
        )) as $after_added |
        (reduce $deleted[0][] as $item ($after_added;
            (map(.key) | index($item.key)) as $idx |
            if $idx == null then .
            else
                .[$idx].owners = (((.[$idx].owners // []) - [$bundleId]) | unique) |
                .[$idx].updatedAt = $now
            end
        )) as $routes |
        $o + {
            schemaVersion: 1,
            managedInterface: $s.targetInterface,
            managedMetric: 1200,
            routes: ($routes | map(select(((.owners // []) | length) > 0))),
            updatedAt: $now
        }
    ' >"$output/routes.json" || return 1

    jq -n \
        --slurpfile old "$bundle_registry" \
        --slurpfile added "$added_json" \
        --slurpfile deleted "$deleted_json" \
        --arg now "$now" '
        $old[0] as $o |
        (reduce $added[0][] as $item (($o.routeKeys // []);
            if $item.kind == "create" then (. + [$item.key] | unique)
            else (. - [$item.key]) end
        )) as $after_added |
        (reduce $deleted[0][] as $item ($after_added; . - [$item.key])) as $keys |
        $o + {
            routeKeys: $keys,
            managedRouteKeys: $keys,
            externalRouteKeys: [],
            updatedAt: $now
        }
    ' >"$output/bundle.json" || return 1

    jq \
        --arg now "$now" \
        --arg message "$message" \
        --arg errorRoute "$error_route" \
        --arg operation "$(jq -r '.mode // "install"' "$sync_plan")" \
        --argjson current "$BRORAY_SYNC_PROGRESS_CURRENT" \
        --argjson total "$BRORAY_SYNC_PROGRESS_TOTAL" \
        --argjson created "$((previous_created + added_count))" \
        --argjson deleted "$((previous_deleted + deleted_count))" '
        .lastVerifiedAt = null |
        .verifyResult = null |
        .preflight = null |
        .resumeOperation = {
            operation: $operation,
            resumable: true,
            current: $current,
            total: $total,
            created: $created,
            deleted: $deleted,
            message: $message,
            errorRoute: (if $errorRoute == "" then null else $errorRoute end),
            updatedAt: $now
        } |
        .lastError = (
            if $errorRoute == "" then null
            else {code: "ROUTES_OPERATION_PAUSED_AFTER_ERROR", message: $message, route: $errorRoute, resumable: true}
            end
        ) |
        .updatedAt = $now
    ' "$state" >"$output/state.json" || return 1

    jq -e --arg interface "$(jq -r '.targetInterface' "$sync_plan")" '
        (.schemaVersion == 1) and
        (.managedInterface == $interface) and
        (.managedMetric == 1200) and
        (([.routes[].key] | length) == ([.routes[].key] | unique | length)) and
        (all(.routes[];
            .interface == $interface and
            .metric == 1200 and
            .createdByBROray == true and
            .managed == true and
            (((.owners // []) | length) > 0)
        ))
    ' "$output/routes.json" >/dev/null 2>&1 || return 1

    jq -e --arg id "$BRORAY_SYNC_BUNDLE" '
        (.bundleId == $id) and
        ((.routeKeys | type) == "array") and
        ((.managedRouteKeys | type) == "array") and
        (.routeKeys == .managedRouteKeys)
    ' "$output/bundle.json" >/dev/null 2>&1 || return 1

    jq -e '.resumeOperation.resumable == true' "$output/state.json" >/dev/null 2>&1 || return 1

    cp -p "$output/routes.json" "$registry.new.$$" && mv "$registry.new.$$" "$registry" || return 1
    cp -p "$output/bundle.json" "$bundle_registry.new.$$" && mv "$bundle_registry.new.$$" "$bundle_registry" || return 1
    cp -p "$output/state.json" "$state.new.$$" && mv "$state.new.$$" "$state" || return 1
    chmod 644 "$registry" "$bundle_registry" "$state" 2>/dev/null || true
    return 0
}

broray_routes_sync_pause()
{
    local sync_plan message stopped_by_user error_route exit_code phase_message

    sync_plan="$1"
    message="$2"
    stopped_by_user="${3:-true}"
    error_route="${4:-}"
    case "$stopped_by_user" in true|false) ;; *) stopped_by_user=true ;; esac

    trap - EXIT HUP INT TERM
    if [ "$stopped_by_user" = true ]; then
        phase_message="Остановка после текущего маршрута."
        exit_code=74
    else
        phase_message="Операция приостановлена после ошибки."
        exit_code=76
    fi

    broray_routes_sync_progress_update \
        "stopping" "$BRORAY_SYNC_PROGRESS_CURRENT" "$BRORAY_SYNC_PROGRESS_TOTAL" \
        "$phase_message" "$error_route" >/dev/null 2>&1 || true

    if [ "$BRORAY_SYNC_ROUTER_CHANGED" = true ]; then
        broray_routes_sync_save_config ||
            broray_routes_sync_abort "Не удалось сохранить частично выполненную операцию; выполнен откат."
    fi

    broray_routes_sync_partial_commit "$sync_plan" "$message" "$error_route" ||
        broray_routes_sync_abort "Не удалось сохранить состояние для продолжения; выполнен откат."

    broray_routes_sync_transaction_write "paused" "$message" >/dev/null 2>&1 || true
    BRORAY_SYNC_ROUTER_CHANGED=false
    BRORAY_SYNC_LOCAL_COMMITTED=false
    broray_routes_sync_progress_pause "$message" "$stopped_by_user" "$error_route" >/dev/null 2>&1 || true
    broray_routes_sync_lock_release

    printf '%s\n' "$message"
    printf 'Выполнено: %s из %s\n' "$BRORAY_SYNC_PROGRESS_CURRENT" "$BRORAY_SYNC_PROGRESS_TOTAL"
    printf '%s\n' 'Операцию можно продолжить позднее.'

    broray_routes_sync_cleanup
    exit "$exit_code"
}

broray_routes_sync_stop_if_requested()
{
    local sync_plan
    sync_plan="$1"
    command -v broray_routes_progress_stop_requested >/dev/null 2>&1 || return 0
    if broray_routes_progress_stop_requested "$BRORAY_SYNC_BUNDLE"; then
        broray_routes_sync_pause \
            "$sync_plan" \
            "Операция остановлена пользователем. Выполнено $BRORAY_SYNC_PROGRESS_CURRENT из $BRORAY_SYNC_PROGRESS_TOTAL." \
            true ""
    fi
}

broray_routes_sync_route_failure()
{
    local sync_plan message route
    sync_plan="$1"
    message="$2"
    route="$3"
    broray_routes_sync_pause \
        "$sync_plan" \
        "$message Выполнено $BRORAY_SYNC_PROGRESS_CURRENT из $BRORAY_SYNC_PROGRESS_TOTAL. Можно продолжить после устранения причины." \
        false "$route"
}

broray_routes_sync_abort()
{
    local message
    message="$*"
    trap - EXIT HUP INT TERM
    if [ "$BRORAY_SYNC_ROLLING_BACK" = false ]; then
        BRORAY_SYNC_ROLLING_BACK=true
        [ -n "$BRORAY_SYNC_LOCAL_BACKUP" ] && [ -d "$BRORAY_SYNC_LOCAL_BACKUP" ] && broray_routes_sync_restore_local
        broray_routes_sync_rollback_router
        broray_routes_sync_transaction_write "rolled_back" "$message" >/dev/null 2>&1 || true
    fi
    broray_routes_sync_progress_fail         "$message Изменения операции отменены." true >/dev/null 2>&1 || true
    broray_routes_sync_cleanup
    printf 'ОШИБКА: %s\n' "$message" >&2
    exit 1
}

broray_routes_sync_trap()
{
    broray_routes_sync_abort "Операция прервана; выполнен откат изменений."
}

broray_routes_sync_interface_check()
{
    local interface out err
    interface="$1"

    if [ "${BRORAY_SYNC_SKIP_INTERFACE_CHECK:-0}" = "1" ]; then
        return 0
    fi

    out="$BRORAY_SYNC_WORK/interface.out"
    err="$BRORAY_SYNC_WORK/interface.err"
    broray_routes_sync_ndmc "show interface $interface" "$out" "$err" 10 || return 1
    grep -Eq "^[[:space:]]*id:[[:space:]]*$interface[[:space:]]*$" "$out" || return 1
    grep -Eq '^[[:space:]]*connected:[[:space:]]*yes[[:space:]]*$' "$out" || return 1
    grep -Eq '^[[:space:]]*state:[[:space:]]*up[[:space:]]*$' "$out" || return 1
    return 0
}

broray_routes_sync_ensure_global_registry()
{
    local registry config interface now claims reg id count plan new

    registry="$BRORAY_SYNC_ROUTES/installed/routes.json"
    config="$BRORAY_SYNC_ROUTES/config.json"
    interface="$(jq -r '.managedInterface // empty' "$config" 2>/dev/null)"
    now="$(broray_routes_sync_now)"

    if [ -r "$registry" ]; then
        jq -e --arg interface "$interface" '
            (.schemaVersion == 1) and
            (.managedInterface == $interface) and
            ((.managedMetric // 1200) == 1200) and
            ((.routes | type) == "array") and
            (([.routes[].key] | length) == ([.routes[].key] | unique | length)) and
            (all(.routes[];
                (.key | type) == "string" and
                (.interface == $interface) and
                ((.metric // 1200) == 1200) and
                (.createdByBROray == true) and
                (.managed == true) and
                ((.owners | type) == "array") and
                ((.owners | length) > 0)
            ))
        ' "$registry" >/dev/null 2>&1 || return 1
        return 0
    fi

    mkdir -p "$(dirname "$registry")" "$BRORAY_SYNC_ROUTES/tmp" || return 1
    claims="$BRORAY_SYNC_ROUTES/tmp/global-registry-claims.$$"
    : >"$claims" || return 1

    for reg in "$BRORAY_SYNC_ROUTES"/installed/bundles/*.json; do
        [ -f "$reg" ] || continue
        id="$(jq -r '.bundleId // empty' "$reg" 2>/dev/null)"
        broray_routes_sync_id_valid "$id" || { rm -f "$claims"; return 1; }
        jq -e --arg id "$id" '
            (.schemaVersion == 1) and
            (.bundleId == $id) and
            ((.managedRouteKeys | type) == "array")
        ' "$reg" >/dev/null 2>&1 || { rm -f "$claims"; return 1; }
        count="$(jq -r '.managedRouteKeys | length' "$reg")"
        [ "$count" -gt 0 ] || continue
        jq -e '.installedVersion != null' "$reg" >/dev/null 2>&1 || { rm -f "$claims"; return 1; }
        plan="$BRORAY_SYNC_ROUTES/catalog/$id/export-plan.json"
        [ -r "$plan" ] || { rm -f "$claims"; return 1; }
        jq -c --slurpfile reg "$reg" --arg id "$id" --arg now "$now" '
            ($reg[0].managedRouteKeys // []) as $keys |
            .routes[] |
            select(.key as $key | ($keys | index($key)) != null) |
            {
                key: .key,
                family: .family,
                network: .network,
                prefix: .prefix,
                mask: .mask,
                interface: .targetInterface,
                gatewayToken: .gatewayToken,
                metric: .metric,
                automatic: .automatic,
                exclusive: .exclusive,
                comment: .comment,
                createdByBROray: true,
                managed: true,
                routerCommentPersisted: false,
                actualStatus: "unknown",
                owners: [$id],
                createdAt: $now,
                updatedAt: $now
            }
        ' "$plan" >>"$claims" || { rm -f "$claims"; return 1; }
        [ "$(jq -s --arg id "$id" '[.[] | select((.owners | index($id)) != null)] | length' "$claims")" -ge "$count" ] || { rm -f "$claims"; return 1; }
    done

    new="$registry.new.$$"
    jq -s --arg interface "$interface" --arg now "$now" '
        (reduce .[] as $route ({};
            if has($route.key) then
                .[$route.key].owners = ((.[$route.key].owners + $route.owners) | unique) |
                .[$route.key].updatedAt = $now
            else
                .[$route.key] = $route
            end
        )) as $byKey |
        {
            schemaVersion: 1,
            managedInterface: $interface,
            managedMetric: 1200,
            routes: ($byKey | to_entries | map(.value)),
            updatedAt: $now
        }
    ' "$claims" >"$new" || { rm -f "$claims" "$new"; return 1; }
    rm -f "$claims"

    jq -e --arg interface "$interface" '
        (.schemaVersion == 1) and
        (.managedInterface == $interface) and
        (.managedMetric == 1200) and
        ((.routes | type) == "array") and
        (([.routes[].key] | length) == ([.routes[].key] | unique | length))
    ' "$new" >/dev/null 2>&1 || { rm -f "$new"; return 1; }
    chmod 644 "$new" 2>/dev/null || true
    mv "$new" "$registry"
}

broray_routes_sync_build_plan_core()
{
    local bundle_id output config bundles catalog plan registry bundle_registry state actual interface metric comment now

    bundle_id="$1"
    output="$2"
    config="$BRORAY_SYNC_ROUTES/config.json"
    bundles="$BRORAY_SYNC_ROUTES/bundles.json"
    catalog="$BRORAY_SYNC_ROUTES/catalog/$bundle_id"
    plan="$catalog/export-plan.json"
    registry="$BRORAY_SYNC_ROUTES/installed/routes.json"
    bundle_registry="$BRORAY_SYNC_ROUTES/installed/bundles/$bundle_id.json"
    state="$BRORAY_SYNC_ROUTES/state/$bundle_id.json"
    actual="$BRORAY_SYNC_WORK/running-config.json"

    for file in "$config" "$bundles" "$plan" "$registry" "$bundle_registry" "$state"; do
        [ -r "$file" ] || return 1
    done

    jq -e --arg id "$bundle_id" '.schemaVersion == 1 and (.bundles | index($id) != null)' "$bundles" >/dev/null 2>&1 || return 1
    interface="$(jq -r '.managedInterface // empty' "$config")"
    metric="$(jq -r '.managedMetric // empty' "$config")"
    comment="$(jq -r '.routeComment // empty' "$config")"
    case "$interface" in Proxy[0-9]*) ;; *) return 1 ;; esac
    case "${interface#Proxy}" in ''|*[!0-9]*) return 1 ;; esac
    [ "$metric" = "1200" ] && [ "$comment" = "BROray" ] || return 1

    jq -e --arg interface "$interface" '
        (.schemaVersion == 1) and
        (.managedInterface == $interface) and
        (.managedMetric == 1200) and
        (.routeComment == "BROray") and
        (.ownershipPolicy.adoptExistingRoutes == false) and
        (.ownershipPolicy.modifyExternalRoutes == false) and
        (.ownershipPolicy.deleteExternalRoutes == false) and
        (.ownershipPolicy.touchOtherInterfaces == false) and
        (.ownershipPolicy.deleteOnlyExactManagedMatch == true)
    ' "$config" >/dev/null 2>&1 || return 1

    jq -e --arg id "$bundle_id" --arg interface "$interface" '
        (.schemaVersion == 1) and
        (.bundleId == $id) and
        (.targetInterface == $interface) and
        (.managedMetric == 1200) and
        (.routeComment == "BROray") and
        ((.routes | type) == "array") and
        ((.routes | length) > 0)
    ' "$plan" >/dev/null 2>&1 || return 1

    [ "$(jq -r '.downloadedVersion.contentSha256 // empty' "$state")" = "$(jq -r '.contentSha256 // empty' "$plan")" ] || return 1

    BRORAY_ROUTES_CONFIG_NDMC="$BRORAY_SYNC_NDMC"
    export BRORAY_ROUTES_CONFIG_NDMC
    rm -f "${BRORAY_ROUTES_CONFIG_CACHE:-}" 2>/dev/null || true
    command -v broray_routes_config_fetch >/dev/null 2>&1 || return 1
    broray_routes_config_fetch "$actual" || return 1
    now="$(broray_routes_sync_now)"

    jq -n \
        --slurpfile desired "$plan" \
        --slurpfile global "$registry" \
        --slurpfile bundle "$bundle_registry" \
        --slurpfile state "$state" \
        --slurpfile actual "$actual" \
        --arg bundleId "$bundle_id" \
        --arg interface "$interface" \
        --arg now "$now" '
        $desired[0] as $p |
        $global[0] as $g |
        $bundle[0] as $b |
        $state[0] as $s |
        $actual[0] as $a |
        ($p.routes | map(.key)) as $desiredKeys |
        ($b.routeKeys // []) as $oldKeys |
        ($b.managedRouteKeys // []) as $oldManagedKeys |

        def exact_matches($route):
            [
                $a.routes[]? |
                select(
                    .destination == ($route.network + "/" + ($route.prefix | tostring)) and
                    .interface == $interface and
                    ((.gateway // "0.0.0.0") == "0.0.0.0") and
                    ((.metric // 1000) == 1200) and
                    (((.proto // "static") | ascii_downcase) == "static")
                )
            ];

        def target_matches($route):
            [
                $a.routes[]? |
                select(
                    .destination == ($route.network + "/" + ($route.prefix | tostring)) and
                    .interface == $interface
                )
            ];

        [
            $p.routes[] as $r |
            (exact_matches($r)) as $exact |
            (target_matches($r)) as $target |
            ([$g.routes[]? | select(.key == $r.key)]) as $registered |
            {
                route: $r,
                status: (
                    if (($target | length) > ($exact | length)) or (($exact | length) > 1) or (($registered | length) > 1) then "conflict"
                    elif ($exact | length) == 1 then
                        if (($registered | length) == 1 and ($registered[0].createdByBROray == true) and ($registered[0].managed == true))
                        then "managed_existing"
                        else "external_existing"
                        end
                    elif ($target | length) == 0 then "create"
                    else "conflict"
                    end
                ),
                exactMatchCount: ($exact | length),
                targetMatchCount: ($target | length),
                registered: (($registered | length) == 1),
                existingOwners: (($registered[0].owners // []) | unique)
            }
        ] as $desiredPlan |

        [
            $oldManagedKeys[] as $key |
            select(($desiredKeys | index($key)) == null) |
            ([$g.routes[]? | select(.key == $key)]) as $registered |
            if ($registered | length) != 1 then
                {key: $key, status: "conflict", reason: "registry_missing"}
            else
                $registered[0] as $route |
                (($route.owners // []) - [$bundleId] | unique) as $remaining |
                (exact_matches({network: $route.network, prefix: $route.prefix})) as $exact |
                (target_matches({network: $route.network, prefix: $route.prefix})) as $target |
                {
                    key: $key,
                    route: {
                        key: $route.key,
                        family: ($route.family // "ipv4"),
                        network: $route.network,
                        prefix: $route.prefix,
                        mask: $route.mask,
                        targetInterface: $route.interface,
                        gatewayToken: ($route.gatewayToken // "0.0.0.0"),
                        metric: ($route.metric // 1200),
                        automatic: ($route.automatic // null),
                        exclusive: ($route.exclusive // null),
                        comment: ($route.comment // "BROray")
                    },
                    remainingOwners: $remaining,
                    status: (
                        if (($target | length) > ($exact | length)) or (($exact | length) > 1) then "conflict"
                        elif ($remaining | length) > 0 then
                            if ($exact | length) == 1 then "shared_keep"
                            elif ($target | length) == 0 then "shared_restore"
                            else "conflict"
                            end
                        else
                            if ($exact | length) == 1 then "delete"
                            elif ($target | length) == 0 then "already_absent"
                            else "conflict"
                            end
                        end
                    ),
                    exactMatchCount: ($exact | length),
                    targetMatchCount: ($target | length)
                }
            end
        ] as $obsoletePlan |

        ([ $desiredPlan[] | select(.status == "conflict" or .status == "external_existing") ] |
         . + [ $obsoletePlan[] | select(.status == "conflict") ]) as $blocking |
        ($desiredKeys - $oldKeys | length) as $addedToBundle |
        ($oldKeys - $desiredKeys | length) as $removedFromBundle |
        ([ $desiredKeys[] as $key | select(($oldKeys | index($key)) != null) | $key ] | length) as $unchangedInBundle |
        (
            if $s.installedVersion == null then "install"
            elif ($s.installedVersion.contentSha256 // "") != ($s.downloadedVersion.contentSha256 // "") then "update"
            elif ([ $desiredPlan[] | select(.status == "create") ] | length) > 0 then "restore"
            else "none"
            end
        ) as $mode |
        {
            schemaVersion: 1,
            bundleId: $bundleId,
            mode: $mode,
            targetInterface: $interface,
            managedMetric: 1200,
            contentSha256: $p.contentSha256,
            checkedAt: $now,
            canApply: (($blocking | length) == 0),
            summary: {
                total: ($desiredPlan | length),
                addedRoutes: $addedToBundle,
                removedRoutes: $removedFromBundle,
                unchangedRoutes: $unchangedInBundle,
                toCreate: ([ $desiredPlan[] | select(.status == "create") ] | length),
                managedExisting: ([ $desiredPlan[] | select(.status == "managed_existing") ] | length),
                externalExisting: ([ $desiredPlan[] | select(.status == "external_existing") ] | length),
                conflicts: ([ $blocking[] ] | length),
                toDelete: ([ $obsoletePlan[] | select(.status == "delete") ] | length),
                sharedKept: ([ $obsoletePlan[] | select(.status == "shared_keep") ] | length),
                sharedToRestore: ([ $obsoletePlan[] | select(.status == "shared_restore") ] | length),
                alreadyAbsent: ([ $obsoletePlan[] | select(.status == "already_absent") ] | length)
            },
            desired: $desiredPlan,
            obsolete: $obsoletePlan,
            blocking: $blocking,
            message: (
                if ($blocking | length) > 0 then "Обнаружены конфликты или внешние маршруты на управляемом интерфейсе."
                elif $mode == "install" then "План установки маршрутов подготовлен."
                elif $mode == "update" then "План обновления маршрутов подготовлен."
                elif $mode == "restore" then "План восстановления маршрутов подготовлен."
                else "Изменения в Keenetic не требуются."
                end
            )
        }
    ' >"$output" || return 1

    jq -e --arg id "$bundle_id" --arg interface "$interface" '
        (.schemaVersion == 1) and
        (.bundleId == $id) and
        (.targetInterface == $interface) and
        (.managedMetric == 1200) and
        ((.summary.total | type) == "number") and
        ((.desired | type) == "array") and
        ((.obsolete | type) == "array") and
        ((.blocking | type) == "array")
    ' "$output" >/dev/null 2>&1
}

broray_routes_sync_plan()
{
    local bundle_id lock_rc output
    bundle_id="${1:-}"
    broray_routes_sync_id_valid "$bundle_id" || broray_routes_sync_abort "Некорректный идентификатор набора."
    BRORAY_SYNC_BUNDLE="$bundle_id"
    export BRORAY_SYNC_BUNDLE

    lock_rc=0
    broray_routes_sync_lock_acquire || lock_rc=$?
    case "$lock_rc" in
        0) ;;
        2) broray_routes_sync_abort "Другая операция с маршрутами уже выполняется." ;;
        *) broray_routes_sync_abort "Не удалось установить блокировку операции." ;;
    esac

    BRORAY_SYNC_WORK="$BRORAY_SYNC_ROUTES/tmp/sync-plan-$bundle_id.$$"
    mkdir -p "$BRORAY_SYNC_WORK" || broray_routes_sync_abort "Не удалось создать рабочий каталог."
    broray_routes_sync_ensure_global_registry || broray_routes_sync_abort "Не удалось восстановить или проверить общий реестр маршрутов."
    output="$BRORAY_SYNC_WORK/plan.json"
    broray_routes_sync_build_plan_core "$bundle_id" "$output" || broray_routes_sync_abort "Не удалось построить безопасный план установки."
    cat "$output"
    broray_routes_sync_cleanup
}

broray_routes_sync_prepare_local()
{
    local sync_plan registry bundle_registry state export_plan output now mode message
    local total created deleted shared kept restored absent unchanged previous_created previous_deleted

    sync_plan="$1"
    output="$2"
    registry="$BRORAY_SYNC_ROUTES/installed/routes.json"
    bundle_registry="$BRORAY_SYNC_ROUTES/installed/bundles/$BRORAY_SYNC_BUNDLE.json"
    state="$BRORAY_SYNC_ROUTES/state/$BRORAY_SYNC_BUNDLE.json"
    export_plan="$BRORAY_SYNC_ROUTES/catalog/$BRORAY_SYNC_BUNDLE/export-plan.json"
    now="$(broray_routes_sync_now)"
    mode="$(jq -r '.mode' "$sync_plan")"
    total="$(jq -r '.summary.total' "$sync_plan")"
    previous_created="$(jq -r '.resumeOperation.created // 0' "$state" 2>/dev/null || printf 0)"
    previous_deleted="$(jq -r '.resumeOperation.deleted // 0' "$state" 2>/dev/null || printf 0)"
    created="$((previous_created + $(wc -l <"$BRORAY_SYNC_ADDED_FILE" | tr -d ' ')))"
    deleted="$((previous_deleted + $(wc -l <"$BRORAY_SYNC_DELETED_FILE" | tr -d ' ')))"
    shared="$(jq -r '.summary.sharedKept' "$sync_plan")"
    restored="$(jq -r '.summary.sharedToRestore' "$sync_plan")"
    absent="$(jq -r '.summary.alreadyAbsent' "$sync_plan")"
    unchanged="$(jq -r '.summary.unchangedRoutes' "$sync_plan")"

    case "$mode" in
        install) message="Маршруты установлены в Keenetic" ;;
        update) message="Маршруты обновлены в Keenetic" ;;
        restore) message="Маршруты восстановлены в Keenetic" ;;
        *) message="Маршруты в Keenetic проверены" ;;
    esac

    mkdir -p "$output" || return 1

    jq -n --slurpfile old "$registry" --slurpfile sync "$sync_plan" --arg bundleId "$BRORAY_SYNC_BUNDLE" --arg now "$now" '
        $old[0] as $o |
        $sync[0] as $s |
        [
            ($o.routes // [])[] |
            if ((.owners // []) | index($bundleId)) != null then
                .owners = ((.owners // []) - [$bundleId] | unique) |
                .updatedAt = $now
            else . end |
            select((.owners | length) > 0)
        ] as $withoutOld |
        (reduce ($s.desired[] | select(.status == "create" or .status == "managed_existing")) as $item
            ($withoutOld;
                ($item.route) as $r |
                (map(.key) | index($r.key)) as $idx |
                if $idx == null then
                    . + [{
                        key: $r.key,
                        family: $r.family,
                        network: $r.network,
                        prefix: $r.prefix,
                        mask: $r.mask,
                        interface: $r.targetInterface,
                        gatewayToken: $r.gatewayToken,
                        metric: $r.metric,
                        automatic: $r.automatic,
                        exclusive: $r.exclusive,
                        comment: $r.comment,
                        createdByBROray: true,
                        managed: true,
                        routerCommentPersisted: false,
                        actualStatus: "present",
                        owners: [$bundleId],
                        createdAt: $now,
                        updatedAt: $now
                    }]
                else
                    .[$idx].owners = ((.[$idx].owners + [$bundleId]) | unique) |
                    .[$idx].actualStatus = "present" |
                    .[$idx].updatedAt = $now
                end
            )) as $routes |
        $o + {
            schemaVersion: 1,
            managedInterface: $s.targetInterface,
            managedMetric: 1200,
            routes: $routes,
            updatedAt: $now
        }
    ' >"$output/routes.json" || return 1

    jq -n --slurpfile old "$bundle_registry" --slurpfile state "$state" --slurpfile sync "$sync_plan" --arg id "$BRORAY_SYNC_BUNDLE" --arg now "$now" '
        $old[0] as $o | $state[0] as $st | $sync[0] as $s |
        $o + {
            schemaVersion: 1,
            bundleId: $id,
            installedVersion: $st.downloadedVersion,
            routeKeys: ([$s.desired[].route.key] | unique),
            managedRouteKeys: ([$s.desired[].route.key] | unique),
            externalRouteKeys: [],
            targetInterface: $s.targetInterface,
            managedMetric: 1200,
            installedAt: ($o.installedAt // $now),
            removedAt: null,
            updatedAt: $now
        }
    ' >"$output/bundle.json" || return 1

    jq --slurpfile sync "$sync_plan" --arg now "$now" --arg message "$message" --arg mode "$mode" \
        --argjson total "$total" --argjson created "$created" --argjson deleted "$deleted" \
        --argjson shared "$shared" --argjson restored "$restored" --argjson absent "$absent" --argjson unchanged "$unchanged" '
        .status = "installed" |
        .installedVersion = .downloadedVersion |
        .lastExportedAt = $now |
        .lastVerifiedAt = $now |
        .verifyResult = {
            result: "complete",
            success: true,
            message: "Набор исправен. В Keenetic найдено " + ($total | tostring) + " из " + ($total | tostring) + " маршрутов.",
            checkedAt: $now,
            contentSha256: (.downloadedVersion.contentSha256 // null),
            local: {
                valid: true,
                routeCount: $total,
                sourceFileCount: (.downloadedVersion.sourceFileCount // 0),
                duplicateRouteCount: 0,
                invalidRouteCount: 0
            },
            keenetic: {
                checked: true,
                available: true,
                status: "complete",
                expectedRouteCount: $total,
                presentRouteCount: $total,
                missingRouteCount: 0,
                conflictCount: 0,
                externalRouteCount: 0,
                complete: true,
                updatePending: false,
                mode: $mode,
                canApply: true,
                targetInterface: $sync[0].targetInterface,
                managedMetric: 1200
            }
        } |
        .preflight = $sync[0] |
        .exportResult = {
            result: "installed",
            operation: $mode,
            message: $message,
            total: $total,
            created: $created,
            deleted: $deleted,
            sharedKept: $shared,
            sharedRestored: $restored,
            alreadyAbsent: $absent,
            unchanged: $unchanged,
            targetInterface: $sync[0].targetInterface,
            managedMetric: 1200,
            configurationSaved: true,
            completedAt: $now
        } |
        .lastError = null |
        .resumeOperation = null |
        .updatedAt = $now
    ' "$state" >"$output/state.json" || return 1

    jq --arg now "$now" --arg message "$message" --arg mode "$mode" \
        --argjson created "$created" --argjson deleted "$deleted" --slurpfile sync "$sync_plan" '
        .routerApplied = true |
        .configurationSaved = true |
        .appliedAt = $now |
        .applyResult = {
            operation: $mode,
            message: $message,
            created: $created,
            deleted: $deleted,
            sharedKept: $sync[0].summary.sharedKept,
            sharedRestored: $sync[0].summary.sharedToRestore
        }
    ' "$export_plan" >"$output/export-plan.json" || return 1

    jq -n --slurpfile sync "$sync_plan" --slurpfile state "$output/state.json" --arg now "$now" --arg message "$message" \
        --argjson created "$created" --argjson deleted "$deleted" '
        {
            schemaVersion: 1,
            bundleId: $sync[0].bundleId,
            targetInterface: $sync[0].targetInterface,
            managedMetric: 1200,
            sourceCommit: $state[0].installedVersion.sourceCommit,
            contentSha256: $state[0].installedVersion.contentSha256,
            operation: $sync[0].mode,
            message: $message,
            summary: ($sync[0].summary + {created: $created, deleted: $deleted}),
            routerChanged: (($created + $deleted) > 0),
            configurationSaved: true,
            completedAt: $now
        }
    ' >"$output/router-export-result.json" || return 1

    jq -e --arg interface "$(jq -r '.targetInterface' "$sync_plan")" '
        (.schemaVersion == 1) and (.managedInterface == $interface) and (.managedMetric == 1200) and
        (([.routes[].key] | length) == ([.routes[].key] | unique | length)) and
        (all(.routes[]; .interface == $interface and .metric == 1200 and .createdByBROray == true and .managed == true and ((.owners | length) > 0)))
    ' "$output/routes.json" >/dev/null 2>&1 || return 1
    jq -e --arg id "$BRORAY_SYNC_BUNDLE" '.bundleId == $id and .installedVersion != null and ((.routeKeys | length) == (.managedRouteKeys | length))' "$output/bundle.json" >/dev/null 2>&1 || return 1
    jq -e '.status == "installed" and .installedVersion != null and .exportResult.result == "installed"' "$output/state.json" >/dev/null 2>&1 || return 1
    return 0
}

broray_routes_sync_apply()
{
    local bundle_id lock_rc sync_plan interface out err tab key network prefix mask metric status rc
    local to_create to_delete added_count deleted_count actual_after prepared original registry bundle_registry state export_plan result transaction
    local mode processed_count progress_total progress_message current_route remaining_total resume_values
    local kind

    bundle_id="${1:-}"
    broray_routes_sync_id_valid "$bundle_id" || broray_routes_sync_abort "Некорректный идентификатор набора."
    BRORAY_SYNC_BUNDLE="$bundle_id"
    export BRORAY_SYNC_BUNDLE

    lock_rc=0
    broray_routes_sync_lock_acquire || lock_rc=$?
    case "$lock_rc" in
        0) ;;
        2) broray_routes_sync_abort "Другая операция с маршрутами уже выполняется." ;;
        *) broray_routes_sync_abort "Не удалось установить блокировку операции." ;;
    esac

    BRORAY_SYNC_WORK="$BRORAY_SYNC_ROUTES/tmp/sync-apply-$bundle_id.$$"
    mkdir -p "$BRORAY_SYNC_WORK" || broray_routes_sync_abort "Не удалось создать рабочий каталог."
    BRORAY_SYNC_ADDED_FILE="$BRORAY_SYNC_WORK/added.tsv"
    BRORAY_SYNC_DELETED_FILE="$BRORAY_SYNC_WORK/deleted.tsv"
    : >"$BRORAY_SYNC_ADDED_FILE"
    : >"$BRORAY_SYNC_DELETED_FILE"
    trap 'broray_routes_sync_trap' EXIT HUP INT TERM

    broray_routes_sync_ensure_global_registry || broray_routes_sync_abort "Не удалось восстановить или проверить общий реестр маршрутов."
    sync_plan="$BRORAY_SYNC_WORK/plan.json"
    broray_routes_sync_build_plan_core "$bundle_id" "$sync_plan" || broray_routes_sync_abort "Не удалось построить безопасный план установки."
    jq -e '.canApply == true' "$sync_plan" >/dev/null 2>&1 || broray_routes_sync_abort "План содержит конфликты или внешние маршруты на Proxy0."

    interface="$(jq -r '.targetInterface' "$sync_plan")"
    if [ "${BRORAY_SYNC_SKIP_INTERFACE_OWNER:-0}" != "1" ]; then
        command -v broray_interface_owner_record_valid >/dev/null 2>&1 || broray_routes_sync_abort "Модуль владения ProxyN недоступен."
        broray_interface_owner_record_valid "$interface" || broray_routes_sync_abort "Локальный реестр владельца не подтверждает $interface."
        broray_interface_owner_valid "$interface" || broray_routes_sync_abort "Интерфейс $interface не подтверждён как принадлежащий BROray."
    fi
    broray_routes_sync_interface_check "$interface" || broray_routes_sync_abort "$interface не подключён или не находится в состоянии up."

    registry="$BRORAY_SYNC_ROUTES/installed/routes.json"
    bundle_registry="$BRORAY_SYNC_ROUTES/installed/bundles/$bundle_id.json"
    state="$BRORAY_SYNC_ROUTES/state/$bundle_id.json"
    export_plan="$BRORAY_SYNC_ROUTES/catalog/$bundle_id/export-plan.json"
    result="$BRORAY_SYNC_ROUTES/catalog/$bundle_id/router-export-result.json"
    transaction="$BRORAY_SYNC_ROUTES/transactions/$(broray_routes_sync_stamp)-sync-$bundle_id-$$"
    original="$transaction/original"
    BRORAY_SYNC_TRANSACTION="$transaction"
    BRORAY_SYNC_LOCAL_BACKUP="$original"

    mkdir -p "$original" || broray_routes_sync_abort "Не удалось создать транзакционную резервную копию."
    cp -p "$registry" "$original/routes.json" || broray_routes_sync_abort "Не удалось сохранить общий реестр."
    cp -p "$bundle_registry" "$original/bundle.json" || broray_routes_sync_abort "Не удалось сохранить реестр набора."
    cp -p "$state" "$original/state.json" || broray_routes_sync_abort "Не удалось сохранить состояние набора."
    cp -p "$export_plan" "$original/export-plan.json" || broray_routes_sync_abort "Не удалось сохранить план набора."
    if [ -f "$result" ]; then
        cp -p "$result" "$original/router-export-result.json" || broray_routes_sync_abort "Не удалось сохранить предыдущий результат."
    else
        : >"$original/result.missing"
    fi
    cp -p "$sync_plan" "$transaction/plan.json" || broray_routes_sync_abort "Не удалось сохранить безопасный план операции."
    cp -p "$BRORAY_SYNC_WORK/running-config.json" "$transaction/running-config-before.json" ||
        broray_routes_sync_abort "Не удалось сохранить снимок конфигурации Keenetic."
    chmod -R go-rwx "$transaction" 2>/dev/null || true
    broray_routes_sync_transaction_write "planned" "Безопасный план подготовлен; Keenetic ещё не изменён." ||
        broray_routes_sync_abort "Не удалось записать журнал транзакции."

    jq -r '
        (.desired[] | select(.status == "create") | ["create", .route.key, .route.network, (.route.prefix | tostring), .route.mask, .route.targetInterface, (.route.metric | tostring)]),
        (.obsolete[] | select(.status == "shared_restore") | ["shared_restore", .route.key, .route.network, (.route.prefix | tostring), .route.mask, .route.targetInterface, (.route.metric | tostring)]) |
        @tsv
    ' "$sync_plan" >"$BRORAY_SYNC_WORK/to-create.tsv" || broray_routes_sync_abort "Не удалось подготовить список добавления."

    jq -r '.obsolete[] | select(.status == "delete") | ["delete", .route.key, .route.network, (.route.prefix | tostring), .route.mask, .route.targetInterface, (.route.metric | tostring)] | @tsv' \
        "$sync_plan" >"$BRORAY_SYNC_WORK/to-delete.tsv" || broray_routes_sync_abort "Не удалось подготовить список удаления."

    to_create="$(wc -l <"$BRORAY_SYNC_WORK/to-create.tsv" | tr -d ' ')"
    to_delete="$(wc -l <"$BRORAY_SYNC_WORK/to-delete.tsv" | tr -d ' ')"
    remaining_total="$((to_create + to_delete))"
    mode="$(jq -r '.mode // "install"' "$sync_plan")"
    resume_values="0	$remaining_total	false"
    if command -v broray_routes_progress_resume_values >/dev/null 2>&1; then
        resume_values="$(broray_routes_progress_resume_values "$bundle_id" "$mode" "$remaining_total")" ||
            resume_values="0	$remaining_total	false"
    fi
    BRORAY_SYNC_RESUME_BASE="$(printf '%s' "$resume_values" | cut -f1)"
    progress_total="$(printf '%s' "$resume_values" | cut -f2)"
    BRORAY_SYNC_RESUMED="$(printf '%s' "$resume_values" | cut -f3)"
    case "$BRORAY_SYNC_RESUME_BASE" in ''|*[!0-9]*) BRORAY_SYNC_RESUME_BASE=0 ;; esac
    case "$progress_total" in ''|*[!0-9]*) progress_total="$remaining_total" ;; esac
    case "$BRORAY_SYNC_RESUMED" in true|false) ;; *) BRORAY_SYNC_RESUMED=false ;; esac
    processed_count="$BRORAY_SYNC_RESUME_BASE"
    case "$mode" in
        install) progress_message="Установка маршрутов в Keenetic." ;;
        update) progress_message="Обновление маршрутов в Keenetic." ;;
        restore) progress_message="Восстановление маршрутов в Keenetic." ;;
        *) mode="install"; progress_message="Установка маршрутов в Keenetic." ;;
    esac
    BRORAY_SYNC_PROGRESS_CURRENT=0
    BRORAY_SYNC_PROGRESS_TOTAL="$progress_total"
    broray_routes_sync_progress_begin \
        "$bundle_id" "$mode" "$progress_total" \
        "Подготовка операции: $processed_count из $progress_total." \
        "$processed_count" "$BRORAY_SYNC_RESUMED" ||
        broray_routes_sync_abort "Не удалось создать состояние прогресса операции."
    broray_routes_sync_progress_update \
        "applying" "$processed_count" "$progress_total" "$progress_message" "" ||
        broray_routes_sync_abort "Не удалось обновить состояние прогресса операции."

    BRORAY_SYNC_ROUTER_CHANGED=true
    tab="$(printf '\t')"

    while IFS="$tab" read -r kind key network prefix mask interface metric; do
        [ -n "$network" ] || continue
        out="$BRORAY_SYNC_WORK/add.out"
        err="$BRORAY_SYNC_WORK/add.err"
        rc=0
        broray_routes_sync_ndmc "ip route $network $mask $interface $metric" "$out" "$err" 10 || rc=$?
        [ "$rc" -eq 0 ] || broray_routes_sync_abort "Не удалось добавить маршрут $network/$prefix."
        grep -Eiq 'Io::Netlink error|system failed|invalid command|unknown command' "$out" "$err" 2>/dev/null && broray_routes_sync_abort "Keenetic вернул ошибку при добавлении $network/$prefix."
        grep -Fq "Added static route: $network/$prefix via $interface." "$out" || broray_routes_sync_abort "Keenetic не подтвердил создание $network/$prefix."
        printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$key" "$network" "$prefix" "$mask" "$interface" "$metric" >>"$BRORAY_SYNC_ADDED_FILE"
        processed_count=$((processed_count + 1))
        BRORAY_SYNC_PROGRESS_CURRENT="$processed_count"
        current_route="$network/$prefix"
        broray_routes_sync_progress_tick \
            "$processed_count" "$current_route" ||
            broray_routes_sync_abort "Не удалось записать прогресс установки маршрутов."
        broray_routes_sync_stop_if_requested "$sync_plan"
    done <"$BRORAY_SYNC_WORK/to-create.tsv"

    while IFS="$tab" read -r kind key network prefix mask interface metric; do
        [ -n "$network" ] || continue
        out="$BRORAY_SYNC_WORK/delete.out"
        err="$BRORAY_SYNC_WORK/delete.err"
        rc=0
        broray_routes_sync_ndmc "no ip route $network $mask $interface" "$out" "$err" 10 || rc=$?
        if grep -Fq "Deleted static route: $network/$prefix via $interface." "$out"; then
            printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$key" "$network" "$prefix" "$mask" "$interface" "$metric" >>"$BRORAY_SYNC_DELETED_FILE"
        elif [ "$rc" = "122" ] && cat "$out" "$err" 2>/dev/null | grep -Fq 'got an error response: file exists.'; then
            printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$key" "$network" "$prefix" "$mask" "$interface" "$metric" >>"$BRORAY_SYNC_DELETED_FILE"
        elif cat "$out" "$err" 2>/dev/null | grep -Fq 'No such route:'; then
            :
        else
            broray_routes_sync_route_failure "$sync_plan" "Не удалось удалить устаревший маршрут $network/$prefix." "$network/$prefix"
        fi
        processed_count=$((processed_count + 1))
        BRORAY_SYNC_PROGRESS_CURRENT="$processed_count"
        current_route="$network/$prefix"
        broray_routes_sync_progress_tick \
            "$processed_count" "$current_route" ||
            broray_routes_sync_abort "Не удалось записать прогресс обновления маршрутов."
        broray_routes_sync_stop_if_requested "$sync_plan"
    done <"$BRORAY_SYNC_WORK/to-delete.tsv"

    broray_routes_sync_progress_update \
        "verifying" "$processed_count" "$progress_total" \
        "Проверка маршрутов в Keenetic." "" ||
        broray_routes_sync_abort "Не удалось записать этап проверки маршрутов."

    actual_after="$BRORAY_SYNC_WORK/running-config-after.json"
    BRORAY_ROUTES_CONFIG_NDMC="$BRORAY_SYNC_NDMC"
    export BRORAY_ROUTES_CONFIG_NDMC
    rm -f "${BRORAY_ROUTES_CONFIG_CACHE:-}" 2>/dev/null || true
    broray_routes_config_fetch "$actual_after" || broray_routes_sync_abort "Не удалось повторно прочитать running-config после изменений."

    jq -n -e \
        --slurpfile plan "$sync_plan" \
        --slurpfile actual "$actual_after" '
        $plan[0] as $p |
        $actual[0] as $a |
        def exact($r):
            [
                $a.routes[]? |
                select(
                    .destination == ($r.network + "/" + ($r.prefix | tostring)) and
                    .interface == $p.targetInterface and
                    ((.gateway // "0.0.0.0") == "0.0.0.0") and
                    ((.metric // 1000) == 1200) and
                    (((.proto // "static") | ascii_downcase) == "static")
                )
            ] |
            length;
        (all($p.desired[]; exact(.route) == 1)) and
        (all(
            $p.obsolete[] |
            select(.status == "shared_keep" or .status == "shared_restore");
            exact(.route) == 1
        )) and
        (all(
            $p.obsolete[] |
            select(.status == "delete" or .status == "already_absent");
            exact(.route) == 0
        ))
    ' >/dev/null 2>&1 ||
        broray_routes_sync_abort "Итоговая конфигурация Keenetic не совпала с безопасным планом."

    cp -p "$actual_after" "$transaction/running-config-after.json" ||
        broray_routes_sync_abort "Не удалось сохранить итоговый снимок конфигурации Keenetic."
    broray_routes_sync_transaction_write "router_verified" "Изменения в Keenetic применены и проверены." ||
        broray_routes_sync_abort "Не удалось обновить журнал транзакции."

    registry="$BRORAY_SYNC_ROUTES/installed/routes.json"
    bundle_registry="$BRORAY_SYNC_ROUTES/installed/bundles/$bundle_id.json"
    state="$BRORAY_SYNC_ROUTES/state/$bundle_id.json"
    export_plan="$BRORAY_SYNC_ROUTES/catalog/$bundle_id/export-plan.json"
    result="$BRORAY_SYNC_ROUTES/catalog/$bundle_id/router-export-result.json"
    prepared="$BRORAY_SYNC_WORK/prepared"

    broray_routes_sync_prepare_local "$sync_plan" "$prepared" || broray_routes_sync_abort "Не удалось подготовить новые локальные реестры."

    cp -p "$prepared/routes.json" "$registry.new.$$" && mv "$registry.new.$$" "$registry" || broray_routes_sync_abort "Не удалось записать общий реестр."
    cp -p "$prepared/bundle.json" "$bundle_registry.new.$$" && mv "$bundle_registry.new.$$" "$bundle_registry" || broray_routes_sync_abort "Не удалось записать реестр набора."
    cp -p "$prepared/state.json" "$state.new.$$" && mv "$state.new.$$" "$state" || broray_routes_sync_abort "Не удалось записать состояние набора."
    cp -p "$prepared/export-plan.json" "$export_plan.new.$$" && mv "$export_plan.new.$$" "$export_plan" || broray_routes_sync_abort "Не удалось записать план набора."
    cp -p "$prepared/router-export-result.json" "$result.new.$$" && mv "$result.new.$$" "$result" || broray_routes_sync_abort "Не удалось записать результат установки."
    chmod 644 "$registry" "$bundle_registry" "$state" "$export_plan" "$result" 2>/dev/null || true
    BRORAY_SYNC_LOCAL_COMMITTED=true
    broray_routes_sync_transaction_write "local_committed" "Локальные реестры обновлены." ||
        broray_routes_sync_abort "Не удалось обновить журнал транзакции."

    broray_routes_sync_progress_update \
        "saving" "$processed_count" "$progress_total" \
        "Сохранение конфигурации Keenetic." "" ||
        broray_routes_sync_abort "Не удалось записать этап сохранения конфигурации."
    broray_routes_sync_save_config || broray_routes_sync_abort "Не удалось сохранить конфигурацию Keenetic."
    broray_routes_sync_transaction_write "committed" "Маршруты применены, проверены и сохранены." ||
        broray_routes_sync_abort "Не удалось завершить журнал транзакции."

    broray_routes_sync_progress_complete \
        "$progress_message Выполнено $progress_total из $progress_total." || true

    added_count="$(wc -l <"$BRORAY_SYNC_ADDED_FILE" | tr -d ' ')"
    deleted_count="$(wc -l <"$BRORAY_SYNC_DELETED_FILE" | tr -d ' ')"

    BRORAY_SYNC_ROUTER_CHANGED=false
    BRORAY_SYNC_LOCAL_COMMITTED=false
    trap - EXIT HUP INT TERM
    broray_routes_sync_lock_release

    printf '%s\n' "$(jq -r '.message' "$result")"
    printf 'Набор: %s\n' "$bundle_id"
    printf 'Управляемый интерфейс: %s\n' "$(jq -r '.targetInterface' "$result")"
    printf 'Всего маршрутов: %s\n' "$(jq -r '.summary.total' "$result")"
    printf 'Добавлено: %s\n' "$added_count"
    printf 'Удалено: %s\n' "$deleted_count"
    printf 'Сохранено общих: %s\n' "$(jq -r '.summary.sharedKept' "$result")"
    printf 'Конфигурация сохранена: true\n'

    broray_routes_sync_cleanup
    return 0
}
