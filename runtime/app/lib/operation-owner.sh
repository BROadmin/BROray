#!/opt/bin/ash
# Identity snapshots only. This module never sends process signals.

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
    local expected pid live rc boot
    expected="$1"
    OPS_OWNER_STATUS=AMBIGUOUS
    OPS_OWNER_REASON=invalid_identity
    printf '%s\n' "$expected" | broray_ops_owner_valid || return 0
    boot="$(broray_ops_boot_id)" || return 0
    [ -n "$boot" ] || return 0
    if [ "$(printf '%s\n' "$expected" | jq -r '.bootId')" != "$boot" ]; then
        OPS_OWNER_STATUS=STALE; OPS_OWNER_REASON=previous_boot; return 0
    fi
    pid="$(printf '%s\n' "$expected" | jq -r '.pid')"
    rc=0; live="$(broray_ops_capture_owner "$pid")" || rc=$?
    if [ "$rc" = 2 ]; then OPS_OWNER_STATUS=STALE; OPS_OWNER_REASON=absent; return 0; fi
    [ "$rc" = 0 ] || { OPS_OWNER_REASON=process_unreadable; return 0; }
    if [ "$(printf '%s\n' "$expected" | jq -r '.startTicks')" != "$(printf '%s\n' "$live" | jq -r '.startTicks')" ]; then
        OPS_OWNER_STATUS=STALE; OPS_OWNER_REASON=pid_reused; return 0
    fi
    if jq -en --argjson a "$expected" --argjson b "$live" '$a==$b' >/dev/null 2>&1; then
        OPS_OWNER_STATUS=ACTIVE; OPS_OWNER_REASON=identity_matches
    else
        OPS_OWNER_REASON=identity_changed
    fi
}
