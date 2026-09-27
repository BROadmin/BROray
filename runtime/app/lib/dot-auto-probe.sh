#!/opt/bin/ash
# Only private outputs. The owner publishes after native helpers-drain.
set -u
umask 077
[ "${BRORAY_OPS_SUPERVISED:-}" = ptrace/1 ] && [ "$#" = 1 ] || exit 73
work="$1"
if [ -n "${BRORAY_DOT_QUEUE_REQUEST:-}" ]; then
 case "$BRORAY_DOT_QUEUE_REQUEST" in q-*) ;; *) exit 73 ;; esac
 suffix="${BRORAY_DOT_QUEUE_REQUEST#q-}"
 case "$suffix" in *[!0-9a-f]*) exit 73 ;; esac
 [ "${#suffix}" = 32 ] || exit 73
 [ "$work" = "${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}/requests/$BRORAY_DOT_QUEUE_REQUEST/dot-probe-$BRORAY_BACKGROUND_OPERATION_ID" ] || exit 73
else
 case "$work" in "${BRORAY_ROOT:-/opt/broray}/tmp/dot-auto-"*) ;; *) exit 73 ;; esac
fi
[ -d "$work" ] && [ ! -L "$work" ] || exit 74
[ "$(cat "$work/operation-id")" = "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || exit 73
. "$BRORAY_ROOT/lib/routes-dot.sh" || exit 74
broray_dot_presets >"$work/catalog.json" || exit 74
broray_dot_validate_catalog_file "$work/catalog.json" || exit 74
jq -e --slurpfile catalog "$work/catalog.json" '
 .serverIds as $ids | ($ids|type)=="array" and ($ids|length)>0 and ($ids|length)<=8 and
  ($ids|unique|length)==($ids|length) and all($ids[];. as $id|any($catalog[0][];.id==$id))' "$work/request.json" >/dev/null || exit 74
if [ -n "${BRORAY_DOT_QUEUE_REQUEST:-}" ]; then
 jq -e '.serverIds|length==1' "$work/request.json" >/dev/null || exit 74
fi
jq -n --slurpfile req "$work/request.json" --slurpfile catalog "$work/catalog.json" \
 '[$req[0].serverIds[] as $id | $catalog[0][] | select(.id==$id)]' >"$work/expected.json" || exit 74
[ "$(jq -cS . "$work/entries.json")" = "$(jq -cS . "$work/expected.json")" ] || exit 74
openssl_bin="$(broray_dot_openssl_path)"; timeout_bin="$(broray_dot_timeout_path)"
[ -n "$timeout_bin" ] || exit 74
ulimit -f 128 || exit 74
jq -c '.[]' "$work/entries.json" >"$work/entries.jsonl" || exit 74
: >"$work/results.jsonl"
while IFS= read -r endpoint; do
 address="$(printf '%s' "$endpoint" | jq -r '.address')"
 port="$(printf '%s' "$endpoint" | jq -r '.effectivePort')"
 sni="$(printf '%s' "$endpoint" | jq -r '.sni')"
 start="$(date '+%s')"; rc=127
 if [ -n "$openssl_bin" ]; then
  rc=0
  "$timeout_bin" -k 2 12 "$openssl_bin" s_client -connect "$address:$port" -servername "$sni" \
    -verify_hostname "$sni" -verify_return_error -brief </dev/null >"$work/openssl.out" 2>&1 || rc=$?
 fi
 end="$(date '+%s')"; at="$(date '+%Y-%m-%dT%H:%M:%S%z')"; elapsed=$(((end-start)*1000))
 case "$rc" in 0) ok=true; status=ok; message='TLS-соединение и имя сертификата проверены.' ;;
 127) ok=false; status=unavailable; message='OpenSSL недоступен.' ;;
 *) ok=false; status=failed; message='TLS-проверка завершилась ошибкой.' ;; esac
 printf '%s' "$endpoint" | jq -c --argjson ok "$ok" --arg status "$status" --arg message "$message" \
 --arg at "$at" --argjson epoch "$end" --argjson latency "$elapsed" \
 '{id,address,effectivePort,sni,spki,interface,domain,ok:$ok,status:$status,message:$message,
 testedAt:$at,testedEpoch:$epoch,latencyMs:(if $latency>=0 then $latency else null end)}' >>"$work/results.jsonl" || exit 74
 rm -f "$work/openssl.out"
done <"$work/entries.jsonl"
jq -s '.' "$work/results.jsonl" >"$work/results.json" || exit 74
exit 0
