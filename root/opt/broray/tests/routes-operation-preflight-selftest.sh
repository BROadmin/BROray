#!/bin/sh

set -eu

SOURCE_ROOT="${BRORAY_ROOT:-/opt/broray}"
LIBRARY="$SOURCE_ROOT/lib/routes-operation-preflight.sh"
WORK="${TMPDIR:-/tmp}/broray-routes-operation-preflight-selftest-$$"
ROOT="$WORK/root"
ROUTES="$ROOT/routes"
PLAN="$WORK/plan.json"
RESULT="$WORK/result.json"
MOCK_NDMC="$WORK/ndmc"
MOCK_CONFIG_LIBRARY="$WORK/routes-router-config.sh"

cleanup()
{
    rm -rf "$WORK"
}

fail()
{
    echo "BROray routes operation preflight self-test: FAIL — $*" >&2
    exit 1
}

expect_rc()
{
    expected="$1"
    shift
    rc=0
    "$@" || rc=$?
    [ "$rc" -eq "$expected" ] ||
        fail "ожидался код $expected, получен $rc: $*"
}

trap cleanup EXIT HUP INT TERM

[ -r "$LIBRARY" ] || fail "модуль предварительной проверки недоступен"
mkdir -p \
    "$ROUTES/catalog/demo" \
    "$ROUTES/state" \
    "$ROUTES/installed/bundles" \
    "$ROUTES/operations" \
    "$ROUTES/preflight"

cat >"$MOCK_NDMC" <<'NDMC'
#!/bin/sh
if [ "${1:-}" = "-c" ] && [ "${2:-}" = "show interface Proxy0" ]; then
    cat <<'OUT'
id: Proxy0
connected: yes
state: up
OUT
    exit 0
fi
exit 1
NDMC
chmod 755 "$MOCK_NDMC"

cat >"$MOCK_CONFIG_LIBRARY" <<'LIB'
broray_routes_config_fetch()
{
    cp "$PREFLIGHT_ACTUAL_FIXTURE" "$1"
}
LIB

cat >"$ROUTES/state/demo.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "demo",
  "downloadedVersion": {"contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
}
JSON
cat >"$ROUTES/installed/bundles/demo.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "demo",
  "installedVersion": {"contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "routeKeys": ["r1", "r2", "r3"],
  "managedRouteKeys": ["r1", "r2", "r3"],
  "externalRouteKeys": [],
  "targetInterface": "Proxy0",
  "managedMetric": 1200
}
JSON
cat >"$ROUTES/config.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200
}
JSON
cat >"$ROUTES/installed/routes.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routes": [
    {"key":"r1","network":"198.51.100.0","prefix":24,"mask":"255.255.255.0","interface":"Proxy0","metric":1200,"createdByBROray":true,"managed":true,"owners":["demo"]},
    {"key":"r2","network":"203.0.113.0","prefix":24,"mask":"255.255.255.0","interface":"Proxy0","metric":1200,"createdByBROray":true,"managed":true,"owners":["demo","shared"]},
    {"key":"r3","network":"192.0.2.0","prefix":24,"mask":"255.255.255.0","interface":"Proxy0","metric":1200,"createdByBROray":true,"managed":true,"owners":["demo"]}
  ]
}
JSON
cat >"$WORK/actual.json" <<'JSON'
{
  "schemaVersion": 1,
  "routes": [
    {"network":"198.51.100.0","prefix":24,"mask":"255.255.255.0","interface":"Proxy0","gateway":"0.0.0.0","metric":1200,"proto":"static"},
    {"network":"203.0.113.0","prefix":24,"mask":"255.255.255.0","interface":"Proxy0","gateway":"0.0.0.0","metric":1200,"proto":"static"}
  ]
}
JSON
cat >"$PLAN" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "demo",
  "mode": "install",
  "targetInterface": "Proxy0",
  "managedMetric": 1200,
  "contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "canApply": true,
  "summary": {
    "total": 3,
    "managedExisting": 1,
    "toCreate": 2,
    "toDelete": 0,
    "sharedKept": 0,
    "alreadyAbsent": 0,
    "conflicts": 0,
    "externalExisting": 0,
    "unchangedRoutes": 1,
    "addedRoutes": 2,
    "removedRoutes": 0
  }
}
JSON

BRORAY_ROOT="$ROOT"
BRORAY_ROUTES_ROOT="$ROUTES"
BRORAY_ROUTES_OPERATION_PREFLIGHT_DIR="$ROUTES/preflight"
BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC="$MOCK_NDMC"
BRORAY_ROUTES_OPERATION_PREFLIGHT_CONFIG_LIBRARY="$MOCK_CONFIG_LIBRARY"
PREFLIGHT_ACTUAL_FIXTURE="$WORK/actual.json"
export BRORAY_ROOT BRORAY_ROUTES_ROOT BRORAY_ROUTES_OPERATION_PREFLIGHT_DIR
export BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC BRORAY_ROUTES_OPERATION_PREFLIGHT_CONFIG_LIBRARY
export PREFLIGHT_ACTUAL_FIXTURE
. "$LIBRARY"

export_values="$(broray_routes_operation_preflight_resolve_action demo export)"
[ "$(printf '%s' "$export_values" | cut -f1)" = export ] &&
[ -z "$(printf '%s' "$export_values" | cut -f2)" ] ||
    fail "обычная установка получила неверный сохранённый тип операции"

broray_routes_operation_preflight_finalize demo export export install "$PLAN" "$RESULT" ||
    fail "готовая предварительная проверка не сформирована"

jq -e '
    .schemaVersion == 1 and .ready == true and
    .requestedAction == "export" and .resolvedAction == "export" and
    .operation == "install" and .summary.total == 3 and
    .summary.toCreate == 2 and .checks.operationLock.ok == true and
    .checks.ndmc.ok == true and .checks.keenetic.ok == true and
    .checks.storage.ok == true and .checks.localSet.ok == true and
    (.token | length) == 64
' "$RESULT" >/dev/null || fail "результат готовой проверки неверен"

token="$(jq -r '.token' "$RESULT")"
broray_routes_operation_preflight_validate demo export "$token" ||
    fail "актуальный токен не принят"
[ ! -e "$ROUTES/preflight/demo.json" ] || fail "использованный токен не удалён"

broray_routes_operation_preflight_finalize demo export export install "$PLAN" "$RESULT" ||
    fail "повторная проверка не сформирована"
token="$(jq -r '.token' "$RESULT")"
file="$ROUTES/preflight/demo.json"
jq '.checkedEpoch = 0' "$file" >"$file.new" && mv "$file.new" "$file"
expect_rc 3 broray_routes_operation_preflight_validate demo export "$token"

broray_routes_operation_preflight_finalize demo export export install "$PLAN" "$RESULT" ||
    fail "проверка перед изменением набора не сформирована"
token="$(jq -r '.token' "$RESULT")"
jq '.downloadedVersion.contentSha256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' \
    "$ROUTES/state/demo.json" >"$ROUTES/state/demo.json.new" &&
    mv "$ROUTES/state/demo.json.new" "$ROUTES/state/demo.json"
expect_rc 4 broray_routes_operation_preflight_validate demo export "$token"
jq '.downloadedVersion.contentSha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"' \
    "$ROUTES/state/demo.json" >"$ROUTES/state/demo.json.new" &&
    mv "$ROUTES/state/demo.json.new" "$ROUTES/state/demo.json"

cat >"$ROUTES/operations/demo.json" <<'JSON'
{
  "schemaVersion": 2,
  "bundleId": "demo",
  "operation": "install",
  "current": 1,
  "total": 3,
  "running": false,
  "resumable": true
}
JSON
resume_values="$(broray_routes_operation_preflight_resolve_action demo resume)"
[ "$(printf '%s' "$resume_values" | cut -f1)" = export ] &&
[ "$(printf '%s' "$resume_values" | cut -f2)" = install ] ||
    fail "продолжение установки разрешено неверно"
broray_routes_operation_preflight_finalize demo resume export install "$PLAN" "$RESULT" ||
    fail "проверка продолжения не сформирована"
jq -e '.ready == true and .resume.operation == "install" and .resume.current == 1 and .resume.total == 3' \
    "$RESULT" >/dev/null || fail "параметры продолжения неверны"
token="$(jq -r '.token' "$RESULT")"
jq '.current = 2' "$ROUTES/operations/demo.json" >"$ROUTES/operations/demo.json.new" &&
    mv "$ROUTES/operations/demo.json.new" "$ROUTES/operations/demo.json"
expect_rc 4 broray_routes_operation_preflight_validate demo resume "$token"

PREFLIGHT_ACTUAL_FIXTURE="$WORK/actual.json"
export PREFLIGHT_ACTUAL_FIXTURE
broray_routes_operation_preflight_delete_plan demo "$WORK/delete-plan.json" ||
    fail "план удаления не сформирован"
jq -e '
    .canApply == true and .mode == "delete" and
    .summary.total == 3 and .summary.toDelete == 1 and
    .summary.sharedKept == 1 and .summary.alreadyAbsent == 1 and
    .summary.conflicts == 0
' "$WORK/delete-plan.json" >/dev/null || fail "сводка предварительного удаления неверна"

jq '(.routes[] | select(.key == "r1") | .owners) = []' \
    "$ROUTES/installed/routes.json" >"$ROUTES/installed/routes.json.new" &&
    mv "$ROUTES/installed/routes.json.new" "$ROUTES/installed/routes.json"
broray_routes_operation_preflight_delete_plan demo "$WORK/delete-conflict.json" ||
    fail "конфликтный план удаления не сформирован"
jq -e '.canApply == false and .summary.conflicts == 1' "$WORK/delete-conflict.json" >/dev/null ||
    fail "конфликт владения не заблокировал удаление"

cat >"$WORK/blocked-plan.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "demo",
  "mode": "install",
  "targetInterface": "Proxy0",
  "managedMetric": 1200,
  "contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "canApply": false,
  "summary": {"total":3,"managedExisting":0,"toCreate":0,"toDelete":0,"sharedKept":0,"alreadyAbsent":0,"conflicts":1,"externalExisting":0,"unchangedRoutes":0,"addedRoutes":0,"removedRoutes":0}
}
JSON
broray_routes_operation_preflight_finalize demo export export install "$WORK/blocked-plan.json" "$RESULT" ||
    fail "заблокированная проверка не сформирована"
jq -e '.ready == false and .checks.ownership.ok == false and .summary.conflicts == 1' "$RESULT" >/dev/null ||
    fail "конфликтный план ошибочно разрешён"

expect_rc 2 broray_routes_operation_preflight_validate demo export "$(jq -r '.token' "$RESULT")"

echo "BROray routes operation preflight self-test: PASS"
