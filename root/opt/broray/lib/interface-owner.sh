#!/opt/bin/ash

# BROray managed proxy ownership and safe ProxyN selection.
# A ProxyN interface is touched only when both the local ownership record and
# its live Keenetic signature match BROray.

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_INTERFACE_OWNER_FILE="${BRORAY_INTERFACE_OWNER_FILE:-$BRORAY_BASE/config/interface.json}"
BRORAY_INTERFACE_FALLBACK="${BRORAY_INTERFACE_FALLBACK:-Proxy0}"
BRORAY_XRAY_CONFIG="${BRORAY_XRAY_CONFIG:-$BRORAY_BASE/config/config.json}"
BRORAY_ROUTES_CONFIG_FILE="${BRORAY_ROUTES_CONFIG_FILE:-$BRORAY_BASE/routes/config.json}"
BRORAY_ROUTES_MANIFEST_DIR="${BRORAY_ROUTES_MANIFEST_DIR:-$BRORAY_BASE/routes/manifests}"
BRORAY_ROUTES_SHARE_MANIFEST_DIR="${BRORAY_ROUTES_SHARE_MANIFEST_DIR:-$BRORAY_BASE/share/routes/manifests}"
BRORAY_INTERFACE_NDMC="${BRORAY_INTERFACE_NDMC:-${BRORAY_ROUTES_CONFIG_NDMC:-ndmc}}"
BRORAY_INTERFACE_MAX_INDEX="${BRORAY_INTERFACE_MAX_INDEX:-255}"

if [ -z "${BRORAY_PROXY_HOST:-}" ] && [ -r "$BRORAY_XRAY_CONFIG" ]; then
    BRORAY_PROXY_HOST="$(
        jq -r '[.inbounds[]? | select(.protocol == "socks") | .listen // empty][0] // empty' \
            "$BRORAY_XRAY_CONFIG" 2>/dev/null
    )"
fi

if [ -z "${BRORAY_PROXY_PORT:-}" ] && [ -r "$BRORAY_XRAY_CONFIG" ]; then
    BRORAY_PROXY_PORT="$(
        jq -r '[.inbounds[]? | select(.protocol == "socks") | .port // empty][0] // empty' \
            "$BRORAY_XRAY_CONFIG" 2>/dev/null
    )"
fi

BRORAY_PROXY_HOST="${BRORAY_PROXY_HOST:-192.168.1.1}"
BRORAY_PROXY_PORT="${BRORAY_PROXY_PORT:-2080}"

broray_interface_name_valid()
{
    local value suffix

    value="${1:-}"
    case "$value" in
        Proxy[0-9]*)
            suffix="${value#Proxy}"
            case "$suffix" in
                ''|*[!0-9]*) return 1 ;;
            esac
            return 0
            ;;
        *) return 1 ;;
    esac
}

broray_interface_ndmc_path()
{
    local path

    case "$BRORAY_INTERFACE_NDMC" in
        */*) path="$BRORAY_INTERFACE_NDMC" ;;
        *) path="$(command -v "$BRORAY_INTERFACE_NDMC" 2>/dev/null || true)" ;;
    esac

    [ -n "$path" ] && [ -x "$path" ] || return 1
    printf '%s\n' "$path"
}

broray_interface_running_config_all()
{
    local ndmc_bin

    if [ -n "${BRORAY_INTERFACE_RUNNING_CONFIG_FIXTURE:-}" ]; then
        cat "$BRORAY_INTERFACE_RUNNING_CONFIG_FIXTURE"
        return $?
    fi

    ndmc_bin="$(broray_interface_ndmc_path)" || return 1
    "$ndmc_bin" -c 'show running-config' 2>/dev/null
}

broray_interface_owner_block()
{
    local name

    name="${1:-$(broray_interface_selected_name)}"
    broray_interface_name_valid "$name" || return 1

    broray_interface_running_config_all |
        awk -v wanted="$name" '
            $0 == "interface " wanted { found = 1 }
            found { print }
            found && $0 == "!" { exit }
        '
}

broray_interface_exists_name()
{
    local name

    name="${1:-}"
    broray_interface_name_valid "$name" || return 1

    broray_interface_running_config_all |
        awk -v wanted="$name" '
            $0 == "interface " wanted { found = 1 }
            END { exit(found ? 0 : 1) }
        '
}

broray_interface_owner_signature_matches()
{
    local name block

    name="${1:-$(broray_interface_selected_name)}"
    block="$(broray_interface_owner_block "$name")" || return 1
    [ -n "$block" ] || return 1

    printf '%s\n' "$block" | grep -Fq 'proxy protocol socks5' || return 1
    printf '%s\n' "$block" |
        grep -Fq "proxy upstream $BRORAY_PROXY_HOST $BRORAY_PROXY_PORT" || return 1
    printf '%s\n' "$block" |
        grep -Eq '^[[:space:]]*description[[:space:]]+"?BROray([[:space:]]|—|-|"|$)' || return 1

    return 0
}

broray_interface_owner_name()
{
    local name

    [ -r "$BRORAY_INTERFACE_OWNER_FILE" ] || return 1
    name="$(jq -r '.interfaceName // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    broray_interface_name_valid "$name" || return 1
    printf '%s\n' "$name"
}

broray_interface_configured_name()
{
    local name

    [ -r "$BRORAY_ROUTES_CONFIG_FILE" ] || return 1
    name="$(jq -r '.managedInterface // empty' "$BRORAY_ROUTES_CONFIG_FILE" 2>/dev/null)"
    broray_interface_name_valid "$name" || return 1
    printf '%s\n' "$name"
}

broray_interface_selected_name()
{
    local name

    if [ -n "${BRORAY_INTERFACE:-}" ]; then
        broray_interface_name_valid "$BRORAY_INTERFACE" || return 1
        printf '%s\n' "$BRORAY_INTERFACE"
        return 0
    fi

    name="$(broray_interface_owner_name 2>/dev/null || true)"
    if [ -n "$name" ]; then
        printf '%s\n' "$name"
        return 0
    fi

    name="$(broray_interface_configured_name 2>/dev/null || true)"
    if [ -n "$name" ]; then
        printf '%s\n' "$name"
    else
        printf '%s\n' "$BRORAY_INTERFACE_FALLBACK"
    fi
}

broray_interface_owner_record_valid()
{
    local selected recorded owner protocol host port

    selected="${1:-$(broray_interface_selected_name)}"
    [ -r "$BRORAY_INTERFACE_OWNER_FILE" ] || return 1

    recorded="$(jq -r '.interfaceName // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    owner="$(jq -r '.owner // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    protocol="$(jq -r '.protocol // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    host="$(jq -r '.upstream.host // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    port="$(jq -r '.upstream.port // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"

    [ "$recorded" = "$selected" ] || return 1
    [ "$owner" = "BROray" ] || return 1
    [ "$protocol" = "socks5" ] || return 1
    [ "$host" = "$BRORAY_PROXY_HOST" ] || return 1
    [ "$port" = "$BRORAY_PROXY_PORT" ] || return 1
}

broray_interface_owner_valid()
{
    local selected

    selected="${1:-$(broray_interface_selected_name)}"
    broray_interface_owner_record_valid "$selected" || return 1
    broray_interface_owner_signature_matches "$selected"
}

broray_interface_owner_write()
{
    local name mode now dir temp

    name="${1:-$(broray_interface_selected_name)}"
    mode="${2:-allocated}"
    broray_interface_name_valid "$name" || return 1

    dir="${BRORAY_INTERFACE_OWNER_FILE%/*}"
    mkdir -p "$dir" || return 1
    temp="$BRORAY_INTERFACE_OWNER_FILE.new.$$"
    now="$(date '+%Y-%m-%dT%H:%M:%S%z')"

    jq -n \
        --arg interfaceName "$name" \
        --arg owner "BROray" \
        --arg protocol "socks5" \
        --arg host "$BRORAY_PROXY_HOST" \
        --argjson port "$BRORAY_PROXY_PORT" \
        --arg selectionMode "$mode" \
        --arg updatedAt "$now" '
        {
            schemaVersion: 1,
            owner: $owner,
            interfaceName: $interfaceName,
            protocol: $protocol,
            upstream: {host: $host, port: $port},
            selectionMode: $selectionMode,
            updatedAt: $updatedAt
        }
    ' >"$temp" || {
        rm -f "$temp"
        return 1
    }

    chmod 600 "$temp" 2>/dev/null || true
    mv -f "$temp" "$BRORAY_INTERFACE_OWNER_FILE"
}

broray_interface_first_matching()
{
    local config name count found

    config="$(mktemp "${TMPDIR:-/tmp}/broray-interface-config.XXXXXX")" || return 1
    broray_interface_running_config_all >"$config" || {
        rm -f "$config"
        return 1
    }

    count=0
    found=""
    for name in $(awk '/^interface Proxy[0-9]+$/ {print $2}' "$config"); do
        BRORAY_INTERFACE_RUNNING_CONFIG_FIXTURE="$config" \
            broray_interface_owner_signature_matches "$name" || continue
        count=$((count + 1))
        found="$name"
    done

    rm -f "$config"
    [ "$count" -eq 1 ] || return 1
    printf '%s\n' "$found"
}

broray_interface_first_free()
{
    local config index name

    config="$(mktemp "${TMPDIR:-/tmp}/broray-interface-config.XXXXXX")" || return 1
    broray_interface_running_config_all >"$config" || {
        rm -f "$config"
        return 1
    }

    index=0
    while [ "$index" -le "$BRORAY_INTERFACE_MAX_INDEX" ]; do
        name="Proxy$index"
        if ! grep -Fqx "interface $name" "$config"; then
            rm -f "$config"
            printf '%s\n' "$name"
            return 0
        fi
        index=$((index + 1))
    done

    rm -f "$config"
    return 1
}

broray_interface_select_safe()
{
    local recorded preferred matched selected mode

    recorded="$(broray_interface_owner_name 2>/dev/null || true)"
    if [ -n "$recorded" ]; then
        if ! broray_interface_exists_name "$recorded"; then
            # The recorded BROray interface was deleted. Reuse the same free
            # identifier so existing route policy remains coherent.
            printf '%s\n' "$recorded"
            return 0
        fi

        if broray_interface_owner_valid "$recorded"; then
            printf '%s\n' "$recorded"
            return 0
        fi
        # The recorded name is now occupied by a foreign/changed interface.
        # Never touch it; allocate another free ProxyN below.
    fi

    preferred="$(broray_interface_configured_name 2>/dev/null || true)"
    if [ -n "$preferred" ] &&
       broray_interface_owner_signature_matches "$preferred"
    then
        selected="$preferred"
        mode="adopted-existing"
    else
        matched="$(broray_interface_first_matching 2>/dev/null || true)"
        if [ -n "$matched" ]; then
            selected="$matched"
            mode="adopted-existing"
        else
            selected="$(broray_interface_first_free)" || return 1
            mode="allocated-free"
        fi
    fi

    broray_interface_owner_write "$selected" "$mode" || return 1
    printf '%s\n' "$selected"
}

broray_interface_require_owned()
{
    local name

    name="${1:-$(broray_interface_selected_name)}"
    if ! broray_interface_owner_valid "$name"; then
        printf 'ОШИБКА: интерфейс %s не подтверждён как принадлежащий BROray.\n' "$name" >&2
        printf '%s\n' 'Чужой или изменённый прокси-интерфейс не будет перезаписан или удалён.' >&2
        return 1
    fi
}

broray_interface_sync_route_policy()
{
    local name old file temp changed bundle state registry bundle_registry catalog

    name="${1:-$(broray_interface_selected_name)}"
    broray_interface_name_valid "$name" || return 1
    [ -r "$BRORAY_ROUTES_CONFIG_FILE" ] || return 1

    old="$(jq -r '.managedInterface // empty' "$BRORAY_ROUTES_CONFIG_FILE" 2>/dev/null)"
    changed=false
    [ "$old" = "$name" ] || changed=true

    temp="$BRORAY_ROUTES_CONFIG_FILE.new.$$"
    jq --arg interface "$name" '.managedInterface = $interface' \
        "$BRORAY_ROUTES_CONFIG_FILE" >"$temp" || {
        rm -f "$temp"
        return 1
    }
    mv -f "$temp" "$BRORAY_ROUTES_CONFIG_FILE" || return 1

    for manifest_dir in "$BRORAY_ROUTES_MANIFEST_DIR" "$BRORAY_ROUTES_SHARE_MANIFEST_DIR"; do
        [ -d "$manifest_dir" ] || continue
        for file in "$manifest_dir"/*.json; do
            [ -f "$file" ] || continue
            temp="$file.new.$$"
            jq --arg interface "$name" '.targetInterface = $interface' \
                "$file" >"$temp" || {
                rm -f "$temp"
                return 1
            }
            mv -f "$temp" "$file" || return 1
        done
    done

    if [ -d "$BRORAY_BASE/routes/catalog" ]; then
        for catalog in "$BRORAY_BASE"/routes/catalog/*; do
            [ -d "$catalog" ] || continue

            for file in "$catalog/routes.json" "$catalog/version.json"; do
                [ -f "$file" ] || continue
                temp="$file.new.$$"
                jq --arg interface "$name" '.targetInterface = $interface' \
                    "$file" >"$temp" || {
                    rm -f "$temp"
                    return 1
                }
                mv -f "$temp" "$file" || return 1
            done

            if [ "$changed" = true ]; then
                rm -f \
                    "$catalog/export-plan.json" \
                    "$catalog/keenetic-routes.bat" \
                    "$catalog/router-preflight.json" \
                    "$catalog/router-export-result.json" \
                    "$catalog/router-delete-result.json" \
                    "$catalog/router-export-missing.json" \
                    "$catalog/router-export-conflicts.json" \
                    2>/dev/null || true
            fi
        done
    fi

    if [ "$changed" = true ]; then
        registry="$BRORAY_BASE/routes/installed/routes.json"
        if [ -f "$registry" ]; then
            temp="$registry.new.$$"
            jq --arg interface "$name" '
                .managedInterface = $interface |
                .routes = [] |
                .updatedAt = (now | todateiso8601)
            ' "$registry" >"$temp" || {
                rm -f "$temp"
                return 1
            }
            mv -f "$temp" "$registry" || return 1
        fi

        for bundle_registry in "$BRORAY_BASE"/routes/installed/bundles/*.json; do
            [ -f "$bundle_registry" ] || continue
            temp="$bundle_registry.new.$$"
            jq --arg interface "$name" '
                .installedVersion = null |
                .routeKeys = [] |
                .managedRouteKeys = [] |
                .externalRouteKeys = [] |
                .targetInterface = $interface |
                .managedMetric = 1200 |
                .installedAt = null |
                .updatedAt = (now | todateiso8601)
            ' "$bundle_registry" >"$temp" || {
                rm -f "$temp"
                return 1
            }
            mv -f "$temp" "$bundle_registry" || return 1
        done

        for state in "$BRORAY_BASE"/routes/state/*.json; do
            [ -f "$state" ] || continue
            temp="$state.new.$$"
            jq --arg interface "$name" '
                .status = (if .downloadedVersion != null then "downloaded" else "not_checked" end) |
                .installedVersion = null |
                .preflight = null |
                .exportResult = null |
                .deleteResult = null |
                .routerPresence = null |
                .downloadResult.managedInterface = (
                    if .downloadResult == null then null else $interface end
                ) |
                .lastError = null |
                .updatedAt = (now | todateiso8601)
            ' "$state" >"$temp" || {
                rm -f "$temp"
                return 1
            }
            mv -f "$temp" "$state" || return 1
        done
    fi

    chmod 644 "$BRORAY_ROUTES_CONFIG_FILE" 2>/dev/null || true
    rm -f "$BRORAY_BASE/run/routes-router-config-cache.json" 2>/dev/null || true
    return 0
}

broray_interface_select_and_sync()
{
    local selected

    selected="$(broray_interface_select_safe)" || return 1
    broray_interface_sync_route_policy "$selected" || return 1
    printf '%s\n' "$selected"
}
