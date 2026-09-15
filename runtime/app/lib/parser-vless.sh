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

    BRORAY_UUID="${authority%@*}"
    hostport="${authority#*@}"
    [ "$BRORAY_UUID" != "$authority" ] || broray_die "не найден UUID"
    BRORAY_ADDRESS="${hostport%:*}"
    BRORAY_PORT="${hostport##*:}"

    BRORAY_NETWORK="$(broray_url_decode "$(broray_query_value type "$query")")"
    BRORAY_SECURITY="$(broray_url_decode "$(broray_query_value security "$query")")"
    BRORAY_ENCRYPTION="$(broray_url_decode "$(broray_query_value encryption "$query")")"
    BRORAY_FLOW="$(broray_url_decode "$(broray_query_value flow "$query")")"
    BRORAY_SNI="$(broray_url_decode "$(broray_query_value sni "$query")")"
    BRORAY_FP="$(broray_url_decode "$(broray_query_value fp "$query")")"
    BRORAY_PBK="$(broray_url_decode "$(broray_query_value pbk "$query")")"
    BRORAY_SID="$(broray_url_decode "$(broray_query_value sid "$query")")"
    BRORAY_SPX="$(broray_url_decode "$(broray_query_value spx "$query")")"
    BRORAY_HOST="$(broray_url_decode "$(broray_query_value host "$query")")"
    BRORAY_PATH="$(broray_url_decode "$(broray_query_value path "$query")")"
    BRORAY_SERVICE_NAME="$(broray_url_decode "$(broray_query_value serviceName "$query")")"
    [ -n "$BRORAY_SERVICE_NAME" ] || BRORAY_SERVICE_NAME="$(broray_url_decode "$(broray_query_value service_name "$query")")"
    BRORAY_MODE="$(broray_url_decode "$(broray_query_value mode "$query")")"
    BRORAY_HEADER_TYPE="$(broray_url_decode "$(broray_query_value headerType "$query")")"
    BRORAY_EXTRA="$(broray_url_decode "$(broray_query_value extra "$query")")"
    BRORAY_NAME="$(broray_url_decode "$fragment")"

    alpn_value="$(broray_url_decode "$(broray_query_value alpn "$query")")"
    BRORAY_ALPN="$(jq -Rn --arg value "$alpn_value" '$value | split(",") | map(select(length > 0))')"
    insecure_value="$(broray_url_decode "$(broray_query_value allowInsecure "$query")")"
    case "$insecure_value" in 1|true|TRUE|yes) BRORAY_ALLOW_INSECURE=true ;; *) BRORAY_ALLOW_INSECURE=false ;; esac

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
    case "$BRORAY_FLOW" in
        ''|xtls-rprx-vision) ;;
        *) broray_die "неподдерживаемый режим VLESS flow: $BRORAY_FLOW" ;;
    esac
    if [ -n "$BRORAY_FLOW" ] &&
       { [ "$BRORAY_NETWORK" != raw ] || [ "$BRORAY_SECURITY" != reality ]; }; then
        broray_die "VLESS flow поддерживается только для TCP/RAW + REALITY"
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
