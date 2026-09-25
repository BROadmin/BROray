#!/opt/bin/ash

# Общие функции BROray.
# Файл подключается через:
# . /opt/broray/lib/util.sh

broray_die() {
    echo "Ошибка: $*" >&2
    exit 1
}

broray_warn() {
    echo "Предупреждение: $*" >&2
}

broray_info() {
    echo "$*"
}

broray_require_command() {
    command_name="$1"

    command -v "$command_name" >/dev/null 2>&1 ||
        broray_die "не найдена обязательная команда: $command_name"
}

broray_require_file() {
    required_file="$1"

    [ -f "$required_file" ] ||
        broray_die "не найден обязательный файл: $required_file"
}

broray_url_decode() {
    encoded_value="$1"

    decoded_value="$(
        printf '%s' "$encoded_value" |
            sed 's/+/ /g; s/%/\\x/g'
    )"

    printf '%b' "$decoded_value"
}

broray_uri_component_decode() {
    local rest pair prefix
    [ "$#" = 1 ] || return 1
    # Validate the whole component before emitting any decoded bytes.
    rest="$1"
    while :; do
        case "$rest" in
            *%*) rest="${rest#*%}" ;;
            *) break ;;
        esac
        case "$rest" in
            [0-9a-fA-F][0-9a-fA-F]*) rest="${rest#??}" ;;
            *) echo 'Ошибка: некорректное percent-кодирование URI' >&2; return 1 ;;
        esac
    done
    rest="$1"
    while :; do
        case "$rest" in
            *%*)
                prefix="${rest%%\%*}"
                printf '%s' "$prefix"
                rest="${rest#*%}"
                pair="${rest%"${rest#??}"}"
                printf '%b' "\\x$pair"
                rest="${rest#??}"
                ;;
            *) printf '%s' "$rest"; return 0 ;;
        esac
    done
}

broray_uri_query_value() {
    local wanted rest item key value found result alias
    wanted="$1"; rest="$2"; shift 2
    found=0; result=""
    while [ -n "$rest" ]; do
        item="${rest%%&*}"
        case "$rest" in *'&'*) rest="${rest#*&}" ;; *) rest="" ;; esac
        key="$(broray_uri_component_decode "${item%%=*}")" || return 1
        for alias in "$wanted" "$@"; do
            [ "$key" = "$alias" ] || continue
            [ "$found" = 0 ] || {
                echo "Ошибка: повторяющийся параметр URI: $wanted" >&2
                return 1
            }
            found=1
            case "$item" in *=*) value="${item#*=}" ;; *) value="" ;; esac
            result="$(broray_uri_component_decode "$value")" || return 1
            break
        done
    done
    printf '%s' "$result"
}

broray_query_value() {
    query_key="$1"
    query_string="$2"

    printf '%s' "$query_string" |
        tr '&' '\n' |
        sed -n "s/^${query_key}=//p" |
        head -n 1
}

broray_timestamp() {
    date '+%Y%m%d-%H%M%S'
}

broray_json_validate() {
    json_file="$1"

    [ -f "$json_file" ] ||
        broray_die "JSON-файл не найден: $json_file"

    jq -e . "$json_file" >/dev/null 2>&1 ||
        broray_die "неправильный JSON: $json_file"
}

broray_check_dependencies() {
    broray_require_command jq
    broray_require_command sed
    broray_require_command tr
    broray_require_command head

    broray_require_file /opt/broray/runtime/xray
    broray_require_file /opt/broray/config/system/settings.json

    broray_json_validate /opt/broray/config/system/settings.json
}

# Supported Xray VLESS encryption public-key forms (no silent fallback).
broray_vless_encryption_valid() {
    jq -en --arg value "${1:-none}" '$value | (. == "none" or (
        split(".") as $p |
        ($p | length) >= 4 and $p[0] == "mlkem768x25519plus" and
        ($p[1] == "native" or $p[1] == "xorpub" or $p[1] == "random") and
        ($p[2] == "0rtt" or $p[2] == "1rtt") and
        (([range(3;($p|length)) | select(($p[.]|length) >= 20)][0]) as $key |
         $key != null and
         all($p[$key:][]; (length == 43 or length == 1579) and
             all(explode[]; (. >= 48 and . <= 57) or (. >= 65 and . <= 90) or (. >= 97 and . <= 122) or . == 45 or . == 95)) and
         ($p[3:$key] as $padding |
          if ($padding|length) == 0 then true else
            all($padding[]; length < 20 and (split("-") | length == 3 and
              all(.[]; length > 0 and all(explode[]; . >= 48 and . <= 57)))) and
            ($padding | map(split("-") | map(tonumber)) |
              all(.[]; all(.[]; . >= 0 and . <= 2147483647) and .[1] <= .[2]) and
              .[0][0] >= 100 and .[0][1] >= 35 and .[0][2] >= 35 and
              ([to_entries[] | select(.key % 2 == 0) | .value[2]] | add) <= 65553)
          end))))' >/dev/null 2>&1
}

# Current Xray stream fields shared by URI parsers; initialize on EVERY node.
broray_parse_stream_extensions() {
    broray_parse_kcp_options "$1" || return 1
    BRORAY_RAW_HEADER="$(broray_uri_query_value header "$1")" || return 1
    if broray_uri_query_has header "$1"; then
        [ "$BRORAY_NETWORK" = raw ] && [ -n "$BRORAY_RAW_HEADER" ] || return 1
        for raw_key in host path headerType; do
            if broray_uri_query_has "$raw_key" "$1"; then
                broray_die "header нельзя сочетать с host/path/headerType"
            fi
        done
    fi
    BRORAY_ECH="$(broray_uri_query_value ech "$1")" || return 1
    BRORAY_PCS="$(broray_uri_query_value pcs "$1")" || return 1
    BRORAY_VCN="$(broray_uri_query_value vcn "$1")" || return 1
    BRORAY_PQV="$(broray_uri_query_value pqv "$1")" || return 1
    BRORAY_STREAM_MASK='{}'
    if [ "$BRORAY_NETWORK" != hysteria ]; then
        BRORAY_STREAM_MASK="$(broray_uri_query_value fm "$1")" || return 1
        [ -n "$BRORAY_STREAM_MASK" ] || BRORAY_STREAM_MASK='{}'
    fi
}

# The same validation is used before saving and before generating a config.
# No regex dependency: Entware jq can be built without Oniguruma.
broray_server_stream_fields_valid() {
    jq -e '
      def text: type == "string" and all(explode[]; . >= 32 and . != 127);
      def hexpin: type == "string" and length == 64 and all(explode[];
        (. >= 48 and . <= 57) or (. >= 97 and . <= 102));
      def urlkey: type == "string" and length == 2603 and all(explode[];
        (. >= 48 and . <= 57) or (. >= 65 and . <= 90) or (. >= 97 and . <= 122) or . == 45 or . == 95);
      def kcp_options:
        type == "object" and
        ((keys - ["mtu","tti","uplinkCapacity","downlinkCapacity","cwndMultiplier","maxSendingWindow"]) | length == 0) and
        all(.[]; type == "number" and . == floor and . >= 0 and . <= 4294967295) and
        ((.mtu // 1350) >= 21 and (.mtu // 1350) <= 65507) and
        ((.tti // 20) >= 10 and (.tti // 20) <= 1000) and
        ((.cwndMultiplier // 1) >= 1) and
        ((.maxSendingWindow // 1048576) >= (.mtu // 1350));
      def mask:
        type == "object" and ((keys - ["tcp","udp","quicParams"]) | length == 0) and
        ((.tcp // []) | type == "array" and all(.[];
          type == "object" and ((keys - ["type","settings"]) | length == 0) and
          (.type as $t | ["header-custom","fragment","sudoku","xmc"] | index($t) != null) and
          ((.settings // {}) | type == "object"))) and
        ((.udp // []) | type == "array" and all(.[];
          type == "object" and ((keys - ["type","settings"]) | length == 0) and
          (.type as $t | ["header-custom","mkcp-legacy","noise","salamander","sudoku","xdns","xicmp","realm","udphop"] | index($t) != null) and
          ((.settings // {}) | type == "object")));
      . as $s |
      (if .protocol == "hysteria2" then
         (.auth | text) and
         ((.hysteria.obfs // "") as $o | ["","salamander","gecko"] | index($o) != null) and
         ((.hysteria.obfsPassword // "") | text) and
         (((.hysteria.obfs // "") == "") == ((.hysteria.obfsPassword // "") == "")) and
         ((.hysteria.finalMask // {}) | mask) and
         ((.hysteria.obfs // "") == "" or all((.hysteria.finalMask.udp // [])[]; .type != "salamander")) and
         ((.hysteria.ports // "") == "" or (
           all((.hysteria.finalMask.udp // [])[]; .type != "udphop") and
           (.hysteria.ports | type == "string" and
             (split(",") | length > 0 and length <= 128 and all(.[];
               split("-") | (length == 1 or length == 2) and all(.[];
                 length > 0 and length <= 5 and all(explode[]; . >= 48 and . <= 57)) and
               (map(tonumber) | all(.[]; . >= 1 and . <= 65535) and (length == 1 or .[0] <= .[1]))))) and
           (.port == (.hysteria.ports | split(",")[0] | split("-")[0] | tonumber))))
       else true end) and
      (.tls.allowInsecure != true) and
      (if .protocol == "vless" or .protocol == "vmess" or .protocol == "trojan" then
         (if .network == "xhttp" then
            ((.transport.mode // "auto") as $m | ["auto","packet-up","stream-up","stream-one"] | index($m) != null)
          elif .network == "grpc" then
            ((.transport.mode // "gun") as $m | ["gun","multi","auto"] | index($m) != null)
          else true end) and
         (.network == "xhttp" or ((.transport.extra // {}) | length) == 0) and
         (if .network == "raw" then
            (.transport.headerType == null or .transport.headerType == "none" or .transport.headerType == "http")
          elif .network == "kcp" then true
          else (.transport.headerType == null or .transport.headerType == "none") end)
       else true end) and
      (if .network == "kcp" then
         (.security == "none" or .security == "tls") and
         ((.transport.kcp // {}) | kcp_options) and
         ((.transport.kcpSeed // "") | text) and
         ((.transport.kcpLegacy // false) | type == "boolean") and
         ((.transport.headerType // "none") as $h | ["none","srtp","utp","wechat-video","wechat","dtls","wireguard","dns"] | index($h) != null) and
         ((.transport.host // "") == "" or .transport.headerType == "dns") and
         ((.transport.kcpLegacy // false) == false or ((.transport.finalMask // {}) | length) == 0)
       else true end) and
      all(["echConfigList","verifyPeerCertByName","pinnedPeerCertSha256"][];
        . as $k | ($s.tls | has($k) | not) or
        ($s.security == "tls" and ($s.tls[$k] | text))) and
      ((.tls.pinnedPeerCertSha256 // "") == "" or
        (.tls.pinnedPeerCertSha256 | split(",") | all(.[]; hexpin))) and
      ((.reality.mldsa65Verify // "") == "" or
        (.security == "reality" and (.reality.mldsa65Verify | urlkey))) and
      ((.transport | has("finalMask") | not) or (.transport.finalMask | mask))
    ' "$1" >/dev/null 2>&1 || return 1
    jq -es --arg validate_model yes --argjson max_nodes 1 \
        -f "$BRORAY_BASE/lib/subscription-xray-json.jq" "$1" >/dev/null 2>&1
}

broray_uri_query_has() {
    local wanted="$1" rest="$2" item key
    while [ -n "$rest" ]; do
        item="${rest%%&*}"
        case "$rest" in *'&'*) rest="${rest#*&}" ;; *) rest="" ;; esac
        key="$(broray_uri_component_decode "${item%%=*}")" || return 2
        [ "$key" != "$wanted" ] || return 0
    done
    return 1
}

broray_parse_kcp_options() {
    local kcp_query="$1" key value
    BRORAY_KCP='{}'; BRORAY_KCP_SEED=''; BRORAY_KCP_LEGACY=false
    for key in congestion readBufferSize writeBufferSize; do
        if broray_uri_query_has "$key" "$kcp_query"; then
            broray_die "параметр mKCP $key не поддерживается текущим Xray"
        fi
    done
    if [ "$BRORAY_NETWORK" != kcp ]; then
        for key in seed mtu tti uplinkCapacity downlinkCapacity cwndMultiplier maxSendingWindow; do
            if broray_uri_query_has "$key" "$kcp_query"; then
                broray_die "параметр $key поддерживается только для mKCP"
            fi
        done
        return 0
    fi
    BRORAY_KCP_SEED="$(broray_uri_query_value seed "$kcp_query")" || return 1
    if broray_uri_query_has seed "$kcp_query" || broray_uri_query_has headerType "$kcp_query"; then
        BRORAY_KCP_LEGACY=true
    fi
    for key in mtu tti uplinkCapacity downlinkCapacity cwndMultiplier maxSendingWindow; do
        value="$(broray_uri_query_value "$key" "$kcp_query")" || return 1
        [ -n "$value" ] || continue
        case "$value" in *[!0-9]*) broray_die "mKCP: параметр $key должен быть целым числом" ;; esac
        BRORAY_KCP="$(printf '%s' "$BRORAY_KCP" | jq -c --arg k "$key" --arg v "$value" '.[$k]=($v|tonumber)')" || return 1
    done
}
