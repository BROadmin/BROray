#!/bin/sh

set -u

FEED_FILE="/opt/etc/opkg/broray.conf"
FEED_BACKUP="$FEED_FILE.bootstrap-backup"
FEED_URL="https://api.brovibe.cloud/releases/opkg/aarch64-3.10"

fail()
{
    printf 'ОШИБКА: %s\n' "$*" >&2
    exit 1
}

command -v opkg >/dev/null 2>&1 ||
    fail "Entware OPKG не установлен"

case "$(uname -m 2>/dev/null)" in
    aarch64|arm64)
        ;;
    *)
        fail "Поддерживается только архитектура ARM64"
        ;;
esac

mkdir -p /opt/etc/opkg ||
    fail "Не удалось создать каталог настроек OPKG"

rm -f "$FEED_BACKUP"

if [ -f "$FEED_FILE" ]; then
    mv "$FEED_FILE" "$FEED_BACKUP" ||
        fail "Не удалось временно отключить прежний репозиторий BROray"
fi

printf '%s\n' "Включается поддержка HTTPS для OPKG..."

if ! opkg update ||
   ! opkg install ca-bundle ca-certificates wget-ssl
then
    if [ -f "$FEED_BACKUP" ]; then
        mv "$FEED_BACKUP" "$FEED_FILE" || true
    fi

    fail "Не удалось установить HTTPS-загрузчик OPKG"
fi

rm -f "$FEED_BACKUP"

feed_temp="$FEED_FILE.new"

if ! printf 'src/gz broray %s\n' "$FEED_URL" >"$feed_temp"; then
    fail "Не удалось создать настройку репозитория BROray"
fi

mv "$feed_temp" "$FEED_FILE" ||
    fail "Не удалось подключить репозиторий BROray"

printf '%s\n' "Репозиторий BROray подключён"

opkg update ||
    fail "Не удалось обновить списки пакетов"

opkg install broray ||
    fail "Не удалось установить BROray"

printf '\n%s\n' "BROray установлен через OPKG"
