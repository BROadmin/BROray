#!/usr/bin/env bash
set -euo pipefail

REPOSITORY_ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_ROOT="$REPOSITORY_ROOT/root/opt/broray"
INIT_ROOT="$REPOSITORY_ROOT/root/opt/etc/init.d"
WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/broray-2.1.1-2-verify.XXXXXX")"

cleanup() { rm -rf "$WORK_ROOT"; }
fail() { printf 'VERIFY 2.1.1-2 ERROR: %s\n' "$*" >&2; exit 1; }
trap cleanup EXIT HUP INT TERM

printf '%s\n' '=== BROray 2.1.1-2 verification 1/3: source and privacy ==='
bash -n \
    "$REPOSITORY_ROOT/scripts/build-opkg-release-2.1.1-2.sh" \
    "$REPOSITORY_ROOT/scripts/safe-opkg-upgrade-2.1.1-2.sh" \
    "$REPOSITORY_ROOT/scripts/install-opkg-release-2.1.1-2.sh" \
    "$REPOSITORY_ROOT/scripts/verify-release-2.1.1-2.sh"

{
    find "$RUNTIME_ROOT/bin" -type f ! -name xray
    find "$RUNTIME_ROOT/lib" "$RUNTIME_ROOT/tests" -type f -name '*.sh'
    find "$RUNTIME_ROOT/web-new/api" "$RUNTIME_ROOT/web-src/api" -type f \( -name '*.cgi' -o -name '*.sh' \)
    find "$INIT_ROOT" -type f
} | sort -u > "$WORK_ROOT/shell-files"
while IFS= read -r shell_file; do busybox ash -n "$shell_file" || fail "shell syntax: $shell_file"; done < "$WORK_ROOT/shell-files"

find "$RUNTIME_ROOT" -type f -name '*.json' -print0 |
while IFS= read -r -d '' json_file; do jq -e . "$json_file" >/dev/null || fail "invalid JSON: $json_file"; done

[ "$(sed -n '1p' "$RUNTIME_ROOT/config/version")" = '2.1.1' ] || fail 'config version is not 2.1.1'
grep -Fq 'VERSION="2.1.1"' "$RUNTIME_ROOT/lib/package-setup.sh" || fail 'package setup version is not 2.1.1'
grep -Fq 'server.max-request-size = 5120' "$RUNTIME_ROOT/lib/package-setup.sh" || fail 'lighttpd upload limit is not 5120 KiB'
for id in telegram whatsapp youtube chatgpt facebook instagram meta tiktok speedtest; do
    [ -r "$RUNTIME_ROOT/share/routes/manifests/$id.json" ] || fail "missing route manifest: $id"
done
! grep -R -F 'Экспортировать' "$RUNTIME_ROOT/web-new" >/dev/null 2>&1 || fail 'legacy UI label found'

if rg -n --no-messages '85[.]9[.]223[.]218|netcraze[.]link|zhornov|dimkatver|user-d98794' "$RUNTIME_ROOT" "$REPOSITORY_ROOT/docs" "$REPOSITORY_ROOT/site"; then
    fail 'local or personal marker found'
fi
printf '%s\n' 'Verification 1 PASS'

printf '%s\n' '=== BROray 2.1.1-2 verification 2/3: canonical WebUI ==='
BRORAY_ROOT="$RUNTIME_ROOT" BRORAY_WEB_SRC="$RUNTIME_ROOT/web-src" BRORAY_WEB_OUT="$WORK_ROOT/web-build" \
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
    cmp "$RUNTIME_ROOT/web-new/$relative_file" "$WORK_ROOT/web-build/$relative_file" || fail "generated WebUI differs: $relative_file"
done < "$WORK_ROOT/committed-web-files"
jq 'del(.builtAt,.source)' "$RUNTIME_ROOT/web-new/build.json" > "$WORK_ROOT/a.json"
jq 'del(.builtAt,.source)' "$WORK_ROOT/web-build/build.json" > "$WORK_ROOT/b.json"
cmp "$WORK_ROOT/a.json" "$WORK_ROOT/b.json" || fail 'WebUI build metadata differs'
printf '%s\n' 'Verification 2 PASS'

printf '%s\n' '=== BROray 2.1.1-2 verification 3/3: release scripts ==='
grep -Fq 'TARGET_VERSION="2.1.1-2"' "$REPOSITORY_ROOT/scripts/safe-opkg-upgrade-2.1.1-2.sh" || fail 'updater target version'
grep -Fq 'TARGET_VERSION="2.1.1-2"' "$REPOSITORY_ROOT/scripts/install-opkg-release-2.1.1-2.sh" || fail 'installer target version'
grep -Fq 'clean_before_backup' "$REPOSITORY_ROOT/scripts/safe-opkg-upgrade-2.1.1-2.sh" || fail 'cleanup before backup missing'
grep -Fq 'BACKUP="$WORK/backup.tar.gz"' "$REPOSITORY_ROOT/scripts/safe-opkg-upgrade-2.1.1-2.sh" || fail 'temporary backup missing'
grep -Fq 'https://t.me/BROvibe_vpn' "$REPOSITORY_ROOT/README.md" || fail 'support link missing'
printf '%s\n' 'ALL BROray 2.1.1-2 VERIFICATIONS PASS'
