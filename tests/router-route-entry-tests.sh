#!/opt/bin/ash
# Harmless backends in a private prefix; never invokes ndmc or installed CLI.
set -eu
umask 077
T=/opt/tmp/broray-311-route-entry-20260916
RAM=/tmp/broray-311-route-entry-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-ROUTE-ENTRY-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-ROUTE-ENTRY-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_ROUTES_ROOT="$T/app/routes"
export BRORAY_STATE_ROOT="$T/state" BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock"
export BRORAY_OPS_UPDATER_ROOT="$T/updater"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
unset BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN BRORAY_BACKGROUND_LAUNCH_NONCE
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/state" "$T/app/routes/operations" "$T/app/routes/manifests" "$T/app/routes/state" "$T/app/tmp"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
idle() {
  [ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
  for f in "$T/state/operations/"*/state.json; do
    [ -e "$f" ] || continue
    jq -e '.running==false' "$f" >/dev/null
  done
}
cat >"$T/app/lib/routes-download.sh" <<'FIXTURE'
broray_routes_check_run() {
  test -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" || return 93
  jq -e '.scope=="routes" and .cancelability=="protected" and .running' \
    "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json" >/dev/null || return 94
  echo CHANGED >"$BRORAY_ROOT/changed"
}
FIXTURE
mkdir "$T/global.lock"; echo KEEP >"$T/global.lock/sentinel"
rc=0; /opt/bin/ash "$T/app/bin/broray-routes" check fixture >"$T/conflict.out" 2>"$T/conflict.err" || rc=$?
[ "$rc" = 2 ] && [ ! -e "$T/app/changed" ]
[ "$(cat "$T/global.lock/sentinel")" = KEEP ]
# Retire only this test's known one-file fixture, not an application lock.
rm "$T/global.lock/sentinel"; rmdir "$T/global.lock"
pass cli_preserves_foreign_fence
/opt/bin/ash "$T/app/bin/broray-routes" check fixture >"$T/cli.out" 2>"$T/cli.err"
[ "$(cat "$T/app/changed")" = CHANGED ]; idle
pass cli_records_protected_owner_and_releases_after_drain
cat >"$T/app/lib/routes-download.sh" <<'FIXTURE'
broray_routes_check_run() {
  /opt/bin/ash -c 'sleep 2; echo LATE >"$BRORAY_ROOT/late"' >/dev/null 2>&1 &
  return 0
}
FIXTURE
/opt/bin/ash "$T/app/bin/broray-routes" check fixture >"$T/detached.out" 2>"$T/detached.err"
sleep 3; [ ! -e "$T/app/late" ]; idle
pass detached_writer_cannot_outlive_command
# The original CGI/auth files have only their /opt/broray prefix translated.
# Authentication is a local fixture; this is not HTTP/session acceptance.
for file in "$T/app/web-new/api/auth-common.sh" "$T/app/web-new/api/routes/"*; do
  [ -f "$file" ] || continue
  sed "s|/opt/broray|$T/app|g" "$file" >"$file.prefix"
  mv "$file.prefix" "$file"
done
cat >"$T/app/lib/web-auth.sh" <<'FIXTURE'
broray_cookie_value() { printf fixture; }
broray_session_validate() { [ "${TEST_AUTH:-yes}" = yes ]; }
FIXTURE
echo '{"schemaVersion":1,"bundles":["fixture"]}' >"$T/app/routes/bundles.json"
echo '{"managedInterface":"Proxy0"}' >"$T/app/routes/config.json"
echo '{"id":"fixture","targetInterface":"Proxy0","exportComment":"BROray"}' >"$T/app/routes/manifests/fixture.json"
echo '{"schemaVersion":1,"bundleId":"fixture","status":"available"}' >"$T/app/routes/state/fixture.json"
export REQUEST_METHOD=POST QUERY_STRING=bundleId=fixture
cat >"$T/app/lib/routes-download.sh" <<'FIXTURE'
broray_routes_check_run() { test -n "${BRORAY_BACKGROUND_OPERATION_ID:-}"; }
FIXTURE
/opt/bin/ash "$T/app/web-new/api/routes/check.cgi" >"$T/web-check.reply" 2>"$T/web-check.err"
sed 's/\r$//' "$T/web-check.reply" | sed '1,/^$/d' | jq -e '.success==true' >/dev/null
idle; pass web_and_cli_share_protected_job
cat >"$T/app/lib/routes-download.sh" <<'FIXTURE'
broray_routes_check_run() { return 42; }
FIXTURE
/opt/bin/ash "$T/app/web-new/api/routes/check.cgi" >"$T/web-failed.reply" 2>"$T/web-failed.err"
grep -q '^Status: 502 ' "$T/web-failed.reply"
sed 's/\r$//' "$T/web-failed.reply" | sed '1,/^$/d' | jq -e '.success==false' >/dev/null
jq -s -e 'map(select(.state=="failed"))|length==1' "$T/state/operations/"*/state.json >/dev/null
idle; pass http_error_records_failed_job
cat >"$T/app/lib/routes-user-import.sh" <<'FIXTURE'
broray_user_routes_cleanup() { :; }
broray_user_routes_preview() { test -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" || return 93; cat "$1"; }
FIXTURE
echo '{"label":"Fixture request","input":["192.0.2.0/24"]}' >"$T/body.json"
CONTENT_LENGTH="$(wc -c <"$T/body.json")"; export CONTENT_LENGTH
/opt/bin/ash "$T/app/web-new/api/routes/custom-preview.cgi" <"$T/body.json" >"$T/web-preview.reply" 2>"$T/web-preview.err"
sed 's/\r$//' "$T/web-preview.reply" | sed '1,/^$/d' | jq -e --slurpfile body "$T/body.json" '.success==true and .data==$body[0]' >/dev/null
idle; pass custom_preview_body_survives_supervised_replay
# Each command finished through helpers-drain; no supervisor registrations
# may remain before the host archives and retires this private prefix.
for file in "$T/state/operations/"*/supervisors.json; do
  [ -e "$file" ] || continue
  jq -e '.supervisors|length==0' "$file" >/dev/null
done
jq -n --rawfile tests "$T/passed.txt" '{status:"PASS",tests:($tests|split("\n")|map(select(length>0))),environment:"physical ARM, actual proc/ptrace, private prefix and harmless backends",applicationInstalled:false,routerRoutesModified:false,httpSessionAcceptance:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
