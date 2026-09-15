#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-journal-20260915
RAM=/tmp/broray-311-journal-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-JOURNAL-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-JOURNAL-20260915 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir "$T/cases"
. "$T/app/lib/operation-client.sh"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
case_dir() {
  R="$T/cases/$1"; mkdir -p "$R/state"
  export R BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
  export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
  J="$R/state/operation-events"
}
completed() {
  broray_ops_begin system servers:check servers USER cooperative
  broray_ops_finish completed
}
view() { broray_ops_call events >"$R/events.json"; }

case_dir normal
completed; completed; view
jq -e '.complete==true and [.events[].sequence]==[1,2,3,4,5,6]' "$R/events.json" >/dev/null
[ "$(tail -n 1 "$J/events.jsonl" | sha256sum | awk '{print $1}')" = "$(jq -r '.lastHash' "$J/head.json")" ]
jq -e '.pending==false and .allocatedSequence==6 and .lastSequence==6' "$J/head.json" >/dev/null
pass monotonic_sequence_and_durable_tail_witness

case_dir tail_loss
completed
cp "$J/events.jsonl" "$R/before.jsonl"
sed '$d' "$R/before.jsonl" >"$J/events.jsonl"
view; jq -e '.complete==false and .errors==["JOURNAL_GAP"]' "$R/events.json" >/dev/null
completed; view
jq -e '.complete==false and .events[-1].sequence==6' "$R/events.json" >/dev/null
pass complete_tail_loss_remains_detectable_after_append

case_dir segment_loss
completed; cp "$J/events.jsonl" "$R/before.jsonl"; rm "$J/events.jsonl"
view; jq -e '.complete==false' "$R/events.json" >/dev/null
completed; view; jq -e '.complete==false' "$R/events.json" >/dev/null
pass missing_segment_is_not_silently_reinitialized

case_dir valid_tamper
completed; cp "$J/events.jsonl" "$R/before.jsonl"
sed '$s/"source":"USER"/"source":"SYSTEM_RECOVERY"/' "$R/before.jsonl" >"$J/events.jsonl"
view; jq -e '.complete==false' "$R/events.json" >/dev/null
pass changed_valid_tail_is_detected_by_hash

for point in reserved appended; do
  case_dir "crash_$point"
  broray_ops_begin system servers:check servers USER cooperative
  # Real /proc identity, explicit private-root test hook. Only the coordinator
  # sends SIGKILL to itself. The foreground shell never signals a stored PID.
  rc=0
  env BRORAY_OPS_TEST=1 BRORAY_OPS_TEST_JOURNAL_CRASH="$point" \
    "$BRORAY_OPS_GUARD" "$R/state/operations.guard" /opt/bin/ash "$T/app/lib/operation-coordinator.sh" \
    tick "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" fetching >"$R/crash.txt" 2>"$R/crash.err" || rc=$?
  [ "$rc" = 137 ]
  jq -e '.pending==true and .allocatedSequence==3' "$J/head.json" >/dev/null
  view; jq -e '.complete==false' "$R/events.json" >/dev/null
  broray_ops_finish completed
  [ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ]
  view; jq -e '.complete==false and .events[-1].sequence==4' "$R/events.json" >/dev/null
  pass "self_crash_${point}_keeps_gap_and_allows_safe_job_finish"
done

case_dir append_safety
echo '{}' >"$R/record"
echo KEEP >"$R/foreign"
ln -s "$R/foreign" "$R/target"
rc=0; "$BRORAY_OPS_GUARD" --append-file "$R/record" "$R/target" || rc=$?
[ "$rc" = 74 ]; [ "$(cat "$R/foreign")" = KEEP ]
rm "$R/target"; ln "$R/foreign" "$R/target"
rc=0; "$BRORAY_OPS_GUARD" --append-file "$R/record" "$R/target" || rc=$?
[ "$rc" = 74 ]; [ "$(cat "$R/foreign")" = KEEP ]
pass native_appender_rejects_symlink_and_hardlink_targets

jq -n --rawfile tests "$T/passed.txt" '{status:"PASS",tests:($tests|split("\n")|map(select(length>0))),environment:"physical ARM64 durable journal, real owner identities, controlled coordinator self-crashes and simulated record loss",applicationInstalled:false}' >"$T/RESULT.json"
