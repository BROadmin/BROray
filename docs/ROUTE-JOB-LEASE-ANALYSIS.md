# Route job / resource lease binding — 2026-09-16

Base: `684e89ee308a703eba8abfff226fac2967fb536a`.

Before editing the resource library/controller, two failures were reproduced
in `route-job-lease-baseline-20260916` with real Linux processes:

1. A lease acquired inside a protected route job recorded only its resource
   token and process identity; it had no link to the operation/supervisor.
2. An unrelated process carrying copied operation token/flags could use the
   legacy resource API without proving membership in the protected job.

## Change under test

Resolve the operation context through the existing coordinator before taking
the resource guard. Under the resource guard, independently verify the live
supervisor identity, durable job/supervisor record, active protected state,
actual caller and publisher `TracerPid`, and both process birth identities in
the native child ledger. Store the exact operation ID, token digest (not the
job token), supervisor identity, action and bundle in the lease's `job` field.

The release path must match that same context as well as the existing resource
token and full lease-owner identity. An unbound compatibility lease uses a
null context and keeps its former conservative lifecycle. Existing ambiguous,
partial and unknown leases remain untouched.

No coordinator call occurs while holding the resource guard. This preserves
the lock order needed by a later coordinator-led recovery. Active traced
callers/publishers prevent ordinary job retirement during publication.

## Results

- `route-job-lease-linux-20260916`: 2 new binding checks plus 10 existing
  resource-lease checks PASS on real Linux processes.
- `route-job-lease-linux-20260916-02`: the 2 binding checks PASS again with an
  additional direct-publisher attempt using the live caller PID and copied
  context. The untraced publisher is rejected before creating a lease.
- `physical-route-job-lease-20260916`: 3 private-prefix ARM checks PASS: exact
  binding, compatibility lease lifecycle, and rejection of a completed job's
  copied token. The persistent Xray identity remains unchanged; test namespaces
  were archived and retired. No application installation or route write.
- Source checks PASS. These are 12 distinct Linux tests and 3 physical-prefix
  checks; the repeated 2 tests are not counted twice.

## Scope

This stage records ownership evidence; it does not implement stale resource
retirement, change route progress, continue routes, or permit protected crash
recovery. Those transitions need their own baseline and failure tests.
