#!/opt/bin/ash

set -u

PATH="/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

PRODUCT="BROray"
VERSION="2.2.0"
TARGET="/opt/broray"
LIGHTTPD=""
LAN_IP=""

fail()
{
    printf 'ОШИБКА: %s\n' "$*" >&2
    exit 1
}

require_command()
{
    command -v "$1" >/dev/null 2>&1 ||
        fail "Не найдена команда: $1"
}

create_runtime_directories()
{
    mkdir -p \
        "$TARGET/backup" \
        "$TARGET/backups" \
        "$TARGET/config/disabled-subscription-servers" \
        "$TARGET/config/subscriptions" \
        "$TARGET/data" \
        "$TARGET/deleted-subscriptions" \
        "$TARGET/logs" \
        "$TARGET/routes/backup" \
        "$TARGET/routes/catalog" \
        "$TARGET/routes/installed/bundles" \
        "$TARGET/routes/locks" \
        "$TARGET/routes/manifests" \
        "$TARGET/routes/state" \
        "$TARGET/routes/tmp" \
        "$TARGET/routes/tmp/user-previews" \
        "$TARGET/routes/transactions" \
        "$TARGET/run/server-quality" \
        "$TARGET/run/subscriptions" \
        "$TARGET/run/web-new/sessions" \
        "$TARGET/servers" \
        "$TARGET/subscriptions" \
        "$TARGET/tmp" \
        "$TARGET/update" \
        /opt/bin ||
        fail "Не удалось создать рабочие каталоги"

    chmod 700 \
        "$TARGET/backup" \
        "$TARGET/backups" \
        "$TARGET/config/disabled-subscription-servers" \
        "$TARGET/config/subscriptions" \
        "$TARGET/deleted-subscriptions" \
        "$TARGET/run/web-new/sessions" \
        "$TARGET/servers" \
        "$TARGET/subscriptions" \
        "$TARGET/tmp" 2>/dev/null || true
}

set_permissions()
{
    chmod 755 \
        "$TARGET/bin/"* \
        "$TARGET/lib/package-setup.sh" \
        /opt/etc/init.d/S23broray-monitor \
        /opt/etc/init.d/S24broray \
        /opt/etc/init.d/S25broray-web \
        /opt/etc/init.d/S27broray-auto-switch \
        /opt/etc/init.d/S28broray-subscriptions ||
        fail "Не удалось назначить права исполняемым файлам"

    find "$TARGET/web-new/api" \
        -type f \
        \( -name '*.cgi' -o -name '*.sh' \) \
        -exec chmod 755 {} \; ||
        fail "Не удалось назначить права CGI"

    chmod 600 \
        "$TARGET/config/system/settings.json" \
        "$TARGET/config/system/server-auto-switch.json" \
        2>/dev/null || true
}

create_command_links()
{
    for command_name in \
        broray \
        broray-routes \
        broray-routes-user \
        broray-server \
        broray-servers \
        broray-subscriptions \
        broray-system
    do
        [ -x "$TARGET/bin/$command_name" ] || continue

        ln -sf \
            "$TARGET/bin/$command_name" \
            "/opt/bin/$command_name" ||
            fail "Не удалось создать ссылку /opt/bin/$command_name"
    done
}

configure_local_address()
{
    network_library="$TARGET/lib/network.sh"
    settings_file="$TARGET/config/system/settings.json"
    settings_temp="$settings_file.opkg-new"

    [ -r "$network_library" ] ||
        fail "Не найден $network_library"

    . "$network_library"

    LAN_IP="$(broray_save_lan_ip)" ||
        fail "Не удалось определить LAN-IP"

    if ! jq -e 'type == "object"' \
        "$settings_file" >/dev/null 2>&1
    then
        fail "Некорректный файл настроек $settings_file"
    fi

    jq \
        --arg ip "$LAN_IP" \
        '.listenAddress = $ip' \
        "$settings_file" >"$settings_temp" ||
        fail "Не удалось обновить LAN-IP в настройках"

    jq -e . "$settings_temp" >/dev/null 2>&1 ||
        fail "Получен некорректный settings.json"

    chmod 600 "$settings_temp"
    mv "$settings_temp" "$settings_file" ||
        fail "Не удалось сохранить settings.json"

    printf 'LAN-IP: %s\n' "$LAN_IP"
}

configure_lighttpd()
{
    lighttpd_file="$TARGET/config/lighttpd.conf"
    lighttpd_temp="$lighttpd_file.opkg-new"
    backup_stamp="$(date '+%Y%m%d-%H%M%S')"

    if [ -f "$lighttpd_file" ]; then
        cp -p \
            "$lighttpd_file" \
            "$TARGET/backup/lighttpd.conf.before-$VERSION-$backup_stamp" \
            2>/dev/null || true
    fi

    cat >"$lighttpd_temp" <<EOF
server.modules = (
    "mod_cgi"
)

server.document-root = "/opt/broray/web-new"
server.bind = "$LAN_IP"
server.port = 8080
server.max-request-size = 5120

server.pid-file = "/opt/broray/run/lighttpd.pid"
server.errorlog = "/opt/broray/logs/lighttpd-error.log"

index-file.names = ( "index.html" )

mimetype.assign = (
    ".html" => "text/html; charset=utf-8",
    ".css"  => "text/css; charset=utf-8",
    ".js"   => "application/javascript; charset=utf-8",
    ".json" => "application/json; charset=utf-8",
    ".svg"  => "image/svg+xml",
    ".png"  => "image/png",
    ".ico"  => "image/x-icon"
)

cgi.assign = (
    ".cgi" => ""
)

static-file.exclude-extensions = (
    ".cgi"
)
EOF

    "$LIGHTTPD" -tt -f "$lighttpd_temp" ||
        fail "Новая конфигурация lighttpd некорректна"

    chmod 644 "$lighttpd_temp"
    mv "$lighttpd_temp" "$lighttpd_file" ||
        fail "Не удалось сохранить конфигурацию lighttpd"
}

configure_initial_xray()
{
    xray_config="$TARGET/config/config.json"

    if [ -f "$xray_config" ]; then
        printf '%s\n' "Существующая конфигурация Xray сохранена"
        return 0
    fi

    cat >"$xray_config" <<EOF
{
  "log": {
    "access": "$TARGET/logs/access.log",
    "error": "$TARGET/logs/error.log",
    "loglevel": "warning"
  },
  "inbounds": [
    {
      "tag": "socks",
      "listen": "$LAN_IP",
      "port": 2080,
      "protocol": "socks",
      "settings": {
        "auth": "noauth",
        "udp": true
      }
    }
  ],
  "outbounds": [
    {
      "tag": "proxy",
      "protocol": "blackhole",
      "settings": {
        "response": {
          "type": "none"
        }
      }
    }
  ]
}
EOF

    chmod 600 "$xray_config"

    jq -e . "$xray_config" >/dev/null 2>&1 ||
        fail "Начальная конфигурация Xray некорректна"

    printf '%s\n' "Создана безопасная начальная конфигурация Xray"
}

configure_auto_switch()
{
    auto_file="$TARGET/config/system/server-auto-switch.json"

    if jq -e 'type == "object"' "$auto_file" >/dev/null 2>&1; then
        chmod 600 "$auto_file"
        return 0
    fi

    cat >"$auto_file" <<'EOF'
{
  "schemaVersion": 2,
  "enabled": false,
  "failureThreshold": 3,
  "cooldownMinutes": 10,
  "minimumRating": "acceptable",
  "selectionRule": "best-quality",
  "preferredServerId": null,
  "updatedAt": null
}
EOF

    chmod 600 "$auto_file"
}

install_route_manifests()
{
    source_dir="$TARGET/share/routes/manifests"
    target_dir="$TARGET/routes/manifests"
    managed_interface="$(
        jq -r '.managedInterface // "Proxy0"' \
            "$TARGET/routes/config.json" 2>/dev/null
    )"

    case "$managed_interface" in
        Proxy[0-9]*)
            ;;
        *)
            managed_interface="Proxy0"
            ;;
    esac

    for source_file in "$source_dir"/*.json; do
        [ -f "$source_file" ] || continue

        target_file="$target_dir/${source_file##*/}"
        target_temp="$target_file.opkg-new"

        jq \
            --arg interface "$managed_interface" \
            '.targetInterface = $interface' \
            "$source_file" >"$target_temp" ||
            fail "Не удалось подготовить ${source_file##*/}"

        chmod 644 "$target_temp"
        mv "$target_temp" "$target_file" ||
            fail "Не удалось установить ${source_file##*/}"
    done
}

repair_route_runtime()
{
    runtime_library="$TARGET/lib/routes-runtime-repair.sh"

    [ -r "$runtime_library" ] ||
        fail "Не найден $runtime_library"

    BRORAY_ROOT="$TARGET"
    BRORAY_ROUTES_ROOT="$TARGET/routes"
    export BRORAY_ROOT BRORAY_ROUTES_ROOT

    . "$runtime_library"

    broray_routes_runtime_prepare ||
        fail "Не удалось подготовить runtime маршрутов"

    printf '%s
' "Runtime маршрутов проверен и подготовлен"
}

configure_web_proxy()
{
    ndmc -c "no ip http proxy broray" >/dev/null 2>&1 || true

    ndmc -c "ip http proxy broray" >/dev/null 2>&1 ||
        fail "Не удалось создать HTTP-прокси BROray"

    ndmc -c \
        "ip http proxy broray upstream http $LAN_IP 8080" \
        >/dev/null 2>&1 ||
        fail "Не удалось настроить upstream WebUI"

    ndmc -c "ip http proxy broray domain ndns" \
        >/dev/null 2>&1 ||
        fail "Не удалось привязать WebUI к KeenDNS"

    ndmc -c "ip http proxy broray ssl redirect" \
        >/dev/null 2>&1 ||
        fail "Не удалось включить перенаправление HTTPS"

    ndmc -c "ip http proxy broray security-level public" \
        >/dev/null 2>&1 ||
        fail "Не удалось настроить уровень доступа WebUI"

    ndmc -c "system configuration save" >/dev/null 2>&1 ||
        fail "Не удалось сохранить конфигурацию KeeneticOS"
}

configure_proxy_interface()
{
    interface_script="$TARGET/lib/interface.sh"

    [ -r "$interface_script" ] ||
        fail "Не найден модуль ProxyN"

    /opt/bin/ash "$interface_script" repair ||
        fail "Не удалось создать или восстановить прокси-интерфейс Keenetic"
}

validate_shell_files()
{
    validation_list="$TARGET/tmp/package-shell-files.$$"

    {
        find "$TARGET/bin" \
            -type f \
            ! -name 'xray'
        find "$TARGET/lib" \
            -type f \
            -name '*.sh'
        find "$TARGET/web-new/api" \
            -type f \
            \( -name '*.cgi' -o -name '*.sh' \)
        find /opt/etc/init.d \
            -maxdepth 1 \
            -type f \
            -name 'S2*broray*'
    } | sort -u >"$validation_list"

    while IFS= read -r shell_file; do
        [ -f "$shell_file" ] || continue

        /opt/bin/ash -n "$shell_file" ||
            fail "Ошибка синтаксиса: $shell_file"
    done <"$validation_list"

    rm -f "$validation_list"
}

validate_release()
{
    for required_file in \
        "$TARGET/bin/broray" \
        "$TARGET/bin/broray-routes" \
        "$TARGET/bin/broray-routes-user" \
        "$TARGET/bin/broray-servers" \
        "$TARGET/bin/broray-subscriptions" \
        "$TARGET/bin/xray" \
        "$TARGET/lib/interface-owner.sh" \
        "$TARGET/lib/routes-router-sync.sh" \
        "$TARGET/lib/routes-runtime-repair.sh" \
        "$TARGET/lib/routes-user-import.sh" \
        "$TARGET/lib/package-setup.sh" \
        "$TARGET/web-new/index.html" \
        "$TARGET/web-new/home.html" \
        "$TARGET/web-new/api/session.cgi" \
        "$TARGET/web-new/api/routes/plan.cgi" \
        "$TARGET/web-new/api/routes/custom-preview.cgi" \
        "$TARGET/web-new/api/routes/custom-commit.cgi"
    do
        [ -f "$required_file" ] ||
            fail "Не найден обязательный файл: $required_file"
    done

    validate_shell_files

    "$LIGHTTPD" -tt -f "$TARGET/config/lighttpd.conf" ||
        fail "Конфигурация lighttpd некорректна"

    XRAY_LOCATION_ASSET="$TARGET/bin" \
        "$TARGET/bin/xray" \
        run \
        -test \
        -c "$TARGET/config/config.json" \
        >/dev/null 2>&1 ||
        fail "Конфигурация Xray некорректна"

    [ "$("$TARGET/bin/broray" version)" = "$PRODUCT $VERSION" ] ||
        fail "Версия CLI не совпадает с версией пакета"
}

start_services()
{
    /opt/etc/init.d/S24broray start ||
        fail "Не удалось запустить Xray"

    /opt/etc/init.d/S23broray-monitor start ||
        fail "Не удалось запустить монитор соединения"

    /opt/etc/init.d/S27broray-auto-switch start ||
        fail "Не удалось запустить службу автовыбора"

    /opt/etc/init.d/S28broray-subscriptions start ||
        fail "Не удалось запустить обновление подписок"

    /opt/etc/init.d/S25broray-web start ||
        fail "Не удалось запустить WebUI"
}

validate_services()
{
    sleep 1

    for service in \
        /opt/etc/init.d/S23broray-monitor \
        /opt/etc/init.d/S24broray \
        /opt/etc/init.d/S25broray-web \
        /opt/etc/init.d/S27broray-auto-switch \
        /opt/etc/init.d/S28broray-subscriptions
    do
        [ -x "$service" ] ||
            fail "Не найдена служба: $service"

        "$service" status >/dev/null 2>&1 ||
            fail "Служба не работает: ${service##*/}"
    done

    printf '%s\n' "Все пять служб BROray работают"
}

validate_webui()
{
    attempt=1

    while [ "$attempt" -le 10 ]; do
        if curl \
            -fsS \
            --max-time 5 \
            "http://$LAN_IP:8080/" \
            >/dev/null 2>&1
        then
            printf '%s\n' "WebUI отвечает"
            return 0
        fi

        sleep 1
        attempt=$((attempt + 1))
    done

    fail "WebUI не отвечает по адресу http://$LAN_IP:8080/"
}

print_result()
{
    printf '\n%s %s установлен через OPKG\n' "$PRODUCT" "$VERSION"
    printf 'WebUI: http://%s:8080/\n' "$LAN_IP"
    printf 'Документация: https://docs.brovibe.cloud/broray/\n'
    printf 'Исходный код: https://github.com/BROadmin/BROray\n'
}

[ -z "${IPKG_INSTROOT:-}" ] || exit 0

for required_command in \
    ash \
    awk \
    curl \
    find \
    grep \
    ip \
    jq \
    lighttpd \
    ln \
    mkdir \
    ndmc \
    sed \
    sha256sum \
    sleep
do
    require_command "$required_command"
done

case "$(uname -m 2>/dev/null)" in
    aarch64|arm64)
        ;;
    *)
        fail "Поддерживается только архитектура ARM64"
        ;;
esac

LIGHTTPD="$(command -v lighttpd)"

create_runtime_directories
set_permissions
create_command_links
configure_local_address
configure_lighttpd
configure_initial_xray
configure_auto_switch
install_route_manifests
repair_route_runtime
configure_web_proxy
configure_proxy_interface
validate_release
start_services
validate_services
validate_webui
print_result
