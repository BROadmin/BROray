#!/opt/bin/ash
set -eu
T=/opt/tmp/broray-311-route-ndmc-20260916
RAM=/tmp/broray-311-route-ndmc-20260916
MARKER=BRORAY311-ROUTE-NDMC-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ "$(cat "$T/TEST-OWNER")" = "$MARKER" ]
mkdir -m 700 "$RAM"; printf '%s\n' "$MARKER" >"$RAM/TEST-OWNER"
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
export BRORAY_ROOT="$T/app" BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_NDMC_RUNNER="$T/app/bin/broray-ndmc-run"
export PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"
# ndmc is a Keenetic binary; do not inject Entware's libraries into it.
unset LD_LIBRARY_PATH
mkdir -p "$T/app/run"
. "$T/app/lib/routes-router-config.sh"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" >>"$T/passed.txt"; }
mkdir "$BRORAY_ROUTES_CONFIG_NDMC_LOCK"
printf KEEP >"$BRORAY_ROUTES_CONFIG_NDMC_LOCK/foreign"
rc=0; broray_routes_config_ndmc_capture /opt/bin/true 'show running-config' "$RAM/out" "$RAM/err" 1 || rc=$?
[ "$rc" = 125 ]
[ "$(cat "$BRORAY_ROUTES_CONFIG_NDMC_LOCK/foreign")" = KEEP ]
pass unknown_legacy_lane_preserved
# Retire only this script's exact two-entry fixture after asserting content.
rm "$BRORAY_ROUTES_CONFIG_NDMC_LOCK/foreign"; rmdir "$BRORAY_ROUTES_CONFIG_NDMC_LOCK"
ndmc_bin="$(command -v ndmc)"
broray_routes_config_ndmc_capture "$ndmc_bin" 'show running-config' "$RAM/config" "$RAM/config.err" 15
[ -s "$RAM/config" ]; pass actual_ndmc_read_succeeds
BRORAY_ROUTES_CONFIG_NDMC="$ndmc_bin"; export BRORAY_ROUTES_CONFIG_NDMC
broray_routes_config_get_cache
jq -e '.source=="running-config" and (.routes|type)=="array"' "$BRORAY_ROUTES_CONFIG_CACHE" >/dev/null
pass actual_cache_refresh_succeeds
rc=0
"$BRORAY_NDMC_RUNNER" "$BRORAY_ROUTES_CONFIG_NDMC_LOCK.guard" "$BRORAY_ROUTES_CONFIG_NDMC_LOCK" 1 1 /opt/bin/ash \
  '"$BRORAY_ROOT/../bin/fixture" session-wait & wait' || rc=$?
[ "$rc" = 124 ]
for proc in /proc/[0-9]*/exe; do
    target="$(readlink -f "$proc" 2>/dev/null || true)"
    [ "$target" != "$T/bin/fixture" ]
done
pass timeout_drains_detached_descendants
broray_routes_config_ndmc_capture "$ndmc_bin" 'show running-config' "$RAM/config2" "$RAM/config2.err" 15
cmp "$RAM/config" "$RAM/config2"; pass lane_reusable_and_router_config_unchanged
export BRORAY_STATE_ROOT="$T/state" BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock"
export BRORAY_OPS_RAM_ROOT="$RAM/ops" BRORAY_OPS_UPDATER_ROOT="$T/updater" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
cat >"$T/app/lib/routes-download.sh" <<'FIXTURE'
broray_routes_check_run() {
    . "$BRORAY_ROOT/lib/routes-router-config.sh"
    broray_routes_config_ndmc_capture "$(command -v ndmc)" 'show running-config' "$BRORAY_ROOT/run/protected-config" "$BRORAY_ROOT/run/protected-error" 15
}
FIXTURE
/opt/bin/ash "$T/app/bin/broray-routes" check fixture
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
for f in "$T/state/operations/"*/supervisors.json; do jq -e '.supervisors==[]' "$f" >/dev/null; done
pass actual_ndmc_under_protected_route_tracer
# The bounded runner returned only after ECHILD. All protected supervisors
# have drained; no test-owned background jobs remain before archival.
jq -n --rawfile tests "$T/passed.txt" '{status:"PASS",tests:($tests|split("\n")|map(select(length>0))),applicationInstalled:false,routerRoutesModified:false}' >"$T/RESULT.json"
