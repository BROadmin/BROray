#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
LIBRARY="$ROOT/lib/routes-router-delete.sh"
PROGRESS_LIBRARY="$ROOT/lib/routes-operation-progress.sh"
WORK="$ROOT/routes/tmp/router-delete-selftest-$$"
MOCK_BIN="$WORK/bin"
TEST_ROOT="$WORK/root"
LOG="$WORK/ndmc.log"

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

[ -r "$LIBRARY" ] || fail "Модуль удаления недоступен"
[ -r "$PROGRESS_LIBRARY" ] || fail "Модуль прогресса недоступен"

mkdir -p \
    "$MOCK_BIN" \
    "$TEST_ROOT/lib" \
    "$TEST_ROOT/bin" \
    "$TEST_ROOT/routes/installed/bundles" \
    "$TEST_ROOT/routes/state" \
    "$TEST_ROOT/routes/catalog/telegram" \
    "$TEST_ROOT/routes/locks" \
    "$TEST_ROOT/routes/tmp" \
    "$TEST_ROOT/routes/transactions"

cp -p "$LIBRARY" "$TEST_ROOT/lib/routes-router-delete.sh"
cp -p "$PROGRESS_LIBRARY" "$TEST_ROOT/lib/routes-operation-progress.sh"

cat >"$TEST_ROOT/lib/interface-owner.sh" <<'OWNER_LIBRARY'
#!/bin/sh

broray_interface_owner_record_valid()
{
    return 0
}

broray_interface_exists_name()
{
    return 1
}

broray_interface_owner_valid()
{
    return 0
}
OWNER_LIBRARY

cat >"$TEST_ROOT/lib/routes-router-config.sh" <<'ROUTES_CONFIG_LIBRARY'
#!/bin/sh

broray_routes_config_fetch()
{
    local output last_198 present_198 last_203 present_203

    output="$1"
    last_198="$(
        grep -E \
            '^(no )?ip route 198\.51\.100\.0 255\.255\.255\.0 Proxy0( 1200)?$' \
            "${BRORAY_TEST_CONFIG_LOG:-/dev/null}" 2>/dev/null |
            tail -n 1
    )"
    present_198=true
    last_203=""
    present_203=true
    [ -f "${BRORAY_TEST_203_DELETED:-/nonexistent}" ] && present_203=false

    case "$last_198" in
        "no ip route "*) present_198=false ;;
        "ip route "*) present_198=true ;;
    esac

    jq -n \
        --argjson present_198 "$present_198" \
        --argjson present_203 "$present_203" '
        {
            schemaVersion: 1,
            source: "running-config",
            routes: (
                (
                    if $present_198 then
                        [{
                            network: "198.51.100.0",
                            mask: "255.255.255.0",
                            destination: "198.51.100.0/24",
                            interface: "Proxy0",
                            gateway: "0.0.0.0",
                            metric: 1200,
                            proto: "static"
                        }]
                    else
                        []
                    end
                ) + (
                    if $present_203 then
                        [{
                            network: "203.0.113.0",
                            mask: "255.255.255.0",
                            destination: "203.0.113.0/24",
                            interface: "Proxy0",
                            gateway: "0.0.0.0",
                            metric: 1200,
                            proto: "static"
                        }]
                    else
                        []
                    end
                )
            )
        }
    ' >"$output"
}

broray_routes_config_snapshot()
{
    broray_routes_config_fetch "$1"
}
ROUTES_CONFIG_LIBRARY

cat >"$TEST_ROOT/routes/bundles.json" <<'JSON'
{"schemaVersion":1,"bundles":["telegram"]}
JSON

cat >"$TEST_ROOT/routes/config.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routeComment": "BROray",
  "ownershipPolicy": {
    "deleteOnlyExactManagedMatch": true,
    "deleteExternalRoutes": false,
    "touchOtherInterfaces": false
  }
}
JSON

cat >"$TEST_ROOT/routes/installed/routes.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routes": [
    {
      "key": "ipv4|198.51.100.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "family": "ipv4",
      "network": "198.51.100.0",
      "prefix": 24,
      "mask": "255.255.255.0",
      "interface": "Proxy0",
      "metric": 1200,
      "createdByBROray": true,
      "managed": true,
      "owners": ["telegram"]
    },
    {
      "key": "ipv4|203.0.113.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "family": "ipv4",
      "network": "203.0.113.0",
      "prefix": 24,
      "mask": "255.255.255.0",
      "interface": "Proxy0",
      "metric": 1200,
      "createdByBROray": true,
      "managed": true,
      "owners": ["telegram", "instagram"]
    }
  ]
}
JSON

cat >"$TEST_ROOT/routes/installed/bundles/telegram.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "telegram",
  "installedVersion": {
    "sourceCommit": "abc",
    "contentSha256": "def"
  },
  "managedMetric": 1200,
  "routeKeys": [
    "ipv4|198.51.100.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
    "ipv4|203.0.113.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified"
  ],
  "managedRouteKeys": [
    "ipv4|198.51.100.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
    "ipv4|203.0.113.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified"
  ],
  "externalRouteKeys": [],
  "installedAt": "2026-01-01T00:00:00+0000"
}
JSON

cat >"$TEST_ROOT/routes/state/telegram.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "telegram",
  "status": "installed",
  "availableVersion": {"contentSha256":"def"},
  "downloadedVersion": {"contentSha256":"def"},
  "installedVersion": {"contentSha256":"def"},
  "lastError": null
}
JSON

cat >"$TEST_ROOT/routes/catalog/telegram/export-plan.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "telegram",
  "targetInterface": "Proxy0",
  "managedMetric": 1200,
  "routerApplied": true,
  "configurationSaved": true,
  "routes": []
}
JSON

cat >"$MOCK_BIN/ndmc" <<'MOCK'
#!/bin/sh

printf '%s\n' "$2" >>"$BRORAY_TEST_NDMC_LOG"

case "$2" in
  "no ip route 198.51.100.0 255.255.255.0 Proxy0")
    echo "Io::Netlink error[268239256]: system failed [0xcffd0198], got an error response: file exists."
    exit 122
    ;;
  "system configuration save")
    echo "Core::ConfigurationSaver: Saving configuration."
    exit 0
    ;;
  *)
    echo "unexpected command: $2" >&2
    exit 9
    ;;
esac
MOCK
chmod 755 "$MOCK_BIN/ndmc"

BRORAY_ROOT="$TEST_ROOT"
BRORAY_TEST_NDMC_LOG="$LOG"
BRORAY_TEST_CONFIG_LOG="$LOG"
export BRORAY_ROOT BRORAY_TEST_NDMC_LOG BRORAY_TEST_CONFIG_LOG
PATH="$MOCK_BIN:$PATH"
export PATH

. "$TEST_ROOT/lib/routes-router-delete.sh"
trap broray_routes_delete_cleanup EXIT HUP INT TERM
broray_routes_router_delete_run telegram >/dev/null
trap - EXIT HUP INT TERM
broray_routes_delete_cleanup
trap cleanup EXIT HUP INT TERM

jq -e '
    (.routes | length) == 1 and
    .routes[0].network == "203.0.113.0" and
    .routes[0].owners == ["instagram"]
' "$TEST_ROOT/routes/installed/routes.json" >/dev/null ||
    fail "Глобальный реестр обновлён неверно"

jq -e '
    .installedVersion == null and
    (.routeKeys | length) == 0 and
    (.managedRouteKeys | length) == 0
' "$TEST_ROOT/routes/installed/bundles/telegram.json" >/dev/null ||
    fail "Реестр набора не очищен"

jq -e '
    .status == "downloaded" and
    .installedVersion == null and
    .deleteResult.deleted == 1 and
    .deleteResult.sharedKept == 1 and
    .deleteResult.externalKept == 0
' "$TEST_ROOT/routes/state/telegram.json" >/dev/null ||
    fail "Состояние набора обновлено неверно"

[ "$(grep -F -x -c 'no ip route 198.51.100.0 255.255.255.0 Proxy0' "$LOG")" = "1" ] ||
    fail "Команда удаления по сети и Proxy0 не выполнена ровно один раз"

[ "$(grep -F -c '203.0.113.0' "$LOG" 2>/dev/null || true)" = "0" ] ||
    fail "Общий маршрут был затронут"

[ "$(grep -F -x -c 'system configuration save' "$LOG")" = "1" ] ||
    fail "Конфигурация не сохранена ровно один раз"

jq -e '
    .operation == "delete" and
    .phase == "completed" and
    .current == 1 and .total == 1 and .percent == 100 and
    .running == false and .success == true
' "$TEST_ROOT/routes/operations/telegram.json" >/dev/null ||
    fail "Прогресс успешного удаления не завершён корректно"

# Проверка отката при частичной ошибке удаления.
FAIL_ROOT="$WORK/failure-root"
FAIL_LOG="$WORK/failure-ndmc.log"
BEFORE_SUMS="$WORK/failure-before.sha256"
AFTER_SUMS="$WORK/failure-after.sha256"

mkdir -p \
    "$FAIL_ROOT/lib" \
    "$FAIL_ROOT/routes/installed/bundles" \
    "$FAIL_ROOT/routes/state" \
    "$FAIL_ROOT/routes/catalog/telegram" \
    "$FAIL_ROOT/routes/locks" \
    "$FAIL_ROOT/routes/tmp" \
    "$FAIL_ROOT/routes/transactions"

cp -p "$LIBRARY" "$FAIL_ROOT/lib/routes-router-delete.sh"
cp -p "$PROGRESS_LIBRARY" "$FAIL_ROOT/lib/routes-operation-progress.sh"
cp -p "$TEST_ROOT/lib/interface-owner.sh" \
    "$FAIL_ROOT/lib/interface-owner.sh"
cp -p "$TEST_ROOT/lib/routes-router-config.sh" \
    "$FAIL_ROOT/lib/routes-router-config.sh"
cp -p "$TEST_ROOT/routes/bundles.json" \
    "$FAIL_ROOT/routes/bundles.json"
cp -p "$TEST_ROOT/routes/config.json" "$FAIL_ROOT/routes/config.json"

cat >"$FAIL_ROOT/routes/installed/routes.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routes": [
    {
      "key": "ipv4|198.51.100.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "network": "198.51.100.0",
      "prefix": 24,
      "mask": "255.255.255.0",
      "interface": "Proxy0",
      "metric": 1200,
      "createdByBROray": true,
      "managed": true,
      "owners": ["telegram"]
    },
    {
      "key": "ipv4|203.0.113.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "network": "203.0.113.0",
      "prefix": 24,
      "mask": "255.255.255.0",
      "interface": "Proxy0",
      "metric": 1200,
      "createdByBROray": true,
      "managed": true,
      "owners": ["telegram"]
    }
  ]
}
JSON

cat >"$FAIL_ROOT/routes/installed/bundles/telegram.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "telegram",
  "installedVersion": {"contentSha256":"def"},
  "managedMetric": 1200,
  "routeKeys": [
    "ipv4|198.51.100.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
    "ipv4|203.0.113.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified"
  ],
  "managedRouteKeys": [
    "ipv4|198.51.100.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
    "ipv4|203.0.113.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified"
  ],
  "externalRouteKeys": [],
  "installedAt": "2026-01-01T00:00:00+0000"
}
JSON

cp -p "$TEST_ROOT/routes/state/telegram.json" \
    "$FAIL_ROOT/routes/state/telegram.json"
cp -p "$TEST_ROOT/routes/catalog/telegram/export-plan.json" \
    "$FAIL_ROOT/routes/catalog/telegram/export-plan.json"

sha256sum \
    "$FAIL_ROOT/routes/installed/routes.json" \
    "$FAIL_ROOT/routes/installed/bundles/telegram.json" \
    "$FAIL_ROOT/routes/state/telegram.json" \
    "$FAIL_ROOT/routes/catalog/telegram/export-plan.json" \
    >"$BEFORE_SUMS"

cat >"$MOCK_BIN/ndmc" <<'MOCK'
#!/bin/sh

printf '%s\n' "$2" >>"$BRORAY_TEST_FAIL_LOG"

case "$2" in
  "no ip route 198.51.100.0 255.255.255.0 Proxy0")
    echo "Network::RoutingTable: Deleted static route: 198.51.100.0/24 via Proxy0."
    exit 0
    ;;
  "no ip route 203.0.113.0 255.255.255.0 Proxy0")
    if [ ! -f "$BRORAY_TEST_FAIL_ONCE" ]; then
      : >"$BRORAY_TEST_FAIL_ONCE"
      echo "simulated delete failure" >&2
      exit 9
    fi
    : >"$BRORAY_TEST_203_DELETED"
    echo "Network::RoutingTable: Deleted static route: 203.0.113.0/24 via Proxy0."
    exit 0
    ;;
  "ip route 198.51.100.0 255.255.255.0 Proxy0 1200")
    echo "Network::RoutingTable: Added static route: 198.51.100.0/24 via Proxy0."
    exit 0
    ;;
  "system configuration save")
    echo "Core::ConfigurationSaver: Saving configuration."
    exit 0
    ;;
  *)
    echo "unexpected command: $2" >&2
    exit 11
    ;;
esac
MOCK
chmod 755 "$MOCK_BIN/ndmc"

BRORAY_ROOT="$FAIL_ROOT"
BRORAY_TEST_FAIL_LOG="$FAIL_LOG"
BRORAY_TEST_CONFIG_LOG="$FAIL_LOG"
BRORAY_TEST_FAIL_ONCE="$WORK/delete-fail-once"
BRORAY_TEST_203_DELETED="$WORK/route-203-deleted"
export BRORAY_ROOT BRORAY_TEST_FAIL_LOG BRORAY_TEST_CONFIG_LOG BRORAY_TEST_FAIL_ONCE BRORAY_TEST_203_DELETED

unset BRORAY_ROUTES_DELETE_PROGRESS_LIBRARY
unset BRORAY_ROUTES_PROGRESS_DIR
unset BRORAY_ROUTES_PROGRESS_FILE
unset BRORAY_ROUTES_PROGRESS_COUNTER_FILE

set +e
(
    . "$FAIL_ROOT/lib/routes-router-delete.sh"
    trap broray_routes_delete_cleanup EXIT HUP INT TERM
    broray_routes_router_delete_run telegram >/dev/null 2>&1
)
first_rc=$?
set -e
[ "$first_rc" = 76 ] || fail "Ошибка удаления не сохранила возобновляемое состояние: rc=$first_rc"

jq -e '
    (.routes | length) == 1 and
    .routes[0].network == "203.0.113.0" and
    .routes[0].owners == ["telegram"]
' "$FAIL_ROOT/routes/installed/routes.json" >/dev/null ||
    fail "Успешно удалённый маршрут не исключён из частичного реестра"

jq -e '
    (.routeKeys | length) == 1 and
    (.managedRouteKeys | length) == 1 and
    (.routeKeys[0] | contains("203.0.113.0/24"))
' "$FAIL_ROOT/routes/installed/bundles/telegram.json" >/dev/null ||
    fail "Реестр набора не сохранил только необработанный маршрут"

jq -e '
    .status == "installed" and
    .resumeOperation.operation == "delete" and
    .resumeOperation.resumable == true and
    .resumeOperation.current == 1 and .resumeOperation.total == 2 and
    .resumeOperation.deleted == 1 and
    .lastError.resumable == true
' "$FAIL_ROOT/routes/state/telegram.json" >/dev/null ||
    fail "Состояние удаления не допускает продолжение после ошибки"

jq -e '
    .operation == "delete" and
    .phase == "failed_resumable" and
    .current == 1 and .total == 2 and .percent == 50 and
    .running == false and .success == false and .resumable == true and
    .rolledBack == false and .errorRoute == "203.0.113.0/24"
' "$FAIL_ROOT/routes/operations/telegram.json" >/dev/null ||
    fail "Прогресс неудачного удаления не зафиксировал возможность продолжения"

[ "$(grep -F -x -c 'ip route 198.51.100.0 255.255.255.0 Proxy0 1200' "$FAIL_LOG" 2>/dev/null || true)" = 0 ] ||
    fail "Успешно удалённый маршрут был ошибочно восстановлен"
[ "$(grep -F -x -c 'system configuration save' "$FAIL_LOG" 2>/dev/null || true)" = 1 ] ||
    fail "Частичное удаление не было сохранено ровно один раз"

# Повторный запуск продолжает только с оставшегося маршрута.
unset BRORAY_ROUTES_DELETE_PROGRESS_LIBRARY
unset BRORAY_ROUTES_PROGRESS_DIR
unset BRORAY_ROUTES_PROGRESS_FILE
unset BRORAY_ROUTES_PROGRESS_COUNTER_FILE
(
    . "$FAIL_ROOT/lib/routes-router-delete.sh"
    trap broray_routes_delete_cleanup EXIT HUP INT TERM
    broray_routes_router_delete_run telegram >/dev/null
    trap - EXIT HUP INT TERM
    broray_routes_delete_cleanup
)

jq -e '(.routes | length) == 0' "$FAIL_ROOT/routes/installed/routes.json" >/dev/null ||
    fail "Продолженное удаление не очистило общий реестр"
jq -e '.installedVersion == null and (.routeKeys | length) == 0' \
    "$FAIL_ROOT/routes/installed/bundles/telegram.json" >/dev/null ||
    fail "Продолженное удаление не завершило реестр набора"
jq -e '
    .status == "downloaded" and .installedVersion == null and
    .deleteResult.deleted == 2 and (.resumeOperation == null)
' "$FAIL_ROOT/routes/state/telegram.json" >/dev/null ||
    fail "Итог продолженного удаления не объединён"
jq -e '
    .phase == "completed" and .current == 2 and .total == 2 and
    .percent == 100 and .resumed == true and .resumable == false
' "$FAIL_ROOT/routes/operations/telegram.json" >/dev/null ||
    fail "Прогресс продолженного удаления завершён неверно"

[ "$(grep -F -x -c 'no ip route 198.51.100.0 255.255.255.0 Proxy0' "$FAIL_LOG")" = 1 ] ||
    fail "Первый маршрут был обработан повторно"
[ "$(grep -F -x -c 'no ip route 203.0.113.0 255.255.255.0 Proxy0' "$FAIL_LOG")" = 2 ] ||
    fail "Ошибочный маршрут не был повторён при продолжении"

echo "BROray routes router delete self-test: PASS"
