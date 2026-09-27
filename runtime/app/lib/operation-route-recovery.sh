#!/opt/bin/ash
# Existing route repair remains a page action. This retires only dead job state.
ops_route_history_plain_file()
{
    [ -f "$1" ] && [ ! -L "$1" ] &&
      [ "$(find "$1" -maxdepth 0 -type f -links 1 -print)" = "$1" ]
}

ops_route_history_entry()
{
    local entry file child nested name bundle operation
    entry="$1"; name="${entry##*/}"
    case "$name" in ''|*[!a-zA-Z0-9._-]*) return 1 ;; esac
    [ ! -L "$entry" ] || return 1
    if [ -d "$entry" ]; then
        [ "$(readlink -f "$entry")" = "$entry" ] || return 1
        file="$entry/transaction.json"
        # These are the snapshots emitted by routes-router-sync.sh. Unknown
        # entries are evidence, not garbage; preserve the entire directory.
        for child in "$entry"/* "$entry"/.[!.]* "$entry"/..?*; do
            [ -e "$child" ] || [ -L "$child" ] || continue
            case "${child##*/}" in
                transaction.json|plan.json|running-config-before.json|running-config-after.json)
                    ops_route_history_plain_file "$child" || return 1 ;;
                original)
                    [ -d "$child" ] && [ ! -L "$child" ] || return 1
                    for nested in "$child"/* "$child"/.[!.]* "$child"/..?*; do
                        [ -e "$nested" ] || [ -L "$nested" ] || continue
                        case "${nested##*/}" in routes.json|bundle.json|state.json|export-plan.json|router-export-result.json|result.missing) ;; *) return 1 ;; esac
                        ops_route_history_plain_file "$nested" || return 1
                    done ;;
                *) return 1 ;;
            esac
        done
    else file="$entry"; fi
    ops_route_history_plain_file "$file" && [ "$(wc -c <"$file")" -le 16384 ] || return 1
    jq -e 'type=="object" and .schemaVersion==1 and .phase=="committed" and
      (.operation=="sync" or .operation=="export" or .operation=="delete") and
      (.bundleId|type)=="string" and (.bundleId|length)>0 and (.bundleId|length)<=63 and
      (.bundleId|all(explode[]; (.>=97 and .<=122) or (.>=48 and .<=57) or .==45 or .==95)) and
      (.updatedAt|type)=="string"' "$file" >/dev/null || return 1
    bundle="$(jq -r .bundleId "$file")"; operation="$(jq -r .operation "$file")"
    case "$operation:$name" in
        sync:????????-??????-sync-"$bundle"-*) [ -d "$entry" ] || return 1 ;;
        export:export-"$bundle"-*.json|delete:delete-"$bundle"-*.json) [ -f "$entry" ] || return 1 ;;
        *) return 1 ;;
    esac
    # The router writer uses %z (+0300); fixtures/UTC writers also use Z.
    # Validate the complete timestamp and compare UTC seconds across offsets.
    jq -er '
      def digits: length>0 and all(explode[]; .>=48 and .<=57);
      .updatedAt as $t |
      select(($t|length)==20 and $t[19:]=="Z" or
        (($t|length)==24 and ($t[19:20]=="+" or $t[19:20]=="-") and
         ($t[20:24]|digits) and ($t[20:22]|tonumber)<24 and ($t[22:24]|tonumber)<60)) |
      ($t[0:19]+"Z" | fromdateiso8601) as $base |
      select(($base|strftime("%Y-%m-%dT%H:%M:%S"))==$t[0:19]) |
      $base - (if ($t|length)==20 then 0 else
        (($t[20:22]|tonumber)*3600+($t[22:24]|tonumber)*60) *
        (if $t[19:20]=="+" then 1 else -1 end) end)' "$file"
}

ops_route_history_prune()
{
    local routes root fence file entry stamp size name count total tab
    routes="$OPS_APP/routes"; root="$routes/transactions"
    # Both native guards are verified by routes-resource-recover.sh. Never
    # infer a stale lock from its age/PID; defer while any owner is published.
    for fence in "${BRORAY_ROUTES_API_LOCK:-${BRORAY_GLOBAL_LOCK:-/opt/var/lock/broray/global-operation.lock}}" \
      "${BRORAY_LEGACY_GLOBAL_LOCK:-/tmp/broray-global-operation.lock}" "$routes/locks/operation.lock"; do
        [ ! -e "$fence" ] && [ ! -L "$fence" ] || return 75
    done
    [ -e "$root" ] || { [ ! -L "$root" ]; return $?; }
    for entry in "$routes" "$root" "$routes/operations"; do
        [ -d "$entry" ] && [ ! -L "$entry" ] && [ "$(readlink -f "$entry")" = "$entry" ] || return 75
    done
    for file in "$routes/operations"/*.json; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        ops_route_history_plain_file "$file" && [ "$(wc -c <"$file")" -le 32768 ] &&
          jq -e 'type=="object" and .running==false and .resumable!=true' "$file" >/dev/null || return 75
    done
    # Time sorts already-proven terminal records; it never establishes safety.
    for entry in "$root"/*; do
        [ -e "$entry" ] || [ -L "$entry" ] || continue
        stamp="$(ops_route_history_entry "$entry" 2>/dev/null)" || continue
        size="$(du -sk "$entry" | awk '{print $1}')" || return 1
        case "$size" in ''|*[!0-9]*) return 1 ;; esac
        printf '%s\t%s\t%s\n' "$stamp" "$size" "${entry##*/}"
    done | sort -nr | {
        count=0; total=0; tab="$(printf '\t')"
        while IFS="$tab" read -r stamp size name; do
            entry="$root/$name"
            if [ "$count" -lt 5 ] && [ $((total + size)) -le 4096 ]; then
                count=$((count + 1)); total=$((total + size)); continue
            fi
            # Revalidate before unlink. Partial cleanup remains restartable:
            # no retained record is needed to authorize a future route write.
            ops_route_history_entry "$entry" >/dev/null 2>&1 || continue
            rm -rf "$entry" || return 1
        done
    }
}

ops_route_resource_absent()
{
    local resource
    resource="$OPS_APP/routes/locks/operation.lock"
    [ ! -e "$resource" ] && [ ! -L "$resource" ]
}

ops_route_finish_ready()
{
    local bundle file
    ops_route_resource_absent || return 1
    jq -e '.scope=="routes"' "$OPS_CURRENT/state.json" >/dev/null || return 0
    bundle="$(jq -r .bundleId "$OPS_CURRENT/state.json")"
    case "$bundle" in *[!a-z0-9_-]*) return 1 ;; esac
    [ "${#bundle}" -le 63 ] || return 1
    file="$OPS_APP/routes/operations/$bundle.json"
    [ -e "$file" ] || { [ ! -L "$file" ]; return $?; }
    ops_file_safe "$file" && jq -e '.running==false' "$file" >/dev/null
}

ops_route_resource_recover()
{
    local parent
    parent="$OPS_APP/routes/locks"
    if [ ! -e "$parent" ] && [ ! -L "$parent" ]; then return 0; fi
    ops_dir_safe "$parent" && [ "$(readlink -f "$parent")" = "$parent" ] || return 1
    "$OPS_GUARD" "$parent/resource.control.guard" "${BRORAY_OPS_ASH:-/opt/bin/ash}" \
      "${OPS_CODE:-$OPS_APP}/lib/routes-resource-recover.sh" "$parent/operation.lock" "$OPS_ID" "$1"
}

ops_route_progress_prepare()
{
    local bundle file plan before after current counter record
    bundle="$1"; file="$OPS_APP/routes/operations/$bundle.json"
    plan="$OPS_CURRENT/route-recovery-progress.json"
    if [ -e "$plan" ] || [ -L "$plan" ]; then
        ops_file_safe "$plan" 65536 && ops_file_safe "$file" || return 1
        jq -e --arg id "$OPS_ID" --arg bundle "$bundle" --slurpfile current "$file" '
          .schemaVersion==1 and .operationId==$id and .bundleId==$bundle and
          .before.backgroundOperationId==$id and .after.backgroundOperationId==$id and
          .after.running==false and .after.phase=="interrupted" and
          ($current[0]==.before or $current[0]==.after)' "$plan" >/dev/null || return 1
        return 0
    fi
    [ -e "$file" ] || { [ ! -L "$file" ]; return $?; }
    ops_file_safe "$file" || return 1
    jq -e '.running==true' "$file" >/dev/null || return 0
    jq -e --arg id "$OPS_ID" --arg bundle "$bundle" '
      .schemaVersion==2 and .kind=="routes" and .bundleId==$bundle and .backgroundOperationId==$id and
      (.operation=="install" or .operation=="update" or .operation=="restore" or .operation=="delete") and
      (.current|type)=="number" and .current>=0 and .current==(.current|floor) and
      (.total|type)=="number" and .total>=.current and .total<=2147483647 and .total==(.total|floor) and
      .resumable==false' "$file" >/dev/null || return 1
    before="$(cat "$file")"; current="$(jq -r .current "$file")"
    counter="$OPS_APP/routes/operations/$bundle.counter"
    if [ -e "$counter" ] || [ -L "$counter" ]; then
        ops_file_safe "$counter" 4096 || return 1
        current="$(cut -f1 "$counter")"
        case "$current" in ''|*[!0-9]*) return 1 ;; esac
        [ "${#current}" -le 10 ] || return 1
    fi
    after="$(jq -ce --argjson current "$current" '
      select($current>=.current and $current<=.total) |
      .current=$current | .percent=(if .total>0 then (.current*100/.total|floor) else 0 end) |
      .phase="interrupted" | .running=false | .success=false | .rolledBack=false |
      .resumable=false | .canStop=false | .stopRequested=false | .stoppedByUser=false | .currentRoute=null |
      .message="Операция неожиданно завершилась. Выполните проверку набора перед продолжением." |
      .completedAt=(.completedAt // .updatedAt)' "$file")" || return 1
    record="$(jq -nc --arg id "$OPS_ID" --arg bundle "$bundle" --argjson before "$before" --argjson after "$after" \
      '{schemaVersion:1,operationId:$id,bundleId:$bundle,before:$before,after:$after}')" || return 1
    ops_write "$plan" "$record"
}

ops_route_recover()
{
    local marker bundle owner plan file
    [ "${verb:-}" = recover ] && [ "$OPS_EXECUTOR" = "$OPS_CURRENT/owner.json" ] || return 1
    marker="$OPS_CURRENT/route-supervision.json"
    ops_file_safe "$marker" 4096 || return 1
    jq -e --arg id "$OPS_ID" '.schemaVersion==1 and .kind=="protected-route-supervision" and .operationId==$id and
      (.supervisorId|type)=="string" and (.supervisorId|length)==32 and
      (.supervisorId|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102)))' "$marker" >/dev/null || return 1
    owner="$(jq -c .owner "$marker")"; broray_ops_classify_owner "$owner"
    [ "$OPS_OWNER_STATUS" = STALE ] || return 1
    jq -e '.scope=="routes" and .cancelability=="protected" and .acknowledged==true' "$OPS_CURRENT/state.json" >/dev/null || return 1
    bundle="$(jq -r .bundleId "$OPS_CURRENT/state.json")"
    case "$bundle" in *[!a-z0-9_-]*) return 1 ;; esac
    [ "${#bundle}" -le 63 ] || return 1
    # Other transactions and foreign running progress still block recovery.
    ops_route_resource_recover check || return 1
    ops_pending_domain resume "$bundle" "$OPS_ID" && return 1
    ops_route_progress_prepare "$bundle" || return 1
    ops_route_resource_recover retire || return 1
    plan="$OPS_CURRENT/route-recovery-progress.json"
    if [ -e "$plan" ]; then
        file="$OPS_APP/routes/operations/$bundle.json"
        ops_write "$file" "$(jq -c .after "$plan")" || return 1
    fi
    ops_route_resource_absent
}
