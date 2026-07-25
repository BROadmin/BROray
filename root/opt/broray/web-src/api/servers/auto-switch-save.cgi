#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
CONFIG="/opt/broray/config/system/server-auto-switch.json"
SERVERS="/opt/broray/servers"
INIT="/opt/etc/init.d/S27broray-auto-switch"

. "$AUTH"

broray_api_require_method POST
broray_api_require_session

CONTENT_LENGTH="${CONTENT_LENGTH:-0}"

case "$CONTENT_LENGTH" in
    ''|*[!0-9]*)
        broray_api_error \
            "400 Bad Request" \
            "INVALID_CONTENT_LENGTH" \
            "Некорректный размер запроса."
        ;;
esac

if [ "$CONTENT_LENGTH" -gt 16384 ]; then
    broray_api_error \
        "413 Payload Too Large" \
        "REQUEST_TOO_LARGE" \
        "Запрос слишком большой."
fi

BODY="$(dd bs=1 count="$CONTENT_LENGTH" 2>/dev/null)"

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
    --arg updatedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" '
    {
        schemaVersion: 2,
        enabled: $enabled,
        failureThreshold: $failureThreshold,
        cooldownMinutes: $cooldownMinutes,
        minimumRating: $minimumRating,
        selectionRule: $selectionRule,
        preferredServerId: (if $preferredServerId == "" then null else $preferredServerId end),
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
