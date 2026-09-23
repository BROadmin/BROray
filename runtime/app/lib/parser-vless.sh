#!/opt/bin/ash

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"

. "$BRORAY_BASE/lib/util.sh"

broray_parse_vless() {
    uri="$1"

    [ -n "$uri" ] || broray_die "не указана ссылка VLESS"
    case "$uri" in vless://*) ;; *) broray_die "поддерживаются только ссылки vless://" ;; esac

    body="${uri#vless://}"
    fragment=""
    case "$body" in *#*) fragment="${body#*#}"; body="${body%%#*}" ;; esac
    query=""
    case "$body" in
        *\?*) query="${body#*\?}"; authority="${body%%\?*}" ;;
        *) authority="$body" ;;
    esac

    case "$authority" in
        */*)
            [ "/${authority#*/}" = / ] || broray_die "путь в ссылке VLESS не поддерживается"
            authority="${authority%%/*}"
            ;;
    esac
    encoded_id="${authority%@*}"
    hostport="${authority#*@}"
    [ "$encoded_id" != "$authority" ] || broray_die "не найден UUID"
    BRORAY_UUID="$(broray_uri_component_decode "$encoded_id")" || broray_die "неправильное кодирование VLESS ID"
    BRORAY_ADDRESS="${hostport%:*}"
    BRORAY_PORT="${hostport##*:}"

    BRORAY_NETWORK="$(broray_uri_query_value type "$query")" || return 1
    BRORAY_SECURITY="$(broray_uri_query_value security "$query")" || return 1
    BRORAY_ENCRYPTION="$(broray_uri_query_value encryption "$query")" || return 1
    BRORAY_FLOW="$(broray_uri_query_value flow "$query")" || return 1
    BRORAY_SNI="$(broray_uri_query_value sni "$query")" || return 1
    BRORAY_FP="$(broray_uri_query_value fp "$query")" || return 1
    BRORAY_PBK="$(broray_uri_query_value pbk "$query")" || return 1
    BRORAY_SID="$(broray_uri_query_value sid "$query")" || return 1
    BRORAY_SPX="$(broray_uri_query_value spx "$query")" || return 1
    BRORAY_HOST="$(broray_uri_query_value host "$query")" || return 1
    authority_value="$(broray_uri_query_value authority "$query")" || return 1
    BRORAY_PATH="$(broray_uri_query_value path "$query")" || return 1
    BRORAY_SERVICE_NAME="$(broray_uri_query_value serviceName "$query" service_name)" || return 1
    BRORAY_MODE="$(broray_uri_query_value mode "$query")" || return 1
    BRORAY_HEADER_TYPE="$(broray_uri_query_value headerType "$query")" || return 1
    BRORAY_EXTRA="$(broray_uri_query_value extra "$query")" || return 1
    BRORAY_NAME="$(broray_url_decode "$fragment")"

    alpn_value="$(broray_uri_query_value alpn "$query")" || return 1
    BRORAY_ALPN="$(jq -Rn --arg value "$alpn_value" '$value | split(",") | map(select(length > 0))')"
    insecure_value="$(broray_uri_query_value allowInsecure "$query")" || return 1
    case "$insecure_value" in
        1|true|TRUE|yes|YES) broray_die "неподдерживаемая настройка TLS allowInsecure" ;;
        ''|0|false|FALSE) BRORAY_ALLOW_INSECURE=false ;;
        *) broray_die "неподдерживаемая настройка TLS allowInsecure" ;;
    esac

    case "$BRORAY_NETWORK" in
        ''|tcp|raw) BRORAY_NETWORK=raw ;;
        ws|websocket) BRORAY_NETWORK=ws ;;
        grpc) BRORAY_NETWORK=grpc ;;
        httpupgrade|httpUpgrade) BRORAY_NETWORK=httpupgrade ;;
        xhttp|splithttp) BRORAY_NETWORK=xhttp ;;
        *) broray_die "неподдерживаемый транспорт VLESS: $BRORAY_NETWORK" ;;
    esac
    case "$BRORAY_SECURITY" in
        ''|none) BRORAY_SECURITY=none ;;
        tls|reality) ;;
        *) broray_die "неподдерживаемая защита VLESS: $BRORAY_SECURITY" ;;
    esac
    if [ "$BRORAY_NETWORK" = grpc ] && [ -n "$authority_value" ]; then
        [ -z "$BRORAY_HOST" ] || [ "$BRORAY_HOST" = "$authority_value" ] ||
            broray_die "VLESS gRPC: host и authority различаются"
        BRORAY_HOST="$authority_value"
    fi
    if [ "$BRORAY_SECURITY" = reality ]; then
        case "$BRORAY_NETWORK" in raw|grpc|xhttp) ;; *) broray_die "VLESS REALITY не поддерживает этот транспорт" ;; esac
    fi
    case "$BRORAY_FLOW" in
        ''|xtls-rprx-vision) ;;
        *) broray_die "неподдерживаемый режим VLESS flow: $BRORAY_FLOW" ;;
    esac
    if [ -n "$BRORAY_FLOW" ] &&
       { [ "$BRORAY_NETWORK" != raw ] || { [ "$BRORAY_SECURITY" != reality ] && [ "$BRORAY_SECURITY" != tls ]; }; }; then
        broray_die "VLESS flow поддерживается только для TCP/RAW + TLS/REALITY"
    fi

    [ -n "$BRORAY_FP" ] || BRORAY_FP=chrome
    [ -n "$BRORAY_PATH" ] || BRORAY_PATH=/
    [ -n "$BRORAY_MODE" ] || BRORAY_MODE=auto
    [ -n "$BRORAY_HEADER_TYPE" ] || BRORAY_HEADER_TYPE=none
    [ -n "$BRORAY_ENCRYPTION" ] || BRORAY_ENCRYPTION=none
    [ -n "$BRORAY_EXTRA" ] || BRORAY_EXTRA='{}'
    [ -n "$BRORAY_NAME" ] || BRORAY_NAME="$BRORAY_ADDRESS:$BRORAY_PORT"
    if [ "$BRORAY_SECURITY" = tls ] && [ -z "$BRORAY_SNI" ]; then BRORAY_SNI="$BRORAY_ADDRESS"; fi
}
