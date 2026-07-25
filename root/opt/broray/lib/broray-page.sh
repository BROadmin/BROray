#!/opt/bin/ash

# BROray 2.1 — backend страницы «BROray».
# Совместим с BusyBox ash. JSON формируется только через jq.

BRORAY_BASE="${BRORAY_BASE:-/opt/broray}"
BRORAY_BIN="$BRORAY_BASE/bin/broray-system"
BRORAY_RUN="$BRORAY_BASE/run/broray"
BRORAY_UPDATE="$BRORAY_BASE/update"
BRORAY_BACKUP="$BRORAY_BASE/backup"
BRORAY_STATUS="$BRORAY_RUN/operation.json"
BRORAY_UPDATE_CACHE="$BRORAY_RUN/update.json"
BRORAY_LOCK="$BRORAY_RUN/operation.lock"
BRORAY_LOG="$BRORAY_RUN/operation.log"
BRORAY_LAST_BACKUP="$BRORAY_RUN/last-backup"
BRORAY_PACKAGE="broray"
BRORAY_PROJECT_URL="https://docs.brovibe.cloud/broray/"
BRORAY_GITHUB_URL="https://github.com/BROadmin/BROray"
BRORAY_DONATE_URL="https://pay.cloudtips.ru/p/09b23d0a"

broray_system_now() {
    date -u '+%Y-%m-%dT%H:%M:%SZ'
}

broray_system_require_runtime() {
    command -v jq >/dev/null 2>&1 || {
        printf '%s\n' 'Для backend BROray требуется jq.' >&2
        return 1
    }
    mkdir -p "$BRORAY_RUN" "$BRORAY_UPDATE" "$BRORAY_BACKUP"
}

broray_system_atomic_json() {
    target="$1"
    tmp="$target.tmp.$$"
    cat >"$tmp" || {
        rm -f "$tmp"
        return 1
    }
    jq -e . "$tmp" >/dev/null 2>&1 || {
        rm -f "$tmp"
        return 1
    }
    mv -f "$tmp" "$target"
}

broray_system_error_json() {
    code="$1"
    message="$2"
    jq -nc \
        --arg code "$code" \
        --arg message "$message" \
        '{ok:false,error:{code:$code,message:$message}}'
}

broray_system_version() {
    version=""

    if [ -r "$BRORAY_BASE/config/version" ]; then
        version="$(sed -n '1p' "$BRORAY_BASE/config/version")"
    fi

    if [ -z "$version" ] && [ -r "$BRORAY_BASE/VERSION" ]; then
        version="$(sed -n '1p' "$BRORAY_BASE/VERSION")"
    fi

    if [ -z "$version" ] && command -v opkg >/dev/null 2>&1; then
        version="$(
            opkg list-installed "$BRORAY_PACKAGE" 2>/dev/null |
                awk -F ' - ' 'NR == 1 {print $2}'
        )"
    fi

    if [ -z "$version" ] && [ -x "$BRORAY_BASE/bin/broray" ]; then
        version="$(
            "$BRORAY_BASE/bin/broray" version 2>/dev/null |
                awk 'NR == 1 {print $NF}'
        )"
    fi

    [ -n "$version" ] || version="не определена"
    printf '%s\n' "$version"
}

broray_system_build() {
    if [ -r "$BRORAY_BASE/BUILD" ]; then
        sed -n '1p' "$BRORAY_BASE/BUILD"
        return
    fi

    if [ -r "$BRORAY_BASE/.build" ]; then
        sed -n '1p' "$BRORAY_BASE/.build"
        return
    fi

    printf '%s\n' 'Локальная сборка'
}

broray_system_architecture() {
    if [ -x "$BRORAY_BASE/bin/xray" ]; then
        arch="$(
            "$BRORAY_BASE/bin/xray" version 2>/dev/null |
                awk 'NR == 1 {
                    for (i = 1; i <= NF; i++) {
                        if ($i ~ /linux\//) {
                            sub(/.*linux\//, "", $i)
                            gsub(/[^[:alnum:]_.-].*$/, "", $i); print $i
                            exit
                        }
                    }
                }'
        )"
        [ -n "$arch" ] && {
            printf '%s\n' "$arch"
            return
        }
    fi

    uname -m 2>/dev/null || printf '%s\n' 'не определена'
}

broray_system_component_json() {
    id="$1"
    name="$2"
    path="$3"
    required="$4"
    version="$5"

    installed=false
    [ -e "$path" ] && installed=true

    jq -nc \
        --arg id "$id" \
        --arg name "$name" \
        --arg path "$path" \
        --arg version "$version" \
        --argjson installed "$installed" \
        --argjson required "$required" \
        '{
            id:$id,
            name:$name,
            path:$path,
            version:$version,
            installed:$installed,
            required:$required,
            healthy:$installed
        }'
}

broray_system_parser_available() {
    protocol="$1"
    case "$protocol" in
        vless)
            [ -s "$BRORAY_BASE/lib/parser-vless.sh" ]
            ;;
        vmess)
            [ -s "$BRORAY_BASE/lib/parser-vmess.sh" ]
            ;;
        trojan)
            [ -s "$BRORAY_BASE/lib/parser-trojan.sh" ]
            ;;
        hysteria2)
            [ -s "$BRORAY_BASE/lib/parser-hysteria2.sh" ]
            ;;
        *)
            return 1
            ;;
    esac
}

broray_system_operation_running() {
    [ -d "$BRORAY_LOCK" ] || return 1
    [ -r "$BRORAY_LOCK/pid" ] || return 0

    pid="$(sed -n '1p' "$BRORAY_LOCK/pid")"
    case "$pid" in
        ''|*[!0-9]*)
            return 0
            ;;
    esac

    kill -0 "$pid" 2>/dev/null
}

broray_system_recover_stale_lock() {
    [ -d "$BRORAY_LOCK" ] || return 0
    if broray_system_operation_running; then
        return 1
    fi
    rm -rf "$BRORAY_LOCK"
    return 0
}

broray_system_status_write() {
    operation_id="$1"
    operation="$2"
    state="$3"
    stage="$4"
    progress="$5"
    message="$6"
    error_message="${7:-}"
    updated_at="$(broray_system_now)"

    jq -nc \
        --arg operationId "$operation_id" \
        --arg operation "$operation" \
        --arg state "$state" \
        --arg stage "$stage" \
        --arg message "$message" \
        --arg error "$error_message" \
        --arg updatedAt "$updated_at" \
        --argjson progress "$progress" \
        '{
            ok:true,
            operationId:$operationId,
            operation:$operation,
            state:$state,
            stage:$stage,
            progress:$progress,
            message:$message,
            error:(if $error == "" then null else $error end),
            running:($state == "queued" or $state == "running" or $state == "restoring"),
            updatedAt:$updatedAt
        }' | broray_system_atomic_json "$BRORAY_STATUS"
}

broray_system_log_reset() {
    : >"$BRORAY_LOG"
}

broray_system_log() {
    printf '%s  %s\n' "$(broray_system_now)" "$*" >>"$BRORAY_LOG"
}

broray_system_status_json() {
    broray_system_require_runtime || {
        broray_system_error_json RUNTIME_ERROR 'Не удалось подготовить служебный каталог.'
        return 1
    }

    if [ ! -s "$BRORAY_STATUS" ]; then
        jq -nc \
            --arg updatedAt "$(broray_system_now)" \
            '{
                ok:true,
                operationId:null,
                operation:null,
                state:"idle",
                stage:"idle",
                progress:0,
                message:"Операции ещё не выполнялись.",
                error:null,
                running:false,
                logTail:"",
                updatedAt:$updatedAt
            }'
        return
    fi

    if jq -e '.running == true' "$BRORAY_STATUS" >/dev/null 2>&1; then
        if ! broray_system_operation_running; then
            operation_id="$(jq -r '.operationId // "unknown"' "$BRORAY_STATUS")"
            operation="$(jq -r '.operation // "unknown"' "$BRORAY_STATUS")"
            broray_system_status_write \
                "$operation_id" "$operation" error interrupted 100 \
                'Операция была прервана.' \
                'Фоновый процесс операции больше не выполняется.'
            rm -rf "$BRORAY_LOCK"
        fi
    fi

    log_tail=""
    [ -r "$BRORAY_LOG" ] && log_tail="$(tail -n 80 "$BRORAY_LOG" 2>/dev/null)"
    jq -c --arg logTail "$log_tail" '. + {logTail:$logTail}' "$BRORAY_STATUS"
}

broray_system_available_version() {
    if [ -s "$BRORAY_UPDATE_CACHE" ]; then
        jq -r '.availableVersion // empty' "$BRORAY_UPDATE_CACHE" 2>/dev/null
    fi
}

broray_system_info_json() {
    broray_system_require_runtime || {
        broray_system_error_json RUNTIME_ERROR 'Не удалось подготовить служебный каталог.'
        return 1
    }

    current_version="$(broray_system_version)"
    build="$(broray_system_build)"
    architecture="$(broray_system_architecture)"
    available_version="$(broray_system_available_version)"
    update_available=false
    last_checked_at=""
    last_backup=""

    if [ -s "$BRORAY_UPDATE_CACHE" ]; then
        update_available="$(jq -r '.updateAvailable // false' "$BRORAY_UPDATE_CACHE" 2>/dev/null)"
        last_checked_at="$(jq -r '.checkedAt // empty' "$BRORAY_UPDATE_CACHE" 2>/dev/null)"
    fi
    [ -r "$BRORAY_LAST_BACKUP" ] && last_backup="$(sed -n '1p' "$BRORAY_LAST_BACKUP")"

    components="$({
        broray_system_component_json core 'Ядро BROray' "$BRORAY_BASE/bin/broray" true "$current_version"
        broray_system_component_json xray 'Xray' "$BRORAY_BASE/bin/xray" true "$("$BRORAY_BASE/bin/xray" version 2>/dev/null | awk 'NR == 1 {print $2}')"
        broray_system_component_json servers 'Серверы' "$BRORAY_BASE/lib/server-service.sh" true '2.1'
        if [ -e "$BRORAY_BASE/lib/subscription-service.sh" ]; then
            broray_system_component_json subscriptions 'Подписки' "$BRORAY_BASE/lib/subscription-service.sh" true '2.1'
        else
            broray_system_component_json subscriptions 'Подписки' "$BRORAY_BASE/lib/subscription.sh" true '2.1'
        fi
        broray_system_component_json keenetic 'Keenetic' "$BRORAY_BASE/lib/keenetic-page.sh" true '2.1'
        broray_system_component_json routes 'Маршруты' "$BRORAY_BASE/bin/broray-routes" true '2.1'
        broray_system_component_json webui 'WebUI' "$BRORAY_BASE/web-new/home.html" true '2.1'
    } | jq -sc '.')"

    protocols="$({
        for protocol in vless vmess trojan hysteria2; do
            supported=false
            broray_system_parser_available "$protocol" && supported=true
            jq -nc \
                --arg id "$protocol" \
                --argjson supported "$supported" \
                '{id:$id,supported:$supported}'
        done
    } | jq -sc '.')"

    healthy="$(
        printf '%s' "$components" |
            jq 'all(.[]; (.required | not) or (.healthy == true))'
    )"

    jq -nc \
        --arg version "$current_version" \
        --arg build "$build" \
        --arg architecture "$architecture" \
        --arg channel 'stable' \
        --arg availableVersion "$available_version" \
        --arg lastCheckedAt "$last_checked_at" \
        --arg lastBackup "$last_backup" \
        --arg projectUrl "$BRORAY_PROJECT_URL" \
        --arg githubUrl "$BRORAY_GITHUB_URL" \
        --arg donateUrl "$BRORAY_DONATE_URL" \
        --arg updatedAt "$(broray_system_now)" \
        --argjson updateAvailable "$update_available" \
        --argjson installationHealthy "$healthy" \
        --argjson components "$components" \
        --argjson protocols "$protocols" \
        '{
            ok:true,
            version:$version,
            build:$build,
            architecture:$architecture,
            updateChannel:$channel,
            updateAvailable:$updateAvailable,
            availableVersion:(if $availableVersion == "" then null else $availableVersion end),
            lastCheckedAt:(if $lastCheckedAt == "" then null else $lastCheckedAt end),
            lastBackup:(if $lastBackup == "" then null else $lastBackup end),
            installationHealthy:$installationHealthy,
            components:$components,
            protocols:$protocols,
            capabilities:[
                "Управление Xray",
                "Импорт подписок",
                "Выбор активного сервера",
                "Интеграция с управляемым ProxyN",
                "Маршрутизация сервисов",
                "Проверка и восстановление компонентов",
                "Обновление через WebUI"
            ],
            links:{github:$githubUrl,project:$projectUrl,donate:$donateUrl},
            updatedAt:$updatedAt
        }'
}

broray_system_feed_value() {
    wanted_version="$1"
    wanted_key="$2"

    for feed in /opt/var/opkg-lists/*; do
        [ -f "$feed" ] || continue
        awk \
            -v wanted_package="$BRORAY_PACKAGE" \
            -v wanted_version="$wanted_version" \
            -v wanted_key="$wanted_key" '
            function reset_record() {
                package_name = ""
                package_version = ""
                value = ""
            }
            function emit_record() {
                if (package_name == wanted_package &&
                    package_version == wanted_version &&
                    value != "") {
                    print value
                    found = 1
                }
            }
            BEGIN {
                reset_record()
                found = 0
            }
            $1 == "Package:" {
                if (package_name != "") {
                    emit_record()
                    if (found == 1) {
                        exit
                    }
                }
                reset_record()
                package_name = $2
                next
            }
            $1 == "Version:" {
                package_version = $2
                next
            }
            index($0, wanted_key ":") == 1 {
                value = $0
                sub(wanted_key ":[ \t]*", "", value)
                next
            }
            END {
                if (found == 0) {
                    emit_record()
                }
            }
        ' "$feed"
    done | sed -n '1p'
}

broray_system_update_check_internal() {
    broray_system_log 'Обновляются списки пакетов OPKG.'
    opkg update >>"$BRORAY_LOG" 2>&1 || return 1

    line="$(
        opkg list-upgradable 2>/dev/null |
            awk -F ' - ' -v package="$BRORAY_PACKAGE" '$1 == package {print; exit}'
    )"
    current_version="$(broray_system_version)"
    available_version=""
    update_available=false

    if [ -n "$line" ]; then
        available_version="$(printf '%s\n' "$line" | awk -F ' - ' '{print $3}')"
        [ -n "$available_version" ] && update_available=true
    fi

    jq -nc \
        --arg currentVersion "$current_version" \
        --arg availableVersion "$available_version" \
        --arg checkedAt "$(broray_system_now)" \
        --argjson updateAvailable "$update_available" \
        '{
            ok:true,
            currentVersion:$currentVersion,
            availableVersion:(if $availableVersion == "" then null else $availableVersion end),
            updateAvailable:$updateAvailable,
            checkedAt:$checkedAt
        }' | broray_system_atomic_json "$BRORAY_UPDATE_CACHE"
}

broray_system_update_check() {
    broray_system_require_runtime || {
        broray_system_error_json RUNTIME_ERROR 'Не удалось подготовить служебный каталог.'
        return 1
    }

    broray_system_recover_stale_lock || {
        broray_system_error_json OPERATION_BUSY 'Сейчас выполняется другая операция.'
        return 1
    }

    broray_system_log_reset
    if ! broray_system_update_check_internal; then
        broray_system_error_json UPDATE_CHECK_FAILED 'Не удалось проверить обновление через OPKG.'
        return 1
    fi
    cat "$BRORAY_UPDATE_CACHE"
}

broray_system_backup_create() {
    purpose="$1"
    stamp="$(date -u '+%Y%m%d-%H%M%S')"
    archive="$BRORAY_BACKUP/${purpose}-${stamp}.tar.gz"
    temporary="/tmp/broray-backup-${purpose}-${stamp}.tar.gz"

    set --
    for item in \
        VERSION BUILD .build \
        bin lib web-new web config subscriptions servers routes; do
        [ -e "$BRORAY_BASE/$item" ] && set -- "$@" "$item"
    done

    [ "$#" -gt 0 ] || return 1
    rm -f "$temporary"
    tar -czf "$temporary" -C "$BRORAY_BASE" "$@" >>"$BRORAY_LOG" 2>&1 || {
        rm -f "$temporary"
        return 1
    }
    mv -f "$temporary" "$archive" || return 1
    printf '%s\n' "$archive" >"$BRORAY_LAST_BACKUP"
    printf '%s\n' "$archive"
}

broray_system_backup_restore() {
    archive="$1"
    [ -f "$archive" ] || return 1
    tar -xzf "$archive" -C "$BRORAY_BASE" >>"$BRORAY_LOG" 2>&1
}

broray_system_health_check() {
    required_files="
$BRORAY_BASE/bin/broray
$BRORAY_BASE/bin/xray
$BRORAY_BASE/lib/xray.sh
$BRORAY_BASE/lib/server-service.sh
$BRORAY_BASE/web-new/index.html
$BRORAY_BASE/web-new/home.html
$BRORAY_BASE/web-new/broray.html
$BRORAY_BASE/web-new/api/auth-common.sh
"

    printf '%s\n' "$required_files" |
        while IFS= read -r file; do
            [ -n "$file" ] || continue
            [ -s "$file" ] || {
                printf 'Отсутствует обязательный файл: %s\n' "$file" >&2
                exit 1
            }
        done || return 1

    for file in \
        "$BRORAY_BASE/lib/broray-page.sh" \
        "$BRORAY_BASE/lib/component-lifecycle.sh" \
        "$BRORAY_BASE/lib/web-publish.sh" \
        "$BRORAY_BASE/bin/broray-system" \
        "$BRORAY_BASE/web-new/api/broray/"*.cgi; do
        [ -f "$file" ] || continue
        ash -n "$file" || return 1
    done

    jq -e . "$BRORAY_BASE/config/config.json" >/dev/null 2>&1 || return 1
    return 0
}

broray_system_restart_services() {
    if [ -x /opt/etc/init.d/S25broray-web ]; then
        /opt/etc/init.d/S25broray-web restart >>"$BRORAY_LOG" 2>&1 || return 1
    fi
    if [ -x /opt/etc/init.d/S24broray ]; then
        /opt/etc/init.d/S24broray restart >>"$BRORAY_LOG" 2>&1 || return 1
    fi
    return 0
}

broray_system_worker_update() {
    operation_id="$1"
    backup=""

    broray_system_status_write "$operation_id" update running check 5 \
        'Проверяется наличие обновления.' ''

    if ! broray_system_update_check_internal; then
        broray_system_status_write "$operation_id" update error check 100 \
            'Проверка обновления завершилась ошибкой.' \
            'OPKG не смог обновить список пакетов.'
        return 1
    fi

    available_version="$(broray_system_available_version)"
    update_available="$(jq -r '.updateAvailable // false' "$BRORAY_UPDATE_CACHE")"
    if [ "$update_available" != true ] || [ -z "$available_version" ]; then
        broray_system_status_write "$operation_id" update success complete 100 \
            'Установлена актуальная версия BROray.' ''
        return 0
    fi

    staging="$BRORAY_UPDATE/$operation_id"
    rm -rf "$staging"
    mkdir -p "$staging" || return 1

    broray_system_status_write "$operation_id" update running download 20 \
        "Загружается BROray $available_version." ''
    broray_system_log "Загружается пакет BROray $available_version."
    (
        cd "$staging" || exit 1
        opkg download "$BRORAY_PACKAGE"
    ) >>"$BRORAY_LOG" 2>&1 || {
        broray_system_status_write "$operation_id" update error download 100 \
            'Не удалось загрузить пакет обновления.' \
            'Команда opkg download завершилась ошибкой.'
        return 1
    }

    package_file="$(find "$staging" -maxdepth 1 -type f -name 'broray_*.ipk' | sed -n '1p')"
    [ -n "$package_file" ] && [ -f "$package_file" ] || {
        broray_system_status_write "$operation_id" update error verify 100 \
            'Загруженный пакет не найден.' \
            'В каталоге обновления нет файла broray_*.ipk.'
        return 1
    }

    broray_system_status_write "$operation_id" update running verify 35 \
        'Проверяется пакет обновления.' ''

    expected_sha="$(broray_system_feed_value "$available_version" SHA256sum)"
    expected_filename="$(broray_system_feed_value "$available_version" Filename)"
    expected_arch="$(broray_system_feed_value "$available_version" Architecture)"
    actual_sha="$(sha256sum "$package_file" | awk '{print $1}')"

    [ -n "$expected_sha" ] && [ "$actual_sha" = "$expected_sha" ] || {
        broray_system_status_write "$operation_id" update error verify 100 \
            'Контрольная сумма пакета не совпала.' \
            'Установка остановлена до изменения файлов.'
        return 1
    }

    if [ -n "$expected_filename" ]; then
        expected_basename="$(basename "$expected_filename")"
        actual_basename="$(basename "$package_file")"
        [ "$actual_basename" = "$expected_basename" ] || {
            broray_system_status_write "$operation_id" update error verify 100 \
                'Имя пакета не совпало с индексом репозитория.' \
                'Установка остановлена до изменения файлов.'
            return 1
        }
    fi

    if [ -n "$expected_arch" ] && [ "$expected_arch" != all ]; then
        opkg print-architecture 2>/dev/null |
            awk -v wanted="$expected_arch" '$2 == wanted {found = 1} END {exit(found ? 0 : 1)}' || {
                broray_system_status_write "$operation_id" update error verify 100 \
                    'Архитектура пакета не поддерживается устройством.' \
                    "Ожидалась архитектура $expected_arch."
                return 1
            }
    fi

    free_kb="$(df -Pk /opt 2>/dev/null | awk 'NR == 2 {print $4}')"
    used_kb="$(du -sk "$BRORAY_BASE" 2>/dev/null | awk 'NR == 1 {print $1}')"
    package_kb="$(du -k "$package_file" 2>/dev/null | awk 'NR == 1 {print $1}')"
    case "$free_kb:$used_kb:$package_kb" in
        *[!0-9:]*|'')
            broray_system_status_write "$operation_id" update error verify 100 \
                'Не удалось определить свободное место.' \
                'Установка остановлена до создания резервной копии.'
            return 1
            ;;
    esac
    required_kb=$((used_kb + package_kb * 2 + 10240))
    [ "$free_kb" -gt "$required_kb" ] || {
        broray_system_status_write "$operation_id" update error verify 100 \
            'Недостаточно свободного места для безопасного обновления.' \
            "Требуется не менее ${required_kb} КБ свободного места."
        return 1
    }

    broray_system_status_write "$operation_id" update running backup 50 \
        'Создаётся резервная копия.' ''
    backup="$(broray_system_backup_create system-before-update)" || {
        broray_system_status_write "$operation_id" update error backup 100 \
            'Не удалось создать резервную копию.' \
            'Установка пакета не запускалась.'
        return 1
    }
    broray_system_log "Создана резервная копия: $backup"

    broray_system_status_write "$operation_id" update running install 68 \
        "Устанавливается BROray $available_version." ''
    opkg install "$package_file" >>"$BRORAY_LOG" 2>&1 || {
        broray_system_log 'OPKG сообщил об ошибке. Запускается восстановление.'
        broray_system_status_write "$operation_id" update restoring restore 82 \
            'Обновление завершилось ошибкой. Восстанавливается предыдущая версия.' ''
        if broray_system_backup_restore "$backup" && broray_system_restart_services; then
            broray_system_status_write "$operation_id" update error restored 100 \
                'Предыдущая версия восстановлена.' \
                'OPKG не смог установить обновление.'
        else
            broray_system_status_write "$operation_id" update error restore-failed 100 \
                'Автоматическое восстановление не завершено.' \
                "Резервная копия: $backup"
        fi
        return 1
    }

    broray_system_status_write "$operation_id" update running health 88 \
        'Проверяется обновлённая установка.' ''
    if ! broray_system_health_check; then
        broray_system_log 'Проверка установки не пройдена. Запускается восстановление.'
        broray_system_status_write "$operation_id" update restoring restore 92 \
            'Проверка не пройдена. Восстанавливается предыдущая версия.' ''
        if broray_system_backup_restore "$backup" && broray_system_restart_services; then
            broray_system_status_write "$operation_id" update error restored 100 \
                'Предыдущая версия восстановлена.' \
                'Обновлённая установка не прошла проверку.'
        else
            broray_system_status_write "$operation_id" update error restore-failed 100 \
                'Автоматическое восстановление не завершено.' \
                "Резервная копия: $backup"
        fi
        return 1
    fi

    rm -rf "$staging"
    broray_system_status_write "$operation_id" update success complete 100 \
        "BROray обновлён до версии $(broray_system_version)." ''
    return 0
}

broray_system_worker_restore() {
    operation_id="$1"
    [ -r "$BRORAY_LAST_BACKUP" ] || {
        broray_system_status_write "$operation_id" restore error backup 100 \
            'Резервная копия не найдена.' \
            'Сначала необходимо выполнить обновление с резервным копированием.'
        return 1
    }
    archive="$(sed -n '1p' "$BRORAY_LAST_BACKUP")"

    broray_system_status_write "$operation_id" restore running restore 30 \
        'Восстанавливаются файлы BROray.' ''
    broray_system_backup_restore "$archive" || {
        broray_system_status_write "$operation_id" restore error restore 100 \
            'Не удалось распаковать резервную копию.' "$archive"
        return 1
    }

    broray_system_status_write "$operation_id" restore running health 75 \
        'Проверяется восстановленная установка.' ''
    broray_system_health_check && broray_system_restart_services || {
        broray_system_status_write "$operation_id" restore error health 100 \
            'Восстановленная установка не прошла проверку.' "$archive"
        return 1
    }

    broray_system_status_write "$operation_id" restore success complete 100 \
        'BROray восстановлен из резервной копии.' ''
}

broray_system_copy_worker() {
    operation_id="$1"
    worker_base="/tmp/broray-system-$operation_id"
    worker_bin="$worker_base.sh"
    worker_lib="$worker_base.lib.sh"

    cp -p "$BRORAY_BIN" "$worker_bin" || return 1
    cp -p "$BRORAY_BASE/lib/broray-page.sh" "$worker_lib" || {
        rm -f "$worker_bin"
        return 1
    }
    chmod 700 "$worker_bin" "$worker_lib"
    printf '%s\n%s\n' "$worker_bin" "$worker_lib"
}

broray_system_start_worker() {
    operation="$1"
    mode="${2:-}"
    broray_system_require_runtime || {
        broray_system_error_json RUNTIME_ERROR 'Не удалось подготовить служебный каталог.'
        return 1
    }

    broray_system_recover_stale_lock || {
        broray_system_error_json OPERATION_BUSY 'Сейчас выполняется другая операция.'
        return 1
    }

    mkdir "$BRORAY_LOCK" 2>/dev/null || {
        broray_system_error_json OPERATION_BUSY 'Сейчас выполняется другая операция.'
        return 1
    }

    operation_id="${operation}-$(date -u '+%Y%m%d%H%M%S')-$$"
    workers="$(broray_system_copy_worker "$operation_id")" || {
        rm -rf "$BRORAY_LOCK"
        broray_system_error_json WORKER_COPY_FAILED 'Не удалось подготовить фоновую операцию.'
        return 1
    }
    worker_bin="$(printf '%s\n' "$workers" | sed -n '1p')"
    worker_lib="$(printf '%s\n' "$workers" | sed -n '2p')"

    broray_system_log_reset
    broray_system_status_write "$operation_id" "$operation" queued queued 0 \
        'Операция поставлена в очередь.' ''

    BRORAY_SYSTEM_LIB="$worker_lib" \
        ash "$worker_bin" "worker-$operation" "$operation_id" "$mode" \
        >>"$BRORAY_LOG" 2>&1 </dev/null &
    worker_pid=$!
    printf '%s\n' "$worker_pid" >"$BRORAY_LOCK/pid"
    printf '%s\n' "$operation_id" >"$BRORAY_LOCK/operation-id"

    jq -nc \
        --arg operationId "$operation_id" \
        --arg operation "$operation" \
        '{ok:true,accepted:true,operationId:$operationId,operation:$operation}'
}

broray_system_worker_finish() {
    worker_bin="$1"
    worker_lib="$2"
    rm -rf "$BRORAY_LOCK"
    rm -f "$worker_bin" "$worker_lib"
}

broray_system_uninstall_start() {
    mode="$1"
    confirmation="$2"

    case "$mode" in
        normal)
            expected='УДАЛИТЬ BROray'
            ;;
        full)
            expected='УДАЛИТЬ BROray ПОЛНОСТЬЮ'
            ;;
        *)
            broray_system_error_json INVALID_MODE 'Неизвестный режим удаления.'
            return 1
            ;;
    esac

    [ "$confirmation" = "$expected" ] || {
        broray_system_error_json CONFIRMATION_REQUIRED 'Фраза подтверждения не совпала.'
        return 1
    }

    broray_system_start_worker uninstall "$mode"
}

broray_system_worker_uninstall() {
    operation_id="$1"
    mode="$2"
    preserved_dir="/opt/broray-preserved"
    preserved_archive=""

    broray_system_status_write "$operation_id" uninstall running backup 10 \
        'Сохраняются данные перед удалением.' ''

    if [ "$mode" = normal ]; then
        mkdir -p "$preserved_dir" || return 1
        stamp="$(date -u '+%Y%m%d-%H%M%S')"
        preserved_archive="$preserved_dir/broray-user-data-$stamp.tar.gz"
        temporary="/tmp/broray-user-data-$stamp.tar.gz"
        set --
        for item in config subscriptions servers routes backup; do
            [ -e "$BRORAY_BASE/$item" ] && set -- "$@" "$item"
        done
        [ "$#" -gt 0 ] && \
            tar -czf "$temporary" -C "$BRORAY_BASE" "$@" >>"$BRORAY_LOG" 2>&1 && \
            mv -f "$temporary" "$preserved_archive" || {
                broray_system_status_write "$operation_id" uninstall error backup 100 \
                    'Удаление отменено: данные сохранить не удалось.' ''
                return 1
            }
        broray_system_log "Пользовательские данные сохранены: $preserved_archive"
    fi

    [ -r "$BRORAY_BASE/lib/component-lifecycle.sh" ] || {
        broray_system_status_write "$operation_id" uninstall error lifecycle 100 \
            'Модуль безопасного удаления отсутствует.' \
            "$BRORAY_BASE/lib/component-lifecycle.sh"
        return 1
    }
    . "$BRORAY_BASE/lib/component-lifecycle.sh"

    broray_system_status_write "$operation_id" uninstall running routes 25 \
        'Удаляются маршруты BROray.' ''
    broray_lifecycle_routes_remove_all >>"$BRORAY_LOG" 2>&1 || return 1

    broray_system_status_write "$operation_id" uninstall running keenetic 38 \
        'Удаляется управляемый ProxyN.' ''
    broray_lifecycle_keenetic_delete >>"$BRORAY_LOG" 2>&1 || return 1

    broray_system_status_write "$operation_id" uninstall running publish 48 \
        'Удаляется KeenDNS HTTP Proxy BROray.' ''
    broray_lifecycle_web_publish_delete >>"$BRORAY_LOG" 2>&1 || return 1

    broray_system_status_write "$operation_id" uninstall running servers 58 \
        'Отключается активный сервер.' ''
    broray_lifecycle_servers_deactivate >>"$BRORAY_LOG" 2>&1 || return 1

    broray_system_status_write "$operation_id" uninstall running xray 68 \
        'Останавливается Xray.' ''
    broray_lifecycle_xray_stop >>"$BRORAY_LOG" 2>&1 || return 1

    broray_system_status_write "$operation_id" uninstall running remove 82 \
        'Удаляется пакет BROray.' ''
    opkg remove "$BRORAY_PACKAGE" >>"$BRORAY_LOG" 2>&1 || {
        broray_system_status_write "$operation_id" uninstall error remove 100 \
            'OPKG не смог удалить BROray.' ''
        return 1
    }

    if [ "$mode" = full ]; then
        [ "$BRORAY_BASE" = /opt/broray ] || return 1
        rm -rf /opt/broray
        rm -f /opt/etc/opkg/broray.conf
        rm -rf /opt/broray-preserved
    fi

    jq -nc \
        --arg mode "$mode" \
        --arg preservedArchive "$preserved_archive" \
        --arg completedAt "$(broray_system_now)" \
        '{
            ok:true,
            mode:$mode,
            preservedArchive:(if $preservedArchive == "" then null else $preservedArchive end),
            completedAt:$completedAt
        }' >/tmp/broray-uninstall-result.json
    return 0
}

broray_system_main() {
    command_name="${1:-}"
    shift 2>/dev/null || true

    case "$command_name" in
        info)
            broray_system_info_json
            ;;
        status)
            broray_system_status_json
            ;;
        update-check)
            broray_system_update_check
            ;;
        update-start)
            broray_system_start_worker update
            ;;
        restore-start)
            broray_system_start_worker restore
            ;;
        uninstall-start)
            broray_system_uninstall_start "${1:-}" "${2:-}"
            ;;
        worker-update|worker-restore|worker-uninstall)
            operation_id="${1:-}"
            mode="${2:-}"
            worker_bin="$0"
            worker_lib="${BRORAY_SYSTEM_LIB:-$BRORAY_BASE/lib/broray-page.sh}"
            result=0
            case "$command_name" in
                worker-update)
                    broray_system_worker_update "$operation_id" || result=$?
                    ;;
                worker-restore)
                    broray_system_worker_restore "$operation_id" || result=$?
                    ;;
                worker-uninstall)
                    broray_system_worker_uninstall "$operation_id" "$mode" || result=$?
                    ;;
            esac
            if [ "$result" -ne 0 ] && [ -s "$BRORAY_STATUS" ]; then
                if jq -e '.running == true' "$BRORAY_STATUS" >/dev/null 2>&1; then
                    operation="$(jq -r '.operation // "operation"' "$BRORAY_STATUS")"
                    broray_system_status_write \
                        "$operation_id" "$operation" error failed 100 \
                        'Операция завершилась ошибкой.' \
                        'Подробности сохранены в техническом журнале.'
                fi
            fi
            broray_system_worker_finish "$worker_bin" "$worker_lib"
            return "$result"
            ;;
        *)
            printf '%s\n' 'Использование: broray-system {info|status|update-check|update-start|restore-start|uninstall-start}' >&2
            return 2
            ;;
    esac
}
