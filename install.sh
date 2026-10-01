#!/bin/sh
set -eu
export PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/sbin
for c in curl jq mktemp sha256sum awk; do
 command -v "$c" >/dev/null || { printf 'Установите зависимости Entware: opkg install ca-bundle ca-certificates curl jq\n' >&2; exit 1; }
done
t="$(mktemp /tmp/broray-public-install.XXXXXXXX)"
trap 'rm -f "$t"' EXIT
curl -q -fL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 180 'https://api.brovibe.cloud/releases/stable/broray/3.2.0-r02/INSTALL-ON-ROUTER.sh' -o "$t"
[ "$(sha256sum "$t" | awk '{print $1}')" = 'd635f05ec0426bcb11f5cb666874301d06dba5d53b4ed33ef27d14e11a80d2a1' ]
/opt/bin/ash "$t"
