#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
WORK="$ROOT/routes/tmp/routes-parallel-metric-selftest-$$"

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

mkdir -p "$WORK"

. "$ROOT/lib/routes-router-preflight.sh"
. "$ROOT/lib/routes-router-export.sh"

cat >"$WORK/plan.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "test",
  "sourceCommit": "test",
  "contentSha256": "test",
  "targetInterface": "Proxy0",
  "routes": [
    {
      "key": "ipv4|198.51.100.254/32|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "family": "ipv4",
      "network": "198.51.100.254",
      "prefix": 32,
      "mask": "255.255.255.255",
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

cat >"$WORK/actual-other.json" <<'JSON'
{
  "schemaVersion": 1,
  "routes": [
    {
      "destination": "198.51.100.254/32",
      "gateway": "0.0.0.0",
      "interface": "Wireguard0",
      "metric": 1100,
      "proto": "static"
    }
  ]
}
JSON

cat >"$WORK/registry.json" <<'JSON'
{"schemaVersion":1,"managedInterface":"Proxy0","routes":[]}
JSON

broray_routes_preflight_classify \
    "$WORK/plan.json" \
    "$WORK/actual-other.json" \
    "$WORK/registry.json" \
    "$WORK/preflight.json" \
    "2026-07-23T00:00:00+0300" ||
    fail "Классификация preflight завершилась ошибкой"

jq -e '
    (.canExport == true) and
    (.summary.toCreate == 1) and
    (.summary.conflicts == 0) and
    (.summary.parallelStaticOther == 1) and
    (.routes[0].status == "create") and
    (.routes[0].parallelStaticInterfaces == ["Wireguard0"])
' "$WORK/preflight.json" >/dev/null ||
    fail "Параллельный маршрут ошибочно признан конфликтом"

broray_routes_router_export_classify \
    "$WORK/plan.json" \
    "$WORK/actual-other.json" \
    "$WORK/registry.json" \
    "$WORK/export-preflight.json" \
    "2026-07-23T00:00:00+0300" ||
    fail "Классификация export завершилась ошибкой"

jq -e '
    (.canExport == true) and
    (.summary.toCreate == 1) and
    (.summary.conflicts == 0)
' "$WORK/export-preflight.json" >/dev/null ||
    fail "Export не разрешил параллельный маршрут"

cat >"$WORK/actual-proxy.json" <<'JSON'
{
  "schemaVersion": 1,
  "routes": [
    {
      "destination": "198.51.100.254/32",
      "gateway": "0.0.0.0",
      "interface": "Proxy0",
      "metric": 1200,
      "proto": "static"
    }
  ]
}
JSON

COUNT="$(broray_routes_router_export_exact_count "$WORK/actual-proxy.json" "198.51.100.254/32" "Proxy0")"
[ "$COUNT" = "1" ] || fail "Маршрут Proxy0 с метрикой 1200 не подтверждён"

broray_routes_router_export_verify_expected_rci \
    "$WORK/plan.json" \
    "$WORK/actual-proxy.json" ||
    fail "Итоговая проверка метрики 1200 завершилась ошибкой"

echo "BROray routes parallel metric self-test: PASS"
