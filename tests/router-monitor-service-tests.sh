#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-monitor-20260915
RAM=/tmp/broray-311-monitor-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-MONITOR-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; echo BRORAY311-MONITOR-20260915 >"$RAM/TEST-OWNER"
export PATH="$T/app/bin:$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export LD_LIBRARY_PATH="$T/lib:/opt/lib" BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_STATE_ROOT="$T/state" BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_MONITOR_ROOT="$T/app" BRORAY_MONITOR_PATH="$PATH"
export BRORAY_MONITOR_BRORAY="$T/bin/current-address" BRORAY_MONITOR_MAINTENANCE="$T/bin/maintenance"
export BRORAY_MONITOR_INTERVAL=1
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/tmp" "$T/app/run" "$T/app/logs"
ln -s "$T/bin/jq" "$T/app/bin/jq"
printf '#!/opt/bin/ash\nexit 0\n' >"$T/bin/current-address"
printf '#!/opt/bin/ash\nexit 0\n' >"$T/bin/maintenance"
chmod 700 "$T/bin/current-address" "$T/bin/maintenance"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
service() { /opt/bin/ash "$T/app/bin/broray-service" connection-monitor "$@"; }
init_service() { /opt/bin/ash "$T/bin/S23broray-monitor" "$@"; }

service status-json >"$T/empty.json"
jq -e '.complete and .running==false' "$T/empty.json" >/dev/null
[ ! -e "$T/state" ]
pass empty_status_is_read_only

printf '99999999\n' >"$T/app/run/connection-monitor.pid"
for action in stop restart; do
  rc=0; init_service "$action" >"$T/legacy-$action.txt" 2>&1 || rc=$?
  [ "$rc" = 75 ]
  [ "$(cat "$T/app/run/connection-monitor.pid")" = 99999999 ]
done
service status-json >"$T/legacy-status.json"
jq -e '.complete==false and .running==null' "$T/legacy-status.json" >/dev/null
[ ! -e "$T/state/services/connection-monitor/identity.json" ]
cp "$T/app/run/connection-monitor.pid" "$T/legacy-pid-evidence"
rm "$T/app/run/connection-monitor.pid"
pass unconfirmed_identity_preserved_by_stop_and_restart

init_service start >"$T/start-1.json"
init_service check >"$T/init-check.json"
for n in 1 2 3 4 5; do [ ! -e "$T/app/run/connection-status.json" ] || break; sleep 1; done
jq -e '.available==false and .address==""' "$T/app/run/connection-status.json" >/dev/null
cp "$T/state/services/connection-monitor/identity.json" "$T/first-identity.json"
init_service stop >"$T/stop-1.json"
[ ! -e "$T/app/run/connection-monitor.pid" ]
pass real_monitor_cycle_and_cooperative_stop

service start >"$T/concurrent-a.json" & a=$!
service start >"$T/concurrent-b.json" & b=$!
wait "$a"; wait "$b"
jq -en --slurpfile a "$T/concurrent-a.json" --slurpfile b "$T/concurrent-b.json" '$a[0].ready and $b[0].ready and $a[0].pid==$b[0].pid' >/dev/null
printf '{"schemaVersion":1,"generation":"00000000000000000000000000000000"}\n' >"$T/state/services/connection-monitor/stop.json"
sleep 2
service status-json >"$T/old-stop.json"
jq -e '.complete and .ready and .running' "$T/old-stop.json" >/dev/null
service stop >"$T/stop-2.json"
pass concurrent_start_and_old_stop_generation

cat >"$T/bin/maintenance" <<'MAINTENANCE'
#!/opt/bin/ash
echo ready >"$BRORAY_ROOT/tmp/maintenance.ready"
for n in $(seq 1 45); do
  [ ! -e "$BRORAY_ROOT/tmp/maintenance.release" ] || exit 0
  sleep 1
done
exit 1
MAINTENANCE
service start >"$T/busy-start.json"
for n in 1 2 3 4 5; do [ ! -e "$T/app/tmp/maintenance.ready" ] || break; sleep 1; done
[ -e "$T/app/tmp/maintenance.ready" ]
rc=0; service stop >"$T/busy-stop.json" 2>&1 || rc=$?
[ "$rc" = 75 ]
service status-json >"$T/busy-state.json"
jq -e '.complete and .running and .state=="stopping"' "$T/busy-state.json" >/dev/null
: >"$T/app/tmp/maintenance.release"
service stop >"$T/released-stop.json"
pass busy_cycle_returns_pending_until_finished

cp "$T/app/bin/broray-connection-monitor" "$T/original-monitor"
cat >"$T/app/bin/broray-connection-monitor" <<'CRASH'
#!/opt/bin/ash
. "$BRORAY_ROOT/lib/service-lifecycle.sh"
broray_service_daemon_enter connection-monitor || exit $?
kill -KILL "$$"
CRASH
rc=0; /opt/bin/ash "$T/app/bin/broray-connection-monitor" >"$T/crash.txt" 2>&1 || rc=$?
[ "$rc" = 137 ]
cp "$T/state/services/connection-monitor/identity.json" "$T/crashed-identity.json"
cp "$T/original-monitor" "$T/app/bin/broray-connection-monitor"
service stop >"$T/retired.json"
service start >"$T/restarted.json"
service stop >"$T/final-stop.json"
jq -e '.complete and .running==false' "$T/final-stop.json" >/dev/null
[ ! -e "$T/app/run/connection-monitor.pid" ]
"$BRORAY_OPS_GUARD" "$T/state/services/connection-monitor/lifetime.guard" /opt/bin/ash -c ':'
pass crashed_monitor_retires_before_restart

jq -n --rawfile names "$T/passed.txt" '{status:"PASS",tests:($names|split("\n")|map(select(length>0))),environment:"physical Keenetic ARM64 real monitor, isolated files and harmless address source",applicationInstalled:false,persistentXrayStarted:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
