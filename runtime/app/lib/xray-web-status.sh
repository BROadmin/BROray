#!/opt/bin/ash
# Status reads never reclaim a PID file or modify installation state.
broray_xray_web_status()
{
    local root result pointer id snapshot job running complete log_tail
    root="${BRORAY_ROOT:-/opt/broray}"; pointer="$root/run/xray-web-operation.json"
    result=null; id=""; job=null; running=false; complete=true; log_tail=""
    if [ -f "$pointer" ] && [ ! -L "$pointer" ] && jq -e 'type=="object"' "$pointer" >/dev/null 2>&1; then
        result="$(cat "$pointer")"
        id="$(printf '%s\n' "$result" | jq -r -L "$root/lib" 'include "operation-public"; .backgroundOperationId|operation_id // empty')"
    fi
    if [ -n "$id" ]; then
        . "$root/lib/operation-client.sh"
        snapshot="$(broray_ops_call status)" || complete=false
        printf '%s\n' "$snapshot" | jq -e '.complete==true' >/dev/null 2>&1 || complete=false
        job="$(printf '%s\n' "$snapshot" | jq -c --arg id "$id" 'first(.operations[]? | select(.operationId==$id)) // null')" || job=null
        if [ "$job" != null ]; then
            running="$(printf '%s\n' "$job" | jq -c .running)"
            [ "$running" != null ] || complete=false
        elif printf '%s\n' "$result" | jq -e '.running==false and (.success|type)=="boolean"' >/dev/null; then
            running=false
        else
            running=null; complete=false
        fi
        log_tail="$(tail -c 4096 "$root/run/xray-operations/$id/worker.log" 2>/dev/null || true)"
    elif [ -e "$root/run/xray-web-operation.pid" ] || [ -L "$root/run/xray-web-operation.pid" ]; then
        running=null; complete=false
    fi
    jq -n --argjson result "$result" --argjson job "$job" --argjson running "$running" --argjson complete "$complete" \
      --arg logTail "$log_tail" '
      (if $result.operation=="install" or $result.operation=="update" or $result.operation=="reinstall"
        then $result.operation else "reinstall" end) as $mode |
      {operation:$mode,operationId:($result.backgroundOperationId // null),operationRunning:$running,
        complete:$complete,pid:null,backgroundOperation:$job,
        result:(if $job!=null and $job.running==false and $result.running==true then
          $result + {running:false,operationRunning:false,success:false,
            error:(if $job.state=="aborted" then "Операция Xray отменена." else "Операция Xray прервана. Откройте диагностику." end)}
          else $result end),logTail:(if $logTail=="" then null else $logTail end)}'
}
