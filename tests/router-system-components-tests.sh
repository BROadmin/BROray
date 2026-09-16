#!/opt/bin/ash
set -eu
T=/opt/tmp/broray-311-system-components-20260916
RAM=/tmp/broray-311-system-components-20260916
MARKER=BRORAY311-SYSTEM-COMPONENTS-20260916
[ "$(readlink -f "$T")" = "$T" ]
[ "$(cat "$T/TEST-OWNER")" = "$MARKER" ]
mkdir -m 700 "$RAM"; printf '%s\n' "$MARKER" >"$RAM/TEST-OWNER"
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app" BRORAY_TX_APP_ROOT="$T/app"
export BRORAY_STATE_ROOT="$T/state" BRORAY_TMP_ROOT="$RAM" BRORAY_TX_TMP_BASE="$RAM"
export BRORAY_TX_STATE_ROOT="$T/txstate" BRORAY_TX_CURRENT="$RAM/current" BRORAY_TX_LOCK_DIR="$RAM/transaction.lock"
export BRORAY_GLOBAL_LOCK="$RAM/global.lock" BRORAY_TX_GLOBAL_LOCK="$RAM/global.lock"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_TX_OPKG=/opt/bin/opkg BRORAY_TX_ASH=/opt/bin/ash
export PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"
unset LD_LIBRARY_PATH
mkdir -p "$T/app/config" "$T/app/run" "$T/app/routes/locks" "$T/txstate"
. "$T/app/lib/broray-page.sh"
. "$T/app/lib/component-lifecycle.sh"
operation_id=uninstall-fixture
mkdir "$BRORAY_GLOBAL_LOCK"
printf 'system\n' >"$BRORAY_GLOBAL_LOCK/scope"
printf 'uninstall\n' >"$BRORAY_GLOBAL_LOCK/action"
: >"$BRORAY_GLOBAL_LOCK/bundle"
printf '2026-09-16T00:00:00Z\n' >"$BRORAY_GLOBAL_LOCK/startedAt"
printf '%s\n' "$operation_id" >"$BRORAY_GLOBAL_LOCK/operation-id"
broray_tx_control_owner_identity_capture "$$" "$BRORAY_GLOBAL_LOCK/owner-identity.tsv"
broray_lifecycle_uninstall_owner
cat >"$T/app/lib/routes-router-delete.sh" <<'FIXTURE'
. "$BRORAY_ROOT/lib/routes-resource-lock.sh"
broray_routes_delete_cleanup() { :; }
broray_routes_router_delete_run() {
    broray_tx_control_transition_assert || return 91
    broray_route_resource_acquire "$BRORAY_ROOT/routes/locks/operation.lock" delete "$1" || return 92
    cp "$BRORAY_ROOT/routes/locks/operation.lock/owner.json" "$BRORAY_ROOT/run/resource-owner.json" || return 93
    broray_route_resource_release "$BRORAY_ROOT/routes/locks/operation.lock" "$BRORAY_ROUTE_RESOURCE_TOKEN" || return 94
    echo DELETE >>"$BRORAY_ROOT/changed"
}
FIXTURE
cat >"$T/app/lib/server-service.sh" <<'FIXTURE'
broray_server_deactivate_commit() {
    broray_tx_control_transition_assert || return 91
    echo DEACTIVATE >>"$BRORAY_ROOT/changed"
}
FIXTURE
cat >"$T/app/lib/routes-export-build.sh" <<'FIXTURE'
broray_routes_export_build_run() {
    broray_tx_control_transition_assert || return 91
    echo BUILD >>"$BRORAY_ROOT/changed"
}
FIXTURE
cat >"$T/app/lib/routes-router-sync.sh" <<'FIXTURE'
broray_routes_sync_apply() {
    broray_tx_control_transition_assert || return 91
    echo APPLY >>"$BRORAY_ROOT/changed"
}
FIXTURE
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" >>"$T/passed.txt"; }
broray_lifecycle_component route-delete fixture
[ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" = 0 ]
[ "$BRORAY_TX_CONTROL_TRANSITION_DEPTH" = 0 ]
[ ! -e "$T/app/routes/locks/operation.lock" ]
pass delete_under_real_opkg_mutex_and_resource_lease
broray_lifecycle_component server-deactivate
[ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" = 0 ]; pass deactivate_under_real_opkg_mutex
broray_lifecycle_component route-export fixture
[ "$BRORAY_TX_NATIVE_OPKG_LOCK_HELD" = 0 ]; pass rollback_build_apply_under_real_opkg_mutex
printf 'DELETE\nDEACTIVATE\nBUILD\nAPPLY\n' >"$T/expected"
cmp "$T/expected" "$T/app/changed"
printf 'update\n' >"$BRORAY_GLOBAL_LOCK/action"
rc=0; broray_lifecycle_component route-delete fixture || rc=$?
[ "$rc" != 0 ]; cmp "$T/expected" "$T/app/changed"; pass foreign_action_preserved
printf 'uninstall\n' >"$BRORAY_GLOBAL_LOCK/action"
broray_lifecycle_uninstall_owner
# No component ran a real route command, service change or package install.
# Native OPKG acquired its real lock by blocking on a private empty FIFO and
# released through EOF, with its ownership and kernel lock checked throughout.
for dir in "$RAM"/.broray-control-mutex-*; do [ ! -e "$dir" ]; done
jq -n --rawfile tests "$T/passed.txt" '{status:"PASS",tests:($tests|split("\n")|map(select(length>0))),applicationInstalled:false,routerRoutesModified:false,nativeOpkg:true,businessFixture:true}' >"$T/RESULT.json"
