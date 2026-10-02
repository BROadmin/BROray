#!/opt/bin/ash
# Identity snapshots only. This module never sends process signals.

broray_ops_supervisor_ledger_view()
(
    ledger="$1"; mode="${2:-live}"
    case "$mode" in live|finished|absent) ;; *) exit 1 ;; esac
    [ -f "$ledger" ] && [ ! -L "$ledger" ] || exit 1
    policy="$(jq -er '.ledgerPolicy // "legacy"' "$ledger")" || exit 1
    if [ "$policy" = legacy ]; then
        [ ! -e "$ledger.anchor" ] && [ ! -L "$ledger.anchor" ] &&
          [ ! -e "$ledger.current" ] && [ ! -L "$ledger.current" ] &&
          [ ! -e "$ledger.terminal" ] && [ ! -L "$ledger.terminal" ] || exit 1
        printf '%s\n' "$ledger"; exit 0
    fi
    [ "$policy" = sealed-boundaries/2 ] || exit 1
    for record in "$ledger" "$ledger.current"; do
        [ -f "$record" ] && [ ! -L "$record" ] || exit 1
        [ "$(broray_ops_file_stat -c '%u:%a:%h' "$record")" = '0:600:1' ] || exit 1
        [ "$(wc -c <"$record")" -le 65536 ] || exit 1
    done
    jq -e --slurpfile anchor "$ledger" '
      . as $current | ($anchor|length)==1 and
      $anchor[0]==({schemaVersion:1,ledgerPolicy:"sealed-boundaries/2",
        operationId:$current.operationId,supervisorId:$current.supervisorId,
        supervisorPid:$current.supervisorPid,supervisorStartTicks:$current.supervisorStartTicks,
        bootId:$current.bootId,revision:1,state:"gated",termSent:false,killTriggered:false,children:[]}) and
      .schemaVersion==1 and .ledgerPolicy=="sealed-boundaries/2" and (.revision|type)=="number" and .revision>=1
    ' "$ledger.current" >/dev/null || exit 1
    # An interrupted tracer may leave a valid nonterminal projection. It is
    # evidence for the caller's fresh /proc absence proof, never successful
    # completion. Requiring a completion record after SIGKILL would strand
    # every such operation. Final states always require their sealed record.
    if [ "$mode" = absent ]; then
        case "$(jq -er .state "$ledger.current")" in
            gated|running|stopping|armed) mode=live ;;
            *) mode=finished ;;
        esac
    fi
    if [ "$mode" = finished ]; then
        [ -f "$ledger.terminal" ] && [ ! -L "$ledger.terminal" ] || exit 1
        [ "$(broray_ops_file_stat -c '%u:%a:%h' "$ledger.terminal")" = '0:600:1' ] || exit 1
        [ "$(wc -c <"$ledger.terminal")" -le 65536 ] || exit 1
        cmp -s "$ledger.current" "$ledger.terminal" || exit 1
        printf '%s\n' "$ledger.terminal"
    else
        printf '%s\n' "$ledger.current"
    fi
)

broray_ops_owner_valid()
{
    jq -e 'type=="object" and (.pid|type)=="number" and .pid>1 and
      (.startTicks|type)=="string" and (.startTicks|length)>0 and (.startTicks|all(explode[]; .>=48 and .<=57)) and
      (.bootId|type)=="string" and (.bootId|length)>0 and
      (.executable|type)=="string" and (.executable|startswith("/")) and
      (.commandDigest|type)=="string" and (.commandDigest|length)==64 and
      (.commandDigest|all(explode[]; (.>=48 and .<=57) or (.>=97 and .<=102)))' >/dev/null 2>&1
}

broray_ops_start_ticks()
{
    awk 'NR==1 { s=$0; sub(/^.*\) /,"",s); n=split(s,a," ");
      if(n>=20 && a[20]~/^[0-9]+$/ && a[20]!="0") print a[20] }' "$1/stat" 2>/dev/null
}

broray_ops_boot_id()
{
    [ -d "$OPS_PROC" ] && [ ! -L "$OPS_PROC" ] || return 1
    [ -f "$OPS_PROC/sys/kernel/random/boot_id" ] && [ ! -L "$OPS_PROC/sys/kernel/random/boot_id" ] || return 1
    head -c 128 "$OPS_PROC/sys/kernel/random/boot_id" | tr -d '\r\n'
}

broray_ops_capture_owner()
{
    local pid start1 start2 exe1 exe2 cmd1 cmd2 boot snapshot status
    pid="${1:-}"
    case "$pid" in ''|*[!0-9]*|0|1) return 1 ;; esac
    # Explicit test fixture, never selected by a product HTTP request.
    if [ "${BRORAY_OPS_TEST:-0}" = 1 ] && [ "$OPS_APP" != /opt/broray ] &&
       [ -n "${BRORAY_OPS_TEST_IDENTITIES:-}" ]; then
        snapshot="$(jq -ce --arg pid "$pid" '.[$pid]' "$BRORAY_OPS_TEST_IDENTITIES" 2>/dev/null)" || return 1
        status="$(printf '%s\n' "$snapshot" | jq -r '.status // "present"')"
        [ "$status" != absent ] || return 2
        [ "$status" = present ] || return 1
        printf '%s\n' "$snapshot" | broray_ops_owner_valid || return 1
        printf '%s\n' "$snapshot" | jq -c 'del(.status)'
        return 0
    fi
    boot="$(broray_ops_boot_id)" || return 1
    [ -n "$boot" ] || return 1
    if [ ! -e "$OPS_PROC/$pid" ] && [ ! -L "$OPS_PROC/$pid" ]; then
        kill -0 "$pid" 2>/dev/null && return 1
        return 2
    fi
    [ -d "$OPS_PROC/$pid" ] && [ ! -L "$OPS_PROC/$pid" ] || return 1
    start1="$(broray_ops_start_ticks "$OPS_PROC/$pid")" || return 1
    [ -n "$start1" ] || return 1
    exe1="$(readlink -f "$OPS_PROC/$pid/exe")" || return 1
    [ -r "$OPS_PROC/$pid/cmdline" ] || return 1
    cmd1="$(sha256sum "$OPS_PROC/$pid/cmdline" | awk '{print $1}')" || return 1
    start2="$(broray_ops_start_ticks "$OPS_PROC/$pid")" || return 1
    exe2="$(readlink -f "$OPS_PROC/$pid/exe")" || return 1
    cmd2="$(sha256sum "$OPS_PROC/$pid/cmdline" | awk '{print $1}')" || return 1
    [ "$start1:$exe1:$cmd1" = "$start2:$exe2:$cmd2" ] || return 1
    jq -nc --argjson pid "$pid" --arg start "$start1" --arg boot "$boot" --arg exe "$exe1" --arg cmd "$cmd1" \
      '{pid:$pid,startTicks:$start,bootId:$boot,executable:$exe,commandDigest:$cmd}'
}

broray_ops_classify_owner()
{
    local expected pid live rc boot comparison
    expected="$1"
    OPS_OWNER_STATUS=AMBIGUOUS
    OPS_OWNER_REASON=invalid_identity
    printf '%s\n' "$expected" | broray_ops_owner_valid || return 0
    boot="$(broray_ops_boot_id)" || return 0
    [ -n "$boot" ] || return 0
    pid="$(printf '%s\n' "$expected" | jq -r --arg boot "$boot" '
      if .bootId!=$boot then "previous_boot" else (.pid|tostring) end')"
    if [ "$pid" = previous_boot ]; then
        OPS_OWNER_STATUS=STALE; OPS_OWNER_REASON=previous_boot; return 0
    fi
    rc=0; live="$(broray_ops_capture_owner "$pid")" || rc=$?
    if [ "$rc" = 2 ]; then OPS_OWNER_STATUS=STALE; OPS_OWNER_REASON=absent; return 0; fi
    [ "$rc" = 0 ] || { OPS_OWNER_REASON=process_unreadable; return 0; }
    # Compare the same validated identities in one query. Capture remains a
    # fresh double /proc snapshot; no PID, executable or command check is lost.
    comparison="$(jq -nr --argjson a "$expected" --argjson b "$live" '
      if $a.startTicks!=$b.startTicks then "pid_reused"
      elif $a==$b then "identity_matches" else "identity_changed" end')" || comparison=identity_changed
    case "$comparison" in
      pid_reused) OPS_OWNER_STATUS=STALE; OPS_OWNER_REASON=pid_reused ;;
      identity_matches) OPS_OWNER_STATUS=ACTIVE; OPS_OWNER_REASON=identity_matches ;;
      *) OPS_OWNER_REASON=identity_changed ;;
    esac
}

# File identity uses the same BusyBox terse fields as protected platform checks.
broray_ops_file_stat()
{
    # Entware may omit standalone stat and BusyBox FEATURE_STAT_FORMAT.
    # An installed stat's failure remains authoritative; never mask it.
    if command -v stat >/dev/null 2>&1; then
        stat "$@"
        return $?
    fi
    # Keep fallback scratch variables/options isolated, without spawning an
    # extra shell for every metadata read on systems with standalone stat.
(
    follow=''
    if [ "${1:-}" = -L ]; then follow=-L; shift; fi
    [ "$#" -ge 3 ] && [ "$1" = -c ] || exit 75
    format="$2"; shift 2
    case "$format" in '%a'|'%a:%u'|'%u:%a'|'%u:%a:%h'|'%s:%u:%a:%h'|'%u %a %h %s') ;; *) exit 75 ;; esac
    for path in "$@"
    do
        if [ -n "$follow" ]; then
            row="$(busybox stat -L -t "$path")" || exit 75
        else
            row="$(busybox stat -t "$path")" || exit 75
        fi
        # Strip the exact filename first so embedded spaces do not shift fields.
        case "$row" in "$path "*) fields="${row#"$path "}" ;; *) exit 75 ;; esac
        set -f
        set -- $fields
        [ "$#" -eq 14 ] || exit 75
        size="$1"; raw_mode="$3"; owner="$4"; links="$8"
        case "$raw_mode" in ''|*[!0-9a-fA-F]*) exit 75 ;; esac
        [ "${#raw_mode}" -le 8 ] || exit 75
        case "$size" in ''|*[!0-9]*) exit 75 ;; esac
        case "$owner" in ''|*[!0-9]*) exit 75 ;; esac
        case "$links" in ''|*[!0-9]*) exit 75 ;; esac
        mode="$((0x$raw_mode & 07777))"
        case "$format" in
            '%a') printf '%o\n' "$mode" ;;
            '%u:%a') printf '%s:%o\n' "$owner" "$mode" ;;
            '%s:%u:%a:%h') printf '%s:%s:%o:%s\n' "$size" "$owner" "$mode" "$links" ;;
            '%u %a %h %s') printf '%s %o %s %s\n' "$owner" "$mode" "$links" "$size" ;;
            '%a:%u') printf '%o:%s\n' "$mode" "$owner" ;;
            '%u:%a:%h') printf '%s:%o:%s\n' "$owner" "$mode" "$links" ;;
        esac
    done
)
}
