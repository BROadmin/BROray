#!/opt/bin/ash

BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-/opt/broray/routes}"
BRORAY_ROUTES_USER_AGENT="${BRORAY_ROUTES_USER_AGENT:-BROray-routes/1.1}"
BRORAY_ROUTES_BUNDLES="$BRORAY_ROUTES_ROOT/bundles.json"
BRORAY_ROUTES_CONFIG="$BRORAY_ROUTES_ROOT/config.json"
BRORAY_ROUTES_MANIFEST=""
BRORAY_ROUTES_STATE=""
BRORAY_ROUTES_LOCK="$BRORAY_ROUTES_ROOT/locks/operation.lock"
BRORAY_ROUTES_TMP="$BRORAY_ROUTES_ROOT/tmp"
BRORAY_ROUTES_ACTIVE_WORK=""
BRORAY_ROUTES_ACTIVE_BUNDLE=""

broray_routes_check_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_check_error()
{
    printf '%s\n' "$*" >&2
    exit 1
}

broray_routes_bundle_id_valid()
{
    case "${1:-}" in
        ''|*[!a-z0-9_-]*)
            return 1
            ;;
    esac

    return 0
}

broray_routes_safe_repository()
{
    local value owner name

    value="${1:-}"
    owner="${value%%/*}"
    name="${value#*/}"

    [ -n "$owner" ] || return 1
    [ -n "$name" ] || return 1
    [ "$owner/$name" = "$value" ] || return 1

    case "$owner" in
        *[!A-Za-z0-9_.-]*) return 1 ;;
    esac

    case "$name" in
        *[!A-Za-z0-9_.-]*|*/*) return 1 ;;
    esac

    return 0
}

broray_routes_safe_branch()
{
    case "${1:-}" in
        ''|*[!A-Za-z0-9_.-]*)
            return 1
            ;;
    esac

    return 0
}

broray_routes_safe_directory()
{
    case "${1:-}" in
        ''|/*|*/|*..*|*[!A-Za-z0-9_./\(\)-]*)
            return 1
            ;;
    esac

    return 0
}

broray_routes_safe_filename()
{
    case "${1:-}" in
        ''|*/*|*..*|*[!A-Za-z0-9_.\(\)-]*)
            return 1
            ;;
    esac

    return 0
}

broray_routes_is_pid()
{
    case "${1:-}" in
        ''|*[!0-9]*)
            return 1
            ;;
    esac

    return 0
}

broray_routes_lock_acquire()
{
    local lock_parent lock_pid

    lock_parent="$(dirname "$BRORAY_ROUTES_LOCK")"
    mkdir -p "$lock_parent" || return 1

    if mkdir "$BRORAY_ROUTES_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_ROUTES_LOCK/pid"
        printf '%s\n' "check" >"$BRORAY_ROUTES_LOCK/action"
        printf '%s\n' "$BRORAY_ROUTES_ACTIVE_BUNDLE" >"$BRORAY_ROUTES_LOCK/bundle"
        return 0
    fi

    lock_pid="$(sed -n '1p' "$BRORAY_ROUTES_LOCK/pid" 2>/dev/null)"

    if broray_routes_is_pid "$lock_pid" &&
       kill -0 "$lock_pid" 2>/dev/null
    then
        return 2
    fi

    rm -rf "$BRORAY_ROUTES_LOCK" 2>/dev/null || return 1

    if mkdir "$BRORAY_ROUTES_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_ROUTES_LOCK/pid"
        printf '%s\n' "check" >"$BRORAY_ROUTES_LOCK/action"
        printf '%s\n' "$BRORAY_ROUTES_ACTIVE_BUNDLE" >"$BRORAY_ROUTES_LOCK/bundle"
        return 0
    fi

    return 1
}

broray_routes_lock_release()
{
    rm -rf "$BRORAY_ROUTES_LOCK" 2>/dev/null || true
}

broray_routes_check_cleanup()
{
    if [ -n "$BRORAY_ROUTES_ACTIVE_WORK" ]; then
        rm -rf "$BRORAY_ROUTES_ACTIVE_WORK" 2>/dev/null || true
    fi

    broray_routes_lock_release
}

broray_routes_http_get()
{
    local url output fixture_name

    url="$1"
    output="$2"

    rm -f "$output"

    if [ -n "${BRORAY_ROUTES_HTTP_FIXTURE_DIR:-}" ]; then
        case "$url" in
            *'/commits?'*)
                cp "$BRORAY_ROUTES_HTTP_FIXTURE_DIR/commit.json" "$output"
                ;;
            *'/contents/'*'?ref='*)
                cp "$BRORAY_ROUTES_HTTP_FIXTURE_DIR/contents.json" "$output"
                ;;
            *'raw.githubusercontent.com/'*)
                fixture_name="${url##*/}"
                cp "$BRORAY_ROUTES_HTTP_FIXTURE_DIR/$fixture_name" "$output"
                ;;
            *)
                return 1
                ;;
        esac

        return $?
    fi

    if command -v curl >/dev/null 2>&1; then
        curl \
            -f \
            -s \
            -S \
            -L \
            --connect-timeout 5 \
            --max-time 20 \
            -H "Accept: application/vnd.github+json" \
            -H "User-Agent: $BRORAY_ROUTES_USER_AGENT" \
            -o "$output" \
            "$url"
        return $?
    fi

    if command -v wget >/dev/null 2>&1; then
        wget \
            -q \
            -T 20 \
            --header="Accept: application/vnd.github+json" \
            --header="User-Agent: $BRORAY_ROUTES_USER_AGENT" \
            -O "$output" \
            "$url"
        return $?
    fi

    return 127
}

broray_routes_commit_sha_valid()
{
    local sha

    sha="${1:-}"

    [ "${#sha}" -eq 40 ] || return 1

    case "$sha" in
        *[!0-9a-fA-F]*)
            return 1
            ;;
    esac

    return 0
}

broray_routes_parse_windows_bat()
{
    local input output error_file max_routes parsed route_count

    input="$1"
    output="$2"
    error_file="$3"
    max_routes="$4"
    parsed="$output.parsed.$$"

    rm -f "$output" "$error_file" "$parsed"

    awk -v error_file="$error_file" '
        function report(message) {
            print "Строка " NR ": " message > error_file
            bad = 1
        }

        function valid_octet(value, number) {
            if (value !~ /^[0-9]+$/) {
                return 0
            }

            number = value + 0
            return number >= 0 && number <= 255
        }

        function valid_ipv4(value, parts, count, i) {
            count = split(value, parts, ".")

            if (count != 4) {
                return 0
            }

            for (i = 1; i <= 4; i += 1) {
                if (!valid_octet(parts[i])) {
                    return 0
                }
            }

            return 1
        }

        function mask_octet_bits(value) {
            if (value == 255) return 8
            if (value == 254) return 7
            if (value == 252) return 6
            if (value == 248) return 5
            if (value == 240) return 4
            if (value == 224) return 3
            if (value == 192) return 2
            if (value == 128) return 1
            if (value == 0) return 0
            return -1
        }

        function mask_prefix(value, parts, count, i, bits, zero_seen, prefix) {
            count = split(value, parts, ".")

            if (count != 4) {
                return -1
            }

            zero_seen = 0
            prefix = 0

            for (i = 1; i <= 4; i += 1) {
                if (!valid_octet(parts[i])) {
                    return -1
                }

                bits = mask_octet_bits(parts[i] + 0)

                if (bits < 0) {
                    return -1
                }

                if (zero_seen && bits != 0) {
                    return -1
                }

                if (bits < 8) {
                    zero_seen = 1
                }

                prefix += bits
            }

            return prefix
        }

        function normalized_network(ip, mask, ip_parts, mask_parts, i, block, octet, result) {
            split(ip, ip_parts, ".")
            split(mask, mask_parts, ".")
            result = ""

            for (i = 1; i <= 4; i += 1) {
                if ((mask_parts[i] + 0) == 255) {
                    octet = ip_parts[i] + 0
                } else {
                    block = 256 - (mask_parts[i] + 0)
                    octet = int((ip_parts[i] + 0) / block) * block
                }

                result = result (i == 1 ? "" : ".") octet
            }

            return result
        }

        {
            sub(/\r$/, "")
            line = $0
            sub(/^[ \t]+/, "", line)
            sub(/[ \t]+$/, "", line)

            lower_line = tolower(line)

            if (line == "" ||
                substr(line, 1, 1) == "#" ||
                substr(line, 1, 2) == "::" ||
                lower_line ~ /^rem([ \t]|$)/ ||
                lower_line ~ /^@?echo([ \t]|$)/ ||
                lower_line ~ /^chcp([ \t]|$)/ ||
                lower_line ~ /^title([ \t]|$)/ ||
                lower_line ~ /^pause([ \t]|$)/ ||
                lower_line ~ /^cls([ \t]|$)/ ||
                lower_line ~ /^setlocal([ \t]|$)/ ||
                lower_line ~ /^endlocal([ \t]|$)/ ||
                lower_line ~ /^exit[ \t]+\/b([ \t]|$)/)
            {
                next
            }

            count = split(line, fields, /[ \t]+/)

            if (count < 6 ||
                tolower(fields[1]) != "route" ||
                tolower(fields[2]) != "add" ||
                tolower(fields[4]) != "mask")
            {
                report("разрешена только команда route add <IPv4> mask <маска> 0.0.0.0")
                next
            }

            if (count > 6 && (count < 8 || fields[7] != "&" || tolower(fields[8]) != "rem")) {

                report("после маршрута разрешён только комментарий & rem")
                next
            }

            network = fields[3]
            mask = fields[5]
            gateway = fields[6]

            if (!valid_ipv4(network)) {
                report("некорректный IPv4-адрес сети")
                next
            }

            prefix = mask_prefix(mask)

            if (prefix < 0) {
                report("некорректная или непоследовательная маска")
                next
            }

            if (gateway != "0.0.0.0") {
                report("разрешён только шлюз 0.0.0.0")
                next
            }

            network = normalized_network(network, mask)

            if (network == "0.0.0.0" && prefix == 0) {
                report("маршрут по умолчанию запрещён")
                next
            }

            print network "/" prefix
        }

        END {
            if (bad) {
                exit 1
            }
        }
    ' "$input" >"$parsed" || {
        rm -f "$parsed"
        return 1
    }

    LC_ALL=C sort -u "$parsed" >"$output" || {
        rm -f "$parsed"
        return 1
    }

    rm -f "$parsed"

    route_count="$(wc -l <"$output" | tr -d ' ')"

    case "$route_count" in
        ''|*[!0-9]*)
            return 1
            ;;
    esac

    [ "$route_count" -gt 0 ] || {
        printf '%s\n' "В источнике не найдено маршрутов." >"$error_file"
        return 1
    }

    [ "$route_count" -le "$max_routes" ] || {
        printf '%s\n' \
            "Количество маршрутов превышает лимит: $route_count > $max_routes" \
            >"$error_file"
        return 1
    }

    return 0
}

broray_routes_check_state_error()
{
    local message now state_new

    message="$1"
    now="$(broray_routes_check_now)"
    state_new="$BRORAY_ROUTES_STATE.new.$$"

    jq \
        --arg now "$now" \
        --arg message "$message" \
        '
            .status = "check_error" |
            .lastCheckedAt = $now |
            .lastError = {
                message: $message,
                at: $now
            } |
            .updatedAt = $now
        ' \
        "$BRORAY_ROUTES_STATE" >"$state_new" || {
            rm -f "$state_new"
            return 1
        }

    mv "$state_new" "$BRORAY_ROUTES_STATE"
}

broray_routes_check_create_state()
{
    local bundle_id now state_new state_parent

    bundle_id="$1"
    now="$(broray_routes_check_now)"
    state_parent="$(dirname "$BRORAY_ROUTES_STATE")"
    state_new="$BRORAY_ROUTES_STATE.new.$$"

    mkdir -p "$state_parent" || return 1

    cat >"$state_new" <<STATE_JSON
{
  "schemaVersion": 1,
  "bundleId": "$bundle_id",
  "status": "not_checked",
  "availableVersion": null,
  "downloadedVersion": null,
  "installedVersion": null,
  "routeCount": null,
  "lastCheckedAt": null,
  "lastDownloadedAt": null,
  "lastExportedAt": null,
  "lastError": null,
  "updatedAt": "$now"
}
STATE_JSON

    jq -e \
        --arg bundle_id "$bundle_id" \
        '(.schemaVersion == 1) and (.bundleId == $bundle_id)' \
        "$state_new" >/dev/null || {
            rm -f "$state_new"
            return 1
        }

    mv "$state_new" "$BRORAY_ROUTES_STATE" || return 1
    chmod 644 "$BRORAY_ROUTES_STATE" 2>/dev/null || true
}

broray_routes_check_bind_bundle()
{
    local bundle_id managed_interface manifest_interface

    bundle_id="$1"

    broray_routes_bundle_id_valid "$bundle_id" ||
        broray_routes_check_error "Некорректный идентификатор набора маршрутов."

    [ -r "$BRORAY_ROUTES_BUNDLES" ] ||
        broray_routes_check_error "Реестр наборов маршрутов недоступен."

    [ -r "$BRORAY_ROUTES_CONFIG" ] ||
        broray_routes_check_error "Конфигурация маршрутов недоступна."

    if ! jq -e \
        --arg bundle_id "$bundle_id" \
        '(.schemaVersion == 1) and (.bundles | index($bundle_id) != null)' \
        "$BRORAY_ROUTES_BUNDLES" >/dev/null 2>&1
    then
        broray_routes_check_error "Набор маршрутов не разрешён: $bundle_id"
    fi

    BRORAY_ROUTES_MANIFEST="$BRORAY_ROUTES_ROOT/manifests/$bundle_id.json"
    BRORAY_ROUTES_STATE="$BRORAY_ROUTES_ROOT/state/$bundle_id.json"
    BRORAY_ROUTES_ACTIVE_BUNDLE="$bundle_id"

    [ -r "$BRORAY_ROUTES_MANIFEST" ] ||
        broray_routes_check_error "Манифест набора недоступен: $bundle_id"

    managed_interface="$(jq -r '.managedInterface // empty' "$BRORAY_ROUTES_CONFIG")"
    manifest_interface="$(jq -r '.targetInterface // empty' "$BRORAY_ROUTES_MANIFEST")"

    [ -n "$managed_interface" ] ||
        broray_routes_check_error "В конфигурации не задан управляемый интерфейс."

    [ "$manifest_interface" = "$managed_interface" ] ||
        broray_routes_check_error "Манифест пытается использовать неуправляемый интерфейс."

    if ! jq -e \
        --arg bundle_id "$bundle_id" \
        --arg managed_interface "$managed_interface" \
        '
            (.schemaVersion == 1) and
            (.id == $bundle_id) and
            (.source.provider == "github") and
            ((.source.repository | type) == "string") and
            ((.source.branch | type) == "string") and
            ((.source.directory | type) == "string") and
            ((.source.files | type) == "array") and
            ((.source.files | length) > 0) and
            (
                (.source.discovery == null) or
                (
                    (.source.discovery.mode == "all-bat") and
                    ((.source.discovery.recursive // false) == false)
                )
            ) and
            (.targetInterface == $managed_interface) and
            ((.limits.maxSourceBytes | type) == "number") and
            (.limits.maxSourceBytes > 0) and
            ((.limits.maxRoutes | type) == "number") and
            (.limits.maxRoutes > 0)
        ' \
        "$BRORAY_ROUTES_MANIFEST" >/dev/null 2>&1
    then
        broray_routes_check_error "Манифест набора повреждён: $bundle_id"
    fi

    if [ ! -f "$BRORAY_ROUTES_STATE" ]; then
        broray_routes_check_create_state "$bundle_id" ||
            broray_routes_check_error "Не удалось создать состояние набора: $bundle_id"
    fi

    if ! jq -e \
        --arg bundle_id "$bundle_id" \
        '(.schemaVersion == 1) and (.bundleId == $bundle_id)' \
        "$BRORAY_ROUTES_STATE" >/dev/null 2>&1
    then
        broray_routes_check_error "Состояние набора повреждено: $bundle_id"
    fi
}

broray_routes_check_run()
{
    local bundle_id manifest state lock_result work commit_json contents_json enabled_files combined normalized parse_errors
    local repository branch source_directory bundle_name max_bytes max_routes managed_interface discovery_mode
    local enabled_count unsupported total_bytes index parser source_file source_path source_raw source_part source_errors tab
    local commit_url contents_url raw_url source_commit source_date source_bytes details route_count content_sha256
    local now check_result status message state_new api_message
    local source_files_tsv source_files_json source_file_routes source_file_sha source_html_url
    local catalog baseline_normalized baseline_source_files baseline_source_set baseline_content_sha baseline_route_count
    local source_set_tsv source_set_sha256 file_changes_json route_added route_removed route_unchanged
    local added_files changed_files removed_files unchanged_files source_changed routes_changed download_required
    local downloaded_present installed_present installed_content_sha keenetic_update_required

    bundle_id="${1:-}"

    broray_routes_check_bind_bundle "$bundle_id"

    manifest="$BRORAY_ROUTES_MANIFEST"
    state="$BRORAY_ROUTES_STATE"
    bundle_name="$(jq -r '.name // .id' "$manifest")"
    repository="$(jq -r '.source.repository' "$manifest")"
    branch="$(jq -r '.source.branch' "$manifest")"
    source_directory="$(jq -r '.source.directory' "$manifest")"
    max_bytes="$(jq -r '.limits.maxSourceBytes' "$manifest")"
    max_routes="$(jq -r '.limits.maxRoutes' "$manifest")"
    managed_interface="$(jq -r '.targetInterface' "$manifest")"
    discovery_mode="$(jq -r '.source.discovery.mode // "manifest"' "$manifest")"

    broray_routes_safe_repository "$repository" ||
        broray_routes_check_error "Манифест содержит некорректный репозиторий."

    broray_routes_safe_branch "$branch" ||
        broray_routes_check_error "Манифест содержит некорректную ветку."

    broray_routes_safe_directory "$source_directory" ||
        broray_routes_check_error "Манифест содержит некорректный каталог источника."

    case "$max_bytes" in
        ''|*[!0-9]*) broray_routes_check_error "Некорректный лимит размера источника." ;;
    esac

    case "$max_routes" in
        ''|*[!0-9]*) broray_routes_check_error "Некорректный лимит маршрутов." ;;
    esac

    lock_result=0
    broray_routes_lock_acquire || lock_result=$?

    case "$lock_result" in
        0) ;;
        2) broray_routes_check_error "Другая операция с маршрутами уже выполняется." ;;
        *) broray_routes_check_error "Не удалось установить блокировку операции." ;;
    esac

    work="$BRORAY_ROUTES_TMP/check-$bundle_id.$$"
    BRORAY_ROUTES_ACTIVE_WORK="$work"
    commit_json="$work/commit.json"
    contents_json="$work/contents.json"
    enabled_files="$work/enabled-files.tsv"
    combined="$work/combined.txt"
    normalized="$work/normalized.txt"
    parse_errors="$work/parse-errors.txt"
    source_files_tsv="$work/source-files.tsv"
    source_files_json="$work/source-files.json"
    source_set_tsv="$work/source-set.tsv"
    file_changes_json="$work/file-changes.json"
    baseline_normalized="$work/baseline-normalized.txt"
    baseline_source_files="$work/baseline-source-files.json"

    mkdir -p "$work" || {
        broray_routes_lock_release
        broray_routes_check_error "Не удалось создать временный каталог."
    }

    trap 'broray_routes_check_cleanup' EXIT HUP INT TERM

    jq -r '
        .source.files[] |
        select(.enabled == true) |
        [(.type // .parser // ""), (.name // "")] |
        @tsv
    ' "$manifest" >"$enabled_files" || {
        message="Не удалось прочитать список файлов источника."
        broray_routes_check_state_error "$message" || true
        broray_routes_check_error "$message"
    }

    enabled_count="$(wc -l <"$enabled_files" | tr -d ' ')"
    tab="$(printf '\t')"

    case "$enabled_count" in
        ''|*[!0-9]*) enabled_count=0 ;;
    esac

    [ "$enabled_count" -gt 0 ] || {
        message="В манифесте нет включённых файлов маршрутов."
        broray_routes_check_state_error "$message" || true
        broray_routes_check_error "$message"
    }

    unsupported="$(awk -F '\t' '$1 != "windows-route-bat" {print $1}' "$enabled_files" | sed -n '1p')"

    [ -z "$unsupported" ] || {
        message="Манифест содержит неподдерживаемый включённый парсер: $unsupported"
        broray_routes_check_state_error "$message" || true
        broray_routes_check_error "$message"
    }

    while IFS="$tab" read -r parser source_file
    do
        broray_routes_safe_filename "$source_file" || {
            message="Манифест содержит некорректное имя файла источника."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        }
    done <"$enabled_files"

    commit_url="https://api.github.com/repos/$repository/commits?sha=$branch&path=$source_directory&per_page=1"

    if ! broray_routes_http_get "$commit_url" "$commit_json"; then
        message="Не удалось получить сведения о версии $bundle_name с GitHub."
        broray_routes_check_state_error "$message" || true
        broray_routes_check_error "$message"
    fi

    if ! jq -e '
        ((type) == "array") and
        (length >= 1) and
        ((.[0].sha | type) == "string") and
        (((.[0].commit.committer.date // .[0].commit.author.date) | type) == "string")
    ' "$commit_json" >/dev/null 2>&1
    then
        api_message="$(jq -r '.message // empty' "$commit_json" 2>/dev/null)"
        if [ -n "$api_message" ]; then
            message="GitHub вернул ошибку: $api_message"
        else
            message="GitHub вернул неожиданный ответ о версии $bundle_name."
        fi
        broray_routes_check_state_error "$message" || true
        broray_routes_check_error "$message"
    fi

    source_commit="$(jq -r '.[0].sha' "$commit_json")"
    source_date="$(jq -r '.[0].commit.committer.date // .[0].commit.author.date' "$commit_json")"

    if ! broray_routes_commit_sha_valid "$source_commit"; then
        message="GitHub вернул некорректный SHA коммита."
        broray_routes_check_state_error "$message" || true
        broray_routes_check_error "$message"
    fi

    if [ "$discovery_mode" = "all-bat" ]; then
        contents_url="https://api.github.com/repos/$repository/contents/$source_directory?ref=$source_commit"

        if ! broray_routes_http_get "$contents_url" "$contents_json"; then
            message="Не удалось прочитать реальный список файлов раздела $bundle_name на GitHub."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        fi

        if ! jq -e 'type == "array"' "$contents_json" >/dev/null 2>&1; then
            api_message="$(jq -r '.message // empty' "$contents_json" 2>/dev/null)"
            if [ -n "$api_message" ]; then
                message="GitHub вернул ошибку при чтении раздела: $api_message"
            else
                message="GitHub вернул неожиданный список файлов раздела $bundle_name."
            fi
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        fi

        jq -r '
            [
                .[] |
                select(.type == "file") |
                select((.name | ascii_downcase | endswith(".bat"))) |
                {type: "windows-route-bat", name: .name}
            ] |
            sort_by(.name | ascii_downcase)[] |
            [.type, .name] |
            @tsv
        ' "$contents_json" >"$enabled_files" || {
            message="Не удалось обработать список .bat-файлов раздела $bundle_name."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        }

        enabled_count="$(wc -l <"$enabled_files" | tr -d ' ')"
        case "$enabled_count" in
            ''|*[!0-9]*) enabled_count=0 ;;
        esac

        [ "$enabled_count" -gt 0 ] || {
            message="В разделе $bundle_name на GitHub не найдено ни одного .bat-файла."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        }

        while IFS="$tab" read -r parser source_file
        do
            [ "$parser" = "windows-route-bat" ] || {
                message="Обнаружен неподдерживаемый тип файла источника."
                broray_routes_check_state_error "$message" || true
                broray_routes_check_error "$message"
            }
            broray_routes_safe_filename "$source_file" || {
                message="GitHub вернул некорректное имя файла источника."
                broray_routes_check_state_error "$message" || true
                broray_routes_check_error "$message"
            }
        done <"$enabled_files"
    fi

    : >"$combined"
    : >"$source_files_tsv"
    total_bytes=0
    index=0

    while IFS="$tab" read -r parser source_file
    do
        index=$((index + 1))
        source_path="$source_directory/$source_file"
        source_raw="$work/source-$index.bat"
        source_part="$work/normalized-$index.txt"
        source_errors="$work/parse-errors-$index.txt"
        raw_url="https://raw.githubusercontent.com/$repository/$source_commit/$source_path"

        if ! broray_routes_http_get "$raw_url" "$source_raw"; then
            message="Не удалось получить файл $source_file для найденной версии."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        fi

        source_bytes="$(wc -c <"$source_raw" | tr -d ' ')"
        case "$source_bytes" in
            ''|*[!0-9]*)
                message="Не удалось определить размер файла $source_file."
                broray_routes_check_state_error "$message" || true
                broray_routes_check_error "$message"
                ;;
        esac

        [ "$source_bytes" -gt 0 ] || {
            message="Файл $source_file пуст."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        }

        total_bytes=$((total_bytes + source_bytes))
        [ "$total_bytes" -le "$max_bytes" ] || {
            message="Общий размер файлов набора превышает разрешённый предел."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        }

        if ! broray_routes_parse_windows_bat "$source_raw" "$source_part" "$source_errors" "$max_routes"; then
            details="$(sed -n '1,8p' "$source_errors" 2>/dev/null)"
            message="Файл $source_file не прошёл проверку."
            [ -z "$details" ] || message="$message $details"
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        fi

        source_file_routes="$(wc -l <"$source_part" | tr -d ' ')"
        source_file_sha="$(sha256sum "$source_raw" | awk '{print $1}')"
        source_html_url="https://github.com/$repository/blob/$source_commit/$source_path"

        printf '%s\t%s\t%s\t%s\t%s\n' \
            "$source_file" "$source_file_sha" "$source_bytes" "$source_file_routes" "$source_html_url" \
            >>"$source_files_tsv" || {
                message="Не удалось записать сведения о файле $source_file."
                broray_routes_check_state_error "$message" || true
                broray_routes_check_error "$message"
            }

        cat "$source_part" >>"$combined" || {
            message="Не удалось объединить маршруты набора."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        }
    done <"$enabled_files"

    LC_ALL=C sort -u "$combined" >"$normalized" || {
        message="Не удалось нормализовать объединённый список маршрутов."
        broray_routes_check_state_error "$message" || true
        broray_routes_check_error "$message"
    }

    jq -R -s '
        split("\n") |
        map(select(length > 0)) |
        map(split("\t") | {
            name: .[0],
            parser: "windows-route-bat",
            sha256: .[1],
            sizeBytes: (.[2] | tonumber),
            routeCount: (.[3] | tonumber),
            htmlUrl: .[4]
        })
    ' "$source_files_tsv" >"$source_files_json" || {
        message="Не удалось сформировать проверенный список файлов источника."
        broray_routes_check_state_error "$message" || true
        broray_routes_check_error "$message"
    }

    route_count="$(wc -l <"$normalized" | tr -d ' ')"
    case "$route_count" in
        ''|*[!0-9]*)
            message="Не удалось определить количество маршрутов."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
            ;;
    esac

    [ "$route_count" -gt 0 ] || {
        message="В наборе не найдено маршрутов."
        broray_routes_check_state_error "$message" || true
        broray_routes_check_error "$message"
    }
    [ "$route_count" -le "$max_routes" ] || {
        message="Количество маршрутов превышает лимит: $route_count > $max_routes"
        broray_routes_check_state_error "$message" || true
        broray_routes_check_error "$message"
    }

    content_sha256="$(sha256sum "$normalized" | awk '{print $1}')"
    jq -r 'sort_by(.name | ascii_downcase)[] | [.name, .sha256, (.sizeBytes | tostring)] | @tsv' \
        "$source_files_json" >"$source_set_tsv" || broray_routes_check_error "Не удалось сформировать отпечаток файлов источника."
    source_set_sha256="$(sha256sum "$source_set_tsv" | awk '{print $1}')"

    catalog="$BRORAY_ROUTES_ROOT/catalog/$bundle_id"
    : >"$baseline_normalized"
    printf '%s\n' '[]' >"$baseline_source_files"
    baseline_source_set=""
    baseline_content_sha=""
    baseline_route_count=0
    downloaded_present=false
    installed_present=false

    if jq -e '.downloadedVersion != null' "$state" >/dev/null 2>&1; then
        downloaded_present=true
        [ -r "$catalog/normalized.txt" ] && [ -r "$catalog/source-files.json" ] && [ -r "$catalog/version.json" ] || {
            message="Локальный каталог скачанной версии отсутствует или повреждён."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        }
        cp "$catalog/normalized.txt" "$baseline_normalized" || broray_routes_check_error "Не удалось прочитать локальный список маршрутов."
        cp "$catalog/source-files.json" "$baseline_source_files" || broray_routes_check_error "Не удалось прочитать локальный список файлов."
        baseline_content_sha="$(sha256sum "$baseline_normalized" | awk '{print $1}')"
        [ "$baseline_content_sha" = "$(jq -r '.downloadedVersion.contentSha256 // empty' "$state")" ] || {
            message="Контрольная сумма локального каталога не совпадает с состоянием."
            broray_routes_check_state_error "$message" || true
            broray_routes_check_error "$message"
        }
        baseline_route_count="$(wc -l <"$baseline_normalized" | tr -d ' ')"
        jq -r 'sort_by(.name | ascii_downcase)[] | [.name, .sha256, (.sizeBytes | tostring)] | @tsv' \
            "$baseline_source_files" >"$work/baseline-source-set.tsv" || broray_routes_check_error "Не удалось проверить локальный список файлов."
        baseline_source_set="$(sha256sum "$work/baseline-source-set.tsv" | awk '{print $1}')"
    fi

    jq -n --slurpfile old "$baseline_source_files" --slurpfile current "$source_files_json" '
        ($old[0] // []) as $o |
        ($current[0] // []) as $n |
        ($o | map({key: .name, value: .}) | from_entries) as $om |
        ($n | map({key: .name, value: .}) | from_entries) as $nm |
        {
            addedFiles: [$n[] | select(($om[.name] // null) == null) | .name],
            removedFiles: [$o[] | select(($nm[.name] // null) == null) | .name],
            changedFiles: [
                $n[] as $f |
                ($om[$f.name] // null) as $p |
                select($p != null and (($p.sha256 != $f.sha256) or ($p.sizeBytes != $f.sizeBytes))) |
                $f.name
            ],
            unchangedFiles: [
                $n[] as $f |
                ($om[$f.name] // null) as $p |
                select($p != null and ($p.sha256 == $f.sha256) and ($p.sizeBytes == $f.sizeBytes)) |
                $f.name
            ]
        }
    ' >"$file_changes_json" || broray_routes_check_error "Не удалось сравнить файлы источника."

    route_added="$(awk 'FILENAME == ARGV[1] {old[$0]=1; next} !($0 in old) {count++} END {print count+0}' "$baseline_normalized" "$normalized")"
    route_removed="$(awk 'FILENAME == ARGV[1] {new[$0]=1; next} !($0 in new) {count++} END {print count+0}' "$normalized" "$baseline_normalized")"
    route_unchanged=$((route_count - route_added))
    [ "$route_unchanged" -ge 0 ] 2>/dev/null || route_unchanged=0

    added_files="$(jq -r '.addedFiles | length' "$file_changes_json")"
    changed_files="$(jq -r '.changedFiles | length' "$file_changes_json")"
    removed_files="$(jq -r '.removedFiles | length' "$file_changes_json")"
    unchanged_files="$(jq -r '.unchangedFiles | length' "$file_changes_json")"

    if [ "$added_files" -gt 0 ] || [ "$changed_files" -gt 0 ] || [ "$removed_files" -gt 0 ]; then
        source_changed=true
    else
        source_changed=false
    fi

    if [ "$route_added" -gt 0 ] || [ "$route_removed" -gt 0 ]; then
        routes_changed=true
    else
        routes_changed=false
    fi

    if [ "$downloaded_present" = false ]; then
        download_required=true
        check_result="initial_available"
        status="available"
        message="Найдено файлов: $enabled_count. Маршрутов: $route_count."
    elif [ "$source_changed" = false ] && [ "$routes_changed" = false ]; then
        download_required=false
        check_result="no_changes"
        status="no_changes"
        message="Проверено файлов: $enabled_count. Новых или изменённых маршрутов нет."
    elif [ "$routes_changed" = false ]; then
        download_required=true
        check_result="source_changed_routes_unchanged"
        status="update_available"
        message="Источник изменился, но итоговый список маршрутов не изменился. Обновление Keenetic не требуется."
    else
        download_required=true
        check_result="changed"
        status="update_available"
        message="На GitHub добавлено $added_files, изменено $changed_files, удалено $removed_files файлов. Маршруты: добавлено $route_added, удалено $route_removed, без изменений $route_unchanged."
    fi

    installed_content_sha="$(jq -r '.installedVersion.contentSha256 // empty' "$state")"
    if [ -n "$installed_content_sha" ]; then
        installed_present=true
    fi
    if [ "$installed_present" = true ] && [ "$installed_content_sha" != "$content_sha256" ]; then
        keenetic_update_required=true
    else
        keenetic_update_required=false
    fi

    now="$(broray_routes_check_now)"
    state_new="$state.new.$$"

    jq \
        --arg bundle_id "$bundle_id" \
        --arg status "$status" \
        --arg check_result "$check_result" \
        --arg source_commit "$source_commit" \
        --arg source_date "$source_date" \
        --arg content_sha256 "$content_sha256" \
        --arg source_set_sha256 "$source_set_sha256" \
        --arg baseline_content_sha "$baseline_content_sha" \
        --arg baseline_source_set "$baseline_source_set" \
        --arg message "$message" \
        --arg now "$now" \
        --arg managed_interface "$managed_interface" \
        --argjson route_count "$route_count" \
        --argjson source_file_count "$enabled_count" \
        --argjson baseline_route_count "$baseline_route_count" \
        --argjson route_added "$route_added" \
        --argjson route_removed "$route_removed" \
        --argjson route_unchanged "$route_unchanged" \
        --argjson source_changed "$source_changed" \
        --argjson routes_changed "$routes_changed" \
        --argjson download_required "$download_required" \
        --argjson keenetic_update_required "$keenetic_update_required" \
        --slurpfile source_files "$source_files_json" \
        --slurpfile file_changes "$file_changes_json" '
            .bundleId = $bundle_id |
            .status = $status |
            .availableVersion = {
                sourceCommit: $source_commit,
                sourceDate: $source_date,
                contentSha256: $content_sha256,
                sourceSetSha256: $source_set_sha256,
                sourceFileCount: $source_file_count,
                sourceFiles: $source_files[0]
            } |
            .routeCount = $route_count |
            .lastCheckedAt = $now |
            .lastError = null |
            .checkResult = {
                result: $check_result,
                baselineContentSha256: (if $baseline_content_sha == "" then null else $baseline_content_sha end),
                currentContentSha256: $content_sha256,
                baselineSourceSetSha256: (if $baseline_source_set == "" then null else $baseline_source_set end),
                currentSourceSetSha256: $source_set_sha256,
                sourceFileCount: $source_file_count,
                sourceFiles: $source_files[0],
                fileChanges: $file_changes[0],
                routeChanges: {
                    before: $baseline_route_count,
                    after: $route_count,
                    added: $route_added,
                    removed: $route_removed,
                    unchanged: $route_unchanged
                },
                sourceChanged: $source_changed,
                routesChanged: $routes_changed,
                downloadRequired: $download_required,
                keeneticUpdateRequired: $keenetic_update_required,
                discoveryMode: "all-bat",
                managedInterface: $managed_interface,
                message: $message
            } |
            .updatedAt = $now
        ' "$state" >"$state_new" || {
            rm -f "$state_new"
            broray_routes_check_error "Не удалось обновить локальное состояние."
        }

    jq -e --arg bundle_id "$bundle_id" --arg managed_interface "$managed_interface" '
        (.schemaVersion == 1) and
        (.bundleId == $bundle_id) and
        ((.status | type) == "string") and
        ((.availableVersion.sourceCommit | type) == "string") and
        ((.availableVersion.sourceDate | type) == "string") and
        ((.availableVersion.contentSha256 | type) == "string") and
        ((.availableVersion.sourceSetSha256 | type) == "string") and
        ((.availableVersion.sourceFileCount | type) == "number") and
        ((.availableVersion.sourceFiles | type) == "array") and
        ((.availableVersion.sourceFiles | length) == .availableVersion.sourceFileCount) and
        ((.routeCount | type) == "number") and
        ((.lastCheckedAt | type) == "string") and
        (.lastError == null) and
        (.checkResult.managedInterface == $managed_interface) and
        ((.checkResult.fileChanges.addedFiles | type) == "array") and
        ((.checkResult.fileChanges.changedFiles | type) == "array") and
        ((.checkResult.fileChanges.removedFiles | type) == "array") and
        ((.checkResult.routeChanges.added | type) == "number") and
        ((.checkResult.routeChanges.removed | type) == "number") and
        ((.checkResult.message | type) == "string")
    ' "$state_new" >/dev/null || {
        rm -f "$state_new"
        broray_routes_check_error "Новое состояние не прошло проверку."
    }

    mv "$state_new" "$state" || broray_routes_check_error "Не удалось установить новое состояние."
    chmod 644 "$state" 2>/dev/null || true

    printf '%s\n' "$message"
    printf 'Набор: %s\n' "$bundle_id"
    printf 'Управляемый интерфейс: %s\n' "$managed_interface"
    printf 'Версия источника: %s\n' "${source_commit%${source_commit#???????}}"
    printf 'Дата источника: %s\n' "$source_date"
    printf 'Файлов источника: %s\n' "$enabled_count"
    printf 'Маршрутов: %s\n' "$route_count"
    printf 'SHA-256 списка маршрутов: %s\n' "$content_sha256"
    printf 'SHA-256 состава файлов: %s\n' "$source_set_sha256"

    broray_routes_check_cleanup
    BRORAY_ROUTES_ACTIVE_WORK=""
    trap - EXIT HUP INT TERM
    return 0
}
