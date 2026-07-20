#!/bin/sh

PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"
export PATH

BRORAY="/opt/broray/bin/broray"
JQ="$(command -v jq 2>/dev/null || true)"

headers()
{
    printf 'Content-Type: application/json; charset=utf-8\r\n'
    printf 'Cache-Control: no-store\r\n'
    printf '\r\n'
}

fail()
{
    if [ -n "$JQ" ]; then
        "$JQ" -nc \
            --arg error "$1" \
            '{ok:false,error:$error,nodes:[]}'
    else
        printf '{"ok":false,"error":"CGI configuration error","nodes":[]}\n'
    fi
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

ID="$(
    printf '%s' "$BODY" |
        "$JQ" -r '.name // empty'
)"

[ -n "$ID" ] ||
    fail "Не указана подписка"

case "$ID" in
    *[!a-zA-Z0-9._-]*)
        fail "Некорректный идентификатор подписки"
        ;;
esac

OUTPUT="$("$BRORAY" subscription-nodes "$ID" 2>&1)"
STATUS=$?

if [ "$STATUS" -ne 0 ]; then
    "$JQ" -nc \
        --arg output "$OUTPUT" \
        '{
            ok: false,
            error: "Не удалось получить узлы подписки",
            output: $output,
            nodes: []
        }'
    exit 0
fi

NODES="$(
    printf '%s\n' "$OUTPUT" |
        awk '/^[a-zA-Z0-9+.-]+:\/\// { print }' |
        "$JQ" -Rsc '
            split("\n") |
            map(select(length > 0))
        '
)"

"$JQ" -nc \
    --arg output "$OUTPUT" \
    --argjson nodes "$NODES" \
    '{
        ok: true,
        output: $output,
        nodes: $nodes
    }'
