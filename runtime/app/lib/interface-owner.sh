#!/opt/bin/ash

# BROray managed proxy ownership and safe ProxyN selection.
# A ProxyN interface is touched only when both the local ownership record and
# its live Keenetic signature match BROray.

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_INTERFACE_OWNER_FILE="${BRORAY_INTERFACE_OWNER_FILE:-$BRORAY_BASE/config/interface.json}"
BRORAY_INTERFACE_RESERVATION_FILE="${BRORAY_INTERFACE_RESERVATION_FILE:-$BRORAY_INTERFACE_OWNER_FILE.create-reservation}"
BRORAY_INTERFACE_FALLBACK="${BRORAY_INTERFACE_FALLBACK:-Proxy0}"
BRORAY_XRAY_CONFIG="${BRORAY_XRAY_CONFIG:-$BRORAY_BASE/config/config.json}"
BRORAY_ROUTES_CONFIG_FILE="${BRORAY_ROUTES_CONFIG_FILE:-$BRORAY_BASE/routes/config.json}"
BRORAY_ROUTES_MANIFEST_DIR="${BRORAY_ROUTES_MANIFEST_DIR:-$BRORAY_BASE/routes/manifests}"
BRORAY_ROUTES_SHARE_MANIFEST_DIR="${BRORAY_ROUTES_SHARE_MANIFEST_DIR:-$BRORAY_BASE/share/routes/manifests}"
BRORAY_INTERFACE_NDMC="${BRORAY_INTERFACE_NDMC:-${BRORAY_ROUTES_CONFIG_NDMC:-ndmc}}"
BRORAY_INTERFACE_MAX_INDEX="${BRORAY_INTERFACE_MAX_INDEX:-255}"
BRORAY_INTERFACE_WRITE_POLICY_LIBRARY="${BRORAY_INTERFACE_WRITE_POLICY_LIBRARY:-$BRORAY_BASE/lib/keenetic-write-policy.sh}"
BRORAY_INTERFACE_OPERATION_FILE="${BRORAY_INTERFACE_OPERATION_FILE:-$BRORAY_INTERFACE_OWNER_FILE.operation}"
BRORAY_INTERFACE_QUARANTINE_FILE="${BRORAY_INTERFACE_QUARANTINE_FILE:-$BRORAY_INTERFACE_OWNER_FILE.quarantine}"
BRORAY_INTERFACE_CONFIG_COMPARE_LIBRARY="${BRORAY_INTERFACE_CONFIG_COMPARE_LIBRARY:-$BRORAY_BASE/lib/keenetic-config-compare.sh}"

if ! command -v broray_keenetic_write_policy_check >/dev/null 2>&1; then
    if [ -r "$BRORAY_INTERFACE_WRITE_POLICY_LIBRARY" ]; then
        . "$BRORAY_INTERFACE_WRITE_POLICY_LIBRARY" || true
    fi
fi

# r14c21 ProxyN admission intentionally does not load the complete-export comparator.

broray_interface_ipv4_valid()
{
    printf '%s\n' "${1:-}" | awk -F. '
        NF != 4 {exit 1}
        {
            for (i = 1; i <= 4; i++) {
                if ($i !~ /^[0-9]+$/ || $i < 0 || $i > 255) exit 1
            }
        }
    '
}

broray_interface_port_valid()
{
    local value

    value="${1:-}"
    case "$value" in
        ''|*[!0-9]*) return 1 ;;
    esac
    [ "$value" -ge 1 ] && [ "$value" -le 65535 ]
}

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

BRORAY_PROXY_HOST="${BRORAY_PROXY_HOST:-}"
BRORAY_PROXY_PORT="${BRORAY_PROXY_PORT:-2080}"

broray_interface_ipv4_valid "$BRORAY_PROXY_HOST" &&
broray_interface_port_valid "$BRORAY_PROXY_PORT" ||
    return 1 2>/dev/null || exit 1

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

broray_interface_owner_description_valid()
{
    local description

    description="${1:-}"
    [ -n "$description" ] && [ "${#description}" -le 128 ] || return 1
    case "$description" in
        BROray|BROray\ -\ *) ;;
        *) return 1 ;;
    esac
    case "$description" in
        *'"'*|*"'"*|*'\'*|*';'*|*'`'*|*'$'*|*'|'*|*'&'*|*'<'*|*'>') return 1 ;;
    esac
    [ "$(printf '%s' "$description" | wc -l)" -eq 0 ]
}

broray_interface_description_config_line()
{
    local description

    description="${1:-}"
    broray_interface_owner_description_valid "$description" || return 1
    if [ "$description" = BROray ]; then
        printf '    description BROray\n'
    else
        printf '    description "%s"\n' "$description"
    fi
}

broray_interface_description_config_matches()
{
    local description actual literal prefix encoded remainder decoded chunk hex value octal

    description="$1"
    actual="$2"
    literal="$(broray_interface_description_config_line "$description")" || return 1
    [ "$actual" = "$literal" ] && return 0

    prefix='    description "'
    case "$actual" in
        "$prefix"*'"') ;;
        *) return 1 ;;
    esac
    encoded="${actual#"$prefix"}"
    encoded="${encoded%\"}"
    remainder="$encoded"
    decoded=""
    while :; do
        case "$remainder" in
            *'\'*)
                chunk="${remainder%%\\*}"
                decoded="${decoded}${chunk}"
                remainder="${remainder#*\\}"
                case "$remainder" in
                    x[89abcdef][0123456789abcdef]*)
                        hex="${remainder#x}"
                        hex="${hex%"${hex#??}"}"
                        value=$((0x$hex))
                        octal="$(printf '%03o' "$value")" || return 1
                        decoded="${decoded}\\${octal}"
                        remainder="${remainder#???}"
                        ;;
                    *) return 1 ;;
                esac
                ;;
            *)
                decoded="${decoded}${remainder}"
                break
                ;;
        esac
    done
    decoded="$(printf '%b' "$decoded")" || return 1
    [ "$decoded" = "$description" ]
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











broray_interface_block_provisional_safe()
{
    local block name operation create_stage protocol_stage upstream_stage description_stage security_stage up_stage description description_line host port

    block="$1"
    name="$2"
    operation="$3"
    broray_interface_operation_valid "$name" || return 1
    create_stage="$(jq -r '(.attemptedStages|index("create-interface"))!=null' "$operation")" || return 1
    protocol_stage="$(jq -r '(.attemptedStages|index("protocol"))!=null' "$operation")" || return 1
    upstream_stage="$(jq -r '(.attemptedStages|index("upstream"))!=null' "$operation")" || return 1
    description_stage="$(jq -r '(.attemptedStages|index("description"))!=null' "$operation")" || return 1
    security_stage="$(jq -r '(.attemptedStages|index("security-level"))!=null' "$operation")" || return 1
    up_stage="$(jq -r '(.attemptedStages|index("admin-up"))!=null' "$operation")" || return 1
    description="$(jq -r '.desired.description' "$operation")" || return 1
    description_line="$(sed -n '2p' "$block")" || return 1
    broray_interface_description_config_matches "$description" "$description_line" || return 1
    host="$(jq -r '.desired.upstream.host' "$operation")" || return 1
    port="$(jq -r '.desired.upstream.port' "$operation")" || return 1
    [ "$create_stage" = true ] || return 1

    awk -v parent="interface $name" -v protocol_stage="$protocol_stage" \
      -v upstream_stage="$upstream_stage" \
      -v description_stage="$description_stage" -v security_stage="$security_stage" \
      -v up_stage="$up_stage" \
      -v upstream="    proxy upstream $host $port" '
      NR==1 { if ($0!=parent) invalid=1; next }
      {
        if (NR==2) { if (description_stage!="true") invalid=1; descriptions++; next }
        if ($0=="    proxy protocol socks5") { if (protocol_stage!="true") invalid=1; protocols++; next }
        if ($0==upstream) { if (upstream_stage!="true") invalid=1; upstreams++; next }
        if ($0=="    security-level public") { if (security_stage!="true") invalid=1; securities++; next }
        if ($0=="    up") { if (up_stage!="true") invalid=1; ups++; next }
        unknown++
      }
      END {
        ok=!invalid && protocols<=1 && upstreams<=1 && descriptions<=1 &&
           securities<=1 && ups<=1 && unknown==0
        exit(ok ? 0 : 1)
      }
    ' "$block"
}

broray_interface_provisional_cleanup_safe()
{
    local name source snapshot block block_rc references rc

    name="$1"
    broray_interface_operation_valid "$name" || return 1
    rc=0
    for source in running startup; do
        snapshot="$(mktemp "${TMPDIR:-/tmp}/broray-interface-cleanup-$source.XXXXXX")" || return 1
        block="$(mktemp "${TMPDIR:-/tmp}/broray-interface-cleanup-block.XXXXXX")" || {
            rm -f "$snapshot"
            return 1
        }
        broray_interface_capture_config "$source" "$snapshot" || rc=1
        if [ "$rc" -eq 0 ]; then
            block_rc=0
            broray_interface_block_from_snapshot "$snapshot" "$name" "$block" || block_rc=$?
            case "$block_rc" in
                0)
                    references="$(broray_interface_snapshot_reference_count "$snapshot" "$name")" || rc=1
                    [ "$references" = 1 ] || rc=1
                    [ "$rc" -ne 0 ] || broray_interface_block_provisional_safe "$block" "$name" "$BRORAY_INTERFACE_OPERATION_FILE" || rc=1
                    ;;
                1)
                    references="$(broray_interface_snapshot_reference_count "$snapshot" "$name")" || rc=1
                    [ "$references" = 0 ] || rc=1
                    ;;
                *) rc=1 ;;
            esac
        fi
        rm -f "$snapshot" "$block"
        [ "$rc" -eq 0 ] || return 1
    done
    return 0
}

broray_interface_operation_clear()
{
    local name

    name="$1"
    broray_interface_operation_valid "$name" || return 1
    rm -f "$BRORAY_INTERFACE_OPERATION_FILE"
}

broray_interface_json_prepare_preserved_temp()
{
    local source temp

    source="$1"
    temp="$2"

    [ -f "$source" ] && [ ! -L "$source" ] || return 1
    [ ! -e "$temp" ] && [ ! -L "$temp" ] || return 1
    cp -p "$source" "$temp" || {
        rm -f "$temp"
        return 1
    }
    [ -f "$temp" ] && [ ! -L "$temp" ] || {
        rm -f "$temp"
        return 1
    }
}

broray_interface_json_set_string_preserve_mode()
{
    local file field value current temp

    file="$1"
    field="$2"
    value="$3"

    [ -f "$file" ] && [ ! -L "$file" ] || return 1
    jq -e 'type == "object"' "$file" >/dev/null 2>&1 || return 1
    current="$(jq -r --arg field "$field" '.[$field] // ""' "$file")" || return 1
    [ "$current" = "$value" ] && return 0

    temp="$file.new.$$"
    broray_interface_json_prepare_preserved_temp "$file" "$temp" || return 1
    jq --arg field "$field" --arg value "$value" '.[$field] = $value' \
        "$file" >"$temp" || {
        rm -f "$temp"
        return 1
    }
    jq -e --arg field "$field" --arg value "$value" \
        'type == "object" and .[$field] == $value' "$temp" >/dev/null 2>&1 || {
        rm -f "$temp"
        return 1
    }
    mv -f "$temp" "$file" || {
        rm -f "$temp"
        return 1
    }
}

broray_interface_sync_route_policy()
{
    local name old file temp changed bundle state registry bundle_registry catalog

    name="${1:-$(broray_interface_selected_name)}"
    broray_interface_name_valid "$name" || return 1
    [ -f "$BRORAY_ROUTES_CONFIG_FILE" ] && [ ! -L "$BRORAY_ROUTES_CONFIG_FILE" ] || return 1
    jq -e 'type == "object"' "$BRORAY_ROUTES_CONFIG_FILE" >/dev/null 2>&1 || return 1

    old="$(jq -r '.managedInterface // empty' "$BRORAY_ROUTES_CONFIG_FILE" 2>/dev/null)"
    changed=false
    [ "$old" = "$name" ] || changed=true

    broray_interface_json_set_string_preserve_mode \
        "$BRORAY_ROUTES_CONFIG_FILE" managedInterface "$name" || return 1

    # The share directory belongs to the immutable application slot and is a
    # template source only.  Runtime interface selection must update only the
    # persistent route manifests copied below /opt/broray/routes.
    for manifest_dir in "$BRORAY_ROUTES_MANIFEST_DIR"; do
        if [ -e "$manifest_dir" ] || [ -L "$manifest_dir" ]; then
            [ -d "$manifest_dir" ] && [ ! -L "$manifest_dir" ] || return 1
        else
            continue
        fi
        for file in "$manifest_dir"/*.json; do
            if [ -e "$file" ] || [ -L "$file" ]; then
                broray_interface_json_set_string_preserve_mode \
                    "$file" targetInterface "$name" || return 1
            fi
        done
    done

    if [ -e "$BRORAY_BASE/routes/catalog" ] || [ -L "$BRORAY_BASE/routes/catalog" ]; then
        [ -d "$BRORAY_BASE/routes/catalog" ] && \
            [ ! -L "$BRORAY_BASE/routes/catalog" ] || return 1
        for catalog in "$BRORAY_BASE"/routes/catalog/*; do
            if [ -e "$catalog" ] || [ -L "$catalog" ]; then
                [ -d "$catalog" ] && [ ! -L "$catalog" ] || return 1
            else
                continue
            fi

            for file in "$catalog/routes.json" "$catalog/version.json"; do
                if [ -e "$file" ] || [ -L "$file" ]; then
                    broray_interface_json_set_string_preserve_mode \
                        "$file" targetInterface "$name" || return 1
                fi
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
        if [ -e "$registry" ] || [ -L "$registry" ]; then
            temp="$registry.new.$$"
            broray_interface_json_prepare_preserved_temp "$registry" "$temp" || return 1
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
            [ -e "$bundle_registry" ] || [ -L "$bundle_registry" ] || continue
            temp="$bundle_registry.new.$$"
            broray_interface_json_prepare_preserved_temp "$bundle_registry" "$temp" || return 1
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
            [ -e "$state" ] || [ -L "$state" ] || continue
            temp="$state.new.$$"
            broray_interface_json_prepare_preserved_temp "$state" "$temp" || return 1
            jq --arg interface "$name" '
                .status = (if .downloadedVersion != null then "downloaded" else "not_checked" end) |
                .installedVersion = null |
                .lastVerifiedAt = null |
                .verifyResult = null |
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

    rm -f "$BRORAY_BASE/run/routes-router-config-cache.json" 2>/dev/null || true
    return 0
}


# R14C01 strict ProxyN ownership contract.  The older helpers above are retained
# only for source-history readability; the definitions below are the active
# implementations.  A write-policy hash, an exact complete running/startup
# block and a unique interface-name selector are all required before a ProxyN
# can grant mutation or delete authority.

broray_interface_write_policy_sha256()
{
    command -v broray_keenetic_write_policy_proxy_interface_profile_check >/dev/null 2>&1 || return 1
    broray_keenetic_write_policy_proxy_interface_profile_check || return 1
    broray_keenetic_write_policy_sha256
}

broray_interface_require_write_policy()
{
    local rc

    command -v broray_keenetic_write_policy_proxy_interface_profile_check >/dev/null 2>&1 || {
        printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_WRITE_POLICY_INVALID:общий immutable Keenetic write-policy не загружен' >&2
        return 1
    }
    rc=0
    broray_keenetic_write_policy_proxy_interface_profile_check || rc=$?
    case "$rc" in
        0) return 0 ;;
        2)
            printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_PHYSICAL_WRITE_PROTOCOL_REQUIRED:ProxyN write path отключён в development bytes' >&2
            ;;
        3)
            printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_WRITE_POLICY_INVALID:immutable Keenetic write-policy повреждён или несогласован' >&2
            ;;
        4)
            printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_WRITE_PATH_UNKNOWN:proxy-interface отсутствует в immutable Keenetic write-policy' >&2
            ;;
        5)
            printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_SERIALIZATION_DISCOVERY_REQUIRED:физическая ProxyN serialization ещё unknown; production mutation запрещена' >&2
            ;;
        *)
            printf 'BRORAY_PROXY_ERROR:PROXY_WRITE_POLICY_INVALID:неожиданный rc=%s\n' "$rc" >&2
            ;;
    esac
    return 1
}

broray_interface_json_atomic()
{
    local target temp

    target="$1"
    temp="$target.new.$$"
    cat >"$temp" || { rm -f "$temp"; return 1; }
    jq -e . "$temp" >/dev/null 2>&1 || { rm -f "$temp"; return 1; }
    chmod 0600 "$temp" || { rm -f "$temp"; return 1; }
    mv -f "$temp" "$target"
}

broray_interface_capture_config()
{
    local source output error fixture command_text ndmc_bin rc

    source="$1"
    output="$2"
    error="$output.err"
    fixture=''
    command_text=''
    case "$source" in
        running)
            fixture="${BRORAY_INTERFACE_RUNNING_CONFIG_FIXTURE:-}"
            command_text='show running-config'
            ;;
        startup)
            fixture="${BRORAY_INTERFACE_STARTUP_CONFIG_FIXTURE:-}"
            command_text='more startup-config'
            ;;
        *) return 1 ;;
    esac

    if [ -n "$fixture" ]; then
        [ -f "$fixture" ] && [ ! -L "$fixture" ] || return 1
        cat "$fixture" >"$output"
        return $?
    fi

    ndmc_bin="$(broray_interface_ndmc_path)" || return 1
    rc=0
    "$ndmc_bin" -c "$command_text" >"$output" 2>"$error" || rc=$?
    if [ "$rc" -ne 0 ] || [ -s "$error" ]; then
        rm -f "$output" "$error"
        return 1
    fi
    rm -f "$error"
    return 0
}

broray_interface_block_from_snapshot()
{
    local snapshot name output

    snapshot="$1"
    name="$2"
    output="$3"
    broray_interface_name_valid "$name" || return 2
    [ -f "$snapshot" ] && [ ! -L "$snapshot" ] || return 2

    awk -v parent="interface $name" '
      {
        raw=$0
        sub(/\r$/, "", raw)
        trimmed=raw
        sub(/^[[:space:]]+/, "", trimmed)
        sub(/[[:space:]]+$/, "", trimmed)

        if (trimmed==parent) {
          parents++
          if (raw!=parent || inside) malformed=1
          inside=1
        }
        if (inside && raw!="!") print raw
        if (inside && raw=="!") {
          terminators++
          inside=0
        } else if (inside && trimmed ~ /^interface[[:space:]]+/ && trimmed!=parent) {
          malformed=1
        }
      }
      END {
        if (parents==0) exit 1
        if (malformed || parents!=1 || terminators!=1 || inside) exit 2
        exit 0
      }
    ' "$snapshot" >"$output"
}

broray_interface_snapshot_reference_count()
{
    local snapshot name

    snapshot="$1"
    name="$2"
    broray_interface_name_valid "$name" || return 1
    awk -v wanted="$name" '
      {
        raw=$0
        sub(/\r$/, "", raw)
        n=split(raw, token, /[[:space:]]+/)
        for (i=1; i<=n; i++) {
          value=token[i]
          gsub(/^"|"$/, "", value)
          if (value==wanted) count++
        }
      }
      END { print count+0 }
    ' "$snapshot"
}

broray_interface_snapshot_declaration_count()
{
    local snapshot name

    snapshot="$1"
    name="$2"
    broray_interface_name_valid "$name" || return 1
    awk -v parent="interface $name" '
      {
        raw=$0
        sub(/\r$/, "", raw)
        if (raw==parent) count++
      }
      END { print count+0 }
    ' "$snapshot"
}

broray_interface_block_signature_exact()
{
    local block name host port description description_line

    block="$1"
    name="$2"
    host="$3"
    port="$4"
    description="$5"
    broray_interface_name_valid "$name" || return 1
    broray_interface_ipv4_valid "$host" || return 1
    broray_interface_port_valid "$port" || return 1
    [ -f "$block" ] && [ ! -L "$block" ] || return 1
    description_line="$(sed -n '2p' "$block")" || return 1
    broray_interface_description_config_matches "$description" "$description_line" || return 1

    awk \
      -v parent="interface $name" \
      -v upstream="    proxy upstream $host $port" '
      function binding_valid(line, token) {
        if (index(line, "    proxy connect via ") != 1) return 0
        token=substr(line, 23)
        if (length(token)<1 || length(token)>64) return 0
        if (token !~ /^[A-Za-z][A-Za-z0-9._\/-]*$/) return 0
        if (token ~ /\.\./ || token ~ /\/\//) return 0
        return 1
      }
      NR==1 { if ($0!=parent) invalid=1; next }
      NR==2 { next }
      NR==3 { if ($0!="    security-level public") invalid=1; next }
      NR==4 { if ($0!="    proxy protocol socks5") invalid=1; next }
      NR==5 { if ($0!=upstream) invalid=1; next }
      NR==6 {
        if ($0=="    up") { admin_up++; next }
        if (binding_valid($0)) { bindings++; next }
        invalid=1
        next
      }
      NR==7 {
        if (bindings==1 && $0=="    up") { admin_up++; next }
        invalid=1
        next
      }
      NR>7 { invalid=1 }
      END {
        valid_length=(NR==6 || NR==7)
        exit(!invalid && valid_length && admin_up==1 && bindings<=1 ? 0 : 1)
      }
    ' "$block"
}

broray_interface_block_sha256()
{
    local block canonical digest rc

    block="$1"
    [ -f "$block" ] && [ ! -L "$block" ] || return 1
    canonical="$(mktemp "${TMPDIR:-/tmp}/broray-interface-canonical.XXXXXX")" || return 1
    rc=0
    awk '
      function binding_valid(line, token) {
        if (index(line, "    proxy connect via ") != 1) return 0
        token=substr(line, 23)
        if (length(token)<1 || length(token)>64) return 0
        if (token !~ /^[A-Za-z][A-Za-z0-9._\/-]*$/) return 0
        if (token ~ /\.\./ || token ~ /\/\//) return 0
        return 1
      }
      index($0, "    proxy connect via ")==1 {
        if (!binding_valid($0) || ++bindings>1) invalid=1
        next
      }
      { print }
      END { exit(invalid ? 1 : 0) }
    ' "$block" >"$canonical" || rc=1
    if [ "$rc" -eq 0 ]; then
        digest="$(sha256sum "$canonical" | awk 'NR==1{print $1;exit}')" || rc=1
    fi
    rm -f "$canonical"
    [ "$rc" -eq 0 ] || return 1
    case "$digest" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#digest}" -eq 64 ] || return 1
    printf '%s\n' "$digest"
}

broray_interface_source_exact()
{
    local source name host port description expected_sha snapshot block references digest rc

    source="$1"
    name="$2"
    host="$3"
    port="$4"
    description="$5"
    expected_sha="${6:-}"
    snapshot="$(mktemp "${TMPDIR:-/tmp}/broray-interface-$source.XXXXXX")" || return 1
    block="$(mktemp "${TMPDIR:-/tmp}/broray-interface-block.XXXXXX")" || {
        rm -f "$snapshot"
        return 1
    }
    rc=0
    broray_interface_capture_config "$source" "$snapshot" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_from_snapshot "$snapshot" "$name" "$block" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_signature_exact "$block" "$name" "$host" "$port" "$description" || rc=1
    if [ "$rc" -eq 0 ]; then
        references="$(broray_interface_snapshot_declaration_count "$snapshot" "$name")" || rc=1
        [ "$references" = 1 ] || rc=1
    fi
    if [ "$rc" -eq 0 ]; then
        digest="$(broray_interface_block_sha256 "$block")" || rc=1
        [ -z "$expected_sha" ] || [ "$digest" = "$expected_sha" ] || rc=1
    fi
    rm -f "$snapshot" "$block"
    return "$rc"
}

broray_interface_source_name_absent()
{
    local source name snapshot block references block_rc rc

    source="$1"
    name="$2"
    broray_interface_name_valid "$name" || return 1
    snapshot="$(mktemp "${TMPDIR:-/tmp}/broray-interface-absent.XXXXXX")" || return 1
    block="$(mktemp "${TMPDIR:-/tmp}/broray-interface-absent-block.XXXXXX")" || {
        rm -f "$snapshot"
        return 1
    }
    rc=0
    broray_interface_capture_config "$source" "$snapshot" || rc=1
    if [ "$rc" -eq 0 ]; then
        block_rc=0
        broray_interface_block_from_snapshot "$snapshot" "$name" "$block" || block_rc=$?
        [ "$block_rc" -eq 1 ] || rc=1
    fi
    if [ "$rc" -eq 0 ]; then
        references="$(broray_interface_snapshot_reference_count "$snapshot" "$name")" || rc=1
        [ "$references" = 0 ] || rc=1
    fi
    rm -f "$snapshot" "$block"
    return "$rc"
}

broray_interface_scope_baseline_sha256()
{
    local name policy_sha digest

    name="$1"
    broray_interface_name_valid "$name" || return 1
    policy_sha="$(broray_interface_write_policy_sha256)" || return 1
    digest="$(
        printf '%s\n' \
            'contract=r14c21-proxy-runtime-selection-baseline/1' \
            'candidate=3.0.0-r15c16' \
            "interface=$name" \
            'protocol=socks5' \
            "upstream=$BRORAY_PROXY_HOST:$BRORAY_PROXY_PORT" \
            'selectionProof=show-interface-rc123' \
            "writePolicySha256=$policy_sha" |
        sha256sum |
        awk 'NR==1{print $1;exit}'
    )" || return 1
    case "$digest" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#digest}" -eq 64 ] || return 1
    printf '%s\n' "$digest"
}

# Physically proven KN-1811/KN-2710 contract for a validated ProxyN name:
#   rc=0   -> the interface exists;
#   rc=123 -> the interface is absent.
# The human-readable rc=123 text is diagnostic only and is deliberately not
# fingerprinted: KeeneticOS may vary that text while preserving the command rc.
broray_interface_runtime_name_absent()
{
    local name ndmc_bin output error health health_error rc health_rc

    name="$1"
    broray_interface_name_valid "$name" || return 2
    ndmc_bin="$(broray_interface_ndmc_path)" || return 2
    output="$(mktemp "${TMPDIR:-/tmp}/broray-interface-show-name.XXXXXX")" || return 2
    error="$output.err"
    rc=0
    "$ndmc_bin" -c "show interface $name" >"$output" 2>"$error" || rc=$?

    if [ "$rc" -eq 0 ]; then
        if [ -s "$output" ] && [ ! -s "$error" ]; then
            rm -f "$output" "$error"
            return 1
        fi
        rm -f "$output" "$error"
        printf 'BRORAY_PROXY_ERROR:PROXY_RUNTIME_QUERY_AMBIGUOUS:%s returned rc=0 without a valid response\n' "$name" >&2
        return 2
    fi

    if [ "$rc" -eq 123 ] && [ -s "$output" ] && [ ! -s "$error" ]; then
        health="$output.health"
        health_error="$health.err"
        health_rc=0
        "$ndmc_bin" -c 'show version' >"$health" 2>"$health_error" || health_rc=$?
        if [ "$health_rc" -eq 0 ] && [ -s "$health" ] && [ ! -s "$health_error" ]; then
            rm -f "$output" "$error" "$health" "$health_error"
            return 0
        fi
        rm -f "$output" "$error" "$health" "$health_error"
        printf 'BRORAY_PROXY_ERROR:PROXY_RUNTIME_HEALTH_AMBIGUOUS:%s absence rc=123 could not be health-checked\n' "$name" >&2
        return 2
    fi

    rm -f "$output" "$error"
    printf 'BRORAY_PROXY_ERROR:PROXY_RUNTIME_QUERY_AMBIGUOUS:%s returned unexpected rc=%s\n' "$name" "$rc" >&2
    return 2
}

broray_interface_config_pair_without_proxy_sha256()
{
    local name host port description running startup running_block startup_block running_refs startup_refs running_sha startup_sha rc

    name="$1"
    host="$2"
    port="$3"
    description="$4"
    broray_interface_name_valid "$name" || return 1
    running="$(mktemp "${TMPDIR:-/tmp}/broray-interface-pair-running.XXXXXX")" || return 1
    startup="$(mktemp "${TMPDIR:-/tmp}/broray-interface-pair-startup.XXXXXX")" || { rm -f "$running"; return 1; }
    running_block="$(mktemp "${TMPDIR:-/tmp}/broray-interface-pair-running-block.XXXXXX")" || { rm -f "$running" "$startup"; return 1; }
    startup_block="$(mktemp "${TMPDIR:-/tmp}/broray-interface-pair-startup-block.XXXXXX")" || { rm -f "$running" "$startup" "$running_block"; return 1; }
    rc=0
    broray_interface_capture_config running "$running" &&
    broray_interface_capture_config startup "$startup" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_from_snapshot "$running" "$name" "$running_block" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_from_snapshot "$startup" "$name" "$startup_block" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_signature_exact "$running_block" "$name" "$host" "$port" "$description" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_signature_exact "$startup_block" "$name" "$host" "$port" "$description" || rc=1
    if [ "$rc" -eq 0 ]; then
        running_refs="$(broray_interface_snapshot_reference_count "$running" "$name")" || rc=1
        startup_refs="$(broray_interface_snapshot_reference_count "$startup" "$name")" || rc=1
        [ "$running_refs" = 1 ] && [ "$startup_refs" = 1 ] || rc=1
    fi
    if [ "$rc" -eq 0 ]; then
        running_sha="$(broray_interface_block_sha256 "$running_block")" || rc=1
        startup_sha="$(broray_interface_block_sha256 "$startup_block")" || rc=1
        [ "$running_sha" = "$startup_sha" ] || rc=1
    fi
    rm -f "$running" "$startup" "$running_block" "$startup_block"
    [ "$rc" -eq 0 ] || return 1
    broray_interface_scope_baseline_sha256 "$name"
}

broray_interface_config_pair_absent_matches_sha256()
{
    local name expected running startup actual rc

    name="$1"
    expected="$2"
    broray_interface_name_valid "$name" || return 1
    running="$(mktemp "${TMPDIR:-/tmp}/broray-interface-absent-pair-running.XXXXXX")" || return 1
    startup="$(mktemp "${TMPDIR:-/tmp}/broray-interface-absent-pair-startup.XXXXXX")" || { rm -f "$running"; return 1; }
    rc=0
    broray_interface_capture_config running "$running" &&
    broray_interface_capture_config startup "$startup" || rc=1
    [ "$rc" -ne 0 ] || BRORAY_INTERFACE_RUNNING_CONFIG_FIXTURE="$running" \
        broray_interface_source_name_absent running "$name" || rc=1
    [ "$rc" -ne 0 ] || BRORAY_INTERFACE_STARTUP_CONFIG_FIXTURE="$startup" \
        broray_interface_source_name_absent startup "$name" || rc=1
    if [ "$rc" -eq 0 ]; then
        actual="$(broray_interface_scope_baseline_sha256 "$name")" || rc=1
        [ "$rc" -ne 0 ] || [ "$actual" = "$expected" ] || rc=1
    fi
    rm -f "$running" "$startup"
    return "$rc"
}

broray_interface_config_pair_present_matches_sha256()
{
    local name host port description expected actual

    name="$1"
    host="$2"
    port="$3"
    description="$4"
    expected="$5"
    actual="$(broray_interface_config_pair_without_proxy_sha256 \
        "$name" "$host" "$port" "$description")" || return 1
    [ "$actual" = "$expected" ]
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
    broray_interface_name_valid "$name" || return 2

    broray_interface_running_config_all |
        awk -v wanted="$name" '
            $0 == "interface " wanted { found = 1 }
            END { exit(found ? 0 : 1) }
        '
}

broray_interface_create_reservation_write()
{
    local name mode dir temporary baseline_sha policy_sha now runtime_rc

    name="$1"
    mode="${2:-allocated-free}"
    broray_interface_name_valid "$name" || return 1
    case "$mode" in allocated-free|recorded-recreate) ;; *) return 1 ;; esac
    policy_sha="$(broray_interface_write_policy_sha256)" || return 1
    [ ! -e "$BRORAY_INTERFACE_OWNER_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OWNER_FILE" ] || return 1
    [ ! -e "$BRORAY_INTERFACE_RESERVATION_FILE" ] && [ ! -L "$BRORAY_INTERFACE_RESERVATION_FILE" ] || return 1
    [ ! -e "$BRORAY_INTERFACE_OPERATION_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OPERATION_FILE" ] || return 1

    runtime_rc=0
    broray_interface_runtime_name_absent "$name" || runtime_rc=$?
    case "$runtime_rc" in
        0) ;;
        1)
            printf 'BRORAY_PROXY_ERROR:PROXY_SELECTION_RACE:%s became occupied before reservation\n' "$name" >&2
            return 1
            ;;
        *)
            printf 'BRORAY_PROXY_ERROR:PROXY_RUNTIME_QUERY_AMBIGUOUS:%s could not be classified before reservation\n' "$name" >&2
            return 1
            ;;
    esac

    baseline_sha="$(broray_interface_scope_baseline_sha256 "$name")" || return 1
    dir="${BRORAY_INTERFACE_RESERVATION_FILE%/*}"
    mkdir -p "$dir" || return 1
    [ -d "$dir" ] && [ ! -L "$dir" ] || return 1
    temporary="$(mktemp "$dir/.interface-create-reservation.XXXXXX")" || return 1
    now="$(date '+%Y-%m-%dT%H:%M:%S%z')"
    jq -n --arg interfaceName "$name" --arg host "$BRORAY_PROXY_HOST" \
      --argjson port "$BRORAY_PROXY_PORT" --arg selectionMode "$mode" \
      --arg baselineConfigSha256 "$baseline_sha" --arg writeProtocolSha256 "$policy_sha" \
      --arg createdAt "$now" '
      {schemaVersion:4,contract:"r14c21-proxy-create-reservation/1",candidateId:"3.0.0-r15c16",
       owner:"BROray",purpose:"create-reservation-not-ownership",interfaceName:$interfaceName,
       protocol:"socks5",upstream:{host:$host,port:$port},selectionMode:$selectionMode,
       scopeState:{runtime:"absent"},
       selectionProof:{primitive:"show interface ProxyN",presentRc:0,absentRc:123},
       baselineConfigSha256:$baselineConfigSha256,writeProtocolSha256:$writeProtocolSha256,
       createdAt:$createdAt}
    ' >"$temporary" || { rm -f "$temporary"; return 1; }
    chmod 0600 "$temporary" && ln "$temporary" "$BRORAY_INTERFACE_RESERVATION_FILE" || {
        rm -f "$temporary"
        return 1
    }
    rm -f "$temporary"
}

broray_interface_create_reservation_valid()
{
    local name policy_sha expected_baseline actual_baseline runtime_rc

    name="$1"
    broray_interface_name_valid "$name" || return 1
    policy_sha="$(broray_interface_write_policy_sha256)" || return 1
    [ ! -e "$BRORAY_INTERFACE_OWNER_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OWNER_FILE" ] || return 1
    [ -f "$BRORAY_INTERFACE_RESERVATION_FILE" ] && [ ! -L "$BRORAY_INTERFACE_RESERVATION_FILE" ] || return 1
    jq -e --arg name "$name" --arg host "$BRORAY_PROXY_HOST" --argjson port "$BRORAY_PROXY_PORT" \
      --arg policySha "$policy_sha" '
      .schemaVersion==4 and .contract=="r14c21-proxy-create-reservation/1" and
      .candidateId=="3.0.0-r15c16" and .owner=="BROray" and
      .purpose=="create-reservation-not-ownership" and .interfaceName==$name and
      .protocol=="socks5" and .upstream=={host:$host,port:$port} and
      .scopeState=={runtime:"absent"} and
      .selectionProof=={primitive:"show interface ProxyN",presentRc:0,absentRc:123} and
      .writeProtocolSha256==$policySha and
      (.selectionMode=="allocated-free" or .selectionMode=="recorded-recreate") and
      all([.baselineConfigSha256,.writeProtocolSha256][];
        (type=="string") and length==64 and all(explode[];
          ((.>=48) and (.<=57)) or ((.>=97) and (.<=102))))
    ' "$BRORAY_INTERFACE_RESERVATION_FILE" >/dev/null 2>&1 || return 1

    expected_baseline="$(jq -r '.baselineConfigSha256' "$BRORAY_INTERFACE_RESERVATION_FILE")" || return 1
    actual_baseline="$(broray_interface_scope_baseline_sha256 "$name")" || return 1
    [ "$actual_baseline" = "$expected_baseline" ] || {
        printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_RESERVATION_CONTRACT_CHANGED:selected ProxyN reservation contract changed' >&2
        return 1
    }

    runtime_rc=0
    broray_interface_runtime_name_absent "$name" || runtime_rc=$?
    case "$runtime_rc" in
        0) return 0 ;;
        1)
            printf 'BRORAY_PROXY_ERROR:PROXY_RESERVATION_SCOPE_CHANGED:%s became occupied after reservation\n' "$name" >&2
            return 1
            ;;
        *)
            printf 'BRORAY_PROXY_ERROR:PROXY_RUNTIME_QUERY_AMBIGUOUS:%s could not be classified after reservation\n' "$name" >&2
            return 1
            ;;
    esac
}

broray_interface_create_reservation_clear()
{
    local name

    name="$1"
    broray_interface_name_valid "$name" || return 1
    [ -e "$BRORAY_INTERFACE_RESERVATION_FILE" ] || [ -L "$BRORAY_INTERFACE_RESERVATION_FILE" ] || return 0
    [ -f "$BRORAY_INTERFACE_RESERVATION_FILE" ] && [ ! -L "$BRORAY_INTERFACE_RESERVATION_FILE" ] || return 1
    jq -e --arg name "$name" '
      .schemaVersion==4 and .contract=="r14c21-proxy-create-reservation/1" and
      .candidateId=="3.0.0-r15c16" and .owner=="BROray" and
      .purpose=="create-reservation-not-ownership" and .interfaceName==$name
    ' "$BRORAY_INTERFACE_RESERVATION_FILE" >/dev/null 2>&1 || return 1
    rm -f "$BRORAY_INTERFACE_RESERVATION_FILE"
}

broray_interface_operation_begin_create()
{
    local name description policy_sha operation_id now

    name="$1"
    description="$2"
    broray_interface_create_reservation_valid "$name" || return 1
    [ ! -e "$BRORAY_INTERFACE_OPERATION_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OPERATION_FILE" ] || return 1
    policy_sha="$(broray_interface_write_policy_sha256)" || return 1
    operation_id="proxy-create-$$-$(date '+%s')"
    now="$(date '+%Y-%m-%dT%H:%M:%S%z')"
    jq -n --arg operationId "$operation_id" --arg name "$name" --arg host "$BRORAY_PROXY_HOST" \
      --argjson port "$BRORAY_PROXY_PORT" --arg description "$description" \
      --arg protocolSha "$policy_sha" --arg at "$now" '
      {schemaVersion:1,contract:"r14c01-proxy-provisional-create/1",owner:"BROray",
       ownershipState:"provisional",operationId:$operationId,interfaceName:$name,
       desired:{description:$description,securityLevel:"public",protocol:"socks5",
         upstream:{host:$host,port:$port},connect:"any",adminState:"up"},
       writeProtocolSha256:$protocolSha,attemptedStages:[],acceptedStages:[],
       state:"prepared",startedAt:$at,updatedAt:$at}
    ' | broray_interface_json_atomic "$BRORAY_INTERFACE_OPERATION_FILE"
}

broray_interface_operation_mark()
{
    local name stage result now field temp

    name="$1"
    stage="$2"
    result="$3"
    broray_interface_name_valid "$name" || return 1
    case "$stage" in create-interface|protocol|upstream|connect|description|security-level|admin-up|save) ;; *) return 1 ;; esac
    case "$result" in attempted) field=attemptedStages ;; accepted) field=acceptedStages ;; *) return 1 ;; esac
    [ -f "$BRORAY_INTERFACE_OPERATION_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OPERATION_FILE" ] || return 1
    now="$(date '+%Y-%m-%dT%H:%M:%S%z')"
    jq --arg name "$name" --arg stage "$stage" --arg field "$field" --arg at "$now" '
      select(.schemaVersion==1 and .contract=="r14c01-proxy-provisional-create/1" and
        .owner=="BROray" and .ownershipState=="provisional" and .interfaceName==$name) |
      if $field=="attemptedStages" then
        .attemptedStages=((.attemptedStages+[$stage])|unique)
      else
        .acceptedStages=((.acceptedStages+[$stage])|unique)
      end |
      .state=(if $stage=="save" and $field=="acceptedStages" then "save-accepted" else "mutating" end) |
      .updatedAt=$at
    ' "$BRORAY_INTERFACE_OPERATION_FILE" | broray_interface_json_atomic "$BRORAY_INTERFACE_OPERATION_FILE"
}

broray_interface_operation_valid()
{
    local name policy_sha host port description

    name="$1"
    policy_sha="$(broray_interface_write_policy_sha256)" || return 1
    [ -f "$BRORAY_INTERFACE_OPERATION_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OPERATION_FILE" ] || return 1
    jq -e --arg name "$name" --arg policySha "$policy_sha" '
      def allowed_stages:
        ["create-interface","protocol","upstream","connect","description",
         "security-level","admin-up","save"];
      .schemaVersion==1 and .contract=="r14c01-proxy-provisional-create/1" and
      .owner=="BROray" and .ownershipState=="provisional" and .interfaceName==$name and
      .desired.protocol=="socks5" and (.desired.upstream.host|type)=="string" and
      (.desired.upstream.port|type)=="number" and
      .desired.securityLevel=="public" and .desired.connect=="any" and
      .desired.adminState=="up" and .writeProtocolSha256==$policySha and
      (.attemptedStages|type)=="array" and (.acceptedStages|type)=="array" and
      ((.attemptedStages|unique|length)==(.attemptedStages|length)) and
      ((.acceptedStages|unique|length)==(.acceptedStages|length)) and
      (.attemptedStages|all(. as $stage | allowed_stages|index($stage)!=null)) and
      (.acceptedStages|all(. as $stage | allowed_stages|index($stage)!=null)) and
      ((.acceptedStages-.attemptedStages)|length)==0
    ' "$BRORAY_INTERFACE_OPERATION_FILE" >/dev/null 2>&1 || return 1
    host="$(jq -r '.desired.upstream.host' "$BRORAY_INTERFACE_OPERATION_FILE")" || return 1
    port="$(jq -r '.desired.upstream.port' "$BRORAY_INTERFACE_OPERATION_FILE")" || return 1
    description="$(jq -r '.desired.description' "$BRORAY_INTERFACE_OPERATION_FILE")" || return 1
    broray_interface_ipv4_valid "$host" && broray_interface_port_valid "$port" &&
        broray_interface_owner_description_valid "$description"
}

broray_interface_owner_record_valid()
{
    local selected recorded owner protocol host port schema description policy_sha running_sha startup_sha

    selected="${1:-$(broray_interface_selected_name)}"
    [ -r "$BRORAY_INTERFACE_OWNER_FILE" ] || return 1

    recorded="$(jq -r '.interfaceName // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    owner="$(jq -r '.owner // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    protocol="$(jq -r '.protocol // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    host="$(jq -r '.upstream.host // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    port="$(jq -r '.upstream.port // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    schema="$(jq -r '.schemaVersion // 0' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"

    [ "$recorded" = "$selected" ] || return 1
    [ "$owner" = "BROray" ] || return 1
    [ "$protocol" = "socks5" ] || return 1
    [ "$host" = "$BRORAY_PROXY_HOST" ] || return 1
    [ "$port" = "$BRORAY_PROXY_PORT" ] || return 1
    if [ "$schema" = 1 ]; then
        return 0
    fi
    [ "$schema" = 2 ] || return 1
    description="$(jq -r '.description // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    policy_sha="$(broray_interface_write_policy_sha256)" || return 1
    running_sha="$(jq -r '.runningBlockSha256 // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    startup_sha="$(jq -r '.startupBlockSha256 // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    broray_interface_owner_description_valid "$description" || return 1
    [ "$running_sha" = "$startup_sha" ] || return 1
    jq -e --arg policySha "$policy_sha" '
      (.contract=="r14c34-proxy-owned-interface/1" or
       .contract=="r14c35-proxy-owned-interface/1" or
       .contract=="r14c36-proxy-owned-interface/1" or
       .contract=="r14c37-proxy-owned-interface/1" or
       .contract=="r14c38-proxy-owned-interface/1") and
      .writeProtocolSha256==$policySha and
      ((.runningBlockSha256|type)=="string" and (.runningBlockSha256|length)==64) and
      ((.startupBlockSha256|type)=="string" and (.startupBlockSha256|length)==64)
    ' "$BRORAY_INTERFACE_OWNER_FILE" >/dev/null 2>&1 || return 1
    broray_interface_source_exact running "$selected" "$host" "$port" "$description" "$running_sha" &&
    broray_interface_source_exact startup "$selected" "$host" "$port" "$description" "$startup_sha"
}

broray_interface_legacy_block_exact()
{
    local block name host port

    block="$1"
    name="$2"
    host="$3"
    port="$4"
    [ -f "$block" ] && [ ! -L "$block" ] || return 1
    awk -v parent="interface $name" -v upstream="    proxy upstream $host $port" '
      NR==1 { if ($0!=parent) invalid=1; next }
      NR==2 && $0=="    description null" { description_null=1; next }
      $0=="    security-level public" { security++; next }
      $0=="    proxy protocol socks5" { protocol++; next }
      $0==upstream { upstream_count++; next }
      $0=="    up" { admin_up++; next }
      { invalid=1 }
      END {
        expected=(description_null ? 6 : 5)
        exit(!invalid && NR==expected && security==1 && protocol==1 &&
             upstream_count==1 && admin_up==1 ? 0 : 1)
      }
    ' "$block"
}

broray_interface_legacy_owner_migratable()
{
    local name host port running startup running_block startup_block running_refs startup_refs running_sha startup_sha rc

    name="$1"
    jq -e --arg name "$name" --arg host "$BRORAY_PROXY_HOST" --argjson port "$BRORAY_PROXY_PORT" '
      .schemaVersion==1 and .owner=="BROray" and .interfaceName==$name and
      .protocol=="socks5" and .upstream.host==$host and .upstream.port==$port
    ' "$BRORAY_INTERFACE_OWNER_FILE" >/dev/null 2>&1 || return 1
    host="$BRORAY_PROXY_HOST"
    port="$BRORAY_PROXY_PORT"
    running="$(mktemp "${TMPDIR:-/tmp}/broray-legacy-running.XXXXXX")" || return 1
    startup="$(mktemp "${TMPDIR:-/tmp}/broray-legacy-startup.XXXXXX")" || { rm -f "$running"; return 1; }
    running_block="$(mktemp "${TMPDIR:-/tmp}/broray-legacy-running-block.XXXXXX")" || { rm -f "$running" "$startup"; return 1; }
    startup_block="$(mktemp "${TMPDIR:-/tmp}/broray-legacy-startup-block.XXXXXX")" || { rm -f "$running" "$startup" "$running_block"; return 1; }
    rc=0
    broray_interface_capture_config running "$running" &&
    broray_interface_capture_config startup "$startup" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_from_snapshot "$running" "$name" "$running_block" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_from_snapshot "$startup" "$name" "$startup_block" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_legacy_block_exact "$running_block" "$name" "$host" "$port" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_legacy_block_exact "$startup_block" "$name" "$host" "$port" || rc=1
    if [ "$rc" -eq 0 ]; then
        running_refs="$(broray_interface_snapshot_declaration_count "$running" "$name")" || rc=1
        startup_refs="$(broray_interface_snapshot_declaration_count "$startup" "$name")" || rc=1
        [ "$running_refs" = 1 ] && [ "$startup_refs" = 1 ] || rc=1
    fi
    if [ "$rc" -eq 0 ]; then
        running_sha="$(broray_interface_block_sha256 "$running_block")" || rc=1
        startup_sha="$(broray_interface_block_sha256 "$startup_block")" || rc=1
        [ "$running_sha" = "$startup_sha" ] || rc=1
    fi
    rm -f "$running" "$startup" "$running_block" "$startup_block"
    return "$rc"
}

broray_interface_owner_name()
{
    local name

    [ -f "$BRORAY_INTERFACE_OWNER_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OWNER_FILE" ] || return 1
    name="$(jq -r '.interfaceName // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    broray_interface_owner_record_valid "$name" || return 1
    printf '%s\n' "$name"
}

broray_interface_owner_record_host()
{
    local selected
    selected="${1:-$(broray_interface_selected_name)}"
    broray_interface_owner_record_valid "$selected" || return 1
    jq -r '.upstream.host' "$BRORAY_INTERFACE_OWNER_FILE"
}

broray_interface_owner_record_port()
{
    local selected
    selected="${1:-$(broray_interface_selected_name)}"
    broray_interface_owner_record_valid "$selected" || return 1
    jq -r '.upstream.port' "$BRORAY_INTERFACE_OWNER_FILE"
}

broray_interface_owner_valid()
{
    local selected schema

    selected="${1:-$(broray_interface_selected_name)}"
    broray_interface_owner_record_valid "$selected" || return 1
    schema="$(jq -r '.schemaVersion // 0' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null)"
    if [ "$schema" = 1 ]; then
        broray_interface_legacy_owner_migratable "$selected"
    else
        return 0
    fi
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

broray_interface_desired_signature_matches()
{
    local name description
    name="${1:-$(broray_interface_selected_name)}"
    description="$(broray_interface_expected_description)" || return 1
    broray_interface_owner_signature_matches "$name" "$BRORAY_PROXY_HOST" "$BRORAY_PROXY_PORT" "$description"
}

broray_interface_owner_write()
{
    local name mode description now dir temp policy_sha running startup running_block startup_block running_sha startup_sha rc

    name="${1:-$(broray_interface_selected_name)}"
    mode="${2:-allocated}"
    description="${3:-$(broray_interface_expected_description)}"
    broray_interface_name_valid "$name" || return 1
    broray_interface_owner_description_valid "$description" || return 1

    running="$(mktemp "${TMPDIR:-/tmp}/broray-owner-running.XXXXXX")" || return 1
    startup="$(mktemp "${TMPDIR:-/tmp}/broray-owner-startup.XXXXXX")" || { rm -f "$running"; return 1; }
    running_block="$(mktemp "${TMPDIR:-/tmp}/broray-owner-running-block.XXXXXX")" || { rm -f "$running" "$startup"; return 1; }
    startup_block="$(mktemp "${TMPDIR:-/tmp}/broray-owner-startup-block.XXXXXX")" || { rm -f "$running" "$startup" "$running_block"; return 1; }
    rc=0
    broray_interface_capture_config running "$running" &&
    broray_interface_capture_config startup "$startup" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_from_snapshot "$running" "$name" "$running_block" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_from_snapshot "$startup" "$name" "$startup_block" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_signature_exact "$running_block" "$name" "$BRORAY_PROXY_HOST" "$BRORAY_PROXY_PORT" "$description" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_signature_exact "$startup_block" "$name" "$BRORAY_PROXY_HOST" "$BRORAY_PROXY_PORT" "$description" || rc=1
    if [ "$rc" -eq 0 ]; then
        running_sha="$(broray_interface_block_sha256 "$running_block")" || rc=1
        startup_sha="$(broray_interface_block_sha256 "$startup_block")" || rc=1
        [ "$running_sha" = "$startup_sha" ] || rc=1
    fi
    rm -f "$running" "$startup" "$running_block" "$startup_block"
    [ "$rc" -eq 0 ] || return 1
    policy_sha="$(broray_interface_write_policy_sha256)" || return 1

    dir="${BRORAY_INTERFACE_OWNER_FILE%/*}"
    mkdir -p "$dir" || return 1
    [ -d "$dir" ] && [ ! -L "$dir" ] || return 1
    temp="$BRORAY_INTERFACE_OWNER_FILE.new.$$"
    now="$(date '+%Y-%m-%dT%H:%M:%S%z')"

    jq -n \
        --arg interfaceName "$name" \
        --arg owner "BROray" \
        --arg protocol "socks5" \
        --arg host "$BRORAY_PROXY_HOST" \
        --argjson port "$BRORAY_PROXY_PORT" \
        --arg selectionMode "$mode" \
        --arg description "$description" \
        --arg writeProtocolSha256 "$policy_sha" \
        --arg runningBlockSha256 "$running_sha" \
        --arg startupBlockSha256 "$startup_sha" \
        --arg updatedAt "$now" '
        {
            schemaVersion: 2,
            contract: "r14c38-proxy-owned-interface/1",
            owner: $owner,
            interfaceName: $interfaceName,
            protocol: $protocol,
            upstream: {host: $host, port: $port},
            description: $description,
            selectionMode: $selectionMode,
            writeProtocolSha256: $writeProtocolSha256,
            runningBlockSha256: $runningBlockSha256,
            startupBlockSha256: $startupBlockSha256,
            updatedAt: $updatedAt
        }
    ' >"$temp" || {
        rm -f "$temp"
        return 1
    }

    chmod 600 "$temp" 2>/dev/null || true
    mv -f "$temp" "$BRORAY_INTERFACE_OWNER_FILE"
}

broray_interface_owner_quarantine()
{
    local reason now temp

    reason="$1"
    [ -f "$BRORAY_INTERFACE_OWNER_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OWNER_FILE" ] || return 1
    [ ! -e "$BRORAY_INTERFACE_QUARANTINE_FILE" ] && [ ! -L "$BRORAY_INTERFACE_QUARANTINE_FILE" ] || return 1
    now="$(date '+%Y-%m-%dT%H:%M:%S%z')"
    temp="$BRORAY_INTERFACE_QUARANTINE_FILE.new.$$"
    jq -n --arg reason "$reason" --arg at "$now" --slurpfile receipt "$BRORAY_INTERFACE_OWNER_FILE" '
      {schemaVersion:1,contract:"r14c01-proxy-owner-quarantine/1",reason:$reason,
       quarantinedAt:$at,receipt:$receipt[0]}
    ' >"$temp" || { rm -f "$temp"; return 1; }
    chmod 0600 "$temp" || { rm -f "$temp"; return 1; }
    mv -f "$temp" "$BRORAY_INTERFACE_QUARANTINE_FILE" || return 1
    rm -f "$BRORAY_INTERFACE_OWNER_FILE"
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
    local recorded preferred matched selected mode exists_rc

    recorded="$(broray_interface_owner_name 2>/dev/null || true)"
    if [ -n "$recorded" ]; then
        exists_rc=0
        broray_interface_exists_name "$recorded" || exists_rc=$?
        case "$exists_rc" in
            1)
                printf '%s\n' "$recorded"
                return 0
                ;;
            0)
                if broray_interface_owner_valid "$recorded"; then
                    printf '%s\n' "$recorded"
                    return 0
                fi
                ;;
            *)
                return 1
                ;;
        esac
        # A foreign/changed interface is never touched. Allocate another name.
    fi

    preferred="$(broray_interface_configured_name 2>/dev/null || true)"
    if [ -n "$preferred" ] && broray_interface_owner_signature_matches "$preferred"; then
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

    if [ "$mode" != allocated-free ]; then
        broray_interface_owner_write "$selected" "$mode" || return 1
    fi
    printf '%s\n' "$selected"
}

broray_interface_require_owned()
{
    local name
    name="${1:-$(broray_interface_selected_name)}"
    broray_interface_require_write_policy || return 1
    if ! broray_interface_owner_valid "$name"; then
        printf 'BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:%s не имеет полного policy-bound receipt/live/startup equality\n' "$name" >&2
        return 1
    fi
}

# Strict R14C01 compatibility overrides.  A few historical public entry points
# remain above because other packaged libraries may source them.  Their final
# definitions must use the same fail-closed snapshots and ownership rules as
# the R14C01 transaction path; source order must never reactivate the legacy
# running-config parser or signature-based adoption.
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

broray_interface_snapshot_name_absent()
{
    local snapshot name block block_rc references rc

    snapshot="$1"
    name="$2"
    broray_interface_name_valid "$name" || return 1
    [ -f "$snapshot" ] && [ ! -L "$snapshot" ] || return 1
    block="$(mktemp "${TMPDIR:-/tmp}/broray-interface-snapshot-absent.XXXXXX")" || return 1
    block_rc=0
    broray_interface_block_from_snapshot "$snapshot" "$name" "$block" || block_rc=$?
    rc=0
    [ "$block_rc" -eq 1 ] || rc=1
    if [ "$rc" -eq 0 ]; then
        references="$(broray_interface_snapshot_reference_count "$snapshot" "$name")" || rc=1
        [ "$references" = 0 ] || rc=1
    fi
    rm -f "$block"
    return "$rc"
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

broray_interface_select_and_sync()
{
    local selected

    selected="$(broray_interface_select_safe)" || return 1
    broray_interface_sync_route_policy "$selected" || return 1
    printf '%s\n' "$selected"
}
