#!/opt/bin/ash
set -eu
umask 077

BASE=/opt/broray
AUTH="$BASE/lib/web-auth.sh"
NATIVE="$BASE/lib/web-auth-native.sh"
INIT=/opt/etc/init.d/S25broray-web
LAN_FILE="$BASE/run/lan-ip"

fail()
{
    echo "AUTH_HOTFIX=FAIL REASON=$1" >&2
    exit 1
}

[ -f "$AUTH" ] && [ ! -L "$AUTH" ] || fail web-auth-unsafe
[ -f "$NATIVE" ] && [ ! -L "$NATIVE" ] || fail native-auth-unsafe
[ -x "$INIT" ] || fail init-missing

lan="$(sed -n '1p' "$LAN_FILE" 2>/dev/null || true)"
printf '%s\n' "$lan" | awk -F. '
    NF != 4 {exit 1}
    {
        for (i = 1; i <= 4; i++) {
            if ($i !~ /^[0-9]+$/ || $i < 0 || $i > 255) exit 1
        }
        if ($1 == 0 || $1 == 127 || $1 >= 224) exit 1
        if ($1 == 169 && $2 == 254) exit 1
    }
' || fail lan-invalid

grep -F 'http://127.0.0.1:79|http://127.0.0.1:18079)' "$AUTH" >/dev/null 2>&1 ||
    grep -F "http://$lan|http://127.0.0.1:79|http://127.0.0.1:18079)" "$AUTH" >/dev/null 2>&1 ||
    fail web-auth-layout-unknown

grep -F 'BRORAY_NATIVE_AUTH_SYSTEM_URL="${BRORAY_NATIVE_AUTH_SYSTEM_URL:-http://127.0.0.1:79}"' "$NATIVE" >/dev/null 2>&1 ||
    grep -F "BRORAY_NATIVE_AUTH_SYSTEM_URL=\"\${BRORAY_NATIVE_AUTH_SYSTEM_URL:-http://$lan}\"" "$NATIVE" >/dev/null 2>&1 ||
    fail native-auth-layout-unknown

backup="$BASE/backup/auth-hotfix-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$backup"
cp -p "$AUTH" "$backup/web-auth.sh"
cp -p "$NATIVE" "$backup/web-auth-native.sh"

auth_new="$AUTH.new.$$"
native_new="$NATIVE.new.$$"
cp -p "$AUTH" "$auth_new"
cp -p "$NATIVE" "$native_new"

if grep -F 'http://127.0.0.1:79|http://127.0.0.1:18079)' "$auth_new" >/dev/null 2>&1; then
    sed -i "s#http://127\\.0\\.0\\.1:79|http://127\\.0\\.0\\.1:18079)#http://$lan|http://127.0.0.1:79|http://127.0.0.1:18079)#" "$auth_new"
fi

if grep -F 'BRORAY_NATIVE_AUTH_SYSTEM_URL="${BRORAY_NATIVE_AUTH_SYSTEM_URL:-http://127.0.0.1:79}"' "$native_new" >/dev/null 2>&1; then
    sed -i "s#http://127\\.0\\.0\\.1:79}#http://$lan}#" "$native_new"
fi

if grep -F '[0-9A-Za-z_-]+[[:space:]\r]*$' "$native_new" >/dev/null 2>&1; then
    sed -i 's#\[0-9A-Za-z_-\]+#\[^[:space:]\]\[^[:space:]\]*#' "$native_new"
fi

/opt/bin/ash -n "$auth_new" || fail web-auth-syntax
/opt/bin/ash -n "$native_new" || fail native-auth-syntax

mv -f "$auth_new" "$AUTH"
mv -f "$native_new" "$NATIVE"

if LD_LIBRARY_PATH= LD_PRELOAD= "$INIT" restart; then
    echo "AUTH_HOTFIX=PASS LAN=$lan BACKUP=$backup"
    exit 0
fi

cp -p "$backup/web-auth.sh" "$AUTH"
cp -p "$backup/web-auth-native.sh" "$NATIVE"
LD_LIBRARY_PATH= LD_PRELOAD= "$INIT" restart >/dev/null 2>&1 || true
echo "AUTH_HOTFIX=ROLLBACK BACKUP=$backup" >&2
exit 1
