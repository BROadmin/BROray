#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
PREFLIGHT="$ROOT/lib/routes-router-preflight.sh"
EXPORT="$ROOT/lib/routes-router-export.sh"
WORK="$ROOT/routes/tmp/routes-rci-fetch-selftest-$$"
MOCK="$WORK/mock-bin"

cleanup()
{
    rm -rf "$WORK"
}

fail()
{
    echo "ОШИБКА self-test: $*" >&2
    exit 1
}

trap cleanup EXIT HUP INT TERM

mkdir -p "$MOCK"

cat >"$MOCK/ndmc" <<'MOCK_NDMC'
#!/bin/sh

[ "${1:-}" = "-c" ] &&
[ "${2:-}" = "show running-config" ] ||
    exit 2

cat <<'RUNNING_CONFIG'
ip route 104.16.0.0 255.240.0.0 Wireguard0 1000
RUNNING_CONFIG
MOCK_NDMC
chmod 755 "$MOCK/ndmc"

OLD_PATH="$PATH"
PATH="$MOCK:/opt/bin:/usr/bin:/bin"
export PATH

. "$PREFLIGHT"
. "$EXPORT"

broray_routes_preflight_fetch_rci \
    "$WORK/preflight-raw.json" \
    "$WORK/preflight.json" ||
    fail "preflight fetch завершился ошибкой"

jq -e '
    (.schemaVersion == 1) and
    (.source == "running-config") and
    (.routes | length == 1) and
    (.routes[0].destination == "104.16.0.0/12") and
    (.routes[0].proto == "static")
' "$WORK/preflight.json" >/dev/null ||
    fail "preflight вернул неверный JSON"

broray_routes_router_export_fetch_rci \
    "$WORK/export.json" ||
    fail "export fetch завершился ошибкой"

jq -e '
    (.schemaVersion == 1) and
    (.source == "running-config") and
    (.routes | length == 1) and
    (.routes[0].interface == "Wireguard0")
' "$WORK/export.json" >/dev/null ||
    fail "export вернул неверный JSON"

PATH="$OLD_PATH"
export PATH

echo "BROray routes RCI fetch self-test: PASS"
