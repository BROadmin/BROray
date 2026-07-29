#!/bin/sh

set -eu

SOURCE_ROOT="${BRORAY_ROOT:-/opt/broray}"
PREFLIGHT_CGI="${1:-$SOURCE_ROOT/web-new/api/routes/preflight.cgi}"
WORK="${TMPDIR:-/tmp}/broray-routes-preflight-api-selftest-$$"
ROOT="$WORK/root"

cleanup()
{
    rm -rf "$WORK"
}

fail()
{
    echo "BROray routes preflight API self-test: FAIL — $*" >&2
    exit 1
}

trap cleanup EXIT HUP INT TERM

[ -r "$PREFLIGHT_CGI" ] || fail "CGI предварительной проверки недоступен"
mkdir -p \
    "$ROOT/bin" "$ROOT/lib" "$ROOT/run" \
    "$ROOT/routes/state" "$ROOT/routes/installed/bundles" \
    "$ROOT/routes/catalog/demo" "$ROOT/routes/preflight" \
    "$ROOT/web-new/api"

cp "$SOURCE_ROOT/lib/routes-api-operation.sh" "$ROOT/lib/routes-api-operation.sh"
cp "$SOURCE_ROOT/lib/routes-operation-preflight.sh" "$ROOT/lib/routes-operation-preflight.sh"
chmod 755 "$ROOT/lib/routes-api-operation.sh" "$ROOT/lib/routes-operation-preflight.sh"

cat >"$ROOT/web-new/api/auth-common.sh" <<'AUTH'
broray_api_require_method()
{
    [ "${REQUEST_METHOD:-}" = "$1" ] || exit 1
}
broray_api_require_session()
{
    return 0
}
broray_api_error()
{
    status="$1"
    code="$2"
    message="$3"
    details="${4:-}"
    jq -cn --arg status "$status" --arg code "$code" --arg message "$message" --arg details "$details" \
        '{success:false,status:$status,data:null,error:{code:$code,message:$message,details:$details}}'
    exit 0
}
broray_api_success()
{
    printf '%s\n' "$1"
    exit 0
}
AUTH

cat >"$ROOT/routes/bundles.json" <<'JSON'
{"schemaVersion":1,"bundles":["demo"]}
JSON
cat >"$ROOT/routes/state/demo.json" <<'JSON'
{"schemaVersion":1,"bundleId":"demo","downloadedVersion":{"contentSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
JSON
cat >"$ROOT/routes/installed/bundles/demo.json" <<'JSON'
{"schemaVersion":1,"bundleId":"demo","installedVersion":null,"routeKeys":[],"managedRouteKeys":[],"externalRouteKeys":[],"targetInterface":"Proxy0","managedMetric":1200}
JSON

cat >"$ROOT/bin/broray-routes" <<'CLI'
#!/bin/sh
[ "${1:-}" = plan ] && [ "${2:-}" = demo ] || exit 1
cat <<'JSON'
{
  "schemaVersion":1,
  "bundleId":"demo",
  "mode":"install",
  "targetInterface":"Proxy0",
  "managedMetric":1200,
  "contentSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "canApply":true,
  "summary":{"total":2,"managedExisting":0,"toCreate":2,"toDelete":0,"sharedKept":0,"alreadyAbsent":0,"conflicts":0,"externalExisting":0,"unchangedRoutes":0,"addedRoutes":2,"removedRoutes":0}
}
JSON
CLI
chmod 755 "$ROOT/bin/broray-routes"

cat >"$ROOT/bin/ndmc" <<'NDMC'
#!/bin/sh
[ "${1:-}" = -c ] && [ "${2:-}" = "show interface Proxy0" ] || exit 1
printf '%s\n' 'id: Proxy0' 'connected: yes' 'state: up'
NDMC
chmod 755 "$ROOT/bin/ndmc"

output="$(
    BRORAY_ROOT="$ROOT" \
    BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC="$ROOT/bin/ndmc" \
    REQUEST_METHOD=POST \
    QUERY_STRING='bundleId=demo&action=export' \
    /opt/bin/ash "$PREFLIGHT_CGI"
)"
printf '%s\n' "$output" |
    jq -e '
        .schemaVersion == 1 and .bundleId == "demo" and
        .requestedAction == "export" and .operation == "install" and
        .ready == true and .summary.total == 2 and .summary.toCreate == 2 and
        .checks.operationLock.ok == true and .checks.keenetic.ok == true and
        (.token | length) == 64
    ' >/dev/null || fail "CGI вернул неверную предварительную проверку"
[ ! -d "$ROOT/run/global-operation.lock" ] || fail "CGI не освободил общую блокировку"

mkdir -p "$ROOT/run/global-operation.lock"
printf '%s\n' "$$" >"$ROOT/run/global-operation.lock/pid"
printf '%s\n' routes >"$ROOT/run/global-operation.lock/scope"
printf '%s\n' export >"$ROOT/run/global-operation.lock/action"
printf '%s\n' telegram >"$ROOT/run/global-operation.lock/bundle"
printf '%s\n' 2026-01-01T00:00:00+0000 >"$ROOT/run/global-operation.lock/startedAt"

output="$(
    BRORAY_ROOT="$ROOT" \
    BRORAY_ROUTES_OPERATION_PREFLIGHT_NDMC="$ROOT/bin/ndmc" \
    REQUEST_METHOD=POST \
    QUERY_STRING='bundleId=demo&action=export' \
    /opt/bin/ash "$PREFLIGHT_CGI"
)"
printf '%s\n' "$output" |
    jq -e '.success == false and .error.code == "ROUTES_OPERATION_BUSY"' >/dev/null ||
    fail "CGI не отклонил конфликтующую операцию"

echo "BROray routes preflight API self-test: PASS"
