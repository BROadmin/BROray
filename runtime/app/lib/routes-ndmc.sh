#!/opt/bin/ash
# Authorization remains in each domain's existing exact command policy.
broray_routes_ndmc_capture()
{
    local executable limit wait_limit old_lane runner rc
    executable="$1"; limit="${5:-10}"
    case "$executable" in */*) ;; *) executable="$(command -v "$executable")" || return 127 ;; esac
    [ -x "$executable" ] || return 127
    case "$limit" in ''|*[!0-9]*) return 64 ;; esac
    old_lane="${BRORAY_ROUTES_CONFIG_NDMC_LOCK:-$BRORAY_ROOT/run/routes-router-ndmc.lock}"
    runner="${BRORAY_NDMC_RUNNER:-$BRORAY_ROOT/bin/broray-ndmc-run}"
    wait_limit="${BRORAY_ROUTES_CONFIG_NDMC_LOCK_WAIT:-$((limit + 5))}"
    [ -x "$runner" ] || return 127
    [ ! -L "${old_lane%/*}" ] || return 74
    mkdir -p "${old_lane%/*}" || return 74
    [ "$(readlink -f "${old_lane%/*}")" = "${old_lane%/*}" ] || return 74
    rc=0
    "$runner" "$old_lane.guard" "$old_lane" "$wait_limit" "$limit" "$executable" "$2" >"$3" 2>"$4" || rc=$?
    case "$rc" in
        124) printf '%s\n' 'ROUTES_CONFIG_NDMC_TIMEOUT' >>"$4" ;;
        125) printf '%s\n' 'ROUTES_CONFIG_NDMC_LOCK_FAILED' >>"$4" ;;
    esac
    return "$rc"
}
