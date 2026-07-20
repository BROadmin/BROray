#!/bin/sh

broray_server_usage() {
    cat <<'EOF_USAGE'
Использование:
  broray server list
      Показать сохранённые серверы.

  broray server show <SERVER_ID>
      Показать полную запись сервера.

  broray server active
      Показать активный сервер.

  broray server active-id
      Показать ID активного сервера.

  broray server validate [SERVER_ID]
      Проверить структуру выбранного или активного сервера.

  broray server use <SERVER_ID>
      Проверить, применить сервер и перезапустить Xray.

  broray server help
      Показать эту справку.
EOF_USAGE
}

broray_server_show() {
    broray_server_show_id="${1:-}"

    [ -n "$broray_server_show_id" ] ||
        broray_die "не указан идентификатор сервера"

    broray_server_validate_id "$broray_server_show_id"

    broray_server_exists "$broray_server_show_id" ||
        broray_die \
            "сервер $broray_server_show_id не найден"

    broray_server_show_file="$(
        broray_server_path "$broray_server_show_id"
    )"

    broray_server_validate "$broray_server_show_file"

    jq '.' "$broray_server_show_file"
}

broray_server_validate_command() {
    broray_server_validation_id="${1:-}"

    if [ -z "$broray_server_validation_id" ]; then
        broray_server_validation_id="$(
            broray_server_get_active_id
        )"
    fi

    broray_server_validate_id \
        "$broray_server_validation_id"

    broray_server_exists \
        "$broray_server_validation_id" ||
        broray_die \
            "сервер $broray_server_validation_id не найден"

    broray_server_validation_file="$(
        broray_server_path \
            "$broray_server_validation_id"
    )"

    broray_server_validate \
        "$broray_server_validation_file"

    broray_server_validation_protocol="$(
        jq -r '.protocol' \
            "$broray_server_validation_file"
    )"

    jq -n \
        --arg id "$broray_server_validation_id" \
        --arg file "$broray_server_validation_file" \
        --arg protocol "$broray_server_validation_protocol" '
        {
            success: true,
            id: $id,
            file: $file,
            protocol: $protocol,
            schemaVersion: 2
        }
    '
}

broray_server_use_command() {
    broray_server_use_id="${1:-}"

    [ -n "$broray_server_use_id" ] ||
        broray_die "не указан идентификатор сервера"

    broray_server_validate_id "$broray_server_use_id"

    broray_server_exists "$broray_server_use_id" ||
        broray_die \
            "сервер $broray_server_use_id не найден"

    broray_xray_apply_server "$broray_server_use_id"

    broray_keenetic_update_title >/dev/null 2>&1 || true

    if [ -f "$BRORAY_INTERFACE_STATUS" ]; then
        echo
        cat "$BRORAY_INTERFACE_STATUS"
    fi
}

broray_server_command() {
    broray_server_subcommand="${1:-help}"

    case "$broray_server_subcommand" in
        list)
            broray_server_list
            ;;
        show)
            shift
            broray_server_show "${1:-}"
            ;;
        active|current)
            broray_server_current
            ;;
        active-id|current-id)
            broray_server_get_active_id
            ;;
        validate)
            shift
            broray_server_validate_command "${1:-}"
            ;;
        use)
            shift
            broray_server_use_command "${1:-}"
            ;;
        help|--help|-h)
            broray_server_usage
            ;;
        *)
            echo \
                "Неизвестная команда server: $broray_server_subcommand" \
                >&2
            echo >&2
            broray_server_usage >&2
            return 1
            ;;
    esac
}
