#!/opt/bin/ash

. /opt/broray/lib/web-auth.sh

if [ "${REQUEST_METHOD:-}" != "GET" ]; then
    broray_json_response \
        "405 Method Not Allowed" \
        '{"ok":false,"error":"METHOD_NOT_ALLOWED"}'
    exit 0
fi

broray_session_require

escaped_username="$(
    broray_json_escape "$BRORAY_SESSION_USERNAME"
)"

broray_json_response \
    "200 OK" \
    "{\"ok\":true,\"authenticated\":true,\"user\":\"$escaped_username\",\"expiresAt\":$BRORAY_SESSION_EXPIRES}"
