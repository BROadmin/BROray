#!/opt/bin/ash
# Read-only transport measurement through the existing runtime, under a supervisor.
set -u
[ "${BRORAY_OPS_SUPERVISED:-}" = ptrace/1 ] && [ "$#" = 2 ] || exit 73
umask 077
probe_config="$1"; probe_server="$2"
probe_endpoint=''; probe_attempts=0; probe_code=0
probe_emit() {
    jq -n --arg serverId "$probe_server" --arg status "$1" --arg errorCode "$2" \
      --arg endpoint "$probe_endpoint" --argjson attempts "$probe_attempts" \
      --argjson curlCode "$probe_code" --arg checkedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
      --argjson checkedEpoch "$(date '+%s')" \
      '{method:"current-socks-https",serverId:$serverId,status:$status,errorCode:$errorCode,
        endpoint:$endpoint,attempts:$attempts,curlCode:$curlCode,checkedAt:$checkedAt,checkedEpoch:$checkedEpoch}'
}
probe_unknown() { probe_emit unknown "$1"; exit 2; }
[ -f "$probe_config" ] && [ ! -L "$probe_config" ] || probe_unknown CONFIG_UNAVAILABLE
command -v curl >/dev/null 2>&1 || probe_unknown CURL_UNAVAILABLE
probe_listener="$(jq -ec '
  [.inbounds[]? | select(.protocol=="socks")] | select(length==1) | .[0] |
  select((.port|type)=="number" and .port>=1 and .port<=65535 and .port==(.port|floor)) |
  select((.settings.auth // "noauth")=="noauth") |
  {host:(.listen // "0.0.0.0"),port:.port} | select((.host|type)=="string")
  ' "$probe_config" 2>/dev/null)" || probe_unknown SOCKS_ENDPOINT_UNSUPPORTED
probe_host="$(printf '%s\n' "$probe_listener" | jq -r '.host')"
probe_port="$(printf '%s\n' "$probe_listener" | jq -r '.port')"
case "$probe_host" in
    0.0.0.0) probe_host=127.0.0.1 ;;
    ::) probe_host=::1 ;;
esac
# Numeric local addresses only; no DNS resolution of a proxy hostname.
case "$probe_host" in
    *:*)
        case "$probe_host" in *[!0-9a-fA-F:]*) probe_unknown SOCKS_ENDPOINT_UNSUPPORTED ;; esac
        probe_family=-6; probe_endpoint="[$probe_host]:$probe_port" ;;
    *)
        printf '%s\n' "$probe_host" | jq -Re 'split(".") | length==4 and all(.[];
          length>=1 and length<=3 and (explode|all(.[];.>=48 and .<=57)) and
          (tonumber>=0 and tonumber<=255))' >/dev/null 2>&1 || probe_unknown SOCKS_ENDPOINT_UNSUPPORTED
        probe_family=-4; probe_endpoint="$probe_host:$probe_port" ;;
esac
probe_addresses="$(ip "$probe_family" addr show 2>/dev/null)" || probe_unknown LOCAL_ADDRESS_UNAVAILABLE
printf '%s\n' "$probe_addresses" | awk -v host="$probe_host" '
  $1=="inet" || $1=="inet6" {v=$2;sub(/\/.*/,"",v);if(v==host)n++}
  END {exit n==1?0:1}' || probe_unknown SOCKS_ADDRESS_NOT_LOCAL

# One completed sample: the first successful endpoint wins; both must fail
# for an unhealthy result. -q and --noproxy prevent config/env bypass of SOCKS.
for probe_url in https://cp.cloudflare.com/generate_204 https://www.gstatic.com/generate_204; do
    probe_attempts=$((probe_attempts + 1)); probe_code=0
    probe_http="$(curl -q --silent --show-error --noproxy '' \
      --proxy "socks5h://$probe_endpoint" --proto '=https' \
      --connect-timeout 3 --max-time 6 --max-filesize 4096 \
      --output /dev/null --write-out '%{http_code}' "$probe_url" 2>/dev/null)" || probe_code=$?
    if [ "$probe_code" = 0 ] && [ "$probe_http" = 204 ]; then
        probe_emit healthy ''; exit 0
    fi
done
probe_emit unhealthy PROXY_HTTPS_FAILED
exit 1
