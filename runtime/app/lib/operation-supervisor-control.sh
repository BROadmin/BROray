#!/opt/bin/ash
# Native supervisor startup only; this callback is outside the traced tree.
set -eu
APP="${BRORAY_ROOT:-/opt/broray}"
. "${BRORAY_OPS_CODE_ROOT:-$APP}/lib/operation-client.sh"
case "${1:-}" in
    register)
        [ "$#" = 3 ] || exit 64
        broray_ops_call supervisor-register "${BRORAY_BACKGROUND_OPERATION_ID:-}" "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" "$2" "$3"
        ;;
    register-service-stop)
        [ "$#" = 3 ] || exit 64
        broray_ops_call updater-stop-supervisor-register "${BRORAY_BACKGROUND_OPERATION_ID:-}" "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" "$2" "$3"
        ;;
    authorize-service-stop)
        [ "$#" = 5 ] || exit 64
        broray_ops_call platform-preflight-stop-authorize "${BRORAY_BACKGROUND_OPERATION_ID:-}" "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" "$2" "$3" "$4" "$5"
        ;;
    register-platform)
        [ "$#" = 3 ] || exit 64
        broray_ops_call platform-supervisor-register "${BRORAY_BACKGROUND_OPERATION_ID:-}" "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" "$2" "$3"
        ;;
    register-interface)
        [ "$#" = 3 ] || exit 64
        broray_ops_call interface-supervisor-register "${BRORAY_BACKGROUND_OPERATION_ID:-}" "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" "$2" "$3"
        ;;
    register-route)
        [ "$#" = 3 ] || exit 64
        broray_ops_call route-supervisor-register "${BRORAY_BACKGROUND_OPERATION_ID:-}" "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" "$2" "$3"
        ;;
    *) exit 64 ;;
esac
