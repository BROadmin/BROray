#!/opt/bin/ash
# Real production probe/supervisor, deterministic Xray and network fixtures.
set -eu
umask 077
T=/opt/tmp/broray-311-server-jobs-20260915
RAM=/tmp/broray-311-server-jobs-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-SERVER-JOBS-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; echo BRORAY311-SERVER-JOBS-20260915 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app"
export BRORAY_PROXY_HOST=127.0.0.1 BRORAY_PROXY_PORT=2080 BRORAY_INTERFACE=Proxy0
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_XRAY_BINARY="$T/app/bin/fixture-xray"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/tmp" "$T/app/config/system" "$T/app/bin" "$T/cases" "$T/app/run/server-quality"
ln -s "$T/bin/jq" "$T/app/bin/jq"
cat >"$T/app/bin/fixture-xray" <<'XRAY'
#!/opt/bin/ash
case " $* " in *' -test '*) exit 0 ;; esac
jq -r '.inbounds[0].port' "$3" >"$TEST_PORT"
echo $$ >"$TEST_XRAY_PID"
trap '' TERM
exec sleep 60
XRAY
cat >"$T/app/bin/netstat" <<'NETSTAT'
#!/opt/bin/ash
if [ -s "$TEST_PORT" ]; then printf 'tcp 0 0 127.0.0.1:%s 0.0.0.0:* LISTEN\n' "$(cat "$TEST_PORT")"; fi
NETSTAT
cat >"$T/app/bin/curl" <<'CURL'
#!/opt/bin/ash
case "$1" in --help) echo --socks5-hostname; exit 0 ;; esac
case "${TEST_MODE:-normal}" in
  wait) echo ready >"$TEST_READY"; trap '' TERM; sleep 60; exit 28 ;;
  error) exit 28 ;;
esac
printf '204 0.02'
CURL
cat >"$T/app/bin/ping" <<'PING'
#!/opt/bin/ash
printf 'rtt min/avg/max/mdev = 10/20/30/1 ms\n'
PING
chmod 700 "$T/app/bin/fixture-xray" "$T/app/bin/netstat" "$T/app/bin/curl" "$T/app/bin/ping"
printf '%s\n' '{"listenAddress":"127.0.0.1","socksPort":2080}' >"$T/app/config/system/settings.json"
printf '%s\n' 'vless://11111111-2222-4333-8444-555555555555@93.184.216.34:443?security=tls&type=tcp&sni=example.invalid#Fixture' >"$T/payload.txt"
export TEST_PAYLOAD="$T/payload.txt" TEST_MODE=normal
PATH="$T/app/bin:$PATH"; export PATH
. "$T/app/lib/server-service.sh"
. "$T/app/lib/server-import.sh"
broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription fixture 0 >/dev/null
SERVER=subscription-fixture-0000
QUALITY="$T/app/run/server-quality/$SERVER.json"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
case_dir() {
  R="$T/cases/$1"; mkdir -p "$R/state"
  export R BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
  export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
  export TEST_READY="$R/ready" TEST_PORT="$R/port" TEST_XRAY_PID="$R/xray-pid"
  TEST_MODE=normal
}
old_quality() {
  printf '%s\n' '{"successfulChecks":7,"failedChecks":2,"disconnects":0,"status":"available"}' >"$QUALITY"
  cp "$QUALITY" "$R/before.json"
}
terminal() {
  [ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ] || exit 1
  for file in "$R/state/operations/"*/state.json; do jq -e --arg state "$1" '.state==$state' "$file" >/dev/null; done
  for file in "$R/state/operations/"*/supervisors.json; do [ ! -e "$file" ] || jq -e '.supervisors==[]' "$file" >/dev/null; done
}
check_job() { /opt/bin/ash "$T/app/bin/broray-servers" check "$SERVER" "${1:-manual}"; }

case_dir unowned
old_quality; rc=0
broray_server_check "$SERVER" || rc=$?
[ "$rc" = 73 ]; cmp -s "$QUALITY" "$R/before.json"
pass unowned_service_cannot_write_quality

case_dir normal
old_quality
check_job >"$R/result.json" 2>"$R/error.txt"
jq -e '.success==true and .quality.successfulChecks==8 and .quality.failedChecks==2 and .quality.ping==20 and .quality.jitter==20' "$R/result.json" >/dev/null
terminal completed
[ ! -e "/proc/$(cat "$TEST_XRAY_PID")" ]
pass real_probe_result_committed_after_temporary_tree_drain

case_dir negative
old_quality; TEST_MODE=error; rc=0
check_job >"$R/result.json" 2>"$R/error.txt" || rc=$?
[ "$rc" = 1 ]
jq -e '.success==false and .quality.successfulChecks==7 and .quality.failedChecks==3' "$R/result.json" >/dev/null
terminal failed
pass negative_measurement_records_failed_job

case_dir cancel
old_quality; TEST_MODE=wait
check_job >"$R/result.json" 2>"$R/error.txt" & worker=$!
n=0; while [ ! -f "$TEST_READY" ]; do n=$((n+1)); [ "$n" -lt 200 ] || exit 1; /opt/bin/busybox usleep 100000; done
broray_ops_call status >"$R/status.json"
operation="$(jq -er '.operations[0].operationId' "$R/status.json")"
broray_ops_call cancel "$operation" >"$R/cancel.json"
rc=0; wait "$worker" || rc=$?
[ "$rc" = 130 ]; cmp -s "$QUALITY" "$R/before.json"
terminal aborted
[ ! -e "/proc/$(cat "$TEST_XRAY_PID")" ]
pass cancel_preserves_quality_and_drains_probe_tree

case_dir inherited
old_quality
broray_job_begin system servers:check servers USER cooperative
rc=0; ( broray_server_check "$SERVER" ) || rc=$?
[ "$rc" = 2 ]; cmp -s "$QUALITY" "$R/before.json"
broray_job_finish completed
pass inherited_owner_token_rejected

case_dir import
/opt/bin/ash "$T/app/bin/broray-servers" import "$(cat "$TEST_PAYLOAD")" >"$R/result.json" 2>"$R/error.txt"
jq -e '.imported==true' "$R/result.json" >/dev/null
terminal completed
pass manual_import_staged_then_committed

case_dir invalid_import
rc=0; /opt/bin/ash "$T/app/bin/broray-servers" import invalid >"$R/result.json" 2>"$R/error.txt" || rc=$?
[ "$rc" = 1 ]; terminal failed
pass invalid_import_releases_without_catalog_mutation

case_dir paused
old_quality
broray_ops_call pause >"$R/pause.json"
rc=0; check_job scheduled >"$R/result.json" 2>"$R/error.txt" || rc=$?
[ "$rc" = 76 ]; cmp -s "$QUALITY" "$R/before.json"
pass paused_automatic_check_does_not_measure

case_dir api_error
TEST_MODE=error
/opt/bin/ash -c '. "$BRORAY_ROOT/web-new/api/servers/common.sh"; broray_servers_api_lock check; broray_servers_api_run broray_server_check subscription-fixture-0000 manual' >"$R/http.txt" 2>"$R/error.txt"
grep -q '400 Bad Request' "$R/http.txt"
terminal failed
pass api_preserves_failed_result_despite_http_exit_zero

jq -n --rawfile tests "$T/passed.txt" '{status:"PASS",tests:($tests|split("\n")|map(select(length>0))),environment:"physical ARM64 production server jobs/probe with local Xray and network fixtures",applicationInstalled:false}' >"$T/RESULT.json"
