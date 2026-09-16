#!/opt/bin/ash
# Existing route repair remains a page action. This retires only dead job state.
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
      "$OPS_APP/lib/routes-resource-recover.sh" "$parent/operation.lock" "$OPS_ID" "$1"
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
