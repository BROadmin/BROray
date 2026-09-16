# Xray JSON subscription import for 3.1.1

Requested for this release on 2026-09-16 after the anonymized Axo response was
received. Original source: `09ea1a15b8c2c8b77c36f53be88d7594b45b3ab5`.

## Baseline and lifecycle

The received document is valid JSON: 14 profiles, containing 22 VLESS outbound
occurrences (not necessarily distinct servers), 14 freedom and 14 blackhole.
Ten profiles contain one VLESS outbound; four contain three and balancing rules.
Transport/security pairs are gRPC/REALITY, gRPC/TLS and TCP/REALITY.

The unchanged extractor returns `PARSE_ERROR` on this input, matching the supplied
screenshot. Offline evidence is in the workspace's
`docs/evidence/axo-subscription-format-20260916/format-analysis.json`. No supplied
credentials, addresses or raw profiles were copied to the repository/evidence,
and the provider was not contacted.

Download and parsing run inside the bounded subscription helper's private
directory. The owner commits only after helper completion. The existing staging
pipeline validates each URI, deduplicates by connection identity, assigns stable
IDs and `source.subscriptionId`, and retains old servers if preparation fails.
The JSON adapter must feed that pipeline, not publish servers itself.

## Scope

Accept a complete Xray object or array of such objects, plain or Base64. Extract
individual supported VLESS TCP/RAW or gRPC endpoints with TLS/REALITY/none, using
the existing URI parser and server schema. Do not import freedom/blackhole as
servers. Preserve endpoint credentials, SNI, REALITY parameters, TLS ALPN,
fingerprint, flow, gRPC serviceName/authority/multiMode and profile names.
This adapter accepts DNS/IPv4 endpoint addresses. Other address forms and other
protocol/transport configurations are rejected with the existing node warnings;
this is not a general importer for every Xray configuration.

DNS, inbound listeners, routing, balancing, observatory and profile fragmentation
settings are not router configuration. Report that distinction explicitly in
the subscription's existing warnings. Unknown connection requirements (for
example proxy chaining, sockopt or unsupported stream settings) must produce a
rejected node with a fixed warning, never be silently stripped. No user values
from rejected JSON enter that warning.

Malformed/unrecognized JSON and all-invalid payloads leave the existing server
catalog intact. Repeated updates keep the subscription link and stable IDs;
other subscriptions and manual servers remain untouched. Existing URI/Base64
lists remain supported.

## Acceptance

Required: parser field preservation and encoded characters, multi-node profiles,
deduplication, stable update identity and credential rotation, invalid update
preservation, limits, unsupported fields, unchanged URI formats, and ARM prefix
checks. Full installed-candidate and actual provider VPN connectivity are
separate release/integration checks; local passing tests cannot establish them.

## Failures found during integration

- `subscription-json-baseline-20260916`: the old extractor rejects supported
  JSON. The URI regression also exposed a test-only assertion joining two empty
  warning files with a newline; the assertion was corrected separately.
- `subscription-json-linux-20260916`: BusyBox base64 accepted some plain JSON
  punctuation as ignorable data. Detect JSON before attempting Base64, so the
  decoder cannot replace a plain JSON document with meaningless bytes.
- `subscription-json-linux-20260916-02`: all 9 JSON tests PASS. The existing
  subscription regression then failed WebUI admission: subscriptions used the
  old `routes` scope and inherited the new protected-route policy.
- `subscription-api-scope-baseline-20260916` proves that unchanged WebUI
  admission records `scope=routes, cancelability=protected`. Subscription API
  and CLI entry points now use `system`, matching their existing service tests;
  route cancellation policy and global exclusion remain unchanged.

## Accepted checkpoint

- `subscription-json-linux-20260916-03`: 28 PASS (1 admission, 12 subscription
  lifecycle, 6 presentation, 9 JSON/import/update checks), 582.81 seconds in
  the offline Linux guest. Final runtime hashes are recorded in its manifest.
- `physical-subscription-json-20260916-02`: 7 PASS using ARM/Entware and
  synthetic HTTP responses in a private prefix. Includes actual WebUI-handler
  and CLI update paths, stable IDs and credential rotation, protection of other
  sources, bad-update preservation, Base64/multiMode and Xray config validation.
  The earlier 5 checks are repeated and are not counted twice.
- The actual anonymized input produces 22 VLESS URI occurrences through the
  unchanged production extractor in a local harness. No unique-server or live
  connectivity claim follows from this structural check.
- Both physical namespaces were archived and retired. The installed Xray's
  PID, birth identity, executable and command hash remained unchanged.
- Source integrity/syntax checks PASS. The feature is committed for 3.1.1;
  the full candidate is not installed or published by this checkpoint. Live
  provider fetching/VPN connectivity and overall release acceptance remain open.
