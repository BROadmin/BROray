#!/opt/bin/ash

. /opt/broray/lib/web-auth.sh

broray_session_require

escaped_username="$(
    broray_json_escape "$BRORAY_SESSION_USERNAME"
)"

broray_json_response \
    "200 OK" \
    "{\"ok\":true,\"protected\":true,\"user\":\"$escaped_username\"}"
