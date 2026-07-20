#!/bin/sh

export PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"

BASE="/opt/broray"
BRORAY="$BASE/bin/broray"
JQ="/opt/bin/jq"

[ -x "$JQ" ] || JQ="$(command -v jq 2>/dev/null)"

printf 'Content-Type: application/json\r\n'
printf 'Cache-Control: no-store\r\n'
printf '\r\n'

if [ ! -x "$BRORAY" ]; then
    printf '%s\n' \
        '{"ok":false,"error":"Команда BROray не найдена"}'
    exit 0
fi

if [ -z "$JQ" ] || [ ! -x "$JQ" ]; then
    printf '%s\n' \
        '{"ok":false,"error":"На роутере не найден jq"}'
    exit 0
fi

STATUS="$(mktemp /tmp/broray-xray-status.XXXXXX)" || exit 1
VERSION="$(mktemp /tmp/broray-xray-version.XXXXXX)" || {
    rm -f "$STATUS"
    exit 1
}
CONFIG="$(mktemp /tmp/broray-xray-config.XXXXXX)" || {
    rm -f "$STATUS" "$VERSION"
    exit 1
}

cleanup() {
    rm -f "$STATUS" "$VERSION" "$CONFIG"
}

trap cleanup EXIT HUP INT TERM

"$BRORAY" xray status >"$STATUS" 2>/dev/null || printf '{}\n' >"$STATUS"
"$BRORAY" xray version >"$VERSION" 2>/dev/null || printf '{}\n' >"$VERSION"
"$BRORAY" xray config >"$CONFIG" 2>/dev/null || printf '{}\n' >"$CONFIG"

set -- $(df -k /opt 2>/dev/null | awk 'NR == 2 {
    print $2, $3, $4
}')

STORAGE_TOTAL_KB="${1:-0}"
STORAGE_USED_KB="${2:-0}"
STORAGE_FREE_KB="${3:-0}"

set -- $(df -k /tmp 2>/dev/null | awk 'NR == 2 {
    print $2, $3, $4
}')

TMP_TOTAL_KB="${1:-0}"
TMP_USED_KB="${2:-0}"
TMP_FREE_KB="${3:-0}"

BRORAY_SIZE_KB="$(
    du -sk "$BASE" 2>/dev/null |
    awk 'NR == 1 {print $1}'
)"
BRORAY_SIZE_KB="${BRORAY_SIZE_KB:-0}"

STORAGE_TOTAL_BYTES=$((STORAGE_TOTAL_KB * 1024))
STORAGE_USED_BYTES=$((STORAGE_USED_KB * 1024))
STORAGE_FREE_BYTES=$((STORAGE_FREE_KB * 1024))

TMP_TOTAL_BYTES=$((TMP_TOTAL_KB * 1024))
TMP_USED_BYTES=$((TMP_USED_KB * 1024))
TMP_FREE_BYTES=$((TMP_FREE_KB * 1024))

BRORAY_SIZE_BYTES=$((BRORAY_SIZE_KB * 1024))

if [ "$STORAGE_FREE_KB" -lt 20480 ]; then
    STORAGE_LEVEL="critical"
elif [ "$STORAGE_FREE_KB" -lt 40960 ]; then
    STORAGE_LEVEL="warning"
else
    STORAGE_LEVEL="normal"
fi

"$JQ" -n \
    --slurpfile status "$STATUS" \
    --slurpfile version "$VERSION" \
    --slurpfile config "$CONFIG" \
    --argjson storageTotalBytes "$STORAGE_TOTAL_BYTES" \
    --argjson storageUsedBytes "$STORAGE_USED_BYTES" \
    --argjson storageFreeBytes "$STORAGE_FREE_BYTES" \
    --argjson tmpTotalBytes "$TMP_TOTAL_BYTES" \
    --argjson tmpUsedBytes "$TMP_USED_BYTES" \
    --argjson tmpFreeBytes "$TMP_FREE_BYTES" \
    --argjson broraySizeBytes "$BRORAY_SIZE_BYTES" \
    --arg storageLevel "$STORAGE_LEVEL" \
'
{
    ok: true,
    status: $status[0],
    version: $version[0],
    config: $config[0],
    storage: {
        mount: "/opt",
        totalBytes: $storageTotalBytes,
        usedBytes: $storageUsedBytes,
        freeBytes: $storageFreeBytes,
        level: $storageLevel
    },
    temporaryStorage: {
        mount: "/tmp",
        totalBytes: $tmpTotalBytes,
        usedBytes: $tmpUsedBytes,
        freeBytes: $tmpFreeBytes
    },
    broray: {
        sizeBytes: $broraySizeBytes
    }
}
'
