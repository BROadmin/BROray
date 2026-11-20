# DoT probe evidence (3.5.9)

The active probe is `areas/dns/probe.go`, used by both manual checking and
the existing scheduled check. Applying router configuration remains a separate
guarded operation. This probe does not modify WAN, IPv6 or router DNS settings.

A successful check requires all three stages:

1. TCP to the selected literal address on port 853, explicitly using `tcp4` or
   `tcp6`. Hostnames, zone identifiers and IPv4-mapped IPv6 are rejected.
2. TLS 1.2 or later with normal CA-chain and SNI hostname verification. The
   existing Entware CA-bundle locations remain supported.
3. A valid root-zone IN NS response over that same TLS connection. Go's DNS
   parser validates the response and requires at least one NS result. The
   root NS lookup cannot be satisfied by the hosts file. The resolver cannot
   open another connection or fall back to ordinary system DNS.

The complete probe has a 12-second maximum, shortened by a caller deadline.
Cancellation closes the connection. Resolver-internal read timeouts retain
their timeout diagnosis even if its subsequent dial attempt is refused.

Manual and scheduled `state.json` test rows, exposed as `servers[].test` in
DoT status, retain existing fields and add:

| Field | Values / meaning |
| --- | --- |
| `addressFamily` | `IPv4` or `IPv6`, determined from the selected literal address |
| `tcpStatus` | `ok`, `failed`, `not_checked` |
| `tlsStatus` | `ok`, `failed`, `not_checked` |
| `dnsStatus` | `ok`, `failed`, `not_checked` |
| `errorCode` | Empty on success; otherwise one of the codes below |

Codes: `DOT_ADDRESS_INVALID`, `DOT_CA_UNAVAILABLE`, `DOT_CONNECT_FAILED`,
`DOT_IPV6_PATH_UNAVAILABLE`, `DOT_TLS_FAILED`, `DOT_DNS_FAILED`,
`DOT_PROBE_CANCELLED`, `DOT_PROBE_TIMEOUT`. A network-unreachable,
host-unreachable or unsupported-family error on IPv6 produces the path code;
a connection timeout alone is not proof that IPv6 is absent.

`ok` is true only after all three stages succeed. `latencyMs` measures the
complete probe in milliseconds; it is null when the CA store is unavailable.
Old stored rows without the extra fields remain readable and are not upgraded
to new evidence. Existing saved selection and the test/apply separation remain.

This is upstream connectivity evidence from the BROray process. It does not
prove that the Keenetic DoT component is installed, that the firmware accepts
IPv6 upstream commands, or that the router has applied this server. Those
capability, ownership/readback and physical-router checks are separate gates.

## IPv6 catalog and router records

The eight existing IPv4 presets retain their IDs, order, addresses and SNI.
Eight IPv6 choices are appended, with `addressFamily` in each catalog object.
The saved selection is unchanged; the combined DoT/DoH capacity remains eight.

| IDs | Addresses | SNI |
| --- | --- | --- |
| `google-primary-ipv6`, `google-secondary-ipv6` | `2001:4860:4860::8888`, `2001:4860:4860::8844` | `dns.google` |
| `cloudflare-primary-ipv6`, `cloudflare-secondary-ipv6` | `2606:4700:4700::1111`, `2606:4700:4700::1001` | `one.one.one.one` |
| `quad9-primary-ipv6`, `quad9-secondary-ipv6` | `2620:fe::fe`, `2620:fe::9` | `dns.quad9.net` |
| `adguard-primary-ipv6`, `adguard-secondary-ipv6` | `2a10:50c0::ad1:ff`, `2a10:50c0::ad2:ff` | `dns.adguard-dns.com` |

Official sources checked 2026-11-17:
[Google addresses](https://developers.google.com/speed/public-dns/docs/using),
[Google DoT](https://developers.google.com/speed/public-dns/docs/dns-over-tls),
[Cloudflare DoT](https://developers.cloudflare.com/1.1.1.1/encryption/dns-over-tls/),
[Quad9 services](https://docs.quad9.net/services/),
[AdGuard default resolver](https://adguard-dns.io/kb/general/dns-providers/#default).
These sources establish catalog data, not reachability from a particular router.

[Yandex's official page](https://dns.yandex.ru/) lists IPv6 addresses for ordinary
DNS but labels its DoT endpoint IPv4. Its existing IPv4 preset is retained;
an IPv6 DoT preset awaits specific confirmation.
[KeeneticOS 5.1 documentation](https://support.keenetic.com/titan-se/kn-4210/en/101291-latest-main-release.html)
explicitly describes IPv6 secure DNS addresses. It does not establish the
earliest compatible firmware version or a particular router's capabilities.

Both command allowlists and observation parsers admit literal unzoned IPv4 and
IPv6. Brackets, prefixes, mapped addresses, malformed literals and command
injection are rejected. Address comparison uses canonical IP identity, so
compressed/expanded spellings reconcile and duplicate selectors still block
mutation. Raw observations, receipt fields and deletion fingerprints retain
their original spelling; port representation and extra attributes remain checked.

The existing receipt protocol identifier is retained for compatibility; it is
not a checksum of the 3.5.9 Go source. Only a matching trusted protocol receipt
can authorize removal or retained ownership during apply/uninstall. Selecting
an existing external endpoint never adopts it. Uninstall requires determinate,
reconciled observations before removal and on readback; uncertain readback
retains the receipt. Explicit confirmed deletion retains its existing contract.

Local parser, command, rollback, confirmed-deletion and ownership tests do not
replace the pending firmware/component diagnostics, WebUI presentation or
physical Keenetic IPv6 apply/resolve/remove checks.

## Saved selection and automatic checking

The existing test button also saves an empty selection explicitly. This clears
only the saved selection/test results; it does not apply/delete DNS records,
change ownership receipts, or change the automatic-check preference. Apply and
delete still reject empty selections. The browser distinguishes an unsaved empty
choice from a saved empty choice and retains the existing action buttons.

Automatic-check settings expose a revision derived from the same validated file
snapshot as `enabled`. New clients send `enabled` and `expectedRevision`; stale
revisions are rejected under the existing operation fence. The legacy enabled-only
request remains compatible. Duplicate, unknown, incorrectly cased, null or trailing
request data are rejected, and invalid stored settings are preserved for diagnosis.

The switch autosaves only its own preference, preserving unsaved server choices.
After an ambiguous response it reads back without another write; if readback fails,
the switch stays unavailable until fresh valid status confirms the state. Older
poll responses cannot overwrite a completed save. Preference errors do not claim
that router DNS records were mutated. TCP, TLS and DNS stages are displayed
separately; IPv6 endpoints are bracketed and compared by canonical identity.

These guarantees have separate Go, Node and local fake-API browser evidence.
Physical router and real backend/browser integration gates remain open.

## Router capability evidence

`capabilities.dotComponent` is present, missing or unknown. Read-only
`show/version` is bounded by a three-second context and parsed only from the
observed `ndw.components` CSV schema: bounded unique component names with a base
marker. The independent `dns-tls` component is required; `ndns` and `dns-https`
do not substitute for it. Unknown, malformed or unavailable inventory is not an
absence. A fully reconciled System TLS runtime independently confirms presence;
contradictory inventory/runtime evidence is unknown. Explicit absence blocks
apply before transaction commands; unknown preserves the existing strict
observation/ownership/rollback gates. Manual upstream probing remains separate.

The schema is based on first-hand show/version output posted by device owners in
[the manufacturer forum](https://forum.keenetic.com/topic/14184-delete-component-keendns-re-appears/).
It is not a claim of universal firmware schema coverage. Unrecognized schemas
remain unknown; the actual target must still be checked physically.

`ipv6DoT` is confirmed only for an IPv6 upstream in reconciled router runtime.
A successful BROray probe is evidence of its own connection, not of the router's
DNS proxy capability. Firmware is displayed as observed; no minimum version is
inferred. Official [KeeneticOS 5.1 notes](https://support.keenetic.com/peak/kn-2710/en/9109-latest-main-release.html)
confirm IPv6 secure-DNS configuration, but are not used as a capability predicate.
No component installation, firmware update or global IPv6 change is automatic.
