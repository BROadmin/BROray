#!/opt/bin/ash
# Generate and validate only a private candidate. Never restart persistent Xray.
set -eu
umask 077
[ "$#" = 2 ] || exit 64
activate_prepare_dir="$1"; activate_prepare_server="$2"
BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"
case "$activate_prepare_dir" in "$BRORAY_BASE/tmp/server-activate-"*) ;; *) exit 64 ;; esac
[ -d "$activate_prepare_dir" ] && [ ! -L "$activate_prepare_dir" ] || exit 64
[ "$(cat "$activate_prepare_dir/operation-id")" = "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || exit 73
BRORAY_SERVER_TMP_CONFIG="$activate_prepare_dir/config.json"
export BRORAY_SERVER_TMP_CONFIG
. "$BRORAY_BASE/lib/server-xray-manager.sh"
broray_generate_server_config "$activate_prepare_server" >/dev/null
broray_xray_test_file "$BRORAY_SERVER_TMP_CONFIG" ||
    broray_die "Xray отклонил конфигурацию сервера"
chmod 600 "$BRORAY_SERVER_TMP_CONFIG"
