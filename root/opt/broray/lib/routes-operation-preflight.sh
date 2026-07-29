#!/opt/bin/ash

# Builds and validates short-lived confirmations for destructive or long-running
# route operations. The actual operation always rebuilds its plan under the
# low-level route lock; this token only proves that the user confirmed a fresh,
# successful preflight for the same bundle and operation.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_ROUTES_OPERATION_PREFLIGHT_DIR="${BRORAY_ROUTES_OPERATION_PREFLIGHT_DIR:-$BRORAY_ROUTES_ROOT/preflight}"
BRORAY_ROUTES_OPERATION_PREFLIGHT_TTL="${BRORAY_ROUTES_OPERATION_PREFLIGHT_TTL:-120}"
BRORAY_ROUTES_OPERATION_PREFLIGHT_CONFIG_LIBRARY="${BRORAY_ROUTES_OPERATION_PREFLIGHT_CONFIG_LIBRARY:-$BRORAY_ROOT/lib/routes-router-config.sh}"
BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC="${BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC:-ndmc}"

broray_routes_operation_preflight_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_operation_preflight_epoch()
{
    date '+%s'
}

broray_routes_operation_preflight_bundle_valid()
{
    case "${1:-}" in
        ''|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
            return 1
            ;;
    esac
    return 0
}

broray_routes_operation_preflight_token_valid()
{
    local token
    token="${1:-}"
    [ "${#token}" -eq 64 ] || return 1
    case "$token" in *[!0-9a-f]*) return 1 ;; esac
    return 0
}

broray_routes_operation_preflight_uint()
{
    case "${1:-}" in ''|*[!0-9]*) return 1 ;; esac
    return 0
}

broray_routes_operation_preflight_ndmc_path()
{
    case "$BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC" in
        */*) printf '%s\n' "$BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC" ;;
        *) command -v "$BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC" 2>/dev/null || true ;;
    esac
}

broray_routes_operation_preflight_resolve_action()
{
    local bundle requested progress operation

    bundle="${1:-}"
    requested="${2:-}"
    broray_routes_operation_preflight_bundle_valid "$bundle" || return 1

    case "$requested" in
        export) printf '%s\t\n' export ;;
        delete) printf '%s\t%s\n' delete delete ;;
        resume)
            progress="$BRORAY_ROUTES_ROOT/operations/$bundle.json"
            [ -r "$progress" ] || return 2
            jq -e '.resumable == true and .running == false' "$progress" >/dev/null 2>&1 || return 2
            operation="$(jq -r '.operation // empty' "$progress" 2>/dev/null)"
            case "$operation" in
                install|update|restore) printf '%s\t%s\n' export "$operation" ;;
                delete) printf '%s\t%s\n' delete delete ;;
                *) return 2 ;;
            esac
            ;;
        *) return 1 ;;
    esac
}

broray_routes_operation_preflight_delete_plan()
{
    local bundle output config registry bundle_registry actual temp interface metric rc

    bundle="${1:-}"
    output="${2:-}"
    broray_routes_operation_preflight_bundle_valid "$bundle" || return 1
    [ -n "$output" ] || return 1

    config="$BRORAY_ROUTES_ROOT/config.json"
    registry="$BRORAY_ROUTES_ROOT/installed/routes.json"
    bundle_registry="$BRORAY_ROUTES_ROOT/installed/bundles/$bundle.json"
    [ -r "$config" ] && [ -r "$registry" ] && [ -r "$bundle_registry" ] || return 1

    interface="$(jq -r '.managedInterface // empty' "$config" 2>/dev/null)"
    metric="$(jq -r '.managedMetric // empty' "$config" 2>/dev/null)"
    case "$interface" in Proxy[0-9]*) ;; *) return 1 ;; esac
    case "${interface#Proxy}" in ''|*[!0-9]*) return 1 ;; esac
    [ "$metric" = 1200 ] || return 1

    jq -e --arg id "$bundle" --arg interface "$interface" '
        (.schemaVersion == 1) and (.bundleId == $id) and
        (.installedVersion != null) and (.targetInterface == $interface) and
        (.managedMetric == 1200) and
        ((.routeKeys | type) == "array") and
        ((.managedRouteKeys | type) == "array") and
        ((.externalRouteKeys | type) == "array")
    ' "$bundle_registry" >/dev/null 2>&1 || return 1

    jq -e --arg interface "$interface" '
        (.schemaVersion == 1) and (.managedInterface == $interface) and
        (.managedMetric == 1200) and ((.routes | type) == "array")
    ' "$registry" >/dev/null 2>&1 || return 1

    [ -r "$BRORAY_ROUTES_OPERATION_PREFLIGHT_CONFIG_LIBRARY" ] || return 1
    . "$BRORAY_ROUTES_OPERATION_PREFLIGHT_CONFIG_LIBRARY"
    temp="$output.actual.$$"
    actual="$temp.json"
    BRORAY_ROUTES_CONFIG_NDMC="$BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC"
    export BRORAY_ROUTES_CONFIG_NDMC
    rm -f "${BRORAY_ROUTES_CONFIG_CACHE:-}" 2>/dev/null || true
    broray_routes_config_fetch "$actual" || {
        rm -f "$actual" "$actual.raw" "$actual.tsv" "$actual.err"
        return 1
    }

    jq -n \
        --slurpfile global "$registry" \
        --slurpfile bundle "$bundle_registry" \
        --slurpfile actual "$actual" \
        --arg bundleId "$bundle" \
        --arg interface "$interface" '
        $global[0] as $g |
        $bundle[0] as $b |
        $actual[0] as $a |
        def exact_matches($route):
            [
                $a.routes[]? |
                select(
                    .network == $route.network and
                    .prefix == $route.prefix and
                    .interface == $interface and
                    ((.gateway // "0.0.0.0") == "0.0.0.0") and
                    ((.metric // 1000) == 1200) and
                    (((.proto // "static") | ascii_downcase) == "static")
                )
            ];
        [
            $b.managedRouteKeys[] as $key |
            ([$g.routes[]? | select(.key == $key)]) as $registered |
            if ($registered | length) != 1 then
                {key:$key,status:"conflict",reason:"registry_mismatch"}
            else
                $registered[0] as $route |
                (exact_matches($route)) as $exact |
                {
                    key: $key,
                    route: $route,
                    exactMatchCount: ($exact | length),
                    status: (
                        if ($route.createdByBROray != true) or ($route.managed != true) or
                           ($route.interface != $interface) or (($route.metric // 0) != 1200) or
                           ((($route.owners // []) | index($bundleId)) == null)
                        then "conflict"
                        elif (($route.owners // []) | length) > 1 then "shared_keep"
                        elif ($exact | length) == 1 then "delete"
                        elif ($exact | length) == 0 then "already_absent"
                        else "conflict"
                        end
                    )
                }
            end
        ] as $items |
        ([$items[] | select(.status == "conflict")]) as $blocking |
        {
            schemaVersion: 1,
            bundleId: $bundleId,
            mode: "delete",
            targetInterface: $interface,
            managedMetric: 1200,
            contentSha256: ($b.installedVersion.contentSha256 // null),
            canApply: (($blocking | length) == 0),
            summary: {
                total: (($b.routeKeys // []) | length),
                toCreate: 0,
                managedExisting: 0,
                toDelete: ([$items[] | select(.status == "delete")] | length),
                sharedKept: ([$items[] | select(.status == "shared_keep")] | length),
                alreadyAbsent: ([$items[] | select(.status == "already_absent")] | length),
                conflicts: ($blocking | length),
                externalExisting: (($b.externalRouteKeys // []) | length),
                unchangedRoutes: 0,
                addedRoutes: 0,
                removedRoutes: (($b.routeKeys // []) | length)
            },
            items: $items,
            blocking: $blocking,
            message: (
                if ($blocking | length) > 0 then
                    "Удаление заблокировано: реестр владения или конфигурация Keenetic содержит конфликт."
                else
                    "Предварительная проверка удаления завершена."
                end
            )
        }
    ' >"$output"
    rc=$?
    rm -f "$actual" "$actual.raw" "$actual.tsv" "$actual.err"
    [ "$rc" -eq 0 ] || return 1

    jq -e --arg id "$bundle" '
        (.schemaVersion == 1) and (.bundleId == $id) and
        (.mode == "delete") and ((.canApply | type) == "boolean") and
        ((.summary | type) == "object")
    ' "$output" >/dev/null 2>&1
}

broray_routes_operation_preflight_storage_values()
{
    local bundle routes catalog_kb free_kb required_kb

    bundle="${1:-}"
    routes="${2:-0}"
    broray_routes_operation_preflight_uint "$routes" || routes=0
    catalog_kb="$(du -sk "$BRORAY_ROUTES_ROOT/catalog/$bundle" 2>/dev/null | awk 'NR == 1 {print $1}')"
    free_kb="$(df -Pk "$BRORAY_ROOT" 2>/dev/null | awk 'NR == 2 {print $4}')"
    broray_routes_operation_preflight_uint "$catalog_kb" || catalog_kb=0
    broray_routes_operation_preflight_uint "$free_kb" || free_kb=0
    required_kb=$((2048 + catalog_kb * 2 + (routes + 3) / 4))
    [ "$required_kb" -ge 4096 ] || required_kb=4096
    printf '%s\t%s\n' "$free_kb" "$required_kb"
}

broray_routes_operation_preflight_finalize()
{
    local bundle requested resolved saved_operation plan output state bundle_registry progress
    local checked_at checked_epoch interface metric content_sha mode route_count plan_ready
    local ndmc_path interface_out interface_err keenetic_available connected state_up interface_ok
    local storage_values free_kb required_kb storage_ok local_ok ready token canonical
    local resume_current resume_total resume_operation preflight_file temp message

    bundle="${1:-}"
    requested="${2:-}"
    resolved="${3:-}"
    saved_operation="${4:-}"
    plan="${5:-}"
    output="${6:-}"
    broray_routes_operation_preflight_bundle_valid "$bundle" || return 1
    case "$requested" in export|delete|resume) ;; *) return 1 ;; esac
    case "$resolved" in export|delete) ;; *) return 1 ;; esac
    [ -r "$plan" ] && [ -n "$output" ] || return 1

    state="$BRORAY_ROUTES_ROOT/state/$bundle.json"
    bundle_registry="$BRORAY_ROUTES_ROOT/installed/bundles/$bundle.json"
    progress="$BRORAY_ROUTES_ROOT/operations/$bundle.json"
    [ -r "$state" ] && [ -r "$bundle_registry" ] || return 1

    jq -e --arg id "$bundle" '
        (.schemaVersion == 1) and (.bundleId == $id) and
        ((.canApply | type) == "boolean") and ((.summary | type) == "object")
    ' "$plan" >/dev/null 2>&1 || return 1

    checked_at="$(broray_routes_operation_preflight_now)"
    checked_epoch="$(broray_routes_operation_preflight_epoch)"
    interface="$(jq -r '.targetInterface // empty' "$plan")"
    metric="$(jq -r '.managedMetric // 0' "$plan")"
    mode="$(jq -r '.mode // empty' "$plan")"
    route_count="$(jq -r '.summary.total // 0' "$plan")"
    plan_ready="$(jq -r '.canApply' "$plan")"
    broray_routes_operation_preflight_uint "$route_count" || return 1
    [ "$metric" = 1200 ] || return 1

    if [ "$resolved" = delete ]; then
        content_sha="$(jq -r '.installedVersion.contentSha256 // empty' "$bundle_registry")"
        mode=delete
    else
        content_sha="$(jq -r '.contentSha256 // empty' "$plan")"
        [ -n "$content_sha" ] || content_sha="$(jq -r '.downloadedVersion.contentSha256 // empty' "$state")"
    fi

    ndmc_path="$(broray_routes_operation_preflight_ndmc_path)"
    interface_out="$output.interface.out"
    interface_err="$output.interface.err"
    keenetic_available=false
    connected=false
    state_up=false
    interface_ok=false
    if [ -n "$ndmc_path" ] && [ -x "$ndmc_path" ]; then
        if "$ndmc_path" -c "show interface $interface" >"$interface_out" 2>"$interface_err"; then
            keenetic_available=true
            grep -Eq "^[[:space:]]*id:[[:space:]]*$interface[[:space:]]*$" "$interface_out" &&
                grep -Eq '^[[:space:]]*connected:[[:space:]]*yes[[:space:]]*$' "$interface_out" && connected=true
            grep -Eq '^[[:space:]]*state:[[:space:]]*up[[:space:]]*$' "$interface_out" && state_up=true
            [ "$connected" = true ] && [ "$state_up" = true ] && interface_ok=true
        fi
    fi

    storage_values="$(broray_routes_operation_preflight_storage_values "$bundle" "$route_count")"
    free_kb="$(printf '%s' "$storage_values" | cut -f1)"
    required_kb="$(printf '%s' "$storage_values" | cut -f2)"
    storage_ok=false
    [ "$free_kb" -ge "$required_kb" ] 2>/dev/null && storage_ok=true
    local_ok=false
    [ -n "$content_sha" ] && [ "$route_count" -gt 0 ] && local_ok=true

    resume_current=0
    resume_total=0
    resume_operation=""
    if [ "$requested" = resume ]; then
        [ -r "$progress" ] || return 1
        resume_current="$(jq -r '.current // 0' "$progress")"
        resume_total="$(jq -r '.total // 0' "$progress")"
        resume_operation="$(jq -r '.operation // empty' "$progress")"
        broray_routes_operation_preflight_uint "$resume_current" || return 1
        broray_routes_operation_preflight_uint "$resume_total" || return 1
        [ "$resume_current" -le "$resume_total" ] || return 1
    fi

    ready=false
    if [ "$plan_ready" = true ] && [ "$interface_ok" = true ] &&
       [ "$storage_ok" = true ] && [ "$local_ok" = true ]
    then
        ready=true
    fi

    canonical="$bundle|$requested|$resolved|$saved_operation|$mode|$checked_epoch|$content_sha|$route_count|$resume_current|$resume_total|$$"
    token="$(printf '%s' "$canonical" | sha256sum | awk '{print $1}')"
    broray_routes_operation_preflight_token_valid "$token" || return 1

    if [ "$ready" = true ]; then
        message="Предварительная проверка завершена. Операция готова к запуску."
    elif [ "$plan_ready" != true ]; then
        message="Операция заблокирована: обнаружены конфликты маршрутов или владения."
    elif [ "$interface_ok" != true ]; then
        message="Операция заблокирована: Keenetic или управляемый интерфейс недоступен."
    elif [ "$storage_ok" != true ]; then
        message="Операция заблокирована: недостаточно свободного места в /opt."
    else
        message="Операция заблокирована: локальный набор не готов."
    fi

    jq -n \
        --slurpfile plan "$plan" \
        --arg token "$token" \
        --arg bundleId "$bundle" \
        --arg requestedAction "$requested" \
        --arg resolvedAction "$resolved" \
        --arg operation "$saved_operation" \
        --arg mode "$mode" \
        --arg checkedAt "$checked_at" \
        --arg contentSha256 "$content_sha" \
        --arg targetInterface "$interface" \
        --arg ndmcPath "$ndmc_path" \
        --arg message "$message" \
        --arg resumeOperation "$resume_operation" \
        --argjson checkedEpoch "$checked_epoch" \
        --argjson expiresAfterSeconds "$BRORAY_ROUTES_OPERATION_PREFLIGHT_TTL" \
        --argjson ready "$ready" \
        --argjson keeneticAvailable "$keenetic_available" \
        --argjson connected "$connected" \
        --argjson stateUp "$state_up" \
        --argjson interfaceOk "$interface_ok" \
        --argjson storageOk "$storage_ok" \
        --argjson freeKb "$free_kb" \
        --argjson requiredKb "$required_kb" \
        --argjson localOk "$local_ok" \
        --argjson routeCount "$route_count" \
        --argjson resumeCurrent "$resume_current" \
        --argjson resumeTotal "$resume_total" '
        $plan[0] as $p |
        {
            schemaVersion: 1,
            token: $token,
            bundleId: $bundleId,
            requestedAction: $requestedAction,
            resolvedAction: $resolvedAction,
            operation: (if $operation == "" then $mode else $operation end),
            mode: $mode,
            ready: $ready,
            checkedAt: $checkedAt,
            checkedEpoch: $checkedEpoch,
            expiresAfterSeconds: $expiresAfterSeconds,
            contentSha256: (if $contentSha256 == "" then null else $contentSha256 end),
            targetInterface: $targetInterface,
            managedMetric: 1200,
            checks: {
                operationLock: {ok:true, message:"Конфликтующих операций не обнаружено."},
                ndmc: {ok:($ndmcPath != ""), path:(if $ndmcPath == "" then null else $ndmcPath end)},
                keenetic: {
                    ok:$interfaceOk,
                    available:$keeneticAvailable,
                    interface:$targetInterface,
                    connected:$connected,
                    stateUp:$stateUp
                },
                storage: {ok:$storageOk, freeKb:$freeKb, requiredKb:$requiredKb},
                localSet: {ok:$localOk, routeCount:$routeCount, duplicateRouteCount:0, invalidRouteCount:0},
                ownership: {ok:$p.canApply, conflictCount:($p.summary.conflicts // 0)}
            },
            summary: {
                total: ($p.summary.total // 0),
                alreadyPresent: ($p.summary.managedExisting // 0),
                toCreate: ($p.summary.toCreate // 0),
                toDelete: ($p.summary.toDelete // 0),
                sharedKept: ($p.summary.sharedKept // 0),
                alreadyAbsent: ($p.summary.alreadyAbsent // 0),
                conflicts: ($p.summary.conflicts // 0),
                externalKept: ($p.summary.externalExisting // 0),
                unchanged: ($p.summary.unchangedRoutes // 0),
                addedToBundle: ($p.summary.addedRoutes // 0),
                removedFromBundle: ($p.summary.removedRoutes // 0)
            },
            resume: {
                operation: (if $resumeOperation == "" then null else $resumeOperation end),
                current: $resumeCurrent,
                total: $resumeTotal
            },
            message: $message
        }
    ' >"$output" || return 1
    rm -f "$interface_out" "$interface_err"

    mkdir -p "$BRORAY_ROUTES_OPERATION_PREFLIGHT_DIR" || return 1
    preflight_file="$BRORAY_ROUTES_OPERATION_PREFLIGHT_DIR/$bundle.json"
    temp="$preflight_file.new.$$"
    cp -p "$output" "$temp" || { rm -f "$temp"; return 1; }
    chmod 600 "$temp" 2>/dev/null || true
    mv -f "$temp" "$preflight_file" || return 1
    return 0
}

broray_routes_operation_preflight_validate()
{
    local bundle requested token file now checked ttl age content_sha current_sha progress
    local saved_current saved_total current total saved_operation operation

    bundle="${1:-}"
    requested="${2:-}"
    token="${3:-}"
    broray_routes_operation_preflight_bundle_valid "$bundle" || return 1
    case "$requested" in export|delete|resume) ;; *) return 1 ;; esac
    broray_routes_operation_preflight_token_valid "$token" || return 2
    file="$BRORAY_ROUTES_OPERATION_PREFLIGHT_DIR/$bundle.json"
    [ -r "$file" ] || return 2

    jq -e \
        --arg bundle "$bundle" \
        --arg requested "$requested" \
        --arg token "$token" '
        (.schemaVersion == 1) and (.bundleId == $bundle) and
        (.requestedAction == $requested) and (.token == $token) and
        (.ready == true) and ((.checkedEpoch | type) == "number") and
        ((.expiresAfterSeconds | type) == "number")
    ' "$file" >/dev/null 2>&1 || return 2

    now="$(broray_routes_operation_preflight_epoch)"
    checked="$(jq -r '.checkedEpoch' "$file")"
    ttl="$(jq -r '.expiresAfterSeconds' "$file")"
    broray_routes_operation_preflight_uint "$checked" || return 3
    broray_routes_operation_preflight_uint "$ttl" || return 3
    age=$((now - checked))
    [ "$age" -ge 0 ] && [ "$age" -le "$ttl" ] || return 3

    content_sha="$(jq -r '.contentSha256 // empty' "$file")"
    case "$requested" in
        export)
            current_sha="$(jq -r '.downloadedVersion.contentSha256 // empty' "$BRORAY_ROUTES_ROOT/state/$bundle.json" 2>/dev/null)"
            [ -n "$current_sha" ] && [ "$current_sha" = "$content_sha" ] || return 4
            ;;
        delete)
            current_sha="$(jq -r '.installedVersion.contentSha256 // empty' "$BRORAY_ROUTES_ROOT/installed/bundles/$bundle.json" 2>/dev/null)"
            [ -n "$current_sha" ] && [ "$current_sha" = "$content_sha" ] || return 4
            ;;
        resume)
            progress="$BRORAY_ROUTES_ROOT/operations/$bundle.json"
            [ -r "$progress" ] || return 4
            saved_current="$(jq -r '.resume.current // 0' "$file")"
            saved_total="$(jq -r '.resume.total // 0' "$file")"
            saved_operation="$(jq -r '.resume.operation // empty' "$file")"
            current="$(jq -r '.current // 0' "$progress")"
            total="$(jq -r '.total // 0' "$progress")"
            operation="$(jq -r '.operation // empty' "$progress")"
            [ "$saved_current" = "$current" ] && [ "$saved_total" = "$total" ] &&
                [ "$saved_operation" = "$operation" ] || return 4
            ;;
    esac

    rm -f "$file" 2>/dev/null || return 1
    return 0
}
