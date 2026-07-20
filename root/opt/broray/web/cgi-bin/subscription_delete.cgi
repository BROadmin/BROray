#!/bin/sh

PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"
export PATH

SUBS_DIR="/opt/broray/subscriptions"
TRASH_DIR="/opt/broray/deleted-subscriptions"
JQ="$(command -v jq 2>/dev/null || true)"

headers()
{
    printf 'Content-Type: application/json; charset=utf-8\r\n'
    printf 'Cache-Control: no-store\r\n'
    printf '\r\n'
}

fail()
{
    "$JQ" -nc \
        --arg error "$1" \
        '{ok:false,error:$error}'
    exit 0
}

headers

[ -n "$JQ" ] || {
    printf '{"ok":false,"error":"На роутере не найден jq"}\n'
    exit 0
}

[ "$REQUEST_METHOD" = "POST" ] ||
    fail "Разрешён только POST-запрос"

BODY="$(cat)"

printf '%s' "$BODY" | "$JQ" -e . >/dev/null 2>&1 ||
    fail "Получены некорректные данные"

ID="$(printf '%s' "$BODY" | "$JQ" -r '.name // empty')"

[ -n "$ID" ] || fail "Не указана подписка"

case "$ID" in
    *[!a-zA-Z0-9._-]*)
        fail "Некорректный идентификатор подписки"
        ;;
esac

SOURCE="$SUBS_DIR/$ID"

[ -d "$SOURCE" ] ||
    fail "Подписка не найдена"

mkdir -p "$TRASH_DIR"

DESTINATION="$TRASH_DIR/${ID}-$(date +%Y%m%d-%H%M%S)"

mv "$SOURCE" "$DESTINATION" || fail "Не удалось удалить подписку"

"$JQ" -nc \
    --arg id "$ID" \
    --arg backup "$DESTINATION" \
    '{
        ok:true,
        id:$id,
        message:"Подписка удалена",
        backup:$backup
    }'
