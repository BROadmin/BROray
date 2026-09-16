#!/opt/bin/ash
# CGI errors use shell exit zero. Translate the validated HTTP/JSON result to
# the job result without exposing any response before finalization.
set -u
[ "$#" = 2 ] || exit 64
. "${BRORAY_ROOT:-/opt/broray}/lib/route-job.sh" || exit 74
broray_route_worker_check >/dev/null || exit 73
"${BRORAY_OPS_ASH:-/opt/bin/ash}" "$1" >"$2" || exit 74
[ -s "$2" ] && [ ! -L "$2" ] || exit 74
body="$(sed 's/\r$//' "$2" | sed '1,/^$/d')" || exit 74
printf '%s\n' "$body" | jq -es 'length==1 and (.[0]|type=="object" and (.success|type)=="boolean")' >/dev/null || exit 74
status="$(sed -n '1,/^\r\{0,1\}$/p' "$2" | sed -n 's/^Status: \([0-9][0-9][0-9]\).*$/\1/p')"
case "$status" in ''|2[0-9][0-9]) ;; *) exit 1 ;; esac
printf '%s\n' "$body" | jq -e '.success==true' >/dev/null || exit 1
