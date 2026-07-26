#!/opt/bin/ash

set -u

ROOT="${BRORAY_SELFTEST_ROOT:-/opt/broray}"
ASH_BIN="${BRORAY_ASH_BIN:-/opt/bin/ash}"
LIBRARY="$ROOT/lib/routes-user-import.sh"
TEST_ROOT="${TMPDIR:-/tmp}/broray-user-routes-selftest-$$"

fail()
{
    echo "BROray user routes self-test: FAIL — $*" >&2
    exit 1
}

cleanup()
{
    rm -rf "$TEST_ROOT"
}

trap cleanup EXIT HUP INT TERM

[ -r "$LIBRARY" ] || fail "не найден $LIBRARY"
command -v jq >/dev/null 2>&1 || fail "jq недоступен"
command -v base64 >/dev/null 2>&1 || fail "base64 недоступен"
command -v sha256sum >/dev/null 2>&1 || fail "sha256sum недоступен"

mkdir -p "$TEST_ROOT/root/lib" "$TEST_ROOT/root/bin" "$TEST_ROOT/root/routes/installed/bundles"
cp "$LIBRARY" "$TEST_ROOT/root/lib/routes-user-import.sh" || fail "не удалось скопировать модуль"
cp "$ROOT/bin/broray-routes-user" "$TEST_ROOT/root/bin/broray-routes-user" || fail "не удалось скопировать CLI"

cat >"$TEST_ROOT/root/routes/config.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routeComment": "BROray",
  "ownershipPolicy": {
    "touchOtherInterfaces": false,
    "modifyExternalRoutes": false,
    "deleteExternalRoutes": false
  }
}
JSON

cat >"$TEST_ROOT/root/routes/bundles.json" <<'JSON'
{"schemaVersion":1,"bundles":["telegram","whatsapp","youtube","chatgpt","facebook","instagram","meta","tiktok","speedtest"]}
JSON

cat >"$TEST_ROOT/good.bat" <<'BAT'
@echo off
route add 10.0.0.1 mask 255.255.255.0 0.0.0.0
route add 10.0.0.0 mask 255.255.255.0 0.0.0.0
route add 10.0.1.0 mask 255.255.255.0 0.0.0.0
route add 57.0.0.0 mask 255.0.0.0 0.0.0.0
BAT

encoded="$(base64 "$TEST_ROOT/good.bat" | tr -d '\n')"
jq -n --arg name "Self-test" --arg encoded "$encoded" '{name:$name,files:[{name:"good.bat",contentBase64:$encoded}]}' >"$TEST_ROOT/preview.json"

result="$(
    BRORAY_ROOT="$TEST_ROOT/root" \
    BRORAY_ROUTES_ROOT="$TEST_ROOT/root/routes" \
    BRORAY_USER_ROUTES_PREVIEW_TTL=3600 \
    "$ASH_BIN" "$TEST_ROOT/root/bin/broray-routes-user" preview "$TEST_ROOT/preview.json"
)" || fail "предварительная проверка завершилась ошибкой"

printf '%s\n' "$result" | jq -e '
    .sourceRouteLineCount == 4 and
    .canonicalRouteCount == 3 and
    .exportRouteCount == 2 and
    .normalizedNetworkCount == 1 and
    .duplicateCount == 1 and
    .broadRouteCount == 1 and
    .ready == true
' >/dev/null || fail "неверные показатели предварительной проверки"

token="$(printf '%s\n' "$result" | jq -r '.token')"
jq -n --arg token "$token" --arg name "Self-test" '{token:$token,name:$name,bundleId:null}' >"$TEST_ROOT/commit.json"

commit="$(
    BRORAY_ROOT="$TEST_ROOT/root" \
    BRORAY_ROUTES_ROOT="$TEST_ROOT/root/routes" \
    BRORAY_USER_ROUTES_PREVIEW_TTL=3600 \
    "$ASH_BIN" "$TEST_ROOT/root/bin/broray-routes-user" commit "$TEST_ROOT/commit.json"
)" || fail "сохранение набора завершилось ошибкой"

bundle_id="$(printf '%s\n' "$commit" | jq -r '.bundleId')"
[ -n "$bundle_id" ] || fail "не получен идентификатор набора"

jq -e --arg id "$bundle_id" '.bundles | any(.id == $id)' \
    "$TEST_ROOT/root/routes/custom.json" >/dev/null || fail "набор не добавлен в пользовательский реестр"
jq -e --arg id "$bundle_id" '.bundles | index($id) != null' \
    "$TEST_ROOT/root/routes/bundles.json" >/dev/null || fail "набор не добавлен в общий реестр"
jq -e '.bundles[0:9] == ["telegram","whatsapp","youtube","chatgpt","facebook","instagram","meta","tiktok","speedtest"]' \
    "$TEST_ROOT/root/routes/bundles.json" >/dev/null || fail "порядок встроенных карточек был изменён"
jq -e '.routeCount == 2 and (.routes | length) == 2' \
    "$TEST_ROOT/root/routes/catalog/$bundle_id/routes.json" >/dev/null || fail "экспортный каталог некорректен"
[ "$(wc -l <"$TEST_ROOT/root/routes/catalog/$bundle_id/canonical.txt" | tr -d ' ')" = "3" ] || fail "канонический список не сохранён"
[ "$(wc -l <"$TEST_ROOT/root/routes/catalog/$bundle_id/normalized.txt" | tr -d ' ')" = "2" ] || fail "экспортный normalized.txt некорректен"
expected_export_sha="$(jq -r '.contentSha256' "$TEST_ROOT/root/routes/catalog/$bundle_id/routes.json")"
actual_export_sha="$(sha256sum "$TEST_ROOT/root/routes/catalog/$bundle_id/normalized.txt" | awk '{print $1}')"
[ "$actual_export_sha" = "$expected_export_sha" ] || fail "normalized.txt не совпадает с контрактом действующего экспортера"

BRORAY_ROOT="$TEST_ROOT/root" \
BRORAY_ROUTES_ROOT="$TEST_ROOT/root/routes" \
"$ASH_BIN" "$TEST_ROOT/root/bin/broray-routes-user" validate "$bundle_id" >/dev/null || fail "повторная проверка не прошла"

if BRORAY_ROOT="$TEST_ROOT/root" \
   BRORAY_ROUTES_ROOT="$TEST_ROOT/root/routes" \
   "$ASH_BIN" "$TEST_ROOT/root/bin/broray-routes-user" validate 'user-aa../../escape' \
   >"$TEST_ROOT/invalid-id.out" 2>"$TEST_ROOT/invalid-id.err"
then
    fail "идентификатор с выходом из каталога был принят"
fi
grep -q '^BRORAY_ERROR:BUNDLE_INVALID:' "$TEST_ROOT/invalid-id.err" || \
    fail "опасный идентификатор не отклонён до обращения к файлам"

source_file="$TEST_ROOT/root/routes/catalog/$bundle_id/source/01.bat"
cp "$source_file" "$TEST_ROOT/source.backup" || fail "не удалось подготовить проверку целостности исходника"
printf '%s\n' 'rem tampered' >>"$source_file"
if BRORAY_ROOT="$TEST_ROOT/root" \
   BRORAY_ROUTES_ROOT="$TEST_ROOT/root/routes" \
   "$ASH_BIN" "$TEST_ROOT/root/bin/broray-routes-user" validate "$bundle_id" \
   >"$TEST_ROOT/tamper.out" 2>"$TEST_ROOT/tamper.err"
then
    fail "изменённый исходный BAT-файл прошёл проверку"
fi
grep -q '^BRORAY_ERROR:BUNDLE_DAMAGED:' "$TEST_ROOT/tamper.err" || \
    fail "изменённый исходник не получил ошибку целостности"
mv "$TEST_ROOT/source.backup" "$source_file" || fail "не удалось восстановить исходник self-test"
BRORAY_ROOT="$TEST_ROOT/root" \
BRORAY_ROUTES_ROOT="$TEST_ROOT/root/routes" \
"$ASH_BIN" "$TEST_ROOT/root/bin/broray-routes-user" validate "$bundle_id" >/dev/null || \
    fail "проверка после восстановления исходника не прошла"

cat >"$TEST_ROOT/two-halves.bat" <<'BAT'
route add 0.0.0.0 mask 128.0.0.0 0.0.0.0
route add 128.0.0.0 mask 128.0.0.0 0.0.0.0
BAT
encoded="$(base64 "$TEST_ROOT/two-halves.bat" | tr -d '\n')"
jq -n --arg name "Two halves" --arg encoded "$encoded" \
    '{name:$name,files:[{name:"two-halves.bat",contentBase64:$encoded}]}' \
    >"$TEST_ROOT/two-halves.json"
halves="$(
    BRORAY_ROOT="$TEST_ROOT/root" \
    BRORAY_ROUTES_ROOT="$TEST_ROOT/root/routes" \
    "$ASH_BIN" "$TEST_ROOT/root/bin/broray-routes-user" preview "$TEST_ROOT/two-halves.json"
)" || fail "проверка двух /1 завершилась ошибкой"
printf '%s\n' "$halves" | jq -e '.canonicalRouteCount == 2 and .exportRouteCount == 2' \
    >/dev/null || fail "оптимизатор создал запрещённый маршрут /0"

cat >"$TEST_ROOT/dangerous.bat" <<'BAT'
route add 10.0.0.0 mask 255.255.255.0 0.0.0.0
powershell -Command "Remove-Item C:\\*"
BAT
encoded="$(base64 "$TEST_ROOT/dangerous.bat" | tr -d '\n')"
jq -n --arg name "Dangerous" --arg encoded "$encoded" '{name:$name,files:[{name:"dangerous.bat",contentBase64:$encoded}]}' >"$TEST_ROOT/dangerous.json"

if BRORAY_ROOT="$TEST_ROOT/root" \
   BRORAY_ROUTES_ROOT="$TEST_ROOT/root/routes" \
   "$ASH_BIN" "$TEST_ROOT/root/bin/broray-routes-user" preview "$TEST_ROOT/dangerous.json" >/dev/null 2>&1
then
    fail "опасная команда была принята"
fi

cat >"$TEST_ROOT/default.bat" <<'BAT'
route add 0.0.0.0 mask 0.0.0.0 0.0.0.0
BAT
encoded="$(base64 "$TEST_ROOT/default.bat" | tr -d '\n')"
jq -n --arg name "Default" --arg encoded "$encoded" '{name:$name,files:[{name:"default.bat",contentBase64:$encoded}]}' >"$TEST_ROOT/default.json"

if BRORAY_ROOT="$TEST_ROOT/root" \
   BRORAY_ROUTES_ROOT="$TEST_ROOT/root/routes" \
   "$ASH_BIN" "$TEST_ROOT/root/bin/broray-routes-user" preview "$TEST_ROOT/default.json" >/dev/null 2>&1
then
    fail "маршрут по умолчанию был принят"
fi

state_file="$TEST_ROOT/root/routes/state/$bundle_id.json"
registry_file="$TEST_ROOT/root/routes/installed/bundles/$bundle_id.json"
jq '.installedVersion = .downloadedVersion' "$state_file" >"$state_file.new" && \
    mv "$state_file.new" "$state_file" || fail "не удалось подготовить installed state"
jq --slurpfile state "$state_file" '
    .installedVersion = $state[0].installedVersion |
    .routeKeys = ["ipv4|10.0.0.0/23|Proxy0|test"] |
    .managedRouteKeys = .routeKeys
' "$registry_file" >"$registry_file.new" && \
    mv "$registry_file.new" "$registry_file" || fail "не удалось подготовить installed registry"
cat >"$TEST_ROOT/root/bin/broray-routes" <<'STUB'
#!/bin/sh
printf '%s\n' "$*" >"$BRORAY_TEST_DELETE_LOG"
exit 0
STUB
chmod 755 "$TEST_ROOT/root/bin/broray-routes"
BRORAY_TEST_DELETE_LOG="$TEST_ROOT/delete.log" \
BRORAY_ROOT="$TEST_ROOT/root" \
BRORAY_ROUTES_ROOT="$TEST_ROOT/root/routes" \
"$ASH_BIN" "$TEST_ROOT/root/bin/broray-routes-user" remove "$bundle_id" >/dev/null || fail "удаление набора не прошло"
grep -qx "delete $bundle_id" "$TEST_ROOT/delete.log" || fail "установленный набор не прошёл через безопасный основной удалитель"

jq -e --arg id "$bundle_id" '.bundles | all(.id != $id)' \
    "$TEST_ROOT/root/routes/custom.json" >/dev/null || fail "набор остался в реестре"

echo "BROray user routes self-test: PASS"
