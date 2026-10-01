#!/opt/bin/ash
set -eu
umask 077
BASE=/opt/broray
AUTH="$BASE/lib/web-auth.sh"
MARK="# BEGIN BROray temporary LAN auth fallback v3"
fail(){ echo "AUTH_HOTFIX=FAIL REASON=$1" >&2; exit 1; }
[ -f "$AUTH" ] && [ ! -L "$AUTH" ] || fail web-auth-unsafe
grep -F 'broray_keenetic_authenticate()' "$AUTH" >/dev/null 2>&1 || fail auth-function-missing
! grep -F "$MARK" "$AUTH" >/dev/null 2>&1 || { echo "AUTH_HOTFIX=PASS ALREADY_APPLIED"; exit 0; }
LAN="$(sed -n '1p' "$BASE/run/lan-ip" 2>/dev/null || true)"
printf '%s\n' "$LAN" | awk -F. 'NF==4{for(i=1;i<=4;i++)if($i!~/^[0-9]+$/||$i<0||$i>255)exit 1;exit 0}{exit 1}' || fail lan-invalid
P="$BASE/tmp/auth-hotfix-probe.$$"; rm -rf "$P"; mkdir -p "$P" || fail probe-dir
trap 'rm -rf "$P"' EXIT HUP INT TERM
probe(){
  U="$1"; K="$2"; rm -f "$P/h" "$P/b";
  if [ "$K" = 1 ]; then TLS="-k"; else TLS=""; fi;
  ( unset http_proxy https_proxy all_proxy no_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY; /opt/bin/curl -q --noproxy '*' $TLS -sS --connect-timeout 5 --max-time 10 -D "$P/h" -o "$P/b" "$U/auth" >/dev/null 2>&1 || true; );
  R="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Rr]ealm:[[:space:]]*//p' "$P/h" 2>/dev/null | tr -d '\r' | head -n1)";
  C="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Cc]hallenge:[[:space:]]*//p' "$P/h" 2>/dev/null | tr -d '\r' | head -n1)";
  [ -n "$R" ] && [ -n "$C" ];
}
[ -x /opt/bin/curl ] || fail curl-missing
ENDPOINT=""; INSECURE=0
if probe "http://$LAN" 0; then ENDPOINT="http://$LAN";
elif probe "https://$LAN" 1; then ENDPOINT="https://$LAN"; INSECURE=1;
else fail lan-auth-unavailable; fi
B="$BASE/backup/auth-hotfix-$(date +%Y%m%d-%H%M%S)"; mkdir -p "$B" || fail backup-dir; cp -p "$AUTH" "$B/web-auth.sh" || fail backup-auth
N="$AUTH.new.$$"; cp -p "$AUTH" "$N" || fail stage-copy
cat >>"$N" <<'EOF_HOTFIX'

# BEGIN BROray temporary LAN auth fallback v3
broray_hotfix_curl()
{
    (
        unset http_proxy https_proxy all_proxy no_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY
        /opt/bin/curl -q --noproxy '*' "$@"
    )
}

broray_keenetic_authenticate()
{
    local login password base lan endpoint insecure d h b c p realm challenge md5 response code tls
    login="$1"; password="$2"; base="${BRORAY_BASE:-/opt/broray}"
    lan="$(sed -n '1p' "$base/run/lan-ip" 2>/dev/null || true)"
    printf '%s\n' "$lan" | awk -F. 'NF==4{for(i=1;i<=4;i++)if($i!~/^[0-9]+$/||$i<0||$i>255)exit 1;exit 0}{exit 1}' || { password=""; return 2; }
    endpoint="http://$lan"; insecure=0
    d="$base/run/web-new/auth-lan.$$"; h="$d/h"; b="$d/b"; c="$d/c"; p="$d/p"
    rm -rf "$d" 2>/dev/null || true; mkdir -p "$d" || { password=""; return 2; }; chmod 700 "$d" || { rm -rf "$d"; password=""; return 2; }
    broray_hotfix_curl --silent --show-error --connect-timeout 5 --max-time 10 --dump-header "$h" --output "$b" --cookie-jar "$c" "$endpoint/auth" >/dev/null 2>&1 || true
    realm="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Rr]ealm:[[:space:]]*//p' "$h" 2>/dev/null | tr -d '\r' | head -n1)"
    challenge="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Cc]hallenge:[[:space:]]*//p' "$h" 2>/dev/null | tr -d '\r' | head -n1)"
    if [ -z "$realm" ] || [ -z "$challenge" ]; then
        rm -f "$h" "$b" "$c"; endpoint="https://$lan"; insecure=1
        broray_hotfix_curl -k --silent --show-error --connect-timeout 5 --max-time 10 --dump-header "$h" --output "$b" --cookie-jar "$c" "$endpoint/auth" >/dev/null 2>&1 || true
        realm="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Rr]ealm:[[:space:]]*//p' "$h" 2>/dev/null | tr -d '\r' | head -n1)"
        challenge="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Cc]hallenge:[[:space:]]*//p' "$h" 2>/dev/null | tr -d '\r' | head -n1)"
    fi
    [ -n "$realm" ] && [ -n "$challenge" ] || { rm -rf "$d"; password=""; return 2; }
    md5="$(printf '%s' "$login:$realm:$password" | md5sum | awk '{print $1}')"
    response="$(printf '%s' "$challenge$md5" | sha256sum | awk '{print $1}')"
    jq -n --arg login "$login" --arg password "$response" '{login:$login,password:$password}' >"$p" || { rm -rf "$d"; password=""; return 2; }
    if [ "$insecure" = 1 ]; then tls="-k"; else tls=""; fi
    code="$(broray_hotfix_curl $tls --silent --show-error --connect-timeout 5 --max-time 10 --cookie "$c" --cookie-jar "$c" --header 'Content-Type: application/json' --request POST --data-binary "@$p" --output "$b" --write-out '%{http_code}' "$endpoint/auth" 2>/dev/null || printf '000')"
    password=""; md5=""; response=""; rm -rf "$d"
    case "$code" in 200) return 0 ;; 401|403) return 1 ;; *) return 2 ;; esac
}
# END BROray temporary LAN auth fallback v3
EOF_HOTFIX
/opt/bin/ash -n "$N" || { rm -f "$N"; fail syntax; }
mv -f "$N" "$AUTH" || { rm -f "$N"; fail install; }
rm -rf "$P"; trap - EXIT HUP INT TERM
echo "AUTH_HOTFIX=PASS ENDPOINT=$ENDPOINT BACKUP=$B"
exit 0
