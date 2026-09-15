#!/opt/bin/ash

# Execute updaterctl completely before emitting CGI headers.  A successful
# enqueue therefore always returns a non-empty JSON body with HTTP 202.

broray_updater_api_call()
{
    success_status="$1"
    shift

    api_command="${1:-}"
    api_subcommand="${2:-}"

    api_tmp_root="${BRORAY_API_TMP_ROOT:-/opt/broray/tmp}"
    updater_ctl="${BRORAY_UPDATER_CTL:-/opt/bin/broray-updaterctl}"
    api_ash="${BRORAY_API_ASH:-/opt/bin/ash}"
    mkdir -p "$api_tmp_root" || broray_api_error \
        '500 Internal Server Error' \
        RUNTIME_UNAVAILABLE \
        'Не удалось подготовить временный файл ответа.'

    api_output="$api_tmp_root/updater-api-output.$$.json"
    api_error="$api_tmp_root/updater-api-error.$$"
    api_enriched="$api_tmp_root/updater-api-enriched.$$.json"
    api_handoff="$api_tmp_root/updater-api-handoff.$$.json"
    handoff="${BRORAY_PLATFORM_HANDOFF:-/opt/broray/current/app/lib/universal-platform-handoff.sh}"
    rm -f -- "$api_output" "$api_error" "$api_enriched" "$api_handoff"

    # If the application was activated by an older compact updater, the same
    # update operation remains active until its universal platform handoff is
    # terminal.  A retry from the new WebUI resumes that handoff; it never
    # creates a second app update and never touches OPKG.
    if [ "$api_command" = request ] &&
       { [ "$api_subcommand" = update ] || [ "$api_subcommand" = reinstall ]; } &&
       [ -f "$handoff" ] && [ ! -L "$handoff" ] && [ -x "$handoff" ]
    then
        handoff_rc=0
        "$api_ash" "$handoff" status >"$api_handoff" 2>/dev/null || handoff_rc=$?
        if [ "$handoff_rc" -ne 0 ] ||
           ! jq -e '.state=="success" and .running==false and .code=="UNIVERSAL_PLATFORM_READY"' \
                "$api_handoff" >/dev/null 2>&1
        then
            if "$api_ash" "$handoff" schedule >"$api_error" 2>&1; then
                operation_id="$(jq -r '.operationId // "platform-handoff"' "$api_handoff" 2>/dev/null || printf '%s' platform-handoff)"
                jq -nc --arg operationId "$operation_id" '
                  {ok:true,accepted:true,operation:"update",state:"queued",operationId:$operationId,continuation:"universal-platform-handoff"}' >"$api_output"
                printf 'Status: %s\r\n' "$success_status"
                broray_api_print_json_headers
                printf '\r\n'
                cat "$api_output"
                rm -f -- "$api_output" "$api_error" "$api_enriched" "$api_handoff"
                exit 0
            fi
            details="$(tail -n 12 "$api_error" 2>/dev/null || true)"
            rm -f -- "$api_output" "$api_error" "$api_enriched" "$api_handoff"
            broray_api_error \
                '503 Service Unavailable' \
                PLATFORM_HANDOFF_FAILED \
                "Не удалось продолжить переход на универсальный updater. $details"
        fi
    fi

    updater_rc=0
    "$updater_ctl" "$@" >"$api_output" 2>"$api_error" || updater_rc=$?

    # The source updater may already report app-slot success while the
    # transactionally scheduled platform continuation is still running.  Keep
    # the original WebUI poll alive until the complete universal release is
    # ready, or expose its rollback failure as the terminal update result.
    if [ "$api_command" = status ] &&
       [ "$updater_rc" -eq 0 ] &&
       [ -s "$api_output" ] &&
       jq -e 'type=="object" and .ok==true' "$api_output" >/dev/null 2>&1 &&
       [ -f "$handoff" ] && [ ! -L "$handoff" ] && [ -x "$handoff" ]
    then
        handoff_rc=0
        "$api_ash" "$handoff" status >"$api_handoff" 2>/dev/null || handoff_rc=$?
        if [ -s "$api_handoff" ] && jq -e 'type=="object"' "$api_handoff" >/dev/null 2>&1; then
            handoff_state="$(jq -r '.state // "error"' "$api_handoff")"
            case "$handoff_state" in
                running)
                    jq --slurpfile handoff "$api_handoff" '
                      .operation="update" |
                      .state="running" |
                      .stage="platform" |
                      .progress=98 |
                      .running=true |
                      .message=$handoff[0].message |
                      .error=null |
                      .platformHandoff=$handoff[0]
                    ' "$api_output" >"$api_enriched" && mv -f "$api_enriched" "$api_output"
                    ;;
                error)
                    jq --slurpfile handoff "$api_handoff" '
                      .operation="update" |
                      .state="error" |
                      .stage="platform" |
                      .progress=100 |
                      .running=false |
                      .message=$handoff[0].message |
                      .error=$handoff[0].message |
                      .platformHandoff=$handoff[0]
                    ' "$api_output" >"$api_enriched" && mv -f "$api_enriched" "$api_output"
                    ;;
                success)
                    jq --slurpfile handoff "$api_handoff" '.platformHandoff=$handoff[0]' \
                        "$api_output" >"$api_enriched" && mv -f "$api_enriched" "$api_output"
                    ;;
            esac
        fi
    fi

    if [ "$updater_rc" -eq 0 ] &&
       [ -s "$api_output" ] &&
       jq -e 'type == "object" and .ok == true' "$api_output" >/dev/null 2>&1
    then
        printf 'Status: %s\r\n' "$success_status"
        broray_api_print_json_headers
        printf '\r\n'
        cat "$api_output"
        rm -f -- "$api_output" "$api_error" "$api_enriched" "$api_handoff"
        exit 0
    fi

    if [ ! -s "$api_output" ] ||
       ! jq -e 'type == "object" and .ok == false and (.error | type == "object")' \
            "$api_output" >/dev/null 2>&1
    then
        details="$(tail -n 12 "$api_error" 2>/dev/null || true)"
        jq -nc \
            --arg details "$details" \
            --argjson updaterRc "$updater_rc" '
            {
              ok:false,
              error:{
                code:"UPDATER_BACKEND_FAILED",
                message:"Постоянный updater не вернул корректный ответ.",
                details:(if $details == "" then null else $details end),
                updaterRc:$updaterRc
              }
            }' >"$api_output"
    else
        jq --argjson updaterRc "$updater_rc" '.error.updaterRc = $updaterRc' \
            "$api_output" >"$api_enriched" || {
                rm -f -- "$api_output" "$api_error" "$api_enriched" "$api_handoff"
                broray_api_error \
                    '500 Internal Server Error' \
                    RESPONSE_ENCODING_FAILED \
                    'Не удалось сохранить код завершения updater.'
            }
        mv -f "$api_enriched" "$api_output"
    fi

    code="$(jq -r '.error.code // "UPDATER_BACKEND_FAILED"' "$api_output")"
    case "$code" in
        OPERATION_BUSY|UPDATE_NOT_AVAILABLE|UPDATE_CHECK_REQUIRED)
            failure_status='409 Conflict'
            ;;
        UPDATE_CHECK_FAILED|UPDATE_INDEX_INVALID)
            failure_status='502 Bad Gateway'
            ;;
        *)
            failure_status='503 Service Unavailable'
            ;;
    esac

    printf 'Status: %s\r\n' "$failure_status"
    broray_api_print_json_headers
    printf '\r\n'
    cat "$api_output"
    rm -f -- "$api_output" "$api_error" "$api_enriched" "$api_handoff"
    exit 0
}
