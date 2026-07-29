#!/opt/bin/ash

set -u

TEST_ROOT="/tmp/broray-reinstall-selftest-$$"
SOURCE_ROOT="${BRORAY_TEST_SOURCE_ROOT:-/opt/broray}"
BASE="$TEST_ROOT/broray"
MOCK_BIN="$TEST_ROOT/bin"
MOCK_INIT="$TEST_ROOT/init.d"
MOCK_LISTS="$TEST_ROOT/opkg-lists"
MOCK_FEED="$TEST_ROOT/opkg/broray.conf"
MOCK_PACKAGE="$TEST_ROOT/broray_2.1.1-2_all.ipk"
MOCK_STATE="$TEST_ROOT/state"

cleanup()
{
    rm -rf "$TEST_ROOT"
    rm -f /tmp/broray-opkg-existing-backup /tmp/broray-opkg-services-before-upgrade
}
trap cleanup EXIT HUP INT TERM

fail()
{
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_eq()
{
    [ "$1" = "$2" ] || fail "ожидалось '$1', получено '$2'"
}

make_package()
{
    work="$TEST_ROOT/ipk-work"
    rm -rf "$work"
    mkdir -p "$work/control" "$work/data"
    cat >"$work/control/control" <<'CONTROL'
Package: broray
Version: 2.1.1-2
Architecture: all
Description: BROray selftest package
CONTROL
    cat >"$work/control/preinst" <<'SCRIPT'
#!/bin/sh
exit 0
SCRIPT
    cat >"$work/control/postinst" <<'SCRIPT'
#!/bin/sh
exit 0
SCRIPT
    chmod 755 "$work/control/preinst" "$work/control/postinst"
    printf '2.0\n' >"$work/debian-binary"
    (cd "$work/control" && tar -czf "$work/control.tar.gz" .) || return 1
    (cd "$work/data" && tar -czf "$work/data.tar.gz" .) || return 1
    (cd "$work" && tar -czf "$MOCK_PACKAGE" ./debian-binary ./data.tar.gz ./control.tar.gz) || return 1
}

make_mock_commands()
{
    mkdir -p "$MOCK_BIN" "$MOCK_STATE"
    ln -sf "$(command -v busybox)" "$MOCK_BIN/ash"
    ln -sf "$(command -v busybox)" "$MOCK_BIN/tar"

    cat >"$MOCK_BIN/curl" <<'SCRIPT'
#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) out="$2"; shift 2 ;;
        *) shift ;;
    esac
done
[ -n "$out" ] || exit 2
cp "$MOCK_PACKAGE" "$out"
SCRIPT

    cat >"$MOCK_BIN/df" <<'SCRIPT'
#!/bin/sh
printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n'
printf '/dev/mock 100000 1000 99000 1%% /opt\n'
SCRIPT

    cat >"$MOCK_BIN/opkg" <<'SCRIPT'
#!/bin/sh
command_name="${1:-}"
case "$command_name" in
    list-installed)
        [ "${2:-}" = broray ] && printf 'broray - 2.1.1-2\n'
        ;;
    status)
        [ "${2:-}" = broray ] && {
            printf 'Package: broray\nVersion: 2.1.1-2\nArchitecture: all\nStatus: install user installed\n'
        }
        ;;
    update)
        exit 0
        ;;
    install)
        count="$(cat "$MOCK_STATE/install-count" 2>/dev/null || printf 0)"
        count=$((count + 1))
        printf '%s\n' "$count" >"$MOCK_STATE/install-count"
        printf 'package-write-%s\n' "$count" >"$BRORAY_BASE/lib/package-write-marker"
        printf '{"user":"changed-by-package"}\n' >"$BRORAY_BASE/config/user.json"
        if [ "${MOCK_INSTALL_MODE:-success}" = fail-first ] && [ "$count" -eq 1 ]; then
            printf 'corrupted\n' >"$BRORAY_BASE/lib/core-marker"
            exit 1
        fi
        printf 'repaired\n' >"$BRORAY_BASE/lib/core-marker"
        exit 0
        ;;
    print-architecture)
        printf 'arch all 1\n'
        ;;
    *)
        exit 0
        ;;
esac
SCRIPT
    chmod 755 "$MOCK_BIN/curl" "$MOCK_BIN/df" "$MOCK_BIN/opkg"
}

make_service()
{
    file="$1"
    cat >"$file" <<'SCRIPT'
#!/bin/sh
if [ "${1:-}" = restart ] && [ "${MOCK_SERVICE_MUTATE_ONCE:-0}" = 1 ] &&
   [ ! -e "$MOCK_STATE/service-mutated" ]
then
    printf '{"user":"changed-after-restart"}\n' >"$BRORAY_BASE/config/user.json"
    : >"$MOCK_STATE/service-mutated"
fi
case "${1:-}" in
    status|start|stop|restart) exit 0 ;;
    *) exit 0 ;;
esac
SCRIPT
    chmod 755 "$file"
}

prepare_base()
{
    rm -rf "$BASE" "$MOCK_INIT"
    mkdir -p \
        "$BASE/bin" "$BASE/lib" "$BASE/web-new/api/broray" \
        "$BASE/web-new/api/routes" "$BASE/config" "$BASE/data" \
        "$BASE/deleted-subscriptions" "$BASE/subscriptions" "$BASE/servers" \
        "$BASE/routes/dot" "$BASE/routes/tmp" "$BASE/routes/locks" \
        "$BASE/routes/transactions" "$BASE/run/broray" "$BASE/backup" \
        "$MOCK_INIT"

    cp "$SOURCE_ROOT/lib/broray-page.sh" "$BASE/lib/broray-page.sh"
    cat >"$BASE/bin/broray-system" <<'SCRIPT'
#!/bin/sh
exit 0
SCRIPT
    cat >"$BASE/bin/broray" <<'SCRIPT'
#!/bin/sh
exit 0
SCRIPT
    cat >"$BASE/bin/xray" <<'SCRIPT'
#!/bin/sh
[ "${1:-}" = version ] && printf 'Xray 26.7.28 linux/amd64\n'
exit 0
SCRIPT
    cat >"$BASE/bin/broray-routes-dot" <<'SCRIPT'
#!/bin/sh
exit 0
SCRIPT
    cat >"$BASE/lib/xray.sh" <<'SCRIPT'
#!/bin/sh
:
SCRIPT
    cat >"$BASE/lib/server-service.sh" <<'SCRIPT'
#!/bin/sh
:
SCRIPT
    cat >"$BASE/lib/routes-dot.sh" <<'SCRIPT'
#!/bin/sh
:
SCRIPT
    cat >"$BASE/lib/component-lifecycle.sh" <<'SCRIPT'
#!/bin/sh
:
SCRIPT
    cat >"$BASE/lib/web-publish.sh" <<'SCRIPT'
#!/bin/sh
:
SCRIPT
    printf 'original\n' >"$BASE/lib/core-marker"
    printf '2.1.1\n' >"$BASE/config/version"
    printf '{"log":{},"inbounds":[],"outbounds":[]}\n' >"$BASE/config/config.json"
    printf '{"user":"original"}\n' >"$BASE/config/user.json"
    printf 'subscription-secret\n' >"$BASE/subscriptions/demo.txt"
    printf 'server-secret\n' >"$BASE/servers/demo.json"
    printf '{"schemaVersion":1,"servers":[{"address":"1.1.1.1","tlsName":"cloudflare-dns.com"}]}\n' >"$BASE/routes/dot/config.json"
    printf 'temporary\n' >"$BASE/routes/tmp/ignored.tmp"
    ln -s ../config/user.json "$BASE/data/user-link"
    printf '<!doctype html>\n' >"$BASE/web-new/index.html"
    cp "$BASE/web-new/index.html" "$BASE/web-new/home.html"
    cp "$BASE/web-new/index.html" "$BASE/web-new/broray.html"
    cat >"$BASE/web-new/api/auth-common.sh" <<'SCRIPT'
#!/bin/sh
:
SCRIPT
    cat >"$BASE/web-new/api/broray/info.cgi" <<'SCRIPT'
#!/bin/sh
:
SCRIPT
    cat >"$BASE/web-new/api/broray/reinstall.cgi" <<'SCRIPT'
#!/bin/sh
:
SCRIPT
    cat >"$BASE/web-new/api/routes/dot-status.cgi" <<'SCRIPT'
#!/bin/sh
:
SCRIPT

    chmod 755 "$BASE/bin/"* "$BASE/web-new/api/broray/"*.cgi "$BASE/web-new/api/routes/"*.cgi
    for service in S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions; do
        make_service "$MOCK_INIT/$service"
    done
    rm -f "$MOCK_STATE/install-count" "$MOCK_STATE/service-mutated"
}

prepare_feed()
{
    mkdir -p "$(dirname "$MOCK_FEED")" "$MOCK_LISTS"
    printf 'src/gz broray https://example.invalid/feed\n' >"$MOCK_FEED"
    sha="$(sha256sum "$MOCK_PACKAGE" | awk '{print $1}')"
    cat >"$MOCK_LISTS/broray" <<EOF_FEED
Package: broray
Version: 2.1.1-2
Architecture: all
Filename: broray_2.1.1-2_all.ipk
SHA256sum: $sha

EOF_FEED
}

mkdir -p "$TEST_ROOT"
make_package || fail "не удалось создать тестовый IPK"
make_mock_commands
prepare_feed

export BRORAY_BASE="$BASE"
export BRORAY_INIT_ROOT="$MOCK_INIT"
export BRORAY_FEED_FILE="$MOCK_FEED"
export BRORAY_OPKG_LISTS_DIR="$MOCK_LISTS"
export BRORAY_GLOBAL_LOCK="$BASE/run/global-operation.lock"
export MOCK_PACKAGE MOCK_STATE
PATH="$MOCK_BIN:/usr/bin:/bin"
export PATH

prepare_base
. "$BASE/lib/broray-page.sh"
broray_system_require_runtime || fail "runtime не подготовлен"

info="$(broray_system_info_json)" || fail "info завершился ошибкой"
assert_eq true "$(printf '%s' "$info" | jq -r '.reinstallSupported')"
assert_eq 2.1.1-2 "$(printf '%s' "$info" | jq -r '.installedPackageVersion')"

broray_system_start_worker()
{
    jq -nc --arg operation "$1" '{ok:true,operation:$operation}'
}
dispatch="$(broray_system_main reinstall-start)" || fail "reinstall-start не принят диспетчером"
assert_eq reinstall "$(printf '%s' "$dispatch" | jq -r '.operation')"

export MOCK_INSTALL_MODE=success
export MOCK_SERVICE_MUTATE_ONCE=0
broray_system_worker_reinstall reinstall-success || fail "успешная переустановка завершилась ошибкой"
assert_eq success "$(jq -r '.state' "$BRORAY_STATUS")"
assert_eq complete "$(jq -r '.stage' "$BRORAY_STATUS")"
assert_eq '{"user":"original"}' "$(cat "$BASE/config/user.json")"
assert_eq subscription-secret "$(cat "$BASE/subscriptions/demo.txt")"
assert_eq server-secret "$(cat "$BASE/servers/demo.json")"
assert_eq repaired "$(cat "$BASE/lib/core-marker")"
assert_eq package-write-1 "$(cat "$BASE/lib/package-write-marker")"
assert_eq 1 "$(cat "$MOCK_STATE/install-count")"
assert_eq ../config/user.json "$(readlink "$BASE/data/user-link")"
[ ! -e "$BASE/routes/tmp/ignored.tmp" ] || fail "временный файл маршрутов попал в пользовательскую копию"
find "$BASE/backup" -name 'system-before-reinstall-*.tar.gz' -type f | grep -q . || fail "нет полного снимка"
find "$BASE/backup" -name 'user-before-reinstall-*.tar.gz' -type f | grep -q . || fail "нет пользовательской копии"

prepare_base
broray_system_require_runtime || fail "runtime после сброса не подготовлен"
export MOCK_INSTALL_MODE=fail-first
export MOCK_SERVICE_MUTATE_ONCE=0
if broray_system_worker_reinstall reinstall-failure; then
    fail "ошибка OPKG должна завершить операцию ошибкой"
fi
assert_eq error "$(jq -r '.state' "$BRORAY_STATUS")"
assert_eq restored "$(jq -r '.stage' "$BRORAY_STATUS")"
assert_eq '{"user":"original"}' "$(cat "$BASE/config/user.json")"
assert_eq original "$(cat "$BASE/lib/core-marker")"
[ ! -e "$BASE/lib/package-write-marker" ] || fail "полный возврат оставил новый файл"
assert_eq 2 "$(cat "$MOCK_STATE/install-count")"

prepare_base
broray_system_require_runtime || fail "runtime перед проверкой целостности не подготовлен"
export MOCK_INSTALL_MODE=success
export MOCK_SERVICE_MUTATE_ONCE=1
if broray_system_worker_reinstall reinstall-integrity-failure; then
    fail "изменение пользовательских данных после перезапуска должно вызвать возврат"
fi
assert_eq error "$(jq -r '.state' "$BRORAY_STATUS")"
assert_eq restored "$(jq -r '.stage' "$BRORAY_STATUS")"
assert_eq '{"user":"original"}' "$(cat "$BASE/config/user.json")"
assert_eq original "$(cat "$BASE/lib/core-marker")"
[ ! -e "$BASE/lib/package-write-marker" ] || fail "возврат после проверки оставил новый файл"
assert_eq 2 "$(cat "$MOCK_STATE/install-count")"

prepare_base
broray_system_require_runtime || fail "runtime перед проверкой индекса не подготовлен"
: >"$MOCK_LISTS/broray"
export MOCK_INSTALL_MODE=success
export MOCK_SERVICE_MUTATE_ONCE=0
if broray_system_worker_reinstall reinstall-no-package; then
    fail "переустановка без точного пакета не должна запускаться"
fi
assert_eq error "$(jq -r '.state' "$BRORAY_STATUS")"
assert_eq download "$(jq -r '.stage' "$BRORAY_STATUS")"
[ ! -e "$MOCK_STATE/install-count" ] || fail "OPKG install запущен без проверенного пакета"
assert_eq original "$(cat "$BASE/lib/core-marker")"

printf 'PASS: восстановительная переустановка текущей версии\n'
