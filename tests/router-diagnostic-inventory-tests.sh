#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-diagnostic-inventory-20260916
RAM=/tmp/broray-311-diagnostic-inventory-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-DIAGNOSTIC-INVENTORY-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; echo BRORAY311-DIAGNOSTIC-INVENTORY-20260916 >"$RAM/TEST-OWNER"
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
jq -e '(.serviceDetails|length)==5 and ([.serviceDetails[].service]|index("home-snapshot"))!=null and ([.serviceDetails[].service]|index("interface-reconcile"))!=null' "$T/empty.json" >/dev/null
pass five_background_services_reported
mkdir -p "$T/app/run/home-snapshots" "$T/app/runtime"
cp "$T/bin/diagnostic-runtime" "$T/app/runtime/xray"
printf '{}\n' >"$T/app/config/config.json"
cache_xray() {
  jq -nc --argjson pid "$1" --argjson age "$2" --argjson now "$(date +%s)" --arg stamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{schemaVersion:1,module:"xray",capturedAt:$stamp,capturedEpoch:($now-$age),data:{pid:$pid,version:"26.9.9",running:true,configCheckOutput:"PRIVATE_CANARY"}}' >"$T/app/run/home-snapshots/xray.json"
}
cache_xray 99999999 0
ops report >"$T/stale-pid.json"
jq -e '.runtimeDetails.xray.state=="unknown" and .runtimeDetails.xray.identity==null and .runtimeDetails.xray.cachedVersion=="26.9.9"' "$T/stale-pid.json" >/dev/null
pass cached_pid_does_not_prove_live_runtime
"$T/app/runtime/xray" run -c "$T/app/config/config.json" &
runtime_child=$!
cache_xray "$runtime_child" 0
ops report >"$T/live-runtime.json"
jq -e --argjson pid "$runtime_child" '.runtimeDetails.xray.state=="running" and .runtimeDetails.xray.identity.verified and .runtimeDetails.xray.identity.pid==$pid and .runtimeDetails.xray.identity.role=="persistent-xray"' "$T/live-runtime.json" >/dev/null
: >"$T/app/config/config.json.stop"
wait "$runtime_child"
rm "$T/app/config/config.json.stop"
pass actual_proc_identity_without_command_digest
"$T/app/runtime/xray" run -test -c "$T/app/config/config.json" &
validator_child=$!
cache_xray "$validator_child" 0
ops report >"$T/validator.json"
jq -e '.runtimeDetails.xray.identity==null and .runtimeDetails.xray.state=="unknown"' "$T/validator.json" >/dev/null
: >"$T/app/config/config.json.stop"
wait "$validator_child"
pass validator_is_not_persistent_runtime
cache_xray 99999999 601
ops report >"$T/expired.json"
jq -e '.runtimeDetails.xray.cache==null' "$T/expired.json" >/dev/null
pass expired_cache_not_reused
jq -nc --argjson now "$(date +%s)" --arg stamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '{schemaVersion:1,module:"broray",capturedAt:$stamp,capturedEpoch:$now,data:{lastOperation:{operationId:"update-20260916001050-30079",operation:"update",state:"success",running:false,error:"PRIVATE_CANARY",message:"PRIVATE_CANARY"}}}' >"$T/app/run/home-snapshots/broray.json"
ops report >"$T/updater-cache.json"
jq -e '.runtimeDetails.updater.lastOperation.state=="success" and .runtimeDetails.updater.liveState=="unknown" and .snapshotConsistent==false' "$T/updater-cache.json" >/dev/null
for file in "$T/stale-pid.json" "$T/live-runtime.json" "$T/validator.json" "$T/updater-cache.json"; do
  if grep -qE 'PRIVATE_CANARY|commandDigest|bootId' "$file"; then exit 1; fi
done
pass updater_allowlist_and_no_live_or_secret_claim
jq -Rn '[inputs]' <"$T/passed.txt" >"$T/tests.json"
jq -n --slurpfile tests "$T/tests.json" '{status:"PASS",tests:$tests[0],applicationInstalled:false,persistentXrayStarted:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
