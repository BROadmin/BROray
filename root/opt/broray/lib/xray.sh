#!/opt/bin/ash

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_RUN_DIR="${BRORAY_RUN_DIR:-$BRORAY_ROOT/run}"
BRORAY_SETTINGS_FILE="${BRORAY_SETTINGS_FILE:-$BRORAY_ROOT/config/system/settings.json}"

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
        /opt/broray/bin/xray \
        /opt/broray/xray/xray
    do
        if [ -f "$candidate" ]; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done

    printf '%s\n' "/opt/bin/xray"
}

broray_xray_pid() {
    pid=""

    if command -v pidof >/dev/null 2>&1; then
        pid="$(pidof xray 2>/dev/null | awk '{print $1}')"
    fi

    if [ -n "$pid" ] &&
       [ -d "/proc/$pid" ] &&
       [ -r "/proc/$pid/comm" ] &&
       [ "$(cat "/proc/$pid/comm" 2>/dev/null)" = "xray" ]; then
        printf '%s\n' "$pid"
        return 0
    fi

    for proc_dir in /proc/[0-9]*; do
        [ -d "$proc_dir" ] || continue
        [ -r "$proc_dir/comm" ] || continue

        if [ "$(cat "$proc_dir/comm" 2>/dev/null)" = "xray" ]; then
            printf '%s\n' "${proc_dir#/proc/}"
            return 0
        fi
    done

    return 1
}

broray_xray_process_config_path() {
    pid="$1"

    [ -n "$pid" ] || return 1
    [ -r "/proc/$pid/cmdline" ] || return 1

    command_line="$(
        tr '\000' '\n' < "/proc/$pid/cmdline" 2>/dev/null
    )"

    previous=""

    printf '%s\n' "$command_line" |
    while IFS= read -r argument; do
        case "$previous" in
            -config|-c)
                printf '%s\n' "$argument"
                exit 0
                ;;
        esac

        case "$argument" in
            -config=*|--config=*)
                printf '%s\n' "${argument#*=}"
                exit 0
                ;;
        esac

        previous="$argument"
    done
}

broray_xray_config_path() {
    configured_path=""
    process_path=""
    pid=""

    if pid="$(broray_xray_pid 2>/dev/null)"; then
        process_path="$(
            broray_xray_process_config_path "$pid" 2>/dev/null |
                head -n 1
        )"

        if [ -n "$process_path" ] && [ -f "$process_path" ]; then
            printf '%s\n' "$process_path"
            return 0
        fi
    fi

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

        check_file="/tmp/broray-xray-check-$$.log"

        if broray_xray_check_config \
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

        rm -f "$check_file"
    elif [ -z "$last_error" ]; then
        last_error="Активная конфигурация Xray не найдена."
    fi

    free_kb="$(broray_xray_free_kb 2>/dev/null || true)"

    case "$free_kb" in
        ''|*[!0-9]*)
            free_kb=0
            ;;
    esac

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
