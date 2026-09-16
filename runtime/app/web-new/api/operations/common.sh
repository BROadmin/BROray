#!/opt/bin/ash
. /opt/broray/web-new/api/auth-common.sh
. /opt/broray/lib/operation-client.sh

broray_operations_api()
{
    local method verb body_dir body_file rc response code http message paused
    method="$1"; verb="$2"
    broray_api_require_method "$method"
    broray_api_require_session
    [ -z "${QUERY_STRING:-}" ] || broray_api_error '400 Bad Request' INVALID_REQUEST 'Параметры запроса не поддерживаются.'
    if [ "$method" = POST ]; then
        # A custom header plus an exact same-host Origin prevents form and
        # cross-origin requests. No CORS or JSONP is enabled on these endpoints.
        [ "${HTTP_X_BRORAY_REQUEST:-}" = operations ] || broray_api_error '403 Forbidden' CSRF_REJECTED 'Обновите страницу и повторите действие.'
        [ -n "${HTTP_HOST:-}" ] || broray_api_error '403 Forbidden' ORIGIN_REJECTED 'Источник запроса не подтверждён.'
        case "${HTTP_ORIGIN:-}" in
            "http://$HTTP_HOST"|"https://$HTTP_HOST") ;;
            *) broray_api_error '403 Forbidden' ORIGIN_REJECTED 'Источник запроса не подтверждён.' ;;
        esac
        case "${CONTENT_TYPE:-}" in application/json|'application/json; charset=utf-8') ;;
            *) broray_api_error '415 Unsupported Media Type' INVALID_CONTENT_TYPE 'Требуется JSON.' ;;
        esac
        case "${CONTENT_LENGTH:-}" in ''|*[!0-9]*|?????????*) broray_api_error '400 Bad Request' INVALID_LENGTH 'Некорректная длина запроса.' ;; esac
        [ "$CONTENT_LENGTH" -le 4096 ] || broray_api_error '413 Payload Too Large' REQUEST_TOO_LARGE 'Запрос превышает 4 КБ.'
        umask 077
        body_dir="$(mktemp -d /tmp/broray-operations-http.XXXXXX)" || broray_api_error '503 Service Unavailable' STATE_UNAVAILABLE 'Не удалось прочитать запрос.'
        body_file="$body_dir/body.json"
        BRORAY_OPS_HTTP_DIR="$body_dir"
        trap 'rm -f "$BRORAY_OPS_HTTP_DIR/body.json"; rmdir "$BRORAY_OPS_HTTP_DIR"' EXIT
        trap 'exit 143' TERM
        . /opt/broray/lib/web-request-body.sh
        broray_web_request_body_to_file "$body_file" 4096 || broray_api_error '400 Bad Request' INVALID_BODY 'Запрос получен не полностью.'
        # Exactly one object. jq -s also rejects concatenated JSON documents.
        jq -se 'length==1 and (.[0]|type)=="object"' "$body_file" >/dev/null 2>&1 || broray_api_error '400 Bad Request' INVALID_BODY 'Требуется один JSON-объект.'
        case "$verb" in
            cancel)
                jq -e -L /opt/broray/lib 'include "operation-public"; keys==["operationId"] and (.operationId|operation_id)!=null' "$body_file" >/dev/null 2>&1 || broray_api_error '400 Bad Request' INVALID_REQUEST 'Некорректный идентификатор операции.'
                set -- cancel "$(jq -r '.operationId' "$body_file")" ;;
            stop-background)
                jq -e 'keys==["pauseAutomation"] and .pauseAutomation==true' "$body_file" >/dev/null || broray_api_error '400 Bad Request' INVALID_REQUEST 'Требуется пауза автоматики.'
                set -- stop-background ;;
            automation)
                jq -e 'keys==["paused"] and (.paused|type)=="boolean"' "$body_file" >/dev/null || broray_api_error '400 Bad Request' INVALID_REQUEST 'Некорректное состояние автоматики.'
                paused="$(jq -r '.paused' "$body_file")"
                if [ "$paused" = true ]; then set -- pause; else set -- resume; fi ;;
            recover)
                jq -e 'keys==[]' "$body_file" >/dev/null || broray_api_error '400 Bad Request' INVALID_REQUEST 'Параметры восстановления не поддерживаются.'
                set -- recover ;;
            *) broray_api_error '400 Bad Request' INVALID_REQUEST 'Неизвестное действие.' ;;
        esac
    else
        case "${CONTENT_LENGTH:-0}" in ''|0) ;; *) broray_api_error '400 Bad Request' INVALID_REQUEST 'Тело GET-запроса не поддерживается.' ;; esac
        set -- "$verb"
    fi
    rc=0; response="$(broray_ops_call "$@" 2>/dev/null)" || rc=$?
    printf '%s\n' "$response" | jq -e 'type=="object"' >/dev/null 2>&1 || broray_api_error '503 Service Unavailable' STATE_UNAVAILABLE 'Состояние операций временно недоступно.'
    http='200 OK'
    if [ "$rc" != 0 ] || printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null; then
        code="$(printf '%s\n' "$response" | jq -r '.errorCode // "STATE_UNAVAILABLE"')"
        http='503 Service Unavailable'
        case "$code" in OPERATION_BUSY|DOMAIN_OPERATION_BUSY|CANCEL_NOT_SUPPORTED|RECOVERY_BLOCKED) http='409 Conflict' ;; esac
    elif [ "$method" = POST ] && [ "$verb" != automation ]; then http='202 Accepted'; fi
    printf 'Status: %s\r\n' "$http"
    if [ "$verb" = report ]; then printf 'Content-Disposition: attachment; filename="BROray-diagnostics.json"\r\n'; fi
    broray_api_print_json_headers
    printf '\r\n%s\n' "$response"
}
