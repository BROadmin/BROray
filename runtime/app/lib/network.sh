#!/opt/bin/ash
# LAN-01: preserve an admitted address, never pick a random private segment.
BRORAY_NETWORK_ROOT="${BRORAY_NETWORK_ROOT:-${BRORAY_ROOT:-/opt/broray}}"
broray_network_ipv4_valid() {
 printf '%s\n' "$1" | awk -F. '
 {if(NR!=1 || NF!=4){bad=1;next}
 for(i=1;i<=4;i++)if($i!~/^(0|[1-9][0-9]*)$/ || length($i)>3 || $i>255)bad=1
 if(!($1==10 || ($1==172 && $2>=16 && $2<=31) || ($1==192 && $2==168)))bad=1}
 END{exit (NR==1&&!bad)?0:1}'
}
broray_network_diagnostic() {
 printf 'BRORAY_LAN_DIAG code=%s configured_private=%s live_ipv4=%s matches=%s\n' "$1" "$2" "$3" "$4" >&2
}
broray_network_read() (
 # Bounds apply to command output as well as runtime. No raw configuration logs.
 ulimit -f 2048 || exit 1
 if command -v timeout >/dev/null 2>&1; then timeout -k 2 8 "$@"
 elif command -v busybox >/dev/null 2>&1; then busybox timeout -k 2 8 "$@"
 else exit 127; fi
)
broray_network_safe_file() {
 [ -f "$1" ] && [ ! -L "$1" ] && [ "$(wc -c <"$1")" -le 65536 ]
}
broray_network_matches() {
 # Both snapshots must succeed. Duplicate configured or live IPs are excluded.
 awk -v display="${3:-address}" '
 function valid(v,p,i){if(v!~/^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$/)return 0
 split(v,p,".");for(i=1;i<=4;i++)if(p[i]!~/^(0|[1-9][0-9]*)$/||length(p[i])>3||p[i]>255)return 0
 return p[1]==10||(p[1]==172&&p[2]>=16&&p[2]<=31)||(p[1]==192&&p[2]==168)}
 function flush(i){if(iface!="")for(i=1;i<=naddr;i++){
   count[addr[i]]++;if(sec==1&&priv==1){trusted[addr[i]]++;names[addr[i]]=iface}}
 iface="";sec=0;priv=0;naddr=0}
 FILENAME==ARGV[1]{raw=$0;sub(/\r$/,"",raw);line=raw;sub(/^[[:space:]]+/,"",line);n=split(line,f,/[[:space:]]+/)
 if(raw!~/^[[:space:]]/){flush();if(n==2&&f[1]=="interface"&&f[2]~/^[A-Za-z][A-Za-z0-9_.\/-]*$/)iface=f[2];next}
 if(iface=="")next
 if(f[1]=="security-level"){sec++;if(n==2&&f[2]=="private")priv++;next}
 if(n>=3&&f[1]=="ip"&&f[2]=="address"&&valid(f[3]))addr[++naddr]=f[3]
 next}
 FILENAME==ARGV[2]{if(!flushed){flush();flushed=1}
 if($1=="inet"){v=$2;sub(/\/.*/,"",v);if(valid(v))live[v]++}next}
 END{if(!flushed)flush();for(v in trusted)if(trusted[v]==1&&count[v]==1&&live[v]==1){if(display=="menu")print v "\t" names[v];else print v}}
 ' "$1" "$2"
}
broray_network_select() (
 # Subshell contains traps and scratch, and cannot clobber caller job traps.
 mode="${1:-transport}";settings="$BRORAY_NETWORK_ROOT/config/system/settings.json"
 root="${BRORAY_NETWORK_TMP_ROOT:-$BRORAY_NETWORK_ROOT/tmp}"
 [ -d "$root" ] && [ ! -L "$root" ] || { broray_network_diagnostic SCRATCH_ROOT_UNSAFE unknown unknown unknown;exit 1; }
 work="$(mktemp -d "$root/broray-network.XXXXXXXXXX")" || exit 1
 trap 'rm -f "$work/running" "$work/live" "$work/error" "$work/matches" "$work/settings" "$work/menu" "$work/menu-unsorted"; rmdir "$work" 2>/dev/null || true' EXIT
 trap 'exit 129' HUP;trap 'exit 130' INT;trap 'exit 143' TERM
 for tool in ip jq awk;do command -v "$tool" >/dev/null 2>&1 || { broray_network_diagnostic SNAPSHOT_TOOLS_UNAVAILABLE unknown unknown unknown;exit 1; };done
 broray_network_read "$BRORAY_NETWORK_ROOT/bin/broray-system-ndmc" -c 'show running-config' >"$work/running" 2>"$work/error" &&
 [ ! -s "$work/error" ] && [ -s "$work/running" ] || { broray_network_diagnostic RUNNING_SNAPSHOT_FAILED unknown unknown unknown;exit 1; }
 broray_network_read ip -4 addr show >"$work/live" 2>"$work/error" &&
 [ ! -s "$work/error" ] && [ -s "$work/live" ] || { broray_network_diagnostic LIVE_SNAPSHOT_FAILED unknown unknown unknown;exit 1; }
 broray_network_matches "$work/running" "$work/live" >"$work/matches" || exit 1
 count="$(wc -l <"$work/matches" | tr -d ' ')"
 fail() { broray_network_diagnostic "$1" unknown unknown "$count";exit 1;}
 admitted() { broray_network_ipv4_valid "$1" && grep -Fxq "$1" "$work/matches";}
 printf '{}\n' >"$work/settings"
 if [ -e "$settings" ] || [ -L "$settings" ];then
  broray_network_safe_file "$settings" && jq -es 'length==1 and (.[0]|type)=="object"' "$settings" >/dev/null 2>&1 || fail SETTINGS_INVALID
  cp "$settings" "$work/settings" || fail SETTINGS_INVALID
 fi
 pin="${BRORAY_LAN_IP_OVERRIDE:-}"
 if [ "$mode" = webui ];then
  jq -e '(has("webuiLanAddress")|not) or (.webuiLanAddress|type)=="string"' "$work/settings" >/dev/null || fail WEBUI_PIN_INVALID
  saved_pin="$(jq -r '.webuiLanAddress // ""' "$work/settings")" || fail WEBUI_PIN_INVALID
  # Two explicit and contradictory choices must not silently overwrite one another.
  [ -z "$pin" ] || [ -z "$saved_pin" ] || [ "$pin" = "$saved_pin" ] || fail EXPLICIT_PIN_CONFLICT
  [ -n "$pin" ] || pin="$saved_pin"
 fi
 if [ -n "$pin" ];then
  admitted "$pin" || fail PIN_NOT_PRIVATE_OR_LIVE
  printf '%s\n' "$pin";exit 0
 fi
 # A validated remembered WebUI bind has priority only for the WebUI entrypoint.
 if [ "$mode" = webui ];then
  conf="${BRORAY_WEB_CONF:-$BRORAY_NETWORK_ROOT/config/lighttpd.conf}"
  if [ -L "$conf" ];then fail WEBUI_CONFIG_UNSAFE;fi
  if broray_network_safe_file "$conf";then
   old="$(awk '/^server\.bind[[:space:]]*=/ {n++;line=$0;sub(/\r$/,"",line);if(line~/^server\.bind[[:space:]]*=[[:space:]]*"[0-9.]+"[[:space:]]*$/){sub(/^[^"]*"/,"",line);sub(/".*$/,"",line);v=line}}
     END{if(n==1&&v!="")print v}' "$conf")"
   if [ -n "$old" ] && admitted "$old";then printf '%s\n' "$old";exit 0;fi
  fi
 fi
 old="$(jq -r '.listenAddress | if type=="string" then . else "" end' "$work/settings")"
 if [ -n "$old" ] && admitted "$old";then printf '%s\n' "$old";exit 0;fi
 if [ "$mode" = install ] && [ "$count" -gt 1 ];then
  # Only an explicit installation call may ask. Read /dev/tty, never the
  # install script's stdin (which can be a curl pipe or /dev/null).
  if ! ( : <>/dev/tty ) 2>/dev/null;then
   printf '%s\n' 'Найдено несколько LAN-адресов. Запустите установку в SSH-терминале или задайте BRORAY_LAN_IP_OVERRIDE=<LAN-IP> для команды установки.' >&2
   fail LAN_SELECTION_REQUIRED
  fi
  exec 3<>/dev/tty
  [ -t 3 ] || fail LAN_SELECTION_REQUIRED
  broray_network_matches "$work/running" "$work/live" menu >"$work/menu-unsorted" || exit 1
  # Entware's sort may ignore -o and write to stdout. Use shell redirection
  # so menu rows can never contaminate the function's single-address result.
  LC_ALL=C sort <"$work/menu-unsorted" >"$work/menu" || exit 1
  printf '\n%s\n' 'Выберите LAN-адрес BROray для WebUI и локального SOCKS:' >&3
  awk -F '\t' '{printf "  %d) %s — %s\n",NR,$1,$2}' "$work/menu" >&3
  printf '%s\n' '  0) Отменить установку' >&3
  while :;do
   printf 'Номер [0-%s]: ' "$count" >&3
   IFS= read -r choice <&3 || fail LAN_SELECTION_CANCELLED
   case "$choice" in
    0) fail LAN_SELECTION_CANCELLED ;;
    ''|*[!0-9]*) selected='' ;;
    *) selected="$(awk -F '\t' -v n="$choice" 'n==sprintf("%d",NR){print $1}' "$work/menu")" ;;
   esac
   [ -n "$selected" ] && break
   printf '%s\n' 'Введите номер из списка или 0 для отмены.' >&3
  done
  exec 3>&-
  # The choice may take minutes: re-read both live snapshots before accepting
  # it. A removed/reclassified address must never reach settings or services.
  BRORAY_LAN_IP_OVERRIDE="$selected" broray_network_select transport
  exit $?
 fi
 [ "$count" = 1 ] || fail LAN_SELECTION_REQUIRED
 sed -n '1p' "$work/matches"
)
broray_detect_lan_ip() { broray_network_select transport;}
broray_detect_webui_lan_ip() { broray_network_select webui;}
broray_network_store_lan_ip() {
 local value temporary run
 value="$1";run="$BRORAY_NETWORK_ROOT/run"
 broray_network_ipv4_valid "$value" || return 1
 [ ! -L "$run" ] && { [ ! -e "$run" ] || [ -d "$run" ]; } || return 1
 mkdir -p "$run" || return 1
 [ ! -L "$run/lan-ip" ] && { [ ! -e "$run/lan-ip" ] || [ -f "$run/lan-ip" ]; } || return 1
 temporary="$(mktemp "$run/.lan-ip.XXXXXXXXXX")" || return 1
 printf '%s\n' "$value" >"$temporary" && chmod 600 "$temporary" && mv -f "$temporary" "$run/lan-ip" || { rm -f "$temporary";return 1; }
}
broray_save_lan_ip() {
 local lan_ip
 case "${1:-transport}" in
  transport|install) lan_ip="$(broray_network_select "${1:-transport}")" || return 1 ;;
  *) return 1 ;;
 esac
 broray_network_store_lan_ip "$lan_ip" || return 1
 printf '%s\n' "$lan_ip"
}
