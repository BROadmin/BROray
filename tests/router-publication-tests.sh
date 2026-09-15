#!/opt/bin/ash
# Actual ARM64 owners; only the test owner/coordinator can signal themselves.
set -eu
umask 077
T=/opt/tmp/broray-311-publication-20260915
RAM=/tmp/broray-311-publication-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-PUBLICATION-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; echo BRORAY311-PUBLICATION-20260915 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib" T
export BRORAY_ROOT="$T/app" BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
[ "$("$BRORAY_OPS_GUARD" --version)" = 'broray-ops-guard/6 flock-fork-exec atomic-fence durable-state durable-append sync-state' ]
mkdir "$T/cases"; : >"$T/passed.txt"
. "$T/app/lib/operation-client.sh"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
case_dir() {
  R="$T/cases/$1"; mkdir -p "$R/state" "$R/app/run"
  ln -s "$T/app/lib" "$R/app/lib"
  export R BRORAY_ROOT="$R/app" BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
  export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
  TARGET="$R/app/run/server-auto-switch-state.json"
}
cat >"$T/common.sh" <<'COMMON'
. "$BRORAY_ROOT/lib/operation-client.sh"
begin() { broray_ops_begin system auto-switch servers USER "${MODE:-cooperative}"; }
prepare() {
  nonce="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)"
  revision="$(jq -r '.revision' "$R/state/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json")"
  jq -nc --arg id "$BRORAY_BACKGROUND_OPERATION_ID" --arg status "${STATUS:-disabled}" \
    '{schemaVersion:3,backgroundOperationId:$id,enabled:false,status:$status,consecutiveFailures:0,candidateCount:0,
      qualityRefresh:{status:"disabled",totalCount:0,checkedCount:0,availableCount:0,unavailableCount:0,errorCount:0}}' >"$R/input.json"
}
publish() { broray_ops_call publish-json "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" auto-state '' "$R/input.json" "$nonce" "$revision"; }
COMMON
. "$T/common.sh"

case_dir normal
begin; prepare; publish >"$R/first.json"
cp "$TARGET" "$R/before.json"; publish >"$R/retry.json"
jq -e '.ok==true and .alreadyPublished==true' "$R/retry.json" >/dev/null
cmp -s "$TARGET" "$R/before.json"
old_nonce="$nonce"; old_revision="$revision"; cp "$R/input.json" "$R/old-input.json"
STATUS=manual-off; prepare; publish >"$R/second.json"; unset STATUS
cp "$TARGET" "$R/second-state.json"; cp "$R/old-input.json" "$R/input.json"
nonce="$old_nonce"; revision="$old_revision"; rc=0; publish >"$R/stale.json" || rc=$?
[ "$rc" = 75 ]; jq -e '.errorCode=="STALE_PUBLICATION"' "$R/stale.json" >/dev/null
cmp -s "$TARGET" "$R/second-state.json"
broray_ops_finish completed
pass exact_retry_and_stale_request_never_overwrites_later_state

case_dir cancel
begin; prepare; broray_ops_call cancel "$BRORAY_BACKGROUND_OPERATION_ID" >/dev/null
rc=0; publish >"$R/cancel.json" || rc=$?
[ "$rc" = 2 ]; [ ! -e "$TARGET" ]; jq -e '.errorCode=="CANCELLED"' "$R/cancel.json" >/dev/null
broray_ops_finish aborted CANCELLED
pass cancellation_before_publication_keeps_old_state

cat >"$T/crash-owner.sh" <<'CRASH'
set -eu
. "$T/common.sh"
begin; prepare
rc=0
env BRORAY_OPS_TEST=1 BRORAY_OPS_TEST_PUBLICATION_CRASH="$POINT" \
  "$BRORAY_OPS_GUARD" "$R/state/operations.guard" /opt/bin/ash "$BRORAY_ROOT/lib/operation-coordinator.sh" \
  publish-json "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" auto-state '' "$R/input.json" "$nonce" "$revision" >"$R/crash.json" 2>"$R/crash.err" || rc=$?
[ "$rc" = 137 ]
echo yes >"$R/crash-confirmed"
kill -KILL "$$"
CRASH
crash_owner() {
  rc=0; /opt/bin/ash "$T/crash-owner.sh" >"$R/owner.out" 2>"$R/owner.err" || rc=$?
  [ "$rc" = 137 ]; [ -f "$R/crash-confirmed" ]
}
for POINT in reserved protected prepared replaced restored; do
  case_dir "crash_$POINT"; export POINT
  crash_owner
  existed=false; if [ -f "$TARGET" ]; then cp "$TARGET" "$R/before.json"; existed=true; fi
  broray_ops_call recover >"$R/recovery.json"
  jq -e '.ok==true and .result=="recovered"' "$R/recovery.json" >/dev/null
  [ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ] || exit 1
  if [ "$existed" = true ]; then cmp -s "$TARGET" "$R/before.json"; else [ ! -e "$TARGET" ]; fi
  broray_ops_call recover | jq -e '.ok==true and .result=="absent"' >/dev/null
  pass "actual_owner_crash_$POINT"
done

case_dir nested
POINT=replaced; MODE=protected; export POINT MODE
crash_owner; unset MODE
rc=0; broray_ops_call recover >"$R/recovery.json" || rc=$?
[ "$rc" = 2 ]; jq -e '.result=="protected_recovery"' "$R/recovery.json" >/dev/null
[ -L "$R/global.lock" ]
pass completed_publication_never_releases_outer_protected_transaction

case_dir changed
crash_owner
echo '{"foreign":true}' >"$TARGET"; cp "$TARGET" "$R/before.json"
rc=0; broray_ops_call recover >"$R/recovery.json" || rc=$?
[ "$rc" = 2 ]; jq -e '.result=="publication_unconfirmed"' "$R/recovery.json" >/dev/null
cmp -s "$TARGET" "$R/before.json"; [ -L "$R/global.lock" ]
pass changed_target_is_preserved_and_recovery_refused

case_dir sync
crash_owner
cat >"$R/sync-failure" <<'SYNC'
#!/opt/bin/ash
[ "$1" != --sync-state ] || exit 74
exec "$T/bin/broray-ops-guard" "$@"
SYNC
chmod 700 "$R/sync-failure"
BRORAY_OPS_GUARD="$R/sync-failure"; export BRORAY_OPS_GUARD
rc=0; broray_ops_call recover >"$R/recovery-failed.json" || rc=$?
[ "$rc" = 2 ]; [ -L "$R/global.lock" ]
BRORAY_OPS_GUARD="$T/bin/broray-ops-guard"; export BRORAY_OPS_GUARD
broray_ops_call recover | jq -e '.ok==true' >/dev/null
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ] || exit 1
pass simulated_sync_failure_preserves_fence_until_confirmed_retry

# Every remaining protected fixture owner has exited; no helpers were started.
jq -Rn '[inputs|select(length>0)]|{status:"PASS",tests:.,environment:"actual ARM64 owners, isolated publication fixtures and controlled self-crashes",applicationInstalled:false}' <"$T/passed.txt" >"$T/RESULT.json"
