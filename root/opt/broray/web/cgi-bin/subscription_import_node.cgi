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

URI="$(printf '%s' "$BODY" | "$JQ" -r '.uri // empty')"

SUBSCRIPTION_ID="$(printf '%s' "$BODY" | "$JQ" -r '.subscription_id // empty')"
NODE_INDEX="$(printf '%s' "$BODY" | "$JQ" -r '.node_index // empty')"
[ -n "$SUBSCRIPTION_ID" ] || fail "Не передан идентификатор подписки"
[ -n "$NODE_INDEX" ] || fail "Не передан номер узла"
[ -n "$URI" ] || fail "Не передана конфигурация узла"

case "$URI" in
    vless://*|vmess://*|trojan://*|ss://*|hysteria2://*|hy2://*|tuic://*|socks://*|socks5://*|http://*|https://*)
        ;;
    *)
        fail "Неподдерживаемый формат конфигурации"
        ;;
esac

OUTPUT="$("$BRORAY" import-subscription-node "$SUBSCRIPTION_ID" "$NODE_INDEX" "$URI" 2>&1)"
STATUS=$?

if [ "$STATUS" -eq 0 ]; then
    "$JQ" -nc \
        --arg output "$OUTPUT" \
        '{
            ok:true,
            message:"Сервер импортирован.",
            output:$output
        }'
else
    "$JQ" -nc \
        --arg output "$OUTPUT" \
        '{
            ok:false,
            error:"Не удалось импортировать узел",
            output:$output
        }'
fi
