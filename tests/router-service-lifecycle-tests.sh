#!/opt/bin/ash
# Isolated real daemons; automation is disabled, no Xray runtime is started.
set -eu
umask 077
T=/opt/tmp/broray-311-services-20260915
RAM=/tmp/broray-311-services-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-SERVICES-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; echo BRORAY311-SERVICES-20260915 >"$RAM/TEST-OWNER"
export PATH="$T/app/bin:$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export LD_LIBRARY_PATH="$T/lib:/opt/lib" BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_STATE_ROOT="$T/state" BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_GLOBAL_LOCK="$T/global.lock"
export BRORAY_OPS_UPDATER_ROOT="$T/updater" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock"
export BRORAY_UPDATER_LOCK="$T/updater/request.lock" BRORAY_SYSTEM_LOCK="$T/legacy.lock"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/tmp" "$T/app/run" "$T/app/logs" "$T/app/config/subscriptions" "$T/app/config/system"
ln -s "$T/bin/jq" "$T/app/bin/jq"
printf '%s\n' '{"enabled":false,"qualityRefreshEnabled":false}' >"$T/app/config/system/server-auto-switch.json"
printf '%s\n' '{"status":"disabled","qualityRefresh":{"status":"disabled"}}' >"$T/app/run/server-auto-switch-state.json"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
service() { /opt/bin/ash "$T/app/bin/broray-service" "$@"; }

service subscriptions status-json >"$T/empty-status.json"
jq -e '.complete and .running==false' "$T/empty-status.json" >/dev/null
[ ! -e "$T/state" ]
pass status_is_read_only

printf '99999999\n' >"$T/app/run/subscription-scheduler.pid"
printf '123456\n' >"$T/app/run/subscription-scheduler.starttime"
service subscriptions status-json >"$T/legacy-status.json"
jq -e '.complete==false and .running==null' "$T/legacy-status.json" >/dev/null
rc=0; service subscriptions stop >"$T/legacy-stop.txt" 2>&1 || rc=$?
[ "$rc" = 75 ]
[ "$(cat "$T/app/run/subscription-scheduler.pid")" = 99999999 ]
[ "$(cat "$T/app/run/subscription-scheduler.starttime")" = 123456 ]
cp "$T/app/run/subscription-scheduler.pid" "$T/legacy-pid-evidence"
cp "$T/app/run/subscription-scheduler.starttime" "$T/legacy-start-evidence"
rm "$T/app/run/subscription-scheduler.pid" "$T/app/run/subscription-scheduler.starttime"
pass unconfirmed_legacy_preserved

service subscriptions start >"$T/start-1.json"
cp "$T/state/services/subscriptions/identity.json" "$T/first-identity.json"
service subscriptions status-json >"$T/running.json"
jq -e '.complete and .running and .ready' "$T/running.json" >/dev/null
service subscriptions stop >"$T/stop-1.json"
jq -e '.complete and .running==false' "$T/stop-1.json" >/dev/null
[ ! -e "$T/app/run/subscription-scheduler.pid" ]
pass real_scheduler_start_stop

service subscriptions start >"$T/concurrent-a.json" & a=$!
service subscriptions start >"$T/concurrent-b.json" & b=$!
wait "$a"; wait "$b"
jq -en --slurpfile a "$T/concurrent-a.json" --slurpfile b "$T/concurrent-b.json" '$a[0].pid==$b[0].pid and $a[0].ready and $b[0].ready' >/dev/null
cp "$T/state/services/subscriptions/identity.json" "$T/second-identity.json"
jq -en --slurpfile a "$T/first-identity.json" --slurpfile b "$T/second-identity.json" '$a[0].generation!=$b[0].generation' >/dev/null
service subscriptions stop >"$T/stop-2.json"
pass concurrent_starts_have_one_identity

service auto-switch start >"$T/auto-start.json"
service auto-switch status-json >"$T/auto-status.json"
jq -e '.complete and .running and .ready' "$T/auto-status.json" >/dev/null
service auto-switch stop >"$T/auto-stop.json"
[ ! -e "$T/app/run/server-auto-switch.pid" ]
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ] || exit 1
[ ! -e "$T/app/runtime/xray" ]
pass real_auto_daemon_stops_without_job_or_xray

# A directly owned test wrapper crashes itself after the actual daemon has
# published its identity. No script sends a signal to a PID from a file.
cp "$T/app/bin/broray-subscription-scheduler" "$T/original-scheduler"
cat >"$T/app/bin/broray-subscription-scheduler" <<'CRASH'
#!/opt/bin/ash
. "$BRORAY_ROOT/lib/service-lifecycle.sh"
broray_service_daemon_enter subscriptions || exit $?
kill -KILL "$$"
CRASH
rc=0; /opt/bin/ash "$T/app/bin/broray-subscription-scheduler" >"$T/crash.out" 2>&1 || rc=$?
[ "$rc" = 137 ]
cp "$T/state/services/subscriptions/identity.json" "$T/crashed-identity.json"
cp "$T/original-scheduler" "$T/app/bin/broray-subscription-scheduler"
service subscriptions stop >"$T/stale-stop.json"
[ ! -e "$T/app/run/subscription-scheduler.pid" ] && [ ! -L "$T/app/run/subscription-scheduler.pid" ] || exit 1
[ ! -e "$T/app/run/subscription-scheduler.starttime" ] && [ ! -L "$T/app/run/subscription-scheduler.starttime" ] || exit 1
pass dead_daemon_stop_retires_projections_for_updater
service subscriptions start >"$T/post-crash-start.json"
service subscriptions stop >"$T/post-crash-stop.json"
pass dead_daemon_recovers_under_kernel_lease

cat >"$T/app/bin/broray-subscription-scheduler" <<'FOREGROUND'
#!/opt/bin/ash
. "$BRORAY_ROOT/lib/service-lifecycle.sh"
broray_service_daemon_enter subscriptions || exit $?
broray_service_run_job "$BRORAY_ROOT/tmp/lease-child.sh"
FOREGROUND
cat >"$T/app/tmp/lease-child.sh" <<'HELPER'
#!/opt/bin/ash
echo ready >"$BRORAY_ROOT/tmp/helper.ready"
for n in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  [ ! -f "$BRORAY_ROOT/tmp/helper.release" ] || break
  sleep 1
done
echo done >"$BRORAY_ROOT/tmp/helper.done"
HELPER
"$T/bin/service-parent-fixture" /opt/bin/ash "$T/app/bin/broray-subscription-scheduler" \
  "$T/app/tmp/helper.ready" "$T/app/tmp/helper.release" "$T/app/tmp/helper.done" \
  "$BRORAY_OPS_GUARD" "$T/state/services/subscriptions/lifetime.guard" >"$T/live-helper-proof.txt"
cp "$T/original-scheduler" "$T/app/bin/broray-subscription-scheduler"
service subscriptions start >"$T/post-helper-start.json"
service subscriptions stop >"$T/post-helper-stop.json"
pass live_foreground_helper_does_not_hold_daemon_lease

for name in subscriptions auto-switch; do
  service "$name" status-json >"$T/final-$name.json"
  jq -e '.complete and .running==false' "$T/final-$name.json" >/dev/null
  "$BRORAY_OPS_GUARD" "$T/state/services/$name/lifetime.guard" /opt/bin/ash -c ':'
done
pass final_leases_free
jq -n --rawfile names "$T/passed.txt" '{status:"PASS",tests:($names|split("\n")|map(select(length>0))),environment:"physical Keenetic ARM64 isolated service namespaces",applicationInstalled:false,persistentXrayStarted:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
