#!/opt/bin/ash

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"

. "$BRORAY_BASE/lib/util.sh"

broray_hy2_urldecode()
{
    broray_uri_component_decode "$1"
}

broray_hy2_query_value()
{
    broray_uri_query_value "$2" "$1"
}

broray_parse_hysteria2()
{
    original_uri="$1"

    case "$original_uri" in
        hysteria2://*)
            uri_body="${original_uri#hysteria2://}"
            ;;
        hy2://*)
            uri_body="${original_uri#hy2://}"
            ;;
        *)
            broray_die \
                "ссылка не является Hysteria2"
            ;;
    esac

    case "$uri_body" in
        *'#'*)
            encoded_name="${uri_body#*#}"
            uri_body="${uri_body%%#*}"
            ;;
        *)
            encoded_name=""
            ;;
    esac

    case "$uri_body" in
        *'?'*)
            query_string="${uri_body#*\?}"
            authority="${uri_body%%\?*}"
            ;;
        *)
            query_string=""
            authority="$uri_body"
            ;;
    esac

    # Split the optional root path before interpreting auth, host and port.
    # Keep percent-encoded delimiters intact until their component is decoded.
    case "$authority" in
        */*)
            uri_path="/${authority#*/}"
            authority="${authority%%/*}"
            [ "$uri_path" = / ] ||
                broray_die "путь в ссылке Hysteria2 не поддерживается"
            ;;
    esac

    case "$authority" in
        *@*)
            encoded_auth="${authority%@*}"
            host_port="${authority##*@}"
            ;;
        *)
            broray_die \
                "в ссылке Hysteria2 отсутствует auth"
            ;;
    esac

    BRORAY_AUTH="$(
        broray_uri_component_decode "$encoded_auth"
    )" || broray_die "неправильное кодирование URI Hysteria2"

    [ -n "$BRORAY_AUTH" ] ||
        broray_die \
            "Hysteria2 auth не может быть пустым"

    case "$host_port" in
        \[*\]:*)
            BRORAY_ADDRESS="${host_port%%\]*}"
            BRORAY_ADDRESS="${BRORAY_ADDRESS#\[}"
            BRORAY_PORT="${host_port##*\]:}"
            ;;
        \[*\])
            BRORAY_ADDRESS="${host_port#\[}"
            BRORAY_ADDRESS="${BRORAY_ADDRESS%\]}"
            BRORAY_PORT=443
            ;;
        *:*)
            BRORAY_ADDRESS="${host_port%:*}"
            BRORAY_PORT="${host_port##*:}"
            ;;
        *)
            BRORAY_ADDRESS="$host_port"
            BRORAY_PORT=443
            ;;
    esac

    [ -n "$BRORAY_ADDRESS" ] ||
        broray_die \
            "в ссылке Hysteria2 отсутствует адрес"

    case "$BRORAY_PORT" in
        ''|*[!0-9]*)
            broray_die \
                "порт Hysteria2 должен быть числом"
            ;;
    esac

    [ "$BRORAY_PORT" -ge 1 ] 2>/dev/null &&
    [ "$BRORAY_PORT" -le 65535 ] 2>/dev/null ||
        broray_die \
            "порт Hysteria2 должен находиться в диапазоне 1–65535"

    if [ -n "$encoded_name" ]; then
        BRORAY_NAME="$(
            broray_uri_component_decode "$encoded_name"
        )" || broray_die "неправильное кодирование URI Hysteria2"
    else
        BRORAY_NAME="$BRORAY_ADDRESS"
    fi

    BRORAY_SECURITY="$(broray_hy2_query_value "$query_string" "security")" || broray_die "неправильный параметр URI Hysteria2"

    [ -n "$BRORAY_SECURITY" ] ||
        BRORAY_SECURITY="tls"

    BRORAY_SNI="$(broray_hy2_query_value "$query_string" "sni")" || broray_die "неправильный параметр URI Hysteria2"

    [ -n "$BRORAY_SNI" ] ||
        BRORAY_SNI="$(broray_hy2_query_value "$query_string" "peer")" || broray_die "неправильный параметр URI Hysteria2"

    [ -n "$BRORAY_SNI" ] ||
        BRORAY_SNI="$BRORAY_ADDRESS"

    BRORAY_FP="$(broray_hy2_query_value "$query_string" "fp")" || broray_die "неправильный параметр URI Hysteria2"

    [ -n "$BRORAY_FP" ] ||
        BRORAY_FP="chrome"

    alpn_value="$(broray_hy2_query_value "$query_string" "alpn")" || broray_die "неправильный параметр URI Hysteria2"

    if [ -n "$alpn_value" ]; then
        BRORAY_ALPN="$(
            printf '%s' "$alpn_value" |
                jq -R '
                    split(",") |
                    map(select(length > 0))
                '
        )"
    else
        BRORAY_ALPN='["h3"]'
    fi

    BRORAY_OBFS="$(broray_hy2_query_value "$query_string" "obfs")" || broray_die "неправильный параметр URI Hysteria2"

    BRORAY_OBFS_PASSWORD="$(broray_hy2_query_value "$query_string" "obfs-password")" || broray_die "неправильный параметр URI Hysteria2"

    BRORAY_UP_MBPS="$(broray_hy2_query_value "$query_string" "upmbps")" || broray_die "неправильный параметр URI Hysteria2"

    BRORAY_DOWN_MBPS="$(broray_hy2_query_value "$query_string" "downmbps")" || broray_die "неправильный параметр URI Hysteria2"

    case "$BRORAY_OBFS" in
        '') [ -z "$BRORAY_OBFS_PASSWORD" ] || broray_die "Hysteria2 obfs-password требует obfs=salamander" ;;
        salamander) [ -n "$BRORAY_OBFS_PASSWORD" ] || broray_die "Hysteria2 salamander требует obfs-password" ;;
        *) broray_die "неподдерживаемый Hysteria2 obfs: $BRORAY_OBFS" ;;
    esac
    [ -z "$BRORAY_UP_MBPS" ] && [ -z "$BRORAY_DOWN_MBPS" ] ||
        broray_die "Hysteria2 upmbps/downmbps не поддерживаются"

    BRORAY_PIN_SHA256="$(broray_hy2_query_value "$query_string" pinSHA256)" || broray_die "неправильный pinSHA256"
    if [ -n "$BRORAY_PIN_SHA256" ]; then
        BRORAY_PIN_SHA256="$(printf '%s' "$BRORAY_PIN_SHA256" | tr -d ':' | tr 'A-F' 'a-f')"
        [ "${#BRORAY_PIN_SHA256}" = 64 ] || broray_die "pinSHA256 должен содержать 64 hex-символа"
        case "$BRORAY_PIN_SHA256" in *[!0-9a-f]*) broray_die "неправильный pinSHA256" ;; esac
    fi

    insecure_value="$(broray_hy2_query_value "$query_string" "insecure")" || broray_die "неправильный параметр URI Hysteria2"

    case "$insecure_value" in
        1|true|TRUE|yes|YES)
            [ -n "$BRORAY_PIN_SHA256" ] || broray_die "Hysteria2 insecure требует pinSHA256"
            BRORAY_ALLOW_INSECURE="false"
            ;;
        *)
            BRORAY_ALLOW_INSECURE="false"
            ;;
    esac

    BRORAY_FINAL_MASK_RAW="$(broray_hy2_query_value "$query_string" "fm")" || broray_die "неправильный параметр URI Hysteria2"

    if [ -n "$BRORAY_FINAL_MASK_RAW" ]; then
        printf '%s' "$BRORAY_FINAL_MASK_RAW" | jq -e 'type == "object"' >/dev/null 2>&1 ||
            broray_die "Hysteria2 fm должен быть JSON object"
        BRORAY_FINAL_MASK="$BRORAY_FINAL_MASK_RAW"
    else
        BRORAY_FINAL_MASK='{}'
    fi

    BRORAY_NETWORK="hysteria"
}
