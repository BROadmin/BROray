#!/opt/bin/ash
# Read cached, bounded facts only. No config/log text, network probes or repairs.
ops_report()
{
    local snapshot journal build arch kernel uptime request pending report automation services
    . "$OPS_APP/lib/operation-report-facts.sh" || return 1
    snapshot="$(ops_status)" || snapshot='{"ok":false,"complete":false,"operations":[],"errors":["STATE_UNAVAILABLE"],"automationPaused":null,"globalFence":"unknown"}'
    journal="$(ops_journal_snapshot)" || journal='{"complete":false,"events":[],"errors":["JOURNAL_UNAVAILABLE"],"truncated":false}'
    build='{}'
    if ops_file_safe "$OPS_APP/web-new/build.json" 8192; then
        build="$(jq -c -L "$OPS_APP/lib" 'include "operation-report-public"; build_public' "$OPS_APP/web-new/build.json" 2>/dev/null)" || build='{}'
    fi
    arch="$(uname -m)"
    case "$arch" in aarch64|armv7l|mips|mipsel|x86_64) ;; *) arch=unknown ;; esac
    kernel="$(uname -r 2>/dev/null)"
    case "$kernel" in ''|*[!A-Za-z0-9._+-]*) kernel='' ;; [0-9]*) ;; *) kernel='' ;; esac
    [ "${#kernel}" -le 64 ] || kernel=''
    uptime="$(awk 'NR==1 && $1~/^[0-9]+[.][0-9]+$/ {if($1>=0 && $1<3155760000) printf "%.0f",int($1)}' "$OPS_PROC/uptime" 2>/dev/null)"
    case "$uptime" in ''|*[!0-9]*) uptime=null ;; esac
    request=absent
    if [ -e "$OPS_UPDATER/request.lock" ] || [ -L "$OPS_UPDATER/request.lock" ]; then request=present; fi
    pending=clear; ops_pending_domain && pending=pending
    automation="$(ops_report_automation)" || automation='{"autoSwitch":null,"serverCheck":null,"subscriptionUpdate":null,"complete":false,"errors":["AUTOMATION_SETTINGS_UNAVAILABLE"]}'
    services="$(ops_report_services)" || services='[]'
    report="$(jq -nc --arg now "$(ops_now)" --arg arch "$arch" --argjson uptime "$uptime" \
      --argjson build "$build" --argjson snapshot "$snapshot" --argjson journal "$journal" \
      --arg kernel "$kernel" --argjson automation "$automation" --argjson services "$services" \
      --arg request "$request" --arg pending "$pending" '
      {schemaVersion:1,reportKind:"broray-diagnostics",capturedAt:$now,redactionPolicy:"allowlist-v1",
       complete:false,unavailable:(["keeneticOS","serviceIdentities","vpnContinuity"]+
         (if $kernel=="" then ["kernel"] else [] end)+
         (if $automation.complete then [] else ["automationSettings"] end)),
       build:{appVersion:$build.appVersion,candidateId:$build.candidateId,webuiBuild:$build.webuiBuild},
       platform:{architecture:$arch,kernel:(if $kernel=="" then null else $kernel end),uptimeSeconds:$uptime},
       services:{xray:"unknown",scheduler:([$services[]|select(.service=="subscriptions")|.state][0] // "unknown"),updater:"unknown"},
       serviceDetails:$services,
       automation:{paused:$snapshot.automationPaused,subscriptionUpdate:$automation.subscriptionUpdate,
         serverCheck:$automation.serverCheck,autoSwitch:$automation.autoSwitch},
       automationDetails:($automation|del(.autoSwitch,.serverCheck,.subscriptionUpdate)),
       operations:([$snapshot.operations[] | select(.running!=false)] + [$snapshot.operations[] | select(.running==false)][0:20]),
       fences:{global:$snapshot.globalFence,updaterRequest:$request,domainPending:$pending},
       events:$journal.events,journal:{complete:$journal.complete,truncated:$journal.truncated},
       snapshotComplete:$snapshot.complete,errors:(($snapshot.errors+$journal.errors+$automation.errors+
         [$services[]|select(.complete!=true)|.errorCode])|unique)}')" || return 1
    [ "$(printf '%s' "$report" | wc -c)" -le 1048576 ] || return 1
    printf '%s\n' "$report"
}
