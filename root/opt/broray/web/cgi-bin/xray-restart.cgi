#!/bin/sh

export PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"

BASE="/opt/broray"
BRORAY="$BASE/bin/broray"

printf 'Content-Type: application/json\r\n'
printf 'Cache-Control: no-store\r\n'
printf '\r\n'

OUTPUT="$(""$BRORAY"" xray restart 2>&1)"
STATUS=$?

jq -n     --arg output "$OUTPUT"     --arg action "restart"     --argjson ok $([ $STATUS -eq 0 ] && echo true || echo false) '
{
    ok: $ok,
    action: $action,
    output: $output
}
'
