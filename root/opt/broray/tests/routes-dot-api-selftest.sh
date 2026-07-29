#!/bin/sh
set -eu
SOURCE_ROOT="${BRORAY_ROOT:-/opt/broray}"
WORK="${TMPDIR:-/tmp}/broray-routes-dot-api-selftest-$$"
ROOT="$WORK/root"
cleanup(){ rm -rf "$WORK"; }
fail(){ echo "BROray DNS-over-TLS API self-test: FAIL — $*" >&2; exit 1; }
trap cleanup EXIT HUP INT TERM
mkdir -p "$ROOT/bin" "$ROOT/lib" "$ROOT/run" "$ROOT/tmp" "$ROOT/web-new/api/routes"
cp "$SOURCE_ROOT/lib/routes-api-operation.sh" "$ROOT/lib/"
cp "$SOURCE_ROOT/web-new/api/routes/dot-common.sh" "$ROOT/web-new/api/routes/"
for name in dot-status.cgi dot-test.cgi dot-apply.cgi dot-delete.cgi; do cp "$SOURCE_ROOT/web-new/api/routes/$name" "$ROOT/web-new/api/routes/$name"; done
cat >"$ROOT/web-new/api/auth-common.sh" <<'AUTH'
broray_api_require_method(){ [ "${REQUEST_METHOD:-GET}" = "$1" ] || exit 91; }
broray_api_require_session(){ return 0; }
broray_api_error(){ status="$1"; code="$2"; message="$3"; details="${4:-}"; jq -cn --arg status "$status" --arg code "$code" --arg message "$message" --arg details "$details" '{success:false,status:$status,data:null,error:{code:$code,message:$message,details:(if $details=="" then null else $details end)}}'; exit 0; }
broray_api_success(){ printf '%s\n' "$1" | jq '{success:true,data:.,error:null}'; exit 0; }
AUTH
cat >"$ROOT/bin/broray-routes-dot" <<'CLI'
#!/bin/sh
case "${1:-}" in
 status) echo '{"schemaVersion":1,"installed":false,"servers":[],"actual":{"dot":[],"dohCount":0,"totalSecure":0}}' ;;
 test|apply)
   [ -r "${2:-}" ] || exit 2
   if [ "${DOT_FAKE_ERROR:-}" = confirm ]; then echo 'BRORAY_ERROR:DOT_TEST_CONFIRMATION_REQUIRED:Требуется подтверждение.' >&2; exit 1; fi
   jq -n --arg action "$1" --slurpfile request "$2" '{schemaVersion:1,action:$action,serverIds:$request[0].serverIds}' ;;
 delete) echo '{"schemaVersion":1,"action":"delete","managed":[]}' ;;
 *) exit 2 ;;
esac
CLI
chmod 755 "$ROOT/bin/broray-routes-dot" "$ROOT/web-new/api/routes"/*.cgi

output="$(BRORAY_ROOT="$ROOT" REQUEST_METHOD=GET /opt/bin/ash "$ROOT/web-new/api/routes/dot-status.cgi")"
printf '%s\n' "$output" | jq -e '.success==true and .data.schemaVersion==1' >/dev/null || fail "status CGI"

body='{"serverIds":["google-primary","cloudflare-primary"],"allowUntested":false}'
output="$(printf '%s' "$body" | BRORAY_ROOT="$ROOT" REQUEST_METHOD=POST CONTENT_LENGTH="${#body}" /opt/bin/ash "$ROOT/web-new/api/routes/dot-test.cgi")"
printf '%s\n' "$output" | jq -e '.success==true and .data.action=="test" and (.data.serverIds|length)==2' >/dev/null || fail "test CGI"
[ ! -d "$ROOT/run/global-operation.lock" ] || fail "блокировка test не освобождена"

mkdir -p "$ROOT/run/global-operation.lock"
printf '%s\n' "$$" >"$ROOT/run/global-operation.lock/pid"
printf '%s\n' routes >"$ROOT/run/global-operation.lock/scope"
printf '%s\n' export >"$ROOT/run/global-operation.lock/action"
printf '%s\n' telegram >"$ROOT/run/global-operation.lock/bundle"
printf '%s\n' now >"$ROOT/run/global-operation.lock/startedAt"
output="$(printf '%s' "$body" | BRORAY_ROOT="$ROOT" REQUEST_METHOD=POST CONTENT_LENGTH="${#body}" /opt/bin/ash "$ROOT/web-new/api/routes/dot-apply.cgi")"
printf '%s\n' "$output" | jq -e '.success==false and .error.code=="ROUTES_OPERATION_BUSY"' >/dev/null || fail "конфликтующая операция не отклонена"
rm -rf "$ROOT/run/global-operation.lock"

output="$(printf '%s' "$body" | DOT_FAKE_ERROR=confirm BRORAY_ROOT="$ROOT" REQUEST_METHOD=POST CONTENT_LENGTH="${#body}" /opt/bin/ash "$ROOT/web-new/api/routes/dot-apply.cgi")"
printf '%s\n' "$output" | jq -e '.success==false and .error.code=="DOT_TEST_CONFIRMATION_REQUIRED" and .status=="409 Conflict"' >/dev/null || fail "ошибка подтверждения отображена неверно"

echo 'BROray DNS-over-TLS API self-test: PASS'
