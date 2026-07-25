#!/opt/bin/ash

# BROray route presence v2.
# Presence is evaluated against `show running-config`, not `show ip route`.
# The latter contains only currently active routes and hides configured routes
# when another interface has a lower metric.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_ROUTES_CONFIG_LIBRARY="${BRORAY_ROUTES_CONFIG_LIBRARY:-$BRORAY_ROOT/lib/routes-router-config.sh}"

[ -r "$BRORAY_ROUTES_CONFIG_LIBRARY" ] || return 1 2>/dev/null || exit 1
. "$BRORAY_ROUTES_CONFIG_LIBRARY"

broray_routes_presence_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_presence_bundle()
{
    local bundle_id output bundle_file expected_count snapshot

    bundle_id="$1"
    output="$2"
    bundle_file="$BRORAY_ROUTES_ROOT/installed/bundles/$bundle_id.json"
    snapshot="$output.config.$$"

    rm -f "$output" "$snapshot"

    case "$bundle_id" in
        ''|*[!a-z0-9_-]*) return 1 ;;
    esac

    [ -r "$bundle_file" ] || return 1

    jq -e \
        --arg bundle_id "$bundle_id" '
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        ((.routeKeys | type) == "array")
    ' "$bundle_file" >/dev/null 2>&1 || return 1

    expected_count="$(jq -r '.routeKeys | length' "$bundle_file")"

    case "$expected_count" in
        ''|*[!0-9]*) return 1 ;;
    esac

    if [ "$expected_count" -eq 0 ]; then
        jq -n \
            --arg checked_at "$(broray_routes_presence_now)" '
            {
                available: true,
                registered: false,
                source: "running-config",
                checkedAt: $checked_at,
                cacheAgeSeconds: 0,
                expectedRouteCount: 0,
                presentRouteCount: 0,
                missingRouteCount: 0,
                duplicateRouteCount: 0,
                complete: false,
                actualInstalled: false,
                drift: false,
                status: "not_registered",
                missingRoutes: []
            }
        ' >"$output"
        return $?
    fi

    if ! broray_routes_config_snapshot "$snapshot"; then
        rm -f "$snapshot"

        jq -n \
            --arg checked_at "$(broray_routes_presence_now)" \
            --argjson expected "$expected_count" '
            {
                available: false,
                registered: true,
                source: "running-config",
                checkedAt: $checked_at,
                cacheAgeSeconds: null,
                expectedRouteCount: $expected,
                presentRouteCount: null,
                missingRouteCount: null,
                duplicateRouteCount: null,
                complete: null,
                actualInstalled: null,
                drift: null,
                status: "unavailable",
                missingRoutes: []
            }
        ' >"$output"
        return 0
    fi

    jq -n \
        --slurpfile bundle "$bundle_file" \
        --slurpfile actual "$snapshot" '
        def metric_number:
            if . == null then 1000
            elif (type == "number") then .
            elif (type == "string") then (tonumber? // 1000)
            else 1000
            end;

        $bundle[0] as $b |
        $actual[0] as $a |
        ($b.targetInterface // "") as $bundle_interface |
        [
            $b.routeKeys[] as $key |
            ($key | split("|")) as $parts |
            ($parts[1] // "") as $destination |
            ($parts[2] // $bundle_interface) as $interface |
            (
                ($parts[4] // "metric:1200") |
                split(":")[1] |
                tonumber? // 1200
            ) as $metric |
            ([
                $a.routes[]? |
                select(
                    (.destination // "") == $destination and
                    (.interface // "") == $interface and
                    ((.gateway // "0.0.0.0") == "0.0.0.0") and
                    ((.metric | metric_number) == $metric) and
                    (((.proto // "static") | ascii_downcase) == "static")
                )
            ] | length) as $matches |
            {
                key: $key,
                destination: $destination,
                interface: $interface,
                metric: $metric,
                matchCount: $matches,
                present: ($matches == 1)
            }
        ] as $checks |
        ([$checks[] | select(.present)] | length) as $present |
        ([$checks[] | select(.present | not)] | length) as $missing |
        ([$checks[] | select(.matchCount > 1)] | length) as $duplicates |
        ($checks | length) as $expected |
        ($missing == 0 and $duplicates == 0 and $expected > 0) as $complete |
        {
            available: true,
            registered: true,
            source: "running-config",
            checkedAt: ($a.fetchedAt // null),
            cacheAgeSeconds: (
                ((now | floor) - ($a.fetchedEpoch // (now | floor)))
            ),
            expectedRouteCount: $expected,
            presentRouteCount: $present,
            missingRouteCount: $missing,
            duplicateRouteCount: $duplicates,
            complete: $complete,
            actualInstalled: (
                if $complete then true
                elif $present == 0 then false
                else null
                end
            ),
            drift: ($complete | not),
            status: (
                if $complete then "complete"
                elif $present == 0 then "absent"
                else "partial"
                end
            ),
            missingRoutes: (
                [
                    $checks[] |
                    select(.present | not) |
                    {
                        key,
                        destination,
                        interface,
                        metric,
                        matchCount
                    }
                ] |
                .[0:50]
            )
        }
    ' >"$output"
    rc=$?

    rm -f "$snapshot"
    return "$rc"
}
