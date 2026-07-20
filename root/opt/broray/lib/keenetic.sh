#!/bin/sh

. /opt/broray/lib/util.sh
. /opt/broray/lib/server.sh

BRORAY_BASE="/opt/broray"
BRORAY_INTERFACE_STATUS="$BRORAY_BASE/run/interface-status"
BRORAY_INTERFACE_JSON="$BRORAY_BASE/run/interface-status.json"

broray_keenetic_update_title() {
    mkdir -p "$BRORAY_BASE/run"

    active_server_id="$(
        broray_server_get_active_id
    )"

    server_file="$(
        broray_server_path "$active_server_id"
    )"

    broray_server_validate "$server_file"

    server_name="$(
        jq -r '.name // .id' "$server_file"
    )"

    client_uuid="$(
        jq -r '.uuid // empty' "$server_file"
    )"

    security="$(
        jq -r '.security // empty' "$server_file"
    )"

    network="$(
        jq -r '.network // empty' "$server_file"
    )"

    security_upper="$(
        printf '%s' "$security" |
            tr '[:lower:]' '[:upper:]'
    )"

    network_upper="$(
        printf '%s' "$network" |
            tr '[:lower:]' '[:upper:]'
    )"

    title="BROray • $server_name"
    subtitle="$security_upper + $network_upper"

    {
        printf '%s\n' "$title"
        printf '%s\n' "$subtitle"
    } > "$BRORAY_INTERFACE_STATUS" ||
        broray_die \
            "не удалось сохранить название интерфейса"

    jq -n \
        --arg title "$title" \
        --arg subtitle "$subtitle" \
        --arg serverName "$server_name" \
        --arg serverId "$active_server_id" \
        --arg uuid "$client_uuid" \
        --arg security "$security" \
        --arg network "$network" \
        '{
            title: $title,
            subtitle: $subtitle,
            serverName: $serverName,
            serverId: $serverId,
            uuid: $uuid,
            security: $security,
            network: $network
        }' > "$BRORAY_INTERFACE_JSON" ||
        broray_die \
            "не удалось сохранить состояние интерфейса"

    printf '%s\n' "$BRORAY_INTERFACE_STATUS"
}
