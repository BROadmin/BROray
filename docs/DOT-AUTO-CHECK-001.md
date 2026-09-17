# DoT automatic TLS/SNI checks — stage 10

Base: 725c53f3ae2077e9f1abb8db0129554abc59c320 with exact stages 01–09.
Scope: repeat checks of the SAVED built-in DoT selection; never DNS installation,
removal, provider replacement, subscription updates or arbitrary endpoint discovery.

## User contract
One checkbox on the existing DNS-over-TLS page. Disabled by default for existing
and new installations. Enabling is a separate saved setting, not a change to the
server checkboxes. Unsaved server choices are never persisted by this operation.
The existing scheduler checks due work every normal cycle (normally 60 seconds).
TLS checks are due after 300 seconds; healthy recent manual tests delay a repeat.
This works without an open browser, while the normal BROray scheduler is running.
A busy coordinator defers admission. No guarantee of exact execution under load.
Global automation pause blocks automatic admission. Individual cancellation stops
the current check; the normal schedule resumes unless the feature/automation is off.

## Safety and lifecycle
A fresh dot:auto-check job uses the existing native owner, global fence and native
supervisor. The daemon is never the operation owner. The probe can write only in
its private preparation directory. Native helpers-drain precedes state publication.
Timeout is 12 seconds (+2 seconds kill grace) per endpoint and 150 seconds for the
whole helper. Missing timeout causes a failed attempt, not an unbounded connection.
No new daemon, cron entry, external service, or mandatory package is added.
The helper revalidates endpoints against the built-in catalog before connecting.
No Keenetic CLI command, DNS write, routing change or Xray restart is performed.
It tests TLS certificate identity using the existing OpenSSL options; it does not
send a DNS query and does not prove complete DNS resolution or client connectivity.

## Data and presentation
Setting: routes/dot/auto-check.json {schemaVersion:1,enabled:boolean,updatedAt}.
POST /api/routes/dot-auto-settings.cgi accepts ONLY {enabled:boolean}, requires a
session, POST, X-BROray-Request: operations and the existing KeenDNS-aware Origin
validator. Its owner publishes the setting atomically. Failure does not enable it.
The ordinary DoT status response includes autoCheck; the UI already polls status.
Read-only status retrieval never starts TLS probes or changes the saved selection.
State.tests, lastTestedAt and lastTestedEpoch update only after verified probe output.
The previous lastError and lastOperation (e.g. failed apply/delete) are retained.
autoCheck.lastAttemptEpoch throttles failed/internal attempts as well as successes.
Cancelled or unresolved work cannot publish fresh successful test results.

An expired successful check is not labelled as a failed connection. When automatic
checks are enabled and the installation is independently confirmed, the page shows
that fresh TLS evidence is awaited. Counters remain stale/zero as applicable and
installation still requires the existing exact successful 600-second gate.
A real failed TLS check stays an error even when old. Unknown router observation,
missing OpenSSL, capacity conflicts and recovery markers keep their existing gates.
The feature never removes recovery markers or releases ambiguous locks.

## Integration
CUMULATIVE-CURRENT is the primary delivery for a clean pinned HEAD. The incremental
patch requires exact stage09 cumulative files. Do not apply both forms or regenerate
implementation. Use the existing release builder for permissions, manifests/cache IDs
and candidate packaging. No candidate is built/published by this development package.
Acceptance still requires the physical router, real TLS network, actual KeenDNS
session, stopped/running scheduler, reboot and updater preservation of the setting.
The previously recorded DoT delete confirmation race is a separate open issue.
