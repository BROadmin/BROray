#!/opt/bin/ash
# Inert helpers: no requests or state changes on load.
broray_subscription_user_agent_valid() {
 printf '%s' "$1" | jq -Rse 'length<=128 and all(explode[]; .>=32 and .<=126)' >/dev/null 2>&1
}
broray_subscription_default_user_agent() {
 local version
 version="$(jq -er '.version | select(type=="string" and length>0 and length<=64)' "$BRORAY_BASE/share/release/manifest.json" 2>/dev/null)" || version=''
 [ -n "$version" ] || version="$(sed -n '1p' "$BRORAY_BASE/config/version" 2>/dev/null)"
 case "$version" in ''|*[!a-zA-Z0-9.+_-]*) version=unknown ;; esac
 [ "${#version}" -le 64 ] || version=unknown
 printf 'BROray/%s\n' "$version"
}
broray_subscription_header_lines() {
 awk '{ sub(/\r$/, "") }
 /^HTTP\/[0-9.]+[ \t]+[0-9][0-9][0-9]([ \t]|$)/ { block=""; active=1; next }
 active && $0=="" { active=0; next }
 active && /^[^ \t:]+:/ { block=block $0 "\n" }
 END { printf "%s", block }' "$1"
}
broray_subscription_header_value() {
 broray_subscription_header_lines "$1" | awk -v wanted="$2" '
 { name=$0; sub(/:.*/, "", name); if(tolower(name)!=tolower(wanted)) next
 value=$0; sub(/^[^:]*:[ \t]*/, "", value); sub(/[ \t]+$/, "", value)
 if(seen && value!=result) conflict=1; result=value; seen=1 }
 END { if(conflict) exit 1; if(seen) print result }'
}
broray_subscription_header_true() {
 local value; value="$(broray_subscription_header_value "$1" "$2")" || return 1
 [ "$(printf '%s' "$value" | tr 'A-Z' 'a-z')" = true ]
}
broray_subscription_http_denial() {
 local flag
 for flag in x-hwid-max-devices-reached x-hwid-limit x-hwid-not-supported; do
  if ! broray_subscription_header_value "$1" "$flag" >/dev/null; then
   broray_subscription_set_error HTTP_ERROR 'Провайдер вернул противоречивые служебные заголовки.'; return 0
  fi
 done
 if broray_subscription_header_true "$1" x-hwid-max-devices-reached || broray_subscription_header_true "$1" x-hwid-limit; then
  broray_subscription_set_error SUBSCRIPTION_DEVICE_LIMIT_REACHED 'Провайдер отклонил подписку: достигнут лимит зарегистрированных устройств.'; return 0
 fi
 if broray_subscription_header_true "$1" x-hwid-not-supported; then
  broray_subscription_set_error SUBSCRIPTION_DEVICE_ID_REJECTED 'Провайдер не принял анонимный идентификатор клиента подписки.'; return 0
 fi
 return 1
}
# Before curl 8.20, --max-filesize does not bound decompressed output.
broray_subscription_compression_safe() {
 curl --disable --version 2>/dev/null | awk 'NR==1 && $1=="curl" { split($2,v,"."); ok=(v[1]+0>8 || (v[1]+0==8 && v[2]+0>=20)) } END { exit !ok }'
}
broray_subscription_metadata_collect() {
 local mode input destination records old temporary rc
 mode="$1"; input="$2"; destination="${BRORAY_SUB_PROVIDER_METADATA_FILE:-}"
 [ -n "$destination" ] || return 0
 [ -f "$input" ] && [ ! -L "$input" ] || return 1
 records="$BRORAY_SUB_TMP/subscription-meta-records.$$.json"; temporary="$destination.new.$$"; old="$destination"
 [ ! -L "$old" ] && [ ! -L "$temporary" ] && [ ! -L "$records" ] || return 1
 [ -f "$old" ] || printf '{"schemaVersion":1}\n' > "$old" || return 1
 { if [ "$mode" = http ]; then broray_subscription_header_lines "$input"; else cat "$input"; fi; } |
 awk 'BEGIN { n=split("profile-title subscription-userinfo profile-update-interval support-url profile-web-page-url announce announce-url routing routing-enable update-always custom-tunnel-config",a," "); for(i=1;i<=n;i++) known[a[i]]=1 }
 { line=$0; sub(/\r$/, "", line); sub(/^[ \t]*#?[ \t]*/, "", line); colon=index(line,":"); if(colon<2) next; name=tolower(substr(line,1,colon-1))
 if(!(name in known)) next; value=substr(line,colon+1); print name ":" substr(value,1,4097) }' |
 jq -Rsc 'split("\n")|map(select(length>0)|index(":") as $i|{name:.[0:$i],value:.[$i+1:]})' > "$records" || return 1
 if ! jq --slurpfile previous "$old" -f "$BRORAY_BASE/lib/subscription-metadata.jq" "$records" > "$temporary" 2>/dev/null; then rm -f "$records" "$temporary"; return 1; fi
 chmod 600 "$temporary" && mv "$temporary" "$destination"
 rc=$?; rm -f "$records" "$temporary"; return "$rc"
}
broray_subscription_strip_metadata() {
 awk 'BEGIN { n=split("profile-title subscription-userinfo profile-update-interval support-url profile-web-page-url announce announce-url routing routing-enable update-always custom-tunnel-config",a," "); for(i=1;i<=n;i++) known[a[i]]=1 }
 /^[ \t]*#/ { next }
 { name=$0; sub(/^[ \t]*/, "", name); sub(/:.*/, "", name); if(tolower(name) in known) next; print }' "$1" > "$2"
}
