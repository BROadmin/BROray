#!/opt/bin/ash

# BROray DNS-over-TLS manager for KeeneticOS.
# Stores ownership locally and never removes secure DNS entries that were not
# created by BROray.

BRORAY_ROOT="${BRORAY_ROOT:-/opt/broray}"
BRORAY_DOT_ROOT="${BRORAY_DOT_ROOT:-$BRORAY_ROOT/routes/dot}"
BRORAY_DOT_CONFIG="${BRORAY_DOT_CONFIG:-$BRORAY_DOT_ROOT/config.json}"
BRORAY_DOT_STATE="${BRORAY_DOT_STATE:-$BRORAY_DOT_ROOT/state.json}"
BRORAY_DOT_NDMC="${BRORAY_DOT_NDMC:-ndmc}"
BRORAY_DOT_OPENSSL="${BRORAY_DOT_OPENSSL:-openssl}"
BRORAY_DOT_TIMEOUT="${BRORAY_DOT_TIMEOUT:-timeout}"
BRORAY_DOT_TEST_TTL="${BRORAY_DOT_TEST_TTL:-600}"
BRORAY_DOT_MAX_SERVERS=8

broray_dot_now() { date '+%Y-%m-%dT%H:%M:%S%z'; }
broray_dot_epoch() { date '+%s'; }

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
  {"id":"google-primary","provider":"Google","name":"Google 8.8.8.8","address":"8.8.8.8","sni":"dns.google"},
  {"id":"google-secondary","provider":"Google","name":"Google 8.8.4.4","address":"8.8.4.4","sni":"dns.google"},
  {"id":"cloudflare-primary","provider":"Cloudflare","name":"Cloudflare 1.1.1.1","address":"1.1.1.1","sni":"cloudflare-dns.com"},
  {"id":"cloudflare-secondary","provider":"Cloudflare","name":"Cloudflare 1.0.0.1","address":"1.0.0.1","sni":"cloudflare-dns.com"},
  {"id":"quad9","provider":"Quad9","name":"Quad9 9.9.9.9","address":"9.9.9.9","sni":"dns.quad9.net"},
  {"id":"adguard-primary","provider":"AdGuard DNS","name":"AdGuard 94.140.14.14","address":"94.140.14.14","sni":"dns.adguard-dns.com"},
  {"id":"adguard-secondary","provider":"AdGuard DNS","name":"AdGuard 94.140.15.15","address":"94.140.15.15","sni":"dns.adguard-dns.com"}
]
JSON
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

broray_dot_ensure_files()
{
    broray_dot_require_runtime || return 1
    if [ ! -s "$BRORAY_DOT_CONFIG" ]; then
        jq -n '{schemaVersion:1,selectedIds:["google-primary","cloudflare-primary","quad9"],managed:[],updatedAt:null}' |
            broray_dot_atomic_json "$BRORAY_DOT_CONFIG" || return 1
    fi
    if [ ! -s "$BRORAY_DOT_STATE" ]; then
        jq -n '{schemaVersion:1,tests:[],lastTestedAt:null,lastAppliedAt:null,lastDeletedAt:null,lastOperation:null,lastError:null,updatedAt:null}' |
            broray_dot_atomic_json "$BRORAY_DOT_STATE" || return 1
    fi
    jq -e '(.schemaVersion==1) and ((.selectedIds|type)=="array") and ((.managed|type)=="array")' "$BRORAY_DOT_CONFIG" >/dev/null 2>&1 ||
        broray_dot_error CONFIG_INVALID "Конфигурация DNS-over-TLS повреждена."
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
    output="$1"
    raw="$output.raw"
    tsv="$output.tsv"
    ndmc_bin="$(broray_dot_ndmc_path)"
    [ -n "$ndmc_bin" ] || broray_dot_error NDMC_UNAVAILABLE "Команда ndmc недоступна."
    "$ndmc_bin" -c 'show running-config' >"$raw" 2>"$output.err" || {
        details="$(tail -n 20 "$output.err" 2>/dev/null)"
        rm -f "$raw" "$tsv" "$output.err"
        broray_dot_error KEENETIC_UNAVAILABLE "Не удалось прочитать конфигурацию Keenetic." "$details"
    }
    awk '
      {
        line=$0; sub(/^[[:space:]]+/,"",line); sub(/[[:space:]]+$/,"",line)
        n=split(line,f,/[[:space:]]+/)
        if (n>=4 && f[1]=="dns-proxy" && f[2]=="tls" && f[3]=="upstream") {
          address=f[4]; sni=""; port=""
          i=5
          if (i<=n && f[i] ~ /^[0-9]+$/) { port=f[i]; i++ }
          while (i<=n) { if (f[i]=="sni" && i<n) { sni=f[i+1]; break } i++ }
          if (sni!="") print "dot\t" address "\t" sni "\t" port
        } else if (n>=3 && f[1]=="dns-proxy" && f[2]=="https" && f[3]=="upstream") {
          print "doh\t" f[4] "\t\t"
        }
      }
    ' "$raw" >"$tsv" || { rm -f "$raw" "$tsv" "$output.err"; return 1; }
    jq -Rn '
      [inputs | split("\t") | {kind:.[0],address:.[1],sni:(if .[2]=="" then null else .[2] end),port:(if .[3]=="" then null else (.[3]|tonumber) end)}] as $all |
      {schemaVersion:1,source:"running-config",dot:[$all[]|select(.kind=="dot")|del(.kind)],dohCount:([$all[]|select(.kind=="doh")]|length),totalSecure:($all|length)}
    ' <"$tsv" >"$output" || { rm -f "$raw" "$tsv" "$output.err"; return 1; }
    rm -f "$raw" "$tsv" "$output.err"
}

broray_dot_validate_request()
{
    request="$1"
    [ -r "$request" ] || broray_dot_error REQUEST_INVALID "Запрос DNS-over-TLS отсутствует."
    presets="$BRORAY_ROOT/tmp/dot-presets.$$.json"
    broray_dot_presets >"$presets"
    jq -e --slurpfile presets "$presets" '
      (.serverIds|type)=="array" and (.serverIds|length)>=1 and (.serverIds|length)<=8 and
      ((.serverIds|unique|length)==(.serverIds|length)) and
      all(.serverIds[]; . as $id | any($presets[0][]; .id==$id)) and
      ((.allowUntested // false)|type)=="boolean"
    ' "$request" >/dev/null 2>&1 || { rm -f "$presets"; broray_dot_error REQUEST_INVALID "Выбран некорректный список DNS-over-TLS серверов."; }
    rm -f "$presets"
}

broray_dot_entries_for_request()
{
    request="$1"
    output="$2"
    presets="$BRORAY_ROOT/tmp/dot-presets.$$.json"
    broray_dot_presets >"$presets"
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
    jq -e --argjson now "$now" --argjson ttl "$BRORAY_DOT_TEST_TTL" --slurpfile req "$request" '
      . as $state | $req[0].serverIds as $ids |
      all($ids[]; . as $id | any($state.tests[]?; .id==$id and .ok==true and (($now-(.testedEpoch//0)) <= $ttl)))
    ' "$BRORAY_DOT_STATE" >/dev/null 2>&1
}

broray_dot_test()
{
    request="$1"
    broray_dot_ensure_files || return 1
    broray_dot_validate_request "$request" || return 1
    entries="$BRORAY_ROOT/tmp/dot-test-entries.$$.json"
    results="$BRORAY_ROOT/tmp/dot-test-results.$$.jsonl"
    broray_dot_entries_for_request "$request" "$entries" || return 1
    : >"$results"
    openssl_bin="$(broray_dot_openssl_path)"
    timeout_bin="$(broray_dot_timeout_path)"
    tested_at="$(broray_dot_now)"
    tested_epoch="$(broray_dot_epoch)"
    jq -c '.[]' "$entries" | while IFS= read -r entry; do
        id="$(printf '%s' "$entry" | jq -r '.id')"
        address="$(printf '%s' "$entry" | jq -r '.address')"
        sni="$(printf '%s' "$entry" | jq -r '.sni')"
        fixture="${BRORAY_DOT_TEST_FIXTURE_DIR:-}/$id.json"
        if [ -n "${BRORAY_DOT_TEST_FIXTURE_DIR:-}" ] && [ -r "$fixture" ]; then
            jq -c --arg id "$id" --arg address "$address" --arg sni "$sni" --arg testedAt "$tested_at" --argjson testedEpoch "$tested_epoch" '
              {id:$id,address:$address,sni:$sni,ok:(.ok==true),status:(.status//(if .ok then "ok" else "failed" end)),latencyMs:(.latencyMs//null),message:(.message//null),testedAt:$testedAt,testedEpoch:$testedEpoch}
            ' "$fixture" >>"$results"
            continue
        fi
        if [ -z "$openssl_bin" ]; then
            jq -nc --arg id "$id" --arg address "$address" --arg sni "$sni" --arg testedAt "$tested_at" --argjson testedEpoch "$tested_epoch" '{id:$id,address:$address,sni:$sni,ok:false,status:"unavailable",latencyMs:null,message:"OpenSSL недоступен.",testedAt:$testedAt,testedEpoch:$testedEpoch}' >>"$results"
            continue
        fi
        out="$BRORAY_ROOT/tmp/dot-openssl-$id.$$.out"
        start="$(broray_dot_epoch)"
        if [ -n "$timeout_bin" ]; then
            "$timeout_bin" 12 "$openssl_bin" s_client -connect "$address:853" -servername "$sni" -verify_hostname "$sni" -verify_return_error -brief </dev/null >"$out" 2>&1
        else
            "$openssl_bin" s_client -connect "$address:853" -servername "$sni" -verify_hostname "$sni" -verify_return_error -brief </dev/null >"$out" 2>&1
        fi
        rc=$?
        finish="$(broray_dot_epoch)"
        latency=$(((finish-start)*1000))
        if [ "$rc" -eq 0 ]; then ok=true; status=ok; message="TLS-соединение и имя сертификата проверены."; else ok=false; status=failed; message="TLS-проверка завершилась ошибкой."; fi
        jq -nc --arg id "$id" --arg address "$address" --arg sni "$sni" --arg status "$status" --arg message "$message" --arg testedAt "$tested_at" --argjson testedEpoch "$tested_epoch" --argjson latencyMs "$latency" --argjson ok "$ok" '{id:$id,address:$address,sni:$sni,ok:$ok,status:$status,latencyMs:$latencyMs,message:$message,testedAt:$testedAt,testedEpoch:$testedEpoch}' >>"$results"
        rm -f "$out"
    done
    tests_json="$(jq -s '.' "$results")" || return 1
    jq --argjson tests "$tests_json" --arg testedAt "$tested_at" --argjson testedEpoch "$tested_epoch" --arg updatedAt "$(broray_dot_now)" '.tests=$tests | .lastTestedAt=$testedAt | .lastTestedEpoch=$testedEpoch | .lastError=null | .updatedAt=$updatedAt' "$BRORAY_DOT_STATE" |
        broray_dot_atomic_json "$BRORAY_DOT_STATE" || return 1
    rm -f "$entries" "$results"
    broray_dot_status
}

broray_dot_entry_key() { printf '%s|%s\n' "$1" "$2"; }

broray_dot_command()
{
    command_text="$1"
    ndmc_bin="$(broray_dot_ndmc_path)"
    [ -n "$ndmc_bin" ] || broray_dot_error NDMC_UNAVAILABLE "Команда ndmc недоступна."
    "$ndmc_bin" -c "$command_text"
}

broray_dot_status()
{
    broray_dot_ensure_files || return 1
    running="$BRORAY_ROOT/tmp/dot-running.$$.json"
    presets="$BRORAY_ROOT/tmp/dot-presets.$$.json"
    running_ok=true
    broray_dot_fetch_running "$running" >/dev/null 2>&1 || { running_ok=false; jq -n '{schemaVersion:1,dot:[],dohCount:0,totalSecure:0}' >"$running"; }
    broray_dot_presets >"$presets"
    jq -n --slurpfile presets "$presets" --slurpfile config "$BRORAY_DOT_CONFIG" --slurpfile state "$BRORAY_DOT_STATE" --slurpfile running "$running" --argjson runningOk "$running_ok" --argjson maxServers "$BRORAY_DOT_MAX_SERVERS" '
      $config[0] as $c | $state[0] as $s | $running[0] as $r |
      def key($e): ($e.address+"|"+$e.sni);
      ($c.managed // []) as $managed |
      ($r.dot // []) as $actual |
      ($presets[0] | map(. as $p | $p + {
        selected: (($c.selectedIds//[]) | index($p.id) != null),
        present: (any($actual[]?; .address==$p.address and .sni==$p.sni)),
        managed: (any($managed[]?; .address==$p.address and .sni==$p.sni)),
        test: ([$s.tests[]? | select(.id==$p.id)] | last // null)
      })) as $servers |
      ($managed | map(. as $m | any($actual[]?; .address==$m.address and .sni==$m.sni)) | all) as $allManagedPresent |
      {
        schemaVersion:1,
        maxServers:$maxServers,
        runningConfigAvailable:$runningOk,
        selectedIds:($c.selectedIds//[]),
        managed:($managed),
        servers:$servers,
        actual:{dot:($actual),dohCount:($r.dohCount//0),totalSecure:($r.totalSecure//0)},
        installed:(($managed|length)>0 and $allManagedPresent),
        drift:(($managed|length)>0 and ($allManagedPresent|not)),
        tests:($s.tests//[]),
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

broray_dot_apply()
{
    request="$1"
    broray_dot_ensure_files || return 1
    broray_dot_validate_request "$request" || return 1
    allow="$(jq -r '.allowUntested // false' "$request")"
    if ! broray_dot_tests_fresh_and_ok "$request" && [ "$allow" != true ]; then
        broray_dot_error DOT_TEST_CONFIRMATION_REQUIRED "Не все выбранные DNS-over-TLS серверы успешно проверены. Требуется явное подтверждение."
        return 1
    fi
    desired="$BRORAY_ROOT/tmp/dot-desired.$$.json"
    current="$BRORAY_ROOT/tmp/dot-current.$$.json"
    plan="$BRORAY_ROOT/tmp/dot-plan.$$.json"
    old_config="$BRORAY_ROOT/tmp/dot-old-config.$$.json"
    cp "$BRORAY_DOT_CONFIG" "$old_config" || return 1
    broray_dot_entries_for_request "$request" "$desired" || return 1
    broray_dot_fetch_running "$current" || return 1
    jq -n --slurpfile desired "$desired" --slurpfile current "$current" --slurpfile config "$BRORAY_DOT_CONFIG" '
      $desired[0] as $d | $current[0] as $r | $config[0] as $c |
      def same($a;$b): $a.address==$b.address and $a.sni==$b.sni;
      ($c.managed//[]) as $oldManaged |
      ([$r.dot[]? | select(. as $a | any($oldManaged[]?; same($a;.)) | not)]) as $external |
      ([$oldManaged[]? | select(. as $m | any($d[]; same($m;.)) | not)]) as $remove |
      ([$d[] | select(. as $want | any($r.dot[]?; same($want;.)) | not)]) as $add |
      ([$d[] | select(. as $want | any($external[]?; same($want;.)))]) as $reusedExternal |
      (($r.dohCount//0) + ($external|length) + ([$d[] | select(. as $want | any($external[]?; same($want;.)) | not)]|length)) as $afterTotal |
      {remove:$remove,add:$add,reusedExternal:$reusedExternal,afterTotal:$afterTotal,dohCount:($r.dohCount//0),externalDotCount:($external|length),oldManaged:$oldManaged}
    ' >"$plan" || return 1
    after_total="$(jq -r '.afterTotal' "$plan")"
    [ "$after_total" -le "$BRORAY_DOT_MAX_SERVERS" ] || { rm -f "$desired" "$current" "$plan" "$old_config"; broray_dot_error DOT_LIMIT_EXCEEDED "После экспорта будет больше восьми DoT/DoH серверов."; return 1; }
    added="$BRORAY_ROOT/tmp/dot-added.$$.jsonl"; removed="$BRORAY_ROOT/tmp/dot-removed.$$.jsonl"; : >"$added"; : >"$removed"
    failed=false; failure=""
    jq -c '.remove[]' "$plan" | while IFS= read -r e; do
        a="$(printf '%s' "$e"|jq -r '.address')"; s="$(printf '%s' "$e"|jq -r '.sni')"
        if broray_dot_command "no dns-proxy tls upstream $a sni $s" >/dev/null 2>&1; then printf '%s\n' "$e" >>"$removed"; else printf '%s\n' "remove:$a:$s" >"$BRORAY_ROOT/tmp/dot-failure.$$"; break; fi
    done
    [ ! -s "$BRORAY_ROOT/tmp/dot-failure.$$" ] || failed=true
    if [ "$failed" = false ]; then
      jq -c '.add[]' "$plan" | while IFS= read -r e; do
        a="$(printf '%s' "$e"|jq -r '.address')"; s="$(printf '%s' "$e"|jq -r '.sni')"
        if broray_dot_command "dns-proxy tls upstream $a sni $s" >/dev/null 2>&1; then printf '%s\n' "$e" >>"$added"; else printf '%s\n' "add:$a:$s" >"$BRORAY_ROOT/tmp/dot-failure.$$"; break; fi
      done
      [ ! -s "$BRORAY_ROOT/tmp/dot-failure.$$" ] || failed=true
    fi
    if [ "$failed" = false ]; then broray_dot_command 'system configuration save' >/dev/null 2>&1 || { echo save >"$BRORAY_ROOT/tmp/dot-failure.$$"; failed=true; }; fi
    verify="$BRORAY_ROOT/tmp/dot-verify.$$.json"
    if [ "$failed" = false ]; then
      broray_dot_fetch_running "$verify" >/dev/null 2>&1 || failed=true
      if [ "$failed" = false ]; then
        jq -e --slurpfile desired "$desired" '. as $actual | all($desired[0][]; . as $w | any($actual.dot[]?; .address==$w.address and .sni==$w.sni))' "$verify" >/dev/null 2>&1 || failed=true
      fi
    fi
    if [ "$failed" = true ]; then
      [ -s "$added" ] && jq -c '.' "$added" | while IFS= read -r e; do a="$(printf '%s' "$e"|jq -r '.address')"; s="$(printf '%s' "$e"|jq -r '.sni')"; broray_dot_command "no dns-proxy tls upstream $a sni $s" >/dev/null 2>&1 || true; done
      [ -s "$removed" ] && jq -c '.' "$removed" | while IFS= read -r e; do a="$(printf '%s' "$e"|jq -r '.address')"; s="$(printf '%s' "$e"|jq -r '.sni')"; broray_dot_command "dns-proxy tls upstream $a sni $s" >/dev/null 2>&1 || true; done
      broray_dot_command 'system configuration save' >/dev/null 2>&1 || true
      failure="$(cat "$BRORAY_ROOT/tmp/dot-failure.$$" 2>/dev/null)"
      jq --arg at "$(broray_dot_now)" --arg error "$failure" '.lastError={code:"DOT_APPLY_FAILED",message:"Экспорт DNS-over-TLS не завершён. Изменения отменены.",details:$error} | .lastOperation={type:"apply",success:false,rolledBack:true,completedAt:$at} | .updatedAt=$at' "$BRORAY_DOT_STATE" | broray_dot_atomic_json "$BRORAY_DOT_STATE" || true
      rm -f "$desired" "$current" "$plan" "$old_config" "$added" "$removed" "$verify" "$BRORAY_ROOT/tmp/dot-failure.$$"
      broray_dot_error DOT_APPLY_FAILED "Экспорт DNS-over-TLS не завершён. Выполнен откат." "$failure"
      return 1
    fi
    added_json="$(jq -s '.' "$added")"; old_managed="$(jq '.managed//[]' "$old_config")"; desired_ids="$(jq '.serverIds' "$request")"
    new_managed="$(jq -n --argjson old "$old_managed" --argjson added "$added_json" --slurpfile desired "$desired" '
      def same($a;$b): $a.address==$b.address and $a.sni==$b.sni;
      [$desired[0][] as $d | select(any($old[]?; same($d;.)) or any($added[]?; same($d;.))) | {id:$d.id,address:$d.address,sni:$d.sni,provider:$d.provider}]
    ')"
    now="$(broray_dot_now)"
    jq --argjson selected "$desired_ids" --argjson managed "$new_managed" --arg at "$now" '.selectedIds=$selected | .managed=$managed | .updatedAt=$at' "$BRORAY_DOT_CONFIG" | broray_dot_atomic_json "$BRORAY_DOT_CONFIG" || return 1
    jq --arg at "$now" --argjson selected "$desired_ids" '.lastAppliedAt=$at | .lastError=null | .lastOperation={type:"apply",success:true,rolledBack:false,selectedIds:$selected,completedAt:$at} | .updatedAt=$at' "$BRORAY_DOT_STATE" | broray_dot_atomic_json "$BRORAY_DOT_STATE" || return 1
    rm -f "$desired" "$current" "$plan" "$old_config" "$added" "$removed" "$verify" "$BRORAY_ROOT/tmp/dot-failure.$$"
    broray_dot_status
}

broray_dot_delete()
{
    broray_dot_ensure_files || return 1
    managed="$(jq -c '.managed//[]' "$BRORAY_DOT_CONFIG")"
    [ "$(printf '%s' "$managed" | jq 'length')" -gt 0 ] || { broray_dot_status; return 0; }
    removed="$BRORAY_ROOT/tmp/dot-delete-removed.$$.jsonl"; : >"$removed"; failed=false
    printf '%s' "$managed" | jq -c '.[]' | while IFS= read -r e; do
      a="$(printf '%s' "$e"|jq -r '.address')"; s="$(printf '%s' "$e"|jq -r '.sni')"
      if broray_dot_command "no dns-proxy tls upstream $a sni $s" >/dev/null 2>&1; then printf '%s\n' "$e" >>"$removed"; else echo "$a|$s" >"$BRORAY_ROOT/tmp/dot-delete-failure.$$"; break; fi
    done
    [ ! -s "$BRORAY_ROOT/tmp/dot-delete-failure.$$" ] || failed=true
    if [ "$failed" = false ]; then broray_dot_command 'system configuration save' >/dev/null 2>&1 || failed=true; fi
    verify="$BRORAY_ROOT/tmp/dot-delete-verify.$$.json"
    if [ "$failed" = false ]; then
      broray_dot_fetch_running "$verify" >/dev/null 2>&1 || failed=true
      if [ "$failed" = false ]; then printf '%s' "$managed" | jq -e --slurpfile actual "$verify" '$actual[0] as $a | all(.[]; . as $m | any($a.dot[]?; .address==$m.address and .sni==$m.sni) | not)' >/dev/null 2>&1 || failed=true; fi
    fi
    if [ "$failed" = true ]; then
      [ -s "$removed" ] && jq -c '.' "$removed" | while IFS= read -r e; do a="$(printf '%s' "$e"|jq -r '.address')"; s="$(printf '%s' "$e"|jq -r '.sni')"; broray_dot_command "dns-proxy tls upstream $a sni $s" >/dev/null 2>&1 || true; done
      broray_dot_command 'system configuration save' >/dev/null 2>&1 || true
      now="$(broray_dot_now)"; jq --arg at "$now" '.lastError={code:"DOT_DELETE_FAILED",message:"Удаление DNS-over-TLS не завершено. Выполнен откат."} | .lastOperation={type:"delete",success:false,rolledBack:true,completedAt:$at} | .updatedAt=$at' "$BRORAY_DOT_STATE" | broray_dot_atomic_json "$BRORAY_DOT_STATE" || true
      rm -f "$removed" "$verify" "$BRORAY_ROOT/tmp/dot-delete-failure.$$"
      broray_dot_error DOT_DELETE_FAILED "Удаление DNS-over-TLS не завершено. Выполнен откат."
      return 1
    fi
    now="$(broray_dot_now)"
    jq --arg at "$now" '.managed=[] | .updatedAt=$at' "$BRORAY_DOT_CONFIG" | broray_dot_atomic_json "$BRORAY_DOT_CONFIG" || return 1
    jq --arg at "$now" '.lastDeletedAt=$at | .lastError=null | .lastOperation={type:"delete",success:true,rolledBack:false,completedAt:$at} | .updatedAt=$at' "$BRORAY_DOT_STATE" | broray_dot_atomic_json "$BRORAY_DOT_STATE" || return 1
    rm -f "$removed" "$verify" "$BRORAY_ROOT/tmp/dot-delete-failure.$$"
    broray_dot_status
}
