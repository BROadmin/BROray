#!/opt/bin/ash

. /opt/broray/web-new/api/auth-common.sh

broray_api_require_method POST
broray_api_require_session
[ "${HTTP_X_BRORAY_REQUEST:-}" = 1 ] || {
    broray_api_error '403 Forbidden' INVALID_REQUEST 'Запрос отклонён.'
}

. /opt/broray/web-new/api/broray/updater-api-common.sh
broray_updater_api_call '202 Accepted' request reinstall
