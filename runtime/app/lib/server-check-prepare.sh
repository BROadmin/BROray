#!/opt/bin/ash
# The temporary Xray stays inside the supervised measurement tree.
umask 077
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
PATH="$BRORAY_ROOT/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH BRORAY_ROOT
[ "${BRORAY_OPS_SUPERVISED:-}" = ptrace/1 ] && [ "$#" = 3 ] || exit 73
prepare_dir="$1"; prepare_server="$2"; prepare_source="$3"
case "$prepare_dir" in "$BRORAY_ROOT/tmp/server-check-$BRORAY_BACKGROUND_OPERATION_ID-"*) ;; *) exit 73 ;; esac
[ -d "$prepare_dir" ] && [ ! -L "$prepare_dir" ] &&
  [ "$(cat "$prepare_dir/operation-id")" = "$BRORAY_BACKGROUND_OPERATION_ID" ] || exit 73
BRORAY_SERVER_TMP_CONFIG="$prepare_dir/generated.json"
BRORAY_SERVER_CHECK_TMP="$prepare_dir"
BRORAY_SERVER_PROBE_WORK="$prepare_dir/probe"
export BRORAY_SERVER_TMP_CONFIG BRORAY_SERVER_CHECK_TMP BRORAY_SERVER_PROBE_WORK
. "$BRORAY_ROOT/lib/server-service.sh"
BRORAY_QUALITY_DIR="$prepare_dir/quality"
broray_server_measure "$prepare_server" "$prepare_source" >"$prepare_dir/result.json"
