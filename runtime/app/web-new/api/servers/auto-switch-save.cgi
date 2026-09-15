#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
CONFIG="/opt/broray/config/system/server-auto-switch.json"
SERVERS="/opt/broray/servers"
INIT="/opt/etc/init.d/S27broray-auto-switch"

. "$AUTH"
. /opt/broray/lib/web-request-body.sh

broray_api_require_method POST
broray_api_require_session

[ -r /opt/broray/lib/routes-api-operation.sh ] ||
    broray_api_error "500 Internal Server Error" "GLOBAL_LOCK_UNAVAILABLE" "Общий координатор операций недоступен."
. /opt/broray/lib/routes-api-operation.sh
lock_rc=0
broray_routes_api_lock_acquire 'servers:auto-switch-save' servers || lock_rc=$?
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

BODY_FILE="/opt/broray/tmp/broray-auto-switch-body-$$"
body_rc=0
broray_web_request_body_to_file "$BODY_FILE" 16384 || body_rc=$?
case "$body_rc" in
    0) ;;
    2|3) broray_api_error "400 Bad Request" "INVALID_CONTENT_LENGTH" "Тело запроса пусто или имеет некорректный размер." ;;
    4) broray_api_error "413 Payload Too Large" "REQUEST_TOO_LARGE" "Запрос слишком большой." ;;
    *) broray_api_error "400 Bad Request" "REQUEST_BODY_INCOMPLETE" "Тело запроса передано не полностью." ;;
esac
BODY="$(cat "$BODY_FILE")"
rm -f "$BODY_FILE"

if ! printf '%s' "$BODY" | jq -e 'type == "object"' >/dev/null 2>&1; then
    broray_api_error \
        "400 Bad Request" \
        "INVALID_JSON" \
        "Тело запроса должно быть объектом JSON."
fi

ENABLED="$(printf '%s' "$BODY" | jq -r '.enabled')"
THRESHOLD="$(printf '%s' "$BODY" | jq -r '.failureThreshold')"
COOLDOWN="$(printf '%s' "$BODY" | jq -r '.cooldownMinutes')"
MINIMUM="$(printf '%s' "$BODY" | jq -r '.minimumRating')"
RULE="$(printf '%s' "$BODY" | jq -r '.selectionRule')"
PREFERRED="$(printf '%s' "$BODY" | jq -r '.preferredServerId // empty')"
QUALITY_REFRESH_ENABLED="$(jq -r 'if (.qualityRefreshEnabled | type) == "boolean" then .qualityRefreshEnabled else false end' "$CONFIG" 2>/dev/null)"
QUALITY_REFRESH_INTERVAL="$(jq -r '.qualityRefreshIntervalMinutes // 60' "$CONFIG" 2>/dev/null)"

case "$QUALITY_REFRESH_ENABLED" in
    true|false) ;;
    *) QUALITY_REFRESH_ENABLED=false ;;
esac

case "$QUALITY_REFRESH_INTERVAL" in
    30|60|180|360) ;;
    *) QUALITY_REFRESH_INTERVAL=60 ;;
esac

case "$ENABLED" in
    true|false) ;;
    *)
        broray_api_error \
            "400 Bad Request" \
            "INVALID_ENABLED" \
            "Поле enabled должно быть логическим."
        ;;
esac

case "$THRESHOLD" in
    ''|*[!0-9]*)
        broray_api_error \
            "400 Bad Request" \
            "INVALID_THRESHOLD" \
            "Порог ошибок должен быть числом."
        ;;
esac

if [ "$THRESHOLD" -lt 1 ] || [ "$THRESHOLD" -gt 10 ]; then
    broray_api_error \
        "400 Bad Request" \
        "INVALID_THRESHOLD" \
        "Порог ошибок должен быть от 1 до 10."
fi

case "$COOLDOWN" in
    ''|*[!0-9]*)
        broray_api_error \
            "400 Bad Request" \
            "INVALID_COOLDOWN" \
            "Защитный интервал должен быть числом."
        ;;
esac

if [ "$COOLDOWN" -lt 1 ] || [ "$COOLDOWN" -gt 120 ]; then
    broray_api_error \
        "400 Bad Request" \
        "INVALID_COOLDOWN" \
        "Защитный интервал должен быть от 1 до 120 минут."
fi

case "$MINIMUM" in
    excellent|good|acceptable|poor) ;;
    *)
        broray_api_error \
            "400 Bad Request" \
            "INVALID_MINIMUM_RATING" \
            "Некорректное минимальное качество."
        ;;
esac

case "$RULE" in
    best-quality|lowest-ping|preferred) ;;
    *)
        broray_api_error \
            "400 Bad Request" \
            "INVALID_SELECTION_RULE" \
            "Некорректное правило выбора."
        ;;
esac

if [ "$RULE" = "preferred" ] && [ -z "$PREFERRED" ]; then
    broray_api_error \
        "400 Bad Request" \
        "PREFERRED_SERVER_REQUIRED" \
        "Выберите предпочтительный сервер."
fi

if [ -n "$PREFERRED" ]; then
    case "$PREFERRED" in
        *[!A-Za-z0-9._-]*)
            broray_api_error \
                "400 Bad Request" \
                "INVALID_SERVER_ID" \
                "Некорректный идентификатор сервера."
            ;;
    esac

    [ -f "$SERVERS/$PREFERRED.json" ] ||
        broray_api_error \
            "400 Bad Request" \
            "SERVER_NOT_FOUND" \
            "Предпочтительный сервер не найден."
fi

TMP="$CONFIG.new.$$"
umask 077

jq -n \
    --argjson enabled "$ENABLED" \
    --argjson failureThreshold "$THRESHOLD" \
    --argjson cooldownMinutes "$COOLDOWN" \
    --arg minimumRating "$MINIMUM" \
    --arg selectionRule "$RULE" \
    --arg preferredServerId "$PREFERRED" \
    --argjson qualityRefreshEnabled "$QUALITY_REFRESH_ENABLED" \
    --argjson qualityRefreshIntervalMinutes "$QUALITY_REFRESH_INTERVAL" \
    --arg updatedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
    {
        schemaVersion: 3,
        enabled: $enabled,
        failureThreshold: $failureThreshold,
        cooldownMinutes: $cooldownMinutes,
        minimumRating: $minimumRating,
        selectionRule: $selectionRule,
        preferredServerId: (if $preferredServerId == "" then null else $preferredServerId end),
        qualityRefreshEnabled: $qualityRefreshEnabled,
        qualityRefreshIntervalMinutes: $qualityRefreshIntervalMinutes,
        updatedAt: $updatedAt
    }
' > "$TMP" || {
    rm -f "$TMP"
    broray_api_error \
        "500 Internal Server Error" \
        "CONFIG_WRITE_FAILED" \
        "Не удалось сформировать настройки."
}

chmod 0600 "$TMP"
mv "$TMP" "$CONFIG" || {
    rm -f "$TMP"
    broray_api_error \
        "500 Internal Server Error" \
        "CONFIG_WRITE_FAILED" \
        "Не удалось сохранить настройки."
}

if [ -x "$INIT" ]; then
    "$INIT" start >/dev/null 2>&1 || true
fi

broray_api_success "$(cat "$CONFIG")"
