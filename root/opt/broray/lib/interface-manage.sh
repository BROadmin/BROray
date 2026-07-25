#!/bin/sh

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_INTERFACE="${BRORAY_INTERFACE:-Proxy0}"
BRORAY_PROXY_HOST="${BRORAY_PROXY_HOST:-192.168.1.1}"
BRORAY_PROXY_PORT="${BRORAY_PROXY_PORT:-2080}"

broray_interface_save()
{
    broray_interface_ndmc \
        "system configuration save" \
        >/dev/null ||
    {
        echo "ОШИБКА: конфигурация KeeneticOS не сохранена" >&2
        return 1
    }
}

broray_interface_config_has()
{
    expected_line="$1"

    broray_interface_running_config |
        grep -F -q "$expected_line"
}

broray_interface_connect_configured()
{
    broray_interface_running_config |
        awk '
            $1 == "proxy" &&
            $2 == "connect" {
                found = 1
            }

            END {
                if (found) {
                    exit 0
                }

                exit 1
            }
        '
}

broray_interface_connect_value()
{
    broray_interface_running_config |
        awk '
            $1 == "proxy" &&
            $2 == "connect" &&
            $3 == "via" {
                print $4
                exit
            }

            $1 == "proxy" &&
            $2 == "connect" {
                print "Любое интернет-подключение"
                exit
            }
        '
}

broray_interface_create_unchecked()
{
    if broray_interface_exists; then
        echo "ОШИБКА: интерфейс $BRORAY_INTERFACE уже существует" >&2
        echo "Используйте repair" >&2
        return 1
    fi

    description="$(
        broray_interface_expected_description
    )"

    echo "Создание интерфейса $BRORAY_INTERFACE"
    echo "Описание: $description"
    echo "SOCKS5 upstream: $BRORAY_PROXY_HOST:$BRORAY_PROXY_PORT"
    echo "Подключаться через: любое интернет-подключение"

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE description \"$description\"" \
        >/dev/null ||
    {
        echo "ОШИБКА: интерфейс не создан" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE security-level public" \
        >/dev/null ||
    {
        echo "ОШИБКА: security-level не настроен" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE proxy protocol socks5" \
        >/dev/null ||
    {
        echo "ОШИБКА: протокол SOCKS5 не настроен" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE proxy upstream $BRORAY_PROXY_HOST $BRORAY_PROXY_PORT" \
        >/dev/null ||
    {
        echo "ОШИБКА: SOCKS5 upstream не настроен" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE proxy connect" \
        >/dev/null ||
    {
        echo "ОШИБКА: режим подключения не настроен" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE up" \
        >/dev/null ||
    {
        echo "ОШИБКА: интерфейс не включён" >&2
        return 1
    }

    broray_interface_save ||
        return 1

    if ! broray_interface_exists; then
        echo "ОШИБКА: интерфейс отсутствует после создания" >&2
        return 1
    fi

    echo "Интерфейс $BRORAY_INTERFACE создан"
}

broray_interface_delete_unchecked()
{
    if ! broray_interface_exists; then
        echo "Интерфейс $BRORAY_INTERFACE уже отсутствует"
        return 0
    fi

    echo "Удаление интерфейса $BRORAY_INTERFACE"

    broray_interface_ndmc \
        "no interface $BRORAY_INTERFACE" \
        >/dev/null ||
    {
        echo "ОШИБКА: KeeneticOS отклонила удаление" >&2
        return 1
    }

    broray_interface_save ||
        return 1

    if broray_interface_exists; then
        echo "ОШИБКА: интерфейс остался после удаления" >&2
        return 1
    fi

    echo "Интерфейс $BRORAY_INTERFACE удалён"
}


broray_interface_repair_unchecked()
{
    if ! broray_interface_exists; then
        echo "Интерфейс $BRORAY_INTERFACE отсутствует"
        echo "Выполняется создание"

        broray_interface_create
        return "$?"
    fi

    changed=0

    expected_description="$(
        broray_interface_expected_description
    )"

    current_description="$(
        broray_interface_value description
    )"

    echo "Проверка интерфейса $BRORAY_INTERFACE"

    if [ "$current_description" != "$expected_description" ]; then
        echo "Исправление описания"

        broray_interface_set_description \
            "$expected_description" ||
            return 1

        changed=1
    else
        echo "Описание: правильно"
    fi

    if broray_interface_config_has \
        "security-level public"
    then
        echo "Security level: правильно"
    else
        echo "Исправление security level"

        broray_interface_ndmc \
            "interface $BRORAY_INTERFACE security-level public" \
            >/dev/null ||
            return 1

        changed=1
    fi

    if broray_interface_config_has \
        "proxy protocol socks5"
    then
        echo "Протокол SOCKS5: правильно"
    else
        echo "Исправление протокола SOCKS5"

        broray_interface_ndmc \
            "interface $BRORAY_INTERFACE proxy protocol socks5" \
            >/dev/null ||
            return 1

        changed=1
    fi

    if broray_interface_config_has \
        "proxy upstream $BRORAY_PROXY_HOST $BRORAY_PROXY_PORT"
    then
        echo "SOCKS5 upstream: правильно"
    else
        echo "Исправление SOCKS5 upstream"

        broray_interface_ndmc \
            "interface $BRORAY_INTERFACE proxy upstream $BRORAY_PROXY_HOST $BRORAY_PROXY_PORT" \
            >/dev/null ||
            return 1

        changed=1
    fi

    if broray_interface_connect_configured; then
        connect_value="$(
            broray_interface_connect_value
        )"

        echo "Подключаться через: ${connect_value:-настроено}"
        echo "Параметр сохранён без изменений"
    else
        echo "Параметр подключения отсутствует"
        echo "Установка: любое интернет-подключение"

        broray_interface_ndmc \
            "interface $BRORAY_INTERFACE proxy connect" \
            >/dev/null ||
            return 1

        changed=1
    fi

    if broray_interface_config_has "up"; then
        echo "Интерфейс включён: да"
    else
        echo "Включение интерфейса"

        broray_interface_ndmc \
            "interface $BRORAY_INTERFACE up" \
            >/dev/null ||
            return 1

        changed=1
    fi

    if [ "$changed" -eq 1 ]; then
        broray_interface_save ||
            return 1

        echo "Интерфейс исправлен"
    else
        echo "Исправление не требуется"
    fi

    echo
    broray_interface_check
}


# BROray override: create begin
broray_interface_create_unchecked()
{
    if broray_interface_exists; then
        echo "ОШИБКА: интерфейс $BRORAY_INTERFACE уже существует" >&2
        echo "Используйте repair" >&2
        return 1
    fi

    description="$(
        broray_interface_expected_description
    )"

    echo "Создание интерфейса $BRORAY_INTERFACE"

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE" >/dev/null ||
    {
        echo "ОШИБКА: интерфейс не создан" >&2
        return 1
    }

    if ! broray_interface_exists; then
        echo "ОШИБКА: интерфейс отсутствует после создания" >&2
        return 1
    fi

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE proxy protocol socks5" >/dev/null ||
    {
        echo "ОШИБКА: протокол SOCKS5 не установлен" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE proxy upstream $BRORAY_PROXY_HOST $BRORAY_PROXY_PORT" >/dev/null ||
    {
        echo "ОШИБКА: upstream не установлен" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE proxy connect" >/dev/null ||
    {
        echo "ОШИБКА: proxy connect не установлен" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE description \"$description\"" >/dev/null ||
    {
        echo "ОШИБКА: описание не установлено" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE security-level public" >/dev/null ||
    {
        echo "ОШИБКА: security-level не установлен" >&2
        return 1
    }

    broray_interface_ndmc \
        "interface $BRORAY_INTERFACE up" >/dev/null ||
    {
        echo "ОШИБКА: интерфейс не включён" >&2
        return 1
    }

    broray_interface_save ||
        return 1

    echo "Интерфейс $BRORAY_INTERFACE создан"
}
# BROray override: create end


# BROray ownership guard: public interface actions.
broray_interface_select_for_action()
{
    local selected

    command -v broray_interface_select_and_sync >/dev/null 2>&1 || {
        echo "ОШИБКА: модуль безопасного выбора ProxyN недоступен" >&2
        return 1
    }

    selected="$(broray_interface_select_and_sync)" || {
        echo "ОШИБКА: не удалось выбрать свободный интерфейс ProxyN" >&2
        return 1
    }

    BRORAY_INTERFACE="$selected"
    export BRORAY_INTERFACE
}

broray_interface_create()
{
    broray_interface_select_for_action || return 1

    if broray_interface_exists; then
        if broray_interface_owner_valid "$BRORAY_INTERFACE"; then
            echo "ОШИБКА: интерфейс $BRORAY_INTERFACE уже существует" >&2
            echo "Используйте repair" >&2
        else
            echo "ОШИБКА: интерфейс $BRORAY_INTERFACE занят и не принадлежит BROray" >&2
            echo "Чужой прокси-интерфейс оставлен без изменений" >&2
        fi
        return 1
    fi

    broray_interface_create_unchecked
    create_rc=$?
    if [ "$create_rc" -ne 0 ]; then
        if broray_interface_owner_record_valid "$BRORAY_INTERFACE" &&
           broray_interface_exists
        then
            broray_interface_delete_unchecked >/dev/null 2>&1 || true
        fi
        return "$create_rc"
    fi

    broray_interface_owner_signature_matches "$BRORAY_INTERFACE" || {
        broray_interface_delete_unchecked >/dev/null 2>&1 || true
        echo "ОШИБКА: созданный интерфейс не прошёл проверку принадлежности" >&2
        return 1
    }

    broray_interface_owner_write "$BRORAY_INTERFACE" created || return 1
    broray_interface_sync_route_policy "$BRORAY_INTERFACE" || return 1
}

broray_interface_repair()
{
    broray_interface_select_for_action || return 1

    if broray_interface_exists; then
        broray_interface_require_owned "$BRORAY_INTERFACE" || return 1
        broray_interface_repair_unchecked || return $?
    else
        broray_interface_create_unchecked
        create_rc=$?
        if [ "$create_rc" -ne 0 ]; then
            if broray_interface_owner_record_valid "$BRORAY_INTERFACE" &&
               broray_interface_exists
            then
                broray_interface_delete_unchecked >/dev/null 2>&1 || true
            fi
            return "$create_rc"
        fi
    fi

    broray_interface_owner_signature_matches "$BRORAY_INTERFACE" || {
        echo "ОШИБКА: интерфейс не прошёл проверку принадлежности после восстановления" >&2
        return 1
    }

    broray_interface_owner_write "$BRORAY_INTERFACE" repaired || return 1
    broray_interface_sync_route_policy "$BRORAY_INTERFACE" || return 1
}

broray_interface_delete()
{
    if broray_interface_exists; then
        broray_interface_require_owned "$BRORAY_INTERFACE" || return 1
    fi

    broray_interface_delete_unchecked || return $?
    rm -f "$BRORAY_BASE/run/routes-router-config-cache.json" 2>/dev/null || true
}
