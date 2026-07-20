#!/bin/sh

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_INTERFACE="${BRORAY_INTERFACE:-Proxy0}"
BRORAY_ACTIVE_SERVER_FILE="$BRORAY_BASE/config/active-server"
BRORAY_SERVERS="$BRORAY_BASE/servers"

broray_interface_active_server_id()
{
    if [ ! -s "$BRORAY_ACTIVE_SERVER_FILE" ]; then
        return 1
    fi

    sed -n '1p' "$BRORAY_ACTIVE_SERVER_FILE" |
        tr -d '\r\n'
}

broray_interface_active_server_name()
{
    server_id="$(
        broray_interface_active_server_id
    )"

    [ -n "$server_id" ] || return 1

    server_file="$BRORAY_SERVERS/$server_id.json"

    [ -f "$server_file" ] || return 1

    server_name="$(
        jq -r '
            if (
                (.name | type) == "string" and
                .name != ""
            )
            then
                .name
            elif (
                (.id | type) == "string"
            )
            then
                .id
            else
                ""
            end
        ' "$server_file" 2>/dev/null
    )"

    server_name="$(
        printf '%s' "$server_name" |
            tr '\r\n\t' '   ' |
            sed \
                -e 's/[[:space:]][[:space:]]*/ /g' \
                -e 's/^ //' \
                -e 's/ $//' \
                -e 's/["\\;|&`$<>]//g'
    )"

    [ -n "$server_name" ] || return 1

    printf '%s\n' "$server_name"
}

broray_interface_expected_description()
{
    server_name="$(
        broray_interface_active_server_name 2>/dev/null
    )"

    if [ -n "$server_name" ]; then
        printf 'BROray - %s\n' "$server_name"
    else
        printf '%s\n' "BROray"
    fi
}

broray_interface_set_description()
{
    description="$1"

    [ -n "$description" ] ||
    {
        echo "ОШИБКА: пустое описание интерфейса" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE description \"$description\"" \
        >/dev/null ||
    {
        echo "ОШИБКА: KeeneticOS отклонила описание" >&2
        return 1
    }

    return 0
}

broray_interface_sync_description()
{
    if ! broray_interface_exists; then
        echo "ОШИБКА: интерфейс $BRORAY_INTERFACE отсутствует" >&2
        return 1
    fi

    expected="$(
        broray_interface_expected_description
    )"

    current="$(
        broray_interface_value description
    )"

    echo "Текущее описание: ${current:-не задано}"
    echo "Новое описание: $expected"

    if [ "$current" = "$expected" ]; then
        echo "Синхронизация не требуется"
        return 0
    fi

    broray_interface_set_description "$expected" ||
        return 1

    broray_interface_ndmc \
        "system configuration save" \
        >/dev/null ||
    {
        echo "ОШИБКА: конфигурация не сохранена" >&2
        return 1
    }

    updated="$(
        broray_interface_value description
    )"

    if [ "$updated" != "$expected" ]; then
        echo "ОШИБКА: описание после сохранения не совпало" >&2
        echo "Ожидалось: $expected" >&2
        echo "Получено: ${updated:-пусто}" >&2
        return 1
    fi

    echo "Описание интерфейса синхронизировано"
}
