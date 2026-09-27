#!/opt/bin/ash
# Observations are cooperative. Only the admitted global failover activation
# may change the persistent runtime, through the existing activation helper.
broray_active_proxy_context() (
    local config binary pid start digest active
    config="$(broray_xray_config_path)" || return 1
    binary="$(broray_xray_binary_path)" || return 1
    [ -f "$config" ] && [ ! -L "$config" ] || return 1
    active="$(cat "$BRORAY_ACTIVE_SERVER_FILE" 2>/dev/null)" || return 1
    [ -n "$active" ] && [ "$active" = "$1" ] || return 1
    pid="$(broray_xray_pid)" || return 1
    BRORAY_XRAY_BINARY="$binary"; BRORAY_XRAY_CONFIG="$config"
    start="$(broray_xray_runtime_identity "$pid")" || return 1
    digest="$(sha256sum "$config")" || return 1
    printf '%s\n%s\n%s\n%s\n' "$active" "$pid:$start" "$config" "$digest" | sha256sum | awk '{print $1}'
)

broray_active_proxy_measure()
{
    local active_before active_after active_dir active_rc active_config active_id
    active_id="$1"
    ACTIVE_PROXY_HEALTH=unknown; ACTIVE_PROXY_CONTEXT=''
    ACTIVE_PROXY_RESULT='{"method":"current-socks-https","status":"unknown","errorCode":"ACTIVE_CONTEXT_UNAVAILABLE"}'
    broray_job_checkpoint checking || return $?
    active_before="$(broray_active_proxy_context "$active_id")" || return 0
    active_config="$(broray_xray_config_path)" || return 0
    active_dir="$(mktemp -d "$BRORAY_BASE/tmp/active-health-$BRORAY_BACKGROUND_OPERATION_ID-XXXXXX")" || return 1
    chmod 700 "$active_dir" || return 1
    active_rc=0
    broray_ops_run_helper 25 -- "${BRORAY_OPS_ASH:-/opt/bin/ash}" \
      "$BRORAY_BASE/lib/active-proxy-probe.sh" "$active_config" "$active_id" \
      >"$active_dir/result.json" 2>"$active_dir/error" || active_rc=$?
    if [ "$active_rc" = 75 ]; then BRORAY_JOB_UNRESOLVED=true; return 75; fi
    case "$active_rc" in 0|1|2) ;; *) rm -rf "$active_dir"; return "$active_rc" ;; esac
    active_after="$(broray_active_proxy_context "$active_id")" || active_after=''
    if [ "$active_after" = "$active_before" ] &&
      jq -e --arg id "$active_id" --argjson rc "$active_rc" '
        type=="object" and .method=="current-socks-https" and .serverId==$id and
        .status==(if $rc==0 then "healthy" elif $rc==1 then "unhealthy" else "unknown" end) and
        (.checkedAt|type)=="string" and (.checkedEpoch|type)=="number"
      ' "$active_dir/result.json" >/dev/null 2>&1; then
        ACTIVE_PROXY_RESULT="$(jq -c --arg context "$active_before" '. + {context:$context}' "$active_dir/result.json")"
        ACTIVE_PROXY_CONTEXT="$active_before"
        case "$active_rc" in 0) ACTIVE_PROXY_HEALTH=true ;; 1) ACTIVE_PROXY_HEALTH=false ;; esac
    else
        ACTIVE_PROXY_RESULT='{"method":"current-socks-https","status":"unknown","errorCode":"ACTIVE_CONTEXT_CHANGED_OR_INCOMPLETE"}'
    fi
    rm -rf "$active_dir" || return 1
}

# Read-only presentation of a completed, context-bound SOCKS measurement.
# ICMP and isolated candidate checks cannot authorize a connected badge.
broray_active_proxy_cached() {
    local id context file now rows
(
    id="$1"; shift
    context="$(broray_active_proxy_context "$id" 2>/dev/null)" || context=""
    now="$(date '+%s')"
    rows="$({
        for file in "$@"; do
            [ -f "$file" ] && [ ! -L "$file" ] || continue
            jq -c '.activeHealth // empty' "$file" 2>/dev/null || true
        done
    })"
    printf '%s\n' "$rows" | jq -sc --arg id "$id" --arg context "$context" --argjson now "$now" '
      [ .[] | select(type=="object" and .method=="current-socks-https" and
        .serverId==$id and $context!="" and .context==$context and
        (.checkedEpoch|type)=="number" and .checkedEpoch<=$now and
        ($now-.checkedEpoch)<=120 and (.status=="healthy" or .status=="unhealthy")) ] |
      (max_by(.checkedEpoch) // {method:"current-socks-https",status:"unknown",errorCode:"ACTIVE_HEALTH_NOT_CURRENT"})'
)
}

# One current-proxy sample per admitted observer. Quality progress and failover
# history are not copied back from this worker's earlier cache snapshot.
broray_auto_health_step()
{
    local request directory active context settings config_hash enabled threshold previous count status reason payload health_rc nonce response
    request="$1"
    . "${BRORAY_OPS_CODE_ROOT:-${BRORAY_ROOT:-/opt/broray}}/lib/server-service.sh" || return 74
    broray_job_require_owner || return $?
    directory="$(broray_ops_operation_directory)" || return 74
    active="$(jq -er .bundleId "$directory/state.json")" || return 74
    context="$(jq -er .queueStep.context "$directory/state.json")" || return 74
    [ "$(broray_active_proxy_context "$active")" = "$context" ] || return 76
    settings="$BRORAY_BASE/config/system/server-auto-switch.json"
    [ -f "$settings" ] && [ ! -L "$settings" ] || return 74
    config_hash="$(sha256sum "$settings" | cut -d ' ' -f 1)" || return 74
    enabled="$(jq -er '(.enabled // false) | if .==true or .=="true" then "true" else "false" end' "$settings")" || return 74
    threshold="$(jq -er '(.failureThreshold // 3) | tostring |
      if length>0 and all(explode[];.>=48 and .<=57) then tonumber | if .>=1 and .<=10 then . else 3 end else 3 end' "$settings")" || return 74
    previous='{}'
    if [ -e "$BRORAY_BASE/run/server-auto-switch-state.json" ]; then
        [ ! -L "$BRORAY_BASE/run/server-auto-switch-state.json" ] || return 74
        previous="$(jq -ce 'select(type=="object")' "$BRORAY_BASE/run/server-auto-switch-state.json")" || return 74
    fi
    health_rc=0
    broray_active_proxy_measure "$active" || health_rc=$?
    [ "$health_rc" = 0 ] || return "$health_rc"
    [ "$ACTIVE_PROXY_CONTEXT" = "$context" ] || return 76
    count=0; status=healthy; reason="Проверка трафика через активный VPN успешна"
    if [ "$enabled" = false ]; then
        status=disabled; reason="Автовыбор выключен"
    elif [ "$ACTIVE_PROXY_HEALTH" = false ]; then
        count="$(printf '%s\n' "$previous" | jq -er --arg context "$context" '
          if .lastProxyContext==$context and (.consecutiveFailures|type)=="number" and
            .consecutiveFailures>=0 and (.consecutiveFailures|floor)==.consecutiveFailures then
            [(.consecutiveFailures+1),2147483647]|min else 1 end')" || return 74
        status=waiting-threshold
        reason="Ожидание порога ошибок: $count из $threshold"
        [ "$count" -lt "$threshold" ] || reason="Порог отказов достигнут; ожидается проверка резервных серверов"
    elif [ "$ACTIVE_PROXY_HEALTH" != true ]; then
        status=paused; reason="Не удалось проверить трафик через активный VPN"
    fi
    directory="${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}/requests/$request"
    [ -d "$directory" ] && [ ! -L "$directory" ] || return 74
    payload="$directory/health-$BRORAY_BACKGROUND_OPERATION_ID.json"
    [ ! -e "$payload" ] && [ ! -L "$payload" ] || return 74
    (set -C; jq -nc --arg op "$BRORAY_BACKGROUND_OPERATION_ID" --argjson enabled "$enabled" \
      --arg status "$status" --arg active "$active" --argjson health "$ACTIVE_PROXY_RESULT" \
      --argjson count "$count" --arg context "$context" --arg reason "$reason" --arg hash "$config_hash" '
      {schemaVersion:3,backgroundOperationId:$op,enabled:$enabled,status:$status,activeServerId:$active,
       activeHealth:$health,consecutiveFailures:$count,lastProxyContext:(if $count>0 then $context else null end),
       lastEvaluationAt:$health.checkedAt,lastEvaluationEpoch:$health.checkedEpoch,
       lastReason:$reason,lastError:null,autoConfigSha256:$hash}' >"$payload") || return 74
    chmod 600 "$payload" || return 74
    broray_job_publish_json active-state '' "$payload" || return $?
    if [ "$enabled" = true ] && [ "$count" -ge "$threshold" ]; then
        nonce="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || return 74
        response="$(broray_ops_queue_submit servers:failover "$active" AUTO_SWITCH "$context" "$nonce")" || {
            # A lost submit reply is resolved only through its original nonce.
            response="$(broray_ops_queue_lookup "$nonce")" || return 75
        }
        printf '%s\n' "$response" | jq -es 'length==1 and .[0].ok==true and .[0].priority==1 and
          (. [0].state|IN("queued","running"))' >/dev/null || return 75
    fi
    return 0
}

# Existing service cycles use this producer to admit due work. It grants no
# resource and starts no helper; a finite worker must still claim and ack.
broray_auto_enqueue_request()
{
    local nonce response rc known
    known="${6:-}"
    if broray_ops_request_id_valid "$known"; then
        # A service remembers only its own accepted request. The coordinator
        # still supplies its current state on every tick; this is no owner or
        # readiness cache. Do not append new nonces for the same pending work.
        rc=0
        response="$(broray_ops_queue_lookup "${known#q-}")" || rc=$?
        if [ "$rc" = 0 ]; then
            response="$(printf '%s\n' "$response" | jq -ce --arg id "$known" --argjson priority "$5" '
              select(.ok==true and .requestId==$id and .priority==$priority and
                (.state|IN("queued","running","completed","cancelled","failed")))')" || return 75
            if printf '%s\n' "$response" | jq -e '.state=="queued" or .state=="running"' >/dev/null; then
                printf '%s\n' "$response"
                return 0
            fi
        elif ! printf '%s\n' "$response" | jq -e '.ok==false and .errorCode=="REQUEST_UNCONFIRMED"' >/dev/null; then
            # Lost/corrupt state is not evidence that a new admission is safe.
            printf '%s\n' "$response" >&2
            return "$rc"
        fi
    fi
    nonce="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || return 74
    rc=0
    response="$(broray_ops_queue_submit "$1" "$2" "$3" "$4" "$nonce")" || rc=$?
    if [ "$rc" != 0 ]; then
        if printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1; then
            printf '%s\n' "$response" >&2
            return "$rc"
        fi
        response="$(broray_ops_queue_lookup "$nonce")" || return 75
    fi
    printf '%s\n' "$response" | jq -ce --argjson priority "$5" '
      select(.ok==true and .priority==$priority and (.requestId|type)=="string" and
        (.state|IN("queued","running","completed","cancelled","failed")))' || return 75
}

# Coalesce only periodic observations of the same already-failed connection
# while its bound failover is pending. Manual P0 requests remain independent.
# The existing 120-second evidence lifetime is not extended by this decision.
broray_auto_failover_pending()
{
    local pending response rc
    pending="$(printf '%s\n' "$1" | jq -er --arg active "$2" --arg context "$3" \
      --arg hash "$4" --argjson now "$5" '
        . as $state |
        [.activeHealth,.failover.activeHealth] | map(select(type=="object") |
          select(.serverId==$active and .context==$context and .method=="current-socks-https" and
            (.status|IN("healthy","unhealthy")) and (.checkedEpoch|type)=="number" and
            .checkedEpoch<=$now and ($now-.checkedEpoch)<=120)) |
        max_by([.checkedEpoch,(if .status=="healthy" then 1 else 0 end)]) as $health |
        $state | select(.autoConfigSha256==$hash and $health.status=="unhealthy" and
          .failover.serverId==$active and .failover.sourceContext==$context and
          .failover.autoConfigSha256==$hash and (.failover.status|IN("probing","preparing","prepared"))) |
        .failover.requestId // empty')" || return 1
    broray_ops_request_id_valid "$pending" || return 74
    rc=0
    response="$(broray_ops_queue_lookup "${pending#q-}")" || rc=$?
    if [ "$rc" != 0 ]; then
        if printf '%s\n' "$response" | jq -e '.ok==false and .errorCode=="REQUEST_UNCONFIRMED"' >/dev/null; then
            return 1
        fi
        printf '%s\n' "$response" >&2
        return "$rc"
    fi
    printf '%s\n' "$response" | jq -e --arg id "$pending" '
      .ok==true and .requestId==$id and .priority==1 and
      (.state|IN("queued","running","completed","failed","cancelled"))' >/dev/null || return 75
    printf '%s\n' "$response" | jq -e '.state=="queued" or .state=="running"' >/dev/null
}

broray_auto_enqueue_due()
{
    local settings cache pause config_hash state active context now interval health quality dispatch known pending_rc
    dispatch="${1:-}"
    case "$dispatch" in ''|continue) ;; *) return 64 ;; esac
    settings="$BRORAY_BASE/config/system/server-auto-switch.json"
    cache="$BRORAY_BASE/run/server-auto-switch-state.json"
    pause="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/background-automation.json"
    if [ -e "$pause" ] || [ -L "$pause" ]; then
        [ -f "$pause" ] && [ ! -L "$pause" ] &&
          jq -e 'type=="object" and (.paused|type)=="boolean"' "$pause" >/dev/null || return 74
        if jq -e '.paused==true' "$pause" >/dev/null; then
            printf '%s\n' '{"ok":true,"paused":true,"health":null,"quality":null}'
            return 0
        fi
    fi
    [ -f "$settings" ] && [ ! -L "$settings" ] &&
      jq -e 'type=="object"' "$settings" >/dev/null || return 74
    config_hash="$(sha256sum "$settings" | cut -d ' ' -f 1)" || return 74
    state='{}'
    if [ -e "$cache" ] || [ -L "$cache" ]; then
        [ -f "$cache" ] && [ ! -L "$cache" ] || return 74
        state="$(jq -ce 'select(type=="object")' "$cache")" || return 74
    fi
    now="$(date '+%s')" || return 74
    health=null; quality=null
    active="$(cat "$BRORAY_ACTIVE_SERVER_FILE" 2>/dev/null)" || active=''
    # A stopped or unconfirmed persistent Xray creates no failover request.
    # Isolated quality checks remain independent of manual VPN-off.
    if [ -n "$active" ] && context="$(broray_active_proxy_context "$active" 2>/dev/null)"; then
        if ! printf '%s\n' "$state" | jq -e --arg active "$active" --arg context "$context" \
          --arg hash "$config_hash" --argjson now "$now" '
            .autoConfigSha256==$hash and .activeHealth.serverId==$active and
            .activeHealth.context==$context and .activeHealth.method=="current-socks-https" and
            (.activeHealth.checkedEpoch|type)=="number" and .activeHealth.checkedEpoch<=$now and
            ($now-.activeHealth.checkedEpoch)<15' >/dev/null; then
            pending_rc=0
            broray_auto_failover_pending "$state" "$active" "$context" "$config_hash" "$now" || pending_rc=$?
            case "$pending_rc" in 0|1) ;; *) return "$pending_rc" ;; esac
            if [ "$pending_rc" = 1 ]; then
            known=''
            [ "${BRORAY_AUTO_HEALTH_CONTEXT:-}" != "$context:$config_hash" ] ||
              known="${BRORAY_AUTO_HEALTH_REQUEST:-}"
            health="$(broray_auto_enqueue_request servers:active-health "$active" AUTO_SWITCH "$context" 0 "$known")" || return $?
            BRORAY_AUTO_HEALTH_REQUEST="$(printf '%s\n' "$health" | jq -er .requestId)" || return 75
            BRORAY_AUTO_HEALTH_CONTEXT="$context:$config_hash"
            # Do not make an admitted P0 wait for the lower-priority catalog
            # scan/admission. The worker still must win the canonical claim.
            if [ "$dispatch" = continue ] &&
              printf '%s\n' "$health" | jq -e '.state=="queued"' >/dev/null; then
                broray_job_dispatch_step continue || return $?
            fi
            fi
        fi
    fi
    if jq -e '.qualityRefreshEnabled==true' "$settings" >/dev/null; then
        interval="$(jq -r '.qualityRefreshIntervalMinutes // 60' "$settings")" || return 74
        case "$interval" in 30|60|180|360) ;; *) interval=60 ;; esac
        if ! printf '%s\n' "$state" | jq -e --argjson interval "$interval" --argjson now "$now" '
          .qualityRefresh.intervalMinutes==$interval and
          (.qualityRefresh.nextCheckEpoch|type)=="number" and .qualityRefresh.nextCheckEpoch>$now and
          (.qualityRefresh.status|IN("success","scheduled","partial","error"))' >/dev/null; then
            context="$(broray_quality_context all SERVER_CHECK_AUTO)" || return $?
            known=''
            [ "${BRORAY_AUTO_QUALITY_CONTEXT:-}" != "$context" ] ||
              known="${BRORAY_AUTO_QUALITY_REQUEST:-}"
            quality="$(broray_auto_enqueue_request servers:quality all SERVER_CHECK_AUTO "$context" 3 "$known")" || return $?
            BRORAY_AUTO_QUALITY_REQUEST="$(printf '%s\n' "$quality" | jq -er .requestId)" || return 75
            BRORAY_AUTO_QUALITY_CONTEXT="$context"
        fi
    fi
    jq -nc --argjson health "$health" --argjson quality "$quality" \
      '{ok:true,paused:false,health:$health,quality:$quality}'
}

# P1 starts with a fresh active-proxy sample. A previously failed P0 sample is
# admission evidence, never permission to switch a connection that recovered.
broray_auto_failover_step()
{
    local f_request f_stage f_owner f_active f_context f_directory f_result f_settings f_hash
    local f_policy f_snapshot f_state f_now f_status f_digest
    local f_cursor f_id f_node_hash f_output f_rc f_candidate f_selected f_tmp f_health
    local f_prepare f_runtime f_runtime_hash f_runtime_mode f_system f_system_hash f_binary f_binary_hash f_new_hash
    f_request="$1"; f_stage="$2"
    . "${BRORAY_OPS_CODE_ROOT:-${BRORAY_ROOT:-/opt/broray}}/lib/server-service.sh" || return 74
    broray_job_require_owner || return $?
    f_owner="$(broray_ops_operation_directory)" || return 74
    f_active="$(jq -er .bundleId "$f_owner/state.json")" || return 74
    f_context="$(jq -er .queueStep.context "$f_owner/state.json")" || return 74
    [ "$(broray_active_proxy_context "$f_active")" = "$f_context" ] || return 76
    f_directory="${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}/requests/$f_request"
    [ -d "$f_directory" ] && [ ! -L "$f_directory" ] || return 74
    f_result="$f_directory/result.json"
    f_settings="$BRORAY_BASE/config/system/server-auto-switch.json"
    [ -f "$f_settings" ] && [ ! -L "$f_settings" ] || return 74
    f_hash="$(sha256sum "$f_settings" | cut -d ' ' -f 1)" || return 74
    case "$f_stage" in
      verify)
        [ ! -e "$f_result" ] && [ ! -L "$f_result" ] || return 76
        f_now="$(date '+%s')"
        f_policy="$(jq -ce --arg hash "$f_hash" --arg context "$f_context" --arg active "$f_active" \
          --argjson now "$f_now" --slurpfile config "$f_settings" '
          def bounded($fallback;$lo;$hi):
            tostring | if length>0 and all(explode[];.>=48 and .<=57) then tonumber |
              if .>=$lo and .<=$hi then . else $fallback end else $fallback end;
          $config[0] as $c | ($c.failureThreshold // 3 | bounded(3;1;10)) as $threshold |
          ($c.cooldownMinutes // 10 | bounded(10;1;120)) as $cooldown |
          select(($c.enabled==true or $c.enabled=="true") and .enabled==true and
            .autoConfigSha256==$hash and .activeServerId==$active and .lastProxyContext==$context and
            .activeHealth.context==$context and .activeHealth.status=="unhealthy" and
            (.consecutiveFailures|type)=="number" and .consecutiveFailures>=$threshold) |
          (.failover.lastAttemptEpoch // .lastAttemptEpoch // 0) as $attempt |
          (.lastSwitchEpoch // 0) as $switched |
          select(($attempt|type)=="number" and $attempt>=0 and $attempt<=$now and
            ($switched|type)=="number" and $switched>=0 and $switched<=$now) |
          {failureThreshold:$threshold,cooldownMinutes:$cooldown,lastAttemptEpoch:$attempt,
           attemptGuard:($attempt>0 and ($now-$attempt)<60),
           cooldown:($switched>0 and ($now-$switched)<($cooldown*60)),
           minimumRank:({"excellent":4,"good":3,"acceptable":2,"poor":1}[$c.minimumRating // "acceptable"] // 2),
           rule:(if ($c.selectionRule|IN("best-quality","lowest-ping","preferred")) then $c.selectionRule else "best-quality" end),
           preferredServerId:($c.preferredServerId // "")}' "$BRORAY_BASE/run/server-auto-switch-state.json")" || return 76
        if [ "$(printf '%s\n' "$f_policy" | jq -r '.attemptGuard or .cooldown')" = true ]; then return 0; fi
        broray_active_proxy_measure "$f_active" || return $?
        [ "$ACTIVE_PROXY_CONTEXT" = "$f_context" ] &&
          [ "$(sha256sum "$f_settings" | cut -d ' ' -f 1)" = "$f_hash" ] || return 76
        case "$ACTIVE_PROXY_HEALTH" in true) f_status=recovered ;; false) f_status=probing ;; *) return 76 ;; esac
        f_snapshot='[]'
        if [ "$f_status" = probing ]; then
            f_snapshot="$(broray_quality_snapshot all)" || return $?
            f_snapshot="$(printf '%s\n' "$f_snapshot" | jq -c --arg active "$f_active" --argjson policy "$f_policy" '
              map(select(.id!=$active)) |
              if $policy.rule=="preferred" and $policy.preferredServerId!="" then
                sort_by(if .id==$policy.preferredServerId then 0 else 1 end) else . end')" || return 74
        fi
        f_state="$(jq -nc --arg context "$f_context" --arg active "$f_active" --arg hash "$f_hash" \
          --arg status "$f_status" --argjson policy "$f_policy" --argjson servers "$f_snapshot" \
          --argjson health "$ACTIVE_PROXY_RESULT" --argjson attempt "$(date '+%s')" '
          {schemaVersion:1,kind:"failover",context:$context,activeServerId:$active,autoConfigSha256:$hash,
           status:(if $status=="probing" and ($servers|length)==0 then "no-candidates" else $status end),
           policy:$policy,servers:$servers,cursor:0,candidates:[],activeHealth:$health,
           attemptEpoch:(if $status=="probing" then $attempt else $policy.lastAttemptEpoch end)}')"
        (set -C; printf '%s\n' "$f_state" >"$f_result") || return 74
        chmod 600 "$f_result" || return 74
        broray_auto_failover_progress "$f_request" "$f_state" "$f_directory" || return $?
        [ "$f_status" = probing ] || return 0
        [ "$(printf '%s\n' "$f_snapshot" | jq length)" -gt 0 ] || return 0
        f_digest="$(sha256sum "$f_result" | cut -d ' ' -f 1)" || return 74
        broray_job_yield probe "$f_digest" ;;
      probe|activate)
        f_digest="$(jq -er .queueStep.resultSha256 "$f_owner/state.json")" || return 76
        [ -f "$f_result" ] && [ ! -L "$f_result" ] &&
          [ "$(stat -c '%u:%a:%h' "$f_result")" = "$(id -u):600:1" ] &&
          [ "$(sha256sum "$f_result" | cut -d ' ' -f 1)" = "$f_digest" ] || return 76
        f_state="$(jq -ce --arg context "$f_context" --arg active "$f_active" --arg hash "$f_hash" '
          select(.schemaVersion==1 and .kind=="failover" and .context==$context and
            .activeServerId==$active and .autoConfigSha256==$hash and
            (.servers|type)=="array" and (.candidates|type)=="array" and
            (.cursor|type)=="number" and .cursor>=0 and (.cursor|floor)==.cursor and .cursor<=(.servers|length))' "$f_result")" || return 76
        f_status="$(printf '%s\n' "$f_state" | jq -r .status)"
        case "$f_stage:$f_status" in probe:probing|probe:preparing|activate:prepared) ;; *) return 74 ;; esac
        # Prefer the newest observation of this exact runtime. Old evidence or
        # recovered health cannot authorize even the next candidate probe.
        f_now="$(date '+%s')"
        f_health="$(printf '%s\n' "$f_state" | jq -ce --arg context "$f_context" --arg active "$f_active" \
          --argjson now "$f_now" --slurpfile cache "$BRORAY_BASE/run/server-auto-switch-state.json" '
          [.activeHealth,$cache[0].activeHealth] | map(select(.context==$context and .serverId==$active and
            .method=="current-socks-https" and (.checkedEpoch|type)=="number" and
            .checkedEpoch<=$now and ($now-.checkedEpoch)<=120 and (.status|IN("healthy","unhealthy")))) |
          max_by(.checkedEpoch) | select(.!=null)')" || return 76
        [ "$(printf '%s\n' "$f_health" | jq -r .status)" = unhealthy ] || return 0
        if [ "$f_stage" = activate ]; then
            f_rc=0
            broray_auto_failover_activate "$f_state" "$f_directory" || f_rc=$?
            case "$f_rc" in 0|1) ;; *) return "$f_rc" ;; esac
            # Switching is not settled until its cache/history publication is
            # confirmed. A lost reply must retain the protected global fence.
            BRORAY_JOB_UNRESOLVED=true
            broray_auto_failover_complete "$f_request" "$f_directory" || return $?
            BRORAY_JOB_UNRESOLVED=false
            return "$f_rc"
        fi
        if [ "$f_status" = preparing ]; then
            f_id="$(printf '%s\n' "$f_state" | jq -er .selected.id)" || return 76
            f_node_hash="$(printf '%s\n' "$f_state" | jq -er .selected.sha256)" || return 76
            f_output="$(broray_server_path "$f_id")" || return 76
            [ -f "$f_output" ] && [ ! -L "$f_output" ] &&
              [ "$(sha256sum "$f_output" | cut -d ' ' -f 1)" = "$f_node_hash" ] || return 76
            f_runtime="$(broray_xray_config_path)" || return 76
            f_runtime_hash="$(sha256sum "$f_runtime" | cut -d ' ' -f 1)" || return 76
            f_runtime_mode="$(stat -c %a "$f_runtime")" || return 76
            f_system="$BRORAY_BASE/config/system/settings.json"
            [ -f "$f_system" ] && [ ! -L "$f_system" ] || return 76
            f_system_hash="$(sha256sum "$f_system" | cut -d ' ' -f 1)" || return 76
            f_binary="$(broray_xray_binary_path)" || return 76
            f_binary_hash="$(sha256sum "$f_binary" | cut -d ' ' -f 1)" || return 76
            [ ! -e "$f_directory/previous-runtime.json" ] && [ ! -L "$f_directory/previous-runtime.json" ] &&
              [ ! -e "$f_directory/new-runtime.json" ] && [ ! -L "$f_directory/new-runtime.json" ] || return 76
            (set -C; cat "$f_runtime" >"$f_directory/previous-runtime.json") || return 74
            chmod 600 "$f_directory/previous-runtime.json" || return 74
            [ "$(sha256sum "$f_directory/previous-runtime.json" | cut -d ' ' -f 1)" = "$f_runtime_hash" ] || return 76
            f_prepare="$(mktemp -d "$BRORAY_BASE/tmp/server-activate-$BRORAY_BACKGROUND_OPERATION_ID-XXXXXX")" || return 74
            chmod 700 "$f_prepare" || return 74
            printf '%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >"$f_prepare/operation-id" || return 74
            f_rc=0
            BRORAY_XRAY_BINARY="$f_binary" broray_ops_run_helper 30 -- "${BRORAY_OPS_ASH:-/opt/bin/ash}" \
              "$BRORAY_BASE/lib/server-activate-prepare.sh" "$f_prepare" "$f_id" \
              >"$f_directory/prepare-output" 2>"$f_directory/prepare-error" || f_rc=$?
            if [ "$f_rc" = 75 ]; then BRORAY_JOB_UNRESOLVED=true; return 75; fi
            [ "$f_rc" = 0 ] || return "$f_rc"
            [ "$(broray_active_proxy_context "$f_active")" = "$f_context" ] &&
              [ "$(sha256sum "$f_settings" | cut -d ' ' -f 1)" = "$f_hash" ] &&
              [ "$(sha256sum "$f_system" | cut -d ' ' -f 1)" = "$f_system_hash" ] &&
              [ "$(sha256sum "$f_output" | cut -d ' ' -f 1)" = "$f_node_hash" ] &&
              [ "$(sha256sum "$f_binary" | cut -d ' ' -f 1)" = "$f_binary_hash" ] || return 76
            [ -f "$f_prepare/config.json" ] && [ ! -L "$f_prepare/config.json" ] || return 74
            mv "$f_prepare/config.json" "$f_directory/new-runtime.json" || return 74
            f_new_hash="$(sha256sum "$f_directory/new-runtime.json" | cut -d ' ' -f 1)" || return 74
            # Config generation and its helper drain can outlive the earlier
            # failure sample. Recheck while still cooperative, before claiming
            # the short protected activation. Never extend old evidence.
            broray_active_proxy_measure "$f_active" || return $?
            [ "$ACTIVE_PROXY_CONTEXT" = "$f_context" ] &&
              [ "$(sha256sum "$f_settings" | cut -d ' ' -f 1)" = "$f_hash" ] || return 76
            case "$ACTIVE_PROXY_HEALTH" in false) f_status=prepared ;; true) f_status=recovered ;; *) return 76 ;; esac
            f_state="$(printf '%s\n' "$f_state" | jq -c --arg old "$f_runtime_hash" --arg new "$f_new_hash" \
              --arg mode "$f_runtime_mode" --arg system "$f_system_hash" --arg binary "$f_binary_hash" \
              --arg status "$f_status" --argjson health "$ACTIVE_PROXY_RESULT" '
              .status=$status | .activeHealth=$health |
              .prepared={previousSha256:$old,newSha256:$new,previousMode:$mode,
                systemSettingsSha256:$system,xraySha256:$binary}')" || return 74
            f_tmp="$f_directory/result-$BRORAY_BACKGROUND_OPERATION_ID.tmp"
            [ ! -e "$f_tmp" ] && [ ! -L "$f_tmp" ] &&
              [ "$(sha256sum "$f_result" | cut -d ' ' -f 1)" = "$f_digest" ] || return 76
            (set -C; printf '%s\n' "$f_state" >"$f_tmp") || return 74
            chmod 600 "$f_tmp" && mv "$f_tmp" "$f_result" || return 74
            broray_auto_failover_progress "$f_request" "$f_state" "$f_directory" || return $?
            rm "$f_prepare/operation-id" && rmdir "$f_prepare" || return 74
            [ "$f_status" = prepared ] || return 0
            f_digest="$(sha256sum "$f_result" | cut -d ' ' -f 1)" || return 74
            broray_job_yield activate "$f_digest"
            return $?
        fi
        f_cursor="$(printf '%s\n' "$f_state" | jq -r .cursor)"
        f_id="$(printf '%s\n' "$f_state" | jq -er --argjson n "$f_cursor" '.servers[$n].id')" || return 76
        f_node_hash="$(printf '%s\n' "$f_state" | jq -er --argjson n "$f_cursor" '.servers[$n].sha256')" || return 76
        f_output="$f_directory/candidate-$BRORAY_BACKGROUND_OPERATION_ID.json"
        [ ! -e "$f_output" ] && [ ! -L "$f_output" ] || return 74
        f_rc=0
        broray_server_check "$f_id" auto-switch "$f_node_hash" >"$f_output" || f_rc=$?
        case "$f_rc" in 0|1) ;; *) return "$f_rc" ;; esac
        jq -e --arg id "$f_id" --arg hash "$f_node_hash" --argjson rc "$f_rc" '
          .serverId==$id and .success==($rc==0) and .quality.serverFingerprint==$hash' "$f_output" >/dev/null || return 74
        f_policy="$(printf '%s\n' "$f_state" | jq -c .policy)"
        f_candidate="$(jq -c --argjson policy "$f_policy" --arg hash "$f_node_hash" '
          def metric: if type=="number" and .>=0 and floor==. then . else 999999 end;
          ({"excellent":4,"good":3,"acceptable":2,"poor":1}[.quality.rating] // 0) as $rank |
          if .success==true and $rank>=$policy.minimumRank then
            {id:.serverId,sha256:$hash,ratingRank:$rank,ping:(.quality.ping|metric),jitter:(.quality.jitter|metric)}
          else null end' "$f_output")" || return 74
        f_state="$(printf '%s\n' "$f_state" | jq -c --argjson candidate "$f_candidate" --argjson health "$f_health" '
          .cursor+=1 | .activeHealth=$health |
          if $candidate!=null then .candidates+=[$candidate] else . end')" || return 74
        f_selected="$(printf '%s\n' "$f_state" | jq -c '
          .policy as $p |
          if .cursor==(.servers|length) or
            ($p.rule=="preferred" and any(.candidates[];.id==$p.preferredServerId)) then
            ((.candidates|map(select($p.rule=="preferred" and .id==$p.preferredServerId))|first) //
              (.candidates|sort_by(if $p.rule=="lowest-ping" then [.ping,(-.ratingRank),.jitter,.id]
                else [(-.ratingRank),.ping,.jitter,.id] end)|first)) as $chosen |
            {done:true,selected:$chosen}
          else {done:false,selected:null} end')" || return 74
        f_state="$(printf '%s\n' "$f_state" | jq -c --argjson decision "$f_selected" '
          if $decision.done then .selected=$decision.selected |
            .status=(if .selected==null then "no-candidates" else "preparing" end) else . end')" || return 74
        f_tmp="$f_directory/result-$BRORAY_BACKGROUND_OPERATION_ID.tmp"
        [ ! -e "$f_tmp" ] && [ ! -L "$f_tmp" ] &&
          [ "$(sha256sum "$f_result" | cut -d ' ' -f 1)" = "$f_digest" ] || return 76
        (set -C; printf '%s\n' "$f_state" >"$f_tmp") || return 74
        chmod 600 "$f_tmp" && mv "$f_tmp" "$f_result" || return 74
        broray_auto_failover_progress "$f_request" "$f_state" "$f_directory" || return $?
        [ "$(printf '%s\n' "$f_state" | jq -r .status)" != no-candidates ] || return 0
        f_digest="$(sha256sum "$f_result" | cut -d ' ' -f 1)" || return 74
        broray_job_yield probe "$f_digest" ;;
      *) return 74 ;;
    esac
}

# Each cooperative stage owns only this progress field. It cannot replace
# active observations, counters, quality progress or protected switch history.
broray_auto_failover_progress()
{
    local progress_file
    progress_file="$3/progress-$BRORAY_BACKGROUND_OPERATION_ID.json"
    [ ! -e "$progress_file" ] && [ ! -L "$progress_file" ] || return 74
    (set -C; printf '%s\n' "$2" | jq -c --arg request "$1" --arg op "$BRORAY_BACKGROUND_OPERATION_ID" \
      --arg now "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" --argjson epoch "$(date '+%s')" '
      {schemaVersion:1,requestId:$request,operationId:$op,sourceContext:.context,serverId:.activeServerId,
       autoConfigSha256:.autoConfigSha256,activeHealth:.activeHealth,status:.status,checkedCount:.cursor,totalCount:(.servers|length),
       candidateCount:(.candidates|length),selectedServerId:(.selected.id // null),
       lastAttemptEpoch:.attemptEpoch,updatedAt:$now,updatedEpoch:$epoch}' >"$progress_file") || return 74
    chmod 600 "$progress_file" || return 74
    broray_job_publish_json failover-progress '' "$progress_file"
}

# The long generation/test work already finished in the cooperative stage.
# The global owner rechecks the bound inputs, then uses canonical activation.
broray_auto_failover_activate()
{
    local a_state a_dir a_id a_old a_node a_hash a_runtime a_binary a_mode a_file a_rc a_status a_expected
    a_state="$1"; a_dir="$2"
    a_id="$(printf '%s\n' "$a_state" | jq -er .selected.id)" || return 76
    a_old="$(printf '%s\n' "$a_state" | jq -er .activeServerId)" || return 76
    a_node="$(broray_server_path "$a_id")" || return 76
    a_hash="$(printf '%s\n' "$a_state" | jq -er .selected.sha256)" || return 76
    [ -f "$a_node" ] && [ ! -L "$a_node" ] &&
      [ "$(sha256sum "$a_node" | cut -d ' ' -f 1)" = "$a_hash" ] || return 76
    a_runtime="$(broray_xray_config_path)" || return 76
    # Canonical apply targets this path; never mutate a different config from
    # the runtime whose failure authorized the queued request.
    [ "$a_runtime" = "$BRORAY_CONFIG" ] || return 76
    a_binary="$(broray_xray_binary_path)" || return 76
    [ "$(sha256sum "$BRORAY_BASE/config/system/settings.json" | cut -d ' ' -f 1)" = \
      "$(printf '%s\n' "$a_state" | jq -er .prepared.systemSettingsSha256)" ] &&
      [ "$(sha256sum "$a_binary" | cut -d ' ' -f 1)" = \
      "$(printf '%s\n' "$a_state" | jq -er .prepared.xraySha256)" ] &&
      [ "$(sha256sum "$a_runtime" | cut -d ' ' -f 1)" = \
      "$(printf '%s\n' "$a_state" | jq -er .prepared.previousSha256)" ] || return 76
    a_mode="$(printf '%s\n' "$a_state" | jq -er '.prepared.previousMode |
      select(type=="string" and length>=3 and length<=4 and all(explode[];.>=48 and .<=55))')" || return 76
    [ "$(stat -c %a "$a_runtime")" = "$a_mode" ] || return 76
    for a_file in previous-runtime new-runtime; do
        [ -f "$a_dir/$a_file.json" ] && [ ! -L "$a_dir/$a_file.json" ] &&
          [ "$(stat -c '%u:%a:%h' "$a_dir/$a_file.json")" = "$(id -u):600:1" ] || return 76
        case "$a_file" in previous-runtime) a_expected=previousSha256 ;; *) a_expected=newSha256 ;; esac
        [ "$(sha256sum "$a_dir/$a_file.json" | cut -d ' ' -f 1)" = \
          "$(printf '%s\n' "$a_state" | jq -er --arg k "$a_expected" '.prepared[$k]')" ] || return 76
    done
    broray_job_checkpoint switching || return $?
    BRORAY_JOB_UNRESOLVED=true
    a_rc=0
    (broray_xray_apply_prepared_server "$a_id" "$a_dir/new-runtime.json") \
      >"$a_dir/activate-output" 2>"$a_dir/activate-error" || a_rc=$?
    # Canonical restart failure already attempts rollback. Preserve its fence
    # until recovery proves the service state; no optimistic finish here.
    [ "$a_rc" = 0 ] || return 75
    a_status="$(broray_xray_status_json)" || a_status='{}'
    if [ "$(cat "$BRORAY_ACTIVE_SERVER_FILE")" = "$a_id" ] &&
      printf '%s\n' "$a_status" | jq -e --arg hash "$(printf '%s\n' "$a_state" | jq -r .prepared.newSha256)" \
        '.success==true and .data.running==true and .data.configValid==true and
         .data.socks.active==true and .data.configSha256==$hash' >/dev/null; then
        a_state="$(printf '%s\n' "$a_state" | jq -c '.status="switched"')"
    else
        # Roll back the exact previous runtime, not a newly generated config
        # from a server record that may no longer reproduce those bytes.
        [ "$(cat "$BRORAY_ACTIVE_SERVER_FILE")" = "$a_id" ] &&
          [ "$(sha256sum "$a_runtime" | cut -d ' ' -f 1)" = \
          "$(printf '%s\n' "$a_state" | jq -r .prepared.newSha256)" ] || return 75
        chmod "$a_mode" "$a_dir/previous-runtime.json" || return 75
        (broray_xray_apply_prepared_server "$a_old" "$a_dir/previous-runtime.json") \
          >"$a_dir/rollback-output" 2>"$a_dir/rollback-error" || return 75
        a_status="$(broray_xray_status_json)" || return 75
        [ "$(cat "$BRORAY_ACTIVE_SERVER_FILE")" = "$a_old" ] &&
          [ "$(stat -c %a "$a_runtime")" = "$a_mode" ] &&
          printf '%s\n' "$a_status" | jq -e --arg hash "$(printf '%s\n' "$a_state" | jq -r .prepared.previousSha256)" \
            '.success==true and .data.running==true and .data.configValid==true and
             .data.socks.active==true and .data.configSha256==$hash' >/dev/null || return 75
        a_state="$(printf '%s\n' "$a_state" | jq -c '.status="rolled-back"')"
        a_rc=1
    fi
    [ ! -e "$a_dir/activation-result.tmp" ] && [ ! -L "$a_dir/activation-result.tmp" ] || return 75
    (set -C; printf '%s\n' "$a_state" >"$a_dir/activation-result.tmp") &&
      chmod 600 "$a_dir/activation-result.tmp" && mv "$a_dir/activation-result.tmp" "$a_dir/result.json" || return 75
    BRORAY_JOB_UNRESOLVED=false
    return "$a_rc"
}

# Only the protected global activation calls this. Preserve peer fields and
# require a new current-proxy observation after either restart or rollback.
broray_auto_failover_complete()
{
    local complete_file complete_id complete_name
    complete_file="$2/complete-$BRORAY_BACKGROUND_OPERATION_ID.json"
    [ ! -e "$complete_file" ] && [ ! -L "$complete_file" ] || return 75
    complete_id="$(jq -er .selected.id "$2/result.json")" || return 75
    complete_name="$(jq -er '.name // .id' "$(broray_server_path "$complete_id")")" || return 75
    (set -C; jq -ce --slurpfile result "$2/result.json" --arg request "$1" \
      --arg op "$BRORAY_BACKGROUND_OPERATION_ID" --arg name "$complete_name" \
      --arg now "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" --argjson epoch "$(date '+%s')" '
      $result[0] as $r |
      select(type=="object" and ($r.status|IN("switched","rolled-back")) and
        .failover.requestId==$request and .failover.sourceContext==$r.context and
        .autoConfigSha256==$r.autoConfigSha256) |
      .schemaVersion=3 | .backgroundOperationId=$op |
      .status=$r.status |
      .activeServerId=(if $r.status=="switched" then $r.selected.id else $r.activeServerId end) |
      .activeHealth=null | .lastProxyContext=null | .consecutiveFailures=0 |
      .candidateCount=($r.candidates|length) | .lastAttemptEpoch=$r.attemptEpoch |
      .qualityRefresh=({status:"idle",totalCount:0,checkedCount:0,availableCount:0,
        unavailableCount:0,errorCount:0} + (.qualityRefresh // {})) |
      .failover.status=$r.status | .failover.operationId=$op |
      .failover.updatedAt=$now | .failover.updatedEpoch=$epoch |
      if $r.status=="switched" then
        .lastSwitchEpoch=$epoch | .lastSwitchAt=$now |
        .lastSwitchFrom=$r.activeServerId | .lastSwitchTo=$r.selected.id | .lastSwitchName=$name |
        .lastReason="Выбран резервный сервер; ожидается проверка активного соединения" | .lastError=null
      else
        .lastReason="Переключение не подтверждено; прежняя конфигурация восстановлена" |
        .lastError="FAILOVER_ACTIVATION_ROLLED_BACK"
      end' "$BRORAY_BASE/run/server-auto-switch-state.json" >"$complete_file") || return 75
    chmod 600 "$complete_file" || return 75
    broray_job_publish_json auto-state '' "$complete_file"
}
