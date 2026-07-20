#!/bin/sh

PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"
export PATH

BRORAY="/opt/broray/bin/broray"
SUBS_DIR="/opt/broray/subscriptions"
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

[ -x "$BRORAY" ] ||
    fail "Команда BROray не найдена"

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

[ -d "$SUBS_DIR/$ID" ] ||
    fail "Подписка не найдена"

OUTPUT="$("$BRORAY" subscription-update "$ID" 2>&1)"
STATUS=$?

if [ "$STATUS" -ne 0 ]; then
    "$JQ" -nc \
        --arg error "Не удалось обновить подписку" \
        --arg output "$OUTPUT" \
        '{ok:false,error:$error,output:$output}'
    exit 0
fi

META="$SUBS_DIR/$ID/web-meta.json"
NOW="$(date '+%Y-%m-%d %H:%M:%S')"

if [ -f "$META" ] && "$JQ" -e . "$META" >/dev/null 2>&1; then
    TMP_META="$(mktemp /tmp/broray-meta.XXXXXX)"
    "$JQ" \
        --arg updated_at "$NOW" \
        '.updated_at = $updated_at' \
        "$META" > "$TMP_META"
    mv "$TMP_META" "$META"
else
    "$JQ" -nc \
        --arg id "$ID" \
        --arg name "$ID" \
        --arg updated_at "$NOW" \
        '{
            id:$id,
            name:$name,
            updated_at:$updated_at
        }' > "$META"
fi

"$JQ" -nc \
    --arg output "$OUTPUT" \
    --arg updated_at "$NOW" \
    '{
        ok:true,
        message:"Подписка обновлена",
        updated_at:$updated_at,
        output:$output
    }'
