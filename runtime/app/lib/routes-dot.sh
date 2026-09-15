#!/opt/bin/ash

# BROray DNS-over-TLS manager for KeeneticOS.
# R15 policy: every live DoT record is parsed and classified. Catalog
# membership and historical receipts never imply ownership. Mutations use the
# current selection and exact live equality on a unique Keenetic selector;
# unselected DoT/DoH records are preserved.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_DOT_ROOT="${BRORAY_DOT_ROOT:-$BRORAY_ROOT/routes/dot}"
BRORAY_DOT_CONFIG="${BRORAY_DOT_CONFIG:-$BRORAY_DOT_ROOT/config.json}"
BRORAY_DOT_STATE="${BRORAY_DOT_STATE:-$BRORAY_DOT_ROOT/state.json}"
BRORAY_DOT_NDMC="${BRORAY_DOT_NDMC:-ndmc}"
BRORAY_DOT_OPENSSL="${BRORAY_DOT_OPENSSL:-openssl}"
BRORAY_DOT_TIMEOUT="${BRORAY_DOT_TIMEOUT:-timeout}"
BRORAY_DOT_TEST_TTL="${BRORAY_DOT_TEST_TTL:-600}"
BRORAY_DOT_MAX_SERVERS="${BRORAY_DOT_MAX_SERVERS:-8}"
BRORAY_DOT_CHILD_PID=''
BRORAY_DOT_CHILD_STARTTIME=''
BRORAY_DOT_WRITE_POLICY_LIBRARY="$BRORAY_ROOT/lib/keenetic-write-policy.sh"

if [ -r "$BRORAY_DOT_WRITE_POLICY_LIBRARY" ]; then
    . "$BRORAY_DOT_WRITE_POLICY_LIBRARY"
fi

# These values are intentionally immutable source constants. Physical evidence
# from any one router is validation evidence only and never selects a model or
# preclassified serialization profile. The builder may enable the pre-frozen
# reversible protocol; stable promotion remains external to these bytes.
BRORAY_DOT_WRITE_CONTRACT_CANDIDATE='3.0.0-r15c16'
BRORAY_DOT_WRITE_PROTOCOL_ENABLED=true
BRORAY_DOT_WRITE_PROTOCOL_SHA256='dc8501856b27260738828e9959e10512496410ff44adfceaa8564056bed1ddfa'
readonly BRORAY_DOT_WRITE_CONTRACT_CANDIDATE
readonly BRORAY_DOT_WRITE_PROTOCOL_ENABLED
readonly BRORAY_DOT_WRITE_PROTOCOL_SHA256

broray_dot_now() { date '+%Y-%m-%dT%H:%M:%S%z'; }
broray_dot_epoch() { date '+%s'; }

# Every potentially blocking external command is supervised by its exact Linux
# process identity.  The transaction signal handler can therefore stop the
# command it actually started before it begins live/local rollback; a reused
# PID is never signalled.
broray_dot_proc_starttime()
{
    proc_pid="${1:-}"
    case "$proc_pid" in ''|*[!0-9]*|0) return 1 ;; esac
    [ -r "/proc/$proc_pid/stat" ] || return 1
    proc_start_value="$(awk '
      NR==1 {
        line=$0
        sub(/^.*\) /,"",line)
        n=split(line,field," ")
        if (n>=20 && field[20]~/^[0-9]+$/ && field[20]!="0") print field[20]
      }
    ' "/proc/$proc_pid/stat" 2>/dev/null)" || return 1
    case "$proc_start_value" in ''|*[!0-9]*|0) return 1 ;; esac
    printf '%s\n' "$proc_start_value"
}

broray_dot_child_identity_exact()
{
    exact_pid="${1:-}"
    exact_start="${2:-}"
    case "$exact_pid:$exact_start" in
        *[!0-9:]*|0:*|*:0|:*|*:) return 1 ;;
    esac
    [ "$(broray_dot_proc_starttime "$exact_pid" 2>/dev/null)" = "$exact_start" ]
}

broray_dot_child_run()
{
    "$@" &
    child_pid=$!
    BRORAY_DOT_CHILD_PID="$child_pid"
    BRORAY_DOT_CHILD_STARTTIME=''
    child_start="$(broray_dot_proc_starttime "$child_pid" 2>/dev/null || true)"
    BRORAY_DOT_CHILD_STARTTIME="$child_start"
    child_rc=0
    wait "$child_pid" || child_rc=$?
    BRORAY_DOT_CHILD_PID=''
    BRORAY_DOT_CHILD_STARTTIME=''
    return "$child_rc"
}

broray_dot_child_terminate()
{
    terminate_pid="$BRORAY_DOT_CHILD_PID"
    terminate_start="$BRORAY_DOT_CHILD_STARTTIME"
    BRORAY_DOT_CHILD_PID=''
    BRORAY_DOT_CHILD_STARTTIME=''
    broray_dot_child_identity_exact "$terminate_pid" "$terminate_start" || return 0
    kill -TERM "$terminate_pid" 2>/dev/null || true
    terminate_wait=0
    while broray_dot_child_identity_exact "$terminate_pid" "$terminate_start"; do
        terminate_wait=$((terminate_wait + 1))
        [ "$terminate_wait" -lt 5 ] || break
        sleep 1
    done
    if broray_dot_child_identity_exact "$terminate_pid" "$terminate_start"; then
        kill -KILL "$terminate_pid" 2>/dev/null || true
    fi
    wait "$terminate_pid" 2>/dev/null || true
}

broray_dot_error()
{
    code="$1"
    message="$2"
    details="${3:-}"
    printf 'BRORAY_ERROR:%s:%s\n' "$code" "$message" >&2
    [ -z "$details" ] || printf '%s\n' "$details" >&2
    return 1
}

broray_dot_presets()
{
    cat <<'JSON'
[
  {"id":"google-primary","provider":"Google","name":"Google 8.8.8.8","address":"8.8.8.8","port":853,"effectivePort":853,"sni":"dns.google","spki":"","on":"","interface":"","domain":""},
  {"id":"google-secondary","provider":"Google","name":"Google 8.8.4.4","address":"8.8.4.4","port":853,"effectivePort":853,"sni":"dns.google","spki":"","on":"","interface":"","domain":""},
  {"id":"yandex-primary","provider":"Yandex DNS","name":"Yandex DNS 77.88.8.8","address":"77.88.8.8","port":853,"effectivePort":853,"sni":"common.dot.dns.yandex.net","spki":"","on":"","interface":"","domain":""},
  {"id":"cloudflare-primary","provider":"Cloudflare","name":"Cloudflare 1.1.1.1","address":"1.1.1.1","port":853,"effectivePort":853,"sni":"cloudflare-dns.com","spki":"","on":"","interface":"","domain":""},
  {"id":"cloudflare-secondary","provider":"Cloudflare","name":"Cloudflare 1.0.0.1","address":"1.0.0.1","port":853,"effectivePort":853,"sni":"cloudflare-dns.com","spki":"","on":"","interface":"","domain":""},
  {"id":"quad9","provider":"Quad9","name":"Quad9 9.9.9.9","address":"9.9.9.9","port":853,"effectivePort":853,"sni":"dns.quad9.net","spki":"","on":"","interface":"","domain":""},
  {"id":"adguard-primary","provider":"AdGuard DNS","name":"AdGuard 94.140.14.14","address":"94.140.14.14","port":853,"effectivePort":853,"sni":"dns.adguard-dns.com","spki":"","on":"","interface":"","domain":""},
  {"id":"adguard-secondary","provider":"AdGuard DNS","name":"AdGuard 94.140.15.15","address":"94.140.15.15","port":853,"effectivePort":853,"sni":"dns.adguard-dns.com","spki":"","on":"","interface":"","domain":""}
]
JSON
}

broray_dot_validate_catalog_file()
{
    catalog_file="$1"
    jq -e '
      . as $catalog |
      ($catalog|type)=="array" and ($catalog|length)==8 and
      (($catalog|map(.id)|unique|length)==8) and
      (($catalog|map([.address,.effectivePort,.sni])|unique|length)==8) and
      ([$catalog[].id] == [
        "google-primary","google-secondary","yandex-primary",
        "cloudflare-primary","cloudflare-secondary","quad9",
        "adguard-primary","adguard-secondary"
      ]) and
      all($catalog[];
        (.id|type)=="string" and (.provider|type)=="string" and
        (.name|type)=="string" and (.address|type)=="string" and
        (.address|length)>0 and .port==853 and .effectivePort==853 and
        (.sni|type)=="string" and (.sni|length)>0 and
        .spki=="" and .on=="" and .interface=="" and .domain=="")
    ' "$catalog_file" >/dev/null 2>&1
}

broray_dot_write_catalog()
{
    catalog_file="$1"
    broray_dot_presets >"$catalog_file" || return 1
    broray_dot_validate_catalog_file "$catalog_file"
}

broray_dot_write_protocol_enabled()
{
    [ "$BRORAY_DOT_WRITE_CONTRACT_CANDIDATE" = '3.0.0-r15c16' ] || return 1
    [ "$BRORAY_DOT_WRITE_PROTOCOL_ENABLED" = true ] || return 1
    case "$BRORAY_DOT_WRITE_PROTOCOL_SHA256" in
        *[!0-9a-f]*|'') return 1 ;;
    esac
    [ "${#BRORAY_DOT_WRITE_PROTOCOL_SHA256}" -eq 64 ]
}

broray_dot_require_write_protocol()
{
    command -v broray_keenetic_write_policy_dot_profile_check >/dev/null 2>&1 || {
        broray_dot_error DOT_SHARED_WRITE_POLICY_REQUIRED \
            "Изменение DoT заблокировано: общий immutable R14C01 write-policy недоступен."
        return 1
    }
    broray_keenetic_write_policy_dot_profile_check || {
        broray_dot_error DOT_SHARED_WRITE_POLICY_REQUIRED \
            "Изменение DoT заблокировано: общий R14C01 dot profile не имеет физической привязки."
        return 1
    }
    broray_dot_write_protocol_enabled ||
        broray_dot_error DOT_PHYSICAL_WRITE_PROTOCOL_REQUIRED \
            "Изменение DoT заблокировано: в этих bytes не включён замороженный обратимый Keenetic CLI protocol."
}

broray_dot_require_runtime()
{
    command -v jq >/dev/null 2>&1 || broray_dot_error DEPENDENCY_MISSING "Для DNS-over-TLS требуется jq."
    mkdir -p "$BRORAY_DOT_ROOT" "$BRORAY_ROOT/tmp" "$BRORAY_ROOT/run" ||
        broray_dot_error STORAGE_UNAVAILABLE "Не удалось подготовить хранилище DNS-over-TLS."
}

broray_dot_atomic_json()
{
    target="$1"
    temp="$target.new.$$"
    cat >"$temp" || { rm -f "$temp"; return 1; }
    jq -e . "$temp" >/dev/null 2>&1 || { rm -f "$temp"; return 1; }
    chmod 600 "$temp" 2>/dev/null || true
    mv -f "$temp" "$target"
}

broray_dot_migrate_config()
{
    migrated="$BRORAY_ROOT/tmp/dot-config-migrate.$$.json"
    jq \
        --argjson maxServers "$BRORAY_DOT_MAX_SERVERS" \
        --arg migratedAt "$(broray_dot_now)" '
        . as $old |
        ($old.schemaVersion // 0) as $oldSchema |
        ($old.requestedIds // $old.selectedIds // []) as $requested |
        (($old.quarantinedReceipts // []) +
          [($old.managed // [])[] | {
            reason:"legacy-incomplete-receipt",
            sourceSchemaVersion:$oldSchema,
            quarantinedAt:$migratedAt,
            receipt:.
          }]) as $quarantined |
        .schemaVersion = 3 |
        .requestedIds = $requested |
        .selectedIds = $requested |
        .effectiveIds = [] |
        .migrationState = (
          if ($requested | length) > $maxServers
          then "selection-over-limit"
          elif ($quarantined|length)>0
          then "legacy-receipts-quarantined"
          else "none"
          end
        ) |
        .managed = [] |
        .quarantinedReceipts = $quarantined |
        .updatedAt = (.updatedAt // null)
        ' "$BRORAY_DOT_CONFIG" >"$migrated" || {
            rm -f "$migrated"
            return 1
        }

    jq -e '
        (.schemaVersion == 3) and
        ((.requestedIds | type) == "array") and
        ((.selectedIds | type) == "array") and
        ((.effectiveIds | type) == "array") and
        ((.managed | type) == "array") and
        ((.quarantinedReceipts | type) == "array") and
        (.managed|length)==0
    ' "$migrated" >/dev/null 2>&1 || {
        rm -f "$migrated"
        return 1
    }

    cat "$migrated" | broray_dot_atomic_json "$BRORAY_DOT_CONFIG"
    rc=$?
    rm -f "$migrated"
    return "$rc"
}

broray_dot_ensure_files()
{
    broray_dot_require_runtime || return 1

    if [ ! -s "$BRORAY_DOT_CONFIG" ]; then
        jq -n '{
          schemaVersion:3,
          requestedIds:["google-primary","cloudflare-primary","quad9"],
          selectedIds:["google-primary","cloudflare-primary","quad9"],
          effectiveIds:[],
          migrationState:"none",
          managed:[],
          quarantinedReceipts:[],
          updatedAt:null
        }' | broray_dot_atomic_json "$BRORAY_DOT_CONFIG" || return 1
    fi

    if [ ! -s "$BRORAY_DOT_STATE" ]; then
        jq -n '{schemaVersion:1,tests:[],lastTestedAt:null,lastAppliedAt:null,lastDeletedAt:null,lastOperation:null,lastError:null,updatedAt:null}' |
            broray_dot_atomic_json "$BRORAY_DOT_STATE" || return 1
    fi

    config_schema="$(jq -r '.schemaVersion // 0' "$BRORAY_DOT_CONFIG" 2>/dev/null)"
    case "$config_schema" in
        1|2)
            broray_dot_migrate_config ||
                broray_dot_error CONFIG_MIGRATION_FAILED "Не удалось безопасно обновить схему конфигурации DNS-over-TLS."
            ;;
        3)
            ;;
        *)
            broray_dot_error CONFIG_INVALID "Конфигурация DNS-over-TLS повреждена."
            ;;
    esac

    presets="$BRORAY_ROOT/tmp/dot-presets-validate.$$.json"
    broray_dot_write_catalog "$presets" || return 1
    jq -e --slurpfile presets "$presets" '
      def string_field($value): ($value|type)=="string";
      def complete_receipt:
        . as $receipt |
        (.receiptSchemaVersion==1) and
        string_field(.id) and (.id|length)>0 and
        string_field(.provider) and
        string_field(.address) and (.address|length)>0 and
        string_field(.portRaw) and
        ((.effectivePort|type)=="number") and
        (.effectivePort==853) and
        (.portState=="explicit" or .portState=="omitted") and
        string_field(.sni) and string_field(.spki) and
        string_field(.on) and string_field(.interface) and (.on==.interface) and
        string_field(.domain) and string_field(.observedAt) and
        .spki=="" and .interface=="" and .domain=="" and
        string_field(.writeProtocolSha256) and
        (.writeProtocolSha256|length)==64 and
        (.catalogIdentity=={address:.address,effectivePort:.effectivePort,sni:.sni}) and
        (.selector=={address:.address,effectivePort:.effectivePort}) and
        (any($presets[0][]; .id==$receipt.id and .address==$receipt.address and
          .effectivePort==$receipt.effectivePort and .sni==$receipt.sni));
      (.schemaVersion == 3) and
      ((.requestedIds | type) == "array") and
      ((.selectedIds | type) == "array") and
      (.requestedIds == .selectedIds) and
      ((.effectiveIds | type) == "array") and
      ((.managed | type) == "array") and
      ((.quarantinedReceipts | type) == "array") and
      all(.managed[]?; complete_receipt) and
      (((.migrationState // "none") | type) == "string")
    ' "$BRORAY_DOT_CONFIG" >/dev/null 2>&1 || {
        rm -f "$presets"
        broray_dot_error CONFIG_INVALID "Конфигурация DNS-over-TLS повреждена."
        return 1
    }
    rm -f "$presets"

    jq -e '(.schemaVersion==1) and ((.tests|type)=="array")' "$BRORAY_DOT_STATE" >/dev/null 2>&1 ||
        broray_dot_error STATE_INVALID "Состояние DNS-over-TLS повреждено."
}

broray_dot_ndmc_path()
{
    case "$BRORAY_DOT_NDMC" in
        */*) [ -x "$BRORAY_DOT_NDMC" ] && printf '%s\n' "$BRORAY_DOT_NDMC" ;;
        *) command -v "$BRORAY_DOT_NDMC" 2>/dev/null || true ;;
    esac
}

broray_dot_openssl_path()
{
    case "$BRORAY_DOT_OPENSSL" in
        */*) [ -x "$BRORAY_DOT_OPENSSL" ] && printf '%s\n' "$BRORAY_DOT_OPENSSL" ;;
        *) command -v "$BRORAY_DOT_OPENSSL" 2>/dev/null || true ;;
    esac
}

broray_dot_timeout_path()
{
    case "$BRORAY_DOT_TIMEOUT" in
        */*) [ -x "$BRORAY_DOT_TIMEOUT" ] && printf '%s\n' "$BRORAY_DOT_TIMEOUT" ;;
        *) command -v "$BRORAY_DOT_TIMEOUT" 2>/dev/null || true ;;
    esac
}

broray_dot_fetch_running()
{
    local output raw tsv meta ndmc_bin details doh_count
    output="$1"
    raw="$output.raw"
    tsv="$output.tsv"
    meta="$output.meta"
    ndmc_bin="$(broray_dot_ndmc_path)"
    [ -n "$ndmc_bin" ] || broray_dot_error NDMC_UNAVAILABLE "Команда ndmc недоступна."
    broray_dot_child_run "$ndmc_bin" -c 'show running-config' >"$raw" 2>"$output.err" || {
        details="$(tail -n 20 "$output.err" 2>/dev/null)"
        rm -f "$raw" "$tsv" "$meta" "$output.err"
        broray_dot_error KEENETIC_UNAVAILABLE "Не удалось прочитать конфигурацию Keenetic." "$details"
        return 1
    }
    [ ! -s "$output.err" ] || {
        details="$(tail -n 20 "$output.err" 2>/dev/null)"
        rm -f "$raw" "$tsv" "$meta" "$output.err"
        broray_dot_error KEENETIC_UNAVAILABLE "Keenetic вернул stderr при чтении running-config." "$details"
        return 1
    }
    awk -v meta_file="$meta" '
      BEGIN { OFS="\t"; in_dns_proxy=0; record_index=0; doh_count=0 }
      {
        raw=$0
        sub(/\r$/, "", raw)
        line=raw
        sub(/^[[:space:]]+/,"",line)
        sub(/[[:space:]]+$/,"",line)

        indented=(raw ~ /^[[:space:]]/)
        if (!indented && line!="")
          in_dns_proxy=(line=="dns-proxy")

        command_line=""
        if (!indented && line ~ /^dns-proxy[[:space:]]+tls[[:space:]]+upstream([[:space:]]|$)/)
          command_line=line
        else if (in_dns_proxy && indented && line ~ /^tls[[:space:]]+upstream([[:space:]]|$)/)
          command_line="dns-proxy " line

        if ((!indented && line ~ /^dns-proxy[[:space:]]+https[[:space:]]+upstream([[:space:]]|$)/) ||
            (in_dns_proxy && indented && line ~ /^https[[:space:]]+upstream([[:space:]]|$)/))
          doh_count++

        if (command_line=="") next

        record_index++
        n=split(command_line, field, /[[:space:]]+/)
        valid=1; unknown_count=0; extra_count=0
        address=""; port_raw=""; effective_port="853"; port_state="omitted"
        sni=""; spki=""; on_interface=""; domain=""
        seen_sni=seen_spki=seen_on=seen_domain=0

        if (n<4 || field[1]!="dns-proxy" || field[2]!="tls" || field[3]!="upstream")
          valid=0
        else
          address=field[4]
        if (address=="") valid=0

        i=5
        if (i<=n && field[i] ~ /^[0-9]+$/) {
          port_raw=field[i]
          effective_port=field[i]
          port_state="explicit"
          if ((field[i]+0)<1 || (field[i]+0)>65535) valid=0
          i++
        }

        while (i<=n) {
          key=field[i]
          if (key=="sni") {
            if (seen_sni || i>=n) { valid=0; unknown_count++; i++; continue }
            seen_sni=1; sni=field[i+1]; i+=2; continue
          }
          if (key=="spki") {
            if (seen_spki || i>=n) { valid=0; unknown_count++; i++; continue }
            seen_spki=1; spki=field[i+1]; extra_count++; i+=2; continue
          }
          if (key=="on") {
            if (seen_on || i>=n) { valid=0; unknown_count++; i++; continue }
            seen_on=1; on_interface=field[i+1]; extra_count++; i+=2; continue
          }
          if (key=="domain") {
            if (seen_domain || i>=n) { valid=0; unknown_count++; i++; continue }
            seen_domain=1; domain=field[i+1]; extra_count++; i+=2; continue
          }
          valid=0; unknown_count++; i++
        }

        if (!seen_sni || sni=="") { valid=0; unknown_count++ }

        for (j=1; j<=n; j++)
          if (field[j] ~ /[\t\r\n"\\]/) { valid=0; unknown_count++ }
        if (address ~ /:/) { valid=0; unknown_count++ }

        print record_index, address, port_raw, effective_port, port_state, sni,
          spki, on_interface, domain, extra_count, unknown_count, valid
      }
      END { print "DOH_COUNT\t" (doh_count+0) >meta_file }
    ' "$raw" >"$tsv" || { rm -f "$raw" "$tsv" "$meta" "$output.err"; return 1; }
    doh_count="$(awk -F '\t' '$1=="DOH_COUNT" && $2~/^[0-9]+$/ {print $2}' "$meta")"
    case "$doh_count" in ''|*[!0-9]*) rm -f "$raw" "$tsv" "$meta" "$output.err"; return 1 ;; esac
    jq -Rn --argjson dohCount "$doh_count" '
      [inputs | split("\t")] as $rows |
      (if all($rows[]; length==12) then
        [$rows[] | {
          index:(.[0]|tonumber),address:.[1],portRaw:.[2],
          effectivePort:(.[3]|tonumber),portState:.[4],sni:.[5],spki:.[6],
          on:.[7],interface:.[7],domain:.[8],extraAttributeCount:(.[9]|tonumber),
          unknownTokenCount:(.[10]|tonumber),valid:(.[11]=="1"),parseError:(.[11]!="1")
        }]
      else error("invalid DoT parser field count") end) as $dot |
      ([ $dot | group_by([.address,.effectivePort])[] | select(length>1) ] | length) as $collisions |
      {schemaVersion:2,source:"running-config",parser:"r14c01-strict",dot:$dot,
       dohCount:$dohCount,totalSecure:(($dot|length)+$dohCount),
       dotParseErrorCount:([$dot[]|select(.parseError or .unknownTokenCount!=0)]|length),
       selectorCollisionCount:$collisions,
       determinate:(([$dot[]|select(.parseError or .unknownTokenCount!=0)]|length)==0 and $collisions==0)}
    ' <"$tsv" >"$output" || { rm -f "$raw" "$tsv" "$meta" "$output.err"; return 1; }
    rm -f "$raw" "$tsv" "$meta" "$output.err"
}

broray_dot_fetch_runtime()
{
    local output raw tsv meta ndmc_bin details sections system_profiles
    local system_sections policy_sections unknown_schema
    output="$1"
    raw="$output.raw"
    tsv="$output.tsv"
    meta="$output.meta"
    ndmc_bin="$(broray_dot_ndmc_path)"
    [ -n "$ndmc_bin" ] || broray_dot_error NDMC_UNAVAILABLE "Команда ndmc недоступна."
    broray_dot_child_run "$ndmc_bin" -c 'show dns-proxy' >"$raw" 2>"$output.err" || {
        details="$(tail -n 20 "$output.err" 2>/dev/null)"
        rm -f "$raw" "$tsv" "$meta" "$output.err"
        broray_dot_error KEENETIC_RUNTIME_UNAVAILABLE "Не удалось прочитать runtime DNS proxy Keenetic." "$details"
        return 1
    }
    [ ! -s "$output.err" ] || {
        details="$(tail -n 20 "$output.err" 2>/dev/null)"
        rm -f "$raw" "$tsv" "$meta" "$output.err"
        broray_dot_error KEENETIC_RUNTIME_UNAVAILABLE "Keenetic вернул stderr при чтении DNS proxy." "$details"
        return 1
    }
    awk -v meta_file="$meta" '
      BEGIN {
        OFS="\t"
        in_proxy_tls=0; in_record=0; record_index=0; unknown_schema=0
        sections=0; system_profiles=0; system_sections=0; current_proxy=""
      }
      function reset_record() {
        address=""; port_raw=""; effective_port="853"; port_state="omitted"
        sni=""; spki=""; on_interface=""; domain=""
        seen_address=seen_port=seen_sni=seen_spki=seen_interface=seen_domain=0
        valid=1; unknown_count=0; extra_count=0
      }
      function flush_record() {
        if (!in_record) return
        record_index++
        if (!seen_address || !seen_sni || sni=="") { valid=0; unknown_count++ }
        if (address=="") valid=0
        if (seen_port) {
          if (port_raw=="" || port_raw !~ /^[0-9]+$/) valid=0
          else {
            effective_port=port_raw
            port_state="explicit"
            if ((port_raw+0)<1 || (port_raw+0)>65535) valid=0
          }
        }
        if (address ~ /[:"\\]/ || sni ~ /["\\]/ || spki ~ /["\\]/ || on_interface ~ /["\\]/ || domain ~ /["\\]/) { valid=0; unknown_count++ }
        print record_index,address,port_raw,effective_port,port_state,sni,spki,on_interface,domain,extra_count,unknown_count,valid
        in_record=0
      }
      {
        raw=$0; sub(/\r$/, "", raw); line=raw
        sub(/^[[:space:]]+/, "", line); sub(/[[:space:]]+$/, "", line)
        if (line=="proxy-status:") {
          flush_record(); in_proxy_tls=0; current_proxy=""; next
        }
        if (line ~ /^proxy-name:[[:space:]]*/) {
          flush_record(); in_proxy_tls=0
          current_proxy=line
          sub(/^proxy-name:[[:space:]]*/, "", current_proxy)
          if (current_proxy=="System") system_profiles++
          next
        }
        if (line=="proxy-tls:") {
          flush_record(); sections++
          if (current_proxy=="System") {
            system_sections++; in_proxy_tls=1
          } else {
            in_proxy_tls=0
          }
          next
        }
        if (line ~ /^proxy-[[:alnum:]_-]+:$/ && line!="proxy-tls:") { flush_record(); in_proxy_tls=0; next }
        if (!in_proxy_tls) next
        if (line=="server-tls:") { flush_record(); reset_record(); in_record=1; next }
        if (!in_record) { if (line!="") unknown_schema++; next }
        if (line=="") next
        separator=index(line, ":")
        if (separator==0) { valid=0; unknown_count++; unknown_schema++; next }
        key=substr(line,1,separator-1); value=substr(line,separator+1)
        sub(/^[[:space:]]+/, "", value); sub(/[[:space:]]+$/, "", value)
        if (key=="address") { if (seen_address) {valid=0;unknown_count++}; seen_address=1; address=value }
        else if (key=="port") { if (seen_port) {valid=0;unknown_count++}; seen_port=1; port_raw=value }
        else if (key=="sni") { if (seen_sni) {valid=0;unknown_count++}; seen_sni=1; sni=value }
        else if (key=="spki") { if (seen_spki) {valid=0;unknown_count++}; seen_spki=1; spki=value; if (value!="") extra_count++ }
        else if (key=="interface") { if (seen_interface) {valid=0;unknown_count++}; seen_interface=1; on_interface=value; if (value!="") extra_count++ }
        else if (key=="domain") { if (seen_domain) {valid=0;unknown_count++}; seen_domain=1; domain=value; if (value!="") extra_count++ }
        else { valid=0; unknown_count++; unknown_schema++ }
      }
      END {
        flush_record()
        print "PROXY_TLS_SECTIONS\t" (sections+0) >meta_file
        print "SYSTEM_PROXY_COUNT\t" (system_profiles+0) >>meta_file
        print "SYSTEM_PROXY_TLS_SECTIONS\t" (system_sections+0) >>meta_file
        print "POLICY_PROXY_TLS_SECTIONS\t" ((sections-system_sections)+0) >>meta_file
        print "UNKNOWN_SCHEMA\t" (unknown_schema+0) >>meta_file
      }
    ' "$raw" >"$tsv" || { rm -f "$raw" "$tsv" "$meta" "$output.err"; return 1; }
    sections="$(awk -F '\t' '$1=="PROXY_TLS_SECTIONS" && $2~/^[0-9]+$/ {print $2}' "$meta")"
    system_profiles="$(awk -F '\t' '$1=="SYSTEM_PROXY_COUNT" && $2~/^[0-9]+$/ {print $2}' "$meta")"
    system_sections="$(awk -F '\t' '$1=="SYSTEM_PROXY_TLS_SECTIONS" && $2~/^[0-9]+$/ {print $2}' "$meta")"
    policy_sections="$(awk -F '\t' '$1=="POLICY_PROXY_TLS_SECTIONS" && $2~/^[0-9]+$/ {print $2}' "$meta")"
    unknown_schema="$(awk -F '\t' '$1=="UNKNOWN_SCHEMA" && $2~/^[0-9]+$/ {print $2}' "$meta")"
    case "$sections:$system_profiles:$system_sections:$policy_sections:$unknown_schema" in
      *[!0-9:]*) rm -f "$raw" "$tsv" "$meta" "$output.err"; return 1 ;;
    esac
    jq -Rn --argjson sections "$sections" \
      --argjson systemProfiles "$system_profiles" \
      --argjson systemSections "$system_sections" \
      --argjson policySections "$policy_sections" \
      --argjson unknownSchema "$unknown_schema" '
      [inputs | split("\t")] as $rows |
      (if all($rows[]; length==12) then
        [$rows[] | {index:(.[0]|tonumber),address:.[1],portRaw:.[2],effectivePort:(.[3]|tonumber),
          portState:.[4],sni:.[5],spki:.[6],on:.[7],interface:.[7],domain:.[8],
          extraAttributeCount:(.[9]|tonumber),unknownTokenCount:(.[10]|tonumber),
          valid:(.[11]=="1"),parseError:(.[11]!="1")}]
      else error("invalid runtime DoT parser field count") end) as $dot |
      {schemaVersion:1,source:"show-dns-proxy",parser:"r14c01-strict",
       proxyTlsSections:$sections,systemProxyCount:$systemProfiles,
       systemProxyTlsSections:$systemSections,policyProxyTlsSections:$policySections,
       unknownSchemaCount:$unknownSchema,dot:$dot,
       parseErrorCount:([$dot[]|select(.parseError or .unknownTokenCount!=0)]|length),
       determinate:($systemProfiles==1 and $systemSections==1 and $unknownSchema==0 and
         ([$dot[]|select(.parseError or .unknownTokenCount!=0)]|length)==0)}
    ' <"$tsv" >"$output" || { rm -f "$raw" "$tsv" "$meta" "$output.err"; return 1; }
    rm -f "$raw" "$tsv" "$meta" "$output.err"
}

broray_dot_fetch_observed()
{
    local observed_output observed_config observed_runtime
    observed_output="$1"
    observed_config="$observed_output.config"
    observed_runtime="$observed_output.runtime"
    broray_dot_fetch_running "$observed_config" || { rm -f "$observed_config" "$observed_runtime"; return 1; }
    broray_dot_fetch_runtime "$observed_runtime" || { rm -f "$observed_config" "$observed_runtime"; return 1; }
    jq -n --slurpfile config "$observed_config" --slurpfile runtime "$observed_runtime" '
      $config[0] as $c | $runtime[0] as $r |
      def live_same($a;$b):
        $a.address==$b.address and $a.effectivePort==$b.effectivePort and
        $a.sni==$b.sni and $a.spki==$b.spki and $a.interface==$b.interface and
        $a.domain==$b.domain;
      ([$c.dot[]? as $entry | select($entry.valid and $entry.unknownTokenCount==0 and
        ([ $r.dot[]? | select(.valid and .unknownTokenCount==0 and live_same($entry;.)) ]|length)==1)]|length) as $configMatched |
      ([$r.dot[]? as $entry | select($entry.valid and $entry.unknownTokenCount==0 and
        ([ $c.dot[]? | select(.valid and .unknownTokenCount==0 and live_same($entry;.)) ]|length)==1)]|length) as $runtimeMatched |
      $c + {runtime:$r,runtimeReconciled:($c.determinate and $r.determinate and
        ($c.dot|length)==($r.dot|length) and $configMatched==($c.dot|length) and $runtimeMatched==($r.dot|length)),
        determinate:($c.determinate and $r.determinate and ($c.dot|length)==($r.dot|length) and
          $configMatched==($c.dot|length) and $runtimeMatched==($r.dot|length))}
    ' >"$observed_output" || { rm -f "$observed_config" "$observed_runtime" "$observed_output"; return 1; }
    rm -f "$observed_config" "$observed_runtime"
}

broray_dot_validate_request()
{
    request="$1"
    [ -r "$request" ] || broray_dot_error REQUEST_INVALID "Запрос DNS-over-TLS отсутствует."
    presets="$BRORAY_ROOT/tmp/dot-presets.$$.json"
    broray_dot_write_catalog "$presets" || {
        rm -f "$presets"
        broray_dot_error DOT_CATALOG_INVALID "Встроенный каталог DNS-over-TLS не прошёл R14C01-проверку."
        return 1
    }
    jq -e --argjson maxServers "$BRORAY_DOT_MAX_SERVERS" --slurpfile presets "$presets" '
      (.serverIds|type)=="array" and (.serverIds|length)>=1 and (.serverIds|length)<=$maxServers and
      ((.serverIds|unique|length)==(.serverIds|length)) and
      all(.serverIds[]; . as $id | any($presets[0][]; .id==$id)) and
      ((.allowUntested // false) == false)
    ' "$request" >/dev/null 2>&1 || {
        rm -f "$presets"
        broray_dot_error REQUEST_INVALID "Выбран некорректный список DNS-over-TLS серверов."
        return 1
    }
    rm -f "$presets"
}

broray_dot_entries_for_request()
{
    request="$1"
    output="$2"
    presets="$BRORAY_ROOT/tmp/dot-presets.$$.json"
    broray_dot_write_catalog "$presets" || {
        rm -f "$presets"
        return 1
    }
    jq -n --slurpfile request "$request" --slurpfile presets "$presets" '
      $request[0].serverIds as $ids |
      [$ids[] as $id | $presets[0][] | select(.id==$id)]
    ' >"$output"
    rc=$?
    rm -f "$presets"
    return "$rc"
}

broray_dot_tests_fresh_and_ok()
{
    request="$1"
    now="$(broray_dot_epoch)"
    presets="$BRORAY_ROOT/tmp/dot-presets-fresh.$$.json"
    broray_dot_write_catalog "$presets" || { rm -f "$presets"; return 1; }
    jq -e --argjson now "$now" --argjson ttl "$BRORAY_DOT_TEST_TTL" \
      --slurpfile req "$request" --slurpfile presets "$presets" '
      . as $state | $req[0].serverIds as $ids |
      all($ids[]; . as $id |
        ($presets[0][] | select(.id==$id)) as $endpoint |
        any($state.tests[]?;
        .id==$endpoint.id and .address==$endpoint.address and
        .effectivePort==$endpoint.effectivePort and .sni==$endpoint.sni and
        .spki==$endpoint.spki and .interface==$endpoint.interface and .domain==$endpoint.domain and
        .ok==true and
        ((.testedEpoch|type)=="number") and (.testedEpoch>0) and
        (($now-.testedEpoch) >= 0) and (($now-.testedEpoch) <= $ttl)))
    ' "$BRORAY_DOT_STATE" >/dev/null 2>&1
    rc=$?
    rm -f "$presets"
    return "$rc"
}

broray_dot_test()
{
    request="$1"
    broray_dot_transaction_require_clear || return 1
    broray_dot_ensure_files || return 1
    broray_dot_validate_request "$request" || return 1
    entries="$BRORAY_ROOT/tmp/dot-test-entries.$$.json"
    entries_jsonl="$BRORAY_ROOT/tmp/dot-test-entries.$$.jsonl"
    results="$BRORAY_ROOT/tmp/dot-test-results.$$.jsonl"
    broray_dot_entries_for_request "$request" "$entries" || {
        rm -f "$entries" "$entries_jsonl" "$results"
        return 1
    }
    jq -c '.[]' "$entries" >"$entries_jsonl" || {
        rm -f "$entries" "$entries_jsonl" "$results"
        return 1
    }
    : >"$results" || {
        rm -f "$entries" "$entries_jsonl" "$results"
        return 1
    }
    broray_dot_transaction_arm test '' '' '' || {
        rm -f "$entries" "$entries_jsonl" "$results"
        return 1
    }
    openssl_bin="$(broray_dot_openssl_path)"
    timeout_bin="$(broray_dot_timeout_path)"
    tested_at="$(broray_dot_now)"
    tested_epoch="$(broray_dot_epoch)"
    test_internal_failed=false
    test_internal_failure=''
    out=''
    while IFS= read -r entry; do
        id="$(printf '%s' "$entry" | jq -r '.id')"
        address="$(printf '%s' "$entry" | jq -r '.address')"
        effective_port="$(printf '%s' "$entry" | jq -r '.effectivePort')"
        sni="$(printf '%s' "$entry" | jq -r '.sni')"
        spki="$(printf '%s' "$entry" | jq -r '.spki')"
        interface="$(printf '%s' "$entry" | jq -r '.interface')"
        domain="$(printf '%s' "$entry" | jq -r '.domain')"
        fixture="${BRORAY_DOT_TEST_FIXTURE_DIR:-}/$id.json"
        if [ -n "${BRORAY_DOT_TEST_FIXTURE_DIR:-}" ] && [ -r "$fixture" ]; then
            jq -c --arg id "$id" --arg address "$address" --arg sni "$sni" \
              --arg spki "$spki" --arg interface "$interface" --arg domain "$domain" \
              --argjson effectivePort "$effective_port" --arg testedAt "$tested_at" --argjson testedEpoch "$tested_epoch" '
              {id:$id,address:$address,effectivePort:$effectivePort,sni:$sni,spki:$spki,
               interface:$interface,domain:$domain,ok:(.ok==true),
               status:(.status//(if .ok then "ok" else "failed" end)),latencyMs:(.latencyMs//null),
               message:(.message//null),testedAt:$testedAt,testedEpoch:$testedEpoch}
            ' "$fixture" >>"$results" || {
                test_internal_failed=true
                test_internal_failure="fixture:$id"
                break
            }
            continue
        fi
        if [ -z "$openssl_bin" ]; then
            jq -nc --arg id "$id" --arg address "$address" --arg sni "$sni" \
              --arg spki "$spki" --arg interface "$interface" --arg domain "$domain" \
              --argjson effectivePort "$effective_port" --arg testedAt "$tested_at" --argjson testedEpoch "$tested_epoch" \
              '{id:$id,address:$address,effectivePort:$effectivePort,sni:$sni,spki:$spki,interface:$interface,domain:$domain,ok:false,status:"unavailable",latencyMs:null,message:"OpenSSL недоступен.",testedAt:$testedAt,testedEpoch:$testedEpoch}' >>"$results" || {
                test_internal_failed=true
                test_internal_failure="result:$id"
                break
            }
            continue
        fi
        out="$BRORAY_ROOT/tmp/dot-openssl-$id.$$.out"
        start="$(broray_dot_epoch)"
        if [ -n "$timeout_bin" ]; then
            broray_dot_child_run "$timeout_bin" 12 "$openssl_bin" s_client -connect "$address:$effective_port" -servername "$sni" -verify_hostname "$sni" -verify_return_error -brief </dev/null >"$out" 2>&1
        else
            broray_dot_child_run "$openssl_bin" s_client -connect "$address:$effective_port" -servername "$sni" -verify_hostname "$sni" -verify_return_error -brief </dev/null >"$out" 2>&1
        fi
        rc=$?
        finish="$(broray_dot_epoch)"
        latency=$(((finish-start)*1000))
        if [ "$rc" -eq 0 ]; then ok=true; status=ok; message="TLS-соединение и имя сертификата проверены."; else ok=false; status=failed; message="TLS-проверка завершилась ошибкой."; fi
        jq -nc --arg id "$id" --arg address "$address" --arg sni "$sni" \
          --arg spki "$spki" --arg interface "$interface" --arg domain "$domain" \
          --argjson effectivePort "$effective_port" --arg status "$status" --arg message "$message" \
          --arg testedAt "$tested_at" --argjson testedEpoch "$tested_epoch" --argjson latencyMs "$latency" --argjson ok "$ok" \
          '{id:$id,address:$address,effectivePort:$effectivePort,sni:$sni,spki:$spki,interface:$interface,domain:$domain,ok:$ok,status:$status,latencyMs:$latencyMs,message:$message,testedAt:$testedAt,testedEpoch:$testedEpoch}' >>"$results" || {
            test_internal_failed=true
            test_internal_failure="result:$id"
            rm -f "$out"
            break
        }
        rm -f "$out"
    done <"$entries_jsonl"
    if [ "$test_internal_failed" = false ]; then
        tests_json="$(jq -s '.' "$results")" || {
            test_internal_failed=true
            test_internal_failure='results-json'
        }
    fi
    if [ "$test_internal_failed" = false ]; then
        selected_ids="$(jq '.serverIds' "$request")" || {
            test_internal_failed=true
            test_internal_failure='selected-ids'
        }
    fi
    updated_at="$(broray_dot_now)"
    if [ "$test_internal_failed" = false ]; then
        jq --argjson selected "$selected_ids" --arg updatedAt "$updated_at" '
        .requestedIds=$selected |
        .selectedIds=$selected |
        .migrationState="none" |
        .updatedAt=$updatedAt
    ' "$BRORAY_DOT_CONFIG" |
            broray_dot_atomic_json "$BRORAY_DOT_CONFIG" || {
                test_internal_failed=true
                test_internal_failure='config-commit'
            }
    fi
    if [ "$test_internal_failed" = false ]; then
        jq --argjson tests "$tests_json" --arg testedAt "$tested_at" --argjson testedEpoch "$tested_epoch" --arg updatedAt "$updated_at" '.tests=$tests | .lastTestedAt=$testedAt | .lastTestedEpoch=$testedEpoch | .lastError=null | .lastOperation={type:"test",success:(all($tests[]?; .ok==true)),selectedIds:($tests|map(.id)),completedAt:$updatedAt} | .updatedAt=$updatedAt' "$BRORAY_DOT_STATE" |
            broray_dot_atomic_json "$BRORAY_DOT_STATE" || {
                test_internal_failed=true
                test_internal_failure='state-commit'
            }
    fi
    if [ "$test_internal_failed" = true ]; then
        test_rolled_back=false
        broray_dot_transaction_abort >/dev/null 2>&1 && test_rolled_back=true
        rm -f "$entries" "$entries_jsonl" "$results"
        [ -z "$out" ] || rm -f "$out"
        if [ "$test_rolled_back" = true ]; then
            broray_dot_error DOT_TEST_FAILED "Проверка DNS-over-TLS прервана; локальное состояние восстановлено." "$test_internal_failure"
        else
            broray_dot_error DOT_TEST_FAILED "Проверка DNS-over-TLS прервана; требуется восстановление по recovery-marker." "$test_internal_failure"
        fi
        return 1
    fi
    broray_dot_transaction_disarm || {
        rm -f "$entries" "$entries_jsonl" "$results"
        [ -z "$out" ] || rm -f "$out"
        broray_dot_error DOT_RECOVERY_REQUIRED "Проверка завершена, но транзакционный marker не удалось безопасно закрыть."
        return 1
    }
    rm -f "$entries" "$entries_jsonl" "$results"
    [ -z "$out" ] || rm -f "$out"
    broray_dot_status
}

BRORAY_DOT_STATUS_CACHE_FILE="${BRORAY_DOT_STATUS_CACHE_FILE:-$BRORAY_ROOT/run/dot-status-cache.json}"
BRORAY_DOT_STATUS_CACHE_SECONDS="${BRORAY_DOT_STATUS_CACHE_SECONDS:-30}"

broray_dot_status_cached()
{
    broray_dot_ensure_files || return 1
    dot_cache_now="$(broray_dot_epoch)"
    dot_cache_input_hash="$({
        sha256sum "$BRORAY_DOT_CONFIG" "$BRORAY_DOT_STATE" 2>/dev/null || exit 1
    } | sha256sum | awk 'NR==1{print $1;exit}')" || return 1

    if [ -s "$BRORAY_DOT_STATUS_CACHE_FILE" ] &&
       [ ! -L "$BRORAY_DOT_STATUS_CACHE_FILE" ] &&
       jq -ce \
          --arg inputHash "$dot_cache_input_hash" \
          --argjson now "$dot_cache_now" '
            select(
              .schemaVersion == 1 and
              .inputHash == $inputHash and
              (.expiresEpoch | type) == "number" and
              .expiresEpoch > $now and
              (.data | type) == "object"
            ) | .data
          ' "$BRORAY_DOT_STATUS_CACHE_FILE" 2>/dev/null
    then
        return 0
    fi

    dot_cache_data="$BRORAY_ROOT/tmp/dot-status-cache-data.$$.json"
    dot_cache_tmp="$BRORAY_DOT_STATUS_CACHE_FILE.new.$$"
    rm -f "$dot_cache_data" "$dot_cache_tmp"
    broray_dot_status >"$dot_cache_data" || {
        rm -f "$dot_cache_data" "$dot_cache_tmp"
        return 1
    }

    if [ ! -L "$BRORAY_DOT_STATUS_CACHE_FILE" ]; then
        mkdir -p "${BRORAY_DOT_STATUS_CACHE_FILE%/*}" || true
        dot_cache_expires=$((dot_cache_now + BRORAY_DOT_STATUS_CACHE_SECONDS))
        jq -n \
          --slurpfile data "$dot_cache_data" \
          --arg inputHash "$dot_cache_input_hash" \
          --argjson createdEpoch "$dot_cache_now" \
          --argjson expiresEpoch "$dot_cache_expires" '
            {schemaVersion:1,inputHash:$inputHash,createdEpoch:$createdEpoch,
             expiresEpoch:$expiresEpoch,data:$data[0]}
          ' >"$dot_cache_tmp" &&
        chmod 0600 "$dot_cache_tmp" 2>/dev/null &&
        mv -f "$dot_cache_tmp" "$BRORAY_DOT_STATUS_CACHE_FILE" ||
            rm -f "$dot_cache_tmp"
    fi

    cat "$dot_cache_data"
    dot_cache_rc=$?
    rm -f "$dot_cache_data" "$dot_cache_tmp"
    return "$dot_cache_rc"
}

broray_dot_write_command_allowed()
{
    command_text="${1:-}"
    [ "$command_text" != 'system configuration save' ] || return 0
    printf '%s\n' "$command_text" | awk '
      function ipv4(value, octet,i) {
        if (value !~ /^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$/) return 0
        split(value,octet,".")
        for (i=1;i<=4;i++) if (octet[i]<0 || octet[i]>255) return 0
        return 1
      }
      function port(value) { return value ~ /^[0-9]+$/ && value>=1 && value<=65535 }
      function safe(value) { return value!="" && value !~ /[;"\\]/ }
      NR==1 {
        ok=0
        if (NF==6 && $1=="no" && $2=="dns-proxy" && $3=="tls" &&
            $4=="upstream" && ipv4($5) && port($6) &&
            $0=="no dns-proxy tls upstream " $5 " " $6) ok=1
        if ((NF==7 || NF==9 || NF==11 || NF==13) &&
            $1=="dns-proxy" && $2=="tls" && $3=="upstream" &&
            ipv4($4) && port($5) && $6=="sni" && safe($7)) {
          expected="dns-proxy tls upstream " $4 " " $5 " sni " $7
          valid=1
          if (NF>=9) { valid=valid && $8=="spki" && safe($9); expected=expected " spki " $9 }
          if (NF>=11) { valid=valid && $10=="on" && safe($11); expected=expected " on " $11 }
          if (NF>=13) { valid=valid && $12=="domain" && safe($13); expected=expected " domain " $13 }
          if (valid && $0==expected) ok=1
        }
      }
      NR>1 { ok=0 }
      END { exit(ok ? 0 : 1) }
    '
}

broray_dot_command()
{
    command_text="${1:-}"
    broray_dot_write_command_allowed "$command_text" || {
        broray_dot_error DOT_COMMAND_NOT_AUTHORIZED \
            "Команда не входит в замороженный R14C01 DoT grammar."
        return 126
    }
    broray_dot_require_write_protocol || return 1
    ndmc_bin="$(broray_dot_ndmc_path)"
    [ -n "$ndmc_bin" ] || {
        broray_dot_error NDMC_UNAVAILABLE "Команда ndmc недоступна."
        return 1
    }
    broray_dot_child_run "$ndmc_bin" -c "$command_text"
}

broray_dot_status()
{
    broray_dot_ensure_files || return 1
    running="$BRORAY_ROOT/tmp/dot-observed.$$.json"
    presets="$BRORAY_ROOT/tmp/dot-presets.$$.json"
    running_ok=true
    test_available=false
    ndmc_available=false
    recovery_required=false
    write_protocol_enabled=false
    if [ -e "$BRORAY_DOT_TRANSACTION_MARKER" ] || [ -L "$BRORAY_DOT_TRANSACTION_MARKER" ]; then
        recovery_required=true
    fi
    broray_dot_write_protocol_enabled && write_protocol_enabled=true
    [ -n "$(broray_dot_openssl_path)" ] && test_available=true
    [ -n "$(broray_dot_ndmc_path)" ] && ndmc_available=true
    broray_dot_fetch_observed "$running" >/dev/null 2>&1 || {
        running_ok=false
        jq -n '{schemaVersion:2,source:"unavailable",parser:"r14c01-strict",dot:[],dohCount:0,
          totalSecure:0,dotParseErrorCount:0,selectorCollisionCount:0,determinate:false,
          runtimeReconciled:false,runtime:{schemaVersion:1,source:"unavailable",dot:[],
          proxyTlsSections:0,unknownSchemaCount:0,parseErrorCount:0,determinate:false}}' >"$running"
    }
    broray_dot_write_catalog "$presets" || {
        rm -f "$running" "$presets"
        broray_dot_error DOT_CATALOG_INVALID "Встроенный каталог DNS-over-TLS не прошёл R14C01-проверку."
        return 1
    }
    now="$(broray_dot_epoch)"
    jq -n \
        --slurpfile presets "$presets" \
        --slurpfile config "$BRORAY_DOT_CONFIG" \
        --slurpfile state "$BRORAY_DOT_STATE" \
        --slurpfile running "$running" \
        --argjson runningOk "$running_ok" \
        --argjson testAvailable "$test_available" \
        --argjson ndmcAvailable "$ndmc_available" \
        --argjson recoveryRequired "$recovery_required" \
        --argjson writeProtocolEnabled "$write_protocol_enabled" \
        --arg writeContractCandidate "$BRORAY_DOT_WRITE_CONTRACT_CANDIDATE" \
        --arg writeProtocolSha256 "$BRORAY_DOT_WRITE_PROTOCOL_SHA256" \
        --argjson maxServers "$BRORAY_DOT_MAX_SERVERS" \
        --argjson now "$now" \
        --argjson ttl "$BRORAY_DOT_TEST_TTL" '
      $config[0] as $c | $state[0] as $s | $running[0] as $r |
      def same_endpoint($a;$b):
        $a.address==$b.address and $a.effectivePort==$b.effectivePort and $a.sni==$b.sni;
      def same_full($a;$b):
        $a.address==$b.address and $a.portRaw==$b.portRaw and
        $a.effectivePort==$b.effectivePort and $a.portState==$b.portState and
        $a.sni==$b.sni and $a.spki==$b.spki and $a.interface==$b.interface and
        $a.domain==$b.domain;
      def selector_count($entry):
        [$r.dot[]? | select(.address==$entry.address and .effectivePort==$entry.effectivePort)]|length;
      def catalog_matches($entry):
        if $entry.valid and $entry.unknownTokenCount==0 then
          [$presets[0][] | select(same_endpoint($entry;.))]
        else [] end;
      def quarantine_overlap($entry):
        [($c.quarantinedReceipts//[])[]? | (.receipt//{}) as $receipt |
          select(($receipt.address//null)==$entry.address and
            (($receipt.effectivePort//853)==$entry.effectivePort) and
            (($receipt.sni//$entry.sni)==$entry.sni))] | length;
      ($c.managed // []) as $receipts |
      ($r.dot // []) as $actual |
      ($c.requestedIds // $c.selectedIds // []) as $selectedIds |
      ($actual | map(. as $entry |
        (catalog_matches($entry)|map(.id)) as $catalogIds |
        (selector_count($entry)) as $selectorCount |
        ([$receipts[]? as $receipt | select(
          $receipt.writeProtocolSha256==$writeProtocolSha256 and
          (($catalogIds|index($receipt.id))!=null) and
          same_full($receipt;$entry)
        ) | $receipt]) as $matchingReceipts |
        (quarantine_overlap($entry)) as $quarantineCount |
        (if (($entry.valid|not) or $entry.unknownTokenCount!=0) then "unknown"
         elif $selectorCount!=1 then "selector-collision"
         elif ($catalogIds|length)>1 then "ambiguous"
         elif ($catalogIds|length)==1 and
              ($entry.spki!="" or $entry.interface!="" or $entry.domain!="")
           then "catalog-endpoint-with-extra-attributes"
         elif ($catalogIds|length)==1 then "catalog:"+$catalogIds[0]
         else "not-in-broray-catalog" end) as $classification |
        (if ($matchingReceipts|length)==1 and $selectorCount==1
         then "broray" elif $quarantineCount>0 then "quarantined" else "external" end) as $ownership |
        . + {catalogMatchIds:$catalogIds,
          inCatalog:($entry.valid and $entry.unknownTokenCount==0 and $selectorCount==1 and
            ($catalogIds|length)==1 and $entry.spki=="" and $entry.interface=="" and $entry.domain==""),
          classification:$classification,managed:($ownership=="broray"),ownership:$ownership,
          deleteEligible:($entry.valid and $entry.unknownTokenCount==0 and $selectorCount==1 and
            ($catalogIds|length)==1 and $entry.spki=="" and $entry.interface=="" and $entry.domain==""),
          selectorMatchCount:$selectorCount,
          parseError:(($entry.valid|not) or $entry.unknownTokenCount!=0)}
      )) as $classified |
      ([$receipts[]? as $receipt | select(
        ([$classified[]? | select(.deleteEligible and .ownership=="broray" and same_full($receipt;.))]|length)==1
      )]) as $managedPresent |
      (($receipts|length)>0 and ($managedPresent|length)==($receipts|length)) as $allManagedDeleteEligible |
      ($presets[0] | map(. as $p | $p + {
        selected: ($selectedIds | index($p.id) != null),
        present: (any($classified[]?; .inCatalog and (.catalogMatchIds|index($p.id))!=null)),
        managed: (any($classified[]?; .managed and (.catalogMatchIds|index($p.id))!=null)),
        test: ([$s.tests[]? | select(.id==$p.id and .address==$p.address and
          .effectivePort==$p.effectivePort and .sni==$p.sni and .spki==$p.spki and
          .interface==$p.interface and .domain==$p.domain)] | last // null)
      })) as $servers |
      ([$servers[] | select(.selected)]) as $selectedServers |
      ([$selectedServers[] | select(
        .test != null and .test.ok==true and
        ((.test.testedEpoch|type)=="number") and (.test.testedEpoch>0) and
        (($now-(.test.testedEpoch//0)) >= 0) and (($now-(.test.testedEpoch//0)) <= $ttl)
      )]) as $testedSelected |
      ([$selectedServers[] | select(.present)]) as $presentSelected |
      ([$selectedServers[] as $want | select(
        ([$classified[]? | select(.address==$want.address and .effectivePort==$want.effectivePort)]|length)>0 and
        ([$classified[]? | select(.deleteEligible and (.catalogMatchIds|index($want.id))!=null)]|length)!=1
      ) | $want.id]) as $selectedDeleteConflictIds |
      ([$selectedServers[] as $want | select(
        ([$classified[]? | select(.deleteEligible and (.catalogMatchIds|index($want.id))!=null)]|length)==1
      ) | $want.id]) as $selectedDeleteTargetIds |
      ([$receipts[]? | select(. as $receipt | any($selectedServers[]?; .id==$receipt.id) | not)]) as $managedOutsideSelection |
      ([$classified[]? | select(.managed|not)]) as $externalDot |
      ([$selectedServers[] | select(. as $want |
        any($classified[]?; .inCatalog and (.catalogMatchIds|index($want.id))!=null) | not)]) as $selectedMissing |
      (($r.totalSecure//0) - ($managedOutsideSelection|length) + ($selectedMissing|length)) as $projectedTotal |
      (($selectedServers|length)>0 and
       ($presentSelected|length)==($selectedServers|length) and
       ($managedOutsideSelection|length)==0) as $matchesSelection |
      ([$receipts[]?.id] | unique) as $effectiveIds |
      (($selectedIds|length) > $maxServers) as $selectionOverLimit |
      ($runningOk and $ndmcAvailable and ($r.determinate//false)) as $observationDeterminate |
      (if ($observationDeterminate|not) then "unknown"
       elif (($selectedServers|length)>0 and ($presentSelected|length)==($selectedServers|length)) then "installed"
       else "not-installed" end) as $installationState |
      (if $installationState=="unknown" then null else ($installationState=="installed") end) as $installed |
      (($receipts|length)>0 and ($allManagedDeleteEligible|not)) as $drift |
      ($writeProtocolEnabled and $runningOk and $ndmcAvailable and ($recoveryRequired|not) and
        ($r.determinate//false)) as $mutationAvailable |
      (if ($writeProtocolEnabled|not) then "physical-write-protocol-required"
       elif $recoveryRequired then "recovery-required"
       elif ($runningOk|not) or ($ndmcAvailable|not) then "keenetic-unavailable"
       elif (($r.determinate//false)|not) then "dot-observation-underdetermined"
       else null end) as $mutationBlockedReason |
      ($mutationAvailable and ($selectedServers|length)>0 and
        ($selectedDeleteConflictIds|length)==0 and ($selectedDeleteTargetIds|length)>0) as $deleteEligible |
      (if ($writeProtocolEnabled|not) then "physical-write-protocol-required"
       elif ($mutationAvailable|not) then $mutationBlockedReason
       elif ($selectedServers|length)==0 then "selection-required"
       elif ($selectedDeleteConflictIds|length)>0 then "selected-dot-selector-conflict"
       elif ($selectedDeleteTargetIds|length)==0 then "selected-dot-record-not-present"
       else null end) as $deleteBlockedReason |
      (
        if $recoveryRequired then "error"
        elif ($runningOk|not) or ($ndmcAvailable|not) then "error"
        elif (($r.determinate//false)|not) then "error"
        elif ($writeProtocolEnabled|not) then "warning"
        elif $selectionOverLimit or $projectedTotal>$maxServers then "warning"
        elif ($selectedServers|length)==0 then "warning"
        elif ($testedSelected|length)!=($selectedServers|length) then "warning"
        elif ($matchesSelection|not) then "warning"
        else "ok" end
      ) as $severity |
      (
        if $recoveryRequired then false
        elif ($runningOk|not) or ($ndmcAvailable|not) then false
        elif (($r.determinate//false)|not) then false
        else true end
      ) as $operational |
      (
        ($recoveryRequired|not) and $runningOk and $ndmcAvailable and ($r.determinate//false) and
        ($selectionOverLimit|not) and ($projectedTotal<=$maxServers) and
        (($selectedServers|length)>0) and
        (($testedSelected|length)==($selectedServers|length)) and
        $matchesSelection
      ) as $consistent |
      (
        if $recoveryRequired then [{code:"DOT_RECOVERY_REQUIRED",message:"Предыдущая DNS-over-TLS транзакция требует проверки и восстановления.",details:null}]
        elif ($runningOk|not) then [{code:"KEENETIC_CONFIG_UNAVAILABLE",message:"Не удалось прочитать фактическую конфигурацию Keenetic.",details:null}]
        elif ($ndmcAvailable|not) then [{code:"NDMC_UNAVAILABLE",message:"Команда управления Keenetic недоступна.",details:null}]
        elif (($r.determinate//false)|not) then [{code:"DOT_OBSERVATION_UNDERDETERMINED",message:"Фактические DoT-записи не удалось однозначно разобрать и сверить с runtime.",details:null}]
        elif ($writeProtocolEnabled|not) then [{code:"DOT_PHYSICAL_WRITE_PROTOCOL_REQUIRED",message:"Чтение DoT доступно, но в этих bytes не включён замороженный обратимый Keenetic CLI protocol.",details:null}]
        elif $selectionOverLimit then [{code:"DNS_SELECTION_OVER_LIMIT",message:("Сохранено " + (($selectedIds|length)|tostring) + " выбранных серверов при максимуме " + ($maxServers|tostring) + ". Авторитетных receipt: " + (($receipts|length)|tostring) + ". Оставьте не более " + ($maxServers|tostring) + " серверов."),details:null}]
        elif $projectedTotal>$maxServers then [{code:"DNS_CAPACITY_EXCEEDED",message:("После применения будет " + ($projectedTotal|tostring) + " защищённых DNS-серверов при максимуме " + ($maxServers|tostring) + "."),details:null}]
        elif ($selectedServers|length)==0 then [{code:"DNS_SELECTION_REQUIRED",message:"Выберите хотя бы один DNS-over-TLS-сервер.",details:null}]
        elif ($testedSelected|length)!=($selectedServers|length) then [{code:"DNS_TEST_REQUIRED",message:("Проверено " + (($testedSelected|length)|tostring) + " из " + (($selectedServers|length)|tostring) + " выбранных серверов."),details:null}]
        elif ($matchesSelection|not) then [{code:"DNS_APPLY_REQUIRED",message:"Выбранная конфигурация отличается от фактической конфигурации Keenetic.",details:null}]
        else [] end
      ) as $reasons |
      {
        schemaVersion:5,
        maxServers:$maxServers,
        maxServersSource:"Официальный максимум Keenetic: до 8 DoT/DoH-серверов",
        runningConfigFormat:"flat-or-dns-proxy-block",
        runningConfigAvailable:$runningOk,
        ndmcAvailable:$ndmcAvailable,
        testAvailable:$testAvailable,
        supported:($runningOk and $ndmcAvailable),
        recoveryRequired:$recoveryRequired,
        writeContract:{candidateId:$writeContractCandidate,protocolEnabled:$writeProtocolEnabled,
          protocolSha256:(if $writeProtocolSha256=="" then null else $writeProtocolSha256 end)},
        writeProtocolEnabled:$writeProtocolEnabled,
        mutationAvailable:$mutationAvailable,
        mutationBlockedReason:$mutationBlockedReason,
        deleteEligible:$deleteEligible,
        deleteBlockedReason:$deleteBlockedReason,
        requestedIds:$selectedIds,
        effectiveIds:$effectiveIds,
        selectedIds:$selectedIds,
        selectedCount:($selectedServers|length),
        effectiveCount:($effectiveIds|length),
        selectionOverLimit:$selectionOverLimit,
        migrationState:(if $selectionOverLimit then "selection-over-limit" else ($c.migrationState // "none") end),
        selectedTestedCount:($testedSelected|length),
        selectedPresentCount:($presentSelected|length),
        missingSelectedCount:(($selectedServers|length)-($presentSelected|length)),
        projectedTotal:$projectedTotal,
        capacityExceeded:($projectedTotal>$maxServers),
        availableSlots:([0,($maxServers-($r.totalSecure//0))]|max),
        managed:($receipts),
        quarantinedReceipts:($c.quarantinedReceipts//[]),
        managedPresentCount:($managedPresent|length),
        externalDotCount:($externalDot|length),
        servers:$servers,
        actual:{dot:$classified,dohCount:($r.dohCount//0),totalSecure:($r.totalSecure//0),
          determinate:($r.determinate//false),runtimeReconciled:($r.runtimeReconciled//false),
          runtime:($r.runtime//null)},
        installed:$installed,
        installationState:$installationState,
        observationState:(if $observationDeterminate then "determinate" else "unknown" end),
        matchesSelection:(if $observationDeterminate then $matchesSelection else null end),
        drift:$drift,
        configurationState:(
          if $recoveryRequired then "recovery-required"
          elif ($runningOk|not) or ($ndmcAvailable|not) then "unavailable"
          elif (($r.determinate//false)|not) then "observation-unknown"
          elif $selectionOverLimit then "selection-over-limit"
          elif ($selectedServers|length)==0 then "selection-required"
          elif $projectedTotal>$maxServers then "limit-exceeded"
          elif $matchesSelection then "matches-selection"
          elif ($testedSelected|length)!=($selectedServers|length) then "test-required"
          else "apply-required" end
        ),
        health:{
          schemaVersion:1,
          module:"dns",
          availability:(if $runningOk then "available" else "unavailable" end),
          severity:$severity,
          operational:$operational,
          consistent:$consistent,
          actionRequired:($severity != "ok"),
          freshness:{state:"fresh",checkedAt:(now | todateiso8601)},
          reasons:$reasons,
          facts:{
            requestedCount:($selectedIds|length),
            effectiveCount:($effectiveIds|length),
            managedCount:($receipts|length),
            managedPresentCount:($managedPresent|length),
            quarantinedReceiptCount:(($c.quarantinedReceipts//[])|length),
            maxServers:$maxServers,
            projectedTotal:$projectedTotal
          },
          lastOperation:($s.lastOperation // null)
        },
        tests:($s.tests//[]),
        testTtlSeconds:$ttl,
        lastTestedAt:$s.lastTestedAt,
        lastAppliedAt:$s.lastAppliedAt,
        lastDeletedAt:$s.lastDeletedAt,
        lastOperation:$s.lastOperation,
        lastError:$s.lastError,
        updatedAt:$s.updatedAt
      }
    ' || return 1
    rm -f "$running" "$presets"
}

# A DoT mutation is fenced by a durable marker and an exact pre-state.  The
# signal handler reconciles the live Keenetic object back to that pre-state,
# restores local receipt/state bytes and only then retires the marker.  If the
# live rollback cannot be proved, the marker remains and later mutations fail
# closed instead of silently proceeding over ambiguous router state.
BRORAY_DOT_TRANSACTION_ACTIVE=false
BRORAY_DOT_TRANSACTION_KIND=''
BRORAY_DOT_TRANSACTION_PLAN=''
BRORAY_DOT_TRANSACTION_MANAGED=''
BRORAY_DOT_TRANSACTION_RUNNING_BACKUP=''
BRORAY_DOT_TRANSACTION_CONFIG_BACKUP=''
BRORAY_DOT_TRANSACTION_STATE_BACKUP=''
BRORAY_DOT_TRANSACTION_MARKER="${BRORAY_DOT_TRANSACTION_MARKER:-$BRORAY_DOT_ROOT/transaction-recovery-required.json}"

broray_dot_transaction_require_clear()
{
    [ ! -e "$BRORAY_DOT_TRANSACTION_MARKER" ] &&
    [ ! -L "$BRORAY_DOT_TRANSACTION_MARKER" ] ||
        broray_dot_error DOT_RECOVERY_REQUIRED "Предыдущая DNS-over-TLS транзакция требует проверки и восстановления."
}

broray_dot_transaction_entry_present()
{
    actual_file="$1"
    entry_json="$2"
    jq -e --argjson entry "$entry_json" '
      def same_semantic($a;$b):
        $a.address==$b.address and $a.effectivePort==$b.effectivePort and
        $a.sni==$b.sni and ($a.spki//"")==($b.spki//"") and
        ($a.interface//$a.on//"")==($b.interface//$b.on//"") and
        ($a.domain//"")==($b.domain//"");
      ([.dot[]? | select(.valid and .unknownTokenCount==0 and same_semantic(.;$entry))]|length)==1 and
      ([.dot[]? | select(.address==$entry.address and .effectivePort==$entry.effectivePort)]|length)==1 and
      (if ($entry.receiptSchemaVersion//0)==1 then
        any(.dot[]?; same_semantic(.;$entry) and .portRaw==$entry.portRaw and .portState==$entry.portState)
       else true end)
    ' "$actual_file" >/dev/null 2>&1
}

broray_dot_add_command_for_entry()
{
    entry_json="$1"
    address="$(printf '%s\n' "$entry_json" | jq -er '.address')" || return 1
    effective_port="$(printf '%s\n' "$entry_json" | jq -er '.effectivePort')" || return 1
    sni="$(printf '%s\n' "$entry_json" | jq -er '.sni')" || return 1
    spki="$(printf '%s\n' "$entry_json" | jq -er '.spki // ""')" || return 1
    on_interface="$(printf '%s\n' "$entry_json" | jq -er '.interface // .on // ""')" || return 1
    domain="$(printf '%s\n' "$entry_json" | jq -er '.domain // ""')" || return 1
    case "$address:$effective_port:$sni:$spki:$on_interface:$domain" in
        *[[:space:]]*|*';'*|*'"'*|*'\'*) return 1 ;;
    esac
    case "$effective_port" in ''|*[!0-9]*) return 1 ;; esac
    command_text="dns-proxy tls upstream $address $effective_port sni $sni"
    [ -z "$spki" ] || command_text="$command_text spki $spki"
    [ -z "$on_interface" ] || command_text="$command_text on $on_interface"
    [ -z "$domain" ] || command_text="$command_text domain $domain"
    printf '%s\n' "$command_text"
}

broray_dot_delete_command_for_entry()
{
    entry_json="$1"
    address="$(printf '%s\n' "$entry_json" | jq -er '.address')" || return 1
    effective_port="$(printf '%s\n' "$entry_json" | jq -er '.effectivePort')" || return 1
    case "$address:$effective_port" in
        *[[:space:]]*|*';'*|*'"'*|*'\'*|*:*:*) return 1 ;;
    esac
    case "$effective_port" in ''|*[!0-9]*) return 1 ;; esac
    printf 'no dns-proxy tls upstream %s %s\n' "$address" "$effective_port"
}

broray_dot_wait_for_plan_convergence()
{
    before_file="$1"
    plan_file="$2"
    observed_file="$3"
    convergence_attempt=0
    while [ "$convergence_attempt" -lt 10 ]; do
        convergence_attempt=$((convergence_attempt + 1))
        if broray_dot_fetch_observed "$observed_file" >/dev/null 2>&1 &&
           jq -e --slurpfile before "$before_file" --slurpfile plan "$plan_file" '
             def same_semantic($a;$b):
               $a.address==$b.address and $a.effectivePort==$b.effectivePort and
               $a.sni==$b.sni and ($a.spki//"")==($b.spki//"") and
               ($a.interface//$a.on//"")==($b.interface//$b.on//"") and
               ($a.domain//"")==($b.domain//"");
             def semantic_keys($items):
               [$items[]? | [.address,.effectivePort,.sni,(.spki//""),
                 (.interface//.on//""),(.domain//"")]] | sort;
             def full_keys($items):
               [$items[]? | [.address,.portRaw,.effectivePort,.portState,.sni,
                 (.spki//""),(.interface//.on//""),(.domain//"")]] | sort;
             ($before[0].dot//[]) as $old |
             ($plan[0].remove//[]) as $remove |
             ($plan[0].add//[]) as $add |
             ([$old[] | select(. as $entry | any($remove[]?; same_semantic($entry;.)) | not)]) as $kept |
             ($kept + $add) as $expectedSemantic |
             . as $observed |
             $observed.determinate and $observed.runtimeReconciled and
             (semantic_keys($observed.dot//[])==semantic_keys($expectedSemantic)) and
             (all($kept[]?; . as $keptEntry |
               any($observed.dot[]?; same_semantic(.;$keptEntry) and .portRaw==$keptEntry.portRaw and
                 .portState==$keptEntry.portState))) and
             (($observed.dohCount//0)==($before[0].dohCount//0)) and
             (($observed.totalSecure//0)==(($before[0].totalSecure//0)-($remove|length)+($add|length)))
           ' "$observed_file" >/dev/null 2>&1; then
            return 0
        fi
        [ "$convergence_attempt" -ge 10 ] || sleep 1
    done
    return 1
}

broray_dot_transaction_restore_local()
{
    [ -f "$BRORAY_DOT_TRANSACTION_CONFIG_BACKUP" ] &&
    [ ! -L "$BRORAY_DOT_TRANSACTION_CONFIG_BACKUP" ] || return 1
    [ -f "$BRORAY_DOT_TRANSACTION_STATE_BACKUP" ] &&
    [ ! -L "$BRORAY_DOT_TRANSACTION_STATE_BACKUP" ] || return 1
    cp -p "$BRORAY_DOT_TRANSACTION_CONFIG_BACKUP" "$BRORAY_DOT_CONFIG.rollback.$$" || return 1
    cp -p "$BRORAY_DOT_TRANSACTION_STATE_BACKUP" "$BRORAY_DOT_STATE.rollback.$$" || {
        rm -f "$BRORAY_DOT_CONFIG.rollback.$$"
        return 1
    }
    mv -f "$BRORAY_DOT_CONFIG.rollback.$$" "$BRORAY_DOT_CONFIG" || return 1
    mv -f "$BRORAY_DOT_STATE.rollback.$$" "$BRORAY_DOT_STATE" || return 1
}

broray_dot_transaction_rollback()
{
    rollback_actual="$BRORAY_ROOT/tmp/dot-signal-rollback-actual.$$.json"
    rollback_verify="$BRORAY_ROOT/tmp/dot-signal-rollback-verify.$$.json"
    rollback_entries="$BRORAY_ROOT/tmp/dot-signal-rollback-entries.$$.jsonl"
    rollback_rc=0

    if [ "$BRORAY_DOT_TRANSACTION_KIND" != test ]; then
        [ -f "$BRORAY_DOT_TRANSACTION_RUNNING_BACKUP" ] &&
        [ ! -L "$BRORAY_DOT_TRANSACTION_RUNNING_BACKUP" ] || rollback_rc=1
        [ "$rollback_rc" -ne 0 ] || broray_dot_fetch_observed "$rollback_actual" >/dev/null 2>&1 || rollback_rc=1
    fi
    if [ "$rollback_rc" -eq 0 ]; then
        case "$BRORAY_DOT_TRANSACTION_KIND" in
            apply)
                jq -c '.add[]?' "$BRORAY_DOT_TRANSACTION_PLAN" >"$rollback_entries" || rollback_rc=1
                if [ "$rollback_rc" -eq 0 ]; then
                    while IFS= read -r rollback_entry
                    do
                        [ -n "$rollback_entry" ] || continue
                        if broray_dot_transaction_entry_present "$rollback_actual" "$rollback_entry"; then
                            rollback_command="$(broray_dot_delete_command_for_entry "$rollback_entry")" || rollback_rc=1
                            [ "$rollback_rc" -ne 0 ] || broray_dot_command "$rollback_command" >/dev/null 2>&1 || rollback_rc=1
                        fi
                    done <"$rollback_entries"
                fi
                jq -c '.remove[]?' "$BRORAY_DOT_TRANSACTION_PLAN" >"$rollback_entries" || rollback_rc=1
                if [ "$rollback_rc" -eq 0 ]; then
                    while IFS= read -r rollback_entry
                    do
                        [ -n "$rollback_entry" ] || continue
                        if ! broray_dot_transaction_entry_present "$rollback_actual" "$rollback_entry"; then
                            rollback_command="$(broray_dot_add_command_for_entry "$rollback_entry")" || rollback_rc=1
                            [ "$rollback_rc" -ne 0 ] || broray_dot_command "$rollback_command" >/dev/null 2>&1 || rollback_rc=1
                        fi
                    done <"$rollback_entries"
                fi
                ;;
            delete)
                jq -c '.remove[]?' "$BRORAY_DOT_TRANSACTION_PLAN" >"$rollback_entries" || rollback_rc=1
                if [ "$rollback_rc" -eq 0 ]; then
                    while IFS= read -r rollback_entry
                    do
                        [ -n "$rollback_entry" ] || continue
                        if ! broray_dot_transaction_entry_present "$rollback_actual" "$rollback_entry"; then
                            rollback_command="$(broray_dot_add_command_for_entry "$rollback_entry")" || rollback_rc=1
                            [ "$rollback_rc" -ne 0 ] || broray_dot_command "$rollback_command" >/dev/null 2>&1 || rollback_rc=1
                        fi
                    done <"$rollback_entries"
                fi
                ;;
            test)
                ;;
            *) rollback_rc=1 ;;
        esac
    fi

    if [ "$BRORAY_DOT_TRANSACTION_KIND" != test ]; then
        [ "$rollback_rc" -ne 0 ] || broray_dot_command 'system configuration save' >/dev/null 2>&1 || rollback_rc=1
        rollback_poll=0
        rollback_converged=false
        while [ "$rollback_rc" -eq 0 ] && [ "$rollback_poll" -lt 10 ]; do
            rollback_poll=$((rollback_poll + 1))
            if broray_dot_fetch_observed "$rollback_verify" >/dev/null 2>&1 &&
               jq -e --slurpfile before "$BRORAY_DOT_TRANSACTION_RUNNING_BACKUP" '
                 def keys($items): [$items[]? | [.address,.portRaw,.effectivePort,.portState,
                   .sni,.spki,.interface,.domain]] | sort;
                 .determinate and (keys(.dot//[])==keys($before[0].dot//[])) and
                 ((.dohCount//0)==($before[0].dohCount//0)) and
                 ((.totalSecure//0)==($before[0].totalSecure//0))
               ' "$rollback_verify" >/dev/null 2>&1; then
                rollback_converged=true
                break
            fi
            [ "$rollback_poll" -ge 10 ] || sleep 1
        done
        [ "$rollback_converged" = true ] || rollback_rc=1
    fi

    broray_dot_transaction_restore_local || rollback_rc=1
    rm -f "$rollback_actual" "$rollback_verify" "$rollback_entries"

    if [ "$rollback_rc" -eq 0 ]; then
        rm -f "$BRORAY_DOT_TRANSACTION_MARKER" || return 1
        [ ! -e "$BRORAY_DOT_TRANSACTION_MARKER" ] &&
        [ ! -L "$BRORAY_DOT_TRANSACTION_MARKER" ] || return 1
        sync || return 1
        return 0
    fi
    sync
    return 1
}

broray_dot_transaction_disarm()
{
    trap '' HUP INT TERM
    rm -f "$BRORAY_DOT_TRANSACTION_MARKER" || {
        trap - HUP INT TERM
        return 1
    }
    [ ! -e "$BRORAY_DOT_TRANSACTION_MARKER" ] &&
    [ ! -L "$BRORAY_DOT_TRANSACTION_MARKER" ] || {
        trap - HUP INT TERM
        return 1
    }
    sync || {
        trap - HUP INT TERM
        return 1
    }
    BRORAY_DOT_TRANSACTION_ACTIVE=false
    rm -f "$BRORAY_DOT_TRANSACTION_CONFIG_BACKUP" \
        "$BRORAY_DOT_TRANSACTION_STATE_BACKUP" 2>/dev/null || true
    sync || true
    trap - HUP INT TERM
    return 0
}

broray_dot_transaction_signal()
{
    signal_rc="$1"
    trap '' HUP INT TERM
    broray_dot_child_terminate
    if [ "$BRORAY_DOT_TRANSACTION_ACTIVE" = true ]; then
        if broray_dot_transaction_rollback >/dev/null 2>&1; then
            rm -f "$BRORAY_DOT_TRANSACTION_CONFIG_BACKUP" \
                "$BRORAY_DOT_TRANSACTION_STATE_BACKUP" 2>/dev/null || true
        fi
        BRORAY_DOT_TRANSACTION_ACTIVE=false
    fi
    exit "$signal_rc"
}

broray_dot_transaction_abort()
{
    trap '' HUP INT TERM
    broray_dot_child_terminate
    if broray_dot_transaction_rollback; then
        BRORAY_DOT_TRANSACTION_ACTIVE=false
        rm -f "$BRORAY_DOT_TRANSACTION_CONFIG_BACKUP" \
            "$BRORAY_DOT_TRANSACTION_STATE_BACKUP" 2>/dev/null || true
        return 0
    fi
    BRORAY_DOT_TRANSACTION_ACTIVE=false
    return 1
}

broray_dot_transaction_arm()
{
    BRORAY_DOT_TRANSACTION_KIND="$1"
    BRORAY_DOT_TRANSACTION_PLAN="$2"
    BRORAY_DOT_TRANSACTION_MANAGED="$3"
    BRORAY_DOT_TRANSACTION_RUNNING_BACKUP="${4:-}"
    BRORAY_DOT_TRANSACTION_CONFIG_BACKUP="$BRORAY_ROOT/tmp/dot-transaction-config.$$.json"
    BRORAY_DOT_TRANSACTION_STATE_BACKUP="$BRORAY_ROOT/tmp/dot-transaction-state.$$.json"

    broray_dot_transaction_require_clear || return 1
    case "$BRORAY_DOT_TRANSACTION_KIND" in
        apply|delete)
            [ -f "$BRORAY_DOT_TRANSACTION_PLAN" ] && [ ! -L "$BRORAY_DOT_TRANSACTION_PLAN" ] || return 1
            [ -f "$BRORAY_DOT_TRANSACTION_RUNNING_BACKUP" ] && [ ! -L "$BRORAY_DOT_TRANSACTION_RUNNING_BACKUP" ] || return 1
            ;;
        test) ;;
        *) return 1 ;;
    esac
    trap '' HUP INT TERM
    cp -p "$BRORAY_DOT_CONFIG" "$BRORAY_DOT_TRANSACTION_CONFIG_BACKUP" || {
        trap - HUP INT TERM
        return 1
    }
    cp -p "$BRORAY_DOT_STATE" "$BRORAY_DOT_TRANSACTION_STATE_BACKUP" || {
        rm -f "$BRORAY_DOT_TRANSACTION_CONFIG_BACKUP"
        trap - HUP INT TERM
        return 1
    }
    jq -n --arg kind "$BRORAY_DOT_TRANSACTION_KIND" --argjson pid "$$" --arg at "$(broray_dot_now)" \
        --arg plan "$BRORAY_DOT_TRANSACTION_PLAN" --arg managed "$BRORAY_DOT_TRANSACTION_MANAGED" \
        --arg runningBackup "$BRORAY_DOT_TRANSACTION_RUNNING_BACKUP" \
        --arg configBackup "$BRORAY_DOT_TRANSACTION_CONFIG_BACKUP" \
        --arg stateBackup "$BRORAY_DOT_TRANSACTION_STATE_BACKUP" '
      {schemaVersion:1,operation:$kind,pid:$pid,state:"active",startedAt:$at,
       recovery:{plan:(if $plan=="" then null else $plan end),
         managed:(if $managed=="" then null else $managed end),
         runningBackup:(if $runningBackup=="" then null else $runningBackup end),
         configBackup:$configBackup,stateBackup:$stateBackup}}
    ' >"$BRORAY_DOT_TRANSACTION_MARKER.part.$$" &&
    chmod 0600 "$BRORAY_DOT_TRANSACTION_MARKER.part.$$" &&
    mv -f "$BRORAY_DOT_TRANSACTION_MARKER.part.$$" "$BRORAY_DOT_TRANSACTION_MARKER" &&
    sync || {
        rm -f "$BRORAY_DOT_TRANSACTION_MARKER.part.$$" \
            "$BRORAY_DOT_TRANSACTION_CONFIG_BACKUP" \
            "$BRORAY_DOT_TRANSACTION_STATE_BACKUP"
        trap - HUP INT TERM
        return 1
    }
    BRORAY_DOT_TRANSACTION_ACTIVE=true
    trap 'broray_dot_transaction_signal 129' HUP
    trap 'broray_dot_transaction_signal 130' INT
    trap 'broray_dot_transaction_signal 143' TERM
}

broray_dot_apply()
{
    apply_request="$1"
    broray_dot_transaction_require_clear || return 1
    broray_dot_require_write_protocol || return 1
    broray_dot_ensure_files || return 1
    broray_dot_validate_request "$apply_request" || return 1
    apply_restore_exact="${BRORAY_DOT_RESTORE_EXACT:-false}"
    case "$apply_restore_exact" in true|false) ;; *) apply_restore_exact=false ;; esac
    if [ "$apply_restore_exact" = true ]; then
        jq -e --slurpfile request "$apply_request" '
          (.managed | type)=="array" and (.managed | length)>0 and
          (.requestedIds == $request[0].serverIds)
        ' "$BRORAY_DOT_CONFIG" >/dev/null 2>&1 || {
            broray_dot_error REQUEST_INVALID "Точный откат DNS-over-TLS не совпадает с сохранённым receipt."
            return 1
        }
    elif ! broray_dot_tests_fresh_and_ok "$apply_request"; then
        broray_dot_error DOT_TEST_REQUIRED "Сначала успешно проверьте все выбранные DNS-over-TLS серверы."
        return 1
    fi
    apply_desired="$BRORAY_ROOT/tmp/dot-desired.$$.json"
    apply_current="$BRORAY_ROOT/tmp/dot-current.$$.json"
    apply_plan="$BRORAY_ROOT/tmp/dot-plan.$$.json"
    apply_old_config="$BRORAY_ROOT/tmp/dot-old-config.$$.json"
    cp "$BRORAY_DOT_CONFIG" "$apply_old_config" || return 1
    broray_dot_entries_for_request "$apply_request" "$apply_desired" || {
        rm -f "$apply_old_config" "$apply_desired"
        return 1
    }
    broray_dot_fetch_observed "$apply_current" || {
        rm -f "$apply_old_config" "$apply_desired" "$apply_current"
        return 1
    }
    jq -e '.determinate and .runtimeReconciled' "$apply_current" >/dev/null 2>&1 || {
        rm -f "$apply_old_config" "$apply_desired" "$apply_current"
        broray_dot_error DOT_OBSERVATION_UNDERDETERMINED "DoT-записи running-config и runtime не сведены однозначно; изменение запрещено."
        return 1
    }
    jq -n --argjson restoreExact "$apply_restore_exact" \
      --arg protocolSha "$BRORAY_DOT_WRITE_PROTOCOL_SHA256" \
      --slurpfile desired "$apply_desired" --slurpfile current "$apply_current" \
      --slurpfile config "$BRORAY_DOT_CONFIG" '
      $desired[0] as $d | $current[0] as $r | $config[0] as $c |
      def same_semantic($a;$b): $a.address==$b.address and $a.effectivePort==$b.effectivePort and
        $a.sni==$b.sni and ($a.spki//"")==($b.spki//"") and
        ($a.interface//$a.on//"")==($b.interface//$b.on//"") and ($a.domain//"")==($b.domain//"");
      def same_full($receipt;$live): same_semantic($receipt;$live) and
        $receipt.portRaw==$live.portRaw and $receipt.portState==$live.portState;
      def selector_count($entry): [$r.dot[]? | select(.address==$entry.address and .effectivePort==$entry.effectivePort)]|length;
      ($c.managed//[]) as $receipts |
      ([$d[] as $want | select(
        (selector_count($want)>0 and ([$r.dot[]? | select(same_semantic($want;.))]|length)!=1)
      ) | $want.id]) as $conflictIds |
      ([$receipts[]? as $receipt | select(
        ([$d[]|select(.id==$receipt.id)]|length)==0 and
        ([$r.dot[]?|select(same_full($receipt;.))]|length)==1
      ) | $receipt]) as $remove |
      ([$d[] as $want | select(([$r.dot[]?|select(same_semantic($want;.))]|length)==0) | $want]) as $add |
      ([$d[] as $want | select(
        ([$r.dot[]?|select(same_semantic($want;.))]|length)==1 and
        ([$receipts[]?|select(.id==$want.id and same_full(.;($r.dot[]|select(same_semantic($want;.)))))]|length)==0
      ) | $want]) as $reusedExternal |
      ([$d[].id]|unique) as $trackedDesiredIds |
      {remove:$remove,add:$add,reusedExternal:$reusedExternal,trackedDesiredIds:$trackedDesiredIds,
       conflictIds:$conflictIds,
       afterTotal:(($r.totalSecure//0)-($remove|length)+($add|length)),
       dohCount:($r.dohCount//0),restoreExact:$restoreExact}
    ' >"$apply_plan" || {
        rm -f "$apply_old_config" "$apply_desired" "$apply_current" "$apply_plan"
        return 1
    }
    if [ "$(jq '.conflictIds|length' "$apply_plan")" -ne 0 ]; then
        apply_details="$(jq -c '.conflictIds' "$apply_plan")"
        rm -f "$apply_old_config" "$apply_desired" "$apply_current" "$apply_plan"
        broray_dot_error DOT_SELECTOR_CONFLICT "Адрес и порт выбранного DoT уже заняты иной или неоднозначной записью; внешние записи сохранены." "$apply_details"
        return 1
    fi
    apply_after_total="$(jq -r '.afterTotal' "$apply_plan")"
    if [ "$apply_after_total" -gt "$BRORAY_DOT_MAX_SERVERS" ]; then
        rm -f "$apply_old_config" "$apply_desired" "$apply_current" "$apply_plan"
        broray_dot_error DOT_LIMIT_EXCEEDED "После экспорта будет $apply_after_total из $BRORAY_DOT_MAX_SERVERS DoT/DoH-серверов."
        return 1
    fi
    broray_dot_transaction_arm apply "$apply_plan" '' "$apply_current" || {
        rm -f "$apply_desired" "$apply_current" "$apply_plan" "$apply_old_config"
        return 1
    }
    apply_remove_entries="$BRORAY_ROOT/tmp/dot-remove-entries.$$.jsonl"
    apply_add_entries="$BRORAY_ROOT/tmp/dot-add-entries.$$.jsonl"
    apply_verify="$BRORAY_ROOT/tmp/dot-verify.$$.json"
    apply_failure_file="$BRORAY_ROOT/tmp/dot-failure.$$"
    rm -f "$apply_failure_file"
    apply_failed=false
    jq -c '.remove[]?' "$apply_plan" >"$apply_remove_entries" || { printf '%s\n' plan-remove-json >"$apply_failure_file"; apply_failed=true; }
    jq -c '.add[]?' "$apply_plan" >"$apply_add_entries" || { printf '%s\n' plan-add-json >"$apply_failure_file"; apply_failed=true; }
    if [ "$apply_failed" = false ]; then
      while IFS= read -r apply_entry; do
        [ -n "$apply_entry" ] || continue
        apply_command="$(broray_dot_delete_command_for_entry "$apply_entry")" || { printf '%s\n' remove-command >"$apply_failure_file"; break; }
        broray_dot_command "$apply_command" >/dev/null 2>&1 || { printf '%s\n' "remove-command-rejected" >"$apply_failure_file"; break; }
      done <"$apply_remove_entries"
    fi
    [ ! -s "$apply_failure_file" ] || apply_failed=true
    if [ "$apply_failed" = false ]; then
      while IFS= read -r apply_entry; do
        [ -n "$apply_entry" ] || continue
        apply_command="$(broray_dot_add_command_for_entry "$apply_entry")" || { printf '%s\n' add-command >"$apply_failure_file"; break; }
        broray_dot_command "$apply_command" >/dev/null 2>&1 || { printf '%s\n' "add-command-rejected" >"$apply_failure_file"; break; }
      done <"$apply_add_entries"
      [ ! -s "$apply_failure_file" ] || apply_failed=true
    fi
    if [ "$apply_failed" = false ]; then
      broray_dot_command 'system configuration save' >/dev/null 2>&1 || { printf '%s\n' save >"$apply_failure_file"; apply_failed=true; }
    fi
    if [ "$apply_failed" = false ]; then
      broray_dot_wait_for_plan_convergence "$apply_current" "$apply_plan" "$apply_verify" || {
        printf '%s\n' verify-timeout >"$apply_failure_file"; apply_failed=true;
      }
    fi
    if [ "$apply_failed" = false ]; then
      apply_now="$(broray_dot_now)"
      apply_new_managed="$(jq -n --arg at "$apply_now" \
        --arg protocolSha "$BRORAY_DOT_WRITE_PROTOCOL_SHA256" \
        --slurpfile desired "$apply_desired" --slurpfile verify "$apply_verify" --slurpfile plan "$apply_plan" '
        def same_semantic($a;$b): $a.address==$b.address and $a.effectivePort==$b.effectivePort and
          $a.sni==$b.sni and ($a.spki//"")==($b.spki//"") and
          ($a.interface//$a.on//"")==($b.interface//$b.on//"") and ($a.domain//"")==($b.domain//"");
        [$plan[0].trackedDesiredIds[] as $id |
          ($desired[0][]|select(.id==$id)) as $wanted |
          ([$verify[0].dot[]?|select(.valid and .unknownTokenCount==0 and same_semantic($wanted;.))]) as $matches |
          if ($matches|length)!=1 or
             ([$verify[0].dot[]?|select(.address==$wanted.address and .effectivePort==$wanted.effectivePort)]|length)!=1
          then error("owned endpoint is not unique")
          else $matches[0] as $live |
            {receiptSchemaVersion:1,id:$wanted.id,provider:$wanted.provider,address:$live.address,
             portRaw:$live.portRaw,effectivePort:$live.effectivePort,portState:$live.portState,
             sni:$live.sni,spki:$live.spki,on:$live.interface,interface:$live.interface,domain:$live.domain,
             catalogIdentity:{address:$live.address,effectivePort:$live.effectivePort,sni:$live.sni},
             selector:{address:$live.address,effectivePort:$live.effectivePort},observedAt:$at,
             writeProtocolSha256:$protocolSha}
          end]
      ')" || { printf '%s\n' local-managed-build >"$apply_failure_file"; apply_failed=true; }
    fi
    if [ "$apply_failed" = false ]; then
      apply_desired_ids="$(jq '.serverIds' "$apply_request")" || { printf '%s\n' local-desired-ids >"$apply_failure_file"; apply_failed=true; }
    fi
    if [ "$apply_failed" = false ]; then
      jq --argjson selected "$apply_desired_ids" --argjson managed "$apply_new_managed" --arg at "$apply_now" '
        .schemaVersion=3 |
        .requestedIds=$selected |
        .selectedIds=$selected |
        .effectiveIds=([$managed[]?.id] | unique) |
        .migrationState="none" |
        .managed=$managed |
        .updatedAt=$at
      ' "$BRORAY_DOT_CONFIG" | broray_dot_atomic_json "$BRORAY_DOT_CONFIG" || {
          printf '%s\n' local-config-commit >"$apply_failure_file"
          apply_failed=true
      }
    fi
    if [ "$apply_failed" = false ]; then
      jq --arg at "$apply_now" --argjson selected "$apply_desired_ids" '.lastAppliedAt=$at | .lastError=null | .lastOperation={type:"apply",success:true,rolledBack:false,selectedIds:$selected,completedAt:$at} | .updatedAt=$at' "$BRORAY_DOT_STATE" | broray_dot_atomic_json "$BRORAY_DOT_STATE" || {
          printf '%s\n' local-state-commit >"$apply_failure_file"
          apply_failed=true
      }
    fi
    if [ "$apply_failed" = true ]; then
      apply_failure="$(sed -n '1p' "$apply_failure_file" 2>/dev/null)"
      apply_rolled_back=false
      broray_dot_transaction_abort >/dev/null 2>&1 && apply_rolled_back=true
      case "$apply_failure" in
        add-*) apply_failure_message="Keenetic отклонил добавление DNS-over-TLS." ;;
        remove-*) apply_failure_message="Keenetic отклонил удаление авторитетной записи BROray." ;;
        save) apply_failure_message="Keenetic не сохранил конфигурацию DNS-over-TLS." ;;
        verify-timeout) apply_failure_message="Keenetic не достиг точного состояния DNS-over-TLS за ограниченное время." ;;
        local-*) apply_failure_message="Не удалось атомарно зафиксировать локальный DoT receipt." ;;
        *) apply_failure_message="Экспорт DNS-over-TLS не завершён." ;;
      esac
      if [ "$apply_rolled_back" = true ]; then
        apply_failure_message="$apply_failure_message Выполнен и проверен откат."
      else
        apply_failure_message="$apply_failure_message Откат не подтверждён; дальнейшие изменения заблокированы recovery-marker."
      fi
      if [ "$apply_rolled_back" = true ]; then
        rm -f "$apply_desired" "$apply_current" "$apply_plan" "$apply_old_config"
      else
        rm -f "$apply_desired" "$apply_old_config"
      fi
      rm -f "$apply_remove_entries" "$apply_add_entries" "$apply_verify" "$apply_failure_file"
      broray_dot_error DOT_APPLY_FAILED "$apply_failure_message" "$apply_failure"
      return 1
    fi
    broray_dot_transaction_disarm || {
      broray_dot_error DOT_RECOVERY_REQUIRED "DNS-over-TLS применён, но транзакционный marker не удалось безопасно закрыть."
      return 1
    }
    rm -f "$apply_desired" "$apply_current" "$apply_plan" "$apply_old_config" \
      "$apply_remove_entries" "$apply_add_entries" "$apply_verify" "$apply_failure_file"
    broray_dot_status
}

broray_dot_delete()
{
    broray_dot_transaction_require_clear || return 1
    broray_dot_require_write_protocol || return 1
    broray_dot_ensure_files || return 1
    delete_request="$BRORAY_ROOT/tmp/dot-delete-request.$$.json"
    delete_selected_file="$BRORAY_ROOT/tmp/dot-delete-selected.$$.json"
    delete_current="$BRORAY_ROOT/tmp/dot-delete-current.$$.json"
    delete_plan="$BRORAY_ROOT/tmp/dot-delete-plan.$$.json"
    jq '{serverIds:(.requestedIds//.selectedIds//[])}' "$BRORAY_DOT_CONFIG" >"$delete_request" || return 1
    broray_dot_entries_for_request "$delete_request" "$delete_selected_file" || {
      rm -f "$delete_request" "$delete_selected_file"
      return 1
    }
    [ "$(jq 'length' "$delete_selected_file")" -gt 0 ] || {
      rm -f "$delete_request" "$delete_selected_file"
      broray_dot_status
      return 0
    }
    broray_dot_fetch_observed "$delete_current" || {
      rm -f "$delete_request" "$delete_selected_file" "$delete_current" "$delete_plan"
      return 1
    }
    jq -n --slurpfile selected "$delete_selected_file" --slurpfile current "$delete_current" '
      ($current[0]) as $r |
      def same_semantic($wanted;$live):
        $wanted.address==$live.address and $wanted.effectivePort==$live.effectivePort and
        $wanted.sni==$live.sni and ($wanted.spki//"")==($live.spki//"") and
        ($wanted.interface//$wanted.on//"")==($live.interface//$live.on//"") and
        ($wanted.domain//"")==($live.domain//"");
      def selector_count($wanted):
        [$r.dot[]? | select(.address==$wanted.address and .effectivePort==$wanted.effectivePort)]|length;
      {remove:[$selected[0][] as $wanted |
          ([$r.dot[]? | select(.valid and .unknownTokenCount==0 and same_semantic($wanted;.))]) as $matches |
          select(($matches|length)==1 and selector_count($wanted)==1) | $matches[0]],
       add:[],
       conflictIds:[$selected[0][] as $wanted |
          (selector_count($wanted)) as $selectorCount |
          ([$r.dot[]? | select(.valid and .unknownTokenCount==0 and same_semantic($wanted;.))]|length) as $exactCount |
          select($selectorCount>0 and ($selectorCount!=1 or $exactCount!=1)) | $wanted.id],
       missingIds:[$selected[0][] as $wanted | select(selector_count($wanted)==0) | $wanted.id]}
    ' >"$delete_plan" || {
      rm -f "$delete_request" "$delete_selected_file" "$delete_current" "$delete_plan"
      return 1
    }
    if ! jq -e '.determinate and .runtimeReconciled' "$delete_current" >/dev/null 2>&1 ||
       [ "$(jq '.conflictIds|length' "$delete_plan")" -ne 0 ]; then
      delete_details="$(jq -c '.conflictIds' "$delete_plan")"
      rm -f "$delete_request" "$delete_selected_file" "$delete_current" "$delete_plan"
      broray_dot_error DOT_SELECTOR_CONFLICT "Удаление запрещено: выбранная DoT-запись не совпадает с единственным точным selector address+port." "$delete_details"
      return 1
    fi
    broray_dot_transaction_arm delete "$delete_plan" "$delete_selected_file" "$delete_current" || {
      rm -f "$delete_request" "$delete_selected_file" "$delete_current" "$delete_plan"
      return 1
    }
    delete_entries="$BRORAY_ROOT/tmp/dot-delete-entries.$$.jsonl"
    delete_verify="$BRORAY_ROOT/tmp/dot-delete-verify.$$.json"
    delete_failure_file="$BRORAY_ROOT/tmp/dot-delete-failure.$$"
    rm -f "$delete_failure_file"
    delete_failed=false
    jq -c '.remove[]?' "$delete_plan" >"$delete_entries" || { printf '%s\n' plan-remove-json >"$delete_failure_file"; delete_failed=true; }
    if [ "$delete_failed" = false ]; then
      while IFS= read -r delete_entry; do
        [ -n "$delete_entry" ] || continue
        delete_command="$(broray_dot_delete_command_for_entry "$delete_entry")" || { printf '%s\n' remove-command >"$delete_failure_file"; break; }
        broray_dot_command "$delete_command" >/dev/null 2>&1 || { printf '%s\n' remove-command-rejected >"$delete_failure_file"; break; }
      done <"$delete_entries"
    fi
    [ ! -s "$delete_failure_file" ] || delete_failed=true
    if [ "$delete_failed" = false ]; then
      broray_dot_command 'system configuration save' >/dev/null 2>&1 || { printf '%s\n' save >"$delete_failure_file"; delete_failed=true; }
    fi
    if [ "$delete_failed" = false ]; then
      broray_dot_wait_for_plan_convergence "$delete_current" "$delete_plan" "$delete_verify" || {
        printf '%s\n' verify-timeout >"$delete_failure_file"; delete_failed=true;
      }
    fi
    if [ "$delete_failed" = false ]; then
      delete_now="$(broray_dot_now)"
      jq --arg at "$delete_now" '.requestedIds=[] | .selectedIds=[] | .managed=[] | .effectiveIds=[] | .updatedAt=$at' "$BRORAY_DOT_CONFIG" | broray_dot_atomic_json "$BRORAY_DOT_CONFIG" || {
        printf '%s\n' local-config-commit >"$delete_failure_file"
        delete_failed=true
      }
    fi
    if [ "$delete_failed" = false ]; then
      jq --arg at "$delete_now" '.lastDeletedAt=$at | .lastError=null | .lastOperation={type:"delete",success:true,rolledBack:false,completedAt:$at} | .updatedAt=$at' "$BRORAY_DOT_STATE" | broray_dot_atomic_json "$BRORAY_DOT_STATE" || {
        printf '%s\n' local-state-commit >"$delete_failure_file"
        delete_failed=true
      }
    fi
    if [ "$delete_failed" = true ]; then
      delete_failure="$(sed -n '1p' "$delete_failure_file" 2>/dev/null || true)"
      rolled_back=false
      broray_dot_transaction_abort >/dev/null 2>&1 && rolled_back=true
      if [ "$rolled_back" = true ]; then
        delete_message="Удаление DNS-over-TLS не завершено. Выполнен и проверен откат."
      else
        delete_message="Удаление DNS-over-TLS не завершено. Откат не подтверждён; дальнейшие изменения заблокированы recovery-marker."
      fi
      if [ "$rolled_back" = true ]; then
        rm -f "$delete_request" "$delete_selected_file" "$delete_current" "$delete_plan"
      fi
      rm -f "$delete_entries" "$delete_verify" "$delete_failure_file"
      broray_dot_error DOT_DELETE_FAILED "$delete_message" "$delete_failure"
      return 1
    fi
    broray_dot_transaction_disarm || {
      broray_dot_error DOT_RECOVERY_REQUIRED "DNS-over-TLS удалён, но транзакционный marker не удалось безопасно закрыть."
      return 1
    }
    rm -f "$delete_request" "$delete_selected_file" "$delete_current" "$delete_plan" \
      "$delete_entries" "$delete_verify" "$delete_failure_file"
    broray_dot_status
}
