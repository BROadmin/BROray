# Called only after restore_preserved_if_present verified and restored its archive.
broray_bootstrap_restore_preserved_dot()
{
    [ -n "${preserved_stage:-}" ] || return 0
    bootstrap_dot_root="${BRORAY_SETUP_TARGET:-/opt/broray}"
    bootstrap_dot_config="$bootstrap_dot_root/routes/dot/config.json"
    [ -e "$bootstrap_dot_config" ] || { [ ! -L "$bootstrap_dot_config" ]; return $?; }
    [ -f "$bootstrap_dot_config" ] && [ ! -L "$bootstrap_dot_config" ] || return 1
    jq -e '(.managed|type)=="array" and (.requestedIds|type)=="array"' "$bootstrap_dot_config" >/dev/null || return 1
    [ "$(jq '.managed|length' "$bootstrap_dot_config")" -gt 0 ] || return 0
    jq -e '(.requestedIds|length)>0' "$bootstrap_dot_config" >/dev/null || return 1
    bootstrap_dot_request="$TMP/preserved-dot-request.json"
    bootstrap_dot_result="$TMP/preserved-dot-status.json"
    [ ! -e "$bootstrap_dot_request" ] && [ ! -L "$bootstrap_dot_request" ] || return 1
    [ ! -e "$bootstrap_dot_result" ] && [ ! -L "$bootstrap_dot_result" ] || return 1
    (set -C; jq '{serverIds:.requestedIds,allowUntested:false}' "$bootstrap_dot_config" >"$bootstrap_dot_request") || return 1
    BRORAY_DOT_RESTORE_EXACT=true "$bootstrap_dot_root/bin/broray-routes-dot" apply "$bootstrap_dot_request" >/dev/null || return 1
    "$bootstrap_dot_root/bin/broray-routes-dot" status >"$bootstrap_dot_result" || return 1
    jq -e --slurpfile request "$bootstrap_dot_request" '
      .recoveryRequired==false and .runningConfigAvailable==true and
      .drift==false and .matchesSelection==true and
      .requestedIds==$request[0].serverIds and
      .managedPresentCount==(.managed|length) and .managedPresentCount>0
    ' "$bootstrap_dot_result" >/dev/null || return 1
    printf 'PRESERVED_DOT_RESTORE=PASS\n'
}
