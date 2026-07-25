#!/bin/sh

set -eu

BASE_ROOT="${BRORAY_ROOT:-/opt/broray}"
LIBRARY="$BASE_ROOT/lib/routes-router-export.sh"
WORK="$BASE_ROOT/routes/tmp/routes-added-renewed-selftest-$$"
ORIGINAL_PATH="$PATH"

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

[ -r "$LIBRARY" ] ||
    fail "Не найден модуль экспорта: $LIBRARY"

mkdir -p "$WORK/mock-bin" ||
    fail "Не удалось создать каталог self-test"

cat >"$WORK/mock-bin/ndmc" <<'MOCK_NDMC'
#!/bin/sh

set -eu

command_text=""

if [ "${1:-}" = "-c" ]; then
    command_text="${2:-}"
else
    exit 2
fi

printf '%s\n' "$command_text" >>"$BRORAY_TEST_LOG"

case "$command_text" in
    "show interface Proxy0")
        cat <<'OUTPUT'
               id: Proxy0
             link: up
        connected: yes
            state: up
OUTPUT
        ;;

    "show ip route")
        cat <<'OUTPUT'
================================================================================
Destination         Gateway          Interface
     F  Metric
================================================================================
198.51.100.0/24     0.0.0.0          Wireguard0
     U  1000
OUTPUT
        ;;

    "system configuration save")
        echo "Core::ConfigurationSaver: Saving configuration..."
        : >"$BRORAY_TEST_SAVED"
        ;;

    "ip route 198.51.100.0 255.255.255.0 Proxy0 1200")
        key="198.51.100.0|255.255.255.0|Proxy0|1200"

        if grep -Fqx "$key" "$BRORAY_TEST_ROUTER_STATE" 2>/dev/null; then
            if [ "$BRORAY_TEST_MODE" = "confirm-fail" ]; then
                echo "Network::RoutingTable: Static route response is unavailable."
            else
                echo "Network::RoutingTable: Renewed static route: 198.51.100.0/24 via Proxy0."
            fi
        else
            printf '%s\n' "$key" >>"$BRORAY_TEST_ROUTER_STATE"
            echo "Network::RoutingTable: Added static route: 198.51.100.0/24 via Proxy0."
        fi
        ;;

    "no ip route 198.51.100.0 255.255.255.0 Proxy0" | \
    "no ip route 198.51.100.0 255.255.255.0 Proxy0 1200")
        key="198.51.100.0|255.255.255.0|Proxy0|1200"
        new="$BRORAY_TEST_ROUTER_STATE.new.$$"

        grep -Fvx "$key" "$BRORAY_TEST_ROUTER_STATE" \
            >"$new" 2>/dev/null || true

        mv "$new" "$BRORAY_TEST_ROUTER_STATE"
        echo "Network::RoutingTable: Deleted static route: 198.51.100.0/24 via Proxy0."
        ;;

    *)
        echo "Неизвестная mock-команда: $command_text" >&2
        exit 2
        ;;
esac
MOCK_NDMC

cat >"$WORK/mock-bin/curl" <<'MOCK_CURL'
#!/bin/sh
cat "$BRORAY_TEST_RCI"
MOCK_CURL

cat >"$WORK/mock-bin/wget" <<'MOCK_WGET'
#!/bin/sh
exit 1
MOCK_WGET

chmod 755 \
    "$WORK/mock-bin/ndmc" \
    "$WORK/mock-bin/curl" \
    "$WORK/mock-bin/wget"

prepare_case()
{
    local name mode root route_file_sha

    name="$1"
    mode="$2"
    root="$WORK/$name/root"

    mkdir -p \
        "$root/lib" \
        "$root/routes/catalog/test" \
        "$root/routes/installed/bundles" \
        "$root/routes/state" \
        "$root/routes/transactions" \
        "$root/routes/locks" \
        "$root/routes/tmp"

    cat >"$root/lib/interface-owner.sh" <<'OWNER_LIBRARY'
#!/bin/sh

broray_interface_owner_record_valid()
{
    [ "${1:-}" = "Proxy0" ]
}

broray_interface_owner_valid()
{
    [ "${1:-}" = "Proxy0" ]
}
OWNER_LIBRARY

    cat >"$root/lib/routes-router-config.sh" <<'ROUTES_CONFIG_LIBRARY'
#!/bin/sh

broray_routes_config_fetch()
{
    jq -Rn '
        [
            inputs |
            split("|") |
            select(length == 4) |
            {
                network: .[0],
                mask: .[1],
                prefix: 24,
                destination: (.[0] + "/24"),
                interface: .[2],
                gateway: "0.0.0.0",
                metric: (.[3] | tonumber),
                proto: "static",
                automatic: false,
                exclusive: false
            }
        ] |
        {
            schemaVersion: 1,
            source: "running-config",
            routes: .
        }
    ' <"$BRORAY_TEST_ROUTER_STATE" >"$1"
}
ROUTES_CONFIG_LIBRARY

    : >"$WORK/$name/router-state.tsv"
    : >"$WORK/$name/ndmc.log"

    if [ "$mode" = "preexisting" ]; then
        printf '%s\n' \
            '198.51.100.0|255.255.255.0|Proxy0|1200' \
            >"$WORK/$name/router-state.tsv"
    fi

    cat >"$WORK/$name/rci.json" <<'JSON'
{
  "route": [
    {
      "destination": "198.51.100.0/24",
      "gateway": "0.0.0.0",
      "interface": "Wireguard0",
      "metric": 1000,
      "proto": "static"
    }
  ]
}
JSON

    cat >"$root/routes/bundles.json" <<'JSON'
{"schemaVersion":1,"bundles":["test"]}
JSON

    cat >"$root/routes/config.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routeComment": "BROray",
  "ownershipPolicy": {
    "adoptExistingRoutes": false,
    "modifyExternalRoutes": false,
    "deleteExternalRoutes": false,
    "touchOtherInterfaces": false,
    "deleteOnlyExactManagedMatch": true
  }
}
JSON

    printf '%s\n' \
        'route add 198.51.100.0 mask 255.255.255.0 0.0.0.0 metric 1200 :: rem BROray' \
        >"$root/routes/catalog/test/keenetic-routes.bat"

    route_file_sha="$(
        sha256sum "$root/routes/catalog/test/keenetic-routes.bat" |
            awk '{print $1}'
    )"

    cat >"$root/routes/catalog/test/export-plan.json" <<JSON
{
  "schemaVersion": 1,
  "bundleId": "test",
  "sourceCommit": "1111111111111111111111111111111111111111",
  "sourceDate": "2026-01-01T00:00:00Z",
  "contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "targetInterface": "Proxy0",
  "managedMetric": 1200,
  "routeComment": "BROray",
  "routeFileSha256": "$route_file_sha",
  "routeCount": 1,
  "routerApplied": false,
  "routes": [
    {
      "key": "ipv4|198.51.100.0/24|Proxy0|gateway:none|metric:1200|automatic:unspecified|exclusive:unspecified",
      "family": "ipv4",
      "network": "198.51.100.0",
      "prefix": 24,
      "mask": "255.255.255.0",
      "targetInterface": "Proxy0",
      "gatewayToken": "0.0.0.0",
      "metric": 1200,
      "automatic": null,
      "exclusive": null,
      "comment": "BROray"
    }
  ]
}
JSON

    cat >"$root/routes/catalog/test/router-preflight.json" <<'JSON'
{
  "schemaVersion": 2,
  "bundleId": "test",
  "canExport": true,
  "routerChanged": false,
  "configurationSaved": false
}
JSON

    cat >"$root/routes/installed/routes.json" <<'JSON'
{
  "schemaVersion": 1,
  "managedInterface": "Proxy0",
  "managedMetric": 1200,
  "routes": [],
  "updatedAt": null
}
JSON

    cat >"$root/routes/installed/bundles/test.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "test",
  "installedVersion": null,
  "routeKeys": [],
  "managedRouteKeys": [],
  "externalRouteKeys": [],
  "installedAt": null,
  "updatedAt": null
}
JSON

    cat >"$root/routes/state/test.json" <<'JSON'
{
  "schemaVersion": 1,
  "bundleId": "test",
  "status": "downloaded",
  "availableVersion": {
    "sourceCommit": "1111111111111111111111111111111111111111",
    "sourceDate": "2026-01-01T00:00:00Z",
    "contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "downloadedVersion": {
    "sourceCommit": "1111111111111111111111111111111111111111",
    "sourceDate": "2026-01-01T00:00:00Z",
    "contentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "installedVersion": null,
  "preflight": {
    "result": "ready",
    "routerChanged": false,
    "configurationSaved": false
  },
  "lastError": null
}
JSON
}

run_case()
{
    local name mode expected_success root rc route_lines delete_count save_count

    name="$1"
    mode="$2"
    expected_success="$3"
    root="$WORK/$name/root"

    prepare_case "$name" "$mode"

    BRORAY_ROOT="$root"
    BRORAY_ROUTES_ROOT="$root/routes"
    BRORAY_TEST_MODE="$mode"
    BRORAY_TEST_ROUTER_STATE="$WORK/$name/router-state.tsv"
    BRORAY_TEST_LOG="$WORK/$name/ndmc.log"
    BRORAY_TEST_RCI="$WORK/$name/rci.json"
    BRORAY_TEST_SAVED="$WORK/$name/saved"
    PATH="$WORK/mock-bin:$ORIGINAL_PATH"

    export \
        BRORAY_ROOT \
        BRORAY_ROUTES_ROOT \
        BRORAY_TEST_MODE \
        BRORAY_TEST_ROUTER_STATE \
        BRORAY_TEST_LOG \
        BRORAY_TEST_RCI \
        BRORAY_TEST_SAVED \
        PATH

    . "$LIBRARY"

    rc=0
    (
        broray_routes_router_export_run test
    ) >"$WORK/$name/output.txt" 2>"$WORK/$name/error.txt" ||
        rc=$?

    if [ "$expected_success" = true ]; then
        [ "$rc" = 0 ] || {
            cat "$WORK/$name/error.txt" >&2 || true
            fail "$name: экспорт должен был завершиться успешно"
        }

        grep -Fq \
            'Проверка создания: ответы ndmc + точная сверка show running-config' \
            "$WORK/$name/output.txt" ||
            fail "$name: отсутствует подтверждение точной проверки running-config"

        route_lines="$(
            wc -l <"$WORK/$name/router-state.tsv" |
                tr -d ' '
        )"
        [ "$route_lines" = 1 ] ||
            fail "$name: тестовый маршрут не сохранён"

        [ "$(grep -F -c \
            'ip route 198.51.100.0 255.255.255.0 Proxy0 1200' \
            "$WORK/$name/ndmc.log")" = 2 ] ||
            fail "$name: маршрут не прошёл Added и Renewed"

        [ "$(grep -F -c 'system configuration save' \
            "$WORK/$name/ndmc.log")" = 1 ] ||
            fail "$name: конфигурация сохранена неверное число раз"

        jq -e '
            (.status == "installed") and
            (.exportResult.managedMetric == 1200) and
            (.exportResult.verificationMode == "running-config-exact")
        ' "$root/routes/state/test.json" >/dev/null ||
            fail "$name: состояние экспорта повреждено"

        jq -e '
            (.managedMetric == 1200) and
            (.verificationMode == "running-config-exact") and
            ((.routes | length) == 1) and
            (all(.routes[];
                (.interface == "Proxy0") and
                (.metric == 1200) and
                (.owners == ["test"])
            ))
        ' "$root/routes/installed/routes.json" >/dev/null ||
            fail "$name: глобальный реестр повреждён"

    else
        [ "$rc" -ne 0 ] ||
            fail "$name: экспорт должен был быть остановлен"

        [ ! -f "$WORK/$name/saved" ] ||
            fail "$name: конфигурация не должна сохраняться"

        jq -e '
            (.status == "downloaded") and
            (.installedVersion == null) and
            ((.routes // []) == null)
        ' "$root/routes/state/test.json" >/dev/null 2>&1 || true

        jq -e '(.routes | length) == 0' \
            "$root/routes/installed/routes.json" >/dev/null ||
            fail "$name: реестр изменился после ошибки"

        delete_count="$(
            grep -F -c \
                'no ip route 198.51.100.0 255.255.255.0 Proxy0' \
                "$WORK/$name/ndmc.log" 2>/dev/null ||
                true
        )"

        save_count="$(
            grep -F -c 'system configuration save' \
                "$WORK/$name/ndmc.log" 2>/dev/null ||
                true
        )"

        [ "$save_count" = 0 ] ||
            fail "$name: конфигурация не должна сохраняться"

        case "$mode" in
            confirm-fail)
                [ "$delete_count" = 1 ] ||
                    fail "$name: созданный маршрут не был откатан"
                [ ! -s "$WORK/$name/router-state.tsv" ] ||
                    fail "$name: маршрут остался после отката"
                ;;

            preexisting)
                [ "$delete_count" = 0 ] ||
                    fail "$name: существующий маршрут был затронут"
                [ "$(wc -l <"$WORK/$name/router-state.tsv" | tr -d ' ')" = 1 ] ||
                    fail "$name: существующий маршрут исчез"
                ;;
        esac
    fi

    [ ! -d "$root/routes/locks/operation.lock" ] ||
        fail "$name: блокировка операции осталась"
}

run_case success success true
run_case confirm-fail confirm-fail false
run_case preexisting preexisting false

echo "BROray routes Added/Renewed export self-test: PASS"
