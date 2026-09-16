#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-route-cancel-policy-20260916
RAM=/tmp/broray-311-route-cancel-policy-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-ROUTE-CANCEL-POLICY-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-ROUTE-CANCEL-POLICY-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_ROUTES_ROOT="$T/app/routes"
export BRORAY_STATE_ROOT="$T/state" BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock"
export BRORAY_OPS_UPDATER_ROOT="$T/updater"
export BRORAY_ROUTES_PROGRESS_DIR="$T/app/routes/operations"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/state" "$T/app/routes"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
ops() { "$BRORAY_OPS_GUARD" "$T/state/operations.guard" /opt/bin/ash "$T/app/lib/operation-coordinator.sh" "$@"; }
nonce() { hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom; }
begin() {
    ops begin "$1" "$2" fixture USER "$$" cooperative "$(nonce)" >"$T/begin.json"
    id="$(jq -r .operationId "$T/begin.json")"; token="$(jq -r .token "$T/begin.json")"
    ops ack "$id" "$token" "$$" >/dev/null
    state="$T/state/operations/$id/state.json"
}
begin routes export
jq -e '.cancelability=="protected" and .initialCancelability=="protected"' "$state" >/dev/null
ops tick "$id" "$token" fetching >/dev/null
jq -e '.cancelability=="protected"' "$state" >/dev/null
pass route_admission_and_phase_are_protected
# An older cooperative record must not restore user cancellation.
jq '.cancelability="cooperative" | .initialCancelability="cooperative"' "$state" >"$T/old-state"
cat "$T/old-state" >"$state"
before="$(sha256sum "$state")"; fence="$(readlink "$T/global.lock")"
rc=0; ops cancel "$id" >"$T/cancel.json" || rc=$?
[ "$rc" = 2 ]; jq -e '.errorCode=="CANCEL_NOT_SUPPORTED"' "$T/cancel.json" >/dev/null
[ ! -e "$T/state/operations/$id/cancel.json" ]
pass individual_cancel_preserves_older_route_record
ops stop-background >"$T/stop-all.json"
jq -e '.automationPaused==true and .operations[0].protected==true and .operations[0].cancelRequested==false' "$T/stop-all.json" >/dev/null
[ "$before" = "$(sha256sum "$state")" ]; [ "$fence" = "$(readlink "$T/global.lock")" ]
[ ! -e "$T/state/operations/$id/cancel.json" ]
ops status >"$T/status.json"
jq -e --arg id "$id" '.operations[] | select(.operationId==$id) | .running==true and .cancelability=="protected"' "$T/status.json" >/dev/null
pass bulk_stop_and_public_status_preserve_route
ops finish "$id" "$token" completed '' >/dev/null
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
begin system custom:commit
jq -e '.cancelability=="protected"' "$state" >/dev/null
ops finish "$id" "$token" completed '' >/dev/null
pass route_action_is_protected_even_under_system_scope
. "$T/app/lib/routes-operation-progress.sh"
broray_routes_progress_begin fixture install 3
before="$(sha256sum "$BRORAY_ROUTES_PROGRESS_DIR/fixture.json")"
rc=0; broray_routes_progress_request_stop fixture || rc=$?; [ "$rc" = 4 ]
[ "$before" = "$(sha256sum "$BRORAY_ROUTES_PROGRESS_DIR/fixture.json")" ]
[ ! -e "$BRORAY_ROUTES_PROGRESS_DIR/fixture.stop" ]
rc=0; /opt/bin/ash "$T/app/bin/broray-routes" stop fixture 2>"$T/cli-error" || rc=$?
[ "$rc" = 1 ]; grep -q 'Остановка операций с маршрутами недоступна' "$T/cli-error"
[ "$before" = "$(sha256sum "$BRORAY_ROUTES_PROGRESS_DIR/fixture.json")" ]
pass legacy_library_and_cli_stop_do_not_write
echo OLD_REQUEST >"$BRORAY_ROUTES_PROGRESS_DIR/fixture.stop"
rc=0; broray_routes_progress_stop_requested fixture || rc=$?; [ "$rc" = 1 ]
broray_routes_progress_update applying 1 3
broray_routes_progress_pause 'Fixture error' false '203.0.113.0/24'
broray_routes_progress_resume_values fixture install 2 >"$T/resume.txt"
[ "$(cut -f1 "$T/resume.txt")" = 1 ] && [ "$(cut -f2 "$T/resume.txt")" = 3 ] && [ "$(cut -f3 "$T/resume.txt")" = true ]
broray_routes_progress_read fixture >"$T/progress.json"
jq -e '.resumable==true and .canStop==false and .phase=="failed_resumable"' "$T/progress.json" >/dev/null
pass old_marker_ignored_and_error_resume_preserved
# The preserved pending route must still block unrelated writers. Complete
# this private progress fixture before testing an independent subscription.
rc=0; ops begin system subscriptions:scheduler fixture USER "$$" cooperative "$(nonce)" >"$T/pending.json" || rc=$?
[ "$rc" = 2 ]; jq -e '.errorCode=="DOMAIN_OPERATION_BUSY"' "$T/pending.json" >/dev/null
broray_routes_progress_complete 'Private fixture completed'
pass pending_route_still_blocks_new_writers
begin system subscriptions:scheduler
ops cancel "$id" >"$T/subscription-cancel.json"
jq -e '.cancelRequested==true' "$T/subscription-cancel.json" >/dev/null
ops finish "$id" "$token" aborted CANCELLED >/dev/null
pass subscriptions_remain_cancellable
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
# No background workers were launched; all coordinator calls are synchronous.
jq -n --rawfile tests "$T/passed.txt" '{status:"PASS",tests:($tests|split("\n")|map(select(length>0))),environment:"physical ARM, actual proc owner, private prefix",applicationInstalled:false,routerRoutesModified:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
