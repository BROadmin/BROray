#!/opt/bin/ash
set -eu
umask 077
BASE=/opt/broray
AUTH="$BASE/lib/web-auth.sh"
INIT=/opt/etc/init.d/S25broray-web
fail(){ echo "AUTH_HOTFIX=FAIL REASON=$1" >&2; exit 1; }
[ -f "$AUTH" ] && [ ! -L "$AUTH" ] || fail web-auth-unsafe
[ -x "$INIT" ] || fail init-missing
grep -F 'broray_keenetic_curl()' "$AUTH" >/dev/null 2>&1 || fail curl-helper-missing
grep -F 'broray_keenetic_authenticate()' "$AUTH" >/dev/null 2>&1 || fail auth-function-missing
! grep -F '# BEGIN BROray temporary LAN auth fallback v2' "$AUTH" >/dev/null 2>&1 || fail already-applied
LAN="$(sed -n '1p' "$BASE/run/lan-ip" 2>/dev/null || true)"
printf '%s\n' "$LAN" | awk -F. 'NF==4{for(i=1;i<=4;i++)if($i!~/^[0-9]+$/||$i<0||$i>255)exit 1;exit 0} {exit 1}' || fail lan-invalid
P="$BASE/tmp/auth-hotfix-probe.$$"; rm -rf "$P"; mkdir -p "$P" || fail probe-dir
trap 'rm -rf "$P"' EXIT HUP INT TERM
broray_probe(){ ( unset http_proxy https_proxy all_proxy no_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY; curl -q --noproxy '*' -sS --connect-timeout 5 --max-time 10 -D "$P/h" -o "$P/b" "http://$LAN/auth" >/dev/null 2>&1 || true; ); }
broray_probe
REALM="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Rr]ealm:[[:space:]]*//p' "$P/h" 2>/dev/null | tr -d '\r' | head -n1)"
CHAL="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Cc]hallenge:[[:space:]]*//p' "$P/h" 2>/dev/null | tr -d '\r' | head -n1)"
[ -n "$REALM" ] && [ -n "$CHAL" ] || fail lan-auth-unavailable
B="$BASE/backup/auth-hotfix-$(date +%Y%m%d-%H%M%S)"; mkdir -p "$B" || fail backup-dir; cp -p "$AUTH" "$B/web-auth.sh" || fail backup-auth
N="$AUTH.new.$$"; cp -p "$AUTH" "$N" || fail stage-copy
cat >>"$N" <<'EOF_HOTFIX'

# BEGIN BROray temporary LAN auth fallback v2
broray_keenetic_authenticate()
{
    local login password lan d h b c p realm challenge md5 response code
    login="$1"; password="$2"
    lan="$(sed -n '1p' "$BRORAY_BASE/run/lan-ip" 2>/dev/null || true)"
    printf '%s\n' "$lan" | awk -F. 'NF==4{for(i=1;i<=4;i++)if($i!~/^[0-9]+$/||$i<0||$i>255)exit 1;exit 0} {exit 1}' || { password=''; return 2; }
    d="$BRORAY_BASE/run/web-new/auth-lan.$$"; h="$d/h"; b="$d/b"; c="$d/c"; p="$d/p"
    rm -rf "$d" 2>/dev/null || true; mkdir -p "$d" || { password=''; return 2; }; chmod 700 "$d" || { rm -rf "$d"; password=''; return 2; }
    broray_keenetic_curl --silent --show-error --connect-timeout 5 --max-time 10 --dump-header "$h" --output "$b" --cookie-jar "$c" "http://$lan/auth" >/dev/null 2>&1 || true
    realm="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Rr]ealm:[[:space:]]*//p' "$h" 2>/dev/null | tr -d '\r' | head -n1)"
    challenge="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Cc]hallenge:[[:space:]]*//p' "$h" 2>/dev/null | tr -d '\r' | head -n1)"
    [ -n "$realm" ] && [ -n "$challenge" ] || { rm -rf "$d"; password=''; return 2; }
    md5="$(printf '%s' "$login:$realm:$password" | md5sum | awk '{print $1}')"
    response="$(printf '%s' "$challenge$md5" | sha256sum | awk '{print $1}')"
    jq -n --arg login "$login" --arg password "$response" '{login:$login,password:$password}' >"$p" || { rm -rf "$d"; password=''; return 2; }
    code="$(broray_keenetic_curl --silent --show-error --connect-timeout 5 --max-time 10 --cookie "$c" --cookie-jar "$c" --header 'Content-Type: application/json' --request POST --data-binary "@$p" --output "$b" --write-out '%{http_code}' "http://$lan/auth" 2>/dev/null || printf '000')"
    password=''; md5=''; response=''; rm -rf "$d"
    case "$code" in 200) return 0 ;; 401|403) return 1 ;; *) return 2 ;; esac
}
# END BROray temporary LAN auth fallback v2
EOF_HOTFIX
/opt/bin/ash -n "$N" || { rm -f "$N"; fail syntax; }
mv -f "$N" "$AUTH" || { rm -f "$N"; fail install; }
if LD_LIBRARY_PATH= LD_PRELOAD= "$INIT" restart; then rm -rf "$P"; trap - EXIT HUP INT TERM; echo "AUTH_HOTFIX=PASS LAN=$LAN BACKUP=$B"; exit 0; fi
cp -p "$B/web-auth.sh" "$AUTH" || fail rollback-copy
LD_LIBRARY_PATH= LD_PRELOAD= "$INIT" restart >/dev/null 2>&1 || true
fail restart-rollback
