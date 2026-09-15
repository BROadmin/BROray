#!/opt/bin/ash
# Sourced only by the guarded coordinator. Every record uses one public schema.
OPS_JOURNAL="$OPS_STATE/operation-events"
OPS_JOURNAL_LIMIT=262144

ops_journal_safe()
{
    local file
    [ ! -L "$OPS_JOURNAL" ] || return 1
    mkdir -p "$OPS_JOURNAL" || return 1
    for file in "$OPS_JOURNAL/events.jsonl" "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.2.jsonl"; do
        [ ! -L "$file" ] && { [ ! -e "$file" ] || [ -f "$file" ]; } || return 1
    done
}

ops_event()
{
    local event record state owner size file bytes
    event="$1"; state='{}'; owner='{}'
    if [ -n "${OPS_CURRENT:-}" ]; then
        ops_file_safe "$OPS_CURRENT/state.json" && state="$(cat "$OPS_CURRENT/state.json")"
        ops_file_safe "$OPS_CURRENT/owner.json" 4096 && owner="$(cat "$OPS_CURRENT/owner.json")"
    fi
    record="$(jq -nc -L "$OPS_APP/lib" --argjson state "$state" --argjson owner "$owner" \
      --arg now "$(ops_now)" --arg event "$event" --arg code "${2:-}" \
      'include "operation-public"; {timestamp:$now,operationId:$state.operationId,operationType:$state.type,
       source:($state.source // "SYSTEM_RECOVERY"),event:$event,pid:$owner.owner.pid,
       result:(if $event=="completed" or $event=="recovered" then "success" elif $event=="failed" then "failure" else "pending" end),
       errorCode:($code|if .=="" then $state.errorCode else . end)} | event_public')" || return 1
    bytes="$(printf '%s\n' "$record" | wc -c)"
    [ "$bytes" -le 2048 ] || return 1
    ops_journal_safe || return 1
    file="$OPS_JOURNAL/events.jsonl"; size=0
    [ ! -e "$file" ] || size="$(wc -c <"$file")"
    if [ "$((size+bytes))" -gt "$OPS_JOURNAL_LIMIT" ]; then
        [ ! -e "$OPS_JOURNAL/events.1.jsonl" ] || mv -f "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.2.jsonl" || return 1
        [ ! -e "$file" ] || mv -f "$file" "$OPS_JOURNAL/events.1.jsonl" || return 1
    fi
    printf '%s\n' "$record" >>"$file"
}

ops_journal_read()
{
    local file
    # The output is projected again; old/partial/untrusted lines are never raw.
    for file in "$OPS_JOURNAL/events.2.jsonl" "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.jsonl"; do
        [ ! -L "$file" ] || return 1
        [ -e "$file" ] || continue
        ops_file_safe "$file" "$OPS_JOURNAL_LIMIT" || return 1
    done
    for file in "$OPS_JOURNAL/events.2.jsonl" "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.jsonl"; do
        [ ! -f "$file" ] || cat "$file"
    done | tail -n 500 | jq -Rsc -L "$OPS_APP/lib" 'include "operation-public"; split("\n") | map(select(length>0) | fromjson? | select(type=="object") | event_public)'
}
