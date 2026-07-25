#!/bin/sh

set -eu

LIBRARY="${1:-/opt/broray/lib/routes-source-check.sh}"
WORK="/tmp/broray-routes-source-universal-selftest.$$"
ROUTES="$WORK/routes"
FIXTURES="$WORK/fixtures"

cleanup()
{
    rm -rf "$WORK"
}

trap cleanup EXIT HUP INT TERM

mkdir -p \
    "$ROUTES/manifests" \
    "$ROUTES/state" \
    "$ROUTES/locks" \
    "$ROUTES/tmp" \
    "$FIXTURES"

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

cat >"$ROUTES/state/demo.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "demo",
  "status": "not_checked",
  "availableVersion": null,
  "downloadedVersion": null,
  "installedVersion": null,
  "routeCount": null,
  "lastCheckedAt": null,
  "lastDownloadedAt": null,
  "lastExportedAt": null,
  "lastError": null,
  "updatedAt": "2026-01-01T00:00:00+0000"
}
JSON

cat >"$FIXTURES/commit.json" <<'JSON'
[
  {
    "sha": "0123456789abcdef0123456789abcdef01234567",
    "commit": {
      "committer": {
        "date": "2026-01-02T03:04:05Z"
      }
    }
  }
]
JSON

cat >"$FIXTURES/one.bat" <<'BAT'
# demo one
route add 91.108.4.0 mask 255.255.252.0 0.0.0.0
route add 104.16.0.0 mask 255.240.0.0 0.0.0.0
BAT

cat >"$FIXTURES/two.bat" <<'BAT'
# duplicate plus one
route add 91.108.4.0 mask 255.255.252.0 0.0.0.0
route add 149.154.160.0 mask 255.255.240.0 0.0.0.0
BAT

BRORAY_ROUTES_ROOT="$ROUTES"
BRORAY_ROUTES_HTTP_FIXTURE_DIR="$FIXTURES"
export BRORAY_ROUTES_ROOT BRORAY_ROUTES_HTTP_FIXTURE_DIR

. "$LIBRARY"

broray_routes_bundle_id_valid demo
! broray_routes_bundle_id_valid '../demo'
! broray_routes_bundle_id_valid 'Demo'

broray_routes_check_run demo >/dev/null

jq -e '
    (.bundleId == "demo") and
    (.status == "available") and
    (.routeCount == 3) and
    (.checkResult.sourceFileCount == 2) and
    (.checkResult.managedInterface == "Proxy0") and
    (.lastError == null)
' "$ROUTES/state/demo.json" >/dev/null

[ ! -d "$ROUTES/locks/operation.lock" ]

if grep -E \
    'ndmc|show running-config|show interface|system configuration save' \
    "$LIBRARY" >/dev/null 2>&1
then
    echo "ОШИБКА: найдено запрещённое обращение к Keenetic CLI" >&2
    exit 1
fi

echo "BROray universal routes source self-test: PASS"
