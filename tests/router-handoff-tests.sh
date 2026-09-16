#!/opt/bin/ash
set -eu
T=/opt/tmp/broray-311-handoff-20260915
RAM=/tmp/broray-311-handoff-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-HANDOFF-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; printf '%s\n' BRORAY311-HANDOFF-20260915 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
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
wait_ready() { n=0; while [ ! -f "$R/ready" ]; do n=$((n+1)); [ "$n" -lt 100 ]; /opt/bin/busybox usleep 100000; done; }
cat >"$T/worker.sh" <<'WORKER'
set -eu
. "$BRORAY_ROOT/lib/operation-client.sh"
broray_ops_accept_handoff "$HANDOFF_NONCE"
echo yes >"$R/ready"
n=0
while [ ! -f "$R/go" ]; do n=$((n+1)); [ "$n" -lt 100 ]; /opt/bin/busybox usleep 100000; done
broray_ops_run_helper 10 -- /opt/bin/busybox true
broray_ops_tick committing
broray_ops_finish completed
WORKER

case_dir retries
broray_ops_begin system xray:update xray USER cooperative
ID="$BRORAY_BACKGROUND_OPERATION_ID"; OLD_TOKEN="$BRORAY_BACKGROUND_OPERATION_TOKEN"
cp "$R/state/operations/$ID/owner.json" "$R/generation-before.json"
HANDOFF_NONCE="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)"; export HANDOFF_NONCE
/opt/bin/ash "$T/worker.sh" & worker=$!
/opt/bin/busybox usleep 100000
[ ! -e "$R/ready" ]; pass worker_gated_before_transfer
broray_ops_call handoff "$ID" "$OLD_TOKEN" "$$" "$worker" "$HANDOFF_NONCE" >"$R/handoff.json"
broray_ops_call handoff "$ID" "$OLD_TOKEN" "$$" "$worker" "$HANDOFF_NONCE" >"$R/handoff-retry.json"
cmp -s "$R/handoff.json" "$R/handoff-retry.json"; pass lost_handoff_response_idempotent
wait_ready
jq -e --arg pid "$worker" --arg old "$OLD_TOKEN" '(.owner.pid|tostring)==$pid and .acknowledged==true and .token!=$old' "$R/state/operations/$ID/executor.json" >/dev/null
cmp -s "$R/generation-before.json" "$R/global.lock/owner.json"; pass executor_rotated_generation_preserved
rc=0; broray_ops_call finish "$ID" "$OLD_TOKEN" completed '' >"$R/old-finish.json" || rc=$?
[ "$rc" = 2 ]; jq -e '.errorCode=="OWNER_CHANGED"' "$R/old-finish.json" >/dev/null
pass former_owner_cannot_finish_worker
broray_ops_call status >"$R/status.json"
jq -e '.operations[0].ownerStatus=="ACTIVE" and .globalFence=="managed_active"' "$R/status.json" >/dev/null
pass status_uses_current_executor
: >"$R/go"; wait "$worker"
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ] || exit 1; pass worker_helper_commit_finish
broray_ops_call handoff "$ID" "$OLD_TOKEN" "$$" "$worker" "$HANDOFF_NONCE" >"$R/handoff-after-finish.json"
cmp -s "$R/handoff.json" "$R/handoff-after-finish.json"; pass retry_after_worker_exit_is_read_only
unset BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN BRORAY_BACKGROUND_LAUNCH_NONCE

case_dir parent_client
broray_ops_begin system xray:update xray USER cooperative
HANDOFF_NONCE="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)"; export HANDOFF_NONCE
/opt/bin/ash "$T/worker.sh" & worker=$!
broray_ops_handoff_to "$worker" "$HANDOFF_NONCE"
[ -z "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] && [ -z "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] || exit 1
broray_ops_finish completed
[ -L "$R/global.lock" ]; pass parent_client_drops_old_authority
wait_ready; : >"$R/go"; wait "$worker"
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ] || exit 1; pass production_clients_complete_transfer

jq -Rn '[inputs|select(length>0)]|{status:"PASS",tests:.,routerAccessed:true,applicationInstalled:false}' <"$T/passed.txt" >"$T/RESULT.json"
cat "$T/RESULT.json"
