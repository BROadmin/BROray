#!/opt/bin/ash

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"
BRORAY_ROOT="$BRORAY_BASE"
export BRORAY_ROOT

. "$BRORAY_BASE/lib/util.sh"
. "$BRORAY_BASE/lib/server.sh"
. "$BRORAY_BASE/lib/server-config-generator.sh"
. "$BRORAY_BASE/lib/server-xray-manager.sh"
. "$BRORAY_BASE/lib/xray.sh"
. "$BRORAY_BASE/lib/status-contract.sh"
. "$BRORAY_BASE/lib/server-check-job.sh"

BRORAY_SERVERS="$BRORAY_BASE/servers"
BRORAY_QUALITY_DIR="$BRORAY_BASE/run/server-quality"
BRORAY_ACTIVE_SERVER_FILE="$BRORAY_BASE/config/active-server"
BRORAY_AUTO_SWITCH_FILE="$BRORAY_BASE/config/system/server-auto-switch.json"
BRORAY_ROUTE_STATE="$BRORAY_BASE/run/xray-server-route"
BRORAY_INTERFACE_STATUS="$BRORAY_BASE/run/interface-status.json"
BRORAY_INIT="${BRORAY_INIT:-/opt/etc/init.d/S24broray}"
BRORAY_CONNECTION_STATUS="${BRORAY_CONNECTION_STATUS:-$BRORAY_BASE/run/connection-status.json}"
BRORAY_KEENETIC_STATUS="${BRORAY_KEENETIC_STATUS:-$BRORAY_BASE/run/keenetic-status.json}"
broray_quality_refresh_enabled=false
broray_quality_refresh_interval_minutes=60
if [ -s "$BRORAY_AUTO_SWITCH_FILE" ] &&
   broray_quality_refresh_policy="$(jq -er '
       select(type == "object") |
       [
           (if (.qualityRefreshEnabled | type) == "boolean"
            then .qualityRefreshEnabled else false end),
           (.qualityRefreshIntervalMinutes // 60)
       ] |
       map(tostring) | join(":")
   ' "$BRORAY_AUTO_SWITCH_FILE" 2>/dev/null)"
then
    broray_quality_refresh_enabled="${broray_quality_refresh_policy%%:*}"
    broray_quality_refresh_interval_minutes="${broray_quality_refresh_policy#*:}"
fi
case "$broray_quality_refresh_enabled" in
    true) ;;
    *) broray_quality_refresh_enabled=false ;;
esac
case "$broray_quality_refresh_interval_minutes" in
    30|60|180|360) ;;
    *) broray_quality_refresh_interval_minutes=60 ;;
esac

broray_quality_stale_default=1800
broray_quality_expired_default=10800
if [ "$broray_quality_refresh_enabled" = true ]; then
    broray_quality_refresh_interval_seconds=$((
        broray_quality_refresh_interval_minutes * 60
    ))
    # Keep measurements current through the configured interval and allow the
    # sequential full-list probe to finish before reporting delayed data.
    broray_quality_stale_default=$((
        broray_quality_refresh_interval_seconds + 900
    ))
    broray_quality_expired_default=$((
        broray_quality_refresh_interval_seconds * 3
    ))
    [ "$broray_quality_expired_default" -ge 10800 ] ||
        broray_quality_expired_default=10800
fi
BRORAY_SERVER_QUALITY_STALE_SECONDS="${BRORAY_SERVER_QUALITY_STALE_SECONDS:-$broray_quality_stale_default}"
BRORAY_SERVER_QUALITY_EXPIRED_SECONDS="${BRORAY_SERVER_QUALITY_EXPIRED_SECONDS:-$broray_quality_expired_default}"
BRORAY_CONNECTION_STALE_SECONDS="${BRORAY_CONNECTION_STALE_SECONDS:-60}"
BRORAY_CONNECTION_EXPIRED_SECONDS="${BRORAY_CONNECTION_EXPIRED_SECONDS:-300}"
BRORAY_KEENETIC_STALE_SECONDS="${BRORAY_KEENETIC_STALE_SECONDS:-60}"
BRORAY_KEENETIC_EXPIRED_SECONDS="${BRORAY_KEENETIC_EXPIRED_SECONDS:-300}"

broray_server_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_server_refresh_keenetic_status()
{
    refresh_mode="${1:-cached}"
    refresh_keenetic=true
    refresh_epoch=0
    refresh_age=0

    if [ "$refresh_mode" != force ] &&
       [ -s "$BRORAY_KEENETIC_STATUS" ] &&
       jq -e 'type == "object"' "$BRORAY_KEENETIC_STATUS" >/dev/null 2>&1
    then
        refresh_epoch="$(
            broray_status_file_epoch "$BRORAY_KEENETIC_STATUS" 2>/dev/null ||
                printf '0'
        )"
        case "$refresh_epoch" in ''|*[!0-9]*) refresh_epoch=0 ;; esac
        if [ "$refresh_epoch" -gt 0 ]; then
            refresh_age="$(
                broray_status_age_seconds "$refresh_epoch" "$(date '+%s')" 2>/dev/null ||
                    printf '0'
            )"
            if [ "$refresh_age" -lt "$BRORAY_KEENETIC_STALE_SECONDS" ]; then
                refresh_keenetic=false
            fi
        fi
    fi

    [ "$refresh_keenetic" = true ] || return 0
    [ -r "$BRORAY_BASE/lib/keenetic-page.sh" ] || return 1

    (
        . "$BRORAY_BASE/lib/keenetic-page.sh"
        broray_keenetic_status_json >/dev/null
    )
}

broray_server_quality_path()
{
    quality_server_id="$1"

    broray_server_validate_id "$quality_server_id"

    printf '%s/%s.json\n' \
        "$BRORAY_QUALITY_DIR" \
        "$quality_server_id"
}

broray_server_mask_json()
{
    mask_file="$1"

    jq '
        del(
            .uri,
            .uuid,
            .password,
            .auth,
            .privateKey,
            .connection.uuid,
            .connection.password,
            .connection.auth,
            .connection.privateKey,
            .hysteria.obfsPassword
        )
        |
        if .reality.publicKey? then
            .reality.publicKey =
                (
                    if (.reality.publicKey | length) > 12
                    then
                        (.reality.publicKey[0:6] + "…" +
                        .reality.publicKey[-4:])
                    else "••••••"
                    end
                )
        else .
        end
    ' "$mask_file"
}

broray_server_get_quality()
{
    quality_server_id="$1"
    quality_file="$(broray_server_quality_path "$quality_server_id")"
    quality_now="$(date '+%s')"
    quality_epoch=0
    quality_age=null
    quality_freshness=unknown

    if [ -f "$quality_file" ] &&
       jq -e 'type == "object"' "$quality_file" >/dev/null 2>&1
    then
        quality_epoch="$(broray_status_file_epoch "$quality_file" 2>/dev/null || printf '0')"
        case "$quality_epoch" in ''|*[!0-9]*) quality_epoch=0 ;; esac
        if [ "$quality_epoch" -gt 0 ]; then
            quality_age="$(broray_status_age_seconds "$quality_epoch" "$quality_now" 2>/dev/null || printf '0')"
            quality_freshness="$(broray_status_freshness_from_age "$quality_age" "$BRORAY_SERVER_QUALITY_STALE_SECONDS" "$BRORAY_SERVER_QUALITY_EXPIRED_SECONDS")"
        fi

        jq \
            --arg freshness "$quality_freshness" \
            --argjson ageSeconds "$quality_age" '
            . + {
                freshness:$freshness,
                ageSeconds:$ageSeconds,
                stale:($freshness == "stale" or $freshness == "expired")
            }
            | .measurementMethod = (.measurementMethod // "https-proxy+icmp")
            | .measurementSource = (.measurementSource // "unknown")
        ' "$quality_file"
    else
        jq -n '{
            status:"unknown",
            ping:null,
            jitter:null,
            disconnects:0,
            successfulChecks:0,
            failedChecks:0,
            lastCheckedAt:null,
            lastSuccessAt:null,
            durationMs:null,
            rating:"unknown",
            error:null,
            measurementMethod:"https-proxy+icmp",
            measurementSource:"unknown",
            freshness:"unknown",
            ageSeconds:null,
            stale:true
        }'
    fi
}

broray_server_is_xray_running()
{
    pidof xray >/dev/null 2>&1
}

broray_server_summary()
{
    mkdir -p "$BRORAY_QUALITY_DIR" "$BRORAY_BASE/tmp"
    if [ "${BRORAY_SERVER_SKIP_KEENETIC_REFRESH:-false}" != true ]; then
        broray_server_refresh_keenetic_status >/dev/null 2>&1 || true
    fi

    active_server_id=""
    [ -f "$BRORAY_ACTIVE_SERVER_FILE" ] && active_server_id="$(sed -n '1p' "$BRORAY_ACTIVE_SERVER_FILE")"

    servers_jsonl="$BRORAY_BASE/tmp/server-summary-servers.$$.jsonl"
    servers_array="$BRORAY_BASE/tmp/server-summary-servers.$$.json"
    default_quality="$BRORAY_BASE/tmp/server-summary-quality-default.$$.json"
    : >"$servers_jsonl"
    printf '%s\n' '{}' >"$default_quality"

    for server_file in "$BRORAY_SERVERS"/*.json; do
        [ -f "$server_file" ] || continue

        server_id="${server_file##*/}"
        server_id="${server_id%.json}"
        broray_server_validate_id "$server_id"

        quality_file="$(broray_server_quality_path "$server_id")"
        quality_source="$default_quality"
        quality_age=null
        quality_freshness=unknown

        if [ -f "$quality_file" ]; then
            quality_source="$quality_file"
            quality_epoch="$(broray_status_file_epoch "$quality_file" 2>/dev/null || printf '0')"
            case "$quality_epoch" in ''|*[!0-9]*) quality_epoch=0 ;; esac
            if [ "$quality_epoch" -gt 0 ]; then
                quality_age="$(broray_status_age_seconds "$quality_epoch" "$(date '+%s')" 2>/dev/null || printf '0')"
                quality_freshness="$(broray_status_freshness_from_age "$quality_age" "$BRORAY_SERVER_QUALITY_STALE_SECONDS" "$BRORAY_SERVER_QUALITY_EXPIRED_SECONDS")"
            fi
        fi

        # One jq process reads both the server and the optional quality record.
        # Invalid legacy quality JSON is treated as unknown; the server record
        # itself remains fail-closed and must match its validated filename ID.
        jq -nc \
            --slurpfile server "$server_file" \
            --rawfile quality "$quality_source" \
            --arg serverId "$server_id" \
            --arg activeServerId "$active_server_id" \
            --arg freshness "$quality_freshness" \
            --argjson ageSeconds "$quality_age" '
            ($server[0]) as $s |
            ($quality | fromjson? // {}) as $q |
            if (($s.id // "") != $serverId) then
                error("server id does not match filename")
            else
                {
                    id:$s.id,
                    name:($s.name // $s.id),
                    address:$s.address,
                    port:$s.port,
                    protocol:$s.protocol,
                    transport:($s.network // $s.transport.type // "unknown"),
                    security:($s.security // "none"),
                    sourceType:($s.source.type // "manual"),
                    subscriptionId:($s.source.subscriptionId // null),
                    nodeIndex:($s.source.nodeIndex // null),
                    active:($s.id == $activeServerId),
                    quality:(
                        {
                            status:"unknown",
                            ping:null,
                            jitter:null,
                            disconnects:0,
                            successfulChecks:0,
                            failedChecks:0,
                            lastCheckedAt:null,
                            lastSuccessAt:null,
                            durationMs:null,
                            rating:"unknown",
                            error:null,
                            measurementMethod:"https-proxy+icmp",
                            measurementSource:"unknown"
                        }
                        + $q
                        + {
                            freshness:$freshness,
                            ageSeconds:$ageSeconds,
                            stale:($freshness == "stale" or $freshness == "expired"),
                            measurementMethod:($q.measurementMethod // "https-proxy+icmp"),
                            measurementSource:($q.measurementSource // "unknown")
                        }
                    )
                }
            end
        ' >>"$servers_jsonl" ||
            broray_die "не удалось сформировать данные сервера $server_id"
    done

    jq -s '.' "$servers_jsonl" >"$servers_array" ||
        broray_die 'не удалось сформировать список серверов'
    rm -f "$servers_jsonl" "$default_quality"

    set -- $(jq -r '
        [
            length,
            ([.[] | select(.quality.status == "available")] | length),
            ([.[] | select(.quality.status == "unavailable")] | length),
            ([.[] | select(.quality.freshness == "fresh")] | length),
            ([.[] | select(.quality.freshness == "stale")] | length),
            ([.[] | select(.quality.freshness == "expired")] | length),
            ([.[] | select(.quality.freshness == "unknown")] | length)
        ] | @tsv
    ' "$servers_array") || broray_die 'не удалось подсчитать состояние серверов'

    total="${1:-0}"
    available="${2:-0}"
    unavailable="${3:-0}"
    quality_fresh="${4:-0}"
    quality_stale="${5:-0}"
    quality_expired="${6:-0}"
    quality_unknown="${7:-0}"

    active_json=null
    [ -n "$active_server_id" ] && active_json="$(jq --arg id "$active_server_id" 'first(.[] | select(.id == $id)) // null' "$servers_array")"
    active_present=false
    [ "$active_json" != null ] && active_present=true

    if [ -f "$BRORAY_AUTO_SWITCH_FILE" ] && auto_switch_json="$(jq -ce '
            select(type == "object") |
            .selectionRuleLabel = (
                if .selectionRule == "best-quality" then "Лучшее качество"
                elif .selectionRule == "lowest-ping" then "Минимальная задержка"
                elif .selectionRule == "preferred" then "Предпочитаемый сервер"
                else "Ручной выбор" end
            )
        ' "$BRORAY_AUTO_SWITCH_FILE" 2>/dev/null)"; then
        :
    else
        auto_switch_json='{"enabled":false,"selectionRule":"manual","selectionRuleLabel":"Ручной выбор"}'
    fi

    # The Servers page needs live path readiness, not a repeated integrity scan
    # of the 12 MiB Xray binary. Full checks remain on the Xray page and in the
    # updater health gate. Process and listener probes are authoritative here.
    xray_running=false
    pidof xray >/dev/null 2>&1 && xray_running=true
    socks_address="$(broray_xray_socks_address)"
    socks_port="$(broray_xray_socks_port)"
    socks_active=false
    if [ "$xray_running" = true ] &&
       broray_xray_socks_active "$socks_address" "$socks_port"
    then
        socks_active=true
    fi
    xray_operational=false
    [ "$xray_running" = true ] && [ "$socks_active" = true ] && xray_operational=true

    now_epoch="$(date '+%s')"
    connection_available=false
    connection_up=false
    connection_freshness=unknown
    connection_age=null
    if [ -s "$BRORAY_CONNECTION_STATUS" ] && connection_row="$(jq -er '
        select(type == "object") |
        [(.available // false),(.up // .connected // .healthy // false),(.checked_at // 0)] |
        map(tostring) | @tsv
    ' "$BRORAY_CONNECTION_STATUS" 2>/dev/null)"; then
        saved_ifs="$IFS"
        IFS="$(printf '\t')"
        set -- $connection_row
        IFS="$saved_ifs"
        connection_available="${1:-false}"
        connection_up="${2:-false}"
        connection_epoch="${3:-0}"
        case "$connection_epoch" in ''|*[!0-9]*) connection_epoch=0 ;; esac
        if [ "$connection_epoch" -gt 0 ]; then
            connection_age="$(broray_status_age_seconds "$connection_epoch" "$now_epoch" 2>/dev/null || printf '0')"
            connection_freshness="$(broray_status_freshness_from_age "$connection_age" "$BRORAY_CONNECTION_STALE_SECONDS" "$BRORAY_CONNECTION_EXPIRED_SECONDS")"
        fi
    fi

    keenetic_exists=false
    keenetic_healthy=false
    keenetic_consistent=false
    keenetic_freshness=unknown
    keenetic_age=null
    if [ -s "$BRORAY_KEENETIC_STATUS" ] && keenetic_row="$(jq -er '
        select(type == "object") |
        [(.exists // false),(.healthy // false),(.matchesExpected // false)] |
        map(tostring) | @tsv
    ' "$BRORAY_KEENETIC_STATUS" 2>/dev/null)"; then
        saved_ifs="$IFS"
        IFS="$(printf '\t')"
        set -- $keenetic_row
        IFS="$saved_ifs"
        keenetic_exists="${1:-false}"
        keenetic_healthy="${2:-false}"
        keenetic_consistent="${3:-false}"
        keenetic_epoch="$(broray_status_file_epoch "$BRORAY_KEENETIC_STATUS" 2>/dev/null || printf '0')"
        case "$keenetic_epoch" in ''|*[!0-9]*) keenetic_epoch=0 ;; esac
        if [ "$keenetic_epoch" -gt 0 ]; then
            keenetic_age="$(broray_status_age_seconds "$keenetic_epoch" "$now_epoch" 2>/dev/null || printf '0')"
            keenetic_freshness="$(broray_status_freshness_from_age "$keenetic_age" "$BRORAY_KEENETIC_STALE_SECONDS" "$BRORAY_KEENETIC_EXPIRED_SECONDS")"
        fi
    fi

    connection_current=false
    [ "$connection_freshness" = fresh ] && connection_current=true
    keenetic_current=false
    [ "$keenetic_freshness" = fresh ] && keenetic_current=true

    if [ "$active_present" = true ] &&
       [ "$xray_operational" = true ] &&
       [ "$socks_active" = true ] &&
       [ "$connection_available" = true ] &&
       [ "$connection_up" = true ] &&
       [ "$connection_current" = true ] &&
       [ "$keenetic_healthy" = true ] &&
       [ "$keenetic_current" = true ]
    then
        connection_state=connected
        health_operational=true
        health_consistent="$keenetic_consistent"
    elif [ "$active_present" != true ]; then
        connection_state=disabled
        health_operational=false
        health_consistent=false
    elif [ "$xray_running" = true ] && [ "$socks_active" = true ]; then
        connection_state=degraded
        health_operational=false
        health_consistent=false
    else
        connection_state=error
        health_operational=false
        health_consistent=false
    fi

    health_reasons="$({
        [ "$active_present" = true ] || broray_status_reason ACTIVE_SERVER_MISSING 'Активный сервер не выбран.'
        [ "$xray_operational" = true ] || broray_status_reason XRAY_NOT_OPERATIONAL 'Xray или его локальный SOCKS-интерфейс не готовы.'
        [ "$connection_available" = true ] || broray_status_reason CONNECTION_MONITOR_UNAVAILABLE 'Монитор соединения не вернул состояние.'
        [ "$connection_up" = true ] || broray_status_reason CONNECTION_DOWN 'Соединение через активный сервер не подтверждено.'
        [ "$connection_current" = true ] || broray_status_reason CONNECTION_STATUS_STALE 'Состояние соединения устарело.'
        [ "$keenetic_exists" = true ] || broray_status_reason PROXY0_MISSING 'Управляемый интерфейс ProxyN не создан.'
        [ "$keenetic_healthy" = true ] || broray_status_reason PROXY0_NOT_READY 'Управляемый ProxyN не работает.'
        [ "$keenetic_consistent" = true ] || broray_status_reason PROXY0_MISMATCH 'Управляемый ProxyN не соответствует активному серверу.'
        [ "$keenetic_current" = true ] || broray_status_reason PROXY0_STATUS_STALE 'Проверка управляемого ProxyN устарела.'
        [ "$quality_unknown" -eq 0 ] || broray_status_reason QUALITY_INCOMPLETE 'Не для всех серверов выполнена проверка качества.'
        [ "$quality_stale" -eq 0 ] || broray_status_reason QUALITY_STALE 'Часть измерений качества устарела.'
        [ "$quality_expired" -eq 0 ] || broray_status_reason QUALITY_EXPIRED 'Часть измерений качества просрочена.'
    } | jq -sc '.')"

    # A connection probe and a router snapshot may be temporarily stale while
    # the independently verified Xray/SOCKS path remains operational.  That is
    # a warning, not a module failure.  Reserve error for a missing/failed
    # active proxy path; this keeps Home and Servers consistent with Keenetic.
    if [ "$active_present" = true ] && [ "$xray_operational" != true ]; then
        health_severity=error
    elif [ "$health_operational" != true ] ||
         [ "$health_consistent" != true ] ||
         [ "$quality_unknown" -gt 0 ] ||
         [ "$quality_stale" -gt 0 ] ||
         [ "$quality_expired" -gt 0 ]
    then
        health_severity=warning
    else
        health_severity=ok
    fi
    health_action_required=false
    [ "$health_severity" = ok ] || health_action_required=true
    updated_at="$(broray_server_now)"

    health_facts="$(jq -nc \
        --arg connectionState "$connection_state" \
        --argjson activeServerPresent "$active_present" \
        --argjson xrayOperational "$xray_operational" \
        --argjson socksActive "$socks_active" \
        --argjson connectionAvailable "$connection_available" \
        --argjson connectionUp "$connection_up" \
        --arg connectionFreshness "$connection_freshness" \
        --argjson proxy0Healthy "$keenetic_healthy" \
        --argjson proxy0Consistent "$keenetic_consistent" \
        --arg proxy0Freshness "$keenetic_freshness" \
        --argjson qualityFresh "$quality_fresh" \
        --argjson qualityStale "$quality_stale" \
        --argjson qualityExpired "$quality_expired" \
        --argjson qualityUnknown "$quality_unknown" \
        '{connectionState:$connectionState,activeServerPresent:$activeServerPresent,xrayOperational:$xrayOperational,socksActive:$socksActive,connection:{available:$connectionAvailable,up:$connectionUp,freshness:$connectionFreshness},proxy0:{healthy:$proxy0Healthy,consistent:$proxy0Consistent,freshness:$proxy0Freshness},quality:{fresh:$qualityFresh,stale:$qualityStale,expired:$qualityExpired,unknown:$qualityUnknown}}')"
    health_json="$(broray_status_contract servers available "$health_severity" "$health_operational" "$health_consistent" "$health_action_required" "$(broray_status_freshness_worst "$connection_freshness" "$keenetic_freshness")" "$updated_at" "$health_reasons" "$health_facts" null)"

    jq -n \
        --argjson total "$total" \
        --argjson available "$available" \
        --argjson unavailable "$unavailable" \
        --argjson servers "$(cat "$servers_array")" \
        --argjson activeServer "$active_json" \
        --argjson autoSwitch "$auto_switch_json" \
        --argjson xrayRunning "$xray_running" \
        --argjson socksActive "$socks_active" \
        --arg connectionState "$connection_state" \
        --argjson connectionAvailable "$connection_available" \
        --argjson connectionUp "$connection_up" \
        --arg connectionFreshness "$connection_freshness" \
        --argjson keeneticHealthy "$keenetic_healthy" \
        --argjson keeneticConsistent "$keenetic_consistent" \
        --arg keeneticFreshness "$keenetic_freshness" \
        --argjson health "$health_json" \
        --arg updatedAt "$updated_at" '
        {
            total:$total,
            available:$available,
            unavailable:$unavailable,
            connectionState:$connectionState,
            xrayRunning:$xrayRunning,
            socksActive:$socksActive,
            connectionMonitor:{available:$connectionAvailable,up:$connectionUp,freshness:$connectionFreshness},
            keenetic:{healthy:$keeneticHealthy,consistent:$keeneticConsistent,freshness:$keeneticFreshness},
            activeServer:$activeServer,
            servers:$servers,
            autoSwitch:$autoSwitch,
            qualityPolicy:{
                method:"https-proxy+icmp",
                throughputMeasured:false,
                listLoadProbes:false,
                automaticProbe:"on-active-failure"
            },
            health:$health,
            updatedAt:$updatedAt
        }
    '

    rm -f "$servers_array"
}

broray_server_details()
{
    details_server_id="$1"

    broray_server_validate_id "$details_server_id"
    broray_server_exists "$details_server_id" ||
        broray_die \
            "сервер $details_server_id не найден"

    details_server_file="$(
        broray_server_path "$details_server_id"
    )"

    active_server_id=""

    if [ -f "$BRORAY_ACTIVE_SERVER_FILE" ]; then
        active_server_id="$(
            sed -n '1p' "$BRORAY_ACTIVE_SERVER_FILE"
        )"
    fi

    quality_json="$(
        broray_server_get_quality "$details_server_id"
    )"

    broray_server_mask_json "$details_server_file" |
        jq \
            --arg activeServerId "$active_server_id" \
            --argjson quality "$quality_json" '
            . + {
                active: (.id == $activeServerId),
                quality: $quality
            }
        '
}

broray_server_measure()
{
    [ "${BRORAY_OPS_SUPERVISED:-}" = ptrace/1 ] && [ -n "${BRORAY_SERVER_CHECK_TMP:-}" ] || return 73
    check_server_id="$1"
    check_source="${2:-manual}"

    case "$check_source" in
        manual|auto-switch|scheduled) ;;
        *) broray_die "неподдерживаемый источник проверки сервера" ;;
    esac

    broray_server_validate_id "$check_server_id"
    broray_server_exists "$check_server_id" ||
        broray_die \
            "сервер $check_server_id не найден"

    mkdir -p \
        "$BRORAY_QUALITY_DIR" \
        "$BRORAY_BASE/tmp"

    check_server_file="$(
        broray_server_path "$check_server_id"
    )"

    check_address="$(
        jq -r '.address // empty' "$check_server_file"
    )"

    check_port="$(
        jq -r '.port // empty' "$check_server_file"
    )"

    check_started="$(
        date '+%s'
    )"

    check_success=false
    check_stage="validation"
    check_error=""
    generated_config=""

    if ! broray_server_validate "$check_server_file" 2>"$BRORAY_SERVER_CHECK_TMP/server-check-error"; then
        check_error="$(
            cat "$BRORAY_SERVER_CHECK_TMP/server-check-error"
        )"
    else
        check_stage="xray-config"

        if generated_config="$(
            broray_generate_server_config "$check_server_id" \
                2>"$BRORAY_SERVER_CHECK_TMP/server-check-error"
        )"; then
            # BRORAY_REAL_PROXY_PROBE_V1
            if broray_xray_test_file "$generated_config" \
                >"$BRORAY_SERVER_CHECK_TMP/server-check-output" \
                2>&1; then
                check_stage="proxy-https"

                check_probe_json="$(
                    "${BRORAY_OPS_ASH:-/opt/bin/ash}" "$BRORAY_BASE/bin/broray-server-probe" \
                        "$generated_config" \
                        "$check_server_id" \
                        2>"$BRORAY_SERVER_CHECK_TMP/server-probe-error"
                )" || true

                if printf '%s\n' "$check_probe_json" |
                    jq -e \
                        '.success == true' \
                        >/dev/null 2>&1
                then
                    check_success=true

                    check_stage="$(
                        printf '%s\n' "$check_probe_json" |
                            jq -r \
                                '.stage // "proxy-https"'
                    )"
                else
                    if printf '%s\n' "$check_probe_json" |
                        jq -e \
                            'type == "object"' \
                            >/dev/null 2>&1
                    then
                        check_stage="$(
                            printf '%s\n' "$check_probe_json" |
                                jq -r \
                                    '.stage // "proxy-https"'
                        )"

                        check_error="$(
                            printf '%s\n' "$check_probe_json" |
                                jq -r '
                                    .error //
                                    "Реальный запрос через сервер завершился ошибкой."
                                '
                        )"
                    else
                        check_error="$(
                            cat \
                                "$BRORAY_SERVER_CHECK_TMP/server-probe-error" \
                                2>/dev/null
                        )"

                        [ -n "$check_error" ] ||
                            check_error="Пробник вернул некорректный результат."
                    fi
                fi

                rm -f \
                    "$BRORAY_SERVER_CHECK_TMP/server-probe-error"
            else
                check_error="$(
                    cat "$BRORAY_SERVER_CHECK_TMP/server-check-output"
                )"
            fi
        else
            check_error="$(
                cat "$BRORAY_SERVER_CHECK_TMP/server-check-error"
            )"
        fi
    fi

    ping_value="null"
    jitter_value="null"

    if [ "$check_success" = true ] &&
       [ -n "$check_address" ]; then
        ping_output="$(
            ping -c 3 "$check_address" 2>/dev/null || true
        )"

        ping_average="$(
            printf '%s\n' "$ping_output" |
                awk -F'=' '
                    /min\/avg\/max/ {
                        gsub(/[[:space:]]/, "", $2)
                        split($2, values, "/")
                        print int(values[2] + 0.5)
                        exit
                    }
                '
        )"

        ping_minimum="$(
            printf '%s\n' "$ping_output" |
                awk -F'=' '
                    /min\/avg\/max/ {
                        gsub(/[[:space:]]/, "", $2)
                        split($2, values, "/")
                        print int(values[1] + 0.5)
                        exit
                    }
                '
        )"

        ping_maximum="$(
            printf '%s\n' "$ping_output" |
                awk -F'=' '
                    /min\/avg\/max/ {
                        gsub(/[[:space:]]/, "", $2)
                        split($2, values, "/")
                        print int(values[3] + 0.5)
                        exit
                    }
                '
        )"

        case "$ping_average" in
            ''|*[!0-9]*)
                ;;
            *)
                ping_value="$ping_average"

                case "$ping_minimum:$ping_maximum" in
                    *[!0-9:]*|'':*)
                        ;;
                    *)
                        jitter_value=$((ping_maximum - ping_minimum))
                        ;;
                esac
                ;;
        esac
    fi

    check_finished="$(
        date '+%s'
    )"

    duration_ms=$(((check_finished - check_started) * 1000))
    checked_at="$(
        broray_server_now
    )"

    old_quality="$(
        broray_server_get_quality "$check_server_id"
    )"

    successful_checks="$(
        printf '%s\n' "$old_quality" |
            jq -r '.successfulChecks // 0'
    )"

    failed_checks="$(
        printf '%s\n' "$old_quality" |
            jq -r '.failedChecks // 0'
    )"

    disconnects="$(
        printf '%s\n' "$old_quality" |
            jq -r '.disconnects // 0'
    )"

    if [ "$check_success" = true ]; then
        successful_checks=$((successful_checks + 1))
        quality_status="available"

        if [ "$ping_value" = "null" ]; then
            quality_rating="acceptable"
        elif [ "$ping_value" -le 80 ] 2>/dev/null; then
            quality_rating="excellent"
        elif [ "$ping_value" -le 150 ] 2>/dev/null; then
            quality_rating="good"
        elif [ "$ping_value" -le 300 ] 2>/dev/null; then
            quality_rating="acceptable"
        else
            quality_rating="poor"
        fi

        last_success_at="$checked_at"
        error_json="null"
    else
        failed_checks=$((failed_checks + 1))
        quality_status="unavailable"
        quality_rating="unavailable"
        last_success_at="$(
            printf '%s\n' "$old_quality" |
                jq -r '.lastSuccessAt // empty'
        )"

        if [ -n "$check_error" ]; then
            error_json="$(
                jq -Rn \
                    --arg value "$check_error" \
                    '$value'
            )"
        else
            error_json='"Проверка завершилась ошибкой"'
        fi
    fi

    quality_file="$(
        broray_server_quality_path "$check_server_id"
    )"

    jq -n \
        --arg status "$quality_status" \
        --argjson ping "$ping_value" \
        --argjson jitter "$jitter_value" \
        --argjson disconnects "$disconnects" \
        --argjson successfulChecks "$successful_checks" \
        --argjson failedChecks "$failed_checks" \
        --arg lastCheckedAt "$checked_at" \
        --arg lastSuccessAt "$last_success_at" \
        --argjson durationMs "$duration_ms" \
        --arg rating "$quality_rating" \
        --arg measurementMethod "https-proxy+icmp" \
        --arg measurementSource "$check_source" \
        --argjson error "$error_json" '
        {
            status: $status,
            ping: $ping,
            jitter: $jitter,
            disconnects: $disconnects,
            successfulChecks: $successfulChecks,
            failedChecks: $failedChecks,
            lastCheckedAt: $lastCheckedAt,
            lastSuccessAt: (
                if $lastSuccessAt == ""
                then null
                else $lastSuccessAt
                end
            ),
            durationMs: $durationMs,
            rating: $rating,
            measurementMethod: $measurementMethod,
            measurementSource: $measurementSource,
            error: $error
        }
    ' > "$quality_file" ||
        broray_die \
            "не удалось сохранить результат проверки"

    chmod 600 "$quality_file"

    rm -f \
        "$generated_config" \
        "$BRORAY_SERVER_CHECK_TMP/server-check-error" \
        "$BRORAY_SERVER_CHECK_TMP/server-check-output"

    jq -n \
        --arg serverId "$check_server_id" \
        --argjson success "$check_success" \
        --arg stage "$check_stage" \
        --argjson quality "$(cat "$quality_file")" \
        --arg checkedAt "$checked_at" '
        {
            serverId: $serverId,
            success: $success,
            stage: $stage,
            quality: $quality,
            checkedAt: $checkedAt
        }
    '

    [ "$check_success" = true ]
}

broray_server_activate()
{
    local activate_server_id activate_job_dir activate_job_rc
    broray_job_require_owner || return $?
    activate_server_id="$1"

    broray_server_validate_id "$activate_server_id"
    broray_server_exists "$activate_server_id" ||
        broray_die \
            "сервер $activate_server_id не найден"

    if [ "${BRORAY_JOB_UNRESOLVED:-false}" = true ]; then
        # Auto-switch also calls activation while rolling back an already
        # changed runtime. That domain transaction must never become
        # cooperative, even when its replacement config is being validated.
        broray_job_checkpoint committing || return $?
        broray_xray_apply_server "$activate_server_id" || return $?
        BRORAY_JOB_UNRESOLVED=false
    else
    broray_job_checkpoint checking || return $?
    mkdir -p "$BRORAY_BASE/tmp" || return 1
    activate_job_dir="$(mktemp -d "$BRORAY_BASE/tmp/server-activate-$BRORAY_BACKGROUND_OPERATION_ID-XXXXXX")" || return 1
    chmod 700 "$activate_job_dir" || return 1
    printf '%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >"$activate_job_dir/operation-id" || return 1
    activate_job_rc=0
    broray_ops_run_helper 30 -- "${BRORAY_OPS_ASH:-/opt/bin/ash}" \
      "$BRORAY_BASE/lib/server-activate-prepare.sh" "$activate_job_dir" "$activate_server_id" || activate_job_rc=$?
    if [ "$activate_job_rc" = 75 ]; then BRORAY_JOB_UNRESOLVED=true; return 75; fi
    if [ "$activate_job_rc" != 0 ]; then rm -rf "$activate_job_dir"; return "$activate_job_rc"; fi
    [ -f "$activate_job_dir/config.json" ] && [ ! -L "$activate_job_dir/config.json" ] || return 1
    broray_job_checkpoint committing || { activate_job_rc=$?; rm -rf "$activate_job_dir"; return "$activate_job_rc"; }
    BRORAY_JOB_UNRESOLVED=true
    broray_xray_apply_prepared_server "$activate_server_id" "$activate_job_dir/config.json" || return $?
    BRORAY_JOB_UNRESOLVED=false
    rm -rf "$activate_job_dir" || return 1
    fi

    # The activation response must describe the server that was just applied,
    # not a still-fresh cache entry captured for the previous active server.
    broray_server_refresh_keenetic_status force >/dev/null 2>&1 || true

    broray_server_summary
}

broray_server_disable_auto_switch()
{
    mkdir -p "$BRORAY_BASE/config/system"

    updated_at="$(
        broray_server_now
    )"

    if [ -f "$BRORAY_AUTO_SWITCH_FILE" ] &&
       jq -e . "$BRORAY_AUTO_SWITCH_FILE" >/dev/null 2>&1; then
        jq \
            --arg updatedAt "$updated_at" '
            .enabled = false
            |
            .updatedAt = $updatedAt
        ' "$BRORAY_AUTO_SWITCH_FILE" \
            > "$BRORAY_AUTO_SWITCH_FILE.new" ||
            broray_die \
                "не удалось выключить автоматическое переключение"
    else
        jq -n \
            --arg updatedAt "$updated_at" '{
                schemaVersion: 1,
                enabled: false,
                failureThreshold: 3,
                cooldownMinutes: 10,
                minimumRating: "acceptable",
                selectionRule: "manual",
                preferredServerId: null,
                updatedAt: $updatedAt
            }' > "$BRORAY_AUTO_SWITCH_FILE.new" ||
            broray_die \
                "не удалось создать настройки автоматического переключения"
    fi

    mv "$BRORAY_AUTO_SWITCH_FILE.new" \
        "$BRORAY_AUTO_SWITCH_FILE"

    chmod 600 "$BRORAY_AUTO_SWITCH_FILE"
}

broray_server_remove_route()
{
    [ -f "$BRORAY_ROUTE_STATE" ] || return 0

    route_ip=""
    route_interface=""

    read route_ip route_interface < "$BRORAY_ROUTE_STATE" || true

    if [ -n "$route_ip" ] &&
       [ -n "$route_interface" ]; then
        ip route del "$route_ip/32" \
            dev "$route_interface" \
            2>/dev/null || true
    fi

    rm -f "$BRORAY_ROUTE_STATE"
}

broray_server_deactivate()
{
    broray_job_checkpoint committing || return $?
    BRORAY_JOB_UNRESOLVED=true
    previous_active_id=""

    if [ -f "$BRORAY_ACTIVE_SERVER_FILE" ]; then
        previous_active_id="$(
            sed -n '1p' "$BRORAY_ACTIVE_SERVER_FILE"
        )"
    fi

    if broray_server_is_xray_running; then
        "$BRORAY_INIT" stop ||
            broray_die \
                "не удалось остановить Xray"
    fi

    stop_attempt=0

    while broray_server_is_xray_running; do
        stop_attempt=$((stop_attempt + 1))

        if [ "$stop_attempt" -ge 10 ]; then
            broray_die \
                "процесс Xray не остановился"
        fi

        sleep 1
    done

    broray_server_remove_route
    broray_server_disable_auto_switch

    rm -f \
        "$BRORAY_ACTIVE_SERVER_FILE" \
        "$BRORAY_INTERFACE_STATUS" || return 1
    BRORAY_JOB_UNRESOLVED=false

    jq -n \
        --arg previousActiveServerId "$previous_active_id" \
        --arg updatedAt "$(broray_server_now)" '{
            deactivated: true,
            previousActiveServerId: (
                if $previousActiveServerId == ""
                then null
                else $previousActiveServerId
                end
            ),
            connectionState: "disabled",
            autoSwitch: {
                enabled: false
            },
            updatedAt: $updatedAt
        }'
}

broray_server_delete_safe()
{
    broray_job_require_owner || return $?
    delete_server_id="$1"

    broray_server_validate_id "$delete_server_id"
    broray_server_exists "$delete_server_id" ||
        broray_die \
            "сервер $delete_server_id не найден"

    active_server_id=""

    if [ -f "$BRORAY_ACTIVE_SERVER_FILE" ]; then
        active_server_id="$(
            sed -n '1p' "$BRORAY_ACTIVE_SERVER_FILE"
        )"
    fi

    [ "$delete_server_id" != "$active_server_id" ] ||
        broray_die \
            "активный сервер сначала необходимо отключить или заменить"

    delete_server_file="$(
        broray_server_path "$delete_server_id"
    )"

    delete_quality_file="$(
        broray_server_quality_path "$delete_server_id"
    )"

    broray_job_checkpoint committing || return $?
    BRORAY_JOB_UNRESOLVED=true
    rm -f \
        "$delete_server_file" \
        "$delete_quality_file" ||
        broray_die \
            "не удалось удалить сервер $delete_server_id"
    BRORAY_JOB_UNRESOLVED=false

    jq -n \
        --arg id "$delete_server_id" \
        --arg updatedAt "$(broray_server_now)" '{
            deleted: true,
            id: $id,
            updatedAt: $updatedAt
        }'
}

broray_server_publish_snapshot()
{
    local snapshot_file
    broray_job_require_owner || return $?
    "${BRORAY_OPS_ASH:-/opt/bin/ash}" "$BRORAY_BASE/bin/broray-home-snapshot" refresh servers >/dev/null || return 1
    snapshot_file="$BRORAY_BASE/run/home-snapshots/servers.json"
    jq -e 'type=="object" and .module=="servers" and (.capturedAt|type)=="string" and (.data.servers|type)=="array"' "$snapshot_file" >/dev/null || return 1
    jq '{published:true,totalServers:(.data.servers|length),capturedAt:.capturedAt}' "$snapshot_file"
}
