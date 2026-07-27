#!/opt/bin/ash

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_ROUTES_ROOT="${BRORAY_ROUTES_ROOT:-$BRORAY_ROOT/routes}"
BRORAY_ROUTES_DOWNLOAD_SOURCE_LIBRARY="${BRORAY_ROUTES_DOWNLOAD_SOURCE_LIBRARY:-$BRORAY_ROOT/lib/routes-source-check.sh}"
BRORAY_ROUTES_DOWNLOAD_ACTIVE_WORK=""
BRORAY_ROUTES_DOWNLOAD_LOCK="$BRORAY_ROUTES_ROOT/locks/operation.lock"

if [ ! -r "$BRORAY_ROUTES_DOWNLOAD_SOURCE_LIBRARY" ]; then
    echo "ОШИБКА: модуль проверки источника маршрутов недоступен." >&2
    return 1 2>/dev/null || exit 1
fi

. "$BRORAY_ROUTES_DOWNLOAD_SOURCE_LIBRARY"

broray_routes_download_now()
{
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_routes_download_stamp()
{
    date '+%Y%m%d-%H%M%S'
}

broray_routes_download_error()
{
    printf '%s\n' "$*" >&2
    exit 1
}

broray_routes_download_hash_valid()
{
    local value

    value="${1:-}"

    [ "${#value}" -eq 64 ] || return 1

    case "$value" in
        *[!0-9a-fA-F]*) return 1 ;;
    esac

    return 0
}

broray_routes_download_lock_acquire()
{
    local lock_parent lock_pid

    lock_parent="$(dirname "$BRORAY_ROUTES_DOWNLOAD_LOCK")"
    mkdir -p "$lock_parent" || return 1

    if mkdir "$BRORAY_ROUTES_DOWNLOAD_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_ROUTES_DOWNLOAD_LOCK/pid"
        printf '%s\n' "download" >"$BRORAY_ROUTES_DOWNLOAD_LOCK/action"
        printf '%s\n' "$BRORAY_ROUTES_ACTIVE_BUNDLE" >"$BRORAY_ROUTES_DOWNLOAD_LOCK/bundle"
        return 0
    fi

    lock_pid="$(sed -n '1p' "$BRORAY_ROUTES_DOWNLOAD_LOCK/pid" 2>/dev/null)"

    if broray_routes_is_pid "$lock_pid" &&
       kill -0 "$lock_pid" 2>/dev/null
    then
        return 2
    fi

    rm -rf "$BRORAY_ROUTES_DOWNLOAD_LOCK" 2>/dev/null || return 1

    if mkdir "$BRORAY_ROUTES_DOWNLOAD_LOCK" 2>/dev/null; then
        printf '%s\n' "$$" >"$BRORAY_ROUTES_DOWNLOAD_LOCK/pid"
        printf '%s\n' "download" >"$BRORAY_ROUTES_DOWNLOAD_LOCK/action"
        printf '%s\n' "$BRORAY_ROUTES_ACTIVE_BUNDLE" >"$BRORAY_ROUTES_DOWNLOAD_LOCK/bundle"
        return 0
    fi

    return 1
}

broray_routes_download_lock_release()
{
    rm -rf "$BRORAY_ROUTES_DOWNLOAD_LOCK" 2>/dev/null || true
}

broray_routes_download_cleanup()
{
    if [ -n "$BRORAY_ROUTES_DOWNLOAD_ACTIVE_WORK" ]; then
        rm -rf "$BRORAY_ROUTES_DOWNLOAD_ACTIVE_WORK" 2>/dev/null || true
    fi

    broray_routes_download_lock_release
}

broray_routes_download_state_error()
{
    local state message now state_new

    state="$1"
    message="$2"
    now="$(broray_routes_download_now)"
    state_new="$state.new.$$"

    jq \
        --arg now "$now" \
        --arg message "$message" \
        '
            .status = "download_error" |
            .lastError = {
                message: $message,
                at: $now
            } |
            .updatedAt = $now
        ' \
        "$state" >"$state_new" || {
            rm -f "$state_new"
            return 1
        }

    mv "$state_new" "$state"
}

broray_routes_download_restore_catalog()
{
    local catalog_target catalog_backup had_catalog

    catalog_target="$1"
    catalog_backup="$2"
    had_catalog="$3"

    rm -rf "$catalog_target" 2>/dev/null || true

    if [ "$had_catalog" = "true" ] &&
       [ -d "$catalog_backup" ]
    then
        mv "$catalog_backup" "$catalog_target" 2>/dev/null || true
    fi
}

broray_routes_download_run()
{
    local bundle_id manifest state bundle_name repository source_directory managed_interface route_comment discovery_mode
    local max_bytes max_routes source_commit source_date expected_sha expected_source_set expected_count
    local enabled_files enabled_count unsupported tab work catalog_work source_dir combined normalized metadata_tsv source_set_tsv
    local index parser source_file source_path source_raw source_part source_errors raw_url source_bytes total_bytes
    local route_count actual_sha actual_source_set details message lock_result now version_json routes_json source_files_json
    local source_file_routes source_html_url
    local catalog_parent catalog_target catalog_new catalog_backup had_catalog state_new previous_download_sha previous_download_source_set result
    local installed_content_sha keenetic_update_required status_value

    bundle_id="${1:-}"

    broray_routes_check_bind_bundle "$bundle_id"

    manifest="$BRORAY_ROUTES_MANIFEST"
    state="$BRORAY_ROUTES_STATE"
    bundle_name="$(jq -r '.name // .id' "$manifest")"
    repository="$(jq -r '.source.repository' "$manifest")"
    source_directory="$(jq -r '.source.directory' "$manifest")"
    managed_interface="$(jq -r '.targetInterface' "$manifest")"
    route_comment="$(jq -r '.exportComment // "BROray"' "$manifest")"
    discovery_mode="$(jq -r '.source.discovery.mode // "manifest"' "$manifest")"
    max_bytes="$(jq -r '.limits.maxSourceBytes' "$manifest")"
    max_routes="$(jq -r '.limits.maxRoutes' "$manifest")"

    source_commit="$(jq -r '.availableVersion.sourceCommit // empty' "$state")"
    source_date="$(jq -r '.availableVersion.sourceDate // empty' "$state")"
    expected_sha="$(jq -r '.availableVersion.contentSha256 // empty' "$state")"
    expected_source_set="$(jq -r '.availableVersion.sourceSetSha256 // empty' "$state")"
    expected_count="$(jq -r '.routeCount // empty' "$state")"

    broray_routes_safe_repository "$repository" ||
        broray_routes_download_error "Манифест содержит некорректный репозиторий."

    broray_routes_safe_directory "$source_directory" ||
        broray_routes_download_error "Манифест содержит некорректный каталог источника."

    broray_routes_commit_sha_valid "$source_commit" ||
        broray_routes_download_error "Сначала выполните проверку новых маршрутов."

    [ -n "$source_date" ] ||
        broray_routes_download_error "В локальном состоянии отсутствует дата версии."

    broray_routes_download_hash_valid "$expected_sha" ||
        broray_routes_download_error "В локальном состоянии отсутствует корректный хеш версии."

    if [ -n "$expected_source_set" ]; then
        broray_routes_download_hash_valid "$expected_source_set" ||
            broray_routes_download_error "В локальном состоянии указан некорректный отпечаток файлов источника."
    fi

    case "$expected_count" in
        ''|*[!0-9]*)
            broray_routes_download_error "В локальном состоянии отсутствует количество маршрутов."
            ;;
    esac

    case "$max_bytes" in
        ''|*[!0-9]*) broray_routes_download_error "Некорректный лимит размера источника." ;;
    esac

    case "$max_routes" in
        ''|*[!0-9]*) broray_routes_download_error "Некорректный лимит маршрутов." ;;
    esac

    lock_result=0
    broray_routes_download_lock_acquire || lock_result=$?

    case "$lock_result" in
        0) ;;
        2) broray_routes_download_error "Другая операция с маршрутами уже выполняется." ;;
        *) broray_routes_download_error "Не удалось установить блокировку операции." ;;
    esac

    work="$BRORAY_ROUTES_ROOT/tmp/download-$bundle_id.$$"
    BRORAY_ROUTES_DOWNLOAD_ACTIVE_WORK="$work"
    catalog_work="$work/catalog"
    source_dir="$catalog_work/source"
    enabled_files="$work/enabled-files.tsv"
    combined="$work/combined.txt"
    normalized="$work/normalized.txt"
    metadata_tsv="$work/source-files.tsv"

    mkdir -p "$source_dir" || {
        broray_routes_download_lock_release
        broray_routes_download_error "Не удалось создать временный каталог загрузки."
    }

    trap 'broray_routes_download_cleanup' EXIT HUP INT TERM

    if [ "$discovery_mode" = "all-bat" ]; then
        if ! jq -e '
            (.availableVersion.sourceFiles | type) == "array" and
            (.availableVersion.sourceFiles | length) > 0
        ' "$state" >/dev/null 2>&1
        then
            message="Сначала выполните проверку источника, чтобы получить реальный список всех .bat-файлов."
            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
        fi

        jq -r '
            .availableVersion.sourceFiles[] |
            [(.parser // "windows-route-bat"), (.name // "")] |
            @tsv
        ' "$state" >"$enabled_files" || {
            message="Не удалось прочитать проверенный список файлов источника."
            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
        }
    else
        jq -r '
            .source.files[] |
            select(.enabled == true) |
            [(.type // .parser // ""), (.name // "")] |
            @tsv
        ' "$manifest" >"$enabled_files" || {
            message="Не удалось прочитать список файлов источника."
            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
        }
    fi

    enabled_count="$(wc -l <"$enabled_files" | tr -d ' ')"
    tab="$(printf '\t')"

    case "$enabled_count" in
        ''|*[!0-9]*) enabled_count=0 ;;
    esac

    [ "$enabled_count" -gt 0 ] || {
        message="В манифесте нет включённых файлов маршрутов."
        broray_routes_download_state_error "$state" "$message" || true
        broray_routes_download_error "$message"
    }

    unsupported="$(awk -F '\t' '$1 != "windows-route-bat" {print $1}' "$enabled_files" | sed -n '1p')"

    [ -z "$unsupported" ] || {
        message="Манифест содержит неподдерживаемый включённый парсер: $unsupported"
        broray_routes_download_state_error "$state" "$message" || true
        broray_routes_download_error "$message"
    }

    : >"$combined"
    : >"$metadata_tsv"
    total_bytes=0
    index=0

    while IFS="$tab" read -r parser source_file
    do
        broray_routes_safe_filename "$source_file" || {
            message="Манифест содержит некорректное имя файла источника."
            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
        }

        index=$((index + 1))
        source_path="$source_directory/$source_file"
        source_raw="$work/source-$index.raw"
        source_part="$work/normalized-$index.txt"
        source_errors="$work/parse-errors-$index.txt"
        raw_url="https://raw.githubusercontent.com/$repository/$source_commit/$source_path"

        if ! broray_routes_http_get "$raw_url" "$source_raw"; then
            message="Не удалось скачать файл $source_file зафиксированной версии."
            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
        fi

        source_bytes="$(wc -c <"$source_raw" | tr -d ' ')"

        case "$source_bytes" in
            ''|*[!0-9]*)
                message="Не удалось определить размер файла $source_file."
                broray_routes_download_state_error "$state" "$message" || true
                broray_routes_download_error "$message"
                ;;
        esac

        [ "$source_bytes" -gt 0 ] || {
            message="Файл $source_file пуст."
            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
        }

        total_bytes=$((total_bytes + source_bytes))

        [ "$total_bytes" -le "$max_bytes" ] || {
            message="Общий размер файлов набора превышает разрешённый предел."
            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
        }

        if ! broray_routes_parse_windows_bat \
            "$source_raw" \
            "$source_part" \
            "$source_errors" \
            "$max_routes"
        then
            details="$(sed -n '1,8p' "$source_errors" 2>/dev/null)"
            message="Файл $source_file не прошёл проверку."

            if [ -n "$details" ]; then
                message="$message $details"
            fi

            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
        fi

        cp "$source_raw" "$source_dir/$source_file" || {
            message="Не удалось сохранить исходный файл $source_file."
            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
        }

        source_file_sha="$(sha256sum "$source_raw" | awk '{print $1}')"
        source_file_routes="$(wc -l <"$source_part" | tr -d ' ')"
        source_html_url="https://github.com/$repository/blob/$source_commit/$source_path"

        printf '%s\t%s\t%s\t%s\t%s\n' \
            "$source_file" \
            "$source_file_sha" \
            "$source_bytes" \
            "$source_file_routes" \
            "$source_html_url" >>"$metadata_tsv"

        cat "$source_part" >>"$combined" || {
            message="Не удалось объединить маршруты набора."
            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
        }
    done <"$enabled_files"

    LC_ALL=C sort -u "$combined" >"$normalized" || {
        message="Не удалось нормализовать объединённый список маршрутов."
        broray_routes_download_state_error "$state" "$message" || true
        broray_routes_download_error "$message"
    }

    route_count="$(wc -l <"$normalized" | tr -d ' ')"

    case "$route_count" in
        ''|*[!0-9]*)
            message="Не удалось определить количество маршрутов."
            broray_routes_download_state_error "$state" "$message" || true
            broray_routes_download_error "$message"
            ;;
    esac

    [ "$route_count" -gt 0 ] || {
        message="В наборе не найдено маршрутов."
        broray_routes_download_state_error "$state" "$message" || true
        broray_routes_download_error "$message"
    }

    [ "$route_count" -le "$max_routes" ] || {
        message="Количество маршрутов превышает лимит: $route_count > $max_routes"
        broray_routes_download_state_error "$state" "$message" || true
        broray_routes_download_error "$message"
    }

    [ "$route_count" -eq "$expected_count" ] || {
        message="Количество маршрутов скачанной версии не совпало с результатом проверки."
        broray_routes_download_state_error "$state" "$message" || true
        broray_routes_download_error "$message"
    }

    actual_sha="$(sha256sum "$normalized" | awk '{print $1}')"

    [ "$actual_sha" = "$expected_sha" ] || {
        message="Содержимое источника не совпало с проверенной версией. Экспорт запрещён."
        broray_routes_download_state_error "$state" "$message" || true
        broray_routes_download_error "$message"
    }

    cp "$normalized" "$catalog_work/normalized.txt" ||
        broray_routes_download_error "Не удалось сохранить нормализованный список."

    cp "$manifest" "$catalog_work/manifest.json" ||
        broray_routes_download_error "Не удалось сохранить снимок манифеста."

    source_files_json="$work/source-files.json"
    jq -R -s '
        split("\n") |
        map(select(length > 0)) |
        map(
            split("\t") |
            {
                name: .[0],
                parser: "windows-route-bat",
                sha256: .[1],
                sizeBytes: (.[2] | tonumber),
                routeCount: (.[3] | tonumber),
                htmlUrl: .[4]
            }
        )
    ' "$metadata_tsv" >"$source_files_json" ||
        broray_routes_download_error "Не удалось сформировать сведения о файлах источника."

    mv "$source_files_json" "$catalog_work/source-files.json" ||
        broray_routes_download_error "Не удалось сохранить сведения о файлах источника."

    source_set_tsv="$work/source-set.tsv"
    jq -r '
        sort_by(.name | ascii_downcase)[] |
        [.name, .sha256, (.sizeBytes | tostring)] |
        @tsv
    ' "$catalog_work/source-files.json" >"$source_set_tsv" ||
        broray_routes_download_error "Не удалось сформировать отпечаток файлов источника."

    actual_source_set="$(sha256sum "$source_set_tsv" | awk '{print $1}')"

    if [ -n "$expected_source_set" ] && [ "$actual_source_set" != "$expected_source_set" ]; then
        message="Состав файлов источника изменился после проверки. Выполните поиск обновлений повторно."
        broray_routes_download_state_error "$state" "$message" || true
        broray_routes_download_error "$message"
    fi

    routes_json="$catalog_work/routes.json"
    jq -Rn \
        --arg bundle_id "$bundle_id" \
        --arg target_interface "$managed_interface" \
        --arg route_comment "$route_comment" \
        --arg content_sha256 "$actual_sha" \
        '
            [
                inputs |
                select(length > 0) |
                split("/") as $parts |
                {
                    family: "ipv4",
                    network: $parts[0],
                    prefix: ($parts[1] | tonumber)
                }
            ] as $routes |
            {
                schemaVersion: 1,
                bundleId: $bundle_id,
                targetInterface: $target_interface,
                routeComment: $route_comment,
                contentSha256: $content_sha256,
                routeCount: ($routes | length),
                routes: $routes
            }
        ' <"$normalized" >"$routes_json" ||
        broray_routes_download_error "Не удалось сформировать локальный каталог маршрутов."

    now="$(broray_routes_download_now)"
    version_json="$catalog_work/version.json"

    jq -n \
        --arg bundle_id "$bundle_id" \
        --arg source_commit "$source_commit" \
        --arg source_date "$source_date" \
        --arg content_sha256 "$actual_sha" \
        --arg source_set_sha256 "$actual_source_set" \
        --arg downloaded_at "$now" \
        --arg managed_interface "$managed_interface" \
        --argjson route_count "$route_count" \
        --argjson source_file_count "$enabled_count" \
        --slurpfile source_files "$catalog_work/source-files.json" \
        '
            {
                schemaVersion: 1,
                bundleId: $bundle_id,
                sourceCommit: $source_commit,
                sourceDate: $source_date,
                contentSha256: $content_sha256,
                sourceSetSha256: $source_set_sha256,
                routeCount: $route_count,
                sourceFileCount: $source_file_count,
                sourceFiles: $source_files[0],
                downloadedAt: $downloaded_at,
                targetInterface: $managed_interface
            }
        ' >"$version_json" ||
        broray_routes_download_error "Не удалось сформировать сведения о версии."

    jq -e \
        --arg bundle_id "$bundle_id" \
        --arg target_interface "$managed_interface" \
        --arg content_sha256 "$actual_sha" \
        --argjson route_count "$route_count" \
        '
            (.schemaVersion == 1) and
            (.bundleId == $bundle_id) and
            (.targetInterface == $target_interface) and
            (.contentSha256 == $content_sha256) and
            (.routeCount == $route_count) and
            ((.routes | type) == "array") and
            ((.routes | length) == $route_count) and
            (all(.routes[];
                (.family == "ipv4") and
                ((.network | type) == "string") and
                ((.prefix | type) == "number") and
                (.prefix >= 1) and
                (.prefix <= 32)
            ))
        ' "$routes_json" >/dev/null ||
        broray_routes_download_error "Локальный каталог маршрутов не прошёл проверку."

    jq -e \
        --arg bundle_id "$bundle_id" \
        --arg source_commit "$source_commit" \
        --arg content_sha256 "$actual_sha" \
        --arg source_set_sha256 "$actual_source_set" \
        --arg managed_interface "$managed_interface" \
        --argjson route_count "$route_count" \
        '
            (.schemaVersion == 1) and
            (.bundleId == $bundle_id) and
            (.sourceCommit == $source_commit) and
            (.contentSha256 == $content_sha256) and
            ((.sourceSetSha256 | type) == "string") and
            (.routeCount == $route_count) and
            (.targetInterface == $managed_interface)
        ' "$version_json" >/dev/null ||
        broray_routes_download_error "Сведения о версии не прошли проверку."

    catalog_parent="$BRORAY_ROUTES_ROOT/catalog"
    catalog_target="$catalog_parent/$bundle_id"
    catalog_new="$catalog_parent/.$bundle_id.new.$$"
    catalog_backup="$BRORAY_ROUTES_ROOT/backup/catalog-$bundle_id-$(broray_routes_download_stamp)-$$"
    had_catalog=false

    mkdir -p "$catalog_parent" "$BRORAY_ROUTES_ROOT/backup" ||
        broray_routes_download_error "Не удалось подготовить каталог хранения."

    rm -rf "$catalog_new"
    mv "$catalog_work" "$catalog_new" ||
        broray_routes_download_error "Не удалось подготовить атомарную замену каталога."

    if [ -d "$catalog_target" ]; then
        had_catalog=true
        mv "$catalog_target" "$catalog_backup" || {
            rm -rf "$catalog_new"
            broray_routes_download_error "Не удалось сохранить предыдущий локальный каталог."
        }
    fi

    if ! mv "$catalog_new" "$catalog_target"; then
        broray_routes_download_restore_catalog "$catalog_target" "$catalog_backup" "$had_catalog"
        broray_routes_download_error "Не удалось установить новый локальный каталог."
    fi

    previous_download_sha="$(jq -r '.downloadedVersion.contentSha256 // empty' "$state")"
    previous_download_source_set="$(jq -r '.downloadedVersion.sourceSetSha256 // empty' "$state")"
    installed_content_sha="$(jq -r '.installedVersion.contentSha256 // empty' "$state")"

    if [ -n "$installed_content_sha" ] && [ "$installed_content_sha" = "$actual_sha" ]; then
        keenetic_update_required=false
        status_value="installed"
    else
        keenetic_update_required=true
        status_value="downloaded"
    fi

    if [ "$previous_download_sha" = "$actual_sha" ] &&
       [ "$previous_download_source_set" = "$actual_source_set" ]
    then
        result="already_downloaded"
        message="Проверенная версия файлов уже загружена."
    elif [ -n "$installed_content_sha" ] && [ "$installed_content_sha" = "$actual_sha" ]; then
        result="files_updated_routes_unchanged"
        message="Файлы маршрутов обновлены. Итоговый список маршрутов не изменился; обновление Keenetic не требуется."
    elif [ -n "$installed_content_sha" ]; then
        result="updated"
        message="Файлы маршрутов обновлены. Набор готов к обновлению в Keenetic."
    else
        result="downloaded"
        message="Файлы маршрутов загружены. Набор готов к установке в Keenetic."
    fi

    state_new="$state.new.$$"

    if ! jq \
        --arg status "$status_value" \
        --arg source_commit "$source_commit" \
        --arg source_date "$source_date" \
        --arg content_sha256 "$actual_sha" \
        --arg source_set_sha256 "$actual_source_set" \
        --arg now "$now" \
        --arg result "$result" \
        --arg message "$message" \
        --arg catalog_path "$catalog_target" \
        --arg managed_interface "$managed_interface" \
        --argjson route_count "$route_count" \
        --argjson source_file_count "$enabled_count" \
        --argjson keenetic_update_required "$keenetic_update_required" \
        --slurpfile source_files "$catalog_target/source-files.json" \
        '
            .status = $status |
            .downloadedVersion = {
                sourceCommit: $source_commit,
                sourceDate: $source_date,
                contentSha256: $content_sha256,
                sourceSetSha256: $source_set_sha256,
                sourceFileCount: $source_file_count,
                sourceFiles: $source_files[0]
            } |
            .routeCount = $route_count |
            .lastDownloadedAt = $now |
            .lastError = null |
            .downloadResult = {
                result: $result,
                message: $message,
                catalogPath: $catalog_path,
                managedInterface: $managed_interface,
                routeCount: $route_count,
                sourceFileCount: $source_file_count,
                sourceSetSha256: $source_set_sha256,
                keeneticUpdateRequired: $keenetic_update_required
            } |
            .updatedAt = $now
        ' "$state" >"$state_new"
    then
        rm -f "$state_new"
        broray_routes_download_restore_catalog "$catalog_target" "$catalog_backup" "$had_catalog"
        broray_routes_download_error "Не удалось обновить локальное состояние загрузки."
    fi

    if ! jq -e \
        --arg bundle_id "$bundle_id" \
        --arg source_commit "$source_commit" \
        --arg content_sha256 "$actual_sha" \
        --arg source_set_sha256 "$actual_source_set" \
        --arg managed_interface "$managed_interface" \
        --argjson route_count "$route_count" \
        '
            (.schemaVersion == 1) and
            (.bundleId == $bundle_id) and
            ((.status == "downloaded") or (.status == "installed")) and
            (.downloadedVersion.sourceCommit == $source_commit) and
            (.downloadedVersion.contentSha256 == $content_sha256) and
            (.downloadedVersion.sourceSetSha256 == $source_set_sha256) and
            (.routeCount == $route_count) and
            (.lastError == null) and
            (.downloadResult.managedInterface == $managed_interface)
        ' "$state_new" >/dev/null
    then
        rm -f "$state_new"
        broray_routes_download_restore_catalog "$catalog_target" "$catalog_backup" "$had_catalog"
        broray_routes_download_error "Новое состояние загрузки не прошло проверку."
    fi

    if ! mv "$state_new" "$state"; then
        rm -f "$state_new"
        broray_routes_download_restore_catalog "$catalog_target" "$catalog_backup" "$had_catalog"
        broray_routes_download_error "Не удалось установить состояние загрузки."
    fi

    chmod 644 "$state" 2>/dev/null || true

    printf '%s\n' "$message"
    printf 'Набор: %s\n' "$bundle_id"
    printf 'Версия источника: %s\n' "${source_commit%${source_commit#???????}}"
    printf 'Маршрутов: %s\n' "$route_count"
    printf 'Управляемый интерфейс: %s\n' "$managed_interface"
    printf 'Локальный каталог: %s\n' "$catalog_target"

    broray_routes_download_cleanup
    BRORAY_ROUTES_DOWNLOAD_ACTIVE_WORK=""
    trap - EXIT HUP INT TERM

    return 0
}
