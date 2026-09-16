#!/opt/bin/ash
# Exact production activation on ARM; private business files and restart stub.
set -eu
umask 077
T=/opt/tmp/broray-311-activation-prepare-20260916
RAM=/tmp/broray-311-activation-prepare-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-ACTIVATION-PREPARE-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-ACTIVATION-PREPARE-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app"
export BRORAY_PROXY_HOST=127.0.0.1 BRORAY_PROXY_PORT=2080 BRORAY_INTERFACE=Proxy0
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_XRAY_BINARY="$T/app/bin/validator" REAL_XRAY="$T/bin/real-xray"
export BRORAY_INIT="$T/app/bin/init-fixture"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/tmp" "$T/app/bin" "$T/app/logs" "$T/app/config/system" "$T/cases"
ln -s "$T/bin/jq" "$T/app/bin/jq"
printf '%s\n' '{"listenAddress":"127.0.0.1","socksPort":2080}' >"$T/app/config/system/settings.json"
cat >"$T/app/bin/validator" <<'VALIDATOR'
#!/opt/bin/ash
if [ "${TEST_MODE:-normal}" = wait ]; then
  echo ready >"$R/ready"; trap '' TERM; sleep 60; exit 0
fi
exec "$REAL_XRAY" "$@"
VALIDATOR
cat >"$BRORAY_INIT" <<'INIT'
#!/opt/bin/ash
jq -e '.phase=="committing" and .cancelability=="protected"' "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json" >/dev/null || exit 91
jq -e '.supervisors==[]' "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/supervisors.json" >/dev/null || exit 92
echo restart >>"$R/restarts"
[ "${TEST_RESTART_FAIL:-0}" = 0 ]
INIT
chmod 700 "$T/app/bin/validator" "$BRORAY_INIT"
. "$T/app/lib/server-service.sh"
. "$T/app/lib/server-import.sh"
broray_server_import_dispatch 'vless://11111111-2222-4333-8444-555555555555@93.184.216.34:443?security=none&type=tcp#Rejected' subscription bad 0 >/dev/null
broray_server_import_dispatch 'vless://11111111-2222-4333-8444-555555555555@example.invalid:443?security=tls&type=tcp&sni=example.invalid#Valid' subscription good 0 >/dev/null
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
case_dir() {
  R="$T/cases/$1"; mkdir -p "$R/state"
  export R BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
  export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
  export TEST_MODE=normal TEST_RESTART_FAIL=0
  printf '%s\n' '{"outbounds":[{"protocol":"blackhole"}]}' >"$T/app/config/config.json"
  cp "$T/app/config/config.json" "$R/before.json"
  rm -f "$T/app/config/active-server"
}
terminal() {
  [ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
  for f in "$R/state/operations/"*/state.json; do jq -e --arg state "$1" '.state==$state and .running==false' "$f" >/dev/null; done
  for f in "$R/state/operations/"*/supervisors.json; do jq -e '.supervisors==[]' "$f" >/dev/null; done
}
activate() {
  /opt/bin/ash -c '
  . "$BRORAY_ROOT/lib/server-service.sh"
  broray_server_refresh_keenetic_status() { :; }
  broray_server_summary() { :; }
  broray_interface_sync_description() { :; }
  broray_job_begin routes servers:activate servers USER cooperative || exit $?
  trap '\''rc=$?; trap - EXIT; broray_job_exit "$rc" || rc=75; exit "$rc"'\'' EXIT
  broray_server_activate subscription-good-0000
  '
}
case_dir rejected
/opt/bin/ash -c '. "$BRORAY_ROOT/web-new/api/servers/common.sh"; broray_servers_api_lock activate; broray_servers_api_run broray_server_activate subscription-bad-0000' >"$R/response.http" 2>"$R/error.txt"
grep -q '400 Bad Request' "$R/response.http"
grep -q 'Xray' "$R/response.http"
terminal failed; cmp -s "$R/before.json" "$T/app/config/config.json"
[ ! -e "$T/app/config/active-server" ] && [ ! -e "$R/restarts" ]
pass real_xray_rejection_returns_400_preserves_runtime_releases_fence

case_dir valid
activate >"$R/output.txt" 2>"$R/error.txt"
terminal completed
jq -e '.outbounds[0].protocol=="vless"' "$T/app/config/config.json" >/dev/null
[ "$(cat "$T/app/config/active-server")" = subscription-good-0000 ]
[ "$(cat "$R/restarts")" = restart ]
pass real_xray_validation_then_protected_restart_after_helper_drain

case_dir cancelled
TEST_MODE=wait
activate >"$R/output.txt" 2>"$R/error.txt" & worker=$!
n=0; while [ ! -e "$R/ready" ]; do n=$((n+1)); [ "$n" -lt 200 ]; /opt/bin/busybox usleep 100000; done
broray_ops_call status >"$R/active.json"
id="$(jq -er '.operations[0]|select(.phase=="checking" and .cancelability=="cooperative")|.operationId' "$R/active.json")"
broray_ops_call cancel "$id" >"$R/cancel.json"
rc=0; wait "$worker" || rc=$?; [ "$rc" = 130 ]
terminal aborted; cmp -s "$R/before.json" "$T/app/config/config.json"
[ ! -e "$R/restarts" ]
pass cancellation_drains_validator_without_starting_persistent_xray

case_dir restart_failed
TEST_RESTART_FAIL=1; rc=0
activate >"$R/output.txt" 2>"$R/error.txt" || rc=$?
[ "$rc" = 75 ]; [ -L "$R/global.lock" ]
cmp -s "$R/before.json" "$T/app/config/config.json"
[ ! -e "$T/app/config/active-server" ]
broray_ops_call status >"$R/status.json"
jq -e '.globalFence=="managed_stale" and .operations[0].cancelability=="protected" and .operations[0].running==true' "$R/status.json" >/dev/null
pass failed_restart_retains_protected_fence_for_domain_recovery

case_dir rollback_rejected
rc=0
/opt/bin/ash -c '
  . "$BRORAY_ROOT/lib/server-service.sh"
  broray_job_begin routes servers:activate servers USER cooperative || exit $?
  trap '\''rc=$?; trap - EXIT; broray_job_exit "$rc" || rc=75; exit "$rc"'\'' EXIT
  broray_job_checkpoint committing
  BRORAY_JOB_UNRESOLVED=true
  broray_server_activate subscription-bad-0000
' >"$R/output.txt" 2>"$R/error.txt" || rc=$?
[ "$rc" = 75 ]; [ -L "$R/global.lock" ]
broray_ops_call status >"$R/status.json"
jq -e '.globalFence=="managed_stale" and .operations[0].phase=="committing" and .operations[0].cancelability=="protected"' "$R/status.json" >/dev/null
pass rollback_validation_rejection_never_downgrades_protected_transaction

# No fixture worker remains: each worker was waited and all helper registries
# must be drained before the archive/retirement performed by the host runner.
for f in "$T/cases/"*/state/operations/*/supervisors.json; do [ ! -e "$f" ] || jq -e '.supervisors==[]' "$f" >/dev/null; done
jq -Rn '[inputs] | {status:"PASS",tests:.,installedApplicationChanged:false,realXrayValidator:true,restartStub:true}' <"$T/passed.txt" >"$T/RESULT.json"
