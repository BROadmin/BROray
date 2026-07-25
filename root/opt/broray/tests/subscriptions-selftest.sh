#!/bin/sh

set -u
ROOT="${BRORAY_ROOT:-/opt/broray}"
INIT_ROOT="${BRORAY_INIT_ROOT:-/opt/etc/init.d}"
ASH_BIN="${BRORAY_ASH_BIN:-}"

if [ -z "$ASH_BIN" ]; then
    if [ -x /opt/bin/ash ]; then
        ASH_BIN="/opt/bin/ash"
    elif command -v ash >/dev/null 2>&1; then
        ASH_BIN="$(command -v ash)"
    else
        ASH_BIN="/bin/sh"
    fi
fi

PATH="$ROOT/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

PASS=0
FAIL=0

ok()
{
    PASS=$((PASS + 1))
    printf 'OK  %s\n' "$1"
}

bad()
{
    FAIL=$((FAIL + 1))
    printf 'ERR %s\n' "$1" >&2
}

for syntax_file in \
    "$ROOT/lib/server-subscription-service.sh" \
    "$ROOT/lib/subscription-service.sh" \
    "$ROOT/bin/broray-subscriptions" \
    "$ROOT/bin/broray-subscription-scheduler" \
    "$INIT_ROOT/S28broray-subscriptions" \
    "$ROOT"/web-new/api/subscriptions/*.sh \
    "$ROOT"/web-new/api/subscriptions/*.cgi
do
    [ -f "$syntax_file" ] || continue
    if "$ASH_BIN" -n "$syntax_file" 2>/dev/null; then
        ok "syntax $(basename "$syntax_file")"
    else
        bad "syntax $syntax_file"
    fi
done

. "$ROOT/lib/subscription-service.sh"

TEST_ROOT="$ROOT/tmp/subscriptions-selftest.$$"
mkdir -p "$TEST_ROOT"
trap 'rm -rf "$TEST_ROOT"' EXIT INT TERM

cat > "$TEST_ROOT/base.json" <<'JSON'
{
  "id": "x",
  "name": "One",
  "uri": "secret",
  "protocol": "vless",
  "address": "edge.example.com",
  "port": 443,
  "uuid": "11111111-1111-1111-1111-111111111111",
  "network": "xhttp",
  "security": "reality",
  "reality": {"serverName":"cdn.example.com","publicKey":"abc"},
  "xhttp": {"path":"/api","mode":"auto"},
  "source": {"type":"subscription","subscriptionId":"test"}
}
JSON
jq '.name="Two"' "$TEST_ROOT/base.json" > "$TEST_ROOT/name.json"
jq '.uuid="22222222-2222-2222-2222-222222222222"' "$TEST_ROOT/base.json" > "$TEST_ROOT/credential.json"
jq '.address="edge2.example.com"' "$TEST_ROOT/base.json" > "$TEST_ROOT/host.json"
jq '.xhttp.path="/other"' "$TEST_ROOT/base.json" > "$TEST_ROOT/transport.json"

key_base="$(broray_server_subscription_import_key "$TEST_ROOT/base.json")"
key_name="$(broray_server_subscription_import_key "$TEST_ROOT/name.json")"
key_credential="$(broray_server_subscription_import_key "$TEST_ROOT/credential.json")"
key_host="$(broray_server_subscription_import_key "$TEST_ROOT/host.json")"
key_transport="$(broray_server_subscription_import_key "$TEST_ROOT/transport.json")"

[ "$key_base" = "$key_name" ] && ok "name keeps identity" || bad "name changed identity"
[ "$key_base" = "$key_credential" ] && ok "credentials keep identity" || bad "credentials changed identity"
[ "$key_base" != "$key_host" ] && ok "host changes identity" || bad "host did not change identity"
[ "$key_base" != "$key_transport" ] && ok "transport changes identity" || bad "transport did not change identity"

if broray_subscription_validate_remote_url "http://127.0.0.1/test" >/dev/null 2>&1; then
    bad "loopback URL accepted"
else
    ok "loopback URL rejected"
fi

if broray_subscription_validate_remote_url "https://1.1.1.1/test" >/dev/null 2>&1; then
    ok "public literal URL accepted"
else
    bad "public literal URL rejected"
fi

CRUD_ROOT="$TEST_ROOT/crud"
mkdir -p "$CRUD_ROOT/config/subscriptions" "$CRUD_ROOT/run/subscriptions" "$CRUD_ROOT/tmp" "$CRUD_ROOT/logs"
(
    BRORAY_BASE="$ROOT"
    BRORAY_SUB_BASE="$CRUD_ROOT"
    BRORAY_SUB_DIR="$CRUD_ROOT/config/subscriptions"
    BRORAY_SUB_RUN="$CRUD_ROOT/run/subscriptions"
    BRORAY_SUB_TMP="$CRUD_ROOT/tmp"
    BRORAY_SUB_LOG="$CRUD_ROOT/logs/subscriptions.log"
    export BRORAY_BASE BRORAY_SUB_BASE BRORAY_SUB_DIR BRORAY_SUB_RUN BRORAY_SUB_TMP BRORAY_SUB_LOG
    . "$ROOT/lib/subscription-service.sh"
    cat > "$CRUD_ROOT/body.json" <<'JSON'
{
  "name": "Selftest",
  "url": "https://1.1.1.1/subscription?token=very-secret-value",
  "enabled": true,
  "autoUpdateEnabled": false,
  "updateIntervalMinutes": 60,
  "updateImmediately": false
}
JSON
    broray_subscription_create "$CRUD_ROOT/body.json" > "$CRUD_ROOT/created.json" || exit 1
    jq -e '.name == "Selftest" and .displayUrl != .url' "$CRUD_ROOT/created.json" >/dev/null || exit 2
    test_id="$(jq -r '.id' "$CRUD_ROOT/created.json")"
    broray_subscription_list > "$CRUD_ROOT/list.json" || exit 3
    jq -e 'length == 1' "$CRUD_ROOT/list.json" >/dev/null || exit 4
    broray_subscription_summary > "$CRUD_ROOT/summary.json" || exit 5
    jq -e '.total == 1 and .autoUpdateEnabled == false' "$CRUD_ROOT/summary.json" >/dev/null || exit 6
    broray_subscription_delete "$test_id" >/dev/null || exit 7
    broray_subscription_list > "$CRUD_ROOT/list-after.json" || exit 8
    jq -e 'length == 0' "$CRUD_ROOT/list-after.json" >/dev/null || exit 9
)
crud_result="$?"
if [ "$crud_result" -eq 0 ]; then
    ok "CRUD and URL masking"
else
    bad "CRUD selftest code=$crud_result"
fi

if BRORAY_ROOT="$ROOT" \
   BRORAY_BASE="$ROOT" \
   BRORAY_SUB_BASE="$CRUD_ROOT" \
   BRORAY_SUB_DIR="$CRUD_ROOT/config/subscriptions" \
   BRORAY_SUB_RUN="$CRUD_ROOT/run/subscriptions" \
   BRORAY_SUB_TMP="$CRUD_ROOT/tmp" \
   BRORAY_SUB_LOG="$CRUD_ROOT/logs/subscriptions.log" \
   "$ASH_BIN" "$ROOT/bin/broray-subscriptions" summary | jq -e \
    'has("total") and has("serversReceived") and has("lastUpdateStatus")' \
    >/dev/null 2>&1; then
    ok "summary contract"
else
    bad "summary contract"
fi

printf '\nPASS=%s FAIL=%s\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
