#!/opt/bin/ash

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_STATUS_LIBRARY="${BRORAY_STATUS_LIBRARY:-$BRORAY_ROOT/lib/status-contract.sh}"
. "$BRORAY_STATUS_LIBRARY"

broray_routes_summary_error()
{
    error_code="$1"
    shift
    printf 'BRORAY_ERROR:%s:%s\n' \
        "$error_code" \
        "$*" >&2
    return 1
}

broray_routes_summary()
{
    summary_root="${BRORAY_ROOT:-/opt/broray}"
    summary_bundle_id="${1:-telegram}"

    case "$summary_bundle_id" in
        ""|*[!a-z0-9_-]*)
            broray_routes_summary_error \
                "ROUTES_BUNDLE_NOT_FOUND" \
                "Набор маршрутов не найден."
            return 1
            ;;
    esac

    summary_bundles_file="$summary_root/routes/bundles.json"

    [ -r "$summary_bundles_file" ] &&
    jq -e \
        --arg bundle_id "$summary_bundle_id" '
        (.schemaVersion == 1) and
        ((.bundles | type) == "array") and
        (.bundles | index($bundle_id) != null)
    ' "$summary_bundles_file" >/dev/null 2>&1 || {
        broray_routes_summary_error \
            "ROUTES_BUNDLE_NOT_FOUND" \
            "Набор маршрутов не найден."
        return 1
    }

    summary_state_file="$summary_root/routes/state/$summary_bundle_id.json"
    summary_bundle_file="$summary_root/routes/installed/bundles/$summary_bundle_id.json"
    summary_global_file="$summary_root/routes/installed/routes.json"
    summary_config_file="$summary_root/routes/config.json"
    summary_lock_dir="$summary_root/routes/locks/operation.lock"

    [ -r "$summary_state_file" ] || {
        broray_routes_summary_error \
            "ROUTES_STATE_UNAVAILABLE" \
            "Локальное состояние маршрутов недоступно."
        return 1
    }

    [ -r "$summary_bundle_file" ] || {
        broray_routes_summary_error \
            "ROUTES_BUNDLE_REGISTRY_UNAVAILABLE" \
            "Реестр набора маршрутов недоступен."
        return 1
    }

    [ -r "$summary_config_file" ] || {
        broray_routes_summary_error \
            "ROUTES_CONFIG_UNAVAILABLE" \
            "Конфигурация маршрутов недоступна."
        return 1
    }

    summary_managed_interface="$(jq -r '.managedInterface // empty' "$summary_config_file" 2>/dev/null)"
    case "$summary_managed_interface" in
        Proxy[0-9]*) ;;
        *)
            broray_routes_summary_error \
                "ROUTES_CONFIG_INVALID" \
                "Некорректный управляемый интерфейс Keenetic."
            return 1
            ;;
    esac
    case "${summary_managed_interface#Proxy}" in
        ''|*[!0-9]*)
            broray_routes_summary_error \
                "ROUTES_CONFIG_INVALID" \
                "Некорректный управляемый интерфейс Keenetic."
            return 1
            ;;
    esac

    [ -r "$summary_global_file" ] || {
        broray_routes_summary_error \
            "ROUTES_GLOBAL_REGISTRY_UNAVAILABLE" \
            "Глобальный реестр маршрутов недоступен."
        return 1
    }

    jq -e \
        --arg bundle_id "$summary_bundle_id" '
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        ((.status | type) == "string") and
        (
            (.routeCount == null) or
            (
                ((.routeCount | type) == "number") and
                (.routeCount >= 0)
            )
        ) and
        ((.availableVersion == null) or
            ((.availableVersion | type) == "object")) and
        ((.downloadedVersion == null) or
            ((.downloadedVersion | type) == "object")) and
        ((.installedVersion == null) or
            ((.installedVersion | type) == "object")) and
        ((.lastError == null) or
            ((.lastError | type) == "string") or
            ((.lastError | type) == "object")) and
        ((.lastVerifiedAt == null) or
            ((.lastVerifiedAt | type) == "string")) and
        ((.verifyResult == null) or
            ((.verifyResult | type) == "object"))
    ' "$summary_state_file" >/dev/null 2>&1 || {
        broray_routes_summary_error \
            "ROUTES_STATE_INVALID" \
            "Локальное состояние маршрутов повреждено."
        return 1
    }

    jq -e \
        --arg bundle_id "$summary_bundle_id" '
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        ((.installedVersion == null) or
            ((.installedVersion | type) == "object")) and
        ((.routeKeys | type) == "array") and
        ((.managedRouteKeys | type) == "array") and
        ((.externalRouteKeys | type) == "array")
    ' "$summary_bundle_file" >/dev/null 2>&1 || {
        broray_routes_summary_error \
            "ROUTES_BUNDLE_REGISTRY_INVALID" \
            "Реестр набора маршрутов поврежден."
        return 1
    }

    jq -e '
        (.schemaVersion == 1) and
        ((.managedInterface | type) == "string") and
        ((.managedMetric | type) == "number") and
        ((.routes | type) == "array")
    ' "$summary_global_file" >/dev/null 2>&1 || {
        broray_routes_summary_error \
            "ROUTES_GLOBAL_REGISTRY_INVALID" \
            "Глобальный реестр маршрутов поврежден."
        return 1
    }

    if [ -d "$summary_lock_dir" ]; then
        summary_operation_running=true
    else
        summary_operation_running=false
    fi

    summary_last_verified="$(jq -r '.lastVerifiedAt // empty' "$summary_state_file" 2>/dev/null)"
    summary_state_epoch=""
    if [ -n "$summary_last_verified" ]; then
        summary_state_epoch="$(date -d "$summary_last_verified" '+%s' 2>/dev/null || true)"
    fi
    if [ -z "$summary_state_epoch" ]; then
        summary_state_epoch="$(broray_status_file_epoch "$summary_state_file" 2>/dev/null || printf '0')"
    fi
    summary_state_age="$(broray_status_age_seconds "$summary_state_epoch" "$(date '+%s')" 2>/dev/null || printf '0')"
    summary_state_freshness="$(broray_status_freshness_from_age "$summary_state_age" 3600 86400)"

    jq -n \
        --arg bundleId "$summary_bundle_id" \
        --arg managedInterface "$summary_managed_interface" \
        --argjson operationRunning "$summary_operation_running" \
        --arg stateFreshness "$summary_state_freshness" \
        --argjson stateAgeSeconds "$summary_state_age" \
        --slurpfile state "$summary_state_file" \
        --slurpfile bundle "$summary_bundle_file" \
        --slurpfile global "$summary_global_file" '
        def route_version_key:
            if . == null then null
            elif ((.contentSha256 // "") != "") then
                "sha256:" + .contentSha256
            elif ((.sourceCommit // "") != "") then
                "commit:" + .sourceCommit
            elif ((.sourceDate // "") != "") then
                "date:" + .sourceDate
            else
                (tojson)
            end;

        def source_version_key:
            if . == null then null
            elif ((.sourceSetSha256 // "") != "") then
                "sources:" + .sourceSetSha256
            else
                (route_version_key)
            end;

        def same_route_version($left; $right):
            ($left != null) and
            ($right != null) and
            (($left | route_version_key) == ($right | route_version_key));

        def same_source_version($left; $right):
            ($left != null) and
            ($right != null) and
            (($left | source_version_key) == ($right | source_version_key));

        def operation_item($type; $completedAt; $message):
            if ($completedAt == null) or ($completedAt == "") then
                empty
            else
                {
                    type: $type,
                    completedAt: $completedAt,
                    success: true,
                    message: $message
                }
            end;

        $state[0] as $s |
        $bundle[0] as $b |
        $global[0] as $g |

        ($s.availableVersion != null) as $available |
        ($s.downloadedVersion != null) as $downloaded |
        (
            ($s.installedVersion != null) and
            ($b.installedVersion != null)
        ) as $installed |
        (
            $available and
            (same_source_version($s.availableVersion; $s.downloadedVersion) | not)
        ) as $downloadRequired |
        (
            $downloaded and
            (
                ($s.installedVersion == null) or
                (same_route_version($s.downloadedVersion; $s.installedVersion) | not)
            )
        ) as $keeneticUpdateRequired |
        (
            $downloaded and
            ($s.installedVersion != null) and
            (same_route_version($s.downloadedVersion; $s.installedVersion) | not)
        ) as $keeneticUpdateAvailable |
        (
            $available and
            ($s.downloadedVersion != null) and
            (same_source_version($s.availableVersion; $s.downloadedVersion) | not)
        ) as $sourceUpdateAvailable |
        (
            $sourceUpdateAvailable or $keeneticUpdateAvailable
        ) as $updateAvailable |
        ($s.verifyResult // null) as $verify |
        (
            $downloaded and
            ($verify != null) and
            (($verify.contentSha256 // "") != "") and
            (($verify.contentSha256 // "") == ($s.downloadedVersion.contentSha256 // ""))
        ) as $verifyMatchesDownloaded |
        (
            $verifyMatchesDownloaded and
            ($verify.local.valid == false)
        ) as $localInvalid |
        (
            $verifyMatchesDownloaded and
            ($verify.local.valid == true)
        ) as $verificationCurrent |
        (
            $downloaded and
            ($localInvalid | not) and
            ($verificationCurrent | not)
        ) as $verificationRequired |
        (
            $verificationCurrent and
            (($verify.keenetic.status // "") == "conflict")
        ) as $verificationConflict |
        (
            $verificationCurrent and
            (
                (($verify.keenetic.available // true) == false) or
                (($verify.keenetic.status // "") == "unavailable")
            )
        ) as $verificationRetryRequired |
        ($downloadRequired or $localInvalid) as $downloadActionRequired |

        (($b.routeKeys // []) | length) as $installedRouteCount |
        (($b.managedRouteKeys // []) | length) as $managedRouteCount |
        (($b.externalRouteKeys // []) | length) as $externalRouteCount |
        (
            [
                ($g.routes // [])[] |
                select(
                    ((.owners // []) | index($bundleId)) != null
                )
            ] |
            length
        ) as $globalOwnedRouteCount |

        (
            (
                (($s.installedVersion == null) and
                    ($b.installedVersion == null)) or
                (
                    ($s.installedVersion != null) and
                    ($b.installedVersion != null) and
                    same_route_version(
                        $s.installedVersion;
                        $b.installedVersion
                    )
                )
            ) and
            ($installedRouteCount ==
                ($managedRouteCount + $externalRouteCount)) and
            ($managedRouteCount == $globalOwnedRouteCount) and
            ($g.managedInterface == $managedInterface) and
            ($g.managedMetric == 1200)
        ) as $consistent |

        (
            [
                operation_item(
                    "check";
                    ($s.lastCheckedAt // null);
                    ($s.checkResult.message // null)
                ),
                operation_item(
                    "download";
                    ($s.lastDownloadedAt // null);
                    ($s.downloadResult.message // null)
                ),
                operation_item(
                    "verify";
                    ($s.lastVerifiedAt // null);
                    ($s.verifyResult.message // null)
                ),
                operation_item(
                    "install";
                    ($s.lastExportedAt // null);
                    ($s.exportResult.message // null)
                ),
                operation_item(
                    "delete";
                    ($s.lastDeletedAt // null);
                    ($s.deleteResult.message // null)
                )
            ] |
            sort_by(.completedAt) |
            last // null
        ) as $lastOperation |

        (
            if $s.lastError != null then
                $s.lastError
            elif ($consistent | not) then
                {
                    code: "ROUTES_STATE_INCONSISTENT",
                    message: "Локальное состояние и реестры маршрутов не согласованы."
                }
            else
                null
            end
        ) as $error |

        (
            if $operationRunning then "busy"
            elif $localInvalid or $verificationConflict or ($error != null) then "error"
            elif $installed and $updateAvailable then "update_available"
            elif $installed then "installed"
            elif $downloaded then "downloaded"
            elif $available then "available"
            else "empty"
            end
        ) as $normalizedState |

        (
            if $operationRunning then "wait"
            elif $localInvalid then "download"
            elif $verificationRetryRequired then "verify"
            elif $error != null then "review"
            elif $downloadRequired then "download"
            elif $verificationRequired then "verify"
            elif $verificationConflict then "review"
            elif $keeneticUpdateRequired and ($s.installedVersion != null) then "update"
            elif $keeneticUpdateRequired then "install"
            elif $installed then "check"
            else "check"
            end
        ) as $recommendedAction |

        {
            schemaVersion: 1,
            bundleId: $bundleId,
            state: $normalizedState,
            sourceState: $s.status,
            healthState: (
                if $error != null then "error"
                elif $operationRunning then "busy"
                elif $keeneticUpdateRequired or $downloadRequired or $verificationRequired or $verificationRetryRequired then "warning"
                elif $stateFreshness == "expired" then "warning"
                else "ok" end
            ),
            installed: $installed,
            recordedInstalled: $installed,
            verifiedInstalled: (
                $installed and
                $verificationCurrent and
                (($verify.keenetic.checked // false) == true) and
                (($verify.keenetic.complete // false) == true)
            ),
            downloaded: $downloaded,
            available: $available,
            updateAvailable: $updateAvailable,
            sourceUpdateAvailable: $sourceUpdateAvailable,
            keeneticUpdateAvailable: $keeneticUpdateAvailable,
            downloadRequired: $downloadRequired,
            downloadActionRequired: $downloadActionRequired,
            verificationRequired: $verificationRequired,
            verificationCurrent: $verificationCurrent,
            localSetValid: (
                if $localInvalid then false
                elif $verificationCurrent then true
                else null
                end
            ),
            verificationConflict: $verificationConflict,
            verificationRetryRequired: $verificationRetryRequired,
            keeneticUpdateRequired: $keeneticUpdateRequired,
            exportRequired: $keeneticUpdateRequired,
            operationRunning: $operationRunning,
            recommendedAction: $recommendedAction,
            routeCount: ($s.routeCount // 0),
            installedRouteCount: $installedRouteCount,
            managedRouteCount: $managedRouteCount,
            externalRouteCount: $externalRouteCount,
            globalOwnedRouteCount: $globalOwnedRouteCount,
            managedInterface: $g.managedInterface,
            managedMetric: $g.managedMetric,
            availableVersion: $s.availableVersion,
            downloadedVersion: $s.downloadedVersion,
            installedVersion: $s.installedVersion,
            verifyResult: ($s.verifyResult // null),
            lastCheckedAt: ($s.lastCheckedAt // null),
            lastVerifiedAt: ($s.lastVerifiedAt // null),
            verificationFreshness: {
                state: $stateFreshness,
                ageSeconds: $stateAgeSeconds
            },
            lastDownloadedAt: ($s.lastDownloadedAt // null),
            lastExportedAt: ($s.lastExportedAt // null),
            lastDeletedAt: ($s.lastDeletedAt // null),
            lastOperation: $lastOperation,
            registryConsistent: $consistent,
            consistent: (
                $consistent and
                ($keeneticUpdateRequired | not) and
                ($verificationConflict | not) and
                ($verificationRetryRequired | not)
            ),
            actionRequired: (
                $downloadRequired or
                $verificationRequired or
                $verificationConflict or
                $verificationRetryRequired or
                $keeneticUpdateRequired or
                ($stateFreshness != "fresh") or
                ($error != null)
            ),
            error: $error,
            updatedAt: ($s.updatedAt // null)
        }
    ' || {
        broray_routes_summary_error \
            "ROUTES_SUMMARY_BUILD_FAILED" \
            "Не удалось сформировать summary маршрутов."
        return 1
    }
}

broray_routes_summary_all()
{
    summary_all_root="${BRORAY_ROOT:-/opt/broray}"
    summary_all_bundles="$summary_all_root/routes/bundles.json"
    summary_all_global="$summary_all_root/routes/installed/routes.json"
    summary_all_tmp="$summary_all_root/tmp/routes-summary-all.$$"

    [ -r "$summary_all_bundles" ] || {
        broray_routes_summary_error \
            "ROUTES_BUNDLES_UNAVAILABLE" \
            "Реестр наборов маршрутов недоступен."
        return 1
    }

    [ -r "$summary_all_global" ] || {
        broray_routes_summary_error \
            "ROUTES_GLOBAL_REGISTRY_UNAVAILABLE" \
            "Глобальный реестр маршрутов недоступен."
        return 1
    }

    mkdir -p "$summary_all_root/tmp" || return 1
    : >"$summary_all_tmp" || return 1

    for summary_all_bundle_id in $(
        jq -r '.bundles[]' "$summary_all_bundles"
    )
    do
        if ! broray_routes_summary "$summary_all_bundle_id" \
            >>"$summary_all_tmp"
        then
            rm -f "$summary_all_tmp"
            return 1
        fi
    done

    summary_all_route_count="$(
        jq -r '(.routes // []) | length' "$summary_all_global"
    )" || {
        rm -f "$summary_all_tmp"
        return 1
    }

    jq -s \
        --argjson installedRouteCount "$summary_all_route_count" \
        --arg updatedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        . as $bundles |
        ([$bundles[] | select(.recordedInstalled == true)] | length) as $recordedInstalled |
        ([$bundles[] | select(.verifiedInstalled == true)] | length) as $verifiedInstalled |
        ([$bundles[] | select(.actionRequired == true)] | length) as $attentionCount |
        ([$bundles[] | select(.healthState == "error")] | length) as $errorCount |
        ([$bundles[] | select(.healthState == "busy")] | length) as $busyCount |
        ([$bundles[] | select(.healthState == "warning")] | length) as $warningCount |
        (all($bundles[]; .registryConsistent == true)) as $registryConsistent |
        (all($bundles[]; .consistent == true)) as $fullyConsistent |
        (
            if $errorCount > 0 then "error"
            elif $busyCount > 0 then "busy"
            elif $warningCount > 0 or $attentionCount > 0 then "warning"
            else "ok" end
        ) as $severity |
        (
            [
                $bundles[] |
                if .error != null then {
                    code:(.error.code // "ROUTES_ERROR"),
                    message:(.error.message // "Ошибка набора маршрутов."),
                    details:.bundleId
                }
                elif .keeneticUpdateRequired == true then {
                    code:"KEENETIC_UPDATE_REQUIRED",
                    message:("Набор «" + .bundleId + "» подготовлен, но не подтверждён в Keenetic."),
                    details:.bundleId
                }
                elif .verificationRequired == true then {
                    code:"ROUTES_VERIFICATION_REQUIRED",
                    message:("Для набора «" + .bundleId + "» требуется проверка."),
                    details:.bundleId
                }
                elif .verificationFreshness.state == "expired" then {
                    code:"ROUTES_VERIFICATION_EXPIRED",
                    message:("Проверка набора «" + .bundleId + "» устарела."),
                    details:.bundleId
                }
                else empty end
            ]
        ) as $reasons |
        {
            schemaVersion: 1,
            statusContractVersion: 1,
            bundles: $bundles,
            availableBundles: ($bundles | length),
            installedBundles: $verifiedInstalled,
            verifiedInstalledBundles: $verifiedInstalled,
            recordedInstalledBundles: $recordedInstalled,
            installed: ($verifiedInstalled > 0),
            recordedInstalled: ($recordedInstalled > 0),
            downloaded: any($bundles[]; .downloaded == true),
            updatesAvailableCount: (
                [
                    $bundles[] |
                    select((.bundleId | startswith("user-") | not)) |
                    select(.updateAvailable == true)
                ] |
                length
            ),
            sourceUpdateAvailable: any(
                $bundles[];
                ((.bundleId | startswith("user-") | not) and
                 (.sourceUpdateAvailable == true))
            ),
            keeneticUpdateRequired: any($bundles[]; .keeneticUpdateRequired == true),
            operationRunning: any($bundles[]; .operationRunning == true),
            actionRequired: ($attentionCount > 0),
            attentionCount: $attentionCount,
            installedRouteCount: $installedRouteCount,
            healthy: ($severity == "ok"),
            registryConsistent: $registryConsistent,
            consistent: $fullyConsistent,
            error: (
                [
                    $bundles[] |
                    select(.error != null) |
                    {bundleId:.bundleId,details:.error}
                ] |
                first // null
            ),
            health: {
                schemaVersion: 1,
                module: "routes",
                availability: "available",
                severity: $severity,
                operational: ($errorCount == 0),
                consistent: $fullyConsistent,
                actionRequired: ($attentionCount > 0),
                freshness: {
                    state: (
                        if any($bundles[]; .verificationFreshness.state == "expired") then "expired"
                        elif any($bundles[]; .verificationFreshness.state == "stale") then "stale"
                        else "fresh" end
                    ),
                    checkedAt: (
                        [$bundles[].lastVerifiedAt | select(. != null)] |
                        sort |
                        last // null
                    )
                },
                reasons: $reasons,
                facts: {
                    availableBundles:($bundles|length),
                    recordedInstalledBundles:$recordedInstalled,
                    verifiedInstalledBundles:$verifiedInstalled,
                    attentionCount:$attentionCount,
                    installedRouteCount:$installedRouteCount
                },
                lastOperation: (
                    [$bundles[].lastOperation | select(. != null)] |
                    sort_by(.completedAt) |
                    last // null
                )
            },
            updatedAt: $updatedAt
        } |
        .updateAvailable = (.updatesAvailableCount > 0) |
        .state = (
            if .operationRunning then "busy"
            elif .health.severity == "error" then "error"
            elif .health.severity == "warning" then "attention"
            elif .updateAvailable then "update_available"
            elif .installed then "installed"
            else "empty" end
        )
    ' "$summary_all_tmp"
    summary_all_rc=$?
    rm -f "$summary_all_tmp"
    return "$summary_all_rc"
}
