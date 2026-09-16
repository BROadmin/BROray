# Legacy 3.1.0 recovery: remaining proof obligations

Analysis on 2026-09-16. No legacy recovery implementation or acceptance is
claimed by this document. The installed router is still 3.1.1-r07c01.

## Delivery

The earlier open Web CLI delivery question was resolved by the harmless physical
probe in `docs/evidence/keenetic-webcli-delivery-20260916.json` (workspace root):
Keenetic 5.1 Web CLI executed `exec sh -c` and returned the marker. This proves
command delivery, not successful repair. A signed, verified compatibility bundle
can use that delivery path without relying on admission in the blocked updater.

## Why an absent stored PID is insufficient

The original five-file global lock has pid/scope/action/bundle/startedAt, with no
boot ID, birth time, worker lease or descendant ledger. An orphaned shell/helper
can outlive its parent. Existing full-identity recovery must continue refusing
this record until legacy producers and remaining writers are proved quiescent.
The format label `stale-route-owner` does not identify the original cause.
ROOT_CAUSE_NOT_PROVEN remains unchanged.

## Exact archived source observations

- `broray-subscription-scheduler` gates each cycle on the global lock and sleeps
  60 seconds by default between cycles. The scheduler cycle executes in its
  own shell and may create helpers.
- `broray-server-auto-switch` gates each cycle on the global lock and has an
  additional local cycle lock. Its inter-cycle sleep is 15 seconds; a separate
  three-second sleep occurs inside business work and is not an idle witness.
- `broray-home-snapshotd` can restart the connection monitor and invokes the
  lighttpd guard. It must be quiesced before those producers. Its sole direct
  sleep child of 30 seconds is the already-tested narrow idle migration case.
- `broray-connection-monitor` sleeps 10 seconds by default and performs cache
  and maintenance work. A signal through its old stop script is not authorized
  by PID-file contents alone.
- Old interface reconciliation is an inline `ash -c` program launched from
  `S24broray`, not the new `broray-interface-reconcile` executable. It has a
  finite startup loop. A filename-only inventory would miss this writer.
- Lighttpd creates CGI workers; pausing only schedulers does not exclude manual
  writers. The Home lighttpd guard can restart the web service.
- Old HUP/INT/TERM/EXIT handlers can remove PID-based locks or signal cached
  children. The tested idle helper bypasses these traps only after ptrace pins
  and revalidates the exact task; it is not a general PID-directed kill tool.

## Candidate narrow compatibility approach, not yet implemented

1. Verify the exact supported archive generation and all relevant executable
   sources, preserve configuration and Xray identity, and archive the exact
   global-lock generation before changes.
2. Pause new-version automation. Quiesce only verified legacy producers at
   audited idle points; preserve busy/ambiguous tasks. Account for the Home
   daemon's restart paths and the old inline reconcile worker.
3. Exclude new CGI admission and inspect surviving executors/helpers. A scan
   for the word BROray alone is insufficient: orphaned curl/jq/mv commands need
   separate accounting. Unknown potential writers must retain the fence.
4. Retire only the unchanged complete supported lock generation after this
   proof, retaining domain state and route resume/restore data. Do not infer
   quiescence from lock age or a missing PID.
5. Restore the web service as needed and use the normal signed update path.
   Keep automation paused and verify persistent Xray/configuration continuity.

The inventory/admission exclusion in step 3 is still unresolved; stopping an
idle daemon alone does not satisfy it. Neither the existing private home helper
nor the completed emergency-button stage closes legacy recovery.
