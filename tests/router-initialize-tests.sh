#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-initialize-20260916
RAM=/tmp/broray-311-initialize-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-INITIALIZE-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; echo BRORAY311-INITIALIZE-20260916 >"$RAM/TEST-OWNER"
export PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/sbin"
export LD_LIBRARY_PATH="$T/lib:/opt/lib" BRORAY_ROOT="$T/app"
export BRORAY_STATE_ROOT="$T/state" BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock" BRORAY_OPS_UPDATER_ROOT="$T/updater"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
. "$T/app/lib/operation-client.sh"
[ ! -e "$T/state" ]
/opt/bin/ash "$T/bin/prepare-prefix" >"$T/prepare.txt"
broray_ops_call status >"$T/status.json"
broray_ops_call report >"$T/report.json"
jq -e '.complete and .operations==[]' "$T/status.json" >/dev/null
jq -e '.snapshotComplete' "$T/report.json" >/dev/null
pass actual_startup_prefix_enables_first_read
broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative
broray_ops_call pause >"$T/pause.json"
find "$T/state" -type f -exec sha256sum '{}' ';' | sort >"$T/before.txt"
/opt/bin/ash "$T/bin/prepare-prefix" >"$T/repeated-prepare.txt"
find "$T/state" -type f -exec sha256sum '{}' ';' | sort >"$T/after.txt"
cmp "$T/before.txt" "$T/after.txt"
broray_ops_call status >"$T/active.json"
jq -e '.automationPaused and .globalFence=="managed_active"' "$T/active.json" >/dev/null
broray_ops_finish completed ''
pass repeated_prepare_preserves_real_owner_and_pause
mkdir "$T/global.lock"; printf KEEP >"$T/global.lock/foreign"
/opt/bin/ash "$T/bin/prepare-prefix" >"$T/ambiguous-prepare.txt"
broray_ops_call status >"$T/ambiguous.json"
jq -e '.complete==false and .globalFence=="ambiguous"' "$T/ambiguous.json" >/dev/null
[ "$(cat "$T/global.lock/foreign")" = KEEP ]
pass ambiguous_fence_is_preserved
jq -Rn '[inputs]' <"$T/passed.txt" >"$T/tests.json"
jq -n --slurpfile tests "$T/tests.json" '{status:"PASS",tests:$tests[0],testScope:"private startup prefix and coordinator",hostApplicationAlreadyInstalled:true,installedApplicationChanged:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
