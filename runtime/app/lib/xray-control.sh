#!/opt/bin/ash

BRORAY_XRAY_BINARY="${BRORAY_XRAY_BINARY:-/opt/broray/runtime/xray}"
BRORAY_XRAY_CONFIG="${BRORAY_XRAY_CONFIG:-/opt/broray/config/config.json}"
BRORAY_XRAY_INIT="${BRORAY_XRAY_INIT:-/opt/etc/init.d/S24broray}"
BRORAY_XRAY_ASSET_DIR="${BRORAY_XRAY_ASSET_DIR:-/opt/broray/bin}"

. "${BRORAY_BASE:-/opt/broray}/lib/xray-process.sh" || return 1

broray_xray_pid() {
    broray_xray_runtime_pid
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
    # The CLI and WebUI must use the same verified status implementation.
    # Run it in a subshell so the legacy control helpers keep their names for
    # start/stop/restart operations in the parent shell.
    (
        BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
        BRORAY_SETTINGS_FILE="${BRORAY_SETTINGS_FILE:-$BRORAY_ROOT/config/system/settings.json}"
        . "$BRORAY_ROOT/lib/xray.sh" || exit 1
        broray_xray_status_json | jq -ce '.data'
    )
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

    if ! "$BRORAY_XRAY_INIT" start >/dev/null 2>&1; then
        broray_xray_action_result start false
        return 1
    fi

    if broray_xray_wait_running; then
        broray_xray_action_result start true
        return 0
    fi

    broray_xray_action_result start false
    return 1
}

broray_xray_stop() {
    if ! "$BRORAY_XRAY_INIT" stop >/dev/null 2>&1; then
        broray_xray_action_result stop false
        return 1
    fi

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

    if ! "$BRORAY_XRAY_INIT" restart >/dev/null 2>&1; then
        broray_xray_action_result restart false
        return 1
    fi

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
