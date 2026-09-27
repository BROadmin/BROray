#!/opt/bin/ash
# Inert on load. Reuse the existing scheduler and coordinator, never a new daemon.
DOT_AUTO_ROOT="${BRORAY_DOT_ROOT:-${BRORAY_ROOT:-/opt/broray}/routes/dot}"
DOT_AUTO_CONFIG="${BRORAY_DOT_CONFIG:-$DOT_AUTO_ROOT/config.json}"
DOT_AUTO_STATE="${BRORAY_DOT_STATE:-$DOT_AUTO_ROOT/state.json}"
DOT_AUTO_SETTINGS="$DOT_AUTO_ROOT/auto-check.json"
DOT_AUTO_INTERVAL=300
broray_dot_auto_safe_file() {
 [ -f "$1" ] && [ ! -L "$1" ] && [ "$(wc -c < "$1")" -le 65536 ]
}
broray_dot_auto_paths_safe() {
 [ ! -L "${BRORAY_ROOT:-/opt/broray}/routes" ] && [ ! -L "$DOT_AUTO_ROOT" ] &&
 [ ! -L "$DOT_AUTO_CONFIG" ] && [ ! -L "$DOT_AUTO_STATE" ] && [ ! -L "$DOT_AUTO_SETTINGS" ]
}
broray_dot_auto_enabled() {
 broray_dot_auto_paths_safe || return 1
 if [ ! -e "$DOT_AUTO_SETTINGS" ]; then printf 'false\n'; return 0; fi
 broray_dot_auto_safe_file "$DOT_AUTO_SETTINGS" || return 1
 jq -er 'select(.schemaVersion==1 and (.enabled|type)=="boolean") | .enabled|tostring' "$DOT_AUTO_SETTINGS"
}
broray_dot_auto_ids() {
 broray_dot_auto_paths_safe && broray_dot_auto_safe_file "$DOT_AUTO_CONFIG" || return 1
 jq -ce 'select(.schemaVersion==3 and (.requestedIds|type)=="array" and .requestedIds==.selectedIds) |
 .requestedIds | select(length<=8 and length==(unique|length) and all(.[];type=="string")) | sort' "$DOT_AUTO_CONFIG"
}
broray_dot_auto_due() {
 local ids now
 [ "$(broray_dot_auto_enabled 2>/dev/null)" = true ] || return 1
 ids="$(broray_dot_auto_ids)" || return 1
 [ "$ids" != '[]' ] || return 1
 [ ! -e "$DOT_AUTO_ROOT/transaction-recovery-required.json" ] && [ ! -L "$DOT_AUTO_ROOT/transaction-recovery-required.json" ] || return 1
 broray_dot_auto_safe_file "$DOT_AUTO_STATE" || return 1
 now="$(date '+%s')"
 jq -e --argjson ids "$ids" --argjson now "$now" --argjson interval "$DOT_AUTO_INTERVAL" '
 select(.schemaVersion==1 and (.tests|type)=="array") |
 def recent($t): ($t|type)=="number" and $t>0 and ($now-$t)>=0 and ($now-$t)<$interval;
 ((.autoCheck.selectedIds==$ids and recent(.autoCheck.lastAttemptEpoch))|not) and
 ([.tests[]? | select(.id as $id | $ids|index($id)!=null) | select(recent(.testedEpoch))] |
 map(.id)|unique|length) < ($ids|length)' "$DOT_AUTO_STATE" >/dev/null 2>&1
}
broray_dot_auto_view() {
 local enabled valid ids paused pausefile auto observed now
 valid=true; enabled="$(broray_dot_auto_enabled 2>/dev/null)" || { valid=false; enabled=false; }
 ids="$(broray_dot_auto_ids 2>/dev/null)" || ids='[]'
 paused=false; pausefile="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/background-automation.json"
 if [ -e "$pausefile" ] || [ -L "$pausefile" ]; then
  paused="$(jq -er 'select((.paused|type)=="boolean") | .paused|tostring' "$pausefile" 2>/dev/null)" || paused=null
  [ ! -L "$pausefile" ] || paused=null
 fi
 auto='{}'; now="$(date '+%s')"
 if broray_dot_auto_safe_file "$DOT_AUTO_STATE"; then
  auto="$(jq -c '.autoCheck | if type=="object" then {lastAttemptEpoch,finishedEpoch,status,errorCode,selectedIds} else {} end' "$DOT_AUTO_STATE" 2>/dev/null)" || auto='{}'
 fi
 jq -nc --argjson enabled "$enabled" --argjson valid "$valid" --argjson paused "$paused" \
 --argjson ids "$ids" --argjson now "$now" --argjson last "$auto" \
 '{schemaVersion:1,enabled:$enabled,settingsValid:$valid,paused:$paused,intervalSeconds:300,
 savedIds:$ids,last:$last,observedEpoch:$now}'
}
broray_dot_auto_save() {
 local temporary
 broray_dot_auto_paths_safe && broray_dot_auto_safe_file "$1" || return 74
 jq -e 'type=="object" and keys==["enabled"] and (.enabled|type)=="boolean"' "$1" >/dev/null || return 64
 broray_job_checkpoint committing || return $?
 mkdir -p "$DOT_AUTO_ROOT" || return 74
 temporary="$(mktemp "$DOT_AUTO_ROOT/.auto-check-XXXXXX")" || return 74
 if ! jq --arg at "$(date '+%Y-%m-%dT%H:%M:%S%z')" '{schemaVersion:1,enabled:.enabled,updatedAt:$at}' "$1" >"$temporary" || ! chmod 600 "$temporary"; then rm -f "$temporary"; return 74; fi
 broray_job_checkpoint committing || { rm -f "$temporary"; return 130; }
 mv -f "$temporary" "$DOT_AUTO_SETTINGS" || { rm -f "$temporary"; return 74; }
 broray_dot_auto_view
}
broray_dot_auto_write_state() {
 local temporary rc
 broray_dot_auto_paths_safe && broray_dot_auto_safe_file "$DOT_AUTO_STATE" || return 74
 jq -e '.schemaVersion==1 and (.tests|type)=="array"' "$1" >/dev/null || return 74
 temporary="$(mktemp "$DOT_AUTO_ROOT/.auto-state-XXXXXX")" || return 74
 cp "$1" "$temporary" && chmod 600 "$temporary" || { rm -f "$temporary"; return 74; }
 rc=0; broray_job_checkpoint committing || rc=$?
 [ "$rc" = 0 ] || { rm -f "$temporary"; return "$rc"; }
 mv -f "$temporary" "$DOT_AUTO_STATE" || { rm -f "$temporary"; return 74; }
 # No status cache deletion: its existing bounded lifetime is sufficient.
}
broray_dot_auto_cleanup() {
 [ "${DOT_AUTO_DRAINED:-false}" = true ] || return 0
 case "${DOT_AUTO_WORK:-}" in "${BRORAY_ROOT:-/opt/broray}/tmp/dot-auto-"*) ;; *) return 0 ;; esac
 [ -d "$DOT_AUTO_WORK" ] && [ ! -L "$DOT_AUTO_WORK" ] || return 0
 [ "$(cat "$DOT_AUTO_WORK/operation-id" 2>/dev/null)" = "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 1
 rm -rf "$DOT_AUTO_WORK"
 unset DOT_AUTO_WORK DOT_AUTO_DRAINED
}
broray_dot_auto_run() {
 local ids started rc finished at entries result current outcome
 broray_job_require_owner || return $?
 broray_dot_auto_due || return 0
 . "$BRORAY_ROOT/lib/routes-dot.sh" || return 74
 broray_dot_transaction_require_clear || return 74
 ids="$(broray_dot_auto_ids)" || return 74
 DOT_AUTO_WORK="$(mktemp -d "$BRORAY_ROOT/tmp/dot-auto-XXXXXX")" || return 74
 chmod 700 "$DOT_AUTO_WORK" || return 74
 printf '%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >"$DOT_AUTO_WORK/operation-id" || return 74
 DOT_AUTO_DRAINED=true
 jq -nc --argjson ids "$ids" '{serverIds:$ids,allowUntested:false}' >"$DOT_AUTO_WORK/request.json" || return 74
 broray_dot_validate_request "$DOT_AUTO_WORK/request.json" || return 74
 broray_dot_entries_for_request "$DOT_AUTO_WORK/request.json" "$DOT_AUTO_WORK/entries.json" || return 74
 started="$(date '+%s')"
 jq --argjson ids "$ids" --argjson now "$started" --arg op "$BRORAY_BACKGROUND_OPERATION_ID" \
 '.autoCheck={status:"running",selectedIds:$ids,lastAttemptEpoch:$now,operationId:$op}' "$DOT_AUTO_STATE" >"$DOT_AUTO_WORK/start.json" || return 74
 broray_dot_auto_write_state "$DOT_AUTO_WORK/start.json" || return $?
 broray_job_checkpoint checking || return $?
 DOT_AUTO_DRAINED=false; rc=0
 broray_ops_run_helper 150 -- "${BRORAY_OPS_ASH:-/opt/bin/ash}" "$BRORAY_ROOT/lib/dot-auto-probe.sh" "$DOT_AUTO_WORK" || rc=$?
 [ "$rc" != 75 ] || { BRORAY_JOB_UNRESOLVED=true; return 75; }
 DOT_AUTO_DRAINED=true
 [ "$rc" != 130 ] || return 130
 broray_job_checkpoint checking || return $?
 current="$(broray_dot_auto_ids)" || return 74
 [ "$current" = "$ids" ] && [ "$(broray_dot_auto_enabled)" = true ] || return 74
 result="$DOT_AUTO_WORK/results.json"; entries="$DOT_AUTO_WORK/entries.json"
 finished="$(date '+%s')"; at="$(date '+%Y-%m-%dT%H:%M:%S%z')"
 if [ "$rc" = 0 ] && broray_dot_auto_safe_file "$result" &&
 jq -e --argjson start "$started" --argjson end "$finished" --slurpfile entries "$entries" '
 . as $tests | type=="array" and length==($entries[0]|length) and
 (map(.id)|unique|length)==length and all(.[]; (.ok|type)=="boolean" and
 (.status|IN("ok","failed","unavailable")) and (.testedEpoch|type)=="number" and
 .testedEpoch>=$start and .testedEpoch<=$end) and
 all($entries[0][]; . as $e | any($tests[]; .id==$e.id and .address==$e.address and
 .effectivePort==$e.effectivePort and .sni==$e.sni and .spki==$e.spki and
 .interface==$e.interface and .domain==$e.domain))' "$result" >/dev/null 2>&1; then
  jq --slurpfile result "$result" --arg at "$at" --argjson end "$finished" '
   .tests=$result[0] | .lastTestedAt=$at | .lastTestedEpoch=$end | .updatedAt=$at |
   .autoCheck.status=(if all($result[0][]; .ok) then "success" else "failed" end) |
   .autoCheck.finishedEpoch=$end | .autoCheck.errorCode=null' "$DOT_AUTO_STATE" >"$DOT_AUTO_WORK/final.json" || return 74
  outcome=0
 else
  jq --argjson end "$finished" '.autoCheck.status="error" | .autoCheck.finishedEpoch=$end |
    .autoCheck.errorCode="DOT_AUTO_CHECK_FAILED"' "$DOT_AUTO_STATE" >"$DOT_AUTO_WORK/final.json" || return 74
  outcome=1
 fi
 broray_dot_auto_write_state "$DOT_AUTO_WORK/final.json" || return $?
 return "$outcome"
}
broray_dot_auto_exit() {
 local rc="$1"
 broray_dot_auto_cleanup || true
 broray_job_exit "$rc"
}

# Bind the selected endpoints and enable setting, not the changing test cache.
broray_dot_auto_context() {
 local config settings catalog
 broray_dot_auto_paths_safe && broray_dot_auto_ids >/dev/null || return 76
 broray_dot_auto_safe_file "$DOT_AUTO_SETTINGS" || return 76
 [ "$(broray_dot_auto_enabled)" = true ] || return 76
 [ ! -e "$DOT_AUTO_ROOT/transaction-recovery-required.json" ] &&
 [ ! -L "$DOT_AUTO_ROOT/transaction-recovery-required.json" ] || return 76
 config="$(sha256sum "$DOT_AUTO_CONFIG" | cut -d ' ' -f 1)" || return 76
 settings="$(sha256sum "$DOT_AUTO_SETTINGS" | cut -d ' ' -f 1)" || return 76
 catalog="$(sha256sum "${BRORAY_OPS_CODE_ROOT:-${BRORAY_ROOT:-/opt/broray}}/lib/routes-dot.sh" | cut -d ' ' -f 1)" || return 76
 [ -n "$config" ] && [ -n "$settings" ] && [ -n "$catalog" ] || return 76
 printf '%s\n' "$config" "$settings" "$catalog" | sha256sum | cut -d ' ' -f 1
}

broray_dot_auto_step() {
 local dq_request dq_owner dq_context dq_directory dq_result dq_digest dq_state dq_ids dq_cursor
 local dq_total dq_id dq_work dq_rc dq_started dq_finished dq_at dq_tests dq_status dq_payload dq_tmp
 dq_request="$1"
 broray_job_require_owner || return $?
 dq_owner="$(broray_ops_operation_directory)" || return 74
 dq_context="$(jq -er .queueStep.context "$dq_owner/state.json")" || return 74
 [ "$(broray_dot_auto_context)" = "$dq_context" ] || return 76
 dq_ids="$(broray_dot_auto_ids)" || return 76
 [ "$dq_ids" != '[]' ] || return 76
 dq_directory="${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}/requests/$dq_request"
 [ -d "$dq_directory" ] && [ ! -L "$dq_directory" ] &&
 [ "$(stat -c '%u:%a' "$dq_directory")" = "$(id -u):700" ] || return 74
 dq_result="$dq_directory/result.json"
 dq_digest="$(jq -r '.queueStep.resultSha256 // empty' "$dq_owner/state.json")" || return 74
 if [ -n "$dq_digest" ]; then
  [ -f "$dq_result" ] && [ ! -L "$dq_result" ] &&
  [ "$(stat -c '%u:%a:%h' "$dq_result")" = "$(id -u):600:1" ] &&
  [ "$(sha256sum "$dq_result" | cut -d ' ' -f 1)" = "$dq_digest" ] || return 76
  dq_state="$(jq -ce --arg request "$dq_request" --arg context "$dq_context" --argjson ids "$dq_ids" '
   def count: type=="number" and .>=0 and floor==.;
   select(.schemaVersion==1 and .kind=="dot" and .requestId==$request and .context==$context and
    .selectedIds==$ids and .totalCount==($ids|length) and (.checkedCount|count) and
    .checkedCount<.totalCount and (.failedCount|count) and .failedCount<=.checkedCount and
    (.startedEpoch|count))' "$dq_result")" || return 76
 else
  [ ! -e "$dq_result" ] && [ ! -L "$dq_result" ] || return 76
  dq_state="$(jq -nc --arg request "$dq_request" --arg context "$dq_context" --argjson ids "$dq_ids" \
   --argjson now "$(date '+%s')" '{schemaVersion:1,kind:"dot",requestId:$request,context:$context,
   selectedIds:$ids,totalCount:($ids|length),checkedCount:0,failedCount:0,startedEpoch:$now}')" || return 74
 fi
 dq_cursor="$(printf '%s\n' "$dq_state" | jq -r .checkedCount)"
 dq_total="$(printf '%s\n' "$dq_state" | jq -r .totalCount)"
 dq_id="$(printf '%s\n' "$dq_ids" | jq -er --argjson cursor "$dq_cursor" '.[$cursor]')" || return 76
 dq_work="$dq_directory/dot-probe-$BRORAY_BACKGROUND_OPERATION_ID"
 [ ! -e "$dq_work" ] && [ ! -L "$dq_work" ] || return 74
 mkdir -m 700 "$dq_work" || return 74
 printf '%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >"$dq_work/operation-id" || return 74
 jq -nc --arg id "$dq_id" '{serverIds:[$id],allowUntested:false}' >"$dq_work/request.json" || return 74
 . "${BRORAY_OPS_CODE_ROOT:-${BRORAY_ROOT:-/opt/broray}}/lib/routes-dot.sh" || return 74
 broray_dot_entries_for_request "$dq_work/request.json" "$dq_work/entries.json" || return 74
 dq_started="$(date '+%s')"
 broray_job_checkpoint checking || return $?
 dq_rc=0
 BRORAY_DOT_QUEUE_REQUEST="$dq_request" broray_ops_run_helper 150 -- \
  "${BRORAY_OPS_ASH:-/opt/bin/ash}" "${BRORAY_OPS_CODE_ROOT:-$BRORAY_ROOT}/lib/dot-auto-probe.sh" "$dq_work" || dq_rc=$?
 case "$dq_rc" in
  0) ;;
  75) BRORAY_JOB_UNRESOLVED=true; return 75 ;;
  *) return "$dq_rc" ;;
 esac
 dq_finished="$(date '+%s')"; dq_at="$(date '+%Y-%m-%dT%H:%M:%S%z')"
 broray_job_checkpoint checking || return $?
 [ "$(broray_dot_auto_context)" = "$dq_context" ] || return 76
 broray_dot_auto_safe_file "$dq_work/results.json" &&
 jq -e --arg id "$dq_id" --argjson start "$dq_started" --argjson end "$dq_finished" \
 --slurpfile entries "$dq_work/entries.json" '
  type=="array" and length==1 and .[0].id==$id and
  all(.[]; (.ok|type)=="boolean" and (.status|IN("ok","failed","unavailable")) and
   (.ok==(.status=="ok")) and (.testedEpoch|type)=="number" and
   .testedEpoch>=$start and .testedEpoch<=$end and
   .address==$entries[0][0].address and .effectivePort==$entries[0][0].effectivePort and
   .sni==$entries[0][0].sni and .spki==$entries[0][0].spki and
   .interface==$entries[0][0].interface and .domain==$entries[0][0].domain)' \
 "$dq_work/results.json" >/dev/null || return 74
 dq_tests="$(jq -c . "$dq_work/results.json")" || return 74
 dq_state="$(printf '%s\n' "$dq_state" | jq -c --argjson tests "$dq_tests" '
  .checkedCount+=1 | .failedCount+=(if $tests[0].ok then 0 else 1 end)')" || return 74
 dq_cursor=$((dq_cursor+1)); dq_status=running
 if [ "$dq_cursor" -eq "$dq_total" ]; then
  dq_status=success
  [ "$(printf '%s\n' "$dq_state" | jq -r .failedCount)" = 0 ] || dq_status=failed
 fi
 dq_payload="$dq_directory/publication-$BRORAY_BACKGROUND_OPERATION_ID.json"
 [ ! -e "$dq_payload" ] && [ ! -L "$dq_payload" ] || return 74
 # Dot global writers are excluded while this preparation resource is held.
 # Reread current state after the probe; retain every unrelated/newer entry.
 broray_dot_auto_safe_file "$DOT_AUTO_STATE" &&
 jq -e '.schemaVersion==1 and (.tests|type)=="array"' "$DOT_AUTO_STATE" >/dev/null || return 74
 (set -C; jq --argjson result "$dq_tests" --argjson continuation "$dq_state" --arg id "$dq_id" \
  --arg status "$dq_status" --arg at "$dq_at" --argjson end "$dq_finished" \
  --arg operation "$BRORAY_BACKGROUND_OPERATION_ID" '
  .tests as $old | {
   tests:(if any($old[]?; .id==$id and .testedEpoch>$result[0].testedEpoch) then $old
     else [$old[]? | select(.id!=$id)] + $result end),
   lastTestedAt:$at,lastTestedEpoch:$end,updatedAt:$at,
   autoCheck:($continuation|{requestId,context,selectedIds,totalCount,checkedCount,failedCount,
    lastAttemptEpoch:.startedEpoch,status:$status,operationId:$operation,errorCode:null,
    finishedEpoch:(if $status=="running" then null else $end end)})}' \
  "$DOT_AUTO_STATE" >"$dq_payload") || return 74
 chmod 600 "$dq_payload" || return 74
 broray_job_publish_json dot-tests '' "$dq_payload" || return $?
 dq_tmp="$dq_directory/result-$BRORAY_BACKGROUND_OPERATION_ID.tmp"
 [ ! -e "$dq_tmp" ] && [ ! -L "$dq_tmp" ] && [ ! -L "$dq_result" ] || return 74
 (set -C; printf '%s\n' "$dq_state" >"$dq_tmp") || return 74
 chmod 600 "$dq_tmp" && mv "$dq_tmp" "$dq_result" || return 74
 if [ "$dq_cursor" -lt "$dq_total" ]; then
  dq_digest="$(sha256sum "$dq_result" | cut -d ' ' -f 1)" || return 74
  broray_job_yield probe "$dq_digest" || return $?
 fi
}
