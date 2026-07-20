#!/bin/sh

export PATH="/opt/bin:/opt/sbin:/usr/bin:/usr/sbin:/bin:/sbin"

BASE="/opt/broray"
INTERFACE_SCRIPT="$BASE/lib/interface.sh"
INTERFACE_NAME="${BRORAY_INTERFACE:-Proxy0}"
JQ="/opt/bin/jq"

[ -x "$JQ" ] ||
    JQ="$(command -v jq 2>/dev/null)"

printf 'Content-Type: application/json\r\n'
printf 'Cache-Control: no-store\r\n'
printf '\r\n'

json_error()
{
    error_message="$1"

    if [ -n "$JQ" ] && [ -x "$JQ" ]; then
        "$JQ" -n \
            --arg error "$error_message" \
            '{
                ok: false,
                error: $error
            }'
    else
        printf '%s\n' \
            '{"ok":false,"error":"Внутренняя ошибка CGI"}'
    fi
}

if [ -z "$JQ" ] || [ ! -x "$JQ" ]; then
    json_error "На роутере не найден jq"
    exit 0
fi

if [ ! -f "$INTERFACE_SCRIPT" ]; then
    json_error "Не найден interface.sh"
    exit 0
fi

ACTION="$(
    printf '%s\n' "${QUERY_STRING:-}" |
        sed -n '
            s/^.*action=\([^&]*\).*$/\1/p
        '
)"

[ -n "$ACTION" ] ||
    ACTION="status"

case "$ACTION" in
    status|check|create|repair|delete)
        ;;
    *)
        json_error "Неизвестное действие"
        exit 0
        ;;
esac

if [ "$ACTION" != "status" ] &&
   [ "${REQUEST_METHOD:-GET}" != "POST" ]; then
    json_error "Изменяющие команды разрешены только методом POST"
    exit 0
fi

ACTION_OUTPUT=""
ACTION_RC=0

case "$ACTION" in
    status)
        ;;
    check)
        ACTION_OUTPUT="$(
            ash "$INTERFACE_SCRIPT" check 2>&1
        )"
        ACTION_RC=$?
        ;;
    create)
        ACTION_OUTPUT="$(
            ash "$INTERFACE_SCRIPT" create 2>&1
        )"
        ACTION_RC=$?
        ;;
    repair)
        ACTION_OUTPUT="$(
            ash "$INTERFACE_SCRIPT" repair 2>&1
        )"
        ACTION_RC=$?
        ;;
    delete)
        ACTION_OUTPUT="$(
            ash "$INTERFACE_SCRIPT" delete 2>&1
        )"
        ACTION_RC=$?
        ;;
esac

STATUS_FILE="$(
    mktemp /tmp/broray-interface-status.XXXXXX
)" || {
    json_error "Не удалось создать временный файл"
    exit 0
}

CONFIG_FILE="$(
    mktemp /tmp/broray-interface-config.XXXXXX
)" || {
    rm -f "$STATUS_FILE"
    json_error "Не удалось создать временный файл"
    exit 0
}

CHECK_FILE="$(
    mktemp /tmp/broray-interface-check.XXXXXX
)" || {
    rm -f "$STATUS_FILE" "$CONFIG_FILE"
    json_error "Не удалось создать временный файл"
    exit 0
}

cleanup()
{
    rm -f \
        "$STATUS_FILE" \
        "$CONFIG_FILE" \
        "$CHECK_FILE"
}

trap cleanup EXIT HUP INT TERM

EXISTS=false

if ndmc -c "show interface $INTERFACE_NAME" \
    >"$STATUS_FILE" 2>/dev/null; then
    EXISTS=true
else
    : >"$STATUS_FILE"
fi

ndmc -c 'show running-config' 2>/dev/null |
    awk -v interface_name="$INTERFACE_NAME" '
        $0 == "interface " interface_name {
            found = 1
        }

        found {
            if ($0 ~ /^interface / &&
                $0 != "interface " interface_name) {
                exit
            }

            print
        }
    ' >"$CONFIG_FILE"

ash "$INTERFACE_SCRIPT" check \
    >"$CHECK_FILE" 2>&1

CHECK_RC=$?

field_value()
{
    field_name="$1"

    awk -v field_name="$field_name" '
        {
            line = $0
            sub(/^[[:space:]]*/, "", line)

            if (index(line, field_name ":") == 1) {
                sub(/^[^:]*:[[:space:]]*/, "", line)
                print line
                exit
            }
        }
    ' "$STATUS_FILE"
}

DESCRIPTION="$(field_value description)"
TYPE="$(field_value type)"
LINK="$(field_value link)"
CONNECTED="$(field_value connected)"
STATE="$(field_value state)"
MTU="$(field_value mtu)"
VIA="$(field_value via)"
LOCAL_ADDRESS="$(
    field_value local-endpoint-address
)"
REMOTE_ADDRESS="$(
    field_value remote-endpoint-address
)"

PROTOCOL="$(
    awk '
        /^[[:space:]]*proxy protocol / {
            print $3
            exit
        }
    ' "$CONFIG_FILE"
)"

UPSTREAM_HOST="$(
    awk '
        /^[[:space:]]*proxy upstream / {
            print $3
            exit
        }
    ' "$CONFIG_FILE"
)"

UPSTREAM_PORT="$(
    awk '
        /^[[:space:]]*proxy upstream / {
            print $4
            exit
        }
    ' "$CONFIG_FILE"
)"

ADMIN_UP="$(
    awk '
        /^[[:space:]]*up[[:space:]]*$/ {
            found = 1
        }

        END {
            if (found) {
                print "yes"
            } else {
                print "no"
            }
        }
    ' "$CONFIG_FILE"
)"

CHECK_OUTPUT="$(
    cat "$CHECK_FILE"
)"

HEALTHY=false

if [ "$EXISTS" = true ] &&
   [ "$CHECK_RC" -eq 0 ] &&
   [ "$CONNECTED" = "yes" ] &&
   [ "$STATE" = "up" ]; then
    HEALTHY=true
fi

REQUEST_OK=true

if [ "$ACTION" != "status" ] &&
   [ "$ACTION_RC" -ne 0 ]; then
    REQUEST_OK=false
fi

"$JQ" -n \
    --argjson ok "$REQUEST_OK" \
    --arg action "$ACTION" \
    --argjson actionRc "$ACTION_RC" \
    --arg actionOutput "$ACTION_OUTPUT" \
    --arg name "$INTERFACE_NAME" \
    --argjson exists "$EXISTS" \
    --argjson healthy "$HEALTHY" \
    --arg description "$DESCRIPTION" \
    --arg type "$TYPE" \
    --arg protocol "$PROTOCOL" \
    --arg upstreamHost "$UPSTREAM_HOST" \
    --arg upstreamPort "$UPSTREAM_PORT" \
    --arg link "$LINK" \
    --arg connected "$CONNECTED" \
    --arg state "$STATE" \
    --arg mtu "$MTU" \
    --arg via "$VIA" \
    --arg localAddress "$LOCAL_ADDRESS" \
    --arg remoteAddress "$REMOTE_ADDRESS" \
    --arg adminUp "$ADMIN_UP" \
    --argjson checkRc "$CHECK_RC" \
    --arg checkOutput "$CHECK_OUTPUT" \
'
{
    ok: $ok,
    action: {
        name: $action,
        rc: $actionRc,
        output: $actionOutput
    },
    interface: {
        name: $name,
        exists: $exists,
        healthy: $healthy,
        description: $description,
        type: $type,
        protocol: $protocol,
        upstream: {
            host: $upstreamHost,
            port: $upstreamPort
        },
        link: $link,
        connected: $connected,
        state: $state,
        mtu: $mtu,
        via: $via,
        localAddress: $localAddress,
        remoteAddress: $remoteAddress,
        adminUp: $adminUp
    },
    check: {
        rc: $checkRc,
        output: $checkOutput
    }
}
'
