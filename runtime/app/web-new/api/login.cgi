#!/opt/bin/ash

. /opt/broray/lib/web-auth.sh
. /opt/broray/lib/web-request-body.sh

if [ "${REQUEST_METHOD:-}" != "POST" ]; then
    broray_json_response \
        "405 Method Not Allowed" \
        '{"ok":false,"error":"METHOD_NOT_ALLOWED"}'
    exit 0
fi

request_file="/opt/broray/tmp/broray-login-body-$$"
umask 077
trap 'rm -f "$request_file"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

body_rc=0
broray_web_request_body_to_file "$request_file" 8192 || body_rc=$?
case "$body_rc" in
    0) ;;
    2|3|4)
        broray_json_response "400 Bad Request" '{"ok":false,"error":"INVALID_REQUEST"}'
        exit 0
        ;;
    *)
        broray_json_response "400 Bad Request" '{"ok":false,"error":"REQUEST_BODY_INCOMPLETE"}'
        exit 0
        ;;
esac
jq -e 'type == "object"' "$request_file" >/dev/null 2>&1 || {
    rm -f "$request_file"
    broray_json_response "400 Bad Request" '{"ok":false,"error":"REQUEST_JSON_INVALID"}'
    exit 0
}
request_body="$(cat "$request_file")"
rm -f "$request_file"

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

auth_result=0
broray_keenetic_authenticate "$login" "$password" || auth_result=$?

if [ "$auth_result" -eq 0 ]; then
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
    printf '{"ok":true,"user":"%s","redirect":"/home.html?v=WebUI-3.1.0-r09c02"}\n' \
        "$escaped_login"

    exit 0
fi

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
