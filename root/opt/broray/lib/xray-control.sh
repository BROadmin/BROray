#!/bin/sh

BRORAY_XRAY_BINARY="${BRORAY_XRAY_BINARY:-/opt/broray/bin/xray}"
BRORAY_XRAY_CONFIG="${BRORAY_XRAY_CONFIG:-/opt/broray/config/config.json}"
BRORAY_XRAY_INIT="${BRORAY_XRAY_INIT:-/opt/etc/init.d/S24broray}"
BRORAY_XRAY_ASSET_DIR="${BRORAY_XRAY_ASSET_DIR:-/opt/broray/bin}"

broray_xray_pid() {
    pidof xray 2>/dev/null |
        awk '{print $1}'
}

broray_xray_is_running() {
    broray_xray_check_pid="$(broray_xray_pid)"

    [ -n "$broray_xray_check_pid" ] &&
        kill -0 "$broray_xray_check_pid" 2>/dev/null
}

broray_xray_wait_running() {
    broray_xray_wait_counter=0
    broray_xray_wait_limit=12

    while [ "$broray_xray_wait_counter" -lt "$broray_xray_wait_limit" ]; do
        if broray_xray_is_running; then
            return 0
        fi

        sleep 1
        broray_xray_wait_counter=$((broray_xray_wait_counter + 1))
    done

    return 1
}

broray_xray_wait_stopped() {
    broray_xray_wait_counter=0
    broray_xray_wait_limit=12

    while [ "$broray_xray_wait_counter" -lt "$broray_xray_wait_limit" ]; do
        if ! broray_xray_is_running; then
            return 0
        fi

        sleep 1
        broray_xray_wait_counter=$((broray_xray_wait_counter + 1))
    done

    return 1
}

broray_xray_version() {
    broray_xray_version_line=""

    if [ -x "$BRORAY_XRAY_BINARY" ]; then
        broray_xray_version_line="$(
            XRAY_LOCATION_ASSET="$BRORAY_XRAY_ASSET_DIR" \
                "$BRORAY_XRAY_BINARY" version 2>/dev/null |
                sed -n '1p'
        )"
    fi

    jq -n \
        --arg binary "$BRORAY_XRAY_BINARY" \
        --arg version "$broray_xray_version_line" '
        {
            binary: $binary,
            version: (
                if $version == ""
                then null
                else $version
                end
            )
        }
    '
}

broray_xray_validate() {
    broray_xray_validate_output=""
    broray_xray_validate_success=false

    if [ ! -x "$BRORAY_XRAY_BINARY" ]; then
        broray_xray_validate_output="Исполняемый файл Xray не найден."
    elif [ ! -f "$BRORAY_XRAY_CONFIG" ]; then
        broray_xray_validate_output="Файл конфигурации Xray не найден."
    else
        broray_xray_validate_output="$(
            XRAY_LOCATION_ASSET="$BRORAY_XRAY_ASSET_DIR" \
                "$BRORAY_XRAY_BINARY" run \
                -test \
                -c "$BRORAY_XRAY_CONFIG" 2>&1
        )"
        broray_xray_validate_code=$?

        if [ "$broray_xray_validate_code" -eq 0 ]; then
            broray_xray_validate_success=true
        fi
    fi

    jq -n \
        --argjson success "$broray_xray_validate_success" \
        --arg config "$BRORAY_XRAY_CONFIG" \
        --arg output "$broray_xray_validate_output" '
        {
            success: $success,
            config: $config,
            output: (
                if $output == ""
                then null
                else $output
                end
            )
        }
    '

    [ "$broray_xray_validate_success" = true ]
}

broray_xray_status() {
    broray_xray_running=false
    broray_xray_current_pid=""
    broray_xray_version_line=""
    broray_xray_architecture="$(
        uname -m 2>/dev/null ||
            printf 'unknown'
    )"
    broray_xray_binary_size=0
    broray_xray_socks_listen=""
    broray_xray_socks_port=""
    broray_xray_socks_active=false

    if [ -x "$BRORAY_XRAY_BINARY" ]; then
        broray_xray_version_line="$(
            XRAY_LOCATION_ASSET="$BRORAY_XRAY_ASSET_DIR" \
                "$BRORAY_XRAY_BINARY" version 2>/dev/null |
                sed -n '1p'
        )"

        broray_xray_binary_size="$(
            wc -c < "$BRORAY_XRAY_BINARY" 2>/dev/null |
                tr -d ' '
        )"
    fi

    broray_xray_current_pid="$(broray_xray_pid)"

    if [ -n "$broray_xray_current_pid" ] &&
        kill -0 "$broray_xray_current_pid" 2>/dev/null
    then
        broray_xray_running=true
    else
        broray_xray_current_pid=""
    fi

    if [ -f "$BRORAY_XRAY_CONFIG" ]; then
        broray_xray_socks_listen="$(
            jq -r '
                [
                    .inbounds[]?
                    | select(.protocol == "socks")
                    | .listen // empty
                ][0] // empty
            ' "$BRORAY_XRAY_CONFIG" 2>/dev/null
        )"

        broray_xray_socks_port="$(
            jq -r '
                [
                    .inbounds[]?
                    | select(.protocol == "socks")
                    | .port // empty
                ][0] // empty
            ' "$BRORAY_XRAY_CONFIG" 2>/dev/null
        )"
    fi

    if [ -n "$broray_xray_socks_listen" ] &&
        [ -n "$broray_xray_socks_port" ]
    then
        if netstat -lnt 2>/dev/null |
            awk \
                -v endpoint="$broray_xray_socks_listen:$broray_xray_socks_port" '
                    $4 == endpoint {
                        found = 1
                    }

                    END {
                        exit found ? 0 : 1
                    }
                '
        then
            broray_xray_socks_active=true
        fi
    fi

    jq -n \
        --argjson running "$broray_xray_running" \
        --arg pid "$broray_xray_current_pid" \
        --arg version "$broray_xray_version_line" \
        --arg architecture "$broray_xray_architecture" \
        --arg binary "$BRORAY_XRAY_BINARY" \
        --argjson binarySizeBytes "${broray_xray_binary_size:-0}" \
        --arg config "$BRORAY_XRAY_CONFIG" \
        --arg socksListen "$broray_xray_socks_listen" \
        --arg socksPort "$broray_xray_socks_port" \
        --argjson socksActive "$broray_xray_socks_active" '
        {
            running: $running,
            pid: (
                if $pid == ""
                then null
                else ($pid | tonumber)
                end
            ),
            version: (
                if $version == ""
                then null
                else $version
                end
            ),
            architecture: $architecture,
            binary: $binary,
            binarySizeBytes: $binarySizeBytes,
            config: $config,
            socks: {
                listen: (
                    if $socksListen == ""
                    then null
                    else $socksListen
                    end
                ),
                port: (
                    if $socksPort == ""
                    then null
                    else ($socksPort | tonumber)
                    end
                ),
                active: $socksActive
            }
        }
    '
}

broray_xray_action_result() {
    broray_xray_result_action="$1"
    broray_xray_result_success="$2"
    broray_xray_result_running=false
    broray_xray_result_pid="$(broray_xray_pid)"

    if [ -n "$broray_xray_result_pid" ] &&
        kill -0 "$broray_xray_result_pid" 2>/dev/null
    then
        broray_xray_result_running=true
    else
        broray_xray_result_pid=""
    fi

    jq -n \
        --argjson success "$broray_xray_result_success" \
        --arg action "$broray_xray_result_action" \
        --argjson running "$broray_xray_result_running" \
        --arg pid "$broray_xray_result_pid" '
        {
            success: $success,
            action: $action,
            running: $running,
            pid: (
                if $pid == ""
                then null
                else ($pid | tonumber)
                end
            )
        }
    '
}

broray_xray_start() {
    if ! broray_xray_validate >/dev/null; then
        broray_xray_action_result start false
        return 1
    fi

    "$BRORAY_XRAY_INIT" start >/dev/null 2>&1

    if broray_xray_wait_running; then
        broray_xray_action_result start true
        return 0
    fi

    broray_xray_action_result start false
    return 1
}

broray_xray_stop() {
    "$BRORAY_XRAY_INIT" stop >/dev/null 2>&1

    if broray_xray_wait_stopped; then
        broray_xray_action_result stop true
        return 0
    fi

    broray_xray_action_result stop false
    return 1
}

broray_xray_restart() {
    if ! broray_xray_validate >/dev/null; then
        broray_xray_action_result restart false
        return 1
    fi

    "$BRORAY_XRAY_INIT" restart >/dev/null 2>&1

    if broray_xray_wait_running; then
        broray_xray_action_result restart true
        return 0
    fi

    broray_xray_action_result restart false
    return 1
}

broray_xray_usage() {
    cat <<'EOF_USAGE'
Использование:
  broray xray status
      Показать состояние Xray в формате JSON.

  broray xray version
      Показать версию Xray в формате JSON.

  broray xray validate
      Проверить конфигурацию Xray без запуска.

  broray xray start
      Проверить конфигурацию и запустить Xray.

  broray xray stop
      Остановить Xray.

  broray xray restart
      Проверить конфигурацию и перезапустить Xray.
EOF_USAGE
}

broray_xray_command() {
    broray_xray_subcommand="${1:-help}"

    case "$broray_xray_subcommand" in
        status)
            broray_xray_status
            ;;
        version)
            broray_xray_version
            ;;
        validate)
            broray_xray_validate
            ;;
        start)
            broray_xray_start
            ;;
        stop)
            broray_xray_stop
            ;;
        restart)
            broray_xray_restart
            ;;
        help|--help|-h)
            broray_xray_usage
            ;;
        *)
            echo "Неизвестная команда Xray: $broray_xray_subcommand" >&2
            echo >&2
            broray_xray_usage >&2
            return 1
            ;;
    esac
}
