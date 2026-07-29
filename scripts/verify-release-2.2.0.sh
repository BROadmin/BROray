#!/usr/bin/env bash
set -euo pipefail

REPOSITORY_ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_ROOT="$REPOSITORY_ROOT/root/opt/broray"
INIT_ROOT="$REPOSITORY_ROOT/root/opt/etc/init.d"
WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/broray-2.2.0-verify.XXXXXX")"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-1785283200}"
ALLOW_PLACEHOLDERS="${BRORAY_ALLOW_PLACEHOLDERS:-0}"

cleanup() { rm -rf "$WORK_ROOT"; }
fail() { printf 'VERIFY 2.2.0 ERROR: %s\n' "$*" >&2; exit 1; }
trap cleanup EXIT HUP INT TERM

printf '%s\n' '=== BROray 2.2.0 verification 1/5: source, syntax and privacy ==='
bash -n \
    "$REPOSITORY_ROOT/scripts/build-opkg-release-2.2.0.sh" \
    "$REPOSITORY_ROOT/scripts/verify-release-2.2.0.sh" \
    "$REPOSITORY_ROOT/scripts/test-opkg-preinst-2.2.0.sh"

{
    find "$RUNTIME_ROOT/bin" -type f ! -name xray
    find "$RUNTIME_ROOT/lib" "$RUNTIME_ROOT/tests" -type f -name '*.sh'
    find "$RUNTIME_ROOT/web-new/api" "$RUNTIME_ROOT/web-src/api" -type f \( -name '*.cgi' -o -name '*.sh' \)
    find "$INIT_ROOT" -type f
    printf '%s\n' \
        "$REPOSITORY_ROOT/scripts/safe-opkg-upgrade-2.2.0-1.sh" \
        "$REPOSITORY_ROOT/scripts/install-opkg-release-2.2.0-1.sh" \
        "$REPOSITORY_ROOT/packaging/opkg/preinst" \
        "$REPOSITORY_ROOT/packaging/opkg/postinst"
} | sort -u >"$WORK_ROOT/shell-files"
while IFS= read -r file; do busybox ash -n "$file" || fail "ash syntax: $file"; done <"$WORK_ROOT/shell-files"

find "$RUNTIME_ROOT" -type f -name '*.js' -print0 |
while IFS= read -r -d '' file; do node --check "$file" >/dev/null || fail "JavaScript syntax: $file"; done
find "$RUNTIME_ROOT" -type f -name '*.json' -print0 |
while IFS= read -r -d '' file; do jq -e . "$file" >/dev/null || fail "invalid JSON: $file"; done

[ "$(sed -n '1p' "$RUNTIME_ROOT/config/version")" = '2.2.0' ] || fail 'config version'
grep -Fq 'BRORAY_VERSION="2.2.0"' "$RUNTIME_ROOT/bin/broray" || fail 'CLI version'
grep -Fq 'VERSION="2.2.0"' "$RUNTIME_ROOT/lib/package-setup.sh" || fail 'package setup version'
[ "$(sed -n '1p' "$RUNTIME_ROOT/share/routes/user-import-version")" = '2.2.0' ] || fail 'BAT importer version'
[ "$(sed -n '1p' "$RUNTIME_ROOT/web-src/BUILD")" = '20260729-release-2.2.0-r1' ] || fail 'WebUI build id'

if grep -R -n -E --binary-files=without-match \
    '85[.]9[.]223[.]218|netcraze[.]link|zhornov|dimkatver|user-d98794' \
    "$RUNTIME_ROOT" "$REPOSITORY_ROOT/docs" "$REPOSITORY_ROOT/site"; then
    fail 'local or personal marker found'
fi
printf '%s\n' 'Verification 1 PASS'

printf '%s\n' '=== BROray 2.2.0 verification 2/5: canonical WebUI and functional tests ==='
BRORAY_ROOT="$RUNTIME_ROOT" BRORAY_WEB_SRC="$RUNTIME_ROOT/web-src" BRORAY_WEB_OUT="$WORK_ROOT/web-build" \
    busybox ash "$RUNTIME_ROOT/web-src/build-web.sh" >/dev/null
(
    cd "$RUNTIME_ROOT/web-new"
    find . -type f ! -name build.json | sort
) >"$WORK_ROOT/committed-web-files"
(
    cd "$WORK_ROOT/web-build"
    find . -type f ! -name build.json | sort
) >"$WORK_ROOT/built-web-files"
diff -u "$WORK_ROOT/committed-web-files" "$WORK_ROOT/built-web-files"
while IFS= read -r relative; do
    cmp "$RUNTIME_ROOT/web-new/$relative" "$WORK_ROOT/web-build/$relative" || fail "generated WebUI differs: $relative"
done <"$WORK_ROOT/committed-web-files"
jq 'del(.builtAt,.source)' "$RUNTIME_ROOT/web-new/build.json" >"$WORK_ROOT/a.json"
jq 'del(.builtAt,.source)' "$WORK_ROOT/web-build/build.json" >"$WORK_ROOT/b.json"
cmp "$WORK_ROOT/a.json" "$WORK_ROOT/b.json" || fail 'WebUI build metadata'

mkdir -p "$WORK_ROOT/tools" /opt/bin
ln -sfn /usr/bin/busybox "$WORK_ROOT/tools/ash"
ln -sfn /usr/bin/busybox /opt/bin/ash
sudo_link_created=false
if [ ! -e /opt/broray ]; then
    ln -s "$RUNTIME_ROOT" /opt/broray
    sudo_link_created=true
fi
cleanup_runtime_links() {
    [ "$sudo_link_created" = false ] || rm -f /opt/broray
    rm -f /opt/bin/ash
}
trap 'cleanup_runtime_links; cleanup' EXIT HUP INT TERM

for file in "$RUNTIME_ROOT"/tests/routes-*selftest.sh; do
    BRORAY_ROOT="$RUNTIME_ROOT" BRORAY_ASH_BIN=/opt/bin/ash /opt/bin/ash "$file"
done
node "$RUNTIME_ROOT/tests/routes-ui-action-selftest.js" "$RUNTIME_ROOT/web-new/assets/js/routes.js"
node "$RUNTIME_ROOT/tests/routes-dot-ui-selftest.js" "$RUNTIME_ROOT/web-new/assets/js/routes-dot.js"
BRORAY_ROOT="$RUNTIME_ROOT" BRORAY_ASH_BIN=/opt/bin/ash /opt/bin/ash "$RUNTIME_ROOT/tests/subscriptions-selftest.sh"
BRORAY_TEST_SOURCE_ROOT="$RUNTIME_ROOT" /usr/bin/busybox ash "$RUNTIME_ROOT/tests/broray-reinstall-selftest.sh"
node "$RUNTIME_ROOT/tests/broray-reinstall-ui-selftest.js" "$RUNTIME_ROOT"
BRORAY_TEST_SOURCE_ROOT="$RUNTIME_ROOT" /usr/bin/busybox ash "$RUNTIME_ROOT/tests/broray-cleanup-selftest.sh"
node "$RUNTIME_ROOT/tests/broray-cleanup-ui-selftest.js" "$RUNTIME_ROOT"
node "$RUNTIME_ROOT/tests/broray-support-ui-selftest.js" "$REPOSITORY_ROOT"
bash "$REPOSITORY_ROOT/scripts/test-opkg-preinst-2.2.0.sh"
printf '%s\n' 'Verification 2 PASS'

printf '%s\n' '=== BROray 2.2.0 verification 3/5: reproducible OPKG build ==='
rm -rf "$REPOSITORY_ROOT/dist"
SOURCE_DATE_EPOCH="$SOURCE_DATE_EPOCH" "$REPOSITORY_ROOT/scripts/build-opkg-release-2.1.1-2.sh"
OLD_PACKAGE="$REPOSITORY_ROOT/dist/opkg/aarch64-3.10/broray_2.1.1-2_aarch64-3.10.ipk"
[ -s "$OLD_PACKAGE" ] || fail 'old comparison package was not built'
mkdir -p "$WORK_ROOT/old-archive" "$WORK_ROOT/old-data"
tar -xzf "$OLD_PACKAGE" -C "$WORK_ROOT/old-archive"
tar -xzf "$WORK_ROOT/old-archive/data.tar.gz" -C "$WORK_ROOT/old-data"
XRAY_BINARY="$WORK_ROOT/old-data/opt/broray/bin/xray" SOURCE_DATE_EPOCH="$SOURCE_DATE_EPOCH" \
    "$REPOSITORY_ROOT/scripts/build-opkg-release-2.2.0.sh"

PACKAGE="$REPOSITORY_ROOT/dist/opkg/aarch64-3.10/broray_2.2.0-1_aarch64-3.10.ipk"
PACKAGES="$REPOSITORY_ROOT/dist/opkg/aarch64-3.10/Packages"
[ -s "$PACKAGE" ] || fail '2.2.0 package was not built'
[ -s "$PACKAGES" ] || fail 'Packages index was not built'
mkdir -p "$WORK_ROOT/ipk" "$WORK_ROOT/control" "$WORK_ROOT/data"
tar -xzf "$PACKAGE" -C "$WORK_ROOT/ipk"
tar -xzf "$WORK_ROOT/ipk/control.tar.gz" -C "$WORK_ROOT/control"
tar -xzf "$WORK_ROOT/ipk/data.tar.gz" -C "$WORK_ROOT/data"
grep -Fxq 'Version: 2.2.0-1' "$WORK_ROOT/control/control" || fail 'package version'
grep -Fxq 'Architecture: aarch64-3.10' "$WORK_ROOT/control/control" || fail 'package architecture'
[ "$(sed -n '1p' "$WORK_ROOT/data/opt/broray/config/version")" = '2.2.0' ] || fail 'packaged app version'
[ -x "$WORK_ROOT/data/opt/broray/bin/broray-routes-dot" ] || fail 'DoT command missing from package'
[ -x "$WORK_ROOT/data/opt/broray/lib/broray-cleanup.sh" ] || fail 'cleanup module missing from package'
[ -x "$WORK_ROOT/data/opt/broray/web-new/api/broray/cleanup.cgi" ] || fail 'cleanup API missing from package'
[ -r "$WORK_ROOT/data/opt/broray/web-new/assets/images/support/cloudtips-qr.svg" ] || fail 'support QR missing from package'
for forbidden in \
    opt/broray/config/config.json \
    opt/broray/config/active-server \
    opt/broray/config/interface.json; do
    [ ! -e "$WORK_ROOT/data/$forbidden" ] || fail "runtime file entered package: $forbidden"
done
! find "$WORK_ROOT/data/opt/broray/servers" "$WORK_ROOT/data/opt/broray/subscriptions" "$WORK_ROOT/data/opt/broray/routes/state" -type f -print -quit | grep -q . || fail 'user/runtime data entered package'
package_sha="$(sha256sum "$PACKAGE" | awk '{print $1}')"
index_sha="$(awk '/^SHA256sum:/ {print $2; exit}' "$PACKAGES")"
[ "$package_sha" = "$index_sha" ] || fail 'Packages SHA does not match IPK'
bash "$REPOSITORY_ROOT/scripts/test-opkg-upgrade.sh" "$OLD_PACKAGE" "$PACKAGE"
printf '%s\n' "Built package SHA256: $package_sha"
printf '%s\n' 'Verification 3 PASS'

printf '%s\n' '=== BROray 2.2.0 verification 4/5: installer, updater and documentation ==='
UPDATER="$REPOSITORY_ROOT/scripts/safe-opkg-upgrade-2.2.0-1.sh"
INSTALLER="$REPOSITORY_ROOT/scripts/install-opkg-release-2.2.0-1.sh"
grep -Fq 'TARGET_VERSION="2.2.0-1"' "$UPDATER" || fail 'updater target'
grep -Fq 'TARGET_VERSION="2.2.0-1"' "$INSTALLER" || fail 'installer target'
grep -Fq 'BRORAY_OPKG_BACKUP="$BACKUP" opkg install' "$UPDATER" || fail 'prepared backup is not passed to OPKG'
grep -Fq "printf '%s\\n%s\\n' \"\$BACKUP\"" "$UPDATER" || fail 'backup marker stores wrong value'
! grep -Fq 'rm -rf "$BRORAY_DIR/backup"' "$UPDATER" || fail 'updater deletes saved backups'
! grep -Fq 'tar -X' "$REPOSITORY_ROOT/packaging/opkg/preinst" || fail 'preinst still uses ambiguous tar -X'

updater_package_sha="$(sed -n 's/^PACKAGE_SHA="\([0-9a-f]*\)"/\1/p' "$UPDATER")"
installer_package_sha="$(sed -n 's/^PACKAGE_SHA="\([0-9a-f]*\)"/\1/p' "$INSTALLER")"
if [ "$ALLOW_PLACEHOLDERS" = 1 ]; then
    [ -n "$package_sha" ] || fail 'candidate package SHA is empty'
else
    [ "$updater_package_sha" = "$package_sha" ] || fail 'updater package SHA'
    [ "$installer_package_sha" = "$package_sha" ] || fail 'installer package SHA'
    if grep -R -n -E --binary-files=without-match '__[A-Z_]+__' \
        "$REPOSITORY_ROOT/README.md" "$REPOSITORY_ROOT/site" "$UPDATER" "$INSTALLER"; then
        fail 'release placeholder remains'
    fi
    updater_sha="$(sha256sum "$UPDATER" | awk '{print $1}')"
    installer_sha="$(sha256sum "$INSTALLER" | awk '{print $1}')"
    grep -Fq "$updater_sha  \$TARGET.part" "$REPOSITORY_ROOT/README.md" || fail 'README updater SHA'
    grep -Fq "$installer_sha  \$TARGET.part" "$REPOSITORY_ROOT/README.md" || fail 'README installer SHA'
    grep -Fq "$updater_sha  \$TARGET.part" "$REPOSITORY_ROOT/site/docs.brovibe.cloud/broray/index.html" || fail 'site updater SHA'
    grep -Fq "$installer_sha  \$TARGET.part" "$REPOSITORY_ROOT/site/docs.brovibe.cloud/broray/index.html" || fail 'site installer SHA'
fi
(
    cd "$REPOSITORY_ROOT/site/docs.brovibe.cloud"
    sha256sum -c SHA256SUMS
)
printf '%s\n' 'Verification 4 PASS'

printf '%s\n' '=== BROray 2.2.0 verification 5/5: release surface ==='
grep -Fq 'BROray 2.2.0' "$REPOSITORY_ROOT/README.md" || fail 'README version'
grep -Fq '## 2.2.0 — 2026-07-29' "$REPOSITORY_ROOT/CHANGELOG.md" || fail 'changelog entry'
[ -r "$REPOSITORY_ROOT/docs/RELEASE-2.2.0.md" ] || fail 'release notes missing'
grep -Fq 'BROray 2.2.0' "$REPOSITORY_ROOT/site/docs.brovibe.cloud/broray/index.html" || fail 'site version'
grep -Fq 'https://pay.cloudtips.ru/p/09b23d0a' "$REPOSITORY_ROOT/README.md" || fail 'support link'
printf '%s\n' 'ALL BROray 2.2.0 VERIFICATIONS PASS'
