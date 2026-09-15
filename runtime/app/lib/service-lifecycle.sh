#!/opt/bin/ash
# Identity-bound cooperative daemon control. Never signals a stored PID.
broray_service_setup()
{
    SVC_NAME="$1"
    SVC_APP="${BRORAY_ROOT:-/opt/broray}"
    SVC_STATE="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
    SVC_DIR="$SVC_STATE/services/$SVC_NAME"
    SVC_GUARD="${BRORAY_OPS_GUARD:-$SVC_APP/bin/broray-ops-guard}"
    SVC_ASH="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    SVC_STARTTIME=""; SVC_OLD_LOCK=""
    case "$SVC_NAME" in
      auto-switch)
        SVC_DAEMON="$SVC_APP/bin/broray-server-auto-switch"
        SVC_PIDFILE="$SVC_APP/run/server-auto-switch.pid"
        SVC_LOG="$SVC_APP/logs/server-auto-switch.log" ;;
      connection-monitor)
        SVC_DAEMON="$SVC_APP/bin/broray-connection-monitor"
        SVC_PIDFILE="$SVC_APP/run/connection-monitor.pid"
        SVC_LOG="$SVC_APP/logs/connection-monitor.log" ;;
      home-snapshot)
        SVC_DAEMON="$SVC_APP/bin/broray-home-snapshotd"
        SVC_PIDFILE="$SVC_APP/run/home-snapshotd.pid"
        SVC_LOG="$SVC_APP/logs/home-snapshotd-service.log" ;;
      interface-reconcile)
        SVC_DAEMON="$SVC_APP/bin/broray-interface-reconcile"
        SVC_PIDFILE="$SVC_APP/run/interface-reconcile.pid"
        SVC_LOG="$SVC_APP/logs/interface-reconcile.log" ;;
      subscriptions)
        SVC_DAEMON="$SVC_APP/bin/broray-subscription-scheduler"
        SVC_PIDFILE="$SVC_APP/run/subscription-scheduler.pid"
        SVC_STARTTIME="$SVC_APP/run/subscription-scheduler.starttime"
        SVC_OLD_LOCK="$SVC_APP/run/subscription-scheduler.start.lock"
        SVC_LOG="$SVC_APP/logs/subscription-scheduler.log" ;;
      *) return 64 ;;
    esac
    OPS_PROC=/proc; OPS_APP="$SVC_APP"
    . "$SVC_APP/lib/operation-owner.sh" || return 74
    # Service control always uses the real kernel, including in component tests.
    unset BRORAY_OPS_TEST_IDENTITIES
}

broray_service_file_safe()
{
    local file size
    file="$1"
    [ -f "$file" ] && [ ! -L "$file" ] || return 1
    [ -n "$(find "$file" -maxdepth 0 -type f -links 1 -print 2>/dev/null)" ] || return 1
    size="$(wc -c <"$file")" || return 1
    [ "$size" -le "${2:-4096}" ]
}

broray_service_dirs()
{
    local folder
    umask 077
    for folder in "$SVC_STATE" "$SVC_STATE/services" "$SVC_DIR"; do
        [ ! -L "$folder" ] || return 74
        mkdir -p "$folder" || return 74
        [ -d "$folder" ] || return 74
        chmod 700 "$folder" || return 74
    done
}

broray_service_record()
{
    local folder
    SVC_RECORD=""; SVC_GENERATION=""; SVC_OWNER=""
    for folder in "$SVC_STATE" "$SVC_STATE/services" "$SVC_DIR"; do
        [ ! -L "$folder" ] || return 2
        [ ! -e "$folder" ] || [ -d "$folder" ] || return 2
    done
    [ -e "$SVC_DIR/identity.json" ] || [ -L "$SVC_DIR/identity.json" ] || return 1
    broray_service_file_safe "$SVC_DIR/identity.json" || return 2
    SVC_RECORD="$(jq -ce --arg name "$SVC_NAME" '
      select(.schemaVersion==1 and .service==$name and
        (.generation|type)=="string" and (.generation|length)==32 and
        (.generation|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))) and
        (.state=="starting" or .state=="running" or .state=="stopped"))' "$SVC_DIR/identity.json")" || return 2
    SVC_OWNER="$(printf '%s\n' "$SVC_RECORD" | jq -c '.owner')"
    printf '%s\n' "$SVC_OWNER" | broray_ops_owner_valid || return 2
    SVC_GENERATION="$(printf '%s\n' "$SVC_RECORD" | jq -r '.generation')"
}

broray_service_legacy_matches()
{
    local file expected
    [ -z "$SVC_OLD_LOCK" ] || { [ ! -e "$SVC_OLD_LOCK" ] && [ ! -L "$SVC_OLD_LOCK" ]; } || return 1
    for file in "$SVC_PIDFILE" "$SVC_STARTTIME"; do
        [ -n "$file" ] || continue
        [ -e "$file" ] || [ -L "$file" ] || continue
        [ -n "$SVC_OWNER" ] && broray_service_file_safe "$file" 64 || return 1
        if [ "$file" = "$SVC_PIDFILE" ]; then expected="$(printf '%s\n' "$SVC_OWNER" | jq -r '.pid')"
        else expected="$(printf '%s\n' "$SVC_OWNER" | jq -r '.startTicks')"; fi
        [ "$(cat "$file")" = "$expected" ] || return 1
    done
}

broray_service_fd()
{
    local entry
    for entry in /proc/$$/fd/*; do
        if [ "$(readlink "$entry" 2>/dev/null)" = "$1" ]; then
            case "${entry##*/}" in [3-9]) printf '%s\n' "${entry##*/}"; return 0 ;; esac
        fi
    done
    return 74
}

broray_service_write()
{
    local target temp
    target="$1"; temp="$target.pending"
    # The control flock serializes this fixed, bounded scratch name.
    if [ -e "$temp" ] || [ -L "$temp" ]; then
        broray_service_file_safe "$temp" || return 74
        rm -f "$temp" || return 74
    fi
    printf '%s\n' "$2" >"$temp" || return 74
    chmod 600 "$temp" || return 74
    "$SVC_GUARD" --replace-file "$temp" "$target"
}

broray_service_control()
{
    broray_service_dirs || return $?
    "$SVC_GUARD" "$SVC_DIR/control.guard" "$SVC_ASH" "$SVC_APP/lib/service-control.sh" "$SVC_NAME" "$@"
}

broray_service_status_json()
{
    local rc state running ready complete reason pid
    rc=0; broray_service_record || rc=$?
    complete=true; ready=false; running=false; state=stopped; reason=""; pid=null
    if [ "$rc" = 2 ] || ! broray_service_legacy_matches; then
        complete=false; running=null; state=ambiguous; reason=SERVICE_IDENTITY_UNCONFIRMED
    elif [ "$rc" = 0 ]; then
        broray_ops_classify_owner "$SVC_OWNER"
        case "$OPS_OWNER_STATUS" in
          ACTIVE)
            running=true; pid="$(printf '%s\n' "$SVC_OWNER" | jq -r '.pid')"
            state="$(printf '%s\n' "$SVC_RECORD" | jq -r '.state')"
            [ "$state" != running ] || ready=true
            if [ -f "$SVC_DIR/stop.json" ] && [ ! -L "$SVC_DIR/stop.json" ] &&
              jq -e --arg gen "$SVC_GENERATION" '.generation==$gen' "$SVC_DIR/stop.json" >/dev/null 2>&1; then state=stopping; ready=false; fi ;;
          STALE) state=stopped ;;
          *) complete=false; running=null; state=ambiguous; reason=SERVICE_IDENTITY_UNCONFIRMED ;;
        esac
    fi
    if [ -e "$SVC_DIR/stop.json" ] || [ -L "$SVC_DIR/stop.json" ]; then
        if ! broray_service_stop_valid; then
            complete=false; ready=false; state=ambiguous; reason=SERVICE_STOP_UNCONFIRMED
        fi
    fi
    jq -nc --arg service "$SVC_NAME" --arg state "$state" --arg reason "$reason" --argjson running "$running" \
      --argjson complete "$complete" --argjson ready "$ready" --argjson pid "$pid" \
      '{service:$service,state:$state,running:$running,ready:$ready,complete:$complete,pid:$pid,errorCode:(if $reason=="" then null else $reason end)}'
}

broray_service_daemon_enter()
{
    local lease_fd response
    broray_service_setup "$1" || return $?
    broray_service_dirs || return $?
    if [ "${BRORAY_SERVICE_BOOTSTRAP:-}" != "$SVC_NAME" ]; then
        export BRORAY_SERVICE_BOOTSTRAP="$SVC_NAME"
        exec "$SVC_GUARD" "$SVC_DIR/lifetime.guard" "$SVC_ASH" "$SVC_DAEMON"
        return 74
    fi
    lease_fd="$(broray_service_fd "$SVC_DIR/lifetime.guard")" || return 74
    BRORAY_SERVICE_LEASE_FD="$lease_fd"
    BRORAY_SERVICE_DAEMON_PID="$$"
    export BRORAY_SERVICE_LEASE_FD
    response="$(broray_service_control adopt "$$")" || return $?
    BRORAY_SERVICE_GENERATION="$(printf '%s\n' "$response" | jq -er '.generation')" || return 74
    export BRORAY_SERVICE_GENERATION
}

broray_service_daemon_exit()
{
    [ -n "${BRORAY_SERVICE_GENERATION:-}" ] || return 0
    broray_service_control retire "$$" "$BRORAY_SERVICE_GENERATION" >/dev/null
}

broray_service_stop_valid()
{
    broray_service_file_safe "$SVC_DIR/stop.json" || return 1
    jq -e '.schemaVersion==1 and (.generation|type)=="string" and (.generation|length)==32 and
      (.generation|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102)))' "$SVC_DIR/stop.json" >/dev/null 2>&1
}

broray_service_stop_requested()
{
    [ -e "$SVC_DIR/stop.json" ] || [ -L "$SVC_DIR/stop.json" ] || return 1
    broray_service_stop_valid || return 0
    jq -e --arg gen "$BRORAY_SERVICE_GENERATION" '.generation==$gen' "$SVC_DIR/stop.json" >/dev/null 2>&1
}

broray_service_sleep()
{
    local remaining
    remaining="$1"
    case "$remaining" in ''|*[!0-9]*) return 64 ;; esac
    while [ "$remaining" -gt 0 ]; do
        broray_service_stop_requested && return 1
        sleep 1
        remaining=$((remaining-1))
    done
}

broray_service_run_job()
(
    local child_stat child_fields child_state child_parent child_rest
    # The subshell execs the actual job. A persistent Xray descendant must not
    # inherit the scheduler singleton; its operation fence is independent.
    case "${BRORAY_SERVICE_LEASE_FD:-}" in
      [3-9])
        # An extra asynchronous shell wrapper would retain its own lease.
        # Admit only the direct foreground job child of this daemon.
        IFS= read -r child_stat </proc/self/stat || exit 74
        child_fields="${child_stat##*) }"
        IFS=' ' read -r child_state child_parent child_rest <<EOF_SERVICE_PARENT
$child_fields
EOF_SERVICE_PARENT
        [ "$child_parent" = "${BRORAY_SERVICE_DAEMON_PID:-}" ] || exit 73
        eval "exec ${BRORAY_SERVICE_LEASE_FD}>&-" ;;
      '') ;;
      *) exit 74 ;;
    esac
    unset BRORAY_SERVICE_LEASE_FD BRORAY_SERVICE_BOOTSTRAP BRORAY_SERVICE_GENERATION
    exec "${BRORAY_OPS_ASH:-/opt/bin/ash}" "$@"
)
