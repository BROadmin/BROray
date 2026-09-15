#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
CONFIG="/opt/broray/config/system/server-auto-switch.json"
INIT="/opt/etc/init.d/S27broray-auto-switch"

. "$AUTH"
. /opt/broray/lib/web-request-body.sh

broray_api_require_method POST
broray_api_require_session

[ -r /opt/broray/lib/routes-api-operation.sh ] ||
    broray_api_error "500 Internal Server Error" "GLOBAL_LOCK_UNAVAILABLE" "Общий координатор операций недоступен."
. /opt/broray/lib/routes-api-operation.sh
lock_rc=0
broray_routes_api_lock_acquire 'servers:quality-refresh-save' servers || lock_rc=$?
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

BODY_FILE="/opt/broray/tmp/broray-quality-refresh-body-$$"
body_rc=0
broray_web_request_body_to_file "$BODY_FILE" 4096 || body_rc=$?
case "$body_rc" in
    0) ;;
    2|3) broray_api_error "400 Bad Request" "INVALID_CONTENT_LENGTH" "Тело запроса пусто или имеет некорректный размер." ;;
    4) broray_api_error "413 Payload Too Large" "REQUEST_TOO_LARGE" "Запрос слишком большой." ;;
    *) broray_api_error "400 Bad Request" "REQUEST_BODY_INCOMPLETE" "Тело запроса передано не полностью." ;;
esac
BODY="$(cat "$BODY_FILE")"
rm -f "$BODY_FILE"

if ! printf '%s' "$BODY" | jq -e '
    type == "object" and
    (keys | sort) == ["enabled", "intervalMinutes"] and
    (.enabled | type) == "boolean" and
    (.intervalMinutes | type) == "number"
' >/dev/null 2>&1; then
    broray_api_error "400 Bad Request" "INVALID_JSON" "Ожидались поля enabled и intervalMinutes."
fi

QUALITY_ENABLED="$(printf '%s' "$BODY" | jq -r '.enabled')"
QUALITY_INTERVAL="$(printf '%s' "$BODY" | jq -r '.intervalMinutes')"
case "$QUALITY_INTERVAL" in
    30|60|180|360) ;;
    *) broray_api_error "400 Bad Request" "INVALID_INTERVAL" "Допустимые интервалы: 30, 60, 180 или 360 минут." ;;
esac

if ! jq -e '
    type == "object" and
    (.enabled | type) == "boolean" and
    (.failureThreshold | type) == "number" and
    (.cooldownMinutes | type) == "number" and
    (.minimumRating | type) == "string" and
    (.selectionRule | type) == "string" and
    ((.preferredServerId == null) or (.preferredServerId | type) == "string")
' "$CONFIG" >/dev/null 2>&1; then
    broray_api_error "500 Internal Server Error" "CONFIG_INVALID" "Текущие настройки серверов повреждены; автоматическая проверка не изменена."
fi

ENABLED="$(jq -r '.enabled' "$CONFIG")"
THRESHOLD="$(jq -r '.failureThreshold' "$CONFIG")"
COOLDOWN="$(jq -r '.cooldownMinutes' "$CONFIG")"
MINIMUM="$(jq -r '.minimumRating' "$CONFIG")"
RULE="$(jq -r '.selectionRule' "$CONFIG")"
PREFERRED="$(jq -r '.preferredServerId // empty' "$CONFIG")"

case "$THRESHOLD" in ''|*[!0-9]*) broray_api_error "500 Internal Server Error" "CONFIG_INVALID" "Текущий порог автовыбора некорректен." ;; esac
case "$COOLDOWN" in ''|*[!0-9]*) broray_api_error "500 Internal Server Error" "CONFIG_INVALID" "Текущий защитный интервал некорректен." ;; esac
[ "$THRESHOLD" -ge 1 ] && [ "$THRESHOLD" -le 10 ] || broray_api_error "500 Internal Server Error" "CONFIG_INVALID" "Текущий порог автовыбора вне допустимого диапазона."
[ "$COOLDOWN" -ge 1 ] && [ "$COOLDOWN" -le 120 ] || broray_api_error "500 Internal Server Error" "CONFIG_INVALID" "Текущий защитный интервал вне допустимого диапазона."
case "$MINIMUM" in excellent|good|acceptable|poor) ;; *) broray_api_error "500 Internal Server Error" "CONFIG_INVALID" "Текущее минимальное качество некорректно." ;; esac
case "$RULE" in best-quality|lowest-ping|preferred) ;; *) broray_api_error "500 Internal Server Error" "CONFIG_INVALID" "Текущее правило автовыбора некорректно." ;; esac
case "$PREFERRED" in *[!A-Za-z0-9._-]*) broray_api_error "500 Internal Server Error" "CONFIG_INVALID" "Идентификатор предпочтительного сервера некорректен." ;; esac

TMP="$CONFIG.new.$$"
umask 077
jq -n \
    --argjson enabled "$ENABLED" \
    --argjson failureThreshold "$THRESHOLD" \
    --argjson cooldownMinutes "$COOLDOWN" \
    --arg minimumRating "$MINIMUM" \
    --arg selectionRule "$RULE" \
    --arg preferredServerId "$PREFERRED" \
    --argjson qualityRefreshEnabled "$QUALITY_ENABLED" \
    --argjson qualityRefreshIntervalMinutes "$QUALITY_INTERVAL" \
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
    broray_api_error "500 Internal Server Error" "CONFIG_WRITE_FAILED" "Не удалось сформировать настройки автопроверки."
}

chmod 0600 "$TMP"
mv "$TMP" "$CONFIG" || {
    rm -f "$TMP"
    broray_api_error "500 Internal Server Error" "CONFIG_WRITE_FAILED" "Не удалось сохранить настройки автопроверки."
}

if [ -x "$INIT" ]; then
    "$INIT" start >/dev/null 2>&1 || true
fi

broray_api_success "$(jq '{enabled:.qualityRefreshEnabled,intervalMinutes:.qualityRefreshIntervalMinutes,updatedAt}' "$CONFIG")"
