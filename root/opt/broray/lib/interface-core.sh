#!/bin/sh

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_INTERFACE_OWNER_LIBRARY="${BRORAY_INTERFACE_OWNER_LIBRARY:-$BRORAY_BASE/lib/interface-owner.sh}"

if [ -r "$BRORAY_INTERFACE_OWNER_LIBRARY" ]; then
    . "$BRORAY_INTERFACE_OWNER_LIBRARY"
fi

if command -v broray_interface_selected_name >/dev/null 2>&1; then
    BRORAY_INTERFACE="$(broray_interface_selected_name)"
else
    BRORAY_INTERFACE="${BRORAY_INTERFACE:-Proxy0}"
fi

broray_interface_ndmc()
{
    ndmc -c "$1"
}

broray_interface_exists()
{
    broray_interface_running_config |
        awk -v interface_name="$BRORAY_INTERFACE" '
            $0 == "interface " interface_name {
                found = 1
            }

            END {
                if (found) {
                    result = 0
                } else {
                    result = 1
                }

                exit result
            }
        '
}

broray_interface_output()
{
    broray_interface_ndmc \
        "show interface $BRORAY_INTERFACE" 2>/dev/null
}

broray_interface_value()
{
    field_name="$1"

    broray_interface_output |
        awk -v field="$field_name" '
            {
                line = $0
                sub(/^[[:space:]]*/, "", line)

                prefix = field ":"

                if (index(line, prefix) == 1) {
                    sub(/^[^:]*:[[:space:]]*/, "", line)
                    print line
                    exit
                }
            }
        '
}

broray_interface_running_config()
{
    ndmc -c "show running-config" 2>/dev/null |
        awk -v interface_name="$BRORAY_INTERFACE" '
            $0 == "interface " interface_name {
                found = 1
            }

            found {
                print
            }

            found && $0 == "!" {
                stop = 1
            }

            stop {
                exit
            }
        '
}

broray_interface_status()
{
    if ! broray_interface_exists; then
        echo "Интерфейс: $BRORAY_INTERFACE"
        echo "Существует: нет"
        return 1
    fi

    interface_type="$(
        broray_interface_value type
    )"

    description="$(
        broray_interface_value description
    )"

    link_state="$(
        broray_interface_value link
    )"

    connected_state="$(
        broray_interface_value connected
    )"

    state="$(
        broray_interface_value state
    )"

    mtu="$(
        broray_interface_value mtu
    )"

    via="$(
        broray_interface_value via
    )"

    local_endpoint="$(
        broray_interface_value \
            local-endpoint-address
    )"

    remote_endpoint="$(
        broray_interface_value \
            remote-endpoint-address
    )"

    echo "Интерфейс: $BRORAY_INTERFACE"
    echo "Существует: да"
    echo "Тип: ${interface_type:-не определён}"
    echo "Описание: ${description:-не задано}"
    echo "Link: ${link_state:-не определён}"
    echo "Connected: ${connected_state:-не определён}"
    echo "State: ${state:-не определён}"
    echo "MTU: ${mtu:-не определён}"
    echo "Подключение через: ${via:-не определено}"
    echo "Локальный адрес: ${local_endpoint:-не определён}"
    echo "Удалённый адрес: ${remote_endpoint:-не определён}"
}

broray_interface_check()
{
    failed=0

    if ! broray_interface_exists; then
        echo "Интерфейс $BRORAY_INTERFACE отсутствует"
        return 1
    fi

    interface_type="$(
        broray_interface_value type
    )"

    running_config="$(
        broray_interface_running_config
    )"

    if [ "$interface_type" = "Proxy" ]; then
        echo "Тип интерфейса: правильно"
    else
        echo "Тип интерфейса: ошибка"
        failed=1
    fi

    if printf '%s\n' "$running_config" |
        grep -q 'proxy protocol socks5'
    then
        echo "Протокол SOCKS5: настроен"
    else
        echo "Протокол SOCKS5: не настроен"
        failed=1
    fi

    if printf '%s\n' "$running_config" |
        grep -Fq "proxy upstream $BRORAY_PROXY_HOST $BRORAY_PROXY_PORT"
    then
        echo "Upstream: $BRORAY_PROXY_HOST:$BRORAY_PROXY_PORT"
    else
        echo "Upstream: отличается"
        failed=1
    fi

    link_state="$(
        broray_interface_value link
    )"

    connected_state="$(
        broray_interface_value connected
    )"

    state="$(
        broray_interface_value state
    )"

    if [ "$link_state" = "up" ] &&
       [ "$connected_state" = "yes" ] &&
       [ "$state" = "up" ]
    then
        echo "Рабочее состояние: да"
    else
        echo "Рабочее состояние: нет"
        failed=1
    fi

    return "$failed"
}

# BROray override: exists begin
broray_interface_exists()
{
    ndmc -c \
        "show interface $BRORAY_INTERFACE" \
        >/dev/null 2>&1
}
# BROray override: exists end
