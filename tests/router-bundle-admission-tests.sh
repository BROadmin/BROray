#!/opt/bin/ash
# Official read-only slot validators against a private development bundle.
set -eu
umask 077
T=/opt/tmp/broray-311-bundle-20260915
RAM=/tmp/broray-311-bundle-20260915
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ] || exit 1
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-BUNDLE-20260915 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ] || exit 1
mkdir -m 700 "$RAM"; echo BRORAY311-BUNDLE-20260915 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
mkdir "$T/slot" "$T/checks" "$T/runtime"
/opt/bin/busybox tar -xzf "$T/bin/candidate.tar.gz" -C "$T/slot"
echo 3.1.1-r01c01--builder-admission >"$T/slot/.broray-slot"
printf 'Package: broray\nVersion: 3.0.0-r14\nArchitecture: aarch64-3.10\n' >"$T/control"
# A private executable fixture satisfies the old presence check; no Xray runs.
cp "$T/bin/fixture" "$T/runtime/xray"; chmod 755 "$T/runtime/xray"
sha256sum "$T/runtime/xray" >"$T/runtime-before.sha256"
cp "$T/slot/app/bin/xray" "$T/runtime/wrapper"
OPKG_CONTROL="$T/control"
CURRENT_OPERATION_DIR="$T/checks"
LIFECYCLE_CONTRACT=compact-app-rename/1
ARCHITECTURE=aarch64-3.10
XRAY_WRAPPER="$T/runtime/wrapper"
XRAY_RUNTIME="$T/runtime/xray"
ASH_BIN=/opt/bin/ash
. "$T/bin/slot-validators.sh"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }

slot_tree_valid "$T/slot"
slot_metrics_match "$T/slot" "$(cat "$T/bin/candidate.json")"
pass exact_slot_manifest_versions_modes_and_space_metrics
shell_tree_valid "$T/slot"
pass all_shell_entries_parse_with_target_entware_ash
cp "$T/slot/app/web-new/assets/js/servers-auto-switch.js" "$T/original.js"
printf '\nCORRUPTED\n' >>"$T/slot/app/web-new/assets/js/servers-auto-switch.js"
if slot_tree_valid "$T/slot"; then exit 1; fi
cp "$T/original.js" "$T/slot/app/web-new/assets/js/servers-auto-switch.js"
slot_tree_valid "$T/slot"
pass changed_payload_is_rejected_and_exact_restore_passes
echo KEEP >"$T/slot/UNEXPECTED"
if slot_tree_valid "$T/slot"; then exit 1; fi
rm "$T/slot/UNEXPECTED"
slot_tree_valid "$T/slot"
sha256sum -c "$T/runtime-before.sha256" >/dev/null
pass unlisted_file_is_rejected_shared_runtime_fixture_unchanged
jq -Rn '[inputs|select(length>0)]|{status:"PASS",tests:.,environment:"official slot validators on physical router; isolated fixture metadata, no installed app or VPN",applicationInstalled:false}' <"$T/passed.txt" >"$T/RESULT.json"
