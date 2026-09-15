#!/opt/bin/ash

umask 077

auth_common="${BRORAY_AUTH_COMMON:-/opt/broray/web-new/api/auth-common.sh}"
. "$auth_common"

broray_api_require_method GET
broray_api_require_session

api_tmp_root="${BRORAY_API_TMP_ROOT:-/opt/broray/tmp}"
backend="${BRORAY_SYSTEM_BACKEND:-/opt/broray/bin/broray-system}"
mkdir -p "$api_tmp_root" || broray_api_error \
    '500 Internal Server Error' \
    RUNTIME_UNAVAILABLE \
    'Не удалось подготовить временный файл ответа.'

api_output="$api_tmp_root/broray-info-output.$$.json"
api_error="$api_tmp_root/broray-info-error.$$"
api_handoff="$api_tmp_root/broray-info-handoff.$$.json"
api_enriched="$api_tmp_root/broray-info-enriched.$$.json"
handoff="${BRORAY_PLATFORM_HANDOFF:-/opt/broray/current/app/lib/universal-platform-handoff.sh}"
api_ash="${BRORAY_API_ASH:-/opt/bin/ash}"

cleanup()
{
    rm -f -- "$api_output" "$api_error" "$api_handoff" "$api_enriched"
}

trap cleanup EXIT HUP INT TERM
rm -f -- "$api_output" "$api_error" "$api_handoff" "$api_enriched"

if "$backend" info >"$api_output" 2>"$api_error" &&
   [ -s "$api_output" ] &&
   jq -e 'type == "object" and .ok == true' "$api_output" >/dev/null 2>&1
then
    handoff_rc=0
    if [ -f "$handoff" ] && [ ! -L "$handoff" ] && [ -x "$handoff" ]; then
        "$api_ash" "$handoff" status >"$api_handoff" 2>/dev/null || handoff_rc=$?
    else
        handoff_rc=1
    fi
    if [ "$handoff_rc" -eq 0 ] &&
       jq -e 'type=="object" and .state=="success" and .running==false and .code=="UNIVERSAL_PLATFORM_READY"' \
            "$api_handoff" >/dev/null 2>&1
    then
        jq --slurpfile handoff "$api_handoff" '
          .universalUpdaterReady=true |
          .platformHandoff=$handoff[0]
        ' "$api_output" >"$api_enriched" && mv -f "$api_enriched" "$api_output" ||
            broray_api_error \
                '500 Internal Server Error' \
                RESPONSE_ENCODING_FAILED \
                'Не удалось добавить состояние универсального updater.'
    else
        if [ ! -s "$api_handoff" ] || ! jq -e 'type=="object"' "$api_handoff" >/dev/null 2>&1; then
            jq -nc '
              {schemaVersion:1,contract:"broray-universal-platform-handoff/1",state:"error",running:false,code:"UNIVERSAL_PLATFORM_STATUS_INVALID",message:"Состояние универсального updater недоступно.",mutationStarted:false,rollbackPerformed:false,operationId:null,candidateId:null,recordedAt:null}
            ' >"$api_handoff"
        fi
        jq --slurpfile handoff "$api_handoff" '
          .universalUpdaterReady=false |
          .platformHandoff=$handoff[0] |
          .installationHealthy=false |
          .versionsConsistent=false |
          .reinstallSupported=false |
          .updateAvailable=true |
          .availableVersion=(.releaseId // .version // null)
        ' "$api_output" >"$api_enriched" && mv -f "$api_enriched" "$api_output" ||
            broray_api_error \
                '500 Internal Server Error' \
                RESPONSE_ENCODING_FAILED \
                'Не удалось добавить состояние перехода updater.'
    fi
    printf 'Status: 200 OK\r\n'
    broray_api_print_json_headers
    printf '\r\n'
    cat "$api_output"
    exit 0
fi

if [ ! -s "$api_output" ] ||
   ! jq -e 'type == "object" and .ok == false and (.error | type == "object")' \
        "$api_output" >/dev/null 2>&1
then
    details="$(tail -n 12 "$api_error" 2>/dev/null || true)"
    jq -nc \
        --arg details "$details" '
        {
          ok:false,
          error:{
            code:"SYSTEM_INFO_BACKEND_FAILED",
            message:"Backend страницы BROray не вернул корректный JSON.",
            details:(if $details == "" then null else $details end)
          }
        }' >"$api_output"
fi

printf 'Status: 503 Service Unavailable\r\n'
broray_api_print_json_headers
printf '\r\n'
cat "$api_output"
exit 0
