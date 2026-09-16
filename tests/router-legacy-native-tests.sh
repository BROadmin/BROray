#!/opt/bin/ash
# TEST-ONLY prefix build: ARM ptrace/fsync, not full legacy recovery.
set -eu
umask 077
T=/opt/tmp/broray-311-legacy-native-20260916
RAM=/tmp/broray-311-legacy-native-20260916
MARKER=BRORAY311-LEGACY-NATIVE-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = "$MARKER" ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; printf '%s\n' "$MARKER" >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib" T
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
[ "$(cat /opt/broray/current/.broray-slot)" = 3.1.1-r07c01--update-20260916010925-22011 ]
OPS_APP=/opt/broray; OPS_PROC=/proc
. /opt/broray/lib/operation-owner.sh
. /opt/broray/lib/xray-process.sh
installed_pid="$(broray_xray_runtime_pid)"
broray_ops_capture_owner "$installed_pid" >"$T/installed-xray.before"
sha256sum /opt/broray/config/config.json /opt/var/lib/broray-updater/release-index-url >"$T/installed-files.before"
[ ! -e /opt/var/lock/broray/global-operation.lock ] && [ ! -L /opt/var/lock/broray/global-operation.lock ]
A="$T/opt/broray"; L="$T/opt/var/lock/broray/global-operation.lock"
mkdir -p "$A/bin" "$A/runtime" "$A/config" "$A/run" "$L" "$T/opt/var/lib/broray/legacy-recovery"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
ticks() { sed 's/.*) //' "/proc/$1/stat" | awk '{print $20}'; }
new_session() {
    S="$T/opt/var/lib/broray/legacy-recovery/$(printf '%032d' "$1")"
    mkdir -m 700 "$S"
    cat >"$S/preflight.sh" <<'CALLBACK'
#!/opt/bin/ash
set -eu
S="$1"
test -d "$T/opt/var/lock/broray/global-operation.lock"
web="$(cat "$T/web.pid")"; xray="$(cat "$T/xray.pid")"
[ "$(awk '/^TracerPid:/ {print $2}' "/proc/$web/status")" = "$BRORAY_LEGACY_BARRIER_PID" ]
[ "$(awk '/^TracerPid:/ {print $2}' "/proc/$xray/status")" = 0 ]
printf '%s\n' "$2" >>"$S/phases"
/opt/bin/busybox usleep 300000
CALLBACK
    chmod 600 "$S/preflight.sh"
    "$T/bin/legacy-guard" --discover "$S" >"$S/discovery.json"
}
refuse() {
    rc=0; "$T/bin/legacy-guard" "$S" >"$S/result.json" 2>"$S/error.txt" || rc=$?
    [ "$rc" = 75 ]
    [ -d "$L" ] && [ ! -e "$S/retired-global.lock" ]
    [ "$(ticks "$home")" = "$home_birth" ]
    [ "$(awk '/^TracerPid:/ {print $2}' "/proc/$web/status")" = 0 ]
}
printf '2147483646\n' >"$L/pid"; printf 'system\n' >"$L/scope"
printf 'subscriptions:scheduler\n' >"$L/action"; : >"$L/bundle"
printf '2026-09-16T00:00:00Z\n' >"$L/startedAt"
cp "$T/bin/legacy-process-fixture" "$A/runtime/broray-lighttpd"
cp "$T/bin/legacy-process-fixture" "$A/runtime/xray"
BRORAY_LEGACY_FIXTURE_STOPFILE="$T/web.exit" "$A/runtime/broray-lighttpd" -f "$A/config/lighttpd.conf" & web=$!
printf '%s\n' "$web" >"$T/web.pid"
BRORAY_LEGACY_FIXTURE_STOPFILE="$T/xray.exit" BRORAY_LEGACY_FIXTURE_HEARTBEAT="$T/heartbeat" "$A/runtime/xray" run -c "$A/config/config.json" & xray=$!
printf '%s\n' "$xray" >"$T/xray.pid"
cat >"$A/bin/broray-home-snapshotd" <<'DAEMON'
#!/opt/bin/ash
while [ ! -f "$T/home.exit" ]; do sleep 30; done
DAEMON
/opt/bin/ash "$A/bin/broray-home-snapshotd" & home=$!
printf '%s\n' "$home" >"$A/run/home-snapshotd.pid"
sleep 1
home_birth="$(ticks "$home")"; xray_birth="$(ticks "$xray")"
sleep_pid=''
for entry in /proc/[0-9]*; do
    parent="$(sed 's/.*) //' "$entry/stat" 2>/dev/null | awk '{print $2}')" || continue
    [ "$parent" != "$home" ] || sleep_pid="${entry##*/}"
done
[ -n "$sleep_pid" ]; sleep_birth="$(ticks "$sleep_pid")"

cp "$T/bin/legacy-process-fixture" "$T/ip"
BRORAY_LEGACY_FIXTURE_STOPFILE="$T/ip.exit" "$T/ip" route add fixture & writer=$!
sleep 1
new_session 1; refuse
grep -q LEGACY_WRITER_UNCONFIRMED "$S/error.txt"
pass unknown_native_writer_preserves_owner_and_fence
: >"$T/ip.exit"; wait "$writer"

new_session 2
awk -F '\t' -v OFS='\t' -v pid="$home" '{if ($2==pid) $3=$3+1; print}' "$S/targets.tsv" >"$S/changed"
mv "$S/changed" "$S/targets.tsv"
refuse; pass changed_start_ticks_refused_before_signal

printf '%s\n' "$xray" >"$L/pid"
new_session 3; refuse
pass live_unrelated_lock_pid_is_preserved
printf '2147483646\n' >"$L/pid"

mv "$A/run/home-snapshotd.pid" "$T/home.pid.saved"
mkfifo "$A/run/home-snapshotd.pid"
new_session 4; refuse
pass fifo_pid_projection_refused_without_blocking
[ -p "$A/run/home-snapshotd.pid" ]; rm "$A/run/home-snapshotd.pid"
mv "$T/home.pid.saved" "$A/run/home-snapshotd.pid"

new_session 5
before="$(wc -c <"$T/heartbeat")"
"$T/bin/legacy-guard" "$S" >"$S/result.json" 2>"$S/error.txt"
jq -e '.ok and .result=="legacy_fence_retired"' "$S/result.json" >/dev/null
[ ! -e "$L" ] && [ -d "$S/retired-global.lock" ]
[ ! -s "$S/retired-global.lock/bundle" ]
[ "$(cat "$S/retired-home-snapshotd.pid")" = "$home" ]
[ ! -e "$A/run/home-snapshotd.pid" ]
printf 'check\nfinalize\n' >"$T/expected-phases"
cmp -s "$T/expected-phases" "$S/phases"
[ "$(ticks "$xray")" = "$xray_birth" ]
[ "$(wc -c <"$T/heartbeat")" -gt "$before" ]
[ "$(awk '/^TracerPid:/ {print $2}' "/proc/$web/status")" = 0 ]
rc=0; wait "$home" || rc=$?; [ "$rc" = 137 ]
pass pinned_idle_owner_stopped_and_exact_fence_archived
pass both_callback_phases_finish_before_retirement
pass native_xray_fixture_keeps_running_during_web_pin
: >"$T/web.exit"; : >"$T/xray.exit"; wait "$web"; wait "$xray"
# The harmless orphaned sleep exits naturally. Never signal it by stored PID.
for round in $(seq 1 35); do
    [ -e "/proc/$sleep_pid" ] || break
    [ "$(ticks "$sleep_pid" 2>/dev/null || true)" = "$sleep_birth" ] || break
    state="$(sed 's/.*) //' "/proc/$sleep_pid/stat" | awk '{print $1}')"
    [ "$state" != Z ] && [ "$state" != X ] || break
    sleep 1
done
if [ -e "/proc/$sleep_pid" ] && [ "$(ticks "$sleep_pid" 2>/dev/null || true)" = "$sleep_birth" ]; then
    state="$(sed 's/.*) //' "/proc/$sleep_pid/stat" | awk '{print $1}')"
    [ "$state" = Z ] || [ "$state" = X ]
fi
broray_ops_capture_owner "$installed_pid" >"$T/installed-xray.after"
cmp -s "$T/installed-xray.before" "$T/installed-xray.after"
sha256sum -c "$T/installed-files.before" >/dev/null
[ ! -e /opt/var/lock/broray/global-operation.lock ] && [ ! -L /opt/var/lock/broray/global-operation.lock ]
pass installed_xray_config_channel_and_fence_unchanged
jq -Rn '[inputs | select(length>0)] as $tests | {status:"PASS",tests:$tests,physicalPrefixOnly:true,inventoryScopedToFixture:true,legacyFullApplicationRecoveryTested:false,realVpnTested:false,installedApplicationChanged:false}' <"$T/passed.txt" >"$T/RESULT.json"
