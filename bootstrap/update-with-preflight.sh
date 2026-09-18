#!/opt/bin/ash
# Replace the installer's old `broray-updaterctl request ACTION` entry with
# this authenticated bundle entry. No fallback to the previous unsafe order.
set -u
[ "$#" -eq 4 ] || { echo 'usage: update-with-preflight.sh {update|reinstall} SLOT RUNTIME_MANIFEST_SHA256 PLATFORM_MANIFEST_SHA256' >&2; exit 2; }
action="$1"
shift
case "$action" in update|reinstall) ;; *) exit 2 ;; esac
base="${0%/*}"
ash="${BRORAY_HANDOFF_ASH:-/opt/bin/ash}"
"$ash" "$base/prepare-persistent-updater.sh" "$@" || exit 1
ctl=/opt/bin/broray-updaterctl
if [ "${BRORAY_HANDOFF_TEST_MODE:-0}" = 1 ] && [ -n "${BRORAY_HANDOFF_ROOT_PREFIX:-}" ]; then
    ctl="${BRORAY_BOOTSTRAP_TEST_UPDATERCTL:?missing isolated request fixture}"
fi
[ -f "$ctl" ] && [ ! -L "$ctl" ] && [ -x "$ctl" ] || exit 1
exec "$ctl" request "$action"
