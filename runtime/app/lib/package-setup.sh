#!/opt/bin/ash

# BROray 3.0.0 package finalizer for the universal transaction runner.

set -eu
umask 077

BRORAY_SETUP_PRODUCT="BROray"
BRORAY_SETUP_VERSION="3.0.0"
BRORAY_SETUP_PACKAGE_VERSION="3.0.0-r14"
BRORAY_SETUP_TARGET="${BRORAY_PACKAGE_TARGET:-/opt/broray}"
BRORAY_SETUP_OPT_ROOT="${BRORAY_OPT_ROOT:-/opt}"
BRORAY_SETUP_OPT_BIN="${BRORAY_OPT_BIN:-$BRORAY_SETUP_OPT_ROOT/bin}"
BRORAY_SETUP_INIT_ROOT="${BRORAY_INIT_ROOT:-$BRORAY_SETUP_OPT_ROOT/etc/init.d}"
PATH="$BRORAY_SETUP_OPT_ROOT/bin:$BRORAY_SETUP_OPT_ROOT/sbin:$BRORAY_SETUP_OPT_ROOT/usr/bin:$BRORAY_SETUP_OPT_ROOT/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin"
export PATH
BRORAY_BASE="$BRORAY_SETUP_TARGET"
BRORAY_ROOT="$BRORAY_SETUP_TARGET"
export BRORAY_BASE BRORAY_ROOT
BRORAY_SETUP_NDMC="${BRORAY_NDMC:-ndmc}"
BRORAY_SETUP_ASH="${BRORAY_ASH:-}"
BRORAY_SETUP_LIGHTTPD="${BRORAY_LIGHTTPD:-}"
BRORAY_SETUP_LAN_IP=""
BRORAY_SETUP_SUCCESS=0
BRORAY_SETUP_STATE_FILE="$BRORAY_SETUP_TARGET/run/package-setup.json"
BRORAY_SETUP_LOG_FILE="$BRORAY_SETUP_TARGET/logs/package-setup.log"
BRORAY_SETUP_SKIP_KEENETIC="${BRORAY_PACKAGE_SKIP_KEENETIC:-0}"
BRORAY_SETUP_SKIP_SERVICES="${BRORAY_PACKAGE_SKIP_SERVICES:-0}"
BRORAY_SETUP_SKIP_MAINTENANCE="${BRORAY_PACKAGE_SKIP_MAINTENANCE:-0}"
BRORAY_SETUP_SKIP_DNS_MIGRATION="${BRORAY_PACKAGE_SKIP_DNS_MIGRATION:-0}"
BRORAY_SETUP_PRESERVE_EXISTING="${BRORAY_PACKAGE_PRESERVE_EXISTING:-0}"
BRORAY_SETUP_WRITE_CONTRACT="candidate-bound-preserve-no-services-max64x32KiB/1"

# The universal transaction runs the exact candidate-bound script only on
# this finite-write path.  Services are started by the transaction after the
# bounded setup child exits, so they do not inherit its RLIMIT_FSIZE.
if [ -n "${BRORAY_PACKAGE_WRITE_CONTRACT:-}" ]; then
    [ "$BRORAY_PACKAGE_WRITE_CONTRACT" = "$BRORAY_SETUP_WRITE_CONTRACT" ] &&
        [ "$BRORAY_SETUP_SKIP_KEENETIC" = 1 ] && [ "$BRORAY_SETUP_SKIP_SERVICES" = 1 ] &&
        [ "$BRORAY_SETUP_SKIP_MAINTENANCE" = 1 ] && [ "$BRORAY_SETUP_SKIP_DNS_MIGRATION" = 1 ] &&
        [ "$BRORAY_SETUP_PRESERVE_EXISTING" = 1 ] || exit 78
fi

now()
{
    date -u '+%Y-%m-%dT%H:%M:%SZ'
}

state_write()
{
    local state_status state_stage state_message state_temp
    state_status="$1"
    state_stage="$2"
    state_message="$3"
    state_temp="$BRORAY_SETUP_STATE_FILE.new.$$"

    mkdir -p "$(dirname "$BRORAY_SETUP_STATE_FILE")" 2>/dev/null || return 0
    jq -n \
        --arg status "$state_status" \
        --arg stage "$state_stage" \
        --arg message "$state_message" \
        --arg version "$BRORAY_SETUP_VERSION" \
        --arg updatedAt "$(now)" \
        '{schemaVersion:1,status:$status,stage:$stage,message:$message,version:$version,updatedAt:$updatedAt}' \
        >"$state_temp" 2>/dev/null || { rm -f "$state_temp"; return 0; }
    chmod 600 "$state_temp" 2>/dev/null || true
    mv -f "$state_temp" "$BRORAY_SETUP_STATE_FILE" 2>/dev/null || true
}

fail()
{
    state_write error "${BRORAY_SETUP_STAGE:-unknown}" "$*"
    printf 'ОШИБКА: %s\n' "$*" >&2
    exit 1
}

cleanup()
{
    # BusyBox ash resets `$?` to zero when `local` is executed as a separate
    # command.  Capturing the status after `local rc` therefore made every
    # setup failure look successful to OPKG and to the outer transaction.
    # Keep the trap status in a uniquely named global before doing anything
    # else so a failed postinst can never be silently accepted.
    BRORAY_SETUP_EXIT_RC=$?
    trap - EXIT HUP INT TERM
    if [ "$BRORAY_SETUP_EXIT_RC" -ne 0 ] && [ "$BRORAY_SETUP_SUCCESS" -ne 1 ]; then
        state_write error "${BRORAY_SETUP_STAGE:-unknown}" "Установка не завершена; требуется автоматический откат установщика."
    fi
    exit "$BRORAY_SETUP_EXIT_RC"
}

trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

require_command()
{
    local command_name
    command_name="$1"
    command -v "$command_name" >/dev/null 2>&1 || fail "Не найдена команда: $command_name"
}

resolve_tools()
{
    if [ -z "$BRORAY_SETUP_ASH" ]; then
        BRORAY_SETUP_ASH="$(command -v ash 2>/dev/null || true)"
    fi
    if [ -z "$BRORAY_SETUP_ASH" ] && [ -x "$BRORAY_SETUP_OPT_BIN/ash" ]; then
        BRORAY_SETUP_ASH="$BRORAY_SETUP_OPT_BIN/ash"
    fi
    [ -n "$BRORAY_SETUP_ASH" ] && [ -x "$BRORAY_SETUP_ASH" ] || fail "Не найден BusyBox ash"

    if [ -z "$BRORAY_SETUP_LIGHTTPD" ]; then
        BRORAY_SETUP_LIGHTTPD="$(command -v lighttpd 2>/dev/null || true)"
    fi
    if [ -z "$BRORAY_SETUP_LIGHTTPD" ] && [ -x "$BRORAY_SETUP_OPT_ROOT/sbin/lighttpd" ]; then
        BRORAY_SETUP_LIGHTTPD="$BRORAY_SETUP_OPT_ROOT/sbin/lighttpd"
    fi
    [ -n "$BRORAY_SETUP_LIGHTTPD" ] && [ -x "$BRORAY_SETUP_LIGHTTPD" ] || fail "Не найден lighttpd"
}

create_runtime_directories()
{
    mkdir -p \
        "$BRORAY_SETUP_TARGET/backup" \
        "$BRORAY_SETUP_TARGET/backups" \
        "$BRORAY_SETUP_TARGET/config/disabled-subscription-servers" \
        "$BRORAY_SETUP_TARGET/config/subscriptions" \
        "$BRORAY_SETUP_TARGET/data" \
        "$BRORAY_SETUP_TARGET/deleted-subscriptions" \
        "$BRORAY_SETUP_TARGET/logs" \
        "$BRORAY_SETUP_TARGET/routes/backup" \
        "$BRORAY_SETUP_TARGET/routes/catalog" \
        "$BRORAY_SETUP_TARGET/routes/dot" \
        "$BRORAY_SETUP_TARGET/routes/installed/bundles" \
        "$BRORAY_SETUP_TARGET/routes/locks" \
        "$BRORAY_SETUP_TARGET/routes/manifests" \
        "$BRORAY_SETUP_TARGET/routes/operations" \
        "$BRORAY_SETUP_TARGET/routes/preflight" \
        "$BRORAY_SETUP_TARGET/routes/state" \
        "$BRORAY_SETUP_TARGET/routes/tmp/user-previews" \
        "$BRORAY_SETUP_TARGET/routes/transactions" \
        "$BRORAY_SETUP_TARGET/run/broray" \
        "$BRORAY_SETUP_TARGET/run/operations" \
        "$BRORAY_SETUP_TARGET/run/server-quality" \
        "$BRORAY_SETUP_TARGET/run/subscriptions" \
        "$BRORAY_SETUP_TARGET/run/web-new/sessions" \
        "$BRORAY_SETUP_TARGET/servers" \
        "$BRORAY_SETUP_TARGET/snapshots" \
        "$BRORAY_SETUP_TARGET/subscriptions" \
        "$BRORAY_SETUP_TARGET/tmp" \
        "$BRORAY_SETUP_TARGET/update" \
        "$BRORAY_SETUP_OPT_ROOT/var/lib/broray/operations" \
        "$BRORAY_SETUP_OPT_BIN" || fail "Не удалось создать рабочие каталоги"

    # All setup scratch files stay under the newly owned application tree.
    # Never probe a predictable root-owned path in the shared /tmp namespace.
    [ -d "$BRORAY_SETUP_TARGET/tmp" ] && [ ! -L "$BRORAY_SETUP_TARGET/tmp" ] ||
        fail "Недоступен приватный каталог временных файлов BROray"

    chmod 700 \
        "$BRORAY_SETUP_TARGET/backup" \
        "$BRORAY_SETUP_TARGET/backups" \
        "$BRORAY_SETUP_TARGET/config/disabled-subscription-servers" \
        "$BRORAY_SETUP_TARGET/config/subscriptions" \
        "$BRORAY_SETUP_TARGET/deleted-subscriptions" \
        "$BRORAY_SETUP_TARGET/run/broray" \
        "$BRORAY_SETUP_TARGET/run/operations" \
        "$BRORAY_SETUP_TARGET/run/web-new/sessions" \
        "$BRORAY_SETUP_TARGET/servers" \
        "$BRORAY_SETUP_TARGET/snapshots" \
        "$BRORAY_SETUP_TARGET/subscriptions" \
        "$BRORAY_SETUP_TARGET/tmp" \
        "$BRORAY_SETUP_OPT_ROOT/var/lib/broray" \
        "$BRORAY_SETUP_OPT_ROOT/var/lib/broray/operations" 2>/dev/null || true
}

set_permissions()
{
    local permission_list permission_file
    find "$BRORAY_SETUP_TARGET/bin" -maxdepth 1 -type f ! -name xray -exec chmod 755 {} \; ||
        fail "Не удалось назначить права исполняемым файлам"
    [ ! -f "$BRORAY_SETUP_TARGET/bin/xray" ] || chmod 755 "$BRORAY_SETUP_TARGET/bin/xray" || fail "Не удалось назначить права Xray"

    chmod 755 \
        "$BRORAY_SETUP_TARGET/lib/package-setup.sh" \
        "$BRORAY_SETUP_INIT_ROOT/S23broray-monitor" \
        "$BRORAY_SETUP_INIT_ROOT/S24broray" \
        "$BRORAY_SETUP_INIT_ROOT/S25broray-web" \
        "$BRORAY_SETUP_INIT_ROOT/S27broray-auto-switch" \
        "$BRORAY_SETUP_INIT_ROOT/S28broray-subscriptions" ||
        fail "Не удалось назначить права службам"

    permission_list="$BRORAY_SETUP_TARGET/tmp/package-permissions-$$"
    find "$BRORAY_SETUP_TARGET/web-new/api" -type f \( -name '*.cgi' -o -name '*.sh' \) \
        | sort -u >"$permission_list" || fail "Не удалось составить список CGI"
    while IFS= read -r permission_file; do
        [ -f "$permission_file" ] || {
            rm -f "$permission_list"
            fail "CGI исчез во время установки: $permission_file"
        }
        chmod 755 "$permission_file" || {
            rm -f "$permission_list"
            fail "Не удалось назначить права CGI: $permission_file"
        }
    done <"$permission_list"
    rm -f "$permission_list"

    chmod 600 \
        "$BRORAY_SETUP_TARGET/config/system/settings.json" \
        "$BRORAY_SETUP_TARGET/config/system/server-auto-switch.json" 2>/dev/null || true

    # routes/config.json is protected user state. Preserve its admitted
    # metadata exactly instead of normalizing it during every setup run.
}

create_command_links()
{
    local command_name
    for command_name in \
        broray \
        broray-routes \
        broray-routes-dot \
        broray-routes-user \
        broray-server \
        broray-servers \
        broray-subscriptions \
        broray-system
    do
        [ -x "$BRORAY_SETUP_TARGET/bin/$command_name" ] || continue
        ln -sf "$BRORAY_SETUP_TARGET/bin/$command_name" "$BRORAY_SETUP_OPT_BIN/$command_name" ||
            fail "Не удалось создать ссылку $BRORAY_SETUP_OPT_BIN/$command_name"
    done
}

configure_local_address()
{
    local network_library settings_file settings_temp current_ip
    network_library="$BRORAY_SETUP_TARGET/lib/network.sh"
    settings_file="$BRORAY_SETUP_TARGET/config/system/settings.json"
    settings_temp="$settings_file.opkg-new"

    [ -r "$network_library" ] || fail "Не найден $network_library"
    BRORAY_SETUP_LAN_IP="$(
        BRORAY_ROOT="$BRORAY_SETUP_TARGET" \
        BRORAY_NETWORK_ROOT="$BRORAY_SETUP_TARGET" \
        BRORAY_NETWORK_LIBRARY="$network_library" \
            "$BRORAY_SETUP_ASH" -c '. "$BRORAY_NETWORK_LIBRARY"; broray_save_lan_ip'
    )" || fail "Не удалось определить LAN-IP"
    jq -e 'type == "object"' "$settings_file" >/dev/null 2>&1 ||
        fail "Некорректный файл настроек $settings_file"

    current_ip="$(jq -r '.listenAddress // empty' "$settings_file" 2>/dev/null)"
    if [ "$current_ip" != "$BRORAY_SETUP_LAN_IP" ]; then
        jq --arg ip "$BRORAY_SETUP_LAN_IP" '.listenAddress=$ip' "$settings_file" >"$settings_temp" ||
            fail "Не удалось обновить LAN-IP в настройках"
        jq -e . "$settings_temp" >/dev/null 2>&1 || fail "Получен некорректный settings.json"
        chmod 600 "$settings_temp"
        mv -f "$settings_temp" "$settings_file" || fail "Не удалось сохранить settings.json"
    else
        rm -f "$settings_temp"
    fi
    printf 'LAN-IP: %s\n' "$BRORAY_SETUP_LAN_IP"
}

load_preserved_local_address()
{
    BRORAY_SETUP_LAN_IP="$(jq -r '.listenAddress // empty' "$BRORAY_SETUP_TARGET/config/system/settings.json" 2>/dev/null)"
    case "$BRORAY_SETUP_LAN_IP" in
        ''|127.*|0.0.0.0|255.*|*[!0-9.]*) fail "В сохранённых настройках отсутствует доказанный LAN-IP" ;;
    esac
    printf 'LAN-IP preserved: %s\n' "$BRORAY_SETUP_LAN_IP"
}

configure_lighttpd()
{
    local lighttpd_file lighttpd_temp
    lighttpd_file="$BRORAY_SETUP_TARGET/config/lighttpd.conf"
    lighttpd_temp="$lighttpd_file.opkg-new"

    cat >"$lighttpd_temp" <<EOF_LIGHTTPD
server.modules = (
    "mod_cgi"
)

server.document-root = "$BRORAY_SETUP_TARGET/web-new"
server.bind = "$BRORAY_SETUP_LAN_IP"
server.port = 8080
server.max-request-size = 5120

server.pid-file = "$BRORAY_SETUP_TARGET/run/lighttpd.pid"
server.errorlog = "$BRORAY_SETUP_TARGET/logs/lighttpd-error.log"

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
EOF_LIGHTTPD

    "$BRORAY_SETUP_LIGHTTPD" -tt -f "$lighttpd_temp" || fail "Новая конфигурация lighttpd некорректна"
    chmod 644 "$lighttpd_temp"
    mv -f "$lighttpd_temp" "$lighttpd_file" || fail "Не удалось сохранить конфигурацию lighttpd"
}

configure_initial_xray()
{
    local xray_config
    xray_config="$BRORAY_SETUP_TARGET/config/config.json"
    [ ! -f "$xray_config" ] || { printf '%s\n' "Существующая конфигурация Xray сохранена"; return 0; }

    cat >"$xray_config" <<EOF_XRAY
{
  "log": {
    "access": "$BRORAY_SETUP_TARGET/logs/access.log",
    "error": "$BRORAY_SETUP_TARGET/logs/error.log",
    "loglevel": "warning"
  },
  "inbounds": [
    {
      "tag": "socks",
      "listen": "$BRORAY_SETUP_LAN_IP",
      "port": 2080,
      "protocol": "socks",
      "settings": {"auth":"noauth","udp":true}
    }
  ],
  "outbounds": [
    {"tag":"proxy","protocol":"blackhole","settings":{"response":{"type":"none"}}}
  ]
}
EOF_XRAY
    chmod 600 "$xray_config"
    jq -e . "$xray_config" >/dev/null 2>&1 || fail "Начальная конфигурация Xray некорректна"
}

configure_auto_switch()
{
    local auto_file
    auto_file="$BRORAY_SETUP_TARGET/config/system/server-auto-switch.json"
    if jq -e 'type == "object"' "$auto_file" >/dev/null 2>&1; then
        chmod 600 "$auto_file"
        return 0
    fi
    cat >"$auto_file" <<'EOF_AUTO'
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
EOF_AUTO
    chmod 600 "$auto_file"
}

install_route_manifests()
{
    local source_dir target_dir managed_interface source_file target_file target_temp
    source_dir="$BRORAY_SETUP_TARGET/share/routes/manifests"
    target_dir="$BRORAY_SETUP_TARGET/routes/manifests"
    managed_interface="$(jq -r '.managedInterface // "Proxy0"' "$BRORAY_SETUP_TARGET/routes/config.json" 2>/dev/null || true)"
    case "$managed_interface" in Proxy[0-9]*) ;; *) managed_interface=Proxy0 ;; esac

    for source_file in "$source_dir"/*.json; do
        [ -f "$source_file" ] || continue
        target_file="$target_dir/${source_file##*/}"
        target_temp="$target_file.opkg-new"
        jq --arg interface "$managed_interface" '.targetInterface=$interface' "$source_file" >"$target_temp" ||
            fail "Не удалось подготовить ${source_file##*/}"
        chmod 644 "$target_temp"
        mv -f "$target_temp" "$target_file" || fail "Не удалось установить ${source_file##*/}"
    done
}

repair_route_runtime()
{
    local runtime_library
    runtime_library="$BRORAY_SETUP_TARGET/lib/routes-runtime-repair.sh"
    [ -r "$runtime_library" ] || fail "Не найден $runtime_library"
    BRORAY_ROOT="$BRORAY_SETUP_TARGET" \
    BRORAY_ROUTES_ROOT="$BRORAY_SETUP_TARGET/routes" \
    BRORAY_RUNTIME_LIBRARY="$runtime_library" \
        "$BRORAY_SETUP_ASH" -c '. "$BRORAY_RUNTIME_LIBRARY"; broray_routes_runtime_prepare' ||
        fail "Не удалось подготовить runtime маршрутов"
}

migrate_dns_config()
{
    local dot_library
    dot_library="$BRORAY_SETUP_TARGET/lib/routes-dot.sh"
    [ -r "$dot_library" ] || fail "Не найден $dot_library"
    BRORAY_ROOT="$BRORAY_SETUP_TARGET" \
    BRORAY_DOT_ROOT="$BRORAY_SETUP_TARGET/routes/dot" \
    BRORAY_DOT_NDMC="$BRORAY_SETUP_NDMC" \
    BRORAY_DOT_LIBRARY="$dot_library" \
        "$BRORAY_SETUP_ASH" -c '. "$BRORAY_DOT_LIBRARY"; broray_dot_ensure_files' ||
        fail "Не удалось проверить или безопасно мигрировать DNS-over-TLS"
}

configure_web_proxy()
{
    local web_publish_library
    [ "$BRORAY_SETUP_SKIP_KEENETIC" = 1 ] && return 0
    web_publish_library="$BRORAY_SETUP_TARGET/lib/web-publish.sh"
    [ -r "$web_publish_library" ] || fail "Не найден $web_publish_library"
    mkdir -p "$BRORAY_SETUP_TARGET/run" || fail "Не удалось подготовить runtime LAN-IP"
    printf '%s\n' "$BRORAY_SETUP_LAN_IP" >"$BRORAY_SETUP_TARGET/run/lan-ip" ||
        fail "Не удалось передать LAN-IP ownership-safe HTTP Proxy модулю"
    chmod 600 "$BRORAY_SETUP_TARGET/run/lan-ip" 2>/dev/null || true

    BRORAY_WEB_PUBLISH_ROOT="$BRORAY_SETUP_TARGET"
    BRORAY_WEB_PUBLISH_NDMC="$BRORAY_SETUP_NDMC"
    BRORAY_WEB_PUBLISH_NAME=broray
    BRORAY_WEB_PUBLISH_PORT=8080
    BRORAY_WEB_PUBLISH_OWNER="$BRORAY_SETUP_TARGET/config/web-publish.json"
    . "$web_publish_library" || fail "Не удалось загрузить ownership-safe HTTP Proxy модуль"
    broray_web_publish_ensure || fail "Ownership-safe Keenetic HTTP Proxy setup failed"
}

configure_proxy_interface()
{
    local interface_script
    [ "$BRORAY_SETUP_SKIP_KEENETIC" = 1 ] && return 0
    interface_script="$BRORAY_SETUP_TARGET/lib/interface.sh"
    [ -r "$interface_script" ] || fail "Не найден модуль управляемого ProxyN"

    BRORAY_BASE="$BRORAY_SETUP_TARGET" "$BRORAY_SETUP_ASH" "$interface_script" check >/dev/null 2>&1 && {
        printf '%s\n' "Управляемый ProxyN уже соответствует рабочей конфигурации"
        return 0
    }

    BRORAY_BASE="$BRORAY_SETUP_TARGET" "$BRORAY_SETUP_ASH" "$interface_script" repair ||
        fail "Не удалось создать или восстановить управляемый ProxyN"
}

run_log_maintenance()
{
    local maintenance
    [ "$BRORAY_SETUP_SKIP_MAINTENANCE" = 1 ] && return 0
    maintenance="$BRORAY_SETUP_TARGET/bin/broray-log-maintenance"
    [ -x "$maintenance" ] || fail "Не найден модуль обслуживания журналов"
    BRORAY_ROOT="$BRORAY_SETUP_TARGET" "$BRORAY_SETUP_ASH" "$maintenance" --once || fail "Не удалось безопасно обслужить журналы"
}

validate_shell_files()
{
    local validation_list shell_file
    validation_list="$BRORAY_SETUP_TARGET/tmp/package-shell-files.$$"
    {
        find "$BRORAY_SETUP_TARGET/bin" -type f ! -name xray
        find "$BRORAY_SETUP_TARGET/lib" -type f -name '*.sh'
        find "$BRORAY_SETUP_TARGET/web-new/api" -type f \( -name '*.cgi' -o -name '*.sh' \)
        find "$BRORAY_SETUP_INIT_ROOT" -maxdepth 1 -type f -name 'S2*broray*'
    } | sort -u >"$validation_list"

    while IFS= read -r shell_file; do
        [ -f "$shell_file" ] || continue
        "$BRORAY_SETUP_ASH" -n "$shell_file" || fail "Ошибка синтаксиса: $shell_file"
    done <"$validation_list"
    rm -f "$validation_list"
}

validate_json_files()
{
    find "$BRORAY_SETUP_TARGET" -type f -name '*.json' | while IFS= read -r json_file; do
        json_relative="${json_file#"$BRORAY_SETUP_TARGET"/}"
        # Clean replacement restored these registered user roots byte-for-
        # byte.  They are opaque migration inputs, not candidate JSON: an
        # empty placeholder and even application-invalid content must reach
        # runtime postchecks, whose failure triggers the current snapshot
        # rollback, rather than being silently changed or version-selected.
        case "$json_relative" in
            backup|backup/*|config/config.json|config/interface.json) continue ;;
            config/subscriptions|config/subscriptions/*) continue ;;
            config/disabled-subscription-servers|config/disabled-subscription-servers/*) continue ;;
            config/system/settings.json|config/system/server-auto-switch.json) continue ;;
            config/system/dns.json|config/system/dot.json) continue ;;
            config/dns|config/dns/*|config/dot|config/dot/*) continue ;;
            data|data/*|deleted-subscriptions|deleted-subscriptions/*) continue ;;
            subscriptions|subscriptions/*|servers|servers/*) continue ;;
            routes/config.json|routes/bundles.json|routes/custom.json) continue ;;
            routes/catalog|routes/catalog/*|routes/dot/config.json|routes/dot/state.json) continue ;;
            routes/installed|routes/installed/*|routes/state|routes/state/*) continue ;;
            routes/backup|routes/backup/*|routes/manifests/user-*.json) continue ;;
        esac
        jq -e . "$json_file" >/dev/null 2>&1 || {
            printf 'Некорректный JSON: %s\n' "$json_file" >&2
            exit 1
        }
    done || fail "Обнаружен некорректный JSON"
}

validate_release()
{
    local required_file
    for required_file in \
        "$BRORAY_SETUP_TARGET/bin/broray" \
        "$BRORAY_SETUP_TARGET/bin/xray" \
        "$BRORAY_SETUP_TARGET/lib/package-transaction.sh" \
        "$BRORAY_SETUP_TARGET/lib/runtime-capabilities.sh" \
        "$BRORAY_SETUP_TARGET/lib/broray-page.sh" \
        "$BRORAY_SETUP_TARGET/share/release/manifest.json" \
        "$BRORAY_SETUP_TARGET/share/release/requirements-traceability.json" \
        "$BRORAY_SETUP_TARGET/web-new/build.json"
    do [ -s "$required_file" ] || fail "Не найден обязательный файл: $required_file"; done
    validate_shell_files
    validate_json_files
    "$BRORAY_SETUP_LIGHTTPD" -tt -f "$BRORAY_SETUP_TARGET/config/lighttpd.conf" || fail "Конфигурация lighttpd некорректна"
    XRAY_LOCATION_ASSET="$BRORAY_SETUP_TARGET/bin" "$BRORAY_SETUP_TARGET/bin/xray" run -test -c "$BRORAY_SETUP_TARGET/config/config.json" >/dev/null 2>&1 || fail "Конфигурация Xray некорректна"
    [ "$("$BRORAY_SETUP_TARGET/bin/broray" version)" = "BROray 3.0.0" ] || fail "Версия CLI не совпадает с пакетом"
    jq -e --arg release "3.0.0-r14" --arg package "3.0.0-r14" --arg build "WebUI-3.0.0-r15c16" '
      .schemaVersion==3 and .releaseId==$release and .packageVersion==$package and .webUIBuild==$build and
      .candidateId=="3.0.0-r15c16" and .updaterEngine=="broray-updater/5" and
      .lifecycleContract=="compact-app-rename/1" and
      .previousIpkRequired==false and .historicalTransactionStateRequired==false and .statelessBootstrap==false and
      .rollbackStorage=="/opt/broray/releases one compact app rollback" and .cleanReplacement==true and
      .requirementsContract=="1.7.2" and
      .capabilityContract=="keenetic-entware-capabilities/1" and
      .spaceContract=="broray-space/2" and
      .sourceAdmission=="bro-any-structural" and .versionMatrixRequired==false
    ' "$BRORAY_SETUP_TARGET/share/release/manifest.json" >/dev/null 2>&1 || fail "Release manifest не совпадает с кандидатом"
    jq -e --arg build "WebUI-3.0.0-r15c16" --arg release "3.0.0-r14" '
      .buildId==$build and .releaseId==$release and .requirementsContract=="1.7.2" and
      .candidateId=="3.0.0-r15c16" and .updaterEngine=="broray-updater/5" and
      .lifecycleContract=="compact-app-rename/1" and
      .capabilityContract=="keenetic-entware-capabilities/1" and .spaceContract=="broray-space/2" and
      .previousIpkRequired==false and .historicalTransactionStateRequired==false and .statelessBootstrap==false
    ' "$BRORAY_SETUP_TARGET/web-new/build.json" >/dev/null 2>&1 || fail "Версия WebUI не совпадает с релизом"
    jq -e '.schemaVersion==1 and .candidateId=="3.0.0-r15c16" and
      .lifecycleContract=="compact-app-rename/1" and .updaterEngine=="broray-updater/5" and
      .activeUpdatePath==[
        "/opt/bin/broray-updaterctl",
        "/opt/libexec/broray-updater/broray-updater.sh",
        "/opt/broray/current",
        "/opt/broray/runtime/xray"
      ] and
      (.forbiddenUpdatePath|index("opkg"))!=null and
      (.forbiddenUpdatePath|index("legacy overlay bridge"))!=null and
      (.forbiddenUpdatePath|index("universal transaction"))!=null' \
      "$BRORAY_SETUP_TARGET/share/release/requirements-traceability.json" >/dev/null 2>&1 || fail "Трассировка требований не прошла проверку"
}

broray_setup_desired_service_state()
{
    local service_name service_state
    service_name="$1"
    [ -f "${BRORAY_SETUP_SERVICE_STATE_FILE:-}" ] && [ ! -L "$BRORAY_SETUP_SERVICE_STATE_FILE" ] ||
        fail "Не найдено доказанное исходное состояние служб"
    service_state="$(awk -F '\t' -v name="$service_name" '$1==name{print $2; count++} END{if(count!=1)exit 1}' "$BRORAY_SETUP_SERVICE_STATE_FILE")" ||
        fail "Неоднозначное состояние службы: $service_name"
    case "$service_state" in
        running|stopped) printf '%s\n' "$service_state" ;;
        absent)
            case "$service_name" in
                S24broray|S25broray-web) printf '%s\n' running ;;
                *) printf '%s\n' stopped ;;
            esac
            ;;
        *) fail "Некорректное состояние службы: $service_name" ;;
    esac
}

start_services()
{
    local service service_name desired_state
    [ "$BRORAY_SETUP_SKIP_SERVICES" = 1 ] && return 0
    for service in \
        "$BRORAY_SETUP_INIT_ROOT/S24broray" \
        "$BRORAY_SETUP_INIT_ROOT/S23broray-monitor" \
        "$BRORAY_SETUP_INIT_ROOT/S27broray-auto-switch" \
        "$BRORAY_SETUP_INIT_ROOT/S28broray-subscriptions" \
        "$BRORAY_SETUP_INIT_ROOT/S25broray-web"
    do
        [ -x "$service" ] || fail "Не найдена служба: $service"
        service_name="${service##*/}"
        desired_state="$(broray_setup_desired_service_state "$service_name")"
        if [ "$desired_state" = running ]; then
            "$service" start || fail "Не удалось запустить $service_name"
        else
            "$service" stop >/dev/null 2>&1 || true
        fi
    done
}

validate_services()
{
    local service service_name desired_state
    [ "$BRORAY_SETUP_SKIP_SERVICES" = 1 ] && return 0
    sleep 2
    for service in \
        "$BRORAY_SETUP_INIT_ROOT/S23broray-monitor" \
        "$BRORAY_SETUP_INIT_ROOT/S24broray" \
        "$BRORAY_SETUP_INIT_ROOT/S25broray-web" \
        "$BRORAY_SETUP_INIT_ROOT/S27broray-auto-switch" \
        "$BRORAY_SETUP_INIT_ROOT/S28broray-subscriptions"
    do
        service_name="${service##*/}"
        desired_state="$(broray_setup_desired_service_state "$service_name")"
        if [ "$desired_state" = running ]; then
            "$service" status >/dev/null 2>&1 || fail "Служба не работает: $service_name"
        else
            if "$service" status >/dev/null 2>&1; then
                fail "Служба должна оставаться остановленной: $service_name"
            fi
        fi
    done
}

broray_setup_local_http_probe()
{
    local address="$1" timeout="$2"
    case "$address" in ''|*[!0-9.]*) return 1 ;; esac
    case "$timeout" in ''|*[!0-9]*|0) return 1 ;; esac
    (
        unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY no_proxy
        curl -q -fsS --noproxy '*' --max-time "$timeout" "http://$address:8080/"
    )
}

validate_webui()
{
    local attempt
    [ "$BRORAY_SETUP_SKIP_SERVICES" = 1 ] && return 0
    attempt=1
    while [ "$attempt" -le 15 ]; do
        if broray_setup_local_http_probe "$BRORAY_SETUP_LAN_IP" 5 >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
        attempt=$((attempt + 1))
    done
    fail "WebUI не отвечает по адресу http://$BRORAY_SETUP_LAN_IP:8080/"
}

validate_runtime_contracts()
{
    [ "$BRORAY_SETUP_SKIP_SERVICES" = 1 ] && return 0
    BRORAY_ROOT="$BRORAY_SETUP_TARGET" BRORAY_RUN_DIR="$BRORAY_SETUP_TARGET/run" BRORAY_SETTINGS_FILE="$BRORAY_SETUP_TARGET/config/system/settings.json" \
        "$BRORAY_SETUP_ASH" -c '. "$BRORAY_ROOT/lib/xray.sh"; broray_xray_status_json' |
        jq -e '.success==true and .data.health.operational==true and .data.socks.active==true' >/dev/null 2>&1 ||
        fail "Xray или локальный SOCKS не прошли проверку"
}

validate_no_reboot_soak()
{
    local attempt interface_script
    [ "$BRORAY_SETUP_SKIP_SERVICES" = 1 ] && return 0
    interface_script="$BRORAY_SETUP_TARGET/lib/interface.sh"
    attempt=1
    while [ "$attempt" -le 6 ]; do
        validate_services
        validate_webui
        validate_runtime_contracts
        [ "$BRORAY_SETUP_SKIP_KEENETIC" = 1 ] || {
            "$BRORAY_SETUP_NDMC" -c 'show version' >/dev/null 2>&1 ||
                fail "KeeneticOS недоступна в проверке без перезагрузки"
            BRORAY_BASE="$BRORAY_SETUP_TARGET" "$BRORAY_SETUP_ASH" "$interface_script" check >/dev/null 2>&1 ||
                fail "Управляемый ProxyN потерял готовность до перезагрузки"
        }
        [ "$attempt" -eq 6 ] || sleep 3
        attempt=$((attempt + 1))
    done
    printf '%s\n' 'BRORAY_NO_REBOOT_SOAK=PASS durationAtLeastSeconds=27'
}

print_result()
{
    printf '\n%s %s установлен через OPKG\n' "$BRORAY_SETUP_PRODUCT" "$BRORAY_SETUP_VERSION"
    printf 'WebUI: http://%s:8080/\n' "$BRORAY_SETUP_LAN_IP"
    printf 'Сборка: WebUI-3.0.0-r15c16, канал staging\n'
}

[ -z "${IPKG_INSTROOT:-}" ] || exit 0

BRORAY_SETUP_STAGE=runtime
mkdir -p "$BRORAY_SETUP_TARGET/run" "$BRORAY_SETUP_TARGET/logs"
state_write running runtime "Подготовка установки BROray $BRORAY_SETUP_VERSION."

for required_command in awk curl date find grep ip jq ln mkdir mktemp readlink sed sha256sum sleep tar; do
    require_command "$required_command"
done
[ "$BRORAY_SETUP_SKIP_KEENETIC" = 1 ] || require_command "$BRORAY_SETUP_NDMC"
resolve_tools

# Architecture admission is performed by the candidate-bound OPKG capability
# gate (`opkg print-architecture`).  `uname -m` is diagnostic only and must
# never select compatibility or reject an otherwise proven environment.

BRORAY_SETUP_STAGE=directories
state_write running "$BRORAY_SETUP_STAGE" "Подготовка каталогов и прав."
create_runtime_directories
set_permissions
create_command_links

BRORAY_SETUP_STAGE=configuration
state_write running "$BRORAY_SETUP_STAGE" "Проверка локальной конфигурации."
if [ "$BRORAY_SETUP_PRESERVE_EXISTING" = 1 ]; then
    # Clean replacement already restored every registered user/config object
    # byte-for-byte.  Update setup may validate but must not re-detect LAN,
    # rewrite protected JSON, or run content migrations.
    load_preserved_local_address
else
    configure_local_address
    configure_lighttpd
    configure_initial_xray
    configure_auto_switch
    install_route_manifests
    repair_route_runtime
    if [ "$BRORAY_SETUP_SKIP_DNS_MIGRATION" != 1 ]; then
        migrate_dns_config
    fi
fi
configure_web_proxy
configure_proxy_interface

if [ "$BRORAY_SETUP_SKIP_MAINTENANCE" != 1 ]; then
    BRORAY_SETUP_STAGE=maintenance
    state_write running "$BRORAY_SETUP_STAGE" "Безопасная ротация журналов."
    run_log_maintenance
fi

BRORAY_SETUP_STAGE=validation
state_write running "$BRORAY_SETUP_STAGE" "Статическая проверка релиза."
validate_release

BRORAY_SETUP_STAGE=start
state_write running "$BRORAY_SETUP_STAGE" "Восстановление доказанного состояния служб."
start_services
validate_services
validate_webui
validate_runtime_contracts
validate_no_reboot_soak

BRORAY_SETUP_STAGE=complete
BRORAY_SETUP_SUCCESS=1
state_write success complete "BROray $BRORAY_SETUP_VERSION установлен и проверен."
print_result
exit 0
