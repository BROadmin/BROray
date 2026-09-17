#!/opt/bin/ash
# SIP002 + legacy import. Input is data, never shell/printf escape syntax.
BRORAY_BASE="${BRORAY_BASE:-${BRORAY_ROOT:-/opt/broray}}"
. "$BRORAY_BASE/lib/util.sh"

broray_shadowsocks_base64_decode()
{
    local value normalized bare padding canonical
    value="$1"
    case "$value" in ''|*[!a-zA-Z0-9_+/=-]*) return 1 ;; esac
    normalized="$(printf '%s' "$value" | tr '_-' '/+')"
    bare="${normalized%%=*}"; padding="${normalized#"$bare"}"
    case "$padding" in ''|'='|'==') ;; *) return 1 ;; esac
    case "$((${#bare} % 4))" in
        0) canonical="$bare" ;;
        2) canonical="$bare==" ;;
        3) canonical="$bare=" ;;
        *) return 1 ;;
    esac
    [ -z "$padding" ] || [ "$normalized" = "$canonical" ] || return 1
    printf '%s' "$canonical" | base64 -d > "$2" 2>/dev/null || return 1
    [ -s "$2" ] || return 1
    [ "$(base64 < "$2" | tr -d '\r\n')" = "$canonical" ]
}

# Read UTF-8 without allowing jq replacement characters to hide corrupt bytes.
broray_ss_text()
{
    [ "$(base64 < "$1" | tr -d '\r\n')" = "$(jq -Rrs '@base64' "$1" 2>/dev/null)" ] || return 1
    jq -Rse 'length <= 8192 and all(explode[]; . >= 32 and . != 127)' "$1" >/dev/null 2>&1 || return 1
    cat "$1"
}

broray_ss_percent_text()
{
    printf '%s' "$1" | LC_ALL=C awk '
      BEGIN { hex="0123456789abcdef" }
      { for(i=1;i<=length($0);i++) {
          c=substr($0,i,1);
          if(c=="%") {
              if(i+2>length($0)) exit 1;
              a=index(hex,tolower(substr($0,i+1,1)))-1;
              b=index(hex,tolower(substr($0,i+2,1)))-1;
              if(a<0 || b<0 || a*16+b<32 || a*16+b==127) exit 1;
              printf "%c",a*16+b; i+=2;
          } else printf "%s",c;
      }}' > "$2" || return 1
    broray_ss_text "$2"
}

broray_ss_valid_host()
{
    printf '%s\n' "$1" | LC_ALL=C awk '
      function ipv4(s, a,n,i) {
          n=split(s,a,"."); if(n!=4) return 0;
          for(i=1;i<=4;i++) if(a[i]!~/^[0-9]+$/ || length(a[i])>3 || a[i]+0>255 || (length(a[i])>1 && substr(a[i],1,1)=="0")) return 0;
          return 1;
      }
      { s=$0; if(length(s)<1 || length(s)>255) exit 1;
        if(index(s,":")) {
          if(s~/[^0-9a-fA-F:.]/ || s~/:::/) exit 1;
          p=index(s,"::"); if(p && index(substr(s,p+2),"::")) exit 1;
          n=split(s,a,":"); count=0;
          for(i=1;i<=n;i++) {
            if(a[i]=="") {if(!p) exit 1; continue;}
            if(index(a[i],".")) {if(i!=n || !ipv4(a[i])) exit 1; count+=2;}
            else {if(a[i]!~/^[0-9a-fA-F]+$/ || length(a[i])>4) exit 1; count++;}
          }
          if(p) exit !(count<8); else exit !(count==8);
        }
        if(s~/^[0-9.]+$/ && index(s,".")) exit !ipv4(s);
        sub(/\.$/,"",s); n=split(s,a,".");
        for(i=1;i<=n;i++) if(length(a[i])<1 || length(a[i])>63 || a[i]!~/^[a-zA-Z0-9_-]+$/ || a[i]~/^-/ || a[i]~/-$/) exit 1;
      }'
}

# Subshell isolates cleanup traps and all intermediate credential variables.
broray_shadowsocks_uri_json()
(
    umask 077
    ss_dir=""; trap '[ -z "$ss_dir" ] || { rm -f "$ss_dir/data"; rmdir "$ss_dir"; }' EXIT
    trap 'exit 1' HUP INT TERM
    [ "${#1}" -le 16384 ] || broray_die "Shadowsocks: ссылка слишком длинная"
    case "$1" in ss://*) ss_body="${1#ss://}" ;; *) broray_die "поддерживаются только ссылки ss://" ;; esac
    if ! printf '%s' "$ss_body" | jq -Rse 'all(explode[]; . >= 32 and . != 127)' >/dev/null 2>&1; then
        broray_die "Shadowsocks: ссылка содержит управляющие символы"
    fi
    mkdir -p "${BRORAY_TMP:-$BRORAY_BASE/tmp}" || exit 1
    ss_dir="$(mktemp -d "${BRORAY_TMP:-$BRORAY_BASE/tmp}/shadowsocks.XXXXXXXX")" || exit 1
    ss_file="$ss_dir/data"
    case "$ss_body" in *'#'*) ss_name="${ss_body#*#}"; ss_body="${ss_body%%#*}" ;; *) ss_name="" ;; esac
    case "$ss_body" in *'?'*) ss_query="${ss_body#*\?}"; ss_authority="${ss_body%%\?*}" ;; *) ss_query=""; ss_authority="$ss_body" ;; esac
    ss_name="$(broray_ss_percent_text "$ss_name" "$ss_file")" || broray_die "Shadowsocks: неправильное имя сервера"
    while [ -n "$ss_query" ]; do
        ss_pair="${ss_query%%&*}"
        case "$ss_query" in *'&'*) ss_query="${ss_query#*&}" ;; *) ss_query="" ;; esac
        ss_key="$(broray_ss_percent_text "${ss_pair%%=*}" "$ss_file")" || broray_die "Shadowsocks: неправильный параметр ссылки"
        ss_key="$(printf '%s' "$ss_key" | tr 'A-Z' 'a-z')"
        [ "$ss_key" = plugin ] || continue
        case "$ss_pair" in *=*) ss_value="${ss_pair#*=}" ;; *) broray_die "Shadowsocks SIP003 plugins пока не поддерживаются" ;; esac
        ss_value="$(broray_ss_percent_text "$ss_value" "$ss_file")" || broray_die "Shadowsocks: неправильный параметр plugin"
        [ -z "$ss_value" ] || broray_die "Shadowsocks SIP003 plugins пока не поддерживаются"
    done
    case "$ss_authority" in
        *@*)
            ss_credentials="${ss_authority%@*}"; ss_hostport="${ss_authority##*@}"
            case "$ss_credentials" in *@*) broray_die "Shadowsocks: неоднозначный разделитель адреса" ;; esac
            case "$ss_credentials" in
                *:*) ss_credentials="$(broray_ss_percent_text "$ss_credentials" "$ss_file")" || broray_die "Shadowsocks: неправильное кодирование данных подключения" ;;
                *) broray_shadowsocks_base64_decode "$ss_credentials" "$ss_file" || broray_die "Shadowsocks: неправильная Base64-строка"
                   ss_credentials="$(broray_ss_text "$ss_file")" || broray_die "Shadowsocks: данные подключения не являются допустимым UTF-8" ;;
            esac ;;
        *)
            broray_shadowsocks_base64_decode "$ss_authority" "$ss_file" || broray_die "Shadowsocks: неправильная legacy Base64-строка"
            ss_decoded="$(broray_ss_text "$ss_file")" || broray_die "Shadowsocks: legacy данные не являются допустимым UTF-8"
            case "$ss_decoded" in *@*) ss_credentials="${ss_decoded%@*}"; ss_hostport="${ss_decoded##*@}" ;; *) broray_die "Shadowsocks: не найден адрес сервера" ;; esac ;;
    esac
    case "$ss_credentials" in *:*) ss_method="${ss_credentials%%:*}"; ss_password="${ss_credentials#*:}" ;; *) broray_die "Shadowsocks: не найдено method:password" ;; esac
    [ -n "$ss_password" ] || broray_die "Shadowsocks: пустой пароль"
    # Strip only the optional endpoint slash, never a Base64 credential byte.
    ss_hostport="${ss_hostport%/}"
    case "$ss_hostport" in
        \[*\]:*) ss_address="${ss_hostport%%\]*}"; ss_address="${ss_address#\[}"; ss_port="${ss_hostport#*\]:}"
                  case "$ss_address" in *:*) ;; *) broray_die "Shadowsocks: скобки допустимы только для IPv6" ;; esac ;;
        *:*) ss_address="${ss_hostport%:*}"; ss_port="${ss_hostport##*:}"
             case "$ss_address" in *:*) broray_die "Shadowsocks: IPv6 должен быть в квадратных скобках" ;; esac ;;
        *) broray_die "Shadowsocks: не найден порт" ;;
    esac
    broray_ss_valid_host "$ss_address" || broray_die "Shadowsocks: неправильный адрес сервера"
    case "$ss_port" in ''|*[!0-9]*) broray_die "Shadowsocks: порт должен быть числом" ;; esac
    ss_port="$(printf '%s' "$ss_port" | sed 's/^0*//')"
    [ "${#ss_port}" -le 5 ] && [ -n "$ss_port" ] && [ "$ss_port" -ge 1 ] && [ "$ss_port" -le 65535 ] || broray_die "Shadowsocks: порт вне диапазона 1–65535"
    case "$ss_method" in
        aes-128-gcm|aes-256-gcm|chacha20-poly1305|chacha20-ietf-poly1305|xchacha20-poly1305) ss_keybytes=0 ;;
        2022-blake3-aes-128-gcm) ss_keybytes=16 ;;
        2022-blake3-aes-256-gcm|2022-blake3-chacha20-poly1305) ss_keybytes=32 ;;
        *) broray_die "неподдерживаемый метод Shadowsocks" ;;
    esac
    if [ "$ss_keybytes" -gt 0 ]; then
        case "$ss_method:$ss_password" in 2022-blake3-chacha20-poly1305:*:*) broray_die "Shadowsocks: цепочка ключей поддерживается только для AES-2022" ;; esac
        ss_keys="$ss_password"; ss_password=""
        while :; do
            ss_key="${ss_keys%%:*}"
            broray_shadowsocks_base64_decode "$ss_key" "$ss_file" || broray_die "Shadowsocks 2022: неправильное Base64-кодирование ключа"
            [ "$(wc -c < "$ss_file")" -eq "$ss_keybytes" ] || broray_die "Shadowsocks 2022: неправильная длина ключа"
            ss_key="$(base64 < "$ss_file" | tr -d '\r\n')"
            ss_password="${ss_password:+$ss_password:}$ss_key"
            case "$ss_keys" in *:*) ss_keys="${ss_keys#*:}" ;; *) break ;; esac
        done
    fi
    [ -n "$ss_name" ] || ss_name="$ss_address:$ss_port"
    jq -nc --arg method "$ss_method" --arg password "$ss_password" --arg address "$ss_address" \
        --arg port "$ss_port" --arg name "$ss_name" '{method:$method,password:$password,address:$address,port:$port,name:$name}'
)

broray_parse_shadowsocks()
{
    local ss_result
    BRORAY_METHOD=""; BRORAY_PASSWORD=""; BRORAY_ADDRESS=""; BRORAY_PORT=""; BRORAY_NAME=""
    ss_result="$(broray_shadowsocks_uri_json "$1")" || broray_die "Shadowsocks: импорт ссылки отклонён"
    BRORAY_METHOD="$(printf '%s' "$ss_result" | jq -er '.method')" || return 1
    BRORAY_PASSWORD="$(printf '%s' "$ss_result" | jq -er '.password')" || return 1
    BRORAY_ADDRESS="$(printf '%s' "$ss_result" | jq -er '.address')" || return 1
    BRORAY_PORT="$(printf '%s' "$ss_result" | jq -er '.port')" || return 1
    BRORAY_NAME="$(printf '%s' "$ss_result" | jq -er '.name')" || return 1
    BRORAY_PROTOCOL="shadowsocks"; BRORAY_NETWORK="raw"; BRORAY_SECURITY="none"
}
