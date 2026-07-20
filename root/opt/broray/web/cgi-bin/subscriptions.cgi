#!/bin/sh

PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"
export PATH

BRORAY="/opt/broray/bin/broray"
SUBS_DIR="/opt/broray/subscriptions"
JQ="$(command -v jq 2>/dev/null || true)"

printf 'Content-Type: application/json; charset=utf-8\r\n'
printf 'Cache-Control: no-store\r\n'
printf '\r\n'

if [ ! -x "$BRORAY" ]; then
    printf '{"ok":false,"error":"Команда BROray не найдена","subscriptions":[]}\n'
    exit 0
fi

if [ -z "$JQ" ]; then
    printf '{"ok":false,"error":"На роутере не найден jq","subscriptions":[]}\n'
    exit 0
fi

mkdir -p "$SUBS_DIR"

TMP="$(mktemp /tmp/broray-subscriptions.XXXXXX)" || {
    printf '{"ok":false,"error":"Не удалось создать временный файл","subscriptions":[]}\n'
    exit 0
}

trap 'rm -f "$TMP"' EXIT HUP INT TERM

for DIR in "$SUBS_DIR"/*
do
    [ -d "$DIR" ] || continue

    ID="$(basename "$DIR")"
    META="$DIR/web-meta.json"

    NAME="$ID"
    URL=""
    UPDATED_AT=""

    if [ -f "$META" ] && "$JQ" -e . "$META" >/dev/null 2>&1; then
        NAME="$("$JQ" -r '.name // .id // empty' "$META")"
        URL="$("$JQ" -r '.url // empty' "$META")"
        UPDATED_AT="$("$JQ" -r '.updated_at // .created_at // empty' "$META")"
        [ -n "$NAME" ] || NAME="$ID"
    fi

    NODES_OUTPUT="$("$BRORAY" subscription-nodes "$ID" 2>/dev/null || true)"

    NODE_COUNT="$(
        printf '%s\n' "$NODES_OUTPUT" |
            awk '
                /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\// {
                    count++
                }
                END {
                    print count + 0
                }
            '
    )"

    "$JQ" -nc \
        --arg id "$ID" \
        --arg name "$NAME" \
        --arg url "$URL" \
        --arg updated_at "$UPDATED_AT" \
        --argjson node_count "$NODE_COUNT" \
        '{
            id: $id,
            name: $name,
            url: $url,
            updated_at: $updated_at,
            node_count: $node_count
        }' >> "$TMP"
done

SUBSCRIPTIONS="$("$JQ" -s . "$TMP")"

"$JQ" -nc \
    --argjson subscriptions "$SUBSCRIPTIONS" \
    '{
        ok: true,
        subscriptions: $subscriptions
    }'
