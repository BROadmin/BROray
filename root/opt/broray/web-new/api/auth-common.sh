#!/opt/bin/ash
PATH=/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH

BRORAY_SESSION_CGI="${BRORAY_SESSION_CGI:-/opt/broray/web-new/api/session.cgi}"

broray_api_print_json_headers() {
    printf '%s\r\n' 'Content-Type: application/json; charset=utf-8'
    printf '%s\r\n' 'Cache-Control: no-store, no-cache, must-revalidate'
    printf '%s\r\n' 'Pragma: no-cache'
    printf '%s\r\n' 'X-Content-Type-Options: nosniff'
    printf '%s\r\n' 'X-Frame-Options: DENY'
    printf '%s\r\n' 'Referrer-Policy: no-referrer'
}

broray_api_error() {
    http_status="$1"
    error_code="$2"
    error_message="$3"
    error_details="${4:-}"

    printf 'Status: %s\r\n' "$http_status"
    broray_api_print_json_headers
    printf '\r\n'

    jq -n \
        --arg code "$error_code" \
        --arg message "$error_message" \
        --arg details "$error_details" '
        {
            success: false,
            data: null,
            error: {
                code: $code,
                message: $message,
                details: (
                    if $details == ""
                    then null
                    else $details
                    end
                )
            }
        }
    '

    exit 0
}

broray_api_require_method() {
    required_method="$1"
    actual_method="${REQUEST_METHOD:-GET}"

    if [ "$actual_method" != "$required_method" ]; then
        broray_api_error \
            "405 Method Not Allowed" \
            "METHOD_NOT_ALLOWED" \
            "Для этой операции требуется метод $required_method." \
            "Получен метод $actual_method."
    fi
}

broray_api_require_session() {
    if [ ! -r /opt/broray/lib/web-auth.sh ]; then
        broray_api_error \
            "500 Internal Server Error" \
            "SESSION_BACKEND_UNAVAILABLE" \
            "Модуль проверки сессии недоступен."
    fi

    . /opt/broray/lib/web-auth.sh

    session_token="$(
        broray_cookie_value BRORAY_SESSION 2>/dev/null || true
    )"

    if ! broray_session_validate "$session_token"; then
        broray_api_error \
            "401 Unauthorized" \
            "AUTH_REQUIRED" \
            "Требуется авторизация."
    fi

    BRORAY_SESSION_TOKEN="$session_token"
    export BRORAY_SESSION_TOKEN
}
broray_api_success() {
    data_json="$1"

    broray_api_print_json_headers
    printf '\r\n'

    printf '%s\n' "$data_json" |
        jq '{
            success: true,
            data: .,
            error: null
        }'
}
