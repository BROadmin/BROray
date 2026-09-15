#!/opt/bin/ash

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"

. "$BRORAY_BASE/lib/util.sh"

broray_shadowsocks_base64_decode()
{
    encoded_value="$1"
    decoded_file="$2"

    normalized_value="$(
        printf '%s' "$encoded_value" |
            tr '_-' '/+'
    )"
    normalized_length="${#normalized_value}"
    normalized_remainder="$((normalized_length % 4))"
    case "$normalized_remainder" in
        0) ;;
        2) normalized_value="${normalized_value}==" ;;
        3) normalized_value="${normalized_value}=" ;;
        *) broray_die "Shadowsocks содержит неправильную Base64-строку" ;;
    esac

    printf '%s' "$normalized_value" |
        base64 -d > "$decoded_file" 2>/dev/null ||
        broray_die "не удалось декодировать Shadowsocks Base64"
    [ -s "$decoded_file" ] ||
        broray_die "Shadowsocks Base64 содержит пустое значение"
}

broray_parse_shadowsocks()
{
    original_uri="$1"

    case "$original_uri" in
        ss://*) uri_body="${original_uri#ss://}" ;;
        *) broray_die "поддерживаются только ссылки ss://" ;;
    esac

    case "$uri_body" in
        *'#'*)
            encoded_name="${uri_body#*#}"
            uri_body="${uri_body%%#*}"
            ;;
        *) encoded_name="" ;;
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

    plugin_value="$(
        broray_url_decode \
            "$(broray_query_value plugin "$query_string")"
    )"
    [ -z "$plugin_value" ] ||
        broray_die "Shadowsocks SIP003 plugins пока не поддерживаются"

    mkdir -p "$BRORAY_TMP"
    decoded_file="$BRORAY_TMP/shadowsocks-decoded.$$.txt"
    rm -f "$decoded_file"

    case "$authority" in
        *@*)
            encoded_credentials="${authority%@*}"
            host_port="${authority##*@}"
            broray_shadowsocks_base64_decode \
                "$encoded_credentials" "$decoded_file"
            credentials="$(cat "$decoded_file")"
            ;;
        *)
            broray_shadowsocks_base64_decode \
                "$authority" "$decoded_file"
            decoded_authority="$(cat "$decoded_file")"
            case "$decoded_authority" in
                *@*)
                    credentials="${decoded_authority%@*}"
                    host_port="${decoded_authority##*@}"
                    ;;
                *)
                    rm -f "$decoded_file"
                    broray_die "Shadowsocks legacy Base64 не содержит адрес сервера"
                    ;;
            esac
            ;;
    esac
    rm -f "$decoded_file"

    case "$credentials" in
        *:*)
            BRORAY_METHOD="${credentials%%:*}"
            BRORAY_PASSWORD="${credentials#*:}"
            ;;
        *) broray_die "Shadowsocks не содержит method:password" ;;
    esac

    case "$host_port" in
        \[*\]:*)
            BRORAY_ADDRESS="${host_port%%\]*}"
            BRORAY_ADDRESS="${BRORAY_ADDRESS#\[}"
            BRORAY_PORT="${host_port##*\]:}"
            ;;
        *:*)
            BRORAY_ADDRESS="${host_port%:*}"
            BRORAY_PORT="${host_port##*:}"
            ;;
        *) broray_die "Shadowsocks не содержит порт" ;;
    esac

    [ -n "$BRORAY_METHOD" ] ||
        broray_die "Shadowsocks содержит пустой метод шифрования"
    [ -n "$BRORAY_PASSWORD" ] ||
        broray_die "Shadowsocks содержит пустой пароль"
    [ -n "$BRORAY_ADDRESS" ] ||
        broray_die "Shadowsocks не содержит адрес сервера"
    case "$BRORAY_PORT" in
        ''|*[!0-9]*) broray_die "порт Shadowsocks должен быть числом" ;;
    esac
    [ "$BRORAY_PORT" -ge 1 ] 2>/dev/null &&
    [ "$BRORAY_PORT" -le 65535 ] 2>/dev/null ||
        broray_die "порт Shadowsocks должен находиться в диапазоне 1–65535"

    case "$BRORAY_METHOD" in
        aes-128-gcm|aes-256-gcm|chacha20-poly1305|chacha20-ietf-poly1305|xchacha20-poly1305|2022-blake3-aes-128-gcm|2022-blake3-aes-256-gcm|2022-blake3-chacha20-poly1305)
            ;;
        *) broray_die "неподдерживаемый метод Shadowsocks: $BRORAY_METHOD" ;;
    esac

    BRORAY_PROTOCOL="shadowsocks"
    BRORAY_NETWORK="raw"
    BRORAY_SECURITY="none"
    BRORAY_NAME="$(broray_url_decode "$encoded_name")"
    [ -n "$BRORAY_NAME" ] ||
        BRORAY_NAME="$BRORAY_ADDRESS:$BRORAY_PORT"
}
