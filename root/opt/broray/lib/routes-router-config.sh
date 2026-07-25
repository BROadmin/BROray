#!/opt/bin/ash

# BROray configured static routes reader v2.
# `show ip route` contains only active routes. BROray ownership and drift must
# be checked against `show running-config`, which contains all configured
# static routes, including routes hidden by a lower metric on another interface.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_CONFIG_CACHE="${BRORAY_ROUTES_CONFIG_CACHE:-$BRORAY_ROOT/run/routes-router-config-cache.json}"
BRORAY_ROUTES_CONFIG_LOCK="${BRORAY_ROUTES_CONFIG_LOCK:-$BRORAY_ROOT/run/routes-router-config.lock}"
BRORAY_ROUTES_CONFIG_TTL="${BRORAY_ROUTES_CONFIG_TTL:-5}"
BRORAY_ROUTES_CONFIG_NDMC="${BRORAY_ROUTES_CONFIG_NDMC:-ndmc}"

broray_routes_config_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_config_epoch()
{
    date '+%s'
}

broray_routes_config_cache_fresh()
{
    local cache now fetched ttl

    cache="$1"
    [ -s "$cache" ] || return 1

    jq -e '
        (.schemaVersion == 1) and
        (.source == "running-config") and
        ((.routes | type) == "array")
    ' "$cache" >/dev/null 2>&1 || return 1

    now="$(broray_routes_config_epoch)"
    fetched="$(jq -r '.fetchedEpoch // 0' "$cache" 2>/dev/null)"
    ttl="$BRORAY_ROUTES_CONFIG_TTL"

    case "$now:$fetched:$ttl" in
        *[!0-9:]*) return 1 ;;
    esac

    [ $((now - fetched)) -le "$ttl" ] 2>/dev/null
}

broray_routes_config_fetch()
{
    local output raw tsv err fetched_at fetched_epoch ndmc_bin

    output="$1"
    raw="$output.raw"
    tsv="$output.tsv"
    err="$output.err"

    rm -f "$output" "$raw" "$tsv" "$err"

    if [ -n "${BRORAY_ROUTES_CONFIG_FIXTURE:-}" ]; then
        cp "$BRORAY_ROUTES_CONFIG_FIXTURE" "$raw" 2>"$err" || {
            rm -f "$output" "$raw" "$tsv" "$err"
            return 1
        }
    else
        case "$BRORAY_ROUTES_CONFIG_NDMC" in
            */*) ndmc_bin="$BRORAY_ROUTES_CONFIG_NDMC" ;;
            *) ndmc_bin="$(command -v "$BRORAY_ROUTES_CONFIG_NDMC" 2>/dev/null || true)" ;;
        esac

        [ -n "$ndmc_bin" ] && [ -x "$ndmc_bin" ] || {
            rm -f "$output" "$raw" "$tsv" "$err"
            return 1
        }

        "$ndmc_bin" -c "show running-config" >"$raw" 2>"$err" || {
            rm -f "$output" "$raw" "$tsv" "$err"
            return 1
        }
    fi

    [ -s "$raw" ] || {
        rm -f "$output" "$raw" "$tsv" "$err"
        return 1
    }

    awk '
        function is_ipv4(value, parts, count, idx) {
            count = split(value, parts, ".")
            if (count != 4) {
                return 0
            }

            for (idx = 1; idx <= 4; idx += 1) {
                if (parts[idx] !~ /^[0-9]+$/ ||
                    parts[idx] < 0 || parts[idx] > 255) {
                    return 0
                }
            }

            return 1
        }

        function mask_octet_bits(value) {
            if (value == 255) return 8
            if (value == 254) return 7
            if (value == 252) return 6
            if (value == 248) return 5
            if (value == 240) return 4
            if (value == 224) return 3
            if (value == 192) return 2
            if (value == 128) return 1
            if (value == 0) return 0
            return -1
        }

        function mask_prefix(mask, parts, count, idx, bits, prefix, zero_seen) {
            count = split(mask, parts, ".")
            if (count != 4) {
                return -1
            }

            prefix = 0
            zero_seen = 0

            for (idx = 1; idx <= 4; idx += 1) {
                bits = mask_octet_bits(parts[idx] + 0)
                if (bits < 0) {
                    return -1
                }

                if (zero_seen && bits != 0) {
                    return -1
                }

                if (bits < 8) {
                    zero_seen = 1
                }

                prefix += bits
            }

            return prefix
        }

        {
            line = $0
            sub(/^[[:space:]]+/, "", line)
            sub(/[[:space:]]+$/, "", line)

            count = split(line, field, /[[:space:]]+/)
            if (count < 4 || field[1] != "ip" || field[2] != "route") {
                next
            }

            network = field[3]

            if (!is_ipv4(network)) {
                next
            }

            gateway = "0.0.0.0"
            interface_name = ""
            option_start = 0

            # Keenetic normalizes IPv4 /32 routes to the host form:
            #   ip route 1.2.3.4 Proxy0 1200
            # even when they were created with mask 255.255.255.255.
            # Network routes keep the explicit mask form.
            if (!is_ipv4(field[4])) {
                mask = "255.255.255.255"
                prefix = 32
                interface_name = field[4]
                option_start = 5
            } else {
                mask = field[4]
                prefix = mask_prefix(mask)

                if (prefix < 0 || count < 5) {
                    next
                }

                if (is_ipv4(field[5])) {
                    gateway = field[5]
                    if (count < 6) {
                        next
                    }
                    interface_name = field[6]
                    option_start = 7
                } else {
                    interface_name = field[5]
                    option_start = 6
                }
            }

            if (interface_name == "") {
                next
            }

            metric = 1000
            automatic = "false"
            exclusive = "false"

            for (idx = option_start; idx <= count; idx += 1) {
                token = field[idx]

                if (token == "auto") {
                    automatic = "true"
                } else if (token == "exclusive") {
                    exclusive = "true"
                } else if (token ~ /^[0-9]+$/) {
                    metric = token + 0
                }
            }

            print network "\t" mask "\t" prefix "\t" interface_name "\t" gateway "\t" metric "\t" automatic "\t" exclusive
        }
    ' "$raw" >"$tsv" || {
        rm -f "$output" "$raw" "$tsv" "$err"
        return 1
    }

    fetched_at="$(broray_routes_config_now)"
    fetched_epoch="$(broray_routes_config_epoch)"

    jq -Rn \
        --arg fetched_at "$fetched_at" \
        --argjson fetched_epoch "$fetched_epoch" '
        [
            inputs |
            split("\t") |
            select(length == 8) |
            {
                network: .[0],
                mask: .[1],
                prefix: (.[2] | tonumber),
                destination: (.[0] + "/" + .[2]),
                interface: .[3],
                gateway: .[4],
                metric: (.[5] | tonumber),
                proto: "static",
                automatic: (.[6] == "true"),
                exclusive: (.[7] == "true")
            }
        ] as $routes |
        {
            schemaVersion: 1,
            source: "running-config",
            fetchedAt: $fetched_at,
            fetchedEpoch: $fetched_epoch,
            routes: $routes
        }
    ' <"$tsv" >"$output" 2>"$err" || {
        rm -f "$output" "$raw" "$tsv" "$err"
        return 1
    }

    jq -e '
        (.schemaVersion == 1) and
        (.source == "running-config") and
        ((.fetchedAt | type) == "string") and
        ((.fetchedEpoch | type) == "number") and
        ((.routes | type) == "array") and
        (all(.routes[];
            ((.network | type) == "string") and
            ((.mask | type) == "string") and
            ((.prefix | type) == "number") and
            ((.destination | type) == "string") and
            ((.interface | type) == "string") and
            ((.metric | type) == "number") and
            (.proto == "static")
        ))
    ' "$output" >/dev/null 2>&1 || {
        rm -f "$output" "$raw" "$tsv" "$err"
        return 1
    }

    rm -f "$raw" "$tsv" "$err"
    return 0
}

broray_routes_config_get_cache()
{
    local cache_dir tmp owner_pid attempt

    cache_dir="$(dirname "$BRORAY_ROUTES_CONFIG_CACHE")"
    mkdir -p "$cache_dir" || return 1

    if broray_routes_config_cache_fresh "$BRORAY_ROUTES_CONFIG_CACHE"; then
        return 0
    fi

    if [ -d "$BRORAY_ROUTES_CONFIG_LOCK" ]; then
        owner_pid="$(sed -n '1p' "$BRORAY_ROUTES_CONFIG_LOCK/pid" 2>/dev/null)"

        case "$owner_pid" in
            ''|*[!0-9]*)
                rm -rf "$BRORAY_ROUTES_CONFIG_LOCK" 2>/dev/null || true
                ;;
            *)
                if ! kill -0 "$owner_pid" 2>/dev/null; then
                    rm -rf "$BRORAY_ROUTES_CONFIG_LOCK" 2>/dev/null || true
                fi
                ;;
        esac
    fi

    if mkdir "$BRORAY_ROUTES_CONFIG_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_ROUTES_CONFIG_LOCK/pid"
        tmp="$BRORAY_ROUTES_CONFIG_CACHE.new.$$"

        if broray_routes_config_fetch "$tmp"; then
            mv -f "$tmp" "$BRORAY_ROUTES_CONFIG_CACHE" || {
                rm -f "$tmp"
                rm -rf "$BRORAY_ROUTES_CONFIG_LOCK" 2>/dev/null || true
                return 1
            }

            chmod 600 "$BRORAY_ROUTES_CONFIG_CACHE" 2>/dev/null || true
            rm -rf "$BRORAY_ROUTES_CONFIG_LOCK" 2>/dev/null || true
            return 0
        fi

        rm -f "$tmp"
        rm -rf "$BRORAY_ROUTES_CONFIG_LOCK" 2>/dev/null || true

        [ -s "$BRORAY_ROUTES_CONFIG_CACHE" ] &&
            jq -e '
                (.source == "running-config") and
                ((.routes | type) == "array")
            ' "$BRORAY_ROUTES_CONFIG_CACHE" >/dev/null 2>&1
        return $?
    fi

    attempt=0
    while [ "$attempt" -lt 4 ]; do
        sleep 1
        if broray_routes_config_cache_fresh "$BRORAY_ROUTES_CONFIG_CACHE"; then
            return 0
        fi
        attempt=$((attempt + 1))
    done

    [ -s "$BRORAY_ROUTES_CONFIG_CACHE" ] &&
        jq -e '
            (.source == "running-config") and
            ((.routes | type) == "array")
        ' "$BRORAY_ROUTES_CONFIG_CACHE" >/dev/null 2>&1
}

broray_routes_config_snapshot()
{
    local output

    output="$1"

    broray_routes_config_get_cache || return 1
    cp -p "$BRORAY_ROUTES_CONFIG_CACHE" "$output" || return 1

    jq -e '
        (.schemaVersion == 1) and
        (.source == "running-config") and
        ((.routes | type) == "array")
    ' "$output" >/dev/null 2>&1
}
