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
BRORAY_GLOBAL_LOCK="${BRORAY_GLOBAL_LOCK:-$BRORAY_BASE/run/global-operation.lock}"
BRORAY_GLOBAL_LOCK_HELD=false
BRORAY_LOG="$BRORAY_RUN/operation.log"
BRORAY_LAST_BACKUP="$BRORAY_RUN/last-backup"
BRORAY_PACKAGE="broray"
BRORAY_INIT_ROOT="${BRORAY_INIT_ROOT:-/opt/etc/init.d}"
BRORAY_FEED_FILE="${BRORAY_FEED_FILE:-/opt/etc/opkg/broray.conf}"
BRORAY_OPKG_LISTS_DIR="${BRORAY_OPKG_LISTS_DIR:-/opt/var/opkg-lists}"
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

broray_system_is_pid() {
    case "${1:-}" in
        ''|*[!0-9]*) return 1 ;;
    esac
    return 0
}

broray_system_global_operation_running() {
    [ -d "$BRORAY_GLOBAL_LOCK" ] || return 1
    [ -r "$BRORAY_GLOBAL_LOCK/pid" ] || return 1
    global_pid="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/pid" 2>/dev/null)"
    broray_system_is_pid "$global_pid" || return 1
    kill -0 "$global_pid" 2>/dev/null
}

broray_system_global_lock_recover_stale() {
    [ -d "$BRORAY_GLOBAL_LOCK" ] || return 0
    broray_system_global_operation_running && return 1
    rm -rf "$BRORAY_GLOBAL_LOCK"
}

broray_system_global_lock_acquire() {
    global_action="${1:-system}"
    mkdir -p "$(dirname "$BRORAY_GLOBAL_LOCK")" || return 1
    broray_system_global_lock_recover_stale || return 2
    mkdir "$BRORAY_GLOBAL_LOCK" 2>/dev/null || return 2
    printf '%s\n' "$$" >"$BRORAY_GLOBAL_LOCK/pid" || {
        rm -rf "$BRORAY_GLOBAL_LOCK"
        return 1
    }
    printf '%s\n' system >"$BRORAY_GLOBAL_LOCK/scope" || {
        rm -rf "$BRORAY_GLOBAL_LOCK"
        return 1
    }
    printf '%s\n' "$global_action" >"$BRORAY_GLOBAL_LOCK/action" || {
        rm -rf "$BRORAY_GLOBAL_LOCK"
        return 1
    }
    : >"$BRORAY_GLOBAL_LOCK/bundle" || {
        rm -rf "$BRORAY_GLOBAL_LOCK"
        return 1
    }
    printf '%s\n' "$(broray_system_now)" >"$BRORAY_GLOBAL_LOCK/startedAt" || {
        rm -rf "$BRORAY_GLOBAL_LOCK"
        return 1
    }
    BRORAY_GLOBAL_LOCK_HELD=true
    return 0
}

broray_system_global_lock_transfer() {
    global_pid="${1:-}"
    global_operation_id="${2:-}"
    broray_system_is_pid "$global_pid" || return 1
    [ -d "$BRORAY_GLOBAL_LOCK" ] || return 1
    [ "$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/scope" 2>/dev/null || true)" = system ] || return 1
    [ "$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/pid" 2>/dev/null || true)" = "$$" ] || return 1
    printf '%s\n' "$global_pid" >"$BRORAY_GLOBAL_LOCK/pid" || return 1
    printf '%s\n' "$global_operation_id" >"$BRORAY_GLOBAL_LOCK/operation-id" || return 1
    BRORAY_GLOBAL_LOCK_HELD=false
    return 0
}

broray_system_global_lock_release() {
    [ -d "$BRORAY_GLOBAL_LOCK" ] || {
        BRORAY_GLOBAL_LOCK_HELD=false
        return 0
    }
    global_pid="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/pid" 2>/dev/null || true)"
    global_scope="$(sed -n '1p' "$BRORAY_GLOBAL_LOCK/scope" 2>/dev/null || true)"
    if [ "$global_scope" = system ] && {
        [ "$BRORAY_GLOBAL_LOCK_HELD" = true ] || [ "$global_pid" = "$$" ];
    }; then
        rm -rf "$BRORAY_GLOBAL_LOCK" 2>/dev/null || true
    fi
    BRORAY_GLOBAL_LOCK_HELD=false
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
            broray_system_global_lock_release
            broray_system_global_lock_recover_stale >/dev/null 2>&1 || true
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
    installed_package_version="$(broray_system_installed_package_version 2>/dev/null || true)"
    reinstall_supported=false
    if [ -n "$installed_package_version" ] &&
       command -v opkg >/dev/null 2>&1 &&
       command -v curl >/dev/null 2>&1 &&
       command -v sha256sum >/dev/null 2>&1 &&
       command -v tar >/dev/null 2>&1
    then
        reinstall_supported=true
    fi
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
        --arg installedPackageVersion "$installed_package_version" \
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
        --argjson reinstallSupported "$reinstall_supported" \
        --argjson installationHealthy "$healthy" \
        --argjson components "$components" \
        --argjson protocols "$protocols" \
        '{
            ok:true,
            version:$version,
            installedPackageVersion:(if $installedPackageVersion == "" then null else $installedPackageVersion end),
            reinstallSupported:$reinstallSupported,
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
                "Обновление через WebUI",
                "Восстановительная переустановка текущей версии",
                "Безопасная очистка резервных копий и временных файлов"
            ],
            links:{github:$githubUrl,project:$projectUrl,donate:$donateUrl},
            updatedAt:$updatedAt
        }'
}

broray_system_feed_value() {
    wanted_version="$1"
    wanted_key="$2"

    for feed in "$BRORAY_OPKG_LISTS_DIR"/*; do
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

broray_system_installed_package_version() {
    opkg list-installed "$BRORAY_PACKAGE" 2>/dev/null |
        awk -F ' - ' -v package="$BRORAY_PACKAGE" '
            $1 == package {
                print $2
                exit
            }
        '
}

broray_system_installed_package_architecture() {
    opkg status "$BRORAY_PACKAGE" 2>/dev/null |
        awk '
            $1 == "Architecture:" {
                print $2
                exit
            }
        '
}

broray_system_feed_base_url() {
    [ -r "$BRORAY_FEED_FILE" ] || return 1

    awk \
        -v package="$BRORAY_PACKAGE" '
            $1 == "src/gz" && $2 == package {
                print $3
                exit
            }
        ' "$BRORAY_FEED_FILE"
}

broray_system_ipk_control_value() {
    broray_ipk_file="$1"
    broray_ipk_key="$2"
    broray_ipk_work="/tmp/broray-ipk-control-$$"
    broray_ipk_control_tar="$broray_ipk_work/control.tar.gz"
    broray_ipk_control="$broray_ipk_work/control"
    broray_ipk_value=""

    rm -rf "$broray_ipk_work"
    mkdir -p "$broray_ipk_work" || return 1

    if ! tar -xzOf "$broray_ipk_file" ./control.tar.gz \
        >"$broray_ipk_control_tar" 2>/dev/null
    then
        tar -xzOf "$broray_ipk_file" control.tar.gz \
            >"$broray_ipk_control_tar" 2>/dev/null || {
                rm -rf "$broray_ipk_work"
                return 1
            }
    fi

    if ! tar -xzOf "$broray_ipk_control_tar" ./control \
        >"$broray_ipk_control" 2>/dev/null
    then
        tar -xzOf "$broray_ipk_control_tar" control \
            >"$broray_ipk_control" 2>/dev/null || {
                rm -rf "$broray_ipk_work"
                return 1
            }
    fi

    broray_ipk_value="$(
        awk \
            -v wanted="$broray_ipk_key" '
                index($0, wanted ":") == 1 {
                    value = $0
                    sub(wanted ":[ \t]*", "", value)
                    print value
                    exit
                }
            ' "$broray_ipk_control"
    )"

    rm -rf "$broray_ipk_work"
    [ -n "$broray_ipk_value" ] || return 1
    printf '%s\n' "$broray_ipk_value"
}

broray_system_make_rollback_ipk() {
    broray_original_ipk="$1"
    broray_rollback_ipk="$2"
    broray_repack_root="/tmp/broray-rollback-repack-$$"
    broray_repack_outer="$broray_repack_root/outer"
    broray_repack_control="$broray_repack_root/control"

    rm -rf "$broray_repack_root"
    mkdir -p "$broray_repack_outer" "$broray_repack_control" ||
        return 1

    tar -xzf "$broray_original_ipk" -C "$broray_repack_outer" ||
        return 1
    tar -xzf "$broray_repack_outer/control.tar.gz" \
        -C "$broray_repack_control" ||
        return 1

    cat >"$broray_repack_control/preinst" <<'EOF'
#!/bin/sh

[ -z "${IPKG_INSTROOT:-}" ] || exit 0

for service in \
    /opt/etc/init.d/S28broray-subscriptions \
    /opt/etc/init.d/S27broray-auto-switch \
    /opt/etc/init.d/S25broray-web \
    /opt/etc/init.d/S24broray \
    /opt/etc/init.d/S23broray-monitor
do
    [ -x "$service" ] || continue
    "$service" stop >/dev/null 2>&1 || true
done

exit 0
EOF

    cat >"$broray_repack_control/postinst" <<'EOF'
#!/bin/sh

# The update worker restores the exact pre-upgrade snapshot and starts all
# services after OPKG has restored the previous package version.
exit 0
EOF

    chmod 755 \
        "$broray_repack_control/preinst" \
        "$broray_repack_control/postinst" ||
        return 1

    (
        cd "$broray_repack_control" || exit 1
        tar -czf "$broray_repack_outer/control.tar.gz" .
    ) || return 1

    (
        cd "$broray_repack_outer" || exit 1
        tar -czf "$broray_rollback_ipk" \
            ./debian-binary \
            ./data.tar.gz \
            ./control.tar.gz
    ) || return 1

    rm -rf "$broray_repack_root"
    [ -s "$broray_rollback_ipk" ]
}

broray_system_prepare_rollback_package() {
    broray_rollback_dir="$1"
    broray_rollback_version="$2"
    broray_rollback_arch="$(broray_system_installed_package_architecture)"
    broray_rollback_feed="$(broray_system_feed_base_url)"
    broray_rollback_filename="$(
        broray_system_feed_value "$broray_rollback_version" Filename
    )"
    broray_rollback_sha="$(
        broray_system_feed_value "$broray_rollback_version" SHA256sum
    )"

    case "$broray_rollback_version" in
        ''|*[!0-9A-Za-z._+-]*)
            return 1
            ;;
    esac
    case "$broray_rollback_arch" in
        ''|*[!0-9A-Za-z._+-]*)
            return 1
            ;;
    esac
    [ -n "$broray_rollback_feed" ] || return 1

    if [ -z "$broray_rollback_filename" ]; then
        broray_rollback_filename="$BRORAY_PACKAGE"_"$broray_rollback_version"_"$broray_rollback_arch".ipk
    else
        broray_rollback_filename="${broray_rollback_filename##*/}"
    fi

    mkdir -p "$broray_rollback_dir" || return 1
    broray_downloaded_ipk="$broray_rollback_dir/original-$broray_rollback_filename"
    broray_downloaded_part="$broray_downloaded_ipk.part"
    broray_ready_ipk="$broray_rollback_dir/rollback-$broray_rollback_filename"

    rm -f \
        "$broray_downloaded_part" \
        "$broray_downloaded_ipk" \
        "$broray_ready_ipk"

    curl \
        -fL \
        --connect-timeout 15 \
        --max-time 180 \
        -o "$broray_downloaded_part" \
        "${broray_rollback_feed%/}/$broray_rollback_filename" \
        >>"$BRORAY_LOG" 2>&1 ||
        return 1

    mv "$broray_downloaded_part" "$broray_downloaded_ipk" ||
        return 1

    if [ -n "$broray_rollback_sha" ]; then
        broray_rollback_actual_sha="$(
            sha256sum "$broray_downloaded_ipk" |
                awk '{print $1}'
        )"
        [ "$broray_rollback_actual_sha" = "$broray_rollback_sha" ] ||
            return 1
    fi

    [ "$(
        broray_system_ipk_control_value \
            "$broray_downloaded_ipk" Package
    )" = "$BRORAY_PACKAGE" ] ||
        return 1
    [ "$(
        broray_system_ipk_control_value \
            "$broray_downloaded_ipk" Version
    )" = "$broray_rollback_version" ] ||
        return 1
    [ "$(
        broray_system_ipk_control_value \
            "$broray_downloaded_ipk" Architecture
    )" = "$broray_rollback_arch" ] ||
        return 1

    broray_system_make_rollback_ipk \
        "$broray_downloaded_ipk" \
        "$broray_ready_ipk" ||
        return 1

    [ "$(
        broray_system_ipk_control_value \
            "$broray_ready_ipk" Version
    )" = "$broray_rollback_version" ] ||
        return 1

    printf '%s\n' "$broray_ready_ipk"
}

broray_system_rollback_package() {
    broray_rollback_package="$1"
    broray_rollback_version="$2"
    broray_rollback_backup="$3"

    [ -s "$broray_rollback_package" ] || return 1
    [ -s "$broray_rollback_backup" ] || return 1

    rm -f /tmp/broray-opkg-existing-backup
    broray_system_log \
        "OPKG возвращается к пакету $broray_rollback_version."

    opkg install \
        --force-downgrade \
        "$broray_rollback_package" \
        >>"$BRORAY_LOG" 2>&1 ||
        return 1

    [ "$(
        broray_system_installed_package_version
    )" = "$broray_rollback_version" ] ||
        return 1

    broray_system_backup_restore "$broray_rollback_backup" ||
        return 1
    broray_system_restart_services ||
        return 1
    broray_system_health_check || return 1
    rm -f \
        /tmp/broray-opkg-services-before-upgrade \
        /tmp/broray-opkg-existing-backup
    return 0
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
        bin lib web-new web config data deleted-subscriptions subscriptions servers routes; do
        [ -e "$BRORAY_BASE/$item" ] && set -- "$@" "$item"
    done

    [ "$#" -gt 0 ] || return 1
    rm -f "$temporary"
    tar -czf "$temporary" -C "$BRORAY_BASE" "$@" >>"$BRORAY_LOG" 2>&1 || {
        rm -f "$temporary"
        return 1
    }

    backup_size_kb="$(du -k "$temporary" 2>/dev/null | awk 'NR == 1 {print $1}')"
    backup_free_kb="$(df -Pk "$BRORAY_BACKUP" 2>/dev/null | awk 'NR == 2 {print $4}')"
    case "$backup_size_kb:$backup_free_kb" in
        *[!0-9:]*|'')
            rm -f "$temporary"
            return 1
            ;;
    esac
    [ "$backup_free_kb" -gt $((backup_size_kb + 2048)) ] || {
        rm -f "$temporary"
        return 1
    }

    mv -f "$temporary" "$archive" || {
        rm -f "$temporary"
        return 1
    }
    printf '%s\n' "$archive" >"$BRORAY_LAST_BACKUP"
    printf '%s\n' "$archive"
}

broray_system_archive_safe() {
    local archive list result
    archive="$1"
    list="/tmp/broray-archive-list-$$"
    [ -s "$archive" ] || return 1
    rm -f "$list"
    tar -tzf "$archive" >"$list" 2>/dev/null || {
        rm -f "$list"
        return 1
    }
    awk '
        /^\// {bad = 1}
        /(^|\/)\.\.($|\/)/ {bad = 1}
        END {exit bad ? 1 : 0}
    ' "$list"
    result=$?
    rm -f "$list"
    return "$result"
}

broray_system_backup_restore() {
    local archive members roots root
    archive="$1"
    members="/tmp/broray-restore-members-$$"
    roots="/tmp/broray-restore-roots-$$"
    broray_system_archive_safe "$archive" || return 1
    tar -tzf "$archive" >"$members" 2>/dev/null || {
        rm -f "$members" "$roots"
        return 1
    }
    sed 's#^\./##; s#/.*##' "$members" |
        awk 'NF > 0 && $0 != "." {print}' |
        sort -u >"$roots" || {
            rm -f "$members" "$roots"
            return 1
        }
    while IFS= read -r root; do
        case "$root" in
            VERSION|BUILD|.build|bin|lib|web-new|web|config|data|deleted-subscriptions|subscriptions|servers|routes)
                rm -rf "$BRORAY_BASE/$root" || {
                    rm -f "$members" "$roots"
                    return 1
                }
                ;;
            *)
                rm -f "$members" "$roots"
                return 1
                ;;
        esac
    done <"$roots"
    rm -f "$members" "$roots"
    tar -xzf "$archive" -C "$BRORAY_BASE" >>"$BRORAY_LOG" 2>&1
}

broray_system_health_check() {
    required_files="
$BRORAY_BASE/bin/broray
$BRORAY_BASE/bin/xray
$BRORAY_BASE/lib/xray.sh
$BRORAY_BASE/lib/server-service.sh
$BRORAY_BASE/bin/broray-routes-dot
$BRORAY_BASE/lib/routes-dot.sh
$BRORAY_BASE/web-new/index.html
$BRORAY_BASE/web-new/home.html
$BRORAY_BASE/web-new/broray.html
$BRORAY_BASE/web-new/api/auth-common.sh
$BRORAY_BASE/web-new/api/broray/reinstall.cgi
$BRORAY_BASE/web-new/api/routes/dot-status.cgi
$BRORAY_INIT_ROOT/S23broray-monitor
$BRORAY_INIT_ROOT/S24broray
$BRORAY_INIT_ROOT/S25broray-web
$BRORAY_INIT_ROOT/S27broray-auto-switch
$BRORAY_INIT_ROOT/S28broray-subscriptions
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
    broray_system_services_health_check
}

broray_system_services_health_check() {
    for broray_service in \
        "$BRORAY_INIT_ROOT/S23broray-monitor" \
        "$BRORAY_INIT_ROOT/S24broray" \
        "$BRORAY_INIT_ROOT/S25broray-web" \
        "$BRORAY_INIT_ROOT/S27broray-auto-switch" \
        "$BRORAY_INIT_ROOT/S28broray-subscriptions"
    do
        [ -x "$broray_service" ] || return 1
        "$broray_service" status >>"$BRORAY_LOG" 2>&1 ||
            return 1
    done

    return 0
}

broray_system_restart_services() {
    for broray_service in \
        "$BRORAY_INIT_ROOT/S24broray" \
        "$BRORAY_INIT_ROOT/S23broray-monitor" \
        "$BRORAY_INIT_ROOT/S27broray-auto-switch" \
        "$BRORAY_INIT_ROOT/S28broray-subscriptions" \
        "$BRORAY_INIT_ROOT/S25broray-web"
    do
        [ -x "$broray_service" ] || return 1
        "$broray_service" restart >>"$BRORAY_LOG" 2>&1 ||
            return 1
    done

    broray_system_services_health_check
}

broray_system_reinstall_user_items() {
    for item in config data deleted-subscriptions subscriptions servers routes; do
        [ -e "$BRORAY_BASE/$item" ] && printf '%s\n' "$item"
    done
}

broray_system_reinstall_user_backup_create() {
    local purpose stamp archive temporary staging items item item_size_kb
    local source_size_kb backup_size_kb backup_free_kb required_kb
    purpose="$1"
    stamp="$(date -u '+%Y%m%d-%H%M%S')"
    archive="$BRORAY_BACKUP/${purpose}-${stamp}.tar.gz"
    temporary="$BRORAY_BACKUP/.${purpose}-${stamp}.tar.gz.$$"
    staging="$BRORAY_BACKUP/.${purpose}-stage-$$"
    items="/tmp/broray-${purpose}-items-$$"

    rm -rf "$staging"
    rm -f "$temporary" "$items"
    broray_system_reinstall_user_items >"$items" || return 1
    [ -s "$items" ] || {
        rm -f "$items"
        return 1
    }

    source_size_kb=0
    while IFS= read -r item; do
        [ -n "$item" ] || continue
        item_size_kb="$(du -sk "$BRORAY_BASE/$item" 2>/dev/null | awk 'NR == 1 {print $1}')"
        case "$item_size_kb" in
            ''|*[!0-9]*)
                rm -f "$items"
                return 1
                ;;
        esac
        source_size_kb=$((source_size_kb + item_size_kb))
    done <"$items"

    backup_free_kb="$(df -Pk "$BRORAY_BACKUP" 2>/dev/null | awk 'NR == 2 {print $4}')"
    case "$backup_free_kb" in
        ''|*[!0-9]*)
            rm -f "$items"
            return 1
            ;;
    esac
    required_kb=$((source_size_kb * 2 + 2048))
    [ "$backup_free_kb" -gt "$required_kb" ] || {
        rm -f "$items"
        return 1
    }

    mkdir -p "$staging" || {
        rm -f "$items"
        return 1
    }
    while IFS= read -r item; do
        [ -n "$item" ] || continue
        cp -a "$BRORAY_BASE/$item" "$staging/$item" >>"$BRORAY_LOG" 2>&1 || {
            rm -rf "$staging"
            rm -f "$temporary" "$items"
            return 1
        }
    done <"$items"

    rm -rf \
        "$staging/routes/tmp" \
        "$staging/routes/locks" \
        "$staging/routes/transactions" || {
            rm -rf "$staging"
            rm -f "$temporary" "$items"
            return 1
        }

    tar -czf "$temporary" -C "$staging" . >>"$BRORAY_LOG" 2>&1 || {
        rm -rf "$staging"
        rm -f "$temporary" "$items"
        return 1
    }
    rm -rf "$staging"
    rm -f "$items"

    broray_system_archive_safe "$temporary" || {
        rm -f "$temporary"
        return 1
    }
    backup_size_kb="$(du -k "$temporary" 2>/dev/null | awk 'NR == 1 {print $1}')"
    backup_free_kb="$(df -Pk "$BRORAY_BACKUP" 2>/dev/null | awk 'NR == 2 {print $4}')"
    case "$backup_size_kb:$backup_free_kb" in
        *[!0-9:]*|'')
            rm -f "$temporary"
            return 1
            ;;
    esac
    [ "$backup_free_kb" -gt $((backup_size_kb + 2048)) ] || {
        rm -f "$temporary"
        return 1
    }
    mv -f "$temporary" "$archive" || {
        rm -f "$temporary"
        return 1
    }
    chmod 600 "$archive" 2>/dev/null || true
    printf '%s\n' "$archive"
}

broray_system_reinstall_user_manifest() {
    local output temporary paths item path relative target hash
    output="$1"
    temporary="$output.new.$$"
    paths="$output.paths.$$"

    : >"$paths" || return 1
    broray_system_reinstall_user_items |
        while IFS= read -r item; do
            [ -n "$item" ] || continue
            find "$BRORAY_BASE/$item" \( -type f -o -type l \) 2>/dev/null
        done | sort >"$paths" || {
            rm -f "$paths"
            return 1
        }

    : >"$temporary" || {
        rm -f "$paths"
        return 1
    }
    while IFS= read -r path; do
        [ -n "$path" ] || continue
        relative="${path#"$BRORAY_BASE"/}"
        case "$relative" in
            routes/tmp/*|routes/locks/*|routes/transactions/*) continue ;;
        esac
        if [ -L "$path" ]; then
            target="$(readlink "$path" 2>/dev/null)" || {
                rm -f "$paths" "$temporary"
                return 1
            }
            printf 'L  %s  %s\n' "$relative" "$target" >>"$temporary" || return 1
        else
            hash="$(sha256sum "$path" 2>/dev/null | awk '{print $1}')"
            [ -n "$hash" ] || {
                rm -f "$paths" "$temporary"
                return 1
            }
            printf 'F  %s  %s\n' "$relative" "$hash" >>"$temporary" || return 1
        fi
    done <"$paths"
    rm -f "$paths"
    mv -f "$temporary" "$output"
}

broray_system_reinstall_user_backup_restore() {
    local archive item
    archive="$1"
    broray_system_archive_safe "$archive" || return 1

    broray_system_reinstall_user_items |
        while IFS= read -r item; do
            [ -n "$item" ] || continue
            rm -rf "$BRORAY_BASE/$item" || exit 1
        done || return 1

    tar -xzf "$archive" -C "$BRORAY_BASE" >>"$BRORAY_LOG" 2>&1 || return 1
    mkdir -p \
        "$BRORAY_BASE/routes/locks" \
        "$BRORAY_BASE/routes/tmp" \
        "$BRORAY_BASE/routes/transactions" || return 1
    return 0
}

broray_system_download_exact_package() {
    local staging wanted_version installed_arch feed_base filename expected_sha feed_arch
    local package part actual_sha package_arch
    staging="$1"
    wanted_version="$2"
    installed_arch="$(broray_system_installed_package_architecture)"
    feed_base="$(broray_system_feed_base_url)"
    filename="$(broray_system_feed_value "$wanted_version" Filename)"
    expected_sha="$(broray_system_feed_value "$wanted_version" SHA256sum)"
    feed_arch="$(broray_system_feed_value "$wanted_version" Architecture)"

    case "$wanted_version" in
        ''|*[!0-9A-Za-z._+-]*) return 1 ;;
    esac
    case "$installed_arch" in
        ''|*[!0-9A-Za-z._+-]*) return 1 ;;
    esac
    [ -n "$feed_base" ] && [ -n "$filename" ] && [ -n "$expected_sha" ] || return 1
    case "$filename" in
        /*|*'..'*|*[!0-9A-Za-z._+/-]*) return 1 ;;
    esac
    case "$expected_sha" in
        ''|*[!0-9a-fA-F]*) return 1 ;;
    esac
    [ "${#expected_sha}" -eq 64 ] || return 1
    [ "$feed_arch" = "$installed_arch" ] || [ "$feed_arch" = all ] || return 1

    mkdir -p "$staging" || return 1
    package="$staging/${filename##*/}"
    part="$package.part"
    rm -f "$part" "$package"
    curl -fL --connect-timeout 15 --max-time 180 \
        -o "$part" "${feed_base%/}/$filename" >>"$BRORAY_LOG" 2>&1 || {
            rm -f "$part"
            return 1
        }
    mv -f "$part" "$package" || return 1

    actual_sha="$(sha256sum "$package" | awk '{print $1}')"
    [ "$(printf '%s' "$actual_sha" | tr 'A-F' 'a-f')" = "$(printf '%s' "$expected_sha" | tr 'A-F' 'a-f')" ] || return 1
    [ "$(broray_system_ipk_control_value "$package" Package)" = "$BRORAY_PACKAGE" ] || return 1
    [ "$(broray_system_ipk_control_value "$package" Version)" = "$wanted_version" ] || return 1
    package_arch="$(broray_system_ipk_control_value "$package" Architecture)"
    [ "$package_arch" = "$installed_arch" ] || [ "$package_arch" = all ] || return 1
    printf '%s\n' "$package"
}

broray_system_reinstall_rollback() {
    local rollback_package expected_version full_backup
    rollback_package="$1"
    expected_version="$2"
    full_backup="$3"

    [ -s "$rollback_package" ] && [ -s "$full_backup" ] || return 1
    broray_system_log "Возвращается пакет BROray $expected_version после неудачной переустановки."
    opkg install --force-reinstall "$rollback_package" >>"$BRORAY_LOG" 2>&1 || return 1
    [ "$(broray_system_installed_package_version)" = "$expected_version" ] || return 1
    broray_system_backup_restore "$full_backup" || return 1
    broray_system_restart_services || return 1
    broray_system_health_check || return 1
    rm -f /tmp/broray-opkg-services-before-upgrade /tmp/broray-opkg-existing-backup
    return 0
}

broray_system_worker_reinstall() {
    local operation_id installed_version staging package_file rollback_package
    local full_backup user_backup manifest_before manifest_after free_kb reinstall_ok
    operation_id="$1"
    installed_version=""
    staging=""
    package_file=""
    rollback_package=""
    full_backup=""
    user_backup=""
    manifest_before=""
    manifest_after=""

    broray_system_status_write "$operation_id" reinstall running check 5 \
        'Проверяется возможность переустановки текущей версии.' ''

    installed_version="$(broray_system_installed_package_version)"
    [ -n "$installed_version" ] || {
        broray_system_status_write "$operation_id" reinstall error check 100 \
            'Не удалось определить установленный пакет BROray.' \
            'Переустановка остановлена до изменения файлов.'
        return 1
    }

    broray_system_log 'Обновляются списки пакетов OPKG для поиска точной текущей версии.'
    opkg update >>"$BRORAY_LOG" 2>&1 || {
        broray_system_status_write "$operation_id" reinstall error check 100 \
            'Не удалось обновить индекс пакетов.' \
            'Переустановка остановлена до изменения файлов.'
        return 1
    }

    staging="/tmp/broray-reinstall-$operation_id"
    rm -rf "$staging"
    mkdir -p "$staging" || return 1

    broray_system_status_write "$operation_id" reinstall running download 18 \
        "Загружается текущая версия BROray $installed_version." ''
    package_file="$(broray_system_download_exact_package "$staging" "$installed_version")" || {
        broray_system_status_write "$operation_id" reinstall error download 100 \
            'Точный пакет текущей версии недоступен или не прошёл проверку.' \
            'Файлы BROray не изменялись.'
        rm -rf "$staging"
        return 1
    }

    free_kb="$(df -Pk /opt 2>/dev/null | awk 'NR == 2 {print $4}')"
    case "$free_kb" in
        ''|*[!0-9]*)
            broray_system_status_write "$operation_id" reinstall error verify 100 \
                'Не удалось определить свободное место.' \
                'Файлы BROray не изменялись.'
            rm -rf "$staging"
            return 1
            ;;
    esac
    [ "$free_kb" -gt 15360 ] || {
        broray_system_status_write "$operation_id" reinstall error verify 100 \
            'Недостаточно свободного места для безопасной переустановки.' \
            'Требуется более 15360 КБ.'
        rm -rf "$staging"
        return 1
    }

    broray_system_status_write "$operation_id" reinstall running rollback 30 \
        'Подготавливается пакет автоматического возврата.' ''
    rollback_package="$staging/rollback-${package_file##*/}"
    broray_system_make_rollback_ipk "$package_file" "$rollback_package" || {
        broray_system_status_write "$operation_id" reinstall error rollback 100 \
            'Не удалось подготовить автоматический возврат.' \
            'Файлы BROray не изменялись.'
        rm -rf "$staging"
        return 1
    }

    broray_system_status_write "$operation_id" reinstall running backup 42 \
        'Создаётся полный снимок для аварийного возврата.' ''
    full_backup="$(broray_system_backup_create system-before-reinstall)" || {
        broray_system_status_write "$operation_id" reinstall error backup 100 \
            'Не удалось создать полный резервный снимок.' \
            'Файлы BROray не изменялись.'
        rm -rf "$staging"
        return 1
    }

    broray_system_status_write "$operation_id" reinstall running backup 50 \
        'Отдельно сохраняются пользовательские данные.' ''
    user_backup="$(broray_system_reinstall_user_backup_create user-before-reinstall)" || {
        broray_system_status_write "$operation_id" reinstall error backup 100 \
            'Не удалось сохранить пользовательские данные.' \
            'Файлы BROray не изменялись.'
        rm -rf "$staging"
        return 1
    }
    manifest_before="$staging/user-before.manifest"
    manifest_after="$staging/user-after.manifest"
    broray_system_reinstall_user_manifest "$manifest_before" || {
        broray_system_status_write "$operation_id" reinstall error backup 100 \
            'Не удалось зафиксировать контрольные суммы пользовательских данных.' \
            'Файлы BROray не изменялись.'
        rm -rf "$staging"
        return 1
    }

    {
        printf '%s\n' "$full_backup"
        date '+%s'
    } >/tmp/broray-opkg-existing-backup || {
        broray_system_status_write "$operation_id" reinstall error backup 100 \
            'Не удалось передать резервный снимок установщику.' \
            'Файлы BROray не изменялись.'
        rm -rf "$staging"
        return 1
    }

    broray_system_status_write "$operation_id" reinstall running install 65 \
        "Переустанавливается BROray $installed_version." ''
    BRORAY_OPKG_BACKUP="$full_backup" \
        opkg install --force-reinstall "$package_file" >>"$BRORAY_LOG" 2>&1 || {
            broray_system_status_write "$operation_id" reinstall restoring restore 80 \
                'Переустановка завершилась ошибкой. Возвращается исходное состояние.' ''
            if broray_system_reinstall_rollback "$rollback_package" "$installed_version" "$full_backup"; then
                broray_system_status_write "$operation_id" reinstall error restored 100 \
                    'Исходная версия и состояние восстановлены.' \
                    'OPKG не смог переустановить пакет.'
            else
                broray_system_status_write "$operation_id" reinstall error restore-failed 100 \
                    'Автоматический возврат не завершён.' \
                    "Полный снимок: $full_backup"
            fi
            rm -rf "$staging"
            return 1
        }

    broray_system_status_write "$operation_id" reinstall running user-data 76 \
        'Возвращаются пользовательские настройки и данные.' ''
    broray_system_reinstall_user_backup_restore "$user_backup" || {
        broray_system_status_write "$operation_id" reinstall restoring restore 86 \
            'Пользовательские данные восстановить не удалось. Выполняется возврат.' ''
        if broray_system_reinstall_rollback "$rollback_package" "$installed_version" "$full_backup"; then
            broray_system_status_write "$operation_id" reinstall error restored 100 \
                'Исходная версия и состояние восстановлены.' \
                'Восстановление пользовательских данных завершилось ошибкой.'
        else
            broray_system_status_write "$operation_id" reinstall error restore-failed 100 \
                'Автоматический возврат не завершён.' \
                "Полный снимок: $full_backup"
        fi
        rm -rf "$staging"
        return 1
    }

    broray_system_status_write "$operation_id" reinstall running health 88 \
        'Проверяются версия, службы и сохранность пользовательских данных.' ''
    reinstall_ok=true
    [ "$(broray_system_installed_package_version)" = "$installed_version" ] || reinstall_ok=false
    broray_system_restart_services || reinstall_ok=false
    broray_system_health_check || reinstall_ok=false
    broray_system_reinstall_user_manifest "$manifest_after" || reinstall_ok=false
    cmp -s "$manifest_before" "$manifest_after" || reinstall_ok=false

    if [ "$reinstall_ok" != true ]; then
        broray_system_status_write "$operation_id" reinstall restoring restore 94 \
            'Проверка переустановки не пройдена. Выполняется автоматический возврат.' ''
        if broray_system_reinstall_rollback "$rollback_package" "$installed_version" "$full_backup"; then
            broray_system_status_write "$operation_id" reinstall error restored 100 \
                'Исходная версия и состояние восстановлены.' \
                'Переустановленная система не прошла контрольную проверку.'
        else
            broray_system_status_write "$operation_id" reinstall error restore-failed 100 \
                'Автоматический возврат не завершён.' \
                "Полный снимок: $full_backup"
        fi
        rm -rf "$staging"
        return 1
    fi

    rm -f /tmp/broray-opkg-services-before-upgrade /tmp/broray-opkg-existing-backup
    rm -rf "$staging"
    broray_system_status_write "$operation_id" reinstall success complete 100 \
        "BROray $installed_version переустановлен. Пользовательские данные сохранены." ''
    return 0
}

broray_system_worker_update() {
    operation_id="$1"
    backup=""
    rollback_package=""
    installed_package_version=""

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

    installed_package_version="$(
        broray_system_installed_package_version
    )"
    [ -n "$installed_package_version" ] || {
        broray_system_status_write "$operation_id" update error verify 100 \
            'Не удалось определить установленный пакет BROray.' \
            'Обновление остановлено до изменения файлов.'
        return 1
    }

    staging="/tmp/broray-update-$operation_id"
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
        rm -rf "$staging"
        return 1
    }

    package_file="$(find "$staging" -maxdepth 1 -type f -name 'broray_*.ipk' | sed -n '1p')"
    [ -n "$package_file" ] && [ -f "$package_file" ] || {
        broray_system_status_write "$operation_id" update error verify 100 \
            'Загруженный пакет не найден.' \
            'В каталоге обновления нет файла broray_*.ipk.'
        rm -rf "$staging"
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
        rm -rf "$staging"
        return 1
    }

    if [ -n "$expected_filename" ]; then
        expected_basename="$(basename "$expected_filename")"
        actual_basename="$(basename "$package_file")"
        [ "$actual_basename" = "$expected_basename" ] || {
            broray_system_status_write "$operation_id" update error verify 100 \
                'Имя пакета не совпало с индексом репозитория.' \
                'Установка остановлена до изменения файлов.'
            rm -rf "$staging"
            return 1
        }
    fi

    if [ -n "$expected_arch" ] && [ "$expected_arch" != all ]; then
        opkg print-architecture 2>/dev/null |
            awk -v wanted="$expected_arch" '$2 == wanted {found = 1} END {exit(found ? 0 : 1)}' || {
                broray_system_status_write "$operation_id" update error verify 100 \
                    'Архитектура пакета не поддерживается устройством.' \
                    "Ожидалась архитектура $expected_arch."
                rm -rf "$staging"
                return 1
            }
    fi

    free_kb="$(df -Pk /opt 2>/dev/null | awk 'NR == 2 {print $4}')"
    case "$free_kb" in
        ''|*[!0-9]*)
            broray_system_status_write "$operation_id" update error verify 100 \
                'Не удалось определить свободное место.' \
                'Установка остановлена до создания резервной копии.'
            rm -rf "$staging"
            return 1
            ;;
    esac
    [ "$free_kb" -gt 10240 ] || {
        broray_system_status_write "$operation_id" update error verify 100 \
            'Недостаточно свободного места для безопасного обновления.' \
            'Требуется более 10240 КБ до создания проверяемого снимка.'
        rm -rf "$staging"
        return 1
    }

    broray_system_status_write "$operation_id" update running rollback 45 \
        'Подготавливается пакет возврата к предыдущей версии.' ''
    rollback_package="$(
        broray_system_prepare_rollback_package \
            "$staging" \
            "$installed_package_version"
    )" || {
        broray_system_status_write "$operation_id" update error rollback 100 \
            'Не удалось подготовить безопасный возврат через OPKG.' \
            'Установка обновления не запускалась.'
        rm -rf "$staging"
        return 1
    }
    broray_system_log \
        "Подготовлен пакет возврата к $installed_package_version."

    broray_system_status_write "$operation_id" update running backup 55 \
        'Создаётся резервная копия.' ''
    backup="$(broray_system_backup_create system-before-update)" || {
        broray_system_status_write "$operation_id" update error backup 100 \
            'Не удалось создать резервную копию.' \
            'Установка пакета не запускалась.'
        rm -rf "$staging"
        return 1
    }
    broray_system_log "Создана резервная копия: $backup"

    {
        printf '%s\n' "$backup"
        date '+%s'
    } >/tmp/broray-opkg-existing-backup || {
        broray_system_status_write "$operation_id" update error backup 100 \
            'Не удалось передать резервную копию установщику.' \
            'Установка пакета не запускалась.'
        rm -rf "$staging"
        return 1
    }

    broray_system_status_write "$operation_id" update running install 70 \
        "Устанавливается BROray $available_version." ''
    BRORAY_OPKG_BACKUP="$backup" \
        opkg install "$package_file" >>"$BRORAY_LOG" 2>&1 || {
        broray_system_log 'OPKG сообщил об ошибке. Запускается восстановление.'
        broray_system_status_write "$operation_id" update restoring restore 82 \
            'Обновление завершилось ошибкой. Восстанавливается предыдущая версия.' ''
        if broray_system_rollback_package \
            "$rollback_package" \
            "$installed_package_version" \
            "$backup"
        then
            broray_system_status_write "$operation_id" update error restored 100 \
                'Предыдущая версия и состояние OPKG восстановлены.' \
                'OPKG не смог установить обновление.'
        else
            broray_system_status_write "$operation_id" update error restore-failed 100 \
                'Автоматическое восстановление не завершено.' \
                "Резервная копия: $backup"
        fi
        rm -rf "$staging"
        return 1
    }

    broray_system_status_write "$operation_id" update running health 88 \
        'Проверяется обновлённая установка.' ''
    if [ "$(
        broray_system_installed_package_version
    )" != "$available_version" ] ||
       ! broray_system_health_check
    then
        broray_system_log 'Проверка установки не пройдена. Запускается восстановление.'
        broray_system_status_write "$operation_id" update restoring restore 92 \
            'Проверка не пройдена. Восстанавливается предыдущая версия.' ''
        if broray_system_rollback_package \
            "$rollback_package" \
            "$installed_package_version" \
            "$backup"
        then
            broray_system_status_write "$operation_id" update error restored 100 \
                'Предыдущая версия и состояние OPKG восстановлены.' \
                'Обновлённая установка не прошла проверку.'
        else
            broray_system_status_write "$operation_id" update error restore-failed 100 \
                'Автоматическое восстановление не завершено.' \
                "Резервная копия: $backup"
        fi
        rm -rf "$staging"
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
    broray_system_restart_services && broray_system_health_check || {
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

    global_lock_rc=0
    broray_system_global_lock_acquire "$operation" || global_lock_rc=$?
    case "$global_lock_rc" in
        0) ;;
        2)
            broray_system_error_json OPERATION_BUSY 'Сейчас выполняется другая конфликтующая операция.'
            return 1
            ;;
        *)
            broray_system_error_json GLOBAL_LOCK_FAILED 'Не удалось установить общую блокировку операции.'
            return 1
            ;;
    esac

    broray_system_recover_stale_lock || {
        broray_system_global_lock_release
        broray_system_error_json OPERATION_BUSY 'Сейчас выполняется другая операция.'
        return 1
    }

    mkdir "$BRORAY_LOCK" 2>/dev/null || {
        broray_system_global_lock_release
        broray_system_error_json OPERATION_BUSY 'Сейчас выполняется другая операция.'
        return 1
    }

    operation_id="${operation}-$(date -u '+%Y%m%d%H%M%S')-$$"
    workers="$(broray_system_copy_worker "$operation_id")" || {
        rm -rf "$BRORAY_LOCK"
        broray_system_global_lock_release
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
    broray_system_global_lock_transfer "$worker_pid" "$operation_id" || {
        kill "$worker_pid" 2>/dev/null || true
        rm -rf "$BRORAY_LOCK"
        broray_system_global_lock_release
        broray_system_error_json GLOBAL_LOCK_TRANSFER_FAILED 'Не удалось передать общую блокировку фоновой операции.'
        return 1
    }

    jq -nc \
        --arg operationId "$operation_id" \
        --arg operation "$operation" \
        '{ok:true,accepted:true,operationId:$operationId,operation:$operation}'
}

broray_system_worker_finish() {
    worker_bin="$1"
    worker_lib="$2"
    rm -rf "$BRORAY_LOCK"
    broray_system_global_lock_release
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
        reinstall-start)
            broray_system_start_worker reinstall
            ;;
        restore-start)
            broray_system_start_worker restore
            ;;
        uninstall-start)
            broray_system_uninstall_start "${1:-}" "${2:-}"
            ;;
        worker-update|worker-reinstall|worker-restore|worker-uninstall)
            operation_id="${1:-}"
            mode="${2:-}"
            worker_bin="$0"
            worker_lib="${BRORAY_SYSTEM_LIB:-$BRORAY_BASE/lib/broray-page.sh}"
            result=0
            case "$command_name" in
                worker-update)
                    broray_system_worker_update "$operation_id" || result=$?
                    ;;
                worker-reinstall)
                    broray_system_worker_reinstall "$operation_id" || result=$?
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
            printf '%s\n' 'Использование: broray-system {info|status|update-check|update-start|reinstall-start|restore-start|uninstall-start}' >&2
            return 2
            ;;
    esac
}
