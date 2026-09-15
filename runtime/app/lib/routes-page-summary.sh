#!/opt/bin/ash

# BROray routes page summaries.
# One summary request reads Keenetic running-config at most once and builds all
# cards from that shared snapshot. Idle overview does not read Keenetic.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_ROUTES_CONFIG_LIBRARY="${BRORAY_ROUTES_CONFIG_LIBRARY:-$BRORAY_ROOT/lib/routes-router-config.sh}"
BRORAY_ROUTES_PROGRESS_LIBRARY="${BRORAY_ROUTES_PROGRESS_LIBRARY:-$BRORAY_ROOT/lib/routes-operation-progress.sh}"
BRORAY_ROUTES_API_LOCK_LIBRARY="${BRORAY_ROUTES_API_LOCK_LIBRARY:-$BRORAY_ROOT/lib/routes-api-operation.sh}"
BRORAY_ROUTES_PAGE_TMP="${BRORAY_ROUTES_PAGE_TMP:-$BRORAY_ROOT/tmp}"
BRORAY_ROUTES_CATALOG_IDS="telegram whatsapp youtube chatgpt facebook instagram meta tiktok speedtest wikipedia"

broray_routes_page_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_page_interface_display()
{
    local receipt description

    receipt="$BRORAY_ROOT/config/interface.json"
    description=""
    if [ -f "$receipt" ] && [ ! -L "$receipt" ]; then
        description="$(jq -r '.description // empty' "$receipt" 2>/dev/null || true)"
    fi
    case "$description" in
        BROray|BROray\ -\ *) printf '%s\n' "$description" ;;
        *) printf '%s\n' 'BROray' ;;
    esac
}

broray_routes_page_bundle_valid()
{
    case "${1:-}" in
        ''|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
            return 1
            ;;
    esac
    return 0
}

broray_routes_page_custom_ids()
{
    local index bundle_id
    index="$BRORAY_ROUTES_ROOT/custom.json"
    [ -r "$index" ] || return 0
    jq -r '
        select(.schemaVersion == 1 and (.bundles | type) == "array") |
        .bundles[]? | .id // empty
    ' "$index" 2>/dev/null |
        while IFS= read -r bundle_id; do
            broray_routes_page_bundle_valid "$bundle_id" && printf '%s\n' "$bundle_id"
        done
}

broray_routes_page_scope_ids()
{
    local scope
    scope="$1"
    case "$scope" in
        catalog)
            printf '%s\n' $BRORAY_ROUTES_CATALOG_IDS
            ;;
        custom)
            broray_routes_page_custom_ids
            ;;
        all)
            printf '%s\n' $BRORAY_ROUTES_CATALOG_IDS
            broray_routes_page_custom_ids
            ;;
        *)
            return 1
            ;;
    esac
}

broray_routes_page_idle_progress()
{
    local bundle_id
    bundle_id="$1"
    jq -nc --arg bundleId "$bundle_id" '
        {
            schemaVersion:2,
            kind:"routes",
            bundleId:$bundleId,
            operation:null,
            phase:"idle",
            current:0,
            total:0,
            percent:0,
            currentRoute:null,
            message:"Операция не выполняется.",
            running:false,
            success:null,
            rolledBack:false,
            resumable:false,
            stopRequested:false,
            stoppedByUser:false,
            resumed:false,
            errorRoute:null,
            pid:null,
            startedAt:null,
            updatedAt:null,
            completedAt:null
        }
    '
}

broray_routes_page_global_operation()
{
    local output default
    output="$1"
    default='{"active":false,"pending":false,"resumable":false,"pid":null,"scope":null,"action":null,"bundleId":null,"startedAt":null,"updatedAt":null,"stale":false}'

    if [ -r "$BRORAY_ROUTES_API_LOCK_LIBRARY" ]; then
        . "$BRORAY_ROUTES_API_LOCK_LIBRARY"
        if broray_routes_api_lock_read_json >"$output" 2>/dev/null &&
           jq -e 'type == "object" and (.active | type) == "boolean"' "$output" >/dev/null 2>&1
        then
            return 0
        fi
    fi

    printf '%s\n' "$default" >"$output"
}

broray_routes_page_operation_status()
{
    local output global_file progress_file custom_file bundle_id bundle_type bundle_name manifest rc
    output="$1"
    global_file="$BRORAY_ROUTES_PAGE_TMP/routes-page-operation-global.$$.json"
    progress_file="$BRORAY_ROUTES_PAGE_TMP/routes-page-operation-progress.$$.json"
    custom_file="$BRORAY_ROUTES_ROOT/custom.json"

    mkdir -p "$BRORAY_ROUTES_PAGE_TMP" || return 1
    broray_routes_page_global_operation "$global_file" || return 1

    bundle_id="$(jq -r '.bundleId // empty' "$global_file" 2>/dev/null)"
    bundle_type="catalog"
    bundle_name="$bundle_id"

    if [ -n "$bundle_id" ]; then
        if [ -r "$custom_file" ] && jq -e --arg id "$bundle_id" '.bundles[]? | select(.id == $id)' "$custom_file" >/dev/null 2>&1; then
            bundle_type="custom"
            bundle_name="$(jq -r --arg id "$bundle_id" '.bundles[]? | select(.id == $id) | .name // $id' "$custom_file" | sed -n '1p')"
        else
            manifest="$BRORAY_ROUTES_ROOT/manifests/$bundle_id.json"
            if [ -r "$manifest" ]; then
                bundle_name="$(jq -r '.name // .id // empty' "$manifest" 2>/dev/null | sed -n '1p')"
            fi
        fi
    fi
    [ -n "$bundle_name" ] || bundle_name="$bundle_id"

    if [ -n "$bundle_id" ] && [ -r "$BRORAY_ROUTES_PROGRESS_LIBRARY" ]; then
        . "$BRORAY_ROUTES_PROGRESS_LIBRARY"
        if ! broray_routes_progress_read "$bundle_id" >"$progress_file" 2>/dev/null ||
           ! jq -e 'type == "object"' "$progress_file" >/dev/null 2>&1
        then
            broray_routes_page_idle_progress "$bundle_id" >"$progress_file"
        fi
    else
        broray_routes_page_idle_progress "${bundle_id:-}" >"$progress_file"
    fi

    jq -n \
        --slurpfile global "$global_file" \
        --slurpfile progress "$progress_file" \
        --arg bundleType "$bundle_type" \
        --arg bundleName "$bundle_name" \
        --arg generatedAt "$(broray_routes_page_now)" '
        ($global[0] // {}) as $g |
        ($progress[0] // {}) as $p |
        {
            schemaVersion:1,
            generatedAt:$generatedAt,
            active:(($g.active // false) or ($p.running // false) or ($p.resumable // false)),
            pending:($g.pending // false),
            resumable:($p.resumable // $g.resumable // false),
            running:($p.running // false),
            bundleId:(if ($g.bundleId // "") != "" then $g.bundleId elif ($p.bundleId // "") == "" then null else ($p.bundleId // null) end),
            bundleType:(if (($g.bundleId // "") == "" and ($p.bundleId // "") == "") then null else $bundleType end),
            bundleName:(if (($g.bundleId // "") == "" and ($p.bundleId // "") == "") then null else $bundleName end),
            action:($g.action // $p.operation // null),
            globalOperation:$g,
            progress:$p
        }
    ' >"$output"
    rc=$?
    rm -f "$global_file" "$progress_file"
    return "$rc"
}

broray_routes_page_collect_states()
{
    local ids_file output bundle_id file
    ids_file="$1"
    output="$2"
    set --
    while IFS= read -r bundle_id; do
        [ -n "$bundle_id" ] || continue
        file="$BRORAY_ROUTES_ROOT/state/$bundle_id.json"
        [ -r "$file" ] && set -- "$@" "$file"
    done <"$ids_file"

    if [ "$#" -eq 0 ]; then
        printf '[]\n' >"$output"
        return 0
    fi

    jq -s '[.[] | select(type == "object" and (.bundleId | type) == "string") | {
        schemaVersion:(.schemaVersion // 1),
        bundleId,
        status:(.status // "unknown"),
        routeCount:(.routeCount // 0),
        availableVersion:(.availableVersion // null),
        downloadedVersion:(.downloadedVersion // null),
        installedVersion:(.installedVersion // null),
        lastCheckedAt:(.lastCheckedAt // null),
        lastVerifiedAt:(.lastVerifiedAt // null),
        lastDownloadedAt:(.lastDownloadedAt // null),
        lastExportedAt:(.lastExportedAt // null),
        lastDeletedAt:(.lastDeletedAt // null),
        lastError:(.lastError // null),
        checkResult:(.checkResult // null),
        verifyResult:(.verifyResult // null),
        downloadResult:(.downloadResult // null),
        exportBuild:(.exportBuild // null),
        exportResult:(.exportResult // null),
        deleteResult:(.deleteResult // null),
        customImport:(.customImport // null),
        updatedAt:(.updatedAt // null)
    }]' "$@" >"$output"
}

broray_routes_page_collect_installed()
{
    local ids_file output bundle_id file
    ids_file="$1"
    output="$2"
    set --
    while IFS= read -r bundle_id; do
        [ -n "$bundle_id" ] || continue
        file="$BRORAY_ROUTES_ROOT/installed/bundles/$bundle_id.json"
        [ -r "$file" ] && set -- "$@" "$file"
    done <"$ids_file"

    if [ "$#" -eq 0 ]; then
        printf '[]\n' >"$output"
        return 0
    fi

    jq -s '[.[] | select(type == "object" and (.bundleId | type) == "string") | {
        schemaVersion:(.schemaVersion // 1),
        bundleId,
        installedVersion:(.installedVersion // null),
        targetInterface:(.targetInterface // null),
        routeKeys:(.routeKeys // []),
        installedAt:(.installedAt // null)
    }]' "$@" >"$output"
}

broray_routes_page_collect_metadata()
{
    local scope ids_file output custom_file temp bundle_id manifest rc
    scope="$1"
    ids_file="$2"
    output="$3"
    custom_file="$BRORAY_ROUTES_ROOT/custom.json"
    temp="$BRORAY_ROUTES_PAGE_TMP/routes-page-meta.$$.jsonl"
    : >"$temp" || return 1

    while IFS= read -r bundle_id; do
        [ -n "$bundle_id" ] || continue
        if [ -r "$custom_file" ] &&
           jq -e --arg id "$bundle_id" '.bundles[]? | select(.id == $id)' "$custom_file" >/dev/null 2>&1
        then
            jq -c --arg id "$bundle_id" '
                .bundles[]? | select(.id == $id) | {
                    id,
                    name:(.name // .id),
                    description:(.description // "Пользовательский набор маршрутов."),
                    sourceProvider:(.sourceProvider // "local-upload"),
                    canonicalRouteCount:(.canonicalRouteCount // 0),
                    exportRouteCount:(.exportRouteCount // 0),
                    sourceFileCount:(.sourceFileCount // 0),
                    createdAt:(.createdAt // null),
                    updatedAt:(.updatedAt // null)
                }
            ' "$custom_file" >>"$temp" 2>/dev/null || true
        else
            manifest="$BRORAY_ROUTES_ROOT/manifests/$bundle_id.json"
            if [ -r "$manifest" ]; then
                jq -c '{
                    id:(.id // ""),
                    name:(.name // .id // ""),
                    description:null,
                    sourceProvider:(.source.provider // "github"),
                    sourceDirectory:(.source.directory // null),
                    sourceFileCount:([.source.files[]? | select((.enabled // true) == true)] | length),
                    targetInterface:(.targetInterface // null)
                }' "$manifest" >>"$temp" 2>/dev/null || true
            fi
        fi
    done <"$ids_file"

    jq -s '.' "$temp" >"$output"
    rc=$?
    rm -f "$temp"
    return "$rc"
}

broray_routes_page_summary()
{
    local scope output ids states installed metadata actual global operation registry config actual_available managed_interface managed_interface_display managed_metric rc
    scope="$1"
    output="$2"
    ids="$BRORAY_ROUTES_PAGE_TMP/routes-page-ids.$$.txt"
    states="$BRORAY_ROUTES_PAGE_TMP/routes-page-states.$$.json"
    installed="$BRORAY_ROUTES_PAGE_TMP/routes-page-installed.$$.json"
    metadata="$BRORAY_ROUTES_PAGE_TMP/routes-page-metadata.$$.json"
    actual="$BRORAY_ROUTES_PAGE_TMP/routes-page-actual.$$.json"
    global="$BRORAY_ROUTES_PAGE_TMP/routes-page-global.$$.json"
    operation="$BRORAY_ROUTES_PAGE_TMP/routes-page-operation.$$.json"
    registry="$BRORAY_ROUTES_ROOT/installed/routes.json"
    config="$BRORAY_ROUTES_ROOT/config.json"

    mkdir -p "$BRORAY_ROUTES_PAGE_TMP" || return 1
    broray_routes_page_scope_ids "$scope" >"$ids" || return 1
    broray_routes_page_collect_states "$ids" "$states" || return 1
    broray_routes_page_collect_installed "$ids" "$installed" || return 1
    broray_routes_page_collect_metadata "$scope" "$ids" "$metadata" || return 1
    broray_routes_page_global_operation "$global" || return 1
    broray_routes_page_operation_status "$operation" || return 1

    actual_available=true
    if [ -r "$BRORAY_ROUTES_CONFIG_LIBRARY" ]; then
        # Longer cache is intentional for page summaries. Explicit actions and
        # final refresh invalidate or replace it through existing route code.
        BRORAY_ROUTES_CONFIG_TTL="${BRORAY_ROUTES_PAGE_CONFIG_TTL:-30}"
        export BRORAY_ROUTES_CONFIG_TTL
        . "$BRORAY_ROUTES_CONFIG_LIBRARY"
        if ! broray_routes_config_snapshot "$actual"; then
            actual_available=false
        fi
    else
        actual_available=false
    fi

    if [ "$actual_available" != true ]; then
        jq -nc --arg at "$(broray_routes_page_now)" '{schemaVersion:1,source:"unavailable",fetchedAt:$at,fetchedEpoch:0,routes:[]}' >"$actual"
    fi

    if [ ! -r "$registry" ]; then
        jq -nc '{schemaVersion:1,managedInterface:null,managedMetric:1200,routes:[]}' >"$BRORAY_ROUTES_PAGE_TMP/routes-page-empty-registry.$$.json"
        registry="$BRORAY_ROUTES_PAGE_TMP/routes-page-empty-registry.$$.json"
    fi

    managed_interface="$(jq -r '.managedInterface // empty' "$config" 2>/dev/null || true)"
    managed_interface_display="$(broray_routes_page_interface_display)"
    managed_metric="$(jq -r '.managedMetric // 1200' "$config" 2>/dev/null)"
    case "$managed_metric" in ''|*[!0-9]*) managed_metric=1200 ;; esac

    jq -n \
        --slurpfile states "$states" \
        --slurpfile installed "$installed" \
        --slurpfile metadata "$metadata" \
        --slurpfile actual "$actual" \
        --slurpfile global "$global" \
        --slurpfile operation "$operation" \
        --slurpfile registry "$registry" \
        --arg scope "$scope" \
        --arg generatedAt "$(broray_routes_page_now)" \
        --arg managedInterface "$managed_interface" \
        --arg managedInterfaceDisplay "$managed_interface_display" \
        --argjson managedMetric "$managed_metric" \
        --argjson actualAvailable "$actual_available" '
        def version_equal($a; $b):
            if ($a == null or $b == null) then false
            elif (($a.contentSha256 // "") != "" and ($b.contentSha256 // "") != "") then
                $a.contentSha256 == $b.contentSha256
            elif (($a.sourceSetSha256 // "") != "" and ($b.sourceSetSha256 // "") != "") then
                $a.sourceSetSha256 == $b.sourceSetSha256
            else
                (($a.sourceCommit // "") == ($b.sourceCommit // "")) and
                (($a.sourceDate // "") == ($b.sourceDate // ""))
            end;
        def expected_key($key):
            ($key | split("|")) as $p |
            (($p[4] // "metric:1200") | split(":")[1] | tonumber? // 1200) as $metric |
            (($p[1] // "") + "|" + ($p[2] // "") + "|" + ($metric|tostring));
        def actual_key($r):
            (($r.destination // "") + "|" + ($r.interface // "") + "|" + (($r.metric // 1000)|tostring));

        ($states[0] // []) as $stateList |
        ($installed[0] // []) as $installedList |
        ($metadata[0] // []) as $metaList |
        ($actual[0] // {routes:[]}) as $actualData |
        ($registry[0] // {routes:[]}) as $registryData |
        ($global[0] // {}) as $globalData |
        ($operation[0] // {}) as $operationData |
        (
            $actualData.routes |
            map(select((.gateway // "0.0.0.0") == "0.0.0.0" and ((.proto // "static")|ascii_downcase) == "static")) |
            group_by(actual_key(.)) |
            map({key:actual_key(.[0]),value:length}) |
            from_entries
        ) as $actualIndex |
        [
            $metaList[] as $meta |
            ($stateList | map(select(.bundleId == $meta.id)) | first // {bundleId:$meta.id,status:"not_checked",routeCount:0}) as $state |
            ($installedList | map(select(.bundleId == $meta.id)) | first // null) as $bundle |
            (($bundle.routeKeys // []) | map(expected_key(.))) as $expectedKeys |
            ($expectedKeys | map(($actualIndex[.] // 0))) as $matches |
            ($matches | map(select(. == 1)) | length) as $presentCount |
            ($matches | map(select(. == 0)) | length) as $missingCount |
            ($matches | map(select(. > 1)) | length) as $duplicateCount |
            ($expectedKeys | length) as $expectedCount |
            (($bundle != null) and (($bundle.installedVersion // null) != null) and ($expectedCount > 0)) as $registered |
            ([$registryData.routes[]? | select((.owners // []) | index($meta.id) != null)]) as $ownedRoutes |
            ([$ownedRoutes[] | select((((.owners // []) | length) == 1))] | length) as $uniqueCount |
            ([$ownedRoutes[] | select((((.owners // []) | length) > 1))] | length) as $sharedCount |
            (($state.availableVersion // null) as $available |
             ($state.downloadedVersion // null) as $downloaded |
             ($state.installedVersion // null) as $installedVersion |
             (($available != null) and (version_equal($available; $downloaded) | not))) as $downloadRequired |
            (($state.downloadedVersion != null) and ($state.installedVersion == null or (version_equal($state.downloadedVersion; $state.installedVersion)|not))) as $keeneticUpdateRequired |
            (($expectedCount > 0) and ($missingCount == 0) and ($duplicateCount == 0)) as $complete |
            ($registered and $actualAvailable and ($complete|not)) as $drift |
            (
                ($state.verifyResult // null) as $verification |
                ($verification.keenetic.status // "") as $verificationStatus |
                if ($verification == null) or (($state.downloadedVersion // null) == null) then null
                elif (($verification.local.valid // false) != true) then null
                elif (($verification.contentSha256 // "") == "") or
                     (($verification.contentSha256 // "") != ($state.downloadedVersion.contentSha256 // "")) then null
                elif ($actualAvailable | not) then
                    if (($verification.keenetic.available // true) == false) and ($verificationStatus == "unavailable")
                    then $verification else null end
                elif (($verification.keenetic.available // false) != true) then null
                elif $registered then
                    if $complete then
                        if (["complete", "update_pending", "conflict"] | index($verificationStatus)) != null
                        then $verification else null end
                    else
                        if (["restore_required", "conflict"] | index($verificationStatus)) != null
                        then $verification else null end
                    end
                else
                    if (["not_installed", "conflict"] | index($verificationStatus)) != null
                    then $verification else null end
                end
            ) as $currentVerification |
            (($state.lastError != null) or $drift or $downloadRequired or $keeneticUpdateRequired or ($currentVerification == null and ($state.downloadedVersion // null) != null)) as $attention |
            ($operationData.progress // {}) as $activeProgress |
            ($operationData.bundleId == $meta.id) as $operationBelongs |
            ($state + {
                verifyResult:$currentVerification,
                lastVerifiedAt:(if $currentVerification == null then null else ($state.lastVerifiedAt // null) end),
                id:$meta.id,
                metadata:$meta,
                routerPresence:{
                    available:$actualAvailable,
                    registered:$registered,
                    source:($actualData.source // "running-config"),
                    checkedAt:($actualData.fetchedAt // null),
                    cacheAgeSeconds:(if ($actualData.fetchedEpoch // 0) > 0 then ((now|floor)-$actualData.fetchedEpoch) else null end),
                    expectedRouteCount:$expectedCount,
                    presentRouteCount:(if $actualAvailable then $presentCount else null end),
                    missingRouteCount:(if $actualAvailable then $missingCount else null end),
                    duplicateRouteCount:(if $actualAvailable then $duplicateCount else null end),
                    complete:(if $actualAvailable and $expectedCount > 0 then $complete else null end),
                    actualInstalled:(if ($actualAvailable|not) then null elif ($registered|not) then false elif $complete then true elif $presentCount == 0 then false else null end),
                    drift:(if $actualAvailable and $registered then $drift else null end),
                    status:(if ($actualAvailable|not) then "unavailable" elif ($registered|not) then "not_registered" elif $complete then "complete" elif $presentCount == 0 then "absent" else "partial" end),
                    missingRoutes:[]
                },
                ownership:{uniqueRouteCount:$uniqueCount,sharedRouteCount:$sharedCount,totalOwnedRouteCount:($ownedRoutes|length)},
                recordedInstalled:(($state.installedVersion // null) != null),
                verifiedInstalled:(if $actualAvailable then ($complete and $registered) else false end),
                sourceUpdateAvailable:$downloadRequired,
                keeneticUpdateRequired:$keeneticUpdateRequired,
                actionRequired:$attention,
                attention:$attention,
                operationProgress:(if $operationBelongs then $activeProgress else {schemaVersion:2,kind:"routes",bundleId:$meta.id,operation:null,phase:"idle",current:0,total:0,percent:0,currentRoute:null,message:"Операция не выполняется.",running:false,success:null,rolledBack:false,resumable:false,stopRequested:false,stoppedByUser:false,resumed:false,errorRoute:null,pid:null,startedAt:null,updatedAt:null,completedAt:null} end),
                globalOperation:$globalData
            })
        ] as $bundles |
        {
            schemaVersion:1,
            scope:$scope,
            generatedAt:$generatedAt,
            managedInterface:(if $managedInterface=="" then null else $managedInterface end),
            managedInterfaceDisplay:$managedInterfaceDisplay,
            managedMetric:$managedMetric,
            routerSnapshot:{available:$actualAvailable,source:($actualData.source // null),checkedAt:($actualData.fetchedAt // null),routeCount:($actualData.routes|length)},
            globalOperation:$globalData,
            operation:$operationData,
            totals:{
                bundleCount:($bundles|length),
                installedCount:([$bundles[] | select(.verifiedInstalled == true)]|length),
                verifiedInstalledCount:([$bundles[] | select(.verifiedInstalled == true)]|length),
                recordedInstalledCount:([$bundles[] | select(.recordedInstalled == true)]|length),
                registeredCount:([$bundles[] | select(.routerPresence.registered == true)]|length),
                attentionCount:([$bundles[] | select(.attention == true)]|length),
                expectedRouteCount:([$bundles[].routerPresence.expectedRouteCount]|add // 0),
                presentRouteCount:([$bundles[].routerPresence.presentRouteCount // 0]|add // 0),
                uniqueRouteCount:([$bundles[].ownership.uniqueRouteCount]|add // 0),
                sharedOwnershipCount:([$bundles[].ownership.sharedRouteCount]|add // 0)
            },
            health:(
                ([$bundles[] | select(.lastError != null)] | length) as $errorCount |
                ([$bundles[] | select(.attention == true)] | length) as $attentionCount |
                (($globalData.running // false) == true) as $busy |
                {
                    schemaVersion:1,
                    module:"routes",
                    availability:(if $actualAvailable then "available" else "partial" end),
                    severity:(
                        if $errorCount > 0 then "error"
                        elif $busy then "busy"
                        elif ($actualAvailable|not) or $attentionCount > 0 then "warning"
                        else "ok" end
                    ),
                    operational:($actualAvailable and $errorCount == 0),
                    consistent:($actualAvailable and all($bundles[]; ((.recordedInstalled|not) or .verifiedInstalled) and (.keeneticUpdateRequired|not))),
                    actionRequired:(($actualAvailable|not) or $attentionCount > 0),
                    freshness:{state:(if $actualAvailable then "fresh" else "unknown" end),checkedAt:($actualData.fetchedAt // null)},
                    reasons:(
                        ([if ($actualAvailable|not) then {code:"KEENETIC_ROUTES_UNAVAILABLE",message:"Не удалось получить фактические маршруты Keenetic.",details:null} else empty end] +
                         [$bundles[] |
                            if .lastError != null then {code:(.lastError.code // "ROUTES_ERROR"),message:(.lastError.message // "Ошибка набора маршрутов."),details:.id}
                            elif .keeneticUpdateRequired then {code:"KEENETIC_UPDATE_REQUIRED",message:("Набор «" + (.metadata.name // .id) + "» требует установки или обновления в Keenetic."),details:.id}
                            elif (.routerPresence.drift // false) then {code:"ROUTES_DRIFT",message:("Фактические маршруты набора «" + (.metadata.name // .id) + "» расходятся с реестром BROray."),details:.id}
                            elif .attention then {code:"ROUTES_ATTENTION",message:("Набор «" + (.metadata.name // .id) + "» требует проверки."),details:.id}
                            else empty end]) | unique_by(.code + "|" + (.details // ""))
                    ),
                    facts:{
                        bundleCount:($bundles|length),
                        verifiedInstalledCount:([$bundles[] | select(.verifiedInstalled == true)]|length),
                        recordedInstalledCount:([$bundles[] | select(.recordedInstalled == true)]|length),
                        attentionCount:$attentionCount,
                        presentRouteCount:([$bundles[].routerPresence.presentRouteCount // 0]|add // 0)
                    },
                    lastOperation:($operationData // null)
                }
            ),
            bundles:$bundles
        }
    ' >"$output"
    rc=$?

    rm -f "$ids" "$states" "$installed" "$metadata" "$actual" "$global" "$operation" \
        "$BRORAY_ROUTES_PAGE_TMP/routes-page-empty-registry.$$.json"
    return "$rc"
}

broray_routes_page_overview()
{
    local output ids states global operation custom registry config managed_interface managed_interface_display rc
    output="$1"
    ids="$BRORAY_ROUTES_PAGE_TMP/routes-overview-ids.$$.txt"
    states="$BRORAY_ROUTES_PAGE_TMP/routes-overview-states.$$.json"
    global="$BRORAY_ROUTES_PAGE_TMP/routes-overview-global.$$.json"
    operation="$BRORAY_ROUTES_PAGE_TMP/routes-overview-operation.$$.json"
    custom="$BRORAY_ROUTES_ROOT/custom.json"
    registry="$BRORAY_ROUTES_ROOT/installed/routes.json"
    config="$BRORAY_ROUTES_ROOT/config.json"

    mkdir -p "$BRORAY_ROUTES_PAGE_TMP" || return 1
    broray_routes_page_scope_ids all >"$ids" || return 1
    broray_routes_page_collect_states "$ids" "$states" || return 1
    broray_routes_page_global_operation "$global" || return 1
    broray_routes_page_operation_status "$operation" || return 1

    [ -r "$custom" ] || jq -nc '{schemaVersion:1,bundles:[]}' >"$BRORAY_ROUTES_PAGE_TMP/routes-overview-empty-custom.$$.json"
    [ -r "$custom" ] || custom="$BRORAY_ROUTES_PAGE_TMP/routes-overview-empty-custom.$$.json"
    [ -r "$registry" ] || jq -nc '{schemaVersion:1,routes:[]}' >"$BRORAY_ROUTES_PAGE_TMP/routes-overview-empty-registry.$$.json"
    [ -r "$registry" ] || registry="$BRORAY_ROUTES_PAGE_TMP/routes-overview-empty-registry.$$.json"

    managed_interface="$(jq -r '.managedInterface // empty' "$config" 2>/dev/null || true)"
    managed_interface_display="$(broray_routes_page_interface_display)"

    jq -n \
        --slurpfile states "$states" \
        --slurpfile custom "$custom" \
        --slurpfile registry "$registry" \
        --slurpfile global "$global" \
        --slurpfile operation "$operation" \
        --arg generatedAt "$(broray_routes_page_now)" \
        --arg managedInterface "$managed_interface" \
        --arg managedInterfaceDisplay "$managed_interface_display" \
        --argjson catalogTotal 10 '
        ($states[0] // []) as $stateList |
        ($custom[0].bundles // []) as $customBundles |
        ($registry[0].routes // []) as $routes |
        def is_custom: (.bundleId | startswith("user-"));
        def version_equal($a; $b):
            if ($a == null or $b == null) then false
            elif (($a.contentSha256 // "") != "" and ($b.contentSha256 // "") != "") then
                $a.contentSha256 == $b.contentSha256
            elif (($a.sourceSetSha256 // "") != "" and ($b.sourceSetSha256 // "") != "") then
                $a.sourceSetSha256 == $b.sourceSetSha256
            else
                (($a.sourceCommit // "") == ($b.sourceCommit // "")) and
                (($a.sourceDate // "") == ($b.sourceDate // ""))
            end;
        def state_attention:
            (.lastError != null) or
            ((.availableVersion != null) and (version_equal(.availableVersion; .downloadedVersion) | not)) or
            ((.downloadedVersion != null) and (.installedVersion == null or (version_equal(.downloadedVersion; .installedVersion) | not))) or
            ((.installedVersion != null) and ((.verifyResult // null) == null or (.verifyResult.success // false) != true));
        {
            schemaVersion:1,
            generatedAt:$generatedAt,
            managedInterface:(if $managedInterface=="" then null else $managedInterface end),
            managedInterfaceDisplay:$managedInterfaceDisplay,
            globalOperation:($global[0] // {}),
            operation:($operation[0] // {}),
            totalManagedRoutes:($routes|length),
            sharedRoutes:([$routes[] | select((((.owners // []) | length) > 1))]|length),
            custom:{
                total:($customBundles|length),
                installed:([$stateList[] | select(is_custom and .installedVersion != null)]|length),
                attention:([$stateList[] | select(is_custom and state_attention)]|length),
                routeCount:([$stateList[] | select(is_custom) | .routeCount // 0]|add // 0)
            },
            catalog:{
                total:$catalogTotal,
                installed:([$stateList[] | select((is_custom|not) and .installedVersion != null)]|length),
                attention:([$stateList[] | select((is_custom|not) and state_attention)]|length),
                routeCount:([$stateList[] | select(is_custom|not) | .routeCount // 0]|add // 0)
            },
            lastUpdatedAt:([$stateList[].updatedAt // empty] | sort | last // null),
            health:(
                ([$stateList[] | select(state_attention)] | length) as $attentionCount |
                ([$stateList[] | select(.lastError != null)] | length) as $errorCount |
                (($global[0].running // false) == true) as $busy |
                {
                    schemaVersion:1,
                    module:"routes",
                    availability:"available",
                    severity:(if $errorCount > 0 then "error" elif $busy then "busy" elif $attentionCount > 0 then "warning" else "ok" end),
                    operational:($errorCount == 0),
                    consistent:($attentionCount == 0),
                    actionRequired:($attentionCount > 0),
                    freshness:{state:"unknown",checkedAt:([$stateList[].updatedAt // empty]|sort|last // null)},
                    reasons:([$stateList[] | select(state_attention) | {code:(if .lastError != null then (.lastError.code // "ROUTES_ERROR") else "ROUTES_ATTENTION" end),message:(if .lastError != null then (.lastError.message // "Ошибка набора маршрутов.") else ("Набор «" + (.bundleId // "") + "» требует действия.") end),details:(.bundleId // null)}]),
                    facts:{attentionCount:$attentionCount,totalManagedRoutes:($routes|length)},
                    lastOperation:($operation[0] // null)
                }
            )
        }
    ' >"$output"
    rc=$?
    rm -f "$ids" "$states" "$global" "$operation" \
        "$BRORAY_ROUTES_PAGE_TMP/routes-overview-empty-custom.$$.json" \
        "$BRORAY_ROUTES_PAGE_TMP/routes-overview-empty-registry.$$.json"
    return "$rc"
}
