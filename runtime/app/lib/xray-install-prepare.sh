#!/opt/bin/ash
# Temporary download, extraction and validation only. Never start the runtime.
set -u
umask 077
[ "${BRORAY_OPS_SUPERVISED:-}" = ptrace/1 ] || exit 73
BRORAY_BASE="${BRORAY_ROOT:-/opt/broray}"
BRORAY_XRAY_UPDATE_WORK="$1"
case "$BRORAY_XRAY_UPDATE_WORK" in "$BRORAY_BASE/tmp/xray-job-$BRORAY_BACKGROUND_OPERATION_ID-"*) ;; *) exit 73 ;; esac
[ -d "$BRORAY_XRAY_UPDATE_WORK" ] && [ ! -L "$BRORAY_XRAY_UPDATE_WORK" ] || exit 73
[ "$(cat "$BRORAY_XRAY_UPDATE_WORK/operation-id")" = "$BRORAY_BACKGROUND_OPERATION_ID" ] || exit 73
BRORAY_XRAY_DOWNLOAD_ROOT="$BRORAY_XRAY_UPDATE_WORK/download"
BRORAY_XRAY_UPDATE_TMP_ROOT="$BRORAY_XRAY_UPDATE_WORK"
export BRORAY_XRAY_UPDATE_WORK BRORAY_XRAY_DOWNLOAD_ROOT BRORAY_XRAY_UPDATE_TMP_ROOT
. "$BRORAY_BASE/lib/xray-control.sh"
. "$BRORAY_BASE/lib/xray-update.sh"
. "$BRORAY_BASE/lib/xray-releases.sh"
broray_xray_update_mode="$2"
broray_xray_requested_file="$BRORAY_XRAY_UPDATE_WORK/request.json"
mkdir -m 700 "$BRORAY_XRAY_DOWNLOAD_ROOT" || exit 1
broray_xray_update_prepare_body()
{
    broray_xray_check_file="$BRORAY_XRAY_UPDATE_WORK/check.json"

    if ! broray_xray_install_check "$broray_xray_update_mode" "$broray_xray_requested_file" \
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
        broray_xray_update_free_bytes "$BRORAY_XRAY_UPDATE_TMP_ROOT"
    )"

    broray_xray_tmp_required="$(
        expr "$broray_xray_current_size" + 1048576
    )"

    if [ "$broray_xray_tmp_free" -lt \
         "$broray_xray_tmp_required" ]
    then
        broray_xray_update_error \
            "Недостаточно свободной памяти в приватном каталоге BROray: требуется $broray_xray_tmp_required байт, доступно $broray_xray_tmp_free байт."
        return 1
    fi

    broray_xray_opt_free="$(broray_xray_update_free_bytes "$BRORAY_BASE")"
    broray_xray_opt_required_base="$broray_xray_current_size"
    if [ "$broray_xray_asset_size" -gt "$broray_xray_opt_required_base" ]; then
        broray_xray_opt_required_base="$broray_xray_asset_size"
    fi
    broray_xray_opt_required="$(expr "$broray_xray_opt_required_base" + 1048576)"
    if [ "$broray_xray_opt_free" -lt "$broray_xray_opt_required" ]; then
        broray_xray_update_error \
            "На /opt недостаточно места: требуется $broray_xray_opt_required байт, доступно $broray_xray_opt_free байт."
        return 1
    fi

    broray_xray_archive="$BRORAY_XRAY_DOWNLOAD_ROOT/$broray_xray_asset_name"
    broray_xray_digest="$BRORAY_XRAY_DOWNLOAD_ROOT/$broray_xray_digest_name"
    broray_xray_candidate="$BRORAY_XRAY_UPDATE_WORK/xray.new"
    broray_xray_old_backup="${BRORAY_XRAY_BINARY}.broray-$BRORAY_BACKGROUND_OPERATION_ID-backup"
    if [ -e "$broray_xray_old_backup" ] || [ -L "$broray_xray_old_backup" ]; then
        broray_xray_update_error \
            "Обнаружена незавершённая замена Xray; требуется диагностика."
        return 1
    fi

    if ! curl \
        -q -fL --proto '=https' --proto-redir '=https' --tlsv1.2 \
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
        -q -fL --proto '=https' --proto-redir '=https' --tlsv1.2 \
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

    if [ "$broray_xray_actual_sha256" != "$(jq -r '.archiveSha256' "$broray_xray_check_file")" ]; then
        broray_xray_update_error "Архив не совпадает с подтверждённой пользователем версией."
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

    broray_xray_old_sha256="$(
        sha256sum "$BRORAY_XRAY_BINARY" |
            awk '{print $1}'
    )"
    broray_xray_update_validate_sha256 "$broray_xray_old_sha256" || {
        broray_xray_update_error "Не удалось зафиксировать SHA256 текущего Xray."
        return 1
    }

    # The signed archive has already been verified and extracted. Releasing it
    # before replacement keeps /opt consumption bounded by one new binary.
    rm -f "$broray_xray_archive" "$broray_xray_digest"

    jq -n --arg current "$broray_xray_current_version" --arg target "$broray_xray_target_version" \
      --arg oldSha "$broray_xray_old_sha256" --arg candidateSha "$(sha256sum "$broray_xray_candidate" | awk '{print $1}')" \
      --argjson size "$broray_xray_candidate_size" \
      '{currentVersion:$current,targetVersion:$target,oldSha256:$oldSha,candidateSha256:$candidateSha,candidateSize:$size}' >"$BRORAY_XRAY_UPDATE_WORK/prepared.json"
}
broray_xray_update_prepare_body
