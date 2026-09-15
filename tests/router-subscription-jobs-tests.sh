#!/opt/bin/ash
# Production subscription functions; local fixture transport and private files.
set -eu
umask 077
T=/opt/tmp/broray-311-subscription-jobs-20260915
RAM=/tmp/broray-311-subscription-jobs-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-SUBSCRIPTION-JOBS-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-SUBSCRIPTION-JOBS-20260915 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app"
export BRORAY_PROXY_HOST=127.0.0.1 BRORAY_PROXY_PORT=2080 BRORAY_INTERFACE=Proxy0
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/tmp" "$T/app/config/subscriptions" "$T/app/bin" "$T/cases"
ln -s "$T/bin/jq" "$T/app/bin/jq"
cat >"$T/app/bin/curl" <<'CURL'
#!/opt/bin/ash
case "${TEST_MODE:-normal}" in
  wait) trap "" TERM; echo ready >"$TEST_READY"; sleep 60; exit 28 ;;
  error) exit 28 ;;
esac
while [ "$#" -gt 0 ]; do
  case "$1" in --dump-header) headers="$2"; shift ;; --output) body="$2"; shift ;; esac
  shift
done
printf 'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n' >"$headers"
cp "$TEST_PAYLOAD" "$body"
printf 200
CURL
chmod 700 "$T/app/bin/curl"
printf '%s\n' 'vless://11111111-2222-4333-8444-555555555555@93.184.216.34:443?security=tls&type=tcp&sni=example.invalid#Fixture' >"$T/payload.txt"
export TEST_PAYLOAD="$T/payload.txt" TEST_MODE=normal
PATH="$T/app/bin:$PATH"; export PATH
. "$T/app/lib/subscription-service.sh"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
case_dir() {
  R="$T/cases/$1"; mkdir -p "$R/state"
  export R BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
  export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
  export TEST_READY="$R/ready"
}
record() {
  jq -nc --arg status "${1:-never}" '{schemaVersion:1,id:"test",name:"Test",url:"https://93.184.216.34/sub/PRIVATE_CANARY",
    clientHwid:"broray-1234567890abcdef1234567890abcdef",enabled:true,autoUpdateEnabled:true,updateIntervalMinutes:60,
    lastUpdateStatus:$status,nextUpdateEpoch:1,createdAt:"2026-09-15T00:00:00Z",updatedAt:"2026-09-15T00:00:00Z",serversReceived:0}' >"$T/app/config/subscriptions/test.json"
}
begin() { broray_job_begin system subscriptions:refresh subscriptions USER cooperative; }
catalog_hash() { find "$T/app/servers" -name '*.json' -type f -exec sha256sum '{}' ';' | sort | sha256sum; }
cat >"$T/job.sh" <<'JOB'
. "$BRORAY_ROOT/lib/subscription-service.sh"
broray_job_begin system subscriptions:refresh subscriptions USER cooperative || exit $?
trap 'broray_job_exit "$?"' EXIT
broray_subscription_update test manual
exit $?
JOB

case_dir readonly
record running
mkdir -p "$T/app/run/subscriptions/test.lock"
echo KEEP >"$T/app/run/subscriptions/test.lock/foreign"
cp "$T/app/config/subscriptions/test.json" "$R/before.json"
broray_subscription_list >"$R/list.json"
broray_subscription_summary >"$R/summary.json"
cmp -s "$R/before.json" "$T/app/config/subscriptions/test.json"
[ "$(cat "$T/app/run/subscriptions/test.lock/foreign")" = KEEP ]
rm "$T/app/run/subscriptions/test.lock/foreign"; rmdir "$T/app/run/subscriptions/test.lock"
pass get_preserves_state_and_unknown_lock

case_dir normal
record
/opt/bin/ash "$T/job.sh" >"$R/result.json" 2>"$R/error.txt"
jq -e '.lastUpdateStatus=="success" and .lastUpdateResult.accepted==1' "$R/result.json" >/dev/null
count=0; for file in "$T/app/servers"/*.json; do [ -f "$file" ]; count=$((count+1)); done; [ "$count" = 1 ]
for file in "$R/state/operations"/*/state.json; do jq -e '.state=="completed"' "$file" >/dev/null; done
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
pass actual_parser_and_catalog_commit

case_dir inherited
begin
rc=0
( broray_subscription_write_json "$T/app/config/subscriptions/test.json" "$T/app/config/subscriptions/test.json" ) || rc=$?
[ "$rc" = 2 ]; broray_job_finish completed
pass inherited_token_cannot_authorize_subshell_writer

case_dir error
record; TEST_MODE=error
before="$(catalog_hash)"; rc=0
/opt/bin/ash "$T/job.sh" >"$R/result.json" 2>"$R/error.txt" || rc=$?
[ "$rc" = 1 ]; [ "$(catalog_hash)" = "$before" ]
jq -e '.lastUpdateStatus=="error"' "$T/app/config/subscriptions/test.json" >/dev/null
for file in "$R/state/operations"/*/state.json; do jq -e '.state=="failed"' "$file" >/dev/null; done
pass failed_download_retains_catalog_and_records_failure

case_dir cancel
record; TEST_MODE=wait
before="$(catalog_hash)"
/opt/bin/ash "$T/job.sh" >"$R/result.json" 2>"$R/error.txt" & worker=$!
n=0; while [ ! -f "$TEST_READY" ]; do n=$((n+1)); [ "$n" -lt 150 ] || exit 1; /opt/bin/busybox usleep 100000; done
broray_ops_call status >"$R/status.json"
operation="$(jq -er '.operations[0].operationId' "$R/status.json")"
broray_ops_call cancel "$operation" >"$R/cancel.json"
rc=0; wait "$worker" || rc=$?
[ "$rc" = 130 ] && [ "$(catalog_hash)" = "$before" ]
jq -e '.state=="aborted"' "$R/state/operations/$operation/state.json" >/dev/null
jq -e '.supervisors==[]' "$R/state/operations/$operation/supervisors.json" >/dev/null
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
pass cancelled_tree_drained_before_fence_release
cp "$T/app/config/subscriptions/test.json" "$R/cancelled-durable.json"
broray_subscription_get test >"$R/public.json"
broray_subscription_summary >"$R/summary.json"
jq -e '.lastUpdateStatus=="error"' "$R/public.json" >/dev/null
jq -e '.runningCount==0 and .errorCount==1' "$R/summary.json" >/dev/null
cmp -s "$R/cancelled-durable.json" "$T/app/config/subscriptions/test.json"
pass cancelled_status_is_projected_without_get_mutation

case_dir paused
record; TEST_MODE=normal
cp "$T/app/config/subscriptions/test.json" "$R/before.json"
broray_ops_call pause >"$R/paused.json"
/opt/bin/ash "$T/app/bin/broray-subscription-scheduler" --once
cmp -s "$R/before.json" "$T/app/config/subscriptions/test.json"
for file in "$R/state/operations"/*/state.json; do [ ! -e "$file" ]; done
pass paused_scheduler_performs_no_update

case_dir scheduler_error
record; TEST_MODE=error
rc=0
/opt/bin/ash "$T/app/bin/broray-subscription-scheduler" --once >"$R/output" 2>"$R/error.txt" || rc=$?
[ "$rc" = 1 ]
for file in "$R/state/operations"/*/state.json; do jq -e '.state=="failed" and .source=="SUBSCRIPTION_AUTO"' "$file" >/dev/null; done
[ ! -e "$T/app/run/subscription-scheduler.pid" ]
pass scheduler_records_actual_job_failure

case_dir cli_error
record; TEST_MODE=error
rc=0
/opt/bin/ash "$T/app/bin/broray-subscriptions" refresh test >"$R/output" 2>"$R/error.txt" || rc=$?
[ "$rc" = 1 ]
for file in "$R/state/operations"/*/state.json; do jq -e '.state=="failed" and .source=="USER" and .operation=="subscriptions:refresh"' "$file" >/dev/null; done
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
pass cli_refresh_has_owned_lifecycle

case_dir api_error
record; TEST_MODE=error
/opt/bin/ash -c '. "$BRORAY_ROOT/web-new/api/subscriptions/common.sh"; broray_subscriptions_api_lock refresh; broray_subscriptions_api_run broray_subscription_update test manual' >"$R/response.txt"
grep -q '504 Gateway Timeout' "$R/response.txt"
for file in "$R/state/operations"/*/state.json; do jq -e '.state=="failed"' "$file" >/dev/null; done
broray_ops_call report >"$R/report.json"
! grep -q PRIVATE_CANARY "$R/report.json"
pass api_zero_exit_business_error_records_failed_job

jq -Rn '[inputs|select(length>0)]|{status:"PASS",tests:.,routerAccessed:true,applicationInstalled:false,transport:"local fixture; no external subscription request"}' <"$T/passed.txt" >"$T/RESULT.json"
cat "$T/RESULT.json"
