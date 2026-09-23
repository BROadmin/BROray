#!/opt/bin/ash
# Replace the installer's old `broray-updaterctl request ACTION` entry with
# this authenticated bundle entry. No fallback to the previous unsafe order.
set -u
[ "$#" -eq 5 ] || { echo 'usage: update-with-preflight.sh {update|reinstall} SLOT RUNTIME_MANIFEST_SHA256 PLATFORM_MANIFEST_SHA256 ARCHIVE_SHA256' >&2; exit 2; }
action="$1"
shift
case "$action" in update|reinstall) ;; *) exit 2 ;; esac
base="${0%/*}"
ash="${BRORAY_HANDOFF_ASH:-/opt/bin/ash}"
slot="$1"; runtime_sha="$2"; platform_sha="$3"; archive_sha="$4"
# All three digests come from the caller's authenticated archive metadata.
# Never rediscover the prepared target from a mutable channel after preflight.
for digest in "$runtime_sha" "$platform_sha" "$archive_sha"; do
    [ "${#digest}" -eq 64 ] || exit 2
    case "$digest" in *[!0-9a-f]*) exit 2 ;; esac
done
prepared_candidate()
{
    [ -d "$slot" ] && [ ! -L "$slot" ] &&
      [ -f "$slot/SHA256SUMS" ] && [ ! -L "$slot/SHA256SUMS" ] &&
      [ -f "$slot/release.json" ] && [ ! -L "$slot/release.json" ] || return 1
    actual="$(sha256sum "$slot/SHA256SUMS")" || return 1
    [ "${actual%% *}" = "$runtime_sha" ] || return 1
    release_sha="$(awk '$2=="release.json" {n++; sha=$1} END {if(n==1)print sha; else exit 1}' "$slot/SHA256SUMS")" || return 1
    actual="$(sha256sum "$slot/release.json")" || return 1
    [ "${actual%% *}" = "$release_sha" ] || return 1
    jq -er '.candidateId|select(type=="string" and length>0 and length<=96 and ((explode[0]|(.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122)) and all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==95 or .==45 or .==46)))' "$slot/release.json"
}
candidate="$(prepared_candidate)" || exit 1
"$ash" "$base/prepare-persistent-updater.sh" "$slot" "$runtime_sha" "$platform_sha" || exit 1
[ "$(prepared_candidate)" = "$candidate" ] || exit 1
ctl=/opt/bin/broray-updaterctl
if [ "${BRORAY_HANDOFF_TEST_MODE:-0}" = 1 ] && [ -n "${BRORAY_HANDOFF_ROOT_PREFIX:-}" ]; then
    ctl="${BRORAY_BOOTSTRAP_TEST_UPDATERCTL:?missing isolated request fixture}"
fi
[ -f "$ctl" ] && [ ! -L "$ctl" ] && [ -x "$ctl" ] || exit 1
exec "$ctl" request "$action" --prepared-target "$candidate" "$archive_sha"
