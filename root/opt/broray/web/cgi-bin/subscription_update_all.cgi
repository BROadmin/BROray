#!/bin/sh

PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"
export PATH

BRORAY="/opt/broray/bin/broray"
SUBS_DIR="/opt/broray/subscriptions"
JQ="$(command -v jq 2>/dev/null || true)"

printf 'Content-Type: application/json; charset=utf-8\r\n'
printf 'Cache-Control: no-store\r\n'
printf '\r\n'

if [ "$REQUEST_METHOD" != "POST" ]; then
    printf '{"ok":false,"error":"Разрешён только POST-запрос"}\n'
    exit 0
fi

if [ ! -x "$BRORAY" ]; then
    printf '{"ok":false,"error":"Команда BROray не найдена"}\n'
    exit 0
fi

if [ -z "$JQ" ]; then
    printf '{"ok":false,"error":"На роутере не найден jq"}\n'
    exit 0
fi

TMP="$(mktemp /tmp/broray-update-all.XXXXXX)" || {
    printf '{"ok":false,"error":"Не удалось создать временный файл"}\n'
    exit 0
}

trap 'rm -f "$TMP"' EXIT HUP INT TERM

TOTAL=0
SUCCESS=0
FAILED=0

for DIR in "$SUBS_DIR"/*
do
    [ -d "$DIR" ] || continue

    ID="$(basename "$DIR")"
    TOTAL=$((TOTAL + 1))

    OUTPUT="$("$BRORAY" subscription-update "$ID" 2>&1)"
    STATUS=$?

    if [ "$STATUS" -eq 0 ]; then
        SUCCESS=$((SUCCESS + 1))
        RESULT=true

        META="$DIR/web-meta.json"
        NOW="$(date '+%Y-%m-%d %H:%M:%S')"

        if [ -f "$META" ] && "$JQ" -e . "$META" >/dev/null 2>&1; then
            TMP_META="$(mktemp /tmp/broray-meta.XXXXXX)"
            "$JQ" \
                --arg updated_at "$NOW" \
                '.updated_at = $updated_at' \
                "$META" > "$TMP_META"
            mv "$TMP_META" "$META"
        fi
    else
        FAILED=$((FAILED + 1))
        RESULT=false
    fi

    "$JQ" -nc \
        --arg id "$ID" \
        --arg output "$OUTPUT" \
        --argjson ok "$RESULT" \
        '{
            id:$id,
            ok:$ok,
            output:$output
        }' >> "$TMP"
done

RESULTS="$("$JQ" -s . "$TMP")"

"$JQ" -nc \
    --argjson total "$TOTAL" \
    --argjson success "$SUCCESS" \
    --argjson failed "$FAILED" \
    --argjson results "$RESULTS" \
    '{
        ok:($failed == 0),
        total:$total,
        success:$success,
        failed:$failed,
        results:$results
    }'
