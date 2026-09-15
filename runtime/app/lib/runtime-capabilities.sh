#!/opt/bin/ash
# BROray 3.0.0-r14 runtime capability contract.
#
# This file is intentionally stateless.  It validates the operations used by
# the current invocation and writes evidence only below that invocation's
# /tmp workspace.  Utility names, versions, router models and source package
# versions are never used as compatibility selectors.

BRORAY_RUNTIME_CAPABILITY_CONTRACT="keenetic-entware-capabilities/1"
BRORAY_RUNTIME_FAILURE_ID=""
BRORAY_RUNTIME_FAILURE_TOOL=""
BRORAY_RUNTIME_FAILURE_OPERATION=""

broray_runtime_path_init()
{
    if [ -n "${BRORAY_RUNTIME_PATH:-}" ]; then
        [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "${BRORAY_TX_FS_ROOT:-/}" != / ] || return 1
        PATH="$BRORAY_RUNTIME_PATH"
    else
        # Keenetic's official OPKG environment documents all four Entware
        # locations.  Keep their documented precedence and never select tools
        # by router model, firmware version or utility version text.
        PATH="/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin"
    fi
    export PATH
    LC_ALL=C
    export LC_ALL
}

broray_runtime_safe_field()
{
    printf '%s\n' "${1:-}" | awk 'NR>1 || index($0,"\t") || index($0,"\r") {bad=1} END{exit bad?1:0}'
}

broray_runtime_record()
{
    broray_runtime_record_id="$1"
    broray_runtime_record_tool="$2"
    broray_runtime_record_path="$3"
    broray_runtime_record_operation="$4"
    broray_runtime_record_rc="$5"
    broray_runtime_record_result="$6"
    broray_runtime_record_argv_dir="${7:-}"
    broray_runtime_record_stdout="${8:-$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capability-empty-stream}"
    broray_runtime_record_stderr="${9:-$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capability-empty-stream}"
    broray_runtime_safe_field "$broray_runtime_record_id" || return 1
    broray_runtime_safe_field "$broray_runtime_record_tool" || return 1
    broray_runtime_safe_field "$broray_runtime_record_path" || return 1
    broray_runtime_safe_field "$broray_runtime_record_operation" || return 1
    [ -d "$broray_runtime_record_argv_dir" ] && [ ! -L "$broray_runtime_record_argv_dir" ] || return 1
    [ -f "$broray_runtime_record_stdout" ] && [ ! -L "$broray_runtime_record_stdout" ] || return 1
    [ -f "$broray_runtime_record_stderr" ] && [ ! -L "$broray_runtime_record_stderr" ] || return 1
    broray_runtime_safe_field "$broray_runtime_record_argv_dir" || return 1
    broray_runtime_safe_field "$broray_runtime_record_stdout" || return 1
    broray_runtime_safe_field "$broray_runtime_record_stderr" || return 1
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$broray_runtime_record_id" "$broray_runtime_record_tool" \
        "$broray_runtime_record_path" "$broray_runtime_record_operation" \
        "$broray_runtime_record_rc" "$broray_runtime_record_result" \
        "$broray_runtime_record_argv_dir" "$broray_runtime_record_stdout" "$broray_runtime_record_stderr" \
        >>"$BRORAY_RUNTIME_CAPABILITY_TSV"
}

# Store argv as one newline-delimited argument file per actual invocation.
# Arguments containing CR/LF/TAB are rejected instead of being ambiguously
# escaped.  The files are converted to JSON arrays only after jq itself has
# passed, so even the early command-resolution rows are evidence-bearing.
broray_runtime_argv_reset()
{
    broray_runtime_argv_id="$1"
    case "$broray_runtime_argv_id" in ''|*[!0-9A-Za-z._-]*) return 1 ;; esac
    broray_runtime_argv_dir="$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capability-argv/$broray_runtime_argv_id"
    rm -rf "$broray_runtime_argv_dir" || return 1
    mkdir -p "$broray_runtime_argv_dir" || return 1
    BRORAY_RUNTIME_ARGV_DIR="$broray_runtime_argv_dir"
    BRORAY_RUNTIME_ARGV_INDEX=0
}

broray_runtime_argv_add()
{
    [ -d "${BRORAY_RUNTIME_ARGV_DIR:-}" ] && [ ! -L "$BRORAY_RUNTIME_ARGV_DIR" ] || return 1
    BRORAY_RUNTIME_ARGV_INDEX=$((BRORAY_RUNTIME_ARGV_INDEX + 1))
    broray_runtime_argv_file="$BRORAY_RUNTIME_ARGV_DIR/$BRORAY_RUNTIME_ARGV_INDEX.args"
    : >"$broray_runtime_argv_file" || return 1
    for broray_runtime_argv_arg in "$@"; do
        broray_runtime_safe_field "$broray_runtime_argv_arg" || return 1
        printf '%s\n' "$broray_runtime_argv_arg" >>"$broray_runtime_argv_file" || return 1
    done
}

broray_runtime_stream_select()
{
    BRORAY_RUNTIME_STDOUT_FILE="${1:-$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capability-empty-stream}"
    BRORAY_RUNTIME_STDERR_FILE="${2:-$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capability-empty-stream}"
}

broray_runtime_probe_evidence_prepare()
{
    broray_runtime_probe_evidence_id="$1"
    broray_runtime_argv_reset "$broray_runtime_probe_evidence_id" || return 1
    broray_runtime_stream_select
    case "$broray_runtime_probe_evidence_id" in
        mount.contract)
            if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ]; then
                broray_runtime_argv_add test -d "${BRORAY_TX_OPT_ROOT:-/opt}" || return 1
                broray_runtime_argv_add test -d "${BRORAY_TX_TMP_BASE:-/tmp}" || return 1
                broray_runtime_argv_add test -w "${BRORAY_TX_TMP_BASE:-/tmp}" || return 1
                broray_runtime_argv_add printf 'lab-opt|lab-opt-dev|/|%s|rw|fixture-opt\n' "${BRORAY_TX_OPT_ROOT:-/opt}" || return 1
                broray_runtime_argv_add printf 'lab-tmp|lab-tmp-dev|/|%s|rw|fixture-tmp\n' "${BRORAY_TX_TMP_BASE:-/tmp}" || return 1
                broray_runtime_argv_add printf '%s\n' lab-fixture || return 1
            else
                broray_runtime_argv_add "$BRORAY_CAP_READLINK" "${BRORAY_TX_OPT_ROOT:-/opt}/tmp" || return 1
                broray_runtime_argv_add "$BRORAY_CAP_READLINK" -f "${BRORAY_TX_OPT_ROOT:-/opt}/tmp" || return 1
                broray_runtime_argv_add "$BRORAY_CAP_READLINK" -f "${BRORAY_TX_OPT_ROOT:-/opt}" || return 1
                broray_runtime_argv_add "$BRORAY_CAP_READLINK" -f "${BRORAY_TX_TMP_BASE:-/tmp}" || return 1
            fi
            broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/opt.mount"
            ;;
        allocation.unit.contract)
            broray_runtime_argv_add du -sk "${BRORAY_TX_OPT_ROOT:-/opt}/.broray-capability-allocation.$$" || return 1
            broray_runtime_argv_add du -sk "${BRORAY_TX_TMP_BASE:-/tmp}/.broray-capability-allocation.$$" || return 1
            ;;
        ash.syntax) broray_runtime_argv_add "${BRORAY_TX_ASH:-/opt/bin/ash}" -n "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.sh" ;;
        awk.contract) broray_runtime_argv_add "$BRORAY_CAP_AWK" -F: -v expected=2 '$1=="alpha" && $2==expected {ok=1} END{exit ok?0:1}' ;;
        basename.contract) broray_runtime_argv_add basename /tmp/broray/probe.txt ;;
        dirname.contract) broray_runtime_argv_add dirname /tmp/broray/probe.txt ;;
        cat.contract) broray_runtime_argv_add cat "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample"; broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/cat.out" ;;
        chmod.contract) broray_runtime_argv_add chmod 600 "$BRORAY_RUNTIME_CAPABILITY_PROBE/cat.out" ;;
        cp.contract)
            broray_runtime_argv_add cp -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/cat.out" "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy.out" || return 1
            broray_runtime_argv_add cp -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/cat.out" "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy-tree/source/value" || return 1
            broray_runtime_argv_add cp -pR "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy-tree/source" "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy-tree/destination" || return 1
            ;;
        cut.contract) broray_runtime_argv_add cut -d: -f2 ;;
        date.contract)
            broray_runtime_argv_add date +%s || return 1
            broray_runtime_argv_add date -u +%Y-%m-%dT%H:%M:%SZ || return 1
            broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/date.out"
            ;;
        df.contract) broray_runtime_argv_add df -Pk "$BRORAY_RUNTIME_CAPABILITY_PROBE"; broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/df.out" ;;
        du.contract) broray_runtime_argv_add du -sk "$BRORAY_RUNTIME_CAPABILITY_PROBE/source"; broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/du.out" ;;
        tail.contract) broray_runtime_argv_add tail -n 1 "$BRORAY_RUNTIME_CAPABILITY_PROBE/head-tail.in" ;;
        ip.contract) broray_runtime_argv_add ip -4 addr show; broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/ip.out" "$BRORAY_RUNTIME_CAPABILITY_PROBE/ip.stderr" ;;
        ln.contract)
            broray_runtime_argv_add ln "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample" "$BRORAY_RUNTIME_CAPABILITY_PROBE/hardlink" || return 1
            broray_runtime_argv_add ln -s source/sub/sample "$BRORAY_RUNTIME_CAPABILITY_PROBE/link" || return 1
            ;;
        mktemp.contract) broray_runtime_argv_add mktemp -d "$BRORAY_RUNTIME_CAPABILITY_PROBE/mktemp.XXXXXX" ;;
        mkdir.contract)
            broray_runtime_argv_add mkdir "$broray_runtime_mktemp/move-source" || return 1
            broray_runtime_argv_add mkdir -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy-tree/source" || return 1
            ;;
        mv.contract) broray_runtime_argv_add mv "$broray_runtime_mktemp/move-source" "$broray_runtime_mktemp/move-target" ;;
        rmdir.contract)
            broray_runtime_argv_add rmdir "$broray_runtime_mktemp/move-target" || return 1
            broray_runtime_argv_add rmdir "$broray_runtime_mktemp" || return 1
            ;;
        od.contract) broray_runtime_argv_add od -An -tu1 -v "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample"; broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/od.out" ;;
        rm.contract)
            broray_runtime_argv_add rm -f "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy.out" || return 1
            broray_runtime_argv_add rm -rf "$BRORAY_RUNTIME_CAPABILITY_PROBE/rm-tree" || return 1
            ;;
        sleep.contract) broray_runtime_argv_add sleep 0 ;;
        start-stop-daemon.contract)
            broray_runtime_argv_add start-stop-daemon -S -b -m -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/daemon.pid" -x "${BRORAY_TX_ASH:-/opt/bin/ash}" -- "$BRORAY_RUNTIME_CAPABILITY_PROBE/daemon-body.sh"
            broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/start-stop-daemon.out" "$BRORAY_RUNTIME_CAPABILITY_PROBE/start-stop-daemon.stderr"
            ;;
        test.contract) broray_runtime_argv_add test -f "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample" ;;
        kill.contract) broray_runtime_argv_add "${BRORAY_TX_ASH:-/opt/bin/ash}" -c 'kill -0 "$$"' ;;
        ash.ulimit-file.contract)
            broray_runtime_argv_add "${BRORAY_TX_ASH:-/opt/bin/ash}" -c \
                'ulimit -S -f 64 && [ "$(ulimit -S -f)" -eq 64 ]'
            ;;
        tr.contract) broray_runtime_argv_add tr -d ' ' ;;
        wc.contract) broray_runtime_argv_add wc -c ;;
        sed.contract) broray_runtime_argv_add "$BRORAY_CAP_SED" -n -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e '/^alpha$/p' ;;
        grep.contract) broray_runtime_argv_add "$BRORAY_CAP_GREP" -Fxq alpha ;;
        find.contract)
            broray_runtime_argv_add "$BRORAY_CAP_FIND" -P "$BRORAY_RUNTIME_CAPABILITY_PROBE/source" -xdev -mindepth 1 -maxdepth 2 -type f -name sample -path '*/sample' -print -printf '%m|%U|%G|%D|%i|%n|%b|%s|%p\n' -exec test -f '{}' ';'
            broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/find.out"
            ;;
        sort.contract)
            broray_runtime_argv_add "$BRORAY_CAP_SORT" -u "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.in" || return 1
            broray_runtime_argv_add "$BRORAY_CAP_SORT" -r "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.in" || return 1
            broray_runtime_argv_add "$BRORAY_CAP_SORT" -n || return 1
            broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.numeric"
            ;;
        readlink.contract)
            broray_runtime_argv_add "$BRORAY_CAP_READLINK" "$BRORAY_RUNTIME_CAPABILITY_PROBE/link" || return 1
            broray_runtime_argv_add "$BRORAY_CAP_READLINK" -f "$BRORAY_RUNTIME_CAPABILITY_PROBE/link" || return 1
            ;;
        sha256sum.contract)
            broray_runtime_argv_add "$BRORAY_CAP_SHA256SUM" source/sub/sample || return 1
            broray_runtime_argv_add "$BRORAY_CAP_SHA256SUM" -c sample.sha256 || return 1
            broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/sample.sha256"
            ;;
        tar.contract)
            broray_runtime_argv_add "$BRORAY_CAP_TAR" --format=gnu --blocking-factor=1 -czf "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.tar.gz" -C "$BRORAY_RUNTIME_CAPABILITY_PROBE/source" sub/sample || return 1
            broray_runtime_argv_add "$BRORAY_CAP_TAR" -tzf "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.tar.gz" || return 1
            broray_runtime_argv_add "$BRORAY_CAP_TAR" -tvzf "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.tar.gz" || return 1
            broray_runtime_argv_add "$BRORAY_CAP_TAR" -xzOf "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.tar.gz" sub/sample || return 1
            broray_runtime_argv_add "$BRORAY_CAP_TAR" -xzf "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.tar.gz" -C "$BRORAY_RUNTIME_CAPABILITY_PROBE/extract" || return 1
            broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/tar.stdout"
            ;;
        gzip.contract)
            broray_runtime_argv_add "$BRORAY_CAP_GZIP" -c "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample" || return 1
            broray_runtime_argv_add "$BRORAY_CAP_GZIP" -t "$BRORAY_RUNTIME_CAPABILITY_PROBE/sample.gz" || return 1
            broray_runtime_argv_add "$BRORAY_CAP_GZIP" -dc "$BRORAY_RUNTIME_CAPABILITY_PROBE/sample.gz" || return 1
            broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/sample.unpacked"
            ;;
        mkfifo.contract) broray_runtime_argv_add "$BRORAY_CAP_MKFIFO" "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.fifo" ;;
        dd.contract)
            broray_runtime_argv_add "$BRORAY_CAP_DD" if=/dev/zero of="$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.fifo" bs=1024 count=1 || return 1
            broray_runtime_argv_add "$BRORAY_CAP_DD" if="$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.fifo" of="$BRORAY_RUNTIME_CAPABILITY_PROBE/dd.out" bs=1024 count=1 iflag=fullblock || return 1
            ;;
        jq) broray_runtime_argv_add jq -e '.probe==true' ;;
        jq.contract)
            broray_runtime_argv_add "$BRORAY_CAP_JQ" -nc --arg value probe --argjson enabled true '{value:$value,enabled:$enabled}' || return 1
            broray_runtime_argv_add "$BRORAY_CAP_JQ" -e '.value=="probe" and .enabled==true' || return 1
            ;;
        jq.sha256-core) broray_runtime_argv_add "$BRORAY_CAP_JQ" -e "$broray_runtime_jq_sha_program" ;;
        curl.resolution) broray_runtime_argv_add command -v curl ;;
        sync.contract) broray_runtime_argv_add sync ;;
        opkg.contract)
            broray_runtime_argv_add "${BRORAY_TX_OPKG:-opkg}" print-architecture
            broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/opkg.arch" "$BRORAY_RUNTIME_CAPABILITY_PROBE/opkg.arch.stderr"
            ;;
        ndmc.read-only)
            if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ -n "${BRORAY_TX_TEST_KEENETIC_STATE:-}" ]; then
                broray_runtime_argv_add cat "$BRORAY_TX_TEST_KEENETIC_STATE" || return 1
            else
                broray_runtime_argv_add ndmc -c 'show running-config' || return 1
            fi
            broray_runtime_stream_select "$BRORAY_RUNTIME_CAPABILITY_PROBE/ndmc-running-config" "$BRORAY_RUNTIME_CAPABILITY_PROBE/ndmc-running-config.stderr"
            ;;
        *) return 1 ;;
    esac
}

broray_runtime_failure_write()
{
    BRORAY_RUNTIME_FAILURE_ID="$1"
    BRORAY_RUNTIME_FAILURE_TOOL="$2"
    BRORAY_RUNTIME_FAILURE_OPERATION="$3"
    printf 'contract=%s\ncapability=%s\ntool=%s\noperation=%s\nmutationStarted=false\n' \
        "$BRORAY_RUNTIME_CAPABILITY_CONTRACT" "$BRORAY_RUNTIME_FAILURE_ID" \
        "$BRORAY_RUNTIME_FAILURE_TOOL" "$BRORAY_RUNTIME_FAILURE_OPERATION" \
        >"$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capability-failure.env" 2>/dev/null || true
    return 1
}

broray_runtime_command_path()
{
    broray_runtime_command_name="$1"
    case "$broray_runtime_command_name" in
        /*)
            [ -x "$broray_runtime_command_name" ] && [ ! -d "$broray_runtime_command_name" ] || return 1
            printf '%s\n' "$broray_runtime_command_name"
            return 0
            ;;
        */*) return 1 ;;
    esac
    broray_runtime_command_old_ifs="$IFS"
    IFS=:
    for broray_runtime_command_dir in $PATH; do
        [ -n "$broray_runtime_command_dir" ] || broray_runtime_command_dir=.
        if [ -x "$broray_runtime_command_dir/$broray_runtime_command_name" ] && [ ! -d "$broray_runtime_command_dir/$broray_runtime_command_name" ]; then
            IFS="$broray_runtime_command_old_ifs"
            printf '%s\n' "$broray_runtime_command_dir/$broray_runtime_command_name"
            return 0
        fi
    done
    IFS="$broray_runtime_command_old_ifs"
    broray_runtime_command_result="$(command -v "$broray_runtime_command_name" 2>/dev/null)" || return 1
    case "$broray_runtime_command_result" in
        /*) printf '%s\n' "$broray_runtime_command_result" ;;
        "$broray_runtime_command_name") printf '%s\n' "$broray_runtime_command_result" ;;
        *) return 1 ;;
    esac
}

broray_runtime_resolve_release_tools()
{
    BRORAY_CAP_AWK="$(broray_runtime_command_path awk)" || return 1
    BRORAY_CAP_CURL="$(broray_runtime_command_path curl)" || return 1
    BRORAY_CAP_DD="$(broray_runtime_command_path dd)" || return 1
    BRORAY_CAP_FIND="$(broray_runtime_command_path find)" || return 1
    BRORAY_CAP_GREP="$(broray_runtime_command_path grep)" || return 1
    BRORAY_CAP_GZIP="$(broray_runtime_command_path gzip)" || return 1
    BRORAY_CAP_JQ="$(broray_runtime_command_path jq)" || return 1
    BRORAY_CAP_MKFIFO="$(broray_runtime_command_path mkfifo)" || return 1
    BRORAY_CAP_READLINK="$(broray_runtime_command_path readlink)" || return 1
    BRORAY_CAP_SED="$(broray_runtime_command_path sed)" || return 1
    BRORAY_CAP_SHA256SUM="$(broray_runtime_command_path sha256sum)" || return 1
    BRORAY_CAP_SORT="$(broray_runtime_command_path sort)" || return 1
    BRORAY_CAP_TAR="$(broray_runtime_command_path tar)" || return 1
    export BRORAY_CAP_AWK BRORAY_CAP_CURL BRORAY_CAP_DD BRORAY_CAP_FIND BRORAY_CAP_GREP
    export BRORAY_CAP_GZIP BRORAY_CAP_JQ BRORAY_CAP_MKFIFO BRORAY_CAP_READLINK
    export BRORAY_CAP_SED BRORAY_CAP_SHA256SUM BRORAY_CAP_SORT BRORAY_CAP_TAR
}

broray_runtime_activate_if_resolved()
{
    [ -n "${BRORAY_CAP_FIND:-}" ] || return 0
    awk() { "$BRORAY_CAP_AWK" "$@"; }
    curl() { "$BRORAY_CAP_CURL" "$@"; }
    dd() { "$BRORAY_CAP_DD" "$@"; }
    find() { "$BRORAY_CAP_FIND" "$@"; }
    grep() { "$BRORAY_CAP_GREP" "$@"; }
    gzip() { "$BRORAY_CAP_GZIP" "$@"; }
    jq() { "$BRORAY_CAP_JQ" "$@"; }
    mkfifo() { "$BRORAY_CAP_MKFIFO" "$@"; }
    readlink() { "$BRORAY_CAP_READLINK" "$@"; }
    sed() { "$BRORAY_CAP_SED" "$@"; }
    sha256sum() { "$BRORAY_CAP_SHA256SUM" "$@"; }
    sort() { "$BRORAY_CAP_SORT" "$@"; }
    tar() { "$BRORAY_CAP_TAR" "$@"; }
}

# Re-resolve the tools and bind them only when their exact paths match the
# machine-readable PASS evidence of this current operation.
broray_runtime_reactivate_current_operation()
{
    broray_runtime_reactivate_work="$1"
    # Re-establish the documented/test-guarded search path in this process
    # before any current-operation path is re-resolved.  Callers must not rely
    # on an inherited shell PATH surviving the hand-off.
    broray_runtime_path_init || return 1
    broray_runtime_reactivate_json="$broray_runtime_reactivate_work/evidence/capabilities.json"
    broray_runtime_reactivate_tsv="$broray_runtime_reactivate_work/evidence/capabilities.tsv"
    [ -f "$broray_runtime_reactivate_json" ] && [ ! -L "$broray_runtime_reactivate_json" ] || return 1
    [ -f "$broray_runtime_reactivate_tsv" ] && [ ! -L "$broray_runtime_reactivate_tsv" ] || return 1
    jq -e --arg contract "$BRORAY_RUNTIME_CAPABILITY_CONTRACT" \
        '.contract==$contract and .requirementsContract=="1.7.2" and
         .lifecycleContract=="current-operation-full-tmp-snapshot/1" and
         .capabilityContract=="keenetic-entware-capabilities/1" and .spaceContract=="broray-space/2" and
         .previousIpkRequired==false and .historicalTransactionStateRequired==false and .statelessBootstrap==true and
         ((.candidateBindingStage=="pre-candidate" and .candidateSha256==null) or
          (.candidateBindingStage=="candidate-verified" and
           (.candidateSha256|type)=="string" and (.candidateSha256|length)==64 and
           all(.candidateSha256|explode[];
               ((. >= 48) and (. <= 57)) or ((. >= 97) and (. <= 102))))) and
         (.result=="PREFLIGHT_PASS" or .result=="LAB_EXERCISED") and .mutationStarted==false and
         (.probes|length)>0 and all(.probes[];.pass)' \
        "$broray_runtime_reactivate_json" >/dev/null 2>&1 || return 1
    if [ "$(jq -r '.result' "$broray_runtime_reactivate_json")" = LAB_EXERCISED ]; then
        [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ "${BRORAY_TX_FS_ROOT:-/}" != / ] || return 1
    fi
    broray_runtime_resolve_release_tools || return 1
    for broray_runtime_reactivate_spec in \
        "awk:$BRORAY_CAP_AWK" "curl:$BRORAY_CAP_CURL" "dd:$BRORAY_CAP_DD" \
        "find:$BRORAY_CAP_FIND" "grep:$BRORAY_CAP_GREP" "gzip:$BRORAY_CAP_GZIP" \
        "jq:$BRORAY_CAP_JQ" "mkfifo:$BRORAY_CAP_MKFIFO" "readlink:$BRORAY_CAP_READLINK" \
        "sed:$BRORAY_CAP_SED" "sha256sum:$BRORAY_CAP_SHA256SUM" "sort:$BRORAY_CAP_SORT" \
        "tar:$BRORAY_CAP_TAR"
    do
        broray_runtime_reactivate_name="${broray_runtime_reactivate_spec%%:*}"
        broray_runtime_reactivate_actual="${broray_runtime_reactivate_spec#*:}"
        broray_runtime_reactivate_expected="$(awk -F '\t' -v id="command.$broray_runtime_reactivate_name" \
            '$1==id && $6=="true" {print $3; exit}' "$broray_runtime_reactivate_tsv")"
        [ -n "$broray_runtime_reactivate_expected" ] && \
            [ "$broray_runtime_reactivate_expected" = "$broray_runtime_reactivate_actual" ] || return 1
    done
    for broray_runtime_reactivate_name in \
        basename cat chmod cp cut date df dirname du ip ln mkdir mktemp mv od rm rmdir \
        sleep start-stop-daemon sync tail test tr wc
    do
        broray_runtime_reactivate_actual="$(broray_runtime_command_path "$broray_runtime_reactivate_name" 2>/dev/null)" || return 1
        broray_runtime_reactivate_expected="$(awk -F '\t' -v id="command.$broray_runtime_reactivate_name" \
            '$1==id && $6=="true" {print $3; exit}' "$broray_runtime_reactivate_tsv")"
        [ -n "$broray_runtime_reactivate_expected" ] && \
            [ "$broray_runtime_reactivate_expected" = "$broray_runtime_reactivate_actual" ] || return 1
    done
    broray_runtime_reactivate_ash_expected="$(awk -F '\t' '$1=="command.ash" && $6=="true" {print $3;exit}' "$broray_runtime_reactivate_tsv")"
    broray_runtime_reactivate_opkg_expected="$(awk -F '\t' '$1=="command.opkg" && $6=="true" {print $3;exit}' "$broray_runtime_reactivate_tsv")"
    [ -n "$broray_runtime_reactivate_ash_expected" ] && [ -x "$broray_runtime_reactivate_ash_expected" ] || return 1
    [ -n "$broray_runtime_reactivate_opkg_expected" ] && [ -x "$broray_runtime_reactivate_opkg_expected" ] || return 1
    [ "$(readlink -f "${BRORAY_TX_ASH:-/opt/bin/ash}" 2>/dev/null)" = \
      "$(readlink -f "$broray_runtime_reactivate_ash_expected" 2>/dev/null)" ] || return 1
    [ "$(readlink -f "$(broray_runtime_command_path "${BRORAY_TX_OPKG:-opkg}")" 2>/dev/null)" = \
      "$(readlink -f "$broray_runtime_reactivate_opkg_expected" 2>/dev/null)" ] || return 1
    BRORAY_TX_ASH="$broray_runtime_reactivate_ash_expected"
    BRORAY_TX_OPKG="$broray_runtime_reactivate_opkg_expected"
    export BRORAY_TX_ASH BRORAY_TX_OPKG
    broray_runtime_activate_if_resolved
}

broray_runtime_require_command()
{
    broray_runtime_require_name="$1"
    broray_runtime_argv_reset "command.$broray_runtime_require_name" || return 1
    broray_runtime_argv_add command -v "$broray_runtime_require_name" || return 1
    broray_runtime_require_path="$(broray_runtime_command_path "$broray_runtime_require_name")" || {
        broray_runtime_record "command.$broray_runtime_require_name" "$broray_runtime_require_name" "" command-v 127 false \
            "$BRORAY_RUNTIME_ARGV_DIR" || true
        broray_runtime_failure_write "command.$broray_runtime_require_name" "$broray_runtime_require_name" command-v
        return 1
    }
    printf '%s\n' "$broray_runtime_require_path" >"$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/command.$broray_runtime_require_name.stdout" || return 1
    broray_runtime_record "command.$broray_runtime_require_name" "$broray_runtime_require_name" \
        "$broray_runtime_require_path" command-v 0 true "$BRORAY_RUNTIME_ARGV_DIR" \
        "$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/command.$broray_runtime_require_name.stdout"
}

broray_runtime_probe_result()
{
    broray_runtime_probe_id="$1"
    broray_runtime_probe_tool="$2"
    broray_runtime_probe_operation="$3"
    broray_runtime_probe_rc="$4"
    case "$broray_runtime_probe_tool" in
        ash) broray_runtime_probe_path="${BRORAY_TX_ASH:-/opt/bin/ash}" ;;
        opkg) broray_runtime_probe_path="$(broray_runtime_command_path "${BRORAY_TX_OPKG:-opkg}" 2>/dev/null || printf '')" ;;
        *) broray_runtime_probe_path="$(broray_runtime_command_path "$broray_runtime_probe_tool" 2>/dev/null || printf '')" ;;
    esac
    broray_runtime_probe_evidence_prepare "$broray_runtime_probe_id" || {
        broray_runtime_failure_write "$broray_runtime_probe_id" "$broray_runtime_probe_tool" evidence-preparation
        return 1
    }
    if [ "${BRORAY_RUNTIME_FORCE_FAIL:-}" = "$broray_runtime_probe_id" ]; then
        broray_runtime_probe_rc=97
    fi
    if [ "$broray_runtime_probe_rc" -eq 0 ]; then
        broray_runtime_record "$broray_runtime_probe_id" "$broray_runtime_probe_tool" \
            "$broray_runtime_probe_path" "$broray_runtime_probe_operation" 0 true \
            "$BRORAY_RUNTIME_ARGV_DIR" "$BRORAY_RUNTIME_STDOUT_FILE" "$BRORAY_RUNTIME_STDERR_FILE"
        return 0
    fi
    broray_runtime_record "$broray_runtime_probe_id" "$broray_runtime_probe_tool" \
        "$broray_runtime_probe_path" "$broray_runtime_probe_operation" "$broray_runtime_probe_rc" false \
        "$BRORAY_RUNTIME_ARGV_DIR" "$BRORAY_RUNTIME_STDOUT_FILE" "$BRORAY_RUNTIME_STDERR_FILE" || true
    broray_runtime_failure_write "$broray_runtime_probe_id" "$broray_runtime_probe_tool" "$broray_runtime_probe_operation"
}

broray_runtime_evidence_finalize()
{
    broray_runtime_evidence_base="$BRORAY_RUNTIME_CAPABILITY_TSV"
    broray_runtime_evidence_final="$BRORAY_RUNTIME_CAPABILITY_TSV.final"
    : >"$broray_runtime_evidence_final" || return 1
    while IFS="$(printf '\t')" read -r broray_runtime_evidence_id broray_runtime_evidence_tool \
        broray_runtime_evidence_path broray_runtime_evidence_operation broray_runtime_evidence_rc \
        broray_runtime_evidence_pass broray_runtime_evidence_argv_dir broray_runtime_evidence_stdout \
        broray_runtime_evidence_stderr
    do
        [ -n "$broray_runtime_evidence_id" ] || return 1
        [ -d "$broray_runtime_evidence_argv_dir" ] && [ ! -L "$broray_runtime_evidence_argv_dir" ] || return 1
        broray_runtime_evidence_argv_rows="$broray_runtime_evidence_argv_dir/argv.ndjson"
        : >"$broray_runtime_evidence_argv_rows" || return 1
        for broray_runtime_evidence_args in "$broray_runtime_evidence_argv_dir"/*.args; do
            [ -f "$broray_runtime_evidence_args" ] && [ ! -L "$broray_runtime_evidence_args" ] || continue
            "$BRORAY_CAP_JQ" -Rsc 'split("\n")[:-1]' "$broray_runtime_evidence_args" \
                >>"$broray_runtime_evidence_argv_rows" || return 1
        done
        broray_runtime_evidence_argv_json="$("$BRORAY_CAP_JQ" -sc '.' "$broray_runtime_evidence_argv_rows")" || return 1
        # jq -c emits one compact JSON value; command substitution removes its
        # terminating newline.  Do not test LF with `$(printf '\n')`: POSIX
        # command substitution removes that byte and turns the pattern into
        # `*""*`, which matches every value and makes the preflight fail
        # unconditionally.  Literal CR/TAB remain forbidden, and a compact jq
        # value must be non-empty.
        [ -n "$broray_runtime_evidence_argv_json" ] || return 1
        case "$broray_runtime_evidence_argv_json" in *"$(printf '\t')"*|*"$(printf '\r')"*) return 1 ;; esac

        broray_runtime_evidence_canonical=""
        broray_runtime_evidence_recheck=false
        if [ "$broray_runtime_evidence_path" = test-fixture ]; then
            [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] || return 1
            broray_runtime_evidence_recheck=true
        elif [ -n "$broray_runtime_evidence_path" ]; then
            broray_runtime_evidence_canonical="$("$BRORAY_CAP_READLINK" -f "$broray_runtime_evidence_path" 2>/dev/null)" || return 1
            [ -n "$broray_runtime_evidence_canonical" ] || return 1
            case "$broray_runtime_evidence_id" in
                command.ash|ash.syntax|kill.contract|ash.ulimit-file.contract)
                    [ "$broray_runtime_evidence_path" = "${BRORAY_TX_ASH:-/opt/bin/ash}" ] || return 1
                    broray_runtime_evidence_recheck=true
                    ;;
                *)
                    broray_runtime_evidence_now="$(broray_runtime_command_path "$broray_runtime_evidence_tool" 2>/dev/null)" || return 1
                    [ "$broray_runtime_evidence_now" = "$broray_runtime_evidence_path" ] || return 1
                    broray_runtime_evidence_recheck=true
                    ;;
            esac
        fi
        [ -f "$broray_runtime_evidence_stdout" ] && [ ! -L "$broray_runtime_evidence_stdout" ] || return 1
        [ -f "$broray_runtime_evidence_stderr" ] && [ ! -L "$broray_runtime_evidence_stderr" ] || return 1
        broray_runtime_evidence_stdout_sha="$("$BRORAY_CAP_SHA256SUM" "$broray_runtime_evidence_stdout" | "$BRORAY_CAP_AWK" 'NR==1{print $1;exit}')" || return 1
        broray_runtime_evidence_stderr_sha="$("$BRORAY_CAP_SHA256SUM" "$broray_runtime_evidence_stderr" | "$BRORAY_CAP_AWK" 'NR==1{print $1;exit}')" || return 1
        broray_runtime_evidence_stdout_bytes="$(wc -c <"$broray_runtime_evidence_stdout" | tr -d ' ')"
        broray_runtime_evidence_stderr_bytes="$(wc -c <"$broray_runtime_evidence_stderr" | tr -d ' ')"
        case "$broray_runtime_evidence_stdout_sha:$broray_runtime_evidence_stderr_sha" in
            *[!0-9a-f:]*) return 1 ;;
        esac
        [ "${#broray_runtime_evidence_stdout_sha}" -eq 64 ] && [ "${#broray_runtime_evidence_stderr_sha}" -eq 64 ] || return 1
        case "$broray_runtime_evidence_stdout_bytes:$broray_runtime_evidence_stderr_bytes" in *[!0-9:]*) return 1 ;; esac
        printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
            "$broray_runtime_evidence_id" "$broray_runtime_evidence_tool" "$broray_runtime_evidence_path" \
            "$broray_runtime_evidence_operation" "$broray_runtime_evidence_rc" "$broray_runtime_evidence_pass" \
            "$broray_runtime_evidence_canonical" "$broray_runtime_evidence_argv_json" \
            "$broray_runtime_evidence_stdout_sha" "$broray_runtime_evidence_stderr_sha" \
            "$broray_runtime_evidence_stdout_bytes" "$broray_runtime_evidence_stderr_bytes" \
            "$broray_runtime_evidence_recheck" >>"$broray_runtime_evidence_final" || return 1
    done <"$broray_runtime_evidence_base"
    mv -f "$broray_runtime_evidence_final" "$broray_runtime_evidence_base"
}

broray_runtime_probe_cleanup()
{
    BRORAY_RUNTIME_CAPABILITY_WORK="$1"
    BRORAY_RUNTIME_CAPABILITY_TSV="$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capabilities.tsv"
    broray_runtime_path_init
    mkdir -p "$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capability-argv" || return 1
    : >"$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capability-empty-stream" || return 1
    : >"$BRORAY_RUNTIME_CAPABILITY_TSV" || return 1
    for broray_runtime_cleanup_tool in awk chmod cp date du grep jq mkdir mv rm sed tr wc; do
        broray_runtime_require_command "$broray_runtime_cleanup_tool" || return 1
    done
    printf '{"probe":true}\n' | jq -e '.probe==true' >/dev/null 2>&1
    broray_runtime_probe_cleanup_rc=$?
    broray_runtime_probe_result jq jq 'parse-and-exit-status' "$broray_runtime_probe_cleanup_rc"
}

broray_runtime_probe_opkg_contract()
{
    "${BRORAY_TX_OPKG:-opkg}" print-architecture >"$BRORAY_RUNTIME_CAPABILITY_PROBE/opkg.arch" 2>"$BRORAY_RUNTIME_CAPABILITY_PROBE/opkg.arch.stderr" &&
        "$BRORAY_CAP_AWK" -v wanted="${BRORAY_TX_TARGET_ARCH:-aarch64-3.10}" '
          BEGIN { rows=0; wantedRows=0; bad=0 }
          {
            rows++
            if (NF!=3 || $1!="arch" || $2!~/^[0-9A-Za-z._+-]+$/ ||
                $3!~/^(0|[1-9][0-9]*)$/ || seen[$2]++) { bad=1; next }
            if ($2==wanted) wantedRows++
          }
          END { exit (bad || rows<1 || wantedRows!=1) ? 1 : 0 }
        ' "$BRORAY_RUNTIME_CAPABILITY_PROBE/opkg.arch" >/dev/null 2>&1
    broray_runtime_opkg_rc=$?
    [ "$broray_runtime_opkg_rc" -eq 0 ] && [ -s "$BRORAY_RUNTIME_CAPABILITY_PROBE/opkg.arch" ] || broray_runtime_opkg_rc=1
    broray_runtime_probe_result opkg.contract opkg \
        'all rows strictly parsed; architecture names unique; target row exactly once' \
        "$broray_runtime_opkg_rc"
}

broray_runtime_mount_identity()
{
    broray_runtime_mount_input="$1"
    broray_runtime_mount_output="$2"
    broray_runtime_mount_resolved="$($BRORAY_CAP_READLINK -f "$broray_runtime_mount_input" 2>/dev/null)" || return 1
    [ -n "$broray_runtime_mount_resolved" ] || return 1
    "$BRORAY_CAP_AWK" -v path="$broray_runtime_mount_resolved" '
      function unescape(v) {
        gsub(/\\040/, " ", v); gsub(/\\011/, "\t", v);
        gsub(/\\012/, "\n", v); gsub(/\\134/, "\\", v); return v
      }
      {
        sep=0
        for (i=1;i<=NF;i++) if ($i=="-") {sep=i; break}
        if (!sep) next
        mp=unescape($5)
        if (path==mp || (mp!="/" && index(path,mp "/")==1) || mp=="/") {
          if (length(mp)>best) {
            best=length(mp); id=$1; dev=$3; root=unescape($4);
            mountpoint=mp; opts=$6; fstype=$(sep+1); bestCount=1
          } else if (length(mp)==best) {
            bestCount++
          }
        }
      }
      END {
        if (!best || bestCount!=1 || opts !~ /(^|,)rw(,|$)/) exit 1
        printf "%s|%s|%s|%s|%s|%s\n", id,dev,root,mountpoint,opts,fstype
      }
    ' /proc/self/mountinfo >"$broray_runtime_mount_output"
}

broray_runtime_probe_mount_contract()
{
    broray_runtime_opt_root="${BRORAY_TX_OPT_ROOT:-/opt}"
    broray_runtime_tmp_root="${BRORAY_TX_TMP_BASE:-/tmp}"
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ]; then
        test -d "$broray_runtime_opt_root" && test -d "$broray_runtime_tmp_root" &&
            test -w "$broray_runtime_tmp_root" || return 1
        printf 'lab-opt|lab-opt-dev|/|%s|rw|fixture-opt\n' "$broray_runtime_opt_root" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/opt.mount"
        printf 'lab-tmp|lab-tmp-dev|/|%s|rw|fixture-tmp\n' "$broray_runtime_tmp_root" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/tmp.mount"
        printf '%s\n' lab-fixture >"$BRORAY_RUNTIME_CAPABILITY_PROBE/opt-tmp.target"
        broray_runtime_probe_result mount.contract test \
            'LAB fixture only; physical /proc/self/mountinfo and exact /opt/tmp -> /tmp remain NOT_RUN' 0 || return 1
        return 0
    fi

    [ -d "$broray_runtime_opt_root" ] && [ -d "$broray_runtime_tmp_root" ] &&
        [ -w "$broray_runtime_tmp_root" ] || return 1
    [ -L "$broray_runtime_opt_root/tmp" ] || return 1
    [ "$($BRORAY_CAP_READLINK "$broray_runtime_opt_root/tmp" 2>/dev/null)" = /tmp ] || return 1
    [ "$($BRORAY_CAP_READLINK -f "$broray_runtime_opt_root/tmp" 2>/dev/null)" = /tmp ] || return 1
    broray_runtime_mount_identity "$broray_runtime_opt_root" "$BRORAY_RUNTIME_CAPABILITY_PROBE/opt.mount" || return 1
    broray_runtime_mount_identity "$broray_runtime_tmp_root" "$BRORAY_RUNTIME_CAPABILITY_PROBE/tmp.mount" || return 1
    printf '%s\n' /tmp >"$BRORAY_RUNTIME_CAPABILITY_PROBE/opt-tmp.target"
    broray_runtime_probe_result mount.contract readlink \
        'readlink /opt/tmp == /tmp; readlink -f; longest rw /proc/self/mountinfo match for /opt and /tmp' 0
}

broray_runtime_probe_full()
{
    BRORAY_RUNTIME_CAPABILITY_WORK="$1"
    BRORAY_RUNTIME_CAPABILITY_TSV="$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capabilities.tsv"
    BRORAY_RUNTIME_CAPABILITY_PROBE="$BRORAY_RUNTIME_CAPABILITY_WORK/capability-probe"
    broray_runtime_path_init
    rm -rf "$BRORAY_RUNTIME_CAPABILITY_PROBE" || return 1
    mkdir -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub" "$BRORAY_RUNTIME_CAPABILITY_PROBE/extract" \
        "$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capability-argv" || return 1
    : >"$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capability-empty-stream" || return 1
    printf 'capability-probe\n' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample" || return 1

    # Exact production call-graph allowlist.  Historical r12 utilities and
    # best-effort diagnostics are deliberately not compatibility gates.
    for broray_runtime_full_tool in \
        ash awk basename cat chmod cp curl cut date dd df dirname du find grep gzip ip \
        jq ln mkdir mkfifo mktemp mv od opkg readlink rm rmdir sed sha256sum sleep sort \
        start-stop-daemon sync tail tar test tr wc
    do
        if [ "$broray_runtime_full_tool" = ash ]; then
            broray_runtime_full_path="${BRORAY_TX_ASH:-/opt/bin/ash}"
            broray_runtime_argv_reset command.ash || return 1
            broray_runtime_argv_add test -x "$broray_runtime_full_path" || return 1
            [ -x "$broray_runtime_full_path" ] || {
                broray_runtime_record command.ash ash "$broray_runtime_full_path" executable 127 false "$BRORAY_RUNTIME_ARGV_DIR" || true
                broray_runtime_failure_write command.ash ash executable
                return 1
            }
            printf '%s\n' "$broray_runtime_full_path" >"$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/command.ash.stdout" || return 1
            broray_runtime_record command.ash ash "$broray_runtime_full_path" executable 0 true "$BRORAY_RUNTIME_ARGV_DIR" \
                "$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/command.ash.stdout" || return 1
        elif [ "$broray_runtime_full_tool" = opkg ]; then
            broray_runtime_full_path="${BRORAY_TX_OPKG:-opkg}"
            broray_runtime_argv_reset command.opkg || return 1
            case "$broray_runtime_full_path" in
                /*) broray_runtime_argv_add test -x "$broray_runtime_full_path" || return 1 ;;
                *) broray_runtime_argv_add command -v "$broray_runtime_full_path" || return 1 ;;
            esac
            case "$broray_runtime_full_path" in /*) [ -x "$broray_runtime_full_path" ] ;; *) command -v "$broray_runtime_full_path" >/dev/null 2>&1 ;; esac || {
                broray_runtime_record command.opkg opkg "$broray_runtime_full_path" command-v 127 false "$BRORAY_RUNTIME_ARGV_DIR" || true
                broray_runtime_failure_write command.opkg opkg command-v
                return 1
            }
            broray_runtime_full_resolved="$(broray_runtime_command_path "$broray_runtime_full_path" 2>/dev/null || printf '%s' "$broray_runtime_full_path")"
            [ -x "$broray_runtime_full_resolved" ] || return 1
            BRORAY_TX_OPKG="$broray_runtime_full_resolved"
            export BRORAY_TX_OPKG
            printf '%s\n' "$broray_runtime_full_resolved" >"$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/command.opkg.stdout" || return 1
            broray_runtime_record command.opkg opkg "$broray_runtime_full_resolved" command-v 0 true "$BRORAY_RUNTIME_ARGV_DIR" \
                "$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/command.opkg.stdout" || return 1
        else
            broray_runtime_require_command "$broray_runtime_full_tool" || return 1
        fi
    done
    broray_runtime_resolve_release_tools || {
        broray_runtime_failure_write tool-resolution release-path command-v
        return 1
    }

    broray_runtime_probe_mount_contract || {
        broray_runtime_failure_write mount.contract readlink '/opt,/tmp mount graph and exact /opt/tmp -> /tmp'
        return 1
    }
    broray_runtime_opt_allocation_probe="${BRORAY_TX_OPT_ROOT:-/opt}/.broray-capability-allocation.$$"
    broray_runtime_tmp_allocation_probe="${BRORAY_TX_TMP_BASE:-/tmp}/.broray-capability-allocation.$$"
    printf x >"$broray_runtime_opt_allocation_probe" 2>/dev/null &&
        printf x >"$broray_runtime_tmp_allocation_probe" 2>/dev/null || return 1
    broray_runtime_opt_allocation_kb="$(du -sk "$broray_runtime_opt_allocation_probe" 2>/dev/null | awk 'NR==1{print $1}')"
    broray_runtime_tmp_allocation_kb="$(du -sk "$broray_runtime_tmp_allocation_probe" 2>/dev/null | awk 'NR==1{print $1}')"
    rm -f "$broray_runtime_opt_allocation_probe" "$broray_runtime_tmp_allocation_probe" || return 1
    case "$broray_runtime_opt_allocation_kb:$broray_runtime_tmp_allocation_kb" in
        *[!0-9:]*|0:*|*:0|'':*|*:'') broray_runtime_probe_rc=1 ;;
        *) broray_runtime_probe_rc=0 ;;
    esac
    broray_runtime_probe_result allocation.unit.contract du 'one-byte regular file allocation on /opt and /tmp' "$broray_runtime_probe_rc" || return 1

    printf '#!/opt/bin/ash\nprintf probe\\n\n' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.sh"
    "${BRORAY_TX_ASH:-/opt/bin/ash}" -n "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.sh" >/dev/null 2>&1
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result ash.syntax ash '-n FILE' "$broray_runtime_probe_rc" || return 1

    printf 'alpha:2\n' | "$BRORAY_CAP_AWK" -F: -v expected=2 '$1=="alpha" && $2==expected {ok=1} END{exit ok?0:1}' >/dev/null 2>&1
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result awk.contract awk '-F -v program' "$broray_runtime_probe_rc" || return 1

    [ "$(basename /tmp/broray/probe.txt)" = probe.txt ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result basename.contract basename 'PATH basename' "$broray_runtime_probe_rc" || return 1
    [ "$(dirname /tmp/broray/probe.txt)" = /tmp/broray ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result dirname.contract dirname 'PATH dirname' "$broray_runtime_probe_rc" || return 1

    cat "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/cat.out" 2>/dev/null &&
        [ "$(cat "$BRORAY_RUNTIME_CAPABILITY_PROBE/cat.out")" = capability-probe ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result cat.contract cat 'FILE and redirected output' "$broray_runtime_probe_rc" || return 1

    chmod 600 "$BRORAY_RUNTIME_CAPABILITY_PROBE/cat.out" 2>/dev/null &&
        cp -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/cat.out" "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy.out" 2>/dev/null &&
        mkdir -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy-tree/source" &&
        cp -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/cat.out" "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy-tree/source/value" &&
        cp -pR "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy-tree/source" "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy-tree/destination" 2>/dev/null &&
        [ -f "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy-tree/destination/value" ] &&
        [ "$(sha256sum "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy.out" | awk 'NR==1{print $1;exit}')" = \
          "$(sha256sum "$BRORAY_RUNTIME_CAPABILITY_PROBE/cat.out" | awk 'NR==1{print $1;exit}')" ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result chmod.contract chmod 'MODE FILE' "$broray_runtime_probe_rc" || return 1
    broray_runtime_probe_result cp.contract cp '-p and -pR SOURCE DESTINATION' "$broray_runtime_probe_rc" || return 1

    [ "$(printf 'left:right\n' | cut -d: -f2)" = right ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result cut.contract cut '-d DELIMITER -f FIELD' "$broray_runtime_probe_rc" || return 1

    broray_runtime_date_epoch="$(date '+%s' 2>/dev/null)"
    broray_runtime_date_utc="$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null)"
    printf '%s\n%s\n' "$broray_runtime_date_epoch" "$broray_runtime_date_utc" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/date.out"
    case "$broray_runtime_date_epoch:$broray_runtime_date_utc" in
        *[!0-9:TZ-]*|:*) broray_runtime_probe_rc=1 ;;
        *:????-??-??T??:??:??Z) broray_runtime_probe_rc=0 ;;
        *) broray_runtime_probe_rc=1 ;;
    esac
    broray_runtime_probe_result date.contract date '+%s and UTC timestamp formatting' "$broray_runtime_probe_rc" || return 1

    df -Pk "$BRORAY_RUNTIME_CAPABILITY_PROBE" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/df.out" 2>/dev/null &&
        awk 'NR==2 && $2~/^[0-9]+$/ && $4~/^[0-9]+$/ {ok=1} END{exit !ok}' "$BRORAY_RUNTIME_CAPABILITY_PROBE/df.out" &&
        du -sk "$BRORAY_RUNTIME_CAPABILITY_PROBE/source" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/du.out" 2>/dev/null &&
        awk 'NR==1 && $1~/^[0-9]+$/ {ok=1} END{exit !ok}' "$BRORAY_RUNTIME_CAPABILITY_PROBE/du.out"
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result df.contract df '-P -k PATH numeric columns' "$broray_runtime_probe_rc" || return 1
    broray_runtime_probe_result du.contract du '-s -k PATH numeric size' "$broray_runtime_probe_rc" || return 1
    printf 'first\nsecond\n' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/head-tail.in"
    [ "$(tail -n 1 "$BRORAY_RUNTIME_CAPABILITY_PROBE/head-tail.in")" = second ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result tail.contract tail '-n COUNT FILE' "$broray_runtime_probe_rc" || return 1

    ip -4 addr show >"$BRORAY_RUNTIME_CAPABILITY_PROBE/ip.out" 2>"$BRORAY_RUNTIME_CAPABILITY_PROBE/ip.stderr"
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result ip.contract ip '-4 addr show read-only' "$broray_runtime_probe_rc" || return 1

    ln "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample" "$BRORAY_RUNTIME_CAPABILITY_PROBE/hardlink" 2>/dev/null &&
        [ -f "$BRORAY_RUNTIME_CAPABILITY_PROBE/hardlink" ] &&
        ln -s source/sub/sample "$BRORAY_RUNTIME_CAPABILITY_PROBE/link" 2>/dev/null &&
        [ -L "$BRORAY_RUNTIME_CAPABILITY_PROBE/link" ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result ln.contract ln 'hardlink and -s symlink' "$broray_runtime_probe_rc" || return 1

    broray_runtime_mktemp="$(mktemp -d "$BRORAY_RUNTIME_CAPABILITY_PROBE/mktemp.XXXXXX" 2>/dev/null)" &&
        [ -d "$broray_runtime_mktemp" ] && mkdir "$broray_runtime_mktemp/move-source" &&
        mv "$broray_runtime_mktemp/move-source" "$broray_runtime_mktemp/move-target" &&
        rmdir "$broray_runtime_mktemp/move-target" && rmdir "$broray_runtime_mktemp"
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result mktemp.contract mktemp '-d TEMPLATE inside operation workspace' "$broray_runtime_probe_rc" || return 1
    broray_runtime_probe_result mkdir.contract mkdir 'directory creation including -p' "$broray_runtime_probe_rc" || return 1
    broray_runtime_probe_result mv.contract mv 'SOURCE DESTINATION' "$broray_runtime_probe_rc" || return 1
    broray_runtime_probe_result rmdir.contract rmdir 'empty directory removal' "$broray_runtime_probe_rc" || return 1

    od -An -tu1 -v "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/od.out" 2>/dev/null &&
        awk 'NF>0{found=1} END{exit !found}' "$BRORAY_RUNTIME_CAPABILITY_PROBE/od.out"
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result od.contract od '-An -tu1 -v FILE' "$broray_runtime_probe_rc" || return 1

    mkdir -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/rm-tree/sub" &&
        : >"$BRORAY_RUNTIME_CAPABILITY_PROBE/rm-tree/sub/value" &&
        rm -f "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy.out" &&
        rm -rf "$BRORAY_RUNTIME_CAPABILITY_PROBE/rm-tree" &&
        [ ! -e "$BRORAY_RUNTIME_CAPABILITY_PROBE/copy.out" ] &&
        [ ! -e "$BRORAY_RUNTIME_CAPABILITY_PROBE/rm-tree" ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result rm.contract rm '-f FILE and -rf disposable probe directory' "$broray_runtime_probe_rc" || return 1

    sleep 0
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result sleep.contract sleep 'bounded integer seconds' "$broray_runtime_probe_rc" || return 1

    printf 'sleep 1\n' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/daemon-body.sh"
    start-stop-daemon -S -b -m -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/daemon.pid" \
        -x "${BRORAY_TX_ASH:-/opt/bin/ash}" -- "$BRORAY_RUNTIME_CAPABILITY_PROBE/daemon-body.sh" \
        >"$BRORAY_RUNTIME_CAPABILITY_PROBE/start-stop-daemon.out" 2>"$BRORAY_RUNTIME_CAPABILITY_PROBE/start-stop-daemon.stderr"
    broray_runtime_probe_rc=$?
    [ "$broray_runtime_probe_rc" -eq 0 ] && [ -s "$BRORAY_RUNTIME_CAPABILITY_PROBE/daemon.pid" ] || broray_runtime_probe_rc=1
    sleep 2
    broray_runtime_daemon_pid="$(sed -n '1p' "$BRORAY_RUNTIME_CAPABILITY_PROBE/daemon.pid" 2>/dev/null)"
    case "$broray_runtime_daemon_pid" in
        ''|*[!0-9]*) broray_runtime_probe_rc=1 ;;
        *) kill -0 "$broray_runtime_daemon_pid" 2>/dev/null && broray_runtime_probe_rc=1 ;;
    esac
    broray_runtime_probe_result start-stop-daemon.contract start-stop-daemon '-S -b -m -p -x -- bounded ash child' "$broray_runtime_probe_rc" || return 1

    test -f "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample"
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result test.contract test '-f FILE' "$broray_runtime_probe_rc" || return 1
    "${BRORAY_TX_ASH:-/opt/bin/ash}" -c 'kill -0 "$$"' >/dev/null 2>&1
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result kill.contract ash 'ash builtin kill -0 PID' "$broray_runtime_probe_rc" || return 1
    "${BRORAY_TX_ASH:-/opt/bin/ash}" -c 'ulimit -S -f 64 && [ "$(ulimit -S -f)" -eq 64 ]' >/dev/null 2>&1
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result ash.ulimit-file.contract ash \
        'ash builtin soft RLIMIT_FSIZE in POSIX 512-byte blocks' "$broray_runtime_probe_rc" || return 1

    [ "$(printf ' A B \n' | tr -d ' ' | wc -c | tr -d ' ')" -eq 3 ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result tr.contract tr '-d SET' "$broray_runtime_probe_rc" || return 1
    broray_runtime_probe_result wc.contract wc '-c redirected input' "$broray_runtime_probe_rc" || return 1

    printf '  alpha  \n' | "$BRORAY_CAP_SED" -n -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e '/^alpha$/p' | "$BRORAY_CAP_GREP" -Fxq alpha
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result sed.contract sed '-n -e POSIX expressions' "$broray_runtime_probe_rc" || return 1
    broray_runtime_probe_result grep.contract grep '-E -F -q' "$broray_runtime_probe_rc" || return 1

    "$BRORAY_CAP_FIND" -P "$BRORAY_RUNTIME_CAPABILITY_PROBE/source" -xdev -mindepth 1 -maxdepth 2 \
        -type f -name sample -path '*/sample' -print \
        -printf '%m|%U|%G|%D|%i|%n|%b|%s|%p\n' \
        -exec test -f '{}' ';' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/find.out" 2>/dev/null
    broray_runtime_probe_rc=$?
    [ "$broray_runtime_probe_rc" -eq 0 ] && [ "$(wc -l <"$BRORAY_RUNTIME_CAPABILITY_PROBE/find.out" | tr -d ' ')" -eq 2 ] &&
        tail -n 1 "$BRORAY_RUNTIME_CAPABILITY_PROBE/find.out" |
        "$BRORAY_CAP_AWK" -F '|' 'NF==9 && $1~/^[0-7]+$/ && $2~/^[0-9]+$/ && $3~/^[0-9]+$/ && $4~/^[0-9]+$/ && $5~/^[0-9]+$/ && $6~/^[0-9]+$/ && $7~/^[0-9]+$/ && $8~/^[0-9]+$/ {ok=1} END{exit !ok}' \
        || broray_runtime_probe_rc=1
    broray_runtime_probe_result find.contract find '-P -xdev -mindepth -maxdepth -type -name -path -print -printf mode/uid/gid/device/inode/nlink/blocks/bytes/path -exec' "$broray_runtime_probe_rc" || return 1

    printf 'b\na\na\n' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.in"
    "$BRORAY_CAP_SORT" -u "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.in" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.unique" 2>/dev/null &&
        "$BRORAY_CAP_SORT" -r "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.in" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.reverse" 2>/dev/null &&
        printf '2\tb\n1\ta\n' | "$BRORAY_CAP_SORT" -n >"$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.numeric" 2>/dev/null
    broray_runtime_probe_rc=$?
    printf 'a\nb\n' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.unique.expected"
    printf 'b\na\na\n' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.reverse.expected"
    printf '1\ta\n2\tb\n' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.numeric.expected"
    [ "$broray_runtime_probe_rc" -eq 0 ] &&
        cmp -s "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.unique.expected" "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.unique" &&
        cmp -s "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.reverse.expected" "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.reverse" &&
        cmp -s "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.numeric.expected" "$BRORAY_RUNTIME_CAPABILITY_PROBE/sort.numeric" ||
        broray_runtime_probe_rc=1
    broray_runtime_probe_result sort.contract sort '-u -r -n with exact redirected output' "$broray_runtime_probe_rc" || return 1

    [ "$("$BRORAY_CAP_READLINK" "$BRORAY_RUNTIME_CAPABILITY_PROBE/link" 2>/dev/null)" = source/sub/sample ] &&
        [ "$("$BRORAY_CAP_READLINK" -f "$BRORAY_RUNTIME_CAPABILITY_PROBE/link" 2>/dev/null)" = "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample" ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result readlink.contract readlink 'FILE and -f FILE' "$broray_runtime_probe_rc" || return 1

    (cd "$BRORAY_RUNTIME_CAPABILITY_PROBE" && "$BRORAY_CAP_SHA256SUM" source/sub/sample >sample.sha256 && "$BRORAY_CAP_SHA256SUM" -c sample.sha256 >/dev/null 2>&1)
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result sha256sum.contract sha256sum 'calculate and -c' "$broray_runtime_probe_rc" || return 1

    "$BRORAY_CAP_TAR" --format=gnu --blocking-factor=1 -czf "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.tar.gz" -C "$BRORAY_RUNTIME_CAPABILITY_PROBE/source" sub/sample 2>/dev/null &&
        "$BRORAY_CAP_TAR" -tzf "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.tar.gz" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/tar.list" 2>/dev/null &&
        "$BRORAY_CAP_TAR" -tvzf "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.tar.gz" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/tar.verbose" 2>/dev/null &&
        "$BRORAY_CAP_TAR" -xzOf "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.tar.gz" sub/sample >"$BRORAY_RUNTIME_CAPABILITY_PROBE/tar.stdout" 2>/dev/null &&
        "$BRORAY_CAP_TAR" -xzf "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.tar.gz" -C "$BRORAY_RUNTIME_CAPABILITY_PROBE/extract" 2>/dev/null &&
        [ -f "$BRORAY_RUNTIME_CAPABILITY_PROBE/extract/sub/sample" ] &&
        [ "$(cat "$BRORAY_RUNTIME_CAPABILITY_PROBE/tar.stdout")" = capability-probe ] &&
        awk 'NR==1 && (substr($0,1,1)=="-" || substr($0,1,1)=="d") {ok=1} END{exit ok?0:1}' "$BRORAY_RUNTIME_CAPABILITY_PROBE/tar.verbose"
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result tar.contract tar '--format=gnu --blocking-factor=1 -czf; -tzf; -tvzf; -xzOf; -xzf -C regular fixture' "$broray_runtime_probe_rc" || return 1

    "$BRORAY_CAP_GZIP" -c "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/sample.gz" 2>/dev/null &&
        "$BRORAY_CAP_GZIP" -t "$BRORAY_RUNTIME_CAPABILITY_PROBE/sample.gz" >/dev/null 2>&1 &&
        "$BRORAY_CAP_GZIP" -dc "$BRORAY_RUNTIME_CAPABILITY_PROBE/sample.gz" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/sample.unpacked" 2>/dev/null &&
        [ "$("$BRORAY_CAP_SHA256SUM" "$BRORAY_RUNTIME_CAPABILITY_PROBE/sample.unpacked" | "$BRORAY_CAP_AWK" 'NR==1{print $1;exit}')" = "$("$BRORAY_CAP_SHA256SUM" "$BRORAY_RUNTIME_CAPABILITY_PROBE/source/sub/sample" | "$BRORAY_CAP_AWK" 'NR==1{print $1;exit}')" ]
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result gzip.contract gzip '-c -d -t without version probe' "$broray_runtime_probe_rc" || return 1

    "$BRORAY_CAP_MKFIFO" "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.fifo" 2>/dev/null
    broray_runtime_probe_rc=$?
    if [ "$broray_runtime_probe_rc" -eq 0 ]; then
        [ -p "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.fifo" ] && [ ! -L "$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.fifo" ] || broray_runtime_probe_rc=1
    fi
    if [ "$broray_runtime_probe_rc" -eq 0 ]; then
        "$BRORAY_CAP_DD" if=/dev/zero of="$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.fifo" bs=1024 count=1 2>/dev/null &
        broray_runtime_writer_pid=$!
        "$BRORAY_CAP_DD" if="$BRORAY_RUNTIME_CAPABILITY_PROBE/probe.fifo" of="$BRORAY_RUNTIME_CAPABILITY_PROBE/dd.out" bs=1024 count=1 iflag=fullblock 2>/dev/null
        broray_runtime_probe_rc=$?
        wait "$broray_runtime_writer_pid" || broray_runtime_probe_rc=1
        [ "$(wc -c <"$BRORAY_RUNTIME_CAPABILITY_PROBE/dd.out" | tr -d ' ')" -eq 1024 ] || broray_runtime_probe_rc=1
    fi
    broray_runtime_probe_result mkfifo.contract mkfifo 'NAME without version probe' "$broray_runtime_probe_rc" || return 1
    broray_runtime_probe_result dd.contract dd 'if of bs count iflag=fullblock bounded stream' "$broray_runtime_probe_rc" || return 1

    "$BRORAY_CAP_JQ" -nc --arg value probe --argjson enabled true '{value:$value,enabled:$enabled}' | "$BRORAY_CAP_JQ" -e '.value=="probe" and .enabled==true' >/dev/null 2>&1
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result jq.contract jq '-n -c -e --arg --argjson' "$broray_runtime_probe_rc" || return 1

    broray_runtime_jq_sha_program='def broray_sha256: ((type == "string") and (length == 64) and all(explode[]; ((. >= 48) and (. <= 57)) or ((. >= 97) and (. <= 102)))); (.sha256 | broray_sha256)'
    printf '{"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}\n' |
        "$BRORAY_CAP_JQ" -e "$broray_runtime_jq_sha_program" >/dev/null 2>&1
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result jq.sha256-core jq \
        'type length explode all numeric comparisons; no ONIGURUMA/test dependency' \
        "$broray_runtime_probe_rc" || return 1

    # curl is not granted a synthetic PASS from a connection failure.  The
    # exact production argv is executed by the bounded candidate download and
    # bound to its SHA-256 before mutation; this preflight only records the
    # resolved executable.
    broray_runtime_probe_evidence_prepare curl.resolution || return 1
    printf '%s\n' "$BRORAY_CAP_CURL" >"$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/curl.resolution.stdout" || return 1
    broray_runtime_record curl.resolution curl "$BRORAY_CAP_CURL" \
        'executable resolution only; transfer remains unclaimed until candidate-bound execution' 0 true \
        "$BRORAY_RUNTIME_ARGV_DIR" "$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/curl.resolution.stdout" || return 1

    sync
    broray_runtime_probe_rc=$?
    broray_runtime_probe_result sync.contract sync 'flush before mutation barrier' "$broray_runtime_probe_rc" || return 1

    broray_runtime_probe_opkg_contract || return 1

    broray_runtime_ndmc_tool=ndmc
    broray_runtime_ndmc_operation='nonempty textual running-config parsed'
    if [ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && [ -n "${BRORAY_TX_TEST_KEENETIC_STATE:-}" ]; then
        broray_runtime_probe_path="$(broray_runtime_command_path cat 2>/dev/null || printf '')"
        broray_runtime_ndmc_tool=cat
        broray_runtime_ndmc_operation='LAB fixture content parsed; physical ndmc remains NOT_RUN'
        cat "$BRORAY_TX_TEST_KEENETIC_STATE" >"$BRORAY_RUNTIME_CAPABILITY_PROBE/ndmc-running-config" \
            2>"$BRORAY_RUNTIME_CAPABILITY_PROBE/ndmc-running-config.stderr"
        broray_runtime_probe_rc=$?
    else
        broray_runtime_probe_path="$(broray_runtime_command_path ndmc 2>/dev/null || printf '')"
        if [ -n "$broray_runtime_probe_path" ]; then
            ndmc -c 'show running-config' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/ndmc-running-config" \
                2>"$BRORAY_RUNTIME_CAPABILITY_PROBE/ndmc-running-config.stderr"
            broray_runtime_probe_rc=$?
        else
            broray_runtime_probe_rc=127
        fi
    fi
    if [ "$broray_runtime_probe_rc" -eq 0 ]; then
        [ -s "$BRORAY_RUNTIME_CAPABILITY_PROBE/ndmc-running-config" ] &&
            "$BRORAY_CAP_AWK" '
              { sub(/\r$/,""); if ($0 ~ /[^[:space:]]/) records++ }
              END { exit records>0 ? 0 : 1 }
            ' "$BRORAY_RUNTIME_CAPABILITY_PROBE/ndmc-running-config" >/dev/null 2>&1 || broray_runtime_probe_rc=1
    fi
    broray_runtime_probe_evidence_prepare ndmc.read-only || return 1
    if [ "$broray_runtime_probe_rc" -eq 0 ]; then
        broray_runtime_record ndmc.read-only "$broray_runtime_ndmc_tool" "$broray_runtime_probe_path" "$broray_runtime_ndmc_operation" 0 true \
            "$BRORAY_RUNTIME_ARGV_DIR" "$BRORAY_RUNTIME_STDOUT_FILE" "$BRORAY_RUNTIME_STDERR_FILE" || return 1
    else
        broray_runtime_record ndmc.read-only "$broray_runtime_ndmc_tool" "$broray_runtime_probe_path" "$broray_runtime_ndmc_operation" "$broray_runtime_probe_rc" false \
            "$BRORAY_RUNTIME_ARGV_DIR" "$BRORAY_RUNTIME_STDOUT_FILE" "$BRORAY_RUNTIME_STDERR_FILE" || true
        broray_runtime_failure_write ndmc.read-only "$broray_runtime_ndmc_tool" "$broray_runtime_ndmc_operation"
        return 1
    fi

    printf '%s\n' \
      mount.contract allocation.unit.contract ash.syntax ash.ulimit-file.contract awk.contract basename.contract dirname.contract \
      cat.contract chmod.contract cp.contract cut.contract date.contract df.contract du.contract \
      tail.contract ip.contract ln.contract mktemp.contract mkdir.contract mv.contract rmdir.contract od.contract \
      rm.contract sleep.contract start-stop-daemon.contract test.contract kill.contract tr.contract wc.contract sed.contract grep.contract \
      find.contract sort.contract readlink.contract sha256sum.contract tar.contract gzip.contract mkfifo.contract \
      dd.contract jq.contract jq.sha256-core curl.resolution sync.contract opkg.contract ndmc.read-only \
      | "$BRORAY_CAP_JQ" -Rsc 'split("\n")[:-1]' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/required-probes.json" || return 1
    printf '%s\n' \
      ash awk basename cat chmod cp curl cut date dd df dirname du find grep gzip ip jq ln \
      mkdir mkfifo mktemp mv od opkg readlink rm rmdir sed sha256sum sleep sort \
      start-stop-daemon sync tail tar test tr wc \
      | "$BRORAY_CAP_JQ" -Rsc 'split("\n")[:-1]' >"$BRORAY_RUNTIME_CAPABILITY_PROBE/required-commands.json" || return 1

    broray_runtime_opt_mount="$(cat "$BRORAY_RUNTIME_CAPABILITY_PROBE/opt.mount" 2>/dev/null)"
    broray_runtime_tmp_mount="$(cat "$BRORAY_RUNTIME_CAPABILITY_PROBE/tmp.mount" 2>/dev/null)"
    broray_runtime_opt_dev="$(printf '%s\n' "$broray_runtime_opt_mount" | "$BRORAY_CAP_AWK" -F '|' 'NF==6{print $2}')"
    broray_runtime_tmp_dev="$(printf '%s\n' "$broray_runtime_tmp_mount" | "$BRORAY_CAP_AWK" -F '|' 'NF==6{print $2}')"
    [ -n "$broray_runtime_opt_dev" ] && [ -n "$broray_runtime_tmp_dev" ] || return 1
    if [ "$broray_runtime_opt_dev" = "$broray_runtime_tmp_dev" ]; then broray_runtime_same_fs=true; else broray_runtime_same_fs=false; fi
    broray_runtime_evidence_finalize || return 1
    "$BRORAY_CAP_JQ" -Rn --arg contract "$BRORAY_RUNTIME_CAPABILITY_CONTRACT" \
        --arg generatedAt "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
        --arg operationId "$(sed -n '1p' "$BRORAY_RUNTIME_CAPABILITY_WORK/operation-id" 2>/dev/null)" \
        --arg optMount "$broray_runtime_opt_mount" \
        --arg tmpMount "$broray_runtime_tmp_mount" \
        --arg optTmpTarget "$(cat "$BRORAY_RUNTIME_CAPABILITY_PROBE/opt-tmp.target" 2>/dev/null)" \
        --argjson optAllocationUnitKB "$broray_runtime_opt_allocation_kb" \
        --argjson tmpAllocationUnitKB "$broray_runtime_tmp_allocation_kb" \
        --argjson sameBackingFs "$broray_runtime_same_fs" \
        --slurpfile required "$BRORAY_RUNTIME_CAPABILITY_PROBE/required-probes.json" \
        --slurpfile requiredCommands "$BRORAY_RUNTIME_CAPABILITY_PROBE/required-commands.json" \
        --argjson tsvRows "$(wc -l <"$BRORAY_RUNTIME_CAPABILITY_TSV" | tr -d ' ')" \
        --argjson lab "$([ "${BRORAY_TX_TEST_MODE:-0}" = 1 ] && printf true || printf false)" '
        [inputs | split("\t") |
          select(length==13) |
          {id:.[0],tool:.[1],resolvedPath:.[2],operation:.[3],exitCode:(.[4]|tonumber),pass:(.[5]=="true"),
           canonicalPath:(if .[6]=="" then null else .[6] end),argv:(.[7]|fromjson),
           stdoutSha256:.[8],stderrSha256:.[9],stdoutBytes:(.[10]|tonumber),stderrBytes:(.[11]|tonumber),
           pathRevalidated:(.[12]=="true")}] as $probes |
        ($required[0] - [$probes[].id]) as $missing |
        ($requiredCommands[0] - [$probes[] | select(.id|startswith("command.")) | .tool]) as $missingCommands |
        {schemaVersion:2,contract:$contract,requirementsContract:"1.7.2",
         lifecycleContract:"current-operation-full-tmp-snapshot/1",
         capabilityContract:"keenetic-entware-capabilities/1",spaceContract:"broray-space/2",
         previousIpkRequired:false,historicalTransactionStateRequired:false,statelessBootstrap:true,
         selection:"functional-capability-not-model-or-version",
         operationId:$operationId,candidateSha256:null,candidateBindingStage:"pre-candidate",
         generatedAt:$generatedAt,mutationStarted:false,
         mountGraph:{opt:{identity:$optMount,allocationUnitKB:$optAllocationUnitKB},
           tmp:{identity:$tmpMount,allocationUnitKB:$tmpAllocationUnitKB},
           optTmpTarget:$optTmpTarget,sameBackingFs:$sameBackingFs},probes:$probes,
         physicalGates:{mountAndSymlink:(if $lab then "NOT_RUN" else "PREFLIGHT_ONLY" end),
           opkgLock:"NOT_RUN",serviceArgvIdentity:"NOT_RUN",serviceHookTimeout:"NOT_RUN",
           lanListener:"NOT_RUN",socksHandshake:"NOT_RUN",proxy0EndToEnd:"NOT_RUN",
           dotDohRoutePreservation:"NOT_RUN",rebootAndUnmount:"NOT_RUN"},
         requiredProbeIds:$required[0],missingRequiredProbeIds:$missing,
         requiredCommandNames:$requiredCommands[0],missingRequiredCommandNames:$missingCommands,
         physicalExecution:(if $lab then "NOT_RUN" else "PARTIAL_PREFLIGHT" end),
         result:(if ($probes|length)==$tsvRows and ($missing|length)==0 and ($missingCommands|length)==0 and
           all($probes[];.pass and .pathRevalidated and
             (.argv|type)=="array" and (.stdoutSha256|length)==64 and (.stderrSha256|length)==64)
           then (if $lab then "LAB_EXERCISED" else "PREFLIGHT_PASS" end) else "FAIL" end)}
    ' <"$BRORAY_RUNTIME_CAPABILITY_TSV" >"$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capabilities.json.part" || return 1
    mv -f "$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capabilities.json.part" \
        "$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capabilities.json" || return 1
    "$BRORAY_CAP_JQ" -e '.requirementsContract=="1.7.2" and
      .lifecycleContract=="current-operation-full-tmp-snapshot/1" and
      .capabilityContract=="keenetic-entware-capabilities/1" and .spaceContract=="broray-space/2" and
      .previousIpkRequired==false and .historicalTransactionStateRequired==false and
      .statelessBootstrap==true and .candidateSha256==null and .candidateBindingStage=="pre-candidate" and
      (.result=="PREFLIGHT_PASS" or .result=="LAB_EXERCISED") and (.probes|length)>0 and
      all(.probes[];.pass and .pathRevalidated and (.argv|type)=="array" and
        (.stdoutSha256|length)==64 and (.stderrSha256|length)==64) and
      (.missingRequiredProbeIds|length)==0 and (.missingRequiredCommandNames|length)==0 and
      (.requiredCommandNames|length)>0 and (.mountGraph.sameBackingFs|type)=="boolean" and
      ((.physicalGates|keys|sort)==(["dotDohRoutePreservation","lanListener","mountAndSymlink","opkgLock",
        "proxy0EndToEnd","rebootAndUnmount","serviceArgvIdentity","serviceHookTimeout","socksHandshake"]|sort))' \
        "$BRORAY_RUNTIME_CAPABILITY_WORK/evidence/capabilities.json" >/dev/null 2>&1 || return 1
    rm -rf "$BRORAY_RUNTIME_CAPABILITY_PROBE" || return 1
    broray_runtime_activate_if_resolved
}
