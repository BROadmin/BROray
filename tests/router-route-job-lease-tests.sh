#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-route-job-lease-20260916
RAM=/tmp/broray-311-route-job-lease-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-ROUTE-JOB-LEASE-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-ROUTE-JOB-LEASE-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_ROUTES_ROOT="$T/app/routes"
export BRORAY_STATE_ROOT="$T/state" BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock"
export BRORAY_OPS_UPDATER_ROOT="$T/updater"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
unset BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN BRORAY_BACKGROUND_LAUNCH_NONCE
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/routes/operations" "$T/app/tmp" "$T/state"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
cat >"$T/app/lib/routes-download.sh" <<'FIXTURE'
broray_routes_check_run() {
  . "$BRORAY_ROOT/lib/routes-resource-lock.sh"
  broray_route_resource_acquire "$BRORAY_ROOT/routes/locks/operation.lock" check fixture || return $?
  cp "$BRORAY_ROOT/routes/locks/operation.lock/owner.json" "$BRORAY_ROOT/lease.json" || return $?
  jq -e --arg id "$BRORAY_BACKGROUND_OPERATION_ID" '.job.operationId==$id and (.job.jobTokenDigest|length)==64' "$BRORAY_ROOT/lease.json" >/dev/null || return 93
  broray_route_resource_release "$BRORAY_ROOT/routes/locks/operation.lock" "$BRORAY_ROUTE_RESOURCE_TOKEN"
}
FIXTURE
/opt/bin/ash "$T/app/bin/broray-routes" check fixture >"$T/check.out" 2>"$T/check.err"
lock="$T/app/routes/locks/operation.lock"
[ ! -e "$lock" ] && [ ! -L "$lock" ]
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
id="$(jq -r .job.operationId "$T/app/lease.json")"
jq -e --slurpfile context "$T/state/operations/$id/route-supervision.json" '
  .job.operationId==$context[0].operationId and .job.supervisorId==$context[0].supervisorId and
  .job.supervisorOwner==$context[0].owner' "$T/app/lease.json" >/dev/null
pass lease_has_exact_job_and_supervisor_identity
. "$T/app/lib/routes-resource-lock.sh"
broray_route_resource_acquire "$lock" check fixture
jq -e '.job==null' "$lock/owner.json" >/dev/null
broray_route_resource_release "$lock" "$BRORAY_ROUTE_RESOURCE_TOKEN"
[ ! -e "$lock" ] && [ ! -L "$lock" ]
pass unbound_compatibility_lease_keeps_its_owner_lifecycle
(
  BRORAY_BACKGROUND_OPERATION_ID="$id"
  BRORAY_BACKGROUND_OPERATION_TOKEN="$(jq -r .token "$T/state/operations/$id/owner.json")"
  export BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN
  export BRORAY_OPS_SUPERVISED=ptrace/1 BRORAY_OPS_ROUTE_SUPERVISED=ptrace/1
  rc=0; broray_route_resource_acquire "$lock" check fixture || rc=$?
  [ "$rc" != 0 ]
)
[ ! -e "$lock" ] && [ ! -L "$lock" ]
pass copied_completed_job_token_cannot_publish_lease
jq -e '.running==false and .state=="completed"' "$T/state/operations/$id/state.json" >/dev/null
for file in "$T/state/operations/"*/supervisors.json; do
  [ -e "$file" ] || continue
  jq -e '.supervisors|length==0' "$file" >/dev/null
done
jq -n --rawfile tests "$T/passed.txt" '{status:"PASS",tests:($tests|split("\n")|map(select(length>0))),environment:"physical ARM, real protected owner and route resource, harmless backend",applicationInstalled:false,routerRoutesModified:false,crashRecoveryImplemented:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
