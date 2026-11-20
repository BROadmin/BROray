# Routing policy API (3.5.9)

This additive API edits custom domain rules, managed GeoIP/GeoSite selections,
and independent rule controls. It queues selected-data preparation and per-kind
manual updates. Existing BAT/IP-route endpoints and schedules keep their
contracts. The optional daily geodata scheduler shares the existing minute loop.
The static pages `/routes-domains.html` and `/routes-blocking.html` use this API.
Their only section navigation is the expandable routing submenu.

The domains page exposes individual ready-service switches and custom text-list
add/import/edit controls within the custom-list card. New lists remain disabled;
explicit enabling first confirms the VPN destination, including after an
interrupted creation. Editing content preserves enabled/action settings. The
editor retains its original name/domain signature: a passive refresh cannot
authorize overwriting another tab's content, and a conflict keeps the draft.

The blocking page separates enabled switches from deny/direct controls and
offers per-kind update buttons plus the independently saved daily setting.
Country choices use ISO codes; each selected source file must still pass the
backend's fetch/native/core checks. Presets use verified published DLC tags;
the illustrative `category-gambling` tag was absent and is not exposed.

The shared browser client stores a queue nonce before sending a geo operation.
A lost reply is recovered by observing that nonce, without a second mutation.
Unknown outcomes block further writes until explicit successful readback.
Independent daily changes remain possible while policy work is busy. User text
is rendered as text, and passive state updates leave the editor DOM untouched.

## Readback

`GET /api/routes/policy.cgi` uses the existing CGI success/error envelope.
Its `data` contains:

```json
{
  "state": {"schemaVersion": 1, "rules": []},
  "revision": "<64 lowercase hex characters>",
  "busy": false,
  "pending": false
}
```

`revision` is SHA-256 of canonical serialized state, including disabled rules
and the managed geodata revision. It is a concurrency token, not an upstream
signature. Missing saved state means empty/default-off. Invalid or unsafe state
is an error; the reader never repairs it.

The UI must lock mutations while busy/pending or when readback fails. A lost
HTTP response requires readback before further edits. A successful old response
must not overwrite a newer UI revision. The API returns saved state, not form
fields the browser has not submitted. `busy` includes queued/running geodata
requests. Optional `state.geoUpdatedAt` maps `geoip`/`geosite` to successful
check/update timestamps in UTC RFC3339 format. Old state without this field is
valid; null, unknown kinds and malformed timestamps are rejected.

## Mutations

`POST /api/routes/policy.cgi` requires the existing
`X-BROray-Request: operations` header and an allowed same-origin `Origin`.
Send JSON with `Content-Type: application/json`.

Every operation requires `expectedRevision`, `operation`, and `id`.
The request must contain exactly the fields for that operation:

| Operation | Additional fields | Effect |
| --- | --- | --- |
| `upsert` | `name` string, `domains` string | Add/edit a custom domain list. Domains are newline-separated import text. |
| `enabled` | `enabled` boolean | Change only the switch. |
| `action` | `action`: `deny`, `direct`, or `vpn` | Change only the destination action. |
| `delete` | None | Remove the matching rule. |
| `geo-upsert` | `name` string, `kind`: `geoip` or `geosite`, `tags`: nonempty string array | Queue adding/editing a managed selection, including downloading and checking the needed data. |
| `geo-update` | `kind`: `geoip` or `geosite`; `id` must equal `kind` | Queue checking/updating all added selections of that kind, including disabled rules. |

Unknown keys, duplicate keys (including case aliases), missing fields, null,
invalid types, trailing JSON, and invalid operations are rejected. The request
limit is 4 MiB; geo requests are additionally limited to 64 KiB. Decoded domain
text is limited to 2 MiB and 50,000 domains.
Names are trimmed, nonempty, at most 128 Unicode characters, without controls.
There can be at most 256 rules. Domain parsing and normalization use the shared
policy compiler: plain/suffix/exact domain entries, comments and deduplication;
URLs, ports, raw regex and IP literals are rejected.

New generic rules are **disabled with action `deny`**. A domains-page client
must explicitly select `vpn` before enabling its newly created VPN list.
Editing content preserves the existing enabled/action values. Editing a managed
GeoIP/GeoSite rule through custom-domain `upsert` is rejected. Existing managed
rules can use the independent controls only while their selected data remains
installed and verified, including selections in disabled rules. Ordinary
switch/action changes do not download missing data. Explicit `geo-upsert`
prepares the selection before saving it. Matcher kinds cannot be changed by
overwriting a custom/managed rule through the other operation.

Geo operations return HTTP 200 with the existing queue receipt in `data`
(`requestId`, `state`, `priority`, `coalesced`, `ok`). This confirms acceptance,
not a successful update. Follow the existing operations/queue result, then read
the policy again. Optional header `X-BROray-Nonce` contains 32 lowercase hex
characters and must be reused for an identical retry. Without it, a nonce is
generated; identical live requests still coalesce. CAS is rechecked after the
queued operation acquires its fence. A stale request fails before downloading.

## Transaction boundary

### Optional daily geodata updates

`GET /api/routes/geodata-schedule.cgi` returns `enabled`, its independent
`revision`, `intervalHours: 24`, `retryMinutes: 15`, `automationPaused` and
`attempts` keyed by `geoip`/`geosite`. Each attempt records operation ID, source,
start/retry times, status (`running`, `ok`, `failed`), optional completion time
and error code. It contains no downloaded payloads or raw server errors.

`POST` to the same endpoint requires the same request guards and exactly
`{"enabled":true,"expectedRevision":"<revision>"}` (4 KiB limit). The saved
setting is default off and independent of active policy, core availability,
unsaved forms and status updates. A stale settings revision returns 409.
Status writes preserve the latest switch; toggling preserves attempt metadata.
An unconfirmed response requires readback before another change.

The minute loop queues one due selected kind at a time, including selections
whose rules are off. Successful checks schedule the next check after 24 hours;
failures and unfinished attempts retain a 15-minute retry time across restarts.
The scheduler respects global automation pause, pending recovery and live jobs,
and obtains a fresh policy revision for each kind. After waiting for the policy
fence it checks the switch and pause again before fetching. Disabling during an
already running update allows that protected transaction to finish and prevents
the next automatic update. Manual updates remain available when daily updates
are off or global automation is paused.

The separate bounded 8 KiB `geodata-schedule.v1.json` uses strict canonical JSON,
nofollow directory/file checks, directory flock and atomic synced replacement.
Only a missing leaf means default off; unsafe/corrupt data blocks automatic work.
A failed final status write is reported as unconfirmed; it does not undo already
committed working policy or leave the global policy fence pending. The policy
and operation readback remain authoritative for the actual update result.

### Policy and data commit

1. Acquire the application `routes:policy` fence, bundle `policy`. Queued geo
   operations use the daemon-owned `routes:geodata` action in the same scope.
2. Read the saved state and capture state revision, exact existing config hash
   and fully verified geodata revision. Compare the client's revision.
3. Validate the prospective state and render it using an immutable context-local
   preview. Prepared rendering requires the mutation fence. Other requests and
   health probes continue to use their original configuration paths.
4. If an active server exists, generate and validate its prospective configuration
   with the installed Xray core, without applying it. Missing/ambiguous active
   identity is not treated as an inactive connection. Missing prior active
   config blocks the change because exact rollback cannot be guaranteed.
5. Recheck the captured baseline and persist the durable journal. Publish a
   prepared geodata pointer if needed, then apply the changed config through
   the existing Xray service. Install the policy state only after these steps,
   and confirm/read back the complete transaction.

Geo preparation includes every selected rule, even when disabled. It pins
immutable source commits, fetches only selected country files plus the common
GeoSite file, and verifies native contents, size and hashes. An unchanged
other-kind payload is reused only after full validation. The newly requested
tags are validated against its contents before the new metadata is accepted.
There is no fallback to a full GeoIP download.

Every selected asset is also loaded by the installed Xray core using a synthetic
scoped configuration, even with no active server. Validation passes JSON through
an owned pipe descriptor with explicit JSON format, writes no scratch/config
file, and does not start Xray. Missing validation or preview services are errors.

If all source identities and selections remain unchanged, the existing fully
verified generation is core-tested again and only the successful timestamp is
saved. No new generation, download or VPN restart is needed. Adding disabled
selections persists their files without enabling the rules or starting VPN.

No active server means state-only saving: existing retained config bytes remain
untouched and Xray is not started. The journal's nil config argument means
preserve the prior config, never delete it.

An active server is still previewed and core-validated. If the prospective config
is byte-for-byte identical to the captured current config, saving only commits
the state without restarting Xray. Adding/editing a disabled rule therefore does
not interrupt an otherwise unchanged active connection.

The protected operation uses the daemon's job context rather than the HTTP
request lifetime. Closing the page does not abandon an in-progress transaction.
An apply/install error restores the exact previous configuration through the
Xray service, verifies disk readback, and restores the old state and geodata
pointer. Owned candidates can be removed before the journal takes responsibility;
journal-referenced generations are retained for recovery. An unconfirmed
rollback or confirmation retains the journal and global fence. Startup recovery
uses the existing daemon-owned policy guard and exclusive kernel exclusions;
generic emergency recovery must not discard a pending policy transaction.

## Stable errors

| Code | HTTP | Meaning |
| --- | --- | --- |
| `CSRF_REJECTED` / `ORIGIN_REJECTED` | 403 | Existing request guards failed. |
| `POLICY_REQUEST_INVALID` | 400 | Invalid command or missing rule. |
| `POLICY_RULE_INVALID` | 400 | Compiler validation or selected geodata failed. |
| `POLICY_REVISION_CHANGED` | 409 | Reload saved state before retrying. |
| `POLICY_BUSY` | 409 | Another operation owns the fence. |
| `POLICY_APPLY_FAILED` | 500 | Previous settings were restored and verified. |
| `POLICY_RECOVERY_REQUIRED` | 503 | Outcome is uncertain; changes stay fenced. |

For queued geo operations these error codes appear in the operation result;
the earlier HTTP acceptance is not retroactively a successful update.
Other storage/core failures use the existing application error envelope. They
must not be interpreted as a successful save or implicit inactive state.

## Evidence limits

The implementation is exercised in offline Linux ARM64 QEMU with the pinned
official Xray core. Tests distinguish real core configuration validation from
fake service apply/rollback tests. Physical Keenetic/Entware behavior, memory
usage, UI integration, daily scheduling, backup/retention, interaction with VPN
reserve switching, extracted candidate validation and release readiness require
their own gates.
