#!/opt/bin/ash
# Bounded local reads only. No collectors, service actions or raw text fallback.
ops_report_file_safe()
{
    ops_file_safe "$1" "${2:-32768}" &&
      [ "$(readlink -f "$1")" = "$1" ] &&
      [ "$(find "$1" -maxdepth 0 -type f -links 1 -print 2>/dev/null)" = "$1" ]
}

ops_report_automation()
{
    local config directory file settings flags enabled complete count automatic subcomplete candidates
    config="$OPS_APP/config/system/server-auto-switch.json"
    settings='{"autoSwitch":null,"serverCheck":null}'
    complete=false
    if ops_report_file_safe "$config"; then
        flags="$(jq -ces 'select(length==1) | .[0] |
          select(type=="object" and (.enabled|type)=="boolean" and (.qualityRefreshEnabled|type)=="boolean") |
          {autoSwitch:.enabled,serverCheck:.qualityRefreshEnabled}' "$config" 2>/dev/null)" || flags=''
        if [ -n "$flags" ]; then settings="$flags"; complete=true; fi
    fi
    directory="$OPS_APP/config/subscriptions"
    enabled=false; count=0; automatic=0; subcomplete=true
    if [ ! -d "$directory" ] || [ -L "$directory" ] || [ "$(readlink -f "$directory")" != "$directory" ]; then
        subcomplete=false
    else
        # Reject a large catalog before opening records. Keep a diagnostic GET
        # bounded even on slow flash; callers can see the explicit partial result.
        candidates=0
        for file in "$directory"/*.json; do
            [ -e "$file" ] || [ -L "$file" ] || continue
            candidates=$((candidates+1))
            [ "$candidates" -le 32 ] || { subcomplete=false; break; }
        done
        if [ "$subcomplete" = true ]; then
        for file in "$directory"/*.json; do
            [ -e "$file" ] || [ -L "$file" ] || continue
            count=$((count+1))
            if ! ops_report_file_safe "$file" 65536; then subcomplete=false; continue; fi
            flags="$(jq -cs 'select(length==1) | .[0] |
              select(type=="object" and (.enabled|type)=="boolean" and (.autoUpdateEnabled|type)=="boolean") |
              (.enabled and .autoUpdateEnabled)' "$file" 2>/dev/null)" || flags=''
            case "$flags" in
              true) automatic=$((automatic+1)); enabled=true ;;
              false) ;;
              *) subcomplete=false ;;
            esac
        done
        fi
    fi
    [ "$subcomplete" = true ] || enabled=null
    jq -nc --argjson settings "$settings" --argjson complete "$complete" --argjson subcomplete "$subcomplete" \
      --argjson enabled "$enabled" --argjson count "$count" --argjson automatic "$automatic" '
      $settings+{subscriptionUpdate:$enabled,complete:($complete and $subcomplete),
        subscriptionRecordsRead:$count,automaticSubscriptions:(if $subcomplete then $automatic else null end),
        errors:((if $complete then [] else ["AUTOMATION_SETTINGS_UNAVAILABLE"] end)+
          (if $subcomplete then [] else ["SUBSCRIPTION_SETTINGS_UNAVAILABLE"] end))}'
}

ops_report_service()
(
    # Service setup selects actual /proc and unsets test identities. These
    # globals must never change the containing coordinator's owner context.
    BRORAY_ROOT="$OPS_APP"; BRORAY_STATE_ROOT="$OPS_STATE"
    . "$OPS_APP/lib/service-lifecycle.sh" || exit 1
    broray_service_setup "$1" || exit 1
    broray_service_status_json | jq -ce --arg service "$1" '
      select(.service==$service and (.complete|type)=="boolean") |
      {service:$service,
       state:(.state as $s | if ["starting","running","stopping","stopped","ambiguous"]|index($s) then $s else "ambiguous" end),
       running:(if (.running|type)=="boolean" then .running else null end),
       ready:(.ready==true),complete:.complete,
       errorCode:(.errorCode as $e | if ["SERVICE_IDENTITY_UNCONFIRMED","SERVICE_STOP_UNCONFIRMED"]|index($e) then $e else null end)}'
)

ops_report_services()
{
    local name data records
    records='[]'
    for name in subscriptions auto-switch connection-monitor home-snapshot interface-reconcile; do
        data="$(ops_report_service "$name" 2>/dev/null)" || data=''
        if [ -z "$data" ]; then
            data="$(jq -nc --arg name "$name" '{service:$name,state:"ambiguous",running:null,ready:false,complete:false,errorCode:"SERVICE_STATUS_UNAVAILABLE"}')" || return 1
        fi
        records="$(jq -nc --argjson records "$records" --argjson data "$data" '$records+[$data]')" || return 1
    done
    printf '%s\n' "$records"
}

# Read the already-collected Home cache without invoking its refresh path.
# Its PID is only a candidate; it is never proof that a service is running.
ops_report_home()
{
    local module file now
    module="$1"
    case "$module" in xray|broray) ;; *) return 1 ;; esac
    file="$OPS_APP/run/home-snapshots/$module.json"
    ops_report_file_safe "$file" 131072 || return 1
    now="$(date '+%s')"
    jq -ces -L "$OPS_APP/lib" --arg module "$module" --argjson now "$now" '
      include "operation-public"; include "operation-report-public";
      select(length==1) | .[0] |
      select(.schemaVersion==1 and .module==$module and (.data|type)=="object" and
        (.capturedEpoch|type)=="number" and .capturedEpoch==(.capturedEpoch|floor) and
        .capturedEpoch>0 and .capturedEpoch<=$now and ($now-.capturedEpoch)<=600 and
        (.capturedAt|timestamp)!=null) |
      {cache:{capturedAt:(.capturedAt|timestamp),ageSeconds:($now-.capturedEpoch),
        freshness:(if ($now-.capturedEpoch)<=90 then "fresh" else "stale" end)},
       data:(if $module=="xray" then
         {pid:(.data.pid|diagnostic_pid),version:(.data.version|version_value)}
       else .data.lastOperation | updater_public end)}' "$file" 2>/dev/null
}

ops_report_xray()
(
    local cached pid owner started after identity state
    cached="$(ops_report_home xray)" || cached='{"cache":null,"data":{"pid":null,"version":null}}'
    pid="$(printf '%s\n' "$cached" | jq -r '.data.pid // empty')"
    identity=null; state=unknown
    # Use the actual kernel and exact persistent executable/config pair. Do
    # not accept test owners, the cached running boolean, or a validator PID.
    OPS_PROC=/proc
    unset BRORAY_OPS_TEST_IDENTITIES
    BRORAY_XRAY_BINARY="$OPS_APP/runtime/xray"
    BRORAY_XRAY_CONFIG="$OPS_APP/config/config.json"
    . "$OPS_APP/lib/xray-process.sh" || exit 1
    if [ -n "$pid" ]; then
        owner="$(broray_ops_capture_owner "$pid")" || owner=''
        started="$(broray_xray_runtime_identity "$pid")" || started=''
        after="$(broray_ops_capture_owner "$pid")" || after=''
        if [ -n "$owner" ] && [ "$owner" = "$after" ] && [ -n "$started" ] &&
          [ "$(printf '%s\n' "$owner" | jq -r '.startTicks')" = "$started" ]; then
            identity="$(printf '%s\n' "$owner" | jq -c '{verified:true,pid,startTicks,sameBoot:true,role:"persistent-xray"}')"
            state=running
        fi
    fi
    jq -nc --argjson cached "$cached" --argjson identity "$identity" --arg state "$state" '
      {state:$state,identity:$identity,cachedVersion:$cached.data.version,cache:$cached.cache,
       complete:($identity!=null),errorCode:(if $identity==null then "XRAY_IDENTITY_UNCONFIRMED" else null end)}'
)

ops_report_updater()
{
    local cached
    cached="$(ops_report_home broray)" || cached='{"cache":null,"data":null}'
    jq -nc --argjson cached "$cached" '
      {lastOperation:$cached.data,cache:$cached.cache,liveState:"unknown",
       complete:($cached.data!=null),errorCode:(if $cached.data==null then "UPDATER_STATUS_UNAVAILABLE" else null end)}'
}
