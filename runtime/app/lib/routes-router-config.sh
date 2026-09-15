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
BRORAY_ROUTES_CONFIG_COMMAND_TIMEOUT="${BRORAY_ROUTES_CONFIG_COMMAND_TIMEOUT:-10}"
BRORAY_ROUTES_CONFIG_CONVERGENCE_ATTEMPTS="${BRORAY_ROUTES_CONFIG_CONVERGENCE_ATTEMPTS:-12}"
BRORAY_ROUTES_CONFIG_NDMC_LOCK="${BRORAY_ROUTES_CONFIG_NDMC_LOCK:-$BRORAY_ROOT/run/routes-router-ndmc.lock}"
BRORAY_ROUTES_WRITE_POLICY_LIBRARY="$BRORAY_ROOT/lib/keenetic-write-policy.sh"

if [ -r "$BRORAY_ROUTES_WRITE_POLICY_LIBRARY" ]; then
    . "$BRORAY_ROUTES_WRITE_POLICY_LIBRARY"
fi

# R14C01 static-route writes share the immutable Keenetic write-policy carrier.
# The source tree is deliberately disabled.  A staged probe may enable the
# helper and bind it to a 64-hex protocol SHA before any target manifest is
# produced.  Callers must not emulate or override that state with environment
# variables.
broray_routes_static_write_policy_check()
{
    command -v broray_keenetic_write_policy_static_routes_profile_check >/dev/null 2>&1 || return 3
    broray_keenetic_write_policy_static_routes_profile_check
}

broray_routes_static_write_policy_sha256()
{
    broray_routes_static_write_policy_check || return $?
    broray_keenetic_write_policy_sha256
}

broray_routes_static_ipv4_valid()
{
    printf '%s\n' "${1:-}" | awk -F. '
        NF != 4 {exit 1}
        {
            for (i=1; i<=4; i++) {
                if ($i !~ /^[0-9]+$/ || $i < 0 || $i > 255) exit 1
            }
        }
    '
}

broray_routes_static_interface_valid()
{
    local suffix

    case "${1:-}" in
        Proxy[0-9]*)
            suffix="${1#Proxy}"
            case "$suffix" in ''|*[!0-9]*) return 1 ;; esac
            ;;
        *) return 1 ;;
    esac
}

# Raw dispatchers may use only these exact read commands.  In particular, a
# caller cannot smuggle a write through a helper whose current production use
# happens to be read-only.
broray_routes_static_read_command_allowed()
{
    local command_text interface

    command_text="${1:-}"
    case "$command_text" in
        'show running-config'|'more startup-config') return 0 ;;
        'show interface '*)
            interface="${command_text#show interface }"
            broray_routes_static_interface_valid "$interface" || return 1
            [ "$command_text" = "show interface $interface" ]
            ;;
        *) return 1 ;;
    esac
}

# The R14C01 static-route writer deliberately has a finite grammar.  Metrics
# other than BROray's bound metric and every extra CLI token are rejected at
# the final dispatch boundary, even if a higher-level caller is defective.
broray_routes_static_write_command_allowed()
{
    local command_text

    command_text="${1:-}"
    [ "$command_text" != 'system configuration save' ] || return 0

    set -- $command_text
    if [ "$#" -eq 6 ] &&
       [ "$1" = ip ] && [ "$2" = route ] &&
       broray_routes_static_ipv4_valid "$3" &&
       broray_routes_static_ipv4_valid "$4" &&
       broray_routes_static_interface_valid "$5" &&
       [ "$6" = 1200 ] &&
       [ "$command_text" = "ip route $3 $4 $5 $6" ]
    then
        return 0
    fi

    if [ "$#" -eq 6 ] &&
       [ "$1" = no ] && [ "$2" = ip ] && [ "$3" = route ] &&
       broray_routes_static_ipv4_valid "$4" &&
       broray_routes_static_ipv4_valid "$5" &&
       broray_routes_static_interface_valid "$6" &&
       [ "$command_text" = "no ip route $4 $5 $6" ]
    then
        return 0
    fi
    return 1
}

broray_routes_static_dispatch_authorize()
{
    local command_text

    command_text="${1:-}"
    broray_routes_static_read_command_allowed "$command_text" && return 0
    broray_routes_static_write_command_allowed "$command_text" || return 126
    broray_routes_static_write_policy_check || return $?
}

broray_routes_config_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_config_epoch()
{
    date '+%s'
}

# All route-state readers share one bounded ndmc lane.  Page summaries and a
# manual verification may otherwise issue overlapping `show running-config`
# commands, which is especially unreliable on slower routers or large configs.
broray_routes_config_ndmc_lock_acquire()
{
    local wait_seconds elapsed max_ticks fast_wait owner

    wait_seconds="$1"
    case "$wait_seconds" in ''|*[!0-9]*) return 1 ;; esac
    [ "$wait_seconds" -gt 0 ] 2>/dev/null || return 1

    mkdir -p "$(dirname "$BRORAY_ROUTES_CONFIG_NDMC_LOCK")" || return 1
    elapsed=0
    fast_wait=false
    if command -v usleep >/dev/null 2>&1; then
        fast_wait=true
        max_ticks=$((wait_seconds * 10))
    else
        max_ticks="$wait_seconds"
    fi

    while [ "$elapsed" -le "$max_ticks" ]; do
        if mkdir "$BRORAY_ROUTES_CONFIG_NDMC_LOCK" 2>/dev/null; then
            if printf '%s\n' "$$" >"$BRORAY_ROUTES_CONFIG_NDMC_LOCK/pid"; then
                return 0
            fi
            rm -rf "$BRORAY_ROUTES_CONFIG_NDMC_LOCK" 2>/dev/null || true
            return 1
        fi

        owner="$(sed -n '1p' "$BRORAY_ROUTES_CONFIG_NDMC_LOCK/pid" 2>/dev/null)"
        case "$owner" in
            ''|*[!0-9]*)
                if [ "$fast_wait" = true ]; then usleep 100000; else sleep 1; fi
                owner="$(sed -n '1p' "$BRORAY_ROUTES_CONFIG_NDMC_LOCK/pid" 2>/dev/null)"
                case "$owner" in
                    ''|*[!0-9]*) rm -rf "$BRORAY_ROUTES_CONFIG_NDMC_LOCK" 2>/dev/null || true ;;
                esac
                ;;
            *)
                kill -0 "$owner" 2>/dev/null ||
                    rm -rf "$BRORAY_ROUTES_CONFIG_NDMC_LOCK" 2>/dev/null || true
                ;;
        esac

        [ "$elapsed" -lt "$max_ticks" ] || return 125
        if [ "$fast_wait" = true ]; then usleep 100000; else sleep 1; fi
        elapsed=$((elapsed + 1))
    done
    return 125
}

broray_routes_config_ndmc_lock_release()
{
    local owner

    owner="$(sed -n '1p' "$BRORAY_ROUTES_CONFIG_NDMC_LOCK/pid" 2>/dev/null)"
    [ "$owner" = "$$" ] || return 0
    rm -rf "$BRORAY_ROUTES_CONFIG_NDMC_LOCK" 2>/dev/null || true
}

broray_routes_config_ndmc_capture()
{
    local ndmc_bin command_text output error limit pid elapsed rc max_ticks fast_wait wait_limit lock_rc

    ndmc_bin="$1"
    command_text="$2"
    output="$3"
    error="$4"
    limit="${5:-$BRORAY_ROUTES_CONFIG_COMMAND_TIMEOUT}"

    broray_routes_static_dispatch_authorize "$command_text" || return $?

    case "$limit" in ''|*[!0-9]*) return 1 ;; esac
    [ "$limit" -gt 0 ] 2>/dev/null || return 1
    : >"$output" || return 1
    : >"$error" || return 1

    wait_limit="${BRORAY_ROUTES_CONFIG_NDMC_LOCK_WAIT:-$((limit + 5))}"
    lock_rc=0
    broray_routes_config_ndmc_lock_acquire "$wait_limit" || lock_rc=$?
    if [ "$lock_rc" -ne 0 ]; then
        printf 'ROUTES_CONFIG_NDMC_LOCK_FAILED rc=%s\n' "$lock_rc" >"$error"
        return "$lock_rc"
    fi

    "$ndmc_bin" -c "$command_text" >"$output" 2>"$error" &
    pid=$!
    elapsed=0
    fast_wait=false
    if command -v usleep >/dev/null 2>&1; then
        fast_wait=true
        max_ticks=$((limit * 10))
    else
        max_ticks="$limit"
    fi
    while kill -0 "$pid" 2>/dev/null; do
        if [ "$elapsed" -ge "$max_ticks" ]; then
            kill "$pid" 2>/dev/null || true
            sleep 1
            kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true
            wait "$pid" 2>/dev/null || true
            printf '%s\n' 'ROUTES_CONFIG_NDMC_TIMEOUT' >>"$error"
            broray_routes_config_ndmc_lock_release
            return 124
        fi
        if [ "$fast_wait" = true ]; then
            usleep 100000
        else
            sleep 1
        fi
        elapsed=$((elapsed + 1))
    done

    if wait "$pid" 2>/dev/null; then rc=0; else rc=$?; fi
    broray_routes_config_ndmc_lock_release
    return "$rc"
}

broray_routes_config_cache_fresh()
{
    local cache now fetched ttl

    cache="$1"
    [ -s "$cache" ] || return 1

    jq -e '
        (.schemaVersion == 1) and
        (.source == "running-config") and
        ((.routes | type) == "array") and
        ((.serializationComplete | type) == "boolean")
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
    local output raw tsv err fetched_at fetched_epoch ndmc_bin rc

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
            printf '%s\n' 'ROUTES_CONFIG_NDMC_UNAVAILABLE' >&2
            rm -f "$output" "$raw" "$tsv" "$err"
            return 1
        }

        if broray_routes_config_ndmc_capture \
            "$ndmc_bin" "show running-config" "$raw" "$err"
        then
            :
        else
            rc=$?
            [ ! -s "$err" ] || tail -n 20 "$err" >&2
            printf 'ROUTES_CONFIG_RUNNING_CONFIG_READ_FAILED rc=%s\n' "$rc" >&2
            rm -f "$output" "$raw" "$tsv" "$err"
            return "$rc"
        fi
    fi

    [ -s "$raw" ] || {
        printf '%s\n' 'ROUTES_CONFIG_RUNNING_CONFIG_EMPTY' >&2
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

        function emit_unknown(reason, line_number) {
            print "false\t" reason "\t" line_number "\t-\t-\t-\t-\t-\t-\tfalse\tfalse\tfalse\tunknown"
        }

        {
            line = $0
            sub(/^[[:space:]]+/, "", line)
            sub(/[[:space:]]+$/, "", line)

            count = split(line, field, /[[:space:]]+/)
            if (field[1] != "ip" || field[2] != "route") {
                next
            }

            if (count < 4) {
                emit_unknown("insufficient-fields", NR)
                next
            }

            network = field[3]

            if (!is_ipv4(network)) {
                emit_unknown("unsupported-network-token", NR)
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
                serialization_form = "host-interface"
            } else {
                mask = field[4]
                prefix = mask_prefix(mask)

                if (prefix < 0 || count < 5) {
                    emit_unknown("invalid-mask-or-missing-interface", NR)
                    next
                }

                if (is_ipv4(field[5])) {
                    gateway = field[5]
                    if (count < 6) {
                        emit_unknown("missing-interface-after-gateway", NR)
                        next
                    }
                    interface_name = field[6]
                    option_start = 7
                    serialization_form = "network-gateway-interface"
                } else {
                    interface_name = field[5]
                    option_start = 6
                    serialization_form = "network-interface"
                }
            }

            if (interface_name == "") {
                emit_unknown("empty-interface", NR)
                next
            }

            metric = "-"
            metric_explicit = "false"
            automatic = "false"
            exclusive = "false"
            known = "true"
            reason = "-"

            for (idx = option_start; idx <= count; idx += 1) {
                token = field[idx]

                if (token == "auto") {
                    automatic = "true"
                } else if (token == "exclusive") {
                    exclusive = "true"
                } else if (token ~ /^[0-9]+$/) {
                    if (metric_explicit == "true") {
                        known = "false"
                        reason = "multiple-numeric-options"
                    } else {
                        metric = token + 0
                        metric_explicit = "true"
                    }
                } else {
                    # `auto` and `exclusive` are exact running-config tokens
                    # already captured by BROray physical route evidence.
                    # Every other option remains deliberately fail-closed.
                    known = "false"
                    reason = "unsupported-option-token"
                }
            }

            print known "\t" reason "\t" NR "\t" network "\t" mask "\t" prefix "\t" interface_name "\t" gateway "\t" metric "\t" metric_explicit "\t" automatic "\t" exclusive "\t" serialization_form
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
            select(length == 13) |
            if .[0] == "true" then
                {
                    network: .[3],
                    mask: .[4],
                    prefix: (.[5] | tonumber),
                    destination: (.[3] + "/" + .[5]),
                    interface: .[6],
                    gateway: .[7],
                    metric: (if .[8] == "-" then null else (.[8] | tonumber) end),
                    proto: "static",
                    automatic: (.[10] == "true"),
                    exclusive: (.[11] == "true"),
                    identity: (.[3] + "/" + .[5] + "|" + .[6] + "|" + .[7]),
                    serialization: {
                        known: true,
                        form: .[12],
                        lineNumber: (.[2] | tonumber),
                        metricExplicit: (.[9] == "true")
                    }
                }
            else
                {
                    network: null,
                    mask: null,
                    prefix: null,
                    destination: null,
                    interface: null,
                    gateway: null,
                    metric: null,
                    proto: "static",
                    automatic: null,
                    exclusive: null,
                    identity: null,
                    serialization: {
                        known: false,
                        form: "unknown",
                        lineNumber: (.[2] | tonumber),
                        metricExplicit: false,
                        reason: .[1]
                    }
                }
            end
        ] as $routes |
        {
            schemaVersion: 1,
            source: "running-config",
            fetchedAt: $fetched_at,
            fetchedEpoch: $fetched_epoch,
            routeLineCount: ($routes | length),
            unknownRouteCount: ([$routes[] | select(.serialization.known == false)] | length),
            serializationComplete: (all($routes[]; .serialization.known == true)),
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
        ((.routeLineCount | type) == "number") and
        ((.unknownRouteCount | type) == "number") and
        ((.serializationComplete | type) == "boolean") and
        (all(.routes[];
            (.proto == "static") and
            ((.serialization.known | type) == "boolean") and
            (
                if .serialization.known then
                    ((.network | type) == "string") and
                    ((.mask | type) == "string") and
                    ((.prefix | type) == "number") and
                    ((.destination | type) == "string") and
                    ((.interface | type) == "string") and
                    ((.gateway | type) == "string") and
                    ((.metric == null) or ((.metric | type) == "number")) and
                    ((.automatic | type) == "boolean") and
                    ((.exclusive | type) == "boolean") and
                    ((.identity | type) == "string")
                else
                    (.identity == null) and
                    ((.serialization.reason | type) == "string")
                end
            )
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
                ((.routes | type) == "array") and
                ((.serializationComplete | type) == "boolean")
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
            ((.routes | type) == "array") and
            ((.serializationComplete | type) == "boolean")
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
        ((.routes | type) == "array") and
        ((.serializationComplete | type) == "boolean")
    ' "$output" >/dev/null 2>&1
}

# Route identity follows the deletion command's observable object boundary:
# destination + interface + gateway.  Metric is an attribute and is never an
# identity discriminator.  Attribute equality is checked separately where an
# operation needs byte-reconstructible rollback.
broray_routes_config_identity_count()
{
    local file network prefix interface gateway

    file="$1"
    network="$2"
    prefix="$3"
    interface="$4"
    gateway="${5:-0.0.0.0}"

    jq -r \
        --arg destination "$network/$prefix" \
        --arg interface "$interface" \
        --arg gateway "$gateway" '
        [
            .routes[]? |
            select(
                .serialization.known == true and
                .destination == $destination and
                .interface == $interface and
                .gateway == $gateway and
                .proto == "static"
            )
        ] | length
    ' "$file"
}

broray_routes_config_attribute_count()
{
    local file network prefix interface gateway metric

    file="$1"
    network="$2"
    prefix="$3"
    interface="$4"
    gateway="${5:-0.0.0.0}"
    metric="$6"

    jq -r \
        --arg destination "$network/$prefix" \
        --arg interface "$interface" \
        --arg gateway "$gateway" \
        --argjson metric "$metric" '
        [
            .routes[]? |
            select(
                .serialization.known == true and
                .serialization.metricExplicit == true and
                .destination == $destination and
                .interface == $interface and
                .gateway == $gateway and
                .proto == "static" and
                .metric == $metric
            )
        ] | length
    ' "$file"
}

broray_routes_config_serialization_complete()
{
    jq -e '
        (.source == "running-config") and
        (.serializationComplete == true) and
        (.unknownRouteCount == 0) and
        (all(.routes[]; .serialization.known == true))
    ' "$1" >/dev/null 2>&1
}

broray_routes_config_wait_attribute_state()
{
    local expected network prefix interface gateway metric output_base attempts
    local attempt snapshot identity_count attribute_count

    expected="$1"
    network="$2"
    prefix="$3"
    interface="$4"
    gateway="$5"
    metric="$6"
    output_base="$7"
    attempts="${8:-$BRORAY_ROUTES_CONFIG_CONVERGENCE_ATTEMPTS}"

    case "$expected" in present|absent) ;; *) return 1 ;; esac
    case "$attempts" in ''|*[!0-9]*) return 1 ;; esac
    [ "$attempts" -gt 0 ] 2>/dev/null || return 1

    attempt=1
    while [ "$attempt" -le "$attempts" ]; do
        snapshot="$output_base.$attempt.json"
        if broray_routes_config_fetch "$snapshot" &&
           broray_routes_config_serialization_complete "$snapshot"
        then
            identity_count="$(broray_routes_config_identity_count \
                "$snapshot" "$network" "$prefix" "$interface" "$gateway")" ||
                identity_count=""
            attribute_count="$(broray_routes_config_attribute_count \
                "$snapshot" "$network" "$prefix" "$interface" "$gateway" "$metric")" ||
                attribute_count=""

            if [ "$expected" = present ] &&
               [ "$identity_count" = 1 ] && [ "$attribute_count" = 1 ]; then
                cp -p "$snapshot" "$output_base.final.json" 2>/dev/null || true
                return 0
            fi
            if [ "$expected" = absent ] && [ "$identity_count" = 0 ]; then
                cp -p "$snapshot" "$output_base.final.json" 2>/dev/null || true
                return 0
            fi
        fi

        attempt=$((attempt + 1))
        [ "$attempt" -gt "$attempts" ] || sleep 1
    done
    return 1
}

# `system configuration save` is asynchronous on the target.  Success is not
# inferred from stdout.  R14C01 requires two consecutive, byte-identical pairs of
# `show running-config` and `more startup-config` within a bounded window.
broray_routes_config_save_converged()
{
    local work ndmc_bin attempts out err attempt running startup digest previous stable

    work="$1"
    attempts="${2:-$BRORAY_ROUTES_CONFIG_CONVERGENCE_ATTEMPTS}"
    case "$attempts" in ''|*[!0-9]*) return 1 ;; esac
    [ "$attempts" -gt 1 ] 2>/dev/null || return 1
    broray_routes_static_write_policy_check || return $?

    case "$BRORAY_ROUTES_CONFIG_NDMC" in
        */*) ndmc_bin="$BRORAY_ROUTES_CONFIG_NDMC" ;;
        *) ndmc_bin="$(command -v "$BRORAY_ROUTES_CONFIG_NDMC" 2>/dev/null || true)" ;;
    esac
    [ -n "$ndmc_bin" ] && [ -x "$ndmc_bin" ] || return 127
    mkdir -p "$work" || return 1

    out="$work/save.out"
    err="$work/save.err"
    broray_routes_config_ndmc_capture \
        "$ndmc_bin" "system configuration save" "$out" "$err" 15 || return $?

    attempt=1
    previous=""
    stable=0
    while [ "$attempt" -le "$attempts" ]; do
        running="$work/running.$attempt.txt"
        startup="$work/startup.$attempt.txt"
        if broray_routes_config_ndmc_capture \
               "$ndmc_bin" "show running-config" "$running" "$work/running.$attempt.err" &&
           broray_routes_config_ndmc_capture \
               "$ndmc_bin" "more startup-config" "$startup" "$work/startup.$attempt.err" &&
           [ -s "$running" ] && [ -s "$startup" ] && cmp -s "$running" "$startup"
        then
            digest="$(sha256sum "$running" | awk 'NR == 1 {print $1}')"
            if [ "$digest" = "$previous" ]; then
                stable=$((stable + 1))
            else
                previous="$digest"
                stable=1
            fi
            if [ "$stable" -ge 2 ]; then
                printf '%s\n' "$digest" >"$work/converged.sha256"
                return 0
            fi
        else
            previous=""
            stable=0
        fi
        attempt=$((attempt + 1))
        [ "$attempt" -gt "$attempts" ] || sleep 1
    done
    return 1
}
