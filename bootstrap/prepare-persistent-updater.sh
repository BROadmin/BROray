#!/opt/bin/ash
# Installer hook only. The two expected digests MUST come from authenticated
# release metadata, not from recomputing hashes of an untrusted download.
# Run after archive authentication/safe extraction, before request update/reinstall.
set -u
[ "$#" -eq 3 ] || { echo 'usage: prepare-persistent-updater.sh SLOT RUNTIME_MANIFEST_SHA256 PLATFORM_MANIFEST_SHA256' >&2; exit 2; }
slot="$1"
expected_runtime="$2"
expected_platform="$3"
for digest in "$expected_runtime" "$expected_platform"; do
    [ "${#digest}" -eq 64 ] || exit 2
    case "$digest" in *[!0-9a-f]*) exit 2 ;; esac
done
[ -d "$slot" ] && [ ! -L "$slot" ] || exit 1
[ -f "$slot/SHA256SUMS" ] && [ ! -L "$slot/SHA256SUMS" ] || exit 1
actual="$(sha256sum "$slot/SHA256SUMS")" || exit 1
[ "${actual%% *}" = "$expected_runtime" ] || exit 1
# Reject manifest paths that could escape the authenticated slot.
awk 'NF!=2 || length($1)!=64 || $1~/[^0-9a-f]/ || $2~/^\// || $2~/(^|\/)\.\.?(\/|$)/ {bad=1} {if(seen[$2]++)bad=1} END {exit bad || NR==0}' "$slot/SHA256SUMS" || exit 1
[ -z "$(find "$slot" -mindepth 1 ! -type d ! -type f -print -quit)" ] || exit 1
(cd "$slot" && sha256sum -c SHA256SUMS >/dev/null 2>&1) || exit 1
handoff="$slot/app/lib/universal-platform-handoff.sh"
lock_library="$slot/app/lib/routes-api-operation.sh"
for relative in app/lib/universal-platform-handoff.sh app/lib/routes-api-operation.sh app/share/updater-platform/SHA256SUMS; do
    [ "$(awk -v path="$relative" '$2==path {n++} END {print n+0}' "$slot/SHA256SUMS")" = 1 ] || exit 1
done
[ -x "$handoff" ] && [ -f "$lock_library" ] || exit 1
BRORAY_HANDOFF_SELF="$handoff" \
BRORAY_HANDOFF_PREFLIGHT_LOCK_LIBRARY="$lock_library" \
BRORAY_HANDOFF_PAYLOAD_ROOT="$slot/app/share/updater-platform" \
    "${BRORAY_HANDOFF_ASH:-/opt/bin/ash}" "$handoff" preflight "$expected_platform"
