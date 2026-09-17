# Subscription device information — stage 09

Scope: optional device-description request headers. No HWID, server-schema, VPN-core,
router-policy or updater changes. Base: 725c53f3ae2077e9f1abb8db0129554abc59c320 plus exact stages 01–08.

## User contract

`sendDeviceInfo` is an optional boolean on each subscription. Missing means false.
New subscriptions also default to false. The existing form contains one visible checkbox:
«Сообщать провайдеру модель и версию роутера». Partial settings updates preserve it.
No migration rewrites existing records or regenerates their clientHwid. Changes take effect
on the next subscription fetch, not by restarting Xray. Disabling cannot delete previously
received information from the provider. The existing HWID is still sent as before.
The setting applies to this subscription, including a deliberately edited subscription URL.

## Outgoing fields when opted in

- `X-Device-OS`: `KeeneticOS`, only when show-version identifies a supported Keenetic model/vendor.
- `X-Device-Model`: strictly the KN-xxxx model index, not a unique identifier.
- `X-Ver-OS`: human `title` with a validated `release` fallback; never ndw.version.
- `X-App-Version`: actual BROray release version, independent of a custom User-Agent.
`User-Agent` and `X-HWID` retain their existing behavior. Unknown fields are omitted.
The backend does not accept model/OS strings from the browser. Marketing names are not sent.
No MAC, serial, service tag, hostname, Wi-Fi name, user name, credentials or client inventory
are read for these headers. The full show-version output is transient in a private directory;
only whitelisted fields are returned, and raw diagnostics are not sent to the provider.

## Read boundary

A single `ndmc -c 'show version'` per opted-in fetch, using the existing locator when loaded,
otherwise PATH lookup. No requests on module load; no extra read when disabled.
The query has timeout 2 seconds plus 1 second kill grace and a child-only file-size limit;
accepted output is limited to 16 KiB. Missing timeout/ndmc, read failure, invalid fields or
unrecognized device are nonfatal. Private output is cleaned up. The owner and its limits
are not changed. No RCI token is created, no authentication is bypassed, no router write occurs.
Supported input: single JSON object or whitelisted key:value lines. Conflicting model indices
are not resolved by guessing; model is omitted. Unknown legacy model identifiers stay unknown.

## Recipient and trust boundary

Device fields are sent only to the initial validated origin (scheme, lower-case host, port).
A change of origin in redirects permanently suppresses these four fields for that fetch,
even if it redirects back. Existing cross-origin HWID behavior is unchanged. No arbitrary
headers are introduced, no automatic Happ/Android impersonation, no browser-device guessing.
Provider-side display and platform icons remain controlled by the provider.
Request headers describe the subscription requester; they do not identify each VPN connection.

## Evidence and pending acceptance

Unit tests use actual shell/JQ/file limits/timeout, but synthetic ndmc/curl/DNS.
Protected update tests use real owner/native guard/supervisor/commit with synthetic HTTP/ndmc.
Browser tests exercise the existing form with intercepted API and a stubbed global shell.
No real provider, router, physical connection or package installation is claimed.
Before candidate acceptance: verify show-version output on target KeeneticOS builds;
verify captured outgoing headers and the provider's record using an authorized test subscription;
verify missing-device-info does not interrupt updates, and switching off keeps the old HWID.
The pending general audit gates, including atomic DoT delete confirmation, remain open.

## Sources

https://docs.rw/features/hwid-device-limit/ — optional device OS/model/version headers.
https://docs.v2raytun.com/overview/device-limit — includes X-App-Version.
https://forum.keenetic.ru/topic/14319-opkg-on-speedster/ — firsthand show-version field examples.
Existing BROray runtime/app/lib/interface-core.sh and interface-owner.sh already use show version.
These sources establish external conventions; no closed-client parser internals are claimed.
