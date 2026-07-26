#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 1 ]; then
    printf 'Usage: %s OLD_IPK\n' "$0" >&2
    exit 2
fi

OLD_IPK="$1"
REPOSITORY_ROOT="$(
    CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." &&
        pwd
)"
WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/broray-rollback-test.XXXXXX")"
TEST_ROOT="$WORK_ROOT/root"
INIT_ROOT="$WORK_ROOT/init.d"
MOCK_BIN="$WORK_ROOT/bin"
STATE_ROOT="$WORK_ROOT/service-state"
SNAPSHOT_ROOT="$WORK_ROOT/snapshot"
BACKUP="$WORK_ROOT/pre-upgrade.tar.gz"
ROLLBACK_IPK="$WORK_ROOT/rollback.ipk"
VERSION_FILE="$WORK_ROOT/installed-version"
OPKG_LOG="$WORK_ROOT/opkg.log"
SERVICE_LOG="$WORK_ROOT/services.log"

cleanup()
{
    rm -rf "$WORK_ROOT"
}

fail()
{
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

trap cleanup EXIT

[ -s "$OLD_IPK" ] || fail "old package not found: $OLD_IPK"

mkdir -p \
    "$TEST_ROOT" \
    "$INIT_ROOT" \
    "$MOCK_BIN" \
    "$STATE_ROOT" \
    "$SNAPSHOT_ROOT"

printf '%s\n' '2.1.0-2' >"$VERSION_FILE"
printf '%s\n' 'new-state' >"$TEST_ROOT/state-marker"
printf '%s\n' 'old-state' >"$SNAPSHOT_ROOT/state-marker"
tar -czf "$BACKUP" -C "$SNAPSHOT_ROOT" .

cat >"$MOCK_BIN/opkg" <<'MOCK'
#!/bin/sh

case "${1:-}" in
    list-installed)
        printf 'broray - %s\n' "$(sed -n '1p' "$FAKE_OPKG_VERSION_FILE")"
        ;;
    status)
        printf 'Package: broray\n'
        printf 'Version: %s\n' "$(sed -n '1p' "$FAKE_OPKG_VERSION_FILE")"
        printf 'Architecture: aarch64-3.10\n'
        ;;
    install)
        printf '%s\n' "$*" >>"$FAKE_OPKG_LOG"
        [ "${2:-}" = "--force-downgrade" ] || exit 9
        printf '%s\n' "$FAKE_OPKG_OLD_VERSION" >"$FAKE_OPKG_VERSION_FILE"
        ;;
    *)
        exit 8
        ;;
esac
MOCK
chmod 755 "$MOCK_BIN/opkg"

cat >"$WORK_ROOT/service" <<'SERVICE'
#!/bin/sh

name="${0##*/}"

case "${1:-}" in
    restart)
        printf '%s\n' "$name" >"$FAKE_SERVICE_STATE/$name"
        printf 'restart %s\n' "$name" >>"$FAKE_SERVICE_LOG"
        ;;
    status)
        [ -f "$FAKE_SERVICE_STATE/$name" ] || exit 1
        printf 'status %s\n' "$name" >>"$FAKE_SERVICE_LOG"
        ;;
    *)
        exit 2
        ;;
esac
SERVICE
chmod 755 "$WORK_ROOT/service"

for service_name in \
    S23broray-monitor \
    S24broray \
    S25broray-web \
    S27broray-auto-switch \
    S28broray-subscriptions
do
    cp "$WORK_ROOT/service" "$INIT_ROOT/$service_name"
done

export PATH="$MOCK_BIN:$PATH"
export FAKE_OPKG_VERSION_FILE="$VERSION_FILE"
export FAKE_OPKG_LOG="$OPKG_LOG"
export FAKE_OPKG_OLD_VERSION="2.1.0-1"
export FAKE_SERVICE_STATE="$STATE_ROOT"
export FAKE_SERVICE_LOG="$SERVICE_LOG"
export BRORAY_BASE="$TEST_ROOT"
export BRORAY_INIT_ROOT="$INIT_ROOT"
export BRORAY_LOG="$WORK_ROOT/rollback.log"

# shellcheck source=/dev/null
. "$REPOSITORY_ROOT/root/opt/broray/lib/broray-page.sh"
BRORAY_LOG="$WORK_ROOT/rollback.log"

broray_system_make_rollback_ipk "$OLD_IPK" "$ROLLBACK_IPK" ||
    fail "rollback package repack failed"

[ "$(broray_system_ipk_control_value "$ROLLBACK_IPK" Version)" = "2.1.0-1" ] ||
    fail "rollback package version changed"

broray_system_health_check()
{
    [ "$(sed -n '1p' "$BRORAY_BASE/state-marker")" = "old-state" ] &&
        broray_system_services_health_check
}

broray_system_rollback_package \
    "$ROLLBACK_IPK" \
    "2.1.0-1" \
    "$BACKUP" ||
    fail "OPKG rollback failed"

[ "$(sed -n '1p' "$VERSION_FILE")" = "2.1.0-1" ] ||
    fail "OPKG database version was not downgraded"
[ "$(sed -n '1p' "$TEST_ROOT/state-marker")" = "old-state" ] ||
    fail "pre-upgrade files were not restored"
grep -Fxq 'install --force-downgrade '"$ROLLBACK_IPK" "$OPKG_LOG" ||
    fail "OPKG was not called with --force-downgrade"

for service_name in \
    S23broray-monitor \
    S24broray \
    S25broray-web \
    S27broray-auto-switch \
    S28broray-subscriptions
do
    [ "$(grep -Fxc "restart $service_name" "$SERVICE_LOG")" -eq 1 ] ||
        fail "$service_name was not restarted exactly once"
    [ "$(grep -Fxc "status $service_name" "$SERVICE_LOG")" -eq 2 ] ||
        fail "$service_name was not checked exactly twice"
done

printf 'BROray OPKG downgrade rollback test: PASS\n'
