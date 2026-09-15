#!/opt/bin/ash
# Native supervisor startup only; this callback is outside the traced tree.
set -eu
APP="${BRORAY_ROOT:-/opt/broray}"
. "$APP/lib/operation-client.sh"
case "${1:-}" in
    register)
        [ "$#" = 3 ] || exit 64
        broray_ops_call supervisor-register "${BRORAY_BACKGROUND_OPERATION_ID:-}" "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" "$2" "$3"
        ;;
    *) exit 64 ;;
esac
