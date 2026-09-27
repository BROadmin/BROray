#!/opt/bin/ash
# Part of the coordinator: every entry point runs under operations.guard.
# Waiting requests own no process and no domain fence. Never execute queue text.

ops_queue_policy()
{
    OPS_Q_PRIORITY=''; OPS_Q_STAGE=''
    case "$1:$2" in
        servers:active-health:AUTO_SWITCH|servers:active-health:USER) OPS_Q_PRIORITY=0; OPS_Q_STAGE=checking ;;
        servers:failover:AUTO_SWITCH) OPS_Q_PRIORITY=1; OPS_Q_STAGE=verify ;;
        servers:quality:USER) OPS_Q_PRIORITY=2; OPS_Q_STAGE=probe ;;
        servers:quality:SERVER_CHECK_AUTO) OPS_Q_PRIORITY=3; OPS_Q_STAGE=probe ;;
        subscriptions:refresh:USER) OPS_Q_PRIORITY=2; OPS_Q_STAGE=fetch ;;
        subscriptions:refresh:SUBSCRIPTION_AUTO) OPS_Q_PRIORITY=4; OPS_Q_STAGE=fetch ;;
        dot:auto-check:SCHEDULER) OPS_Q_PRIORITY=5; OPS_Q_STAGE=probe ;;
        *) return 1 ;;
    esac
}

ops_queue_sha_valid()
{
    case "$1" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#1}" = 64 ]
}

ops_queue_private()
{
    local metadata size identity
    [ -f "$1" ] && [ ! -L "$1" ] || return 1
    metadata="$(broray_ops_file_stat -c '%s:%u:%a:%h' "$1" 2>/dev/null)" || return 1
    size="${metadata%%:*}"; identity="${metadata#*:}"
    case "$size" in ''|*[!0-9]*) return 1 ;; esac
    [ "$size" -le "${2:-524288}" ] && [ "$identity" = "$(id -u):600:1" ]
}

ops_queue_load()
{
    local directory create evidence
    create="${1:-read}"
    OPS_Q_BOOT="$(broray_ops_boot_id)" || ops_error QUEUE_STATE_INVALID
    case "$OPS_Q_BOOT" in ''|*[!A-Za-z0-9-]*) ops_error QUEUE_STATE_INVALID ;; esac
    OPS_Q_DIR="$OPS_RAM/queue/$OPS_Q_BOOT"; OPS_Q_FILE="$OPS_Q_DIR/state.json"
    OPS_Q_NEW=false
    for directory in "$OPS_RAM" "$OPS_RAM/queue"; do
        if [ ! -e "$directory" ] && [ ! -L "$directory" ]; then
            [ "$create" != create ] || mkdir -m 700 "$directory" || ops_error QUEUE_STATE_INVALID
        else
            ops_dir_safe "$directory" && [ "$(broray_ops_file_stat -c '%u:%a' "$directory" 2>/dev/null)" = "$(id -u):700" ] || ops_error QUEUE_STATE_INVALID
        fi
    done
    if [ ! -e "$OPS_Q_DIR" ] && [ ! -L "$OPS_Q_DIR" ]; then
        # Losing a namespace while its same-boot owners remain is corruption,
        # not a reboot. Do not detach those owners from their requests.
        for evidence in "$OPS_RAM/steps"/*/owner.json "$OPS_ROOT"/*/owner.json; do
            [ -e "$evidence" ] || [ -L "$evidence" ] || continue
            case "$evidence" in "$OPS_ROOT/"*)
                ops_file_safe "${evidence%/owner.json}/state.json" || ops_error QUEUE_STATE_INVALID
                jq -e 'has("queueStep")' "${evidence%/owner.json}/state.json" >/dev/null || continue ;;
            esac
            ops_queue_private "$evidence" 4096 &&
              jq -e --arg boot "$OPS_Q_BOOT" '(.owner.bootId|type)=="string" and .owner.bootId!=$boot' "$evidence" >/dev/null || ops_error QUEUE_STATE_INVALID
        done
        OPS_Q_NEW=true
        OPS_Q_JSON="$(jq -nc --arg boot "$OPS_Q_BOOT" '{schemaVersion:1,bootId:$boot,revision:0,sequence:0,fairCursor:0,requests:[],receipts:[]}')" || ops_error QUEUE_STATE_INVALID
        return 0
    fi
    # An existing namespace without its ledger is damaged, never a new queue.
    ops_dir_safe "$OPS_Q_DIR" && [ "$(broray_ops_file_stat -c '%u:%a' "$OPS_Q_DIR" 2>/dev/null)" = "$(id -u):700" ] || ops_error QUEUE_STATE_INVALID
    ops_queue_private "$OPS_Q_FILE" || ops_error QUEUE_STATE_INVALID
    OPS_Q_JSON="$(jq -ce --arg boot "$OPS_Q_BOOT" '
      def hex($n): type=="string" and length==$n and all(explode[];(.>=48 and .<=57) or (.>=97 and .<=102));
      def uint: type=="number" and .>=0 and .<9007199254740000 and floor==.;
      def rid: type=="string" and startswith("q-") and (.[2:]|hex(32));
      def expected_priority:
        if .action=="servers:active-health" and (.source|IN("AUTO_SWITCH","USER")) then 0
        elif .action=="servers:failover" and .source=="AUTO_SWITCH" then 1
        elif (.action|IN("servers:quality","subscriptions:refresh")) and .source=="USER" then 2
        elif .action=="servers:quality" and .source=="SERVER_CHECK_AUTO" then 3
        elif .action=="subscriptions:refresh" and .source=="SUBSCRIPTION_AUTO" then 4
        elif .action=="dot:auto-check" and .source=="SCHEDULER" then 5 else -1 end;
      select(type=="object" and .schemaVersion==1 and .bootId==$boot and (.revision|uint) and (.sequence|uint) and
       (.fairCursor|uint) and .fairCursor<6 and (.requests|type)=="array" and (.requests|length)<=67 and
       ([.requests[]|select(.state=="queued")]|length)<=64 and (.receipts|type)=="array" and (.receipts|length)<=128 and
       ([.requests[].requestId]|length)==([.requests[].requestId]|unique|length) and
       ([.receipts[].nonce]|length)==([.receipts[].nonce]|unique|length) and
       all(.requests[]; (.requestId|rid) and (.sequence|uint) and .sequence>0 and
        (.priority|uint) and .priority==expected_priority and (.state|IN("queued","running")) and
        (.context|hex(64)) and (.inputHash|hex(64)) and (.action|type)=="string" and
        (.source|type)=="string" and (.targetId|type)=="string" and (.stage|type)=="string") and
       all(.receipts[]; (.nonce|hex(32)) and (.inputHash|hex(64)) and (.requestId|rid) and
        (.state|IN("queued","running","completed","cancelled","failed")) and (.priority|uint) and .priority<=5))
      ' "$OPS_Q_FILE")" || ops_error QUEUE_STATE_INVALID
}

ops_queue_store()
{
    local directory
    OPS_Q_JSON="$(printf '%s\n' "$OPS_Q_JSON" | jq -c '.revision+=1')" || ops_error QUEUE_STATE_INVALID
    [ "$(printf '%s\n' "$OPS_Q_JSON" | wc -c)" -le 524288 ] || ops_error QUEUE_FULL
    if [ "$OPS_Q_NEW" = true ]; then
        # Publish a complete initial namespace under the coordinator guard.
        # A crash before rename leaves no admitted requests. A missing ledger
        # after rename is preserved as evidence instead of being recreated.
        directory="$(mktemp -d "$OPS_RAM/queue/.initializing-$OPS_Q_BOOT-XXXXXX")" || ops_error QUEUE_STATE_INVALID
        ops_write "$directory/state.json" "$OPS_Q_JSON" || ops_error QUEUE_STATE_INVALID
        [ ! -e "$OPS_Q_DIR" ] && [ ! -L "$OPS_Q_DIR" ] || ops_error QUEUE_STATE_INVALID
        mv -T "$directory" "$OPS_Q_DIR" || ops_error QUEUE_STATE_INVALID
        OPS_Q_NEW=false
    else
        ops_queue_private "$OPS_Q_FILE" || ops_error QUEUE_STATE_INVALID
        ops_write "$OPS_Q_FILE" "$OPS_Q_JSON" || ops_error QUEUE_STATE_INVALID
    fi
}

ops_queue_receipt()
{
    local nonce row
    nonce="$1"
    row="$(printf '%s\n' "$OPS_Q_JSON" | jq -c --arg nonce "$nonce" '
      ([.receipts[]|select(.nonce==$nonce)]|first) //
      ([.requests[]|select(.requestId==("q-"+$nonce))]|first) // empty')" || return 1
    [ -n "$row" ] || return 1
    printf '%s\n' "$OPS_Q_JSON" | jq -c --argjson r "$row" '
      ([.requests[] | select(.requestId==$r.requestId)]|first) as $live |
      {ok:true,requestId:$r.requestId,state:($live.state // $r.state),priority:$r.priority,coalesced:($r.coalesced==true)}'
}

ops_queue_submit()
{
    local action target source context nonce digest existing record reply id count
    action="$1"; target="$2"; source="$3"; context="$4"; nonce="$5"
    ops_queue_policy "$action" "$source" || ops_error INVALID_QUEUE_ACTION
    ops_id_valid "$target" && ops_queue_sha_valid "$context" && ops_nonce_valid "$nonce" || ops_error INVALID_REQUEST 1
    # Admission proves a current failed proxy sample. It does not authorize
    # activation: the P1 worker must reverify and prepare a separate writer step.
    [ "$action" != servers:failover ] || ops_failover_current "$target" "$context" || ops_error FAILOVER_UNCONFIRMED
    ops_queue_load create
    digest="$(printf '%s\n' "$action" "$target" "$source" "$context" | sha256sum | cut -d ' ' -f 1)" || ops_error QUEUE_STATE_INVALID
    existing="$(printf '%s\n' "$OPS_Q_JSON" | jq -c --arg n "$nonce" '
      ([.receipts[]|select(.nonce==$n)]|first) //
      ([.requests[]|select(.requestId==("q-"+$n))]|first) // empty')" || ops_error QUEUE_STATE_INVALID
    if [ -n "$existing" ]; then
        [ "$(printf '%s\n' "$existing" | jq -r .inputHash)" = "$digest" ] || ops_error REQUEST_MISMATCH
        ops_queue_receipt "$nonce" || ops_error REQUEST_UNCONFIRMED
        return 0
    fi
    existing="$(printf '%s\n' "$OPS_Q_JSON" | jq -c --arg hash "$digest" '[.requests[]|select(.inputHash==$hash)]|first // empty')" || ops_error QUEUE_STATE_INVALID
    if [ -n "$existing" ]; then
        id="$(printf '%s\n' "$existing" | jq -r .requestId)"
        reply=true
    else
        count="$(printf '%s\n' "$OPS_Q_JSON" | jq '[.requests[]|select(.state=="queued")]|length')" || ops_error QUEUE_STATE_INVALID
        [ "$count" -lt 64 ] || ops_error QUEUE_FULL
        id="q-$nonce"; reply=false
        record="$(jq -nc --arg id "$id" --arg action "$action" --arg target "$target" --arg source "$source" --arg context "$context" --arg hash "$digest" --arg stage "$OPS_Q_STAGE" --argjson priority "$OPS_Q_PRIORITY" \
          '{requestId:$id,action:$action,targetId:$target,source:$source,context:$context,inputHash:$hash,stage:$stage,priority:$priority,state:"queued"}')" || ops_error QUEUE_STATE_INVALID
        [ "$(printf '%s\n' "$record" | wc -c)" -le 4096 ] || ops_error QUEUE_FULL
        OPS_Q_JSON="$(printf '%s\n' "$OPS_Q_JSON" | jq -c --argjson r "$record" '.sequence+=1 | .requests += [$r+{sequence:.sequence}]')" || ops_error QUEUE_STATE_INVALID
    fi
    OPS_Q_JSON="$(printf '%s\n' "$OPS_Q_JSON" | jq -c --arg n "$nonce" --arg hash "$digest" --arg id "$id" --argjson priority "$OPS_Q_PRIORITY" --argjson coalesced "$reply" \
       '.receipts=(.receipts+[{nonce:$n,inputHash:$hash,requestId:$id,state:"queued",priority:$priority,coalesced:$coalesced}] | .[-128:])')" || ops_error QUEUE_STATE_INVALID
    ops_queue_store
    ops_queue_receipt "$nonce" || ops_error REQUEST_UNCONFIRMED
}

ops_failover_current()
(
    local target context cache config hash operation now
    target="$1"; context="$2"
    cache="$OPS_APP/run/server-auto-switch-state.json"
    config="$OPS_APP/config/system/server-auto-switch.json"
    ops_publication_private "$cache" && ops_file_safe "$config" || return 1
    hash="$(sha256sum "$config" | cut -d ' ' -f 1)" || return 1
    now="$(date '+%s')" || return 1
    jq -e --arg target "$target" --arg context "$context" --arg hash "$hash" --argjson now "$now" --slurpfile config "$config" '
      def threshold: (.failureThreshold // 3)|tostring|
        if length>0 and all(explode[];.>=48 and .<=57) then tonumber|if .>=1 and .<=10 then . else 3 end else 3 end;
      ($config|length)==1 and ($config[0].enabled==true or $config[0].enabled=="true") and
      .schemaVersion==3 and .enabled==true and .autoConfigSha256==$hash and
      .activeServerId==$target and .lastProxyContext==$context and
      .activeHealth.method=="current-socks-https" and .activeHealth.serverId==$target and
      .activeHealth.status=="unhealthy" and .activeHealth.context==$context and
      (.activeHealth.checkedEpoch|type)=="number" and .activeHealth.checkedEpoch<=$now and
      ($now-.activeHealth.checkedEpoch)<=120 and
      (.consecutiveFailures|type)=="number" and (.consecutiveFailures|floor)==.consecutiveFailures and
      .consecutiveFailures>=($config[0]|threshold)
    ' "$cache" >/dev/null || return 1
    [ "$(ops_field_active_context "$target")" = "$context" ] || return 1
    operation="$(jq -er .backgroundOperationId "$cache")" || return 1
    ops_load "$operation" && ops_step_binding || return 1
    jq -e --arg target "$target" --arg context "$context" '
      .operation=="servers:active-health" and .queueStep.stage=="checking" and
      .bundleId==$target and .queueStep.context==$context and .acknowledged==true
    ' "$OPS_CURRENT/state.json" >/dev/null || return 1
    ops_publication_load || return 1
    printf '%s\n' "$OPS_PUB_RECORD" | jq -e --slurpfile cache "$cache" '
      .kind=="field" and .resource=="active-state" and .complete==true and .outcome==.newHash and
      (.payload|to_entries|all(.[]; .value==$cache[0][.key]))' >/dev/null
)

ops_queue_lookup()
{
    ops_nonce_valid "$1" || ops_error INVALID_REQUEST 1
    ops_queue_load
    ops_queue_receipt "$1" || ops_error REQUEST_UNCONFIRMED
}

ops_queue_public()
{
    # Read-only projection: never initializes, recovers or prunes evidence.
    ops_queue_load
    printf '%s\n' "$OPS_Q_JSON" | jq -c -L "$OPS_CODE/lib" --argjson paused "$1" '
      include "operation-public";
      .requests as $live |
      (($live|sort_by(.priority,.sequence)|map(
        .reason=(if .state!="queued" then null
          elif $paused and .source!="USER" then "automation_paused"
          elif any($live[];.priority<2 and ($paused==false or .source=="USER" or .state=="running")) and .priority>=2 then "active_connection"
          else "awaiting_resource" end))) +
       ([.receipts[]|select(.state=="completed" or .state=="cancelled" or .state=="failed")|
         select(.requestId as $id|all($live[];.requestId!=$id))] |
        unique_by(.requestId)|map(.stage="finished"))) | map(queue_public)'
}

ops_queue_select()
{
    ops_queue_load
    ops_queue_select_loaded
}

# Internal selection within the same guarded call that validated OPS_Q_JSON.
# queue-next still loads afresh; claim need not parse its ledger a second time.
ops_queue_select_loaded()
{
    local paused observer prepare writer
    paused=false
    if [ -e "$OPS_AUTOMATION" ] || [ -L "$OPS_AUTOMATION" ]; then
        ops_file_safe "$OPS_AUTOMATION" 4096 || ops_error AUTOMATION_STATE_INVALID
        paused="$(jq -er 'select((.paused|type)=="boolean")|.paused|tostring' "$OPS_AUTOMATION")" || ops_error AUTOMATION_STATE_INVALID
    fi
    observer=true; prepare=true; writer=true
    [ ! -e "$OPS_RAM/resources/active-observer" ] && [ ! -L "$OPS_RAM/resources/active-observer" ] || observer=false
    [ ! -e "$OPS_RAM/resources/background-prepare" ] && [ ! -L "$OPS_RAM/resources/background-prepare" ] || prepare=false
    [ ! -e "$OPS_GLOBAL" ] && [ ! -L "$OPS_GLOBAL" ] || writer=false
    if [ "$writer" = false ]; then
        ops_step_global_compatible active-observer || observer=false
        ops_step_global_compatible background-prepare || prepare=false
    fi
    # Every queued global stage changes runtime/catalog state. Select it only
    # when the same exclusion required by ops_begin can actually be granted.
    [ "$observer" = true ] && [ "$prepare" = true ] || writer=false
    printf '%s\n' "$OPS_Q_JSON" | jq -c --argjson paused "$paused" --argjson observer "$observer" --argjson prepare "$prepare" --argjson writer "$writer" '
      .fairCursor as $cursor |
      ([.requests[] | select(.state=="queued" and ($paused==false or .source=="USER") and
        .priority<3 and (.stage=="apply" or .stage=="activate"))] |
        sort_by(.priority,.sequence) | first) as $reservation |
      [.requests[] | select(.state=="queued" and ($paused==false or .source=="USER")) |
        select(if .action=="servers:active-health" then $observer
               elif .stage=="apply" or .stage=="activate" then $writer
               else $prepare and ($reservation==null or
                 [.priority,.sequence]<[$reservation.priority,$reservation.sequence]) end)] as $ready |
      ([$ready[]|select(.priority<3)]|sort_by(.priority,.sequence)|first) as $urgent |
      ([range(0;6) | [3,3,3,4,4,5][(($cursor+.)%6)] as $p |
        ($ready|map(select(.priority==$p))|sort_by(.sequence)|first) | select(.!=null)]|first) as $background |
      ($urgent // $background) as $r |
      if $r==null then {ok:true,requestId:null} else {ok:true}+($r|{requestId,action,targetId,source,context,priority,stage,state}) end'
}

ops_step_resource()
{
    # Only the coordinator chooses rights. A queue record cannot grant rights
    # by claiming a resource name or a caller-supplied priority.
    ops_queue_policy "$1" "$2" || return 1
    OPS_STEP_RESOURCE=background-prepare; OPS_STEP_PHASE=checking
    case "$1:$3" in
      servers:active-health:checking) OPS_STEP_RESOURCE=active-observer ;;
      servers:quality:probe|servers:failover:verify|servers:failover:probe|dot:auto-check:probe) ;;
      subscriptions:refresh:fetch) OPS_STEP_PHASE=fetching ;;
      subscriptions:refresh:parse) OPS_STEP_PHASE=parsing ;;
      subscriptions:refresh:apply) OPS_STEP_RESOURCE=global; OPS_STEP_PHASE=committing ;;
      servers:failover:activate) OPS_STEP_RESOURCE=global; OPS_STEP_PHASE=switching ;;
      *) return 1 ;;
    esac
}

ops_is_queue_step()
{
    jq -e 'has("queueStep")' "$OPS_CURRENT/state.json" >/dev/null
}

ops_step_binding()
{
    local action source stage binding priority resource
    binding="$(jq -er '
      select(.queueStep.schemaVersion==1 and (.queueStep.requestId|type)=="string" and
        (.queueStep.requestId|startswith("q-")) and .scope=="system" and
        (.queueStep.priority|type)=="number" and (.queueStep.priority|floor)==.queueStep.priority and
        (.resourceLocks|type)=="array" and (.resourceLocks|length)==1) |
      [.operation,.source,.queueStep.stage,(.queueStep.priority|tostring),.resourceLocks[0]] |
      select(all(.[];type=="string" and length>0 and all(explode[];
        (.>=65 and .<=90) or (.>=97 and .<=122) or (.>=48 and .<=57) or IN(45,58,95)))) |
      join("|")' "$OPS_CURRENT/state.json")" || return 1
    IFS='|' read -r action source stage priority resource <<EOF_STEP_BINDING
$binding
EOF_STEP_BINDING
    ops_step_resource "$action" "$source" "$stage" || return 1
    [ "$priority" = "$OPS_Q_PRIORITY" ] && [ "$resource" = "$OPS_STEP_RESOURCE" ]
}

ops_resources_match()
{
    local pointer
    if ! ops_is_queue_step; then ops_global_matches; return $?; fi
    ops_step_binding || return 1
    if [ "$OPS_STEP_RESOURCE" = global ]; then ops_global_matches; return $?; fi
    case "$OPS_CURRENT" in "$OPS_RAM/steps/"*) ;; *) return 1 ;; esac
    pointer="$OPS_RAM/resources/$OPS_STEP_RESOURCE"
    [ -L "$pointer" ] && [ "$(readlink "$pointer")" = "$OPS_CURRENT/fence" ] &&
      ops_dir_safe "$OPS_CURRENT/fence" && ops_file_safe "$pointer/owner.json" 4096 &&
      cmp -s "$pointer/owner.json" "$OPS_CURRENT/owner.json"
}

ops_step_permission()
{
    ops_is_queue_step || return 0
    ops_step_binding || return 1
    case "$1" in
      owner-check|ack|helpers-drain|finish|queue-yield) return 0 ;;
      supervisor-register) [ "$OPS_STEP_RESOURCE" != global ]; return $? ;;
      tick) [ "$2" = working ] || [ "$2" = "$OPS_STEP_PHASE" ]; return $? ;;
      *) return 1 ;;
    esac
}

ops_retire_resources()
{
    local pointer
    ops_resources_match || return 1
    if ! ops_is_queue_step || [ "$OPS_STEP_RESOURCE" = global ]; then ops_retire_global; return $?; fi
    pointer="$OPS_RAM/resources/$OPS_STEP_RESOURCE"
    [ ! -e "$OPS_CURRENT/retired-lock" ] && [ ! -L "$OPS_CURRENT/retired-lock" ] || return 1
    mv "$pointer" "$OPS_CURRENT/retired-lock"
}

ops_step_retired_valid()
{
    # Exact retirement receipt; missing resource alone is never proof.
    ops_step_binding || return 1
    [ -L "$OPS_CURRENT/retired-lock" ] &&
      [ "$(readlink "$OPS_CURRENT/retired-lock")" = "$OPS_CURRENT/fence" ] &&
      ops_dir_safe "$OPS_CURRENT/fence" &&
      ops_queue_private "$OPS_CURRENT/fence/owner.json" 4096 &&
      cmp -s "$OPS_CURRENT/fence/owner.json" "$OPS_CURRENT/owner.json"
}

ops_step_allows_observation()
{
    # Explicitly read/config-only legacy actions. Unknown and monolithic
    # scheduler actions may activate Xray and require runtime exclusion.
    case "$1" in
      check|download|build-export|verify|plan|export|delete|resume|custom:*|preflight:*|dot:*|servers:check|servers:quality) return 0 ;;
      *) return 1 ;;
    esac
}

ops_step_runtime_gate()
{
    local resource
    # Normal route/config work may overlap a read-only observer/preparation.
    # Runtime replacement/activation may not invalidate a running helper.
    if ops_step_allows_observation "$1"; then
        case "$1" in
          servers:check|servers:quality|dot:*)
            [ ! -e "$OPS_RAM/resources/background-prepare" ] && [ ! -L "$OPS_RAM/resources/background-prepare" ]
            return $? ;;
          *) return 0 ;;
        esac
    fi
    for resource in active-observer background-prepare; do
        [ ! -e "$OPS_RAM/resources/$resource" ] && [ ! -L "$OPS_RAM/resources/$resource" ] || return 1
    done
}

ops_step_global_compatible()
{
    local target id action
    [ -e "$OPS_GLOBAL" ] || [ -L "$OPS_GLOBAL" ] || return 0
    # Unknown/legacy global owners never authorize concurrent work.
    [ -L "$OPS_GLOBAL" ] || return 1
    target="$(readlink "$OPS_GLOBAL")"
    case "$target" in "$OPS_ROOT/"*/fence) ;; *) return 1 ;; esac
    id="${target#"$OPS_ROOT/"}"; id="${id%/fence}"
    ops_id_valid "$id" && ops_file_safe "$OPS_ROOT/$id/state.json" || return 1
    action="$(jq -er 'select(.running==true and .acknowledged==true)|.operation' "$OPS_ROOT/$id/state.json")" || return 1
    ops_step_allows_observation "$action" || return 1
    case "$action:$1" in
      servers:check:background-prepare|servers:quality:background-prepare|dot:*:background-prepare) return 1 ;;
    esac
    # Compare the exact managed fence independently of the queue owner.
    (ops_load "$id" && ops_global_matches)
}

ops_queue_request_tree_safe()
(
    local file uid
    set -o pipefail
    [ -d "$1" ] && [ ! -L "$1" ] && [ "$(readlink -f "$1")" = "$1" ] || exit 1
    uid="$(id -u)" || exit 1
    [ -z "$(find "$1" ! -type d ! -type f -print)" ] || exit 1
    find "$1" -type d -print | while IFS= read -r file; do
        [ "$(broray_ops_file_stat -c '%u:%a' "$file")" = "$uid:700" ] || exit 1
    done || exit 1
    find "$1" -type f -print | while IFS= read -r file; do
        [ "$(broray_ops_file_stat -c '%u:%a:%h' "$file")" = "$uid:600:1" ] || exit 1
    done
)

ops_queue_prune_requests()
(
    local directory request record id found valid owner pointer index matches
    [ -e "$OPS_RAM/requests" ] || [ -L "$OPS_RAM/requests" ] || exit 0
    ops_dir_safe "$OPS_RAM/requests" && [ "$(readlink -f "$OPS_RAM/requests")" = "$OPS_RAM/requests" ] || exit 1
    # Build a read-only index once per guarded claim, not two jq processes
    # for every request x operation pair. Unknown evidence still prevents
    # cleanup. The index grants no authority: each matching owner and its
    # children/publication/retirement proof are checked below before removal.
    set --
    for record in "$OPS_RAM/steps"/*/state.json "$OPS_ROOT"/*/state.json; do
        [ -e "$record" ] || [ -L "$record" ] || continue
        ops_file_safe "$record" 32768 || exit 0
        set -- "$@" "$record"
    done
    [ "$#" -gt 0 ] || exit 0
    index="$(jq -nc '[inputs | if type=="object" then
      {path:input_filename,request:.queueStep.requestId} else error("unknown operation record") end]' "$@")" || exit 0
    for directory in "$OPS_RAM/requests"/*; do
        [ -e "$directory" ] || [ -L "$directory" ] || continue
        request="${directory##*/}"
        case "$request" in q-*) ;; *) continue ;; esac
        ops_nonce_valid "${request#q-}" || continue
        # Missing receipt, lost settlement, another stage or a live owner is
        # never authority to remove RAM evidence. No timestamp participates.
        printf '%s\n' "$OPS_Q_JSON" | jq -e --arg r "$request" '
          (any(.requests[];.requestId==$r)|not) and
          ([.receipts[]|select(.requestId==$r)] | length>0 and
            all(.[];.state|IN("completed","cancelled","failed")))' >/dev/null || continue
        found=0; valid=true
        matches="$(printf '%s\n' "$index" | jq -r --arg r "$request" '.[]|select(.request==$r)|.path')" || exit 0
        while IFS= read -r record; do
            [ -n "$record" ] || continue
            found=$((found+1)); id="${record%/state.json}"; id="${id##*/}"
            ops_load "$id" || { valid=false; break; }
            owner="$(jq -c .owner "$OPS_EXECUTOR")" || { valid=false; break; }
            broray_ops_classify_owner "$owner"
            [ "$OPS_OWNER_STATUS" = STALE ] || { valid=false; break; }
            ops_step_retired_valid &&
              jq -e '.running==false and .phase=="finished" and (.state|IN("completed","failed","aborted","recovered"))' "$OPS_CURRENT/state.json" >/dev/null &&
              ops_children_absent && ops_publication_ready || { valid=false; break; }
            pointer="$OPS_RAM/resources/$OPS_STEP_RESOURCE"
            [ "$OPS_STEP_RESOURCE" != global ] || pointer="$OPS_GLOBAL"
            if [ -L "$pointer" ] && [ "$(readlink "$pointer")" = "$OPS_CURRENT/fence" ]; then valid=false; break; fi
        done <<EOF_REQUEST_OWNERS
$matches
EOF_REQUEST_OWNERS
        [ "$valid" = true ] && [ "$found" -gt 0 ] || continue
        ops_queue_request_tree_safe "$directory" || continue
        # Exact request root, terminal receipt, retired owners and drained
        # children verified above. Small operation/queue receipts are retained.
        rm -rf "$directory" || exit 1
    done
)

ops_queue_prune_history()
(
    local file id count owner stamp kept pointer line
    [ -e "$OPS_RAM/steps" ] || [ -L "$OPS_RAM/steps" ] || return 0
    ops_dir_safe "$OPS_RAM/steps" || return 1
    count=0
    for file in "$OPS_RAM/steps"/*; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        count=$((count+1))
    done
    [ "$count" -le 128 ] || return 1
    [ "$count" -gt 24 ] || return 0
    kept=0
    set --
    for file in "$OPS_RAM/steps"/*/state.json; do
        ops_queue_private "$file" 32768 || continue
        set -- "$@" "$file"
    done
    [ "$#" -gt 0 ] || return 0
    # Coordinator records are compact JSON. This batch reader is only an
    # ordering hint: malformed/pretty records are kept, and removal below
    # always validates the complete file and exact ownership independently.
    for file in "$@"; do
        while IFS= read -r line || [ -n "$line" ]; do printf '%s\n' "$line"; done <"$file"
    done | jq -Rrc 'fromjson? | select(.kind=="background" and .running==false and .phase=="finished" and
      (.state|IN("completed","failed","aborted","recovered"))) | [.finishedAt,.operationId]|@tsv' |
      sort -r | while IFS="$(printf '\t')" read -r stamp id; do
        # Retained rows need no ownership/child probes: they authorize no
        # cleanup. Validate full authority only for actual removal candidates.
        kept=$((kept+1))
        [ "$kept" -gt 20 ] || continue
        ops_load "$id" && ops_step_retired_valid || continue
        if printf '%s\n' "$OPS_Q_JSON" | jq -e --arg id "$id" 'any(.requests[];.operationId==$id)' >/dev/null; then continue; fi
        pointer="$OPS_RAM/resources/$OPS_STEP_RESOURCE"
        [ ! -L "$pointer" ] || [ "$(readlink "$pointer")" != "$OPS_CURRENT/fence" ] || continue
        ops_children_absent && ops_publication_ready || continue
        owner="$(jq -c .owner "$OPS_EXECUTOR")"; broray_ops_classify_owner "$owner"
        [ "$OPS_OWNER_STATUS" = STALE ] || continue
        # The direct child, identity and retirement receipt were checked above.
        rm -rf "$OPS_RAM/steps/$id" || return 1
        [ -L "$OPS_RAM/$id.json" ] || rm -f "$OPS_RAM/$id.json"
    done
)

ops_queue_claim()
{
    local request launch pid row owner action source stage priority resource selected id token directory record state step result fence
    request="$1"; launch="$2"; pid="$3"
    ops_id_valid "$request" && ops_nonce_valid "$launch" || ops_error INVALID_REQUEST 1
    ops_queue_load
    row="$(printf '%s\n' "$OPS_Q_JSON" | jq -ce --arg id "$request" '.requests[]|select(.requestId==$id)')" || ops_error REQUEST_UNCONFIRMED
    owner="$(broray_ops_capture_owner "$pid")" || ops_error OWNER_UNCONFIRMED
    if [ "$(printf '%s\n' "$row" | jq -r '.operationId // empty')" != '' ]; then
        id="$(printf '%s\n' "$row" | jq -r .operationId)"
        ops_load "$id" || ops_error STATE_UNAVAILABLE
        jq -e --arg launch "$launch" --argjson owner "$owner" '.launchNonce==$launch and .owner==$owner' "$OPS_EXECUTOR" >/dev/null || ops_error OWNER_CHANGED
        ops_resources_match || ops_error OWNER_CHANGED
        jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null || ops_error OPERATION_FINISHED
        ops_queue_claim_reply
        return 0
    fi
    action="$(printf '%s\n' "$row" | jq -r .action)"; source="$(printf '%s\n' "$row" | jq -r .source)"
    stage="$(printf '%s\n' "$row" | jq -r .stage)"
    ops_step_resource "$action" "$source" "$stage" || ops_error QUEUE_STATE_INVALID
    priority="$OPS_Q_PRIORITY"; resource="$OPS_STEP_RESOURCE"
    printf '%s\n' "$row" | jq -e --argjson p "$priority" '.state=="queued" and .priority==$p' >/dev/null || ops_error QUEUE_STATE_INVALID
    step="$(printf '%s\n' "$row" | jq -c '{schemaVersion:1,requestId,stage,priority,context} +
      (if has("resultSha256") then {resultSha256} else {} end)')" || ops_error QUEUE_STATE_INVALID
    # Resume only the exact never-acknowledged launch whose fence was already
    # published before a lost queue-store response. No new owner is created.
    id="op-q-$launch"; directory="$OPS_RAM/steps/$id"
    if [ "$resource" = global ] && [ -L "$OPS_GLOBAL" ]; then
        fence="$(readlink "$OPS_GLOBAL")"
        case "$fence" in "$OPS_ROOT/"*/fence)
            id="${fence#"$OPS_ROOT/"}"; id="${id%/fence}"
            ops_id_valid "$id" || ops_error OWNER_CHANGED
            directory="$OPS_ROOT/$id" ;;
          *) ops_error OWNER_CHANGED ;;
        esac
    fi
    if [ -e "$directory" ] || [ -L "$directory" ]; then
        ops_load "$id" || ops_error STATE_UNAVAILABLE
        jq -e --arg launch "$launch" --argjson owner "$owner" '.launchNonce==$launch and .owner==$owner' "$OPS_EXECUTOR" >/dev/null || ops_error OWNER_CHANGED
        jq -e --argjson step "$step" --arg action "$action" --arg source "$source" \
          '.queueStep==$step and .operation==$action and .source==$source and .state=="starting" and .acknowledged==false' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
        ops_resources_match || ops_error OWNER_CHANGED
        ops_children_absent || ops_error CHILDREN_UNCONFIRMED
        ops_queue_bind_claim "$request" "$id" "$priority"
        ops_queue_claim_reply
        return 0
    fi
    [ "$resource" = global ] || {
        [ ! -e "$OPS_RAM/resources/$resource" ] && [ ! -L "$OPS_RAM/resources/$resource" ] || ops_error RESOURCE_BUSY
        ops_step_global_compatible "$resource" || ops_error OPERATION_BUSY
    }
    ops_pending_domain && ops_error DOMAIN_OPERATION_BUSY
    selected="$(ops_queue_select_loaded)" || ops_error QUEUE_STATE_INVALID
    [ "$(printf '%s\n' "$selected" | jq -r '.requestId // empty')" = "$request" ] || ops_error REQUEST_DEFERRED
    # Refused/duplicate launches must not hold the guard cleaning unrelated
    # payloads. Maintenance still runs before a genuinely new owner is written
    # and retains the exact same retirement/identity/child safety predicates.
    ops_queue_prune_requests || ops_error HISTORY_LIMIT
    ops_queue_prune_history || ops_error HISTORY_LIMIT
    if [ "$resource" = global ]; then
        OPS_QUEUE_BEGIN="$step"
        result="$(ops_begin system "$action" "$(printf '%s\n' "$row" | jq -r .targetId)" "$source" "$pid" protected "$launch")" || { printf '%s\n' "$result"; exit 2; }
        OPS_QUEUE_BEGIN=null
        id="$(printf '%s\n' "$result" | jq -er .operationId)" || ops_error STATE_UNAVAILABLE
        ops_load "$id" || ops_error STATE_UNAVAILABLE
    else
        id="op-q-$launch"; directory="$OPS_RAM/steps/$id"
        for fence in "$OPS_RAM/steps" "$OPS_RAM/resources" "$OPS_RAM/requests" "$OPS_RAM/requests/$request"; do
            if [ ! -e "$fence" ] && [ ! -L "$fence" ]; then mkdir -m 700 "$fence" || ops_error UNSAFE_STATE; fi
            ops_dir_safe "$fence" && [ "$(broray_ops_file_stat -c '%u:%a' "$fence")" = "$(id -u):700" ] || ops_error UNSAFE_STATE
        done
        # A failed launch remains inspectable and cannot silently be reused.
        [ ! -e "$directory" ] && [ ! -L "$directory" ] || ops_error OPERATION_EXISTS
        mkdir -m 700 "$directory" || ops_error STATE_UNAVAILABLE
        token="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || ops_error RANDOM_UNAVAILABLE
        ops_nonce_valid "$token" || ops_error RANDOM_UNAVAILABLE
        record="$(jq -nc --arg id "$id" --arg token "$token" --arg launch "$launch" --argjson owner "$owner" '{schemaVersion:2,operationId:$id,token:$token,launchNonce:$launch,owner:$owner}')" || ops_error STATE_UNAVAILABLE
        state="$(jq -nc --arg id "$id" --arg action "$action" --arg source "$source" --arg bundle "$(printf '%s\n' "$row" | jq -r .targetId)" --arg resource "$resource" --arg now "$(ops_now)" --argjson step "$step" \
          '{schemaVersion:2,kind:"background",operationId:$id,operation:$action,type:$action,source:$source,scope:"system",bundleId:$bundle,queueStep:$step,
            state:"starting",phase:"starting",running:true,revision:1,resourceLocks:[$resource],cancelRequested:false,cancelability:"cooperative",initialCancelability:"cooperative",acknowledged:false,
            startedAt:$now,updatedAt:$now,finishedAt:null,errorCode:null}')" || ops_error STATE_UNAVAILABLE
        ops_write "$directory/owner.json" "$record" && ops_write "$directory/state.json" "$state" || ops_error STATE_UNAVAILABLE
        mkdir -m 700 "$directory/fence" || ops_error STATE_UNAVAILABLE
        ops_write "$directory/fence/owner.json" "$record" || ops_error STATE_UNAVAILABLE
        printf '%s\n' "$pid" >"$directory/fence/pid"
        printf '%s\n' system >"$directory/fence/scope"
        printf '%s\n' "$action" >"$directory/fence/action"
        printf '%s\n' "$(printf '%s\n' "$row" | jq -r .targetId)" >"$directory/fence/bundle"
        printf '%s\n' "$(ops_now)" >"$directory/fence/startedAt"
        "$OPS_GUARD" --publish-fence "$directory/fence" "$OPS_RAM/resources/$resource" || ops_error OWNER_PUBLICATION_FAILED
        ops_load "$id" || ops_error STATE_UNAVAILABLE
    fi
    ops_queue_bind_claim "$request" "$id" "$priority"
    ops_queue_claim_reply
}

ops_queue_bind_claim()
{
    OPS_Q_JSON="$(printf '%s\n' "$OPS_Q_JSON" | jq -c --arg r "$1" --arg op "$2" --argjson p "$3" '
      .requests|=map(if .requestId==$r then .state="running"|.operationId=$op else . end) |
      .receipts|=map(if .requestId==$r then .state="running" else . end) |
      if $p>=3 then .fairCursor as $c |
        .fairCursor=([range(0;6)|select([3,3,3,4,4,5][(($c+.)%6)]==$p)|(($c+.+1)%6)]|first)
      else . end')" || ops_error QUEUE_STATE_INVALID
    ops_queue_store
}

ops_queue_claim_reply()
{
    jq -nc --slurpfile owner "$OPS_EXECUTOR" --slurpfile state "$OPS_CURRENT/state.json" --arg directory "$OPS_CURRENT" \
      '$state[0] as $s | {ok:true,operationId:$s.operationId,token:$owner[0].token,requestId:$s.queueStep.requestId,
        stage:$s.queueStep.stage,priority:$s.queueStep.priority,resourceLocks:$s.resourceLocks,operationDirectory:$directory}'
}

ops_queue_step_finish()
{
    local request next digest record state row
    # Idempotent terminal settlement. Caller already checked token and drain.
    request="$(jq -er .queueStep.requestId "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE
    ops_queue_load
    state="$(jq -r .state "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE
    if jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null; then
        ops_resources_match || ops_error OWNER_CHANGED
        ops_state_transition "$1" finished "${2:-}" || ops_error STATE_UNAVAILABLE
        state="$1"
    fi
    if ops_resources_match; then ops_retire_resources || ops_error STATE_UNAVAILABLE
    else ops_step_retired_valid || ops_error OWNER_CHANGED; fi
    row="$(printf '%s\n' "$OPS_Q_JSON" | jq -c --arg request "$request" '.requests[]|select(.requestId==$request)')" || ops_error QUEUE_STATE_INVALID
    [ -n "$row" ] || { printf '%s\n' '{"ok":true,"alreadyFinished":true}'; return 0; }
    if [ "$(printf '%s\n' "$row" | jq -r '.operationId // empty')" != "$OPS_ID" ]; then
        # A later stage already owns this request; never replay old settlement.
        printf '%s\n' '{"ok":true,"alreadyFinished":true}'; return 0
    fi
    next="$(jq -r '.queueYield.nextStage // empty' "$OPS_CURRENT/state.json")"
    digest="$(jq -r '.queueYield.resultSha256 // empty' "$OPS_CURRENT/state.json")"
    if [ -n "$next" ] && [ "$state" = completed ]; then
        OPS_Q_JSON="$(printf '%s\n' "$OPS_Q_JSON" | jq -c --arg request "$request" --arg next "$next" --arg digest "$digest" \
          '.requests|=map(if .requestId==$request then .state="queued"|.stage=$next|.resultSha256=$digest|del(.operationId) else . end) |
           .receipts|=map(if .requestId==$request then .state="queued" else . end)')" || ops_error QUEUE_STATE_INVALID
    else
        OPS_Q_JSON="$(printf '%s\n' "$OPS_Q_JSON" | jq -c --arg request "$request" --arg state "$state" \
          '.requests|=map(select(.requestId!=$request)) | .receipts|=map(if .requestId==$request then .state=(if $state=="aborted" then "cancelled" elif $state=="recovered" then "failed" else $state end) else . end)')" || ops_error QUEUE_STATE_INVALID
    fi
    ops_queue_store
    printf '%s\n' '{"ok":true}'
}

ops_queue_yield()
{
    local request action stage result digest
    ops_load "$1" || ops_error STATE_UNAVAILABLE
    [ "$(jq -r .token "$OPS_EXECUTOR")" = "$2" ] || ops_error OWNER_CHANGED
    ops_owner_authorize "$3"
    ops_is_queue_step && ops_step_binding || ops_error STEP_PERMISSION_DENIED
    if jq -e 'has("queueYield")' "$OPS_CURRENT/state.json" >/dev/null; then
        jq -e --arg next "$4" --arg digest "$5" '.queueYield.nextStage==$next and .queueYield.resultSha256==$digest' "$OPS_CURRENT/state.json" >/dev/null || ops_error OWNER_CHANGED
    else
        ops_authorize "$1" "$2"
        ops_resources_match || ops_error OWNER_CHANGED
        jq -e '.acknowledged==true' "$OPS_CURRENT/state.json" >/dev/null || ops_error NOT_ACKNOWLEDGED
        action="$(jq -r .operation "$OPS_CURRENT/state.json")"; stage="$(jq -r .queueStep.stage "$OPS_CURRENT/state.json")"
        case "$action:$stage:$4" in
          subscriptions:refresh:fetch:parse|subscriptions:refresh:parse:apply|servers:quality:probe:probe|dot:auto-check:probe:probe|servers:failover:verify:probe|servers:failover:probe:probe|servers:failover:probe:activate) ;;
          *) ops_error STEP_PERMISSION_DENIED ;;
        esac
        ops_queue_sha_valid "$5" || ops_error INVALID_REQUEST
        request="$(jq -r .queueStep.requestId "$OPS_CURRENT/state.json")"
        result="$OPS_RAM/requests/$request/result.json"
        ops_queue_private "$result" 4194304 || ops_error RESULT_UNCONFIRMED
        digest="$(sha256sum "$result" | cut -d ' ' -f 1)" || ops_error RESULT_UNCONFIRMED
        [ "$digest" = "$5" ] || ops_error RESULT_UNCONFIRMED
        ops_children_absent || ops_error CHILDREN_UNCONFIRMED
        ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED
        ops_queue_load
        ops_write "$OPS_CURRENT/state.json" "$(jq -c --arg next "$4" --arg digest "$5" '.queueYield={nextStage:$next,resultSha256:$digest}' "$OPS_CURRENT/state.json")" || ops_error STATE_UNAVAILABLE
    fi
    ops_children_absent || ops_error CHILDREN_UNCONFIRMED
    ops_queue_step_finish completed
}

ops_queue_recover()
{
    local resource pointer target id status verified_live
    verified_live=''
    ops_queue_load
    for resource in active-observer background-prepare; do
        pointer="$OPS_RAM/resources/$resource"
        [ -e "$pointer" ] || [ -L "$pointer" ] || continue
        [ -L "$pointer" ] || ops_error OWNER_UNCONFIRMED
        target="$(readlink "$pointer")"
        case "$target" in "$OPS_RAM/steps/"*/fence) ;; *) ops_error OWNER_UNCONFIRMED ;; esac
        id="${target#"$OPS_RAM/steps/"}"; id="${id%/fence}"
        ops_load "$id" && ops_resources_match || ops_error OWNER_UNCONFIRMED
        if jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null; then
            broray_ops_classify_owner "$(jq -c .owner "$OPS_EXECUTOR")"
            case "$OPS_OWNER_STATUS" in
              ACTIVE) verified_live="$verified_live $id"; continue ;;
              STALE) ;;
              *) ops_error OWNER_UNCONFIRMED ;;
            esac
        fi
        ops_children_absent || ops_error CHILDREN_UNCONFIRMED
        ops_publication_recover || ops_error PUBLICATION_UNCONFIRMED
        # No signals. The existing identity + child-accounting contract proved
        # absence; a lost user result is failed/aborted, never silently rerun.
        ops_queue_step_finish aborted OWNER_DISAPPEARED >/dev/null
    done
    # Settlement can crash after releasing the pointer but before queue-store.
    # Reconcile the exact terminal owner; never grant or replay its work.
    for id in $(printf '%s\n' "$OPS_Q_JSON" | jq -r '.requests[]|select(.state=="running")|.operationId'); do
        # These exact RAM owners and resource bindings were already checked
        # above under this same guard. No persistent cache or PID-only shortcut.
        # Missing pointers, terminal settlements and global steps still run
        # the full path below.
        case " $verified_live " in *" $id "*) continue ;; esac
        ops_load "$id" && ops_step_binding || ops_error OWNER_UNCONFIRMED
        if jq -e '.running==true' "$OPS_CURRENT/state.json" >/dev/null; then
            ops_resources_match || ops_error OWNER_UNCONFIRMED
            continue
        fi
        ops_step_retired_valid || ops_error OWNER_UNCONFIRMED
        ops_children_absent || ops_error CHILDREN_UNCONFIRMED
        ops_publication_ready || ops_error PUBLICATION_UNCONFIRMED
        ops_queue_step_finish "$(jq -r .state "$OPS_CURRENT/state.json")" >/dev/null
    done
    printf '%s\n' '{"ok":true}'
}

ops_queue_cancel()
{
    local id state
    id="$1"; ops_id_valid "$id" || ops_error INVALID_REQUEST 1
    ops_queue_load
    state="$(printf '%s\n' "$OPS_Q_JSON" | jq -r --arg id "$id" '.requests[]|select(.requestId==$id)|.state')" || ops_error QUEUE_STATE_INVALID
    if [ -z "$state" ]; then
        printf '%s\n' "$OPS_Q_JSON" | jq -e --arg id "$id" 'any(.receipts[];.requestId==$id and .state=="cancelled")' >/dev/null || ops_error REQUEST_UNCONFIRMED
    else
        [ "$state" = queued ] || ops_error OPERATION_BUSY
        OPS_Q_JSON="$(printf '%s\n' "$OPS_Q_JSON" | jq -c --arg id "$id" '.requests|=map(select(.requestId!=$id)) | .receipts|=map(if .requestId==$id then .state="cancelled" else . end)')" || ops_error QUEUE_STATE_INVALID
        ops_queue_store
    fi
    jq -nc --arg id "$id" '{ok:true,requestId:$id,state:"cancelled"}'
}
