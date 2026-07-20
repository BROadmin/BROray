#!/bin/sh

export PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"

BASE="/opt/broray"
BRORAY="$BASE/bin/broray"
TMP="$BASE/tmp/delete-cgi.$$"

cleanup() {
    rm -f "$TMP"
}
trap cleanup EXIT INT TERM

printf 'Content-Type: application/json; charset=UTF-8\r\n'
printf 'Cache-Control: no-store\r\n'
printf '\r\n'

json_error() {
    jq -n --arg error "$1" '{ok:false,error:$error}'
    exit 1
}

command -v jq >/dev/null 2>&1 || json_error "jq не установлен"
[ -x "$BRORAY" ] || json_error "CLI BROray не найден"
[ "${REQUEST_METHOD:-}" = "POST" ] || json_error "разрешён только POST"

BODY="$(cat)"

SERVER_ID="$(
printf '%s' "$BODY" |
tr '&' '\n' |
sed -n 's/^id=//p' |
head -n1
)"

[ -n "$SERVER_ID" ] || json_error "не передан ID сервера"

if ! "$BRORAY" delete "$SERVER_ID" >"$TMP" 2>&1; then
    json_error "$(tail -n5 "$TMP" | tr '\n' ' ')"
fi

"$BRORAY" current 2>/dev/null |
jq '
{
    ok:true,
    current:{
        id,
        uuid,
        name,
        address,
        port,
        network,
        security
    }
}'
