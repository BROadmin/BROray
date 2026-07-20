#!/bin/sh

. /opt/broray/lib/util.sh

broray_vmess_base64_decode() {
    encoded="$1"
    output_file="$2"

    normalized="$(
        printf '%s' "$encoded" |
            tr '_-' '/+'
    )"

    remainder="$(( ${#normalized} % 4 ))"

    case "$remainder" in
        0)
            ;;
        2)
            normalized="${normalized}=="
            ;;
        3)
            normalized="${normalized}="
            ;;
        *)
            broray_die \
                "VMess содержит неправильную Base64-строку"
            ;;
    esac

    printf '%s' "$normalized" |
        base64 -d > "$output_file" 2>/dev/null ||
        broray_die \
            "не удалось декодировать VMess Base64"
}

broray_parse_vmess() {
    uri="$1"

    [ -n "$uri" ] ||
        broray_die "не указана ссылка VMess"

    case "$uri" in
        vmess://*)
            ;;
        *)
            broray_die \
                "поддерживаются только ссылки vmess://"
            ;;
    esac

    command -v base64 >/dev/null 2>&1 ||
        broray_die "не найдена команда base64"

    command -v jq >/dev/null 2>&1 ||
        broray_die "не найдена команда jq"

    encoded="${uri#vmess://}"

    case "$encoded" in
        *#*)
            encoded="${encoded%%#*}"
            ;;
    esac

    decoded_file="/opt/broray/tmp/vmess-decoded.$$.json"

    mkdir -p /opt/broray/tmp

    broray_vmess_base64_decode \
        "$encoded" \
        "$decoded_file"

    jq -e '
        type == "object"
    ' "$decoded_file" >/dev/null 2>&1 ||
        broray_die \
            "VMess Base64 не содержит корректный JSON"

    BRORAY_PROTOCOL="vmess"

    BRORAY_NAME="$(
        jq -r \
            '.ps // .remarks // .name // empty' \
            "$decoded_file"
    )"

    BRORAY_ADDRESS="$(
        jq -r \
            '.add // .address // empty' \
            "$decoded_file"
    )"

    BRORAY_PORT="$(
        jq -r \
            '.port // empty | tostring' \
            "$decoded_file"
    )"

    BRORAY_UUID="$(
        jq -r \
            '.id // .uuid // empty' \
            "$decoded_file"
    )"

    BRORAY_ALTER_ID="$(
        jq -r \
            '.aid // .alterId // 0 | tostring' \
            "$decoded_file"
    )"

    BRORAY_ENCRYPTION="$(
        jq -r \
            '.scy // .securityCipher // .cipher // "auto"' \
            "$decoded_file"
    )"

    BRORAY_NETWORK="$(
        jq -r \
            '.net // .network // "tcp"' \
            "$decoded_file"
    )"

    BRORAY_SECURITY="$(
        jq -r \
            '.tls // .security // "none"' \
            "$decoded_file"
    )"

    BRORAY_SNI="$(
        jq -r \
            '.sni // .serverName // empty' \
            "$decoded_file"
    )"

    BRORAY_FP="$(
        jq -r \
            '.fp // .fingerprint // "chrome"' \
            "$decoded_file"
    )"

    BRORAY_ALPN="$(
        jq -c '
            if (.alpn | type) == "array" then
                .alpn
            elif (.alpn | type) == "string" and
                 (.alpn | length) > 0 then
                (.alpn | split(",") |
                    map(gsub("^\\s+|\\s+$"; "")) |
                    map(select(length > 0)))
            else
                []
            end
        ' "$decoded_file"
    )"

    BRORAY_ALLOW_INSECURE="$(
        jq -r '
            .allowInsecure //
            .allow_insecure //
            false
            |
            if . == true or . == 1 or . == "1" or
               . == "true"
            then "true"
            else "false"
            end
        ' "$decoded_file"
    )"

    BRORAY_HOST="$(
        jq -r \
            '.host // empty' \
            "$decoded_file"
    )"

    BRORAY_PATH="$(
        jq -r \
            '.path // "/"' \
            "$decoded_file"
    )"

    BRORAY_SERVICE_NAME="$(
        jq -r \
            '.serviceName // .service_name // .path // empty' \
            "$decoded_file"
    )"

    BRORAY_MODE="$(
        jq -r \
            '.mode // "auto"' \
            "$decoded_file"
    )"

    BRORAY_HEADER_TYPE="$(
        jq -r \
            '.type // .headerType // "none"' \
            "$decoded_file"
    )"

    BRORAY_PBK="$(
        jq -r \
            '.pbk // .publicKey // empty' \
            "$decoded_file"
    )"

    BRORAY_SID="$(
        jq -r \
            '.sid // .shortId // empty' \
            "$decoded_file"
    )"

    BRORAY_SPX="$(
        jq -r \
            '.spx // .spiderX // empty' \
            "$decoded_file"
    )"

    BRORAY_EXTRA="$(
        jq -c '
            if (.extra | type) == "object"
            then .extra
            else {}
            end
        ' "$decoded_file"
    )"

    rm -f "$decoded_file"

    case "$BRORAY_NETWORK" in
        tcp)
            BRORAY_NETWORK="raw"
            ;;
        ws|websocket)
            BRORAY_NETWORK="ws"
            ;;
        grpc)
            BRORAY_NETWORK="grpc"
            ;;
        httpupgrade|httpUpgrade)
            BRORAY_NETWORK="httpupgrade"
            ;;
        xhttp|splithttp)
            BRORAY_NETWORK="xhttp"
            ;;
        raw)
            ;;
        *)
            broray_die \
                "неподдерживаемый транспорт VMess: $BRORAY_NETWORK"
            ;;
    esac

    case "$BRORAY_SECURITY" in
        ""|none)
            BRORAY_SECURITY="none"
            ;;
        tls)
            ;;
        reality)
            ;;
        *)
            broray_die \
                "неподдерживаемая защита VMess: $BRORAY_SECURITY"
            ;;
    esac

    case "$BRORAY_ALTER_ID" in
        ''|*[!0-9]*)
            broray_die \
                "VMess alterId должен быть числом"
            ;;
    esac

    [ -n "$BRORAY_ADDRESS" ] ||
        broray_die "VMess не содержит адрес сервера"

    [ -n "$BRORAY_PORT" ] ||
        broray_die "VMess не содержит порт"

    [ -n "$BRORAY_UUID" ] ||
        broray_die "VMess не содержит UUID"

    [ -n "$BRORAY_NAME" ] ||
        BRORAY_NAME="$BRORAY_ADDRESS:$BRORAY_PORT"
}
