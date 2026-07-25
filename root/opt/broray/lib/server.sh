#!/bin/sh

BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"

. "$BRORAY_BASE/lib/util.sh"

BRORAY_SERVERS="$BRORAY_BASE/servers"
BRORAY_ACTIVE_SERVER_FILE="$BRORAY_BASE/config/active-server"

broray_server_validate_id() {
    server_id="$1"

    [ -n "$server_id" ] ||
        broray_die "не указан идентификатор сервера"

    case "$server_id" in
        *[!a-zA-Z0-9._-]*)
            broray_die \
                "идентификатор сервера содержит недопустимые символы"
            ;;
    esac
}

broray_server_path() {
    server_id="$1"

    broray_server_validate_id "$server_id"

    printf '%s/%s.json\n' \
        "$BRORAY_SERVERS" \
        "$server_id"
}

broray_server_exists() {
    server_id="$1"
    server_file="$(broray_server_path "$server_id")"

    [ -f "$server_file" ]
}

broray_server_validate() {
    validation_file="$1"

    broray_json_validate "$validation_file"

    jq -e '
        (.schemaVersion == 2) and
        (.id | type == "string" and length > 0) and
        (.name | type == "string") and
        (.protocol | type == "string" and length > 0) and
        (.address | type == "string" and length > 0) and
        (.port | type == "number" and . >= 1 and . <= 65535) and
        (
            (
                .protocol == "trojan" and
                (.password | type == "string" and length > 0)
            ) or
            (
                .protocol != "trojan" and
                (.uuid | type == "string" and length > 0)
            )
        ) and
        (.network | type == "string" and length > 0) and
        (.security | type == "string" and length > 0) and
        (.source | type == "object") and
        (.source.type | type == "string" and length > 0)
    ' "$validation_file" >/dev/null 2>&1 ||
        broray_die \
            "сервер имеет неправильную структуру: $server_file"
}

broray_server_set_active() {
    server_id="$1"

    broray_server_exists "$server_id" ||
        broray_die "сервер $server_id не найден"

    printf '%s\n' "$server_id" \
        > "$BRORAY_ACTIVE_SERVER_FILE" ||
        broray_die "не удалось записать активный сервер"
}

broray_server_get_active_id() {
    [ -f "$BRORAY_ACTIVE_SERVER_FILE" ] ||
        broray_die "активный сервер не выбран"

    active_server_id="$(
        cat "$BRORAY_ACTIVE_SERVER_FILE"
    )"

    [ -n "$active_server_id" ] ||
        broray_die "файл активного сервера пуст"

    broray_server_exists "$active_server_id" ||
        broray_die \
            "активный сервер $active_server_id не найден"

    printf '%s\n' "$active_server_id"
}

broray_server_current() {
    active_server_id="$(
        broray_server_get_active_id
    )"

    server_file="$(
        broray_server_path "$active_server_id"
    )"

    broray_server_validate "$server_file"

    jq '{
        schemaVersion,
        id,
        name,
        source,
        uri,
        protocol,
        address,
        port,
        uuid,
        network,
        security,
        reality,
        xhttp
    }' "$server_file"
}

broray_server_list() {
    found=0

    if [ -f "$BRORAY_ACTIVE_SERVER_FILE" ]; then
        active_server_id="$(
            cat "$BRORAY_ACTIVE_SERVER_FILE"
        )"
    else
        active_server_id=""
    fi

    for server_file in "$BRORAY_SERVERS"/*.json; do
        [ -f "$server_file" ] || continue

        broray_server_validate "$server_file"

        found=1

        server_id="$(jq -r '.id' "$server_file")"
        server_name="$(
            jq -r '.name // .id' "$server_file"
        )"
        server_address="$(
            jq -r '.address' "$server_file"
        )"
        server_port="$(
            jq -r '.port' "$server_file"
        )"
        server_source="$(
            jq -r '.source.type // "unknown"' \
                "$server_file"
        )"

        marker=" "
        [ "$server_id" = "$active_server_id" ] &&
            marker="*"

        printf '%s %s | %s | %s:%s | %s\n' \
            "$marker" \
            "$server_id" \
            "$server_name" \
            "$server_address" \
            "$server_port" \
            "$server_source"
    done

    [ "$found" -eq 1 ] ||
        echo "Серверы отсутствуют."
}

broray_server_delete() {
    server_id="$1"
    broray_server_validate_id "$server_id"

    broray_server_exists "$server_id" ||
        broray_die "сервер $server_id не найден"

    active_server_id=""
    if [ -f "$BRORAY_ACTIVE_SERVER_FILE" ]; then
        active_server_id="$(cat "$BRORAY_ACTIVE_SERVER_FILE")"
    fi

    [ "$server_id" != "$active_server_id" ] ||
        broray_die "нельзя удалить активный сервер"

    server_file="$(broray_server_path "$server_id")"

    rm -f "$server_file" ||
        broray_die "не удалось удалить сервер $server_id"

    printf 'Сервер удалён: %s\n' "$server_id"
}

broray_server_rename() {
    server_id="$1"
    new_name="$2"

    broray_server_validate_id "$server_id"
    [ -n "$new_name" ] || broray_die "не указано новое имя"

    server_file="$(broray_server_path "$server_id")"
    broray_server_exists "$server_id" ||
        broray_die "сервер $server_id не найден"

    tmp="${server_file}.tmp"

    jq --arg name "$new_name" \
        '.name = $name' \
        "$server_file" > "$tmp" ||
        broray_die "не удалось изменить имя"

    mv "$tmp" "$server_file" ||
        broray_die "не удалось сохранить сервер"

    printf 'Сервер переименован: %s\n' "$new_name"
}

# BROray HY2 validation override
broray_server_validate()
{
    validation_file="$1"

    broray_json_validate "$validation_file"

    jq -e '
        (.schemaVersion == 2) and
        (.id | type == "string" and length > 0) and
        (.name | type == "string") and
        (.protocol | type == "string" and length > 0) and
        (.address | type == "string" and length > 0) and
        (.port | type == "number" and . >= 1 and . <= 65535) and
        (
            if .protocol == "trojan" then
                (.password | type == "string" and length > 0)
            elif .protocol == "hysteria2" then
                (.auth | type == "string" and length > 0) and
                (.network == "hysteria") and
                (.security == "tls") and
                (.hysteria | type == "object") and
                (.hysteria.version == 2) and
                (.tls | type == "object") and
                (
                    .tls.serverName |
                    type == "string" and length > 0
                )
            else
                (.uuid | type == "string" and length > 0)
            end
        ) and
        (.network | type == "string" and length > 0) and
        (.security | type == "string" and length > 0) and
        (.source | type == "object") and
        (.source.type | type == "string" and length > 0)
    ' "$validation_file" >/dev/null 2>&1 ||
        broray_die \
            "сервер имеет неправильную структуру: $validation_file"
}
