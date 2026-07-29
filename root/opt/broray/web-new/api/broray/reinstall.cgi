#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh

broray_api_require_method POST
broray_api_require_session
[ "${HTTP_X_BRORAY_REQUEST:-}" = 1 ] || {
    broray_api_error \
        "403 Forbidden" \
        "INVALID_REQUEST" \
        "Запрос отклонён."
}

broray_api_print_json_headers
printf '\r\n'
/opt/broray/bin/broray-system reinstall-start
