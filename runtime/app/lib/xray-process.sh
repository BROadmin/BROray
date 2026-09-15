#!/opt/bin/ash

# Shared by status, CLI lifecycle and S24. Never mistake a validator for runtime.
broray_xray_runtime_identity() {
    local process_pid expected_binary expected_config actual_binary started finished
    process_pid="$1"
    case "$process_pid" in ''|*[!0-9]*|0|1) return 1 ;; esac
    expected_binary="$(readlink -f "${BRORAY_XRAY_BINARY:-/opt/broray/runtime/xray}" 2>/dev/null)" || return 1
    expected_config="${BRORAY_XRAY_CONFIG:-/opt/broray/config/config.json}"
    [ -n "$expected_binary" ] && [ -r "/proc/$process_pid/stat" ] || return 1
    started="$(sed 's/.*) //' "/proc/$process_pid/stat" 2>/dev/null | awk '$1!="Z" {print $20}')"
    [ -n "$started" ] || return 1
    actual_binary="$(readlink "/proc/$process_pid/exe" 2>/dev/null)" || return 1
    [ "$actual_binary" = "$expected_binary" ] || return 1
    [ -r "/proc/$process_pid/cmdline" ] || return 1
    tr '\000' '\n' 2>/dev/null <"/proc/$process_pid/cmdline" |
        awk -v expected="$expected_config" '
            NR==1 {next}
            NR==2 {if ($0!="run") bad=1; next}
            $0=="-test" || $0=="--test" || /^--?test=/ {bad=1}
            /^--?confdir($|=)/ {bad=1}
            value {if ($0!=expected) bad=1; value=0; next}
            $0=="-c" || $0=="-config" || $0=="--config" {count++; value=1; next}
            /^--?(c|config)=/ {count++; sub(/^[^=]*=/, ""); if ($0!=expected) bad=1}
            END {exit (NR<4 || bad || value || count!=1)}
        ' || return 1
    finished="$(sed 's/.*) //' "/proc/$process_pid/stat" 2>/dev/null | awk '$1!="Z" {print $20}')"
    [ "$started" = "$finished" ] && kill -0 "$process_pid" 2>/dev/null || return 1
    printf '%s\n' "$started"
}

broray_xray_runtime_pids() {
    local candidates process_pid found
    found=false
    if command -v pidof >/dev/null 2>&1; then
        candidates="$(command pidof xray 2>/dev/null || true)"
    else
        candidates="$(printf '%s\n' /proc/[0-9]* | sed 's|.*/||')"
    fi
    for process_pid in $candidates; do
        if broray_xray_runtime_identity "$process_pid" >/dev/null; then
            printf '%s\n' "$process_pid"
            found=true
        fi
    done
    [ "$found" = true ]
}

broray_xray_runtime_pid() {
    local candidates
    candidates="$(broray_xray_runtime_pids)" || return 1
    set -- $candidates
    [ "$#" -gt 0 ] || return 1
    printf '%s\n' "$1"
}

broray_xray_runtime_signal() {
    local signal process_pid candidates identity result
    signal="$1"
    case "$signal" in TERM|9|SIGHUP) ;; *) return 1 ;; esac
    candidates="$(broray_xray_runtime_pids)" || return 1
    result=0
    for process_pid in $candidates; do
        identity="$(broray_xray_runtime_identity "$process_pid")" || continue
        [ "$identity" = "$(broray_xray_runtime_identity "$process_pid")" ] || continue
        kill "-$signal" "$process_pid" 2>/dev/null || result=1
    done
    return "$result"
}
