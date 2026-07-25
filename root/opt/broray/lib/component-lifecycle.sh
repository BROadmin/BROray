#!/opt/bin/ash

# Единая точка оркестрации удаления. Здесь нет реализации маршрутов,
# управляемый ProxyN, серверов или Xray — вызываются только публичные интерфейсы модулей.

BRORAY_LIFECYCLE_BASE="${BRORAY_LIFECYCLE_BASE:-${BRORAY_BASE:-/opt/broray}}"

broray_lifecycle_routes_remove_all() {
    routes_cli="$BRORAY_LIFECYCLE_BASE/bin/broray-routes"
    routes_bundles="$BRORAY_LIFECYCLE_BASE/routes/bundles.json"
    [ -x "$routes_cli" ] || return 0
    [ -r "$routes_bundles" ] || return 0

    [ -r "$BRORAY_LIFECYCLE_BASE/lib/routes-summary.sh" ] || return 1
    . "$BRORAY_LIFECYCLE_BASE/lib/routes-summary.sh"

    for routes_bundle_id in $(
        jq -r '.bundles[]' "$routes_bundles" 2>/dev/null
    )
    do
        summary="$(
            broray_routes_summary "$routes_bundle_id" 2>/dev/null
        )" || return 1

        installed="$(
            printf '%s' "$summary" |
                jq -r '.installed' 2>/dev/null
        )"

        case "$installed" in
            true|1)
                "$routes_cli" delete "$routes_bundle_id" || return 1
                ;;
            false|0)
                ;;
            *)
                printf '%s\n' \
                    'Модуль маршрутов вернул некорректный статус установки.' >&2
                return 1
                ;;
        esac
    done

    return 0
}

broray_lifecycle_keenetic_delete() {
    if [ -r "$BRORAY_LIFECYCLE_BASE/lib/keenetic-page.sh" ]; then
        . "$BRORAY_LIFECYCLE_BASE/lib/keenetic-page.sh"
        if command -v broray_keenetic_run_action >/dev/null 2>&1; then
            broray_keenetic_run_action delete
            return $?
        fi
    fi

    if [ -r "$BRORAY_LIFECYCLE_BASE/lib/interface.sh" ]; then
        ash "$BRORAY_LIFECYCLE_BASE/lib/interface.sh" delete
        return $?
    fi

    return 0
}

broray_lifecycle_web_publish_delete() {
    [ -r "$BRORAY_LIFECYCLE_BASE/lib/web-publish.sh" ] || return 0
    . "$BRORAY_LIFECYCLE_BASE/lib/web-publish.sh"
    broray_web_publish_delete
}

broray_lifecycle_servers_deactivate() {
    [ -x "$BRORAY_LIFECYCLE_BASE/bin/broray-servers" ] || return 0
    [ -s "$BRORAY_LIFECYCLE_BASE/config/active-server" ] || return 0
    "$BRORAY_LIFECYCLE_BASE/bin/broray-servers" deactivate
}

broray_lifecycle_xray_stop() {
    [ -x "$BRORAY_LIFECYCLE_BASE/bin/broray" ] || return 0
    "$BRORAY_LIFECYCLE_BASE/bin/broray" xray stop
}
