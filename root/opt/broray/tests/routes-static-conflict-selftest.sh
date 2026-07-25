#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
PREFLIGHT="$ROOT/lib/routes-router-preflight.sh"
EXPORT="$ROOT/lib/routes-router-export.sh"
WORK="$ROOT/routes/tmp/routes-static-conflict-selftest-$$"

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

[ -r "$PREFLIGHT" ] || fail "модуль preflight недоступен"
[ -r "$EXPORT" ] || fail "модуль export недоступен"

. "$PREFLIGHT"
. "$EXPORT"

mkdir -p "$WORK" || fail "не удалось создать тестовый каталог"

cat >"$WORK/plan.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "test",
  "sourceCommit": "1111111111111111111111111111111111111111",
  "contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "targetInterface": "Proxy0",
  "routes": [
    {"key":"k1","family":"ipv4","network":"10.0.0.0","prefix":8,"targetInterface":"Proxy0","metric":1200},
    {"key":"k2","family":"ipv4","network":"20.0.0.0","prefix":8,"targetInterface":"Proxy0","metric":1200},
    {"key":"k3","family":"ipv4","network":"30.0.0.0","prefix":8,"targetInterface":"Proxy0","metric":1200},
    {"key":"k4","family":"ipv4","network":"40.0.0.0","prefix":8,"targetInterface":"Proxy0","metric":1200}
  ]
}
JSON

cat >"$WORK/actual.json" <<'JSON'
{
  "schemaVersion": 1,
  "routes": [
    {"destination":"10.0.0.0/8","interface":"Wireguard0","gateway":"0.0.0.0","proto":"dhcp"},
    {"destination":"20.0.0.0/8","interface":"Wireguard0","gateway":"0.0.0.0","proto":"static"},
    {"destination":"30.0.0.0/8","interface":"Proxy0","gateway":"0.0.0.0","metric":1200,"proto":"static"},
    {"destination":"40.0.0.0/8","interface":"Proxy0","gateway":"0.0.0.0","metric":1200,"proto":"static"}
  ]
}
JSON

cat >"$WORK/registry.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "routes": [
    {"key":"k3","createdByBROray":true,"managed":true}
  ]
}
JSON

broray_routes_preflight_classify \
    "$WORK/plan.json" \
    "$WORK/actual.json" \
    "$WORK/registry.json" \
    "$WORK/preflight.json" \
    "2026-01-01T00:00:00+0000" ||
    fail "классификация preflight завершилась ошибкой"

jq -e '
    (.schemaVersion == 2) and
    (.source == "local-rci") and
    (.summary.toCreate == 2) and
    (.summary.managedExisting == 1) and
    (.summary.externalExisting == 1) and
    (.summary.conflicts == 0) and
    (.summary.staticOtherConflicts == 0) and
    (.summary.parallelStaticOther == 1) and
    (.summary.withOtherInterfaceMatches == 2) and
    (.canExport == false) and
    ([.routes[] | select(.key == "k1")][0].status == "create") and
    ([.routes[] | select(.key == "k2")][0].status == "create") and
    ([.routes[] | select(.key == "k3")][0].status == "managed_existing") and
    ([.routes[] | select(.key == "k4")][0].status == "external_existing")
' "$WORK/preflight.json" >/dev/null ||
    fail "неверная классификация маршрутов"

broray_routes_router_export_static_conflicts \
    "$WORK/plan.json" \
    "$WORK/actual.json" \
    "$WORK/conflicts.json" ||
    fail "проверка статических конфликтов завершилась ошибкой"

jq -e '
    length == 0
' "$WORK/conflicts.json" >/dev/null ||
    fail "параллельный статический маршрут ошибочно признан конфликтом"

COUNT="$(
    broray_routes_router_export_exact_count \
        "$WORK/actual.json" \
        "30.0.0.0/8" \
        "Proxy0"
)"

[ "$COUNT" = "1" ] ||
    fail "точный маршрут Proxy0 подсчитан неверно"

if grep -Fq 'for (index=NR;' "$EXPORT"; then
    fail "в BusyBox awk осталось зарезервированное имя index"
fi

grep -Fq 'Renewed static route' "$EXPORT" ||
    fail "защита от обновления чужого маршрута отсутствует"

echo "BROray routes static conflict self-test: PASS"
