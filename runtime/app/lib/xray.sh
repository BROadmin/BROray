#!/opt/bin/ash

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_RUN_DIR="${BRORAY_RUN_DIR:-$BRORAY_ROOT/run}"
BRORAY_SETTINGS_FILE="${BRORAY_SETTINGS_FILE:-$BRORAY_ROOT/config/system/settings.json}"
BRORAY_STATUS_LIBRARY="${BRORAY_STATUS_LIBRARY:-$BRORAY_ROOT/lib/status-contract.sh}"

[ -r "$BRORAY_STATUS_LIBRARY" ] && . "$BRORAY_STATUS_LIBRARY"
. "$BRORAY_ROOT/lib/xray-process.sh" || return 1

broray_xray_now() {
    date '+%Y-%m-%dT%H:%M:%S%z'
}

broray_xray_bool() {
    case "$1" in
        1|true|yes|on)
            printf '%s\n' true
            ;;
        *)
            printf '%s\n' false
            ;;
    esac
}

broray_xray_binary_path() {
    configured_path=""

    if [ -f "$BRORAY_SETTINGS_FILE" ]; then
        configured_path="$(
            jq -r '
                .xray.binaryPath //
                .xrayBinary //
                .paths.xray //
                empty
            ' "$BRORAY_SETTINGS_FILE" 2>/dev/null
        )"
    fi

    if [ -n "$configured_path" ]; then
        printf '%s\n' "$configured_path"
        return 0
    fi

    for candidate in \
        /opt/bin/xray \
        /opt/broray/runtime/xray \
        /opt/broray/xray/xray
    do
        if [ -f "$candidate" ]; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done

    printf '%s\n' "/opt/bin/xray"
}

broray_xray_pid() (
    BRORAY_XRAY_BINARY="$(broray_xray_binary_path)"
    BRORAY_XRAY_CONFIG="$(broray_xray_config_path)"
    broray_xray_runtime_pid
)

broray_xray_config_path() {
    configured_path=""

    if [ -f "$BRORAY_SETTINGS_FILE" ]; then
        configured_path="$(
            jq -r '
                .xray.configPath //
                .xrayConfig //
                .paths.xrayConfig //
                empty
            ' "$BRORAY_SETTINGS_FILE" 2>/dev/null
        )"
    fi

    if [ -n "$configured_path" ]; then
        printf '%s\n' "$configured_path"
        return 0
    fi

    for candidate in \
        "$BRORAY_RUN_DIR/config.json" \
        "$BRORAY_RUN_DIR/xray-config.json" \
        "$BRORAY_ROOT/config/xray/config.json" \
        "$BRORAY_ROOT/config/config.json" \
        /opt/etc/xray/config.json
    do
        if [ -f "$candidate" ]; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done

    printf '%s\n' "$BRORAY_RUN_DIR/config.json"
}

broray_xray_version_output() {
    binary_path="$1"

    [ -x "$binary_path" ] || return 1

    "$binary_path" version 2>&1
}

broray_xray_version() {
    binary_path="$1"

    broray_xray_version_output "$binary_path" |
        awk '
            NR == 1 {
                for (i = 1; i <= NF; i++) {
                    if ($i ~ /^[vV]?[0-9]+([.][0-9A-Za-z_-]+)+$/) {
                        value = $i
                        sub(/^[vV]/, "", value)
                        print value
                        exit
                    }
                }

                if (NF >= 2) {
                    value = $2
                    sub(/^[vV]/, "", value)
                    print value
                    exit
                }
            }
        '
}

broray_xray_architecture() {
    binary_path="$1"
    architecture=""

    architecture="$(
        broray_xray_version_output "$binary_path" 2>/dev/null |
            sed -n '
                s#.*linux/\([A-Za-z0-9_-][A-Za-z0-9_-]*\).*#\1#p
            ' |
            head -n 1
    )"

    if [ -n "$architecture" ]; then
        printf '%s\n' "$architecture"
        return 0
    fi

    case "$(uname -m 2>/dev/null)" in
        aarch64|arm64)
            printf '%s\n' arm64
            ;;
        armv7l|armv7)
            printf '%s\n' arm
            ;;
        mipsel|mipsle)
            printf '%s\n' mipsle
            ;;
        mips)
            printf '%s\n' mips
            ;;
        x86_64|amd64)
            printf '%s\n' amd64
            ;;
        i386|i486|i586|i686)
            printf '%s\n' 386
            ;;
        *)
            uname -m 2>/dev/null || printf '%s\n' unknown
            ;;
    esac
}

broray_xray_file_size() {
    file_path="$1"

    if [ ! -f "$file_path" ]; then
        printf '%s\n' 0
        return 0
    fi

    wc -c < "$file_path" |
        tr -d '[:space:]'
}

broray_xray_sha256() {
    file_path="$1"

    [ -f "$file_path" ] || return 1

    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$file_path" |
            awk '{print $1}'
        return $?
    fi

    if command -v openssl >/dev/null 2>&1; then
        openssl dgst -sha256 "$file_path" 2>/dev/null |
            awk '{print $NF}'
        return $?
    fi

    return 1
}

broray_xray_socks_address() {
    value=""

    if [ -f "$BRORAY_SETTINGS_FILE" ]; then
        value="$(
            jq -r '
                .xray.socksAddress //
                .listenAddress //
                .socksAddress //
                empty
            ' "$BRORAY_SETTINGS_FILE" 2>/dev/null
        )"
    fi

    if [ -z "$value" ]; then
        value="127.0.0.1"
    fi

    printf '%s\n' "$value"
}

broray_xray_socks_port() {
    value=""

    if [ -f "$BRORAY_SETTINGS_FILE" ]; then
        value="$(
            jq -r '
                .xray.socksPort //
                .socksPort //
                empty
            ' "$BRORAY_SETTINGS_FILE" 2>/dev/null
        )"
    fi

    case "$value" in
        ''|*[!0-9]*)
            value="2080"
            ;;
    esac

    printf '%s\n' "$value"
}

broray_xray_socks_active() {
    socks_address="$1"
    socks_port="$2"

    case "$socks_port" in
        ''|*[!0-9]*) return 1 ;;
    esac

    if [ -n "${BRORAY_XRAY_NETSTAT_FILE:-}" ] &&
       [ -r "$BRORAY_XRAY_NETSTAT_FILE" ]; then
        cat "$BRORAY_XRAY_NETSTAT_FILE"
    elif command -v netstat >/dev/null 2>&1; then
        netstat -lnt 2>/dev/null
    else
        return 1
    fi |
        awk \
            -v endpoint="$socks_address:$socks_port" \
            -v wildcard4="0.0.0.0:$socks_port" \
            -v wildcard6=":::$socks_port" \
            -v wildcard6b="[::]:$socks_port" '
            {
                local_address = $4
                state = $6
                address_match = 0
                if (local_address == endpoint) address_match = 1
                if (local_address == wildcard4) address_match = 1
                if (local_address == wildcard6) address_match = 1
                if (local_address == wildcard6b) address_match = 1
                if (state == "LISTEN" && address_match == 1) found = 1
            }
            END { exit found ? 0 : 1 }
        '
}

broray_xray_free_kb() {
    df -k /opt 2>/dev/null |
        awk '
            NR > 1 {
                print $4
                exit
            }
        '
}

broray_xray_check_config() {
    binary_path="$1"
    config_path="$2"
    output_file="$3"

    : > "$output_file"

    if [ ! -x "$binary_path" ]; then
        printf '%s\n' "Бинарный файл Xray отсутствует или не исполняется." \
            > "$output_file"
        return 1
    fi

    if [ ! -f "$config_path" ]; then
        printf '%s\n' "Активная конфигурация Xray не найдена." \
            > "$output_file"
        return 1
    fi

    "$binary_path" run -test -config "$config_path" \
        > "$output_file" 2>&1
}

broray_xray_status_json() {
    binary_path="$(broray_xray_binary_path)"
    config_path="$(broray_xray_config_path)"

    installed=false
    executable=false
    running=false
    config_exists=false
    config_valid=false
    pid=""
    version=""
    architecture=""
    last_error=""
    config_check_output=""
    binary_size=0
    config_size=0
    binary_sha256=""
    config_sha256=""
    free_kb=0
    socks_address="$(broray_xray_socks_address)"
    socks_port="$(broray_xray_socks_port)"
    socks_active=false
    updated_at="$(broray_xray_now)"

    if [ -f "$binary_path" ]; then
        installed=true
        binary_size="$(broray_xray_file_size "$binary_path")"
        binary_sha256="$(
            broray_xray_sha256 "$binary_path" 2>/dev/null || true
        )"
    else
        last_error="Бинарный файл Xray не найден."
    fi

    if [ -x "$binary_path" ]; then
        executable=true
        version="$(
            broray_xray_version "$binary_path" 2>/dev/null || true
        )"
        architecture="$(
            broray_xray_architecture "$binary_path" 2>/dev/null || true
        )"

        if [ -z "$version" ] && [ -z "$last_error" ]; then
            last_error="Не удалось определить версию Xray."
        fi
    elif [ "$installed" = true ] && [ -z "$last_error" ]; then
        last_error="Бинарный файл Xray не является исполняемым."
    fi

    if pid="$(broray_xray_pid 2>/dev/null)"; then
        running=true
    else
        pid=""
    fi

    if [ -f "$config_path" ]; then
        config_exists=true
        config_size="$(broray_xray_file_size "$config_path")"
        config_sha256="$(
            broray_xray_sha256 "$config_path" 2>/dev/null || true
        )"

        check_file=""
        if [ -d "$BRORAY_ROOT/tmp" ] && [ ! -L "$BRORAY_ROOT/tmp" ] &&
           command -v mktemp >/dev/null 2>&1; then
            check_file="$(mktemp "$BRORAY_ROOT/tmp/broray-xray-check.XXXXXXXXXX" 2>/dev/null || true)"
        fi

        if [ -n "$check_file" ] && [ -f "$check_file" ] && [ ! -L "$check_file" ] &&
           broray_xray_check_config \
               "$binary_path" \
               "$config_path" \
               "$check_file"
        then
            config_valid=true
        else
            config_valid=false
            config_check_output="$(
                tail -n 20 "$check_file" 2>/dev/null || true
            )"

            if [ -z "$last_error" ]; then
                last_error="Конфигурация Xray не прошла проверку."
            fi
        fi

        [ -z "$check_file" ] || rm -f "$check_file"
    elif [ -z "$last_error" ]; then
        last_error="Активная конфигурация Xray не найдена."
    fi

    if [ "$running" = true ] &&
       broray_xray_socks_active "$socks_address" "$socks_port"
    then
        socks_active=true
    fi

    free_kb="$(broray_xray_free_kb 2>/dev/null || true)"

    case "$free_kb" in
        ''|*[!0-9]*)
            free_kb=0
            ;;
    esac

    health_reasons="$({
        [ "$installed" = true ] || broray_status_reason XRAY_BINARY_MISSING "Бинарный файл Xray не найден."
        [ "$executable" = true ] || broray_status_reason XRAY_NOT_EXECUTABLE "Бинарный файл Xray нельзя запустить."
        [ "$config_exists" = true ] || broray_status_reason XRAY_CONFIG_MISSING "Активная конфигурация Xray не найдена."
        [ "$config_valid" = true ] || broray_status_reason XRAY_CONFIG_INVALID "Конфигурация Xray не прошла проверку."
        [ "$running" = true ] || broray_status_reason XRAY_PROCESS_STOPPED "Процесс Xray остановлен."
        [ "$socks_active" = true ] || broray_status_reason XRAY_SOCKS_INACTIVE "Локальный SOCKS-интерфейс не слушает настроенный адрес и порт."
    } | jq -sc '.')"

    if [ "$installed" != true ] ||
       [ "$executable" != true ] ||
       [ "$config_exists" != true ] ||
       [ "$config_valid" != true ]
    then
        health_severity=error
    elif [ "$running" != true ] || [ "$socks_active" != true ]; then
        health_severity=warning
    else
        health_severity=ok
    fi

    if [ "$installed" = true ] &&
       [ "$executable" = true ] &&
       [ "$config_exists" = true ] &&
       [ "$config_valid" = true ] &&
       [ "$running" = true ] &&
       [ "$socks_active" = true ]
    then
        health_operational=true
        health_consistent=true
        health_action_required=false
    else
        health_operational=false
        health_consistent="$config_valid"
        health_action_required=true
    fi

    health_facts="$(jq -nc \
        --argjson processRunning "$running" \
        --argjson configValid "$config_valid" \
        --argjson socksActive "$socks_active" \
        --arg address "$socks_address" \
        --argjson port "$socks_port" \
        '{processRunning:$processRunning,configValid:$configValid,socksActive:$socksActive,socksAddress:$address,socksPort:$port}')"

    health_json="$(broray_status_contract \
        xray available "$health_severity" \
        "$health_operational" "$health_consistent" "$health_action_required" \
        fresh "$updated_at" "$health_reasons" "$health_facts" null)"

    jq -n \
        --argjson installed "$installed" \
        --argjson executable "$executable" \
        --argjson running "$running" \
        --arg pid "$pid" \
        --arg version "$version" \
        --arg architecture "$architecture" \
        --arg deviceArchitecture "$(uname -m 2>/dev/null || true)" \
        --arg binaryPath "$binary_path" \
        --argjson binarySizeBytes "$binary_size" \
        --arg binarySha256 "$binary_sha256" \
        --arg configPath "$config_path" \
        --argjson configExists "$config_exists" \
        --argjson configSizeBytes "$config_size" \
        --arg configSha256 "$config_sha256" \
        --argjson configValid "$config_valid" \
        --arg configCheckOutput "$config_check_output" \
        --arg socksAddress "$socks_address" \
        --argjson socksPort "$socks_port" \
        --argjson socksActive "$socks_active" \
        --argjson health "$health_json" \
        --argjson storageFreeKb "$free_kb" \
        --arg lastError "$last_error" \
        --arg updatedAt "$updated_at" '
        {
            success: true,
            data: {
                installed: $installed,
                executable: $executable,
                running: $running,
                pid: (
                    if $pid == ""
                    then null
                    else ($pid | tonumber)
                    end
                ),
                version: (
                    if $version == ""
                    then null
                    else $version
                    end
                ),
                architecture: (
                    if $architecture == ""
                    then null
                    else $architecture
                    end
                ),
                deviceArchitecture: (
                    if $deviceArchitecture == ""
                    then null
                    else $deviceArchitecture
                    end
                ),
                binaryPath: $binaryPath,
                binarySizeBytes: $binarySizeBytes,
                binarySha256: (
                    if $binarySha256 == ""
                    then null
                    else $binarySha256
                    end
                ),
                configPath: $configPath,
                configExists: $configExists,
                configSizeBytes: $configSizeBytes,
                configSha256: (
                    if $configSha256 == ""
                    then null
                    else $configSha256
                    end
                ),
                configValid: $configValid,
                configCheckOutput: (
                    if $configCheckOutput == ""
                    then null
                    else $configCheckOutput
                    end
                ),
                socksAddress: $socksAddress,
                socksPort: $socksPort,
                socksActive: $socksActive,
                socks: {
                    address: $socksAddress,
                    listen: $socksAddress,
                    port: $socksPort,
                    active: $socksActive
                },
                health: $health,
                storagePath: "/opt",
                storageFreeKb: $storageFreeKb,
                updateAvailable: false,
                lastError: (
                    if $lastError == ""
                    then null
                    else $lastError
                    end
                ),
                updatedAt: $updatedAt
            },
            error: null
        }
    '
}

BRORAY_XRAY_STATUS_CACHE_FILE="${BRORAY_XRAY_STATUS_CACHE_FILE:-$BRORAY_ROOT/run/xray-status-cache.json}"
BRORAY_XRAY_STATUS_CACHE_SECONDS="${BRORAY_XRAY_STATUS_CACHE_SECONDS:-15}"

broray_xray_status_json_cached() {
    xray_cache_file="$BRORAY_XRAY_STATUS_CACHE_FILE"
    xray_cache_now="$(date '+%s')"
    xray_cache_epoch=0
    xray_cache_age=0
    xray_current_pid="$(broray_xray_pid 2>/dev/null || true)"

    if [ -s "$xray_cache_file" ] && [ ! -L "$xray_cache_file" ] &&
       jq -e '.success == true and (.data | type) == "object"' "$xray_cache_file" >/dev/null 2>&1
    then
        xray_cache_epoch="$(
            find -P "$xray_cache_file" -maxdepth 0 -printf '%T@\n' 2>/dev/null |
                awk -F. 'NR==1{print $1;exit}'
        )"
        case "$xray_cache_epoch" in ''|*[!0-9]*) xray_cache_epoch=0 ;; esac
        if [ "$xray_cache_epoch" -gt 0 ]; then
            xray_cache_age=$((xray_cache_now - xray_cache_epoch))
            [ "$xray_cache_age" -ge 0 ] || xray_cache_age=0
        fi
        xray_cached_pid="$(jq -r '.data.pid // empty' "$xray_cache_file" 2>/dev/null)"
        if [ "$xray_cache_age" -lt "$BRORAY_XRAY_STATUS_CACHE_SECONDS" ] &&
           [ "$xray_cached_pid" = "$xray_current_pid" ]
        then
            cat "$xray_cache_file"
            return 0
        fi
    fi

    mkdir -p "${xray_cache_file%/*}" || return 1
    [ ! -L "$xray_cache_file" ] || {
        broray_xray_status_json
        return $?
    }
    xray_cache_tmp="$xray_cache_file.new.$$"
    rm -f "$xray_cache_tmp"
    broray_xray_status_json >"$xray_cache_tmp" || {
        rm -f "$xray_cache_tmp"
        return 1
    }
    chmod 0600 "$xray_cache_tmp" 2>/dev/null || true
    mv -f "$xray_cache_tmp" "$xray_cache_file" || {
        rm -f "$xray_cache_tmp"
        return 1
    }
    cat "$xray_cache_file"
}
