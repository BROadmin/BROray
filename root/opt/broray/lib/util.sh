#!/bin/sh

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

    broray_require_file /opt/broray/bin/xray
    broray_require_file /opt/broray/config/system/settings.json

    broray_json_validate /opt/broray/config/system/settings.json
}
