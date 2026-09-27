#!/opt/bin/ash
. /opt/broray/web-new/api/auth-common.sh
. /opt/broray/lib/operation-client.sh

broray_operations_api()
{
    local method verb body_dir body_file rc response code http message paused id nonce action context path
    method="$1"; verb="$2"
    broray_api_require_method "$method"
    broray_api_require_session
    [ -z "${QUERY_STRING:-}" ] || broray_api_error '400 Bad Request' INVALID_REQUEST 'Параметры запроса не поддерживаются.'
    if [ "$method" = POST ]; then
        # Preserve session + custom-header checks. Validate a rewritten Host
        # against the exact live KeenDNS publication, never forwarded headers.
        [ "${HTTP_X_BRORAY_REQUEST:-}" = operations ] || broray_api_error '403 Forbidden' CSRF_REJECTED 'Обновите страницу и повторите действие.'
        [ -f /opt/broray/lib/operations-origin.sh ] &&
        [ ! -L /opt/broray/lib/operations-origin.sh ] ||
            broray_api_error '503 Service Unavailable' ORIGIN_VALIDATION_UNAVAILABLE 'Проверка источника запроса недоступна.'
        . /opt/broray/lib/operations-origin.sh ||
            broray_api_error '503 Service Unavailable' ORIGIN_VALIDATION_UNAVAILABLE 'Проверка источника запроса недоступна.'
        broray_operations_origin_allowed ||
            broray_api_error '403 Forbidden' ORIGIN_REJECTED 'Источник запроса не подтверждён.'
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
            servers-check|subscription-refresh)
                jq -e -L /opt/broray/lib 'include "operation-public";
                  keys==["id","nonce"] and (.nonce|type)=="string" and (.nonce|length)==32 and (.nonce|ascii_hex) and
                  (.id|type)=="string" and (.id|length)>0 and (.id|length)<=96 and
                  (.id|startswith(".")|not) and (.id|startswith("-")|not) and
                  (.id|explode|all(.[];(.>=48 and .<=57) or (.>=65 and .<=90) or (.>=97 and .<=122) or IN(45,46,95)))' \
                  "$body_file" >/dev/null || broray_api_error '400 Bad Request' INVALID_REQUEST 'Некорректные параметры задания.'
                id="$(jq -r .id "$body_file")"; nonce="$(jq -r .nonce "$body_file")"
                if [ "$verb" = servers-check ]; then
                    . "${BRORAY_ROOT:-/opt/broray}/lib/server-service.sh" ||
                        broray_api_error '503 Service Unavailable' STATE_UNAVAILABLE 'Служба серверов недоступна.'
                    if [ "$id" != all ]; then
                        [ -f "$BRORAY_SERVERS/$id.json" ] && [ ! -L "$BRORAY_SERVERS/$id.json" ] ||
                            broray_api_error '404 Not Found' SERVER_NOT_FOUND 'Сервер не найден.'
                    fi
                    action=servers:quality
                    context=''
                    if [ "$id" != all ]; then context="$(broray_active_proxy_context "$id" 2>/dev/null)" || context=''; fi
                    if [ -n "$context" ]; then action=servers:active-health
                    else context="$(broray_quality_context "$id" USER)" ||
                        broray_api_error '503 Service Unavailable' STATE_UNAVAILABLE 'Не удалось подтвердить список серверов.'; fi
                else
                    . "${BRORAY_ROOT:-/opt/broray}/lib/subscription-service.sh" ||
                        broray_api_error '503 Service Unavailable' STATE_UNAVAILABLE 'Служба подписок недоступна.'
                    path="$(broray_subscription_path "$id")" ||
                        broray_api_error '400 Bad Request' INVALID_REQUEST 'Некорректная подписка.'
                    [ -f "$path" ] && [ ! -L "$path" ] ||
                        broray_api_error '404 Not Found' SUBSCRIPTION_NOT_FOUND 'Подписка не найдена.'
                    context="$(sha256sum "$path" | cut -d ' ' -f 1)" ||
                        broray_api_error '503 Service Unavailable' STATE_UNAVAILABLE 'Не удалось прочитать подписку.'
                    action=subscriptions:refresh
                fi
                set -- queue-submit "$action" "$id" USER "$context" "$nonce" ;;
            cancel)
                jq -e -L /opt/broray/lib 'include "operation-public";
                  (keys==["operationId"] and (.operationId|operation_id)!=null) or
                  (keys==["requestId"] and (.requestId|request_id)!=null)' "$body_file" >/dev/null 2>&1 || broray_api_error '400 Bad Request' INVALID_REQUEST 'Некорректный идентификатор операции.'
                if jq -e 'has("requestId")' "$body_file" >/dev/null; then
                    set -- queue-cancel "$(jq -r '.requestId' "$body_file")"
                else set -- cancel "$(jq -r '.operationId' "$body_file")"; fi ;;
            queue-lookup)
                jq -e -L /opt/broray/lib 'include "operation-public";
                  keys==["nonce"] and (.nonce|type)=="string" and (.nonce|length)==32 and (.nonce|ascii_hex)' \
                  "$body_file" >/dev/null || broray_api_error '400 Bad Request' INVALID_REQUEST 'Некорректный идентификатор запроса.'
                set -- queue-lookup "$(jq -r '.nonce' "$body_file")" ;;
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
    case "$verb:$rc" in
        servers-check:0|subscription-refresh:0)
            response="$(printf '%s\n' "$response" | jq -c '. + {accepted:(.ok==true)}')" ;;
    esac
    http='200 OK'
    if [ "$rc" != 0 ] || printf '%s\n' "$response" | jq -e '.ok==false' >/dev/null; then
        code="$(printf '%s\n' "$response" | jq -r '.errorCode // "STATE_UNAVAILABLE"')"
        http='503 Service Unavailable'
        case "$code" in OPERATION_BUSY|DOMAIN_OPERATION_BUSY|CANCEL_NOT_SUPPORTED|RECOVERY_BLOCKED) http='409 Conflict' ;;
          REQUEST_UNCONFIRMED) http='404 Not Found' ;; esac
    elif [ "$method" = POST ] && [ "$verb" != automation ] && [ "$verb" != queue-lookup ]; then http='202 Accepted'; fi
    printf 'Status: %s\r\n' "$http"
    if [ "$verb" = report ]; then printf 'Content-Disposition: attachment; filename="BROray-diagnostics.json"\r\n'; fi
    broray_api_print_json_headers
    printf '\r\n%s\n' "$response"
}
