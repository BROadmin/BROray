#!/opt/bin/ash
set -eu
umask 077
T=/opt/tmp/broray-311-idle-quiescence-20260916
RAM=/tmp/broray-311-idle-quiescence-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-IDLE-QUIESCENCE-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-IDLE-QUIESCENCE-20260916 >"$RAM/TEST-OWNER"
export PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/sbin" LD_LIBRARY_PATH="$T/lib:/opt/lib"
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
OPS_PROC=/proc; OPS_APP="$T/app"
. "$T/app/lib/operation-owner.sh"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
cat >"$T/bin/broray-home-snapshotd" <<'FIXTURE'
#!/opt/bin/ash
trap 'exit 0' TERM
while :; do sleep 3 & wait $!; done
FIXTURE
/opt/bin/ash "$T/bin/broray-home-snapshotd" >"$T/fixture.log" 2>&1 & parent=$!
sleep 1
broray_ops_capture_owner "$parent" >"$T/owner.json"
cp "/proc/$parent/cmdline" "$T/expected.cmdline"
ticks="$(jq -r '.startTicks' "$T/owner.json")"
boot="$(jq -r '.bootId' "$T/owner.json")"
exe="$(jq -r '.executable' "$T/owner.json")"
rc=0; "$T/bin/quiesce-idle-home" "$parent" "$ticks" wrong-boot "$exe" "$T/expected.cmdline" 3 >"$T/wrong-boot.txt" 2>&1 || rc=$?
[ "$rc" = 75 ]
broray_ops_classify_owner "$(cat "$T/owner.json")"
[ "$OPS_OWNER_STATUS" = ACTIVE ]
rc=0; "$T/bin/quiesce-idle-home" "$parent" "$((ticks+1))" "$boot" "$exe" "$T/expected.cmdline" 3 >"$T/wrong-ticks.txt" 2>&1 || rc=$?
[ "$rc" = 75 ]
broray_ops_classify_owner "$(cat "$T/owner.json")"
[ "$OPS_OWNER_STATUS" = ACTIVE ]
pass mismatched_full_identity_preserves_parent

sleep 8 & canary=$!
for n in 1 2 3 4 5; do
  rc=0; "$T/bin/quiesce-idle-home" "$parent" "$ticks" "$boot" "$exe" "$T/expected.cmdline" 3 >"$T/stopped.json" 2>"$T/stopped.stderr" || rc=$?
  [ "$rc" != 0 ] || break
  [ "$rc" = 75 ]; sleep 1
done
[ "$rc" = 0 ]
jq -e --argjson parent "$parent" --arg ticks "$ticks" '.quiesced and .parentPid==$parent and .parentStartTicks==$ticks' "$T/stopped.json" >/dev/null
rc=0; wait "$parent" || rc=$?
[ "$rc" = 137 ]
kill -0 "$canary"
child="$(jq -r '.sleepPid' "$T/stopped.json")"
birth="$(jq -r '.sleepStartTicks' "$T/stopped.json")"
sleep 4
if [ -d "/proc/$child" ]; then
  [ "$(broray_ops_start_ticks "/proc/$child")" != "$birth" ]
fi
wait "$canary"
pass pinned_idle_parent_stops_sleep_finishes_canary_survives
jq -n --rawfile names "$T/passed.txt" '{status:"PASS",tests:($names|split("\n")|map(select(length>0))),testScope:"standalone ptrace migration helper and private legacy-shell fixtures",installedApplicationChanged:false,persistentXrayStopped:false}' >"$T/RESULT.json"
cat "$T/RESULT.json"
