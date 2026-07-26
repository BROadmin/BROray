#!/usr/bin/env bash

set -euo pipefail

REPOSITORY_ROOT="$(
    CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." &&
        pwd
)"
RUNTIME_ROOT="$REPOSITORY_ROOT/root/opt/broray"
INIT_ROOT="$REPOSITORY_ROOT/root/opt/etc/init.d"
WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/broray-verify.XXXXXX")"
ASH_BIN="${ASH_BIN:-/bin/dash}"

cleanup()
{
    rm -rf "$WORK_ROOT"
}

fail()
{
    printf 'VERIFY ERROR: %s\n' "$*" >&2
    exit 1
}

trap cleanup EXIT

[ -x "$ASH_BIN" ] ||
    fail "missing POSIX shell: $ASH_BIN"

printf '%s\n' "=== Verification 1/3: source, syntax and privacy ==="

bash -n \
    "$REPOSITORY_ROOT/scripts/build-opkg-release.sh" \
    "$REPOSITORY_ROOT/scripts/test-manual-to-opkg-migration.sh" \
    "$REPOSITORY_ROOT/scripts/test-opkg-rollback.sh" \
    "$REPOSITORY_ROOT/scripts/test-safe-opkg-upgrade.sh" \
    "$REPOSITORY_ROOT/scripts/test-opkg-upgrade.sh" \
    "$REPOSITORY_ROOT/scripts/verify-release.sh"

{
    find "$RUNTIME_ROOT/bin" \
        -type f \
        ! -name 'xray'
    find "$RUNTIME_ROOT/lib" "$RUNTIME_ROOT/tests" \
        -type f \
        -name '*.sh'
    find "$RUNTIME_ROOT/web-new/api" "$RUNTIME_ROOT/web-src/api" \
        -type f \
        \( -name '*.cgi' -o -name '*.sh' \)
    find "$INIT_ROOT" \
        -type f
    printf '%s\n' \
        "$REPOSITORY_ROOT/install.sh" \
        "$REPOSITORY_ROOT/scripts/migrate-manual-to-opkg.sh" \
        "$REPOSITORY_ROOT/scripts/safe-opkg-upgrade.sh" \
        "$REPOSITORY_ROOT/packaging/opkg/opkg.sh" \
        "$REPOSITORY_ROOT/packaging/opkg/preinst" \
        "$REPOSITORY_ROOT/packaging/opkg/postinst" \
        "$REPOSITORY_ROOT/packaging/opkg/prerm" \
        "$REPOSITORY_ROOT/packaging/opkg/postrm"
} | sort -u >"$WORK_ROOT/shell-files"

while IFS= read -r shell_file; do
    "$ASH_BIN" -n "$shell_file" ||
        fail "shell syntax: $shell_file"
done <"$WORK_ROOT/shell-files"

find "$RUNTIME_ROOT" \
    -type f \
    -name '*.json' \
    -print0 |
while IFS= read -r -d '' json_file; do
    jq -e . "$json_file" >/dev/null ||
        fail "invalid JSON: $json_file"
done

[ "$(sed -n '1p' "$RUNTIME_ROOT/config/version")" = "2.1.0" ] ||
    fail "config version is not 2.1.0"
grep -Fq 'BRORAY_VERSION="2.1.0"' "$RUNTIME_ROOT/bin/broray" ||
    fail "CLI version is not 2.1.0"
grep -Fq "VERSION=\"2.1.0\"" "$RUNTIME_ROOT/lib/package-setup.sh" ||
    fail "package setup version is not 2.1.0"

if rg -n --no-messages \
    'netcraze[.]link|zhornov|dimkatver|@[Yy][Aa][.]ru|85[.]9[.]223[.]218' \
    "$RUNTIME_ROOT" \
    "$REPOSITORY_ROOT/packaging" \
    "$REPOSITORY_ROOT/docs" \
    "$REPOSITORY_ROOT/README.md" \
    "$REPOSITORY_ROOT/SECURITY.md" \
    "$REPOSITORY_ROOT/install.sh"
then
    fail "personal host, address or email found"
fi

if rg -n --no-messages \
    '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}' \
    "$RUNTIME_ROOT" \
    -g '!tests/**'
then
    fail "UUID found outside test fixtures"
fi

for forbidden_path in \
    "$RUNTIME_ROOT/config/config.json" \
    "$RUNTIME_ROOT/config/active-server" \
    "$RUNTIME_ROOT/config/interface.json"
do
    [ ! -e "$forbidden_path" ] ||
        fail "runtime configuration found: $forbidden_path"
done

printf '%s\n' "Verification 1 PASS"
printf '%s\n' "=== Verification 2/3: WebUI and self-tests ==="

mkdir -p "$WORK_ROOT/tools"
ln -s "$ASH_BIN" "$WORK_ROOT/tools/ash"

PATH="$WORK_ROOT/tools:$PATH" \
BRORAY_ROOT="$RUNTIME_ROOT" \
BRORAY_WEB_SRC="$RUNTIME_ROOT/web-src" \
BRORAY_WEB_OUT="$WORK_ROOT/web-build" \
    "$ASH_BIN" "$RUNTIME_ROOT/web-src/build-web.sh"

(
    cd "$RUNTIME_ROOT/web-new"
    find . -type f ! -name build.json | sort
) >"$WORK_ROOT/committed-web-files"
(
    cd "$WORK_ROOT/web-build"
    find . -type f ! -name build.json | sort
) >"$WORK_ROOT/built-web-files"

diff -u \
    "$WORK_ROOT/committed-web-files" \
    "$WORK_ROOT/built-web-files"

while IFS= read -r relative_file; do
    cmp \
        "$RUNTIME_ROOT/web-new/$relative_file" \
        "$WORK_ROOT/web-build/$relative_file" ||
        fail "generated WebUI differs: $relative_file"
done <"$WORK_ROOT/committed-web-files"

jq 'del(.builtAt)' \
    "$RUNTIME_ROOT/web-new/build.json" \
    >"$WORK_ROOT/committed-build.json"
jq 'del(.builtAt)' \
    "$WORK_ROOT/web-build/build.json" \
    >"$WORK_ROOT/generated-build.json"
cmp \
    "$WORK_ROOT/committed-build.json" \
    "$WORK_ROOT/generated-build.json" ||
    fail "WebUI build metadata differs"

run_test()
{
    test_name="$1"
    shift
    printf 'RUN %s\n' "$test_name"
    "$ASH_BIN" "$@"
}

run_test routes-download \
    "$RUNTIME_ROOT/tests/routes-download-selftest.sh" \
    "$RUNTIME_ROOT/lib/routes-download.sh" \
    "$RUNTIME_ROOT/lib/routes-source-check.sh"
run_test routes-export-build \
    "$RUNTIME_ROOT/tests/routes-export-build-selftest.sh" \
    "$RUNTIME_ROOT/lib/routes-export-build.sh"
run_test routes-source-check \
    "$RUNTIME_ROOT/tests/routes-source-check-selftest.sh" \
    "$RUNTIME_ROOT/lib/routes-source-check.sh"

for test_file in \
    routes-added-renewed-selftest.sh \
    routes-parallel-metric-selftest.sh \
    routes-r9-export-chain-selftest.sh \
    routes-r9-source-normalization-selftest.sh \
    routes-rci-fetch-selftest.sh \
    routes-router-delete-selftest.sh \
    routes-router-export-selftest.sh \
    routes-router-preflight-selftest.sh \
    routes-static-conflict-selftest.sh
do
    printf 'RUN %s\n' "$test_file"
    BRORAY_ROOT="$RUNTIME_ROOT" \
    BRORAY_ASH_BIN="$ASH_BIN" \
        "$ASH_BIN" "$RUNTIME_ROOT/tests/$test_file"
done

BRORAY_ROOT="$RUNTIME_ROOT" \
BRORAY_INIT_ROOT="$INIT_ROOT" \
BRORAY_ASH_BIN="$ASH_BIN" \
    "$ASH_BIN" "$RUNTIME_ROOT/tests/subscriptions-selftest.sh"

if find "$RUNTIME_ROOT/routes/tmp" \
    -mindepth 1 \
    -print -quit |
    grep -q .
then
    fail "self-tests left files in routes/tmp"
fi

printf '%s\n' "Verification 2 PASS"
printf '%s\n' "=== Verification 3/3: OPKG package and feed ==="

"$REPOSITORY_ROOT/scripts/build-opkg-release.sh"

PACKAGE="$REPOSITORY_ROOT/dist/opkg/aarch64-3.10/broray_2.1.0-2_aarch64-3.10.ipk"
PACKAGES="$REPOSITORY_ROOT/dist/opkg/aarch64-3.10/Packages"
SAFE_UPGRADE="$REPOSITORY_ROOT/dist/broray-safe-upgrade-2.1.0-2.sh"
MANUAL_MIGRATOR="$REPOSITORY_ROOT/dist/broray-manual-to-opkg-2.1.0-2.sh"

[ -s "$PACKAGE" ] ||
    fail "package was not built"
[ -s "$PACKAGES" ] ||
    fail "Packages index was not built"
[ -s "$SAFE_UPGRADE" ] ||
    fail "safe transition script was not built"
[ -s "$MANUAL_MIGRATOR" ] ||
    fail "manual migration script was not built"
[ "$(
    sha256sum "$MANUAL_MIGRATOR" |
        awk '{print $1}'
)" = "6c976a0b3958a8ad1b78584ff54874f283b8218c9c97dd57937ea0b86c214518" ] ||
    fail "manual migration script SHA256 changed"

mkdir -p \
    "$WORK_ROOT/ipk" \
    "$WORK_ROOT/control" \
    "$WORK_ROOT/data"
tar -xzf "$PACKAGE" -C "$WORK_ROOT/ipk"
tar -xzf "$WORK_ROOT/ipk/control.tar.gz" -C "$WORK_ROOT/control"
tar -xzf "$WORK_ROOT/ipk/data.tar.gz" -C "$WORK_ROOT/data"

grep -Fxq 'Version: 2.1.0-2' "$WORK_ROOT/control/control" ||
    fail "wrong package version"
grep -Fxq 'Architecture: aarch64-3.10' "$WORK_ROOT/control/control" ||
    fail "wrong package architecture"
grep -Fq '@users.noreply.github.com>' "$WORK_ROOT/control/control" ||
    fail "neutral maintainer email is missing"

package_sha="$(
    sha256sum "$PACKAGE" |
        awk '{print $1}'
)"
index_sha="$(
    awk '/^SHA256sum:/ {print $2}' "$PACKAGES"
)"
[ "$package_sha" = "$index_sha" ] ||
    fail "Packages SHA256 does not match package"

xray_sha="$(
    sha256sum "$WORK_ROOT/data/opt/broray/bin/xray" |
        awk '{print $1}'
)"
[ "$xray_sha" = "dd3ba298aa32af9442163ee791d54f562bd89aa860fed1d0c47306fb019c1e64" ] ||
    fail "unexpected Xray binary"

[ "$(find "$WORK_ROOT/data/opt/broray/routes/manifests" -type f -name '*.json' | wc -l)" -eq 9 ] ||
    fail "package does not contain nine route manifests"

for forbidden_directory in \
    servers \
    subscriptions \
    deleted-subscriptions \
    backup \
    backups \
    data \
    logs \
    run \
    tmp \
    update \
    config/subscriptions \
    config/disabled-subscription-servers \
    routes/backup \
    routes/catalog \
    routes/installed \
    routes/locks \
    routes/state \
    routes/tmp \
    routes/transactions
do
    if find \
        "$WORK_ROOT/data/opt/broray/$forbidden_directory" \
        -type f \
        -print -quit |
        grep -q .
    then
        fail "package contains runtime files: $forbidden_directory"
    fi
done

[ ! -e "$WORK_ROOT/data/opt/broray/config/config.json" ] ||
    fail "package contains active Xray configuration"
[ ! -d "$WORK_ROOT/data/opt/broray/web-src" ] ||
    fail "package contains WebUI source"
[ ! -d "$WORK_ROOT/data/opt/broray/tests" ] ||
    fail "package contains self-tests"

if rg -n --no-messages \
    'netcraze[.]link|zhornov|dimkatver|@[Yy][Aa][.]ru|85[.]9[.]223[.]218' \
    "$WORK_ROOT/control" \
    "$WORK_ROOT/data" \
    -g '!opt/broray/bin/xray'
then
    fail "personal value found in package"
fi

if rg -n --no-messages -- '--exclude' \
    "$WORK_ROOT/control/preinst"
then
    fail "GNU tar --exclude entered the BusyBox preinst"
fi

for service_name in \
    S23broray-monitor \
    S24broray \
    S25broray-web \
    S27broray-auto-switch \
    S28broray-subscriptions
do
    grep -Fq "$service_name" \
        "$WORK_ROOT/data/opt/broray/lib/package-setup.sh" ||
        fail "package setup does not verify $service_name"
done

OLD_DIST="$WORK_ROOT/old-dist"
BRORAY_DIST_ROOT="$OLD_DIST" \
BRORAY_PACKAGE_REVISION=1 \
XRAY_BINARY="$WORK_ROOT/data/opt/broray/bin/xray" \
    "$REPOSITORY_ROOT/scripts/build-opkg-release.sh"

OLD_PACKAGE="$OLD_DIST/opkg/aarch64-3.10/broray_2.1.0-1_aarch64-3.10.ipk"

"$REPOSITORY_ROOT/scripts/test-opkg-upgrade.sh" \
    "$OLD_PACKAGE" \
    "$PACKAGE"
"$REPOSITORY_ROOT/scripts/test-opkg-rollback.sh" \
    "$OLD_PACKAGE"
"$REPOSITORY_ROOT/scripts/test-safe-opkg-upgrade.sh" \
    "$OLD_PACKAGE" \
    "$PACKAGE"
BRORAY_TEST_PACKAGE="$PACKAGE" \
BRORAY_TEST_PACKAGE_SHA256="$package_sha" \
    "$REPOSITORY_ROOT/scripts/test-manual-to-opkg-migration.sh"

grep -Fq 'TARGET_VERSION="2.1.0-2"' "$SAFE_UPGRADE" ||
    fail "safe transition script targets the wrong package"
grep -Fq 'TARGET_VERSION="2.1.0-2"' "$MANUAL_MIGRATOR" ||
    fail "manual migration script targets the wrong package"

printf '%s\n' "Verification 3 PASS"
printf '%s\n' "ALL VERIFICATIONS PASS"
