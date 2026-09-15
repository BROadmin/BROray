#!/opt/bin/ash
# Short native-flock transaction. The lifetime lease is never unlinked.
umask 077
. "${BRORAY_ROOT:-/opt/broray}/lib/service-lifecycle.sh" || exit 74
broray_service_setup "${1:-}" || exit $?
shift
[ "${BRORAY_OPS_GUARD_HELD:-}" = 1 ] || exit 73
broray_service_fd "$SVC_DIR/control.guard" >/dev/null || exit 73
action="${1:-}"; shift
rc=0; broray_service_record || rc=$?
[ "$rc" != 2 ] && broray_service_legacy_matches || exit 75

case "$action" in
  adopt)
    [ "$#" = 1 ] || exit 64
    # adopt runs below a command-substitution shell; verify the supplied daemon
    # identity and its inherited lifetime descriptor, never a pathname-only PID.
    broray_service_fd "$SVC_DIR/lifetime.guard" >/dev/null || exit 73
    if [ "$rc" = 0 ]; then
        broray_ops_classify_owner "$SVC_OWNER"
        [ "$OPS_OWNER_STATUS" = STALE ] || exit 75
    fi
    new_owner="$(broray_ops_capture_owner "$1")" || exit 75
    expected_command="$(printf '%s\000%s\000' "$SVC_ASH" "$SVC_DAEMON" | sha256sum | awk '{print $1}')" || exit 74
    jq -en --argjson owner "$new_owner" --arg digest "$expected_command" \
      --arg exe "$(readlink -f "$SVC_ASH")" '$owner.commandDigest==$digest and $owner.executable==$exe' >/dev/null || exit 73
    [ -n "$SVC_OWNER" ] || { [ ! -e "$SVC_PIDFILE" ] && [ ! -L "$SVC_PIDFILE" ]; } || exit 75
    # Only an exactly matching old projection is eligible for retirement.
    rm -f "$SVC_PIDFILE" || exit 74
    [ -z "$SVC_STARTTIME" ] || rm -f "$SVC_STARTTIME" || exit 74
    if [ -e "$SVC_DIR/stop.json" ] || [ -L "$SVC_DIR/stop.json" ]; then
        broray_service_file_safe "$SVC_DIR/stop.json" || exit 75
        rm -f "$SVC_DIR/stop.json" || exit 74
    fi
    generation="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || exit 74
    record="$(jq -nc --arg name "$SVC_NAME" --arg gen "$generation" --argjson owner "$new_owner" \
      '{schemaVersion:1,service:$name,generation:$gen,owner:$owner,state:"starting"}')" || exit 74
    broray_service_write "$SVC_DIR/identity.json" "$record" || exit 74
    mkdir -p "$SVC_APP/run" || exit 74
    broray_service_write "$SVC_PIDFILE" "$1" || exit 74
    [ -z "$SVC_STARTTIME" ] || broray_service_write "$SVC_STARTTIME" "$(printf '%s\n' "$new_owner" | jq -r '.startTicks')" || exit 74
    record="$(printf '%s\n' "$record" | jq -c '.state="running"')"
    broray_service_write "$SVC_DIR/identity.json" "$record" || exit 74
    printf '%s\n' "$record" ;;
  stop)
    [ "$#" = 0 ] || exit 64
    [ "$rc" = 0 ] || exit 0
    broray_ops_classify_owner "$SVC_OWNER"
    [ "$OPS_OWNER_STATUS" != AMBIGUOUS ] || exit 75
    [ "$OPS_OWNER_STATUS" = ACTIVE ] || exit 0
    stop="$(jq -nc --arg generation "$SVC_GENERATION" '{schemaVersion:1,generation:$generation}')" || exit 74
    broray_service_write "$SVC_DIR/stop.json" "$stop" ;;
  retire-dead)
    [ "$#" = 0 ] || exit 64
    broray_service_fd "$SVC_DIR/lifetime.guard" >/dev/null || exit 73
    [ "$rc" = 0 ] || exit 0
    broray_ops_classify_owner "$SVC_OWNER"
    [ "$OPS_OWNER_STATUS" = STALE ] || exit 75
    # Both locks are held. No older daemon or native writer can still run;
    # projections matched the full recorded identity before this first unlink.
    rm -f "$SVC_PIDFILE" || exit 74
    [ -z "$SVC_STARTTIME" ] || rm -f "$SVC_STARTTIME" || exit 74
    if printf '%s\n' "$SVC_RECORD" | jq -e '.state=="stopped"' >/dev/null; then exit 0; fi
    record="$(printf '%s\n' "$SVC_RECORD" | jq -c '.state="stopped"')"
    broray_service_write "$SVC_DIR/identity.json" "$record" ;;
  retire)
    [ "$#" = 2 ] && [ "$rc" = 0 ] && [ "$SVC_GENERATION" = "$2" ] || exit 73
    broray_service_fd "$SVC_DIR/lifetime.guard" >/dev/null || exit 73
    expected="$(broray_ops_capture_owner "$1")" || exit 75
    jq -en --argjson a "$expected" --argjson b "$SVC_OWNER" '$a==$b' >/dev/null || exit 73
    rm -f "$SVC_PIDFILE" || exit 74
    [ -z "$SVC_STARTTIME" ] || rm -f "$SVC_STARTTIME" || exit 74
    record="$(printf '%s\n' "$SVC_RECORD" | jq -c '.state="stopped"')"
    broray_service_write "$SVC_DIR/identity.json" "$record" ;;
  *) exit 64 ;;
esac
