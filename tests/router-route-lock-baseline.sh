#!/opt/bin/ash
# Reproduce legacy route lock behavior only inside the private test namespace.
set -eu
umask 077
T=/opt/tmp/broray-311-route-lock-baseline-20260916
RAM=/tmp/broray-311-route-lock-baseline-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-ROUTE-LOCK-BASELINE-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-ROUTE-LOCK-BASELINE-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app" BRORAY_ROUTES_ROOT="$T/routes"
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock"
export BRORAY_UPDATER_REQUEST_LOCK="$T/updater/request.lock" BRORAY_UPDATER_OPERATION_POINTER="$T/state/last-operation"
export BRORAY_UPDATER_OPERATION_ROOT="$T/state/operations" BRORAY_ROUTES_API_STALE_LOCK_ROOT="$T/stale"
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
: >"$T/reproduced.txt"
. "$T/app/lib/routes-download.sh"
mkdir -p "$T/routes/locks/operation.lock"
printf 'UNKNOWN_OWNER_MUST_SURVIVE\n' >"$T/routes/locks/operation.lock/foreign"
BRORAY_ROUTES_ACTIVE_BUNDLE=fixture
rc=0; broray_routes_download_lock_acquire || rc=$?; [ "$rc" = 0 ]
[ ! -e "$T/routes/locks/operation.lock/foreign" ]
printf '%s\n' download_replaces_unknown_resource_lock | tee -a "$T/reproduced.txt"
broray_routes_download_lock_release

mkdir "$T/routes/locks/operation.lock"
printf 'UNKNOWN_OWNER_MUST_SURVIVE\n' >"$T/routes/locks/operation.lock/foreign"
rc=0; broray_routes_lock_acquire || rc=$?; [ "$rc" = 0 ]
[ ! -e "$T/routes/locks/operation.lock/foreign" ]
printf '%s\n' check_replaces_unknown_resource_lock | tee -a "$T/reproduced.txt"
broray_routes_lock_release

. "$T/app/lib/routes-api-operation.sh"
mkdir "$T/global.lock"
printf '99999999\n' >"$T/global.lock/pid"
printf 'routes\n' >"$T/global.lock/scope"
printf 'download\n' >"$T/global.lock/action"
printf 'fixture\n' >"$T/global.lock/bundle"
printf '2026-09-16T00:00:00Z\n' >"$T/global.lock/startedAt"
[ ! -e /proc/99999999 ]
rc=0; broray_routes_api_lock_reclaim_stale || rc=$?; [ "$rc" = 0 ]
[ ! -e "$T/global.lock" ]
printf '%s\n' api_archives_pid_only_owner_without_child_or_boot_proof | tee -a "$T/reproduced.txt"
jq -Rn '[inputs]|{status:"BASELINE_REPRODUCED",tests:.,installedApplicationChanged:false,safetyAcceptance:"FAIL_REQUIRES_FIX"}' <"$T/reproduced.txt" >"$T/RESULT.json"
