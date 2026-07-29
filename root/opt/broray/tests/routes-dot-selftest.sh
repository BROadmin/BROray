#!/bin/sh
set -eu

ROOT="${BRORAY_ROOT:-/opt/broray}"
WORK="$ROOT/routes/tmp/routes-dot-selftest-$$"
DOT_ROOT="$WORK/dot"
MOCK="$WORK/mock-bin"
CONFIG="$WORK/running-config.txt"
FIXTURES="$WORK/fixtures"
CLI="$ROOT/bin/broray-routes-dot"

cleanup() { rm -rf "$WORK"; }
fail() { echo "ОШИБКА routes-dot self-test: $*" >&2; exit 1; }
trap cleanup EXIT HUP INT TERM
mkdir -p "$MOCK" "$FIXTURES" "$DOT_ROOT"

cat >"$MOCK/ndmc" <<'MOCK'
#!/bin/sh
set -eu
[ "${1:-}" = "-c" ] || exit 2
cmd="${2:-}"
case "$cmd" in
  'show running-config') cat "$MOCK_NDMC_CONFIG" ;;
  'system configuration save') exit 0 ;;
  dns-proxy\ tls\ upstream\ *)
    [ "${MOCK_NDMC_FAIL_MATCH:-}" = "" ] || case "$cmd" in *"$MOCK_NDMC_FAIL_MATCH"*) exit 9 ;; esac
    grep -Fx "$cmd" "$MOCK_NDMC_CONFIG" >/dev/null 2>&1 || printf '%s\n' "$cmd" >>"$MOCK_NDMC_CONFIG"
    ;;
  no\ dns-proxy\ tls\ upstream\ *)
    [ "${MOCK_NDMC_FAIL_MATCH:-}" = "" ] || case "$cmd" in *"$MOCK_NDMC_FAIL_MATCH"*) exit 9 ;; esac
    target="${cmd#no }"
    grep -Fvx "$target" "$MOCK_NDMC_CONFIG" >"$MOCK_NDMC_CONFIG.new" || true
    mv "$MOCK_NDMC_CONFIG.new" "$MOCK_NDMC_CONFIG"
    ;;
  *) exit 3 ;;
esac
MOCK
chmod 755 "$MOCK/ndmc"

cat >"$FIXTURES/google-primary.json" <<'JSON'
{"ok":true,"status":"ok","latencyMs":31,"message":"OK"}
JSON
cat >"$FIXTURES/cloudflare-primary.json" <<'JSON'
{"ok":true,"status":"ok","latencyMs":24,"message":"OK"}
JSON
cat >"$FIXTURES/quad9.json" <<'JSON'
{"ok":false,"status":"failed","latencyMs":1000,"message":"timeout"}
JSON

export BRORAY_ROOT="$ROOT"
export BRORAY_DOT_ROOT="$DOT_ROOT"
export BRORAY_DOT_CONFIG="$DOT_ROOT/config.json"
export BRORAY_DOT_STATE="$DOT_ROOT/state.json"
export BRORAY_DOT_NDMC="$MOCK/ndmc"
export BRORAY_DOT_TEST_FIXTURE_DIR="$FIXTURES"
export MOCK_NDMC_CONFIG="$CONFIG"

cat >"$CONFIG" <<'CFG'
dns-proxy tls upstream 149.112.112.112 sni dns.quad9.net
dns-proxy https upstream https://example.test/dns-query dnsm
CFG

cat >"$WORK/request.json" <<'JSON'
{"serverIds":["google-primary","cloudflare-primary"],"allowUntested":false}
JSON

"$CLI" status >"$WORK/status.json" || fail "status"
jq -e '.actual.totalSecure==2 and .actual.dohCount==1 and (.managed|length)==0' "$WORK/status.json" >/dev/null || fail "неверный начальный статус"

"$CLI" test "$WORK/request.json" >"$WORK/test.json" || fail "test"
jq -e '[.tests[]|select(.ok==true)]|length==2' "$WORK/test.json" >/dev/null || fail "TLS-тесты не сохранены"

"$CLI" apply "$WORK/request.json" >"$WORK/apply.json" || fail "apply"
grep -Fx 'dns-proxy tls upstream 149.112.112.112 sni dns.quad9.net' "$CONFIG" >/dev/null || fail "чужая DoT-запись удалена"
grep -Fx 'dns-proxy https upstream https://example.test/dns-query dnsm' "$CONFIG" >/dev/null || fail "DoH-запись удалена"
grep -Fx 'dns-proxy tls upstream 8.8.8.8 sni dns.google' "$CONFIG" >/dev/null || fail "Google не добавлен"
grep -Fx 'dns-proxy tls upstream 1.1.1.1 sni cloudflare-dns.com' "$CONFIG" >/dev/null || fail "Cloudflare не добавлен"
jq -e '.installed==true and (.managed|length)==2 and .actual.totalSecure==4' "$WORK/apply.json" >/dev/null || fail "неверный статус после экспорта"

cat >"$WORK/update.json" <<'JSON'
{"serverIds":["cloudflare-primary","quad9"],"allowUntested":true}
JSON
"$CLI" apply "$WORK/update.json" >"$WORK/update-result.json" || fail "update"
! grep -Fx 'dns-proxy tls upstream 8.8.8.8 sni dns.google' "$CONFIG" >/dev/null || fail "старая управляемая запись не удалена"
grep -Fx 'dns-proxy tls upstream 1.1.1.1 sni cloudflare-dns.com' "$CONFIG" >/dev/null || fail "сохраняемая запись пропала"
grep -Fx 'dns-proxy tls upstream 9.9.9.9 sni dns.quad9.net' "$CONFIG" >/dev/null || fail "Quad9 не добавлен"
grep -Fx 'dns-proxy tls upstream 149.112.112.112 sni dns.quad9.net' "$CONFIG" >/dev/null || fail "чужая запись повреждена при обновлении"

"$CLI" delete >"$WORK/delete.json" || fail "delete"
! grep -Fx 'dns-proxy tls upstream 1.1.1.1 sni cloudflare-dns.com' "$CONFIG" >/dev/null || fail "управляемая Cloudflare не удалена"
! grep -Fx 'dns-proxy tls upstream 9.9.9.9 sni dns.quad9.net' "$CONFIG" >/dev/null || fail "управляемая Quad9 не удалена"
grep -Fx 'dns-proxy tls upstream 149.112.112.112 sni dns.quad9.net' "$CONFIG" >/dev/null || fail "чужая запись удалена вместе с BROray"
jq -e '.installed==false and (.managed|length)==0' "$WORK/delete.json" >/dev/null || fail "неверный статус после удаления"

# Лимит восьми защищённых DNS-серверов.
: >"$CONFIG"
i=1
while [ "$i" -le 7 ]; do printf 'dns-proxy tls upstream 192.0.2.%s sni external%s.example\n' "$i" "$i" >>"$CONFIG"; i=$((i+1)); done
printf '%s\n' 'dns-proxy https upstream https://external.example/dns-query dnsm' >>"$CONFIG"
if "$CLI" apply "$WORK/request.json" >"$WORK/limit.out" 2>"$WORK/limit.err"; then fail "экспорт сверх лимита разрешён"; fi
grep -F 'DOT_LIMIT_EXCEEDED' "$WORK/limit.err" >/dev/null || fail "неверная ошибка лимита"
[ "$(wc -l <"$CONFIG" | tr -d ' ')" = 8 ] || fail "конфигурация изменилась при ошибке лимита"

# Откат частично выполненного обновления.
cat >"$CONFIG" <<'CFG'
dns-proxy tls upstream 149.112.112.112 sni dns.quad9.net
CFG
rm -f "$BRORAY_DOT_CONFIG" "$BRORAY_DOT_STATE"
"$CLI" test "$WORK/request.json" >/dev/null || fail "повторный test"
"$CLI" apply "$WORK/request.json" >/dev/null || fail "подготовительный apply"
cat >"$WORK/failing.json" <<'JSON'
{"serverIds":["quad9"],"allowUntested":true}
JSON
export MOCK_NDMC_FAIL_MATCH='9.9.9.9'
if "$CLI" apply "$WORK/failing.json" >"$WORK/fail.out" 2>"$WORK/fail.err"; then fail "ошибочный apply завершился успешно"; fi
unset MOCK_NDMC_FAIL_MATCH
grep -Fx 'dns-proxy tls upstream 8.8.8.8 sni dns.google' "$CONFIG" >/dev/null || fail "Google не восстановлен откатом"
grep -Fx 'dns-proxy tls upstream 1.1.1.1 sni cloudflare-dns.com' "$CONFIG" >/dev/null || fail "Cloudflare не восстановлен откатом"
grep -Fx 'dns-proxy tls upstream 149.112.112.112 sni dns.quad9.net' "$CONFIG" >/dev/null || fail "чужая запись пропала при откате"
jq -e '.lastOperation.success==false and .lastOperation.rolledBack==true' "$BRORAY_DOT_STATE" >/dev/null || fail "откат не зафиксирован"

echo 'BROray DNS-over-TLS self-test: PASS'
