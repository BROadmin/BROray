#!/opt/bin/ash

BRORAY_LIGHTTPD_GUARD_ROOT="${BRORAY_LIGHTTPD_GUARD_ROOT:-/opt/var/lib/broray/lighttpd-guard}"
BRORAY_LIGHTTPD_GUARD_RECEIPT="$BRORAY_LIGHTTPD_GUARD_ROOT/receipt"
BRORAY_LIGHTTPD_GUARD_ORIGINAL="$BRORAY_LIGHTTPD_GUARD_ROOT/S80lighttpd.original"
BRORAY_LIGHTTPD_INIT="${BRORAY_LIGHTTPD_INIT:-/opt/etc/init.d/S80lighttpd}"
BRORAY_LIGHTTPD_CONFIG="${BRORAY_LIGHTTPD_CONFIG:-/opt/etc/lighttpd/lighttpd.conf}"
BRORAY_LIGHTTPD_LOG="${BRORAY_LIGHTTPD_LOG:-/opt/broray/logs/lighttpd-guard.log}"
BRORAY_LIGHTTPD_PROC_ROOT="${BRORAY_LIGHTTPD_PROC_ROOT:-/proc}"
BRORAY_LIGHTTPD_OPKG_STATUS="${BRORAY_LIGHTTPD_OPKG_STATUS:-/opt/lib/opkg/status}"
BRORAY_LIGHTTPD_INFO_ROOT="${BRORAY_LIGHTTPD_INFO_ROOT:-/opt/lib/opkg/info}"

broray_lighttpd_guard_log()
{
    mkdir -p "${BRORAY_LIGHTTPD_LOG%/*}" 2>/dev/null || return 0
    printf '%s  %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*" >>"$BRORAY_LIGHTTPD_LOG" 2>/dev/null || true
}

broray_lighttpd_guard_sha()
{
    sha256sum "$1" 2>/dev/null | awk 'NR==1{print $1;exit}'
}

broray_lighttpd_guard_regular()
{
    [ -f "$1" ] && [ ! -L "$1" ]
}

broray_lighttpd_guard_value()
{
    guard_value_file="$1"
    guard_value_key="$2"
    awk -F= -v key="$guard_value_key" '
      $1==key {count++; value=substr($0,length(key)+2)}
      END {if(count==1) print value; else exit 1}
    ' "$guard_value_file"
}

broray_lighttpd_guard_package_installed()
{
    opkg status "$1" 2>/dev/null | awk -F ': ' '
      $1=="Package"{p++;pn=$2}
      $1=="Status"{s++;sv=$2}
      END{exit !(p==1&&pn==package&&s==1&&(sv=="install user installed"||sv=="install ok installed"))}
    ' package="$1"
}

broray_lighttpd_guard_package_version()
{
    opkg status lighttpd 2>/dev/null | awk -F ': ' '
      $1=="Package"&&$2=="lighttpd"{p++}
      $1=="Version"{v++;vv=$2}
      $1=="Status"&&($2=="install user installed"||$2=="install ok installed"){s++}
      END{if(p==1&&v==1&&s==1)print vv;else exit 1}
    '
}

broray_lighttpd_guard_package_config_sha()
{
    opkg status lighttpd 2>/dev/null | awk '
      $1=="/opt/etc/lighttpd/lighttpd.conf" && $2 ~ /^[0-9a-f]{64}$/ {count++; value=$2}
      END{if(count==1)print value;else exit 1}
    '
}

broray_lighttpd_guard_info_value()
{
    guard_info_file="$1"
    guard_info_key="$2"
    awk -F ': ' -v key="$guard_info_key" '
      $1==key {count++; value=substr($0,length(key)+3)}
      END {if(count==1)print value;else exit 1}
    ' "$guard_info_file"
}

broray_lighttpd_guard_transient_metadata_valid()
{
    guard_lighttpd_control="$BRORAY_LIGHTTPD_INFO_ROOT/lighttpd.control"
    guard_lighttpd_conffiles="$BRORAY_LIGHTTPD_INFO_ROOT/lighttpd.conffiles"
    guard_cgi_control="$BRORAY_LIGHTTPD_INFO_ROOT/lighttpd-mod-cgi.control"
    guard_cgi_conffiles="$BRORAY_LIGHTTPD_INFO_ROOT/lighttpd-mod-cgi.conffiles"
    for guard_info_file in \
        "$guard_lighttpd_control" "$guard_lighttpd_conffiles" \
        "$guard_cgi_control" "$guard_cgi_conffiles"
    do
        broray_lighttpd_guard_regular "$guard_info_file" || return 1
        [ "$(find -P "$guard_info_file" -maxdepth 0 -type f -printf '%m|%U\n' 2>/dev/null)" = '644|0' ] || return 1
    done
    [ "$(broray_lighttpd_guard_info_value "$guard_lighttpd_control" Package)" = lighttpd ] || return 1
    [ "$(broray_lighttpd_guard_info_value "$guard_lighttpd_control" Architecture)" = aarch64-3.10 ] || return 1
    guard_info_version="$(broray_lighttpd_guard_info_value "$guard_lighttpd_control" Version)" || return 1
    case "$guard_info_version" in ''|*[!0-9A-Za-z._+-]*) return 1 ;; esac
    [ "$(broray_lighttpd_guard_info_value "$guard_cgi_control" Package)" = lighttpd-mod-cgi ] || return 1
    [ "$(broray_lighttpd_guard_info_value "$guard_cgi_control" Architecture)" = aarch64-3.10 ] || return 1
    guard_cgi_depends="$(broray_lighttpd_guard_info_value "$guard_cgi_control" Depends)" || return 1
    printf '%s\n' "$guard_cgi_depends" | awk '
      BEGIN{ok=0}
      {
        count=split($0,item,/, */)
        for(i=1;i<=count;i++) if(item[i]=="lighttpd")ok++
      }
      END{exit !(ok==1)}
    ' || return 1
    [ "$(wc -l <"$guard_lighttpd_conffiles" | tr -d ' ')" -eq 1 ] || return 1
    [ "$(sed -n '1p' "$guard_lighttpd_conffiles")" = /opt/etc/lighttpd/lighttpd.conf ] || return 1
    [ "$(wc -l <"$guard_cgi_conffiles" | tr -d ' ')" -eq 1 ] || return 1
    [ "$(sed -n '1p' "$guard_cgi_conffiles")" = /opt/etc/lighttpd/conf.d/30-cgi.conf ] || return 1
}

broray_lighttpd_guard_files_valid()
{
    broray_lighttpd_guard_regular "$BRORAY_LIGHTTPD_INIT" || return 1
    [ -x "$BRORAY_LIGHTTPD_INIT" ] || return 1
    broray_lighttpd_guard_regular "$BRORAY_LIGHTTPD_CONFIG" || return 1
    [ "$(find -P "$BRORAY_LIGHTTPD_INIT" -maxdepth 0 -type f -printf '%m|%U\n' 2>/dev/null)" = '755|0' ] || return 1
    [ "$(find -P "$BRORAY_LIGHTTPD_CONFIG" -maxdepth 0 -type f -printf '%m|%U\n' 2>/dev/null)" = '644|0' ] || return 1
    ash -n "$BRORAY_LIGHTTPD_INIT" >/dev/null 2>&1 || return 1
    [ "$(grep -Ec '^[[:space:]]*ENABLED=(yes|no)[[:space:]]*$' "$BRORAY_LIGHTTPD_INIT" 2>/dev/null)" -eq 1 ] || return 1
    [ "$(grep -Ec '^[[:space:]]*PROCS=lighttpd[[:space:]]*$' "$BRORAY_LIGHTTPD_INIT" 2>/dev/null)" -eq 1 ] || return 1
    [ "$(grep -Ec '^[[:space:]]*ARGS="-f /opt/etc/lighttpd/lighttpd.conf"[[:space:]]*$' "$BRORAY_LIGHTTPD_INIT" 2>/dev/null)" -eq 1 ] || return 1
}

broray_lighttpd_guard_assets_valid_once()
{
    BRORAY_LIGHTTPD_ASSETS_REASON=package-lighttpd
    broray_lighttpd_guard_package_installed lighttpd || return 1
    BRORAY_LIGHTTPD_ASSETS_REASON=package-lighttpd-mod-cgi
    broray_lighttpd_guard_package_installed lighttpd-mod-cgi || return 1
    BRORAY_LIGHTTPD_ASSETS_REASON=transient-metadata
    broray_lighttpd_guard_transient_metadata_valid || return 1
    BRORAY_LIGHTTPD_ASSETS_REASON=standard-files
    broray_lighttpd_guard_files_valid || return 1
    BRORAY_LIGHTTPD_ASSETS_REASON=package-config-sha
    guard_expected_config_sha="$(broray_lighttpd_guard_package_config_sha)" || return 1
    BRORAY_LIGHTTPD_ASSETS_REASON=current-config-sha
    guard_current_config_sha="$(broray_lighttpd_guard_sha "$BRORAY_LIGHTTPD_CONFIG")" || return 1
    BRORAY_LIGHTTPD_ASSETS_REASON=config-sha-mismatch
    [ "$guard_current_config_sha" = "$guard_expected_config_sha" ] || return 1
    BRORAY_LIGHTTPD_ASSETS_REASON=ok
}

broray_lighttpd_guard_assets_valid()
{
    guard_assets_attempt=1
    while [ "$guard_assets_attempt" -le 3 ]
    do
        if broray_lighttpd_guard_assets_valid_once; then
            return 0
        fi
        guard_assets_reason="${BRORAY_LIGHTTPD_ASSETS_REASON:-unknown}"
        if [ "$guard_assets_attempt" -lt 3 ]; then
            broray_lighttpd_guard_log \
                "LIGHTTPD_GUARD_ASSETS_RETRY=$guard_assets_attempt reason=$guard_assets_reason"
            sleep 1
        else
            broray_lighttpd_guard_log \
                "LIGHTTPD_GUARD_ASSETS=FAIL attempts=$guard_assets_attempt reason=$guard_assets_reason"
            return 1
        fi
        guard_assets_attempt=$((guard_assets_attempt + 1))
    done
    return 1
}

broray_lighttpd_guard_port_owner()
{
    guard_port="$1"
    netstat -lntp 2>/dev/null | awk -v port="$guard_port" '
      $4 ~ (":" port "$") && $6=="LISTEN" {
        split($7,owner,"/"); if(owner[1] ~ /^[0-9]+$/) print owner[1]
      }
    ' | sort -u | while IFS= read -r guard_pid
    do
        [ -r "/proc/$guard_pid/comm" ] || continue
        sed -n '1p' "/proc/$guard_pid/comm"
    done | sort -u
}

broray_lighttpd_guard_port_is()
{
    guard_port_is_port="$1"
    guard_port_is_owner="$2"
    guard_port_is_actual="$(broray_lighttpd_guard_port_owner "$guard_port_is_port")" || return 1
    [ "$guard_port_is_actual" = "$guard_port_is_owner" ]
}

broray_lighttpd_guard_default_pids()
{
    guard_default_pids=''
    guard_foreign_process=false
    for guard_process_dir in "$BRORAY_LIGHTTPD_PROC_ROOT"/[0-9]*
    do
        [ -r "$guard_process_dir/comm" ] && [ -r "$guard_process_dir/cmdline" ] || continue
        [ "$(sed -n '1p' "$guard_process_dir/comm" 2>/dev/null)" = lighttpd ] || continue
        guard_process_cmd="$(tr '\000' ' ' <"$guard_process_dir/cmdline" 2>/dev/null)" || continue
        case "$guard_process_cmd" in
            'lighttpd -f /opt/etc/lighttpd/lighttpd.conf '|'/opt/sbin/lighttpd -f /opt/etc/lighttpd/lighttpd.conf ')
                guard_process_pid="${guard_process_dir##*/}"
                case "$guard_process_pid" in ''|*[!0-9]*) return 2 ;; esac
                guard_default_pids="$guard_default_pids $guard_process_pid"
                ;;
            *) guard_foreign_process=true ;;
        esac
    done
    [ "$guard_foreign_process" = false ] || return 2
    [ -n "$guard_default_pids" ] || return 1
    printf '%s\n' "$guard_default_pids" | awk '{$1=$1;print}'
}

broray_lighttpd_guard_default_process_present()
{
    broray_lighttpd_guard_default_pids >/dev/null 2>&1
}

broray_lighttpd_guard_disable_init()
{
    guard_disable_tmp="$BRORAY_LIGHTTPD_GUARD_ROOT/.S80lighttpd.disabled.$$"
    [ ! -e "$guard_disable_tmp" ] && [ ! -L "$guard_disable_tmp" ] || return 1
    if grep -q '^[[:space:]]*ENABLED=no[[:space:]]*$' "$BRORAY_LIGHTTPD_INIT"; then
        return 0
    fi
    grep -q '^[[:space:]]*ENABLED=yes[[:space:]]*$' "$BRORAY_LIGHTTPD_INIT" || return 1
    cp -p "$BRORAY_LIGHTTPD_INIT" "$guard_disable_tmp" || return 1
    sed -i 's/^[[:space:]]*ENABLED=yes[[:space:]]*$/ENABLED=no/' "$guard_disable_tmp" || {
        rm -f "$guard_disable_tmp"
        return 1
    }
    grep -q '^[[:space:]]*ENABLED=no[[:space:]]*$' "$guard_disable_tmp" &&
    [ "$(grep -Ec '^[[:space:]]*ENABLED=(yes|no)[[:space:]]*$' "$guard_disable_tmp")" -eq 1 ] &&
    ash -n "$guard_disable_tmp" >/dev/null 2>&1 || {
        rm -f "$guard_disable_tmp"
        return 1
    }
    cp -p "$guard_disable_tmp" "$BRORAY_LIGHTTPD_INIT" || {
        rm -f "$guard_disable_tmp"
        return 1
    }
    rm -f "$guard_disable_tmp"
    sync
}

broray_lighttpd_guard_stop_default()
{
    guard_process_rc=0
    guard_process_pids="$(broray_lighttpd_guard_default_pids 2>/dev/null)" || guard_process_rc=$?
    case "$guard_process_rc" in
        1) return 0 ;;
        0)
            for guard_process_pid in $guard_process_pids
            do
                kill "$guard_process_pid" 2>/dev/null || true
            done
            ;;
        *) return 1 ;;
    esac
    guard_wait=1
    while [ "$guard_wait" -le 15 ]
    do
        guard_process_rc=0
        guard_process_pids="$(broray_lighttpd_guard_default_pids 2>/dev/null)" || guard_process_rc=$?
        case "$guard_process_rc" in
            1) return 0 ;;
            0)
                for guard_process_pid in $guard_process_pids
                do
                    kill "$guard_process_pid" 2>/dev/null || true
                done
                ;;
            *) return 1 ;;
        esac
        sleep 1
        guard_wait=$((guard_wait + 1))
    done
    return 1
}

broray_lighttpd_guard_wait_ports()
{
    guard_wait=1
    while [ "$guard_wait" -le 20 ]
    do
        if broray_lighttpd_guard_port_is 80 nginx &&
           broray_lighttpd_guard_port_is 8080 broray-lighttpd
        then
            return 0
        fi
        sleep 1
        guard_wait=$((guard_wait + 1))
    done
    return 1
}

broray_lighttpd_guard_wait_port80()
{
    guard_wait=1
    while [ "$guard_wait" -le 20 ]
    do
        broray_lighttpd_guard_port_is 80 nginx && return 0
        sleep 1
        guard_wait=$((guard_wait + 1))
    done
    return 1
}

broray_lighttpd_guard_receipt_valid()
{
    broray_lighttpd_guard_regular "$BRORAY_LIGHTTPD_GUARD_RECEIPT" || return 1
    broray_lighttpd_guard_regular "$BRORAY_LIGHTTPD_GUARD_ORIGINAL" || return 1
    [ "$(find -P "$BRORAY_LIGHTTPD_GUARD_RECEIPT" -maxdepth 0 -type f -printf '%m|%U\n' 2>/dev/null)" = '600|0' ] || return 1
    [ "$(find -P "$BRORAY_LIGHTTPD_GUARD_ORIGINAL" -maxdepth 0 -type f -printf '%m|%U\n' 2>/dev/null)" = '600|0' ] || return 1
    [ "$(wc -l <"$BRORAY_LIGHTTPD_GUARD_RECEIPT" | tr -d ' ')" -eq 10 ] || return 1
    [ "$(broray_lighttpd_guard_value "$BRORAY_LIGHTTPD_GUARD_RECEIPT" schema)" = 1 ] || return 1
    [ "$(broray_lighttpd_guard_value "$BRORAY_LIGHTTPD_GUARD_RECEIPT" owner)" = BROray ] || return 1
    guard_receipt_baseline="$(broray_lighttpd_guard_value "$BRORAY_LIGHTTPD_GUARD_RECEIPT" baseline)" || return 1
    case "$guard_receipt_baseline" in fresh|legacy-r14) ;; *) return 1 ;; esac
    guard_receipt_original_sha="$(broray_lighttpd_guard_value "$BRORAY_LIGHTTPD_GUARD_RECEIPT" original_init_sha256)" || return 1
    [ "$(broray_lighttpd_guard_sha "$BRORAY_LIGHTTPD_GUARD_ORIGINAL")" = "$guard_receipt_original_sha" ] || return 1
    guard_receipt_original_enabled="$(broray_lighttpd_guard_value "$BRORAY_LIGHTTPD_GUARD_RECEIPT" original_enabled)" || return 1
    case "$guard_receipt_original_enabled" in yes|no) ;; *) return 1 ;; esac
    guard_receipt_port80="$(broray_lighttpd_guard_value "$BRORAY_LIGHTTPD_GUARD_RECEIPT" original_port80)" || return 1
    case "$guard_receipt_port80" in nginx|none|lighttpd) ;; *) return 1 ;; esac
    guard_receipt_lighttpd_before="$(broray_lighttpd_guard_value "$BRORAY_LIGHTTPD_GUARD_RECEIPT" lighttpd_before)" || return 1
    guard_receipt_cgi_before="$(broray_lighttpd_guard_value "$BRORAY_LIGHTTPD_GUARD_RECEIPT" cgi_before)" || return 1
    case "$guard_receipt_lighttpd_before:$guard_receipt_cgi_before" in
        absent:absent|present:present|present:absent) ;;
        *) return 1 ;;
    esac
    case "$(broray_lighttpd_guard_value "$BRORAY_LIGHTTPD_GUARD_RECEIPT" package_version)" in ''|*[!0-9A-Za-z._+-]*) return 1 ;; esac
    case "$(broray_lighttpd_guard_value "$BRORAY_LIGHTTPD_GUARD_RECEIPT" adopted_at)" in
        [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]Z) ;;
        *) return 1 ;;
    esac
}

broray_lighttpd_guard_baseline_valid()
{
    guard_baseline="$1"
    broray_lighttpd_guard_regular "$guard_baseline" || return 1
    [ "$(find -P "$guard_baseline" -maxdepth 0 -type f -printf '%m|%U\n' 2>/dev/null)" = '600|0' ] || return 1
    [ "$(wc -l <"$guard_baseline" | tr -d ' ')" -eq 10 ] || return 1
    [ "$(broray_lighttpd_guard_value "$guard_baseline" schema)" = 1 ] || return 1
    [ "$(broray_lighttpd_guard_value "$guard_baseline" owner)" = BROray-installer ] || return 1
    guard_baseline_legacy="$(broray_lighttpd_guard_value "$guard_baseline" legacy_track)" || return 1
    case "$guard_baseline_legacy" in none|3.0.0-r14) ;; *) return 1 ;; esac
    guard_baseline_lighttpd="$(broray_lighttpd_guard_value "$guard_baseline" lighttpd_package)" || return 1
    guard_baseline_cgi="$(broray_lighttpd_guard_value "$guard_baseline" cgi_package)" || return 1
    guard_baseline_init="$(broray_lighttpd_guard_value "$guard_baseline" init)" || return 1
    guard_baseline_config="$(broray_lighttpd_guard_value "$guard_baseline" config)" || return 1
    case "$guard_baseline_lighttpd:$guard_baseline_cgi:$guard_baseline_init:$guard_baseline_config" in
        absent:absent:absent:absent|present:*:present:present) ;;
        *) return 1 ;;
    esac
    guard_baseline_init_sha="$(broray_lighttpd_guard_value "$guard_baseline" init_sha256)" || return 1
    guard_baseline_config_sha="$(broray_lighttpd_guard_value "$guard_baseline" config_sha256)" || return 1
    if [ "$guard_baseline_lighttpd" = absent ]; then
        [ "$guard_baseline_init_sha:$guard_baseline_config_sha" = '-:-' ] || return 1
    else
        case "$guard_baseline_init_sha:$guard_baseline_config_sha" in *[!0-9a-f:]*) return 1 ;; esac
        [ "${#guard_baseline_init_sha}" -eq 64 ] && [ "${#guard_baseline_config_sha}" -eq 64 ] || return 1
    fi
    guard_baseline_port80="$(broray_lighttpd_guard_value "$guard_baseline" port80)" || return 1
    case "$guard_baseline_port80" in nginx|none|lighttpd) ;; *) return 1 ;; esac
}

broray_lighttpd_guard_known_legacy_adopt()
{
    [ ! -e "$BRORAY_LIGHTTPD_GUARD_ROOT" ] && [ ! -L "$BRORAY_LIGHTTPD_GUARD_ROOT" ] || return 1
    broray_lighttpd_guard_regular "$BRORAY_LIGHTTPD_INIT" || return 1
    broray_lighttpd_guard_regular "$BRORAY_LIGHTTPD_CONFIG" || return 1
    [ "$(broray_lighttpd_guard_sha "$BRORAY_LIGHTTPD_INIT")" = b5e63c7cf340d06f55366ec719fc5294811fc0e96bd37ed789b77f535d8ecbf8 ] || return 1
    [ "$(broray_lighttpd_guard_sha "$BRORAY_LIGHTTPD_CONFIG")" = a445465f0e6193ca32af65c9b9939ae05323bf6d2e1a23e6b73c451ec5962308 ] || return 1
    guard_known_init_identity="$(find -P "$BRORAY_LIGHTTPD_INIT" -maxdepth 0 -type f -printf '%m|%U\n' 2>/dev/null)"
    case "$guard_known_init_identity" in
        '600|0') chmod 0755 "$BRORAY_LIGHTTPD_INIT" || return 1 ;;
        '755|0') ;;
        *) return 1 ;;
    esac
    broray_lighttpd_guard_files_valid || return 1
    guard_known_parent="${BRORAY_LIGHTTPD_GUARD_ROOT%/*}"
    guard_known_baseline="$guard_known_parent/.lighttpd-baseline.$$"
    [ -d "$guard_known_parent" ] && [ ! -L "$guard_known_parent" ] || return 1
    [ ! -e "$guard_known_baseline" ] && [ ! -L "$guard_known_baseline" ] || return 1
    umask 077
    {
        printf '%s\n' \
            'schema=1' \
            'owner=BROray-installer' \
            'legacy_track=3.0.0-r14' \
            'lighttpd_package=present' \
            'cgi_package=present' \
            'init=present' \
            'init_sha256=b5e63c7cf340d06f55366ec719fc5294811fc0e96bd37ed789b77f535d8ecbf8' \
            'config=present' \
            'config_sha256=a445465f0e6193ca32af65c9b9939ae05323bf6d2e1a23e6b73c451ec5962308' \
            'port80=nginx'
    } >"$guard_known_baseline" || return 1
    chmod 0600 "$guard_known_baseline" || { rm -f "$guard_known_baseline"; return 1; }
    guard_known_result=0
    broray_lighttpd_guard_adopt_transient "$guard_known_baseline" || guard_known_result=$?
    rm -f "$guard_known_baseline" || return 1
    return "$guard_known_result"
}

broray_lighttpd_guard_restore_original()
{
    broray_lighttpd_guard_receipt_valid || return 1
    broray_lighttpd_guard_transient_metadata_valid || return 1
    broray_lighttpd_guard_files_valid || return 1
    broray_lighttpd_guard_stop_default || return 1
    cp -p "$BRORAY_LIGHTTPD_GUARD_ORIGINAL" "$BRORAY_LIGHTTPD_INIT" || return 1
    chmod 0755 "$BRORAY_LIGHTTPD_INIT" || return 1
    [ "$(find -P "$BRORAY_LIGHTTPD_INIT" -maxdepth 0 -type f -printf '%m|%U\n' 2>/dev/null)" = '755|0' ] || return 1
    [ "$(broray_lighttpd_guard_sha "$BRORAY_LIGHTTPD_INIT")" = "$guard_receipt_original_sha" ] || return 1
    if [ "$guard_receipt_port80" = lighttpd ]; then
        "$BRORAY_LIGHTTPD_INIT" start >/dev/null 2>&1 || return 1
    fi
    sync
}

broray_lighttpd_guard_adopt_transient()
{
    guard_baseline_file="$1"
    broray_lighttpd_guard_baseline_valid "$guard_baseline_file" || return 1
    [ ! -e "$BRORAY_LIGHTTPD_GUARD_ROOT" ] && [ ! -L "$BRORAY_LIGHTTPD_GUARD_ROOT" ] || return 1
    if [ "$guard_baseline_lighttpd" = present ] && [ "$guard_baseline_legacy" != 3.0.0-r14 ]; then
        printf '%s\n' 'CONFLICT: Lighttpd existed before BROray; installation did not change or stop it.' >&2
        return 1
    fi
    broray_lighttpd_guard_transient_metadata_valid || {
        printf '%s\n' 'CONFLICT: unpacked Lighttpd package metadata is unsafe or incomplete.' >&2
        return 1
    }
    broray_lighttpd_guard_files_valid || {
        printf '%s\n' 'CONFLICT: standard Lighttpd files are modified or ambiguous.' >&2
        return 1
    }
    if [ "$guard_baseline_lighttpd" = present ]; then
        [ "$(broray_lighttpd_guard_sha "$BRORAY_LIGHTTPD_INIT")" = "$guard_baseline_init_sha" ] || return 1
        [ "$(broray_lighttpd_guard_sha "$BRORAY_LIGHTTPD_CONFIG")" = "$guard_baseline_config_sha" ] || return 1
        guard_receipt_class=legacy-r14
    else
        guard_receipt_class=fresh
    fi
    mkdir -m 700 "$BRORAY_LIGHTTPD_GUARD_ROOT" || return 1
    cp -p "$BRORAY_LIGHTTPD_INIT" "$BRORAY_LIGHTTPD_GUARD_ORIGINAL" || return 1
    chmod 600 "$BRORAY_LIGHTTPD_GUARD_ORIGINAL" || return 1
    guard_original_sha="$(broray_lighttpd_guard_sha "$BRORAY_LIGHTTPD_GUARD_ORIGINAL")" || return 1
    if grep -q '^[[:space:]]*ENABLED=yes[[:space:]]*$' "$BRORAY_LIGHTTPD_GUARD_ORIGINAL"; then
        guard_original_enabled=yes
    else
        guard_original_enabled=no
    fi
    guard_receipt_tmp="$BRORAY_LIGHTTPD_GUARD_ROOT/.receipt.$$"
    {
        printf 'schema=1\nowner=BROray\nbaseline=%s\n' "$guard_receipt_class"
        printf 'lighttpd_before=%s\ncgi_before=%s\n' "$guard_baseline_lighttpd" "$guard_baseline_cgi"
        printf 'original_init_sha256=%s\noriginal_enabled=%s\n' "$guard_original_sha" "$guard_original_enabled"
        printf 'original_port80=%s\npackage_version=%s\nadopted_at=%s\n' \
            "$guard_baseline_port80" "$guard_info_version" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    } >"$guard_receipt_tmp" || return 1
    chmod 600 "$guard_receipt_tmp" || return 1
    mv -f "$guard_receipt_tmp" "$BRORAY_LIGHTTPD_GUARD_RECEIPT" || return 1
    if ! broray_lighttpd_guard_disable_init ||
       ! broray_lighttpd_guard_stop_default ||
       ! broray_lighttpd_guard_wait_port80
    then
        broray_lighttpd_guard_restore_original >/dev/null 2>&1 || true
        rm -rf "$BRORAY_LIGHTTPD_GUARD_ROOT" 2>/dev/null || true
        return 1
    fi
    broray_lighttpd_guard_log "LIGHTTPD_GUARD_ADOPT=PASS baseline=$guard_receipt_class"
}

broray_lighttpd_guard_finalize()
{
    broray_lighttpd_guard_receipt_valid || return 1
    broray_lighttpd_guard_assets_valid || return 1
    broray_lighttpd_guard_maintain || return 1
    broray_lighttpd_guard_wait_ports || return 1
    broray_lighttpd_guard_log 'LIGHTTPD_GUARD_FINALIZE=PASS'
}

broray_lighttpd_guard_maintain()
{
    broray_lighttpd_guard_receipt_valid || return 1
    broray_lighttpd_guard_assets_valid || {
        broray_lighttpd_guard_log \
            "LIGHTTPD_GUARD_MAINTAIN=FAIL reason=modified-standard-assets detail=${BRORAY_LIGHTTPD_ASSETS_REASON:-unknown}"
        return 1
    }
    guard_port80_now="$(broray_lighttpd_guard_port_owner 80)" || return 1
    case "$guard_port80_now" in
        nginx|'') ;;
        lighttpd) ;;
        *)
            broray_lighttpd_guard_log "LIGHTTPD_GUARD_MAINTAIN=FAIL reason=foreign-port80 owner=$guard_port80_now"
            return 1
            ;;
    esac
    broray_lighttpd_guard_disable_init &&
    broray_lighttpd_guard_stop_default &&
    broray_lighttpd_guard_wait_port80 || {
        broray_lighttpd_guard_log 'LIGHTTPD_GUARD_MAINTAIN=FAIL reason=port-contract'
        return 1
    }
}

broray_lighttpd_guard_other_dependents()
{
    awk 'BEGIN{RS="";FS="\n"}
      {
        package=""; status=""; depends=""
        for(i=1;i<=NF;i++) {
          if($i ~ /^Package: /) package=substr($i,10)
          else if($i ~ /^Status: /) status=substr($i,9)
          else if($i ~ /^Depends: /) depends=substr($i,10)
        }
        if((status=="install user installed" || status=="install ok installed") &&
           package!="broray" && package!="lighttpd-mod-cgi" &&
           (depends ~ /(^|, | )lighttpd($|,| )/ || depends ~ /(^|, | )lighttpd-any($|,| )/)) print package
      }
    ' "$BRORAY_LIGHTTPD_OPKG_STATUS" 2>/dev/null
}

broray_lighttpd_guard_uninstall_unmanaged()
{
    # Compact clean bootstrap uses a separate BROray web service. It neither
    # adopts nor owns the shared Entware service. Missing ownership alone is
    # insufficient: require its exact package contract and intact stock assets.
    [ ! -e "$BRORAY_LIGHTTPD_GUARD_ROOT" ] && [ ! -L "$BRORAY_LIGHTTPD_GUARD_ROOT" ] || return 1
    guard_compact_control="$BRORAY_LIGHTTPD_INFO_ROOT/broray.control"
    broray_lighttpd_guard_regular "$guard_compact_control" || return 1
    [ "$(broray_lighttpd_guard_info_value "$guard_compact_control" Package)" = broray ] || return 1
    [ "$(broray_lighttpd_guard_info_value "$guard_compact_control" X-BROray-Canonical-Lifecycle)" = compact-app-rename/1 ] || return 1
    [ "$(broray_lighttpd_guard_info_value "$guard_compact_control" X-BROray-Distribution-Role)" = metadata-only-clean-bootstrap ] || return 1
    grep -q '^[[:space:]]*ENABLED=yes[[:space:]]*$' "$BRORAY_LIGHTTPD_INIT" || return 1
    broray_lighttpd_guard_assets_valid
}

broray_lighttpd_guard_uninstall_preflight()
{
    broray_lighttpd_guard_receipt_valid ||
        broray_lighttpd_guard_known_legacy_adopt || return 1
    broray_lighttpd_guard_assets_valid || return 1
    broray_lighttpd_guard_maintain || return 1
    if [ "$guard_receipt_baseline" = fresh ]; then
        [ "$guard_receipt_lighttpd_before:$guard_receipt_cgi_before" = absent:absent ] || return 1
        broray_lighttpd_guard_package_installed lighttpd-mod-cgi || return 1
        [ -z "$(broray_lighttpd_guard_other_dependents)" ] || return 1
    fi
}

broray_lighttpd_guard_uninstall_restore()
{
    broray_lighttpd_guard_uninstall_preflight || return 1
    if [ "$guard_receipt_baseline" = legacy-r14 ]; then
        broray_lighttpd_guard_restore_original || return 1
        broray_lighttpd_guard_log 'LIGHTTPD_GUARD_UNINSTALL_RESTORE=PASS baseline=legacy-r14'
        return 0
    fi
    broray_lighttpd_guard_stop_default || return 1
    opkg remove lighttpd-mod-cgi lighttpd >/dev/null 2>&1 || return 1
    ! broray_lighttpd_guard_package_installed lighttpd || return 1
    [ ! -e "$BRORAY_LIGHTTPD_INIT" ] && [ ! -L "$BRORAY_LIGHTTPD_INIT" ] || return 1
    guard_nginx_wait=1
    while [ "$guard_nginx_wait" -le 20 ]
    do
        broray_lighttpd_guard_port_is 80 nginx && break
        sleep 1
        guard_nginx_wait=$((guard_nginx_wait + 1))
    done
    broray_lighttpd_guard_port_is 80 nginx || return 1
    broray_lighttpd_guard_log 'LIGHTTPD_GUARD_UNINSTALL_RESTORE=PASS baseline=fresh'
}

broray_lighttpd_guard_status()
{
    if { broray_lighttpd_guard_receipt_valid ||
         broray_lighttpd_guard_known_legacy_adopt; } &&
       broray_lighttpd_guard_maintain &&
       broray_lighttpd_guard_wait_ports
    then
        printf '{"ok":true,"managed":true,"port80":"nginx","port8080":"broray-lighttpd"}\n'
    else
        printf '{"ok":false,"managed":false,"error":"LIGHTTPD_PORT_OWNERSHIP_INVALID"}\n'
        return 1
    fi
}

broray_lighttpd_guard_main()
{
    case "${1:-}" in
        adopt-transient) broray_lighttpd_guard_adopt_transient "${2:-}" ;;
        finalize) broray_lighttpd_guard_finalize ;;
        maintain) broray_lighttpd_guard_maintain ;;
        status) broray_lighttpd_guard_status ;;
        rollback) broray_lighttpd_guard_restore_original ;;
        uninstall-preflight) broray_lighttpd_guard_uninstall_preflight ;;
        uninstall-unmanaged) broray_lighttpd_guard_uninstall_unmanaged ;;
        uninstall-restore) broray_lighttpd_guard_uninstall_restore ;;
        *) printf '%s\n' 'usage: lighttpd-guard.sh {adopt-transient BASELINE|finalize|maintain|status|rollback|uninstall-preflight|uninstall-restore}' >&2; return 2 ;;
    esac
}

if [ "${0##*/}" = lighttpd-guard.sh ]; then
    broray_lighttpd_guard_main "$@"
fi
