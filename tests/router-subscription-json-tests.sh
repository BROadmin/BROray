#!/opt/bin/ash
# Private app state and synthetic HTTP response. No provider request/activation.
set -eu
umask 077
T=/opt/tmp/broray-311-subscription-json-20260916
RAM=/tmp/broray-311-subscription-json-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-SUBSCRIPTION-JSON-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-SUBSCRIPTION-JSON-20260916 >"$RAM/TEST-OWNER"
PATH="$T/app/bin:$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app" BRORAY_STATE_ROOT="$T/state"
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock"
export BRORAY_OPS_UPDATER_ROOT="$T/updater" BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_PROXY_HOST=127.0.0.1 BRORAY_PROXY_PORT=2080 BRORAY_INTERFACE=Proxy0
unset BRORAY_OPS_TEST BRORAY_OPS_TEST_IDENTITIES BRORAY_OPS_PROC_ROOT
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/tmp" "$T/app/bin" "$T/app/config/subscriptions" "$T/app/config/system" "$T/app/logs"
ln -s "$T/bin/jq" "$T/app/bin/jq"
cat >"$T/app/bin/curl" <<'CURL'
#!/opt/bin/ash
while [ "$#" -gt 0 ]; do
  case "$1" in --dump-header) headers="$2"; shift ;; --output) body="$2"; shift ;; esac
  shift
done
printf 'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n' >"$headers"
cp "$TEST_PAYLOAD" "$body"
printf 200
CURL
chmod 700 "$T/app/bin/curl"
export TEST_PAYLOAD="$T/payload.json"
jq -nc '{remarks:"JSON fixture",dns:{servers:["dns.example.invalid"]},routing:{rules:[]},outbounds:[
 {protocol:"vless",tag:"proxy",settings:{vnext:[{address:"vpn.example.invalid",port:443,
 users:[{id:"11111111-2222-4333-8444-555555555555",encryption:"none",flow:""}]}]},
 streamSettings:{network:"grpc",security:"reality",grpcSettings:{serviceName:"api/a?b&c",authority:"authority.example.invalid",mode:false},
 realitySettings:{serverName:"sni.example.invalid",fingerprint:"firefox",publicKey:"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",shortId:"aabbccdd"}}},
 {protocol:"freedom"},{protocol:"blackhole"}]}|[.,(.|.remarks="Balanced"|.routing.balancers=[{selector:["proxy"]}])]' >"$TEST_PAYLOAD"
jq -nc '{schemaVersion:1,id:"test",name:"JSON test",url:"https://93.184.216.34/sub/fixture",
 clientHwid:"broray-1234567890abcdef1234567890abcdef",enabled:true,autoUpdateEnabled:true,updateIntervalMinutes:60,
 lastUpdateStatus:"never",nextUpdateEpoch:1,createdAt:"2026-09-16T00:00:00Z",updatedAt:"2026-09-16T00:00:00Z",serversReceived:0}' \
 >"$T/app/config/subscriptions/test.json"
cat >"$T/job.sh" <<'JOB'
. "$BRORAY_ROOT/lib/subscription-service.sh"
broray_job_begin system subscriptions:refresh subscriptions USER cooperative || exit $?
trap 'broray_job_exit "$?"' EXIT
broray_subscription_update test manual
exit $?
JOB
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }
catalog_hash() { find "$T/app/servers" -name '*.json' -type f -exec sha256sum '{}' ';' | sort | sha256sum; }
/opt/bin/ash "$T/job.sh" >"$T/first.json" 2>"$T/first.err"
jq -e '.lastUpdateStatus=="partial" and .lastUpdateResult.accepted==1 and .lastUpdateResult.rejected==1 and (.lastUpdateResult.warnings|length)>0' "$T/first.json" >/dev/null
set -- "$T/app/servers"/*.json; [ "$#" = 1 ]; file="$1"
jq -e '.source.subscriptionId=="test" and .transport.serviceName=="api/a?b&c" and .transport.host=="authority.example.invalid" and .reality.fingerprint=="firefox"' "$file" >/dev/null
pass json_update_preserves_fields_deduplicates_and_warns

# Use the same importer on Base64 JSON and canonical gRPC multiMode.
jq '.[0]|.outbounds[0].streamSettings.grpcSettings.multiMode=true|del(.outbounds[0].streamSettings.realitySettings.shortId)' "$TEST_PAYLOAD" >"$T/multi.json"
base64 "$T/multi.json" >"$T/multi.base64"
(
 . "$T/app/lib/subscription-service.sh"
 broray_subscription_extract_nodes "$T/multi.base64" "$T/multi.nodes"
 broray_subscription_stage_nodes fixture "$T/multi.nodes" "$T/multi-stage" true
)
for multi in "$T/multi-stage"/*.json; do
 jq -e '.transport.mode=="multi" and .reality.shortId=="" and .source.subscriptionId=="fixture"' "$multi" >/dev/null
done
pass base64_json_multimode_and_optional_short_id

id="$(jq -r .id "$file")"
jq '.id="manual-fixture"|.source={type:"manual"}' "$file" >"$T/app/servers/manual-fixture.json"
jq '.id="subscription-other-fixture"|.source.subscriptionId="other"' "$file" >"$T/app/servers/subscription-other-fixture.json"
sha256sum "$T/app/servers/manual-fixture.json" "$T/app/servers/subscription-other-fixture.json" >"$T/foreign.sha256"
jq '.[0].remarks="Renamed"|.[0].outbounds[0].settings.vnext[0].users[0].id="aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"|[.[0]]' "$TEST_PAYLOAD" >"$T/next.json"
mv "$T/next.json" "$TEST_PAYLOAD"
/opt/bin/ash "$T/job.sh" >"$T/second.json" 2>"$T/second.err"
jq -e --arg id "$id" '.id==$id and .name=="Renamed" and .uuid=="aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" and .source.subscriptionId=="test"' "$file" >/dev/null
sha256sum -c "$T/foreign.sha256" >/dev/null
pass repeated_update_keeps_id_rotates_credentials_and_preserves_other_sources

before="$(catalog_hash)"; printf '%s\n' '{"servers":[]}' >"$TEST_PAYLOAD"
rc=0; /opt/bin/ash "$T/job.sh" >"$T/invalid.json" 2>"$T/invalid.err" || rc=$?
[ "$rc" = 1 ] && [ "$(catalog_hash)" = "$before" ]
jq -e '.lastUpdateStatus=="error" and .lastUpdateResult.errorCode=="PARSE_ERROR"' "$T/app/config/subscriptions/test.json" >/dev/null
pass invalid_update_preserves_last_catalog

# Generate a disposable config and ask the installed Xray binary to validate it.
# -test does not launch inbounds or activate a server.
printf '%s\n' '{"listenAddress":"127.0.0.1","socksPort":22080}' >"$T/app/config/system/settings.json"
. "$T/app/lib/server-config-generator.sh"
config="$(broray_generate_server_config "$id")"
[ "$config" = "$T/app/tmp/server-config.new.json" ]
/opt/broray/runtime/xray run -test -config "$config" >"$T/xray-config-test.txt" 2>&1
pass generated_json_server_config_is_accepted_by_xray

cp "$T/multi.json" "$TEST_PAYLOAD"
/opt/bin/ash -c '. "$BRORAY_ROOT/web-new/api/subscriptions/common.sh"; broray_subscriptions_api_lock refresh; broray_subscriptions_api_run broray_subscription_update test manual' >"$T/api-response.txt" 2>"$T/api-response.err"
tr -d '\r' <"$T/api-response.txt" | sed '1,/^$/d' >"$T/api-body.json"
jq -e '.success and .data.lastUpdateStatus=="success"' "$T/api-body.json" >/dev/null
operation="$(jq -r .backgroundOperationId "$T/app/config/subscriptions/test.json")"
jq -e '.scope=="system" and .initialCancelability=="cooperative" and .state=="completed"' "$T/state/operations/$operation/state.json" >/dev/null
pass web_api_json_update_has_cancellable_subscription_job

/opt/bin/ash "$T/app/bin/broray-subscriptions" refresh test >"$T/cli-response.json" 2>"$T/cli-response.err"
operation="$(jq -r .backgroundOperationId "$T/app/config/subscriptions/test.json")"
jq -e '.scope=="system" and .initialCancelability=="cooperative" and .state=="completed"' "$T/state/operations/$operation/state.json" >/dev/null
pass cli_json_update_has_cancellable_subscription_job

[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
for state in "$T/state/operations"/*/state.json; do jq -e '.running==false' "$state" >/dev/null; done
for ledger in "$T/state/operations"/*/supervisors.json; do jq -e '.supervisors==[]' "$ledger" >/dev/null; done
for pidfile in /proc/[0-9]*/cmdline; do
  pid="${pidfile#/proc/}"; pid="${pid%%/*}"; [ "$pid" != "$$" ] || continue
  [ -r "$pidfile" ] || continue
  # Read outside the tested tree. Only the current test shell may remain.
  command="$(tr '\000' ' ' <"$pidfile" 2>/dev/null || true)"
  case "$command" in *"$T/app/"*|*"$T/job.sh"*) exit 1 ;; esac
done
jq -Rn '[inputs|select(length>0)]|{status:"PASS",tests:.,routerAccessed:true,applicationInstalled:false,providerAccessed:false,transport:"synthetic local response"}' <"$T/passed.txt" >"$T/RESULT.json"
cat "$T/RESULT.json"
