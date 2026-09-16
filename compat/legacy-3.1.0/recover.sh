#!/opt/bin/ash
# Invoke only after verifying the signed complete bundle, outside BROray CGI.
# No download, package install, reboot, network change or service restart here.
set -eu
umask 077
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
LC_ALL=C
export PATH LC_ALL
[ "$(id -u)" = 0 ] || exit 73
[ "$(uname -m)" = aarch64 ] || exit 73
package="$(readlink -f "${0%/*}")"
[ -d "$package" ] && [ ! -L "${0%/*}" ] || exit 73
(cd "$package"; sha256sum -c SHA256SUMS >/dev/null 2>&1) || exit 73
parent=/opt/var/lib/broray/legacy-recovery
if [ ! -e "$parent" ] && [ ! -L "$parent" ]; then mkdir -m 700 "$parent"; fi
[ -d "$parent" ] && [ ! -L "$parent" ] && [ "$(readlink -f "$parent")" = "$parent" ] || exit 73
for attempt in $(seq 1 15); do
    nonce="$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
    case "$nonce" in ''|*[!0-9a-f]*) exit 74 ;; esac
    [ "${#nonce}" = 32 ] || exit 74
    session="$parent/$nonce"
    mkdir -m 700 "$session"
    for file in legacy-recovery-guard state-writer preflight.sh source.sha256 source.files platform.sha256 platform.files recover.sh SHA256SUMS; do
        [ -f "$package/$file" ] && [ ! -L "$package/$file" ] || exit 73
        cp "$package/$file" "$session/$file"
    done
    chmod 700 "$session/legacy-recovery-guard" "$session/state-writer"
    (cd "$session"; sha256sum -c SHA256SUMS >/dev/null 2>&1) || exit 73
    rc=0
    "$session/legacy-recovery-guard" --discover "$session" >"$session/discovery.json" 2>"$session/discovery.stderr" || rc=$?
    if [ "$rc" = 0 ]; then
        "$session/legacy-recovery-guard" "$session" >"$session/result.json" 2>"$session/refusal.stderr" || rc=$?
        if [ "$rc" = 0 ]; then
            cat "$session/result.json"
            exit 0
        fi
    fi
    # Before the callback, refusal has stopped no daemon. A fresh immutable
    # snapshot may retry a busy idle point. Never retry after bookkeeping starts.
    if [ "$rc" != 75 ] || [ -e "$session/check.stdout" ] || [ -e "$session/quiescent.json" ]; then
        printf '{"ok":false,"errorCode":"LEGACY_RECOVERY_REFUSED","detailsPath":"%s","exitCode":%s}\n' "$session" "$rc"
        exit "$rc"
    fi
    [ "$attempt" = 15 ] || sleep 2
done
printf '{"ok":false,"errorCode":"LEGACY_EXECUTORS_NOT_QUIESCENT","detailsPath":"%s"}\n' "$session"
exit 75
