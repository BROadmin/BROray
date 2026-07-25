#!/bin/sh

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"

. "$BRORAY_BASE/lib/util.sh"
. "$BRORAY_BASE/lib/parser-vless.sh"
. "$BRORAY_BASE/lib/server.sh"

BRORAY_SERVERS="$BRORAY_BASE/servers"
BRORAY_TMP="$BRORAY_BASE/tmp"

broray_server_generate_id() {
    source_type="$1"
    source_id="$2"
    source_index="$3"

    case "$source_type" in
        subscription)
            [ -n "$source_id" ] ||
                broray_die \
                    "не указан идентификатор подписки"

            case "$source_id" in
                *[!a-zA-Z0-9._-]*)
                    broray_die \
                        "некорректный идентификатор подписки"
                    ;;
            esac

            case "$source_index" in
                ''|*[!0-9]*)
                    broray_die \
                        "некорректный номер узла подписки"
                    ;;
            esac

            printf 'subscription-%s-%04d\n' \
                "$source_id" \
                "$source_index"
            ;;

        manual)
            timestamp="$(date '+%Y%m%d%H%M%S')"
            process_id="$$"

            printf 'manual-%s-%s\n' \
                "$timestamp" \
                "$process_id"
            ;;

        *)
            broray_die \
                "неподдерживаемый источник сервера"
            ;;
    esac
}

broray_server_save_parsed() {
    original_uri="$1"
    server_id="$2"
    source_type="$3"
    source_id="${4:-}"
    source_index="${5:-0}"

    broray_server_validate_id "$server_id"

    [ -n "$BRORAY_UUID" ] ||
        broray_die "парсер не вернул UUID"

    [ -n "$BRORAY_ADDRESS" ] ||
        broray_die "парсер не вернул адрес"

    [ -n "$BRORAY_PORT" ] ||
        broray_die "парсер не вернул порт"

    case "$BRORAY_PORT" in
        *[!0-9]*)
            broray_die "порт должен быть числом"
            ;;
    esac

    [ "$BRORAY_PORT" -ge 1 ] 2>/dev/null &&
    [ "$BRORAY_PORT" -le 65535 ] 2>/dev/null ||
        broray_die \
            "порт должен находиться в диапазоне 1–65535"

    [ "$BRORAY_NETWORK" = "xhttp" ] ||
        broray_die \
            "пока поддерживается только транспорт XHTTP"

    [ "$BRORAY_SECURITY" = "reality" ] ||
        broray_die \
            "пока поддерживается только Reality"

    [ -n "$BRORAY_SNI" ] ||
        broray_die "не указан SNI"

    [ -n "$BRORAY_PBK" ] ||
        broray_die \
            "не указан Reality public key"

    printf '%s' "$BRORAY_EXTRA" |
        jq -e . >/dev/null 2>&1 ||
        broray_die \
            "параметр extra содержит неправильный JSON"

    mkdir -p \
        "$BRORAY_SERVERS" \
        "$BRORAY_TMP"

    server_file="$(
        broray_server_path "$server_id"
    )"

    temporary_file="$BRORAY_TMP/server-import.$$.json"

    jq -n \
        --arg id "$server_id" \
        --arg name "$BRORAY_NAME" \
        --arg uri "$original_uri" \
        --arg protocol "vless" \
        --arg address "$BRORAY_ADDRESS" \
        --argjson port "$BRORAY_PORT" \
        --arg uuid "$BRORAY_UUID" \
        --arg network "$BRORAY_NETWORK" \
        --arg security "$BRORAY_SECURITY" \
        --arg sourceType "$source_type" \
        --arg sourceId "$source_id" \
        --argjson sourceIndex "$source_index" \
        --arg sni "$BRORAY_SNI" \
        --arg fingerprint "$BRORAY_FP" \
        --arg publicKey "$BRORAY_PBK" \
        --arg shortId "$BRORAY_SID" \
        --arg spiderX "$BRORAY_SPX" \
        --arg path "$BRORAY_PATH" \
        --arg mode "$BRORAY_MODE" \
        --argjson extra "$BRORAY_EXTRA" \
        '{
            schemaVersion: 2,
            id: $id,
            name: $name,
            source: (
                if $sourceType == "subscription"
                then {
                    type: $sourceType,
                    subscriptionId: $sourceId,
                    nodeIndex: $sourceIndex
                }
                else {
                    type: $sourceType
                }
                end
            ),
            uri: $uri,
            protocol: $protocol,
            address: $address,
            port: $port,
            uuid: $uuid,
            network: $network,
            security: $security,
            reality: {
                serverName: $sni,
                fingerprint: $fingerprint,
                publicKey: $publicKey,
                shortId: $shortId,
                spiderX: $spiderX
            },
            xhttp: {
                path: $path,
                mode: $mode,
                extra: $extra
            }
        }' > "$temporary_file" ||
        broray_die \
            "не удалось создать файл сервера"

    broray_server_validate "$temporary_file"

    mv "$temporary_file" "$server_file" ||
        broray_die \
            "не удалось сохранить сервер"

    printf '%s\n' "$server_file"
}

broray_server_import_vless() {
    original_uri="$1"
    source_type="${2:-manual}"
    source_id="${3:-}"
    source_index="${4:-0}"

    [ -n "$original_uri" ] ||
        broray_die "не указана ссылка VLESS"

    case "$original_uri" in
        vless://*)
            ;;
        *)
            broray_die \
                "пока поддерживается импорт только VLESS"
            ;;
    esac

    broray_parse_vless "$original_uri"

    server_id="$(
        broray_server_generate_id \
            "$source_type" \
            "$source_id" \
            "$source_index"
    )"

    server_file="$(
        broray_server_save_parsed \
            "$original_uri" \
            "$server_id" \
            "$source_type" \
            "$source_id" \
            "$source_index"
    )"


    echo "Сервер импортирован:"
    echo "ID: $server_id"
    echo "Название: $BRORAY_NAME"
    echo "UUID клиента: $BRORAY_UUID"
    echo "Адрес: $BRORAY_ADDRESS:$BRORAY_PORT"
    echo "Файл: $server_file"
}

broray_server_detect_protocol() {
    uri="$1"

    case "$uri" in
        vless://*)
            printf '%s\n' "vless"
            ;;
        vmess://*)
            printf '%s\n' "vmess"
            ;;
        trojan://*)
            printf '%s\n' "trojan"
            ;;
        ss://*)
            printf '%s\n' "shadowsocks"
            ;;
        hysteria2://*|hy2://*)
            printf '%s\n' "hysteria2"
            ;;
        tuic://*)
            printf '%s\n' "tuic"
            ;;
        socks://*|socks5://*)
            printf '%s\n' "socks"
            ;;
        http://*|https://*)
            printf '%s\n' "http"
            ;;
        *)
            broray_die \
                "неподдерживаемый формат конфигурации"
            ;;
    esac
}

broray_server_import() {
    original_uri="$1"
    source_type="${2:-manual}"
    source_id="${3:-}"
    source_index="${4:-0}"

    [ -n "$original_uri" ] ||
        broray_die "не указана ссылка конфигурации"

    protocol="$(
        broray_server_detect_protocol "$original_uri"
    )"

    case "$protocol" in
        vless)
            broray_server_import_vless \
                "$original_uri" \
                "$source_type" \
                "$source_id" \
                "$source_index"
            ;;
        vmess)
            broray_server_import_vmess \
                "$original_uri" \
                "$source_type" \
                "$source_id" \
                "$source_index"
            ;;
        trojan)
            broray_server_import_trojan \
                "$original_uri" \
                "$source_type" \
                "$source_id" \
                "$source_index"
            ;;
        shadowsocks)
            broray_die \
                "парсер Shadowsocks ещё не реализован"
            ;;
        hysteria2)
            broray_die \
                "парсер Hysteria2 ещё не реализован"
            ;;
        tuic)
            broray_die \
                "парсер TUIC ещё не реализован"
            ;;
        socks)
            broray_die \
                "парсер SOCKS ещё не реализован"
            ;;
        http)
            broray_die \
                "парсер HTTP-прокси ещё не реализован"
            ;;
        *)
            broray_die \
                "для протокола $protocol отсутствует импортёр"
            ;;
    esac
}

broray_server_save_vmess() {
    original_uri="$1"
    server_id="$2"
    source_type="$3"
    source_id="${4:-}"
    source_index="${5:-0}"

    broray_server_validate_id "$server_id"

    case "$BRORAY_PORT" in
        ''|*[!0-9]*)
            broray_die \
                "порт VMess должен быть числом"
            ;;
    esac

    [ "$BRORAY_PORT" -ge 1 ] 2>/dev/null &&
    [ "$BRORAY_PORT" -le 65535 ] 2>/dev/null ||
        broray_die \
            "порт VMess должен находиться в диапазоне 1–65535"

    case "$BRORAY_NETWORK" in
        raw|ws|grpc|httpupgrade|xhttp)
            ;;
        *)
            broray_die \
                "неподдерживаемый транспорт VMess: $BRORAY_NETWORK"
            ;;
    esac

    case "$BRORAY_SECURITY" in
        none|tls|reality)
            ;;
        *)
            broray_die \
                "неподдерживаемая защита VMess: $BRORAY_SECURITY"
            ;;
    esac

    if [ "$BRORAY_SECURITY" = "reality" ]; then
        case "$BRORAY_NETWORK" in
            raw|grpc|xhttp)
                ;;
            *)
                broray_die \
                    "REALITY несовместима с транспортом $BRORAY_NETWORK"
                ;;
        esac

        [ -n "$BRORAY_SNI" ] ||
            broray_die \
                "для VMess REALITY не указан SNI"

        [ -n "$BRORAY_PBK" ] ||
            broray_die \
                "для VMess REALITY не указан public key"
    fi

    printf '%s' "$BRORAY_EXTRA" |
        jq -e . >/dev/null 2>&1 ||
        broray_die \
            "VMess extra содержит неправильный JSON"

    printf '%s' "$BRORAY_ALPN" |
        jq -e 'type == "array"' >/dev/null 2>&1 ||
        broray_die \
            "VMess ALPN имеет неправильный формат"

    mkdir -p \
        "$BRORAY_SERVERS" \
        "$BRORAY_TMP"

    server_file="$(
        broray_server_path "$server_id"
    )"

    temporary_file="$BRORAY_TMP/server-import.$$.json"

    jq -n \
        --arg id "$server_id" \
        --arg name "$BRORAY_NAME" \
        --arg uri "$original_uri" \
        --arg address "$BRORAY_ADDRESS" \
        --argjson port "$BRORAY_PORT" \
        --arg uuid "$BRORAY_UUID" \
        --argjson alterId "$BRORAY_ALTER_ID" \
        --arg encryption "$BRORAY_ENCRYPTION" \
        --arg network "$BRORAY_NETWORK" \
        --arg security "$BRORAY_SECURITY" \
        --arg sourceType "$source_type" \
        --arg sourceId "$source_id" \
        --argjson sourceIndex "$source_index" \
        --arg sni "$BRORAY_SNI" \
        --arg fingerprint "$BRORAY_FP" \
        --argjson allowInsecure "$BRORAY_ALLOW_INSECURE" \
        --argjson alpn "$BRORAY_ALPN" \
        --arg publicKey "$BRORAY_PBK" \
        --arg shortId "$BRORAY_SID" \
        --arg spiderX "$BRORAY_SPX" \
        --arg host "$BRORAY_HOST" \
        --arg path "$BRORAY_PATH" \
        --arg serviceName "$BRORAY_SERVICE_NAME" \
        --arg mode "$BRORAY_MODE" \
        --arg headerType "$BRORAY_HEADER_TYPE" \
        --argjson extra "$BRORAY_EXTRA" \
        '{
            schemaVersion: 2,
            id: $id,
            name: $name,
            source: (
                if $sourceType == "subscription"
                then {
                    type: $sourceType,
                    subscriptionId: $sourceId,
                    nodeIndex: $sourceIndex
                }
                else {
                    type: $sourceType
                }
                end
            ),
            uri: $uri,
            protocol: "vmess",
            address: $address,
            port: $port,
            uuid: $uuid,
            alterId: $alterId,
            encryption: $encryption,
            network: $network,
            security: $security,
            tls: {
                serverName: $sni,
                fingerprint: $fingerprint,
                allowInsecure: $allowInsecure,
                alpn: $alpn
            },
            reality: {
                serverName: $sni,
                fingerprint: $fingerprint,
                publicKey: $publicKey,
                shortId: $shortId,
                spiderX: $spiderX
            },
            transport: {
                host: $host,
                path: $path,
                serviceName: $serviceName,
                mode: $mode,
                headerType: $headerType,
                extra: $extra
            }
        }' > "$temporary_file" ||
        broray_die \
            "не удалось создать файл VMess-сервера"

    broray_server_validate "$temporary_file"

    mv "$temporary_file" "$server_file" ||
        broray_die \
            "не удалось сохранить VMess-сервер"

    printf '%s\n' "$server_file"
}

broray_server_import_vmess() {
    original_uri="$1"
    source_type="${2:-manual}"
    source_id="${3:-}"
    source_index="${4:-0}"

    . "$BRORAY_BASE/lib/parser-vmess.sh"

    broray_parse_vmess "$original_uri"

    server_id="$(
        broray_server_generate_id \
            "$source_type" \
            "$source_id" \
            "$source_index"
    )"

    server_file="$(
        broray_server_save_vmess \
            "$original_uri" \
            "$server_id" \
            "$source_type" \
            "$source_id" \
            "$source_index"
    )"

    echo "Сервер импортирован:"
    echo "ID: $server_id"
    echo "Название: $BRORAY_NAME"
    echo "Протокол: VMess"
    echo "UUID клиента: $BRORAY_UUID"
    echo "Адрес: $BRORAY_ADDRESS:$BRORAY_PORT"
    echo "Транспорт: $BRORAY_NETWORK"
    echo "Защита: $BRORAY_SECURITY"
    echo "Файл: $server_file"
}

broray_server_save_trojan()
{
    original_uri="$1"
    server_id="$2"
    source_type="$3"
    source_id="${4:-}"
    source_index="${5:-0}"

    broray_server_validate_id "$server_id"

    [ -n "$BRORAY_PASSWORD" ] ||
        broray_die "парсер Trojan не вернул пароль"

    [ -n "$BRORAY_ADDRESS" ] ||
        broray_die "парсер Trojan не вернул адрес"

    case "$BRORAY_PORT" in
        ''|*[!0-9]*)
            broray_die \
                "порт Trojan должен быть числом"
            ;;
    esac

    [ "$BRORAY_PORT" -ge 1 ] 2>/dev/null &&
    [ "$BRORAY_PORT" -le 65535 ] 2>/dev/null ||
        broray_die \
            "порт Trojan должен находиться в диапазоне 1–65535"

    case "$BRORAY_NETWORK" in
        raw|ws|grpc|httpupgrade|xhttp)
            ;;
        *)
            broray_die \
                "неподдерживаемый транспорт Trojan: $BRORAY_NETWORK"
            ;;
    esac

    [ "$BRORAY_SECURITY" = "tls" ] ||
        broray_die \
            "пока Trojan поддерживается только с TLS"

    [ -n "$BRORAY_SNI" ] ||
        broray_die "для Trojan TLS не указан SNI"

    printf '%s' "$BRORAY_ALPN" |
        jq -e 'type == "array"' >/dev/null 2>&1 ||
        broray_die \
            "Trojan ALPN имеет неправильный формат"

    mkdir -p \
        "$BRORAY_SERVERS" \
        "$BRORAY_TMP"

    server_file="$(
        broray_server_path "$server_id"
    )"

    temporary_file="$BRORAY_TMP/server-import.$$.json"

    jq -n \
        --arg id "$server_id" \
        --arg name "$BRORAY_NAME" \
        --arg uri "$original_uri" \
        --arg address "$BRORAY_ADDRESS" \
        --argjson port "$BRORAY_PORT" \
        --arg password "$BRORAY_PASSWORD" \
        --arg network "$BRORAY_NETWORK" \
        --arg security "$BRORAY_SECURITY" \
        --arg sourceType "$source_type" \
        --arg sourceId "$source_id" \
        --argjson sourceIndex "$source_index" \
        --arg sni "$BRORAY_SNI" \
        --arg fingerprint "$BRORAY_FP" \
        --argjson allowInsecure "$BRORAY_ALLOW_INSECURE" \
        --argjson alpn "$BRORAY_ALPN" \
        --arg host "$BRORAY_HOST" \
        --arg path "$BRORAY_PATH" \
        --arg serviceName "$BRORAY_SERVICE_NAME" \
        --arg mode "$BRORAY_MODE" \
        '{
            schemaVersion: 2,
            id: $id,
            name: $name,
            source: (
                if $sourceType == "subscription"
                then {
                    type: $sourceType,
                    subscriptionId: $sourceId,
                    nodeIndex: $sourceIndex
                }
                else {
                    type: $sourceType
                }
                end
            ),
            uri: $uri,
            protocol: "trojan",
            address: $address,
            port: $port,
            password: $password,
            network: $network,
            security: $security,
            tls: {
                serverName: $sni,
                fingerprint: $fingerprint,
                allowInsecure: $allowInsecure,
                alpn: $alpn
            },
            transport: {
                host: $host,
                path: $path,
                serviceName: $serviceName,
                mode: $mode
            }
        }' > "$temporary_file" ||
        broray_die \
            "не удалось создать файл Trojan-сервера"

    broray_server_validate "$temporary_file"

    mv "$temporary_file" "$server_file" ||
        broray_die \
            "не удалось сохранить Trojan-сервер"

    chmod 600 "$server_file"

    printf '%s\n' "$server_file"
}

broray_server_import_trojan()
{
    original_uri="$1"
    source_type="${2:-manual}"
    source_id="${3:-}"
    source_index="${4:-0}"

    . "$BRORAY_BASE/lib/parser-trojan.sh"

    broray_parse_trojan "$original_uri"

    server_id="$(
        broray_server_generate_id \
            "$source_type" \
            "$source_id" \
            "$source_index"
    )"

    server_file="$(
        broray_server_save_trojan \
            "$original_uri" \
            "$server_id" \
            "$source_type" \
            "$source_id" \
            "$source_index"
    )"

    echo "Сервер импортирован:"
    echo "ID: $server_id"
    echo "Название: $BRORAY_NAME"
    echo "Протокол: Trojan"
    echo "Адрес: $BRORAY_ADDRESS:$BRORAY_PORT"
    echo "Транспорт: $BRORAY_NETWORK"
    echo "Защита: $BRORAY_SECURITY"
    echo "Файл: $server_file"
}

broray_server_save_hysteria2()
{
    original_uri="$1"
    server_id="$2"
    source_type="$3"
    source_id="${4:-}"
    source_index="${5:-0}"

    broray_server_validate_id "$server_id"

    [ -n "$BRORAY_AUTH" ] ||
        broray_die \
            "парсер Hysteria2 не вернул auth"

    [ -n "$BRORAY_ADDRESS" ] ||
        broray_die \
            "парсер Hysteria2 не вернул адрес"

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

    [ "$BRORAY_SECURITY" = "tls" ] ||
        broray_die \
            "Hysteria2 поддерживается только с TLS"

    [ -n "$BRORAY_SNI" ] ||
        broray_die \
            "для Hysteria2 не указан SNI"

    printf '%s' "$BRORAY_ALPN" |
        jq -e 'type == "array"' >/dev/null 2>&1 ||
        broray_die \
            "Hysteria2 ALPN имеет неправильный формат"

    printf '%s' "$BRORAY_FINAL_MASK" |
        jq -e 'type == "object"' >/dev/null 2>&1 ||
        broray_die \
            "Hysteria2 final mask имеет неправильный формат"

    mkdir -p \
        "$BRORAY_SERVERS" \
        "$BRORAY_TMP"

    server_file="$(
        broray_server_path "$server_id"
    )"

    temporary_file="$BRORAY_TMP/server-import.$$.json"

    jq -n \
        --arg id "$server_id" \
        --arg name "$BRORAY_NAME" \
        --arg uri "$original_uri" \
        --arg address "$BRORAY_ADDRESS" \
        --argjson port "$BRORAY_PORT" \
        --arg auth "$BRORAY_AUTH" \
        --arg sourceType "$source_type" \
        --arg sourceId "$source_id" \
        --argjson sourceIndex "$source_index" \
        --arg security "$BRORAY_SECURITY" \
        --arg sni "$BRORAY_SNI" \
        --arg fingerprint "$BRORAY_FP" \
        --argjson alpn "$BRORAY_ALPN" \
        --argjson allowInsecure "$BRORAY_ALLOW_INSECURE" \
        --arg obfs "$BRORAY_OBFS" \
        --arg obfsPassword "$BRORAY_OBFS_PASSWORD" \
        --arg upMbps "$BRORAY_UP_MBPS" \
        --arg downMbps "$BRORAY_DOWN_MBPS" \
        --argjson finalMask "$BRORAY_FINAL_MASK" \
        '{
            schemaVersion: 2,
            id: $id,
            name: $name,
            source: (
                if $sourceType == "subscription"
                then {
                    type: $sourceType,
                    subscriptionId: $sourceId,
                    nodeIndex: $sourceIndex
                }
                else {
                    type: $sourceType
                }
                end
            ),
            uri: $uri,
            protocol: "hysteria2",
            address: $address,
            port: $port,
            auth: $auth,
            network: "hysteria",
            security: $security,
            tls: {
                serverName: $sni,
                fingerprint: $fingerprint,
                alpn: $alpn,
                allowInsecure: $allowInsecure
            },
            hysteria: {
                version: 2,
                obfs: $obfs,
                obfsPassword: $obfsPassword,
                upMbps: $upMbps,
                downMbps: $downMbps,
                finalMask: $finalMask
            }
        }' > "$temporary_file" ||
        broray_die \
            "не удалось создать файл Hysteria2-сервера"

    broray_server_validate "$temporary_file"

    mv "$temporary_file" "$server_file" ||
        broray_die \
            "не удалось сохранить Hysteria2-сервер"

    chmod 600 "$server_file"

    printf '%s\n' "$server_file"
}

broray_server_import_hysteria2()
{
    original_uri="$1"
    source_type="${2:-manual}"
    source_id="${3:-}"
    source_index="${4:-0}"

    . "$BRORAY_BASE/lib/parser-hysteria2.sh"

    broray_parse_hysteria2 "$original_uri"

    server_id="$(
        broray_server_generate_id \
            "$source_type" \
            "$source_id" \
            "$source_index"
    )"

    server_file="$(
        broray_server_save_hysteria2 \
            "$original_uri" \
            "$server_id" \
            "$source_type" \
            "$source_id" \
            "$source_index"
    )"

    echo "Сервер импортирован:"
    echo "ID: $server_id"
    echo "Название: $BRORAY_NAME"
    echo "Протокол: Hysteria2"
    echo "Адрес: $BRORAY_ADDRESS:$BRORAY_PORT"
    echo "SNI: $BRORAY_SNI"
    echo "Маскировка: ${BRORAY_OBFS:-нет}"
    echo "Файл: $server_file"
}

# BROray protocol dispatcher override with HY2 support
broray_server_import()
{
    original_uri="$1"
    source_type="${2:-manual}"
    source_id="${3:-}"
    source_index="${4:-0}"

    [ -n "$original_uri" ] ||
        broray_die \
            "не указана ссылка конфигурации"

    protocol="$(
        broray_server_detect_protocol "$original_uri"
    )"

    case "$protocol" in
        vless)
            broray_server_import_vless \
                "$original_uri" \
                "$source_type" \
                "$source_id" \
                "$source_index"
            ;;
        vmess)
            broray_server_import_vmess \
                "$original_uri" \
                "$source_type" \
                "$source_id" \
                "$source_index"
            ;;
        trojan)
            broray_server_import_trojan \
                "$original_uri" \
                "$source_type" \
                "$source_id" \
                "$source_index"
            ;;
        hysteria2)
            broray_server_import_hysteria2 \
                "$original_uri" \
                "$source_type" \
                "$source_id" \
                "$source_index"
            ;;
        shadowsocks)
            broray_die \
                "парсер Shadowsocks ещё не реализован"
            ;;
        tuic)
            broray_die \
                "парсер TUIC ещё не реализован"
            ;;
        socks)
            broray_die \
                "парсер SOCKS ещё не реализован"
            ;;
        http)
            broray_die \
                "парсер HTTP-прокси ещё не реализован"
            ;;
        *)
            broray_die \
                "для протокола $protocol отсутствует импортёр"
            ;;
    esac
}

# BROray active subscription server transaction v1

broray_server_import_dispatch()
{
    transaction_uri="$1"
    transaction_source_type="${2:-manual}"
    transaction_source_id="${3:-}"
    transaction_source_index="${4:-0}"

    transaction_protocol="$(
        broray_server_detect_protocol "$transaction_uri"
    )"

    case "$transaction_protocol" in
        vless)
            broray_server_import_vless \
                "$transaction_uri" \
                "$transaction_source_type" \
                "$transaction_source_id" \
                "$transaction_source_index"
            ;;
        vmess)
            broray_server_import_vmess \
                "$transaction_uri" \
                "$transaction_source_type" \
                "$transaction_source_id" \
                "$transaction_source_index"
            ;;
        trojan)
            broray_server_import_trojan \
                "$transaction_uri" \
                "$transaction_source_type" \
                "$transaction_source_id" \
                "$transaction_source_index"
            ;;
        hysteria2)
            broray_server_import_hysteria2 \
                "$transaction_uri" \
                "$transaction_source_type" \
                "$transaction_source_id" \
                "$transaction_source_index"
            ;;
        shadowsocks)
            broray_die \
                "парсер Shadowsocks ещё не реализован"
            ;;
        tuic)
            broray_die \
                "парсер TUIC ещё не реализован"
            ;;
        socks)
            broray_die \
                "парсер SOCKS ещё не реализован"
            ;;
        http)
            broray_die \
                "парсер HTTP-прокси ещё не реализован"
            ;;
        *)
            broray_die \
                "для протокола $transaction_protocol отсутствует импортёр"
            ;;
    esac
}

broray_server_import()
{
    transaction_uri="$1"
    transaction_source_type="${2:-manual}"
    transaction_source_id="${3:-}"
    transaction_source_index="${4:-0}"

    [ -n "$transaction_uri" ] ||
        broray_die \
            "не указана ссылка конфигурации"

    transaction_active_id=""

    if [ -f /opt/broray/config/active-server ]; then
        transaction_active_id="$(
            sed -n '1p' /opt/broray/config/active-server
        )"
    fi

    transaction_target_id=""

    if [ "$transaction_source_type" = "subscription" ]; then
        case "$transaction_source_index" in
            ''|*[!0-9]*)
                broray_die \
                    "неправильный индекс узла подписки"
                ;;
        esac

        transaction_target_id="$(
            printf 'subscription-%s-%04d' \
                "$transaction_source_id" \
                "$transaction_source_index"
        )"
    fi

    transaction_old_server=""
    transaction_is_active=false

    if [ -n "$transaction_target_id" ] &&
       [ "$transaction_target_id" = "$transaction_active_id" ]; then

        transaction_is_active=true
        transaction_old_server="/opt/broray/tmp/active-subscription-server.$$.json"

        if [ -f "/opt/broray/servers/$transaction_target_id.json" ]; then
            cp \
                "/opt/broray/servers/$transaction_target_id.json" \
                "$transaction_old_server" ||
                broray_die \
                    "не удалось сохранить прежнюю версию активного сервера"
        fi
    fi

    if ! broray_server_import_dispatch \
        "$transaction_uri" \
        "$transaction_source_type" \
        "$transaction_source_id" \
        "$transaction_source_index"; then

        rm -f "$transaction_old_server"
        broray_die \
            "не удалось импортировать сервер"
    fi

    if [ "$transaction_is_active" = true ]; then
        echo
        echo "Обновлён активный сервер подписки."
        echo "Проверка и применение новой конфигурации..."

        if (
            broray_xray_apply_server "$transaction_target_id"
        ); then
            echo \
                "Новая версия активного сервера успешно применена."
            rm -f "$transaction_old_server"
        else
            echo \
                "Новая версия активного сервера не применена. Выполняется восстановление." \
                >&2

            if [ -f "$transaction_old_server" ]; then
                cp \
                    "$transaction_old_server" \
                    "/opt/broray/servers/$transaction_target_id.json" ||
                    broray_die \
                        "не удалось восстановить прежний сервер"
            fi

            if ! (
                broray_xray_apply_server "$transaction_target_id"
            ); then
                rm -f "$transaction_old_server"

                broray_die \
                    "не удалось восстановить прежнюю рабочую конфигурацию активного сервера"
            fi

            rm -f "$transaction_old_server"

            broray_die \
                "обновление активного сервера отменено: сохранена прежняя рабочая версия"
        fi
    fi
}
