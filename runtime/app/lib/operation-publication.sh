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
    action="$(jq -r '.operation' "$OPS_CURRENT/state.json")" || return 1
    case "$resource:$action" in
      auto-state:auto-switch)
        [ -z "$key" ] || return 1
        OPS_PUB_TARGET="$OPS_APP/run/server-auto-switch-state.json" ;;
      server-quality:servers:check|server-quality:auto-switch)
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
    ops_global_matches || ops_error OWNER_CHANGED
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
