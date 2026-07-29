#!/opt/bin/ash
set -u
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
. "$BRORAY_ROOT/web-new/api/routes/dot-common.sh"
broray_api_require_method GET
broray_api_require_session
[ -x "$BRORAY_DOT_CLI" ] || broray_api_error "500 Internal Server Error" DOT_CLI_UNAVAILABLE "Команда DNS-over-TLS недоступна."
broray_dot_api_run status
