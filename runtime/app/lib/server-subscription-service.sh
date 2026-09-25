#!/opt/bin/ash

BRORAY_SERVER_SUB_BASE="${BRORAY_SERVER_SUB_BASE:-${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}}"

. "$BRORAY_SERVER_SUB_BASE/lib/server-service.sh"
. "$BRORAY_SERVER_SUB_BASE/lib/operation-job.sh"

BRORAY_SERVER_SUB_LIVE="$BRORAY_SERVER_SUB_BASE/servers"
BRORAY_SERVER_SUB_DISABLED="$BRORAY_SERVER_SUB_BASE/config/disabled-subscription-servers"
BRORAY_SERVER_SUB_QUALITY="$BRORAY_SERVER_SUB_BASE/run/server-quality"
BRORAY_SERVER_SUB_LOCK="$BRORAY_SERVER_SUB_BASE/run/server-subscription.lock"
BRORAY_SERVER_SUB_TMP="$BRORAY_SERVER_SUB_BASE/tmp"

broray_server_subscription_error()
{
    error_code="$1"
    shift
    printf 'BRORAY_ERROR:%s:%s\n' "$error_code" "$*" >&2
    return 1
}

broray_server_subscription_validate_id()
{
    subscription_id="$1"
    [ -n "$subscription_id" ] || {
        broray_server_subscription_error \
            "INVALID_SUBSCRIPTION_ID" \
            "Не указан идентификатор подписки."
        return 1
    }
    case "$subscription_id" in
        *[!a-zA-Z0-9._-]*)
            broray_server_subscription_error \
                "INVALID_SUBSCRIPTION_ID" \
                "Идентификатор подписки содержит недопустимые символы."
            return 1
            ;;
    esac
    return 0
}

broray_server_subscription_acquire_lock()
{
    broray_job_require_owner || return $?
    if [ -e "$BRORAY_SERVER_SUB_LOCK" ] || [ -L "$BRORAY_SERVER_SUB_LOCK" ]; then
        broray_server_subscription_error \
            "SERVER_SYNC_BUSY" \
            "Прежняя синхронизация серверов требует восстановления."
        return 1
    fi
    broray_job_checkpoint committing
}

broray_server_subscription_release_lock()
{
    return 0
}

broray_server_subscription_import_key()
{
    local import_file connection
    import_file="$1"
    connection="$(jq -cS '
        {
            protocol: (.protocol // ""),
            address: ((.address // "") | ascii_downcase),
            port: (.port // 0),
            network: (.network // .transport.type // ""),
            security: (.security // "none"),
            uuid: (.uuid // ""), encryption: (.encryption // ""),
            flow: (.flow // ""), alterId: (.alterId // 0),
            password: (.password // ""), method: (.method // ""), auth: (.auth // ""),
            serverName: (if .security == "reality" then .reality.serverName // "" else .tls.serverName // .reality.serverName // .sni // "" end),
            fingerprint: (if .security == "reality" then .reality.fingerprint // "chrome" else .tls.fingerprint // .reality.fingerprint // "chrome" end),
            alpn: (.tls.alpn // []), allowInsecure: (.tls.allowInsecure // false),
            publicKey: (.reality.publicKey // ""), shortId: (.reality.shortId // ""),
            spiderX: (.reality.spiderX // ""), pinnedPeerCertSha256: (.tls.pinnedPeerCertSha256 // ""),
            host: (.transport.host // .grpc.authority // .ws.host // .httpupgrade.host // ""),
            path: (.xhttp.path // .ws.path // .httpupgrade.path // .transport.path // ""),
            serviceName: (.grpc.serviceName // .transport.serviceName // ""),
            mode: (.xhttp.mode // .transport.mode // ""),
            headerType: (.transport.headerType // "none"), header: (.transport.header // {}),
            extra: (.xhttp.extra // .transport.extra // {}),
            ports: (.hysteria.ports // ""),
            obfs: (.hysteria.obfs // ""), obfsPassword: (.hysteria.obfsPassword // ""),
            finalMask: (.hysteria.finalMask // .transport.finalMask // {}),
            echConfigList: (.tls.echConfigList // ""),
            verifyPeerCertByName: (.tls.verifyPeerCertByName // ""),
            mldsa65Verify: (.reality.mldsa65Verify // ""),
            kcp: (.transport.kcp // {}), kcpSeed: (.transport.kcpSeed // ""),
            kcpLegacy: (.transport.kcpLegacy // false)
        }
    ' "$import_file" 2>/dev/null)" || return 1
    printf '%s\n' "$connection" | sha256sum | awk '{print $1}'
}

broray_server_subscription_continuity_key()
{
    local connection
    connection="$(jq -cS '
        {
            protocol:(.protocol // ""), address:((.address // "") | ascii_downcase),
            port:(.port // 0), network:(.network // .transport.type // ""), security:(.security // "none"),
            serverName:(if .security == "reality" then .reality.serverName // "" else .tls.serverName // .reality.serverName // .sni // "" end),
            host:(.transport.host // .grpc.authority // .ws.host // .httpupgrade.host // ""),
            path:(.xhttp.path // .ws.path // .httpupgrade.path // .transport.path // ""),
            serviceName:(.grpc.serviceName // .transport.serviceName // ""),
            mode:(.xhttp.mode // .transport.mode // "")
        }
    ' "$1" 2>/dev/null)" || return 1
    printf '%s\n' "$connection" | sha256sum | awk '{print $1}'
}

# Match in private work only. The original stage and live catalog stay immutable
# until the complete one-to-one mapping and existing sync checks have succeeded.
broray_server_subscription_remap()
{
    local file key continuity id old_hash target match_key
    BRORAY_SYNC_MAP_ERROR=SERVER_SYNC_CONFLICT
    : > "$sync_maps/old.jsonl"
    : > "$sync_maps/new.jsonl"
    while IFS="$(printf '\t')" read -r key file old_hash; do
        [ -n "$key" ] || continue
        [ -f "$file" ] && [ ! -L "$file" ] || return 1
        (broray_server_validate "$file" >/dev/null 2>&1) || return 1
        id="$(jq -er '.id' "$file")" || return 1
        case "$id" in ''|.|..|*[!A-Za-z0-9._-]*) return 1 ;; esac
        [ "${file##*/}" = "$id.json" ] || return 1
        continuity="$(broray_server_subscription_continuity_key "$file")" || return 1
        jq -nc --arg key "$key" --arg continuity "$continuity" --arg id "$id" --arg file "$file" \
            '{key:$key,continuity:$continuity,id:$id,file:$file}' >> "$sync_maps/old.jsonl" || return 1
    done < "$sync_maps/old.tsv"
    for file in "$sync_stage_dir"/*.json; do
        [ -f "$file" ] && [ ! -L "$file" ] || return 1
        id="$(jq -er '.id' "$file")" || return 1
        case "$id" in ''|.|..|*[!A-Za-z0-9._-]*) return 1 ;; esac
        [ "${file##*/}" = "$id.json" ] || return 1
        jq -e '.source.importKey | type == "string" and length > 0' "$file" >/dev/null || return 1
        key="$(broray_server_subscription_import_key "$file")" || return 1
        continuity="$(broray_server_subscription_continuity_key "$file")" || return 1
        jq -nc --arg key "$key" --arg continuity "$continuity" --arg id "$id" --arg file "$file" \
            '{key:$key,continuity:$continuity,id:$id,file:$file}' >> "$sync_maps/new.jsonl" || return 1
    done
    if ! jq -n --slurpfile old "$sync_maps/old.jsonl" --slurpfile new "$sync_maps/new.jsonl" '
        if ($old | group_by(.key) | any(length > 1)) or
           ($new | group_by(.key) | any(length > 1)) then error("ambiguous exact identity") else . end |
        [$new[] as $n | $n + {old:([$old[] | select(.key == $n.key)][0])}] as $exact |
        [$old[] as $o | select(any($exact[]; .old.id == $o.id) | not) | $o] as $remainingOld |
        [$exact[] | select(.old == null)] as $remainingNew |
        (($remainingOld + $remainingNew) | map(.continuity) | unique) as $keys |
        if any($keys[]; . as $key |
            ([$remainingOld[] | select(.continuity == $key)] | length) > 1 or
            ([$remainingNew[] | select(.continuity == $key)] | length) > 1)
        then error("ambiguous continuity identity") else . end |
        [$exact[] as $n |
            ($n.old // ([$remainingOld[] | select(.continuity == $n.continuity)][0])) as $o |
            {key:$n.key,matchKey:($o.key // $n.key),id:($o.id // $n.id),file:$n.file}]
    ' > "$sync_maps/plan.json"; then
        BRORAY_SYNC_MAP_ERROR=SERVER_SYNC_AMBIGUOUS_IDENTITY
        return 1
    fi
    jq -r '.[] | [.key,.matchKey,.id,.file] | @tsv' "$sync_maps/plan.json" > "$sync_maps/plan.tsv" || return 1
    mkdir "$sync_work/remapped" || return 1
    : > "$sync_maps/remap.tsv"
    while IFS="$(printf '\t')" read -r key match_key id file; do
        target="$sync_work/remapped/$id.json"
        [ ! -e "$target" ] && [ ! -L "$target" ] || return 1
        jq --arg id "$id" --arg key "$key" '.id=$id | .source.importKey=$key' "$file" > "$target" || return 1
        chmod 600 "$target" || return 1
        printf '%s\t%s\n' "$match_key" "$target" >> "$sync_maps/remap.tsv" || return 1
    done < "$sync_maps/plan.tsv"
    sync_stage_dir="$sync_work/remapped"
}

broray_server_subscription_config_hash()
{
    config_hash_file="$1"
    jq -cS '
        del(
            .id,
            .name,
            .uri,
            .source
        )
    ' "$config_hash_file" 2>/dev/null |
        sha256sum |
        awk '{print $1}'
}

broray_server_subscription_file_matches()
{
    match_file="$1"
    match_subscription_id="$2"
    [ -f "$match_file" ] || return 1
    jq -e \
        --arg id "$match_subscription_id" '
        .source.type == "subscription" and
        .source.subscriptionId == $id
    ' "$match_file" >/dev/null 2>&1
}

broray_server_subscription_active_id()
{
    if [ -f "$BRORAY_ACTIVE_SERVER_FILE" ]; then
        sed -n '1p' "$BRORAY_ACTIVE_SERVER_FILE"
    fi
}

broray_server_subscription_active_belongs_to()
{
    belongs_subscription_id="$1"
    belongs_active_id="$(broray_server_subscription_active_id)"
    [ -n "$belongs_active_id" ] || return 1
    belongs_active_file="$BRORAY_SERVER_SUB_LIVE/$belongs_active_id.json"
    broray_server_subscription_file_matches \
        "$belongs_active_file" \
        "$belongs_subscription_id"
}

broray_server_subscription_count()
{
    count_subscription_id="$1"
    broray_server_subscription_validate_id "$count_subscription_id" || return 1
    count_value=0
    for count_file in "$BRORAY_SERVER_SUB_LIVE"/*.json; do
        [ -f "$count_file" ] || continue
        if broray_server_subscription_file_matches \
            "$count_file" "$count_subscription_id"; then
            count_value=$((count_value + 1))
        fi
    done
    count_disabled_dir="$BRORAY_SERVER_SUB_DISABLED/$count_subscription_id"
    for count_file in "$count_disabled_dir"/*.json; do
        [ -f "$count_file" ] || continue
        if broray_server_subscription_file_matches \
            "$count_file" "$count_subscription_id"; then
            count_value=$((count_value + 1))
        fi
    done
    printf '%s\n' "$count_value"
}

broray_server_subscription_count_all()
{
    count_all=0
    for count_all_file in "$BRORAY_SERVER_SUB_LIVE"/*.json; do
        [ -f "$count_all_file" ] || continue
        if jq -e '.source.type == "subscription"' \
            "$count_all_file" >/dev/null 2>&1; then
            count_all=$((count_all + 1))
        fi
    done
    for count_all_file in "$BRORAY_SERVER_SUB_DISABLED"/*/*.json; do
        [ -f "$count_all_file" ] || continue
        if jq -e '.source.type == "subscription"' \
            "$count_all_file" >/dev/null 2>&1; then
            count_all=$((count_all + 1))
        fi
    done
    printf '%s\n' "$count_all"
}

broray_server_subscription_list()
{
    list_subscription_id="$1"
    broray_server_subscription_validate_id "$list_subscription_id" || return 1
    mkdir -p "$BRORAY_SERVER_SUB_TMP"
    list_array="$BRORAY_SERVER_SUB_TMP/subscription-server-list.$$.json"
    printf '%s\n' '[]' > "$list_array"
    list_active_id="$(broray_server_subscription_active_id)"

    for list_location in enabled disabled; do
        if [ "$list_location" = "enabled" ]; then
            list_pattern="$BRORAY_SERVER_SUB_LIVE/*.json"
            list_enabled=true
        else
            list_pattern="$BRORAY_SERVER_SUB_DISABLED/$list_subscription_id/*.json"
            list_enabled=false
        fi
        for list_file in $list_pattern; do
            [ -f "$list_file" ] || continue
            broray_server_subscription_file_matches \
                "$list_file" "$list_subscription_id" || continue
            list_item="$BRORAY_SERVER_SUB_TMP/subscription-server-item.$$.json"
            broray_server_mask_json "$list_file" |
                jq \
                    --arg activeId "$list_active_id" \
                    --argjson enabled "$list_enabled" '
                    {
                        id: .id,
                        name: (.name // .id),
                        protocol: .protocol,
                        address: .address,
                        port: .port,
                        network: (.network // "unknown"),
                        security: (.security // "none"),
                        source: .source,
                        enabled: $enabled,
                        active: (.id == $activeId)
                    }
                ' > "$list_item" || {
                    rm -f "$list_item" "$list_array"
                    broray_server_subscription_error \
                        "SERVER_LIST_FAILED" \
                        "Не удалось сформировать список серверов подписки."
                    return 1
                }
            jq --slurpfile item "$list_item" \
                '. + [$item[0]]' "$list_array" \
                > "$list_array.new" || {
                    rm -f "$list_item" "$list_array" "$list_array.new"
                    broray_server_subscription_error \
                        "SERVER_LIST_FAILED" \
                        "Не удалось сформировать список серверов подписки."
                    return 1
                }
            mv "$list_array.new" "$list_array"
            rm -f "$list_item"
        done
    done

    list_total="$(jq 'length' "$list_array")"
    jq -n \
        --argjson items "$(cat "$list_array")" \
        --argjson total "$list_total" '
        {
            items: $items,
            total: $total
        }
    '
    rm -f "$list_array"
}

broray_server_subscription_set_enabled()
{
    enabled_subscription_id="$1"
    enabled_value="$2"
    broray_server_subscription_validate_id "$enabled_subscription_id" || return 1
    case "$enabled_value" in
        true|false) ;;
        *)
            broray_server_subscription_error \
                "INVALID_ENABLED_STATE" \
                "Состояние подписки должно быть true или false."
            return 1
            ;;
    esac

    broray_server_subscription_acquire_lock || return 1
    enabled_moved=0
    enabled_disabled_dir="$BRORAY_SERVER_SUB_DISABLED/$enabled_subscription_id"
    mkdir -p \
        "$BRORAY_SERVER_SUB_LIVE" \
        "$enabled_disabled_dir" \
        "$BRORAY_SERVER_SUB_QUALITY"

    if [ "$enabled_value" = "false" ]; then
        if broray_server_subscription_active_belongs_to \
            "$enabled_subscription_id"; then
            broray_server_subscription_release_lock
            broray_server_subscription_error \
                "ACTIVE_SERVER_CONFLICT" \
                "Нельзя выключить подписку: её сервер сейчас активен. Сначала отключите или замените активный сервер."
            return 1
        fi
        for enabled_file in "$BRORAY_SERVER_SUB_LIVE"/*.json; do
            [ -f "$enabled_file" ] || continue
            broray_server_subscription_file_matches \
                "$enabled_file" "$enabled_subscription_id" || continue
            enabled_name="$(basename "$enabled_file")"
            if ! mv "$enabled_file" "$enabled_disabled_dir/$enabled_name"; then
                broray_server_subscription_release_lock
                broray_server_subscription_error \
                    "SERVER_SOURCE_STATE_FAILED" \
                    "Не удалось выключить серверы подписки."
                return 1
            fi
            enabled_moved=$((enabled_moved + 1))
        done
    else
        for enabled_file in "$enabled_disabled_dir"/*.json; do
            [ -f "$enabled_file" ] || continue
            enabled_name="$(basename "$enabled_file")"
            if [ -e "$BRORAY_SERVER_SUB_LIVE/$enabled_name" ]; then
                broray_server_subscription_release_lock
                broray_server_subscription_error \
                    "SERVER_ID_CONFLICT" \
                    "Не удалось включить подписку: идентификатор сервера уже занят."
                return 1
            fi
            if ! mv "$enabled_file" "$BRORAY_SERVER_SUB_LIVE/$enabled_name"; then
                broray_server_subscription_release_lock
                broray_server_subscription_error \
                    "SERVER_SOURCE_STATE_FAILED" \
                    "Не удалось включить серверы подписки."
                return 1
            fi
            enabled_moved=$((enabled_moved + 1))
        done
        rmdir "$enabled_disabled_dir" 2>/dev/null || true
    fi

    broray_server_subscription_release_lock
    jq -n \
        --arg subscriptionId "$enabled_subscription_id" \
        --argjson enabled "$enabled_value" \
        --argjson affected "$enabled_moved" '
        {
            subscriptionId: $subscriptionId,
            enabled: $enabled,
            affected: $affected,
            activeServerImpact: "none"
        }
    '
}

broray_server_subscription_remove()
{
    remove_subscription_id="$1"
    broray_server_subscription_validate_id "$remove_subscription_id" || return 1
    broray_server_subscription_acquire_lock || return 1

    if broray_server_subscription_active_belongs_to "$remove_subscription_id"; then
        broray_server_subscription_release_lock
        broray_server_subscription_error \
            "ACTIVE_SERVER_CONFLICT" \
            "Нельзя удалить подписку: её сервер сейчас активен. Сначала отключите или замените активный сервер."
        return 1
    fi

    remove_count=0
    for remove_file in "$BRORAY_SERVER_SUB_LIVE"/*.json; do
        [ -f "$remove_file" ] || continue
        broray_server_subscription_file_matches \
            "$remove_file" "$remove_subscription_id" || continue
        remove_id="$(jq -r '.id // empty' "$remove_file")"
        rm -f "$remove_file" "$BRORAY_SERVER_SUB_QUALITY/$remove_id.json" || {
            broray_server_subscription_release_lock
            broray_server_subscription_error \
                "SERVER_SOURCE_REMOVE_FAILED" \
                "Не удалось удалить серверы подписки."
            return 1
        }
        remove_count=$((remove_count + 1))
    done

    remove_disabled_dir="$BRORAY_SERVER_SUB_DISABLED/$remove_subscription_id"
    for remove_file in "$remove_disabled_dir"/*.json; do
        [ -f "$remove_file" ] || continue
        remove_id="$(jq -r '.id // empty' "$remove_file")"
        rm -f "$remove_file" "$BRORAY_SERVER_SUB_QUALITY/$remove_id.json" || {
            broray_server_subscription_release_lock
            broray_server_subscription_error \
                "SERVER_SOURCE_REMOVE_FAILED" \
                "Не удалось удалить выключенные серверы подписки."
            return 1
        }
        remove_count=$((remove_count + 1))
    done
    rmdir "$remove_disabled_dir" 2>/dev/null || true

    broray_server_subscription_release_lock
    jq -n \
        --arg subscriptionId "$remove_subscription_id" \
        --argjson removed "$remove_count" '
        {
            subscriptionId: $subscriptionId,
            removed: $removed,
            activeServerImpact: "none"
        }
    '
}

broray_server_subscription_restore_backup()
{
    restore_subscription_id="$1"
    restore_backup_dir="$2"
    restore_active_value="$3"

    for restore_file in "$BRORAY_SERVER_SUB_LIVE"/*.json; do
        [ -f "$restore_file" ] || continue
        broray_server_subscription_file_matches \
            "$restore_file" "$restore_subscription_id" || continue
        rm -f "$restore_file"
    done
    rm -rf "$BRORAY_SERVER_SUB_DISABLED/$restore_subscription_id"
    mkdir -p \
        "$BRORAY_SERVER_SUB_LIVE" \
        "$BRORAY_SERVER_SUB_DISABLED/$restore_subscription_id"

    for restore_file in "$restore_backup_dir/live"/*.json; do
        [ -f "$restore_file" ] || continue
        cp "$restore_file" "$BRORAY_SERVER_SUB_LIVE/" || return 1
    done
    for restore_file in "$restore_backup_dir/disabled"/*.json; do
        [ -f "$restore_file" ] || continue
        cp "$restore_file" \
            "$BRORAY_SERVER_SUB_DISABLED/$restore_subscription_id/" || return 1
    done
    rmdir "$BRORAY_SERVER_SUB_DISABLED/$restore_subscription_id" \
        2>/dev/null || true

    if [ -n "$restore_active_value" ]; then
        printf '%s\n' "$restore_active_value" > "$BRORAY_ACTIVE_SERVER_FILE"
    else
        rm -f "$BRORAY_ACTIVE_SERVER_FILE"
    fi
    return 0
}

# Merge only in private sync work, before any live catalog write.
# A rejected row has no trustworthy identity: retain ALL unmatched old keys.
broray_server_subscription_merge_partial()
{
    local input merged file id key old_path old_hash target limit
    BRORAY_SYNC_RETAIN_ERROR=SERVER_SYNC_CONFLICT
    input="$sync_stage_dir"; merged="$sync_work/merged"
    limit="${BRORAY_SUB_MAX_NODES:-500}"
    case "$limit" in ''|*[!0-9]*) return 1 ;; esac
    [ "$limit" -ge 1 ] && [ "$limit" -le 500 ] || return 1
    mkdir "$merged" || return 1
    # Ambiguous old identities must not be guessed or collapsed during repair.
    awk -F '\t' 'seen[$1]++ {bad=1} END {exit bad}' "$sync_maps/old.tsv" || return 1
    for file in "$input"/*.json; do
        [ -f "$file" ] || continue
        [ ! -L "$file" ] || return 1
        id="$(jq -er '.id' "$file")" || return 1
        case "$id" in ''|.|..|*[!A-Za-z0-9._-]*) return 1 ;; esac
        [ "${file##*/}" = "$id.json" ] || return 1
        cp "$file" "$merged/$id.json" || return 1
    done
    while IFS="$(printf '\t')" read -r key old_path old_hash; do
        [ -n "$key" ] || continue
        if awk -F '\t' -v key="$key" '$1==key {found=1} END {exit !found}' "$sync_maps/new.tsv"; then continue; fi
        [ -f "$old_path" ] && [ ! -L "$old_path" ] || return 1
        ( broray_server_validate "$old_path" >/dev/null 2>&1 ) || return 1
        id="$(jq -er '.id' "$old_path")" || return 1
        case "$id" in ''|.|..|*[!A-Za-z0-9._-]*) return 1 ;; esac
        [ "${old_path##*/}" = "$id.json" ] || return 1
        target="$merged/$id.json"
        [ ! -e "$target" ] && [ ! -L "$target" ] || return 1
        case "$old_path" in
            "$BRORAY_SERVER_SUB_LIVE/"*) file="$sync_backup/live/$id.json" ;;
            "$sync_disabled_dir/"*) file="$sync_backup/disabled/$id.json" ;;
            *) return 1 ;;
        esac
        cp "$file" "$target" || return 1
        chmod 600 "$target" || return 1
        sync_retained=$((sync_retained + 1))
        if [ $((sync_stage_count + sync_retained)) -gt "$limit" ]; then
            BRORAY_SYNC_RETAIN_ERROR=PARTIAL_UPDATE_LIMIT
            return 1
        fi
        printf '%s\t%s\t%s\n' "$key" "$target" "$old_hash" >> "$sync_maps/new.tsv" || return 1
    done < "$sync_maps/old.tsv"
    # Do not overwrite a foreign source even if a filename happens to collide.
    for file in "$merged"/*.json; do
        [ -f "$file" ] || continue
        id="${file##*/}"
        for target in "$BRORAY_SERVER_SUB_LIVE/$id" "$sync_disabled_dir/$id"; do
            [ ! -L "$target" ] || return 1
            if [ -e "$target" ]; then
                broray_server_subscription_file_matches "$target" "$sync_subscription_id" || return 1
            fi
        done
    done
    sync_stage_dir="$merged"
    return 0
}

broray_server_subscription_sync()
{
    local sync_policy sync_retained sync_catalog_total BRORAY_SYNC_RETAIN_ERROR BRORAY_SYNC_MAP_ERROR
    sync_subscription_id="$1"
    sync_stage_dir="$2"
    sync_enabled="$3"
    sync_update_id="$4"
    sync_policy="${5:-replace}"
    case "$sync_policy" in replace|retain-unmatched) ;; *)
        broray_server_subscription_error SERVER_SYNC_CONFLICT "Неизвестная политика обновления серверов."
        return 1 ;;
    esac

    broray_server_subscription_validate_id "$sync_subscription_id" || return 1
    [ -d "$sync_stage_dir" ] || {
        broray_server_subscription_error \
            "SERVER_SYNC_ERROR" \
            "Каталог подготовленных серверов не найден."
        return 1
    }
    case "$sync_enabled" in
        true|false) ;;
        *)
            broray_server_subscription_error \
                "SERVER_SYNC_ERROR" \
                "Передано неправильное состояние подписки."
            return 1
            ;;
    esac

    sync_stage_count=0
    for sync_stage_file in "$sync_stage_dir"/*.json; do
        [ -f "$sync_stage_file" ] || continue
        if ! (
            broray_server_validate "$sync_stage_file" >/dev/null 2>&1
        ); then
            broray_server_subscription_error \
                "SERVER_SYNC_ERROR" \
                "Подготовленный сервер не прошёл проверку серверного модуля."
            return 1
        fi
        if ! broray_server_subscription_file_matches \
            "$sync_stage_file" "$sync_subscription_id"; then
            broray_server_subscription_error \
                "SERVER_SYNC_ERROR" \
                "Подготовленный сервер принадлежит другой подписке."
            return 1
        fi
        sync_stage_count=$((sync_stage_count + 1))
    done
    [ "$sync_stage_count" -gt 0 ] || {
        broray_server_subscription_error \
            "NO_VALID_NODES" \
            "Серверный модуль не получил ни одного узла."
        return 1
    }

    broray_server_subscription_acquire_lock || return 1
    sync_work="$BRORAY_SERVER_SUB_TMP/server-subscription-sync.$$.${sync_update_id}"
    sync_backup="$sync_work/backup"
    sync_maps="$sync_work/maps"
    mkdir -p \
        "$sync_backup/live" \
        "$sync_backup/disabled" \
        "$sync_maps" \
        "$BRORAY_SERVER_SUB_LIVE" \
        "$BRORAY_SERVER_SUB_DISABLED/$sync_subscription_id" \
        "$BRORAY_SERVER_SUB_QUALITY"

    sync_active_before="$(broray_server_subscription_active_id)"
    sync_active_old_file=""
    sync_active_new_file=""
    sync_active_new_id=""
    sync_active_config_changed=false
    sync_active_name_changed=false
    sync_active_impact="none"

    : > "$sync_maps/old.tsv"
    for sync_old_file in "$BRORAY_SERVER_SUB_LIVE"/*.json; do
        [ -f "$sync_old_file" ] || continue
        broray_server_subscription_file_matches \
            "$sync_old_file" "$sync_subscription_id" || continue
        cp "$sync_old_file" "$sync_backup/live/" || {
            rm -rf "$sync_work"
            broray_server_subscription_release_lock
            broray_server_subscription_error \
                "SERVER_SYNC_ERROR" \
                "Не удалось создать резервную копию серверов."
            return 1
        }
        sync_old_key="$(broray_server_subscription_import_key "$sync_old_file")"
        sync_old_hash="$(jq -cS 'del(.source.nodeIndex,.source.updatedAt)' "$sync_old_file" | sha256sum | awk '{print $1}')"
        printf '%s\t%s\t%s\n' \
            "$sync_old_key" "$sync_old_file" "$sync_old_hash" \
            >> "$sync_maps/old.tsv"
        sync_old_id="$(jq -r '.id // empty' "$sync_old_file")"
        if [ -n "$sync_active_before" ] && \
           [ "$sync_old_id" = "$sync_active_before" ]; then
            sync_active_old_file="$sync_old_file"
        fi
    done

    sync_disabled_dir="$BRORAY_SERVER_SUB_DISABLED/$sync_subscription_id"
    for sync_old_file in "$sync_disabled_dir"/*.json; do
        [ -f "$sync_old_file" ] || continue
        broray_server_subscription_file_matches \
            "$sync_old_file" "$sync_subscription_id" || continue
        cp "$sync_old_file" "$sync_backup/disabled/" || {
            rm -rf "$sync_work"
            broray_server_subscription_release_lock
            broray_server_subscription_error \
                "SERVER_SYNC_ERROR" \
                "Не удалось создать резервную копию выключенных серверов."
            return 1
        }
        sync_old_key="$(broray_server_subscription_import_key "$sync_old_file")"
        sync_old_hash="$(jq -cS 'del(.source.nodeIndex,.source.updatedAt)' "$sync_old_file" | sha256sum | awk '{print $1}')"
        printf '%s\t%s\t%s\n' \
            "$sync_old_key" "$sync_old_file" "$sync_old_hash" \
            >> "$sync_maps/old.tsv"
    done

    if ! broray_server_subscription_remap; then
        rm -rf "$sync_work"
        broray_server_subscription_release_lock
        broray_server_subscription_error "$BRORAY_SYNC_MAP_ERROR" "Не удалось однозначно сопоставить серверы подписки. Прежний каталог сохранён."
        return 1
    fi

    : > "$sync_maps/new.tsv"
    sync_added=0
    sync_updated=0
    sync_unchanged=0
    for sync_new_file in "$sync_stage_dir"/*.json; do
        [ -f "$sync_new_file" ] || continue
        sync_new_key="$(awk -F '\t' -v file="$sync_new_file" '$2 == file {print $1; exit}' "$sync_maps/remap.tsv")"
        [ -n "$sync_new_key" ] || {
            rm -rf "$sync_work"
            broray_server_subscription_release_lock
            broray_server_subscription_error \
                "SERVER_SYNC_ERROR" \
                "Подготовленный сервер не содержит importKey."
            return 1
        }
        sync_new_hash="$(jq -cS 'del(.source.nodeIndex,.source.updatedAt)' "$sync_new_file" | sha256sum | awk '{print $1}')"
        printf '%s\t%s\t%s\n' \
            "$sync_new_key" "$sync_new_file" "$sync_new_hash" \
            >> "$sync_maps/new.tsv"
        sync_old_row="$(awk -F '\t' -v key="$sync_new_key" '$1 == key {print; exit}' "$sync_maps/old.tsv")"
        if [ -z "$sync_old_row" ]; then
            sync_added=$((sync_added + 1))
        else
            sync_old_hash_match="$(printf '%s\n' "$sync_old_row" | awk -F '\t' '{print $3}')"
            if [ "$sync_old_hash_match" = "$sync_new_hash" ]; then
                sync_unchanged=$((sync_unchanged + 1))
            else
                sync_updated=$((sync_updated + 1))
            fi
        fi
    done

    sync_retained=0
    if [ "$sync_policy" = retain-unmatched ] && ! broray_server_subscription_merge_partial; then
        rm -rf "$sync_work"
        broray_server_subscription_release_lock
        broray_server_subscription_error "$BRORAY_SYNC_RETAIN_ERROR" "Не удалось безопасно объединить частичный ответ с прежними серверами. Прежний каталог сохранён."
        return 1
    fi
    sync_removed=0
    while IFS="$(printf '\t')" read -r sync_old_key sync_old_path sync_old_hash; do
        [ -n "$sync_old_key" ] || continue
        if ! awk -F '\t' -v key="$sync_old_key" \
            '$1 == key {found=1} END {exit !found}' \
            "$sync_maps/new.tsv"; then
            sync_removed=$((sync_removed + 1))
        fi
    done < "$sync_maps/old.tsv"

    if [ -n "$sync_active_old_file" ]; then
        sync_active_key="$(broray_server_subscription_import_key "$sync_active_old_file")"
        sync_active_row="$(awk -F '\t' -v key="$sync_active_key" '$1 == key {print; exit}' "$sync_maps/new.tsv")"
        if [ -z "$sync_active_row" ]; then
            rm -rf "$sync_work"
            broray_server_subscription_release_lock
            broray_server_subscription_error \
                "ACTIVE_SERVER_CONFLICT" \
                "Активный сервер исчез из подписки. Обновление отменено, сохранена прежняя рабочая конфигурация."
            return 1
        fi
        sync_active_new_file="$(printf '%s\n' "$sync_active_row" | awk -F '\t' '{print $2}')"
        sync_active_new_id="$(jq -r '.id' "$sync_active_new_file")"
        sync_active_old_name="$(jq -r '.name // .id' "$sync_active_old_file")"
        sync_active_new_name="$(jq -r '.name // .id' "$sync_active_new_file")"
        if [ "$sync_active_old_name" != "$sync_active_new_name" ]; then
            sync_active_name_changed=true
            sync_active_impact="display-name-changed"
        fi
        sync_active_old_config_hash="$(broray_server_subscription_config_hash "$sync_active_old_file")"
        sync_active_new_config_hash="$(broray_server_subscription_config_hash "$sync_active_new_file")"
        if [ "$sync_active_old_config_hash" != "$sync_active_new_config_hash" ]; then
            sync_active_config_changed=true
            sync_active_impact="configuration-changed"
            if ! (
                BRORAY_SERVERS="$sync_stage_dir"
                export BRORAY_SERVERS
                sync_test_config="$(broray_generate_server_config "$sync_active_new_id")" || exit 1
                broray_xray_test_file "$sync_test_config" >/dev/null 2>&1
                sync_test_result="$?"
                rm -f "$sync_test_config"
                exit "$sync_test_result"
            ); then
                rm -rf "$sync_work"
                broray_server_subscription_release_lock
                broray_server_subscription_error \
                    "ACTIVE_SERVER_CONFLICT" \
                    "Новая конфигурация активного сервера не прошла проверку Xray. Обновление отменено."
                return 1
            fi
        elif [ "$sync_active_new_id" != "$sync_active_before" ]; then
            sync_active_impact="configuration-changed"
        fi
    fi

    if [ "$sync_enabled" = "true" ]; then
        sync_target_dir="$BRORAY_SERVER_SUB_LIVE"
    else
        sync_target_dir="$BRORAY_SERVER_SUB_DISABLED/$sync_subscription_id"
    fi
    mkdir -p "$sync_target_dir"

    sync_commit_failed=false
    for sync_new_file in "$sync_stage_dir"/*.json; do
        [ -f "$sync_new_file" ] || continue
        sync_new_name="$(basename "$sync_new_file")"
        sync_temp_target="$sync_target_dir/.${sync_new_name}.new.$$"
        if ! cp "$sync_new_file" "$sync_temp_target" || \
           ! chmod 600 "$sync_temp_target" || \
           ! mv "$sync_temp_target" "$sync_target_dir/$sync_new_name"; then
            rm -f "$sync_temp_target"
            sync_commit_failed=true
            break
        fi
    done

    if [ "$sync_commit_failed" = "true" ]; then
        broray_server_subscription_restore_backup \
            "$sync_subscription_id" "$sync_backup" "$sync_active_before" || true
        rm -rf "$sync_work"
        broray_server_subscription_release_lock
        broray_server_subscription_error \
            "SERVER_SYNC_ERROR" \
            "Не удалось атомарно сохранить серверы подписки."
        return 1
    fi

    if [ -n "$sync_active_old_file" ]; then
        if [ "$sync_enabled" != "true" ]; then
            broray_server_subscription_restore_backup \
                "$sync_subscription_id" "$sync_backup" "$sync_active_before" || true
            rm -rf "$sync_work"
            broray_server_subscription_release_lock
            broray_server_subscription_error \
                "ACTIVE_SERVER_CONFLICT" \
                "Активный сервер не может быть перенесён в выключенный источник."
            return 1
        fi
        if [ "$sync_active_config_changed" = "true" ]; then
            if ! (
                broray_xray_apply_server "$sync_active_new_id"
            ) >/dev/null 2>&1; then
                broray_server_subscription_restore_backup \
                    "$sync_subscription_id" "$sync_backup" "$sync_active_before" || true
                rm -rf "$sync_work"
                broray_server_subscription_release_lock
                broray_server_subscription_error \
                    "ACTIVE_SERVER_CONFLICT" \
                    "Новая версия активного сервера не была применена. Восстановлена прежняя рабочая версия."
                return 1
            fi
        elif [ "$sync_active_new_id" != "$sync_active_before" ]; then
            if ! printf '%s\n' "$sync_active_new_id" \
                > "$BRORAY_ACTIVE_SERVER_FILE"; then
                broray_server_subscription_restore_backup \
                    "$sync_subscription_id" "$sync_backup" "$sync_active_before" || true
                rm -rf "$sync_work"
                broray_server_subscription_release_lock
                broray_server_subscription_error \
                    "SERVER_SYNC_ERROR" \
                    "Не удалось обновить идентификатор активного сервера."
                return 1
            fi
        fi
        if [ "$sync_active_name_changed" = "true" ] &&
           [ "$sync_active_config_changed" != "true" ]; then
            if ! broray_interface_sync_description >/dev/null 2>&1; then
                broray_server_subscription_restore_backup \
                    "$sync_subscription_id" "$sync_backup" "$sync_active_before" || true
                rm -rf "$sync_work"
                broray_server_subscription_release_lock
                broray_server_subscription_error \
                    "ACTIVE_SERVER_CONFLICT" \
                    "Новое имя активного сервера не удалось синхронизировать с управляемым интерфейсом. Восстановлена прежняя версия подписки."
                return 1
            fi
            broray_server_refresh_keenetic_status force >/dev/null 2>&1 || true
        fi
    fi

    : > "$sync_maps/keep.ids"
    for sync_new_file in "$sync_stage_dir"/*.json; do
        [ -f "$sync_new_file" ] || continue
        jq -r '.id' "$sync_new_file" >> "$sync_maps/keep.ids"
    done

    for sync_existing_file in "$BRORAY_SERVER_SUB_LIVE"/*.json; do
        [ -f "$sync_existing_file" ] || continue
        broray_server_subscription_file_matches \
            "$sync_existing_file" "$sync_subscription_id" || continue
        sync_existing_id="$(jq -r '.id // empty' "$sync_existing_file")"
        if [ "$sync_target_dir" != "$BRORAY_SERVER_SUB_LIVE" ] || \
           ! grep -Fxq "$sync_existing_id" "$sync_maps/keep.ids"; then
            rm -f \
                "$sync_existing_file" \
                "$BRORAY_SERVER_SUB_QUALITY/$sync_existing_id.json"
        fi
    done

    for sync_existing_file in "$BRORAY_SERVER_SUB_DISABLED/$sync_subscription_id"/*.json; do
        [ -f "$sync_existing_file" ] || continue
        broray_server_subscription_file_matches \
            "$sync_existing_file" "$sync_subscription_id" || continue
        sync_existing_id="$(jq -r '.id // empty' "$sync_existing_file")"
        if [ "$sync_target_dir" != "$BRORAY_SERVER_SUB_DISABLED/$sync_subscription_id" ] || \
           ! grep -Fxq "$sync_existing_id" "$sync_maps/keep.ids"; then
            rm -f \
                "$sync_existing_file" \
                "$BRORAY_SERVER_SUB_QUALITY/$sync_existing_id.json"
        fi
    done
    rmdir "$BRORAY_SERVER_SUB_DISABLED/$sync_subscription_id" \
        2>/dev/null || true

    sync_total="$sync_stage_count"
    sync_catalog_total="$(wc -l < "$sync_maps/new.tsv" | tr -d ' ')"
    rm -rf "$sync_work"
    broray_server_subscription_release_lock

    jq -n \
        --arg subscriptionId "$sync_subscription_id" \
        --arg updateId "$sync_update_id" \
        --argjson accepted "$sync_total" \
        --argjson retained "$sync_retained" --argjson total "$sync_catalog_total" --arg deletionPolicy "$sync_policy" \
        --argjson added "$sync_added" \
        --argjson updated "$sync_updated" \
        --argjson unchanged "$sync_unchanged" \
        --argjson removed "$sync_removed" \
        --arg activeServerImpact "$sync_active_impact" '
        {
            subscriptionId: $subscriptionId,
            updateId: $updateId,
            accepted: $accepted,
            rejected: 0,
            added: $added,
            updated: $updated,
            unchanged: $unchanged,
            removed: $removed,
            retained: $retained,
            total: $total,
            deletionPolicy: $deletionPolicy,
            warnings: (if $retained>0 then ["Сохранено прежних серверов: " + ($retained|tostring) + ". Ответ разобран не полностью; отсутствие этих серверов не подтверждено. Их параметры могут быть устаревшими. Удаление отсутствующих серверов возможно после обновления без отклонённых узлов."] else [] end),
            activeServerImpact: $activeServerImpact
        }
    '
}
