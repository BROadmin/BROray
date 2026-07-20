#!/bin/sh
export PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"

BRORAY="/opt/broray/bin/broray"
JQ="$(command -v jq 2>/dev/null || true)"
TMP="/opt/broray/tmp/import-cgi.$$"

cleanup() {
    rm -f "$TMP"
}
trap cleanup EXIT INT TERM

printf 'Content-Type: application/json; charset=UTF-8\r\n'
printf 'Cache-Control: no-store\r\n'
printf '\r\n'

fail() {
    "$JQ" -nc --arg error "$1" '{ok:false,error:$error}'
    exit 0
}

[ -n "$JQ" ] || {
    printf '{"ok":false,"error":"jq не установлен"}\n'
    exit 0
}

[ "${REQUEST_METHOD:-}" = "POST" ] ||
    fail "Разрешён только POST"

[ -x "$BRORAY" ] ||
    fail "CLI BROray не найден"

BODY="$(cat)"

printf '%s' "$BODY" | "$JQ" -e . >/dev/null 2>&1 ||
    fail "Получены некорректные данные"

URI="$(printf '%s' "$BODY" | "$JQ" -r '.uri // empty')"

[ -n "$URI" ] ||
    fail "Не передана конфигурация"

case "$URI" in
    vless://*|vmess://*|trojan://*|ss://*|hysteria2://*|hy2://*|tuic://*|socks://*|socks5://*|http://*|https://*)
        ;;
    *)
        fail "Неподдерживаемый формат конфигурации"
        ;;
esac

if ! "$BRORAY" import "$URI" >"$TMP" 2>&1; then
    fail "$(tail -n 8 "$TMP" | tr '\n' ' ')"
fi

"$JQ" -nc \
    --arg output "$(cat "$TMP")" \
    '{
        ok:true,
        message:"Конфигурация добавлена",
        output:$output
    }'
