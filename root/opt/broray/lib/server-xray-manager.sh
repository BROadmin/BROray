#!/bin/sh

. /opt/broray/lib/util.sh
. /opt/broray/lib/server.sh
. /opt/broray/lib/server-config-generator.sh
. /opt/broray/lib/interface-core.sh
. /opt/broray/lib/interface-sync.sh

BRORAY_BASE="/opt/broray"
BRORAY_CONFIG="$BRORAY_BASE/config/config.json"
BRORAY_BACKUP="$BRORAY_BASE/backup"
BRORAY_INIT="/opt/etc/init.d/S24broray"
BRORAY_XRAY="$BRORAY_BASE/bin/xray"

broray_xray_test_file() {
    config_file="$1"

    [ -f "$config_file" ] ||
        broray_die \
            "файл конфигурации не найден: $config_file"

    XRAY_LOCATION_ASSET="$BRORAY_BASE/bin" \
        "$BRORAY_XRAY" run -test -c "$config_file"
}

broray_xray_restart() {
    "$BRORAY_INIT" restart
}

broray_xray_status() {
    pidof xray
}

broray_xray_apply_server() {
    server_id="$1"

    [ -n "$server_id" ] ||
        broray_die "не указан идентификатор сервера"

    broray_server_exists "$server_id" ||
        broray_die "сервер $server_id не найден"

    mkdir -p \
        "$BRORAY_BACKUP" \
        "$BRORAY_BASE/tmp"

    generated_config="$(
        broray_generate_server_config "$server_id"
    )"

    echo "Проверка конфигурации..."

    broray_xray_test_file "$generated_config" ||
        broray_die \
            "Xray отклонил конфигурацию сервера"

    timestamp="$(broray_timestamp)"

    backup_config="$BRORAY_BACKUP/config.server.$timestamp.json"
    backup_active="$BRORAY_BACKUP/active-server.$timestamp"

    previous_server_id=""

    if [ -f "$BRORAY_ACTIVE_SERVER_FILE" ]; then
        previous_server_id="$(
            cat "$BRORAY_ACTIVE_SERVER_FILE"
        )"

        cp "$BRORAY_ACTIVE_SERVER_FILE" \
            "$backup_active" ||
            broray_die \
                "не удалось сохранить активный сервер"
    fi

    if [ -f "$BRORAY_CONFIG" ]; then
        cp "$BRORAY_CONFIG" \
            "$backup_config" ||
            broray_die \
                "не удалось создать резервную копию config.json"
    fi

    mv "$generated_config" "$BRORAY_CONFIG" ||
        broray_die \
            "не удалось установить новую конфигурацию"

    broray_server_set_active "$server_id"

    if ! "$BRORAY_INIT" restart; then
        echo \
            "Перезапуск завершился ошибкой. Выполняется откат." \
            >&2

        if [ -f "$backup_config" ]; then
            cp "$backup_config" \
                "$BRORAY_CONFIG"
        fi

        if [ -n "$previous_server_id" ]; then
            printf '%s\n' "$previous_server_id" \
                > "$BRORAY_ACTIVE_SERVER_FILE"
        else
            rm -f "$BRORAY_ACTIVE_SERVER_FILE"
        fi

        "$BRORAY_INIT" restart || true

        broray_die \
            "не удалось применить сервер"
    fi

    broray_interface_sync_description ||
        echo "ПРЕДУПРЕЖДЕНИЕ: описание интерфейса не обновлено" >&2

    server_file="$(
        broray_server_path "$server_id"
    )"

    server_name="$(
        jq -r '.name // .id' "$server_file"
    )"

    server_protocol="$(
        jq -r '.protocol' "$server_file"
    )"

    server_address="$(
        jq -r '.address' "$server_file"
    )"

    server_port="$(
        jq -r '.port' "$server_file"
    )"

    echo
    echo "Активирован сервер:"
    echo "Название: $server_name"
    echo "ID сервера: $server_id"
    echo "Протокол: $server_protocol"
    echo "Адрес: $server_address:$server_port"
}
