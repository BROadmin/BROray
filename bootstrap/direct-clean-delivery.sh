#!/opt/bin/ash
set -eu
umask 077
export PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
root=/tmp/broray-clean-3.2.0-r01c37
fail() { printf 'BROray clean delivery ERROR: %s\n' "$*" >&2; exit 1; }
[ "$(readlink -f /tmp)" = /tmp ] && [ ! -L /tmp ] || fail 'unsafe RAM path'
awk '$2=="/tmp" && ($3=="tmpfs" || $3=="ramfs"){ok=1} END{exit !ok}' /proc/mounts || fail '/tmp must be RAM-backed'
[ ! -e "$root" ] && [ ! -L "$root" ] || fail 'delivery workspace already exists; evidence retained'
mkdir -m 700 "$root" || fail 'cannot reserve RAM workspace'
curl -q -fL --connect-timeout 15 --max-time 180 --max-filesize 14391780 --proto '=https' --proto-redir '=https' -H 'Accept-Encoding: identity' 'https://api.brovibe.cloud/releases/stable/broray/3.2.0-r01/broray-clean-payload-3.2.0-r01c37.tar.gz' -o "$root/payload.tar.gz" || fail 'download failed; workspace retained'
[ "$(wc -c <"$root/payload.tar.gz" | tr -d ' ')" = 14391780 ] || fail 'payload size differs'
printf '%s\n' '56ac2b9928fa34849b4f079712e1c8843bc5105e99b5459e598844f494c328d6  payload.tar.gz' | (cd "$root" && sha256sum -c -) || fail 'payload differs'
/opt/bin/tar -xzf "$root/payload.tar.gz" -C "$root" || fail 'payload extraction failed'
(cd "$root" && sha256sum -c SHA256SUMS) || fail 'payload files differ'
opkg install "$root/broray_3.0.0-r14_aarch64-3.10.ipk" || fail 'installation incomplete; retained state requires inspection'
[ "$(readlink -f "$root")" = "$root" ] && [ ! -L "$root" ] && [ "$(cat "$root/TEST-OWNER")" = 3.2.0-r01c37 ] || fail 'workspace identity changed'
rm -rf "$root"
printf '%s\n' 'CLEAN_APP_STAGE=PASS' 'UPDATER_READINESS=PENDING_CANONICAL_PREFLIGHT'
