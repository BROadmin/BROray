#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 2 ]; then
    printf 'Usage: %s OLD_IPK NEW_IPK\n' "$0" >&2
    exit 2
fi

OLD_IPK="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
NEW_IPK="$(cd "$(dirname "$2")" && pwd)/$(basename "$2")"
REPOSITORY_ROOT="$(
    CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." &&
        pwd
)"
WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/broray-safe-upgrade-test.XXXXXX")"
OPT_ROOT="$WORK_ROOT/opt"
MOCK_BIN="$WORK_ROOT/bin"
VERSION_FILE="$WORK_ROOT/installed-version"
OPKG_LOG="$WORK_ROOT/opkg.log"
SERVICE_LOG="$WORK_ROOT/services.log"
OUTPUT="$WORK_ROOT/output"

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

mkdir -p \
    "$MOCK_BIN" \
    "$OPT_ROOT/broray/backups" \
    "$OPT_ROOT/broray/run" \
    "$OPT_ROOT/etc/init.d" \
    "$OPT_ROOT/var/opkg-lists" \
    "$WORK_ROOT/service-state"

printf '%s\n' '2.1.0-1' >"$VERSION_FILE"
printf '%s\n' 'pre-upgrade-state' >"$OPT_ROOT/broray/data-marker"
printf '%s\n' '192.0.2.1' >"$OPT_ROOT/broray/run/lan-ip"

NEW_SHA="$(sha256sum "$NEW_IPK" | awk '{print $1}')"
NEW_NAME="$(basename "$NEW_IPK")"

cat >"$OPT_ROOT/var/opkg-lists/broray" <<EOF
Package: broray
Version: 2.1.0-2
Architecture: aarch64-3.10
Filename: $NEW_NAME
SHA256sum: $NEW_SHA

EOF

cat >"$MOCK_BIN/uname" <<'MOCK'
#!/bin/sh
printf '%s\n' aarch64
MOCK

cat >"$MOCK_BIN/ash" <<'MOCK'
#!/bin/sh
exec /bin/sh "$@"
MOCK

cat >"$MOCK_BIN/curl" <<'MOCK'
#!/bin/sh

output=""
url=""
need_output=false

for argument in "$@"; do
    if [ "$need_output" = true ]; then
        output="$argument"
        need_output=false
        continue
    fi

    case "$argument" in
        -o)
            need_output=true
            ;;
        http://*|https://*)
            url="$argument"
            ;;
    esac
done

case "$url" in
    http://*)
        exit 0
        ;;
    *broray_2.1.0-2_aarch64-3.10.ipk)
        cp "$FAKE_NEW_IPK" "$output"
        ;;
    *broray_2.1.0-1_aarch64-3.10.ipk)
        cp "$FAKE_OLD_IPK" "$output"
        ;;
    *)
        printf 'unexpected URL: %s\n' "$url" >&2
        exit 9
        ;;
esac
MOCK

cat >"$MOCK_BIN/opkg" <<'MOCK'
#!/bin/sh

case "${1:-}" in
    list-installed)
        printf 'broray - %s\n' "$(sed -n '1p' "$FAKE_VERSION_FILE")"
        ;;
    update)
        exit 0
        ;;
    install)
        printf '%s\n' "$*" >>"$FAKE_OPKG_LOG"

        if [ "${2:-}" = "--force-downgrade" ]; then
            printf '%s\n' '2.1.0-1' >"$FAKE_VERSION_FILE"
            exit 0
        fi

        snapshot="/tmp/broray-safe-upgrade-fixture-$$.tar.gz"
        tar -czf "$snapshot" \
            -C "$FAKE_OPT_ROOT" \
            broray/data-marker \
            broray/run/lan-ip ||
            exit 8
        mv "$snapshot" \
            "$FAKE_OPT_ROOT/broray/backups/opkg-before-2.1.0-2-test.tar.gz" ||
            exit 8

        printf '%s\n' '2.1.0-2' >"$FAKE_VERSION_FILE"
        printf '%s\n' 'failed-new-state' \
            >"$FAKE_OPT_ROOT/broray/data-marker"
        exit 7
        ;;
    *)
        exit 6
        ;;
esac
MOCK

cat >"$WORK_ROOT/service" <<'SERVICE'
#!/bin/sh

name="${0##*/}"

case "${1:-}" in
    restart)
        : >"$FAKE_SERVICE_STATE/$name"
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

chmod 755 \
    "$MOCK_BIN/uname" \
    "$MOCK_BIN/ash" \
    "$MOCK_BIN/curl" \
    "$MOCK_BIN/opkg" \
    "$WORK_ROOT/service"

for service_name in \
    S23broray-monitor \
    S24broray \
    S25broray-web \
    S27broray-auto-switch \
    S28broray-subscriptions
do
    cp "$WORK_ROOT/service" "$OPT_ROOT/etc/init.d/$service_name"
done

export PATH="$MOCK_BIN:$PATH"
export FAKE_OLD_IPK="$OLD_IPK"
export FAKE_NEW_IPK="$NEW_IPK"
export FAKE_VERSION_FILE="$VERSION_FILE"
export FAKE_OPKG_LOG="$OPKG_LOG"
export FAKE_OPT_ROOT="$OPT_ROOT"
export FAKE_SERVICE_STATE="$WORK_ROOT/service-state"
export FAKE_SERVICE_LOG="$SERVICE_LOG"
export BRORAY_OPT_ROOT="$OPT_ROOT"
export BRORAY_OPKG_LIST_ROOT="$OPT_ROOT/var/opkg-lists"
export BRORAY_FEED_URL="https://fixture.invalid/opkg"

result=0
/bin/sh "$REPOSITORY_ROOT/scripts/safe-opkg-upgrade.sh" \
    >"$OUTPUT" 2>&1 ||
    result=$?

[ "$result" -eq 1 ] ||
    fail "simulated failed upgrade returned $result instead of 1"
[ "$(sed -n '1p' "$VERSION_FILE")" = "2.1.0-1" ] ||
    fail "safe updater did not restore the OPKG version"
[ "$(sed -n '1p' "$OPT_ROOT/broray/data-marker")" = "pre-upgrade-state" ] ||
    fail "safe updater did not restore the pre-upgrade snapshot"
grep -Fq 'Предыдущий пакет 2.1.0-1 и данные восстановлены: OK' "$OUTPUT" ||
    fail "safe updater did not report a successful rollback"
grep -Fq 'Терминал остаётся открытым.' "$OUTPUT" ||
    fail "safe updater did not confirm the terminal remains open"

for service_name in \
    S23broray-monitor \
    S24broray \
    S25broray-web \
    S27broray-auto-switch \
    S28broray-subscriptions
do
    [ "$(grep -Fxc "restart $service_name" "$SERVICE_LOG")" -eq 1 ] ||
        fail "$service_name was not restarted exactly once"
    [ "$(grep -Fxc "status $service_name" "$SERVICE_LOG")" -eq 1 ] ||
        fail "$service_name was not checked exactly once"
done

printf 'BROray safe transition failure/rollback test: PASS\n'
