#!/opt/bin/ash
broray_server_import_job()
{
    local import_job_dir import_job_rc import_job_file import_job_id import_job_target import_job_candidate
    broray_job_require_owner || return $?
    [ "${2:-manual}" = manual ] && [ -n "${1:-}" ] || return 64
    broray_job_checkpoint parsing || return $?
    mkdir -p "$BRORAY_BASE/tmp" "$BRORAY_SERVERS" || return 1
    import_job_dir="$(mktemp -d "$BRORAY_BASE/tmp/server-import-$BRORAY_BACKGROUND_OPERATION_ID-XXXXXX")" || return 1
    chmod 700 "$import_job_dir" || return 1
    printf '%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >"$import_job_dir/operation-id" || return 1
    printf '%s' "$1" >"$import_job_dir/uri" || return 1
    import_job_rc=0
    broray_ops_run_helper 30 -- "${BRORAY_OPS_ASH:-/opt/bin/ash}" \
      "$BRORAY_BASE/lib/server-import-prepare.sh" "$import_job_dir" || import_job_rc=$?
    if [ "$import_job_rc" = 75 ]; then BRORAY_JOB_UNRESOLVED=true; return 75; fi
    if [ "$import_job_rc" != 0 ]; then rm -rf "$import_job_dir"; return "$import_job_rc"; fi
    set -- "$import_job_dir/servers/"*.json
    if [ "$#" != 1 ] || [ ! -f "$1" ] || [ -L "$1" ]; then rm -rf "$import_job_dir"; return 1; fi
    import_job_file="$1"; import_job_id="${1##*/}"; import_job_id="${import_job_id%.json}"
    broray_server_validate_id "$import_job_id"
    jq -e --arg id "$import_job_id" '.id==$id and .source.type=="manual"' "$import_job_file" >/dev/null || return 1
    import_job_target="$BRORAY_SERVERS/$import_job_id.json"
    [ ! -e "$import_job_target" ] && [ ! -L "$import_job_target" ] || { rm -rf "$import_job_dir"; return 1; }
    broray_job_checkpoint committing || { import_job_rc=$?; rm -rf "$import_job_dir"; return "$import_job_rc"; }
    import_job_candidate="$(mktemp "$BRORAY_SERVERS/.import-XXXXXX")" || return 1
    if ! cp "$import_job_file" "$import_job_candidate" || ! chmod 600 "$import_job_candidate" ||
      ! "${BRORAY_OPS_GUARD:-$BRORAY_BASE/bin/broray-ops-guard}" --replace-file "$import_job_candidate" "$import_job_target"; then
        BRORAY_JOB_UNRESOLVED=true; return 75
    fi
    jq -n --arg id "$import_job_id" '{imported:true,id:$id}'
    rm -rf "$import_job_dir"
}
