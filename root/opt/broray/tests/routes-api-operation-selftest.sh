#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
LIBRARY="$ROOT/lib/routes-api-operation.sh"
WORK="${TMPDIR:-/tmp}/broray-routes-api-lock-selftest-$$"
LOCK="$WORK/routes/locks/api-operation.lock"

cleanup()
{
    rm -rf "$WORK"
}

fail()
{
    echo "BROray routes API operation lock self-test: FAIL — $*" >&2
    exit 1
}

trap cleanup EXIT HUP INT TERM

[ -r "$LIBRARY" ] || fail "модуль блокировки API недоступен"
mkdir -p "$WORK/routes/locks"
BRORAY_ROUTES_API_LOCK="$LOCK"
export BRORAY_ROUTES_API_LOCK
. "$LIBRARY"

broray_routes_api_lock_acquire "preflight:export" telegram ||
    fail "не удалось получить свободную блокировку"

broray_routes_api_lock_read_json |
    jq -e '
        .active == true and .stale == false and .scope == "routes" and
        .action == "preflight:export" and .bundleId == "telegram" and
        (.pid | type) == "number" and .startedAt != null
    ' >/dev/null || fail "активная блокировка описана неверно"

if BRORAY_ROUTES_API_LOCK="$LOCK" /opt/bin/ash -c '
    . "$1"
    rc=0
    broray_routes_api_lock_acquire "download" whatsapp || rc=$?
    [ "$rc" -eq 2 ]
' ash "$LIBRARY"
then
    :
else
    fail "параллельная операция не была отклонена"
fi

broray_routes_api_lock_release
[ ! -d "$LOCK" ] || fail "блокировка не освобождена"

mkdir -p "$LOCK"
printf '%s\n' 999999 >"$LOCK/pid"
printf '%s\n' verify >"$LOCK/action"
printf '%s\n' youtube >"$LOCK/bundle"
printf '%s\n' 2026-01-01T00:00:00+0000 >"$LOCK/startedAt"

broray_routes_api_lock_read_json |
    jq -e '.active == false and .stale == true and .bundleId == "youtube"' >/dev/null ||
    fail "устаревшая блокировка не распознана"

broray_routes_api_lock_acquire "delete" youtube ||
    fail "устаревшая блокировка не восстановлена"
[ "$(sed -n '1p' "$LOCK/pid")" = "$$" ] ||
    fail "после восстановления записан неверный владелец"
broray_routes_api_lock_release

broray_routes_api_lock_read_json |
    jq -e '.active == false and .pid == null and .action == null' >/dev/null ||
    fail "состояние свободной блокировки неверно"

echo "BROray routes API operation lock self-test: PASS"
