#!/opt/bin/ash

# Fail-closed Keenetic configuration comparator based on physical Keenetic
# evidence BRK-CMD-021 and BRK-CMD-027. It normalizes only the exact record-1
# system-header allowlist, the two exact volatile metadata records and, when
# explicitly requested, one exact BROray ProxyN block together with its exact
# following `!` boundary record.

broray_config_regular_file()
{
    [ -f "${1:-}" ] && [ ! -L "${1:-}" ]
}

broray_config_canonicalize_metadata()
{
    local source destination

    source="$1"
    destination="$2"
    broray_config_regular_file "$source" || return 1

    awk '
      function digits(value, expected,    i, c) {
        if (length(value) != expected) return 0
        for (i=1; i<=length(value); i++) {
          c=substr(value,i,1)
          if (c !~ /^[0-9]$/) return 0
        }
        return 1
      }
      function lower_hex(value, expected,    i, c) {
        if (length(value) != expected) return 0
        for (i=1; i<=length(value); i++) {
          c=substr(value,i,1)
          if (c !~ /^[0-9a-f]$/) return 0
        }
        return 1
      }
      function weekday(value) {
        return value=="Sun," || value=="Mon," || value=="Tue," ||
               value=="Wed," || value=="Thu," || value=="Fri," || value=="Sat,"
      }
      function month(value) {
        return value=="Jan" || value=="Feb" || value=="Mar" || value=="Apr" ||
               value=="May" || value=="Jun" || value=="Jul" || value=="Aug" ||
               value=="Sep" || value=="Oct" || value=="Nov" || value=="Dec"
      }
      function printable_ascii(value,    i, c) {
        for (i=1; i<=length(value); i++) {
          c=substr(value,i,1)
          if (c !~ /^[ -~]$/) return 0
        }
        return 1
      }
      function valid_system_header(value,    size) {
        size=length(value)
        if (size<2 || size>160 || !printable_ascii(value) ||
            index(value,"\t")!=0 || index(value,"\r")!=0 ||
            index(tolower(value),"broray")!=0) return 0
        if (value=="! System configuration" ||
            value=="! Running configuration" ||
            value=="! Startup configuration" ||
            value=="! Current configuration" ||
            value=="! Saved configuration" ||
            value=="! System configuration: running" ||
            value=="! System configuration (running)" ||
            value=="! System configuration: startup" ||
            value=="! System configuration (startup)") return 1
        if (substr(value,1,13)=="! $$$ Model: " && size>13) return 1
        if (substr(value,1,15)=="! $$$ Version: " && size>15) return 1
        if (substr(value,1,13)=="! $$$ Agent: " && size>13) return 1
        return 0
      }
      function valid_time(value,    part, count) {
        count=split(value,part,":")
        return count==3 && digits(part[1],2) && digits(part[2],2) && digits(part[3],2) &&
               (part[1]+0)<=23 && (part[2]+0)<=59 && (part[3]+0)<=59
      }
      function valid_last_change(value,    field, count) {
        if (length(value)!=48 || substr(value,1,19)!="! $$$ Last change: ") return 0
        if (index(value,"\t")!=0) return 0
        count=split(value,field,/[ ]+/)
        return count==10 && field[1]=="!" && field[2]=="$$$" &&
               field[3]=="Last" && field[4]=="change:" && weekday(field[5]) &&
               digits(field[6],2) && (field[6]+0)>=1 && (field[6]+0)<=31 &&
               month(field[7]) && digits(field[8],4) && valid_time(field[9]) &&
               field[10]=="GMT"
      }
      function valid_md5(value,    prefix, payload) {
        prefix="! $$$ Md5 checksum: "
        if (length(value)!=52 || substr(value,1,length(prefix))!=prefix) return 0
        payload=substr(value,length(prefix)+1)
        return lower_hex(payload,32)
      }
      {
        if (index($0,"\r")!=0) invalid=1
        if ($0!="!") content_index++
        if (valid_system_header($0)) system_header_total++
        if (valid_last_change($0)) last_total++
        if (valid_md5($0)) md5_total++
        if (NR==1) {
          if (!valid_system_header($0)) invalid=1
          print "! $$$ System header: canonical"
          next
        }
        if ($0!="!" && content_index==2) {
          if (!valid_last_change($0)) invalid=1
          print "! $$$ Last change: Xxx, 00 Xxx 0000 00:00:00 GMT"
          next
        }
        if ($0!="!" && content_index==3) {
          if (!valid_md5($0)) invalid=1
          print "! $$$ Md5 checksum: 00000000000000000000000000000000"
          next
        }
        print $0
      }
      END {
        if (NR<3 || invalid || system_header_total!=1 ||
            last_total!=1 || md5_total!=1) exit 1
      }
    ' "$source" >"$destination"
}

broray_config_canonical_sha256()
{
    local source canonical digest

    source="$1"
    canonical="$(mktemp "${TMPDIR:-/tmp}/broray-config-canonical.XXXXXX")" || return 1
    broray_config_canonicalize_metadata "$source" "$canonical" || {
        rm -f "$canonical"
        return 1
    }
    digest="$(sha256sum "$canonical" | awk 'NR==1{print $1;exit}')" || {
        rm -f "$canonical"
        return 1
    }
    rm -f "$canonical"
    case "$digest" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#digest}" -eq 64 ] || return 1
    printf '%s\n' "$digest"
}

broray_config_remove_exact_proxy_boundary()
{
    local source destination name host port description

    source="$1"
    destination="$2"
    name="$3"
    host="$4"
    port="$5"
    description="$6"
    broray_config_regular_file "$source" || return 1

    awk -v parent="interface $name" \
      -v description_line="    description $description" \
      -v upstream_line="    proxy upstream $host $port" '
      function expected(position, value) {
        if (position==1) return value==parent
        if (position==2) return value==description_line
        if (position==3) return value=="    security-level public"
        if (position==4) return value=="    proxy protocol socks5"
        if (position==5) return value==upstream_line
        if (position==6) return value=="    up"
        return 0
      }
      {
        raw=$0
        clean=raw
        sub(/^[ \t]*/,"",clean)
        sub(/[ \t]*$/,"",clean)
        top_level=(raw !~ /^[ \t]/)

        if (!inside && clean==parent) {
          parents++
          inside=1
          block_index=1
          if (!expected(block_index,raw)) invalid=1
          next
        }
        if (inside) {
          if (top_level) {
            if (block_index!=6 || raw!="!") invalid=1
            else boundaries++
            inside=0
            next
          }
          block_index++
          if (!expected(block_index,raw)) invalid=1
          next
        }
        print raw
      }
      END {
        if (inside || invalid || parents!=1 || boundaries!=1) exit 1
      }
    ' "$source" >"$destination"
}

broray_config_without_exact_proxy_sha256()
{
    local source name host port description reduced digest

    source="$1"
    name="$2"
    host="$3"
    port="$4"
    description="$5"
    reduced="$(mktemp "${TMPDIR:-/tmp}/broray-config-without-proxy.XXXXXX")" || return 1
    broray_config_remove_exact_proxy_boundary \
        "$source" "$reduced" "$name" "$host" "$port" "$description" || {
        rm -f "$reduced"
        return 1
    }
    digest="$(broray_config_canonical_sha256 "$reduced")" || {
        rm -f "$reduced"
        return 1
    }
    rm -f "$reduced"
    printf '%s\n' "$digest"
}
