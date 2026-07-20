#!/bin/sh

. /opt/broray/lib/util.sh

BRORAY_BASE="/opt/broray"
BRORAY_SUBSCRIPTIONS="$BRORAY_BASE/subscriptions"
BRORAY_TMP="$BRORAY_BASE/tmp"

broray_subscription_validate_id() {
    subscription_id="$1"

    [ -n "$subscription_id" ] ||
        broray_die "не указано имя подписки"

    case "$subscription_id" in
        *[!a-zA-Z0-9._-]*)
            broray_die \
                "имя подписки может содержать только буквы, цифры, точку, дефис и подчёркивание"
            ;;
    esac
}

broray_subscription_dir() {
    subscription_id="$1"

    broray_subscription_validate_id "$subscription_id"

    printf '%s/%s\n' \
        "$BRORAY_SUBSCRIPTIONS" \
        "$subscription_id"
}

broray_subscription_contains_uri() {
    source_file="$1"

    grep -Eq \
        '(^|[[:space:]])(vless|vmess|trojan|ss|hysteria2|hy2|tuic|socks|socks5|http|https)://' \
        "$source_file"
}

broray_subscription_extract_uris() {
    source_file="$1"
    destination_file="$2"

    tr '\r\t ' '\n\n\n' < "$source_file" |
        sed -n \
            -e '/^vless:\/\//p' \
            -e '/^vmess:\/\//p' \
            -e '/^trojan:\/\//p' \
            -e '/^ss:\/\//p' \
            -e '/^hysteria2:\/\//p' \
            -e '/^hy2:\/\//p' \
            -e '/^tuic:\/\//p' \
            -e '/^socks:\/\//p' \
            -e '/^socks5:\/\//p' \
            -e '/^http:\/\//p' \
            -e '/^https:\/\//p' |
        sed '/^[[:space:]]*$/d' \
            > "$destination_file" ||
        broray_die \
            "не удалось обработать подписку"
}

broray_subscription_decode_file() {
    source_file="$1"
    destination_file="$2"

    decoded_file="$BRORAY_TMP/subscription.decoded.$$"

    [ -f "$source_file" ] ||
        broray_die \
            "файл подписки не найден: $source_file"

    mkdir -p "$BRORAY_TMP"

    if broray_subscription_contains_uri "$source_file"; then
        cat "$source_file" > "$decoded_file" ||
            broray_die \
                "не удалось прочитать подписку"
    else
        command -v base64 >/dev/null 2>&1 ||
            broray_die \
                "не найдена команда base64"

        tr -d '\r\n ' < "$source_file" |
            base64 -d > "$decoded_file" 2>/dev/null ||
            broray_die \
                "не удалось декодировать подписку Base64"
    fi

    broray_subscription_extract_uris \
        "$decoded_file" \
        "$destination_file"

    rm -f "$decoded_file"

    node_count="$(
        wc -l < "$destination_file" |
            tr -d '[:space:]'
    )"

    case "$node_count" in
        ''|*[!0-9]*)
            broray_die \
                "не удалось определить количество серверов"
            ;;
    esac

    [ "$node_count" -gt 0 ] 2>/dev/null ||
        broray_die \
            "в подписке не найдено поддерживаемых конфигураций"

    printf '%s\n' "$node_count"
}

broray_subscription_cleanup_servers() {
    subscription_id="$1"
    maximum_index="$2"

    broray_subscription_validate_id "$subscription_id"

    case "$maximum_index" in
        ""|*[!0-9]*)
            broray_die "неправильное количество узлов подписки"
            ;;
    esac

    active_server_id=""
    active_server_file="$BRORAY_BASE/config/active-server"

    if [ -f "$active_server_file" ]; then
        active_server_id="$(cat "$active_server_file")"
    fi

    removed_count=0
    preserved_count=0

    for server_file in "$BRORAY_BASE/servers"/*.json; do
        [ -f "$server_file" ] || continue

        server_source_type="$(
            jq -r ".source.type // empty" "$server_file"
        )"

        [ "$server_source_type" = "subscription" ] || continue

        server_subscription_id="$(
            jq -r ".source.subscriptionId // empty" "$server_file"
        )"

        [ "$server_subscription_id" = "$subscription_id" ] || continue

        server_index="$(
            jq -r ".source.nodeIndex // empty" "$server_file"
        )"

        case "$server_index" in
            ""|*[!0-9]*)
                echo "Предупреждение: пропущен сервер с неправильным nodeIndex: $server_file" >&2
                continue
                ;;
        esac

        if [ "$server_index" -ge 1 ] 2>/dev/null &&
           [ "$server_index" -le "$maximum_index" ] 2>/dev/null; then
            continue
        fi

        server_id="$(
            jq -r ".id // empty" "$server_file"
        )"

        if [ -n "$active_server_id" ] &&
           [ "$server_id" = "$active_server_id" ]; then
            echo "Предупреждение: устаревший активный сервер сохранён: $server_id" >&2
            preserved_count=$((preserved_count + 1))
            continue
        fi

        rm -f "$server_file" ||
            broray_die "не удалось удалить устаревший сервер $server_id"

        echo "Удалён устаревший сервер: $server_id"
        removed_count=$((removed_count + 1))
    done

    echo "Удалено устаревших серверов: $removed_count"

    if [ "$preserved_count" -gt 0 ]; then
        echo "Сохранено активных устаревших серверов: $preserved_count"
    fi
}

broray_subscription_add() {
    subscription_id="$1"
    subscription_url="$2"

    broray_subscription_validate_id "$subscription_id"

    [ -n "$subscription_url" ] ||
        broray_die \
            "не указана ссылка подписки"

    command -v curl >/dev/null 2>&1 ||
        broray_die \
            "не найдена команда curl"

    subscription_dir="$(
        broray_subscription_dir "$subscription_id"
    )"

    raw_file="$BRORAY_TMP/subscription.raw.$$"
    nodes_file="$BRORAY_TMP/subscription.nodes.$$"

    mkdir -p \
        "$subscription_dir" \
        "$BRORAY_TMP"

    echo "Загрузка подписки..."

    curl -fL \
        --connect-timeout 15 \
        --max-time 60 \
        -A "BROray/0.1.0" \
        "$subscription_url" \
        -o "$raw_file" ||
        broray_die \
            "не удалось загрузить подписку"

    node_count="$(
        broray_subscription_decode_file \
            "$raw_file" \
            "$nodes_file"
    )"

    mv "$raw_file" "$subscription_dir/raw.txt" ||
        broray_die \
            "не удалось сохранить исходную подписку"

    mv "$nodes_file" "$subscription_dir/nodes.txt" ||
        broray_die \
            "не удалось сохранить список серверов"

    updated_at="$(date '+%Y-%m-%dT%H:%M:%S%z')"

    jq -n \
        --arg id "$subscription_id" \
        --arg url "$subscription_url" \
        --arg updatedAt "$updated_at" \
        --argjson nodeCount "$node_count" \
        '{
            schemaVersion: 1,
            id: $id,
            url: $url,
            nodeCount: $nodeCount,
            updatedAt: $updatedAt
        }' > "$subscription_dir/meta.json" ||
        broray_die \
            "не удалось сохранить данные подписки"

    echo
    echo "Подписка сохранена:"
    echo "Импорт серверов подписки..."
    node_index=0
    while IFS= read -r node_uri || [ -n "$node_uri" ]; do
        [ -n "$node_uri" ] || continue
        node_index=$((node_index + 1))
        /opt/broray/bin/broray import-subscription-node \
            "$subscription_id" \
            "$node_index" \
            "$node_uri" ||
            broray_die \
                "не удалось импортировать узел $node_index подписки $subscription_id"
    done < "$subscription_dir/nodes.txt"
    echo "Импортировано серверов: $node_index"
    echo "Очистка устаревших серверов подписки..."
    broray_subscription_cleanup_servers \
        "$subscription_id" \
        "$node_index"
    echo
    echo "Название: $subscription_id"
    echo "Серверов: $node_count"
    echo "Каталог: $subscription_dir"
}

broray_subscription_update() {
    subscription_id="$1"

    subscription_dir="$(
        broray_subscription_dir "$subscription_id"
    )"

    meta_file="$subscription_dir/meta.json"

    broray_json_validate "$meta_file"

    subscription_url="$(
        jq -r '.url // empty' "$meta_file"
    )"

    [ -n "$subscription_url" ] ||
        broray_die \
            "в подписке отсутствует URL"

    broray_subscription_add \
        "$subscription_id" \
        "$subscription_url"
}

broray_subscription_list() {
    found=0

    for meta_file in "$BRORAY_SUBSCRIPTIONS"/*/meta.json; do
        [ -f "$meta_file" ] || continue

        found=1

        jq -r '
            "\(.id) | серверов: \(.nodeCount) | обновлена: \(.updatedAt)"
        ' "$meta_file"
    done

    [ "$found" -eq 1 ] ||
        echo "Подписки отсутствуют."
}

broray_subscription_nodes() {
    subscription_id="$1"

    subscription_dir="$(
        broray_subscription_dir "$subscription_id"
    )"

    nodes_file="$subscription_dir/nodes.txt"

    [ -f "$nodes_file" ] ||
        broray_die \
            "подписка не найдена: $subscription_id"

    cat "$nodes_file"
}
