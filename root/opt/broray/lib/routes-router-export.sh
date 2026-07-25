#!/opt/bin/ash

# BROray routes static conflict guard v1
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_ROUTES_ROUTER_EXPORT_LOCK="$BRORAY_ROUTES_ROOT/locks/operation.lock"

BRORAY_ROUTES_ROUTER_EXPORT_RCI_URL="${BRORAY_ROUTES_RCI_URL:-http://127.0.0.1:79/rci/show/ip/route}"

BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_PID=""
BRORAY_ROUTES_ROUTER_EXPORT_WATCHDOG_PID=""
BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK=""
BRORAY_ROUTES_ROUTER_EXPORT_CREATED_FILE=""
BRORAY_ROUTES_ROUTER_EXPORT_LOCAL_COMMITTED=false
BRORAY_ROUTES_ROUTER_EXPORT_ROUTER_SAVED=false
BRORAY_ROUTES_ROUTER_EXPORT_ORIGINAL_DIR=""
BRORAY_ROUTES_ROUTER_EXPORT_RESULT_PATH=""
BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE="${BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE:-Proxy0}"

BRORAY_INTERFACE_OWNER_LIBRARY="${BRORAY_INTERFACE_OWNER_LIBRARY:-$BRORAY_ROOT/lib/interface-owner.sh}"
if [ -r "$BRORAY_INTERFACE_OWNER_LIBRARY" ]; then
    BRORAY_BASE="$BRORAY_ROOT"
    export BRORAY_BASE
    . "$BRORAY_INTERFACE_OWNER_LIBRARY"
fi

broray_routes_router_export_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_router_export_stamp()
{
    date '+%Y%m%d-%H%M%S'
}

broray_routes_router_export_error()
{
    printf 'ОШИБКА: %s\n' "$*" >&2
    exit 1
}

broray_routes_router_export_bundle_id_valid()
{
    local value

    value="${1:-}"

    [ -n "$value" ] || return 1

    case "$value" in
        *[!a-z0-9_-]*) return 1 ;;
    esac

    return 0
}

broray_routes_router_export_is_pid()
{
    case "${1:-}" in
        ''|*[!0-9]*) return 1 ;;
    esac

    [ "$1" -gt 1 ] 2>/dev/null
}

broray_routes_router_export_lock_acquire()
{
    local lock_parent lock_pid

    lock_parent="$(dirname "$BRORAY_ROUTES_ROUTER_EXPORT_LOCK")"
    mkdir -p "$lock_parent" || return 1

    if mkdir "$BRORAY_ROUTES_ROUTER_EXPORT_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_ROUTES_ROUTER_EXPORT_LOCK/pid"
        printf '%s\n' "export" >"$BRORAY_ROUTES_ROUTER_EXPORT_LOCK/action"
        printf '%s\n' "$BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE" \
            >"$BRORAY_ROUTES_ROUTER_EXPORT_LOCK/bundle"
        return 0
    fi

    lock_pid="$(
        sed -n '1p' \
            "$BRORAY_ROUTES_ROUTER_EXPORT_LOCK/pid" \
            2>/dev/null
    )"

    if broray_routes_router_export_is_pid "$lock_pid" &&
       kill -0 "$lock_pid" 2>/dev/null
    then
        return 2
    fi

    rm -rf "$BRORAY_ROUTES_ROUTER_EXPORT_LOCK" 2>/dev/null ||
        return 1

    if mkdir "$BRORAY_ROUTES_ROUTER_EXPORT_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_ROUTES_ROUTER_EXPORT_LOCK/pid"
        printf '%s\n' "export" >"$BRORAY_ROUTES_ROUTER_EXPORT_LOCK/action"
        printf '%s\n' "$BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE" \
            >"$BRORAY_ROUTES_ROUTER_EXPORT_LOCK/bundle"
        return 0
    fi

    return 1
}

broray_routes_router_export_lock_release()
{
    rm -rf "$BRORAY_ROUTES_ROUTER_EXPORT_LOCK" 2>/dev/null || true
}

broray_routes_router_export_kill_active()
{
    local pid watchdog

    pid="$BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_PID"
    watchdog="$BRORAY_ROUTES_ROUTER_EXPORT_WATCHDOG_PID"

    if broray_routes_router_export_is_pid "$watchdog" &&
       kill -0 "$watchdog" 2>/dev/null
    then
        kill "$watchdog" 2>/dev/null || true
        wait "$watchdog" 2>/dev/null || true
    fi

    BRORAY_ROUTES_ROUTER_EXPORT_WATCHDOG_PID=""

    if broray_routes_router_export_is_pid "$pid" &&
       kill -0 "$pid" 2>/dev/null
    then
        kill "$pid" 2>/dev/null || true
        sleep 1

        if kill -0 "$pid" 2>/dev/null; then
            kill -9 "$pid" 2>/dev/null || true
        fi
    fi

    if broray_routes_router_export_is_pid "$pid"; then
        wait "$pid" 2>/dev/null || true
    fi

    BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_PID=""
}

broray_routes_router_export_ndmc()
{
    local command_text output_file error_file limit timeout_file
    local command_pid watchdog_pid result timed_out

    command_text="$1"
    output_file="$2"
    error_file="$3"
    limit="${4:-8}"
    timeout_file="$output_file.timeout"

    : >"$output_file"
    : >"$error_file"
    rm -f "$timeout_file"

    "$BRORAY_ROUTES_ROUTER_EXPORT_NDMC" -c "$command_text" \
        >"$output_file" 2>"$error_file" &

    command_pid=$!
    BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_PID="$command_pid"

    (
        sleep "$limit"

        if kill -0 "$command_pid" 2>/dev/null; then
            : >"$timeout_file"
            kill "$command_pid" 2>/dev/null || true
            sleep 1

            if kill -0 "$command_pid" 2>/dev/null; then
                kill -9 "$command_pid" 2>/dev/null || true
            fi
        fi
    ) &

    watchdog_pid=$!
    BRORAY_ROUTES_ROUTER_EXPORT_WATCHDOG_PID="$watchdog_pid"

    if wait "$command_pid" 2>/dev/null; then
        result=0
    else
        result=$?
    fi

    if kill -0 "$watchdog_pid" 2>/dev/null; then
        kill "$watchdog_pid" 2>/dev/null || true
    fi

    wait "$watchdog_pid" 2>/dev/null || true

    BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_PID=""
    BRORAY_ROUTES_ROUTER_EXPORT_WATCHDOG_PID=""

    timed_out=false
    [ ! -f "$timeout_file" ] || timed_out=true
    rm -f "$timeout_file"

    if [ "$timed_out" = true ]; then
        return 124
    fi

    return "$result"
}

broray_routes_router_export_fetch_rci()
{
    local output_file raw_file

    output_file="$1"
    raw_file="$output_file.raw"

    if command -v curl >/dev/null 2>&1; then
        curl \
            --silent \
            --show-error \
            --fail \
            --connect-timeout 2 \
            --max-time 8 \
            "$BRORAY_ROUTES_ROUTER_EXPORT_RCI_URL" >"$raw_file" || return 1
    elif command -v wget >/dev/null 2>&1; then
        wget -q -T 8 -O "$raw_file" "$BRORAY_ROUTES_ROUTER_EXPORT_RCI_URL" || return 1
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

    rm -f "$raw_file"

    jq -e '
        (.schemaVersion == 1) and
        ((.routes | type) == "array")
    ' "$output_file" >/dev/null
}

broray_routes_router_export_static_conflicts()
{
    local plan_file actual_file output_file

    plan_file="$1"
    actual_file="$2"
    output_file="$3"

    jq -n \
        --slurpfile plan "$plan_file" \
        --slurpfile actual "$actual_file" '
        $plan[0] as $p |
        $actual[0] as $a |
        [
            $p.routes[] as $route |
            ($route.network + "/" + ($route.prefix | tostring)) as $destination |
            [
                $a.routes[]? |
                select(
                    .destination == $destination and
                    .interface != $p.targetInterface and
                    (((.proto // "") | ascii_downcase) == "static")
                )
            ] as $matches |
            select(($matches | length) > 0) |
            {
                destination: $destination,
                interfaces: ([$matches[].interface] | unique),
                entries: $matches
            }
        ]
    ' >"$output_file"
}

broray_routes_router_export_exact_count()
{
    local actual_file destination target_interface

    actual_file="$1"
    destination="$2"
    target_interface="$3"

    jq -r \
        --arg destination "$destination" \
        --arg target_interface "$target_interface" '
        [
            .routes[]? |
            select(
                .destination == $destination and
                .interface == $target_interface and
                ((.gateway // "0.0.0.0") == "0.0.0.0") and
                (((.proto // "static") | ascii_downcase) == "static")
            )
        ] |
        length
    ' "$actual_file"
}

broray_routes_router_export_verify_expected_rci()
{
    local plan_file actual_file

    plan_file="$1"
    actual_file="$2"

    jq -n \
        --slurpfile plan "$plan_file" \
        --slurpfile actual "$actual_file" '
        $plan[0] as $p |
        $actual[0] as $a |
        [
            $p.routes[] |
            (.network + "/" + (.prefix | tostring)) as $destination |
            [
                $a.routes[]? |
                select(
                    .destination == $destination and
                    .interface == $p.targetInterface and
                    ((.gateway // "0.0.0.0") == "0.0.0.0") and
                    (((.proto // "static") | ascii_downcase) == "static")
                )
            ] |
            length
        ] as $counts |
        all($counts[]; . == 1)
    ' | grep -qx 'true'
}

broray_routes_router_export_parse_table()
{
    local input_file output_file

    input_file="$1"
    output_file="$2"

    awk '
        function trim(value) {
            sub(/^[[:space:]]+/, "", value)
            sub(/[[:space:]]+$/, "", value)
            return value
        }

        $1 ~ /^[0-9][0-9.]*\/[0-9][0-9]*$/ && NF >= 3 {
            destination=$1
            gateway=$2
            interface_name=$3
            metric=""

            if (getline metric_line > 0) {
                metric_line=trim(metric_line)
                count=split(metric_line, part, /[[:space:]]+/)

                if (count > 0 && part[count] ~ /^[0-9][0-9]*$/) {
                    metric=part[count]
                }
            }

            printf "%s\t%s\t%s\t%s\n", \
                destination, gateway, interface_name, metric
        }
    ' "$input_file" >"$output_file"
}

broray_routes_router_export_actual_json()
{
    local input_file output_file

    input_file="$1"
    output_file="$2"

    jq -Rn '
        [
            inputs |
            select(length > 0) |
            split("\t") |
            {
                destination: .[0],
                gateway: .[1],
                interface: .[2],
                metric: (
                    if (.[3] // "") == "" then
                        null
                    else
                        (.[3] | tonumber)
                    end
                )
            }
        ] |
        {
            schemaVersion: 1,
            routes: .
        }
    ' <"$input_file" >"$output_file"
}

broray_routes_router_export_classify()
{
    local plan_file actual_file registry_file output_file checked_at

    plan_file="$1"
    actual_file="$2"
    registry_file="$3"
    output_file="$4"
    checked_at="${5:-$(broray_routes_router_export_now)}"

    jq -n \
        --slurpfile plan "$plan_file" \
        --slurpfile actual "$actual_file" \
        --slurpfile registry "$registry_file" \
        --arg checked_at "$checked_at" \
        '
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
            ] as $managed_interface_matches |
            [
                $managed_interface_matches[] |
                select(.gateway == "0.0.0.0")
            ] as $exact_matches |
            [
                $matches[] |
                select(.interface != $p.targetInterface)
            ] as $other_interface_matches |
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
                if ($exact_matches | length) > 1 then
                    "conflict"
                elif ($exact_matches | length) == 1 then
                    if ($owned_entries | length) == 1 then
                        "managed_existing"
                    else
                        "external_existing"
                    end
                elif ($managed_interface_matches | length) > 0 then
                    "conflict"
                else
                    "create"
                end
            ) as $status |

            $route + {
                destination: $destination,
                status: $status,
                managedInterfaceMatches: $managed_interface_matches,
                otherInterfaceMatches: $other_interface_matches,
                otherInterfaces: (
                    [$other_interface_matches[].interface] |
                    unique
                ),
                ownership: (
                    if $status == "managed_existing" then
                        "broray"
                    elif $status == "external_existing" then
                        "external"
                    else
                        null
                    end
                ),
                conflictReason: (
                    if ($exact_matches | length) > 1 then
                        "На управляемом интерфейсе найдено несколько одинаковых записей."
                    elif
                        ($managed_interface_matches | length) > 0 and
                        ($exact_matches | length) == 0
                    then
                        "На управляемом интерфейсе существует маршрут с другим шлюзом."
                    else
                        null
                    end
                )
            }
        ] as $routes |

        {
            schemaVersion: 1,
            bundleId: $p.bundleId,
            sourceCommit: $p.sourceCommit,
            contentSha256: $p.contentSha256,
            targetInterface: $p.targetInterface,
            checkedAt: $checked_at,
            routerChanged: false,
            configurationSaved: false,
            summary: {
                total: ($routes | length),
                toCreate: (
                    [$routes[] | select(.status == "create")] |
                    length
                ),
                managedExisting: (
                    [$routes[] | select(.status == "managed_existing")] |
                    length
                ),
                externalExisting: (
                    [$routes[] | select(.status == "external_existing")] |
                    length
                ),
                conflicts: (
                    [$routes[] | select(.status == "conflict")] |
                    length
                ),
                withOtherInterfaceMatches: (
                    [
                        $routes[] |
                        select((.otherInterfaces | length) > 0)
                    ] |
                    length
                )
            },
            routes: $routes
        } |
        .canExport = (
            (.summary.conflicts == 0) and
            (.summary.externalExisting == 0)
        )
        ' >"$output_file"
}

broray_routes_router_export_verify_expected()
{
    local plan_file actual_file

    plan_file="$1"
    actual_file="$2"

    jq -n \
        --slurpfile plan "$plan_file" \
        --slurpfile actual "$actual_file" '
        $plan[0] as $p |
        $actual[0] as $a |

        [
            $p.routes[] |
            (.network + "/" + (.prefix | tostring)) as $destination |
            [
                $a.routes[]? |
                select(
                    .destination == $destination and
                    .interface == $p.targetInterface and
                    .gateway == "0.0.0.0"
                )
            ] |
            length
        ] as $counts |

        all($counts[]; . == 1)
    ' | grep -qx 'true'
}

broray_routes_router_export_build_managed_json()
{
    local preflight_file output_file now tab fingerprint key
    local family network prefix mask target gateway metric automatic exclusive comment
    local input_tsv output_tsv

    preflight_file="$1"
    output_file="$2"
    now="$3"
    input_tsv="$BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK/managed-input.tsv"
    output_tsv="$BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK/managed.tsv"
    tab="$(printf '\t')"

    jq -r '
        .routes[] |
        select(
            .status == "create" or
            .status == "managed_existing"
        ) |
        [
            .key,
            .family,
            .network,
            (.prefix | tostring),
            .mask,
            .targetInterface,
            .gatewayToken,
            (
                if .metric == null then
                    "null"
                else
                    (.metric | tostring)
                end
            ),
            (
                if .automatic == null then
                    "null"
                else
                    (.automatic | tostring)
                end
            ),
            (
                if .exclusive == null then
                    "null"
                else
                    (.exclusive | tostring)
                end
            ),
            .comment
        ] |
        @tsv
    ' "$preflight_file" >"$input_tsv" ||
        return 1

    : >"$output_tsv"

    while IFS="$tab" read -r \
        key family network prefix mask target gateway metric automatic exclusive comment
    do
        [ -n "$key" ] || continue

        fingerprint="$(
            printf '%s' "$key|$comment" |
                sha256sum |
                awk '{print $1}'
        )"

        printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
            "$key" \
            "$family" \
            "$network" \
            "$prefix" \
            "$mask" \
            "$target" \
            "$gateway" \
            "$metric" \
            "$automatic" \
            "$exclusive" \
            "$comment" \
            "$fingerprint" \
            >>"$output_tsv"
    done <"$input_tsv"

    jq -Rn \
        --arg now "$now" '
        [
            inputs |
            select(length > 0) |
            split("\t") |
            {
                key: .[0],
                family: .[1],
                network: .[2],
                prefix: (.[3] | tonumber),
                mask: .[4],
                interface: .[5],
                gatewayToken: .[6],
                metric: (
                    if .[7] == "null" then null
                    else (.[7] | tonumber)
                    end
                ),
                automatic: (
                    if .[8] == "null" then null
                    else (.[8] == "true")
                    end
                ),
                exclusive: (
                    if .[9] == "null" then null
                    else (.[9] == "true")
                    end
                ),
                comment: .[10],
                fingerprint: .[11],
                createdByBROray: true,
                managed: true,
                routerCommentPersisted: false,
                actualStatus: "present",
                createdAt: $now,
                updatedAt: $now
            }
        ] |
        {
            schemaVersion: 1,
            routes: .
        }
    ' <"$output_tsv" >"$output_file"
}

broray_routes_router_export_prepare_files()
{
    local bundle_id preflight_file registry_file bundle_registry_file
    local state_file plan_file output_dir now managed_json
    local global_new bundle_new state_new plan_new result_new
    local total created managed_existing external_existing other_matches
    local installed_version message

    bundle_id="$1"
    preflight_file="$2"
    registry_file="$3"
    bundle_registry_file="$4"
    state_file="$5"
    plan_file="$6"
    output_dir="$7"
    now="$8"

    managed_json="$output_dir/managed.json"
    global_new="$output_dir/routes.json"
    bundle_new="$output_dir/bundle.json"
    state_new="$output_dir/state.json"
    plan_new="$output_dir/export-plan.json"
    result_new="$output_dir/router-export-result.json"

    mkdir -p "$output_dir" || return 1

    broray_routes_router_export_build_managed_json \
        "$preflight_file" \
        "$managed_json" \
        "$now" ||
        return 1

    jq -n \
        --slurpfile old "$registry_file" \
        --slurpfile managed "$managed_json" \
        --arg bundle_id "$bundle_id" \
        --arg now "$now" \
        --arg target_interface "$BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE" '
        $old[0] as $o |
        $managed[0].routes as $managed_routes |

        (reduce $managed_routes[] as $route (
            ($o.routes // []);
            (map(.key) | index($route.key)) as $index |

            if $index == null then
                . + [
                    $route + {
                        owners: [$bundle_id]
                    }
                ]
            else
                .[$index] = (
                    .[$index] +
                    $route +
                    {
                        createdAt: (
                            .[$index].createdAt //
                            $route.createdAt
                        ),
                        owners: (
                            (
                                (.[$index].owners // []) +
                                [$bundle_id]
                            ) |
                            unique
                        ),
                        updatedAt: $now
                    }
                )
            end
        )) as $routes |

        $o + {
            schemaVersion: 1,
            managedInterface: $target_interface,
            routes: $routes,
            updatedAt: $now
        }
    ' >"$global_new" ||
        return 1

    jq -n \
        --slurpfile old "$bundle_registry_file" \
        --slurpfile preflight "$preflight_file" \
        --slurpfile state "$state_file" \
        --arg bundle_id "$bundle_id" \
        --arg target_interface "$BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE" \
        --arg now "$now" '
        $old[0] as $o |
        $preflight[0] as $p |
        $state[0] as $s |

        $o + {
            schemaVersion: 1,
            bundleId: $bundle_id,
            installedVersion: $s.downloadedVersion,
            routeKeys: (
                [$p.routes[].key] |
                unique
            ),
            managedRouteKeys: (
                [
                    $p.routes[] |
                    select(
                        .status == "create" or
                        .status == "managed_existing"
                    ) |
                    .key
                ] |
                unique
            ),
            externalRouteKeys: (
                [
                    $p.routes[] |
                    select(.status == "external_existing") |
                    .key
                ] |
                unique
            ),
            targetInterface: $target_interface,
            managedMetric: 1200,
            installedAt: $now,
            updatedAt: $now
        }
    ' >"$bundle_new" ||
        return 1

    total="$(jq -r '.summary.total' "$preflight_file")"
    created="$(jq -r '.summary.toCreate' "$preflight_file")"
    managed_existing="$(jq -r '.summary.managedExisting' "$preflight_file")"
    external_existing="$(jq -r '.summary.externalExisting' "$preflight_file")"
    other_matches="$(
        jq -r '.summary.withOtherInterfaceMatches' \
            "$preflight_file"
    )"
    message="Маршруты экспортированы в Keenetic"

    jq \
        --arg now "$now" \
        --arg message "$message" \
        --arg target_interface "$BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE" \
        --argjson total "$total" \
        --argjson created "$created" \
        --argjson managed_existing "$managed_existing" \
        --argjson external_existing "$external_existing" \
        --argjson other_matches "$other_matches" '
        .status = "installed" |
        .installedVersion = .downloadedVersion |
        .lastExportedAt = $now |
        .exportResult = {
            result: "installed",
            message: $message,
            total: $total,
            created: $created,
            managedExisting: $managed_existing,
            externalExisting: $external_existing,
            withOtherInterfaceMatches: $other_matches,
            targetInterface: $target_interface,
            configurationSaved: true,
            completedAt: $now
        } |
        .preflight.routerChanged = ($created > 0) |
        .preflight.configurationSaved = true |
        .lastError = null |
        .updatedAt = $now
    ' "$state_file" >"$state_new" ||
        return 1

    jq \
        --arg now "$now" \
        --arg message "$message" \
        --argjson created "$created" \
        --argjson managed_existing "$managed_existing" \
        --argjson external_existing "$external_existing" '
        .routerApplied = true |
        .configurationSaved = true |
        .appliedAt = $now |
        .applyResult = {
            message: $message,
            created: $created,
            managedExisting: $managed_existing,
            externalExisting: $external_existing
        }
    ' "$plan_file" >"$plan_new" ||
        return 1

    jq -n \
        --slurpfile preflight "$preflight_file" \
        --slurpfile state "$state_new" \
        --arg bundle_id "$bundle_id" \
        --arg now "$now" \
        --arg message "$message" \
        --arg target_interface "$BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE" '
        {
            schemaVersion: 1,
            bundleId: $bundle_id,
            targetInterface: $target_interface,
            sourceCommit: $state[0].installedVersion.sourceCommit,
            contentSha256: $state[0].installedVersion.contentSha256,
            message: $message,
            summary: {
                total: $preflight[0].summary.total,
                created: $preflight[0].summary.toCreate,
                managedExisting: $preflight[0].summary.managedExisting,
                externalExisting: $preflight[0].summary.externalExisting,
                withOtherInterfaceMatches:
                    $preflight[0].summary.withOtherInterfaceMatches
            },
            routerChanged: (
                $preflight[0].summary.toCreate > 0
            ),
            configurationSaved: true,
            completedAt: $now
        }
    ' >"$result_new" ||
        return 1

    jq -e --arg target_interface "$BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE" '
        (.schemaVersion == 1) and
        (.managedInterface == $target_interface) and
        ((.routes | type) == "array") and
        (all(.routes[];
            (.interface == $target_interface) and
            (.createdByBROray == true) and
            (.managed == true) and
            ((.owners | type) == "array") and
            ((.owners | length) > 0)
        ))
    ' "$global_new" >/dev/null ||
        return 1

    jq -e \
        --arg bundle_id "$bundle_id" '
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        (.installedVersion != null) and
        ((.routeKeys | type) == "array") and
        ((.managedRouteKeys | type) == "array") and
        ((.externalRouteKeys | type) == "array")
    ' "$bundle_new" >/dev/null ||
        return 1

    jq -e --arg target_interface "$BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE" '
        (.status == "installed") and
        (.installedVersion != null) and
        (.exportResult.result == "installed") and
        (.exportResult.targetInterface == $target_interface) and
        (.exportResult.configurationSaved == true)
    ' "$state_new" >/dev/null ||
        return 1

    jq -e --arg target_interface "$BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE" '
        (.targetInterface == $target_interface) and
        (.routerApplied == true) and
        (.configurationSaved == true)
    ' "$plan_new" >/dev/null ||
        return 1

    return 0
}

broray_routes_router_export_backup_local()
{
    local registry bundle_registry state plan result original

    registry="$1"
    bundle_registry="$2"
    state="$3"
    plan="$4"
    result="$5"
    original="$6"

    mkdir -p "$original" || return 1

    cp -p "$registry" "$original/routes.json" || return 1
    cp -p "$bundle_registry" "$original/bundle.json" || return 1
    cp -p "$state" "$original/state.json" || return 1
    cp -p "$plan" "$original/export-plan.json" || return 1

    if [ -f "$result" ]; then
        cp -p "$result" "$original/router-export-result.json" ||
            return 1
    else
        : >"$original/result-did-not-exist"
    fi

    return 0
}

broray_routes_router_export_install_local()
{
    local prepared registry bundle_registry state plan result

    prepared="$1"
    registry="$2"
    bundle_registry="$3"
    state="$4"
    plan="$5"
    result="$6"

    mv "$prepared/routes.json" "$registry" || return 1
    mv "$prepared/bundle.json" "$bundle_registry" || return 1
    mv "$prepared/state.json" "$state" || return 1
    mv "$prepared/export-plan.json" "$plan" || return 1
    mv "$prepared/router-export-result.json" "$result" || return 1

    chmod 644 \
        "$registry" \
        "$bundle_registry" \
        "$state" \
        "$plan" \
        "$result" ||
        return 1

    return 0
}

broray_routes_router_export_restore_local()
{
    local registry bundle_registry state plan result original

    registry="$1"
    bundle_registry="$2"
    state="$3"
    plan="$4"
    result="$5"
    original="$6"

    [ -d "$original" ] || return 0

    cp -p "$original/routes.json" "$registry" 2>/dev/null || true
    cp -p "$original/bundle.json" "$bundle_registry" 2>/dev/null || true
    cp -p "$original/state.json" "$state" 2>/dev/null || true
    cp -p "$original/export-plan.json" "$plan" 2>/dev/null || true

    if [ -f "$original/result-did-not-exist" ]; then
        rm -f "$result" 2>/dev/null || true
    elif [ -f "$original/router-export-result.json" ]; then
        cp -p \
            "$original/router-export-result.json" \
            "$result" \
            2>/dev/null ||
            true
    fi

    BRORAY_ROUTES_ROUTER_EXPORT_LOCAL_COMMITTED=false
}

broray_routes_router_export_rollback_created()
{
    local created_file reverse_file network mask key
    local output error tab

    created_file="$BRORAY_ROUTES_ROUTER_EXPORT_CREATED_FILE"

    [ -s "$created_file" ] || return 0
    [ -n "$BRORAY_ROUTES_ROUTER_EXPORT_NDMC" ] || return 1

    reverse_file="$BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK/created.reverse.tsv"
    tab="$(printf '\t')"

    awk '
        {
            line[NR]=$0
        }

        END {
            for (line_no=NR; line_no>=1; line_no-=1) {
                print line[line_no]
            }
        }
    ' "$created_file" >"$reverse_file" ||
        return 1

    echo
    echo "Выполняется откат маршрутов текущей операции..." >&2

    while IFS="$tab" read -r network mask key
    do
        [ -n "$network" ] || continue

        output="$BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK/rollback.out"
        error="$BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK/rollback.err"

        if ! broray_routes_router_export_ndmc \
            "no ip route $network $mask $BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE" \
            "$output" \
            "$error" \
            8
        then
            echo \
                "ВНИМАНИЕ: не удалось удалить $network/$mask ($key)" \
                >&2
        fi
    done <"$reverse_file"

    return 0
}

broray_routes_router_export_cleanup()
{
    trap - EXIT HUP INT TERM

    broray_routes_router_export_kill_active

    if [ "$BRORAY_ROUTES_ROUTER_EXPORT_ROUTER_SAVED" != true ]; then
        if [ "$BRORAY_ROUTES_ROUTER_EXPORT_LOCAL_COMMITTED" = true ]; then
            broray_routes_router_export_restore_local \
                "$BRORAY_ROUTES_ROUTER_EXPORT_REGISTRY" \
                "$BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE_REGISTRY" \
                "$BRORAY_ROUTES_ROUTER_EXPORT_STATE" \
                "$BRORAY_ROUTES_ROUTER_EXPORT_PLAN" \
                "$BRORAY_ROUTES_ROUTER_EXPORT_RESULT_PATH" \
                "$BRORAY_ROUTES_ROUTER_EXPORT_ORIGINAL_DIR"
        fi

        broray_routes_router_export_rollback_created || true
    fi

    if [ -n "$BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK" ]; then
        rm -rf \
            "$BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK" \
            2>/dev/null ||
            true
    fi

    broray_routes_router_export_lock_release
}

broray_routes_router_export_transaction_write()
{
    local file phase bundle_id now created_count saved committed message new

    file="$1"
    phase="$2"
    bundle_id="$3"
    now="$4"
    created_count="$5"
    saved="$6"
    committed="$7"
    message="$8"
    new="$file.new.$$"

    jq -n \
        --arg phase "$phase" \
        --arg bundle_id "$bundle_id" \
        --arg now "$now" \
        --arg message "$message" \
        --argjson created_count "$created_count" \
        --argjson saved "$saved" \
        --argjson committed "$committed" '
        {
            schemaVersion: 1,
            operation: "export",
            bundleId: $bundle_id,
            phase: $phase,
            createdCount: $created_count,
            configurationSaved: $saved,
            committed: $committed,
            message: $message,
            updatedAt: $now
        }
    ' >"$new" ||
        return 1

    mv "$new" "$file" || return 1
    chmod 600 "$file" || return 1
}

broray_routes_router_export_run()
{
    local bundle_id bundles config catalog plan preflight registry
    local bundle_registry state route_file result_path transactions
    local managed_interface route_comment downloaded_sha available_sha plan_sha
    local route_file_sha expected_route_file_sha lock_result work now
    local interface_out interface_err table_out table_err table_tsv actual_json
    local fresh_preflight conflicts can_export total to_create managed_existing
    local external_existing other_matches created_file tab network mask key destination
    local add_index add_out add_err verify_out verify_err verify_tsv verify_json
    local rci_before static_conflicts_file conflict_count route_rci exact_count
    local prepared original transaction_file created_count save_out save_err
    local final_out final_err final_tsv final_json message

    bundle_id="${1:-}"

    broray_routes_router_export_bundle_id_valid "$bundle_id" ||
        broray_routes_router_export_error \
            "Некорректный идентификатор набора."

    BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE="$bundle_id"
    export BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE

    bundles="$BRORAY_ROUTES_ROOT/bundles.json"
    config="$BRORAY_ROUTES_ROOT/config.json"
    catalog="$BRORAY_ROUTES_ROOT/catalog/$bundle_id"
    plan="$catalog/export-plan.json"
    preflight="$catalog/router-preflight.json"
    registry="$BRORAY_ROUTES_ROOT/installed/routes.json"
    bundle_registry="$BRORAY_ROUTES_ROOT/installed/bundles/$bundle_id.json"
    state="$BRORAY_ROUTES_ROOT/state/$bundle_id.json"
    route_file="$catalog/keenetic-routes.bat"
    result_path="$catalog/router-export-result.json"
    transactions="$BRORAY_ROUTES_ROOT/transactions"

    for file in \
        "$bundles" \
        "$config" \
        "$plan" \
        "$preflight" \
        "$registry" \
        "$bundle_registry" \
        "$state" \
        "$route_file"
    do
        [ -r "$file" ] ||
            broray_routes_router_export_error \
                "Не найден обязательный файл: $file"
    done

    jq -e --arg bundle_id "$bundle_id" '
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (.bundles | index($bundle_id) != null)
    ' "$bundles" >/dev/null ||
        broray_routes_router_export_error \
            "Набор маршрутов не разрешён: $bundle_id"

    managed_interface="$(jq -r '.managedInterface // empty' "$config")"
    route_comment="$(jq -r '.routeComment // empty' "$config")"

    case "$managed_interface" in
        Proxy[0-9]*) ;;
        *) broray_routes_router_export_error "Некорректный управляемый интерфейс ProxyN." ;;
    esac
    case "${managed_interface#Proxy}" in
        ''|*[!0-9]*) broray_routes_router_export_error "Некорректный управляемый интерфейс ProxyN." ;;
    esac
    BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE="$managed_interface"
    export BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE


    command -v broray_interface_owner_record_valid >/dev/null 2>&1 ||
        broray_routes_router_export_error "Модуль владения ProxyN недоступен."
    broray_interface_owner_record_valid "$managed_interface" ||
        broray_routes_router_export_error "Локальный реестр владельца не подтверждает $managed_interface."
    broray_interface_owner_valid "$managed_interface" ||
        broray_routes_router_export_error "Интерфейс $managed_interface не подтверждён как принадлежащий BROray."

    [ "$route_comment" = "BROray" ] ||
        broray_routes_router_export_error \
            "Некорректная метка маршрутов."

    jq -e --arg target_interface "$managed_interface" '
        (.schemaVersion == 1) and
        (.managedInterface == $target_interface) and
        (.routeComment == "BROray") and
        (.ownershipPolicy.adoptExistingRoutes == false) and
        (.ownershipPolicy.modifyExternalRoutes == false) and
        (.ownershipPolicy.deleteExternalRoutes == false) and
        (.ownershipPolicy.touchOtherInterfaces == false) and
        (.ownershipPolicy.deleteOnlyExactManagedMatch == true)
    ' "$config" >/dev/null ||
        broray_routes_router_export_error \
            "Политика безопасности маршрутов повреждена."

    downloaded_sha="$(
        jq -r '.downloadedVersion.contentSha256 // empty' \
            "$state"
    )"
    available_sha="$(
        jq -r '.availableVersion.contentSha256 // empty' \
            "$state"
    )"
    plan_sha="$(jq -r '.contentSha256 // empty' "$plan")"

    [ -n "$downloaded_sha" ] &&
    [ "$downloaded_sha" = "$plan_sha" ] ||
        broray_routes_router_export_error \
            "Локальный план не соответствует загруженной версии."

    jq -e \
        --arg bundle_id "$bundle_id" \
        --arg content_sha "$downloaded_sha" \
        --arg target_interface "$managed_interface" '
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        (.contentSha256 == $content_sha) and
        (.targetInterface == $target_interface) and
        (.routeComment == "BROray") and
        (.routerApplied == false) and
        ((.routes | type) == "array") and
        ((.routes | length) > 0) and
        (all(.routes[];
            (.targetInterface == $target_interface) and
            (.gatewayToken == "0.0.0.0") and
            (.comment == "BROray")
        ))
    ' "$plan" >/dev/null ||
        broray_routes_router_export_error \
            "План экспорта повреждён."

    expected_route_file_sha="$(
        jq -r '.routeFileSha256 // empty' "$plan"
    )"
    route_file_sha="$(
        sha256sum "$route_file" |
            awk '{print $1}'
    )"

    [ -n "$expected_route_file_sha" ] &&
    [ "$route_file_sha" = "$expected_route_file_sha" ] ||
        broray_routes_router_export_error \
            "Файл маршрутов изменился после подготовки."

    if [ -n "${BRORAY_ROUTES_CONFIG_NDMC:-}" ] &&
       [ -x "$BRORAY_ROUTES_CONFIG_NDMC" ]
    then
        BRORAY_ROUTES_ROUTER_EXPORT_NDMC="$BRORAY_ROUTES_CONFIG_NDMC"
    else
        BRORAY_ROUTES_ROUTER_EXPORT_NDMC="$(command -v ndmc 2>/dev/null || true)"
    fi

    [ -n "$BRORAY_ROUTES_ROUTER_EXPORT_NDMC" ] ||
        broray_routes_router_export_error \
            "Команда ndmc недоступна."

    lock_result=0
    broray_routes_router_export_lock_acquire ||
        lock_result=$?

    case "$lock_result" in
        0) ;;
        2)
            broray_routes_router_export_error \
                "Другая операция с маршрутами уже выполняется."
            ;;
        *)
            broray_routes_router_export_error \
                "Не удалось установить блокировку операции."
            ;;
    esac

    work="$BRORAY_ROUTES_ROOT/tmp/router-export-$bundle_id.$$"
    BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK="$work"
    BRORAY_ROUTES_ROUTER_EXPORT_REGISTRY="$registry"
    BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE_REGISTRY="$bundle_registry"
    BRORAY_ROUTES_ROUTER_EXPORT_STATE="$state"
    BRORAY_ROUTES_ROUTER_EXPORT_PLAN="$plan"
    BRORAY_ROUTES_ROUTER_EXPORT_RESULT_PATH="$result_path"

    mkdir -p "$work" "$transactions" || {
        broray_routes_router_export_lock_release
        broray_routes_router_export_error \
            "Не удалось создать рабочий каталог."
    }

    created_file="$work/created.tsv"
    : >"$created_file"
    BRORAY_ROUTES_ROUTER_EXPORT_CREATED_FILE="$created_file"

    trap 'broray_routes_router_export_cleanup' EXIT HUP INT TERM

    interface_out="$work/interface.out"
    interface_err="$work/interface.err"

    if ! broray_routes_router_export_ndmc \
        "show interface $managed_interface" \
        "$interface_out" \
        "$interface_err" \
        8
    then
        broray_routes_router_export_error \
            "Не удалось проверить $managed_interface."
    fi

    grep -Eq "^[[:space:]]*id:[[:space:]]*$managed_interface[[:space:]]*$" \
        "$interface_out" ||
        broray_routes_router_export_error \
            "Ответ Keenetic не относится к $managed_interface."

    grep -Eq '^[[:space:]]*connected:[[:space:]]*yes[[:space:]]*$' \
        "$interface_out" ||
        broray_routes_router_export_error \
            "$managed_interface не подключён."

    grep -Eq '^[[:space:]]*state:[[:space:]]*up[[:space:]]*$' \
        "$interface_out" ||
        broray_routes_router_export_error \
            "$managed_interface не находится в состоянии up."

    rci_before="$work/routes-static-before.json"
    static_conflicts_file="$work/static-other-conflicts.json"

    broray_routes_router_export_fetch_rci "$rci_before" ||
        broray_routes_router_export_error \
            "Не удалось получить статические маршруты через локальный RCI."

    broray_routes_router_export_static_conflicts \
        "$plan" \
        "$rci_before" \
        "$static_conflicts_file" ||
        broray_routes_router_export_error \
            "Не удалось проверить статические маршруты других интерфейсов."

    conflict_count="$(jq -r 'length' "$static_conflicts_file")"

    if [ "$conflict_count" -gt 0 ]; then
        cp -p \
            "$static_conflicts_file" \
            "$catalog/router-export-conflicts.json" \
            2>/dev/null || true

        broray_routes_router_export_error \
            "Обнаружены статические маршруты на других интерфейсах: $conflict_count. Экспорт остановлен до изменений."
    fi

    rm -f "$catalog/router-export-conflicts.json" 2>/dev/null || true

    # Classify against all configured static routes. `show ip route` only
    # exposes active winners and hides valid managed-interface routes with metric 1200
    # when another interface has a lower metric.
    actual_json="$work/actual-config-before.json"
    cp -p "$rci_before" "$actual_json" ||
        broray_routes_router_export_error \
            "Не удалось подготовить снимок настроенных маршрутов."

    broray_routes_router_export_classify \
        "$plan" \
        "$actual_json" \
        "$registry" \
        "$fresh_preflight" \
        "$now" ||
        broray_routes_router_export_error \
            "Не удалось выполнить свежую проверку экспорта."

    conflicts="$(jq -r '.summary.conflicts' "$fresh_preflight")"
    can_export="$(jq -r '.canExport' "$fresh_preflight")"

    [ "$conflicts" = "0" ] &&
    [ "$can_export" = "true" ] ||
        broray_routes_router_export_error \
            "Обнаружены конфликты маршрутов на $managed_interface."

    total="$(jq -r '.summary.total' "$fresh_preflight")"
    to_create="$(jq -r '.summary.toCreate' "$fresh_preflight")"
    managed_existing="$(
        jq -r '.summary.managedExisting' \
            "$fresh_preflight"
    )"
    external_existing="$(
        jq -r '.summary.externalExisting' \
            "$fresh_preflight"
    )"
    other_matches="$(
        jq -r '.summary.withOtherInterfaceMatches' \
            "$fresh_preflight"
    )"

    transaction_file="$transactions/export-$bundle_id-$(broray_routes_router_export_stamp).json"

    broray_routes_router_export_transaction_write \
        "$transaction_file" \
        "checked" \
        "$bundle_id" \
        "$now" \
        0 \
        false \
        false \
        "Свежая проверка завершена." ||
        broray_routes_router_export_error \
            "Не удалось создать журнал операции."

    tab="$(printf '\t')"

    jq -r '
        .routes[] |
        select(.status == "create") |
        [
            .network,
            .mask,
            .key,
            .destination
        ] |
        @tsv
    ' "$fresh_preflight" >"$work/to-create.tsv" ||
        broray_routes_router_export_error \
            "Не удалось подготовить список добавления."

    add_index=0

    while IFS="$tab" read -r network mask key destination
    do
        [ -n "$network" ] || continue

        add_index=$((add_index + 1))
        add_out="$work/add-$add_index.out"
        add_err="$work/add-$add_index.err"

        if ! broray_routes_router_export_ndmc \
            "ip route $network $mask $managed_interface $BRORAY_ROUTES_ROUTER_EXPORT_METRIC" \
            "$add_out" \
            "$add_err" \
            8
        then
            broray_routes_router_export_error \
                "Не удалось добавить маршрут $network/$mask через $managed_interface."
        fi

        if grep -Eiq \
            'Renewed static route|Io::Netlink error|system failed|invalid command|unknown command' \
            "$add_out" "$add_err" 2>/dev/null
        then
            broray_routes_router_export_error \
                "Keenetic не создал новый маршрут $destination: обнаружено обновление или ошибка существующего маршрута."
        fi

        route_rci="$work/add-$add_index-rci.json"

        broray_routes_router_export_fetch_rci "$route_rci" ||
            broray_routes_router_export_error \
                "Не удалось проверить маршрут $destination через локальный RCI."

        exact_count="$(
            broray_routes_router_export_exact_count \
                "$route_rci" \
                "$destination" \
                "$managed_interface"
        )"

        [ "$exact_count" = "1" ] ||
            broray_routes_router_export_error \
                "Маршрут $destination не подтверждён на $managed_interface ровно один раз."

        printf '%s\t%s\t%s\n' \
            "$network" \
            "$mask" \
            "$key" \
            >>"$created_file"
    done <"$work/to-create.tsv"

    created_count="$(
        wc -l <"$created_file" |
            tr -d ' '
    )"

    [ "$created_count" = "$to_create" ] ||
        broray_routes_router_export_error \
            "Добавлено неверное число маршрутов."

    # Do not commit local ownership until every expected route is present in
    # the full running configuration. This detects partial exports even when
    # ndmc returned a success message for every individual command.
    rci_after="$work/routes-static-after.json"

    broray_routes_router_export_fetch_rci "$rci_after" ||
        broray_routes_router_export_error \
            "Не удалось повторно прочитать настроенные маршруты после экспорта."

    if ! broray_routes_router_export_verify_expected_rci \
        "$plan" \
        "$rci_after"
    then
        jq -n \
            --slurpfile plan "$plan" \
            --slurpfile actual "$rci_after" '
            $plan[0] as $p |
            $actual[0] as $a |
            [
                $p.routes[] as $route |
                ($route.network + "/" + ($route.prefix | tostring)) as $destination |
                ([
                    $a.routes[]? |
                    select(
                        .destination == $destination and
                        .interface == $p.targetInterface and
                        ((.gateway // "0.0.0.0") == "0.0.0.0") and
                        ((.metric // 1000) == $route.metric) and
                        (((.proto // "static") | ascii_downcase) == "static")
                    )
                ] | length) as $matches |
                select($matches != 1) |
                {
                    destination: $destination,
                    expectedInterface: $p.targetInterface,
                    expectedMetric: $route.metric,
                    matchCount: $matches
                }
            ]
        ' >"$catalog/router-export-missing.json" 2>/dev/null || true

        broray_routes_router_export_error \
            "После экспорта в running-config отсутствуют некоторые маршруты. Выполнен откат текущей операции."
    fi

    rm -f "$catalog/router-export-missing.json" 2>/dev/null || true

    broray_routes_router_export_transaction_write \
        "$transaction_file" \
        "routes-added" \
        "$bundle_id" \
        "$(broray_routes_router_export_now)" \
        "$created_count" \
        false \
        false \
        "Маршруты добавлены в рабочую конфигурацию." ||
        broray_routes_router_export_error \
            "Не удалось обновить журнал операции."

    verify_json="$work/routes-added-rci.json"

    broray_routes_router_export_fetch_rci "$verify_json" ||
        broray_routes_router_export_error \
            "Не удалось проверить добавленные маршруты через локальный RCI."

    broray_routes_router_export_verify_expected_rci \
        "$plan" \
        "$verify_json" ||
        broray_routes_router_export_error \
            "Не все маршруты подтверждены на $managed_interface ровно один раз."

    prepared="$work/prepared"
    original="$work/original"
    BRORAY_ROUTES_ROUTER_EXPORT_ORIGINAL_DIR="$original"
    now="$(broray_routes_router_export_now)"

    broray_routes_router_export_prepare_files \
        "$bundle_id" \
        "$fresh_preflight" \
        "$registry" \
        "$bundle_registry" \
        "$state" \
        "$plan" \
        "$prepared" \
        "$now" ||
        broray_routes_router_export_error \
            "Не удалось подготовить локальные реестры."

    broray_routes_router_export_backup_local \
        "$registry" \
        "$bundle_registry" \
        "$state" \
        "$plan" \
        "$result_path" \
        "$original" ||
        broray_routes_router_export_error \
            "Не удалось сохранить локальные реестры перед записью."

    broray_routes_router_export_install_local \
        "$prepared" \
        "$registry" \
        "$bundle_registry" \
        "$state" \
        "$plan" \
        "$result_path" ||
        broray_routes_router_export_error \
            "Не удалось установить локальные реестры."

    BRORAY_ROUTES_ROUTER_EXPORT_LOCAL_COMMITTED=true

    broray_routes_router_export_transaction_write \
        "$transaction_file" \
        "local-committed" \
        "$bundle_id" \
        "$(broray_routes_router_export_now)" \
        "$created_count" \
        false \
        false \
        "Локальные реестры подготовлены." ||
        broray_routes_router_export_error \
            "Не удалось обновить журнал операции."

    save_out="$work/save.out"
    save_err="$work/save.err"

    if ! broray_routes_router_export_ndmc \
        "system configuration save" \
        "$save_out" \
        "$save_err" \
        12
    then
        broray_routes_router_export_error \
            "Не удалось сохранить конфигурацию Keenetic."
    fi

    if grep -Eiq \
        'Io::Netlink error|system failed|invalid command|unknown command|save failed' \
        "$save_out" "$save_err" 2>/dev/null
    then
        broray_routes_router_export_error \
            "Keenetic вернул ошибку при сохранении конфигурации."
    fi

    BRORAY_ROUTES_ROUTER_EXPORT_ROUTER_SAVED=true

    final_json="$work/routes-final-rci.json"

    broray_routes_router_export_fetch_rci "$final_json" ||
        broray_routes_router_export_error \
            "Конфигурация сохранена, но не удалось выполнить итоговую проверку через локальный RCI."

    broray_routes_router_export_verify_expected_rci \
        "$plan" \
        "$final_json" ||
        broray_routes_router_export_error \
            "После сохранения не все маршруты подтверждены на $managed_interface."

    message="Маршруты экспортированы в Keenetic"

    broray_routes_router_export_transaction_write \
        "$transaction_file" \
        "committed" \
        "$bundle_id" \
        "$(broray_routes_router_export_now)" \
        "$created_count" \
        true \
        true \
        "$message" ||
        broray_routes_router_export_error \
            "Экспорт выполнен, но не удалось завершить журнал операции."

    BRORAY_ROUTES_ROUTER_EXPORT_RESULT_PATH="$result_path"

    # The configured-route cache must never outlive a successful mutation.
    rm -f "${BRORAY_ROUTES_CONFIG_CACHE:-}" 2>/dev/null || true

    broray_routes_router_export_lock_release
    rm -rf "$work"
    BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK=""
    trap - EXIT HUP INT TERM

    echo "$message"
    echo "Набор: $bundle_id"
    echo "Управляемый интерфейс: $managed_interface"
    echo "Всего маршрутов: $total"
    echo "Создано BROray: $created_count"
    echo "Уже управлялись BROray: $managed_existing"
    echo "Существовали на $managed_interface вне BROray: $external_existing"
    echo "Совпадения на других интерфейсах: $other_matches"
    echo "Конфигурация сохранена: true"
    echo "Видимая метка rem в CLI-маршруты не записывается."
    echo "Владение зафиксировано во внутреннем реестре BROray."
}

# BROray reliable local RCI reader v2
broray_routes_router_export_fetch_rci()
{
    local output_file raw_file error_file attempt fetch_ok route_filter

    output_file="$1"
    raw_file="${output_file}.raw"
    error_file="${output_file}.error"
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
                "$BRORAY_ROUTES_ROUTER_EXPORT_RCI_URL" \
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
                "$BRORAY_ROUTES_ROUTER_EXPORT_RCI_URL" \
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
                    rm -f "$raw_file" "$error_file"
                    return 0
                fi
            fi
        fi

        attempt=$((attempt + 1))
        [ "$attempt" -gt 3 ] || sleep 1
    done

    cp -f "$raw_file" /tmp/broray-rci-export-last.json 2>/dev/null || true
    cp -f "$error_file" /tmp/broray-rci-export-last.error 2>/dev/null || true
    return 1
}

# BROray parallel static routes by dedicated metric v1
BRORAY_ROUTES_ROUTER_EXPORT_METRIC="${BRORAY_ROUTES_ROUTER_EXPORT_METRIC:-1200}"

broray_routes_router_export_static_conflicts()
{
    local output_file

    output_file="$3"
    printf '%s\n' '[]' >"$output_file"
}

broray_routes_router_export_classify()
{
    local plan_file actual_file registry_file output_file checked_at

    plan_file="$1"
    actual_file="$2"
    registry_file="$3"
    output_file="$4"
    checked_at="${5:-$(broray_routes_router_export_now)}"

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
                select(
                    ((.gateway // "0.0.0.0") == "0.0.0.0") and
                    ((.metric // 1000) == $route.metric)
                )
            ] as $exact_matches |
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
                if ($exact_matches | length) > 1 then
                    "conflict"
                elif ($target_matches | length) > ($exact_matches | length) then
                    "conflict"
                elif ($exact_matches | length) == 1 then
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
                otherInterfaces: ([$other_matches[].interface] | unique),
                ownership: (
                    if $status == "managed_existing" then "broray"
                    elif $status == "external_existing" then "external"
                    else null
                    end
                ),
                conflictReason: (
                    if ($exact_matches | length) > 1 then
                        "На управляемом интерфейсе найдено несколько одинаковых маршрутов BROray."
                    elif ($target_matches | length) > ($exact_matches | length) then
                        "На управляемом интерфейсе существует маршрут той же сети с другими параметрами."
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
            checkedAt: $checked_at,
            routerChanged: false,
            configurationSaved: false,
            summary: {
                total: ($routes | length),
                toCreate: ([$routes[] | select(.status == "create")] | length),
                managedExisting: ([$routes[] | select(.status == "managed_existing")] | length),
                externalExisting: ([$routes[] | select(.status == "external_existing")] | length),
                conflicts: ([$routes[] | select(.status == "conflict")] | length),
                withOtherInterfaceMatches: (
                    [
                        $routes[] |
                        select((.otherInterfaces | length) > 0)
                    ] |
                    length
                )
            },
            routes: $routes
        } |
        .canExport = (
            (.summary.conflicts == 0) and
            (.summary.externalExisting == 0)
        )
    ' >"$output_file"
}

broray_routes_router_export_exact_count()
{
    local actual_file destination target_interface

    actual_file="$1"
    destination="$2"
    target_interface="$3"

    jq -r \
        --arg destination "$destination" \
        --arg target_interface "$target_interface" \
        --argjson metric "$BRORAY_ROUTES_ROUTER_EXPORT_METRIC" '
        [
            .routes[]? |
            select(
                .destination == $destination and
                .interface == $target_interface and
                ((.gateway // "0.0.0.0") == "0.0.0.0") and
                ((.metric // 1000) == $metric) and
                (((.proto // "static") | ascii_downcase) == "static")
            )
        ] |
        length
    ' "$actual_file"
}

broray_routes_router_export_verify_expected_rci()
{
    local plan_file actual_file

    plan_file="$1"
    actual_file="$2"

    jq -n \
        --slurpfile plan "$plan_file" \
        --slurpfile actual "$actual_file" '
        $plan[0] as $p |
        $actual[0] as $a |
        [
            $p.routes[] |
            (.network + "/" + (.prefix | tostring)) as $destination |
            .metric as $metric |
            [
                $a.routes[]? |
                select(
                    .destination == $destination and
                    .interface == $p.targetInterface and
                    ((.gateway // "0.0.0.0") == "0.0.0.0") and
                    ((.metric // 1000) == $metric) and
                    (((.proto // "static") | ascii_downcase) == "static")
                )
            ] |
            length
        ] as $counts |
        all($counts[]; . == 1)
    ' | grep -qx 'true'
}

# BROray hidden parallel route verification v2
broray_routes_router_export_tag_prepared_metric()
{
    local prepared file new

    prepared="$1"

    for file in \
        "$prepared/routes.json" \
        "$prepared/bundle.json" \
        "$prepared/state.json" \
        "$prepared/export-plan.json" \
        "$prepared/router-export-result.json"
    do
        [ -f "$file" ] || return 1
    done

    file="$prepared/routes.json"
    new="$file.new.$$"
    jq '
        .managedMetric = 1200 |
        .verificationMode = "running-config-exact"
    ' "$file" >"$new" || return 1
    mv "$new" "$file" || return 1

    file="$prepared/bundle.json"
    new="$file.new.$$"
    jq '
        .managedMetric = 1200 |
        .verificationMode = "running-config-exact"
    ' "$file" >"$new" || return 1
    mv "$new" "$file" || return 1

    file="$prepared/state.json"
    new="$file.new.$$"
    jq '
        .exportResult.managedMetric = 1200 |
        .exportResult.verificationMode = "running-config-exact"
    ' "$file" >"$new" || return 1
    mv "$new" "$file" || return 1

    file="$prepared/export-plan.json"
    new="$file.new.$$"
    jq '
        .managedMetric = 1200 |
        .applyResult.managedMetric = 1200 |
        .applyResult.verificationMode = "running-config-exact"
    ' "$file" >"$new" || return 1
    mv "$new" "$file" || return 1

    file="$prepared/router-export-result.json"
    new="$file.new.$$"
    jq '
        .managedMetric = 1200 |
        .verificationMode = "running-config-exact"
    ' "$file" >"$new" || return 1
    mv "$new" "$file" || return 1

    jq -e --arg target_interface "$BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE" '
        (.managedInterface == $target_interface) and
        (.managedMetric == 1200) and
        (.verificationMode == "running-config-exact") and
        (all(.routes[];
            (.interface == $target_interface) and
            (.metric == 1200)
        ))
    ' "$prepared/routes.json" >/dev/null || return 1

    jq -e '
        (.bundleId != null) and
        (.managedMetric == 1200) and
        (.verificationMode == "running-config-exact")
    ' "$prepared/bundle.json" >/dev/null || return 1

    jq -e '
        (.status == "installed") and
        (.exportResult.managedMetric == 1200) and
        (.exportResult.verificationMode == "running-config-exact")
    ' "$prepared/state.json" >/dev/null || return 1

    jq -e '
        (.routerApplied == true) and
        (.managedMetric == 1200) and
        (.applyResult.managedMetric == 1200) and
        (.applyResult.verificationMode == "running-config-exact")
    ' "$prepared/export-plan.json" >/dev/null || return 1

    jq -e '
        (.configurationSaved == true) and
        (.managedMetric == 1200) and
        (.verificationMode == "running-config-exact")
    ' "$prepared/router-export-result.json" >/dev/null || return 1

    return 0
}

broray_routes_router_export_run()
{
    local bundle_id bundles config catalog plan preflight registry
    local bundle_registry state route_file result_path transactions
    local managed_interface managed_metric route_comment downloaded_sha available_sha plan_sha
    local route_file_sha expected_route_file_sha lock_result work now
    local interface_out interface_err table_out table_err table_tsv actual_json
    local fresh_preflight conflicts can_export total to_create managed_existing
    local external_existing other_matches created_file tab network mask key destination
    local add_index add_out add_err confirm_out confirm_err
    local rci_before rci_after static_conflicts_file conflict_count
    local prepared original transaction_file created_count save_out save_err
    local message

    bundle_id="${1:-}"

    broray_routes_router_export_bundle_id_valid "$bundle_id" ||
        broray_routes_router_export_error \
            "Некорректный идентификатор набора."

    BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE="$bundle_id"
    export BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE

    bundles="$BRORAY_ROUTES_ROOT/bundles.json"
    config="$BRORAY_ROUTES_ROOT/config.json"
    catalog="$BRORAY_ROUTES_ROOT/catalog/$bundle_id"
    plan="$catalog/export-plan.json"
    preflight="$catalog/router-preflight.json"
    registry="$BRORAY_ROUTES_ROOT/installed/routes.json"
    bundle_registry="$BRORAY_ROUTES_ROOT/installed/bundles/$bundle_id.json"
    state="$BRORAY_ROUTES_ROOT/state/$bundle_id.json"
    route_file="$catalog/keenetic-routes.bat"
    result_path="$catalog/router-export-result.json"
    transactions="$BRORAY_ROUTES_ROOT/transactions"

    for file in \
        "$bundles" \
        "$config" \
        "$plan" \
        "$preflight" \
        "$registry" \
        "$bundle_registry" \
        "$state" \
        "$route_file"
    do
        [ -r "$file" ] ||
            broray_routes_router_export_error \
                "Не найден обязательный файл: $file"
    done

    jq -e --arg bundle_id "$bundle_id" '
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (.bundles | index($bundle_id) != null)
    ' "$bundles" >/dev/null ||
        broray_routes_router_export_error \
            "Набор маршрутов не разрешён: $bundle_id"

    managed_interface="$(jq -r '.managedInterface // empty' "$config")"
    managed_metric="$(jq -r '.managedMetric // empty' "$config")"
    route_comment="$(jq -r '.routeComment // empty' "$config")"

    case "$managed_interface" in
        Proxy[0-9]*) ;;
        *) broray_routes_router_export_error "Некорректный управляемый интерфейс ProxyN." ;;
    esac
    case "${managed_interface#Proxy}" in
        ''|*[!0-9]*) broray_routes_router_export_error "Некорректный управляемый интерфейс ProxyN." ;;
    esac
    BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE="$managed_interface"
    export BRORAY_ROUTES_ROUTER_EXPORT_INTERFACE


    command -v broray_interface_owner_record_valid >/dev/null 2>&1 ||
        broray_routes_router_export_error "Модуль владения ProxyN недоступен."
    broray_interface_owner_record_valid "$managed_interface" ||
        broray_routes_router_export_error "Локальный реестр владельца не подтверждает $managed_interface."
    broray_interface_owner_valid "$managed_interface" ||
        broray_routes_router_export_error "Интерфейс $managed_interface не подтверждён как принадлежащий BROray."

    [ "$managed_metric" = "$BRORAY_ROUTES_ROUTER_EXPORT_METRIC" ] ||
        broray_routes_router_export_error \
            "Некорректная управляемая метрика маршрутов."

    [ "$route_comment" = "BROray" ] ||
        broray_routes_router_export_error \
            "Некорректная метка маршрутов."

    jq -e --arg target_interface "$managed_interface" '
        (.schemaVersion == 1) and
        (.managedInterface == $target_interface) and
        (.managedMetric == 1200) and
        (.routeComment == "BROray") and
        (.ownershipPolicy.adoptExistingRoutes == false) and
        (.ownershipPolicy.modifyExternalRoutes == false) and
        (.ownershipPolicy.deleteExternalRoutes == false) and
        (.ownershipPolicy.touchOtherInterfaces == false) and
        (.ownershipPolicy.deleteOnlyExactManagedMatch == true)
    ' "$config" >/dev/null ||
        broray_routes_router_export_error \
            "Политика безопасности маршрутов повреждена."

    downloaded_sha="$(
        jq -r '.downloadedVersion.contentSha256 // empty' \
            "$state"
    )"
    available_sha="$(
        jq -r '.availableVersion.contentSha256 // empty' \
            "$state"
    )"
    plan_sha="$(jq -r '.contentSha256 // empty' "$plan")"

    [ -n "$downloaded_sha" ] &&
    [ "$downloaded_sha" = "$plan_sha" ] ||
        broray_routes_router_export_error \
            "Локальный план не соответствует загруженной версии."

    jq -e \
        --arg bundle_id "$bundle_id" \
        --arg content_sha "$downloaded_sha" \
        --arg target_interface "$managed_interface" '
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        (.contentSha256 == $content_sha) and
        (.targetInterface == $target_interface) and
        (.managedMetric == 1200) and
        (.routeComment == "BROray") and
        (.routerApplied == false) and
        ((.routes | type) == "array") and
        ((.routes | length) > 0) and
        (all(.routes[];
            (.targetInterface == $target_interface) and
            (.gatewayToken == "0.0.0.0") and
            (.metric == 1200) and
            (.comment == "BROray")
        ))
    ' "$plan" >/dev/null ||
        broray_routes_router_export_error \
            "План экспорта повреждён."

    expected_route_file_sha="$(
        jq -r '.routeFileSha256 // empty' "$plan"
    )"
    route_file_sha="$(
        sha256sum "$route_file" |
            awk '{print $1}'
    )"

    [ -n "$expected_route_file_sha" ] &&
    [ "$route_file_sha" = "$expected_route_file_sha" ] ||
        broray_routes_router_export_error \
            "Файл маршрутов изменился после подготовки."

    if [ -n "${BRORAY_ROUTES_CONFIG_NDMC:-}" ] &&
       [ -x "$BRORAY_ROUTES_CONFIG_NDMC" ]
    then
        BRORAY_ROUTES_ROUTER_EXPORT_NDMC="$BRORAY_ROUTES_CONFIG_NDMC"
    else
        BRORAY_ROUTES_ROUTER_EXPORT_NDMC="$(command -v ndmc 2>/dev/null || true)"
    fi

    [ -n "$BRORAY_ROUTES_ROUTER_EXPORT_NDMC" ] ||
        broray_routes_router_export_error \
            "Команда ndmc недоступна."

    lock_result=0
    broray_routes_router_export_lock_acquire ||
        lock_result=$?

    case "$lock_result" in
        0) ;;
        2)
            broray_routes_router_export_error \
                "Другая операция с маршрутами уже выполняется."
            ;;
        *)
            broray_routes_router_export_error \
                "Не удалось установить блокировку операции."
            ;;
    esac

    work="$BRORAY_ROUTES_ROOT/tmp/router-export-$bundle_id.$$"
    BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK="$work"
    BRORAY_ROUTES_ROUTER_EXPORT_REGISTRY="$registry"
    BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE_REGISTRY="$bundle_registry"
    BRORAY_ROUTES_ROUTER_EXPORT_STATE="$state"
    BRORAY_ROUTES_ROUTER_EXPORT_PLAN="$plan"
    BRORAY_ROUTES_ROUTER_EXPORT_RESULT_PATH="$result_path"

    mkdir -p "$work" "$transactions" || {
        broray_routes_router_export_lock_release
        broray_routes_router_export_error \
            "Не удалось создать рабочий каталог."
    }

    now="$(broray_routes_router_export_now)"
    fresh_preflight="$work/fresh-preflight.json"

    created_file="$work/created.tsv"
    : >"$created_file"
    BRORAY_ROUTES_ROUTER_EXPORT_CREATED_FILE="$created_file"

    trap 'broray_routes_router_export_cleanup' EXIT HUP INT TERM

    interface_out="$work/interface.out"
    interface_err="$work/interface.err"

    if ! broray_routes_router_export_ndmc \
        "show interface $managed_interface" \
        "$interface_out" \
        "$interface_err" \
        8
    then
        broray_routes_router_export_error \
            "Не удалось проверить $managed_interface."
    fi

    grep -Eq "^[[:space:]]*id:[[:space:]]*$managed_interface[[:space:]]*$" \
        "$interface_out" ||
        broray_routes_router_export_error \
            "Ответ Keenetic не относится к $managed_interface."

    grep -Eq '^[[:space:]]*connected:[[:space:]]*yes[[:space:]]*$' \
        "$interface_out" ||
        broray_routes_router_export_error \
            "$managed_interface не подключён."

    grep -Eq '^[[:space:]]*state:[[:space:]]*up[[:space:]]*$' \
        "$interface_out" ||
        broray_routes_router_export_error \
            "$managed_interface не находится в состоянии up."

    rci_before="$work/routes-static-before.json"
    static_conflicts_file="$work/static-other-conflicts.json"

    broray_routes_router_export_fetch_rci "$rci_before" ||
        broray_routes_router_export_error \
            "Не удалось получить статические маршруты через локальный RCI."

    broray_routes_router_export_static_conflicts \
        "$plan" \
        "$rci_before" \
        "$static_conflicts_file" ||
        broray_routes_router_export_error \
            "Не удалось проверить статические маршруты других интерфейсов."

    conflict_count="$(jq -r 'length' "$static_conflicts_file")"

    if [ "$conflict_count" -gt 0 ]; then
        cp -p \
            "$static_conflicts_file" \
            "$catalog/router-export-conflicts.json" \
            2>/dev/null || true

        broray_routes_router_export_error \
            "Обнаружены статические маршруты на других интерфейсах: $conflict_count. Экспорт остановлен до изменений."
    fi

    rm -f "$catalog/router-export-conflicts.json" 2>/dev/null || true

    # Classify against all configured static routes. `show ip route` only
    # exposes active winners and hides valid managed-interface routes with metric 1200
    # when another interface has a lower metric.
    actual_json="$work/actual-config-before.json"
    cp -p "$rci_before" "$actual_json" ||
        broray_routes_router_export_error \
            "Не удалось подготовить снимок настроенных маршрутов."

    broray_routes_router_export_classify \
        "$plan" \
        "$actual_json" \
        "$registry" \
        "$fresh_preflight" \
        "$now" ||
        broray_routes_router_export_error \
            "Не удалось выполнить свежую проверку экспорта."

    conflicts="$(jq -r '.summary.conflicts' "$fresh_preflight")"
    can_export="$(jq -r '.canExport' "$fresh_preflight")"

    [ "$conflicts" = "0" ] &&
    [ "$can_export" = "true" ] ||
        broray_routes_router_export_error \
            "Обнаружены конфликты маршрутов на $managed_interface."

    total="$(jq -r '.summary.total' "$fresh_preflight")"
    to_create="$(jq -r '.summary.toCreate' "$fresh_preflight")"
    managed_existing="$(
        jq -r '.summary.managedExisting' \
            "$fresh_preflight"
    )"
    external_existing="$(
        jq -r '.summary.externalExisting' \
            "$fresh_preflight"
    )"
    other_matches="$(
        jq -r '.summary.withOtherInterfaceMatches' \
            "$fresh_preflight"
    )"

    transaction_file="$transactions/export-$bundle_id-$(broray_routes_router_export_stamp).json"

    broray_routes_router_export_transaction_write \
        "$transaction_file" \
        "checked" \
        "$bundle_id" \
        "$now" \
        0 \
        false \
        false \
        "Свежая проверка завершена." ||
        broray_routes_router_export_error \
            "Не удалось создать журнал операции."

    tab="$(printf '\t')"

    jq -r '
        .routes[] |
        select(.status == "create") |
        [
            .network,
            .mask,
            .key,
            .destination
        ] |
        @tsv
    ' "$fresh_preflight" >"$work/to-create.tsv" ||
        broray_routes_router_export_error \
            "Не удалось подготовить список добавления."

    add_index=0

    while IFS="$tab" read -r network mask key destination
    do
        [ -n "$network" ] || continue

        add_index=$((add_index + 1))
        add_out="$work/add-$add_index.out"
        add_err="$work/add-$add_index.err"

        if ! broray_routes_router_export_ndmc \
            "ip route $network $mask $managed_interface $BRORAY_ROUTES_ROUTER_EXPORT_METRIC" \
            "$add_out" \
            "$add_err" \
            8
        then
            broray_routes_router_export_error \
                "Не удалось добавить маршрут $destination через $managed_interface."
        fi

        if grep -Eiq \
            'Io::Netlink error|system failed|invalid command|unknown command' \
            "$add_out" "$add_err" 2>/dev/null
        then
            broray_routes_router_export_error \
                "Keenetic вернул ошибку при добавлении маршрута $destination."
        fi

        if grep -Fq \
            "Renewed static route: $destination via $managed_interface." \
            "$add_out" "$add_err" 2>/dev/null
        then
            broray_routes_router_export_error \
                "Маршрут $destination уже существовал на $managed_interface с метрикой $BRORAY_ROUTES_ROUTER_EXPORT_METRIC. BROray не присваивает существующий маршрут."
        fi

        grep -Fq \
            "Added static route: $destination via $managed_interface." \
            "$add_out" ||
            broray_routes_router_export_error \
                "Keenetic не подтвердил создание нового маршрута $destination."

        # С этого момента маршрут точно создан текущей операцией.
        # Сначала записываем его в журнал отката, затем проверяем.
        printf '%s\t%s\t%s\n' \
            "$network" \
            "$mask" \
            "$key" \
            >>"$created_file"

        confirm_out="$work/confirm-$add_index.out"
        confirm_err="$work/confirm-$add_index.err"

        if ! broray_routes_router_export_ndmc \
            "ip route $network $mask $managed_interface $BRORAY_ROUTES_ROUTER_EXPORT_METRIC" \
            "$confirm_out" \
            "$confirm_err" \
            8
        then
            broray_routes_router_export_error \
                "Не удалось подтвердить маршрут $destination повторной точной командой."
        fi

        if grep -Eiq \
            'Io::Netlink error|system failed|invalid command|unknown command' \
            "$confirm_out" "$confirm_err" 2>/dev/null
        then
            broray_routes_router_export_error \
                "Keenetic вернул ошибку при подтверждении маршрута $destination."
        fi

        grep -Fq \
            "Renewed static route: $destination via $managed_interface." \
            "$confirm_out" ||
            broray_routes_router_export_error \
                "Маршрут $destination не подтверждён ответом Renewed static route."

        broray_routes_router_export_transaction_write \
            "$transaction_file" \
            "adding" \
            "$bundle_id" \
            "$(broray_routes_router_export_now)" \
            "$add_index" \
            false \
            false \
            "Маршрут $destination создан и подтверждён." ||
            broray_routes_router_export_error \
                "Не удалось обновить журнал после добавления маршрута $destination."
    done <"$work/to-create.tsv"

    created_count="$(
        wc -l <"$created_file" |
            tr -d ' '
    )"

    [ "$created_count" = "$to_create" ] ||
        broray_routes_router_export_error \
            "Добавлено неверное число маршрутов."

    # Do not commit local ownership until every expected route is present in
    # the full running configuration. This detects partial exports even when
    # ndmc returned a success message for every individual command.
    rci_after="$work/routes-static-after.json"

    broray_routes_router_export_fetch_rci "$rci_after" ||
        broray_routes_router_export_error \
            "Не удалось повторно прочитать настроенные маршруты после экспорта."

    if ! broray_routes_router_export_verify_expected_rci \
        "$plan" \
        "$rci_after"
    then
        jq -n \
            --slurpfile plan "$plan" \
            --slurpfile actual "$rci_after" '
            $plan[0] as $p |
            $actual[0] as $a |
            [
                $p.routes[] as $route |
                ($route.network + "/" + ($route.prefix | tostring)) as $destination |
                ([
                    $a.routes[]? |
                    select(
                        .destination == $destination and
                        .interface == $p.targetInterface and
                        ((.gateway // "0.0.0.0") == "0.0.0.0") and
                        ((.metric // 1000) == $route.metric) and
                        (((.proto // "static") | ascii_downcase) == "static")
                    )
                ] | length) as $matches |
                select($matches != 1) |
                {
                    destination: $destination,
                    expectedInterface: $p.targetInterface,
                    expectedMetric: $route.metric,
                    matchCount: $matches
                }
            ]
        ' >"$catalog/router-export-missing.json" 2>/dev/null || true

        broray_routes_router_export_error \
            "После экспорта в running-config отсутствуют некоторые маршруты. Выполнен откат текущей операции."
    fi

    rm -f "$catalog/router-export-missing.json" 2>/dev/null || true

    broray_routes_router_export_transaction_write \
        "$transaction_file" \
        "routes-added" \
        "$bundle_id" \
        "$(broray_routes_router_export_now)" \
        "$created_count" \
        false \
        false \
        "Маршруты добавлены в рабочую конфигурацию." ||
        broray_routes_router_export_error \
            "Не удалось обновить журнал операции."

    prepared="$work/prepared"
    original="$work/original"
    BRORAY_ROUTES_ROUTER_EXPORT_ORIGINAL_DIR="$original"
    now="$(broray_routes_router_export_now)"

    broray_routes_router_export_prepare_files \
        "$bundle_id" \
        "$fresh_preflight" \
        "$registry" \
        "$bundle_registry" \
        "$state" \
        "$plan" \
        "$prepared" \
        "$now" ||
        broray_routes_router_export_error \
            "Не удалось подготовить локальные реестры."

    broray_routes_router_export_tag_prepared_metric \
        "$prepared" ||
        broray_routes_router_export_error \
            "Не удалось зафиксировать метрику и способ проверки в локальных реестрах."

    broray_routes_router_export_backup_local \
        "$registry" \
        "$bundle_registry" \
        "$state" \
        "$plan" \
        "$result_path" \
        "$original" ||
        broray_routes_router_export_error \
            "Не удалось сохранить локальные реестры перед записью."

    broray_routes_router_export_install_local \
        "$prepared" \
        "$registry" \
        "$bundle_registry" \
        "$state" \
        "$plan" \
        "$result_path" ||
        broray_routes_router_export_error \
            "Не удалось установить локальные реестры."

    BRORAY_ROUTES_ROUTER_EXPORT_LOCAL_COMMITTED=true

    broray_routes_router_export_transaction_write \
        "$transaction_file" \
        "local-committed" \
        "$bundle_id" \
        "$(broray_routes_router_export_now)" \
        "$created_count" \
        false \
        false \
        "Локальные реестры подготовлены." ||
        broray_routes_router_export_error \
            "Не удалось обновить журнал операции."

    save_out="$work/save.out"
    save_err="$work/save.err"

    if ! broray_routes_router_export_ndmc \
        "system configuration save" \
        "$save_out" \
        "$save_err" \
        12
    then
        broray_routes_router_export_error \
            "Не удалось сохранить конфигурацию Keenetic."
    fi

    if grep -Eiq \
        'Io::Netlink error|system failed|invalid command|unknown command|save failed' \
        "$save_out" "$save_err" 2>/dev/null
    then
        broray_routes_router_export_error \
            "Keenetic вернул ошибку при сохранении конфигурации."
    fi

    BRORAY_ROUTES_ROUTER_EXPORT_ROUTER_SAVED=true

    message="Маршруты экспортированы в Keenetic"

    broray_routes_router_export_transaction_write \
        "$transaction_file" \
        "committed" \
        "$bundle_id" \
        "$(broray_routes_router_export_now)" \
        "$created_count" \
        true \
        true \
        "$message" ||
        broray_routes_router_export_error \
            "Экспорт выполнен, но не удалось завершить журнал операции."

    BRORAY_ROUTES_ROUTER_EXPORT_RESULT_PATH="$result_path"

    broray_routes_router_export_lock_release
    rm -rf "$work"
    BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK=""
    trap - EXIT HUP INT TERM

    echo "$message"
    echo "Набор: $bundle_id"
    echo "Управляемый интерфейс: $managed_interface"
    echo "Всего маршрутов: $total"
    echo "Создано BROray: $created_count"
    echo "Уже управлялись BROray: $managed_existing"
    echo "Существовали на $managed_interface вне BROray: $external_existing"
    echo "Совпадения на других интерфейсах: $other_matches"
    echo "Конфигурация сохранена: true"
    echo "Проверка создания: ответы ndmc + точная сверка show running-config"
    echo "Метрика BROray: $BRORAY_ROUTES_ROUTER_EXPORT_METRIC"
    echo "Видимая метка rem в CLI-маршруты не записывается."
    echo "Владение зафиксировано во внутреннем реестре BROray."
}

# BROray configured-route source override r10a.
BRORAY_ROUTES_CONFIG_LIBRARY="${BRORAY_ROUTES_CONFIG_LIBRARY:-$BRORAY_ROOT/lib/routes-router-config.sh}"

if [ -r "$BRORAY_ROUTES_CONFIG_LIBRARY" ]; then
    . "$BRORAY_ROUTES_CONFIG_LIBRARY"
fi

broray_routes_router_export_fetch_rci()
{
    local output_file attempt

    output_file="$1"
    attempt=1

    command -v broray_routes_config_fetch >/dev/null 2>&1 || return 1

    while [ "$attempt" -le 3 ]; do
        rm -f "$output_file" "${output_file}.raw" "${output_file}.error"

        if broray_routes_config_fetch "$output_file"; then
            return 0
        fi

        attempt=$((attempt + 1))
        [ "$attempt" -gt 3 ] || sleep 1
    done

    return 1
}
