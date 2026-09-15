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

ops_event_append()
{
    local event record state owner size file bytes key
    event="$1"; state='{}'; owner='{}'
    if [ -n "${OPS_CURRENT:-}" ]; then
        ops_file_safe "$OPS_CURRENT/state.json" && state="$(cat "$OPS_CURRENT/state.json")"
        ops_file_safe "${OPS_EXECUTOR:-$OPS_CURRENT/owner.json}" 8192 && owner="$(cat "${OPS_EXECUTOR:-$OPS_CURRENT/owner.json}")"
    fi
    key=''
    if [ -n "${3:-}" ]; then
        key="$(printf '%s:%s:%s' "${OPS_ID:-}" "$event" "$3" | sha256sum | cut -c 1-32)" || return 1
    fi
    record="$(jq -nc -L "$OPS_APP/lib" --argjson state "$state" --argjson owner "$owner" --arg key "$key" \
      --arg now "$(ops_now)" --arg event "$event" --arg code "${2:-}" \
      'include "operation-public"; {eventId:$key,timestamp:$now,operationId:$state.operationId,operationType:$state.type,
       source:($state.source // "SYSTEM_RECOVERY"),event:$event,pid:$owner.owner.pid,
       result:(if $event=="completed" or $event=="recovered" then "success" elif $event=="failed" then "failure" elif $event=="aborted" then "cancelled" else "pending" end),
       errorCode:($code|if .=="" then $state.errorCode else . end)} | event_public')" || return 1
    bytes="$(printf '%s\n' "$record" | wc -c)"
    [ "$bytes" -le 2048 ] || return 1
    ops_journal_safe || return 1
    if [ -n "$key" ]; then
        for file in "$OPS_JOURNAL/events.jsonl" "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.2.jsonl"; do
            [ ! -f "$file" ] || ! grep -Fq "\"eventId\":\"$key\"" "$file" || return 0
        done
    fi
    file="$OPS_JOURNAL/events.jsonl"; size=0
    [ ! -e "$file" ] || size="$(wc -c <"$file")"
    if [ "$((size+bytes))" -gt "$OPS_JOURNAL_LIMIT" ]; then
        [ ! -e "$OPS_JOURNAL/events.1.jsonl" ] || mv -f "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.2.jsonl" || return 1
        [ ! -e "$file" ] || mv -f "$file" "$OPS_JOURNAL/events.1.jsonl" || return 1
    fi
    printf '%s\n' "$record" >>"$file"
}

ops_event()
{
    if ops_event_append "$@"; then return 0; fi
    # Sticky evidence: failure of a diagnostic write cannot prevent retirement.
    # RAM is the fallback when the persistent filesystem cannot accept writes.
    if [ ! -L "$OPS_RAM" ] && mkdir -p "$OPS_RAM"; then
        [ -L "$OPS_RAM/journal-gap" ] || printf '%s\n' gap >"$OPS_RAM/journal-gap" 2>/dev/null || true
    fi
    if [ -d "$OPS_JOURNAL" ] && [ ! -L "$OPS_JOURNAL" ] && [ ! -L "$OPS_JOURNAL/gap" ]; then
        printf '%s\n' gap >"$OPS_JOURNAL/gap" 2>/dev/null || true
    fi
    return 1
}

ops_journal_snapshot()
{
    local file gap
    gap=false
    [ ! -e "$OPS_JOURNAL" ] && [ ! -L "$OPS_JOURNAL" ] || ops_dir_safe "$OPS_JOURNAL" || return 1
    if [ -e "$OPS_JOURNAL/gap" ] || [ -L "$OPS_JOURNAL/gap" ] ||
       [ -e "$OPS_RAM/journal-gap" ] || [ -L "$OPS_RAM/journal-gap" ]; then gap=true; fi
    # The output is projected again; old/partial/untrusted lines are never raw.
    for file in "$OPS_JOURNAL/events.2.jsonl" "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.jsonl"; do
        [ ! -L "$file" ] || return 1
        [ -e "$file" ] || continue
        ops_file_safe "$file" "$OPS_JOURNAL_LIMIT" || return 1
    done
    for file in "$OPS_JOURNAL/events.2.jsonl" "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.jsonl"; do
        [ ! -f "$file" ] || cat "$file"
    done | jq -Rsc -L "$OPS_APP/lib" --argjson gap "$gap" '
      include "operation-public";
      split("\n") | map(select(length>0)) as $lines |
      ($lines | map(try fromjson catch null)) as $rows |
      ($gap or any($rows[]; type!="object")) as $incomplete |
      {ok:true,complete:($incomplete|not),truncated:($rows|length>500),nextCursor:null,
       errors:(if $incomplete then ["JOURNAL_GAP"] else [] end),
       events:($rows[-500:] | map(select(type=="object") | event_public))}'
}

ops_journal_read() { ops_journal_snapshot | jq -c '.events'; }
