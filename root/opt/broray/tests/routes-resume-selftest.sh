#!/bin/sh

set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
SYNC_LIBRARY="$ROOT/lib/routes-router-sync.sh"
PROGRESS_LIBRARY="$ROOT/lib/routes-operation-progress.sh"
WORK="${TMPDIR:-/tmp}/broray-routes-resume-selftest-$$"

cleanup() { rm -rf "$WORK"; }
fail() { echo "ОШИБКА self-test: $*" >&2; exit 1; }
trap cleanup EXIT HUP INT TERM

mkdir -p "$WORK/routes/installed/bundles" "$WORK/routes/state" "$WORK/routes/tmp" "$WORK/routes/operations"
cat >"$WORK/routes/installed/routes.json" <<'JSON'
{"schemaVersion":1,"managedInterface":"Proxy0","managedMetric":1200,"routes":[],"updatedAt":null}
JSON
cat >"$WORK/routes/installed/bundles/telegram.json" <<'JSON'
{"schemaVersion":1,"bundleId":"telegram","installedVersion":null,"routeKeys":[],"managedRouteKeys":[],"externalRouteKeys":[],"targetInterface":"Proxy0","managedMetric":1200}
JSON
cat >"$WORK/routes/state/telegram.json" <<'JSON'
{"schemaVersion":1,"bundleId":"telegram","status":"downloaded","downloadedVersion":{"contentSha256":"abc"},"installedVersion":null,"lastError":null}
JSON
cat >"$WORK/plan.json" <<'JSON'
{
  "schemaVersion":1,
  "bundleId":"telegram",
  "mode":"install",
  "targetInterface":"Proxy0",
  "managedMetric":1200,
  "desired":[
    {"status":"create","route":{"key":"r1","family":"ipv4","network":"198.51.100.0","prefix":24,"mask":"255.255.255.0","targetInterface":"Proxy0","gatewayToken":"0.0.0.0","metric":1200,"automatic":null,"exclusive":null,"comment":"BROray"}},
    {"status":"create","route":{"key":"r2","family":"ipv4","network":"203.0.113.0","prefix":24,"mask":"255.255.255.0","targetInterface":"Proxy0","gatewayToken":"0.0.0.0","metric":1200,"automatic":null,"exclusive":null,"comment":"BROray"}}
  ],
  "obsolete":[],
  "summary":{"total":2}
}
JSON

BRORAY_ROOT="$WORK"
BRORAY_ROUTES_ROOT="$WORK/routes"
export BRORAY_ROOT BRORAY_ROUTES_ROOT
. "$SYNC_LIBRARY"
BRORAY_SYNC_ROUTES="$WORK/routes"
BRORAY_SYNC_WORK="$WORK/routes/tmp/sync"
BRORAY_SYNC_BUNDLE="telegram"
BRORAY_SYNC_ADDED_FILE="$BRORAY_SYNC_WORK/added.tsv"
BRORAY_SYNC_DELETED_FILE="$BRORAY_SYNC_WORK/deleted.tsv"
BRORAY_SYNC_PROGRESS_CURRENT=1
BRORAY_SYNC_PROGRESS_TOTAL=2
mkdir -p "$BRORAY_SYNC_WORK"
printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
  create r1 198.51.100.0 24 255.255.255.0 Proxy0 1200 >"$BRORAY_SYNC_ADDED_FILE"
: >"$BRORAY_SYNC_DELETED_FILE"
broray_routes_sync_partial_commit "$WORK/plan.json" "Остановлено: 1 из 2." "" ||
    fail "Не удалось сохранить частично выполненную установку"

jq -e '
  (.routes | length) == 1 and .routes[0].key == "r1" and
  .routes[0].owners == ["telegram"] and .routes[0].actualStatus == "present"
' "$WORK/routes/installed/routes.json" >/dev/null || fail "Неверный частичный общий реестр"
jq -e '
  .routeKeys == ["r1"] and .managedRouteKeys == ["r1"]
' "$WORK/routes/installed/bundles/telegram.json" >/dev/null || fail "Неверный частичный реестр набора"
jq -e '
  .resumeOperation.operation == "install" and .resumeOperation.resumable == true and
  .resumeOperation.current == 1 and .resumeOperation.total == 2 and
  .resumeOperation.created == 1
' "$WORK/routes/state/telegram.json" >/dev/null || fail "Не сохранено состояние продолжения установки"

BRORAY_ROUTES_PROGRESS_DIR="$WORK/routes/operations"
export BRORAY_ROUTES_PROGRESS_DIR
. "$PROGRESS_LIBRARY"
broray_routes_progress_begin telegram install 2 "Начало." || fail "Не создан прогресс"
broray_routes_progress_tick 1 "198.51.100.0/24" || fail "Не записан прогресс"
broray_routes_progress_pause "Остановлено." true "" || fail "Не записана пауза"
values="$(broray_routes_progress_resume_values telegram install 1)"
[ "$(printf '%s' "$values" | cut -f1)" = 1 ] || fail "Не восстановлен текущий счётчик"
[ "$(printf '%s' "$values" | cut -f2)" = 2 ] || fail "Не восстановлена общая цель"
[ "$(printf '%s' "$values" | cut -f3)" = true ] || fail "Операция не распознана как продолжаемая"

echo "BROray routes resume self-test: PASS"
