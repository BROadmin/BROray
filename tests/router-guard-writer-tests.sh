#!/opt/bin/ash
# Real fork/exec and parent self-SIGKILL; no PID-file signals or service changes.
set -eu
umask 077
T=/opt/tmp/broray-311-guard-writer-20260915
RAM=/tmp/broray-311-guard-writer-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-GUARD-WRITER-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; printf '%s\n' BRORAY311-GUARD-WRITER-20260915 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
"$BRORAY_OPS_GUARD" --version >"$T/guard-version.txt"
uname -a >"$T/kernel.txt"
. "$T/app/lib/operation-client.sh"
mkdir "$T/cases"; : >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }

for mode in replace-file publish-fence; do
  R="$T/cases/$mode"; mkdir "$R"
  if [ "$mode" = replace-file ]; then
    source="$R/new.json"; target="$R/state.json"
    printf '{"complete":true}\n' >"$source"
    printf '{"complete":false}\n' >"$target"
  else
    source="$R/operation/fence"; target="$R/global.lock"
    mkdir -p "$source"
    for name in owner.json state.json; do echo '{}' >"$R/operation/$name"; done
    for name in owner.json pid scope action bundle startedAt; do echo test >"$source/$name"; done
  fi
  rc=0
  "$BRORAY_OPS_GUARD" "$R/guard" "$T/bin/writer-fixture" "$BRORAY_OPS_GUARD" \
    "$R/ready" "$R/release" "--$mode" "$source" "$target" >"$R/parent.out" 2>"$R/parent.err" || rc=$?
  contender=0
  "$BRORAY_OPS_GUARD" "$R/guard" /opt/bin/busybox true || contender=$?
  # Always release the bounded child before checking assertions.
  : >"$R/release"
  "$BRORAY_OPS_GUARD" "$R/guard" /opt/bin/busybox true
  [ "$rc" = 137 ] && [ -s "$R/ready" ] && [ "$contender" = 75 ] || exit 1
  if [ "$mode" = replace-file ]; then
    jq -e '.complete==true' "$target" >/dev/null
  else
    [ -L "$target" ] && [ "$(readlink "$target")" = "$source" ] || exit 1
  fi
  pass "orphan_${mode}_retains_exclusion_until_complete"
done

R="$T/cases/client"; mkdir -p "$R/state"
export R BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
broray_ops_begin system subscriptions:scheduler subscriptions USER cooperative
pass client_begin_and_ack
for n in 1 2 3; do
  broray_ops_run_helper 10 -- /opt/bin/busybox true
  jq -e '.supervisors==[]' "$R/state/operations/$BRORAY_BACKGROUND_OPERATION_ID/supervisors.json" >/dev/null
done
pass helper_registration_drain_no_descriptor_deadlock
broray_ops_tick committing
broray_ops_finish completed
[ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ] || exit 1
pass commit_and_finish
jq -Rn '[inputs|select(length>0)]|{status:"PASS",tests:.,routerAccessed:true,applicationInstalled:false}' <"$T/passed.txt" >"$T/RESULT.json"
cat "$T/RESULT.json"
