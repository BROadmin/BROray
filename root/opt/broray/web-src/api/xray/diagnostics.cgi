#!/opt/bin/ash

AUTH="/opt/broray/web-new/api/auth-common.sh"
BRORAY="/opt/broray/bin/broray"
XRAY_LIBRARY="/opt/broray/lib/xray.sh"

. "$AUTH"

broray_api_require_method GET
broray_api_require_session

if [ ! -x "$BRORAY" ] || [ ! -r "$XRAY_LIBRARY" ]; then
    broray_api_error \
        "500 Internal Server Error" \
        "XRAY_BACKEND_UNAVAILABLE" \
        "Backend Xray недоступен."
fi

. "$XRAY_LIBRARY"

STATUS_JSON="$(
    "$BRORAY" xray status 2>/dev/null || printf '{}'
)"

VALIDATE_JSON="$(
    "$BRORAY" xray validate 2>/dev/null || printf '{}'
)"

BINARY="/opt/broray/bin/xray"
CONFIG="/opt/broray/config/config.json"

binary_exists=false
binary_executable=false
config_exists=false
process_running=false
socks_active=false
config_valid=false
sha_available=false

[ -f "$BINARY" ] && binary_exists=true
[ -x "$BINARY" ] && binary_executable=true
[ -f "$CONFIG" ] && config_exists=true

if printf '%s\n' "$STATUS_JSON" |
    jq -e '.running == true' >/dev/null 2>&1
then
    process_running=true
fi

if printf '%s\n' "$STATUS_JSON" |
    jq -e '.socks.active == true' >/dev/null 2>&1
then
    socks_active=true
fi

if printf '%s\n' "$VALIDATE_JSON" |
    jq -e '.success == true' >/dev/null 2>&1
then
    config_valid=true
fi

BINARY_SHA=""

if [ -f "$BINARY" ] &&
   command -v sha256sum >/dev/null 2>&1
then
    BINARY_SHA="$(
        sha256sum "$BINARY" |
            awk '{print $1}'
    )"

    [ -n "$BINARY_SHA" ] && sha_available=true
fi

OPT_FREE_KB="$(
    df -k /opt 2>/dev/null |
        awk 'NR > 1 {print $4; exit}'
)"

TMP_FREE_KB="$(
    df -k /tmp 2>/dev/null |
        awk 'NR > 1 {print $4; exit}'
)"

case "$OPT_FREE_KB" in
    ''|*[!0-9]*)
        OPT_FREE_KB=0
        ;;
esac

case "$TMP_FREE_KB" in
    ''|*[!0-9]*)
        TMP_FREE_KB=0
        ;;
esac

LAST_TEST_SUCCESS=false
LAST_TEST_FILE=""

for candidate in \
    /opt/broray/run/xray-reinstall-test.json \
    /opt/broray/run/xray-lifecycle-test.json
do
    if [ -f "$candidate" ]; then
        LAST_TEST_FILE="$candidate"

        if jq -e '.success == true' \
            "$candidate" >/dev/null 2>&1
        then
            LAST_TEST_SUCCESS=true
        fi

        break
    fi
done

checks_json="$(
    jq -n \
        --argjson binaryExists "$binary_exists" \
        --argjson binaryExecutable "$binary_executable" \
        --argjson processRunning "$process_running" \
        --argjson configExists "$config_exists" \
        --argjson configValid "$config_valid" \
        --argjson socksActive "$socks_active" \
        --argjson shaAvailable "$sha_available" \
        --argjson optFreeKb "$OPT_FREE_KB" \
        --argjson tmpFreeKb "$TMP_FREE_KB" \
        --argjson lastTestSuccess "$LAST_TEST_SUCCESS" \
        --arg lastTestFile "$LAST_TEST_FILE" '
        [
            {
                id: "binary-exists",
                status: (
                    if $binaryExists
                    then "ok"
                    else "error"
                    end
                ),
                title: "Бинарный файл Xray",
                details: (
                    if $binaryExists
                    then "Файл найден."
                    else "Файл отсутствует."
                    end
                )
            },
            {
                id: "binary-executable",
                status: (
                    if $binaryExecutable
                    then "ok"
                    else "error"
                    end
                ),
                title: "Права запуска",
                details: (
                    if $binaryExecutable
                    then "Бинарный файл исполняемый."
                    else "Нет права исполнения."
                    end
                )
            },
            {
                id: "process-running",
                status: (
                    if $processRunning
                    then "ok"
                    else "warning"
                    end
                ),
                title: "Процесс Xray",
                details: (
                    if $processRunning
                    then "Процесс работает."
                    else "Процесс остановлен."
                    end
                )
            },
            {
                id: "config-exists",
                status: (
                    if $configExists
                    then "ok"
                    else "error"
                    end
                ),
                title: "Конфигурация",
                details: (
                    if $configExists
                    then "Файл конфигурации найден."
                    else "Файл конфигурации отсутствует."
                    end
                )
            },
            {
                id: "config-valid",
                status: (
                    if $configValid
                    then "ok"
                    else "error"
                    end
                ),
                title: "Проверка конфигурации",
                details: (
                    if $configValid
                    then "Configuration OK."
                    else "Конфигурация не прошла проверку."
                    end
                )
            },
            {
                id: "socks-active",
                status: (
                    if $socksActive
                    then "ok"
                    else "warning"
                    end
                ),
                title: "SOCKS",
                details: (
                    if $socksActive
                    then "Локальный SOCKS-порт активен."
                    else "SOCKS-порт не подтверждён."
                    end
                )
            },
            {
                id: "sha256",
                status: (
                    if $shaAvailable
                    then "ok"
                    else "warning"
                    end
                ),
                title: "SHA-256",
                details: (
                    if $shaAvailable
                    then "Контрольная сумма вычислена."
                    else "Контрольная сумма недоступна."
                    end
                )
            },
            {
                id: "opt-storage",
                status: (
                    if $optFreeKb >= 45000
                    then "ok"
                    elif $optFreeKb >= 20000
                    then "warning"
                    else "error"
                    end
                ),
                title: "Свободное место /opt",
                details: (($optFreeKb | tostring) + " КБ")
            },
            {
                id: "tmp-storage",
                status: (
                    if $tmpFreeKb >= 130000
                    then "ok"
                    elif $tmpFreeKb >= 90000
                    then "warning"
                    else "error"
                    end
                ),
                title: "Временное хранилище /tmp",
                details: (($tmpFreeKb | tostring) + " КБ")
            },
            {
                id: "real-device-test",
                status: (
                    if $lastTestSuccess
                    then "ok"
                    else "warning"
                    end
                ),
                title: "Испытание на Keenetic",
                details: (
                    if $lastTestSuccess
                    then $lastTestFile
                    else "Успешный результат испытания не найден."
                    end
                )
            }
        ]
    '
)"

broray_api_print_json_headers
printf '\r\n'

jq -n \
    --arg checkedAt "$(date '+%Y-%m-%dT%H:%M:%S%z')" \
    --arg binarySha256 "$BINARY_SHA" \
    --argjson status "$STATUS_JSON" \
    --argjson validation "$VALIDATE_JSON" \
    --argjson checks "$checks_json" '
    {
        success: true,
        data: {
            checkedAt: $checkedAt,
            status: $status,
            validation: $validation,
            binarySha256: (
                if $binarySha256 == ""
                then null
                else $binarySha256
                end
            ),
            checks: $checks,
            summary: {
                ok: (
                    [$checks[] | select(.status == "ok")] |
                    length
                ),
                warning: (
                    [$checks[] | select(.status == "warning")] |
                    length
                ),
                error: (
                    [$checks[] | select(.status == "error")] |
                    length
                )
            }
        },
        error: null
    }
'
