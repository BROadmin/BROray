# DIAGNOSTIC-INVENTORY-001

Installed r06 baseline omitted home-snapshot and interface-reconcile and always
reported Xray as unknown. The read-only baseline is preserved in the workspace
at `docs/evidence/diagnostic-inventory-baseline-20260916`.

The report now includes all five background services. A bounded, single-document
Home cache provides an Xray PID candidate and an explicitly cached version.
Before reporting a running Xray, the reader verifies the actual executable,
exact persistent config arguments, non-validator role and stable full process
identity across the sample. It exports only PID, start ticks and role; it never
exports command digest, boot ID, config text, URLs or arbitrary errors.

The last updater operation is projected through a fixed allowlist from its
existing Home cache. Cached updater status is explicitly separate from live
daemon identity. Expired, future, oversized, linked or malformed cache data is
unavailable without refresh or raw fallback. The report adds a short summary
and states that independently sampled runtime facts are not an atomic snapshot.

Validation: 35 Linux tests (16 diagnostics, 7 projection, 12 HTTP) and 12 physical
ARM prefix checks. The first attempt found a missing direct jq module import;
both failed attempts and the corrected passing attempts are preserved. The ARM
identity fixture is bounded and was cooperatively stopped; namespaces were
archived and retired. The installed application and persistent Xray were not
changed by these prefix tests.

Limits: KeeneticOS metadata, live updater identity, direct WAN VPN continuity,
remaining route writer integration and final release acceptance remain open.
The installed router remains on r06c01 until a later signed candidate update.
