#!/bin/sh

BRORAY_XRAY_RELEASES_API="${BRORAY_XRAY_RELEASES_API:-https://api.github.com/repos/XTLS/Xray-core/releases?per_page=5}"
BRORAY_XRAY_RELEASE_ASSET="${BRORAY_XRAY_RELEASE_ASSET:-Xray-linux-arm64-v8a.zip}"
BRORAY_XRAY_RELEASE_DIGEST="${BRORAY_XRAY_RELEASE_DIGEST:-Xray-linux-arm64-v8a.zip.dgst}"
BRORAY_XRAY_RELEASE_CACHE="${BRORAY_XRAY_RELEASE_CACHE:-/tmp/broray-xray-releases.json}"

broray_xray_version_number()
{
    broray_version_binary="${1:-$BRORAY_XRAY_BINARY}"

    [ -n "$broray_version_binary" ] || return 1
    [ -x "$broray_version_binary" ] || return 1

    XRAY_LOCATION_ASSET="$BRORAY_XRAY_ASSET_DIR" \
        "$broray_version_binary" version 2>/dev/null |
        sed -n '1s/^Xray \([^ ]*\).*/\1/p'
}

broray_xray_version_key()
{
    broray_key_version="$1"

    broray_key_old_ifs="$IFS"
    IFS='.'
    set -- $broray_key_version
    IFS="$broray_key_old_ifs"

    broray_key_year="${1:-0}"
    broray_key_month="${2:-0}"
    broray_key_day="${3:-0}"

    case "$broray_key_year" in
        ''|*[!0-9]*)
            broray_key_year=0
            ;;
    esac

    case "$broray_key_month" in
        ''|*[!0-9]*)
            broray_key_month=0
            ;;
    esac

    case "$broray_key_day" in
        ''|*[!0-9]*)
            broray_key_day=0
            ;;
    esac

    printf '%04d%02d%02d\n' \
        "$broray_key_year" \
        "$broray_key_month" \
        "$broray_key_day"
}

broray_xray_update_fetch_releases()
{
    broray_release_tmp="${BRORAY_XRAY_RELEASE_CACHE}.tmp"

    rm -f "$broray_release_tmp"

    curl -fsSL \
        --connect-timeout 15 \
        --max-time 120 \
        -H 'Accept: application/vnd.github+json' \
        -H 'User-Agent: BROray/2.0.0' \
        "$BRORAY_XRAY_RELEASES_API" \
        >"$broray_release_tmp" ||
        {
            rm -f "$broray_release_tmp"
            return 1
        }

    jq -e '
        type == "array" and length > 0
    ' "$broray_release_tmp" >/dev/null 2>&1 ||
        {
            rm -f "$broray_release_tmp"
            return 1
        }

    mv "$broray_release_tmp" \
       "$BRORAY_XRAY_RELEASE_CACHE"
}

broray_xray_update_latest_release()
{
    jq -c \
        --arg assetName "$BRORAY_XRAY_RELEASE_ASSET" \
        --arg digestName "$BRORAY_XRAY_RELEASE_DIGEST" '
        [
            .[]
            | select(.draft == false)
            | select(
                [
                    .assets[]
                    | select(.name == $assetName)
                ]
                | length > 0
            )
            | {
                tagName: .tag_name,
                prerelease: .prerelease,
                publishedAt: .published_at,
                asset:
                    (
                        [
                            .assets[]
                            | select(.name == $assetName)
                            | {
                                name: .name,
                                size: .size,
                                url: .browser_download_url
                            }
                        ][0]
                    ),
                digest:
                    (
                        [
                            .assets[]
                            | select(.name == $digestName)
                            | {
                                name: .name,
                                size: .size,
                                url: .browser_download_url
                            }
                        ][0]
                    )
            }
        ][0]
    ' "$BRORAY_XRAY_RELEASE_CACHE"
}

broray_xray_update_check()
{
    broray_current_version="$(
        broray_xray_version_number \
            "$BRORAY_XRAY_BINARY"
    )"

    if [ -z "$broray_current_version" ]; then
        jq -n '
            {
                success: false,
                error:
                    "Не удалось определить установленную версию Xray."
            }
        '
        return 1
    fi

    if ! broray_xray_update_fetch_releases; then
        jq -n \
            --arg currentVersion \
                "$broray_current_version" '
            {
                success: false,
                currentVersion: $currentVersion,
                error:
                    "Не удалось получить список официальных релизов Xray."
            }
        '
        return 1
    fi

    broray_release_json="$(
        broray_xray_update_latest_release
    )"

    broray_latest_tag="$(
        printf '%s\n' "$broray_release_json" |
            jq -r '.tagName // empty'
    )"

    case "$broray_latest_tag" in
        v*)
            broray_latest_version="${broray_latest_tag#v}"
            ;;
        *)
            broray_latest_version="$broray_latest_tag"
            ;;
    esac

    if [ -z "$broray_latest_version" ]; then
        jq -n \
            --arg currentVersion \
                "$broray_current_version" '
            {
                success: false,
                currentVersion: $currentVersion,
                error:
                    "Не найден официальный архив Xray для Linux ARM64."
            }
        '
        return 1
    fi

    broray_current_key="$(
        broray_xray_version_key \
            "$broray_current_version"
    )"

    broray_latest_key="$(
        broray_xray_version_key \
            "$broray_latest_version"
    )"

    broray_update_available=false
    broray_installed_newer=false

    if [ "$broray_latest_key" -gt "$broray_current_key" ]; then
        broray_update_available=true
    elif [ "$broray_current_key" -gt "$broray_latest_key" ]; then
        broray_installed_newer=true
    fi

    broray_storage_free_kb="$(
        df -k /opt 2>/dev/null |
            sed -n '2s/  */ /gp' |
            cut -d ' ' -f 4
    )"

    broray_tmp_free_kb="$(
        df -k /tmp 2>/dev/null |
            sed -n '2s/  */ /gp' |
            cut -d ' ' -f 4
    )"

    case "$broray_storage_free_kb" in
        ''|*[!0-9]*)
            broray_storage_free_kb=0
            ;;
    esac

    case "$broray_tmp_free_kb" in
        ''|*[!0-9]*)
            broray_tmp_free_kb=0
            ;;
    esac

    broray_storage_free_bytes=$(
        expr "$broray_storage_free_kb" \* 1024
    )

    broray_tmp_free_bytes=$(
        expr "$broray_tmp_free_kb" \* 1024
    )

    printf '%s\n' "$broray_release_json" |
        jq \
            --arg currentVersion \
                "$broray_current_version" \
            --arg latestVersion \
                "$broray_latest_version" \
            --argjson updateAvailable \
                "$broray_update_available" \
            --argjson installedNewer \
                "$broray_installed_newer" \
            --argjson storageFreeBytes \
                "$broray_storage_free_bytes" \
            --argjson temporaryFreeBytes \
                "$broray_tmp_free_bytes" '
            {
                success: true,
                currentVersion: $currentVersion,
                latestVersion: $latestVersion,
                latestTag: .tagName,
                channel:
                    (
                        if .prerelease
                        then "pre-release"
                        else "stable"
                        end
                    ),
                prerelease: .prerelease,
                publishedAt: .publishedAt,
                updateAvailable: $updateAvailable,
                installedNewer: $installedNewer,
                asset: .asset,
                digest: .digest,
                storage: {
                    freeBytes: $storageFreeBytes
                },
                temporaryStorage: {
                    freeBytes: $temporaryFreeBytes
                },
                message:
                    (
                        if $updateAvailable
                        then
                            "Доступна новая версия Xray."
                        elif $installedNewer
                        then
                            "Установленная версия новее последнего официального релиза."
                        else
                            "Установлена актуальная версия Xray."
                        end
                    )
            }
        '
}


# BROray safe Xray installer v2

BRORAY_XRAY_UPDATE_STATE="/opt/broray/update"
BRORAY_XRAY_UPDATE_LOCK="$BRORAY_XRAY_UPDATE_STATE/xray.lock"
BRORAY_XRAY_UPDATE_WORK="/tmp/broray-xray-update"

broray_xray_update_error()
{
    jq -n \
        --arg error "$1" '
        {
            success: false,
            error: $error
        }
    '

    return 1
}

broray_xray_update_numeric()
{
    case "$1" in
        ''|*[!0-9]*)
            printf '0\n'
            ;;
        *)
            printf '%s\n' "$1"
            ;;
    esac
}

broray_xray_update_free_bytes()
{
    broray_xray_update_free_path="$1"

    broray_xray_update_free_kb="$(
        df -k "$broray_xray_update_free_path" \
            2>/dev/null |
            awk 'NR == 2 {print $4}'
    )"

    broray_xray_update_free_kb="$(
        broray_xray_update_numeric \
            "$broray_xray_update_free_kb"
    )"

    expr "$broray_xray_update_free_kb" \* 1024
}

broray_xray_update_lock_acquire()
{
    mkdir -p "$BRORAY_XRAY_UPDATE_STATE" ||
        return 1

    if mkdir "$BRORAY_XRAY_UPDATE_LOCK" \
        2>/dev/null
    then
        printf '%s\n' "$$" \
            > "$BRORAY_XRAY_UPDATE_LOCK/pid"

        return 0
    fi

    broray_xray_update_lock_pid=""

    if [ -f "$BRORAY_XRAY_UPDATE_LOCK/pid" ]; then
        broray_xray_update_lock_pid="$(
            cat "$BRORAY_XRAY_UPDATE_LOCK/pid"
        )"
    fi

    case "$broray_xray_update_lock_pid" in
        ''|*[!0-9]*)
            rm -rf "$BRORAY_XRAY_UPDATE_LOCK"
            ;;
        *)
            if ! kill -0 \
                "$broray_xray_update_lock_pid" \
                2>/dev/null
            then
                rm -rf "$BRORAY_XRAY_UPDATE_LOCK"
            fi
            ;;
    esac

    if mkdir "$BRORAY_XRAY_UPDATE_LOCK" \
        2>/dev/null
    then
        printf '%s\n' "$$" \
            > "$BRORAY_XRAY_UPDATE_LOCK/pid"

        return 0
    fi

    return 1
}

broray_xray_update_lock_release()
{
    rm -rf "$BRORAY_XRAY_UPDATE_LOCK"
}

broray_xray_update_work_clean()
{
    rm -rf "$BRORAY_XRAY_UPDATE_WORK"
}

broray_xray_update_expected_sha256()
{
    awk '
        {
            line = tolower($0)
        }

        line ~ /^sha2-256[= ]/ {
            print $NF
            exit
        }
    ' "$1" |
        tr -d '\r'
}

broray_xray_update_validate_sha256()
{
    broray_xray_update_hash="$1"

    broray_xray_update_hash_length="$(
        printf '%s' "$broray_xray_update_hash" |
            wc -c |
            tr -d ' '
    )"

    broray_xray_update_hash_invalid="$(
        printf '%s' "$broray_xray_update_hash" |
            tr -d '0123456789abcdefABCDEF'
    )"

    [ "$broray_xray_update_hash_length" -eq 64 ] &&
        [ -z "$broray_xray_update_hash_invalid" ]
}

broray_xray_update_restore_binary()
{
    broray_xray_restore_backup="$1"
    broray_xray_restore_was_running="$2"

    broray_xray_stop >/dev/null 2>&1 || true

    rm -f \
        "$BRORAY_XRAY_BINARY" \
        "$BRORAY_XRAY_BINARY.new"

    if ! cp "$broray_xray_restore_backup" \
        "$BRORAY_XRAY_BINARY"
    then
        return 1
    fi

    chmod 755 "$BRORAY_XRAY_BINARY" ||
        return 1

    if [ "$broray_xray_restore_was_running" = "true" ]; then
        broray_xray_start >/dev/null 2>&1 &&
            broray_xray_wait_running
        return $?
    fi

    return 0
}

broray_xray_update_success_cleanup()
{
    broray_xray_update_work_clean
    broray_xray_update_lock_release

    if [ -n "${BRORAY_XRAY_RELEASE_CACHE:-}" ]; then
        case "$BRORAY_XRAY_RELEASE_CACHE" in
            /opt/broray/*)
                rm -f "$BRORAY_XRAY_RELEASE_CACHE"
                ;;
        esac
    fi

    rm -rf \
        /opt/broray/update/xray \
        /opt/broray/backup/xray-binaries

    rm -f \
        /tmp/Xray-linux-arm64-v8a.zip \
        /tmp/Xray-linux-arm64-v8a.zip.dgst
}

broray_xray_update_install()
{
    broray_xray_update_mode="${1:-update}"

    case "$broray_xray_update_mode" in
        update|reinstall)
            ;;
        *)
            broray_xray_update_error \
                "Неизвестный режим установки."
            return 1
            ;;
    esac

    if ! broray_xray_update_lock_acquire; then
        broray_xray_update_error \
            "Другая операция обновления Xray уже выполняется."
        return 1
    fi

    broray_xray_update_work_clean

    mkdir -p "$BRORAY_XRAY_UPDATE_WORK" ||
    {
        broray_xray_update_lock_release

        broray_xray_update_error \
            "Не удалось создать рабочий каталог в /tmp."
        return 1
    }

    trap '
        broray_xray_update_work_clean
        broray_xray_update_lock_release
    ' 0 1 2 15

    broray_xray_check_file="$BRORAY_XRAY_UPDATE_WORK/check.json"

    if ! broray_xray_update_check \
        > "$broray_xray_check_file"
    then
        cat "$broray_xray_check_file"
        return 1
    fi

    if ! jq -e '.success == true' \
        "$broray_xray_check_file" \
        >/dev/null 2>&1
    then
        cat "$broray_xray_check_file"
        return 1
    fi

    broray_xray_current_version="$(
        jq -r '.currentVersion // empty' \
            "$broray_xray_check_file"
    )"

    broray_xray_target_version="$(
        jq -r '.latestVersion // empty' \
            "$broray_xray_check_file"
    )"

    broray_xray_update_available="$(
        jq -r '.updateAvailable // false' \
            "$broray_xray_check_file"
    )"

    broray_xray_installed_newer="$(
        jq -r '.installedNewer // false' \
            "$broray_xray_check_file"
    )"

    broray_xray_asset_name="$(
        jq -r '.asset.name // empty' \
            "$broray_xray_check_file"
    )"

    broray_xray_asset_url="$(
        jq -r '.asset.url // empty' \
            "$broray_xray_check_file"
    )"

    broray_xray_asset_size="$(
        jq -r '.asset.size // 0' \
            "$broray_xray_check_file"
    )"

    broray_xray_digest_name="$(
        jq -r '.digest.name // empty' \
            "$broray_xray_check_file"
    )"

    broray_xray_digest_url="$(
        jq -r '.digest.url // empty' \
            "$broray_xray_check_file"
    )"

    [ -n "$broray_xray_current_version" ] &&
    [ -n "$broray_xray_target_version" ] &&
    [ -n "$broray_xray_asset_name" ] &&
    [ -n "$broray_xray_asset_url" ] &&
    [ -n "$broray_xray_digest_name" ] &&
    [ -n "$broray_xray_digest_url" ] ||
    {
        broray_xray_update_error \
            "Получены неполные данные официального релиза."
        return 1
    }

    if [ "$broray_xray_update_mode" = "update" ] &&
       [ "$broray_xray_update_available" != "true" ]
    then
        broray_xray_update_error \
            "Новая версия Xray отсутствует."
        return 1
    fi

    if [ "$broray_xray_update_mode" = "reinstall" ]; then
        if [ "$broray_xray_installed_newer" = "true" ]; then
            broray_xray_update_error \
                "Автоматическое понижение версии запрещено."
            return 1
        fi

        if [ "$broray_xray_current_version" != \
             "$broray_xray_target_version" ]
        then
            broray_xray_update_error \
                "Для доступной новой версии нужно использовать update."
            return 1
        fi
    fi

    broray_xray_asset_size="$(
        broray_xray_update_numeric \
            "$broray_xray_asset_size"
    )"

    broray_xray_current_size="$(
        wc -c < "$BRORAY_XRAY_BINARY" |
            tr -d ' '
    )"

    broray_xray_tmp_free="$(
        broray_xray_update_free_bytes /tmp
    )"

    broray_xray_tmp_required="$(
        expr \
            "$broray_xray_asset_size" \
            + "$broray_xray_current_size" \* 2 \
            + 16777216
    )"

    if [ "$broray_xray_tmp_free" -lt \
         "$broray_xray_tmp_required" ]
    then
        broray_xray_update_error \
            "Недостаточно свободной памяти в /tmp для безопасного обновления."
        return 1
    fi

    broray_xray_archive="$BRORAY_XRAY_UPDATE_WORK/$broray_xray_asset_name"
    broray_xray_digest="$BRORAY_XRAY_UPDATE_WORK/$broray_xray_digest_name"
    broray_xray_candidate="$BRORAY_XRAY_UPDATE_WORK/xray.new"
    broray_xray_old_backup="$BRORAY_XRAY_UPDATE_WORK/xray.old"

    if ! curl \
        -fL \
        --connect-timeout 15 \
        --max-time 180 \
        -o "$broray_xray_archive" \
        "$broray_xray_asset_url"
    then
        broray_xray_update_error \
            "Не удалось скачать официальный архив Xray."
        return 1
    fi

    broray_xray_downloaded_size="$(
        wc -c < "$broray_xray_archive" |
            tr -d ' '
    )"

    if [ "$broray_xray_asset_size" -gt 0 ] &&
       [ "$broray_xray_downloaded_size" -ne \
         "$broray_xray_asset_size" ]
    then
        broray_xray_update_error \
            "Размер скачанного архива не совпадает с данными релиза."
        return 1
    fi

    if ! curl \
        -fL \
        --connect-timeout 15 \
        --max-time 60 \
        -o "$broray_xray_digest" \
        "$broray_xray_digest_url"
    then
        broray_xray_update_error \
            "Не удалось скачать официальный файл контрольных сумм."
        return 1
    fi

    broray_xray_expected_sha256="$(
        broray_xray_update_expected_sha256 \
            "$broray_xray_digest"
    )"

    if ! broray_xray_update_validate_sha256 \
        "$broray_xray_expected_sha256"
    then
        broray_xray_update_error \
            "Не удалось прочитать SHA2-256 официального архива."
        return 1
    fi

    broray_xray_actual_sha256="$(
        sha256sum "$broray_xray_archive" |
            awk '{print $1}'
    )"

    if [ "$broray_xray_actual_sha256" != \
         "$broray_xray_expected_sha256" ]
    then
        broray_xray_update_error \
            "SHA256 архива не совпадает с официальной контрольной суммой."
        return 1
    fi

    if ! unzip -p \
        "$broray_xray_archive" \
        xray \
        > "$broray_xray_candidate"
    then
        rm -f "$broray_xray_candidate"

        broray_xray_update_error \
            "Не удалось извлечь бинарный файл xray."
        return 1
    fi

    chmod 755 "$broray_xray_candidate" ||
    {
        broray_xray_update_error \
            "Не удалось установить права нового бинарника."
        return 1
    }

    broray_xray_candidate_size="$(
        wc -c < "$broray_xray_candidate" |
            tr -d ' '
    )"

    [ "$broray_xray_candidate_size" -gt 10000000 ] ||
    {
        broray_xray_update_error \
            "Извлечённый бинарный файл имеет недопустимый размер."
        return 1
    }

    broray_xray_candidate_version="$(
        broray_xray_version_number \
            "$broray_xray_candidate"
    )"

    if [ "$broray_xray_candidate_version" != \
         "$broray_xray_target_version" ]
    then
        broray_xray_update_error \
            "Версия нового бинарника не соответствует релизу."
        return 1
    fi

    if ! XRAY_LOCATION_ASSET="$BRORAY_XRAY_ASSET_DIR" \
        "$broray_xray_candidate" \
        run \
        -test \
        -c "$BRORAY_XRAY_CONFIG" \
        > "$BRORAY_XRAY_UPDATE_WORK/preinstall-test.log" \
        2>&1
    then
        broray_xray_test_details="$(
            tail -n 20 \
                "$BRORAY_XRAY_UPDATE_WORK/preinstall-test.log"
        )"

        jq -n \
            --arg error \
                "Новая версия Xray несовместима с действующей конфигурацией BROray." \
            --arg details "$broray_xray_test_details" '
            {
                success: false,
                error: $error,
                details: $details
            }
        '

        return 1
    fi

    if ! cp "$BRORAY_XRAY_BINARY" \
        "$broray_xray_old_backup"
    then
        broray_xray_update_error \
            "Не удалось сохранить текущий Xray в /tmp."
        return 1
    fi

    chmod 755 "$broray_xray_old_backup"

    broray_xray_old_sha256="$(
        sha256sum "$BRORAY_XRAY_BINARY" |
            awk '{print $1}'
    )"

    broray_xray_backup_sha256="$(
        sha256sum "$broray_xray_old_backup" |
            awk '{print $1}'
    )"

    if [ "$broray_xray_old_sha256" != \
         "$broray_xray_backup_sha256" ]
    then
        broray_xray_update_error \
            "Резервная копия текущего Xray повреждена."
        return 1
    fi

    broray_xray_was_running=false

    if broray_xray_is_running; then
        broray_xray_was_running=true
    fi

    if [ "$broray_xray_was_running" = "true" ]; then
        if ! broray_xray_stop \
            > "$BRORAY_XRAY_UPDATE_WORK/stop.log" \
            2>&1
        then
            broray_xray_update_error \
                "Не удалось безопасно остановить Xray."
            return 1
        fi

        if broray_xray_is_running; then
            broray_xray_update_error \
                "Xray продолжает работать после команды остановки."
            return 1
        fi
    fi

    broray_xray_opt_free="$(
        broray_xray_update_free_bytes /opt
    )"

    broray_xray_opt_effective="$(
        expr \
            "$broray_xray_opt_free" \
            + "$broray_xray_current_size"
    )"

    broray_xray_opt_required="$(
        expr \
            "$broray_xray_candidate_size" \
            + 8388608
    )"

    if [ "$broray_xray_opt_effective" -lt \
         "$broray_xray_opt_required" ]
    then
        if [ "$broray_xray_was_running" = "true" ]; then
            broray_xray_start >/dev/null 2>&1
        fi

        broray_xray_update_error \
            "На /opt недостаточно места даже после удаления старого бинарника."
        return 1
    fi

    rm -f "$BRORAY_XRAY_BINARY.new"

    if ! rm -f "$BRORAY_XRAY_BINARY"; then
        if [ "$broray_xray_was_running" = "true" ]; then
            broray_xray_start >/dev/null 2>&1
        fi

        broray_xray_update_error \
            "Не удалось удалить старый бинарник после создания проверенной резервной копии."
        return 1
    fi

    if ! cp "$broray_xray_candidate" \
        "$BRORAY_XRAY_BINARY.new"
    then
        broray_xray_update_restore_binary \
            "$broray_xray_old_backup" \
            "$broray_xray_was_running"

        broray_xray_update_error \
            "Не удалось записать новый Xray. Старый бинарник восстановлен."
        return 1
    fi

    chmod 755 "$BRORAY_XRAY_BINARY.new" ||
    {
        broray_xray_update_restore_binary \
            "$broray_xray_old_backup" \
            "$broray_xray_was_running"

        broray_xray_update_error \
            "Не удалось установить права. Старый Xray восстановлен."
        return 1
    }

    broray_xray_written_sha256="$(
        sha256sum "$BRORAY_XRAY_BINARY.new" |
            awk '{print $1}'
    )"

    broray_xray_candidate_sha256="$(
        sha256sum "$broray_xray_candidate" |
            awk '{print $1}'
    )"

    if [ "$broray_xray_written_sha256" != \
         "$broray_xray_candidate_sha256" ]
    then
        broray_xray_update_restore_binary \
            "$broray_xray_old_backup" \
            "$broray_xray_was_running"

        broray_xray_update_error \
            "Новый бинарник повреждён при записи. Выполнен откат."
        return 1
    fi

    if ! mv "$BRORAY_XRAY_BINARY.new" \
        "$BRORAY_XRAY_BINARY"
    then
        broray_xray_update_restore_binary \
            "$broray_xray_old_backup" \
            "$broray_xray_was_running"

        broray_xray_update_error \
            "Не удалось завершить замену. Выполнен откат."
        return 1
    fi

    broray_xray_install_ok=true
    broray_xray_failure_reason=""

    broray_xray_installed_version="$(
        broray_xray_version_number \
            "$BRORAY_XRAY_BINARY"
    )"

    if [ "$broray_xray_installed_version" != \
         "$broray_xray_target_version" ]
    then
        broray_xray_install_ok=false
        broray_xray_failure_reason="После замены определена неправильная версия."
    fi

    if [ "$broray_xray_install_ok" = "true" ] &&
       ! XRAY_LOCATION_ASSET="$BRORAY_XRAY_ASSET_DIR" \
            "$BRORAY_XRAY_BINARY" \
            run \
            -test \
            -c "$BRORAY_XRAY_CONFIG" \
            > "$BRORAY_XRAY_UPDATE_WORK/final-test.log" \
            2>&1
    then
        broray_xray_install_ok=false
        broray_xray_failure_reason="Финальная проверка конфигурации завершилась ошибкой."
    fi

    if [ "$broray_xray_install_ok" = "true" ] &&
       [ "$broray_xray_was_running" = "true" ]
    then
        if ! broray_xray_start \
            > "$BRORAY_XRAY_UPDATE_WORK/start.log" \
            2>&1
        then
            broray_xray_install_ok=false
            broray_xray_failure_reason="Новая версия Xray не запустилась."
        elif ! broray_xray_wait_running; then
            broray_xray_install_ok=false
            broray_xray_failure_reason="Не подтверждён запуск новой версии Xray."
        fi
    fi

    if [ "$broray_xray_install_ok" = "true" ] &&
       [ "$broray_xray_was_running" = "true" ] &&
       ! broray_xray_is_running
    then
        broray_xray_install_ok=false
        broray_xray_failure_reason="Xray завершился сразу после запуска."
    fi

    if [ "$broray_xray_install_ok" != "true" ]; then
        broray_xray_rollback_ok=false

        if broray_xray_update_restore_binary \
            "$broray_xray_old_backup" \
            "$broray_xray_was_running"
        then
            broray_xray_rollback_ok=true
        fi

        jq -n \
            --arg error "$broray_xray_failure_reason" \
            --argjson rollbackSuccess \
                "$broray_xray_rollback_ok" '
            {
                success: false,
                rolledBack: true,
                rollbackSuccess: $rollbackSuccess,
                error:
                    (
                        $error +
                        " Выполнен автоматический откат."
                    )
            }
        '

        return 1
    fi

    broray_xray_running=false

    if broray_xray_is_running; then
        broray_xray_running=true
    fi

    jq -n \
        --arg mode "$broray_xray_update_mode" \
        --arg previousVersion \
            "$broray_xray_current_version" \
        --arg version \
            "$broray_xray_installed_version" \
        --arg sha256 \
            "$broray_xray_candidate_sha256" \
        --argjson running "$broray_xray_running" '
        {
            success: true,
            installed: true,
            mode: $mode,
            previousVersion: $previousVersion,
            version: $version,
            sha256: $sha256,
            running: $running,
            message:
                (
                    if $mode == "reinstall"
                    then
                        "Текущая версия Xray безопасно переустановлена."
                    else
                        "Новая версия Xray безопасно установлена."
                    end
                )
        }
    '

    broray_xray_update_success_cleanup
    trap - 0 1 2 15
}

broray_xray_update_command()
{
    broray_xray_update_install update
}

broray_xray_reinstall_command()
{
    broray_xray_update_install reinstall
}

broray_xray_update_clean()
{
    broray_xray_update_lock_pid=""

    if [ -f "$BRORAY_XRAY_UPDATE_LOCK/pid" ]; then
        broray_xray_update_lock_pid="$(
            cat "$BRORAY_XRAY_UPDATE_LOCK/pid"
        )"
    fi

    case "$broray_xray_update_lock_pid" in
        ''|*[!0-9]*)
            rm -rf "$BRORAY_XRAY_UPDATE_LOCK"
            ;;
        *)
            if kill -0 \
                "$broray_xray_update_lock_pid" \
                2>/dev/null
            then
                broray_xray_update_error \
                    "Операция обновления Xray ещё выполняется."
                return 1
            fi

            rm -rf "$BRORAY_XRAY_UPDATE_LOCK"
            ;;
    esac

    broray_xray_update_work_clean

    jq -n '
        {
            success: true,
            message:
                "Временные файлы обновления Xray удалены."
        }
    '
}
