#!/opt/bin/ash
# Bounded helpers in a private namespace. No production service or settings.
set -eu
T=/opt/tmp/broray-311-supervisor-20260915
RAM=/tmp/broray-311-supervisor-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-SUPERVISOR-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"
printf '%s\n' BRORAY311-SUPERVISOR-20260915 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_SUPERVISOR_TEST_FIXTURE="$T/bin/fixture"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
. "$T/app/lib/operation-client.sh"
mkdir "$T/cases"; : >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
case_dir() {
  R="$T/cases/$1"; mkdir -p "$R/state"
  export R BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
  export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
}
begin() { broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative; }
finish() { broray_ops_finish "${1:-completed}" "${2:-}"; [ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ] || exit 1; }
check_empty_registry() {
  jq -e '.supervisors==[]' "$R/state/operations/$BRORAY_BACKGROUND_OPERATION_ID/supervisors.json" >/dev/null
}
cancel_when_ready() {
  n=0
  while [ ! -f "$R/ready" ]; do n=$((n+1)); [ "$n" -lt 100 ] || return 1; /opt/bin/busybox usleep 100000; done
  broray_ops_call cancel "$BRORAY_BACKGROUND_OPERATION_ID" >"$R/cancel-result.json"
}

[ "$(uname -m)" = aarch64 ]
[ "$("$BRORAY_OPS_SUPERVISOR" --version)" = 'broray-ops-supervisor/1 ptrace-exitkill cooperative-helper' ]
pass static_arm64_supervisor
case_dir normal
begin
broray_ops_run_helper 10 -- /opt/bin/ash -c 'printf "%s" "literal spaces"' >"$R/output"
[ "$(cat "$R/output")" = 'literal spaces' ]
check_empty_registry
broray_ops_tick committing
finish
pass normal_helper_drained_before_commit

case_dir thread_exec
begin
rc=0; broray_ops_run_helper 10 -- "$T/bin/fixture" thread-exec >"$R/output" || rc=$?
[ "$rc" = 19 ] && [ "$(cat "$R/output")" = thread-exec-ok ] || exit 1
check_empty_registry; finish failed OPERATION_FAILED
pass nonleader_exec_retains_exit_status

case_dir thread_churn
begin
broray_ops_run_helper 20 -- "$T/bin/fixture" thread-churn
check_empty_registry; finish
pass hundred_thread_lifecycles

case_dir timeout
begin
rc=0; broray_ops_run_helper 1 -- /opt/bin/ash -c 'trap "" TERM; sleep 60' || rc=$?
[ "$rc" = 124 ]; check_empty_registry
broray_ops_call events >"$R/events.json"
jq -e '[.events[].event] | index("term")!=null and index("kill")!=null' "$R/events.json" >/dev/null
finish failed OPERATION_FAILED
pass timeout_term_exitkill_journal

case_dir cancel
begin
cancel_when_ready & requester=$!
rc=0; broray_ops_run_helper 20 -- /opt/bin/ash -c 'trap "" TERM; echo yes >"$R/ready"; sleep 60' || rc=$?
wait "$requester"; [ "$rc" = 130 ]; check_empty_registry
broray_ops_call events >"$R/events-before.json"
broray_ops_call helpers-drain "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" >/dev/null
broray_ops_call events >"$R/events-after.json"
cmp -s "$R/events-before.json" "$R/events-after.json"
finish aborted CANCELLED
pass cancellation_and_idempotent_collection

case_dir group_stop
begin
cancel_when_ready & requester=$!
rc=0; broray_ops_run_helper 20 -- /opt/bin/ash -c 'echo yes >"$R/ready"; kill -STOP $$; sleep 60' || rc=$?
wait "$requester"; [ "$rc" = 130 ]; check_empty_registry; finish aborted CANCELLED
pass stopped_helper_cancellation

case_dir escaped_session
begin
/opt/bin/ash -c 'echo yes >"$R/sentinel-ready"; n=0; while [ ! -f "$R/sentinel-stop" ]; do n=$((n+1)); [ "$n" -lt 300 ] || exit 1; /opt/bin/busybox usleep 100000; done; echo yes >"$R/sentinel-finished"' & sentinel=$!
cancel_when_ready & requester=$!
rc=0; broray_ops_run_helper 20 -- /opt/bin/ash -c '"$BRORAY_SUPERVISOR_TEST_FIXTURE" session-wait & echo yes >"$R/ready"; wait' || rc=$?
wait "$requester"; [ "$rc" = 130 ]; check_empty_registry
[ -f "$R/sentinel-ready" ] && [ ! -f "$R/sentinel-finished" ] || exit 1
: >"$R/sentinel-stop"; wait "$sentinel"
[ -f "$R/sentinel-finished" ]; finish aborted CANCELLED
pass escaped_session_stopped_sentinel_untouched

case_dir protected
begin
broray_ops_tick committing
rc=0; broray_ops_run_helper 10 -- /opt/bin/ash -c 'echo BAD >"$R/forbidden"' || rc=$?
[ "$rc" = 74 ] && [ ! -f "$R/forbidden" ] || exit 1; finish
pass protected_phase_never_opens_helper_gate

case_dir repeat
begin
for n in 1 2 3 4 5; do
  broray_ops_run_helper 10 -- /opt/bin/busybox true
  check_empty_registry
done
finish
pass repeated_helpers_keep_registry_bounded

jq -Rn '[inputs|select(length>0)]|{status:"PASS",tests:.,routerAccessed:true,applicationInstalled:false}' <"$T/passed.txt" >"$T/RESULT.json"
cat "$T/RESULT.json"
