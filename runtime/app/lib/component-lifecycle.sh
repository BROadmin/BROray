#!/opt/bin/ash

# Оркестрация удаления внутри уже принадлежащей ему системной транзакции.

BRORAY_LIFECYCLE_BASE="${BRORAY_LIFECYCLE_BASE:-${BRORAY_BASE:-/opt/broray}}"

broray_lifecycle_uninstall_owner() {
    [ -n "${operation_id:-}" ] || return 1
    broray_system_global_control_validate uninstall "$operation_id" &&
        broray_tx_control_owner_assert_self "$BRORAY_GLOBAL_LOCK/owner-identity.tsv"
}

# Internal synchronous calls must not enter a second background job beneath
# the uninstall fence. The actual native OPKG control mutex spans the call;
# its FIFO writer is inherited by descendants, so owner death cannot admit a
# successor while an old component is still running. No public bypass flag.
broray_lifecycle_component() {
    local component component_rc
    component="$1"; shift
    case "$component" in route-delete|route-export|server-deactivate) ;; *) return 64 ;; esac
    [ -z "${BRORAY_BACKGROUND_OPERATION_ID:-}" ] || return 1
    broray_lifecycle_uninstall_owner || return 1
    broray_tx_control_transition_begin || return 1
    component_rc=1
    if broray_tx_control_transition_assert && broray_lifecycle_uninstall_owner; then
        component_rc=0
        (
            case "$component" in
                route-delete)
                    . "$BRORAY_LIFECYCLE_BASE/lib/routes-router-delete.sh" || exit 1
                    trap broray_routes_delete_cleanup EXIT
                    trap 'exit 129' HUP; trap 'exit 130' INT; trap 'exit 143' TERM
                    broray_routes_router_delete_run "$1"
                    ;;
                route-export)
                    # Separate trap/lease scopes, as in the original CLI.
                    (. "$BRORAY_LIFECYCLE_BASE/lib/routes-export-build.sh" &&
                        broray_routes_export_build_run "$1") || exit $?
                    . "$BRORAY_LIFECYCLE_BASE/lib/routes-router-sync.sh" || exit 1
                    broray_routes_sync_apply "$1"
                    ;;
                server-deactivate)
                    . "$BRORAY_LIFECYCLE_BASE/lib/server-service.sh" || exit 1
                    broray_server_deactivate_commit
                    ;;
            esac
        ) || component_rc=$?
    fi
    broray_tx_control_transition_end || return 1
    return "$component_rc"
}

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
            true) broray_lifecycle_component route-delete "$routes_bundle_id" || return 1 ;;
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
    broray_lifecycle_component server-deactivate
}

broray_lifecycle_xray_stop() {
    [ -x "$BRORAY_LIFECYCLE_BASE/bin/broray" ] || return 0
    "$BRORAY_LIFECYCLE_BASE/bin/broray" xray stop
}
