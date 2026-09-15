#!/opt/bin/ash

# R14C01 ProxyN read/command core. Every write command records exact stage,
# command, rc, stdout and stderr. A zero ndmc rc is only command acceptance;
# callers must separately prove bounded running/startup/runtime convergence.

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_INTERFACE_OWNER_LIBRARY="${BRORAY_INTERFACE_OWNER_LIBRARY:-$BRORAY_BASE/lib/interface-owner.sh}"
BRORAY_INTERFACE_LAST_EVIDENCE="${BRORAY_INTERFACE_LAST_EVIDENCE:-$BRORAY_BASE/run/interface-last-command.json}"

[ -r "$BRORAY_INTERFACE_OWNER_LIBRARY" ] && . "$BRORAY_INTERFACE_OWNER_LIBRARY" || return 1 2>/dev/null || exit 1

BRORAY_INTERFACE="${BRORAY_INTERFACE:-$(broray_interface_selected_name)}"
broray_interface_name_valid "$BRORAY_INTERFACE" || return 1 2>/dev/null || exit 1

broray_interface_command_evidence_write()
{
    local stage command_text rc stdout_file stderr_file now dir temporary

    stage="$1"
    command_text="$2"
    rc="$3"
    stdout_file="$4"
    stderr_file="$5"
    now="$(date '+%Y-%m-%dT%H:%M:%S%z')"
    dir="${BRORAY_INTERFACE_LAST_EVIDENCE%/*}"
    mkdir -p "$dir" || return 1
    [ -d "$dir" ] && [ ! -L "$dir" ] || return 1
    temporary="$BRORAY_INTERFACE_LAST_EVIDENCE.new.$$"
    jq -n --arg stage "$stage" --arg command "$command_text" --argjson rc "$rc" \
      --rawfile stdout "$stdout_file" --rawfile stderr "$stderr_file" --arg at "$now" '
      {schemaVersion:1,contract:"r14c01-proxy-ndmc-stage-evidence/1",
       stage:$stage,command:$command,rc:$rc,stdout:$stdout,stderr:$stderr,observedAt:$at}
    ' >"$temporary" || { rm -f "$temporary"; return 1; }
    chmod 0600 "$temporary" && mv -f "$temporary" "$BRORAY_INTERFACE_LAST_EVIDENCE"
}

broray_interface_description_value_valid()
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

broray_interface_description_command()
{
    local description

    description="${1:-}"
    broray_interface_description_value_valid "$description" || return 1
    if [ "$description" = BROray ]; then
        printf 'interface %s description BROray\n' "$BRORAY_INTERFACE"
    else
        printf 'interface %s description "%s"\n' "$BRORAY_INTERFACE" "$description"
    fi
}

broray_interface_stage_command_allowed()
{
    local stage command_text prefix description

    stage="${1:-}"
    command_text="${2:-}"
    broray_interface_name_valid "$BRORAY_INTERFACE" || return 1

    case "$stage" in
        create-interface|rollback-create-interface)
            [ "$command_text" = "interface $BRORAY_INTERFACE" ]
            ;;
        protocol|rollback-protocol)
            [ "$command_text" = "interface $BRORAY_INTERFACE proxy protocol socks5" ]
            ;;
        upstream|rollback-upstream)
            set -- $command_text
            [ "$#" -eq 6 ] &&
            [ "$1" = interface ] && [ "$2" = "$BRORAY_INTERFACE" ] &&
            [ "$3" = proxy ] && [ "$4" = upstream ] &&
            broray_interface_ipv4_valid "$5" &&
            broray_interface_port_valid "$6" &&
            [ "$command_text" = "interface $2 proxy upstream $5 $6" ]
            ;;
        connect|rollback-connect)
            [ "$command_text" = "interface $BRORAY_INTERFACE proxy connect" ]
            ;;
        description|rollback-description)
            if [ "$stage" = rollback-description ] &&
               [ "$command_text" = "interface $BRORAY_INTERFACE no description" ]; then
                return 0
            fi
            if [ "$stage" = rollback-description ] &&
               [ "$command_text" = "interface $BRORAY_INTERFACE description null" ]; then
                return 0
            fi
            prefix="interface $BRORAY_INTERFACE description "
            case "$command_text" in
                "$prefix"*) ;;
                *) return 1 ;;
            esac
            description="${command_text#"$prefix"}"
            if [ "$description" = BROray ]; then
                broray_interface_description_value_valid "$description" &&
                [ "$command_text" = "$prefix$description" ]
                return $?
            fi
            case "$description" in
                '"BROray - '*'"') ;;
                *) return 1 ;;
            esac
            description="${description#\"}"
            description="${description%\"}"
            broray_interface_description_value_valid "$description" &&
            [ "$command_text" = "$prefix\"$description\"" ]
            ;;
        security-level|rollback-security-level)
            [ "$command_text" = "interface $BRORAY_INTERFACE security-level public" ]
            ;;
        admin-up|rollback-admin-up)
            [ "$command_text" = "interface $BRORAY_INTERFACE up" ]
            ;;
        save|rollback-save|cleanup-save)
            [ "$command_text" = 'system configuration save' ]
            ;;
        delete-interface|cleanup-delete)
            [ "$command_text" = "no interface $BRORAY_INTERFACE" ]
            ;;
        *) return 1 ;;
    esac
}

broray_interface_ndmc_stage()
{
    local stage command_text ndmc_bin stdout_file stderr_file rc

    stage="$1"
    command_text="$2"
    case "$stage" in
        create-interface|protocol|upstream|connect|description|security-level|admin-up|save|rollback-create-interface|rollback-protocol|rollback-upstream|rollback-connect|rollback-description|rollback-security-level|rollback-admin-up|rollback-save|delete-interface|cleanup-delete|cleanup-save) ;;
        *)
            printf 'BRORAY_PROXY_STAGE_ERROR:stage=unknown requested=%s rc=internal\n' "$stage" >&2
            return 1
            ;;
    esac
    broray_interface_stage_command_allowed "$stage" "$command_text" || {
        printf 'BRORAY_PROXY_STAGE_ERROR:stage=%s rc=internal reason=command-not-authorized\n' "$stage" >&2
        return 1
    }
    broray_interface_require_write_policy || return 1
    ndmc_bin="$(broray_interface_ndmc_path)" || {
        printf 'BRORAY_PROXY_STAGE_ERROR:stage=%s rc=127 reason=ndmc-unavailable\n' "$stage" >&2
        return 127
    }
    stdout_file="$(mktemp "${TMPDIR:-/tmp}/broray-proxy-stage-stdout.XXXXXX")" || return 1
    stderr_file="$(mktemp "${TMPDIR:-/tmp}/broray-proxy-stage-stderr.XXXXXX")" || {
        rm -f "$stdout_file"
        return 1
    }
    rc=0
    "$ndmc_bin" -c "$command_text" >"$stdout_file" 2>"$stderr_file" || rc=$?
    broray_interface_command_evidence_write "$stage" "$command_text" "$rc" "$stdout_file" "$stderr_file" || {
        rm -f "$stdout_file" "$stderr_file"
        printf 'BRORAY_PROXY_STAGE_ERROR:stage=%s rc=%s reason=evidence-write-failed\n' "$stage" "$rc" >&2
        return 1
    }
    if [ "$rc" -ne 0 ] || [ -s "$stderr_file" ]; then
        printf 'BRORAY_PROXY_STAGE_ERROR:stage=%s rc=%s\n' "$stage" "$rc" >&2
        [ ! -s "$stdout_file" ] || { printf '%s\n' '--- ndmc stdout ---' >&2; sed -n '1,80p' "$stdout_file" >&2; }
        [ ! -s "$stderr_file" ] || { printf '%s\n' '--- ndmc stderr ---' >&2; sed -n '1,80p' "$stderr_file" >&2; }
        rm -f "$stdout_file" "$stderr_file"
        return 1
    fi
    rm -f "$stdout_file" "$stderr_file"
    return 0
}

# Compatibility entry point for read-only legacy callers.  It is deliberately
# incapable of dispatching a write: every R14C01 mutation must use the labelled
# evidence-producing wrapper above.
broray_interface_ndmc()
{
    local command_text ndmc_bin

    command_text="${1:-}"
    [ -n "$command_text" ] || return 1
    ndmc_bin="$(broray_interface_ndmc_path)" || return 127
    "$ndmc_bin" -c "$command_text"
}

broray_interface_exists()
{
    broray_interface_exists_name "$BRORAY_INTERFACE"
}

broray_interface_running_config()
{
    broray_interface_owner_block "$BRORAY_INTERFACE"
}

broray_interface_output()
{
    local ndmc_bin output error rc

    ndmc_bin="$(broray_interface_ndmc_path)" || return 1
    output="$(mktemp "${TMPDIR:-/tmp}/broray-interface-show.XXXXXX")" || return 1
    error="$output.err"
    rc=0
    "$ndmc_bin" -c "show interface $BRORAY_INTERFACE" >"$output" 2>"$error" || rc=$?
    if [ "$rc" -ne 0 ] || [ -s "$error" ]; then
        rm -f "$output" "$error"
        return 1
    fi
    cat "$output"
    rc=$?
    rm -f "$output" "$error"
    return "$rc"
}

broray_interface_value()
{
    local field_name output value count

    field_name="$1"
    case "$field_name" in type|description|link|connected|state|mtu|via|local-endpoint-address|remote-endpoint-address|admin-only) ;; *) return 1 ;; esac
    output="$(mktemp "${TMPDIR:-/tmp}/broray-interface-value.XXXXXX")" || return 1
    broray_interface_output >"$output" || { rm -f "$output"; return 1; }
    count="$(awk -v field="$field_name" '
      {
        line=$0
        sub(/\r$/, "", line)
        sub(/^[[:space:]]+/, "", line)
        if (index(line, field ":")==1 && !found) found=1
      }
      END { print found+0 }
    ' "$output")" || { rm -f "$output"; return 1; }
    [ "$count" = 1 ] || { rm -f "$output"; return 1; }
    value="$(awk -v field="$field_name" '
      {
        line=$0
        sub(/\r$/, "", line)
        sub(/^[[:space:]]+/, "", line)
        if (index(line, field ":")==1) {
          sub(/^[^:]*:[[:space:]]*/, "", line)
          sub(/[[:space:]]+$/, "", line)
          print line
          exit
        }
      }
    ' "$output")" || { rm -f "$output"; return 1; }
    rm -f "$output"
    [ -n "$value" ] || return 1
    printf '%s\n' "$value"
}

broray_interface_runtime_ready()
{
    local expected_description output rc

    expected_description="${1:-}"
    output="$(mktemp "${TMPDIR:-/tmp}/broray-interface-runtime.XXXXXX")" || return 1
    broray_interface_output >"$output" || { rm -f "$output"; return 1; }
    awk -v expected_description="$expected_description" '
      {
        line=$0
        sub(/\r$/, "", line)
        sub(/^[[:space:]]+/, "", line)
        sub(/[[:space:]]+$/, "", line)
        separator=index(line, ":")
        if (separator==0) next
        key=substr(line,1,separator-1)
        value=substr(line,separator+1)
        sub(/^[[:space:]]+/, "", value)
        if (key=="type" && !type_count) { type_count=1; type_value=value }
        else if (key=="description" && !description_count) { description_count=1; description_value=value }
        else if (key=="link" && !link_count) { link_count=1; link_value=value }
        else if (key=="connected" && !connected_count) { connected_count=1; connected_value=value }
        else if (key=="state" && !state_count) { state_count=1; state_value=value }
      }
      END {
        ok=type_count==1 && type_value=="Proxy" && link_count==1 && link_value=="up" &&
           description_count==1 && (expected_description=="" || description_value==expected_description) &&
           connected_count==1 && connected_value=="yes" && state_count==1 && state_value=="up"
        exit(ok ? 0 : 1)
      }
    ' "$output"
    rc=$?
    rm -f "$output"
    return "$rc"
}

broray_interface_runtime_absent()
{
    local ndmc_bin output error expected health health_error rc health_rc

    ndmc_bin="$(broray_interface_ndmc_path)" || return 1
    output="$(mktemp "${TMPDIR:-/tmp}/broray-interface-show-absent.XXXXXX")" || return 1
    error="$output.err"
    rc=0
    "$ndmc_bin" -c "show interface $BRORAY_INTERFACE" >"$output" 2>"$error" || rc=$?
    # Physical Keenetic evidence (BRK-CMD-021): an absent
    # interface is the exact rc=123/stdout parser-error fingerprint below.
    # Any other error remains ambiguous and fails closed.
    expected="$output.expected"
    printf '%s\n' 'Command::Base error[7405602]: argument parse error.' >"$expected" || {
        rm -f "$output" "$error" "$expected"
        return 1
    }
    [ "$rc" -eq 123 ] && [ ! -s "$error" ] && cmp -s "$expected" "$output" || {
        rm -f "$output" "$error" "$expected"
        return 1
    }
    health="$output.health"
    health_error="$health.err"
    health_rc=0
    "$ndmc_bin" -c 'show version' >"$health" 2>"$health_error" || health_rc=$?
    [ "$health_rc" -eq 0 ] && [ -s "$health" ] && [ ! -s "$health_error" ]
    rc=$?
    rm -f "$output" "$error" "$expected" "$health" "$health_error"
    return "$rc"
}

broray_interface_status()
{
    local exists_rc interface_type description link_state connected_state state mtu via local_endpoint remote_endpoint

    exists_rc=0
    broray_interface_exists || exists_rc=$?
    case "$exists_rc" in
        0) ;;
        1)
            printf 'Интерфейс: %s\nСуществует: нет\n' "$BRORAY_INTERFACE"
            return 1
            ;;
        *)
            printf 'Интерфейс: %s\nСостояние: чтение running-config неоднозначно или недоступно\n' "$BRORAY_INTERFACE"
            return 2
            ;;
    esac
    interface_type="$(broray_interface_value type 2>/dev/null || true)"
    description="$(broray_interface_value description 2>/dev/null || true)"
    link_state="$(broray_interface_value link 2>/dev/null || true)"
    connected_state="$(broray_interface_value connected 2>/dev/null || true)"
    state="$(broray_interface_value state 2>/dev/null || true)"
    mtu="$(broray_interface_value mtu 2>/dev/null || true)"
    via="$(broray_interface_value via 2>/dev/null || true)"
    local_endpoint="$(broray_interface_value local-endpoint-address 2>/dev/null || true)"
    remote_endpoint="$(broray_interface_value remote-endpoint-address 2>/dev/null || true)"
    printf 'Интерфейс: %s\nСуществует: да\n' "$BRORAY_INTERFACE"
    printf 'Тип: %s\nОписание: %s\nLink: %s\nConnected: %s\nState: %s\nMTU: %s\n' \
      "${interface_type:-не определён}" "${description:-не задано}" "${link_state:-не определён}" \
      "${connected_state:-не определён}" "${state:-не определён}" "${mtu:-не определён}"
    printf 'Подключение через: %s\nЛокальный адрес: %s\nУдалённый адрес: %s\n' \
      "${via:-не определено}" "${local_endpoint:-не определён}" "${remote_endpoint:-не определён}"
}

broray_interface_check()
{
    local description

    broray_interface_owner_record_valid "$BRORAY_INTERFACE" || {
        printf '%s\n' 'Полный R14C01 ownership receipt: нет' >&2
        return 1
    }
    broray_interface_owner_valid "$BRORAY_INTERFACE" || {
        printf '%s\n' 'Running/startup exact receipt equality: нет' >&2
        return 1
    }
    description="$(jq -r '.description' "$BRORAY_INTERFACE_OWNER_FILE")"
    broray_interface_runtime_ready "$description" || {
        printf '%s\n' 'Runtime Proxy/link/connected/state: не готов' >&2
        return 1
    }
    printf 'Интерфейс: %s\nПолный R14C01 ownership receipt: да\n' "$BRORAY_INTERFACE"
    printf 'Running/startup exact receipt equality: да\nRuntime Proxy/link/connected/state: готов\n'
    printf 'Описание: %s\nUpstream: %s:%s\n' "$description" \
      "$(jq -r '.upstream.host' "$BRORAY_INTERFACE_OWNER_FILE")" \
      "$(jq -r '.upstream.port' "$BRORAY_INTERFACE_OWNER_FILE")"
}
