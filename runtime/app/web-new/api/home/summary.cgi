#!/opt/bin/ash

# Home is a cache-only view. Module workers own collection and atomically
# publish snapshots; this request performs no router or service probes.

BRORAY_HOME_ROOT="${BRORAY_HOME_ROOT:-/opt/broray}"
PATH="$BRORAY_HOME_ROOT/bin:/opt/sbin:/opt/bin:/sbin:/bin:$PATH"
export PATH BRORAY_HOME_ROOT

. "$BRORAY_HOME_ROOT/web-new/api/auth-common.sh"
. "$BRORAY_HOME_ROOT/lib/home-snapshot.sh"

broray_api_require_method GET
broray_api_require_session

errors='[]'
xray='null'
servers='null'
subscriptions='null'
dns='null'
keenetic='null'
routes='null'
broray='null'

home_add_error()
{
    module_id="$1"
    errors="$(
        jq -nc \
            --argjson current "$errors" \
            --arg module "$module_id" \
            '$current + [$module]'
    )"
}

home_read_snapshot()
{
    snapshot_id="$1"
    broray_home_snapshot_read "$snapshot_id"
}

if ! xray="$(home_read_snapshot xray)"; then xray='null'; home_add_error xray; fi
if ! keenetic="$(home_read_snapshot keenetic)"; then keenetic='null'; home_add_error keenetic; fi
if ! servers="$(home_read_snapshot servers)"; then servers='null'; home_add_error servers; fi
if ! subscriptions="$(home_read_snapshot subscriptions)"; then subscriptions='null'; home_add_error subscriptions; fi
if ! dns="$(home_read_snapshot dns)"; then dns='null'; home_add_error dns; fi
if ! routes="$(home_read_snapshot routes)"; then routes='null'; home_add_error routes; fi
if ! broray="$(home_read_snapshot broray)"; then broray='null'; home_add_error broray; fi

# Home owns this presentation clarification.  The DNS-over-TLS engine and its
# health decision remain unchanged; only the ambiguous counter sentence is
# made explicit before Home aggregates module reasons.
if [ "$dns" != null ]; then
    dns="$(
        printf '%s\n' "$dns" |
            jq -c '
                . as $root |
                if ((.health.reasons // null) | type) == "array" then
                    .health.reasons |= map(
                        if .code == "DNS_TEST_REQUIRED" then
                            .message = (
                                "Проверено " +
                                (($root.selectedTestedCount // 0) | tostring) +
                                " из " +
                                (($root.selectedCount // 0) | tostring) +
                                " выбранных серверов DNS-over-TLS."
                            )
                        else . end
                    )
                else . end
            '
    )" || {
        dns='null'
        home_add_error dns
    }
fi

home_json="$(
    jq -nc \
        --argjson xray "$xray" \
        --argjson servers "$servers" \
        --argjson subscriptions "$subscriptions" \
        --argjson dns "$dns" \
        --argjson keenetic "$keenetic" \
        --argjson routes "$routes" \
        --argjson broray "$broray" \
        --argjson errors "$errors" \
        --arg updatedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" '
        def rank:
            if . == "error" then 50
            elif . == "busy" then 40
            elif . == "warning" then 30
            elif . == "unknown" then 20
            elif . == "ok" then 10
            else 20 end;
        def unavailable_health($module; $message): {
            schemaVersion:1,
            module:$module,
            availability:"unavailable",
            severity:"error",
            operational:false,
            consistent:false,
            actionRequired:true,
            freshness:{state:"unknown",checkedAt:null},
            reasons:[{code:"MODULE_SNAPSHOT_UNAVAILABLE",message:$message,details:null}],
            facts:{},
            lastOperation:null
        };
        def contract_health($module): {
            schemaVersion:1,
            module:$module,
            availability:"partial",
            severity:"unknown",
            operational:false,
            consistent:false,
            actionRequired:true,
            freshness:{state:"unknown",checkedAt:null},
            reasons:[{code:"STATUS_CONTRACT_MISSING",message:"Снимок модуля не содержит единого контракта состояния.",details:null}],
            facts:{},
            lastOperation:null
        };
        def snapshot_health($module; $value):
            if $value == null then
                unavailable_health($module; "Валидный снимок модуля ещё не опубликован.")
            else
                (if (($value.health // null) | type) == "object"
                 then $value.health
                 else contract_health($module) end) as $base |
                ($value._snapshot.freshness // "unknown") as $state |
                ($value._snapshot.capturedAt // null) as $capturedAt |
                ($value._snapshot.ageSeconds // null) as $age |
                if $state == "fresh" then
                    $base | .freshness = {state:"fresh",checkedAt:$capturedAt}
                elif ($state == "stale" or $state == "expired") then
                    $base |
                    .severity = (if ((.severity // "unknown") | rank) < ("warning" | rank) then "warning" else (.severity // "unknown") end) |
                    .actionRequired = true |
                    .freshness = {state:$state,checkedAt:$capturedAt} |
                    .reasons = ((.reasons // []) + [{
                        code:(if $state == "expired" then "MODULE_SNAPSHOT_EXPIRED" else "MODULE_SNAPSHOT_STALE" end),
                        message:(if $state == "expired" then "Показано последнее известное состояние; фоновое обновление давно не завершалось."
                                 else "Показано последнее известное состояние; фоновое обновление задерживается." end),
                        details:{capturedAt:$capturedAt,ageSeconds:$age}
                    }])
                else
                    $base |
                    .severity = (if ((.severity // "unknown") | rank) < ("warning" | rank) then "warning" else (.severity // "unknown") end) |
                    .actionRequired = true |
                    .freshness = {state:"unknown",checkedAt:$capturedAt}
                end
            end;

        [
            {id:"xray", health:snapshot_health("xray"; $xray)},
            {id:"servers", health:snapshot_health("servers"; $servers)},
            {id:"subscriptions", health:snapshot_health("subscriptions"; $subscriptions)},
            {id:"dns", health:snapshot_health("dns"; $dns)},
            {id:"routes", health:snapshot_health("routes"; $routes)},
            {id:"keenetic", health:snapshot_health("keenetic"; $keenetic)},
            {id:"broray", health:snapshot_health("broray"; $broray)}
        ] as $modules |
        ($modules | map(.health.severity // "unknown") | max_by(rank)) as $severity |
        ($modules | map(select((.health.severity // "unknown") != "ok") | {
            module:.id,
            severity:(.health.severity // "unknown"),
            actionRequired:(.health.actionRequired // true),
            reasons:(.health.reasons // [])
        })) as $issues |
        {
            schemaVersion:3,
            xray:$xray,
            servers:$servers,
            subscriptions:$subscriptions,
            dns:$dns,
            keenetic:$keenetic,
            routes:$routes,
            broray:$broray,
            snapshotRuntime:{readOnly:true,collectorProcessesStarted:0,aggregateLock:false},
            collectorRuntime:{mode:"snapshot-only",maxConcurrentOperations:0,budgetSeconds:0,elapsedSeconds:0},
            modules:$modules,
            errors:$errors,
            issues:$issues,
            health:{
                schemaVersion:1,
                module:"home",
                availability:(if ($errors|length)==0 then "available" else "partial" end),
                severity:$severity,
                operational:([$modules[].health.operational // false] | all),
                consistent:([$modules[].health.consistent // false] | all),
                actionRequired:([$modules[].health.actionRequired // true] | any),
                freshness:{
                    state:(if any($modules[].health.freshness.state; . == "expired") then "expired"
                           elif any($modules[].health.freshness.state; . == "stale") then "stale"
                           elif all($modules[].health.freshness.state; . == "fresh") then "fresh"
                           else "unknown" end),
                    checkedAt:$updatedAt
                },
                reasons:([$issues[] as $issue | $issue.reasons[]? | . + {module:$issue.module}]),
                facts:{moduleCount:($modules|length),unavailableCount:($errors|length),issueCount:($issues|length)},
                lastOperation:null
            },
            healthy:($severity == "ok"),
            updatedAt:$updatedAt
        }
    '
)" || {
    broray_api_error \
        "500 Internal Server Error" \
        "HOME_SUMMARY_FAILED" \
        "Не удалось прочитать сохранённые состояния модулей."
}

broray_api_success "$home_json"
