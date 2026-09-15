#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-sidecars-20260916
RAM=/tmp/broray-311-sidecars-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-SIDECARS-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-SIDECARS-20260916 >"$RAM/TEST-OWNER"
export PATH="$T/app/bin:$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export LD_LIBRARY_PATH="$T/lib:/opt/lib" BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_STATE_ROOT="$T/state" BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock" BRORAY_OPS_UPDATER_ROOT="$T/updater"
export BRORAY_HOME_SNAPSHOT_REFRESH="$T/bin/refresh" BRORAY_LIGHTTPD_GUARD="$T/bin/ok" BRORAY_MONITOR_SERVICE="$T/bin/ok"
export BRORAY_RECONCILE_INTERFACE="$T/bin/interface"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
mkdir -p "$T/app/tmp" "$T/app/run" "$T/app/logs"
ln -s "$T/bin/jq" "$T/app/bin/jq"
printf '#!/opt/bin/ash\nexit 0\n' >"$T/bin/ok"
printf '#!/opt/bin/ash\necho "$2" >>"$BRORAY_ROOT/tmp/refreshed"\n' >"$T/bin/refresh"
cat >"$T/bin/interface" <<'INTERFACE'
#!/opt/bin/ash
[ -L "$BRORAY_ROUTES_API_LOCK" ] || exit 99
echo "$1" >>"$BRORAY_ROOT/tmp/interface.calls"
INTERFACE
chmod 700 "$T/bin/ok" "$T/bin/refresh" "$T/bin/interface"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
service() { /opt/bin/ash "$T/app/bin/broray-service" "$@"; }
. "$T/app/lib/startup-sidecars.sh"

for pair in home-snapshot:home-snapshotd.pid interface-reconcile:interface-reconcile.pid; do
  name="${pair%:*}"; file="${pair#*:}"
  service "$name" status-json >"$T/empty-$name.json"
  jq -e '.complete and .running==false' "$T/empty-$name.json" >/dev/null
  printf '99999999\n' >"$T/app/run/$file"
  for action in stop restart; do
    rc=0; service "$name" "$action" >"$T/legacy-$name-$action.txt" 2>&1 || rc=$?
    [ "$rc" = 75 ]; [ "$(cat "$T/app/run/$file")" = 99999999 ]
  done
  cp "$T/app/run/$file" "$T/legacy-$file"; rm "$T/app/run/$file"
done
pass unknown_legacy_sidecars_preserved

broray_home_snapshot_start >"$T/home-start.json"
broray_home_snapshot_running
for n in 1 2 3 4 5; do [ ! -e "$T/app/tmp/refreshed" ] || break; sleep 1; done
[ -s "$T/app/tmp/refreshed" ]
broray_home_snapshot_stop >"$T/home-stop.json"
[ ! -e "$T/app/run/home-snapshotd.pid" ]
pass real_home_cycle_and_s24_control

service home-snapshot start >"$T/concurrent-a.json" & a=$!
service home-snapshot start >"$T/concurrent-b.json" & b=$!
wait "$a"; wait "$b"
jq -en --slurpfile a "$T/concurrent-a.json" --slurpfile b "$T/concurrent-b.json" '$a[0].ready and $b[0].ready and $a[0].pid==$b[0].pid' >/dev/null
printf '{"schemaVersion":1,"generation":"00000000000000000000000000000000"}\n' >"$T/state/services/home-snapshot/stop.json"
sleep 2
service home-snapshot status-json >"$T/old-stop.json"
jq -e '.complete and .ready and .running' "$T/old-stop.json" >/dev/null
service home-snapshot stop >"$T/concurrent-stop.json"
pass concurrent_start_and_stale_stop_generation

cat >"$T/bin/refresh" <<'BUSY'
#!/opt/bin/ash
echo ready >"$BRORAY_ROOT/tmp/busy.ready"
for n in $(seq 1 50); do [ ! -e "$BRORAY_ROOT/tmp/busy.release" ] || exit 0; sleep 1; done
exit 1
BUSY
service home-snapshot start >"$T/busy-start.json"
for n in 1 2 3 4 5; do [ ! -e "$T/app/tmp/busy.ready" ] || break; sleep 1; done
[ -e "$T/app/tmp/busy.ready" ]
rc=0; service home-snapshot stop >"$T/busy-stop.json" 2>&1 || rc=$?
[ "$rc" = 75 ]
service home-snapshot status-json >"$T/busy-state.json"
jq -e '.complete and .running and .state=="stopping"' "$T/busy-state.json" >/dev/null
: >"$T/app/tmp/busy.release"
service home-snapshot stop >"$T/released-stop.json"
pass busy_refresh_waits_without_signals

/opt/bin/ash "$T/app/bin/broray-interface-reconcile" >"$T/reconcile.txt" 2>&1
[ "$(cat "$T/app/tmp/interface.calls")" = "$(printf 'check\nsync-name\ncheck')" ]
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
service interface-reconcile status-json >"$T/reconcile-stopped.json"
jq -e '.complete and .running==false' "$T/reconcile-stopped.json" >/dev/null
pass reconcile_uses_protected_admission

printf '{"paused":true}\n' >"$T/state/background-automation.json"
before="$(sha256sum "$T/app/tmp/interface.calls" | awk '{print $1}')"
service interface-reconcile start >"$T/paused-start.json"
sleep 4
service interface-reconcile stop >"$T/paused-stop.json"
[ "$before" = "$(sha256sum "$T/app/tmp/interface.calls" | awk '{print $1}')" ]
grep -qx 'state=deferred' "$T/app/run/interface-reconcile.status"
printf '{"paused":false}\n' >"$T/state/background-automation.json"
pass automation_pause_defers_reconcile

printf '#!/opt/bin/ash\nexit 1\n' >"$T/bin/interface"
rc=0; /opt/bin/ash "$T/app/bin/broray-interface-reconcile" >"$T/reconcile-failed.txt" 2>&1 || rc=$?
[ "$rc" = 75 ]; [ -L "$T/global.lock" ]
service interface-reconcile stop >"$T/failed-stop.json"
[ -L "$T/global.lock" ]
grep -qx 'method=recovery-required' "$T/app/run/interface-reconcile.status"
# Preserve this deliberate protected fixture inside the archived namespace.
for name in home-snapshot interface-reconcile; do
  service "$name" status-json >"$T/final-$name.json"
  jq -e '.complete and .running==false' "$T/final-$name.json" >/dev/null
  "$BRORAY_OPS_GUARD" "$T/state/services/$name/lifetime.guard" /opt/bin/ash -c ':'
done
pass failed_mutation_preserves_protected_fence
jq -n --rawfile names "$T/passed.txt" '{status:"PASS",tests:($names|split("\n")|map(select(length>0))),testScope:"real ARM64 sidecars, isolated state and harmless transport",hostApplicationAlreadyInstalled:true,installedApplicationChanged:false,vpnTrafficVerified:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
