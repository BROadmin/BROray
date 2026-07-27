#!/opt/bin/ash

# BROray route runtime initializer and integrity repair.
# Creates only missing built-in runtime files, preserves all valid user data,
# and reconstructs the global ownership registry only from verified per-bundle
# registries and export plans.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_ROUTES_RUNTIME_SYNC_LIBRARY="${BRORAY_ROUTES_RUNTIME_SYNC_LIBRARY:-$BRORAY_ROOT/lib/routes-router-sync.sh}"

broray_routes_runtime_error()
{
    printf 'ОШИБКА: %s\n' "$*" >&2
    return 1
}

broray_routes_runtime_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_runtime_valid_id()
{
    case "${1:-}" in
        ''|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
            return 1
            ;;
    esac
    return 0
}

broray_routes_runtime_prepare()
{
    local routes share bundles config interface now work stage id manifest state registry
    local custom custom_ids custom_id custom_manifest custom_state custom_registry custom_catalog

    routes="$BRORAY_ROUTES_ROOT"
    share="$BRORAY_ROOT/share/routes/manifests"
    bundles="$routes/bundles.json"
    config="$routes/config.json"
    custom="$routes/custom.json"
    now="$(broray_routes_runtime_now)"
    work="$BRORAY_ROOT/tmp/routes-runtime-repair-$$"
    stage="$work/stage"

    command -v jq >/dev/null 2>&1 ||
        broray_routes_runtime_error "Команда jq недоступна." || return 1

    [ -r "$config" ] ||
        broray_routes_runtime_error "Конфигурация маршрутов недоступна." || return 1
    [ -r "$bundles" ] ||
        broray_routes_runtime_error "Реестр наборов маршрутов недоступен." || return 1
    [ -d "$share" ] ||
        broray_routes_runtime_error "Встроенные манифесты маршрутов недоступны." || return 1

    interface="$(jq -r '.managedInterface // empty' "$config" 2>/dev/null)"
    case "$interface" in
        Proxy[0-9]*) ;;
        *) broray_routes_runtime_error "Некорректный управляемый интерфейс ProxyN."; return 1 ;;
    esac
    case "${interface#Proxy}" in
        ''|*[!0-9]*) broray_routes_runtime_error "Некорректный управляемый интерфейс ProxyN."; return 1 ;;
    esac

    jq -e '
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (([.bundles[]] | length) == ([.bundles[]] | unique | length))
    ' "$bundles" >/dev/null 2>&1 || {
        broray_routes_runtime_error "Реестр наборов маршрутов повреждён."
        return 1
    }

    rm -rf "$work" 2>/dev/null || true
    mkdir -p \
        "$stage/manifests" \
        "$stage/state" \
        "$stage/installed/bundles" \
        "$routes/catalog" \
        "$routes/manifests" \
        "$routes/state" \
        "$routes/installed/bundles" \
        "$routes/locks" \
        "$routes/tmp" \
        "$routes/transactions" \
        "$BRORAY_ROOT/tmp" || {
        rm -rf "$work"
        broray_routes_runtime_error "Не удалось подготовить каталоги маршрутов."
        return 1
    }

    jq '
        [
            "telegram", "whatsapp", "youtube", "chatgpt", "facebook",
            "instagram", "meta", "tiktok", "speedtest"
        ] as $built_in |
        .schemaVersion = 1 |
        .bundles = (
            $built_in + [
                (.bundles // [])[] as $id |
                select(($built_in | index($id)) == null) |
                $id
            ]
        )
    ' "$bundles" >"$stage/bundles.json" || {
        rm -rf "$work"
        broray_routes_runtime_error "Не удалось подготовить реестр наборов."
        return 1
    }

    for id in telegram whatsapp youtube chatgpt facebook instagram meta tiktok speedtest
    do
        manifest="$share/$id.json"
        state="$routes/state/$id.json"
        registry="$routes/installed/bundles/$id.json"

        jq -e \
            --arg id "$id" \
            --arg interface "$interface" '
            (.schemaVersion == 1) and
            (.id == $id) and
            (.source.provider == "github") and
            (.targetInterface == $interface) and
            (.exportComment == "BROray")
        ' "$manifest" >/dev/null 2>&1 || {
            rm -rf "$work"
            broray_routes_runtime_error "Повреждён встроенный манифест: $id"
            return 1
        }

        cp -p "$manifest" "$stage/manifests/$id.json" || {
            rm -rf "$work"
            return 1
        }

        if [ -r "$state" ]; then
            jq -e --arg id "$id" '
                (.schemaVersion == 1) and
                (.bundleId == $id) and
                ((.status | type) == "string") and
                ((.availableVersion == null) or ((.availableVersion | type) == "object")) and
                ((.downloadedVersion == null) or ((.downloadedVersion | type) == "object")) and
                ((.installedVersion == null) or ((.installedVersion | type) == "object"))
            ' "$state" >/dev/null 2>&1 || {
                rm -rf "$work"
                broray_routes_runtime_error "Повреждено состояние набора: $id"
                return 1
            }
            cp -p "$state" "$stage/state/$id.json" || {
                rm -rf "$work"
                return 1
            }
        else
            jq -n --arg id "$id" --arg now "$now" '{
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
            }' >"$stage/state/$id.json" || {
                rm -rf "$work"
                return 1
            }
        fi

        if [ -r "$registry" ]; then
            jq -e --arg id "$id" '
                (.schemaVersion == 1) and
                (.bundleId == $id) and
                ((.installedVersion == null) or ((.installedVersion | type) == "object")) and
                ((.routeKeys | type) == "array") and
                ((.managedRouteKeys | type) == "array") and
                ((.externalRouteKeys | type) == "array")
            ' "$registry" >/dev/null 2>&1 || {
                rm -rf "$work"
                broray_routes_runtime_error "Повреждён реестр установки набора: $id"
                return 1
            }
            if jq -e --arg interface "$interface" '
                (.targetInterface == $interface) and
                ((.managedMetric // 1200) == 1200)
            ' "$registry" >/dev/null 2>&1
            then
                cp -p "$registry" "$stage/installed/bundles/$id.json" || {
                    rm -rf "$work"
                    return 1
                }
            else
                jq --arg interface "$interface" --arg now "$now" '
                    .targetInterface = $interface |
                    .managedMetric = 1200 |
                    .updatedAt = $now
                ' "$registry" >"$stage/installed/bundles/$id.json" || {
                    rm -rf "$work"
                    return 1
                }
            fi
        else
            jq -n --arg id "$id" --arg interface "$interface" --arg now "$now" '{
                schemaVersion: 1,
                bundleId: $id,
                installedVersion: null,
                routeKeys: [],
                managedRouteKeys: [],
                externalRouteKeys: [],
                targetInterface: $interface,
                managedMetric: 1200,
                installedAt: null,
                removedAt: null,
                updatedAt: $now
            }' >"$stage/installed/bundles/$id.json" || {
                rm -rf "$work"
                return 1
            }
        fi
    done

    if [ -r "$custom" ]; then
        jq -e '
            (.schemaVersion == 1) and
            ((.bundles | type) == "array") and
            (all(.bundles[];
                ((.id | type) == "string") and
                ((.name | type) == "string")
            )) and
            (([.bundles[].id] | length) == ([.bundles[].id] | unique | length))
        ' "$custom" >/dev/null 2>&1 || {
            rm -rf "$work"
            broray_routes_runtime_error "Реестр пользовательских наборов повреждён."
            return 1
        }

        custom_ids="$(jq -r '.bundles[].id' "$custom")"
        for custom_id in $custom_ids
        do
            broray_routes_runtime_valid_id "$custom_id" || {
                rm -rf "$work"
                broray_routes_runtime_error "Некорректный идентификатор пользовательского набора."
                return 1
            }
            case "$custom_id" in
                user-*) ;;
                *)
                    rm -rf "$work"
                    broray_routes_runtime_error "Некорректный идентификатор пользовательского набора: $custom_id"
                    return 1
                    ;;
            esac

            custom_manifest="$routes/manifests/$custom_id.json"
            custom_state="$routes/state/$custom_id.json"
            custom_registry="$routes/installed/bundles/$custom_id.json"
            custom_catalog="$routes/catalog/$custom_id"

            [ -r "$custom_manifest" ] &&
            [ -r "$custom_state" ] &&
            [ -r "$custom_registry" ] &&
            [ -d "$custom_catalog" ] || {
                rm -rf "$work"
                broray_routes_runtime_error "Файлы пользовательского набора отсутствуют: $custom_id"
                return 1
            }

            jq -e --arg id "$custom_id" --arg interface "$interface" '
                (.schemaVersion == 1) and
                (.id == $id) and
                (.source.provider == "local-upload") and
                (.targetInterface == $interface)
            ' "$custom_manifest" >/dev/null 2>&1 || {
                rm -rf "$work"
                broray_routes_runtime_error "Повреждён манифест пользовательского набора: $custom_id"
                return 1
            }
            jq -e --arg id "$custom_id" '.schemaVersion == 1 and .bundleId == $id' "$custom_state" >/dev/null 2>&1 || {
                rm -rf "$work"
                broray_routes_runtime_error "Повреждено состояние пользовательского набора: $custom_id"
                return 1
            }
            jq -e --arg id "$custom_id" '
                .schemaVersion == 1 and
                .bundleId == $id and
                ((.routeKeys | type) == "array") and
                ((.managedRouteKeys | type) == "array") and
                ((.externalRouteKeys | type) == "array")
            ' "$custom_registry" >/dev/null 2>&1 || {
                rm -rf "$work"
                broray_routes_runtime_error "Повреждён реестр пользовательского набора: $custom_id"
                return 1
            }
        done

        jq --slurpfile custom "$custom" '
            .bundles = (
                .bundles + [
                    $custom[0].bundles[].id as $id |
                    select((.bundles | index($id)) == null) |
                    $id
                ]
            )
        ' "$stage/bundles.json" >"$stage/bundles-with-custom.json" || {
            rm -rf "$work"
            return 1
        }
        mv "$stage/bundles-with-custom.json" "$stage/bundles.json" || {
            rm -rf "$work"
            return 1
        }
    fi

    jq -e '
        (.schemaVersion == 1) and
        (.bundles[0:9] == [
            "telegram", "whatsapp", "youtube", "chatgpt", "facebook",
            "instagram", "meta", "tiktok", "speedtest"
        ]) and
        (([.bundles[]] | length) == ([.bundles[]] | unique | length))
    ' "$stage/bundles.json" >/dev/null 2>&1 || {
        rm -rf "$work"
        broray_routes_runtime_error "Подготовленный реестр наборов не прошёл проверку."
        return 1
    }

    cp -p "$stage/bundles.json" "$bundles.new.$$" &&
    mv "$bundles.new.$$" "$bundles" || {
        rm -f "$bundles.new.$$" 2>/dev/null || true
        rm -rf "$work"
        broray_routes_runtime_error "Не удалось сохранить реестр наборов."
        return 1
    }

    for id in telegram whatsapp youtube chatgpt facebook instagram meta tiktok speedtest
    do
        cp -p "$stage/manifests/$id.json" "$routes/manifests/$id.json.new.$$" &&
        mv "$routes/manifests/$id.json.new.$$" "$routes/manifests/$id.json" &&
        cp -p "$stage/state/$id.json" "$routes/state/$id.json.new.$$" &&
        mv "$routes/state/$id.json.new.$$" "$routes/state/$id.json" &&
        cp -p "$stage/installed/bundles/$id.json" "$routes/installed/bundles/$id.json.new.$$" &&
        mv "$routes/installed/bundles/$id.json.new.$$" "$routes/installed/bundles/$id.json" || {
            rm -rf "$work"
            broray_routes_runtime_error "Не удалось установить runtime-файлы набора: $id"
            return 1
        }
        chmod 644 \
            "$routes/manifests/$id.json" \
            "$routes/state/$id.json" \
            "$routes/installed/bundles/$id.json" 2>/dev/null || true
    done

    [ -r "$BRORAY_ROUTES_RUNTIME_SYNC_LIBRARY" ] || {
        rm -rf "$work"
        broray_routes_runtime_error "Модуль общего реестра маршрутов недоступен."
        return 1
    }

    . "$BRORAY_ROUTES_RUNTIME_SYNC_LIBRARY"
    broray_routes_sync_ensure_global_registry || {
        rm -rf "$work"
        broray_routes_runtime_error "Не удалось восстановить или проверить общий реестр маршрутов."
        return 1
    }

    rm -rf "$work"
    return 0
}
