#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
BRORAY="/opt/broray/bin/broray"
RUN="/opt/broray/run"
SCRIPT="$RUN/xray-web-reinstall.sh"
LOG="$RUN/xray-web-reinstall.log"
RESULT="$RUN/xray-web-reinstall.json"
PIDFILE="$RUN/xray-web-reinstall.pid"

. "$AUTH"

broray_api_require_method POST
broray_api_require_session

mkdir -p "$RUN"

OLD_PID="$(
    cat "$PIDFILE" 2>/dev/null || true
)"

if [ -n "$OLD_PID" ] &&
   kill -0 "$OLD_PID" 2>/dev/null
then
    broray_api_error \
        "409 Conflict" \
        "XRAY_OPERATION_IN_PROGRESS" \
        "Переустановка Xray уже выполняется."
fi

cat > "$SCRIPT" <<'WORKER'
#!/opt/bin/ash

BRORAY="/opt/broray/bin/broray"
LOG="/opt/broray/run/xray-web-reinstall.log"
RESULT="/opt/broray/run/xray-web-reinstall.json"

exec > "$LOG" 2>&1

success=false
error=""
output_json="{}"

finish()
{
    running=false
    pid=""

    pid="$(
        pidof xray 2>/dev/null |
            awk '{print $1}'
    )"

    if [ -n "$pid" ]; then
        running=true
    else
        /opt/etc/init.d/S24broray start \
            >/dev/null 2>&1 || true

        sleep 3

        pid="$(
            pidof xray 2>/dev/null |
                awk '{print $1}'
        )"

        [ -n "$pid" ] && running=true
    fi

    jq -n \
        --argjson success "$success" \
        --arg error "$error" \
        --argjson output "$output_json" \
        --argjson running "$running" \
        --arg pid "$pid" \
        --arg completedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        {
            success: $success,
            operation: "reinstall",
            result: $output,
            running: $running,
            pid: (
                if $pid == ""
                then null
                else ($pid | tonumber)
                end
            ),
            error: (
                if $error == ""
                then null
                else $error
                end
            ),
            completedAt: $completedAt
        }
    ' > "$RESULT"
}

trap finish EXIT INT TERM

TEMP="/tmp/broray-xray-web-reinstall-$$.json"

if "$BRORAY" xray reinstall > "$TEMP"; then
    output_json="$(
        cat "$TEMP"
    )"

    if [ "$(
        jq -r '.success // false' "$TEMP"
    )" = true ]; then
        success=true
    else
        error="$(
            jq -r '.error // "Переустановка завершилась ошибкой."' \
                "$TEMP"
        )"
    fi
else
    output_json="$(
        cat "$TEMP" 2>/dev/null || printf '{}'
    )"

    error="$(
        printf '%s\n' "$output_json" |
            jq -r '.error // "Команда переустановки завершилась ошибкой."' \
                2>/dev/null ||
            printf 'Команда переустановки завершилась ошибкой.'
    )"
fi

rm -f "$TEMP"
exit 0
WORKER

chmod 0755 "$SCRIPT"
ash -n "$SCRIPT"

rm -f "$LOG" "$RESULT" "$PIDFILE"

start-stop-daemon \
    -S \
    -b \
    -m \
    -p "$PIDFILE" \
    -x /opt/bin/ash \
    -- "$SCRIPT"

WORKER_PID="$(
    cat "$PIDFILE" 2>/dev/null || true
)"

broray_api_success "$(
    jq -n \
        --arg pid "$WORKER_PID" \
        --arg log "$LOG" \
        --arg result "$RESULT" \
        --arg startedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        {
            accepted: true,
            operation: "reinstall",
            pid: (
                if $pid == ""
                then null
                else ($pid | tonumber)
                end
            ),
            logPath: $log,
            resultPath: $result,
            startedAt: $startedAt
        }
    '
)"
