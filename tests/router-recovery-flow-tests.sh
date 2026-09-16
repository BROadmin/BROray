#!/opt/bin/ash
# Actual ARM processes in private namespaces; installed application is untouched.
set -eu
umask 077
T=/opt/tmp/broray-311-recovery-flow-20260916
RAM=/tmp/broray-311-recovery-flow-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-RECOVERY-FLOW-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; echo BRORAY311-RECOVERY-FLOW-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib" T
export BRORAY_ROOT="$T/app" BRORAY_OPS_GUARD="$T/bin/broray-ops-guard"
export BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor" BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
. "$T/app/lib/operation-client.sh"
mkdir "$T/cases"; : >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
case_dir() {
    R="$T/cases/$1"; mkdir -p "$R/state"
    export R BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
    export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
}
wait_file() {
    n=0
    while [ ! -f "$1" ]; do n=$((n+1)); [ "$n" -lt 200 ] || return 1; /opt/bin/busybox usleep 100000; done
}
recover_blocked() {
    rc=0; broray_ops_call recover >"$R/recovery.json" || rc=$?
    [ "$rc" = 2 ]; jq -e '.ok==false and .automationPaused==true and .errorCode=="RECOVERY_BLOCKED"' "$R/recovery.json" >/dev/null
}
cat >"$T/worker.sh" <<'WORKER'
set -eu
. "$BRORAY_ROOT/lib/operation-client.sh"
broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative
rc=0
broray_ops_run_helper 35 -- /opt/bin/ash -c 'echo yes >"$R/ready"; trap "" TERM; sleep 60' || rc=$?
[ "$rc" = 130 ]
echo yes >"$R/drained"
# Deliberately omit finish to model an executor exiting without finalization.
n=0
while [ ! -f "$R/exit-worker" ]; do n=$((n+1)); [ "$n" -lt 200 ] || exit 1; /opt/bin/busybox usleep 100000; done
WORKER
case_dir cooperative
/opt/bin/ash "$T/worker.sh" >"$R/worker.out" 2>"$R/worker.err" & worker=$!
wait_file "$R/ready"
recover_blocked
jq -e '.result=="ACTIVE" and .retryable==true' "$R/recovery.json" >/dev/null
[ -L "$R/global.lock" ]
pass recovery_requests_stop_but_keeps_live_executor_fence
wait_file "$R/drained"
recover_blocked
jq -e '.result=="ACTIVE" and .retryable==true' "$R/recovery.json" >/dev/null
[ -L "$R/global.lock" ]
pass drained_helpers_do_not_override_live_executor
: >"$R/exit-worker"; wait "$worker"
broray_ops_call recover >"$R/recovered.json"
jq -e '.ok==true and .result=="recovered" and .retryable==false and .automationPaused==true' "$R/recovered.json" >/dev/null
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ] || exit 1
pass executor_exit_allows_verified_recovery
broray_ops_call recover >"$R/again.json"
jq -e '.result=="absent" and .automationPaused==true' "$R/again.json" >/dev/null
pass recovery_is_idempotent_and_leaves_automation_paused

case_dir protected
broray_ops_begin routes export fixture USER protected
state="$R/state/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json"
before="$(sha256sum "$state")"
recover_blocked
jq -e '.result=="ACTIVE" and .retryable==false and .operations[0].protected==true' "$R/recovery.json" >/dev/null
[ ! -e "$R/state/operations/$BRORAY_BACKGROUND_OPERATION_ID/cancel.json" ]
[ "$before" = "$(sha256sum "$state")" ]
broray_ops_finish completed ''
pass routes_continue_without_cancellation

case_dir legacy
mkdir "$R/global.lock"
printf '%s\n' "$$" >"$R/global.lock/pid"
echo system >"$R/global.lock/scope"; echo auto-switch >"$R/global.lock/action"
echo >"$R/global.lock/bundle"; echo 2020 >"$R/global.lock/startedAt"
before="$(sha256sum "$R/global.lock/"*)"
recover_blocked
jq -e '.result=="legacy_owner_ambiguous" and .retryable==false' "$R/recovery.json" >/dev/null
[ "$before" = "$(sha256sum "$R/global.lock/"*)" ]
pass legacy_ownership_preserved_and_reason_reported

case_dir updater
mkdir -p "$R/updater/request.lock"; echo fixture >"$R/updater/request.lock/record"
recover_blocked
jq -e '.result=="updater_pending" and .retryable==false' "$R/recovery.json" >/dev/null
[ "$(cat "$R/updater/request.lock/record")" = fixture ]
pass updater_lock_preserved_and_reported

case_dir updater_pointer
mkdir -p "$R/state/operations/update-fixture"
echo update-fixture >"$R/state/last-operation"
echo '{"kind":"update","running":true}' >"$R/state/operations/update-fixture/state.json"
before="$(sha256sum "$R/state/operations/update-fixture/state.json")"
recover_blocked
jq -e '.result=="updater_pending"' "$R/recovery.json" >/dev/null
[ "$before" = "$(sha256sum "$R/state/operations/update-fixture/state.json")" ]
pass updater_state_without_request_lock_is_preserved

case_dir updater_fifo
mkfifo "$R/state/last-operation"
recover_blocked
jq -e '.result=="updater_pending"' "$R/recovery.json" >/dev/null
[ -p "$R/state/last-operation" ]
pass nonregular_updater_pointer_is_not_read

# Every process created above was joined; fixture locks remain only in the
# archived private namespace. No installed service/configuration was modified.
jq -Rn '[inputs|select(length>0)]|{status:"PASS",tests:.,routerAccessed:true,applicationInstalled:false,environment:"physical ARM native coordinator/supervisor, actual proc identities"}' <"$T/passed.txt" >"$T/RESULT.json"
cat "$T/RESULT.json"
