#!/opt/bin/ash
set -eu
umask 077
BASE=/opt/broray
T="$BASE/tmp/auth-restore.$$"
B="$BASE/backup/auth-restore-$(date +%Y%m%d-%H%M%S)"
ARCHIVE="$T/app.tar.gz"
URL="https://github.com/BROadmin/BROray/releases/download/v3.1.1-r12/broray-app-3.1.1-r12c01.tar.gz"
ARCHIVE_SHA="b51c44ff874cc5d5792b7e57657206623adda4ca2744fe625fc02a96151b62aa"
AUTH_SHA="9ee2c1d0efe518dff4de71adf5741f4b5d583f65b48282514adff5c73f4d8cb1"
NATIVE_SHA="f77d3766dc685c6105e6009d42bc33e153f2da3a240cada5152fc14fcd59346d"
fail(){ echo "AUTH_RESTORE=FAIL REASON=$1" >&2; exit 1; }
rm -rf "$T"
mkdir -p "$T" "$B" || fail mkdir
trap 'rm -rf "$T"' EXIT HUP INT TERM
/opt/bin/curl -q --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 120 -fsSL "$URL" -o "$ARCHIVE" || fail download
[ "$(sha256sum "$ARCHIVE" | awk 'NR==1{print $1}')" = "$ARCHIVE_SHA" ] || fail archive-sha
tar -xzf "$ARCHIVE" -C "$T" app/lib/web-auth.sh app/lib/web-auth-native.sh || fail extract
[ "$(sha256sum "$T/app/lib/web-auth.sh" | awk 'NR==1{print $1}')" = "$AUTH_SHA" ] || fail auth-sha
[ "$(sha256sum "$T/app/lib/web-auth-native.sh" | awk 'NR==1{print $1}')" = "$NATIVE_SHA" ] || fail native-sha
/opt/bin/ash -n "$T/app/lib/web-auth.sh" || fail auth-syntax
/opt/bin/ash -n "$T/app/lib/web-auth-native.sh" || fail native-syntax
for F in web-auth.sh web-auth-native.sh; do
  P="$BASE/lib/$F"
  if [ -e "$P" ] || [ -L "$P" ]; then
    [ -f "$P" ] && [ ! -L "$P" ] || fail unsafe-live-file
    cp -p "$P" "$B/$F" || fail backup-live
  fi
  cp -p "$T/app/lib/$F" "$P.new.$$" || fail stage-copy
  mv -f "$P.new.$$" "$P" || fail install
done
rm -rf "$T"
trap - EXIT HUP INT TERM
echo "AUTH_RESTORE=PASS BACKUP=$B"
