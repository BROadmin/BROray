#!/opt/bin/ash

MODE="${XRAY_WEB_OPERATION_MODE:-}"
RUN="/opt/broray/run"
BRORAY="/opt/broray/bin/broray"
SCRIPT="$RUN/xray-web-operation.sh"
LOG="$RUN/xray-web-operation.log"
RESULT="$RUN/xray-web-operation.json"
PIDFILE="$RUN/xray-web-operation.pid"
MODEFILE="$RUN/xray-web-operation.mode"
REQUESTFILE="$RUN/xray-web-operation.request.json"
umask 077

case "$MODE" in
    update|reinstall|install)
        ;;
    *)
        broray_api_error \
            "500 Internal Server Error" \
            "XRAY_OPERATION_MODE_INVALID" \
            "Получен неизвестный режим обслуживания Xray."
        ;;
esac

[ -r /opt/broray/lib/routes-api-operation.sh ] ||
    broray_api_error "500 Internal Server Error" "GLOBAL_LOCK_UNAVAILABLE" "Общий координатор операций недоступен."
. /opt/broray/lib/routes-api-operation.sh
lock_rc=0
broray_routes_api_lock_acquire "xray:$MODE" xray || lock_rc=$?
case "$lock_rc" in
    0)
        trap 'broray_routes_api_lock_release' EXIT
        trap 'exit 129' HUP
        trap 'exit 130' INT
        trap 'exit 143' TERM
        ;;
    2) broray_api_error "409 Conflict" "OPERATION_BUSY" "Другая конфликтующая операция BROray уже выполняется." ;;
    *) broray_api_error "500 Internal Server Error" "GLOBAL_LOCK_FAILED" "Не удалось установить общую блокировку BROray." ;;
esac

mkdir -p "$RUN" ||
    broray_api_error \
        "500 Internal Server Error" \
        "XRAY_OPERATION_STATE_UNAVAILABLE" \
        "Не удалось подготовить каталог операции Xray."

OLD_PID="$(cat "$PIDFILE" 2>/dev/null || true)"

case "$OLD_PID" in
    ''|*[!0-9]*)
        ;;
    *)
        if kill -0 "$OLD_PID" 2>/dev/null; then
            broray_api_error \
                "409 Conflict" \
                "XRAY_OPERATION_IN_PROGRESS" \
                "Другая операция обслуживания Xray уже выполняется."
        fi
        ;;
esac

if [ "$MODE" = install ]; then
    . /opt/broray/lib/web-request-body.sh
    BRORAY_BASE=/opt/broray
    . /opt/broray/lib/xray-releases.sh
    if ! broray_web_request_body_to_file "$REQUESTFILE.part" 4096 ||
       ! broray_xray_install_request_valid "$REQUESTFILE.part"; then
        rm -f "$REQUESTFILE.part"
        broray_api_error "400 Bad Request" "XRAY_INSTALL_REQUEST_INVALID" "Некорректный запрос выбора версии Xray. Обновите список версий."
    fi
    mv "$REQUESTFILE.part" "$REQUESTFILE" ||
        broray_api_error "500 Internal Server Error" "XRAY_REQUEST_SAVE_FAILED" "Не удалось сохранить выбор версии Xray."
fi

rm -f "$PIDFILE"
printf '%s\n' "$MODE" >"$MODEFILE.part" ||
    broray_api_error \
        "500 Internal Server Error" \
        "XRAY_OPERATION_STATE_FAILED" \
        "Не удалось сохранить режим операции Xray."
mv "$MODEFILE.part" "$MODEFILE" ||
    broray_api_error \
        "500 Internal Server Error" \
        "XRAY_OPERATION_STATE_FAILED" \
        "Не удалось активировать режим операции Xray."

cat >"$SCRIPT" <<'WORKER'
#!/opt/bin/ash

BRORAY="/opt/broray/bin/broray"
RUN="/opt/broray/run"
MODEFILE="$RUN/xray-web-operation.mode"
REQUESTFILE="$RUN/xray-web-operation.request.json"
LOG="$RUN/xray-web-operation.log"
RESULT="$RUN/xray-web-operation.json"
PIDFILE="$RUN/xray-web-operation.pid"
GLOBAL_LOCK="/opt/var/lock/broray/global-operation.lock"

MODE="$(cat "$MODEFILE" 2>/dev/null || true)"
case "$MODE" in
    update|reinstall|install)
        ;;
    *)
        exit 1
        ;;
esac

exec >"$LOG" 2>&1

success=false
error=""
output_json="{}"

finish()
{
    runtime_running=false
    runtime_pid=""

    runtime_pid="$(pidof xray 2>/dev/null | awk '{print $1}')"

    if [ -n "$runtime_pid" ]; then
        runtime_running=true
    fi

    if ! printf '%s\n' "$output_json" | jq -e . >/dev/null 2>&1; then
        output_json="{}"
    fi

    jq -n \
        --argjson success "$success" \
        --arg operation "$MODE" \
        --arg error "$error" \
        --argjson output "$output_json" \
        --argjson runtimeRunning "$runtime_running" \
        --arg runtimePid "$runtime_pid" \
        --arg completedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        {
            success: $success,
            operation: $operation,
            result: $output,
            running: false,
            pid: null,
            operationRunning: false,
            workerPid: null,
            runtimeRunning: $runtimeRunning,
            runtimePid: (
                if $runtimePid == ""
                then null
                else ($runtimePid | tonumber)
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
    ' >"$RESULT.part" 2>/dev/null &&
        jq -e . "$RESULT.part" >/dev/null 2>&1 &&
        mv "$RESULT.part" "$RESULT"

    lock_owner="$(sed -n '1p' "$GLOBAL_LOCK/pid" 2>/dev/null || true)"
    lock_action="$(sed -n '1p' "$GLOBAL_LOCK/action" 2>/dev/null || true)"
    if [ "$lock_owner" = "$$" ] && [ "$lock_action" = "xray:$MODE" ]; then
        rm -rf "$GLOBAL_LOCK" 2>/dev/null || true
    fi
    rm -f "$RESULT.part" "$PIDFILE" "$REQUESTFILE"
}

trap finish EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# Do not touch Xray until the CGI has durably transferred the shared fence to
# this exact worker PID.  Thus every post-spawn handoff failure is still
# pre-mutation and the parent can safely return an error and release its lock.
handoff_ready=false
handoff_attempt=0
while [ "$handoff_attempt" -lt 5 ]; do
    lock_owner="$(sed -n '1p' "$GLOBAL_LOCK/pid" 2>/dev/null || true)"
    lock_action="$(sed -n '1p' "$GLOBAL_LOCK/action" 2>/dev/null || true)"
    if [ "$lock_owner" = "$$" ] && [ "$lock_action" = "xray:$MODE" ]; then
        handoff_ready=true
        break
    fi
    handoff_attempt=$((handoff_attempt + 1))
    sleep 1
done
[ "$handoff_ready" = true ] || {
    error="Не удалось подтвердить передачу общей блокировки Xray."
    exit 1
}

TEMP="/opt/broray/tmp/broray-xray-web-$MODE-$$.json"
rm -f "$TEMP"

set -- "$BRORAY" xray "$MODE"
[ "$MODE" != install ] || set -- "$@" "$REQUESTFILE"
if BRORAY_XRAY_PARENT_LOCK=1 "$@" >"$TEMP"; then
    if jq -e . "$TEMP" >/dev/null 2>&1; then
        output_json="$(cat "$TEMP")"
    fi

    if [ "$(jq -r '.success // false' "$TEMP" 2>/dev/null)" = true ]; then
        success=true
    else
        error="$(
            jq -r '.error // "Операция Xray завершилась ошибкой."' \
                "$TEMP" 2>/dev/null ||
                printf 'Операция Xray завершилась ошибкой.'
        )"
    fi
else
    if jq -e . "$TEMP" >/dev/null 2>&1; then
        output_json="$(cat "$TEMP")"
    fi

    error="$(
        printf '%s\n' "$output_json" |
            jq -r '.error // "Команда обслуживания Xray завершилась ошибкой."' \
                2>/dev/null ||
            printf 'Команда обслуживания Xray завершилась ошибкой.'
    )"
fi

rm -f "$TEMP"
exit 0
WORKER

chmod 0755 "$SCRIPT" ||
    broray_api_error \
        "500 Internal Server Error" \
        "XRAY_OPERATION_WORKER_FAILED" \
        "Не удалось назначить права рабочему процессу Xray."

/opt/bin/ash -n "$SCRIPT" ||
    broray_api_error \
        "500 Internal Server Error" \
        "XRAY_OPERATION_WORKER_INVALID" \
        "Рабочий процесс Xray не прошёл проверку BusyBox ash."

rm -f "$LOG" "$RESULT" "$PIDFILE"

if ! start-stop-daemon \
    -S \
    -b \
    -m \
    -p "$PIDFILE" \
    -x /opt/bin/ash \
    -- "$SCRIPT"
then
    rm -f "$PIDFILE" "$MODEFILE"
    broray_api_error \
        "500 Internal Server Error" \
        "XRAY_OPERATION_START_FAILED" \
        "Не удалось запустить фоновую операцию Xray."
fi

WORKER_PID="$(cat "$PIDFILE" 2>/dev/null || true)"
case "$WORKER_PID" in ''|*[!0-9]*) broray_api_error "500 Internal Server Error" "XRAY_OPERATION_OWNER_INVALID" "Не удалось подтвердить владельца фоновой операции Xray." ;; esac

# Transfer the common lock before returning HTTP 202.  The worker releases it
# only in its finish trap, so update/reinstall stays fenced for its full life.
lock_owner="$(sed -n '1p' "$BRORAY_ROUTES_API_LOCK/pid" 2>/dev/null || true)"
[ "$lock_owner" = "$$" ] || broray_api_error "500 Internal Server Error" "GLOBAL_LOCK_OWNERSHIP_LOST" "Владение общей блокировкой Xray потеряно."
printf '%s\n' "$WORKER_PID" >"$BRORAY_ROUTES_API_LOCK/pid.new.$$" &&
    mv -f "$BRORAY_ROUTES_API_LOCK/pid.new.$$" "$BRORAY_ROUTES_API_LOCK/pid" ||
    broray_api_error "500 Internal Server Error" "GLOBAL_LOCK_HANDOFF_FAILED" "Не удалось передать блокировку фоновой операции Xray."
BRORAY_ROUTES_API_LOCK_HELD=false

broray_api_success "$(
    jq -n \
        --arg operation "$MODE" \
        --arg pid "$WORKER_PID" \
        --arg log "$LOG" \
        --arg result "$RESULT" \
        --arg startedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
        {
            accepted: true,
            operation: $operation,
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
