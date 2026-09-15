#!/bin/sh
# READ-ONLY. Run on the test router only after it is explicitly released.
# No SSH transport, writes, signals, service commands, network calls or configs.
set -u
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin
export PATH
command -v jq >/dev/null 2>&1 || { printf '%s\n' '{"ok":false,"errorCode":"JQ_UNAVAILABLE","readOnly":true}'; exit 1; }
prefix="${BRORAY_PREFLIGHT_FIXTURE_ROOT:-}"
test_mode=false; [ -z "$prefix" ] || test_mode=true
proc="$prefix/proc"
root="$prefix/opt/broray"
flags='{}'
for name in ash jq sha256sum readlink od ln stat; do
    available=false; command -v "$name" >/dev/null 2>&1 && available=true
    flags="$(printf '%s\n' "$flags" | jq -c --arg name "$name" --argjson available "$available" '.[$name]=$available')"
done
boot=false; [ -r "$proc/sys/kernel/random/boot_id" ] && boot=true
identity=false
if [ -r "$proc/$$/stat" ] && [ -r "$proc/$$/cmdline" ] && [ -L "$proc/$$/exe" ]; then identity=true; fi
arch="$(uname -m 2>/dev/null || true)"
case "$arch" in aarch64|armv7l|armv8l|mips|mipsel|x86_64) ;; *) arch=unknown ;; esac
kernel="$(uname -r 2>/dev/null || true)"
case "$kernel" in ''|*[!A-Za-z0-9._+-]*) kernel=unknown ;; esac
uptime="$(awk 'NR==1 && $1~/^[0-9]+([.][0-9]+)?$/{print $1;exit}' "$proc/uptime" 2>/dev/null)"
case "$uptime" in ''|*[!0-9.]*) uptime=0 ;; esac
build='{}'
if [ -f "$root/web-new/build.json" ] && [ ! -L "$root/web-new/build.json" ] &&
   [ "$(wc -c <"$root/web-new/build.json")" -le 8192 ]; then
    build="$(jq -c 'def version: if type=="string" and test("^(WebUI-)?[0-9]+[.][0-9]+[.][0-9]+(-r[0-9]+(c[0-9]+)?)?$") then . else null end;
      {appVersion:(.appVersion|version),candidateId:(.candidateId|version),releaseId:(.releaseId|version),webuiBuild:(.buildId|version)}' "$root/web-new/build.json" 2>/dev/null)" || build='{}'
fi
lock="$prefix/opt/var/lock/broray/global-operation.lock"
lock_shape=absent
if [ -L "$lock" ]; then lock_shape=symlink
elif [ -d "$lock" ]; then lock_shape=directory
elif [ -e "$lock" ]; then lock_shape=other; fi
states='{}'
for name in subscription-scheduler server-auto-switch connection-monitor; do
    file="$root/run/$name.pid"; state=unavailable
    if [ -f "$file" ] && [ ! -L "$file" ] && [ "$(wc -c <"$file")" -le 64 ]; then
        pid="$(sed -n '1p' "$file")"
        case "$pid" in ''|*[!0-9]*|0|1) state=ambiguous ;;
          *) state=absent; [ ! -d "$proc/$pid" ] || state=present_unconfirmed ;; esac
    fi
    states="$(printf '%s\n' "$states" | jq -c --arg name "$name" --arg state "$state" '.[$name]=$state')"
done
jq -nc --arg arch "$arch" --arg kernel "$kernel" --argjson uptime "$uptime" --argjson commands "$flags" \
  --argjson boot "$boot" --argjson identity "$identity" --argjson build "$build" --arg shape "$lock_shape" \
  --argjson services "$states" --argjson test "$test_mode" \
  '{schemaVersion:1,readOnly:true,testFixture:$test,architecture:$arch,kernel:$kernel,uptimeSeconds:$uptime,
    commands:$commands,bootIdReadable:$boot,selfIdentityReadable:$identity,build:$build,globalFenceShape:$shape,
    servicePidFiles:$services,notChecked:["native ARM64 execution","fcntl and directory fsync on target filesystem","process cancellation","VPN continuity","blocked updater delivery"],
    readyToDeploy:false}'
