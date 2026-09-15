# Server job admission and supervised measurements

The server CGI now parses input before handing work to a dedicated executor.
That executor acquires and acknowledges its own operation. Closing or killing
the CGI cannot retire the worker's ownership. The CLI executes the same worker;
scheduled checks honor the persistent automation pause. Backend failure and
HTTP process exit are separate: a business error keeps the HTTP envelope while
the operation records failure.

The reproduced pre-fix service wrote persistent quality without admission. The
new service requires the actual executor identity, including rejection of an
ash subshell with inherited `$$` and token. The original failing source/test/log
are frozen in workspace evidence `server-owner-reproducer-20260915`.

Checks run the production generator, Xray config validation, temporary proxy
probe and ping under the native supervisor with a 120-second deadline. Scratch
and a copy of previous quality live in a private operation directory. The
temporary Xray belongs to that tree: the probe no longer sends signals to a PID
from a shell variable. The owner confirms tree drain and validates the complete
result before atomic, durable publication. A complete negative measurement
updates failure counters; cancellation, timeout and incomplete output preserve
the previous quality. Persistent VPN Xray is never started inside this helper.

Manual import parses into a private supervised server directory, then the owner
publishes the validated new server. Invalid input releases the operation without
changing the catalog. Delete, activate, deactivate and the final Home snapshot
use owned jobs. Failed or interrupted protected mutations retain their fence
until domain consistency can be established.

## Validation boundaries

Tests use real Linux processes and the production server/probe code with local
synthetic Xray, curl, netstat and ping executables. Physical tests use the same
production code and ARM64 supervisor on the verified Keenetic, in private test
directories. These fixtures do not establish persistent Xray/VPN continuity.

An initial implementation test found that a local declaration in the EXIT
handler discarded the incoming status. The handler now receives the original
status as an explicit trap argument; failure evidence is preserved separately.
The orphan CGI test waits for the complete worker exit protocol: durable
terminal state intentionally precedes fence retirement.

## Open release gates

- Auto-switch and the separate legacy `broray` command still need coherent
  admission integration; they must not call the CLI while owning another job.
- Protected domain crash recovery, active configuration rollback and failed
  fsync recovery remain unverified. Keeping the fence is a fail-closed boundary,
  not a completed recovery implementation.
- Runtime generation and journal/report integration, legacy delivery, full
  installed application, real VPN, reboot and endurance acceptance remain open.

The checkpoint manifest records final counts and exact source/evidence hashes.
This stage is not a release artifact or release-readiness declaration.
