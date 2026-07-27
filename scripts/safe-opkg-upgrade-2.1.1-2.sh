#!/opt/bin/ash
set -u
cd / || exit 1
OPT_ROOT="${BRORAY_OPT_ROOT:-/opt}"
TMP_ROOT="${BRORAY_TMP_ROOT:-/tmp}"
PATH="$OPT_ROOT/broray/bin:$OPT_ROOT/sbin:$OPT_ROOT/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

TARGET_VERSION="2.1.1-2"
TARGET_APP_VERSION="2.1.1"
PACKAGE_URL="https://api.brovibe.cloud/releases/opkg/aarch64-3.10/broray_2.1.1-2_aarch64-3.10.ipk"
PACKAGE_SHA="c7e627cb8aa73f0ae20a96e028af195037b8f087a4ac4caa98b99cfd580bc3fb"
CACHE_TAG="20260727-official-2.1.1-2-r1"
BRORAY_DIR="$OPT_ROOT/broray"
WORK="$TMP_ROOT/broray-safe-upgrade-2.1.1-2-$$"
LOG="$WORK/update.log"
NEW_IPK="$WORK/broray_2.1.1-2_aarch64-3.10.ipk"
BACKUP="$WORK/backup.tar.gz"
META="$WORK/meta"
MARKER="$TMP_ROOT/broray-opkg-prepared-update"
BACKUP_READY=false
INSTALL_STARTED=false
ROLLBACK_OK=false

SERVICES="
$OPT_ROOT/etc/init.d/S23broray-monitor
$OPT_ROOT/etc/init.d/S24broray
$OPT_ROOT/etc/init.d/S25broray-web
$OPT_ROOT/etc/init.d/S27broray-auto-switch
$OPT_ROOT/etc/init.d/S28broray-subscriptions
"
COMMANDS="broray broray-routes broray-routes-user broray-server broray-servers broray-subscriptions broray-system"

say() { printf '%s\n' "$*"; printf '%s\n' "$*" >>"$LOG" 2>/dev/null || true; }
cleanup_work() { rm -f "$MARKER"; rm -rf "$WORK" 2>/dev/null || true; }
save_log() {
    [ -d "$OPT_ROOT/broray/logs" ] || return 0
    tail -n 500 "$LOG" >"$OPT_ROOT/broray/logs/update-last.log" 2>/dev/null || true
}
finish() {
    rc="$1"
    save_log
    if [ "$rc" -eq 0 ] || [ "$ROLLBACK_OK" = true ] || [ "$BACKUP_READY" = false ]; then cleanup_work; fi
    printf '\nТерминал остаётся открытым.\n'
    exit "$rc"
}

installed_version() {
    opkg list-installed broray 2>/dev/null | awk -F ' - ' '$1 == "broray" {print $2; exit}'
}

download_file() {
    url="$1"; out="$2"
    rm -f "$out" "$out.part"
    if [ -x "$OPT_ROOT/bin/curl" ]; then
        "$OPT_ROOT/bin/curl" -fL -H 'Accept-Encoding: identity' -H 'Cache-Control: no-cache, no-store' \
            --connect-timeout 20 --max-time 600 -o "$out.part" "$url?v=$CACHE_TAG-$(date +%s)-$$" >>"$LOG" 2>&1 || return 1
    elif [ -x "$OPT_ROOT/bin/wget" ]; then
        "$OPT_ROOT/bin/wget" -qO "$out.part" --header='Cache-Control: no-cache, no-store' \
            "$url?v=$CACHE_TAG-$(date +%s)-$$" >>"$LOG" 2>&1 || return 1
    else
        return 1
    fi
    mv "$out.part" "$out"
}

ipk_value() {
    file="$1"; key="$2"; root="$WORK/control-read"; rm -rf "$root"; mkdir -p "$root" || return 1
    tar -xzOf "$file" ./control.tar.gz >"$root/control.tar.gz" 2>/dev/null || tar -xzOf "$file" control.tar.gz >"$root/control.tar.gz" 2>/dev/null || return 1
    tar -xzOf "$root/control.tar.gz" ./control 2>/dev/null | awk -v key="$key" 'index($0,key ":")==1 {v=$0; sub(key ":[ \t]*","",v); print v; exit}'
}

free_opt_kb() { df -Pk "$OPT_ROOT" 2>/dev/null | awk 'NR==2 {print $4; exit}'; }

record_services() {
    mkdir -p "$META" || return 1
    : >"$META/services-running" || return 1
    for service in $SERVICES; do
        [ -x "$service" ] || continue
        "$service" status >/dev/null 2>&1 && printf '%s\n' "$service" >>"$META/services-running"
    done
}
stop_services() { for service in $SERVICES; do [ -x "$service" ] && "$service" stop >>"$LOG" 2>&1 || true; done; }
start_old_services() {
    [ -r "$META/services-running" ] || return 0
    while IFS= read -r service; do [ -x "$service" ] || continue; "$service" start >>"$LOG" 2>&1 || return 1; done <"$META/services-running"
}

prepare_dependencies() {
    say "Проверяются зависимости Entware..."
    opkg update >>"$LOG" 2>&1 || return 1
    opkg install ca-bundle ca-certificates curl jq lighttpd lighttpd-mod-cgi tar >>"$LOG" 2>&1 || return 1
    command -v tar >/dev/null 2>&1 || return 1
}

clean_before_backup() {
    say "Очищаются старые резервные копии, временные файлы, журналы и кэш..."
    rm -rf "$OPT_ROOT/broray-backups" "$OPT_ROOT/broray-test-backups" "$OPT_ROOT/broray-test-logs" || return 1
    for d in "$BRORAY_DIR/backup" "$BRORAY_DIR/backups" "$BRORAY_DIR/tmp" "$BRORAY_DIR/update" "$BRORAY_DIR/cache" "$BRORAY_DIR/routes/tmp"; do
        [ -d "$d" ] || continue
        rm -rf "$d"/* "$d"/.[!.]* "$d"/..?* 2>/dev/null || true
    done
    if [ -d "$BRORAY_DIR/logs" ]; then
        find "$BRORAY_DIR/logs" -type f -exec "$OPT_ROOT/bin/ash" -c ': > "$1"' ash {} \; 2>/dev/null || true
    fi
    for item in \
        "$OPT_ROOT"/BROray-test-kit-* "$OPT_ROOT"/BROray-public-test-* "$OPT_ROOT"/BROray-site-staging-* \
        "$OPT_ROOT"/broray-clean-install-* "$OPT_ROOT"/broray-create-clean-snapshot-* \
        "$OPT_ROOT"/broray-routes-*-hotfix-* "$OPT_ROOT"/broray-routes-approved-scenarios-*; do
        [ -e "$item" ] || [ -L "$item" ] || continue
        rm -rf "$item" || return 1
    done
    rm -rf "$OPT_ROOT/var/cache/opkg" "$OPT_ROOT/var/opkg-lists" || return 1
    mkdir -p "$OPT_ROOT/var/cache/opkg" "$OPT_ROOT/var/opkg-lists" || return 1
    sync
    say "Свободно в /opt после очистки: $(free_opt_kb) КБ"
}

create_backup() {
    say "Создаётся временная резервная копия в оперативной памяти..."
    mkdir -p "$META/opkg-info" || return 1
    record_services || return 1
    [ -f "$OPT_ROOT/lib/opkg/status" ] && cp -p "$OPT_ROOT/lib/opkg/status" "$META/opkg-status" || : >"$META/opkg-status.absent"
    for f in "$OPT_ROOT/lib/opkg/info"/broray.*; do [ -e "$f" ] || continue; cp -p "$f" "$META/opkg-info/" || return 1; done
    if [ -f "$OPT_ROOT/etc/opkg/broray.conf" ]; then cp -p "$OPT_ROOT/etc/opkg/broray.conf" "$META/broray.conf" || return 1; else : >"$META/broray.conf.absent"; fi
    if [ -d "$BRORAY_DIR" ]; then
        set -- broray
        for rel in etc/init.d/S23broray-monitor etc/init.d/S24broray etc/init.d/S25broray-web etc/init.d/S27broray-auto-switch etc/init.d/S28broray-subscriptions; do [ -e "$OPT_ROOT/$rel" ] && set -- "$@" "$rel"; done
        for name in $COMMANDS; do [ -e "$OPT_ROOT/bin/$name" ] && set -- "$@" "bin/$name"; done
        tar -czf "$BACKUP.part" -C "$OPT_ROOT" \
            --exclude='broray/backup' --exclude='broray/backups' --exclude='broray/cache' \
            --exclude='broray/logs' --exclude='broray/run' --exclude='broray/tmp' \
            --exclude='broray/update' --exclude='broray/routes/tmp' "$@" >>"$LOG" 2>&1 || return 1
        tar -tzf "$BACKUP.part" >/dev/null 2>&1 || return 1
        mv "$BACKUP.part" "$BACKUP" || return 1
    else
        : >"$META/no-broray-dir"
    fi
    BACKUP_READY=true
    say "Временная резервная копия проверена: OK"
}

restore_backup() {
    say "Выполняется автоматический откат..."
    stop_services
    rm -rf "$BRORAY_DIR"
    for service in $SERVICES; do rm -f "$service"; done
    for name in $COMMANDS; do rm -f "$OPT_ROOT/bin/$name"; done
    [ -f "$BACKUP" ] && tar -xzf "$BACKUP" -C "$OPT_ROOT" >>"$LOG" 2>&1 || [ -f "$META/no-broray-dir" ] || return 1
    rm -f "$OPT_ROOT/lib/opkg/info"/broray.*
    [ -f "$META/opkg-status" ] && cp -p "$META/opkg-status" "$OPT_ROOT/lib/opkg/status" || rm -f "$OPT_ROOT/lib/opkg/status"
    for f in "$META/opkg-info"/broray.*; do [ -e "$f" ] || continue; cp -p "$f" "$OPT_ROOT/lib/opkg/info/" || return 1; done
    if [ -f "$META/broray.conf" ]; then mkdir -p "$OPT_ROOT/etc/opkg"; cp -p "$META/broray.conf" "$OPT_ROOT/etc/opkg/broray.conf" || return 1; else rm -f "$OPT_ROOT/etc/opkg/broray.conf"; fi
    start_old_services || return 1
    ROLLBACK_OK=true
    say "Предыдущая установка и данные восстановлены: OK"
}

health_check() {
    [ "$(installed_version)" = "$TARGET_VERSION" ] || return 1
    [ -x "$BRORAY_DIR/bin/broray" ] || return 1
    "$BRORAY_DIR/bin/broray" version 2>/dev/null | grep -Fq "BROray $TARGET_APP_VERSION" || return 1
    for service in $SERVICES; do [ -x "$service" ] || return 1; "$service" status >/dev/null 2>&1 || return 1; done
    lan="$(jq -r '.listenAddress // "192.168.1.1"' "$BRORAY_DIR/config/system/settings.json" 2>/dev/null)"
    "$OPT_ROOT/bin/curl" -fsS --connect-timeout 10 "http://$lan:8080/" >/dev/null 2>&1 || return 1
    for id in telegram whatsapp youtube chatgpt facebook instagram meta tiktok speedtest; do
        [ -r "$BRORAY_DIR/share/routes/manifests/$id.json" ] || return 1
        grep -Fq '"'"$id"'"' "$BRORAY_DIR/routes/bundles.json" || return 1
    done
    [ -r "$BRORAY_DIR/routes/installed/routes.json" ] || return 1
    ! grep -R -F 'Экспортировать' "$BRORAY_DIR/web-new" >/dev/null 2>&1 || return 1
}

fail_update() {
    reason="$1"
    say "ОШИБКА: $reason"
    if [ "$BACKUP_READY" = true ]; then
        restore_backup || { say "КРИТИЧЕСКАЯ ОШИБКА: откат не завершён. Временная копия сохранена: $BACKUP"; finish 2; }
    fi
    finish 1
}

mkdir -p "$WORK" || { echo "ОШИБКА: не удалось создать $WORK"; exit 1; }
: >"$LOG"
say "=================================================="
say "BROray — универсальное безопасное обновление до $TARGET_VERSION"
say "=================================================="
OLD_VERSION="$(installed_version)"; [ -n "$OLD_VERSION" ] || OLD_VERSION="ручная/не зарегистрирована"
say "Исходная версия: $OLD_VERSION"
case "$(uname -m 2>/dev/null)" in aarch64|arm64) ;; *) fail_update "поддерживается только ARM64" ;; esac
command -v opkg >/dev/null 2>&1 || fail_update "Entware OPKG не установлен"
prepare_dependencies || fail_update "не удалось подготовить зависимости Entware"
download_file "$PACKAGE_URL" "$NEW_IPK" || fail_update "не удалось скачать точный пакет"
[ "$(sha256sum "$NEW_IPK" | awk '{print $1}')" = "$PACKAGE_SHA" ] || fail_update "SHA-256 пакета не совпала"
[ "$(ipk_value "$NEW_IPK" Package)" = "broray" ] || fail_update "в пакете неверное имя"
[ "$(ipk_value "$NEW_IPK" Version)" = "$TARGET_VERSION" ] || fail_update "в пакете неверная версия"
[ "$(ipk_value "$NEW_IPK" Architecture)" = "aarch64-3.10" ] || fail_update "в пакете неверная архитектура"
installed_size="$(ipk_value "$NEW_IPK" Installed-Size)"; case "$installed_size" in ''|*[!0-9]*) fail_update "неверный Installed-Size";; esac
clean_before_backup || fail_update "не удалось выполнить предварительную очистку"
required_kb=$(( (installed_size + 1023) / 1024 + 4096 ))
free_kb="$(free_opt_kb)"; case "$free_kb" in ''|*[!0-9]*) fail_update "не удалось определить свободное место";; esac
[ "$free_kb" -ge "$required_kb" ] || fail_update "недостаточно места после очистки: $free_kb КБ, требуется $required_kb КБ"
create_backup || fail_update "не удалось создать временную резервную копию"
free_kb="$(free_opt_kb)"; [ "$free_kb" -ge "$required_kb" ] || fail_update "недостаточно места после резервного копирования"
printf '%s\n%s\n' "$TARGET_VERSION" "$(date '+%s')" >"$MARKER" || fail_update "не удалось подготовить OPKG"
stop_services
INSTALL_STARTED=true
say "Устанавливается проверенный локальный IPK..."
opkg install "$NEW_IPK" >>"$LOG" 2>&1 || fail_update "OPKG не установил пакет"
health_check || fail_update "после установки не пройдена проверка работоспособности"
say "Пакет $TARGET_VERSION установлен: OK"
say "Все пять служб работают: OK"
say "WebUI и девять наборов маршрутов, включая Meta: OK"
say "Старые резервные копии и временный мусор удалены: OK"
say "Обсуждение и поддержка: https://t.me/BROvibe_vpn"
finish 0
