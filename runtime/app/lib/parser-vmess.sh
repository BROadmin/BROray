#!/opt/bin/ash

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"

. "$BRORAY_BASE/lib/util.sh"

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
    BRORAY_RAW_HEADER=""
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

    modern_authority="${uri%%#*}"
    modern_authority="${modern_authority%%\?*}"
    case "$modern_authority" in *@*) broray_parse_vmess_modern "$uri"; return $? ;; esac
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

    jq -e '
      . as $s |
      all(["allowInsecure","allow_insecure","insecure"][];
        . as $k | ($s|has($k)|not) or ($s[$k] == false or $s[$k] == 0 or $s[$k] == "0" or $s[$k] == "false" or $s[$k] == "")) and
      ((has("extra")|not) or (.extra|type)=="object") and
      all(["ech","pcs","vcn","pqv"][]; . as $k | ($s|has($k)|not) or ($s[$k]|type)=="string") and
      ((has("fm")|not) or (.fm|type)=="object")
    ' "$decoded_file" >/dev/null 2>&1 ||
        broray_die "VMess: неподдерживаемый insecure или неправильные extra/TLS/FinalMask"
    BRORAY_ECH="$(jq -r '.ech // ""' "$decoded_file")"
    BRORAY_PCS="$(jq -r '.pcs // ""' "$decoded_file")"
    BRORAY_VCN="$(jq -r '.vcn // ""' "$decoded_file")"
    BRORAY_PQV="$(jq -r '.pqv // ""' "$decoded_file")"
    BRORAY_STREAM_MASK="$(jq -c '.fm // {}' "$decoded_file")"
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
            # jq in the Keenetic Entware feed is built without Oniguruma.
            # This is the exact Unicode White_Space set matched by the former
            # Oniguruma \s expression; trimming it by code point preserves the
            # accepted VMess input language without a regex dependency.
            def broray_alpn_space:
                . == 9 or . == 10 or . == 11 or . == 12 or . == 13 or
                . == 32 or . == 133 or . == 160 or . == 5760 or
                (. >= 8192 and . <= 8202) or . == 8232 or . == 8233 or
                . == 8239 or . == 8287 or . == 12288;
            def broray_alpn_ltrim_codes:
                if length == 0 or (.[0] | broray_alpn_space | not) then .
                else .[1:] | broray_alpn_ltrim_codes
                end;
            def broray_alpn_trim:
                explode |
                broray_alpn_ltrim_codes |
                reverse | broray_alpn_ltrim_codes | reverse |
                implode;
            if (.alpn | type) == "array" then
                .alpn
            elif (.alpn | type) == "string" and
                 (.alpn | length) > 0 then
                (.alpn | split(",") |
                    map(broray_alpn_trim) |
                    map(select(length > 0)))
            else
                []
            end
        ' "$decoded_file"
    )"

    BRORAY_ALLOW_INSECURE=false

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
            '.mode // (if (.net // .network) == "grpc" then (if .type == "multi" then "multi" else "gun" end) elif (.net // .network) == "xhttp" or (.net // .network) == "splithttp" then (if .type == null or .type == "" or .type == "none" then "auto" else .type end) else "auto" end)' \
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

    if [ "$BRORAY_NETWORK" = grpc ]; then
        case "$BRORAY_HEADER_TYPE" in
            multi|gun)
                [ "$BRORAY_MODE" = "$BRORAY_HEADER_TYPE" ] || broray_die "VMess gRPC: mode и type различаются"
                BRORAY_HEADER_TYPE=none ;;
            none) ;;
            *) broray_die "неподдерживаемый VMess gRPC type" ;;
        esac
        case "$BRORAY_MODE" in gun|multi) ;; *) broray_die "неподдерживаемый VMess gRPC mode" ;; esac
    fi
    case "$BRORAY_NETWORK" in
        xhttp|splithttp)
            case "$BRORAY_MODE" in auto|packet-up|stream-up|stream-one) ;; *) broray_die "неподдерживаемый VMess XHTTP mode" ;; esac
            case "$BRORAY_HEADER_TYPE" in
                none|"") ;;
                *) [ "$BRORAY_HEADER_TYPE" = "$BRORAY_MODE" ] || broray_die "VMess XHTTP: mode и type различаются" ;;
            esac
            BRORAY_HEADER_TYPE=none
            ;;
        *) [ "$BRORAY_EXTRA" = '{}' ] || broray_die "VMess extra поддерживается только для XHTTP" ;;
    esac
    BRORAY_KCP='{}'; BRORAY_KCP_SEED=''; BRORAY_KCP_LEGACY=false
    if [ "$BRORAY_NETWORK" = kcp ]; then
        jq -e 'has("congestion") or has("readBufferSize") or has("writeBufferSize")' "$decoded_file" >/dev/null 2>&1 && broray_die "VMess mKCP: устаревшие параметры congestion/readBufferSize/writeBufferSize не поддерживаются текущим Xray"
        jq -e '(.seed == null or (.seed|type)=="string") and (.path == null or (.path|type)=="string") and (.seed == null or .path == null or .seed == .path)' "$decoded_file" >/dev/null 2>&1 || broray_die "VMess mKCP: seed и path должны совпадать"
        BRORAY_KCP_LEGACY=true
        BRORAY_KCP_SEED="$(jq -r '.seed // .path // ""' "$decoded_file")"
        BRORAY_KCP="$(jq -c 'with_entries(select(.key as $k | ["mtu","tti","uplinkCapacity","downlinkCapacity","cwndMultiplier","maxSendingWindow"] | index($k) != null)) | with_entries(.value |= (if type == "string" then tonumber else . end))' "$decoded_file")" || broray_die "неправильные параметры VMess mKCP"
    fi
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
        raw|kcp)
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

broray_parse_vmess_modern() {
    local vm_body="${1#vmess://}" vm_fragment='' vm_endpoint vm_query='' vm_clean=''
    local vm_item vm_key vm_rest vm_cipher vm_mode
    case "$vm_body" in *'#'*) vm_fragment="${vm_body#*#}"; vm_body="${vm_body%%#*}" ;; esac
    case "$vm_body" in *'?'*) vm_query="${vm_body#*\?}"; vm_endpoint="${vm_body%%\?*}" ;; *) vm_endpoint="$vm_body" ;; esac
    vm_cipher="$(broray_uri_query_value encryption "$vm_query")" || return 1
    if [ -z "$vm_cipher" ]; then
        broray_uri_query_has encryption "$vm_query" && broray_die "VMess encryption не может быть пустым"
        vm_cipher=auto
    fi
    case "$vm_cipher" in auto|aes-128-gcm|chacha20-poly1305|none) ;; *) broray_die "неподдерживаемый VMess encryption" ;; esac
    vm_rest="$vm_query"
    while [ -n "$vm_rest" ]; do
        vm_item="${vm_rest%%&*}"
        case "$vm_rest" in *'&'*) vm_rest="${vm_rest#*&}" ;; *) vm_rest="" ;; esac
        vm_key="$(broray_uri_component_decode "${vm_item%%=*}")" || return 1
        case "$vm_key" in
            encryption) continue ;;
            flow|aid|alterId) broray_die "современный VMess AEAD не поддерживает параметр $vm_key" ;;
        esac
        vm_clean="${vm_clean:+$vm_clean&}$vm_item"
    done
    . "$BRORAY_BASE/lib/parser-vless.sh"
    broray_parse_vless "vless://$vm_endpoint?$vm_clean#$vm_fragment" || return 1
    BRORAY_PROTOCOL=vmess; BRORAY_ALTER_ID=0; BRORAY_ENCRYPTION="$vm_cipher"
    if [ "$BRORAY_NETWORK" = grpc ]; then
        vm_mode="$(broray_uri_query_value mode "$vm_query")" || return 1
        BRORAY_MODE="${vm_mode:-gun}"
        case "$BRORAY_MODE" in gun|multi) ;; *) broray_die "неподдерживаемый VMess gRPC mode" ;; esac
    fi
}
