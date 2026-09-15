#!/opt/bin/ash
set -u
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
. "$BRORAY_ROOT/web-new/api/routes/dot-common.sh"
broray_api_require_method POST
broray_api_require_session
request="$BRORAY_ROOT/tmp/dot-test-request.$$.json"
BRORAY_DOT_API_REQUEST_FILE="$request"
broray_dot_api_install_traps
broray_dot_api_read_body "$request"
broray_dot_api_lock test
broray_dot_api_run test "$request"
