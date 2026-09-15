#!/opt/bin/ash

# Local fail-closed bridge to KeeneticOS' native authentication SCGI socket.
# The normal Keenetic nginx endpoint on 127.0.0.1:79 is preferred.  A private
# loopback-only nginx instance is started only when the firmware web server is
# unavailable (for example, because an unrelated Entware service owns port 80).

BRORAY_NATIVE_AUTH_BASE="${BRORAY_NATIVE_AUTH_BASE:-${BRORAY_BASE:-/opt/broray}}"
BRORAY_NATIVE_AUTH_DIR="${BRORAY_NATIVE_AUTH_DIR:-$BRORAY_NATIVE_AUTH_BASE/run/web-new/native-auth}"
BRORAY_NATIVE_AUTH_SOURCE="${BRORAY_NATIVE_AUTH_SOURCE:-/usr/sbin/nginx}"
BRORAY_NATIVE_AUTH_RUNTIME="${BRORAY_NATIVE_AUTH_RUNTIME:-$BRORAY_NATIVE_AUTH_DIR/broray-ndm-auth-nginx}"
BRORAY_NATIVE_AUTH_CONFIG="${BRORAY_NATIVE_AUTH_CONFIG:-$BRORAY_NATIVE_AUTH_DIR/nginx.conf}"
BRORAY_NATIVE_AUTH_PIDFILE="${BRORAY_NATIVE_AUTH_PIDFILE:-$BRORAY_NATIVE_AUTH_DIR/nginx.pid}"
BRORAY_NATIVE_AUTH_LOG="${BRORAY_NATIVE_AUTH_LOG:-$BRORAY_NATIVE_AUTH_DIR/nginx.log}"
BRORAY_NATIVE_AUTH_SYSTEM_URL="${BRORAY_NATIVE_AUTH_SYSTEM_URL:-http://127.0.0.1:79}"
BRORAY_NATIVE_AUTH_SIDECAR_URL="${BRORAY_NATIVE_AUTH_SIDECAR_URL:-http://127.0.0.1:18079}"
BRORAY_NATIVE_AUTH_PROC_ROOT="${BRORAY_NATIVE_AUTH_PROC_ROOT:-/proc}"

broray_native_auth_pid_read() {
    [ -f "$BRORAY_NATIVE_AUTH_PIDFILE" ] && [ ! -L "$BRORAY_NATIVE_AUTH_PIDFILE" ] || return 1
    awk '
      NR==1 && $0~/^[0-9]+$/ && $0>1 {pid=$0; next}
      {bad=1}
      END{if(!bad && pid!="") print pid; else exit 1}
    ' "$BRORAY_NATIVE_AUTH_PIDFILE"
}

broray_native_auth_process_once() {
    native_pid="$1"
    case "$native_pid" in ''|*[!0-9]*) return 1 ;; esac
    [ "$native_pid" -gt 1 ] || return 1
    kill -0 "$native_pid" 2>/dev/null || return 1
    [ "$(readlink "$BRORAY_NATIVE_AUTH_PROC_ROOT/$native_pid/exe" 2>/dev/null)" = "$BRORAY_NATIVE_AUTH_RUNTIME" ] || return 1
    tr '\000' '\n' <"$BRORAY_NATIVE_AUTH_PROC_ROOT/$native_pid/cmdline" 2>/dev/null |
        grep -F "$BRORAY_NATIVE_AUTH_CONFIG" >/dev/null 2>&1 || return 1
    kill -0 "$native_pid" 2>/dev/null
}

broray_native_auth_process_twice() {
    broray_native_auth_process_once "$1" && broray_native_auth_process_once "$1"
}

broray_native_auth_runtime_in_use() {
    for native_exe in "$BRORAY_NATIVE_AUTH_PROC_ROOT"/[0-9]*/exe; do
        [ -L "$native_exe" ] || continue
        [ "$(readlink "$native_exe" 2>/dev/null)" = "$BRORAY_NATIVE_AUTH_RUNTIME" ] && return 0
    done
    return 1
}

broray_native_auth_probe() {
    native_url="$1"
    native_headers="$(mktemp /tmp/broray-native-auth-probe.XXXXXX)" || return 1
    case "$native_headers" in /tmp/broray-native-auth-probe.*) ;; *) rm -f "$native_headers"; return 1 ;; esac
    chmod 600 "$native_headers" || { rm -f "$native_headers"; return 1; }
    broray_keenetic_curl --silent --show-error --connect-timeout 3 --max-time 5 \
        --dump-header "$native_headers" --output /dev/null "$native_url/auth" \
        >/dev/null 2>&1 || true
    native_probe_ok=false
    if [ "$(grep -Eic '^HTTP/[0-9.]+ 401([[:space:]]|$)' "$native_headers" 2>/dev/null)" -eq 1 ] &&
       [ "$(grep -Eic '^X-NDM-Realm:[[:space:]]*[^[:space:]].*' "$native_headers" 2>/dev/null)" -eq 1 ] &&
       [ "$(grep -Eic '^X-NDM-Challenge:[[:space:]]*[0-9A-Za-z_-]+[[:space:]\r]*$' "$native_headers" 2>/dev/null)" -eq 1 ]
    then
        native_probe_ok=true
    fi
    rm -f "$native_headers"
    [ "$native_probe_ok" = true ]
}

broray_native_auth_prepare() {
    [ -f "$BRORAY_NATIVE_AUTH_SOURCE" ] && [ ! -L "$BRORAY_NATIVE_AUTH_SOURCE" ] &&
        [ -x "$BRORAY_NATIVE_AUTH_SOURCE" ] || return 1
    [ -f /etc/nginx/scgi_params ] && [ ! -L /etc/nginx/scgi_params ] || return 1
    [ -S /var/run/ndm.auth.socket ] && [ ! -L /var/run/ndm.auth.socket ] || return 1
    if [ -e "$BRORAY_NATIVE_AUTH_DIR" ] || [ -L "$BRORAY_NATIVE_AUTH_DIR" ]; then
        [ -d "$BRORAY_NATIVE_AUTH_DIR" ] && [ ! -L "$BRORAY_NATIVE_AUTH_DIR" ] || return 1
    else
        mkdir -p "$BRORAY_NATIVE_AUTH_DIR" || return 1
    fi
    chmod 700 "$BRORAY_NATIVE_AUTH_DIR" || return 1

    native_runtime_part="$BRORAY_NATIVE_AUTH_RUNTIME.new.$$"
    [ ! -e "$native_runtime_part" ] && [ ! -L "$native_runtime_part" ] || return 1
    cp "$BRORAY_NATIVE_AUTH_SOURCE" "$native_runtime_part" || {
        rm -f "$native_runtime_part"
        return 1
    }
    chmod 700 "$native_runtime_part" || {
        rm -f "$native_runtime_part"
        return 1
    }
    cmp -s "$BRORAY_NATIVE_AUTH_SOURCE" "$native_runtime_part" || {
        rm -f "$native_runtime_part"
        return 1
    }
    mv -f "$native_runtime_part" "$BRORAY_NATIVE_AUTH_RUNTIME" || return 1

    native_config_part="$BRORAY_NATIVE_AUTH_CONFIG.new.$$"
    [ ! -e "$native_config_part" ] && [ ! -L "$native_config_part" ] || return 1
    cat >"$native_config_part" <<EOF_NATIVE_AUTH
user nobody nobody;
worker_processes 1;
pid $BRORAY_NATIVE_AUTH_PIDFILE;
error_log $BRORAY_NATIVE_AUTH_LOG notice;
events { worker_connections 32; }
http {
    access_log off;
    server_tokens off;
    client_max_body_size 8k;
    upstream broray_ndm_auth { server unix:/var/run/ndm.auth.socket; }
    server {
        listen 127.0.0.1:18079;
        location = /auth {
            scgi_pass broray_ndm_auth;
            include /etc/nginx/scgi_params;
            scgi_param SERVER_ADDR 127.0.0.1;
            scgi_param REMOTE_ADDR 127.0.0.1;
            scgi_param REMOTE_IDENT BROray-auth;
            scgi_param SCGI_EZCFG 0;
        }
        location / { return 404; }
    }
}
EOF_NATIVE_AUTH
    chmod 600 "$native_config_part" || {
        rm -f "$native_config_part"
        return 1
    }
    "$BRORAY_NATIVE_AUTH_RUNTIME" -t -p / -c "$native_config_part" >/dev/null 2>&1 || {
        rm -f "$native_config_part"
        return 1
    }
    mv -f "$native_config_part" "$BRORAY_NATIVE_AUTH_CONFIG"
}

broray_native_auth_stop() {
    [ -e "$BRORAY_NATIVE_AUTH_DIR" ] || [ -L "$BRORAY_NATIVE_AUTH_DIR" ] || return 0
    [ -d "$BRORAY_NATIVE_AUTH_DIR" ] && [ ! -L "$BRORAY_NATIVE_AUTH_DIR" ] || return 1
    if [ -e "$BRORAY_NATIVE_AUTH_PIDFILE" ] || [ -L "$BRORAY_NATIVE_AUTH_PIDFILE" ]; then
        native_pid="$(broray_native_auth_pid_read 2>/dev/null)" || return 1
        [ -n "$native_pid" ] || return 1
    else
        native_pid=''
        broray_native_auth_runtime_in_use && return 1
    fi
    if [ -n "$native_pid" ] && kill -0 "$native_pid" 2>/dev/null; then
        broray_native_auth_process_twice "$native_pid" || return 1
        kill -QUIT "$native_pid" 2>/dev/null || return 1
        native_wait=0
        while kill -0 "$native_pid" 2>/dev/null && [ "$native_wait" -lt 10 ]; do
            sleep 1
            native_wait=$((native_wait + 1))
        done
        if kill -0 "$native_pid" 2>/dev/null; then
            broray_native_auth_process_once "$native_pid" && return 1
        fi
    elif [ -n "$native_pid" ]; then
        [ ! -e "$BRORAY_NATIVE_AUTH_PROC_ROOT/$native_pid" ] &&
            [ ! -L "$BRORAY_NATIVE_AUTH_PROC_ROOT/$native_pid" ] || return 1
    fi
    case "$BRORAY_NATIVE_AUTH_DIR" in "$BRORAY_NATIVE_AUTH_BASE"/run/web-new/native-auth) ;; *) return 1 ;; esac
    rm -rf "$BRORAY_NATIVE_AUTH_DIR"
}

broray_native_auth_ensure() {
    if broray_native_auth_probe "$BRORAY_NATIVE_AUTH_SYSTEM_URL"; then
        BRORAY_NATIVE_AUTH_ACTIVE_URL="$BRORAY_NATIVE_AUTH_SYSTEM_URL"
        export BRORAY_NATIVE_AUTH_ACTIVE_URL
        return 0
    fi
    if [ -e "$BRORAY_NATIVE_AUTH_PIDFILE" ] || [ -L "$BRORAY_NATIVE_AUTH_PIDFILE" ]; then
        native_pid="$(broray_native_auth_pid_read 2>/dev/null || true)"
        [ -n "$native_pid" ] || return 1
        if broray_native_auth_process_twice "$native_pid"; then
            if broray_native_auth_probe "$BRORAY_NATIVE_AUTH_SIDECAR_URL"; then
                BRORAY_NATIVE_AUTH_ACTIVE_URL="$BRORAY_NATIVE_AUTH_SIDECAR_URL"
                export BRORAY_NATIVE_AUTH_ACTIVE_URL
                return 0
            fi
            broray_native_auth_stop || return 1
        elif kill -0 "$native_pid" 2>/dev/null; then
            return 1
        else
            [ ! -e "$BRORAY_NATIVE_AUTH_PROC_ROOT/$native_pid" ] &&
                [ ! -L "$BRORAY_NATIVE_AUTH_PROC_ROOT/$native_pid" ] || return 1
            rm -f "$BRORAY_NATIVE_AUTH_PIDFILE" || return 1
        fi
    elif broray_native_auth_runtime_in_use; then
        return 1
    fi
    broray_native_auth_prepare || return 1
    "$BRORAY_NATIVE_AUTH_RUNTIME" -p / -c "$BRORAY_NATIVE_AUTH_CONFIG" >/dev/null 2>&1 || return 1
    native_wait=0
    while [ "$native_wait" -lt 10 ]; do
        native_pid="$(broray_native_auth_pid_read 2>/dev/null || true)"
        if [ -n "$native_pid" ] && broray_native_auth_process_twice "$native_pid" &&
           broray_native_auth_probe "$BRORAY_NATIVE_AUTH_SIDECAR_URL"
        then
            BRORAY_NATIVE_AUTH_ACTIVE_URL="$BRORAY_NATIVE_AUTH_SIDECAR_URL"
            export BRORAY_NATIVE_AUTH_ACTIVE_URL
            return 0
        fi
        sleep 1
        native_wait=$((native_wait + 1))
    done
    broray_native_auth_stop >/dev/null 2>&1 || true
    return 1
}
