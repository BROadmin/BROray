#!/bin/sh

. /opt/broray/lib/util.sh

broray_parse_vless() {

    uri="$1"

    [ -n "$uri" ] || broray_die "не указана ссылка VLESS"

    case "$uri" in
        vless://*) ;;
        *) broray_die "поддерживаются только ссылки vless://" ;;
    esac

    body="${uri#vless://}"

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
            authority="${body%%\?*}"
            ;;
        *)
            authority="$body"
            ;;
    esac

    BRORAY_UUID="${authority%@*}"
    hostport="${authority#*@}"

    [ "$BRORAY_UUID" != "$authority" ] ||
        broray_die "не найден UUID"

    BRORAY_ADDRESS="${hostport%:*}"
    BRORAY_PORT="${hostport##*:}"

    BRORAY_NETWORK="$(broray_url_decode "$(broray_query_value type "$query")")"
    BRORAY_SECURITY="$(broray_url_decode "$(broray_query_value security "$query")")"

    BRORAY_SNI="$(broray_url_decode "$(broray_query_value sni "$query")")"
    BRORAY_FP="$(broray_url_decode "$(broray_query_value fp "$query")")"
    BRORAY_PBK="$(broray_url_decode "$(broray_query_value pbk "$query")")"
    BRORAY_SID="$(broray_url_decode "$(broray_query_value sid "$query")")"
    BRORAY_SPX="$(broray_url_decode "$(broray_query_value spx "$query")")"

    BRORAY_PATH="$(broray_url_decode "$(broray_query_value path "$query")")"
    BRORAY_MODE="$(broray_url_decode "$(broray_query_value mode "$query")")"
    BRORAY_EXTRA="$(broray_url_decode "$(broray_query_value extra "$query")")"

    BRORAY_NAME="$(broray_url_decode "$fragment")"

    [ -n "$BRORAY_FP" ] || BRORAY_FP="chrome"
    [ -n "$BRORAY_PATH" ] || BRORAY_PATH="/"
    [ -n "$BRORAY_MODE" ] || BRORAY_MODE="auto"

    [ -n "$BRORAY_NAME" ] ||
        BRORAY_NAME="$BRORAY_ADDRESS:$BRORAY_PORT"

    [ -n "$BRORAY_EXTRA" ] ||
        BRORAY_EXTRA='{}'
}
