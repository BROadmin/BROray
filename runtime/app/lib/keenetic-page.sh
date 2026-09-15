#!/opt/bin/ash

BRORAY_KEENETIC_CLI="${BRORAY_KEENETIC_CLI:-/opt/broray/bin/broray}"
BRORAY_KEENETIC_INTERFACE_SCRIPT="${BRORAY_KEENETIC_INTERFACE_SCRIPT:-/opt/broray/lib/interface.sh}"
BRORAY_KEENETIC_OWNER_LIBRARY="${BRORAY_KEENETIC_OWNER_LIBRARY:-/opt/broray/lib/interface-owner.sh}"

if [ -r "$BRORAY_KEENETIC_OWNER_LIBRARY" ]; then
    . "$BRORAY_KEENETIC_OWNER_LIBRARY"
fi

if command -v broray_interface_selected_name >/dev/null 2>&1; then
    BRORAY_KEENETIC_INTERFACE_NAME="$(broray_interface_selected_name)"
else
    BRORAY_KEENETIC_INTERFACE_NAME="${BRORAY_KEENETIC_INTERFACE_NAME:-}"
fi
BRORAY_KEENETIC_STATUS_FILE="${BRORAY_KEENETIC_STATUS_FILE:-/opt/broray/run/keenetic-status.json}"
BRORAY_KEENETIC_RUN_DIR="${BRORAY_KEENETIC_RUN_DIR:-/opt/broray/run}"

broray_keenetic_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_keenetic_json_valid()
{
    jq -e . "$1" >/dev/null 2>&1
}

broray_keenetic_read_expected_json()
{
    local xray_file server_file listen port socks_active
    local server_name description ready upstream

    xray_file="/opt/broray/tmp/broray-keenetic-xray-$$.json"
    server_file="/opt/broray/tmp/broray-keenetic-server-$$.json"

    "$BRORAY_KEENETIC_CLI" xray status \
        >"$xray_file" 2>/dev/null || printf '{}\n' >"$xray_file"
    "$BRORAY_KEENETIC_CLI" current \
        >"$server_file" 2>/dev/null || printf '{}\n' >"$server_file"

    broray_keenetic_json_valid "$xray_file" || printf '{}\n' >"$xray_file"
    broray_keenetic_json_valid "$server_file" || printf '{}\n' >"$server_file"

    listen="$(jq -r '.socks.address // .socks.listen // ""' "$xray_file")"
    port="$(jq -r '.socks.port // 0' "$xray_file")"
    socks_active="$(jq -r '.socks.active // false' "$xray_file")"
    server_name="$(jq -r '.name // .id // ""' "$server_file")"
    server_name="$(printf '%s' "$server_name" | tr '\r\n\t' '   ' |
        sed -e 's/[[:space:]][[:space:]]*/ /g' -e 's/^ //' -e 's/ $//' -e 's/["\\;|&`$<>]//g')"

    rm -f "$xray_file" "$server_file"

    case "$port" in
        ''|*[!0-9]*) port=0 ;;
    esac
    case "$socks_active" in
        true) socks_active=true ;;
        *) socks_active=false ;;
    esac

    description=""
    if [ -n "$server_name" ]; then
        description="BROray - $server_name"
        if command -v broray_interface_owner_description_valid >/dev/null 2>&1; then
            broray_interface_owner_description_valid "$description" || description=""
        fi
    fi

    upstream=""
    if [ -n "$listen" ] && [ "$port" -gt 0 ]; then
        upstream="$listen:$port"
    fi

    ready=false
    if [ -n "$listen" ] &&
       [ "$port" -gt 0 ] &&
       [ -n "$server_name" ] &&
       [ -n "$description" ]; then
        ready=true
    fi

    jq -n \
        --arg interfaceName "$BRORAY_KEENETIC_INTERFACE_NAME" \
        --arg description "$description" \
        --arg protocol "socks5" \
        --arg listen "$listen" \
        --argjson port "$port" \
        --arg upstream "$upstream" \
        --arg serverName "$server_name" \
        --argjson socksActive "$socks_active" \
        --argjson ready "$ready" '
        {
            ready: $ready,
            interfaceName: $interfaceName,
            description: $description,
            protocol: $protocol,
            upstream: $upstream,
            socks: {
                listen: $listen,
                port: $port,
                active: $socksActive
            },
            activeServer: {
                name: $serverName
            }
        }
    '
}

broray_keenetic_extract_value()
{
    local key input
    key="$1"
    input="$2"

    printf '%s\n' "$input" |
        awk -v key="$key" '
            {
                line = $0
                sub(/^[[:space:]]*/, "", line)
                pattern = "(^|[[:space:]])" key ":[[:space:]]*"
                if (line ~ pattern) {
                    sub("^.*" key ":[[:space:]]*", "", line)
                    print line
                    exit
                }
            }
        '
}

broray_keenetic_read_actual_json__shadowed_legacy_1_unused()
{
    local output exists description type protocol upstream
    local link connected state mtu via local_address remote_address
    local admin_only interface_name

    output="$(
        ndmc -c "show interface $BRORAY_KEENETIC_INTERFACE_NAME" \
            2>/dev/null || true
    )"

    exists=false
    if printf '%s\n' "$output" |
       grep -Fq "Interface, name = \"$BRORAY_KEENETIC_INTERFACE_NAME\""; then
        exists=true
    fi

    interface_name="$BRORAY_KEENETIC_INTERFACE_NAME"
    description="$(broray_keenetic_extract_value description "$output")"
    type="$(broray_keenetic_extract_value type "$output")"
    protocol="$(broray_keenetic_extract_value protocol "$output")"
    upstream="$(broray_keenetic_extract_value upstream "$output")"
    link="$(broray_keenetic_extract_value link "$output")"
    connected="$(broray_keenetic_extract_value connected "$output")"
    state="$(broray_keenetic_extract_value state "$output")"
    mtu="$(broray_keenetic_extract_value mtu "$output")"
    via="$(broray_keenetic_extract_value via "$output")"
    local_address="$(broray_keenetic_extract_value local "$output")"
    remote_address="$(broray_keenetic_extract_value remote "$output")"
    admin_only="$(broray_keenetic_extract_value admin-only "$output")"

    case "$mtu" in
        ''|*[!0-9]*) mtu=0 ;;
    esac

    jq -n \
        --argjson exists "$exists" \
        --arg interfaceName "$interface_name" \
        --arg description "$description" \
        --arg type "$type" \
        --arg protocol "$protocol" \
        --arg upstream "$upstream" \
        --arg link "$link" \
        --arg connected "$connected" \
        --arg state "$state" \
        --argjson mtu "$mtu" \
        --arg via "$via" \
        --arg localAddress "$local_address" \
        --arg remoteAddress "$remote_address" \
        --arg adminOnly "$admin_only" '
        {
            exists: $exists,
            interfaceName: $interfaceName,
            description: $description,
            type: $type,
            protocol: $protocol,
            upstream: $upstream,
            link: ($link == "up"),
            connected: ($connected == "yes" or $connected == "true"),
            state: $state,
            mtu: $mtu,
            via: $via,
            localAddress: $localAddress,
            remoteAddress: $remoteAddress,
            adminOnly: ($adminOnly == "yes" or $adminOnly == "true"),
            raw: {
                link: $link,
                connected: $connected,
                state: $state
            }
        }
    '
}

broray_keenetic_status_json__shadowed_legacy_1_unused()
{
    local expected actual result temp_file

    expected="$(broray_keenetic_read_expected_json)" || return 1
    actual="$(broray_keenetic_read_actual_json)" || return 1

    result="$(
        jq -n \
            --argjson expected "$expected" \
            --argjson actual "$actual" \
            --arg updatedAt "$(broray_keenetic_now)" '
            def check($value; $message):
                if $value then [] else [$message] end;

            ($expected.description // "BROray") as $displayName |
            ($actual.exists) as $exists |
            ($expected.ready and $exists and
                $actual.protocol == $expected.protocol) as $protocolMatch |
            ($expected.ready and $exists and
                $actual.upstream == $expected.upstream) as $upstreamMatch |
            ($expected.ready and $exists and
                $actual.description == $expected.description) as $descriptionMatch |
            ($exists and $actual.link) as $linkOk |
            ($exists and $actual.connected) as $connectedOk |
            ($exists and $actual.state == "up") as $stateOk |
            ($expected.socks.active) as $upstreamReachable |
            ($expected.ready and $exists and
                $protocolMatch and $upstreamMatch and $descriptionMatch) as $matchesExpected |
            ($exists and $upstreamReachable and
                $linkOk and $connectedOk and $stateOk) as $healthy |

            {
                exists: $exists,
                healthy: $healthy,
                matchesExpected: $matchesExpected,
                interfaceName: $expected.interfaceName,
                description: $actual.description,
                protocol: $actual.protocol,
                upstream: $actual.upstream,
                link: $actual.link,
                connected: $actual.connected,
                state: $actual.state,
                updatedAt: $updatedAt,
                expectedReady: $expected.ready,
                upstreamReachable: $upstreamReachable,
                expected: $expected,
                actual: $actual,
                checks: {
                    exists: $exists,
                    protocol: $protocolMatch,
                    upstream: $upstreamMatch,
                    description: $descriptionMatch,
                    upstreamReachable: $upstreamReachable,
                    link: $linkOk,
                    connected: $connectedOk,
                    state: $stateOk
                },
                technical: {
                    type: $actual.type,
                    mtu: $actual.mtu,
                    via: $actual.via,
                    localAddress: $actual.localAddress,
                    remoteAddress: $actual.remoteAddress,
                    adminOnly: $actual.adminOnly
                },
                problems:
                    (check($expected.ready;
                        "Не получены параметры SOCKS Xray или имя активного сервера.") +
                     check($exists;
                        "Интерфейс " + $expected.interfaceName + " не создан.") +
                     (if $exists and ($protocolMatch | not) then
                        ["Протокол " + $expected.interfaceName + " не совпадает с ожидаемым."] else [] end) +
                     (if $exists and ($upstreamMatch | not) then
                        ["Адрес Xray в " + $expected.interfaceName + " настроен неправильно."] else [] end) +
                     (if $exists and ($descriptionMatch | not) then
                        ["Описание " + $expected.interfaceName + " не совпадает с активным сервером."] else [] end) +
                     check($upstreamReachable;
                        "Прокси Xray недоступен.") +
                     (if $exists then check($linkOk;
                        "Связь интерфейса " + $expected.interfaceName + " не установлена.") else [] end) +
                     (if $exists then check($connectedOk;
                        "Интерфейс " + $expected.interfaceName + " не подключён.") else [] end) +
                     (if $exists then check($stateOk;
                        "Интерфейс " + $expected.interfaceName + " не работает.") else [] end)) |
                    unique
            }
        '
    )" || return 1

    mkdir -p "$BRORAY_KEENETIC_RUN_DIR" || return 1
    temp_file="$BRORAY_KEENETIC_STATUS_FILE.tmp.$$"
    printf '%s\n' "$result" >"$temp_file" || return 1
    mv "$temp_file" "$BRORAY_KEENETIC_STATUS_FILE" || return 1
    printf '%s\n' "$result"
}

broray_keenetic_expected_ready()
{
    broray_keenetic_read_expected_json |
        jq -e '.ready == true' >/dev/null 2>&1
}

broray_keenetic_run_action()
{
    local action interface_action output rc interface_suffix
    action="$1"

    case "$BRORAY_KEENETIC_INTERFACE_NAME" in
        Proxy*)
            interface_suffix="${BRORAY_KEENETIC_INTERFACE_NAME#Proxy}"
            case "$interface_suffix" in ''|*[!0-9]*) return 1 ;; esac
            ;;
        *) return 1 ;;
    esac

    case "$action" in
        create|repair)
            broray_keenetic_expected_ready || {
                printf '%s\n' \
                    "Невозможно настроить $BRORAY_KEENETIC_INTERFACE_NAME без SOCKS Xray и активного сервера." >&2
                return 2
            }
            interface_action="$action"
            ;;
        delete)
            interface_action=delete
            ;;
        sync-description)
            broray_keenetic_sync_description || return $?
            sleep 1
            broray_keenetic_status_json
            return $?
            ;;
        check-upstream|refresh)
            broray_keenetic_status_json
            return $?
            ;;
        *)
            printf 'Неизвестное действие Keenetic: %s\n' "$action" >&2
            return 2
            ;;
    esac

    if [ ! -r "$BRORAY_KEENETIC_INTERFACE_SCRIPT" ]; then
        printf '%s\n' 'Низкоуровневый модуль управляемого ProxyN недоступен.' >&2
        return 1
    fi

    output="$(
        ash "$BRORAY_KEENETIC_INTERFACE_SCRIPT" "$interface_action" 2>&1
    )"
    rc=$?
    if [ "$rc" -ne 0 ]; then
        printf '%s\n' "$output" >&2
        return "$rc"
    fi

    case "$action" in
        create|repair)
            broray_keenetic_sync_description || return $?
            ;;
    esac

    sleep 1
    broray_keenetic_status_json
}

# BROray Keenetic actual state adapter: begin

BRORAY_KEENETIC_INTERFACE_CORE="${BRORAY_KEENETIC_INTERFACE_CORE:-/opt/broray/lib/interface-core.sh}"

broray_keenetic_read_actual_json()
{
    local exists
    local interface_type
    local description
    local protocol
    local upstream
    local upstream_host
    local upstream_port
    local running_config
    local link_raw
    local connected_raw
    local state_raw
    local mtu_raw
    local via
    local local_address
    local remote_address
    local admin_raw
    local link_json
    local connected_json
    local admin_json
    local mtu_json

    exists=false
    interface_type=""
    description=""
    protocol=""
    upstream=""
    upstream_host=""
    upstream_port=""
    running_config=""
    link_raw=""
    connected_raw=""
    state_raw=""
    mtu_raw=""
    via=""
    local_address=""
    remote_address=""
    admin_raw=""
    link_json=false
    connected_json=false
    admin_json=false
    mtu_json=null

    if [ ! -r "$BRORAY_KEENETIC_INTERFACE_CORE" ]; then
        printf '%s\n' \
            "Модуль чтения интерфейса недоступен: $BRORAY_KEENETIC_INTERFACE_CORE" \
            >&2
        return 1
    fi

    BRORAY_INTERFACE="$BRORAY_KEENETIC_INTERFACE_NAME"
    . "$BRORAY_KEENETIC_INTERFACE_CORE"

    if broray_interface_exists; then
        exists=true

        interface_type="$(
            broray_interface_value type 2>/dev/null ||
                true
        )"

        description="$(
            broray_interface_value description 2>/dev/null ||
                true
        )"

        link_raw="$(
            broray_interface_value link 2>/dev/null ||
                true
        )"

        connected_raw="$(
            broray_interface_value connected 2>/dev/null ||
                true
        )"

        state_raw="$(
            broray_interface_value state 2>/dev/null ||
                true
        )"

        mtu_raw="$(
            broray_interface_value mtu 2>/dev/null ||
                true
        )"

        via="$(
            broray_interface_value via 2>/dev/null ||
                true
        )"

        local_address="$(
            broray_interface_value \
                local-endpoint-address \
                2>/dev/null ||
                true
        )"

        remote_address="$(
            broray_interface_value \
                remote-endpoint-address \
                2>/dev/null ||
                true
        )"

        admin_raw="$(
            broray_interface_value admin-only 2>/dev/null ||
                true
        )"

        running_config="$(
            broray_interface_running_config 2>/dev/null ||
                true
        )"

        protocol="$(
            printf '%s\n' "$running_config" |
                awk '
                    $1 == "proxy" &&
                    $2 == "protocol" {
                        print $3
                        exit
                    }
                ' |
                tr '[:upper:]' '[:lower:]'
        )"

        upstream_host="$(
            printf '%s\n' "$running_config" |
                awk '
                    $1 == "proxy" &&
                    $2 == "upstream" {
                        print $3
                        exit
                    }
                '
        )"

        upstream_port="$(
            printf '%s\n' "$running_config" |
                awk '
                    $1 == "proxy" &&
                    $2 == "upstream" {
                        print $4
                        exit
                    }
                '
        )"

        if [ -n "$upstream_host" ] &&
           [ -n "$upstream_port" ]; then
            upstream="$upstream_host:$upstream_port"
        fi
    fi

    case "$link_raw" in
        up|yes|true|on)
            link_json=true
            ;;
    esac

    case "$connected_raw" in
        up|yes|true|on)
            connected_json=true
            ;;
    esac

    case "$admin_raw" in
        yes|true|on)
            admin_json=true
            ;;
    esac

    case "$mtu_raw" in
        ''|*[!0-9]*)
            mtu_json=null
            ;;
        *)
            mtu_json="$mtu_raw"
            ;;
    esac

    jq -n \
        --argjson exists "$exists" \
        --arg interfaceName "$BRORAY_KEENETIC_INTERFACE_NAME" \
        --arg description "$description" \
        --arg type "$interface_type" \
        --arg protocol "$protocol" \
        --arg upstream "$upstream" \
        --argjson link "$link_json" \
        --argjson connected "$connected_json" \
        --arg state "$state_raw" \
        --argjson mtu "$mtu_json" \
        --arg via "$via" \
        --arg localAddress "$local_address" \
        --arg remoteAddress "$remote_address" \
        --argjson adminOnly "$admin_json" \
        --arg linkRaw "$link_raw" \
        --arg connectedRaw "$connected_raw" \
        --arg stateRaw "$state_raw" '
        {
            exists: $exists,
            interfaceName: $interfaceName,
            description: $description,
            type: $type,
            protocol: $protocol,
            upstream: $upstream,
            link: $link,
            connected: $connected,
            state: $state,
            mtu: $mtu,
            via: $via,
            localAddress: $localAddress,
            remoteAddress: $remoteAddress,
            adminOnly: $adminOnly,
            raw: {
                link: $linkRaw,
                connected: $connectedRaw,
                state: $stateRaw
            }
        }
    '
}

# BROray Keenetic actual state adapter: end

# BROray Keenetic dash comparison: begin

broray_keenetic_normalize_description()
{
    printf '%s' "$1" |
        tr '\r\n\t' '   ' |
        sed \
            -e 's/ / /g' \
            -e 's/‐/-/g' \
            -e 's/-/-/g' \
            -e 's/‒/-/g' \
            -e 's/–/-/g' \
            -e 's/—/-/g' \
            -e 's/―/-/g' \
            -e 's/−/-/g' \
            -e 's/﹘/-/g' \
            -e 's/﹣/-/g' \
            -e 's/－/-/g' \
            -e 's/[[:space:]]*-[[:space:]]*/ - /g' \
            -e 's/[[:space:]][[:space:]]*/ /g' \
            -e 's/^ //' \
            -e 's/ $//'
}

broray_keenetic_status_json()
{
    local expected
    local actual
    local result
    local temp_file
    local expected_description
    local actual_description
    local expected_normalized
    local actual_normalized
    local description_match
    local ownership_receipt_present

    expected="$(
        broray_keenetic_read_expected_json
    )" || return 1

    actual="$(
        broray_keenetic_read_actual_json
    )" || return 1

    expected_description="$(
        printf '%s\n' "$expected" |
            jq -r '.description // ""'
    )"

    actual_description="$(
        printf '%s\n' "$actual" |
            jq -r '.description // ""'
    )"

    expected_normalized="$(
        broray_keenetic_normalize_description \
            "$expected_description"
    )"

    actual_normalized="$(
        broray_keenetic_normalize_description \
            "$actual_description"
    )"

    description_match=false

    if [ "$actual_normalized" = "$expected_normalized" ]; then
        description_match=true
    fi

    result="$(
        jq -n \
            --argjson expected "$expected" \
            --argjson actual "$actual" \
            --argjson descriptionMatch "$description_match" \
            --arg updatedAt "$(broray_keenetic_now)" '
            def check($value; $message):
                if $value then [] else [$message] end;

            ($expected.description // "BROray") as $displayName |
            ($actual.exists) as $exists |

            (
                $expected.ready and
                $exists and
                $actual.protocol == $expected.protocol
            ) as $protocolMatch |

            (
                $expected.ready and
                $exists and
                $actual.upstream == $expected.upstream
            ) as $upstreamMatch |

            ($descriptionMatch) as $descriptionMatch |

            (
                $exists and
                $actual.link
            ) as $linkOk |

            (
                $exists and
                $actual.connected
            ) as $connectedOk |

            (
                $exists and
                $actual.state == "up"
            ) as $stateOk |

            ($expected.socks.active) as $upstreamReachable |

            (
                $expected.ready and
                $exists and
                $protocolMatch and
                $upstreamMatch and
                $descriptionMatch
            ) as $matchesExpected |

            (
                $exists and
                $upstreamReachable and
                $linkOk and
                $connectedOk and
                $stateOk
            ) as $healthy |

            {
                exists: $exists,
                healthy: $healthy,
                matchesExpected: $matchesExpected,
                interfaceName: $expected.interfaceName,
                description: $actual.description,
                protocol: $actual.protocol,
                upstream: $actual.upstream,
                link: $actual.link,
                connected: $actual.connected,
                state: $actual.state,
                updatedAt: $updatedAt,
                expectedReady: $expected.ready,
                upstreamReachable: $upstreamReachable,

                expected: $expected,
                actual: $actual,

                checks: {
                    exists: $exists,
                    protocol: $protocolMatch,
                    upstream: $upstreamMatch,
                    description: $descriptionMatch,
                    upstreamReachable: $upstreamReachable,
                    link: $linkOk,
                    connected: $connectedOk,
                    state: $stateOk
                },

                technical: {
                    type: $actual.type,
                    mtu: $actual.mtu,
                    via: $actual.via,
                    localAddress: $actual.localAddress,
                    remoteAddress: $actual.remoteAddress,
                    adminOnly: $actual.adminOnly
                },

                problems:
                    (
                        check(
                            $expected.ready;
                            "Не получены параметры SOCKS Xray или имя активного сервера."
                        ) +

                        check(
                            $exists;
                            "Интерфейс " + $displayName + " не создан."
                        ) +

                        (
                            if $exists and ($protocolMatch | not)
                            then [
                                "Протокол " + $displayName + " не совпадает с ожидаемым."
                            ]
                            else []
                            end
                        ) +

                        (
                            if $exists and ($upstreamMatch | not)
                            then [
                                "Адрес Xray в " + $displayName + " настроен неправильно."
                            ]
                            else []
                            end
                        ) +

                        (
                            if $exists and ($descriptionMatch | not)
                            then [
                                "Описание " + $displayName + " не совпадает с активным сервером."
                            ]
                            else []
                            end
                        ) +

                        check(
                            $upstreamReachable;
                            "Прокси Xray недоступен."
                        ) +

                        (
                            if $exists
                            then check(
                                $linkOk;
                                "Связь интерфейса " + $displayName + " не установлена."
                            )
                            else []
                            end
                        ) +

                        (
                            if $exists
                            then check(
                                $connectedOk;
                                "Интерфейс " + $displayName + " не подключён."
                            )
                            else []
                            end
                        ) +

                        (
                            if $exists
                            then check(
                                $stateOk;
                                "Интерфейс " + $displayName + " не работает."
                            )
                            else []
                            end
                        )
                    ) |
                    unique
            }
        '
    )" || return 1

    ownership_confirmed=false
    if [ "$(printf '%s\n' "$actual" | jq -r '.exists // false')" = true ] &&
       command -v broray_interface_owner_valid >/dev/null 2>&1 &&
       broray_interface_owner_valid "$BRORAY_KEENETIC_INTERFACE_NAME"
    then
        ownership_confirmed=true
    fi

    ownership_receipt_present=false
    if [ -f "$BRORAY_INTERFACE_OWNER_FILE" ] &&
       [ ! -L "$BRORAY_INTERFACE_OWNER_FILE" ] &&
       jq -e --arg interfaceName "$BRORAY_KEENETIC_INTERFACE_NAME" '
           (.schemaVersion == 1 or .schemaVersion == 2) and
           .owner == "BROray" and
           .interfaceName == $interfaceName
       ' "$BRORAY_INTERFACE_OWNER_FILE" >/dev/null 2>&1
    then
        ownership_receipt_present=true
    fi

    result="$(
        printf '%s\n' "$result" |
            jq \
                --argjson ownershipConfirmed "$ownership_confirmed" \
                --argjson ownershipReceiptPresent "$ownership_receipt_present" \
                --arg interfaceName "$BRORAY_KEENETIC_INTERFACE_NAME" '
                (.expected.description // "BROray") as $displayName |
                .ownershipConfirmed = $ownershipConfirmed |
                .ownershipReceiptPresent = $ownershipReceiptPresent |
                .interfaceType = "managed-socks-proxy" |
                .interfaceDisplayName = $displayName |
                .checks.ownership = (
                    if .exists then $ownershipConfirmed else true end
                ) |
                (if .exists and ($ownershipConfirmed | not) then
                    .healthy = false |
                    .matchesExpected = false |
                    .problems = (
                        (.problems // []) + [
                            "Интерфейс " + $displayName +
                            " не подтверждён как принадлежащий BROray."
                        ] |
                        unique
                    )
                else
                    .
                end) |
                . as $status |
                (
                    if ($status.expectedReady | not) then "error"
                    elif ($status.exists | not) then "error"
                    elif ($status.ownershipConfirmed | not) then "error"
                    elif ($status.healthy | not) then "error"
                    elif ($status.matchesExpected | not) then "warning"
                    else "ok" end
                ) as $severity |
                (
                    [] +
                    (if ($status.expectedReady | not) then [{code:"KEENETIC_EXPECTED_UNAVAILABLE",message:"Не получены параметры SOCKS Xray или активного сервера.",details:null}] else [] end) +
                    (if ($status.exists | not) then [{code:"PROXY0_MISSING",message:("Интерфейс " + $displayName + " не создан."),details:null}] else [] end) +
                    (if $status.exists and ($status.ownershipConfirmed | not) then [{code:"PROXY0_OWNERSHIP_UNCONFIRMED",message:("Интерфейс " + $displayName + " не подтверждён как принадлежащий BROray."),details:null}] else [] end) +
                    (if ($status.upstreamReachable | not) then [{code:"XRAY_SOCKS_UNAVAILABLE",message:"Прокси Xray недоступен.",details:null}] else [] end) +
                    (if $status.exists and (($status.checks.protocol | not) or ($status.checks.upstream | not) or ($status.checks.description | not)) then [{code:"PROXY0_CONFIGURATION_MISMATCH",message:("Настройки " + $displayName + " отличаются от ожидаемых."),details:null}] else [] end) +
                    (if $status.exists and (($status.checks.link | not) or ($status.checks.connected | not) or ($status.checks.state | not)) then [{code:"PROXY0_DOWN",message:("Интерфейс " + $displayName + " не находится в рабочем состоянии."),details:null}] else [] end)
                ) as $reasons |
                .health = {
                    schemaVersion:1,
                    module:"keenetic",
                    availability:"available",
                    severity:$severity,
                    operational:($status.healthy == true),
                    consistent:($status.matchesExpected == true and ($status.exists | not or $status.ownershipConfirmed == true)),
                    actionRequired:($severity != "ok"),
                    freshness:{state:"fresh",checkedAt:($status.updatedAt // null)},
                    reasons:($reasons | unique_by(.code)),
                    facts:{
                        interfaceName:$interfaceName,
                        exists:($status.exists // false),
                        ownershipConfirmed:($status.ownershipConfirmed // false),
                        ownershipReceiptPresent:($status.ownershipReceiptPresent // false),
                        socksActive:($status.upstreamReachable // false),
                        link:($status.link // false),
                        connected:($status.connected // false),
                        state:($status.state // null),
                        matchesExpected:($status.matchesExpected // false)
                    },
                    lastOperation:null
                }
            '
    )" || return 1

    mkdir -p "$BRORAY_KEENETIC_RUN_DIR" ||
        return 1

    temp_file="$BRORAY_KEENETIC_STATUS_FILE.tmp.$$"

    printf '%s\n' "$result" >"$temp_file" ||
        return 1

    mv "$temp_file" "$BRORAY_KEENETIC_STATUS_FILE" ||
        return 1

    printf '%s\n' "$result"
}

# BROray Keenetic dash comparison: end


# BROray ownership-safe description sync.
broray_keenetic_sync_description()
{
    local output rc

    if [ ! -r "$BRORAY_KEENETIC_INTERFACE_SCRIPT" ]; then
        printf '%s\n' 'Низкоуровневый модуль управляемого ProxyN недоступен.' >&2
        return 1
    fi

    output="$(ash "$BRORAY_KEENETIC_INTERFACE_SCRIPT" sync-name 2>&1)"
    rc=$?
    if [ "$rc" -ne 0 ]; then
        printf '%s\n' "$output" >&2
        return "$rc"
    fi
}

BRORAY_KEENETIC_STATUS_CACHE_SECONDS="${BRORAY_KEENETIC_STATUS_CACHE_SECONDS:-30}"

broray_keenetic_status_json_cached()
{
    keenetic_cache_now="$(date '+%s')"
    keenetic_cache_epoch=0
    keenetic_cache_age=0

    if [ -s "$BRORAY_KEENETIC_STATUS_FILE" ] &&
       [ ! -L "$BRORAY_KEENETIC_STATUS_FILE" ] &&
       broray_keenetic_json_valid "$BRORAY_KEENETIC_STATUS_FILE"
    then
        keenetic_cache_epoch="$(
            find -P "$BRORAY_KEENETIC_STATUS_FILE" -maxdepth 0 -printf '%T@\n' 2>/dev/null |
                awk -F. 'NR==1{print $1;exit}'
        )"
        case "$keenetic_cache_epoch" in ''|*[!0-9]*) keenetic_cache_epoch=0 ;; esac
        if [ "$keenetic_cache_epoch" -gt 0 ]; then
            keenetic_cache_age=$((keenetic_cache_now - keenetic_cache_epoch))
            [ "$keenetic_cache_age" -ge 0 ] || keenetic_cache_age=0
            if [ "$keenetic_cache_age" -lt "$BRORAY_KEENETIC_STATUS_CACHE_SECONDS" ]; then
                cat "$BRORAY_KEENETIC_STATUS_FILE"
                return 0
            fi
        fi
    fi

    broray_keenetic_status_json
}
