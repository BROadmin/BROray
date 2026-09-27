#!/opt/bin/ash
# Single-file business publications, always inside the coordinator flock.

ops_publication_private()
{
    ops_file_safe "$1" "${2:-16384}" &&
      [ "$(readlink -f "$1")" = "$1" ] &&
      [ "$(find "$1" -maxdepth 0 -type f -links 1 -perm 600 -print)" = "$1" ]
}

ops_publication_target()
{
    local resource key directory action
    resource="$1"; key="$2"
    OPS_PUB_FIELDS=''
    action="$(jq -r '.operation' "$OPS_CURRENT/state.json")" || return 1
    case "$resource:$action" in
      active-state:servers:active-health)
        [ -z "$key" ] || return 1
        OPS_PUB_TARGET="$OPS_APP/run/server-auto-switch-state.json"; OPS_PUB_FIELD=''
        OPS_PUB_FIELDS='["schemaVersion","backgroundOperationId","enabled","status","activeServerId","activeHealth","consecutiveFailures","lastProxyContext","lastEvaluationAt","lastEvaluationEpoch","lastReason","lastError","autoConfigSha256"]' ;;
      active-health:servers:active-health)
        [ -z "$key" ] || return 1
        OPS_PUB_TARGET="$OPS_APP/run/server-auto-switch-state.json"; OPS_PUB_FIELD=activeHealth ;;
      quality-progress:servers:quality)
        [ -z "$key" ] || return 1
        OPS_PUB_TARGET="$OPS_APP/run/server-auto-switch-state.json"; OPS_PUB_FIELD=qualityRefresh ;;
      dot-tests:dot:auto-check)
        [ -z "$key" ] || return 1
        ops_step_binding && [ "$OPS_STEP_RESOURCE" = background-prepare ] || return 1
        OPS_PUB_TARGET="$OPS_APP/routes/dot/state.json"; OPS_PUB_FIELD=''
        OPS_PUB_FIELDS='["tests","autoCheck","lastTestedAt","lastTestedEpoch","updatedAt"]' ;;
      failover-progress:servers:failover)
        [ -z "$key" ] || return 1
        ops_step_binding && [ "$OPS_STEP_RESOURCE" = background-prepare ] || return 1
        OPS_PUB_TARGET="$OPS_APP/run/server-auto-switch-state.json"; OPS_PUB_FIELD=failover ;;
      auto-state:auto-switch)
        [ -z "$key" ] || return 1
        OPS_PUB_TARGET="$OPS_APP/run/server-auto-switch-state.json" ;;
      auto-state:servers:failover)
        [ -z "$key" ] || return 1
        ops_step_binding && [ "$OPS_STEP_RESOURCE" = global ] || return 1
        OPS_PUB_TARGET="$OPS_APP/run/server-auto-switch-state.json" ;;
      server-quality:servers:check|server-quality:auto-switch|server-quality:servers:quality|server-quality:servers:failover)
        ops_id_valid "$key" || return 1
        OPS_PUB_TARGET="$OPS_APP/run/server-quality/$key.json" ;;
      *) return 1 ;;
    esac
    directory="${OPS_PUB_TARGET%/*}"
    ops_dir_safe "$directory" && [ "$(readlink -f "$directory")" = "$directory" ] || return 1
    OPS_PUB_PENDING="$OPS_PUB_TARGET.ops-pending"
}

ops_publication_hash()
{
    if [ ! -e "$1" ] && [ ! -L "$1" ]; then printf '%s\n' absent; return 0; fi
    ops_publication_private "$1" || return 1
    sha256sum "$1" | cut -d ' ' -f 1
}

ops_publication_load()
{
    ops_publication_private "$OPS_CURRENT/publication.json" 32768 || return 1
    OPS_PUB_RECORD="$(cat "$OPS_CURRENT/publication.json")" || return 1
    if printf '%s\n' "$OPS_PUB_RECORD" | jq -e '.kind=="field"' >/dev/null; then
        ops_field_publication_valid
        return $?
    fi
    printf '%s\n' "$OPS_PUB_RECORD" | jq -e --arg id "$OPS_ID" '
      def hex($n): type=="string" and length==$n and all(explode[];(.>=48 and .<=57) or (.>=97 and .<=102));
      .schemaVersion==1 and .operationId==$id and (.nonce|hex(32)) and
      (.resource=="auto-state" or .resource=="server-quality") and (.key|type)=="string" and
      (.oldHash=="absent" or (.oldHash|hex(64))) and (.newHash|hex(64)) and
      (.baseRevision|type)=="number" and .baseRevision>=1 and .baseRevision<9007199254740000 and (.baseRevision|floor)==.baseRevision and
      (.previousPhase|IN("working","checking","fetching","parsing","waiting","committing","switching")) and
      (.previousMode=="cooperative" or .previousMode=="protected") and (.complete|type)=="boolean"
    ' >/dev/null 2>&1
}

ops_publication_ready()
{
    [ -e "$OPS_CURRENT/publication.json" ] || [ -L "$OPS_CURRENT/publication.json" ] || return 0
    ops_publication_load && printf '%s\n' "$OPS_PUB_RECORD" | jq -e '.complete==true' >/dev/null
}

ops_publication_state_position()
{
    # Exact revisions prevent an old witness authorizing a later protected step.
    jq -r --argjson record "$OPS_PUB_RECORD" '
      if .revision==$record.baseRevision and .phase==$record.previousPhase and .cancelability==$record.previousMode then "before"
      elif .revision==($record.baseRevision+1) and .phase=="committing" and .cancelability=="protected" then "inside"
      elif .revision==($record.baseRevision+2) and .phase==$record.previousPhase and .cancelability==$record.previousMode then "after"
      else "unconfirmed" end' "$OPS_CURRENT/state.json"
}

ops_publication_restore()
{
    local phase mode
    phase="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r '.previousPhase')"
    mode="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r '.previousMode')"
    ops_state_transition running "$phase" '' "$mode"
}

ops_publication_complete()
{
    ops_write "$OPS_CURRENT/publication.json" "$(printf '%s\n' "$OPS_PUB_RECORD" | jq -c --arg outcome "$1" '.complete=true | .outcome=$outcome')"
}

ops_publication_test_point()
{
    if [ "${BRORAY_OPS_TEST:-0}" = 1 ] && [ "$OPS_APP" != /opt/broray ] &&
      [ "${BRORAY_OPS_TEST_PUBLICATION_CRASH:-}" = "$1" ]; then kill -KILL "$$"; fi
    return 0
}

ops_publication_recover()
{
    local resource key position current old new pending
    [ -e "$OPS_CURRENT/publication.json" ] || [ -L "$OPS_CURRENT/publication.json" ] || return 0
    ops_publication_load || return 1
    printf '%s\n' "$OPS_PUB_RECORD" | jq -e '.complete==true' >/dev/null && return 0
    if printf '%s\n' "$OPS_PUB_RECORD" | jq -e '.kind=="field"' >/dev/null; then
        ops_field_publication_recover
        return $?
    fi
    resource="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r '.resource')"
    key="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r '.key')"
    ops_publication_target "$resource" "$key" || return 1
    position="$(ops_publication_state_position)"; [ "$position" != unconfirmed ] || return 1
    old="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r '.oldHash')"
    new="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r '.newHash')"
    current="$(ops_publication_hash "$OPS_PUB_TARGET")" || return 1
    [ "$current" = "$old" ] || [ "$current" = "$new" ] || return 1
    [ "$position" != before ] || [ "$current" = "$old" ] || return 1
    [ "$position" != after ] || [ "$current" = "$new" ] || return 1
    pending="$(ops_publication_hash "$OPS_PUB_PENDING")" || return 1
    [ "$pending" = absent ] || [ "$pending" = "$new" ] || return 1
    "$OPS_GUARD" --sync-state "$OPS_PUB_TARGET" || return 1
    if [ "$pending" != absent ]; then
        rm "$OPS_PUB_PENDING" || return 1
        "$OPS_GUARD" --sync-state "$OPS_PUB_PENDING" || return 1
    fi
    if [ "$position" = inside ]; then ops_publication_restore || return 1; fi
    ops_publication_complete "$current"
}

ops_publish_json()
{
    local resource key input nonce revision json new old position current pending existing_nonce
    ops_authorize "$1" "$2"; ops_owner_authorize "$3"
    ops_platform_is_preflight && ops_error PLATFORM_MUTATION_NOT_IMPLEMENTED
    if ops_is_queue_step; then
        if [ "$4" != server-quality ] && [ "$4" != auto-state ]; then
            ops_publish_field "$@"; return $?
        fi
        ops_resources_match || ops_error OWNER_CHANGED
    else ops_global_matches || ops_error OWNER_CHANGED; fi
    jq -e '.acknowledged==true' "$OPS_CURRENT/state.json" >/dev/null || ops_error NOT_ACKNOWLEDGED
    resource="$4"; key="$5"; input="$6"; nonce="$7"; revision="$8"
    ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    case "$revision" in ''|*[!0-9]*) ops_error INVALID_REQUEST 1 ;; esac
    [ "${#revision}" -le 15 ] || ops_error INVALID_REQUEST 1
    ops_publication_target "$resource" "$key" || ops_error INVALID_PUBLICATION 1
    ops_publication_private "$input" || ops_error INVALID_PUBLICATION 1
    json="$(jq -cs --arg resource "$resource" --arg id "$OPS_ID" '
      def count: type=="number" and .>=0 and floor==.;
      select(length==1) | .[0] |
      select(type=="object") |
      if $resource=="auto-state" then
        select(.schemaVersion==3 and .backgroundOperationId==$id and (.enabled|type)=="boolean" and
          (.status|type)=="string" and (.status|length)<=64 and (.qualityRefresh|type)=="object" and
          (.qualityRefresh.status|type)=="string" and all([.consecutiveFailures,.candidateCount,.qualityRefresh.totalCount,.qualityRefresh.checkedCount,.qualityRefresh.availableCount,.qualityRefresh.unavailableCount,.qualityRefresh.errorCount][];count))
      else
        select((.status=="available" or .status=="unavailable") and
          (.measurementSource|IN("manual","auto-switch","scheduled")) and
          all([.successfulChecks,.failedChecks,.disconnects,.durationMs][];count))
      end' "$input")" || ops_error INVALID_PUBLICATION 1
    [ -n "$json" ] || ops_error INVALID_PUBLICATION 1
    new="$(printf '%s\n' "$json" | sha256sum | cut -d ' ' -f 1)"
    if [ -e "$OPS_CURRENT/publication.json" ] || [ -L "$OPS_CURRENT/publication.json" ]; then
        ops_publication_load || ops_error PUBLICATION_UNCONFIRMED 75
        existing_nonce="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r '.nonce')"
        if [ "$existing_nonce" = "$nonce" ]; then
            printf '%s\n' "$OPS_PUB_RECORD" | jq -e --arg resource "$resource" --arg key "$key" --arg hash "$new" --argjson revision "$revision" \
              '.resource==$resource and .key==$key and .newHash==$hash and .baseRevision==$revision' >/dev/null || ops_error PUBLICATION_MISMATCH 75
            position="$(ops_publication_state_position)"
            [ "$position" != unconfirmed ] || ops_error STALE_PUBLICATION 75
            if printf '%s\n' "$OPS_PUB_RECORD" | jq -e '.complete==true' >/dev/null; then
                [ "$position" = after ] && [ "$(ops_publication_hash "$OPS_PUB_TARGET")" = "$new" ] || ops_error STALE_PUBLICATION 75
                printf '%s\n' '{"ok":true,"published":true,"alreadyPublished":true}'; return 0
            fi
        else
            ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
            OPS_PUB_RECORD=''
        fi
    else OPS_PUB_RECORD=''; fi
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    if [ -z "$OPS_PUB_RECORD" ]; then
        if ops_is_queue_step && [ "$resource" = server-quality ]; then
            # Read-only probe results can become stale while another allowed
            # writer changes a server. Compare at publication under the guard.
            ops_file_safe "$OPS_APP/servers/$key.json" || ops_error SERVER_CONTEXT_CHANGED
            current="$(sha256sum "$OPS_APP/servers/$key.json" | cut -d ' ' -f 1)" || ops_error SERVER_CONTEXT_CHANGED
            printf '%s\n' "$json" | jq -e --arg hash "$current" '.serverFingerprint==$hash' >/dev/null || ops_error SERVER_CONTEXT_CHANGED
        fi
        jq -e --argjson revision "$revision" '.revision==$revision' "$OPS_CURRENT/state.json" >/dev/null || ops_error STALE_PUBLICATION 75
        [ ! -e "$OPS_CURRENT/cancel.json" ] && [ ! -L "$OPS_CURRENT/cancel.json" ] || ops_error CANCELLED
        old="$(ops_publication_hash "$OPS_PUB_TARGET")" || ops_error INVALID_PUBLICATION 1
        [ ! -e "$OPS_PUB_PENDING" ] && [ ! -L "$OPS_PUB_PENDING" ] || ops_error PUBLICATION_UNCONFIRMED 75
        OPS_PUB_RECORD="$(jq -c --arg id "$OPS_ID" --arg resource "$resource" --arg key "$key" --arg nonce "$nonce" --arg old "$old" --arg new "$new" \
          '{schemaVersion:1,operationId:$id,resource:$resource,key:$key,nonce:$nonce,oldHash:$old,newHash:$new,baseRevision:.revision,previousPhase:.phase,previousMode:.cancelability,complete:false}' "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 75
        ops_write "$OPS_CURRENT/publication.json" "$OPS_PUB_RECORD" || ops_error STATE_UNAVAILABLE 75
        ops_publication_test_point reserved
    fi
    position="$(ops_publication_state_position)"
    case "$position" in
      before)
        if [ -e "$OPS_CURRENT/cancel.json" ] || [ -L "$OPS_CURRENT/cancel.json" ]; then
            ops_publication_recover || ops_error PUBLICATION_UNCONFIRMED 75
            ops_error CANCELLED
        fi
        ops_state_transition running committing '' protected || ops_error STATE_UNAVAILABLE 75
        ops_publication_test_point protected ;;
      inside|after) ;;
      *) ops_error PUBLICATION_UNCONFIRMED 75 ;;
    esac
    old="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r '.oldHash')"
    current="$(ops_publication_hash "$OPS_PUB_TARGET")" || ops_error PUBLICATION_UNCONFIRMED 75
    [ "$current" = "$old" ] || [ "$current" = "$new" ] || ops_error PUBLICATION_UNCONFIRMED 75
    if [ "$current" != "$new" ]; then
        [ "$position" != after ] || ops_error PUBLICATION_UNCONFIRMED 75
        pending="$(ops_publication_hash "$OPS_PUB_PENDING")" || ops_error PUBLICATION_UNCONFIRMED 75
        if [ "$pending" = absent ]; then ops_write "$OPS_PUB_PENDING" "$json" || ops_error STATE_UNAVAILABLE 75
        else [ "$pending" = "$new" ] || ops_error PUBLICATION_UNCONFIRMED 75; fi
        ops_publication_test_point prepared
        "$OPS_GUARD" --replace-file "$OPS_PUB_PENDING" "$OPS_PUB_TARGET" || ops_error STATE_UNAVAILABLE 75
        ops_publication_test_point replaced
    else "$OPS_GUARD" --sync-state "$OPS_PUB_TARGET" || ops_error STATE_UNAVAILABLE 75; fi
    if [ "$position" != after ]; then ops_publication_restore || ops_error STATE_UNAVAILABLE 75; fi
    ops_publication_test_point restored
    ops_publication_complete "$new" || ops_error STATE_UNAVAILABLE 75
    printf '%s\n' '{"ok":true,"published":true}'
}

# Concurrent observers/preparers publish disjoint cache fields. Their intent
# compares only the owned field; a peer changing another field is preserved.
# Domain/catalog publications retain the exclusive legacy transaction above.
ops_field_publication_valid()
{
    printf '%s\n' "$OPS_PUB_RECORD" | jq -e --arg id "$OPS_ID" '
      def hex($n): type=="string" and length==$n and all(explode[];(.>=48 and .<=57) or (.>=97 and .<=102));
      .schemaVersion==1 and .kind=="field" and .operationId==$id and (.nonce|hex(32)) and
      (.resource|IN("active-health","active-state","quality-progress","failover-progress","dot-tests")) and .key=="" and
      (.oldHash|hex(64)) and (.newHash|hex(64)) and (.payload|type)=="object" and
      (.baseRevision|type)=="number" and .baseRevision>=1 and (.baseRevision|floor)==.baseRevision and
      (.previousPhase|IN("working","checking")) and .previousMode=="cooperative" and (.complete|type)=="boolean"
    ' >/dev/null || return 1
    [ "$(printf '%s\n' "$OPS_PUB_RECORD" | jq -c .payload | sha256sum | cut -d ' ' -f 1)" = "$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r .newHash)" ]
}

ops_field_read()
{
    if [ ! -e "$OPS_PUB_TARGET" ] && [ ! -L "$OPS_PUB_TARGET" ]; then
        OPS_FIELD_BASE='{}'
    else
        ops_publication_private "$OPS_PUB_TARGET" || return 1
        OPS_FIELD_BASE="$(jq -ce 'select(type=="object")' "$OPS_PUB_TARGET")" || return 1
    fi
    OPS_FIELD_HASH="$(printf '%s\n' "$OPS_FIELD_BASE" | jq -c --arg field "$OPS_PUB_FIELD" --argjson fields "${OPS_PUB_FIELDS:-[]}" '
      if ($fields|length)==0 then .[$field] else . as $base |
        reduce $fields[] as $key ({}; .[$key]=$base[$key]) end' | sha256sum | cut -d ' ' -f 1)"
}

ops_field_publication_recover()
{
    local resource position old new
    resource="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r .resource)"
    ops_publication_target "$resource" '' && ops_field_read || return 1
    position="$(ops_publication_state_position)"; [ "$position" != unconfirmed ] || return 1
    old="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r .oldHash)"; new="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r .newHash)"
    [ "$OPS_FIELD_HASH" = "$old" ] || [ "$OPS_FIELD_HASH" = "$new" ] || return 1
    [ "$position" != before ] || [ "$OPS_FIELD_HASH" = "$old" ] || return 1
    [ "$position" != after ] || [ "$OPS_FIELD_HASH" = "$new" ] || return 1
    "$OPS_GUARD" --sync-state "$OPS_PUB_TARGET" || return 1
    if [ "$position" = inside ]; then ops_publication_restore || return 1; fi
    ops_publication_complete "$OPS_FIELD_HASH"
}

ops_field_active_context() (
    local server
    server="$1"
    if [ "${BRORAY_OPS_TEST:-0}" = 1 ] && [ "$OPS_APP" != /opt/broray ] && [ -n "${BRORAY_OPS_TEST_ACTIVE_CONTEXT:-}" ]; then
        ops_queue_private "$BRORAY_OPS_TEST_ACTIVE_CONTEXT" 4096 || return 1
        jq -er --arg id "$server" 'select(.serverId==$id)|.context' "$BRORAY_OPS_TEST_ACTIVE_CONTEXT"
        return $?
    fi
    # Load authenticated code first, then address the live installation state.
    BRORAY_ROOT="$OPS_CODE"; BRORAY_SETTINGS_FILE="$OPS_APP/config/system/settings.json"
    BRORAY_RUN_DIR="$OPS_APP/run"
    . "$OPS_CODE/lib/xray.sh" || return 1
    BRORAY_ROOT="$OPS_APP"; BRORAY_BASE="$OPS_APP"
    BRORAY_ACTIVE_SERVER_FILE="$OPS_APP/config/active-server"
    . "$OPS_CODE/lib/active-proxy-health.sh" || return 1
    broray_active_proxy_context "$server"
)

ops_publication_context_error()
{
    # A retry with an existing intent requires recovery even when the input
    # context changed. Only the guarded pre-intent path can attest zero writes.
    [ -z "${OPS_PUB_RECORD:-}" ] || ops_error PUBLICATION_UNCONFIRMED 75
    jq -nc --arg code "$1" '{ok:false,errorCode:$code,mutationStarted:false}'
    exit 2
}

ops_field_dot_context() (
    BRORAY_ROOT="$OPS_APP"; BRORAY_OPS_CODE_ROOT="$OPS_CODE"
    BRORAY_DOT_ROOT="$OPS_APP/routes/dot"
    BRORAY_DOT_CONFIG="$BRORAY_DOT_ROOT/config.json"; BRORAY_DOT_STATE="$BRORAY_DOT_ROOT/state.json"
    . "$OPS_CODE/lib/dot-auto.sh" || return 1
    broray_dot_auto_context
)

ops_publish_field()
{
    local resource input nonce revision json new old position context server merged
    ops_resources_match || ops_error OWNER_CHANGED
    jq -e '.acknowledged==true' "$OPS_CURRENT/state.json" >/dev/null || ops_error NOT_ACKNOWLEDGED
    resource="$4"; input="$6"; nonce="$7"; revision="$8"
    ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    case "$revision" in ''|*[!0-9]*) ops_error INVALID_REQUEST 1 ;; esac
    ops_publication_target "$resource" "$5" || ops_error INVALID_PUBLICATION 1
    case "$resource" in active-health|active-state|quality-progress|failover-progress|dot-tests) ;; *) ops_error INVALID_PUBLICATION 1 ;; esac
    ops_publication_private "$input" || ops_error INVALID_PUBLICATION 1
    json="$(jq -cs --arg resource "$resource" --arg operation "$OPS_ID" --argjson fields "${OPS_PUB_FIELDS:-[]}" '
      def count: type=="number" and .>=0 and floor==.;
      def hex64: type=="string" and length==64 and all(explode[];(.>=48 and .<=57) or (.>=97 and .<=102));
      select(length==1)|.[0]|select(type=="object")|
      if $resource=="active-health" then
        select(.method=="current-socks-https" and (.status|IN("healthy","unhealthy","unknown")) and
          (.serverId|type)=="string" and (.context|hex64) and (.checkedEpoch|count) and (.checkedAt|type)=="string")
      elif $resource=="active-state" then
        select((keys|sort)==($fields|sort) and .schemaVersion==3 and
          .backgroundOperationId==$operation and (.enabled|type)=="boolean" and
          (.status|IN("healthy","waiting-threshold","disabled","paused")) and
          .activeServerId==.activeHealth.serverId and
          .activeHealth.method=="current-socks-https" and
          (.activeHealth.status|IN("healthy","unhealthy","unknown")) and
          (.activeHealth.context|hex64) and (.activeHealth.checkedEpoch|count) and
          (.activeHealth.checkedAt|type)=="string" and (.consecutiveFailures|count) and
          .consecutiveFailures<=2147483647 and (.autoConfigSha256|hex64) and
          .lastEvaluationAt==.activeHealth.checkedAt and .lastEvaluationEpoch==.activeHealth.checkedEpoch and
          (.lastReason|type)=="string" and .lastError==null and
          (.lastProxyContext==null or .lastProxyContext==.activeHealth.context)) |
        . as $value | reduce $fields[] as $key ({}; .[$key]=$value[$key])
      elif $resource=="dot-tests" then
        select((keys|sort)==($fields|sort) and (.tests|type)=="array" and (.tests|length)<=500 and
          (.lastTestedAt|type)=="string" and (.lastTestedEpoch|count) and .updatedAt==.lastTestedAt and
          (.autoCheck|type)=="object" and .autoCheck.operationId==$operation and
          (.autoCheck.context|hex64) and (.autoCheck.requestId|type)=="string" and
          (.autoCheck.status|IN("running","success","failed")) and
          (.autoCheck.selectedIds|type)=="array" and
          .autoCheck.totalCount==(.autoCheck.selectedIds|length) and .autoCheck.totalCount<=8 and
          all([.autoCheck.checkedCount,.autoCheck.failedCount,.autoCheck.lastAttemptEpoch][];count) and
          .autoCheck.failedCount<=.autoCheck.checkedCount and .autoCheck.checkedCount<=.autoCheck.totalCount)
      elif $resource=="failover-progress" then
        select((keys|sort)==(["schemaVersion","requestId","operationId","sourceContext","serverId",
          "autoConfigSha256","activeHealth","status","checkedCount","totalCount","candidateCount","selectedServerId",
          "lastAttemptEpoch","updatedAt","updatedEpoch"]|sort) and
          .schemaVersion==1 and .operationId==$operation and (.requestId|type)=="string" and
          (.sourceContext|hex64) and (.autoConfigSha256|hex64) and (.serverId|type)=="string" and
          (.status|IN("probing","preparing","prepared","no-candidates","recovered")) and
          (.activeHealth|type)=="object" and .activeHealth.method=="current-socks-https" and
          .activeHealth.serverId==.serverId and .activeHealth.context==.sourceContext and
          (.activeHealth.status|IN("healthy","unhealthy")) and (.activeHealth.checkedEpoch|count) and
          .activeHealth.checkedEpoch<=.updatedEpoch and (.activeHealth.checkedAt|type)=="string" and
          all([.checkedCount,.totalCount,.candidateCount,.lastAttemptEpoch,.updatedEpoch][];count) and
          .checkedCount<=.totalCount and .candidateCount<=.checkedCount and
          .lastAttemptEpoch<=.updatedEpoch and (.updatedAt|type)=="string" and
          (.selectedServerId==null or (.selectedServerId|type)=="string"))
      else
        select((.status|type)=="string" and (.status|length)<=64 and
          all([.totalCount,.checkedCount,.availableCount,.unavailableCount,.errorCount][];count))
      end' "$input")" || ops_error INVALID_PUBLICATION 1
    [ -n "$json" ] || ops_error INVALID_PUBLICATION 1
    new="$(printf '%s\n' "$json" | sha256sum | cut -d ' ' -f 1)"
    if [ -e "$OPS_CURRENT/publication.json" ] || [ -L "$OPS_CURRENT/publication.json" ]; then
        ops_publication_load || ops_error PUBLICATION_UNCONFIRMED 75
        if [ "$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r .nonce)" = "$nonce" ]; then
            printf '%s\n' "$OPS_PUB_RECORD" | jq -e --arg resource "$resource" --arg new "$new" --argjson revision "$revision" \
              '.kind=="field" and .resource==$resource and .newHash==$new and .baseRevision==$revision' >/dev/null || ops_error PUBLICATION_MISMATCH 75
            if printf '%s\n' "$OPS_PUB_RECORD" | jq -e '.complete==true' >/dev/null; then
                printf '%s\n' "$OPS_PUB_RECORD" | jq -e --arg new "$new" '.outcome==$new' >/dev/null || ops_error STALE_PUBLICATION 75
                printf '%s\n' '{"ok":true,"published":true,"alreadyPublished":true}'; return 0
            fi
        else
            ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED 75
            OPS_PUB_RECORD=''
        fi
    else OPS_PUB_RECORD=''; fi
    if [ "$resource" = active-health ] || [ "$resource" = active-state ]; then
        context="$(printf '%s\n' "$json" | jq -r '(.activeHealth // .).context')"
        server="$(printf '%s\n' "$json" | jq -r '(.activeHealth // .).serverId')"
        ops_id_valid "$server" || ops_error INVALID_PUBLICATION 1
        jq -e --arg context "$context" --arg server "$server" '.queueStep.context==$context and .bundleId==$server' "$OPS_CURRENT/state.json" >/dev/null || ops_publication_context_error ACTIVE_CONTEXT_CHANGED
        [ "$(ops_field_active_context "$server")" = "$context" ] || ops_publication_context_error ACTIVE_CONTEXT_CHANGED
        if [ "$resource" = active-state ]; then
            ops_file_safe "$OPS_APP/config/system/server-auto-switch.json" || ops_publication_context_error ACTIVE_SETTINGS_CHANGED
            [ "$(sha256sum "$OPS_APP/config/system/server-auto-switch.json" | cut -d ' ' -f 1)" = "$(printf '%s\n' "$json" | jq -r .autoConfigSha256)" ] || ops_publication_context_error ACTIVE_SETTINGS_CHANGED
        fi
    fi
    if [ "$resource" = failover-progress ]; then
        context="$(printf '%s\n' "$json" | jq -r .sourceContext)"
        server="$(printf '%s\n' "$json" | jq -r .serverId)"
        ops_id_valid "$server" || ops_error INVALID_PUBLICATION 1
        jq -e --argjson payload "$json" '.queueStep.context==$payload.sourceContext and
          .queueStep.requestId==$payload.requestId and .bundleId==$payload.serverId' "$OPS_CURRENT/state.json" >/dev/null &&
          [ "$(ops_field_active_context "$server")" = "$context" ] || ops_publication_context_error FAILOVER_CONTEXT_CHANGED
        ops_file_safe "$OPS_APP/config/system/server-auto-switch.json" &&
          [ "$(sha256sum "$OPS_APP/config/system/server-auto-switch.json" | cut -d ' ' -f 1)" = \
          "$(printf '%s\n' "$json" | jq -r .autoConfigSha256)" ] || ops_publication_context_error FAILOVER_CONTEXT_CHANGED
    fi
    if [ "$resource" = dot-tests ]; then
        context="$(printf '%s\n' "$json" | jq -r .autoCheck.context)"
        jq -e --argjson payload "$json" '.queueStep.context==$payload.autoCheck.context and
          .queueStep.requestId==$payload.autoCheck.requestId' "$OPS_CURRENT/state.json" >/dev/null &&
          [ "$(ops_field_dot_context)" = "$context" ] || ops_publication_context_error DOT_CONTEXT_CHANGED
        # Native-guard admission excludes manual dot global writers. An
        # unknown/new global fence during publication must also fail closed.
        ops_step_global_compatible background-prepare || ops_publication_context_error DOT_CONTEXT_CHANGED
        ops_publication_private "$OPS_PUB_TARGET" &&
          jq -e '.schemaVersion==1 and (.tests|type)=="array"' "$OPS_PUB_TARGET" >/dev/null || ops_error INVALID_PUBLICATION 1
    fi
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_field_read || ops_error INVALID_PUBLICATION 1
    if [ -z "$OPS_PUB_RECORD" ]; then
        jq -e --argjson revision "$revision" '.revision==$revision' "$OPS_CURRENT/state.json" >/dev/null || ops_error STALE_PUBLICATION 75
        [ ! -e "$OPS_CURRENT/cancel.json" ] && [ ! -L "$OPS_CURRENT/cancel.json" ] || ops_error CANCELLED
        OPS_PUB_RECORD="$(jq -c --arg id "$OPS_ID" --arg resource "$resource" --arg nonce "$nonce" --arg old "$OPS_FIELD_HASH" --arg new "$new" --argjson payload "$json" \
          '{schemaVersion:1,kind:"field",operationId:$id,resource:$resource,key:"",nonce:$nonce,oldHash:$old,newHash:$new,payload:$payload,
            baseRevision:.revision,previousPhase:.phase,previousMode:.cancelability,complete:false}' "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE 75
        ops_write "$OPS_CURRENT/publication.json" "$OPS_PUB_RECORD" || ops_error STATE_UNAVAILABLE 75
        ops_publication_test_point reserved
    fi
    position="$(ops_publication_state_position)"
    old="$(printf '%s\n' "$OPS_PUB_RECORD" | jq -r .oldHash)"
    [ "$OPS_FIELD_HASH" = "$old" ] || [ "$OPS_FIELD_HASH" = "$new" ] || ops_error PUBLICATION_UNCONFIRMED 75
    case "$position" in
      before)
        [ "$OPS_FIELD_HASH" = "$old" ] || ops_error PUBLICATION_UNCONFIRMED 75
        if [ -e "$OPS_CURRENT/cancel.json" ] || [ -L "$OPS_CURRENT/cancel.json" ]; then
            ops_publication_complete "$old" || ops_error STATE_UNAVAILABLE 75
            ops_error CANCELLED
        fi
        ops_state_transition running committing '' protected || ops_error STATE_UNAVAILABLE 75
        ops_publication_test_point protected ;;
      inside|after) ;;
      *) ops_error PUBLICATION_UNCONFIRMED 75 ;;
    esac
    if [ "$OPS_FIELD_HASH" != "$new" ]; then
        [ "$position" != after ] || ops_error PUBLICATION_UNCONFIRMED 75
        merged="$(printf '%s\n' "$OPS_FIELD_BASE" | jq -c --arg field "$OPS_PUB_FIELD" --argjson fields "${OPS_PUB_FIELDS:-[]}" --argjson payload "$json" '
          if ($fields|length)==0 then .[$field]=$payload else .+$payload end')" || ops_error INVALID_PUBLICATION 1
        ops_write "$OPS_PUB_TARGET" "$merged" || ops_error STATE_UNAVAILABLE 75
        ops_publication_test_point replaced
    else "$OPS_GUARD" --sync-state "$OPS_PUB_TARGET" || ops_error STATE_UNAVAILABLE 75; fi
    if [ "$position" != after ]; then ops_publication_restore || ops_error STATE_UNAVAILABLE 75; fi
    ops_publication_test_point restored
    ops_publication_complete "$new" || ops_error STATE_UNAVAILABLE 75
    printf '%s\n' '{"ok":true,"published":true}'
}
