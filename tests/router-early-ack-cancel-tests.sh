#!/opt/bin/ash
# Real ARM owners and isolated state; never invokes installed job operations.
set -eu
umask 077
T=/opt/tmp/broray-311-early-ack-20260916
RAM=/tmp/broray-311-early-ack-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-EARLY-ACK-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-EARLY-ACK-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_OPS_GUARD="$T/bin/broray-ops-guard"
export BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor" BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
. "$T/app/lib/operation-client.sh"
mkdir "$T/cases"; : >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
begin_case() {
    R="$T/cases/$1"; mkdir -p "$R/state"
    export R BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
    export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
    nonce="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)"
    broray_ops_call begin system subscriptions:scheduler subscriptions USER "$$" cooperative "$nonce" >"$R/begin.json"
    id="$(jq -r .operationId "$R/begin.json")"; token="$(jq -r .token "$R/begin.json")"
    state="$R/state/operations/$id/state.json"
}
stop_all() { broray_ops_call stop-background >"$R/stop.json"; }
ack_error() {
    rc=0; broray_ops_call ack "$id" "$token" "$2" >"$R/ack-error.json" || rc=$?
    [ "$rc" = 2 ]; jq -e --arg code "$1" '.errorCode==$code' "$R/ack-error.json" >/dev/null
}
begin_case early
stop_all
ack_error CANCELLED "$$"
broray_ops_call status >"$R/status.json"
jq -e '.globalFence=="absent" and .automationPaused' "$R/status.json" >/dev/null
jq -e '.state=="aborted" and .acknowledged==false and .errorCode=="CANCELLED" and .running==false' "$state" >/dev/null
pass cancelled_start_retires_exact_owned_fence
ack_error OPERATION_FINISHED "$$"
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
pass repeated_ack_cannot_resurrect_terminal_start

begin_case acknowledged
broray_ops_call ack "$id" "$token" "$$" >"$R/ack.json"
stop_all
broray_ops_call ack "$id" "$token" "$$" >"$R/repeated-ack.json"
broray_ops_call status >"$R/status.json"
jq -e '.globalFence=="managed_active"' "$R/status.json" >/dev/null
broray_ops_call finish "$id" "$token" aborted CANCELLED >"$R/finish.json"
pass acknowledged_live_job_remains_fenced_until_finish

begin_case foreign
stop_all
before="$(sha256sum "$state")"
/opt/bin/sleep 3 & foreign=$!
ack_error OWNER_CHANGED "$foreign"
wait "$foreign"
[ -L "$R/global.lock" ] && [ "$before" = "$(sha256sum "$state")" ]
ack_error CANCELLED "$$"
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
pass foreign_owner_cannot_retire_another_start
jq -Rn '[inputs|select(length>0)]|{status:"PASS",tests:.,routerAccessed:true,applicationInstalled:false,environment:"physical ARM native coordinator, actual proc identities"}' <"$T/passed.txt" >"$T/RESULT.json"
cat "$T/RESULT.json"
