# Field report after 3.1.1-r12

## Confirmed source defects

1. The persistent updater's `routes_owner_interface()` checks the ownership
   receipt against literal `192.168.1.1:2080`. This rejects an otherwise valid
   installation on a different LAN before route capture and app-slot switching.
   An application-only update cannot repair the currently running updater before
   it captures routes. Delivery must reconcile the persistent platform first
   using the existing signed universal installer's platform transaction.
2. The connection monitor emits `available=true, up=false` for an address with
   failed connectivity. The auto-switch consumer reads `available` first and
   ignores `up`, resets failure count and exits as healthy. Previous integration
   coverage used legacy `available=false`, missing the current producer shape.

The original incident's global-lock cause is still not established. These two
defects do not prove that removing a lock was safe or sufficient.

## Correction contract

- Derive the SOCKS host and port from the validated ownership receipt. Require
  agreement with exactly one SOCKS inbound in the current regular Xray config
  and exactly one matching live local IPv4 address. Retain receipt schema,
  protocol, managed interface/metric and hash checks. Foreign, malformed,
  missing and ambiguous endpoints fail closed. Do not add a list of LAN ranges.
- Prefer `up`, then legacy `connected`, `healthy`, `available`. A present field
  with a wrong type produces unknown; it cannot fall back to apparent success.
  Count distinct failed monitor snapshots, retain threshold/cooldown and the
  no-automatic-start policy. Monitor freshness/target binding need separate
  consideration; do not equate JSON availability with VPN availability.
- Subscription UI silence is not yet reproduced. The precise action and UI
  state were requested; no personal subscription URL is needed at this stage.

## Reproduction

`tests/test_field_regressions.py` runs production shell functions with real
`ash`/`jq` in isolated Linux and deterministic address/status fixtures. Baseline
evidence is recorded separately at workspace
`docs/evidence/field-regressions-baseline-20260916/` before production changes.
This is not a physical update or failover acceptance result.

Published r12 artifacts remain immutable. A changed updater platform needs its
own signed metadata and physical update/rollback validation before publication.

## Verification and delivery notes

- The first fixed Linux run passed 19 tests (10 field regression methods and
  9 auto-switch integration tests), including distinct down samples, threshold,
  repeated-snapshot suppression and recovery reset. These use isolated files,
  real Linux operation owners and fixture transport, not a live VPN outage.
- Physical read-only validation initially caught a portability error in the
  proposed IPv4 validator: Entware jq 1.8.1 was compiled without Oniguruma.
  The final validator uses character codes and numeric octets, without regex.
  The corrected native ARM run accepted the actual ownership receipt and
  completed route capture and verification. This router had zero managed
  routes; the 868-route case is an isolated Linux fixture. Configuration,
  active server, installed updater and running network configuration were
  unchanged. Monitor up/down cases used private temporary status files.
- The Linux route lifecycle suite passed 13 methods, including capture and
  restore of 868 routes on LAN 192.168.2.1, no route writes when unchanged,
  restoration of one missing route, preservation of foreign routes, and
  refusal of an ambiguous managed-route record. Its first 45-second fixture
  budget was too short for thousands of processes under QEMU; the expanded
  budget passed without changing production route restoration code.
- Six production-page browser scenarios passed with HTTP fixtures: create
  success, parse error, HTTP error, malformed JSON, network failure, and a
  pending request followed by success. A pending request displays a busy
  button. This does not reproduce the customer's private URL or demonstrate
  end-to-end subscription loading on that installation.
- The historical lock baseline test now reads the immutable archived updater
  and retains both its archive and source hash assertions. It no longer
  mistakes the evolving release source for the original 3.1.0 updater.
- The final full-activation regression passed: a down monitor triggered
  candidate probing, protected config application, active-server replacement,
  terminal job completion and cooldown without a second restart. A native
  private process satisfies the production Xray identity check; transport and
  init remain fixtures. The initial PID-1 mock correctly failed that check and
  caused rollback, so it was replaced in the test, not allowed in production.
  This is not a real VPN outage or a physical Xray restart test.

Final coverage: 13 field methods and 1 activation method in the final VM,
9 previously passing auto-switch job methods with identical auto-switch source,
1 archived legacy baseline method, and 6 browser scenarios. Source checks passed
for 273 shell files, 36 JSON files and 33 JavaScript files. The bundled platform
manifest matches the revised updater. No new release has been published.

Evidence is in workspace `docs/evidence/field-regressions-fixed-20260916/`,
`field-final-20260916/`, `field-router-readonly-v3-20260916/`, and
`field-subscription-ui-20260916/`. Failed preliminary runs are retained separately.

The existing public universal installer calls `ensure_target_platform()` before
requesting an app update. That transaction verifies the signed platform bundle,
backs up regular installed files, stops and rechecks the idle updater, replaces
and verifies the platform, then restarts it; activation failure restores the
previous files. It permits a regular locally patched updater to be reconciled.
An application handoff occurs only after app-slot activation, so it cannot
repair this pre-capture failure on its own. A future signed release must update
both the platform bundle metadata/updater digest and the application's bundled
platform manifest. The existing r12 publication script copies the old platform
and is unsuitable for this delivery without an explicit new release workflow.
