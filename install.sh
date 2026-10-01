#!/bin/sh
set -eu
export PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/sbin
for c in curl jq mktemp sha256sum awk; do
 command -v "$c" >/dev/null || { printf 'Установите зависимости Entware: opkg install ca-bundle ca-certificates curl jq\n' >&2; exit 1; }
done
t="$(mktemp /tmp/broray-public-install.XXXXXXXX)"
trap 'rm -f "$t"' EXIT
curl -q -fL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 180 'https://api.brovibe.cloud/releases/stable/broray/3.1.1-r12/INSTALL-ON-ROUTER.sh' -o "$t"
[ "$(sha256sum "$t" | awk '{print $1}')" = 'ac334c4f3ce16e9119dcc3b84e21bd076ba5fce5252cf0f6aef2df5510edba1a' ]
/opt/bin/ash "$t"
