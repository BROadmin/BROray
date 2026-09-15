#!/opt/bin/ash
# Isolated candidate library; installed runtime and subscription catalog untouched.
set -eu
umask 077
T=/opt/tmp/broray-311-subscription-presentation-20260916
RAM=/tmp/broray-311-subscription-presentation-20260916
[ "$(readlink -f "$T")" = "$T" ] && [ ! -L "$T" ]
[ "$(cat "$T/TEST-OWNER")" = BRORAY311-SUBSCRIPTION-PRESENTATION-20260916 ]
[ ! -e "$RAM" ] && [ ! -L "$RAM" ]
mkdir -m 700 "$RAM"; echo BRORAY311-SUBSCRIPTION-PRESENTATION-20260916 >"$RAM/TEST-OWNER"
PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH LD_LIBRARY_PATH="$T/lib:/opt/lib"
export BRORAY_ROOT="$T/app" BRORAY_BASE="$T/app" BRORAY_STATE_ROOT="$T/state"
export BRORAY_PROXY_HOST=127.0.0.1 BRORAY_PROXY_PORT=2080 BRORAY_INTERFACE=Proxy0
export BRORAY_OPS_GUARD="$T/bin/broray-ops-guard" BRORAY_OPS_SUPERVISOR="$T/bin/broray-ops-supervisor"
export BRORAY_OPS_ASH=/opt/bin/ash BRORAY_OPS_RAM_ROOT="$RAM"
export BRORAY_ROUTES_API_LOCK="$T/global.lock" BRORAY_LEGACY_GLOBAL_LOCK="$T/legacy.lock" BRORAY_OPS_UPDATER_ROOT="$T/updater"
cd "$T"; sha256sum -c SHA256SUMS >/dev/null
[ "$(uname -m)" = aarch64 ]
mkdir -p "$T/app/tmp" "$T/app/config/subscriptions" "$T/state/operations/op-presentation"
. "$T/app/lib/subscription-service.sh"
: >"$T/passed.txt"
pass() { printf '%s\n' "$1" | tee -a "$T/passed.txt"; }

nslookup api.brovibe.cloud >"$T/nslookup.txt"
broray_subscription_resolve_public_ip api.brovibe.cloud
[ "$BRORAY_SUB_RESOLVED_IP" = 85.9.223.218 ]
pass real_keenetic_dns_selects_literal_address

broray_subscription_fetch 'https://api.brovibe.cloud/releases/staging/broray/3.1.1-r02c01/release.json' "$T/download.json" ''
[ "$(sha256sum "$T/download.json" | awk '{print $1}')" = fb78b0788bdc516d5921ac0cc1cba522d7e38c408fbee4d408e2778d1aae5868 ]
pass actual_curl_download_matches_immutable_signed_index

for address in reverse.example 127.0.0.1 10.0.0.1 172.16.1.1 192.168.1.1 100.64.1.1 01.2.3.4 1.2.3.999 ::1 ::ffff:127.0.0.1 2001:0DB8::1 2606::1:; do
  if broray_subscription_ip_is_public "$address"; then exit 1; fi
done
broray_subscription_ip_is_public 2606:4700:4700::1111
pass literal_and_private_address_rejection

printf '%s\n' '{"kind":"background","operationId":"op-presentation","running":false,"state":"aborted","errorCode":"CANCELLED","finishedAt":"2026-09-15T22:29:33Z"}' >"$T/state/operations/op-presentation/state.json"
printf '%s\n' '{"schemaVersion":1,"id":"test","name":"Test","url":"https://example.test/list","enabled":true,"autoUpdateEnabled":false,"updateIntervalMinutes":60,"lastUpdateStatus":"running","backgroundOperationId":"op-presentation","lastUpdatedAt":"2026-09-15T22:27:44Z","lastUpdateResult":{"errorCode":"HTTP_ERROR","durationMs":8000},"createdAt":"2026-09-15T22:00:00Z","updatedAt":"2026-09-15T22:29:06Z","serversReceived":0}' >"$T/app/config/subscriptions/test.json"
cp "$T/app/config/subscriptions/test.json" "$T/subscription.before"
broray_subscription_get test >"$T/public.json"
jq -e '.lastUpdateStatus=="error" and .lastUpdateResult.errorCode=="CANCELLED" and .lastUpdateResult.durationMs==null and .lastUpdatedAt=="2026-09-15T22:29:33Z"' "$T/public.json" >/dev/null
cmp -s "$T/subscription.before" "$T/app/config/subscriptions/test.json"
pass cancelled_projection_is_current_and_read_only

broray_job_begin system subscriptions:refresh subscriptions USER cooperative
broray_subscription_recover_stale
broray_job_finish completed
jq -e '.lastUpdateStatus=="error" and .lastUpdateResult.errorCode=="CANCELLED" and .lastUpdateResult.durationMs==null and .lastUpdatedAt=="2026-09-15T22:29:33Z"' "$T/app/config/subscriptions/test.json" >/dev/null
[ ! -e "$T/global.lock" ] && [ ! -L "$T/global.lock" ]
pass admitted_recovery_preserves_terminal_meaning

jq -Rn '[inputs] | {status:"PASS",tests:.,installedApplicationChanged:false}' <"$T/passed.txt" >"$T/RESULT.json"
