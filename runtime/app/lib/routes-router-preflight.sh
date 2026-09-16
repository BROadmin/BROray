#!/opt/bin/ash

# BROray routes static conflict guard v1
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_ROUTES_PREFLIGHT_LOCK="$BRORAY_ROUTES_ROOT/locks/operation.lock"
BRORAY_ROUTES_PREFLIGHT_RCI_URL="${BRORAY_ROUTES_RCI_URL:-http://127.0.0.1:79/rci/show/ip/route}"
BRORAY_ROUTES_PREFLIGHT_ACTIVE_PID=""
BRORAY_ROUTES_PREFLIGHT_ACTIVE_WORK=""

. "${BRORAY_ROOT:-/opt/broray}/lib/routes-resource-lock.sh" || return 1

broray_routes_preflight_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_preflight_error()
{
    printf 'ОШИБКА: %s\n' "$*" >&2
    exit 1
}

broray_routes_preflight_bundle_id_valid()
{
    local value

    value="${1:-}"
    [ -n "$value" ] || return 1

    case "$value" in
        *[!a-z0-9_-]*) return 1 ;;
    esac

    return 0
}

broray_routes_preflight_is_pid()
{
    case "${1:-}" in
        ''|*[!0-9]*) return 1 ;;
    esac

    [ "$1" -gt 1 ] 2>/dev/null
}

broray_routes_preflight_lock_acquire()
{
    broray_route_resource_acquire "$BRORAY_ROUTES_PREFLIGHT_LOCK" "preflight" "${BRORAY_ROUTES_PREFLIGHT_BUNDLE:-}" || return $?
    BRORAY_ROUTES_PREFLIGHT_LOCK_TOKEN="$BRORAY_ROUTE_RESOURCE_TOKEN"
    return 0
}

broray_routes_preflight_lock_release()
{
    broray_route_resource_release "$BRORAY_ROUTES_PREFLIGHT_LOCK" "${BRORAY_ROUTES_PREFLIGHT_LOCK_TOKEN:-}" || return $?
    BRORAY_ROUTES_PREFLIGHT_LOCK_TOKEN=''
    return 0
}

broray_routes_preflight_kill_active()
{
    # The synchronous native runner owns command termination and tree drain.
    :
}

broray_routes_preflight_cleanup()
{
    broray_routes_preflight_kill_active

    if [ -n "$BRORAY_ROUTES_PREFLIGHT_ACTIVE_WORK" ]; then
        rm -rf "$BRORAY_ROUTES_PREFLIGHT_ACTIVE_WORK" 2>/dev/null || true
    fi

    broray_routes_preflight_lock_release
}

broray_routes_preflight_read_command_allowed()
{
    local command_text interface suffix

    command_text="${1:-}"
    case "$command_text" in
        'show interface Proxy'[0-9]*)
            interface="${command_text#show interface }"
            suffix="${interface#Proxy}"
            case "$suffix" in ''|*[!0-9]*) return 1 ;; esac
            [ "$command_text" = "show interface Proxy$suffix" ]
            ;;
        *) return 1 ;;
    esac
}

broray_routes_preflight_ndmc()
{
    broray_routes_preflight_read_command_allowed "$1" || return 126
    . "$BRORAY_ROOT/lib/routes-ndmc.sh" || return 1
    broray_routes_ndmc_capture "$BRORAY_ROUTES_PREFLIGHT_NDMC" "$1" "$2" "$3" "${4:-8}"
}

broray_routes_preflight_fetch_rci__shadowed_legacy_1_unused()
{
    local raw_file output_file

    raw_file="$1"
    output_file="$2"

    if command -v curl >/dev/null 2>&1; then
        curl \
            --silent \
            --show-error \
            --fail \
            --connect-timeout 2 \
            --max-time 8 \
            "$BRORAY_ROUTES_PREFLIGHT_RCI_URL" >"$raw_file" || return 1
    elif command -v wget >/dev/null 2>&1; then
        wget -q -T 8 -O "$raw_file" "$BRORAY_ROUTES_PREFLIGHT_RCI_URL" || return 1
    else
        return 1
    fi

    jq -e . "$raw_file" >/dev/null || return 1

    jq '
        if ((.route? | type) == "array") then
            {schemaVersion: 1, routes: .route}
        elif ((.routes? | type) == "array") then
            {schemaVersion: 1, routes: .routes}
        elif ((.show?.ip?.route?.route? | type) == "array") then
            {schemaVersion: 1, routes: .show.ip.route.route}
        elif (type == "array") then
            {schemaVersion: 1, routes: .}
        else
            error("route array not found")
        end
    ' "$raw_file" >"$output_file" || return 1

    jq -e '
        (.schemaVersion == 1) and
        ((.routes | type) == "array")
    ' "$output_file" >/dev/null
}

broray_routes_preflight_classify__shadowed_legacy_1_unused()
{
    local plan_file actual_file registry_file output_file checked_at

    plan_file="$1"
    actual_file="$2"
    registry_file="$3"
    output_file="$4"
    checked_at="${5:-$(broray_routes_preflight_now)}"

    jq -n \
        --slurpfile plan "$plan_file" \
        --slurpfile actual "$actual_file" \
        --slurpfile registry "$registry_file" \
        --arg checked_at "$checked_at" '
        $plan[0] as $p |
        $actual[0] as $a |
        $registry[0] as $g |

        [
            $p.routes[] as $route |
            ($route.network + "/" + ($route.prefix | tostring)) as $destination |
            [
                $a.routes[]? |
                select(.destination == $destination)
            ] as $matches |
            [
                $matches[] |
                select(.interface == $p.targetInterface)
            ] as $target_matches |
            [
                $target_matches[] |
                select((.gateway // "0.0.0.0") == "0.0.0.0")
            ] as $exact_target_matches |
            [
                $matches[] |
                select(
                    .interface != $p.targetInterface and
                    (((.proto // "") | ascii_downcase) == "static")
                )
            ] as $other_static_matches |
            [
                $matches[] |
                select(.interface != $p.targetInterface)
            ] as $other_matches |
            [
                $g.routes[]? |
                select(
                    .key == $route.key and
                    (
                        (.createdByBROray // false) == true or
                        (.managed // false) == true
                    )
                )
            ] as $owned_entries |

            (
                if ($other_static_matches | length) > 0 then
                    "conflict"
                elif ($exact_target_matches | length) > 1 then
                    "conflict"
                elif ($target_matches | length) > ($exact_target_matches | length) then
                    "conflict"
                elif ($exact_target_matches | length) == 1 then
                    if ($owned_entries | length) == 1 then
                        "managed_existing"
                    else
                        "external_existing"
                    end
                else
                    "create"
                end
            ) as $status |

            $route + {
                destination: $destination,
                status: $status,
                managedInterfaceMatches: $target_matches,
                otherInterfaceMatches: $other_matches,
                staticOtherInterfaceMatches: $other_static_matches,
                otherInterfaces: ([$other_matches[].interface] | unique),
                staticOtherInterfaces: ([$other_static_matches[].interface] | unique),
                ownership: (
                    if $status == "managed_existing" then "broray"
                    elif $status == "external_existing" then "external"
                    else null
                    end
                ),
                conflictReason: (
                    if ($other_static_matches | length) > 0 then
                        "Статический маршрут уже настроен через другой интерфейс: " +
                        (([$other_static_matches[].interface] | unique) | join(", ")) +
                        ". BROray не изменяет чужие маршруты."
                    elif ($exact_target_matches | length) > 1 then
                        "На управляемом интерфейсе найдено несколько одинаковых записей."
                    elif ($target_matches | length) > ($exact_target_matches | length) then
                        "На управляемом интерфейсе существует маршрут с другим шлюзом."
                    else
                        null
                    end
                )
            }
        ] as $routes |

        {
            schemaVersion: 2,
            bundleId: $p.bundleId,
            sourceCommit: $p.sourceCommit,
            contentSha256: $p.contentSha256,
            targetInterface: $p.targetInterface,
            checkedAt: $checked_at,
            routerChanged: false,
            configurationSaved: false,
            source: "local-rci",
            summary: {
                total: ($routes | length),
                toCreate: ([$routes[] | select(.status == "create")] | length),
                managedExisting: ([$routes[] | select(.status == "managed_existing")] | length),
                externalExisting: ([$routes[] | select(.status == "external_existing")] | length),
                conflicts: ([$routes[] | select(.status == "conflict")] | length),
                staticOtherConflicts: (
                    [
                        $routes[] |
                        select((.staticOtherInterfaceMatches | length) > 0)
                    ] |
                    length
                ),
                withOtherInterfaceMatches: (
                    [
                        $routes[] |
                        select((.otherInterfaceMatches | length) > 0)
                    ] |
                    length
                )
            },
            routes: $routes
        } |
        .canExport = (
            (.summary.conflicts == 0) and
            (.summary.externalExisting == 0)
        ) |
        .message = (
            if .summary.staticOtherConflicts > 0 then
                "Обнаружены статические маршруты на других интерфейсах."
            elif .summary.conflicts > 0 then
                "Обнаружены конфликты маршрутов."
            elif .summary.toCreate == 0 then
                "Маршруты уже представлены на управляемом интерфейсе."
            else
                "Предварительная проверка экспорта завершена."
            end
        )
    ' >"$output_file"
}

broray_routes_preflight_run()
{
    local bundle_id bundles config plan registry state catalog managed_interface
    local lock_result work interface_out interface_err raw_rci actual_json
    local preflight_new preflight_path state_new now summary_total summary_create
    local summary_managed summary_external summary_conflicts summary_static_other
    local summary_other can_export message

    bundle_id="${1:-}"

    broray_routes_preflight_bundle_id_valid "$bundle_id" ||
        broray_routes_preflight_error "Некорректный идентификатор набора."

    BRORAY_ROUTES_PREFLIGHT_BUNDLE="$bundle_id"
    export BRORAY_ROUTES_PREFLIGHT_BUNDLE

    bundles="$BRORAY_ROUTES_ROOT/bundles.json"
    config="$BRORAY_ROUTES_ROOT/config.json"
    catalog="$BRORAY_ROUTES_ROOT/catalog/$bundle_id"
    plan="$catalog/export-plan.json"
    registry="$BRORAY_ROUTES_ROOT/installed/routes.json"
    state="$BRORAY_ROUTES_ROOT/state/$bundle_id.json"

    for file in "$bundles" "$config" "$plan" "$registry" "$state"
    do
        [ -r "$file" ] || broray_routes_preflight_error "Не найден обязательный файл: $file"
    done

    jq -e --arg bundle_id "$bundle_id" '
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (.bundles | index($bundle_id) != null)
    ' "$bundles" >/dev/null ||
        broray_routes_preflight_error "Набор маршрутов не разрешён: $bundle_id"

    managed_interface="$(jq -r '.managedInterface // empty' "$config")"
    case "$managed_interface" in
        Proxy[0-9]*) ;;
        *) broray_routes_preflight_error "Некорректный управляемый интерфейс Keenetic." ;;
    esac
    case "${managed_interface#Proxy}" in
        ''|*[!0-9]*) broray_routes_preflight_error "Некорректный управляемый интерфейс Keenetic." ;;
    esac



    jq -e --arg target_interface "$managed_interface" '
        (.schemaVersion == 1) and
        (.managedInterface == $target_interface) and
        (.ownershipPolicy.touchOtherInterfaces == false) and
        (.ownershipPolicy.modifyExternalRoutes == false) and
        (.ownershipPolicy.deleteExternalRoutes == false)
    ' "$config" >/dev/null ||
        broray_routes_preflight_error "Политика защиты маршрутов повреждена."

    jq -e --arg bundle_id "$bundle_id" --arg target_interface "$managed_interface" '
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        (.targetInterface == $target_interface) and
        (.routerApplied == false) and
        ((.routes | type) == "array") and
        ((.routes | length) > 0) and
        (all(.routes[]; .targetInterface == $target_interface))
    ' "$plan" >/dev/null ||
        broray_routes_preflight_error "План экспорта повреждён или предназначен не для $managed_interface."

    jq -e --arg target_interface "$managed_interface" '
        (.schemaVersion == 1) and
        (.managedInterface == $target_interface) and
        ((.routes | type) == "array")
    ' "$registry" >/dev/null ||
        broray_routes_preflight_error "Глобальный реестр маршрутов повреждён."

    if [ -n "${BRORAY_ROUTES_CONFIG_NDMC:-}" ] &&
       [ -x "$BRORAY_ROUTES_CONFIG_NDMC" ]
    then
        BRORAY_ROUTES_PREFLIGHT_NDMC="$BRORAY_ROUTES_CONFIG_NDMC"
    else
        BRORAY_ROUTES_PREFLIGHT_NDMC="$(command -v ndmc 2>/dev/null || true)"
    fi
    [ -n "$BRORAY_ROUTES_PREFLIGHT_NDMC" ] ||
        broray_routes_preflight_error "Команда ndmc недоступна."

    lock_result=0
    broray_routes_preflight_lock_acquire || lock_result=$?

    case "$lock_result" in
        0) ;;
        2) broray_routes_preflight_error "Другая операция с маршрутами уже выполняется." ;;
        *) broray_routes_preflight_error "Не удалось установить блокировку операции." ;;
    esac

    work="$BRORAY_ROUTES_ROOT/tmp/router-preflight-$bundle_id.$$"
    BRORAY_ROUTES_PREFLIGHT_ACTIVE_WORK="$work"
    mkdir -p "$work" || {
        broray_routes_preflight_lock_release
        broray_routes_preflight_error "Не удалось создать временный каталог."
    }

    trap 'broray_routes_preflight_cleanup' EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM

    interface_out="$work/interface.out"
    interface_err="$work/interface.err"

    if ! broray_routes_preflight_ndmc "show interface $managed_interface" "$interface_out" "$interface_err" 8; then
        broray_routes_preflight_error "Не удалось проверить состояние $managed_interface."
    fi

    # `show interface <name>` is scoped to the configured ProxyN and is used
    # as the actual Keenetic existence/up-state check for route operations.
    # token.  R14C01 does not assign undocumented identity semantics to `id:`.
    grep -Eq '^[[:space:]]*connected:[[:space:]]*yes[[:space:]]*$' "$interface_out" ||
        broray_routes_preflight_error "$managed_interface не подключён."
    grep -Eq '^[[:space:]]*state:[[:space:]]*up[[:space:]]*$' "$interface_out" ||
        broray_routes_preflight_error "$managed_interface не находится в состоянии up."

    raw_rci="$work/routes-rci-raw.json"
    actual_json="$work/routes-rci.json"

    broray_routes_preflight_fetch_rci "$raw_rci" "$actual_json" ||
        broray_routes_preflight_error "Не удалось получить статические маршруты через локальный RCI."

    now="$(broray_routes_preflight_now)"
    preflight_new="$work/router-preflight.json"

    broray_routes_preflight_classify "$plan" "$actual_json" "$registry" "$preflight_new" "$now" ||
        broray_routes_preflight_error "Не удалось сформировать предварительный план."

    jq -e --arg bundle_id "$bundle_id" --arg target_interface "$managed_interface" '
        (.schemaVersion == 2) and
        (.bundleId == $bundle_id) and
        (.targetInterface == $target_interface) and
        (.source == "running-config") and
        (.routerChanged == false) and
        (.configurationSaved == false) and
        ((.summary.staticOtherConflicts | type) == "number") and
        ((.summary.unknownSerialization | type) == "number") and
        (.identityModel == "destination-interface-gateway") and
        (.writePolicy.path == "static-routes") and
        ((.writePolicy.enabled | type) == "boolean") and
        ((.routes | type) == "array") and
        (all(.routes[]; .targetInterface == $target_interface))
    ' "$preflight_new" >/dev/null ||
        broray_routes_preflight_error "Предварительный план не прошёл проверку."

    preflight_path="$catalog/router-preflight.json"
    mv "$preflight_new" "$preflight_path" ||
        broray_routes_preflight_error "Не удалось сохранить предварительный план."
    chmod 644 "$preflight_path" ||
        broray_routes_preflight_error "Не удалось установить права предварительного плана."

    summary_total="$(jq -r '.summary.total' "$preflight_path")"
    summary_create="$(jq -r '.summary.toCreate' "$preflight_path")"
    summary_managed="$(jq -r '.summary.managedExisting' "$preflight_path")"
    summary_external="$(jq -r '.summary.externalExisting' "$preflight_path")"
    summary_conflicts="$(jq -r '.summary.conflicts' "$preflight_path")"
    summary_static_other="$(jq -r '.summary.staticOtherConflicts' "$preflight_path")"
    summary_other="$(jq -r '.summary.withOtherInterfaceMatches' "$preflight_path")"
    can_export="$(jq -r '.canExport' "$preflight_path")"
    message="$(jq -r '.message' "$preflight_path")"

    state_new="$state.new.$$"

    jq \
        --arg now "$now" \
        --arg message "$message" \
        --arg preflight_file "$preflight_path" \
        --argjson total "$summary_total" \
        --argjson to_create "$summary_create" \
        --argjson managed_existing "$summary_managed" \
        --argjson external_existing "$summary_external" \
        --argjson conflicts "$summary_conflicts" \
        --argjson static_other "$summary_static_other" \
        --argjson other_matches "$summary_other" \
        --argjson can_export "$can_export" '
        .preflight = {
            result: (if $conflicts > 0 then "conflicts" else "ready" end),
            message: $message,
            file: $preflight_file,
            total: $total,
            toCreate: $to_create,
            managedExisting: $managed_existing,
            externalExisting: $external_existing,
            conflicts: $conflicts,
            staticOtherConflicts: $static_other,
            withOtherInterfaceMatches: $other_matches,
            canExport: $can_export,
            routerChanged: false,
            configurationSaved: false,
            source: "running-config",
            checkedAt: $now
        } |
        .lastError = null |
        .updatedAt = $now
    ' "$state" >"$state_new" ||
        broray_routes_preflight_error "Не удалось обновить локальное состояние."

    mv "$state_new" "$state" ||
        broray_routes_preflight_error "Не удалось сохранить локальное состояние."
    chmod 644 "$state" ||
        broray_routes_preflight_error "Не удалось установить права локального состояния."

    broray_routes_preflight_lock_release
    rm -rf "$work"
    BRORAY_ROUTES_PREFLIGHT_ACTIVE_WORK=""
    trap - EXIT HUP INT TERM

    echo "$message"
    echo "Набор: $bundle_id"
    echo "Управляемый интерфейс: $managed_interface"
    echo "Всего маршрутов: $summary_total"
    echo "Будет создано: $summary_create"
    echo "Уже управляются BROray: $summary_managed"
    echo "Существуют на $managed_interface вне BROray: $summary_external"
    echo "Совпадения на других интерфейсах: $summary_other"
    echo "Статических конфликтов на других интерфейсах: $summary_static_other"
    echo "Всего конфликтов: $summary_conflicts"
    echo "Экспорт разрешён: $can_export"
    echo "Маршруты Keenetic не изменялись."
}

# BROray reliable local RCI reader v2
broray_routes_preflight_fetch_rci__shadowed_legacy_2_unused()
{
    local raw_file output_file attempt fetch_ok route_filter error_file

    raw_file="$1"
    output_file="$2"
    error_file="${raw_file}.error"
    attempt=1

    while [ "$attempt" -le 3 ]
    do
        rm -f "$raw_file" "$output_file" "$error_file"
        fetch_ok=false

        if command -v curl >/dev/null 2>&1; then
            if curl \
                --silent \
                --show-error \
                --fail \
                --connect-timeout 2 \
                --max-time 8 \
                "$BRORAY_ROUTES_PREFLIGHT_RCI_URL" \
                >"$raw_file" 2>"$error_file"
            then
                fetch_ok=true
            fi
        fi

        if [ "$fetch_ok" != true ] && command -v wget >/dev/null 2>&1; then
            rm -f "$raw_file"

            if wget \
                -q \
                -T 8 \
                -O "$raw_file" \
                "$BRORAY_ROUTES_PREFLIGHT_RCI_URL" \
                2>"$error_file"
            then
                fetch_ok=true
            fi
        fi

        if [ "$fetch_ok" = true ] && [ -s "$raw_file" ] && jq -e . "$raw_file" >/dev/null 2>&1; then
            route_filter=""

            if jq -e '(.route | type) == "array"' "$raw_file" >/dev/null 2>&1; then
                route_filter='.route'
            elif jq -e '(.routes | type) == "array"' "$raw_file" >/dev/null 2>&1; then
                route_filter='.routes'
            elif jq -e '(.show.ip.route.route | type) == "array"' "$raw_file" >/dev/null 2>&1; then
                route_filter='.show.ip.route.route'
            elif jq -e 'type == "array"' "$raw_file" >/dev/null 2>&1; then
                route_filter='.'
            fi

            if [ -n "$route_filter" ]; then
                if jq "$route_filter | {schemaVersion: 1, routes: .}" "$raw_file" >"$output_file" 2>"$error_file" &&
                    jq -e '(.schemaVersion == 1) and ((.routes | type) == "array")' "$output_file" >/dev/null 2>&1
                then
                    rm -f "$error_file"
                    return 0
                fi
            fi
        fi

        attempt=$((attempt + 1))
        [ "$attempt" -gt 3 ] || sleep 1
    done

    cp -f "$raw_file" /opt/broray/tmp/broray-rci-preflight-last.json 2>/dev/null || true
    cp -f "$error_file" /opt/broray/tmp/broray-rci-preflight-last.error 2>/dev/null || true
    return 1
}

# R14C01 route identity is destination + interface + gateway.  Metric remains an
# observed attribute; it is never used to distinguish two route objects.
broray_routes_preflight_classify()
{
    local plan_file actual_file registry_file output_file checked_at
    local write_policy_enabled write_policy_sha write_policy_rc

    plan_file="$1"
    actual_file="$2"
    registry_file="$3"
    output_file="$4"
    checked_at="${5:-$(broray_routes_preflight_now)}"
    write_policy_enabled=false
    write_policy_sha=""
    write_policy_rc=3

    if command -v broray_routes_static_write_policy_check >/dev/null 2>&1; then
        write_policy_rc=0
        broray_routes_static_write_policy_check || write_policy_rc=$?
        if [ "$write_policy_rc" -eq 0 ]; then
            write_policy_enabled=true
            write_policy_sha="$(broray_routes_static_write_policy_sha256 2>/dev/null || true)"
        fi
    fi

    jq -n \
        --slurpfile plan "$plan_file" \
        --slurpfile actual "$actual_file" \
        --slurpfile registry "$registry_file" \
        --arg checked_at "$checked_at" \
        --arg write_policy_sha "$write_policy_sha" \
        --argjson write_policy_enabled "$write_policy_enabled" \
        --argjson write_policy_rc "$write_policy_rc" '
        $plan[0] as $p |
        $actual[0] as $a |
        $registry[0] as $g |
        ([$a.routes[]? | select(.serialization.known != true)] | length) as $unknown_count |
        [
            $p.routes[] as $route |
            ($route.network + "/" + ($route.prefix | tostring)) as $destination |
            [$a.routes[]? | select(.serialization.known == true and .destination == $destination)] as $matches |
            [$matches[] | select(.interface == $p.targetInterface)] as $target_matches |
            [$target_matches[] | select(.gateway == "0.0.0.0" and .proto == "static")] as $identity_matches |
            [$identity_matches[] | select(.serialization.metricExplicit == true and .metric == $route.metric)] as $attribute_matches |
            [$matches[] | select(.interface != $p.targetInterface)] as $other_matches |
            [
                $g.routes[]? |
                select(
                    .key == $route.key and
                    .interface == $p.targetInterface and
                    ((.gatewayToken // "0.0.0.0") == "0.0.0.0") and
                    .createdByBROray == true and
                    .managed == true
                )
            ] as $owned_entries |
            (
                if $unknown_count > 0 then "conflict"
                elif ($identity_matches | length) > 1 then "conflict"
                elif ($target_matches | length) > ($identity_matches | length) then "conflict"
                elif ($identity_matches | length) == 1 then
                    if ($attribute_matches | length) != 1 then "conflict"
                    elif ($owned_entries | length) == 1 then "managed_existing"
                    else "external_existing"
                    end
                elif ($target_matches | length) == 0 then "create"
                else "conflict"
                end
            ) as $status |
            $route + {
                destination: $destination,
                status: $status,
                managedInterfaceMatches: $target_matches,
                identityMatches: $identity_matches,
                attributeMatches: $attribute_matches,
                otherInterfaceMatches: $other_matches,
                parallelStaticMatches: $other_matches,
                otherInterfaces: ([$other_matches[].interface] | unique),
                parallelStaticInterfaces: ([$other_matches[].interface] | unique),
                staticOtherInterfaceMatches: [],
                staticOtherInterfaces: [],
                ownership: (
                    if $status == "managed_existing" then "broray"
                    elif $status == "external_existing" then "external"
                    else null end
                ),
                conflictReason: (
                    if $unknown_count > 0 then
                        "running-config содержит неподтверждённую сериализацию статического маршрута."
                    elif ($identity_matches | length) > 1 then
                        "Найдено несколько записей одной идентичности маршрута."
                    elif ($target_matches | length) > ($identity_matches | length) then
                        "Маршрут той же сети имеет другие параметры идентичности."
                    elif ($identity_matches | length) == 1 and ($attribute_matches | length) != 1 then
                        "Атрибуты маршрута отличаются; безопасное обновление не подтверждено."
                    else null end
                )
            }
        ] as $routes |
        {
            schemaVersion: 2,
            bundleId: $p.bundleId,
            sourceCommit: $p.sourceCommit,
            contentSha256: $p.contentSha256,
            targetInterface: $p.targetInterface,
            managedMetric: 1200,
            identityModel: "destination-interface-gateway",
            checkedAt: $checked_at,
            routerChanged: false,
            configurationSaved: false,
            source: "running-config",
            summary: {
                total: ($routes | length),
                toCreate: ([$routes[] | select(.status == "create")] | length),
                managedExisting: ([$routes[] | select(.status == "managed_existing")] | length),
                externalExisting: ([$routes[] | select(.status == "external_existing")] | length),
                conflicts: ([$routes[] | select(.status == "conflict")] | length),
                unknownSerialization: $unknown_count,
                staticOtherConflicts: 0,
                parallelStaticOther: ([$routes[] | select((.parallelStaticMatches | length) > 0)] | length),
                withOtherInterfaceMatches: ([$routes[] | select((.otherInterfaceMatches | length) > 0)] | length)
            },
            routes: $routes,
            writePolicy: {
                path: "static-routes",
                enabled: $write_policy_enabled,
                protocolSha256: (if $write_policy_sha == "" then null else $write_policy_sha end),
                checkRc: $write_policy_rc
            }
        } |
        .canExport = (
            (.summary.conflicts == 0) and
            (.summary.externalExisting == 0) and
            (.summary.unknownSerialization == 0) and
            (.writePolicy.enabled == true)
        ) |
        .message = (
            if .writePolicy.enabled != true then
                "Запись статических маршрутов отключена до привязки физического протокола Keenetic CLI."
            elif .summary.unknownSerialization > 0 then
                "Обнаружена неподтверждённая сериализация статического маршрута."
            elif .summary.conflicts > 0 then
                "Обнаружены конфликты маршрутов на управляемом интерфейсе."
            elif .summary.toCreate == 0 then
                "Маршруты уже представлены на управляемом интерфейсе."
            elif .summary.parallelStaticOther > 0 then
                "Параллельные маршруты на других интерфейсах будут сохранены."
            else
                "Предварительная проверка экспорта завершена."
            end
        )
    ' >"$output_file"
}

# BROray configured-route source override r10a.
# Full running-config is required because `show ip route` hides configured
# routes that lose metric selection to another interface.
BRORAY_ROUTES_CONFIG_LIBRARY="${BRORAY_ROUTES_CONFIG_LIBRARY:-$BRORAY_ROOT/lib/routes-router-config.sh}"

if [ -r "$BRORAY_ROUTES_CONFIG_LIBRARY" ]; then
    . "$BRORAY_ROUTES_CONFIG_LIBRARY"
fi

broray_routes_preflight_fetch_rci()
{
    local raw_file output_file

    raw_file="$1"
    output_file="$2"

    rm -f "$raw_file" "$output_file" "${raw_file}.error"

    command -v broray_routes_config_fetch >/dev/null 2>&1 || return 1
    broray_routes_config_fetch "$output_file"
}
