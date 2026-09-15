#!/opt/bin/ash
umask 077
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
PATH="$BRORAY_ROOT/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH BRORAY_ROOT
[ "${BRORAY_OPS_SUPERVISED:-}" = ptrace/1 ] && [ "$#" = 1 ] || exit 73
import_dir="$1"
case "$import_dir" in "$BRORAY_ROOT/tmp/server-import-$BRORAY_BACKGROUND_OPERATION_ID-"*) ;; *) exit 73 ;; esac
[ -d "$import_dir" ] && [ ! -L "$import_dir" ] &&
  [ "$(cat "$import_dir/operation-id")" = "$BRORAY_BACKGROUND_OPERATION_ID" ] || exit 73
. "$BRORAY_ROOT/lib/server-import.sh"
BRORAY_SERVERS="$import_dir/servers"
BRORAY_TMP="$import_dir"
broray_server_import_dispatch "$(cat "$import_dir/uri")" manual "" 0 >"$import_dir/result.txt"
