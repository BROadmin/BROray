#!/opt/bin/ash
# Real ARM flock and client, entirely under isolated test prefixes.
set -eu
umask 077
T=/opt/tmp/broray-311-coordinator-contention-20260916
RAM=/tmp/broray-311-coordinator-contention-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-COORDINATOR-CONTENTION-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-COORDINATOR-CONTENTION-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app" BRORAY_STATE_ROOT="$T/state"
export BRORAY_PROXY_HOST=127.0.0.1 BRORAY_PROXY_PORT=2080 BRORAY_INTERFACE=Proxy0
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock" BRORAY_OPS_UPDATER_ROOT="$T/updater"
export TEST_ROOT="$T"
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/tmp" "$T/app/config/subscriptions" "$T/state"
. "$T/app/lib/subscription-service.sh"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
hold() {
  rm -f "$T/ready"
  "$BRORAY_OPS_GUARD" "$T/state/operations.guard" /opt/bin/ash -c 'echo READY >"$TEST_ROOT/ready"; sleep "$1"' holder "$1" & HOLDER=$!
  n=0; while [ ! -f "$T/ready" ]; do n=$((n+1)); [ "$n" -lt 50 ]; /opt/bin/busybox usleep 100000; done
}
broray_ops_begin system subscriptions:refresh subscriptions USER cooperative
fence="$(readlink "$T/global.lock")"
hold 4
broray_ops_call stop-background >"$T/stop.json"
wait "$HOLDER"
jq -e '.ok and .automationPaused and .operations[0].cancelRequested' "$T/stop.json" >/dev/null
[ "$(readlink "$T/global.lock")" = "$fence" ]
broray_ops_finish aborted CANCELLED
pass stop_waits_for_transient_contention_without_replacing_fence

broray_ops_call resume >/dev/null
before="$(sha256sum "$T/state/background-automation.json")"
hold 9
rc=0; broray_ops_call pause >"$T/busy-response" || rc=$?
[ "$rc" = 75 ] && [ ! -s "$T/busy-response" ]
[ "$(sha256sum "$T/state/background-automation.json")" = "$before" ]
wait "$HOLDER"
pass persistent_contention_is_bounded_and_preserves_state

mkdir "$T/state/operations/op-projection"
printf '%s\n' '{"kind":"background","operationId":"op-projection","running":false,"state":"aborted","errorCode":"CANCELLED","finishedAt":"2026-09-15T22:49:57Z"}' >"$T/state/operations/op-projection/state.json"
printf '%s\n' '{"schemaVersion":1,"id":"test","name":"Test","url":"https://example.test/list","enabled":true,"autoUpdateEnabled":false,"updateIntervalMinutes":60,"lastUpdateStatus":"running","backgroundOperationId":"op-projection","lastUpdatedEpoch":1700000000,"lastUpdatedAt":"2023-11-14T22:13:20Z","lastUpdateResult":{"errorCode":"HTTP_ERROR","durationMs":8000,"warnings":["OLD"]},"createdAt":"2026-09-15T22:00:00Z","updatedAt":"2026-09-15T22:29:06Z","serversReceived":0}' >"$T/app/config/subscriptions/test.json"
before="$(sha256sum "$T/app/config/subscriptions/test.json")"
broray_subscription_summary >"$T/summary.json"
jq -e '.lastUpdatedAt=="2026-09-15T22:49:57Z" and .lastUpdateStatus=="error" and .runningCount==0' "$T/summary.json" >/dev/null
[ "$(sha256sum "$T/app/config/subscriptions/test.json")" = "$before" ]
pass summary_uses_terminal_attempt_time_without_durable_writes
jq -Rn '[inputs] | {status:"PASS",tests:.,installedApplicationChanged:false}' <"$T/passed.txt" >"$T/RESULT.json"
