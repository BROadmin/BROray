#!/bin/sh

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"

. "$BRORAY_BASE/lib/util.sh"

broray_hy2_urldecode()
{
    encoded_value="$1"

    escaped_value="$(
        printf '%s' "$encoded_value" |
            sed \
                -e 's/+/ /g' \
                -e 's/%/\\x/g'
    )"

    printf '%b' "$escaped_value"
}

broray_hy2_query_value()
{
    query_string="$1"
    requested_key="$2"

    old_ifs="$IFS"
    IFS='&'

    for query_item in $query_string; do
        query_key="${query_item%%=*}"

        if [ "$query_item" = "$query_key" ]; then
            query_value=""
        else
            query_value="${query_item#*=}"
        fi

        decoded_key="$(
            broray_hy2_urldecode "$query_key"
        )"

        if [ "$decoded_key" = "$requested_key" ]; then
            broray_hy2_urldecode "$query_value"
            IFS="$old_ifs"
            return 0
        fi
    done

    IFS="$old_ifs"
    return 1
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
        broray_hy2_urldecode "$encoded_auth"
    )"

    [ -n "$BRORAY_AUTH" ] ||
        broray_die \
            "Hysteria2 auth не может быть пустым"

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
        *)
            broray_die \
                "в ссылке Hysteria2 отсутствует порт"
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
            broray_hy2_urldecode "$encoded_name"
        )"
    else
        BRORAY_NAME="$BRORAY_ADDRESS"
    fi

    BRORAY_SECURITY="$(
        broray_hy2_query_value \
            "$query_string" \
            "security" 2>/dev/null ||
            printf '%s' "tls"
    )"

    [ -n "$BRORAY_SECURITY" ] ||
        BRORAY_SECURITY="tls"

    BRORAY_SNI="$(
        broray_hy2_query_value \
            "$query_string" \
            "sni" 2>/dev/null ||
            true
    )"

    [ -n "$BRORAY_SNI" ] ||
        BRORAY_SNI="$(
            broray_hy2_query_value \
                "$query_string" \
                "peer" 2>/dev/null ||
                true
        )"

    [ -n "$BRORAY_SNI" ] ||
        BRORAY_SNI="$BRORAY_ADDRESS"

    BRORAY_FP="$(
        broray_hy2_query_value \
            "$query_string" \
            "fp" 2>/dev/null ||
            printf '%s' "chrome"
    )"

    [ -n "$BRORAY_FP" ] ||
        BRORAY_FP="chrome"

    alpn_value="$(
        broray_hy2_query_value \
            "$query_string" \
            "alpn" 2>/dev/null ||
            true
    )"

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

    BRORAY_OBFS="$(
        broray_hy2_query_value \
            "$query_string" \
            "obfs" 2>/dev/null ||
            true
    )"

    BRORAY_OBFS_PASSWORD="$(
        broray_hy2_query_value \
            "$query_string" \
            "obfs-password" 2>/dev/null ||
            true
    )"

    BRORAY_UP_MBPS="$(
        broray_hy2_query_value \
            "$query_string" \
            "upmbps" 2>/dev/null ||
            true
    )"

    BRORAY_DOWN_MBPS="$(
        broray_hy2_query_value \
            "$query_string" \
            "downmbps" 2>/dev/null ||
            true
    )"

    insecure_value="$(
        broray_hy2_query_value \
            "$query_string" \
            "insecure" 2>/dev/null ||
            true
    )"

    case "$insecure_value" in
        1|true|TRUE|yes|YES)
            BRORAY_ALLOW_INSECURE="true"
            ;;
        *)
            BRORAY_ALLOW_INSECURE="false"
            ;;
    esac

    BRORAY_FINAL_MASK_RAW="$(
        broray_hy2_query_value \
            "$query_string" \
            "fm" 2>/dev/null ||
            true
    )"

    if [ -n "$BRORAY_FINAL_MASK_RAW" ] &&
       printf '%s' "$BRORAY_FINAL_MASK_RAW" |
            jq -e 'type == "object"' >/dev/null 2>&1
    then
        BRORAY_FINAL_MASK="$BRORAY_FINAL_MASK_RAW"
    else
        BRORAY_FINAL_MASK='{}'
    fi

    BRORAY_NETWORK="hysteria"
}
