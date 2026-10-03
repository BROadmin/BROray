#!/opt/bin/ash
set -eu
umask 077
B=/opt/broray
A="$B/lib/web-auth.sh"
M='# BROray support auth fallback v4'
f(){ echo "AUTH_HOTFIX=FAIL REASON=$1" >&2; exit 1; }
[ -x /opt/bin/curl ] || f curl-missing
[ -f "$A" ] && [ ! -L "$A" ] || f web-auth-unsafe
grep -F 'broray_session_create' "$A" >/dev/null 2>&1 || f session-helper-missing
grep -F "$M" "$A" >/dev/null 2>&1 && { echo AUTH_HOTFIX=PASS ALREADY_APPLIED; exit 0; }
L="$(sed -n '1p' "$B/run/lan-ip" 2>/dev/null || true)"
printf '%s\n' "$L" | awk -F. 'NF==4{for(i=1;i<=4;i++)if($i!~/^[0-9]+$/||$i<0||$i>255)exit 1;exit 0}{exit 1}' || f lan-invalid
D="$B/tmp/auth-hotfix.$$"; rm -rf "$D"; mkdir -p "$D" || f tmp; trap 'rm -rf "$D"' EXIT
probe(){ U="$1"; K="$2"; rm -f "$D/h" "$D/b" "$D/c"; [ "$K" = 1 ] && X=-k || X=''; ( unset http_proxy https_proxy all_proxy no_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY; /opt/bin/curl -q --noproxy '*' $X -sS --connect-timeout 5 --max-time 10 -D "$D/h" -o "$D/b" -c "$D/c" "$U/auth" >/dev/null 2>&1 || true ); R="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Rr]ealm:[[:space:]]*//p' "$D/h" 2>/dev/null|tr -d '\r'|head -n1)"; C="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Cc]hallenge:[[:space:]]*//p' "$D/h" 2>/dev/null|tr -d '\r'|head -n1)"; [ -n "$R" ] && [ -n "$C" ]; }
if probe "http://$L" 0; then U="http://$L"; K=0; elif probe "https://$L" 1; then U="https://$L"; K=1; else f lan-auth-challenge-unavailable; fi
S="$B/backup/auth-hotfix-$(date +%Y%m%d-%H%M%S)"; mkdir -p "$S" || f backup-dir; cp -p "$A" "$S/web-auth.sh" || f backup
N="$A.new.$$"; cp -p "$A" "$N" || f stage
printf '\n%s\nBRORAY_SUPPORT_AUTH_URL=%s\nBRORAY_SUPPORT_AUTH_TLS=%s\n' "$M" "$(printf %s "$U"|sed "s/'/'\\\\''/g"|awk '{printf "\047%s\047",$0}')" "$(printf %s "$K"|awk '{printf "\047%s\047",$0}')" >>"$N"
cat >>"$N" <<'EOS'
broray_keenetic_authenticate(){
  l="$1"; p="$2"; d="${BRORAY_BASE:-/opt/broray}/run/web-new/auth-support.$$"; rm -rf "$d"; mkdir -p "$d" || return 2; chmod 700 "$d" || { rm -rf "$d"; return 2; }; [ "$BRORAY_SUPPORT_AUTH_TLS" = 1 ] && x=-k || x='';
  ( unset http_proxy https_proxy all_proxy no_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY; /opt/bin/curl -q --noproxy '*' $x -sS --connect-timeout 5 --max-time 10 -D "$d/h" -o "$d/b" -c "$d/c" "$BRORAY_SUPPORT_AUTH_URL/auth" >/dev/null 2>&1 || true );
  r="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Rr]ealm:[[:space:]]*//p' "$d/h" 2>/dev/null|tr -d '\r'|head -n1)"; c="$(sed -n 's/^[Xx]-[Nn][Dd][Mm]-[Cc]hallenge:[[:space:]]*//p' "$d/h" 2>/dev/null|tr -d '\r'|head -n1)"; [ -n "$r" ] && [ -n "$c" ] || { rm -rf "$d"; p=''; return 2; };
  m="$(printf %s "$l:$r:$p"|md5sum|awk '{print $1}')"; q="$(printf %s "$c$m"|sha256sum|awk '{print $1}')"; jq -n --arg login "$l" --arg password "$q" '{login:$login,password:$password}' >"$d/p" || { rm -rf "$d"; p=''; return 2; };
  z="$( ( unset http_proxy https_proxy all_proxy no_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY; /opt/bin/curl -q --noproxy '*' $x -sS --connect-timeout 5 --max-time 10 -b "$d/c" -c "$d/c" -H 'Content-Type: application/json' -X POST --data-binary "@$d/p" -o "$d/b" -w '%{http_code}' "$BRORAY_SUPPORT_AUTH_URL/auth" ) 2>/dev/null || printf 000 )"; p=''; m=''; q=''; rm -rf "$d"; case "$z" in 200)return 0;;401|403)return 1;;*)return 2;;esac
}
EOS
/opt/bin/ash -n "$N" || { rm -f "$N"; f syntax; }
mv -f "$N" "$A" || { rm -f "$N"; f install; }
rm -rf "$D"; trap - EXIT
printf 'AUTH_HOTFIX=PASS ENDPOINT=%s BACKUP=%s\n' "$U" "$S"
