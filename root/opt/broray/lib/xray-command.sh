#!/bin/sh

# BROray Xray updater
. /opt/broray/lib/xray-update.sh

broray_xray_usage()
{
    cat <<'EOF_USAGE'
Использование:
  broray xray status
  broray xray version
  broray xray validate
  broray xray test [CONFIG]
  broray xray config
  broray xray start
  broray xray stop
  broray xray restart
  broray xray update-check
  broray xray update
  broray xray reinstall
  broray xray update-clean
  broray xray help
EOF_USAGE
}

broray_xray_test_command()
{
    config="${1:-$BRORAY_XRAY_CONFIG}"

    [ -f "$config" ] ||
        broray_die "файл не найден: $config"

    XRAY_LOCATION_ASSET="$BRORAY_XRAY_ASSET_DIR" \
        "$BRORAY_XRAY_BINARY" \
        run \
        -test \
        -c "$config"
}

broray_xray_config_command()
{
    config="$BRORAY_XRAY_CONFIG"

    [ -f "$config" ] ||
        broray_die \
            "конфигурация не найдена: $config"

    active_server=""

    if [ -f "$BRORAY_ACTIVE_SERVER_FILE" ]; then
        active_server="$(
            cat "$BRORAY_ACTIVE_SERVER_FILE"
        )"
    fi

    size_bytes="$(
        wc -c < "$config" |
            tr -d ' '
    )"
    modified="$(
        date -r "$config" \
            '+%Y-%m-%d %H:%M:%S'
    )"
    sha256="$(
        sha256sum "$config" |
            awk '{print $1}'
    )"

    jq -n \
        --arg file "$config" \
        --arg activeServer "$active_server" \
        --arg sizeBytes "$size_bytes" \
        --arg modified "$modified" \
        --arg sha256 "$sha256" '
        {
            file: $file,
            sizeBytes: ($sizeBytes | tonumber),
            modified: $modified,
            sha256: $sha256,
            activeServer: $activeServer
        }
    '
}

broray_xray_command()
{
    cmd="${1:-help}"

    case "$cmd" in
        status)
            broray_xray_status
            ;;
        version)
            broray_xray_version
            ;;
        validate)
            broray_xray_validate
            ;;
        test)
            shift
            broray_xray_test_command "${1:-}"
            ;;
        config)
            broray_xray_config_command
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
        update-check)
            broray_xray_update_check
            ;;
        update)
            broray_xray_update_command
            ;;
        reinstall)
            broray_xray_reinstall_command
            ;;
        update-clean)
            broray_xray_update_clean
            ;;
        help|--help|-h)
            broray_xray_usage
            ;;
        *)
            echo \
                "Неизвестная команда Xray: $cmd" \
                >&2
            echo >&2
            broray_xray_usage >&2
            return 1
            ;;
    esac
}
