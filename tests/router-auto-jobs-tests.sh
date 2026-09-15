#!/opt/bin/ash
# Real production probe/supervisor, deterministic Xray and network fixtures.
set -eu
umask 077
T=/opt/tmp/broray-311-auto-jobs-20260915
RAM=/tmp/broray-311-auto-jobs-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-AUTO-JOBS-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-AUTO-JOBS-20260915 >"$RAM/TEST-OWNER"
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
  export BRORAY_GLOBAL_LOCK="$R/global.lock" BRORAY_UPDATER_LOCK="$R/updater/request.lock" BRORAY_SYSTEM_LOCK="$R/legacy.lock"
  rm -f "$T/app/run/server-auto-switch-state.json"
  printf '%s\n' '{"enabled":false,"failureThreshold":1,"cooldownMinutes":10,"minimumRating":"acceptable","selectionRule":"best-quality","qualityRefreshEnabled":false,"qualityRefreshIntervalMinutes":60}' >"$T/app/config/system/server-auto-switch.json"
}
old_quality() {
  printf '%s\n' '{"successfulChecks":7,"failedChecks":2,"disconnects":0,"status":"available"}' >"$QUALITY"
  cp "$QUALITY" "$R/before.json"
}
terminal() {
  [ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
  for file in "$R/state/operations/"*/state.json; do jq -e --arg state "$1" '.state==$state' "$file" >/dev/null; done
  for file in "$R/state/operations/"*/supervisors.json; do [ ! -e "$file" ] || jq -e '.supervisors==[]' "$file" >/dev/null; done
}
check_job() { /opt/bin/ash "$T/app/bin/broray-servers" check "$SERVER" "${1:-manual}"; }


auto_state="$T/app/run/server-auto-switch-state.json"
auto_config="$T/app/config/system/server-auto-switch.json"
auto_once() { /opt/bin/ash "$T/app/bin/broray-server-auto-switch" --once; }
quality_on() { jq '.qualityRefreshEnabled=true' "$auto_config" >"$R/config.json"; cp "$R/config.json" "$auto_config"; }

case_dir paused
quality_on; old_quality
broray_ops_call pause >"$R/pause.json"
auto_once >"$R/out" 2>"$R/err"
[ ! -e "$auto_state" ]; cmp -s "$QUALITY" "$R/before.json"
[ ! -e "$R/global.lock" ]; [ ! -e "$R/state/operations" ] || [ -z "$(ls -A "$R/state/operations")" ]
pass pause_prevents_automatic_cycle

case_dir disabled
auto_once >"$R/out" 2>"$R/err"
jq -e '.status=="disabled" and .qualityRefresh.status=="disabled" and (.backgroundOperationId|length)>0' "$auto_state" >/dev/null
cp "$auto_state" "$R/before.json"
auto_once >>"$R/out" 2>>"$R/err"
cmp -s "$auto_state" "$R/before.json"
[ "$(find "$R/state/operations" -name state.json | wc -l)" = 1 ]
terminal completed
pass disabled_cycle_publishes_once_then_stays_idle

case_dir quality
quality_on; old_quality
auto_once >"$R/out" 2>"$R/err"
jq -e '.qualityRefresh.status=="success" and .qualityRefresh.availableCount==1 and .qualityRefresh.checkedCount==1' "$auto_state" >/dev/null
jq -e '.successfulChecks==8' "$QUALITY" >/dev/null
auto_once >>"$R/out" 2>>"$R/err"
[ "$(find "$R/state/operations" -name state.json | wc -l)" = 1 ]
terminal completed
[ ! -e "/proc/$(cat "$TEST_XRAY_PID")" ]
pass due_quality_batch_uses_one_owner_and_drains_probe

case_dir negative
quality_on; old_quality; TEST_MODE=error
auto_once >"$R/out" 2>"$R/err"
jq -e '.qualityRefresh.status=="success" and .qualityRefresh.unavailableCount==1 and .qualityRefresh.errorCount==0' "$auto_state" >/dev/null
terminal completed
pass unavailable_server_is_complete_negative_measurement

case_dir cancel
quality_on; old_quality; TEST_MODE=wait
auto_once >"$R/out" 2>"$R/err" & worker=$!
n=0; while [ ! -f "$TEST_READY" ]; do n=$((n+1)); [ "$n" -lt 250 ] || exit 1; /opt/bin/busybox usleep 100000; done
broray_ops_call status >"$R/status.json"
operation="$(jq -er '.operations[0].operationId' "$R/status.json")"
broray_ops_call cancel "$operation" >"$R/cancel.json"
rc=0; wait "$worker" || rc=$?
[ "$rc" = 130 ]; cmp -s "$QUALITY" "$R/before.json"
terminal aborted
[ ! -e "/proc/$(cat "$TEST_XRAY_PID")" ]
cp "$auto_state" "$R/cached.json"
. "$T/app/lib/auto-switch-status.sh"
broray_auto_switch_public_state "$auto_state" >"$R/projected.json"
jq -e '.backgroundOperationState=="aborted" and .qualityRefresh.status=="error"' "$R/projected.json" >/dev/null
cmp -s "$auto_state" "$R/cached.json"
pass cancellation_drains_helpers_and_status_read_projects_terminal_job

case_dir legacy
mkdir "$T/app/run/server-auto-switch-cycle.lock"
echo KEEP >"$T/app/run/server-auto-switch-cycle.lock/foreign"
rc=0; auto_once >"$R/out" 2>"$R/err" || rc=$?
[ "$rc" = 2 ]; [ "$(cat "$T/app/run/server-auto-switch-cycle.lock/foreign")" = KEEP ]
[ ! -e "$auto_state" ]
# Only this case's explicitly-created fixture is retired.
rm "$T/app/run/server-auto-switch-cycle.lock/foreign"
rmdir "$T/app/run/server-auto-switch-cycle.lock"
pass ambiguous_legacy_cycle_lock_preserved

case_dir manual_off
jq '.enabled=true' "$auto_config" >"$R/config.json"; cp "$R/config.json" "$auto_config"
auto_once >"$R/out" 2>"$R/err"
jq -e '.status=="manual-off"' "$auto_state" >/dev/null
[ ! -e "$T/app/config/active-server" ]; terminal completed
pass manual_vpn_off_is_preserved

jq -n --rawfile tests "$T/passed.txt" '{status:"PASS",tests:($tests|split("\n")|map(select(length>0))),environment:"physical ARM64 production automatic cycle and probe with local transport fixtures",applicationInstalled:false}' >"$T/RESULT.json"
