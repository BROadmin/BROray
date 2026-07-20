#!/bin/sh

set -u

VERSION="2.0.0"
PRODUCT="BROray"
SCRIPT_DIR="$(
    CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null &&
        pwd
)"
SOURCE_ROOT="$SCRIPT_DIR/root"
TARGET="/opt/broray"
MIN_FREE_KB="51200"
LIGHTTPD=""

fail() {
    echo "ОШИБКА: $*" >&2
    exit 1
}

step() {
    printf '\n=== %s ===\n' "$1"
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || {
        fail "Не найдена команда: $1"
    }
}

install_dependencies() {
    REQUIRED_PACKAGES="
ca-bundle
ca-certificates
curl
jq
lighttpd
lighttpd-mod-cgi
"

    MISSING_PACKAGES=""

    for PACKAGE in $REQUIRED_PACKAGES; do
        if ! opkg status "$PACKAGE" 2>/dev/null |
            grep -q '^Status:.* installed'
        then
            MISSING_PACKAGES="$MISSING_PACKAGES $PACKAGE"
        fi
    done

    if [ -z "$MISSING_PACKAGES" ]; then
        echo "Все зависимости уже установлены"
        return 0
    fi

    echo "Будут установлены:$MISSING_PACKAGES"

    opkg update ||
        fail "Не удалось обновить список пакетов OPKG"

    opkg install $MISSING_PACKAGES ||
        fail "Не удалось установить зависимости"
}

check_environment() {
    require_command ash
    require_command awk
    require_command cp
    require_command grep
    require_command ip
    require_command ln
    require_command mkdir
    require_command ndmc
    require_command opkg
    require_command sed

    [ -d "$SOURCE_ROOT/opt/broray" ] ||
        fail "Не найден каталог релиза: $SOURCE_ROOT/opt/broray"

    [ -x "$SOURCE_ROOT/opt/broray/bin/xray" ] ||
        fail "В релизе отсутствует исполняемый файл Xray"

    MACHINE="$(uname -m 2>/dev/null)"

    case "$MACHINE" in
        aarch64|arm64)
            ;;
        *)
            fail "Неподдерживаемая архитектура: $MACHINE"
            ;;
    esac

    AVAILABLE_KB="$(
        df -k /opt 2>/dev/null |
            awk 'NR == 2 { print $4; exit }'
    )"

    case "$AVAILABLE_KB" in
        ''|*[!0-9]*)
            fail "Не удалось определить свободное место в /opt"
            ;;
    esac

    if [ "$AVAILABLE_KB" -lt "$MIN_FREE_KB" ]; then
        fail "Недостаточно места в /opt: ${AVAILABLE_KB} КБ"
    fi

    echo "Архитектура: $MACHINE"
    echo "Свободно в /opt: ${AVAILABLE_KB} КБ"
}

stop_existing_services() {
    for SCRIPT in \
        /opt/etc/init.d/S23broray-monitor \
        /opt/etc/init.d/S24broray \
        /opt/etc/init.d/S25broray-web
    do
        if [ -x "$SCRIPT" ]; then
            "$SCRIPT" stop >/dev/null 2>&1 || true
        fi
    done
}

copy_release() {
    mkdir -p /opt /opt/etc/init.d ||
        fail "Не удалось создать системные каталоги"

    mkdir -p "$TARGET" ||
        fail "Не удалось создать $TARGET"

    cp -Rp "$SOURCE_ROOT/opt/broray/." "$TARGET/" ||
        fail "Не удалось скопировать файлы BROray"

    cp -p \
        "$SOURCE_ROOT/opt/etc/init.d/S23broray-monitor" \
        "$SOURCE_ROOT/opt/etc/init.d/S24broray" \
        "$SOURCE_ROOT/opt/etc/init.d/S25broray-web" \
        /opt/etc/init.d/ ||
        fail "Не удалось установить init-скрипты"

    chmod 755 \
        "$TARGET/bin/"* \
        "$TARGET/web/cgi-bin/"*.cgi \
        /opt/etc/init.d/S23broray-monitor \
        /opt/etc/init.d/S24broray \
        /opt/etc/init.d/S25broray-web ||
        fail "Не удалось назначить права доступа"

    ln -sf "$TARGET/bin/broray" /opt/bin/broray ||
        fail "Не удалось создать ссылку /opt/bin/broray"

    ln -sf "$TARGET/bin/broray-server" /opt/bin/broray-server ||
        fail "Не удалось создать ссылку /opt/bin/broray-server"
}

configure_local_address() {
    NETWORK="$TARGET/lib/network.sh"
    SETTINGS="$TARGET/config/system/settings.json"
    LIGHTTPD_CONF="$TARGET/config/lighttpd.conf"

    [ -f "$NETWORK" ] ||
        fail "Не найден $NETWORK"

    [ -f "$SETTINGS" ] ||
        fail "Не найден $SETTINGS"

    [ -f "$LIGHTTPD_CONF" ] ||
        fail "Не найден $LIGHTTPD_CONF"

    . "$NETWORK"

    LAN_IP="$(broray_save_lan_ip)" ||
        fail "Не удалось определить LAN-IP"

    SETTINGS_TMP="$SETTINGS.install-new"

    jq \
        --arg ip "$LAN_IP" \
        '.listenAddress = $ip' \
        "$SETTINGS" > "$SETTINGS_TMP" ||
        fail "Не удалось обновить settings.json"

    jq -e . "$SETTINGS_TMP" >/dev/null 2>&1 ||
        fail "Получен некорректный settings.json"

    mv "$SETTINGS_TMP" "$SETTINGS" ||
        fail "Не удалось сохранить settings.json"

    if grep -q '^server\.bind[[:space:]]*=' "$LIGHTTPD_CONF"; then
        sed -i \
            "s|^server\.bind[[:space:]]*=.*|server.bind = \"$LAN_IP\"|" \
            "$LIGHTTPD_CONF" ||
            fail "Не удалось обновить server.bind"
    else
        sed -i \
            "/^server\.port/i server.bind = \"$LAN_IP\"" \
            "$LIGHTTPD_CONF" ||
            fail "Не удалось добавить server.bind"
    fi

    echo "LAN-IP: $LAN_IP"
}

configure_keenetic_proxy() {
    ndmc -c "no ip http proxy broray" >/dev/null 2>&1 || true

    ndmc -c \
        "ip http proxy broray upstream http $LAN_IP 8080" \
        >/dev/null 2>&1 ||
        fail "Не удалось настроить upstream HTTP-прокси"

    ndmc -c \
        "ip http proxy broray domain ndns" \
        >/dev/null 2>&1 ||
        fail "Не удалось привязать HTTP-прокси к KeenDNS"

    ndmc -c \
        "ip http proxy broray ssl redirect" \
        >/dev/null 2>&1 ||
        fail "Не удалось включить перенаправление HTTPS"

    ndmc -c \
        "ip http proxy broray security-level public" \
        >/dev/null 2>&1 ||
        fail "Не удалось установить уровень доступа HTTP-прокси"

    ndmc -c "system configuration save" >/dev/null 2>&1 ||
        fail "Не удалось сохранить конфигурацию Keenetic"

    ndmc -c "show running-config" 2>/dev/null |
        grep -A 5 '^ip http proxy broray$' |
        grep -q "upstream http $LAN_IP 8080" ||
        fail "HTTP-прокси BROray не обнаружен после настройки"

    echo "HTTP-прокси Keenetic настроен"
}

validate_release() {
    for FILE in \
        "$TARGET/bin/broray" \
        "$TARGET/bin/broray-connection-monitor" \
        "$TARGET/bin/broray-route-prepare" \
        "$TARGET/bin/broray-server" \
        "$TARGET/bin/broray-test" \
        "$TARGET/bin/broray-xray-start" \
        "$TARGET/lib/"*.sh \
        "$TARGET/web/cgi-bin/"*.cgi \
        /opt/etc/init.d/S23broray-monitor \
        /opt/etc/init.d/S24broray \
        /opt/etc/init.d/S25broray-web
    do
        [ -f "$FILE" ] || fail "Не найден файл: $FILE"

        ash -n "$FILE" ||
            fail "Ошибка синтаксиса: $FILE"
    done

    "$LIGHTTPD" \
        -tt \
        -f "$TARGET/config/lighttpd.conf" ||
        fail "Конфигурация lighttpd некорректна"

    "$TARGET/bin/broray" test ||
        fail "Конфигурация Xray некорректна"

    echo "Проверка файлов завершена"
}

start_services() {
    /opt/etc/init.d/S23broray-monitor start ||
        fail "Не удалось запустить монитор соединения"

    /opt/etc/init.d/S24broray start ||
        fail "Не удалось запустить BROray"

    /opt/etc/init.d/S25broray-web start ||
        fail "Не удалось запустить WebUI"
}

validate_webui() {
    ATTEMPT=1

    while [ "$ATTEMPT" -le 10 ]; do
        if curl \
            -fsS \
            --max-time 5 \
            "http://$LAN_IP:8080/" \
            >/dev/null 2>&1
        then
            echo "WebUI отвечает"
            return 0
        fi

        sleep 1
        ATTEMPT=$((ATTEMPT + 1))
    done

    fail "WebUI не отвечает по адресу http://$LAN_IP:8080/"
}

validate_external_webui() {
    NETWORK="$TARGET/lib/network.sh"

    [ -f "$NETWORK" ] || {
        echo "Файл network.sh не найден — внешняя проверка пропущена"
        return 0
    }

    . "$NETWORK"

    command -v broray_detect_keendns_fqdn >/dev/null 2>&1 || {
        echo "Определение KeenDNS недоступно — внешняя проверка пропущена"
        return 0
    }

    KEENDNS_FQDN="$(
        broray_detect_keendns_fqdn 2>/dev/null
    )"

    if [ -z "$KEENDNS_FQDN" ]; then
        echo "KeenDNS не настроен — внешняя проверка пропущена"
        return 0
    fi

    EXTERNAL_URL="https://broray.$KEENDNS_FQDN/"

    if curl \
        -fsS \
        --max-time 15 \
        "$EXTERNAL_URL" \
        >/dev/null 2>&1
    then
        echo "Внешний WebUI отвечает: $EXTERNAL_URL"
        return 0
    fi

    echo "ПРЕДУПРЕЖДЕНИЕ: внешний WebUI пока не отвечает:"
    echo "$EXTERNAL_URL"
    echo "Локальная установка при этом завершена успешно"

    return 0
}

print_result() {
    . "$TARGET/lib/network.sh"

    printf '\n'
    printf '%s %s успешно установлен\n' "$PRODUCT" "$VERSION"
    printf '\nАдрес WebUI:\n'

    broray_detect_webui_urls |
    while IFS= read -r URL; do
        printf '  %s\n' "$URL"
    done

    printf '\nПроект: https://broray.keenetic.pro/\n'
}

step "ПРОВЕРКА СРЕДЫ"
check_environment

step "УСТАНОВКА ЗАВИСИМОСТЕЙ"
install_dependencies

require_command curl
require_command jq
require_command lighttpd
LIGHTTPD="$(command -v lighttpd)"

step "ОСТАНОВКА ПРЕДЫДУЩЕЙ ВЕРСИИ"
stop_existing_services

step "КОПИРОВАНИЕ ФАЙЛОВ"
copy_release

step "НАСТРОЙКА LAN-АДРЕСА"
configure_local_address

step "НАСТРОЙКА KEENETIC"
configure_keenetic_proxy

step "ПРОВЕРКА КОНФИГУРАЦИИ"
validate_release

step "ЗАПУСК СЛУЖБ"
start_services

step "ПРОВЕРКА WEBUI"
validate_webui

step "ПРОВЕРКА ВНЕШНЕГО WEBUI"
validate_external_webui

print_result
