#!/bin/sh

set -eu

EXPORT_LIBRARY="${1:-/opt/broray/lib/routes-export-build.sh}"
WORK="/tmp/broray-routes-export-selftest.$$"
ROOT="$WORK/root"
ROUTES="$ROOT/routes"
CATALOG="$ROUTES/catalog/demo"

cleanup()
{
    rm -rf "$WORK"
}

fail()
{
    echo "SELFTEST ERROR: $*" >&2
    exit 1
}

trap cleanup EXIT HUP INT TERM

mkdir -p \
    "$ROOT/lib" \
    "$ROUTES/catalog/demo" \
    "$ROUTES/state" \
    "$ROUTES/locks" \
    "$ROUTES/tmp"

cp "$EXPORT_LIBRARY" "$ROOT/lib/routes-export-build.sh"

cat >"$ROUTES/bundles.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundles": ["demo"]
}
JSON

cat >"$ROUTES/config.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routeComment": "BROray",
  "ownershipPolicy": {
    "touchOtherInterfaces": false,
    "modifyExternalRoutes": false,
    "deleteExternalRoutes": false
  }
}
JSON

cat >"$CATALOG/normalized.txt" <<'EOF_ROUTES'
10.0.0.0/8
192.168.50.0/24
EOF_ROUTES

CONTENT_SHA="$(sha256sum "$CATALOG/normalized.txt" | awk '{print $1}')"

jq -n \
    --arg sha "$CONTENT_SHA" \
    '{
        schemaVersion: 1,
        bundleId: "demo",
        targetInterface: "Proxy0",
        routeComment: "BROray",
        contentSha256: $sha,
        routeCount: 2,
        routes: [
            {family: "ipv4", network: "10.0.0.0", prefix: 8},
            {family: "ipv4", network: "192.168.50.0", prefix: 24}
        ]
    }' >"$CATALOG/routes.json"

jq -n \
    --arg sha "$CONTENT_SHA" \
    '{
        schemaVersion: 1,
        bundleId: "demo",
        sourceCommit: "0123456789abcdef0123456789abcdef01234567",
        sourceDate: "2026-01-01T00:00:00Z",
        contentSha256: $sha,
        routeCount: 2,
        targetInterface: "Proxy0"
    }' >"$CATALOG/version.json"

cat >"$ROUTES/state/demo.json" <<JSON
{
  "schemaVersion": 1,
  "bundleId": "demo",
  "status": "downloaded",
  "availableVersion": {
    "sourceCommit": "0123456789abcdef0123456789abcdef01234567",
    "sourceDate": "2026-01-01T00:00:00Z",
    "contentSha256": "$CONTENT_SHA"
  },
  "downloadedVersion": {
    "sourceCommit": "0123456789abcdef0123456789abcdef01234567",
    "sourceDate": "2026-01-01T00:00:00Z",
    "contentSha256": "$CONTENT_SHA"
  },
  "installedVersion": null,
  "routeCount": 2,
  "lastError": null
}
JSON

BRORAY_ROOT="$ROOT"
BRORAY_ROUTES_ROOT="$ROUTES"
export BRORAY_ROOT BRORAY_ROUTES_ROOT

. "$ROOT/lib/routes-export-build.sh"

broray_routes_export_build_run demo >/dev/null

[ -f "$CATALOG/keenetic-routes.bat" ] ||
    fail "Не создан файл Keenetic"

[ -f "$CATALOG/export-plan.json" ] ||
    fail "Не создан план экспорта"

[ "$(grep -c '^route add ' "$CATALOG/keenetic-routes.bat")" = "2" ] ||
    fail "Неверное число строк BAT"

grep -Fq \
    'route add 10.0.0.0 mask 255.0.0.0 0.0.0.0 metric 1200 :: rem BROray' \
    "$CATALOG/keenetic-routes.bat" ||
    fail "Неверная строка /8"

grep -Fq \
    'route add 192.168.50.0 mask 255.255.255.0 0.0.0.0 metric 1200 :: rem BROray' \
    "$CATALOG/keenetic-routes.bat" ||
    fail "Неверная строка /24"

jq -e '
    (.routerApplied == false) and
    (.targetInterface == "Proxy0") and
    (.routeComment == "BROray") and
    (.routeCount == 2) and
    ((.routes | length) == 2) and
    (all(.routes[]; (.targetInterface == "Proxy0") and (.metric == 1200)))
' "$CATALOG/export-plan.json" >/dev/null ||
    fail "План экспорта повреждён"

jq -e '
    (.exportBuild.routerApplied == false) and
    (.exportBuild.targetInterface == "Proxy0") and
    (.exportBuild.message == "Маршруты готовы к экспорту")
' "$ROUTES/state/demo.json" >/dev/null ||
    fail "Состояние не обновлено"

[ ! -d "$ROUTES/locks/operation.lock" ] ||
    fail "Блокировка не снята"

echo "BROray routes export build self-test: PASS"
