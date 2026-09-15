#!/opt/bin/ash
set -eu
T=/opt/tmp/broray-311-launch-20260915
RAM=/tmp/broray-311-launch-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-LAUNCH-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; printf '%s\n' BRORAY311-LAUNCH-20260915 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_OPS_GUARD="$T/bin/broray-ops-guard"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
. "$T/app/lib/operation-client.sh"
mkdir "$T/cases"; : >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
case_dir() {
  R="$T/cases/$1"; mkdir -p "$R/state/operations"
  export R BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
  export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
}
nonce() { hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom; }
for point in directory owner state fence published_directory; do
  case_dir "$point"
  launch="$(nonce)"; rc=0
  BRORAY_OPS_TEST=1 BRORAY_OPS_TEST_LAUNCH_CRASH="$point" \
    "$BRORAY_OPS_GUARD" "$R/state/operations.guard" /opt/bin/ash "$T/app/lib/operation-coordinator.sh" \
    begin system subscriptions:scheduler subscriptions USER "$$" cooperative "$launch" >"$R/crash.json" 2>"$R/crash.err" || rc=$?
  [ "$rc" = 137 ]
  [ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
  broray_ops_call status >"$R/status.json"
  count=0; [ "$point" != published_directory ] || count=1
  jq -e --argjson count "$count" '.ok==true and (.operations|length)==$count' "$R/status.json" >/dev/null
  BRORAY_BACKGROUND_LAUNCH_NONCE="$launch"
  broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative
  [ -L "$R/global.lock" ]
  for stage in "$R/state/operations"/.launch-*; do [ ! -e "$stage" ] && [ ! -L "$stage" ]; done
  broray_ops_finish completed
  pass "self_crash_after_$point"
done

case_dir foreign
stage="$R/state/operations/.launch-$$-$(nonce)"; mkdir "$stage"; echo KEEP >"$stage/foreign"
broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative
[ "$(cat "$stage/foreign")" = KEEP ]; broray_ops_finish completed; pass foreign_metadata_preserved

case_dir symlink
mkdir "$R/foreign"; echo KEEP >"$R/foreign/KEEP"
stage="$R/state/operations/.launch-$$-$(nonce)"; ln -s "$R/foreign" "$stage"
broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative
[ -L "$stage" ] && [ "$(cat "$R/foreign/KEEP")" = KEEP ]
broray_ops_finish completed; pass symlink_never_followed

case_dir hardlink
stage="$R/state/operations/.launch-$$-$(nonce)"; mkdir "$stage"; echo KEEP >"$R/KEEP"
ln "$R/KEEP" "$stage/state.json.tmp.$$"
broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative
[ "$(find "$R/KEEP" -maxdepth 0 -type f -links 2 -print)" = "$R/KEEP" ] && [ "$(cat "$stage/state.json.tmp.$$")" = KEEP ]
broray_ops_finish completed; pass hardlink_preserved

cat >"$T/response-shim.sh" <<'SHIM'
broray_ops_call() {
  local rc
  if [ "$1" = "$DROP_METHOD" ] && { [ "$DROP_KIND" = always ] || [ ! -e "$R/dropped-$1" ]; }; then
    rc=0
    "$BRORAY_OPS_GUARD" "$BRORAY_STATE_ROOT/operations.guard" /opt/bin/ash "$BRORAY_ROOT/lib/operation-coordinator.sh" "$@" >"$R/reply-$1" || rc=$?
    [ "$rc" = 0 ] || { cat "$R/reply-$1"; return "$rc"; }
    : >"$R/dropped-$1"
    case "$DROP_KIND" in
      malformed) printf '{"ok":' ;;
      wrong_token) printf '{"ok":true,"operationId":"op-fake","token":"bad"}' ;;
      nonzero) return 74 ;;
    esac
    return 0
  fi
  "$BRORAY_OPS_GUARD" "$BRORAY_STATE_ROOT/operations.guard" /opt/bin/ash "$BRORAY_ROOT/lib/operation-coordinator.sh" "$@"
}
SHIM
. "$T/response-shim.sh"
DROP_METHOD=begin; export DROP_METHOD DROP_KIND
for DROP_KIND in empty malformed wrong_token nonzero; do
  case_dir "response_begin_$DROP_KIND"
  broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative
  broray_ops_finish completed
  count=0; for dir in "$R/state/operations"/op-*; do count=$((count+1)); done; [ "$count" = 1 ]
  pass "response_begin_$DROP_KIND"
done
cat >"$T/response-worker.sh" <<'WORKER'
set -eu
. "$BRORAY_ROOT/lib/operation-client.sh"
. "$TEST_LAUNCH_ROOT/response-shim.sh"
broray_ops_accept_handoff "$HANDOFF_NONCE"
echo yes >"$R/accepted"
broray_ops_finish completed
WORKER
TEST_LAUNCH_ROOT="$T"; export TEST_LAUNCH_ROOT
for DROP_METHOD in handoff accept-handoff; do
  for DROP_KIND in empty malformed; do
    case_dir "response_${DROP_METHOD}_$DROP_KIND"
    broray_ops_begin routes xray:update xray USER protected
    HANDOFF_NONCE="$(nonce)"; export HANDOFF_NONCE
    /opt/bin/ash "$T/response-worker.sh" & worker=$!
    broray_ops_handoff_to "$worker" "$HANDOFF_NONCE"
    [ -z "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ]
    wait "$worker"
    [ -f "$R/accepted" ] && [ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
    pass "response_${DROP_METHOD}_$DROP_KIND"
  done
done
case_dir response_always_lost
DROP_METHOD=begin; DROP_KIND=always; rc=0
broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative || rc=$?
[ "$rc" = 1 ] && [ -z "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ]
for dir in "$R/state/operations"/op-*; do jq -e '.state=="starting" and .acknowledged==false' "$dir/state.json" >/dev/null; done
pass all_responses_lost_never_opens_work_gate
# The only remaining fence was never acknowledged; its caller is this test.
# The transport retires this isolated tree after the caller exits and the
# evidence has been archived. No helper was admitted by this case.

jq -Rn '[inputs|select(length>0)]|{status:"PASS",tests:.,routerAccessed:true,applicationInstalled:false}' <"$T/passed.txt" >"$T/RESULT.json"
cat "$T/RESULT.json"
