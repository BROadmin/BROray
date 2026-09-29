#!/bin/sh
set -eu
export PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/sbin
for c in curl jq mktemp sha256sum awk; do
 command -v "$c" >/dev/null || { printf 'Установите зависимости Entware: opkg install ca-bundle ca-certificates curl jq\n' >&2; exit 1; }
done
t="$(mktemp /tmp/broray-public-install.XXXXXXXX)"
trap 'rm -f "$t"' EXIT
curl -q -fL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 180 'https://api.brovibe.cloud/releases/stable/broray/3.2.0-r01/INSTALL-ON-ROUTER.sh' -o "$t"
[ "$(sha256sum "$t" | awk '{print $1}')" = 'f2301073a0a1e44b900593275dfd6922880e4a1a24c5efefbd96d564f9b8b8d8' ]
/opt/bin/ash "$t"
