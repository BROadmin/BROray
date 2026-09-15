#!/opt/bin/ash

. /opt/broray/lib/web-auth.sh

if [ "${REQUEST_METHOD:-}" != "POST" ]; then
    broray_json_response \
        "405 Method Not Allowed" \
        '{"ok":false,"error":"METHOD_NOT_ALLOWED"}'
    exit 0
fi

token="$(broray_cookie_value BRORAY_SESSION)"
broray_session_delete "$token"

printf 'Status: 200 OK\r\n'
printf 'Content-Type: application/json; charset=utf-8\r\n'
printf 'Cache-Control: no-store, no-cache, must-revalidate\r\n'
printf 'Set-Cookie: BRORAY_SESSION=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0\r\n'
printf 'X-Content-Type-Options: nosniff\r\n'
printf '\r\n'
printf '{"ok":true}\n'
