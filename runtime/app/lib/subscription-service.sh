#!/opt/bin/ash

BRORAY_SUB_BASE="${BRORAY_SUB_BASE:-${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}}"
BRORAY_BASE="${BRORAY_BASE:-$BRORAY_SUB_BASE}"
BRORAY_ROOT="$BRORAY_BASE"
export BRORAY_ROOT

. "$BRORAY_BASE/lib/server-subscription-service.sh"
BRORAY_STATUS_LIBRARY="${BRORAY_STATUS_LIBRARY:-$BRORAY_BASE/lib/status-contract.sh}"
. "$BRORAY_STATUS_LIBRARY"
. "$BRORAY_BASE/lib/operation-job.sh"
. "$BRORAY_BASE/lib/subscription-job.sh"
BRORAY_SUB_DIR="${BRORAY_SUB_DIR:-$BRORAY_SUB_BASE/config/subscriptions}"
BRORAY_SUB_RUN="${BRORAY_SUB_RUN:-$BRORAY_SUB_BASE/run/subscriptions}"
BRORAY_SUB_TMP="${BRORAY_SUB_TMP:-$BRORAY_SUB_BASE/tmp}"
BRORAY_SUB_LOG="${BRORAY_SUB_LOG:-$BRORAY_SUB_BASE/logs/subscriptions.log}"
BRORAY_SUB_MAX_BYTES="${BRORAY_SUB_MAX_BYTES:-2097152}"
BRORAY_SUB_MAX_NODES="${BRORAY_SUB_MAX_NODES:-500}"
BRORAY_SUB_MIN_INTERVAL="${BRORAY_SUB_MIN_INTERVAL:-5}"
BRORAY_SUB_MAX_INTERVAL="${BRORAY_SUB_MAX_INTERVAL:-10080}"
BRORAY_SUB_USER_AGENT="BROray/3.0.0"

BRORAY_SUB_ERROR_CODE=""
BRORAY_SUB_ERROR_MESSAGE=""

broray_subscription_set_error()
{
    BRORAY_SUB_ERROR_CODE="$1"
    shift
    BRORAY_SUB_ERROR_MESSAGE="$*"
    return 1
}

broray_subscription_emit_error()
{
    printf 'BRORAY_ERROR:%s:%s\n' \
        "${BRORAY_SUB_ERROR_CODE:-INTERNAL_ERROR}" \
        "${BRORAY_SUB_ERROR_MESSAGE:-Внутренняя ошибка подписки.}" \
        >&2
    return 1
}

broray_subscription_now_epoch()
{
    date '+%s'
}

broray_subscription_now_iso()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_subscription_iso_from_epoch()
{
    iso_epoch="$1"
    date -d "@$iso_epoch" '+%Y-%m-%dT%H:%M:%S%z' 2>/dev/null || \
        date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_subscription_prepare_dirs()
{
    mkdir -p \
        "$BRORAY_SUB_DIR" \
        "$BRORAY_SUB_RUN" \
        "$BRORAY_SUB_TMP" \
        "$(dirname "$BRORAY_SUB_LOG")"
}

broray_subscription_log()
{
    broray_subscription_prepare_dirs
    if [ -f "$BRORAY_SUB_LOG" ]; then
        log_size="$(wc -c < "$BRORAY_SUB_LOG" 2>/dev/null | tr -d ' ')"
        case "$log_size" in
            ''|*[!0-9]*) log_size=0 ;;
        esac
        if [ "$log_size" -gt 262144 ]; then
            tail -n 600 "$BRORAY_SUB_LOG" > "$BRORAY_SUB_LOG.new" 2>/dev/null && \
                mv "$BRORAY_SUB_LOG.new" "$BRORAY_SUB_LOG"
        fi
    fi
    printf '%s %s\n' "$(broray_subscription_now_iso)" "$*" >> "$BRORAY_SUB_LOG"
}

broray_subscription_validate_id()
{
    validate_id="$1"
    [ -n "$validate_id" ] || {
        broray_subscription_set_error \
            "INVALID_SUBSCRIPTION_ID" \
            "Не указан идентификатор подписки."
        return 1
    }
    case "$validate_id" in
        *[!a-zA-Z0-9._-]*)
            broray_subscription_set_error \
                "INVALID_SUBSCRIPTION_ID" \
                "Идентификатор подписки содержит недопустимые символы."
            return 1
            ;;
    esac
    return 0
}

broray_subscription_path()
{
    path_id="$1"
    broray_subscription_validate_id "$path_id" || return 1
    printf '%s/%s.json\n' "$BRORAY_SUB_DIR" "$path_id"
}

broray_subscription_exists()
{
    exists_id="$1"
    exists_path="$(broray_subscription_path "$exists_id")" || return 1
    [ -f "$exists_path" ]
}

broray_subscription_validate_file()
{
    validate_file="$1"
    jq -e \
        --argjson min "$BRORAY_SUB_MIN_INTERVAL" \
        --argjson max "$BRORAY_SUB_MAX_INTERVAL" '
        type == "object" and
        .schemaVersion == 1 and
        (((.id | type) == "string") and ((.id | length) > 0)) and
        (((.name | type) == "string") and ((.name | length) > 0) and ((.name | length) <= 128)) and
        (((.url | type) == "string") and ((.url | length) > 0) and ((.url | length) <= 4096)) and
        ((has("clientHwid") | not) or
         (((.clientHwid | type) == "string") and
          ((.clientHwid | length) >= 10) and
          ((.clientHwid | length) <= 64))) and
        ((.enabled | type) == "boolean") and
        ((.autoUpdateEnabled | type) == "boolean") and
        (((.updateIntervalMinutes | type) == "number") and ((.updateIntervalMinutes | floor) == .updateIntervalMinutes) and (.updateIntervalMinutes >= $min) and (.updateIntervalMinutes <= $max)) and
        (.lastUpdateStatus | IN("never", "running", "success", "partial", "error")) and
        (((.serversReceived | type) == "number") and (.serversReceived >= 0)) and
        ((.createdAt | type) == "string") and
        ((.updatedAt | type) == "string")
    ' "$validate_file" >/dev/null 2>&1 || return 1

    if jq -e 'has("clientHwid")' "$validate_file" >/dev/null 2>&1; then
        validate_client_hwid="$(jq -r '.clientHwid' "$validate_file" 2>/dev/null)"
        broray_subscription_client_hwid_valid "$validate_client_hwid" || return 1
    fi
    return 0
}

broray_subscription_write_json()
{
    broray_job_checkpoint committing || return $?
    write_target="$1"
    write_source="$2"
    write_temp="$write_target.new.$$"
    jq -S . "$write_source" > "$write_temp" || {
        rm -f "$write_temp"
        broray_subscription_set_error \
            "PERSISTENCE_ERROR" \
            "Не удалось подготовить данные подписки."
        return 1
    }
    if ! broray_subscription_validate_file "$write_temp"; then
        rm -f "$write_temp"
        broray_subscription_set_error \
            "PERSISTENCE_ERROR" \
            "Данные подписки не прошли проверку."
        return 1
    fi
    chmod 600 "$write_temp" || true
    "${BRORAY_OPS_GUARD:-$BRORAY_BASE/bin/broray-ops-guard}" --replace-file "$write_temp" "$write_target" || {
        rm -f "$write_temp"
        broray_subscription_set_error \
            "PERSISTENCE_ERROR" \
            "Не удалось сохранить подписку."
        return 1
    }
    return 0
}

broray_subscription_generate_id()
{
    generate_entropy="$(
        {
            date '+%s%N' 2>/dev/null || date '+%s'
            printf '%s\n' "$$"
            dd if=/dev/urandom bs=16 count=1 2>/dev/null || true
        } | sha256sum | awk '{print $1}'
    )"
    printf 'sub-%s\n' "$(printf '%s' "$generate_entropy" | cut -c 1-16)"
}

broray_subscription_generate_client_hwid()
{
    client_hwid_entropy="$(
        {
            date '+%s%N' 2>/dev/null || date '+%s'
            printf '%s\n' "$$"
            dd if=/dev/urandom bs=32 count=1 2>/dev/null || true
        } | sha256sum | awk '{print $1}'
    )"
    printf 'broray-%s\n' "$(printf '%s' "$client_hwid_entropy" | cut -c 1-32)"
}

broray_subscription_client_hwid_valid()
{
    client_hwid_value="$1"
    client_hwid_length="${#client_hwid_value}"
    [ "$client_hwid_length" -ge 10 ] 2>/dev/null &&
        [ "$client_hwid_length" -le 64 ] 2>/dev/null || return 1
    case "$client_hwid_value" in
        *[!a-zA-Z0-9=-]*) return 1 ;;
    esac
    return 0
}

broray_subscription_ensure_client_hwid()
{
    ensure_hwid_file="$1"
    BRORAY_SUB_CLIENT_HWID="$(jq -r '.clientHwid // empty' "$ensure_hwid_file" 2>/dev/null)"
    if broray_subscription_client_hwid_valid "$BRORAY_SUB_CLIENT_HWID"; then
        return 0
    fi

    BRORAY_SUB_CLIENT_HWID="$(broray_subscription_generate_client_hwid)"
    broray_subscription_client_hwid_valid "$BRORAY_SUB_CLIENT_HWID" || {
        broray_subscription_set_error \
            "CLIENT_ID_ERROR" \
            "Не удалось создать анонимный идентификатор клиента подписки."
        return 1
    }
    ensure_hwid_temp="$BRORAY_SUB_TMP/subscription-client-hwid.$$.json"
    jq --arg clientHwid "$BRORAY_SUB_CLIENT_HWID" \
        '.clientHwid = $clientHwid' \
        "$ensure_hwid_file" > "$ensure_hwid_temp" || {
        rm -f "$ensure_hwid_temp"
        broray_subscription_set_error \
            "PERSISTENCE_ERROR" \
            "Не удалось подготовить анонимный идентификатор подписки."
        return 1
    }
    broray_subscription_write_json "$ensure_hwid_file" "$ensure_hwid_temp" || {
        rm -f "$ensure_hwid_temp"
        return 1
    }
    rm -f "$ensure_hwid_temp"
    return 0
}

broray_subscription_mask_url()
{
    mask_url="$1"

    case "$mask_url" in
        http://*|https://*)
            ;;
        *)
            printf '%s\n' '***'
            return 0
            ;;
    esac

    mask_scheme="${mask_url%%://*}"
    mask_rest="${mask_url#*://}"
    mask_rest="${mask_rest%%#*}"

    case "$mask_rest" in
        */*)
            mask_authority="${mask_rest%%/*}"
            mask_path="/${mask_rest#*/}"
            ;;
        *)
            mask_authority="$mask_rest"
            mask_path=""
            ;;
    esac

    case "$mask_authority" in
        *@*)
            mask_authority="***@${mask_authority#*@}"
            ;;
    esac

    mask_has_query=false

    case "$mask_path" in
        *\?*)
            mask_has_query=true
            mask_path="${mask_path%%\?*}"
            ;;
    esac

    mask_last_segment="${mask_path##*/}"
    mask_secret_path=false

    case "$mask_path" in
        /sub/*|\
        /subscription/*|\
        /subscriptions/*|\
        /subscribe/*|\
        /link/*|\
        /s/*)
            mask_secret_path=true
            ;;
        *)
            if [ "${#mask_last_segment}" -ge 12 ]; then
                mask_secret_path=true
            fi
            ;;
    esac

    if [ "$mask_secret_path" = true ] &&
       [ -n "$mask_last_segment" ]; then
        mask_path="${mask_path%/*}/***"
    fi

    printf '%s://%s%s' \
        "$mask_scheme" \
        "$mask_authority" \
        "$mask_path"

    if [ "$mask_has_query" = true ]; then
        printf '%s' '?***'
    fi

    printf '\n'
}

broray_subscription_ip_is_public()
{
    public_ip="$(printf '%s' "$1" | tr 'A-F' 'a-f')"
    # curl --resolve needs a literal address, never nslookup's reverse name.
    # Reject ambiguous IPv4 forms and non-global/mapped IPv6 addresses.
    printf '%s\n' "$public_ip" | awk '
      /:/ {
        if ($0 ~ /[^0-9a-f:]/ || $0 ~ /:::/) exit 1
        text=$0; compressed=sub(/::/, "@", text)
        if (text ~ /::/ || $0 ~ /^:[^:]/ || $0 ~ /[^:]:$/) exit 1
        n=split($0,a,":"); groups=0
        for (i=1;i<=n;i++) {
          if (length(a[i])>4) exit 1
          if (a[i]!="") groups++
        }
        if ((compressed && groups>=8) || (!compressed && groups!=8)) exit 1
        if (length(a[1])!=4 || a[1]!~/^[23]/) exit 1
        sub(/^0+/, "", a[2])
        if (a[1]=="2001" && a[2]=="db8") exit 1
        exit 0
      }
      {
        if (split($0,a,".")!=4) exit 1
        for (i=1;i<=4;i++)
          if (a[i]!~/^[0-9]+$/ || a[i]>255 || (length(a[i])>1 && a[i]~/^0/)) exit 1
      }' || return 1
    case "$public_ip" in
        ''|0.*|10.*|127.*|169.254.*|192.168.*|224.*|225.*|226.*|227.*|228.*|229.*|230.*|231.*|232.*|233.*|234.*|235.*|236.*|237.*|238.*|239.*|24[0-9].*|25[0-9].*)
            return 1
            ;;
        100.*)
            public_second="$(printf '%s' "$public_ip" | cut -d. -f2)"
            case "$public_second" in
                ''|*[!0-9]*) ;;
                *) [ "$public_second" -ge 64 ] && [ "$public_second" -le 127 ] && return 1 ;;
            esac
            ;;
        172.*)
            public_second="$(printf '%s' "$public_ip" | cut -d. -f2)"
            case "$public_second" in
                ''|*[!0-9]*) ;;
                *) [ "$public_second" -ge 16 ] && [ "$public_second" -le 31 ] && return 1 ;;
            esac
            ;;
        198.18.*|198.19.*|192.0.0.*|192.0.2.*|198.51.100.*|203.0.113.*)
            return 1
            ;;
        ::|::1|[fF][cCdD]*|[fF][eE][89aAbB]*|[fF][fF]*|2001:db8:*|2001:DB8:*)
            return 1
            ;;
    esac
    return 0
}

broray_subscription_nslookup_addresses()
{
    # BusyBox prints "Address 1: IP reverse.name"; other variants use
    # "Address: IP#53". Only fields after Name belong to the answer.
    awk '
      /^Name:/ {answer=1; next}
      answer && /^Address [0-9]+:/ {print $3; next}
      answer && /^Address:/ {print $2}
    ' | sed 's/#.*//' | sort -u
}

broray_subscription_parse_url()
{
    parse_url="$1"
    BRORAY_SUB_URL_SCHEME=""
    BRORAY_SUB_URL_HOST=""
    BRORAY_SUB_URL_PORT=""
    BRORAY_SUB_URL_AUTHORITY=""

    case "$parse_url" in
        http://*) BRORAY_SUB_URL_SCHEME="http" ;;
        https://*) BRORAY_SUB_URL_SCHEME="https" ;;
        *)
            broray_subscription_set_error \
                "INVALID_URL" \
                "URL подписки должен использовать HTTP или HTTPS."
            return 1
            ;;
    esac
    if printf '%s' "$parse_url" | grep -q '[[:space:]]'; then
        broray_subscription_set_error \
            "INVALID_URL" \
            "URL подписки содержит пробельные символы."
        return 1
    fi
    case "$parse_url" in
        *\"*|*\'*|*\`*|*\\*)
            broray_subscription_set_error \
                "INVALID_URL" \
                "URL подписки содержит недопустимые символы."
            return 1
            ;;
    esac

    parse_rest="${parse_url#*://}"
    parse_authority="${parse_rest%%/*}"
    parse_authority="${parse_authority%%\?*}"
    parse_authority="${parse_authority%%#*}"
    [ -n "$parse_authority" ] || {
        broray_subscription_set_error \
            "INVALID_URL" \
            "В URL подписки отсутствует адрес сервера."
        return 1
    }
    case "$parse_authority" in
        *@*)
            broray_subscription_set_error \
                "INVALID_URL" \
                "Данные пользователя в URL подписки запрещены."
            return 1
            ;;
    esac

    BRORAY_SUB_URL_AUTHORITY="$parse_authority"
    case "$parse_authority" in
        \[*\]*)
            BRORAY_SUB_URL_HOST="$(printf '%s' "$parse_authority" | sed -n 's/^\[\([^]]*\)\].*/\1/p')"
            BRORAY_SUB_URL_PORT="$(printf '%s' "$parse_authority" | sed -n 's/^\[[^]]*\]:\([0-9][0-9]*\)$/\1/p')"
            ;;
        *:*)
            BRORAY_SUB_URL_HOST="${parse_authority%%:*}"
            BRORAY_SUB_URL_PORT="${parse_authority##*:}"
            ;;
        *)
            BRORAY_SUB_URL_HOST="$parse_authority"
            ;;
    esac
    [ -n "$BRORAY_SUB_URL_HOST" ] || {
        broray_subscription_set_error \
            "INVALID_URL" \
            "В URL подписки отсутствует имя хоста."
        return 1
    }
    BRORAY_SUB_URL_HOST="$(printf '%s' "$BRORAY_SUB_URL_HOST" | tr 'A-Z' 'a-z')"
    case "$BRORAY_SUB_URL_HOST" in
        localhost|*.localhost|*.local|*.lan|*.home|metadata|metadata.google.internal)
            broray_subscription_set_error \
                "DOWNLOAD_SECURITY" \
                "Локальные и служебные адреса запрещены."
            return 1
            ;;
    esac
    if [ -z "$BRORAY_SUB_URL_PORT" ]; then
        if [ "$BRORAY_SUB_URL_SCHEME" = "https" ]; then
            BRORAY_SUB_URL_PORT=443
        else
            BRORAY_SUB_URL_PORT=80
        fi
    fi
    case "$BRORAY_SUB_URL_PORT" in
        ''|*[!0-9]*)
            broray_subscription_set_error \
                "INVALID_URL" \
                "Порт в URL подписки имеет неправильный формат."
            return 1
            ;;
    esac
    [ "$BRORAY_SUB_URL_PORT" -ge 1 ] 2>/dev/null && \
    [ "$BRORAY_SUB_URL_PORT" -le 65535 ] 2>/dev/null || {
        broray_subscription_set_error \
            "INVALID_URL" \
            "Порт в URL подписки находится вне допустимого диапазона."
        return 1
    }
    return 0
}

broray_subscription_resolve_public_ip()
{
    resolve_host="$1"
    BRORAY_SUB_RESOLVED_IP=""

    case "$resolve_host" in
        *:*)
            if broray_subscription_ip_is_public "$resolve_host"; then
                BRORAY_SUB_RESOLVED_IP="$resolve_host"
                return 0
            fi
            broray_subscription_set_error \
                "DOWNLOAD_SECURITY" \
                "Локальный или служебный IP-адрес запрещён."
            return 1
            ;;
        *[!0-9.]* ) ;;
        *)
            if broray_subscription_ip_is_public "$resolve_host"; then
                BRORAY_SUB_RESOLVED_IP="$resolve_host"
                return 0
            fi
            broray_subscription_set_error \
                "DOWNLOAD_SECURITY" \
                "Локальный или служебный IP-адрес запрещён."
            return 1
            ;;
    esac

    resolve_file="$BRORAY_SUB_TMP/subscription-dns.$$.txt"
    : > "$resolve_file"
    if command -v getent >/dev/null 2>&1; then
        getent ahosts "$resolve_host" 2>/dev/null |
            awk '{print $1}' |
            sort -u > "$resolve_file"
    elif command -v nslookup >/dev/null 2>&1; then
        nslookup "$resolve_host" 2>/dev/null |
            broray_subscription_nslookup_addresses > "$resolve_file"
    else
        rm -f "$resolve_file"
        broray_subscription_set_error \
            "DOWNLOAD_SECURITY" \
            "На устройстве отсутствует безопасный DNS-резолвер."
        return 1
    fi

    while IFS= read -r resolve_ip; do
        [ -n "$resolve_ip" ] || continue
        if ! broray_subscription_ip_is_public "$resolve_ip"; then
            rm -f "$resolve_file"
            broray_subscription_set_error \
                "DOWNLOAD_SECURITY" \
                "Имя подписки разрешается в локальный или служебный адрес."
            return 1
        fi
        [ -n "$BRORAY_SUB_RESOLVED_IP" ] || \
            BRORAY_SUB_RESOLVED_IP="$resolve_ip"
    done < "$resolve_file"
    rm -f "$resolve_file"

    [ -n "$BRORAY_SUB_RESOLVED_IP" ] || {
        broray_subscription_set_error \
            "HTTP_ERROR" \
            "Не удалось определить IP-адрес сервера подписки."
        return 1
    }
    return 0
}

broray_subscription_validate_remote_url()
{
    validate_remote_url="$1"
    broray_subscription_parse_url "$validate_remote_url" || return 1
    broray_subscription_resolve_public_ip "$BRORAY_SUB_URL_HOST" || return 1
    return 0
}

broray_subscription_resolve_redirect()
{
    redirect_base="$1"
    redirect_location="$2"
    case "$redirect_location" in
        http://*|https://*)
            printf '%s\n' "$redirect_location"
            ;;
        //*)
            redirect_scheme="${redirect_base%%://*}"
            printf '%s:%s\n' "$redirect_scheme" "$redirect_location"
            ;;
        /*)
            broray_subscription_parse_url "$redirect_base" || return 1
            printf '%s://%s%s\n' \
                "$BRORAY_SUB_URL_SCHEME" \
                "$BRORAY_SUB_URL_AUTHORITY" \
                "$redirect_location"
            ;;
        *)
            redirect_prefix="${redirect_base%%\?*}"
            redirect_prefix="${redirect_prefix%/*}"
            printf '%s/%s\n' "$redirect_prefix" "$redirect_location"
            ;;
    esac
}

broray_subscription_header_true()
{
    header_true_file="$1"
    header_true_name="$2"
    awk -v wanted="$header_true_name" '
        {
            line=$0
            sub(/\r$/, "", line)
            name=line
            sub(/:.*/, "", name)
            if (tolower(name) != tolower(wanted)) next
            value=line
            sub(/^[^:]*:[[:space:]]*/, "", value)
            if (tolower(value) == "true") found=1
        }
        END { exit(found ? 0 : 1) }
    ' "$header_true_file"
}

broray_subscription_fetch()
{
    fetch_url="$1"
    fetch_output="$2"
    fetch_client_hwid="${3:-}"
    command -v curl >/dev/null 2>&1 || {
        broray_subscription_set_error \
            "HTTP_ERROR" \
            "Для обновления подписок требуется curl."
            return 1
    }
    if [ -n "$fetch_client_hwid" ] &&
       ! broray_subscription_client_hwid_valid "$fetch_client_hwid"; then
        broray_subscription_set_error \
            "CLIENT_ID_ERROR" \
            "Анонимный идентификатор клиента подписки повреждён."
        return 1
    fi

    fetch_current="$fetch_url"
    fetch_redirects=0
    fetch_headers="$BRORAY_SUB_TMP/subscription-headers.$$.txt"
    fetch_body="$BRORAY_SUB_TMP/subscription-body.$$.bin"
    fetch_error="$BRORAY_SUB_TMP/subscription-curl-error.$$.txt"
    rm -f "$fetch_headers" "$fetch_body" "$fetch_error" "$fetch_output"

    while :; do
        broray_subscription_validate_remote_url "$fetch_current" || {
            rm -f "$fetch_headers" "$fetch_body" "$fetch_error"
            return 1
        }
        fetch_resolve="$BRORAY_SUB_URL_HOST:$BRORAY_SUB_URL_PORT:$BRORAY_SUB_RESOLVED_IP"
        case "$BRORAY_SUB_RESOLVED_IP" in
            *:*)
                fetch_resolve="$BRORAY_SUB_URL_HOST:$BRORAY_SUB_URL_PORT:[$BRORAY_SUB_RESOLVED_IP]"
                ;;
        esac
        : > "$fetch_headers"
        : > "$fetch_error"
        set -- \
            curl \
            --silent \
            --show-error \
            --noproxy '*' \
            --proto '=http,https' \
            --connect-timeout 10 \
            --max-time 35 \
            --max-redirs 0 \
            --user-agent "$BRORAY_SUB_USER_AGENT" \
            --resolve "$fetch_resolve"
        if [ -n "$fetch_client_hwid" ]; then
            set -- "$@" --header "x-hwid: $fetch_client_hwid"
        fi
        if curl --help all 2>/dev/null | grep -q -- '--max-filesize'; then
            set -- "$@" --max-filesize "$BRORAY_SUB_MAX_BYTES"
        fi
        fetch_status="$(
            "$@" \
                --dump-header "$fetch_headers" \
                --output "$fetch_body" \
                --write-out '%{http_code}' \
                "$fetch_current" \
                2> "$fetch_error"
        )"
        fetch_curl_code="$?"
        if [ "$fetch_curl_code" -ne 0 ]; then
            case "$fetch_curl_code" in
                28)
                    broray_subscription_set_error \
                        "DOWNLOAD_TIMEOUT" \
                        "Сервер подписки не ответил вовремя."
                    ;;
                63)
                    broray_subscription_set_error \
                        "CONTENT_TOO_LARGE" \
                        "Ответ подписки превышает допустимый размер."
                    ;;
                *)
                    broray_subscription_set_error \
                        "HTTP_ERROR" \
                        "Не удалось скачать подписку."
                    ;;
            esac
            rm -f "$fetch_headers" "$fetch_body" "$fetch_error"
            return 1
        fi

        fetch_bytes="$(wc -c < "$fetch_body" 2>/dev/null | tr -d ' ')"
        case "$fetch_bytes" in
            ''|*[!0-9]*) fetch_bytes=0 ;;
        esac
        if [ "$fetch_bytes" -gt "$BRORAY_SUB_MAX_BYTES" ]; then
            broray_subscription_set_error \
                "CONTENT_TOO_LARGE" \
                "Ответ подписки превышает допустимый размер."
            rm -f "$fetch_headers" "$fetch_body" "$fetch_error"
            return 1
        fi

        case "$fetch_status" in
            2??)
                if broray_subscription_header_true \
                    "$fetch_headers" "x-hwid-max-devices-reached"; then
                    broray_subscription_set_error \
                        "SUBSCRIPTION_DEVICE_LIMIT_REACHED" \
                        "Провайдер отклонил подписку: достигнут лимит зарегистрированных устройств."
                    rm -f "$fetch_headers" "$fetch_body" "$fetch_error"
                    return 1
                fi
                if broray_subscription_header_true \
                    "$fetch_headers" "x-hwid-not-supported"; then
                    broray_subscription_set_error \
                        "SUBSCRIPTION_DEVICE_ID_REJECTED" \
                        "Провайдер не принял анонимный идентификатор клиента подписки."
                    rm -f "$fetch_headers" "$fetch_body" "$fetch_error"
                    return 1
                fi
                mv "$fetch_body" "$fetch_output" || {
                    broray_subscription_set_error \
                        "INTERNAL_ERROR" \
                        "Не удалось сохранить загруженную подписку."
                    rm -f "$fetch_headers" "$fetch_body" "$fetch_error"
                    return 1
                }
                BRORAY_SUB_FETCH_CONTENT_TYPE="$(
                    awk 'BEGIN{IGNORECASE=1} /^Content-Type:/ {line=$0} END{sub(/\r$/, "", line); sub(/^[^:]*:[[:space:]]*/, "", line); print line}' \
                        "$fetch_headers"
                )"
                BRORAY_SUB_FETCH_BYTES="$fetch_bytes"
                BRORAY_SUB_FETCH_FINAL_URL="$fetch_current"
                rm -f "$fetch_headers" "$fetch_error"
                return 0
                ;;
            301|302|303|307|308)
                fetch_redirects=$((fetch_redirects + 1))
                if [ "$fetch_redirects" -gt 3 ]; then
                    broray_subscription_set_error \
                        "HTTP_ERROR" \
                        "Сервер подписки выполнил слишком много перенаправлений."
                    rm -f "$fetch_headers" "$fetch_body" "$fetch_error"
                    return 1
                fi
                fetch_location="$(
                    awk 'BEGIN{IGNORECASE=1} /^Location:/ {line=$0} END{sub(/\r$/, "", line); sub(/^[^:]*:[[:space:]]*/, "", line); print line}' \
                        "$fetch_headers"
                )"
                [ -n "$fetch_location" ] || {
                    broray_subscription_set_error \
                        "HTTP_ERROR" \
                        "Сервер подписки вернул перенаправление без адреса."
                    rm -f "$fetch_headers" "$fetch_body" "$fetch_error"
                    return 1
                }
                fetch_current="$(
                    broray_subscription_resolve_redirect \
                        "$fetch_current" "$fetch_location"
                )" || {
                    rm -f "$fetch_headers" "$fetch_body" "$fetch_error"
                    return 1
                }
                rm -f "$fetch_body"
                ;;
            *)
                broray_subscription_set_error \
                    "HTTP_ERROR" \
                    "Сервер подписки вернул HTTP $fetch_status."
                rm -f "$fetch_headers" "$fetch_body" "$fetch_error"
                return 1
                ;;
        esac
    done
}

broray_subscription_decode_base64()
{
    decode_input="$1"
    decode_output="$2"
    decode_compact="$BRORAY_SUB_TMP/subscription-base64.$$.txt"
    tr -d '\r\n\t ' < "$decode_input" |
        tr '_-' '/+' > "$decode_compact"
    decode_length="$(wc -c < "$decode_compact" | tr -d ' ')"
    case "$decode_length" in
        ''|*[!0-9]*) decode_length=0 ;;
    esac
    decode_mod=$((decode_length % 4))
    case "$decode_mod" in
        2) printf '==' >> "$decode_compact" ;;
        3) printf '=' >> "$decode_compact" ;;
    esac
    if base64 -d "$decode_compact" > "$decode_output" 2>/dev/null; then
        rm -f "$decode_compact"
        return 0
    fi
    rm -f "$decode_compact" "$decode_output"
    return 1
}

broray_subscription_extract_nodes()
{
    extract_input="$1"
    extract_output="$2"
    extract_normalized="$BRORAY_SUB_TMP/subscription-normalized.$$.txt"
    extract_decoded="$BRORAY_SUB_TMP/subscription-decoded.$$.txt"

    tr -d '\r' < "$extract_input" > "$extract_normalized"
    sed -i '1s/^\xef\xbb\xbf//' "$extract_normalized" 2>/dev/null || true

    if grep -Eiq '<!doctype[[:space:]]+html|<html([[:space:]>])' \
        "$extract_normalized"; then
        rm -f "$extract_normalized" "$extract_decoded"
        broray_subscription_set_error \
            "UNSUPPORTED_CONTENT" \
            "Сервер вернул HTML вместо подписки."
        return 1
    fi

    if grep -Eq '^(vless|vmess|trojan|ss|hysteria2|hy2|tuic|socks|socks5|http|https)://' \
        "$extract_normalized"; then
        cp "$extract_normalized" "$extract_output"
    elif broray_subscription_decode_base64 \
        "$extract_normalized" "$extract_decoded" && \
        grep -Eq '^(vless|vmess|trojan|ss|hysteria2|hy2|tuic|socks|socks5|http|https)://' \
            "$extract_decoded"; then
        tr -d '\r' < "$extract_decoded" > "$extract_output"
    else
        rm -f "$extract_normalized" "$extract_decoded" "$extract_output"
        broray_subscription_set_error \
            "PARSE_ERROR" \
            "Содержимое подписки не удалось распознать."
        return 1
    fi

    sed -i \
        -e '/^[[:space:]]*$/d' \
        -e '/^[[:space:]]*#/d' \
        "$extract_output" 2>/dev/null || true
    # Count logical records, including a final URI without a trailing LF.
    extract_count="$(awk 'END { print NR + 0 }' "$extract_output")"
    case "$extract_count" in
        ''|*[!0-9]*) extract_count=0 ;;
    esac
    if [ "$extract_count" -eq 0 ]; then
        rm -f "$extract_normalized" "$extract_decoded" "$extract_output"
        broray_subscription_set_error \
            "NO_VALID_NODES" \
            "Подписка не содержит узлов."
        return 1
    fi
    if [ "$extract_count" -gt "$BRORAY_SUB_MAX_NODES" ]; then
        rm -f "$extract_normalized" "$extract_decoded" "$extract_output"
        broray_subscription_set_error \
            "CONTENT_TOO_LARGE" \
            "Подписка содержит слишком много узлов."
        return 1
    fi
    BRORAY_SUB_RECEIVED="$extract_count"
    rm -f "$extract_normalized" "$extract_decoded"
    return 0
}

broray_subscription_provider_denial_marker()
{
    denial_marker_file="$1"
    jq -e '(.address == "0.0.0.0") and (.port == 1)' \
        "$denial_marker_file" >/dev/null 2>&1 || return 1

    denial_marker_name="$(jq -r '.name // ""' "$denial_marker_file" 2>/dev/null)"
    case "$denial_marker_name" in
        *"Приложение не поддерж"*) return 0 ;;
    esac
    denial_marker_ascii="$(
        printf '%s' "$denial_marker_name" |
            tr 'A-Z' 'a-z' |
            tr -d ' _-'
    )"
    case "$denial_marker_ascii" in
        *appnotsupported*|*applicationnotsupported*|*enablehwid*) return 0 ;;
    esac
    return 1
}

broray_subscription_stage_nodes()
{
    stage_subscription_id="$1"
    stage_nodes_file="$2"
    stage_output_dir="$3"
    stage_enabled="$4"
    stage_raw_dir="$BRORAY_SUB_TMP/subscription-raw.$$.d"
    stage_parse_tmp="$BRORAY_SUB_TMP/subscription-parse.$$.d"
    stage_warnings="$BRORAY_SUB_TMP/subscription-warnings.$$.txt"
    rm -rf "$stage_raw_dir" "$stage_parse_tmp" "$stage_output_dir"
    mkdir -p "$stage_raw_dir" "$stage_parse_tmp" "$stage_output_dir"
    : > "$stage_warnings"

    BRORAY_SUB_PARSED=0
    BRORAY_SUB_ACCEPTED=0
    BRORAY_SUB_REJECTED=0
    BRORAY_SUB_DENIAL_MARKERS=0
    stage_index=0
    while IFS= read -r stage_uri || [ -n "$stage_uri" ]; do
        stage_index=$((stage_index + 1))
        stage_error="$BRORAY_SUB_TMP/subscription-node-error.$$.txt"
        stage_output="$BRORAY_SUB_TMP/subscription-node-output.$$.txt"
        : > "$stage_error"
        : > "$stage_output"
        if (
            . "$BRORAY_BASE/lib/server-import.sh"
            BRORAY_SERVERS="$stage_raw_dir"
            BRORAY_TMP="$stage_parse_tmp"
            export BRORAY_SERVERS BRORAY_TMP
            if ! command -v broray_server_import_dispatch >/dev/null 2>&1; then
                printf '%s\n' \
                    'Серверный импортёр не предоставляет безопасный dispatch.' \
                    >&2
                exit 1
            fi
            broray_server_import_dispatch \
                "$stage_uri" \
                subscription \
                "$stage_subscription_id" \
                "$stage_index"
        ) > "$stage_output" 2> "$stage_error"; then
            BRORAY_SUB_PARSED=$((BRORAY_SUB_PARSED + 1))
            stage_raw_id="$(printf 'subscription-%s-%04d' "$stage_subscription_id" "$stage_index")"
            stage_raw_file="$stage_raw_dir/$stage_raw_id.json"
            if [ ! -f "$stage_raw_file" ]; then
                BRORAY_SUB_REJECTED=$((BRORAY_SUB_REJECTED + 1))
                printf 'Узел %s: импортёр не создал сервер.\n' "$stage_index" \
                    >> "$stage_warnings"
            elif broray_subscription_provider_denial_marker "$stage_raw_file"; then
                BRORAY_SUB_REJECTED=$((BRORAY_SUB_REJECTED + 1))
                BRORAY_SUB_DENIAL_MARKERS=$((BRORAY_SUB_DENIAL_MARKERS + 1))
                printf 'Узел %s: провайдер вернул служебный отказ вместо сервера.\n' \
                    "$stage_index" >> "$stage_warnings"
            else
                stage_key="$(broray_server_subscription_import_key "$stage_raw_file")"
                if [ -z "$stage_key" ]; then
                    BRORAY_SUB_REJECTED=$((BRORAY_SUB_REJECTED + 1))
                    printf 'Узел %s: не удалось вычислить importKey.\n' "$stage_index" \
                        >> "$stage_warnings"
                else
                    stage_stable_id="subscription-${stage_subscription_id}-$(printf '%s' "$stage_key" | cut -c 1-16)"
                    stage_file="$stage_output_dir/$stage_stable_id.json"
                    if [ -f "$stage_file" ]; then
                        BRORAY_SUB_REJECTED=$((BRORAY_SUB_REJECTED + 1))
                        printf 'Узел %s: дубликат уже полученного сервера.\n' "$stage_index" \
                            >> "$stage_warnings"
                    else
                        stage_now="$(broray_subscription_now_iso)"
                        if jq \
                            --arg id "$stage_stable_id" \
                            --arg subscriptionId "$stage_subscription_id" \
                            --arg importKey "$stage_key" \
                            --argjson nodeIndex "$stage_index" \
                            --arg updatedAt "$stage_now" \
                            --argjson enabled "$stage_enabled" '
                            .id = $id |
                            .source = {
                                type: "subscription",
                                subscriptionId: $subscriptionId,
                                importKey: $importKey,
                                nodeIndex: $nodeIndex,
                                enabled: $enabled,
                                updatedAt: $updatedAt
                            }
                        ' "$stage_raw_file" > "$stage_file" && \
                        (
                            broray_server_validate "$stage_file" >/dev/null 2>&1
                        ); then
                            chmod 600 "$stage_file" || true
                            BRORAY_SUB_ACCEPTED=$((BRORAY_SUB_ACCEPTED + 1))
                        else
                            rm -f "$stage_file"
                            BRORAY_SUB_REJECTED=$((BRORAY_SUB_REJECTED + 1))
                            printf 'Узел %s: нормализованный сервер не прошёл проверку.\n' "$stage_index" \
                                >> "$stage_warnings"
                        fi
                    fi
                fi
            fi
        else
            BRORAY_SUB_REJECTED=$((BRORAY_SUB_REJECTED + 1))
            stage_reason="$(tail -n 1 "$stage_error" | cut -c 1-180)"
            [ -n "$stage_reason" ] || stage_reason="формат узла не поддерживается"
            printf 'Узел %s: %s\n' "$stage_index" "$stage_reason" \
                >> "$stage_warnings"
        fi
        rm -f "$stage_error" "$stage_output"
        stage_uri=""
    done < "$stage_nodes_file"

    rm -rf "$stage_raw_dir" "$stage_parse_tmp"
    if [ "$BRORAY_SUB_ACCEPTED" -eq 0 ]; then
        BRORAY_SUB_WARNINGS_FILE="$stage_warnings"
        if [ "$BRORAY_SUB_DENIAL_MARKERS" -gt 0 ]; then
            broray_subscription_set_error \
                "SUBSCRIPTION_ACCESS_DENIED" \
                "Провайдер не выдал серверы: приложение или идентификатор устройства не поддержан."
        else
            broray_subscription_set_error \
                "NO_VALID_NODES" \
                "Подписка не содержит ни одного поддерживаемого и валидного узла."
        fi
        return 1
    fi
    BRORAY_SUB_WARNINGS_FILE="$stage_warnings"
    return 0
}

broray_subscription_lock_path()
{
    lock_id="$1"
    printf '%s/%s.lock\n' "$BRORAY_SUB_RUN" "$lock_id"
}

broray_subscription_acquire_lock()
{
    acquire_id="$1"
    broray_job_require_owner || return $?
    acquire_lock="$(broray_subscription_lock_path "$acquire_id")"
    if [ -e "$acquire_lock" ] || [ -L "$acquire_lock" ]; then
        broray_subscription_set_error \
            "UPDATE_ALREADY_RUNNING" \
            "Состояние прежней операции подписки требует восстановления."
        return 1
    fi
    # The managed global fence already excludes all subscription writers.
    # Never recreate or reclaim a second PID-only ownership namespace.
    broray_job_checkpoint committing
}

broray_subscription_release_lock()
{
    # Ownership lasts until the enclosing job finishes.
    broray_subscription_cleanup_preparation
}

broray_subscription_schedule_values()
{
    schedule_enabled="$1"
    schedule_auto="$2"
    schedule_interval="$3"
    schedule_base_epoch="${4:-$(broray_subscription_now_epoch)}"
    BRORAY_SUB_NEXT_EPOCH=0
    BRORAY_SUB_NEXT_AT=""
    if [ "$schedule_enabled" = "true" ] && \
       [ "$schedule_auto" = "true" ]; then
        BRORAY_SUB_NEXT_EPOCH=$((schedule_base_epoch + schedule_interval * 60))
        BRORAY_SUB_NEXT_AT="$(broray_subscription_iso_from_epoch "$BRORAY_SUB_NEXT_EPOCH")"
    fi
}

broray_subscription_effective_status()
{
    local effective_id effective_state
    BRORAY_SUB_EFFECTIVE_STATUS="$(jq -r '.lastUpdateStatus // "never"' "$1")"
    BRORAY_SUB_EFFECTIVE_ERROR=""
    BRORAY_SUB_EFFECTIVE_CODE=""
    BRORAY_SUB_EFFECTIVE_FINISHED=""
    [ "$BRORAY_SUB_EFFECTIVE_STATUS" = running ] || return 0
    effective_id="$(jq -r '.backgroundOperationId // empty' "$1")"
    case "$effective_id" in op-*) ;; *) return 0 ;; esac
    case "$effective_id" in *[!A-Za-z0-9._-]*) return 0 ;; esac
    [ "${#effective_id}" -le 96 ] || return 0
    effective_state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/operations/$effective_id/state.json"
    [ -f "$effective_state" ] && [ ! -L "$effective_state" ] || return 0
    if jq -e --arg id "$effective_id" '.kind=="background" and .operationId==$id and .running==false and
      (.state=="aborted" or .state=="failed" or .state=="recovered" or .state=="completed")' "$effective_state" >/dev/null; then
        BRORAY_SUB_EFFECTIVE_STATUS=error
        BRORAY_SUB_EFFECTIVE_ERROR="Обновление прервано. Сохранены последние доступные данные подписки."
        BRORAY_SUB_EFFECTIVE_CODE=OPERATION_INTERRUPTED
        BRORAY_SUB_EFFECTIVE_FINISHED="$(jq -r '.finishedAt // empty' "$effective_state")"
        if jq -e '.state=="aborted" and .errorCode=="CANCELLED"' "$effective_state" >/dev/null; then
            BRORAY_SUB_EFFECTIVE_CODE=CANCELLED
            BRORAY_SUB_EFFECTIVE_ERROR="Обновление остановлено пользователем. Сохранены последние доступные данные подписки."
        fi
    fi
}

broray_subscription_public_file()
{
    public_file="$1"
    public_include_url="${2:-false}"
    public_id="$(jq -r '.id' "$public_file")"
    public_count="$(broray_server_subscription_count "$public_id" 2>/dev/null || printf '0')"
    public_display_url="$(broray_subscription_mask_url "$(jq -r '.url' "$public_file")")"
    broray_subscription_effective_status "$public_file"
    jq \
        --arg effectiveStatus "$BRORAY_SUB_EFFECTIVE_STATUS" --arg effectiveError "$BRORAY_SUB_EFFECTIVE_ERROR" \
        --arg effectiveCode "$BRORAY_SUB_EFFECTIVE_CODE" --arg effectiveFinished "$BRORAY_SUB_EFFECTIVE_FINISHED" \
        --arg displayUrl "$public_display_url" \
        --argjson serversCount "$public_count" \
        --argjson includeUrl "$public_include_url" '
        .lastUpdateStatus=$effectiveStatus |
        (if $effectiveError!="" then
            .lastError=$effectiveError |
            .lastUpdateResult={errorCode:$effectiveCode,durationMs:null,warnings:[]} |
            (if $effectiveFinished!="" then .lastUpdatedAt=$effectiveFinished else . end)
         else . end) |
        . + {
            displayUrl: $displayUrl,
            serversCount: $serversCount
        } |
        if $includeUrl then . else del(.url) end |
        del(.clientHwid, .lastUpdatedEpoch, .nextUpdateEpoch)
    ' "$public_file"
}

broray_subscription_recover_stale()
{
    broray_job_require_owner || return $?
    broray_subscription_prepare_dirs
    for recover_file in "$BRORAY_SUB_DIR"/*.json; do
        [ -f "$recover_file" ] && [ ! -L "$recover_file" ] || continue
        recover_status="$(jq -r '.lastUpdateStatus // "never"' "$recover_file" 2>/dev/null)"
        [ "$recover_status" = "running" ] || continue
        recover_id="$(jq -r '.id // empty' "$recover_file")"
        broray_subscription_validate_id "$recover_id" || continue
        recover_lock="$(broray_subscription_lock_path "$recover_id")"
        [ ! -e "$recover_lock" ] && [ ! -L "$recover_lock" ] || continue
        recover_operation="$(jq -r '.backgroundOperationId // empty' "$recover_file")"
        case "$recover_operation" in op-*) ;; *) continue ;; esac
        case "$recover_operation" in *[!A-Za-z0-9._-]*) continue ;; esac
        [ "${#recover_operation}" -le 96 ] || continue
        recover_state="${BRORAY_STATE_ROOT:-/opt/var/lib/broray}/operations/$recover_operation/state.json"
        [ -f "$recover_state" ] && [ ! -L "$recover_state" ] || continue
        jq -e --arg id "$recover_operation" '.kind=="background" and .operationId==$id and .running==false and
          (.state=="completed" or .state=="failed" or .state=="aborted" or .state=="recovered")' "$recover_state" >/dev/null || continue
        recover_now_epoch="$(broray_subscription_now_epoch)"
        recover_now="$(broray_subscription_now_iso)"
        broray_subscription_effective_status "$recover_file"
        recover_temp="$BRORAY_SUB_TMP/subscription-recover.$$.json"
        recover_enabled="$(jq -r '.enabled' "$recover_file")"
        recover_auto="$(jq -r '.autoUpdateEnabled' "$recover_file")"
        recover_interval="$(jq -r '.updateIntervalMinutes' "$recover_file")"
        broray_subscription_schedule_values \
            "$recover_enabled" "$recover_auto" "$recover_interval" "$recover_now_epoch"
        jq \
            --arg now "$recover_now" \
            --argjson epoch "$recover_now_epoch" \
            --arg error "$BRORAY_SUB_EFFECTIVE_ERROR" \
            --arg code "$BRORAY_SUB_EFFECTIVE_CODE" \
            --arg finished "$BRORAY_SUB_EFFECTIVE_FINISHED" \
            --arg nextAt "$BRORAY_SUB_NEXT_AT" \
            --argjson nextEpoch "$BRORAY_SUB_NEXT_EPOCH" '
            .lastUpdateStatus = "error" |
            .lastUpdatedAt = (if $finished!="" then $finished else $now end) |
            .lastUpdatedEpoch = $epoch |
            .lastError = $error |
            .lastUpdateResult = {errorCode:$code,durationMs:null,warnings:[]} |
            .nextUpdateAt = (if $nextAt == "" then null else $nextAt end) |
            .nextUpdateEpoch = (if $nextEpoch == 0 then null else $nextEpoch end) |
            .updatedAt = $now
        ' "$recover_file" > "$recover_temp" && \
            broray_subscription_write_json "$recover_file" "$recover_temp"
        rm -f "$recover_temp"
    done
}

broray_subscription_list()
{
    broray_subscription_prepare_dirs
    list_file="$BRORAY_SUB_TMP/subscriptions-list.$$.json"
    printf '%s\n' '[]' > "$list_file"
    for subscription_file in "$BRORAY_SUB_DIR"/*.json; do
        [ -f "$subscription_file" ] || continue
        broray_subscription_validate_file "$subscription_file" || continue
        item_file="$BRORAY_SUB_TMP/subscriptions-item.$$.json"
        broray_subscription_public_file "$subscription_file" false > "$item_file" || continue
        jq --slurpfile item "$item_file" '. + [$item[0]]' \
            "$list_file" > "$list_file.new" && mv "$list_file.new" "$list_file"
        rm -f "$item_file"
    done
    jq 'sort_by(.createdAt) | reverse' "$list_file"
    rm -f "$list_file"
}

broray_subscription_get()
{
    get_id="$1"
    broray_subscription_validate_id "$get_id" || {
        broray_subscription_emit_error
        return 1
    }
    get_path="$(broray_subscription_path "$get_id")" || return 1
    [ -f "$get_path" ] || {
        broray_subscription_set_error \
            "SUBSCRIPTION_NOT_FOUND" \
            "Подписка не найдена."
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_public_file "$get_path" true
}

broray_subscription_validate_body()
{
    body_json_file="$1"
    jq -e 'type == "object"' "$body_json_file" >/dev/null 2>&1 || {
        broray_subscription_set_error \
            "INVALID_REQUEST" \
            "Тело запроса должно быть JSON-объектом."
        return 1
    }
    return 0
}

broray_subscription_create()
{
    create_body="$1"
    broray_subscription_prepare_dirs
    broray_subscription_validate_body "$create_body" || {
        broray_subscription_emit_error
        return 1
    }
    create_name="$(jq -r '.name // empty' "$create_body")"
    create_url="$(jq -r '.url // empty' "$create_body")"
    create_enabled="$(jq -r 'if has("enabled") then .enabled else true end' "$create_body")"
    create_auto="$(jq -r 'if has("autoUpdateEnabled") then .autoUpdateEnabled else true end' "$create_body")"
    create_interval="$(jq -r '.updateIntervalMinutes // 360' "$create_body")"
    create_immediate="$(jq -r 'if has("updateImmediately") then .updateImmediately else false end' "$create_body")"

    [ -n "$create_name" ] && [ "${#create_name}" -le 128 ] || {
        broray_subscription_set_error \
            "INVALID_NAME" \
            "Название подписки должно содержать от 1 до 128 символов."
        broray_subscription_emit_error
        return 1
    }
    [ -n "$create_url" ] && [ "${#create_url}" -le 4096 ] || {
        broray_subscription_set_error \
            "INVALID_URL" \
            "URL подписки не указан или слишком длинный."
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_parse_url "$create_url" || {
        broray_subscription_emit_error
        return 1
    }
    case "$create_enabled:$create_auto:$create_immediate" in
        true:true:true|true:true:false|true:false:true|true:false:false|false:true:true|false:true:false|false:false:true|false:false:false) ;;
        *)
            broray_subscription_set_error \
                "INVALID_REQUEST" \
                "Поля enabled, autoUpdateEnabled и updateImmediately должны быть логическими."
            broray_subscription_emit_error
            return 1
            ;;
    esac
    case "$create_interval" in
        ''|*[!0-9]*)
            broray_subscription_set_error \
                "INVALID_INTERVAL" \
                "Интервал обновления должен быть целым числом минут."
            broray_subscription_emit_error
            return 1
            ;;
    esac
    if [ "$create_interval" -lt "$BRORAY_SUB_MIN_INTERVAL" ] || \
       [ "$create_interval" -gt "$BRORAY_SUB_MAX_INTERVAL" ]; then
        broray_subscription_set_error \
            "INVALID_INTERVAL" \
            "Интервал обновления должен быть от $BRORAY_SUB_MIN_INTERVAL до $BRORAY_SUB_MAX_INTERVAL минут."
        broray_subscription_emit_error
        return 1
    fi

    create_id="$(broray_subscription_generate_id)"
    create_client_hwid="$(broray_subscription_generate_client_hwid)"
    broray_subscription_client_hwid_valid "$create_client_hwid" || {
        broray_subscription_set_error \
            "CLIENT_ID_ERROR" \
            "Не удалось создать анонимный идентификатор клиента подписки."
        broray_subscription_emit_error
        return 1
    }
    create_path="$(broray_subscription_path "$create_id")"
    create_now_epoch="$(broray_subscription_now_epoch)"
    create_now="$(broray_subscription_now_iso)"
    broray_subscription_schedule_values \
        "$create_enabled" "$create_auto" "$create_interval" "$create_now_epoch"
    create_temp="$BRORAY_SUB_TMP/subscription-create.$$.json"
    jq -n \
        --arg id "$create_id" \
        --arg name "$create_name" \
        --arg url "$create_url" \
        --arg clientHwid "$create_client_hwid" \
        --argjson enabled "$create_enabled" \
        --argjson autoUpdateEnabled "$create_auto" \
        --argjson updateIntervalMinutes "$create_interval" \
        --arg nextUpdateAt "$BRORAY_SUB_NEXT_AT" \
        --argjson nextUpdateEpoch "$BRORAY_SUB_NEXT_EPOCH" \
        --arg createdAt "$create_now" '
        {
            schemaVersion: 1,
            id: $id,
            name: $name,
            url: $url,
            clientHwid: $clientHwid,
            enabled: $enabled,
            autoUpdateEnabled: $autoUpdateEnabled,
            updateIntervalMinutes: $updateIntervalMinutes,
            lastUpdateStatus: "never",
            lastUpdatedAt: null,
            lastUpdatedEpoch: null,
            nextUpdateAt: (if $nextUpdateAt == "" then null else $nextUpdateAt end),
            nextUpdateEpoch: (if $nextUpdateEpoch == 0 then null else $nextUpdateEpoch end),
            lastError: null,
            serversReceived: 0,
            lastUpdateResult: null,
            createdAt: $createdAt,
            updatedAt: $createdAt
        }
    ' > "$create_temp" || {
        rm -f "$create_temp"
        broray_subscription_set_error \
            "PERSISTENCE_ERROR" \
            "Не удалось создать данные подписки."
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_write_json "$create_path" "$create_temp" || {
        rm -f "$create_temp"
        broray_subscription_emit_error
        return 1
    }
    rm -f "$create_temp"
    broray_subscription_log \
        "subscription=$create_id action=create url=$(broray_subscription_mask_url "$create_url")"

    if [ "$create_immediate" = "true" ]; then
        create_refresh_rc=0
        broray_subscription_update "$create_id" initial >/dev/null 2>&1 || create_refresh_rc=$?
        case "$create_refresh_rc" in 130|75) return "$create_refresh_rc" ;; esac
    fi
    broray_subscription_get "$create_id"
}

broray_subscription_update_settings()
{
    settings_id="$1"
    settings_body="$2"
    broray_subscription_validate_id "$settings_id" || {
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_validate_body "$settings_body" || {
        broray_subscription_emit_error
        return 1
    }
    settings_path="$(broray_subscription_path "$settings_id")"
    [ -f "$settings_path" ] || {
        broray_subscription_set_error \
            "SUBSCRIPTION_NOT_FOUND" \
            "Подписка не найдена."
        broray_subscription_emit_error
        return 1
    }

    settings_name="$(jq -r --arg old "$(jq -r '.name' "$settings_path")" 'if has("name") then .name else $old end' "$settings_body")"
    settings_url="$(jq -r --arg old "$(jq -r '.url' "$settings_path")" 'if has("url") then .url else $old end' "$settings_body")"
    settings_enabled="$(jq -r --argjson old "$(jq '.enabled' "$settings_path")" 'if has("enabled") then .enabled else $old end' "$settings_body")"
    settings_auto="$(jq -r --argjson old "$(jq '.autoUpdateEnabled' "$settings_path")" 'if has("autoUpdateEnabled") then .autoUpdateEnabled else $old end' "$settings_body")"
    settings_interval="$(jq -r --argjson old "$(jq '.updateIntervalMinutes' "$settings_path")" 'if has("updateIntervalMinutes") then .updateIntervalMinutes else $old end' "$settings_body")"
    settings_old_enabled="$(jq -r '.enabled' "$settings_path")"

    [ -n "$settings_name" ] && [ "${#settings_name}" -le 128 ] || {
        broray_subscription_set_error \
            "INVALID_NAME" \
            "Название подписки должно содержать от 1 до 128 символов."
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_parse_url "$settings_url" || {
        broray_subscription_emit_error
        return 1
    }
    case "$settings_enabled:$settings_auto" in
        true:true|true:false|false:true|false:false) ;;
        *)
            broray_subscription_set_error \
                "INVALID_REQUEST" \
                "Поля enabled и autoUpdateEnabled должны быть логическими."
            broray_subscription_emit_error
            return 1
            ;;
    esac
    case "$settings_interval" in
        ''|*[!0-9]*)
            broray_subscription_set_error \
                "INVALID_INTERVAL" \
                "Интервал обновления должен быть целым числом минут."
            broray_subscription_emit_error
            return 1
            ;;
    esac
    if [ "$settings_interval" -lt "$BRORAY_SUB_MIN_INTERVAL" ] || \
       [ "$settings_interval" -gt "$BRORAY_SUB_MAX_INTERVAL" ]; then
        broray_subscription_set_error \
            "INVALID_INTERVAL" \
            "Интервал обновления должен быть от $BRORAY_SUB_MIN_INTERVAL до $BRORAY_SUB_MAX_INTERVAL минут."
        broray_subscription_emit_error
        return 1
    fi

    if [ "$settings_enabled" != "$settings_old_enabled" ]; then
        settings_server_result="$BRORAY_SUB_TMP/subscription-enable.$$.json"
        settings_server_error="$BRORAY_SUB_TMP/subscription-enable.$$.err"
        if ! broray_server_subscription_set_enabled \
            "$settings_id" "$settings_enabled" \
            > "$settings_server_result" 2> "$settings_server_error"; then
            settings_error_line="$(tail -n 1 "$settings_server_error")"
            settings_error_code="$(printf '%s' "$settings_error_line" | cut -d: -f2)"
            settings_error_message="$(printf '%s' "$settings_error_line" | cut -d: -f3-)"
            rm -f "$settings_server_result" "$settings_server_error"
            broray_subscription_set_error \
                "${settings_error_code:-SERVER_SOURCE_STATE_FAILED}" \
                "${settings_error_message:-Не удалось изменить состояние серверов подписки.}"
            broray_subscription_emit_error
            return 1
        fi
        rm -f "$settings_server_result" "$settings_server_error"
    fi

    settings_now_epoch="$(broray_subscription_now_epoch)"
    settings_now="$(broray_subscription_now_iso)"
    broray_subscription_schedule_values \
        "$settings_enabled" "$settings_auto" "$settings_interval" "$settings_now_epoch"
    settings_temp="$BRORAY_SUB_TMP/subscription-settings.$$.json"
    jq \
        --arg name "$settings_name" \
        --arg url "$settings_url" \
        --argjson enabled "$settings_enabled" \
        --argjson autoUpdateEnabled "$settings_auto" \
        --argjson updateIntervalMinutes "$settings_interval" \
        --arg nextUpdateAt "$BRORAY_SUB_NEXT_AT" \
        --argjson nextUpdateEpoch "$BRORAY_SUB_NEXT_EPOCH" \
        --arg updatedAt "$settings_now" '
        .name = $name |
        .url = $url |
        .enabled = $enabled |
        .autoUpdateEnabled = $autoUpdateEnabled |
        .updateIntervalMinutes = $updateIntervalMinutes |
        .nextUpdateAt = (if $nextUpdateAt == "" then null else $nextUpdateAt end) |
        .nextUpdateEpoch = (if $nextUpdateEpoch == 0 then null else $nextUpdateEpoch end) |
        .updatedAt = $updatedAt
    ' "$settings_path" > "$settings_temp" || {
        rm -f "$settings_temp"
        broray_subscription_set_error \
            "PERSISTENCE_ERROR" \
            "Не удалось подготовить изменения подписки."
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_write_json "$settings_path" "$settings_temp" || {
        rm -f "$settings_temp"
        broray_subscription_emit_error
        return 1
    }
    rm -f "$settings_temp"
    broray_subscription_log \
        "subscription=$settings_id action=update-settings url=$(broray_subscription_mask_url "$settings_url") enabled=$settings_enabled auto=$settings_auto interval=$settings_interval"
    broray_subscription_get "$settings_id"
}

broray_subscription_save_failed_update()
{
    failed_id="$1"
    failed_code="$2"
    failed_message="$3"
    failed_trigger="$4"
    failed_started_epoch="$5"
    failed_file="$(broray_subscription_path "$failed_id")"
    [ -f "$failed_file" ] || return 1
    failed_now_epoch="$(broray_subscription_now_epoch)"
    failed_now="$(broray_subscription_now_iso)"
    failed_duration=$(((failed_now_epoch - failed_started_epoch) * 1000))
    failed_enabled="$(jq -r '.enabled' "$failed_file")"
    failed_auto="$(jq -r '.autoUpdateEnabled' "$failed_file")"
    failed_interval="$(jq -r '.updateIntervalMinutes' "$failed_file")"
    broray_subscription_schedule_values \
        "$failed_enabled" "$failed_auto" "$failed_interval" "$failed_now_epoch"
    failed_temp="$BRORAY_SUB_TMP/subscription-failed.$$.json"
    jq \
        --arg now "$failed_now" \
        --argjson epoch "$failed_now_epoch" \
        --arg error "$failed_message" \
        --arg code "$failed_code" \
        --arg trigger "$failed_trigger" \
        --argjson durationMs "$failed_duration" \
        --arg nextAt "$BRORAY_SUB_NEXT_AT" \
        --argjson nextEpoch "$BRORAY_SUB_NEXT_EPOCH" '
        .lastUpdateStatus = "error" |
        .lastUpdatedAt = $now |
        .lastUpdatedEpoch = $epoch |
        .lastError = $error |
        .lastUpdateResult = {
            received: 0,
            parsed: 0,
            accepted: 0,
            rejected: 0,
            added: 0,
            updated: 0,
            unchanged: 0,
            removed: 0,
            warnings: [],
            durationMs: $durationMs,
            trigger: $trigger,
            errorCode: $code,
            activeServerImpact: (
                if $code == "ACTIVE_SERVER_CONFLICT"
                then "requires-decision"
                else "none"
                end
            )
        } |
        .nextUpdateAt = (if $nextAt == "" then null else $nextAt end) |
        .nextUpdateEpoch = (if $nextEpoch == 0 then null else $nextEpoch end) |
        .updatedAt = $now
    ' "$failed_file" > "$failed_temp" && \
        broray_subscription_write_json "$failed_file" "$failed_temp"
    rm -f "$failed_temp"
}

broray_subscription_update()
{
    broray_job_require_owner || return $?
    update_subscription_id="$1"
    update_trigger="${2:-manual}"
    case "$update_trigger" in
        initial|manual|automatic) ;;
        *)
            broray_subscription_set_error \
                "INVALID_TRIGGER" \
                "Неизвестный источник обновления."
            broray_subscription_emit_error
            return 1
            ;;
    esac
    broray_subscription_validate_id "$update_subscription_id" || {
        broray_subscription_emit_error
        return 1
    }
    update_path="$(broray_subscription_path "$update_subscription_id")"
    [ -f "$update_path" ] || {
        broray_subscription_set_error \
            "SUBSCRIPTION_NOT_FOUND" \
            "Подписка не найдена."
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_acquire_lock "$update_subscription_id" || {
        broray_subscription_emit_error
        return 1
    }

    if ! broray_subscription_ensure_client_hwid "$update_path"; then
        broray_subscription_release_lock "$update_subscription_id"
        broray_subscription_emit_error
        return 1
    fi
    update_client_hwid="$BRORAY_SUB_CLIENT_HWID"

    update_started_epoch="$(broray_subscription_now_epoch)"
    update_started_at="$(broray_subscription_now_iso)"
    update_id="$(printf '%s-%s-%s' "$update_subscription_id" "$update_started_epoch" "$$")"
    update_temp="$BRORAY_SUB_TMP/subscription-running.$$.json"
    jq \
        --arg now "$update_started_at" --arg operationId "$BRORAY_BACKGROUND_OPERATION_ID" '
        .lastUpdateStatus = "running" |
        .backgroundOperationId = $operationId |
        .lastError = null |
        .updatedAt = $now
    ' "$update_path" > "$update_temp" && \
        broray_subscription_write_json "$update_path" "$update_temp" || return 1
    rm -f "$update_temp"

    update_url="$(jq -r '.url' "$update_path")"
    update_enabled="$(jq -r '.enabled' "$update_path")"
    update_download="$BRORAY_SUB_TMP/subscription-download.$$.bin"
    update_nodes="$BRORAY_SUB_TMP/subscription-nodes.$$.txt"
    update_stage="$BRORAY_SUB_TMP/subscription-stage.$$.d"
    update_sync="$BRORAY_SUB_TMP/subscription-sync.$$.json"
    update_sync_error="$BRORAY_SUB_TMP/subscription-sync.$$.err"
    update_fail_code=""
    update_fail_message=""

    update_prepare_rc=0
    broray_subscription_prepare_update || update_prepare_rc=$?
    case "$update_prepare_rc" in
        130) broray_subscription_cleanup_preparation; return 130 ;;
        75) return 75 ;;
    esac
    if [ "$update_prepare_rc" != 0 ]; then
        update_fail_code="${BRORAY_SUB_ERROR_CODE:-INTERNAL_ERROR}"
        update_fail_message="${BRORAY_SUB_ERROR_MESSAGE:-Не удалось подготовить подписку.}"
    elif ! broray_job_checkpoint committing; then
        broray_subscription_cleanup_preparation
        broray_ops_cancel_requested && return 130
        return 1
    elif ! broray_server_subscription_sync \
        "$update_subscription_id" "$update_stage" "$update_enabled" "$update_id" \
        > "$update_sync" 2> "$update_sync_error"; then
        update_error_line="$(tail -n 1 "$update_sync_error")"
        update_fail_code="$(printf '%s' "$update_error_line" | cut -d: -f2)"
        update_fail_message="$(printf '%s' "$update_error_line" | cut -d: -f3-)"
        [ -n "$update_fail_code" ] || update_fail_code="SERVER_SYNC_ERROR"
        [ -n "$update_fail_message" ] || update_fail_message="Серверный модуль не применил обновление."
    fi

    if [ -n "$update_fail_code" ]; then
        broray_subscription_save_failed_update \
            "$update_subscription_id" \
            "$update_fail_code" \
            "$update_fail_message" \
            "$update_trigger" \
            "$update_started_epoch"
        broray_subscription_log \
            "subscription=$update_subscription_id action=update trigger=$update_trigger status=error code=$update_fail_code url=$(broray_subscription_mask_url "$update_url")"
        rm -rf \
            "$update_download" "$update_nodes" "$update_stage" \
            "$update_sync" "$update_sync_error" \
            "${BRORAY_SUB_WARNINGS_FILE:-}"
        broray_subscription_release_lock "$update_subscription_id"
        broray_subscription_set_error "$update_fail_code" "$update_fail_message"
        broray_subscription_emit_error
        return 1
    fi

    update_finished_epoch="$(broray_subscription_now_epoch)"
    update_finished_at="$(broray_subscription_now_iso)"
    update_duration=$(((update_finished_epoch - update_started_epoch) * 1000))
    update_interval="$(jq -r '.updateIntervalMinutes' "$update_path")"
    update_auto="$(jq -r '.autoUpdateEnabled' "$update_path")"
    broray_subscription_schedule_values \
        "$update_enabled" "$update_auto" "$update_interval" "$update_finished_epoch"

    if [ -f "${BRORAY_SUB_WARNINGS_FILE:-}" ]; then
        update_warnings_json="$(jq -R -s 'split("\n") | map(select(length > 0))' "$BRORAY_SUB_WARNINGS_FILE")"
    else
        update_warnings_json='[]'
    fi
    update_status="success"
    if [ "${BRORAY_SUB_REJECTED:-0}" -gt 0 ]; then
        update_status="partial"
    fi
    update_result="$BRORAY_SUB_TMP/subscription-result.$$.json"
    jq -n \
        --argjson received "${BRORAY_SUB_RECEIVED:-0}" \
        --argjson parsed "${BRORAY_SUB_PARSED:-0}" \
        --argjson accepted "${BRORAY_SUB_ACCEPTED:-0}" \
        --argjson rejected "${BRORAY_SUB_REJECTED:-0}" \
        --argjson sync "$(cat "$update_sync")" \
        --argjson warnings "$update_warnings_json" \
        --argjson durationMs "$update_duration" \
        --arg trigger "$update_trigger" '
        {
            received: $received,
            parsed: $parsed,
            accepted: $accepted,
            rejected: $rejected,
            added: $sync.added,
            updated: $sync.updated,
            unchanged: $sync.unchanged,
            removed: $sync.removed,
            warnings: ($warnings + ($sync.warnings // [])),
            durationMs: $durationMs,
            trigger: $trigger,
            errorCode: null,
            activeServerImpact: ($sync.activeServerImpact // "none")
        }
    ' > "$update_result" || {
        broray_subscription_save_failed_update \
            "$update_subscription_id" "INTERNAL_ERROR" \
            "Не удалось сохранить результат обновления." \
            "$update_trigger" "$update_started_epoch"
        rm -rf \
            "$update_download" "$update_nodes" "$update_stage" \
            "$update_sync" "$update_sync_error" "$update_result" \
            "${BRORAY_SUB_WARNINGS_FILE:-}"
        broray_subscription_release_lock "$update_subscription_id"
        broray_subscription_set_error \
            "INTERNAL_ERROR" \
            "Не удалось сохранить результат обновления."
        broray_subscription_emit_error
        return 1
    }

    update_save="$BRORAY_SUB_TMP/subscription-success.$$.json"
    jq \
        --arg status "$update_status" \
        --arg now "$update_finished_at" \
        --argjson epoch "$update_finished_epoch" \
        --argjson received "${BRORAY_SUB_ACCEPTED:-0}" \
        --argjson result "$(cat "$update_result")" \
        --arg nextAt "$BRORAY_SUB_NEXT_AT" \
        --argjson nextEpoch "$BRORAY_SUB_NEXT_EPOCH" '
        .lastUpdateStatus = $status |
        .lastUpdatedAt = $now |
        .lastUpdatedEpoch = $epoch |
        .lastError = null |
        .serversReceived = $received |
        .lastUpdateResult = $result |
        .nextUpdateAt = (if $nextAt == "" then null else $nextAt end) |
        .nextUpdateEpoch = (if $nextEpoch == 0 then null else $nextEpoch end) |
        .updatedAt = $now
    ' "$update_path" > "$update_save" || {
        broray_subscription_release_lock "$update_subscription_id"
        broray_subscription_set_error \
            "PERSISTENCE_ERROR" \
            "Не удалось сохранить состояние подписки."
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_write_json "$update_path" "$update_save" || {
        rm -f "$update_save"
        broray_subscription_release_lock "$update_subscription_id"
        broray_subscription_emit_error
        return 1
    }

    broray_subscription_log \
        "subscription=$update_subscription_id action=update trigger=$update_trigger status=$update_status accepted=${BRORAY_SUB_ACCEPTED:-0} rejected=${BRORAY_SUB_REJECTED:-0} url=$(broray_subscription_mask_url "$update_url")"
    rm -rf \
        "$update_download" "$update_nodes" "$update_stage" \
        "$update_sync" "$update_sync_error" "$update_result" \
        "$update_save" "${BRORAY_SUB_WARNINGS_FILE:-}"
    broray_subscription_release_lock "$update_subscription_id"
    broray_subscription_get "$update_subscription_id"
}

broray_subscription_delete()
{
    delete_id="$1"
    broray_subscription_validate_id "$delete_id" || {
        broray_subscription_emit_error
        return 1
    }
    delete_path="$(broray_subscription_path "$delete_id")"
    [ -f "$delete_path" ] || {
        broray_subscription_set_error \
            "SUBSCRIPTION_NOT_FOUND" \
            "Подписка не найдена."
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_acquire_lock "$delete_id" || {
        broray_subscription_emit_error
        return 1
    }
    delete_result="$BRORAY_SUB_TMP/subscription-delete.$$.json"
    delete_error="$BRORAY_SUB_TMP/subscription-delete.$$.err"
    if ! broray_server_subscription_remove "$delete_id" \
        > "$delete_result" 2> "$delete_error"; then
        delete_error_line="$(tail -n 1 "$delete_error")"
        delete_code="$(printf '%s' "$delete_error_line" | cut -d: -f2)"
        delete_message="$(printf '%s' "$delete_error_line" | cut -d: -f3-)"
        rm -f "$delete_result" "$delete_error"
        broray_subscription_release_lock "$delete_id"
        broray_subscription_set_error \
            "${delete_code:-SERVER_SOURCE_REMOVE_FAILED}" \
            "${delete_message:-Не удалось удалить серверы подписки.}"
        broray_subscription_emit_error
        return 1
    fi
    rm -f "$delete_path" || {
        rm -f "$delete_result" "$delete_error"
        broray_subscription_release_lock "$delete_id"
        broray_subscription_set_error \
            "PERSISTENCE_ERROR" \
            "Серверы удалены, но запись подписки удалить не удалось."
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_release_lock "$delete_id"
    broray_subscription_log "subscription=$delete_id action=delete"
    cat "$delete_result"
    rm -f "$delete_result" "$delete_error"
}

broray_subscription_servers()
{
    servers_id="$1"
    broray_subscription_validate_id "$servers_id" || {
        broray_subscription_emit_error
        return 1
    }
    broray_subscription_exists "$servers_id" || {
        broray_subscription_set_error \
            "SUBSCRIPTION_NOT_FOUND" \
            "Подписка не найдена."
        broray_subscription_emit_error
        return 1
    }
    broray_server_subscription_list "$servers_id"
}

broray_subscription_summary()
{
    broray_subscription_prepare_dirs
    summary_total=0
    summary_enabled=0
    summary_auto=false
    summary_latest_epoch=0
    summary_latest_status="never"
    summary_latest_at=""
    summary_latest_warnings='[]'
    summary_partial=0
    summary_errors=0
    summary_running=0
    summary_stale=0
    summary_never=0
    summary_now_epoch="$(broray_subscription_now_epoch)"

    for summary_file in "$BRORAY_SUB_DIR"/*.json; do
        [ -f "$summary_file" ] || continue
        broray_subscription_validate_file "$summary_file" || continue
        summary_total=$((summary_total + 1))
        summary_file_enabled="$(jq -r '.enabled' "$summary_file")"
        summary_file_auto="$(jq -r '.autoUpdateEnabled' "$summary_file")"
        broray_subscription_effective_status "$summary_file"
        summary_file_status="$BRORAY_SUB_EFFECTIVE_STATUS"

        [ "$summary_file_enabled" = "true" ] &&
            summary_enabled=$((summary_enabled + 1))
        if [ "$summary_file_enabled" = "true" ] &&
           [ "$summary_file_auto" = "true" ]; then
            summary_auto=true
        fi

        case "$summary_file_status" in
            partial) summary_partial=$((summary_partial + 1)) ;;
            error) summary_errors=$((summary_errors + 1)) ;;
            running) summary_running=$((summary_running + 1)) ;;
        esac

        summary_epoch="$(jq -r '.lastUpdatedEpoch // 0' "$summary_file")"
        summary_file_at="$(jq -r '.lastUpdatedAt // empty' "$summary_file")"
        summary_file_warnings="$(jq -c '.lastUpdateResult.warnings // []' "$summary_file")"
        if [ -n "$BRORAY_SUB_EFFECTIVE_FINISHED" ]; then
            # Coordinator timestamps are UTC. BusyBox date accepts the space
            # separated form; preserve the same attempt as the public card.
            summary_finished_epoch="$(date -u -d "$(printf '%s' "$BRORAY_SUB_EFFECTIVE_FINISHED" | tr T ' ' | sed 's/Z$//')" '+%s' 2>/dev/null)" || summary_finished_epoch=''
            case "$summary_finished_epoch" in ''|*[!0-9]*) ;;
                *) summary_epoch="$summary_finished_epoch"; summary_file_at="$BRORAY_SUB_EFFECTIVE_FINISHED"; summary_file_warnings='[]' ;;
            esac
        fi
        case "$summary_epoch" in
            ''|*[!0-9]*) summary_epoch=0 ;;
        esac

        if [ "$summary_file_enabled" = "true" ]; then
            summary_interval_minutes="$(jq -r '.updateIntervalMinutes // 360' "$summary_file")"
            case "$summary_interval_minutes" in
                ''|*[!0-9]*) summary_interval_minutes=360 ;;
            esac
            [ "$summary_interval_minutes" -ge 15 ] || summary_interval_minutes=15
            summary_stale_after=$((summary_interval_minutes * 120))
            if [ "$summary_epoch" -le 0 ]; then
                summary_never=$((summary_never + 1))
            elif [ $((summary_now_epoch - summary_epoch)) -gt "$summary_stale_after" ]; then
                summary_stale=$((summary_stale + 1))
            fi
        fi

        if [ "$summary_epoch" -gt "$summary_latest_epoch" ]; then
            summary_latest_epoch="$summary_epoch"
            summary_latest_status="$summary_file_status"
            summary_latest_at="$summary_file_at"
            summary_latest_warnings="$summary_file_warnings"
        fi
    done

    summary_servers="$(broray_server_subscription_count_all 2>/dev/null || printf '0')"
    summary_updated_at="$(broray_subscription_now_iso)"

    health_reasons="$({
        [ "$summary_errors" -eq 0 ] || broray_status_reason SUBSCRIPTION_UPDATE_ERROR "Последнее обновление одной или нескольких подписок завершилось ошибкой."
        [ "$summary_partial" -eq 0 ] || broray_status_reason SUBSCRIPTION_UPDATE_PARTIAL "Одна или несколько подписок обновлены частично."
        [ "$summary_running" -eq 0 ] || broray_status_reason SUBSCRIPTION_UPDATE_RUNNING "Обновление подписки выполняется."
        [ "$summary_stale" -eq 0 ] || broray_status_reason SUBSCRIPTION_UPDATE_STALE "Автоматическое обновление одной или нескольких подписок просрочено."
        [ "$summary_never" -eq 0 ] || broray_status_reason SUBSCRIPTION_NEVER_UPDATED "Одна или несколько активных подписок ещё не обновлялись."
    } | jq -sc '.')"

    if [ "$summary_errors" -gt 0 ]; then
        health_severity=error
        health_operational=false
        health_action_required=true
    elif [ "$summary_partial" -gt 0 ] || [ "$summary_stale" -gt 0 ] || [ "$summary_never" -gt 0 ]; then
        health_severity=warning
        health_operational=true
        health_action_required=true
    elif [ "$summary_running" -gt 0 ]; then
        health_severity=busy
        health_operational=true
        health_action_required=false
    else
        health_severity=ok
        health_operational=true
        health_action_required=false
    fi

    health_facts="$(jq -nc \
        --argjson total "$summary_total" \
        --argjson enabled "$summary_enabled" \
        --argjson partial "$summary_partial" \
        --argjson errors "$summary_errors" \
        --argjson running "$summary_running" \
        --argjson stale "$summary_stale" \
        --argjson neverUpdated "$summary_never" \
        --arg lastUpdateStatus "$summary_latest_status" \
        '{total:$total,enabled:$enabled,partial:$partial,errors:$errors,running:$running,stale:$stale,neverUpdated:$neverUpdated,lastUpdateStatus:$lastUpdateStatus}')"

    health_consistent=true
    [ "$summary_partial" -eq 0 ] || health_consistent=false
    [ "$summary_errors" -eq 0 ] || health_consistent=false
    [ "$summary_stale" -eq 0 ] || health_consistent=false
    [ "$summary_never" -eq 0 ] || health_consistent=false

    health_freshness=fresh
    [ "$summary_stale" -eq 0 ] || health_freshness=stale
    [ "$summary_never" -eq 0 ] || health_freshness=unknown

    health_json="$(broray_status_contract \
        subscriptions available "$health_severity" \
        "$health_operational" "$health_consistent" "$health_action_required" \
        "$health_freshness" "$summary_latest_at" "$health_reasons" "$health_facts" null)"

    jq -n \
        --argjson total "$summary_total" \
        --argjson enabled "$summary_enabled" \
        --argjson serversReceived "$summary_servers" \
        --arg lastUpdateStatus "$summary_latest_status" \
        --arg lastUpdatedAt "$summary_latest_at" \
        --argjson lastWarnings "$summary_latest_warnings" \
        --argjson partialCount "$summary_partial" \
        --argjson errorCount "$summary_errors" \
        --argjson runningCount "$summary_running" \
        --argjson staleCount "$summary_stale" \
        --argjson neverUpdatedCount "$summary_never" \
        --argjson autoUpdateEnabled "$summary_auto" \
        --argjson health "$health_json" '
        {
            total: $total,
            enabled: $enabled,
            serversReceived: $serversReceived,
            lastUpdateStatus: $lastUpdateStatus,
            lastUpdatedAt: (
                if $lastUpdatedAt == ""
                then null
                else $lastUpdatedAt
                end
            ),
            lastWarnings: $lastWarnings,
            partialCount: $partialCount,
            errorCount: $errorCount,
            runningCount: $runningCount,
            staleCount: $staleCount,
            neverUpdatedCount: $neverUpdatedCount,
            autoUpdateEnabled: $autoUpdateEnabled,
            health: $health
        }
    '
}

broray_subscription_scheduler_once()
{
    local scheduler_result scheduler_rc
    scheduler_result=0
    broray_subscription_prepare_dirs
    broray_subscription_recover_stale || return $?
    scheduler_now="$(broray_subscription_now_epoch)"
    for scheduler_file in "$BRORAY_SUB_DIR"/*.json; do
        [ -f "$scheduler_file" ] || continue
        scheduler_id="$(jq -r '.id // empty' "$scheduler_file")"
        scheduler_enabled="$(jq -r '.enabled // false' "$scheduler_file")"
        scheduler_auto="$(jq -r '.autoUpdateEnabled // false' "$scheduler_file")"
        scheduler_next="$(jq -r '.nextUpdateEpoch // 0' "$scheduler_file")"
        case "$scheduler_next" in
            ''|*[!0-9]*) scheduler_next=0 ;;
        esac
        [ -n "$scheduler_id" ] || continue
        [ "$scheduler_enabled" = "true" ] || continue
        [ "$scheduler_auto" = "true" ] || continue
        [ "$scheduler_next" -gt 0 ] || continue
        [ "$scheduler_next" -le "$scheduler_now" ] || continue
        broray_job_checkpoint working || return $?
        scheduler_rc=0
        broray_subscription_update "$scheduler_id" automatic \
            >/dev/null 2>&1 || scheduler_rc=$?
        case "$scheduler_rc" in 130|75) return "$scheduler_rc" ;; esac
        [ "$scheduler_rc" = 0 ] || scheduler_result=1
    done
    return "$scheduler_result"
}
