#!/opt/bin/ash
# Only the admitted executor publishes a completed measurement.
. "$BRORAY_BASE/lib/operation-job.sh"

broray_server_check()
{
    local job_server job_source job_dir job_rc job_quality job_target job_publish_rc job_fingerprint job_server_file
    broray_job_require_owner || return $?
    job_server="$1"; job_source="${2:-manual}"
    job_fingerprint="${3:-}"
    case "$job_source" in manual|auto-switch|scheduled) ;; *) return 64 ;; esac
    broray_server_validate_id "$job_server"
    broray_server_exists "$job_server" || return 1
    job_server_file="$BRORAY_SERVERS/$job_server.json"
    if [ -n "$job_fingerprint" ]; then
        [ ! -L "$job_server_file" ] && [ "$(sha256sum "$job_server_file" | cut -d ' ' -f 1)" = "$job_fingerprint" ] || return 76
    fi
    broray_job_checkpoint checking || return $?
    mkdir -p "$BRORAY_BASE/tmp" "$BRORAY_QUALITY_DIR" || return 1
    job_dir="$(mktemp -d "$BRORAY_BASE/tmp/server-check-$BRORAY_BACKGROUND_OPERATION_ID-XXXXXX")" || return 1
    chmod 700 "$job_dir" || return 1
    printf '%s\n' "$BRORAY_BACKGROUND_OPERATION_ID" >"$job_dir/operation-id" || return 1
    mkdir -m 700 "$job_dir/quality" || return 1
    job_target="$BRORAY_QUALITY_DIR/$job_server.json"
    job_quality="$job_dir/quality/$job_server.json"
    if [ -e "$job_target" ] || [ -L "$job_target" ]; then
        [ -f "$job_target" ] && [ ! -L "$job_target" ] || return 1
        cp "$job_target" "$job_quality" || return 1
    fi
    job_rc=0
    broray_ops_run_helper 120 -- "${BRORAY_OPS_ASH:-/opt/bin/ash}" \
      "$BRORAY_BASE/lib/server-check-prepare.sh" "$job_dir" "$job_server" "$job_source" || job_rc=$?
    if [ "$job_rc" = 75 ]; then BRORAY_JOB_UNRESOLVED=true; return 75; fi
    case "$job_rc" in
      0|1) ;;
      *) rm -rf "$job_dir"; return "$job_rc" ;;
    esac
    # A complete negative measurement has exit 1. A crash, timeout or
    # cancellation without a complete result never replaces quality.
    if [ ! -f "$job_dir/result.json" ] || [ -L "$job_dir/result.json" ] ||
      [ ! -f "$job_quality" ] || [ -L "$job_quality" ] ||
      ! jq -e --arg id "$job_server" --arg source "$job_source" --argjson rc "$job_rc" \
        --slurpfile quality "$job_quality" 'type=="object" and .serverId==$id and
        .success==($rc==0) and .quality==$quality[0] and
        (.quality|type)=="object" and .quality.measurementSource==$source and
        (.quality.status==(if $rc==0 then "available" else "unavailable" end)) and
        all([.quality.successfulChecks,.quality.failedChecks,.quality.disconnects,.quality.durationMs][];
          type=="number" and .>=0 and floor==.)' "$job_dir/result.json" >/dev/null; then
        rm -rf "$job_dir"; return 1
    fi
    if [ -z "$job_fingerprint" ] && [ "$(cat "$BRORAY_ACTIVE_SERVER_FILE" 2>/dev/null)" = "$job_server" ]; then
        . "$BRORAY_BASE/lib/active-proxy-health.sh" || return 1
        broray_active_proxy_measure "$job_server" || return $?
        jq --argjson activeHealth "$ACTIVE_PROXY_RESULT" '. + {activeHealth:$activeHealth}' \
            "$job_quality" >"$job_dir/active-quality.json" || return 1
        mv "$job_dir/active-quality.json" "$job_quality" || return 1
        jq --slurpfile quality "$job_quality" '.quality=$quality[0]' \
            "$job_dir/result.json" >"$job_dir/active-result.json" || return 1
        mv "$job_dir/active-result.json" "$job_dir/result.json" || return 1
    fi
    if [ -n "$job_fingerprint" ]; then
        if [ -L "$job_server_file" ] || [ "$(sha256sum "$job_server_file" | cut -d ' ' -f 1)" != "$job_fingerprint" ]; then
            rm -rf "$job_dir"; return 76
        fi
        jq --arg hash "$job_fingerprint" '.serverFingerprint=$hash' "$job_quality" >"$job_dir/bound-quality.json" || return 1
        mv "$job_dir/bound-quality.json" "$job_quality" || return 1
        jq --slurpfile quality "$job_quality" '.quality=$quality[0]' "$job_dir/result.json" >"$job_dir/bound-result.json" || return 1
        mv "$job_dir/bound-result.json" "$job_dir/result.json" || return 1
    fi
    chmod 600 "$job_quality" || return 1
    job_publish_rc=0
    broray_job_publish_json server-quality "$job_server" "$job_quality" || job_publish_rc=$?
    [ "$job_publish_rc" = 0 ] || return "$job_publish_rc"
    cat "$job_dir/result.json"
    rm -rf "$job_dir" || return 1
    return "$job_rc"
}

# Snapshot exact inputs without credentials in the queue. One stage consumes
# one entry; a changed catalog never rewrites the remaining snapshot silently.
broray_quality_snapshot()
(
    local target file id hash
    target="$1"
    if [ "$target" != all ]; then
        case "$target" in ''|.|..|*[!a-zA-Z0-9._-]*) exit 74 ;; esac
        [ -f "$BRORAY_SERVERS/$target.json" ] && [ ! -L "$BRORAY_SERVERS/$target.json" ] || exit 76
    fi
    set -o pipefail
    {
        set --
        for file in "$BRORAY_SERVERS"/*.json; do
            [ -e "$file" ] || [ -L "$file" ] || continue
            id="${file##*/}"; id="${id%.json}"
            [ "$target" = all ] || [ "$target" = "$id" ] || continue
            case "$id" in ''|.|..|*[!a-zA-Z0-9._-]*) exit 74 ;; esac
            [ -f "$file" ] && [ ! -L "$file" ] || exit 74
            set -- "$@" "$file"
            if [ "$#" = 32 ]; then
                sha256sum "$@" || exit 74
                set --
            fi
        done
        [ "$#" = 0 ] || sha256sum "$@" || exit 74
    } | while IFS=' ' read -r hash file; do
            # sha256sum output is data. Match its directory/filename and
            # digest, without eval or a per-node hashing process.
            case "$file" in "$BRORAY_SERVERS/"*) ;; *) exit 74 ;; esac
            id="${file##*/}"; id="${id%.json}"
            [ "$file" = "$BRORAY_SERVERS/$id.json" ] || exit 74
            case "$id" in ''|.|..|*[!a-zA-Z0-9._-]*) exit 74 ;; esac
            case "$hash" in *[!0-9a-f]*) exit 74 ;; esac
            [ "${#hash}" = 64 ] || exit 74
            printf '{"id":"%s","sha256":"%s"}\n' "$id" "$hash"
    done | jq -sc 'sort_by(.id) | select(length<=4096)'
)

broray_quality_context()
{
    local snapshot config_hash config
    snapshot="$(broray_quality_snapshot "$1")" || return $?
    [ -n "$snapshot" ] || return 74
    config_hash=''
    case "${2:-SERVER_CHECK_AUTO}" in
      SERVER_CHECK_AUTO)
        config="$BRORAY_BASE/config/system/server-auto-switch.json"
        [ -f "$config" ] && [ ! -L "$config" ] || return 74
        jq -e 'type=="object"' "$config" >/dev/null || return 74
        config_hash="$(sha256sum "$config" | cut -d ' ' -f 1)" || return 74 ;;
      USER) ;;
      *) return 64 ;;
    esac
    jq -nc --argjson servers "$snapshot" --arg config "$config_hash" \
      '{servers:$servers,autoConfigSha256:$config}' | sha256sum | cut -d ' ' -f 1
}

broray_quality_step()
{
    local q_request q_owner q_directory q_result q_digest q_target q_context q_source q_snapshot q_state
    local q_cursor q_total q_id q_hash q_output q_rc q_status q_progress q_now q_interval q_tmp q_origin q_config
    q_request="$1"
    broray_job_require_owner || return $?
    q_owner="$(broray_ops_operation_directory)" || return 74
    q_target="$(jq -er .bundleId "$q_owner/state.json")" || return 74
    q_context="$(jq -er .queueStep.context "$q_owner/state.json")" || return 74
    q_source="$(jq -er .source "$q_owner/state.json")" || return 74
    q_origin="$q_source"
    [ "$(broray_quality_context "$q_target" "$q_origin")" = "$q_context" ] || return 76
    q_config="$BRORAY_BASE/config/system/server-auto-switch.json"
    if [ "$q_origin" = SERVER_CHECK_AUTO ]; then
        jq -e '.qualityRefreshEnabled==true' "$q_config" >/dev/null || return 76
    fi
    q_interval="$(jq -r '.qualityRefreshIntervalMinutes // 60' "$q_config" 2>/dev/null)" || q_interval=60
    case "$q_interval" in 30|60|180|360) ;; *) q_interval=60 ;; esac
    case "$q_source" in USER) q_source=manual ;; SERVER_CHECK_AUTO) q_source=scheduled ;; *) return 74 ;; esac
    q_directory="${BRORAY_OPS_RAM_ROOT:-/tmp/broray-operations}/requests/$q_request"
    [ -d "$q_directory" ] && [ ! -L "$q_directory" ] || return 74
    q_result="$q_directory/result.json"
    q_digest="$(jq -r '.queueStep.resultSha256 // empty' "$q_owner/state.json")" || return 74
    if [ -n "$q_digest" ]; then
        [ -f "$q_result" ] && [ ! -L "$q_result" ] &&
          [ "$(stat -c '%u:%a:%h' "$q_result")" = "$(id -u):600:1" ] &&
          [ "$(sha256sum "$q_result" | cut -d ' ' -f 1)" = "$q_digest" ] || return 76
        q_state="$(jq -ce --arg context "$q_context" '
          def count: type=="number" and .>=0 and floor==.;
          select(.schemaVersion==1 and .kind=="quality" and .context==$context and
            (.servers|type)=="array" and (.servers|length)==.totalCount and .totalCount<=4096 and
            (.cursor|count) and .cursor<=.totalCount and .checkedCount==.cursor and
            all([.availableCount,.unavailableCount,.errorCount][];count) and
            .checkedCount==(.availableCount+.unavailableCount+.errorCount))' "$q_result")" || return 76
    else
        [ ! -e "$q_result" ] && [ ! -L "$q_result" ] || return 76
        q_snapshot="$(broray_quality_snapshot "$q_target")" || return $?
        q_state="$(jq -nc --arg context "$q_context" --argjson servers "$q_snapshot" --arg now "$(broray_server_now)" '
          {schemaVersion:1,kind:"quality",context:$context,servers:$servers,cursor:0,totalCount:($servers|length),
           checkedCount:0,availableCount:0,unavailableCount:0,errorCount:0,runStartedAt:$now}')"
    fi
    q_cursor="$(printf '%s\n' "$q_state" | jq -r .cursor)"
    q_total="$(printf '%s\n' "$q_state" | jq -r .totalCount)"
    if [ "$q_cursor" -lt "$q_total" ]; then
        q_id="$(printf '%s\n' "$q_state" | jq -er --argjson n "$q_cursor" '.servers[$n].id')" || return 76
        q_hash="$(printf '%s\n' "$q_state" | jq -er --argjson n "$q_cursor" '.servers[$n].sha256')" || return 76
        q_output="$q_directory/probe-$BRORAY_BACKGROUND_OPERATION_ID.json"
        [ ! -e "$q_output" ] && [ ! -L "$q_output" ] || return 74
        q_rc=0
        broray_server_check "$q_id" "$q_source" "$q_hash" >"$q_output" || q_rc=$?
        case "$q_rc" in 0|1) ;; *) return "$q_rc" ;; esac
        # Exit 1 means a complete negative measurement only with its receipt.
        jq -e --arg id "$q_id" --arg hash "$q_hash" --argjson rc "$q_rc" '
          .serverId==$id and .success==($rc==0) and .quality.serverFingerprint==$hash' "$q_output" >/dev/null || return 74
        q_state="$(printf '%s\n' "$q_state" | jq -c --argjson rc "$q_rc" '
          .cursor+=1 | .checkedCount+=1 |
          if $rc==0 then .availableCount+=1 else .unavailableCount+=1 end')" || return 74
        q_cursor=$((q_cursor+1))
    fi
    q_status=running
    [ "$q_cursor" -lt "$q_total" ] || q_status=success
    q_now="$(date '+%s')"
    [ "$(broray_quality_context "$q_target" "$q_origin")" = "$q_context" ] || return 76
    q_progress="$q_directory/progress-$BRORAY_BACKGROUND_OPERATION_ID.json"
    [ ! -e "$q_progress" ] && [ ! -L "$q_progress" ] || return 74
    (set -C; printf '%s\n' "$q_state" | jq -c --arg status "$q_status" --arg now "$(broray_server_now)" \
      --arg operation "$BRORAY_BACKGROUND_OPERATION_ID" --arg request "$q_request" \
      --argjson next "$((q_now+q_interval*60))" --argjson interval "$q_interval" '
      {status:$status,backgroundOperationId:$operation,requestId:$request,intervalMinutes:$interval,
       runStartedAt:(if $status=="running" then .runStartedAt else null end),
       lastCompletedAt:(if $status=="success" then $now else null end),
       nextCheckEpoch:(if $status=="success" then $next else 0 end),
       totalCount,checkedCount,availableCount,unavailableCount,errorCount,lastError:null,
       lastResult:("Проверено: "+(.checkedCount|tostring)+" из "+(.totalCount|tostring))}' >"$q_progress") || return 74
    chmod 600 "$q_progress" || return 74
    broray_job_publish_json quality-progress '' "$q_progress" || return $?
    # The result is RAM-only. A crash leaves the request failed; it cannot
    # replay a completed probe or invent a later cursor.
    q_tmp="$q_directory/result-$BRORAY_BACKGROUND_OPERATION_ID.tmp"
    [ ! -e "$q_tmp" ] && [ ! -L "$q_tmp" ] && [ ! -L "$q_result" ] || return 74
    (set -C; printf '%s\n' "$q_state" >"$q_tmp") || return 74
    chmod 600 "$q_tmp" && mv "$q_tmp" "$q_result" || return 74
    if [ "$q_cursor" -lt "$q_total" ]; then
        q_digest="$(sha256sum "$q_result" | cut -d ' ' -f 1)" || return 74
        broray_job_yield probe "$q_digest" || return $?
    fi
    return 0
}
