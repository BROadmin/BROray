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

    BRORAY_PASSWORD="${endpoint%@*}"
    hostport="${endpoint#*@}"

    [ "$BRORAY_PASSWORD" != "$endpoint" ] ||
        broray_die "Trojan не содержит пароль"

    BRORAY_PASSWORD="$(
        broray_uri_component_decode "$BRORAY_PASSWORD"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_ADDRESS="${hostport%:*}"
    BRORAY_PORT="${hostport##*:}"

    [ -n "$BRORAY_ADDRESS" ] ||
        broray_die "Trojan не содержит адрес сервера"

    [ -n "$BRORAY_PORT" ] ||
        broray_die "Trojan не содержит порт"

    BRORAY_PROTOCOL="trojan"

    BRORAY_NETWORK="$(
        broray_uri_component_decode \
            "$(broray_query_value type "$query")"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_SECURITY="$(
        broray_uri_component_decode \
            "$(broray_query_value security "$query")"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_SNI="$(
        broray_uri_component_decode \
            "$(broray_query_value sni "$query")"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_FP="$(
        broray_uri_component_decode \
            "$(broray_query_value fp "$query")"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_HOST="$(
        broray_uri_component_decode \
            "$(broray_query_value authority "$query")"
    )" || broray_die "неправильное кодирование URI Trojan"

    [ -n "$BRORAY_HOST" ] || {
        BRORAY_HOST="$(
            broray_uri_component_decode \
                "$(broray_query_value host "$query")"
        )" || broray_die "неправильное кодирование URI Trojan"
    }

    BRORAY_SERVICE_NAME="$(
        broray_uri_component_decode \
            "$(broray_query_value serviceName "$query")"
    )" || broray_die "неправильное кодирование URI Trojan"

    [ -n "$BRORAY_SERVICE_NAME" ] || {
        BRORAY_SERVICE_NAME="$(
            broray_uri_component_decode \
                "$(broray_query_value service_name "$query")"
        )" || broray_die "неправильное кодирование URI Trojan"
    }

    BRORAY_MODE="$(
        broray_uri_component_decode \
            "$(broray_query_value mode "$query")"
    )" || broray_die "неправильное кодирование URI Trojan"

    BRORAY_PATH="$(
        broray_uri_component_decode \
            "$(broray_query_value path "$query")"
    )" || broray_die "неправильное кодирование URI Trojan"

    alpn_text="$(
        broray_uri_component_decode \
            "$(broray_query_value alpn "$query")"
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
        broray_uri_component_decode \
            "$(broray_query_value allowInsecure "$query")"
    )" || broray_die "неправильное кодирование URI Trojan"

    case "$allow_insecure" in
        1|true|TRUE|yes|YES)
            broray_die "неподдерживаемая настройка TLS allowInsecure"
            ;;
        *)
            BRORAY_ALLOW_INSECURE="false"
            ;;
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
}
