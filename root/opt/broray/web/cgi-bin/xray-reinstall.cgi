#!/bin/sh

export PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"

BASE="/opt/broray"
BRORAY="$BASE/bin/broray"
JQ="/opt/bin/jq"

[ -x "$JQ" ] || JQ="$(command -v jq 2>/dev/null)"

printf 'Content-Type: application/json\r\n'
printf 'Cache-Control: no-store\r\n'
printf '\r\n'

if [ ! -x "$BRORAY" ]; then
    printf '%s\n' \
        '{"success":false,"error":"Команда BROray не найдена."}'
    exit 0
fi

if [ -z "$JQ" ] || [ ! -x "$JQ" ]; then
    printf '%s\n' \
        '{"success":false,"error":"На роутере не найден jq."}'
    exit 0
fi

RESULT="$(
    mktemp /tmp/broray-xray-update-check.XXXXXX
)" || {
    printf '%s\n' \
        '{"success":false,"error":"Не удалось создать временный файл."}'
    exit 0
}

cleanup()
{
    rm -f "$RESULT"
}

trap cleanup EXIT HUP INT TERM

if "$BRORAY" xray reinstall >"$RESULT" 2>/dev/null; then
    if "$JQ" -e . "$RESULT" >/dev/null 2>&1; then
        cat "$RESULT"
        exit 0
    fi

    printf '%s\n' \
        '{"success":false,"error":"BROray вернул некорректный JSON."}'
    exit 0
fi

if "$JQ" -e . "$RESULT" >/dev/null 2>&1; then
    cat "$RESULT"
else
    printf '%s\n' \
        '{"success":false,"error":"Не удалось проверить обновления Xray."}'
fi

exit 0
