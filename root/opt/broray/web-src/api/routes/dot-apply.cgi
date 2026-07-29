#!/opt/bin/ash
set -u
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
. "$BRORAY_ROOT/web-new/api/routes/dot-common.sh"
broray_api_require_method POST
broray_api_require_session
request="$BRORAY_ROOT/tmp/dot-apply-request.$$.json"
trap 'rm -f "$request"; command -v broray_routes_api_lock_release >/dev/null 2>&1 && broray_routes_api_lock_release' EXIT HUP INT TERM
broray_dot_api_read_body "$request"
broray_dot_api_lock apply
broray_dot_api_run apply "$request"
