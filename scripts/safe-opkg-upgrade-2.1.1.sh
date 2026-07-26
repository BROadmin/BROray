#!/bin/sh

# Safe transition to the verified BROray 2.1.1-1 package.
# Run as a child shell: ash /tmp/broray-safe-upgrade.sh

set -u

PACKAGE="broray"
TARGET_VERSION="2.1.1-1"
OPT_ROOT="${BRORAY_OPT_ROOT:-/opt}"
BRORAY_DIR="$OPT_ROOT/broray"
INIT_ROOT="$OPT_ROOT/etc/init.d"
OPKG_LIST_ROOT="${BRORAY_OPKG_LIST_ROOT:-$OPT_ROOT/var/opkg-lists}"
FEED_URL="${BRORAY_FEED_URL:-https://api.brovibe.cloud/releases/opkg/aarch64-3.10}"
BACKUP_GLOB="$BRORAY_DIR/backups/opkg-before-2.1.1-1-*.tar.gz"
WORK="/tmp/broray-safe-upgrade-$$"
LOG="/tmp/broray-safe-upgrade-$$.log"
NEW_IPK="$WORK/broray_$TARGET_VERSION.ipk"
OLD_IPK="$WORK/broray-old.ipk"
ROLLBACK_IPK="$WORK/broray-rollback.ipk"

services="
    $INIT_ROOT/S23broray-monitor
    $INIT_ROOT/S24broray
    $INIT_ROOT/S25broray-web
    $INIT_ROOT/S27broray-auto-switch
    $INIT_ROOT/S28broray-subscriptions
"

cleanup()
{
    rm -rf "$WORK"
}

finish()
{
    result="$1"
    cleanup
    printf '\nЖурнал: %s\n' "$LOG"
    printf '%s\n' "Терминал остаётся открытым."
    exit "$result"
}

fail()
{
    printf 'ОШИБКА: %s\n' "$*" >&2
    finish 1
}

installed_version()
{
    opkg list-installed "$PACKAGE" 2>/dev/null |
        awk -F ' - ' -v package="$PACKAGE" '
            $1 == package {
                print $2
                exit
            }
        '
}

feed_value()
{
    wanted_version="$1"
    wanted_key="$2"

    for feed in "$OPKG_LIST_ROOT"/*; do
        [ -f "$feed" ] || continue
        awk \
            -v wanted_package="$PACKAGE" \
            -v wanted_version="$wanted_version" \
            -v wanted_key="$wanted_key" '
            function reset_record() {
                package_name = ""
                package_version = ""
                value = ""
            }
            function emit_record() {
                if (package_name == wanted_package &&
                    package_version == wanted_version &&
                    value != "") {
                    print value
                    found = 1
                }
            }
            BEGIN {
                reset_record()
                found = 0
            }
            $1 == "Package:" {
                if (package_name != "") {
                    emit_record()
                    if (found == 1) {
                        exit
                    }
                }
                reset_record()
                package_name = $2
                next
            }
            $1 == "Version:" {
                package_version = $2
                next
            }
            index($0, wanted_key ":") == 1 {
                value = $0
                sub(wanted_key ":[ \t]*", "", value)
                next
            }
            END {
                if (found == 0) {
                    emit_record()
                }
            }
        ' "$feed"
    done | sed -n '1p'
}

ipk_control_value()
{
    package_file="$1"
    wanted_key="$2"
    control_root="$WORK/control-$$"
    control_tar="$control_root/control.tar.gz"
    control_file="$control_root/control"

    rm -rf "$control_root"
    mkdir -p "$control_root" || return 1

    if ! tar -xzOf "$package_file" ./control.tar.gz \
        >"$control_tar" 2>/dev/null
    then
        tar -xzOf "$package_file" control.tar.gz \
            >"$control_tar" 2>/dev/null || return 1
    fi

    if ! tar -xzOf "$control_tar" ./control \
        >"$control_file" 2>/dev/null
    then
        tar -xzOf "$control_tar" control \
            >"$control_file" 2>/dev/null || return 1
    fi

    awk \
        -v wanted="$wanted_key" '
            index($0, wanted ":") == 1 {
                value = $0
                sub(wanted ":[ \t]*", "", value)
                print value
                exit
            }
        ' "$control_file"
}

make_rollback_ipk()
{
    outer="$WORK/rollback-outer"
    control="$WORK/rollback-control"

    rm -rf "$outer" "$control"
    mkdir -p "$outer" "$control" || return 1
    tar -xzf "$OLD_IPK" -C "$outer" || return 1
    tar -xzf "$outer/control.tar.gz" -C "$control" || return 1

    cat >"$control/preinst" <<'EOF'
#!/bin/sh

[ -z "${IPKG_INSTROOT:-}" ] || exit 0

for service in \
    /opt/etc/init.d/S28broray-subscriptions \
    /opt/etc/init.d/S27broray-auto-switch \
    /opt/etc/init.d/S25broray-web \
    /opt/etc/init.d/S24broray \
    /opt/etc/init.d/S23broray-monitor
do
    [ -x "$service" ] || continue
    "$service" stop >/dev/null 2>&1 || true
done

exit 0
EOF

    cat >"$control/postinst" <<'EOF'
#!/bin/sh
exit 0
EOF

    chmod 755 "$control/preinst" "$control/postinst" || return 1

    (
        cd "$control" || exit 1
        tar -czf "$outer/control.tar.gz" .
    ) || return 1

    (
        cd "$outer" || exit 1
        tar -czf "$ROLLBACK_IPK" \
            ./debian-binary \
            ./data.tar.gz \
            ./control.tar.gz
    ) || return 1

    [ -s "$ROLLBACK_IPK" ]
}

restart_and_check_services()
{
    for service in $services; do
        [ -x "$service" ] || return 1
        "$service" restart >>"$LOG" 2>&1 || return 1
    done

    for service in $services; do
        "$service" status >>"$LOG" 2>&1 || return 1
    done
}

latest_backup()
{
    ls -1t $BACKUP_GLOB 2>/dev/null |
        sed -n '1p'
}

rollback()
{
    backup_after="$1"

    printf '%s\n' \
        "Проверка обновления не пройдена. Возврат к $OLD_VERSION..."

    opkg install \
        --force-downgrade \
        "$ROLLBACK_IPK" \
        >>"$LOG" 2>&1 ||
        return 1

    [ "$(installed_version)" = "$OLD_VERSION" ] ||
        return 1

    if [ -n "$backup_after" ] && [ -f "$backup_after" ]; then
        tar -xzf "$backup_after" -C "$OPT_ROOT" >>"$LOG" 2>&1 ||
            return 1
    fi

    restart_and_check_services || return 1
    rm -f \
        /tmp/broray-opkg-services-before-upgrade \
        /tmp/broray-opkg-existing-backup
    printf '%s\n' \
        "Предыдущий пакет $OLD_VERSION и данные восстановлены: OK"
    return 0
}

: >"$LOG" || fail "не удалось создать журнал"
mkdir -p "$WORK" || fail "не удалось создать временный каталог"

for required_command in \
    ash awk curl jq opkg sed sha256sum tar
do
    command -v "$required_command" >/dev/null 2>&1 ||
        fail "не найдена команда: $required_command"
done

case "$(uname -m 2>/dev/null)" in
    aarch64|arm64)
        ;;
    *)
        fail "поддерживается только ARM64"
        ;;
esac

OLD_VERSION="$(installed_version)"
[ -n "$OLD_VERSION" ] ||
    fail "установленный пакет BROray не найден"

if [ "$OLD_VERSION" = "$TARGET_VERSION" ]; then
    restart_and_check_services ||
        fail "пакет уже установлен, но не все пять служб работают"
    printf '%s\n' "BROray $TARGET_VERSION уже установлен: OK"
    finish 0
fi

printf '%s\n' \
    "=== Безопасное обновление BROray $OLD_VERSION -> $TARGET_VERSION ==="

opkg update >>"$LOG" 2>&1 ||
    fail "не удалось обновить индекс OPKG"

NEW_FILENAME="$(feed_value "$TARGET_VERSION" Filename)"
NEW_SHA="$(feed_value "$TARGET_VERSION" SHA256sum)"
NEW_ARCH="$(feed_value "$TARGET_VERSION" Architecture)"

[ -n "$NEW_FILENAME" ] && [ -n "$NEW_SHA" ] ||
    fail "пакет $TARGET_VERSION отсутствует в индексе"
[ "$NEW_ARCH" = "aarch64-3.10" ] ||
    fail "неожиданная архитектура пакета: $NEW_ARCH"

NEW_FILENAME="${NEW_FILENAME##*/}"
curl \
    -fL \
    --connect-timeout 15 \
    --max-time 180 \
    -o "$NEW_IPK.part" \
    "$FEED_URL/$NEW_FILENAME" \
    >>"$LOG" 2>&1 ||
    fail "не удалось загрузить пакет $TARGET_VERSION"
mv "$NEW_IPK.part" "$NEW_IPK" ||
    fail "не удалось сохранить пакет обновления"

[ "$(sha256sum "$NEW_IPK" | awk '{print $1}')" = "$NEW_SHA" ] ||
    fail "контрольная сумма нового пакета не совпала"
[ "$(ipk_control_value "$NEW_IPK" Package)" = "$PACKAGE" ] ||
    fail "неверное имя нового пакета"
[ "$(ipk_control_value "$NEW_IPK" Version)" = "$TARGET_VERSION" ] ||
    fail "неверная версия нового пакета"

case "$OLD_VERSION" in
    *[!0-9A-Za-z._+-]*|'')
        fail "небезопасный формат текущей версии"
        ;;
esac

OLD_FILENAME="${PACKAGE}_${OLD_VERSION}_aarch64-3.10.ipk"
curl \
    -fL \
    --connect-timeout 15 \
    --max-time 180 \
    -o "$OLD_IPK.part" \
    "$FEED_URL/$OLD_FILENAME" \
    >>"$LOG" 2>&1 ||
    fail "предыдущий пакет $OLD_VERSION недоступен для отката"
mv "$OLD_IPK.part" "$OLD_IPK" ||
    fail "не удалось сохранить предыдущий пакет"

[ "$(ipk_control_value "$OLD_IPK" Package)" = "$PACKAGE" ] ||
    fail "неверное имя предыдущего пакета"
[ "$(ipk_control_value "$OLD_IPK" Version)" = "$OLD_VERSION" ] ||
    fail "версия предыдущего пакета не совпала"

make_rollback_ipk ||
    fail "не удалось подготовить OPKG-пакет отката"

BACKUP_BEFORE="$(latest_backup)"
printf '%s\n' "Пакеты обновления и отката проверены: OK"
printf '%s\n' "Устанавливается $TARGET_VERSION..."

install_ok=true
opkg install "$NEW_IPK" >>"$LOG" 2>&1 ||
    install_ok=false

BACKUP_AFTER="$(latest_backup)"
if [ "$BACKUP_AFTER" = "$BACKUP_BEFORE" ]; then
    BACKUP_AFTER=""
fi

health_ok=true
[ "$install_ok" = true ] || health_ok=false
[ "$(installed_version)" = "$TARGET_VERSION" ] || health_ok=false

if [ "$health_ok" = true ]; then
    for service in $services; do
        "$service" status >>"$LOG" 2>&1 || health_ok=false
    done
fi

if [ "$health_ok" = true ]; then
    LAN_IP="$(sed -n '1p' "$BRORAY_DIR/run/lan-ip" 2>/dev/null)"
    [ -n "$LAN_IP" ] &&
        curl -fsS --max-time 5 \
            "http://$LAN_IP:8080/" \
            >/dev/null 2>&1 ||
        health_ok=false
fi

if [ "$health_ok" = true ]; then
    printf '%s\n' "Пакет $TARGET_VERSION установлен: OK"
    printf '%s\n' "Все пять служб работают: OK"
    printf '%s\n' "WebUI отвечает: OK"
    finish 0
fi

if rollback "$BACKUP_AFTER"; then
    printf '%s\n' \
        "Обновление отменено без смены рабочей версии."
    finish 1
fi

printf '%s\n' \
    "КРИТИЧЕСКАЯ ОШИБКА: автоматический откат не завершён." >&2
[ -n "$BACKUP_AFTER" ] &&
    printf 'Резервная копия: %s\n' "$BACKUP_AFTER" >&2
finish 1
