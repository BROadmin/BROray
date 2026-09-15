#!/opt/bin/ash
# Sourced only by the guarded coordinator. Every record uses one public schema.
OPS_JOURNAL="$OPS_STATE/operation-events"
OPS_JOURNAL_LIMIT=262144

ops_journal_head_read()
{
    local file legacy
    file="$OPS_JOURNAL/head.json"
    if [ -e "$file" ] || [ -L "$file" ]; then
        ops_file_safe "$file" 4096 && jq -e '
          def integer: type=="number" and .>=0 and .<=9007199254740991 and floor==.;
          .schemaVersion==1 and (.allocatedSequence|integer) and (.lastSequence|integer) and
          (.firstSequence|integer) and .firstSequence>0 and .lastSequence<=.allocatedSequence and
          (.pending|type)=="boolean" and (.gap|type)=="boolean" and
          (.lastHash|type)=="string" and (.lastHash|length==0 or length==64)' "$file" >/dev/null || return 1
        OPS_JOURNAL_HEAD="$(cat "$file")"
    else
        legacy=false
        for file in "$OPS_JOURNAL/events.jsonl" "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.2.jsonl"; do
            [ ! -s "$file" ] || legacy=true
        done
        OPS_JOURNAL_HEAD="$(jq -nc --argjson gap "$legacy" \
          '{schemaVersion:1,allocatedSequence:0,lastSequence:0,firstSequence:1,lastHash:"",pending:false,gap:$gap}')"
    fi
}

ops_journal_test_point()
{
    if [ "${BRORAY_OPS_TEST:-0}" = 1 ] && [ "$OPS_APP" != /opt/broray ] &&
       [ "${BRORAY_OPS_TEST_JOURNAL_CRASH:-}" = "$1" ]; then kill -KILL "$$"; fi
    return 0
}

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
    local event record state owner size file bytes key sequence pending first hash temporary
    event="$1"; state='{}'; owner='{}'
    if [ -n "${OPS_CURRENT:-}" ]; then
        ops_file_safe "$OPS_CURRENT/state.json" && state="$(cat "$OPS_CURRENT/state.json")"
        ops_file_safe "${OPS_EXECUTOR:-$OPS_CURRENT/owner.json}" 8192 && owner="$(cat "${OPS_EXECUTOR:-$OPS_CURRENT/owner.json}")"
    fi
    key=''
    if [ -n "${3:-}" ]; then
        key="$(printf '%s:%s:%s' "${OPS_ID:-}" "$event" "$3" | sha256sum | cut -c 1-32)" || return 1
    fi
    ops_journal_safe && ops_journal_head_read || return 1
    sequence="$(printf '%s\n' "$OPS_JOURNAL_HEAD" | jq -er '.allocatedSequence+1 | select(.<=9007199254740991)')" || return 1
    record="$(jq -nc -L "$OPS_APP/lib" --argjson sequence "$sequence" --argjson state "$state" --argjson owner "$owner" --arg key "$key" \
      --arg now "$(ops_now)" --arg event "$event" --arg code "${2:-}" \
      'include "operation-public"; {sequence:$sequence,eventId:$key,timestamp:$now,operationId:$state.operationId,operationType:$state.type,
       source:($state.source // "SYSTEM_RECOVERY"),event:$event,pid:$owner.owner.pid,
       result:(if $event=="completed" or $event=="recovered" then "success" elif $event=="failed" then "failure" elif $event=="aborted" then "cancelled" else "pending" end),
       errorCode:($code|if .=="" then $state.errorCode else . end)} | event_public')" || return 1
    bytes="$(printf '%s\n' "$record" | wc -c)"
    [ "$bytes" -le 2048 ] || return 1
    if [ -n "$key" ]; then
        for file in "$OPS_JOURNAL/events.jsonl" "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.2.jsonl"; do
            [ ! -f "$file" ] || ! grep -Fq "\"eventId\":\"$key\"" "$file" || return 0
        done
    fi
    # Reserve before any append/rotation. A process or power failure leaves a
    # durable pending bit; the allocated sequence is never silently reused.
    pending="$(printf '%s\n' "$OPS_JOURNAL_HEAD" | jq -c --argjson sequence "$sequence" \
      '.gap=(.gap or .pending) | .pending=true | .allocatedSequence=$sequence')" || return 1
    ops_write "$OPS_JOURNAL/head.json" "$pending" || return 1
    ops_journal_test_point reserved
    first="$(printf '%s\n' "$pending" | jq -r '.firstSequence')"
    file="$OPS_JOURNAL/events.jsonl"; size=0
    [ ! -e "$file" ] || size="$(wc -c <"$file")"
    if [ "$((size+bytes))" -gt "$OPS_JOURNAL_LIMIT" ]; then
        [ ! -e "$OPS_JOURNAL/events.1.jsonl" ] || mv -f "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.2.jsonl" || return 1
        [ ! -e "$file" ] || mv -f "$file" "$OPS_JOURNAL/events.1.jsonl" || return 1
        # Rotation deliberately drops the oldest segment. Only then may the
        # retained lower bound move; a missing segment outside rotation is loss.
        for file in "$OPS_JOURNAL/events.2.jsonl" "$OPS_JOURNAL/events.1.jsonl"; do
            [ -s "$file" ] || continue
            first="$(head -n 1 "$file" | jq -er '.sequence | select(type=="number" and .>0 and floor==.)')" || {
                first=1; pending="$(printf '%s\n' "$pending" | jq -c '.gap=true')"
            }
            break
        done
        ops_journal_test_point rotated
    fi
    file="$OPS_JOURNAL/events.jsonl"
    temporary="$OPS_JOURNAL/record.pending"
    # The inherited flock excludes every previous journal writer, including
    # an orphan native appender. This fixed scratch file cannot be live here.
    if [ -e "$temporary" ] || [ -L "$temporary" ]; then
        ops_file_safe "$temporary" 2048 && [ "$(find "$temporary" -maxdepth 0 -type f -links 1 -print)" = "$temporary" ] || return 1
        rm "$temporary" || return 1
    fi
    (set -C; printf '%s\n' "$record" >"$temporary") && chmod 600 "$temporary" || return 1
    "$OPS_GUARD" --append-file "$temporary" "$file" || return 1
    ops_journal_test_point appended
    hash="$(sha256sum "$temporary" | awk '{print $1}')"
    pending="$(printf '%s\n' "$pending" | jq -c --arg hash "$hash" --argjson first "$first" \
      '.pending=false | .lastSequence=.allocatedSequence | .firstSequence=$first | .lastHash=$hash')" || return 1
    ops_write "$OPS_JOURNAL/head.json" "$pending" || return 1
    rm "$temporary"
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
    local file gap last_hash
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
    ops_journal_head_read || return 1
    last_hash="$(
      for file in "$OPS_JOURNAL/events.2.jsonl" "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.jsonl"; do
          [ ! -f "$file" ] || cat "$file"
      done | tail -n 1 | sha256sum | awk '{print $1}'
    )"
    for file in "$OPS_JOURNAL/events.2.jsonl" "$OPS_JOURNAL/events.1.jsonl" "$OPS_JOURNAL/events.jsonl"; do
        [ ! -f "$file" ] || cat "$file"
    done | jq -Rsc -L "$OPS_APP/lib" --argjson gap "$gap" --argjson head "$OPS_JOURNAL_HEAD" --arg lastHash "$last_hash" '
      include "operation-public";
      split("\n") | map(select(length>0)) as $lines |
      ($lines | map(try fromjson catch null)) as $rows |
      ($rows | map(if type=="object" then .sequence else null end)) as $seq |
      ($seq | all(.[]; type=="number" and .>0 and .<=9007199254740991 and floor==.) and
        all(range(1;length); $seq[.]==($seq[.-1]+1))) as $ordered |
      ($gap or $head.gap or $head.pending or any($rows[]; type!="object") or ($ordered|not) or
        $head.allocatedSequence!=$head.lastSequence or
        (if ($rows|length)==0 then $head.lastSequence!=0 else
          $seq[0]!=$head.firstSequence or $seq[-1]!=$head.lastSequence or $lastHash!=$head.lastHash end)) as $incomplete |
      {ok:true,complete:($incomplete|not),truncated:($rows|length>500),nextCursor:null,
       firstSequence:$head.firstSequence,lastSequence:$head.lastSequence,
       errors:(if $incomplete then ["JOURNAL_GAP"] else [] end),
       events:($rows[-500:] | map(select(type=="object") | event_public))}'
}

ops_journal_read() { ops_journal_snapshot | jq -c '.events'; }
