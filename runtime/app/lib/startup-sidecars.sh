#!/opt/bin/ash
# S24 sidecars use the same identity and kernel lifetime lease as schedulers.
# Unknown legacy projections remain evidence; they are never stop authority.
broray_sidecar_control()
{
    "${BRORAY_OPS_ASH:-/opt/bin/ash}" "${BRORAY_ROOT:-/opt/broray}/bin/broray-service" "$@"
}
broray_home_snapshot_running() { broray_sidecar_control home-snapshot status >/dev/null; }
broray_home_snapshot_start() { broray_sidecar_control home-snapshot start; }
broray_home_snapshot_stop() { broray_sidecar_control home-snapshot stop; }
broray_reconcile_stop() { broray_sidecar_control interface-reconcile stop; }
broray_reconcile_owned()
{
    broray_owned_interface_name >/dev/null 2>&1 || return 0
    broray_sidecar_control interface-reconcile start
}
