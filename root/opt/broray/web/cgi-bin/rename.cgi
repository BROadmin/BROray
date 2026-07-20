#!/bin/sh
export PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"

BRORAY="/opt/broray/bin/broray"
JQ="$(command -v jq 2>/dev/null || true)"
TMP="/opt/broray/tmp/rename-cgi.$$"

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

ID="$(printf '%s' "$BODY" | "$JQ" -r '.id // empty')"
NAME="$(printf '%s' "$BODY" | "$JQ" -r '.name // empty')"

[ -n "$ID" ] || fail "Не передан идентификатор сервера"
[ -n "$NAME" ] || fail "Не указано новое имя"

if ! "$BRORAY" rename "$ID" "$NAME" >"$TMP" 2>&1; then
    fail "$(tail -n 8 "$TMP" | tr '\n' ' ')"
fi

"$JQ" -nc \
    --arg output "$(cat "$TMP")" \
    '{
        ok:true,
        message:"Сервер переименован",
        output:$output
    }'
