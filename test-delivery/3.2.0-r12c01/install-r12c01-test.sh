#!/opt/bin/ash
set -eu
umask 077
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/sbin
export PATH

BASE='https://raw.githubusercontent.com/BROadmin/BROray/test-r12c01-manual-delivery-20261004/test-delivery/3.2.0-r12c01'
STABLE_INSTALLER='https://api.brovibe.cloud/releases/stable/broray/3.1.1-r12/INSTALL-ON-ROUTER.sh'
STABLE_INSTALLER_SHA='ac334c4f3ce16e9119dcc3b84e21bd076ba5fce5252cf0f6aef2df5510edba1'
STABLE_INSTALLER_BYTES='41016'
TARGET='3.2.0-r12c01'
WORK="/tmp/broray-r12c01-manual.$$"
HOOK="$WORK/fetch-hook.sh"

fail() { printf 'BROray r12c01 test installer ERROR: %s\n' "$*" >&2; exit 1; }
cleanup() {
  rc=$?
  trap - EXIT HUP INT TERM
  case "$WORK" in /tmp/broray-r12c01-manual.*) [ ! -L "$WORK" ] && rm -rf "$WORK" 2>/dev/null || true ;; esac
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

[ -x /opt/bin/ash ] || fail 'Entware /opt/bin/ash is required'
command -v opkg >/dev/null 2>&1 || fail 'Entware opkg is required'
for c in curl jq sha256sum awk mktemp wc tr cp chmod rm mkdir readlink; do
  command -v "$c" >/dev/null 2>&1 || fail "required command is unavailable: $c"
done
[ "$(readlink -f /tmp)" = /tmp ] && [ ! -L /tmp ] || fail 'unsafe /tmp'
mkdir -m 700 "$WORK" || fail 'cannot create RAM workspace'

fetch_exact() {
  name="$1"; bytes="$2"; digest="$3"
  out="$WORK/$name"
  curl -q -fL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 180 \
    -H 'Accept-Encoding: identity' -H 'Cache-Control: no-cache, no-store, max-age=0' \
    "$BASE/$name" -o "$out" || fail "download failed: $name"
  [ "$(wc -c <"$out" | tr -d ' ')" = "$bytes" ] || fail "size mismatch: $name"
  [ "$(sha256sum "$out" | awk 'NR==1{print $1}')" = "$digest" ] || fail "SHA-256 mismatch: $name"
}

status="$(opkg status broray 2>/dev/null || true)"
if [ -z "$status" ]; then
  stable="$WORK/stable-3.1.1-installer.sh"
  curl -q -fL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 180 \
    -H 'Accept-Encoding: identity' -H 'Cache-Control: no-cache, no-store, max-age=0' \
    "$STABLE_INSTALLER" -o "$stable" || fail 'Stable bootstrap download failed'
  [ "$(wc -c <"$stable" | tr -d ' ')" = "$STABLE_INSTALLER_BYTES" ] || fail 'Stable bootstrap size mismatch'
  [ "$(sha256sum "$stable" | awk 'NR==1{print $1}')" = "$STABLE_INSTALLER_SHA" ] || fail 'Stable bootstrap SHA-256 mismatch'
  /opt/bin/ash "$stable" || {
    rc=$?
    [ "$rc" = 75 ] && printf '%s\n' 'REBOOT_REQUIRED=YES' 'After reboot, run the same command again.'
    exit "$rc"
  }
else
  printf '%s\n' "$status" | awk -F ': ' '
    $1=="Package"{p++;pv=$2}
    $1=="Status"{s++;sv=$2}
    END{exit !(p==1&&pv=="broray"&&s==1&&sv=="install user installed")}
  ' || fail 'BROray package state is partial or ambiguous; refused without changes'
fi

fetch_exact 'release.json' '1943' '021a3613b317d3ef44039f22f1ecb73a18d3f6f1a0fe0c6c2f76b659fe2a8595'
fetch_exact 'release.json.minisig' '285' '5ebface05ba92c0e6a0221106737aa363972da1c9d51f3bc723a60cd310db349'
fetch_exact 'broray-app-3.2.0-r12c01.tar.gz' '1609274' 'c8fc8b5478178bf9eb0f07db1f67453722a32a940b664ce91ed448029829399f'
fetch_exact 'prepare-persistent-updater.sh' '3002' '2896e85cfa355eab3c92e0ec02e76994f9de934c46b329eadd62c9cc2283fa9e'
fetch_exact 'INSTALL-ON-ROUTER.sh' '24487' '708ef201c6f8eab48fbb0896a7b464a64e16227b201bd5762b196e4574bdb35c'

real_curl="$(command -v curl)"
cat >"$HOOK" <<EOF_HOOK
#!/opt/bin/ash
set -eu
url="\$1"; out="\$2"; maximum="\$3"
copy_local() {
  src="\$1"
  bytes="\$(wc -c <"\$src" | tr -d ' ')"
  [ "\$bytes" -le "\$maximum" ] || exit 1
  cp "\$src" "\$out"
}
case "\$url" in
  'https://manual-r12c01.invalid/release.json') copy_local '$WORK/release.json' ;;
  'https://manual-r12c01.invalid/release.json.minisig') copy_local '$WORK/release.json.minisig' ;;
  'https://api.brovibe.cloud/releases/stable/broray/3.2.0-r12/broray-app-3.2.0-r12c01.tar.gz') copy_local '$WORK/broray-app-3.2.0-r12c01.tar.gz' ;;
  'https://api.brovibe.cloud/releases/staging/broray/3.2.0-r12c01-delivery-v1/prepare-persistent-updater.sh') copy_local '$WORK/prepare-persistent-updater.sh' ;;
  'https://api.brovibe.cloud/releases/stable/broray/3.2.0-r12/direct-clean-delivery.sh') exit 97 ;;
  'https://api.brovibe.cloud/releases/stable/broray/3.2.0-r12/broray-compact-updater-platform-5.tar.gz') exit 97 ;;
  *) exec '$real_curl' -q -fL --retry 3 --retry-delay 1 --connect-timeout 15 --max-time 180 --max-filesize "\$maximum" \
       -H 'Accept-Encoding: identity' -H 'Cache-Control: no-cache, no-store, max-age=0' "\$url" -o "\$out" ;;
esac
EOF_HOOK
chmod 0700 "$HOOK" || fail 'cannot enable local fetch hook'

printf '%s\n' 'TEST_ONLY=YES' 'TARGET=3.2.0-r12c01' 'WARNING=withdrawn candidate; controlled validation only'

BRORAY_INSTALLER_FETCH_HOOK="$HOOK" \
BRORAY_INSTALLER_RELEASE_INDEX_URL='https://manual-r12c01.invalid/release.json' \
BRORAY_INSTALLER_RELEASE_SIGNATURE_URL='https://manual-r12c01.invalid/release.json.minisig' \
  /opt/bin/ash "$WORK/INSTALL-ON-ROUTER.sh" || {
    rc=$?
    [ "$rc" = 75 ] && printf '%s\n' 'REBOOT_REQUIRED=YES' 'After reboot, run the same command again.'
    exit "$rc"
  }

info="$(/opt/broray/bin/broray-system info)" || fail 'installed health unavailable'
printf '%s\n' "$info" | jq -e --arg c "$TARGET" '.ok==true and .candidateId==$c and .installationHealthy==true and .versionsConsistent==true and .opkgRegistrationHealthy==true' >/dev/null \
  || fail 'installed candidate health contract failed'
printf '%s\n' "$info"
printf '%s\n' 'BRORAY_R12C01_TEST_INSTALL=PASS'
