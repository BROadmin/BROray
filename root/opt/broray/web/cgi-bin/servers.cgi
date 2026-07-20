#!/bin/sh

echo 'Content-Type: application/json'
echo

active=""

if [ -f /opt/broray/config/active-server ]; then
    IFS= read -r active \
        < /opt/broray/config/active-server
fi

set -- /opt/broray/servers/*.json

if [ ! -f "$1" ]; then
    printf '%s\n' \
        '{"ok":true,"count":0,"current":null,"servers":[]}'
    exit 0
fi

/opt/bin/jq -s \
    --arg active "$active" '
        map(
            select(type == "object") |
            . + {
                active: (.id == $active)
            }
        ) as $servers |

        {
            ok: true,
            count: ($servers | length),
            current: (
                $servers |
                map(select(.active == true)) |
                first // null
            ),
            servers: $servers
        }
    ' "$@"
