#!/opt/bin/ash
# Read cached, bounded facts only. No config/log text, network probes or repairs.
ops_report()
{
    local snapshot journal build arch uptime request pending report
    snapshot="$(ops_status)" || snapshot='{"ok":false,"complete":false,"operations":[],"errors":["STATE_UNAVAILABLE"],"automationPaused":null,"globalFence":"unknown"}'
    journal="$(ops_journal_snapshot)" || journal='{"complete":false,"events":[],"errors":["JOURNAL_UNAVAILABLE"],"truncated":false}'
    build='{}'
    if ops_file_safe "$OPS_APP/web-new/build.json" 8192; then
        build="$(jq -c -L "$OPS_APP/lib" 'include "operation-report-public"; build_public' "$OPS_APP/web-new/build.json" 2>/dev/null)" || build='{}'
    fi
    arch="$(uname -m)"
    case "$arch" in aarch64|armv7l|mips|mipsel|x86_64) ;; *) arch=unknown ;; esac
    uptime="$(awk 'NR==1 && $1~/^[0-9]+[.][0-9]+$/ {if($1>=0 && $1<3155760000) printf "%.0f",int($1)}' "$OPS_PROC/uptime" 2>/dev/null)"
    case "$uptime" in ''|*[!0-9]*) uptime=null ;; esac
    request=absent
    if [ -e "$OPS_UPDATER/request.lock" ] || [ -L "$OPS_UPDATER/request.lock" ]; then request=present; fi
    pending=clear; ops_pending_domain && pending=pending
    report="$(jq -nc --arg now "$(ops_now)" --arg arch "$arch" --argjson uptime "$uptime" \
      --argjson build "$build" --argjson snapshot "$snapshot" --argjson journal "$journal" \
      --arg request "$request" --arg pending "$pending" '
      {schemaVersion:1,reportKind:"broray-diagnostics",capturedAt:$now,redactionPolicy:"allowlist-v1",
       complete:false,unavailable:["keeneticOS","kernel","serviceIdentities","automationSettings","vpnContinuity"],
       build:{appVersion:$build.appVersion,candidateId:$build.candidateId,webuiBuild:$build.webuiBuild},
       platform:{architecture:$arch,kernel:null,uptimeSeconds:$uptime},
       services:{xray:"unknown",scheduler:"unknown",updater:"unknown"},
       automation:{paused:$snapshot.automationPaused,subscriptionUpdate:null,serverCheck:null,autoSwitch:null},
       operations:([$snapshot.operations[] | select(.running!=false)] + [$snapshot.operations[] | select(.running==false)][0:20]),
       fences:{global:$snapshot.globalFence,updaterRequest:$request,domainPending:$pending},
       events:$journal.events,journal:{complete:$journal.complete,truncated:$journal.truncated},
       snapshotComplete:$snapshot.complete,errors:(($snapshot.errors+$journal.errors)|unique)}')" || return 1
    [ "$(printf '%s' "$report" | wc -c)" -le 1048576 ] || return 1
    printf '%s\n' "$report"
}
