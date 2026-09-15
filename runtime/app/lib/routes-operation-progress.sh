#!/opt/bin/ash

# Persistent progress state for long-running route operations.
# The file is written atomically and can be polled by WebUI while ndmc applies
# thousands of routes one by one.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_PROGRESS_DIR="${BRORAY_ROUTES_PROGRESS_DIR:-${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}/operations}"
BRORAY_ROUTES_PROGRESS_FILE=""
BRORAY_ROUTES_PROGRESS_COUNTER_FILE=""
BRORAY_ROUTES_PROGRESS_BUNDLE=""
BRORAY_ROUTES_PROGRESS_OPERATION=""
BRORAY_ROUTES_PROGRESS_PHASE="idle"
BRORAY_ROUTES_PROGRESS_CURRENT=0
BRORAY_ROUTES_PROGRESS_TOTAL=0
BRORAY_ROUTES_PROGRESS_MESSAGE=""
BRORAY_ROUTES_PROGRESS_ROUTE=""
BRORAY_ROUTES_PROGRESS_STARTED_AT=""
BRORAY_ROUTES_PROGRESS_COMPLETED_AT=""
BRORAY_ROUTES_PROGRESS_RUNNING=false
BRORAY_ROUTES_PROGRESS_SUCCESS=null
BRORAY_ROUTES_PROGRESS_ROLLED_BACK=false
BRORAY_ROUTES_PROGRESS_ACTIVE=false
BRORAY_ROUTES_PROGRESS_STOP_FILE=""
BRORAY_ROUTES_PROGRESS_RESUMABLE=false
BRORAY_ROUTES_PROGRESS_STOP_REQUESTED=false
BRORAY_ROUTES_PROGRESS_STOPPED_BY_USER=false
BRORAY_ROUTES_PROGRESS_ERROR_ROUTE=""
BRORAY_ROUTES_PROGRESS_RESUMED=false

broray_routes_progress_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_progress_bundle_valid()
{
    case "${1:-}" in
        ''|*[!a-z0-9_-]*|????????????????????????????????????????????????????????????????*)
            return 1
            ;;
    esac

    return 0
}

broray_routes_progress_uint()
{
    case "${1:-}" in
        ''|*[!0-9]*) return 1 ;;
    esac
    return 0
}

broray_routes_progress_percent()
{
    local current total percent

    current="${1:-0}"
    total="${2:-0}"
    broray_routes_progress_uint "$current" || current=0
    broray_routes_progress_uint "$total" || total=0

    if [ "$total" -le 0 ]; then
        if [ "$BRORAY_ROUTES_PROGRESS_RUNNING" = false ] &&
           [ "$BRORAY_ROUTES_PROGRESS_SUCCESS" = true ]
        then
            printf '%s\n' 100
        else
            printf '%s\n' 0
        fi
        return 0
    fi

    [ "$current" -le "$total" ] || current="$total"
    percent=$((current * 100 / total))
    [ "$percent" -le 100 ] || percent=100
    printf '%s\n' "$percent"
}


broray_routes_progress_counter_write()
{
    local temp route

    [ "$BRORAY_ROUTES_PROGRESS_ACTIVE" = true ] || return 0
    [ -n "$BRORAY_ROUTES_PROGRESS_COUNTER_FILE" ] || return 1
    route="$(printf '%s' "$BRORAY_ROUTES_PROGRESS_ROUTE" | tr '\t\r\n' '   ')"
    temp="$BRORAY_ROUTES_PROGRESS_COUNTER_FILE.new.$$"
    printf '%s\t%s\n' \
        "$BRORAY_ROUTES_PROGRESS_CURRENT" "$route" >"$temp" || {
        rm -f "$temp"
        return 1
    }
    chmod 644 "$temp" 2>/dev/null || true
    mv -f "$temp" "$BRORAY_ROUTES_PROGRESS_COUNTER_FILE"
}

broray_routes_progress_tick()
{
    local current current_route

    [ "$BRORAY_ROUTES_PROGRESS_ACTIVE" = true ] || return 0
    current="${1:-$BRORAY_ROUTES_PROGRESS_CURRENT}"
    current_route="${2:-}"
    broray_routes_progress_uint "$current" || return 1
    [ "$current" -le "$BRORAY_ROUTES_PROGRESS_TOTAL" ] ||
        current="$BRORAY_ROUTES_PROGRESS_TOTAL"

    BRORAY_ROUTES_PROGRESS_CURRENT="$current"
    BRORAY_ROUTES_PROGRESS_ROUTE="$current_route"
    broray_routes_progress_counter_write
}

broray_routes_progress_write()
{
    local updated_at percent temp

    [ "$BRORAY_ROUTES_PROGRESS_ACTIVE" = true ] || return 0
    [ -n "$BRORAY_ROUTES_PROGRESS_FILE" ] || return 1

    mkdir -p "$BRORAY_ROUTES_PROGRESS_DIR" || return 1
    updated_at="$(broray_routes_progress_now)"
    percent="$(broray_routes_progress_percent \
        "$BRORAY_ROUTES_PROGRESS_CURRENT" \
        "$BRORAY_ROUTES_PROGRESS_TOTAL")"
    temp="$BRORAY_ROUTES_PROGRESS_FILE.new.$$"

    jq -n \
        --arg bundleId "$BRORAY_ROUTES_PROGRESS_BUNDLE" \
        --arg operation "$BRORAY_ROUTES_PROGRESS_OPERATION" \
        --arg phase "$BRORAY_ROUTES_PROGRESS_PHASE" \
        --arg message "$BRORAY_ROUTES_PROGRESS_MESSAGE" \
        --arg currentRoute "$BRORAY_ROUTES_PROGRESS_ROUTE" \
        --arg startedAt "$BRORAY_ROUTES_PROGRESS_STARTED_AT" \
        --arg updatedAt "$updated_at" \
        --arg completedAt "$BRORAY_ROUTES_PROGRESS_COMPLETED_AT" \
        --arg errorRoute "$BRORAY_ROUTES_PROGRESS_ERROR_ROUTE" \
        --argjson current "$BRORAY_ROUTES_PROGRESS_CURRENT" \
        --argjson total "$BRORAY_ROUTES_PROGRESS_TOTAL" \
        --argjson percent "$percent" \
        --argjson running "$BRORAY_ROUTES_PROGRESS_RUNNING" \
        --argjson success "$BRORAY_ROUTES_PROGRESS_SUCCESS" \
        --argjson rolledBack "$BRORAY_ROUTES_PROGRESS_ROLLED_BACK" \
        --argjson resumable "$BRORAY_ROUTES_PROGRESS_RESUMABLE" \
        --argjson stopRequested "$BRORAY_ROUTES_PROGRESS_STOP_REQUESTED" \
        --argjson stoppedByUser "$BRORAY_ROUTES_PROGRESS_STOPPED_BY_USER" \
        --argjson resumed "$BRORAY_ROUTES_PROGRESS_RESUMED" \
        --argjson pid "$$" '
        {
            schemaVersion: 2,
            kind: "routes",
            bundleId: $bundleId,
            operation: $operation,
            phase: $phase,
            current: $current,
            total: $total,
            percent: $percent,
            currentRoute: (
                if $currentRoute == "" then null else $currentRoute end
            ),
            message: $message,
            running: $running,
            success: $success,
            rolledBack: $rolledBack,
            resumable: $resumable,
            stopRequested: $stopRequested,
            stoppedByUser: $stoppedByUser,
            resumed: $resumed,
            errorRoute: (
                if $errorRoute == "" then null else $errorRoute end
            ),
            pid: $pid,
            startedAt: $startedAt,
            updatedAt: $updatedAt,
            completedAt: (
                if $completedAt == "" then null else $completedAt end
            )
        }
    ' >"$temp" || {
        rm -f "$temp"
        return 1
    }

    chmod 644 "$temp" 2>/dev/null || true
    mv -f "$temp" "$BRORAY_ROUTES_PROGRESS_FILE" || return 1
    broray_routes_progress_counter_write
}

broray_routes_progress_begin()
{
    local bundle operation total message initial_current resumed

    bundle="${1:-}"
    operation="${2:-}"
    total="${3:-0}"
    message="${4:-Подготовка операции с маршрутами.}"
    initial_current="${5:-0}"
    resumed="${6:-false}"

    broray_routes_progress_bundle_valid "$bundle" || return 1
    case "$operation" in
        install|update|restore|delete) ;;
        *) return 1 ;;
    esac
    broray_routes_progress_uint "$total" || return 1
    broray_routes_progress_uint "$initial_current" || return 1
    [ "$initial_current" -le "$total" ] || return 1
    case "$resumed" in true|false) ;; *) resumed=false ;; esac

    BRORAY_ROUTES_PROGRESS_BUNDLE="$bundle"
    BRORAY_ROUTES_PROGRESS_OPERATION="$operation"
    BRORAY_ROUTES_PROGRESS_PHASE="preparing"
    BRORAY_ROUTES_PROGRESS_CURRENT="$initial_current"
    BRORAY_ROUTES_PROGRESS_TOTAL="$total"
    BRORAY_ROUTES_PROGRESS_MESSAGE="$message"
    BRORAY_ROUTES_PROGRESS_ROUTE=""
    BRORAY_ROUTES_PROGRESS_STARTED_AT="$(broray_routes_progress_now)"
    BRORAY_ROUTES_PROGRESS_COMPLETED_AT=""
    BRORAY_ROUTES_PROGRESS_RUNNING=true
    BRORAY_ROUTES_PROGRESS_SUCCESS=null
    BRORAY_ROUTES_PROGRESS_ROLLED_BACK=false
    BRORAY_ROUTES_PROGRESS_RESUMABLE=false
    BRORAY_ROUTES_PROGRESS_STOP_REQUESTED=false
    BRORAY_ROUTES_PROGRESS_STOPPED_BY_USER=false
    BRORAY_ROUTES_PROGRESS_ERROR_ROUTE=""
    BRORAY_ROUTES_PROGRESS_RESUMED="$resumed"
    BRORAY_ROUTES_PROGRESS_FILE="$BRORAY_ROUTES_PROGRESS_DIR/$bundle.json"
    BRORAY_ROUTES_PROGRESS_COUNTER_FILE="$BRORAY_ROUTES_PROGRESS_DIR/$bundle.counter"
    BRORAY_ROUTES_PROGRESS_STOP_FILE="$BRORAY_ROUTES_PROGRESS_DIR/$bundle.stop"
    BRORAY_ROUTES_PROGRESS_ACTIVE=true

    rm -f "$BRORAY_ROUTES_PROGRESS_STOP_FILE" 2>/dev/null || true
    broray_routes_progress_write
}

broray_routes_progress_update()
{
    local phase current total message current_route

    [ "$BRORAY_ROUTES_PROGRESS_ACTIVE" = true ] || return 0
    phase="${1:-applying}"
    current="${2:-$BRORAY_ROUTES_PROGRESS_CURRENT}"
    total="${3:-$BRORAY_ROUTES_PROGRESS_TOTAL}"
    message="${4:-$BRORAY_ROUTES_PROGRESS_MESSAGE}"
    current_route="${5:-}"

    broray_routes_progress_uint "$current" || return 1
    broray_routes_progress_uint "$total" || return 1
    [ "$current" -le "$total" ] || current="$total"

    BRORAY_ROUTES_PROGRESS_PHASE="$phase"
    BRORAY_ROUTES_PROGRESS_CURRENT="$current"
    BRORAY_ROUTES_PROGRESS_TOTAL="$total"
    BRORAY_ROUTES_PROGRESS_MESSAGE="$message"
    BRORAY_ROUTES_PROGRESS_ROUTE="$current_route"
    BRORAY_ROUTES_PROGRESS_RUNNING=true
    BRORAY_ROUTES_PROGRESS_SUCCESS=null
    BRORAY_ROUTES_PROGRESS_COMPLETED_AT=""

    broray_routes_progress_write
}

broray_routes_progress_complete()
{
    local message

    [ "$BRORAY_ROUTES_PROGRESS_ACTIVE" = true ] || return 0
    message="${1:-Операция с маршрутами завершена.}"

    BRORAY_ROUTES_PROGRESS_PHASE="completed"
    BRORAY_ROUTES_PROGRESS_CURRENT="$BRORAY_ROUTES_PROGRESS_TOTAL"
    BRORAY_ROUTES_PROGRESS_MESSAGE="$message"
    BRORAY_ROUTES_PROGRESS_ROUTE=""
    BRORAY_ROUTES_PROGRESS_RUNNING=false
    BRORAY_ROUTES_PROGRESS_SUCCESS=true
    BRORAY_ROUTES_PROGRESS_ROLLED_BACK=false
    BRORAY_ROUTES_PROGRESS_RESUMABLE=false
    BRORAY_ROUTES_PROGRESS_STOP_REQUESTED=false
    BRORAY_ROUTES_PROGRESS_STOPPED_BY_USER=false
    BRORAY_ROUTES_PROGRESS_ERROR_ROUTE=""
    BRORAY_ROUTES_PROGRESS_COMPLETED_AT="$(broray_routes_progress_now)"

    rm -f "$BRORAY_ROUTES_PROGRESS_STOP_FILE" 2>/dev/null || true
    broray_routes_progress_write
}

broray_routes_progress_fail()
{
    local message rolled_back

    [ "$BRORAY_ROUTES_PROGRESS_ACTIVE" = true ] || return 0
    message="${1:-Операция с маршрутами завершилась ошибкой.}"
    rolled_back="${2:-false}"
    case "$rolled_back" in true|false) ;; *) rolled_back=false ;; esac

    if [ "$rolled_back" = true ]; then
        BRORAY_ROUTES_PROGRESS_PHASE="rolled_back"
    else
        BRORAY_ROUTES_PROGRESS_PHASE="failed"
    fi
    BRORAY_ROUTES_PROGRESS_MESSAGE="$message"
    BRORAY_ROUTES_PROGRESS_ROUTE=""
    BRORAY_ROUTES_PROGRESS_RUNNING=false
    BRORAY_ROUTES_PROGRESS_SUCCESS=false
    BRORAY_ROUTES_PROGRESS_ROLLED_BACK="$rolled_back"
    BRORAY_ROUTES_PROGRESS_RESUMABLE=false
    BRORAY_ROUTES_PROGRESS_STOP_REQUESTED=false
    BRORAY_ROUTES_PROGRESS_STOPPED_BY_USER=false
    BRORAY_ROUTES_PROGRESS_ERROR_ROUTE=""
    BRORAY_ROUTES_PROGRESS_COMPLETED_AT="$(broray_routes_progress_now)"

    rm -f "$BRORAY_ROUTES_PROGRESS_STOP_FILE" 2>/dev/null || true
    broray_routes_progress_write
}

broray_routes_progress_pause()
{
    local message stopped_by_user error_route

    [ "$BRORAY_ROUTES_PROGRESS_ACTIVE" = true ] || return 0
    message="${1:-Операция приостановлена.}"
    stopped_by_user="${2:-true}"
    error_route="${3:-}"
    case "$stopped_by_user" in true|false) ;; *) stopped_by_user=true ;; esac

    if [ "$stopped_by_user" = true ]; then
        BRORAY_ROUTES_PROGRESS_PHASE="paused"
        BRORAY_ROUTES_PROGRESS_SUCCESS=null
    else
        BRORAY_ROUTES_PROGRESS_PHASE="failed_resumable"
        BRORAY_ROUTES_PROGRESS_SUCCESS=false
    fi
    BRORAY_ROUTES_PROGRESS_MESSAGE="$message"
    BRORAY_ROUTES_PROGRESS_ROUTE=""
    BRORAY_ROUTES_PROGRESS_RUNNING=false
    BRORAY_ROUTES_PROGRESS_ROLLED_BACK=false
    BRORAY_ROUTES_PROGRESS_RESUMABLE=true
    BRORAY_ROUTES_PROGRESS_STOP_REQUESTED=false
    BRORAY_ROUTES_PROGRESS_STOPPED_BY_USER="$stopped_by_user"
    BRORAY_ROUTES_PROGRESS_ERROR_ROUTE="$error_route"
    BRORAY_ROUTES_PROGRESS_COMPLETED_AT="$(broray_routes_progress_now)"

    rm -f "$BRORAY_ROUTES_PROGRESS_STOP_FILE" 2>/dev/null || true
    broray_routes_progress_write
}

broray_routes_progress_stop_requested()
{
    local bundle stop_file

    bundle="${1:-$BRORAY_ROUTES_PROGRESS_BUNDLE}"
    broray_routes_progress_bundle_valid "$bundle" || return 1
    stop_file="$BRORAY_ROUTES_PROGRESS_DIR/$bundle.stop"
    [ -f "$stop_file" ]
}

broray_routes_progress_request_stop()
{
    local bundle file stop_file temp now

    bundle="${1:-}"
    broray_routes_progress_bundle_valid "$bundle" || return 1
    file="$BRORAY_ROUTES_PROGRESS_DIR/$bundle.json"
    stop_file="$BRORAY_ROUTES_PROGRESS_DIR/$bundle.stop"
    [ -r "$file" ] || return 2
    jq -e '.running == true and (.operation != null)' "$file" >/dev/null 2>&1 || return 3

    mkdir -p "$BRORAY_ROUTES_PROGRESS_DIR" || return 1
    now="$(broray_routes_progress_now)"
    temp="$stop_file.new.$$"
    jq -n --arg bundleId "$bundle" --arg requestedAt "$now" --argjson requestedByPid "$$" '
        {
            schemaVersion: 1,
            bundleId: $bundleId,
            requestedAt: $requestedAt,
            requestedByPid: $requestedByPid
        }
    ' >"$temp" || { rm -f "$temp"; return 1; }
    chmod 644 "$temp" 2>/dev/null || true
    mv -f "$temp" "$stop_file" || return 1

    temp="$file.new.$$"
    jq --arg now "$now" '
        .stopRequested = true |
        .phase = "stopping" |
        .message = "Остановка запрошена. Текущий маршрут будет завершён." |
        .updatedAt = $now
    ' "$file" >"$temp" || { rm -f "$temp"; return 1; }
    chmod 644 "$temp" 2>/dev/null || true
    mv -f "$temp" "$file" || return 1
    return 0
}

broray_routes_progress_resume_values()
{
    local bundle operation remaining file current total resumable saved_operation

    bundle="${1:-}"
    operation="${2:-}"
    remaining="${3:-0}"
    broray_routes_progress_bundle_valid "$bundle" || return 1
    broray_routes_progress_uint "$remaining" || return 1
    file="$BRORAY_ROUTES_PROGRESS_DIR/$bundle.json"

    if [ -r "$file" ]; then
        resumable="$(jq -r '.resumable // false' "$file" 2>/dev/null || true)"
        saved_operation="$(jq -r '.operation // empty' "$file" 2>/dev/null || true)"
        current="$(jq -r '.current // 0' "$file" 2>/dev/null || true)"
        total="$(jq -r '.total // 0' "$file" 2>/dev/null || true)"
        if [ "$resumable" = true ] && [ "$saved_operation" = "$operation" ] &&
           broray_routes_progress_uint "$current" &&
           broray_routes_progress_uint "$total" &&
           [ "$current" -le "$total" ] &&
           [ "$((total - current))" -eq "$remaining" ]
        then
            printf '%s\t%s\t%s\n' "$current" "$total" true
            return 0
        fi
    fi

    printf '0\t%s\t%s\n' "$remaining" false
}

broray_routes_progress_idle_json()
{
    local bundle
    bundle="${1:-}"

    jq -n --arg bundleId "$bundle" '
        {
            schemaVersion: 2,
            kind: "routes",
            bundleId: $bundleId,
            operation: null,
            phase: "idle",
            current: 0,
            total: 0,
            percent: 0,
            currentRoute: null,
            message: "Операция не выполняется.",
            running: false,
            success: null,
            rolledBack: false,
            resumable: false,
            stopRequested: false,
            stoppedByUser: false,
            resumed: false,
            errorRoute: null,
            pid: null,
            startedAt: null,
            updatedAt: null,
            completedAt: null
        }
    '
}

broray_routes_progress_read()
{
    local bundle file counter lock_pid pid current current_route total percent interrupted

    bundle="${1:-}"
    broray_routes_progress_bundle_valid "$bundle" || return 1
    file="$BRORAY_ROUTES_PROGRESS_DIR/$bundle.json"
    counter="$BRORAY_ROUTES_PROGRESS_DIR/$bundle.counter"

    if [ ! -r "$file" ]; then
        broray_routes_progress_idle_json "$bundle"
        return 0
    fi

    jq -e --arg bundleId "$bundle" '
        ((.schemaVersion == 1) or (.schemaVersion == 2)) and
        (.kind == "routes") and
        (.bundleId == $bundleId) and
        ((.operation == null) or (.operation == "install") or
         (.operation == "update") or (.operation == "restore") or
         (.operation == "delete")) and
        ((.phase | type) == "string") and
        ((.current | type) == "number") and (.current >= 0) and
        ((.total | type) == "number") and (.total >= 0) and
        ((.percent | type) == "number") and (.percent >= 0) and (.percent <= 100) and
        ((.running | type) == "boolean") and
        ((.success == null) or ((.success | type) == "boolean")) and
        ((.rolledBack | type) == "boolean") and
        (((.resumable // false) | type) == "boolean") and
        (((.stopRequested // false) | type) == "boolean") and
        (((.stoppedByUser // false) | type) == "boolean") and
        (((.resumed // false) | type) == "boolean") and
        ((.errorRoute == null) or ((.errorRoute | type) == "string"))
    ' "$file" >/dev/null 2>&1 || return 1

    current="$(jq -r '.current' "$file")"
    total="$(jq -r '.total' "$file")"
    current_route=""

    if [ -r "$counter" ]; then
        current="$(cut -f1 "$counter" 2>/dev/null | sed -n '1p')"
        current_route="$(cut -f2- "$counter" 2>/dev/null | sed -n '1p')"
    fi
    broray_routes_progress_uint "$current" || current=0
    broray_routes_progress_uint "$total" || total=0
    [ "$current" -le "$total" ] || current="$total"
    if [ "$total" -gt 0 ]; then
        percent=$((current * 100 / total))
    else
        percent="$(jq -r 'if .success == true then 100 else 0 end' "$file")"
    fi

    pid="$(jq -r 'if .running == true then (.pid // empty) else empty end' "$file" 2>/dev/null)"
    case "$pid" in ''|*[!0-9]*) pid="" ;; esac
    interrupted=false
    lock_pid="$(sed -n '1p' "${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}/locks/operation.lock/pid" 2>/dev/null || true)"
    case "$lock_pid" in ''|*[!0-9]*) lock_pid="" ;; esac
    if [ -n "$pid" ]; then
        if ! kill -0 "$pid" 2>/dev/null || [ "$lock_pid" != "$pid" ]; then
            interrupted=true
        fi
    fi

    jq \
        --argjson current "$current" \
        --argjson percent "$percent" \
        --arg currentRoute "$current_route" \
        --argjson interrupted "$interrupted" '
        .current = $current |
        .percent = $percent |
        .currentRoute = (
            if $currentRoute == "" then null else $currentRoute end
        ) |
        if $interrupted then
            .phase = "interrupted" |
            .running = false |
            .success = false |
            .rolledBack = false |
            .resumable = false |
            .stopRequested = false |
            .stoppedByUser = false |
            .currentRoute = null |
            .message = "Операция неожиданно завершилась. Выполните проверку набора перед продолжением." |
            .completedAt = (.completedAt // .updatedAt)
        else
            .
        end
    ' "$file"
}
