#!/opt/bin/ash

# Ownership-safe publication of BROray WebUI through KeenDNS HTTP Proxy.
# The object is modified only when it is absent or when the local ownership
# receipt and the exact scoped running-config block both match BROray.

BRORAY_WEB_PUBLISH_ROOT="${BRORAY_WEB_PUBLISH_ROOT:-${BRORAY_BASE:-/opt/broray}}"
BRORAY_WEB_PUBLISH_NDMC="${BRORAY_WEB_PUBLISH_NDMC:-ndmc}"
BRORAY_WEB_PUBLISH_NAME="${BRORAY_WEB_PUBLISH_NAME:-broray}"
BRORAY_WEB_PUBLISH_PORT="${BRORAY_WEB_PUBLISH_PORT:-8080}"
BRORAY_WEB_PUBLISH_OWNER="${BRORAY_WEB_PUBLISH_OWNER:-$BRORAY_WEB_PUBLISH_ROOT/config/web-publish.json}"
BRORAY_WEB_PUBLISH_RECOVERY="$BRORAY_WEB_PUBLISH_ROOT/config/web-publish-recovery-required.json"
BRORAY_WEB_PUBLISH_POLICY_LIBRARY="$BRORAY_WEB_PUBLISH_ROOT/lib/keenetic-write-policy.sh"
BRORAY_WEB_PUBLISH_VERIFY_ATTEMPTS=8
BRORAY_WEB_PUBLISH_VERIFY_DELAY_SECONDS=1

[ -r "$BRORAY_WEB_PUBLISH_POLICY_LIBRARY" ] || {
    printf '%s\n' 'BRORAY_WEB_PUBLISH_ERROR:library:WRITE_POLICY_LIBRARY_UNAVAILABLE:Общий R14C01 write-policy недоступен.' >&2
    return 1 2>/dev/null || exit 1
}
. "$BRORAY_WEB_PUBLISH_POLICY_LIBRARY" || {
    printf '%s\n' 'BRORAY_WEB_PUBLISH_ERROR:library:WRITE_POLICY_LIBRARY_INVALID:Общий R14C01 write-policy не загружен.' >&2
    return 1 2>/dev/null || exit 1
}

broray_web_publish_error()
{
    local stage code message

    stage="$1"
    code="$2"
    message="$3"
    printf 'BRORAY_WEB_PUBLISH_ERROR:%s:%s:%s\n' "$stage" "$code" "$message" >&2
}

broray_web_publish_policy_require()
{
    local stage rc

    stage="$1"
    rc=0
    broray_keenetic_write_policy_web_publish_profile_check || rc=$?
    case "$rc" in
        0) return 0 ;;
        2) broray_web_publish_error "$stage" WEB_PUBLISH_DEVELOPMENT_WRITE_DISABLED 'Development bytes не имеют права изменять Keenetic.' ;;
        3) broray_web_publish_error "$stage" WEB_PUBLISH_WRITE_POLICY_INVALID 'R14C01 write-policy повреждён или противоречив.' ;;
        4) broray_web_publish_error "$stage" WEB_PUBLISH_WRITE_PATH_INVALID 'Идентификатор Keenetic write-path не разрешён.' ;;
        5) broray_web_publish_error "$stage" WEB_PUBLISH_PHYSICAL_SERIALIZATION_REQUIRED 'Физическая сериализация HTTP Proxy ещё не привязана к staged policy.' ;;
        *) broray_web_publish_error "$stage" WEB_PUBLISH_WRITE_POLICY_FAILED 'Не удалось проверить R14C01 write-policy.' ;;
    esac
    return 1
}

broray_web_publish_port_valid()
{
    local value

    value="${1:-}"
    case "$value" in
        ''|*[!0-9]*) return 1 ;;
    esac
    [ "$value" -ge 1 ] && [ "$value" -le 65535 ]
}

broray_web_publish_name_valid()
{
    case "${1:-}" in
        ''|*[!A-Za-z0-9_-]*) return 1 ;;
        *) return 0 ;;
    esac
}

broray_web_publish_read_command_allowed()
{
    case "${1:-}" in
        'show running-config'|'show ndns') return 0 ;;
        *) return 1 ;;
    esac
}

broray_web_publish_write_command_allowed()
{
    local command_text

    command_text="${1:-}"
    broray_web_publish_name_valid "$BRORAY_WEB_PUBLISH_NAME" || return 1
    case "$command_text" in
        'system configuration save') return 0 ;;
        "ip http proxy $BRORAY_WEB_PUBLISH_NAME") return 0 ;;
        "ip http proxy $BRORAY_WEB_PUBLISH_NAME domain ndns") return 0 ;;
        "ip http proxy $BRORAY_WEB_PUBLISH_NAME ssl redirect") return 0 ;;
        "ip http proxy $BRORAY_WEB_PUBLISH_NAME security-level public") return 0 ;;
        "no ip http proxy $BRORAY_WEB_PUBLISH_NAME") return 0 ;;
    esac

    set -- $command_text
    [ "$#" -eq 8 ] &&
    [ "$1" = ip ] && [ "$2" = http ] && [ "$3" = proxy ] &&
    [ "$4" = "$BRORAY_WEB_PUBLISH_NAME" ] &&
    [ "$5" = upstream ] && [ "$6" = http ] &&
    broray_web_publish_lan_value_valid "$7" &&
    broray_web_publish_port_valid "$8" &&
    [ "$command_text" = "ip http proxy $4 upstream http $7 $8" ]
}

broray_web_publish_ndmc()
{
    local command_text

    command_text="${1:-}"
    if broray_web_publish_read_command_allowed "$command_text"; then
        :
    elif broray_web_publish_write_command_allowed "$command_text"; then
        broray_web_publish_policy_require dispatch-policy || return 1
    else
        broray_web_publish_error dispatch WEB_PUBLISH_COMMAND_NOT_AUTHORIZED \
            'Команда не входит в замороженный R14C01 HTTP Proxy grammar.'
        return 126
    fi
    "$BRORAY_WEB_PUBLISH_NDMC" -c "$command_text"
}

broray_web_publish_lan_ip()
{
    local value
    value="$(sed -n '1p' "$BRORAY_WEB_PUBLISH_ROOT/run/lan-ip" 2>/dev/null || true)"
    printf '%s\n' "$value" | awk -F. '
        NF != 4 {exit 1}
        {for(i=1;i<=4;i++) if($i !~ /^[0-9]+$/ || $i > 255) exit 1}
    ' || return 1
    printf '%s\n' "$value"
}

broray_web_publish_running_config()
{
    local target
    target="$1"
    broray_web_publish_ndmc 'show running-config' >"$target" 2>"$target.err" || {
        rm -f "$target" "$target.err"
        return 1
    }
    rm -f "$target.err"
}

broray_web_publish_ndns_prerequisite()
{
    local target rc

    target="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-show-ndns.$$.out"
    mkdir -p "${target%/*}" || return 1
    rc=0
    broray_web_publish_ndmc 'show ndns' >"$target" 2>"$target.err" || rc=$?
    if [ "$rc" -ne 0 ] || [ ! -s "$target" ]; then
        rm -f "$target" "$target.err"
        return 1
    fi
    rm -f "$target" "$target.err"
}

broray_web_publish_ndns_snapshot()
{
    local target

    target="$1"
    broray_web_publish_ndmc 'show ndns' >"$target" 2>"$target.err" || {
        rm -f "$target" "$target.err"
        return 1
    }
    if [ ! -s "$target" ]; then
        rm -f "$target" "$target.err"
        return 1
    fi
    rm -f "$target.err"
}

broray_web_publish_ndns_field()
{
    local field source

    field="$1"
    source="$2"
    awk -v wanted="$field" '
        {
            line=$0
            sub(/\r$/, "", line)
            key=line
            sub(/:.*/, "", key)
            sub(/^[[:space:]]*/, "", key)
            sub(/[[:space:]]*$/, "", key)
            if (key != wanted) next
            value=line
            sub(/^[^:]*:/, "", value)
            sub(/^[[:space:]]*/, "", value)
            sub(/[[:space:]]*$/, "", value)
            print value
            exit
        }
    ' "$source"
}

broray_web_publish_dns_label_valid()
{
    printf '%s\n' "${1:-}" | awk '
        length($0) < 1 || length($0) > 63 {exit 1}
        $0 !~ /^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$/ {exit 1}
    '
}

broray_web_publish_dns_domain_valid()
{
    printf '%s\n' "${1:-}" | awk -F. '
        length($0) < 3 || length($0) > 253 || NF < 2 {exit 1}
        {
            for (i = 1; i <= NF; i++) {
                if (length($i) < 1 || length($i) > 63) exit 1
                if ($i !~ /^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$/) exit 1
            }
        }
    '
}

# Read-only presentation state for the authenticated Keenetic page.  The
# state never adopts, repairs, or removes a live object: write authority stays
# exclusively in broray_web_publish_ensure/delete below.
broray_web_publish_status_json()
{
    local workspace config ndns lan local_address ndns_name ndns_domain ndns_access address
    local block_exists owner_present owner_valid owned_exact recovery_required policy_available
    local ndns_available consistent can_enable can_disable state reason_code reason_message

    workspace="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-status.$$"
    config="$workspace/running-config"
    ndns="$workspace/ndns"
    mkdir -p "$workspace" || return 1

    lan="$(broray_web_publish_lan_ip 2>/dev/null || true)"
    local_address=""
    if broray_web_publish_lan_value_valid "$lan"; then
        local_address="http://$lan:$BRORAY_WEB_PUBLISH_PORT/"
    else
        lan=""
    fi

    ndns_available=false
    ndns_name=""
    ndns_domain=""
    ndns_access=""
    address=""
    if broray_web_publish_ndns_snapshot "$ndns"; then
        ndns_name="$(broray_web_publish_ndns_field name "$ndns")"
        ndns_domain="$(broray_web_publish_ndns_field domain "$ndns")"
        ndns_access="$(broray_web_publish_ndns_field access "$ndns")"
        if broray_web_publish_dns_label_valid "$ndns_name" &&
           broray_web_publish_dns_domain_valid "$ndns_domain"
        then
            ndns_available=true
            address="https://$BRORAY_WEB_PUBLISH_NAME.$ndns_name.$ndns_domain/"
        fi
    fi

    if ! broray_web_publish_running_config "$config"; then
        rm -f "$config" "$config.err" "$ndns" "$ndns.err"
        rmdir "$workspace" 2>/dev/null || true
        return 1
    fi

    block_exists=false
    owner_present=false
    owner_valid=false
    owned_exact=false
    recovery_required=false
    policy_available=false
    consistent=false
    can_enable=false
    can_disable=false

    broray_web_publish_block_exists "$config" && block_exists=true
    { [ -e "$BRORAY_WEB_PUBLISH_OWNER" ] || [ -L "$BRORAY_WEB_PUBLISH_OWNER" ]; } && owner_present=true
    broray_web_publish_owner_record_valid && owner_valid=true
    if [ "$block_exists" = true ] && [ "$owner_valid" = true ] &&
       broray_web_publish_owned_block_exact "$config"
    then
        owned_exact=true
    fi
    { [ -e "$BRORAY_WEB_PUBLISH_RECOVERY" ] || [ -L "$BRORAY_WEB_PUBLISH_RECOVERY" ]; } && recovery_required=true
    broray_keenetic_write_policy_web_publish_profile_check >/dev/null 2>&1 && policy_available=true

    if [ "$recovery_required" = true ]; then
        state=recovery
        reason_code=WEB_ACCESS_RECOVERY_REQUIRED
        reason_message='Предыдущая операция с адресом WebUI требует восстановления.'
    elif [ "$owned_exact" = true ]; then
        state=enabled
        can_disable=true
        if [ -n "$lan" ] && broray_web_publish_owner_matches_desired "$lan"; then
            consistent=true
        elif [ -n "$lan" ] && [ "$ndns_available" = true ] && [ "$policy_available" = true ]; then
            can_enable=true
        fi

        if [ "$consistent" != true ]; then
            reason_code=WEB_ACCESS_LAN_CHANGED
            reason_message='LAN-адрес роутера изменился; адрес WebUI можно восстановить.'
        elif [ "$ndns_available" != true ]; then
            reason_code=KEENDNS_ADDRESS_UNAVAILABLE
            reason_message='Публикация включена, но KeenDNS не вернул корректное доменное имя.'
        else
            reason_code=WEB_ACCESS_ENABLED
            reason_message='Адрес WebUI через KeenDNS включён и принадлежит BROray.'
        fi
    elif [ "$block_exists" != true ] && [ "$owner_present" != true ]; then
        state=disabled
        if [ -n "$lan" ] && [ "$ndns_available" = true ] && [ "$policy_available" = true ]; then
            can_enable=true
            reason_code=WEB_ACCESS_DISABLED
            reason_message='Адрес WebUI через KeenDNS выключен.'
        elif [ -z "$lan" ]; then
            reason_code=WEB_ACCESS_LAN_UNAVAILABLE
            reason_message='Не удалось определить локальный адрес WebUI.'
        elif [ "$ndns_available" != true ]; then
            reason_code=KEENDNS_ADDRESS_UNAVAILABLE
            reason_message='KeenDNS не вернул корректное доменное имя.'
        else
            reason_code=WEB_ACCESS_POLICY_UNAVAILABLE
            reason_message='Управление адресом WebUI недоступно для текущего профиля записи.'
        fi
    else
        state=conflict
        reason_code=WEB_ACCESS_OWNERSHIP_CONFLICT
        reason_message='Обнаружен чужой, изменённый или неполный объект broray; автоматическое изменение запрещено.'
    fi

    rm -f "$config" "$config.err" "$ndns" "$ndns.err"
    rmdir "$workspace" 2>/dev/null || true
    jq -n \
        --arg state "$state" \
        --arg address "$address" \
        --arg localAddress "$local_address" \
        --arg name "$ndns_name" \
        --arg domain "$ndns_domain" \
        --arg access "$ndns_access" \
        --arg reasonCode "$reason_code" \
        --arg reasonMessage "$reason_message" \
        --argjson enabled "$([ "$state" = enabled ] && printf true || printf false)" \
        --argjson consistent "$consistent" \
        --argjson canEnable "$can_enable" \
        --argjson canDisable "$can_disable" \
        --argjson ndnsAvailable "$ndns_available" \
        --argjson recoveryRequired "$recovery_required" \
        --argjson liveBlockPresent "$block_exists" \
        --argjson receiptPresent "$owner_present" \
        --argjson receiptValid "$owner_valid" \
        --argjson liveBlockOwnedExact "$owned_exact" '
        {
            schemaVersion: 1,
            state: $state,
            enabled: $enabled,
            consistent: $consistent,
            canEnable: $canEnable,
            canDisable: $canDisable,
            address: (if $address == "" then null else $address end),
            localAddress: (if $localAddress == "" then null else $localAddress end),
            keenDns: {
                available: $ndnsAvailable,
                name: (if $name == "" then null else $name end),
                domain: (if $domain == "" then null else $domain end),
                access: (if $access == "" then null else $access end)
            },
            ownership: {
                liveBlockPresent: $liveBlockPresent,
                receiptPresent: $receiptPresent,
                receiptValid: $receiptValid,
                liveBlockOwnedExact: $liveBlockOwnedExact
            },
            recoveryRequired: $recoveryRequired,
            reason: {code: $reasonCode, message: $reasonMessage}
        }
    '
}

broray_web_publish_mutation_prerequisites()
{
    local stage

    stage="$1"
    if [ -e "$BRORAY_WEB_PUBLISH_RECOVERY" ] || [ -L "$BRORAY_WEB_PUBLISH_RECOVERY" ]; then
        broray_web_publish_error "$stage" WEB_PUBLISH_RECOVERY_REQUIRED 'Предыдущая HTTP Proxy транзакция требует восстановления.'
        return 1
    fi
    broray_web_publish_policy_require "$stage" || return 1
    broray_web_publish_ndns_prerequisite || {
        broray_web_publish_error "$stage" WEB_PUBLISH_NDNS_PREREQUISITE_FAILED 'Read-only команда show ndns не подтвердила доступность KeenDNS.'
        return 1
    }
}

broray_web_publish_block()
{
    local config name
    config="$1"
    name="$BRORAY_WEB_PUBLISH_NAME"
    awk -v wanted="ip http proxy $name" '
        {
            line=$0
            sub(/^[[:space:]]*/, "", line)
            sub(/[[:space:]]*$/, "", line)
        }
        line == wanted {inside=1}
        inside {print line}
        inside && line == "!" {exit}
    ' "$config"
}

broray_web_publish_block_exists()
{
    local config
    config="$1"
    broray_web_publish_block "$config" |
        grep -Fx "ip http proxy $BRORAY_WEB_PUBLISH_NAME" >/dev/null 2>&1
}

broray_web_publish_block_exact()
{
    local config lan port expected_domain expected_ssl expected_security profile
    config="$1"
    lan="$2"
    port="${3:-$BRORAY_WEB_PUBLISH_PORT}"
    expected_domain="${4:-}"
    expected_ssl="${5:-}"
    expected_security="${6:-}"
    broray_web_publish_lan_value_valid "$lan" || return 1
    broray_web_publish_port_valid "$port" || return 1
    broray_keenetic_write_policy_web_publish_serialization profile >/dev/null || return 1
    profile="$(broray_web_publish_block_profile "$config" "$lan" "$port")" || return 1
    if [ -n "$expected_domain$expected_ssl$expected_security" ]; then
        broray_web_publish_serialization_value_valid "$expected_domain" || return 1
        broray_web_publish_serialization_value_valid "$expected_ssl" || return 1
        broray_web_publish_serialization_value_valid "$expected_security" || return 1
        [ "$profile" = "$expected_domain	$expected_ssl	$expected_security" ] || return 1
    fi
    return 0
}

broray_web_publish_serialization_value_valid()
{
    case "${1:-}" in
        present|omitted) return 0 ;;
        *) return 1 ;;
    esac
}

# Validate one complete scoped object against a closed grammar and emit the
# actually observed optional-field profile. The profile is discovered from
# live state after mutation; no router identity or firmware string is used.
broray_web_publish_block_profile()
{
    local config lan port block parents terminators domain_count ssl_count security_count
    local domain_serialization ssl_serialization security_serialization expected_count count

    config="$1"
    lan="$2"
    port="${3:-$BRORAY_WEB_PUBLISH_PORT}"
    broray_web_publish_lan_value_valid "$lan" || return 1
    broray_web_publish_port_valid "$port" || return 1
    block="$(broray_web_publish_block "$config")" || return 1
    [ -n "$block" ] || return 1
    parents="$(awk -v wanted="ip http proxy $BRORAY_WEB_PUBLISH_NAME" '
        {line=$0; sub(/^[[:space:]]*/,"",line); sub(/[[:space:]]*$/,"",line); if(line==wanted)n++}
        END{print n+0}' "$config")"
    [ "$parents" = 1 ] || return 1
    terminators="$(printf '%s\n' "$block" | grep -Fxc '!')"
    [ "$terminators" = 1 ] || return 1
    [ "$(printf '%s\n' "$block" | tail -n 1)" = '!' ] || return 1

    for line in \
        "ip http proxy $BRORAY_WEB_PUBLISH_NAME" \
        "upstream http $lan $port"
    do
        [ "$(printf '%s\n' "$block" | grep -Fxc "$line")" = 1 ] || return 1
    done

    domain_count="$(printf '%s\n' "$block" | grep -Fxc 'domain ndns' || true)"
    ssl_count="$(printf '%s\n' "$block" | grep -Fxc 'ssl redirect' || true)"
    security_count="$(printf '%s\n' "$block" | grep -Fxc 'security-level public' || true)"
    [ "$domain_count" -le 1 ] && [ "$ssl_count" -le 1 ] && [ "$security_count" -le 1 ] || return 1
    [ "$domain_count" -eq 1 ] && domain_serialization=present || domain_serialization=omitted
    [ "$ssl_count" -eq 1 ] && ssl_serialization=present || ssl_serialization=omitted
    [ "$security_count" -eq 1 ] && security_serialization=present || security_serialization=omitted
    expected_count=$((2 + domain_count + ssl_count + security_count))

    count="$(printf '%s\n' "$block" | awk 'NF && $0 != "!" {count++} END {print count+0}')"
    [ "$count" = "$expected_count" ] || return 1
    printf '%s\t%s\t%s\n' "$domain_serialization" "$ssl_serialization" "$security_serialization"
}

broray_web_publish_owner_record_valid()
{
    local policy_sha

    broray_web_publish_name_valid "$BRORAY_WEB_PUBLISH_NAME" || return 1
    policy_sha="$(broray_keenetic_write_policy_sha256)" || return 1
    [ "$(broray_keenetic_write_policy_web_publish_serialization profile)" = dynamic-bounded-v1 ] || return 1
    [ -f "$BRORAY_WEB_PUBLISH_OWNER" ] &&
    [ ! -L "$BRORAY_WEB_PUBLISH_OWNER" ] || return 1
    jq -e \
        --arg name "$BRORAY_WEB_PUBLISH_NAME" \
        --arg policySha "$policy_sha" '
        .schemaVersion == 3 and .owner == "BROray" and .name == $name and
        .writePolicySha256 == $policySha and
        .serializationProfile == "dynamic-bounded-v1" and
        .upstream.scheme == "http" and
        (.upstream.host | type) == "string" and
        (.upstream.port | type) == "number" and
        .domain == "ndns" and
        .sslRedirect == true and .securityLevel == "public" and
        ([.serialization.domain,.serialization.sslRedirect,
          .serialization.securityLevel] | all(. == "present" or . == "omitted"))
    ' "$BRORAY_WEB_PUBLISH_OWNER" >/dev/null 2>&1 || return 1

    broray_web_publish_lan_value_valid "$(
        jq -r '.upstream.host' "$BRORAY_WEB_PUBLISH_OWNER" 2>/dev/null
    )" || return 1
    broray_web_publish_port_valid "$(
        jq -r '.upstream.port' "$BRORAY_WEB_PUBLISH_OWNER" 2>/dev/null
    )"
}

broray_web_publish_lan_value_valid()
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

broray_web_publish_owner_host()
{
    broray_web_publish_owner_record_valid || return 1
    jq -r '.upstream.host' "$BRORAY_WEB_PUBLISH_OWNER" 2>/dev/null
}

broray_web_publish_owner_port()
{
    broray_web_publish_owner_record_valid || return 1
    jq -r '.upstream.port' "$BRORAY_WEB_PUBLISH_OWNER" 2>/dev/null
}

broray_web_publish_owner_matches_desired()
{
    local lan

    lan="$1"
    broray_web_publish_owner_record_valid || return 1
    [ "$(broray_web_publish_owner_host)" = "$lan" ] || return 1
    [ "$(broray_web_publish_owner_port)" = "$BRORAY_WEB_PUBLISH_PORT" ]
}

broray_web_publish_owned_block_exact()
{
    local config recorded_host recorded_port domain_serialization ssl_serialization security_serialization

    config="$1"
    broray_web_publish_owner_record_valid || return 1
    recorded_host="$(broray_web_publish_owner_host)" || return 1
    recorded_port="$(broray_web_publish_owner_port)" || return 1
    domain_serialization="$(jq -r '.serialization.domain' "$BRORAY_WEB_PUBLISH_OWNER")" || return 1
    ssl_serialization="$(jq -r '.serialization.sslRedirect' "$BRORAY_WEB_PUBLISH_OWNER")" || return 1
    security_serialization="$(jq -r '.serialization.securityLevel' "$BRORAY_WEB_PUBLISH_OWNER")" || return 1
    broray_web_publish_block_exact "$config" "$recorded_host" "$recorded_port" \
        "$domain_serialization" "$ssl_serialization" "$security_serialization"
}

broray_web_publish_owner_valid()
{
    local config rc

    # Public ownership proof: recorded old endpoint must match one complete,
    # unique, !-terminated live object.  The optional historical LAN argument
    # is deliberately ignored; desired state is checked separately.
    broray_web_publish_owner_record_valid || return 1
    config="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-owner-live.$$.conf"
    mkdir -p "${config%/*}" || return 1
    broray_web_publish_running_config "$config" || return 1
    broray_web_publish_owned_block_exact "$config"
    rc=$?
    rm -f "$config"
    return "$rc"
}

broray_web_publish_owner_write()
{
    local lan temp config policy_sha profile domain_serialization ssl_serialization security_serialization
    lan="$1"
    broray_web_publish_name_valid "$BRORAY_WEB_PUBLISH_NAME" || return 1
    broray_web_publish_lan_value_valid "$lan" || return 1
    broray_web_publish_port_valid "$BRORAY_WEB_PUBLISH_PORT" || return 1
    policy_sha="$(broray_keenetic_write_policy_sha256)" || return 1
    [ "$(broray_keenetic_write_policy_web_publish_serialization profile)" = dynamic-bounded-v1 ] || return 1
    config="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-owner-profile.$$.conf"
    mkdir -p "${config%/*}" || return 1
    broray_web_publish_running_config "$config" || { rm -f "$config"; return 1; }
    profile="$(broray_web_publish_block_profile "$config" "$lan" "$BRORAY_WEB_PUBLISH_PORT")" || {
        rm -f "$config"
        return 1
    }
    rm -f "$config"
    domain_serialization="$(printf '%s\n' "$profile" | awk -F '\t' 'NF==3{print $1}')"
    ssl_serialization="$(printf '%s\n' "$profile" | awk -F '\t' 'NF==3{print $2}')"
    security_serialization="$(printf '%s\n' "$profile" | awk -F '\t' 'NF==3{print $3}')"
    broray_web_publish_serialization_value_valid "$domain_serialization" || return 1
    broray_web_publish_serialization_value_valid "$ssl_serialization" || return 1
    broray_web_publish_serialization_value_valid "$security_serialization" || return 1
    temp="$BRORAY_WEB_PUBLISH_OWNER.new.$$"
    mkdir -p "${BRORAY_WEB_PUBLISH_OWNER%/*}" || return 1
    jq -n \
        --arg name "$BRORAY_WEB_PUBLISH_NAME" \
        --arg lan "$lan" \
        --argjson port "$BRORAY_WEB_PUBLISH_PORT" \
        --arg policySha "$policy_sha" \
        --arg domainSerialization "$domain_serialization" \
        --arg sslSerialization "$ssl_serialization" \
        --arg securitySerialization "$security_serialization" \
        --arg updatedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        {schemaVersion:3,owner:"BROray",name:$name,writePolicySha256:$policySha,
         serializationProfile:"dynamic-bounded-v1",
         upstream:{scheme:"http",host:$lan,port:$port},domain:"ndns",
         sslRedirect:true,securityLevel:"public",
         serialization:{domain:$domainSerialization,sslRedirect:$sslSerialization,
           securityLevel:$securitySerialization},updatedAt:$updatedAt}
    ' >"$temp" || { rm -f "$temp"; return 1; }
    chmod 600 "$temp" 2>/dev/null || true
    mv -f "$temp" "$BRORAY_WEB_PUBLISH_OWNER"
}

broray_web_publish_owner_restore()
{
    local backup existed temporary

    backup="$1"
    existed="$2"
    if [ "$existed" = true ]; then
        [ -f "$backup" ] && [ ! -L "$backup" ] || return 1
        temporary="$BRORAY_WEB_PUBLISH_OWNER.restore.$$"
        cp -p "$backup" "$temporary" || return 1
        mv -f "$temporary" "$BRORAY_WEB_PUBLISH_OWNER"
    else
        rm -f "$BRORAY_WEB_PUBLISH_OWNER"
    fi
}

broray_web_publish_apply_exact()
{
    local lan port command

    lan="$1"
    port="$2"
    broray_web_publish_lan_value_valid "$lan" || return 1
    broray_web_publish_port_valid "$port" || return 1

    for command in \
        "ip http proxy $BRORAY_WEB_PUBLISH_NAME" \
        "ip http proxy $BRORAY_WEB_PUBLISH_NAME upstream http $lan $port" \
        "ip http proxy $BRORAY_WEB_PUBLISH_NAME domain ndns" \
        "ip http proxy $BRORAY_WEB_PUBLISH_NAME ssl redirect" \
        "ip http proxy $BRORAY_WEB_PUBLISH_NAME security-level public"
    do
        broray_web_publish_ndmc "$command" >/dev/null 2>&1 || return 1
    done
}

broray_web_publish_verify_exact_live()
{
    local lan port config rc

    lan="$1"
    port="$2"
    config="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-verify.$$.conf"
    broray_web_publish_running_config "$config" || return 1
    broray_web_publish_block_exact "$config" "$lan" "$port"
    rc=$?
    rm -f "$config"
    return "$rc"
}

broray_web_publish_verify_absent_live()
{
    local config rc

    config="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-verify-absent.$$.conf"
    broray_web_publish_running_config "$config" || return 1
    if broray_web_publish_block_exists "$config"; then
        rc=1
    else
        rc=0
    fi
    rm -f "$config"
    return "$rc"
}

broray_web_publish_wait_exact_live()
{
    local lan port attempt

    lan="$1"
    port="$2"
    attempt=1
    while [ "$attempt" -le "$BRORAY_WEB_PUBLISH_VERIFY_ATTEMPTS" ]; do
        broray_web_publish_verify_exact_live "$lan" "$port" && return 0
        [ "$attempt" -ge "$BRORAY_WEB_PUBLISH_VERIFY_ATTEMPTS" ] ||
            sleep "$BRORAY_WEB_PUBLISH_VERIFY_DELAY_SECONDS"
        attempt=$((attempt + 1))
    done
    return 1
}

broray_web_publish_wait_absent_live()
{
    local attempt

    attempt=1
    while [ "$attempt" -le "$BRORAY_WEB_PUBLISH_VERIFY_ATTEMPTS" ]; do
        broray_web_publish_verify_absent_live && return 0
        [ "$attempt" -ge "$BRORAY_WEB_PUBLISH_VERIFY_ATTEMPTS" ] ||
            sleep "$BRORAY_WEB_PUBLISH_VERIFY_DELAY_SECONDS"
        attempt=$((attempt + 1))
    done
    return 1
}

broray_web_publish_save_and_wait_exact()
{
    local lan port

    lan="$1"
    port="$2"
    broray_web_publish_ndmc 'system configuration save' >/dev/null 2>&1 || return 1
    broray_web_publish_wait_exact_live "$lan" "$port"
}

broray_web_publish_save_and_wait_absent()
{
    broray_web_publish_ndmc 'system configuration save' >/dev/null 2>&1 || return 1
    broray_web_publish_wait_absent_live
}

broray_web_publish_recovery_mark()
{
    local stage temp policy_sha

    stage="$1"
    policy_sha="$(broray_keenetic_write_policy_sha256 2>/dev/null || true)"
    temp="$BRORAY_WEB_PUBLISH_RECOVERY.new.$$"
    mkdir -p "${BRORAY_WEB_PUBLISH_RECOVERY%/*}" || return 1
    jq -n \
        --arg stage "$stage" \
        --arg policySha "$policy_sha" \
        --arg updatedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        {schemaVersion:1,state:"recovery-required",path:"web-publish",
         failedStage:$stage,writePolicySha256:$policySha,updatedAt:$updatedAt}
    ' >"$temp" || { rm -f "$temp"; return 1; }
    chmod 600 "$temp" 2>/dev/null || true
    mv -f "$temp" "$BRORAY_WEB_PUBLISH_RECOVERY"
}

broray_web_publish_rollback_absent()
{
    local backup owner_existed

    backup="$1"
    owner_existed="$2"
    broray_web_publish_ndmc "no ip http proxy $BRORAY_WEB_PUBLISH_NAME" >/dev/null 2>&1 || return 1
    broray_web_publish_wait_absent_live || return 1
    broray_web_publish_save_and_wait_absent || return 1
    broray_web_publish_owner_restore "$backup" "$owner_existed"
}

broray_web_publish_rollback_owned()
{
    local old_host old_port backup owner_existed

    old_host="$1"
    old_port="$2"
    backup="$3"
    owner_existed="$4"
    broray_web_publish_apply_exact "$old_host" "$old_port" || return 1
    broray_web_publish_wait_exact_live "$old_host" "$old_port" || return 1
    broray_web_publish_save_and_wait_exact "$old_host" "$old_port" || return 1
    broray_web_publish_owner_restore "$backup" "$owner_existed"
}

broray_web_publish_rollback_after_failure()
{
    local failed_stage initial_state old_host old_port owner_backup owner_existed

    failed_stage="$1"
    initial_state="$2"
    old_host="$3"
    old_port="$4"
    owner_backup="$5"
    owner_existed="$6"
    if [ "$initial_state" = owned ]; then
        broray_web_publish_rollback_owned \
            "$old_host" "$old_port" "$owner_backup" "$owner_existed" && return 0
    else
        broray_web_publish_rollback_absent \
            "$owner_backup" "$owner_existed" && return 0
    fi
    broray_web_publish_recovery_mark "$failed_stage" >/dev/null 2>&1 || true
    return 1
}

broray_web_publish_transaction_fail()
{
    local failed_stage code message initial_state old_host old_port owner_backup owner_existed

    failed_stage="$1"
    code="$2"
    message="$3"
    initial_state="$4"
    old_host="$5"
    old_port="$6"
    owner_backup="$7"
    owner_existed="$8"
    broray_web_publish_error "$failed_stage" "$code" "$message"
    if ! broray_web_publish_rollback_after_failure \
        "$failed_stage" "$initial_state" "$old_host" "$old_port" \
        "$owner_backup" "$owner_existed"
    then
        broray_web_publish_error rollback WEB_PUBLISH_ROLLBACK_FAILED 'Byte-exact rollback не подтверждён; установлен recovery marker.'
    fi
    rm -f "$owner_backup"
    return 1
}

broray_web_publish_exists()
{
    local config rc
    config="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-status.$$.conf"
    mkdir -p "${config%/*}" || return 1
    broray_web_publish_running_config "$config" || return 1
    broray_web_publish_block_exists "$config"
    rc=$?
    rm -f "$config"
    return "$rc"
}

broray_web_publish_ensure()
{
    local lan config owner_backup owner_existed initial_state old_host old_port message
    lan="$(broray_web_publish_lan_ip)" || {
        broray_web_publish_error ensure-preflight WEB_PUBLISH_LAN_IP_INVALID 'Не удалось определить актуальный LAN-IP для WebUI.'
        return 1
    }
    broray_web_publish_name_valid "$BRORAY_WEB_PUBLISH_NAME" || {
        broray_web_publish_error ensure-preflight WEB_PUBLISH_NAME_INVALID 'Имя HTTP Proxy не прошло строгую проверку.'
        return 1
    }
    broray_web_publish_port_valid "$BRORAY_WEB_PUBLISH_PORT" || {
        broray_web_publish_error ensure-preflight WEB_PUBLISH_PORT_INVALID 'Порт HTTP Proxy не прошёл строгую проверку.'
        return 1
    }
    if [ -e "$BRORAY_WEB_PUBLISH_RECOVERY" ] || [ -L "$BRORAY_WEB_PUBLISH_RECOVERY" ]; then
        broray_web_publish_error ensure-preflight WEB_PUBLISH_RECOVERY_REQUIRED 'Предыдущая HTTP Proxy транзакция требует восстановления.'
        return 1
    fi
    config="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-before.$$.conf"
    owner_backup="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-owner-before.$$.json"
    mkdir -p "${config%/*}" || {
        broray_web_publish_error ensure-preflight WEB_PUBLISH_STORAGE_UNAVAILABLE 'Не удалось подготовить private workspace.'
        return 1
    }
    broray_web_publish_running_config "$config" || {
        broray_web_publish_error ensure-preflight WEB_PUBLISH_RUNNING_CONFIG_UNAVAILABLE 'Read-only show running-config завершился ошибкой.'
        return 1
    }

    if broray_web_publish_block_exists "$config" ||
       [ -e "$BRORAY_WEB_PUBLISH_OWNER" ] || [ -L "$BRORAY_WEB_PUBLISH_OWNER" ]
    then
        broray_web_publish_policy_require ensure-policy || {
            rm -f "$config"
            return 1
        }
    fi

    owner_existed=false
    if [ -e "$BRORAY_WEB_PUBLISH_OWNER" ] || [ -L "$BRORAY_WEB_PUBLISH_OWNER" ]; then
        broray_web_publish_owner_record_valid || {
            rm -f "$config"
            broray_web_publish_error ensure-preflight WEB_PUBLISH_RECEIPT_INVALID 'Receipt KeenDNS HTTP Proxy повреждён, legacy или не связан с текущим policy SHA.'
            return 1
        }
        cp -p "$BRORAY_WEB_PUBLISH_OWNER" "$owner_backup" || {
            rm -f "$config"
            broray_web_publish_error ensure-preflight WEB_PUBLISH_RECEIPT_BACKUP_FAILED 'Не удалось сохранить byte-exact backup receipt.'
            return 1
        }
        owner_existed=true
    fi

    if broray_web_publish_block_exists "$config"; then
        # First prove old receipt <-> old complete live object.  Only then may
        # the desired LAN endpoint be compared or migrated.
        if ! broray_web_publish_owned_block_exact "$config"; then
            rm -f "$config"
            rm -f "$owner_backup"
            broray_web_publish_error ensure-preflight WEB_PUBLISH_OWNERSHIP_MISMATCH 'HTTP Proxy broray существует, но receipt не совпадает с полным live block.'
            return 1
        fi
        old_host="$(broray_web_publish_owner_host)" || {
            rm -f "$config" "$owner_backup"
            broray_web_publish_error ensure-preflight WEB_PUBLISH_RECEIPT_INVALID 'Не удалось прочитать host из authoritative receipt.'
            return 1
        }
        old_port="$(broray_web_publish_owner_port)" || {
            rm -f "$config" "$owner_backup"
            broray_web_publish_error ensure-preflight WEB_PUBLISH_RECEIPT_INVALID 'Не удалось прочитать port из authoritative receipt.'
            return 1
        }
        if broray_web_publish_owner_matches_desired "$lan" &&
           broray_web_publish_block_exact "$config" "$lan" "$BRORAY_WEB_PUBLISH_PORT"
        then
            rm -f "$config" "$owner_backup"
            printf '%s\n' 'KeenDNS HTTP Proxy BROray уже настроен и подтверждён.'
            return 0
        fi
        initial_state=owned
        message='KeenDNS HTTP Proxy BROray обновлён до актуального LAN-IP.'
    else
        if [ "$owner_existed" = true ]; then
            rm -f "$config" "$owner_backup"
            broray_web_publish_error ensure-preflight WEB_PUBLISH_RECEIPT_LIVE_MISMATCH 'Receipt существует, но live HTTP Proxy отсутствует; автоматическое усыновление запрещено.'
            return 1
        fi
        initial_state=absent
        old_host=''
        old_port=''
        message='KeenDNS HTTP Proxy BROray создан и проверен.'
    fi
    rm -f "$config"

    broray_web_publish_mutation_prerequisites ensure-mutation || {
        rm -f "$owner_backup"
        return 1
    }

    broray_web_publish_apply_exact "$lan" "$BRORAY_WEB_PUBLISH_PORT" || {
        broray_web_publish_transaction_fail ensure-apply WEB_PUBLISH_APPLY_COMMAND_FAILED \
            'KeeneticOS отклонила одну из документированных HTTP Proxy команд.' \
            "$initial_state" "$old_host" "$old_port" "$owner_backup" "$owner_existed" || true
        return 1
    }

    broray_web_publish_wait_exact_live "$lan" "$BRORAY_WEB_PUBLISH_PORT" || {
        broray_web_publish_transaction_fail ensure-verify-before-save WEB_PUBLISH_VERIFY_TIMEOUT \
            'HTTP Proxy не сошёлся к exact scoped running-config до save.' \
            "$initial_state" "$old_host" "$old_port" "$owner_backup" "$owner_existed" || true
        return 1
    }

    broray_web_publish_save_and_wait_exact "$lan" "$BRORAY_WEB_PUBLISH_PORT" || {
        broray_web_publish_transaction_fail ensure-save WEB_PUBLISH_SAVE_CONVERGENCE_FAILED \
            'Save не подтвердил bounded exact convergence HTTP Proxy.' \
            "$initial_state" "$old_host" "$old_port" "$owner_backup" "$owner_existed" || true
        return 1
    }

    broray_web_publish_owner_write "$lan" || {
        broray_web_publish_transaction_fail ensure-receipt WEB_PUBLISH_RECEIPT_COMMIT_FAILED \
            'Live HTTP Proxy сохранён, но authoritative receipt не записан.' \
            "$initial_state" "$old_host" "$old_port" "$owner_backup" "$owner_existed" || true
        return 1
    }

    if ! broray_web_publish_verify_exact_live "$lan" "$BRORAY_WEB_PUBLISH_PORT" ||
       ! broray_web_publish_owner_matches_desired "$lan"
    then
        broray_web_publish_transaction_fail ensure-final-verify WEB_PUBLISH_FINAL_VERIFY_FAILED \
            'Финальная live/receipt сверка не прошла.' \
            "$initial_state" "$old_host" "$old_port" "$owner_backup" "$owner_existed" || true
        return 1
    fi

    rm -f "$owner_backup"
    printf '%s\n' "$message"
}

broray_web_publish_delete()
{
    local config old_host old_port owner_backup
    if [ -e "$BRORAY_WEB_PUBLISH_RECOVERY" ] || [ -L "$BRORAY_WEB_PUBLISH_RECOVERY" ]; then
        broray_web_publish_error delete-preflight WEB_PUBLISH_RECOVERY_REQUIRED 'Предыдущая HTTP Proxy транзакция требует восстановления.'
        return 1
    fi
    config="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-delete.$$.conf"
    owner_backup="$BRORAY_WEB_PUBLISH_ROOT/tmp/web-publish-owner-delete.$$.json"
    mkdir -p "${config%/*}" || {
        broray_web_publish_error delete-preflight WEB_PUBLISH_STORAGE_UNAVAILABLE 'Не удалось подготовить private workspace.'
        return 1
    }
    broray_web_publish_running_config "$config" || {
        broray_web_publish_error delete-preflight WEB_PUBLISH_RUNNING_CONFIG_UNAVAILABLE 'Read-only show running-config завершился ошибкой.'
        return 1
    }
    if ! broray_web_publish_block_exists "$config"; then
        rm -f "$config" "$owner_backup"
        if [ -e "$BRORAY_WEB_PUBLISH_OWNER" ] || [ -L "$BRORAY_WEB_PUBLISH_OWNER" ]; then
            broray_web_publish_error delete-preflight WEB_PUBLISH_RECEIPT_LIVE_MISMATCH 'Receipt существует, но live HTTP Proxy отсутствует; автоматическое удаление receipt запрещено.'
            return 1
        fi
        return 0
    fi

    broray_web_publish_policy_require delete-policy || {
        rm -f "$config"
        return 1
    }

    broray_web_publish_owned_block_exact "$config" || {
        rm -f "$config"
        broray_web_publish_error delete-preflight WEB_PUBLISH_DELETE_AUTHORITY_REFUSED 'Чужой, legacy или изменённый HTTP Proxy не будет удалён.'
        return 1
    }
    old_host="$(broray_web_publish_owner_host)" || {
        rm -f "$config"
        broray_web_publish_error delete-preflight WEB_PUBLISH_RECEIPT_INVALID 'Authoritative receipt не содержит корректный old host.'
        return 1
    }
    old_port="$(broray_web_publish_owner_port)" || {
        rm -f "$config"
        broray_web_publish_error delete-preflight WEB_PUBLISH_RECEIPT_INVALID 'Authoritative receipt не содержит корректный old port.'
        return 1
    }
    cp -p "$BRORAY_WEB_PUBLISH_OWNER" "$owner_backup" || {
        rm -f "$config"
        broray_web_publish_error delete-preflight WEB_PUBLISH_RECEIPT_BACKUP_FAILED 'Не удалось сохранить byte-exact backup receipt.'
        return 1
    }
    rm -f "$config"

    broray_web_publish_mutation_prerequisites delete-mutation || {
        rm -f "$owner_backup"
        return 1
    }

    broray_web_publish_ndmc "no ip http proxy $BRORAY_WEB_PUBLISH_NAME" >/dev/null 2>&1 || {
        broray_web_publish_transaction_fail delete-command WEB_PUBLISH_DELETE_COMMAND_FAILED \
            'KeeneticOS отклонила документированную delete-команду HTTP Proxy.' \
            owned "$old_host" "$old_port" "$owner_backup" true || true
        return 1
    }
    broray_web_publish_wait_absent_live || {
        broray_web_publish_transaction_fail delete-verify-before-save WEB_PUBLISH_VERIFY_TIMEOUT \
            'HTTP Proxy не исчез из running-config до save.' \
            owned "$old_host" "$old_port" "$owner_backup" true || true
        return 1
    }
    broray_web_publish_save_and_wait_absent || {
        broray_web_publish_transaction_fail delete-save WEB_PUBLISH_SAVE_CONVERGENCE_FAILED \
            'Save не подтвердил bounded отсутствие HTTP Proxy.' \
            owned "$old_host" "$old_port" "$owner_backup" true || true
        return 1
    }
    rm -f "$BRORAY_WEB_PUBLISH_OWNER" || {
        broray_web_publish_transaction_fail delete-receipt WEB_PUBLISH_RECEIPT_DELETE_FAILED \
            'Live HTTP Proxy удалён, но authoritative receipt не удалён.' \
            owned "$old_host" "$old_port" "$owner_backup" true || true
        return 1
    }
    rm -f "$owner_backup"
}
