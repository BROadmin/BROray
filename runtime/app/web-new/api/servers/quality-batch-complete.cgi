#!/opt/bin/ash

. /opt/broray/web-new/api/servers/common.sh

broray_api_require_method POST
broray_api_require_session
broray_servers_api_lock quality-batch-complete

snapshot_cli=/opt/broray/bin/broray-home-snapshot
snapshot_file=/opt/broray/run/home-snapshots/servers.json

[ -x "$snapshot_cli" ] ||
    broray_api_error \
        "500 Internal Server Error" \
        "HOME_SNAPSHOT_UNAVAILABLE" \
        "Модуль итогового состояния серверов недоступен."

if ! "$snapshot_cli" refresh servers >/dev/null 2>&1; then
    broray_api_error \
        "500 Internal Server Error" \
        "HOME_SNAPSHOT_REFRESH_FAILED" \
        "Проверка серверов завершена, но итоговое состояние для Главной не опубликовано."
fi

jq -e '
  type == "object" and
  .module == "servers" and
  (.capturedAt | type) == "string" and
  (.data | type) == "object" and
  (.data.servers | type) == "array"
' \
    "$snapshot_file" >/dev/null 2>&1 ||
    broray_api_error \
        "500 Internal Server Error" \
        "HOME_SNAPSHOT_INVALID" \
        "Итоговое состояние серверов для Главной не прошло проверку."

broray_api_success "$(
    jq -nc \
        --argjson total "$(jq -r '.data.servers | length' "$snapshot_file")" \
        --arg capturedAt "$(jq -r '.capturedAt // empty' "$snapshot_file")" \
        '{published:true,totalServers:$total,capturedAt:(if $capturedAt == "" then null else $capturedAt end)}'
)"
