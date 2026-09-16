#!/opt/bin/ash
# Conservative compatibility lease for the shared route resource. This does
# not recover a crashed route transaction or replace global job admission.
broray_route_resource_request()
{
    local lock parent guard ash pid rest rc
    lock="$1"; shift
    parent="${lock%/*}"
    case "$lock" in /*/operation.lock) ;; *) return 1 ;; esac
    [ ! -L "$parent" ] || return 1
    mkdir -p "$parent" || return 1
    [ "$(readlink -f "$parent")" = "$parent" ] || return 1
    guard="${BRORAY_OPS_GUARD:-${BRORAY_ROOT:-/opt/broray}/bin/broray-ops-guard}"
    ash="${BRORAY_OPS_ASH:-/opt/bin/ash}"
    IFS=' ' read -r pid rest </proc/self/stat || return 1
    rc=0
    "$guard" "$parent/resource.control.guard" "$ash" \
      "${BRORAY_ROOT:-/opt/broray}/lib/routes-resource-control.sh" "$lock" "$pid" "$@" || rc=$?
    case "$rc" in 0) return 0 ;; 2|75) return 2 ;; *) return 1 ;; esac
}

broray_route_resource_acquire()
{
    local response rc
    # Capture the actual caller before command substitution changes /proc/self.
    IFS=' ' read -r BRORAY_ROUTE_RESOURCE_CALLER broray_resource_rest </proc/self/stat || return 1
    export BRORAY_ROUTE_RESOURCE_CALLER
    BRORAY_ROUTE_RESOURCE_TOKEN=''
    rc=0; response="$(broray_route_resource_request "$1" acquire "$2" "${3:-}")" || rc=$?
    unset BRORAY_ROUTE_RESOURCE_CALLER
    [ "$rc" = 0 ] || return "$rc"
    case "$response" in ''|*[!0-9a-f]*) return 1 ;; esac
    [ "${#response}" = 32 ] || return 1
    BRORAY_ROUTE_RESOURCE_TOKEN="$response"
}

broray_route_resource_release()
{
    [ -n "${2:-}" ] || return 0
    broray_route_resource_request "$1" release "$2"
}
