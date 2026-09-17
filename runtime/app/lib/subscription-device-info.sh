#!/opt/bin/ash
# Optional show-version reader. Inert on load; never changes router configuration.
broray_subscription_device_snapshot() (
    umask 077
    command -v timeout >/dev/null 2>&1 || { printf '{}\n'; exit 0; }
    device_ndmc=''
    if command -v broray_interface_ndmc_path >/dev/null 2>&1; then
        device_ndmc="$(broray_interface_ndmc_path 2>/dev/null)" || device_ndmc=''
    else
        device_ndmc="$(command -v ndmc 2>/dev/null)" || device_ndmc=''
    fi
    [ -n "$device_ndmc" ] && [ -x "$device_ndmc" ] || { printf '{}\n'; exit 0; }
    device_tmp="$(mktemp -d "$BRORAY_SUB_TMP/device-info.XXXXXX" 2>/dev/null)" || { printf '{}\n'; exit 0; }
    trap 'rm -rf "$device_tmp"' EXIT
    # File-size limit and timeout are scoped to this read-only child, not the owner.
    if ! (ulimit -f 16 || exit 1; timeout -k 1 2 "$device_ndmc" -c 'show version' >"$device_tmp/raw" 2>/dev/null) 2>/dev/null; then
        printf '{}\n'; exit 0
    fi
    device_bytes="$(wc -c <"$device_tmp/raw" | tr -d ' ')"
    case "$device_bytes" in ''|*[!0-9]*) printf '{}\n'; exit 0 ;; esac
    [ "$device_bytes" -le 16384 ] || { printf '{}\n'; exit 0; }
    if jq -e 'type=="object"' "$device_tmp/raw" >/dev/null 2>&1; then
        cp "$device_tmp/raw" "$device_tmp/input" || { printf '{}\n'; exit 0; }
    else
        LC_ALL=C awk '
        { line=$0; sub(/\r$/, "", line); sub(/^[ \t]+/, "", line); colon=index(line, ":"); if(colon<2) next;
          key=substr(line,1,colon-1); if(key!="hw_id" && key!="model" && key!="title" && key!="release" && key!="vendor" && key!="manufacturer") next;
          value=substr(line,colon+1); sub(/^[ \t]+/, "", value); print key ":" value }
        ' "$device_tmp/raw" | jq -Rsc 'split("\n")|map(select(length>0)|index(":") as $i|{key:.[0:$i],value:.[$i+1:]})|group_by(.key)|map(select(map(.value)|unique|length==1)|.[0])|from_entries' >"$device_tmp/input" || { printf '{}\n'; exit 0; }
    fi
    if ! jq -sc 'if length==1 and (.[0]|type)=="object" then .[0] else {} end' "$device_tmp/input" |
        jq -c -f "$BRORAY_BASE/lib/subscription-device-info.jq" >"$device_tmp/result" 2>/dev/null; then
        printf '{}\n'; exit 0
    fi
    cat "$device_tmp/result"
)
broray_subscription_device_info() (
    device_info="$(broray_subscription_device_snapshot)" || device_info='{}'
    device_version="$(broray_subscription_default_user_agent)" || device_version='BROray/unknown'
    device_version="${device_version#BROray/}"
    [ "$device_version" != unknown ] || device_version=''
    case "$device_version" in *[!a-zA-Z0-9.+_-]*) device_version='' ;; esac
    printf '%s' "$device_info" | jq -c --arg app "$device_version" '
      if type!="object" then {} else . end |
      if ($app|length)>0 and ($app|length)<=64 then . + {appVersion:$app} else . end'
)
