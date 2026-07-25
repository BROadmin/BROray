#!/opt/bin/ash

broray_routes_registry_error()
{
    printf 'ОШИБКА: %s\n' "$*" >&2
    return 1
}

broray_routes_registry_restore()
{
    registry_routes="$1"
    registry_backup="$2"
    registry_targets="$registry_backup/targets.txt"
    registry_originals="$registry_backup/originals.txt"

    [ -r "$registry_targets" ] || return 0

    while IFS= read -r registry_relative
    do
        [ -n "$registry_relative" ] || continue
        registry_target="$registry_routes/$registry_relative"

        if grep -Fqx "$registry_relative" "$registry_originals"; then
            cp -p \
                "$registry_backup/files/$registry_relative" \
                "$registry_target" 2>/dev/null || true
        else
            rm -f "$registry_target" 2>/dev/null || true
        fi
    done <"$registry_targets"
}

broray_routes_registry_migrate()
{
    registry_root="${BRORAY_ROOT:-/opt/broray}"
    registry_routes="$registry_root/routes"
    registry_share="$registry_root/share/routes/manifests"
    registry_work="$registry_root/tmp/routes-registry.$$"
    registry_stage="$registry_work/stage"
    registry_backup="$registry_work/backup"
    registry_now="$(date '+%Y-%m-%dT%H:%M:%S%z')"
    registry_ids="whatsapp youtube chatgpt facebook instagram meta tiktok speedtest"
    registry_interface="$(jq -r '.managedInterface // empty' "$registry_routes/config.json" 2>/dev/null)"

    case "$registry_interface" in
        Proxy[0-9]*) ;;
        *) broray_routes_registry_error "Некорректный управляемый интерфейс ProxyN."; return 1 ;;
    esac
    case "${registry_interface#Proxy}" in
        ''|*[!0-9]*) broray_routes_registry_error "Некорректный управляемый интерфейс ProxyN."; return 1 ;;
    esac

    command -v jq >/dev/null 2>&1 ||
        broray_routes_registry_error "Команда jq недоступна." ||
        return 1

    [ -r "$registry_routes/bundles.json" ] ||
        broray_routes_registry_error \
            "Не найден существующий реестр наборов маршрутов." ||
        return 1

    jq -e '
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (.bundles | index("telegram") != null)
    ' "$registry_routes/bundles.json" >/dev/null 2>&1 ||
        broray_routes_registry_error \
            "Существующий реестр наборов маршрутов повреждён." ||
        return 1

    rm -rf "$registry_work" 2>/dev/null || true
    mkdir -p \
        "$registry_stage/manifests" \
        "$registry_stage/state" \
        "$registry_stage/installed/bundles" \
        "$registry_backup/files" ||
        broray_routes_registry_error \
            "Не удалось подготовить обновление реестра маршрутов." ||
        return 1

    jq '
        [
            "telegram",
            "whatsapp",
            "youtube",
            "chatgpt",
            "facebook",
            "instagram",
            "meta",
            "tiktok",
            "speedtest"
        ] as $built_in |
        .schemaVersion = 1 |
        .bundles = (
            $built_in +
            [
                (.bundles // [])[] |
                . as $id |
                select(($built_in | index($id)) == null)
            ]
        )
    ' "$registry_routes/bundles.json" \
        >"$registry_stage/bundles.json" || {
        rm -rf "$registry_work"
        return 1
    }

    for registry_id in $registry_ids
    do
        registry_manifest="$registry_share/$registry_id.json"
        registry_state="$registry_routes/state/$registry_id.json"
        registry_bundle="$registry_routes/installed/bundles/$registry_id.json"

        jq -e \
            --arg id "$registry_id" \
            --arg target_interface "$registry_interface" '
            (.schemaVersion == 1) and
            (.id == $id) and
            (.source.provider == "github") and
            (.targetInterface == $target_interface) and
            (.exportComment == "BROray")
        ' "$registry_manifest" >/dev/null 2>&1 || {
            rm -rf "$registry_work"
            broray_routes_registry_error \
                "Повреждён встроенный манифест: $registry_id"
            return 1
        }

        cp -p \
            "$registry_manifest" \
            "$registry_stage/manifests/$registry_id.json" || {
            rm -rf "$registry_work"
            return 1
        }

        if [ -f "$registry_state" ]; then
            jq -e \
                --arg id "$registry_id" '
                (.schemaVersion == 1) and
                (.bundleId == $id)
            ' "$registry_state" >/dev/null 2>&1 || {
                rm -rf "$registry_work"
                broray_routes_registry_error \
                    "Повреждено состояние набора: $registry_id"
                return 1
            }
            cp -p "$registry_state" \
                "$registry_stage/state/$registry_id.json" || {
                rm -rf "$registry_work"
                return 1
            }
        else
            jq -n \
                --arg id "$registry_id" \
                --arg now "$registry_now" \
                --arg target_interface "$registry_interface" '
                {
                    schemaVersion: 1,
                    bundleId: $id,
                    status: "not_checked",
                    availableVersion: null,
                    downloadedVersion: null,
                    installedVersion: null,
                    routeCount: null,
                    lastCheckedAt: null,
                    lastDownloadedAt: null,
                    lastExportedAt: null,
                    lastDeletedAt: null,
                    lastError: null,
                    checkResult: null,
                    downloadResult: null,
                    exportBuild: null,
                    preflight: null,
                    exportResult: null,
                    deleteResult: null,
                    updatedAt: $now
                }
            ' >"$registry_stage/state/$registry_id.json" || {
                rm -rf "$registry_work"
                return 1
            }
        fi

        if [ -f "$registry_bundle" ]; then
            jq -e \
                --arg id "$registry_id" '
                (.schemaVersion == 1) and
                (.bundleId == $id) and
                ((.routeKeys | type) == "array") and
                ((.managedRouteKeys | type) == "array") and
                ((.externalRouteKeys | type) == "array")
            ' "$registry_bundle" >/dev/null 2>&1 || {
                rm -rf "$registry_work"
                broray_routes_registry_error \
                    "Повреждён реестр установки набора: $registry_id"
                return 1
            }
            jq \
                --arg target_interface "$registry_interface" \
                --arg now "$registry_now" '
                .targetInterface = $target_interface |
                .managedMetric = 1200 |
                .updatedAt = $now
            ' "$registry_bundle" \
                >"$registry_stage/installed/bundles/$registry_id.json" || {
                rm -rf "$registry_work"
                return 1
            }
        else
            jq -n \
                --arg id "$registry_id" \
                --arg target_interface "$registry_interface" \
                --arg now "$registry_now" '
                {
                    schemaVersion: 1,
                    bundleId: $id,
                    installedVersion: null,
                    routeKeys: [],
                    managedRouteKeys: [],
                    externalRouteKeys: [],
                    targetInterface: $target_interface,
                    managedMetric: 1200,
                    installedAt: null,
                    removedAt: null,
                    updatedAt: $now
                }
            ' >"$registry_stage/installed/bundles/$registry_id.json" || {
                rm -rf "$registry_work"
                return 1
            }
        fi
    done

    jq -e '
        (.schemaVersion == 1) and
        (.bundles[0:9] == [
            "telegram",
            "whatsapp",
            "youtube",
            "chatgpt",
            "facebook",
            "instagram",
            "meta",
            "tiktok",
            "speedtest"
        ])
    ' "$registry_stage/bundles.json" >/dev/null 2>&1 || {
        rm -rf "$registry_work"
        return 1
    }

    {
        printf '%s\n' "bundles.json"
        for registry_id in $registry_ids
        do
            printf '%s\n' \
                "manifests/$registry_id.json" \
                "state/$registry_id.json" \
                "installed/bundles/$registry_id.json"
        done
    } >"$registry_backup/targets.txt" || {
        rm -rf "$registry_work"
        return 1
    }

    : >"$registry_backup/originals.txt"

    while IFS= read -r registry_relative
    do
        registry_target="$registry_routes/$registry_relative"

        if [ -f "$registry_target" ]; then
            mkdir -p \
                "$registry_backup/files/$(dirname "$registry_relative")" ||
                {
                    rm -rf "$registry_work"
                    return 1
                }
            cp -p "$registry_target" \
                "$registry_backup/files/$registry_relative" || {
                rm -rf "$registry_work"
                return 1
            }
            printf '%s\n' "$registry_relative" \
                >>"$registry_backup/originals.txt"
        fi
    done <"$registry_backup/targets.txt"

    while IFS= read -r registry_relative
    do
        registry_target="$registry_routes/$registry_relative"
        registry_new="$registry_target.new.$$"

        mkdir -p "$(dirname "$registry_target")" &&
        cp -p "$registry_stage/$registry_relative" "$registry_new" &&
        mv -f "$registry_new" "$registry_target" || {
            rm -f "$registry_new" 2>/dev/null || true
            broray_routes_registry_restore \
                "$registry_routes" "$registry_backup"
            rm -rf "$registry_work"
            broray_routes_registry_error \
                "Не удалось применить реестр наборов маршрутов."
            return 1
        }
    done <"$registry_backup/targets.txt"

    rm -rf "$registry_work"
    return 0
}
