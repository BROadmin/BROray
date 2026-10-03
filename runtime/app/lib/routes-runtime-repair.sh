#!/opt/bin/ash

# BROray route runtime initializer and integrity repair.
# Recovers built-in state from verified ownership, preserving damaged bytes.
# Existing ownership and operation evidence remain authoritative.

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

broray_routes_runtime_apply_staged_metadata()
{
    local target staged metadata

    target="${1:-}"
    staged="${2:-}"
    metadata="${staged}.metadata.$$"

    [ -f "$staged" ] && [ ! -L "$staged" ] || {
        broray_routes_runtime_error "Подготовленный runtime-файл небезопасен: $staged"
        return 1
    }

    if [ -e "$target" ] || [ -L "$target" ]; then
        [ -f "$target" ] && [ ! -L "$target" ] || {
            broray_routes_runtime_error "Существующий runtime-файл небезопасен: $target"
            return 1
        }

        rm -f "$metadata" 2>/dev/null || return 1
        cp -p "$target" "$metadata" || {
            rm -f "$metadata" 2>/dev/null || true
            broray_routes_runtime_error "Не удалось сохранить метаданные runtime-файла: $target"
            return 1
        }
        cat "$staged" >"$metadata" || {
            rm -f "$metadata" 2>/dev/null || true
            broray_routes_runtime_error "Не удалось подготовить содержимое runtime-файла: $target"
            return 1
        }
        mv -f "$metadata" "$staged" || {
            rm -f "$metadata" 2>/dev/null || true
            broray_routes_runtime_error "Не удалось применить метаданные runtime-файла: $target"
            return 1
        }
    else
        chmod 644 "$staged" || {
            broray_routes_runtime_error "Не удалось назначить права нового runtime-файла: $target"
            return 1
        }
    fi

    return 0
}

broray_routes_runtime_safe_path()
{
    local path
    path="$1"
    case "$path" in "$BRORAY_ROOT"/*) ;; *) return 1 ;; esac
    case "$path" in */../*|*/./*|*//*) return 1 ;; esac
    [ ! -L "$path" ] || return 1
    path="${path%/*}"
    while [ "$path" != "$BRORAY_ROOT" ]; do
        [ -d "$path" ] && [ ! -L "$path" ] || return 1
        path="${path%/*}"
    done
    [ -d "$path" ] && [ ! -L "$path" ]
}

broray_routes_runtime_regular()
{
    broray_routes_runtime_safe_path "$1" && [ -f "$1" ] && [ ! -L "$1" ] &&
        [ "$(find "$1" -maxdepth 0 -type f -links 1 -print)" = "$1" ]
}

broray_routes_runtime_state_valid()
{
    jq -es --arg id "$2" '
        length == 1 and (.[0] |
        type == "object" and .schemaVersion == 1 and .bundleId == $id and
        (.status | type == "string") and
        (has("installedVersion") and has("availableVersion") and has("downloadedVersion")) and
        (all(.availableVersion,.downloadedVersion,.installedVersion; . == null or type == "object")) and
        (.routeCount == null or (.routeCount | type == "number" and . >= 0 and floor == .)) and
        (all(.lastCheckedAt,.lastVerifiedAt,.lastDownloadedAt,.lastExportedAt,.lastDeletedAt,.updatedAt;
             . == null or type == "string")) and
        (all(.checkResult,.verifyResult,.downloadResult,.preflight,.exportBuild,.exportResult,.deleteResult;
             . == null or type == "object")) and
        (.lastError == null or (.lastError | type == "string" or type == "object")))
    ' "$1" >/dev/null 2>&1
}

# Catalog files are downloaded input, never ownership authority. No writes here.
broray_routes_runtime_catalog_valid()
{
    local catalog file
    catalog="$BRORAY_ROUTES_ROOT/catalog/$1"
    for file in routes.json version.json source-files.json; do
        broray_routes_runtime_regular "$catalog/$file" || return 1
    done
    jq -nes --arg id "$1" --slurpfile r "$catalog/routes.json" \
        --slurpfile v "$catalog/version.json" --slurpfile f "$catalog/source-files.json" '
        def hash: type == "string" and length == 64 and
          (explode | all(. >= 48 and . <= 57 or . >= 97 and . <= 102));
        ($r|length)==1 and ($v|length)==1 and ($f|length)==1 and
        ($r[0] as $r | $v[0] as $v | $f[0] as $f |
         $r.schemaVersion==1 and $v.schemaVersion==1 and
         $r.bundleId==$id and $v.bundleId==$id and
         ($r.contentSha256|hash) and $r.contentSha256==$v.contentSha256 and
         ($r.targetInterface|type=="string") and $r.targetInterface==$v.targetInterface and
         $r.routeComment=="BROray" and
         ($r.routes|type=="array") and ($r.routeCount|type=="number") and
         $r.routeCount>0 and ($r.routes|length)==$r.routeCount and $r.routeCount==$v.routeCount and
         all($r.routes[]; .family=="ipv4" and (.network|type=="string") and
             (.prefix|type=="number" and floor==. and .>=1 and .<=32)) and
         ($f|type=="array") and ($f|length)>0 and
         all($f[]; (.name|type=="string") and (.sha256|hash) and (.sizeBytes|type=="number" and .>=0)) and
         ($v.sourceFiles==$f) and ($v.sourceFileCount==($f|length)))
    ' >/dev/null 2>&1
}

broray_routes_runtime_preserve()
{
    local state id base evidence original copied
    state="$1"; id="$2"
    broray_routes_runtime_regular "$state" || return 1
    base="$BRORAY_ROOT/backup"
    broray_routes_runtime_safe_path "$base" && [ -d "$base" ] || return 1
    base="$base/routes-runtime-recovery"
    broray_routes_runtime_safe_path "$base" || return 1
    if [ ! -e "$base" ]; then (umask 077; mkdir "$base") || return 1; fi
    [ -d "$base" ] && [ ! -L "$base" ] || return 1
    evidence="$(mktemp -d "$base/$id.XXXXXXXX")" || return 1
    original="$(sha256sum "$state")" || return 1; original="${original%% *}"
    cp -p "$state" "$evidence/state.json" || return 1
    copied="$(sha256sum "$evidence/state.json")" || return 1; copied="${copied%% *}"
    [ "$original" = "$copied" ] && cmp -s "$state" "$evidence/state.json" || return 1
    jq -n --arg id "$id" --arg path "$state" --arg original "$original" --arg copied "$copied" \
        --arg at "$(broray_routes_runtime_now)" \
        '{schemaVersion:1,bundleId:$id,path:$path,originalSha256:$original,backupSha256:$copied,at:$at}' \
        >"$evidence/recovery.json" || return 1
    chmod 600 "$evidence/state.json" "$evidence/recovery.json" || return 1
    BRORAY_ROUTES_RUNTIME_EVIDENCE="$evidence/state.json"
}

broray_routes_runtime_recovery_registry_valid()
{
    local registry id global
    registry="$1"; id="$2"; global="$BRORAY_ROUTES_ROOT/installed/routes.json"
    broray_routes_runtime_regular "$registry" && broray_routes_runtime_regular "$global" || return 1
    jq -es --arg id "$id" --arg interface "$interface" --slurpfile g "$global" '
      def keys_ok: type=="array" and all(.[]; type=="string" and length>0) and (length==(unique|length));
      length==1 and ($g|length)==1 and (.[0] as $b | $g[0] as $g |
      $b.schemaVersion==1 and $b.bundleId==$id and $b.targetInterface==$interface and
      ($b.managedMetric // 1200)==1200 and ($b|has("installedVersion")) and
      ($b.installedVersion==null or ($b.installedVersion|type=="object")) and
      ($b.routeKeys|keys_ok) and ($b.managedRouteKeys|keys_ok) and ($b.externalRouteKeys|keys_ok) and
      (($b.managedRouteKeys+$b.externalRouteKeys|sort)==($b.routeKeys|sort)) and
      ($b.installedVersion!=null or ($b.routeKeys|length)==0) and
      $g.schemaVersion==1 and $g.managedInterface==$interface and ($g.managedMetric // 1200)==1200 and
      ($g.routes|type=="array") and ([$g.routes[].key]|keys_ok) and
      all($g.routes[]; .interface==$interface and (.metric // 1200)==1200 and .managed==true and
          .createdByBROray==true and (.owners|keys_ok) and (.owners|length)>0) and
      ([$g.routes[] | select(.owners|index($id)!=null) | .key]|sort)==($b.managedRouteKeys|sort))
    ' "$registry" >/dev/null 2>&1
}

# Use the existing native guard shared with resource acquisition. A present
# lease remains BUSY/ambiguous; startup never removes or adopts it.
broray_routes_runtime_prepare()
{
    local path guard
    for path in "$BRORAY_ROUTES_ROOT" "$BRORAY_ROUTES_ROOT/locks"; do
        broray_routes_runtime_safe_path "$path" || return 1
        if [ ! -e "$path" ]; then mkdir "$path" || return 1; fi
        [ -d "$path" ] && [ ! -L "$path" ] || return 1
    done
    guard="${BRORAY_OPS_GUARD:-$BRORAY_ROOT/bin/broray-ops-guard}"
    BRORAY_ROOT="$BRORAY_ROOT" BRORAY_ROUTES_ROOT="$BRORAY_ROUTES_ROOT" \
    BRORAY_ROUTES_RUNTIME_SYNC_LIBRARY="$BRORAY_ROUTES_RUNTIME_SYNC_LIBRARY" \
    "$guard" "$BRORAY_ROUTES_ROOT/locks/resource.control.guard" /opt/bin/ash -c \
        '. "$BRORAY_ROOT/lib/routes-runtime-repair.sh"; broray_routes_runtime_prepare_locked'
}

broray_routes_runtime_prepare_locked()
{
    local routes share bundles config interface now work stage id manifest state registry
    local custom custom_ids custom_id custom_manifest custom_state custom_registry custom_catalog path file needs_repair temporary

    routes="$BRORAY_ROUTES_ROOT"
    share="$BRORAY_ROOT/share/routes/manifests"
    bundles="$routes/bundles.json"
    config="$routes/config.json"
    custom="$routes/custom.json"
    now="$(broray_routes_runtime_now)"
    work="$BRORAY_ROOT/tmp/routes-runtime-repair-$$"
    stage="$work/stage"

    [ "${BRORAY_OPS_GUARD_HELD:-}" = 1 ] || return 1
    local held fd
    held=false
    for fd in /proc/$$/fd/*; do
        [ "$(readlink "$fd" 2>/dev/null)" != "$routes/locks/resource.control.guard" ] || held=true
    done
    [ "$held" = true ] || return 1
    for path in "$routes/locks/operation.lock" "$routes/rollback-required.json"; do
        [ ! -e "$path" ] && [ ! -L "$path" ] || {
            broray_routes_runtime_error "ROUTES_RECOVERY_BLOCKED: сохранено свидетельство операции."; return 1;
        }
    done
    # Check directories before mkdir/jq/cp can follow unexpected objects.
    for path in "$routes/catalog" "$routes/manifests" "$routes/state" "$routes/installed" \
        "$routes/installed/bundles" "$routes/tmp" "$routes/transactions" "$BRORAY_ROOT/tmp"; do
        broray_routes_runtime_safe_path "$path" || return 1
        if [ ! -e "$path" ]; then mkdir "$path" || return 1; fi
        [ -d "$path" ] && [ ! -L "$path" ] || return 1
    done
    for file in "$config" "$bundles" "$custom" "$routes/installed/routes.json" \
        "$routes"/state/*.json "$routes"/installed/bundles/*.json "$routes"/manifests/*.json; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        broray_routes_runtime_regular "$file" && continue
        # Legacy updater seeds missing files as a hardlink to its rollback
        # owner. Accept only an exact packaged builtin manifest for reading;
        # the staged atomic replacement below never writes into that inode.
        # State, ownership and custom manifests retain the single-link rule.
        case "$file" in "$routes/manifests/"*.json) ;; *) return 1 ;; esac
        id="${file##*/}"; id="${id%.json}"
        case "$id" in
            telegram|whatsapp|youtube|chatgpt|facebook|instagram|meta|tiktok|speedtest|wikipedia) ;;
            *) return 1 ;;
        esac
        broray_routes_runtime_safe_path "$file" && [ -f "$file" ] && [ ! -L "$file" ] &&
            [ "$(find "$file" -maxdepth 0 -type f -links 2 -print)" = "$file" ] &&
            [ -f "$share/$id.json" ] && [ ! -L "$share/$id.json" ] &&
            cmp -s "$file" "$share/$id.json" || return 1
    done

    command -v jq >/dev/null 2>&1 ||
        broray_routes_runtime_error "Команда jq недоступна." || return 1

    [ -r "$config" ] ||
        broray_routes_runtime_error "Конфигурация маршрутов недоступна." || return 1
    [ -r "$bundles" ] ||
        broray_routes_runtime_error "Реестр наборов маршрутов недоступен." || return 1
    [ -d "$share" ] ||
        broray_routes_runtime_error "Встроенные манифесты маршрутов недоступны." || return 1

    jq -es 'length==1 and (.[0] | .schemaVersion==1 and .managedMetric==1200 and
        .routeComment=="BROray" and .ownershipPolicy.adoptExistingRoutes==false and
        .ownershipPolicy.modifyExternalRoutes==false and .ownershipPolicy.deleteExternalRoutes==false and
        .ownershipPolicy.touchOtherInterfaces==false and .ownershipPolicy.deleteOnlyExactManagedMatch==true)
    ' "$config" >/dev/null 2>&1 || {
        broray_routes_runtime_error "ROUTES_CONFIG_INVALID: конфигурация маршрутов повреждена."; return 1;
    }
    if [ ! -e "$routes/installed/routes.json" ]; then
        for registry in "$routes"/installed/bundles/*.json; do
            [ ! -e "$registry" ] || {
                broray_routes_runtime_error "ROUTES_RECOVERY_OWNERSHIP_INVALID: общий реестр отсутствует."; return 1;
            }
        done
    fi

    interface="$(jq -r '.managedInterface // empty' "$config" 2>/dev/null)"
    case "$interface" in
        Proxy[0-9]*) ;;
        *) broray_routes_runtime_error "Некорректный управляемый интерфейс Keenetic."; return 1 ;;
    esac
    case "${interface#Proxy}" in
        ''|*[!0-9]*) broray_routes_runtime_error "Некорректный управляемый интерфейс Keenetic."; return 1 ;;
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
            "instagram", "meta", "tiktok", "speedtest", "wikipedia"
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

    for id in telegram whatsapp youtube chatgpt facebook instagram meta tiktok speedtest wikipedia
    do
        manifest="$share/$id.json"
        state="$routes/state/$id.json"
        registry="$routes/installed/bundles/$id.json"

        if [ -e "$state" ] && [ ! -e "$registry" ]; then
            rm -rf "$work"
            broray_routes_runtime_error "ROUTES_RECOVERY_OWNERSHIP_INVALID: реестр набора отсутствует: $id"
            return 1
        fi

        jq -e \
            --arg id "$id" '
            (.schemaVersion == 1) and
            (.id == $id) and
            (.source.provider == "github") and
            (.targetInterface == "Proxy0") and
            (.exportComment == "BROray")
        ' "$manifest" >/dev/null 2>&1 || {
            rm -rf "$work"
            broray_routes_runtime_error "Повреждён встроенный манифест: $id"
            return 1
        }

        jq --arg interface "$interface" '
            .targetInterface = $interface
        ' "$manifest" >"$stage/manifests/$id.json" || {
            rm -rf "$work"
            return 1
        }

        needs_repair=false
        if [ -r "$state" ] && broray_routes_runtime_state_valid "$state" "$id"; then
            cp -p "$state" "$stage/state/$id.json" || {
                rm -rf "$work"
                return 1
            }
        else
            needs_repair=true
            jq -n --arg id "$id" --arg now "$now" '{
                schemaVersion: 1,
                bundleId: $id,
                status: "not_checked",
                availableVersion: null,
                downloadedVersion: null,
                installedVersion: null,
                routeCount: null,
                lastCheckedAt: null,
                lastVerifiedAt: null,
                lastDownloadedAt: null,
                lastExportedAt: null,
                lastDeletedAt: null,
                lastError: null,
                checkResult: null,
                verifyResult: null,
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

        if [ "$needs_repair" = true ]; then
            if [ -e "$state" ] || [ -e "$registry" ]; then
                broray_routes_runtime_recovery_registry_valid "$registry" "$id" || {
                    rm -rf "$work"
                    broray_routes_runtime_error "ROUTES_RECOVERY_OWNERSHIP_INVALID: $id"
                    return 1
                }
                jq --slurpfile registry "$registry" '
                    .installedVersion = $registry[0].installedVersion |
                    .status = (if .installedVersion == null then "not_checked" else "installed" end) |
                    .routeCount = (if .installedVersion == null then null else ($registry[0].routeKeys|length) end)
                ' "$stage/state/$id.json" >"$stage/state/$id.rebuilt" &&
                mv "$stage/state/$id.rebuilt" "$stage/state/$id.json" || { rm -rf "$work"; return 1; }
            elif [ -e "$routes/installed/routes.json" ]; then
                # Missing files of a newly added builtin are allowed only when
                # the verified global registry does not claim its ownership.
                jq -e --arg id "$id" 'all(.routes[]; (.owners|index($id))==null)' \
                    "$routes/installed/routes.json" >/dev/null 2>&1 || { rm -rf "$work"; return 1; }
            fi
        fi

        if [ -e "$routes/catalog/$id" ] || [ -L "$routes/catalog/$id" ]; then
            broray_routes_runtime_safe_path "$routes/catalog/$id" &&
                [ -d "$routes/catalog/$id" ] && [ ! -L "$routes/catalog/$id" ] || { rm -rf "$work"; return 1; }
            if ! broray_routes_runtime_catalog_valid "$id"; then
                # Keep the downloaded bytes intact for diagnosis. The state
                # cannot authorize exporting an earlier plan after invalidation.
                jq '
                    .downloadedVersion=null | .availableVersion=null |
                    .verifyResult=null | .downloadResult=null | .checkResult=null |
                    .preflight=null | .exportBuild=null |
                    .status=(if .installedVersion==null then "not_checked" else "installed" end) |
                    .lastError={code:"ROUTES_CATALOG_INVALID",message:"Локальный каталог повреждён. Проверить обновления / Скачать заново."}
                ' "$stage/state/$id.json" >"$stage/state/$id.catalog" &&
                mv "$stage/state/$id.catalog" "$stage/state/$id.json" || { rm -rf "$work"; return 1; }
            fi
        fi
        broray_routes_runtime_state_valid "$stage/state/$id.json" "$id" || { rm -rf "$work"; return 1; }
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
        (.bundles[0:10] == [
            "telegram", "whatsapp", "youtube", "chatgpt", "facebook",
            "instagram", "meta", "tiktok", "speedtest", "wikipedia"
        ]) and
        (([.bundles[]] | length) == ([.bundles[]] | unique | length))
    ' "$stage/bundles.json" >/dev/null 2>&1 || {
        rm -rf "$work"
        broray_routes_runtime_error "Подготовленный реестр наборов не прошёл проверку."
        return 1
    }

    # Validate existing ownership before any live replacement. The existing
    # initializer may create a missing global registry only for initial setup.
    [ -r "$BRORAY_ROUTES_RUNTIME_SYNC_LIBRARY" ] || { rm -rf "$work"; return 1; }
    . "$BRORAY_ROUTES_RUNTIME_SYNC_LIBRARY"
    if [ -e "$routes/installed/routes.json" ]; then
        broray_routes_sync_ensure_global_registry || { rm -rf "$work"; return 1; }
    fi
    # Complete and verify evidence for every changed state before committing
    # even the first state. Failure never truncates the live corrupt file.
    for id in telegram whatsapp youtube chatgpt facebook instagram meta tiktok speedtest wikipedia; do
        state="$routes/state/$id.json"
        if [ -e "$state" ] && ! cmp -s "$state" "$stage/state/$id.json"; then
            broray_routes_runtime_preserve "$state" "$id" || {
                rm -rf "$work"; broray_routes_runtime_error "ROUTES_RECOVERY_EVIDENCE_FAILED: $id"; return 1;
            }
            printf '%s\n' "$BRORAY_ROUTES_RUNTIME_EVIDENCE" >"$stage/state/$id.evidence" || { rm -rf "$work"; return 1; }
        fi
    done

    broray_routes_runtime_apply_staged_metadata "$bundles" "$stage/bundles.json" || {
        rm -rf "$work"
        return 1
    }
    cp -p "$stage/bundles.json" "$bundles.new.$$" &&
    mv "$bundles.new.$$" "$bundles" || {
        rm -f "$bundles.new.$$" 2>/dev/null || true
        rm -rf "$work"
        broray_routes_runtime_error "Не удалось сохранить реестр наборов."
        return 1
    }

    for id in telegram whatsapp youtube chatgpt facebook instagram meta tiktok speedtest wikipedia
    do
        broray_routes_runtime_apply_staged_metadata \
            "$routes/manifests/$id.json" "$stage/manifests/$id.json" &&
        broray_routes_runtime_apply_staged_metadata \
            "$routes/state/$id.json" "$stage/state/$id.json" &&
        broray_routes_runtime_apply_staged_metadata \
            "$routes/installed/bundles/$id.json" "$stage/installed/bundles/$id.json" || {
            rm -rf "$work"
            return 1
        }

        state="$routes/state/$id.json"
        if ! cmp -s "$state" "$stage/state/$id.json"; then
            broray_routes_runtime_safe_path "$state" || { rm -rf "$work"; return 1; }
            if [ -e "$stage/state/$id.evidence" ]; then
                broray_routes_runtime_regular "$state" &&
                    cmp -s "$state" "$(cat "$stage/state/$id.evidence")" || { rm -rf "$work"; return 1; }
            else
                [ ! -e "$state" ] && [ ! -L "$state" ] || { rm -rf "$work"; return 1; }
            fi
            temporary="$(mktemp "$routes/state/.$id.recovery.XXXXXXXX")" || { rm -rf "$work"; return 1; }
            if ! cp -p "$stage/state/$id.json" "$temporary" ||
               ! broray_routes_runtime_state_valid "$temporary" "$id" ||
               ! cmp -s "$stage/state/$id.json" "$temporary" ||
               ! mv -f "$temporary" "$state"; then
                rm -f "$temporary"; rm -rf "$work"; return 1
            fi
            broray_routes_runtime_state_valid "$state" "$id" &&
                cmp -s "$state" "$stage/state/$id.json" || { rm -rf "$work"; return 1; }
        fi
        cp -p "$stage/manifests/$id.json" "$routes/manifests/$id.json.new.$$" &&
        mv "$routes/manifests/$id.json.new.$$" "$routes/manifests/$id.json" &&
        cp -p "$stage/installed/bundles/$id.json" "$routes/installed/bundles/$id.json.new.$$" &&
        mv "$routes/installed/bundles/$id.json.new.$$" "$routes/installed/bundles/$id.json" || {
            rm -rf "$work"
            broray_routes_runtime_error "Не удалось установить runtime-файлы набора: $id"
            return 1
        }
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
