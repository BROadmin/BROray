#!/opt/bin/ash
set -u
T=/opt/tmp/broray-311-test-20260915
export PATH="$T/bin:/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin" LD_LIBRARY_PATH="$T/lib:/opt/lib"
echo REGEX_SMOKE
jq -n '"100" | test("^[0-9]+$")'
printf 'REGEX_RC=%s\n' "$?"
OPS_APP="$T/app"; OPS_PROC=/proc
. "$T/app/lib/operation-owner.sh"
snapshot="$(broray_ops_capture_owner $$)"
printf '%s\n' "$snapshot" | jq -e 'type=="object" and (.pid|type)=="number" and .pid>1 and
      (.startTicks|type)=="string" and (.startTicks|test("^[0-9]+$")) and
      (.bootId|type)=="string" and (.bootId|length)>0 and
      (.executable|type)=="string" and (.executable|startswith("/")) and
      (.commandDigest|type)=="string" and (.commandDigest|test("^[a-f0-9]{64}$"))'
printf 'VALIDATION_RC=%s\n' "$?"
