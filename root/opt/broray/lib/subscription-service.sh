#!/opt/bin/ash

BRORAY_SUB_BASE="${BRORAY_SUB_BASE:-${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}}"
BRORAY_BASE="${BRORAY_BASE:-$BRORAY_SUB_BASE}"

. "$BRORAY_BASE/lib/server-subscription-service.sh"
BRORAY_SUB_DIR="${BRORAY_SUB_DIR:-$BRORAY_SUB_BASE/config/subscriptions}"
BRORAY_SUB_RUN="${BRORAY_SUB_RUN:-$BRORAY_SUB_BASE/run/subscriptions}"
BRORAY_SUB_TMP="${BRORAY_SUB_TMP:-$BRORAY_SUB_BASE/tmp}"
BRORAY_SUB_LOG="${BRORAY_SUB_LOG:-$BRORAY_SUB_BASE/logs/subscriptions.log}"
BRORAY_SUB_MAX_BYTES="${BRORAY_SUB_MAX_BYTES:-2097152}"
BRORAY_SUB_MAX_NODES="${BRORAY_SUB_MAX_NODES:-500}"
BRORAY_SUB_MIN_INTERVAL="${BRORAY_SUB_MIN_INTERVAL:-5}"
BRORAY_SUB_MAX_INTERVAL="${BRORAY_SUB_MAX_INTERVAL:-10080}"

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
        (.id | type == "string" and length > 0) and
        (.name | type == "string" and length > 0 and length <= 128) and
        (.url | type == "string" and length > 0 and length <= 4096) and
        (.enabled | type == "boolean") and
        (.autoUpdateEnabled | type == "boolean") and
        (.updateIntervalMinutes | type == "number" and floor == . and . >= $min and . <= $max) and
        (.lastUpdateStatus | IN("never", "running", "success", "partial", "error")) and
        (.serversReceived | type == "number" and . >= 0) and
        (.createdAt | type == "string") and
        (.updatedAt | type == "string")
    ' "$validate_file" >/dev/null 2>&1
}

broray_subscription_write_json()
{
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
    mv "$write_temp" "$write_target" || {
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
    public_ip="$1"
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
            awk '
                /^Name:/ {answer=1; next}
                answer && /^Address [0-9]*:/ {print $NF}
                answer && /^Address:/ {print $2}
            ' |
            sed 's/#.*//' |
            sort -u > "$resolve_file"
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

broray_subscription_fetch()
{
    fetch_url="$1"
    fetch_output="$2"
    command -v curl >/dev/null 2>&1 || {
        broray_subscription_set_error \
            "HTTP_ERROR" \
            "Для обновления подписок требуется curl."
        return 1
    }

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
        fetch_size_args=""
        if curl --help all 2>/dev/null | grep -q -- '--max-filesize'; then
            fetch_size_args="--max-filesize $BRORAY_SUB_MAX_BYTES"
        fi
        : > "$fetch_headers"
        : > "$fetch_error"
        # shellcheck disable=SC2086
        fetch_status="$(
            curl \
                --silent \
                --show-error \
                --noproxy '*' \
                --proto '=http,https' \
                --connect-timeout 10 \
                --max-time 35 \
                --max-redirs 0 \
                --resolve "$fetch_resolve" \
                $fetch_size_args \
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
    extract_count="$(wc -l < "$extract_output" | tr -d ' ')"
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
        broray_subscription_set_error \
            "NO_VALID_NODES" \
            "Подписка не содержит ни одного поддерживаемого и валидного узла."
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
    broray_subscription_prepare_dirs
    acquire_lock="$(broray_subscription_lock_path "$acquire_id")"
    if mkdir "$acquire_lock" 2>/dev/null; then
        printf '%s\n' "$$" > "$acquire_lock/pid"
        return 0
    fi
    acquire_old_pid="$(cat "$acquire_lock/pid" 2>/dev/null || true)"
    if [ -n "$acquire_old_pid" ] && kill -0 "$acquire_old_pid" 2>/dev/null; then
        broray_subscription_set_error \
            "UPDATE_ALREADY_RUNNING" \
            "Обновление этой подписки уже выполняется."
        return 1
    fi
    rm -rf "$acquire_lock"
    if mkdir "$acquire_lock" 2>/dev/null; then
        printf '%s\n' "$$" > "$acquire_lock/pid"
        return 0
    fi
    broray_subscription_set_error \
        "UPDATE_ALREADY_RUNNING" \
        "Не удалось получить блокировку подписки."
    return 1
}

broray_subscription_release_lock()
{
    release_id="$1"
    rm -rf "$(broray_subscription_lock_path "$release_id")"
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

broray_subscription_public_file()
{
    public_file="$1"
    public_include_url="${2:-false}"
    public_id="$(jq -r '.id' "$public_file")"
    public_count="$(broray_server_subscription_count "$public_id" 2>/dev/null || printf '0')"
    public_display_url="$(broray_subscription_mask_url "$(jq -r '.url' "$public_file")")"
    jq \
        --arg displayUrl "$public_display_url" \
        --argjson serversCount "$public_count" \
        --argjson includeUrl "$public_include_url" '
        . + {
            displayUrl: $displayUrl,
            serversCount: $serversCount
        } |
        if $includeUrl then . else del(.url) end |
        del(.lastUpdatedEpoch, .nextUpdateEpoch)
    ' "$public_file"
}

broray_subscription_recover_stale()
{
    broray_subscription_prepare_dirs
    for recover_file in "$BRORAY_SUB_DIR"/*.json; do
        [ -f "$recover_file" ] || continue
        recover_status="$(jq -r '.lastUpdateStatus // "never"' "$recover_file" 2>/dev/null)"
        [ "$recover_status" = "running" ] || continue
        recover_id="$(jq -r '.id // empty' "$recover_file")"
        [ -n "$recover_id" ] || continue
        recover_lock="$(broray_subscription_lock_path "$recover_id")"
        recover_pid="$(cat "$recover_lock/pid" 2>/dev/null || true)"
        if [ -n "$recover_pid" ] && kill -0 "$recover_pid" 2>/dev/null; then
            continue
        fi
        rm -rf "$recover_lock"
        recover_now_epoch="$(broray_subscription_now_epoch)"
        recover_now="$(broray_subscription_now_iso)"
        recover_temp="$BRORAY_SUB_TMP/subscription-recover.$$.json"
        recover_enabled="$(jq -r '.enabled' "$recover_file")"
        recover_auto="$(jq -r '.autoUpdateEnabled' "$recover_file")"
        recover_interval="$(jq -r '.updateIntervalMinutes' "$recover_file")"
        broray_subscription_schedule_values \
            "$recover_enabled" "$recover_auto" "$recover_interval" "$recover_now_epoch"
        jq \
            --arg now "$recover_now" \
            --argjson epoch "$recover_now_epoch" \
            --arg error "Предыдущее обновление было прервано перезапуском процесса." \
            --arg nextAt "$BRORAY_SUB_NEXT_AT" \
            --argjson nextEpoch "$BRORAY_SUB_NEXT_EPOCH" '
            .lastUpdateStatus = "error" |
            .lastUpdatedAt = $now |
            .lastUpdatedEpoch = $epoch |
            .lastError = $error |
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
    broray_subscription_recover_stale
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
        broray_subscription_update "$create_id" initial >/dev/null 2>&1 || true
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

    update_started_epoch="$(broray_subscription_now_epoch)"
    update_started_at="$(broray_subscription_now_iso)"
    update_id="$(printf '%s-%s-%s' "$update_subscription_id" "$update_started_epoch" "$$")"
    update_temp="$BRORAY_SUB_TMP/subscription-running.$$.json"
    jq \
        --arg now "$update_started_at" '
        .lastUpdateStatus = "running" |
        .lastError = null |
        .updatedAt = $now
    ' "$update_path" > "$update_temp" && \
        broray_subscription_write_json "$update_path" "$update_temp"
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

    if ! broray_subscription_fetch "$update_url" "$update_download"; then
        update_fail_code="$BRORAY_SUB_ERROR_CODE"
        update_fail_message="$BRORAY_SUB_ERROR_MESSAGE"
    elif ! broray_subscription_extract_nodes "$update_download" "$update_nodes"; then
        update_fail_code="$BRORAY_SUB_ERROR_CODE"
        update_fail_message="$BRORAY_SUB_ERROR_MESSAGE"
    elif ! broray_subscription_stage_nodes \
        "$update_subscription_id" "$update_nodes" "$update_stage" "$update_enabled"; then
        update_fail_code="$BRORAY_SUB_ERROR_CODE"
        update_fail_message="$BRORAY_SUB_ERROR_MESSAGE"
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
    broray_subscription_recover_stale
    summary_total=0
    summary_enabled=0
    summary_auto=false
    summary_latest_epoch=0
    summary_latest_status="never"
    summary_latest_at=""
    for summary_file in "$BRORAY_SUB_DIR"/*.json; do
        [ -f "$summary_file" ] || continue
        broray_subscription_validate_file "$summary_file" || continue
        summary_total=$((summary_total + 1))
        summary_file_enabled="$(jq -r '.enabled' "$summary_file")"
        summary_file_auto="$(jq -r '.autoUpdateEnabled' "$summary_file")"
        [ "$summary_file_enabled" = "true" ] && \
            summary_enabled=$((summary_enabled + 1))
        if [ "$summary_file_enabled" = "true" ] && \
           [ "$summary_file_auto" = "true" ]; then
            summary_auto=true
        fi
        summary_epoch="$(jq -r '.lastUpdatedEpoch // 0' "$summary_file")"
        case "$summary_epoch" in
            ''|*[!0-9]*) summary_epoch=0 ;;
        esac
        if [ "$summary_epoch" -gt "$summary_latest_epoch" ]; then
            summary_latest_epoch="$summary_epoch"
            summary_latest_status="$(jq -r '.lastUpdateStatus' "$summary_file")"
            summary_latest_at="$(jq -r '.lastUpdatedAt // empty' "$summary_file")"
        fi
    done
    summary_servers="$(broray_server_subscription_count_all 2>/dev/null || printf '0')"
    jq -n \
        --argjson total "$summary_total" \
        --argjson enabled "$summary_enabled" \
        --argjson serversReceived "$summary_servers" \
        --arg lastUpdateStatus "$summary_latest_status" \
        --arg lastUpdatedAt "$summary_latest_at" \
        --argjson autoUpdateEnabled "$summary_auto" '
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
            autoUpdateEnabled: $autoUpdateEnabled
        }
    '
}

broray_subscription_scheduler_once()
{
    broray_subscription_prepare_dirs
    broray_subscription_recover_stale
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
        broray_subscription_update "$scheduler_id" automatic \
            >/dev/null 2>&1 || true
    done
}
