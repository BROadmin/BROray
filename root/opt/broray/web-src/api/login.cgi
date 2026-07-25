#!/opt/bin/ash

. /opt/broray/lib/web-auth.sh

if [ "${REQUEST_METHOD:-}" != "POST" ]; then
    broray_json_response \
        "405 Method Not Allowed" \
        '{"ok":false,"error":"METHOD_NOT_ALLOWED"}'
    exit 0
fi

case "${CONTENT_LENGTH:-}" in
    ''|*[!0-9]*)
        broray_json_response \
            "400 Bad Request" \
            '{"ok":false,"error":"INVALID_REQUEST"}'
        exit 0
        ;;
esac

if [ "$CONTENT_LENGTH" -le 0 ] ||
   [ "$CONTENT_LENGTH" -gt 8192 ]; then
    broray_json_response \
        "400 Bad Request" \
        '{"ok":false,"error":"INVALID_REQUEST"}'
    exit 0
fi

request_body="$(
    dd bs=1 count="$CONTENT_LENGTH" 2>/dev/null
)"

login="$(
    printf '%s' "$request_body" |
        jq -r '.login // empty' 2>/dev/null
)"

password="$(
    printf '%s' "$request_body" |
        jq -r '.password // empty' 2>/dev/null
)"

request_body=""

if [ -z "$login" ] || [ -z "$password" ]; then
    password=""

    broray_json_response \
        "400 Bad Request" \
        '{"ok":false,"error":"FIELDS_REQUIRED","message":"Введите логин и пароль."}'
    exit 0
fi

if [ "${#login}" -gt 128 ] ||
   [ "${#password}" -gt 512 ]; then
    password=""

    broray_json_response \
        "400 Bad Request" \
        '{"ok":false,"error":"INVALID_REQUEST"}'
    exit 0
fi

broray_sessions_cleanup

if broray_keenetic_authenticate "$login" "$password"; then
    password=""

    token="$(broray_session_create "$login")" || {
        broray_json_response \
            "500 Internal Server Error" \
            '{"ok":false,"error":"SESSION_CREATE_FAILED","message":"Не удалось создать сессию."}'
        exit 0
    }

    escaped_login="$(broray_json_escape "$login")"

    printf 'Status: 200 OK\r\n'
    printf 'Content-Type: application/json; charset=utf-8\r\n'
    printf 'Cache-Control: no-store, no-cache, must-revalidate\r\n'
    printf 'Pragma: no-cache\r\n'
    printf 'Set-Cookie: BRORAY_SESSION=%s; Path=/; HttpOnly; SameSite=Strict; Max-Age=1800\r\n' "$token"
    printf 'X-Content-Type-Options: nosniff\r\n'
    printf 'X-Frame-Options: DENY\r\n'
    printf 'Referrer-Policy: no-referrer\r\n'
    printf '\r\n'
    printf '{"ok":true,"user":"%s","redirect":"/home.html"}\n' \
        "$escaped_login"

    exit 0
fi

auth_result="$?"
password=""

if [ "$auth_result" -eq 2 ]; then
    broray_json_response \
        "503 Service Unavailable" \
        '{"ok":false,"error":"KEENETIC_UNAVAILABLE","message":"Не удалось связаться с KeeneticOS."}'
else
    sleep 1

    broray_json_response \
        "401 Unauthorized" \
        '{"ok":false,"error":"INVALID_CREDENTIALS","message":"Неверный логин или пароль."}'
fi
