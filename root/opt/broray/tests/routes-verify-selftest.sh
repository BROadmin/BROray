#!/bin/sh

set -eu

VERIFY_CGI="${1:-/opt/broray/web-new/api/routes/verify.cgi}"
API_LOCK_LIBRARY="${2:-/opt/broray/lib/routes-api-operation.sh}"
WORK="/tmp/broray-routes-verify-selftest.$$"
ROOT="$WORK/root"
STATE="$ROOT/routes/state/demo.json"

cleanup()
{
    rm -rf "$WORK"
}

fail()
{
    echo "BROray routes verify self-test: FAIL — $*" >&2
    exit 1
}

trap cleanup EXIT HUP INT TERM

mkdir -p \
    "$ROOT/bin" \
    "$ROOT/lib" \
    "$ROOT/routes/state" \
    "$ROOT/web-new/api"

[ -r "$API_LOCK_LIBRARY" ] || fail "модуль блокировки API не найден"
cp "$API_LOCK_LIBRARY" "$ROOT/lib/routes-api-operation.sh" || fail "не удалось скопировать модуль блокировки API"
chmod 755 "$ROOT/lib/routes-api-operation.sh"

cat >"$ROOT/routes/bundles.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundles": ["demo"]
}
JSON

cat >"$ROOT/web-new/api/auth-common.sh" <<'SH'
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
    jq -cn \
        --arg status "$status" \
        --arg code "$code" \
        --arg message "$message" \
        --arg details "$details" '
        {success:false,status:$status,data:null,error:{code:$code,message:$message,details:$details}}
    '
    exit 0
}

broray_api_success()
{
    printf '%s\n' "$1"
    exit 0
}
SH

cat >"$ROOT/bin/broray-routes" <<'SH'
#!/bin/sh
set -u

command_name="${1:-}"
scenario="${VERIFY_SCENARIO:-complete}"

case "$command_name" in
    build-export)
        if [ "$scenario" = "invalid_local" ]; then
            echo "Каталог маршрутов повреждён." >&2
            exit 1
        fi
        echo "Файл Keenetic подготовлен"
        ;;
    plan)
        case "$scenario" in
            complete)
                mode="none"
                can_apply=true
                managed=2
                create=0
                conflicts=0
                external=0
                ;;
            not_installed)
                mode="install"
                can_apply=true
                managed=0
                create=2
                conflicts=0
                external=0
                ;;
            conflict)
                mode="none"
                can_apply=false
                managed=1
                create=0
                conflicts=1
                external=1
                ;;
            router_unavailable)
                echo "Не удалось прочитать running-config." >&2
                exit 1
                ;;
            *) exit 2 ;;
        esac

        jq -n \
            --arg mode "$mode" \
            --argjson canApply "$can_apply" \
            --argjson managed "$managed" \
            --argjson create "$create" \
            --argjson conflicts "$conflicts" \
            --argjson external "$external" '
            {
                schemaVersion: 1,
                bundleId: "demo",
                mode: $mode,
                targetInterface: "Proxy0",
                managedMetric: 1200,
                contentSha256: "demo-sha",
                canApply: $canApply,
                summary: {
                    total: 2,
                    addedRoutes: 0,
                    removedRoutes: 0,
                    unchangedRoutes: 2,
                    toCreate: $create,
                    managedExisting: $managed,
                    externalExisting: $external,
                    conflicts: $conflicts,
                    toDelete: 0,
                    sharedKept: 0,
                    sharedToRestore: 0,
                    alreadyAbsent: 0
                },
                desired: [],
                obsolete: [],
                blocking: []
            }
        '
        ;;
    *) exit 2 ;;
esac
SH
chmod +x "$ROOT/bin/broray-routes"

write_state()
{
    installed_json="$1"
    cat >"$STATE" <<JSON
{
  "schemaVersion": 1,
  "bundleId": "demo",
  "status": "downloaded",
  "availableVersion": {
    "contentSha256": "demo-sha",
    "sourceSetSha256": "source-sha"
  },
  "downloadedVersion": {
    "contentSha256": "demo-sha",
    "sourceSetSha256": "source-sha",
    "sourceFileCount": 2
  },
  "installedVersion": $installed_json,
  "routeCount": 2,
  "lastCheckedAt": "2026-07-28T08:00:00+0000",
  "lastVerifiedAt": null,
  "lastDownloadedAt": "2026-07-28T08:01:00+0000",
  "lastExportedAt": null,
  "lastDeletedAt": null,
  "lastError": null,
  "checkResult": null,
  "verifyResult": null,
  "downloadResult": null,
  "exportBuild": null,
  "preflight": null,
  "exportResult": null,
  "deleteResult": null,
  "updatedAt": "2026-07-28T08:01:00+0000"
}
JSON
}

run_verify()
{
    scenario="$1"
    output="$WORK/$scenario.out"
    REQUEST_METHOD=POST \
    QUERY_STRING='bundleId=demo' \
    BRORAY_ROOT="$ROOT" \
    VERIFY_SCENARIO="$scenario" \
        busybox ash "$VERIFY_CGI" >"$output"
}

INSTALLED='{"contentSha256":"demo-sha","sourceSetSha256":"source-sha"}'

write_state "$INSTALLED"
run_verify complete
jq -e '
    (.lastError == null) and
    (.verifyResult.success == true) and
    (.verifyResult.local.valid == true) and
    (.verifyResult.local.duplicateRouteCount == 0) and
    (.verifyResult.keenetic.status == "complete") and
    (.verifyResult.keenetic.presentRouteCount == 2) and
    (.verifyResult.keenetic.missingRouteCount == 0)
' "$STATE" >/dev/null || fail "полное соответствие определено неверно"

write_state null
run_verify not_installed
jq -e '
    (.verifyResult.success == true) and
    (.verifyResult.keenetic.status == "not_installed") and
    (.verifyResult.keenetic.expectedRouteCount == 2) and
    (.verifyResult.keenetic.presentRouteCount == 0)
' "$STATE" >/dev/null || fail "неустановленный набор определён неверно"

write_state "$INSTALLED"
run_verify conflict
jq -e '
    (.lastError == null) and
    (.verifyResult.success == false) and
    (.verifyResult.local.valid == true) and
    (.verifyResult.keenetic.status == "conflict") and
    (.verifyResult.keenetic.conflictCount == 1)
' "$STATE" >/dev/null || fail "конфликт Keenetic определён неверно"

write_state null
run_verify invalid_local
jq -e '
    (.lastError.code == "ROUTES_SET_INVALID") and
    (.verifyResult.success == false) and
    (.verifyResult.local.valid == false) and
    (.verifyResult.keenetic.status == "not_checked")
' "$STATE" >/dev/null || fail "повреждение локального набора не сохранено"

write_state "$INSTALLED"
run_verify router_unavailable
jq -e '
    (.lastError.code == "ROUTES_VERIFY_ROUTER_FAILED") and
    (.verifyResult.local.valid == true) and
    (.verifyResult.keenetic.available == false) and
    (.verifyResult.keenetic.status == "unavailable")
' "$STATE" >/dev/null || fail "недоступность Keenetic не сохранена"

echo "BROray routes verify self-test: PASS"
