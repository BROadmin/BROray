#!/opt/bin/ash
BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_BASE="$BRORAY_ROOT"
export BRORAY_ROOT BRORAY_BASE
. "$BRORAY_ROOT/lib/operation-job.sh"
MODE="${XRAY_WEB_OPERATION_MODE:-}"
case "$MODE" in install|update|reinstall) ;; *) broray_api_error '400 Bad Request' XRAY_OPERATION_MODE_INVALID 'Неизвестный режим обслуживания Xray.' ;; esac
RUN="$BRORAY_ROOT/run"
RESULT="$RUN/xray-web-operation.json"
umask 077
if [ -e "$RUN/xray-web-operation.pid" ] || [ -L "$RUN/xray-web-operation.pid" ]; then
    broray_api_error '409 Conflict' XRAY_LEGACY_OWNER_UNCONFIRMED 'Владелец предыдущей операции Xray не подтверждён. Откройте диагностику операций.'
fi
launch_rc=0
broray_job_begin routes "xray:$MODE" xray USER cooperative || launch_rc=$?
case "$launch_rc" in
  0) ;;
  2) broray_api_error '409 Conflict' OPERATION_BUSY 'Другая конфликтующая операция BROray уже выполняется.' ;;
  *) broray_api_error '503 Service Unavailable' OPERATION_UNAVAILABLE 'Не удалось подготовить операцию Xray.' ;;
esac
trap 'broray_job_exit "$?"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
OPERATION="$BRORAY_BACKGROUND_OPERATION_ID"
WORK="$RUN/xray-operations/$OPERATION"
mkdir -p "$RUN/xray-operations" && [ ! -L "$RUN/xray-operations" ] && mkdir -m 700 "$WORK" ||
  broray_api_error '500 Internal Server Error' XRAY_OPERATION_STATE_FAILED 'Не удалось подготовить каталог операции Xray.'
printf '%s\n' "$OPERATION" >"$WORK/operation-id" ||
  broray_api_error '500 Internal Server Error' XRAY_OPERATION_STATE_FAILED 'Не удалось записать идентификатор операции Xray.'
if [ "$MODE" = install ]; then
    . "$BRORAY_ROOT/lib/web-request-body.sh"
    . "$BRORAY_ROOT/lib/xray-releases.sh"
    if ! broray_web_request_body_to_file "$WORK/request.json" 4096 ||
       ! broray_xray_install_request_valid "$WORK/request.json"; then
        broray_api_error '400 Bad Request' XRAY_INSTALL_REQUEST_INVALID 'Некорректный запрос выбора версии Xray. Обновите список версий.'
    fi
fi
nonce="$(hexdump -n 16 -v -e '1/1 "%02x"' /dev/urandom)"
[ "${#nonce}" = 32 ] || broray_api_error '503 Service Unavailable' RANDOM_UNAVAILABLE 'Не удалось подготовить передачу операции.'
temporary="$RESULT.new.$$"
jq -n --arg id "$OPERATION" --arg mode "$MODE" \
  '{backgroundOperationId:$id,operation:$mode,operationRunning:true,running:true,success:null,result:null,error:null}' >"$temporary" &&
  chmod 600 "$temporary" && "${BRORAY_OPS_GUARD:-$BRORAY_ROOT/bin/broray-ops-guard}" --replace-file "$temporary" "$RESULT" ||
  broray_api_error '503 Service Unavailable' XRAY_OPERATION_STATE_FAILED 'Не удалось сохранить начало операции Xray.'
"${BRORAY_OPS_ASH:-/opt/bin/ash}" "$BRORAY_ROOT/lib/xray-web-worker.sh" "$WORK" "$MODE" "$nonce" \
  </dev/null >"$WORK/worker.log" 2>&1 & WORKER_PID=$!
if ! broray_ops_handoff_to "$WORKER_PID" "$nonce"; then
    BRORAY_JOB_UNRESOLVED=true
    broray_api_error '503 Service Unavailable' XRAY_HANDOFF_UNCONFIRMED 'Передача операции Xray ещё не подтверждена. Проверьте её состояние в журнале.'
fi
BRORAY_JOB_ACTIVE=false
printf 'Status: 202 Accepted\r\n'
broray_api_success "$(jq -n --arg operation "$MODE" --arg operationId "$OPERATION" \
  '{accepted:true,operation:$operation,operationId:$operationId,pid:null}')"
