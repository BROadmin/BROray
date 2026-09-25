#!/opt/bin/ash

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"

. "$BRORAY_BASE/lib/util.sh"

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

    case "$endpoint" in
        */*)
            [ "/${endpoint#*/}" = / ] || broray_die "путь в ссылке Trojan не поддерживается"
            endpoint="${endpoint%%/*}"
            ;;
    esac
    BRORAY_PASSWORD="${endpoint%@*}"
    hostport="${endpoint#*@}"

    [ "$BRORAY_PASSWORD" != "$endpoint" ] ||
        broray_die "Trojan не содержит пароль"

    BRORAY_PASSWORD="$(
        broray_uri_component_decode "$BRORAY_PASSWORD"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_ADDRESS="${hostport%:*}"
    BRORAY_PORT="${hostport##*:}"
    case "$BRORAY_ADDRESS" in
        \[*\]) BRORAY_ADDRESS="${BRORAY_ADDRESS#\[}"; BRORAY_ADDRESS="${BRORAY_ADDRESS%\]}" ;;
    esac

    [ -n "$BRORAY_ADDRESS" ] ||
        broray_die "Trojan не содержит адрес сервера"

    [ -n "$BRORAY_PORT" ] ||
        broray_die "Trojan не содержит порт"

    BRORAY_PROTOCOL="trojan"

    BRORAY_NETWORK="$(
        broray_uri_query_value type "$query"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_SECURITY="$(
        broray_uri_query_value security "$query"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_SNI="$(
        broray_uri_query_value sni "$query"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_FP="$(
        broray_uri_query_value fp "$query"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_HOST="$(broray_uri_query_value host "$query")" || return 1
    authority_value="$(broray_uri_query_value authority "$query")" || return 1
    BRORAY_SERVICE_NAME="$(broray_uri_query_value serviceName "$query" service_name)" || return 1
    BRORAY_HEADER_TYPE="$(broray_uri_query_value headerType "$query")" || return 1
    [ -n "$BRORAY_HEADER_TYPE" ] || BRORAY_HEADER_TYPE=none

    BRORAY_MODE="$(
        broray_uri_query_value mode "$query"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_PATH="$(
        broray_uri_query_value path "$query"
    )" || broray_die "неправильное кодирование URI Trojan"

    alpn_text="$(
        broray_uri_query_value alpn "$query"
    )" || broray_die "неправильное кодирование URI Trojan"

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
        broray_uri_query_value allowInsecure "$query" insecure
    )" || broray_die "неправильное кодирование URI Trojan"

    case "$allow_insecure" in
        1|true|TRUE|yes|YES)
            broray_die "неподдерживаемая настройка TLS allowInsecure"
            ;;
        ''|0|false|FALSE)
            BRORAY_ALLOW_INSECURE="false"
            ;;
        *) broray_die "неподдерживаемая настройка TLS allowInsecure" ;;
    esac

    BRORAY_NAME="$(
        broray_uri_component_decode "$fragment"
    )" || broray_die "неправильное кодирование URI Trojan"

    [ -n "$BRORAY_NETWORK" ] ||
        BRORAY_NETWORK="raw"

    case "$BRORAY_NETWORK" in
        tcp|raw)
            BRORAY_NETWORK="raw"
            ;;
        kcp)
            BRORAY_NETWORK="kcp"
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

    if [ "$BRORAY_NETWORK" = grpc ] && [ -n "$authority_value" ]; then
        [ -z "$BRORAY_HOST" ] || [ "$BRORAY_HOST" = "$authority_value" ] ||
            broray_die "Trojan gRPC: host и authority различаются"
        BRORAY_HOST="$authority_value"
    elif [ -n "$authority_value" ]; then
        broray_die "Trojan authority поддерживается только для gRPC"
    fi
    case "$BRORAY_HEADER_TYPE:$BRORAY_NETWORK" in
        none:*|http:raw|srtp:kcp|utp:kcp|wechat-video:kcp|wechat:kcp|dtls:kcp|wireguard:kcp|dns:kcp) ;;
        *) broray_die "неподдерживаемый Trojan headerType для этого транспорта" ;;
    esac
    BRORAY_PBK="$(broray_uri_query_value pbk "$query")" || return 1
    BRORAY_SID="$(broray_uri_query_value sid "$query")" || return 1
    BRORAY_SPX="$(broray_uri_query_value spx "$query")" || return 1
    [ -n "$BRORAY_SECURITY" ] ||
        BRORAY_SECURITY="tls"

    case "$BRORAY_SECURITY" in
        tls) ;;
        reality)
            case "$BRORAY_NETWORK" in raw|grpc|xhttp) ;; *) broray_die "Trojan REALITY не поддерживает этот транспорт" ;; esac
            [ -n "$BRORAY_SNI" ] && [ -n "$BRORAY_PBK" ] || broray_die "Trojan REALITY требует SNI и public key"
            ;;
        *)
            broray_die "неподдерживаемая защита Trojan"
            ;;
    esac

    [ -n "$BRORAY_SNI" ] ||
        BRORAY_SNI="$BRORAY_ADDRESS"

    [ -n "$BRORAY_FP" ] ||
        BRORAY_FP="chrome"

    case "$BRORAY_NETWORK" in
        grpc)
            [ -n "$BRORAY_MODE" ] || BRORAY_MODE=gun
            case "$BRORAY_MODE" in gun|multi) ;; *) broray_die "неподдерживаемый режим Trojan gRPC" ;; esac
            ;;
        xhttp)
            [ -n "$BRORAY_MODE" ] || BRORAY_MODE=auto
            case "$BRORAY_MODE" in auto|packet-up|stream-up|stream-one) ;; *) broray_die "неподдерживаемый режим Trojan XHTTP" ;; esac
            ;;
    esac

    BRORAY_EXTRA="$(broray_uri_query_value extra "$query")" || broray_die "неправильный параметр Trojan extra"
    local extra_query extra_item extra_key
    extra_query="$query"
    while [ -n "$extra_query" ]; do
        extra_item="${extra_query%%&*}"
        case "$extra_query" in *'&'*) extra_query="${extra_query#*&}" ;; *) extra_query="" ;; esac
        extra_key="$(broray_uri_component_decode "${extra_item%%=*}")" || broray_die "неправильный параметр Trojan extra"
        [ "$extra_key" != extra ] || [ "$BRORAY_NETWORK" = xhttp ] ||
            broray_die "Trojan extra поддерживается только для XHTTP"
    done
    [ -n "$BRORAY_EXTRA" ] || BRORAY_EXTRA='{}'
    printf '%s' "$BRORAY_EXTRA" | jq -e 'type == "object"' >/dev/null 2>&1 ||
        broray_die "Trojan extra должен быть JSON object"

    [ -n "$BRORAY_PATH" ] ||
        BRORAY_PATH="/"

    [ -n "$BRORAY_NAME" ] ||
        BRORAY_NAME="$BRORAY_ADDRESS:$BRORAY_PORT"

    [ -n "$BRORAY_PASSWORD" ] ||
        broray_die "Trojan содержит пустой пароль"
    broray_parse_stream_extensions "$query" || return 1

}
