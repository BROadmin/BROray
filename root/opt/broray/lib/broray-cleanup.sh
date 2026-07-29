#!/opt/bin/ash

# Safe, BusyBox-compatible cleanup planner for BROray.
# Only explicitly managed files below /opt/broray may be selected.

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_CLEANUP_RUN="${BRORAY_CLEANUP_RUN:-$BRORAY_BASE/run/cleanup}"
BRORAY_CLEANUP_PLAN_DIR="${BRORAY_CLEANUP_PLAN_DIR:-$BRORAY_CLEANUP_RUN/plans}"
BRORAY_CLEANUP_GLOBAL_LOCK="${BRORAY_CLEANUP_GLOBAL_LOCK:-$BRORAY_BASE/run/global-operation.lock}"
BRORAY_CLEANUP_TTL="${BRORAY_CLEANUP_TTL:-120}"
BRORAY_CLEANUP_LOCK_HELD=false
BRORAY_CLEANUP_ERROR_CODE=""
BRORAY_CLEANUP_ERROR_MESSAGE=""

broray_cleanup_error()
{
    local code message
    code="$1"
    message="$2"
    BRORAY_CLEANUP_ERROR_CODE="$code"
    BRORAY_CLEANUP_ERROR_MESSAGE="$message"
    return 1
}

broray_cleanup_now_epoch()
{
    date +%s
}

broray_cleanup_now_iso()
{
    date -u '+%Y-%m-%dT%H:%M:%SZ'
}

broray_cleanup_is_uint()
{
    case "${1:-}" in
        ''|*[!0-9]*) return 1 ;;
    esac
    return 0
}

broray_cleanup_require_runtime()
{
    command -v jq >/dev/null 2>&1 ||
        broray_cleanup_error CLEANUP_JQ_MISSING 'Для очистки требуется jq.' || return 1
    command -v sha256sum >/dev/null 2>&1 ||
        broray_cleanup_error CLEANUP_SHA256_MISSING 'Для очистки требуется sha256sum.' || return 1
    mkdir -p "$BRORAY_CLEANUP_PLAN_DIR" ||
        broray_cleanup_error CLEANUP_RUNTIME_FAILED 'Не удалось создать служебный каталог очистки.' || return 1
    chmod 700 "$BRORAY_CLEANUP_RUN" "$BRORAY_CLEANUP_PLAN_DIR" 2>/dev/null || true
    return 0
}

broray_cleanup_token_valid()
{
    local token
    case "${1:-}" in
        ????????????????????????????????) ;;
        *) return 1 ;;
    esac
    case "$1" in
        *[!0-9a-f]*) return 1 ;;
    esac
    return 0
}

broray_cleanup_token_create()
{
    printf '%s:%s:%s:%s\n' "$(broray_cleanup_now_epoch)" "$$" "${PPID:-0}" "$(cat /proc/uptime 2>/dev/null || true)" |
        sha256sum | awk '{print substr($1, 1, 32)}'
}

broray_cleanup_path_relative()
{
    local path
    case "$1" in
        "$BRORAY_BASE"/*) printf '%s\n' "${1#"$BRORAY_BASE"/}" ;;
        *) return 1 ;;
    esac
}

broray_cleanup_name_safe()
{
    local name
    case "$1" in
        ''|.|..|*'/'*|*'\t'*|*'\n'*|*'\r'*) return 1 ;;
    esac
    return 0
}

broray_cleanup_is_old()
{
    local path days
    path="$1"
    days="$2"
    [ -n "$(find "$path" -maxdepth 0 -mtime "+$days" -print 2>/dev/null)" ]
}

broray_cleanup_size_bytes()
{
    local path size
    path="$1"
    if [ -L "$path" ]; then
        return 1
    fi
    if [ -f "$path" ]; then
        size="$(stat -c '%s' "$path" 2>/dev/null || true)"
        if ! broray_cleanup_is_uint "$size"; then
            size="$(wc -c <"$path" 2>/dev/null | tr -d ' ')"
        fi
    elif [ -d "$path" ]; then
        size="$(du -sk "$path" 2>/dev/null | awk 'NR == 1 {print $1}')"
        broray_cleanup_is_uint "$size" || return 1
        size=$((size * 1024))
    else
        return 1
    fi
    broray_cleanup_is_uint "$size" || return 1
    printf '%s\n' "$size"
}

broray_cleanup_candidate_add()
{
    local output category path relative kind size
    output="$1"
    category="$2"
    path="$3"
    [ -e "$path" ] || return 0
    [ ! -L "$path" ] || return 0
    relative="$(broray_cleanup_path_relative "$path")" || return 0
    case "$relative" in
        *'..'*|*'\t'*|*'\n'*|*'\r'*) return 0 ;;
    esac
    if [ -f "$path" ]; then
        kind=file
    elif [ -d "$path" ]; then
        kind=directory
    else
        return 0
    fi
    size="$(broray_cleanup_size_bytes "$path")" || return 0
    printf '%s\t%s\t%s\t%s\n' "$category" "$kind" "$size" "$relative" >>"$output"
}

broray_cleanup_add_aged_glob()
{
    local output category days path
    output="$1"
    category="$2"
    days="$3"
    shift 3
    for path in "$@"; do
        [ -e "$path" ] || continue
        [ ! -L "$path" ] || continue
        broray_cleanup_is_old "$path" "$days" || continue
        broray_cleanup_candidate_add "$output" "$category" "$path"
    done
}

broray_cleanup_add_preserved_group()
{
    local output category keep days list path base index
    output="$1"
    category="$2"
    keep="$3"
    days="$4"
    shift 4
    list="/tmp/broray-cleanup-group-$$.$category"
    : >"$list" || return 1
    for path in "$@"; do
        [ -e "$path" ] || continue
        [ ! -L "$path" ] || continue
        base="${path##*/}"
        broray_cleanup_name_safe "$base" || continue
        printf '%s\n' "$path" >>"$list"
    done
    sort -r "$list" -o "$list" 2>/dev/null || {
        rm -f "$list"
        return 1
    }
    index=0
    while IFS= read -r path; do
        [ -n "$path" ] || continue
        index=$((index + 1))
        [ "$index" -gt "$keep" ] || continue
        broray_cleanup_is_old "$path" "$days" || continue
        broray_cleanup_candidate_add "$output" "$category" "$path"
    done <"$list"
    rm -f "$list"
    return 0
}

broray_cleanup_collect_temp()
{
    local output
    output="$1"
    broray_cleanup_add_aged_glob "$output" temp 0 \
        "$BRORAY_BASE"/tmp/broray-* \
        "$BRORAY_BASE"/tmp/routes-* \
        "$BRORAY_BASE"/tmp/.broray-* \
        "$BRORAY_BASE"/tmp/*.part \
        "$BRORAY_BASE"/tmp/*.download \
        "$BRORAY_BASE"/tmp/*.tmp.* \
        "$BRORAY_BASE"/routes/tmp/* \
        "$BRORAY_BASE"/routes/transactions/* \
        "$BRORAY_BASE"/run/broray/*.tmp.*
}

broray_cleanup_collect_backups()
{
    local output backup
    output="$1"
    backup="$BRORAY_BASE/backup"
    broray_cleanup_add_preserved_group "$output" backups 3 0 \
        "$backup"/system-before-*.tar.gz \
        "$backup"/system-before-reinstall-*.tar.gz
    broray_cleanup_add_preserved_group "$output" backups 3 0 \
        "$backup"/user-before-*.tar.gz \
        "$backup"/user-before-reinstall-*.tar.gz
    broray_cleanup_add_preserved_group "$output" backups 3 0 \
        "$backup"/web-new-before-*.tar.gz \
        "$backup"/web-before-*.tar.gz
    broray_cleanup_add_preserved_group "$output" backups 3 0 \
        "$backup"/xray-before-*.tar.gz \
        "$backup"/xray-binaries/*
    broray_cleanup_add_preserved_group "$output" backups 3 0 \
        "$backup"/lighttpd.conf.before-* \
        "$backup"/package-before-*.tar.gz \
        "$backup"/before-update-*.tar.gz \
        "$backup"/before-restore-*.tar.gz \
        "$backup"/before-uninstall-*.tar.gz
}

broray_cleanup_collect_route_backups()
{
    local output backup
    output="$1"
    backup="$BRORAY_BASE/routes/backup"
    broray_cleanup_add_preserved_group "$output" routeBackups 3 0 \
        "$backup"/catalog-* \
        "$backup"/user-removed-* \
        "$backup"/rollback-* \
        "$backup"/transaction-*
}

broray_cleanup_collect_logs()
{
    local output
    output="$1"
    broray_cleanup_add_aged_glob "$output" logs 6 \
        "$BRORAY_BASE"/logs/*.gz \
        "$BRORAY_BASE"/logs/*.old \
        "$BRORAY_BASE"/logs/*.log.[0-9]* \
        "$BRORAY_BASE"/logs/*.log-* \
        "$BRORAY_BASE"/run/broray/operation.log.* \
        "$BRORAY_BASE"/run/broray/*.old
}

broray_cleanup_build_list()
{
    local output include_temp include_backups include_route_backups include_logs raw
    output="$1"
    include_temp="$2"
    include_backups="$3"
    include_route_backups="$4"
    include_logs="$5"
    raw="$output.raw.$$"
    : >"$raw" || return 1
    [ "$include_temp" = true ] && broray_cleanup_collect_temp "$raw"
    [ "$include_backups" = true ] && broray_cleanup_collect_backups "$raw"
    [ "$include_route_backups" = true ] && broray_cleanup_collect_route_backups "$raw"
    [ "$include_logs" = true ] && broray_cleanup_collect_logs "$raw"
    sort -u "$raw" >"$output" || {
        rm -f "$raw"
        return 1
    }
    rm -f "$raw"
    return 0
}

broray_cleanup_list_digest()
{
    local list
    sha256sum "$1" | awk '{print $1}'
}

broray_cleanup_list_count()
{
    local list
    awk 'NF > 0 {count++} END {print count + 0}' "$1"
}

broray_cleanup_list_bytes()
{
    local list
    awk -F '\t' 'NF == 4 {sum += $3} END {printf "%.0f\n", sum + 0}' "$1"
}

broray_cleanup_list_json()
{
    local list ndjson tab category kind size relative rc
    list="$1"
    ndjson="$list.ndjson.$$"
    : >"$ndjson" || return 1
    tab="$(printf '\t')"
    while IFS="$tab" read -r category kind size relative; do
        [ -n "$relative" ] || continue
        jq -nc \
            --arg category "$category" \
            --arg kind "$kind" \
            --arg path "$relative" \
            --argjson sizeBytes "$size" \
            '{category:$category,kind:$kind,path:$path,sizeBytes:$sizeBytes}' >>"$ndjson" || {
                rm -f "$ndjson"
                return 1
            }
    done <"$list"
    jq -s '.' "$ndjson"
    rc=$?
    rm -f "$ndjson"
    return "$rc"
}

broray_cleanup_summary_json()
{
    local list
    list="$1"
    jq -Rn \
        '[inputs | split("\t") | select(length == 4) | {category:.[0], bytes:(.[2] | tonumber)}]
         | group_by(.category)
         | map({key:.[0].category,value:{count:length,estimatedBytes:(map(.bytes)|add)}})
         | from_entries' <"$list"
}

broray_cleanup_expired_plans_remove()
{
    local now plan expires token
    now="$(broray_cleanup_now_epoch)"
    for plan in "$BRORAY_CLEANUP_PLAN_DIR"/*.json; do
        [ -f "$plan" ] || continue
        expires="$(jq -r '.expiresEpoch // 0' "$plan" 2>/dev/null || printf '0')"
        broray_cleanup_is_uint "$expires" || expires=0
        [ "$expires" -ge "$now" ] || {
            token="${plan##*/}"
            token="${token%.json}"
            rm -f "$plan" "$BRORAY_CLEANUP_PLAN_DIR/$token.list"
        }
    done
}

broray_cleanup_global_active()
{
    local owner
    [ -d "$BRORAY_CLEANUP_GLOBAL_LOCK" ] || return 1
    owner="$(sed -n '1p' "$BRORAY_CLEANUP_GLOBAL_LOCK/pid" 2>/dev/null || true)"
    broray_cleanup_is_uint "$owner" || return 1
    kill -0 "$owner" 2>/dev/null
}

broray_cleanup_global_lock_acquire()
{
    local owner
    mkdir -p "$(dirname "$BRORAY_CLEANUP_GLOBAL_LOCK")" || return 1
    if mkdir "$BRORAY_CLEANUP_GLOBAL_LOCK" 2>/dev/null; then
        :
    else
        if broray_cleanup_global_active; then
            return 2
        fi
        rm -rf "$BRORAY_CLEANUP_GLOBAL_LOCK" 2>/dev/null || return 1
        mkdir "$BRORAY_CLEANUP_GLOBAL_LOCK" 2>/dev/null || return 1
    fi
    printf '%s\n' "$$" >"$BRORAY_CLEANUP_GLOBAL_LOCK/pid" || return 1
    printf '%s\n' system >"$BRORAY_CLEANUP_GLOBAL_LOCK/scope" || return 1
    printf '%s\n' cleanup >"$BRORAY_CLEANUP_GLOBAL_LOCK/action" || return 1
    : >"$BRORAY_CLEANUP_GLOBAL_LOCK/bundle" || return 1
    printf '%s\n' "$(broray_cleanup_now_iso)" >"$BRORAY_CLEANUP_GLOBAL_LOCK/startedAt" || return 1
    BRORAY_CLEANUP_LOCK_HELD=true
    return 0
}

broray_cleanup_global_lock_release()
{
    local owner action
    [ "$BRORAY_CLEANUP_LOCK_HELD" = true ] || return 0
    owner="$(sed -n '1p' "$BRORAY_CLEANUP_GLOBAL_LOCK/pid" 2>/dev/null || true)"
    action="$(sed -n '1p' "$BRORAY_CLEANUP_GLOBAL_LOCK/action" 2>/dev/null || true)"
    if [ "$owner" = "$$" ] && [ "$action" = cleanup ]; then
        rm -rf "$BRORAY_CLEANUP_GLOBAL_LOCK" 2>/dev/null || true
    fi
    BRORAY_CLEANUP_LOCK_HELD=false
}

broray_cleanup_plan_create()
{
    local include_temp include_backups include_route_backups include_logs value token list plan digest count bytes created expires summary candidates
    include_temp="$1"
    include_backups="$2"
    include_route_backups="$3"
    include_logs="$4"
    for value in "$include_temp" "$include_backups" "$include_route_backups" "$include_logs"; do
        case "$value" in true|false) ;; *)
            broray_cleanup_error CLEANUP_OPTIONS_INVALID 'Параметры очистки некорректны.'
            return 1
        esac
    done
    [ "$include_temp$include_backups$include_route_backups$include_logs" != falsefalsefalsefalse ] || {
        broray_cleanup_error CLEANUP_NOTHING_SELECTED 'Выберите хотя бы одну категорию очистки.'
        return 1
    }
    broray_cleanup_require_runtime || return 1
    broray_cleanup_expired_plans_remove
    if broray_cleanup_global_active; then
        broray_cleanup_error CLEANUP_OPERATION_BUSY 'Другая конфликтующая операция уже выполняется.'
        return 1
    fi
    token="$(broray_cleanup_token_create)"
    broray_cleanup_token_valid "$token" || {
        broray_cleanup_error CLEANUP_TOKEN_FAILED 'Не удалось создать подтверждение очистки.'
        return 1
    }
    list="$BRORAY_CLEANUP_PLAN_DIR/$token.list"
    plan="$BRORAY_CLEANUP_PLAN_DIR/$token.json"
    broray_cleanup_build_list "$list" "$include_temp" "$include_backups" "$include_route_backups" "$include_logs" || {
        rm -f "$list" "$plan"
        broray_cleanup_error CLEANUP_PLAN_FAILED 'Не удалось составить план очистки.'
        return 1
    }
    digest="$(broray_cleanup_list_digest "$list")"
    count="$(broray_cleanup_list_count "$list")"
    bytes="$(broray_cleanup_list_bytes "$list")"
    created="$(broray_cleanup_now_epoch)"
    expires=$((created + BRORAY_CLEANUP_TTL))
    summary="$(broray_cleanup_summary_json "$list")" || summary='{}'
    candidates="$(broray_cleanup_list_json "$list")" || candidates='[]'
    jq -nc \
        --arg token "$token" \
        --arg createdAt "$(broray_cleanup_now_iso)" \
        --arg digest "$digest" \
        --argjson createdEpoch "$created" \
        --argjson expiresEpoch "$expires" \
        --argjson ttlSeconds "$BRORAY_CLEANUP_TTL" \
        --argjson includeTemp "$include_temp" \
        --argjson includeBackups "$include_backups" \
        --argjson includeRouteBackups "$include_route_backups" \
        --argjson includeLogs "$include_logs" \
        --argjson candidateCount "$count" \
        --argjson estimatedBytes "$bytes" \
        --argjson categories "$summary" \
        --argjson candidates "$candidates" \
        '{schemaVersion:1,token:$token,createdAt:$createdAt,createdEpoch:$createdEpoch,
          expiresEpoch:$expiresEpoch,ttlSeconds:$ttlSeconds,digest:$digest,
          options:{temp:$includeTemp,backups:$includeBackups,routeBackups:$includeRouteBackups,logs:$includeLogs},
          candidateCount:$candidateCount,estimatedBytes:$estimatedBytes,categories:$categories,candidates:$candidates}' |
        tee "$plan" || {
            rm -f "$list" "$plan"
            broray_cleanup_error CLEANUP_PLAN_FAILED 'Не удалось сохранить план очистки.'
            return 1
        }
    chmod 600 "$list" "$plan" 2>/dev/null || true
    return 0
}

broray_cleanup_path_allowed()
{
    local category kind relative
    category="$1"
    kind="$2"
    relative="$3"
    case "$relative" in
        ''|/*|*'..'*|*'\t'*|*'\n'*|*'\r'*) return 1 ;;
    esac
    case "$category:$kind:$relative" in
        temp:file:tmp/broray-*|temp:file:tmp/routes-*|temp:file:tmp/.broray-*|temp:file:tmp/*.part|temp:file:tmp/*.download|temp:file:tmp/*.tmp.*|\
        temp:file:routes/tmp/*|temp:directory:routes/tmp/*|temp:file:routes/transactions/*|temp:directory:routes/transactions/*|temp:file:run/broray/*.tmp.*) ;;
        backups:file:backup/system-before-*.tar.gz|backups:file:backup/system-before-reinstall-*.tar.gz|\
        backups:file:backup/user-before-*.tar.gz|backups:file:backup/user-before-reinstall-*.tar.gz|\
        backups:file:backup/web-new-before-*.tar.gz|backups:file:backup/web-before-*.tar.gz|\
        backups:file:backup/xray-before-*.tar.gz|backups:file:backup/xray-binaries/*|\
        backups:file:backup/lighttpd.conf.before-*|backups:file:backup/package-before-*.tar.gz|\
        backups:file:backup/before-update-*.tar.gz|backups:file:backup/before-restore-*.tar.gz|backups:file:backup/before-uninstall-*.tar.gz) ;;
        routeBackups:directory:routes/backup/catalog-*|routeBackups:directory:routes/backup/user-removed-*|\
        routeBackups:directory:routes/backup/rollback-*|routeBackups:directory:routes/backup/transaction-*|\
        routeBackups:file:routes/backup/catalog-*|routeBackups:file:routes/backup/user-removed-*|\
        routeBackups:file:routes/backup/rollback-*|routeBackups:file:routes/backup/transaction-*) ;;
        logs:file:logs/*.gz|logs:file:logs/*.old|logs:file:logs/*.log.[0-9]*|logs:file:logs/*.log-*|\
        logs:file:run/broray/operation.log.*|logs:file:run/broray/*.old) ;;
        *) return 1 ;;
    esac
    return 0
}

broray_cleanup_execute()
{
    local token plan list now expires include_temp include_backups include_route_backups include_logs expected_digest current current_digest lock_rc deleted bytes failed tab category kind size relative path
    token="$1"
    broray_cleanup_require_runtime || return 1
    broray_cleanup_token_valid "$token" || {
        broray_cleanup_error CLEANUP_TOKEN_INVALID 'Подтверждение очистки некорректно.'
        return 1
    }
    plan="$BRORAY_CLEANUP_PLAN_DIR/$token.json"
    list="$BRORAY_CLEANUP_PLAN_DIR/$token.list"
    [ -s "$plan" ] && [ -f "$list" ] || {
        broray_cleanup_error CLEANUP_PLAN_NOT_FOUND 'План очистки не найден. Выполните проверку заново.'
        return 1
    }
    now="$(broray_cleanup_now_epoch)"
    expires="$(jq -r '.expiresEpoch // 0' "$plan" 2>/dev/null || printf '0')"
    broray_cleanup_is_uint "$expires" || expires=0
    [ "$expires" -ge "$now" ] || {
        rm -f "$plan" "$list"
        broray_cleanup_error CLEANUP_PLAN_EXPIRED 'Срок подтверждения истёк. Выполните проверку заново.'
        return 1
    }
    include_temp="$(jq -r '.options.temp // false' "$plan")"
    include_backups="$(jq -r '.options.backups // false' "$plan")"
    include_route_backups="$(jq -r '.options.routeBackups // false' "$plan")"
    include_logs="$(jq -r '.options.logs // false' "$plan")"
    expected_digest="$(jq -r '.digest // empty' "$plan")"
    current="$BRORAY_CLEANUP_PLAN_DIR/$token.current.$$"
    broray_cleanup_build_list "$current" "$include_temp" "$include_backups" "$include_route_backups" "$include_logs" || {
        rm -f "$current"
        broray_cleanup_error CLEANUP_RECHECK_FAILED 'Не удалось повторно проверить план очистки.'
        return 1
    }
    current_digest="$(broray_cleanup_list_digest "$current")"
    if [ "$current_digest" != "$expected_digest" ] || ! cmp -s "$list" "$current"; then
        rm -f "$current" "$plan" "$list"
        broray_cleanup_error CLEANUP_PLAN_CHANGED 'Состав файлов изменился. Выполните проверку заново.'
        return 1
    fi
    rm -f "$current"

    lock_rc=0
    broray_cleanup_global_lock_acquire || lock_rc=$?
    case "$lock_rc" in
        0) ;;
        2)
            broray_cleanup_error CLEANUP_OPERATION_BUSY 'Другая конфликтующая операция уже выполняется.'
            return 1
            ;;
        *)
            broray_cleanup_error CLEANUP_LOCK_FAILED 'Не удалось установить блокировку очистки.'
            return 1
            ;;
    esac

    deleted=0
    bytes=0
    failed=""
    tab="$(printf '\t')"
    while IFS="$tab" read -r category kind size relative; do
        [ -n "$relative" ] || continue
        broray_cleanup_path_allowed "$category" "$kind" "$relative" || {
            failed="$relative"
            break
        }
        path="$BRORAY_BASE/$relative"
        [ ! -L "$path" ] || {
            failed="$relative"
            break
        }
        if [ "$kind" = file ]; then
            [ -f "$path" ] || {
                failed="$relative"
                break
            }
            rm -f "$path" || {
                failed="$relative"
                break
            }
        else
            [ -d "$path" ] || {
                failed="$relative"
                break
            }
            rm -rf "$path" || {
                failed="$relative"
                break
            }
        fi
        deleted=$((deleted + 1))
        bytes=$((bytes + size))
    done <"$list"

    broray_cleanup_global_lock_release
    if [ -n "$failed" ]; then
        rm -f "$plan" "$list"
        broray_cleanup_error CLEANUP_DELETE_FAILED "Очистка остановлена на объекте: $failed"
        return 1
    fi
    rm -f "$plan" "$list"
    jq -nc \
        --arg completedAt "$(broray_cleanup_now_iso)" \
        --argjson deletedCount "$deleted" \
        --argjson freedBytes "$bytes" \
        '{ok:true,deletedCount:$deletedCount,freedBytes:$freedBytes,completedAt:$completedAt}'
}
