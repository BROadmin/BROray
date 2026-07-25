#!/usr/bin/env bash

set -euo pipefail

PRODUCT="broray"
VERSION="2.1.0"
REVISION="1"
PACKAGE_VERSION="${VERSION}-${REVISION}"
ARCHITECTURE="aarch64-3.10"
XRAY_VERSION="26.7.11"
XRAY_ASSET="Xray-linux-arm64-v8a.zip"
XRAY_ASSET_SHA256="89cfe01674d7c9f6847b7dd9389537be9acb3b9dc3c6cb9fdeba87a3e4e57fc1"
XRAY_BINARY_SHA256="dd3ba298aa32af9442163ee791d54f562bd89aa860fed1d0c47306fb019c1e64"

REPOSITORY_ROOT="$(
    CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." &&
        pwd
)"
SOURCE_ROOT="$REPOSITORY_ROOT/root"
PACKAGING_ROOT="$REPOSITORY_ROOT/packaging/opkg"
DIST_ROOT="$REPOSITORY_ROOT/dist"
WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/broray-opkg-build.XXXXXX")"
DATA_ROOT="$WORK_ROOT/data"
CONTROL_ROOT="$WORK_ROOT/control"
IPK_ROOT="$WORK_ROOT/ipk"
FEED_ROOT="$DIST_ROOT/opkg/$ARCHITECTURE"
PACKAGE_NAME="${PRODUCT}_${PACKAGE_VERSION}_${ARCHITECTURE}.ipk"
PACKAGE_PATH="$FEED_ROOT/$PACKAGE_NAME"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(date +%s)}"

cleanup()
{
    rm -rf "$WORK_ROOT"
}

fail()
{
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

require_command()
{
    command -v "$1" >/dev/null 2>&1 ||
        fail "missing command: $1"
}

for required_command in \
    curl \
    find \
    gzip \
    jq \
    sed \
    sha256sum \
    tar \
    unzip
do
    require_command "$required_command"
done

trap cleanup EXIT

[ -d "$SOURCE_ROOT/opt/broray" ] ||
    fail "missing release root"
[ -d "$PACKAGING_ROOT" ] ||
    fail "missing OPKG metadata"

mkdir -p \
    "$DATA_ROOT/opt/broray" \
    "$DATA_ROOT/opt/etc/init.d" \
    "$CONTROL_ROOT" \
    "$IPK_ROOT" \
    "$FEED_ROOT"

for source_name in bin config lib routes share web-new; do
    cp -a \
        "$SOURCE_ROOT/opt/broray/$source_name" \
        "$DATA_ROOT/opt/broray/"
done

cp -a \
    "$SOURCE_ROOT/opt/etc/init.d/." \
    "$DATA_ROOT/opt/etc/init.d/"

mkdir -p \
    "$DATA_ROOT/opt/broray/backup" \
    "$DATA_ROOT/opt/broray/backups" \
    "$DATA_ROOT/opt/broray/config/disabled-subscription-servers" \
    "$DATA_ROOT/opt/broray/config/subscriptions" \
    "$DATA_ROOT/opt/broray/data" \
    "$DATA_ROOT/opt/broray/deleted-subscriptions" \
    "$DATA_ROOT/opt/broray/logs" \
    "$DATA_ROOT/opt/broray/routes/backup" \
    "$DATA_ROOT/opt/broray/routes/catalog" \
    "$DATA_ROOT/opt/broray/routes/installed/bundles" \
    "$DATA_ROOT/opt/broray/routes/locks" \
    "$DATA_ROOT/opt/broray/routes/manifests" \
    "$DATA_ROOT/opt/broray/routes/state" \
    "$DATA_ROOT/opt/broray/routes/tmp" \
    "$DATA_ROOT/opt/broray/routes/transactions" \
    "$DATA_ROOT/opt/broray/run/server-quality" \
    "$DATA_ROOT/opt/broray/run/subscriptions" \
    "$DATA_ROOT/opt/broray/run/web-new/sessions" \
    "$DATA_ROOT/opt/broray/servers" \
    "$DATA_ROOT/opt/broray/subscriptions" \
    "$DATA_ROOT/opt/broray/tmp" \
    "$DATA_ROOT/opt/broray/update" \
    "$DATA_ROOT/opt/broray/share/licenses/xray"

cp -a \
    "$REPOSITORY_ROOT/third_party/xray/LICENSE" \
    "$DATA_ROOT/opt/broray/share/licenses/xray/LICENSE"
cp -a \
    "$REPOSITORY_ROOT/third_party/xray/README.md" \
    "$DATA_ROOT/opt/broray/share/licenses/xray/README.md"

for manifest in "$DATA_ROOT/opt/broray/share/routes/manifests"/*.json; do
    [ -f "$manifest" ] || continue
    cp -a \
        "$manifest" \
        "$DATA_ROOT/opt/broray/routes/manifests/${manifest##*/}"
done

XRAY_DOWNLOAD_ROOT="$WORK_ROOT/xray"
XRAY_ARCHIVE="$XRAY_DOWNLOAD_ROOT/$XRAY_ASSET"
XRAY_DIGEST="$XRAY_ARCHIVE.dgst"
mkdir -p "$XRAY_DOWNLOAD_ROOT"

if [ -n "${XRAY_BINARY:-}" ]; then
    [ -f "$XRAY_BINARY" ] ||
        fail "XRAY_BINARY does not exist"
    cp -a "$XRAY_BINARY" "$DATA_ROOT/opt/broray/bin/xray"
else
    XRAY_BASE_URL="https://github.com/XTLS/Xray-core/releases/download/v$XRAY_VERSION"

    curl \
        -fL \
        --connect-timeout 15 \
        --max-time 180 \
        -o "$XRAY_ARCHIVE" \
        "$XRAY_BASE_URL/$XRAY_ASSET"
    curl \
        -fL \
        --connect-timeout 15 \
        --max-time 60 \
        -o "$XRAY_DIGEST" \
        "$XRAY_BASE_URL/$XRAY_ASSET.dgst"

    official_archive_sha="$(
        awk '
            tolower($0) ~ /^sha2-256[= ]/ {
                print $NF
                exit
            }
        ' "$XRAY_DIGEST" |
            tr -d '\r'
    )"
    actual_archive_sha="$(
        sha256sum "$XRAY_ARCHIVE" |
            awk '{print $1}'
    )"

    [ "$official_archive_sha" = "$XRAY_ASSET_SHA256" ] ||
        fail "official Xray digest changed"
    [ "$actual_archive_sha" = "$XRAY_ASSET_SHA256" ] ||
        fail "Xray archive checksum mismatch"

    unzip -p \
        "$XRAY_ARCHIVE" \
        xray \
        >"$DATA_ROOT/opt/broray/bin/xray"
fi

chmod 755 "$DATA_ROOT/opt/broray/bin/xray"

actual_xray_sha="$(
    sha256sum "$DATA_ROOT/opt/broray/bin/xray" |
        awk '{print $1}'
)"
[ "$actual_xray_sha" = "$XRAY_BINARY_SHA256" ] ||
    fail "Xray binary checksum mismatch"

find "$DATA_ROOT/opt/broray/bin" \
    -type f \
    -exec chmod 755 {} +
find "$DATA_ROOT/opt/broray/web-new/api" \
    -type f \
    \( -name '*.cgi' -o -name '*.sh' \) \
    -exec chmod 755 {} +
chmod 755 \
    "$DATA_ROOT/opt/broray/lib/package-setup.sh" \
    "$DATA_ROOT/opt/etc/init.d/"*
chmod 600 \
    "$DATA_ROOT/opt/broray/config/system/settings.json" \
    "$DATA_ROOT/opt/broray/config/system/server-auto-switch.json"

find "$DATA_ROOT/opt/broray/servers" \
    "$DATA_ROOT/opt/broray/subscriptions" \
    "$DATA_ROOT/opt/broray/deleted-subscriptions" \
    "$DATA_ROOT/opt/broray/config/subscriptions" \
    "$DATA_ROOT/opt/broray/routes/backup" \
    "$DATA_ROOT/opt/broray/routes/catalog" \
    "$DATA_ROOT/opt/broray/routes/installed" \
    "$DATA_ROOT/opt/broray/routes/state" \
    "$DATA_ROOT/opt/broray/routes/transactions" \
    -type f \
    -print -quit |
    grep -q . &&
    fail "runtime or user data entered the package"

[ ! -e "$DATA_ROOT/opt/broray/config/config.json" ] ||
    fail "active Xray configuration entered the package"

installed_size="$(
    find "$DATA_ROOT" -type f -printf '%s\n' |
        awk '{sum += $1} END {print sum + 0}'
)"

sed \
    -e "s/@PACKAGE_VERSION@/$PACKAGE_VERSION/g" \
    -e "s/@INSTALLED_SIZE@/$installed_size/g" \
    "$PACKAGING_ROOT/control.in" \
    >"$CONTROL_ROOT/control"

for control_file in conffiles preinst postinst prerm postrm; do
    cp -a \
        "$PACKAGING_ROOT/$control_file" \
        "$CONTROL_ROOT/$control_file"
done

chmod 755 \
    "$CONTROL_ROOT/preinst" \
    "$CONTROL_ROOT/postinst" \
    "$CONTROL_ROOT/prerm" \
    "$CONTROL_ROOT/postrm"
chmod 644 \
    "$CONTROL_ROOT/control" \
    "$CONTROL_ROOT/conffiles"

printf '2.0\n' >"$IPK_ROOT/debian-binary"

tar \
    --sort=name \
    --mtime="@$SOURCE_DATE_EPOCH" \
    --owner=0 \
    --group=0 \
    --numeric-owner \
    -C "$DATA_ROOT" \
    -cf - \
    . |
    gzip -n -9 >"$IPK_ROOT/data.tar.gz"

tar \
    --sort=name \
    --mtime="@$SOURCE_DATE_EPOCH" \
    --owner=0 \
    --group=0 \
    --numeric-owner \
    -C "$CONTROL_ROOT" \
    -cf - \
    . |
    gzip -n -9 >"$IPK_ROOT/control.tar.gz"

tar \
    --sort=name \
    --mtime="@$SOURCE_DATE_EPOCH" \
    --owner=0 \
    --group=0 \
    --numeric-owner \
    -C "$IPK_ROOT" \
    -cf - \
    ./debian-binary \
    ./data.tar.gz \
    ./control.tar.gz |
    gzip -n -9 >"$PACKAGE_PATH"

package_size="$(wc -c <"$PACKAGE_PATH" | tr -d ' ')"
package_sha="$(
    sha256sum "$PACKAGE_PATH" |
        awk '{print $1}'
)"

{
    cat "$CONTROL_ROOT/control"
    printf 'Filename: %s\n' "$PACKAGE_NAME"
    printf 'Size: %s\n' "$package_size"
    printf 'SHA256sum: %s\n' "$package_sha"
    printf '\n'
} >"$FEED_ROOT/Packages"

gzip -n -9 -c \
    "$FEED_ROOT/Packages" \
    >"$FEED_ROOT/Packages.gz"

cp -a \
    "$PACKAGING_ROOT/opkg.sh" \
    "$DIST_ROOT/opkg.sh"
chmod 644 \
    "$DIST_ROOT/opkg.sh" \
    "$FEED_ROOT/Packages" \
    "$FEED_ROOT/Packages.gz" \
    "$PACKAGE_PATH"

(
    cd "$DIST_ROOT"
    sha256sum \
        opkg.sh \
        "opkg/$ARCHITECTURE/Packages" \
        "opkg/$ARCHITECTURE/Packages.gz" \
        "opkg/$ARCHITECTURE/$PACKAGE_NAME"
) >"$DIST_ROOT/SHA256SUMS"

UPDATE_ARCHIVE="$DIST_ROOT/BROray-opkg-update-$PACKAGE_VERSION.tar.gz"
tar \
    --sort=name \
    --mtime="@$SOURCE_DATE_EPOCH" \
    --owner=0 \
    --group=0 \
    --numeric-owner \
    -C "$DIST_ROOT" \
    -czf "$UPDATE_ARCHIVE" \
    opkg.sh \
    SHA256SUMS \
    "opkg/$ARCHITECTURE/Packages" \
    "opkg/$ARCHITECTURE/Packages.gz" \
    "opkg/$ARCHITECTURE/$PACKAGE_NAME"

printf 'Built %s\n' "$PACKAGE_PATH"
printf 'SHA256 %s\n' "$package_sha"
printf 'Update archive %s\n' "$UPDATE_ARCHIVE"
