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
    case "$component" in route-delete|route-export|server-deactivate|dot-delete|dot-restore) ;; *) return 64 ;; esac
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
                dot-delete)
                    # The uninstall confirmation grants deletion only of its
                    # verified managed DoT set. Bind the fresh preview to the
                    # mutation under the same native transition/owner fence.
                    . "$BRORAY_LIFECYCLE_BASE/lib/routes-dot.sh" || exit 1
                    dot_confirmation="$(mktemp "$BRORAY_LIFECYCLE_BASE/tmp/uninstall-dot.XXXXXX")" || exit 1
                    trap 'rm -f "$dot_confirmation"' EXIT
                    chmod 600 "$dot_confirmation" || exit 1
                    broray_dot_delete_preview >"$dot_confirmation" || exit 1
                    broray_dot_delete "$dot_confirmation"
                    ;;
                dot-restore)
                    [ "$#" = 1 ] && [ "${BRORAY_DOT_RESTORE_EXACT:-}" = true ] || exit 64
                    . "$BRORAY_LIFECYCLE_BASE/lib/routes-dot.sh" || exit 1
                    broray_dot_apply "$1"
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

broray_lifecycle_dot_remove_owned() {
    # Keenetic removes interface routes when the owned interface is deleted.
    # DoT is independent: delete only the verified managed entries under its
    # existing confirmation and ownership protocol.
    if [ "${uninstall_dot_owned:-false}" = true ]; then
        broray_lifecycle_component dot-delete >/dev/null || return 1
    fi

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
