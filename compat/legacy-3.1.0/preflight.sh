#!/opt/bin/ash
# UNPUBLISHED compatibility policy. Run only by the verified native barrier.
# check: read-only outside the private session. finalize: persist pause only.
# PID projections and the global fence are retired by the native parent.
set -eu
umask 077
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
LC_ALL=C
export PATH LC_ALL
S="$1"
PHASE="$2"
A=/opt/broray
U=/opt/var/lib/broray-updater
STATE=/opt/var/lib/broray
FENCE=/opt/var/lock/broray/global-operation.lock
fail() { printf '%s\n' "$1" >&2; exit 75; }
absent() { [ ! -e "$1" ] && [ ! -L "$1" ]; }
regular() {
    [ -f "$1" ] && [ ! -L "$1" ] || return 1
    [ -n "$(find "$1" -maxdepth 0 -type f -links 1 -print)" ] || return 1
    [ "$(wc -c <"$1")" -le "$2" ]
}
canonical_dir() { [ -d "$1" ] && [ ! -L "$1" ] && [ "$(readlink -f "$1")" = "$1" ]; }
safe_tree() {
    canonical_dir "$1" || return 1
    unsafe="$(find "$1" -xdev ! -type d ! -type f -print -quit)" || return 1
    [ -z "$unsafe" ] || return 1
    unsafe="$(find "$1" -xdev -type f ! -links 1 -print -quit)" || return 1
    [ -z "$unsafe" ]
}
case "$S" in /opt/var/lib/broray/legacy-recovery/*) ;; *) fail SESSION_INVALID ;; esac
canonical_dir "$S" || fail SESSION_INVALID
case "$PHASE" in check|finalize) ;; *) fail PHASE_INVALID ;; esac
case "${BRORAY_LEGACY_BARRIER_PID:-}" in ''|*[!0-9]*) fail BARRIER_MISSING ;; esac
[ "$(sed 's/.*) //' /proc/$$/stat | awk '{print $2}')" = "$BRORAY_LEGACY_BARRIER_PID" ] || fail BARRIER_MISSING
[ -d "$FENCE" ] && [ ! -L "$FENCE" ] || fail FENCE_MISSING
(cd "$S"; sha256sum -c SHA256SUMS >/dev/null 2>&1) || fail BUNDLE_CHANGED

source_check() {
    safe_tree "$A/current" || fail SOURCE_UNSAFE
    regular "$A/current/.broray-slot" 256 || fail SLOT_INVALID
    [ "$(wc -l <"$A/current/.broray-slot")" = 1 ] || fail SLOT_INVALID
    case "$(cat "$A/current/.broray-slot")" in ''|.*|-*|*[!A-Za-z0-9._-]*) fail SLOT_INVALID ;; esac
    (cd "$A/current"; sha256sum -c "$S/source.sha256" >/dev/null 2>&1) || fail SOURCE_CHANGED
    (cd "$A/current"; find . -type f ! -path './.broray-slot') >"$S/source.unsorted.$PHASE" || fail SOURCE_UNREADABLE
    sed 's,^./,,' "$S/source.unsorted.$PHASE" | sort >"$S/source.actual.$PHASE"
    cmp -s "$S/source.files" "$S/source.actual.$PHASE" || fail SOURCE_CHANGED
    for part in bin lib web-new share; do
        [ -L "$A/$part" ] && [ "$(readlink "$A/$part")" = "current/app/$part" ] || fail SOURCE_LINK_CHANGED
    done
    for service in S23broray-monitor S24broray S25broray-web S27broray-auto-switch S28broray-subscriptions; do
        [ -L "/opt/etc/init.d/$service" ] && [ "$(readlink "/opt/etc/init.d/$service")" = "$A/current/init/$service" ] || fail INIT_LINK_CHANGED
    done
    canonical_dir /opt/libexec/broray-updater && canonical_dir /opt/etc/init.d || fail PLATFORM_UNSAFE
    while IFS= read -r path; do regular "/$path" 2097152 || fail PLATFORM_UNSAFE; done <"$S/platform.files"
    (cd /; sha256sum -c "$S/platform.sha256" >/dev/null 2>&1) || fail PLATFORM_CHANGED
}

domain_check() {
    # These are installation/runtime commits, not ordinary stale work locks.
    for path in "$U/request.lock" /tmp/broray-global-operation.lock /tmp/broray-update.lock \
        /tmp/broray-system-operation.lock "$A/update/xray.lock" "$A/run/xray-update/xray.lock" \
        "$A/run/server-auto-switch-cycle.lock"; do
        absent "$path" || fail PROTECTED_TRANSACTION_PRESENT
    done
    canonical_dir "$U/queue" || fail UPDATER_QUEUE_UNSAFE
    queued="$(find "$U/queue" -mindepth 1 -print -quit)" || fail UPDATER_QUEUE_UNSAFE
    [ -z "$queued" ] || fail UPDATER_QUEUE_PENDING
    if ! absent "$STATE/last-operation"; then
        regular "$STATE/last-operation" 256 || fail UPDATER_POINTER_UNSAFE
        id="$(cat "$STATE/last-operation")"
        case "$id" in ''|.*|-*|*[!A-Za-z0-9._-]*) fail UPDATER_POINTER_UNSAFE ;; esac
        status="$STATE/operations/$id/state.json"
        regular "$status" 32768 || fail UPDATER_STATE_UNSAFE
        jq -e '.schemaVersion==1 and .engine=="broray-updater/5" and .running==false and
            ((.state=="success") or (.state=="failed" and (.mutationStarted==false or .rollbackPerformed==true)))' "$status" >/dev/null || fail UPDATER_TRANSACTION_PENDING
    fi
    if ! absent "$STATE/operations"; then
        safe_tree "$STATE/operations" || fail UPDATER_STATE_UNSAFE
        for operation in "$STATE/operations"/* "$STATE/operations"/.[!.]* "$STATE/operations"/..?*; do
            absent "$operation" && continue
            canonical_dir "$operation" || fail UPDATER_STATE_UNSAFE
            regular "$operation/state.json" 32768 || fail UPDATER_STATE_UNSAFE
            jq -e '.schemaVersion==1 and .engine=="broray-updater/5" and .running==false and
                ((.state=="success") or (.state=="failed" and (.mutationStarted==false or .rollbackPerformed==true)))' "$operation/state.json" >/dev/null || fail UPDATER_TRANSACTION_PENDING
        done
    fi
    scope="$(cat "$FENCE/scope")"; action="$(cat "$FENCE/action")"; bundle="$(cat "$FENCE/bundle")"
    case "$scope:$action:$bundle" in
        system:subscriptions:scheduler:|system:auto-switch:) ;;
        routes:subscriptions:create:subscriptions|routes:subscriptions:update:subscriptions|routes:subscriptions:delete:subscriptions|routes:subscriptions:refresh:subscriptions) ;;
        routes:servers:import:servers|routes:servers:delete:servers|routes:servers:check:servers|routes:servers:quality-batch-complete:servers|routes:servers:quality-refresh-save:servers|routes:servers:auto-switch-save:servers) ;;
        routes:plan:*|routes:check:*|routes:download:*|routes:verify:*|routes:preflight:*|routes:export:*|routes:resume:*|routes:delete:*)
            case "$bundle" in ''|.*|-*|*[!A-Za-z0-9._-]*) fail ROUTE_BUNDLE_INVALID ;; esac ;;
        *) fail PROTECTED_OR_UNSUPPORTED_ACTION ;;
    esac
    # A route's existing progress is deliberately retained for its own UI.
    regular "$A/config/config.json" 4194304 || fail XRAY_CONFIG_UNSAFE
    jq -e 'type=="object" and (.inbounds|type)=="array" and (.outbounds|type)=="array"' "$A/config/config.json" >/dev/null || fail XRAY_CONFIG_INVALID
}

business_snapshot() {
    output="$1"
    : >"$output"
    for part in config servers routes update; do
        if absent "$A/$part"; then printf 'absent %s\n' "$part" >>"$output"; continue; fi
        safe_tree "$A/$part" || fail BUSINESS_TREE_UNSAFE
        (cd "$A"; find "$part" -type d) >"$S/business.directories" || fail BUSINESS_UNREADABLE
        sort "$S/business.directories" >>"$output" || fail BUSINESS_UNREADABLE
        (cd "$A"; find "$part" -type f) >"$S/business.files" || fail BUSINESS_UNREADABLE
        sort "$S/business.files" >"$S/business.sorted" || fail BUSINESS_UNREADABLE
        while IFS= read -r file; do
            (cd "$A"; sha256sum "$file") >>"$output" || fail BUSINESS_UNREADABLE
        done <"$S/business.sorted"
    done
    regular "$A/runtime/xray" 134217728 || fail XRAY_RUNTIME_UNSAFE
    sha256sum "$A/runtime/xray" >>"$output"
}

source_check
domain_check
if [ "$PHASE" = check ]; then
    business_snapshot "$S/business.before"
    printf 'CHECK_PASS\n' >"$S/check.complete"
    exit 0
fi
regular "$S/check.complete" 64 && [ "$(cat "$S/check.complete")" = CHECK_PASS ] || fail CHECK_MISSING
business_snapshot "$S/business.after"
cmp -s "$S/business.before" "$S/business.after" || fail BUSINESS_STATE_CHANGED
canonical_dir "$STATE" || fail STATE_UNSAFE
pause="$STATE/background-automation.json"
if ! absent "$pause"; then regular "$pause" 4096 || fail PAUSE_UNSAFE; fi
temporary="$STATE/.legacy-pause-${S##*/}"
absent "$temporary" || fail PAUSE_TEMP_EXISTS
printf '{"schemaVersion":1,"paused":true,"updatedAt":"%s"}\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >"$temporary"
"$S/state-writer" --replace-file "$temporary" "$pause" || fail PAUSE_PERSIST_FAILED
printf 'FINALIZE_PASS\n' >"$S/finalize.complete"
