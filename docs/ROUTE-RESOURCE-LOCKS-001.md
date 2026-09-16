# ROUTE-RESOURCE-LOCKS-001

The physical baseline reproduced removal of unknown download/check resource
locks and automatic archival of a five-file global route lock solely because
its PID was absent. Baseline evidence remains unchanged.

All eight consumers of `routes/locks/operation.lock` now use the same short
native guard for publication and release. A generation token and full actual
owner identity bind release to the process that acquired the resource. Release
checks every entry and compatibility projection before the first unlink.
Changed generations, unknown/hidden entries, symlinks and copied tokens are
preserved. Existing locks are never reclaimed by PID absence, age or boot change.
The kernel guard file remains in place; its kernel lease ends with its command.
Bundle projections and the delete operation/start timestamp are preserved.

The old global route API reclaimer now refuses five-file automatic recovery.
This is a conservative compatibility fix. It does not implement global route
job admission, helper cancellation or protected domain recovery. A crashed
resource remains fenced until a separately verified recovery can establish
that its helpers and domain mutation have ended.

Verification covers all eight consumers, actual process identity, foreign
generations, copied tokens, hidden evidence, symlinks and guard contention.
The Linux business-flow fixture additionally runs check, download and export
file generation without network or router mutations. Physical prefix checks
do not install or export any real route.

The first guard timing fixture was flawed: it expected a two-second upper bound
including shell overhead and let a three-second holder expire during acquisition.
The corrected fixture uses a cooperative stop file and a bounded 20-second
fallback. Earlier failed and passing intermediate attempts remain preserved.

Still required: global CGI/CLI admission and ownership transfer, safe ndmc lane,
removal of legacy PID-directed timeout signals, protected resume/rollback,
installed full application acceptance and final release tests.
