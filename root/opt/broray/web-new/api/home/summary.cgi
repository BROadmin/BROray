#!/opt/bin/ash

# Главная BROray только объединяет summary существующих модулей.

PATH="/opt/broray/bin:/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PATH

. /opt/broray/web-new/api/auth-common.sh

broray_api_require_method GET
broray_api_require_session

errors='[]'
xray='null'
servers='null'
subscriptions='null'
keenetic='null'
routes='null'
broray='null'

home_add_error()
{
    module_id="$1"
    errors="$(
        jq -nc \
            --argjson current "$errors" \
            --arg module "$module_id" \
            '$current + [$module]'
    )"
}

xray_raw="$(
    (
        . /opt/broray/lib/xray.sh
        broray_xray_status_json
    ) 2>/dev/null
)"

if ! xray="$(
    printf '%s\n' "$xray_raw" |
        jq -ce '
            select(
                .success == true and
                (.data | type) == "object"
            ) |
            .data
        ' 2>/dev/null
)"; then
    xray='null'
    home_add_error xray
fi

servers_raw="$(
    (
        . /opt/broray/lib/server-service.sh
        broray_server_summary
    ) 2>/dev/null
)"

if ! servers="$(
    printf '%s\n' "$servers_raw" |
        jq -ce 'select(type == "object")' 2>/dev/null
)"; then
    servers='null'
    home_add_error servers
fi

subscriptions_raw="$(
    (
        . /opt/broray/lib/subscription-service.sh
        broray_subscription_summary
    ) 2>/dev/null
)"

if ! subscriptions="$(
    printf '%s\n' "$subscriptions_raw" |
        jq -ce 'select(type == "object")' 2>/dev/null
)"; then
    subscriptions='null'
    home_add_error subscriptions
fi

keenetic_raw="$(
    (
        . /opt/broray/lib/keenetic-page.sh
        broray_keenetic_status_json
    ) 2>/dev/null
)"

if ! keenetic="$(
    printf '%s\n' "$keenetic_raw" |
        jq -ce 'select(type == "object")' 2>/dev/null
)"; then
    keenetic='null'
    home_add_error keenetic
fi

routes_raw="$(
    (
        . /opt/broray/lib/routes-summary.sh
        broray_routes_summary_all
    ) 2>/dev/null
)"

if ! routes="$(
    printf '%s\n' "$routes_raw" |
        jq -ce 'select(type == "object")' 2>/dev/null
)"; then
    routes='null'
    home_add_error routes
fi

broray_raw="$(
    /opt/broray/bin/broray-system info 2>/dev/null
)"

if ! broray="$(
    printf '%s\n' "$broray_raw" |
        jq -ce '
            select(
                .ok == true and
                (type == "object")
            )
        ' 2>/dev/null
)"; then
    broray='null'
    home_add_error broray
fi

home_json="$(
    jq -nc \
        --argjson xray "$xray" \
        --argjson servers "$servers" \
        --argjson subscriptions "$subscriptions" \
        --argjson keenetic "$keenetic" \
        --argjson routes "$routes" \
        --argjson broray "$broray" \
        --argjson errors "$errors" \
        --arg updatedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" '
        {
            xray: $xray,
            servers: $servers,
            subscriptions: $subscriptions,
            keenetic: $keenetic,
            routes: $routes,
            broray: $broray,
            errors: $errors,
            healthy: (($errors | length) == 0),
            updatedAt: $updatedAt
        }
    '
)" || {
    broray_api_error \
        "500 Internal Server Error" \
        "HOME_SUMMARY_FAILED" \
        "Не удалось объединить состояние модулей."
}

broray_api_success "$home_json"
