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
