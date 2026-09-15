#!/opt/bin/ash

BRORAY_XRAY_RELEASES_API="${BRORAY_XRAY_RELEASES_API:-https://api.github.com/repos/XTLS/Xray-core/releases?per_page=5}"
BRORAY_XRAY_RELEASE_ASSET="${BRORAY_XRAY_RELEASE_ASSET:-Xray-linux-arm64-v8a.zip}"
BRORAY_XRAY_RELEASE_DIGEST="${BRORAY_XRAY_RELEASE_DIGEST:-Xray-linux-arm64-v8a.zip.dgst}"
BRORAY_XRAY_UPDATE_TMP_ROOT="${BRORAY_XRAY_UPDATE_TMP_ROOT:-/tmp}"
BRORAY_XRAY_RELEASE_CACHE="${BRORAY_XRAY_RELEASE_CACHE:-$BRORAY_XRAY_UPDATE_TMP_ROOT/broray-xray-releases.json}"
BRORAY_XRAY_DOWNLOAD_ROOT="${BRORAY_XRAY_DOWNLOAD_ROOT:-/opt/broray/tmp/broray-xray-download}"

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
    broray_xray_catalog_fetch
}

broray_xray_update_latest_release()
{
    jq -c \
        --arg assetName "$BRORAY_XRAY_RELEASE_ASSET" \
        --arg digestName "$BRORAY_XRAY_RELEASE_DIGEST" '
        [
            .[]
            | select(.draft == false and .prerelease == false)
            | select((
                [
                    .assets[]
                    | select(.name == $assetName)
                ]
                | length) > 0
            )
            | {
                tagName: .tag_name,
                prerelease: .prerelease,
                publishedAt: .published_at,
                archiveSha256: (.assets[0].digest // "" | ltrimstr("sha256:")),
                compatibility: .brorayCompatibility,
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

broray_xray_update_check_impl()
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
        df -k "$BRORAY_XRAY_UPDATE_TMP_ROOT" 2>/dev/null |
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

    broray_asset_size="$(printf '%s\n' "$broray_release_json" | jq -r '.asset.size // 0')"
    case "$broray_asset_size" in ''|*[!0-9]*) broray_asset_size=0 ;; esac
    broray_current_size="$(wc -c < "$BRORAY_XRAY_BINARY" 2>/dev/null | tr -d ' ')"
    case "$broray_current_size" in ''|*[!0-9]*) broray_current_size=0 ;; esac
    broray_tmp_required_bytes="$(expr "$broray_current_size" + 1048576)"
    broray_tmp_shortfall_bytes=0
    broray_storage_required_base="$broray_current_size"
    if [ "$broray_asset_size" -gt "$broray_storage_required_base" ]; then
        broray_storage_required_base="$broray_asset_size"
    fi
    broray_storage_required_bytes="$(expr "$broray_storage_required_base" + 1048576)"
    broray_storage_shortfall_bytes=0
    broray_storage_ok=true
    if [ "$broray_storage_free_bytes" -lt "$broray_storage_required_bytes" ]; then
        broray_storage_shortfall_bytes="$(expr "$broray_storage_required_bytes" - "$broray_storage_free_bytes")"
        broray_storage_ok=false
    fi
    broray_reinstall_allowed=true
    if [ "$broray_tmp_free_bytes" -lt "$broray_tmp_required_bytes" ]; then
        broray_tmp_shortfall_bytes="$(expr "$broray_tmp_required_bytes" - "$broray_tmp_free_bytes")"
        broray_reinstall_allowed=false
    fi
    [ "$broray_storage_ok" = true ] || broray_reinstall_allowed=false

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
            --argjson storageRequiredBytes "$broray_storage_required_bytes" \
            --argjson storageShortfallBytes "$broray_storage_shortfall_bytes" \
            --argjson storageOk "$broray_storage_ok" \
            --argjson temporaryFreeBytes \
                "$broray_tmp_free_bytes" \
            --argjson temporaryRequiredBytes "$broray_tmp_required_bytes" \
            --argjson temporaryShortfallBytes "$broray_tmp_shortfall_bytes" \
            --argjson reinstallAllowed "$broray_reinstall_allowed" '
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
                archiveSha256: .archiveSha256,
                compatibility: .compatibility,
                storage: {
                    freeBytes: $storageFreeBytes,
                    requiredBytes: $storageRequiredBytes,
                    shortfallBytes: $storageShortfallBytes,
                    ok: $storageOk
                },
                temporaryStorage: {
                    freeBytes: $temporaryFreeBytes,
                    requiredBytes: $temporaryRequiredBytes,
                    shortfallBytes: $temporaryShortfallBytes,
                    reinstallAllowed: $reinstallAllowed
                },
                message:
                    (
                        if ($storageOk|not)
                        then
                            "Недостаточно свободного места в /opt для безопасной установки Xray."
                        elif ($reinstallAllowed|not)
                        then
                            "Недостаточно временной памяти для безопасной установки Xray."
                        elif $updateAvailable
                        then
                            "Доступна новая версия Xray."
                        elif $installedNewer
                        then
                            "Установленная версия новее последнего стабильного релиза."
                        else
                            "Установлена актуальная версия Xray."
                        end
                    )
            }
        '
}


# BROray safe Xray installer v2

BRORAY_XRAY_UPDATE_STATE="${BRORAY_BASE:-/opt/broray}/update"
BRORAY_XRAY_UPDATE_LOCK="$BRORAY_XRAY_UPDATE_STATE/xray.lock"
BRORAY_XRAY_UPDATE_WORK="${BRORAY_XRAY_UPDATE_WORK:-$BRORAY_XRAY_UPDATE_TMP_ROOT/broray-xray-update}"
BRORAY_XRAY_REPLACEMENT_ACTIVE=false

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
    broray_job_require_owner || return $?
    # Old resources have no verifiable birth identity. Preserve them.
    [ ! -e "$BRORAY_XRAY_UPDATE_LOCK" ] && [ ! -L "$BRORAY_XRAY_UPDATE_LOCK" ]
}

broray_xray_update_lock_release() { :; }

broray_xray_update_work_clean()
{
    [ "${BRORAY_XRAY_PREP_DRAINED:-false}" = true ] || return 0
    case "${BRORAY_XRAY_UPDATE_WORK:-}" in "$BRORAY_BASE/tmp/xray-job-$BRORAY_BACKGROUND_OPERATION_ID-"*) ;; *) return 1 ;; esac
    [ -d "$BRORAY_XRAY_UPDATE_WORK" ] && [ ! -L "$BRORAY_XRAY_UPDATE_WORK" ] || return 1
    [ "$(cat "$BRORAY_XRAY_UPDATE_WORK/operation-id")" = "$BRORAY_BACKGROUND_OPERATION_ID" ] || return 1
    rm -rf "$BRORAY_XRAY_UPDATE_WORK" || return 1
    BRORAY_XRAY_PREP_DRAINED=false
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
    local restore_backup restore_running restore_sha
    restore_backup="$1"; restore_running="$2"
    broray_job_require_owner || return $?
    [ -f "$restore_backup" ] && [ ! -L "$restore_backup" ] || return 1
    restore_sha="$(sha256sum "$restore_backup" | awk '{print $1}')"
    [ "$restore_sha" = "$broray_xray_old_sha256" ] || return 1
    broray_xray_stop >/dev/null 2>&1 || return 1
    [ ! -L "$BRORAY_XRAY_BINARY" ] && [ ! -L "$BRORAY_XRAY_BINARY.new" ] || return 1
    cp "$restore_backup" "$BRORAY_XRAY_BINARY.new" && chmod 755 "$BRORAY_XRAY_BINARY.new" || return 1
    [ "$(sha256sum "$BRORAY_XRAY_BINARY.new" | awk '{print $1}')" = "$restore_sha" ] || return 1
    mv "$BRORAY_XRAY_BINARY.new" "$BRORAY_XRAY_BINARY" || return 1
    if [ "$restore_running" = true ]; then
        broray_xray_start >/dev/null 2>&1 && broray_xray_selected_runtime_ready || return 1
    else
        broray_xray_is_running && return 1
    fi
    rm -f "$restore_backup" || return 1
    BRORAY_XRAY_REPLACEMENT_ACTIVE=false
    BRORAY_XRAY_COMMIT_STARTED=false
    BRORAY_JOB_UNRESOLVED=false
}

broray_xray_update_abort_cleanup()
{
    if [ "${BRORAY_XRAY_COMMIT_STARTED:-false}" = true ]; then
        BRORAY_JOB_UNRESOLVED=true
        if [ -f "${broray_xray_old_backup:-}" ]; then
            broray_xray_update_restore_binary "$broray_xray_old_backup" "$broray_xray_was_running" || return 75
        else
            [ "$(sha256sum "$BRORAY_XRAY_BINARY" | awk '{print $1}')" = "$broray_xray_old_sha256" ] || return 75
            if [ "$broray_xray_was_running" = true ]; then
                broray_xray_start >/dev/null 2>&1 && broray_xray_selected_runtime_ready || return 75
            else
                broray_xray_is_running && return 75
            fi
            BRORAY_XRAY_COMMIT_STARTED=false
            BRORAY_JOB_UNRESOLVED=false
        fi
    fi
    broray_xray_update_work_clean
}

broray_xray_update_success_cleanup()
{
    broray_xray_update_work_clean
}

broray_xray_commit_failure()
{
    local failure_message rollback_ok
    failure_message="$1"; rollback_ok=false
    broray_xray_update_restore_binary "$broray_xray_old_backup" "$broray_xray_was_running" && rollback_ok=true
    jq -n --arg error "$failure_message" --argjson rollbackSuccess "$rollback_ok" \
      '{success:false,rolledBack:$rollbackSuccess,rollbackSuccess:$rollbackSuccess,
        error:($error + (if $rollbackSuccess then " Предыдущий Xray восстановлен."
          else " Восстановление не подтверждено. Резервный бинарник и блокировка сохранены." end))}'
    [ "$rollback_ok" = true ] && return 1
    return 75
}

broray_xray_update_commit()
{
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
        broray_xray_update_free_bytes "$BRORAY_BASE"
    )"

    broray_xray_opt_required="$(
        expr \
            "$broray_xray_candidate_size" \
            + 8388608
    )"

    if [ "$broray_xray_opt_free" -lt \
         "$broray_xray_opt_required" ]
    then
        if [ "$broray_xray_was_running" = "true" ]; then
            broray_xray_start >/dev/null 2>&1
        fi

        broray_xray_update_error \
            "На /opt недостаточно места для атомарной замены Xray."
        return 1
    fi

    rm -f "$BRORAY_XRAY_BINARY.new"

    if ! mv "$BRORAY_XRAY_BINARY" "$broray_xray_old_backup"; then
        if [ "$broray_xray_was_running" = "true" ]; then
            broray_xray_start >/dev/null 2>&1
        fi

        broray_xray_update_error \
            "Не удалось атомарно зафиксировать текущий Xray для отката."
        return 1
    fi
    BRORAY_XRAY_REPLACEMENT_ACTIVE=true

    broray_xray_backup_sha256="$(sha256sum "$broray_xray_old_backup" | awk '{print $1}')"
    if [ "$broray_xray_old_sha256" != "$broray_xray_backup_sha256" ]; then
        broray_xray_commit_failure "Резервная копия текущего Xray не прошла SHA256-проверку."
        return $?
    fi

    if ! cp "$broray_xray_candidate" \
        "$BRORAY_XRAY_BINARY.new"
    then
        broray_xray_commit_failure "Не удалось записать новый Xray."
        return $?
    fi

    chmod 755 "$BRORAY_XRAY_BINARY.new" ||
    {
        broray_xray_commit_failure "Не удалось установить права нового Xray."
        return $?
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
        broray_xray_commit_failure "Новый бинарник повреждён при записи."
        return $?
    fi

    if ! mv "$BRORAY_XRAY_BINARY.new" \
        "$BRORAY_XRAY_BINARY"
    then
        broray_xray_commit_failure "Не удалось завершить замену Xray."
        return $?
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
        elif ! broray_xray_selected_runtime_ready; then
            broray_xray_install_ok=false
            broray_xray_failure_reason="Не подтверждены запуск Xray и доступность локального SOCKS."
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
        broray_xray_commit_failure "$broray_xray_failure_reason"
        return $?
    fi

    broray_xray_running=false

    if broray_xray_is_running; then
        broray_xray_running=true
    fi

    rm -f "$broray_xray_old_backup" || {
        broray_xray_update_error \
            "Xray установлен, но не удалось завершить очистку резервного бинарника."
        return 1
    }
    BRORAY_XRAY_REPLACEMENT_ACTIVE=false
    BRORAY_XRAY_COMMIT_STARTED=false
    BRORAY_JOB_UNRESOLVED=false

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
}

. "$BRORAY_BASE/lib/xray-install-job.sh"

broray_xray_update_command()
{
    broray_xray_install_dispatch update
}

broray_xray_reinstall_command()
{
    broray_xray_install_dispatch reinstall
}

broray_xray_update_clean()
{
    broray_job_require_owner || return $?
    broray_xray_update_lock_acquire || return $?
    # Cleanup of previous jobs requires their recovery evidence. A CLI command
    # cannot infer ownership of an old shared download directory.
    jq -n '{success:true,message:"Активных временных файлов этого задания нет. Предыдущие операции доступны в диагностике."}'
}
