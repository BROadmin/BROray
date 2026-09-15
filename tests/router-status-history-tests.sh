#!/opt/bin/ash
# Isolated ARM comparison and cancellation response contract; no installed writes.
set -eu
umask 077
T=/opt/tmp/broray-311-status-history-20260916
RAM=/tmp/broray-311-status-history-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-STATUS-HISTORY-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-STATUS-HISTORY-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app" BRORAY_STATE_ROOT="$T/state"
export BRORAY_PROXY_HOST=127.0.0.1 BRORAY_PROXY_PORT=2080 BRORAY_INTERFACE=Proxy0
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock" BRORAY_OPS_UPDATER_ROOT="$T/updater"
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/tmp" "$T/app/config/subscriptions" "$T/state"
. "$T/app/lib/operation-client.sh"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
clock() { awk '{print $1;exit}' /proc/uptime; }
broray_ops_begin system subscriptions:refresh subscriptions USER cooperative
template="$T/state/operations/$BRORAY_BACKGROUND_OPERATION_ID"
broray_ops_finish completed
n=1
while [ "$n" -le 18 ]; do
  id="$(printf 'op-20260916000000-900001-%012d' "$n")"
  dir="$T/state/operations/$id"; mkdir "$dir"
  for file in owner.json state.json; do jq --arg id "$id" '.operationId=$id' "$template/$file" >"$dir/$file"; done
  n=$((n+1))
done
find "$T/state/operations" -type f -exec sha256sum '{}' '+' | sort >"$T/before.sha256"
start="$(clock)"
"$BRORAY_OPS_GUARD" "$T/state/operations.guard" /opt/bin/ash "$T/bin/baseline-controller" status >"$T/old.json"
end="$(clock)"; old="$(awk -v start="$start" -v end="$end" 'BEGIN {printf "%.3f",end-start}')"
start="$(clock)"; broray_ops_call status >"$T/new.json"; end="$(clock)"
new="$(awk -v start="$start" -v end="$end" 'BEGIN {printf "%.3f",end-start}')"
jq -n --argjson old "$old" --argjson new "$new" '{terminalRecords:19,baselineSeconds:$old,newSeconds:$new}' >"$T/timing.json"
cat "$T/timing.json"
jq -e '.complete and .globalFence=="absent" and (.operations|length)==19 and all(.operations[];.running==false and .ownerStatus=="FINISHED")' "$T/new.json" >/dev/null
awk -v old="$old" -v new="$new" 'BEGIN {exit !(new<old/2 && new<2)}'
find "$T/state/operations" -type f -exec sha256sum '{}' '+' | sort >"$T/after.sha256"
cmp -s "$T/before.sha256" "$T/after.sha256"
pass terminal_history_read_under_two_seconds_and_at_least_twice_faster

/opt/bin/ash -c '
 . "$BRORAY_ROOT/web-new/api/subscriptions/common.sh"
 broray_subscriptions_api_lock refresh
 early() { broray_ops_call cancel "$BRORAY_BACKGROUND_OPERATION_ID" >/dev/null || return 1; return 75; }
 broray_subscriptions_api_run early
' >"$T/early-cancel.http"
grep -q 'Status: 409 Conflict' "$T/early-cancel.http"
grep -q 'OPERATION_CANCELLED' "$T/early-cancel.http"
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
pass confirmed_early_cancel_returns_409_and_releases_fence

/opt/bin/ash -c '
 . "$BRORAY_ROOT/web-new/api/subscriptions/common.sh"
 broray_subscriptions_api_lock refresh
 unresolved() { broray_ops_call cancel "$BRORAY_BACKGROUND_OPERATION_ID" >/dev/null || return 1; BRORAY_JOB_UNRESOLVED=true; return 75; }
 broray_subscriptions_api_run unresolved
' >"$T/unresolved.http"
grep -q 'Status: 503 Service Unavailable' "$T/unresolved.http"
grep -q 'OPERATION_UNRESOLVED' "$T/unresolved.http"
[ -L "$T/global.lock" ]
pass unresolved_cancel_keeps_fence_and_returns_503
# The controlled test owner exited without any helpers or domain writes.
broray_ops_call recover >"$T/recovery.json"
jq -e '.ok and .result=="recovered"' "$T/recovery.json" >/dev/null
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
pass explicit_recovery_after_test_owner_exit
jq -Rn '[inputs] | {status:"PASS",tests:.,installedApplicationChanged:false}' <"$T/passed.txt" >"$T/RESULT.json"
