#!/opt/bin/ash
set -eu
umask 077

FILE=/opt/broray/lib/routes-router-sync.sh
EXPECTED_SHA=f69e73c3bad58e891b0951be6cce39221e94b93c67153b2c0fef2f8c7c78f81b
OLD='    broray_route_resource_acquire "$BRORAY_SYNC_LOCK" "sync" "" || return $?'
NEW='    broray_route_resource_acquire "$BRORAY_SYNC_LOCK" "sync" "${BRORAY_SYNC_BUNDLE:-}" || return $?'

fail()
{
    echo "ROUTES_SYNC_HOTFIX=FAIL REASON=$1" >&2
    exit 1
}

[ -f "$FILE" ] && [ ! -L "$FILE" ] || fail unsafe-file

actual_sha="$(sha256sum "$FILE" | awk 'NR==1{print $1}')"
if [ "$actual_sha" != "$EXPECTED_SHA" ]; then
    if grep -F "$NEW" "$FILE" >/dev/null 2>&1; then
        echo "ROUTES_SYNC_HOTFIX=PASS ALREADY_APPLIED"
        exit 0
    fi
    fail unexpected-source
fi

[ "$(grep -Fxc "$OLD" "$FILE" 2>/dev/null || true)" = 1 ] || fail old-line-count
[ "$(grep -Fxc "$NEW" "$FILE" 2>/dev/null || true)" = 0 ] || fail already-patched-mixed

BACKUP="/opt/broray/backup/routes-sync-bundle-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP" || fail backup-dir
cp -p "$FILE" "$BACKUP/routes-router-sync.sh" || fail backup

TMP="$FILE.new.$$"
cp -p "$FILE" "$TMP" || fail stage
trap 'rm -f "$TMP"' EXIT HUP INT TERM

awk -v old="$OLD" -v new="$NEW" '
    $0 == old { print new; changed++; next }
    { print }
    END { if (changed != 1) exit 7 }
' "$FILE" >"$TMP" || fail replace

[ "$(grep -Fxc "$NEW" "$TMP" 2>/dev/null || true)" = 1 ] || fail new-line-count
[ "$(grep -Fxc "$OLD" "$TMP" 2>/dev/null || true)" = 0 ] || fail old-line-remains

/opt/bin/ash -n "$TMP" || fail syntax
chmod --reference="$FILE" "$TMP" 2>/dev/null || chmod 755 "$TMP" || fail chmod
mv -f "$TMP" "$FILE" || fail install
trap - EXIT HUP INT TERM

echo "ROUTES_SYNC_HOTFIX=PASS BACKUP=$BACKUP"
