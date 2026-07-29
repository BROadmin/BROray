#!/opt/bin/ash
set -u
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
. "$BRORAY_ROOT/web-new/api/routes/dot-common.sh"
broray_api_require_method POST
broray_api_require_session
broray_dot_api_lock delete
broray_dot_api_run delete
