#!/opt/bin/ash
# Client of the single coordinator; shared by old Operation Manager consumers.

broray_ops_queue_submit() { broray_ops_call queue-submit "$@"; }
broray_ops_queue_lookup() { broray_ops_call queue-lookup "$@"; }
broray_ops_queue_next() { broray_ops_call queue-next; }
broray_ops_queue_cancel() { broray_ops_call queue-cancel "$@"; }

broray_ops_request_id_valid()
{
    case "${1:-}" in q-*) ;; *) return 1 ;; esac
    [ "${#1}" = 34 ] || return 1
    case "${1#q-}" in *[!0-9a-f]*) return 1 ;; esac
}

broray_ops_queue_claim()
{
    local request pid rest response attempt rc id token
    request="${1:-}"
    broray_ops_request_id_valid "$request" || return 64
    [ -z "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 73
    BRORAY_BACKGROUND_LAUNCH_NONCE="${2:-$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)}"
    IFS=' ' read -r pid rest </proc/self/stat || return 73
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call queue-claim "$request" "$BRORAY_BACKGROUND_LAUNCH_NONCE" "$pid")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | broray_ops_response_valid token &&
           printf '%s\n' "$response" | jq -es --arg id "$request" 'length==1 and .[0].requestId==$id' >/dev/null 2>&1; then break; fi
        [ "$rc" != 0 ] || rc=1
        if printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1; then rc=2; break; fi
    done
    [ "$rc" = 0 ] || { BRORAY_OPS_LAST_ERROR="$response"; return "$rc"; }
    id="$(printf '%s\n' "$response" | jq -er .operationId)" || return 1
    token="$(printf '%s\n' "$response" | jq -er .token)" || return 1
    BRORAY_BACKGROUND_OPERATION_ID="$id"; BRORAY_BACKGROUND_OPERATION_TOKEN="$token"
    export BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call ack "$id" "$token" "$pid")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | jq -es 'length==1 and .[0].ok==true and .[0].acknowledged==true' >/dev/null 2>&1; then return 0; fi
        if printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1; then break; fi
    done
    # No acknowledged identity means no handler may start.
    return 1
}

broray_ops_call()
{
    local app code state guard ash controller rc response attempt attempt_limit
    app="${BRORAY_ROOT:-${BRORAY_BASE:-/opt/broray}}"
    state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
    code="${BRORAY_OPS_CODE_ROOT:-$app}"
    case "$code" in /*) ;; *) return 1 ;; esac
    [ -d "$code" ] && [ ! -L "$code" ] || return 1
    guard="${BRORAY_OPS_GUARD:-$code/bin/broray-ops-guard}"
    ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    controller="$code/lib/operation-coordinator.sh"
    [ -f "$guard" ] && [ ! -L "$guard" ] && [ -f "$controller" ] || return 1
    [ ! -L "$state" ] || return 1
    case "${1:-}" in status|events|report|classify|platform-preflight-boot-context)
        [ -d "$state" ] && [ -f "$state/operations.guard" ] && [ ! -L "$state/operations.guard" ] || return 1 ;;
    *)
        mkdir -p "$state" || return 1
        chmod 700 "$state" 2>/dev/null || true ;;
    esac
    # Concurrent status readers can hold the guard beyond six seconds on ARM.
    # An empty rc75 proves no command executed, so bounded admission may wait.
    attempt_limit=15
    attempt=0
    while :; do
        # A scheduling daemon may stop while waiting for the coordinator.
        # Check only before a new admission attempt, never after an executed
        # command/lost response. Workers clear service identity on spawn and
        # must still complete their own cancellation/settlement protocol.
        case "${1:-}" in
          queue-submit|queue-next|queue-recover|queue-lookup)
            if [ -n "${BRORAY_SERVICE_GENERATION:-}" ] &&
              command -v broray_service_stop_requested >/dev/null 2>&1 &&
              broray_service_stop_requested; then
                printf '%s\n' '{"ok":false,"errorCode":"SERVICE_STOP_REQUESTED"}'
                return 2
            fi ;;
        esac
        attempt=$((attempt+1)); rc=0
        if [ "${BRORAY_OPS_TEST:-0}" = 1 ] && [ "$app" != /opt/broray ]; then
            response="$("$guard" "$state/operations.guard" "$ash" ash "$controller" "$@")" || rc=$?
        else
            response="$("$guard" "$state/operations.guard" "$ash" "$controller" "$@")" || rc=$?
        fi
        if [ "${1:-}" = history-prune ] && [ "$rc" = 75 ] && [ -z "$response" ]; then
            # Retention is optional maintenance. No command ran under the busy
            # guard; defer to the next pass so its monitor can stop cooperatively.
            # Never retry an executed command or treat an unknown reply as PASS.
            printf '%s\n' '{"ok":false,"errorCode":"HISTORY_MAINTENANCE_DEFERRED"}'
            return 77
        fi
        if [ "$rc" = 75 ] && [ -z "$response" ] &&
           [ -n "${BRORAY_SERVICE_GENERATION:-}" ] &&
           command -v broray_service_stop_requested >/dev/null 2>&1; then
            # No command executed. Scheduling maintenance must yield to a
            # finite worker's publication/drain instead of competing through
            # repeated guard waits. Workers have no service generation.
            case "${1:-}" in queue-submit|queue-next|queue-recover|queue-lookup)
                if broray_service_stop_requested; then
                    printf '%s\n' '{"ok":false,"errorCode":"SERVICE_STOP_REQUESTED"}'
                    return 2
                fi ;;
            esac
            case "${1:-}:${2:-}:${4:-}" in
              queue-submit:servers:active-health:AUTO_SWITCH) ;;
              queue-submit:*|queue-next:*|queue-recover:*|queue-lookup:*)
                printf '%s\n' '{"ok":false,"errorCode":"QUEUE_ADMISSION_DEFERRED"}'
                return 77 ;;
            esac
        fi
        # Guard exit 75 with no response means its two-second lock wait ended
        # before exec: no coordinator work has run. Bound total wait to 30 seconds.
        # Structured publication errors and all other failures remain
        # final, including errors returned after a durable mutation.
        [ "$rc" = 75 ] && [ -z "$response" ] && [ "$attempt" -lt "$attempt_limit" ] || break
    done
    [ -z "$response" ] || printf '%s\n' "$response"
    if [ "$rc" != 0 ] && [ -z "$response" ]; then
        # Expected structured refusals (paused/busy) are handled by the caller.
        # Only absence of a response needs this transport diagnostic.
        code=UNCONFIRMED_RESPONSE
        [ "$rc" != 75 ] || code=GUARD_WAIT_EXHAUSTED
        printf 'BRORAY_COORDINATOR_ERROR command=%s rc=%s code=%s attempts=%s\n' "${1:-unknown}" "$rc" "$code" "$attempt" >&2
    fi
    return "$rc"
}

broray_ops_begin()
{
    local response rc attempt id token
    # Keep this private nonce until finish. Retrying a lost response must recover
    # the same launch, including across a date boundary, without another claim.
    if [ -z "${BRORAY_BACKGROUND_LAUNCH_NONCE:-}" ]; then
        BRORAY_BACKGROUND_LAUNCH_NONCE="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || return 1
    fi
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call begin "$1" "$2" "${3:-}" "${4:-USER}" "$$" "${5:-protected}" "$BRORAY_BACKGROUND_LAUNCH_NONCE")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | broray_ops_response_valid token; then break; fi
        # A structured rejection is final. Retry only unavailable responses.
        [ "$rc" != 0 ] || rc=1
        if printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1; then rc=2; break; fi
    done
    [ "$rc" = 0 ] || { BRORAY_OPS_LAST_ERROR="$response"; return "$rc"; }
    id="$(printf '%s\n' "$response" | jq -er '.operationId')" || return 1
    token="$(printf '%s\n' "$response" | jq -er '.token')" || return 1
    BRORAY_BACKGROUND_OPERATION_ID="$id"
    BRORAY_BACKGROUND_OPERATION_TOKEN="$token"
    export BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1))
        broray_ops_call ack "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" >/dev/null && return 0
    done
    # The caller must exit on failure; it has no permission to execute its job.
    return 1
}

broray_ops_response_valid()
{
    # A zero transport exit code with an empty/truncated body is still a lost
    # response. Never install half an identity or open the worker gate for it.
    jq -es --arg mode "$1" 'length==1 and (.[0] | type=="object" and .ok==true and
      (if $mode=="transfer" then .transferred==true else
        (.operationId|type)=="string" and (.operationId|startswith("op-")) and
        (.operationId|length)<=96 and (.operationId|all(explode[]; (.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or .==45 or .==46 or .==95)) and
        (.token|type)=="string" and (.token|length)==32 and
        (.token|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102))) end))' >/dev/null 2>&1
}

broray_ops_finish()
{
    [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 0
    # Job callers bind identity and mutation under one guard acquisition.
    # Existing protected/legacy callers retain their original call contract.
    if [ "$#" -ge 3 ]; then
        broray_ops_call finish "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "${1:-completed}" "${2:-}" "$3" >/dev/null || return $?
    else
        broray_ops_call finish "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "${1:-completed}" "${2:-}" >/dev/null || return $?
    fi
    unset BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN BRORAY_BACKGROUND_LAUNCH_NONCE
}

broray_ops_tick()
{
    [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 0
    if [ "$#" -ge 2 ]; then
        broray_ops_call tick "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "${1:-working}" "$2" >/dev/null
    else
        broray_ops_call tick "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "${1:-working}" >/dev/null
    fi
}

broray_ops_operation_directory()
{
    local state ram id directory
    state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
    ram="${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}"
    id="${BRORAY_BACKGROUND_OPERATION_ID:-}"
    case "$id" in ''|*[!A-Za-z0-9._-]*|.*|-*) return 1 ;; esac
    directory="$state/operations/$id"
    if [ -e "$ram/steps/$id" ] || [ -L "$ram/steps/$id" ]; then
        [ ! -e "$directory" ] && [ ! -L "$directory" ] || return 1
        directory="$ram/steps/$id"
    fi
    [ -d "$directory" ] && [ ! -L "$directory" ] &&
      [ -f "$directory/state.json" ] && [ ! -L "$directory/state.json" ] || return 1
    jq -e --arg id "$id" '.schemaVersion==2 and .operationId==$id' "$directory/state.json" >/dev/null || return 1
    printf '%s\n' "$directory"
}

broray_ops_cancel_requested()
{
    local directory
    directory="$(broray_ops_operation_directory)" || return 1
    [ -f "$directory/cancel.json" ] && [ ! -L "$directory/cancel.json" ]
}

broray_ops_run_helper()
{
    # Only bounded cooperative work belongs here. Starting the persistent Xray
    # service is a protected owner action, never a traced helper command.
    local app code ash supervisor state timeout rc attempt directory
    [ "$#" -ge 3 ] && [ "$2" = -- ] || return 64
    timeout="$1"; shift 2
    [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] && [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] || return 73
    [ "${BRORAY_OPS_SUPERVISED:-}" != ptrace/1 ] || return 73
    app="${BRORAY_ROOT:-/opt/broray}"
    ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
    code="${BRORAY_OPS_CODE_ROOT:-$app}"
    supervisor="${BRORAY_OPS_SUPERVISOR:-$code/bin/broray-ops-supervisor}"
    [ -f "$supervisor" ] && [ ! -L "$supervisor" ] || return 74
    directory="$(broray_ops_operation_directory)" || return 73
    rc=0
    "$supervisor" "$ash" "$code/lib/operation-supervisor-control.sh" \
      "$directory/cancel.json" "$timeout" 1 2 -- "$@" || rc=$?
    # EXITKILL is asynchronous. The next commit/finish is permitted only after
    # the coordinator confirms every registered helper has disappeared.
    attempt=0
    while [ "$attempt" -lt 5 ]; do
        attempt=$((attempt+1))
        broray_ops_call helpers-drain "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" >/dev/null && return "$rc"
        [ "$attempt" = 5 ] || sleep 1
    done
    return 75
}

broray_ops_handoff_to()
{
    local attempt response rc
    [ "$#" = 2 ] && [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 64
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call handoff "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" "$1" "$2")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | broray_ops_response_valid transfer; then
            unset BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN BRORAY_BACKGROUND_LAUNCH_NONCE
            return 0
        fi
        [ "$rc" != 0 ] || rc=1
        printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1 && return 2
    done
    return "$rc"
}

broray_ops_accept_handoff()
{
    local attempt response rc token
    [ "$#" = 1 ] && [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 64
    attempt=0
    while [ "$attempt" -lt 10 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call accept-handoff "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$$" "$1")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | broray_ops_response_valid token; then
            token="$(printf '%s\n' "$response" | jq -er '.token')" || return 1
            BRORAY_BACKGROUND_OPERATION_TOKEN="$token"; export BRORAY_BACKGROUND_OPERATION_TOKEN
            return 0
        fi
        # Only an unpublished handoff or missing transport response is retried.
        printf '%s\n' "$response" | jq -e '.ok==false and .errorCode!="HANDOFF_NOT_READY"' >/dev/null 2>&1 && return 2
        [ "$attempt" = 10 ] || sleep 1
    done
    return 1
}

# Maintenance admission only: does not stop/install/start a platform or enqueue.
broray_ops_preflight_admit()
{
    local expected app code guard live_guard protocol response rc attempt id token
    [ "$#" = 1 ] || return 64
    expected="$1"
    case "$expected" in ''|*[!0-9a-f]*) return 64 ;; esac
    [ "${#expected}" -eq 64 ] || return 64
    [ -z "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 73
    app="${BRORAY_ROOT:-/opt/broray}"
    code="${BRORAY_OPS_CODE_ROOT:-$app}"
    guard="${BRORAY_OPS_GUARD:-$code/bin/broray-ops-guard}"
    protocol='broray-ops-guard/6 flock-fork-exec atomic-fence durable-state durable-append sync-state'
    [ -f "$guard" ] && [ ! -L "$guard" ] && [ -x "$guard" ] || return 74
    [ "$("$guard" --version 2>/dev/null)" = "$protocol" ] || return 74
    live_guard="$app/bin/broray-ops-guard"
    if [ "$code" != "$app" ] && { [ -e "$live_guard" ] || [ -L "$live_guard" ]; }; then
        [ -f "$live_guard" ] && [ ! -L "$live_guard" ] && [ -x "$live_guard" ] || return 74
        [ "$("$live_guard" --version 2>/dev/null)" = "$protocol" ] || return 74
    fi
    for id in operation-owner.sh operation-client.sh operation-coordinator.sh \
      operation-journal.sh operation-report.sh operation-report-facts.sh \
      operation-publication.sh operation-route-recovery.sh operation-platform-recovery.sh operation-platform-service.sh \
      operation-public.jq operation-report-public.jq; do
        [ -f "$code/lib/$id" ] && [ ! -L "$code/lib/$id" ] || return 74
    done
    if [ -z "${BRORAY_BACKGROUND_LAUNCH_NONCE:-}" ]; then
        BRORAY_BACKGROUND_LAUNCH_NONCE="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || return 74
    fi
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call platform-preflight-begin "$expected" "$$" "$BRORAY_BACKGROUND_LAUNCH_NONCE")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | broray_ops_response_valid token; then break; fi
        [ "$rc" != 0 ] || rc=1
        if printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1; then rc=2; break; fi
    done
    [ "$rc" = 0 ] || { BRORAY_OPS_LAST_ERROR="$response"; return "$rc"; }
    id="$(printf '%s\n' "$response" | jq -er .operationId)" || return 74
    token="$(printf '%s\n' "$response" | jq -er .token)" || return 74
    BRORAY_BACKGROUND_OPERATION_ID="$id"
    BRORAY_BACKGROUND_OPERATION_TOKEN="$token"
    export BRORAY_BACKGROUND_OPERATION_ID BRORAY_BACKGROUND_OPERATION_TOKEN
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call ack "$id" "$token" "$$")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | jq -es \
          'length==1 and .[0].ok==true and .[0].acknowledged==true' >/dev/null 2>&1; then return 0; fi
        printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1 && break
    done
    # Caller MUST NOT perform platform work on a missing/invalid acknowledgement.
    return 74
}

# No command may start until the exact STOP_INTENT response is received.
broray_ops_preflight_stop_intent()
{
    local pid rest rc response attempt
    [ "$#" = 1 ] && [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] && \
      [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] || return 73
    [ "${BRORAY_OPS_SUPERVISED:-}" != ptrace/1 ] || return 73
    IFS=' ' read -r pid rest </proc/self/stat || return 73
    if [ -z "${BRORAY_PREFLIGHT_STOP_NONCE:-}" ]; then
        BRORAY_PREFLIGHT_STOP_NONCE="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)" || return 74
    fi
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call platform-preflight-stop-intent \
          "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$pid" \
          "$1" "$BRORAY_PREFLIGHT_STOP_NONCE")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | jq -es \
          'length==1 and .[0].ok==true and .[0].phase=="STOP_INTENT" and .[0].mutationIntentDurable==true' >/dev/null 2>&1; then
            export BRORAY_PREFLIGHT_STOP_NONCE
            return 0
        fi
        printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1 && return 2
    done
    return 74
}

# Finite helper ONLY. Never run init start/a persistent daemon inside this tree.
broray_ops_run_platform_stop_helper()
{
    local code ash supervisor state timeout pid rest rc attempt
    [ "$#" -ge 3 ] && [ "$2" = -- ] || return 64
    timeout="$1"; shift 2
    [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] && \
      [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] && \
      [ -n "${BRORAY_PREFLIGHT_STOP_NONCE:-}" ] || return 73
    [ "${BRORAY_OPS_SUPERVISED:-}" != ptrace/1 ] || return 73
    code="${BRORAY_OPS_CODE_ROOT:-${BRORAY_ROOT:-/opt/broray}}"
    supervisor="${BRORAY_OPS_SUPERVISOR:-$code/bin/broray-ops-supervisor}"
    ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}"
    [ -f "$supervisor" ] && [ ! -L "$supervisor" ] && [ -x "$supervisor" ] || return 74
    [ "$("$supervisor" --platform-capability 2>/dev/null)" = protected-platform-stop/1 ] || return 74
    IFS=' ' read -r pid rest </proc/self/stat || return 73
    broray_ops_call owner-check "$BRORAY_BACKGROUND_OPERATION_ID" \
      "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$pid" >/dev/null || return 73
    rc=0
    "$supervisor" --protected-platform "$ash" "$code/lib/operation-supervisor-control.sh" \
      "$state/operations/$BRORAY_BACKGROUND_OPERATION_ID/cancel.json" "$timeout" 0 2 -- "$@" || rc=$?
    attempt=0
    while [ "$attempt" -lt 5 ]; do
        attempt=$((attempt+1))
        broray_ops_call helpers-drain "$BRORAY_BACKGROUND_OPERATION_ID" \
          "$BRORAY_BACKGROUND_OPERATION_TOKEN" >/dev/null && return "$rc"
        [ "$attempt" = 5 ] || sleep 1
    done
    return 75
}

# Capture concrete service evidence; this does NOT authorize any signal.
broray_ops_preflight_bind_service()
{
    local pid rest response rc attempt
    [ "$#" = 0 ] && [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] && \
      [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] && [ -n "${BRORAY_PREFLIGHT_STOP_NONCE:-}" ] || return 73
    [ "${BRORAY_OPS_SUPERVISED:-}" != ptrace/1 ] || return 73
    IFS=' ' read -r pid rest </proc/self/stat || return 73
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call platform-preflight-service-bind \
          "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$pid" "$BRORAY_PREFLIGHT_STOP_NONCE")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | jq -es \
          'length==1 and .[0].ok==true and .[0].serviceBound==true and .[0].signalsAuthorized==false and .[0].phase=="STOP_INTENT"' >/dev/null 2>&1; then return 0; fi
        printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1 && return 2
    done
    return 74
}


# Preparation only: retain the protected operation and require a reboot.
# A lost response retries the same immutable intent and nonce; explicit errors
# stop immediately. This is not platform readiness or permission to install.
broray_ops_preflight_stage_legacy()
{
    local pid rest response rc attempt
    [ "$#" = 0 ] && [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] && \
      [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] && [ -n "${BRORAY_PREFLIGHT_STOP_NONCE:-}" ] || return 73
    [ "${BRORAY_OPS_SUPERVISED:-}" != ptrace/1 ] || return 73
    IFS=' ' read -r pid rest </proc/self/stat || return 73
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1)); rc=0
        response="$(broray_ops_call platform-preflight-migration-stage \
          "$BRORAY_BACKGROUND_OPERATION_ID" "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$pid" "$BRORAY_PREFLIGHT_STOP_NONCE")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | jq -es \
          'length==1 and .[0].ok==true and .[0].phase=="REBOOT_REQUIRED" and .[0].platformReady==false and .[0].serviceStopped==false and .[0].activationAllowed==false and .[0].signalsAuthorized==false' >/dev/null; then
            printf '%s\n' "$response"; return 0
        fi
        printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1 && return 2
    done
    return 74
}

# Two-entry legacy boot guard preparation, under the existing protected owner.
# Only a lost reply is retried; explicit failures retain the fence immediately.
broray_ops_preflight_stage_bootguard()
{
    local pid rest response rc attempt
    [ "$#" = 0 ] && [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] &&
      [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] && [ -n "${BRORAY_PREFLIGHT_STOP_NONCE:-}" ] || return 73
    [ "${BRORAY_OPS_SUPERVISED:-}" != ptrace/1 ] || return 73
    IFS=' ' read -r pid rest </proc/self/stat || return 73
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1));rc=0
        response="$(broray_ops_call platform-preflight-bootguard-stage "$BRORAY_BACKGROUND_OPERATION_ID" \
          "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$pid" "$BRORAY_PREFLIGHT_STOP_NONCE")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="BOOT_GUARDS_STAGED" and .[0].platformReady==false and .[0].serviceStopped==false and .[0].activationAllowed==false and .[0].signalsAuthorized==false' >/dev/null; then
            printf '%s\n' "$response";return 0
        fi
        printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1 && { printf '%s\n' "$response";return 2; }
    done
    return 74
}

# Ask the canonical coordinator to stop an existing from-birth generation.
# Polling is pacing only. Completion requires its authenticated terminal proof.
broray_ops_preflight_stop_generation()
{
    local pid rest response rc attempt
    [ "$#" = 2 ] && [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] &&
      [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] && [ -n "${BRORAY_PREFLIGHT_STOP_NONCE:-}" ] || return 73
    [ "${BRORAY_OPS_SUPERVISED:-}" != ptrace/1 ] || return 73
    IFS=' ' read -r pid rest </proc/self/stat || return 73
    attempt=0
    while [ "$attempt" -lt 12 ]; do
        attempt=$((attempt+1));rc=0
        response="$(broray_ops_call platform-preflight-stop-generation "$BRORAY_BACKGROUND_OPERATION_ID" \
          "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$pid" "$BRORAY_PREFLIGHT_STOP_NONCE" "$1" "$2")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | jq -es 'length==1 and .[0].ok==true and .[0].phase=="STOPPED" and .[0].serviceStopped==true and .[0].platformReady==false' >/dev/null 2>&1; then
            printf '%s\n' "$response";return 0
        fi
        printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1 && { printf '%s\n' "$response";return 2; }
        [ "$attempt" = 12 ] || sleep 1
    done
    return 75
}

# Legacy observation-only binding remains unable to authorize STOPPED.
broray_ops_preflight_stop_service()
{
    local pid rest app code ash supervisor response owner target ticks rc attempt phase
    [ "$#" = 0 ] && [ -n "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] && \
      [ -n "${BRORAY_BACKGROUND_OPERATION_TOKEN:-}" ] && [ -n "${BRORAY_PREFLIGHT_STOP_NONCE:-}" ] || return 73
    [ "${BRORAY_OPS_SUPERVISED:-}" != ptrace/1 ] || return 73
    IFS=' ' read -r pid rest </proc/self/stat || return 73
    app="${BRORAY_ROOT:-/opt/broray}";code="${BRORAY_OPS_CODE_ROOT:-$app}"
    ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    supervisor="${BRORAY_OPS_SUPERVISOR:-$code/bin/broray-ops-supervisor}"
    [ -f "$supervisor" ] && [ ! -L "$supervisor" ] && [ -x "$supervisor" ] || return 74
    [ "$("$supervisor" --service-stop-capability 2>/dev/null)" = bound-updater-stop/3 ] || return 74
    response="$(broray_ops_call platform-preflight-stop-target "$BRORAY_BACKGROUND_OPERATION_ID" \
      "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$pid" "$BRORAY_PREFLIGHT_STOP_NONCE")" || return $?
    printf '%s\n' "$response" | jq -es 'length==1 and .[0].ok==true and (. [0].phase=="STOP_INTENT" or .[0].phase=="STOPPED")' >/dev/null || return 74
    owner="$(printf '%s\n' "$response" | jq -c .owner)";phase="$(printf '%s\n' "$response" | jq -r .phase)"
    if [ "$owner" != null ] && [ "$phase" != STOPPED ]; then
        target="$(printf '%s\n' "$owner" | jq -er .pid)" || return 74
        ticks="$(printf '%s\n' "$owner" | jq -er .startTicks)" || return 74
        rc=0
        "$supervisor" --stop-updater "$ash" "$code/lib/operation-supervisor-control.sh" "$target" "$ticks" 15 || rc=$?
        [ "$rc" = 0 ] || return "$rc"
    fi
    attempt=0
    while [ "$attempt" -lt 3 ]; do
        attempt=$((attempt+1));rc=0
        response="$(broray_ops_call platform-preflight-service-stopped "$BRORAY_BACKGROUND_OPERATION_ID" \
          "$BRORAY_BACKGROUND_OPERATION_TOKEN" "$pid" "$BRORAY_PREFLIGHT_STOP_NONCE")" || rc=$?
        if [ "$rc" = 0 ] && printf '%s\n' "$response" | jq -es \
          'length==1 and .[0].ok==true and .[0].phase=="STOPPED" and .[0].serviceStopped==true and .[0].platformReady==false' >/dev/null; then
            printf '%s\n' "$response";return 0
        fi
        printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null 2>&1 && return 2
    done
    return 74
}

# Read-only metadata helper for finite queue workers. Keep the implementation
# in the existing owner module, which is already in authenticated recovery code.
broray_ops_stat()
{
    if command -v stat >/dev/null 2>&1; then
        stat "$@"
        return $?
    fi
(
    . "${BRORAY_OPS_CODE_ROOT:-${BRORAY_ROOT:-/opt/broray}}/lib/operation-owner.sh" || exit 74
    broray_ops_file_stat "$@"
)
}
