#!/opt/bin/ash
# Physical prototype tests: private filesystem and self-signals only.
set -eu
T=/opt/tmp/broray-311-test-20260915
[ -d "$T" ] && [ ! -L "$T" ] && [ "$(readlink -f "$T")" = "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-PHYSICAL-20260915 ]
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH
export LD_LIBRARY_PATH="$T/lib:/opt/lib"
cd "$T"
sha256sum -c SHA256SUMS >/dev/null
G="$T/bin/broray-ops-guard"
export BRORAY_ROOT="$T/app" BRORAY_OPS_GUARD="$G"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES
mkdir "$T/cases"
: > "$T/passed.txt"
pass() { printf '%s\n' "$1" >>"$T/passed.txt"; printf 'PASS %s\n' "$1"; }
case_dir() {
  R="$T/cases/$1"; mkdir -p "$R/state"
  export BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
  export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
}
ops() { "$G" "$R/state/operations.guard" /opt/bin/ash "$T/app/lib/operation-coordinator.sh" "$@"; }
begin() {
  ops begin system subscriptions:scheduler subscriptions USER "$$" "${1:-cooperative}" >"$R/start.json"
  ID="$(jq -er '.operationId' "$R/start.json")"; TOKEN="$(jq -er '.token' "$R/start.json")"
}
finish() { ops finish "$ID" "$TOKEN" "${1:-completed}" "${2:-}" >/dev/null; }

[ "$(uname -m)" = aarch64 ]
[ "$("$G" --version)" = 'broray-ops-guard/2 fcntl-exec atomic-fence' ]
pass arm64_static_execution

case_dir kernel
"$G" "$R/kernel.guard" /opt/bin/ash -c 'echo ready >"$1/ready"; while [ ! -f "$1/stop" ]; do /opt/bin/busybox usleep 100000; done' kernel-test "$R" &
holder=$!
n=0; while [ ! -f "$R/ready" ]; do n=$((n+1)); [ "$n" -lt 100 ]; /opt/bin/busybox usleep 100000; done
rc=0; "$G" "$R/kernel.guard" /opt/bin/busybox true || rc=$?
[ "$rc" = 75 ]; pass kernel_contention_rejected
: >"$R/stop"; wait "$holder"
"$G" "$R/kernel.guard" /opt/bin/busybox true
pass cooperative_holder_exit_releases_guard
rc=0; "$G" "$R/kernel.guard" /opt/bin/ash -c 'kill -TERM $$' || rc=$?
[ "$rc" = 143 ]; "$G" "$R/kernel.guard" /opt/bin/busybox true
pass guard_self_term_releases_kernel_lock
rc=0; "$G" "$R/kernel.guard" /opt/bin/ash -c 'kill -KILL $$' || rc=$?
[ "$rc" = 137 ]; "$G" "$R/kernel.guard" /opt/bin/busybox true
pass guard_self_kill_releases_kernel_lock
printf KEEP >"$R/foreign"; ln -s "$R/foreign" "$R/unsafe.guard"
rc=0; "$G" "$R/unsafe.guard" /opt/bin/busybox true || rc=$?
[ "$rc" = 74 ] && [ "$(cat "$R/foreign")" = KEEP ]
pass foreign_guard_symlink_preserved

case_dir concurrency
pids=''
for n in 1 2 3 4 5 6; do
  "$G" "$R/parallel.guard" /opt/bin/ash -c 'mkdir "$1/critical" || exit 99; /opt/bin/busybox usleep 100000; rmdir "$1/critical"' writer "$R" &
  pids="$pids $!"
done
for child in $pids; do wait "$child"; done
pass six_kernel_writers_serialized

case_dir normal
begin
[ -L "$R/global.lock" ] && [ "$(readlink "$R/global.lock")" = "$R/state/operations/$ID/fence" ]
pass fsync_and_atomic_fence_on_router_filesystem
ops classify "$ID" >"$R/classification.json"
jq -e '.ownerStatus=="ACTIVE"' "$R/classification.json" >/dev/null
pass actual_proc_identity_matches
rc=0; ops recover >"$R/recover-active.json" || rc=$?
[ "$rc" = 2 ] && jq -e '.result=="ACTIVE"' "$R/recover-active.json" >/dev/null
pass live_operation_never_recovered
printf '{"heartbeatAt":"2000-01-01T00:00:00Z"}\n' >"$R/state/operations/$ID/heartbeat.json"
rc=0; ops recover >/dev/null || rc=$?
[ "$rc" = 2 ] && [ -L "$R/global.lock" ]
pass old_heartbeat_does_not_override_live_owner
GLOBAL_OPERATION_LOCK="$R/global.lock"
. "$T/old-updater-classifier.sh"
rc=0; global_operation_lock_classify || rc=$?
[ "$rc" = 1 ] && [ "$GLOBAL_OPERATION_LOCK_STATE" = unsafe-object ]
pass old_updater_rejects_new_active_fence
ops cancel "$ID" >/dev/null
ops status | jq -e '.operations[0].cancelRequested==true' >/dev/null
pass cancellation_request_recorded
ops status >"$R/public.json"
if grep -F "$TOKEN" "$R/public.json"; then exit 1; fi
pass public_status_omits_private_token
finish aborted CANCELLED
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
ops events | jq -e '[.events[].event]==["started","lock_acquired","cancel_requested","aborted"]' >/dev/null
global_operation_lock_classify
[ "$GLOBAL_OPERATION_LOCK_STATE" = absent ]
pass terminal_cleanup_and_old_updater_admission
begin; finish
pass next_operation_after_completion

case_dir crash
# The test child kills itself, so no signal is addressed using an external PID.
export T
rc=0
/opt/bin/ash -c '
  "$BRORAY_OPS_GUARD" "$BRORAY_STATE_ROOT/operations.guard" /opt/bin/ash "$BRORAY_ROOT/lib/operation-coordinator.sh" begin system subscriptions:scheduler subscriptions USER "$$" cooperative >"$BRORAY_STATE_ROOT/start.json" || exit 99
  kill -KILL $$
' || rc=$?
[ "$rc" = 137 ]
ops recover | jq -e '.ok and .result=="recovered"' >/dev/null
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
pass actual_owner_crash_recovered
ops recover | jq -e '.result=="absent"' >/dev/null
pass recovery_idempotent

case_dir protected
begin protected
rc=0; ops cancel "$ID" >"$R/cancel-protected.json" || rc=$?
[ "$rc" = 2 ] && jq -e '.errorCode=="CANCEL_NOT_SUPPORTED"' "$R/cancel-protected.json" >/dev/null
finish
pass protected_operation_rejects_cancel

case_dir pause
ops pause >/dev/null
rc=0; ops begin system subscriptions:scheduler subscriptions SUBSCRIPTION_AUTO "$$" cooperative >"$R/auto.json" || rc=$?
[ "$rc" = 2 ] && jq -e '.errorCode=="AUTOMATION_PAUSED"' "$R/auto.json" >/dev/null
begin; finish; ops resume >/dev/null
pass paused_automation_still_allows_manual_operation

case_dir legacy
mkdir "$R/global.lock"
printf '%s\n' 900001 >"$R/global.lock/pid"
printf '%s\n' system >"$R/global.lock/scope"
printf '%s\n' auto-switch >"$R/global.lock/action"
: >"$R/global.lock/bundle"; printf '%s\n' old >"$R/global.lock/startedAt"
rc=0; ops recover >"$R/legacy.json" || rc=$?
[ "$rc" = 2 ] && jq -e '.result=="legacy_owner_ambiguous"' "$R/legacy.json" >/dev/null
[ -f "$R/global.lock/pid" ]
pass legacy_owner_requires_separate_recovery

jq -Rn --arg kernel "$(uname -r)" --arg arch "$(uname -m)" \
  '{status:"PASS",environment:"Physical test router; native ARM64, actual kernel and proc, /opt filesystem",kernel:$kernel,architecture:$arch,
    tests:[inputs],scope:"Isolated Operation Manager prototype; no service/API integration",
    notTested:["full application integration","managed child TERM/KILL escalation","VPN continuity","reboot/power loss","legacy blocked-update delivery","long soak"]}' \
  <"$T/passed.txt" >"$T/RESULT.json"
echo PHYSICAL_TESTS_COMPLETE
