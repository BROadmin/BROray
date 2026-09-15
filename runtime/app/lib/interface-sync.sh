#!/opt/bin/ash

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_INTERFACE="${BRORAY_INTERFACE:-Proxy0}"
BRORAY_ACTIVE_SERVER_FILE="$BRORAY_BASE/config/active-server"
BRORAY_SERVERS="$BRORAY_BASE/servers"
BRORAY_PROXY_CONVERGENCE_ATTEMPTS="${BRORAY_PROXY_CONVERGENCE_ATTEMPTS:-10}"
BRORAY_PROXY_CONVERGENCE_DELAY="${BRORAY_PROXY_CONVERGENCE_DELAY:-1}"

broray_interface_active_server_id()
{
    local server_id

    [ -s "$BRORAY_ACTIVE_SERVER_FILE" ] && [ ! -L "$BRORAY_ACTIVE_SERVER_FILE" ] || return 1
    server_id="$(sed -n '1p' "$BRORAY_ACTIVE_SERVER_FILE" | tr -d '\r\n')"
    case "$server_id" in ''|*/*|*'..'*) return 1 ;; esac
    printf '%s\n' "$server_id"
}

broray_interface_active_server_name()
{
    local server_id server_file server_name

    server_id="$(broray_interface_active_server_id)" || return 1
    server_file="$BRORAY_SERVERS/$server_id.json"
    [ -f "$server_file" ] && [ ! -L "$server_file" ] || return 1
    server_name="$(jq -r '
      if ((.name|type)=="string" and .name!="") then .name
      elif ((.id|type)=="string" and .id!="") then .id
      else "" end
    ' "$server_file" 2>/dev/null)" || return 1
    server_name="$(printf '%s' "$server_name" | tr '\r\n\t' '   ' |
      sed -e 's/[[:space:]][[:space:]]*/ /g' -e 's/^ //' -e 's/ $//' -e 's/["\\;|&`$<>]//g')"
    [ -n "$server_name" ] || return 1
    printf '%s\n' "$server_name"
}

broray_interface_expected_description()
{
    local server_name description

    server_name="$(broray_interface_active_server_name 2>/dev/null || true)"
    if [ -z "$server_name" ]; then
        printf '%s\n' 'BROray'
        return 0
    fi
    description="BROray - $server_name"
    broray_interface_description_value_valid "$description" || return 1
    printf '%s\n' "$description"
}

broray_interface_set_description()
{
    local description command_text

    description="$1"
    command_text="$(broray_interface_description_command "$description")" || return 1
    broray_interface_ndmc_stage description "$command_text"
}

broray_interface_sync_wait_exact()
{
    local name host port description attempt

    name="$1"
    host="$2"
    port="$3"
    description="$4"
    attempt=0
    while [ "$attempt" -lt "$BRORAY_PROXY_CONVERGENCE_ATTEMPTS" ]; do
        attempt=$((attempt + 1))
        if broray_interface_source_exact running "$name" "$host" "$port" "$description" &&
           broray_interface_source_exact startup "$name" "$host" "$port" "$description" &&
           broray_interface_runtime_ready "$description"; then
            return 0
        fi
        [ "$attempt" -ge "$BRORAY_PROXY_CONVERGENCE_ATTEMPTS" ] || sleep "$BRORAY_PROXY_CONVERGENCE_DELAY"
    done
    return 1
}

broray_interface_config_description_decode()
{
    local line prefix encoded remainder decoded chunk hex value octal

    line="$1"
    if [ "$line" = '    description BROray' ]; then
        printf '%s\n' BROray
        return 0
    fi
    prefix='    description "'
    case "$line" in
        "$prefix"*'"') ;;
        *) return 1 ;;
    esac
    encoded="${line#"$prefix"}"
    encoded="${encoded%\"}"
    remainder="$encoded"
    decoded=''
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
    broray_interface_owner_description_valid "$decoded" || return 1
    printf '%s\n' "$decoded"
}

broray_interface_running_description()
{
    local name snapshot block line description rc

    name="$1"
    snapshot="$(mktemp "${TMPDIR:-/tmp}/broray-sync-running.XXXXXX")" || return 1
    block="$(mktemp "${TMPDIR:-/tmp}/broray-sync-block.XXXXXX")" || {
        rm -f "$snapshot"
        return 1
    }
    rc=0
    broray_interface_capture_config running "$snapshot" || rc=1
    [ "$rc" -ne 0 ] || broray_interface_block_from_snapshot "$snapshot" "$name" "$block" || rc=1
    if [ "$rc" -eq 0 ]; then
        line="$(sed -n '2p' "$block")" || rc=1
        description="$(broray_interface_config_description_decode "$line")" || rc=1
    fi
    rm -f "$snapshot" "$block"
    [ "$rc" -eq 0 ] || return 1
    printf '%s\n' "$description"
}

broray_interface_description_drift_admissible()
{
    local name expected policy_sha actual_description

    name="$1"
    expected="$2"
    [ -f "$BRORAY_INTERFACE_OWNER_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OWNER_FILE" ] || return 1
    broray_interface_name_valid "$name" || return 1
    broray_interface_owner_description_valid "$expected" || return 1
    policy_sha="$(broray_interface_write_policy_sha256)" || return 1
    jq -e \
      --arg name "$name" \
      --arg host "$BRORAY_PROXY_HOST" \
      --argjson port "$BRORAY_PROXY_PORT" \
      --arg expected "$expected" \
      --arg policySha "$policy_sha" '
      .schemaVersion == 2 and
      (.contract == "r14c34-proxy-owned-interface/1" or
       .contract == "r14c35-proxy-owned-interface/1" or
       .contract == "r14c36-proxy-owned-interface/1" or
       .contract == "r14c37-proxy-owned-interface/1" or
       .contract == "r14c38-proxy-owned-interface/1") and
      .owner == "BROray" and
      .interfaceName == $name and
      .protocol == "socks5" and
      .upstream.host == $host and
      .upstream.port == $port and
      .description == $expected and
      .writeProtocolSha256 == $policySha and
      ((.runningBlockSha256 | type) == "string" and (.runningBlockSha256 | length) == 64) and
      .runningBlockSha256 == .startupBlockSha256
    ' "$BRORAY_INTERFACE_OWNER_FILE" >/dev/null 2>&1 || return 1

    actual_description="$(broray_interface_running_description "$name" 2>/dev/null || true)"
    broray_interface_owner_description_valid "$actual_description" || return 1
    [ "$actual_description" != "$expected" ] || return 1
    broray_interface_source_exact running \
        "$name" "$BRORAY_PROXY_HOST" "$BRORAY_PROXY_PORT" "$actual_description" || return 1
    broray_interface_source_exact startup \
        "$name" "$BRORAY_PROXY_HOST" "$BRORAY_PROXY_PORT" "$actual_description" || return 1
    broray_interface_runtime_ready '' || return 1
    printf '%s\n' "$actual_description"
}

broray_interface_sync_description()
{
    local name host port old_description expected failure rollback_command exact_owner

    broray_interface_require_write_policy || return 1
    expected="$(broray_interface_expected_description)" || return 1
    name="$(broray_interface_owner_name 2>/dev/null || true)"
    exact_owner=true
    if [ -z "$name" ]; then
        exact_owner=false
        [ -f "$BRORAY_INTERFACE_OWNER_FILE" ] && [ ! -L "$BRORAY_INTERFACE_OWNER_FILE" ] || {
            printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:description sync требует полный R14C01 receipt' >&2
            return 1
        }
        name="$(jq -r '.interfaceName // empty' "$BRORAY_INTERFACE_OWNER_FILE" 2>/dev/null || true)"
        broray_interface_name_valid "$name" || {
            printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:interfaceName receipt недостоверен' >&2
            return 1
        }
    fi
    BRORAY_INTERFACE="$name"
    export BRORAY_INTERFACE
    if [ "$exact_owner" = true ]; then
        broray_interface_require_owned "$name" || return 1
        old_description="$(jq -r '.description' "$BRORAY_INTERFACE_OWNER_FILE")" || return 1
    else
        old_description="$(broray_interface_description_drift_admissible "$name" "$expected")" || {
            printf '%s\n' 'BRORAY_PROXY_ERROR:PROXY_DELETE_AUTHORITY_REFUSED:расхождение не ограничено одним именем owned ProxyN' >&2
            return 1
        }
    fi
    host="$(jq -r '.upstream.host' "$BRORAY_INTERFACE_OWNER_FILE")" || return 1
    port="$(jq -r '.upstream.port' "$BRORAY_INTERFACE_OWNER_FILE")" || return 1
    if [ "$old_description" = "$expected" ]; then
        printf '%s\n' 'Синхронизация не требуется'
        return 0
    fi

    failure=''
    broray_interface_set_description "$expected" || failure=description
    [ -n "$failure" ] || sleep "${BRORAY_PROXY_CONFIG_SETTLE_DELAY:-5}"
    [ -n "$failure" ] || broray_interface_ndmc_stage save 'system configuration save' || failure=save
    [ -n "$failure" ] || broray_interface_sync_wait_exact "$name" "$host" "$port" "$expected" || failure=convergence-timeout
    if [ -n "$failure" ]; then
        rollback_command="$(broray_interface_description_command "$old_description")" || return 1
        if broray_interface_ndmc_stage rollback-description \
             "$rollback_command" &&
           broray_interface_ndmc_stage rollback-save 'system configuration save' &&
           broray_interface_sync_wait_exact "$name" "$host" "$port" "$old_description"; then
            printf 'BRORAY_PROXY_SYNC_ROLLBACK=PASS failedStage=%s\n' "$failure" >&2
        else
            printf 'BRORAY_PROXY_ERROR:PROXY_RECOVERY_REQUIRED:description rollback не доказан failedStage=%s\n' "$failure" >&2
        fi
        return 1
    fi
    BRORAY_PROXY_HOST="$host"
    BRORAY_PROXY_PORT="$port"
    export BRORAY_PROXY_HOST BRORAY_PROXY_PORT
    broray_interface_owner_write "$name" description-synced "$expected" || return 1
    printf '%s\n' 'Описание интерфейса синхронизировано и подтверждено exact receipt'
}
