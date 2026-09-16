# Route entry supervision — 2026-09-16

## Source/lifecycle analysis before the change

Base: `cae055b4938f71b43933e53185a7234043b6b57b` on
`codex/broray-3.1.1-operations`. The existing route engine and its resume logic
remain the business implementation.

- `broray-routes` ran directly; its resource lease did not publish a common
  background-operation owner. Direct CLI admission could bypass a global fence.
- Route CGIs held a legacy five-file global fence and invoked the CLI. Merely
  wrapping that CLI in the coordinator creates a self-conflict. `verify.cgi`,
  preflight and custom import also write state outside the CLI call, so the
  whole CGI must belong to the protected job.
- Custom preview/commit consume stdin before acquiring the fence. Replaying
  the entry requires the already-read request file, with a separate child
  temporary file; the request path is not a bundle ID.
- CGI error helpers return shell status zero. Job completion must interpret
  their HTTP/JSON result to avoid recording errors as successful work.
- `broray-routes-user list` initialized the catalog. Listing should remain
  available while a mutation is blocked and should not create catalog state.

## Reproduced baselines

Evidence in the workspace `docs/evidence/`:

- `route-entry-baseline-20260916`: three real-Linux failures demonstrate missing
  owner admission, bypass of a foreign fence and a detached late writer.
- `route-api-entry-baseline-20260916`: original CGIs, fixture auth/business
  backends; three of five checks fail after CLI migration but before CGI
  migration. Auth and foreign-fence rejection already pass.
- `route-custom-entry-baseline-20260916`: direct custom preview returns success
  and writes its fixture despite the foreign fence; five prior CLI checks pass.
- `route-entry-linux-20260916` is a preserved harness failure (Alpine tools were
  outside the production CLI PATH), not a passing product test.

## Implementation

- One protected route job owns either the direct CLI or the complete CGI.
  The native supervisor ignores user cancellation for this mode, while owner
  death, OS signals and its internal deadline still terminate traced processes.
- A durable record links the route job to its supervisor. Nested entry requires
  a live full supervisor identity, its registry entry, actual `TracerPid`, and
  the worker's boot ID/PID/start ticks in the native child ledger. Copied tokens
  and environment flags alone do not grant admission.
- Allowed nested CLI actions stay within the original bundle and the existing
  call graph (for example, resume → export → build-export).
- CGI execution is selected from an exact local entry-path allowlist. The
  original auth and input validation run again inside the supervised process.
  Its response is buffered until helper drain and job finalization complete.
  HTTP/JSON errors mark the job failed.
- Custom request bodies survive replay; custom list reads the existing catalog
  or returns an empty list without a mutation job or catalog initialization.
- No route resume marker, transaction, resource lock or old ownership record
  is deleted as part of this change.

## Verified checkpoint results

- `route-entry-checkpoint-linux-20260916`: 59 PASS (6 CLI entry, 6 actual
  CGI entry with private auth/backend fixtures, 10 resource leases, 9 route
  cancellation, 6 resume admission, 14 native supervisor, 8 integration).
- `physical-route-entry-20260916`: 6 PASS on the verified Peak KN-2710 ARM64
  router, private prefix. Successful and failed CGI paths, custom request body,
  foreign fence, protected owner and detached writer all pass. The private
  namespace was archived and retired after coordinator helper drain.
- Running/startup Keenetic configuration hashes and the persistent Xray
  PID/start ticks/boot/executable/command hash match before and after the
  physical test. This is identity preservation, not VPN-traffic acceptance.
- Native ARM64 and x86_64 builds are reproducible; source checks pass.

## Remaining boundaries

This checkpoint is entry supervision, not complete route crash recovery or
release readiness. Resource-lease binding, exact dead-owner recovery, internal
ndmc timeout ownership, and installed-app route restore/resume acceptance remain
open. The historical system-uninstall rollback calls route export while holding
its own transaction fence and requires explicit integration/regression before
this source can replace the installed candidate. Prefix tests use harmless
backends and do not prove a real route mutation, HTTP session or VPN continuity.
