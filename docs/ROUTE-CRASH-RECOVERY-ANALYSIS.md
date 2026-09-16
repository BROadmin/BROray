# Route crash recovery — baseline, 2026-09-16

Source before recovery edits: `09ea1a15b8c2c8b77c36f53be88d7594b45b3ab5`.

`route-crash-recovery-baseline-20260916` reproduces two failures on real Linux
processes in an offline guest. The actual protected CLI starts a harmless route
backend fixture, acquires its bound resource lease, and optionally records
progress. The test terminates only its own unreaped direct child and waits for
the native supervisor and traced descendants to disappear. Both recovery calls
then return `protected_recovery` / `RECOVERY_BLOCKED`.

This is an expected failing baseline, not a passing recovery check. No router,
route command, network request, or VPN connection was used.

## Narrow intended change

Explicit recovery may retire a route job's control records only after proving
the full executor identity stale, all registered supervisors and descendants
absent, and publication settled. A durable protected-route supervisor marker
must identify the new job protocol; legacy or ambiguous ownership stays fenced.

Under coordinator then resource guard, retire only the exact resource generation
bound to that job. Preserve unexpected files, foreign bindings and live owners.
Archive a complete known generation before releasing the global fence; never
delete arbitrary route locks by PID or age.

Bind newly written progress to the verified job. For an interrupted running
record, preserve the old JSON and materialize the existing interrupted display
state. Keep counters and transaction evidence. Do not invent `resumable=true` or
claim routes were restored. An already committed resumable record stays intact.

Recovery must not run route commands, continue a route operation, roll it back,
or introduce new route controls. Actual route repair remains in the existing
page actions. A retained resource lease must also prevent normal job finish from
releasing the global fence.

## Required evidence before acceptance

Successful stale recovery and retry; progress backup and interrupted state;
preservation of live/ambiguous helpers, legacy/foreign bindings and unexpected
files; rejection of unguarded recovery; failed cleanup retaining the fence;
already resumable progress unchanged. Then ARM private-prefix checks and actual
installed-candidate acceptance, with route transactions and updater protection
preserved. None of these follow-up checks is claimed complete by this baseline.

## Implemented and verified, 2026-09-16

The explicit recovery path now validates the recorded protected-route marker,
stale full identities, settled publication and the drained supervisor registry.
Under both native guards it archives only the exact bound resource generation.
Unknown files, foreign bindings and live or ambiguous owners keep the fence.
Running progress is bound to its background operation. A durable recovery plan
preserves the original JSON and counter before publishing an interrupted state;
retry accepts only the recorded before/after pair. Existing resumable progress
is preserved. No route commands, automatic resume or rollback run in recovery.

Normal finish also retains the global fence if a resource lease or running
progress remains. This latter defect was reproduced on the physical ARM router
in `physical-route-finish-baseline-20260916` before correction.

Evidence outside the source repository:

- `route-crash-recovery-linux-20260916-02`: 34 PASS (14 crash cases,
  2 lease binding, 6 admission, 12 recovery flow), before the final finish check.
- `route-recovery-final-linux-20260916`: 29 PASS on the final source (5 affected
  crash/finish cases, 6 entry supervision, 6 admission, 12 recovery flow).
- `physical-route-crash-20260916`: 7 PASS in an isolated ARM prefix.
- `physical-route-finish-20260916`: 1 PASS with the final finish check, followed
  by successful explicit recovery. All physical namespaces archived and retired;
  installed Xray identity unchanged. Harmless route backends, no network changes.
- Local source checks PASS. Early Linux attempts exceeded fixture readiness
  budgets under concurrent TCG guests; those failures remain recorded. The
  fixture budget was increased; production deadlines were not changed.

These are source and private-prefix checks. Full installed-candidate acceptance
and release publication remain separate stages.
