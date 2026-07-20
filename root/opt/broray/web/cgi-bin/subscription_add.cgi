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

escape_json()
{
    printf '%s' "$1" |
        sed \
            -e 's/\\/\\\\/g' \
            -e 's/"/\\"/g' \
            -e ':a;N;$!ba;s/\n/\\n/g'
}

fail()
{
    ERROR_ESCAPED="$(escape_json "$1")"
    printf '{"ok":false,"error":"%s"}\n' "$ERROR_ESCAPED"
    exit 0
}

headers

[ "$REQUEST_METHOD" = "POST" ] ||
    fail "Разрешён только POST-запрос"

[ -x "$BRORAY" ] ||
    fail "Команда BROray не найдена"

[ -n "$JQ" ] ||
    fail "На роутере не найден jq"

BODY="$(cat)"

printf '%s' "$BODY" | "$JQ" -e . >/dev/null 2>&1 ||
    fail "Получены некорректные данные"

DISPLAY_NAME="$(
    printf '%s' "$BODY" |
        "$JQ" -r '.name // empty'
)"

URL="$(
    printf '%s' "$BODY" |
        "$JQ" -r '.url // empty'
)"

[ -n "$DISPLAY_NAME" ] ||
    fail "Укажите название подписки"

[ -n "$URL" ] ||
    fail "Укажите ссылку подписки"

case "$URL" in
    http://*|https://*)
        ;;
    *)
        fail "Ссылка должна начинаться с http:// или https://"
        ;;
esac

BASE_ID="$(
    printf '%s' "$DISPLAY_NAME" |
        tr 'A-Z' 'a-z' |
        sed \
            -e 's/[^a-z0-9._-]/_/g' \
            -e 's/__*/_/g' \
            -e 's/^_*//' \
            -e 's/_*$//' |
        cut -c1-40
)"

case "$BASE_ID" in
    ""|*[!a-z0-9._-]*)
        BASE_ID="subscription"
        ;;
esac

SUBSCRIPTION_ID="$BASE_ID"
NUMBER=1

while [ -e "$SUBS_DIR/$SUBSCRIPTION_ID" ]
do
    NUMBER=$((NUMBER + 1))
    SUBSCRIPTION_ID="${BASE_ID}_${NUMBER}"
done

OUTPUT="$("$BRORAY" subscription-add "$SUBSCRIPTION_ID" "$URL" 2>&1)"
STATUS=$?

if [ "$STATUS" -ne 0 ]; then
    "$JQ" -nc \
        --arg error "Не удалось добавить подписку" \
        --arg output "$OUTPUT" \
        '{
            ok: false,
            error: $error,
            output: $output
        }'
    exit 0
fi

mkdir -p "$SUBS_DIR/$SUBSCRIPTION_ID"

"$JQ" -nc \
    --arg id "$SUBSCRIPTION_ID" \
    --arg name "$DISPLAY_NAME" \
    --arg url "$URL" \
    --arg created_at "$(date '+%Y-%m-%d %H:%M:%S')" \
    '{
        id: $id,
        name: $name,
        url: $url,
        created_at: $created_at
    }' > "$SUBS_DIR/$SUBSCRIPTION_ID/web-meta.json"

"$JQ" -nc \
    --arg id "$SUBSCRIPTION_ID" \
    --arg name "$DISPLAY_NAME" \
    --arg output "$OUTPUT" \
    '{
        ok: true,
        id: $id,
        name: $name,
        message: "Подписка добавлена",
        output: $output
    }'
