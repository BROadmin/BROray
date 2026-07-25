#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 2 ]; then
    printf 'Usage: %s OLD_IPK NEW_IPK\n' "$0" >&2
    exit 2
fi

OLD_IPK="$1"
NEW_IPK="$2"
EXPECTED_XRAY_SHA256="dd3ba298aa32af9442163ee791d54f562bd89aa860fed1d0c47306fb019c1e64"
WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/broray-upgrade-test.XXXXXX")"
OLD_ROOT="$WORK_ROOT/old"
NEW_ROOT="$WORK_ROOT/new"
UPGRADE_ROOT="$WORK_ROOT/upgrade"
PRESERVE_ROOT="$WORK_ROOT/preserve"

cleanup()
{
    rm -rf "$WORK_ROOT"
}

fail()
{
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

extract_ipk()
{
    local package_path output_root

    package_path="$1"
    output_root="$2"

    mkdir -p "$output_root/archive" "$output_root/control" "$output_root/data"

    if ! tar -xzf "$package_path" -C "$output_root/archive" 2>/dev/null; then
        (
            cd "$output_root/archive"
            ar x "$package_path"
        )
    fi
    tar -xzf "$output_root/archive/control.tar.gz" -C "$output_root/control"
    tar -xzf "$output_root/archive/data.tar.gz" -C "$output_root/data"
}

trap cleanup EXIT

for required_command in ar cp jq mkdir mktemp sha256sum tar; do
    command -v "$required_command" >/dev/null 2>&1 ||
        fail "missing command: $required_command"
done

[ -f "$OLD_IPK" ] || fail "old package not found: $OLD_IPK"
[ -f "$NEW_IPK" ] || fail "new package not found: $NEW_IPK"

OLD_IPK="$(cd "$(dirname "$OLD_IPK")" && pwd)/$(basename "$OLD_IPK")"
NEW_IPK="$(cd "$(dirname "$NEW_IPK")" && pwd)/$(basename "$NEW_IPK")"

extract_ipk "$OLD_IPK" "$OLD_ROOT"
extract_ipk "$NEW_IPK" "$NEW_ROOT"

cp -a "$OLD_ROOT/data/." "$UPGRADE_ROOT/"

mkdir -p \
    "$UPGRADE_ROOT/opt/broray/config/subscriptions" \
    "$UPGRADE_ROOT/opt/broray/routes/state" \
    "$UPGRADE_ROOT/opt/broray/servers"

jq -n '{
    marker: "upgrade-user-xray-config",
    outbounds: []
}' >"$UPGRADE_ROOT/opt/broray/config/config.json"

jq -n '{
    listenAddress: "192.0.2.55",
    marker: "upgrade-user-settings"
}' >"$UPGRADE_ROOT/opt/broray/config/system/settings.json"

jq -n '{
    id: "upgrade-user-subscription",
    url: "https://example.invalid/private-subscription"
}' >"$UPGRADE_ROOT/opt/broray/config/subscriptions/upgrade-user.json"

jq -n '{
    id: "upgrade-user-server",
    marker: "upgrade-user-server"
}' >"$UPGRADE_ROOT/opt/broray/servers/upgrade-user.json"

jq -n '{
    bundleId: "upgrade-user-routes",
    marker: "upgrade-user-routes"
}' >"$UPGRADE_ROOT/opt/broray/routes/state/upgrade-user.json"

while IFS= read -r conffile; do
    [ -n "$conffile" ] || continue

    relative_path="${conffile#/}"
    [ -f "$UPGRADE_ROOT/$relative_path" ] || continue

    mkdir -p "$PRESERVE_ROOT/$(dirname "$relative_path")"
    cp -a \
        "$UPGRADE_ROOT/$relative_path" \
        "$PRESERVE_ROOT/$relative_path"
done <"$NEW_ROOT/control/conffiles"

cp -a "$NEW_ROOT/data/." "$UPGRADE_ROOT/"

if [ -d "$PRESERVE_ROOT" ]; then
    cp -a "$PRESERVE_ROOT/." "$UPGRADE_ROOT/"
fi

jq -e \
    '.marker == "upgrade-user-xray-config"' \
    "$UPGRADE_ROOT/opt/broray/config/config.json" >/dev/null ||
    fail "active Xray configuration was overwritten"

jq -e \
    '.marker == "upgrade-user-settings"' \
    "$UPGRADE_ROOT/opt/broray/config/system/settings.json" >/dev/null ||
    fail "OPKG conffile settings were overwritten"

jq -e \
    '.id == "upgrade-user-subscription"' \
    "$UPGRADE_ROOT/opt/broray/config/subscriptions/upgrade-user.json" >/dev/null ||
    fail "subscription was lost"

jq -e \
    '.marker == "upgrade-user-server"' \
    "$UPGRADE_ROOT/opt/broray/servers/upgrade-user.json" >/dev/null ||
    fail "server was lost"

jq -e \
    '.marker == "upgrade-user-routes"' \
    "$UPGRADE_ROOT/opt/broray/routes/state/upgrade-user.json" >/dev/null ||
    fail "route state was lost"

[ "$(sed -n '1p' "$UPGRADE_ROOT/opt/broray/config/version")" = "2.1.0" ] ||
    fail "version was not upgraded"

[ -f "$UPGRADE_ROOT/opt/broray/web-new/index.html" ] ||
    fail "WebUI 2.1.0 is missing"

[ "$(
    sha256sum "$UPGRADE_ROOT/opt/broray/bin/xray" |
        awk '{print $1}'
)" = "$EXPECTED_XRAY_SHA256" ] ||
    fail "Xray binary checksum mismatch after upgrade"

printf 'BROray OPKG upgrade preservation test: PASS\n'
