#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
LIBRARY="$ROOT/lib/routes-router-export.sh"
WORK="$ROOT/routes/tmp/router-export-selftest-$$"

cleanup()
{
    rm -rf "$WORK"
}

fail()
{
    echo "ОШИБКА self-test: $*" >&2
    exit 1
}

trap cleanup EXIT HUP INT TERM

[ -r "$LIBRARY" ] ||
    fail "модуль экспорта недоступен"

. "$LIBRARY"

mkdir -p "$WORK/prepared" ||
    fail "не удалось создать тестовый каталог"

cat >"$WORK/plan.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "test",
  "sourceCommit": "1111111111111111111111111111111111111111",
  "sourceDate": "2026-01-01T00:00:00Z",
  "contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "targetInterface": "Proxy0",
  "routeComment": "BROray",
  "routeFileSha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "routeCount": 4,
  "routerApplied": false,
  "routes": [
    {
      "key": "ipv4|10.0.0.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "family": "ipv4",
      "network": "10.0.0.0",
      "prefix": 24,
      "mask": "255.255.255.0",
      "targetInterface": "Proxy0",
      "gatewayToken": "0.0.0.0",
      "metric": 1200,
      "automatic": null,
      "exclusive": null,
      "comment": "BROray"
    },
    {
      "key": "ipv4|20.0.0.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "family": "ipv4",
      "network": "20.0.0.0",
      "prefix": 24,
      "mask": "255.255.255.0",
      "targetInterface": "Proxy0",
      "gatewayToken": "0.0.0.0",
      "metric": 1200,
      "automatic": null,
      "exclusive": null,
      "comment": "BROray"
    },
    {
      "key": "ipv4|30.0.0.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "family": "ipv4",
      "network": "30.0.0.0",
      "prefix": 24,
      "mask": "255.255.255.0",
      "targetInterface": "Proxy0",
      "gatewayToken": "0.0.0.0",
      "metric": 1200,
      "automatic": null,
      "exclusive": null,
      "comment": "BROray"
    },
    {
      "key": "ipv4|40.0.0.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "family": "ipv4",
      "network": "40.0.0.0",
      "prefix": 24,
      "mask": "255.255.255.0",
      "targetInterface": "Proxy0",
      "gatewayToken": "0.0.0.0",
      "metric": 1200,
      "automatic": null,
      "exclusive": null,
      "comment": "BROray"
    }
  ]
}
JSON

cat >"$WORK/actual.json" <<'JSON'
{
  "schemaVersion": 1,
  "routes": [
    {
      "destination": "20.0.0.0/24",
      "gateway": "0.0.0.0",
      "interface": "Proxy0",
      "metric": 1200
    },
    {
      "destination": "30.0.0.0/24",
      "gateway": "0.0.0.0",
      "interface": "Proxy0",
      "metric": 1200
    },
    {
      "destination": "40.0.0.0/24",
      "gateway": "0.0.0.0",
      "interface": "Wireguard0",
      "metric": 1000
    }
  ]
}
JSON

cat >"$WORK/registry.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "routes": [
    {
      "key": "ipv4|30.0.0.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "family": "ipv4",
      "network": "30.0.0.0",
      "prefix": 24,
      "mask": "255.255.255.0",
      "interface": "Proxy0",
      "metric": 1200,
      "createdByBROray": true,
      "managed": true,
      "owners": ["old-service"],
      "fingerprint": "old"
    }
  ],
  "updatedAt": "2026-01-01T00:00:00+0000"
}
JSON

cat >"$WORK/bundle.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "test",
  "installedVersion": null,
  "routeKeys": [],
  "updatedAt": "2026-01-01T00:00:00+0000"
}
JSON

cat >"$WORK/state.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "test",
  "status": "downloaded",
  "availableVersion": {
    "sourceCommit": "1111111111111111111111111111111111111111",
    "sourceDate": "2026-01-01T00:00:00Z",
    "contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "downloadedVersion": {
    "sourceCommit": "1111111111111111111111111111111111111111",
    "sourceDate": "2026-01-01T00:00:00Z",
    "contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "installedVersion": null,
  "preflight": {
    "result": "ready",
    "routerChanged": false,
    "configurationSaved": false
  }
}
JSON

BRORAY_ROUTES_ROUTER_EXPORT_ACTIVE_WORK="$WORK"

broray_routes_router_export_classify \
    "$WORK/plan.json" \
    "$WORK/actual.json" \
    "$WORK/registry.json" \
    "$WORK/preflight.json" \
    "2026-01-02T00:00:00+0000" ||
    fail "классификация завершилась ошибкой"

jq -e '
    (.summary.total == 4) and
    (.summary.toCreate == 2) and
    (.summary.managedExisting == 1) and
    (.summary.externalExisting == 1) and
    (.summary.conflicts == 0) and
    (.canExport == false) and
    (.summary.withOtherInterfaceMatches == 1) and
    ([.routes[] | select(.network == "10.0.0.0")][0].status == "create") and
    ([.routes[] | select(.network == "20.0.0.0")][0].status == "external_existing") and
    ([.routes[] | select(.network == "30.0.0.0")][0].status == "managed_existing") and
    ([.routes[] | select(.network == "40.0.0.0")][0].status == "create")
' "$WORK/preflight.json" >/dev/null ||
    fail "классификация неверна"

jq '
    .routes |= map(
        select(.destination != "20.0.0.0/24")
    )
' "$WORK/actual.json" >"$WORK/actual-safe.json" ||
    fail "не удалось подготовить безопасный снимок маршрутов"

broray_routes_router_export_classify \
    "$WORK/plan.json" \
    "$WORK/actual-safe.json" \
    "$WORK/registry.json" \
    "$WORK/preflight.json" \
    "2026-01-02T00:00:00+0000" ||
    fail "безопасная классификация завершилась ошибкой"

jq -e '
    (.summary.toCreate == 3) and
    (.summary.managedExisting == 1) and
    (.summary.externalExisting == 0) and
    (.summary.conflicts == 0) and
    (.canExport == true)
' "$WORK/preflight.json" >/dev/null ||
    fail "безопасная классификация неверна"

broray_routes_router_export_prepare_files \
    "test" \
    "$WORK/preflight.json" \
    "$WORK/registry.json" \
    "$WORK/bundle.json" \
    "$WORK/state.json" \
    "$WORK/plan.json" \
    "$WORK/prepared" \
    "2026-01-02T00:00:00+0000" ||
    fail "подготовка реестров завершилась ошибкой"

jq -e '
    (.routes | length == 4) and
    (
        [
            .routes[] |
            select(.network == "30.0.0.0")
        ][0].owners |
        index("old-service") != null
    ) and
    (
        [
            .routes[] |
            select(.network == "30.0.0.0")
        ][0].owners |
        index("test") != null
    ) and
    ([.routes[] | select(.network == "20.0.0.0")] | length == 1) and
    (all(.routes[]; .interface == "Proxy0"))
' "$WORK/prepared/routes.json" >/dev/null ||
    fail "глобальный реестр сформирован неверно"

jq -e '
    (.routeKeys | length == 4) and
    (.managedRouteKeys | length == 4) and
    (.externalRouteKeys | length == 0) and
    (.installedVersion != null)
' "$WORK/prepared/bundle.json" >/dev/null ||
    fail "реестр набора сформирован неверно"

jq -e '
    (.status == "installed") and
    (.installedVersion != null) and
    (.exportResult.created == 3) and
    (.exportResult.externalExisting == 0)
' "$WORK/prepared/state.json" >/dev/null ||
    fail "состояние сформировано неверно"

echo "BROray routes router export self-test: PASS"
