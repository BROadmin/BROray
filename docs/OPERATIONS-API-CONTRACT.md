# Operations API and diagnostic contract — design for 3.1.1

Status: specified; CGI endpoints are not installed. Use the existing `auth-common.sh` session/auth flow and existing request-size/CSRF/origin checks. A status GET must not issue a network probe, mutate subscription state, or invoke old `list` recovery helpers. Recovery is an explicit POST or a controlled service-start scan.

## Endpoints

| Method/path under `/api/operations/` | Input | Successful response | Side effect |
| --- | --- | --- | --- |
| GET `status.cgi` | none | public snapshot with revision, automationPaused, operations, completeness/errors | none |
| GET `events.cgi` | optional bounded cursor, maximum 500 | centrally projected events, nextCursor, truncated, complete | none |
| GET `report.cgi` | none | one JSON attachment ≤1 MiB | none; no raw config/log collection |
| POST `cancel.cgi` | `{operationId}` | 202 cancelRequested, or alreadyFinished | same-generation cancel flag |
| POST `stop-background.cgi` | `{pauseAutomation:true}` | paused state and per-operation outcomes | atomically pause new automatic admission and request cancellation of eligible operations |
| POST `recover.cgi` | `{}` | recovered / active / needsRecovery with reason | reclassify under coordinator and retire only confirmed stale fences |
| POST `automation.cgi` | `{paused:boolean}` | saved pause state | preserve each automation's user settings; manual operations still admitted normally |

Reject unknown keys, malformed JSON, bodies >4 KiB, GET mutations, unauthenticated sessions, mismatched origin/CSRF, arrays, oversized identifiers and unexpected media types. Never accept PID, signal number, owner token, command, filesystem path or URL from HTTP. No mutating JSONP/CORS.

409 = live incompatible owner or protected operation; 503 = missing guard/state, ambiguous owner, incomplete snapshot; 400 = malformed input; 401/403 = existing auth contract. HTTP 200 with an empty list must never hide a registry read failure. Idempotency uses operation ID plus current generation; cancellation cannot apply to a newer operation with a recycled PID.

## Snapshot

Public fields come from `operation-public.jq` for all egress. Internal owner token, executable/cmdline digest, bundle IDs and arbitrary text are excluded. Keep `capturedAt`, `complete`, component availability and error codes separate from operation status. UI maps fixed enums to readable Russian text, uses `textContent`, and retains the last known snapshot with a stale indicator if refresh fails. Cancel accepted means a request was recorded, not that a process has stopped.

## One-file report

```json
{
  "schemaVersion": 1,
  "reportKind": "broray-diagnostics",
  "capturedAt": "2026-09-15T12:00:00Z",
  "redactionPolicy": "allowlist-v1",
  "complete": false,
  "unavailable": ["keeneticOS", "vpnContinuity"],
  "build": {"appVersion": "3.1.1", "candidateId": null, "webuiBuild": null},
  "platform": {"architecture": "aarch64", "kernel": null, "uptimeSeconds": null},
  "services": {"xray": "unknown", "scheduler": "unknown", "updater": "unknown"},
  "automation": {"paused": false, "subscriptionUpdate": null, "serverCheck": null, "autoSwitch": null},
  "operations": [],
  "fences": {"global": "unknown", "updaterRequest": "unknown", "routesPending": "unknown"},
  "events": [],
  "errors": []
}
```

This is a schema example, not an observation of a router. Populate only already cached/available facts. Parse versions with strict version grammar, enums for service/fence states and boolean automation settings; reject free-form platform descriptions. Include at most 500 projected events and 20 terminal operations; never prune active/ambiguous/protected records. Unknown is distinct from stopped/false/empty.

Do not include subscription URLs, server URIs, UUIDs, credentials, keys, cookie/header values, `/proc/*/cmdline`, configuration files, raw stderr, serial numbers, hostnames or external IPs. A masked hostname can still identify a private provider; omit URLs completely in this release unless a field is demonstrably required. Copy and download use the same report builder/projection.

Content-Disposition uses a fixed safe filename with a timestamp, Content-Type application/json, Cache-Control no-store, X-Content-Type-Options nosniff. Bound reads before parsing. If a component fails, produce a partial report with availability flags; never silently declare success or paste raw fallback output. A reporter must not take the long-operation resource fence. Its short snapshot lock has a bounded wait and an explicit busy result.
