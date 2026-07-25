#!/opt/bin/ash

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"

. "$BRORAY_BASE/lib/util.sh"
. "$BRORAY_BASE/lib/server.sh"
. "$BRORAY_BASE/lib/server-config-generator.sh"
. "$BRORAY_BASE/lib/server-xray-manager.sh"

BRORAY_SERVERS="$BRORAY_BASE/servers"
BRORAY_QUALITY_DIR="$BRORAY_BASE/run/server-quality"
BRORAY_ACTIVE_SERVER_FILE="$BRORAY_BASE/config/active-server"
BRORAY_AUTO_SWITCH_FILE="$BRORAY_BASE/config/system/server-auto-switch.json"
BRORAY_ROUTE_STATE="$BRORAY_BASE/run/xray-server-route"
BRORAY_INTERFACE_STATUS="$BRORAY_BASE/run/interface-status.json"
BRORAY_INIT="${BRORAY_INIT:-/opt/etc/init.d/S24broray}"

broray_server_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
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
    quality_file="$(
        broray_server_quality_path "$quality_server_id"
    )"

    if [ -f "$quality_file" ] &&
       jq -e . "$quality_file" >/dev/null 2>&1; then
        cat "$quality_file"
    else
        jq -n '{
            status: "unknown",
            ping: null,
            jitter: null,
            disconnects: 0,
            successfulChecks: 0,
            failedChecks: 0,
            lastCheckedAt: null,
            lastSuccessAt: null,
            durationMs: null,
            rating: "unknown",
            error: null
        }'
    fi
}

broray_server_is_xray_running()
{
    pidof xray >/dev/null 2>&1
}

broray_server_summary()
{
    mkdir -p "$BRORAY_QUALITY_DIR"

    active_server_id=""

    if [ -f "$BRORAY_ACTIVE_SERVER_FILE" ]; then
        active_server_id="$(
            sed -n '1p' "$BRORAY_ACTIVE_SERVER_FILE"
        )"
    fi

    servers_array="$BRORAY_BASE/tmp/server-summary-servers.$$.json"
    printf '%s\n' '[]' > "$servers_array"

    for server_file in "$BRORAY_SERVERS"/*.json; do
        [ -f "$server_file" ] || continue

        server_id="$(
            jq -r '.id // empty' "$server_file"
        )"

        [ -n "$server_id" ] || continue

        quality_json="$(
            broray_server_get_quality "$server_id"
        )"

        masked_server="$BRORAY_BASE/tmp/server-summary-item.$$.json"

        broray_server_mask_json "$server_file" |
            jq \
                --arg activeServerId "$active_server_id" \
                --argjson quality "$quality_json" '
                {
                    id: .id,
                    name: (.name // .id),
                    address: .address,
                    port: .port,
                    protocol: .protocol,
                    transport: (
                        .network //
                        .transport.type //
                        "unknown"
                    ),
                    security: (.security // "none"),
                    sourceType: (.source.type // "manual"),
                    subscriptionId: (
                        .source.subscriptionId // null
                    ),
                    nodeIndex: (
                        .source.nodeIndex // null
                    ),
                    active: (.id == $activeServerId),
                    quality: $quality
                }
            ' > "$masked_server" ||
            broray_die \
                "не удалось сформировать данные сервера $server_id"

        jq \
            --slurpfile item "$masked_server" \
            '. + [$item[0]]' \
            "$servers_array" \
            > "$servers_array.new" ||
            broray_die \
                "не удалось сформировать список серверов"

        mv "$servers_array.new" "$servers_array"
        rm -f "$masked_server"
    done

    total="$(
        jq 'length' "$servers_array"
    )"

    available="$(
        jq '
            [
                .[]
                | select(
                    .quality.status == "available"
                )
            ]
            | length
        ' "$servers_array"
    )"

    unavailable="$(
        jq '
            [
                .[]
                | select(
                    .quality.status == "unavailable"
                )
            ]
            | length
        ' "$servers_array"
    )"

    active_json="null"

    if [ -n "$active_server_id" ]; then
        active_json="$(
            jq \
                --arg id "$active_server_id" '
                first(
                    .[]
                    | select(.id == $id)
                ) // null
            ' "$servers_array"
        )"
    fi

    if [ -f "$BRORAY_AUTO_SWITCH_FILE" ] &&
       jq -e . "$BRORAY_AUTO_SWITCH_FILE" >/dev/null 2>&1; then
        auto_switch_json="$(
            cat "$BRORAY_AUTO_SWITCH_FILE"
        )"
    else
        auto_switch_json='{"enabled":false,"selectionRule":"manual"}'
    fi

    if broray_server_is_xray_running; then
        xray_running=true
    else
        xray_running=false
    fi

    updated_at="$(
        broray_server_now
    )"

    jq -n \
        --argjson total "$total" \
        --argjson available "$available" \
        --argjson unavailable "$unavailable" \
        --argjson servers "$(cat "$servers_array")" \
        --argjson activeServer "$active_json" \
        --argjson autoSwitch "$auto_switch_json" \
        --argjson xrayRunning "$xray_running" \
        --arg updatedAt "$updated_at" '
        {
            total: $total,
            available: $available,
            unavailable: $unavailable,
            connectionState: (
                if $activeServer == null
                then "disabled"
                elif $xrayRunning
                then "connected"
                else "error"
                end
            ),
            xrayRunning: $xrayRunning,
            activeServer: $activeServer,
            servers: $servers,
            autoSwitch: $autoSwitch,
            updatedAt: $updatedAt
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

broray_server_check()
{
    check_server_id="$1"

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

    if ! broray_server_validate "$check_server_file" 2>"$BRORAY_BASE/tmp/server-check-error.$$"; then
        check_error="$(
            cat "$BRORAY_BASE/tmp/server-check-error.$$"
        )"
    else
        check_stage="xray-config"

        if generated_config="$(
            broray_generate_server_config "$check_server_id" \
                2>"$BRORAY_BASE/tmp/server-check-error.$$"
        )"; then
            # BRORAY_REAL_PROXY_PROBE_V1
            if broray_xray_test_file "$generated_config" \
                >"$BRORAY_BASE/tmp/server-check-output.$$" \
                2>&1; then
                check_stage="proxy-https"

                check_probe_json="$(
                    "$BRORAY_BASE/bin/broray-server-probe" \
                        "$generated_config" \
                        "$check_server_id" \
                        2>"$BRORAY_BASE/tmp/server-probe-error.$$"
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
                                "$BRORAY_BASE/tmp/server-probe-error.$$" \
                                2>/dev/null
                        )"

                        [ -n "$check_error" ] ||
                            check_error="Пробник вернул некорректный результат."
                    fi
                fi

                rm -f \
                    "$BRORAY_BASE/tmp/server-probe-error.$$"
            else
                check_error="$(
                    cat "$BRORAY_BASE/tmp/server-check-output.$$"
                )"
            fi
        else
            check_error="$(
                cat "$BRORAY_BASE/tmp/server-check-error.$$"
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
            error: $error
        }
    ' > "$quality_file" ||
        broray_die \
            "не удалось сохранить результат проверки"

    chmod 600 "$quality_file"

    rm -f \
        "$generated_config" \
        "$BRORAY_BASE/tmp/server-check-error.$$" \
        "$BRORAY_BASE/tmp/server-check-output.$$"

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
    activate_server_id="$1"

    broray_server_validate_id "$activate_server_id"
    broray_server_exists "$activate_server_id" ||
        broray_die \
            "сервер $activate_server_id не найден"

    broray_xray_apply_server "$activate_server_id"

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
        "$BRORAY_INTERFACE_STATUS"

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

    rm -f \
        "$delete_server_file" \
        "$delete_quality_file" ||
        broray_die \
            "не удалось удалить сервер $delete_server_id"

    jq -n \
        --arg id "$delete_server_id" \
        --arg updatedAt "$(broray_server_now)" '{
            deleted: true,
            id: $id,
            updatedAt: $updatedAt
        }'
}
