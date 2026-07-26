#!/usr/bin/env bash

set -euo pipefail

REPOSITORY_ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_ROOT="$REPOSITORY_ROOT/root/opt/broray"
INIT_ROOT="$REPOSITORY_ROOT/root/opt/etc/init.d"
WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/broray-2.1.1-verify.XXXXXX")"
ASH_BIN="${ASH_BIN:-$(command -v busybox)}"

cleanup() { rm -rf "$WORK_ROOT"; }
fail() { printf 'VERIFY 2.1.1 ERROR: %s\n' "$*" >&2; exit 1; }
trap cleanup EXIT HUP INT TERM

printf '%s\n' '=== BROray 2.1.1 verification 1/3: source and privacy ==='

bash -n \
    "$REPOSITORY_ROOT/scripts/build-opkg-release-2.1.1.sh" \
    "$REPOSITORY_ROOT/scripts/safe-opkg-upgrade-2.1.1.sh" \
    "$REPOSITORY_ROOT/scripts/verify-release-2.1.1.sh"

{
    find "$RUNTIME_ROOT/bin" -type f ! -name xray
    find "$RUNTIME_ROOT/lib" "$RUNTIME_ROOT/tests" -type f -name '*.sh'
    find "$RUNTIME_ROOT/web-new/api" "$RUNTIME_ROOT/web-src/api" \
        -type f \( -name '*.cgi' -o -name '*.sh' \)
    find "$INIT_ROOT" -type f
} | sort -u > "$WORK_ROOT/shell-files"

while IFS= read -r shell_file; do
    busybox ash -n "$shell_file" || fail "shell syntax: $shell_file"
done < "$WORK_ROOT/shell-files"

find "$RUNTIME_ROOT" -type f -name '*.json' -print0 |
while IFS= read -r -d '' json_file; do
    jq -e . "$json_file" >/dev/null || fail "invalid JSON: $json_file"
done

[ "$(sed -n '1p' "$RUNTIME_ROOT/config/version")" = '2.1.1' ] ||
    fail 'config version is not 2.1.1'
grep -Fq 'BRORAY_VERSION="2.1.1"' "$RUNTIME_ROOT/bin/broray" ||
    fail 'CLI version is not 2.1.1'
grep -Fq 'VERSION="2.1.1"' "$RUNTIME_ROOT/lib/package-setup.sh" ||
    fail 'package setup version is not 2.1.1'
grep -Fq 'server.max-request-size = 5120' "$RUNTIME_ROOT/lib/package-setup.sh" ||
    fail 'lighttpd upload limit is not 5120 KiB'

if rg -n --no-messages \
    'A-Keenetic_Combined_Dedup_Only_MetaAdded|user-7be618|85[.]9[.]223[.]218|netcraze[.]link|zhornov|dimkatver|routes-user-bat-before' \
    "$RUNTIME_ROOT" "$REPOSITORY_ROOT/docs" "$REPOSITORY_ROOT/site"; then
    fail 'local or personal marker found'
fi

for forbidden_file in \
    "$RUNTIME_ROOT/config/config.json" \
    "$RUNTIME_ROOT/config/active-server" \
    "$RUNTIME_ROOT/config/interface.json" \
    "$RUNTIME_ROOT/bin/xray"
do
    [ ! -e "$forbidden_file" ] || fail "runtime file found: $forbidden_file"
done

for runtime_dir in \
    servers subscriptions deleted-subscriptions backup backups data logs run tmp update \
    config/subscriptions config/disabled-subscription-servers \
    routes/backup routes/catalog routes/installed routes/locks routes/state routes/tmp routes/transactions
do
    if [ -d "$RUNTIME_ROOT/$runtime_dir" ] &&
       find "$RUNTIME_ROOT/$runtime_dir" -type f -print -quit | grep -q .
    then
        fail "runtime data found: $runtime_dir"
    fi
done

printf '%s\n' 'Verification 1 PASS'
printf '%s\n' '=== BROray 2.1.1 verification 2/3: WebUI and BAT importer ==='

BRORAY_ROOT="$RUNTIME_ROOT" \
BRORAY_WEB_SRC="$RUNTIME_ROOT/web-src" \
BRORAY_WEB_OUT="$WORK_ROOT/web-build" \
    busybox ash "$RUNTIME_ROOT/web-src/build-web.sh" >/dev/null

(
    cd "$RUNTIME_ROOT/web-new"
    find . -type f ! -name build.json | sort
) > "$WORK_ROOT/committed-web-files"
(
    cd "$WORK_ROOT/web-build"
    find . -type f ! -name build.json | sort
) > "$WORK_ROOT/built-web-files"
diff -u "$WORK_ROOT/committed-web-files" "$WORK_ROOT/built-web-files"
while IFS= read -r relative_file; do
    cmp "$RUNTIME_ROOT/web-new/$relative_file" "$WORK_ROOT/web-build/$relative_file" ||
        fail "generated WebUI differs: $relative_file"
done < "$WORK_ROOT/committed-web-files"
jq 'del(.builtAt)' "$RUNTIME_ROOT/web-new/build.json" > "$WORK_ROOT/a.json"
jq 'del(.builtAt)' "$WORK_ROOT/web-build/build.json" > "$WORK_ROOT/b.json"
cmp "$WORK_ROOT/a.json" "$WORK_ROOT/b.json" || fail 'WebUI build metadata differs'

BRORAY_SELFTEST_ROOT="$RUNTIME_ROOT" \
BRORAY_ASH_BIN=/bin/dash \
    /bin/dash "$RUNTIME_ROOT/tests/routes-user-import-selftest.sh"

printf '%s\n' 'Verification 2 PASS'
printf '%s\n' '=== BROray 2.1.1 verification 3/3: OPKG package ==='

"$REPOSITORY_ROOT/scripts/build-opkg-release-2.1.1.sh"
PACKAGE="$REPOSITORY_ROOT/dist/opkg/aarch64-3.10/broray_2.1.1-1_aarch64-3.10.ipk"
PACKAGES="$REPOSITORY_ROOT/dist/opkg/aarch64-3.10/Packages"
SAFE_UPGRADE="$REPOSITORY_ROOT/dist/broray-safe-upgrade-2.1.1-1.sh"

[ -s "$PACKAGE" ] || fail 'package was not built'
[ -s "$PACKAGES" ] || fail 'Packages index was not built'
[ -s "$SAFE_UPGRADE" ] || fail 'safe upgrade script was not built'

mkdir -p "$WORK_ROOT/ipk" "$WORK_ROOT/control" "$WORK_ROOT/data"
tar -xzf "$PACKAGE" -C "$WORK_ROOT/ipk"
tar -xzf "$WORK_ROOT/ipk/control.tar.gz" -C "$WORK_ROOT/control"
tar -xzf "$WORK_ROOT/ipk/data.tar.gz" -C "$WORK_ROOT/data"
grep -Fxq 'Version: 2.1.1-1' "$WORK_ROOT/control/control" || fail 'wrong package version'
grep -Fxq 'Architecture: aarch64-3.10' "$WORK_ROOT/control/control" || fail 'wrong package architecture'

package_sha="$(sha256sum "$PACKAGE" | awk '{print $1}')"
index_sha="$(awk '/^SHA256sum:/ {print $2}' "$PACKAGES")"
[ "$package_sha" = "$index_sha" ] || fail 'Packages SHA256 does not match package'
[ "$(sha256sum "$WORK_ROOT/data/opt/broray/bin/xray" | awk '{print $1}')" = \
  'dd3ba298aa32af9442163ee791d54f562bd89aa860fed1d0c47306fb019c1e64' ] ||
    fail 'unexpected Xray binary'
[ -x "$WORK_ROOT/data/opt/broray/bin/broray-routes-user" ] ||
    fail 'user route CLI missing from package'
[ -x "$WORK_ROOT/data/opt/broray/web-new/api/routes/custom-preview.cgi" ] ||
    fail 'custom route API missing from package'
grep -Fq 'TARGET_VERSION="2.1.1-1"' "$SAFE_UPGRADE" ||
    fail 'safe upgrade targets the wrong package'

for runtime_dir in \
    servers subscriptions deleted-subscriptions backup backups data logs run tmp update \
    config/subscriptions config/disabled-subscription-servers \
    routes/backup routes/catalog routes/installed routes/locks routes/state routes/tmp routes/transactions
do
    if find "$WORK_ROOT/data/opt/broray/$runtime_dir" -type f -print -quit 2>/dev/null | grep -q .; then
        fail "package contains runtime files: $runtime_dir"
    fi
done

printf '%s\n' 'Verification 3 PASS'
printf '%s\n' 'ALL BROray 2.1.1 VERIFICATIONS PASS'
