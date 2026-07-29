#!/bin/sh

set -eu

SOURCE_ROOT="${BRORAY_ROOT:-/opt/broray}"
ROUTES_LIBRARY="$SOURCE_ROOT/lib/routes-api-operation.sh"
SYSTEM_LIBRARY="$SOURCE_ROOT/lib/broray-page.sh"
WORK="${TMPDIR:-/tmp}/broray-global-operation-selftest-$$"
TEST_ROOT="$WORK/root"
GLOBAL_LOCK="$TEST_ROOT/run/global-operation.lock"

cleanup()
{
    rm -rf "$WORK"
}

fail()
{
    echo "BROray global operation lock self-test: FAIL — $*" >&2
    exit 1
}

trap cleanup EXIT HUP INT TERM

[ -r "$ROUTES_LIBRARY" ] || fail "модуль блокировки маршрутов недоступен"
[ -r "$SYSTEM_LIBRARY" ] || fail "backend страницы BROray недоступен"
mkdir -p "$TEST_ROOT/run"

BRORAY_BASE="$TEST_ROOT"
BRORAY_GLOBAL_LOCK="$GLOBAL_LOCK"
export BRORAY_BASE BRORAY_GLOBAL_LOCK
. "$SYSTEM_LIBRARY"

BRORAY_ROOT="$TEST_ROOT"
BRORAY_ROUTES_API_LOCK="$GLOBAL_LOCK"
export BRORAY_ROOT BRORAY_ROUTES_API_LOCK
. "$ROUTES_LIBRARY"

broray_system_global_lock_acquire update ||
    fail "системная операция не получила общую блокировку"

broray_routes_api_lock_read_json |
    jq -e '.active == true and .scope == "system" and .action == "update" and .bundleId == null' >/dev/null ||
    fail "системная блокировка не видна странице маршрутов"

rc=0
broray_routes_api_lock_acquire export telegram || rc=$?
[ "$rc" -eq 2 ] || fail "маршруты не заблокированы системной операцией"
broray_system_global_lock_release
[ ! -d "$GLOBAL_LOCK" ] || fail "системная блокировка не освобождена"

broray_routes_api_lock_acquire export telegram ||
    fail "операция маршрутов не получила общую блокировку"
broray_routes_api_lock_read_json |
    jq -e '.active == true and .scope == "routes" and .action == "export" and .bundleId == "telegram"' >/dev/null ||
    fail "блокировка маршрутов описана неверно"

rc=0
broray_system_global_lock_acquire restore || rc=$?
[ "$rc" -eq 2 ] || fail "системная операция не заблокирована маршрутами"
broray_routes_api_lock_release
[ ! -d "$GLOBAL_LOCK" ] || fail "блокировка маршрутов не освобождена"

mkdir -p "$GLOBAL_LOCK"
printf '%s\n' 999999 >"$GLOBAL_LOCK/pid"
printf '%s\n' system >"$GLOBAL_LOCK/scope"
printf '%s\n' update >"$GLOBAL_LOCK/action"
: >"$GLOBAL_LOCK/bundle"
printf '%s\n' 2026-01-01T00:00:00Z >"$GLOBAL_LOCK/startedAt"
broray_system_global_lock_acquire uninstall ||
    fail "устаревшая общая блокировка не восстановлена"
[ "$(sed -n '1p' "$GLOBAL_LOCK/action")" = uninstall ] ||
    fail "после восстановления записано неверное действие"
broray_system_global_lock_release

echo "BROray global operation lock self-test: PASS"
