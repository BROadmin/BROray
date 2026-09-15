#!/opt/bin/ash
# Production installer/coordinator; native version fixtures, no real VPN.
set -eu
umask 077
T=/opt/tmp/broray-311-xray-jobs-20260915
RAM=/tmp/broray-311-xray-jobs-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-XRAY-JOBS-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; echo BRORAY311-XRAY-JOBS-20260915 >"$RAM/TEST-OWNER"
PATH="$T/app/bin:$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/bin" "$T/app/config" "$T/app/tmp" "$T/app/run" "$T/cases"
ln -s "$T/bin/jq" "$T/app/bin/jq"
cat >"$T/app/bin/curl" <<'CURL'
#!/opt/bin/ash
case "${TEST_MODE:-normal}" in
 wait) echo ready >"$TEST_READY"; trap '' TERM; sleep 60; exit 28 ;;
 error) exit 28 ;;
esac
out=; url=
while [ "$#" -gt 0 ]; do
 case "$1" in -o) out="$2"; shift ;; https://*) url="$1" ;; esac
 shift
done
[ -n "$out" ] || exit 2
case "$url" in
 */releases/tags/*) cp "$TEST_FIXTURE/release.json" "$out" ;;
 *.zip.dgst) cp "$TEST_FIXTURE/digest" "$out" ;;
 *.zip) cp "$TEST_FIXTURE/candidate.zip" "$out" ;;
 *) exit 22 ;;
esac
CURL
cat >"$T/app/bin/fixture-init" <<'INIT'
#!/opt/bin/ash
[ "${TEST_FAIL_ROLLBACK:-0}" != 1 ]
INIT
chmod 700 "$T/app/bin/curl" "$T/app/bin/fixture-init"
export TEST_FIXTURE="$T/bin" BRORAY_XRAY_INIT="$T/app/bin/fixture-init"
export BRORAY_XRAY_CONFIG="$T/app/config/config.json"
echo '{"PRIVATE_CANARY":"unchanged"}' >"$BRORAY_XRAY_CONFIG"
cp "$BRORAY_XRAY_CONFIG" "$T/config-before.json"
. "$T/app/lib/operation-client.sh"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
case_dir() {
  R="$T/cases/$1"; mkdir -p "$R/state" "$R/runtime"
  export R BRORAY_STATE_ROOT="$R/state" BRORAY_ROUTES_API_LOCK="$R/global.lock"
  export BRORAY_OPS_UPDATER_ROOT="$R/updater" BRORAY_LEGACY_GLOBAL_LOCK="$R/legacy.lock"
  export BRORAY_XRAY_BINARY="$R/runtime/xray" TEST_READY="$R/ready"
  export TEST_MODE=normal TEST_XRAY_FAIL_INSTALLED=0 TEST_FAIL_ROLLBACK=0
  cp "$T/bin/xray-install-old" "$BRORAY_XRAY_BINARY"; chmod 755 "$BRORAY_XRAY_BINARY"
}
install_job() { /opt/bin/ash "$T/app/bin/broray" xray install "$T/bin/request.json"; }
terminal() {
  [ ! -e "$R/global.lock" ] && [ ! -L "$R/global.lock" ] || exit 1
  for file in "$R/state/operations/"*/state.json; do jq -e --arg state "$1" '.state==$state' "$file" >/dev/null; done
  for file in "$R/state/operations/"*/supervisors.json; do [ ! -e "$file" ] || jq -e '.supervisors==[]' "$file" >/dev/null; done
}
retire_finished_binary() {
  # Its exact bytes are already present in the archived native input fixtures.
  # Record this case's digest before retiring the completed private copy.
  [ -f "$BRORAY_XRAY_BINARY" ] && [ ! -L "$BRORAY_XRAY_BINARY" ] || exit 1
  case "$BRORAY_XRAY_BINARY" in "$T/cases/"*/runtime/xray) ;; *) return 1 ;; esac
  sha256sum "$BRORAY_XRAY_BINARY" >"$R/binary-result.sha256"
  rm "$BRORAY_XRAY_BINARY"
}

case_dir unowned
rc=0
/opt/bin/ash -c '. "$BRORAY_ROOT/lib/xray-control.sh"; . "$BRORAY_ROOT/lib/xray-update.sh"; broray_xray_update_install install "$TEST_FIXTURE/request.json"' >"$R/out" 2>"$R/err" || rc=$?
[ "$rc" = 73 ]; cmp -s "$BRORAY_XRAY_BINARY" "$T/bin/xray-install-old"
pass unowned_installer_cannot_mutate_binary
retire_finished_binary

case_dir normal
install_job >"$R/result.json" 2>"$R/err"
jq -e '.success==true and .version=="26.9.15" and .running==false' "$R/result.json" >/dev/null
cmp -s "$BRORAY_XRAY_BINARY" "$T/bin/xray-install-new"
cmp -s "$BRORAY_XRAY_CONFIG" "$T/config-before.json"
terminal completed
pass staged_native_binary_installed_with_config_preserved
retire_finished_binary

case_dir rollback
TEST_XRAY_FAIL_INSTALLED=1; rc=0
install_job >"$R/result.json" 2>"$R/err" || rc=$?
[ "$rc" = 1 ]; jq -e '.success==false and .rollbackSuccess==true and .rolledBack==true' "$R/result.json" >/dev/null
cmp -s "$BRORAY_XRAY_BINARY" "$T/bin/xray-install-old"; terminal failed
pass failed_final_validation_restores_verified_previous_binary
retire_finished_binary

case_dir failed_rollback
TEST_XRAY_FAIL_INSTALLED=1; TEST_FAIL_ROLLBACK=1; rc=0
install_job >"$R/result.json" 2>"$R/err" || rc=$?
[ "$rc" = 75 ]; jq -e '.success==false and .rollbackSuccess==false and .rolledBack==false' "$R/result.json" >/dev/null
[ -L "$R/global.lock" ]
[ "$(find "$R/runtime" -name 'xray.broray-*-backup' | wc -l)" = 1 ]
pass failed_rollback_preserves_backup_and_protected_fence

case_dir cancel
TEST_MODE=wait
install_job >"$R/result.json" 2>"$R/err" & worker=$!
n=0; while [ ! -f "$TEST_READY" ]; do n=$((n+1)); [ "$n" -lt 250 ] || exit 1; /opt/bin/busybox usleep 100000; done
broray_ops_call status >"$R/status.json"
operation="$(jq -er '.operations[0].operationId' "$R/status.json")"
broray_ops_call cancel "$operation" >"$R/cancel.json"
rc=0; wait "$worker" || rc=$?
[ "$rc" = 130 ]; cmp -s "$BRORAY_XRAY_BINARY" "$T/bin/xray-install-old"; terminal aborted
pass download_cancellation_drains_helper_and_keeps_previous_binary
retire_finished_binary

case_dir web
TEST_MODE=wait
CONTENT_LENGTH="$(wc -c <"$T/bin/request.json")" XRAY_WEB_OPERATION_MODE=install \
  /opt/bin/ash -c '. "$BRORAY_ROOT/web-new/api/auth-common.sh"; . "$BRORAY_ROOT/lib/xray-web-operation.sh"' \
  <"$T/bin/request.json" >"$R/http.txt" 2>"$R/err"
grep -q '202 Accepted' "$R/http.txt"
n=0; while [ ! -f "$TEST_READY" ]; do n=$((n+1)); [ "$n" -lt 250 ] || exit 1; /opt/bin/busybox usleep 100000; done
broray_ops_call status >"$R/status.json"
operation="$(jq -er '.operations[0].operationId' "$R/status.json")"
jq -e '.operations[0].ownerStatus=="ACTIVE"' "$R/status.json" >/dev/null
broray_ops_call cancel "$operation" >"$R/cancel.json"
n=0; while [ -L "$R/global.lock" ]; do n=$((n+1)); [ "$n" -lt 350 ] || exit 1; /opt/bin/busybox usleep 100000; done
terminal aborted
. "$T/app/lib/xray-web-status.sh"
broray_xray_web_status >"$R/view.json"
jq -e '.complete==true and .operationRunning==false and .result.success==false and .backgroundOperation.state=="aborted"' "$R/view.json" >/dev/null
cmp -s "$BRORAY_XRAY_BINARY" "$T/bin/xray-install-old"
pass web_executor_survives_cgi_exit_and_remains_cancellable
retire_finished_binary

case_dir legacy
mkdir -p "$T/app/update/xray.lock"; echo KEEP >"$T/app/update/xray.lock/foreign"
rc=0; install_job >"$R/result.json" 2>"$R/err" || rc=$?
[ "$rc" != 0 ]; [ "$(cat "$T/app/update/xray.lock/foreign")" = KEEP ]
cmp -s "$BRORAY_XRAY_BINARY" "$T/bin/xray-install-old"
pass ambiguous_legacy_xray_lock_is_preserved

jq -n --rawfile tests "$T/passed.txt" '{status:"PASS",tests:($tests|split("\n")|map(select(length>0))),environment:"physical ARM64 production Xray installer and coordinator with native binary and network fixtures",applicationInstalled:false}' >"$T/RESULT.json"
