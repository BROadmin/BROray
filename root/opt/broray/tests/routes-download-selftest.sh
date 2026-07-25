#!/bin/sh

set -eu

DOWNLOAD_LIBRARY="${1:-/opt/broray/lib/routes-download.sh}"
SOURCE_LIBRARY="${2:-/opt/broray/lib/routes-source-check.sh}"
WORK="/tmp/broray-routes-download-selftest.$$"
ROOT="$WORK/root"
ROUTES="$ROOT/routes"
FIXTURES="$WORK/fixtures"

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
    "$ROUTES/manifests" \
    "$ROUTES/state" \
    "$ROUTES/catalog" \
    "$ROUTES/backup" \
    "$ROUTES/locks" \
    "$ROUTES/tmp" \
    "$FIXTURES"

cp "$SOURCE_LIBRARY" "$ROOT/lib/routes-source-check.sh"
cp "$DOWNLOAD_LIBRARY" "$ROOT/lib/routes-download.sh"

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
  "routeComment": "BROray",
  "ownershipPolicy": {
    "touchOtherInterfaces": false
  }
}
JSON

cat >"$ROUTES/manifests/demo.json" <<'JSON'
{
  "schemaVersion": 1,
  "id": "demo",
  "name": "Demo",
  "source": {
    "provider": "github",
    "repository": "example/routes",
    "branch": "main",
    "directory": "Global/Demo",
    "files": [
      {
        "name": "one.bat",
        "type": "windows-route-bat",
        "required": true,
        "enabled": true
      },
      {
        "name": "two.bat",
        "type": "windows-route-bat",
        "required": true,
        "enabled": true
      }
    ]
  },
  "targetInterface": "Proxy0",
  "exportComment": "BROray",
  "limits": {
    "maxSourceBytes": 65536,
    "maxRoutes": 20
  }
}
JSON

cat >"$FIXTURES/one.bat" <<'BAT'
route add 91.108.4.0 mask 255.255.252.0 0.0.0.0
route add 104.16.0.0 mask 255.240.0.0 0.0.0.0
BAT

cat >"$FIXTURES/two.bat" <<'BAT'
route add 91.108.4.0 mask 255.255.252.0 0.0.0.0
route add 149.154.160.0 mask 255.255.240.0 0.0.0.0
BAT

cat >"$WORK/expected.txt" <<'TXT'
104.16.0.0/12
149.154.160.0/20
91.108.4.0/22
TXT

EXPECTED_SHA="$(sha256sum "$WORK/expected.txt" | awk '{print $1}')"
COMMIT="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

cat >"$ROUTES/state/demo.json" <<JSON
{
  "schemaVersion": 1,
  "bundleId": "demo",
  "status": "available",
  "availableVersion": {
    "sourceCommit": "$COMMIT",
    "sourceDate": "2026-07-22T00:00:00Z",
    "contentSha256": "$EXPECTED_SHA"
  },
  "downloadedVersion": null,
  "installedVersion": null,
  "routeCount": 3,
  "lastCheckedAt": "2026-07-22T00:00:00+0000",
  "lastDownloadedAt": null,
  "lastExportedAt": null,
  "lastError": null,
  "updatedAt": "2026-07-22T00:00:00+0000"
}
JSON

BRORAY_ROOT="$ROOT"
BRORAY_ROUTES_ROOT="$ROUTES"
BRORAY_ROUTES_HTTP_FIXTURE_DIR="$FIXTURES"
BRORAY_ROUTES_DOWNLOAD_SOURCE_LIBRARY="$ROOT/lib/routes-source-check.sh"
export BRORAY_ROOT BRORAY_ROUTES_ROOT BRORAY_ROUTES_HTTP_FIXTURE_DIR BRORAY_ROUTES_DOWNLOAD_SOURCE_LIBRARY

. "$ROOT/lib/routes-download.sh"

broray_routes_download_run demo >/dev/null

[ -f "$ROUTES/catalog/demo/source/one.bat" ] ||
    fail "не сохранён первый исходный файл"

[ -f "$ROUTES/catalog/demo/source/two.bat" ] ||
    fail "не сохранён второй исходный файл"

cmp "$WORK/expected.txt" "$ROUTES/catalog/demo/normalized.txt" >/dev/null ||
    fail "нормализованный список не совпал"

jq -e \
    --arg sha "$EXPECTED_SHA" \
    '
        (.status == "downloaded") and
        (.downloadedVersion.contentSha256 == $sha) and
        (.routeCount == 3) and
        (.lastError == null)
    ' "$ROUTES/state/demo.json" >/dev/null ||
    fail "состояние загрузки некорректно"

jq -e '
    (.schemaVersion == 1) and
    (.bundleId == "demo") and
    (.targetInterface == "Proxy0") and
    (.routeComment == "BROray") and
    (.routeCount == 3) and
    ((.routes | length) == 3)
' "$ROUTES/catalog/demo/routes.json" >/dev/null ||
    fail "каталог маршрутов некорректен"

[ ! -d "$ROUTES/locks/operation.lock" ] ||
    fail "блокировка не снята"

echo "BROray routes download self-test: PASS"
