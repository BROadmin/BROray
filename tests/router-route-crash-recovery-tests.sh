#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-route-crash-20260916
RAM=/tmp/broray-311-route-crash-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-ROUTE-CRASH-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-ROUTE-CRASH-20260916 >"$RAM/TEST-OWNER"
export PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin" LD_LIBRARY_PATH="$T/lib:/opt/lib"
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
. "$T/app/lib/operation-client.sh"
cat >"$T/app/lib/routes-export-build.sh" <<'FIXTURE'
broray_routes_export_build_run() { :; }
FIXTURE
cat >"$T/app/lib/routes-router-sync.sh" <<'FIXTURE'
broray_routes_sync_apply() {
  . "$BRORAY_ROOT/lib/routes-resource-lock.sh"
  broray_route_resource_acquire "$BRORAY_ROOT/routes/locks/operation.lock" export fixture || return $?
  . "$BRORAY_ROOT/lib/routes-operation-progress.sh"
  broray_routes_progress_begin fixture install 3 || return $?
  broray_routes_progress_update applying 1 3 || return $?
  broray_routes_progress_tick 2 192.0.2.0/24 || return $?
  echo READY >"$BRORAY_ROOT/ready"
  while :; do sleep 1; done
}
FIXTURE
"$T/bin/route-crash-driver" "$T/app/ready" /opt/bin/ash "$T/app/bin/broray-routes" export fixture >"$T/entry.out" 2>"$T/entry.err"
lock="$T/app/routes/locks/operation.lock"
id="$(jq -r .job.operationId "$lock/owner.json")"
op="$T/state/operations/$id"
cp "$lock/owner.json" "$T/lease-before.json"
cp "$T/app/routes/operations/fixture.json" "$T/progress-before.json"
jq -e --arg id "$id" '.backgroundOperationId==$id and .running==true' "$T/progress-before.json" >/dev/null
pass actual_protected_owner_crash_drains_supervisor_tree
# Even a caller under the resource guard cannot imitate the coordinator.
rc=0
"$BRORAY_OPS_GUARD" "$T/app/routes/locks/resource.control.guard" /opt/bin/ash \
  "$T/app/lib/routes-resource-recover.sh" "$lock" "$id" retire || rc=$?
[ "$rc" = 73 ] && [ -d "$lock" ]
pass recovery_requires_both_guards
echo KEEP >"$lock/.foreign"
rc=0; broray_ops_call recover >"$T/recover-blocked.json" || rc=$?
[ "$rc" = 2 ] && [ -L "$T/global.lock" ] && [ "$(cat "$lock/.foreign")" = KEEP ]
cmp -s "$T/progress-before.json" "$T/app/routes/operations/fixture.json"
pass unknown_resource_content_preserves_fence_and_progress
rm "$lock/.foreign"
broray_ops_call recover >"$T/recover.json"
jq -e '.ok==true and .result=="recovered"' "$T/recover.json" >/dev/null
[ ! -e "$lock" ] && [ ! -L "$lock" ]
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
cmp -s "$T/lease-before.json" "$T/app/routes/locks/recovered-$id/owner.json"
jq -e '.running==false and .state=="recovered"' "$op/state.json" >/dev/null
pass dead_bound_resource_archived_before_global_release
jq -e --slurpfile before "$T/progress-before.json" --slurpfile after "$T/app/routes/operations/fixture.json" '
  .before==$before[0] and .after==$after[0] and .after.current==2 and .after.total==3 and
  .after.running==false and .after.phase=="interrupted" and .after.resumable==false and .after.success==false' \
  "$op/route-recovery-progress.json" >/dev/null
pass interrupted_progress_keeps_backup_and_latest_counter
broray_ops_call recover >"$T/recover-retry.json"
jq -e '.ok==true' "$T/recover-retry.json" >/dev/null
pass recovery_retry_is_idempotent
cat >"$T/app/lib/routes-download.sh" <<'FIXTURE'
broray_routes_check_run() { echo CHECKED >"$BRORAY_ROOT/checked"; }
FIXTURE
/opt/bin/ash "$T/app/bin/broray-routes" check fixture >"$T/recheck.out" 2>"$T/recheck.err"
[ "$(cat "$T/app/checked")" = CHECKED ]
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
pass existing_route_entry_available_after_recovery
for file in "$T/state/operations/"*/supervisors.json; do jq -e '.supervisors==[]' "$file" >/dev/null; done
for d in /proc/[0-9]*; do
  [ "${d##*/}" != "$$" ] || continue
  [ -r "$d/cmdline" ] || continue
  cmd="$(tr '\000' ' ' <"$d/cmdline" 2>/dev/null)" || continue
  case "$cmd" in *"$T/"*) exit 1 ;; esac
done
jq -n --rawfile tests "$T/passed.txt" '{status:"PASS",tests:($tests|split("\n")|map(select(length>0))),environment:"physical ARM protected CLI, native direct-child crash, bound lease and real progress; harmless route backend",applicationInstalled:false,routerRoutesModified:false}' >"$T/RESULT.json"
