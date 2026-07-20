#!/bin/sh

. /opt/broray/lib/util.sh

broray_parse_trojan()
{
    uri="$1"

    [ -n "$uri" ] ||
        broray_die "не указана ссылка Trojan"

    case "$uri" in
        trojan://*)
            ;;
        *)
            broray_die \
                "поддерживаются только ссылки trojan://"
            ;;
    esac

    body="${uri#trojan://}"
    fragment=""

    case "$body" in
        *#*)
            fragment="${body#*#}"
            body="${body%%#*}"
            ;;
    esac

    query=""

    case "$body" in
        *\?*)
            query="${body#*\?}"
            endpoint="${body%%\?*}"
            ;;
        *)
            endpoint="$body"
            ;;
    esac

    BRORAY_PASSWORD="${endpoint%@*}"
    hostport="${endpoint#*@}"

    [ "$BRORAY_PASSWORD" != "$endpoint" ] ||
        broray_die "Trojan не содержит пароль"

    BRORAY_PASSWORD="$(
        broray_url_decode "$BRORAY_PASSWORD"
    )"

    BRORAY_ADDRESS="${hostport%:*}"
    BRORAY_PORT="${hostport##*:}"

    [ -n "$BRORAY_ADDRESS" ] ||
        broray_die "Trojan не содержит адрес сервера"

    [ -n "$BRORAY_PORT" ] ||
        broray_die "Trojan не содержит порт"

    BRORAY_PROTOCOL="trojan"

    BRORAY_NETWORK="$(
        broray_url_decode \
            "$(broray_query_value type "$query")"
    )"

    BRORAY_SECURITY="$(
        broray_url_decode \
            "$(broray_query_value security "$query")"
    )"

    BRORAY_SNI="$(
        broray_url_decode \
            "$(broray_query_value sni "$query")"
    )"

    BRORAY_FP="$(
        broray_url_decode \
            "$(broray_query_value fp "$query")"
    )"

    BRORAY_HOST="$(
        broray_url_decode \
            "$(broray_query_value authority "$query")"
    )"

    [ -n "$BRORAY_HOST" ] || {
        BRORAY_HOST="$(
            broray_url_decode \
                "$(broray_query_value host "$query")"
        )"
    }

    BRORAY_SERVICE_NAME="$(
        broray_url_decode \
            "$(broray_query_value serviceName "$query")"
    )"

    [ -n "$BRORAY_SERVICE_NAME" ] || {
        BRORAY_SERVICE_NAME="$(
            broray_url_decode \
                "$(broray_query_value service_name "$query")"
        )"
    }

    BRORAY_MODE="$(
        broray_url_decode \
            "$(broray_query_value mode "$query")"
    )"

    BRORAY_PATH="$(
        broray_url_decode \
            "$(broray_query_value path "$query")"
    )"

    alpn_text="$(
        broray_url_decode \
            "$(broray_query_value alpn "$query")"
    )"

    if [ -n "$alpn_text" ]; then
        BRORAY_ALPN="$(
            printf '%s' "$alpn_text" |
                tr ',' '\n' |
                sed \
                    -e 's/^[[:space:]]*//' \
                    -e 's/[[:space:]]*$//' \
                    -e '/^$/d' |
                jq -R -s '
                    split("\n") |
                    map(select(length > 0))
                '
        )"
    else
        BRORAY_ALPN='[]'
    fi

    allow_insecure="$(
        broray_url_decode \
            "$(broray_query_value allowInsecure "$query")"
    )"

    case "$allow_insecure" in
        1|true|TRUE|yes|YES)
            BRORAY_ALLOW_INSECURE="true"
            ;;
        *)
            BRORAY_ALLOW_INSECURE="false"
            ;;
    esac

    BRORAY_NAME="$(
        broray_url_decode "$fragment"
    )"

    [ -n "$BRORAY_NETWORK" ] ||
        BRORAY_NETWORK="raw"

    case "$BRORAY_NETWORK" in
        tcp|raw)
            BRORAY_NETWORK="raw"
            ;;
        grpc)
            BRORAY_NETWORK="grpc"
            ;;
        ws|websocket)
            BRORAY_NETWORK="ws"
            ;;
        httpupgrade|httpUpgrade)
            BRORAY_NETWORK="httpupgrade"
            ;;
        xhttp|splithttp)
            BRORAY_NETWORK="xhttp"
            ;;
        *)
            broray_die \
                "неподдерживаемый транспорт Trojan: $BRORAY_NETWORK"
            ;;
    esac

    [ -n "$BRORAY_SECURITY" ] ||
        BRORAY_SECURITY="tls"

    case "$BRORAY_SECURITY" in
        tls)
            ;;
        *)
            broray_die \
                "пока Trojan поддерживается только с TLS"
            ;;
    esac

    [ -n "$BRORAY_SNI" ] ||
        BRORAY_SNI="$BRORAY_ADDRESS"

    [ -n "$BRORAY_FP" ] ||
        BRORAY_FP="chrome"

    [ -n "$BRORAY_MODE" ] ||
        BRORAY_MODE="gun"

    [ -n "$BRORAY_PATH" ] ||
        BRORAY_PATH="/"

    [ -n "$BRORAY_NAME" ] ||
        BRORAY_NAME="$BRORAY_ADDRESS:$BRORAY_PORT"

    [ -n "$BRORAY_PASSWORD" ] ||
        broray_die "Trojan содержит пустой пароль"
}
