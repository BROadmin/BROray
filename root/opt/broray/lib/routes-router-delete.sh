#!/opt/bin/ash

# BROray owned route deletion v2 (Keenetic destination + interface identity)

BRORAY_ROUTES_DELETE_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_DELETE_ROUTES="$BRORAY_ROUTES_DELETE_ROOT/routes"
BRORAY_ROUTES_DELETE_LOCK="$BRORAY_ROUTES_DELETE_ROUTES/locks/operation.lock"
BRORAY_ROUTES_DELETE_ACTIVE_PID=""
BRORAY_ROUTES_DELETE_LOCK_OWNED=false
BRORAY_ROUTES_DELETE_COMMITTED=false
BRORAY_ROUTES_DELETE_ROLLBACK_NEEDED=false
BRORAY_ROUTES_DELETE_DELETED_FILE=""
BRORAY_ROUTES_DELETE_WORK=""
BRORAY_ROUTES_DELETE_ORIGINAL=""
BRORAY_ROUTES_DELETE_BUNDLE_ID=""
BRORAY_ROUTES_DELETE_INTERFACE="${BRORAY_ROUTES_DELETE_INTERFACE:-Proxy0}"
BRORAY_ROUTES_DELETE_NDMC=""

BRORAY_ROOT="${BRORAY_ROOT:-$BRORAY_ROUTES_DELETE_ROOT}"
BRORAY_INTERFACE_OWNER_LIBRARY="${BRORAY_INTERFACE_OWNER_LIBRARY:-$BRORAY_ROOT/lib/interface-owner.sh}"
if [ -r "$BRORAY_INTERFACE_OWNER_LIBRARY" ]; then
    BRORAY_BASE="$BRORAY_ROOT"
    export BRORAY_BASE
    . "$BRORAY_INTERFACE_OWNER_LIBRARY"
fi

broray_routes_delete_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_delete_stamp()
{
    date '+%Y%m%d-%H%M%S'
}

broray_routes_delete_error()
{
    echo "ОШИБКА: $*" >&2
}

broray_routes_delete_bundle_id_valid()
{
    case "$1" in
        ""|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
            return 1
            ;;
    esac

    return 0
}

broray_routes_delete_is_pid()
{
    case "$1" in
        ""|*[!0-9]*) return 1 ;;
        *) return 0 ;;
    esac
}

broray_routes_delete_lock_acquire()
{
    local bundle_id owner_pid now

    bundle_id="$1"
    mkdir -p "$(dirname "$BRORAY_ROUTES_DELETE_LOCK")" || return 1

    if mkdir "$BRORAY_ROUTES_DELETE_LOCK" 2>/dev/null; then
        :
    else
        owner_pid="$(cat "$BRORAY_ROUTES_DELETE_LOCK/pid" 2>/dev/null || true)"

        if broray_routes_delete_is_pid "$owner_pid" &&
           kill -0 "$owner_pid" 2>/dev/null
        then
            broray_routes_delete_error \
                "Другая операция с маршрутами уже выполняется."
            return 1
        fi

        rm -rf "$BRORAY_ROUTES_DELETE_LOCK" 2>/dev/null || return 1
        mkdir "$BRORAY_ROUTES_DELETE_LOCK" 2>/dev/null || {
            broray_routes_delete_error \
                "Не удалось получить блокировку маршрутов."
            return 1
        }
    fi

    now="$(broray_routes_delete_now)"
    printf '%s\n' "$$" >"$BRORAY_ROUTES_DELETE_LOCK/pid" || return 1
    printf '%s\n' "delete" >"$BRORAY_ROUTES_DELETE_LOCK/operation" || return 1
    printf '%s\n' "$bundle_id" >"$BRORAY_ROUTES_DELETE_LOCK/bundle" || return 1
    printf '%s\n' "$now" >"$BRORAY_ROUTES_DELETE_LOCK/startedAt" || return 1

    BRORAY_ROUTES_DELETE_LOCK_OWNED=true
    return 0
}

broray_routes_delete_lock_release()
{
    if [ "$BRORAY_ROUTES_DELETE_LOCK_OWNED" = true ]; then
        rm -rf "$BRORAY_ROUTES_DELETE_LOCK" 2>/dev/null || true
        BRORAY_ROUTES_DELETE_LOCK_OWNED=false
    fi
}

broray_routes_delete_kill_active()
{
    [ -n "$BRORAY_ROUTES_DELETE_ACTIVE_PID" ] || return 0

    if kill -0 "$BRORAY_ROUTES_DELETE_ACTIVE_PID" 2>/dev/null; then
        kill "$BRORAY_ROUTES_DELETE_ACTIVE_PID" 2>/dev/null || true
        sleep 1

        if kill -0 "$BRORAY_ROUTES_DELETE_ACTIVE_PID" 2>/dev/null; then
            kill -9 "$BRORAY_ROUTES_DELETE_ACTIVE_PID" 2>/dev/null || true
        fi
    fi

    wait "$BRORAY_ROUTES_DELETE_ACTIVE_PID" 2>/dev/null || true
    BRORAY_ROUTES_DELETE_ACTIVE_PID=""
}

broray_routes_delete_ndmc()
{
    local command_text output_file error_file limit elapsed rc

    command_text="$1"
    output_file="$2"
    error_file="$3"
    limit="${4:-8}"

    : >"$output_file"
    : >"$error_file"

    "$BRORAY_ROUTES_DELETE_NDMC" -c "$command_text" >"$output_file" 2>"$error_file" &
    BRORAY_ROUTES_DELETE_ACTIVE_PID=$!
    elapsed=0

    while kill -0 "$BRORAY_ROUTES_DELETE_ACTIVE_PID" 2>/dev/null
    do
        if [ "$elapsed" -ge "$limit" ]; then
            broray_routes_delete_kill_active
            return 124
        fi

        sleep 1
        elapsed=$((elapsed + 1))
    done

    if wait "$BRORAY_ROUTES_DELETE_ACTIVE_PID" 2>/dev/null; then
        rc=0
    else
        rc=$?
    fi

    BRORAY_ROUTES_DELETE_ACTIVE_PID=""
    return "$rc"
}

broray_routes_delete_transaction_write()
{
    local file phase deleted_count saved committed message now new

    file="$1"
    phase="$2"
    deleted_count="$3"
    saved="$4"
    committed="$5"
    message="$6"
    now="$(broray_routes_delete_now)"
    new="$file.new.$$"

    jq -n \
        --arg phase "$phase" \
        --arg message "$message" \
        --arg bundleId "$BRORAY_ROUTES_DELETE_BUNDLE_ID" \
        --arg updatedAt "$now" \
        --argjson deletedCount "$deleted_count" \
        --argjson configurationSaved "$saved" \
        --argjson committed "$committed" '
        {
            schemaVersion: 1,
            operation: "delete",
            bundleId: $bundleId,
            phase: $phase,
            deletedCount: $deletedCount,
            configurationSaved: $configurationSaved,
            committed: $committed,
            message: $message,
            updatedAt: $updatedAt
        }
    ' >"$new" || return 1

    mv "$new" "$file"
}

broray_routes_delete_prepare()
{
    local bundle_id registry bundle_registry state plan output_dir now
    local delete_tsv global_new bundle_new state_new plan_new result_new
    local total managed_count external_count physical shared message

    bundle_id="$1"
    registry="$2"
    bundle_registry="$3"
    state="$4"
    plan="$5"
    output_dir="$6"
    now="$7"

    delete_tsv="$output_dir/delete.tsv"
    global_new="$output_dir/routes.json"
    bundle_new="$output_dir/bundle.json"
    state_new="$output_dir/state.json"
    plan_new="$output_dir/export-plan.json"
    result_new="$output_dir/router-delete-result.json"

    mkdir -p "$output_dir" || return 1

    jq -r \
        --slurpfile bundle "$bundle_registry" \
        --arg bundle_id "$bundle_id" \
        --arg target_interface "$managed_interface" '
        $bundle[0].managedRouteKeys as $keys |
        .routes[] |
        select(
            (.key as $key | $keys | index($key)) != null and
            ((.owners // []) | index($bundle_id)) != null and
            ((.owners // []) | length) == 1
        ) |
        [
            .key,
            .network,
            (.prefix | tostring),
            .mask,
            .interface,
            (.metric | tostring)
        ] |
        @tsv
    ' "$registry" >"$delete_tsv" || return 1

    jq -n \
        --slurpfile old "$registry" \
        --slurpfile bundle "$bundle_registry" \
        --arg bundle_id "$bundle_id" \
        --arg now "$now" \
        --arg target_interface "$BRORAY_ROUTES_DELETE_INTERFACE" '
        $old[0] as $o |
        $bundle[0].routeKeys as $bundle_keys |

        [
            ($o.routes // [])[] |
            if (
                (.key as $key | $bundle_keys | index($key)) != null and
                ((.owners // []) | index($bundle_id)) != null
            ) then
                (.owners - [$bundle_id]) as $remaining |
                if ($remaining | length) == 0 then
                    empty
                else
                    .owners = $remaining |
                    .updatedAt = $now
                end
            else
                .
            end
        ] as $routes |

        $o + {
            schemaVersion: 1,
            managedInterface: $target_interface,
            managedMetric: 1200,
            routes: $routes,
            updatedAt: $now
        }
    ' >"$global_new" || return 1

    jq \
        --arg now "$now" \
        --arg target_interface "$BRORAY_ROUTES_DELETE_INTERFACE" '
        .installedVersion = null |
        .routeKeys = [] |
        .managedRouteKeys = [] |
        .externalRouteKeys = [] |
        .targetInterface = $target_interface |
        .managedMetric = 1200 |
        .installedAt = null |
        .removedAt = $now |
        .updatedAt = $now
    ' "$bundle_registry" >"$bundle_new" || return 1

    total="$(jq -r '(.routeKeys // []) | length' "$bundle_registry")"
    managed_count="$(jq -r '(.managedRouteKeys // []) | length' "$bundle_registry")"
    external_count="$(jq -r '(.externalRouteKeys // []) | length' "$bundle_registry")"
    physical="$(wc -l <"$delete_tsv" | tr -d ' ')"
    shared="$((managed_count - physical))"
    message="Маршруты удалены из Keenetic"

    jq \
        --arg now "$now" \
        --arg message "$message" \
        --arg target_interface "$BRORAY_ROUTES_DELETE_INTERFACE" \
        --argjson total "$total" \
        --argjson deleted "$physical" \
        --argjson sharedKept "$shared" \
        --argjson externalKept "$external_count" '
        .status = (
            if .downloadedVersion != null then
                "downloaded"
            else
                "not_checked"
            end
        ) |
        .installedVersion = null |
        .lastDeletedAt = $now |
        .deleteResult = {
            result: "removed",
            message: $message,
            total: $total,
            deleted: $deleted,
            sharedKept: $sharedKept,
            externalKept: $externalKept,
            targetInterface: $target_interface,
            managedMetric: 1200,
            configurationSaved: ($deleted > 0),
            completedAt: $now
        } |
        .exportResult = null |
        .preflight = null |
        .lastError = null |
        .updatedAt = $now
    ' "$state" >"$state_new" || return 1

    jq \
        --arg now "$now" \
        --arg message "$message" \
        --argjson deleted "$physical" \
        --argjson sharedKept "$shared" \
        --argjson externalKept "$external_count" '
        .routerApplied = false |
        .configurationSaved = false |
        .removedAt = $now |
        .deleteResult = {
            message: $message,
            deleted: $deleted,
            sharedKept: $sharedKept,
            externalKept: $externalKept,
            managedMetric: 1200
        }
    ' "$plan" >"$plan_new" || return 1

    jq -n \
        --arg bundleId "$bundle_id" \
        --arg now "$now" \
        --arg message "$message" \
        --arg target_interface "$BRORAY_ROUTES_DELETE_INTERFACE" \
        --argjson total "$total" \
        --argjson deleted "$physical" \
        --argjson sharedKept "$shared" \
        --argjson externalKept "$external_count" '
        {
            schemaVersion: 1,
            bundleId: $bundleId,
            targetInterface: $target_interface,
            managedMetric: 1200,
            message: $message,
            summary: {
                total: $total,
                deleted: $deleted,
                sharedKept: $sharedKept,
                externalKept: $externalKept
            },
            routerChanged: ($deleted > 0),
            configurationSaved: ($deleted > 0),
            completedAt: $now
        }
    ' >"$result_new" || return 1

    jq -e --arg target_interface "$BRORAY_ROUTES_DELETE_INTERFACE" '
        (.schemaVersion == 1) and
        (.managedInterface == $target_interface) and
        (.managedMetric == 1200) and
        ((.routes | type) == "array") and
        (all(.routes[];
            (.interface == $target_interface) and
            (.metric == 1200) and
            ((.owners | type) == "array") and
            ((.owners | length) > 0)
        ))
    ' "$global_new" >/dev/null || return 1

    jq -e \
        --arg bundle_id "$bundle_id" '
        (.bundleId == $bundle_id) and
        (.installedVersion == null) and
        ((.routeKeys | length) == 0) and
        ((.managedRouteKeys | length) == 0) and
        ((.externalRouteKeys | length) == 0)
    ' "$bundle_new" >/dev/null || return 1

    jq -e --arg target_interface "$BRORAY_ROUTES_DELETE_INTERFACE" '
        (.installedVersion == null) and
        (.deleteResult.result == "removed") and
        (.deleteResult.targetInterface == $target_interface) and
        (.deleteResult.managedMetric == 1200)
    ' "$state_new" >/dev/null || return 1

    return 0
}

broray_routes_delete_backup_local()
{
    local original registry bundle_registry state plan result

    original="$1"
    registry="$2"
    bundle_registry="$3"
    state="$4"
    plan="$5"
    result="$6"

    mkdir -p "$original" || return 1
    cp -p "$registry" "$original/routes.json" || return 1
    cp -p "$bundle_registry" "$original/bundle.json" || return 1
    cp -p "$state" "$original/state.json" || return 1
    cp -p "$plan" "$original/export-plan.json" || return 1

    if [ -f "$result" ]; then
        cp -p "$result" "$original/router-delete-result.json" || return 1
    else
        : >"$original/router-delete-result.did-not-exist"
    fi
}

broray_routes_delete_install_local()
{
    local prepared registry bundle_registry state plan result

    prepared="$1"
    registry="$2"
    bundle_registry="$3"
    state="$4"
    plan="$5"
    result="$6"

    cp -p "$prepared/routes.json" "$registry.new.$$" || return 1
    cp -p "$prepared/bundle.json" "$bundle_registry.new.$$" || return 1
    cp -p "$prepared/state.json" "$state.new.$$" || return 1
    cp -p "$prepared/export-plan.json" "$plan.new.$$" || return 1
    cp -p "$prepared/router-delete-result.json" "$result.new.$$" || return 1

    mv "$registry.new.$$" "$registry" || return 1
    mv "$bundle_registry.new.$$" "$bundle_registry" || return 1
    mv "$state.new.$$" "$state" || return 1
    mv "$plan.new.$$" "$plan" || return 1
    mv "$result.new.$$" "$result" || return 1
}

broray_routes_delete_restore_local()
{
    local original registry bundle_registry state plan result

    original="$1"
    registry="$2"
    bundle_registry="$3"
    state="$4"
    plan="$5"
    result="$6"

    cp -p "$original/routes.json" "$registry" 2>/dev/null || true
    cp -p "$original/bundle.json" "$bundle_registry" 2>/dev/null || true
    cp -p "$original/state.json" "$state" 2>/dev/null || true
    cp -p "$original/export-plan.json" "$plan" 2>/dev/null || true

    if [ -f "$original/router-delete-result.did-not-exist" ]; then
        rm -f "$result" 2>/dev/null || true
    elif [ -f "$original/router-delete-result.json" ]; then
        cp -p "$original/router-delete-result.json" "$result" 2>/dev/null || true
    fi
}

broray_routes_delete_rollback_routes()
{
    local tab key network prefix mask interface metric out err command_text

    [ -n "$BRORAY_ROUTES_DELETE_DELETED_FILE" ] || return 0
    [ -f "$BRORAY_ROUTES_DELETE_DELETED_FILE" ] || return 0

    tab="$(printf '\t')"

    sed '1!G;h;$!d' "$BRORAY_ROUTES_DELETE_DELETED_FILE" |
    while IFS="$tab" read -r key network prefix mask interface metric
    do
        [ -n "$network" ] || continue

        out="$BRORAY_ROUTES_DELETE_WORK/rollback-add.out"
        err="$BRORAY_ROUTES_DELETE_WORK/rollback-add.err"
        command_text="ip route $network $mask $BRORAY_ROUTES_DELETE_INTERFACE 1200"

        if ! broray_routes_delete_ndmc "$command_text" "$out" "$err" 8; then
            broray_routes_delete_error \
                "Не удалось восстановить маршрут $network/$prefix."
            continue
        fi

        if ! grep -Eq \
            'Added static route|Renewed static route' \
            "$out"
        then
            broray_routes_delete_error \
                "Keenetic не подтвердил восстановление $network/$prefix."
        fi
    done
}

broray_routes_delete_cleanup()
{
    trap - EXIT HUP INT TERM

    broray_routes_delete_kill_active

    if [ "$BRORAY_ROUTES_DELETE_ROLLBACK_NEEDED" = true ] &&
       [ "$BRORAY_ROUTES_DELETE_COMMITTED" != true ]
    then
        broray_routes_delete_rollback_routes

        if [ -n "$BRORAY_ROUTES_DELETE_ORIGINAL" ] &&
           [ -d "$BRORAY_ROUTES_DELETE_ORIGINAL" ]
        then
            broray_routes_delete_restore_local \
                "$BRORAY_ROUTES_DELETE_ORIGINAL" \
                "$BRORAY_ROUTES_DELETE_ROUTES/installed/routes.json" \
                "$BRORAY_ROUTES_DELETE_ROUTES/installed/bundles/$BRORAY_ROUTES_DELETE_BUNDLE_ID.json" \
                "$BRORAY_ROUTES_DELETE_ROUTES/state/$BRORAY_ROUTES_DELETE_BUNDLE_ID.json" \
                "$BRORAY_ROUTES_DELETE_ROUTES/catalog/$BRORAY_ROUTES_DELETE_BUNDLE_ID/export-plan.json" \
                "$BRORAY_ROUTES_DELETE_ROUTES/catalog/$BRORAY_ROUTES_DELETE_BUNDLE_ID/router-delete-result.json"
        fi
    fi

    broray_routes_delete_lock_release

    if [ -n "$BRORAY_ROUTES_DELETE_WORK" ]; then
        rm -rf "$BRORAY_ROUTES_DELETE_WORK" 2>/dev/null || true
    fi
}

broray_routes_router_delete_run()
{
    local bundle_id config registry bundle_registry state plan result transactions
    local stamp now prepared original transaction delete_tsv deleted_tsv
    local total managed_count external_count physical shared already_absent tab key network prefix mask interface metric
    local actual_config filtered_tsv runtime_absent
    local out err command_text delete_rc deleted_count save_needed message

    bundle_id="$1"

    broray_routes_delete_bundle_id_valid "$bundle_id" || {
        broray_routes_delete_error "Некорректный идентификатор набора."
        return 2
    }

    [ -r "$BRORAY_ROUTES_DELETE_ROUTES/bundles.json" ] || {
        broray_routes_delete_error "Реестр наборов маршрутов недоступен."
        return 1
    }

    jq -e \
        --arg bundle_id "$bundle_id" '
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (.bundles | index($bundle_id) != null)
    ' "$BRORAY_ROUTES_DELETE_ROUTES/bundles.json" >/dev/null 2>&1 || {
        broray_routes_delete_error "Набор маршрутов не найден."
        return 1
    }

    BRORAY_ROUTES_DELETE_BUNDLE_ID="$bundle_id"

    config="$BRORAY_ROUTES_DELETE_ROUTES/config.json"
    registry="$BRORAY_ROUTES_DELETE_ROUTES/installed/routes.json"
    bundle_registry="$BRORAY_ROUTES_DELETE_ROUTES/installed/bundles/$bundle_id.json"
    state="$BRORAY_ROUTES_DELETE_ROUTES/state/$bundle_id.json"
    plan="$BRORAY_ROUTES_DELETE_ROUTES/catalog/$bundle_id/export-plan.json"
    result="$BRORAY_ROUTES_DELETE_ROUTES/catalog/$bundle_id/router-delete-result.json"
    transactions="$BRORAY_ROUTES_DELETE_ROUTES/transactions"

    for file in "$config" "$registry" "$bundle_registry" "$state" "$plan"
    do
        [ -r "$file" ] || {
            broray_routes_delete_error "Не найден файл: $file"
            return 1
        }
    done

    if [ -n "${BRORAY_ROUTES_CONFIG_NDMC:-}" ] &&
       [ -x "$BRORAY_ROUTES_CONFIG_NDMC" ]
    then
        BRORAY_ROUTES_DELETE_NDMC="$BRORAY_ROUTES_CONFIG_NDMC"
    else
        BRORAY_ROUTES_DELETE_NDMC="$(command -v ndmc 2>/dev/null || true)"
    fi
    [ -n "$BRORAY_ROUTES_DELETE_NDMC" ] || {
        broray_routes_delete_error "Команда ndmc недоступна."
        return 1
    }

    managed_interface="$(jq -r '.managedInterface // empty' "$config")"
    case "$managed_interface" in
        Proxy[0-9]*) ;;
        *)
            broray_routes_delete_error "Некорректный управляемый интерфейс ProxyN."
            return 1
            ;;
    esac
    case "${managed_interface#Proxy}" in
        ''|*[!0-9]*)
            broray_routes_delete_error "Некорректный управляемый интерфейс ProxyN."
            return 1
            ;;
    esac
    BRORAY_ROUTES_DELETE_INTERFACE="$managed_interface"
    export BRORAY_ROUTES_DELETE_INTERFACE


    command -v broray_interface_owner_record_valid >/dev/null 2>&1 || {
        broray_routes_delete_error "Модуль владения ProxyN недоступен."
        return 1
    }
    broray_interface_owner_record_valid "$managed_interface" || {
        broray_routes_delete_error "Локальный реестр владельца не подтверждает $managed_interface."
        return 1
    }
    if broray_interface_exists_name "$managed_interface"; then
        broray_interface_owner_valid "$managed_interface" || {
            broray_routes_delete_error "Интерфейс $managed_interface не подтверждён как принадлежащий BROray."
            return 1
        }
    fi

    jq -e --arg target_interface "$managed_interface" '
        (.managedInterface == $target_interface) and
        (.managedMetric == 1200) and
        (.ownershipPolicy.deleteOnlyExactManagedMatch == true) and
        (.ownershipPolicy.deleteExternalRoutes == false) and
        (.ownershipPolicy.touchOtherInterfaces == false)
    ' "$config" >/dev/null || {
        broray_routes_delete_error "Политика удаления повреждена."
        return 1
    }

    jq -e \
        --arg bundle_id "$bundle_id" '
        (.bundleId == $bundle_id) and
        (.installedVersion != null) and
        (.managedMetric == 1200) and
        ((.routeKeys | type) == "array") and
        ((.managedRouteKeys | type) == "array")
    ' "$bundle_registry" >/dev/null || {
        broray_routes_delete_error "Набор не установлен или его реестр повреждён."
        return 1
    }

    jq -e \
        --slurpfile bundle "$bundle_registry" \
        --arg bundle_id "$bundle_id" \
        --arg target_interface "$managed_interface" '
        . as $registry |
        $bundle[0].managedRouteKeys as $keys |
        [
            $keys[] as $key |
            any($registry.routes[];
                .key == $key and
                .interface == $target_interface and
                .metric == 1200 and
                .createdByBROray == true and
                .managed == true and
                ((.owners // []) | index($bundle_id)) != null
            )
        ] |
        all
    ' "$registry" >/dev/null || {
        broray_routes_delete_error \
            "Реестр владения не подтверждает все управляемые маршруты."
        return 1
    }

    broray_routes_delete_lock_acquire "$bundle_id" || return 1

    stamp="$(broray_routes_delete_stamp)"
    now="$(broray_routes_delete_now)"
    BRORAY_ROUTES_DELETE_WORK="$BRORAY_ROUTES_DELETE_ROUTES/tmp/delete-$bundle_id-$stamp-$$"
    prepared="$BRORAY_ROUTES_DELETE_WORK/prepared"
    original="$BRORAY_ROUTES_DELETE_WORK/original"
    transaction="$transactions/delete-$bundle_id-$stamp.json"
    deleted_tsv="$BRORAY_ROUTES_DELETE_WORK/deleted.tsv"

    mkdir -p "$BRORAY_ROUTES_DELETE_WORK" "$transactions" || {
        broray_routes_delete_error "Не удалось создать рабочий каталог."
        return 1
    }

    actual_config="$BRORAY_ROUTES_DELETE_WORK/running-config-before.json"

    command -v broray_routes_config_fetch >/dev/null 2>&1 || {
        broray_routes_delete_error             "Модуль чтения полной конфигурации маршрутов недоступен."
        return 1
    }

    broray_routes_config_fetch "$actual_config" || {
        broray_routes_delete_error             "Не удалось прочитать полный список настроенных маршрутов Keenetic."
        return 1
    }

    : >"$deleted_tsv"
    BRORAY_ROUTES_DELETE_DELETED_FILE="$deleted_tsv"
    BRORAY_ROUTES_DELETE_ORIGINAL="$original"

    broray_routes_delete_prepare \
        "$bundle_id" "$registry" "$bundle_registry" "$state" "$plan" \
        "$prepared" "$now" || {
        broray_routes_delete_error "Не удалось подготовить локальное состояние удаления."
        return 1
    }

    # The ownership registry may be stale after manual deletion in Keenetic.
    # Delete only exact BROray objects that still exist in running-config.
    delete_tsv="$prepared/delete.tsv"
    filtered_tsv="$BRORAY_ROUTES_DELETE_WORK/delete.configured.tsv"

    broray_routes_delete_filter_configured \
        "$delete_tsv" \
        "$actual_config" \
        "$filtered_tsv" || {
        broray_routes_delete_error             "Не удалось сопоставить реестр BROray с running-config."
        return 1
    }

    mv -f "$filtered_tsv" "$delete_tsv" || {
        broray_routes_delete_error             "Не удалось подготовить безопасный список удаления."
        return 1
    }

    broray_routes_delete_backup_local \
        "$original" "$registry" "$bundle_registry" "$state" "$plan" "$result" || {
        broray_routes_delete_error "Не удалось создать резервную копию локального состояния."
        return 1
    }

    delete_tsv="$prepared/delete.tsv"
    total="$(jq -r '(.routeKeys // []) | length' "$bundle_registry")"
    managed_count="$(jq -r '(.managedRouteKeys // []) | length' "$bundle_registry")"
    external_count="$(jq -r '(.externalRouteKeys // []) | length' "$bundle_registry")"
    physical="$(wc -l <"$delete_tsv" | tr -d ' ')"
    shared="$(
        jq -r \
            --slurpfile bundle "$bundle_registry" \
            --arg bundle_id "$bundle_id" '
            $bundle[0].managedRouteKeys as $keys |
            [
                .routes[] |
                select(
                    (.key as $key | $keys | index($key)) != null and
                    ((.owners // []) | index($bundle_id)) != null and
                    ((.owners // []) | length) > 1
                )
            ] |
            length
        ' "$registry"
    )"
    already_absent="$((managed_count - shared - physical))"
    [ "$already_absent" -ge 0 ] 2>/dev/null || already_absent=0

    broray_routes_delete_adjust_prepared \
        "$prepared" \
        "$physical" \
        "$shared" \
        "$already_absent" || {
        broray_routes_delete_error             "Не удалось уточнить результат удаления по running-config."
        return 1
    }

    deleted_count=0
    save_needed=false

    broray_routes_delete_transaction_write \
        "$transaction" "checked" 0 false false \
        "Проверка владения завершена." || true

    BRORAY_ROUTES_DELETE_ROLLBACK_NEEDED=true
    tab="$(printf '\t')"

    while IFS="$tab" read -r key network prefix mask interface metric
    do
        [ -n "$network" ] || continue

        [ "$interface" = "$managed_interface" ] || {
            broray_routes_delete_error \
                "План удаления вышел за пределы $managed_interface."
            return 1
        }

        [ "$metric" = "1200" ] || {
            broray_routes_delete_error \
                "План удаления содержит чужую метрику."
            return 1
        }

        out="$BRORAY_ROUTES_DELETE_WORK/delete.out"
        err="$BRORAY_ROUTES_DELETE_WORK/delete.err"

        # Keenetic identifies a user route by destination and interface.
        # The metric is a mutable attribute of that object, not a second
        # independent identity on the same interface. On KeeneticOS 5.1.1
        # Preview, deleting a hidden route while the same destination is
        # present on another interface can complete successfully but return
        # code 122 with a Netlink "file exists" message.
        command_text="no ip route $network $mask $managed_interface"
        delete_rc=0

        if broray_routes_delete_ndmc "$command_text" "$out" "$err" 8; then
            delete_rc=0
        else
            delete_rc=$?
        fi

        if grep -Fq \
            "Deleted static route: $network/$prefix via $managed_interface." \
            "$out"
        then
            :
        elif [ "$delete_rc" = "122" ] &&
             cat "$out" "$err" 2>/dev/null |
                 grep -Fq \
                     "got an error response: file exists."
        then
            # Verified on a real KN-1811: the target managed-interface object is
            # removed, while the parallel route on another interface remains.
            :
        elif cat "$out" "$err" 2>/dev/null |
             grep -Fq "No such route:"
        then
            # The route was removed manually after our running-config snapshot.
            # This is an idempotent success, not an operation failure.
            already_absent=$((already_absent + 1))
            continue
        else
            details="$(tail -n 20 "$out" 2>/dev/null; tail -n 20 "$err" 2>/dev/null)"
            broray_routes_delete_error \
                "Не удалось удалить $network/$prefix. $details"
            return 1
        fi

        printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
            "$key" "$network" "$prefix" "$mask" "$interface" "$metric" \
            >>"$deleted_tsv"

        deleted_count=$((deleted_count + 1))
        save_needed=true

        broray_routes_delete_transaction_write \
            "$transaction" "deleting" "$deleted_count" false false \
            "Удаляются управляемые маршруты." || true
    done <"$delete_tsv"

    runtime_absent="$((physical - deleted_count))"
    [ "$runtime_absent" -ge 0 ] 2>/dev/null || runtime_absent=0

    # Recalculate the persisted result from commands that were actually
    # confirmed. Races with manual deletion remain a successful idempotent
    # removal and are counted as already absent.
    broray_routes_delete_adjust_prepared \
        "$prepared" \
        "$deleted_count" \
        "$shared" \
        "$already_absent" || {
        broray_routes_delete_error             "Не удалось зафиксировать итог безопасного удаления."
        return 1
    }


    # Confirm the real configured state before clearing local ownership.
    # ndmc messages alone are insufficient for hidden routes that coexist on
    # another interface.
    actual_after="$BRORAY_ROUTES_DELETE_WORK/actual-after.json"
    rm -f "${BRORAY_ROUTES_CONFIG_CACHE:-}" 2>/dev/null || true
    broray_routes_config_snapshot "$actual_after" || {
        broray_routes_delete_error "Не удалось повторно прочитать running-config после удаления."
        return 1
    }

    broray_routes_delete_verify_result \
        "$delete_tsv" \
        "$bundle_registry" \
        "$registry" \
        "$actual_after" || {
        broray_routes_delete_error "Итог удаления не подтверждён полной конфигурацией Keenetic."
        return 1
    }

    broray_routes_delete_install_local \
        "$prepared" "$registry" "$bundle_registry" "$state" "$plan" "$result" || {
        broray_routes_delete_error "Не удалось установить локальное состояние удаления."
        return 1
    }

    if [ "$save_needed" = true ]; then
        out="$BRORAY_ROUTES_DELETE_WORK/save.out"
        err="$BRORAY_ROUTES_DELETE_WORK/save.err"

        if ! broray_routes_delete_ndmc \
            "system configuration save" "$out" "$err" 12
        then
            details="$(tail -n 20 "$out" 2>/dev/null; tail -n 20 "$err" 2>/dev/null)"
            broray_routes_delete_error \
                "Не удалось сохранить конфигурацию Keenetic. $details"
            return 1
        fi
    fi

    BRORAY_ROUTES_DELETE_COMMITTED=true
    BRORAY_ROUTES_DELETE_ROLLBACK_NEEDED=false

    message="Маршруты удалены из Keenetic"
    broray_routes_delete_transaction_write \
        "$transaction" "committed" "$deleted_count" \
        "$save_needed" true "$message" || true

    broray_routes_delete_lock_release
    rm -rf "$BRORAY_ROUTES_DELETE_WORK" 2>/dev/null || true
    BRORAY_ROUTES_DELETE_WORK=""

    echo "$message"
    echo "Набор: $bundle_id"
    echo "Удалено физических маршрутов BROray: $deleted_count"
    echo "Сохранено общих маршрутов: $shared"
    echo "Уже отсутствовало в Keenetic: $already_absent"
    echo "Не затронуто внешних маршрутов: $external_count"
    echo "Интерфейс удаления: $managed_interface"
    echo "Метрика владения BROray: 1200"
    echo "Команда удаления: сеть + маска + $managed_interface"
    echo "Конфигурация сохранена: $save_needed"
    echo "Локально скачанный набор сохранён."
}

# BROray configured-route helpers r10a.
BRORAY_ROUTES_CONFIG_LIBRARY="${BRORAY_ROUTES_CONFIG_LIBRARY:-$BRORAY_ROUTES_DELETE_ROOT/lib/routes-router-config.sh}"

if [ -r "$BRORAY_ROUTES_CONFIG_LIBRARY" ]; then
    . "$BRORAY_ROUTES_CONFIG_LIBRARY"
fi

broray_routes_delete_filter_configured()
{
    local candidates actual output candidate_json

    candidates="$1"
    actual="$2"
    output="$3"
    candidate_json="$output.candidates.$$"

    jq -Rn '
        [
            inputs |
            split("\t") |
            select(length == 6) |
            {
                key: .[0],
                network: .[1],
                prefix: (.[2] | tonumber),
                mask: .[3],
                interface: .[4],
                metric: (.[5] | tonumber)
            }
        ]
    ' <"$candidates" >"$candidate_json" || {
        rm -f "$candidate_json"
        return 1
    }

    jq -r \
        --slurpfile actual "$actual" '
        $actual[0] as $a |
        .[] |
        . as $candidate |
        ([
            $a.routes[]? |
            select(
                .network == $candidate.network and
                .mask == $candidate.mask and
                .interface == $candidate.interface and
                ((.gateway // "0.0.0.0") == "0.0.0.0") and
                ((.metric // 1000) == $candidate.metric) and
                (((.proto // "static") | ascii_downcase) == "static")
            )
        ] | length) as $matches |
        select($matches == 1) |
        [
            .key,
            .network,
            (.prefix | tostring),
            .mask,
            .interface,
            (.metric | tostring)
        ] |
        @tsv
    ' "$candidate_json" >"$output" || {
        rm -f "$candidate_json" "$output"
        return 1
    }

    rm -f "$candidate_json"
    return 0
}

broray_routes_delete_adjust_prepared()
{
    local prepared deleted shared already_absent file new

    prepared="$1"
    deleted="$2"
    shared="$3"
    already_absent="$4"

    file="$prepared/state.json"
    new="$file.new.$$"
    jq \
        --argjson deleted "$deleted" \
        --argjson shared "$shared" \
        --argjson already_absent "$already_absent" '
        .deleteResult.deleted = $deleted |
        .deleteResult.sharedKept = $shared |
        .deleteResult.alreadyAbsent = $already_absent |
        .deleteResult.configurationSaved = ($deleted > 0)
    ' "$file" >"$new" && mv -f "$new" "$file" || return 1

    file="$prepared/export-plan.json"
    new="$file.new.$$"
    jq \
        --argjson deleted "$deleted" \
        --argjson shared "$shared" \
        --argjson already_absent "$already_absent" '
        .deleteResult.deleted = $deleted |
        .deleteResult.sharedKept = $shared |
        .deleteResult.alreadyAbsent = $already_absent
    ' "$file" >"$new" && mv -f "$new" "$file" || return 1

    file="$prepared/router-delete-result.json"
    new="$file.new.$$"
    jq \
        --argjson deleted "$deleted" \
        --argjson shared "$shared" \
        --argjson already_absent "$already_absent" '
        .summary.deleted = $deleted |
        .summary.sharedKept = $shared |
        .summary.alreadyAbsent = $already_absent |
        .routerChanged = ($deleted > 0) |
        .configurationSaved = ($deleted > 0)
    ' "$file" >"$new" && mv -f "$new" "$file" || return 1

    return 0
}


broray_routes_delete_verify_result()
{
    local deleted_tsv bundle_file registry_file actual_file deleted_json

    deleted_tsv="$1"
    bundle_file="$2"
    registry_file="$3"
    actual_file="$4"
    deleted_json="$actual_file.deleted.$$"

    jq -Rn '
        [
            inputs |
            split("\t") |
            select(length == 6) |
            {
                key: .[0],
                network: .[1],
                prefix: (.[2] | tonumber),
                mask: .[3],
                interface: .[4],
                metric: (.[5] | tonumber)
            }
        ]
    ' <"$deleted_tsv" >"$deleted_json" || {
        rm -f "$deleted_json"
        return 1
    }

    jq -e \
        --slurpfile deleted "$deleted_json" \
        --slurpfile bundle "$bundle_file" \
        --slurpfile registry "$registry_file" \
        --slurpfile actual "$actual_file" '
        def exact_match($candidate):
            [
                $actual[0].routes[]? |
                select(
                    .network == $candidate.network and
                    .mask == $candidate.mask and
                    .interface == $candidate.interface and
                    ((.gateway // "0.0.0.0") == "0.0.0.0") and
                    ((.metric // 1000) == $candidate.metric) and
                    (((.proto // "static") | ascii_downcase) == "static")
                )
            ] | length;

        ($bundle[0].managedRouteKeys // []) as $bundle_keys |
        [
            $registry[0].routes[]? |
            select(
                (.key as $key | $bundle_keys | index($key)) != null and
                ((.owners // []) | length) > 1
            )
        ] as $shared |

        (all($deleted[0][]; exact_match(.) == 0)) and
        (all($shared[]; exact_match(.) == 1))
    ' "$actual_file" >/dev/null 2>&1
    rc=$?

    rm -f "$deleted_json"
    return "$rc"
}
