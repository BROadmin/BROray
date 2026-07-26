#!/bin/sh

# One-time migration of an existing manual BROray 2.1.0 installation
# to the verified BROray 2.1.0-2 OPKG package.
# Run as a child shell:
#   ash /tmp/broray-manual-to-opkg-2.1.0-2.sh

set -u

PACKAGE="broray"
TARGET_VERSION="2.1.0-2"
CLI_VERSION="BROray 2.1.0"
ARCHITECTURE="aarch64-3.10"
OPT_ROOT="${BRORAY_OPT_ROOT:-/opt}"
TMP_ROOT="${BRORAY_TMP_ROOT:-/tmp}"
BRORAY_DIR="$OPT_ROOT/broray"
INIT_ROOT="$OPT_ROOT/etc/init.d"
FEED_FILE="${BRORAY_FEED_FILE:-$OPT_ROOT/etc/opkg/broray.conf}"
FEED_URL="${BRORAY_FEED_URL:-https://api.brovibe.cloud/releases/opkg/aarch64-3.10}"
PACKAGE_NAME="broray_${TARGET_VERSION}_${ARCHITECTURE}.ipk"
PACKAGE_URL="${BRORAY_PACKAGE_URL:-$FEED_URL/$PACKAGE_NAME}"
PACKAGE_SHA256="${BRORAY_PACKAGE_SHA256:-7c47d5b45a4f5627aa3efd8c0780b5e881907b0f0f8914e609012b19f97519ff}"
XRAY_BINARY_SHA256="${BRORAY_XRAY_BINARY_SHA256:-dd3ba298aa32af9442163ee791d54f562bd89aa860fed1d0c47306fb019c1e64}"
BACKUP_STAMP="$(date '+%Y%m%d-%H%M%S')"
MANUAL_BACKUP="${BRORAY_MANUAL_BACKUP:-$BRORAY_DIR/backups/manual-before-opkg-$BACKUP_STAMP-$$.tar.gz}"
MANUAL_BACKUP_SUM="${BRORAY_MANUAL_BACKUP_SUM:-$MANUAL_BACKUP.sha256}"
MANUAL_BACKUP_PART="$MANUAL_BACKUP.part"
WORK="$TMP_ROOT/broray-manual-opkg-migration-$$"
LOG="$TMP_ROOT/broray-manual-opkg-migration-$$.log"
PACKAGE_FILE="$WORK/$PACKAGE_NAME"
DATA_TAR="$WORK/data.tar.gz"
CONTROL_TAR="$WORK/control.tar.gz"
CONTROL_FILE="$WORK/control"
PACKAGE_XRAY="$WORK/package-xray"
CONFFILES_ARCHIVE="$WORK/manual-conffiles.tar.gz"
CONFFILES_SUM="$WORK/manual-conffiles.sha256"
SERVICE_BACKUP="$WORK/services"
LINK_BACKUP="$WORK/links"
STATUS_BACKUP="$WORK/opkg-status.before"
FEED_BACKUP="$WORK/broray.conf.before"
PREPARED_BACKUP="$BRORAY_DIR/backup/manual-opkg-migration-$$.tar.gz"
BACKUP_MARKER="${BRORAY_BACKUP_MARKER:-$TMP_ROOT/broray-opkg-existing-backup}"
BACKUP_EXCLUDES="$WORK/manual-backup-excludes"
BACKUP_LIST="$WORK/manual-backup.list"
CHANGED=false
ROLLBACK_RUNNING=false
KEEP_WORK=false
STATUS_FILE=""

services="
    $INIT_ROOT/S23broray-monitor
    $INIT_ROOT/S24broray
    $INIT_ROOT/S25broray-web
    $INIT_ROOT/S27broray-auto-switch
    $INIT_ROOT/S28broray-subscriptions
"

commands="
    broray
    broray-routes
    broray-server
    broray-servers
    broray-subscriptions
    broray-system
"

cleanup()
{
    rm -f \
        "$BACKUP_MARKER" \
        "$PREPARED_BACKUP" \
        "$MANUAL_BACKUP_PART" \
        "$MANUAL_BACKUP_SUM.new"

    if [ "$KEEP_WORK" = false ]; then
        rm -rf "$WORK"
    fi
}

finish()
{
    result="$1"
    cleanup
    printf '\nЖурнал: %s\n' "$LOG"

    if [ "$KEEP_WORK" = true ]; then
        printf 'Файлы аварийного восстановления: %s\n' "$WORK"
    fi

    printf '%s\n' "Терминал остаётся открытым."
    exit "$result"
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

stop_services()
{
    for service in \
        "$INIT_ROOT/S28broray-subscriptions" \
        "$INIT_ROOT/S27broray-auto-switch" \
        "$INIT_ROOT/S25broray-web" \
        "$INIT_ROOT/S24broray" \
        "$INIT_ROOT/S23broray-monitor"
    do
        [ -x "$service" ] || continue
        "$service" stop >>"$LOG" 2>&1 || true
    done
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

check_webui()
{
    lan_ip="$(sed -n '1p' "$BRORAY_DIR/run/lan-ip" 2>/dev/null)"
    [ -n "$lan_ip" ] || return 1

    attempt=1
    while [ "$attempt" -le 10 ]; do
        if curl \
            -fsS \
            --max-time 5 \
            "http://$lan_ip:8080/" \
            >/dev/null 2>&1
        then
            return 0
        fi

        sleep 1
        attempt=$((attempt + 1))
    done

    return 1
}

restore_web_proxy()
{
    lan_ip="$(sed -n '1p' "$BRORAY_DIR/run/lan-ip" 2>/dev/null)"
    [ -n "$lan_ip" ] || return 1

    ndmc -c "no ip http proxy broray" >/dev/null 2>&1 || true
    ndmc -c "ip http proxy broray" >/dev/null 2>&1 || return 1
    ndmc -c \
        "ip http proxy broray upstream http $lan_ip 8080" \
        >/dev/null 2>&1 || return 1
    ndmc -c "ip http proxy broray domain ndns" \
        >/dev/null 2>&1 || return 1
    ndmc -c "ip http proxy broray ssl redirect" \
        >/dev/null 2>&1 || return 1
    ndmc -c "ip http proxy broray security-level public" \
        >/dev/null 2>&1 || return 1
    ndmc -c "system configuration save" >/dev/null 2>&1 || return 1
}

restore_opkg_state()
{
    cp -p "$STATUS_BACKUP" "$STATUS_FILE.restore" || return 1
    mv "$STATUS_FILE.restore" "$STATUS_FILE" || return 1

    rm -f "$OPT_ROOT/lib/opkg/info/broray."*
    rm -f "$OPT_ROOT/var/lib/opkg/info/broray."*
    rm -f "$OPT_ROOT/var/opkg-lists/broray"

    if [ -f "$FEED_BACKUP" ]; then
        mkdir -p "${FEED_FILE%/*}" || return 1
        cp -p "$FEED_BACKUP" "$FEED_FILE" || return 1
    else
        rm -f "$FEED_FILE"
    fi
}

remove_new_package_files()
{
    tar -tzf "$DATA_TAR" 2>/dev/null |
        while IFS= read -r archive_path; do
            relative_path="${archive_path#./}"

            case "$relative_path" in
                ''|*/)
                    continue
                    ;;
                opt/broray/*)
                    ;;
                opt/etc/init.d/S23broray-monitor|\
                opt/etc/init.d/S24broray|\
                opt/etc/init.d/S25broray-web|\
                opt/etc/init.d/S27broray-auto-switch|\
                opt/etc/init.d/S28broray-subscriptions)
                    ;;
                *)
                    continue
                    ;;
            esac

            case "$relative_path" in
                opt/*)
                    target_path="$OPT_ROOT/${relative_path#opt/}"
                    ;;
                *)
                    continue
                    ;;
            esac

            rm -f "$target_path"
        done
}

restore_links()
{
    for command_name in $commands; do
        rm -f "$OPT_ROOT/bin/$command_name"

        if [ -e "$LINK_BACKUP/$command_name" ] ||
           [ -L "$LINK_BACKUP/$command_name" ]
        then
            cp -a \
                "$LINK_BACKUP/$command_name" \
                "$OPT_ROOT/bin/$command_name" ||
                return 1
        fi
    done
}

rollback_manual_installation()
{
    [ "$ROLLBACK_RUNNING" = false ] || return 1
    ROLLBACK_RUNNING=true

    printf '%s\n' \
        "Проверка миграции не пройдена. Восстанавливается ручная установка..."

    stop_services
    opkg remove "$PACKAGE" >>"$LOG" 2>&1 || true
    remove_new_package_files

    tar -xzf "$MANUAL_BACKUP" -C "$OPT_ROOT" >>"$LOG" 2>&1 ||
        return 1

    for service in $services; do
        service_name="${service##*/}"
        [ -f "$SERVICE_BACKUP/$service_name" ] || return 1
        cp -p "$SERVICE_BACKUP/$service_name" "$service" || return 1
    done

    restore_links || return 1
    restore_opkg_state || return 1
    restore_web_proxy || return 1
    restart_and_check_services || return 1
    check_webui || return 1

    [ "$(installed_version)" = "" ] || return 1
    [ "$("$BRORAY_DIR/bin/broray" version 2>/dev/null)" = "$CLI_VERSION" ] ||
        return 1

    printf '%s\n' "Ручная установка BROray 2.1.0 восстановлена: OK"
    return 0
}

fail()
{
    printf 'ОШИБКА: %s\n' "$*" >&2

    if [ "$CHANGED" = true ]; then
        if rollback_manual_installation; then
            printf '%s\n' "Миграция отменена без смены рабочей версии."
        else
            KEEP_WORK=true
            printf '%s\n' \
                "КРИТИЧЕСКАЯ ОШИБКА: автоматическое восстановление не завершено." \
                >&2
            printf 'Проверенный снимок: %s\n' "$MANUAL_BACKUP" >&2
        fi
    fi

    finish 1
}

on_signal()
{
    fail "выполнение прервано сигналом"
}

ipk_control_value()
{
    wanted_key="$1"

    awk \
        -v wanted="$wanted_key" '
            index($0, wanted ":") == 1 {
                value = $0
                sub(wanted ":[ \t]*", "", value)
                print value
                exit
            }
        ' "$CONTROL_FILE"
}

save_manual_conffiles()
{
    (
        cd "$BRORAY_DIR" || exit 1
        set --

        for relative_path in \
            config/system/settings.json \
            config/system/server-auto-switch.json \
            routes/config.json
        do
            [ -f "$relative_path" ] || continue
            set -- "$@" "$relative_path"
        done

        [ "$#" -gt 0 ] || exit 1
        tar -czf "$CONFFILES_ARCHIVE" "$@" || exit 1
        sha256sum "$@" >"$CONFFILES_SUM" || exit 1
    )
}

restore_and_check_manual_conffiles()
{
    tar -xzf "$CONFFILES_ARCHIVE" -C "$BRORAY_DIR" >>"$LOG" 2>&1 ||
        return 1

    (
        cd "$BRORAY_DIR" || exit 1
        sha256sum -c "$CONFFILES_SUM"
    ) >>"$LOG" 2>&1
}

check_free_space()
{
    minimum_opt_kb="$1"
    tmp_free_kb="$(
        df -Pk "$TMP_ROOT" 2>/dev/null |
            awk 'NR == 2 {print $4}'
    )"
    opt_free_kb="$(
        df -Pk "$OPT_ROOT" 2>/dev/null |
            awk 'NR == 2 {print $4}'
    )"

    case "$tmp_free_kb:$opt_free_kb" in
        *[!0-9:]*|'')
            fail "не удалось определить свободное место"
            ;;
    esac

    [ "$tmp_free_kb" -ge 70000 ] ||
        fail "в $TMP_ROOT требуется не менее 70 МБ свободного места"
    [ "$opt_free_kb" -ge "$minimum_opt_kb" ] ||
        fail "в $OPT_ROOT требуется не менее $minimum_opt_kb КБ свободного места"
}

verify_manual_backup()
{
    [ -f "$MANUAL_BACKUP" ] ||
        return 1
    [ -f "$MANUAL_BACKUP_SUM" ] ||
        return 1

    expected_backup_sha="$(
        awk 'NR == 1 {print $1}' "$MANUAL_BACKUP_SUM"
    )"
    actual_backup_sha="$(
        sha256sum "$MANUAL_BACKUP" |
            awk '{print $1}'
    )"

    [ -n "$expected_backup_sha" ] ||
        return 1
    [ "$actual_backup_sha" = "$expected_backup_sha" ] ||
        return 1

    tar -tzf "$MANUAL_BACKUP" >"$BACKUP_LIST" 2>>"$LOG" ||
        return 1
    grep -Fxq 'broray/bin/broray' "$BACKUP_LIST" ||
        return 1
    grep -Fxq 'broray/bin/xray' "$BACKUP_LIST" ||
        return 1
}

prepare_manual_backup()
{
    if [ -f "$MANUAL_BACKUP" ] || [ -f "$MANUAL_BACKUP_SUM" ]; then
        verify_manual_backup ||
            fail "существующий снимок или его SHA-256 не прошли проверку"
        printf 'Проверенный снимок: %s\n' "$MANUAL_BACKUP"
        return 0
    fi

    mkdir -p "$BRORAY_DIR/backups" ||
        fail "не удалось создать каталог резервных копий"
    chmod 700 "$BRORAY_DIR/backups" 2>/dev/null || true

    printf '%s\n' \
        'broray/backup' \
        'broray/backup/*' \
        'broray/backups' \
        'broray/backups/*' \
        'broray/cache' \
        'broray/cache/*' \
        'broray/logs' \
        'broray/logs/*' \
        'broray/run' \
        'broray/run/*' \
        'broray/tmp' \
        'broray/tmp/*' \
        'broray/update' \
        'broray/update/*' \
        'broray/routes/tmp' \
        'broray/routes/tmp/*' \
        >"$BACKUP_EXCLUDES" ||
        fail "не удалось подготовить список исключений"

    tar \
        -X "$BACKUP_EXCLUDES" \
        -czf "$MANUAL_BACKUP_PART" \
        -C "$OPT_ROOT" \
        broray \
        >>"$LOG" 2>&1 ||
        fail "не удалось создать страховочную копию"

    tar -tzf "$MANUAL_BACKUP_PART" >/dev/null 2>&1 ||
        fail "созданная страховочная копия не читается"
    chmod 600 "$MANUAL_BACKUP_PART"
    mv "$MANUAL_BACKUP_PART" "$MANUAL_BACKUP" ||
        fail "не удалось сохранить страховочную копию"

    sha256sum "$MANUAL_BACKUP" >"$MANUAL_BACKUP_SUM.new" ||
        fail "не удалось вычислить SHA-256 страховочной копии"
    chmod 600 "$MANUAL_BACKUP_SUM.new"
    mv "$MANUAL_BACKUP_SUM.new" "$MANUAL_BACKUP_SUM" ||
        fail "не удалось сохранить SHA-256 страховочной копии"

    verify_manual_backup ||
        fail "созданная страховочная копия не прошла проверку"
    printf 'Создан проверенный снимок: %s\n' "$MANUAL_BACKUP"
}

: >"$LOG" || {
    printf '%s\n' "ОШИБКА: не удалось создать журнал" >&2
    exit 1
}
mkdir -p "$WORK" "$SERVICE_BACKUP" "$LINK_BACKUP" ||
    fail "не удалось создать временные каталоги"

trap on_signal HUP INT TERM

for required_command in \
    ash awk cp curl date df grep jq ln mkdir mv ndmc opkg rm sed \
    sha256sum sleep tar uname wget
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

[ -x "$BRORAY_DIR/bin/broray" ] ||
    fail "ручная установка BROray не найдена"
[ "$("$BRORAY_DIR/bin/broray" version 2>/dev/null)" = "$CLI_VERSION" ] ||
    fail "ожидалась ручная установка BROray 2.1.0"

CURRENT_OPKG_VERSION="$(installed_version)"
if [ "$CURRENT_OPKG_VERSION" = "$TARGET_VERSION" ]; then
    restart_and_check_services ||
        fail "пакет уже зарегистрирован, но не все пять служб работают"
    check_webui ||
        fail "пакет уже зарегистрирован, но WebUI не отвечает"
    printf '%s\n' "BROray $TARGET_VERSION уже управляется OPKG: OK"
    finish 0
fi
[ -z "$CURRENT_OPKG_VERSION" ] ||
    fail "в OPKG уже зарегистрирована другая версия: $CURRENT_OPKG_VERSION"

for status_candidate in \
    "$OPT_ROOT/lib/opkg/status" \
    "$OPT_ROOT/var/lib/opkg/status"
do
    [ -f "$status_candidate" ] || continue
    STATUS_FILE="$status_candidate"
    break
done
[ -n "$STATUS_FILE" ] ||
    fail "база состояния OPKG не найдена"

opkg --help 2>&1 |
    awk '
        index($0, "--force-space") {
            found = 1
        }
        END {
            exit(found ? 0 : 1)
        }
    ' ||
    fail "эта версия OPKG не поддерживает безопасный обход проверки места"

for dependency in \
    ca-bundle \
    ca-certificates \
    curl \
    jq \
    lighttpd \
    lighttpd-mod-cgi \
    wget-ssl
do
    opkg list-installed "$dependency" 2>/dev/null |
        awk -F ' - ' -v package="$dependency" '
            $1 == package {
                found = 1
            }
            END {
                exit(found ? 0 : 1)
            }
        ' ||
        fail "не установлена зависимость OPKG: $dependency"
done

wget --version 2>&1 |
    grep -Fq '+https' ||
    fail "установленный wget не поддерживает HTTPS"

for service in $services; do
    [ -x "$service" ] ||
        fail "не найдена служба: ${service##*/}"
    "$service" status >>"$LOG" 2>&1 ||
        fail "до миграции не работает служба: ${service##*/}"
done
check_webui ||
    fail "до миграции WebUI не отвечает"

check_free_space 24576
prepare_manual_backup
check_free_space 20000

printf '%s\n' \
    "=== Миграция ручной установки BROray 2.1.0 в OPKG $TARGET_VERSION ==="
printf '%s\n' "Снимок и предварительное состояние проверены: OK"

if [ -n "${BRORAY_PACKAGE_FILE:-}" ]; then
    cp -p "$BRORAY_PACKAGE_FILE" "$PACKAGE_FILE" ||
        fail "не удалось скопировать локальный пакет"
else
    curl \
        -fL \
        --connect-timeout 15 \
        --max-time 180 \
        -o "$PACKAGE_FILE.part" \
        "$PACKAGE_URL" \
        >>"$LOG" 2>&1 ||
        fail "не удалось загрузить пакет $TARGET_VERSION"
    mv "$PACKAGE_FILE.part" "$PACKAGE_FILE" ||
        fail "не удалось сохранить пакет"
fi

[ "$(sha256sum "$PACKAGE_FILE" | awk '{print $1}')" = "$PACKAGE_SHA256" ] ||
    fail "SHA-256 пакета не совпала"

tar -xzOf "$PACKAGE_FILE" ./data.tar.gz >"$DATA_TAR" 2>>"$LOG" ||
    tar -xzOf "$PACKAGE_FILE" data.tar.gz >"$DATA_TAR" 2>>"$LOG" ||
    fail "не удалось извлечь данные пакета"
tar -xzOf "$PACKAGE_FILE" ./control.tar.gz >"$CONTROL_TAR" 2>>"$LOG" ||
    tar -xzOf "$PACKAGE_FILE" control.tar.gz >"$CONTROL_TAR" 2>>"$LOG" ||
    fail "не удалось извлечь метаданные пакета"
tar -xzOf "$CONTROL_TAR" ./control >"$CONTROL_FILE" 2>>"$LOG" ||
    tar -xzOf "$CONTROL_TAR" control >"$CONTROL_FILE" 2>>"$LOG" ||
    fail "не удалось прочитать метаданные пакета"

[ "$(ipk_control_value Package)" = "$PACKAGE" ] ||
    fail "неверное имя пакета"
[ "$(ipk_control_value Version)" = "$TARGET_VERSION" ] ||
    fail "неверная версия пакета"
[ "$(ipk_control_value Architecture)" = "$ARCHITECTURE" ] ||
    fail "неверная архитектура пакета"
tar -tzf "$DATA_TAR" >/dev/null 2>&1 ||
    fail "данные пакета не прошли проверку"

tar -xzOf "$DATA_TAR" ./opt/broray/bin/xray >"$PACKAGE_XRAY" 2>>"$LOG" ||
    tar -xzOf "$DATA_TAR" opt/broray/bin/xray >"$PACKAGE_XRAY" 2>>"$LOG" ||
    fail "не удалось проверить Xray внутри пакета"
[ "$(sha256sum "$PACKAGE_XRAY" | awk '{print $1}')" = "$XRAY_BINARY_SHA256" ] ||
    fail "контрольная сумма Xray внутри пакета не совпала"
[ -f "$BRORAY_DIR/bin/xray" ] ||
    fail "действующий Xray не найден"
[ "$(sha256sum "$BRORAY_DIR/bin/xray" | awk '{print $1}')" = "$XRAY_BINARY_SHA256" ] ||
    fail "действующий Xray отличается от Xray в пакете"
rm -f "$PACKAGE_XRAY"

cp -p "$STATUS_FILE" "$STATUS_BACKUP" ||
    fail "не удалось сохранить базу состояния OPKG"

if [ -f "$FEED_FILE" ]; then
    cp -p "$FEED_FILE" "$FEED_BACKUP" ||
        fail "не удалось сохранить конфигурацию репозитория"
fi

for service in $services; do
    cp -p "$service" "$SERVICE_BACKUP/${service##*/}" ||
        fail "не удалось сохранить ${service##*/}"
done

for command_name in $commands; do
    command_path="$OPT_ROOT/bin/$command_name"

    if [ -e "$command_path" ] || [ -L "$command_path" ]; then
        cp -a "$command_path" "$LINK_BACKUP/$command_name" ||
            fail "не удалось сохранить $command_path"
    fi
done

save_manual_conffiles ||
    fail "не удалось сохранить пользовательские конфиги"

mkdir -p "$BRORAY_DIR/backup" ||
    fail "не удалось подготовить ссылку на проверенный снимок"
ln "$MANUAL_BACKUP" "$PREPARED_BACKUP" ||
    fail "не удалось подготовить снимок без расходования места"
printf '%s\n%s\n' \
    "$PREPARED_BACKUP" \
    "$(date '+%s')" \
    >"$BACKUP_MARKER" ||
    fail "не удалось передать снимок установщику"

printf '%s\n' "Пакет проверен: OK"
printf '%s\n' "Xray в ручной установке и пакете идентичен: OK"
printf '%s\n' "Устанавливается $TARGET_VERSION и создаётся запись OPKG..."

CHANGED=true
opkg --force-space install "$PACKAGE_FILE" >>"$LOG" 2>&1 ||
    fail "OPKG не установил пакет"

rm -f "$BACKUP_MARKER" "$PREPARED_BACKUP"

[ "$(installed_version)" = "$TARGET_VERSION" ] ||
    fail "OPKG не зарегистрировал версию $TARGET_VERSION"

restore_and_check_manual_conffiles ||
    fail "не удалось восстановить пользовательские конфиги"

mkdir -p "${FEED_FILE%/*}" ||
    fail "не удалось создать каталог конфигурации OPKG"
printf 'src/gz broray %s\n' "$FEED_URL" >"$FEED_FILE.new" ||
    fail "не удалось подготовить репозиторий BROray"
mv "$FEED_FILE.new" "$FEED_FILE" ||
    fail "не удалось подключить репозиторий BROray"
opkg update >>"$LOG" 2>&1 ||
    fail "не удалось обновить индекс OPKG"

restart_and_check_services ||
    fail "не все пять служб работают после миграции"
check_webui ||
    fail "WebUI не отвечает после миграции"

[ "$("$BRORAY_DIR/bin/broray" version 2>/dev/null)" = "$CLI_VERSION" ] ||
    fail "CLI BROray сообщает неверную версию"
[ "$(installed_version)" = "$TARGET_VERSION" ] ||
    fail "итоговая версия OPKG не совпала"

printf '%s\n' "BROray $TARGET_VERSION зарегистрирован в OPKG: OK"
printf '%s\n' "Пользовательские конфиги сохранены: OK"
printf '%s\n' "Все пять служб работают: OK"
printf '%s\n' "WebUI отвечает: OK"
printf '%s\n' "Миграция завершена успешно."
finish 0
