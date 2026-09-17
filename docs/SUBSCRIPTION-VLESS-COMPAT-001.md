# Subscription VLESS compatibility — stage 05

This extends endpoint import, not whole Xray-profile execution.
Base: `b8aab47470d8a0cdee39be374c349723aac176e0`.
Only `subscription-xray-json.jq` and `server-config-generator.sh` change in runtime.

## Supported extension

VLESS JSON uses the existing endpoint model and URI importer, including nested
`settings.vnext[].users[]` or flat `settings.address/port/id/encryption/flow`.
Added JSON transport mapping: WS, HTTPUpgrade, XHTTP/SplitHTTP; existing TCP/RAW
and gRPC remain. Accepted aliases map to existing canonical network names.
IPv6 literals are validated without regex and emitted in brackets. Existing
parser/model identity is unchanged; Xray 26.9.9 ParseAddress accepts brackets.
REALITY accepts `password` or `publicKey`; differing simultaneous values fail.
WS legacy Host headers map to host only; unknown headers do not disappear silently.
XHTTP Host already present in transport.host is now preserved by the generator
for both URI and JSON imports. Tuning is restricted to a validated allowlist.
Direct tuning together with `extra` is rejected because the core treats extra as
replacement, not a merge. Unknown mandatory options are not discarded.
Output for one outbound is buffered up to max_nodes+1; a later invalid user in
that outbound cannot leak earlier users before the unsupported marker.

## Deliberate boundaries

No HTTP metadata/HWID/User-Agent or Shadowsocks parser changes in this package.
No subscriptions/user data migration, new schema, ID algorithm or frontend change.
No arbitrary routing, DNS, inbounds, chained outbounds or balance semantics are
installed in Keenetic. Existing extraction warnings remain; inherited acceptance
of top-level profile fields and outbound `fragment` does NOT mean they execute.
No claim of identical Happ/v2RayTun internals or of all their inputs being supported.
