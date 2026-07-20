#!/bin/sh

export PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"

BASE="/opt/broray"
BRORAY="$BASE/bin/broray"
CONNECTION_FILE="$BASE/run/connection-status.json"

printf 'Content-Type: application/json\r\n'
printf 'Cache-Control: no-store\r\n'
printf '\r\n'

if pidof xray >/dev/null 2>&1; then
    XRAY_STATUS="running"
else
    XRAY_STATUS="stopped"
fi

VERSION="$(
    "$BRORAY" version 2>/dev/null |
        sed 's/^BROray //'
)"

CURRENT_NAME="$(
    "$BRORAY" current-name 2>/dev/null || true
)"

CURRENT_ID="$(
    "$BRORAY" current-id 2>/dev/null || true
)"

CURRENT_ADDRESS="$(
    "$BRORAY" current-address 2>/dev/null || true
)"

CURRENT_PORT="$(
    "$BRORAY" current-port 2>/dev/null || echo 0
)"

UPTIME="$(
    uptime |
        sed 's/.*up //;s/, *[0-9]* user.*//'
)"

if [ -s "$CONNECTION_FILE" ] &&
   jq -e . "$CONNECTION_FILE" >/dev/null 2>&1; then
    CONNECTION_JSON="$(cat "$CONNECTION_FILE")"
else
    CONNECTION_JSON='
    {
      "available":false,
      "up":false,
      "address":"",
      "ping_ms":null,
      "jitter_ms":null,
      "packet_loss_percent":null,
      "quality_score":0,
      "quality":"Ожидание",
      "checked_at":null,
      "breaks":0
    }
    '
fi

jq -n \
    --arg status "$XRAY_STATUS" \
    --arg version "$VERSION" \
    --arg uptime "$UPTIME" \
    --arg name "$CURRENT_NAME" \
    --arg id "$CURRENT_ID" \
    --arg address "$CURRENT_ADDRESS" \
    --argjson port "${CURRENT_PORT:-0}" \
    --argjson connection "$CONNECTION_JSON" \
'
{
  ok:true,
  xray:{
    status:$status
  },
  broray:{
    version:$version
  },
  system:{
    uptime:$uptime
  },
  current:{
    name:$name,
    id:$id,
    address:$address,
    port:$port
  },
  connection:$connection
}
'
