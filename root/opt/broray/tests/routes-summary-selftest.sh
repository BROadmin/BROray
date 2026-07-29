#!/bin/sh

set -u

PATH="/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

SUMMARY_LIBRARY="${BRORAY_SUMMARY_LIBRARY:-/opt/broray/lib/routes-summary.sh}"
TEST_ROOT="${BRORAY_SUMMARY_TEST_ROOT:-/tmp/broray-routes-summary-selftest-$$}"

cleanup()
{
    rm -rf "$TEST_ROOT"
}

fail()
{
    echo "BROray routes summary self-test: FAIL — $*" >&2
    cleanup
    exit 1
}

trap cleanup EXIT HUP INT TERM

[ -r "$SUMMARY_LIBRARY" ] ||
    fail "библиотека недоступна"

. "$SUMMARY_LIBRARY"

mkdir -p \
    "$TEST_ROOT/routes/state" \
    "$TEST_ROOT/routes/installed/bundles" \
    "$TEST_ROOT/routes/installed" \
    "$TEST_ROOT/routes/locks" ||
    fail "не удалось создать тестовую структуру"

cat >"$TEST_ROOT/routes/bundles.json" <<'EOF_BUNDLES'
{
  "schemaVersion": 1,
  "bundles": ["telegram"]
}
EOF_BUNDLES

cat >"$TEST_ROOT/routes/config.json" <<'EOF_CONFIG'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routeComment": "BROray"
}
EOF_CONFIG

write_state()
{
    state_status="$1"
    available_json="$2"
    downloaded_json="$3"
    installed_json="$4"
    verify_json="${5:-null}"

    cat >"$TEST_ROOT/routes/state/telegram.json" <<EOF_STATE
{
  "schemaVersion": 1,
  "bundleId": "telegram",
  "status": "$state_status",
  "routeCount": 2,
  "availableVersion": $available_json,
  "downloadedVersion": $downloaded_json,
  "installedVersion": $installed_json,
  "lastCheckedAt": "2026-07-23T10:00:00+0300",
  "lastVerifiedAt": null,
  "lastDownloadedAt": "2026-07-23T10:01:00+0300",
  "lastExportedAt": null,
  "lastDeletedAt": null,
  "lastError": null,
  "checkResult": {"message":"Проверка обновления завершена"},
  "verifyResult": $verify_json,
  "downloadResult": {"message":"Маршруты готовы к экспорту"},
  "exportResult": null,
  "deleteResult": null,
  "updatedAt": "2026-07-23T10:01:00+0300"
}
EOF_STATE
}

write_bundle_empty()
{
    cat >"$TEST_ROOT/routes/installed/bundles/telegram.json" <<'EOF_BUNDLE'
{
  "schemaVersion": 1,
  "bundleId": "telegram",
  "installedVersion": null,
  "routeKeys": [],
  "managedRouteKeys": [],
  "externalRouteKeys": [],
  "installedAt": null,
  "removedAt": null
}
EOF_BUNDLE
}

write_global_empty()
{
    cat >"$TEST_ROOT/routes/installed/routes.json" <<'EOF_GLOBAL'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routes": []
}
EOF_GLOBAL
}

VERSION_OLD='{"sourceCommit":"old","sourceDate":"2026-01-01T00:00:00Z","contentSha256":"old-sha"}'
VERSION_NEW='{"sourceCommit":"new","sourceDate":"2026-02-01T00:00:00Z","contentSha256":"new-sha"}'
VERIFY_NEW_NOT_INSTALLED='{"contentSha256":"new-sha","local":{"valid":true},"keenetic":{"status":"not_installed"},"message":"Набор исправен"}'
VERIFY_OLD_COMPLETE='{"contentSha256":"old-sha","local":{"valid":true},"keenetic":{"status":"complete"},"message":"Набор соответствует Keenetic"}'
VERIFY_NEW_UPDATE_PENDING='{"contentSha256":"new-sha","local":{"valid":true},"keenetic":{"status":"update_pending"},"message":"Ожидается обновление Keenetic"}'
VERIFY_NEW_INVALID='{"contentSha256":"new-sha","local":{"valid":false},"keenetic":{"status":"not_checked"},"message":"Набор повреждён"}'

write_state "downloaded" "$VERSION_NEW" "$VERSION_NEW" "null"
write_bundle_empty
write_global_empty

BRORAY_ROOT="$TEST_ROOT"
export BRORAY_ROOT

broray_routes_summary telegram >"$TEST_ROOT/downloaded.json" ||
    fail "downloaded summary не сформирован"

jq -e '
    (.state == "downloaded") and
    (.installed == false) and
    (.downloaded == true) and
    (.verificationRequired == true) and
    (.recommendedAction == "verify") and
    (.routeCount == 2) and
    (.installedRouteCount == 0) and
    (.consistent == true)
' "$TEST_ROOT/downloaded.json" >/dev/null 2>&1 ||
    fail "downloaded summary неверен"

write_state "downloaded" "$VERSION_NEW" "$VERSION_NEW" "null" "$VERIFY_NEW_NOT_INSTALLED"

broray_routes_summary telegram >"$TEST_ROOT/verified.json" ||
    fail "verified summary не сформирован"

jq -e '
    (.state == "downloaded") and
    (.verificationCurrent == true) and
    (.localSetValid == true) and
    (.recommendedAction == "install")
' "$TEST_ROOT/verified.json" >/dev/null 2>&1 ||
    fail "verified summary неверен"

cat >"$TEST_ROOT/routes/installed/bundles/telegram.json" <<EOF_BUNDLE_INSTALLED
{
  "schemaVersion": 1,
  "bundleId": "telegram",
  "installedVersion": $VERSION_OLD,
  "routeKeys": ["route-1", "route-2"],
  "managedRouteKeys": ["route-1", "route-2"],
  "externalRouteKeys": [],
  "installedAt": "2026-07-23T10:02:00+0300",
  "removedAt": null
}
EOF_BUNDLE_INSTALLED

cat >"$TEST_ROOT/routes/installed/routes.json" <<'EOF_GLOBAL_INSTALLED'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routes": [
    {"key":"route-1","owners":["telegram"]},
    {"key":"route-2","owners":["telegram"]}
  ]
}
EOF_GLOBAL_INSTALLED

write_state "installed" "$VERSION_OLD" "$VERSION_OLD" "$VERSION_OLD" "$VERIFY_OLD_COMPLETE"

broray_routes_summary telegram >"$TEST_ROOT/installed.json" ||
    fail "installed summary не сформирован"

jq -e '
    (.state == "installed") and
    (.installed == true) and
    (.recommendedAction == "check") and
    (.installedRouteCount == 2) and
    (.managedRouteCount == 2) and
    (.globalOwnedRouteCount == 2) and
    (.consistent == true)
' "$TEST_ROOT/installed.json" >/dev/null 2>&1 ||
    fail "installed summary неверен"

write_state "installed" "$VERSION_NEW" "$VERSION_NEW" "$VERSION_OLD" "$VERIFY_NEW_UPDATE_PENDING"

broray_routes_summary telegram >"$TEST_ROOT/update.json" ||
    fail "update summary не сформирован"

jq -e '
    (.state == "update_available") and
    (.updateAvailable == true) and
    (.downloadRequired == false) and
    (.exportRequired == true) and
    (.recommendedAction == "update") and
    (.consistent == true)
' "$TEST_ROOT/update.json" >/dev/null 2>&1 ||
    fail "update summary неверен"

write_state "downloaded" "$VERSION_NEW" "$VERSION_NEW" "null" "$VERIFY_NEW_INVALID"
write_bundle_empty
write_global_empty

broray_routes_summary telegram >"$TEST_ROOT/invalid-local.json" ||
    fail "invalid local summary не сформирован"

jq -e '
    (.state == "error") and
    (.localSetValid == false) and
    (.downloadActionRequired == true) and
    (.recommendedAction == "download")
' "$TEST_ROOT/invalid-local.json" >/dev/null 2>&1 ||
    fail "invalid local summary неверен"

cat >"$TEST_ROOT/routes/installed/bundles/telegram.json" <<EOF_BUNDLE_UPDATE
{
  "schemaVersion": 1,
  "bundleId": "telegram",
  "installedVersion": $VERSION_OLD,
  "routeKeys": ["route-1", "route-2"],
  "managedRouteKeys": ["route-1", "route-2"],
  "externalRouteKeys": [],
  "installedAt": "2026-07-23T10:02:00+0300",
  "removedAt": null
}
EOF_BUNDLE_UPDATE

cat >"$TEST_ROOT/routes/installed/routes.json" <<'EOF_GLOBAL_UPDATE'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routes": [
    {"key":"route-1","owners":["telegram"]},
    {"key":"route-2","owners":["telegram"]}
  ]
}
EOF_GLOBAL_UPDATE

write_state "installed" "$VERSION_NEW" "$VERSION_NEW" "$VERSION_OLD" "$VERIFY_NEW_UPDATE_PENDING"

mkdir -p "$TEST_ROOT/routes/locks/operation.lock"

broray_routes_summary telegram >"$TEST_ROOT/busy.json" ||
    fail "busy summary не сформирован"

jq -e '
    (.state == "busy") and
    (.operationRunning == true) and
    (.recommendedAction == "wait")
' "$TEST_ROOT/busy.json" >/dev/null 2>&1 ||
    fail "busy summary неверен"

rmdir "$TEST_ROOT/routes/locks/operation.lock"
write_bundle_empty

broray_routes_summary telegram >"$TEST_ROOT/inconsistent.json" ||
    fail "inconsistent summary не сформирован"

jq -e '
    (.state == "error") and
    (.health == "error") and
    (.consistent == false) and
    (.error.code == "ROUTES_STATE_INCONSISTENT")
' "$TEST_ROOT/inconsistent.json" >/dev/null 2>&1 ||
    fail "inconsistent summary неверен"

echo "BROray routes summary self-test: PASS"
