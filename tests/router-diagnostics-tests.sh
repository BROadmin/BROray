#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-diagnostics-20260916
RAM=/tmp/broray-311-diagnostics-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-DIAGNOSTICS-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; echo BRORAY311-DIAGNOSTICS-20260916 >"$RAM/TEST-OWNER"
export PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export LD_LIBRARY_PATH="$T/lib:/opt/lib" BRORAY_ROOT="$T/app"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_ASH=/opt/bin/ash
export BRORAY_STATE_ROOT="$T/state" BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_OPS_UPDATER_ROOT="$T/updater" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/config/system" "$T/app/config/subscriptions" "$T/app/run" "$T/state/operations"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
ops() { "$BRORAY_OPS_GUARD" "$T/state/operations.guard" /opt/bin/ash "$T/app/lib/operation-coordinator.sh" "$@"; }
printf '{"enabled":false,"qualityRefreshEnabled":false}\n' >"$T/app/config/system/server-auto-switch.json"
ops report >"$T/empty.json"
jq -e '.automation.autoSwitch==false and .automation.serverCheck==false and .automation.subscriptionUpdate==false and .automationDetails.complete and (.platform.kernel|length)>0 and .complete==false' "$T/empty.json" >/dev/null
pass known_false_and_kernel_without_vpn_claim
printf '{"enabled":true,"autoUpdateEnabled":true,"url":"https://PRIVATE_CANARY/secret"}\n' >"$T/app/config/subscriptions/one.json"
ops report >"$T/enabled.json"
jq -e '.automation.subscriptionUpdate and .automationDetails.automaticSubscriptions==1' "$T/enabled.json" >/dev/null
if grep -q PRIVATE_CANARY "$T/enabled.json"; then exit 1; fi
pass enabled_record_and_secret_redaction
printf 'PRIVATE_CANARY\n' >"$T/app/config/subscriptions/one.json"
ops report >"$T/invalid.json"
jq -e '.automation.subscriptionUpdate==null and (.errors|index("SUBSCRIPTION_SETTINGS_UNAVAILABLE"))!=null' "$T/invalid.json" >/dev/null
if grep -q PRIVATE_CANARY "$T/invalid.json"; then exit 1; fi
pass invalid_is_unknown
rm "$T/app/config/subscriptions/one.json"
for n in $(seq 1 33); do printf '{"enabled":false,"autoUpdateEnabled":false}\n' >"$T/app/config/subscriptions/$n.json"; done
ops report >"$T/bounded.json"
jq -e '.automation.subscriptionUpdate==null and .automationDetails.subscriptionRecordsRead==0' "$T/bounded.json" >/dev/null
pass large_catalog_rejected_before_record_reads
printf '99999999\n' >"$T/app/run/connection-monitor.pid"
ops report >"$T/legacy.json"
jq -e '.serviceDetails[]|select(.service=="connection-monitor")|.state=="ambiguous" and .running==null and .complete==false' "$T/legacy.json" >/dev/null
[ "$(cat "$T/app/run/connection-monitor.pid")" = 99999999 ]
pass legacy_identity_preserved
nonce="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)"
ops begin system subscriptions:scheduler subscriptions USER "$$" cooperative "$nonce" >"$T/begin.json"
id="$(jq -r .operationId "$T/begin.json")"; token="$(jq -r .token "$T/begin.json")"
ops ack "$id" "$token" "$$" >/dev/null
find "$T/state" -type f -exec sha256sum '{}' ';' | sort >"$T/before.txt"
ops report >"$T/active.json"
find "$T/state" -type f -exec sha256sum '{}' ';' | sort >"$T/after.txt"
cmp "$T/before.txt" "$T/after.txt"
jq -e '.fences.global=="managed_active" and .snapshotComplete' "$T/active.json" >/dev/null
if grep -q "$token" "$T/active.json"; then exit 1; fi
ops finish "$id" "$token" completed '' >/dev/null
pass report_read_only_with_real_active_owner
jq -Rn '[inputs]' <"$T/passed.txt" >"$T/tests.json"
jq -n --slurpfile tests "$T/tests.json" '{status:"PASS",tests:$tests[0],applicationInstalled:false,persistentXrayStarted:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
