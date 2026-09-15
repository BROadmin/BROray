#!/opt/bin/ash

# Единая точка оркестрации удаления. Здесь нет реализации маршрутов,
# управляемый интерфейс Keenetic, серверов или Xray — вызываются только публичные интерфейсы модулей.

BRORAY_LIFECYCLE_BASE="${BRORAY_LIFECYCLE_BASE:-${BRORAY_BASE:-/opt/broray}}"

broray_lifecycle_routes_remove_all() {
    routes_cli="$BRORAY_LIFECYCLE_BASE/bin/broray-routes"
    dot_cli="$BRORAY_LIFECYCLE_BASE/bin/broray-routes-dot"
    routes_bundles="$BRORAY_LIFECYCLE_BASE/routes/bundles.json"
    [ -x "$routes_cli" ] || return 1
    [ -f "$routes_bundles" ] && [ ! -L "$routes_bundles" ] || return 1
    [ -r "$BRORAY_LIFECYCLE_BASE/lib/routes-summary.sh" ] || return 1
    jq -e '
      (.bundles|type)=="array" and
      ((.bundles|length)==(.bundles|unique|length)) and
      all(.bundles[]; type=="string" and length>0 and
          all(explode[];
              (.>=48 and .<=57) or (.>=65 and .<=90) or
              (.>=97 and .<=122) or .==45 or .==46 or .==95))
    ' "$routes_bundles" >/dev/null 2>&1 || return 1
    routes_bundle_list="$(jq -r '.bundles[]' "$routes_bundles" 2>/dev/null)" || return 1
    . "$BRORAY_LIFECYCLE_BASE/lib/routes-summary.sh"

    while IFS= read -r routes_bundle_id
    do
        [ -n "$routes_bundle_id" ] || continue
        summary="$(
            broray_routes_summary "$routes_bundle_id" 2>/dev/null
        )" || return 1

        installed="$(
            printf '%s' "$summary" |
                jq -er '.installed | if .==true then "true" elif .==false then "false" else error("invalid") end' \
                    2>/dev/null
        )" || return 1

        case "$installed" in
            true) "$routes_cli" delete "$routes_bundle_id" || return 1 ;;
            false) ;;
            *) return 1 ;;
        esac
    done <<EOF_ROUTES_BUNDLES
$routes_bundle_list
EOF_ROUTES_BUNDLES

    # DNS-over-TLS has an independent ownership receipt and transaction
    # engine.  Removing ordinary route bundles must not strand those owned
    # Keenetic entries when the package is uninstalled.
    [ -x "$dot_cli" ] || return 1
    "$dot_cli" delete >/dev/null || return 1

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
