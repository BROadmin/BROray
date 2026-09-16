#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-route-resource-locks-20260916
RAM=/tmp/broray-311-route-resource-locks-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-ROUTE-RESOURCE-LOCKS-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-ROUTE-RESOURCE-LOCKS-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_ROUTES_ROOT="$T/app/routes"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock"
export BRORAY_UPDATER_REQUEST_LOCK="$T/updater/request.lock" BRORAY_UPDATER_OPERATION_POINTER="$T/state/last-operation"
export BRORAY_ROUTES_API_STALE_LOCK_ROOT="$T/archive"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
mkdir -p "$T/app/routes/locks"
lock="$T/app/routes/locks/operation.lock"
for spec in routes-source-check.sh:broray_routes_lock routes-download.sh:broray_routes_download_lock routes-export-build.sh:broray_routes_export_lock routes-router-preflight.sh:broray_routes_preflight_lock routes-router-export.sh:broray_routes_router_export_lock routes-router-sync.sh:broray_routes_sync_lock routes-router-delete.sh:broray_routes_delete_lock routes-user-import.sh:broray_user_routes_lock; do
    module="${spec%%:*}"; prefix="${spec#*:}"
    (
      . "$T/app/lib/$module"
      BRORAY_ROUTES_ACTIVE_BUNDLE=fixture; BRORAY_ROUTES_EXPORT_BUNDLE=fixture
      BRORAY_ROUTES_PREFLIGHT_BUNDLE=fixture; BRORAY_ROUTES_ROUTER_EXPORT_BUNDLE=fixture
      "${prefix}_acquire" fixture
      jq -e '.kind=="route-resource-lock" and .owner.pid>1 and (.token|length)==32' "$lock/owner.json" >/dev/null
      case "$prefix" in
        broray_routes_sync_lock|broray_user_routes_lock) ;;
        *) [ "$(cat "$lock/bundle")" = fixture ] ;;
      esac
      if [ "$prefix" = broray_routes_delete_lock ]; then
        [ "$(cat "$lock/operation")" = delete ] && [ -s "$lock/startedAt" ]
      fi
      "${prefix}_release"
      [ ! -e "$lock" ]
    )
    mkdir "$lock"; echo KEEP >"$lock/foreign"
    (
      . "$T/app/lib/$module"
      rc=0; "${prefix}_acquire" fixture || rc=$?; [ "$rc" = 2 ]
      "${prefix}_release"
      [ "$(cat "$lock/foreign")" = KEEP ]
    )
    rm "$lock/foreign"; rmdir "$lock"
    pass "$module-own-release-and-ambiguous-preservation"
done
. "$T/app/lib/routes-download.sh"
broray_routes_download_lock_acquire fixture
export TEST_TOKEN="$BRORAY_ROUTES_DOWNLOAD_LOCK_TOKEN"
/opt/bin/ash -c '. "$BRORAY_ROOT/lib/routes-resource-lock.sh"; rc=0; broray_route_resource_release "$BRORAY_ROOT/routes/locks/operation.lock" "$TEST_TOKEN" || rc=$?; [ "$rc" = 2 ]'
[ -f "$lock/owner.json" ]
broray_routes_download_lock_release
pass copied_token_rejected_for_different_actual_owner
broray_routes_download_lock_acquire fixture
echo KEEP >"$lock/.foreign"
rc=0; broray_routes_download_lock_release || rc=$?; [ "$rc" = 2 ]
[ "$(cat "$lock/.foreign")" = KEEP ] && [ -f "$lock/owner.json" ]
rm "$lock/.foreign"
broray_routes_download_lock_release
pass hidden_evidence_preserved_before_first_unlink
mkdir "$T/global.lock"
printf '99999999\n' >"$T/global.lock/pid"
printf 'routes\n' >"$T/global.lock/scope"
printf 'download\n' >"$T/global.lock/action"
printf 'fixture\n' >"$T/global.lock/bundle"
printf '2000\n' >"$T/global.lock/startedAt"
find "$T/global.lock" -type f -exec sha256sum '{}' ';' | sort >"$T/global-before.txt"
. "$T/app/lib/routes-api-operation.sh"
rc=0; broray_routes_api_lock_reclaim_stale || rc=$?; [ "$rc" = 1 ]
rc=0; broray_routes_api_lock_acquire download fixture || rc=$?; [ "$rc" = 2 ]
broray_routes_api_lock_release
find "$T/global.lock" -type f -exec sha256sum '{}' ';' | sort >"$T/global-after.txt"
cmp "$T/global-before.txt" "$T/global-after.txt"
[ ! -e "$T/archive" ]
pass pid_only_global_owner_is_not_archived
"$BRORAY_OPS_GUARD" "$T/app/routes/locks/resource.control.guard" /opt/bin/ash -c 'echo ready >"$1"; n=0; while [ ! -f "$1.stop" ] && [ "$n" -lt 20 ]; do sleep 1; n=$((n+1)); done' holder "$T/guard-ready" &
holder=$!
n=0
while [ ! -f "$T/guard-ready" ] && [ "$n" -lt 5 ]; do sleep 1; n=$((n+1)); done
[ -f "$T/guard-ready" ]
rc=0; broray_routes_download_lock_acquire fixture || rc=$?
: >"$T/guard-ready.stop"
wait "$holder"
[ "$rc" = 2 ]
broray_routes_download_lock_acquire fixture
broray_routes_download_lock_release
pass kernel_guard_releases_after_owned_command_exits
jq -Rn '[inputs]' <"$T/passed.txt" >"$T/tests.json"
jq -n --slurpfile tests "$T/tests.json" '{status:"PASS",tests:$tests[0],applicationInstalled:false,persistentXrayStarted:false,domainRecoveryImplemented:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
