#!/opt/bin/ash
# BROray-01: an authenticated operations POST may arrive through KeenDNS,
# which can replace Host. Never derive additional trust from forwarded headers.

# Parse one serialized HTTP(S) origin, not a URL. No paths, credentials,
# escapes, lists or whitespace. Default ports are normalized; IPv6 is accepted
# in bracketed hexadecimal form. The result is safe for exact string equality.
broray_operations_normalize_origin()
{
    [ "${#1}" -le 320 ] || return 1
    printf '%s\n' "$1" | LC_ALL=C awk '
      function reject() {bad=1; exit}
      NR != 1 {reject()}
      {
        if ($0 ~ /^http:\/\//) {scheme="http"; a=substr($0,8); default_port=80}
        else if ($0 ~ /^https:\/\//) {scheme="https"; a=substr($0,9); default_port=443}
        else reject()
        if (a=="" || a ~ /[^A-Za-z0-9.:\[\]-]/) reject()
        port=""
        if (substr(a,1,1)=="[") {
          end=index(a,"]"); if (end<4) reject()
          host=tolower(substr(a,1,end)); ip=substr(host,2,length(host)-2)
          if (ip ~ /[^0-9a-f:]/ || index(ip,":")==0 || index(ip,":::")>0) reject()
          compressed=index(ip,"::")>0
          if ((substr(ip,1,1)==":" && substr(ip,1,2)!="::") ||
              (substr(ip,length(ip),1)==":" && substr(ip,length(ip)-1)!="::")) reject()
          temp=ip; if (gsub(/::/,"@",temp)>1) reject()
          n=split(ip,parts,":"); groups=0
          for(i=1;i<=n;i++) {
            if(length(parts[i])>4) reject()
            if(parts[i]!="") groups++
          }
          if ((compressed && groups>=8) || (!compressed && groups!=8)) reject()
          suffix=substr(a,end+1)
          if(suffix!="") {if(substr(suffix,1,1)!=":") reject(); port=substr(suffix,2); if(port=="") reject()}
        } else {
          n=split(a,parts,":"); if(n>2) reject()
          host=tolower(parts[1]); if(n==2) {port=parts[2]; if(port=="") reject()}
          if(length(host)<1 || length(host)>253) reject()
          n=split(host,labels,".")
          for(i=1;i<=n;i++) {
            if(length(labels[i])<1 || length(labels[i])>63) reject()
            if(labels[i] !~ /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/) reject()
          }
        }
        if(port!="") {
          if(port !~ /^[1-9][0-9]*$/ || length(port)>5 || port+0>65535) reject()
          if(port+0==default_port) port=""
        }
        result=scheme "://" host (port=="" ? "" : ":" port)
      }
      END {if(bad || NR!=1 || result=="") exit 1; print result}
    '
}

# Reuse the existing exact live publication/receipt validation. The subshell
# isolates its variables and traps from the operations API. Only the two
# existing read commands are dispatched, each with a bounded timeout.
broray_operations_read_web_publication()
(
    umask 077
    [ -f /opt/broray/lib/web-publish.sh ] &&
    [ ! -L /opt/broray/lib/web-publish.sh ] || exit 1
    command -v timeout >/dev/null 2>&1 || exit 1
    [ -x /opt/broray/bin/broray-system-ndmc ] || exit 1
    command -v jq >/dev/null 2>&1 || exit 1
    BRORAY_WEB_PUBLISH_ROOT=/opt/broray
    BRORAY_WEB_PUBLISH_NAME=broray
    BRORAY_WEB_PUBLISH_PORT=8080
    BRORAY_WEB_PUBLISH_OWNER=/opt/broray/config/web-publish.json
    . /opt/broray/lib/web-publish.sh || exit 1
    broray_web_publish_ndmc()
    {
        [ "$#" = 1 ] || return 126
        case "$1" in 'show ndns'|'show running-config') ;; *) return 126 ;; esac
        # A large running-config takes longer while supervised workers run.
        # Keep the small identity read bounded separately; trust checks stay exact.
        case "$1" in
            'show ndns') timeout -k 1 3 /opt/broray/bin/broray-system-ndmc -c "$1" ;;
            'show running-config') timeout -k 1 30 /opt/broray/bin/broray-system-ndmc -c "$1" ;;
        esac
    }
    # Duplicate identity fields cannot establish a unique public origin.
    broray_web_publish_ndns_field()
    {
        LC_ALL=C awk -v wanted="$1" '
          {line=$0; sub(/\r$/,"",line); key=line; sub(/:.*/,"",key)
           sub(/^[[:space:]]*/,"",key); sub(/[[:space:]]*$/,"",key)
           if(key==wanted) {count++; value=line; sub(/^[^:]*:/,"",value)
             sub(/^[[:space:]]*/,"",value); sub(/[[:space:]]*$/,"",value)}}
          END {if(count!=1) exit 1; print value}
        ' "$2"
    }
    # Do not reuse a stale or pre-created status workspace. Retire only files
    # created by the read-only status function, including after an interrupted read.
    [ -d /opt/broray/tmp ] && [ ! -L /opt/broray/tmp ] || exit 1
    origin_workspace="/opt/broray/tmp/web-publish-status.$$"
    mkdir "$origin_workspace" || exit 1
    trap 'rm -f "$origin_workspace/running-config" "$origin_workspace/running-config.err" "$origin_workspace/ndns" "$origin_workspace/ndns.err"; rmdir "$origin_workspace" 2>/dev/null || true' EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    broray_web_publish_status_json
)

broray_operations_keendns_origin()
{
    local publication origin normalized
    publication="$(broray_operations_read_web_publication 2>/dev/null)" || return 1
    origin="$(printf '%s\n' "$publication" | jq -ers '
      if length!=1 then error("ambiguous publication") else .[0] end |
      select(type=="object" and .schemaVersion==1 and .state=="enabled" and
        .enabled==true and .consistent==true and .recoveryRequired==false and
        .keenDns.available==true and .ownership.liveBlockPresent==true and
        .ownership.receiptPresent==true and .ownership.receiptValid==true and
        .ownership.liveBlockOwnedExact==true) |
      select((.keenDns.name|type)=="string" and (.keenDns.domain|type)=="string") |
      # Entware jq can be built without Oniguruma. The origin normalizer
      # validates ASCII labels; keep this filter free of regex functions.
      select((.keenDns.name|length)>0 and (.keenDns.name|length)<=63 and
        (.keenDns.name|contains(".")|not)) |
      select(.keenDns.domain|contains(".")) |
      ("https://broray." + .keenDns.name + "." + .keenDns.domain) as $expected |
      select(.address==($expected + "/")) | $expected
    ' 2>/dev/null)" || return 1
    # The configured name/domain cannot contain a port, even a default one.
    # Browser origins may include :443, but that rule does not apply to domains.
    case "${origin#https://}" in *:*) return 1 ;; esac
    normalized="$(broray_operations_normalize_origin "$origin")" || return 1
    case "$normalized" in https://broray.*) ;; *) return 1 ;; esac
    printf '%s\n' "$normalized"
}

broray_operations_origin_allowed()
{
    local origin host_http host_https published page_origin proxy_origin
    [ -n "${HTTP_HOST:-}" ] || return 1
    proxy_origin=false
    if [ -n "${HTTP_ORIGIN:-}" ]; then
        origin="$(broray_operations_normalize_origin "$HTTP_ORIGIN")" || return 1
        if [ -n "${HTTP_X_BRORAY_ORIGIN:-}" ]; then
            page_origin="$(broray_operations_normalize_origin "$HTTP_X_BRORAY_ORIGIN")" || return 1
            [ "$origin" = "$page_origin" ] || return 1
        fi
    else
        # KeenDNS can remove Origin. The authenticated UI sends its page origin
        # in a non-simple header; cross-origin browsers require a denied CORS
        # preflight. Accept this fallback only with exact live KeenDNS proof.
        [ "${HTTP_X_BRORAY_REQUEST:-}" = operations ] || return 1
        origin="$(broray_operations_normalize_origin "${HTTP_X_BRORAY_ORIGIN:-}")" || return 1
        proxy_origin=true
    fi
    host_http="$(broray_operations_normalize_origin "http://$HTTP_HOST")" || return 1
    host_https="$(broray_operations_normalize_origin "https://$HTTP_HOST")" || return 1
    # Keep the existing same-host HTTP/HTTPS contract. Local recovery does not
    # depend on ndmc, an external name, or the availability of KeenDNS.
    if [ "$proxy_origin" = false ] &&
       { [ "$origin" = "$host_http" ] || [ "$origin" = "$host_https" ]; }; then
        return 0
    fi
    case "$origin" in https://broray.*) ;; *) return 1 ;; esac
    published="$(broray_operations_keendns_origin)" || return 1
    [ "$origin" = "$published" ]
}
